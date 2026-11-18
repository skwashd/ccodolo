package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/skwashd/ccodolo/internal/dashboard"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Show running sessions in a live dashboard",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return dashboard.Run()
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}

// warnDashboardFlagDeprecated tells users of the old --dashboard flag to
// switch to the subcommand. It goes to stderr so scripts reading stdout are
// unaffected.
func warnDashboardFlagDeprecated(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Warning: --dashboard is deprecated and will be removed in a future release; use `ccodolo dashboard` instead.")
}
