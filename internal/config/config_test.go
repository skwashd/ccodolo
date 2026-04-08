package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileParsesToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.toml")

	content := `
agent = "claude"

[tools]
python = ""
uv = "0.5"

[build]
custom_steps = [
    'RUN apt-get update',
]

[[volumes]]
host = "~/.aws"
container = "/home/coder/.aws"
readonly = true

[environment]
AWS_PROFILE = "dev"
`
	_ = os.WriteFile(path, []byte(content), 0o644)

	cfg, err := loadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Agent != "claude" {
		t.Errorf("expected agent 'claude', got %q", cfg.Agent)
	}
	if len(cfg.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(cfg.Tools))
	}
	if _, ok := cfg.Tools["python"]; !ok {
		t.Error("expected tool 'python' to be present")
	}
	if cfg.Tools["uv"] != "0.5" {
		t.Errorf("expected uv version '0.5', got %q", cfg.Tools["uv"])
	}
	if len(cfg.Build.CustomSteps) != 1 {
		t.Fatalf("expected 1 custom step, got %d", len(cfg.Build.CustomSteps))
	}
	if len(cfg.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(cfg.Volumes))
	}
	if cfg.Volumes[0].Host != "~/.aws" {
		t.Errorf("expected volume host '~/.aws', got %q", cfg.Volumes[0].Host)
	}
	if !cfg.Volumes[0].ReadOnly {
		t.Error("expected volume to be readonly")
	}
	if cfg.Environment["AWS_PROFILE"] != "dev" {
		t.Errorf("expected AWS_PROFILE='dev', got %q", cfg.Environment["AWS_PROFILE"])
	}
}

func TestLoadFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	_ = os.WriteFile(path, []byte("not valid toml [[["), 0o644)

	_, err := loadFile(path)
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestLoadFileNonexistent(t *testing.T) {
	_, err := loadFile("/nonexistent/path/ccodolo.toml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadProjectOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.toml")

	content := `agent = "gemini"

[tools]
python = ""
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProjectOnly(dir)
	if err != nil {
		t.Fatalf("LoadProjectOnly() error: %v", err)
	}
	if cfg.Agent != "gemini" {
		t.Errorf("expected agent 'gemini', got %q", cfg.Agent)
	}
	if len(cfg.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(cfg.Tools))
	}
	if _, ok := cfg.Tools["python"]; !ok {
		t.Errorf("expected python tool, got %v", cfg.Tools)
	}
}

func TestLoadProjectOnlyNonexistent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadProjectOnly(dir)
	if err != nil {
		t.Fatalf("LoadProjectOnly() error: %v", err)
	}
	if cfg.Agent != "" {
		t.Errorf("expected empty agent, got %q", cfg.Agent)
	}
	if len(cfg.Tools) != 0 {
		t.Errorf("expected no tools, got %v", cfg.Tools)
	}
}

func TestMergeAgent(t *testing.T) {
	global := &Config{Agent: "claude"}
	project := &Config{Agent: "gemini"}
	result := Merge(global, project)
	if result.Agent != "gemini" {
		t.Errorf("expected project agent 'gemini', got %q", result.Agent)
	}

	// Empty project agent should use global.
	project2 := &Config{}
	result2 := Merge(global, project2)
	if result2.Agent != "claude" {
		t.Errorf("expected global agent 'claude', got %q", result2.Agent)
	}
}

func TestMergeTools(t *testing.T) {
	global := &Config{
		Tools: map[string]string{
			"python": "",
			"uv":     "",
		},
	}
	project := &Config{
		Tools: map[string]string{
			"python":    "3.12",
			"terraform": "",
		},
	}
	result := Merge(global, project)

	if len(result.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(result.Tools))
	}
	if _, ok := result.Tools["python"]; !ok {
		t.Error("python should be in merged tools")
	}
	if result.Tools["python"] != "3.12" {
		t.Errorf("expected python version '3.12', got %q", result.Tools["python"])
	}
	if _, ok := result.Tools["uv"]; !ok {
		t.Error("uv should be in merged tools")
	}
	if _, ok := result.Tools["terraform"]; !ok {
		t.Error("terraform should be in merged tools")
	}
}

func TestMergeCustomSteps(t *testing.T) {
	global := &Config{Build: BuildConfig{CustomSteps: []string{"RUN echo global"}}}
	project := &Config{Build: BuildConfig{CustomSteps: []string{"RUN echo project"}}}
	result := Merge(global, project)

	if len(result.Build.CustomSteps) != 2 {
		t.Fatalf("expected 2 custom steps, got %d", len(result.Build.CustomSteps))
	}
	if result.Build.CustomSteps[0] != "RUN echo global" {
		t.Errorf("expected first step 'RUN echo global', got %q", result.Build.CustomSteps[0])
	}
	if result.Build.CustomSteps[1] != "RUN echo project" {
		t.Errorf("expected second step 'RUN echo project', got %q", result.Build.CustomSteps[1])
	}
}

func TestMergeVolumes(t *testing.T) {
	global := &Config{
		Volumes: []Volume{
			{Host: "~/.aws", Container: "/home/coder/.aws", ReadOnly: true},
		},
	}
	project := &Config{
		Volumes: []Volume{
			{Host: "~/.aws-project", Container: "/home/coder/.aws", ReadOnly: false},
			{Host: "~/.ssh", Container: "/home/coder/.ssh"},
		},
	}
	result := Merge(global, project)

	if len(result.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(result.Volumes))
	}
	// Project should override the .aws volume.
	for _, v := range result.Volumes {
		if v.Container == "/home/coder/.aws" {
			if v.Host != "~/.aws-project" {
				t.Errorf("expected project .aws host override, got %q", v.Host)
			}
			if v.ReadOnly {
				t.Error("expected project override to not be readonly")
			}
		}
	}
}

func TestMergeEnvironment(t *testing.T) {
	global := &Config{Environment: map[string]string{"A": "1", "B": "2"}}
	project := &Config{Environment: map[string]string{"B": "3", "C": "4"}}
	result := Merge(global, project)

	if result.Environment["A"] != "1" {
		t.Errorf("expected A='1', got %q", result.Environment["A"])
	}
	if result.Environment["B"] != "3" {
		t.Errorf("expected B='3' (project override), got %q", result.Environment["B"])
	}
	if result.Environment["C"] != "4" {
		t.Errorf("expected C='4', got %q", result.Environment["C"])
	}
}

func TestValidate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{
			Agent: "claude",
			Tools: map[string]string{"python": ""},
		}
		if err := Validate(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid agent", func(t *testing.T) {
		cfg := &Config{Agent: "invalid"}
		if err := Validate(cfg); err == nil {
			t.Error("expected error for invalid agent")
		}
	})

	t.Run("invalid tool", func(t *testing.T) {
		cfg := &Config{
			Agent: "claude",
			Tools: map[string]string{"nonexistent": ""},
		}
		if err := Validate(cfg); err == nil {
			t.Error("expected error for invalid tool")
		}
	})

	t.Run("invalid custom step", func(t *testing.T) {
		cfg := &Config{
			Agent: "claude",
			Build: BuildConfig{CustomSteps: []string{"ENV FOO=bar"}},
		}
		if err := Validate(cfg); err == nil {
			t.Error("expected error for invalid custom step (ENV)")
		}
	})

	t.Run("valid custom steps", func(t *testing.T) {
		cfg := &Config{
			Agent: "claude",
			Build: BuildConfig{CustomSteps: []string{
				"RUN apt-get update",
				"COPY foo /bar",
				"ADD file /dest",
			}},
		}
		if err := Validate(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("relative volume container path", func(t *testing.T) {
		cfg := &Config{
			Agent:   "claude",
			Volumes: []Volume{{Host: "~/.aws", Container: "relative/path"}},
		}
		if err := Validate(cfg); err == nil {
			t.Error("expected error for relative volume container path")
		}
	})

	t.Run("empty agent is valid", func(t *testing.T) {
		cfg := &Config{}
		if err := Validate(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestValidateCustomStep(t *testing.T) {
	tests := []struct {
		step  string
		valid bool
	}{
		{"RUN echo hello", true},
		{"run echo hello", true},
		{"COPY foo /bar", true},
		{"ADD foo /bar", true},
		{"ENV FOO=bar", false},
		{"WORKDIR /app", false},
		{"USER root", false},
		{"LABEL foo=bar", false},
		{"EXPOSE 8080", false},
	}
	for _, tt := range tests {
		err := validateCustomStep(tt.step)
		if tt.valid && err != nil {
			t.Errorf("validateCustomStep(%q) unexpected error: %v", tt.step, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateCustomStep(%q) expected error", tt.step)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.toml")

	cfg := &Config{
		Agent: "gemini",
		Tools: map[string]string{
			"python": "",
			"uv":     "0.5",
		},
		Build: BuildConfig{CustomSteps: []string{"RUN echo test"}},
		Volumes: []Volume{
			{Host: "~/.aws", Container: "/home/coder/.aws", ReadOnly: true},
		},
		Environment: map[string]string{"FOO": "bar"},
	}

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := loadFile(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if loaded.Agent != cfg.Agent {
		t.Errorf("expected agent %q, got %q", cfg.Agent, loaded.Agent)
	}
	if len(loaded.Tools) != len(cfg.Tools) {
		t.Errorf("expected %d tools, got %d", len(cfg.Tools), len(loaded.Tools))
	}
	if loaded.Environment["FOO"] != "bar" {
		t.Errorf("expected FOO='bar', got %q", loaded.Environment["FOO"])
	}
}

func TestToolSelections(t *testing.T) {
	cfg := &Config{
		Tools: map[string]string{
			"python": "",
			"uv":     "0.5",
		},
	}
	sels := cfg.ToolSelections()
	if len(sels) != 2 {
		t.Fatalf("expected 2 selections, got %d", len(sels))
	}
	// Map iteration order is nondeterministic, so check by name.
	selMap := make(map[string]string)
	for _, s := range sels {
		selMap[s.Name] = s.Version
	}
	if selMap["python"] != "" {
		t.Errorf("expected python version '', got %q", selMap["python"])
	}
	if selMap["uv"] != "0.5" {
		t.Errorf("expected uv version '0.5', got %q", selMap["uv"])
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	got, err := ExpandHome("~/foo")
	if err != nil {
		t.Fatalf("ExpandHome('~/foo') error: %v", err)
	}
	if got != filepath.Join(home, "foo") {
		t.Errorf("ExpandHome('~/foo') = %q, want %q", got, filepath.Join(home, "foo"))
	}
	got, err = ExpandHome("/abs/path")
	if err != nil {
		t.Fatalf("ExpandHome('/abs/path') error: %v", err)
	}
	if got != "/abs/path" {
		t.Errorf("ExpandHome('/abs/path') = %q, want '/abs/path'", got)
	}
	got, err = ExpandHome("relative")
	if err != nil {
		t.Fatalf("ExpandHome('relative') error: %v", err)
	}
	if got != "relative" {
		t.Errorf("ExpandHome('relative') = %q, want 'relative'", got)
	}
}
