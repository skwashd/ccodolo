package docker

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/skwashd/ccodolo/embedded"
	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/fsutil"
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
func Build(cfg *config.Config, project, projectPath string, force bool) (string, error) {
	dockerfile, err := RenderDockerfile(cfg)
	if err != nil {
		return "", fmt.Errorf("rendering Dockerfile: %w", err)
	}

	// Resolve build-context staged files (custom_steps/root_steps COPY/ADD
	// sources) before computing the tag, so editing a staged file rotates
	// the tag even without --rebuild.
	staged, err := resolveBuildContextFiles(cfg, projectPath)
	if err != nil {
		return "", fmt.Errorf("resolving build context files: %w", err)
	}

	tag := ImageTag(project, cfg.Agent, dockerfile, staged)

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

	// Write embedded dotfiles and startup scripts.
	if err := writeEmbeddedTree(embedded.Dotfiles, "dotfiles", tmpDir); err != nil {
		return "", fmt.Errorf("writing dotfiles: %w", err)
	}
	if err := writeEmbeddedTree(embedded.Scripts, "scripts", tmpDir); err != nil {
		return "", fmt.Errorf("writing scripts: %w", err)
	}

	// Stage COPY/ADD source files referenced by custom_steps/root_steps.
	if err := stageBuildContextFiles(staged, tmpDir); err != nil {
		return "", fmt.Errorf("staging build context files: %w", err)
	}

	// Run docker build.
	fmt.Fprintf(os.Stderr, "Building image %s...\n", tag)
	cmd := exec.Command("docker", "build",
		"--progress=plain",
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

// writeEmbeddedTree copies every file under root in fsys into
// <tmpDir>/<root>/, preserving the layout below root.
func writeEmbeddedTree(fsys fs.FS, root, tmpDir string) error {
	dstDir := filepath.Join(tmpDir, root)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return readErr
		}
		// path is "<root>/name" — strip the leading "<root>/" for the destination.
		relPath := strings.TrimPrefix(path, root+"/")
		dstPath := filepath.Join(dstDir, relPath)
		if mkErr := os.MkdirAll(filepath.Dir(dstPath), 0o755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(dstPath, data, 0o644)
	})
}

// stagedFile represents one file or directory to copy into the Docker
// build context, keyed by its destination-relative path — the same path a
// COPY/ADD instruction in the rendered Dockerfile references, relative to
// the context root.
type stagedFile struct {
	RelPath string // path relative to the build context root
	SrcPath string // absolute path to the source file/dir on disk
}

// resolveBuildContextFiles walks build.root_steps then build.custom_steps
// looking for COPY/ADD instructions, and resolves each source against the
// common/ directory matching the step's origin (global vs project). Root
// steps are walked first because they land earlier in the rendered
// Dockerfile; staging order doesn't actually affect the result, this is
// just for consistency with the template's step ordering.
//
// It returns a hard error naming the offending step for: JSON-array form
// (unsupported for staging), an absolute source path, a source resolving
// outside the common dir (path traversal), and a source that doesn't
// exist. A step with a --from=<stage> flag is skipped entirely — it copies
// from another build stage or image, not the host filesystem.
//
// The returned slice is sorted by RelPath for deterministic hashing.
func resolveBuildContextFiles(cfg *config.Config, projectPath string) ([]stagedFile, error) {
	var files []stagedFile

	for i, step := range cfg.Build.RootSteps {
		staged, err := resolveStepFiles(step, cfg.RootStepOrigin(i), projectPath)
		if err != nil {
			return nil, err
		}
		files = append(files, staged...)
	}

	for i, step := range cfg.Build.CustomSteps {
		staged, err := resolveStepFiles(step, cfg.CustomStepOrigin(i), projectPath)
		if err != nil {
			return nil, err
		}
		files = append(files, staged...)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	return files, nil
}

// resolveStepFiles resolves the COPY/ADD sources in a single build step.
// It returns (nil, nil) for steps that aren't COPY/ADD, and for COPY/ADD
// steps that use --from=<stage>.
func resolveStepFiles(step string, origin config.StepOrigin, projectPath string) ([]stagedFile, error) {
	// Normalize Dockerfile line continuations before tokenizing —
	// custom_steps/root_steps are often written as multi-line TOML strings.
	normalized := strings.ReplaceAll(step, "\\\r\n", " ")
	normalized = strings.ReplaceAll(normalized, "\\\n", " ")
	trimmed := strings.TrimSpace(normalized)
	upper := strings.ToUpper(trimmed)

	var body string
	switch {
	case strings.HasPrefix(upper, "COPY "):
		body = trimmed[len("COPY "):]
	case strings.HasPrefix(upper, "ADD "):
		body = trimmed[len("ADD "):]
	default:
		return nil, nil // RUN or another non-copying instruction; nothing to stage.
	}

	tokens := strings.Fields(body)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("custom step %q: COPY/ADD requires a source and a destination", step)
	}

	// Skip leading --flags (--chown=, --chmod=, --from=, ...) to find the
	// source/destination tokens.
	flagCount := 0
	fromStage := false
	for flagCount < len(tokens) && strings.HasPrefix(tokens[flagCount], "--") {
		if strings.HasPrefix(tokens[flagCount], "--from=") {
			fromStage = true
		}
		flagCount++
	}
	rest := tokens[flagCount:]
	if len(rest) == 0 {
		return nil, fmt.Errorf("custom step %q: COPY/ADD requires a source and a destination", step)
	}

	// JSON-array form ('COPY ["a","b"]') reads its source list from JSON,
	// which this staging pass does not parse.
	if strings.HasPrefix(rest[0], "[") {
		return nil, fmt.Errorf(
			"custom step %q: JSON array form is not supported for build-context staging; use shell form (COPY src... dst)",
			step,
		)
	}

	// --from=<stage> copies from another build stage or image, not the
	// host filesystem — nothing for us to stage.
	if fromStage {
		return nil, nil
	}

	if len(rest) < 2 {
		return nil, fmt.Errorf("custom step %q: COPY/ADD requires at least one source and a destination", step)
	}
	sources := rest[:len(rest)-1]

	commonDir, err := originCommonDir(origin, projectPath)
	if err != nil {
		return nil, err
	}

	var files []stagedFile
	for _, src := range sources {
		matched, err := resolveSource(step, src, commonDir)
		if err != nil {
			return nil, err
		}
		files = append(files, matched...)
	}
	return files, nil
}

// resolveSource resolves a single COPY/ADD source token against commonDir,
// expanding glob patterns, and returns one stagedFile per match.
func resolveSource(step, src, commonDir string) ([]stagedFile, error) {
	if filepath.IsAbs(src) {
		return nil, fmt.Errorf("custom step %q: source %q must be a relative path into %s", step, src, commonDir)
	}

	resolved := filepath.Join(commonDir, src)
	if !withinDir(commonDir, resolved) {
		return nil, fmt.Errorf("custom step %q: source %q resolves outside %s", step, src, commonDir)
	}

	var matches []string
	if strings.ContainsAny(src, "*?[") {
		var err error
		matches, err = filepath.Glob(resolved)
		if err != nil {
			return nil, fmt.Errorf("custom step %q: invalid glob %q: %w", step, src, err)
		}
	} else if _, statErr := os.Stat(resolved); statErr == nil {
		matches = []string{resolved}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("custom step %q: source %q not found in %s", step, src, commonDir)
	}

	files := make([]stagedFile, 0, len(matches))
	for _, m := range matches {
		if !withinDir(commonDir, m) {
			return nil, fmt.Errorf("custom step %q: source %q resolves outside %s", step, src, commonDir)
		}
		rel, err := filepath.Rel(commonDir, m)
		if err != nil {
			return nil, fmt.Errorf("custom step %q: resolving %q: %w", step, src, err)
		}
		files = append(files, stagedFile{RelPath: rel, SrcPath: m})
	}
	return files, nil
}

// originCommonDir maps a step's origin to the common/ directory its
// COPY/ADD sources resolve against: the global common dir for steps
// declared in ~/.ccodolo/ccodolo.toml, the project's common dir otherwise.
func originCommonDir(origin config.StepOrigin, projectPath string) (string, error) {
	if origin == config.OriginGlobal {
		return config.GlobalCommonDir()
	}
	return config.CommonDir(projectPath), nil
}

// withinDir reports whether target is inside (or equal to) base, guarding
// against COPY/ADD sources like "../../etc/passwd" that would otherwise
// read outside the common dir.
func withinDir(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// stageBuildContextFiles writes resolved staged files into the build
// context at tmpDir, preserving file modes and recursing into directory
// sources.
func stageBuildContextFiles(files []stagedFile, tmpDir string) error {
	for _, f := range files {
		dst := filepath.Join(tmpDir, f.RelPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", f.RelPath, err)
		}

		info, err := os.Stat(f.SrcPath)
		if err != nil {
			return fmt.Errorf("staging %s: %w", f.RelPath, err)
		}

		if info.IsDir() {
			if err := fsutil.CopyDir(f.SrcPath, dst); err != nil {
				return fmt.Errorf("staging %s: %w", f.RelPath, err)
			}
			continue
		}

		if err := fsutil.CopyFile(f.SrcPath, dst); err != nil {
			return fmt.Errorf("staging %s: %w", f.RelPath, err)
		}
	}
	return nil
}
