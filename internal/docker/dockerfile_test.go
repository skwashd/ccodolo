package docker

import (
	"strings"
	"testing"

	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/tool"
)

func TestRenderDockerfileClaude(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
		Tools: map[string]string{
			"python": "",
			"uv":     "",
		},
	}

	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain base image.
	if !strings.Contains(result, "FROM public.ecr.aws/docker/library/debian:trixie-slim") {
		t.Error("should contain base image")
	}

	// Should contain python tool.
	if !strings.Contains(result, "# Tool: python") {
		t.Error("should contain python tool comment")
	}

	// Should contain uv tool.
	if !strings.Contains(result, "# Tool: uv") {
		t.Error("should contain uv tool comment")
	}

	// Should contain claude install.
	if !strings.Contains(result, "@anthropic-ai/claude-code") {
		t.Error("should contain claude install command")
	}

	// Should contain entrypoint.
	if !strings.Contains(result, `["claude","--dangerously-skip-permissions"]`) {
		t.Error("should contain claude entrypoint")
	}

	// Should contain FROM scratch for squash.
	if !strings.Contains(result, "FROM scratch") {
		t.Error("should contain FROM scratch for squash")
	}

	// Claude is npm-based, so nodejs should be auto-added.
	if !strings.Contains(result, "# Tool: nodejs") {
		t.Error("should auto-add nodejs for claude agent")
	}
}

func TestRenderDockerfileGemini(t *testing.T) {
	cfg := &config.Config{
		Agent: "gemini",
	}

	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should auto-add nodejs since gemini is npm-based.
	if !strings.Contains(result, "# Tool: nodejs") {
		t.Error("should auto-add nodejs for gemini agent")
	}

	// Should contain npm config.
	if !strings.Contains(result, "prefix=/home/coder/.local/") {
		t.Error("should contain npm config when nodejs is present")
	}

	// Should contain gemini install.
	if !strings.Contains(result, "gemini-cli") {
		t.Error("should contain gemini install command")
	}

	// Should contain gemini entrypoint.
	if !strings.Contains(result, `["gemini","--approval-mode=yolo"]`) {
		t.Error("should contain gemini entrypoint")
	}
}

func TestRenderDockerfileKiro(t *testing.T) {
	cfg := &config.Config{
		Agent: "kiro",
	}

	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain kiro extra env.
	if !strings.Contains(result, "Q_FAKE_IS_REMOTE=1") {
		t.Error("should contain Q_FAKE_IS_REMOTE env")
	}

	// Should contain kiro entrypoint.
	if !strings.Contains(result, `["kiro-cli","chat","--trust-all-tools"]`) {
		t.Error("should contain kiro entrypoint")
	}
}

func TestRenderDockerfileWithCustomSteps(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
		Build: config.BuildConfig{
			CustomSteps: []string{
				"RUN sudo apt-get update && sudo apt-get install -y postgresql-client",
			},
		},
	}

	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "postgresql-client") {
		t.Error("should contain custom step")
	}
}

func TestRenderDockerfileInvalidAgent(t *testing.T) {
	cfg := &config.Config{Agent: "invalid"}
	_, err := RenderDockerfile(cfg)
	if err == nil {
		t.Error("expected error for invalid agent")
	}
}

func TestAddAgentDepsNpm(t *testing.T) {
	sels := []tool.ToolSelection{{Name: "python"}}
	meta := struct {
		DependsOn []string
	}{
		DependsOn: []string{"nodejs"},
	}
	// Manually test the logic.
	existing := make(map[string]bool)
	for _, s := range sels {
		existing[s.Name] = true
	}
	for _, dep := range meta.DependsOn {
		if !existing[dep] {
			sels = append(sels, tool.ToolSelection{Name: dep})
		}
	}
	if len(sels) != 2 {
		t.Errorf("expected 2 selections, got %d", len(sels))
	}
}

func TestAddAgentDepsNoDuplicate(t *testing.T) {
	sels := []tool.ToolSelection{{Name: "nodejs"}}
	meta := struct {
		DependsOn []string
	}{
		DependsOn: []string{"nodejs"},
	}
	existing := make(map[string]bool)
	for _, s := range sels {
		existing[s.Name] = true
	}
	for _, dep := range meta.DependsOn {
		if !existing[dep] {
			sels = append(sels, tool.ToolSelection{Name: dep})
		}
	}
	if len(sels) != 1 {
		t.Errorf("expected 1 selection (no duplicate), got %d", len(sels))
	}
}

func TestRenderDockerfileRustEnvConditional(t *testing.T) {
	// With rust: should have RUSTUP_HOME, CARGO_HOME, and /usr/local/cargo/bin in PATH.
	cfg := &config.Config{
		Agent: "claude",
		Tools: map[string]string{"rust": ""},
	}
	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `RUSTUP_HOME="/usr/local/rustup"`) {
		t.Error("rust selected: should contain RUSTUP_HOME")
	}
	if !strings.Contains(result, `CARGO_HOME="/usr/local/cargo"`) {
		t.Error("rust selected: should contain CARGO_HOME")
	}
	if !strings.Contains(result, "/usr/local/cargo/bin") {
		t.Error("rust selected: PATH should contain /usr/local/cargo/bin")
	}

	// Without rust: should NOT have RUSTUP_HOME, CARGO_HOME, or /usr/local/cargo/bin.
	cfgNoRust := &config.Config{
		Agent: "claude",
		Tools: map[string]string{"python": ""},
	}
	resultNoRust, err := RenderDockerfile(cfgNoRust)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(resultNoRust, "RUSTUP_HOME") {
		t.Error("no rust: should NOT contain RUSTUP_HOME")
	}
	if strings.Contains(resultNoRust, "CARGO_HOME") {
		t.Error("no rust: should NOT contain CARGO_HOME")
	}
	if strings.Contains(resultNoRust, "/usr/local/cargo/bin") {
		t.Error("no rust: PATH should NOT contain /usr/local/cargo/bin")
	}
}

func TestRenderDockerfileGolangPathConditional(t *testing.T) {
	// With golang: PATH should contain /usr/local/go/bin.
	cfg := &config.Config{
		Agent: "claude",
		Tools: map[string]string{"golang": ""},
	}
	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "/usr/local/go/bin") {
		t.Error("golang selected: PATH should contain /usr/local/go/bin")
	}

	// Without golang: PATH should NOT contain /usr/local/go/bin.
	cfgNoGo := &config.Config{
		Agent: "claude",
		Tools: map[string]string{"python": ""},
	}
	resultNoGo, err := RenderDockerfile(cfgNoGo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(resultNoGo, "/usr/local/go/bin") {
		t.Error("no golang: PATH should NOT contain /usr/local/go/bin")
	}
}

func TestRenderDockerfileUserRootBeforeTools(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
		Tools: map[string]string{"python": ""},
	}
	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	userRootIdx := strings.Index(result, "USER root")
	toolIdx := strings.Index(result, "# Tool: python")
	if userRootIdx == -1 {
		t.Fatal("should contain USER root")
	}
	if toolIdx == -1 {
		t.Fatal("should contain tool comment")
	}
	if userRootIdx >= toolIdx {
		t.Error("USER root should appear before tool instructions")
	}
}

func TestRenderDockerfileNoUsernameArg(t *testing.T) {
	cfg := &config.Config{
		Agent: "claude",
	}
	result, err := RenderDockerfile(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "ARG USERNAME") {
		t.Error("should not contain ARG USERNAME")
	}
	if strings.Contains(result, "$USERNAME") {
		t.Error("should not contain $USERNAME references")
	}
}

func TestRenderDockerfileAllAgents(t *testing.T) {
	agents := []string{"claude", "codex", "copilot", "gemini", "kiro", "opencode"}
	for _, a := range agents {
		t.Run(a, func(t *testing.T) {
			cfg := &config.Config{Agent: a}
			result, err := RenderDockerfile(cfg)
			if err != nil {
				t.Fatalf("unexpected error for agent %s: %v", a, err)
			}
			if result == "" {
				t.Errorf("empty Dockerfile for agent %s", a)
			}
			if !strings.Contains(result, "FROM scratch") {
				t.Errorf("missing FROM scratch for agent %s", a)
			}
			if !strings.Contains(result, "ENTRYPOINT") {
				t.Errorf("missing ENTRYPOINT for agent %s", a)
			}
		})
	}
}
