package main

import (
	"context"
	"fmt"
	"time"
)

type pyPIFetcher struct{}

type pyPIProject struct {
	Releases map[string][]struct {
		UploadTime string `json:"upload_time_iso_8601"`
	} `json:"releases"`
}

// Fetch retrieves PyPI release versions for the given project name.
func (f *pyPIFetcher) Fetch(ctx context.Context, ref string) ([]Candidate, error) {
	apiURL := fmt.Sprintf("https://pypi.org/pypi/%s/json", ref)
	var proj pyPIProject
	if err := httpGetJSON(ctx, apiURL, nil, &proj); err != nil {
		return nil, fmt.Errorf("pypi %s: %w", ref, err)
	}
	var cands []Candidate
	for ver, files := range proj.Releases {
		v, prefix, ok := ParseVersion(ver)
		if !ok {
			continue
		}
		c := Candidate{
			RawTag:     ver,
			Ver:        v,
			Prefix:     prefix,
			PreRelease: !IsStable(ver),
		}
		// Use the latest upload time across all files in the release.
		for _, fi := range files {
			if fi.UploadTime == "" {
				continue
			}
			t, err := time.Parse("2006-01-02T15:04:05.999999Z", fi.UploadTime)
			if err != nil {
				t, err = time.Parse(time.RFC3339, fi.UploadTime)
			}
			if err == nil && t.After(c.Published) {
				c.Published = t.UTC()
			}
		}
		cands = append(cands, c)
	}
	return cands, nil
}
