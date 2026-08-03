package miner

// network.go — persisted custom-network selection for the miner CLI.
// `provider choose_network <api_url> <connect_url>` writes the chosen
// network to network.json in the provider state directory (alongside
// jwt, client_id and .provider.key — see providerStateDir);
// `provider choose_network --reset` removes it. resolveApiUrl and
// resolveConnectUrl apply the flag > saved-config > default precedence
// on top of this file.
//
// The read/write/reset functions take the state directory explicitly
// rather than resolving the home directory themselves, matching
// clientidentity.go. That keeps the tests off the developer's real
// ~/.urnetwork: overriding $HOME does not work on Windows, where
// os.UserHomeDir reads %USERPROFILE%, so a test that reset the config
// would delete the config of whoever ran it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"
)

// networkConfigFileName is the saved network, in the provider state
// directory beside jwt and client_id.
const networkConfigFileName = "network.json"

// networkConfig is the on-disk shape of network.json.
type networkConfig struct {
	ApiUrl     string `json:"api_url"`
	ConnectUrl string `json:"connect_url"`
}

// validateApiUrl requires an http or https URL.
func validateApiUrl(rawUrl string) error {
	if rawUrl == "" {
		return errors.New("invalid api_url: empty")
	}
	u, err := url.Parse(rawUrl)
	if err != nil {
		return fmt.Errorf("invalid api_url %q: %w", rawUrl, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid api_url %q: scheme must be http or https, got %q", rawUrl, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid api_url %q: missing host", rawUrl)
	}
	return nil
}

// validateConnectUrl requires a ws or wss URL.
func validateConnectUrl(rawUrl string) error {
	if rawUrl == "" {
		return errors.New("invalid connect_url: empty")
	}
	u, err := url.Parse(rawUrl)
	if err != nil {
		return fmt.Errorf("invalid connect_url %q: %w", rawUrl, err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("invalid connect_url %q: scheme must be ws or wss, got %q", rawUrl, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid connect_url %q: missing host", rawUrl)
	}
	return nil
}

// urlScheme returns the scheme of rawUrl, lowercased by url.Parse, or ""
// when it does not parse.
func urlScheme(rawUrl string) string {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return ""
	}
	return u.Scheme
}

// insecureNetworkWarning returns a warning when the chosen network would
// carry the provider's credentials in the clear, or "" when both URLs
// are encrypted.
//
// The network jwt travels as an `Authorization: Bearer` header on every
// api call, so a plaintext api_url puts it on the wire in the clear for
// anyone on the path. The one-off --api_url flag has always allowed
// this, but a saved network makes it persistent and invisible — which is
// why it is worth saying out loud at the point it gets written down.
func insecureNetworkWarning(apiUrl, connectUrl string) string {
	insecure := []string{}
	if urlScheme(apiUrl) == "http" {
		insecure = append(insecure, "api_url uses http")
	}
	if urlScheme(connectUrl) == "ws" {
		insecure = append(insecure, "connect_url uses ws")
	}
	if len(insecure) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"warning: %s. Traffic to this network is unencrypted, including the provider's auth token, "+
			"and is readable by anyone on the network path. Use https:// and wss:// unless this is a "+
			"local test deployment.",
		strings.Join(insecure, " and "),
	)
}

// networkSwitchNotice warns that the credentials already in dir belong to
// the previously selected network, or returns "" when there are none.
//
// The jwt is minted by one network's api and means nothing to another, so
// after a switch `provider provide` fails with an auth error whose cause
// is several steps removed from the message. The stored client id goes
// stale the same way, but that one recovers on its own (provideAuth
// discards a rejected id and asks for a new one), so only the jwt needs
// the operator to act.
func networkSwitchNotice(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "jwt")); err != nil {
		return ""
	}
	return fmt.Sprintf(
		"note: the jwt in %s was issued by the previously selected network. "+
			"Run `provider auth` against the new network before `provider provide`.",
		dir,
	)
}

// readNetworkConfig loads the saved network config from dir. ok is false
// (with a nil error) when the file does not exist — a fresh install with
// no custom network saved.
//
// A file that parses but does not describe a complete, usable network is
// an error rather than a config: returning it would hand an empty base
// URL to the api client and fail much further from the cause. That covers
// `{}`, `null`, and a half-written or hand-edited file with one field.
func readNetworkConfig(dir string) (cfg networkConfig, ok bool, err error) {
	p := filepath.Join(dir, networkConfigFileName)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return networkConfig{}, false, nil
	}
	if err != nil {
		return networkConfig{}, false, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return networkConfig{}, false, fmt.Errorf("parse %s: %w", p, err)
	}
	if err := validateApiUrl(cfg.ApiUrl); err != nil {
		return networkConfig{}, false, fmt.Errorf("%s: %w", p, err)
	}
	if err := validateConnectUrl(cfg.ConnectUrl); err != nil {
		return networkConfig{}, false, fmt.Errorf("%s: %w", p, err)
	}
	return cfg, true, nil
}

// writeNetworkConfig validates apiUrl (http/https) and connectUrl
// (ws/wss), then writes them to network.json in dir, creating dir if
// needed. Nothing is written if validation fails.
func writeNetworkConfig(dir, apiUrl, connectUrl string) error {
	if err := validateApiUrl(apiUrl); err != nil {
		return err
	}
	if err := validateConnectUrl(connectUrl); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(networkConfig{ApiUrl: apiUrl, ConnectUrl: connectUrl}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, networkConfigFileName), append(b, '\n'))
}

// writeFileAtomic writes b to p through a temp file in the same directory
// and a rename, so a crash or a full disk cannot leave a half-written
// file behind. That matters here because every miner command now reads
// network.json: a truncated one would fail them all on the parse error,
// which under the provider's Restart=always unit is a permanent stop
// rather than a bad run.
//
// The temp file is created 0600 by os.CreateTemp, which is the mode this
// file wants — it names the endpoint the provider's credentials are sent
// to.
func writeFileAtomic(p string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// no-op once the rename below succeeds; cleans up every path that
	// does not get that far
	defer os.Remove(tmp)

	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// resetNetworkConfig removes network.json from dir. Removing a
// nonexistent file is not an error.
func resetNetworkConfig(dir string) error {
	err := os.Remove(filepath.Join(dir, networkConfigFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// resolveApiUrlIn implements the 3-tier precedence for the API URL
// against an explicit state directory:
// --api_url flag > saved network config > DefaultApiUrl.
func resolveApiUrlIn(opts docopt.Opts, dir string) (string, error) {
	if apiUrl, err := opts.String("--api_url"); err == nil {
		return apiUrl, nil
	}
	cfg, ok, err := readNetworkConfig(dir)
	if err != nil {
		return "", err
	}
	if ok {
		return cfg.ApiUrl, nil
	}
	return DefaultApiUrl, nil
}

// resolveConnectUrlIn implements the 3-tier precedence for the connect
// URL against an explicit state directory:
// --connect_url flag > saved network config > DefaultConnectUrl.
func resolveConnectUrlIn(opts docopt.Opts, dir string) (string, error) {
	if connectUrl, err := opts.String("--connect_url"); err == nil {
		return connectUrl, nil
	}
	cfg, ok, err := readNetworkConfig(dir)
	if err != nil {
		return "", err
	}
	if ok {
		return cfg.ConnectUrl, nil
	}
	return DefaultConnectUrl, nil
}

// resolveApiUrl resolves the API URL against the real provider state
// directory. The flag is checked before the home lookup so an explicit
// --api_url still works where the home directory cannot be resolved,
// which is what the flag did before a saved config existed.
func resolveApiUrl(opts docopt.Opts) (string, error) {
	if apiUrl, err := opts.String("--api_url"); err == nil {
		return apiUrl, nil
	}
	dir, err := providerStateDir()
	if err != nil {
		return "", err
	}
	return resolveApiUrlIn(opts, dir)
}

// resolveConnectUrl resolves the connect URL against the real provider
// state directory. See resolveApiUrl for the flag short-circuit.
func resolveConnectUrl(opts docopt.Opts) (string, error) {
	if connectUrl, err := opts.String("--connect_url"); err == nil {
		return connectUrl, nil
	}
	dir, err := providerStateDir()
	if err != nil {
		return "", err
	}
	return resolveConnectUrlIn(opts, dir)
}
