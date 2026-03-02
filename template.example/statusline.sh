#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# Claude Code Powerline Statusline
# ─────────────────────────────────────────────────────────────────────────────
# Single-line powerline statusline with Nerd Font icons.
#
# Layout:
#   Model ▶ Directory ▶ Git Branch ▶ Context % ▶ In tokens │ Out tokens ▶ 5h Quota ▶ 7d Quota
#
# Install:
#   1. Save to ~/.claude/statusline.sh
#   2. chmod +x ~/.claude/statusline.sh
#   3. Add to ~/.claude/settings.json:
#      {
#        "statusLine": {
#          "type": "command",
#          "command": "~/.claude/statusline.sh",
#          "padding": 0
#        }
#      }
#
# Requirements: jq
# Optional:     curl (for quota tracking)
#
# Quota tracking: Auto-detects Claude Code credentials (~/.claude/.credentials.json),
#                 or set CLAUDE_OAUTH_TOKEN env var, or create ~/.claude-session-key
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

# ── Colors ───────────────────────────────────────────────────────────────────
RST='\033[0m'
BOLD='\033[1m'

# Foreground
FG_BLACK='\033[30m'
FG_WHITE='\033[97m'
FG_GRAY='\033[90m'

# Background
BG_BLUE='\033[44m'
BG_MAGENTA='\033[45m'
BG_CYAN='\033[46m'
BG_GREEN='\033[42m'
BG_YELLOW='\033[43m'
BG_RED='\033[41m'
BG_GRAY='\033[100m'

# Foreground matching backgrounds (for powerline separators)
FG_BLUE='\033[34m'
FG_MAGENTA='\033[35m'
FG_CYAN='\033[36m'
FG_GREEN='\033[32m'
FG_YELLOW='\033[33m'
FG_RED='\033[31m'
FG_DGRAY='\033[90m'

# ── Nerd Font Icons ──────────────────────────────────────────────────────────
SEP=""          # Powerline separator
ICON_MODEL="🧠" # Brain
ICON_DIR=""    # Folder
ICON_BRANCH="" # Git branch
ICON_CTX="󰊪"    # Window / context
ICON_IN="󰇚"     # Download arrow (input)
ICON_OUT="󰕒"    # Upload arrow (output)
ICON_QUOTA="󰄉"  # Gauge / meter
ICON_CLOCK=""  # Clock
ICON_CAL=""    # Calendar

# ── Read JSON from stdin ─────────────────────────────────────────────────────
INPUT=$(cat)

# ── Extract fields ───────────────────────────────────────────────────────────
MODEL=$(echo "$INPUT" | jq -r '.model.display_name // "?"')
CWD=$(echo "$INPUT" | jq -r '.workspace.current_dir // .cwd // "?"')
DIR_BASE=$(basename "$CWD")

# Context window data
CTX_PCT=$(echo "$INPUT" | jq -r '.context_window.used_percentage // 0' | cut -d. -f1)
INPUT_TOKENS=$(echo "$INPUT" | jq -r '.context_window.total_input_tokens // 0')
OUTPUT_TOKENS=$(echo "$INPUT" | jq -r '.context_window.total_output_tokens // 0')

# ── Git branch ───────────────────────────────────────────────────────────────
GIT_BRANCH=""
if command -v git &>/dev/null; then
    GIT_BRANCH=$(git -C "$CWD" branch --show-current 2>/dev/null || true)
fi
GIT_BRANCH="${GIT_BRANCH:-detached}"

# ── Format token counts (e.g. 12345 → 12.3k, 1234567 → 1.2M) ───────────────
format_tokens() {
    local n="$1"
    if [[ "$n" -ge 1000000 ]]; then
        printf "%.1fM" "$(echo "scale=1; $n / 1000000" | bc)"
    elif [[ "$n" -ge 1000 ]]; then
        printf "%.1fk" "$(echo "scale=1; $n / 1000" | bc)"
    else
        printf "%d" "$n"
    fi
}

IN_FMT=$(format_tokens "$INPUT_TOKENS")
OUT_FMT=$(format_tokens "$OUTPUT_TOKENS")

# ── Context color (green < 50%, yellow < 80%, red ≥ 80%) ────────────────────
ctx_color() {
    local pct="$1"
    if [[ "$pct" -lt 50 ]]; then
        echo "GREEN"
    elif [[ "$pct" -lt 80 ]]; then
        echo "YELLOW"
    else
        echo "RED"
    fi
}

CTX_LEVEL=$(ctx_color "$CTX_PCT")

# ── Quota (cached, fetched every 60s) ───────────────────────────────────────
QUOTA_CACHE="/tmp/.claude_quota_cache"
QUOTA_TTL=60  # seconds

get_oauth_token() {
    local credfile="$HOME/.claude/.credentials.json"
    if [[ -f "$credfile" ]]; then
        local token
        token=$(jq -r '.claudeAiOauth.accessToken // .claudeAiOauth.access_token // empty' "$credfile" 2>/dev/null)
        if [[ -n "$token" ]]; then
            echo "$token"
            return
        fi
    fi
    return 1
}

fetch_quota() {
    local token
    token=$(get_oauth_token 2>/dev/null) || return 1

    local response
    response=$(curl -sf --max-time 3 \
        -H "Accept: application/json" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $token" \
        -H "anthropic-beta: oauth-2025-04-20" \
        "https://api.anthropic.com/api/oauth/usage" 2>/dev/null) || return 1

    echo "$response" > "$QUOTA_CACHE.data"
    date +%s > "$QUOTA_CACHE.ts"
}

# ── Quota helpers ────────────────────────────────────────────────────────────

# Parse an ISO timestamp into a human-readable time-remaining string.
# For durations ≥ 24h, shows "Xd Yh"; otherwise "Xh Ym".
format_reset_time() {
    local reset_at="$1"
    [[ -z "$reset_at" || "$reset_at" == "null" ]] && return 1

    local reset_epoch
    if [[ "$(uname)" == "Darwin" ]]; then
        reset_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%S" "${reset_at%%.*}" +%s 2>/dev/null \
            || date -j -f "%Y-%m-%dT%H:%M:%SZ" "${reset_at}" +%s 2>/dev/null \
            || echo "0")
    else
        reset_epoch=$(date -d "$reset_at" +%s 2>/dev/null || echo "0")
    fi

    local now_epoch diff_s
    now_epoch=$(date +%s)
    diff_s=$(( reset_epoch - now_epoch ))

    if [[ "$diff_s" -le 0 ]]; then
        echo "now"
    elif [[ "$diff_s" -ge 86400 ]]; then
        local days=$(( diff_s / 86400 ))
        local hours=$(( (diff_s % 86400) / 3600 ))
        echo "${days}d${hours}h"
    else
        local hours=$(( diff_s / 3600 ))
        local mins=$(( (diff_s % 3600) / 60 ))
        echo "${hours}h${mins}m"
    fi
}

# Parse a quota JSON block → sets _Q_PCT and _Q_RESET for the caller.
parse_quota_block() {
    local block="$1"
    _Q_PCT=""
    _Q_RESET=""
    [[ -z "$block" || "$block" == "null" ]] && return 1

    local utilization reset_at
    utilization=$(echo "$block" | jq -r '.utilization // empty')
    reset_at=$(echo "$block" | jq -r '.reset_at // .resets_at // empty')

    if [[ -n "$utilization" && "$utilization" != "null" ]]; then
        _Q_PCT=$(printf "%.0f" "$utilization" 2>/dev/null || echo "0")
    fi

    if command -v date &>/dev/null; then
        _Q_RESET=$(format_reset_time "$reset_at" 2>/dev/null || true)
    fi
}

# ── Fetch & parse quota ─────────────────────────────────────────────────────

Q5_PCT=""
Q5_RESET=""
Q7_PCT=""
Q7_RESET=""

if [[ -f "$QUOTA_CACHE.ts" && -f "$QUOTA_CACHE.data" ]]; then
    CACHE_AGE=$(( $(date +%s) - $(cat "$QUOTA_CACHE.ts") ))
    if [[ "$CACHE_AGE" -gt "$QUOTA_TTL" ]]; then
        fetch_quota &>/dev/null &  # background refresh
    fi
else
    fetch_quota &>/dev/null &  # initial fetch
fi

if [[ -f "$QUOTA_CACHE.data" ]]; then
    QUOTA_DATA=$(cat "$QUOTA_CACHE.data")

    # 5-hour quota
    FIVE_HR=$(echo "$QUOTA_DATA" | jq -r '.five_hour // empty' 2>/dev/null)
    if parse_quota_block "$FIVE_HR" 2>/dev/null; then
        Q5_PCT="$_Q_PCT"; Q5_RESET="$_Q_RESET"
    fi

    # 7-day quota
    SEVEN_DAY=$(echo "$QUOTA_DATA" | jq -r '.seven_day // empty' 2>/dev/null)
    if parse_quota_block "$SEVEN_DAY" 2>/dev/null; then
        Q7_PCT="$_Q_PCT"; Q7_RESET="$_Q_RESET"
    fi
fi

# Defaults if unavailable
Q5_PCT="${Q5_PCT:-—}";   Q5_RESET="${Q5_RESET:-—}"
Q7_PCT="${Q7_PCT:-—}";   Q7_RESET="${Q7_RESET:-—}"

# ── Build Line 1 ────────────────────────────────────────────────────────────
# [Model] ▶ [Directory] ▶ [Git Branch]
LINE=""
# Model
LINE+="${BOLD}${FG_BLACK}${BG_BLUE} ${ICON_MODEL} ${MODEL} ${RST}"
LINE+="${FG_BLUE}${BG_MAGENTA}${SEP}${RST}"
# Directory
LINE+="${BOLD}${FG_BLACK}${BG_MAGENTA} ${ICON_DIR} ${DIR_BASE} ${RST}"
LINE+="${FG_MAGENTA}${BG_CYAN}${SEP}${RST}"
# Git branch
LINE+="${BOLD}${FG_BLACK}${BG_CYAN} ${ICON_BRANCH} ${GIT_BRANCH} ${RST}"

# ── Metrics section ──────────────────────────────────────────────────────────

# Pick context background color
case "$CTX_LEVEL" in
    GREEN)  CTX_BG="$BG_GREEN";   CTX_FG_SEP="$FG_GREEN"  ;;
    YELLOW) CTX_BG="$BG_YELLOW";  CTX_FG_SEP="$FG_YELLOW" ;;
    RED)    CTX_BG="$BG_RED";     CTX_FG_SEP="$FG_RED"    ;;
esac

# Helper: pick bg/fg color for a quota percentage
quota_colors() {
    local pct="$1"
    if [[ "$pct" == "—" ]]; then
        echo "$BG_GRAY" "$FG_DGRAY"
    elif [[ "$pct" -ge 80 ]] 2>/dev/null; then
        echo "$BG_RED" "$FG_RED"
    elif [[ "$pct" -ge 50 ]] 2>/dev/null; then
        echo "$BG_YELLOW" "$FG_YELLOW"
    else
        echo "$BG_GREEN" "$FG_GREEN"
    fi
}

read -r Q5_BG Q5_FG_SEP <<< "$(quota_colors "$Q5_PCT")"
read -r Q7_BG Q7_FG_SEP <<< "$(quota_colors "$Q7_PCT")"

# Format 5h quota display
if [[ "$Q5_PCT" == "—" ]]; then
    Q5_DISPLAY="${ICON_QUOTA} 5h —"
else
    Q5_DISPLAY="${ICON_QUOTA} 5h ${Q5_PCT}%  ${ICON_CLOCK} ${Q5_RESET}"
fi

# Format 7d quota display
if [[ "$Q7_PCT" == "—" ]]; then
    Q7_DISPLAY="${ICON_QUOTA} 7d —"
else
    Q7_DISPLAY="${ICON_QUOTA} 7d ${Q7_PCT}%  ${ICON_CAL} ${Q7_RESET}"
fi

# Separator: cyan → context color
LINE+="${FG_CYAN}${CTX_BG}${SEP}${RST}"
# Context %
LINE+="${BOLD}${FG_BLACK}${CTX_BG} ${ICON_CTX} ${CTX_PCT}% ${RST}"
LINE+="${CTX_FG_SEP}${BG_GRAY}${SEP}${RST}"
# Input tokens
LINE+="${BOLD}${FG_WHITE}${BG_GRAY} ${ICON_IN} ${IN_FMT} ${RST}"
LINE+="${FG_DGRAY}${BG_GRAY}│${RST}"
# Output tokens
LINE+="${BOLD}${FG_WHITE}${BG_GRAY} ${ICON_OUT} ${OUT_FMT} ${RST}"

# 5h Quota
if [[ "$Q5_PCT" == "—" ]]; then
    LINE+="${FG_DGRAY}${BG_GRAY}│${RST}"
    LINE+="${BOLD}${FG_WHITE}${BG_GRAY} ${ICON_QUOTA} 5h — ${RST}"
else
    LINE+="${FG_DGRAY}${Q5_BG}${SEP}${RST}"
    LINE+="${BOLD}${FG_BLACK}${Q5_BG} ${Q5_DISPLAY} ${RST}"
fi

# 7d Quota
if [[ "$Q7_PCT" == "—" ]]; then
    if [[ "$Q5_PCT" == "—" ]]; then
        LINE+="${FG_DGRAY}${BG_GRAY}│${RST}"
    else
        LINE+="${Q5_FG_SEP}${BG_GRAY}${SEP}${RST}"
    fi
    LINE+="${BOLD}${FG_WHITE}${BG_GRAY} ${ICON_QUOTA} 7d — ${RST}"
    LINE+="${FG_DGRAY}${SEP}${RST}"
else
    if [[ "$Q5_PCT" == "—" ]]; then
        LINE+="${FG_DGRAY}${Q7_BG}${SEP}${RST}"
    else
        LINE+="${Q5_FG_SEP}${Q7_BG}${SEP}${RST}"
    fi
    LINE+="${BOLD}${FG_BLACK}${Q7_BG} ${Q7_DISPLAY} ${RST}"
    LINE+="${Q7_FG_SEP}${SEP}${RST}"
fi

# ── Output ───────────────────────────────────────────────────────────────────
printf '%b\n' "$LINE"
