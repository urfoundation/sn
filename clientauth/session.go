package clientauth

// Shared renewable-client authentication for the subnet miner and validator.
// The network JWT remains a backwards-compatible bootstrap credential; every
// long-lived process runs on a separately persisted client JWT refreshed by
// sdk.Api.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

type JwtRefreshListenerFunc func(string)

func (self JwtRefreshListenerFunc) JwtRefreshed(jwt string) {
	self(jwt)
}

type AuthLogoutListenerFunc func()

func (self AuthLogoutListenerFunc) AuthLogout() {
	self()
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
	return nil
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

func networkCredentialFingerprint(path string, byJwt string) string {
	generation := strings.TrimSpace(byJwt)
	if info, err := os.Stat(path); err == nil {
		generation += fmt.Sprintf("\n%d", info.ModTime().UnixNano())
	}
	sum := sha256.Sum256([]byte(generation))
	return hex.EncodeToString(sum[:])
}

// MarkRejected removes a rejected client credential and records which
// network-login token was present. Automatic restarts may not use that same
// powerful bootstrap credential to recreate a revoked client. Running the
// explicit auth command writes a new network JWT, whose different fingerprint
// permits a deliberate bootstrap on the next start.
func MarkRejected(clientJwtPath string, networkJwtPath string) error {
	if err := RemoveToken(clientJwtPath); err != nil {
		return err
	}
	marker := "blocked"
	if networkJwt, err := ReadToken(networkJwtPath); err == nil {
		marker = networkCredentialFingerprint(networkJwtPath, networkJwt)
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
		clientId, parseErr := ClientIdFromJwt(byClientJwt)
		if parseErr != nil {
			return "", connect.Id{}, fmt.Errorf("invalid stored client JWT %s: %w", clientJwtPath, parseErr)
		}
		api.SetByJwt(byClientJwt)
		if err := clearRejection(clientJwtPath); err != nil {
			return "", connect.Id{}, err
		}

		// Validate and rotate at restart before any connect transport is
		// created. A confirmed rejection falls through to the one-time network
		// JWT bootstrap; a transient outage retains the still-usable client JWT
		// and lets Api's background worker retry.
		refreshResult, refreshErr := api.RefreshJwtSync()
		if refreshErr == nil && refreshResult != nil && refreshResult.Error == nil && refreshResult.ByJwt != "" {
			refreshedClientId, parseErr := ClientIdFromJwt(refreshResult.ByJwt)
			if parseErr != nil {
				return "", connect.Id{}, fmt.Errorf("refresh returned an invalid client JWT: %w", parseErr)
			}
			if err := WriteToken(clientJwtPath, refreshResult.ByJwt); err != nil {
				return "", connect.Id{}, err
			}
			api.SetByJwt(refreshResult.ByJwt)
			return refreshResult.ByJwt, refreshedClientId, nil
		}

		confirmedRejected := refreshErr == nil && refreshResult != nil && refreshResult.Error != nil
		var statusErr *connect.HttpStatusError
		if errors.As(refreshErr, &statusErr) && statusErr.StatusCode == http.StatusUnauthorized {
			confirmedRejected = true
		}
		if !confirmedRejected {
			return byClientJwt, clientId, nil
		}
		if err := MarkRejected(clientJwtPath, networkJwtPath); err != nil {
			return "", connect.Id{}, err
		}
		return "", connect.Id{}, fmt.Errorf("stored client JWT was rejected; run the auth command before restarting")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", connect.Id{}, err
	}

	byJwt, err := ReadToken(networkJwtPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", connect.Id{}, fmt.Errorf("network JWT does not exist at %s; run the auth command first", networkJwtPath)
		}
		return "", connect.Id{}, err
	}
	if marker, markerErr := ReadToken(rejectionPath(clientJwtPath)); markerErr == nil {
		if marker == "blocked" || marker == networkCredentialFingerprint(networkJwtPath, byJwt) {
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
		return "", connect.Id{}, err
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
