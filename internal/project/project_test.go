package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()

	err := EnsureDirs(dir, ".claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check directories exist.
	for _, sub := range []string{"commandhistory", "common", ".claude"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Errorf("directory %q should exist: %v", sub, err)
		} else if !info.IsDir() {
			t.Errorf("%q should be a directory", sub)
		}
	}

	// Check .claude.json exists for claude agent.
	jsonPath := filepath.Join(dir, ".claude.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("expected .claude.json to exist: %v", err)
	}
	if string(data) != "{}\n" {
		t.Errorf("expected .claude.json to contain '{}\\n', got %q", string(data))
	}
}

func TestEnsureDirsNonClaude(t *testing.T) {
	dir := t.TempDir()

	err := EnsureDirs(dir, ".gemini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// .gemini dir should exist.
	if _, err := os.Stat(filepath.Join(dir, ".gemini")); err != nil {
		t.Error("expected .gemini directory to exist")
	}

	// .claude.json should NOT exist for non-claude agents.
	if _, err := os.Stat(filepath.Join(dir, ".claude.json")); !os.IsNotExist(err) {
		t.Error(".claude.json should not exist for non-claude agents")
	}
}

func TestEnsureDirsIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Call twice - should not error.
	if err := EnsureDirs(dir, ".claude"); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if err := EnsureDirs(dir, ".claude"); err != nil {
		t.Fatalf("second call error: %v", err)
	}
}

func TestCopyTemplate(t *testing.T) {
	// This test uses a temp dir as the template source.
	// Since CopyTemplate looks at ~/.ccodolo/template/, we can't easily test
	// it without mocking the home dir. Just verify it doesn't error with no template.
	dir := t.TempDir()
	err := CopyTemplate(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create source structure.
	_ = os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0o644)
	_ = os.MkdirAll(filepath.Join(src, "subdir"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("world"), 0o644)

	err := copyDir(src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check files.
	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatalf("expected file.txt to exist: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(dst, "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("expected subdir/nested.txt to exist: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got %q", string(data))
	}
}
