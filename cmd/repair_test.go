package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorktreeDirs(t *testing.T) {
	dir := t.TempDir()

	// Linked worktree in each convention dir: has a .git file.
	for _, wt := range []string{
		filepath.Join(".worktrees", "feature"),
		filepath.Join(".claude", "worktrees", "task"),
	} {
		if err := os.MkdirAll(filepath.Join(dir, wt), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, wt, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Directory without a .git entry — not a worktree, must be skipped.
	if err := os.MkdirAll(filepath.Join(dir, ".worktrees", "scratch"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Nested clone: .git is a directory, not a worktree link. Passing one to
	// `git worktree repair` makes it fail for every path in the same run, so
	// it must be skipped.
	if err := os.MkdirAll(filepath.Join(dir, ".worktrees", "clone", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Plain file in .worktrees/ — must be skipped.
	if err := os.WriteFile(filepath.Join(dir, ".worktrees", "README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got := worktreeDirs(dir)
	want := []string{
		filepath.Join(".claude", "worktrees", "task"),
		filepath.Join(".worktrees", "feature"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("worktreeDirs() = %v, want %v", got, want)
	}
}

func TestWorktreeDirsNoConventionDirs(t *testing.T) {
	if got := worktreeDirs(t.TempDir()); len(got) != 0 {
		t.Errorf("expected no worktrees in an empty dir, got %v", got)
	}
}

// gitRepo returns a repository with one commit and a worktree per path, plus
// the repository root as git reports it (which on macOS differs from
// t.TempDir()'s symlinked path).
func gitRepo(t *testing.T, worktrees ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=ccodolo", "GIT_AUTHOR_EMAIL=ccodolo@example.com",
			"GIT_COMMITTER_NAME=ccodolo", "GIT_COMMITTER_EMAIL=ccodolo@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f")
	run("commit", "-qm", "init")
	for i, wt := range worktrees {
		run("worktree", "add", "-q", "-b", fmt.Sprintf("b%d", i), wt)
	}
	return dir
}

func TestRelativizeGitdirs(t *testing.T) {
	dir := gitRepo(t,
		filepath.Join(".worktrees", "feature"),
		filepath.Join(".claude", "worktrees", "task"),
	)
	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}

	dirs := worktreeDirs(root)
	if len(dirs) != 2 {
		t.Fatalf("worktreeDirs() = %v, want 2 entries", dirs)
	}
	if n := relativizeGitdirs(root, dirs); n != 2 {
		t.Fatalf("relativizeGitdirs() = %d, want 2", n)
	}

	// Must match what the container-side hygiene script writes, so a
	// host-side repair does not undo it.
	for wt, want := range map[string]string{
		filepath.Join(".worktrees", "feature"):        "gitdir: ../../.git/worktrees/feature\n",
		filepath.Join(".claude", "worktrees", "task"): "gitdir: ../../../.git/worktrees/task\n",
	} {
		got, err := os.ReadFile(filepath.Join(root, wt, ".git"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s/.git = %q, want %q", wt, got, want)
		}
	}

	// The rewritten pointer must still resolve for git run inside the
	// worktree — that is the point of making it relative.
	if _, err := gitOutput(filepath.Join(root, ".worktrees", "feature"), "rev-parse", "--show-toplevel"); err != nil {
		t.Errorf("git inside the worktree broke after the rewrite: %v", err)
	}

	// Already-relative gitdirs are left alone.
	if n := relativizeGitdirs(root, dirs); n != 0 {
		t.Errorf("second run rewrote %d file(s), want 0", n)
	}
}

func TestRepairWorktreesFromSubdirectory(t *testing.T) {
	dir := gitRepo(t, filepath.Join(".worktrees", "feature"))
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// The conventions are anchored at the repository root, so invoking from
	// a subdirectory must still find them rather than silently doing
	// nothing and reporting success.
	old := flagRepairWorkdir
	t.Cleanup(func() { flagRepairWorkdir = old })
	flagRepairWorkdir = sub

	if err := runRepairWorktrees(nil, nil); err != nil {
		t.Fatalf("runRepairWorktrees from a subdirectory: %v", err)
	}

	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".worktrees", "feature", ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "gitdir: ../../.git/worktrees/feature\n"; string(got) != want {
		t.Errorf(".worktrees/feature/.git = %q, want %q", got, want)
	}
}

func TestRepairWorktreesContinuesPastBrokenWorktree(t *testing.T) {
	dir := gitRepo(t,
		filepath.Join(".worktrees", "feature"),
		filepath.Join(".worktrees", "orphan"),
	)
	// A leftover directory from another checkout: git no longer tracks it,
	// so repairing it fails. Repairing the whole set in one git invocation
	// would take the healthy worktree down with it.
	if err := os.RemoveAll(filepath.Join(dir, ".git", "worktrees", "orphan")); err != nil {
		t.Fatal(err)
	}

	old := flagRepairWorkdir
	t.Cleanup(func() { flagRepairWorkdir = old })
	flagRepairWorkdir = dir

	if err := runRepairWorktrees(nil, nil); err == nil {
		t.Error("expected an error naming the worktree that could not be repaired")
	}

	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".worktrees", "feature", ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "gitdir: ../../.git/worktrees/feature\n"; string(got) != want {
		t.Errorf("healthy worktree was left unrepaired: .git = %q, want %q", got, want)
	}
}

func TestRepairWorktreesSkipsNestedClone(t *testing.T) {
	dir := gitRepo(t, filepath.Join(".worktrees", "feature"))
	// A nested clone's .git is a directory. Passing one to
	// `git worktree repair` makes it fail for every path in the same run,
	// which would abort the repair of the real worktree beside it.
	if err := os.MkdirAll(filepath.Join(dir, ".worktrees", "nested", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := flagRepairWorkdir
	t.Cleanup(func() { flagRepairWorkdir = old })
	flagRepairWorkdir = dir

	if err := runRepairWorktrees(nil, nil); err != nil {
		t.Fatalf("a nested clone must not fail the repair: %v", err)
	}
}
