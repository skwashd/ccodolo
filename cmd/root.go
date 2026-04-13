package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/skwashd/ccodolo/internal/agent"
	"github.com/skwashd/ccodolo/internal/config"
	"github.com/skwashd/ccodolo/internal/docker"
	"github.com/skwashd/ccodolo/internal/project"
	"github.com/skwashd/ccodolo/internal/tool"
)

// errAborted is returned when the user declines to apply changes.
var errAborted = errors.New("aborted")

var (
	flagProject     string
	flagWorkdir     string
	flagAgent       string
	flagTools       string
	flagCreateNew   bool
	flagExec        bool
	flagRebuild     bool
	flagBuildOnly   bool
	flagReconfigure bool
)

var rootCmd = &cobra.Command{
	Use:   "ccodolo",
	Short: "Multi-agent coding environment in Docker",
	Long:  "CCoDoLo launches sandboxed Docker containers for AI coding assistants with isolated project environments.",
	// Accept arbitrary args after --.
	Args:              cobra.ArbitraryArgs,
	DisableFlagParsing: false,
	SilenceUsage:      true,
	RunE:              runRoot,
}

func init() {
	rootCmd.Flags().StringVar(&flagProject, "project", "", "Project name (required)")
	rootCmd.Flags().StringVar(&flagWorkdir, "workdir", "", "Working directory (default: current directory)")
	rootCmd.Flags().StringVar(&flagAgent, "agent", "", "Agent to use: claude, codex, copilot, gemini, kiro, opencode")
	rootCmd.Flags().StringVar(&flagTools, "tools", "", "Comma-separated list of dev tools to install")
	rootCmd.Flags().BoolVar(&flagCreateNew, "create-new", false, "Create new project without confirmation prompt")
	rootCmd.Flags().BoolVar(&flagExec, "exec", false, "Attach to existing container instead of creating new one")
	rootCmd.Flags().BoolVar(&flagRebuild, "rebuild", false, "Force image rebuild")
	rootCmd.Flags().BoolVar(&flagBuildOnly, "build-only", false, "Build the image but don't launch a container")
	rootCmd.Flags().BoolVar(&flagReconfigure, "reconfigure", false, "Update agent and tools for an existing project")

	_ = rootCmd.MarkFlagRequired("project")

	rootCmd.AddCommand(versionCmd)
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func runRoot(cmd *cobra.Command, args []string) error {
	// Validate flag combinations.
	if flagReconfigure {
		if flagCreateNew {
			return fmt.Errorf("--reconfigure and --create-new are mutually exclusive")
		}
		if flagExec {
			return fmt.Errorf("--reconfigure and --exec are mutually exclusive")
		}
		if flagBuildOnly {
			return fmt.Errorf("--reconfigure and --build-only are mutually exclusive")
		}
	}

	// Resolve project path.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	projectPath := filepath.Join(home, ".ccodolo", "projects", flagProject)

	// Resolve workdir.
	workdir := flagWorkdir
	if workdir == "" {
		workdir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
	}

	// Check if project exists (for new-project detection).
	projectExists := dirExists(projectPath)

	// Load config: global merged with project (with migration).
	cfg, err := config.Load(projectPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// CLI --agent overrides config.
	if flagAgent != "" {
		if !agent.Valid(flagAgent) {
			return fmt.Errorf("invalid agent %q, must be one of: %v", flagAgent, agent.AllNames())
		}
		cfg.Agent = flagAgent
	}

	// Default agent to claude.
	if cfg.Agent == "" {
		cfg.Agent = "claude"
	}

	// Handle --reconfigure mode.
	if flagReconfigure {
		if !projectExists {
			return fmt.Errorf("project %q does not exist; use --create-new to create it", flagProject)
		}
		if err := runReconfigure(projectPath, cfg); err != nil {
			if errors.Is(err, errAborted) {
				return nil
			}
			return err
		}
		// Reload merged config so the build/run uses the updated settings.
		cfg, err = config.Load(projectPath)
		if err != nil {
			return fmt.Errorf("reloading config: %w", err)
		}
		if cfg.Agent == "" {
			cfg.Agent = "claude"
		}
	}

	// Create project directory if needed.
	if !projectExists {
		if !flagCreateNew {
			fmt.Fprintf(os.Stderr, "Project directory does not exist. Create new project '%s'? (y/n) ", flagProject)
			var answer string
			_, _ = fmt.Scanln(&answer)
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				fmt.Fprintln(os.Stderr, "Aborted.")
				return nil
			}
		}

		fmt.Fprintf(os.Stderr, "Creating project directory at %s\n", projectPath)
		if err := os.MkdirAll(projectPath, 0o755); err != nil {
			return fmt.Errorf("creating project directory: %w", err)
		}

		// Copy template.
		if err := project.CopyTemplate(projectPath); err != nil {
			return fmt.Errorf("copying template: %w", err)
		}

		// Tool selection for new projects.
		if flagTools != "" {
			tools, err := parseToolFlag(flagTools)
			if err != nil {
				return err
			}
			cfg.SetTools(tools)
		} else if isInteractive() {
			// Launch interactive TUI for tool selection.
			selected, err := runToolTUI(cfg)
			if err != nil {
				return fmt.Errorf("tool selection: %w", err)
			}
			cfg.SetTools(selected)
		}

		// Save config for new project.
		if err := config.Save(cfg, config.ProjectConfigPath(projectPath)); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	// Get agent metadata for directory setup.
	a, err := agent.Parse(cfg.Agent)
	if err != nil {
		return err
	}
	meta, err := agent.Get(a)
	if err != nil {
		return err
	}

	// Ensure project directories exist.
	if err := project.EnsureDirs(projectPath, meta.ConfigDir); err != nil {
		return fmt.Errorf("setting up project directories: %w", err)
	}

	// Agent-specific setup.
	switch a {
	case agent.Copilot:
		if err := project.SetupCopilot(projectPath, flagProject); err != nil {
			return fmt.Errorf("configuring copilot: %w", err)
		}
	case agent.Gemini:
		if err := project.SetupGemini(projectPath, flagProject); err != nil {
			return fmt.Errorf("configuring gemini: %w", err)
		}
	case agent.Kiro:
		if err := project.SetupKiro(projectPath); err != nil {
			return fmt.Errorf("configuring kiro: %w", err)
		}
	}

	// Validate workdir exists.
	if !dirExists(workdir) {
		return fmt.Errorf("--workdir doesn't exist: %s", workdir)
	}

	// Validate config.
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Handle --exec mode.
	if flagExec {
		return docker.Exec(flagProject, workdir)
	}

	// Build image.
	imageTag, err := docker.Build(cfg, flagProject, flagRebuild)
	if err != nil {
		return fmt.Errorf("building image: %w", err)
	}

	// Handle --build-only mode.
	if flagBuildOnly {
		fmt.Println(imageTag)
		return nil
	}

	// Run container.
	return docker.Run(cfg, flagProject, workdir, imageTag, args)
}

// runToolTUI launches the interactive tool selection TUI.
func runToolTUI(cfg *config.Config) ([]config.ToolEntry, error) {
	// Pre-select tools from config.
	preSelected := make(map[string]bool)
	for name := range cfg.Tools {
		preSelected[name] = true
	}

	// Collect existing version overrides.
	existingVersions := make(map[string]string)
	for name, version := range cfg.Tools {
		if version != "" {
			existingVersions[name] = version
		}
	}

	fmt.Fprintf(os.Stderr, "Creating project '%s' with agent '%s'\n\n", flagProject, cfg.Agent)

	selectedTools, err := runToolSelectTUI(preSelected)
	if err != nil {
		return nil, err
	}

	return resolveAndPinTools(selectedTools, existingVersions)
}

// isInteractive returns true if stdin is a terminal.
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// parseToolFlag parses a comma-separated tool list with optional version pinning.
// Format: "python:3.12-slim,uv,nodejs:22-slim"
func parseToolFlag(flagValue string) ([]config.ToolEntry, error) {
	var entries []config.ToolEntry
	for _, item := range strings.Split(flagValue, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, ":", 2)
		name := parts[0]
		if !tool.Valid(name) {
			return nil, fmt.Errorf("unknown tool %q, must be one of: %v", name, tool.AllNames())
		}
		entry := config.ToolEntry{Name: name}
		if len(parts) == 2 {
			entry.Version = parts[1]
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// reconfigResult holds the result of the interactive agent+tool selection TUI.
type reconfigResult struct {
	agent string
	tools []config.ToolEntry
}

// runReconfigure handles the --reconfigure flow.
func runReconfigure(projectPath string, mergedCfg *config.Config) error {
	// Load only the project-level config so we don't write global values back.
	projectCfg, err := config.LoadProjectOnly(projectPath)
	if err != nil {
		return fmt.Errorf("loading project config: %w", err)
	}

	// Keep a copy of the original for diff display.
	originalAgent := projectCfg.Agent
	originalTools := projectCfg.ToolEntries()

	if flagAgent != "" {
		projectCfg.Agent = flagAgent
	}

	if flagTools != "" {
		// Non-interactive: parse --tools flag with version support.
		tools, err := parseToolFlag(flagTools)
		if err != nil {
			return err
		}
		projectCfg.SetTools(tools)
	} else if isInteractive() {
		// Interactive: launch TUI with current values pre-selected.
		result, err := runAgentAndToolTUI(mergedCfg)
		if err != nil {
			return fmt.Errorf("reconfigure: %w", err)
		}
		projectCfg.Agent = result.agent
		projectCfg.SetTools(result.tools)
	} else if flagAgent == "" {
		return fmt.Errorf("--reconfigure requires --agent and/or --tools flags in non-interactive mode, or run interactively")
	}

	// Validate before saving.
	if err := config.Validate(projectCfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Show diff and confirm.
	hasChanges := showConfigDiff(originalAgent, originalTools, projectCfg)
	if !hasChanges {
		fmt.Fprintln(os.Stderr, "No changes detected, launching with current config.")
		return nil
	}

	// In interactive mode, confirm before saving.
	if isInteractive() {
		for {
			fmt.Fprintf(os.Stderr, "\nApply these changes? (y/n) ")
			var answer string
			_, _ = fmt.Scanln(&answer)
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer == "" {
				continue
			}
			if answer == "y" || answer == "yes" {
				break
			}
			fmt.Fprintln(os.Stderr, "Aborted.")
			return errAborted
		}
	}

	// Save updated project config.
	if err := config.Save(projectCfg, config.ProjectConfigPath(projectPath)); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %q reconfigured successfully.\n", flagProject)
	return nil
}

// runAgentAndToolTUI launches the interactive agent and tool selection TUI.
func runAgentAndToolTUI(cfg *config.Config) (reconfigResult, error) {
	selectedAgent := cfg.Agent
	if selectedAgent == "" {
		selectedAgent = "claude"
	}

	agentOptions := make([]huh.Option[string], 0, len(agent.AllNames()))
	for _, name := range agent.AllNames() {
		agentOptions = append(agentOptions, huh.NewOption(name, name))
	}

	// Pre-select tools from current config.
	preSelected := make(map[string]bool)
	for name := range cfg.Tools {
		preSelected[name] = true
	}

	// Collect existing version overrides.
	existingVersions := make(map[string]string)
	for name, version := range cfg.Tools {
		if version != "" {
			existingVersions[name] = version
		}
	}

	fmt.Fprintf(os.Stderr, "Reconfiguring project '%s'\n\n", flagProject)

	// Step 1: Agent selection.
	agentForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select agent").
				Options(agentOptions...).
				Value(&selectedAgent),
		),
	)
	if err := agentForm.Run(); err != nil {
		return reconfigResult{}, err
	}

	// Step 2: Tool selection + dependency resolution + version pinning.
	selectedTools, err := runToolSelectTUI(preSelected)
	if err != nil {
		return reconfigResult{}, err
	}

	entries, err := resolveAndPinTools(selectedTools, existingVersions)
	if err != nil {
		return reconfigResult{}, err
	}

	return reconfigResult{
		agent: selectedAgent,
		tools: entries,
	}, nil
}

// toolLabel builds the display label for a tool in the TUI, appending
// dependency info derived from the tool's Dependencies field.
func toolLabel(t tool.Tool) string {
	desc := t.Description
	if len(t.Dependencies) > 0 {
		desc += " (installs " + strings.Join(t.Dependencies, ", ") + ")"
	}
	return fmt.Sprintf("%-14s %s", t.Name, desc)
}

// runToolSelectTUI shows the multi-select tool picker and returns the selected tool names.
func runToolSelectTUI(preSelected map[string]bool) ([]string, error) {
	var selectedTools []string

	// Sort tools alphabetically by name.
	allTools := make([]tool.Tool, len(tool.All()))
	copy(allTools, tool.All())
	sort.Slice(allTools, func(i, j int) bool {
		return allTools[i].Name < allTools[j].Name
	})

	var options []huh.Option[string]
	for _, t := range allTools {
		options = append(options, huh.NewOption(toolLabel(t), t.Name).Selected(preSelected[t.Name]))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select dev tools to install").
				Height(len(options)+2).
				Options(options...).
				Value(&selectedTools),
		),
	)

	if err := form.Run(); err != nil {
		return nil, err
	}
	return selectedTools, nil
}

// resolveAndPinTools takes selected tool names, resolves dependencies, and
// optionally prompts for version pinning on tools that support it.
func resolveAndPinTools(selectedTools []string, existingVersions map[string]string) ([]config.ToolEntry, error) {
	// Resolve dependencies — this may add tools the user didn't explicitly pick.
	resolved := tool.ResolveDependencyNames(selectedTools)

	// Inform user about auto-added dependencies.
	selectedSet := make(map[string]bool, len(selectedTools))
	for _, name := range selectedTools {
		selectedSet[name] = true
	}
	var autoAdded []string
	for _, name := range resolved {
		if !selectedSet[name] {
			autoAdded = append(autoAdded, name)
		}
	}
	if len(autoAdded) > 0 {
		fmt.Fprintf(os.Stderr, "Auto-installing dependencies: %s\n\n", strings.Join(autoAdded, ", "))
	}

	// Identify tools that support version pinning (those with a DefaultTag).
	type pinnable struct {
		name       string
		defaultTag string
		version    string // pointer to form value
	}
	var pinnableTools []pinnable
	for _, name := range resolved {
		t, err := tool.Get(name)
		if err != nil || t.DefaultTag == "" {
			continue
		}
		initial := t.DefaultVersion()
		if v, ok := existingVersions[name]; ok {
			initial = v
		}
		pinnableTools = append(pinnableTools, pinnable{name: name, defaultTag: t.DefaultVersion(), version: initial})
	}

	// Show version pinning form if there are pinnable tools.
	if len(pinnableTools) > 0 && isInteractive() {
		var fields []huh.Field
		for i := range pinnableTools {
			p := &pinnableTools[i]
			fields = append(fields, huh.NewInput().
				Title(p.name).
				Description(fmt.Sprintf("default: %s", p.defaultTag)).
				Value(&p.version))
		}

		versionForm := huh.NewForm(
			huh.NewGroup(fields...).
				Title("Pin tool versions (leave as-is for defaults)"),
		)

		if err := versionForm.Run(); err != nil {
			return nil, err
		}
	}

	// Build the final tool entries.
	// Create a map of pinned versions.
	pinnedVersions := make(map[string]string)
	for _, p := range pinnableTools {
		// Only set version if it differs from the default.
		if p.version != p.defaultTag {
			pinnedVersions[p.name] = p.version
		}
	}

	var entries []config.ToolEntry
	for _, name := range resolved {
		entry := config.ToolEntry{Name: name}
		if v, ok := pinnedVersions[name]; ok {
			entry.Version = v
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// showConfigDiff displays the differences between old and new configuration.
// Returns true if there are any changes.
func showConfigDiff(oldAgent string, oldTools []config.ToolEntry, newCfg *config.Config) bool {
	hasChanges := false

	fmt.Fprintf(os.Stderr, "\nChanges to project '%s':\n", flagProject)

	// Agent diff.
	if oldAgent != newCfg.Agent {
		old := oldAgent
		if old == "" {
			old = "(default)"
		}
		fmt.Fprintf(os.Stderr, "  Agent: %s -> %s\n", old, newCfg.Agent)
		hasChanges = true
	}

	// Tool diff.
	oldToolMap := make(map[string]string) // name -> version
	for _, t := range oldTools {
		oldToolMap[t.Name] = t.Version
	}

	var added, removed, versionChanged, unchanged []string

	for name, version := range newCfg.Tools {
		if oldVer, ok := oldToolMap[name]; ok {
			if oldVer != version {
				if oldVer == "" {
					oldVer = "(default)"
				}
				ver := version
				if ver == "" {
					ver = "(default)"
				}
				versionChanged = append(versionChanged, fmt.Sprintf("%s: %s -> %s", name, oldVer, ver))
			} else {
				unchanged = append(unchanged, name)
			}
		} else {
			added = append(added, name)
		}
	}
	for _, t := range oldTools {
		if _, ok := newCfg.Tools[t.Name]; !ok {
			removed = append(removed, t.Name)
		}
	}

	if len(added) > 0 {
		fmt.Fprintf(os.Stderr, "  Tools added: %s\n", strings.Join(added, ", "))
		hasChanges = true
	}
	if len(removed) > 0 {
		fmt.Fprintf(os.Stderr, "  Tools removed: %s\n", strings.Join(removed, ", "))
		hasChanges = true
	}
	if len(versionChanged) > 0 {
		fmt.Fprintf(os.Stderr, "  Tools version changed: %s\n", strings.Join(versionChanged, ", "))
		hasChanges = true
	}
	if len(unchanged) > 0 {
		fmt.Fprintf(os.Stderr, "  Tools unchanged: %s\n", strings.Join(unchanged, ", "))
	}

	return hasChanges
}

// dirExists returns true if the path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
