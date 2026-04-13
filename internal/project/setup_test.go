package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupCopilot(t *testing.T) {
	dir := t.TempDir()

	err := SetupCopilot(dir, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(dir, ".copilot", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config.json to exist: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	folders, ok := cfg["trusted_folders"].([]interface{})
	if !ok {
		t.Fatal("expected trusted_folders array")
	}
	if len(folders) != 1 {
		t.Fatalf("expected 1 trusted folder, got %d", len(folders))
	}
	if folders[0] != "/workspace/myproject" {
		t.Errorf("expected '/workspace/myproject', got %v", folders[0])
	}
}

func TestSetupCopilotIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Call twice with the same project.
	if err := SetupCopilot(dir, "myproject"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := SetupCopilot(dir, "myproject"); err != nil {
		t.Fatalf("second call: %v", err)
	}

	configPath := filepath.Join(dir, ".copilot", "config.json")
	data, _ := os.ReadFile(configPath)

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	folders := cfg["trusted_folders"].([]interface{})
	if len(folders) != 1 {
		t.Errorf("expected 1 folder (no duplicates), got %d", len(folders))
	}
}

func TestSetupCopilotExistingConfig(t *testing.T) {
	dir := t.TempDir()
	copilotDir := filepath.Join(dir, ".copilot")
	if err := os.MkdirAll(copilotDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write existing config with a folder.
	existing := map[string]interface{}{
		"trusted_folders": []string{"/workspace/other"},
		"other_key":       "preserved",
	}
	data, _ := json.Marshal(existing)
	if err := os.WriteFile(filepath.Join(copilotDir, "config.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := SetupCopilot(dir, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := os.ReadFile(filepath.Join(copilotDir, "config.json"))
	var cfg map[string]interface{}
	if err := json.Unmarshal(result, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	folders := cfg["trusted_folders"].([]interface{})
	if len(folders) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(folders))
	}

	// other_key should be preserved.
	if cfg["other_key"] != "preserved" {
		t.Error("expected other_key to be preserved")
	}
}

func TestSetupGemini(t *testing.T) {
	dir := t.TempDir()

	err := SetupGemini(dir, "myproject")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(dir, ".gemini", "settings.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected settings.json to exist: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	ctx, ok := cfg["context"].(map[string]interface{})
	if !ok {
		t.Fatal("expected context object")
	}

	dirs := ctx["includeDirectories"].([]interface{})
	if len(dirs) != 1 || dirs[0] != "/workspace/myproject" {
		t.Errorf("unexpected includeDirectories: %v", dirs)
	}

	files := ctx["fileName"].([]interface{})
	if len(files) != 2 {
		t.Errorf("expected 2 fileNames, got %d", len(files))
	}
}

func TestSetupKiro(t *testing.T) {
	dir := t.TempDir()

	err := SetupKiro(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(dir, ".kiro", "settings", "cli.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected cli.json to exist: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if cfg["telemetry.enabled"] != false {
		t.Errorf("expected telemetry.enabled=false, got %v", cfg["telemetry.enabled"])
	}
}

func TestSetupKiroExistingConfig(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".kiro", "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existing := map[string]interface{}{
		"other_setting":     true,
		"telemetry.enabled": true,
	}
	data, _ := json.Marshal(existing)
	if err := os.WriteFile(filepath.Join(settingsDir, "cli.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := SetupKiro(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, _ := os.ReadFile(filepath.Join(settingsDir, "cli.json"))
	var cfg map[string]interface{}
	if err := json.Unmarshal(result, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if cfg["telemetry.enabled"] != false {
		t.Errorf("expected telemetry.enabled=false, got %v", cfg["telemetry.enabled"])
	}
	if cfg["other_setting"] != true {
		t.Error("expected other_setting to be preserved")
	}
}
