// A provider's durable client request binds its actual role, endpoint, key and
// proxy slot. No chain, validator or native-registration authority is inferred.
package miner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
)

// Fault observers run only after a real attempt or complete authenticated
// handoff. They cannot replace disk custody, an API result or identity checks.
type providerRegistrationHooks struct {
	afterAttempt       func(error) error
	afterAuthenticated func(string, connect.Id, []byte) error
	afterApiJoined     func()
}

// Hooks belong to one explicitly supplied run context, never global state.
type providerRegistrationHooksKey struct{}

// Exact direct/proxy identity excludes credentials and map iteration order.
func providerRegistrationSlot(settings *connect.ProxySettings) string {
	if settings == nil {
		return "direct"
	}
	sum := sha256.Sum256([]byte(settings.Network + "\x00" + settings.Address))
	return "proxy-sha256:" + hex.EncodeToString(sum[:])
}

// Full slot and legacy credential-path uniqueness both precede all workers.
// A truncated filename collision is a refusal, never a shared client identity.
func validateProviderRegistrationSlots(providers []*connect.ProxySettings) error {
	if len(providers) > 4096 {
		return errors.New("provider registration exceeds its 4096 member bound")
	}
	slotKVs, pathKVs := map[string]bool{}, map[string]bool{}
	for _, settings := range providers {
		slot := providerRegistrationSlot(settings)
		path, err := providerClientJwtPath(settings)
		if err != nil {
			return err
		}
		if slotKVs[slot] || pathKVs[path] {
			return errors.New("provider registration slots or credential paths overlap")
		}
		slotKVs[slot], pathKVs[path] = true, true
	}
	return nil
}

// Only the versioned route's typed availability leaves may replay. Completed
// refusals, malformed replies and local custody faults cannot become retries.
func providerRegistrationRetryable(err error, depth int) bool {
	if err == nil || depth > 32 {
		return false
	}
	switch err.(type) {
	case *sdk.NetworkClientRegistrationUnavailableError, *clientauth.RegistrationRefreshUnavailableError:
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > 32 {
			return false
		}
		for _, cause := range causes {
			if !providerRegistrationRetryable(cause, depth+1) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return providerRegistrationRetryable(wrapped.Unwrap(), depth+1)
	}
	return false
}

// Each attempt is finite. Later attempts replay the same durably retained
// operation and never fall back to the legacy allocating endpoint.
func authenticateProvider(ctx context.Context, api *sdk.Api, networkPath, clientPath string, keyOwner *clientauth.ProviderClientKeyOwner, slot string, allowCreate bool, output *providerDiagnostics, index uint64) (string, connect.Id, error) {
	hooks, _ := ctx.Value(providerRegistrationHooksKey{}).(providerRegistrationHooks)
	for {
		attempt, cancel := context.WithTimeout(ctx, 300*time.Second)
		token, id, err := keyOwner.LoadOrRegisterClientJwt(attempt, api, networkPath, clientPath, slot, allowCreate)
		cancel()
		if hooks.afterAttempt != nil {
			if hookErr := hooks.afterAttempt(err); hookErr != nil {
				return "", connect.Id{}, errors.Join(err, hookErr)
			}
		}
		if err == nil {
			if hooks.afterAuthenticated != nil {
				if err := hooks.afterAuthenticated(token, id, keyOwner.Seed()); err != nil {
					return "", connect.Id{}, err
				}
			}
			return token, id, nil
		}
		if ctx.Err() != nil {
			return "", connect.Id{}, errors.Join(err, ctx.Err())
		}
		if !providerRegistrationRetryable(err, 0) {
			return "", connect.Id{}, fmt.Errorf("provider client identity requires recovery: %w", err)
		}
		output.observe(providerAuthenticationWait, index, true, err, time.Second, nil)
		select {
		case <-ctx.Done():
			return "", connect.Id{}, errors.Join(err, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// Refresh identity is checked before the existing persistence callback. A
// malformed completed refresh can cancel only its captured current API owner.
type providerBoundRefresh struct {
	original  string
	callbacks *providerAuthenticationCallbacks
}

// Stable principal, roles and client/device identity survive token renewal.
func (self *providerBoundRefresh) JwtRefreshed(token string) {
	if err := clientauth.ValidateRefreshedClientJwt(self.original, token); err != nil {
		self.callbacks.retainFailure(err)
		self.callbacks.cancel()
		self.callbacks.diagnostics.observe(providerAuthenticationRejected, self.callbacks.provider, true, err, 0, nil)
		return
	}
	self.callbacks.JwtRefreshed(token)
}

// The SDK generation check occurs at the cancellation boundary, including an
// equal-byte replacement login. No new credential or revocation is invented.
func (self *providerBoundRefresh) ClientRefreshInvalid(notice *sdk.ClientRefreshIntegrityNotice) {
	if notice.CloseApiIfCurrent() {
		err := errors.New("provider client refresh requires identity recovery")
		self.callbacks.retainFailure(err)
		self.callbacks.cancel()
		self.callbacks.diagnostics.observe(providerAuthenticationRejected, self.callbacks.provider, true, err, 0, nil)
	}
}
