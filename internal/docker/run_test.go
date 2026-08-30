package docker

import (
	"os"
	"path/filepath"
	"reflect"
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
		"-v", workdir + ":/workspace/proj/wd",
		"-v", projectPath + "/commandhistory:/commandhistory",
		"-v", projectPath + "/common:/home/coder/project",
		"-v", projectPath + "/.claude:/home/coder/.claude",
		"-v", projectPath + "/.claude.json:/home/coder/.claude.json",
		"-v", projectPath + "/.claude-plugin:/home/coder/.claude-plugin",
		"-v", "/host/data:/data:ro",
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
