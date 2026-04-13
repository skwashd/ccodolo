package docker

import (
	"strings"
	"testing"
)

func TestImageTag(t *testing.T) {
	tag := ImageTag("myproject", "claude", "FROM debian:trixie-slim\nRUN echo hello")

	if !strings.HasPrefix(tag, "ccodolo:myproject-claude-") {
		t.Errorf("expected tag to start with 'ccodolo:myproject-claude-', got %q", tag)
	}

	// Hash suffix should be 8 hex chars.
	parts := strings.SplitN(tag, "-", 3)
	if len(parts) != 3 {
		t.Fatalf("expected tag format 'ccodolo:project-agent-hash', got %q", tag)
	}
	hash := parts[2]
	if len(hash) != 8 {
		t.Errorf("expected 8-char hash, got %q (%d chars)", hash, len(hash))
	}
}

func TestImageTagDeterministic(t *testing.T) {
	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	tag1 := ImageTag("proj", "claude", dockerfile)
	tag2 := ImageTag("proj", "claude", dockerfile)
	if tag1 != tag2 {
		t.Errorf("expected deterministic tags, got %q and %q", tag1, tag2)
	}
}

func TestImageTagDifferentInputs(t *testing.T) {
	tag1 := ImageTag("proj", "claude", "FROM debian:trixie-slim\nRUN echo hello")
	tag2 := ImageTag("proj", "claude", "FROM debian:trixie-slim\nRUN echo world")
	if tag1 == tag2 {
		t.Error("different Dockerfiles should produce different tags")
	}
}

func TestImageTagDifferentProjects(t *testing.T) {
	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	tag1 := ImageTag("proj1", "claude", dockerfile)
	tag2 := ImageTag("proj2", "claude", dockerfile)

	// Same hash but different project names.
	if !strings.HasPrefix(tag1, "ccodolo:proj1-claude-") {
		t.Error("tag1 should have project 'proj1'")
	}
	if !strings.HasPrefix(tag2, "ccodolo:proj2-claude-") {
		t.Error("tag2 should have project 'proj2'")
	}
}

func TestImageTagDifferentAgents(t *testing.T) {
	dockerfile := "FROM debian:trixie-slim\nRUN echo hello"
	tag1 := ImageTag("proj", "claude", dockerfile)
	tag2 := ImageTag("proj", "copilot", dockerfile)
	if tag1 == tag2 {
		t.Error("different agents should produce different tags")
	}
	if !strings.HasPrefix(tag1, "ccodolo:proj-claude-") {
		t.Errorf("expected tag1 to start with 'ccodolo:proj-claude-', got %q", tag1)
	}
	if !strings.HasPrefix(tag2, "ccodolo:proj-copilot-") {
		t.Errorf("expected tag2 to start with 'ccodolo:proj-copilot-', got %q", tag2)
	}
}
