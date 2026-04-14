package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
)

// Tool represents a bundled dev tool that can be installed in the container.
//
// JSON tags allow Tool entries to be loaded from ~/.ccodolo/custom-tools.json.
// The Category field is parsed but always overwritten to "custom" for any
// tool loaded from that file.
type Tool struct {
	Name         string            `json:"name"`                   // unique identifier, e.g. "python"
	Category     string            `json:"category,omitempty"`     // "runtime", "package-manager", "cloud", "custom"
	Description  string            `json:"description,omitempty"`  // shown in TUI
	SourceImage  string            `json:"source_image,omitempty"` // Docker image to COPY from, e.g. "public.ecr.aws/docker/library/python"
	DefaultTag   string            `json:"default_tag,omitempty"`  // e.g. "3.13-slim"
	TagSuffix    string            `json:"tag_suffix,omitempty"`   // image variant suffix, e.g. "-slim", appended to user-provided version
	Instructions []string          `json:"instructions"`           // Dockerfile lines (COPY, RUN, ENV)
	Dependencies []string          `json:"dependencies,omitempty"` // other tool names auto-included
	PathEntries  []string          `json:"path_entries,omitempty"` // paths to prepend to PATH in final stage
	EnvVars      map[string]string `json:"env_vars,omitempty"`     // env vars for the final stage
}

// DefaultVersion returns the version part of DefaultTag with the TagSuffix stripped.
func (t Tool) DefaultVersion() string {
	if t.TagSuffix != "" {
		return strings.TrimSuffix(t.DefaultTag, t.TagSuffix)
	}
	return t.DefaultTag
}

// BuildTag constructs the full image tag from a user-provided version,
// appending TagSuffix if set and not already present.
func (t Tool) BuildTag(version string) string {
	if t.TagSuffix != "" && !strings.HasSuffix(version, t.TagSuffix) {
		return version + t.TagSuffix
	}
	return version
}

// builtinCatalog holds the compiled-in tool definitions. The effective
// catalog used by all package functions is the merge of this slice and any
// entries loaded from ~/.ccodolo/custom-tools.json — see effectiveCatalog().
var builtinCatalog = []Tool{
	// ── runtime ───────────────────────────────────────────────────────────
	{
		Name:        "bun",
		Category:    "runtime",
		Description: "Bun runtime",
		SourceImage: "oven/bun",
		DefaultTag:  "1.3.12",
		Instructions: []string{
			"COPY --from=%s /usr/local/bin/bun /usr/local/bin/bun",
			"RUN ln -sf /usr/local/bin/bun /usr/local/bin/bunx",
		},
	},
	{
		Name:        "deno",
		Category:    "runtime",
		Description: "Deno runtime",
		SourceImage: "denoland/deno",
		DefaultTag:  "2.7.12",
		Instructions: []string{
			"COPY --from=%s /usr/bin/deno /usr/local/bin/deno",
		},
	},
	{
		Name:        "dotnet",
		Category:    "runtime",
		Description: ".NET SDK 9.0",
		SourceImage: "mcr.microsoft.com/dotnet/sdk",
		DefaultTag:  "9.0",
		Instructions: []string{
			"COPY --from=%s /usr/share/dotnet /usr/share/dotnet",
		},
		PathEntries: []string{"/usr/share/dotnet"},
		EnvVars: map[string]string{
			"DOTNET_ROOT": "/usr/share/dotnet",
		},
	},
	{
		Name:        "golang",
		Category:    "runtime",
		Description: "Go",
		SourceImage: "public.ecr.aws/docker/library/golang",
		DefaultTag:  "1.24",
		Instructions: []string{
			"COPY --from=%s /usr/local/go /usr/local/go",
			"ENV PATH=\"/usr/local/go/bin:$PATH\"",
		},
		PathEntries: []string{"/usr/local/go/bin"},
	},
	{
		Name:        "java",
		Category:    "runtime",
		Description: "Java (Eclipse Temurin) JDK",
		SourceImage: "public.ecr.aws/docker/library/eclipse-temurin",
		DefaultTag:  "21",
		TagSuffix:   "-jdk",
		Instructions: []string{
			"COPY --from=%s /opt/java/openjdk /opt/java/openjdk",
		},
		PathEntries: []string{"/opt/java/openjdk/bin"},
		EnvVars: map[string]string{
			"JAVA_HOME": "/opt/java/openjdk",
		},
	},
	{
		Name:        "nodejs",
		Category:    "runtime",
		Description: "Node.js",
		SourceImage: "public.ecr.aws/docker/library/node",
		DefaultTag:  "22",
		TagSuffix:   "-slim",
		Instructions: []string{
			"COPY --from=%s /usr/local/bin/node /usr/local/bin/node",
			"COPY --from=%s /usr/local/lib/node_modules /usr/local/lib/node_modules",
			`RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \` + "\n" +
				`  && ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx`,
		},
	},
	{
		Name:        "php",
		Category:    "runtime",
		Description: "PHP",
		SourceImage: "public.ecr.aws/docker/library/php",
		DefaultTag:  "8.4",
		TagSuffix:   "-cli",
		Instructions: []string{
			"COPY --from=%s /usr/local/bin/php* /usr/local/bin/",
			"COPY --from=%s /usr/local/lib/php /usr/local/lib/php",
			"COPY --from=%s /usr/local/etc/php /usr/local/etc/php",
		},
	},
	{
		Name:        "python",
		Category:    "runtime",
		Description: "Python",
		SourceImage: "public.ecr.aws/docker/library/python",
		DefaultTag:  "3.13",
		TagSuffix:   "-slim",
		Instructions: []string{
			"COPY --from=%s /usr/local/lib/python{{.Version}} /usr/local/lib/python{{.Version}}",
			"COPY --from=%s /usr/local/bin/python3* /usr/local/bin/",
			"COPY --from=%s /usr/local/bin/pip* /usr/local/bin/",
			"RUN ln -sf /usr/local/bin/python3 /usr/local/bin/python",
		},
	},
	{
		Name:        "ruby",
		Category:    "runtime",
		Description: "Ruby",
		SourceImage: "public.ecr.aws/docker/library/ruby",
		DefaultTag:  "3.3",
		TagSuffix:   "-slim",
		Instructions: []string{
			"COPY --from=%s /usr/local/lib/ruby /usr/local/lib/ruby",
			"COPY --from=%s /usr/local/bin/ruby /usr/local/bin/ruby",
			"COPY --from=%s /usr/local/bin/gem /usr/local/bin/gem",
			"COPY --from=%s /usr/local/bin/bundle* /usr/local/bin/",
			"COPY --from=%s /usr/local/bin/irb /usr/local/bin/irb",
			"COPY --from=%s /usr/local/include/ruby* /usr/local/include/",
		},
	},
	{
		Name:        "rust",
		Category:    "runtime",
		Description: "Rust toolchain (includes cargo)",
		SourceImage: "public.ecr.aws/docker/library/rust",
		DefaultTag:  "1.94.1",
		TagSuffix:   "-slim",
		Instructions: []string{
			"COPY --from=%s /usr/local/rustup /usr/local/rustup",
			"COPY --from=%s /usr/local/cargo /usr/local/cargo",
		},
		PathEntries: []string{"/usr/local/cargo/bin"},
		EnvVars: map[string]string{
			"RUSTUP_HOME": "/usr/local/rustup",
			"CARGO_HOME":  "/usr/local/cargo",
		},
	},

	// ── package-manager ───────────────────────────────────────────────────
	{
		Name:         "composer",
		Category:     "package-manager",
		Description:  "Composer PHP package manager",
		SourceImage:  "public.ecr.aws/docker/library/composer",
		DefaultTag:   "2.9.5",
		Dependencies: []string{"php"},
		Instructions: []string{
			"COPY --from=%s /usr/bin/composer /usr/local/bin/composer",
		},
	},
	{
		Name:         "gradle",
		Category:     "package-manager",
		Description:  "Gradle build tool",
		SourceImage:  "public.ecr.aws/docker/library/gradle",
		DefaultTag:   "jdk21",
		Dependencies: []string{"java"},
		Instructions: []string{
			"COPY --from=%s /opt/gradle /opt/gradle",
		},
		PathEntries: []string{"/opt/gradle/bin"},
	},
	{
		Name:         "maven",
		Category:     "package-manager",
		Description:  "Apache Maven",
		SourceImage:  "public.ecr.aws/docker/library/maven",
		DefaultTag:   "3-eclipse-temurin-21",
		Dependencies: []string{"java"},
		Instructions: []string{
			"COPY --from=%s /usr/share/maven /usr/share/maven",
		},
		PathEntries: []string{"/usr/share/maven/bin"},
		EnvVars: map[string]string{
			"MAVEN_HOME": "/usr/share/maven",
		},
	},
	{
		Name:         "pnpm",
		Category:     "package-manager",
		Description:  "pnpm package manager",
		DefaultTag:   "10.33.0",
		Dependencies: []string{"nodejs"},
		Instructions: []string{
			"RUN npm install -g pnpm@{{.Tag}}",
		},
	},
	{
		Name:         "skills",
		Category:     "package-manager",
		Description:  "Vercel skill installer",
		DefaultTag:   "1.5.0",
		Dependencies: []string{"nodejs"},
		Instructions: []string{
			"RUN npm install -g skills@{{.Tag}}",
		},
	},
	{
		Name:        "uv",
		Category:    "package-manager",
		Description: "Python package manager (astral-sh/uv)",
		SourceImage: "ghcr.io/astral-sh/uv",
		DefaultTag:  "0.11.6",
		Instructions: []string{
			"COPY --from=%s /uv /uvx /usr/local/bin/",
		},
	},
	{
		Name:         "yarn",
		Category:     "package-manager",
		Description:  "Yarn package manager",
		DefaultTag:   "1.22.22",
		Dependencies: []string{"nodejs"},
		Instructions: []string{
			"RUN npm install -g yarn@{{.Tag}}",
		},
	},

	// ── cloud ─────────────────────────────────────────────────────────────
	{
		Name:         "aws-cdk",
		Category:     "cloud",
		Description:  "AWS CDK",
		Dependencies: []string{"nodejs"},
		Instructions: []string{
			"RUN npm install -g aws-cdk",
		},
	},
	{
		Name:        "aws-cli",
		Category:    "cloud",
		Description: "AWS CLI v2",
		SourceImage: "public.ecr.aws/aws-cli/aws-cli",
		DefaultTag:  "2.34.29",
		Instructions: []string{
			"COPY --from=%s /usr/local/aws-cli /usr/local/aws-cli",
			"RUN ln -sf /usr/local/aws-cli/v2/current/bin/aws /usr/local/bin/aws",
		},
	},
	{
		Name:        "azure-cli",
		Category:    "cloud",
		Description: "Azure CLI",
		Instructions: []string{
			"RUN curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash",
		},
	},
	{
		Name:        "gcloud",
		Category:    "cloud",
		Description: "Google Cloud CLI",
		SourceImage: "gcr.io/google.com/cloudsdktool/google-cloud-cli",
		DefaultTag:  "slim",
		Instructions: []string{
			"COPY --from=%s /usr/lib/google-cloud-sdk /usr/lib/google-cloud-sdk",
			"RUN ln -sf /usr/lib/google-cloud-sdk/bin/gcloud /usr/local/bin/gcloud",
		},
	},
	{
		Name:        "helm",
		Category:    "cloud",
		Description: "Helm package manager for Kubernetes",
		Instructions: []string{
			"RUN curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash",
		},
	},
	{
		Name:        "kubectl",
		Category:    "cloud",
		Description: "Kubernetes CLI",
		DefaultTag:  "1.35.3",
		Instructions: []string{
			`RUN curl -fsSL "https://dl.k8s.io/release/v{{.Tag}}/bin/linux/$(dpkg --print-architecture)/kubectl" -o /usr/local/bin/kubectl \` + "\n" +
				`  && chmod +x /usr/local/bin/kubectl`,
		},
	},
	{
		Name:        "terraform",
		Category:    "cloud",
		Description: "HashiCorp Terraform",
		SourceImage: "public.ecr.aws/hashicorp/terraform",
		DefaultTag:  "1.4.7",
		Instructions: []string{
			"COPY --from=%s /bin/terraform /usr/local/bin/terraform",
		},
	},
	{
		Name:         "tflint",
		Category:     "cloud",
		Description:  "TFLint Terraform linter",
		DefaultTag:   "0.61.0",
		Dependencies: []string{"terraform"},
		Instructions: []string{
			`RUN curl -fsSL "https://github.com/terraform-linters/tflint/releases/download/v{{.Tag}}/tflint_linux_$(dpkg --print-architecture).zip" -o /tmp/tflint.zip \` + "\n" +
				`  && unzip -o /tmp/tflint.zip -d /usr/local/bin \` + "\n" +
				`  && rm /tmp/tflint.zip`,
		},
	},

	// ── database ──────────────────────────────────────────────────────────
	{
		Name:        "mysql-client",
		Category:    "database",
		Description: "MySQL/MariaDB client",
		Instructions: []string{
			"RUN apt update && apt install -y --no-install-recommends default-mysql-client && rm -rf /var/lib/apt/lists/*",
		},
	},
	{
		Name:        "postgresql-client",
		Category:    "database",
		Description: "PostgreSQL client",
		Instructions: []string{
			"RUN apt update && apt install -y --no-install-recommends postgresql-client && rm -rf /var/lib/apt/lists/*",
		},
	},
	{
		Name:        "redis-cli",
		Category:    "database",
		Description: "Redis CLI client",
		Instructions: []string{
			"RUN apt update && apt install -y --no-install-recommends redis-tools && rm -rf /var/lib/apt/lists/*",
		},
	},
	{
		Name:        "sqlite",
		Category:    "database",
		Description: "SQLite database engine",
		Instructions: []string{
			"RUN apt update && apt install -y --no-install-recommends sqlite3 && rm -rf /var/lib/apt/lists/*",
		},
	},

	// ── testing ───────────────────────────────────────────────────────────
	{
		Name:         "hugo",
		Category:     "testing",
		Description:  "Hugo static site generator (extended)",
		DefaultTag:   "0.160.1",
		Dependencies: []string{"golang"},
		Instructions: []string{
			`RUN curl -fsSL "https://github.com/gohugoio/hugo/releases/download/v{{.Tag}}/hugo_extended_{{.Tag}}_linux-$(dpkg --print-architecture).tar.gz" \` + "\n" +
				`  | tar xz -C /usr/local/bin hugo`,
		},
	},
	{
		Name:         "playwright",
		Category:     "testing",
		Description:  "Playwright browser testing CLI",
		DefaultTag:   "1.59.1",
		Dependencies: []string{"nodejs"},
		Instructions: []string{
			"RUN npm install -g @playwright/cli@{{.Tag}}",
			"RUN playwright-cli install-browser chromium",
		},
	},

	// ── utils ─────────────────────────────────────────────────────────────
	{
		Name:        "gh",
		Category:    "utils",
		Description: "GitHub CLI",
		DefaultTag:  "2.89.0",
		Instructions: []string{
			`RUN curl -fsSL "https://github.com/cli/cli/releases/download/v{{.Tag}}/gh_{{.Tag}}_linux_$(dpkg --print-architecture).tar.gz" \` + "\n" +
				`  | tar xz --strip-components=2 -C /usr/local/bin --wildcards '*/bin/gh'`,
		},
	},
	{
		Name:        "make",
		Category:    "utils",
		Description: "GNU Make",
		Instructions: []string{
			"RUN apt update && apt install -y --no-install-recommends make && rm -rf /var/lib/apt/lists/*",
		},
	},
	{
		Name:        "rumdl",
		Category:    "utils",
		Description: "Markdown linter",
		DefaultTag:  "0.1.71",
		Instructions: []string{
			`RUN ARCH=$(dpkg --print-architecture) \` + "\n" +
				`  && if [ "$ARCH" = "amd64" ]; then ARCH=x86_64; elif [ "$ARCH" = "arm64" ]; then ARCH=aarch64; fi \` + "\n" +
				`  && curl -fsSL "https://github.com/rvben/rumdl/releases/download/v{{.Tag}}/rumdl-v{{.Tag}}-${ARCH}-unknown-linux-gnu.tar.gz" \` + "\n" +
				`  | tar xz -C /usr/local/bin rumdl`,
		},
	},
	{
		Name:        "ssh",
		Category:    "utils",
		Description: "OpenSSH client",
		Instructions: []string{
			"RUN apt update && apt install -y --no-install-recommends openssh-client && rm -rf /var/lib/apt/lists/*",
		},
	},
	{
		Name:        "wget",
		Category:    "utils",
		Description: "GNU Wget",
		Instructions: []string{
			"RUN apt update && apt install -y --no-install-recommends wget && rm -rf /var/lib/apt/lists/*",
		},
	},
	{
		Name:        "zizmor",
		Category:    "utils",
		Description: "GitHub Actions workflow security analyzer",
		SourceImage: "ghcr.io/zizmorcore/zizmor",
		DefaultTag:  "1.23.1",
		Instructions: []string{
			"COPY --from=%s /usr/bin/zizmor /usr/local/bin/zizmor",
		},
	},
}

// customToolsFile is the on-disk JSON shape of ~/.ccodolo/custom-tools.json.
type customToolsFile struct {
	Tools  []Tool   `json:"tools"`
	Ignore []string `json:"ignore"`
}

// catalogMu guards the cached effective catalog.
var (
	catalogMu     sync.RWMutex
	cachedCatalog []Tool
	catalogLoaded bool

	// skipCustomToolsForTest, when true, makes effectiveCatalog() return
	// builtinCatalog without consulting the user's custom-tools.json. Tests
	// flip this in TestMain so a developer's local file can never pollute
	// test runs.
	skipCustomToolsForTest bool
)

// resetCustomToolsForTest clears the cached catalog so the next call to
// effectiveCatalog() reloads from disk. Test-only.
func resetCustomToolsForTest() {
	catalogMu.Lock()
	cachedCatalog = nil
	catalogLoaded = false
	catalogMu.Unlock()
}

// effectiveCatalog returns the merged catalog (built-in + custom tools from
// ~/.ccodolo/custom-tools.json). The merge happens lazily on first call and
// is cached for the lifetime of the process. Failure to read or parse the
// file is non-fatal: warnings are written to stderr and the built-in catalog
// is returned unchanged.
func effectiveCatalog() []Tool {
	catalogMu.RLock()
	if catalogLoaded {
		c := cachedCatalog
		catalogMu.RUnlock()
		return c
	}
	catalogMu.RUnlock()

	catalogMu.Lock()
	defer catalogMu.Unlock()
	if catalogLoaded {
		return cachedCatalog
	}

	if skipCustomToolsForTest {
		cachedCatalog = builtinCatalog
		catalogLoaded = true
		return cachedCatalog
	}

	cachedCatalog = loadAndMergeCustomTools()
	catalogLoaded = true
	return cachedCatalog
}

// loadAndMergeCustomTools resolves the custom-tools.json path under the
// user's home directory, loads it, merges it with builtinCatalog, and
// returns the merged result. All warnings and non-fatal errors are written
// to stderr. Returns builtinCatalog unchanged if anything goes wrong.
func loadAndMergeCustomTools() []Tool {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "custom-tools.json: cannot resolve home directory: %v (using built-in tools only)\n", err)
		return builtinCatalog
	}
	path := filepath.Join(home, ".ccodolo", "custom-tools.json")

	file, err := loadCustomToolsFrom(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File missing → silent no-op, the documented happy path.
			return builtinCatalog
		}
		fmt.Fprintf(os.Stderr, "custom-tools.json: %v (using built-in tools only)\n", err)
		return builtinCatalog
	}

	merged, warnings := mergeCatalog(builtinCatalog, file.Tools, file.Ignore)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "custom-tools.json: %s\n", w)
	}
	return merged
}

// loadCustomToolsFrom reads and parses a custom-tools.json file at path.
// Returns the parsed file and an error. If the file is missing the returned
// error satisfies os.IsNotExist; callers should treat that as a no-op.
func loadCustomToolsFrom(path string) (customToolsFile, error) {
	var f customToolsFile
	data, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return f, fmt.Errorf("file %s is empty", path)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("parsing %s: %w", path, err)
	}
	return f, nil
}

// mergeCatalog applies an `ignore` list and a list of custom tool entries on
// top of a built-in catalog and returns the merged result plus any warnings
// generated during the merge.
//
// Order of operations:
//  1. ignore: drop built-ins whose names appear in `ignore`. Names that
//     don't match any built-in are warned and skipped.
//  2. custom: validate and merge each entry. Empty names and empty
//     instructions are rejected with warnings. A custom entry that collides
//     with a name still in the merged catalog (i.e. a built-in that wasn't
//     ignored) replaces it and emits an "overriding built-in" warning.
//     Duplicate custom names within the same load replace earlier definitions
//     with a warning. Custom tools are always assigned category "custom",
//     regardless of any value in the JSON.
func mergeCatalog(builtins []Tool, custom []Tool, ignore []string) ([]Tool, []string) {
	var warnings []string

	// Build a set of built-in names for quick lookup.
	builtinNames := make(map[string]bool, len(builtins))
	for _, t := range builtins {
		builtinNames[t.Name] = true
	}

	// Apply ignore: warn on unknowns, otherwise mark for removal.
	ignoreSet := make(map[string]bool)
	for _, name := range ignore {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !builtinNames[name] {
			warnings = append(warnings, fmt.Sprintf("ignore: %q does not match any built-in tool, skipping", name))
			continue
		}
		ignoreSet[name] = true
	}

	// Start the merged catalog with built-ins minus ignored entries.
	merged := make([]Tool, 0, len(builtins)+len(custom))
	indexByName := make(map[string]int, len(builtins)+len(custom))
	for _, t := range builtins {
		if ignoreSet[t.Name] {
			continue
		}
		indexByName[t.Name] = len(merged)
		merged = append(merged, t)
	}

	// Process custom tools.
	customSeen := make(map[string]bool)
	for _, t := range custom {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			warnings = append(warnings, "custom tool with empty name skipped")
			continue
		}
		if len(t.Instructions) == 0 {
			warnings = append(warnings, fmt.Sprintf("custom tool %q has no instructions, skipping", name))
			continue
		}

		// Force category to "custom" — the user's value (if any) is discarded.
		t.Name = name
		t.Category = "custom"

		if customSeen[name] {
			warnings = append(warnings, fmt.Sprintf("custom tool %q defined more than once, replacing earlier definition", name))
			merged[indexByName[name]] = t
			continue
		}
		customSeen[name] = true

		if idx, exists := indexByName[name]; exists {
			// Name still in merged catalog → overriding a built-in that
			// wasn't ignored. (If the user explicitly ignored it first,
			// idx would not exist here, and we wouldn't warn.)
			warnings = append(warnings, fmt.Sprintf("overriding built-in tool %q", name))
			merged[idx] = t
		} else {
			indexByName[name] = len(merged)
			merged = append(merged, t)
		}
	}

	return merged, warnings
}

// All returns all tools in the catalog (built-in plus any custom tools loaded
// from ~/.ccodolo/custom-tools.json).
func All() []Tool {
	return effectiveCatalog()
}

// Get returns a tool by name, or an error if not found.
func Get(name string) (Tool, error) {
	for _, t := range effectiveCatalog() {
		if t.Name == name {
			return t, nil
		}
	}
	return Tool{}, fmt.Errorf("unknown tool %q", name)
}

// Valid returns true if the tool name is in the catalog.
func Valid(name string) bool {
	_, err := Get(name)
	return err == nil
}

// AllNames returns the names of all tools.
func AllNames() []string {
	cat := effectiveCatalog()
	names := make([]string, len(cat))
	for i, t := range cat {
		names[i] = t.Name
	}
	return names
}

// ByCategory returns tools grouped by category.
func ByCategory() map[string][]Tool {
	groups := make(map[string][]Tool)
	for _, t := range effectiveCatalog() {
		groups[t.Category] = append(groups[t.Category], t)
	}
	return groups
}

// builtinCategoryOrder is the display order for the built-in categories.
// The "custom" category is appended dynamically by CategoryOrder() when at
// least one custom tool is loaded, so this list stays stable for tests that
// pin the built-in order.
var builtinCategoryOrder = []string{"runtime", "package-manager", "cloud", "database", "testing", "utils"}

// CategoryOrder returns the display order for categories. If any custom tools
// are present in the effective catalog, "custom" is appended to the end.
func CategoryOrder() []string {
	order := make([]string, len(builtinCategoryOrder))
	copy(order, builtinCategoryOrder)
	for _, t := range effectiveCatalog() {
		if t.Category == "custom" {
			order = append(order, "custom")
			break
		}
	}
	return order
}

// CategoryLabel returns a human-readable label for a category.
func CategoryLabel(cat string) string {
	switch cat {
	case "runtime":
		return "Language Runtimes"
	case "package-manager":
		return "Package Managers"
	case "cloud":
		return "Cloud / IaC"
	case "database":
		return "Database Clients"
	case "testing":
		return "Testing"
	case "utils":
		return "Utilities"
	case "custom":
		return "Custom"
	default:
		return cat
	}
}

// ResolveDependencyNames takes a list of selected tool names and returns the full
// list including any transitive dependencies, in dependency order.
func ResolveDependencyNames(names []string) []string {
	resolved := make(map[string]bool)
	var order []string

	var resolve func(name string)
	resolve = func(name string) {
		if resolved[name] {
			return
		}
		t, err := Get(name)
		if err != nil {
			return
		}
		for _, dep := range t.Dependencies {
			resolve(dep)
		}
		resolved[name] = true
		order = append(order, name)
	}

	for _, name := range names {
		resolve(name)
	}
	return order
}

// VersionPinnable returns tools from the catalog that have a DefaultTag
// (meaning their version can be meaningfully overridden, either via a
// multi-stage `COPY --from` image tag or via `{{.Tag}}` / `{{.Version}}`
// templated into a RUN command).
func VersionPinnable() []Tool {
	var pinnable []Tool
	for _, t := range effectiveCatalog() {
		if t.DefaultTag != "" {
			pinnable = append(pinnable, t)
		}
	}
	return pinnable
}

// ToolSelection represents a user's tool choice, optionally with a version override.
type ToolSelection struct {
	Name    string
	Version string // empty = use default
}

// Resolve takes a list of tool selections, resolves dependencies, and returns
// the full ordered list of tools with their Docker instructions.
func Resolve(selections []ToolSelection) ([]ResolvedTool, error) {
	// Build a map of requested tools.
	requested := make(map[string]ToolSelection)
	for _, s := range selections {
		requested[s.Name] = s
	}

	// Resolve dependencies.
	resolved := make(map[string]bool)
	var order []string

	var resolve func(name string) error
	resolve = func(name string) error {
		if resolved[name] {
			return nil
		}
		t, err := Get(name)
		if err != nil {
			return err
		}
		for _, dep := range t.Dependencies {
			if err := resolve(dep); err != nil {
				return fmt.Errorf("tool %q depends on %q: %w", name, dep, err)
			}
		}
		resolved[name] = true
		order = append(order, name)
		return nil
	}

	for _, s := range selections {
		if err := resolve(s.Name); err != nil {
			return nil, err
		}
	}

	// Build resolved tools.
	var result []ResolvedTool
	for _, name := range order {
		t, _ := Get(name)
		tag := t.BuildTag(t.DefaultTag)
		if sel, ok := requested[name]; ok && sel.Version != "" {
			tag = t.BuildTag(sel.Version)
		}
		rt := ResolvedTool{
			Tool: t,
			Tag:  tag,
		}
		lines, err := rt.renderInstructions()
		if err != nil {
			return nil, err
		}
		rt.DockerLines = lines
		result = append(result, rt)
	}
	return result, nil
}

// ResolvedTool is a tool with its resolved tag and rendered Docker instructions.
type ResolvedTool struct {
	Tool
	Tag         string
	DockerLines []string
}

// ImageRef returns the full image:tag reference for COPY --from.
func (rt ResolvedTool) ImageRef() string {
	if rt.SourceImage == "" {
		return ""
	}
	return rt.SourceImage + ":" + rt.Tag
}

// Version returns Tag with TagSuffix stripped (e.g. "3.13-slim" → "3.13").
// Use this in Instructions via `{{.Version}}` when you need the version alone.
func (rt ResolvedTool) Version() string {
	if rt.TagSuffix != "" {
		return strings.TrimSuffix(rt.Tag, rt.TagSuffix)
	}
	return rt.Tag
}

// renderInstructions returns Dockerfile lines with template placeholders
// substituted. Each instruction is parsed as a Go text/template with data
// {Tag, Version, ImageRef}, then any `%s` in the result is replaced with the
// image ref for backward compatibility with the original COPY --from= form.
func (rt ResolvedTool) renderInstructions() ([]string, error) {
	ref := rt.ImageRef()
	data := struct {
		Tag      string
		Version  string
		ImageRef string
	}{
		Tag:      rt.Tag,
		Version:  rt.Version(),
		ImageRef: ref,
	}
	lines := make([]string, 0, len(rt.Instructions))
	for _, raw := range rt.Instructions {
		tmpl, err := template.New("instr").Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("tool %q: parsing instruction %q: %w", rt.Name, raw, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("tool %q: executing instruction %q: %w", rt.Name, raw, err)
		}
		expanded := buf.String()
		if ref != "" && strings.Contains(expanded, "%s") {
			expanded = fmt.Sprintf(expanded, ref)
		}
		lines = append(lines, expanded)
	}
	return lines, nil
}
