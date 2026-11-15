package dashboard

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/docker"
)

var (
	headerStyle   = lipgloss.NewStyle().Bold(true)
	cursorStyle   = lipgloss.NewStyle().Reverse(true)
	stoppedStyle  = lipgloss.NewStyle().Faint(true)
	faintStyle    = lipgloss.NewStyle().Faint(true)
	titleStyle    = lipgloss.NewStyle().Bold(true).Underline(true)
	errorStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true)
)

// column is one fixed-width table column; the workdir column takes what
// is left.
type column struct {
	title string
	width int
	right bool
	value func(m model, r *row) string
}

var columns = []column{
	{"SESSION", 18, false, func(_ model, r *row) string { return r.Project }},
	{"AGENT", 11, false, func(_ model, r *row) string { return orDash(r.Agent) }},
	{"RT", 6, false, func(_ model, r *row) string { return string(r.Runtime) }},
	{"UPTIME", 8, true, func(m model, r *row) string {
		if !r.Running {
			return "stopped"
		}
		if r.Started.IsZero() {
			return "-"
		}
		return formatDuration(m.now().Sub(r.Started))
	}},
	{"CPU%", 7, true, func(_ model, r *row) string {
		if !r.hasStats {
			return "-"
		}
		return fmt.Sprintf("%.1f", r.stats.CPUPct)
	}},
	{"MEM", 15, true, func(_ model, r *row) string {
		if !r.hasStats {
			return "-"
		}
		return formatBytes(r.stats.MemUsed) + " / " + formatBytes(r.stats.MemLimit)
	}},
	{"MEM%", 6, true, func(_ model, r *row) string {
		if !r.hasStats || r.stats.MemLimit == 0 {
			return "-"
		}
		return fmt.Sprintf("%.1f", float64(r.stats.MemUsed)*100/float64(r.stats.MemLimit))
	}},
	{"NET I/O", 15, true, func(_ model, r *row) string {
		if !r.hasStats {
			return "-"
		}
		return formatBytes(r.stats.NetRx) + " / " + formatBytes(r.stats.NetTx)
	}},
	{"BLOCK I/O", 15, true, func(_ model, r *row) string {
		if !r.hasStats {
			return "-"
		}
		return formatBytes(r.stats.BlkRead) + " / " + formatBytes(r.stats.BlkWrite)
	}},
	{"PIDS", 5, true, func(_ model, r *row) string {
		if !r.hasStats {
			return "-"
		}
		return fmt.Sprint(r.stats.PIDs)
	}},
}

const minWorkdirWidth = 12

func (m model) View() tea.View {
	var content string
	switch m.mode {
	case modeViewer:
		content = m.viewViewer()
	case modeHelp:
		content = helpText
	default:
		content = m.viewTable()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m model) viewTable() string {
	var b strings.Builder

	// Drop trailing columns that do not fit, rather than wrapping.
	cols := columns
	for len(cols) > 4 && tableWidth(cols)+minWorkdirWidth > m.width {
		cols = cols[:len(cols)-1]
	}
	workdirWidth := max(m.width-tableWidth(cols), minWorkdirWidth)

	header := "  "
	for _, c := range cols {
		header += pad(c.title, c.width, c.right) + " "
	}
	header += pad("WORKDIR", workdirWidth, false)
	b.WriteString(headerStyle.Render(ansi.Truncate(header, m.width, "")) + "\n")

	// Leave room for the header, details pane, and footer.
	tableHeight := max(m.height-12, 3)
	start := 0
	if m.cursor >= tableHeight {
		start = m.cursor - tableHeight + 1
	}
	if len(m.visible) == 0 {
		b.WriteString(faintStyle.Render("  No ccodolo sessions running.") + "\n")
	}
	for i := start; i < len(m.visible) && i < start+tableHeight; i++ {
		key := m.visible[i]
		r := m.rows[key]
		mark := "  "
		if m.marked[key] {
			mark = "● "
		}
		line := mark
		for _, c := range cols {
			line += pad(c.value(m, r), c.width, c.right) + " "
		}
		line += truncateLeft(r.Workdir, workdirWidth)
		line = pad(ansi.Truncate(line, m.width, "…"), m.width, false)
		switch {
		case i == m.cursor:
			line = cursorStyle.Render(line)
		case !r.Running:
			line = stoppedStyle.Render(line)
		case m.marked[key]:
			line = selectedStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	for _, line := range strings.SplitAfter(m.viewDetails(), "\n") {
		b.WriteString(ansi.Truncate(line, m.width, "…"))
	}
	b.WriteString(m.viewFooter())
	return b.String()
}

func tableWidth(cols []column) int {
	w := 2 // mark
	for _, c := range cols {
		w += c.width + 1
	}
	return w
}

func (m model) viewDetails() string {
	if m.mode == modePicker {
		return m.viewPicker()
	}
	r := m.current()
	if r == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(r.Project) + "  " + faintStyle.Render(r.Name+" ("+r.ID+")") + "\n")
	b.WriteString("Workdir: " + r.Workdir + "\n")
	if !r.Tmux {
		b.WriteString(faintStyle.Render("No tmux: attach, background commands, and review are unavailable. s opens a shell.") + "\n")
		return b.String()
	}
	var shells, tasks []string
	for _, w := range m.windows[rowKey(r.Runtime, r.ID)] {
		switch {
		case w.Session == docker.TasksSession:
			state := "running"
			if w.Dead {
				state = fmt.Sprintf("exit %d", w.Status)
			}
			tasks = append(tasks, taskLabel(w)+" ("+state+")")
		case strings.HasPrefix(w.Session, "shell-"):
			shells = append(shells, w.Session)
		}
	}
	slices.Sort(shells)
	b.WriteString("Shells: " + orDash(strings.Join(slices.Compact(shells), ", ")) + "\n")
	b.WriteString("Tasks:  " + orDash(strings.Join(tasks, ", ")) + "\n")
	return b.String()
}

func (m model) viewPicker() string {
	var b strings.Builder
	title, help := "Shells", "enter attach · esc back"
	if m.pickKind == pickTasks {
		title, help = "Background commands", "enter view output · a attach · d dismiss finished · esc back"
	}
	b.WriteString(titleStyle.Render(title) + "  " + faintStyle.Render(help) + "\n")
	if len(m.pickItems) == 0 {
		b.WriteString(faintStyle.Render("  none") + "\n")
	}
	for i, w := range m.pickItems {
		label := w.Session
		if m.pickKind == pickTasks {
			state := "running"
			if w.Dead {
				state = fmt.Sprintf("exit %d", w.Status)
			}
			label = pad(state, 8, false) + " " + taskLabel(w)
			if w.Command != "" {
				label += "  " + faintStyle.Render(w.Window)
			}
		}
		line := "  " + label
		if i == m.pickCursor {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m model) viewFooter() string {
	var b strings.Builder
	b.WriteString("\n")
	for _, rt := range []docker.Runtime{docker.RuntimeDocker, docker.RuntimeApple} {
		if e, ok := m.errs[rt]; ok {
			b.WriteString(errorStyle.Render(ansi.Truncate(string(rt)+": "+firstLine(e), m.width, "…")) + "\n")
		}
	}
	switch m.mode {
	case modeFilter, modePrompt:
		b.WriteString(m.input.View() + "\n")
	case modeConfirm:
		b.WriteString(errorStyle.Render(m.confirmMsg) + "\n")
	default:
		if m.status != "" {
			b.WriteString(ansi.Truncate(m.status, m.width, "…") + "\n")
		} else if m.filter != "" {
			b.WriteString(faintStyle.Render("filter: "+m.filter+" (esc clears)") + "\n")
		}
	}
	// Help and quit first, so they survive truncation on narrow terminals.
	b.WriteString(faintStyle.Render(ansi.Truncate("? help · q quit · enter attach · s shell · S shells · b background · t tasks · r review · x stop · space mark · c cleanup · o sort · / filter", m.width, "…")))
	return b.String()
}

func (m model) viewViewer() string {
	header := titleStyle.Render(m.viewerTitle)
	if len(m.matches) > 0 {
		header += faintStyle.Render(fmt.Sprintf("  match %d/%d", m.matchCursor+1, len(m.matches)))
	}
	footer := faintStyle.Render("↑/↓ pgup/pgdn scroll · g/G top/bottom · / search · n/N next/prev · q back")
	if m.searching {
		footer = m.input.View()
	} else if m.status != "" {
		footer = m.status
	}
	return header + "\n" + m.viewer.View() + "\n" + footer
}

const helpText = `ccodolo dashboard

  enter, a   Attach to the agent. Ctrl+] detaches and returns here; the agent keeps running.
  s          Open a new shell in the container (Ctrl+] detaches; it keeps running).
  S          List shells and reattach one.
  b          Run a command in the background. Output is logged under
             ~/.ccodolo/projects/<project>/common/.ccodolo/tasks/.
  t          List background commands: view output, attach, dismiss finished ones.
  r          Review the agent's scrollback (/ to search, n/N to step through matches).
  x          Stop the session, or every marked session.
  space      Mark a session for x.
  c          Remove stopped ccodolo containers.
  o / O      Cycle the sort column / reverse it.
  /          Filter by project, agent, or path.
  q          Quit. Sessions keep running.

Stats refresh every second (every 2 seconds with apple runtime CLIs whose stats call takes 2s).

Press any key to go back.`

// taskLabel names a background task by its command. Tasks started before
// the command was recorded fall back to the window name.
func taskLabel(w docker.TmuxWindow) string {
	if w.Command != "" {
		return w.Command
	}
	return w.Window
}

// taskLogHint is the host path of a task's log, for the status line.
func taskLogHint(project, task string) string {
	projectPath, err := config.ProjectPath(project)
	if err != nil {
		return task + ".log"
	}
	return docker.TaskLogPath(projectPath, task)
}

func pad(s string, width int, right bool) string {
	s = ansi.Truncate(s, width, "…")
	gap := width - ansi.StringWidth(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

// truncateLeft keeps the end of a path, which is the informative part.
func truncateLeft(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && ansi.StringWidth(string(r))+1 > width {
		r = r[1:]
	}
	return "…" + string(r)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < 0:
		return "-"
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func formatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTP"[exp])
}
