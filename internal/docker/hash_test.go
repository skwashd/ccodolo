package docker

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHashEmbeddedFSIncludesPath(t *testing.T) {
	same := []byte("echo hello\n")
	renamed := fstest.MapFS{"scripts/two.sh": &fstest.MapFile{Data: same}}
	original := fstest.MapFS{"scripts/one.sh": &fstest.MapFile{Data: same}}

	a, b := sha256.New(), sha256.New()
	hashEmbeddedFS(a, original)
	hashEmbeddedFS(b, renamed)
	if bytes.Equal(a.Sum(nil), b.Sum(nil)) {
		t.Error("renaming an embedded file should change the digest")
	}
}

func TestHashEmbeddedFSDelimitsFiles(t *testing.T) {
	// Without a delimiter between path and contents, "ab" + "" and "a" +
	// "b" hash identically and a rebuild is skipped.
	split := fstest.MapFS{"a": &fstest.MapFile{Data: []byte("b:c")}}
	joined := fstest.MapFS{"a:b": &fstest.MapFile{Data: []byte("c")}}

	a, b := sha256.New(), sha256.New()
	hashEmbeddedFS(a, split)
	hashEmbeddedFS(b, joined)
	if bytes.Equal(a.Sum(nil), b.Sum(nil)) {
		t.Error("path and contents should not be ambiguous in the digest")
	}
}

func TestImageTag(t *testing.T) {
	tag := ImageTag("myproject", "claude", "FROM debian:trixie-slim\nRUN echo hello", nil)

	if !strings.HasPrefix(tag, "ccodolo:myproject-claude-") {
		t.Errorf("expected tag to start with 'ccodolo:myproject-claude-', got %q", tag)
	}

	// Hash suffix should be 8 hex chars.
	parts := strings.SplitN(tag, "-", 3)
	if len(parts) != 3 {
		t.Fatalf("expected tag format 'ccodolo:project-agent-hash', got %q", tag)
	}
	hash := parts[2]
	if len(hash) != 8 {
		t.Errorf("expected 8-char hash, got %q (%d chars)", hash, len(hash))
	}
}

func TestImageTagDeterministic(t *testing.T) {
	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	tag1 := ImageTag("proj", "claude", dockerfile, nil)
	tag2 := ImageTag("proj", "claude", dockerfile, nil)
	if tag1 != tag2 {
		t.Errorf("expected deterministic tags, got %q and %q", tag1, tag2)
	}
}

func TestImageTagDifferentInputs(t *testing.T) {
	tag1 := ImageTag("proj", "claude", "FROM debian:trixie-slim\nRUN echo hello", nil)
	tag2 := ImageTag("proj", "claude", "FROM debian:trixie-slim\nRUN echo world", nil)
	if tag1 == tag2 {
		t.Error("different Dockerfiles should produce different tags")
	}
}

func TestImageTagDifferentProjects(t *testing.T) {
	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	tag1 := ImageTag("proj1", "claude", dockerfile, nil)
	tag2 := ImageTag("proj2", "claude", dockerfile, nil)

	// Same hash but different project names.
	if !strings.HasPrefix(tag1, "ccodolo:proj1-claude-") {
		t.Error("tag1 should have project 'proj1'")
	}
	if !strings.HasPrefix(tag2, "ccodolo:proj2-claude-") {
		t.Error("tag2 should have project 'proj2'")
	}
}

func TestImageTagDifferentAgents(t *testing.T) {
	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	tag1 := ImageTag("proj", "claude", dockerfile, nil)
	tag2 := ImageTag("proj", "copilot", dockerfile, nil)
	if tag1 == tag2 {
		t.Error("different agents should produce different tags")
	}
	if !strings.HasPrefix(tag1, "ccodolo:proj-claude-") {
		t.Errorf("expected tag1 to start with 'ccodolo:proj-claude-', got %q", tag1)
	}
	if !strings.HasPrefix(tag2, "ccodolo:proj-copilot-") {
		t.Errorf("expected tag2 to start with 'ccodolo:proj-copilot-', got %q", tag2)
	}
}

func TestImageTagStagedFileContentsChangeTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.sh")
	if err := os.WriteFile(path, []byte("echo one"), 0o644); err != nil {
		t.Fatal(err)
	}

	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	staged := []stagedFile{{RelPath: "g.sh", SrcPath: path}}
	tag1 := ImageTag("proj", "claude", dockerfile, staged)

	if err := os.WriteFile(path, []byte("echo two"), 0o644); err != nil {
		t.Fatal(err)
	}
	tag2 := ImageTag("proj", "claude", dockerfile, staged)

	if tag1 == tag2 {
		t.Error("changing staged file contents should change the tag")
	}
}

func TestImageTagStagedFileModeChangesTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.sh")
	if err := os.WriteFile(path, []byte("echo one"), 0o644); err != nil {
		t.Fatal(err)
	}

	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	staged := []stagedFile{{RelPath: "g.sh", SrcPath: path}}
	tag1 := ImageTag("proj", "claude", dockerfile, staged)

	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	tag2 := ImageTag("proj", "claude", dockerfile, staged)

	if tag1 == tag2 {
		t.Error("changing staged file mode should change the tag")
	}
}

func TestImageTagStagedFilesIdenticalInputsSameTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g.sh")
	if err := os.WriteFile(path, []byte("echo one"), 0o644); err != nil {
		t.Fatal(err)
	}

	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	staged := []stagedFile{{RelPath: "g.sh", SrcPath: path}}
	tag1 := ImageTag("proj", "claude", dockerfile, staged)
	tag2 := ImageTag("proj", "claude", dockerfile, staged)

	if tag1 != tag2 {
		t.Errorf("expected identical inputs to produce identical tags, got %q and %q", tag1, tag2)
	}
}
