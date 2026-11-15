package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

// Labels set on every container at launch (runArgs) so the dashboard can
// find sessions and describe them without parsing container names.
const (
	LabelProject = "ccodolo.project"
	LabelWorkdir = "ccodolo.workdir"
	LabelAgent   = "ccodolo.agent"
	LabelTmux    = "ccodolo.tmux"
)

// containerNamePrefix starts every ccodolo container name (see Run).
const containerNamePrefix = "ccodolo-"

// sessionLabelArgs returns the --label flags for a new container.
func sessionLabelArgs(project, workdir, agentName string, tmux bool) []string {
	tmuxVal := "0"
	if tmux {
		tmuxVal = "1"
	}
	return []string{
		"--label", LabelProject + "=" + project,
		"--label", LabelWorkdir + "=" + workdir,
		"--label", LabelAgent + "=" + agentName,
		"--label", LabelTmux + "=" + tmuxVal,
	}
}

// Session is one ccodolo container, running or not.
type Session struct {
	ID      string
	Name    string
	Project string
	Workdir string // host path
	Agent   string
	Runtime Runtime
	Started time.Time // zero when unknown
	Running bool
	// Tmux is true when the agent runs in the "agent" tmux session, so
	// attach, shells, tasks, and review are available.
	Tmux bool
}

// Lister discovers sessions for one backend. It caches what a container's
// inspect output says, so a refresh inspects only containers it has not
// seen before.
type Lister struct {
	rt    Runtime
	cache map[string]Session
}

// NewLister returns a Lister for rt.
func NewLister(rt Runtime) *Lister {
	return &Lister{rt: rt, cache: make(map[string]Session)}
}

// List returns every ccodolo container known to the backend.
func (l *Lister) List(ctx context.Context) ([]Session, error) {
	var sessions []Session
	var err error
	if l.rt == RuntimeApple {
		sessions, err = l.listApple(ctx)
	} else {
		sessions, err = l.listDocker(ctx)
	}
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		seen[s.ID] = true
	}
	for id := range l.cache {
		if !seen[id] {
			delete(l.cache, id)
		}
	}
	return sessions, nil
}

func (l *Lister) listDocker(ctx context.Context) ([]Session, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "name="+containerNamePrefix, "--format", "{{.ID}}\t{{.State}}").Output()
	if err != nil {
		return nil, unavailable(err)
	}
	states := parseDockerPS(out)

	var unseen []string
	for id := range states {
		if _, ok := l.cache[id]; !ok {
			unseen = append(unseen, id)
		}
	}
	if len(unseen) > 0 {
		out, err := exec.CommandContext(ctx, "docker", append([]string{"inspect"}, unseen...)...).Output()
		// A container removed between ps and inspect makes inspect exit
		// non-zero but still print the rest.
		if len(out) == 0 && err != nil {
			return nil, fmt.Errorf("inspecting containers: %w", cliError(err))
		}
		inspected, perr := parseDockerInspect(out)
		if perr != nil {
			return nil, perr
		}
		for _, s := range inspected {
			l.cache[s.ID] = s
		}
	}

	var sessions []Session
	for id, state := range states {
		s, ok := l.cache[id]
		if !ok {
			continue
		}
		s.Running = state == "running"
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func (l *Lister) listApple(ctx context.Context) ([]Session, error) {
	out, err := exec.CommandContext(ctx, "container", "ls", "-a", "--format", "json").Output()
	if err != nil {
		return nil, unavailable(err)
	}
	listed, err := parseAppleSessions(out)
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(listed))
	for _, s := range listed {
		if cached, ok := l.cache[s.ID]; ok && !cached.Started.IsZero() {
			s.Started = cached.Started
		} else if s.Running && s.Started.IsZero() {
			// CLI versions whose listing has no startedDate: PID 1's
			// elapsed time is the uptime. Asked once per container.
			s.Started = appleStarted(ctx, s.ID)
		}
		l.cache[s.ID] = s
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func appleStarted(ctx context.Context, id string) time.Time {
	out, err := exec.CommandContext(ctx, "container", "exec", id, "ps", "-o", "etimes=", "-p", "1").Output()
	if err != nil {
		return time.Time{}
	}
	secs, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return time.Time{}
	}
	return time.Now().Add(-time.Duration(secs) * time.Second)
}

// parseDockerPS parses `ID\tState` lines into a map of ID to state.
func parseDockerPS(out []byte) map[string]string {
	states := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		id, state, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if id != "" {
			states[id] = state
		}
	}
	return states
}

// parseDockerInspect parses `docker inspect` output. IDs are truncated to
// the 12 characters `docker ps` prints, so they key the same cache entry.
func parseDockerInspect(out []byte) ([]Session, error) {
	var raw []struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State struct {
			Running   bool   `json:"Running"`
			StartedAt string `json:"StartedAt"`
		} `json:"State"`
		Mounts []struct {
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing container inspect output: %w", err)
	}
	var sessions []Session
	for _, c := range raw {
		name := strings.TrimPrefix(c.Name, "/")
		if !strings.HasPrefix(name, containerNamePrefix) {
			continue
		}
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		s := Session{ID: id, Name: name, Runtime: RuntimeDocker, Running: c.State.Running}
		if t, err := time.Parse(time.RFC3339Nano, c.State.StartedAt); err == nil && t.Year() > 1 {
			s.Started = t
		}
		mounts := make([]mount, 0, len(c.Mounts))
		for _, m := range c.Mounts {
			mounts = append(mounts, mount{src: m.Source, dst: m.Destination})
		}
		applySessionMetadata(&s, c.Config.Labels, mounts, c.Config.Image)
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// parseAppleSessions parses `container ls -a --format json`, keeping only
// ccodolo containers. As in parseAppleContainerList, the user-supplied
// --name is the container's identifier.
func parseAppleSessions(out []byte) ([]Session, error) {
	var raw []struct {
		Status        appleStatus `json:"status"`
		Configuration struct {
			ID     string            `json:"id"`
			Labels map[string]string `json:"labels"`
			Image  struct {
				Reference string `json:"reference"`
			} `json:"image"`
			Mounts []struct {
				Source      string `json:"source"`
				Destination string `json:"destination"`
			} `json:"mounts"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing container list: %w", err)
	}
	var sessions []Session
	for _, c := range raw {
		id := c.Configuration.ID
		if !strings.HasPrefix(id, containerNamePrefix) {
			continue
		}
		s := Session{ID: id, Name: id, Runtime: RuntimeApple, Running: c.Status.State == "running"}
		if c.Status.StartedDate != nil && s.Running {
			s.Started = *c.Status.StartedDate
		}
		mounts := make([]mount, 0, len(c.Configuration.Mounts))
		for _, m := range c.Configuration.Mounts {
			mounts = append(mounts, mount{src: m.Source, dst: m.Destination})
		}
		applySessionMetadata(&s, c.Configuration.Labels, mounts, c.Configuration.Image.Reference)
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// appleStatus is a container's status in `container ls` JSON. Current
// versions encode an object, {"state", "networks", "startedDate"} (ISO
// 8601); older ones encoded the state as a bare string.
type appleStatus struct {
	State       string     `json:"state"`
	StartedDate *time.Time `json:"startedDate"`
}

func (s *appleStatus) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &s.State)
	}
	type plain appleStatus
	return json.Unmarshal(data, (*plain)(s))
}

type mount struct{ src, dst string }

// applySessionMetadata fills project, workdir, agent, and tmux from labels.
// Containers launched before labels existed are recovered from the
// workspace mount (/workspace/<project>/<dir>) and the image tag
// (ccodolo:<project>-<agent>-<hash>); they predate tmux, so Tmux is false.
func applySessionMetadata(s *Session, labels map[string]string, mounts []mount, image string) {
	if project, ok := labels[LabelProject]; ok {
		s.Project = project
		s.Workdir = labels[LabelWorkdir]
		s.Agent = labels[LabelAgent]
		s.Tmux = labels[LabelTmux] == "1"
		return
	}
	for _, m := range mounts {
		rel, ok := strings.CutPrefix(m.dst, "/workspace/")
		if !ok {
			continue
		}
		if project, _, ok := strings.Cut(rel, "/"); ok {
			s.Project = project
			s.Workdir = m.src
			break
		}
	}
	if s.Project != "" {
		image = path.Base(image)
		if rest, ok := strings.CutPrefix(image, "ccodolo:"+s.Project+"-"); ok {
			if i := strings.LastIndex(rest, "-"); i > 0 {
				s.Agent = rest[:i]
			}
		}
	}
}

// WatchEvents calls fn whenever a ccodolo container starts, stops, or is
// removed, until ctx is cancelled. Docker only: the apple runtime has no
// event stream, so its sessions are re-listed on every stats poll.
func WatchEvents(ctx context.Context, fn func()) error {
	cmd := exec.CommandContext(ctx, "docker", "events",
		"--filter", "type=container",
		"--filter", "event=start", "--filter", "event=die", "--filter", "event=destroy",
		"--format", "{{.Actor.Attributes.name}}")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("watching container events: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), containerNamePrefix) {
			fn()
		}
	}
	return cmd.Wait()
}

// ErrUnavailable means the backend's engine could not be reached: the
// docker daemon or the container system service is not running. The
// dashboard treats that as "no sessions", not as a fault.
var ErrUnavailable = errors.New("container engine not running")

// unavailable classifies a failed listing command. The listing is the
// cheapest call there is, so it failing means the CLI cannot talk to its
// engine (not running, socket gone, service stopped); the CLI's message is
// kept for debugging but callers match on ErrUnavailable.
func unavailable(err error) error {
	return fmt.Errorf("%w: %w", ErrUnavailable, cliError(err))
}

// cliError adds the CLI's stderr to an *exec.ExitError from Output(), so
// "exit status 1" becomes e.g. "Cannot connect to the Docker daemon …".
func cliError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	return err
}
