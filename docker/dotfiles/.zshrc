# Zsh configuration for coding agent environment

# Configure PATH for all installed tools
# - /home/coder/.local/bin: start-agent.sh, claude, uv, npm global packages
export PATH="/home/coder/.local/bin:$PATH"

# Zsh history configuration
export HISTFILE=/commandhistory/.zsh_history
export HISTSIZE=100000
export SAVEHIST=100000

# Allow multiple terminal sessions to append to the history file
setopt APPEND_HISTORY

# Save each command's timestamp in the history file
setopt EXTENDED_HISTORY

# Ignore commands that start with a space character
setopt HIST_IGNORE_SPACE

# Editor configuration
export EDITOR=vim
export VISUAL=vim

# Source fzf key bindings and completion for zsh
if [ -f /usr/share/doc/fzf/examples/key-bindings.zsh ]; then
    source /usr/share/doc/fzf/examples/key-bindings.zsh
fi

if [ -f /usr/share/doc/fzf/examples/completion.zsh ]; then
    source /usr/share/doc/fzf/examples/completion.zsh
fi

# Shift+Enter → Ctrl+J mapping (XTerm sequence)
# Note: This is terminal-dependent and may not work in all terminals
# To test what your terminal sends, run: cat -v (then press Shift+Enter)
bindkey '^[[13;2u' self-insert-unmeta
