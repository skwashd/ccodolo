#!/bin/sh
# ccodolo git worktree hygiene, run once at container start before the agent.
# Keeps in-repo worktrees (.worktrees/ and .claude/worktrees/) valid across
# container sessions and the host/container path boundary. Non-fatal by
# design: the startup runner warns on failure and launches the agent anyway.
# Exits non-zero only when hygiene was attempted and failed — a repo this
# does not apply to is not a failure.
set -u

status=0

git rev-parse --git-dir >/dev/null 2>&1 || exit 0
top=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
cd "$top" || exit 0
common=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || exit 0

# Keep both worktree conventions out of git status without touching tracked
# files.
exclude="$common/info/exclude"
mkdir -p "$common/info" 2>/dev/null || true
for pattern in '/.worktrees/' '/.claude/worktrees/'; do
    grep -qxF "$pattern" "$exclude" 2>/dev/null && continue
    # An exclude file with no trailing newline would otherwise get the
    # pattern glued onto its last one, silently disabling that rule (and
    # leaving the grep above unable to match on the next run).
    if [ -s "$exclude" ] && [ -n "$(tail -c 1 "$exclude" 2>/dev/null)" ]; then
        printf '\n' >>"$exclude" 2>/dev/null || status=1
    fi
    printf '%s\n' "$pattern" >>"$exclude" 2>/dev/null || status=1
done

# Repair worktrees created on the host or in a previous container session —
# the absolute paths git records differ between host and container. A linked
# worktree's .git is a file; anything else (an unrelated directory, a nested
# clone whose .git is a directory) is skipped, because passing one to
# `git worktree repair` makes it fail for every path in the same run.
set --
for wt in .worktrees/*/ .claude/worktrees/*/; do
    [ -f "${wt}.git" ] && set -- "$@" "${wt%/}"
done
[ $# -gt 0 ] || exit $status
# One path at a time: git fails the whole invocation if any single path is
# bad, so a batch would leave every healthy worktree unrepaired because of
# one leftover directory.
for wt in "$@"; do
    git worktree repair "$wt" || status=1
done

# Rewrite each worktree's .git file to a relative gitdir so plain git
# commands inside the worktree also work on the host, where the container
# absolute path does not exist. The repo->worktree pointer must stay
# absolute: git older than 2.48 resolves a relative one against the process
# cwd, and `git worktree prune` would then discard the worktree's metadata.
if [ "$common" != "$top/.git" ]; then
    # A separate git dir (git clone --separate-git-dir, a submodule) sits at
    # no fixed offset from the worktrees, and usually outside the mount
    # altogether, so there is no relative form to write. Say so rather than
    # leaving the container paths in place silently.
    echo "ccodolo: separate git dir ($common); skipping relative gitdir rewrite" >&2
    exit $status
fi
for wt in "$@"; do
    gitfile="$wt/.git"
    [ -f "$gitfile" ] || continue
    gitdir=$(sed -n 's/^gitdir: //p' "$gitfile" 2>/dev/null)
    case "$gitdir" in
    "$common"/worktrees/*)
        admin=${gitdir#"$common"/worktrees/}
        # One ".." per path segment of $wt, so the depth follows where the
        # worktree actually is instead of a per-convention lookup table that
        # has to be kept in step with the glob above.
        up=..
        rest=$wt
        while [ "$rest" != "${rest#*/}" ]; do
            rest=${rest#*/}
            up=$up/..
        done
        printf 'gitdir: %s\n' "$up/.git/worktrees/$admin" >"$gitfile" || status=1
        ;;
    esac
done

exit $status
