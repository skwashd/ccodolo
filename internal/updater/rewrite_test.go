package main

import (
	"bytes"
	"testing"
)

func TestRewriteToolDefaultTag(t *testing.T) {
	src := []byte(`var catalog = []Tool{
	{
		Name:        "bun",
		Category:    "runtime",
		DefaultTag:  "1.3.12",
	},
	{
		Name:        "deno",
		Category:    "runtime",
		DefaultTag:  "2.7.12",
	},
}`)
	got, err := RewriteToolDefaultTag(src, "bun", "1.3.12", "1.3.14")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) == string(src) {
		t.Error("expected source to be modified")
	}
	// deno unchanged
	got2, err := RewriteToolDefaultTag(got, "deno", "2.7.12", "2.8.0")
	if err != nil {
		t.Fatalf("second rewrite error: %v", err)
	}
	if !contains(got2, `"2.8.0"`) {
		t.Error("expected deno to be updated to 2.8.0")
	}
	if !contains(got2, `"1.3.14"`) {
		t.Error("expected bun to still be 1.3.14")
	}
}

func TestRewriteToolDefaultTagNotFound(t *testing.T) {
	src := []byte(`Name: "bun", DefaultTag: "1.3.12",`)
	_, err := RewriteToolDefaultTag(src, "bun", "9.9.9", "9.9.10")
	if err == nil {
		t.Error("expected error for wrong oldTag")
	}
}

func TestRewriteHelmURL(t *testing.T) {
	src := []byte(`RUN curl -fsSL https://raw.githubusercontent.com/helm/helm/refs/tags/v4.1.4/scripts/get-helm-4 | bash`)
	got, err := RewriteHelmURL(src, "4.1.4", "4.2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(got, "refs/tags/v4.2.0/") {
		t.Errorf("expected new version in URL, got: %s", got)
	}
}

func TestRewriteDockerfileArg(t *testing.T) {
	src := []byte("ARG ZSH_IN_DOCKER_VERSION=1.2.1\n")
	got, err := RewriteDockerfileArg(src, "ZSH_IN_DOCKER_VERSION", "1.2.1", "1.2.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(got, "ARG ZSH_IN_DOCKER_VERSION=1.2.2") {
		t.Errorf("expected new arg version, got: %s", got)
	}
}

func contains(b []byte, s string) bool {
	return bytes.Contains(b, []byte(s))
}
