package docker

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/skwashd/ccodolo/internal/agent"
	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/secrets"
)

// runArgsFixture builds a claude project dir (with the .claude.json extra
// file and .claude-plugin extra dir present) and a config exercising
// volumes, environment, and passthrough vars, then returns the argv
// produced by runArgs for the given runtime. memory sets cfg.Memory
// (empty leaves it unset).
func runArgsFixture(t *testing.T, rt Runtime, memory string) (args []string, projectPath, workdir string) {
	t.Helper()

	a, err := agent.Parse("claude")
	if err != nil {
		t.Fatalf("parsing agent: %v", err)
	}
	meta, err := agent.Get(a)
	if err != nil {
		t.Fatalf("getting agent meta: %v", err)
	}

	projectPath = t.TempDir()
	if err := os.WriteFile(filepath.Join(projectPath, ".claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(projectPath, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	workdir = t.TempDir()

	cfg := &config.Config{
		Agent:  "claude",
		Memory: memory,
		Volumes: []config.Volume{
			{Host: "/host/data", Container: "/data", ReadOnly: true},
		},
		Environment:     map[string]string{"FOO": "bar"},
		PassthroughVars: []string{"CCODOLO_TEST_SET", "CCODOLO_TEST_UNSET_XYZ"},
	}
	t.Setenv("CCODOLO_TEST_SET", "sekrit")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "")
	// Clear so the expectations hold when the tests run inside Windows
	// Terminal, which would otherwise trigger the COLORTERM fallback.
	t.Setenv("WT_SESSION", "")

	args, err = runArgs(rt, cfg, meta, "proj", projectPath, workdir,
		"test-name", "/workspace/proj/wd", "ccodolo:proj-claude-abcd1234",
		nil, []string{"--resume"})
	if err != nil {
		t.Fatalf("runArgs error: %v", err)
	}
	return args, projectPath, workdir
}

// wantMount is the flag pair each backend emits for one bind mount: docker
// takes --mount, the Apple runtime keeps -v.
func wantMount(rt Runtime, src, dst string, readOnly bool) []string {
	if rt == RuntimeApple {
		v := src + ":" + dst
		if readOnly {
			v += ":ro"
		}
		return []string{"-v", v}
	}
	m := "type=bind,source=" + src + ",target=" + dst
	if readOnly {
		m += ",readonly"
	}
	return []string{"--mount", m}
}

// expectedRunArgs is the argv for rt, except the passthrough entry for
// CCODOLO_TEST_SET and the memory flag (omitted when memory is empty),
// which the caller supplies.
func expectedRunArgs(rt Runtime, projectPath, workdir, passthroughSet, memory string) []string {
	args := []string{"run", "--rm", "-it"}
	if rt == RuntimeDocker {
		args = append(args, "--detach-keys", "ctrl-]")
	}
	if memory != "" {
		args = append(args, "--memory", memory)
	}
	args = append(args, "--name", "test-name",
		"--label", "ccodolo.project=proj",
		"--label", "ccodolo.workdir="+workdir,
		"--label", "ccodolo.agent=claude",
		"--label", "ccodolo.tmux=1",
		"-w", "/workspace/proj/wd")
	args = append(args, wantMount(rt, workdir, "/workspace/proj/wd", false)...)
	args = append(args, wantMount(rt, filepath.Join(projectPath, "commandhistory"), "/commandhistory", false)...)
	args = append(args, wantMount(rt, filepath.Join(projectPath, "common"), "/home/coder/project", false)...)
	args = append(args, wantMount(rt, filepath.Join(projectPath, ".claude"), "/home/coder/.claude", false)...)
	args = append(args, wantMount(rt, filepath.Join(projectPath, ".claude.json"), "/home/coder/.claude.json", false)...)
	args = append(args, wantMount(rt, filepath.Join(projectPath, ".claude-plugin"), "/home/coder/.claude-plugin", false)...)
	args = append(args, wantMount(rt, "/host/data", "/data", true)...)
	return append(args,
		"-e", "FOO=bar",
		"-e", passthroughSet,
		"-e", "TERM=xterm-256color",
		"ccodolo:proj-claude-abcd1234",
		"--resume",
	)
}

func TestRunArgsDocker(t *testing.T) {
	args, projectPath, workdir := runArgsFixture(t, RuntimeDocker, "")
	want := expectedRunArgs(RuntimeDocker, projectPath, workdir, "CCODOLO_TEST_SET", "")
	if !reflect.DeepEqual(args, want) {
		t.Errorf("runArgs mismatch:\n got: %q\nwant: %q", args, want)
	}
}

// Both runtimes take a bare -e NAME for passthrough_vars, so the host value
// never appears in argv (and so never in ps).
func TestRunArgsApplePassthroughBareName(t *testing.T) {
	args, projectPath, workdir := runArgsFixture(t, RuntimeApple, "")
	want := expectedRunArgs(RuntimeApple, projectPath, workdir, "CCODOLO_TEST_SET", defaultAppleMemory)
	if !reflect.DeepEqual(args, want) {
		t.Errorf("runArgs mismatch:\n got: %q\nwant: %q", args, want)
	}
}

func TestRunArgsPassthroughValueNotInArgv(t *testing.T) {
	for _, rt := range []Runtime{RuntimeDocker, RuntimeApple} {
		args, _, _ := runArgsFixture(t, rt, "")
		for i, a := range args {
			if strings.Contains(a, "sekrit") {
				t.Errorf("%s: passthrough value leaked into argv (arg %d: %q)", rt, i, a)
			}
		}
	}
}

func TestRunArgsMemory(t *testing.T) {
	t.Run("docker configured", func(t *testing.T) {
		args, projectPath, workdir := runArgsFixture(t, RuntimeDocker, "8g")
		want := expectedRunArgs(RuntimeDocker, projectPath, workdir, "CCODOLO_TEST_SET", "8g")
		if !reflect.DeepEqual(args, want) {
			t.Errorf("runArgs mismatch:\n got: %q\nwant: %q", args, want)
		}
	})

	t.Run("apple configured overrides default", func(t *testing.T) {
		args, projectPath, workdir := runArgsFixture(t, RuntimeApple, "2g")
		want := expectedRunArgs(RuntimeApple, projectPath, workdir, "CCODOLO_TEST_SET", "2g")
		if !reflect.DeepEqual(args, want) {
			t.Errorf("runArgs mismatch:\n got: %q\nwant: %q", args, want)
		}
	})
}

func TestRunArgsWindowsTerminalDefaults(t *testing.T) {
	// Windows Terminal sets WT_SESSION but neither TERM nor COLORTERM.
	t.Setenv("TERM", "")
	t.Setenv("COLORTERM", "")
	t.Setenv("WT_SESSION", "some-guid")

	meta, err := agent.Get(agent.Claude)
	if err != nil {
		t.Fatal(err)
	}
	args, err := runArgs(RuntimeDocker, &config.Config{Agent: "claude"}, meta, "p",
		t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", nil, nil)
	if err != nil {
		t.Fatalf("runArgs error: %v", err)
	}
	for _, want := range []string{"TERM=xterm-256color", "COLORTERM=truecolor"} {
		if !slices.Contains(args, want) {
			t.Errorf("expected -e %s in args, got %q", want, args)
		}
	}

	// An explicit host value still wins over the fallback.
	t.Setenv("TERM", "xterm-ghostty")
	args, err = runArgs(RuntimeDocker, &config.Config{Agent: "claude"}, meta, "p",
		t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", nil, nil)
	if err != nil {
		t.Fatalf("runArgs error: %v", err)
	}
	if !slices.Contains(args, "TERM=xterm-ghostty") || slices.Contains(args, "TERM=xterm-256color") {
		t.Errorf("host TERM should override the Windows Terminal default, got %q", args)
	}
}

func TestMountArg(t *testing.T) {
	for _, tc := range []struct {
		rt       Runtime
		src, dst string
		readOnly bool
		want     []string
	}{
		{RuntimeDocker, `C:\Users\dave\proj`, "/workspace/p/proj", false,
			[]string{"--mount", `type=bind,source=C:\Users\dave\proj,target=/workspace/p/proj`}},
		{RuntimeDocker, "/home/dave/.aws", "/home/coder/.aws", true,
			[]string{"--mount", "type=bind,source=/home/dave/.aws,target=/home/coder/.aws,readonly"}},
		{RuntimeApple, "/Users/dave/.aws", "/home/coder/.aws", true,
			[]string{"-v", "/Users/dave/.aws:/home/coder/.aws:ro"}},
		// -v has no CSV value, so the characters docker rejects are fine here.
		{RuntimeApple, "/Users/dave/src/env=prod, v2", "/workspace/p/proj", false,
			[]string{"-v", "/Users/dave/src/env=prod, v2:/workspace/p/proj"}},
	} {
		got, err := mountArg(tc.rt, tc.src, tc.dst, tc.readOnly)
		if err != nil {
			t.Errorf("mountArg(%v, %q, %q): unexpected error: %v", tc.rt, tc.src, tc.dst, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("mountArg(%v, %q, %q) = %q, want %q", tc.rt, tc.src, tc.dst, got, tc.want)
		}
	}

	for _, tc := range []struct{ src, dst string }{
		{`C:\Users\Doe, John\proj`, "/workspace/p/proj"},
		{"/data", "/mnt/a,b"},
		{`/home/dave/proj"v2`, "/workspace/p/proj"},
	} {
		if _, err := mountArg(RuntimeDocker, tc.src, tc.dst, false); err == nil || !strings.Contains(err.Error(), "comma or double quote") {
			t.Errorf("mountArg(docker, %q, %q): expected comma/quote error, got %v", tc.src, tc.dst, err)
		}
	}
}

func TestCheckMounts(t *testing.T) {
	workdir := t.TempDir()
	projectPath := t.TempDir()
	existing := t.TempDir()

	ok := &config.Config{Volumes: []config.Volume{{Host: existing, Container: "/data"}}}
	if err := CheckMounts(RuntimeDocker, ok, workdir, projectPath); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	missing := &config.Config{Volumes: []config.Volume{{Host: filepath.Join(existing, "nope"), Container: "/data"}}}
	for _, rt := range []Runtime{RuntimeDocker, RuntimeApple} {
		err := CheckMounts(rt, missing, workdir, projectPath)
		if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "nope") {
			t.Errorf("%v: expected missing-path error naming the entry, got %v", rt, err)
		}
	}

	badContainer := &config.Config{Volumes: []config.Volume{{Host: existing, Container: "/mnt/a,b"}}}
	if err := CheckMounts(RuntimeDocker, badContainer, workdir, projectPath); err == nil || !strings.Contains(err.Error(), "/mnt/a,b") {
		t.Errorf("expected comma error naming the container path, got %v", err)
	}
	if err := CheckMounts(RuntimeApple, badContainer, workdir, projectPath); err != nil {
		t.Errorf("apple runtime uses -v, a comma should pass: %v", err)
	}

	if err := CheckMounts(RuntimeDocker, &config.Config{}, "/srv/a,b", projectPath); err == nil || !strings.Contains(err.Error(), "workdir") {
		t.Errorf("expected workdir error, got %v", err)
	}
}

func TestRunArgsRejectsCommaInVolumePath(t *testing.T) {
	meta, err := agent.Get(agent.Claude)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Agent:   "claude",
		Volumes: []config.Volume{{Host: "/srv/a,b", Container: "/data"}},
	}
	_, err = runArgs(RuntimeDocker, cfg, meta, "p", t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "/srv/a,b") {
		t.Errorf("expected error naming the offending path, got %v", err)
	}
}

func TestCredentialMountWarnings(t *testing.T) {
	vols := []config.Volume{
		{Host: "~/.aws", Container: "/home/coder/.aws"},
		{Host: "~/.ssh", Container: "/home/coder/.ssh"},
		{Host: "~/.gnupg", Container: "/home/coder/.gnupg/"},
		{Host: `C:\Users\dave\.ssh\id_ed25519`, Container: "/home/coder/key"}, // single key
		{Host: "~/keys", Container: "/home/coder/.ssh/keys"},                  // renamed on the host side
		{Host: "~/.sshfs", Container: "/home/coder/.gnupgx"},                  // not credential dirs
	}
	got := credentialMountWarnings(vols)
	if len(got) != 4 {
		t.Fatalf("expected 4 warnings, got %d: %q", len(got), got)
	}
	for i, want := range []string{"/home/coder/.ssh", "/home/coder/.gnupg", "/home/coder/key", "/home/coder/.ssh/keys"} {
		if !strings.Contains(got[i], want) {
			t.Errorf("warning %d should name %s, got %q", i, want, got[i])
		}
	}
	if got := credentialMountWarnings(nil); len(got) != 0 {
		t.Errorf("expected no warnings for no volumes, got %q", got)
	}
}

func TestMatchContainer(t *testing.T) {
	entries := []containerEntry{
		{Name: "ccodolo-foo-src-202608301200", ID: "abc123"},
		{Name: "ccodolo-bar-src-202608301201", ID: "def456"},
	}

	t.Run("single match", func(t *testing.T) {
		match, err := matchContainer(entries, "ccodolo-foo-src")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if match.ID != "abc123" {
			t.Errorf("expected ID 'abc123', got %q", match.ID)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, err := matchContainer(entries, "ccodolo-baz-src")
		if err == nil || !strings.Contains(err.Error(), "no containers found") {
			t.Errorf("expected 'no containers found' error, got %v", err)
		}
	})

	t.Run("multiple matches", func(t *testing.T) {
		_, err := matchContainer(entries, "ccodolo-")
		if err == nil || !strings.Contains(err.Error(), "multiple containers found") {
			t.Errorf("expected 'multiple containers found' error, got %v", err)
		}
	})

	t.Run("missing ID", func(t *testing.T) {
		_, err := matchContainer([]containerEntry{{Name: "ccodolo-foo-src-1"}}, "ccodolo-foo-src")
		if err == nil || !strings.Contains(err.Error(), "unable to determine container ID") {
			t.Errorf("expected 'unable to determine container ID' error, got %v", err)
		}
	})
}

func TestParseDockerContainerList(t *testing.T) {
	out := []byte("ccodolo-foo-src-1:abc123\nother:def456\n\n")
	entries := parseDockerContainerList(out)
	want := []containerEntry{
		{Name: "ccodolo-foo-src-1", ID: "abc123"},
		{Name: "other", ID: "def456"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("parseDockerContainerList = %v, want %v", entries, want)
	}

	if entries := parseDockerContainerList([]byte("")); len(entries) != 0 {
		t.Errorf("expected no entries for empty output, got %v", entries)
	}
}

func TestParseAppleContainerList(t *testing.T) {
	out := []byte(`[{"configuration":{"id":"ccodolo-foo-src-1"}},{"configuration":{"id":"other"}}]`)
	entries, err := parseAppleContainerList(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []containerEntry{
		{Name: "ccodolo-foo-src-1", ID: "ccodolo-foo-src-1"},
		{Name: "other", ID: "other"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("parseAppleContainerList = %v, want %v", entries, want)
	}

	if _, err := parseAppleContainerList([]byte("not json")); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestRunArgsTmuxOptOut(t *testing.T) {
	meta, err := agent.Get(agent.Claude)
	if err != nil {
		t.Fatal(err)
	}
	off := false
	for _, rt := range []Runtime{RuntimeDocker, RuntimeApple} {
		args, err := runArgs(rt, &config.Config{Agent: "claude", Tmux: &off}, meta, "p",
			t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", nil, nil)
		if err != nil {
			t.Fatalf("runArgs error: %v", err)
		}
		if !slices.Contains(args, "CCODOLO_TMUX=0") {
			t.Errorf("%s: expected -e CCODOLO_TMUX=0, got %q", rt, args)
		}
		if !slices.Contains(args, "ccodolo.tmux=0") {
			t.Errorf("%s: expected the tmux label to record the opt-out, got %q", rt, args)
		}
		if slices.Contains(args, "--detach-keys") {
			t.Errorf("%s: no detach keys without tmux: detaching would orphan the agent, got %q", rt, args)
		}
	}
}

func TestContainerName(t *testing.T) {
	at := time.Date(2026, 11, 1, 21, 42, 0, 0, time.UTC)

	// Short names are unchanged.
	if got, want := containerName("proj", filepath.Join("src", "app"), at), "ccodolo-proj-app-202611012142"; got != want {
		t.Errorf("containerName = %q, want %q", got, want)
	}

	// The name that failed on the apple runtime: 66 characters uncapped.
	workdir := filepath.Join("Users", "dave", "aws-lambda-runtime-interface-emulator")
	got := containerName("ccodolo", workdir, at)
	if len(got) > maxContainerNameLen {
		t.Errorf("containerName = %q (%d chars), want at most %d", got, len(got), maxContainerNameLen)
	}
	if !strings.HasSuffix(got, "-202611012142") {
		t.Errorf("containerName = %q: the timestamp must survive shortening", got)
	}
	// --exec finds the container by the namespace prefix.
	if ns := containerNamespace("ccodolo", workdir); !strings.HasPrefix(got, ns) {
		t.Errorf("name %q does not start with namespace %q", got, ns)
	}
}

// Secrets reach the container as a bare -e NAME on both runtimes. runArgs
// is only given names, so a value cannot end up in argv.
func TestRunArgsSecrets(t *testing.T) {
	meta, err := agent.Get(agent.Claude)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "")
	t.Setenv("WT_SESSION", "")
	t.Setenv("CCODOLO_TEST_SET", "from-host")

	cfg := &config.Config{
		Agent:           "claude",
		Environment:     map[string]string{"FOO": "bar"},
		PassthroughVars: []string{"CCODOLO_TEST_SET"},
	}
	for _, rt := range []Runtime{RuntimeDocker, RuntimeApple} {
		args, err := runArgs(rt, cfg, meta, "p", t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img",
			[]string{"API_KEY", "DB_PASSWORD"}, []string{"--resume"})
		if err != nil {
			t.Fatalf("%s: runArgs error: %v", rt, err)
		}
		// Everything from the first -e on, in order.
		first := slices.Index(args, "-e")
		if first < 0 {
			t.Fatalf("%s: no -e in args: %q", rt, args)
		}
		want := []string{
			"-e", "FOO=bar",
			"-e", "CCODOLO_TEST_SET",
			"-e", "API_KEY",
			"-e", "DB_PASSWORD",
			"-e", "TERM=xterm-256color",
			"img", "--resume",
		}
		if got := args[first:]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: env args mismatch:\n got: %q\nwant: %q", rt, got, want)
		}
	}
}

// A secret named TERM or COLORTERM wins over the host default, the same as
// an [environment] or passthrough_vars entry does.
func TestRunArgsSecretSkipsTermDefault(t *testing.T) {
	meta, err := agent.Get(agent.Claude)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("WT_SESSION", "")

	for _, rt := range []Runtime{RuntimeDocker, RuntimeApple} {
		args, err := runArgs(rt, &config.Config{Agent: "claude"}, meta, "p",
			t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", []string{"TERM"}, nil)
		if err != nil {
			t.Fatalf("%s: runArgs error: %v", rt, err)
		}
		if slices.Contains(args, "TERM=xterm-256color") {
			t.Errorf("%s: host TERM default emitted although a secret sets TERM: %q", rt, args)
		}
		if i := slices.Index(args, "TERM"); i < 1 || args[i-1] != "-e" {
			t.Errorf("%s: expected a bare -e TERM for the secret, got %q", rt, args)
		}
		if !slices.Contains(args, "COLORTERM=truecolor") {
			t.Errorf("%s: COLORTERM default should be unaffected, got %q", rt, args)
		}
	}
}

func TestSecretEnv(t *testing.T) {
	if got := secretEnv(nil); got != nil {
		t.Errorf("secretEnv(nil) = %q, want nil so execCLI leaves the environment alone", got)
	}
	got := secretEnv([]secrets.Var{{Name: "A", Value: "1"}, {Name: "EMPTY", Value: ""}, {Name: "EQ", Value: "a=b"}})
	want := []string{"A=1", "EMPTY=", "EQ=a=b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("secretEnv = %q, want %q", got, want)
	}
}

func TestMergeEnv(t *testing.T) {
	tests := []struct {
		name     string
		base     []string
		extra    []string
		foldCase bool
		want     []string
	}{
		{
			name:  "appends new names",
			base:  []string{"A=1", "B=2"},
			extra: []string{"C=3"},
			want:  []string{"A=1", "B=2", "C=3"},
		},
		{
			name:  "replaces in place instead of duplicating",
			base:  []string{"A=1", "B=2", "C=3"},
			extra: []string{"B=secret"},
			want:  []string{"A=1", "B=secret", "C=3"},
		},
		{
			name:  "drops later duplicates already in base",
			base:  []string{"A=1", "B=2", "B=3"},
			extra: []string{"B=secret"},
			want:  []string{"A=1", "B=secret"},
		},
		{
			name:  "last extra entry wins",
			base:  []string{"A=1"},
			extra: []string{"B=first", "B=second"},
			want:  []string{"A=1", "B=second"},
		},
		{
			name:  "empty value still replaces",
			base:  []string{"A=1"},
			extra: []string{"A="},
			want:  []string{"A="},
		},
		{
			name:  "value containing equals",
			base:  []string{"A=x=y"},
			extra: []string{"A=p=q"},
			want:  []string{"A=p=q"},
		},
		{
			name:  "case matters off Windows",
			base:  []string{"Path=/bin"},
			extra: []string{"PATH=/secret"},
			want:  []string{"Path=/bin", "PATH=/secret"},
		},
		{
			name:     "case is ignored on Windows",
			base:     []string{"Path=C:\\Windows", "B=2"},
			extra:    []string{"PATH=secret"},
			foldCase: true,
			want:     []string{"PATH=secret", "B=2"},
		},
		{
			name:     "Windows drive entries are left alone",
			base:     []string{"=C:=C:\\proj", "A=1"},
			extra:    []string{"C=3"},
			foldCase: true,
			want:     []string{"=C:=C:\\proj", "A=1", "C=3"},
		},
		{
			name: "no extra returns base unchanged",
			base: []string{"A=1"},
			want: []string{"A=1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := slices.Clone(tt.base)
			got := mergeEnvFold(tt.base, tt.extra, tt.foldCase)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeEnvFold = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(tt.base, base) {
				t.Errorf("base was modified: %q", tt.base)
			}
		})
	}
}
