package tool

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readmeToolTableHeader marks the start of the Dev Tools table in README.md.
// parseREADMEToolTable scans from this line to the next blank line.
const readmeToolTableHeader = "| Tool | Category | Description |"

// parseREADMEToolTable reads ../../README.md and returns the set of tool
// names listed in its "Dev Tools" table. It fails the test if README.md
// cannot be read or if the table header is never found, so a rename of the
// table (or of the file) surfaces as a clear failure rather than a silent
// empty set.
func parseREADMEToolTable(t *testing.T) map[string]bool {
	t.Helper()

	path := filepath.Join("..", "..", "README.md")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	names := make(map[string]bool)
	inTable := false
	sawHeader := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if !inTable {
			if strings.TrimSpace(line) == readmeToolTableHeader {
				inTable = true
				sawHeader = true
			}
			continue
		}

		// A blank line ends the table.
		if strings.TrimSpace(line) == "" {
			break
		}

		// Skip the header separator row, e.g. "|------|----------|-------------|".
		if strings.HasPrefix(strings.TrimSpace(line), "|---") {
			continue
		}

		name, ok := firstBacktickedCell(line)
		if ok {
			names[name] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !sawHeader {
		t.Fatalf("%s: could not find tool table header %q — did the table move or get renamed?", path, readmeToolTableHeader)
	}

	return names
}

// firstBacktickedCell extracts the tool name from a markdown table row whose
// first cell is a backticked identifier, e.g. "| `python` | ... |" -> "python".
func firstBacktickedCell(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return "", false
	}
	start := strings.Index(line, "`")
	if start == -1 {
		return "", false
	}
	end := strings.Index(line[start+1:], "`")
	if end == -1 {
		return "", false
	}
	return line[start+1 : start+1+end], true
}

// TestREADMEMatchesCatalog fails when the Dev Tools table in README.md and
// builtinCatalog disagree about which tools exist. It checks builtinCatalog
// rather than the effective (merged) catalog, so a developer's local
// ~/.ccodolo/custom-tools.json can never affect the result — the same
// precedent AutoUpdatable() follows.
//
// The table intentionally carries no version numbers (they live in
// DefaultTag and are bumped by the updater), so this test compares names
// only.
func TestREADMEMatchesCatalog(t *testing.T) {
	documented := parseREADMEToolTable(t)

	for _, tl := range builtinCatalog {
		if !documented[tl.Name] {
			t.Errorf("tool %q is in the catalog but missing from the README Dev Tools table", tl.Name)
		}
		delete(documented, tl.Name)
	}
	for name := range documented {
		t.Errorf("README Dev Tools table lists %q, which is not in the catalog", name)
	}
}
