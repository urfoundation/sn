package miner

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/connect/v2026"
)

// clientIdFileName holds the client id the platform issued this provider.
// It sits beside the network jwt in ~/.urnetwork.
const clientIdFileName = "client_id"

// readStoredClientId returns the client id persisted in dir, or nil when none
// is stored or the stored value is unusable.
//
// A missing or corrupt file is deliberately NOT an error. The caller falls
// back to authenticating a fresh client, which is exactly the old behaviour --
// a provider must still start when its state directory is damaged.
func readStoredClientId(dir string) (*connect.Id, error) {
	b, err := os.ReadFile(filepath.Join(dir, clientIdFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	clientId, parseErr := connect.ParseId(strings.TrimSpace(string(b)))
	if parseErr != nil {
		return nil, nil
	}
	return &clientId, nil
}

// writeStoredClientId persists the client id so the next run reuses this
// identity instead of having the platform mint a new one.
func writeStoredClientId(dir string, clientId connect.Id) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, clientIdFileName), []byte(clientId.String()), 0600)
}

// clearStoredClientId discards the persisted client id so that the next
// authentication asks the platform for a fresh identity.
//
// A missing file is deliberately NOT an error: clearing runs on rejection
// paths where there may be nothing to clear, and it must never be the reason
// a provider fails to start.
func clearStoredClientId(dir string) error {
	if err := os.Remove(filepath.Join(dir, clientIdFileName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// shouldRetryWithNewIdentity reports whether a failed authentication attempt
// should be retried after discarding the stored client identity.
//
// The two error channels mean very different things and must not be collapsed:
//
//   - transportErr is ApiCallbackResult.Error, a transport/callback failure --
//     the network is down, the context was cancelled, the api is unreachable.
//     The stored identity is almost certainly still valid, so discarding it
//     here would mint a brand new client id in response to a transient blip,
//     which is exactly the identity churn the stored id exists to prevent.
//
//   - resultErrMessage is AuthNetworkClientResult.Error.Message, the server
//     rejecting the request at the application level. "Client does not exist."
//     arrives here whenever the stored id names a client the platform no
//     longer knows: a different deployment via --api_url, a re-auth to another
//     network, the device removed in the app, or the idle client reap. Before
//     the id was persisted every one of these simply minted a new client, and
//     falling back restores that.
//
// Only an attempt that actually sent a stored id is retryable -- a fresh auth
// the server rejects has no identity left to discard, and retrying it would
// just repeat the same request.
//
// The message text is deliberately not matched. Any application-level
// rejection of a stored id is grounds to fall back, and the server's wording
// is free to change.
func shouldRetryWithNewIdentity(sentStoredId bool, transportErr error, resultErrMessage string) bool {
	if !sentStoredId {
		return false
	}
	if transportErr != nil {
		return false
	}
	return resultErrMessage != ""
}
