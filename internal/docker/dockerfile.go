package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/skwashd/ccodolo/embedded"
	"github.com/skwashd/ccodolo/internal/agent"
	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/tool"
)

// startupRunnerPath is where the startup-hook runner script is installed in
// the image (embedded/Dockerfile.tmpl) and the ENTRYPOINT it's wrapped with
// point at the same path — keep them in sync.
const startupRunnerPath = "/usr/local/bin/ccodolo-startup"

// startupHookDir is the directory startup hook scripts are installed into.
// Must match the glob the runner script (embedded/Dockerfile.tmpl) iterates.
const startupHookDir = "/etc/ccodolo/startup.d"

// RenderData holds all values injected into the Dockerfile template.
type RenderData struct {
	ToolInstructions []string // rendered tool Dockerfile lines
	RootSteps        []string // raw root-level Dockerfile steps (pre-tools)
	CustomSteps      []string // raw custom Dockerfile steps
	AgentInstall     string   // agent install RUN command
	AgentExtraEnv    []string // agent extra ENV lines ("KEY=VALUE")
	Entrypoint       string   // JSON array for ENTRYPOINT
	ToolEnvVars      []string // sorted "KEY=VALUE" lines from tools
	ToolPath         string   // pre-computed PATH value
	NpmConfig        string   // npm .npmrc setup (empty if no nodejs)
	StartupHookSteps []string // RUN instructions installing tool startup hooks
	HasStartupHooks  bool     // gates the startup-hook runner install + ENTRYPOINT wrap
}

// resolveTools resolves the final, dependency-ordered, deterministically
// sorted set of tools for cfg — agent-required tools included. Shared by
// RenderDockerfile and the run-time startup-hook pre-flight check (see
// missingHookVars in hooks.go) so both see the exact same tool set.
func resolveTools(cfg *config.Config, meta agent.Meta) ([]tool.ResolvedTool, error) {
	// Resolve agent tool dependencies.
	selections := cfg.ToolSelections()
	selections = addAgentDeps(selections, meta)

	// Sort selections by name for deterministic Dockerfile output.
	// cfg.Tools is a map, so ToolSelections() returns entries in random order;
	// without sorting here the rendered Dockerfile (and its SHA-256 image tag)
	// would differ on every run, defeating the rebuild-on-change cache.
	sort.Slice(selections, func(i, j int) bool {
		return selections[i].Name < selections[j].Name
	})

	// Resolve all tool dependencies and generate instructions.
	resolved, err := tool.Resolve(selections)
	if err != nil {
		return nil, fmt.Errorf("resolving tools: %w", err)
	}
	return resolved, nil
}

// RenderDockerfile generates the full Dockerfile content from a config.
func RenderDockerfile(cfg *config.Config) (string, error) {
	a, err := agent.Parse(cfg.Agent)
	if err != nil {
		return "", err
	}

	meta, err := agent.Get(a)
	if err != nil {
		return "", err
	}

	resolved, err := resolveTools(cfg, meta)
	if err != nil {
		return "", err
	}

	var toolLines []string
	for _, rt := range resolved {
		if len(rt.DockerLines) > 0 {
			toolLines = append(toolLines, fmt.Sprintf("# Tool: %s (%s)", rt.Name, rt.Tag))
			toolLines = append(toolLines, rt.DockerLines...)
			toolLines = append(toolLines, "")
		}
	}

	// Collect tool env vars and path entries.
	envMap := make(map[string]string)
	var pathEntries []string
	for _, rt := range resolved {
		for k, v := range rt.EnvVars {
			envMap[k] = v
		}
		pathEntries = append(pathEntries, rt.PathEntries...)
	}

	// Sort env var keys for deterministic output.
	var envKeys []string
	for k := range envMap {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	var toolEnvVars []string
	for _, k := range envKeys {
		toolEnvVars = append(toolEnvVars, fmt.Sprintf("%s=\"%s\"", k, envMap[k]))
	}

	// Compute ToolPath.
	toolPath := "/home/coder/.local/bin:$PATH"
	if len(pathEntries) > 0 {
		toolPath = strings.Join(pathEntries, ":") + ":" + toolPath
	}

	// Build npm config if nodejs is present.
	var npmConfig string
	if hasNodejs(resolved) {
		npmConfig = "# npm global config\n" +
			`RUN printf 'prefix=/home/coder/.local/\ncache=/home/coder/.npm\nglobal=true\n' > ~/.npmrc`
	}

	// Agent extra env. Sort for deterministic output (meta.ExtraEnv is a map).
	var envLines []string
	for k, v := range meta.ExtraEnv {
		envLines = append(envLines, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(envLines)

	// Startup hook install steps, in resolved (dependency, then sorted-name)
	// order, so filenames stay stable across runs.
	var hookSteps []string
	for i, rt := range resolved {
		if rt.StartupHook == "" {
			continue
		}
		hookSteps = append(hookSteps, renderStartupHookStep(i, rt))
	}

	// Entrypoint as JSON array. When any selected tool has a startup hook,
	// wrap the agent entrypoint with the hook runner so hooks execute before
	// the agent launches. Images with no hooks render byte-identical to
	// before this feature existed, so existing image tags are unaffected.
	entrypoint := meta.Entrypoint
	if len(hookSteps) > 0 {
		entrypoint = append([]string{startupRunnerPath}, meta.Entrypoint...)
	}
	epJSON, err := json.Marshal(entrypoint)
	if err != nil {
		return "", fmt.Errorf("marshaling entrypoint: %w", err)
	}

	data := RenderData{
		ToolInstructions: toolLines,
		RootSteps:        cfg.Build.RootSteps,
		CustomSteps:      cfg.Build.CustomSteps,
		AgentInstall:     meta.InstallCmd,
		AgentExtraEnv:    envLines,
		Entrypoint:       string(epJSON),
		ToolEnvVars:      toolEnvVars,
		ToolPath:         toolPath,
		NpmConfig:        npmConfig,
		StartupHookSteps: hookSteps,
		HasStartupHooks:  len(hookSteps) > 0,
	}

	tmpl, err := template.New("Dockerfile").Parse(string(embedded.DockerfileTemplate))
	if err != nil {
		return "", fmt.Errorf("parsing Dockerfile template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing Dockerfile template: %w", err)
	}

	return buf.String(), nil
}

// addAgentDeps ensures agent-required tools are in the selection list.
func addAgentDeps(selections []tool.ToolSelection, meta agent.Meta) []tool.ToolSelection {
	existing := make(map[string]bool)
	for _, s := range selections {
		existing[s.Name] = true
	}
	for _, dep := range meta.DependsOn {
		if !existing[dep] {
			selections = append(selections, tool.ToolSelection{Name: dep})
			existing[dep] = true
		}
	}
	return selections
}

// hasNodejs checks if nodejs is in the resolved tool list.
func hasNodejs(resolved []tool.ResolvedTool) bool {
	for _, rt := range resolved {
		if strings.EqualFold(rt.Name, "nodejs") {
			return true
		}
	}
	return false
}

// renderStartupHookStep builds the Dockerfile RUN instruction that installs
// one tool's startup hook as a standalone script under startupHookDir. The
// hook is executed (not sourced) by the runner at container start, guarded
// by a check that every declared StartupHookVars entry is set and
// non-empty — if any is missing, the script prints a one-line notice to the
// runner's log and exits 0 without running the hook.
//
// index prefixes the filename so hooks install and (if ever inspected)
// list in the same deterministic order resolveTools produced them in.
//
// StartupHook is literal shell, not run through the Instructions template
// pass — renderInstructions' %s substitution would corrupt a hook
// containing printf-style format specifiers (e.g. `printf '%s\n' "$TOKEN"`).
// Each line is written via `printf '%s\n'` with every argument single-quoted
// (embedded single quotes escaped as '\''), so the hook's shell reaches the
// script byte-for-byte regardless of its content.
func renderStartupHookStep(index int, rt tool.ResolvedTool) string {
	scriptPath := fmt.Sprintf("%s/%02d-%s.sh", startupHookDir, index, rt.Name)

	var lines []string
	lines = append(lines, fmt.Sprintf("# ccodolo startup hook: %s", rt.Name))
	if len(rt.StartupHookVars) > 0 {
		lines = append(lines, fmt.Sprintf("for __v in %s; do", strings.Join(rt.StartupHookVars, " ")))
		lines = append(lines, `    eval "__val=\${$__v:-}"`)
		lines = append(lines, fmt.Sprintf(
			`    [ -n "$__val" ] || { echo "skipping %s hook: $__v not set"; exit 0; }`, rt.Name,
		))
		lines = append(lines, "done")
	}
	lines = append(lines, strings.Split(rt.StartupHook, "\n")...)

	quoted := make([]string, len(lines))
	for i, l := range lines {
		quoted[i] = shellSingleQuote(l)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Startup hook: %s (%s)\n", rt.Name, rt.Tag)
	fmt.Fprintf(&b, "RUN mkdir -p %s \\\n", startupHookDir)
	b.WriteString(" && printf '%s\\n' \\\n")
	for _, q := range quoted {
		fmt.Fprintf(&b, "    %s \\\n", q)
	}
	fmt.Fprintf(&b, "    > %s \\\n", scriptPath)
	fmt.Fprintf(&b, " && chmod 0755 %s", scriptPath)

	return b.String()
}

// shellSingleQuote wraps s in single quotes for safe inclusion as a single
// POSIX shell word, escaping any embedded single quotes.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
