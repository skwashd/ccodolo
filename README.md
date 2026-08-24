# CCoDoLo: Multi-Agent Coding Environment

CCoDoLo runs AI coding assistants in sandboxed Docker containers, in YOLO
mode. Each project gets an isolated container with only the agent and the
dev tools you need.

The name combines Claude Code, Docker, and YOLO — the three original
components of the environment.

## Contents

- [Prerequisites](#prerequisites)
- [Quickstart](#quickstart)
- [Supported Agents](#supported-agents)
- [Command](#command)
- [Configuration](#configuration)
- [Dev Tools](#dev-tools)
- [Custom Tools](#custom-tools)
- [Custom Build Steps](#custom-build-steps)
- [Project Directories](#project-directories)
- [Image Architecture](#image-architecture)
- [Shell Support](#shell-support)
- [Authentication](#authentication)
- [Project Templates](#project-templates)
- [Troubleshooting](#troubleshooting)
- [Migrating from the Shell Script](#migrating-from-the-shell-script)
- [License](#license)

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) installed and running

## Quickstart

### Install

```bash
# macOS — Homebrew (notarized universal binary, recommended for macOS)
brew install --cask skwashd/tap/ccodolo

# Using Go
go install github.com/skwashd/ccodolo@latest

# Or download a pre-built binary from GitHub releases:
# 1. Download the archive for your OS/architecture from:
#    https://github.com/skwashd/ccodolo/releases/latest
#    macOS: ccodolo_*_darwin_all.tar.gz  (universal binary, runs on Intel and Apple Silicon)
#    Linux: ccodolo_*_linux_amd64.tar.gz or ccodolo_*_linux_arm64.tar.gz
# 2. Extract and move to a directory in your PATH:
tar xzf ccodolo_*.tar.gz
sudo mv ccodolo /usr/local/bin/
# Or install to a user-local directory (no sudo required):
mkdir -p ~/.local/bin && mv ccodolo ~/.local/bin/
# Ensure ~/.local/bin is in your PATH (add to ~/.bashrc or ~/.zshrc):
# export PATH="$HOME/.local/bin:$PATH"
```

> **macOS note:** Apple signs and notarizes release binaries with a
> Developer ID certificate. An archive downloaded through a browser passes
> Gatekeeper's online check on first run, which needs network access to
> Apple's servers. A binary installed with `go install`, or downloaded with
> `curl` or `wget`, carries no quarantine flag, so Gatekeeper does not run
> for those paths.

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

The interactive TUI lets you select dev tools. The container starts with
Claude Code, the default agent. ccodolo mounts your working directory at
`/workspace/my-first-project/<repo>` inside the container.

## Supported Agents

- **antigravity** - Google Antigravity CLI
- **claude** - Anthropic Claude Code
- **codex** - OpenAI Codex
- **copilot** - GitHub Copilot CLI
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
| `--agent <name>` | Agent to use | `claude` (or from configuration) |
| `--tools <list>` | Comma-separated dev tools (supports version pinning: `python:3.12-slim`) | Interactive TUI |
| `--create-new` | Skip confirmation for new project | — |
| `--reconfigure` | Update agent and tools for existing project | — |
| `--exec` | Attach to existing container | — |
| `--rebuild` | Force image rebuild | — |
| `--build-only` | Build image without launching | — |

ccodolo also has a `version` subcommand, which prints the build version, the
commit hash, and the build date. There is no `--version` flag.

`--reconfigure` cannot combine with `--create-new`, `--exec`, or
`--build-only`. If you run `--reconfigure` with no terminal attached, for
example inside a script, you must also pass `--agent`, `--tools`, or both.

### Examples

```bash
# Create project with specific tools (no TUI)
ccodolo --project myapp --create-new --tools python,uv,terraform

# Create project with pinned tool versions
ccodolo --project myapp --create-new --tools python:3.12-slim,nodejs:22-slim

# Use existing project (agent from ccodolo.toml)
ccodolo --project myapp

# Switch agent for a session
ccodolo --project myapp --agent antigravity

# Reconfigure existing project (interactive TUI)
ccodolo --project myapp --reconfigure

# Reconfigure via flags (non-interactive)
ccodolo --project myapp --reconfigure --agent antigravity --tools python,uv,terraform

# Attach to running container
ccodolo --project myapp --exec

# Pass flags directly to the agent
ccodolo --project myapp -- -p "Refactor the auth module"

# Build image for CI/CD
IMAGE=$(ccodolo --project myapp --build-only)

# Force rebuild after configuration changes
ccodolo --project myapp --rebuild
```

## Configuration

CCoDoLo uses TOML configuration with two levels:

- **Global**: `~/.ccodolo/ccodolo.toml` — defaults for all projects
- **Project**: `~/.ccodolo/projects/<name>/ccodolo.toml` — project-specific overrides

### Example configuration

```toml
agent = "claude"
passthrough_vars = [
    "GITHUB_TOKEN",
    "AWS_SECRET_ACCESS_KEY",
    "ANTHROPIC_API_KEY",
]

[tools]
python = ""
uv = ""
nodejs = ""

[build]
root_steps = [
    '''RUN curl -fsSL https://pki.acme.example/root-ca.crt -o /tmp/internal-ca.crt \
     && openssl x509 -in /tmp/internal-ca.crt -noout -fingerprint -sha256 \
        | grep -q 'SHA256 Fingerprint=3A:1B:5C:...:FF' \
     && mv /tmp/internal-ca.crt /usr/local/share/ca-certificates/internal-ca.crt \
     && update-ca-certificates''',
]
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
| `tools` | Union, deduplicated by name. Project version overrides. |
| `build.root_steps` | Concatenated (global first, then project) |
| `build.custom_steps` | Concatenated (global first, then project) |
| `volumes` | Union (project overrides if same container path) |
| `environment` | Merged (project keys override global) |
| `passthrough_vars` | Union, deduplicated, order preserved. Global entries first. |

### Passthrough env vars

`passthrough_vars` lists host environment variable names to forward into
the container at run time. ccodolo reads each value from the shell that
invokes it. Use this for secrets, for example API keys and tokens. Do not
commit these values to `ccodolo.toml`.

The host name is reused as the container name. If a listed variable is not
set on the host, ccodolo prints a warning to stderr and omits the variable.
The container still starts.

`passthrough_vars` is a top-level key. Like `agent` and `tools`, it must
appear *before* any `[table]` header (`[environment]`, `[build]`,
`[[volumes]]`), or TOML scopes it inside that table instead.

### Migration from ccodolo.config

If you have an existing `ccodolo.config`, ccodolo migrates it to
`ccodolo.toml` automatically on first run. ccodolo renames the old file to
`ccodolo.config.bak`.

## Dev Tools

ccodolo installs tools with multi-stage `COPY --from`, for fast and
reproducible builds. Select tools during project creation in the TUI, with
`--tools`, or later with `--reconfigure`:

| Tool | Category | Description |
|------|----------|-------------|
| `bun` | Language Runtimes | Bun runtime |
| `deno` | Language Runtimes | Deno runtime |
| `dotnet` | Language Runtimes | .NET SDK |
| `golang` | Language Runtimes | Go |
| `java` | Language Runtimes | Java (Eclipse Temurin) JDK |
| `nodejs` | Language Runtimes | Node.js |
| `php` | Language Runtimes | PHP |
| `python` | Language Runtimes | Python |
| `ruby` | Language Runtimes | Ruby |
| `rust` | Language Runtimes | Rust toolchain (includes cargo) |
| `composer` | Package Managers | Composer PHP package manager |
| `gradle` | Package Managers | Gradle build tool |
| `maven` | Package Managers | Apache Maven |
| `pixi` | Package Managers | Conda/PyPI package manager (prefix-dev/pixi) |
| `pnpm` | Package Managers | pnpm package manager |
| `skills` | Package Managers | Vercel skill installer |
| `uv` | Package Managers | Python package manager (astral-sh/uv) |
| `yarn` | Package Managers | Yarn package manager |
| `aws-cdk` | Cloud / IaC | AWS CDK |
| `aws-cli` | Cloud / IaC | AWS CLI v2 |
| `azure-cli` | Cloud / IaC | Azure CLI |
| `cloudflare-cli` | Cloud / IaC | Cloudflare CLI (cf) — Workers, Pages, DNS, D1, R2, KV, and more |
| `gcloud` | Cloud / IaC | Google Cloud CLI |
| `helm` | Cloud / IaC | Helm package manager for Kubernetes |
| `kubectl` | Cloud / IaC | Kubernetes CLI |
| `terraform` | Cloud / IaC | HashiCorp Terraform |
| `terraform-docs` | Cloud / IaC | terraform-docs documentation generator |
| `tflint` | Cloud / IaC | TFLint Terraform linter |
| `mysql-client` | Database Clients | MySQL/MariaDB client |
| `postgresql-client` | Database Clients | PostgreSQL client |
| `redis-cli` | Database Clients | Redis CLI client |
| `sqlite` | Database Clients | SQLite database engine |
| `hugo` | Testing | Hugo static site generator (extended) |
| `playwright` | Testing | Playwright browser testing CLI with pre-built Chromium |
| `playwright-cli` | Testing | Playwright agent CLI with SKILLs (@playwright/cli) |
| `chromium` | Testing | Chromium browser (headless-capable, for browser automation/testing) |
| `lighthouse` | Testing | Google Lighthouse web page auditing CLI |
| `acli` | Utilities | Atlassian CLI |
| `ffmpeg` | Utilities | FFmpeg audio/video toolkit |
| `gh` | Utilities | GitHub CLI |
| `golangci-lint` | Utilities | Go linters aggregator |
| `imagemagick` | Utilities | ImageMagick image processing suite |
| `linear-cli` | Utilities | Linear CLI (unofficial) |
| `lychee` | Utilities | Lychee link checker |
| `make` | Utilities | GNU Make |
| `op` | Utilities | 1Password CLI |
| `pinact` | Utilities | Pin GitHub Actions versions |
| `readwise` | Utilities | Readwise Reader CLI |
| `rumdl` | Utilities | Markdown linter |
| `ssh` | Utilities | OpenSSH client |
| `twg` | Utilities | Atlassian Teamwork Graph CLI |
| `wget` | Utilities | GNU Wget |
| `youtube-transcript-api` | Utilities | Python library/CLI to fetch YouTube transcripts |
| `yt-dlp` | Utilities | yt-dlp video downloader |
| `zizmor` | Utilities | GitHub Actions workflow security analyzer |

Default versions are pinned in `internal/tool/tool.go` and are bumped
automatically by the updater. See
[Running the tool updater locally](CLAUDE.md#running-the-tool-updater-locally)
in `CLAUDE.md`.

### Tool dependencies

These tools install their dependencies automatically:

- `composer` installs `php`
- `gradle` installs `java`
- `maven` installs `java`
- `pnpm` installs `nodejs`
- `skills` installs `nodejs`
- `yarn` installs `nodejs`
- `aws-cdk` installs `nodejs`
- `cloudflare-cli` installs `nodejs`
- `tflint` installs `terraform`
- `hugo` installs `golang`
- `golangci-lint` installs `golang`
- `playwright` installs `nodejs`
- `playwright-cli` installs `playwright`
- `lighthouse` installs `nodejs` and `chromium`
- `linear-cli` installs `nodejs`
- `readwise` installs `nodejs`
- `youtube-transcript-api` installs `python`
- `yt-dlp` installs `python`
- npm-based agents (`claude`, `codex`, `copilot`, `opencode`) auto-install `nodejs`

### Base system tools (always installed)

Every container includes these, regardless of tool selection:

- **Shell**: zsh (default), bash
- **Editor**: vim
- **Git**: git
- **Security**: ca-certificates, gpg
- **Utilities**: curl, fzf, jq, less, procps, sudo, unzip, xxd, xz-utils
- **System**: adduser, libatomic1, locales (locale set to `en_US.UTF-8`)

## Custom Tools

You can add tools, override a built-in tool, or remove a built-in tool.
Create `~/.ccodolo/custom-tools.json`. ccodolo reads this file on every run.
If the file does not exist, nothing happens. If the file exists but fails to
parse, ccodolo prints a warning to stderr, ignores the file, and continues
the build with the built-in catalog.

See [docs/custom-tools.md](docs/custom-tools.md) for the file format,
template placeholders, and failure modes.

## Custom Build Steps

Add custom Dockerfile instructions with `build.custom_steps`:

```toml
[build]
custom_steps = [
    'RUN sudo apt-get update && sudo apt-get install -y postgresql-client',
    'COPY mytools/lint.sh /usr/local/bin/lint.sh',
]
```

Only **RUN**, **COPY**, and **ADD** are allowed. The single-layer squash
loses other instructions, for example `ENV` and `WORKDIR`.

COPY/ADD source paths resolve relative to the `common/` directory beside
the config file that declared the step:

| Step declared in | Sources resolve against |
|-------------------|--------------------------|
| `~/.ccodolo/ccodolo.toml` (global) | `~/.ccodolo/common/` |
| `~/.ccodolo/projects/<name>/ccodolo.toml` (project) | `~/.ccodolo/projects/<name>/common/` |

This applies to both `custom_steps` and `root_steps`. `common/` doubles as
both the directory ccodolo mounts into the container (see
[Common Directory](#common-directory)) and the staging area for build-time
COPY/ADD sources. A source that doesn't exist fails the build with an error
naming the step and the directory searched.

### Early root steps

`build.root_steps` inserts instructions earlier in the build. They run
immediately after the base `apt` setup and before any network-fetching
step, including the `zsh-in-docker` install and all dev-tool installs. Use
`root_steps` when a later step depends on this setup. For example, an
internal CA certificate or a private apt source must exist before anything
runs `curl` against internal infrastructure.

`root_steps` COPY/ADD sources are staged the same way as `custom_steps`.
`WORKDIR` is `/` during `root_steps` — it only becomes `/workspace` later,
before `custom_steps` runs — so COPY/ADD destinations in `root_steps`
should be absolute paths. A CA-certificate install, fetched over the
network and verified, looks like this:

```toml
[build]
root_steps = [
    '''RUN curl -fsSL https://pki.acme.example/root-ca.crt -o /tmp/internal-ca.crt \
     && openssl x509 -in /tmp/internal-ca.crt -noout -fingerprint -sha256 \
        | grep -q 'SHA256 Fingerprint=3A:1B:5C:...:FF' \
     && mv /tmp/internal-ca.crt /usr/local/share/ca-certificates/internal-ca.crt \
     && update-ca-certificates''',
]
```

`openssl x509 -fingerprint` hashes the DER encoding of the certificate, not
the file bytes. This value matches what your CA publishes. It does not
change with whitespace or line-ending differences on the server. If the
fingerprint does not match, `grep -q` fails. The `&&` chain stops. The build
aborts before the certificate reaches the trust store — ccodolo never trusts
a new root silently.

`root_steps` run as `root`, early in the build. `custom_steps` run later,
after tools are installed. Pick `root_steps` only when a downstream step,
for example a network fetch, an apt source, or the trust store, depends on
this setup. Otherwise, pick `custom_steps`.

## Project Directories

Each project keeps its configuration under
`~/.ccodolo/projects/<project-name>/`. ccodolo creates three items on every
run: `ccodolo.toml`, `commandhistory/`, and `common/`. It also creates the
configuration directory for the agent you select, for example `.claude/`
for Claude Code:

```
~/.ccodolo/projects/myapp/
├── ccodolo.toml         # Project configuration
├── commandhistory/      # Shell history persistence
├── common/               # ~/project in container (agent-agnostic)
└── .claude/              # Config for the agent you last used
```

Directories for other agents appear the first time you use them. For Claude
Code, ccodolo also creates `.claude.json`. If `.claude-plugin/` or
`.claude.json` already exist in the project directory, ccodolo mounts them
into the container.

### Common Directory

ccodolo mounts `common/` to `~/project` in the container, for **all**
agents. Use it for:

- Agent-agnostic scripts and utilities
- Skills and prompts shared across sessions
- Documentation or notes you want available in the container but not
  committed to your working directory

## Image Architecture

ccodolo builds each image dynamically, per project:

1. **Base layer**: Debian trixie-slim with essential system tools (git, zsh, vim, fzf, and more)
2. **Dev tools**: Multi-stage `COPY --from=<source-image>` for selected tools
3. **Custom steps**: User-defined RUN/COPY/ADD instructions
4. **Agent**: Single agent installation
5. **Squash**: `FROM scratch` + `COPY --from=base / /` for a single-layer image

ccodolo tags images `ccodolo:<project>-<agent>-<8-hex-sha256>`, based on the
content of the rendered Dockerfile and the embedded dotfiles. It skips
rebuilds if the image already exists. Use `--rebuild` to force one.

## Shell Support

The container supports both **zsh** (default) and **bash**:

- Default shell: zsh, with oh-my-zsh and the default powerlevel10k theme
- Switch to bash: `ccodolo --project myapp --exec` then `/bin/bash`
- Both shells: 100k history, fzf integration, Shift+Enter mapping
- History files stored in `/commandhistory/` persist across container restarts

The `zsh-in-docker` installer generates `~/.zshrc` at build time. ccodolo's
own dotfile is copied to `~/.zshrc.local` instead and sourced at the end of
the generated `~/.zshrc`, so it always applies. Add your own aliases,
prompt tweaks, or `bindkey`s the same way: a `custom_steps`/`root_steps`
step that appends to `~/.zshrc.local` picks them up on the next shell.

## Authentication

Each agent needs authentication inside the container. Credentials live in
agent-specific project directories, for isolation between agents.

By default, ccodolo does not forward host environment variables into the
container. To forward specific variables, for example API keys, add them to
`passthrough_vars` (see [Passthrough env vars](#passthrough-env-vars)). To
mount credential files, for example `~/.aws`, use `[[volumes]]` instead.

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

### Google Antigravity
- **Config directory**: `.gemini/` (Antigravity reuses Gemini's settings tree at `~/.gemini/antigravity-cli/settings.json`)
- **Setup**: Authenticate within the container on first run
- **Documentation**: https://antigravity.google/docs

### Kiro
- **Config directory**: `.kiro/`
- **Setup**: Uses device flow authentication on first launch (no browser required)
- **Documentation**: https://kiro.dev/docs/cli/

### OpenCode AI
- **Config directory**: `.opencode/`
- **Setup**: Authenticate within the container on first run
- **Documentation**: https://opencode.ai

## Project Templates

Create a template at `~/.ccodolo/template/`. ccodolo copies it to new
projects:

```bash
mkdir -p ~/.ccodolo/template/common
cp my-config ~/.ccodolo/template/
```

See `template.example/` in this repository for example templates. They
include a Claude Code status line, an auto-commit hook, and a PyPI version
lookup skill.

**Note**: user templates go in `~/.ccodolo/template/` (gitignored). The
`template.example/` directory in the repository is for reference only.

## Troubleshooting

### Terminal size is wrong inside the container

**Symptom**: the agent's UI stays at 80x24 instead of the full window size,
and does not change when you resize. The keyboard still works.

**Cause**: Docker sizes the container's pseudo-terminal from *its own
stdout*, and follows window resizes only when that descriptor is a real
terminal. If something between your shell and `ccodolo` replaces stdout
with a pipe, Docker reads no size. The pty stays at the daemon default and
never resizes.

The usual cause is the 1Password CLI. `op run` masks secrets by default. To
redact them, it reads the child process's output, so it gives the child
pipes for stdout and stderr instead of a terminal. stdin stays a real
terminal. This is why only the size is wrong.

**Fix**: turn masking off, which leaves all three streams attached to the
terminal:

```bash
op run --no-masking -- ccodolo --project myapp
```

With masking off, secrets that print inside the container are not redacted
in your terminal history.

Masking gives little protection in this case. With a TTY, Docker merges the
container's stdout and stderr into one raw stream of ANSI escapes and
partial redraws. `op`'s substring match only catches a secret when its
bytes land together in a single read.

Keep masking on for non-interactive, line-based commands, for example
`op run -- ./deploy.sh | tee build.log`. Masking works reliably there.

Any other wrapper that pipes stdout, for example `ccodolo ... | tee` or
some CI runners, causes the same symptom for the same reason.

## Migrating from the Shell Script

If you previously used the shell script version of CCoDoLo:

1. Install the Go binary (see [Install](#install) above)
2. Remove the old shell script from your PATH
3. Run `ccodolo --project <name> --reconfigure` for each existing project,
   to verify and update your configuration

Existing project directories under `~/.ccodolo/projects/` stay compatible.
ccodolo migrates the `ccodolo.config` shell format to `ccodolo.toml`
automatically on first run.

## License

MIT License - see [LICENSE](LICENSE)
