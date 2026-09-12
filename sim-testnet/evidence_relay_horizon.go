//go:build linux || darwin

// A funded campaign has one activation-anchored horizon. Actual immutable
// producer work is a forecast, not a restriction on public audit subjects;
// additional valid subjects consume the same finite delay/subject headroom.
package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/urfoundation/sn/protocol"
)

// Phase work is expressed in configured block equivalents. It does not
// predict public block speed or promise unbounded preparation/archive I/O.
type evidenceRelayWork struct {
	releasePreparation    uint64
	releaseObservation    uint64
	productionPreparation uint64
	productionObservation uint64
	settlementCadence     uint64
	nativeCadence         uint64
}

// Use the actual scenario definitions/watchdogs, including fault schedules,
// and existing preparation waits. Residual approved slots own all other I/O.
func evidenceRelayConfiguredWork(cfg *ResolvedConfig) (evidenceRelayWork, error) {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || cfg.Policy == nil || cfg.Hyperparameters == nil || cfg.Public.Chain.ExpectedBlockSeconds == 0 {
		return evidenceRelayWork{}, errors.New("evidence relay work has no complete configured clocks")
	}
	work := evidenceRelayWork{settlementCadence: min(cfg.Policy.Settlement.EpochBlocks, cfg.Policy.ProductionCadence.EpochBlocks), nativeCadence: hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"])}
	if work.settlementCadence == 0 || work.nativeCadence == 0 || cfg.Config.Topology.ClientsPerHeadFleet <= 0 {
		return evidenceRelayWork{}, errors.New("evidence relay work has a zero cadence or missing fleet geometry")
	}
	seconds := cfg.Public.Chain.ExpectedBlockSeconds
	ceil := func(value, divisor uint64) uint64 {
		result := value / divisor
		if value%divisor != 0 {
			result++
		}
		return result
	}
	watchdog := func(phase string) (uint64, error) {
		definition, err := scenarioDefinitionFor(cfg, phase)
		if err != nil {
			return 0, err
		}
		duration, err := evidenceRelayWatchdogDuration(cfg, definition)
		if err != nil {
			return 0, err
		}
		return ceil(ceil(uint64(duration), uint64(time.Second)), seconds), nil
	}
	var err error
	work.releaseObservation, err = watchdog("release-1.0")
	if err != nil {
		return evidenceRelayWork{}, err
	}
	work.productionObservation, err = watchdog("production-soak")
	if err != nil {
		return evidenceRelayWork{}, err
	}
	sum := func(values ...uint64) (uint64, error) {
		var result uint64
		for _, value := range values {
			next, ok := checkedAdd(result, value)
			if !ok {
				return 0, errors.New("evidence relay work overflows")
			}
			result = next
		}
		return result, nil
	}
	twoNative, ok := checkedMul(work.nativeCadence, 2)
	if !ok {
		return evidenceRelayWork{}, errors.New("evidence relay native preparation overflows")
	}
	bindings, ok := checkedMul(uint64(cfg.Config.Topology.ClientsPerHeadFleet), 2)
	if !ok {
		return evidenceRelayWork{}, errors.New("evidence relay preparation source count overflows")
	}
	bindWindows, ok := checkedMul(bindings, futureEpochInclusionSafetyBlocks)
	if !ok {
		return evidenceRelayWork{}, errors.New("evidence relay binding wait overflows")
	}
	// Precompile dividend, governance readiness, then both takeover binding
	// groups. Each future binding may arrive in the configured unsafe tail.
	work.releasePreparation, err = sum(twoNative, 20, cfg.Policy.Settlement.EpochBlocks, cfg.Policy.Settlement.FinalizeOffsetBlocks, 20, bindWindows)
	if err != nil {
		return evidenceRelayWork{}, err
	}
	auditBlocks, err := sum(cfg.Policy.ProductionCadence.RootCommitWindowBlocks, twoNative)
	if err != nil {
		return evidenceRelayWork{}, err
	}
	auditBlocks = max(auditBlocks, cfg.Policy.ProductionCadence.EpochBlocks)
	// Future-policy tail, a fresh complete production boundary, the exact
	// dishonest-audit watchdog and the existing 30-second recovery health wait.
	work.productionPreparation, err = sum(futureEpochInclusionSafetyBlocks, max(cfg.Policy.Settlement.EpochBlocks, cfg.Policy.ProductionCadence.EpochBlocks), auditBlocks, ceil(120, seconds), ceil(30, seconds))
	if err != nil {
		return evidenceRelayWork{}, err
	}
	return work, nil
}

// Check the actual two campaign watchdog formulas before their duration
// conversion. Equality with the execution deadline makes future drift fail
// admission rather than silently funding fewer blocks than the real phase.
func evidenceRelayWatchdogDuration(cfg *ResolvedConfig, definition scenarioDefinition) (time.Duration, error) {
	blocks, ok := checkedMul(definition.GoalEpochs, cfg.Policy.Settlement.EpochBlocks)
	if !ok {
		return 0, errors.New("evidence relay observation block count overflows")
	}
	if definition.Name == "release-1.0" {
		accepted, ok := checkedAdd(blocks, cfg.Policy.Settlement.FinalizeOffsetBlocks)
		if !ok {
			return 0, errors.New("evidence relay release finalization count overflows")
		}
		lifecycle, err := fleetLifecycleReleaseScheduleRequired(hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["tempo"]), hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["commit_reveal_period"]))
		if err != nil {
			return 0, err
		}
		blocks, ok = checkedAdd(cfg.Policy.Settlement.EpochBlocks, max(accepted, lifecycle))
		if !ok {
			return 0, errors.New("evidence relay release baseline count overflows")
		}
	} else if definition.Name == "production-soak" {
		if cfg.Config.Scenarios.ProductionEpochs <= 0 {
			return 0, errors.New("evidence relay production epoch count is absent")
		}
		epochs, ok := checkedAdd(uint64(cfg.Config.Scenarios.ProductionEpochs), 1)
		if !ok {
			return 0, errors.New("evidence relay production baseline count overflows")
		}
		blocks, ok = checkedMul(epochs, cfg.Policy.ProductionCadence.EpochBlocks)
		if !ok {
			return 0, errors.New("evidence relay production epoch count overflows")
		}
		blocks, ok = checkedAdd(blocks, cfg.Policy.ProductionCadence.FinalizeOffsetBlocks)
		if !ok {
			return 0, errors.New("evidence relay production finalization count overflows")
		}
	} else {
		return 0, errors.New("evidence relay watchdog has no funded phase")
	}
	for _, fault := range definition.Faults {
		end, ok := checkedAdd(fault.TriggerOffsetBlocks, fault.DurationBlocks)
		if !ok {
			return 0, errors.New("evidence relay fault watchdog overflows")
		}
		blocks = max(blocks, end)
	}
	if blocks == 0 {
		return 0, errors.New("evidence relay observation has no bounded block work")
	}
	blocks, ok = checkedAdd(blocks, 10)
	if !ok {
		return 0, errors.New("evidence relay watchdog slack overflows")
	}
	seconds, ok := checkedMul(blocks, cfg.Public.Chain.ExpectedBlockSeconds)
	if !ok {
		return 0, errors.New("evidence relay watchdog clock overflows")
	}
	seconds, ok = checkedAdd(seconds, 120)
	if !ok {
		return 0, errors.New("evidence relay watchdog grace overflows")
	}
	nanos, ok := checkedMul(seconds, uint64(time.Second))
	if !ok || nanos > uint64(^uint64(0)>>1) {
		return 0, errors.New("evidence relay watchdog duration overflows")
	}
	if definition.AdversarialMatrixHash != "" {
		adversaries := cfg.Config.Scenarios.Adversaries
		if adversaries.MinimumSamplesPerActor < 0 || adversaries.SampleIntervalMilliseconds <= 0 || adversaries.RequestTimeoutMilliseconds <= 0 {
			return 0, errors.New("evidence relay adversary watchdog is invalid")
		}
		samples, ok := checkedAdd(uint64(adversaries.MinimumSamplesPerActor), 2)
		if !ok {
			return 0, errors.New("evidence relay adversary sample count overflows")
		}
		millis, ok := checkedMul(samples, uint64(adversaries.SampleIntervalMilliseconds))
		if !ok {
			return 0, errors.New("evidence relay adversary interval overflows")
		}
		millis, ok = checkedAdd(millis, uint64(adversaries.RequestTimeoutMilliseconds))
		if !ok {
			return 0, errors.New("evidence relay adversary request grace overflows")
		}
		minimum, ok := checkedMul(millis, uint64(time.Millisecond))
		if !ok || minimum > uint64(^uint64(0)>>1) {
			return 0, errors.New("evidence relay adversary duration overflows")
		}
		nanos = max(nanos, minimum)
	}
	duration := time.Duration(nanos)
	if duration != scenarioTimeout(cfg, definition) {
		return 0, errors.New("evidence relay forecast differs from actual scenario watchdog")
	}
	return duration, nil
}

// Production cannot reset the original horizon. Release entry reserves the
// later production work too; post-preparation only drops completed setup work.
func (self evidenceRelayWork) remaining(phase string, prepared bool) (uint64, error) {
	values := []uint64{self.productionObservation}
	if phase == "release-1.0" {
		values = append(values, self.releaseObservation, self.productionPreparation)
		if !prepared {
			values = append(values, self.releasePreparation)
		}
	} else if phase == "production-soak" {
		if !prepared {
			values = append(values, self.productionPreparation)
		}
	} else {
		return 0, errors.New("evidence relay horizon has no complete campaign phase")
	}
	var result uint64
	for _, value := range values {
		next, ok := checkedAdd(result, value)
		if !ok || value == 0 {
			return 0, errors.New("evidence relay remaining work is zero or overflows")
		}
		result = next
	}
	return result, nil
}

// One source is the exact original validator hotkey/operator activation.
// No candidate-provided validator index or arrival order selects this owner.
type evidenceRelayHorizonSource struct {
	hotkey [32]byte
	noId   uint64
}

// Only the existing relay worker mutates this bounded metadata. Original
// requests and public bytes remain retained by their existing disk owners.
type evidenceRelayHorizon struct {
	work              evidenceRelayWork
	maximum           uint64
	anchorBlock       uint64
	anchorEpoch       uint64
	anchorNativeEpoch uint64
	minimumEnd        uint64
	minimumNativeEnd  uint64
	sourceKVs         map[evidenceRelayHorizonSource]protocol.ValidatorEvidenceActivation
	headerKVs         map[[32]byte]protocol.ValidatorEvidenceHeader
	continuation      *EvidenceRelayContinuation
}

// Ceilings count a partial boundary on both clocks. The one/native term is
// solely the real producer forecast: every additional actual subject is also
// debited, even at the same native epoch or delivered in another order.
func (self *evidenceRelayHorizon) forecast(span uint64) (uint64, uint64, uint64, error) {
	if self == nil {
		return 0, 0, 0, errors.New("evidence relay horizon forecast owner is absent")
	}
	return evidenceRelayForecast(self.work, uint64(len(self.sourceKVs)), span)
}

// The same finite population/clock arithmetic serves source-storage admission
// and the actual authenticated runtime census; it never allocates subjects.
func evidenceRelayForecast(work evidenceRelayWork, sources, span uint64) (uint64, uint64, uint64, error) {
	if sources == 0 || work.settlementCadence == 0 || work.nativeCadence == 0 {
		return 0, 0, 0, errors.New("evidence relay horizon forecast owner is absent")
	}
	count := func(cadence uint64) (uint64, error) {
		value := span / cadence
		if span%cadence != 0 {
			value++
		}
		value, ok := checkedAdd(value, 1)
		if !ok {
			return 0, errors.New("evidence relay horizon epoch count overflows")
		}
		return value, nil
	}
	closed, err := count(work.settlementCadence)
	if err != nil {
		return 0, 0, 0, err
	}
	audits, err := count(work.nativeCadence)
	if err != nil {
		return 0, 0, 0, err
	}
	perSource, ok := checkedAdd(closed, audits)
	if !ok {
		return 0, 0, 0, errors.New("evidence relay horizon source count overflows")
	}
	all, ok := checkedMul(perSource, sources)
	if !ok {
		return 0, 0, 0, errors.New("evidence relay horizon population overflows")
	}
	return all, closed, audits, nil
}

// Maximum no-extra span is a resource ceiling, never a promised completion
// time. The actual worker subtracts every additional valid audit subject.
func evidenceRelayConfiguredHorizon(cfg *ResolvedConfig) (uint64, uint64, uint64, error) {
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		return 0, 0, 0, err
	}
	if cfg.Config.Topology.Validators <= 0 || cfg.Config.Topology.Operators <= 0 {
		return 0, 0, 0, errors.New("evidence relay configured source census is absent")
	}
	sources, ok := checkedMul(uint64(cfg.Config.Topology.Validators), uint64(cfg.Config.Topology.Operators))
	if !ok {
		return 0, 0, 0, errors.New("evidence relay configured source census overflows")
	}
	return evidenceRelayMaximumSpan(work, sources, cfg.Config.ValidatorEvidenceRelay.MaxSlots, 0)
}

// Binary search a checked monotone count, not the ordering of public headers.
func evidenceRelayMaximumSpan(work evidenceRelayWork, sources, maximum, extra uint64) (uint64, uint64, uint64, error) {
	if extra >= maximum {
		return 0, 0, 0, errors.New("evidence relay extra subjects exhausted the approved allowance")
	}
	upper, ok := checkedMul(maximum-extra, work.settlementCadence)
	if !ok {
		return 0, 0, 0, errors.New("evidence relay block horizon overflows")
	}
	var lower uint64
	for lower < upper {
		middle := lower + (upper-lower)/2 + (upper-lower)%2
		required, _, _, err := evidenceRelayForecast(work, sources, middle)
		if err != nil {
			return 0, 0, 0, err
		}
		if required > maximum-extra {
			upper = middle - 1
		} else {
			lower = middle
		}
	}
	required, closed, native, err := evidenceRelayForecast(work, sources, lower)
	if err != nil || required > maximum-extra {
		return 0, 0, 0, errors.Join(errors.New("evidence relay allowance cannot cover its original source census"), err)
	}
	return lower, closed, native, nil
}

// Every same-native additional subject consumes one extra original slot.
// Set cardinality, not journal/delivery chronology, proves this debit.
func (self *evidenceRelayHorizon) extraSubjects(candidate *protocol.ValidatorEvidenceHeader) (uint64, error) {
	type nativeSource struct {
		source evidenceRelayHorizonSource
		epoch  uint64
	}
	seenKVs := map[nativeSource]bool{}
	var audits uint64
	add := func(header protocol.ValidatorEvidenceHeader) {
		if header.Kind == protocol.ValidatorEvidenceDepositAudit {
			audits++
			seenKVs[nativeSource{source: evidenceRelayHorizonSource{hotkey: header.Hotkey, noId: header.NoID}, epoch: header.Subject.NativeEpoch}] = true
		}
	}
	for _, header := range self.headerKVs {
		add(header)
	}
	if candidate != nil {
		slot, err := candidate.SlotKey()
		if err != nil {
			return 0, err
		}
		if _, found := self.headerKVs[slot]; !found {
			add(*candidate)
		}
	}
	return audits - uint64(len(seenKVs)), nil
}

// Original activation plus remaining approved slots determines the furthest
// funded block, settlement epoch and native epoch. No restart supplies a new
// origin, and extra audit subjects shrink headroom rather than new approval.
func (self *evidenceRelayHorizon) ceilings(candidate *protocol.ValidatorEvidenceHeader) (uint64, uint64, uint64, error) {
	if self == nil || self.maximum == 0 || self.anchorBlock == 0 {
		return 0, 0, 0, errors.New("evidence relay horizon is absent")
	}
	if self.continuation != nil {
		required, err := self.continuation.requiredSubjects(self.headerKVs, candidate)
		if err != nil || required > self.maximum {
			return 0, 0, 0, errors.Join(errors.New("relay continuation exhausted its retained plus future subject allowance"), err)
		}
		return self.continuation.EndBlock, self.continuation.EndSettlementEpoch, self.continuation.EndNativeEpoch, nil
	}
	extra, err := self.extraSubjects(candidate)
	if err != nil {
		return 0, 0, 0, err
	}
	span, closed, native, err := evidenceRelayMaximumSpan(self.work, uint64(len(self.sourceKVs)), self.maximum, extra)
	if err != nil {
		return 0, 0, 0, err
	}
	block, blockOk := checkedAdd(self.anchorBlock, span)
	epoch, epochOk := checkedAdd(self.anchorEpoch, closed-1)
	nativeEpoch, nativeOk := checkedAdd(self.anchorNativeEpoch, native-1)
	if !blockOk || !epochOk || !nativeOk {
		return 0, 0, 0, errors.New("evidence relay anchored horizon overflows")
	}
	return block, epoch, nativeEpoch, nil
}

// A private discovery record cannot replace a source, refund a failed debit
// or change an immutable header. Public signature and whole-census validation
// happen before this arithmetic admission, never instead of it.
func (self *evidenceRelayHorizon) admit(header protocol.ValidatorEvidenceHeader, currentBlock uint64) error {
	if self == nil {
		return errors.New("evidence relay horizon admission owner is absent")
	}
	source := evidenceRelayHorizonSource{hotkey: header.Hotkey, noId: header.NoID}
	activation, found := self.sourceKVs[source]
	if !found {
		return errors.New("evidence relay horizon source differs from original activation")
	}
	domain, err := activation.EvidenceDomain()
	if err != nil || header.Domain != domain || header.VPK != activation.VPK {
		return errors.Join(errors.New("evidence relay horizon source domain changed"), err)
	}
	slot, err := header.SlotKey()
	if err != nil {
		return err
	}
	if previous, found := self.headerKVs[slot]; found {
		if previous != header {
			return errors.New("evidence relay horizon retry changes its original header")
		}
	} else if uint64(len(self.headerKVs)) >= self.maximum {
		return errors.New("evidence relay horizon has no remaining original slots")
	}
	block, epoch, nativeEpoch, err := self.ceilings(&header)
	if err != nil {
		return err
	}
	if currentBlock < self.anchorBlock || currentBlock > block || self.minimumEnd > block || self.minimumNativeEnd > nativeEpoch || header.Epoch < self.anchorEpoch || header.Epoch > epoch || header.BoundaryBlock > currentBlock || header.BoundaryBlock > block ||
		header.Kind == protocol.ValidatorEvidenceDepositAudit && (header.Subject.NativeEpoch < self.anchorNativeEpoch || header.Subject.NativeEpoch > nativeEpoch || header.Subject.ObservationEpoch > epoch) {
		return fmt.Errorf("evidence relay insufficient remaining horizon before spend: observed_block=%d required_end=%d funded_end=%d", currentBlock, self.minimumEnd, block)
	}
	// Extra subjects cannot evict an already authenticated delayed/future
	// header. Check the whole set, never journal or delivery ordering.
	for _, retained := range self.headerKVs {
		if retained.Epoch > epoch || retained.BoundaryBlock > block || retained.Kind == protocol.ValidatorEvidenceDepositAudit && (retained.Subject.NativeEpoch > nativeEpoch || retained.Subject.ObservationEpoch > epoch) {
			return errors.New("evidence relay extra subject would underfund an original retained slot")
		}
	}
	self.headerKVs[slot] = header
	return nil
}

// A fresh phase/after-preparation snapshot must still leave every remaining
// required block inside the same original allowance; no files or sends occur.
func (self *evidenceRelayHorizon) requireRemaining(currentBlock, nativeEpoch, remaining uint64) error {
	end, ok := checkedAdd(currentBlock, remaining)
	if !ok || remaining == 0 {
		return errors.New("evidence relay remaining horizon is zero or overflows")
	}
	block, _, maximumNativeEpoch, err := self.ceilings(nil)
	if err != nil {
		return err
	}
	nativeRemaining := remaining / self.work.nativeCadence
	if remaining%self.work.nativeCadence != 0 {
		nativeRemaining++
	}
	nativeEnd, nativeOk := checkedAdd(nativeEpoch, nativeRemaining)
	if !nativeOk || currentBlock < self.anchorBlock || currentBlock > block || nativeEpoch < self.anchorNativeEpoch || nativeEpoch > maximumNativeEpoch || nativeEnd > maximumNativeEpoch || end > block {
		return fmt.Errorf("evidence relay insufficient remaining horizon before preparation: observed_block=%d native_epoch=%d required_end=%d funded_end=%d", currentBlock, nativeEpoch, end, block)
	}
	self.minimumEnd = end
	self.minimumNativeEnd = nativeEnd
	return nil
}
