//go:build secrets_1password

package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/1password/onepassword-sdk-go"
)

// accountEnvVar is the variable the op CLI reads its account from. A ref
// with no account part falls back to it.
const accountEnvVar = "OP_ACCOUNT"

func init() {
	Register("1password", onePassword{getenv: os.Getenv, connect: connectDesktopApp})
}

// environmentReader is the one SDK call the provider makes.
type environmentReader interface {
	GetVariables(ctx context.Context, environmentID string) (onepassword.GetVariablesResponse, error)
}

// onePassword reads a 1Password Environment through the 1Password desktop
// app. Its fields are the two things a test replaces: the host environment
// and the SDK client.
type onePassword struct {
	getenv  func(string) string
	connect func(ctx context.Context, account string) (environmentReader, error)
}

// Fetch returns the variables of the Environment that ref names. The
// Masked flag is ignored: every variable is injected whether or not the
// app hides its value. An empty value is passed through as empty.
func (p onePassword) Fetch(ctx context.Context, ref string) ([]Var, error) {
	account, envID, err := p.parseRef(ref)
	if err != nil {
		return nil, err
	}
	client, err := p.connect(ctx, account)
	if err != nil {
		return nil, explainError(err)
	}
	resp, err := client.GetVariables(ctx, envID)
	if err != nil {
		return nil, explainError(err)
	}
	vars := make([]Var, len(resp.Variables))
	for i, v := range resp.Variables {
		vars[i] = Var{Name: v.Name, Value: v.Value}
	}
	return vars, nil
}

// parseRef splits "[<account>/]<env-id>" on the last slash: an Environment
// ID has no slash, but an account name might. With no account part it uses
// OP_ACCOUNT.
func (p onePassword) parseRef(ref string) (account, envID string, err error) {
	envID = ref
	hasAccount := false
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		account, envID, hasAccount = ref[:i], ref[i+1:], true
	}
	if envID == "" {
		return "", "", fmt.Errorf("1password ref %q has no Environment ID: use 1password:<account>/<env-id>", ref)
	}
	if hasAccount && account == "" {
		return "", "", fmt.Errorf("1password ref %q has an empty account: use 1password:<account>/<env-id>", ref)
	}
	if !hasAccount {
		account = p.getenv(accountEnvVar)
	}
	if account == "" {
		return "", "", fmt.Errorf(
			"1password ref %q has no account: use 1password:<account>/<env-id>, or set %s and use 1password:<env-id>",
			ref, accountEnvVar)
	}
	return account, envID, nil
}

// desktopClient keeps the SDK client reachable for as long as it is in
// use. The SDK releases a client from a finalizer, and Environments()
// returns a value that does not reference the *Client, so without the
// KeepAlive the client could be released in the middle of a call.
type desktopClient struct {
	client *onepassword.Client
}

func (d desktopClient) GetVariables(ctx context.Context, environmentID string) (onepassword.GetVariablesResponse, error) {
	resp, err := d.client.Environments().GetVariables(ctx, environmentID)
	runtime.KeepAlive(d.client)
	return resp, err
}

// connectDesktopApp authenticates through the 1Password desktop app. The
// app asks the user to approve access (Touch ID, password) when needed.
func connectDesktopApp(ctx context.Context, account string) (environmentReader, error) {
	client, err := onepassword.NewClient(ctx,
		onepassword.WithDesktopAppIntegration(account),
		onepassword.WithIntegrationInfo("ccodolo", Version()),
	)
	if err != nil {
		return nil, err
	}
	return desktopClient{client}, nil
}

// integrateHint is the app setting every desktop-integration failure comes
// back to.
const integrateHint = "in the 1Password app, turn on Settings → Developer → Integrate with other apps"

// errorHints maps text from SDK errors to what to do about it. The SDK is
// 0.x and its error text is not a stable contract, so explainError always
// keeps the original error, and an error that matches nothing here passes
// through unchanged.
var errorHints = []struct{ match, hint string }{
	{"desktop application not found", "the 1Password desktop app was not found; install it, then " + integrateHint},
	{"requires CGO", "this ccodolo binary was built without cgo, which the 1Password desktop integration needs on macOS and Linux; install the ccodolo-1password release build"},
	{"failed to open library", "the 1Password app's SDK library could not be loaded; update the 1Password app, and on Apple Silicon run ccodolo natively rather than under Rosetta"},
	{"failed to load send_message", "the 1Password app's SDK library is not the version this build expects; update the 1Password app"},
	{"failed to load free_message", "the 1Password app's SDK library is not the version this build expects; update the 1Password app"},
	{"connection channel is closed", integrateHint},
	{"unexpectedly dropped by the desktop app", "make sure the 1Password app is running and unlocked; " + integrateHint},
}

// explainError puts a short fix in front of an SDK error it recognises.
func explainError(err error) error {
	var expired *onepassword.DesktopSessionExpiredError
	if errors.As(err, &expired) {
		return fmt.Errorf("the 1Password session expired or the request was not approved; unlock the 1Password app and try again: %w", err)
	}
	var limited *onepassword.RateLimitExceededError
	if errors.As(err, &limited) {
		return fmt.Errorf("1Password is rate limiting requests; wait a little and try again: %w", err)
	}
	msg := err.Error()
	for _, h := range errorHints {
		if strings.Contains(msg, h.match) {
			return fmt.Errorf("%s: %w", h.hint, err)
		}
	}
	return err
}
