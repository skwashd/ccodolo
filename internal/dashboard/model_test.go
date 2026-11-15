package dashboard

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/skwashd/ccodolo/internal/docker"
)

type fakeController struct {
	refreshes, pauses, resumes int
}

func (f *fakeController) Refresh() { f.refreshes++ }
func (f *fakeController) Pause()   { f.pauses++ }
func (f *fakeController) Resume()  { f.resumes++ }

// fakeRunner records every non-interactive command.
type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	out   []byte
}

func (f *fakeRunner) run(_ context.Context, rt docker.Runtime, args []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string{rt.Binary()}, args...))
	return f.out, nil
}

var testNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func testModel(t *testing.T) (model, *fakeController, *fakeRunner) {
	t.Helper()
	ctrl, run := &fakeController{}, &fakeRunner{}
	m := newModel(ctrl, run.run)
	m.now = func() time.Time { return testNow }
	m = update(t, m, sessionsMsg{rt: docker.RuntimeDocker, sessions: []docker.Session{
		{ID: "aaa", Project: "alpha", Agent: "claude", Workdir: "/src/alpha", Runtime: docker.RuntimeDocker,
			Running: true, Tmux: true, Started: testNow.Add(-2 * time.Hour)},
		{ID: "bbb", Project: "bravo", Agent: "codex", Workdir: "/src/bravo", Runtime: docker.RuntimeDocker,
			Running: true, Tmux: true, Started: testNow.Add(-5 * time.Minute)},
		{ID: "ccc", Project: "charlie", Agent: "claude", Workdir: "/src/charlie", Runtime: docker.RuntimeDocker,
			Running: false},
	}})
	m = update(t, m, sessionsMsg{rt: docker.RuntimeApple, sessions: []docker.Session{
		{ID: "ccodolo-delta", Project: "delta", Agent: "opencode", Workdir: "/src/delta", Runtime: docker.RuntimeApple,
			Running: true, Tmux: false, Started: testNow.Add(-time.Minute)},
	}})
	return m, ctrl, run
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(model)
}

func press(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(keyMsg(k))
		m = next.(model)
	}
	return m, cmd
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func projects(m model) []string {
	var out []string
	for _, k := range m.visible {
		out = append(out, m.rows[k].Project)
	}
	return out
}

func TestSortAndFilter(t *testing.T) {
	m, _, _ := testModel(t)
	// Running sessions first, then by project.
	if got, want := projects(m), []string{"alpha", "bravo", "delta", "charlie"}; !reflect.DeepEqual(got, want) {
		t.Errorf("default order = %v, want %v", got, want)
	}

	m = update(t, m, statsMsg{rt: docker.RuntimeDocker, id: "bbb", stats: docker.Stats{CPUPct: 90, MemUsed: 10}})
	m = update(t, m, statsMsg{rt: docker.RuntimeApple, id: "ccodolo-delta", stats: docker.Stats{CPUPct: 50, MemUsed: 500}})
	m, _ = press(t, m, "o", "o") // uptime -> cpu
	if got, want := projects(m), []string{"bravo", "delta", "alpha", "charlie"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cpu order = %v, want %v", got, want)
	}
	m, _ = press(t, m, "o") // memory
	if got, want := projects(m), []string{"delta", "bravo", "alpha", "charlie"}; !reflect.DeepEqual(got, want) {
		t.Errorf("memory order = %v, want %v", got, want)
	}
	m, _ = press(t, m, "O")
	if got, want := projects(m), []string{"alpha", "bravo", "delta", "charlie"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reversed memory order = %v, want %v", got, want)
	}

	m, _ = press(t, m, "/", "c", "l", "a", "u", "d", "e", "enter")
	if got, want := projects(m), []string{"alpha", "charlie"}; !reflect.DeepEqual(got, want) {
		t.Errorf("filtered = %v, want %v", got, want)
	}
	m, _ = press(t, m, "esc")
	if len(m.visible) != 4 {
		t.Errorf("esc should clear the filter, got %v", projects(m))
	}
}

func TestSessionsMsgKeepsCursorAndDropsGone(t *testing.T) {
	m, _, _ := testModel(t)
	m, _ = press(t, m, "down") // bravo
	m = update(t, m, sessionsMsg{rt: docker.RuntimeDocker, sessions: []docker.Session{
		{ID: "bbb", Project: "bravo", Runtime: docker.RuntimeDocker, Running: true, Tmux: true},
	}})
	if got := m.current().Project; got != "bravo" {
		t.Errorf("cursor moved to %q, want bravo", got)
	}
	if got, want := projects(m), []string{"bravo", "delta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v (apple rows untouched by a docker refresh)", got, want)
	}
}

func TestStopConfirmFlow(t *testing.T) {
	m, _, run := testModel(t)

	m, _ = press(t, m, "x", "n")
	if m.mode != modeTable || len(run.calls) != 0 {
		t.Fatalf("declining should run nothing (mode %v, calls %v)", m.mode, run.calls)
	}

	// Mark alpha and delta (different runtimes): one stop per runtime.
	m, _ = press(t, m, "space", "down", "space")
	m, cmd := press(t, m, "x")
	if m.mode != modeConfirm || !strings.Contains(m.confirmMsg, "alpha, delta") {
		t.Fatalf("expected a confirmation naming both, got mode %v %q", m.mode, m.confirmMsg)
	}
	if cmd != nil {
		t.Fatal("nothing should run before confirming")
	}
	_, cmd = press(t, m, "y")
	runBatch(t, cmd)
	slices.SortFunc(run.calls, func(a, b []string) int { return strings.Compare(a[0], b[0]) })
	want := [][]string{{"container", "stop", "ccodolo-delta"}, {"docker", "stop", "aaa"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Errorf("calls = %q, want %q", run.calls, want)
	}
}

func TestCleanupRemovesStopped(t *testing.T) {
	m, _, run := testModel(t)
	m, _ = press(t, m, "c")
	if !strings.Contains(m.confirmMsg, "1 stopped") {
		t.Fatalf("confirm = %q", m.confirmMsg)
	}
	_, cmd := press(t, m, "y")
	runBatch(t, cmd)
	if want := [][]string{{"docker", "rm", "ccc"}}; !reflect.DeepEqual(run.calls, want) {
		t.Errorf("calls = %q, want %q", run.calls, want)
	}
}

func TestAttachPausesStats(t *testing.T) {
	m, ctrl, _ := testModel(t)
	m, cmd := press(t, m, "enter")
	if cmd == nil || ctrl.pauses != 1 {
		t.Fatalf("attach should pause stats and exec (pauses %d, cmd %v)", ctrl.pauses, cmd != nil)
	}
	update(t, m, execDoneMsg{})
	if ctrl.resumes != 1 {
		t.Errorf("returning from attach should resume stats, resumes = %d", ctrl.resumes)
	}

	// delta has no tmux: attach explains instead of exec'ing.
	m, _ = press(t, m, "down", "down")
	m, cmd = press(t, m, "enter")
	if cmd != nil || ctrl.pauses != 1 || !strings.Contains(m.status, "no tmux") {
		t.Errorf("attach without tmux: pauses %d, status %q", ctrl.pauses, m.status)
	}
}

func TestBackgroundTask(t *testing.T) {
	m, ctrl, run := testModel(t)
	m = update(t, m, windowsMsg{key: rowKey(docker.RuntimeDocker, "aaa"), windows: []docker.TmuxWindow{
		{Session: "agent", Window: "claude"},
		{Session: "tasks", Window: "task-old", Dead: true},
		{Session: "shell-1", Window: "zsh"},
	}})
	keys := append([]string{"b"}, strings.Split("make test", "")...)
	m, _ = press(t, m, keys...)
	m, cmd := press(t, m, "enter")
	msg := cmd()
	want := append([]string{"docker"}, docker.TaskArgs("aaa", "task-20261101-120000", "make test", true)...)
	if !reflect.DeepEqual(run.calls, [][]string{want}) {
		t.Errorf("calls = %q, want %q", run.calls, want)
	}
	m = update(t, m, msg)
	if !strings.Contains(m.status, "task-20261101-120000") || ctrl.refreshes != 1 {
		t.Errorf("status %q, refreshes %d", m.status, ctrl.refreshes)
	}
	if got := m.nextShellName(m.current()); got != "shell-2" {
		t.Errorf("nextShellName = %q, want shell-2", got)
	}
}

func TestViewerSearch(t *testing.T) {
	m, _, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 10})
	var lines []string
	for i := range 40 {
		lines = append(lines, "line "+string(rune('a'+i%26)))
	}
	m = update(t, m, viewerMsg{title: "Agent", content: "\x1b[1m" + strings.Join(lines, "\n") + "\x1b[0m"})
	if m.mode != modeViewer {
		t.Fatalf("mode = %v", m.mode)
	}
	m, _ = press(t, m, "/", "l", "i", "n", "e", " ", "c", "enter")
	if want := []int{2, 28}; !reflect.DeepEqual(m.matches, want) {
		t.Fatalf("matches = %v, want %v", m.matches, want)
	}
	if m.matchCursor != 1 {
		t.Errorf("search should start at the last match, got %d", m.matchCursor)
	}
	m, _ = press(t, m, "n")
	if m.matchCursor != 0 || m.viewer.YOffset() != 2 {
		t.Errorf("n: cursor %d offset %d", m.matchCursor, m.viewer.YOffset())
	}
	m, _ = press(t, m, "q")
	if m.mode != modeTable {
		t.Errorf("q should close the viewer")
	}
}

func TestViewRenders(t *testing.T) {
	m, _, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 160, Height: 30})
	m = update(t, m, statsMsg{rt: docker.RuntimeDocker, id: "aaa", stats: docker.Stats{
		CPUPct: 12.34, MemUsed: 512 << 20, MemLimit: 4 << 30, PIDs: 7}})
	out := m.View().Content
	for _, want := range []string{"SESSION", "WORKDIR", "alpha", "2h00m", "12.3", "512.0M / 4.0G", "12.5", "stopped", "/src/alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}

	// Narrow terminals drop trailing columns rather than wrapping.
	m = update(t, m, tea.WindowSizeMsg{Width: 70, Height: 30})
	for _, line := range strings.Split(m.View().Content, "\n") {
		if w := len([]rune(stripANSI(line))); w > 70 {
			t.Errorf("line wider than terminal (%d): %q", w, line)
		}
	}
}

func TestFormatting(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{42 * time.Second, "0m42s"},
		{65 * time.Minute, "1h05m"},
		{50 * time.Hour, "2d02h"},
	} {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	for _, tc := range []struct {
		n    uint64
		want string
	}{
		{512, "512B"}, {1536, "1.5K"}, {3 << 30, "3.0G"},
	} {
		if got := formatBytes(tc.n); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
	if got := truncateLeft("/home/dave/src/project", 10); got != "…c/project" {
		t.Errorf("truncateLeft = %q", got)
	}
}

// runBatch runs cmd and, for a batch, each command in it.
func runBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	}
}

func stripANSI(s string) string { return ansi.Strip(s) }

func TestEngineNotRunningIsSilent(t *testing.T) {
	m, ctrl, _ := testModel(t)
	m = update(t, m, sourceErrMsg{rt: docker.RuntimeDocker, err: errors.New("stats stream died")})
	if ctrl.refreshes != 1 {
		t.Errorf("a stats failure should trigger a re-list, refreshes = %d", ctrl.refreshes)
	}

	down := fmt.Errorf("%w: failed to connect to the docker API", docker.ErrUnavailable)
	m = update(t, m, sessionsMsg{rt: docker.RuntimeDocker, err: down})
	if len(m.errs) != 0 {
		t.Errorf("an engine that is not running is not an error, got %v", m.errs)
	}
	if got, want := projects(m), []string{"delta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want only the apple session", got)
	}
	m = update(t, m, sourceErrMsg{rt: docker.RuntimeDocker, err: errors.New("late stats error")})
	if len(m.errs) != 0 {
		t.Errorf("errors from an engine known to be down are dropped, got %v", m.errs)
	}
	if strings.Contains(m.View().Content, "docker:") {
		t.Errorf("view should not mention docker:\n%s", m.View().Content)
	}

	// Other failures from a running engine are still shown.
	m = update(t, m, sessionsMsg{rt: docker.RuntimeDocker, err: errors.New("parsing container inspect output: bad")})
	if m.errs[docker.RuntimeDocker] == "" {
		t.Error("expected a real failure to be reported")
	}
}

func TestTasksShowCommand(t *testing.T) {
	m, _, _ := testModel(t)
	m = update(t, m, tea.WindowSizeMsg{Width: 160, Height: 40})
	m = update(t, m, windowsMsg{key: rowKey(docker.RuntimeDocker, "aaa"), windows: []docker.TmuxWindow{
		{Session: "agent", Window: "claude"},
		{Session: "tasks", Window: "task-20261101-120000", Command: "go test ./..."},
		{Session: "tasks", Window: "task-20261101-110000", Dead: true, Status: 1, Command: "make lint"},
		{Session: "tasks", Window: "task-old"}, // started before commands were recorded
	}})

	details := stripANSI(m.View().Content)
	for _, want := range []string{"go test ./... (running)", "make lint (exit 1)", "task-old (running)"} {
		if !strings.Contains(details, want) {
			t.Errorf("details pane missing %q:\n%s", want, details)
		}
	}

	m, _ = press(t, m, "t")
	picker := stripANSI(m.View().Content)
	for _, want := range []string{"running  go test ./...  task-20261101-120000", "exit 1   make lint  task-20261101-110000", "running  task-old"} {
		if !strings.Contains(picker, want) {
			t.Errorf("task list missing %q:\n%s", want, picker)
		}
	}
}
