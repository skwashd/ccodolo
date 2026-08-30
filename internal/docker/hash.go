package docker

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/skwashd/ccodolo/embedded"
)

// ImageTag computes the image tag for a project from the rendered
// Dockerfile, the embedded dotfile and startup script contents, and the
// resolved build-context staged files (custom_steps/root_steps COPY/ADD
// sources). Hashing these keeps the tag in sync with them — otherwise
// editing an embedded or staged file's contents, without touching the
// Dockerfile, would leave the cached image stale.
func ImageTag(project, agentName, dockerfile string, staged []stagedFile) string {
	h := sha256.New()
	h.Write([]byte(dockerfile))

	hashEmbeddedFS(h, embedded.Dotfiles)
	hashEmbeddedFS(h, embedded.Scripts)

	// Include staged build-context files (relative path, mode, contents),
	// in the order resolveBuildContextFiles already sorted them for
	// determinism.
	for _, f := range staged {
		_ = hashStagedFile(h, f)
	}

	hash := fmt.Sprintf("%x", h.Sum(nil))[:8]
	return fmt.Sprintf("ccodolo:%s-%s-%s", project, agentName, hash)
}

// hashEmbeddedFS writes the path and contents of every file in fsys into w,
// in WalkDir's deterministic lexical order. The path is part of the digest
// for the same reason it is in hashStagedFile: without it, renaming an
// embedded file — or moving content between the two trees, which are hashed
// back to back into one stream — leaves the tag unchanged and ImageExists
// reuses the stale image. Mode is not hashed: embed.FS reports every file as
// 0444, so it carries no information.
func hashEmbeddedFS(w io.Writer, fsys fs.FS) {
	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		// Length-prefixed so no combination of path and contents can be
		// re-split into a different one with the same digest.
		_, _ = fmt.Fprintf(w, "%d:%s%d:", len(path), path, len(data))
		_, _ = w.Write(data)
		return nil
	})
}

// hashStagedFile writes a staged file's relative path, mode, and contents
// into w. For a directory source it walks the directory (in the sorted
// order filepath.WalkDir already guarantees) and hashes each contained
// file the same way.
func hashStagedFile(w io.Writer, f stagedFile) error {
	info, err := os.Stat(f.SrcPath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return hashFile(w, f.RelPath, f.SrcPath, info.Mode())
	}

	return filepath.WalkDir(f.SrcPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(f.SrcPath, path)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		return hashFile(w, filepath.Join(f.RelPath, rel), path, info.Mode())
	})
}

// hashFile writes relPath, mode, and the file contents at srcPath into w.
func hashFile(w io.Writer, relPath, srcPath string, mode fs.FileMode) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(w, relPath)
	_, _ = fmt.Fprintf(w, ":%04o:", mode.Perm())
	_, _ = w.Write(data)
	return nil
}
