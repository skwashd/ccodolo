//go:build windows

package docker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

// execCLI runs the runtime CLI as a child process, because Windows has no
// execve (syscall.Exec returns EWINDOWS) for the replacement exec_unix.go
// does. It returns only if the child could not be started; otherwise it
// exits with the child's status.
func execCLI(r Runtime, args []string) error {
	bin := r.Binary()
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("finding %s executable: %w", bin, err)
	}

	cmd := exec.Command(path, args...)
	// Assigning the *os.File values directly matters: os/exec passes real
	// files through as handles rather than pumping them through pipes, so
	// docker.exe can put the console into raw mode and `-it` sees a TTY.
	// cmd.Env is left nil so the child inherits our environment, which the
	// bare `-e NAME` passthrough entries in runArgs rely on.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// Ctrl+C reaches every process on the console. Swallow it here so docker
	// and the agent inside the container get to handle it. Not signal.Ignore:
	// on Windows that clears the signal from the runtime's wanted set, the
	// console handler falls through to the default action, and we die
	// anyway. Nothing reads the channel: the handler reports the event
	// handled once the signal is wanted, and delivery to a full channel is
	// a non-blocking drop. No CREATE_NEW_PROCESS_GROUP either, which would
	// stop Ctrl+C reaching the child at all.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Mirror execve semantics: the container's exit status is ours.
			// main.go would otherwise flatten any error to exit 1.
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("running %s: %w", bin, err)
	}
	return nil
}
