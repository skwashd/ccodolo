package agent

import "fmt"

// Agent represents a supported AI coding agent.
type Agent string

const (
	Claude   Agent = "claude"
	Codex    Agent = "codex"
	Copilot  Agent = "copilot"
	Gemini   Agent = "gemini"
	Kiro     Agent = "kiro"
	OpenCode Agent = "opencode"
)

// All returns all supported agents.
func All() []Agent {
	return []Agent{Claude, Codex, Copilot, Gemini, Kiro, OpenCode}
}

// AllNames returns the string names of all supported agents.
func AllNames() []string {
	agents := All()
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = string(a)
	}
	return names
}

// Valid returns true if the agent name is recognized.
func Valid(name string) bool {
	for _, a := range All() {
		if string(a) == name {
			return true
		}
	}
	return false
}

// Parse validates and returns an Agent from a string.
func Parse(name string) (Agent, error) {
	if Valid(name) {
		return Agent(name), nil
	}
	return "", fmt.Errorf("unknown agent %q, must be one of: %v", name, AllNames())
}

// Meta holds metadata for an agent.
type Meta struct {
	Name       Agent
	ConfigDir  string            // project-relative config directory, e.g. ".claude"
	ExtraFiles []string          // extra files to mount, e.g. ".claude.json"
	ExtraDirs  []string          // extra directories to mount, e.g. ".claude-plugin"
	InstallCmd string            // Dockerfile RUN instruction to install the agent
	Entrypoint []string          // container ENTRYPOINT
	ExtraEnv   map[string]string // extra env vars for the container
	DependsOn  []string          // tool names auto-included (e.g. "nodejs" for npm agents)
}

var registry = map[Agent]Meta{
	Claude: {
		Name:       Claude,
		ConfigDir:  ".claude",
		ExtraFiles: []string{".claude.json"},
		ExtraDirs:  []string{".claude-plugin"},
		InstallCmd: `RUN npm install -g @anthropic-ai/claude-code@v2.1.116`,
		Entrypoint: []string{"claude", "--dangerously-skip-permissions"},
		DependsOn:  []string{"nodejs"},
	},
	Codex: {
		Name:       Codex,
		ConfigDir:  ".codex",
		InstallCmd: `RUN npm install -g @openai/codex@v0.122.0`,
		Entrypoint: []string{"codex", "--dangerously-bypass-approvals-and-sandbox"},
		DependsOn:  []string{"nodejs"},
	},
	Copilot: {
		Name:       Copilot,
		ConfigDir:  ".copilot",
		InstallCmd: `RUN npm install -g @github/copilot@v1.0.34`,
		Entrypoint: []string{"copilot", "--allow-all"},
		DependsOn:  []string{"nodejs"},
	},
	Gemini: {
		Name:       Gemini,
		ConfigDir:  ".gemini",
		InstallCmd: `RUN npm install -g @google/gemini-cli@v0.38.2`,
		Entrypoint: []string{"gemini", "--approval-mode=yolo"},
		DependsOn:  []string{"nodejs"},
	},
	Kiro: {
		Name:       Kiro,
		ConfigDir:  ".kiro",
		InstallCmd: `RUN curl -fsSL https://cli.kiro.dev/install | bash`,
		Entrypoint: []string{"kiro-cli", "chat", "--trust-all-tools"},
		ExtraEnv:   map[string]string{"Q_FAKE_IS_REMOTE": "1"},
	},
	OpenCode: {
		Name:       OpenCode,
		ConfigDir:  ".opencode",
		InstallCmd: `RUN npm install -g opencode-ai@v1.14.20`,
		Entrypoint: []string{"opencode"},
		DependsOn:  []string{"nodejs"},
	},
}

// Get returns the metadata for the given agent.
func Get(a Agent) (Meta, error) {
	m, ok := registry[a]
	if !ok {
		return Meta{}, fmt.Errorf("no metadata for agent %q", a)
	}
	return m, nil
}

// RequiredTools returns tool names that the agent requires (e.g. nodejs for npm-based agents).
func RequiredTools(a Agent) []string {
	m, err := Get(a)
	if err != nil {
		return nil
	}
	return m.DependsOn
}
