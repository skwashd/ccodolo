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

// RenderData holds all values injected into the Dockerfile template.
type RenderData struct {
	ToolInstructions []string // rendered tool Dockerfile lines
	CustomSteps      []string // raw custom Dockerfile steps
	AgentInstall     string   // agent install RUN command
	AgentExtraEnv    []string // agent extra ENV lines ("KEY=VALUE")
	Entrypoint       string   // JSON array for ENTRYPOINT
	ToolEnvVars      []string // sorted "KEY=VALUE" lines from tools
	ToolPath         string   // pre-computed PATH value
	NpmConfig        string   // npm .npmrc setup (empty if no nodejs)
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

	// Resolve agent tool dependencies.
	selections := cfg.ToolSelections()
	selections = addAgentDeps(selections, meta)

	// Resolve all tool dependencies and generate instructions.
	resolved, err := tool.Resolve(selections)
	if err != nil {
		return "", fmt.Errorf("resolving tools: %w", err)
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

	// Agent extra env.
	var envLines []string
	for k, v := range meta.ExtraEnv {
		envLines = append(envLines, fmt.Sprintf("%s=%s", k, v))
	}

	// Entrypoint as JSON array.
	epJSON, err := json.Marshal(meta.Entrypoint)
	if err != nil {
		return "", fmt.Errorf("marshaling entrypoint: %w", err)
	}

	data := RenderData{
		ToolInstructions: toolLines,
		CustomSteps:      cfg.Build.CustomSteps,
		AgentInstall:     meta.InstallCmd,
		AgentExtraEnv:    envLines,
		Entrypoint:       string(epJSON),
		ToolEnvVars:      toolEnvVars,
		ToolPath:         toolPath,
		NpmConfig:        npmConfig,
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
