package secrets

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skwashd/ccodolo/internal/config"
)

// blockingProvider never returns and ignores its context, like a cgo call
// that is waiting on an approval prompt.
type blockingProvider struct{ release chan struct{} }

func (b blockingProvider) Fetch(context.Context, string) ([]Var, error) {
	<-b.release
	return nil, nil
}

func TestResolve(t *testing.T) {
	useProviders(t, nil, map[string]Provider{
		"fake": fakeProvider{vars: map[string][]Var{
			"one": {{Name: "A", Value: "1"}, {Name: "B", Value: ""}},
			"two": {{Name: "C", Value: "3"}},
		}},
	})

	got, err := Resolve(context.Background(), []Source{{"fake", "one", ""}, {"fake", "two", config.OriginGlobal}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Var{
		{Name: "A", Value: "1", Source: "fake:one"},
		{Name: "B", Value: "", Source: "fake:one"},
		{Name: "C", Value: "3", Source: "fake:two", Origin: config.OriginGlobal},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve = %+v, want %+v", got, want)
	}
	if names := Names(got); !reflect.DeepEqual(names, []string{"A", "B", "C"}) {
		t.Errorf("Names = %v", names)
	}
}

func TestResolveNoSources(t *testing.T) {
	got, err := Resolve(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Errorf("Resolve(nil) = %v, %v; want no vars and no error", got, err)
	}
}

func TestResolveFetchErrorAborts(t *testing.T) {
	boom := errors.New("vault is locked")
	useProviders(t, nil, map[string]Provider{
		"good": fakeProvider{vars: map[string][]Var{"x": {{Name: "A", Value: "1"}}}},
		"bad":  fakeProvider{err: boom},
	})

	got, err := Resolve(context.Background(), []Source{{"good", "x", ""}, {"bad", "y", ""}})
	if got != nil {
		t.Errorf("expected no vars on failure, got %+v", got)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
	if want := "fetching secrets from bad:y: vault is locked"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestResolveProviderNotCompiledIn(t *testing.T) {
	useProviders(t, map[string]string{"1password": "ccodolo-1password"}, nil)
	_, err := Resolve(context.Background(), []Source{{"1password", "acct/env", ""}})
	if err == nil || !strings.Contains(err.Error(), "install the ccodolo-1password build") {
		t.Errorf("error = %v, want the not-compiled-in message", err)
	}
}

func TestResolveReturnsWhenContextEnds(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	useProviders(t, nil, map[string]Provider{"block": blockingProvider{release}})
	sources := []Source{{"block", "x", ""}}

	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := resolveWithin(t, ctx, sources)
		if err == nil || !strings.Contains(err.Error(), "fetching secrets from block:x: timed out") {
			t.Errorf("error = %v, want a timeout naming the source", err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(20*time.Millisecond, cancel)
		_, err := resolveWithin(t, ctx, sources)
		if want := "fetching secrets from block:x: cancelled"; err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
}

// resolveWithin fails the test if Resolve is still blocked after 5s.
func resolveWithin(t *testing.T, ctx context.Context, sources []Source) ([]Var, error) {
	t.Helper()
	type result struct {
		vars []Var
		err  error
	}
	done := make(chan result, 1)
	go func() {
		vars, err := Resolve(ctx, sources)
		done <- result{vars, err}
	}()
	select {
	case r := <-done:
		return r.vars, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve did not return after its context ended")
		return nil, nil
	}
}

func TestCheckConflicts(t *testing.T) {
	// Merge populates the [environment] origins the errors name.
	cfg := config.Merge(
		&config.Config{
			Environment:     map[string]string{"FROM_GLOBAL": "g", "OVERRIDDEN": "g"},
			PassthroughVars: []string{"GITHUB_TOKEN"},
		},
		&config.Config{
			Environment: map[string]string{"FROM_PROJECT": "p", "OVERRIDDEN": "p"},
		},
	)
	const src = "1password:acct/env"

	tests := []struct {
		name string
		vars []Var
		want string // exact error text; "" means no error
	}{
		{"no clash", []Var{{"API_KEY", "v", src, ""}, {"_x1", "", src, ""}}, ""},
		{"no vars", nil, ""},
		{
			"global environment",
			[]Var{{"FROM_GLOBAL", "v", src, ""}},
			`secret "FROM_GLOBAL" from 1password:acct/env conflicts with [environment] in the global config`,
		},
		{
			"project environment",
			[]Var{{"FROM_PROJECT", "v", src, ""}},
			`secret "FROM_PROJECT" from 1password:acct/env conflicts with [environment] in the project config`,
		},
		{
			"environment key the project overrides",
			[]Var{{"OVERRIDDEN", "v", src, ""}},
			`secret "OVERRIDDEN" from 1password:acct/env conflicts with [environment] in the project config`,
		},
		{
			"passthrough_vars",
			[]Var{{"GITHUB_TOKEN", "v", src, ""}},
			`secret "GITHUB_TOKEN" from 1password:acct/env conflicts with passthrough_vars`,
		},
		{
			"another source",
			[]Var{{"API_KEY", "v", src, ""}, {"API_KEY", "w", "1password:other", ""}},
			`secret "API_KEY" from 1password:other is also set by 1password:acct/env`,
		},
		{
			"same source twice",
			[]Var{{"API_KEY", "v", src, ""}, {"API_KEY", "w", src, ""}},
			`secret "API_KEY" is set twice by 1password:acct/env`,
		},
		{
			"every problem is listed",
			[]Var{{"FROM_GLOBAL", "v", src, ""}, {"OK", "v", src, ""}, {"GITHUB_TOKEN", "v", "1password:other", ""}},
			`secret "FROM_GLOBAL" from 1password:acct/env conflicts with [environment] in the global config` + "\n" +
				`secret "GITHUB_TOKEN" from 1password:other conflicts with passthrough_vars`,
		},
		{
			"invalid name",
			[]Var{{"HAS-DASH", "v", src, ""}},
			`secret "HAS-DASH" from 1password:acct/env: not a valid environment variable name (must match [A-Za-z_][A-Za-z0-9_]*)`,
		},
		{
			"empty name",
			[]Var{{"", "v", src, ""}},
			`secret "" from 1password:acct/env: not a valid environment variable name (must match [A-Za-z_][A-Za-z0-9_]*)`,
		},
		{
			"reserved prefix",
			[]Var{{"CCODOLO_TMUX", "0", src, ""}},
			`secret "CCODOLO_TMUX" from 1password:acct/env: names starting with CCODOLO_ are reserved for ccodolo`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := conflictErr(tt.vars, cfg, false)
			if tt.want == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Errorf("error:\n got: %v\nwant: %s", err, tt.want)
			}
		})
	}
}

func TestCheckConflictsNeverPrintsValues(t *testing.T) {
	cfg := &config.Config{Environment: map[string]string{"A": "1"}, PassthroughVars: []string{"B"}}
	err := conflictErr([]Var{
		{"A", "hunter2", "fake:x", ""}, {"B", "hunter2", "fake:x", ""}, {"PATH", "hunter2", "fake:x", ""},
		{"C", "hunter2", "fake:x", ""}, {"C", "hunter2", "fake:y", ""},
	}, cfg, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks a secret value: %v", err)
	}
}

func TestCheckConflictsDeniedNames(t *testing.T) {
	const reason = "this name would change how the container runtime CLI itself runs; rename it in the provider, or set it inside the container with [environment]"
	cfg := &config.Config{}

	for _, name := range []string{
		"PATH", "HOME", "TMPDIR", "USERPROFILE", "APPDATA", "SYSTEMROOT",
		"HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSH_AUTH_SOCK",
		"DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CONTEXT", "DOCKER_CLI_HINTS", "GODEBUG",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES",
	} {
		for _, foldCase := range []bool{false, true} {
			err := conflictErr([]Var{{name, "v", "fake:x", ""}}, cfg, foldCase)
			want := `secret "` + name + `" from fake:x: ` + reason
			if err == nil || err.Error() != want {
				t.Errorf("%s (foldCase=%v): error = %v, want %q", name, foldCase, err, want)
			}
		}
	}

	// Go reads the lower-case proxy names on every host.
	for _, name := range []string{"http_proxy", "https_proxy", "no_proxy", "all_proxy"} {
		if err := conflictErr([]Var{{name, "v", "fake:x", ""}}, cfg, false); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	// Names that only look close, and secrets the CLI does not read, are fine.
	for _, name := range []string{
		"PATHS", "MY_PATH", "HOMEDIR", "DOCKER", "LDAP_URL", "OLD_PATH",
		"DOCKER_USERNAME", "DOCKER_PASSWORD", "DOCKER_TOKEN", "SSH_PRIVATE_KEY",
		"OTEL_EXPORTER_OTLP_HEADERS", "GOPRIVATE",
	} {
		if err := conflictErr([]Var{{name, "v", "fake:x", ""}}, cfg, false); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}
}

// Windows environment names are case-insensitive, so there a lower-case
// name is still the denied variable; elsewhere it is a different one.
func TestCheckConflictsCaseRule(t *testing.T) {
	cfg := &config.Config{PassthroughVars: []string{"GITHUB_TOKEN"}}

	for _, name := range []string{"Path", "home", "docker_host", "ld_preload", "Dyld_Library_Path", "ccodolo_tmux"} {
		if err := conflictErr([]Var{{name, "v", "fake:x", ""}}, cfg, false); err != nil {
			t.Errorf("%s, case-sensitive host: unexpected error: %v", name, err)
		}
		if err := conflictErr([]Var{{name, "v", "fake:x", ""}}, cfg, true); err == nil {
			t.Errorf("%s, Windows: expected an error", name)
		}
	}

	mixed := []Var{{"github_token", "v", "fake:x", ""}}
	if err := conflictErr(mixed, cfg, false); err != nil {
		t.Errorf("case-sensitive host: unexpected error: %v", err)
	}
	want := `secret "github_token" from fake:x conflicts with passthrough_vars`
	if err := conflictErr(mixed, cfg, true); err == nil || err.Error() != want {
		t.Errorf("Windows: error = %v, want %q", err, want)
	}

	twice := []Var{{"Api_Key", "v", "fake:x", ""}, {"API_KEY", "v", "fake:y", ""}}
	if err := conflictErr(twice, cfg, false); err != nil {
		t.Errorf("case-sensitive host: unexpected error: %v", err)
	}
	want = `secret "API_KEY" from fake:y is also set by fake:x`
	if err := conflictErr(twice, cfg, true); err == nil || err.Error() != want {
		t.Errorf("Windows: error = %v, want %q", err, want)
	}
}

// conflictErr is checkConflicts for tests that only look at the error.
func conflictErr(vars []Var, cfg *config.Config, foldCase bool) error {
	_, err := checkConflicts(vars, cfg, foldCase)
	return err
}

// A project secret overrides a global secret of the same name. Within one
// config file, or for --secrets, two sources setting a name still clash.
func TestCheckConflictsProjectOverridesGlobal(t *testing.T) {
	cfg := &config.Config{}
	global := func(name, value, source string) Var {
		return Var{Name: name, Value: value, Source: source, Origin: config.OriginGlobal}
	}
	project := func(name, value, source string) Var {
		return Var{Name: name, Value: value, Source: source, Origin: config.OriginProject}
	}

	t.Run("project wins", func(t *testing.T) {
		vars := []Var{
			global("API_KEY", "g", "fake:g"), global("ONLY_GLOBAL", "g", "fake:g"),
			project("API_KEY", "p", "fake:p"), project("ONLY_PROJECT", "p", "fake:p"),
		}
		got, err := checkConflicts(vars, cfg, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []Var{vars[1], vars[2], vars[3]}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("kept = %+v, want %+v", got, want)
		}
	})

	t.Run("case folds on Windows", func(t *testing.T) {
		vars := []Var{global("api_key", "g", "fake:g"), project("API_KEY", "p", "fake:p")}
		got, err := checkConflicts(vars, cfg, true)
		if err != nil || !reflect.DeepEqual(got, vars[1:]) {
			t.Errorf("kept = %+v, err = %v; want only the project secret", got, err)
		}
	})

	for _, tt := range []struct {
		name string
		vars []Var
		want string
	}{
		{"two global sources", []Var{global("K", "1", "fake:a"), global("K", "2", "fake:b")}, `secret "K" from fake:b is also set by fake:a`},
		{"two project sources", []Var{project("K", "1", "fake:a"), project("K", "2", "fake:b")}, `secret "K" from fake:b is also set by fake:a`},
		{"two --secrets sources", []Var{{"K", "1", "fake:a", ""}, {"K", "2", "fake:b", ""}}, `secret "K" from fake:b is also set by fake:a`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := conflictErr(tt.vars, cfg, false); err == nil || err.Error() != tt.want {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
