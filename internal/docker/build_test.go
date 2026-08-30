package docker

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/skwashd/ccodolo/embedded"
	"github.com/skwashd/ccodolo/internal/config"
)

func TestWriteEmbeddedTreeStagesScripts(t *testing.T) {
	tmp := t.TempDir()
	if err := writeEmbeddedTree(embedded.Scripts, "scripts", tmp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "scripts", "git-hygiene.sh"))
	if err != nil {
		t.Fatalf("expected git-hygiene.sh in the build context: %v", err)
	}
	if !strings.Contains(string(data), "git worktree repair") {
		t.Error("expected the staged git-hygiene.sh to contain the repair step")
	}
}

// setupCommonDir creates <projectPath>/common with the given fixture files
// (relative paths) and returns projectPath.
func setupCommonDir(t *testing.T, files map[string]string) string {
	t.Helper()
	projectPath := t.TempDir()
	commonDir := filepath.Join(projectPath, "common")
	for rel, content := range files {
		full := filepath.Join(commonDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return projectPath
}

func TestResolveStepFilesPlainCopy(t *testing.T) {
	projectPath := setupCommonDir(t, map[string]string{"g.sh": "echo hi"})

	files, err := resolveStepFiles("COPY g.sh /opt/g.sh", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 staged file, got %d", len(files))
	}
	if files[0].RelPath != "g.sh" {
		t.Errorf("expected RelPath 'g.sh', got %q", files[0].RelPath)
	}
	wantSrc := filepath.Join(projectPath, "common", "g.sh")
	if files[0].SrcPath != wantSrc {
		t.Errorf("expected SrcPath %q, got %q", wantSrc, files[0].SrcPath)
	}
}

func TestResolveStepFilesSkipsFlags(t *testing.T) {
	projectPath := setupCommonDir(t, map[string]string{"g.sh": "echo hi"})

	files, err := resolveStepFiles("COPY --chown=coder:coder g.sh /opt/g.sh", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 staged file, got %d", len(files))
	}
	if files[0].RelPath != "g.sh" {
		t.Errorf("expected RelPath 'g.sh', got %q", files[0].RelPath)
	}
}

func TestResolveStepFilesMultiSource(t *testing.T) {
	projectPath := setupCommonDir(t, map[string]string{
		"a.txt": "a",
		"b.txt": "b",
	})

	files, err := resolveStepFiles("COPY a.txt b.txt /opt/", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 staged files, got %d", len(files))
	}
	var rels []string
	for _, f := range files {
		rels = append(rels, f.RelPath)
	}
	sort.Strings(rels)
	if rels[0] != "a.txt" || rels[1] != "b.txt" {
		t.Errorf("expected [a.txt b.txt], got %v", rels)
	}
}

func TestResolveStepFilesFromFlagSkipsStep(t *testing.T) {
	// No fixtures at all — if --from= weren't skipped, this would error
	// with "not found".
	projectPath := t.TempDir()

	files, err := resolveStepFiles("COPY --from=builder /app/bin /usr/local/bin", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files != nil {
		t.Errorf("expected no staged files for --from= step, got %v", files)
	}
}

func TestResolveStepFilesGlob(t *testing.T) {
	projectPath := setupCommonDir(t, map[string]string{
		"scripts/a.sh":  "a",
		"scripts/b.sh":  "b",
		"scripts/c.txt": "c",
	})

	files, err := resolveStepFiles("COPY scripts/*.sh /opt/", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 staged files (glob *.sh), got %d: %v", len(files), files)
	}
	var rels []string
	for _, f := range files {
		rels = append(rels, f.RelPath)
	}
	sort.Strings(rels)
	if rels[0] != "scripts/a.sh" || rels[1] != "scripts/b.sh" {
		t.Errorf("expected [scripts/a.sh scripts/b.sh], got %v", rels)
	}
}

func TestResolveStepFilesJSONArrayRejected(t *testing.T) {
	projectPath := t.TempDir()

	_, err := resolveStepFiles(`COPY ["a.txt", "b.txt", "/dst/"]`, config.OriginProject, projectPath)
	if err == nil {
		t.Fatal("expected error for JSON array form")
	}
	if !strings.Contains(err.Error(), "JSON array form") {
		t.Errorf("expected error to mention JSON array form, got %q", err.Error())
	}
}

func TestResolveStepFilesAbsoluteSourceRejected(t *testing.T) {
	projectPath := t.TempDir()

	_, err := resolveStepFiles("COPY /etc/passwd /dst/passwd", config.OriginProject, projectPath)
	if err == nil {
		t.Fatal("expected error for absolute source path")
	}
	if !strings.Contains(err.Error(), "must be a relative path") {
		t.Errorf("expected error to mention relative path requirement, got %q", err.Error())
	}
}

func TestResolveStepFilesPathTraversalRejected(t *testing.T) {
	projectPath := t.TempDir()

	_, err := resolveStepFiles("COPY ../../etc/passwd /dst/passwd", config.OriginProject, projectPath)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "resolves outside") {
		t.Errorf("expected error to mention resolving outside the common dir, got %q", err.Error())
	}
}

func TestResolveStepFilesMissingSourceNamesSearchedDir(t *testing.T) {
	projectPath := t.TempDir()

	_, err := resolveStepFiles("COPY nope.txt /dst/nope.txt", config.OriginProject, projectPath)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	wantDir := filepath.Join(projectPath, "common")
	if !strings.Contains(err.Error(), wantDir) {
		t.Errorf("expected error to name searched directory %q, got %q", wantDir, err.Error())
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to say 'not found', got %q", err.Error())
	}
}

func TestResolveStepFilesNonCopyAddIgnored(t *testing.T) {
	projectPath := t.TempDir()

	files, err := resolveStepFiles("RUN echo hello", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files != nil {
		t.Errorf("expected no staged files for RUN step, got %v", files)
	}
}

func TestResolveStepFilesDirectorySource(t *testing.T) {
	projectPath := setupCommonDir(t, map[string]string{
		"shared/one.sh":        "one",
		"shared/nested/two.sh": "two",
	})

	files, err := resolveStepFiles("COPY shared/ /opt/shared", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 staged entry (directory), got %d: %v", len(files), files)
	}
	if files[0].RelPath != "shared" {
		t.Errorf("expected RelPath 'shared', got %q", files[0].RelPath)
	}
	info, err := os.Stat(files[0].SrcPath)
	if err != nil || !info.IsDir() {
		t.Errorf("expected SrcPath to be a directory: %v", err)
	}
}

// TestOriginCommonDirMapping verifies global-origin steps resolve against
// the global common dir and project-origin steps resolve against the
// project's common dir — the core fix for both original bugs.
func TestOriginCommonDirMapping(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	globalCommon := filepath.Join(fakeHome, ".ccodolo", "common")
	if err := os.MkdirAll(globalCommon, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalCommon, "g.sh"), []byte("global"), 0o644); err != nil {
		t.Fatal(err)
	}

	projectPath := setupCommonDir(t, map[string]string{"p.sh": "project"})

	// Global-origin step resolves against the global common dir.
	globalFiles, err := resolveStepFiles("COPY g.sh /opt/g.sh", config.OriginGlobal, projectPath)
	if err != nil {
		t.Fatalf("unexpected error resolving global step: %v", err)
	}
	if len(globalFiles) != 1 || globalFiles[0].SrcPath != filepath.Join(globalCommon, "g.sh") {
		t.Errorf("expected global step to resolve against %s, got %v", globalCommon, globalFiles)
	}

	// Project-origin step resolves against the project's common dir.
	projectFiles, err := resolveStepFiles("COPY p.sh /opt/p.sh", config.OriginProject, projectPath)
	if err != nil {
		t.Fatalf("unexpected error resolving project step: %v", err)
	}
	wantProjectSrc := filepath.Join(projectPath, "common", "p.sh")
	if len(projectFiles) != 1 || projectFiles[0].SrcPath != wantProjectSrc {
		t.Errorf("expected project step to resolve against %s, got %v", wantProjectSrc, projectFiles)
	}

	// A project-origin step referencing a global-only file fails, naming
	// the project's common dir (not the global one) as the searched dir.
	_, err = resolveStepFiles("COPY g.sh /opt/g.sh", config.OriginProject, projectPath)
	if err == nil {
		t.Fatal("expected error: g.sh only exists in the global common dir")
	}
	if !strings.Contains(err.Error(), filepath.Join(projectPath, "common")) {
		t.Errorf("expected error to name the project's common dir, got %q", err.Error())
	}
}

// TestResolveBuildContextFilesSortedAndCombinesRootAndCustom verifies
// resolveBuildContextFiles walks both root_steps and custom_steps and
// returns a deterministically sorted result.
func TestResolveBuildContextFilesSortedAndCombinesRootAndCustom(t *testing.T) {
	projectPath := setupCommonDir(t, map[string]string{
		"z.txt": "z",
		"a.txt": "a",
	})

	cfg := &config.Config{
		Build: config.BuildConfig{
			RootSteps:   []string{"COPY z.txt /dst/z.txt"},
			CustomSteps: []string{"COPY a.txt /dst/a.txt"},
		},
	}

	files, err := resolveBuildContextFiles(cfg, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 staged files, got %d", len(files))
	}
	if files[0].RelPath != "a.txt" || files[1].RelPath != "z.txt" {
		t.Errorf("expected sorted [a.txt z.txt], got [%s %s]", files[0].RelPath, files[1].RelPath)
	}
}

func TestStageBuildContextFilesPreservesMode(t *testing.T) {
	src := t.TempDir()
	scriptPath := filepath.Join(src, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	files := []stagedFile{{RelPath: "bin/run.sh", SrcPath: scriptPath}}
	if err := stageBuildContextFiles(files, tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(filepath.Join(tmpDir, "bin", "run.sh"))
	if err != nil {
		t.Fatalf("expected staged file to exist: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected mode 0o755, got %o", info.Mode().Perm())
	}
}

func TestStageBuildContextFilesDirectorySource(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "shared", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "shared", "one.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "shared", "nested", "two.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	files := []stagedFile{{RelPath: "shared", SrcPath: filepath.Join(src, "shared")}}
	if err := stageBuildContextFiles(files, tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "shared", "nested", "two.txt"))
	if err != nil {
		t.Fatalf("expected nested file to be staged: %v", err)
	}
	if string(data) != "two" {
		t.Errorf("expected 'two', got %q", string(data))
	}
}

func TestBuildArgs(t *testing.T) {
	want := []string{
		"build",
		"--progress=plain",
		"--build-arg", "CCODOLO_AGENT=claude",
		"-t", "ccodolo:proj-claude-abcd1234", ".",
	}
	for _, rt := range []Runtime{RuntimeDocker, RuntimeApple} {
		got := buildArgs(rt, "claude", "ccodolo:proj-claude-abcd1234")
		if !reflect.DeepEqual(got, want) {
			t.Errorf("buildArgs(%q) = %q, want %q", rt, got, want)
		}
	}
}
