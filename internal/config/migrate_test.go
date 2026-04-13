package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseShellConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.config")

	content := `# comment
agent="claude"

`
	_ = os.WriteFile(path, []byte(content), 0o644)

	values, err := parseShellConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values["agent"] != "claude" {
		t.Errorf("expected agent='claude', got %q", values["agent"])
	}
}

func TestParseShellConfigSingleQuotes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ccodolo.config")

	content := `agent='gemini'`
	_ = os.WriteFile(path, []byte(content), 0o644)

	values, err := parseShellConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values["agent"] != "gemini" {
		t.Errorf("expected agent='gemini', got %q", values["agent"])
	}
}

func TestMigration(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "ccodolo.config")
	newPath := filepath.Join(dir, "ccodolo.toml")

	_ = os.WriteFile(oldPath, []byte(`agent="copilot"`), 0o644)

	err := tryMigrate(dir)
	if err != nil {
		t.Fatalf("migration error: %v", err)
	}

	// New TOML file should exist.
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("ccodolo.toml should have been created")
	}

	// Old file should be renamed to .bak.
	backupPath := oldPath + ".bak"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("ccodolo.config.bak should exist")
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("ccodolo.config should have been renamed")
	}

	// Load the new config.
	cfg, err := loadFile(newPath)
	if err != nil {
		t.Fatalf("loading new config: %v", err)
	}
	if cfg.Agent != "copilot" {
		t.Errorf("expected agent 'copilot', got %q", cfg.Agent)
	}
}

func TestMigrationNoOldConfig(t *testing.T) {
	dir := t.TempDir()
	err := tryMigrate(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No files should be created.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files, got %d", len(entries))
	}
}

func TestMigrationAlreadyMigrated(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "ccodolo.config")
	newPath := filepath.Join(dir, "ccodolo.toml")

	_ = os.WriteFile(oldPath, []byte(`agent="claude"`), 0o644)
	_ = os.WriteFile(newPath, []byte(`agent = "gemini"`), 0o644)

	err := tryMigrate(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// TOML should not be overwritten.
	cfg, _ := loadFile(newPath)
	if cfg.Agent != "gemini" {
		t.Errorf("expected existing toml to be preserved with 'gemini', got %q", cfg.Agent)
	}
}
