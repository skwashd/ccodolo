# CCoDoLo: Multi-Agent Coding Environment

Sandboxed Docker containers for running AI coding assistants in YOLO mode. Each project gets an isolated container with only the agent and dev tools you need.

The project name is a combination of Claude Code, Docker and YOLO. The original 3 key components of the environment.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) installed and running

## Quickstart

### Install

```bash
# Using Go
go install github.com/skwashd/ccodolo@latest

# Or download a pre-built binary from GitHub releases:
# 1. Download the archive for your OS/architecture from:
#    https://github.com/skwashd/ccodolo/releases/latest
# 2. Extract and move to a directory in your PATH:
tar xzf ccodolo_*.tar.gz
sudo mv ccodolo /usr/local/bin/
# Or install to a user-local directory (no sudo required):
mkdir -p ~/.local/bin && mv ccodolo ~/.local/bin/
# Ensure ~/.local/bin is in your PATH (add to ~/.bashrc or ~/.zshrc):
# export PATH="$HOME/.local/bin:$PATH"
```

### Build from source

```bash
git clone https://github.com/skwashd/ccodolo.git
cd ccodolo
go build -o ccodolo .
# Optionally move to a directory in your PATH:
sudo mv ccodolo /usr/local/bin/
```

### Launch

```bash
cd /path/to/your/repo
ccodolo --project my-first-project --create-new
```

The interactive TUI will let you select dev tools. The container will start with Claude Code (the default agent). Your working directory will be mounted at `/workspace/my-first-project/<repo>` inside the container.

## Supported Agents

- **claude** - Anthropic Claude Code
- **codex** - OpenAI Codex
- **copilot** - GitHub Copilot CLI
- **gemini** - Google Gemini CLI
- **kiro** - Kiro AI CLI
- **opencode** - OpenCode AI

## Command

```bash
ccodolo --project <project-name> [OPTIONS] [-- extra-agent-args]
```

### Options

| Flag | Description | Default |
|------|-------------|---------|
| `--project <name>` | Project name (required) | — |
| `--workdir <path>` | Working directory to mount | Current directory |
| `--agent <name>` | Agent to use | `claude` (or from config) |
| `--tools <list>` | Comma-separated dev tools (supports version pinning: `python:3.12-slim`) | Interactive TUI |
| `--create-new` | Skip confirmation for new project | — |
| `--reconfigure` | Update agent and tools for existing project | — |
| `--exec` | Attach to existing container | — |
| `--rebuild` | Force image rebuild | — |
| `--build-only` | Build image without launching | — |

### Examples

```bash
# Create project with specific tools (no TUI)
ccodolo --project myapp --create-new --tools python,uv,terraform

# Create project with pinned tool versions
ccodolo --project myapp --create-new --tools python:3.12-slim,nodejs:22-slim

# Use existing project (agent from ccodolo.toml)
ccodolo --project myapp

# Switch agent for a session
ccodolo --project myapp --agent gemini

# Reconfigure existing project (interactive TUI)
ccodolo --project myapp --reconfigure

# Reconfigure via flags (non-interactive)
ccodolo --project myapp --reconfigure --agent gemini --tools python,uv,terraform

# Attach to running container
ccodolo --project myapp --exec

# Pass flags directly to the agent
ccodolo --project myapp -- -p "Refactor the auth module"

# Build image for CI/CD
IMAGE=$(ccodolo --project myapp --build-only)

# Force rebuild after config changes
ccodolo --project myapp --rebuild
```

## Configuration

CCoDoLo uses TOML configuration with two levels:

- **Global**: `~/.ccodolo/ccodolo.toml` — defaults for all projects
- **Project**: `~/.ccodolo/projects/<name>/ccodolo.toml` — project-specific overrides

### Example config

```toml
agent = "claude"

[tools]
python = ""
uv = ""
nodejs = ""

[build]
custom_steps = [
    'RUN sudo apt-get update && sudo apt-get install -y postgresql-client',
]

[[volumes]]
host = "~/.aws"
container = "/home/coder/.aws"
readonly = true

[environment]
AWS_PROFILE = "dev"
```

### Merge semantics (global + project)

| Field | Strategy |
|-------|----------|
| `agent` | Project overrides global |
| `tools` | Union (deduplicated by name; project version overrides) |
| `build.custom_steps` | Concatenated (global first, then project) |
| `volumes` | Union (project overrides if same container path) |
| `environment` | Merged (project keys override global) |

### Migration from ccodolo.config

If you have an existing `ccodolo.config`, it will be automatically migrated to `ccodolo.toml` on first run. The old file is renamed to `ccodolo.config.bak`.

## Dev Tools

Tools are installed via multi-stage `COPY --from` for fast, reproducible builds. Select them during project creation (TUI), via `--tools`, or update later with `--reconfigure`:

| Tool | Category | Description |
|------|----------|-------------|
| `python` | Runtime | Python 3.13 |
| `nodejs` | Runtime | Node.js 22 |
| `golang` | Runtime | Go 1.24 |
| `bun` | Runtime | Bun runtime |
| `rust` | Runtime | Rust toolchain (includes cargo) |
| `ruby` | Runtime | Ruby 3.3 |
| `deno` | Runtime | Deno runtime |
| `php` | Runtime | PHP 8.4 |
| `dotnet` | Runtime | .NET SDK 9.0 |
| `java` | Runtime | Eclipse Temurin JDK 21 |
| `uv` | Package Manager | Python package manager (astral-sh/uv) |
| `composer` | Package Manager | Composer PHP package manager |
| `gradle` | Package Manager | Gradle build tool |
| `maven` | Package Manager | Apache Maven |
| `yarn` | Package Manager | Yarn package manager |
| `pnpm` | Package Manager | pnpm package manager |
| `skills` | Package Manager | Vercel skill installer |
| `terraform` | Cloud | HashiCorp Terraform |
| `aws-cli` | Cloud | AWS CLI v2 |
| `aws-cdk` | Cloud | AWS CDK |
| `gcloud` | Cloud | Google Cloud CLI |
| `azure-cli` | Cloud | Azure CLI |
| `kubectl` | Cloud | Kubernetes CLI |
| `helm` | Cloud | Helm package manager for Kubernetes |
| `mysql-client` | Database | MySQL/MariaDB client |
| `postgresql-client` | Database | PostgreSQL client |
| `redis-cli` | Database | Redis CLI client |
| `sqlite` | Database | SQLite database engine |
| `playwright` | Testing | Playwright browser testing CLI |
| `hugo` | Testing | Hugo static site generator (extended) |
| `gh` | Utilities | GitHub CLI |
| `zizmor` | Utilities | GitHub Actions workflow security analyzer |
| `make` | Utilities | GNU Make |
| `ssh` | Utilities | OpenSSH client |
| `wget` | Utilities | GNU Wget |

### Tool dependencies

These tools automatically install their dependencies:

- `composer` installs `php`
- `gradle` installs `java`
- `maven` installs `java`
- `yarn` installs `nodejs`
- `pnpm` installs `nodejs`
- `skills` installs `nodejs`
- `aws-cdk` installs `nodejs`
- `playwright` installs `nodejs`
- `hugo` installs `golang`
- npm-based agents (codex, copilot, gemini, opencode) auto-install `nodejs`

### Base system tools (always installed)

Every container includes these regardless of tool selection:

- **Shell**: zsh (default, with powerline10k), bash
- **Editor**: vim
- **Git**: git
- **Utilities**: curl, fzf, jq, less, unzip, sudo, procps

### Custom Tools

You can add your own tools, override built-ins, or remove built-ins entirely
by creating `~/.ccodolo/custom-tools.json`. The file is read on every
`ccodolo` invocation. If it does not exist nothing happens. If it exists but
fails to parse, a warning is printed to stderr and the file is ignored — your
build still proceeds with the built-in catalog.

#### File format

```json
{
  "ignore": ["ruby", "php"],
  "tools": [
    {
      "name": "htop",
      "description": "Interactive process viewer",
      "instructions": [
        "RUN apt update && apt install -y --no-install-recommends htop && rm -rf /var/lib/apt/lists/*"
      ]
    },
    {
      "name": "internal-cli",
      "description": "Acme internal CLI",
      "source_image": "registry.acme.internal/tools/internal-cli",
      "default_tag": "1.2.3",
      "instructions": [
        "COPY --from=%s /usr/bin/internal-cli /usr/local/bin/internal-cli"
      ]
    },
    {
      "name": "python",
      "description": "Python 3.11 (corporate-pinned)",
      "source_image": "public.ecr.aws/docker/library/python",
      "default_tag": "3.11",
      "tag_suffix": "-slim",
      "instructions": [
        "COPY --from=%s /usr/local/lib/python{{.Version}} /usr/local/lib/python{{.Version}}",
        "COPY --from=%s /usr/local/bin/python3* /usr/local/bin/",
        "COPY --from=%s /usr/local/bin/pip* /usr/local/bin/",
        "RUN ln -sf /usr/local/bin/python3 /usr/local/bin/python"
      ]
    }
  ]
}
```

Top-level keys:

| Key | Type | Description |
|-----|------|-------------|
| `tools` | array | Custom tool definitions to add to the catalog. |
| `ignore` | array of strings | Names of built-in tools to remove from the catalog. |

Tool entry fields (all snake_case in JSON):

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier. If it matches a built-in, your entry overrides the built-in. |
| `description` | no | Shown in the TUI. |
| `source_image` | no | Docker image to multi-stage `COPY --from`. Required if any instruction uses `%s`. |
| `default_tag` | no | Tag for `source_image`, e.g. `3.13`. If `tag_suffix` is set, the suffix is appended automatically at render time — store the suffix-free version here. |
| `tag_suffix` | no | Suffix appended to `default_tag` and to user-supplied versions when pinning, e.g. `-slim`. |
| `instructions` | yes | List of Dockerfile lines (`RUN ...`, `COPY --from=%s ...`). Supports the placeholders documented in [Template Placeholders](#template-placeholders) below. |
| `dependencies` | no | Other tool names that should be auto-installed when this one is selected. |
| `path_entries` | no | Paths prepended to `PATH` in the final image. |
| `env_vars` | no | Map of environment variables set in the final image. |

A `category` field is parsed but always discarded — every custom tool is
shown under the `Custom` category in the TUI, including overrides of
built-ins.

#### Adding a tool

```json
{
  "tools": [
    {
      "name": "htop",
      "description": "Interactive process viewer",
      "instructions": [
        "RUN apt update && apt install -y --no-install-recommends htop && rm -rf /var/lib/apt/lists/*"
      ]
    }
  ]
}
```

#### Overriding a built-in

A custom entry whose `name` matches a built-in **fully replaces** the
built-in. The example above pins Python to 3.11 by giving the same name as
the built-in `python` tool with a different `default_tag` and matching
`instructions`. An informational message (`custom-tools.json: overriding
built-in tool "python"`) is printed to stderr so the override is visible in
build output.

#### Removing built-ins

```json
{ "ignore": ["ruby", "php"] }
```

The named tools disappear from the TUI and from the catalog entirely. Asking
for them with `--tools ruby` produces a clear "unknown tool" error. If a
removed tool is a dependency of another tool you still want (e.g. ignoring
`nodejs` while leaving `yarn` selectable), the build will fail at resolve
time with `tool "yarn" depends on "nodejs": unknown tool "nodejs"`. Either
also ignore the dependent tool, or supply a custom replacement with the same
name as the ignored built-in.

#### Pulling from an internal registry

The `internal-cli` example in the file format above shows the multi-stage
pattern: set `source_image` to your private registry image, set `default_tag`
to a tag, and use `COPY --from=%s ...` in `instructions`. The `%s` is
substituted with `source_image:default_tag` at render time.

#### Template Placeholders

Every string in `instructions` is rendered through Go's
[`text/template`](https://pkg.go.dev/text/template) package before the image
ref is substituted, so you can parameterise your instructions by the tool's
resolved tag. The following placeholders are available:

| Placeholder | Expands to | Notes |
|-------------|------------|-------|
| `%s` | `source_image:tag` | Classic positional substitution, applied **after** the template pass. Only meaningful inside `COPY --from=%s ...`. Requires `source_image` to be set. |
| `{{.ImageRef}}` | `source_image:tag` | Same value as `%s` but usable anywhere in a line (e.g. shell interpolation). Requires `source_image` to be set. |
| `{{.Tag}}` | The full resolved tag, e.g. `3.13-slim` or `1.5.0` | Use when templating version numbers into `RUN` commands that install or download by version. |
| `{{.Version}}` | `{{.Tag}}` with `tag_suffix` stripped | Use when only the version number is needed, e.g. a path or package-name component. Identical to `{{.Tag}}` if `tag_suffix` is unset. |

The resolved tag is `default_tag` by default, or the user-supplied version
(passed via the CLI `-v` flag or the TUI version picker), with `tag_suffix`
appended when present. User overrides flow through these placeholders
automatically, so a tool whose `instructions` use `{{.Tag}}` lets users pin
any version without editing the catalog.

The `python` entry in the file format example above uses this pattern: its
lib-directory path is `{{.Version}}`-templated, so asking for
`--tools python:3.12` rewrites the `COPY` lines to `/usr/local/lib/python3.12`
and points `COPY --from=%s` at `python:3.12-slim` — a single version change
flows through the whole entry.

Another common pattern — templating a CLI version into a RUN line so users
can override it:

```json
{
  "name": "acme-cli",
  "description": "Acme internal CLI",
  "default_tag": "4.2.1",
  "instructions": [
    "RUN curl -fsSL https://downloads.acme.com/cli/v{{.Tag}}/acme-linux-amd64 -o /usr/local/bin/acme && chmod +x /usr/local/bin/acme"
  ]
}
```

Passing `-v acme-cli=4.3.0` will render
`https://downloads.acme.com/cli/v4.3.0/...` without any catalog change.

A malformed template surfaces as a clear error at resolve time (e.g.
`tool "acme-cli": parsing instruction "...": template: instr:1: ...`) — the
build is aborted rather than silently producing a broken Dockerfile.

#### Constraints

Custom tool instructions can use `RUN` and `COPY --from=image` only. There
is **no** mechanism to stage local files from your host into the build
context — anything a custom tool needs must be fetched at build time over the
network (`curl`, `wget`, `apt`) or pulled from a Docker image via
`COPY --from=`. If you need a build-time secret or a local file, the
existing `build.custom_steps` mechanism is the place for that today.

#### Failure modes

| Situation | Behavior |
|-----------|----------|
| File missing | Silent no-op. |
| File present but corrupt JSON | Warning on stderr; the whole file is ignored. |
| Entry with empty `name` | Warning; that entry is skipped, others still load. |
| Entry with empty `instructions` | Warning; that entry is skipped, others still load. |
| Same `name` twice in `tools` | Warning; last definition wins. |
| `ignore` entry that matches no built-in | Warning; that ignore is dropped. |
| `ignore` removes a tool that another tool depends on | The dependent build fails at resolve time with a clear error. |

## Custom Build Steps

Add custom Dockerfile instructions via `build.custom_steps`:

```toml
[build]
custom_steps = [
    'RUN sudo apt-get update && sudo apt-get install -y postgresql-client',
    'COPY mytools/lint.sh /usr/local/bin/lint.sh',
]
```

Only **RUN**, **COPY**, and **ADD** are allowed. Other instructions (ENV, WORKDIR, etc.) are lost during the single-layer squash.

COPY/ADD source paths resolve relative to the project's `common/` directory.

## Project Directories

Each project maintains isolated configuration under `~/.ccodolo/projects/<project-name>/`:

```
~/.ccodolo/projects/myapp/
├── ccodolo.toml         # Project configuration
├── commandhistory/      # Shell history persistence
├── common/              # ~/project in container (agent-agnostic)
├── .claude/             # Claude-specific config
├── .claude.json
├── .claude-plugin/
├── .copilot/
├── .gemini/
├── .codex/
├── .kiro/
└── .opencode/
```

### Common Directory

The `common/` directory is mounted to `~/project` in the container for **all agents**. Use it for:

- Agent-agnostic scripts and utilities
- Skills and prompts shared across sessions
- Documentation or notes you want accessible but not committed to your working directory

## Image Architecture

Each image is built dynamically per-project:

1. **Base layer**: Debian trixie-slim with essential system tools (git, zsh, vim, fzf, etc.)
2. **Dev tools**: Multi-stage `COPY --from=<source-image>` for selected tools
3. **Custom steps**: User-defined RUN/COPY/ADD instructions
4. **Agent**: Single agent installation
5. **Squash**: `FROM scratch` + `COPY --from=base / /` for a single-layer image

Images are tagged `ccodolo:<project>-<8-char-sha256>` based on content. Rebuilds are skipped if the image already exists (use `--rebuild` to force).

## Shell Support

The container supports both **zsh** (default) and **bash**:

- Default shell: zsh with powerline10k theme
- Switch to bash: `ccodolo --project myapp --exec` then `/bin/bash`
- Both shells: 100k history, fzf integration, Shift+Enter mapping
- History files stored in `/commandhistory/` persist across container restarts

## Authentication

Each agent requires authentication within the container. Credentials are stored in agent-specific project directories for isolation.

**Important**: Environment variables are NOT passed from host to container. Use `[[volumes]]` in config to mount credential files (e.g., `~/.aws`).

### Claude Code
- **Config directory**: `.claude/`
- **Setup**: Automatically prompted on first run
- **Documentation**: https://claude.ai

### GitHub Copilot
- **Config directory**: `.copilot/`
- **Setup**: Run `gh auth login` inside the container on first use
- **Requirements**: GitHub account with Copilot subscription
- **Documentation**: https://github.com/github/copilot-cli

### OpenAI Codex
- **Config directory**: `.codex/`
- **Setup**: Authenticate within the container on first run
- **Requirements**: ChatGPT Plus, Pro, Team, Edu, or Enterprise account
- **Documentation**: https://openai.com/codex

### Google Gemini
- **Config directory**: `.gemini/`
- **Setup**: Login with Google (OAuth) or configure API key within the container
- **Documentation**: https://ai.google.dev/gemini-api

### Kiro
- **Config directory**: `.kiro/`
- **Setup**: Uses device flow authentication on first launch (no browser required)
- **Documentation**: https://kiro.dev/docs/cli/

### OpenCode AI
- **Config directory**: `.opencode/`
- **Setup**: Authenticate within the container on first run
- **Documentation**: https://opencode.ai

## Project Templates

Create a template at `~/.ccodolo/template/` that will be copied to new projects:

```bash
mkdir -p ~/.ccodolo/template/common
cp my-config ~/.ccodolo/template/
```

See `template.example/` in this repository for example templates including a Claude Code statusline, auto-commit hook, and PyPI version lookup skill.

**Note**: User templates go in `~/.ccodolo/template/` (gitignored). The `template.example/` directory in the repo is for reference only.

## Migrating from the Shell Script

If you previously used the shell script version of CCoDoLo:

1. Install the Go binary (see [Install](#install) above)
2. Remove the old shell script from your PATH
3. Run `ccodolo --project <name> --reconfigure` for each existing project to verify and update your configuration

Existing project directories under `~/.ccodolo/projects/` are compatible. The `ccodolo.config` shell format is automatically migrated to `ccodolo.toml` on first run.

## License

MIT License - see [LICENSE](LICENSE)
