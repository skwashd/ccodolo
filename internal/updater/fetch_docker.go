package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type dockerHubFetcher struct{}

type dhTagsResponse struct {
	Count   int     `json:"count"`
	Next    *string `json:"next"`
	Results []struct {
		Name       string  `json:"name"`
		LastPushed *string `json:"tag_last_pushed"`
	} `json:"results"`
}

// Fetch retrieves tags from Docker Hub for the given ref (e.g. "library/golang" or "oven/bun").
// Pages up to maxPages to avoid unbounded requests on large repos.
func (f *dockerHubFetcher) Fetch(ctx context.Context, ref string) ([]Candidate, error) {
	const maxPages = 5
	var all []Candidate
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", ref)
	for page := 0; page < maxPages && url != ""; page++ {
		var resp dhTagsResponse
		if err := httpGetJSON(ctx, url, nil, &resp); err != nil {
			return nil, fmt.Errorf("docker hub %s: %w", ref, err)
		}
		for _, r := range resp.Results {
			ver, prefix, ok := ParseVersion(r.Name)
			if !ok {
				continue
			}
			c := Candidate{
				RawTag: r.Name,
				Ver:    ver,
				Prefix: prefix,
			}
			if r.LastPushed != nil {
				t, err := time.Parse(time.RFC3339, *r.LastPushed)
				if err == nil {
					c.Published = t.UTC()
				}
			}
			all = append(all, c)
		}
		if resp.Next != nil && *resp.Next != "" {
			url = *resp.Next
		} else {
			url = ""
		}
	}
	return all, nil
}

// httpGetJSON performs a GET request and decodes the JSON response into v.
func httpGetJSON(ctx context.Context, url string, headers map[string]string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		short := body
		if len(short) > 200 {
			short = short[:200]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, short)
	}
	return json.Unmarshal(body, v)
}
