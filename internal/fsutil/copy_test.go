package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create source structure.
	_ = os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0o644)
	_ = os.MkdirAll(filepath.Join(src, "subdir"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("world"), 0o644)

	err := CopyDir(src, dst)
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

func TestCopyDirPreservesFileMode(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	scriptPath := filepath.Join(src, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "run.sh"))
	if err != nil {
		t.Fatalf("expected run.sh to exist: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected mode 0o755, got %o", info.Mode().Perm())
	}
}

func TestCopyDirForcesDirMode(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.MkdirAll(filepath.Join(src, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(src, "subdir", "f.txt"), []byte("x"), 0o644)

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "subdir"))
	if err != nil {
		t.Fatalf("expected subdir to exist: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected dir mode 0o755, got %o", info.Mode().Perm())
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	if err := os.WriteFile(src, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected dst.txt to exist: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func TestCopyFilePreservesMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "run.sh")
	dst := filepath.Join(dir, "run-copy.sh")

	if err := os.WriteFile(src, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("expected dst to exist: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected mode 0o755, got %o", info.Mode().Perm())
	}
}

func TestCopyFileNonexistentSource(t *testing.T) {
	dir := t.TempDir()
	err := CopyFile(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "dst.txt"))
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}
