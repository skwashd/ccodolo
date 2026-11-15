package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Session actions used by the dashboard. Each *Args function returns the
// argv after the runtime binary and is pure, so both backends are covered
// by unit tests; Command turns one into an *exec.Cmd.

// tmuxConf is installed by embedded/Dockerfile.tmpl. Every tmux invocation
// passes it: whichever one starts the server is the one whose config the
// server loads, and Ctrl+] detaching depends on it.
const tmuxConf = "/etc/ccodolo/tmux.conf"

// AgentSession is the tmux session session.sh runs the agent in.
const AgentSession = "agent"

// TasksSession holds one window per background command.
const TasksSession = "tasks"

// taskLogDir is where background command output is written in the
// container: under the project's common dir, so it is on the host at
// ~/.ccodolo/projects/<project>/common/.ccodolo/tasks and outlives the
// container.
const taskLogDir = "/home/coder/project/.ccodolo/tasks"

// TaskLogPath returns the host path of a background command's log.
func TaskLogPath(projectPath, task string) string {
	return filepath.Join(projectPath, "common", ".ccodolo", "tasks", task+".log")
}

// Command returns an *exec.Cmd running the runtime binary with args.
func Command(ctx context.Context, rt Runtime, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, rt.Binary(), args...)
}

// hostTermVar returns the host's TERM or COLORTERM value. Windows Terminal
// sets neither, and docker's own fallback (TERM=xterm) leaves TUIs in 16
// colours; WT_SESSION identifies it and its capabilities are known.
func hostTermVar(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if os.Getenv("WT_SESSION") != "" {
		return map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}[name]
	}
	return ""
}

// interactiveExecArgs starts `exec -it` with the host's terminal type, so a
// tmux client inside the container knows what the outer terminal can do.
func interactiveExecArgs(id string) []string {
	args := []string{"exec", "-it"}
	for _, name := range []string{"TERM", "COLORTERM"} {
		if v := hostTermVar(name); v != "" {
			args = append(args, "-e", name+"="+v)
		}
	}
	return append(args, id)
}

// AttachArgs attaches to a tmux session (the agent's or a shell's).
// Ctrl+] detaches and leaves it running.
func AttachArgs(id, session string) []string {
	return append(interactiveExecArgs(id), "tmux", "-f", tmuxConf, "attach-session", "-t", session)
}

// NewShellArgs starts a zsh in a new tmux session, attached.
func NewShellArgs(id, session string) []string {
	return append(interactiveExecArgs(id), "tmux", "-f", tmuxConf, "new-session", "-s", session, "zsh")
}

// PlainShellArgs opens zsh directly, for containers without tmux. Exiting
// the shell returns.
func PlainShellArgs(id string) []string {
	return append(interactiveExecArgs(id), "/bin/zsh")
}

// taskCommandOption is the tmux window option TaskArgs stores the command
// in, so the task list can show what each task runs.
const taskCommandOption = "@ccodolo_command"

// tmuxWindowFormat is what ListWindowsArgs prints per window. The command
// comes last so a tab inside it survives the split.
const tmuxWindowFormat = "#{session_name}\t#{window_name}\t#{pane_dead}\t#{pane_dead_status}\t#{" + taskCommandOption + "}"

// ListWindowsArgs lists every tmux window in the container.
func ListWindowsArgs(id string) []string {
	return []string{"exec", id, "tmux", "list-windows", "-a", "-F", tmuxWindowFormat}
}

// TmuxWindow is one line of ListWindowsArgs output.
type TmuxWindow struct {
	Session string
	Window  string
	Dead    bool
	Status  int    // exit status, when Dead
	Command string // background task command; empty for other windows
}

// ParseWindows parses ListWindowsArgs output.
func ParseWindows(out []byte) []TmuxWindow {
	var windows []TmuxWindow
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.SplitN(line, "\t", 5)
		if len(f) != 5 {
			continue
		}
		w := TmuxWindow{Session: f[0], Window: f[1], Dead: f[2] == "1", Command: f[4]}
		w.Status, _ = strconv.Atoi(f[3])
		windows = append(windows, w)
	}
	return windows
}

// TaskArgs runs command in a new window of the tasks session, creating the
// session when it does not exist yet. tmux runs the command list as one
// unit before reading any pane output, so remain-on-exit (which keeps the
// exit status) and the log pipe are in place before the command can
// finish or print. The command is also stored as a window option for
// the task list.
func TaskArgs(id, task, command string, sessionExists bool) []string {
	target := TasksSession + ":" + task
	args := []string{"exec", id, "tmux", "-f", tmuxConf}
	if sessionExists {
		args = append(args, "new-window", "-d", "-t", TasksSession+":", "-n", task, command)
	} else {
		args = append(args, "new-session", "-d", "-s", TasksSession, "-n", task, command)
	}
	logFile := path.Join(taskLogDir, task+".log")
	return append(args,
		";", "set-option", "-w", "-t", target, "remain-on-exit", "on",
		";", "set-option", "-w", "-t", target, taskCommandOption, command,
		";", "pipe-pane", "-t", target, fmt.Sprintf("mkdir -p %s && exec cat >> %s", taskLogDir, logFile),
	)
}

// KillWindowArgs dismisses a finished background command.
func KillWindowArgs(id, session, window string) []string {
	return []string{"exec", id, "tmux", "kill-window", "-t", session + ":" + window}
}

// CaptureArgs prints a tmux pane's full history, with colours.
func CaptureArgs(id, target string) []string {
	return []string{"exec", id, "tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-E", "-", "-t", target}
}

// StopArgs stops containers. session.sh traps SIGTERM, so this is prompt.
func StopArgs(ids ...string) []string {
	return append([]string{"stop"}, ids...)
}

// RemoveArgs deletes stopped containers.
func (r Runtime) RemoveArgs(ids ...string) []string {
	verb := "rm"
	if r == RuntimeApple {
		verb = "delete"
	}
	return append([]string{verb}, ids...)
}
