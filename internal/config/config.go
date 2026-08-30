package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/skwashd/ccodolo/internal/agent"
	"github.com/skwashd/ccodolo/internal/tool"
)

// Config represents the ccodolo.toml configuration.
type Config struct {
	Agent           string            `toml:"agent"`
	Runtime         string            `toml:"runtime,omitempty"`
	Memory          string            `toml:"memory,omitempty"`
	Tools           map[string]string `toml:"tools"`
	Build           BuildConfig       `toml:"build"`
	Volumes         []Volume          `toml:"volumes"`
	Environment     map[string]string `toml:"environment"`
	PassthroughVars []string          `toml:"passthrough_vars"`
}

// envVarNameRegexp matches a valid POSIX environment variable name.
var envVarNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// memoryRegexp matches a memory size in the subset of formats accepted by
// both docker and Apple's container CLI: a number with a k, m, or g suffix
// (optionally followed by b), e.g. "4g", "4gb", "512M".
var memoryRegexp = regexp.MustCompile(`^[0-9]+[kKmMgG][bB]?$`)

// Runtime values accepted by the top-level runtime key. An empty value
// means RuntimeDocker.
const (
	// RuntimeDocker is the default container backend.
	RuntimeDocker = "docker"
	// RuntimeApple is Apple's native container CLI (github.com/apple/container).
	// Experimental; requires macOS 26 on Apple Silicon.
	RuntimeApple = "apple"
)

// AllRuntimeNames returns the accepted runtime names.
func AllRuntimeNames() []string {
	return []string{RuntimeDocker, RuntimeApple}
}

// ValidRuntime returns true if the runtime name is recognized.
func ValidRuntime(name string) bool {
	for _, r := range AllRuntimeNames() {
		if r == name {
			return true
		}
	}
	return false
}

// ToolEntry represents a tool selection used internally.
type ToolEntry struct {
	Name    string
	Version string
}

// ToolEntries returns the Tools map as a slice of ToolEntry for processing.
func (cfg *Config) ToolEntries() []ToolEntry {
	var entries []ToolEntry
	for name, version := range cfg.Tools {
		entries = append(entries, ToolEntry{Name: name, Version: version})
	}
	return entries
}

// SetTools updates the Tools map from a slice of ToolEntry.
func (cfg *Config) SetTools(entries []ToolEntry) {
	cfg.Tools = make(map[string]string, len(entries))
	for _, e := range entries {
		cfg.Tools[e.Name] = e.Version
	}
}

// StepOrigin records which config file declared a build step, so COPY/ADD
// sources in that step resolve against the matching common/ directory.
type StepOrigin string

const (
	// OriginGlobal marks a step declared in the global ~/.ccodolo/ccodolo.toml.
	OriginGlobal StepOrigin = "global"
	// OriginProject marks a step declared in a project's ccodolo.toml.
	OriginProject StepOrigin = "project"
)

// BuildConfig holds custom Dockerfile build steps.
type BuildConfig struct {
	CustomSteps []string `toml:"custom_steps"`
	RootSteps   []string `toml:"root_steps"`

	// CustomStepOrigins and RootStepOrigins run parallel to the step slices
	// above, recording which config file declared each step so COPY/ADD
	// sources resolve against the matching common/ directory. Populated by
	// Merge; never serialised (configs built by LoadProjectOnly or directly
	// in tests never go through Merge, so callers must use the
	// CustomStepOrigin/RootStepOrigin accessors rather than indexing these
	// slices directly).
	CustomStepOrigins []StepOrigin `toml:"-"`
	RootStepOrigins   []StepOrigin `toml:"-"`
}

// CustomStepOrigin returns the origin of CustomSteps[i], defaulting to
// OriginProject when the origins slice is shorter than i — configs built by
// LoadProjectOnly or directly in tests never go through Merge and won't have
// origins populated.
func (cfg *Config) CustomStepOrigin(i int) StepOrigin {
	if i < 0 || i >= len(cfg.Build.CustomStepOrigins) {
		return OriginProject
	}
	return cfg.Build.CustomStepOrigins[i]
}

// RootStepOrigin returns the origin of RootSteps[i], defaulting to
// OriginProject when the origins slice is shorter than i — see
// CustomStepOrigin.
func (cfg *Config) RootStepOrigin(i int) StepOrigin {
	if i < 0 || i >= len(cfg.Build.RootStepOrigins) {
		return OriginProject
	}
	return cfg.Build.RootStepOrigins[i]
}

// Volume represents an additional volume mount.
type Volume struct {
	Host      string `toml:"host"`
	Container string `toml:"container"`
	ReadOnly  bool   `toml:"readonly,omitempty"`
}

// GlobalConfigDir returns the global config directory path.
func GlobalConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".ccodolo"), nil
}

// GlobalConfigPath returns the path to the global ccodolo.toml.
func GlobalConfigPath() (string, error) {
	dir, err := GlobalConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ccodolo.toml"), nil
}

// ProjectConfigPath returns the path to a project's ccodolo.toml.
func ProjectConfigPath(projectPath string) string {
	return filepath.Join(projectPath, "ccodolo.toml")
}

// ProjectPath returns the on-disk directory for a project, given its name.
func ProjectPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".ccodolo", "projects", name), nil
}

// CommonDir returns the common/ directory beside a project's ccodolo.toml.
// COPY/ADD sources in project-origin build steps resolve against this dir.
func CommonDir(projectPath string) string {
	return filepath.Join(projectPath, "common")
}

// GlobalCommonDir returns the common/ directory beside the global
// ccodolo.toml. COPY/ADD sources in global-origin build steps resolve
// against this dir.
func GlobalCommonDir() (string, error) {
	dir, err := GlobalConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "common"), nil
}

// Load loads and merges the global and project configs.
// It also handles migration from the old ccodolo.config format.
func Load(projectPath string) (*Config, error) {
	globalPath, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	global, _ := loadFile(globalPath)
	if global == nil {
		global = &Config{}
	}

	// Try migration for the project config.
	projectToml := ProjectConfigPath(projectPath)
	if _, err := os.Stat(projectToml); os.IsNotExist(err) {
		if err := tryMigrate(projectPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: config migration failed: %v\n", err)
		}
	}

	project, _ := loadFile(projectToml)
	if project == nil {
		project = &Config{}
	}

	// Check for stale old config.
	oldConfig := filepath.Join(projectPath, "ccodolo.config")
	if _, err := os.Stat(projectToml); err == nil {
		if _, err := os.Stat(oldConfig); err == nil {
			fmt.Fprintf(os.Stderr, "Warning: both ccodolo.toml and ccodolo.config exist; using ccodolo.toml (consider removing ccodolo.config)\n")
		}
	}

	merged := Merge(global, project)
	return merged, nil
}

// LoadProjectOnly loads only the project-level config, without merging global.
// Returns an empty Config if the project config file doesn't exist.
func LoadProjectOnly(projectPath string) (*Config, error) {
	cfg, err := loadFile(ProjectConfigPath(projectPath))
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	return cfg, nil
}

// loadFile loads a single TOML config file.
func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

// Save writes a config to the given path.
func Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return f.Close()
}

// Merge combines global and project configs according to the merge semantics:
// - agent: project overrides global
// - runtime: project overrides global
// - memory: project overrides global
// - tools: union (deduplicated by name; project version overrides)
// - build.custom_steps: concatenated (global first, then project)
// - build.root_steps: concatenated (global first, then project)
// - volumes: union (project overrides if same container path)
// - environment: merged (project keys override global)
// - passthrough_vars: union (deduplicated, preserves order; global entries first)
func Merge(global, project *Config) *Config {
	result := &Config{}

	// Agent: project overrides global.
	result.Agent = global.Agent
	if project.Agent != "" {
		result.Agent = project.Agent
	}

	// Runtime: project overrides global.
	result.Runtime = global.Runtime
	if project.Runtime != "" {
		result.Runtime = project.Runtime
	}

	// Memory: project overrides global.
	result.Memory = global.Memory
	if project.Memory != "" {
		result.Memory = project.Memory
	}

	// Tools: union; project version overrides global.
	result.Tools = make(map[string]string)
	for name, version := range global.Tools {
		result.Tools[name] = version
	}
	for name, version := range project.Tools {
		result.Tools[name] = version
	}

	// Build.CustomSteps: concatenated. Origins run in lockstep so COPY/ADD
	// sources later resolve against the matching common/ directory.
	result.Build.CustomSteps = append(result.Build.CustomSteps, global.Build.CustomSteps...)
	result.Build.CustomSteps = append(result.Build.CustomSteps, project.Build.CustomSteps...)
	for range global.Build.CustomSteps {
		result.Build.CustomStepOrigins = append(result.Build.CustomStepOrigins, OriginGlobal)
	}
	for range project.Build.CustomSteps {
		result.Build.CustomStepOrigins = append(result.Build.CustomStepOrigins, OriginProject)
	}

	// Build.RootSteps: concatenated, with origins in lockstep (see above).
	result.Build.RootSteps = append(result.Build.RootSteps, global.Build.RootSteps...)
	result.Build.RootSteps = append(result.Build.RootSteps, project.Build.RootSteps...)
	for range global.Build.RootSteps {
		result.Build.RootStepOrigins = append(result.Build.RootStepOrigins, OriginGlobal)
	}
	for range project.Build.RootSteps {
		result.Build.RootStepOrigins = append(result.Build.RootStepOrigins, OriginProject)
	}

	// Volumes: union, project overrides by container path.
	volMap := make(map[string]Volume)
	var volOrder []string
	for _, v := range global.Volumes {
		volMap[v.Container] = v
		volOrder = append(volOrder, v.Container)
	}
	for _, v := range project.Volumes {
		if _, exists := volMap[v.Container]; !exists {
			volOrder = append(volOrder, v.Container)
		}
		volMap[v.Container] = v
	}
	for _, cp := range volOrder {
		result.Volumes = append(result.Volumes, volMap[cp])
	}

	// Environment: merged, project overrides.
	result.Environment = make(map[string]string)
	for k, v := range global.Environment {
		result.Environment[k] = v
	}
	for k, v := range project.Environment {
		result.Environment[k] = v
	}
	if len(result.Environment) == 0 {
		result.Environment = nil
	}

	// PassthroughVars: union, preserving order, global entries first.
	seen := make(map[string]bool)
	for _, name := range global.PassthroughVars {
		if !seen[name] {
			seen[name] = true
			result.PassthroughVars = append(result.PassthroughVars, name)
		}
	}
	for _, name := range project.PassthroughVars {
		if !seen[name] {
			seen[name] = true
			result.PassthroughVars = append(result.PassthroughVars, name)
		}
	}

	return result
}

// Validate checks the config for errors.
func Validate(cfg *Config) error {
	if cfg.Agent != "" && !agent.Valid(cfg.Agent) {
		return fmt.Errorf("invalid agent %q, must be one of: %v", cfg.Agent, agent.AllNames())
	}

	if cfg.Runtime != "" && !ValidRuntime(cfg.Runtime) {
		return fmt.Errorf("invalid runtime %q, must be one of: %v", cfg.Runtime, AllRuntimeNames())
	}

	if cfg.Memory != "" && !memoryRegexp.MatchString(cfg.Memory) {
		return fmt.Errorf(`invalid memory %q: must be a number with a k, m, or g suffix, e.g. "4g"`, cfg.Memory)
	}

	for name := range cfg.Tools {
		if !tool.Valid(name) {
			return fmt.Errorf("unknown tool %q, must be one of: %v", name, tool.AllNames())
		}
	}

	for _, step := range cfg.Build.CustomSteps {
		if err := validateCustomStep(step); err != nil {
			return err
		}
	}

	for _, step := range cfg.Build.RootSteps {
		if err := validateCustomStep(step); err != nil {
			return err
		}
	}

	for _, v := range cfg.Volumes {
		if !filepath.IsAbs(v.Container) {
			return fmt.Errorf("volume container path must be absolute: %q", v.Container)
		}
	}

	seenPassthrough := make(map[string]bool)
	for _, name := range cfg.PassthroughVars {
		if !envVarNameRegexp.MatchString(name) {
			return fmt.Errorf("invalid passthrough_vars entry %q: must match [A-Za-z_][A-Za-z0-9_]*", name)
		}
		if seenPassthrough[name] {
			return fmt.Errorf("duplicate passthrough_vars entry %q", name)
		}
		seenPassthrough[name] = true
	}

	return nil
}

// validateCustomStep checks that a custom step starts with an allowed instruction.
func validateCustomStep(step string) error {
	trimmed := strings.TrimSpace(step)
	upper := strings.ToUpper(trimmed)
	for _, prefix := range []string{"RUN ", "COPY ", "ADD "} {
		if strings.HasPrefix(upper, prefix) {
			return nil
		}
	}
	return fmt.Errorf(
		"custom step must start with RUN, COPY, or ADD (got %q); "+
			"other instructions (ENV, WORKDIR, USER, etc.) are lost during the single-layer squash",
		trimmed,
	)
}

// ToolSelections converts config tools to tool.ToolSelection for resolution.
func (cfg *Config) ToolSelections() []tool.ToolSelection {
	var sels []tool.ToolSelection
	for name, version := range cfg.Tools {
		sels = append(sels, tool.ToolSelection{
			Name:    name,
			Version: version,
		})
	}
	return sels
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
