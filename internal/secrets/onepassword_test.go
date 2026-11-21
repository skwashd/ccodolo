//go:build secrets_1password

package secrets

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/1password/onepassword-sdk-go"
)

// fakeReader is the SDK client: it records the Environment asked for and
// returns canned variables or an error.
type fakeReader struct {
	gotEnvID *string
	resp     onepassword.GetVariablesResponse
	err      error
}

func (f fakeReader) GetVariables(_ context.Context, environmentID string) (onepassword.GetVariablesResponse, error) {
	*f.gotEnvID = environmentID
	return f.resp, f.err
}

// envWith returns a getenv that knows only OP_ACCOUNT.
func envWith(opAccount string) func(string) string {
	return func(name string) string {
		if name == accountEnvVar {
			return opAccount
		}
		return ""
	}
}

func TestOnePasswordRegistered(t *testing.T) {
	p, err := Lookup("1password")
	if err != nil {
		t.Fatalf("Lookup(1password) in a secrets_1password build: %v", err)
	}
	if _, ok := p.(onePassword); !ok {
		t.Errorf("Lookup returned %T, want onePassword", p)
	}
}

func TestOnePasswordParseRef(t *testing.T) {
	tests := []struct {
		name        string
		ref         string
		opAccount   string
		wantAccount string
		wantEnvID   string
		wantErr     string
	}{
		{name: "with account", ref: "my.1password.com/abc123", wantAccount: "my.1password.com", wantEnvID: "abc123"},
		{name: "account wins over OP_ACCOUNT", ref: "work/abc123", opAccount: "personal", wantAccount: "work", wantEnvID: "abc123"},
		{name: "OP_ACCOUNT fallback", ref: "abc123", opAccount: "personal", wantAccount: "personal", wantEnvID: "abc123"},
		{name: "account name containing a slash", ref: "Acme/Platform Team/abc123", wantAccount: "Acme/Platform Team", wantEnvID: "abc123"},
		{
			name:    "neither",
			ref:     "abc123",
			wantErr: `1password ref "abc123" has no account: use 1password:<account>/<env-id>, or set OP_ACCOUNT and use 1password:<env-id>`,
		},
		{name: "no Environment ID", ref: "work/", opAccount: "personal", wantErr: `1password ref "work/" has no Environment ID`},
		{name: "empty account", ref: "/abc123", opAccount: "personal", wantErr: `1password ref "/abc123" has an empty account`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := onePassword{getenv: envWith(tt.opAccount)}
			account, envID, err := p.parseRef(tt.ref)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if account != tt.wantAccount || envID != tt.wantEnvID {
				t.Errorf("parseRef = (%q, %q), want (%q, %q)", account, envID, tt.wantAccount, tt.wantEnvID)
			}
		})
	}
}

func TestOnePasswordFetch(t *testing.T) {
	var gotAccount, gotEnvID string
	p := onePassword{
		getenv: envWith(""),
		connect: func(_ context.Context, account string) (environmentReader, error) {
			gotAccount = account
			return fakeReader{
				gotEnvID: &gotEnvID,
				resp: onepassword.GetVariablesResponse{Variables: []onepassword.EnvironmentVariable{
					{Name: "API_KEY", Value: "k", Masked: true},
					{Name: "REGION", Value: "ap-southeast-2", Masked: false},
					{Name: "EMPTY", Value: "", Masked: true},
				}},
			}, nil
		},
	}

	got, err := p.Fetch(context.Background(), "work/abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAccount != "work" || gotEnvID != "abc123" {
		t.Errorf("connected to (%q, %q), want (work, abc123)", gotAccount, gotEnvID)
	}
	// Masked is ignored and an empty value stays empty.
	want := []Var{{Name: "API_KEY", Value: "k"}, {Name: "REGION", Value: "ap-southeast-2"}, {Name: "EMPTY", Value: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Fetch = %+v, want %+v", got, want)
	}
}

func TestOnePasswordFetchBadRefNeverConnects(t *testing.T) {
	p := onePassword{
		getenv: envWith(""),
		connect: func(context.Context, string) (environmentReader, error) {
			t.Error("connect called for a ref with no account")
			return nil, errors.New("unreachable")
		},
	}
	if _, err := p.Fetch(context.Background(), "abc123"); err == nil {
		t.Error("expected an error")
	}
}

func TestOnePasswordFetchExplainsErrors(t *testing.T) {
	notFound := errors.New("1Password desktop application not found")
	var gotEnvID string

	t.Run("connect failure", func(t *testing.T) {
		p := onePassword{
			getenv:  envWith(""),
			connect: func(context.Context, string) (environmentReader, error) { return nil, notFound },
		}
		_, err := p.Fetch(context.Background(), "work/abc123")
		if !errors.Is(err, notFound) || !strings.Contains(err.Error(), "Integrate with other apps") {
			t.Errorf("error = %v, want the hint and the original error", err)
		}
	})

	t.Run("GetVariables failure", func(t *testing.T) {
		dropped := errors.New("connection was unexpectedly dropped by the desktop app. Make sure the desktop app is running")
		p := onePassword{
			getenv: envWith(""),
			connect: func(context.Context, string) (environmentReader, error) {
				return fakeReader{gotEnvID: &gotEnvID, err: dropped}, nil
			},
		}
		_, err := p.Fetch(context.Background(), "work/abc123")
		if !errors.Is(err, dropped) || !strings.Contains(err.Error(), "running and unlocked") {
			t.Errorf("error = %v, want the hint and the original error", err)
		}
	})
}

func TestExplainError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint string // "" means passed through unchanged
	}{
		{"app not found", errors.New("1Password desktop application not found"), "install it, then in the 1Password app, turn on Settings → Developer → Integrate with other apps"},
		{"wrapped by NewClient", fmt.Errorf("error initializing client: %w", errors.New("desktop app connection channel is closed. Make sure Settings > Developer > Integrate with other apps is enabled, or contact 1Password support")), "turn on Settings → Developer → Integrate with other apps"},
		{"no cgo option", errors.New("WithDesktopAppIntegration requires CGO to cross-compile. See README CGO section"), "built without cgo"},
		{"library will not load", errors.New("failed to open library"), "could not be loaded"},
		{"library too old", errors.New("failed to load send_message"), "update the 1Password app"},
		{"connection dropped", errors.New("connection was unexpectedly dropped by the desktop app. Make sure the desktop app is running and Settings > Developer > Integrate with other apps is enabled, or contact 1Password support"), "running and unlocked"},
		{"session expired", fmt.Errorf("error initializing client: %w", &onepassword.DesktopSessionExpiredError{}), "unlock the 1Password app and try again"},
		{"rate limited", &onepassword.RateLimitExceededError{}, "rate limiting"},
		{"unrecognised", errors.New("an internal error occurred. Please contact 1Password support and mention the return code: -9"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := explainError(tt.err)
			if tt.wantHint == "" {
				if got != tt.err {
					t.Errorf("explainError changed an unrecognised error: %v", got)
				}
				return
			}
			if !strings.Contains(got.Error(), tt.wantHint) {
				t.Errorf("error %q missing hint %q", got, tt.wantHint)
			}
			// The SDK's own text is always kept.
			if !errors.Is(got, tt.err) || !strings.HasSuffix(got.Error(), tt.err.Error()) {
				t.Errorf("error %q does not keep the original %q", got, tt.err)
			}
		})
	}
}
