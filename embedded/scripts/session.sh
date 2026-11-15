#!/bin/sh
# ccodolo session wrapper: PID 1 of every container, exec'd by the startup
# runner with the agent's argv. Runs the agent inside a tmux session named
# "agent" so it survives the launch terminal going away and can be
# reattached (ccodolo --dashboard). The container lives exactly as long as
# that session, and exits with the agent's status.
#
# The shape below is load-bearing:
# - PID 1 has no default SIGTERM action, so without the trap `docker stop`
#   waits out its timeout and SIGKILLs.
# - sh runs traps only once a foreground child exits, so the blocking
#   commands (attach-session, wait-for) run in the background under
#   `wait`, which a trap interrupts.
# - SIGHUP is ignored: losing the launch terminal must not end the session.
set -u

if [ "${CCODOLO_TMUX:-1}" = "0" ] || [ ! -t 0 ] || ! command -v tmux >/dev/null 2>&1; then
    exec "$@"
fi

conf=/etc/ccodolo/tmux.conf
exit_file=/tmp/ccodolo-agent-exit
rm -f "$exit_file"

set -- sh -c '"$@"; echo $? > /tmp/ccodolo-agent-exit; tmux wait-for -S agent-done' sh "$@"
# Start at the terminal's size so the agent's first frame fits; a TTY with
# no size yet (0 0) gets tmux's default.
size=$(stty size 2>/dev/null)
rows=${size% *}
cols=${size#* }
if [ "${rows:-0}" -gt 0 ] 2>/dev/null && [ "${cols:-0}" -gt 0 ] 2>/dev/null; then
    set -- -x "$cols" -y "$rows" "$@"
fi
tmux -f "$conf" new-session -d -s agent "$@" || exit 1

trap 'tmux kill-server 2>/dev/null' TERM INT
trap '' HUP

# A non-interactive sh points a background job's stdin at /dev/null, so
# hand attach the TTY through a spare fd.
exec 3<&0
tmux -f "$conf" attach-session -t agent <&3 3<&- &
wait $!
exec 3<&-

if tmux has-session -t agent 2>/dev/null; then
    echo "Detached; the agent is still running. Reattach with: ccodolo --dashboard" >&2
fi
while tmux has-session -t agent 2>/dev/null; do
    tmux wait-for agent-done &
    wait $!
done

[ -s "$exit_file" ] && exit "$(cat "$exit_file")"
exit 0
