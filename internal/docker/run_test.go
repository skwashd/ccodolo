package docker

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/skwashd/ccodolo/internal/agent"
	"github.com/skwashd/ccodolo/internal/config"
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

	args, err = runArgs(rt, cfg, meta, projectPath, workdir,
		"test-name", "/workspace/proj/wd", "ccodolo:proj-claude-abcd1234",
		[]string{"--resume"})
	if err != nil {
		t.Fatalf("runArgs error: %v", err)
	}
	return args, projectPath, workdir
}

// expectedRunArgs is the argv both backends share, except the passthrough
// entry for CCODOLO_TEST_SET and the memory flag (omitted when memory is
// empty), which the caller supplies.
func expectedRunArgs(projectPath, workdir, passthroughSet, memory string) []string {
	args := []string{"run", "--rm", "-it"}
	if memory != "" {
		args = append(args, "--memory", memory)
	}
	return append(args,
		"--name", "test-name",
		"-w", "/workspace/proj/wd",
		"--mount", "type=bind,source="+workdir+",target=/workspace/proj/wd",
		"--mount", "type=bind,source="+filepath.Join(projectPath, "commandhistory")+",target=/commandhistory",
		"--mount", "type=bind,source="+filepath.Join(projectPath, "common")+",target=/home/coder/project",
		"--mount", "type=bind,source="+filepath.Join(projectPath, ".claude")+",target=/home/coder/.claude",
		"--mount", "type=bind,source="+filepath.Join(projectPath, ".claude.json")+",target=/home/coder/.claude.json",
		"--mount", "type=bind,source="+filepath.Join(projectPath, ".claude-plugin")+",target=/home/coder/.claude-plugin",
		"--mount", "type=bind,source=/host/data,target=/data,readonly",
		"-e", "FOO=bar",
		"-e", passthroughSet,
		"-e", "TERM=xterm-256color",
		"ccodolo:proj-claude-abcd1234",
		"--resume",
	)
}

func TestRunArgsDocker(t *testing.T) {
	args, projectPath, workdir := runArgsFixture(t, RuntimeDocker, "")
	want := expectedRunArgs(projectPath, workdir, "CCODOLO_TEST_SET", "")
	if !reflect.DeepEqual(args, want) {
		t.Errorf("runArgs mismatch:\n got: %q\nwant: %q", args, want)
	}
}

func TestRunArgsApplePassthroughResolved(t *testing.T) {
	args, projectPath, workdir := runArgsFixture(t, RuntimeApple, "")
	want := expectedRunArgs(projectPath, workdir, "CCODOLO_TEST_SET=sekrit", defaultAppleMemory)
	if !reflect.DeepEqual(args, want) {
		t.Errorf("runArgs mismatch:\n got: %q\nwant: %q", args, want)
	}
	for i, a := range args {
		if a == "CCODOLO_TEST_SET" {
			t.Errorf("apple runtime should not emit bare passthrough name (arg %d)", i)
		}
	}
}

func TestRunArgsMemory(t *testing.T) {
	t.Run("docker configured", func(t *testing.T) {
		args, projectPath, workdir := runArgsFixture(t, RuntimeDocker, "8g")
		want := expectedRunArgs(projectPath, workdir, "CCODOLO_TEST_SET", "8g")
		if !reflect.DeepEqual(args, want) {
			t.Errorf("runArgs mismatch:\n got: %q\nwant: %q", args, want)
		}
	})

	t.Run("apple configured overrides default", func(t *testing.T) {
		args, projectPath, workdir := runArgsFixture(t, RuntimeApple, "2g")
		want := expectedRunArgs(projectPath, workdir, "CCODOLO_TEST_SET=sekrit", "2g")
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
	args, err := runArgs(RuntimeDocker, &config.Config{Agent: "claude"}, meta,
		t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", nil)
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
	args, err = runArgs(RuntimeDocker, &config.Config{Agent: "claude"}, meta,
		t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", nil)
	if err != nil {
		t.Fatalf("runArgs error: %v", err)
	}
	if !slices.Contains(args, "TERM=xterm-ghostty") || slices.Contains(args, "TERM=xterm-256color") {
		t.Errorf("host TERM should override the Windows Terminal default, got %q", args)
	}
}

func TestMountArg(t *testing.T) {
	got, err := mountArg(`C:\Users\dave\proj`, "/workspace/p/proj", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := `type=bind,source=C:\Users\dave\proj,target=/workspace/p/proj`; got != want {
		t.Errorf("mountArg = %q, want %q", got, want)
	}

	got, err = mountArg("/home/dave/.aws", "/home/coder/.aws", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "type=bind,source=/home/dave/.aws,target=/home/coder/.aws,readonly"; got != want {
		t.Errorf("mountArg = %q, want %q", got, want)
	}

	for _, tc := range []struct{ src, dst string }{
		{`C:\Users\Doe, John\proj`, "/workspace/p/proj"},
		{"/data", "/mnt/a,b"},
		{`/home/dave/proj"v2`, "/workspace/p/proj"},
	} {
		if _, err := mountArg(tc.src, tc.dst, false); err == nil || !strings.Contains(err.Error(), "comma or double quote") {
			t.Errorf("mountArg(%q, %q): expected comma/quote error, got %v", tc.src, tc.dst, err)
		}
	}
}

func TestCheckMounts(t *testing.T) {
	workdir := t.TempDir()
	projectPath := t.TempDir()
	existing := t.TempDir()

	ok := &config.Config{Volumes: []config.Volume{{Host: existing, Container: "/data"}}}
	if err := CheckMounts(ok, workdir, projectPath); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	missing := &config.Config{Volumes: []config.Volume{{Host: filepath.Join(existing, "nope"), Container: "/data"}}}
	err := CheckMounts(missing, workdir, projectPath)
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "nope") {
		t.Errorf("expected missing-path error naming the entry, got %v", err)
	}

	badContainer := &config.Config{Volumes: []config.Volume{{Host: existing, Container: "/mnt/a,b"}}}
	if err := CheckMounts(badContainer, workdir, projectPath); err == nil || !strings.Contains(err.Error(), "/mnt/a,b") {
		t.Errorf("expected comma error naming the container path, got %v", err)
	}

	if err := CheckMounts(&config.Config{}, "/srv/a,b", projectPath); err == nil || !strings.Contains(err.Error(), "workdir") {
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
	_, err = runArgs(RuntimeDocker, cfg, meta, t.TempDir(), t.TempDir(), "n", "/workspace/p/wd", "img", nil)
	if err == nil || !strings.Contains(err.Error(), "/srv/a,b") {
		t.Errorf("expected error naming the offending path, got %v", err)
	}
}

func TestCredentialMountWarnings(t *testing.T) {
	vols := []config.Volume{
		{Host: "~/.aws", Container: "/home/coder/.aws"},
		{Host: "~/.ssh", Container: "/home/coder/.ssh"},
		{Host: "~/.gnupg", Container: "/home/coder/.gnupg/"},
	}
	got := credentialMountWarnings(vols)
	if len(got) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %q", len(got), got)
	}
	if !strings.Contains(got[0], "/home/coder/.ssh") || !strings.Contains(got[1], "/home/coder/.gnupg") {
		t.Errorf("warnings should name the mounted paths, got %q", got)
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
