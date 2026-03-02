#!/usr/bin/env bash
#
# Claude Code "Stop" Hook: Tell Claude to commit before finishing
#
# Instead of committing directly, this hook checks for uncommitted changes
# and tells Claude Code to do the commit itself — leveraging its full
# context of what it just worked on for a better commit message.
#
# Setup:
#   1. mkdir -p .claude/hooks
#   2. cp auto-commit.sh .claude/hooks/auto-commit.sh
#   3. chmod +x .claude/hooks/auto-commit.sh
#   4. Add the hook config to .claude/settings.json (see README)

set -euo pipefail

cd "${CLAUDE_PROJECT_DIR:-.}"

# Read hook input from stdin
input=$(cat)

# Prevent infinite loops: if Claude is already continuing from a stop hook, let it stop
stop_hook_active=$(echo "$input" | grep -o '"stop_hook_active"\s*:\s*true' || true)
if [ -n "$stop_hook_active" ]; then
  exit 0
fi

# Exit early if not a git repo
if ! git rev-parse --is-inside-work-tree &>/dev/null; then
  exit 0
fi

# Check for any uncommitted changes
if git diff --quiet HEAD 2>/dev/null && git diff --cached --quiet 2>/dev/null && [ -z "$(git ls-files --others --exclude-standard)" ]; then
  exit 0
fi

# There are uncommitted changes — block the stop and ask Claude to commit
cat <<'EOF'
{
  "decision": "block",
  "reason": "There are uncommitted changes. Please stage all changes with `git add -A` and commit them with a descriptive commit message. Do no use using commit message prefixes (e.g. feat:, fix:, refactor:, docs:, chore:), just explain the change. Do not ask for confirmation — just commit."
}
EOF

exit 0
