package agent

import (
	"testing"
)

func TestAllAgents(t *testing.T) {
	agents := All()
	if len(agents) != 6 {
		t.Errorf("expected 6 agents, got %d", len(agents))
	}
}

func TestAllNames(t *testing.T) {
	names := AllNames()
	expected := []string{"claude", "codex", "copilot", "gemini", "kiro", "opencode"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected name[%d] = %q, got %q", i, expected[i], name)
		}
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"claude", true},
		{"codex", true},
		{"copilot", true},
		{"gemini", true},
		{"kiro", true},
		{"opencode", true},
		{"unknown", false},
		{"", false},
		{"Claude", false},
	}
	for _, tt := range tests {
		if got := Valid(tt.name); got != tt.valid {
			t.Errorf("Valid(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}

func TestParse(t *testing.T) {
	a, err := Parse("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != Claude {
		t.Errorf("expected Claude, got %v", a)
	}

	_, err = Parse("invalid")
	if err == nil {
		t.Error("expected error for invalid agent")
	}
}

func TestGet(t *testing.T) {
	meta, err := Get(Claude)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.ConfigDir != ".claude" {
		t.Errorf("expected ConfigDir .claude, got %q", meta.ConfigDir)
	}
	if meta.InstallCmd == "" {
		t.Error("expected non-empty InstallCmd")
	}
	if len(meta.Entrypoint) == 0 {
		t.Error("expected non-empty Entrypoint")
	}
}

func TestGetAllAgentsHaveMetadata(t *testing.T) {
	for _, a := range All() {
		meta, err := Get(a)
		if err != nil {
			t.Errorf("agent %q: %v", a, err)
			continue
		}
		if meta.ConfigDir == "" {
			t.Errorf("agent %q: empty ConfigDir", a)
		}
		if meta.InstallCmd == "" {
			t.Errorf("agent %q: empty InstallCmd", a)
		}
		if len(meta.Entrypoint) == 0 {
			t.Errorf("agent %q: empty Entrypoint", a)
		}
	}
}

func TestNpmAgentsDependOnNodejs(t *testing.T) {
	npmAgents := []Agent{Claude, Codex, Copilot, Gemini, OpenCode}
	for _, a := range npmAgents {
		deps := RequiredTools(a)
		found := false
		for _, d := range deps {
			if d == "nodejs" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("npm agent %q should depend on nodejs", a)
		}
	}
}

func TestNonNpmAgentsDoNotDependOnNodejs(t *testing.T) {
	for _, a := range []Agent{Kiro} {
		deps := RequiredTools(a)
		for _, d := range deps {
			if d == "nodejs" {
				t.Errorf("agent %q should not depend on nodejs", a)
			}
		}
	}
}

func TestClaudeExtraFiles(t *testing.T) {
	meta, _ := Get(Claude)
	if len(meta.ExtraFiles) == 0 {
		t.Error("claude should have extra files (.claude.json)")
	}
	if len(meta.ExtraDirs) == 0 {
		t.Error("claude should have extra dirs (.claude-plugin)")
	}
}

func TestKiroExtraEnv(t *testing.T) {
	meta, _ := Get(Kiro)
	if meta.ExtraEnv == nil {
		t.Fatal("kiro should have extra env vars")
	}
	if meta.ExtraEnv["Q_FAKE_IS_REMOTE"] != "1" {
		t.Errorf("kiro Q_FAKE_IS_REMOTE should be '1', got %q", meta.ExtraEnv["Q_FAKE_IS_REMOTE"])
	}
}
