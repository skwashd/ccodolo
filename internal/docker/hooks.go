package docker

import (
	"fmt"
	"os"
	"strings"

	"github.com/skwashd/ccodolo/internal/tool"
)

// hookWarning names a tool whose startup hook will be skipped because one
// or more of its required environment variables won't reach the container.
type hookWarning struct {
	Tool    string
	Missing []string
}

// missingHookVars checks each resolved tool's StartupHookVars against
// available — the set of environment variable names that will actually
// reach the container (config [environment] plus passthrough_vars present
// on the host). A tool with no StartupHookVars always runs and never
// appears here. Order matches resolved.
func missingHookVars(resolved []tool.ResolvedTool, available map[string]bool) []hookWarning {
	var warnings []hookWarning
	for _, rt := range resolved {
		if rt.StartupHook == "" || len(rt.StartupHookVars) == 0 {
			continue
		}
		var missing []string
		for _, name := range rt.StartupHookVars {
			if !available[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			warnings = append(warnings, hookWarning{Tool: rt.Name, Missing: missing})
		}
	}
	return warnings
}

// warnMissingHookVars prints a "Warning: skipping startup hook ..." line to
// stderr for each tool in warnings, matching the passthrough_vars warning
// style in Run.
func warnMissingHookVars(warnings []hookWarning) {
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: skipping startup hook for %q: %s not available in container\n",
			w.Tool, strings.Join(w.Missing, ", "))
	}
}
