package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/skwashd/ccodolo/internal/tool"
)

const (
	toolGoPath     = "internal/tool/tool.go"
	toolTestPath   = "internal/tool/tool_test.go"
	dockerfilePath = "embedded/Dockerfile.tmpl"
)

func main() {
	fCheck   := flag.Bool("check", false, "Print a plan table and exit (default mode)")
	fJSON    := flag.Bool("json", false, "Print the plan as JSON and exit")
	fWrite   := flag.Bool("write", false, "Rewrite files and create one git commit per update")
	fOnly    := flag.String("only", "", "Comma-separated list of tool names to restrict to")
	fTimeout := flag.Duration("timeout", 2*time.Minute, "Overall HTTP timeout")
	fAllowUV := flag.Bool("allow-unverified", false, "Include candidates with no publish timestamp")
	flag.Parse()

	// Default mode is -check
	if !*fJSON && !*fWrite {
		*fCheck = true
	}

	root := mustRepoRoot()

	cutoff := buildCutoff()
	ctx, cancel := context.WithTimeout(context.Background(), *fTimeout)
	defer cancel()

	githubToken := os.Getenv("GITHUB_TOKEN")

	// Build the list of all update targets.
	targets := buildTargets(root)

	// Apply -only filter.
	if *fOnly != "" {
		allow := make(map[string]bool)
		for _, n := range strings.Split(*fOnly, ",") {
			allow[strings.TrimSpace(n)] = true
		}
		var filtered []updateTarget
		for _, t := range targets {
			if allow[t.name] {
				filtered = append(filtered, t)
			}
		}
		targets = filtered
	}

	// Collect updates.
	var updates []Update
	for _, tgt := range targets {
		f, err := NewFetcher(tgt.src, githubToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "::warning::%s: no fetcher for source %q: %v\n", tgt.name, tgt.src, err)
			continue
		}
		cands, err := f.Fetch(ctx, tgt.ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "::warning::%s: fetch failed: %v\n", tgt.name, err)
			continue
		}
		// Determine comparison parameters.
		curFullTag := tgt.curFullTag()
		matchSuffix := matchSuffixFor(curFullTag)
		precision := tgt.precision()

		best, ok := PickLatest(curFullTag, matchSuffix, precision, cands, cutoff, *fAllowUV)
		if !ok {
			continue
		}
		newDT := DeriveNewDefaultTag(best, tgt.tagSuffix, tgt.src)
		bump := ClassifyBump(
			func() Version { v, _, _ := ParseVersion(curFullTag); return v }(),
			best.Ver,
		)
		updates = append(updates, Update{
			Name:       tgt.name,
			Source:     tgt.src,
			Ref:        tgt.ref,
			OldTag:     tgt.currentDefaultTag,
			NewTag:     newDT,
			Bump:       bump,
			ReleaseURL: releaseURL(tgt.src, tgt.ref, best.RawTag),
			Files:      tgt.files(root),
		})
	}

	switch {
	case *fJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(updates); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case *fCheck:
		if len(updates) == 0 {
			fmt.Println("All tools are up to date.")
			return
		}
		fmt.Printf("%-25s  %-12s  %-14s  %-6s\n", "TOOL", "OLD", "NEW", "BUMP")
		fmt.Println(strings.Repeat("-", 65))
		for _, u := range updates {
			fmt.Printf("%-25s  %-12s  %-14s  %-6s\n", u.Name, u.OldTag, u.NewTag, u.Bump)
		}
	case *fWrite:
		if len(updates) == 0 {
			fmt.Println("Nothing to update.")
			return
		}
		for _, u := range updates {
			if err := applyUpdate(root, u); err != nil {
				fmt.Fprintf(os.Stderr, "error applying %s: %v\n", u.Name, err)
				os.Exit(1)
			}
			fmt.Printf("Bumped %s from %s to %s\n", u.Name, u.OldTag, u.NewTag)
		}
	}
}

// updateTarget is an internal struct driving the plan-build loop.
type updateTarget struct {
	name              string
	src               tool.UpdateSource
	ref               string
	currentDefaultTag string // the DefaultTag value in the source file
	tagSuffix         string // Tool.TagSuffix (may be "")
	filesFunc         func(root string) []string
	rewriteFunc       func(root, oldTag, newTag string) error
}

func (t *updateTarget) curFullTag() string {
	if t.src == tool.UpdateDockerHub {
		return t.currentDefaultTag + t.tagSuffix
	}
	return t.currentDefaultTag
}

func (t *updateTarget) precision() int {
	return inferPrecision(t.curFullTag())
}

func (t *updateTarget) files(root string) []string {
	if t.filesFunc != nil {
		return t.filesFunc(root)
	}
	return []string{filepath.Join(root, toolGoPath)}
}

// buildTargets constructs the complete list of auto-updatable targets.
func buildTargets(root string) []updateTarget {
	var targets []updateTarget

	// Auto-updatable tools from the catalog.
	for _, t := range tool.AutoUpdatable() {
		tc := t // capture
		targets = append(targets, updateTarget{
			name:              tc.Name,
			src:               tc.UpdateSource,
			ref:               tc.UpdateRef,
			currentDefaultTag: tc.DefaultTag,
			tagSuffix:         tc.TagSuffix,
			filesFunc: func(r string) []string {
				files := []string{filepath.Join(r, toolGoPath)}
				// golang, java, python also need the test file updated.
				if tc.Name == "golang" || tc.Name == "java" || tc.Name == "python" {
					files = append(files, filepath.Join(r, toolTestPath))
				}
				return files
			},
			rewriteFunc: func(r, oldTag, newTag string) error {
				return rewriteToolDefaultTag(r, tc.Name, tc.TagSuffix, oldTag, newTag)
			},
		})
	}

	// Extra target: helm (no DefaultTag, URL rewrite in tool.go).
	targets = append(targets, updateTarget{
		name:              "helm",
		src:               tool.UpdateGitHub,
		ref:               "helm/helm",
		currentDefaultTag: "4.1.4",
		tagSuffix:         "",
		filesFunc:         func(r string) []string { return []string{filepath.Join(r, toolGoPath)} },
		rewriteFunc: func(r, oldTag, newTag string) error {
			path := filepath.Join(r, toolGoPath)
			return rewriteFile(path, func(src []byte) ([]byte, error) {
				return RewriteHelmURL(src, oldTag, newTag)
			})
		},
	})

	// Extra target: zsh-in-docker (ARG in Dockerfile.tmpl).
	targets = append(targets, updateTarget{
		name:              "zsh-in-docker",
		src:               tool.UpdateGitHub,
		ref:               "deluan/zsh-in-docker",
		currentDefaultTag: "1.2.1",
		tagSuffix:         "",
		filesFunc:         func(r string) []string { return []string{filepath.Join(r, dockerfilePath)} },
		rewriteFunc: func(r, oldTag, newTag string) error {
			path := filepath.Join(r, dockerfilePath)
			return rewriteFile(path, func(src []byte) ([]byte, error) {
				return RewriteDockerfileArg(src, "ZSH_IN_DOCKER_VERSION", oldTag, newTag)
			})
		},
	})

	return targets
}

// rewriteToolDefaultTag applies the tool.go DefaultTag rewrite and, for
// golang/java/python, also patches tool_test.go.
func rewriteToolDefaultTag(root, name, tagSuffix, oldTag, newTag string) error {
	toolGoFile := filepath.Join(root, toolGoPath)
	if err := rewriteFile(toolGoFile, func(src []byte) ([]byte, error) {
		return RewriteToolDefaultTag(src, name, oldTag, newTag)
	}); err != nil {
		return fmt.Errorf("rewrite tool.go for %s: %w", name, err)
	}

	// For golang/java/python the test file asserts DefaultVersion() directly.
	// We need to update it in the same commit to keep go test green.
	if name == "golang" || name == "java" || name == "python" {
		// oldVer and newVer are the bare version numbers (DefaultTag, no TagSuffix).
		oldVer := oldTag
		newVer := newTag
		testFile := filepath.Join(root, toolTestPath)
		if err := rewriteFile(testFile, func(src []byte) ([]byte, error) {
			return RewriteTestVersion(src, oldVer, newVer)
		}); err != nil {
			return fmt.Errorf("rewrite tool_test.go for %s: %w", name, err)
		}
	}
	return nil
}

// applyUpdate rewrites the relevant files and creates a git commit.
func applyUpdate(root string, u Update) error {
	tgt := findTarget(root, u.Name)
	if tgt == nil {
		return fmt.Errorf("no target found for %q", u.Name)
	}
	if err := tgt.rewriteFunc(root, u.OldTag, u.NewTag); err != nil {
		return err
	}
	msg := fmt.Sprintf("Bump %s from %s to %s", u.Name, u.OldTag, u.NewTag)
	return gitCommit(u.Files, msg)
}

// findTarget searches the target list by name. Rebuilds the list once per call;
// the list is small (<40 entries) so this is fine.
func findTarget(root string, name string) *updateTarget {
	for _, t := range buildTargets(root) {
		if t.name == name {
			tc := t
			return &tc
		}
	}
	return nil
}
