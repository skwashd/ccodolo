package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitCommit stages the given files and creates a commit with the given message.
func gitCommit(files []string, msg string) error {
	// git add
	args := append([]string{"add", "--"}, files...)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}
	// git commit
	out, err := exec.Command("git", "commit", "-m", msg).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}
	return nil
}

// repoRoot returns the root of the git repository.
func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// mustRepoRoot panics if the repo root cannot be determined.
func mustRepoRoot() string {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot find git repo root: %v\n", err)
		os.Exit(1)
	}
	return root
}
