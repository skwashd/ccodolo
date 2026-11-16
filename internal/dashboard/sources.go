package dashboard

import (
	"context"
	"slices"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/skwashd/ccodolo/internal/docker"
)

// Messages the sources send to the model.
type (
	sessionsMsg struct {
		rt       docker.Runtime
		sessions []docker.Session
		err      error
	}
	statsMsg struct {
		rt    docker.Runtime
		id    string
		stats docker.Stats
	}
	sourceErrMsg struct {
		rt  docker.Runtime
		err error
	}
)

// controller is what the model needs from the data sources; tests swap in
// a fake.
type controller interface {
	// Refresh re-lists sessions now, e.g. after an action.
	Refresh()
	// Pause stops stats collection while an interactive session owns the
	// terminal (nothing is drawn then); Resume restarts it.
	Pause()
	Resume()
}

// sources runs one feed per backend. Docker: a `docker events` stream
// triggers re-lists, and one `docker stats` stream (restarted only when
// the set of running sessions changes) supplies samples. Apple has no
// event stream, so its loop re-lists and samples about once a second.
type sources struct {
	feeds []*feed
}

type feed struct {
	rt     docker.Runtime
	send   func(tea.Msg)
	lister *docker.Lister

	refresh chan struct{}
	mu      sync.Mutex
	paused  bool
	resume  chan struct{} // closed on Resume; replaced on Pause
}

func newSources(runtimes []docker.Runtime, send func(tea.Msg)) *sources {
	s := &sources{}
	for _, rt := range runtimes {
		resume := make(chan struct{})
		close(resume)
		s.feeds = append(s.feeds, &feed{
			rt:      rt,
			send:    send,
			lister:  docker.NewLister(rt),
			refresh: make(chan struct{}, 1),
			resume:  resume,
		})
	}
	return s
}

func (s *sources) start(ctx context.Context) {
	for _, f := range s.feeds {
		if f.rt == docker.RuntimeApple {
			go f.runApple(ctx)
		} else {
			go f.runDocker(ctx)
		}
	}
}

func (s *sources) Refresh() {
	for _, f := range s.feeds {
		f.requestRefresh()
	}
}

func (s *sources) Pause() {
	for _, f := range s.feeds {
		f.mu.Lock()
		if !f.paused {
			f.paused = true
			f.resume = make(chan struct{})
		}
		f.mu.Unlock()
		f.requestRefresh() // wake the loop so it stops its stats process now
	}
}

func (s *sources) Resume() {
	for _, f := range s.feeds {
		f.mu.Lock()
		if f.paused {
			f.paused = false
			close(f.resume)
		}
		f.mu.Unlock()
		f.requestRefresh()
	}
}

func (f *feed) requestRefresh() {
	select {
	case f.refresh <- struct{}{}:
	default: // one is already pending
	}
}

// waitResumed blocks while paused; false means ctx ended.
func (f *feed) waitResumed(ctx context.Context) bool {
	f.mu.Lock()
	ch := f.resume
	f.mu.Unlock()
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

func (f *feed) isPaused() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paused
}

func (f *feed) list(ctx context.Context) []docker.Session {
	sessions, err := f.lister.List(ctx)
	if ctx.Err() == nil {
		f.send(sessionsMsg{rt: f.rt, sessions: sessions, err: err})
	}
	return sessions
}

func runningIDs(sessions []docker.Session) []string {
	var ids []string
	for _, s := range sessions {
		if s.Running {
			ids = append(ids, s.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func (f *feed) runDocker(ctx context.Context) {
	go func() {
		// Events only nudge a re-list. If the stream dies (daemon
		// restart), retry slowly rather than spin.
		for ctx.Err() == nil {
			_ = docker.WatchEvents(ctx, f.requestRefresh)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				f.requestRefresh()
			}
		}
	}()

	var (
		statsIDs    []string
		stopStats   context.CancelFunc = func() {}
		statsActive bool
	)
	defer func() { stopStats() }()

	startStats := func(ids []string) {
		stopStats()
		statsIDs = ids
		statsActive = true
		sctx, cancel := context.WithCancel(ctx)
		stopStats = cancel
		go func() {
			err := docker.StreamDockerStats(sctx, ids, func(id string, s docker.Stats) {
				f.send(statsMsg{rt: f.rt, id: id, stats: s})
			})
			if err != nil && sctx.Err() == nil {
				f.send(sourceErrMsg{rt: f.rt, err: err})
			}
		}()
	}

	f.requestRefresh()
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.refresh:
		}
		if f.isPaused() {
			stopStats()
			statsActive = false
			if !f.waitResumed(ctx) {
				return
			}
		}
		ids := runningIDs(f.list(ctx))
		if !statsActive || !slices.Equal(ids, statsIDs) {
			startStats(ids)
		}
	}
}

// Apple loop pacing. With running sessions it polls once a second; a CLI
// version whose stats call itself takes 2s paces it slower. With nothing
// running it re-lists every applePollIdle, or at once on a refresh.
const (
	applePollInterval = time.Second
	applePollIdle     = 2 * time.Second
)

func (f *feed) runApple(ctx context.Context) {
	sampler := docker.NewAppleSampler()
	for ctx.Err() == nil {
		if !f.waitResumed(ctx) {
			return
		}
		started := time.Now()
		ids := runningIDs(f.list(ctx))
		wait := applePollIdle
		if len(ids) > 0 {
			samples, err := sampler.Sample(ctx, ids)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				f.send(sourceErrMsg{rt: f.rt, err: err})
			}
			for id, s := range samples {
				f.send(statsMsg{rt: f.rt, id: id, stats: s})
			}
			// A refresh requested meanwhile is satisfied by the next list.
			select {
			case <-f.refresh:
			default:
			}
			if err == nil {
				wait = max(applePollInterval-time.Since(started), 0)
			}
		}
		select {
		case <-ctx.Done():
		case <-f.refresh:
		case <-time.After(wait):
		}
	}
}
