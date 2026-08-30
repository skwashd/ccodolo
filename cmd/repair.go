package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var flagRepairWorkdir string

var repairWorktreesCmd = &cobra.Command{
	Use:   "repair-worktrees",
	Short: "Repair in-repo git worktrees for host-side use",
	Long: "Worktrees created inside a ccodolo container record container paths.\n" +
		"This runs `git worktree repair` on every worktree under .worktrees/ and\n" +
		".claude/worktrees/ so they work on the host again, then restores the\n" +
		"relative gitdir that lets git commands inside a worktree work on both\n" +
		"sides. Operates on the repository containing --workdir (default: the\n" +
		"current directory).",
	Args: cobra.NoArgs,
	RunE: runRepairWorktrees,
}

func init() {
	repairWorktreesCmd.Flags().StringVar(&flagRepairWorkdir, "workdir", "", "Repository directory (default: current directory)")
	rootCmd.AddCommand(repairWorktreesCmd)
}

func runRepairWorktrees(cmd *cobra.Command, args []string) error {
	workdir := flagRepairWorkdir
	if workdir == "" {
		var err error
		workdir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
	}
	if !dirExists(workdir) {
		return fmt.Errorf("--workdir doesn't exist: %s", workdir)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required but was not found on PATH")
	}

	// The conventions are anchored at the repository root, not at wherever
	// the command was invoked — `git -C` succeeds from any subdirectory, so
	// resolving .worktrees/ against workdir would silently find nothing.
	root, err := gitOutput(workdir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return fmt.Errorf("%s is not a git repository", workdir)
	}

	dirs := worktreeDirs(root)
	if len(dirs) == 0 {
		fmt.Fprintf(os.Stderr, "No worktrees found under .worktrees/ or .claude/worktrees/ in %s\n", root)
		return nil
	}

	// One path at a time: git fails the whole invocation if any single path
	// is bad, so a batch would leave every healthy worktree unrepaired
	// because of one leftover directory.
	var repaired, failed []string
	for _, rel := range dirs {
		repair := exec.Command("git", "-C", root, "worktree", "repair", rel)
		repair.Stdout = os.Stdout
		repair.Stderr = os.Stderr
		if err := repair.Run(); err != nil {
			failed = append(failed, rel)
			continue
		}
		repaired = append(repaired, rel)
	}

	if len(repaired) > 0 {
		fmt.Fprintf(os.Stderr, "Repaired %d worktree(s): %s\n", len(repaired), strings.Join(repaired, ", "))
		if n := relativizeGitdirs(root, repaired); n > 0 {
			fmt.Fprintf(os.Stderr, "Rewrote %d worktree .git file(s) to a relative gitdir\n", n)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not repair %d of %d worktree(s): %s\n"+
			"git may no longer track them — check for a leftover directory whose entry under .git/worktrees/ is gone",
			len(failed), len(dirs), strings.Join(failed, ", "))
	}
	return nil
}

// gitOutput runs a git command in dir and returns its trimmed stdout.
func gitOutput(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// relativizeGitdirs rewrites each worktree's .git file to point at the
// repository's worktree admin dir by a relative path, and returns how many
// it changed. `git worktree repair` writes a host-absolute path, which would
// otherwise undo the relative form the container-side hygiene installs (see
// embedded/scripts/git-hygiene.sh) and re-break the worktree the next time
// the repository moves.
//
// Only this worktree->repo pointer is made relative. The repo->worktree
// pointer must stay absolute: git older than 2.48 resolves a relative one
// against the process cwd, and `git worktree prune` would then discard the
// worktree's metadata.
func relativizeGitdirs(root string, dirs []string) int {
	// A separate git dir sits at no fixed offset from the worktrees, so
	// there is no relative form to write; leave those alone.
	common, err := gitOutput(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || common != filepath.Join(root, ".git") {
		return 0
	}
	prefix := common + "/worktrees/"

	n := 0
	for _, rel := range dirs {
		gitfile := filepath.Join(root, rel, ".git")
		data, err := os.ReadFile(gitfile)
		if err != nil {
			continue
		}
		gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
		if !ok {
			continue
		}
		// Anything not under this repo's admin dir (already relative, or
		// pointing elsewhere entirely) is not ours to rewrite.
		admin, ok := strings.CutPrefix(strings.TrimSpace(gitdir), prefix)
		if !ok {
			continue
		}
		// One ".." per path segment of rel, mirroring the shell script.
		up := strings.Repeat("../", strings.Count(filepath.ToSlash(rel), "/")) + ".."
		want := fmt.Sprintf("gitdir: %s/.git/worktrees/%s\n", up, admin)
		if err := os.WriteFile(gitfile, []byte(want), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not rewrite %s: %v\n", gitfile, err)
			continue
		}
		n++
	}
	return n
}

// worktreeDirs returns the paths (relative to root, sorted) of directories
// under the .worktrees/ and .claude/worktrees/ conventions that look like
// linked worktrees — i.e. contain a .git *file*. A nested clone has a .git
// directory instead, and passing one to `git worktree repair` makes it fail
// for every path in the same run.
func worktreeDirs(root string) []string {
	var dirs []string
	for _, base := range []string{".worktrees", filepath.Join(".claude", "worktrees")} {
		entries, err := os.ReadDir(filepath.Join(root, base))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			rel := filepath.Join(base, e.Name())
			info, err := os.Stat(filepath.Join(root, rel, ".git"))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			dirs = append(dirs, rel)
		}
	}
	sort.Strings(dirs)
	return dirs
}
