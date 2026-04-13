package docker

import (
	"crypto/sha256"
	"fmt"
	"io/fs"

	"github.com/skwashd/ccodolo/embedded"
)

// ImageTag computes the image tag for a project from the rendered Dockerfile
// and embedded dotfile contents.
func ImageTag(project, agentName, dockerfile string) string {
	h := sha256.New()
	h.Write([]byte(dockerfile))

	// Include embedded dotfile contents in the hash.
	_ = fs.WalkDir(embedded.Dotfiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := embedded.Dotfiles.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write(data)
		return nil
	})

	hash := fmt.Sprintf("%x", h.Sum(nil))[:8]
	return fmt.Sprintf("ccodolo:%s-%s-%s", project, agentName, hash)
}
