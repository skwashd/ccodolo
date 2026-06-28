package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type quayFetcher struct{}

type quayTagsResponse struct {
	Tags []struct {
		Name         string `json:"name"`
		LastModified string `json:"last_modified"`
	} `json:"tags"`
}

// Fetch retrieves quay.io tags for the given ref (e.g. "terraform-docs/terraform-docs").
func (f *quayFetcher) Fetch(ctx context.Context, ref string) ([]Candidate, error) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("quay ref %q must be namespace/name", ref)
	}
	apiURL := fmt.Sprintf("https://quay.io/api/v1/repository/%s/%s/tag/?onlyActiveTags=true&limit=100", parts[0], parts[1])
	var resp quayTagsResponse
	if err := httpGetJSON(ctx, apiURL, nil, &resp); err != nil {
		return nil, fmt.Errorf("quay %s: %w", ref, err)
	}
	var cands []Candidate
	for _, t := range resp.Tags {
		ver, prefix, ok := ParseVersion(t.Name)
		if !ok {
			continue
		}
		c := Candidate{
			RawTag: t.Name,
			Ver:    ver,
			Prefix: prefix,
		}
		if t.LastModified != "" {
			// quay returns RFC1123 or RFC1123Z
			for _, layout := range []string{time.RFC1123Z, time.RFC1123} {
				if parsed, err := time.Parse(layout, t.LastModified); err == nil {
					c.Published = parsed.UTC()
					break
				}
			}
		}
		cands = append(cands, c)
	}
	return cands, nil
}
