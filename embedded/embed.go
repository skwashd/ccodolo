package embedded

import "embed"

//go:embed Dockerfile.tmpl
var DockerfileTemplate []byte

//go:embed all:dotfiles
var Dotfiles embed.FS

//go:embed all:scripts
var Scripts embed.FS

// Terminfo holds terminal descriptions compiled into the image with tic.
// The host's TERM is forwarded into the container, and tmux refuses to start
// or attach for a terminal it has no entry for; Debian's ncurses lacks
// Ghostty's xterm-ghostty.
//
//go:embed terminfo
var Terminfo embed.FS
