package docker

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/skwashd/ccodolo/internal/config"
	"golang.org/x/sys/unix"
)

// Runtime selects the container CLI backend. The two backends are modeled
// as an enum with a handful of branch points rather than an interface:
// Apple's container CLI is argv-compatible with docker for everything
// ccodolo does except the divergences handled explicitly in runArgs,
// listContainers, and Build's failure hint.
type Runtime string

const (
	// RuntimeDocker is the default backend.
	RuntimeDocker Runtime = config.RuntimeDocker
	// RuntimeApple is Apple's native container CLI (github.com/apple/container).
	// Experimental; requires macOS 26 on Apple Silicon.
	RuntimeApple Runtime = config.RuntimeApple
)

// ParseRuntime maps a config runtime value to a Runtime. Empty means docker.
func ParseRuntime(name string) (Runtime, error) {
	if name == "" {
		return RuntimeDocker, nil
	}
	if !config.ValidRuntime(name) {
		return "", fmt.Errorf("invalid runtime %q, must be one of: %v", name, config.AllRuntimeNames())
	}
	return Runtime(name), nil
}

// Binary returns the CLI executable name for the backend.
func (r Runtime) Binary() string {
	if r == RuntimeApple {
		return "container"
	}
	return "docker"
}

// CheckHost verifies the host can run this backend. Docker is deliberately
// a no-op: a missing docker binary surfaces at first use, exactly as it
// always has. The Apple runtime hard-fails early with setup instructions,
// so a config with runtime = "apple" still parses and validates on any
// platform but refuses to launch where it cannot deliver VM isolation.
func (r Runtime) CheckHost() error {
	if r != RuntimeApple {
		return nil
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf(
			`runtime = "apple" requires macOS 26 on Apple Silicon (this host is %s/%s); remove runtime = "apple" from ccodolo.toml (or set it to "docker") to use Docker`,
			runtime.GOOS, runtime.GOARCH)
	}
	if _, err := exec.LookPath("container"); err != nil {
		return fmt.Errorf(
			`runtime = "apple" requires Apple's container CLI: install it with "brew install --cask container" and start it with "container system start", or remove runtime = "apple" from ccodolo.toml to use Docker`)
	}
	return nil
}

// execCLI replaces the current process with the runtime CLI via unix.Exec,
// giving it direct ownership of the terminal for interactive TUI apps.
// On success, this function never returns.
func execCLI(r Runtime, args []string) error {
	bin := r.Binary()
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("finding %s executable: %w", bin, err)
	}

	return unix.Exec(path, append([]string{bin}, args...), os.Environ())
}
