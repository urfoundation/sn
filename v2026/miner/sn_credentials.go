// Financial requests select one retained provider credential. Missing identity
// holds the operation; a network bootstrap token never replaces a provider.
package miner

import (
	"errors"
	"fmt"
	"strings"

	"github.com/docopt/docopt-go"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// An explicit legacy operation may use a network token; provider operations
// use the selected file or the direct provider's original .provider.jwt.
type snCredentialSelection struct {
	original      string
	jwtFile       string
	legacyNetwork bool
}

// Selection does not refresh, register, mint or overwrite an identity.
func snCredentialsFromOpts(opts docopt.Opts) snCredentialSelection {
	path, _ := opts.String("--provider-jwt")
	legacyWallet, _ := opts.Bool("--legacy-network-wallet")
	legacyColdkey, _ := opts.String("--legacy-coldkey")
	return snCredentialSelection{jwtFile: strings.TrimSpace(path), legacyNetwork: legacyWallet || strings.TrimSpace(legacyColdkey) != ""}
}

// The selected original path also names the command's consent journal.
func snCredentialPath(selection snCredentialSelection) (string, error) {
	path := selection.jwtFile
	if path == "" {
		name := ".provider.jwt"
		if selection.legacyNetwork {
			name = "jwt"
		}
		var err error
		path, err = providerStatePath(name)
		if err != nil {
			return "", err
		}
	}
	return path, nil
}

// Parsing only selects local wiring. The API verifies the token signature and
// authorization; this function never turns unverified claims into authority.
func readSnCredentials(selection snCredentialSelection) (string, *sdk.Id, error) {
	path, err := snCredentialPath(selection)
	if err != nil {
		return "", nil, err
	}
	token, err := clientauth.ReadToken(path)
	if err != nil {
		return "", nil, fmt.Errorf("claim/wallet credential not ready at %s: %w", path, err)
	}
	if selection.legacyNetwork {
		claims := gojwt.MapClaims{}
		if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
			return "", nil, fmt.Errorf("legacy network credential: %w", err)
		}
		if value, present := claims["client_id"]; present && value != nil {
			return "", nil, errors.New("legacy network operation requires an explicit network credential")
		}
		return token, nil, nil
	}
	clientId, err := clientauth.ClientIdFromJwt(token)
	if err != nil || clientId == (connect.Id{}) {
		return "", nil, fmt.Errorf("provider credential has no usable client identity: %w", errors.Join(err, errors.New("provider claim/wallet is not ready")))
	}
	if selection.original != "" {
		if err := clientauth.ValidateRefreshedClientJwt(selection.original, token); err != nil {
			return "", nil, err
		}
	}
	id, err := sdk.ParseId(clientId.String())
	return token, id, err
}
