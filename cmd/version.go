package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/skwashd/ccodolo/internal/secrets"
)

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// SetVersionInfo sets the build version information from main.go ldflags.
func SetVersionInfo(version, commit, date string) {
	buildVersion = version
	buildCommit = commit
	buildDate = date
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		printVersion(cmd.OutOrStdout())
	},
}

// printVersion writes the version line, then which secrets providers this
// build has: the default build has none, and each provider ships as its own
// build (see internal/secrets).
func printVersion(w io.Writer) {
	_, _ = fmt.Fprintf(w, "ccodolo %s (commit: %s, built: %s)\n", buildVersion, buildCommit, buildDate)
	providers := "none"
	if compiled := secrets.Compiled(); len(compiled) > 0 {
		providers = strings.Join(compiled, ", ")
	}
	_, _ = fmt.Fprintf(w, "secrets: %s\n", providers)
}
