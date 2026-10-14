package clientauth

// Shared renewable-client authentication for the subnet miner and validator.
// The network JWT remains a backwards-compatible bootstrap credential; every
// long-lived process runs on a separately persisted client JWT refreshed by
// sdk.Api.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

type JwtRefreshListenerFunc func(string)

func (self JwtRefreshListenerFunc) JwtRefreshed(jwt string) {
	self(jwt)
}

type AuthLogoutListenerFunc func()

func (self AuthLogoutListenerFunc) AuthLogout() {
	self()
}

type ClientRefreshIntegrityListenerFunc func(*sdk.ClientRefreshIntegrityNotice)

func (self ClientRefreshIntegrityListenerFunc) ClientRefreshInvalid(notice *sdk.ClientRefreshIntegrityNotice) {
	self(notice)
}

func ReadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}

// WriteToken atomically persists a bearer token with owner-only permissions.
func WriteToken(path string, token string) (returnErr error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("refusing to persist an empty token")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if returnErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.WriteString(token); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(parent.Sync(), parent.Close())
}

func RemoveToken(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func rejectionPath(clientJwtPath string) string {
	return clientJwtPath + ".rejected"
}

// MarkRejected removes a rejected client credential and records which
// network-login token was present. Automatic restarts may not use that same
// powerful bootstrap credential to recreate a revoked client, nor a renewal of
// it (network_token.go). Running the explicit auth command writes a new
// network JWT, whose different fingerprint permits a deliberate bootstrap on
// the next start.
func MarkRejected(clientJwtPath string, networkJwtPath string) error {
	if err := RemoveToken(clientJwtPath); err != nil {
		return err
	}
	marker := "blocked"
	if _, fingerprint, err := readNetworkCredential(networkJwtPath); err == nil {
		marker = fingerprint
	}
	return WriteToken(rejectionPath(clientJwtPath), marker)
}

func clearRejection(clientJwtPath string) error {
	return RemoveToken(rejectionPath(clientJwtPath))
}

// ClientIdFromJwt extracts the client identity for local wiring only. The
// token is authoritatively verified by the server when it refreshes/connects.
func ClientIdFromJwt(byJwt string) (connect.Id, error) {
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(byJwt, claims); err != nil {
		return connect.Id{}, err
	}
	clientIdString, ok := claims["client_id"].(string)
	if !ok || clientIdString == "" {
		return connect.Id{}, fmt.Errorf("client JWT has no client_id")
	}
	deviceIdString, ok := claims["device_id"].(string)
	if !ok || deviceIdString == "" {
		return connect.Id{}, fmt.Errorf("client JWT has no device_id")
	}
	return connect.ParseId(clientIdString)
}

// LoadOrCreateClientJwt loads a renewable per-process client JWT, or mints
// and persists one using the legacy network JWT when this is the first
// upgraded start. It installs the returned token in api but deliberately does
// not start refresh until the caller has attached all propagation listeners.
func LoadOrCreateClientJwt(
	ctx context.Context,
	api *sdk.Api,
	networkJwtPath string,
	clientJwtPath string,
	description string,
) (string, connect.Id, error) {
	if byClientJwt, err := ReadToken(clientJwtPath); err == nil {
		return refreshStoredClientJwt(ctx, api, networkJwtPath, clientJwtPath, byClientJwt, func(token string) error { return WriteToken(clientJwtPath, token) })
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", connect.Id{}, err
	}

	byJwt, fingerprint, err := readNetworkCredential(networkJwtPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", connect.Id{}, fmt.Errorf("network JWT does not exist at %s; run the auth command first", networkJwtPath)
		}
		return "", connect.Id{}, err
	}
	if marker, markerErr := ReadToken(rejectionPath(clientJwtPath)); markerErr == nil {
		blocks, err := networkCredentialMarkerBlocks(networkJwtPath, fingerprint, marker)
		if err != nil {
			return "", connect.Id{}, err
		}
		if blocks {
			return "", connect.Id{}, fmt.Errorf("the previous client JWT was rejected; run the auth command before restarting")
		}
		if err := clearRejection(clientJwtPath); err != nil {
			return "", connect.Id{}, err
		}
	} else if !errors.Is(markerErr, os.ErrNotExist) {
		return "", connect.Id{}, markerErr
	}
	api.SetByJwt(byJwt)
	result, err := api.AuthNetworkClientSyncWithContext(ctx, &sdk.AuthNetworkClientArgs{
		DeviceDescription: description,
		DeviceSpec:        "",
	})
	if err != nil {
		// the request carried the network token: a 401 rejects the sign-in
		return "", connect.Id{}, networkCredentialRejection(err)
	}
	if result == nil {
		return "", connect.Id{}, errors.New("auth network client returned a null result")
	}
	if result.Error != nil {
		return "", connect.Id{}, fmt.Errorf("%s", result.Error.Message)
	}
	if result.ByClientJwt == "" {
		return "", connect.Id{}, fmt.Errorf("auth network client returned an empty client JWT")
	}
	clientId, err := ClientIdFromJwt(result.ByClientJwt)
	if err != nil {
		return "", connect.Id{}, fmt.Errorf("auth network client returned an invalid client JWT: %w", err)
	}
	if err := WriteToken(clientJwtPath, result.ByClientJwt); err != nil {
		return "", connect.Id{}, err
	}
	if err := clearRejection(clientJwtPath); err != nil {
		return "", connect.Id{}, err
	}
	api.SetByJwt(result.ByClientJwt)
	return result.ByClientJwt, clientId, nil
}

// Existing client ownership never falls through to another creation. A
// successful refresh must preserve the full local client/device identity;
// only an unavailable request retains the already owned credential.
func refreshStoredClientJwt(ctx context.Context, api *sdk.Api, networkPath, clientPath, token string, persist func(string) error) (string, connect.Id, error) {
	return refreshStoredClientJwtWithReadiness(ctx, api, networkPath, clientPath, token, persist, true)
}

// Production observation does not need speculative API readiness. Its caller
// requires successful refresh, including after an unpersisted old revocation.
func refreshStoredClientJwtWithReadiness(ctx context.Context, api *sdk.Api, networkPath, clientPath, token string, persist func(string) error, allowUnavailable bool) (string, connect.Id, error) {
	return refreshStoredClientJwtWithCustody(ctx, api, token, persist, func() error { return clearRejection(clientPath) }, func() error { return MarkRejected(clientPath, networkPath) }, allowUnavailable)
}

// Credential owners supply their real persistence and rejection effects. The
// provider uses descriptor-relative custody; older consumers keep their paths.
func refreshStoredClientJwtWithCustody(ctx context.Context, api *sdk.Api, token string, persist func(string) error, clearRejected, markRejected func() error, allowUnavailable bool) (string, connect.Id, error) {
	clientId, err := ClientIdFromJwt(token)
	if err != nil {
		return "", connect.Id{}, fmt.Errorf("invalid stored client JWT: %w", err)
	}
	api.SetByJwt(token)
	result, refreshErr := api.RefreshJwtSyncWithContext(ctx)
	if refreshErr == nil && result == nil {
		return "", connect.Id{}, errors.New("refresh returned a null client result")
	}
	if refreshErr == nil && result.Error == nil {
		if result.ByJwt == "" {
			return "", connect.Id{}, errors.New("refresh returned an empty client JWT")
		}
		if err := ValidateRefreshedClientJwt(token, result.ByJwt); err != nil {
			return "", connect.Id{}, &RegistrationResponseIdentityError{}
		}
		if err := persist(result.ByJwt); err != nil {
			return "", connect.Id{}, err
		}
		if err := clearRejected(); err != nil {
			return "", connect.Id{}, err
		}
		api.SetByJwt(result.ByJwt)
		return result.ByJwt, clientId, nil
	}
	confirmedRejected := refreshErr == nil && result != nil && result.Error != nil
	if sdk.ConfirmedClientRefreshRejection(refreshErr) {
		confirmedRejected = true
	}
	if confirmedRejected {
		if err := markRejected(); err != nil {
			return "", connect.Id{}, err
		}
		if !allowUnavailable {
			return "", connect.Id{}, &RegistrationRefusedError{Code: "client_revoked"}
		}
		return "", connect.Id{}, errors.New("stored client JWT was rejected; explicit authentication is required")
	}
	if !retryableClientRefreshError(refreshErr) {
		return "", connect.Id{}, refreshErr
	}
	if err := ctx.Err(); err == context.Canceled {
		return "", connect.Id{}, errors.Join(refreshErr, err)
	}
	if !allowUnavailable {
		return "", connect.Id{}, &RegistrationRefreshUnavailableError{cause: refreshErr}
	}
	if err := ctx.Err(); err != nil {
		return "", connect.Id{}, err
	}
	return token, clientId, nil
}

// Every leaf must be a typed unavailable read. Complete decoder/protocol
// failures and a hard cause joined with a timeout retain their error verdict.
func retryableClientRefreshError(err error) bool {
	if err == nil || err == context.Canceled {
		return false
	}
	if err == context.DeadlineExceeded {
		return true
	}
	if status, ok := err.(*connect.HttpStatusError); ok {
		return status.StatusCode == http.StatusRequestTimeout || status.StatusCode == http.StatusTooEarly || status.StatusCode == http.StatusTooManyRequests || status.StatusCode >= 500 && status.StatusCode <= 599
	}
	if _, bad := err.(*sdk.ClientControlResponseError); bad {
		return false
	}
	if _, unavailable := err.(*sdk.ClientControlUnavailableError); unavailable {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !retryableClientRefreshError(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return retryableClientRefreshError(wrapped.Unwrap())
	}
	if network, ok := err.(net.Error); ok && network.Timeout() {
		return true
	}
	return err == syscall.ECONNRESET || err == syscall.ECONNREFUSED || err == syscall.EPIPE || err == syscall.ETIMEDOUT
}
