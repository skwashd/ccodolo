package tool

import (
	"os"
	"strings"
	"testing"
)

// TestMain disables the custom-tools.json loader for the entire test binary
// so a developer's local ~/.ccodolo/custom-tools.json can never affect test
// runs. Tests that exercise the loader call loadCustomToolsFrom and
// mergeCatalog directly with controlled inputs.
func TestMain(m *testing.M) {
	skipCustomToolsForTest = true
	resetCustomToolsForTest()
	os.Exit(m.Run())
}

func TestAllTools(t *testing.T) {
	tools := All()
	if len(tools) != len(builtinCatalog) {
		t.Errorf("expected %d tools, got %d", len(builtinCatalog), len(tools))
	}
}

func TestGet(t *testing.T) {
	t.Run("known tool", func(t *testing.T) {
		tool, err := Get("python")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tool.Name != "python" {
			t.Errorf("expected name 'python', got %q", tool.Name)
		}
		if tool.Category != "runtime" {
			t.Errorf("expected category 'runtime', got %q", tool.Category)
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		_, err := Get("nonexistent")
		if err == nil {
			t.Error("expected error for unknown tool")
		}
	})
}

func TestValid(t *testing.T) {
	if !Valid("python") {
		t.Error("python should be valid")
	}
	if !Valid("nodejs") {
		t.Error("nodejs should be valid")
	}
	if Valid("nonexistent") {
		t.Error("nonexistent should be invalid")
	}
}

func TestByCategory(t *testing.T) {
	groups := ByCategory()
	if len(groups["runtime"]) == 0 {
		t.Error("runtime category should have tools")
	}
	if len(groups["package-manager"]) == 0 {
		t.Error("package-manager category should have tools")
	}
	if len(groups["cloud"]) == 0 {
		t.Error("cloud category should have tools")
	}
	if len(groups["testing"]) == 0 {
		t.Error("testing category should have tools")
	}
	if len(groups["utils"]) == 0 {
		t.Error("utils category should have tools")
	}
}

func TestCategoryOrder(t *testing.T) {
	order := CategoryOrder()
	expected := []string{"runtime", "package-manager", "cloud", "database", "testing", "utils"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d categories, got %d", len(expected), len(order))
	}
	for i, cat := range order {
		if cat != expected[i] {
			t.Errorf("expected category[%d] = %q, got %q", i, expected[i], cat)
		}
	}
}

func TestCategoryLabel(t *testing.T) {
	tests := map[string]string{
		"runtime":         "Language Runtimes",
		"package-manager": "Package Managers",
		"cloud":           "Cloud / IaC",
		"unknown":         "unknown",
	}
	for cat, expected := range tests {
		if got := CategoryLabel(cat); got != expected {
			t.Errorf("CategoryLabel(%q) = %q, want %q", cat, got, expected)
		}
	}
}

func TestResolveSimple(t *testing.T) {
	sels := []ToolSelection{{Name: "python"}}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved tool, got %d", len(resolved))
	}
	if resolved[0].Name != "python" {
		t.Errorf("expected python, got %q", resolved[0].Name)
	}
	if resolved[0].Tag != "3.13-slim" {
		t.Errorf("expected tag 3.13-slim, got %q", resolved[0].Tag)
	}
}

func TestResolveVersionOverride(t *testing.T) {
	// User provides clean version, suffix appended automatically.
	sels := []ToolSelection{{Name: "python", Version: "3.12"}}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Tag != "3.12-slim" {
		t.Errorf("expected tag 3.12-slim, got %q", resolved[0].Tag)
	}
}

func TestResolveVersionOverrideBackwardCompat(t *testing.T) {
	// Full tag with suffix should not get double-suffixed.
	sels := []ToolSelection{{Name: "python", Version: "3.12-slim"}}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Tag != "3.12-slim" {
		t.Errorf("expected tag 3.12-slim, got %q", resolved[0].Tag)
	}
}

func TestResolveVersionOverrideNoSuffix(t *testing.T) {
	// Tool without TagSuffix uses version as-is.
	sels := []ToolSelection{{Name: "golang", Version: "1.23"}}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].Tag != "1.23" {
		t.Errorf("expected tag 1.23, got %q", resolved[0].Tag)
	}
}

func TestDefaultVersion(t *testing.T) {
	python, _ := Get("python")
	if python.DefaultVersion() != "3.13" {
		t.Errorf("expected 3.13, got %q", python.DefaultVersion())
	}
	golang, _ := Get("golang")
	if golang.DefaultVersion() != "1.26" {
		t.Errorf("expected 1.26, got %q", golang.DefaultVersion())
	}
	java, _ := Get("java")
	if java.DefaultVersion() != "21" {
		t.Errorf("expected 21, got %q", java.DefaultVersion())
	}
}

func TestBuildTag(t *testing.T) {
	python, _ := Get("python")
	if python.BuildTag("3.12") != "3.12-slim" {
		t.Errorf("expected 3.12-slim, got %q", python.BuildTag("3.12"))
	}
	// No double suffix.
	if python.BuildTag("3.12-slim") != "3.12-slim" {
		t.Errorf("expected 3.12-slim, got %q", python.BuildTag("3.12-slim"))
	}
	// Tool without suffix.
	golang, _ := Get("golang")
	if golang.BuildTag("1.23") != "1.23" {
		t.Errorf("expected 1.23, got %q", golang.BuildTag("1.23"))
	}
}

func TestResolveDependencies(t *testing.T) {
	sels := []ToolSelection{{Name: "aws-cdk"}}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include nodejs as a dependency.
	if len(resolved) < 2 {
		t.Fatalf("expected at least 2 resolved tools (nodejs + aws-cdk), got %d", len(resolved))
	}

	// nodejs should come before aws-cdk.
	var names []string
	for _, rt := range resolved {
		names = append(names, rt.Name)
	}
	nodejsIdx := -1
	cdkIdx := -1
	for i, name := range names {
		if name == "nodejs" {
			nodejsIdx = i
		}
		if name == "aws-cdk" {
			cdkIdx = i
		}
	}
	if nodejsIdx == -1 {
		t.Error("nodejs should be in resolved tools")
	}
	if cdkIdx == -1 {
		t.Error("aws-cdk should be in resolved tools")
	}
	if nodejsIdx > cdkIdx {
		t.Error("nodejs should come before aws-cdk")
	}
}

func TestResolveUnknownTool(t *testing.T) {
	sels := []ToolSelection{{Name: "nonexistent"}}
	_, err := Resolve(sels)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestResolvedToolImageRef(t *testing.T) {
	rt := ResolvedTool{
		Tool: Tool{SourceImage: "public.ecr.aws/docker/library/python"},
		Tag:  "3.13-slim",
	}
	expected := "public.ecr.aws/docker/library/python:3.13-slim"
	if got := rt.ImageRef(); got != expected {
		t.Errorf("ImageRef() = %q, want %q", got, expected)
	}
}

func TestResolvedToolDockerLines(t *testing.T) {
	sels := []ToolSelection{{Name: "uv"}}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rt := resolved[0]
	if len(rt.DockerLines) == 0 {
		t.Error("uv should have Docker lines")
	}

	// Check that COPY --from lines reference the correct image (uv's current default tag).
	for _, line := range rt.DockerLines {
		if strings.HasPrefix(line, "COPY --from=") {
			if !strings.Contains(line, "ghcr.io/astral-sh/uv:"+rt.Tag) {
				t.Errorf("expected COPY --from to reference uv image, got: %s", line)
			}
		}
	}
}

func TestRenderInstructionsTemplateVersion(t *testing.T) {
	// python uses {{.Version}} in the COPY path. Default (no override) must
	// produce the library path stripped of the -slim suffix.
	resolved, err := Resolve([]ToolSelection{{Name: "python"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rt := resolved[0]
	if rt.Tag != "3.13-slim" {
		t.Fatalf("expected python tag 3.13-slim, got %q", rt.Tag)
	}
	var libLine string
	for _, line := range rt.DockerLines {
		if strings.Contains(line, "/usr/local/lib/python") {
			libLine = line
			break
		}
	}
	if libLine == "" {
		t.Fatal("expected a COPY line referencing /usr/local/lib/python")
	}
	if !strings.Contains(libLine, "/usr/local/lib/python3.13 /usr/local/lib/python3.13") {
		t.Errorf("expected {{.Version}} to expand to 3.13, got: %s", libLine)
	}
	if !strings.Contains(libLine, "public.ecr.aws/docker/library/python:3.13-slim") {
		t.Errorf("expected COPY --from to reference python:3.13-slim, got: %s", libLine)
	}
}

func TestRenderInstructionsTemplateVersionOverride(t *testing.T) {
	// User overrides python to 3.12 — lib path must become python3.12 and
	// the COPY --from must reference python:3.12-slim.
	resolved, err := Resolve([]ToolSelection{{Name: "python", Version: "3.12"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rt := resolved[0]
	var libLine string
	for _, line := range rt.DockerLines {
		if strings.Contains(line, "/usr/local/lib/python") {
			libLine = line
			break
		}
	}
	if libLine == "" {
		t.Fatal("expected a COPY line referencing /usr/local/lib/python")
	}
	if !strings.Contains(libLine, "/usr/local/lib/python3.12 /usr/local/lib/python3.12") {
		t.Errorf("expected {{.Version}} to expand to 3.12, got: %s", libLine)
	}
	if !strings.Contains(libLine, "public.ecr.aws/docker/library/python:3.12-slim") {
		t.Errorf("expected COPY --from to reference python:3.12-slim, got: %s", libLine)
	}
}

func TestRenderInstructionsTemplateTag(t *testing.T) {
	// skills is installed via `npm install -g skills@{{.Tag}}`. The rendered
	// line must contain the default tag with no -suffix stripping.
	resolved, err := Resolve([]ToolSelection{{Name: "skills"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var skillsLine string
	for _, rt := range resolved {
		if rt.Name != "skills" {
			continue
		}
		for _, line := range rt.DockerLines {
			if strings.Contains(line, "skills@") {
				skillsLine = line
			}
		}
	}
	if skillsLine == "" {
		t.Fatal("expected a RUN line installing skills@<tag>")
	}
	skills, _ := Get("skills")
	want := "skills@" + skills.DefaultTag
	if !strings.Contains(skillsLine, want) {
		t.Errorf("expected %q in skills install line, got: %s", want, skillsLine)
	}
}

func TestRenderInstructionsCustomTool(t *testing.T) {
	// Simulate a user-supplied custom tool whose instructions use {{.Tag}}
	// and {{.Version}}. This exercises the same renderInstructions code path
	// that custom-tools.json entries take once merged into the catalog.
	rt := ResolvedTool{
		Tool: Tool{
			Name:        "acme",
			Category:    "custom",
			SourceImage: "registry.acme.example/acme",
			DefaultTag:  "9.8.7-beta",
			TagSuffix:   "-beta",
			Instructions: []string{
				"COPY --from=%s /usr/bin/acme /usr/local/bin/acme",
				"RUN /usr/local/bin/acme --version | grep {{.Version}}",
				"LABEL acme.tag={{.Tag}} acme.ref={{.ImageRef}}",
			},
		},
		Tag: "9.8.7-beta",
	}
	lines, err := rt.renderInstructions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "COPY --from=registry.acme.example/acme:9.8.7-beta /usr/bin/acme /usr/local/bin/acme" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "RUN /usr/local/bin/acme --version | grep 9.8.7" {
		t.Errorf("line 1 = %q", lines[1])
	}
	if lines[2] != "LABEL acme.tag=9.8.7-beta acme.ref=registry.acme.example/acme:9.8.7-beta" {
		t.Errorf("line 2 = %q", lines[2])
	}
}

func TestRenderInstructionsBadTemplate(t *testing.T) {
	// Malformed template in a custom tool surfaces an error from Resolve
	// rather than silently producing broken Dockerfile lines.
	rt := ResolvedTool{
		Tool: Tool{
			Name: "broken",
			Instructions: []string{
				"RUN echo {{.Tag",
			},
		},
		Tag: "1.0.0",
	}
	if _, err := rt.renderInstructions(); err == nil {
		t.Error("expected error for malformed template")
	}
}

func TestResolveNoDuplicates(t *testing.T) {
	// Request nodejs both directly and as a dependency of aws-cdk.
	sels := []ToolSelection{
		{Name: "nodejs"},
		{Name: "aws-cdk"},
	}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, rt := range resolved {
		if rt.Name == "nodejs" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("nodejs should appear exactly once, appeared %d times", count)
	}
}

func TestResolvePlaywrightDependsOnNodejs(t *testing.T) {
	sels := []ToolSelection{{Name: "playwright"}}
	resolved, err := Resolve(sels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var names []string
	for _, rt := range resolved {
		names = append(names, rt.Name)
	}

	// Should auto-include nodejs.
	nodejsFound := false
	for _, n := range names {
		if n == "nodejs" {
			nodejsFound = true
			break
		}
	}
	if !nodejsFound {
		t.Error("playwright should auto-include nodejs as dependency")
	}

	// Should have install instructions.
	for _, rt := range resolved {
		if rt.Name == "playwright" {
			if len(rt.DockerLines) < 2 {
				t.Errorf("expected at least 2 Docker lines for playwright, got %d", len(rt.DockerLines))
			}
		}
	}
}
