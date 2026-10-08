// validator auth --operator: the operator comes from the operator list, and
// its network JWT goes to <state_dir>/operators/<domain>/jwt, where
// --all-operators reads it. --hotkey_seed_file signs in with the hotkey
// instead of an auth code or password. Without --operator, auth signs in at
// --api_url and writes ~/.urnetwork/jwt as before.
package validator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/hotkeyauth"
	"github.com/urfoundation/sn/v2026/operatorlist"
)

// Where auth signs in and where it writes the network JWT.
type authTarget struct {
	apiUrl  string
	jwtPath string
}

// A domain becomes a path only after the list names it, so an unlisted value
// never selects a directory.
func authTargetFromOpts(ctx context.Context, opts docopt.Opts) (authTarget, error) {
	domain := optString(opts, "--operator", "")
	if domain == "" {
		jwtPath, err := networkJwtPath()
		if err != nil {
			return authTarget{}, err
		}
		return authTarget{apiUrl: optString(opts, "--api_url", DefaultApiUrl), jwtPath: jwtPath}, nil
	}
	stateDir, err := operatorStateDirFromOpts(opts)
	if err != nil {
		return authTarget{}, err
	}
	listUrl := optString(opts, "--operators-url", operatorlist.DefaultUrl)
	snapshot, err := operatorlist.Load(ctx, operatorlist.RefreshSettings{Url: listUrl, CachePath: filepath.Join(stateDir, "operators.yml")})
	if err != nil {
		return authTarget{}, err
	}
	operator, ok := snapshot.List.Operator(domain)
	if !ok {
		return authTarget{}, fmt.Errorf("operator %q is not in the operator list from %s", domain, listUrl)
	}
	return authTarget{apiUrl: operator.ApiUrl, jwtPath: filepath.Join(operatorlist.DomainStateDir(stateDir, operator.Domain), "jwt")}, nil
}

// The measurement runner's --state_dir resolution.
func operatorStateDirFromOpts(opts docopt.Opts) (string, error) {
	stateDir := optString(opts, "--state_dir", "")
	if stateDir == "" {
		var err error
		if stateDir, err = defaultStateDir(); err != nil {
			return "", err
		}
	}
	stateDir, err := filepath.Abs(expandHome(stateDir))
	if err != nil {
		return "", err
	}
	return filepath.Clean(stateDir), nil
}

// The given --hotkey_seed_file, or "" for the shared docopt placeholder.
func authHotkeySeedFile(opts docopt.Opts) string {
	path := optString(opts, "--hotkey_seed_file", "")
	if strings.HasPrefix(path, "<state_dir>") {
		return ""
	}
	return expandHome(path)
}

func hotkeyAuthJwt(ctx context.Context, apiUrl string, seedFile string) (string, error) {
	hotkey, err := loadOperatorHotkey(seedFile)
	if err != nil {
		return "", err
	}
	return hotkeySignInJwt(ctx, apiUrl, hotkey)
}

// The network JWT of the hotkey's network on the operator, created at the
// first sign-in.
func hotkeySignInJwt(ctx context.Context, apiUrl string, hotkey *crv4.Keypair) (string, error) {
	network, err := hotkeyauth.SignIn(ctx, hotkeyauth.Settings{ApiUrl: apiUrl, Hotkey: hotkey})
	if err != nil {
		return "", err
	}
	if network == nil || network.ByJwt == "" {
		return "", errors.New("the operator's hotkey sign-in has no network JWT")
	}
	return network.ByJwt, nil
}
