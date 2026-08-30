package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/skwashd/ccodolo/internal/agent"
	"github.com/skwashd/ccodolo/internal/config"
)

// defaultAppleMemory is the VM memory size for apple-runtime containers
// when the config does not set one.
const defaultAppleMemory = "4g"

// Run launches a new container.
func Run(rt Runtime, cfg *config.Config, project, workdir, imageTag string, extraArgs []string) error {
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

	args, err := runArgs(rt, cfg, meta, projectPath, workdir, containerName, containerWorkspace, imageTag, extraArgs)
	if err != nil {
		return err
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

	return execCLI(rt, args)
}

// runArgs assembles the argv after the binary name for launching a
// container. containerName and containerWorkspace are precomputed by Run so
// this stays deterministic under test (the timestamped name is the only
// nondeterministic input). It still consults the filesystem for agent extra
// file/dir mounts and the host environment for passthrough vars.
func runArgs(
	rt Runtime,
	cfg *config.Config,
	meta agent.Meta,
	projectPath, workdir, containerName, containerWorkspace, imageTag string,
	extraArgs []string,
) ([]string, error) {
	args := []string{"run", "--rm", "-it"}

	// Memory. Docker containers share host memory unless capped, so no flag
	// is emitted when memory is unset; the apple runtime boots each container
	// in its own VM that defaults to 1GB — too small for an agent plus its
	// toolchain — so that backend always gets an explicit size.
	memory := cfg.Memory
	if memory == "" && rt == RuntimeApple {
		memory = defaultAppleMemory
	}
	if memory != "" {
		args = append(args, "--memory", memory)
	}

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
			return nil, fmt.Errorf("expanding volume host path %q: %w", v.Host, err)
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

	// Passthrough env vars from the host shell. Docker reads the value of a
	// bare `-e NAME` from its own environment (preserved by the unix.Exec in
	// execCLI). Apple's container CLI is not documented to do the same, so
	// for that backend the value is resolved host-side into -e NAME=value.
	for _, name := range cfg.PassthroughVars {
		v, ok := os.LookupEnv(name)
		if !ok {
			fmt.Fprintf(os.Stderr, "Warning: passthrough_vars entry %q is not set on host; skipping\n", name)
			continue
		}
		if rt == RuntimeApple {
			args = append(args, "-e", name+"="+v)
		} else {
			args = append(args, "-e", name)
		}
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

	// Image tag.
	args = append(args, imageTag)

	// Extra args (passed after --).
	args = append(args, extraArgs...)

	return args, nil
}

// containerEntry is one row from the backend's container listing.
type containerEntry struct {
	Name string
	ID   string
}

// listContainers returns all containers (including stopped) known to the backend.
func listContainers(rt Runtime) ([]containerEntry, error) {
	if rt == RuntimeApple {
		out, err := exec.Command("container", "ls", "-a", "--format", "json").Output()
		if err != nil {
			return nil, fmt.Errorf("listing containers: %w", err)
		}
		return parseAppleContainerList(out)
	}

	out, err := exec.Command("docker", "container", "ls", "-a", "--format", "{{.Names}}:{{.ID}}").Output()
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}
	return parseDockerContainerList(out), nil
}

// parseDockerContainerList parses `name:id` lines from docker's Go-template
// --format output.
func parseDockerContainerList(out []byte) []containerEntry {
	var entries []containerEntry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		entry := containerEntry{Name: parts[0]}
		if len(parts) == 2 {
			entry.ID = parts[1]
		}
		entries = append(entries, entry)
	}
	return entries
}

// parseAppleContainerList parses `container ls --format json`. In Apple's
// container CLI the user-supplied --name is the container's identifier,
// surfaced as configuration.id in the JSON.
func parseAppleContainerList(out []byte) ([]containerEntry, error) {
	var raw []struct {
		Configuration struct {
			ID string `json:"id"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing container list: %w", err)
	}
	entries := make([]containerEntry, 0, len(raw))
	for _, c := range raw {
		entries = append(entries, containerEntry{Name: c.Configuration.ID, ID: c.Configuration.ID})
	}
	return entries, nil
}

// matchContainer finds exactly one entry whose name has the namespace
// prefix; zero or multiple matches is an error.
func matchContainer(entries []containerEntry, namespace string) (containerEntry, error) {
	var matches []containerEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Name, namespace) {
			matches = append(matches, e)
		}
	}

	if len(matches) == 0 {
		return containerEntry{}, fmt.Errorf("no containers found for namespace: %s", namespace)
	}
	if len(matches) > 1 {
		fmt.Fprintf(os.Stderr, "Multiple containers found for namespace: %s\n", namespace)
		for _, m := range matches {
			fmt.Fprintf(os.Stderr, "  %s:%s\n", m.Name, m.ID)
		}
		return containerEntry{}, fmt.Errorf("multiple containers found for namespace: %s", namespace)
	}
	if matches[0].ID == "" {
		return containerEntry{}, fmt.Errorf("unable to determine container ID for namespace: %s", namespace)
	}
	return matches[0], nil
}

// Exec attaches to an existing container by namespace prefix.
func Exec(rt Runtime, project, workdir string) error {
	workdirBase := filepath.Base(workdir)
	namespace := fmt.Sprintf("ccodolo-%s-%s", project, workdirBase)

	entries, err := listContainers(rt)
	if err != nil {
		return err
	}
	match, err := matchContainer(entries, namespace)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Attaching to container: %s\n", match.ID)
	return execCLI(rt, []string{"exec", "-it", match.ID, "/bin/zsh"})
}
