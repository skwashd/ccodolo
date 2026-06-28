package main

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type npmFetcher struct{}

type npmMeta struct {
	Time     map[string]string   `json:"time"`
	Versions map[string]struct{} `json:"versions"`
}

// Fetch retrieves npm package versions for the given package ref (e.g. "pnpm" or "@playwright/cli").
func (f *npmFetcher) Fetch(ctx context.Context, ref string) ([]Candidate, error) {
	encoded := url.PathEscape(ref)
	apiURL := "https://registry.npmjs.org/" + encoded
	var meta npmMeta
	if err := httpGetJSON(ctx, apiURL, nil, &meta); err != nil {
		return nil, fmt.Errorf("npm %s: %w", ref, err)
	}
	var cands []Candidate
	for ver, timeStr := range meta.Time {
		// Skip metadata keys
		if ver == "created" || ver == "modified" {
			continue
		}
		v, prefix, ok := ParseVersion(ver)
		if !ok {
			continue
		}
		c := Candidate{
			RawTag: ver,
			Ver:    v,
			Prefix: prefix,
			// npm uses pre-release semver identifiers (e.g. "1.0.0-beta")
			PreRelease: !IsStable(ver),
		}
		if timeStr != "" {
			// npm time strings may have fractional seconds or not
			for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.999Z07:00"} {
				if t, err := time.Parse(layout, timeStr); err == nil {
					c.Published = t.UTC()
					break
				}
			}
		}
		cands = append(cands, c)
	}
	return cands, nil
}
