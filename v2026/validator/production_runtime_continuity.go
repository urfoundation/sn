// Continuity review separates an independently delegated semantic verifier from
// owned-Rpc interface observation. Neither this report nor a certificate installs
// production signing authority; the original exact selection remains in force.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const ProductionRuntimeContinuityPolicySchema = "urnetwork-validator-runtime-continuity-policy-v1"
const ProductionRuntimeSemanticRequestSchema = "urnetwork-validator-runtime-semantic-request-v1"
const ProductionRuntimeSemanticCertificateSchema = "urnetwork-validator-runtime-semantic-certificate-v1"
const maximumProductionRuntimeContinuityBytes = 64 * 1024

// Every domain is mandatory. Metadata establishes encoding, never these economic
// or execution semantics. A qualified verifier must evaluate the exact old/new
// Wasm and source/build inputs under the independently approved rules.
func productionRuntimeSemanticDomains() []string {
	return []string{"consumed-calls-storage-events-and-runtime-apis", "signed-extensions-fees-nonces-and-mortality",
		"atomic-source-commitment-and-dispatch", "crv4-timelock-domain-and-role", "native-epoch-clock-and-schedule",
		"stake-eligibility-and-emission-denominator", "frontier-native-evm-mapping"}
}

// Delegation is additive to exact original authority. It cannot extend its
// window, change custody, policy or routes, or let a node approve its own Wasm.
type ProductionRuntimeContinuityPolicy struct {
	Schema                  string                       `json:"schema"`
	OriginalApprovalSha256  [32]byte                     `json:"original_approval_sha256"`
	OriginalConfigHash      [32]byte                     `json:"original_config_hash"`
	GenesisHash             [32]byte                     `json:"genesis_hash"`
	BaseArtifact            crv4.RuntimeArtifactIdentity `json:"base_artifact"`
	InterfaceProfile        string                       `json:"interface_profile"`
	VerifierKey             [32]byte                     `json:"verifier_key_ed25519"`
	VerifierBuildSha256     [32]byte                     `json:"verifier_build_sha256"`
	SemanticRulesSha256     [32]byte                     `json:"semantic_rules_sha256"`
	SemanticDomains         []string                     `json:"semantic_domains"`
	ValidFromNativeBlock    uint64                       `json:"valid_from_native_block"`
	ValidThroughNativeBlock uint64                       `json:"valid_through_native_block"`
}

// The key comes only from the original authenticated config, never this file.
type ProductionRuntimeContinuityEnvelope struct {
	Policy    ProductionRuntimeContinuityPolicy `json:"policy"`
	Signature string                            `json:"signature_ed25519"`
}

// The independent verifier receives an exact artifact and source/build evidence,
// not a runtime version hint. Its output must cover this entire immutable request.
type ProductionRuntimeSemanticRequest struct {
	Schema                  string                       `json:"schema"`
	PolicySha256            [32]byte                     `json:"policy_sha256"`
	Artifact                crv4.RuntimeArtifactIdentity `json:"artifact"`
	SourceCommit            string                       `json:"source_commit"`
	SourceBuildEvidenceHash [32]byte                     `json:"source_build_evidence_sha256"`
	ValidFromNativeBlock    uint64                       `json:"valid_from_native_block"`
	ValidThroughNativeBlock uint64                       `json:"valid_through_native_block"`
}

// A transport implementation cannot grant authority by returning a boolean.
// Consumers must authenticate the signed output below and run their own exact
// block/interface checks. No qualified semantic-verifier implementation ships.
type ProductionRuntimeSemanticVerifier interface {
	VerifyProductionRuntime(context.Context, ProductionRuntimeContinuityPolicy, ProductionRuntimeSemanticRequest) ([]byte, error)
}

// The independently signed output records executable/rules provenance and all
// covered domains. Evidence hashes identify retained proof material; signatures
// authenticate the assertion, not its truth or independent replay of that proof.
type ProductionRuntimeSemanticResult struct {
	Schema              string                           `json:"schema"`
	Request             ProductionRuntimeSemanticRequest `json:"request"`
	VerifierBuildSha256 [32]byte                         `json:"verifier_build_sha256"`
	SemanticRulesSha256 [32]byte                         `json:"semantic_rules_sha256"`
	VerifiedDomains     []string                         `json:"verified_domains"`
	EvidenceSha256      [32]byte                         `json:"evidence_sha256"`
	Compatible          bool                             `json:"compatible"`
}

// Only the envelope's independent verifier key may authenticate this output.
type ProductionRuntimeSemanticCertificate struct {
	Result    ProductionRuntimeSemanticResult `json:"result"`
	Signature string                          `json:"signature_ed25519"`
}

// Review output exposes no metadata pointer, Chain view or opaque signing proof.
// Original transactions, signed sidecars, runtime histories and custody stay put.
type ProductionRuntimeContinuityInspection struct {
	PolicySha256                     [32]byte                     `json:"policy_sha256"`
	CertificateSha256                [32]byte                     `json:"certificate_sha256"`
	OriginalApprovalSha256           [32]byte                     `json:"original_approval_sha256"`
	OriginalConfigHash               [32]byte                     `json:"original_config_hash"`
	Artifact                         crv4.RuntimeArtifactIdentity `json:"artifact"`
	NativeHash                       types.Hash                   `json:"native_hash"`
	NativeNumber                     uint64                       `json:"native_number"`
	CheckedThroughNativeHash         types.Hash                   `json:"checked_through_native_hash"`
	CheckedThroughNativeNumber       uint64                       `json:"checked_through_native_number"`
	InterfaceProfile                 string                       `json:"interface_profile"`
	EvidenceSha256                   [32]byte                     `json:"evidence_sha256"`
	SemanticCertificateAuthenticated bool                         `json:"semantic_certificate_authenticated"`
	SemanticsIndependentlyReplayed   bool                         `json:"semantics_independently_replayed"`
	ProductionSelectionInstalled     bool                         `json:"production_selection_installed"`
	SigningAuthority                 bool                         `json:"signing_authority"`
	ObservationAuthority             string                       `json:"observation_authority"`
}

// External signers use domain-separated canonical values; no private key is read.
func productionRuntimeContinuityMessage(domain string, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maximumProductionRuntimeContinuityBytes {
		return nil, errors.Join(errors.New("runtime continuity message is invalid or oversized"), err)
	}
	hash := sha256.Sum256(append([]byte(domain+"\n"), raw...))
	return hash[:], nil
}

// This message selects a proposal; signing it does not install a producer route.
func (self ProductionRuntimeContinuityPolicy) SigningMessage() ([]byte, error) {
	return productionRuntimeContinuityMessage(ProductionRuntimeContinuityPolicySchema, self)
}

// Output provenance and the whole request participate in the independent signature.
func (self ProductionRuntimeSemanticResult) SigningMessage() ([]byte, error) {
	return productionRuntimeContinuityMessage(ProductionRuntimeSemanticCertificateSchema, self)
}

// Bounded, unambiguous decoding precedes signature verification or any Rpc read.
func decodeProductionRuntimeContinuity(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > maximumProductionRuntimeContinuityBytes {
		return errors.New("runtime continuity document is empty or oversized")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("runtime continuity document has trailing JSON")
	}
	return nil
}

// Canonical nonzero hashes avoid aliases in authority and evidence references.
func productionRuntimeContinuityHash(value string) bool {
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	return err == nil && len(raw) == 32 && value == "0x"+hex.EncodeToString(raw) && !bytes.Equal(raw, make([]byte, 32))
}

// Verifies original independent approval and delegated output before observing
// the candidate. A changed artifact at the same version requires a new explicit
// incompatible-profile policy; neither a node nor this certificate can relax it.
func verifyProductionRuntimeContinuity(cfg *ReleaseConfig, policyRaw, certificateRaw []byte) (*ProductionRuntimeContinuityEnvelope, *ProductionRuntimeSemanticCertificate, error) {
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return nil, nil, err
	}
	if cfg.ownerRecycleProduction.historicalOnly {
		return nil, nil, errors.New("historical production authority cannot propose current continuity")
	}
	if err := validateReleaseProductionRuntimeHistory(cfg); err != nil {
		return nil, nil, err
	}
	if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
		return nil, nil, err
	}
	var envelope ProductionRuntimeContinuityEnvelope
	var certificate ProductionRuntimeSemanticCertificate
	if err := decodeProductionRuntimeContinuity(policyRaw, &envelope); err != nil {
		return nil, nil, err
	}
	if err := decodeProductionRuntimeContinuity(certificateRaw, &certificate); err != nil {
		return nil, nil, err
	}
	policy := envelope.Policy
	key, err := canonicalAttemptHex32("original runtime continuity approver", productionEconomicSelection(cfg).Signer, false)
	if err != nil {
		return nil, nil, err
	}
	verify := func(key []byte, message []byte, signature string) bool {
		raw, err := hex.DecodeString(signature)
		return err == nil && signature == hex.EncodeToString(raw) && len(raw) == ed25519.SignatureSize && ed25519.Verify(key, message, raw)
	}
	message, err := policy.SigningMessage()
	if err != nil || !verify(key[:], message, envelope.Signature) {
		return nil, nil, errors.New("runtime continuity independent policy signature differs")
	}
	if policy.Schema != ProductionRuntimeContinuityPolicySchema || policy.OriginalApprovalSha256 != sha256.Sum256(cfg.ownerRecycleProduction.encoded) ||
		policy.OriginalConfigHash != approved.Approval.ConfigHash || policy.GenesisHash != approved.Approval.Proposal.Runtime.GenesisHash ||
		policy.BaseArtifact != releaseNativeRuntimeIdentity(cfg) || policy.InterfaceProfile != crv4.ValidatorProducerRuntimeProfile ||
		policy.VerifierKey == ([32]byte{}) || policy.VerifierKey == key || policy.VerifierBuildSha256 == ([32]byte{}) || policy.SemanticRulesSha256 == ([32]byte{}) ||
		!slices.Equal(policy.SemanticDomains, productionRuntimeSemanticDomains()) || policy.ValidFromNativeBlock < approved.Approval.ValidFromNativeBlock ||
		policy.ValidThroughNativeBlock > approved.Approval.ValidThroughNativeBlock || policy.ValidThroughNativeBlock < policy.ValidFromNativeBlock {
		return nil, nil, errors.New("runtime continuity policy differs from original authority or semantic scope")
	}
	signers := append(append([][32]byte{approved.Approval.SubnetOwner}, approved.Approval.OwnerHotkeys...), approved.Approval.Production.ValidatorHotkeys...)
	for _, signer := range signers {
		if policy.VerifierKey == signer || key == signer {
			return nil, nil, errors.New("runtime continuity node cannot approve its own artifact")
		}
	}
	result, request := certificate.Result, certificate.Result.Request
	message, err = result.SigningMessage()
	if err != nil || !verify(policy.VerifierKey[:], message, certificate.Signature) {
		return nil, nil, errors.New("runtime continuity independent semantic signature differs")
	}
	if result.Schema != ProductionRuntimeSemanticCertificateSchema || request.Schema != ProductionRuntimeSemanticRequestSchema ||
		request.PolicySha256 != sha256.Sum256(policyRaw) || result.VerifierBuildSha256 != policy.VerifierBuildSha256 || result.SemanticRulesSha256 != policy.SemanticRulesSha256 ||
		!slices.Equal(result.VerifiedDomains, policy.SemanticDomains) || !result.Compatible || result.EvidenceSha256 == ([32]byte{}) ||
		request.SourceBuildEvidenceHash == ([32]byte{}) || request.ValidFromNativeBlock < policy.ValidFromNativeBlock || request.ValidThroughNativeBlock > policy.ValidThroughNativeBlock ||
		request.ValidThroughNativeBlock < request.ValidFromNativeBlock {
		return nil, nil, errors.New("runtime continuity semantic output or provenance differs")
	}
	source, err := hex.DecodeString(request.SourceCommit)
	if err != nil || len(source) != 20 || request.SourceCommit != hex.EncodeToString(source) || bytes.Equal(source, make([]byte, 20)) {
		return nil, nil, errors.New("runtime continuity source-to-Wasm evidence is incomplete")
	}
	base, candidate := policy.BaseArtifact, request.Artifact
	if candidate.Version.SpecName != base.Version.SpecName || candidate.Version.SpecVersion <= base.Version.SpecVersion ||
		candidate.Version.TransactionVersion != base.Version.TransactionVersion || candidate.Version.StateVersion != base.Version.StateVersion ||
		!productionRuntimeContinuityHash(candidate.CodeHash) || !productionRuntimeContinuityHash(candidate.MetadataHash) || strings.EqualFold(candidate.CodeHash, base.CodeHash) {
		return nil, nil, errors.New("runtime continuity candidate is outside the supported successor profile")
	}
	return &envelope, &certificate, nil
}

// Reviews a certificate against the actual owned route and immutable selected
// finalized block. It deliberately never binds the candidate, saves a revision,
// or changes production selection. Semantic proof replay and a qualified durable
// selection owner remain required before any future producer route can use it.
func InspectProductionRuntimeContinuityContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash, policyRaw, certificateRaw []byte) (*ProductionRuntimeContinuityInspection, error) {
	if ctx == nil {
		return nil, errors.New("runtime continuity context is absent")
	}
	policyRaw, certificateRaw = bytes.Clone(policyRaw), bytes.Clone(certificateRaw)
	policy, certificate, err := verifyProductionRuntimeContinuity(cfg, policyRaw, certificateRaw)
	if err != nil {
		return nil, err
	}
	if native == nil || native.API == nil || native.API.Client == nil || native.ProvisionalRuntimeCompatibilityEnabled() ||
		!slices.Contains(cfg.Substrate, native.API.Client.URL()) || native.GenesisHash != types.Hash(policy.Policy.GenesisHash) {
		return nil, errors.New("runtime continuity requires the original non-provisional owned route")
	}
	ctx, cancel := context.WithTimeout(ctx, productionSteeringReadTimeout)
	defer cancel()
	ctx = withRuntimeFinalityOwner(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var finality runtimeFinalityObservation
	return crv4.ReadRuntimeObservationContext(ctx, native, func(ctx context.Context) (*ProductionRuntimeContinuityInspection, error) {
		approved, _ := ownerRecycleProductionApproval(cfg)
		if _, err := readRuntimeNetworkIdentity(ctx, native, approved.Approval.NativeChain, native.GenesisHash); err != nil {
			return nil, err
		}
		head, err := readRuntimeFinalityWitness(ctx, native)
		if err != nil {
			return nil, err
		}
		if block == (types.Hash{}) {
			block, err = crv4.SelectFinalityReadBlockContext(ctx, native, block, head.hash)
			if err != nil {
				return nil, err
			}
		}
		number, _, err := native.ReceiptHeaderAtContext(ctx, block)
		if err != nil {
			return nil, err
		}
		request := certificate.Result.Request
		if number == 0 || number < request.ValidFromNativeBlock || number > request.ValidThroughNativeBlock {
			return nil, errors.New("runtime continuity block is outside its finalized certificate window")
		}
		selectedBlock := runtimeFinalityWitness{hash: block, number: number}
		if err := finality.check(ctx, native, head, selectedBlock); err != nil {
			return nil, err
		}
		artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, native, block, request.Artifact)
		if err != nil {
			return nil, fmt.Errorf("runtime continuity exact candidate: %w", err)
		}
		if err := crv4.ValidateValidatorProducerRuntimeArtifactContext(ctx, native, artifact); err != nil {
			return nil, err
		}
		if err := finality.close(ctx, native, selectedBlock); err != nil {
			return nil, err
		}
		latest := finality.latest
		if _, _, err := verifyProductionRuntimeContinuity(cfg, policyRaw, certificateRaw); err != nil {
			return nil, err
		}
		return &ProductionRuntimeContinuityInspection{PolicySha256: sha256.Sum256(policyRaw), CertificateSha256: sha256.Sum256(certificateRaw),
			OriginalApprovalSha256: policy.Policy.OriginalApprovalSha256, OriginalConfigHash: policy.Policy.OriginalConfigHash, Artifact: request.Artifact,
			NativeHash: block, NativeNumber: number, CheckedThroughNativeHash: latest.hash, CheckedThroughNativeNumber: latest.number,
			InterfaceProfile: policy.Policy.InterfaceProfile, EvidenceSha256: certificate.Result.EvidenceSha256,
			SemanticCertificateAuthenticated: true, ObservationAuthority: "owned-rpc-assertion; signed semantic assertion is not independently replayed proof"}, nil
	})
}
