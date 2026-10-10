// Package hotkeyauth signs in to a network operator with a Bittensor hotkey.
// It uses the wallet sign-in every operator already serves, so it needs no
// new operator API and no chain transaction: the hotkey signs the operator's
// single-use challenge as a TAO wallet, and the operator answers with the JWT
// of the network the hotkey administers, creating that network at the first
// sign-in. Any valid hotkey may sign in, within the operator's wallet sign-in
// rate limits.
//
// The hotkey only ever signs a challenge in the exact sign-in format, wrapped
// in <Bytes></Bytes> as polkadot-js does, so an operator cannot obtain a
// hotkey signature over a transaction or over another statement.
package hotkeyauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The wallet sign-in chain name for sr25519 keys with ss58 prefix 42.
const Blockchain = "TAO"

const maximumResponseBytes = 64 * 1024

// Distinct names tried when an operator refuses a network name as taken.
const networkNameAttempts = 4

var ErrRefused = errors.New("operator refused the hotkey sign-in")

type Settings struct {
	// The operator API, for example https://api.bringyour.com.
	ApiUrl string
	Hotkey *crv4.Keypair
	// Names a network created at the first sign-in; NetworkName(hotkey) when
	// empty.
	NetworkName string
	// Optional transport; requests time out after 30 seconds when nil.
	Client *http.Client
}

type Network struct {
	// The network JWT.
	ByJwt string
	// The hotkey had no network on this operator and this sign-in created it.
	Created bool
}

// The network JWT of the network the hotkey administers on one operator,
// created when the hotkey has none.
func SignIn(ctx context.Context, settings Settings) (*Network, error) {
	if settings.Hotkey == nil {
		return nil, errors.New("hotkey sign-in needs a hotkey")
	}
	if strings.TrimSpace(settings.ApiUrl) == "" {
		return nil, errors.New("hotkey sign-in needs an operator api url")
	}
	if settings.Client == nil {
		settings.Client = &http.Client{Timeout: 30 * time.Second}
	}
	settings.ApiUrl = strings.TrimSuffix(settings.ApiUrl, "/")
	baseName := settings.NetworkName
	if baseName == "" {
		baseName = NetworkName(settings.Hotkey.PublicKey())
	}
	for attempt := 0; attempt < networkNameAttempts; attempt++ {
		byJwt, err := login(ctx, settings)
		if err != nil || byJwt != "" {
			return &Network{ByJwt: byJwt}, err
		}
		name := baseName
		if attempt > 0 {
			name = fmt.Sprintf("%s-%d", baseName, attempt+1)
		}
		byJwt, conflict, err := createNetwork(ctx, settings, name)
		if err != nil {
			return nil, err
		}
		if byJwt != "" {
			return &Network{ByJwt: byJwt, Created: true}, nil
		}
		if !conflict {
			return nil, fmt.Errorf("%w: network create returned no network", ErrRefused)
		}
		// A taken name, or a concurrent sign-in that created the network:
		// log in again before trying the next name.
	}
	return nil, fmt.Errorf("%w: no network name was available after %d attempts", ErrRefused, networkNameAttempts)
}

// The default name of a hotkey's network: stable, unrelated to other hotkeys'
// names, and within the operator's name rules (5 to 50 of [a-z0-9-]).
func NetworkName(hotkey [32]byte) string {
	digest := sha256.Sum256(hotkey[:])
	return "tao-" + hex.EncodeToString(digest[:])[:24]
}

type walletAuth struct {
	WalletAddress   string `json:"wallet_address"`
	WalletSignature string `json:"wallet_signature"`
	WalletMessage   string `json:"wallet_message"`
	Blockchain      string `json:"blockchain"`
}

type resultError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func (self *resultError) err() error {
	if self.Code != "" {
		return fmt.Errorf("%w: %s (%s)", ErrRefused, self.Message, self.Code)
	}
	return fmt.Errorf("%w: %s", ErrRefused, self.Message)
}

// The existing network's JWT, or "" when the hotkey has no network yet.
func login(ctx context.Context, settings Settings) (string, error) {
	auth, err := signedChallenge(ctx, settings)
	if err != nil {
		return "", err
	}
	var result struct {
		Network *struct {
			ByJwt string `json:"by_jwt"`
		} `json:"network,omitempty"`
		Error *resultError `json:"error,omitempty"`
	}
	status, err := post(ctx, settings, "/auth/login", map[string]any{"wallet_auth": auth, "result_errors": true}, &result)
	if err != nil {
		return "", err
	}
	switch {
	case result.Error != nil:
		return "", result.Error.err()
	case status != http.StatusOK:
		return "", fmt.Errorf("%w: login status %d", ErrRefused, status)
	case result.Network != nil && result.Network.ByJwt != "":
		return result.Network.ByJwt, nil
	}
	return "", nil
}

// The created network's JWT, or conflict when the operator refused with 409:
// the name is taken or the hotkey already administers a network.
func createNetwork(ctx context.Context, settings Settings, name string) (byJwt string, conflict bool, returnErr error) {
	auth, err := signedChallenge(ctx, settings)
	if err != nil {
		return "", false, err
	}
	var result struct {
		Network *struct {
			ByJwt *string `json:"by_jwt,omitempty"`
		} `json:"network,omitempty"`
		Error *resultError `json:"error,omitempty"`
	}
	status, err := post(ctx, settings, "/auth/network-create", map[string]any{
		"network_name":  name,
		"terms":         true,
		"wallet_auth":   auth,
		"result_errors": true,
	}, &result)
	if err != nil {
		if status == http.StatusConflict {
			return "", true, nil
		}
		return "", false, err
	}
	if status == http.StatusConflict {
		return "", true, nil
	}
	if result.Error != nil {
		return "", false, result.Error.err()
	}
	if status != http.StatusOK || result.Network == nil || result.Network.ByJwt == nil || *result.Network.ByJwt == "" {
		return "", false, fmt.Errorf("%w: network create status %d", ErrRefused, status)
	}
	return *result.Network.ByJwt, false, nil
}

// Requests a fresh single-use challenge and signs it, after checking that it
// is exactly a sign-in challenge.
func signedChallenge(ctx context.Context, settings Settings) (*walletAuth, error) {
	address := settings.Hotkey.Address()
	var result struct {
		MessageTemplate string       `json:"message_template"`
		Error           *resultError `json:"error,omitempty"`
	}
	status, err := post(ctx, settings, "/auth/wallet-challenge", map[string]any{"wallet_address": address, "blockchain": Blockchain}, &result)
	if err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, result.Error.err()
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%w: challenge status %d", ErrRefused, status)
	}
	if err := ValidateChallenge(result.MessageTemplate); err != nil {
		return nil, err
	}
	signature, err := settings.Hotkey.Sign(wrapBytes(result.MessageTemplate))
	if err != nil {
		return nil, err
	}
	return &walletAuth{
		WalletAddress:   address,
		WalletSignature: "0x" + hex.EncodeToString(signature),
		WalletMessage:   result.MessageTemplate,
		Blockchain:      Blockchain,
	}, nil
}

// A sign-in challenge is exactly three lines: the fixed title, a challenge
// token and a decimal timestamp. Anything else is refused unsigned.
func ValidateChallenge(message string) error {
	lines := strings.Split(message, "\n")
	if len(message) > 512 || len(lines) != 3 || lines[0] != "Sign in to URnetwork" || !strings.HasPrefix(lines[1], "Challenge: ") || !strings.HasPrefix(lines[2], "Timestamp: ") {
		return fmt.Errorf("%w: the operator challenge is not a sign-in challenge", ErrRefused)
	}
	token := strings.TrimPrefix(lines[1], "Challenge: ")
	if len(token) < 16 {
		return fmt.Errorf("%w: the operator challenge token is too short", ErrRefused)
	}
	for _, character := range token {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("+/=-_", character)) {
			return fmt.Errorf("%w: the operator challenge token is not base64 or hex", ErrRefused)
		}
	}
	timestamp := strings.TrimPrefix(lines[2], "Timestamp: ")
	if value, err := strconv.ParseInt(timestamp, 10, 64); err != nil || value <= 0 || strconv.FormatInt(value, 10) != timestamp {
		return fmt.Errorf("%w: the operator challenge timestamp is not a decimal time", ErrRefused)
	}
	return nil
}

// The <Bytes> wrapper keeps a signed message from ever being a valid
// extrinsic payload; operators verify the wrapped and the raw form.
func wrapBytes(message string) []byte {
	return []byte("<Bytes>" + message + "</Bytes>")
}

// Posts JSON and decodes a JSON answer whatever the status, since refusals
// carry their reason in the body. A body that is not JSON is an error that
// carries the status and the text.
func post(ctx context.Context, settings Settings, path string, body any, result any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.ApiUrl+path, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := settings.Client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil {
		return response.StatusCode, err
	}
	if len(answer) > maximumResponseBytes {
		return response.StatusCode, fmt.Errorf("%s answer exceeds %d bytes", path, maximumResponseBytes)
	}
	if err := json.Unmarshal(answer, result); err != nil {
		return response.StatusCode, fmt.Errorf("%w: %s status %d: %s", ErrRefused, path, response.StatusCode, strings.TrimSpace(string(answer)))
	}
	return response.StatusCode, nil
}
