package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/docker"
	"github.com/skwashd/ccodolo/internal/secrets"
)

// launchRecorder stands in for the container runtime and the secrets
// providers, and records what runRoot did with them, in order.
type launchRecorder struct {
	calls []string // "lookup:<provider>", "exec", "build", "resolve", "run"

	lookupErr  error
	resolveErr error
	resolved   []secrets.Var // returned by resolve

	resolveSources []secrets.Source // what resolve was asked for
	runVars        []secrets.Var    // what run was given
}

// setupLaunch points HOME at a temp dir holding an existing project named
// "demo" with the given global and project ccodolo.toml contents, swaps the
// runRoot seams for a recorder, and resets every flag after the test. It
// returns the recorder and the project directory.
func setupLaunch(t *testing.T, globalToml, projectToml string) (*launchRecorder, string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows

	projectPath, err := config.ProjectPath("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if globalToml != "" {
		if err := os.WriteFile(globalPath, []byte(globalToml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.ProjectConfigPath(projectPath), []byte(projectToml), 0o644); err != nil {
		t.Fatal(err)
	}

	// Flags are package globals, so put every one back.
	saved := struct {
		project, workdir, agent, tools                         string
		createNew, exec, rebuild, buildOnly, reconfigure, dash bool
		secrets                                                []string
		noSecrets                                              bool
	}{
		flagProject, flagWorkdir, flagAgent, flagTools,
		flagCreateNew, flagExec, flagRebuild, flagBuildOnly, flagReconfigure, flagDashboard,
		flagSecrets, flagNoSecrets,
	}
	t.Cleanup(func() {
		flagProject, flagWorkdir, flagAgent, flagTools = saved.project, saved.workdir, saved.agent, saved.tools
		flagCreateNew, flagExec, flagRebuild, flagBuildOnly = saved.createNew, saved.exec, saved.rebuild, saved.buildOnly
		flagReconfigure, flagDashboard = saved.reconfigure, saved.dash
		flagSecrets, flagNoSecrets = saved.secrets, saved.noSecrets
	})
	flagProject, flagWorkdir, flagAgent, flagTools = "demo", t.TempDir(), "", ""
	flagCreateNew, flagExec, flagRebuild, flagBuildOnly = false, false, false, false
	flagReconfigure, flagDashboard = false, false
	flagSecrets, flagNoSecrets = nil, false

	rec := &launchRecorder{}
	oldExec, oldBuild, oldRun := dockerExec, dockerBuild, dockerRun
	oldLookup, oldResolve := secretsLookup, secretsResolve
	t.Cleanup(func() {
		dockerExec, dockerBuild, dockerRun = oldExec, oldBuild, oldRun
		secretsLookup, secretsResolve = oldLookup, oldResolve
	})
	dockerExec = func(docker.Runtime, string, string) error {
		rec.calls = append(rec.calls, "exec")
		return nil
	}
	dockerBuild = func(docker.Runtime, *config.Config, string, string, bool) (string, error) {
		rec.calls = append(rec.calls, "build")
		return "ccodolo:test", nil
	}
	dockerRun = func(_ docker.Runtime, _ *config.Config, _, _, _ string, _ []string, vars []secrets.Var) error {
		rec.calls = append(rec.calls, "run")
		rec.runVars = vars
		return nil
	}
	secretsLookup = func(name string) (secrets.Provider, error) {
		rec.calls = append(rec.calls, "lookup:"+name)
		return nil, rec.lookupErr
	}
	secretsResolve = func(_ context.Context, sources []secrets.Source) ([]secrets.Var, error) {
		rec.calls = append(rec.calls, "resolve")
		rec.resolveSources = sources
		return rec.resolved, rec.resolveErr
	}
	return rec, projectPath
}

const (
	globalWithSecrets = "[secrets]\n1password = \"acct/shared-env\"\n"
	projectClaude     = "agent = \"claude\"\n"
)

// errNotCompiledIn is the error a default build returns for 1password.
var errNotCompiledIn = errors.New("this build of ccodolo has no 1password support; install the ccodolo-1password build")

// A default build can still attach to a running container when the config
// has [secrets]: the container already has its values.
func TestExecIgnoresConfiguredSecrets(t *testing.T) {
	rec, _ := setupLaunch(t, globalWithSecrets, projectClaude)
	rec.lookupErr = errNotCompiledIn
	flagExec = true

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"exec"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
}

// A default build can still build an image when the config has [secrets].
func TestBuildOnlyIgnoresConfiguredSecrets(t *testing.T) {
	rec, _ := setupLaunch(t, globalWithSecrets, projectClaude)
	rec.lookupErr = errNotCompiledIn
	flagBuildOnly = true

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"build"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
}

// A build without the provider fails a launch before the slow image build.
func TestLaunchFailsBeforeBuildWithoutProvider(t *testing.T) {
	rec, _ := setupLaunch(t, globalWithSecrets, projectClaude)
	rec.lookupErr = errNotCompiledIn

	err := runRoot(rootCmd, nil)
	if !errors.Is(err, errNotCompiledIn) {
		t.Fatalf("error = %v, want it to wrap the not-compiled-in error", err)
	}
	if !strings.Contains(err.Error(), "--no-secrets") {
		t.Errorf("error should point at --no-secrets, got: %v", err)
	}
	if want := []string{"lookup:1password"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
}

func TestLaunchWithConfiguredSecrets(t *testing.T) {
	rec, _ := setupLaunch(t, globalWithSecrets, projectClaude)
	rec.resolved = []secrets.Var{{Name: "API_KEY", Value: "v", Source: "1password:acct/shared-env"}}

	if err := runRoot(rootCmd, []string{"--resume"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The provider is checked before the build; the fetch comes after it.
	if want := []string{"lookup:1password", "build", "resolve", "run"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
	if want := []secrets.Source{{Provider: "1password", Ref: "acct/shared-env", Origin: config.OriginGlobal}}; !reflect.DeepEqual(rec.resolveSources, want) {
		t.Errorf("resolved sources = %+v, want %+v", rec.resolveSources, want)
	}
	if !reflect.DeepEqual(rec.runVars, rec.resolved) {
		t.Errorf("run got %+v, want %+v", rec.runVars, rec.resolved)
	}
}

// A project list adds to the global one, and an empty list turns it off.
func TestLaunchProjectSecretsAddToGlobal(t *testing.T) {
	t.Run("add", func(t *testing.T) {
		rec, _ := setupLaunch(t, globalWithSecrets, projectClaude+"[secrets]\n1password = [\"acct/a\", \"b\"]\n")
		if err := runRoot(rootCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []secrets.Source{
			{Provider: "1password", Ref: "acct/shared-env", Origin: config.OriginGlobal},
			{Provider: "1password", Ref: "acct/a", Origin: config.OriginProject},
			{Provider: "1password", Ref: "b", Origin: config.OriginProject},
		}
		if !reflect.DeepEqual(rec.resolveSources, want) {
			t.Errorf("resolved sources = %+v, want %+v", rec.resolveSources, want)
		}
	})

	t.Run("empty list turns it off", func(t *testing.T) {
		rec, _ := setupLaunch(t, globalWithSecrets, projectClaude+"[secrets]\n1password = []\n")
		rec.lookupErr = errNotCompiledIn
		if err := runRoot(rootCmd, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := []string{"build", "run"}; !reflect.DeepEqual(rec.calls, want) {
			t.Errorf("calls = %v, want %v", rec.calls, want)
		}
	})
}

func TestNoSecretsFlagSkipsConfiguredSecrets(t *testing.T) {
	rec, _ := setupLaunch(t, globalWithSecrets, projectClaude)
	rec.lookupErr = errNotCompiledIn
	flagNoSecrets = true

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"build", "run"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
	if len(rec.runVars) != 0 {
		t.Errorf("run got secrets with --no-secrets: %+v", rec.runVars)
	}
}

// --secrets replaces the configured sources for one run and is never
// written to ccodolo.toml.
func TestSecretsFlagReplacesConfigAndIsNotSaved(t *testing.T) {
	rec, projectPath := setupLaunch(t, globalWithSecrets, projectClaude)
	rec.resolved = []secrets.Var{{Name: "API_KEY", Value: "v", Source: "1password:acct/flag-env"}}
	flagSecrets = []string{"1password:acct/flag-env", "1password:other"}

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"lookup:1password", "lookup:1password", "build", "resolve", "run"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
	want := []secrets.Source{{Provider: "1password", Ref: "acct/flag-env"}, {Provider: "1password", Ref: "other"}}
	if !reflect.DeepEqual(rec.resolveSources, want) {
		t.Errorf("resolved sources = %+v, want %+v", rec.resolveSources, want)
	}

	data, err := os.ReadFile(config.ProjectConfigPath(projectPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != projectClaude {
		t.Errorf("project config changed:\n%s", data)
	}
}

// A provider missing from this build is reported as is for --secrets: the
// --no-secrets hint is only for sources that came from config.
func TestSecretsFlagWithoutProvider(t *testing.T) {
	rec, _ := setupLaunch(t, "", projectClaude)
	rec.lookupErr = errNotCompiledIn
	flagSecrets = []string{"1password:acct/env"}

	err := runRoot(rootCmd, nil)
	if err == nil || err.Error() != errNotCompiledIn.Error() {
		t.Errorf("error = %v, want %q", err, errNotCompiledIn)
	}
	if want := []string{"lookup:1password"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
}

func TestSecretsFlagCombinations(t *testing.T) {
	tests := []struct {
		name    string
		set     func()
		wantErr string
	}{
		{"with --exec", func() { flagExec = true }, "--secrets cannot be used with --exec"},
		{"with --build-only", func() { flagBuildOnly = true }, "--secrets cannot be used with --build-only"},
		{"with --no-secrets", func() { flagNoSecrets = true }, "--secrets and --no-secrets are mutually exclusive"},
		{"missing colon", func() { flagSecrets = []string{"1password"} }, "expected <provider>:<ref>"},
		{"empty ref", func() { flagSecrets = []string{"1password:"} }, "the ref is empty"},
		{"duplicate spec", func() { flagSecrets = []string{"1password:acct/env", "1password:acct/env"} }, `duplicate secrets spec "1password:acct/env"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, _ := setupLaunch(t, "", projectClaude)
			flagSecrets = []string{"1password:acct/env"}
			tt.set()

			err := runRoot(rootCmd, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want one containing %q", err, tt.wantErr)
			}
			if len(rec.calls) != 0 {
				t.Errorf("nothing should run, got calls %v", rec.calls)
			}
		})
	}
}

func TestFetchFailureAbortsLaunch(t *testing.T) {
	rec, _ := setupLaunch(t, globalWithSecrets, projectClaude)
	rec.resolveErr = errors.New("fetching secrets from 1password:acct/shared-env: vault is locked")

	err := runRoot(rootCmd, nil)
	if !errors.Is(err, rec.resolveErr) {
		t.Fatalf("error = %v, want %v", err, rec.resolveErr)
	}
	if want := []string{"lookup:1password", "build", "resolve"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v (no run)", rec.calls, want)
	}
}

func TestSecretConflictAbortsLaunch(t *testing.T) {
	rec, _ := setupLaunch(t,
		globalWithSecrets+"\n[environment]\nAPI_KEY = \"from-global\"\n",
		"agent = \"claude\"\npassthrough_vars = [\"GITHUB_TOKEN\"]\n")
	rec.resolved = []secrets.Var{
		{Name: "API_KEY", Value: "hunter2", Source: "1password:acct/shared-env"},
		{Name: "GITHUB_TOKEN", Value: "hunter2", Source: "1password:acct/shared-env"},
	}

	err := runRoot(rootCmd, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{
		`secret "API_KEY" from 1password:acct/shared-env conflicts with [environment] in the global config`,
		`secret "GITHUB_TOKEN" from 1password:acct/shared-env conflicts with passthrough_vars`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks a secret value: %v", err)
	}
	if want := []string{"lookup:1password", "build", "resolve"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v (no run)", rec.calls, want)
	}
}

func TestLaunchWithoutSecretsTouchesNoProvider(t *testing.T) {
	rec, _ := setupLaunch(t, "", projectClaude)
	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"build", "run"}; !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
	if rec.runVars != nil {
		t.Errorf("run got %+v, want nil", rec.runVars)
	}
}

func TestSecretsFlagsRegistered(t *testing.T) {
	f := rootCmd.Flags().Lookup("secrets")
	if f == nil || f.Value.Type() != "stringArray" {
		t.Errorf("--secrets should be a repeatable stringArray flag, got %v", f)
	}
	if f := rootCmd.Flags().Lookup("no-secrets"); f == nil || f.Value.Type() != "bool" {
		t.Errorf("--no-secrets should be a bool flag, got %v", f)
	}
}

func TestPrintVersionListsSecretsProviders(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf)

	want := "secrets: none\n"
	if compiled := secrets.Compiled(); len(compiled) > 0 {
		want = "secrets: " + strings.Join(compiled, ", ") + "\n"
	}
	lines := strings.SplitAfter(buf.String(), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "ccodolo ") || lines[1] != want {
		t.Errorf("version output = %q, want a ccodolo line then %q", buf.String(), want)
	}
}

// The project directory must exist for these tests: a missing one makes
// runRoot prompt on stdin.
func TestSetupLaunchCreatesProject(t *testing.T) {
	_, projectPath := setupLaunch(t, "", projectClaude)
	if _, err := os.Stat(filepath.Join(projectPath, "ccodolo.toml")); err != nil {
		t.Fatal(err)
	}
}

// A build without the provider fails before a new project is created, so
// it leaves nothing behind.
func TestNewProjectFailsBeforeCreateWithoutProvider(t *testing.T) {
	for _, tt := range []struct {
		name, global string
		flag         []string
	}{
		{"--secrets", "", []string{"1password:acct/env"}},
		{"[secrets] in global config", globalWithSecrets, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec, projectPath := setupLaunch(t, tt.global, projectClaude)
			if err := os.RemoveAll(projectPath); err != nil {
				t.Fatal(err)
			}
			rec.lookupErr = errNotCompiledIn
			flagCreateNew, flagTools, flagSecrets = true, "python", tt.flag

			if err := runRoot(rootCmd, nil); !errors.Is(err, errNotCompiledIn) {
				t.Fatalf("error = %v, want %v", err, errNotCompiledIn)
			}
			if _, err := os.Stat(projectPath); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("project directory was created (stat error: %v)", err)
			}
		})
	}
}

// A new project's ccodolo.toml holds only its own settings. Copying the
// merged config would pin [secrets] and the rest to the global values of
// the day, and run global build steps twice.
func TestNewProjectSavesOnlyProjectSettings(t *testing.T) {
	global := globalWithSecrets + "\n[tools]\npython = \"\"\n\n[build]\ncustom_steps = [\"RUN echo global\"]\n"
	_, projectPath := setupLaunch(t, global, projectClaude)
	if err := os.RemoveAll(projectPath); err != nil {
		t.Fatal(err)
	}
	flagCreateNew, flagTools = true, "python,uv"

	if err := runRoot(rootCmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	saved, err := config.LoadProjectOnly(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	want := &config.Config{Agent: "claude", Tools: map[string]string{"uv": ""}}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("saved project config = %+v, want %+v", saved, want)
	}
}

// A config file that does not parse stops the launch rather than being
// skipped.
func TestInvalidGlobalConfigAbortsLaunch(t *testing.T) {
	rec, _ := setupLaunch(t, "[secrets]\n1password = 1\n", projectClaude)

	err := runRoot(rootCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "secrets value must be a string or an array of strings") {
		t.Errorf("error = %v, want a [secrets] type error", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing should run, got calls %v", rec.calls)
	}
}
