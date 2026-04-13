package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureDirs creates the standard project subdirectories.
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
	return copyDir(templateDir, projectPath)
}

// copyDir recursively copies src to dst, preserving permissions.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}
