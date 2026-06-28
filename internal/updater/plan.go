package main

import (
	"time"

	"github.com/skwashd/ccodolo/internal/tool"
)

// Candidate is a single release version from an upstream registry.
type Candidate struct {
	RawTag     string    // raw tag from registry, e.g. "v1.27.0", "2026.06.28", "1.27-slim"
	Ver        Version   // parsed numeric core + suffix (for comparison)
	Prefix     string    // stripped version prefix, e.g. "v", "lychee-v", ""
	Published  time.Time // zero if registry exposes no timestamp
	PreRelease bool      // from GitHub's .prerelease field or npm pre-release detection
}

// Update describes a single tool version bump to be applied.
type Update struct {
	Name       string            `json:"name"`
	Source     tool.UpdateSource `json:"source"`
	Ref        string            `json:"ref"`
	OldTag     string            `json:"old_tag"`
	NewTag     string            `json:"new_tag"`
	Bump       string            `json:"bump"` // "major", "minor", or "patch"
	ReleaseURL string            `json:"release_url"`
	Files      []string          `json:"files"`
}

// PickLatest selects the best candidate to upgrade to from curFullTag.
//
//   - curFullTag is the COMPLETE current version tag used for comparison,
//     i.e. DefaultTag+TagSuffix for docker-hub tools ("21-jdk"),
//     or DefaultTag for all others ("0.22.0-arm64", "0.68.1").
//   - matchSuffix is the suffix that candidates must share (e.g. "-jdk", "-arm64", "").
//   - precision is the number of numeric fields to consider (1=major, 2=major.minor, 3=full).
//   - cutoff: candidates published after cutoff are excluded (24h cooldown).
//   - allowUnverified: if true, candidates with zero Published time are included.
//
// Returns (best candidate, true) or (nil, false) if no upgrade is available.
func PickLatest(curFullTag, matchSuffix string, precision int, cands []Candidate, cutoff time.Time, allowUnverified bool) (*Candidate, bool) {
	curVer, _, ok := ParseVersion(curFullTag)
	if !ok {
		return nil, false
	}

	var best *Candidate
	for i := range cands {
		c := &cands[i]
		if c.PreRelease || !IsStable(c.RawTag) {
			continue
		}
		if !c.Published.IsZero() && !c.Published.Before(cutoff) {
			continue // within 24h cooldown
		}
		if c.Published.IsZero() && !allowUnverified {
			continue // no timestamp available
		}
		if c.Ver.Suffix != matchSuffix {
			continue // different variant (e.g. -noble, -alpine)
		}
		if len(c.Ver.Fields) != precision {
			continue // wrong field count (avoids mixing 1.27 with 1.27.1 for precision=2)
		}
		// Truncate to precision for comparison (shouldn't be needed given precision filter)
		cVer := Version{Fields: c.Ver.Fields[:precision], Suffix: c.Ver.Suffix}
		if best == nil || VersionLess(best.Ver, cVer) {
			best = c
		}
	}

	if best == nil {
		return nil, false
	}

	// Only return if strictly newer.
	// Truncate curVer to same precision for fair comparison.
	curTrunc := Version{Fields: curVer.Fields, Suffix: curVer.Suffix}
	if len(curVer.Fields) > precision {
		curTrunc = Version{Fields: curVer.Fields[:precision], Suffix: curVer.Suffix}
	}
	if !VersionLess(curTrunc, best.Ver) {
		return nil, false
	}

	return best, true
}

// DeriveNewDefaultTag computes the new value to write into DefaultTag in tool.go.
//
//   - For docker-hub with a non-empty tagSuffix: the raw candidate tag
//     already includes the suffix (e.g. "25-jdk"); strip it to get DefaultTag ("25").
//   - For all other sources: strip only the version prefix (e.g. "v") to get
//     the plain version ("1.27.0"). The RawTag preserves original formatting
//     including leading zeros (e.g. "2026.06.28" for yt-dlp).
func DeriveNewDefaultTag(cand *Candidate, tagSuffix string, src tool.UpdateSource) string {
	if src == tool.UpdateDockerHub && tagSuffix != "" {
		result := cand.RawTag
		if len(result) > len(tagSuffix) {
			result = result[:len(result)-len(tagSuffix)]
		}
		return result
	}
	// Strip the parsed prefix (e.g. "v", "lychee-v", "") from the raw tag.
	return cand.RawTag[len(cand.Prefix):]
}
