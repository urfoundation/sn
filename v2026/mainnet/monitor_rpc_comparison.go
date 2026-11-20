// Two independently approved routes compare fixed finalized evidence. Neither
// agreement nor the signed read policy grants signing or activation authority.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

const monitorRpcComparisonPolicySchema = "urnetwork-mainnet-rpc-comparison-policy-v1"
const monitorRpcComparisonPurpose = "finalized-native-evm-state-comparison-v1"
const monitorRpcComparisonSchema = "urnetwork-mainnet-rpc-comparison-v1"
const monitorRpcComparisonBudget = 300 * time.Second
const maximumMonitorRpcComparisonKeys = 8
const maximumMonitorRpcComparisonValueBytes = 64 * 1024
const maximumMonitorRpcComparisonEventBytes = 8 * 1024

// The independently pinned policy supplies identities and keys. Endpoint
// replies and different URL spellings cannot assert operator independence.
type monitorRpcComparisonRoute struct {
	OperatorId        string `json:"operator_id"`
	ProviderId        string `json:"provider_id"`
	RouteId           string `json:"route_id"`
	Url               string `json:"url"`
	ApprovalPublicKey string `json:"approval_ed25519_public_key"`
	Signature         string `json:"signature_ed25519"`
}

// Both route approvers sign the entire pair, exact artifact and finite fact
// scope. The external file hash pins their trusted keys before any RPC read.
type monitorRpcComparisonPolicy struct {
	Schema       string                      `json:"schema"`
	Purpose      string                      `json:"purpose"`
	NotBefore    string                      `json:"not_before"`
	ExpiresAt    string                      `json:"expires_at"`
	Network      planNetwork                 `json:"network"`
	Runtime      crv4.RuntimeVersionIdentity `json:"runtime_version"`
	CodeHash     string                      `json:"runtime_code_hash"`
	MetadataHash string                      `json:"runtime_metadata_hash"`
	StorageKeys  []string                    `json:"storage_keys"`
	Primary      monitorRpcComparisonRoute   `json:"primary"`
	Secondary    monitorRpcComparisonRoute   `json:"secondary"`
}

// The domain-separated bytes exclude only signatures, retaining both keys and
// route declarations so an approval cannot be moved to another pair or purpose.
func (self monitorRpcComparisonPolicy) signingBytes() ([]byte, error) {
	self.Primary.Signature, self.Secondary.Signature = "", ""
	raw, err := json.Marshal(self)
	return append([]byte(monitorRpcComparisonPolicySchema+"\x00"), raw...), err
}

// Expiry is checked before and after a sample; observing after expiry cannot
// extend approval just because the route began responding earlier.
func (self monitorRpcComparisonPolicy) current(now time.Time) bool {
	start, startErr := time.Parse(time.RFC3339Nano, self.NotBefore)
	end, endErr := time.Parse(time.RFC3339Nano, self.ExpiresAt)
	return startErr == nil && endErr == nil && start.Before(end) && !now.Before(start) && now.Before(end)
}

// Host normalization rejects trivial same-backend aliases in addition to the
// explicit independent provider/operator declarations. DNS is not independence.
func monitorRpcComparisonHost(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.Scheme != "http" && parsed.Scheme != "https" || len(raw) > 2048 {
		return "", errors.New("comparison route requires a bounded literal HTTP(S) URL")
	}
	host := strings.TrimRight(strings.ToLower(parsed.Hostname()), ".")
	if address := net.ParseIP(host); address != nil {
		host = address.String()
	}
	if host == "" {
		return "", errors.New("comparison route host is empty")
	}
	return host, nil
}

// Admission is offline and closed: no route is contacted until the exact file,
// both signatures, independent identities, runtime and storage scope agree.
func (self monitorRpcComparisonPolicy) validate(primaryUrl string, expected identityExpectation, now time.Time) error {
	if self.Schema != monitorRpcComparisonPolicySchema || self.Purpose != monitorRpcComparisonPurpose || !self.current(now) ||
		self.Primary.Url != primaryUrl || self.Network.NativeChain != expected.NativeChain || self.Network.GenesisHash != expected.GenesisHash || self.Network.EvmChainId != expected.EvmChainId ||
		self.Network.NativeChain == "" || !rootCanonicalHash(self.Network.GenesisHash) || self.Network.EvmChainId == 0 ||
		self.Runtime.SpecName == "" || self.Runtime.SpecVersion == 0 || self.Runtime.TransactionVersion == 0 || self.Runtime.StateVersion > 1 ||
		!rootCanonicalHash(self.CodeHash) || !rootCanonicalHash(self.MetadataHash) || len(self.StorageKeys) == 0 || len(self.StorageKeys) > maximumMonitorRpcComparisonKeys {
		return errors.New("comparison policy scope, validity, runtime or network differs")
	}
	seen := map[string]bool{}
	for _, key := range self.StorageKeys {
		raw, err := hex.DecodeString(strings.TrimPrefix(key, "0x"))
		if err != nil || len(raw) == 0 || len(raw) > 256 || key != "0x"+hex.EncodeToString(raw) || seen[key] {
			return errors.New("comparison storage keys must be distinct bounded canonical hex")
		}
		seen[key] = true
	}
	firstHost, firstErr := monitorRpcComparisonHost(self.Primary.Url)
	secondHost, secondErr := monitorRpcComparisonHost(self.Secondary.Url)
	if firstErr != nil || secondErr != nil || firstHost == secondHost || self.Primary.OperatorId == self.Secondary.OperatorId || self.Primary.ProviderId == self.Secondary.ProviderId || self.Primary.RouteId == self.Secondary.RouteId || self.Primary.ApprovalPublicKey == self.Secondary.ApprovalPublicKey {
		return errors.New("comparison requires independently approved operators, providers, routes, hosts and keys")
	}
	message, err := self.signingBytes()
	if err != nil {
		return err
	}
	for _, route := range []monitorRpcComparisonRoute{self.Primary, self.Secondary} {
		key, keyErr := rootReceiptHex(route.ApprovalPublicKey, ed25519.PublicKeySize)
		signature, signatureErr := hex.DecodeString(route.Signature)
		if !monitorRolePattern.MatchString(route.OperatorId) || !monitorRolePattern.MatchString(route.ProviderId) || !monitorRolePattern.MatchString(route.RouteId) || keyErr != nil || len(key) != ed25519.PublicKeySize || route.ApprovalPublicKey != "0x"+hex.EncodeToString(key) || signatureErr != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, message, signature) {
			return errors.New("comparison route approval is invalid")
		}
	}
	return nil
}

// One synchronous owner holds two signer-free HTTP clients. The outer deadline
// spans both complete views; existing transport attempts are capped at 60s.
type monitorRpcComparison struct {
	policy    monitorRpcComparisonPolicy
	policyPin string
	primary   *rpcClient
	secondary *rpcClient
	now       func() time.Time
}

// The private context key carries an already admitted read owner through the
// existing independently supervised chain worker, without changing role policy.
type monitorRpcComparisonContextKey struct{}

// The chain worker can own either admitted routes or a still unavailable
// pinned input. Optional input failure never stops unrelated monitor roles.
type monitorRpcComparisonReader interface {
	compare(context.Context, string) monitorRpcComparisonResult
	close()
}

// Admission retries only the original pinned file. Once admitted, its route
// authority stays immutable until the command owner joins and restarts.
type monitorRpcComparisonRequest struct {
	path       string
	pin        string
	primaryUrl string
	expected   identityExpectation
	now        func() time.Time
	owner      *monitorRpcComparison
}

// One owner deadline includes optional input admission and both route reads.
func (self *monitorRpcComparisonRequest) compare(ctx context.Context, blockHash string) monitorRpcComparisonResult {
	ownerCtx, cancel := context.WithTimeout(ctx, monitorRpcComparisonBudget)
	defer cancel()
	if self.owner == nil {
		owner, err := loadMonitorRpcComparison(ownerCtx, self.path, self.pin, self.primaryUrl, self.expected, self.now)
		if err != nil {
			result := unknownMonitorRpcComparison("independent RPC policy input could not be admitted")
			result.PolicySha256, result.NativeHash = self.pin, blockHash
			result.ObservedAt = self.now().UTC().Format(time.RFC3339Nano)
			return result
		}
		self.owner = owner
	}
	return self.owner.compare(ownerCtx, blockHash)
}

// The command invokes close only after all its sampling workers have joined.
func (self *monitorRpcComparisonRequest) close() {
	self.owner.close()
}

// Loading never executes a command, resolves an approval URL, or creates keys.
func loadMonitorRpcComparison(ctx context.Context, path, pin, primaryUrl string, expected identityExpectation, now func() time.Time) (*monitorRpcComparison, error) {
	raw, err := readBootstrapChainInput(ctx, planFileReference{Path: path, Sha256: pin}, 32*1024)
	if err != nil {
		return nil, err
	}
	var policy monitorRpcComparisonPolicy
	if err := decodePlanJson(raw, &policy); err != nil {
		return nil, err
	}
	if err := policy.validate(primaryUrl, expected, now()); err != nil {
		return nil, err
	}
	primary, err := newRpcClient(policy.Primary.Url, monitorRpcComparisonBudget)
	if err != nil {
		return nil, err
	}
	secondary, err := newRpcClient(policy.Secondary.Url, monitorRpcComparisonBudget)
	if err != nil {
		primary.httpClient.CloseIdleConnections()
		return nil, err
	}
	return &monitorRpcComparison{policy: policy, policyPin: pin, primary: primary, secondary: secondary, now: now}, nil
}

// Owned HTTP connection pools are released after the chain worker joins.
func (self *monitorRpcComparison) close() {
	if self != nil {
		self.primary.httpClient.CloseIdleConnections()
		self.secondary.httpClient.CloseIdleConnections()
	}
}

// A nil value means an observed absent key, distinct from unavailable input.
type monitorRpcComparisonStorage struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

// Raw headers and exact bounded storage values retain the compared facts.
// Runtime hashes bind separately authenticated raw artifacts, not spec alone.
type monitorRpcComparisonFacts struct {
	identity     chainIdentity
	NativeHash   string                        `json:"native_hash"`
	NativeNumber uint64                        `json:"native_number"`
	Runtime      crv4.RuntimeVersionIdentity   `json:"runtime_version"`
	CodeHash     string                        `json:"runtime_code_hash"`
	MetadataHash string                        `json:"runtime_metadata_hash"`
	NativeHeader rootReceiptHeader             `json:"native_header"`
	PostLog      frontierPostLog               `json:"frontier_post_log"`
	EvmHeader    mappedEvmHeader               `json:"evm_header"`
	Storage      []monitorRpcComparisonStorage `json:"storage"`
}

// Agreement is limited to this explicit fact scope. No inferred completeness,
// quorum, economic outcome, or activation permission is serialized.
type monitorRpcComparisonResult struct {
	Schema              string                     `json:"schema"`
	Status              string                     `json:"status"`
	Detail              string                     `json:"detail"`
	IndependentRpc      bool                       `json:"independent_rpc"`
	ActivationAuthority bool                       `json:"activation_authority"`
	PolicySha256        string                     `json:"policy_sha256,omitempty"`
	Purpose             string                     `json:"purpose"`
	ObservedAt          string                     `json:"observed_at,omitempty"`
	NativeHash          string                     `json:"native_hash,omitempty"`
	PrimaryRouteId      string                     `json:"primary_route_id,omitempty"`
	SecondaryRouteId    string                     `json:"secondary_route_id,omitempty"`
	Primary             *monitorRpcComparisonFacts `json:"primary,omitempty"`
	Secondary           *monitorRpcComparisonFacts `json:"secondary,omitempty"`
}

// Missing route input is an unknown observation, never false agreement.
func unknownMonitorRpcComparison(detail string) monitorRpcComparisonResult {
	return monitorRpcComparisonResult{Schema: monitorRpcComparisonSchema, Status: "input-unknown", Detail: detail, Purpose: monitorRpcComparisonPurpose}
}

// Each route obtains its own authenticated identity at the original hash.
// The mapping close checks follow all storage reads; no later head retargets it.
func (self *monitorRpcComparison) read(ctx context.Context, client *rpcClient, blockHash string) (monitorRpcComparisonFacts, error) {
	identity, err := client.readIdentityAt(ctx, blockHash)
	if err != nil {
		return monitorRpcComparisonFacts{}, err
	}
	expected := identityExpectation{NativeChain: self.policy.Network.NativeChain, GenesisHash: self.policy.Network.GenesisHash, EvmChainId: self.policy.Network.EvmChainId}
	if err := expected.match(identity); err != nil {
		return monitorRpcComparisonFacts{}, errors.Join(errRpcIdentityMismatch, err)
	}
	runtime, err := client.readRuntimeSnapshotAtIdentity(ctx, identity)
	if err != nil {
		return monitorRpcComparisonFacts{}, err
	}
	if runtime.Version != self.policy.Runtime || runtime.CodeHash != self.policy.CodeHash || runtime.MetadataHash != self.policy.MetadataHash {
		return monitorRpcComparisonFacts{}, fmt.Errorf("%w: comparison runtime differs from approved exact artifact", errRpcIdentityMismatch)
	}
	storage := make([]monitorRpcComparisonStorage, 0, len(self.policy.StorageKeys))
	for _, key := range self.policy.StorageKeys {
		var value *string
		if err := client.callBoundedRead(ctx, "state_getStorage", []any{key, blockHash}, &value, true, 2*maximumMonitorRpcComparisonValueBytes+maxRpcReplyBytes); err != nil {
			return monitorRpcComparisonFacts{}, err
		}
		if value != nil {
			raw, decodeErr := hex.DecodeString(strings.TrimPrefix(*value, "0x"))
			if decodeErr != nil || len(raw) > maximumMonitorRpcComparisonValueBytes || *value != "0x"+hex.EncodeToString(raw) {
				return monitorRpcComparisonFacts{}, fmt.Errorf("%w: comparison storage is not bounded canonical hex", errRpcIntegrity)
			}
		}
		storage = append(storage, monitorRpcComparisonStorage{Key: key, Value: value})
	}
	mapping, err := client.readFinalizedMappingAtIdentity(ctx, identity)
	if err != nil {
		return monitorRpcComparisonFacts{}, err
	}
	return monitorRpcComparisonFacts{identity: identity, NativeHash: identity.FinalizedHash, NativeNumber: identity.FinalizedNumber, Runtime: runtime.Version, CodeHash: runtime.CodeHash, MetadataHash: runtime.MetadataHash, NativeHeader: mapping.NativeHeader, PostLog: mapping.PostLog, EvmHeader: mapping.EvmHeader, Storage: storage}, nil
}

// A failed half never becomes a matching pair. Closing both finality witnesses
// after both views catches a route replacement during the other route's reads.
func (self *monitorRpcComparison) compare(ctx context.Context, blockHash string) (result monitorRpcComparisonResult) {
	result = unknownMonitorRpcComparison("independent RPC policy or selected boundary unavailable")
	if self == nil || ctx == nil || !rootCanonicalHash(blockHash) {
		return result
	}
	result.PolicySha256, result.NativeHash = self.policyPin, blockHash
	result.PrimaryRouteId, result.SecondaryRouteId = self.policy.Primary.RouteId, self.policy.Secondary.RouteId
	startedAt := self.now()
	if !self.policy.current(startedAt) {
		result.Detail = "independent RPC approval is outside its validity window"
		return result
	}
	expiresAt, _ := time.Parse(time.RFC3339Nano, self.policy.ExpiresAt)
	ownerCtx, cancel := context.WithTimeout(ctx, min(monitorRpcComparisonBudget, expiresAt.Sub(startedAt)))
	defer cancel()
	finishError := func(err error) monitorRpcComparisonResult {
		result.Detail = "independent RPC observation unavailable"
		if errors.Is(err, errRpcIntegrity) || errors.Is(err, errRpcIdentityMismatch) {
			result.Status, result.Detail = "disagreement", "a route contradicts the approved network, runtime or fixed boundary"
		}
		result.ObservedAt = self.now().UTC().Format(time.RFC3339Nano)
		return result
	}
	primary, err := self.read(ownerCtx, self.primary, blockHash)
	if err != nil {
		return finishError(err)
	}
	secondaryFinality, err := self.secondary.readNativeFinality(ownerCtx)
	if err != nil {
		return finishError(err)
	}
	if secondaryFinality.Number < primary.NativeNumber {
		return finishError(errRpcObservationUnavailable)
	}
	secondary, err := self.read(ownerCtx, self.secondary, blockHash)
	if err != nil {
		return finishError(err)
	}
	// Reauthenticate both routes after the pair, including exact runtime bytes.
	// These closing reads cannot silently replace the original selected hash.
	for index, client := range []*rpcClient{self.primary, self.secondary} {
		originalFacts := primary
		if index == 1 {
			originalFacts = secondary
		}
		original := originalFacts.identity
		if err := client.closeNativeFinality(ownerCtx, original.finalityWitness, nativeFinalityPoint{Hash: blockHash, Number: original.FinalizedNumber}); err != nil {
			return finishError(err)
		}
		identity, err := client.readIdentityAt(ownerCtx, blockHash)
		if err != nil {
			return finishError(err)
		}
		expected := identityExpectation{NativeChain: self.policy.Network.NativeChain, GenesisHash: self.policy.Network.GenesisHash, EvmChainId: self.policy.Network.EvmChainId}
		if err := expected.match(identity); err != nil {
			return finishError(errors.Join(errRpcIdentityMismatch, err))
		}
		runtime, err := client.readRuntimeSnapshotAtIdentity(ownerCtx, identity)
		if err != nil {
			return finishError(err)
		}
		if runtime.Version != self.policy.Runtime || runtime.CodeHash != self.policy.CodeHash || runtime.MetadataHash != self.policy.MetadataHash {
			return finishError(errRpcIdentityMismatch)
		}
		mapping, err := client.readFinalizedMappingAtIdentity(ownerCtx, identity)
		if err != nil {
			return finishError(err)
		}
		if rootObjectHash(mapping.NativeHeader) != rootObjectHash(originalFacts.NativeHeader) || rootObjectHash(mapping.EvmHeader) != rootObjectHash(originalFacts.EvmHeader) || rootObjectHash(mapping.PostLog) != rootObjectHash(originalFacts.PostLog) {
			return finishError(errRpcIntegrity)
		}
		if err := client.closeSnapshotFinality(ownerCtx, original); err != nil {
			return finishError(err)
		}
	}
	if ownerCtx.Err() != nil || !self.policy.current(self.now()) {
		return finishError(errors.Join(ownerCtx.Err(), errors.New("comparison approval expired")))
	}
	result.Primary, result.Secondary = &primary, &secondary
	result.IndependentRpc, result.ObservedAt = true, self.now().UTC().Format(time.RFC3339Nano)
	result.Status, result.Detail = "agreement", "fixed native/EVM headers, exact runtime and approved storage keys agree"
	if rootObjectHash(primary) != rootObjectHash(secondary) {
		result.Status, result.Detail = "disagreement", "authenticated fixed-boundary facts differ between independent routes"
	}
	// The shared exporter has a 16KiB record limit. Leave space for the chain
	// envelope and diagnostics; never claim retained evidence after truncation.
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > maximumMonitorRpcComparisonEventBytes {
		result.Primary, result.Secondary, result.IndependentRpc = nil, nil, false
		result.Status, result.Detail = "input-unknown", "complete comparison evidence exceeds the bounded event profile"
	}
	return result
}
