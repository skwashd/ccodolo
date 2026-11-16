package docker

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestInteractiveExecArgs(t *testing.T) {
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("WT_SESSION", "")

	got := AttachArgs("abc", AgentSession)
	want := []string{"exec", "-it", "-e", "TERM=xterm-ghostty", "-e", "COLORTERM=truecolor", "abc",
		"tmux", "-f", "/etc/ccodolo/tmux.conf", "attach-session", "-t", "agent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AttachArgs = %q, want %q", got, want)
	}

	got = NewShellArgs("abc", "shell-2")
	want = []string{"exec", "-it", "-e", "TERM=xterm-ghostty", "-e", "COLORTERM=truecolor", "abc",
		"tmux", "-f", "/etc/ccodolo/tmux.conf", "new-session", "-s", "shell-2", "zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NewShellArgs = %q, want %q", got, want)
	}

	// Windows Terminal sets neither variable.
	t.Setenv("TERM", "")
	t.Setenv("COLORTERM", "")
	t.Setenv("WT_SESSION", "guid")
	got = PlainShellArgs("abc")
	want = []string{"exec", "-it", "-e", "TERM=xterm-256color", "-e", "COLORTERM=truecolor", "abc", "/bin/zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PlainShellArgs = %q, want %q", got, want)
	}

	t.Setenv("WT_SESSION", "")
	got = PlainShellArgs("abc")
	want = []string{"exec", "-it", "abc", "/bin/zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PlainShellArgs without TERM = %q, want %q", got, want)
	}
}

func TestTaskArgs(t *testing.T) {
	pipe := "mkdir -p /home/coder/project/.ccodolo/tasks && exec cat >> /home/coder/project/.ccodolo/tasks/task-1.log"
	tail := []string{
		";", "set-option", "-w", "-t", "tasks:task-1", "remain-on-exit", "on",
		";", "set-option", "-w", "-t", "tasks:task-1", "@ccodolo_command", "make test",
		";", "pipe-pane", "-t", "tasks:task-1", pipe,
	}

	got := TaskArgs("abc", "task-1", "make test", false)
	want := append([]string{"exec", "abc", "tmux", "-f", "/etc/ccodolo/tmux.conf",
		"new-session", "-d", "-s", "tasks", "-n", "task-1", "make test"}, tail...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TaskArgs (new session) =\n %q\nwant\n %q", got, want)
	}

	got = TaskArgs("abc", "task-1", "make test", true)
	want = append([]string{"exec", "abc", "tmux", "-f", "/etc/ccodolo/tmux.conf",
		"new-window", "-d", "-t", "tasks:", "-n", "task-1", "make test"}, tail...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TaskArgs (existing session) =\n %q\nwant\n %q", got, want)
	}
}

func TestParseWindows(t *testing.T) {
	out := []byte("agent\tclaude\t0\t\t\nshell-1\tzsh\t0\t\t\ntasks\ttask-1\t1\t2\tmake test\ntasks\ttask-2\t0\t\tprintf 'a\tb'\nbad line\n")
	got := ParseWindows(out)
	want := []TmuxWindow{
		{Session: "agent", Window: "claude"},
		{Session: "shell-1", Window: "zsh"},
		{Session: "tasks", Window: "task-1", Dead: true, Status: 2, Command: "make test"},
		// A tab inside the command survives: it is the last field.
		{Session: "tasks", Window: "task-2", Command: "printf 'a\tb'"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseWindows = %+v, want %+v", got, want)
	}
	if got := ParseWindows(nil); len(got) != 0 {
		t.Errorf("ParseWindows(nil) = %+v, want empty", got)
	}
}

func TestSimpleActionArgs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want []string
	}{
		{"list windows", ListWindowsArgs("abc"),
			[]string{"exec", "abc", "tmux", "list-windows", "-a", "-F", "#{session_name}\t#{window_name}\t#{pane_dead}\t#{pane_dead_status}\t#{@ccodolo_command}"}},
		{"kill window", KillWindowArgs("abc", "tasks", "task-1"),
			[]string{"exec", "abc", "tmux", "kill-window", "-t", "tasks:task-1"}},
		{"capture", CaptureArgs("abc", "agent"),
			[]string{"exec", "abc", "tmux", "capture-pane", "-p", "-e", "-J", "-S", "-", "-E", "-", "-t", "agent"}},
		{"stop", StopArgs("a", "b"), []string{"stop", "a", "b"}},
		{"docker rm", RuntimeDocker.RemoveArgs("a"), []string{"rm", "a"}},
		{"apple delete", RuntimeApple.RemoveArgs("a", "b"), []string{"delete", "a", "b"}},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestTaskLogPath(t *testing.T) {
	got := TaskLogPath(filepath.Join("home", "p"), "task-1")
	want := filepath.Join("home", "p", "common", ".ccodolo", "tasks", "task-1.log")
	if got != want {
		t.Errorf("TaskLogPath = %q, want %q", got, want)
	}
}
