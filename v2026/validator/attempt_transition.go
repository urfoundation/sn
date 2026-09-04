package validator

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/urnetwork/connect/v2026"
)

const (
	attemptSettlementTransitionSchema = "urnetwork-validator-settlement-transition-v1"
	attemptSettlementDigestDomain     = "urnetwork/validator/settlement-transition/digest/v1\x00"
	attemptSettlementSignDomain       = "urnetwork/validator/settlement-transition/sign/v1\x00"
)

// AttemptSettlementQuality is one post-fold EMA value derived from an old
// settlement window.
type AttemptSettlementQuality struct {
	ClientID   string `json:"client_id"`
	HasQuality bool   `json:"has_quality"`
	QualityPPM uint32 `json:"quality_ppm"`
}

// AttemptSettlementMember binds every operator transition prepared by one
// validator-wide settlement transaction.
type AttemptSettlementMember struct {
	NoID   uint64 `json:"no_id"`
	Digest string `json:"digest"`
}

// AttemptSettlementTransition publishes the terminal signed attempt replay,
// pre-fold counters, and resulting EMA for one operator. Batch members prove
// that all operator snapshots were prepared as one logical transaction.
type AttemptSettlementTransition struct {
	Schema       string                     `json:"schema"`
	Identity     AttemptLedgerIdentity      `json:"identity"`
	FromBoundary AttemptBoundary            `json:"from_boundary"`
	ToEpoch      uint64                     `json:"to_epoch"`
	PreFold      ReleaseStatsMeasurement    `json:"pre_fold"`
	PostFold     []AttemptSettlementQuality `json:"post_fold"`
	Batch        []AttemptSettlementMember  `json:"batch"`
	Signature    []byte                     `json:"signature"`
}

type attemptSettlementTransitionCore struct {
	Schema       string                     `json:"schema"`
	Identity     AttemptLedgerIdentity      `json:"identity"`
	FromBoundary AttemptBoundary            `json:"from_boundary"`
	ToEpoch      uint64                     `json:"to_epoch"`
	PreFold      ReleaseStatsMeasurement    `json:"pre_fold"`
	PostFold     []AttemptSettlementQuality `json:"post_fold"`
}

type attemptSettlementTransitionPayload struct {
	Core  attemptSettlementTransitionCore `json:"core"`
	Batch []AttemptSettlementMember       `json:"batch"`
}

func attemptSettlementCore(transition *AttemptSettlementTransition) attemptSettlementTransitionCore {
	return attemptSettlementTransitionCore{
		Schema: transition.Schema, Identity: transition.Identity,
		FromBoundary: transition.FromBoundary, ToEpoch: transition.ToEpoch,
		PreFold: transition.PreFold, PostFold: transition.PostFold,
	}
}

func attemptSettlementTransitionDigest(transition *AttemptSettlementTransition) ([32]byte, error) {
	var digest [32]byte
	encoded, err := json.Marshal(attemptSettlementCore(transition))
	if err != nil {
		return digest, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(attemptSettlementDigestDomain))
	_, _ = hash.Write(encoded)
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func attemptSettlementTransitionMessage(transition *AttemptSettlementTransition) ([]byte, error) {
	encoded, err := json.Marshal(attemptSettlementTransitionPayload{Core: attemptSettlementCore(transition), Batch: transition.Batch})
	if err != nil {
		return nil, err
	}
	message := make([]byte, 0, len(attemptSettlementSignDomain)+len(encoded))
	message = append(message, attemptSettlementSignDomain...)
	return append(message, encoded...), nil
}

func sortedAttemptSettlementQualities(measurement ReleaseStatsMeasurement) ([]AttemptSettlementQuality, error) {
	verified, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil {
		return nil, err
	}
	qualities := make([]AttemptSettlementQuality, 0, len(verified.Providers))
	for clientID, provider := range verified.Providers {
		if provider.HasQuality {
			qualities = append(qualities, AttemptSettlementQuality{ClientID: clientID.String(), HasQuality: true, QualityPPM: provider.QualityPPM})
		}
	}
	sort.Slice(qualities, func(i, j int) bool { return qualities[i].ClientID < qualities[j].ClientID })
	return qualities, nil
}

func sortedAttemptEMAQualities(ema map[connect.Id]uint32) []AttemptSettlementQuality {
	qualities := make([]AttemptSettlementQuality, 0, len(ema))
	for clientID, quality := range ema {
		qualities = append(qualities, AttemptSettlementQuality{ClientID: clientID.String(), HasQuality: true, QualityPPM: quality})
	}
	sort.Slice(qualities, func(i, j int) bool { return qualities[i].ClientID < qualities[j].ClientID })
	return qualities
}

func (self *AttemptLedger) signSettlementTransition(transition *AttemptSettlementTransition) error {
	if transition == nil {
		return errors.New("settlement transition is nil")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if transition.Identity != self.identity {
		return errors.New("settlement transition ledger identity differs")
	}
	message, err := attemptSettlementTransitionMessage(transition)
	if err != nil {
		return err
	}
	transition.Signature = ed25519.Sign(self.vsk, message)
	return nil
}

// VerifyAttemptSettlementTransition reconstructs the post-fold EMA and checks
// the validator signature and complete cross-operator transaction membership.
func VerifyAttemptSettlementTransition(transition *AttemptSettlementTransition) error {
	if transition == nil || transition.Schema != attemptSettlementTransitionSchema || transition.ToEpoch == 0 || transition.ToEpoch != transition.FromBoundary.SettlementEpoch+1 {
		return errors.New("settlement transition identity is incomplete")
	}
	vpk, err := canonicalAttemptHex32("settlement transition validator vpk", transition.Identity.ValidatorVPK, false)
	if err != nil {
		return err
	}
	if err := validateAttemptLedgerIdentity(transition.Identity, vpk[:]); err != nil {
		return err
	}
	if err := validateAttemptBoundary(transition.FromBoundary); err != nil {
		return err
	}
	if transition.PreFold.SettlementTransition != nil || transition.PreFold.AttemptCut == nil {
		return errors.New("settlement transition pre-fold input is not terminal")
	}
	cut := transition.PreFold.AttemptCut
	if cut.Identity != transition.Identity || cut.Boundary != transition.FromBoundary {
		return errors.New("settlement transition attempt cut identity differs")
	}
	expectedPostFold, err := sortedAttemptSettlementQualities(transition.PreFold)
	if err != nil {
		return fmt.Errorf("settlement transition pre-fold statistics: %w", err)
	}
	if !slices.Equal(expectedPostFold, transition.PostFold) {
		return errors.New("settlement transition post-fold EMA differs")
	}
	digest, err := attemptSettlementTransitionDigest(transition)
	if err != nil {
		return err
	}
	encodedDigest := attemptHex32(digest)
	memberFound := false
	for index, member := range transition.Batch {
		if member.NoID == 0 || (index > 0 && member.NoID <= transition.Batch[index-1].NoID) {
			return errors.New("settlement transition batch is not strictly ordered")
		}
		if _, err := canonicalAttemptHex32("settlement transition member digest", member.Digest, false); err != nil {
			return err
		}
		if member.NoID == transition.Identity.NoID {
			if member.Digest != encodedDigest {
				return errors.New("settlement transition batch digest differs")
			}
			memberFound = true
		}
	}
	if !memberFound {
		return errors.New("settlement transition is absent from its batch")
	}
	message, err := attemptSettlementTransitionMessage(transition)
	if err != nil {
		return err
	}
	if len(transition.Signature) != ed25519.SignatureSize || !ed25519.Verify(vpk[:], message, transition.Signature) {
		return errors.New("settlement transition validator signature is invalid")
	}
	return nil
}

// VerifyAttemptSettlementBatch requires complete, same-boundary coverage of
// every transition named by the shared validator-wide transaction manifest.
func VerifyAttemptSettlementBatch(transitions []*AttemptSettlementTransition) error {
	if len(transitions) == 0 {
		return errors.New("settlement transition batch is empty")
	}
	ordered := append([]*AttemptSettlementTransition(nil), transitions...)
	for _, transition := range ordered {
		if transition == nil {
			return errors.New("settlement transition batch contains nil")
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Identity.NoID < ordered[j].Identity.NoID
	})
	wantBatch := ordered[0].Batch
	wantIdentity := ordered[0].Identity
	wantBoundary, wantEpoch := ordered[0].FromBoundary, ordered[0].ToEpoch
	if len(wantBatch) != len(ordered) {
		return errors.New("settlement transition batch coverage differs")
	}
	for index, transition := range ordered {
		if err := VerifyAttemptSettlementTransition(transition); err != nil {
			return fmt.Errorf("settlement transition no_id %d: %w", transition.Identity.NoID, err)
		}
		if !equalAttemptSettlementIdentity(wantIdentity, transition.Identity) || transition.FromBoundary != wantBoundary || transition.ToEpoch != wantEpoch || !equalAttemptSettlementBatch(wantBatch, transition.Batch) {
			return errors.New("settlement transitions do not share one transaction")
		}
		if wantBatch[index].NoID != transition.Identity.NoID {
			return errors.New("settlement transition participant coverage differs")
		}
	}
	return nil
}

func verifyAttemptSettlementTransitionForMeasurement(transition *AttemptSettlementTransition, measurement ReleaseStatsMeasurement) error {
	if err := VerifyAttemptSettlementTransition(transition); err != nil {
		return err
	}
	if measurement.Config != transition.PreFold.Config {
		return errors.New("settlement transition scoring config changed")
	}
	want := map[string]AttemptSettlementQuality{}
	for _, quality := range transition.PostFold {
		want[quality.ClientID] = quality
	}
	seen := map[string]bool{}
	for _, provider := range measurement.Providers {
		quality, exists := want[provider.ClientID]
		if provider.HasPriorQuality != exists || (exists && provider.PriorQualityPPM != quality.QualityPPM) {
			return fmt.Errorf("provider %s prior quality differs from settlement transition", provider.ClientID)
		}
		if exists {
			seen[provider.ClientID] = true
		}
	}
	if len(seen) != len(want) {
		return errors.New("settlement transition prior quality coverage differs")
	}
	if measurement.AttemptCut != nil {
		if measurement.AttemptCut.Identity != transition.Identity || measurement.AttemptCut.Boundary.SettlementEpoch != transition.ToEpoch {
			return errors.New("current attempt cut does not follow settlement transition")
		}
	}
	return nil
}

func equalAttemptSettlementBatch(left, right []AttemptSettlementMember) bool {
	return slices.Equal(left, right)
}

func equalAttemptSettlementIdentity(left, right AttemptLedgerIdentity) bool {
	left.NoID, right.NoID = 0, 0
	left.ValidatorVPK, right.ValidatorVPK = "", ""
	return left == right
}
