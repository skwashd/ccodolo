# AGENTS.md - Guide for AI Coding Assistants

This document provides development guidelines for AI-powered coding tools working on the CCoDoLo project itself.

**For project features, usage, and architecture**: See [README.md](README.md)

## What This Project Is

CCoDoLo is a meta-project - it provides sandboxed Docker containers for running AI coding assistants (including you). When modifying this codebase, you're improving the infrastructure that runs AI agents safely.

## Repository Structure

```
ccodolo/
├── .github/
│   └── workflows/
│       ├── release.yml              # goreleaser (triggered by v* tags)
│       └── validate.yml             # go vet, go test, golangci-lint
├── .goreleaser.yml                  # Cross-platform build + Homebrew tap
├── go.mod / go.sum
├── main.go                          # Entrypoint → cmd.Execute()
├── cmd/
│   ├── root.go                      # cobra root command, flags, main run() logic, TUI
│   └── version.go                   # version subcommand
├── internal/
│   ├── agent/
│   │   ├── agent.go                 # Agent enum, metadata registry
│   │   └── agent_test.go
│   ├── config/
│   │   ├── config.go                # TOML struct, Load/Save, merge, validate
│   │   ├── config_test.go
│   │   ├── migrate.go               # ccodolo.config → ccodolo.toml migration
│   │   └── migrate_test.go
│   ├── docker/
│   │   ├── build.go                 # Image build orchestration
│   │   ├── dockerfile.go            # Dockerfile template rendering
│   │   ├── dockerfile_test.go
│   │   ├── hash.go                  # SHA-256 image tag computation
│   │   ├── hash_test.go
│   │   └── run.go                   # docker run / docker exec
│   ├── project/
│   │   ├── project.go               # Dir creation, template copying
│   │   ├── project_test.go
│   │   ├── setup.go                 # Agent-specific JSON config (copilot/gemini/kiro)
│   │   └── setup_test.go
│   └── tool/
│       ├── tool.go                  # Tool catalog, dependency resolution
│       └── tool_test.go
├── embedded/
│   ├── embed.go                     # go:embed directives
│   ├── Dockerfile.tmpl              # Templatized Dockerfile
│   └── dotfiles/                    # Shell configurations (.bashrc, .zshrc, .inputrc)
└── template.example/                # Example project templates
```

Projects are stored in `~/.ccodolo/projects/<name>/` (gitignored).

## Development Guidelines

### Go Code Validation

**Always validate before committing:**

```bash
go vet ./...
go test ./...
golangci-lint run ./...
```

### Architecture Notes

- The `ccodolo` binary is a cross-platform Go CLI using cobra for flags and charmbracelet/huh for the interactive TUI
- Config uses TOML (`ccodolo.toml`) with global (`~/.ccodolo/ccodolo.toml`) + project merge semantics
- Old `ccodolo.config` shell files are auto-migrated to TOML on first load
- Each image contains exactly one agent — no start-agent dispatch script
- Dockerfile is generated dynamically from `embedded/Dockerfile.tmpl` using `text/template`
- Dev tools use multi-stage `COPY --from=<image>` — no apt installs for runtimes/tools
- Final image uses `FROM scratch` + `COPY --from=base` for single-layer squash
- Image tags are content-addressed: `ccodolo:<project>-<8-char-sha256>`

### CLI Flags

| Flag | Description |
|------|-------------|
| `--project` | Project name (required) |
| `--workdir` | Working directory (default: cwd) |
| `--agent` | Agent override (default: claude or from config) |
| `--tools` | Comma-separated tool list (supports version pinning: `python:3.12-slim`) |
| `--create-new` | Skip confirmation prompt |
| `--reconfigure` | Update agent and tools for existing project |
| `--exec` | Attach to existing container |
| `--rebuild` | Force image rebuild |
| `--build-only` | Build image, print tag, exit |

### Docker Best Practices

- Tool source images are pinned in `internal/tool/tool.go`
- Custom build steps only allow RUN, COPY, ADD (other instructions are lost in squash)
- npm-based agents (codex, copilot, gemini, opencode) auto-include nodejs as a dependency

## Testing Changes

Before committing:

1. **Go validation**: `go vet ./...`, `go test ./...`, `golangci-lint run ./...`
2. **Build test**: `go build -o /dev/null .`
3. **Runtime test**: Create test project and verify agent launches
4. **Multi-agent test**: Test with 2+ agents if touching agent selection logic

```bash
# Quick test flow
go build -o ccodolo .
./ccodolo --project test-changes --agent claude --create-new --tools python,uv
# Verify: project dir, ccodolo.toml, container launches with python+uv
exit
./ccodolo --project test-changes --agent copilot  # Test switching

# Test reconfigure with flags
./ccodolo --project test-changes --reconfigure --agent gemini --tools python:3.12-slim,nodejs
# Verify: ccodolo.toml updated, diff shown before applying

# Test reconfigure interactive (manual)
./ccodolo --project test-changes --reconfigure
# Verify: TUI shows current selections pre-selected, diff + confirmation before saving
```

## Common Modifications

**Adding a new agent**: Update `internal/agent/agent.go` (add to registry with install cmd, entrypoint, config dir, dependencies). Add agent-specific setup in `internal/project/setup.go` and wire it in `cmd/root.go`. Document in README.md.

**Adding a new tool**: Update `internal/tool/tool.go` (add to catalog with source image, default tag, COPY instructions, dependencies). Document in README.md.

**Shell configuration**: Edit `embedded/dotfiles/`, ensure both zsh and bash work, test history persistence, rebuild and verify.

**Changing the Dockerfile template**: Edit `embedded/Dockerfile.tmpl`. Note that ENV, USER, WORKDIR, and ENTRYPOINT must be re-declared after the `FROM scratch` squash since they are metadata-only instructions.

## Key Constraints

- Environment variables are NOT passed from host to container
- Both zsh and bash must have feature parity
- Project isolation is critical - test that configs don't leak between projects
- Custom steps must be RUN/COPY/ADD only (squash constraint)
- All code must pass `golangci-lint` (errcheck, etc.)

## CI/CD

- **validate.yml**: Runs on PRs and non-main pushes — `go vet`, `go test -race`, `golangci-lint`
- **release.yml**: Triggered by `v*` tags — runs goreleaser to build binaries and publish to Homebrew tap
- **dependabot.yml**: Weekly updates for github-actions and gomod ecosystems

## Testing Checklist for PRs

- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] `golangci-lint run ./...` passes
- [ ] Tested with at least one agent end-to-end
- [ ] README.md updated if adding features/tools
- [ ] Backward compatible with existing projects
