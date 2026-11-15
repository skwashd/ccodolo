// Package dashboard implements `ccodolo --dashboard`: a live view of every
// running ccodolo session with actions to attach, open shells, run
// background commands, review the agent's output, and stop sessions.
package dashboard

import (
	"context"
	"errors"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/skwashd/ccodolo/internal/docker"
)

// Run shows the dashboard until the user quits. Sessions keep running.
func Run() error {
	runtimes := availableRuntimes()
	if len(runtimes) == 0 {
		return errors.New("no container runtime found: install docker, or Apple's container CLI on macOS 26 / Apple Silicon")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The sources need the program to send to and the model needs the
	// sources; resolve the cycle through a late-bound send.
	var p *tea.Program
	src := newSources(runtimes, func(msg tea.Msg) { p.Send(msg) })
	p = tea.NewProgram(newModel(src, runCommand))
	src.start(ctx)

	_, err := p.Run()
	return err
}

// availableRuntimes is every backend whose CLI is installed. A docker
// binary without a running daemon still counts: its error is shown in the
// dashboard rather than hiding the backend.
func availableRuntimes() []docker.Runtime {
	var runtimes []docker.Runtime
	if _, err := exec.LookPath(docker.RuntimeDocker.Binary()); err == nil {
		runtimes = append(runtimes, docker.RuntimeDocker)
	}
	if docker.RuntimeApple.CheckHost() == nil {
		runtimes = append(runtimes, docker.RuntimeApple)
	}
	return runtimes
}
