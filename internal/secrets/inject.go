package secrets

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"

	"github.com/skwashd/ccodolo/internal/config"
)

// reservedPrefix marks names ccodolo sets itself (CCODOLO_TMUX).
const reservedPrefix = "CCODOLO_"

// The runtime CLI has to see every secret in its own environment, so a
// secret must not be a name that changes how that CLI itself runs. The
// lists name variables rather than whole prefixes where they can, so that
// a harmless secret such as DOCKER_PASSWORD is not caught.
//
// OTEL_* is deliberately absent. The docker CLI does export its own traces
// when OTEL_EXPORTER_OTLP_ENDPOINT is set, but agents read the same names
// for their telemetry, and sending the CLI's traces to that collector too
// does no harm.
var deniedNames = slices.Concat(
	// How the OS finds programs, and where the CLI keeps its files.
	[]string{"PATH", "HOME", "TMPDIR", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR"},
	// The same on Windows, where docker.exe also reads these.
	[]string{"PATHEXT", "COMSPEC", "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "TMP", "TEMP",
		"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA"},
	// How the CLI reaches the daemon: proxies (Go reads the lower-case
	// forms too, on every host), TLS roots, and the ssh agent for an
	// ssh:// DOCKER_HOST.
	[]string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY",
		"http_proxy", "https_proxy", "no_proxy", "all_proxy",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "SSH_AUTH_SOCK", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE"},
	// docker CLI settings. DOCKER_CLI_* is a prefix below.
	[]string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_CERT_PATH",
		"DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_API_VERSION", "DOCKER_DEFAULT_PLATFORM",
		"DOCKER_CONTENT_TRUST", "DOCKER_CONTENT_TRUST_SERVER", "DOCKER_CUSTOM_HEADERS",
		"DOCKER_HIDE_LEGACY_COMMANDS", "DOCKER_BUILDKIT"},
	// The Go runtime, which the docker CLI is built on.
	[]string{"GODEBUG", "GOGC", "GOMAXPROCS", "GOMEMLIMIT", "GOTRACEBACK"},
)

// deniedPrefixes are families of names that change how the CLI runs: the
// dynamic loader on Linux and macOS, and the docker CLI's own settings and
// plugins.
var deniedPrefixes = []string{"LD_", "DYLD_", "DOCKER_CLI_"}

// Resolve fetches every source in order and returns the variables with
// Source set. Any failure aborts: a launch never goes ahead with some of
// its secrets missing.
//
// Each Fetch runs in its own goroutine and Resolve waits on it and on
// ctx.Done together, so a timeout or Ctrl+C ends the wait even if the
// provider call (for 1Password, a cgo call into the desktop app's library)
// does not honour the context. The caller exits straight after, so the
// abandoned goroutine does not matter.
func Resolve(ctx context.Context, sources []Source) ([]Var, error) {
	var all []Var
	for _, src := range sources {
		p, err := Lookup(src.Provider)
		if err != nil {
			return nil, err
		}

		type result struct {
			vars []Var
			err  error
		}
		// Buffered, so an abandoned Fetch can still send and finish.
		done := make(chan result, 1)
		go func() {
			vars, err := p.Fetch(ctx, src.Ref)
			done <- result{vars, err}
		}()

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("fetching secrets from %s: timed out; if the provider showed an approval prompt, it was not answered in time", src)
			}
			return nil, fmt.Errorf("fetching secrets from %s: cancelled", src)
		case r := <-done:
			if r.err != nil {
				return nil, fmt.Errorf("fetching secrets from %s: %w", src, r.err)
			}
			for _, v := range r.vars {
				all = append(all, Var{Name: v.Name, Value: v.Value, Source: src.String(), Origin: src.Origin})
			}
		}
	}
	return all, nil
}

// CheckConflicts returns the secrets to inject, and reports every one that
// cannot be: a name that is not a valid variable name, is reserved, or
// would change how the runtime CLI runs; a name set by two sources in the
// same config file; and a name that [environment] or passthrough_vars
// already sets. A name set by both a project and a global source is not a
// problem: the project secret wins and the global one is dropped. The error
// has one line per problem and never contains a value.
func CheckConflicts(vars []Var, cfg *config.Config) ([]Var, error) {
	return checkConflicts(vars, cfg, runtime.GOOS == "windows")
}

// checkConflicts is CheckConflicts with the host's case rule passed in.
// Environment variable names are case-insensitive on Windows, so names
// that differ only in case are the same variable in the environment handed
// to the runtime CLI. [environment] entries travel in argv instead, where
// case always counts.
func checkConflicts(vars []Var, cfg *config.Config, foldCase bool) ([]Var, error) {
	key := func(name string) string {
		if foldCase {
			return strings.ToUpper(name)
		}
		return name
	}

	passthrough := make(map[string]string, len(cfg.PassthroughVars))
	for _, name := range cfg.PassthroughVars {
		passthrough[key(name)] = name
	}

	// Names a project source sets hide the same name from a global source.
	fromProject := make(map[string]bool)
	for _, v := range vars {
		if v.Origin == config.OriginProject {
			fromProject[key(v.Name)] = true
		}
	}

	var kept []Var
	var problems []string
	firstSource := make(map[string]string)
	for _, v := range vars {
		if v.Origin == config.OriginGlobal && fromProject[key(v.Name)] {
			continue
		}
		if reason := nameProblem(v.Name, foldCase); reason != "" {
			problems = append(problems, fmt.Sprintf("secret %q from %s: %s", v.Name, v.Source, reason))
			continue
		}
		if prev, dup := firstSource[key(v.Name)]; dup {
			if prev == v.Source {
				problems = append(problems, fmt.Sprintf("secret %q is set twice by %s", v.Name, v.Source))
			} else {
				problems = append(problems, fmt.Sprintf("secret %q from %s is also set by %s", v.Name, v.Source, prev))
			}
			continue
		}
		firstSource[key(v.Name)] = v.Source

		if _, ok := cfg.Environment[v.Name]; ok {
			problems = append(problems, fmt.Sprintf("secret %q from %s conflicts with [environment] in the %s config",
				v.Name, v.Source, cfg.EnvironmentOrigin(v.Name)))
		}
		if _, ok := passthrough[key(v.Name)]; ok {
			problems = append(problems, fmt.Sprintf("secret %q from %s conflicts with passthrough_vars", v.Name, v.Source))
		}
		kept = append(kept, v)
	}

	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "\n"))
	}
	return kept, nil
}

// nameProblem returns why name cannot be used for a secret, or "".
func nameProblem(name string, foldCase bool) string {
	if !config.ValidEnvVarName(name) {
		return "not a valid environment variable name (must match [A-Za-z_][A-Za-z0-9_]*)"
	}
	cmp := name
	if foldCase {
		cmp = strings.ToUpper(name)
	}
	if strings.HasPrefix(cmp, reservedPrefix) {
		return "names starting with " + reservedPrefix + " are reserved for ccodolo"
	}
	denied := slices.Contains(deniedNames, cmp)
	for _, prefix := range deniedPrefixes {
		denied = denied || strings.HasPrefix(cmp, prefix)
	}
	if denied {
		return "this name would change how the container runtime CLI itself runs; rename it in the provider, or set it inside the container with [environment]"
	}
	return ""
}

// Names returns the variable names in order.
func Names(vars []Var) []string {
	names := make([]string, len(vars))
	for i, v := range vars {
		names[i] = v.Name
	}
	return names
}
