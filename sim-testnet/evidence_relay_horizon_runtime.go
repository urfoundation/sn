//go:build linux || darwin

// Read-only phase entry authenticates original debits. Strict entry also
// previews pending public censuses; every actual relay authenticates its input.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

// The original native snapshot and a new finalized native snapshot are read
// through the existing metadata-qualified hotkey/stake/schedule reader. Evm
// height is never used to choose a native hash or subnet epoch.
func (self *evidenceRelayRuntime) readHorizonNative(ctx context.Context, activation protocol.ValidatorEvidenceActivation, current bool) (uint64, error) {
	if ctx == nil || self == nil || self.executor == nil || self.executor.substrate == nil || self.executor.substrate.chain == nil || self.executor.cfg == nil || self.executor.cfg.Hyperparameters == nil || self.executor.plan == nil || self.executor.plan.ValidatorEvidence == nil {
		return 0, errors.New("evidence relay native horizon owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	chain := self.executor.substrate.chain
	if chain.API == nil || chain.API.Client == nil {
		return 0, errors.New("evidence relay native horizon client is absent")
	}
	hash, block := types.Hash(activation.NativeHash), activation.NativeBlock
	if current {
		var err error
		hash, err = crv4.FinalizedHeadContext(ctx, chain)
		if err != nil {
			return 0, err
		}
		header, err := chain.HeaderAtContext(ctx, hash)
		if err != nil {
			return 0, err
		}
		block = uint64(header.Number)
	}
	maximum := hyperparameterUint64(self.executor.cfg.Hyperparameters.OwnerControlled["max_allowed_uids"])
	if maximum == 0 || maximum > uint64(^uint16(0)) {
		return 0, errors.New("evidence relay native census bound differs")
	}
	observed, err := crv4.ReadValidatorScheduleAtContext(ctx, chain, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(self.executor.plan.ValidatorEvidence.GenesisHash), BlockHash: hash, BlockNumber: block,
		Netuid: self.executor.cfg.Netuid, Hotkey: activation.Hotkey, MaximumSubnetUIDs: uint32(maximum)}, self.executor.runtimeEvidenceNativeIdentityV2())
	if err != nil || !observed.Stake.MeetsNonSelfStakeAndPermit() {
		return 0, errors.Join(errors.New("evidence relay native horizon lacks independent finalized eligibility/schedule"), err)
	}
	return observed.SubnetEpochIndex, nil
}

// This common reader is used at preflight and immediately before sending.
// Its returned values are metadata only; complete payload buffers are released.
func (self *evidenceRelayRuntime) readClosedPublication(ctx context.Context, source *evidenceRelaySource, manifest *validatorcomponent.ValidatorEvidencePublicationV2Manifest, block uint64, hash [32]byte) ([]validatorcomponent.ValidatorEvidenceTransactionV2Expected, error) {
	epoch := new(big.Int).SetUint64(manifest.Epoch)
	start, err := self.chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, epoch)
	if err != nil {
		return nil, err
	}
	end, err := self.chain.ReleaseEpochEndBlockAtHashContext(ctx, block, hash, epoch)
	if err != nil {
		return nil, err
	}
	if end > block || end <= start || end-start < self.work.settlementCadence {
		return nil, errors.New("evidence relay discovered a terminal publication outside the funded finalized epoch geometry")
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: manifest.Epoch, StartBlock: start, EndBlock: end, FinalizedBlock: end}
	publication, err := validatorcomponent.ReadValidatorEvidencePublicationV2(ctx, manifest, validatorcomponent.ValidatorEvidencePublicationV2ReadOptions{Activations: source.activations, Window: window, Origins: self.origins, Bounds: source.bounds})
	if err != nil {
		return nil, err
	}
	result := make([]validatorcomponent.ValidatorEvidenceTransactionV2Expected, len(publication.Members))
	for member, artifact := range publication.Members {
		result[member] = validatorcomponent.ValidatorEvidenceTransactionV2Expected{Journal: self.executor.plan.ValidatorEvidence.Address, RuntimeHash: [32]byte(self.executor.plan.ValidatorEvidence.RuntimeCodeHash),
			Activation: source.activations[member], Window: window, Evidence: artifact.Evidence, MaxTransactionBytes: 64 * 1024, MaxReceiptLogs: 1024}
	}
	return result, nil
}

// Audit subjects are not collapsed by native epoch. Both actual public
// replicas and every original operator consent remain mandatory.
func (self *evidenceRelayRuntime) readAuditPublication(ctx context.Context, source *evidenceRelaySource, manifest *validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest, block uint64, hash [32]byte) ([]validatorcomponent.ValidatorEvidenceTransactionV2Expected, error) {
	if manifest.Decision.ValidatorID != source.validatorId {
		return nil, errors.New("evidence audit decision differs from its original configured validator")
	}
	epoch := new(big.Int).SetUint64(manifest.Epoch)
	start, err := self.chain.ReleaseEpochStartBlockAtHashContext(ctx, block, hash, epoch)
	if err != nil {
		return nil, err
	}
	end, err := self.chain.ReleaseEpochEndBlockAtHashContext(ctx, block, hash, epoch)
	if err != nil {
		return nil, err
	}
	if end > block || end <= start || end-start < self.work.settlementCadence {
		return nil, errors.New("evidence audit publication is outside the funded finalized epoch geometry")
	}
	window := protocol.ValidatorEvidenceWindow{Epoch: manifest.Epoch, StartBlock: start, EndBlock: end, FinalizedBlock: block, Subject: manifest.Subject}
	publication, err := validatorcomponent.ReadValidatorEvidenceDepositAuditV2(ctx, manifest, validatorcomponent.ValidatorEvidencePublicationV2ReadOptions{Activations: source.activations, Window: window, Origins: self.origins, Bounds: source.bounds})
	if err != nil {
		return nil, err
	}
	result := make([]validatorcomponent.ValidatorEvidenceTransactionV2Expected, len(publication.Members))
	for member, artifact := range publication.Members {
		if artifact.Evidence.Header.Kind != protocol.ValidatorEvidenceDepositAudit {
			return nil, errors.New("audit relay reader returned another evidence kind")
		}
		ownedWindow := window
		ownedWindow.FinalizedBlock = max(end, artifact.Evidence.Header.BoundaryBlock)
		result[member] = validatorcomponent.ValidatorEvidenceTransactionV2Expected{Journal: self.executor.plan.ValidatorEvidence.Address, RuntimeHash: [32]byte(self.executor.plan.ValidatorEvidence.RuntimeCodeHash),
			Activation: source.activations[member], Window: ownedWindow, Evidence: artifact.Evidence, MaxTransactionBytes: 64 * 1024, MaxReceiptLogs: 1024}
	}
	return result, nil
}

// Provisional continuation admits only the next block of work. The original
// horizon and every actual header/debit remain bounded by the paid allowance.
func (self *evidenceRelayRuntime) phaseHorizonRemaining(horizon *evidenceRelayHorizon, block, remaining uint64) (uint64, error) {
	if self == nil || self.executor == nil || !provisionalResumeEnabled(self.executor.cfg) {
		return remaining, nil
	}
	end, ok := checkedAdd(block, remaining)
	if !ok || remaining == 0 {
		return 0, errors.New("evidence relay remaining horizon is zero or overflows")
	}
	funded, _, _, err := horizon.ceilings(nil)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(os.Stderr, "sim-testnet: provisional evidence relay phase forecast; observed_block=%d requested_remaining=%d requested_end=%d funded_end=%d forecast_waived=true final_acceptance=false\n", block, remaining, end, funded)
	return 1, nil
}

func (self *evidenceRelayRuntime) requireHorizonRemaining(horizon *evidenceRelayHorizon, block, native, remaining uint64) error {
	bounded, err := self.phaseHorizonRemaining(horizon, block, remaining)
	if err != nil {
		return err
	}
	if bounded != remaining {
		nativeRemaining := remaining / horizon.work.nativeCadence
		if remaining%horizon.work.nativeCadence != 0 {
			nativeRemaining++
		}
		if _, ok := checkedAdd(native, nativeRemaining); !ok {
			return errors.New("evidence relay remaining native horizon overflows")
		}
	}
	return horizon.requireRemaining(block, native, bounded)
}

// The worker has not admitted or sent anything when this executes. Read each
// original request separately; only its header survives the bounded read.
func (self *evidenceRelayRuntime) prepareHorizon() error {
	block, hash, err := self.chain.FinalizedBlockContext(self.ctx)
	if err != nil {
		return err
	}
	work, err := evidenceRelayConfiguredWork(self.executor.cfg)
	if err != nil {
		return err
	}
	remaining, err := work.remaining(self.phase, self.prepared)
	if err != nil {
		return err
	}
	self.work = work
	if self.executor.journal == nil || len(self.sources) == 0 || len(self.sources[0].activations) == 0 {
		return errors.New("evidence relay horizon lacks original journal/source ownership")
	}
	anchor := self.sources[0].activations[0]
	horizon := &evidenceRelayHorizon{work: work, maximum: self.executor.cfg.Config.ValidatorEvidenceRelay.MaxSlots,
		anchorBlock: anchor.EVMBlock, anchorEpoch: anchor.Domain.Epoch, sourceKVs: map[evidenceRelayHorizonSource]protocol.ValidatorEvidenceActivation{}, headerKVs: map[[32]byte]protocol.ValidatorEvidenceHeader{}}
	for _, source := range self.sources {
		for _, activation := range source.activations {
			key := evidenceRelayHorizonSource{hotkey: activation.Hotkey, noId: activation.NoID}
			if _, found := horizon.sourceKVs[key]; found || activation.EVMBlock != anchor.EVMBlock || activation.EVMHash != anchor.EVMHash || activation.NativeBlock != anchor.NativeBlock || activation.NativeHash != anchor.NativeHash || activation.Domain.Epoch != anchor.Domain.Epoch {
				return errors.New("evidence relay horizon sources have different original activation anchors")
			}
			horizon.sourceKVs[key] = activation
		}
	}
	// Reject plainly insufficient profiles before historical/native/public
	// source I/O. This does not authenticate or credit any private candidate.
	if err := self.requireHorizonRemaining(horizon, block, 0, remaining); err != nil {
		return err
	}
	anchorNative, err := self.readHorizonNative(self.ctx, anchor, false)
	if err != nil {
		return err
	}
	horizon.anchorNativeEpoch = anchorNative
	currentNative, err := self.readHorizonNative(self.ctx, anchor, true)
	if err != nil {
		return err
	}
	if err := self.requireHorizonRemaining(horizon, block, currentNative, remaining); err != nil {
		return err
	}
	canonical, err := self.chain.BlockHashContext(self.ctx, anchor.EVMBlock)
	if err != nil || canonical != anchor.EVMHash {
		return errors.Join(errors.New("evidence relay original activation Evm anchor is not canonical"), err)
	}
	if err := self.readAdmittedHorizon(self.ctx, horizon, block); err != nil {
		return err
	}
	for index := range self.sources {
		if provisionalResumeEnabled(self.executor.cfg) {
			fmt.Fprintln(os.Stderr, "sim-testnet: provisional evidence relay pending_public_census_preview_waived=true; actual publication authentication and slot admission remain required; final_acceptance=false")
			break
		}
		source := &self.sources[index]
		closed, err := validatorcomponent.DiscoverValidatorEvidencePublicationV2Manifests(self.ctx, source.stateDir, source.bounds)
		if err != nil {
			return err
		}
		for index := range closed {
			requests, err := self.readClosedPublication(self.ctx, source, &closed[index], block, hash)
			if err != nil {
				return err
			}
			for _, request := range requests {
				if err := horizon.admit(request.Evidence.Header, block); err != nil {
					return err
				}
			}
		}
		audits, err := validatorcomponent.DiscoverValidatorEvidenceDepositAuditV2Manifests(self.ctx, source.stateDir, source.bounds)
		if err != nil {
			return err
		}
		for index := range audits {
			requests, err := self.readAuditPublication(self.ctx, source, &audits[index], block, hash)
			if err != nil {
				return err
			}
			for _, request := range requests {
				if err := horizon.admit(request.Evidence.Header, block); err != nil {
					return err
				}
			}
		}
	}
	// Discovery/public reads can take real time. Re-read both clocks before
	// giving preparation permission, without turning elapsed time into credit.
	block, _, err = self.chain.FinalizedBlockContext(self.ctx)
	if err != nil {
		return err
	}
	currentNative, err = self.readHorizonNative(self.ctx, anchor, true)
	if err != nil {
		return err
	}
	if err := self.requireHorizonRemaining(horizon, block, currentNative, remaining); err != nil {
		return err
	}
	self.horizon = horizon
	return self.ctx.Err()
}

// Read every original journal-admitted request, including failed/prior-phase
// records. Exact retries deduplicate by immutable slot, never by success stage.
func (self *evidenceRelayRuntime) readAdmittedHorizon(ctx context.Context, horizon *evidenceRelayHorizon, block uint64) error {
	if ctx == nil || horizon == nil || self == nil || self.executor == nil || self.executor.journal == nil || self.executor.plan == nil {
		return errors.New("evidence relay original debit owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	entries := self.executor.journal.Entries()
	seenKVs := map[string]bool{}
	for _, entry := range entries {
		if entry.PlanHash != self.executor.plan.PlanHash || !strings.HasPrefix(entry.ActionID, evidenceRelayActionPrefix) {
			continue
		}
		if seenKVs[entry.ActionID] {
			continue
		}
		if !validCanonicalHashHex("0x"+strings.TrimPrefix(entry.ActionID, evidenceRelayActionPrefix)) || uint64(len(seenKVs)) >= horizon.maximum {
			return errors.New("evidence relay retained request census is malformed or exceeds approval")
		}
		name := strings.TrimPrefix(entry.ActionID, evidenceRelayActionPrefix) + ".json"
		raw, err := validatorcomponent.ReadReleaseEvidenceV2SetupFile(ctx, filepath.Join(self.executor.stateDir, "evidence-relay", name), evidenceRelayActionBytes)
		if err != nil {
			return err
		}
		action, err := validateEvidenceRelayRequest(self.executor.plan, entries, raw)
		if err != nil || action.ID != entry.ActionID {
			return errors.Join(errors.New("evidence relay horizon lacks an authenticated original debit"), err)
		}
		var retained evidenceRelayRequestRecord
		if err := json.Unmarshal(raw, &retained); err != nil {
			return err
		}
		if err := horizon.admit(retained.Evidence.Evidence.Header, block); err != nil {
			return err
		}
		seenKVs[action.ID] = true
	}
	return ctx.Err()
}

// Runtime polling cannot continue beyond the finite activation-anchored
// block horizon, even when no next public manifest has appeared yet.
func (self *evidenceRelayRuntime) checkHorizonBlock(block uint64) error {
	if self.horizon == nil {
		return errors.New("evidence relay has no authenticated funded horizon")
	}
	maximum, _, _, err := self.horizon.ceilings(nil)
	if err != nil {
		return err
	}
	if block < self.horizon.anchorBlock || block > maximum {
		return fmt.Errorf("evidence relay reached its funded activation horizon: block=%d maximum=%d", block, maximum)
	}
	if !self.prepared {
		remaining, err := self.work.remaining(self.phase, true)
		if err != nil {
			return err
		}
		remaining, err = self.phaseHorizonRemaining(self.horizon, block, remaining)
		if err != nil {
			return err
		}
		end, ok := checkedAdd(block, remaining)
		if !ok || end > maximum {
			return fmt.Errorf("evidence relay preparation exhausted required later work: block=%d remaining=%d maximum=%d", block, remaining, maximum)
		}
		self.horizon.minimumEnd = max(self.horizon.minimumEnd, end)
	}
	return nil
}

// One bounded request asks the existing worker to retain the new remaining
// work floor; no caller may race its slot census or hold a lock across Rpc.
type evidenceRelayRemainingRequest struct {
	ctx       context.Context
	remaining uint64
	result    chan error
}

// Called by the worker only, at a real phase transition after preparation.
func (self *evidenceRelayRuntime) checkRemaining(request evidenceRelayRemainingRequest) error {
	if err := request.ctx.Err(); err != nil {
		return err
	}
	block, _, err := self.chain.FinalizedBlockContext(request.ctx)
	if err != nil {
		return err
	}
	native, err := self.readHorizonNative(request.ctx, self.sources[0].activations[0], true)
	if err != nil {
		return err
	}
	if err := self.requireHorizonRemaining(self.horizon, block, native, request.remaining); err != nil {
		return err
	}
	self.prepared = true
	return nil
}

// The constructor owns the worker; campaign preparation must wait for its
// read-only admission result, not merely successful transport construction.
func (self *evidenceRelayRuntime) WaitReady(ctx context.Context) error {
	if self == nil || ctx == nil {
		return errors.New("evidence relay readiness owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-self.ready:
		select {
		case <-self.done:
			return self.stoppedHorizonError()
		default:
		}
		return ctx.Err()
	case <-self.done:
		return self.stoppedHorizonError()
	}
}

// Joining shutdown preserves the real source failure alongside the refusal.
func (self *evidenceRelayRuntime) stoppedHorizonError() error {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return errors.Join(errors.New("evidence relay stopped before funded phase admission"), self.resultErr)
}

// A completed preparation may have consumed delay headroom. Before the
// observed phase starts, recheck its actual remaining watchdog/terminal work.
func (self *evidenceRelayRuntime) RequirePrepared(ctx context.Context) error {
	if err := self.WaitReady(ctx); err != nil {
		return err
	}
	remaining, err := self.work.remaining(self.phase, true)
	if err != nil {
		return err
	}
	request := evidenceRelayRemainingRequest{ctx: ctx, remaining: remaining, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-self.done:
		return self.stoppedHorizonError()
	case self.remainingRequests <- request:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-self.done:
		return self.stoppedHorizonError()
	case err := <-request.result:
		return err
	}
}
