# CCoDoLo: Multi-Agent Coding Environment

A Debian 13 container for running multiple AI coding assistants in YOLO mode. This provides a sandboxed environment for AI agents to execute code.

It isn't perfect, but it is better than letting the bot run directly on your local machine.

The project name is a combination of Claude Code, Docker and YOLO. The original 3 key components of the environment.

## Quickstart

Get started with CCoDoLo in four simple steps:

1. **Clone the repository**:
   ```bash
   git clone git@github.com:skwashd/ccodolo.git ~/.ccodolo
   ```

2. **Add to your PATH**:
   ```bash
   ln -s ~/.ccodolo/docker/ccodolo ~/bin/ccodolo
   # Or any directory in your PATH, e.g., /usr/local/bin
   ```

3. **Build the container**:
   ```bash
   cd ~/.ccodolo/docker
   docker build -t ccodolo:latest --file docker/Dockerfile .
   ```

4. **Navigate to your project**:
   ```bash
   cd /path/to/your/existing/repo
   ```

5. **Launch the container**:
   ```bash
   ccodolo --project my-first-project
   ```

The container will start with Claude Code (the default agent). Your working directory will be mounted at `/workspace/my-first-project/<repo>` inside the container.

## Supported Agents

- **claude** - Anthropic Claude Code
- **codex** - OpenAI Codex
- **copilot** - GitHub Copilot CLI
- **gemini** - Google Gemini CLI
- **kiro** - Kiro AI CLI
- **opencode** - OpenCode AI

## Command

To run the container there is a helper script `ccodolo`. Execute it like so:

```bash
ccodolo --project <project-name> [OPTIONS]
```

### Options

- `--project <name>` - Project name (required). Namespace in `~/.ccodolo/projects/` where agent configuration is stored.
- `--workdir <path>` - Working directory (default: current directory)
- `--agent <name>` - Agent to use: claude, copilot, codex, gemini, kiro, opencode (default: claude or from config)
- `--create-new` - Create new project without confirmation prompt
- `--exec` - Attach to existing container instead of creating new one
- `--help, -h` - Show help message

### Examples

```bash
# Create new project with Claude (with confirmation prompt)
ccodolo --project myapp --workdir ~/code/myapp

# Create new project with Copilot, skip confirmation
ccodolo --project myapp --agent copilot --create-new

# Use existing project (agent from ccodolo.config)
ccodolo --project myapp

# Override configured agent for this session
ccodolo --project myapp --agent gemini

# Attach to existing running container
ccodolo --project myapp --exec
```

## Agent Selection

### Using the ccodolo Script (Recommended)

Agents are selected using the `--agent` flag when running the `ccodolo` script. Each project can have a default agent configured in its `ccodolo.config` file.

### Agent Configuration

When you specify `--agent` on the command line, it is saved to `~/.ccodolo/projects/<project-name>/ccodolo.config`:

```bash
# Creates/updates ccodolo.config with agent="copilot"
ccodolo --project myapp --agent copilot --create-new
```

Subsequent runs will use the configured agent unless overridden:

```bash
# Uses agent from ccodolo.config
ccodolo --project myapp

# Temporarily override without updating config
ccodolo --project myapp --agent gemini
```

### Per-Agent Configuration

Each agent stores its configuration in project-specific directories:

- **claude**: `~/.ccodolo/projects/<project>/.claude/`, `.claude.json`, `.claude-plugin/`
- **codex**: `~/.ccodolo/projects/<project>/.codex/`
- **copilot**: `~/.ccodolo/projects/<project>/.copilot/`
- **gemini**: `~/.ccodolo/projects/<project>/.gemini/`
- **kiro**: `~/.ccodolo/projects/<project>/.kiro/`
- **opencode**: `~/.ccodolo/projects/<project>/.opencode/`

Only the selected agent's directories are mounted into the container.

### Direct Docker Usage (Advanced)

You can also run the container directly without the `ccodolo` script by specifying the agent as an argument:

```bash
docker run -it ccodolo:latest claude
docker run -it ccodolo:latest copilot
docker run -it ccodolo:latest gemini
docker run -it ccodolo:latest codex
docker run -it ccodolo:latest opencode
```

When running directly, you'll need to manually handle volume mounts for persistence:

```bash
# Example: Running Claude with manual mounts
docker run --rm -it \
  -v /path/to/your/code:/workspace \
  -v ~/.ccodolo/projects/myproject/.claude:/home/coder/.claude \
  -v ~/.ccodolo/projects/myproject/.claude.json:/home/coder/.claude.json \
  -v ~/.ccodolo/projects/myproject/commandhistory:/commandhistory \
  -v ~/.ccodolo/projects/myproject/common:/home/coder/project \
  -w /workspace \
  ccodolo:latest claude

# Example: Running Copilot with manual mounts
docker run --rm -it \
  -v /path/to/your/code:/workspace \
  -v ~/.ccodolo/projects/myproject/.copilot:/home/coder/.copilot \
  -v ~/.ccodolo/projects/myproject/commandhistory:/commandhistory \
  -v ~/.ccodolo/projects/myproject/common:/home/coder/project \
  -w /workspace \
  ccodolo:latest copilot
```

The `ccodolo` script automates these mounts and provides additional features like project management, config persistence, and template support.

## Project Directories

Each project maintains isolated configuration under `~/.ccodolo/projects/<project-name>/`:

```
~/.ccodolo/projects/myapp/
├── ccodolo.config       # Project configuration (agent selection, etc.)
├── commandhistory/      # Shell history (.zsh_history, .bash_history)
├── common/              # Shared files (scripts, skills) → ~/project in container
├── .claude/             # Claude-specific config (if using claude)
├── .claude.json         # Claude preferences (if using claude)
├── .copilot/            # Copilot-specific config (if using copilot)
└── .gemini/             # Gemini-specific config (if using gemini)
```

### Common Directory

The `common/` directory is mounted to `~/project` in the container for **all agents**. Use it for:

- Agent-agnostic scripts and utilities
- Skills and prompts shared across sessions
- Documentation or notes you want accessible but not committed to your working directory

```bash
# Inside container, files are accessible at ~/project
$ ls ~/project
my-script.sh  custom-prompts.md  helpers/
```

## Project Templates

You can create a project template at `~/.ccodolo/template/` that will be copied to new projects:

```bash
# Create template
mkdir -p ~/.ccodolo/template/common
echo '#!/bin/bash\necho "Helper script"' > ~/.ccodolo/template/common/helper.sh
echo 'agent="gemini"' > ~/.ccodolo/template/ccodolo.config

# New projects will include template contents
ccodolo --project newapp --create-new
```

**Note**: Use `template.example/` for example templates in this repository. User templates go in `template/` (gitignored).

## Shell Support

The container supports both **zsh** (default) and **bash**:

- **Default shell**: zsh with powerline10k theme
- **Switch to bash**: Use `docker exec -it <container> /bin/bash`
- **Full configuration**: Both shells have identical feature parity (PATH, history, fzf integration)

### Shell Features

Both shells include:
- Command history with 100,000 entry limit
- Commands starting with space are excluded from history (`HIST_IGNORE_SPACE` in zsh, `HISTCONTROL=ignorespace` in bash)
- History files stored in `/commandhistory/` (`.zsh_history` and `.bash_history`)
- fzf integration for fuzzy finding and history search (Ctrl+R)
- Shift+Enter → Ctrl+J key binding (terminal-dependent)
- Full PATH configuration for all installed tools

## Authentication

Each agent requires authentication. Credentials are stored in agent-specific directories under `~/.ccodolo/projects/<project-name>/` to maintain isolation between projects.

**Important**: Environment variables are NOT automatically passed from your host system to the container. Each agent handles authentication within the container through their respective CLIs or configuration files. This prevents accidental exposure of sensitive credentials.

### Claude Code
- **Config directory**: `~/.claude/`
- **Setup**: Automatically prompted on first run
- **Documentation**: https://claude.ai

### GitHub Copilot
- **Requirements**: GitHub account with Copilot subscription
- **Config directory**: `~/.copilot/`
- **Authentication**: Run `gh auth login` inside the container on first use
- **Documentation**: https://github.com/github/copilot-cli

### OpenAI Codex
- **Requirements**: ChatGPT Plus, Pro, Team, Edu, or Enterprise account
- **Config directory**: `~/.codex/`
- **Setup**: Authenticate within the container on first run
- **Documentation**: https://openai.com/codex

### Google Gemini
- **Requirements**: Google account or API key
- **Config directory**: `~/.gemini/`
- **Authentication options**:
  - Login with Google (OAuth) inside container - Free tier: 60 req/min, 1000 req/day
  - Configure API key within the container environment
- **Documentation**: https://ai.google.dev/gemini-api

### Kiro
- **Requirements**: Kiro account
- **Config directory**: `~/.kiro/`
- **Authentication**: Uses device flow authentication on first launch (no browser required)
- **Documentation**: https://kiro.dev/docs/cli/

### OpenCode AI
- **Requirements**: See documentation for specific requirements
- **Config directory**: `~/.opencode/`
- **Setup**: Authenticate within the container on first run
- **Documentation**: https://opencode.ai

## Installed Tools

The container includes:

- **Languages**: Python 3.13, Go 1.26, Node.js
- **Package managers**: pip, npm, uv
- **Cloud tools**: AWS CDK, AWS CLI, Terraform
- **Git tools**: git, git-delta, gh (GitHub CLI)
- **Utilities**: curl, fzf, jq, vim, nano, playwright-cli
- **Networking**: dnsutils, iproute2, aggregate