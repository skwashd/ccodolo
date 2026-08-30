package embedded

import "embed"

//go:embed Dockerfile.tmpl
var DockerfileTemplate []byte

//go:embed all:dotfiles
var Dotfiles embed.FS

//go:embed all:scripts
var Scripts embed.FS
