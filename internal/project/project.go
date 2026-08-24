package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/fsutil"
)

// EnsureDirs creates the standard project subdirectories, plus the global
// common/ directory (beside ~/.ccodolo/ccodolo.toml) so global-origin
// custom_steps/root_steps have somewhere to stage COPY/ADD sources from.
func EnsureDirs(projectPath string, agentConfigDir string) error {
	dirs := []string{
		filepath.Join(projectPath, "commandhistory"),
		filepath.Join(projectPath, "common"),
		filepath.Join(projectPath, agentConfigDir),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	globalCommon, err := config.GlobalCommonDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(globalCommon, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", globalCommon, err)
	}

	// Create initial .claude.json for claude agent.
	if agentConfigDir == ".claude" {
		jsonPath := filepath.Join(projectPath, ".claude.json")
		if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
			if err := os.WriteFile(jsonPath, []byte("{}\n"), 0o644); err != nil {
				return fmt.Errorf("creating %s: %w", jsonPath, err)
			}
		}
	}

	return nil
}

// CopyTemplate copies the user's template directory into a new project.
func CopyTemplate(projectPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	templateDir := filepath.Join(home, ".ccodolo", "template")

	info, err := os.Stat(templateDir)
	if err != nil || !info.IsDir() {
		return nil // no template, nothing to copy
	}

	fmt.Fprintf(os.Stderr, "Copying template from %s\n", templateDir)
	return fsutil.CopyDir(templateDir, projectPath)
}
