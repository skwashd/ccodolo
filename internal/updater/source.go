package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/skwashd/ccodolo/internal/tool"
)

// Fetcher retrieves candidates for a given upstream ref.
type Fetcher interface {
	Fetch(ctx context.Context, ref string) ([]Candidate, error)
}

// NewFetcher returns a Fetcher for the given UpdateSource.
// githubToken is used for authenticated GitHub API calls (empty = unauthenticated).
func NewFetcher(src tool.UpdateSource, githubToken string) (Fetcher, error) {
	switch src {
	case tool.UpdateDockerHub:
		return &dockerHubFetcher{}, nil
	case tool.UpdateGitHub:
		return &githubFetcher{token: githubToken}, nil
	case tool.UpdateNPM:
		return &npmFetcher{}, nil
	case tool.UpdatePyPI:
		return &pyPIFetcher{}, nil
	case tool.UpdateQuay:
		return &quayFetcher{}, nil
	default:
		return nil, fmt.Errorf("unknown update source %q", src)
	}
}

// releaseURL returns a human-readable URL for the new version release.
func releaseURL(src tool.UpdateSource, ref, rawTag string) string {
	switch src {
	case tool.UpdateDockerHub:
		parts := strings.SplitN(ref, "/", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("https://hub.docker.com/r/%s/%s/tags?name=%s", parts[0], parts[1], rawTag)
		}
		return fmt.Sprintf("https://hub.docker.com/r/%s/tags?name=%s", ref, rawTag)
	case tool.UpdateGitHub:
		return fmt.Sprintf("https://github.com/%s/releases/tag/%s", ref, rawTag)
	case tool.UpdateNPM:
		return fmt.Sprintf("https://www.npmjs.com/package/%s/v/%s", ref, rawTag)
	case tool.UpdatePyPI:
		return fmt.Sprintf("https://pypi.org/project/%s/%s/", ref, rawTag)
	case tool.UpdateQuay:
		parts := strings.SplitN(ref, "/", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("https://quay.io/repository/%s/%s?tab=tags&tag=%s", parts[0], parts[1], rawTag)
		}
		return fmt.Sprintf("https://quay.io/repository/%s?tab=tags&tag=%s", ref, rawTag)
	default:
		return ""
	}
}

// inferPrecision returns how many numeric fields are in the tag.
func inferPrecision(tag string) int {
	v, _, ok := ParseVersion(tag)
	if !ok || len(v.Fields) == 0 {
		return 3 // default to full semver
	}
	return len(v.Fields)
}

// matchSuffixFor returns the Suffix that candidates must share when searching
// for upgrades of the given tool.
// For docker-hub with TagSuffix: candidates include the suffix in their tag.
// For all others (github, npm, pypi, quay): match the suffix baked into CurrentTag/DefaultTag.
func matchSuffixFor(fullTag string) string {
	v, _, ok := ParseVersion(fullTag)
	if !ok {
		return ""
	}
	return v.Suffix
}

// buildCutoff returns now minus 24 hours.
func buildCutoff() time.Time {
	return time.Now().UTC().Add(-24 * time.Hour)
}
