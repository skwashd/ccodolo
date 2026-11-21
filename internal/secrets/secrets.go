// Package secrets fetches environment variables from a secrets provider on
// the host, for injection into a container at launch.
//
// Providers are opt-in at build time. Each one lives in a file behind a
// build tag (secrets_<provider>) whose only job is to call Register from
// init. A default build has no providers: the 1Password SDK alone adds
// about 18 MB and needs cgo on macOS and Linux, which is why it is not
// linked into every binary.
package secrets

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/skwashd/ccodolo/internal/config"
)

// Var is one environment variable fetched from a provider.
type Var struct {
	Name  string
	Value string

	// Source is the "<provider>:<ref>" spec the variable came from. Resolve
	// sets it; a Provider leaves it empty.
	Source string

	// Origin is the config file of the source, copied from Source.Origin by
	// Resolve.
	Origin config.StepOrigin
}

// Provider fetches the variables a ref names. The ref is whatever follows
// "<provider>:" in a spec, and each provider defines its own format.
type Provider interface {
	Fetch(ctx context.Context, ref string) ([]Var, error)
}

// Source is one parsed "<provider>:<ref>" spec.
type Source struct {
	Provider string
	Ref      string

	// Origin is the config file that listed the source, or "" for one given
	// with --secrets. A project secret overrides a global one of the same
	// name; see CheckConflicts.
	Origin config.StepOrigin
}

// String returns the spec form, "<provider>:<ref>".
func (s Source) String() string {
	return s.Provider + ":" + s.Ref
}

// knownProviders maps every provider that exists in the codebase, compiled
// in or not, to the release build that carries it. It is what lets Lookup
// tell "install the other build" apart from "no such provider". Its keys
// must match config.SecretProviderNames, which Validate uses.
var knownProviders = map[string]string{
	"1password": "ccodolo-1password",
}

// registry holds the providers compiled into this build.
var registry = map[string]Provider{}

// version is the ccodolo version, for providers that identify their client
// to the service.
var version = "dev"

// SetVersion records the ccodolo version for providers that send one.
func SetVersion(v string) {
	version = v
}

// Version returns the version recorded by SetVersion.
func Version() string {
	return version
}

// Register adds a provider under name. It is called from init in a tagged
// provider file, so a name that is unknown or registered twice is a
// programming error and panics.
func Register(name string, p Provider) {
	if _, ok := knownProviders[name]; !ok {
		panic(fmt.Sprintf("secrets: provider %q is not in knownProviders", name))
	}
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("secrets: provider %q registered twice", name))
	}
	registry[name] = p
}

// Lookup returns the provider registered under name. The error for a
// provider that exists but is not compiled into this build names the build
// that has it.
func Lookup(name string) (Provider, error) {
	if p, ok := registry[name]; ok {
		return p, nil
	}
	if build, ok := knownProviders[name]; ok {
		return nil, fmt.Errorf("this build of ccodolo has no %s support; install the %s build", name, build)
	}
	return nil, fmt.Errorf("unknown secrets provider %q, must be one of: %v", name, Known())
}

// Known returns the name of every provider in the codebase, sorted.
func Known() []string {
	names := make([]string, 0, len(knownProviders))
	for name := range knownProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Compiled returns the names of the providers in this build, sorted.
func Compiled() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ParseSpec parses "<provider>:<ref>". It splits on the first colon, so a
// ref may contain one. The ref is opaque here: each provider parses its own.
func ParseSpec(s string) (Source, error) {
	provider, ref, found := strings.Cut(s, ":")
	if !found {
		return Source{}, fmt.Errorf("invalid secrets spec %q: expected <provider>:<ref>", s)
	}
	if provider == "" {
		return Source{}, fmt.Errorf("invalid secrets spec %q: the provider is empty", s)
	}
	if strings.TrimSpace(ref) == "" {
		return Source{}, fmt.Errorf("invalid secrets spec %q: the ref is empty", s)
	}
	return Source{Provider: provider, Ref: ref}, nil
}

// ParseSpecs parses each spec in order. A spec given twice is an error, so
// the same source is not fetched twice and then reported as a conflict with
// itself.
func ParseSpecs(specs []string) ([]Source, error) {
	sources := make([]Source, 0, len(specs))
	for _, spec := range specs {
		src, err := ParseSpec(spec)
		if err != nil {
			return nil, err
		}
		if slices.Contains(sources, src) {
			return nil, fmt.Errorf("duplicate secrets spec %q", spec)
		}
		sources = append(sources, src)
	}
	return sources, nil
}

// SourcesFromConfig flattens cfg.Secrets into sources, each with the origin
// Merge recorded. Providers go in name order, because a TOML table has none
// of its own; refs keep their merged order.
func SourcesFromConfig(cfg *config.Config) []Source {
	providers := make([]string, 0, len(cfg.Secrets))
	for provider := range cfg.Secrets {
		providers = append(providers, provider)
	}
	slices.Sort(providers)

	var sources []Source
	for _, provider := range providers {
		for i, ref := range cfg.Secrets[provider] {
			sources = append(sources, Source{Provider: provider, Ref: ref, Origin: cfg.SecretOrigin(provider, i)})
		}
	}
	return sources
}
