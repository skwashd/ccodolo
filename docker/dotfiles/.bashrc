# Bash configuration for coding agent environment

# Configure PATH for all installed tools
# - /home/coder/.local/bin: start-agent.sh, claude, uv, npm global packages
export PATH="/home/coder/.local/bin:$PATH"

# Bash history configuration
export HISTFILE=/commandhistory/.bash_history
export HISTSIZE=100000
export HISTFILESIZE=100000

# Ignore commands starting with space and duplicates
export HISTCONTROL=ignorespace:ignoredups

# Append to history file instead of overwriting
shopt -s histappend

# Editor configuration
export EDITOR=vim
export VISUAL=vim

# Source fzf key bindings and completion for bash
if [ -f /usr/share/doc/fzf/examples/key-bindings.bash ]; then
    . /usr/share/doc/fzf/examples/key-bindings.bash
fi

if [ -f /usr/share/doc/fzf/examples/completion.bash ]; then
    . /usr/share/doc/fzf/examples/completion.bash
fi

# Note: Shift+Enter → Ctrl+J mapping is configured in ~/.inputrc
# This is terminal-dependent and may not work in all terminals
