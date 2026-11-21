package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	name := containerName(project, workdir, time.Now())

	args, err := runArgs(rt, cfg, meta, project, projectPath, workdir, name, containerWorkspace, imageTag, extraArgs)
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

	// Docker Desktop on Windows shares host files as mode 0777, which
	// OpenSSH and GnuPG refuse. Only Windows hosts have this problem.
	if runtime.GOOS == "windows" {
		for _, w := range credentialMountWarnings(cfg.Volumes) {
			fmt.Fprintln(os.Stderr, w)
		}
	}

	return execCLI(rt, args)
}

// credentialMountWarnings returns one warning per config volume that mounts
// an SSH or GnuPG directory. Bind mounts from a Windows host arrive as mode
// 0777, which OpenSSH and GnuPG refuse; Docker Desktop documents this as
// not configurable, so a warning is all ccodolo can offer.
func credentialMountWarnings(vols []config.Volume) []string {
	var warnings []string
	for _, v := range vols {
		if isCredentialPath(v.Host) || isCredentialPath(v.Container) {
			warnings = append(warnings, fmt.Sprintf(
				"Warning: %s is bind-mounted from Windows; Docker Desktop shares files as mode 0777, which OpenSSH and GnuPG reject. Use HTTPS with a credential helper for git instead.",
				v.Container))
		}
	}
	return warnings
}

// isCredentialPath reports whether any segment of p is an SSH or GnuPG
// directory. Checking segments rather than the basename, on both sides of
// the mount, catches a single key (~/.ssh/id_ed25519) and a renamed target
// (~/.ssh mounted at /home/coder/keys). Both separators are split on
// because a Windows host path may use either.
func isCredentialPath(p string) bool {
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".ssh" || seg == ".gnupg" {
			return true
		}
	}
	return false
}

// mountArg formats one bind mount as the flag and value for the runtime
// CLI. Docker gets --mount: its named fields avoid the colon ambiguity of
// a Windows drive-letter path (C:\proj:/workspace), at the cost that the
// CSV value cannot carry a comma or a bare double quote. A Windows profile
// directory can legally contain a comma, so the error names the path.
// Apple's container CLI keeps -v: it is macOS-only, so there is no drive
// letter to disambiguate, and its --mount parser rejects "=" in a path.
func mountArg(rt Runtime, src, dst string, readOnly bool) ([]string, error) {
	if rt == RuntimeApple {
		v := src + ":" + dst
		if readOnly {
			v += ":ro"
		}
		return []string{"-v", v}, nil
	}
	for _, p := range []string{src, dst} {
		if err := checkMountPath(rt, p); err != nil {
			return nil, err
		}
	}
	arg := fmt.Sprintf("type=bind,source=%s,target=%s", src, dst)
	if readOnly {
		arg += ",readonly"
	}
	return []string{"--mount", arg}, nil
}

// checkMountPath rejects the characters a docker --mount value cannot
// carry. The Apple runtime uses -v, which has no such limit.
func checkMountPath(rt Runtime, p string) error {
	if rt != RuntimeApple && strings.ContainsAny(p, `,"`) {
		return fmt.Errorf("mount path %q contains a comma or double quote, which the --mount syntax cannot express", p)
	}
	return nil
}

// CheckMounts validates the bind-mount sources Run will use, so a config
// that cannot launch fails before the image build with the offending entry
// named. Unlike -v, --mount does not create a missing host directory.
func CheckMounts(rt Runtime, cfg *config.Config, workdir, projectPath string) error {
	if err := checkMountPath(rt, workdir); err != nil {
		return fmt.Errorf("workdir: %w", err)
	}
	if err := checkMountPath(rt, projectPath); err != nil {
		return fmt.Errorf("project directory: %w", err)
	}
	for _, v := range cfg.Volumes {
		hostPath, err := config.ExpandHome(v.Host)
		if err != nil {
			return fmt.Errorf("expanding volume host path %q: %w", v.Host, err)
		}
		if _, err := os.Stat(hostPath); err != nil {
			return fmt.Errorf("volume host path %q does not exist: %w", v.Host, err)
		}
		if err := checkMountPath(rt, hostPath); err != nil {
			return fmt.Errorf("volume host path %q: %w", v.Host, err)
		}
		if err := checkMountPath(rt, v.Container); err != nil {
			return fmt.Errorf("volume container path %q: %w", v.Container, err)
		}
	}
	return nil
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
	project, projectPath, workdir, containerName, containerWorkspace, imageTag string,
	extraArgs []string,
) ([]string, error) {
	args := []string{"run", "--rm", "-it"}

	// Ctrl+] detaches the launch terminal and leaves the session running.
	// Docker intercepts it before it reaches the container; on the apple
	// runtime the same key reaches tmux, whose binding detaches instead.
	if rt == RuntimeDocker && cfg.TmuxEnabled() {
		args = append(args, "--detach-keys", "ctrl-]")
	}

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

	// Labels identify the session to the dashboard (see sessions.go).
	args = append(args, sessionLabelArgs(project, workdir, cfg.Agent, cfg.TmuxEnabled())...)

	// Working directory.
	args = append(args, "-w", containerWorkspace)

	// Host paths go through filepath.Join so they are native on every
	// platform; container paths are always Linux paths.
	type bindMount struct {
		src, dst string
		readOnly bool
	}
	mounts := []bindMount{
		{src: workdir, dst: containerWorkspace},
		{src: filepath.Join(projectPath, "commandhistory"), dst: "/commandhistory"},
		{src: filepath.Join(projectPath, "common"), dst: "/home/coder/project"},
		{src: filepath.Join(projectPath, meta.ConfigDir), dst: "/home/coder/" + meta.ConfigDir},
	}
	for _, f := range meta.ExtraFiles {
		filePath := filepath.Join(projectPath, f)
		if _, err := os.Stat(filePath); err == nil {
			mounts = append(mounts, bindMount{src: filePath, dst: "/home/coder/" + f})
		}
	}
	for _, d := range meta.ExtraDirs {
		dirPath := filepath.Join(projectPath, d)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			mounts = append(mounts, bindMount{src: dirPath, dst: "/home/coder/" + d})
		}
	}
	for _, v := range cfg.Volumes {
		hostPath, err := config.ExpandHome(v.Host)
		if err != nil {
			return nil, fmt.Errorf("expanding volume host path %q: %w", v.Host, err)
		}
		mounts = append(mounts, bindMount{src: hostPath, dst: v.Container, readOnly: v.ReadOnly})
	}
	for _, m := range mounts {
		pair, err := mountArg(rt, m.src, m.dst, m.readOnly)
		if err != nil {
			return nil, err
		}
		args = append(args, pair...)
	}

	// Config-defined environment variables.
	for k, v := range cfg.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// tmux = false: session.sh execs the agent directly, as before tmux.
	if !cfg.TmuxEnabled() {
		args = append(args, "-e", "CCODOLO_TMUX=0")
	}

	// Passthrough env vars from the host shell. Both runtimes read the value
	// of a bare `-e NAME` from their own environment, which execCLI hands
	// them on every platform. Apple documents this under `container run`:
	// https://github.com/apple/container/blob/main/docs/command-reference.md
	// A bare name keeps the value out of argv, where any local user could
	// read it with ps.
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
	//
	// hostTermVar fills in Windows Terminal, which sets neither.
	for _, name := range []string{"TERM", "COLORTERM"} {
		if _, ok := cfg.Environment[name]; ok {
			continue
		}
		if slices.Contains(cfg.PassthroughVars, name) {
			continue
		}
		if v := hostTermVar(name); v != "" {
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

// maxContainerNameLen is the Apple runtime's container name limit (the DNS
// label maximum); a longer name fails with "is not a valid container ID".
// Docker has no limit, but both get the same names so --exec matches them
// the same way.
const maxContainerNameLen = 63

// containerTimestamp suffixes every container name.
const containerTimestamp = "200601021504"

// containerNamespace is the name prefix of every container for a project and
// workdir: ccodolo-<project>-<dir>, shortened so a name built from it fits
// maxContainerNameLen with its timestamp. --exec matches on it, so Run and
// Exec must both use it.
func containerNamespace(project, workdir string) string {
	ns := fmt.Sprintf("ccodolo-%s-%s", project, filepath.Base(workdir))
	if limit := maxContainerNameLen - len("-"+containerTimestamp); len(ns) > limit {
		ns = ns[:limit]
	}
	return ns
}

// containerName names a new container started at t.
func containerName(project, workdir string, t time.Time) string {
	return containerNamespace(project, workdir) + "-" + t.Format(containerTimestamp)
}

// Exec attaches to an existing container by namespace prefix.
func Exec(rt Runtime, project, workdir string) error {
	namespace := containerNamespace(project, workdir)

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
