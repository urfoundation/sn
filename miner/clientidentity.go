package miner

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/connect"
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
