package docker

import (
	"runtime"
	"strings"
	"testing"
)

func TestParseRuntime(t *testing.T) {
	tests := []struct {
		name    string
		want    Runtime
		wantErr bool
	}{
		{"", RuntimeDocker, false},
		{"docker", RuntimeDocker, false},
		{"apple", RuntimeApple, false},
		{"podman", "", true},
	}
	for _, tt := range tests {
		got, err := ParseRuntime(tt.name)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseRuntime(%q) expected error", tt.name)
			} else if !strings.Contains(err.Error(), "docker") || !strings.Contains(err.Error(), "apple") {
				t.Errorf("ParseRuntime(%q) error should name valid runtimes, got %q", tt.name, err.Error())
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRuntime(%q) unexpected error: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseRuntime(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRuntimeBinary(t *testing.T) {
	if got := RuntimeDocker.Binary(); got != "docker" {
		t.Errorf("RuntimeDocker.Binary() = %q, want 'docker'", got)
	}
	if got := RuntimeApple.Binary(); got != "container" {
		t.Errorf("RuntimeApple.Binary() = %q, want 'container'", got)
	}
}

func TestCheckHostDockerAlwaysNil(t *testing.T) {
	if err := RuntimeDocker.CheckHost(); err != nil {
		t.Errorf("RuntimeDocker.CheckHost() = %v, want nil", err)
	}
}

func TestCheckHostAppleUnsupportedHost(t *testing.T) {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		t.Skip("host is darwin/arm64; the unsupported-host branch is unreachable")
	}
	err := RuntimeApple.CheckHost()
	if err == nil {
		t.Fatal("RuntimeApple.CheckHost() expected error on non-darwin/arm64 host")
	}
	for _, want := range []string{"macOS 26", "Apple Silicon", runtime.GOOS, `runtime = "apple"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("CheckHost error should mention %q, got %q", want, err.Error())
		}
	}
}
