//go:build unix

package docker

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// execCLI replaces the current process with the runtime CLI via execve,
// giving it direct ownership of the terminal for interactive TUI apps.
// On success, this function never returns.
//
// Replacing the process — rather than running the CLI as a child with
// forwarded stdio — is what fixed issue #29 (agent TUIs unresponsive to
// keyboard input under Ghostty and possibly other terminals; commit
// 559c549). Do not reintroduce exec.Command here.
//
// extraEnv is NAME=value entries (secrets) for the CLI's environment only.
// ccodolo's own environment is never modified, and LookPath runs against it
// unchanged.
func execCLI(r Runtime, args []string, extraEnv []string) error {
	bin := r.Binary()
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("finding %s executable: %w", bin, err)
	}

	return syscall.Exec(path, append([]string{bin}, args...), mergeEnv(os.Environ(), extraEnv))
}
