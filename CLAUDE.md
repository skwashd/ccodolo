# CLAUDE.md - Guide for AI Coding Assistants

This document provides development guidelines for AI-powered coding tools working on the CCoDoLo project itself.

**For project features, usage, and architecture**: See [README.md](README.md)

**Note**: `AGENTS.md` is a symlink to this file so non-Claude assistants discover it too — edit `CLAUDE.md`, not the symlink.

## What This Project Is

CCoDoLo is a meta-project - it provides sandboxed Docker containers for running AI coding assistants (including you). When modifying this codebase, you're improving the infrastructure that runs AI agents safely.

## Repository Structure

```
ccodolo/
├── .github/
│   ├── dependabot.yml               # Weekly updates for github-actions and gomod
│   ├── zizmor.yml                   # zizmor security scanner config
│   └── workflows/
│       ├── release.yml              # goreleaser (triggered by v* tags)
│       ├── updates.yml              # daily agent + tool version bumps (opens PRs)
│       ├── validate.yml             # go vet, go test, golangci-lint, cross-platform build, docker image smoke test
│       └── zizmor.yml               # GitHub Actions security scanning
├── .goreleaser.yml                  # Cross-platform binaries + checksums + build attestation
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
│   │   ├── build.go                 # Image build orchestration, build-context staging
│   │   ├── build_test.go
│   │   ├── dockerfile.go            # Dockerfile template rendering
│   │   ├── dockerfile_test.go
│   │   ├── hash.go                  # SHA-256 image tag computation
│   │   ├── hash_test.go
│   │   └── run.go                   # docker run / docker exec
│   ├── fsutil/
│   │   ├── copy.go                  # CopyDir/CopyFile, mode-preserving
│   │   └── copy_test.go
│   ├── project/
│   │   ├── project.go               # Dir creation, template copying
│   │   ├── project_test.go
│   │   ├── setup.go                 # Agent-specific JSON config (copilot/kiro)
│   │   └── setup_test.go
│   ├── tool/
│   │   ├── custom_tools_test.go
│   │   ├── tool.go                  # Tool catalog, dependency resolution
│   │   └── tool_test.go
│   └── updater/                     # Automated tool-version bumper (not in released binary)
│       ├── main.go                  # Flags: -check, -json, -write, -only, -timeout, -allow-unverified
│       ├── version.go               # Numeric-tuple version parse/compare
│       ├── plan.go                  # Candidate selection, bump classification
│       ├── source.go                # Fetcher interface + registry dispatch
│       ├── fetch_docker.go          # Docker Hub API
│       ├── fetch_github.go          # GitHub Releases API
│       ├── fetch_npm.go             # npm registry API
│       ├── fetch_pypi.go            # PyPI JSON API
│       ├── fetch_quay.go            # quay.io API
│       ├── rewrite.go               # Anchored-regex file rewriters
│       ├── git.go                   # exec-based git helpers
│       ├── version_test.go
│       └── rewrite_test.go
├── embedded/
│   ├── embed.go                     # go:embed directives
│   ├── Dockerfile.tmpl              # Templatized Dockerfile
│   └── dotfiles/                    # Shell configurations (.bashrc, .zshrc.local, .inputrc)
├── docs/
│   └── custom-tools.md              # Custom tool catalog reference (custom-tools.json)
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
- Each image contains exactly one agent — no start-agent dispatch script
- Dockerfile is generated dynamically from `embedded/Dockerfile.tmpl` using `text/template`
- Config, merge semantics, and `ccodolo.config` migration: see [README → Configuration](README.md#configuration); code lives in `internal/config/`
- Image architecture (multi-stage `COPY --from`, `FROM scratch` squash, content-addressed tags): see [README → Image Architecture](README.md#image-architecture)

### CLI Flags

CLI flags are documented in [README → Command / Options](README.md#options). Flags that matter most for development:
- `--build-only` — print the image tag and exit (used in CI)
- `--rebuild` — force a rebuild after template/catalog edits
- `--reconfigure` — re-run agent/tool selection for an existing project

### Docker Best Practices

- Tool source images are pinned in `internal/tool/tool.go`
- Custom build steps and tool dependencies: see [README → Custom Build Steps](README.md#custom-build-steps) and [README → Tool dependencies](README.md#tool-dependencies)

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
./ccodolo --project test-changes --reconfigure --agent antigravity --tools python:3.12-slim,nodejs
# Verify: ccodolo.toml updated, diff shown before applying

# Test reconfigure interactive (manual)
./ccodolo --project test-changes --reconfigure
# Verify: TUI shows current selections pre-selected, diff + confirmation before saving
```

## Common Modifications

**Adding a new agent**: Update `internal/agent/agent.go` (add to registry with install cmd, entrypoint, config dir, dependencies). Add agent-specific setup in `internal/project/setup.go` and wire it in `cmd/root.go`. Document in README.md.

**Adding a new tool**: Update `internal/tool/tool.go` (add to catalog with source image, default tag, COPY instructions, dependencies). Add a row to the Dev Tools table in README.md — `TestREADMEMatchesCatalog` (`internal/tool/readme_test.go`) fails without it. Do not put a version number in the row; versions live in `tool.go` only.

**Shell configuration**: Edit `embedded/dotfiles/`, ensure both zsh and bash work, test history persistence, rebuild and verify.

**Changing the Dockerfile template**: Edit `embedded/Dockerfile.tmpl`. Note that ENV, USER, WORKDIR, and ENTRYPOINT must be re-declared after the `FROM scratch` squash since they are metadata-only instructions.

## Key Constraints

- Host env vars are not forwarded by default — use `passthrough_vars` (see [README → Passthrough env vars](README.md#passthrough-env-vars))
- Both zsh and bash must have feature parity (see [README → Shell Support](README.md#shell-support))
- Project isolation is critical - test that configs don't leak between projects
- Custom steps must be RUN/COPY/ADD only (squash constraint — other instructions are lost in the `FROM scratch` layer)
- All code must pass `golangci-lint` (errcheck, etc.)

## CI/CD

- **validate.yml**: Runs on PRs and non-main pushes — `go vet`, `go test -race`, `golangci-lint`, plus a cross-platform build matrix (`linux,darwin` × `amd64,arm64`), and a Docker image smoke test that builds a `claude` + node/go/python image via `ccodolo --build-only` and runs it to verify the toolchain is launchable inside the squashed image
- **release.yml**: Triggered by `v*` tags — runs goreleaser to build cross-platform binaries + `checksums.txt`, and attests the checksums (`actions/attest`); artifacts published to GitHub releases
- **zizmor.yml**: GitHub Actions security scanning
- **updates.yml**: Daily cron (`7 20 * * *`) — two jobs:
  - `update` checks npm for new agent versions, opens one combined PR for minor/patch bumps and a separate PR per major
  - `update-tools` runs `go run ./internal/updater -json` to detect tool catalog updates, then opens PRs the same way
  - Both jobs use the `agent-updates` environment with `UPDATE_AGENTS_TOKEN` (fine-grained PAT with Contents + Pull requests write)
- **dependabot.yml**: Weekly updates for github-actions and gomod ecosystems

### Running the tool updater locally

```bash
# Check what's out of date (no changes written) — this is the default -check mode
go run ./internal/updater

# Output machine-readable JSON plan (used by updates.yml)
go run ./internal/updater -json

# Apply updates and commit each tool separately
go run ./internal/updater -write

# Update only specific tools
go run ./internal/updater -write -only=golang,python,nodejs

# Adjust the per-fetch network timeout (default 2m)
go run ./internal/updater -timeout=5m

# Include candidates with no publish timestamp (normally skipped)
go run ./internal/updater -allow-unverified
```

`internal/updater` is a `package main` that is compiled and tested by `validate.yml` but is not included in the released binary (goreleaser only builds root `main.go`).

## Testing Checklist for PRs

- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] `golangci-lint run ./...` passes
- [ ] Tested with at least one agent end-to-end
- [ ] README.md updated if adding features/tools
- [ ] Backward compatible with existing projects
