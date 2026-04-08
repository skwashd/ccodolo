package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tryMigrate checks for an old ccodolo.config file and migrates it to ccodolo.toml.
func tryMigrate(projectPath string) error {
	oldPath := filepath.Join(projectPath, "ccodolo.config")
	newPath := filepath.Join(projectPath, "ccodolo.toml")

	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil // nothing to migrate
	}

	// Check that the new file doesn't already exist.
	if _, err := os.Stat(newPath); err == nil {
		return nil // already migrated
	}

	// Parse the old shell-style config.
	values, err := parseShellConfig(oldPath)
	if err != nil {
		return fmt.Errorf("parsing old config: %w", err)
	}

	// Build a new Config from the old values.
	cfg := &Config{}
	if a, ok := values["agent"]; ok {
		cfg.Agent = a
	}

	// Save the new TOML config.
	if err := Save(cfg, newPath); err != nil {
		return fmt.Errorf("writing new config: %w", err)
	}

	// Rename the old config as a backup.
	backupPath := oldPath + ".bak"
	if err := os.Rename(oldPath, backupPath); err != nil {
		return fmt.Errorf("backing up old config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Migrated %s → %s (backup: %s)\n", oldPath, newPath, backupPath)
	return nil
}

// parseShellConfig reads key="value" lines from a shell-sourced config file.
func parseShellConfig(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding quotes.
		val = strings.Trim(val, `"'`)
		values[key] = val
	}
	return values, scanner.Err()
}
