//go:build linux || darwin

// Successor capsules join signed approval, an authenticated owner census and
// the original compact provider transcript without changing either old wire.
// They are replayable proposed decisions, never transaction or launch authority.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const ownerRecycleMeasurementSchema = "urnetwork-owner-recycle-measurement-v1"
const ownerRecycleDecisionIntentSchema = "urnetwork-owner-recycle-decision-intent-v1"
const ownerRecycleOperatorMeasurementSchema = "urnetwork-owner-recycle-measurement-v2"
const ownerRecycleOperatorIntentSchema = "urnetwork-owner-recycle-decision-intent-v2"

// The only current state is a verified arithmetic proposal with missing launch
// admission. No ready or executable state is defined by this wire version.
type OwnerRecycleDecisionStatus string

const OwnerRecycleDecisionBlocked OwnerRecycleDecisionStatus = "verified_proposal_blocked"

// Only the real retained-approval and native observer can construct this owner.
// Its detached bytes are private and cannot be replaced by candidate booleans.
// It grants this one exact decision's replay, not a reusable activation lease.
type OwnerRecycleMeasurementAuthority struct {
	approval             []byte
	census               []byte
	expected             ReleaseMeasurementV2Decision
	config               ReleaseConfig
	observation          OwnerRecycleAdmissionObservation
	operatorEvidence     []byte
	operatorProviderHash string
}

// The exact and quantized rows are outputs reconstructed from provider proofs.
// The signed error is a reduced RatString because it can be negative.
type OwnerRecycleMeasuredRow struct {
	Uids               []uint16       `json:"uids"`
	Scores             []RationalJSON `json:"scores"`
	Values             []uint16       `json:"values"`
	OwnerUids          []uint16       `json:"owner_uids"`
	MaskedOwnerUids    []uint16       `json:"masked_owner_uids"`
	WireProviderShare  RationalJSON   `json:"wire_provider_share"`
	ProviderShareError string         `json:"provider_share_error"`
}

// Byte fields preserve their original canonical representation, including the
// final newline. Base64 wrapping cannot reinterpret V1/V2 as successor evidence.
type OwnerRecycleMeasurement struct {
	Schema              string                     `json:"schema"`
	Status              OwnerRecycleDecisionStatus `json:"status"`
	Approval            []byte                     `json:"approval"`
	Census              []byte                     `json:"census"`
	ProviderMeasurement []byte                     `json:"provider_measurement"`
	OperatorEvidence    []byte                     `json:"operator_evidence,omitempty"`
	Row                 OwnerRecycleMeasuredRow    `json:"row"`
}

// This distinct unsigned intent cannot be passed to the existing IntentStore
// or native submission API. Its content address binds all original evidence.
type OwnerRecycleDecisionIntent struct {
	Schema                  string                       `json:"schema"`
	Status                  OwnerRecycleDecisionStatus   `json:"status"`
	ActivationReady         bool                         `json:"activation_ready"`
	NativeOutcomeVerified   bool                         `json:"native_outcome_verified"`
	CapsuleHash             string                       `json:"capsule_hash"`
	ApprovalHash            string                       `json:"approval_hash"`
	ProposalHash            [32]byte                     `json:"proposal_hash"`
	CensusHash              string                       `json:"census_hash"`
	ProviderMeasurementHash string                       `json:"provider_measurement_hash"`
	OperatorEvidenceHash    string                       `json:"operator_evidence_hash,omitempty"`
	Decision                ReleaseMeasurementV2Decision `json:"decision"`
	Row                     OwnerRecycleMeasuredRow      `json:"row"`
	Blockers                []string                     `json:"activation_blockers"`
}

// Authenticates the retained approval and the exact requested canonical census,
// including historical replay after the finalized head advances. Caller config
// must be immutable for admission; it is detached before the first RPC callback.
func ObserveOwnerRecycleMeasurementAuthority(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, expected ReleaseMeasurementV2Decision) (*OwnerRecycleMeasurementAuthority, error) {
	if ctx == nil {
		return nil, errors.New("owner-recycle measurement context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateOwnerRecycleApprovalSelection(cfg); err != nil {
		return nil, err
	}
	remaining := uint64(maximumOwnerRecycleConfigBytes)
	wireConfig := *cfg
	wireConfig.ownerRecycleProduction, wireConfig.productionRuntimeHistory, wireConfig.productionAuthorityHistory = nil, nil, nil
	wireConfig.mainnetRuntimeHistory, wireConfig.historyAdoptionV2 = nil, nil
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(&wireConfig), &remaining); err != nil {
		return nil, fmt.Errorf("owner-recycle measurement configuration bound: %w", err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil || len(raw) > maximumOwnerRecycleConfigBytes {
		return nil, errors.Join(errors.New("owner-recycle measurement configuration is oversized"), err)
	}
	var owned ReleaseConfig
	if err := json.Unmarshal(raw, &owned); err != nil {
		return nil, err
	}
	// Private production authority survives ownership copies only after its
	// complete public configuration has been independently matched.
	if isOwnerRecycleProductionConfig(cfg) {
		if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
			return nil, err
		}
		owned.ownerRecycleProduction = cfg.ownerRecycleProduction
		owned.productionRuntimeHistory = cfg.productionRuntimeHistory
		owned.productionAuthorityHistory = cfg.productionAuthorityHistory
	}
	envelope, err := readRetainedOwnerRecycleApproval(ctx, &owned)
	if err != nil {
		return nil, err
	}
	blockHash, err := parseReleaseHex32("owner-recycle decision native hash", expected.NativeSnapshotHash, false)
	if err != nil {
		return nil, err
	}
	approval := envelope.Approval
	if expected.DeploymentID != owned.DeploymentID || expected.ChainID != 964 || expected.GenesisHash != owned.GenesisHash ||
		expected.Coordinator != owned.Coordinator || expected.SettlementVault != owned.SettlementVault || expected.ValidatorID != owned.ValidatorID ||
		expected.Netuid != owned.Netuid || expected.PolicyHash != owned.PolicyHash || !ownerRecycleDecisionEpochApproved(&approval, expected.SubnetEpoch) ||
		expected.SettlementEpoch < approval.Proposal.EffectiveEpoch {
		return nil, errors.New("owner-recycle measurement differs from the approved first decision and unchanged parent domain")
	}
	observation, err := ObserveOwnerRecycleAdmissionAt(ctx, &owned, native, blockHash)
	if err != nil {
		return nil, err
	}
	if observation.Snapshot.FinalizedNumber != expected.NativeSnapshotBlock || observation.NativeEpoch != expected.SubnetEpoch ||
		observation.Snapshot.FinalizedHash != blockHash {
		return nil, errors.New("owner-recycle measurement and owner census do not share the exact native decision")
	}
	selfFound := false
	for _, registration := range observation.Snapshot.Registrations {
		if registration.Uid == expected.SelfUID {
			selfFound = registration.Hotkey == approval.ValidatorHotkey
		}
	}
	if !selfFound {
		return nil, errors.New("owner-recycle measurement self UID is not the independently approved validator hotkey")
	}
	approvalBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	censusBytes, err := json.Marshal(observation)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &OwnerRecycleMeasurementAuthority{approval: append(approvalBytes, '\n'), census: append(censusBytes, '\n'), expected: expected, config: owned, observation: *observation}, nil
}

// Both input and output use the caller's existing finite measurement allowance.
// No implicit larger envelope budget is created for the wrapped evidence.
func ownerRecycleMeasurementLimit(ctx context.Context, authority *OwnerRecycleMeasurementAuthority, options ReleaseMeasurementV2Options) (uint64, error) {
	if ctx == nil || authority == nil || len(authority.approval) == 0 || len(authority.census) == 0 {
		return 0, errors.New("owner-recycle measurement lacks a real authenticated decision owner")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if options.MaxArtifactBytes == 0 || options.MaxArtifactBytes > maxReleaseMeasurementArtifactBytes || options.MaxControlBytes == 0 || options.MaxControlBytes > maxReleaseMeasurementArtifactBytes {
		return 0, errors.New("owner-recycle measurement allowance is absent or oversized")
	}
	if options.Expected != authority.expected || !reflect.DeepEqual(options.Policy, authority.config.Policy) ||
		!slices.Equal(options.ControlledNOIDs, authority.config.ControlledNOIDs) ||
		(options.ReplayPolicy != nil && (!isOwnerRecycleProductionConfig(&authority.config) || !reflect.DeepEqual(*options.ReplayPolicy, options.Policy))) {
		return 0, errors.New("owner-recycle measurement replay authority differs from its exact approved decision")
	}
	if len(authority.config.Operators) < authority.config.Policy.Safety.MinimumHealthyNOCount || len(authority.config.Operators) != len(options.Operators) {
		return 0, errors.New("owner-recycle measurement operator census differs from the signed configuration")
	}
	seen := make(map[uint64]bool, len(authority.config.Operators))
	for _, operator := range authority.config.Operators {
		if _, exists := options.Operators[operator.NoID]; !exists || operator.NoID == 0 || seen[operator.NoID] {
			return 0, errors.New("owner-recycle measurement operator identity differs from the signed configuration")
		}
		seen[operator.NoID] = true
	}
	return min(options.MaxArtifactBytes, options.MaxControlBytes), nil
}

// Uses only fully replayed provider scores and the real observer's registered
// owners. No live-validator or healthy-operator claims are fabricated to satisfy
// the preview planner's separate admission minimums.
func deriveOwnerRecycleMeasuredRow(authority *OwnerRecycleMeasurementAuthority, artifact *ReleaseMeasurementArtifact, verified *VerifiedReleaseMeasurement) (OwnerRecycleMeasuredRow, error) {
	empty := OwnerRecycleMeasuredRow{}
	registrations := make(map[uint16]OwnerRecycleRegistration, len(authority.observation.Snapshot.Registrations))
	for _, registration := range authority.observation.Snapshot.Registrations {
		registrations[registration.Uid] = registration
	}
	owners := make(map[uint16]bool, len(authority.observation.RecognizedOwners))
	masked := make(map[uint16]bool, len(verified.MaskedUIDs))
	for _, uid := range verified.MaskedUIDs {
		masked[uid] = true
	}
	if !masked[artifact.SelfUID] {
		return empty, errors.New("owner-recycle measured decision omitted its self mask")
	}
	row := &OwnerRecyclePreview{ProposalHash: authority.observation.ProposalHash, FinalizedHash: authority.observation.Snapshot.FinalizedHash}
	for _, owner := range authority.observation.RecognizedOwners {
		owners[owner.Uid] = true
		if masked[owner.Uid] {
			row.MaskedOwnerUids = append(row.MaskedOwnerUids, owner.Uid)
		} else {
			row.OwnerUids = append(row.OwnerUids, owner.Uid)
		}
	}
	checkProvider := func(uid uint16, encodedHotkey string) error {
		hotkey, err := parseReleaseHex32("owner-recycle measured provider hotkey", encodedHotkey, false)
		registration, exists := registrations[uid]
		if err != nil || !exists || registration.Hotkey != hotkey || owners[uid] {
			return errors.Join(errors.New("owner-recycle measured provider differs from the native census or overlaps an owner"), err)
		}
		return nil
	}
	for _, pool := range artifact.Pools {
		if err := checkProvider(pool.UID, pool.PoolHotkey); err != nil {
			return empty, err
		}
	}
	for _, binding := range artifact.Bindings {
		if binding.LiveUIDFound {
			if err := checkProvider(binding.LiveUID, binding.Hotkey); err != nil {
				return empty, err
			}
		}
	}
	if err := completeOwnerRecycleRow(row, artifact.Policy, verified.UIDs, verified.Scores, owners); err != nil {
		return empty, err
	}
	if len(row.Uids) < int(authority.observation.MinimumAllowedWeights) {
		return empty, errors.New("owner-recycle quantized row is below the observed native minimum weights")
	}
	scores, err := rationalJSON(row.Scores)
	if err != nil {
		return empty, err
	}
	share, err := encodeRationalJSON(row.WireProviderShare)
	if err != nil {
		return empty, err
	}
	return OwnerRecycleMeasuredRow{Uids: row.Uids, Scores: scores, Values: row.WireValues, OwnerUids: row.OwnerUids,
		MaskedOwnerUids: row.MaskedOwnerUids, WireProviderShare: share, ProviderShareError: row.ProviderShareError.RatString()}, nil
}

// One full original V2 proof replay derives all scores before the proposed
// successor row is assembled. Replayed input bytes are detached before readers.
func buildOwnerRecycleMeasurement(ctx context.Context, authority *OwnerRecycleMeasurementAuthority, providerBytes []byte, options ReleaseMeasurementV2Options) (*OwnerRecycleMeasurement, error) {
	limit, err := ownerRecycleMeasurementLimit(ctx, authority, options)
	if err != nil {
		return nil, err
	}
	if uint64(len(providerBytes))+uint64(len(authority.approval))+uint64(len(authority.census))+uint64(len(authority.operatorEvidence)) > limit {
		return nil, errors.New("owner-recycle measurement evidence exceeds its complete allowance")
	}
	providerBytes = bytes.Clone(providerBytes)
	if len(authority.operatorEvidence) != 0 && ReleaseMeasurementContentHash(providerBytes) != authority.operatorProviderHash {
		return nil, errors.New("owner-recycle operator evidence belongs to different exact provider bytes")
	}
	artifact, verified, err := DecodeReleaseMeasurementArtifactV2(ctx, providerBytes, options)
	if err != nil {
		return nil, err
	}
	row, err := deriveOwnerRecycleMeasuredRow(authority, artifact, verified.Decision)
	if err != nil {
		return nil, err
	}
	schema := ownerRecycleMeasurementSchema
	if len(authority.operatorEvidence) != 0 {
		schema = ownerRecycleOperatorMeasurementSchema
	}
	return &OwnerRecycleMeasurement{Schema: schema, Status: OwnerRecycleDecisionBlocked,
		Approval: bytes.Clone(authority.approval), Census: bytes.Clone(authority.census), ProviderMeasurement: providerBytes,
		OperatorEvidence: bytes.Clone(authority.operatorEvidence), Row: row}, ctx.Err()
}

// The immutable references and complete row form a distinct blocked intent.
// Eligibility, history and outcome gates cannot be cleared by candidate fields.
func ownerRecycleDecisionIntent(authority *OwnerRecycleMeasurementAuthority, capsule *OwnerRecycleMeasurement, encoded []byte) *OwnerRecycleDecisionIntent {
	intent := &OwnerRecycleDecisionIntent{Schema: ownerRecycleDecisionIntentSchema, Status: OwnerRecycleDecisionBlocked,
		CapsuleHash: ReleaseMeasurementContentHash(encoded), ApprovalHash: authority.observation.ApprovalHash,
		ProposalHash: authority.observation.ProposalHash, CensusHash: ReleaseMeasurementContentHash(capsule.Census),
		ProviderMeasurementHash: ReleaseMeasurementContentHash(capsule.ProviderMeasurement), Decision: authority.expected, Row: capsule.Row,
		Blockers: []string{
			"decision-time active-validator stake, permit, liveness and independence remain unproved",
			"operator health and complete native/EVM source history require separate authenticated admission",
			"drained successor activation, signed envelopes, transaction custody and archive transition remain blocked",
			"final Yuma incentives, owner recycling and the 10/90 native outcome remain unobserved",
		}}
	if len(authority.operatorEvidence) != 0 {
		intent.Schema = ownerRecycleOperatorIntentSchema
		intent.OperatorEvidenceHash = ReleaseMeasurementContentHash(authority.operatorEvidence)
		intent.Blockers[1] = "decision-time operator state and source-root window are observed; API health, key/payout custody and full native/EVM history remain unproved"
	}
	return intent
}

// Seals reviewable evidence only. There is no signer, prepared transaction,
// activation-ready flag or mutation of existing measurement/intent history.
func SealOwnerRecycleMeasurement(ctx context.Context, authority *OwnerRecycleMeasurementAuthority, providerBytes []byte, options ReleaseMeasurementV2Options) ([]byte, *OwnerRecycleDecisionIntent, error) {
	capsule, err := buildOwnerRecycleMeasurement(ctx, authority, providerBytes, options)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(capsule)
	if err != nil {
		return nil, nil, err
	}
	encoded = append(encoded, '\n')
	if uint64(len(encoded)) > min(options.MaxArtifactBytes, options.MaxControlBytes) {
		return nil, nil, errors.New("owner-recycle canonical capsule exceeds its complete allowance")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return encoded, ownerRecycleDecisionIntent(authority, capsule, encoded), nil
}

// Candidate rows stay bounded raw bytes until the real row is reconstructed;
// a compact JSON array cannot amplify into an unbounded typed control graph.
type ownerRecycleMeasurementWire struct {
	Schema              string                     `json:"schema"`
	Status              OwnerRecycleDecisionStatus `json:"status"`
	Approval            []byte                     `json:"approval"`
	Census              []byte                     `json:"census"`
	ProviderMeasurement []byte                     `json:"provider_measurement"`
	OperatorEvidence    []byte                     `json:"operator_evidence,omitempty"`
	Row                 json.RawMessage            `json:"row"`
}

// Every replay requires independently observed authority and the complete
// original proof streams. A matching outer hash alone never replaces replay.
func ReplayOwnerRecycleMeasurement(ctx context.Context, authority *OwnerRecycleMeasurementAuthority, encoded []byte, options ReleaseMeasurementV2Options) (*OwnerRecycleDecisionIntent, error) {
	limit, err := ownerRecycleMeasurementLimit(ctx, authority, options)
	if err != nil {
		return nil, err
	}
	if uint64(len(encoded)) > limit {
		return nil, errors.New("owner-recycle capsule exceeds its complete allowance")
	}
	encoded = bytes.Clone(encoded)
	if err := protocol.ValidateUniqueJsonKeys(encoded); err != nil {
		return nil, err
	}
	var capsule ownerRecycleMeasurementWire
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&capsule); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(capsule)
	if err != nil || !bytes.Equal(encoded, append(canonical, '\n')) {
		return nil, errors.Join(errors.New("owner-recycle capsule is not canonical"), err)
	}
	schema := ownerRecycleMeasurementSchema
	if len(authority.operatorEvidence) != 0 {
		schema = ownerRecycleOperatorMeasurementSchema
	}
	if capsule.Schema != schema || capsule.Status != OwnerRecycleDecisionBlocked ||
		!bytes.Equal(capsule.Approval, authority.approval) || !bytes.Equal(capsule.Census, authority.census) ||
		!bytes.Equal(capsule.OperatorEvidence, authority.operatorEvidence) {
		return nil, errors.New("owner-recycle capsule differs from the original approved decision authority")
	}
	rebuilt, err := buildOwnerRecycleMeasurement(ctx, authority, capsule.ProviderMeasurement, options)
	if err != nil {
		return nil, err
	}
	row, err := json.Marshal(rebuilt.Row)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(capsule.Row, row) {
		return nil, errors.New("owner-recycle declared row differs from replayed measurement and exact quantization")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ownerRecycleDecisionIntent(authority, rebuilt, encoded), nil
}

// The separate intent is compared in full after actual capsule/proof replay.
// Detaching it first prevents a synchronous reader from replacing its claims.
func VerifyOwnerRecycleDecisionIntent(ctx context.Context, authority *OwnerRecycleMeasurementAuthority, encoded []byte, options ReleaseMeasurementV2Options, intent *OwnerRecycleDecisionIntent) error {
	limit, err := ownerRecycleMeasurementLimit(ctx, authority, options)
	if err != nil {
		return err
	}
	if intent == nil {
		return errors.New("owner-recycle decision intent is absent")
	}
	remaining := limit
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(intent), &remaining); err != nil {
		return err
	}
	claimed, err := json.Marshal(intent)
	if err != nil || uint64(len(claimed)) > limit {
		return errors.Join(errors.New("owner-recycle decision intent exceeds its allowance"), err)
	}
	replayed, err := ReplayOwnerRecycleMeasurement(ctx, authority, encoded, options)
	if err != nil {
		return err
	}
	want, err := json.Marshal(replayed)
	if err != nil || !bytes.Equal(claimed, want) {
		return errors.Join(errors.New("owner-recycle decision intent differs from complete replay"), err)
	}
	return ctx.Err()
}
