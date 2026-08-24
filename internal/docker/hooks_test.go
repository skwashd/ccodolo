package docker

import (
	"reflect"
	"testing"

	"github.com/skwashd/ccodolo/internal/tool"
)

func TestMissingHookVars(t *testing.T) {
	hookTool := func(name string, vars ...string) tool.ResolvedTool {
		return tool.ResolvedTool{
			Tool: tool.Tool{
				Name:            name,
				StartupHook:     "run-it",
				StartupHookVars: vars,
			},
		}
	}
	noHookTool := func(name string) tool.ResolvedTool {
		return tool.ResolvedTool{Tool: tool.Tool{Name: name}}
	}

	tests := []struct {
		name      string
		resolved  []tool.ResolvedTool
		available map[string]bool
		want      []hookWarning
	}{
		{
			name:      "all vars present: no warning",
			resolved:  []tool.ResolvedTool{hookTool("acli", "JIRA_TOKEN", "JIRA_SITE")},
			available: map[string]bool{"JIRA_TOKEN": true, "JIRA_SITE": true},
			want:      nil,
		},
		{
			name:      "some vars missing: warns with only the missing ones",
			resolved:  []tool.ResolvedTool{hookTool("acli", "JIRA_TOKEN", "JIRA_SITE", "JIRA_USER")},
			available: map[string]bool{"JIRA_SITE": true},
			want:      []hookWarning{{Tool: "acli", Missing: []string{"JIRA_TOKEN", "JIRA_USER"}}},
		},
		{
			name:      "no vars present: warns with all of them",
			resolved:  []tool.ResolvedTool{hookTool("acli", "JIRA_TOKEN", "JIRA_SITE")},
			available: map[string]bool{},
			want:      []hookWarning{{Tool: "acli", Missing: []string{"JIRA_TOKEN", "JIRA_SITE"}}},
		},
		{
			name:      "no StartupHookVars declared: never warns, regardless of available",
			resolved:  []tool.ResolvedTool{hookTool("unconditional")},
			available: map[string]bool{},
			want:      nil,
		},
		{
			name:      "tool with no hook at all: ignored",
			resolved:  []tool.ResolvedTool{noHookTool("python")},
			available: map[string]bool{},
			want:      nil,
		},
		{
			name: "mixed tools: only the hook-bearing, under-supplied one warns",
			resolved: []tool.ResolvedTool{
				noHookTool("python"),
				hookTool("acli", "JIRA_TOKEN"),
				hookTool("unconditional"),
			},
			available: map[string]bool{},
			want:      []hookWarning{{Tool: "acli", Missing: []string{"JIRA_TOKEN"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingHookVars(tt.resolved, tt.available)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("missingHookVars() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
