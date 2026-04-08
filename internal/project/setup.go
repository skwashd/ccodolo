package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SetupCopilot configures trusted_folders in .copilot/config.json.
func SetupCopilot(projectPath, projectName string) error {
	configDir := filepath.Join(projectPath, ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.json")
	projectDir := "/workspace/" + projectName

	cfg := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing %s: %w", configPath, err)
		}
	}

	// Get or create trusted_folders.
	var folders []string
	if raw, ok := cfg["trusted_folders"]; ok {
		if arr, ok := raw.([]interface{}); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					folders = append(folders, s)
				}
			}
		}
	}

	// Add project dir if not already present.
	found := false
	for _, f := range folders {
		if f == projectDir {
			found = true
			break
		}
	}
	if !found {
		folders = append(folders, projectDir)
	}

	cfg["trusted_folders"] = folders
	return writeJSON(configPath, cfg)
}

// SetupGemini configures context in .gemini/settings.json.
func SetupGemini(projectPath, projectName string) error {
	configDir := filepath.Join(projectPath, ".gemini")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "settings.json")
	projectDir := "/workspace/" + projectName

	cfg := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing %s: %w", configPath, err)
		}
	}

	// Get or create context.
	ctx, _ := cfg["context"].(map[string]interface{})
	if ctx == nil {
		ctx = make(map[string]interface{})
	}

	// Update includeDirectories.
	var dirs []string
	if raw, ok := ctx["includeDirectories"]; ok {
		if arr, ok := raw.([]interface{}); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					dirs = append(dirs, s)
				}
			}
		}
	}
	found := false
	for _, d := range dirs {
		if d == projectDir {
			found = true
			break
		}
	}
	if !found {
		dirs = append(dirs, projectDir)
	}
	ctx["includeDirectories"] = dirs
	ctx["fileName"] = []string{"AGENTS.md", "GEMINI.md"}

	cfg["context"] = ctx
	return writeJSON(configPath, cfg)
}

// SetupKiro disables telemetry in .kiro/settings/cli.json.
func SetupKiro(projectPath string) error {
	settingsDir := filepath.Join(projectPath, ".kiro", "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return err
	}

	configPath := filepath.Join(settingsDir, "cli.json")

	cfg := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing %s: %w", configPath, err)
		}
	}

	cfg["telemetry.enabled"] = false
	return writeJSON(configPath, cfg)
}

// writeJSON writes a JSON object to a file with indentation.
func writeJSON(path string, data interface{}) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
