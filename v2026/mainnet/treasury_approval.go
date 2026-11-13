// Treasury approval commands draft the exact unsigned approval for one
// normalized validator config and assemble an external signature into the
// envelope the producer loads. Neither reads, creates nor holds a signing key.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

const treasuryApprovalInputSchema = "urnetwork-native-treasury-approval-input-v1"
const treasuryApprovalPlanSchema = "urnetwork-native-treasury-approval-plan-v1"
const treasuryApprovalEnvelopeSchema = "urnetwork-native-treasury-approval-envelope-v1"

// Printed beside every digest. The signer recomputes the digest from the
// approval file itself; it never signs a digest supplied to it.
const treasuryApprovalDerivation = "signing_digest is SHA-256 over signing_domain (the 47 ASCII bytes " +
	"urnetwork-native-treasury-approval-signature-v1 and one line feed, 0x0a) immediately followed by the approval JSON: " +
	"the approval file's bytes without its one final line feed, which are Go encoding/json output of the approval body " +
	"with no whitespace and every 32-byte value as an array of 32 decimal numbers. The approval key signs the 32 digest " +
	"bytes, not their hex, with Ed25519 (RFC 8032); the signature is 128 lowercase hex digits. Check: " +
	"printf 'urnetwork-native-treasury-approval-signature-v1\\n%s' \"$(cat APPROVAL_FILE)\" | shasum -a 256"

// Every signed native, proposal and production value is stated, mirroring the
// approval body. The command fixes the treasury schemas, the config supplies
// config_hash and policy-plan the treasury policy. 32-byte values are
// canonical lowercase 0x hex; lists keep their reviewed order.
type treasuryApprovalInput struct {
	Schema                  string                          `json:"schema"`
	NativeChain             string                          `json:"native_chain"`
	RuntimeReviewHash       string                          `json:"runtime_review_hash"`
	ValidatorHotkey         string                          `json:"validator_hotkey"`
	SubnetOwner             string                          `json:"subnet_owner"`
	OwnerHotkeys            []string                        `json:"owner_hotkeys"`
	FirstNativeEpoch        uint64                          `json:"first_native_epoch"`
	ValidFromNativeBlock    uint64                          `json:"valid_from_native_block"`
	ValidThroughNativeBlock uint64                          `json:"valid_through_native_block"`
	MaximumSubnetUids       uint32                          `json:"maximum_subnet_uids"`
	MaximumOwnedHotkeys     uint32                          `json:"maximum_owned_hotkeys"`
	Proposal                treasuryApprovalProposalInput   `json:"proposal"`
	Production              treasuryApprovalProductionInput `json:"production"`
}

// The successor proposal; its runtime pin names the exact native artifact.
type treasuryApprovalProposalInput struct {
	ParentPolicyHash string                       `json:"parent_policy_hash"`
	PolicyId         uint64                       `json:"policy_id"`
	EffectiveEpoch   uint64                       `json:"effective_epoch"`
	ProviderShare    protocol.Rational            `json:"provider_share"`
	Remainder        string                       `json:"remainder"`
	OwnerAllocation  string                       `json:"owner_allocation"`
	Runtime          treasuryApprovalRuntimeInput `json:"runtime"`
}

// Version keeps the runtime identity's established camel-case wire names.
type treasuryApprovalRuntimeInput struct {
	GenesisHash  string                      `json:"genesis_hash"`
	Netuid       uint16                      `json:"netuid"`
	Version      crv4.RuntimeVersionIdentity `json:"version"`
	CodeHash     string                      `json:"code_hash"`
	MetadataHash string                      `json:"metadata_hash"`
	SourceCommit string                      `json:"source_commit"`
}

// An omitted schedule profile would silently select the original schedule, so
// it must be stated. An omitted activation block keeps the original meaning.
type treasuryApprovalProductionInput struct {
	RuntimeCapability       string   `json:"runtime_capability"`
	EpochScheduleProfile    *string  `json:"epoch_schedule_profile"`
	ValidatorHotkeys        []string `json:"validator_hotkeys"`
	MaximumLastUpdateAge    uint64   `json:"maximum_last_update_age"`
	ValidThroughNativeEpoch uint64   `json:"valid_through_native_epoch"`
	ActivationNativeHash    string   `json:"activation_native_hash"`
	ActivationNativeBlock   *uint64  `json:"activation_native_block,omitempty"`
}

// The exact unsigned output of treasury policy-plan. The validator's own
// policy admission recomputes its hash; signed or altered plans are refused.
type treasuryPolicyPlanOutput struct {
	Policy      validator.TreasuryPolicy `json:"treasury_policy"`
	Hash        string                   `json:"treasury_policy_hash"`
	Observation string                   `json:"observation_hash"`
	Approved    bool                     `json:"approved"`
}

// The unsigned result names its exact inputs, bindings and digest. Only the
// approval file's bytes may be hashed by the signer; approved stays false.
type treasuryApprovalPlan struct {
	Schema             string                          `json:"schema"`
	Input              planFileReference               `json:"input"`
	Config             planFileReference               `json:"config"`
	PolicyPlan         planFileReference               `json:"policy_plan"`
	ApprovalKey        string                          `json:"approval_key"`
	ConfigHash         string                          `json:"config_hash"`
	TreasuryPolicyHash string                          `json:"treasury_policy_hash"`
	ObservationHash    string                          `json:"observation_hash"`
	ApprovalFile       validator.ReleaseEvidenceV2File `json:"approval_file"`
	SigningDomain      string                          `json:"signing_domain"`
	SigningDigest      string                          `json:"signing_digest"`
	SigningDerivation  string                          `json:"signing_derivation"`
	Approved           bool                            `json:"approved"`
}

// The selection is pasted into the validator config's treasury_approval.
type treasuryApprovalEnvelopeResult struct {
	Schema           string                                  `json:"schema"`
	TreasuryApproval validator.ReleaseTreasuryApprovalConfig `json:"treasury_approval"`
	ConfigHash       string                                  `json:"config_hash"`
	SigningDigest    string                                  `json:"signing_digest"`
}

// Only canonical spellings are parsed here. Every semantic rule belongs to the
// validator's admission, which runs on the complete body against the config.
func (self treasuryApprovalInput) approval(policy validator.TreasuryPolicy) (validator.TreasuryApproval, error) {
	var result validator.TreasuryApproval
	production := self.Production
	if self.Schema != treasuryApprovalInputSchema || self.OwnerHotkeys == nil || production.EpochScheduleProfile == nil ||
		production.ActivationNativeBlock != nil && *production.ActivationNativeBlock == 0 {
		return result, errors.New(`treasury approval input requires its schema, an explicit owner_hotkeys list ([] for none), an explicit epoch_schedule_profile ("" for the original schedule) and a nonzero activation_native_block when present`)
	}
	var parseErr error
	account := func(name, value string) [32]byte {
		if !rootCanonicalHash(value) {
			parseErr = errors.Join(parseErr, fmt.Errorf("treasury approval %s must be canonical nonzero lowercase 0x-prefixed 32-byte hex", name))
			return [32]byte{}
		}
		return treasuryApprovalKey(value)
	}
	accounts := func(name string, values []string) [][32]byte {
		keys := make([][32]byte, 0, len(values))
		for _, value := range values {
			keys = append(keys, account(name, value))
		}
		return keys
	}
	proposal, runtime := self.Proposal, self.Proposal.Runtime
	treasury := policy
	result = validator.TreasuryApproval{Schema: validator.TreasuryApprovalSchema,
		Proposal: validator.TreasuryProposal{Schema: validator.TreasuryProposalSchema, ParentPolicyHash: account("parent_policy_hash", proposal.ParentPolicyHash),
			PolicyId: proposal.PolicyId, EffectiveEpoch: proposal.EffectiveEpoch, ProviderShare: proposal.ProviderShare, Remainder: proposal.Remainder, OwnerAllocation: proposal.OwnerAllocation,
			Runtime: validator.OwnerRecycleRuntimePin{GenesisHash: account("genesis_hash", runtime.GenesisHash), Netuid: runtime.Netuid, Version: runtime.Version,
				CodeHash: account("code_hash", runtime.CodeHash), MetadataHash: account("metadata_hash", runtime.MetadataHash), SourceCommit: runtime.SourceCommit},
			Treasury: &treasury},
		NativeChain: self.NativeChain, RuntimeReviewHash: account("runtime_review_hash", self.RuntimeReviewHash),
		ValidatorHotkey: account("validator_hotkey", self.ValidatorHotkey), SubnetOwner: account("subnet_owner", self.SubnetOwner),
		OwnerHotkeys: accounts("owner_hotkeys", self.OwnerHotkeys), FirstNativeEpoch: self.FirstNativeEpoch,
		ValidFromNativeBlock: self.ValidFromNativeBlock, ValidThroughNativeBlock: self.ValidThroughNativeBlock,
		MaximumSubnetUids: self.MaximumSubnetUids, MaximumOwnedHotkeys: self.MaximumOwnedHotkeys,
		Production: &validator.TreasuryProductionApproval{Schema: validator.TreasuryProductionScope, RuntimeCapability: production.RuntimeCapability,
			EpochScheduleProfile: *production.EpochScheduleProfile, ValidatorHotkeys: accounts("validator_hotkeys", production.ValidatorHotkeys),
			MaximumLastUpdateAge: production.MaximumLastUpdateAge, ValidThroughNativeEpoch: production.ValidThroughNativeEpoch,
			ActivationNativeHash: account("activation_native_hash", production.ActivationNativeHash)}}
	if production.ActivationNativeBlock != nil {
		result.Production.ActivationNativeBlock = *production.ActivationNativeBlock
	}
	return result, parseErr
}

// Callers have already admitted the canonical nonzero spelling.
func treasuryApprovalKey(value string) [32]byte {
	raw, _ := hex.DecodeString(value[2:])
	return [32]byte(raw)
}

// The plan is checked against its own policy hash before any approval uses it.
func decodeTreasuryPolicyPlan(raw []byte) (treasuryPolicyPlanOutput, error) {
	var result treasuryPolicyPlanOutput
	if err := decodePlanJson(raw, &result); err != nil {
		return result, err
	}
	hash, err := result.Policy.Hash()
	if err != nil {
		return result, err
	}
	if result.Approved || result.Hash != "0x"+hex.EncodeToString(hash[:]) || !planSha256(result.Observation) {
		return result, errors.New("treasury policy plan must be unsigned policy-plan output whose hash matches its policy")
	}
	return result, nil
}

// The draft is bound to the config's exact loader hash and admitted by the
// validator's own rules before its file is written for the external signer.
func treasuryApprovalPlanCommand(ctx context.Context, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("treasury approval-plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "", "explicit native, proposal and production approval values")
	configPath := flags.String("config", "", "normalized schema-3 validator config at the absolute path the validator loads")
	configHash := flags.String("config-sha256", "", "independent config file SHA-256")
	planPath := flags.String("policy-plan", "", "unsigned treasury policy-plan output")
	planHash := flags.String("policy-plan-sha256", "", "independent policy-plan file SHA-256")
	key := flags.String("approval-key", "", "independent Ed25519 approval public key the config pins")
	outPath := flags.String("out", "", "new private approval file for the external signer")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *inputPath == "" || *configPath == "" || *planPath == "" || *outPath == "" ||
		!planSha256(*configHash) || !planSha256(*planHash) || !rootCanonicalHash(*key) {
		return nil, errors.New("treasury approval-plan requires --input, --config with --config-sha256, --policy-plan with --policy-plan-sha256, --approval-key and --out")
	}
	for _, input := range []string{*inputPath, *configPath, *planPath} {
		if input == *outPath {
			return nil, errors.New("treasury approval output overlaps an input")
		}
	}
	raw, inputHash, err := readBootstrapRootFile(ctx, *inputPath, treasuryDescriptorLimit)
	if err != nil {
		return nil, err
	}
	var input treasuryApprovalInput
	if err := decodePlanJson(raw, &input); err != nil {
		return nil, err
	}
	configRaw, actual, err := readBootstrapRootFile(ctx, *configPath, 2*1024*1024)
	if err != nil || actual != *configHash {
		return nil, errors.Join(errors.New("treasury approval config differs from its independent pin"), err)
	}
	planRaw, actual, err := readBootstrapRootFile(ctx, *planPath, treasuryDescriptorLimit)
	if err != nil || actual != *planHash {
		return nil, errors.Join(errors.New("treasury policy plan differs from its independent pin"), err)
	}
	plan, err := decodeTreasuryPolicyPlan(planRaw)
	if err != nil {
		return nil, err
	}
	approval, err := input.approval(plan.Policy)
	if err != nil {
		return nil, err
	}
	draft, err := validator.DraftTreasuryApproval(*configPath, configRaw, treasuryApprovalKey(*key), approval)
	if err != nil {
		return nil, err
	}
	reference, err := writeTreasuryApprovalFile(ctx, *outPath, draft.Encoded)
	if err != nil {
		return nil, err
	}
	return treasuryApprovalPlan{Schema: treasuryApprovalPlanSchema, Input: planFileReference{Path: *inputPath, Sha256: inputHash},
		Config: planFileReference{Path: *configPath, Sha256: *configHash}, PolicyPlan: planFileReference{Path: *planPath, Sha256: *planHash},
		ApprovalKey: *key, ConfigHash: "0x" + hex.EncodeToString(draft.Approval.ConfigHash[:]), TreasuryPolicyHash: plan.Hash, ObservationHash: plan.Observation,
		ApprovalFile: reference, SigningDomain: validator.TreasuryApprovalSignatureDomain, SigningDigest: "0x" + hex.EncodeToString(draft.Digest[:]),
		SigningDerivation: treasuryApprovalDerivation}, nil
}

// The signature must verify over the digest recomputed from the approval file
// before the envelope exists. The printed selection names its exact bytes.
func treasuryApprovalEnvelopeCommand(ctx context.Context, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("treasury approval-envelope", flag.ContinueOnError)
	flags.SetOutput(stderr)
	approvalPath := flags.String("approval", "", "approval file written by approval-plan")
	signature := flags.String("signature", "", "128 lowercase hex Ed25519 signature over the approval's signing digest")
	key := flags.String("approval-key", "", "independent Ed25519 approval public key the config pins")
	outPath := flags.String("out", "", "new private envelope file for treasury_approval.approval")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *approvalPath == "" || *signature == "" || *outPath == "" ||
		*outPath == *approvalPath || !rootCanonicalHash(*key) {
		return nil, errors.New("treasury approval-envelope requires --approval, --signature, --approval-key and a separate --out")
	}
	raw, _, err := readBootstrapRootFile(ctx, *approvalPath, validator.MaximumTreasuryApprovalBytes)
	if err != nil {
		return nil, err
	}
	envelope, draft, err := validator.AssembleTreasuryApprovalEnvelope(raw, treasuryApprovalKey(*key), *signature)
	if err != nil {
		return nil, err
	}
	reference, err := writeTreasuryApprovalFile(ctx, *outPath, envelope)
	if err != nil {
		return nil, err
	}
	return treasuryApprovalEnvelopeResult{Schema: treasuryApprovalEnvelopeSchema,
		TreasuryApproval: validator.ReleaseTreasuryApprovalConfig{Approval: reference, Signer: *key},
		ConfigHash:       "0x" + hex.EncodeToString(draft.Approval.ConfigHash[:]), SigningDigest: "0x" + hex.EncodeToString(draft.Digest[:])}, nil
}

// A reviewed or signed artifact is created once and never replaced, even by
// identical bytes. The validator's immutable writer owns private creation,
// file and directory fsync and the read-back the producer loader performs.
func writeTreasuryApprovalFile(ctx context.Context, path string, encoded []byte) (validator.ReleaseEvidenceV2File, error) {
	if !bootstrapRootAbsolutePath(path) {
		return validator.ReleaseEvidenceV2File{}, errors.New("treasury approval output requires a canonical absolute path")
	}
	if err := validator.ValidateReleaseEvidenceV2Path(path); err != nil {
		return validator.ReleaseEvidenceV2File{}, err
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return validator.ReleaseEvidenceV2File{}, errors.Join(errors.New("treasury approval output already exists and is never replaced"), err)
	}
	return validator.WriteReleaseEvidenceV2File(ctx, path, encoded, validator.MaximumTreasuryApprovalBytes)
}
