package secrets

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/skwashd/ccodolo/internal/config"
)

// fakeProvider returns canned variables, or err.
type fakeProvider struct {
	vars map[string][]Var // by ref
	err  error
}

func (f fakeProvider) Fetch(_ context.Context, ref string) ([]Var, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vars[ref], nil
}

// useProviders swaps the registry and the known-provider table for the
// test, so it behaves the same whether or not a real provider is compiled
// in. Every registered name is also made known.
func useProviders(t *testing.T, known map[string]string, registered map[string]Provider) {
	t.Helper()
	oldKnown, oldRegistry := knownProviders, registry
	t.Cleanup(func() { knownProviders, registry = oldKnown, oldRegistry })

	knownProviders = map[string]string{}
	for name, build := range known {
		knownProviders[name] = build
	}
	registry = map[string]Provider{}
	for name, p := range registered {
		if _, ok := knownProviders[name]; !ok {
			knownProviders[name] = "ccodolo-" + name
		}
		registry[name] = p
	}
}

func TestParseSpec(t *testing.T) {
	tests := []struct {
		spec    string
		want    Source
		wantErr string
	}{
		{spec: "1password:acct/env", want: Source{"1password", "acct/env", ""}},
		{spec: "1password:env", want: Source{"1password", "env", ""}},
		{spec: "vault:https://vault.example:8200/kv", want: Source{"vault", "https://vault.example:8200/kv", ""}},
		{spec: "1password", wantErr: "expected <provider>:<ref>"},
		{spec: "", wantErr: "expected <provider>:<ref>"},
		{spec: ":env", wantErr: "the provider is empty"},
		{spec: "1password:", wantErr: "the ref is empty"},
		{spec: "1password:  ", wantErr: "the ref is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ParseSpec(tt.spec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseSpec = %+v, want %+v", got, tt.want)
			}
			if got.String() != tt.spec {
				t.Errorf("String() = %q, want %q", got.String(), tt.spec)
			}
		})
	}
}

func TestParseSpecs(t *testing.T) {
	got, err := ParseSpecs([]string{"1password:a/b", "1password:c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Source{{"1password", "a/b", ""}, {"1password", "c", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSpecs = %+v, want %+v", got, want)
	}
	if _, err := ParseSpecs([]string{"1password:a", "nocolon"}); err == nil {
		t.Error("expected an error for a spec with no colon")
	}
}

func TestLookup(t *testing.T) {
	fake := fakeProvider{}
	useProviders(t,
		map[string]string{"1password": "ccodolo-1password", "fake": "ccodolo-fake"},
		map[string]Provider{"fake": fake})

	t.Run("compiled in", func(t *testing.T) {
		p, err := Lookup("fake")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := p.(fakeProvider); !ok {
			t.Errorf("Lookup returned %T, want fakeProvider", p)
		}
	})

	t.Run("known but not compiled in", func(t *testing.T) {
		_, err := Lookup("1password")
		want := "this build of ccodolo has no 1password support; install the ccodolo-1password build"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		_, err := Lookup("vault")
		want := `unknown secrets provider "vault", must be one of: [1password fake]`
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})

	if got, want := Compiled(), []string{"fake"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Compiled() = %v, want %v", got, want)
	}
}

func TestCompiledEmptyWithNoProviders(t *testing.T) {
	useProviders(t, map[string]string{"1password": "ccodolo-1password"}, nil)
	if got := Compiled(); len(got) != 0 {
		t.Errorf("Compiled() = %v, want none", got)
	}
}

func TestRegisterPanics(t *testing.T) {
	useProviders(t, map[string]string{"fake": "ccodolo-fake"}, nil)

	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected a panic", name)
			}
		}()
		fn()
	}
	mustPanic("unknown name", func() { Register("nope", fakeProvider{}) })
	Register("fake", fakeProvider{})
	mustPanic("duplicate", func() { Register("fake", fakeProvider{}) })
}

// config.Validate accepts a provider key by name without importing this
// package, so the two lists have to agree.
func TestKnownMatchesConfig(t *testing.T) {
	want := slices.Clone(config.SecretProviderNames())
	slices.Sort(want)
	if got := Known(); !reflect.DeepEqual(got, want) {
		t.Errorf("Known() = %v, config.SecretProviderNames() = %v", got, want)
	}
}

func TestSourcesFromConfig(t *testing.T) {
	cfg := config.Merge(
		&config.Config{Secrets: map[string]config.SecretRefs{"1password": {"shared"}, "vault": {"z"}}},
		&config.Config{Secrets: map[string]config.SecretRefs{"1password": {"project"}, "off": {}}},
	)
	got := SourcesFromConfig(cfg)
	want := []Source{
		{"1password", "shared", config.OriginGlobal},
		{"1password", "project", config.OriginProject},
		{"vault", "z", config.OriginGlobal},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SourcesFromConfig = %+v, want %+v", got, want)
	}

	// A config that never went through Merge has no origins: every ref is
	// a project ref.
	unmerged := &config.Config{Secrets: map[string]config.SecretRefs{"1password": {"a"}}}
	if got, want := SourcesFromConfig(unmerged), []Source{{"1password", "a", config.OriginProject}}; !reflect.DeepEqual(got, want) {
		t.Errorf("SourcesFromConfig(unmerged) = %+v, want %+v", got, want)
	}
	if got := SourcesFromConfig(&config.Config{}); len(got) != 0 {
		t.Errorf("SourcesFromConfig(empty) = %+v, want none", got)
	}
}
