package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestDashboardFlagStillAcceptedButHidden(t *testing.T) {
	f := rootCmd.Flags().Lookup("dashboard")
	if f == nil {
		t.Fatal("--dashboard flag removed; it must stay for backwards compatibility")
	}
	if !f.Hidden {
		t.Error("--dashboard should be hidden from help in favour of the dashboard subcommand")
	}
}

func TestDashboardSubcommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"dashboard"})
	if err != nil || cmd != dashboardCmd {
		t.Fatalf("Find(dashboard) = %v, %v; want dashboardCmd", cmd, err)
	}
}

func TestWarnDashboardFlagDeprecated(t *testing.T) {
	var buf bytes.Buffer
	warnDashboardFlagDeprecated(&buf)
	got := buf.String()
	for _, want := range []string{"--dashboard", "deprecated", "ccodolo dashboard"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q missing %q", got, want)
		}
	}
}
