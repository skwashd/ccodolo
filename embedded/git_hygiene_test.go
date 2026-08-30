package embedded

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The hygiene script is the container-side half of the worktree handling
// whose host-side half lives in cmd/repair.go. The two are separate
// implementations of one convention, so each needs its own coverage to keep
// them from drifting apart.

// hygieneScript materialises the embedded script so sh can run it.
func hygieneScript(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	data, err := Scripts.ReadFile("scripts/git-hygiene.sh")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "git-hygiene.sh")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// gitEnv isolates the fixture from the developer's own git configuration.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=ccodolo", "GIT_AUTHOR_EMAIL=ccodolo@example.com",
		"GIT_COMMITTER_NAME=ccodolo", "GIT_COMMITTER_EMAIL=ccodolo@example.com",
	)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRepo returns a repository with one commit and a worktree per path.
func newRepo(t *testing.T, worktrees ...string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "f")
	git(t, dir, "commit", "-qm", "init")
	for i, wt := range worktrees {
		git(t, dir, "worktree", "add", "-q", "-b", fmt.Sprintf("b%d", i), wt)
	}
	return dir
}

// runHygiene runs the script in repo and returns its combined output.
func runHygiene(t *testing.T, repo string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", hygieneScript(t))
	cmd.Dir = repo
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGitHygieneExcludeKeepsExistingPatterns(t *testing.T) {
	repo := newRepo(t)
	exclude := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	// No trailing newline: appending naively glues the new pattern onto
	// build/, silently disabling it.
	if err := os.WriteFile(exclude, []byte("*.log\nbuild/"), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, err := runHygiene(t, repo); err != nil {
		t.Fatalf("hygiene failed: %v\n%s", err, out)
	}

	got := readFile(t, exclude)
	for _, want := range []string{"*.log\n", "build/\n", "/.worktrees/\n", "/.claude/worktrees/\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("exclude is missing %q; got:\n%s", want, got)
		}
	}

	// Second run must not append the patterns again.
	if out, err := runHygiene(t, repo); err != nil {
		t.Fatalf("second hygiene run failed: %v\n%s", err, out)
	}
	if after := readFile(t, exclude); after != got {
		t.Errorf("hygiene is not idempotent:\nfirst:\n%s\nsecond:\n%s", got, after)
	}
}

func TestGitHygieneExcludesBothConventions(t *testing.T) {
	repo := newRepo(t,
		filepath.Join(".worktrees", "feature"),
		filepath.Join(".claude", "worktrees", "task"),
	)
	if out, err := runHygiene(t, repo); err != nil {
		t.Fatalf("hygiene failed: %v\n%s", err, out)
	}

	cmd := exec.Command("git", "-C", repo, "status", "--short")
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Errorf("worktrees should not show in git status, got:\n%s", out)
	}
}

func TestGitHygieneRewritesGitdirsRelative(t *testing.T) {
	repo := newRepo(t,
		filepath.Join(".worktrees", "feature"),
		filepath.Join(".claude", "worktrees", "task"),
	)
	if out, err := runHygiene(t, repo); err != nil {
		t.Fatalf("hygiene failed: %v\n%s", err, out)
	}

	// The depth must follow the worktree's own location.
	for wt, want := range map[string]string{
		filepath.Join(".worktrees", "feature"):        "gitdir: ../../.git/worktrees/feature\n",
		filepath.Join(".claude", "worktrees", "task"): "gitdir: ../../../.git/worktrees/task\n",
	} {
		if got := readFile(t, filepath.Join(repo, wt, ".git")); got != want {
			t.Errorf("%s/.git = %q, want %q", wt, got, want)
		}
	}

	// The rewritten pointer must still resolve for git run inside the
	// worktree — that is the whole point of making it relative.
	git(t, filepath.Join(repo, ".worktrees", "feature"), "rev-parse", "--show-toplevel")
}

func TestGitHygieneSkipsNonWorktreeDirs(t *testing.T) {
	repo := newRepo(t, filepath.Join(".worktrees", "feature"))
	// A nested clone: its .git is a directory. Passing one to
	// `git worktree repair` makes it fail for every path in the same run.
	if err := os.MkdirAll(filepath.Join(repo, ".worktrees", "nested", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory with no .git at all.
	if err := os.MkdirAll(filepath.Join(repo, ".worktrees", "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runHygiene(t, repo)
	if err != nil {
		t.Fatalf("hygiene failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "error:") {
		t.Errorf("hygiene emitted git errors for a non-worktree directory:\n%s", out)
	}
	// The real worktree alongside it must still be handled.
	want := "gitdir: ../../.git/worktrees/feature\n"
	if got := readFile(t, filepath.Join(repo, ".worktrees", "feature", ".git")); got != want {
		t.Errorf(".worktrees/feature/.git = %q, want %q", got, want)
	}
}

func TestGitHygieneReportsRepairFailure(t *testing.T) {
	repo := newRepo(t, filepath.Join(".worktrees", "feature"))
	// Losing the admin dir makes this worktree unrepairable. The startup
	// runner's warning depends on the script's exit status, so a swallowed
	// failure would launch the agent with silently broken worktrees.
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "feature")); err != nil {
		t.Fatal(err)
	}

	if _, err := runHygiene(t, repo); err == nil {
		t.Error("expected a non-zero exit when git worktree repair fails")
	}
}

func TestGitHygieneRepairsHealthyWorktreesDespiteBrokenOne(t *testing.T) {
	repo := newRepo(t,
		filepath.Join(".worktrees", "feature"),
		filepath.Join(".worktrees", "orphan"),
	)
	// A leftover directory from another checkout: git no longer tracks it,
	// so repairing it fails. Repairing the whole set in one git invocation
	// would take the healthy worktree down with it.
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "orphan")); err != nil {
		t.Fatal(err)
	}

	if _, err := runHygiene(t, repo); err == nil {
		t.Error("expected a non-zero exit when one worktree cannot be repaired")
	}

	want := "gitdir: ../../.git/worktrees/feature\n"
	if got := readFile(t, filepath.Join(repo, ".worktrees", "feature", ".git")); got != want {
		t.Errorf("healthy worktree was left unrepaired: .git = %q, want %q", got, want)
	}
}

func TestGitHygieneNoWorktreesSucceeds(t *testing.T) {
	if out, err := runHygiene(t, newRepo(t)); err != nil {
		t.Errorf("a repo with no worktrees is not a failure: %v\n%s", err, out)
	}
}

func TestGitHygieneOutsideRepoSucceeds(t *testing.T) {
	if out, err := runHygiene(t, t.TempDir()); err != nil {
		t.Errorf("a non-repo directory is not a failure: %v\n%s", err, out)
	}
}
