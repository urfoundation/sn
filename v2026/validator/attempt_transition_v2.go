package validator

// Terminal payloads retain the real compact cut and complete pre/post-fold
// statistics. Member digests exclude the batch and signature; every signature
// then binds the same complete digest census without a circular hash.

import (
	"crypto/sha256"
	"encoding/json"
)

const (
	AttemptSettlementTransitionV2Schema = "urnetwork-validator-settlement-transition-v2"
	AttemptSettlementClosureV2Schema    = "urnetwork-validator-settlement-closure-v2"
	attemptSettlementV2DigestDomain     = "urnetwork/validator/settlement-transition/digest/v2\x00"
	attemptSettlementV2SignDomain       = "urnetwork/validator/settlement-transition/sign/v2\x00"
)

// The cut independently signs the exact terminal streams. PreFold contains
// raw statistics only, never an embedded legacy cut or another transition.
// Activation and the native hotkey remain bound by Cut.Context.Activation.
type AttemptSettlementTransitionV2 struct {
	Schema       string                     `json:"schema"`
	Identity     AttemptLedgerIdentity      `json:"identity"`
	FromBoundary AttemptBoundary            `json:"from_boundary"`
	ToEpoch      uint64                     `json:"to_epoch"`
	PreFold      ReleaseStatsMeasurement    `json:"pre_fold"`
	Cut          AttemptCutV2               `json:"cut"`
	PostFold     []AttemptSettlementQuality `json:"post_fold"`
	Batch        []AttemptSettlementMember  `json:"batch"`
	Signature    []byte                     `json:"signature"`
}

// One canonical closure carries every configured operator once. Its epoch
// names the closed settlement, not the successor or a native measurement.
// Verification requires an independent complete operator/context census.
type AttemptSettlementClosureV2 struct {
	Schema      string                           `json:"schema"`
	Epoch       uint64                           `json:"epoch"`
	Transitions []*AttemptSettlementTransitionV2 `json:"transitions"`
}

// A signed core is independent of the final membership and signature bytes.
// These fields intentionally include the signed cut, not merely its root.
type attemptSettlementTransitionV2Core struct {
	Schema       string                     `json:"schema"`
	Identity     AttemptLedgerIdentity      `json:"identity"`
	FromBoundary AttemptBoundary            `json:"from_boundary"`
	ToEpoch      uint64                     `json:"to_epoch"`
	PreFold      ReleaseStatsMeasurement    `json:"pre_fold"`
	Cut          AttemptCutV2               `json:"cut"`
	PostFold     []AttemptSettlementQuality `json:"post_fold"`
}

// The complete canonical membership follows the independently digested core.
type attemptSettlementTransitionV2Payload struct {
	Core  attemptSettlementTransitionV2Core `json:"core"`
	Batch []AttemptSettlementMember         `json:"batch"`
}

// Only admitted, operation-owned transitions reach canonical hashing.
func attemptSettlementCoreV2(transition *AttemptSettlementTransitionV2) attemptSettlementTransitionV2Core {
	return attemptSettlementTransitionV2Core{
		Schema: transition.Schema, Identity: transition.Identity,
		FromBoundary: transition.FromBoundary, ToEpoch: transition.ToEpoch,
		PreFold: transition.PreFold, Cut: transition.Cut, PostFold: transition.PostFold,
	}
}

// The distinct digest domain prevents legacy or signature-message reuse.
func attemptSettlementTransitionDigestV2(transition *AttemptSettlementTransitionV2) ([32]byte, error) {
	var digest [32]byte
	raw, err := json.Marshal(attemptSettlementCoreV2(transition))
	if err != nil {
		return digest, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(attemptSettlementV2DigestDomain))
	_, _ = hash.Write(raw)
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

// Every expected operator signs its own core and the same complete census.
func attemptSettlementTransitionMessageV2(transition *AttemptSettlementTransitionV2) ([]byte, error) {
	raw, err := json.Marshal(attemptSettlementTransitionV2Payload{Core: attemptSettlementCoreV2(transition), Batch: transition.Batch})
	if err != nil {
		return nil, err
	}
	message := make([]byte, 0, len(attemptSettlementV2SignDomain)+len(raw))
	message = append(message, attemptSettlementV2SignDomain...)
	return append(message, raw...), nil
}
