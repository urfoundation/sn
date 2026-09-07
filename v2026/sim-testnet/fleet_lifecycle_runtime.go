package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
)

const fleetLifecycleMutationSafetyBlocks uint64 = 100

type scenarioFleetLifecycle interface {
	BeginPhase(string, string) error
	BindAcceptanceWindowForPhase(string, *ScenarioAcceptanceWindow) error
	Advance(context.Context, *ScenarioObservation, []ScenarioFaultRecord) error
	Complete() bool
}

type liveFleetLifecycle struct {
	cfg                             *ResolvedConfig
	stateDir                        string
	executor                        *Executor
	evidence                        *FleetLifecycleEvidence
	phase                           string
	resumeValidated                 bool
	authenticatedReleaseHandoff     *FleetLifecycleEvidence
	authenticatedReleaseHandoffHash string
}

func fleetLifecycleReleaseProjection(evidence *FleetLifecycleEvidence) *FleetLifecycleEvidence {
	if evidence == nil {
		return nil
	}
	projection := *evidence
	projection.Stage = fleetLifecycleStageReleaseHandoff
	projection.ProductionRunID = ""
	projection.ReleaseHandoffHash = ""
	projection.ProductionFirstSettlementEpoch = 0
	projection.ProductionAcceptanceStartBlock = 0
	projection.ProductionAcceptanceEndBlock = 0
	projection.ProductionAcceptanceTerminalBlock = 0
	projection.ProductionNativeSchedule = nil
	projection.ProductionEVMEvidenceDeadlineBlock = 0
	projection.CandidateCensuses = nil
	for _, census := range evidence.CandidateCensuses {
		if census.Phase == "release-1.0" {
			projection.CandidateCensuses = append(projection.CandidateCensuses, census)
		}
	}
	return &projection
}

func fleetLifecycleCanonicalBytes(evidence *FleetLifecycleEvidence) ([]byte, error) {
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func fleetLifecycleHasProductionState(evidence *FleetLifecycleEvidence) bool {
	if evidence == nil {
		return false
	}
	if evidence.ProductionRunID != "" || evidence.ReleaseHandoffHash != "" || evidence.ProductionFirstSettlementEpoch != 0 || evidence.ProductionAcceptanceStartBlock != 0 || evidence.ProductionAcceptanceEndBlock != 0 || evidence.ProductionAcceptanceTerminalBlock != 0 || evidence.ProductionNativeSchedule != nil || evidence.ProductionEVMEvidenceDeadlineBlock != 0 {
		return true
	}
	for _, census := range evidence.CandidateCensuses {
		if census.Phase == "production-soak" {
			return true
		}
	}
	return false
}

func fleetLifecycleReleaseHandoffHash(evidence *FleetLifecycleEvidence) (string, error) {
	encoded, err := fleetLifecycleCanonicalBytes(fleetLifecycleReleaseProjection(evidence))
	if err != nil {
		return "", err
	}
	return bytesSHA256(encoded), nil
}

// AuthenticateReleaseHandoff pins the exact release lifecycle bytes before
// production preparation is allowed to mutate chain or process state.
func (self *liveFleetLifecycle) AuthenticateReleaseHandoff(encoded []byte, expectedHash, releaseRunID string) error {
	if self != nil {
		self.authenticatedReleaseHandoff = nil
		self.authenticatedReleaseHandoffHash = ""
	}
	if self == nil || len(encoded) == 0 || expectedHash == "" || releaseRunID == "" || bytesSHA256(encoded) != expectedHash {
		return errors.New("fleet lifecycle release handoff bytes or hash are invalid")
	}
	var handoff FleetLifecycleEvidence
	if err := decodeStrictJSONBytes(encoded, &handoff); err != nil {
		return fmt.Errorf("decode fleet lifecycle release handoff: %w", err)
	}
	canonical, err := fleetLifecycleCanonicalBytes(&handoff)
	if err != nil || !slices.Equal(encoded, canonical) {
		return stateMismatchError(err, "fleet lifecycle release handoff is not canonically encoded")
	}
	if handoff.Stage != fleetLifecycleStageReleaseHandoff || handoff.RunID != releaseRunID || fleetLifecycleHasProductionState(&handoff) {
		return errors.New("fleet lifecycle release handoff contains production state")
	}
	checker := *self
	checker.evidence = &handoff
	if err := checker.validatePersistedStateForPhase("release-1.0", releaseRunID, &handoff); err != nil {
		return fmt.Errorf("authenticate fleet lifecycle release handoff: %w", err)
	}
	self.authenticatedReleaseHandoff = &handoff
	self.authenticatedReleaseHandoffHash = expectedHash
	return nil
}

func fleetLifecycleCompletionStatus(lifecycle scenarioFleetLifecycle) (bool, string) {
	if lifecycle == nil {
		return true, "fleet lifecycle is not configured for this scenario"
	}
	if !lifecycle.Complete() {
		return false, "fleet lifecycle did not reach its phase-specific authenticated handoff"
	}
	return true, "fleet lifecycle reached its phase-specific authenticated handoff"
}

func (self *liveFleetLifecycle) write() error {
	if self.evidence == nil {
		return errors.New("fleet lifecycle state is unavailable")
	}
	return writePublicJSON(filepath.Join(self.stateDir, "public", "fleet-lifecycle.json"), self.evidence)
}

func (self *liveFleetLifecycle) Begin(runID string) error {
	return self.BeginPhase("release-1.0", runID)
}

func (self *liveFleetLifecycle) BeginPhase(phase, runID string) error {
	if self.cfg == nil || self.cfg.Config == nil || self.executor == nil || self.executor.plan == nil || runID == "" || (phase != "release-1.0" && phase != "production-soak") {
		return errors.New("fleet lifecycle dependencies are incomplete")
	}
	if err := validateFleetLifecycleTopology(self.cfg.Config.Topology); err != nil {
		return err
	}
	prior, err := loadFleetLifecycleEvidence(self.stateDir)
	if err == nil {
		if phase == "production-soak" {
			if self.authenticatedReleaseHandoff == nil || self.authenticatedReleaseHandoffHash == "" {
				return errors.New("production fleet lifecycle has no exact authenticated release handoff")
			}
			if prior.RunID != self.authenticatedReleaseHandoff.RunID || !fleetLifecycleCanonicalEqual(fleetLifecycleReleaseProjection(prior), self.authenticatedReleaseHandoff) {
				return errors.New("persisted fleet lifecycle is not an append-only successor of the exact authenticated release handoff")
			}
			if prior.ProductionRunID != "" && prior.ProductionRunID != runID {
				return errors.New("persisted fleet lifecycle is bound to another production run")
			}
			if prior.ProductionRunID == "" {
				if prior.Stage != fleetLifecycleStageReleaseHandoff || fleetLifecycleHasProductionState(prior) {
					return errors.New("unadopted production fleet lifecycle is not the exact release handoff")
				}
				prior.ProductionRunID = runID
				prior.ReleaseHandoffHash = self.authenticatedReleaseHandoffHash
			} else if prior.ReleaseHandoffHash != self.authenticatedReleaseHandoffHash {
				return errors.New("persisted fleet lifecycle names a different release handoff hash")
			}
		}
		if err := self.validatePersistedStateForPhase(phase, runID, prior); err != nil {
			return fmt.Errorf("persisted fleet lifecycle state: %w", err)
		}
		self.evidence = prior
		self.phase = phase
		self.resumeValidated = false
		return self.write()
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if phase != "release-1.0" {
		return errors.New("production fleet lifecycle has no authenticated release handoff")
	}
	targetEffective, err := self.installedEffectiveEpoch(fleetLifecycleVariantTargetTakeover)
	if err != nil {
		return fmt.Errorf("fleet lifecycle target takeover: %w", err)
	}
	companionEffective, err := self.installedEffectiveEpoch(fleetLifecycleVariantCompanionTakeover)
	if err != nil {
		return fmt.Errorf("fleet lifecycle companion takeover: %w", err)
	}
	if targetEffective == 0 || targetEffective != companionEffective {
		return errors.New("fleet lifecycle generation-3 takeovers do not share one effective epoch")
	}
	for _, expected := range []struct {
		variant string
		uid     uint16
	}{{fleetLifecycleVariantTargetTakeover, fleetLifecycleTargetExpectedUID}, {fleetLifecycleVariantCompanionTakeover, fleetLifecycleCompanionExpectedUID}} {
		for member := 1; member <= self.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
			binding, readErr := loadFleetLifecycleBindingEvidence(self.stateDir, expected.variant, member)
			if readErr != nil || binding.UID != expected.uid {
				return stateMismatchError(readErr, "fleet lifecycle %s member=%d UID=%d, want %d", expected.variant, member, func() uint16 {
					if binding == nil {
						return 0
					}
					return binding.UID
				}(), expected.uid)
			}
		}
	}
	launch, err := self.executor.substrate.FleetLifecyclePruneSnapshot()
	if err != nil {
		return err
	}
	if err := validateFleetLifecycleLaunchSnapshot(launch, self.executor.roles); err != nil {
		return err
	}
	self.evidence = &FleetLifecycleEvidence{
		Schema: fleetLifecycleEvidenceSchema, DeploymentID: self.cfg.Config.Deployment.DeploymentID,
		PlanHash: self.executor.plan.PlanHash, RunID: runID, Stage: fleetLifecycleStageAwaitingDemotion,
		TakeoverEffectiveEpoch: targetEffective, LaunchPrune: &launch,
	}
	self.phase = phase
	self.resumeValidated = false
	return self.write()
}

func (self *liveFleetLifecycle) Complete() bool {
	if self == nil || self.evidence == nil || !self.resumeValidated {
		return false
	}
	if self.phase == "release-1.0" {
		return self.evidence.Stage == fleetLifecycleStageReleaseHandoff
	}
	return self.phase == "production-soak" && self.evidence.Stage == fleetLifecycleStageComplete
}

func fleetLifecycleStageRank(stage string) (int, bool) {
	switch stage {
	case fleetLifecycleStageAwaitingDemotion:
		return 0, true
	case fleetLifecycleStageFallbackInstalled:
		return 1, true
	case fleetLifecycleStageFallbackPaid:
		return 2, true
	case fleetLifecycleStageProviderInstalled:
		return 3, true
	case fleetLifecycleStageProviderPaid:
		return 4, true
	case fleetLifecycleStageTerminalInstalled:
		return 5, true
	case fleetLifecycleStageReleaseHandoff:
		return 6, true
	case fleetLifecycleStageComplete:
		return 7, true
	default:
		return 0, false
	}
}

func validateFleetLifecyclePersistedCensus(census FleetLifecycleCandidateCensus) error {
	if census.Phase != "release-1.0" && census.Phase != "production-soak" {
		return errors.New("fleet lifecycle persisted census has no valid phase")
	}
	if _, ok := evidenceFixedHex(census.ObservationHash, 32); !ok {
		return errors.New("fleet lifecycle persisted census has no canonical observation hash")
	}
	for _, head := range []ChainHead{census.ObservedHead, census.NativeObservedHead} {
		if head.Number == 0 {
			return errors.New("fleet lifecycle persisted census has an empty observed head")
		}
		if _, ok := evidenceFixedHex(head.Hash, 32); !ok {
			return errors.New("fleet lifecycle persisted census has a noncanonical observed head")
		}
	}
	if len(census.CandidateUIDs) != 202 || len(census.CandidateHotkeys) != 202 || len(uint16Set(census.CandidateUIDs)) != 202 || len(census.Validators) != 2 {
		return errors.New("fleet lifecycle persisted census is not exact 202/200/2 evidence")
	}
	uidSet := uint16Set(census.CandidateUIDs)
	hotkeys := make(map[string]bool, len(census.CandidateHotkeys))
	for _, hotkey := range census.CandidateHotkeys {
		if _, ok := evidenceFixedHex(hotkey, 32); !ok || hotkeys[strings.ToLower(hotkey)] {
			return errors.New("fleet lifecycle persisted census has a malformed or duplicate hotkey")
		}
		hotkeys[strings.ToLower(hotkey)] = true
	}
	validators := map[int]bool{}
	for validatorIndex, validator := range census.Validators {
		if validator.ValidatorID != validatorIndex+1 || validators[validator.ValidatorID] || validator.SettlementEpoch == 0 || validator.SubnetEpoch == 0 || validator.NativeSnapshot.Number == 0 || validator.NativeSnapshot.Number > validator.Commit.Number || validator.EVMSnapshot.Number == 0 || validator.EVMSnapshot.Number > census.ObservedHead.Number || validator.Commit.Number == 0 || validator.RevealBlock < validator.Commit.Number || validator.Application.Number < validator.RevealBlock || validator.Application.Number > census.NativeObservedHead.Number {
			return errors.New("fleet lifecycle persisted census has invalid validator application identity")
		}
		validators[validator.ValidatorID] = true
		measurementHash, measurementErr := hex.DecodeString(strings.TrimPrefix(validator.MeasurementArtifactHash, "sha256:"))
		if measurementErr != nil || len(measurementHash) != 32 || validator.MeasurementArtifactHash != "sha256:"+hex.EncodeToString(measurementHash) {
			return errors.New("fleet lifecycle persisted validator census has no canonical measurement artifact hash")
		}
		canonical := true
		for _, value := range []string{validator.VectorHash, validator.ExtrinsicHash, validator.NativeSnapshot.Hash, validator.EVMSnapshot.Hash, validator.Commit.Hash, validator.RevealBlockHash, validator.Application.Hash} {
			if _, ok := evidenceFixedHex(value, 32); !ok {
				canonical = false
			}
		}
		if !canonical || len(validator.EligibleUIDs) != 202 || len(validator.SelectedUIDs) != 200 || len(validator.RejectedUIDs) != 2 || len(validator.AppliedWeights) != 202 {
			return errors.New("fleet lifecycle persisted validator census is incomplete")
		}
		eligible, selected, rejected := uint16Set(validator.EligibleUIDs), uint16Set(validator.SelectedUIDs), uint16Set(validator.RejectedUIDs)
		if len(eligible) != 202 || len(selected) != 200 || len(rejected) != 2 {
			return errors.New("fleet lifecycle persisted validator census contains duplicate UIDs")
		}
		weights := make(map[uint16]uint16, len(validator.AppliedWeights))
		for _, weight := range validator.AppliedWeights {
			if !uidSet[weight.UID] {
				return errors.New("fleet lifecycle persisted validator weight names a foreign UID")
			}
			if _, exists := weights[weight.UID]; exists {
				return errors.New("fleet lifecycle persisted validator weight duplicates a UID")
			}
			weights[weight.UID] = weight.Value
		}
		for uid := range uidSet {
			if !eligible[uid] || selected[uid] == rejected[uid] {
				return errors.New("fleet lifecycle persisted validator partition differs from the candidate census")
			}
			value, exists := weights[uid]
			if !exists || (selected[uid] && value == 0) || (rejected[uid] && value != 0) {
				return errors.New("fleet lifecycle persisted validator weights differ from its selected/rejected partition")
			}
		}
	}
	if len(census.Validators) == 2 && (census.Validators[0].SettlementEpoch != census.Validators[1].SettlementEpoch || census.Validators[0].SubnetEpoch != census.Validators[1].SubnetEpoch) {
		return errors.New("fleet lifecycle persisted validators disagree on settlement or native epoch")
	}
	return nil
}

func (self *liveFleetLifecycle) censusHasExactFleetRoles(census *FleetLifecycleCandidateCensus, expected map[int]struct {
	uid  uint16
	role string
}) bool {
	if census == nil || len(census.Validators) != 2 || len(census.CandidateUIDs) != len(census.CandidateHotkeys) {
		return false
	}
	for fleet, identity := range expected {
		if fleet < 1 {
			return false
		}
		hotkey, err := roleBytes32(self.executor.roles, identity.role)
		if err != nil {
			return false
		}
		found := false
		for index, uid := range census.CandidateUIDs {
			if uid == identity.uid && strings.EqualFold(census.CandidateHotkeys[index], fleetLifecycleHex(hotkey)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func fleetLifecycleMilestone(evidence *FleetLifecycleEvidence, name string) *FleetLifecycleCandidateCensus {
	if evidence == nil {
		return nil
	}
	for index := range evidence.CandidateCensuses {
		if evidence.CandidateCensuses[index].Milestone == name {
			return &evidence.CandidateCensuses[index]
		}
	}
	return nil
}

func fleetLifecycleApplicationHead(census *FleetLifecycleCandidateCensus) uint64 {
	if census == nil || len(census.Validators) != 2 {
		return 0
	}
	return max(census.Validators[0].Application.Number, census.Validators[1].Application.Number)
}

func validateFleetLifecycleProductionSchedulePredecessor(evidence *FleetLifecycleEvidence) error {
	if evidence == nil || evidence.ProductionNativeSchedule == nil || evidence.TerminalRegistration == nil {
		return errors.New("fleet lifecycle production schedule predecessor is incomplete")
	}
	provider := fleetLifecycleMilestone(evidence, fleetLifecycleMilestoneProviderActive)
	if provider == nil || evidence.ProductionNativeSchedule.ObservedHead.Number <= evidence.TerminalRegistration.BlockNumber || evidence.ProductionNativeSchedule.ObservedHead.Number <= fleetLifecycleApplicationHead(provider) {
		return errors.New("fleet lifecycle production schedule predates the provider decision or terminal registration")
	}
	return nil
}

func (self *liveFleetLifecycle) validatePersistedPayouts(evidence *FleetLifecycleEvidence, rank int) error {
	want := map[string]struct {
		epoch    uint64
		miners   []int
		excluded bool
	}{}
	if rank >= 2 {
		fallback, err := self.fallbackMembers()
		if err != nil {
			return err
		}
		want["pruned-provider-returned-to-operator-pool"] = struct {
			epoch    uint64
			miners   []int
			excluded bool
		}{evidence.FallbackEffectiveEpoch, fleetLifecycleMembers(self.cfg, fleetLifecycleTargetFleet), false}
		want["fallback-provider-head-excluded"] = struct {
			epoch    uint64
			miners   []int
			excluded bool
		}{evidence.FallbackEffectiveEpoch, fallback, true}
	}
	if rank >= 4 {
		want["reregistered-provider-head-excluded"] = struct {
			epoch    uint64
			miners   []int
			excluded bool
		}{evidence.ProviderEffectiveEpoch, fleetLifecycleMembers(self.cfg, fleetLifecycleTargetFleet), true}
		want["second-pruned-provider-returned-to-operator-pool"] = struct {
			epoch    uint64
			miners   []int
			excluded bool
		}{evidence.ProviderEffectiveEpoch, fleetLifecycleMembers(self.cfg, fleetLifecycleCompanionFleet), false}
	}
	if len(evidence.Payouts) != len(want) {
		return fmt.Errorf("fleet lifecycle persisted payout count=%d, want %d at stage %s", len(evidence.Payouts), len(want), evidence.Stage)
	}
	seen := map[string]bool{}
	for _, payout := range evidence.Payouts {
		expected, ok := want[payout.Disposition]
		if !ok || seen[payout.Disposition] || payout.Epoch != expected.epoch || payout.NoID != operatorForMiner(self.cfg, expected.miners[0]) {
			return errors.New("fleet lifecycle persisted payout has a duplicate or wrong disposition, epoch, or operator")
		}
		seen[payout.Disposition] = true
		clientIDs, err := fleetLifecycleClientIDs(self.executor.roles, expected.miners)
		if err != nil || !slices.Equal(payout.ClientIDs, clientIDs) {
			return stateMismatchError(err, "fleet lifecycle persisted payout client census differs from exact provider members")
		}
		contentHash, hashErr := hex.DecodeString(strings.TrimPrefix(payout.ContentHash, "sha256:"))
		if _, ok := evidenceFixedHex(payout.PayoutRoot, 32); !ok || hashErr != nil || len(contentHash) != 32 || payout.ContentHash != "sha256:"+hex.EncodeToString(contentHash) {
			return errors.New("fleet lifecycle persisted payout lacks a canonical artifact hash or payout root")
		}
	}
	return nil
}

// fleetLifecycleBlockRange is retained for settlement-artifact readers. It
// maps a release settlement offset to its EVM block interval and never uses a
// native subnet-epoch counter.
func fleetLifecycleBlockRange(evidence *FleetLifecycleEvidence, epochOffset uint64) (uint64, uint64, error) {
	if evidence == nil || evidence.AcceptanceStartBlock == 0 || epochOffset > 5 {
		return 0, 0, errors.New("fleet lifecycle acceptance block range is unavailable")
	}
	if epochOffset == 5 {
		if evidence.AcceptanceEndBlock == 0 || evidence.AcceptanceTerminalBlock <= evidence.AcceptanceEndBlock {
			return 0, 0, errors.New("fleet lifecycle terminal block range is invalid")
		}
		return evidence.AcceptanceEndBlock, evidence.AcceptanceTerminalBlock + 1, nil
	}
	epochBlocks := (evidence.AcceptanceEndBlock - evidence.AcceptanceStartBlock) / 5
	if epochBlocks == 0 || epochBlocks*5 != evidence.AcceptanceEndBlock-evidence.AcceptanceStartBlock {
		return 0, 0, errors.New("fleet lifecycle release settlement geometry is invalid")
	}
	offset, ok := checkedMul(epochOffset, epochBlocks)
	if !ok {
		return 0, 0, errors.New("fleet lifecycle block offset overflows")
	}
	start, ok := checkedAdd(evidence.AcceptanceStartBlock, offset)
	if !ok {
		return 0, 0, errors.New("fleet lifecycle block start overflows")
	}
	end, ok := checkedAdd(start, epochBlocks)
	if !ok {
		return 0, 0, errors.New("fleet lifecycle block end overflows")
	}
	return start, end, nil
}

func fleetLifecycleBlockInRange(block, start, end uint64) bool {
	return block >= start && block < end
}

func validateFleetLifecycleArtifactBlocks(evidence *FleetLifecycleEvidence, rank int) error {
	if evidence == nil || evidence.AcceptanceStartBlock == 0 || evidence.AcceptanceTerminalBlock < evidence.AcceptanceStartBlock {
		return errors.New("fleet lifecycle release mutation window is unavailable")
	}
	nativeTerminal := evidence.AcceptanceTerminalBlock
	if evidence.ReleaseHandoffSchedule != nil && evidence.ReleaseHandoffSchedule.ApplicationDeadlineBlock > nativeTerminal {
		nativeTerminal = evidence.ReleaseHandoffSchedule.ApplicationDeadlineBlock
	}
	nativeEnd, nativeEndOK := checkedAdd(nativeTerminal, 1)
	evmTerminal := evidence.ReleaseEVMEvidenceDeadlineBlock
	if evmTerminal < evidence.AcceptanceTerminalBlock {
		evmTerminal = evidence.AcceptanceTerminalBlock
	}
	evmEnd, evmEndOK := checkedAdd(evmTerminal, 1)
	if !nativeEndOK || !evmEndOK {
		return errors.New("fleet lifecycle release mutation terminal overflows")
	}
	checkRegistration := func(name string, registration *FleetLifecycleRegistrationEvidence, predecessor *FleetLifecycleCandidateCensus) error {
		if registration == nil || !fleetLifecycleBlockInRange(registration.BlockNumber, evidence.AcceptanceStartBlock, nativeEnd) || registration.PrePrune.Head.Number < fleetLifecycleApplicationHead(predecessor) || registration.PrePrune.Head.Number > registration.BlockNumber {
			return fmt.Errorf("fleet lifecycle %s registration is outside the release window or predates its applied native decision", name)
		}
		return nil
	}
	checkCleanup := func(name string, cleanup []FleetLifecycleCleanupEvidence, minimumEpoch, maximumEpoch uint64) error {
		for _, item := range cleanup {
			if !fleetLifecycleBlockInRange(item.BlockNumber, evidence.AcceptanceStartBlock, evmEnd) || item.CleanedAtEpoch < minimumEpoch || item.CleanedAtEpoch >= maximumEpoch {
				return fmt.Errorf("fleet lifecycle %s cleanup is outside its dynamic settlement interval", name)
			}
		}
		return nil
	}
	if rank >= 1 {
		if err := checkRegistration("fallback", evidence.FallbackRegistration, fleetLifecycleMilestone(evidence, fleetLifecycleMilestoneTakeoverRejected)); err != nil {
			return err
		}
	}
	if rank >= 3 {
		if err := checkCleanup("target", evidence.TargetCleanup, evidence.FallbackEffectiveEpoch, evidence.ProviderEffectiveEpoch); err != nil {
			return err
		}
		if err := checkRegistration("provider", evidence.ProviderRegistration, fleetLifecycleMilestone(evidence, fleetLifecycleMilestoneFallbackActive)); err != nil {
			return err
		}
	}
	if rank >= 5 {
		if err := checkCleanup("companion", evidence.CompanionCleanup, evidence.ProviderEffectiveEpoch, evidence.TerminalEffectiveEpoch); err != nil {
			return err
		}
		if err := checkCleanup("fallback", evidence.FallbackCleanup, evidence.ProviderEffectiveEpoch, evidence.TerminalEffectiveEpoch); err != nil {
			return err
		}
		if err := checkRegistration("terminal", evidence.TerminalRegistration, fleetLifecycleMilestone(evidence, fleetLifecycleMilestoneProviderActive)); err != nil {
			return err
		}
	}
	return nil
}

func validateFleetLifecycleCensusBlockRange(evidence *FleetLifecycleEvidence, census FleetLifecycleCandidateCensus) error {
	if evidence == nil || len(census.Validators) != 2 {
		return errors.New("fleet lifecycle census has no phase window")
	}
	var start, acceptanceTerminal, evmDeadline, nativeDeadline, firstSettlement, settlementBlocks uint64
	switch census.Phase {
	case "release-1.0":
		start, acceptanceTerminal, evmDeadline, firstSettlement, settlementBlocks = evidence.AcceptanceStartBlock, evidence.AcceptanceTerminalBlock, evidence.ReleaseEVMEvidenceDeadlineBlock, evidence.FirstAcceptedEpoch, 300
		if evidence.ReleaseHandoffSchedule != nil {
			nativeDeadline = evidence.ReleaseHandoffSchedule.ApplicationDeadlineBlock
		}
	case "production-soak":
		start, acceptanceTerminal, evmDeadline, firstSettlement, settlementBlocks = evidence.ProductionAcceptanceStartBlock, evidence.ProductionAcceptanceTerminalBlock, evidence.ProductionEVMEvidenceDeadlineBlock, evidence.ProductionFirstSettlementEpoch, 360
		if evidence.ProductionNativeSchedule != nil {
			nativeDeadline = evidence.ProductionNativeSchedule.ApplicationDeadlineBlock
		}
	default:
		return errors.New("fleet lifecycle census phase is invalid")
	}
	if start == 0 || firstSettlement == 0 || settlementBlocks == 0 || evmDeadline < acceptanceTerminal || nativeDeadline == 0 || census.ObservedHead.Number < start {
		return errors.New("fleet lifecycle census phase bounds are incomplete")
	}
	var postAcceptance *bool
	for _, validator := range census.Validators {
		if validator.EVMSnapshot.Number < start || validator.EVMSnapshot.Number > evmDeadline || validator.EVMSnapshot.Number > census.ObservedHead.Number || validator.Application.Number > nativeDeadline {
			return errors.New("fleet lifecycle validator receipt crosses its independent settlement/native phase bounds")
		}
		snapshotOffset := (validator.EVMSnapshot.Number - start) / settlementBlocks
		snapshotSettlement, snapshotOK := checkedAdd(firstSettlement, snapshotOffset)
		if !snapshotOK || validator.SettlementEpoch != snapshotSettlement || validator.NativeSnapshot.Number > validator.Commit.Number || validator.Commit.Number > validator.RevealBlock || validator.RevealBlock > validator.Application.Number || validator.Application.Number > census.NativeObservedHead.Number {
			return errors.New("fleet lifecycle validator receipt crosses its independent settlement/native phase bounds")
		}
		classified := validator.EVMSnapshot.Number > acceptanceTerminal
		if postAcceptance != nil && *postAcceptance != classified {
			return errors.New("fleet lifecycle validators straddle the acceptance/tail boundary")
		}
		postAcceptance = &classified
	}
	if postAcceptance == nil || census.PostAcceptance != *postAcceptance {
		return errors.New("fleet lifecycle census post-acceptance label differs from its validator EVM snapshots")
	}
	return nil
}

func validateFleetLifecycleWindow(start, end, terminal, epochs, blocks, finalize uint64) error {
	span, ok := checkedMul(epochs, blocks)
	wantEnd, endOK := checkedAdd(start, span)
	wantTerminal, terminalOK := checkedAdd(end, finalize)
	if !ok || !endOK || !terminalOK || start == 0 || end != wantEnd || terminal != wantTerminal {
		return fmt.Errorf("fleet lifecycle window is not exact %dx%d+%d", epochs, blocks, finalize)
	}
	return nil
}

func (self *liveFleetLifecycle) validateMilestone(evidence *FleetLifecycleEvidence, name, phase string, rejected bool, expected map[int]struct {
	uid  uint16
	role string
}) (*FleetLifecycleCandidateCensus, error) {
	census := fleetLifecycleMilestone(evidence, name)
	if census == nil || census.Phase != phase || !self.censusHasExactFleetRoles(census, expected) {
		return nil, fmt.Errorf("fleet lifecycle milestone %s lacks exact phase/role evidence", name)
	}
	uids := make([]uint16, 0, len(expected))
	for _, identity := range expected {
		uids = append(uids, identity.uid)
	}
	if rejected && !fleetLifecycleCensusRejects(*census, uids...) || !rejected && !fleetLifecycleCensusSelects(*census, uids...) {
		return nil, fmt.Errorf("fleet lifecycle milestone %s has wrong applied validator weights", name)
	}
	return census, nil
}

func (self *liveFleetLifecycle) validatePersistedState(runID string, evidence *FleetLifecycleEvidence) error {
	phase := "release-1.0"
	if evidence != nil && evidence.ProductionRunID == runID {
		phase = "production-soak"
	}
	return self.validatePersistedStateForPhase(phase, runID, evidence)
}

func (self *liveFleetLifecycle) validatePersistedStateForPhase(phase, runID string, evidence *FleetLifecycleEvidence) error {
	if evidence == nil || self.cfg == nil || self.cfg.Config == nil || self.executor == nil || self.executor.plan == nil {
		return errors.New("fleet lifecycle persisted-state dependencies are incomplete")
	}
	rank, ok := fleetLifecycleStageRank(evidence.Stage)
	identityMatches := phase == "release-1.0" && evidence.RunID == runID || phase == "production-soak" && evidence.ProductionRunID == runID
	if !ok || !identityMatches || evidence.Schema != fleetLifecycleEvidenceSchema || evidence.DeploymentID != self.cfg.Config.Deployment.DeploymentID || evidence.PlanHash != self.executor.plan.PlanHash || evidence.RunID == "" || evidence.LaunchPrune == nil || evidence.TakeoverEffectiveEpoch == 0 {
		return errors.New("fleet lifecycle state differs from its run, deployment, plan, launch proof, or stage")
	}
	if phase == "release-1.0" {
		if fleetLifecycleHasProductionState(evidence) || rank == 7 {
			return errors.New("release fleet lifecycle contains production successor state")
		}
	} else {
		if rank != 6 && rank != 7 {
			return errors.New("production fleet lifecycle does not append the exact release handoff")
		}
		handoffHash, hashErr := fleetLifecycleReleaseHandoffHash(evidence)
		if hashErr != nil || evidence.ReleaseHandoffHash == "" || evidence.ReleaseHandoffHash != handoffHash {
			return stateMismatchError(hashErr, "production fleet lifecycle release handoff hash differs from its release projection")
		}
		if self.authenticatedReleaseHandoff != nil && (evidence.ReleaseHandoffHash != self.authenticatedReleaseHandoffHash || !fleetLifecycleCanonicalEqual(fleetLifecycleReleaseProjection(evidence), self.authenticatedReleaseHandoff)) {
			return errors.New("production fleet lifecycle differs from the exact authenticated release handoff")
		}
	}
	if err := validateFleetLifecycleLaunchSnapshot(*evidence.LaunchPrune, self.executor.roles); err != nil {
		return fmt.Errorf("fleet lifecycle launch proof: %w", err)
	}
	if evidence.FirstAcceptedEpoch == 0 {
		if phase != "release-1.0" || rank != 0 || evidence.AcceptanceStartBlock != 0 || evidence.AcceptanceEndBlock != 0 || evidence.AcceptanceTerminalBlock != 0 || evidence.ProductionRunID != "" || evidence.FallbackRegistration != nil || evidence.ProviderRegistration != nil || evidence.TerminalRegistration != nil || len(evidence.TargetCleanup) != 0 || len(evidence.CompanionCleanup) != 0 || len(evidence.FallbackCleanup) != 0 || len(evidence.Payouts) != 0 || len(evidence.CandidateCensuses) != 0 || evidence.FallbackEffectiveEpoch != 0 || evidence.ProviderEffectiveEpoch != 0 || evidence.TerminalEffectiveEpoch != 0 || evidence.PostRegistrationRewardBaseline.Number != 0 {
			return errors.New("fleet lifecycle advanced before an acceptance window was bound")
		}
		return nil
	}
	if err := validateFleetLifecycleWindow(evidence.AcceptanceStartBlock, evidence.AcceptanceEndBlock, evidence.AcceptanceTerminalBlock, 5, 300, 150); err != nil {
		return err
	}
	if evidence.TakeoverEffectiveEpoch != evidence.FirstAcceptedEpoch {
		return errors.New("fleet lifecycle takeover binding is not effective at the first release settlement epoch")
	}
	if evidence.ReleaseHandoffSchedule != nil {
		if err := validateFleetLifecycleNativeSchedule(evidence.ReleaseHandoffSchedule, "release-1.0", evidence.AcceptanceStartBlock, evidence.AcceptanceTerminalBlock); err != nil {
			return err
		}
		expectedDeadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(evidence.AcceptanceTerminalBlock, evidence.ReleaseHandoffSchedule)
		if err != nil {
			return err
		}
		if evidence.ReleaseEVMEvidenceDeadlineBlock != expectedDeadline {
			return errors.New("fleet lifecycle release EVM evidence bound differs from its acceptance/native maximum")
		}
	} else if rank > 0 {
		return errors.New("fleet lifecycle mutation exists without its signed release handoff schedule")
	} else if evidence.ReleaseEVMEvidenceDeadlineBlock != 0 {
		return errors.New("fleet lifecycle release EVM evidence bound exists without its signed schedule")
	}
	if evidence.ProductionFirstSettlementEpoch != 0 {
		if evidence.ProductionRunID == "" || evidence.ProductionFirstSettlementEpoch < evidence.TerminalEffectiveEpoch || validateFleetLifecycleWindow(evidence.ProductionAcceptanceStartBlock, evidence.ProductionAcceptanceEndBlock, evidence.ProductionAcceptanceTerminalBlock, 3, 360, 180) != nil {
			return errors.New("fleet lifecycle production window is incomplete")
		}
	} else if evidence.ProductionAcceptanceStartBlock != 0 || evidence.ProductionAcceptanceEndBlock != 0 || evidence.ProductionAcceptanceTerminalBlock != 0 {
		return errors.New("fleet lifecycle production geometry exists without its first settlement epoch")
	}
	if evidence.ProductionNativeSchedule != nil {
		if evidence.ProductionFirstSettlementEpoch == 0 {
			return errors.New("fleet lifecycle native schedule exists without a production window")
		}
		if err := validateFleetLifecycleNativeSchedule(evidence.ProductionNativeSchedule, "production-soak", evidence.ProductionAcceptanceStartBlock, evidence.ProductionAcceptanceTerminalBlock); err != nil {
			return err
		}
		expectedDeadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(evidence.ProductionAcceptanceTerminalBlock, evidence.ProductionNativeSchedule)
		if err != nil {
			return err
		}
		if evidence.ProductionEVMEvidenceDeadlineBlock != expectedDeadline {
			return errors.New("fleet lifecycle production EVM evidence bound differs from its acceptance/native maximum")
		}
		if err := validateFleetLifecycleProductionSchedulePredecessor(evidence); err != nil {
			return err
		}
	} else if evidence.ProductionEVMEvidenceDeadlineBlock != 0 {
		return errors.New("fleet lifecycle production EVM evidence bound exists without its signed schedule")
	}
	seenApplications := map[string]bool{}
	seenMilestones := map[string]bool{}
	for index := range evidence.CandidateCensuses {
		census := &evidence.CandidateCensuses[index]
		if err := validateFleetLifecyclePersistedCensus(*census); err != nil {
			return err
		}
		if err := validateFleetLifecycleCensusBlockRange(evidence, *census); err != nil {
			return err
		}
		key := fmt.Sprintf("%d/%s/%d/%s", census.Validators[0].Application.Number, census.Validators[0].Application.Hash, census.Validators[1].Application.Number, census.Validators[1].Application.Hash)
		if seenApplications[key] {
			return errors.New("fleet lifecycle persisted census reuses an application receipt")
		}
		seenApplications[key] = true
		if census.Milestone != "" {
			if seenMilestones[census.Milestone] {
				return errors.New("fleet lifecycle persisted census duplicates a milestone")
			}
			seenMilestones[census.Milestone] = true
		}
	}
	takeover, takeoverErr := self.validateMilestone(evidence, fleetLifecycleMilestoneTakeoverRejected, "release-1.0", true, map[int]struct {
		uid  uint16
		role string
	}{fleetLifecycleTargetFleet: {fleetLifecycleTargetExpectedUID, churnHotkeyLabel(fleetLifecycleTargetChurn)}, fleetLifecycleCompanionFleet: {fleetLifecycleCompanionExpectedUID, churnHotkeyLabel(fleetLifecycleCompanionChurn)}})
	if rank >= 1 {
		if takeoverErr != nil || evidence.FallbackRegistration == nil || evidence.FallbackEffectiveEpoch <= evidence.TakeoverEffectiveEpoch || validateFleetLifecycleRegistrationEvidence(*evidence.FallbackRegistration) != nil {
			return stateMismatchError(takeoverErr, "fleet lifecycle fallback stage lacks its takeover decision and dynamic replacement evidence")
		}
	} else if takeover != nil || evidence.FallbackRegistration != nil || evidence.FallbackEffectiveEpoch != 0 {
		return errors.New("fleet lifecycle awaiting stage contains future fallback evidence")
	}
	fallback, fallbackErr := self.validateMilestone(evidence, fleetLifecycleMilestoneFallbackActive, "release-1.0", true, map[int]struct {
		uid  uint16
		role string
	}{fleetLifecycleTargetFleet: {fleetLifecycleTargetExpectedUID, churnHotkeyLabel(fleetLifecycleFallbackChurn)}, fleetLifecycleCompanionFleet: {fleetLifecycleCompanionExpectedUID, churnHotkeyLabel(fleetLifecycleCompanionChurn)}})
	if rank >= 2 {
		if fallbackErr != nil {
			return stateMismatchError(fallbackErr, "fleet lifecycle fallback payout lacks its exact applied decision")
		}
	} else if fallback != nil {
		return errors.New("fleet lifecycle pre-fallback-payout stage contains a future fallback milestone")
	}
	if rank >= 3 {
		if evidence.ProviderRegistration == nil || evidence.ProviderEffectiveEpoch <= evidence.FallbackEffectiveEpoch || validateFleetLifecycleRegistrationEvidence(*evidence.ProviderRegistration) != nil || len(evidence.TargetCleanup) != self.cfg.Config.Topology.ClientsPerHeadFleet {
			return errors.New("fleet lifecycle provider stage lacks its payout, cleanup, or replacement evidence")
		}
	} else if evidence.ProviderRegistration != nil || evidence.ProviderEffectiveEpoch != 0 || len(evidence.TargetCleanup) != 0 {
		return errors.New("fleet lifecycle pre-provider stage contains future provider evidence")
	}
	provider, providerErr := self.validateMilestone(evidence, fleetLifecycleMilestoneProviderActive, "release-1.0", true, map[int]struct {
		uid  uint16
		role string
	}{fleetLifecycleTargetFleet: {fleetLifecycleCompanionExpectedUID, churnHotkeyLabel(fleetLifecycleTargetChurn)}, fleetLifecycleCompanionFleet: {fleetLifecycleTargetExpectedUID, churnHotkeyLabel(fleetLifecycleFallbackChurn)}})
	if evidence.PostRegistrationRewardBaseline.Number != 0 {
		if rank < 3 || evidence.ProviderRegistration == nil || evidence.PostRegistrationRewardBaseline.Number <= evidence.ProviderRegistration.BlockNumber || provider != nil && evidence.PostRegistrationRewardBaseline.Number > provider.Validators[0].NativeSnapshot.Number {
			return errors.New("fleet lifecycle reward baseline does not follow the exact provider registration")
		}
		if _, ok := evidenceFixedHex(evidence.PostRegistrationRewardBaseline.Hash, 32); !ok {
			return errors.New("fleet lifecycle reward baseline hash is noncanonical")
		}
	}
	if rank >= 4 {
		if providerErr != nil || evidence.PostRegistrationRewardBaseline.Number == 0 {
			return stateMismatchError(providerErr, "fleet lifecycle provider payout lacks its exact applied decision or reward baseline")
		}
	} else if provider != nil {
		return errors.New("fleet lifecycle pre-provider-payout stage contains a future provider milestone")
	}
	if rank >= 5 {
		if evidence.TerminalRegistration == nil || evidence.TerminalEffectiveEpoch <= evidence.ProviderEffectiveEpoch || validateFleetLifecycleRegistrationEvidence(*evidence.TerminalRegistration) != nil || len(evidence.CompanionCleanup) != self.cfg.Config.Topology.ClientsPerHeadFleet || len(evidence.FallbackCleanup) != self.cfg.Config.Topology.ClientsPerHeadFleet {
			return errors.New("fleet lifecycle terminal stage lacks its payout, cleanup, or replacement evidence")
		}
	} else if evidence.TerminalRegistration != nil || evidence.TerminalEffectiveEpoch != 0 || len(evidence.CompanionCleanup) != 0 || len(evidence.FallbackCleanup) != 0 {
		return errors.New("fleet lifecycle pre-terminal stage contains future terminal evidence")
	}
	terminal, terminalErr := self.validateMilestone(evidence, fleetLifecycleMilestoneTerminalActive, "production-soak", false, map[int]struct {
		uid  uint16
		role string
	}{fleetLifecycleTargetFleet: {fleetLifecycleCompanionExpectedUID, churnHotkeyLabel(fleetLifecycleTargetChurn)}, fleetLifecycleCompanionFleet: {fleetLifecycleTerminalVictimUID, churnHotkeyLabel(fleetLifecycleCompanionChurn)}})
	if rank == 7 {
		if terminalErr != nil || evidence.ProductionFirstSettlementEpoch == 0 || evidence.ProductionRunID == "" || evidence.ProductionNativeSchedule == nil {
			return stateMismatchError(terminalErr, "fleet lifecycle composite completion lacks the production terminal-active decision")
		}
	} else if terminal != nil {
		return errors.New("fleet lifecycle phase handoff contains a premature terminal-active milestone")
	}
	ordered := []*FleetLifecycleCandidateCensus{takeover, fallback, provider, terminal}
	previousApplication, previousNativeEpoch := uint64(0), uint64(0)
	for _, milestone := range ordered {
		if milestone == nil {
			continue
		}
		application := fleetLifecycleApplicationHead(milestone)
		if application <= previousApplication || milestone.Validators[0].SubnetEpoch <= previousNativeEpoch {
			return errors.New("fleet lifecycle milestones are not strictly ordered by native decision/application")
		}
		previousApplication, previousNativeEpoch = application, milestone.Validators[0].SubnetEpoch
	}
	if err := validateFleetLifecycleArtifactBlocks(evidence, rank); err != nil {
		return err
	}
	return self.validatePersistedPayouts(evidence, rank)
}

func fleetLifecycleCanonicalEqual(left, right any) bool {
	leftHash, leftErr := canonicalHashHex(left)
	rightHash, rightErr := canonicalHashHex(right)
	return leftErr == nil && rightErr == nil && leftHash == rightHash
}

func (self *liveFleetLifecycle) validateVariantLineage(ctx context.Context, variantName string, expectedEffectiveEpoch, blockStart, nativeEnd, evmEnd uint64) error {
	manifest, commitmentHash, commitment, err := self.executor.fleetLifecycleManifestAndCommitment(variantName)
	if err != nil {
		return fmt.Errorf("fleet lifecycle %s commitment artifact: %w", variantName, err)
	}
	commitmentActionID, err := fleetLifecycleCommitmentActionID(variantName)
	if err != nil {
		return err
	}
	commitmentAction, err := self.executor.planAction(commitmentActionID)
	if err != nil {
		return err
	}
	if err := self.executor.validateFleetLifecycleCommitmentAction(ctx, commitmentAction, variantName, manifest, commitmentHash, *commitment); err != nil {
		return fmt.Errorf("fleet lifecycle %s commitment lineage: %w", variantName, err)
	}
	if !fleetLifecycleBlockInRange(commitment.FinalizedBlock, blockStart, nativeEnd) {
		return fmt.Errorf("fleet lifecycle %s commitment block=%d is outside native range [%d,%d)", variantName, commitment.FinalizedBlock, blockStart, nativeEnd)
	}
	var mirror FleetLifecycleMirrorEvidence
	if err := readJSONFile(filepath.Join(self.stateDir, "public", fleetLifecycleMirrorEvidenceName(variantName)), &mirror); err != nil {
		return fmt.Errorf("fleet lifecycle %s mirror artifact: %w", variantName, err)
	}
	mirrorActionID, err := fleetLifecycleMirrorActionID(variantName)
	if err != nil {
		return err
	}
	mirrorAction, err := self.executor.planAction(mirrorActionID)
	if err != nil {
		return err
	}
	if err := self.executor.validateFleetLifecycleMirrorAction(ctx, mirrorAction, variantName, manifest, commitmentHash, *commitment, mirror); err != nil {
		return fmt.Errorf("fleet lifecycle %s mirror lineage: %w", variantName, err)
	}
	if !fleetLifecycleBlockInRange(mirror.BlockNumber, blockStart, evmEnd) {
		return fmt.Errorf("fleet lifecycle %s mirror block=%d is outside EVM range [%d,%d)", variantName, mirror.BlockNumber, blockStart, evmEnd)
	}
	finalized, err := finalizedEVMHead(ctx, self.executor.keeper.client)
	if err != nil {
		return err
	}
	for member := 1; member <= self.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		binding, verifyErr := self.executor.verifyFleetLifecycleBindingAt(ctx, variantName, member, finalized)
		if verifyErr != nil {
			return fmt.Errorf("fleet lifecycle %s member %d binding lineage: %w", variantName, member, verifyErr)
		}
		if binding.ValidFromEpoch != expectedEffectiveEpoch {
			return fmt.Errorf("fleet lifecycle %s member %d effective epoch=%d, want %d", variantName, member, binding.ValidFromEpoch, expectedEffectiveEpoch)
		}
		if !fleetLifecycleBlockInRange(binding.BlockNumber, blockStart, evmEnd) {
			return fmt.Errorf("fleet lifecycle %s member %d binding block=%d is outside EVM range [%d,%d)", variantName, member, binding.BlockNumber, blockStart, evmEnd)
		}
	}
	return nil
}

func (self *liveFleetLifecycle) validateEmbeddedRegistration(ctx context.Context, variantName string, embedded *FleetLifecycleRegistrationEvidence) error {
	if embedded == nil {
		return fmt.Errorf("fleet lifecycle %s registration is absent", variantName)
	}
	verified, err := self.readRegistration(ctx, variantName)
	if err != nil {
		return err
	}
	if !fleetLifecycleCanonicalEqual(verified, embedded) {
		return fmt.Errorf("fleet lifecycle %s embedded registration differs from its exact public action artifact", variantName)
	}
	return nil
}

func (self *liveFleetLifecycle) validateEmbeddedCleanup(ctx context.Context, variantName string, embedded []FleetLifecycleCleanupEvidence) error {
	verified, err := self.readCleanup(ctx, variantName)
	if err != nil {
		return err
	}
	if !fleetLifecycleCanonicalEqual(verified, embedded) {
		return fmt.Errorf("fleet lifecycle %s embedded cleanup census differs from its exact public action artifacts", variantName)
	}
	return nil
}

// validateResumeLineage prevents a syntactically coherent lifecycle JSON from
// skipping any native or EVM mutation after a process restart. Every installed
// wave is replayed at its finalized receipt/checkpoint and matched to the exact
// approved journal action before Complete can become true.
func (self *liveFleetLifecycle) validateResumeLineage(ctx context.Context) error {
	if self.resumeValidated {
		return nil
	}
	runID := self.evidence.RunID
	if self.phase == "production-soak" {
		runID = self.evidence.ProductionRunID
	}
	if err := self.validatePersistedStateForPhase(self.phase, runID, self.evidence); err != nil {
		return err
	}
	launchHash, err := types.NewHashFromHexString(self.evidence.LaunchPrune.Head.Hash)
	if err != nil {
		return err
	}
	launch, err := self.executor.substrate.fleetLifecyclePruneSnapshotAt(launchHash, self.evidence.LaunchPrune.Head.Number)
	if err != nil || !fleetLifecycleCanonicalEqual(launch, *self.evidence.LaunchPrune) {
		return stateMismatchError(err, "fleet lifecycle launch prune proof differs from canonical public chain state")
	}
	if err := self.validateVariantLineage(ctx, fleetLifecycleVariantTargetTakeover, self.evidence.TakeoverEffectiveEpoch, 1, self.evidence.AcceptanceStartBlock, self.evidence.AcceptanceStartBlock); err != nil {
		return err
	}
	if err := self.validateVariantLineage(ctx, fleetLifecycleVariantCompanionTakeover, self.evidence.TakeoverEffectiveEpoch, 1, self.evidence.AcceptanceStartBlock, self.evidence.AcceptanceStartBlock); err != nil {
		return err
	}
	rank, _ := fleetLifecycleStageRank(self.evidence.Stage)
	nativeLineageTerminal := self.evidence.AcceptanceTerminalBlock
	if self.evidence.ReleaseHandoffSchedule != nil && self.evidence.ReleaseHandoffSchedule.ApplicationDeadlineBlock > nativeLineageTerminal {
		nativeLineageTerminal = self.evidence.ReleaseHandoffSchedule.ApplicationDeadlineBlock
	}
	nativeLineageEnd, nativeOK := checkedAdd(nativeLineageTerminal, 1)
	evmLineageTerminal := self.evidence.ReleaseEVMEvidenceDeadlineBlock
	if evmLineageTerminal < self.evidence.AcceptanceTerminalBlock {
		evmLineageTerminal = self.evidence.AcceptanceTerminalBlock
	}
	evmLineageEnd, evmOK := checkedAdd(evmLineageTerminal, 1)
	if !nativeOK || !evmOK {
		return errors.New("fleet lifecycle release lineage terminal overflows")
	}
	if rank >= 1 {
		if err := self.validateEmbeddedRegistration(ctx, fleetLifecycleVariantFallback, self.evidence.FallbackRegistration); err != nil {
			return err
		}
		blockStart := fleetLifecycleApplicationHead(fleetLifecycleMilestone(self.evidence, fleetLifecycleMilestoneTakeoverRejected))
		if err := self.validateVariantLineage(ctx, fleetLifecycleVariantFallback, self.evidence.FallbackEffectiveEpoch, blockStart, nativeLineageEnd, evmLineageEnd); err != nil {
			return err
		}
	}
	if rank >= 3 {
		if err := self.validateEmbeddedRegistration(ctx, fleetLifecycleVariantProvider, self.evidence.ProviderRegistration); err != nil {
			return err
		}
		if err := self.validateEmbeddedCleanup(ctx, fleetLifecycleVariantTargetTakeover, self.evidence.TargetCleanup); err != nil {
			return err
		}
		blockStart := fleetLifecycleApplicationHead(fleetLifecycleMilestone(self.evidence, fleetLifecycleMilestoneFallbackActive))
		if err := self.validateVariantLineage(ctx, fleetLifecycleVariantProvider, self.evidence.ProviderEffectiveEpoch, blockStart, nativeLineageEnd, evmLineageEnd); err != nil {
			return err
		}
	}
	if rank >= 5 {
		if err := self.validateEmbeddedRegistration(ctx, fleetLifecycleVariantTerminal, self.evidence.TerminalRegistration); err != nil {
			return err
		}
		if err := self.validateEmbeddedCleanup(ctx, fleetLifecycleVariantCompanionTakeover, self.evidence.CompanionCleanup); err != nil {
			return err
		}
		if err := self.validateEmbeddedCleanup(ctx, fleetLifecycleVariantFallback, self.evidence.FallbackCleanup); err != nil {
			return err
		}
		blockStart := fleetLifecycleApplicationHead(fleetLifecycleMilestone(self.evidence, fleetLifecycleMilestoneProviderActive))
		if err := self.validateVariantLineage(ctx, fleetLifecycleVariantTerminal, self.evidence.TerminalEffectiveEpoch, blockStart, nativeLineageEnd, evmLineageEnd); err != nil {
			return err
		}
	}
	self.resumeValidated = true
	return nil
}

func fleetLifecycleScheduleRequired(milestones, mutations, tempo, revealPeriods, mutationSafety uint64) (uint64, error) {
	if milestones == 0 {
		return 0, errors.New("fleet lifecycle schedule has no required milestone")
	}
	decisionPeriods, ok := checkedAdd(revealPeriods, 1)
	decisionBlocks, decisionOK := checkedMul(decisionPeriods, tempo)
	milestoneBlocks, milestoneOK := checkedMul(milestones, decisionBlocks)
	mutationBlocks, mutationOK := checkedMul(mutations, mutationSafety)
	required, requiredOK := checkedAdd(milestoneBlocks, mutationBlocks)
	if !ok || !decisionOK || !milestoneOK || !mutationOK || !requiredOK || tempo == 0 {
		return 0, errors.New("fleet lifecycle combined schedule overflows or has zero tempo")
	}
	return required, nil
}

func fleetLifecycleCombinedScheduleRequired(tempo, revealPeriods, mutationSafety uint64) (uint64, error) {
	return fleetLifecycleScheduleRequired(4, 3, tempo, revealPeriods, mutationSafety)
}

func fleetLifecycleReleaseScheduleRequired(tempo, revealPeriods uint64) (uint64, error) {
	return fleetLifecycleScheduleRequired(3, 3, tempo, revealPeriods, fleetLifecycleMutationSafetyBlocks)
}

func (self *liveFleetLifecycle) BindAcceptanceWindow(window *ScenarioAcceptanceWindow) error {
	return self.BindAcceptanceWindowForPhase("release-1.0", window)
}

func (self *liveFleetLifecycle) BindAcceptanceWindowForPhase(phase string, window *ScenarioAcceptanceWindow) error {
	if self == nil || self.evidence == nil || self.cfg == nil || self.cfg.Policy == nil || window == nil {
		return errors.New("fleet lifecycle acceptance window is unavailable")
	}
	if self.phase != "" && self.phase != phase {
		return errors.New("fleet lifecycle acceptance window phase differs from its initialized phase")
	}
	switch phase {
	case "release-1.0":
		if window.EpochCount != 5 || window.EpochBlocks != 300 || window.FinalizeOffsetBlocks != 150 || self.cfg.Config.Scenarios.ShortEpochs != 5 || self.cfg.Policy.Settlement.EpochBlocks != 300 || self.cfg.Policy.Settlement.FinalizeOffsetBlocks != 150 {
			return fmt.Errorf("fleet lifecycle requires exact release settlement geometry 5x300+150, got %dx%d+%d", window.EpochCount, window.EpochBlocks, window.FinalizeOffsetBlocks)
		}
		if self.evidence.TakeoverEffectiveEpoch != window.FirstEpoch {
			return fmt.Errorf("fleet lifecycle takeover effective settlement epoch=%d, want first accepted settlement epoch=%d", self.evidence.TakeoverEffectiveEpoch, window.FirstEpoch)
		}
		if self.evidence.FirstAcceptedEpoch != 0 && (self.evidence.FirstAcceptedEpoch != window.FirstEpoch || self.evidence.AcceptanceStartBlock != window.StartBlock || self.evidence.AcceptanceEndBlock != window.EndBlock || self.evidence.AcceptanceTerminalBlock != window.TerminalBlock) {
			return errors.New("fleet lifecycle persisted release settlement window changed")
		}
		self.evidence.FirstAcceptedEpoch = window.FirstEpoch
		self.evidence.AcceptanceStartBlock = window.StartBlock
		self.evidence.AcceptanceEndBlock = window.EndBlock
		self.evidence.AcceptanceTerminalBlock = window.TerminalBlock
	case "production-soak":
		if (self.evidence.Stage != fleetLifecycleStageReleaseHandoff && self.evidence.Stage != fleetLifecycleStageComplete) || self.evidence.ProductionRunID == "" || self.evidence.ReleaseHandoffHash == "" {
			return errors.New("fleet lifecycle production window has no authenticated release handoff")
		}
		if window.EpochCount != 3 || window.EpochBlocks != 360 || window.FinalizeOffsetBlocks != 180 || self.cfg.Config.Scenarios.ProductionEpochs != 3 || self.cfg.Policy.ProductionCadence.EpochBlocks != 360 || self.cfg.Policy.ProductionCadence.FinalizeOffsetBlocks != 180 {
			return fmt.Errorf("fleet lifecycle requires exact production settlement geometry 3x360+180, got %dx%d+%d", window.EpochCount, window.EpochBlocks, window.FinalizeOffsetBlocks)
		}
		if self.evidence.ProductionFirstSettlementEpoch != 0 && (self.evidence.ProductionFirstSettlementEpoch != window.FirstEpoch || self.evidence.ProductionAcceptanceStartBlock != window.StartBlock || self.evidence.ProductionAcceptanceEndBlock != window.EndBlock || self.evidence.ProductionAcceptanceTerminalBlock != window.TerminalBlock) {
			return errors.New("fleet lifecycle persisted production settlement window changed")
		}
		if self.evidence.TerminalEffectiveEpoch == 0 || window.FirstEpoch < self.evidence.TerminalEffectiveEpoch {
			return errors.New("fleet lifecycle production window starts before the terminal generation is settlement-effective")
		}
		self.evidence.ProductionFirstSettlementEpoch = window.FirstEpoch
		self.evidence.ProductionAcceptanceStartBlock = window.StartBlock
		self.evidence.ProductionAcceptanceEndBlock = window.EndBlock
		self.evidence.ProductionAcceptanceTerminalBlock = window.TerminalBlock
	default:
		return fmt.Errorf("fleet lifecycle phase %q is unsupported", phase)
	}
	self.resumeValidated = false
	return self.write()
}

func validateFleetLifecycleNativeSchedule(schedule *FleetLifecycleNativeSchedule, phase string, acceptanceStart, acceptanceTerminal uint64) error {
	if schedule == nil || schedule.Phase != phase || schedule.ObservedHead.Number == 0 || schedule.Tempo == 0 || schedule.RevealPeriodEpochs == 0 || schedule.RequiredMilestones == 0 || acceptanceStart == 0 || acceptanceTerminal < acceptanceStart {
		return errors.New("fleet lifecycle native schedule is incomplete")
	}
	wantMilestones, wantMutations, wantApplicationSafety := uint64(1), uint64(0), fleetLifecycleMutationSafetyBlocks
	if phase == "release-1.0" {
		wantMilestones, wantMutations, wantApplicationSafety = 3, 3, 0
	} else if phase != "production-soak" {
		return errors.New("fleet lifecycle native schedule phase is invalid")
	}
	if schedule.RequiredMilestones != wantMilestones || schedule.RequiredMutations != wantMutations || schedule.ApplicationSafetyBlocks != wantApplicationSafety {
		return errors.New("fleet lifecycle native schedule has wrong causal work counts")
	}
	if _, ok := evidenceFixedHex(schedule.ObservedHead.Hash, 32); !ok {
		return errors.New("fleet lifecycle native schedule head hash is noncanonical")
	}
	state := crv4.EpochScheduleState{
		LastEpochBlock: schedule.LastEpochBlock, PendingEpochAt: schedule.PendingEpochAt, SubnetEpochIndex: schedule.SubnetEpoch,
		Tempo: schedule.Tempo, BlocksSinceLastStep: schedule.BlocksSinceLastStep, CurrentBlock: schedule.ObservedHead.Number,
	}
	if state.CurrentBlock < acceptanceStart {
		state.CurrentBlock = acceptanceStart
	}
	reveal, err := crv4.PredictFirstRevealBlock(&state, schedule.RevealPeriodEpochs)
	decisionPeriods, periodsOK := checkedAdd(schedule.RevealPeriodEpochs, 1)
	decisionBlocks, blocksOK := checkedMul(decisionPeriods, uint64(schedule.Tempo))
	remainingDecisionBlocks, remainingOK := checkedMul(schedule.RequiredMilestones-1, decisionBlocks)
	mutationBlocks, mutationOK := checkedMul(schedule.RequiredMutations, fleetLifecycleMutationSafetyBlocks)
	deadline, deadlineOK := checkedAdd(reveal, remainingDecisionBlocks)
	deadline, mutationDeadlineOK := checkedAdd(deadline, mutationBlocks)
	deadline, safetyDeadlineOK := checkedAdd(deadline, schedule.ApplicationSafetyBlocks)
	if err != nil || !periodsOK || !blocksOK || !remainingOK || !mutationOK || !deadlineOK || !mutationDeadlineOK || !safetyDeadlineOK || schedule.FirstQualifyingRevealBlock != reveal || schedule.ApplicationDeadlineBlock != deadline || deadline < acceptanceStart {
		return stateMismatchError(err, "fleet lifecycle native schedule prediction differs from its exact CRv4 inputs")
	}
	maximumSpan, spanErr := fleetLifecycleScheduleRequired(wantMilestones, wantMutations, uint64(schedule.Tempo), schedule.RevealPeriodEpochs, fleetLifecycleMutationSafetyBlocks)
	maximumSpan, safetySpanOK := checkedAdd(maximumSpan, wantApplicationSafety)
	maximumDeadline, maximumOK := checkedAdd(acceptanceStart, maximumSpan)
	if spanErr != nil || !safetySpanOK || !maximumOK || deadline > maximumDeadline {
		return errors.New("fleet lifecycle native schedule exceeds its bounded post-acceptance evidence tail")
	}
	return nil
}

// fleetLifecycleExpectedEVMEvidenceDeadline keeps EVM evidence available
// through the later acceptance/native boundary and requires a representable
// exclusive end for every inclusive range consumer.
func fleetLifecycleExpectedEVMEvidenceDeadline(acceptanceTerminal uint64, schedule *FleetLifecycleNativeSchedule) (uint64, error) {
	if acceptanceTerminal == 0 || schedule == nil || schedule.ApplicationDeadlineBlock == 0 {
		return 0, errors.New("fleet lifecycle EVM evidence deadline inputs are incomplete")
	}
	deadline := max(acceptanceTerminal, schedule.ApplicationDeadlineBlock)
	if _, ok := checkedAdd(deadline, 1); !ok {
		return 0, errors.New("fleet lifecycle EVM evidence deadline overflows its inclusive range")
	}
	return deadline, nil
}

func (self *liveFleetLifecycle) bindNativeSchedule(phase string) error {
	var schedule **FleetLifecycleNativeSchedule
	var evmDeadline *uint64
	var acceptanceStart, acceptanceTerminal uint64
	var requiredMilestones, requiredMutations, applicationSafety uint64
	if phase == "release-1.0" {
		schedule = &self.evidence.ReleaseHandoffSchedule
		evmDeadline = &self.evidence.ReleaseEVMEvidenceDeadlineBlock
		acceptanceStart, acceptanceTerminal = self.evidence.AcceptanceStartBlock, self.evidence.AcceptanceTerminalBlock
		requiredMilestones, requiredMutations = 3, 3
	} else if phase == "production-soak" {
		schedule = &self.evidence.ProductionNativeSchedule
		evmDeadline = &self.evidence.ProductionEVMEvidenceDeadlineBlock
		acceptanceStart, acceptanceTerminal = self.evidence.ProductionAcceptanceStartBlock, self.evidence.ProductionAcceptanceTerminalBlock
		requiredMilestones = 1
		applicationSafety = fleetLifecycleMutationSafetyBlocks
		if self.evidence.TerminalRegistration == nil || fleetLifecycleMilestone(self.evidence, fleetLifecycleMilestoneProviderActive) == nil {
			return errors.New("fleet lifecycle production schedule has no finalized provider milestone and terminal registration")
		}
	} else {
		return errors.New("fleet lifecycle native schedule phase is invalid")
	}
	if *schedule != nil {
		if err := validateFleetLifecycleNativeSchedule(*schedule, phase, acceptanceStart, acceptanceTerminal); err != nil {
			return err
		}
		expectedDeadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(acceptanceTerminal, *schedule)
		if err != nil {
			return err
		}
		if *evmDeadline != expectedDeadline {
			return errors.New("fleet lifecycle EVM evidence bound differs from its persisted acceptance/native maximum")
		}
		return nil
	}
	if self.executor == nil || self.executor.substrate == nil || self.executor.substrate.chain == nil {
		return errors.New("fleet lifecycle production CRv4 schedule reader is unavailable")
	}
	state, blockHash, err := self.executor.substrate.epochScheduleStateFinalized()
	if err != nil {
		return err
	}
	if phase == "production-soak" {
		candidate := *self.evidence
		candidate.ProductionNativeSchedule = &FleetLifecycleNativeSchedule{ObservedHead: ChainHead{Number: state.CurrentBlock, Hash: strings.ToLower(blockHash.Hex())}}
		if err := validateFleetLifecycleProductionSchedulePredecessor(&candidate); err != nil {
			return err
		}
	}
	revealPeriods, err := self.executor.substrate.chain.RevealPeriodEpochsAt(self.cfg.Netuid, blockHash)
	if err != nil {
		return err
	}
	prediction := *state
	if prediction.CurrentBlock < acceptanceStart {
		prediction.CurrentBlock = acceptanceStart
	}
	reveal, err := crv4.PredictFirstRevealBlock(&prediction, revealPeriods)
	if err != nil {
		return err
	}
	decisionPeriods, periodsOK := checkedAdd(revealPeriods, 1)
	decisionBlocks, blocksOK := checkedMul(decisionPeriods, uint64(state.Tempo))
	remainingDecisionBlocks, remainingOK := checkedMul(requiredMilestones-1, decisionBlocks)
	mutationBlocks, mutationOK := checkedMul(requiredMutations, fleetLifecycleMutationSafetyBlocks)
	deadline, deadlineOK := checkedAdd(reveal, remainingDecisionBlocks)
	deadline, mutationDeadlineOK := checkedAdd(deadline, mutationBlocks)
	deadline, safetyDeadlineOK := checkedAdd(deadline, applicationSafety)
	if !periodsOK || !blocksOK || !remainingOK || !mutationOK || !deadlineOK || !mutationDeadlineOK || !safetyDeadlineOK {
		return errors.New("fleet lifecycle production application deadline overflows")
	}
	nativeSchedule := &FleetLifecycleNativeSchedule{
		Phase:        phase,
		ObservedHead: ChainHead{Number: state.CurrentBlock, Hash: strings.ToLower(blockHash.Hex())}, LastEpochBlock: state.LastEpochBlock,
		PendingEpochAt: state.PendingEpochAt, SubnetEpoch: state.SubnetEpochIndex, Tempo: state.Tempo, BlocksSinceLastStep: state.BlocksSinceLastStep,
		RevealPeriodEpochs: revealPeriods, RequiredMilestones: requiredMilestones, RequiredMutations: requiredMutations, ApplicationSafetyBlocks: applicationSafety, FirstQualifyingRevealBlock: reveal, ApplicationDeadlineBlock: deadline,
	}
	if err := validateFleetLifecycleNativeSchedule(nativeSchedule, phase, acceptanceStart, acceptanceTerminal); err != nil {
		return err
	}
	expectedDeadline, err := fleetLifecycleExpectedEVMEvidenceDeadline(acceptanceTerminal, nativeSchedule)
	if err != nil {
		return err
	}
	*schedule = nativeSchedule
	*evmDeadline = expectedDeadline
	return self.write()
}

func (self *liveFleetLifecycle) bindProductionNativeSchedule() error {
	return self.bindNativeSchedule("production-soak")
}

func fleetLifecycleFaultStatus(faults []ScenarioFaultRecord, id, status string) bool {
	for _, fault := range faults {
		if fault.ID == id {
			return fault.Status == status
		}
	}
	return false
}

func fleetLifecycleDecisionCensusAt(observation *ScenarioObservation, phase string, subnetEpoch uint64) (FleetLifecycleCandidateCensus, bool, error) {
	if observation == nil || observation.Status == nil || observation.Status.Contracts == nil || observation.NativeRewards == nil || len(observation.Validators) != 2 {
		return FleetLifecycleCandidateCensus{}, false, nil
	}
	if phase != "release-1.0" && phase != "production-soak" {
		return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("fleet lifecycle census phase %q is invalid", phase)
	}
	if _, ok := evidenceFixedHex(observation.ObservationHash, 32); !ok {
		return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle observation hash is not canonical")
	}
	if _, ok := evidenceFixedHex(observation.Status.Contracts.FinalizedHead.Hash, 32); !ok || observation.Status.Contracts.FinalizedHead.Number == 0 {
		return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle EVM observation head is not canonical")
	}
	if _, ok := evidenceFixedHex(observation.NativeRewards.FinalizedHead.Hash, 32); !ok || observation.NativeRewards.FinalizedHead.Number == 0 {
		return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle native observation head is not canonical")
	}
	result := FleetLifecycleCandidateCensus{
		Phase:           phase,
		ObservationHash: observation.ObservationHash, ObservedHead: observation.Status.Contracts.FinalizedHead, NativeObservedHead: observation.NativeRewards.FinalizedHead,
	}
	seenValidators := make(map[int]bool, len(observation.Validators))
	for _, validator := range observation.Validators {
		if validator.ValidatorID < 1 || validator.ValidatorID > 2 || seenValidators[validator.ValidatorID] {
			return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle validator census has duplicate or foreign identities")
		}
		seenValidators[validator.ValidatorID] = true
		var selected *HeadDecisionObservation
		for index := range validator.HeadDecisions {
			decision := &validator.HeadDecisions[index]
			if decision.SubnetEpoch == subnetEpoch {
				if selected != nil {
					return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle decision duplicates native epoch %d", validator.ValidatorID, subnetEpoch)
				}
				selected = decision
			}
		}
		if selected == nil {
			return FleetLifecycleCandidateCensus{}, false, nil
		}
		if selected.Error != "" || selected.SettlementEpoch == 0 || selected.SettlementEpoch > observation.Status.Contracts.CurrentEpoch || selected.NativeSnapshot.Number == 0 || selected.NativeSnapshot.Number > selected.FinalizedBlock || selected.EVMSnapshot.Number == 0 || selected.EVMSnapshot.Number > observation.Status.Contracts.FinalizedHead.Number || selected.FinalizedBlock == 0 || selected.RevealBlock < selected.FinalizedBlock || selected.ApplicationBlock < selected.RevealBlock || selected.ApplicationBlock > observation.NativeRewards.FinalizedHead.Number || len(selected.CandidateFleetUIDs) != 202 || len(selected.CandidateFleetHotkeys) != 202 || selected.MeasurementArtifactHash == "" {
			return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle decision has no exact successful application receipt", validator.ValidatorID)
		}
		if len(result.CandidateUIDs) == 0 {
			result.CandidateUIDs = append([]uint16(nil), selected.CandidateFleetUIDs...)
			result.CandidateHotkeys = append([]string(nil), selected.CandidateFleetHotkeys...)
		} else if !slices.Equal(result.CandidateUIDs, selected.CandidateFleetUIDs) || !slices.Equal(result.CandidateHotkeys, selected.CandidateFleetHotkeys) {
			return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle validators authenticated different historical candidate identities")
		}
		candidateSet := uint16Set(selected.CandidateFleetUIDs)
		if len(candidateSet) != 202 {
			return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle authenticated candidate UID census has duplicates")
		}
		seenHotkeys := make(map[string]bool, len(selected.CandidateFleetHotkeys))
		for _, hotkey := range selected.CandidateFleetHotkeys {
			if _, ok := evidenceFixedHex(hotkey, 32); !ok || seenHotkeys[strings.ToLower(hotkey)] {
				return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle authenticated candidate hotkey census is invalid or duplicated")
			}
			seenHotkeys[strings.ToLower(hotkey)] = true
		}
		measurementHash, measurementHashErr := hex.DecodeString(strings.TrimPrefix(selected.MeasurementArtifactHash, "sha256:"))
		if measurementHashErr != nil || len(measurementHash) != 32 || selected.MeasurementArtifactHash != "sha256:"+hex.EncodeToString(measurementHash) {
			return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle measurement artifact hash is not canonical", validator.ValidatorID)
		}
		for name, value := range map[string]string{"vector": selected.VectorHash, "extrinsic": selected.ExtrinsicHash, "native snapshot": selected.NativeSnapshot.Hash, "EVM snapshot": selected.EVMSnapshot.Hash, "commit block": selected.FinalizedBlockHash, "application block": selected.ApplicationBlockHash} {
			if _, ok := evidenceFixedHex(value, 32); !ok {
				return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle %s hash is not canonical", validator.ValidatorID, name)
			}
		}
		if len(selected.EligibleHeadUIDs) != 202 || len(selected.SelectedHeadUIDs) != 200 || len(selected.RejectedHeadUIDs) != 2 || len(uint16Set(selected.EligibleHeadUIDs)) != 202 || len(uint16Set(selected.SelectedHeadUIDs)) != 200 || len(uint16Set(selected.RejectedHeadUIDs)) != 2 {
			return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle decision is not exact 202/200/2", validator.ValidatorID)
		}
		for uid := range candidateSet {
			if !uint16Set(selected.EligibleHeadUIDs)[uid] {
				return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle candidate set differs at UID %d", validator.ValidatorID, uid)
			}
		}
		weights := make(map[uint16]uint16, len(selected.AppliedWeights))
		weightPresent := make(map[uint16]bool, len(selected.AppliedWeights))
		candidateWeight := make(map[uint16]IntentWeightObservation, len(candidateSet))
		for _, weight := range selected.AppliedWeights {
			if weightPresent[weight.UID] {
				return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle weights duplicate UID %d", validator.ValidatorID, weight.UID)
			}
			weights[weight.UID] = weight.Value
			weightPresent[weight.UID] = true
			if candidateSet[weight.UID] {
				candidateWeight[weight.UID] = weight
			}
		}
		selectedSet, rejectedSet := uint16Set(selected.SelectedHeadUIDs), uint16Set(selected.RejectedHeadUIDs)
		for uid := range candidateSet {
			if selectedSet[uid] == rejectedSet[uid] {
				return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d lifecycle UID %d is not in exactly one decision partition", validator.ValidatorID, uid)
			}
		}
		for _, uid := range selected.SelectedHeadUIDs {
			if !weightPresent[uid] || weights[uid] == 0 {
				return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d selected lifecycle UID %d has zero weight", validator.ValidatorID, uid)
			}
		}
		for _, uid := range selected.RejectedHeadUIDs {
			if weights[uid] != 0 {
				return FleetLifecycleCandidateCensus{}, true, fmt.Errorf("validator %d rejected lifecycle UID %d has positive weight", validator.ValidatorID, uid)
			}
		}
		candidateUIDs := make([]uint16, 0, len(candidateSet))
		for uid := range candidateSet {
			candidateUIDs = append(candidateUIDs, uid)
		}
		sort.Slice(candidateUIDs, func(i, j int) bool { return candidateUIDs[i] < candidateUIDs[j] })
		candidateWeights := make([]IntentWeightObservation, 0, len(candidateUIDs))
		for _, uid := range candidateUIDs {
			weight := candidateWeight[uid]
			weight.UID = uid
			candidateWeights = append(candidateWeights, weight)
		}
		result.Validators = append(result.Validators, FleetLifecycleValidatorCensus{
			ValidatorID: validator.ValidatorID, SettlementEpoch: selected.SettlementEpoch, SubnetEpoch: selected.SubnetEpoch, NativeSnapshot: selected.NativeSnapshot, EVMSnapshot: selected.EVMSnapshot, MeasurementArtifactHash: selected.MeasurementArtifactHash, VectorHash: selected.VectorHash, ExtrinsicHash: selected.ExtrinsicHash,
			Commit: ChainHead{Number: selected.FinalizedBlock, Hash: selected.FinalizedBlockHash}, RevealBlock: selected.RevealBlock, RevealBlockHash: selected.RevealBlockHash,
			Application:  ChainHead{Number: selected.ApplicationBlock, Hash: selected.ApplicationBlockHash},
			EligibleUIDs: append([]uint16(nil), selected.EligibleHeadUIDs...), SelectedUIDs: append([]uint16(nil), selected.SelectedHeadUIDs...), RejectedUIDs: append([]uint16(nil), selected.RejectedHeadUIDs...),
			AppliedWeights: candidateWeights,
		})
	}
	sort.Slice(result.Validators, func(i, j int) bool { return result.Validators[i].ValidatorID < result.Validators[j].ValidatorID })
	if result.Validators[0].SettlementEpoch != result.Validators[1].SettlementEpoch || result.Validators[0].SubnetEpoch != result.Validators[1].SubnetEpoch {
		return FleetLifecycleCandidateCensus{}, true, errors.New("fleet lifecycle validators did not apply the same native decision and settlement epoch")
	}
	return result, true, nil
}

// fleetLifecycleDecisionCensuses returns every common applied native decision
// whose independently pinned EVM snapshot lies in the selected phase window.
// A caller persists all returned rows before it is allowed to mutate topology,
// so a later good decision cannot hide an invalid intermediate decision.
func fleetLifecycleDecisionCensuses(observation *ScenarioObservation, phase string, startBlock, terminalBlock uint64) ([]FleetLifecycleCandidateCensus, bool, error) {
	if observation == nil || len(observation.Validators) != 2 {
		return nil, false, nil
	}
	epochs := make(map[uint64]int)
	validatorIDs := map[int]bool{}
	for _, validator := range observation.Validators {
		if validator.ValidatorID < 1 || validator.ValidatorID > 2 || validatorIDs[validator.ValidatorID] {
			return nil, true, errors.New("fleet lifecycle decision history has duplicate or foreign validator identities")
		}
		validatorIDs[validator.ValidatorID] = true
		seen := make(map[uint64]bool)
		for _, decision := range validator.HeadDecisions {
			if decision.EVMSnapshot.Number < startBlock || decision.EVMSnapshot.Number > terminalBlock {
				continue
			}
			if seen[decision.SubnetEpoch] {
				return nil, true, fmt.Errorf("validator %d duplicated applied native epoch %d inside %s", validator.ValidatorID, decision.SubnetEpoch, phase)
			}
			seen[decision.SubnetEpoch] = true
			epochs[decision.SubnetEpoch]++
		}
	}
	common := make([]uint64, 0, len(epochs))
	for epoch, count := range epochs {
		if count != 2 {
			// A poll can land between the two validator applications. Do not
			// mutate from an older common decision while a one-sided successor
			// is unresolved; the next poll must authenticate the same epoch for
			// both validators or the bounded lifecycle deadline will fail.
			return nil, false, nil
		}
		common = append(common, epoch)
	}
	sort.Slice(common, func(i, j int) bool { return common[i] < common[j] })
	result := make([]FleetLifecycleCandidateCensus, 0, len(common))
	for _, epoch := range common {
		census, ready, err := fleetLifecycleDecisionCensusAt(observation, phase, epoch)
		if err != nil {
			return nil, true, err
		}
		if !ready {
			return nil, false, nil
		}
		result = append(result, census)
	}
	return result, len(result) != 0, nil
}

// fleetLifecycleDecisionCensus remains the narrow single-decision helper used
// by deterministic fixtures. Runtime collection uses the all-decisions form.
func fleetLifecycleDecisionCensus(observation *ScenarioObservation) (FleetLifecycleCandidateCensus, bool, error) {
	if observation == nil || len(observation.Validators) != 2 {
		return FleetLifecycleCandidateCensus{}, false, nil
	}
	common := map[uint64]int{}
	for _, validator := range observation.Validators {
		for _, decision := range validator.HeadDecisions {
			common[decision.SubnetEpoch]++
		}
	}
	var selected uint64
	for epoch, count := range common {
		if count == 2 && epoch > selected {
			selected = epoch
		}
	}
	if selected == 0 {
		return FleetLifecycleCandidateCensus{}, false, nil
	}
	return fleetLifecycleDecisionCensusAt(observation, "release-1.0", selected)
}

// Resolve each number-only intent reveal against the canonical finalized
// chain before the census becomes durable evidence. A supplied nonempty hash
// is treated as an assertion and must match rather than being overwritten.
func resolveFleetLifecycleRevealHeads(census *FleetLifecycleCandidateCensus, canonicalHash func(uint64) (string, error)) error {
	if census == nil || canonicalHash == nil || len(census.Validators) != 2 {
		return errors.New("fleet lifecycle reveal-head resolver is unavailable")
	}
	resolved := make(map[uint64]string, len(census.Validators))
	for index := range census.Validators {
		validator := &census.Validators[index]
		hash, found := resolved[validator.RevealBlock]
		if !found {
			var err error
			hash, err = canonicalHash(validator.RevealBlock)
			if err != nil {
				return fmt.Errorf("fleet lifecycle validator %d reveal block %d: %w", validator.ValidatorID, validator.RevealBlock, err)
			}
			hash = strings.ToLower(hash)
			if _, ok := evidenceFixedHex(hash, 32); !ok {
				return fmt.Errorf("fleet lifecycle validator %d reveal block has no canonical hash", validator.ValidatorID)
			}
			resolved[validator.RevealBlock] = hash
		}
		if validator.RevealBlockHash != "" && !strings.EqualFold(validator.RevealBlockHash, hash) {
			return fmt.Errorf("fleet lifecycle validator %d reveal block hash differs from canonical chain", validator.ValidatorID)
		}
		validator.RevealBlockHash = hash
	}
	return nil
}

func fleetLifecycleCensusRejects(census FleetLifecycleCandidateCensus, uids ...uint16) bool {
	if len(census.Validators) != 2 {
		return false
	}
	for _, validator := range census.Validators {
		for _, uid := range uids {
			if !slices.Contains(validator.RejectedUIDs, uid) {
				return false
			}
		}
	}
	return true
}

func fleetLifecycleCensusSelects(census FleetLifecycleCandidateCensus, uids ...uint16) bool {
	if len(census.Validators) != 2 || len(uids) == 0 {
		return false
	}
	for _, validator := range census.Validators {
		weights := make(map[uint16]uint16, len(validator.AppliedWeights))
		for _, weight := range validator.AppliedWeights {
			weights[weight.UID] = weight.Value
		}
		for _, uid := range uids {
			if !slices.Contains(validator.SelectedUIDs, uid) || weights[uid] == 0 {
				return false
			}
		}
	}
	return true
}

func (self *liveFleetLifecycle) appendCensus(census FleetLifecycleCandidateCensus) error {
	for _, prior := range self.evidence.CandidateCensuses {
		if len(prior.Validators) == len(census.Validators) && len(prior.Validators) == 2 && prior.Validators[0].Application == census.Validators[0].Application && prior.Validators[1].Application == census.Validators[1].Application {
			// Re-observing one immutable application at a later pair of
			// finalized heads is expected. Compare the decision and candidate
			// identity, while retaining the first observation and milestone.
			priorHash, priorErr := canonicalHashHex(struct {
				Phase            string                          `json:"phase"`
				CandidateUIDs    []uint16                        `json:"candidate_uids"`
				CandidateHotkeys []string                        `json:"candidate_hotkeys"`
				Validators       []FleetLifecycleValidatorCensus `json:"validators"`
			}{prior.Phase, prior.CandidateUIDs, prior.CandidateHotkeys, prior.Validators})
			currentHash, currentErr := canonicalHashHex(struct {
				Phase            string                          `json:"phase"`
				CandidateUIDs    []uint16                        `json:"candidate_uids"`
				CandidateHotkeys []string                        `json:"candidate_hotkeys"`
				Validators       []FleetLifecycleValidatorCensus `json:"validators"`
			}{census.Phase, census.CandidateUIDs, census.CandidateHotkeys, census.Validators})
			if priorErr != nil || currentErr != nil || priorHash != currentHash {
				return errors.New("fleet lifecycle census application receipts were reused with different evidence")
			}
			return nil
		}
	}
	clone := census
	clone.CandidateUIDs = append([]uint16(nil), census.CandidateUIDs...)
	clone.CandidateHotkeys = append([]string(nil), census.CandidateHotkeys...)
	clone.Validators = make([]FleetLifecycleValidatorCensus, len(census.Validators))
	for index := range census.Validators {
		clone.Validators[index] = census.Validators[index]
		clone.Validators[index].EligibleUIDs = append([]uint16(nil), census.Validators[index].EligibleUIDs...)
		clone.Validators[index].SelectedUIDs = append([]uint16(nil), census.Validators[index].SelectedUIDs...)
		clone.Validators[index].RejectedUIDs = append([]uint16(nil), census.Validators[index].RejectedUIDs...)
		clone.Validators[index].AppliedWeights = append([]IntentWeightObservation(nil), census.Validators[index].AppliedWeights...)
	}
	self.evidence.CandidateCensuses = append(self.evidence.CandidateCensuses, clone)
	return nil
}

func (self *liveFleetLifecycle) markCensusMilestone(census FleetLifecycleCandidateCensus, milestone string) error {
	for index := range self.evidence.CandidateCensuses {
		prior := &self.evidence.CandidateCensuses[index]
		if len(prior.Validators) == 2 && prior.Validators[0].Application == census.Validators[0].Application && prior.Validators[1].Application == census.Validators[1].Application {
			if prior.Milestone != "" && prior.Milestone != milestone {
				return fmt.Errorf("fleet lifecycle application was already assigned milestone %s", prior.Milestone)
			}
			for otherIndex := range self.evidence.CandidateCensuses {
				if otherIndex != index && self.evidence.CandidateCensuses[otherIndex].Milestone == milestone {
					return fmt.Errorf("fleet lifecycle milestone %s is already assigned", milestone)
				}
			}
			prior.Milestone = milestone
			return self.write()
		}
	}
	return fmt.Errorf("fleet lifecycle milestone %s has no persisted application", milestone)
}

func fleetLifecycleLatestCensus(censuses []FleetLifecycleCandidateCensus, phase string) *FleetLifecycleCandidateCensus {
	var result *FleetLifecycleCandidateCensus
	for index := range censuses {
		candidate := &censuses[index]
		if candidate.Phase != phase || len(candidate.Validators) != 2 {
			continue
		}
		if result == nil || fleetLifecycleApplicationHead(candidate) > fleetLifecycleApplicationHead(result) {
			result = candidate
		}
	}
	return result
}

func fleetLifecycleEpoch(first, offset uint64) (uint64, error) {
	value, ok := checkedAdd(first, offset)
	if !ok || first == 0 {
		return 0, errors.New("fleet lifecycle epoch geometry overflows or has no first epoch")
	}
	return value, nil
}

func fleetLifecycleCensusForEpoch(censuses []FleetLifecycleCandidateCensus, epoch uint64, rejectedUID uint16) bool {
	for _, census := range censuses {
		if len(census.Validators) != 2 || census.Validators[0].SubnetEpoch != epoch || census.Validators[1].SubnetEpoch != epoch {
			continue
		}
		if fleetLifecycleCensusRejects(census, rejectedUID) {
			return true
		}
	}
	return false
}

func fleetLifecycleSelectedCensusForEpoch(censuses []FleetLifecycleCandidateCensus, epoch uint64, uids ...uint16) bool {
	for _, census := range censuses {
		if len(census.Validators) == 2 && census.Validators[0].SubnetEpoch == epoch && census.Validators[1].SubnetEpoch == epoch && fleetLifecycleCensusSelects(census, uids...) {
			return true
		}
	}
	return false
}

func (self *liveFleetLifecycle) requireCandidateRole(observation *ScenarioObservation, fleet int, role string, expectedUID uint16) error {
	if fleet < 1 || fleet > len(observation.CandidateFleetUIDs) || fleet > len(observation.CandidateFleetHotkeys) {
		return errors.New("fleet lifecycle candidate role index is unavailable")
	}
	hotkey, err := roleBytes32(self.executor.roles, role)
	if err != nil {
		return err
	}
	if observation.CandidateFleetUIDs[fleet-1] != expectedUID || !strings.EqualFold(observation.CandidateFleetHotkeys[fleet-1], fleetLifecycleHex(hotkey)) {
		return fmt.Errorf("fleet lifecycle fleet %d candidate UID/hotkey drifted from %d/%s", fleet, expectedUID, role)
	}
	return nil
}

func (self *liveFleetLifecycle) enoughMutationWindow(observation *ScenarioObservation) bool {
	contracts := observation.Status.Contracts
	return contracts.CurrentEpochEnd > contracts.FinalizedHead.Number && contracts.CurrentEpochEnd-contracts.FinalizedHead.Number > fleetLifecycleMutationSafetyBlocks
}

func (self *liveFleetLifecycle) execute(ctx context.Context, ids ...string) error {
	for _, id := range ids {
		action, err := self.executor.planAction(id)
		if err != nil {
			return err
		}
		if err := self.executor.Execute(ctx, action); err != nil {
			return err
		}
	}
	return nil
}

func fleetLifecycleActionIDs(prefix string, members int) []string {
	var ids []string
	switch prefix {
	case "prepare":
		for _, provider := range []string{"target", "companion"} {
			base := "lifecycle.prepare." + provider
			ids = append(ids, base+".fund-hotkey", base+".commitment", base+".mirror")
			for member := 1; member <= members; member++ {
				ids = append(ids, fmt.Sprintf("%s.bind.%d", base, member))
			}
			ids = append(ids, base+".installed")
		}
	case "fallback":
		ids = []string{"lifecycle.fallback.fund", "lifecycle.fallback.register", "lifecycle.fallback.fund-hotkey", "lifecycle.fallback.commitment", "lifecycle.fallback.mirror"}
		for member := 1; member <= members; member++ {
			ids = append(ids, fmt.Sprintf("lifecycle.fallback.bind.%d", member))
		}
		ids = append(ids, "lifecycle.fallback.installed")
	case "provider":
		for member := 1; member <= members; member++ {
			ids = append(ids, fmt.Sprintf("lifecycle.provider.cleanup.%d", member))
		}
		ids = append(ids, "lifecycle.provider.fund", "lifecycle.provider.register", "lifecycle.provider.fund-hotkey", "lifecycle.provider.commitment", "lifecycle.provider.mirror")
		for member := 1; member <= members; member++ {
			ids = append(ids, fmt.Sprintf("lifecycle.provider.bind.%d", member))
		}
		ids = append(ids, "lifecycle.provider.installed")
	case "terminal":
		for member := 1; member <= members; member++ {
			ids = append(ids, fmt.Sprintf("lifecycle.terminal.cleanup-companion.%d", member), fmt.Sprintf("lifecycle.terminal.cleanup-fallback.%d", member))
		}
		ids = append(ids, "lifecycle.terminal.fund", "lifecycle.terminal.register", "lifecycle.terminal.fund-hotkey", "lifecycle.terminal.commitment", "lifecycle.terminal.mirror")
		for member := 1; member <= members; member++ {
			ids = append(ids, fmt.Sprintf("lifecycle.terminal.bind.%d", member))
		}
		ids = append(ids, "lifecycle.terminal.installed")
	}
	return ids
}

func fleetLifecycleClientIDs(roles *RoleSecrets, miners []int) ([]string, error) {
	result := make([]string, 0, len(miners))
	for _, miner := range miners {
		role, ok := roles.Clients[fmt.Sprintf("miner-%d", miner)]
		raw, err := hex.DecodeString(role.ClientIDHex)
		if !ok || err != nil || len(raw) != 16 {
			return nil, fmt.Errorf("fleet lifecycle miner-%d client id is unavailable", miner)
		}
		result = append(result, "0x"+hex.EncodeToString(raw))
	}
	sort.Strings(result)
	return result, nil
}

func fleetLifecycleObservationForOperator(observation *ScenarioObservation, noID int) *OperatorObservation {
	for index := range observation.Operators {
		if observation.Operators[index].NoID == noID {
			return &observation.Operators[index]
		}
	}
	return nil
}

func (self *liveFleetLifecycle) payoutEvidence(observation *ScenarioObservation, epoch uint64, miners []int, excluded bool, disposition string) (FleetLifecyclePayoutEvidence, bool, error) {
	if len(miners) == 0 {
		return FleetLifecyclePayoutEvidence{}, false, errors.New("fleet lifecycle payout miner set is empty")
	}
	noID := operatorForMiner(self.cfg, miners[0])
	for _, miner := range miners[1:] {
		if operatorForMiner(self.cfg, miner) != noID {
			return FleetLifecyclePayoutEvidence{}, false, errors.New("fleet lifecycle payout clients cross operators")
		}
	}
	operator := fleetLifecycleObservationForOperator(observation, noID)
	if operator == nil {
		return FleetLifecyclePayoutEvidence{}, false, nil
	}
	var artifact *OperatorLifecyclePayoutArtifactObservation
	for index := range operator.LifecyclePayoutArtifacts {
		candidate := &operator.LifecyclePayoutArtifacts[index]
		if candidate.Epoch == epoch {
			if artifact != nil {
				return FleetLifecyclePayoutEvidence{}, false, fmt.Errorf("fleet lifecycle operator %d duplicated payout artifact epoch %d", noID, epoch)
			}
			artifact = candidate
		}
	}
	if artifact == nil {
		if operator.LatestArtifactEpoch > epoch {
			return FleetLifecyclePayoutEvidence{}, false, fmt.Errorf("fleet lifecycle operator %d history omitted payout artifact epoch %d; latest is %d", noID, epoch, operator.LatestArtifactEpoch)
		}
		return FleetLifecyclePayoutEvidence{}, false, nil
	}
	if artifact.NoID != uint64(noID) {
		return FleetLifecyclePayoutEvidence{}, false, errors.New("fleet lifecycle payout history row has the wrong operator")
	}
	clientIDs, err := fleetLifecycleClientIDs(self.executor.roles, miners)
	if err != nil {
		return FleetLifecyclePayoutEvidence{}, false, err
	}
	tiers := make(map[string]OperatorPayoutClientTierObservation, len(artifact.Clients))
	for _, client := range artifact.Clients {
		key := strings.ToLower(client.ClientID)
		if key == "" || tiers[key].ClientID != "" || client.Leaf == client.HeadExcluded {
			return FleetLifecyclePayoutEvidence{}, false, errors.New("fleet lifecycle payout history has duplicate or ambiguous client membership")
		}
		tiers[key] = client
	}
	for _, clientID := range clientIDs {
		tier, found := tiers[strings.ToLower(clientID)]
		if !found {
			return FleetLifecyclePayoutEvidence{}, false, nil
		}
		if excluded && (!tier.HeadExcluded || tier.Leaf) || !excluded && (!tier.Leaf || tier.HeadExcluded) {
			return FleetLifecyclePayoutEvidence{}, false, fmt.Errorf("fleet lifecycle payout client %s is not exclusively in the expected tier", clientID)
		}
	}
	if _, ok := evidenceFixedHex(artifact.PayoutRoot, 32); !ok || !strings.HasPrefix(artifact.ContentHash, "sha256:") {
		return FleetLifecyclePayoutEvidence{}, false, errors.New("fleet lifecycle payout artifact has no canonical root or content hash")
	}
	return FleetLifecyclePayoutEvidence{Epoch: artifact.Epoch, NoID: noID, ContentHash: artifact.ContentHash, PayoutRoot: artifact.PayoutRoot, ClientIDs: clientIDs, Disposition: disposition}, true, nil
}

func (self *liveFleetLifecycle) appendPayout(payout FleetLifecyclePayoutEvidence) error {
	for _, prior := range self.evidence.Payouts {
		if prior.Disposition == payout.Disposition {
			priorHash, priorErr := canonicalHashHex(prior)
			currentHash, currentErr := canonicalHashHex(payout)
			if priorErr != nil || currentErr != nil || priorHash != currentHash {
				return fmt.Errorf("fleet lifecycle payout disposition %s was reused with different evidence", payout.Disposition)
			}
			return nil
		}
	}
	payout.ClientIDs = append([]string(nil), payout.ClientIDs...)
	self.evidence.Payouts = append(self.evidence.Payouts, payout)
	return nil
}

func fleetLifecycleMembers(cfg *ResolvedConfig, fleet int) []int {
	miners := make([]int, 0, cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miners = append(miners, fleetMemberMinerIndex(cfg, fleet, member))
	}
	return miners
}

func (self *liveFleetLifecycle) fallbackMembers() ([]int, error) {
	miners := make([]int, 0, self.cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= self.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		miner, err := fleetLifecycleFallbackMinerIndex(self.cfg, member)
		if err != nil {
			return nil, err
		}
		miners = append(miners, miner)
	}
	return miners, nil
}

func (self *liveFleetLifecycle) installedEffectiveEpoch(variantName string) (uint64, error) {
	var result uint64
	for member := 1; member <= self.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		evidence, err := loadFleetLifecycleBindingEvidence(self.stateDir, variantName, member)
		if err != nil {
			return 0, err
		}
		if result == 0 {
			result = evidence.ValidFromEpoch
		} else if result != evidence.ValidFromEpoch {
			return 0, errors.New("fleet lifecycle bindings do not share an effective epoch")
		}
	}
	return result, nil
}

func (self *liveFleetLifecycle) readRegistration(ctx context.Context, variantName string) (*FleetLifecycleRegistrationEvidence, error) {
	_, name := fleetLifecycleRegistrationNames(variantName)
	var evidence FleetLifecycleRegistrationEvidence
	if err := readJSONFile(filepath.Join(self.stateDir, "public", name), &evidence); err != nil {
		return nil, err
	}
	actionID := map[string]string{fleetLifecycleVariantFallback: "lifecycle.fallback.register", fleetLifecycleVariantProvider: "lifecycle.provider.register", fleetLifecycleVariantTerminal: "lifecycle.terminal.register"}[variantName]
	action, err := self.executor.planAction(actionID)
	if err != nil {
		return nil, err
	}
	if err := self.executor.validateFleetLifecycleRegistrationAction(ctx, action, variantName, evidence); err != nil {
		return nil, err
	}
	return &evidence, nil
}

func (self *liveFleetLifecycle) readCleanup(ctx context.Context, variantName string) ([]FleetLifecycleCleanupEvidence, error) {
	result := make([]FleetLifecycleCleanupEvidence, 0, self.cfg.Config.Topology.ClientsPerHeadFleet)
	for member := 1; member <= self.cfg.Config.Topology.ClientsPerHeadFleet; member++ {
		var evidence FleetLifecycleCleanupEvidence
		if err := readJSONFile(filepath.Join(self.stateDir, "public", fleetLifecycleCleanupEvidenceName(variantName, member)), &evidence); err != nil {
			return nil, err
		}
		var actionID string
		switch variantName {
		case fleetLifecycleVariantTargetTakeover:
			actionID = fmt.Sprintf("lifecycle.provider.cleanup.%d", member)
		case fleetLifecycleVariantCompanionTakeover:
			actionID = fmt.Sprintf("lifecycle.terminal.cleanup-companion.%d", member)
		case fleetLifecycleVariantFallback:
			actionID = fmt.Sprintf("lifecycle.terminal.cleanup-fallback.%d", member)
		default:
			return nil, fmt.Errorf("fleet lifecycle cleanup variant %s is unsupported", variantName)
		}
		action, err := self.executor.planAction(actionID)
		if err != nil {
			return nil, err
		}
		if err := self.executor.validateFleetLifecycleCleanupAction(ctx, action, variantName, member, evidence); err != nil {
			return nil, err
		}
		result = append(result, evidence)
	}
	return result, nil
}

func (self *liveFleetLifecycle) finishProviderInstall(ctx context.Context, members int) error {
	if err := self.execute(ctx, fleetLifecycleActionIDs("provider", members)...); err != nil {
		return err
	}
	registration, err := self.readRegistration(ctx, fleetLifecycleVariantProvider)
	if err != nil {
		return err
	}
	effective, err := self.installedEffectiveEpoch(fleetLifecycleVariantProvider)
	if err != nil {
		return err
	}
	if effective <= self.evidence.FallbackEffectiveEpoch {
		return fmt.Errorf("fleet lifecycle provider effective settlement epoch=%d does not follow fallback epoch=%d", effective, self.evidence.FallbackEffectiveEpoch)
	}
	cleanup, err := self.readCleanup(ctx, fleetLifecycleVariantTargetTakeover)
	if err != nil {
		return err
	}
	self.evidence.ProviderRegistration = registration
	self.evidence.ProviderEffectiveEpoch = effective
	self.evidence.TargetCleanup = cleanup
	self.evidence.Stage = fleetLifecycleStageProviderInstalled
	self.resumeValidated = false
	return self.write()
}

func (self *liveFleetLifecycle) finishTerminalInstall(ctx context.Context, members int) error {
	if err := self.execute(ctx, fleetLifecycleActionIDs("terminal", members)...); err != nil {
		return err
	}
	registration, err := self.readRegistration(ctx, fleetLifecycleVariantTerminal)
	if err != nil {
		return err
	}
	effective, err := self.installedEffectiveEpoch(fleetLifecycleVariantTerminal)
	if err != nil {
		return err
	}
	if effective <= self.evidence.ProviderEffectiveEpoch {
		return fmt.Errorf("fleet lifecycle terminal effective settlement epoch=%d does not follow provider epoch=%d", effective, self.evidence.ProviderEffectiveEpoch)
	}
	companionCleanup, err := self.readCleanup(ctx, fleetLifecycleVariantCompanionTakeover)
	if err != nil {
		return err
	}
	fallbackCleanup, err := self.readCleanup(ctx, fleetLifecycleVariantFallback)
	if err != nil {
		return err
	}
	self.evidence.TerminalRegistration = registration
	self.evidence.TerminalEffectiveEpoch = effective
	self.evidence.CompanionCleanup = companionCleanup
	self.evidence.FallbackCleanup = fallbackCleanup
	self.evidence.Stage = fleetLifecycleStageTerminalInstalled
	self.resumeValidated = false
	return self.write()
}

func fleetLifecycleCensusFollowsRegistration(census *FleetLifecycleCandidateCensus, registration *FleetLifecycleRegistrationEvidence) bool {
	if census == nil || registration == nil || len(census.Validators) != 2 {
		return false
	}
	for _, validator := range census.Validators {
		if validator.NativeSnapshot.Number <= registration.BlockNumber || validator.Application.Number <= registration.BlockNumber || validator.SettlementEpoch == 0 {
			return false
		}
	}
	return true
}

func (self *liveFleetLifecycle) releaseMutationScheduleFits(census *FleetLifecycleCandidateCensus, laterDecisions, remainingMutations uint64) error {
	if census == nil || self.evidence == nil || self.evidence.ReleaseHandoffSchedule == nil {
		return errors.New("fleet lifecycle release mutation has no applied decision")
	}
	schedule := self.evidence.ReleaseHandoffSchedule
	decisionPeriods, periodOK := checkedAdd(schedule.RevealPeriodEpochs, 1)
	decisionBlocks, decisionOK := checkedMul(decisionPeriods, uint64(schedule.Tempo))
	decisionBlocks, ok := checkedMul(laterDecisions, decisionBlocks)
	mutationBlocks, mutationOK := checkedMul(remainingMutations, fleetLifecycleMutationSafetyBlocks)
	required, requiredOK := checkedAdd(fleetLifecycleApplicationHead(census), decisionBlocks)
	required, endOK := checkedAdd(required, mutationBlocks)
	if !periodOK || !decisionOK || !ok || !mutationOK || !requiredOK || !endOK || required > schedule.ApplicationDeadlineBlock {
		return fmt.Errorf("fleet lifecycle release window cannot fit %d later native decisions and %d mutation reserves", laterDecisions, remainingMutations)
	}
	return nil
}

func fleetLifecycleDecisionKey(settlementEpoch, subnetEpoch uint64) string {
	return fmt.Sprintf("%d/%d", settlementEpoch, subnetEpoch)
}

func validateFleetLifecycleDecisionCoverage(observation *ScenarioObservation, evidence *FleetLifecycleEvidence, phase string, terminal uint64) error {
	if observation == nil || evidence == nil || len(observation.Validators) != 2 {
		return errors.New("fleet lifecycle decision coverage inputs are incomplete")
	}
	var start uint64
	switch phase {
	case "release-1.0":
		start = evidence.AcceptanceStartBlock
	case "production-soak":
		start = evidence.ProductionAcceptanceStartBlock
	default:
		return errors.New("fleet lifecycle decision coverage phase is invalid")
	}
	want := make([]map[string]bool, 2)
	for _, validator := range observation.Validators {
		if validator.ValidatorID < 1 || validator.ValidatorID > 2 || want[validator.ValidatorID-1] != nil {
			return errors.New("fleet lifecycle decision coverage has invalid validator identities")
		}
		want[validator.ValidatorID-1] = map[string]bool{}
		for _, decision := range validator.HeadDecisions {
			if decision.EVMSnapshot.Number < start || decision.EVMSnapshot.Number > terminal {
				continue
			}
			key := fleetLifecycleDecisionKey(decision.SettlementEpoch, decision.SubnetEpoch)
			if want[validator.ValidatorID-1][key] {
				return fmt.Errorf("validator %d duplicated lifecycle decision %s", validator.ValidatorID, key)
			}
			want[validator.ValidatorID-1][key] = true
		}
	}
	if len(want[0]) != len(want[1]) {
		return errors.New("fleet lifecycle validators have unequal applied-decision coverage")
	}
	captured := map[string]bool{}
	for _, census := range evidence.CandidateCensuses {
		if census.Phase != phase || len(census.Validators) != 2 {
			continue
		}
		key := fleetLifecycleDecisionKey(census.Validators[0].SettlementEpoch, census.Validators[0].SubnetEpoch)
		captured[key] = true
	}
	for key := range want[0] {
		if !want[1][key] || !captured[key] {
			return fmt.Errorf("fleet lifecycle applied decision %s is not captured for both validators", key)
		}
	}
	return nil
}

func (self *liveFleetLifecycle) Advance(ctx context.Context, observation *ScenarioObservation, faults []ScenarioFaultRecord) error {
	if self.evidence == nil || observation == nil || observation.Status == nil || observation.Status.Contracts == nil {
		return errors.New("fleet lifecycle advance inputs are incomplete")
	}
	if self.phase != "release-1.0" && self.phase != "production-soak" {
		return errors.New("fleet lifecycle phase is not initialized")
	}
	if err := self.validateResumeLineage(ctx); err != nil {
		return fmt.Errorf("fleet lifecycle immutable resume proof: %w", err)
	}
	var startBlock, terminalBlock uint64
	if self.phase == "release-1.0" {
		if err := self.bindNativeSchedule("release-1.0"); err != nil {
			return err
		}
		startBlock, terminalBlock = self.evidence.AcceptanceStartBlock, self.evidence.ReleaseEVMEvidenceDeadlineBlock
		if terminalBlock < self.evidence.AcceptanceTerminalBlock {
			terminalBlock = self.evidence.AcceptanceTerminalBlock
		}
	} else {
		if err := self.bindProductionNativeSchedule(); err != nil {
			return err
		}
		startBlock, terminalBlock = self.evidence.ProductionAcceptanceStartBlock, self.evidence.ProductionEVMEvidenceDeadlineBlock
		if terminalBlock < self.evidence.ProductionAcceptanceTerminalBlock {
			terminalBlock = self.evidence.ProductionAcceptanceTerminalBlock
		}
	}
	censuses, ready, err := fleetLifecycleDecisionCensuses(observation, self.phase, startBlock, terminalBlock)
	if err != nil {
		return err
	}
	if ready {
		if self.executor == nil || self.executor.substrate == nil || self.executor.substrate.chain == nil || self.executor.substrate.chain.API == nil {
			return errors.New("fleet lifecycle canonical reveal-head reader is unavailable")
		}
		for index := range censuses {
			census := &censuses[index]
			if err := resolveFleetLifecycleRevealHeads(census, func(block uint64) (string, error) {
				hash, readErr := self.executor.substrate.chain.API.RPC.Chain.GetBlockHash(block)
				return hash.Hex(), readErr
			}); err != nil {
				return err
			}
			boundary := self.evidence.AcceptanceTerminalBlock
			if self.phase == "production-soak" {
				boundary = self.evidence.ProductionAcceptanceTerminalBlock
			}
			firstPost := census.Validators[0].EVMSnapshot.Number > boundary
			secondPost := census.Validators[1].EVMSnapshot.Number > boundary
			if firstPost != secondPost {
				return errors.New("fleet lifecycle validators straddle the acceptance/tail boundary")
			}
			census.PostAcceptance = firstPost
			if err := self.appendCensus(*census); err != nil {
				return err
			}
		}
		// The complete decision set is durable before any native or EVM
		// mutation below can change the candidate identities.
		if err := self.write(); err != nil {
			return err
		}
	}
	if self.evidence.FirstAcceptedEpoch == 0 {
		return errors.New("fleet lifecycle acceptance window is not bound")
	}
	members := self.cfg.Config.Topology.ClientsPerHeadFleet
	latest := fleetLifecycleLatestCensus(self.evidence.CandidateCensuses, self.phase)
	if self.phase == "production-soak" {
		if self.evidence.Stage == fleetLifecycleStageComplete {
			return self.write()
		}
		if self.evidence.Stage != fleetLifecycleStageReleaseHandoff {
			return fmt.Errorf("production resumed fleet lifecycle stage %q instead of the release handoff", self.evidence.Stage)
		}
		if latest != nil && fleetLifecycleCensusFollowsRegistration(latest, self.evidence.TerminalRegistration) {
			if !self.censusHasExactFleetRoles(latest, map[int]struct {
				uid  uint16
				role string
			}{fleetLifecycleTargetFleet: {fleetLifecycleCompanionExpectedUID, churnHotkeyLabel(fleetLifecycleTargetChurn)}, fleetLifecycleCompanionFleet: {fleetLifecycleTerminalVictimUID, churnHotkeyLabel(fleetLifecycleCompanionChurn)}}) {
				return errors.New("fleet lifecycle terminal decision reused a UID with the wrong provider identity")
			}
			if !fleetLifecycleCensusSelects(*latest, fleetLifecycleCompanionExpectedUID, fleetLifecycleTerminalVictimUID) {
				return errors.New("fleet lifecycle terminal providers received zero weight in a post-restoration native decision")
			}
			if err := self.markCensusMilestone(*latest, fleetLifecycleMilestoneTerminalActive); err != nil {
				return err
			}
			if err := validateFleetLifecycleDecisionCoverage(observation, self.evidence, self.phase, terminalBlock); err != nil {
				return err
			}
			self.evidence.Stage = fleetLifecycleStageComplete
			self.resumeValidated = false
			if err := self.write(); err != nil {
				return err
			}
			return self.validateResumeLineage(ctx)
		}
		if observation.NativeRewards != nil && observation.NativeRewards.FinalizedHead.Number > self.evidence.ProductionNativeSchedule.ApplicationDeadlineBlock {
			return errors.New("fleet lifecycle terminal-active decision missed its exact CRv4 evidence-tail deadline")
		}
		return self.write()
	}
	if self.evidence.Stage != fleetLifecycleStageReleaseHandoff && observation.NativeRewards != nil && observation.NativeRewards.FinalizedHead.Number > self.evidence.ReleaseHandoffSchedule.ApplicationDeadlineBlock {
		return errors.New("fleet lifecycle release handoff missed its exact CRv4 evidence-tail deadline")
	}
	switch self.evidence.Stage {
	case fleetLifecycleStageAwaitingDemotion:
		if !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-target-prune", "active") || !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-companion-prune", "active") || latest == nil || !self.enoughMutationWindow(observation) {
			return self.write()
		}
		if err := self.requireCandidateRole(observation, fleetLifecycleTargetFleet, churnHotkeyLabel(fleetLifecycleTargetChurn), fleetLifecycleTargetExpectedUID); err != nil {
			return err
		}
		if err := self.requireCandidateRole(observation, fleetLifecycleCompanionFleet, churnHotkeyLabel(fleetLifecycleCompanionChurn), fleetLifecycleCompanionExpectedUID); err != nil {
			return err
		}
		if !fleetLifecycleCensusRejects(*latest, fleetLifecycleTargetExpectedUID, fleetLifecycleCompanionExpectedUID) {
			return errors.New("fleet lifecycle takeover decision did not zero both filtered provider UIDs")
		}
		if err := self.releaseMutationScheduleFits(latest, 2, 3); err != nil {
			return err
		}
		if err := self.markCensusMilestone(*latest, fleetLifecycleMilestoneTakeoverRejected); err != nil {
			return err
		}
		victimHotkey, err := roleBytes32(self.executor.roles, fleetProviderHotkeyLabel(fleetLifecycleTargetFleet))
		if err != nil {
			return err
		}
		victimColdkey, err := roleBytes32(self.executor.roles, fleetProviderColdkeyLabel(fleetLifecycleTargetFleet))
		if err != nil {
			return err
		}
		prune, err := self.executor.substrate.FleetLifecyclePruneSnapshot()
		if err != nil {
			return err
		}
		row, found := fleetLifecyclePruneInputByHotkey(prune, victimHotkey)
		if !found || row.EmissionRao != 0 {
			return self.write()
		}
		if err := validateFleetLifecyclePruneSnapshot(prune, victimHotkey, victimColdkey); err != nil {
			return err
		}
		if err := self.execute(ctx, fleetLifecycleActionIDs("fallback", members)...); err != nil {
			return err
		}
		self.evidence.FallbackRegistration, err = self.readRegistration(ctx, fleetLifecycleVariantFallback)
		if err != nil {
			return err
		}
		self.evidence.FallbackEffectiveEpoch, err = self.installedEffectiveEpoch(fleetLifecycleVariantFallback)
		if err != nil {
			return err
		}
		if self.evidence.FallbackEffectiveEpoch <= latest.Validators[0].SettlementEpoch {
			return fmt.Errorf("fleet lifecycle fallback effective settlement epoch=%d does not follow decision settlement epoch=%d", self.evidence.FallbackEffectiveEpoch, latest.Validators[0].SettlementEpoch)
		}
		self.evidence.Stage = fleetLifecycleStageFallbackInstalled
		self.resumeValidated = false
		return self.write()
	case fleetLifecycleStageFallbackInstalled:
		if !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-target-prune", "active") || !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-companion-prune", "active") || latest == nil || !fleetLifecycleCensusFollowsRegistration(latest, self.evidence.FallbackRegistration) || !self.enoughMutationWindow(observation) {
			return self.write()
		}
		if err := self.requireCandidateRole(observation, fleetLifecycleTargetFleet, churnHotkeyLabel(fleetLifecycleFallbackChurn), fleetLifecycleTargetExpectedUID); err != nil {
			return err
		}
		if err := self.requireCandidateRole(observation, fleetLifecycleCompanionFleet, churnHotkeyLabel(fleetLifecycleCompanionChurn), fleetLifecycleCompanionExpectedUID); err != nil {
			return err
		}
		if !fleetLifecycleCensusRejects(*latest, fleetLifecycleTargetExpectedUID, fleetLifecycleCompanionExpectedUID) {
			return errors.New("fleet lifecycle fallback-active decision did not preserve exact zero weights")
		}
		if err := self.releaseMutationScheduleFits(latest, 1, 2); err != nil {
			return err
		}
		oldPaid, oldFound, err := self.payoutEvidence(observation, self.evidence.FallbackEffectiveEpoch, fleetLifecycleMembers(self.cfg, fleetLifecycleTargetFleet), false, "pruned-provider-returned-to-operator-pool")
		if err != nil || !oldFound {
			return stateMismatchError(err, "fleet lifecycle has no exact full-settlement target pool payout after fallback activation")
		}
		fallbackMembers, err := self.fallbackMembers()
		if err != nil {
			return err
		}
		fallbackExcluded, fallbackFound, err := self.payoutEvidence(observation, self.evidence.FallbackEffectiveEpoch, fallbackMembers, true, "fallback-provider-head-excluded")
		if err != nil || !fallbackFound {
			return stateMismatchError(err, "fleet lifecycle has no exact full-settlement fallback head exclusion")
		}
		if err := self.appendPayout(oldPaid); err != nil {
			return err
		}
		if err := self.appendPayout(fallbackExcluded); err != nil {
			return err
		}
		if err := self.markCensusMilestone(*latest, fleetLifecycleMilestoneFallbackActive); err != nil {
			return err
		}
		victimHotkey, err := roleBytes32(self.executor.roles, fleetProviderHotkeyLabel(fleetLifecycleCompanionFleet))
		if err != nil {
			return err
		}
		victimColdkey, err := roleBytes32(self.executor.roles, fleetProviderColdkeyLabel(fleetLifecycleCompanionFleet))
		if err != nil {
			return err
		}
		prune, err := self.executor.substrate.FleetLifecyclePruneSnapshot()
		if err != nil {
			return err
		}
		row, found := fleetLifecyclePruneInputByHotkey(prune, victimHotkey)
		if !found || row.EmissionRao != 0 {
			return self.write()
		}
		if err := validateFleetLifecyclePruneSnapshot(prune, victimHotkey, victimColdkey); err != nil {
			return err
		}
		self.evidence.Stage = fleetLifecycleStageFallbackPaid
		self.resumeValidated = false
		if err := self.write(); err != nil {
			return err
		}
		return self.finishProviderInstall(ctx, members)
	case fleetLifecycleStageFallbackPaid:
		// A crash after the durable stage marker resumes the exact journal-bound
		// cleanup/re-registration action sequence without repeating mutations.
		return self.finishProviderInstall(ctx, members)
	case fleetLifecycleStageProviderInstalled:
		if self.evidence.ProviderRegistration == nil {
			return errors.New("fleet lifecycle provider registration evidence is absent")
		}
		if !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-target-prune", "active") || !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-companion-prune", "active") {
			return self.write()
		}
		if self.evidence.PostRegistrationRewardBaseline.Number == 0 {
			if observation.NativeRewards == nil || observation.NativeRewards.FinalizedHead.Number <= self.evidence.ProviderRegistration.BlockNumber {
				return self.write()
			}
			if _, ok := evidenceFixedHex(observation.NativeRewards.FinalizedHead.Hash, 32); !ok {
				return errors.New("fleet lifecycle post-registration reward baseline has no exact native block hash")
			}
			self.evidence.PostRegistrationRewardBaseline = observation.NativeRewards.FinalizedHead
			if err := self.write(); err != nil {
				return err
			}
		}
		if latest == nil || !fleetLifecycleCensusFollowsRegistration(latest, self.evidence.ProviderRegistration) || !self.enoughMutationWindow(observation) {
			return self.write()
		}
		if err := self.requireCandidateRole(observation, fleetLifecycleTargetFleet, churnHotkeyLabel(fleetLifecycleTargetChurn), fleetLifecycleCompanionExpectedUID); err != nil {
			return err
		}
		if err := self.requireCandidateRole(observation, fleetLifecycleCompanionFleet, churnHotkeyLabel(fleetLifecycleFallbackChurn), fleetLifecycleTargetExpectedUID); err != nil {
			return err
		}
		if !fleetLifecycleCensusRejects(*latest, fleetLifecycleTargetExpectedUID, fleetLifecycleCompanionExpectedUID) {
			return errors.New("fleet lifecycle provider-active decision did not preserve exact zero weights")
		}
		if err := self.releaseMutationScheduleFits(latest, 0, 1); err != nil {
			return err
		}
		providerExcluded, providerFound, err := self.payoutEvidence(observation, self.evidence.ProviderEffectiveEpoch, fleetLifecycleMembers(self.cfg, fleetLifecycleTargetFleet), true, "reregistered-provider-head-excluded")
		if err != nil || !providerFound {
			return stateMismatchError(err, "fleet lifecycle has no exact full-settlement restored-provider head exclusion")
		}
		companionPaid, companionFound, err := self.payoutEvidence(observation, self.evidence.ProviderEffectiveEpoch, fleetLifecycleMembers(self.cfg, fleetLifecycleCompanionFleet), false, "second-pruned-provider-returned-to-operator-pool")
		if err != nil || !companionFound {
			return stateMismatchError(err, "fleet lifecycle has no exact full-settlement companion pool payout")
		}
		if err := self.appendPayout(providerExcluded); err != nil {
			return err
		}
		if err := self.appendPayout(companionPaid); err != nil {
			return err
		}
		if err := self.markCensusMilestone(*latest, fleetLifecycleMilestoneProviderActive); err != nil {
			return err
		}
		terminalVictim, err := roleBytes32(self.executor.roles, churnHotkeyLabel(fleetLifecycleTerminalVictimChurn))
		if err != nil {
			return err
		}
		terminalOwner, err := roleBytes32(self.executor.roles, churnColdkeyLabel(fleetLifecycleTerminalVictimChurn))
		if err != nil {
			return err
		}
		prune, err := self.executor.substrate.FleetLifecyclePruneSnapshot()
		if err != nil {
			return err
		}
		if err := validateFleetLifecyclePruneSnapshot(prune, terminalVictim, terminalOwner); err != nil {
			return err
		}
		self.evidence.Stage = fleetLifecycleStageProviderPaid
		self.resumeValidated = false
		if err := self.write(); err != nil {
			return err
		}
		return self.finishTerminalInstall(ctx, members)
	case fleetLifecycleStageProviderPaid:
		return self.finishTerminalInstall(ctx, members)
	case fleetLifecycleStageTerminalInstalled:
		if !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-target-prune", "restored") || !fleetLifecycleFaultStatus(faults, "fleet-lifecycle-companion-prune", "restored") || observation.Status.Contracts.CurrentEpoch < self.evidence.TerminalEffectiveEpoch || observation.Status.Contracts.FinalizedHead.Number < self.evidence.AcceptanceEndBlock {
			return self.write()
		}
		if err := validateFleetLifecycleDecisionCoverage(observation, self.evidence, self.phase, terminalBlock); err != nil {
			return err
		}
		self.evidence.Stage = fleetLifecycleStageReleaseHandoff
		self.resumeValidated = false
		if err := self.write(); err != nil {
			return err
		}
		return self.validateResumeLineage(ctx)
	case fleetLifecycleStageReleaseHandoff:
		return self.validateResumeLineage(ctx)
	case fleetLifecycleStageComplete:
		return self.write()
	default:
		return fmt.Errorf("unsupported fleet lifecycle stage %q", self.evidence.Stage)
	}
}
