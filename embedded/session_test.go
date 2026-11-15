package embedded

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sessionScript materialises the embedded session wrapper so sh can run
// it. The tmux path needs a TTY and tmux, so it is covered by the image
// smoke test in validate.yml; these cover the paths that exec the agent
// directly.
func sessionScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		// session.sh only ever runs inside a Linux container. On Windows
		// the sh on PATH is MSYS (Git Bash), which re-parses the flattened
		// command line with its own quoting rules and mangles argv (it's
		// arrives as its), so the test would measure MSYS, not the script.
		t.Skip("session.sh runs in Linux containers only")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	data, err := Scripts.ReadFile("scripts/session.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sh")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSessionExecsDirectlyWithoutTTY(t *testing.T) {
	script := sessionScript(t)
	for _, env := range [][]string{nil, {"CCODOLO_TMUX=0"}} {
		cmd := exec.Command("sh", script, "sh", "-c", `printf '%s|%s' "$1" "$2"; exit 7`, "sh", "a b", "it's")
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdin = strings.NewReader("") // not a TTY
		out, err := cmd.Output()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
			t.Fatalf("env %v: expected the agent's exit status 7, got %v", env, err)
		}
		if got := string(out); got != "a b|it's" {
			t.Errorf("env %v: argv not passed through intact: %q", env, got)
		}
	}
}
