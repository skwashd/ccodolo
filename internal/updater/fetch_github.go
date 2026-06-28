package main

import (
	"context"
	"fmt"
	"time"
)

type githubFetcher struct {
	token string
}

type ghRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	PreRelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
}

// Fetch retrieves GitHub releases for the given repo ref (e.g. "cli/cli").
// Pages up to 3 pages (300 releases) to find the latest stable release.
func (f *githubFetcher) Fetch(ctx context.Context, ref string) ([]Candidate, error) {
	const maxPages = 3
	var all []Candidate
	headers := map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	if f.token != "" {
		headers["Authorization"] = "Bearer " + f.token
	}
	for page := 1; page <= maxPages; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", ref, page)
		var releases []ghRelease
		if err := httpGetJSON(ctx, url, headers, &releases); err != nil {
			return nil, fmt.Errorf("github %s: %w", ref, err)
		}
		if len(releases) == 0 {
			break
		}
		for _, r := range releases {
			if r.Draft {
				continue
			}
			ver, prefix, ok := ParseVersion(r.TagName)
			if !ok {
				continue
			}
			c := Candidate{
				RawTag:     r.TagName,
				Ver:        ver,
				Prefix:     prefix,
				PreRelease: r.PreRelease,
			}
			if r.PublishedAt != "" {
				t, err := time.Parse(time.RFC3339, r.PublishedAt)
				if err == nil {
					c.Published = t.UTC()
				}
			}
			all = append(all, c)
		}
	}
	return all, nil
}
