package docker

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/skwashd/ccodolo/embedded"
	"github.com/skwashd/ccodolo/internal/config"
)

// ImageExists checks if a Docker image with the given tag exists locally.
func ImageExists(tag string) bool {
	cmd := exec.Command("docker", "image", "inspect", tag)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// Build builds a Docker image from the given config.
// It returns the image tag.
func Build(cfg *config.Config, project string, force bool) (string, error) {
	dockerfile, err := RenderDockerfile(cfg)
	if err != nil {
		return "", fmt.Errorf("rendering Dockerfile: %w", err)
	}

	tag := ImageTag(project, cfg.Agent, dockerfile)

	if !force && ImageExists(tag) {
		fmt.Fprintf(os.Stderr, "Image %s already exists, skipping build (use --rebuild to force)\n", tag)
		return tag, nil
	}

	// Create temporary build context.
	tmpDir, err := os.MkdirTemp("", "ccodolo-build-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write Dockerfile.
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}

	// Write dotfiles.
	dotfilesDir := filepath.Join(tmpDir, "dotfiles")
	if err := os.MkdirAll(dotfilesDir, 0o755); err != nil {
		return "", fmt.Errorf("creating dotfiles dir: %w", err)
	}
	err = fs.WalkDir(embedded.Dotfiles, "dotfiles", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := embedded.Dotfiles.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// path is "dotfiles/.bashrc" etc. — strip the leading "dotfiles/" to get the filename.
		relPath := strings.TrimPrefix(path, "dotfiles/")
		dstPath := filepath.Join(dotfilesDir, relPath)
		return os.WriteFile(dstPath, data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("writing dotfiles: %w", err)
	}

	// Copy COPY/ADD source files from project common/ dir if custom steps reference them.
	if err := copyBuildContextFiles(cfg, tmpDir); err != nil {
		return "", fmt.Errorf("copying build context files: %w", err)
	}

	// Run docker build.
	fmt.Fprintf(os.Stderr, "Building image %s...\n", tag)
	cmd := exec.Command("docker", "build",
		"--build-arg", "CCODOLO_AGENT="+cfg.Agent,
		"-t", tag, ".")
	cmd.Dir = tmpDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Image %s built successfully\n", tag)
	return tag, nil
}

// copyBuildContextFiles copies referenced files from the project common/ dir
// into the build context for COPY/ADD custom steps.
func copyBuildContextFiles(cfg *config.Config, tmpDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	for _, step := range cfg.Build.CustomSteps {
		upper := strings.TrimSpace(step)
		// Only process COPY and ADD instructions.
		if !strings.HasPrefix(strings.ToUpper(upper), "COPY ") && !strings.HasPrefix(strings.ToUpper(upper), "ADD ") {
			continue
		}

		// Parse simple "COPY src dst" / "ADD src dst" — not handling multi-src or --from=.
		parts := strings.Fields(upper)
		if len(parts) < 3 {
			continue
		}

		src := parts[1]
		// Skip if it's an absolute path or a --flag.
		if filepath.IsAbs(src) || strings.HasPrefix(src, "-") {
			continue
		}

		// Resolve relative to ~/.ccodolo/projects/<project>/common/
		// For now we just look in the global config dir common or the build context.
		srcPath := filepath.Join(home, ".ccodolo", "common", src)
		if _, err := os.Stat(srcPath); err != nil {
			continue // file doesn't exist, docker build will fail with a clear error
		}

		dstPath := filepath.Join(tmpDir, src)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
