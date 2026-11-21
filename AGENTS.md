# AGENTS.md - Guide for AI Coding Assistants

This document provides development guidelines for AI-powered coding tools working on the CCoDoLo project itself.

**For project features, usage, and architecture**: See [README.md](README.md)

**Note**: `AGENTS.md` is the canonical file, and current Claude Code reads it directly. `CLAUDE.md` is a symlink to it for older Claude Code versions that only look for `CLAUDE.md`. Edit `AGENTS.md`; tools that write through the symlink in place (`sed -i`, `perl -i`) replace it with a regular file, so check `git status` for a `T` (type change) on `CLAUDE.md`.

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
│       ├── validate.yml             # go vet, go test, golangci-lint (default and secrets_1password), cross-platform build, ccodolo-1password snapshot build, docker image smoke test
│       └── zizmor.yml               # GitHub Actions security scanning
├── .golangci.yml                    # Default linters + gofmt as a formatter check
├── .goreleaser.yml                  # Cross-platform binaries (ccodolo and ccodolo-1password) + checksums + build attestation
├── go.mod / go.sum
├── main.go                          # Entrypoint → cmd.Execute()
├── cmd/
│   ├── root.go                      # cobra root command, flags, main run() logic, secrets fetch, TUI
│   ├── dashboard.go                 # dashboard subcommand; deprecated --dashboard flag warning
│   ├── repair.go                    # repair-worktrees subcommand (host-side git worktree repair)
│   ├── secrets_test.go              # --secrets/--no-secrets and launch ordering, through the runRoot seams
│   └── version.go                   # version subcommand (also lists compiled-in secrets providers)
├── internal/
│   ├── agent/
│   │   ├── agent.go                 # Agent enum, metadata registry
│   │   └── agent_test.go
│   ├── config/
│   │   ├── config.go                # TOML struct, Load/Save, merge, validate
│   │   ├── config_test.go
│   │   ├── migrate.go               # ccodolo.config → ccodolo.toml migration
│   │   └── migrate_test.go
│   ├── dashboard/                   # `ccodolo dashboard` TUI (bubbletea)
│   │   ├── dashboard.go             # Run(), backend detection
│   │   ├── sources.go               # Stats/events feeds per backend, pause while attached
│   │   ├── model.go                 # State, keys, actions
│   │   ├── view.go                  # Rendering
│   │   └── model_test.go
│   ├── docker/
│   │   ├── actions.go               # Dashboard action argv (attach, shells, tasks, stop) for both backends
│   │   ├── actions_test.go
│   │   ├── build.go                 # Image build orchestration, build-context staging
│   │   ├── build_test.go
│   │   ├── dockerfile.go            # Dockerfile template rendering
│   │   ├── dockerfile_test.go
│   │   ├── exec_unix.go             # execCLI: replace the process via execve (see Architecture Notes)
│   │   ├── exec_windows.go          # execCLI: child process with inherited console (no execve on Windows)
│   │   ├── hash.go                  # SHA-256 image tag computation
│   │   ├── hash_test.go
│   │   ├── hooks.go                 # Host-side startup-hook pre-flight warning
│   │   ├── hooks_test.go
│   │   ├── run.go                   # docker run / docker exec argv (--mount binds, env, secrets, TERM defaults), mergeEnv
│   │   ├── run_test.go
│   │   ├── runtime.go               # Runtime enum (docker/apple), host checks
│   │   ├── runtime_test.go
│   │   ├── sessions.go              # Session discovery: labels, inspect, legacy fallback, docker events
│   │   ├── sessions_test.go
│   │   ├── stats.go                 # docker stats stream, apple stats poll, size parsing
│   │   └── stats_test.go
│   ├── fsutil/
│   │   ├── copy.go                  # CopyDir/CopyFile, mode-preserving
│   │   └── copy_test.go
│   ├── project/
│   │   ├── project.go               # Dir creation, template copying
│   │   ├── project_test.go
│   │   ├── setup.go                 # Agent-specific JSON config (copilot/kiro)
│   │   └── setup_test.go
│   ├── secrets/
│   │   ├── secrets.go               # Provider interface, registry, known providers, spec parsing
│   │   ├── secrets_test.go
│   │   ├── inject.go                # Resolve (fetch with timeout/cancel), name and conflict checks
│   │   ├── inject_test.go
│   │   ├── onepassword.go           # 1Password provider; only built with -tags secrets_1password
│   │   └── onepassword_test.go      # same tag; no live SDK call
│   ├── tool/
│   │   ├── custom_tools_test.go
│   │   ├── readme_test.go           # Fails when the README Dev Tools table drifts from the catalog
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
│   ├── dotfiles/                    # Shell configurations (.bashrc, .zshrc.local, .inputrc)
│   ├── scripts/                     # Container scripts: git worktree hygiene, session.sh (PID 1, agent in tmux), tmux.conf
│   └── terminfo/                    # Terminfo sources Debian lacks (xterm-ghostty), compiled into the image with tic
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
GOOS=windows go build -o /dev/null ./...   # the binary ships for Windows; catch unix-only imports here

# The same again with the 1Password provider compiled in. Without the tag,
# internal/secrets/onepassword.go is never compiled, tested or linted.
go vet -tags=secrets_1password ./...
go test -tags=secrets_1password ./...
golangci-lint run --build-tags=secrets_1password ./...
```

### Architecture Notes

- The `ccodolo` binary is a cross-platform Go CLI (Linux, macOS, Windows) using cobra for flags and charmbracelet/huh for the interactive TUI
- `execCLI` (hand the terminal to `docker`/`container`) is split by build tag. `exec_unix.go` replaces the process with execve — this is load-bearing, not a style choice: running the CLI as a child with forwarded stdio made agent TUIs unresponsive under Ghostty ([#29](https://github.com/skwashd/ccodolo/issues/29), commit `559c549`). Do not "unify" it back to `exec.Command`. `exec_windows.go` has no execve to use and runs a child; it is the one place that class of bug can recur, and CI cannot exercise it (see CI/CD), so a real Windows launch is part of the release check
- Host paths and container paths are different things: container paths are always Linux paths, so inspect them with `path`, never `path/filepath` (`filepath.IsAbs("/home/coder")` is false on Windows). Host paths go through `filepath`. Bind mounts use `--mount type=bind,source=,target=` rather than `-v` because a Windows drive letter makes the colon form ambiguous, and both backends accept the same keys
- `internal/docker` supports two container backends via the `Runtime` enum: `docker` (default) and `apple` (experimental, Apple's `container` CLI on macOS 26/Apple Silicon, opted into via `runtime = "apple"` in ccodolo.toml). The apple path cannot run in CI, so backend argv construction must stay in pure, unit-tested functions (`buildArgs`, `runArgs`, `parse*ContainerList`, `matchContainer`)
- Secrets providers are opt-in at build time. The convention is one build tag and one release build per provider: `secrets_1password` and `ccodolo-1password` today. A tagged provider file only calls `secrets.Register` from `init`; the registry, spec parsing, fetch and conflict checks in `internal/secrets` are always built and must not import a provider SDK. A new provider adds a tagged file, an entry in `knownProviders` (`internal/secrets/secrets.go`) and in `config.SecretProviderNames` (a test keeps the two in step), a goreleaser build, and the tagged runs in `validate.yml`. A default build must still be able to validate a config that has `[secrets]`, and to run `--exec` and `--build-only` against it: `cmd/root.go` calls `secrets.Lookup` only when a container is about to launch
- 1Password is a separate build, not part of the default one, because the SDK links its 9.5 MB WASM core even though desktop-app mode never uses it: the stripped binary goes from about 8 MB to about 26 MB. Desktop-app mode also needs cgo on macOS and Linux (the SDK `dlopen`s the app's `libop_sdk_ipc_client`), and the default build is cgo-free
- The macOS `ccodolo-1password` builds link with `-linkmode=internal`. They are cross-compiled on Linux with `zig cc`, and zig has no macOS SDK stubs, so external linking fails on `-lresolv`, CoreFoundation and Security; Go's internal linker only records those system library paths. Do not remove the flag without a macOS SDK on the runner
- Secret values travel in one way only: `docker.Run` adds them to the environment handed to the runtime CLI (`execCLI`'s `extraEnv`, merged by `mergeEnv`) and `runArgs` emits a bare `-e NAME`. `runArgs` is given names, never values, so a value cannot reach argv. Do not `os.Setenv` them, write them to a file, or print them in an error. `passthrough_vars` uses the same bare `-e NAME` on both runtimes
- Each image contains exactly one agent — no start-agent dispatch script
- `embedded/scripts/session.sh` is PID 1 (the startup runner execs it) and runs the agent in the `agent` tmux session; user-facing behaviour: see [README → Sessions and Dashboard](README.md#sessions-and-dashboard). Its trap/background-`wait` shape is load-bearing: PID 1 has no default SIGTERM action, and sh defers traps until a foreground child exits, so a foreground wait would make `docker stop` hang until SIGKILL. `validate.yml` smoke-tests the exit status and stop latency, the only coverage of the tmux path
- Every tmux call from the host passes `-f /etc/ccodolo/tmux.conf`: whichever call starts the server decides its config, and Ctrl+] detaching depends on it
- Sessions are found by the `ccodolo.*` labels `runArgs` sets (`internal/docker/sessions.go`). Containers from before the labels are recovered from the `ccodolo-` name prefix, the `/workspace/<project>/…` mount, and the image tag; keep that fallback, because those containers can still be running
- The dashboard must stay cheap on the host: long-lived `docker stats`/`docker events` streams, not a process per tick; tmux state read on selection, not per tick; streams paused during `tea.ExecProcess`. Attaching runs `docker exec` as a child of the Go process, the shape behind #29. If a terminal breaks there, the Unix fix is to hand off via `execCLI` and re-exec `ccodolo dashboard` afterwards
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

1. **Go validation**: `go vet ./...`, `go test ./...`, `golangci-lint run ./...`, then all three again with the `secrets_1password` tag (see Go Code Validation)
2. **Build test**: `go build -o /dev/null .`
3. **Runtime test**: Create test project and verify agent launches
4. **Multi-agent test**: Test with 2+ agents if touching agent selection logic
5. **Windows**: CI runs vet/test/lint on `windows-latest` but cannot launch a Linux container there. A change to `exec_windows.go`, the mount argv, TTY detection, or the environment handed to the runtime CLI needs a manual launch on a Windows machine with Docker Desktop — including typing into the agent TUI (the #29 symptom)

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

**Adding a new tool**: Update `internal/tool/tool.go` (add to catalog with source image, default tag, COPY instructions, dependencies). Add a row to the Dev Tools table in README.md — `TestREADMEMatchesCatalog` (`internal/tool/readme_test.go`) fails without it. Do not put a version number in the row; versions live in `tool.go` only. If the tool needs to authenticate or otherwise run something once at container start, set `StartupHook` (literal shell, not template-rendered) and `StartupHookVars` (env vars gating it — see README → [Startup hooks](README.md#startup-hooks)) instead of cramming setup into `Instructions`.

**Shell configuration**: Edit `embedded/dotfiles/`, ensure both zsh and bash work, test history persistence, rebuild and verify.

**Changing the Dockerfile template**: Edit `embedded/Dockerfile.tmpl`. Note that ENV, USER, WORKDIR, and ENTRYPOINT must be re-declared after the `FROM scratch` squash since they are metadata-only instructions.

## Key Constraints

- Host env vars are not forwarded by default — use `passthrough_vars` (see [README → Passthrough env vars](README.md#passthrough-env-vars)), or `[secrets]` to fetch them from a secrets manager (see [README → Secrets](README.md#secrets))
- Both zsh and bash must have feature parity (see [README → Shell Support](README.md#shell-support))
- Project isolation is critical - test that configs don't leak between projects
- Custom steps must be RUN/COPY/ADD only (squash constraint — other instructions are lost in the `FROM scratch` layer)
- All code must pass `golangci-lint` (errcheck, etc.)

## CI/CD

- **validate.yml**: Runs on PRs and non-main pushes — `go vet`, `go test -race`, `golangci-lint` on Ubuntu; the same (minus `-race`) on `windows-latest`, which is the only lint coverage `exec_windows.go` gets; all of those a second time with `-tags secrets_1password`, the only coverage `onepassword.go` gets; a cgo-free cross-platform build matrix (`linux,darwin,windows` × `amd64,arm64`); a `Build (1password)` job that installs zig and runs `goreleaser build --snapshot --id ccodolo-1password`, which cross-compiles that build's six targets with the real release config; and a Docker image smoke test that builds a `claude` + node/go/python image via `ccodolo --build-only` and runs it to verify the toolchain is launchable inside the squashed image. The smoke test stays on Ubuntu for good: GitHub's Windows runners have no nested virtualisation, so they cannot run Linux containers
- **release.yml**: Triggered by `v*` tags — installs zig (the C compiler for the cgo targets of `ccodolo-1password`; pinned by commit SHA, keep the version in step with validate.yml), then runs goreleaser to build cross-platform binaries for both builds + `checksums.txt`, and attests the checksums (`actions/attest`); artifacts published to GitHub releases
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
- [ ] The same three pass with the `secrets_1password` tag
- [ ] `GOOS=windows go build -o /dev/null ./...` passes
- [ ] Tested with at least one agent end-to-end
- [ ] README.md updated if adding features/tools
- [ ] Backward compatible with existing projects
