//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const evidenceRelayPolicyGapSchema = "urnetwork-sim-evidence-policy-gap-v2"
const evidenceRelayPolicyGapMaximumBytes = 128 * 1024

// The rollover/plan owner supplies these only after verifying the terminal
// ledger and approving its successor. Consent alone is not rollover approval.
type evidenceRelayPolicyGapActivation struct {
	Activation      protocol.ValidatorEvidenceActivation `json:"activation"`
	VPKSignature    []byte                               `json:"vpk_signature"`
	HotkeySignature []byte                               `json:"hotkey_signature"`
}

type evidenceRelayPolicyGapRecord struct {
	Schema                  string                                                    `json:"schema"`
	PlanHash                string                                                    `json:"plan_hash"`
	RolloverPlanHash        string                                                    `json:"rollover_plan_hash,omitempty"`
	ConfigHash              string                                                    `json:"config_hash"`
	ValidatorID             uint64                                                    `json:"validator_id"`
	ManifestHash            string                                                    `json:"manifest_hash"`
	Cutoff                  evidenceRelayPolicyGapActivation                          `json:"rollover_cutoff"`
	Request                 validatorcomponent.ValidatorEvidenceTransactionV2Expected `json:"request"`
	Mismatch                validatorcomponent.ValidatorEvidencePolicyEraMismatchV2   `json:"finalized_policy_mismatch"`
	CompletedEvidence       bool                                                      `json:"completed_evidence"`
	FinalAcceptance         bool                                                      `json:"final_acceptance"`
	SourceGenerationChanged bool                                                      `json:"source_generation_changed"`
	LedgerContinuityClaimed bool                                                      `json:"ledger_continuity_claimed"`
	FirstAcceptanceEpoch    uint64                                                    `json:"first_acceptance_epoch"`
}

type evidenceRelayPolicyGapEnvelope struct {
	Record evidenceRelayPolicyGapRecord `json:"record"`
	MAC    string                       `json:"hmac_sha256"`
}

type evidenceRelayPolicyGapProgress struct {
	validatorID uint64
	header      protocol.ValidatorEvidenceHeader
}

// Install before the worker starts. Default construction installs no cutoff,
// so an unapproved historical policy mismatch retains its terminal behavior.
// The signed complete source census fixes the first eligible acceptance epoch;
// the relay never derives a cutoff from wall time or its oldest missing slot.
func (self *evidenceRelayRuntime) installPolicyGapCutoff(ctx context.Context, validatorID uint64, supplied []evidenceRelayPolicyGapActivation) error {
	return self.installPolicyGapCutoffAndSource(ctx, validatorID, supplied, nil)
}

func (self *evidenceRelayRuntime) installPolicyGapCutoffAndSource(ctx context.Context, validatorID uint64, supplied []evidenceRelayPolicyGapActivation, successor *evidenceRelaySource) error {
	if ctx == nil || self == nil || self.chain == nil || self.executor == nil || self.executor.plan == nil || !validCanonicalHashHex(self.executor.plan.PlanHash) {
		return errors.New("evidence policy gap cutoff has no approved runtime owner")
	}
	self.stateLock.Lock()
	if self.workerStarted || self.horizon != nil || self.policyGapCutoffs[validatorID] != nil {
		self.stateLock.Unlock()
		return errors.New("evidence policy gap cutoff cannot change after installation or startup")
	}
	self.stateLock.Unlock()
	var source *evidenceRelaySource
	for index := range self.sources {
		if self.sources[index].validatorId == validatorID {
			source = &self.sources[index]
			break
		}
	}
	if source == nil || len(supplied) == 0 || len(supplied) != len(source.activations) {
		return errors.New("evidence policy gap cutoff lacks its complete configured source census")
	}
	block, _, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	owned := make([]evidenceRelayPolicyGapActivation, len(supplied))
	firstAcceptance := supplied[0].Activation.Domain.Epoch
	for index, signed := range supplied {
		old, next := source.activations[index], signed.Activation
		oldDomain, nextDomain := old.Domain, next.Domain
		oldDomain.PolicyHash, oldDomain.Epoch = nextDomain.PolicyHash, nextDomain.Epoch
		if oldDomain != nextDomain || next.Domain.PolicyHash == old.Domain.PolicyHash || next.Domain.Epoch <= old.Domain.Epoch ||
			next.Hotkey != old.Hotkey || next.NoID != old.NoID || next.EVMBlock < old.EVMBlock || next.EVMBlock > block ||
			next.VPK != old.VPK && (next.FirstSequence != 1 || next.PriorRoot != ([32]byte{})) ||
			index > 0 && (next.Domain != supplied[0].Activation.Domain || next.EVMBlock != supplied[0].Activation.EVMBlock || next.EVMHash != supplied[0].Activation.EVMHash || next.NativeBlock != supplied[0].Activation.NativeBlock || next.NativeHash != supplied[0].Activation.NativeHash) {
			return errors.New("evidence policy gap cutoff changes its source identity or rollover epoch")
		}
		if err := next.Verify(next, signed.VPKSignature, signed.HotkeySignature); err != nil {
			return err
		}
		if next.VPK != old.VPK {
			var ok bool
			firstAcceptance, ok = checkedAdd(next.Domain.Epoch, 1)
			if !ok {
				return errors.New("fresh evidence generation has no future complete acceptance epoch")
			}
		}
		hash, err := self.chain.BlockHashContext(ctx, next.EVMBlock)
		if err != nil {
			return err
		}
		if hash != next.EVMHash {
			return errors.New("evidence policy gap cutoff snapshot is not canonical")
		}
		domain, err := next.EvidenceDomain()
		if err != nil {
			return err
		}
		if err := self.chain.ValidateValidatorEvidencePolicyEraV2Context(ctx, protocol.ValidatorEvidenceHeader{Domain: domain, Epoch: next.Domain.Epoch}); err != nil {
			return err
		}
		owned[index] = evidenceRelayPolicyGapActivation{Activation: next, VPKSignature: bytes.Clone(signed.VPKSignature), HotkeySignature: bytes.Clone(signed.HotkeySignature)}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.workerStarted || self.horizon != nil || self.policyGapCutoffs[validatorID] != nil {
		return errors.New("evidence policy gap cutoff owner changed during authentication")
	}
	if self.policyGapCutoffs == nil {
		self.policyGapCutoffs = map[uint64][]evidenceRelayPolicyGapActivation{}
	}
	self.policyGapCutoffs[validatorID] = owned
	if self.policyGapFirstEpoch == nil {
		self.policyGapFirstEpoch = map[uint64]uint64{}
	}
	self.policyGapFirstEpoch[validatorID] = firstAcceptance
	if successor != nil {
		source.successor = successor
	}
	return nil
}

// Public payload authentication precedes this call. Only a genuine finalized
// mismatch, outside the installed rollover cutoff and without transaction
// custody, can become a gap. Transport errors and missing evidence never do.
func (self *evidenceRelayRuntime) retainHistoricalPolicyGap(ctx context.Context, source *evidenceRelaySource, manifestHash string, expected validatorcomponent.ValidatorEvidenceTransactionV2Expected) (bool, error) {
	cutoffs := self.policyGapCutoffs[source.validatorId]
	if len(cutoffs) == 0 || expected.Evidence.Header.Epoch >= cutoffs[0].Activation.Domain.Epoch ||
		expected.Evidence.Header.Kind == protocol.ValidatorEvidenceDepositAudit && expected.Evidence.Header.Subject.ObservationEpoch >= self.policyGapFirstEpoch[source.validatorId] {
		return false, nil
	}
	err := self.chain.ValidateValidatorEvidencePolicyEraV2Context(ctx, expected.Evidence.Header)
	if err == nil {
		return false, nil
	}
	var mismatch *validatorcomponent.ValidatorEvidencePolicyEraMismatchV2
	if !errors.As(err, &mismatch) || ctx.Err() != nil {
		return false, err
	}
	var cutoff *evidenceRelayPolicyGapActivation
	for index, original := range source.activations {
		if original == expected.Activation {
			cutoff = &cutoffs[index]
			break
		}
	}
	if cutoff == nil || expected.Window.EndBlock > cutoff.Activation.EVMBlock || expected.Evidence.Header.BoundaryBlock > cutoff.Activation.EVMBlock || !validCanonicalHashHex(manifestHash) {
		return false, errors.New("evidence policy gap is outside its authenticated historical cutoff")
	}
	if self.horizon == nil || self.executor.journal == nil || self.executor.cfg == nil || self.executor.cfg.Config == nil || self.executor.cfg.WalletMaterial == "" || self.executor.cfg.readOnlyAudit || !validCanonicalHashHex(self.executor.cfg.ConfigHash) {
		return false, errors.New("evidence policy gap has no durable authenticated writer")
	}
	if expected.Relayer != (common.Address{}) || len(expected.SignedTransaction) != 0 {
		return false, errors.New("evidence policy gap request already names transaction custody")
	}
	slot, err := expected.Evidence.Header.SlotKey()
	if err != nil {
		return false, err
	}
	actionID := fmt.Sprintf("%s%x", evidenceRelayActionPrefix, slot)
	for _, entry := range self.executor.journal.Entries() {
		if entry.ActionID == actionID && (entry.Stage == StageBroadcast || entry.TransactionHash != "" || entry.Signer != "" || entry.Nonce != "") {
			return false, errors.New("evidence policy gap cannot abandon original transaction custody")
		}
	}
	// The real winner reader authenticates both signatures, deployment and
	// immutable slot state. A previously published slot still follows receipt
	// recovery, even if a later observation reports another policy era.
	winner, winnerErr := self.chain.FindValidatorEvidenceSlotWinnerV2Context(ctx, expected)
	if winnerErr == nil && winner != nil {
		return false, nil
	}
	if !errors.Is(winnerErr, validatorcomponent.ErrValidatorEvidenceAbsent) {
		return false, errors.Join(errors.New("evidence policy gap has no authenticated absent slot"), winnerErr)
	}
	self.stateLock.Lock()
	_, known := self.policyGaps[slot]
	full := uint64(len(self.policyGaps)) >= self.horizon.maximum
	self.stateLock.Unlock()
	if !known && full {
		return false, errors.New("evidence policy gaps exceed their approved source bound")
	}
	// The reader's moving observation head is not part of either signature.
	// Preserve the minimal proved window so audit retries have a stable record.
	expected.Window.FinalizedBlock = max(expected.Window.EndBlock, expected.Evidence.Header.BoundaryBlock)
	record := evidenceRelayPolicyGapRecord{Schema: evidenceRelayPolicyGapSchema, PlanHash: self.executor.plan.PlanHash, RolloverPlanHash: self.policyRolloverPlanHash, ConfigHash: self.executor.cfg.ConfigHash,
		ValidatorID: source.validatorId, ManifestHash: manifestHash, Cutoff: *cutoff, Request: expected, Mismatch: *mismatch,
		SourceGenerationChanged: cutoff.Activation.VPK != expected.Activation.VPK, FirstAcceptanceEpoch: self.policyGapFirstEpoch[source.validatorId]}
	path := filepath.Join(self.executor.stateDir, "evidence-relay", "policy-gaps-v2", strings.TrimPrefix(record.PlanHash, "0x"), fmt.Sprintf("%x.json", slot))
	key := derive32(self.executor.cfg, "evidence-policy-gap/v2")
	raw, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, path, evidenceRelayPolicyGapMaximumBytes)
	if err == nil {
		var retained evidenceRelayPolicyGapEnvelope
		if err := decodeStrictJSONBytes(raw, &retained); err != nil {
			return false, err
		}
		canonical, err := encodeEvidenceRelayPolicyGap(retained.Record, key)
		if err != nil || !hmac.Equal(canonical, raw) {
			return false, errors.Join(errors.New("retained evidence policy gap authentication failed"), err)
		}
		// Keep the first observation immutable across later finalized heads.
		record.Mismatch.FinalizedBlock = retained.Record.Mismatch.FinalizedBlock
		record.Mismatch.FinalizedHash = retained.Record.Mismatch.FinalizedHash
		if !reflect.DeepEqual(record, retained.Record) {
			return false, errors.New("retained evidence policy gap changes its exact source, cutoff or policy")
		}
		if err := self.chain.AuthenticateValidatorEvidencePolicyEraMismatchV2Context(ctx, &retained.Record.Mismatch); err != nil {
			return false, err
		}
	} else {
		if !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
			return false, err
		}
		raw, err = encodeEvidenceRelayPolicyGap(record, key)
		if err != nil {
			return false, err
		}
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(ctx, path, raw, evidenceRelayPolicyGapMaximumBytes); err != nil {
			return false, err
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// A startup prefix means complete receipts for every slot. Freeze that
	// optimization at the first gap; restart reauthenticates each gap normally.
	self.startupCache = nil
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.policyGaps == nil {
		self.policyGaps = map[[32]byte]evidenceRelayPolicyGapProgress{}
	}
	self.policyGaps[slot] = evidenceRelayPolicyGapProgress{validatorID: source.validatorId, header: expected.Evidence.Header}
	close(self.changed)
	self.changed = make(chan struct{})
	return true, nil
}

func encodeEvidenceRelayPolicyGap(record evidenceRelayPolicyGapRecord, key [32]byte) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(raw)
	return json.Marshal(evidenceRelayPolicyGapEnvelope{Record: record, MAC: hex.EncodeToString(mac.Sum(nil))})
}

// stateLock is held by the caller. Old gaps remain explicit even after later
// epochs publish successfully; a high cursor alone can never cover them.
func (self *evidenceRelayRuntime) policyGapRangeError(first, last uint64) error {
	for _, gap := range self.policyGaps {
		if first <= gap.header.Epoch && gap.header.Epoch <= last || gap.header.Kind == protocol.ValidatorEvidenceDepositAudit && first <= gap.header.Subject.ObservationEpoch && gap.header.Subject.ObservationEpoch <= last {
			return fmt.Errorf("evidence acceptance range [%d,%d] intersects validator %d policy gap at epoch %d", first, last, gap.validatorID, gap.header.Epoch)
		}
	}
	for validatorID, minimum := range self.policyGapFirstEpoch {
		if first < minimum {
			return fmt.Errorf("evidence acceptance starts before validator %d authenticated policy rollover minimum %d", validatorID, minimum)
		}
	}
	return nil
}
