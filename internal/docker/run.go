package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/skwashd/ccodolo/internal/agent"
	"github.com/skwashd/ccodolo/internal/config"
	"golang.org/x/sys/unix"
)

// Run launches a new Docker container.
func Run(cfg *config.Config, project, workdir, imageTag string, extraArgs []string) error {
	a, err := agent.Parse(cfg.Agent)
	if err != nil {
		return err
	}
	meta, err := agent.Get(a)
	if err != nil {
		return err
	}

	projectPath, err := config.ProjectPath(project)
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	workdirBase := filepath.Base(workdir)
	containerWorkspace := fmt.Sprintf("/workspace/%s/%s", project, workdirBase)
	containerName := fmt.Sprintf("ccodolo-%s-%s-%s", project, workdirBase, time.Now().Format("200601021504"))

	args := []string{"run", "--rm", "-it"}

	// Container name.
	args = append(args, "--name", containerName)

	// Working directory.
	args = append(args, "-w", containerWorkspace)

	// Workdir mount.
	args = append(args, "-v", fmt.Sprintf("%s:%s", workdir, containerWorkspace))

	// Common mounts.
	args = append(args, "-v", fmt.Sprintf("%s/commandhistory:/commandhistory", projectPath))
	args = append(args, "-v", fmt.Sprintf("%s/common:/home/coder/project", projectPath))

	// Agent config dir mount.
	agentConfigPath := filepath.Join(projectPath, meta.ConfigDir)
	args = append(args, "-v", fmt.Sprintf("%s:/home/coder/%s", agentConfigPath, meta.ConfigDir))

	// Agent extra file mounts.
	for _, f := range meta.ExtraFiles {
		filePath := filepath.Join(projectPath, f)
		if _, err := os.Stat(filePath); err == nil {
			args = append(args, "-v", fmt.Sprintf("%s:/home/coder/%s", filePath, f))
		}
	}

	// Agent extra dir mounts.
	for _, d := range meta.ExtraDirs {
		dirPath := filepath.Join(projectPath, d)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			args = append(args, "-v", fmt.Sprintf("%s:/home/coder/%s", dirPath, d))
		}
	}

	// Config-defined volume mounts.
	for _, v := range cfg.Volumes {
		hostPath, err := config.ExpandHome(v.Host)
		if err != nil {
			return fmt.Errorf("expanding volume host path %q: %w", v.Host, err)
		}
		mount := fmt.Sprintf("%s:%s", hostPath, v.Container)
		if v.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	// Config-defined environment variables.
	for k, v := range cfg.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Passthrough env vars from the host shell. Docker reads the value from
	// our environment (preserved by the unix.Exec below), so we just append
	// `-e NAME` with no `=value`.
	for _, name := range cfg.PassthroughVars {
		if _, ok := os.LookupEnv(name); !ok {
			fmt.Fprintf(os.Stderr, "Warning: passthrough_vars entry %q is not set on host; skipping\n", name)
			continue
		}
		args = append(args, "-e", name)
	}

	// Forward TERM and COLORTERM from the host shell so the container's
	// interactive shell gets a matching terminal capability set. Config
	// [environment] and passthrough_vars entries take precedence — skip a
	// var the user already specified there. Unlike passthrough_vars, a
	// var that's unset on the host is skipped silently: this is a
	// convenience default, not an explicit user request.
	for _, name := range []string{"TERM", "COLORTERM"} {
		if _, ok := cfg.Environment[name]; ok {
			continue
		}
		if slices.Contains(cfg.PassthroughVars, name) {
			continue
		}
		if v := os.Getenv(name); v != "" {
			args = append(args, "-e", fmt.Sprintf("%s=%s", name, v))
		}
	}

	// Warn about any selected tool whose startup hook will be skipped
	// because a required variable won't reach the container. This is
	// informational only — it does not add -e flags itself. The user is
	// still responsible for getting the variable in via [environment] or
	// passthrough_vars. A resolve failure here is not fatal — config and
	// the image build have already succeeded — so just skip the warning.
	if resolved, resolveErr := resolveTools(cfg, meta); resolveErr == nil {
		available := make(map[string]bool)
		for k, v := range cfg.Environment {
			if v != "" {
				available[k] = true
			}
		}
		for _, name := range cfg.PassthroughVars {
			if v := os.Getenv(name); v != "" {
				available[name] = true
			}
		}
		warnMissingHookVars(missingHookVars(resolved, available))
	}

	// Image tag.
	args = append(args, imageTag)

	// Extra args (passed after --).
	args = append(args, extraArgs...)

	return runDocker(args)
}

// Exec attaches to an existing container by namespace prefix.
func Exec(project, workdir string) error {
	workdirBase := filepath.Base(workdir)
	namespace := fmt.Sprintf("ccodolo-%s-%s", project, workdirBase)

	// Find matching containers.
	out, err := exec.Command("docker", "container", "ls", "-a", "--format", "{{.Names}}:{{.ID}}").Output()
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	var matches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, namespace) {
			matches = append(matches, line)
		}
	}

	if len(matches) == 0 {
		return fmt.Errorf("no containers found for namespace: %s", namespace)
	}
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "Multiple containers found for namespace: %s\n", namespace)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		return fmt.Errorf("multiple containers found for namespace: %s", namespace)
	}

	parts := strings.SplitN(matches[0], ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return fmt.Errorf("unable to determine container ID for namespace: %s", namespace)
	}
	containerID := parts[1]

	fmt.Fprintf(os.Stderr, "Attaching to container: %s\n", containerID)
	return runDocker([]string{"exec", "-it", containerID, "/bin/zsh"})
}

// runDocker replaces the current process with docker via unix.Exec,
// giving Docker direct ownership of the terminal for interactive TUI apps.
// On success, this function never returns.
func runDocker(args []string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("finding docker executable: %w", err)
	}

	return unix.Exec(dockerPath, append([]string{"docker"}, args...), os.Environ())
}
