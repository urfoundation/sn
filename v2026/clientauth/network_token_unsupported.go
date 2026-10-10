//go:build !linux && !darwin

package clientauth

import (
	"context"
	"errors"
	"time"
)

// Renewal publishes through descriptor-relative custody, which only Unix
// platforms provide here; elsewhere the token is left to its explicit sign-in.
func RenewNetworkToken(string, string, string) (bool, error) {
	return false, errors.New("network token renewal requires Unix custody support")
}

// Without renewal no write races an explicit sign-in, so it needs no owner
// lock here; it still removes any lineage a Unix host left.
func WriteNetworkTokenWithContext(ctx context.Context, path string, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := WriteToken(path, token); err != nil {
		return err
	}
	return RemoveToken(networkCredentialLineagePath(path))
}

// Quarantine renames through descriptor-relative custody too.
func QuarantineNetworkToken(string, string, time.Time) (NetworkTokenQuarantine, bool, error) {
	return NetworkTokenQuarantine{}, false, errors.New("network token quarantine requires Unix custody support")
}
