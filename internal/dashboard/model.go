package dashboard

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/skwashd/ccodolo/internal/docker"
)

// row is one session plus its latest stats sample.
type row struct {
	docker.Session
	stats    docker.Stats
	hasStats bool
}

func rowKey(rt docker.Runtime, id string) string { return string(rt) + "/" + id }

type mode int

const (
	modeTable mode = iota
	modeFilter
	modePrompt  // background command input
	modeConfirm // y/n
	modePicker  // shells or tasks list
	modeViewer  // review / task output
	modeHelp
)

type sortColumn int

const (
	sortSession sortColumn = iota
	sortUptime
	sortCPU
	sortMem
	numSortColumns
)

func (c sortColumn) String() string {
	return [...]string{"session", "uptime", "cpu", "memory"}[c]
}

// pickerKind says what a modePicker list holds.
type pickerKind int

const (
	pickShells pickerKind = iota
	pickTasks
)

// Messages produced by the model's own commands.
type (
	tickMsg    time.Time
	windowsMsg struct {
		key     string
		windows []docker.TmuxWindow
	}
	actionDoneMsg struct {
		status string
		err    error
	}
	execDoneMsg struct{ err error }
	viewerMsg   struct {
		title   string
		content string
		err     error
	}
)

// runner executes non-interactive runtime commands; tests swap it.
type runner func(ctx context.Context, rt docker.Runtime, args []string) ([]byte, error)

func runCommand(ctx context.Context, rt docker.Runtime, args []string) ([]byte, error) {
	out, err := docker.Command(ctx, rt, args).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return out, fmt.Errorf("%s", msg)
		}
	}
	return out, err
}

// actionTimeout bounds every non-interactive command the dashboard runs.
const actionTimeout = 30 * time.Second

type model struct {
	src controller
	run runner
	now func() time.Time

	rows     map[string]*row
	visible  []string // keys, sorted and filtered
	cursor   int
	marked   map[string]bool
	sortBy   sortColumn
	sortDesc bool
	filter   string
	errs     map[docker.Runtime]string
	down     map[docker.Runtime]bool // engine not running

	mode       mode
	input      textinput.Model
	confirmMsg string
	onConfirm  tea.Cmd

	windows    map[string][]docker.TmuxWindow // per row key
	pickKind   pickerKind
	pickItems  []docker.TmuxWindow
	pickCursor int

	viewer       viewport.Model
	viewerTitle  string
	viewerPlain  []string // ANSI-stripped lines, for search
	searchQuery  string
	searching    bool
	matches      []int
	matchCursor  int
	status       string
	statusExpiry time.Time

	width, height int
}

func newModel(src controller, run runner) model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 4096
	return model{
		src:     src,
		run:     run,
		now:     time.Now,
		rows:    make(map[string]*row),
		marked:  make(map[string]bool),
		errs:    make(map[docker.Runtime]string),
		down:    make(map[docker.Runtime]bool),
		windows: make(map[string][]docker.TmuxWindow),
		input:   ti,
		viewer:  viewport.New(),
		width:   120,
		height:  30,
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd { return tick() }

// current returns the row under the cursor.
func (m model) current() *row {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.rows[m.visible[m.cursor]]
}

func (m *model) setStatus(s string) {
	m.status = s
	m.statusExpiry = m.now().Add(5 * time.Second)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.viewer.SetWidth(m.width)
		m.viewer.SetHeight(max(m.height-2, 1))
		m.input.SetWidth(max(m.width-4, 10))
		return m, nil

	case tickMsg:
		if !m.statusExpiry.IsZero() && m.now().After(m.statusExpiry) {
			m.status = ""
			m.statusExpiry = time.Time{}
		}
		return m, tea.Batch(tick(), m.pollTasks())

	case sessionsMsg:
		return m.applySessions(msg)

	case statsMsg:
		if r, ok := m.rows[rowKey(msg.rt, msg.id)]; ok {
			r.stats, r.hasStats = msg.stats, true
			if m.sortBy == sortCPU || m.sortBy == sortMem {
				m.resort()
			}
		}
		return m, nil

	case sourceErrMsg:
		// A stats failure usually means the engine just went away; the
		// re-list tells, and clears this if so.
		if !m.down[msg.rt] {
			m.errs[msg.rt] = msg.err.Error()
		}
		m.src.Refresh()
		return m, nil

	case windowsMsg:
		m.windows[msg.key] = msg.windows
		if m.mode == modePicker {
			m.loadPicker()
		}
		return m, nil

	case actionDoneMsg:
		if msg.err != nil {
			m.setStatus("Error: " + msg.err.Error())
		} else if msg.status != "" {
			m.setStatus(msg.status)
		}
		m.src.Refresh()
		return m, m.loadWindows()

	case execDoneMsg:
		m.src.Resume()
		if msg.err != nil {
			m.setStatus("Error: " + msg.err.Error())
		}
		return m, m.loadWindows()

	case viewerMsg:
		if msg.err != nil {
			m.setStatus("Error: " + msg.err.Error())
			return m, nil
		}
		m.openViewer(msg.title, msg.content)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	if m.mode == modeViewer {
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) applySessions(msg sessionsMsg) (tea.Model, tea.Cmd) {
	delete(m.down, msg.rt)
	if errors.Is(msg.err, docker.ErrUnavailable) {
		// The engine is not running: it simply has no sessions. Not an
		// error worth showing; its rows go and the feed keeps checking.
		m.down[msg.rt] = true
		msg.sessions, msg.err = nil, nil
	}
	if msg.err != nil {
		m.errs[msg.rt] = msg.err.Error()
		return m, nil
	}
	delete(m.errs, msg.rt)

	prev := ""
	if r := m.current(); r != nil {
		prev = rowKey(r.Runtime, r.ID)
	}
	seen := make(map[string]bool, len(msg.sessions))
	for _, s := range msg.sessions {
		key := rowKey(msg.rt, s.ID)
		seen[key] = true
		if r, ok := m.rows[key]; ok {
			r.Session = s
			if !s.Running {
				r.hasStats = false
			}
		} else {
			m.rows[key] = &row{Session: s}
		}
	}
	for key, r := range m.rows {
		if r.Runtime == msg.rt && !seen[key] {
			delete(m.rows, key)
			delete(m.marked, key)
			delete(m.windows, key)
		}
	}
	m.resort()
	m.cursor = max(slices.Index(m.visible, prev), 0)
	if prev == "" || m.current() == nil || rowKey(m.current().Runtime, m.current().ID) != prev {
		return m, m.loadWindows()
	}
	return m, nil
}

// resort recomputes the visible rows, keeping the cursor on the same row.
func (m *model) resort() {
	prev := ""
	if r := m.current(); r != nil {
		prev = rowKey(r.Runtime, r.ID)
	}
	m.visible = m.visible[:0]
	filter := strings.ToLower(m.filter)
	for key, r := range m.rows {
		if filter != "" && !strings.Contains(strings.ToLower(r.Project+" "+r.Agent+" "+r.Workdir+" "+r.Name), filter) {
			continue
		}
		m.visible = append(m.visible, key)
	}
	slices.SortFunc(m.visible, func(a, b string) int {
		ra, rb := m.rows[a], m.rows[b]
		// Running sessions first, whatever the sort column.
		if ra.Running != rb.Running {
			if ra.Running {
				return -1
			}
			return 1
		}
		var c int
		switch m.sortBy {
		case sortUptime:
			// Longest-running first.
			c = ra.Started.Compare(rb.Started)
		case sortCPU:
			c = -cmp.Compare(ra.stats.CPUPct, rb.stats.CPUPct)
		case sortMem:
			c = -cmp.Compare(ra.stats.MemUsed, rb.stats.MemUsed)
		default:
			c = cmp.Compare(ra.Project, rb.Project)
		}
		if m.sortDesc {
			c = -c
		}
		if c == 0 {
			c = cmp.Compare(a, b)
		}
		return c
	})
	if i := slices.Index(m.visible, prev); i >= 0 {
		m.cursor = i
	}
	m.cursor = min(m.cursor, max(len(m.visible)-1, 0))
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeFilter:
		return m.handleFilterKey(msg)
	case modePrompt:
		return m.handlePromptKey(msg)
	case modeConfirm:
		m.mode = modeTable
		if key == "y" || key == "Y" {
			cmd := m.onConfirm
			m.onConfirm = nil
			return m, cmd
		}
		m.setStatus("Cancelled")
		return m, nil
	case modePicker:
		return m.handlePickerKey(key)
	case modeViewer:
		return m.handleViewerKey(msg)
	case modeHelp:
		m.mode = modeTable
		return m, nil
	}
	return m.handleTableKey(key)
}

func (m model) handleTableKey(key string) (tea.Model, tea.Cmd) {
	r := m.current()
	switch key {
	case "q", "esc":
		if key == "esc" && m.filter != "" {
			m.filter = ""
			m.resort()
			return m, nil
		}
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			return m, m.loadWindows()
		}
	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
			return m, m.loadWindows()
		}
	case "home", "g":
		m.cursor = 0
		return m, m.loadWindows()
	case "end", "G":
		m.cursor = max(len(m.visible)-1, 0)
		return m, m.loadWindows()
	case "o":
		m.sortBy = (m.sortBy + 1) % numSortColumns
		m.resort()
		m.setStatus("Sorted by " + m.sortBy.String())
	case "O":
		m.sortDesc = !m.sortDesc
		m.resort()
	case "/":
		m.mode = modeFilter
		m.input.SetValue(m.filter)
		m.input.Placeholder = "filter by project, agent, or path"
		return m, m.input.Focus()
	case "space":
		if r != nil {
			k := rowKey(r.Runtime, r.ID)
			if m.marked[k] {
				delete(m.marked, k)
			} else {
				m.marked[k] = true
			}
			if m.cursor < len(m.visible)-1 {
				m.cursor++
			}
		}
	case "enter", "a":
		if r == nil || !r.Running {
			return m, nil
		}
		if !r.Tmux {
			m.setStatus("This session has no tmux (opted out or an older image): press s for a shell")
			return m, nil
		}
		return m, m.interactive(r, docker.AttachArgs(r.ID, docker.AgentSession))
	case "s":
		if r == nil || !r.Running {
			return m, nil
		}
		if !r.Tmux {
			return m, m.interactive(r, docker.PlainShellArgs(r.ID))
		}
		return m, m.interactive(r, docker.NewShellArgs(r.ID, m.nextShellName(r)))
	case "S", "t":
		if r == nil || !r.Running || !r.Tmux {
			return m, nil
		}
		m.mode = modePicker
		m.pickKind = pickShells
		if key == "t" {
			m.pickKind = pickTasks
		}
		m.pickCursor = 0
		m.loadPicker()
		return m, m.loadWindows()
	case "b":
		if r == nil || !r.Running || !r.Tmux {
			return m, nil
		}
		m.mode = modePrompt
		m.input.SetValue("")
		m.input.Placeholder = "command to run in the background"
		return m, m.input.Focus()
	case "r":
		if r == nil || !r.Running || !r.Tmux {
			return m, nil
		}
		return m, m.capture(r, docker.AgentSession, "Agent session: "+r.Project)
	case "x":
		targets := m.stopTargets()
		if len(targets) == 0 {
			return m, nil
		}
		names := make([]string, len(targets))
		for i, t := range targets {
			names[i] = t.Project
		}
		m.confirm(fmt.Sprintf("Stop %s? The agent and everything in the container end. [y/N]", strings.Join(names, ", ")),
			m.stop(targets))
	case "c":
		var stale []*row
		for _, k := range m.visible {
			if r := m.rows[k]; !r.Running {
				stale = append(stale, r)
			}
		}
		if len(stale) == 0 {
			m.setStatus("No stopped ccodolo containers")
			return m, nil
		}
		m.confirm(fmt.Sprintf("Remove %d stopped ccodolo container(s)? [y/N]", len(stale)), m.remove(stale))
	}
	return m, nil
}

func (m *model) confirm(prompt string, cmd tea.Cmd) {
	m.mode = modeConfirm
	m.confirmMsg = prompt
	m.onConfirm = cmd
}

// stopTargets is the marked running sessions, or the current one.
func (m model) stopTargets() []*row {
	var targets []*row
	for _, k := range m.visible {
		if r := m.rows[k]; m.marked[k] && r.Running {
			targets = append(targets, r)
		}
	}
	if len(targets) == 0 {
		if r := m.current(); r != nil && r.Running {
			targets = append(targets, r)
		}
	}
	return targets
}

func (m model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeTable
		m.filter = ""
		m.input.Blur()
		m.resort()
		return m, nil
	case "enter":
		m.mode = modeTable
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filter = m.input.Value()
	m.resort()
	return m, cmd
}

func (m model) handlePromptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeTable
		m.input.Blur()
		return m, nil
	case "enter":
		m.mode = modeTable
		m.input.Blur()
		command := strings.TrimSpace(m.input.Value())
		r := m.current()
		if command == "" || r == nil {
			return m, nil
		}
		return m, m.startTask(r, command)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handlePickerKey(key string) (tea.Model, tea.Cmd) {
	r := m.current()
	var item *docker.TmuxWindow
	if m.pickCursor >= 0 && m.pickCursor < len(m.pickItems) {
		item = &m.pickItems[m.pickCursor]
	}
	switch key {
	case "esc", "q":
		m.mode = modeTable
	case "up", "k":
		m.pickCursor = max(m.pickCursor-1, 0)
	case "down", "j":
		m.pickCursor = min(m.pickCursor+1, max(len(m.pickItems)-1, 0))
	case "enter":
		if item == nil || r == nil {
			return m, nil
		}
		if m.pickKind == pickShells {
			m.mode = modeTable
			return m, m.interactive(r, docker.AttachArgs(r.ID, item.Session))
		}
		return m, m.capture(r, docker.TasksSession+":"+item.Window, "Task: "+taskLabel(*item))
	case "a":
		if item == nil || r == nil {
			return m, nil
		}
		m.mode = modeTable
		return m, m.interactive(r, docker.AttachArgs(r.ID, item.Session+":"+item.Window))
	case "d":
		if item == nil || r == nil || m.pickKind != pickTasks || !item.Dead {
			return m, nil
		}
		return m, m.action(r.Runtime, docker.KillWindowArgs(r.ID, item.Session, item.Window), "Dismissed "+item.Window)
	}
	return m, nil
}

func (m model) handleViewerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.searching {
		switch key {
		case "esc":
			m.searching = false
			m.input.Blur()
			return m, nil
		case "enter":
			m.searching = false
			m.input.Blur()
			m.search(m.input.Value())
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	switch key {
	case "q", "esc":
		m.mode = modeTable
		return m, nil
	case "/":
		m.searching = true
		m.input.SetValue(m.searchQuery)
		m.input.Placeholder = "search"
		return m, m.input.Focus()
	case "n", "N":
		if len(m.matches) > 0 {
			step := 1
			if key == "N" {
				step = len(m.matches) - 1
			}
			m.matchCursor = (m.matchCursor + step) % len(m.matches)
			m.viewer.SetYOffset(m.matches[m.matchCursor])
		}
		return m, nil
	case "g", "home":
		m.viewer.GotoTop()
		return m, nil
	case "G", "end":
		m.viewer.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return m, cmd
}

func (m *model) openViewer(title, content string) {
	m.mode = modeViewer
	m.viewerTitle = title
	content = strings.TrimRight(content, "\n")
	m.viewer.SetContent(content)
	m.viewerPlain = strings.Split(ansi.Strip(content), "\n")
	m.matches, m.matchCursor = nil, 0
	if m.searchQuery != "" {
		m.search(m.searchQuery)
	}
	if len(m.matches) == 0 {
		m.viewer.GotoBottom()
	}
}

func (m *model) search(q string) {
	m.searchQuery = q
	m.matches, m.matchCursor = nil, 0
	if q == "" {
		return
	}
	q = strings.ToLower(q)
	for i, line := range m.viewerPlain {
		if strings.Contains(strings.ToLower(line), q) {
			m.matches = append(m.matches, i)
		}
	}
	if len(m.matches) == 0 {
		m.setStatus("No matches for " + strconv.Quote(m.searchQuery))
		return
	}
	// Start from the most recent match: the end of a session is usually
	// what is being looked for.
	m.matchCursor = len(m.matches) - 1
	m.viewer.SetYOffset(m.matches[m.matchCursor])
}

func (m *model) loadPicker() {
	m.pickItems = m.pickItems[:0]
	r := m.current()
	if r == nil {
		return
	}
	for _, w := range m.windows[rowKey(r.Runtime, r.ID)] {
		isTask := w.Session == docker.TasksSession
		if (m.pickKind == pickTasks) == isTask && (isTask || strings.HasPrefix(w.Session, "shell-")) {
			m.pickItems = append(m.pickItems, w)
		}
	}
	m.pickCursor = min(m.pickCursor, max(len(m.pickItems)-1, 0))
}

func (m model) nextShellName(r *row) string {
	used := make(map[string]bool)
	for _, w := range m.windows[rowKey(r.Runtime, r.ID)] {
		used[w.Session] = true
	}
	for n := 1; ; n++ {
		if name := "shell-" + strconv.Itoa(n); !used[name] {
			return name
		}
	}
}

// Commands.

// interactive hands the terminal to an exec session. Stats collection
// pauses meanwhile: nothing is drawn until it returns.
func (m model) interactive(r *row, args []string) tea.Cmd {
	m.src.Pause()
	cmd := docker.Command(context.Background(), r.Runtime, args)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		// A detach (Ctrl+]) exits 0; an exit status from the shell itself
		// is not worth reporting.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			err = nil
		}
		return execDoneMsg{err: err}
	})
}

// loadWindows lists the current session's tmux windows, for the details
// pane and pickers. Asked on selection change and after actions, never
// per tick.
func (m model) loadWindows() tea.Cmd {
	r := m.current()
	if r == nil || !r.Running || !r.Tmux {
		return nil
	}
	key, rt, id, run := rowKey(r.Runtime, r.ID), r.Runtime, r.ID, m.run
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		out, err := run(ctx, rt, docker.ListWindowsArgs(id))
		if err != nil {
			// No tmux server yet is normal for an idle container.
			return windowsMsg{key: key}
		}
		return windowsMsg{key: key, windows: docker.ParseWindows(out)}
	}
}

// pollTasksEvery is how often the selected session's windows are re-read
// while it has a background command still running, so its status updates.
const pollTasksEvery = 5 * time.Second

func (m model) pollTasks() tea.Cmd {
	r := m.current()
	if r == nil || m.now().Unix()%int64(pollTasksEvery/time.Second) != 0 {
		return nil
	}
	for _, w := range m.windows[rowKey(r.Runtime, r.ID)] {
		if w.Session == docker.TasksSession && !w.Dead {
			return m.loadWindows()
		}
	}
	return nil
}

func (m model) action(rt docker.Runtime, args []string, okStatus string) tea.Cmd {
	run := m.run
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		_, err := run(ctx, rt, args)
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{status: okStatus}
	}
}

func (m model) startTask(r *row, command string) tea.Cmd {
	name := "task-" + m.now().Format("20060102-150405")
	exists := false
	for _, w := range m.windows[rowKey(r.Runtime, r.ID)] {
		if w.Session == docker.TasksSession {
			exists = true
			break
		}
	}
	status := fmt.Sprintf("Started %s; output in %s", name, taskLogHint(r.Project, name))
	return m.action(r.Runtime, docker.TaskArgs(r.ID, name, command, exists), status)
}

func (m model) capture(r *row, target, title string) tea.Cmd {
	rt, id, run := r.Runtime, r.ID, m.run
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		out, err := run(ctx, rt, docker.CaptureArgs(id, target))
		return viewerMsg{title: title, content: string(out), err: err}
	}
}

// stop and remove group targets per runtime: one CLI call each.
func (m model) stop(targets []*row) tea.Cmd {
	return m.perRuntime(targets, func(rt docker.Runtime, ids []string) []string { return docker.StopArgs(ids...) }, "Stopped")
}

func (m model) remove(targets []*row) tea.Cmd {
	return m.perRuntime(targets, func(rt docker.Runtime, ids []string) []string { return rt.RemoveArgs(ids...) }, "Removed")
}

func (m model) perRuntime(targets []*row, argv func(docker.Runtime, []string) []string, verb string) tea.Cmd {
	byRT := make(map[docker.Runtime][]string)
	for _, t := range targets {
		byRT[t.Runtime] = append(byRT[t.Runtime], t.ID)
	}
	for _, t := range targets {
		delete(m.marked, rowKey(t.Runtime, t.ID))
	}
	var cmds []tea.Cmd
	for rt, ids := range byRT {
		cmds = append(cmds, m.action(rt, argv(rt, ids), fmt.Sprintf("%s %d session(s)", verb, len(ids))))
	}
	return tea.Batch(cmds...)
}
