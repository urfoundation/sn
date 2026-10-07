// Each operator stores the chain and a delegation of the network to the
// chain's head. Every request carries the network JWT as bearer, and every
// answer is read within a byte bound.
package hotkeywallet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urnetwork/connect/v2026"
)

// Bounds every answer except the wallet list.
const maximumAnswerBytes = 64 * 1024

// The wallet list names one entry per provider wallet of the network.
const maximumWalletsBytes = 16 * 1024 * 1024

// The operator answered with an error status or an error result.
var ErrRefused = errors.New("operator refused the hotkey wallet request")

// One operator's hotkey wallet API, called as the network that the JWT names.
// ApiUrl must use https, or plaintext http only for a literal loopback host.
// Requests time out after 30 seconds when Client is nil. Redirects are never
// followed, so the JWT and the signatures stay on the operator's origin.
type Operator struct {
	ApiUrl string
	// The network JWT.
	ByJwt  string
	Client *http.Client
	// Generations sent per request; maximumNewGenerationsPerSubmission when 0.
	newGenerations int
}

// An operator adds at most this many generations of a chain per request.
const maximumNewGenerationsPerSubmission = 64

// Stores the complete chain at the operator, which must acknowledge the
// chain's own head. The chain is verified before it is sent. A chain longer
// than one request may add goes as successive prefixes, each acknowledged at
// its own head; a prefix the operator already holds is an idempotent replay.
func (self Operator) SubmitChain(ctx context.Context, chain []protocol.HotkeyWalletMappingConsent) (headHash [32]byte, generation uint64, returnErr error) {
	head, _, err := protocol.VerifyHotkeyWalletMappingLineage(ctx, chain)
	if err != nil {
		return [32]byte{}, 0, err
	}
	step := self.newGenerations
	if step <= 0 {
		step = maximumNewGenerationsPerSubmission
	}
	for end := min(len(chain), step); ; end = min(len(chain), end+step) {
		// the original hash the protocol computes, for the prefix's last generation
		raw, err := json.Marshal(chain[end-1])
		if err != nil {
			return [32]byte{}, 0, err
		}
		headHash, generation, returnErr = self.submitPrefix(ctx, chain[:end], head.Hotkey, sha256.Sum256(raw))
		if returnErr != nil || end == len(chain) {
			return
		}
	}
}

// One request with a verified prefix of the chain, ending at the given hash.
func (self Operator) submitPrefix(ctx context.Context, chain []protocol.HotkeyWalletMappingConsent, hotkey [32]byte, hash [32]byte) ([32]byte, uint64, error) {
	generation := uint64(len(chain))
	args := struct {
		Originals []protocol.HotkeyWalletMappingConsent `json:"originals"`
	}{Originals: chain}
	var result struct {
		HotkeySs58 string   `json:"hotkey_ss58"`
		HeadHash   [32]byte `json:"head_hash"`
		Generation uint64   `json:"generation"`
	}
	if err := self.call(ctx, http.MethodPost, "/sn/wallet/hotkey-consent", args, maximumAnswerBytes, &result); err != nil {
		return [32]byte{}, 0, err
	}
	acknowledged, err := ss58.DecodeWithPrefix(result.HotkeySs58, ss58.BittensorPrefix)
	if err != nil || acknowledged != hotkey || result.HeadHash != hash || result.Generation != generation {
		return [32]byte{}, 0, fmt.Errorf("%w: the operator acknowledged head %x generation %d of %q, not head %x generation %d", protocol.ErrWalletMappingIntegrity, result.HeadHash, result.Generation, result.HotkeySs58, hash, generation)
	}
	return hash, generation, nil
}

// Leaves a delegation that already adopts the consent head as it is.
// Otherwise it requests a delegation challenge and signs and accepts it only
// when the protocol decoder accepts it and it names exactly the hotkey, the
// consent head and generation, the epochs, and the network and user of the
// JWT. A challenge that fails a check is refused unsigned. A delegation of the
// hotkey to a later consent generation is never moved back to an earlier one.
// The operator requires fromEpoch to follow its current epoch and the previous
// delegation's from epoch.
func (self Operator) EnsureDelegation(ctx context.Context, hotkey *crv4.Keypair, headHash [32]byte, generation uint64, fromEpoch, throughEpoch uint64) error {
	if hotkey == nil || headHash == ([32]byte{}) || generation == 0 {
		return errors.New("a hotkey delegation needs the hotkey and the consent head")
	}
	networkId, userId, err := networkSession(self.ByJwt)
	if err != nil {
		return err
	}
	adopted, err := self.delegationAdopts(ctx, hotkey.PublicKey(), headHash, generation)
	if err != nil || adopted {
		return err
	}

	challengeArgs := struct {
		HotkeySs58        string   `json:"hotkey_ss58"`
		ConsentHeadHash   [32]byte `json:"consent_head_hash"`
		ConsentGeneration uint64   `json:"consent_generation"`
		FromEpoch         uint64   `json:"from_epoch"`
		ThroughEpoch      uint64   `json:"through_epoch"`
	}{HotkeySs58: hotkey.Address(), ConsentHeadHash: headHash, ConsentGeneration: generation, FromEpoch: fromEpoch, ThroughEpoch: throughEpoch}
	var challenge struct {
		Message string `json:"message"`
	}
	if err := self.call(ctx, http.MethodPost, "/sn/wallet/hotkey-delegation", challengeArgs, maximumAnswerBytes, &challenge); err != nil {
		return err
	}
	statement, err := protocol.DecodeHotkeyNetworkDelegationStatement(challenge.Message)
	if err != nil {
		return fmt.Errorf("the operator's delegation challenge is not a hotkey delegation: %w", err)
	}
	for _, field := range []struct {
		name    string
		matches bool
	}{
		{name: "requested hotkey", matches: statement.Hotkey == hotkey.PublicKey()},
		{name: "requested consent head", matches: statement.ConsentHeadHash == headHash && statement.ConsentGeneration == generation},
		{name: "requested earning epochs", matches: statement.FromEpoch == fromEpoch && statement.ThroughEpoch == throughEpoch},
		{name: "network of the JWT", matches: statement.NetworkId == networkId},
		{name: "user of the JWT", matches: statement.UserId == userId},
	} {
		if !field.matches {
			return fmt.Errorf("%w: the operator's delegation challenge does not match the %s", protocol.ErrWalletMappingIntegrity, field.name)
		}
	}
	signature, err := Sign(hotkey, challenge.Message)
	if err != nil {
		return err
	}
	accepted, hash, err := protocol.VerifyHotkeyNetworkDelegation(ctx, protocol.WalletMappingConsent{Message: challenge.Message, Signature: signature})
	if err != nil {
		return err
	}

	// the accept route of every consent kind; coldkey_ss58 names the signer
	acceptArgs := struct {
		ColdkeySs58 string `json:"coldkey_ss58"`
		Message     string `json:"message"`
		Signature   string `json:"signature"`
	}{ColdkeySs58: hotkey.Address(), Message: challenge.Message, Signature: "0x" + hex.EncodeToString(signature[:])}
	var result struct {
		Error             *resultError `json:"error,omitempty"`
		MappingHash       string       `json:"mapping_hash"`
		MappingGeneration uint64       `json:"mapping_generation"`
	}
	if err := self.call(ctx, http.MethodPost, "/sn/wallet", acceptArgs, maximumAnswerBytes, &result); err != nil {
		return err
	}
	if result.Error != nil {
		return result.Error.err()
	}
	if acknowledged, err := decodeHexHash(result.MappingHash); err != nil || acknowledged != hash || result.MappingGeneration != accepted.Generation {
		return fmt.Errorf("%w: the operator acknowledged delegation %q generation %d, not %x generation %d", protocol.ErrWalletMappingIntegrity, result.MappingHash, result.MappingGeneration, hash, accepted.Generation)
	}
	return nil
}

// Whether the operator lists the network's hotkey entry for this hotkey at the
// consent head. An entry at a later consent generation of the same hotkey is
// an error: the local chain is behind the one the operator already adopted.
func (self Operator) delegationAdopts(ctx context.Context, hotkey [32]byte, headHash [32]byte, generation uint64) (bool, error) {
	var result struct {
		Wallets []struct {
			ClientId          *string `json:"client_id"`
			ConsentScope      string  `json:"consent_scope"`
			HotkeySs58        string  `json:"hotkey_ss58"`
			ConsentHeadHash   string  `json:"consent_head_hash"`
			ConsentGeneration uint64  `json:"consent_generation"`
		} `json:"wallets"`
		Error *resultError `json:"error,omitempty"`
	}
	if err := self.call(ctx, http.MethodGet, "/sn/wallet", nil, maximumWalletsBytes, &result); err != nil {
		return false, err
	}
	if result.Error != nil {
		return false, result.Error.err()
	}
	for _, wallet := range result.Wallets {
		if wallet.ConsentScope != protocol.EarningWalletModeHotkey || wallet.ClientId != nil {
			continue
		}
		if entryHotkey, err := ss58.DecodeWithPrefix(wallet.HotkeySs58, ss58.BittensorPrefix); err != nil || entryHotkey != hotkey {
			continue
		}
		if entryHead, err := decodeHexHash(wallet.ConsentHeadHash); err == nil && entryHead == headHash && wallet.ConsentGeneration == generation {
			return true, nil
		}
		if wallet.ConsentGeneration > generation {
			return false, fmt.Errorf("the operator's delegation already adopts consent generation %d of this hotkey, after the local generation %d", wallet.ConsentGeneration, generation)
		}
	}
	return false, nil
}

// The network and user that the JWT names. The operator verifies the JWT; its
// claims only bound what the hotkey signs. A client JWT is refused, since a
// delegation covers the whole network.
func networkSession(byJwt string) (networkId [16]byte, userId [16]byte, returnErr error) {
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(byJwt, claims); err != nil {
		return [16]byte{}, [16]byte{}, fmt.Errorf("operator network JWT: %w", err)
	}
	if clientId, ok := claims["client_id"]; ok && clientId != nil {
		return [16]byte{}, [16]byte{}, errors.New("the operator JWT is a client JWT; a hotkey delegation needs the network JWT")
	}
	for _, claim := range []struct {
		name  string
		value *[16]byte
	}{
		{name: "network_id", value: &networkId},
		{name: "user_id", value: &userId},
	} {
		raw, ok := claims[claim.name].(string)
		id, err := connect.ParseId(raw)
		if !ok || err != nil || id == (connect.Id{}) {
			return [16]byte{}, [16]byte{}, fmt.Errorf("the operator network JWT names no %s", claim.name)
		}
		*claim.value = [16]byte(id)
	}
	return networkId, userId, nil
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

// One request with the network JWT as bearer. Any status but 200 is a refusal
// that carries the status and the start of the answer. A 200 answer must be
// JSON of at most maximum bytes.
func (self Operator) call(ctx context.Context, method string, path string, body any, maximum int64, result any) error {
	base, err := self.origin()
	if err != nil {
		return err
	}
	if self.ByJwt == "" {
		return errors.New("the operator network JWT is missing")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+self.ByJwt)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{Timeout: 30 * time.Second}
	if self.Client != nil {
		client = *self.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return err
	}
	if int64(len(answer)) > maximum {
		return fmt.Errorf("%s %s answer exceeds %d bytes", method, path, maximum)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s %s: %s: %s", ErrRefused, method, path, response.Status, strings.TrimSpace(string(answer[:min(len(answer), 512)])))
	}
	if err := json.Unmarshal(answer, result); err != nil {
		return fmt.Errorf("%s %s answer is not JSON: %w", method, path, err)
	}
	return nil
}

// The API URL without a trailing slash: https, or plaintext http only for a
// literal loopback host, without credentials, query or fragment.
func (self Operator) origin() (string, error) {
	u, err := url.Parse(self.ApiUrl)
	if err != nil {
		return "", errors.New("the operator api url is not a URL")
	}
	if u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Scheme != "https" && !(u.Scheme == "http" && loopbackHost(u.Hostname())) {
		return "", fmt.Errorf("operator api url %q must use https (plaintext http only for loopback) with no credentials, query or fragment", u.Redacted())
	}
	return strings.TrimSuffix(self.ApiUrl, "/"), nil
}

// Only literal loopback names qualify; DNS is never consulted, so a later
// rebinding cannot move plaintext credentials off the host.
func loopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// A 32-byte hash as hex, with or without 0x, in either case.
func decodeHexHash(value string) ([32]byte, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X"))
	if err != nil || len(raw) != 32 {
		return [32]byte{}, fmt.Errorf("%q is not a 32-byte hex hash", value)
	}
	return [32]byte(raw), nil
}
