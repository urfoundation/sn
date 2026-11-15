//go:build linux || darwin

// Independently pinned authority selects every owner, original key and bounded
// replay profile. Signed window successors select inputs, never replace replay.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

const (
	ProviderAttemptAuthoritySchema       = "urnetwork-provider-attempt-authority-v1"
	ProviderAttemptWindowAuthoritySchema = "urnetwork-provider-attempt-window-authority-v1"
	ProviderAttemptOriginalSchema        = "urnetwork-provider-attempt-original-v1"
	providerAttemptAuthorityMaxBytes     = 16 * 1024 * 1024
)

// The original policy pins this document's exact hash. Its independent signer
// may admit future complete windows without changing the original authority.
// It does not sign transport outcomes or create missing historical requests.
type ProviderAttemptAuthority struct {
	Schema           string                                      `json:"schema"`
	Registry         protocol.ProviderAttemptRegistryExpectation `json:"registry"`
	WindowSigner     [32]byte                                    `json:"window_signer"`
	WindowEndpoint   string                                      `json:"window_endpoint"`
	ScratchRoot      string                                      `json:"scratch_root"`
	MaxWindowBytes   uint64                                      `json:"max_window_bytes"`
	MaxOriginalBytes uint64                                      `json:"max_original_bytes"`
	MaxObjects       uint64                                      `json:"max_objects"`
	MaxProviders     uint64                                      `json:"max_providers"`
	MaxMetadataBytes uint64                                      `json:"max_metadata_bytes"`
	MaxRecordBytes   uint64                                      `json:"max_record_bytes"`
	MaxRecords       uint64                                      `json:"max_records"`
}

// JSON uses an explicit row rather than a map whose key conversion might
// accept aliases. The consumer rejects duplicates before building replay maps.
type ProviderAttemptBindingAuthority struct {
	ClientId connect.Id    `json:"client_id"`
	Binding  FleetScoreKey `json:"binding"`
}

// Original historical server key selection comes from independent admission,
// not from a live current-key endpoint or the receipt being verified.
type ProviderAttemptServerKey struct {
	Id  byte     `json:"id"`
	Key [32]byte `json:"key"`
}

// A portable counterpart of actual cut replay options contains no callbacks,
// scratch verdicts or caller-asserted success fields.
type ProviderAttemptOperatorAuthority struct {
	Expected         AttemptCutV2Context                  `json:"expected"`
	Policy           protocol.Policy                      `json:"policy"`
	Bounds           AttemptCutV2Bounds                   `json:"bounds"`
	Stats            ReleaseStatsConfig                   `json:"stats"`
	Bindings         []ProviderAttemptBindingAuthority    `json:"bindings"`
	MaxProviders     uint64                               `json:"max_providers"`
	MaxEgressHashes  uint64                               `json:"max_egress_hashes"`
	MaxFleetPrefixes uint64                               `json:"max_fleet_prefixes"`
	ReplayBounds     AttemptCutV2ReplayBounds             `json:"replay_bounds"`
	ServerKeys       []ProviderAttemptServerKey           `json:"server_keys"`
	Requests         ProviderAttemptRequestPreparation    `json:"requests"`
	ReceiptScope     protocol.ProviderAttemptReceiptScope `json:"receipt_scope"`
	ReceiptEndpoint  string                               `json:"receipt_endpoint"`
}

// Every original registry lane has one independently admitted replay profile.
type ProviderAttemptValidatorAuthority struct {
	Hotkey             [32]byte                                  `json:"hotkey"`
	Publication        ValidatorEvidencePublicationV2ReadOptions `json:"publication"`
	Operators          []ProviderAttemptOperatorAuthority        `json:"operators"`
	MaxParticipants    uint64                                    `json:"max_participants"`
	MaxTransitionBytes uint64                                    `json:"max_transition_bytes"`
	MaxClosureBytes    uint64                                    `json:"max_closure_bytes"`
}

// Exact canonical chain headers bind Server receipt clocks to the original
// window; the separately pinned signer authenticates their selection. Actual
// chain finality remains the containing monitor's independent responsibility.
type ProviderAttemptWindowAuthority struct {
	Schema        string                              `json:"schema"`
	AuthorityHash [32]byte                            `json:"authority_hash"`
	Domain        protocol.ProviderAttemptDomain      `json:"domain"`
	RegistryHead  [32]byte                            `json:"registry_head"`
	Window        protocol.ValidatorEvidenceWindow    `json:"window"`
	StartHeader   json.RawMessage                     `json:"start_header"`
	EndHeader     json.RawMessage                     `json:"end_header"`
	Validators    []ProviderAttemptValidatorAuthority `json:"validators"`
	Signature     []byte                              `json:"signature"`
}

// Sign only an explicit independent window selection. A producer cannot call
// this with its own receipt key and thereby authorize missing whole coverage.
func SealProviderAttemptWindowAuthority(ctx context.Context, value ProviderAttemptWindowAuthority, signer ed25519.PrivateKey) (*ProviderAttemptWindowAuthority, error) {
	if ctx == nil || len(signer) != ed25519.PrivateKeySize {
		return nil, errors.New("provider window authority owner or key absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value.Signature = nil
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) > providerAttemptAuthorityMaxBytes {
		return nil, protocol.ErrProviderAttemptsCapacity
	}
	var owned ProviderAttemptWindowAuthority
	if err := json.Unmarshal(raw, &owned); err != nil {
		return nil, err
	}
	owned.Signature = ed25519.Sign(signer, append([]byte(ProviderAttemptWindowAuthoritySchema+"\x00"), raw...))
	return &owned, ctx.Err()
}

// A bounded response contains original locators/cuts, never a computed verdict.
type ProviderAttemptWindowResponse struct {
	Authority ProviderAttemptWindowAuthority       `json:"authority"`
	Window    ProviderAttemptWindow                `json:"window"`
	Requests  []ProviderAttemptRequestLaneOriginal `json:"requests"`
}

// The complete sequence from original birth prevents an omitted older pending
// assignment from disappearing at the selected clock boundary.
type ProviderAttemptRequestLaneOriginal struct {
	Hotkey  [32]byte                       `json:"hotkey"`
	NoId    uint64                         `json:"no_id"`
	Windows []ProviderAttemptRequestWindow `json:"windows"`
}

// Captured object bytes keep cold admission independent of public availability.
type ProviderAttemptOriginalObject struct {
	Origin string `json:"origin"`
	Kind   string `json:"kind"`
	Hash   string `json:"hash"`
	Body   []byte `json:"body"`
}

// An original response is joined to its exact original signed request hash.
type ProviderAttemptOriginalResponse struct {
	RequestHash      [32]byte                                  `json:"request_hash"`
	Receipt          *protocol.ProviderAttemptReceipt          `json:"receipt,omitempty"`
	Closure          *protocol.ProviderAttemptRequestClosure   `json:"closure,omitempty"`
	ClosedUnreceived *protocol.ProviderAttemptClosedUnreceived `json:"closed_unreceived,omitempty"`
}

// This grammar is the portable evidence, not an export of trusted counters.
type ProviderAttemptOriginal struct {
	Schema        string                            `json:"schema"`
	AuthorityHash [32]byte                          `json:"authority_hash"`
	Response      ProviderAttemptWindowResponse     `json:"response"`
	Objects       []ProviderAttemptOriginalObject   `json:"objects"`
	Receipts      []ProviderAttemptOriginalResponse `json:"receipts"`
}

// Results remain local and must be joined to complete work, wallet and actual
// binding evidence by the caller before authenticating provider measurements.
type VerifiedProviderAttemptMeasurement struct {
	*VerifiedProviderAttemptWindow
	ReliabilityAMin uint64
	AuthorityHash   [32]byte
	OriginalHash    [32]byte
}

// Constructor closes every descriptor. The detached document is immutable;
// each Read/VerifyRetained owns its bounded scratch and network lifetime.
type ProviderAttemptAuthoritySource struct {
	authority ProviderAttemptAuthority
	hash      [32]byte
}

// The exact file is read through the existing protected reference owner.
func NewProviderAttemptAuthoritySource(ctx context.Context, reference ReleaseEvidenceV2File) (*ProviderAttemptAuthoritySource, error) {
	raw, err := ReadReleaseEvidenceV2File(ctx, reference, providerAttemptAuthorityMaxBytes)
	if err != nil {
		return nil, err
	}
	var authority ProviderAttemptAuthority
	if err := attemptStoreDecode(raw, &authority); err != nil {
		return nil, err
	}
	if err := authority.Registry.Domain.Validate(); err != nil {
		return nil, err
	}
	if authority.Schema != ProviderAttemptAuthoritySchema || authority.Registry.RootHash == ([32]byte{}) || authority.Registry.Signer == ([32]byte{}) || authority.WindowSigner == ([32]byte{}) || !filepath.IsAbs(authority.ScratchRoot) || filepath.Clean(authority.ScratchRoot) != authority.ScratchRoot || authority.MaxWindowBytes == 0 || authority.MaxWindowBytes > 32*1024*1024 || authority.MaxOriginalBytes < authority.MaxWindowBytes || authority.MaxOriginalBytes > 1024*1024*1024 || authority.MaxObjects == 0 || authority.MaxObjects > 1000000 || authority.MaxProviders == 0 || authority.MaxMetadataBytes == 0 || authority.MaxRecordBytes == 0 || authority.MaxRecords == 0 {
		return nil, errors.Join(protocol.ErrProviderAttemptsCapacity, errors.New("provider authority resource profile incomplete"))
	}
	if _, err := providerAttemptEndpoint(authority.WindowEndpoint); err != nil {
		return nil, err
	}
	return &ProviderAttemptAuthoritySource{authority: authority, hash: sha256.Sum256(raw)}, ctx.Err()
}

// Explicit endpoints never inherit proxy credentials or follow redirects.
func providerAttemptEndpoint(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("provider attempt endpoint is not an explicit HTTP(S) URL")
	}
	return parsed, nil
}

// Verify the independent signature before any selected key or origin is used.
func (self *ProviderAttemptAuthoritySource) verifyAuthority(ctx context.Context, value ProviderAttemptWindowAuthority) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value.Schema != ProviderAttemptWindowAuthoritySchema || value.AuthorityHash != self.hash || value.Domain != self.authority.Registry.Domain || value.RegistryHead == ([32]byte{}) || len(value.Signature) != ed25519.SignatureSize || uint64(len(value.Validators)) > self.authority.Registry.MaxOwners {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider window independent authority differs"))
	}
	signature := bytes.Clone(value.Signature)
	value.Signature = nil
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > providerAttemptAuthorityMaxBytes {
		return protocol.ErrProviderAttemptsCapacity
	}
	if !ed25519.Verify(ed25519.PublicKey(self.authority.WindowSigner[:]), append([]byte(ProviderAttemptWindowAuthoritySchema+"\x00"), raw...), signature) {
		return errors.Join(protocol.ErrProviderAttemptsIntegrity, errors.New("provider window independent signature differs"))
	}
	return ctx.Err()
}
