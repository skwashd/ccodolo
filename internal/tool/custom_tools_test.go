package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setCatalogForTest replaces the cached effective catalog with the given
// tools and registers a cleanup that restores the default behavior so the
// next caller sees builtinCatalog (because TestMain leaves
// skipCustomToolsForTest = true).
func setCatalogForTest(t *testing.T, tools []Tool) {
	t.Helper()
	catalogMu.Lock()
	cachedCatalog = tools
	catalogLoaded = true
	catalogMu.Unlock()
	t.Cleanup(resetCustomToolsForTest)
}

// ── loadCustomToolsFrom ───────────────────────────────

func TestLoadCustomToolsFromMissingFile(t *testing.T) {
	_, err := loadCustomToolsFrom(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got %v", err)
	}
}

func TestLoadCustomToolsFromEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-tools.json")
	if err := os.WriteFile(path, []byte("   \n  "), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCustomToolsFrom(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error to mention 'empty', got %v", err)
	}
}

func TestLoadCustomToolsFromCorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-tools.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadCustomToolsFrom(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestLoadCustomToolsFromValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-tools.json")
	body := `{
		"ignore": ["ruby", "php"],
		"tools": [
			{
				"name": "htop",
				"description": "interactive process viewer",
				"instructions": ["RUN apt update && apt install -y htop"]
			},
			{
				"name": "internal-cli",
				"source_image": "registry.acme.example/tools/internal-cli",
				"default_tag": "1.2.3",
				"instructions": ["COPY --from=%s /usr/bin/internal-cli /usr/local/bin/internal-cli"]
			}
		]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := loadCustomToolsFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(f.Tools))
	}
	if f.Tools[0].Name != "htop" {
		t.Errorf("expected first tool 'htop', got %q", f.Tools[0].Name)
	}
	if len(f.Tools[0].Instructions) != 1 {
		t.Errorf("expected 1 instruction for htop, got %d", len(f.Tools[0].Instructions))
	}
	if f.Tools[1].SourceImage != "registry.acme.example/tools/internal-cli" {
		t.Errorf("source_image not parsed correctly: %q", f.Tools[1].SourceImage)
	}
	if len(f.Ignore) != 2 || f.Ignore[0] != "ruby" || f.Ignore[1] != "php" {
		t.Errorf("ignore list not parsed correctly: %v", f.Ignore)
	}
}

func TestLoadCustomToolsFromUnknownKeysTolerated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-tools.json")
	body := `{
		"version": "1.0",
		"tools": [{"name": "htop", "instructions": ["RUN true"]}]
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := loadCustomToolsFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(f.Tools))
	}
}

// ── mergeCatalog ─────────────────────────────────────

func makeBuiltins() []Tool {
	return []Tool{
		{Name: "python", Category: "runtime", Instructions: []string{"RUN echo python"}},
		{Name: "nodejs", Category: "runtime", Instructions: []string{"RUN echo nodejs"}},
		{Name: "ruby", Category: "runtime", Instructions: []string{"RUN echo ruby"}},
	}
}

func TestMergeAddNewTool(t *testing.T) {
	custom := []Tool{
		{Name: "htop", Category: "ignored-by-design", Instructions: []string{"RUN true"}},
	}
	merged, warnings := mergeCatalog(makeBuiltins(), custom, nil)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if len(merged) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(merged))
	}
	got := merged[3]
	if got.Name != "htop" {
		t.Errorf("expected htop at index 3, got %q", got.Name)
	}
	if got.Category != "custom" {
		t.Errorf("expected category 'custom', got %q (user-supplied 'ignored-by-design' must be discarded)", got.Category)
	}
}

func TestMergeOverrideBuiltin(t *testing.T) {
	custom := []Tool{
		{
			Name:         "python",
			Category:     "runtime", // should be overwritten to "custom"
			DefaultTag:   "3.11-slim",
			Instructions: []string{"RUN echo overridden python"},
		},
	}
	merged, warnings := mergeCatalog(makeBuiltins(), custom, nil)
	if len(merged) != 3 {
		t.Fatalf("expected 3 tools (override does not add), got %d", len(merged))
	}
	var got Tool
	for _, m := range merged {
		if m.Name == "python" {
			got = m
		}
	}
	if got.DefaultTag != "3.11-slim" {
		t.Errorf("expected overridden DefaultTag '3.11-slim', got %q", got.DefaultTag)
	}
	if got.Category != "custom" {
		t.Errorf("override must land in 'custom' category, got %q", got.Category)
	}
	if got.Instructions[0] != "RUN echo overridden python" {
		t.Errorf("instructions not overridden, got %v", got.Instructions)
	}
	// Should produce one warning about the override.
	foundOverrideWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "overriding built-in") && strings.Contains(w, "python") {
			foundOverrideWarning = true
		}
	}
	if !foundOverrideWarning {
		t.Errorf("expected an override warning, got %v", warnings)
	}
}

func TestMergeIgnoreBuiltin(t *testing.T) {
	merged, warnings := mergeCatalog(makeBuiltins(), nil, []string{"ruby"})
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 tools after ignore, got %d", len(merged))
	}
	for _, m := range merged {
		if m.Name == "ruby" {
			t.Error("ruby should have been removed")
		}
	}
}

func TestMergeIgnoreThenReadd(t *testing.T) {
	custom := []Tool{
		{Name: "python", DefaultTag: "3.11", Instructions: []string{"RUN echo readded"}},
	}
	merged, warnings := mergeCatalog(makeBuiltins(), custom, []string{"python"})
	if len(merged) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(merged))
	}
	var got Tool
	for _, m := range merged {
		if m.Name == "python" {
			got = m
		}
	}
	if got.DefaultTag != "3.11" {
		t.Errorf("expected re-added python with tag '3.11', got %q", got.DefaultTag)
	}
	if got.Category != "custom" {
		t.Errorf("expected category 'custom', got %q", got.Category)
	}
	// Should NOT produce an "overriding built-in" warning (the user explicitly ignored it first).
	for _, w := range warnings {
		if strings.Contains(w, "overriding built-in") {
			t.Errorf("expected no override warning when name was explicitly ignored, got %v", warnings)
		}
	}
}

func TestMergeIgnoreUnknownName(t *testing.T) {
	merged, warnings := mergeCatalog(makeBuiltins(), nil, []string{"does-not-exist"})
	if len(merged) != 3 {
		t.Errorf("merged catalog should be unchanged, got %d tools", len(merged))
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "does-not-exist") {
		t.Errorf("warning should mention the unknown name, got %q", warnings[0])
	}
}

func TestMergeRejectsEmptyName(t *testing.T) {
	custom := []Tool{
		{Name: "  ", Instructions: []string{"RUN true"}},
		{Name: "valid", Instructions: []string{"RUN true"}},
	}
	merged, warnings := mergeCatalog(makeBuiltins(), custom, nil)
	if len(merged) != 4 {
		t.Errorf("expected 4 tools (empty-name rejected, valid added), got %d", len(merged))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "empty name") {
		t.Errorf("expected one empty-name warning, got %v", warnings)
	}
}

func TestMergeRejectsEmptyInstructions(t *testing.T) {
	custom := []Tool{
		{Name: "broken", Instructions: nil},
		{Name: "valid", Instructions: []string{"RUN true"}},
	}
	merged, warnings := mergeCatalog(makeBuiltins(), custom, nil)
	if len(merged) != 4 {
		t.Errorf("expected 4 tools (broken rejected, valid added), got %d", len(merged))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "no instructions") {
		t.Errorf("expected one no-instructions warning, got %v", warnings)
	}
}

func TestMergeDuplicateCustomNames(t *testing.T) {
	custom := []Tool{
		{Name: "htop", Instructions: []string{"RUN echo first"}},
		{Name: "htop", Instructions: []string{"RUN echo second"}},
	}
	merged, warnings := mergeCatalog(makeBuiltins(), custom, nil)
	if len(merged) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(merged))
	}
	var got Tool
	for _, m := range merged {
		if m.Name == "htop" {
			got = m
		}
	}
	if got.Instructions[0] != "RUN echo second" {
		t.Errorf("expected last definition to win, got %v", got.Instructions)
	}
	foundDup := false
	for _, w := range warnings {
		if strings.Contains(w, "more than once") && strings.Contains(w, "htop") {
			foundDup = true
		}
	}
	if !foundDup {
		t.Errorf("expected duplicate-name warning, got %v", warnings)
	}
}

func TestMergeForcesCustomCategoryEvenWhenSet(t *testing.T) {
	custom := []Tool{
		{Name: "explicit", Category: "runtime", Instructions: []string{"RUN true"}},
	}
	merged, _ := mergeCatalog(makeBuiltins(), custom, nil)
	for _, m := range merged {
		if m.Name == "explicit" && m.Category != "custom" {
			t.Errorf("expected category 'custom' (user 'runtime' must be discarded), got %q", m.Category)
		}
	}
}

// ── Integration with Resolve ─────────────────────────

func TestIntegrationOverridePython(t *testing.T) {
	custom := []Tool{
		{
			Name:         "python",
			DefaultTag:   "3.11-slim",
			TagSuffix:    "-slim",
			SourceImage:  "public.ecr.aws/docker/library/python",
			Instructions: []string{"COPY --from=%s /usr/local/bin/python3 /usr/local/bin/"},
		},
	}
	merged, _ := mergeCatalog(builtinCatalog, custom, nil)
	setCatalogForTest(t, merged)

	resolved, err := Resolve([]ToolSelection{{Name: "python"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Name != "python" {
		t.Fatalf("expected python, got %+v", resolved)
	}
	if resolved[0].Tag != "3.11-slim" {
		t.Errorf("expected tag '3.11-slim' from override, got %q", resolved[0].Tag)
	}
}

func TestIntegrationIgnoreNodejsBreaksYarn(t *testing.T) {
	merged, _ := mergeCatalog(builtinCatalog, nil, []string{"nodejs"})
	setCatalogForTest(t, merged)

	_, err := Resolve([]ToolSelection{{Name: "yarn"}})
	if err == nil {
		t.Fatal("expected error resolving yarn after nodejs ignored")
	}
	if !strings.Contains(err.Error(), "nodejs") {
		t.Errorf("expected error to mention nodejs, got %v", err)
	}
}

func TestIntegrationIgnoreAndReplaceNodejs(t *testing.T) {
	custom := []Tool{
		{
			Name:         "nodejs",
			DefaultTag:   "20-slim",
			TagSuffix:    "-slim",
			SourceImage:  "public.ecr.aws/docker/library/node",
			Instructions: []string{"COPY --from=%s /usr/local/bin/node /usr/local/bin/node"},
		},
	}
	merged, _ := mergeCatalog(builtinCatalog, custom, []string{"nodejs"})
	setCatalogForTest(t, merged)

	resolved, err := Resolve([]ToolSelection{{Name: "yarn"}})
	if err != nil {
		t.Fatalf("unexpected error after ignore+replace: %v", err)
	}
	// yarn depends on nodejs, so both should be in the resolved list.
	var nodeTag string
	foundYarn := false
	for _, rt := range resolved {
		if rt.Name == "nodejs" {
			nodeTag = rt.Tag
		}
		if rt.Name == "yarn" {
			foundYarn = true
		}
	}
	if nodeTag != "20-slim" {
		t.Errorf("expected replaced nodejs with tag '20-slim', got %q", nodeTag)
	}
	if !foundYarn {
		t.Error("yarn missing from resolved tools")
	}
}

func TestIntegrationCustomToolDependsOnBuiltin(t *testing.T) {
	custom := []Tool{
		{
			Name:         "internal-cli",
			Dependencies: []string{"nodejs"},
			Instructions: []string{"RUN npm install -g internal-cli"},
		},
	}
	merged, _ := mergeCatalog(builtinCatalog, custom, nil)
	setCatalogForTest(t, merged)

	resolved, err := Resolve([]ToolSelection{{Name: "internal-cli"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	foundNode, foundCLI := false, false
	for _, rt := range resolved {
		if rt.Name == "nodejs" {
			foundNode = true
		}
		if rt.Name == "internal-cli" {
			foundCLI = true
		}
	}
	if !foundNode {
		t.Error("expected nodejs to be auto-included via custom tool dependency")
	}
	if !foundCLI {
		t.Error("internal-cli missing from resolved tools")
	}
}

// ── Categories ───────────────────────────────────────

func TestCategoryOrderWithoutCustomTools(t *testing.T) {
	// TestMain leaves the catalog at builtinCatalog (no custom tools).
	resetCustomToolsForTest()
	order := CategoryOrder()
	for _, cat := range order {
		if cat == "custom" {
			t.Errorf("custom should not appear in CategoryOrder when no custom tools loaded, got %v", order)
		}
	}
}

func TestCategoryOrderWithCustomTools(t *testing.T) {
	merged := append([]Tool{}, builtinCatalog...)
	merged = append(merged, Tool{Name: "fake", Category: "custom", Instructions: []string{"RUN true"}})
	setCatalogForTest(t, merged)

	order := CategoryOrder()
	if len(order) == 0 || order[len(order)-1] != "custom" {
		t.Errorf("expected 'custom' appended to CategoryOrder, got %v", order)
	}
}

func TestCategoryLabelCustom(t *testing.T) {
	if got := CategoryLabel("custom"); got != "Custom" {
		t.Errorf("CategoryLabel(\"custom\") = %q, want %q", got, "Custom")
	}
}
