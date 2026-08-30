package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileParsesToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.toml")

	content := `
agent = "claude"
runtime = "apple"
passthrough_vars = ["GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY"]

[tools]
python = ""
uv = "0.5"

[build]
custom_steps = [
    'RUN apt-get update',
]
root_steps = [
    "RUN curl -fsSL https://pki.acme.example/root-ca.crt -o /tmp/ca.crt && openssl x509 -in /tmp/ca.crt -noout -fingerprint -sha256 | grep -q 'SHA256 Fingerprint=AA:BB' && mv /tmp/ca.crt /usr/local/share/ca-certificates/internal-ca.crt && update-ca-certificates",
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
	if cfg.Runtime != "apple" {
		t.Errorf("expected runtime 'apple', got %q", cfg.Runtime)
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
	if len(cfg.Build.RootSteps) != 1 {
		t.Fatalf("expected 1 root step, got %d", len(cfg.Build.RootSteps))
	}
	if !strings.Contains(cfg.Build.RootSteps[0], "update-ca-certificates") {
		t.Errorf("expected root step to contain 'update-ca-certificates', got %q", cfg.Build.RootSteps[0])
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
	if len(cfg.PassthroughVars) != 2 {
		t.Fatalf("expected 2 passthrough_vars, got %d", len(cfg.PassthroughVars))
	}
	if cfg.PassthroughVars[0] != "GITHUB_TOKEN" || cfg.PassthroughVars[1] != "AWS_SECRET_ACCESS_KEY" {
		t.Errorf("unexpected passthrough_vars order: %v", cfg.PassthroughVars)
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

	content := `agent = "antigravity"

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
	if cfg.Agent != "antigravity" {
		t.Errorf("expected agent 'antigravity', got %q", cfg.Agent)
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
	project := &Config{Agent: "antigravity"}
	result := Merge(global, project)
	if result.Agent != "antigravity" {
		t.Errorf("expected project agent 'antigravity', got %q", result.Agent)
	}

	// Empty project agent should use global.
	project2 := &Config{}
	result2 := Merge(global, project2)
	if result2.Agent != "claude" {
		t.Errorf("expected global agent 'claude', got %q", result2.Agent)
	}
}

func TestMergeRuntime(t *testing.T) {
	global := &Config{Runtime: RuntimeDocker}
	project := &Config{Runtime: RuntimeApple}
	result := Merge(global, project)
	if result.Runtime != RuntimeApple {
		t.Errorf("expected project runtime 'apple', got %q", result.Runtime)
	}

	// Empty project runtime should use global.
	project2 := &Config{}
	result2 := Merge(global, project2)
	if result2.Runtime != RuntimeDocker {
		t.Errorf("expected global runtime 'docker', got %q", result2.Runtime)
	}
}

func TestMergeMemory(t *testing.T) {
	global := &Config{Memory: "4g"}
	project := &Config{Memory: "8g"}
	result := Merge(global, project)
	if result.Memory != "8g" {
		t.Errorf("expected project memory '8g', got %q", result.Memory)
	}

	// Empty project memory should use global.
	project2 := &Config{}
	result2 := Merge(global, project2)
	if result2.Memory != "4g" {
		t.Errorf("expected global memory '4g', got %q", result2.Memory)
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

func TestMergeRootSteps(t *testing.T) {
	global := &Config{Build: BuildConfig{RootSteps: []string{"RUN echo global-root"}}}
	project := &Config{Build: BuildConfig{RootSteps: []string{"RUN echo project-root"}}}
	result := Merge(global, project)

	if len(result.Build.RootSteps) != 2 {
		t.Fatalf("expected 2 root steps, got %d", len(result.Build.RootSteps))
	}
	if result.Build.RootSteps[0] != "RUN echo global-root" {
		t.Errorf("expected first step 'RUN echo global-root', got %q", result.Build.RootSteps[0])
	}
	if result.Build.RootSteps[1] != "RUN echo project-root" {
		t.Errorf("expected second step 'RUN echo project-root', got %q", result.Build.RootSteps[1])
	}
}

func TestMergeStepOrigins(t *testing.T) {
	global := &Config{Build: BuildConfig{
		CustomSteps: []string{"RUN echo global-custom-1", "RUN echo global-custom-2"},
		RootSteps:   []string{"RUN echo global-root"},
	}}
	project := &Config{Build: BuildConfig{
		CustomSteps: []string{"RUN echo project-custom"},
		RootSteps:   []string{"RUN echo project-root-1", "RUN echo project-root-2"},
	}}
	result := Merge(global, project)

	// CustomStepOrigins must match CustomSteps 1:1, in the same order:
	// global entries first, then project entries.
	if len(result.Build.CustomStepOrigins) != len(result.Build.CustomSteps) {
		t.Fatalf("expected %d custom step origins, got %d",
			len(result.Build.CustomSteps), len(result.Build.CustomStepOrigins))
	}
	wantCustomOrigins := []StepOrigin{OriginGlobal, OriginGlobal, OriginProject}
	for i, want := range wantCustomOrigins {
		if result.CustomStepOrigin(i) != want {
			t.Errorf("CustomStepOrigin(%d) = %q, want %q", i, result.CustomStepOrigin(i), want)
		}
	}

	// RootStepOrigins must match RootSteps 1:1, in the same order.
	if len(result.Build.RootStepOrigins) != len(result.Build.RootSteps) {
		t.Fatalf("expected %d root step origins, got %d",
			len(result.Build.RootSteps), len(result.Build.RootStepOrigins))
	}
	wantRootOrigins := []StepOrigin{OriginGlobal, OriginProject, OriginProject}
	for i, want := range wantRootOrigins {
		if result.RootStepOrigin(i) != want {
			t.Errorf("RootStepOrigin(%d) = %q, want %q", i, result.RootStepOrigin(i), want)
		}
	}
}

func TestStepOriginDefaultsToProjectWhenUnpopulated(t *testing.T) {
	// Configs built by LoadProjectOnly, or directly in tests, never go
	// through Merge and won't have origins populated. The accessors must
	// default to OriginProject rather than panic or silently misresolve.
	cfg := &Config{Build: BuildConfig{
		CustomSteps: []string{"COPY a /dst", "COPY b /dst"},
		RootSteps:   []string{"COPY c /dst"},
	}}
	if got := cfg.CustomStepOrigin(0); got != OriginProject {
		t.Errorf("CustomStepOrigin(0) = %q, want %q", got, OriginProject)
	}
	if got := cfg.CustomStepOrigin(1); got != OriginProject {
		t.Errorf("CustomStepOrigin(1) = %q, want %q", got, OriginProject)
	}
	if got := cfg.RootStepOrigin(0); got != OriginProject {
		t.Errorf("RootStepOrigin(0) = %q, want %q", got, OriginProject)
	}
	// Out-of-range indices must not panic.
	if got := cfg.CustomStepOrigin(99); got != OriginProject {
		t.Errorf("CustomStepOrigin(99) = %q, want %q", got, OriginProject)
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

func TestMergePassthroughVars(t *testing.T) {
	global := &Config{PassthroughVars: []string{"A", "B"}}
	project := &Config{PassthroughVars: []string{"B", "C"}}
	result := Merge(global, project)

	want := []string{"A", "B", "C"}
	if len(result.PassthroughVars) != len(want) {
		t.Fatalf("expected %d passthrough_vars, got %d (%v)", len(want), len(result.PassthroughVars), result.PassthroughVars)
	}
	for i, name := range want {
		if result.PassthroughVars[i] != name {
			t.Errorf("at index %d: expected %q, got %q", i, name, result.PassthroughVars[i])
		}
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

	t.Run("valid runtime apple", func(t *testing.T) {
		cfg := &Config{Agent: "claude", Runtime: RuntimeApple}
		if err := Validate(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty runtime is valid", func(t *testing.T) {
		cfg := &Config{Agent: "claude"}
		if err := Validate(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid runtime", func(t *testing.T) {
		cfg := &Config{Agent: "claude", Runtime: "podman"}
		err := Validate(cfg)
		if err == nil {
			t.Fatal("expected error for invalid runtime")
		}
		if !strings.Contains(err.Error(), "invalid runtime") {
			t.Errorf("expected invalid runtime error message, got %q", err.Error())
		}
	})

	t.Run("valid memory", func(t *testing.T) {
		for _, m := range []string{"4g", "4G", "4gb", "8192m", "512M", "1048576k"} {
			cfg := &Config{Agent: "claude", Memory: m}
			if err := Validate(cfg); err != nil {
				t.Errorf("memory %q: unexpected error: %v", m, err)
			}
		}
	})

	t.Run("invalid memory", func(t *testing.T) {
		for _, m := range []string{"4", "four gigs", "g4", "4t", "-4g", "4 g", "4g "} {
			cfg := &Config{Agent: "claude", Memory: m}
			err := Validate(cfg)
			if err == nil {
				t.Errorf("memory %q: expected error", m)
				continue
			}
			if !strings.Contains(err.Error(), "invalid memory") {
				t.Errorf("memory %q: expected invalid memory error message, got %q", m, err.Error())
			}
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

	t.Run("invalid root step", func(t *testing.T) {
		cfg := &Config{
			Agent: "claude",
			Build: BuildConfig{RootSteps: []string{"ENV FOO=bar"}},
		}
		err := Validate(cfg)
		if err == nil {
			t.Fatal("expected error for invalid root step (ENV)")
		}
		if !strings.Contains(err.Error(), "single-layer squash") {
			t.Errorf("expected squash error message, got %q", err.Error())
		}
	})

	t.Run("valid root steps", func(t *testing.T) {
		cfg := &Config{
			Agent: "claude",
			Build: BuildConfig{RootSteps: []string{
				"RUN curl -fsSL https://pki.acme.example/root-ca.crt -o /tmp/ca.crt && openssl x509 -in /tmp/ca.crt -noout -fingerprint -sha256 | grep -q 'SHA256 Fingerprint=AA:BB' && mv /tmp/ca.crt /usr/local/share/ca-certificates/internal-ca.crt && update-ca-certificates",
			}},
		}
		if err := Validate(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
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

	t.Run("valid passthrough_vars", func(t *testing.T) {
		cfg := &Config{
			Agent:           "claude",
			PassthroughVars: []string{"GITHUB_TOKEN", "_FOO", "BAR_BAZ_1"},
		}
		if err := Validate(cfg); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("passthrough_vars starting with digit", func(t *testing.T) {
		cfg := &Config{
			Agent:           "claude",
			PassthroughVars: []string{"1BAD"},
		}
		if err := Validate(cfg); err == nil {
			t.Error("expected error for name starting with digit")
		}
	})

	t.Run("passthrough_vars containing space", func(t *testing.T) {
		cfg := &Config{
			Agent:           "claude",
			PassthroughVars: []string{"HAS SPACE"},
		}
		if err := Validate(cfg); err == nil {
			t.Error("expected error for name with space")
		}
	})

	t.Run("duplicate passthrough_vars", func(t *testing.T) {
		cfg := &Config{
			Agent:           "claude",
			PassthroughVars: []string{"FOO", "FOO"},
		}
		if err := Validate(cfg); err == nil {
			t.Error("expected error for duplicate entries")
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
		Agent:   "antigravity",
		Runtime: RuntimeApple,
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
	if loaded.Runtime != cfg.Runtime {
		t.Errorf("expected runtime %q, got %q", cfg.Runtime, loaded.Runtime)
	}
	if len(loaded.Tools) != len(cfg.Tools) {
		t.Errorf("expected %d tools, got %d", len(cfg.Tools), len(loaded.Tools))
	}
	if loaded.Environment["FOO"] != "bar" {
		t.Errorf("expected FOO='bar', got %q", loaded.Environment["FOO"])
	}
}

func TestSaveOmitsEmptyRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.toml")

	cfg := &Config{Agent: "claude"}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	if strings.Contains(string(data), "runtime") {
		t.Errorf("saved config should omit empty runtime, got:\n%s", string(data))
	}
}

func TestSaveOmitsStepOrigins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.toml")

	global := &Config{Build: BuildConfig{CustomSteps: []string{"RUN echo global"}}}
	project := &Config{Build: BuildConfig{
		CustomSteps: []string{"RUN echo project"},
		RootSteps:   []string{"COPY a /dst"},
	}}
	cfg := Merge(global, project)
	// Sanity check: origins are actually populated before we assert they
	// don't leak into the saved file.
	if len(cfg.Build.CustomStepOrigins) == 0 || len(cfg.Build.RootStepOrigins) == 0 {
		t.Fatal("test setup error: expected Merge to populate step origins")
	}

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	written := string(data)

	for _, key := range []string{"CustomStepOrigins", "RootStepOrigins", "custom_step_origins", "root_step_origins", "origin"} {
		if strings.Contains(strings.ToLower(written), strings.ToLower(key)) {
			t.Errorf("saved config should not mention %q, got:\n%s", key, written)
		}
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
