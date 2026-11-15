package docker

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestParseDockerPS(t *testing.T) {
	got := parseDockerPS([]byte("abc123\trunning\ndef456\texited\n\n"))
	want := map[string]string{"abc123": "running", "def456": "exited"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseDockerPS = %v, want %v", got, want)
	}
	if got := parseDockerPS(nil); len(got) != 0 {
		t.Errorf("parseDockerPS(nil) = %v, want empty", got)
	}
}

func TestParseDockerInspect(t *testing.T) {
	out := []byte(`[
	{
		"Id": "abc123def4567890",
		"Name": "/ccodolo-proj-src-202611011200",
		"Config": {
			"Image": "ccodolo:proj-claude-abcd1234",
			"Labels": {
				"ccodolo.project": "proj",
				"ccodolo.workdir": "/home/dave/src, with comma",
				"ccodolo.agent": "claude",
				"ccodolo.tmux": "1"
			}
		},
		"State": {"Running": true, "StartedAt": "2026-11-01T12:00:00.123456789Z"},
		"Mounts": []
	},
	{
		"Id": "legacy0000001111",
		"Name": "/ccodolo-my-proj-app-202610011200",
		"Config": {"Image": "ccodolo:my-proj-opencode-0123abcd", "Labels": {}},
		"State": {"Running": false, "StartedAt": "0001-01-01T00:00:00Z"},
		"Mounts": [
			{"Source": "/home/dave/.ccodolo/projects/my-proj/common", "Destination": "/home/coder/project"},
			{"Source": "/home/dave/app", "Destination": "/workspace/my-proj/app"}
		]
	},
	{
		"Id": "unrelated0000000",
		"Name": "/postgres",
		"Config": {"Image": "postgres", "Labels": {}},
		"State": {"Running": true, "StartedAt": "2026-11-01T12:00:00Z"},
		"Mounts": []
	}
]`)
	got, err := parseDockerInspect(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Session{
		{
			ID: "abc123def456", Name: "ccodolo-proj-src-202611011200",
			Project: "proj", Workdir: "/home/dave/src, with comma", Agent: "claude",
			Runtime: RuntimeDocker, Running: true, Tmux: true,
			Started: time.Date(2026, 11, 1, 12, 0, 0, 123456789, time.UTC),
		},
		{
			ID: "legacy000000", Name: "ccodolo-my-proj-app-202610011200",
			Project: "my-proj", Workdir: "/home/dave/app", Agent: "opencode",
			Runtime: RuntimeDocker,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseDockerInspect mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestParseAppleSessions(t *testing.T) {
	out := []byte(`[
		{"status": {"state": "running", "networks": [], "startedDate": "2026-11-01T12:23:45Z"}, "configuration": {
			"id": "ccodolo-proj-src-202611011200",
			"labels": {"ccodolo.project": "proj", "ccodolo.workdir": "/Users/dave/src", "ccodolo.agent": "codex", "ccodolo.tmux": "0"},
			"image": {"reference": "ccodolo:proj-codex-abcd1234"},
			"mounts": []
		}},
		{"status": "stopped", "configuration": {
			"id": "ccodolo-old-app-202610011200",
			"image": {"reference": "docker.io/library/ccodolo:old-claude-0123abcd"},
			"mounts": [{"source": "/Users/dave/app", "destination": "/workspace/old/app"}]
		}},
		{"status": "running", "configuration": {"id": "buildkit"}}
	]`)
	got, err := parseAppleSessions(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Session{
		{ID: "ccodolo-proj-src-202611011200", Name: "ccodolo-proj-src-202611011200",
			Project: "proj", Workdir: "/Users/dave/src", Agent: "codex", Runtime: RuntimeApple, Running: true,
			Started: time.Date(2026, 11, 1, 12, 23, 45, 0, time.UTC)},
		{ID: "ccodolo-old-app-202610011200", Name: "ccodolo-old-app-202610011200",
			Project: "old", Workdir: "/Users/dave/app", Agent: "claude", Runtime: RuntimeApple},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseAppleSessions mismatch:\n got: %+v\nwant: %+v", got, want)
	}

	if _, err := parseAppleSessions([]byte("not json")); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

func TestSessionLabelArgs(t *testing.T) {
	got := sessionLabelArgs("p", "/w", "claude", false)
	want := []string{
		"--label", "ccodolo.project=p",
		"--label", "ccodolo.workdir=/w",
		"--label", "ccodolo.agent=claude",
		"--label", "ccodolo.tmux=0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sessionLabelArgs = %q, want %q", got, want)
	}
}

func TestParseAppleSessionsStringStatus(t *testing.T) {
	// Older CLI versions encoded the status as a bare state string.
	got, err := parseAppleSessions([]byte(`[{"status": "running", "configuration": {
		"id": "ccodolo-p-w-1", "labels": {"ccodolo.project": "p"}, "image": {"reference": "x"}}}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || !got[0].Running || !got[0].Started.IsZero() {
		t.Errorf("got %+v, want one running session with no start time", got)
	}
}

func TestCLIError(t *testing.T) {
	err := cliError(&exec.ExitError{Stderr: []byte("Cannot connect to the Docker daemon at unix:///var/run/docker.sock.\n")})
	if got := err.Error(); got != "Cannot connect to the Docker daemon at unix:///var/run/docker.sock." {
		t.Errorf("cliError = %q", got)
	}
	plain := errors.New("boom")
	if got := cliError(plain); got != plain {
		t.Errorf("non-exit errors pass through, got %v", got)
	}
}

func TestUnavailable(t *testing.T) {
	err := unavailable(&exec.ExitError{Stderr: []byte("failed to connect to the docker API at unix:///x.sock\n")})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
	if want := "container engine not running: failed to connect to the docker API at unix:///x.sock"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}
