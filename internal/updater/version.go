package main

import (
	"strconv"
	"strings"
)

// Version represents the numeric core of a release tag plus any trailing suffix.
// e.g. "1.26" → {[1,26],""}, "21-jdk" → {[21],"-jdk"}, "0.22.0-arm64" → {[0,22,0],"-arm64"},
//
//	"2026.03.17" → {[2026,3,17],""}, "lychee-v0.24.2" → stripped by knownPrefixes → {[0,24,2],""}
type Version struct {
	Fields []int
	Suffix string
}

// knownPrefixes are stripped in order when parsing release tags.
var knownPrefixes = []string{"lychee-v", "v"}

// ParseVersion strips known prefixes, reads the leading numeric dot-separated
// core, and records any trailing non-numeric remainder as Suffix.
// Returns (Version, strippedPrefix, true) or (zero, "", false) on failure.
func ParseVersion(tag string) (Version, string, bool) {
	stripped := ""
	rest := tag
	for _, p := range knownPrefixes {
		if strings.HasPrefix(tag, p) {
			stripped = p
			rest = tag[len(p):]
			break
		}
	}
	// Find the boundary between the numeric core and any suffix.
	// e.g. "0.22.0-arm64" → core="0.22.0", suffix="-arm64"
	// e.g. "3.13" → core="3.13", suffix=""
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= '0' && c <= '9') || c == '.' {
			end++
		} else {
			break
		}
	}
	// end may end with '.', back it off
	for end > 0 && rest[end-1] == '.' {
		end--
	}
	if end == 0 {
		return Version{}, "", false
	}
	coreStr := rest[:end]
	suffix := rest[end:]
	parts := strings.Split(coreStr, ".")
	fields := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return Version{}, "", false
		}
		fields = append(fields, n)
	}
	return Version{Fields: fields, Suffix: suffix}, stripped, true
}

// IsStable returns false if the tag contains any pre-release marker.
func IsStable(tag string) bool {
	lower := strings.ToLower(tag)
	for _, marker := range []string{"rc", "alpha", "beta", "-pre", "preview", "nightly", "dev", "canary", "snapshot"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

// VersionLess returns true if a < b by tuple comparison.
func VersionLess(a, b Version) bool {
	minLen := len(a.Fields)
	if len(b.Fields) < minLen {
		minLen = len(b.Fields)
	}
	for i := 0; i < minLen; i++ {
		if a.Fields[i] != b.Fields[i] {
			return a.Fields[i] < b.Fields[i]
		}
	}
	return len(a.Fields) < len(b.Fields)
}

// ClassifyBump returns "major", "minor", or "patch" based on which field changed.
func ClassifyBump(oldVer, newVer Version) string {
	minLen := len(oldVer.Fields)
	if len(newVer.Fields) < minLen {
		minLen = len(newVer.Fields)
	}
	for i := 0; i < minLen; i++ {
		if oldVer.Fields[i] != newVer.Fields[i] {
			switch i {
			case 0:
				return "major"
			case 1:
				return "minor"
			default:
				return "patch"
			}
		}
	}
	return "patch"
}
