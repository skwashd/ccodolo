# AGENTS.md - Guide for AI Coding Assistants

This document provides development guidelines for AI-powered coding tools working on the CCoDoLo project itself.

**For project features, usage, and architecture**: See [README.md](README.md)

## What This Project Is

CCoDoLo is a meta-project - it provides sandboxed Docker containers for running AI coding assistants (including you). When modifying this codebase, you're improving the infrastructure that runs AI agents safely.

## Repository Structure

```
docker/
├── ccodolo            # Main shell script - container launcher and project manager
├── bin/start-agent    # Container entrypoint (runs inside container)
├── docker/Dockerfile  # Container image definition
└── dotfiles/          # Shell configurations (zsh, bash, powerline10k)
```

Projects are stored in `~/.ccodolo/projects/<name>/` (gitignored).

## Development Guidelines

### Shell Script Validation

**Always validate shell scripts before committing:**

```bash
sh -n ccodolo
sh -n bin/start-agent
bash -n dotfiles/.bashrc
zsh -n dotfiles/.zshrc
```

The `ccodolo` script must remain POSIX-compatible (#!/bin/sh) for portability.

### Docker Best Practices

- Pin base image versions for reproducibility
- Document new tools in README.md "Installed Tools" section
- Keep the image lean - only include tools needed across multiple agents
- Test builds: `docker build -t ccodolo:test -f docker/Dockerfile .`

## Testing Changes

Before committing:

1. **Syntax validation**: `sh -n` on all modified shell scripts
2. **Build test**: Verify container builds successfully
3. **Runtime test**: Create test project and verify agent launches
4. **Multi-agent test**: Test with 2+ agents if touching agent selection logic

```bash
# Quick test flow
./ccodolo --project test-changes --agent claude --create-new
# Inside container, verify mounts and agent launch
exit
./ccodolo --project test-changes --agent copilot  # Test switching
```

## Common Modifications

**Adding a new agent**: Update `ccodolo` (valid agents list, mount logic, help text), update `start-agent` (launch command), document in README.md (see existing agent sections for pattern).

**Adding tools**: Add to Dockerfile, document in README.md. Consider if tool is universal or agent-specific.

**Shell configuration**: Edit `dotfiles/`, ensure both zsh and bash work, test history persistence, rebuild and verify.

## Key Constraints

- `ccodolo` script must remain POSIX sh-compatible
- Environment variables are NOT passed from host to container
- Both zsh and bash must have feature parity
- Project isolation is critical - test that configs don't leak between projects

## Testing Checklist for PRs

- [ ] Shell scripts validated with `sh -n`
- [ ] Container builds successfully
- [ ] Tested with at least one agent end-to-end
- [ ] README.md updated if adding features/tools
- [ ] Backward compatible with existing projects
