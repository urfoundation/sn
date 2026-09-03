package validator

// measurement_stats.go defines the canonical, integer-only statistics input
// consumed by release steering. The detached form is safe to publish: it
// contains provider ids, counters, latency buckets, prior EMA values and
// salted egress-prefix hashes, but no JWTs, private keys or raw IP addresses.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/protocol"
)

// ReleaseStatsConfig is the exact integer scoring policy active at capture.
type ReleaseStatsConfig struct {
	AMin             uint64 `json:"a_min"`
	AlphaNumerator   uint64 `json:"alpha_numerator"`
	AlphaDenominator uint64 `json:"alpha_denominator"`
	LatRefMillis     uint64 `json:"latency_reference_millis"`
}

// ReleaseProviderMeasurement is one provider's complete scoring input. A
// separate boolean distinguishes an absent prior EMA from a valid zero value.
type ReleaseProviderMeasurement struct {
	ClientID          string   `json:"client_id"`
	Assignments       uint64   `json:"assignments"`
	Confirmations     uint64   `json:"confirmations"`
	LatencyBuckets    []uint64 `json:"latency_buckets"`
	HasPriorQuality   bool     `json:"has_prior_quality"`
	PriorQualityPPM   uint32   `json:"prior_quality_ppm"`
	EgressIPHashHexes []string `json:"egress_ip_hashes"`
}

// ReleaseStatsMeasurement is a canonical point-in-time view of one isolated
// operator measurement engine. Egress hashes are detached at a native-tempo
// boundary; settlement counters remain live until their settlement fold.
type ReleaseStatsMeasurement struct {
	Config               ReleaseStatsConfig           `json:"config"`
	Providers            []ReleaseProviderMeasurement `json:"providers"`
	AttemptCut           *AttemptLedgerCut            `json:"attempt_cut,omitempty"`
	SettlementTransition *AttemptSettlementTransition `json:"settlement_transition,omitempty"`
}

// VerifiedReleaseProvider is the independently reconstructed quality and
// exposure for one canonical provider input.
type VerifiedReleaseProvider struct {
	ClientID       connect.Id
	QualityPPM     uint32
	HasQuality     bool
	Exposure       uint64
	EgressIPHashes map[[32]byte]bool
}

// VerifiedReleaseStats is the validated lookup form used by head and pool
// reconstruction. Callers must treat the maps as immutable.
type VerifiedReleaseStats struct {
	Providers map[connect.Id]VerifiedReleaseProvider
}

// releaseP95UpperMillis computes ceil(95*n/100) without floating-point or
// multiplication overflow, then returns the conservative bucket upper edge.
func releaseP95UpperMillis(latencyBuckets []uint64) (uint64, error) {
	if len(latencyBuckets) != statsLatencyBuckets {
		return 0, fmt.Errorf("latency bucket count %d, want %d", len(latencyBuckets), statsLatencyBuckets)
	}
	var total uint64
	for _, count := range latencyBuckets {
		if ^uint64(0)-total < count {
			return 0, errors.New("latency bucket total overflows uint64")
		}
		total += count
	}
	if total == 0 {
		return 0, nil
	}
	rank := total - total/20
	var cumulative uint64
	for index, count := range latencyBuckets {
		cumulative += count
		if cumulative >= rank {
			return uint64(1) << uint(index), nil
		}
	}
	return 0, errors.New("latency percentile is outside the histogram")
}

// releaseQualityPPM reconstructs the production integer Wilson, latency and
// EMA transform from one raw provider record.
func releaseQualityPPM(config ReleaseStatsConfig, provider ReleaseProviderMeasurement) (uint32, bool, error) {
	if config.LatRefMillis > ^uint64(0)/1_000_000 || config.AlphaDenominator > ^uint64(0)/1_000_000 {
		return 0, false, errors.New("release statistics config overflows score arithmetic")
	}
	if provider.Confirmations > provider.Assignments {
		return 0, false, errors.New("confirmations exceed assignments")
	}
	var samples uint64
	for _, count := range provider.LatencyBuckets {
		if ^uint64(0)-samples < count {
			return 0, false, errors.New("latency sample total overflows uint64")
		}
		samples += count
	}
	if samples != provider.Confirmations {
		return 0, false, fmt.Errorf("latency samples %d do not equal confirmations %d", samples, provider.Confirmations)
	}
	if provider.PriorQualityPPM > 1_000_000 || (!provider.HasPriorQuality && provider.PriorQualityPPM != 0) {
		return 0, false, errors.New("prior quality is not canonical")
	}
	if provider.Assignments < config.AMin {
		return provider.PriorQualityPPM, provider.HasPriorQuality, nil
	}
	p95, err := releaseP95UpperMillis(provider.LatencyBuckets)
	if err != nil {
		return 0, false, err
	}
	if config.LatRefMillis > ^uint64(0)-p95 {
		return 0, false, errors.New("latency denominator overflows uint64")
	}
	reliability := uint64(protocol.ReliabilityPPM(provider.Confirmations, provider.Assignments, config.AMin))
	raw := uint32(reliability * config.LatRefMillis / (config.LatRefMillis + p95))
	if !provider.HasPriorQuality {
		return raw, true, nil
	}
	numerator := config.AlphaNumerator*uint64(raw) + (config.AlphaDenominator-config.AlphaNumerator)*uint64(provider.PriorQualityPPM)
	return uint32(numerator / config.AlphaDenominator), true, nil
}

// VerifyReleaseStatsMeasurement rejects non-canonical or internally
// inconsistent measurements and returns their independently derived scores.
func VerifyReleaseStatsMeasurement(measurement ReleaseStatsMeasurement) (VerifiedReleaseStats, error) {
	config := measurement.Config
	if config.AMin == 0 || config.AlphaDenominator == 0 || config.AlphaNumerator > config.AlphaDenominator || config.LatRefMillis == 0 {
		return VerifiedReleaseStats{}, errors.New("release statistics config is invalid")
	}
	verified := VerifiedReleaseStats{Providers: make(map[connect.Id]VerifiedReleaseProvider, len(measurement.Providers))}
	priorClientID := ""
	for index, provider := range measurement.Providers {
		clientID, err := connect.ParseId(provider.ClientID)
		if err != nil || clientID.String() != provider.ClientID {
			return VerifiedReleaseStats{}, fmt.Errorf("provider %d client id is not canonical", index)
		}
		if priorClientID != "" && provider.ClientID <= priorClientID {
			return VerifiedReleaseStats{}, errors.New("release providers are not strictly ordered")
		}
		priorClientID = provider.ClientID
		if len(provider.LatencyBuckets) != statsLatencyBuckets {
			return VerifiedReleaseStats{}, fmt.Errorf("provider %s latency bucket count %d, want %d", provider.ClientID, len(provider.LatencyBuckets), statsLatencyBuckets)
		}
		qualityPPM, hasQuality, err := releaseQualityPPM(config, provider)
		if err != nil {
			return VerifiedReleaseStats{}, fmt.Errorf("provider %s quality: %w", provider.ClientID, err)
		}
		egress := make(map[[32]byte]bool, len(provider.EgressIPHashHexes))
		priorHash := ""
		for hashIndex, encoded := range provider.EgressIPHashHexes {
			if encoded != strings.ToLower(encoded) || len(encoded) != 66 || !strings.HasPrefix(encoded, "0x") || (priorHash != "" && encoded <= priorHash) {
				return VerifiedReleaseStats{}, fmt.Errorf("provider %s egress hash %d is not canonical", provider.ClientID, hashIndex)
			}
			decoded, decodeErr := hex.DecodeString(encoded[2:])
			if decodeErr != nil || len(decoded) != 32 {
				return VerifiedReleaseStats{}, fmt.Errorf("provider %s egress hash %d is invalid", provider.ClientID, hashIndex)
			}
			var hash [32]byte
			copy(hash[:], decoded)
			if hash == ([32]byte{}) {
				return VerifiedReleaseStats{}, fmt.Errorf("provider %s contains the zero egress hash", provider.ClientID)
			}
			egress[hash] = true
			priorHash = encoded
		}
		verified.Providers[clientID] = VerifiedReleaseProvider{
			ClientID: clientID, QualityPPM: qualityPPM, HasQuality: hasQuality,
			Exposure: provider.Assignments, EgressIPHashes: egress,
		}
	}
	if measurement.AttemptCut != nil {
		vpkBytes, err := canonicalAttemptHex32("attempt cut validator vpk", measurement.AttemptCut.Identity.ValidatorVPK, false)
		if err != nil {
			return VerifiedReleaseStats{}, err
		}
		if err := verifyAttemptLedgerCut(measurement.AttemptCut, vpkBytes[:], nil, false); err != nil {
			return VerifiedReleaseStats{}, fmt.Errorf("attempt cut: %w", err)
		}
		if err := verifyReleaseStatsAgainstAttemptCut(measurement, verified); err != nil {
			return VerifiedReleaseStats{}, err
		}
	}
	if measurement.SettlementTransition != nil {
		if err := verifyAttemptSettlementTransitionForMeasurement(measurement.SettlementTransition, measurement); err != nil {
			return VerifiedReleaseStats{}, fmt.Errorf("settlement transition: %w", err)
		}
	}
	return verified, nil
}

type attemptProviderReplay struct {
	assignments    uint64
	confirmations  uint64
	latencyBuckets [statsLatencyBuckets]uint64
	egress         map[[32]byte]bool
}

// verifyReleaseStatsAgainstAttemptCut makes the signed attempt range the
// authority for every raw counter and routable-prefix hash. Prior EMA values
// are checked separately by cross-artifact settlement lineage.
func verifyReleaseStatsAgainstAttemptCut(measurement ReleaseStatsMeasurement, verified VerifiedReleaseStats) error {
	cut := measurement.AttemptCut
	replayed := map[connect.Id]*attemptProviderReplay{}
	provider := func(clientID connect.Id) *attemptProviderReplay {
		value := replayed[clientID]
		if value == nil {
			value = &attemptProviderReplay{egress: map[[32]byte]bool{}}
			replayed[clientID] = value
		}
		return value
	}
	for _, record := range cut.Records {
		if record.Disposition == AttemptDispositionPending {
			continue
		}
		for _, assignment := range record.Assignments {
			value := provider(assignment.NextHop)
			value.assignments++
			if assignment.Confirmed {
				value.confirmations++
				value.latencyBuckets[assignment.LatencyBucket]++
			}
		}
		if record.Sequence < cut.EgressFirstSequence || record.Disposition != AttemptDispositionComplete || record.Proof == nil {
			continue
		}
		for hopIndex := 1; hopIndex < len(record.Proof.Hops); hopIndex++ {
			hop := record.Proof.Hops[hopIndex]
			if hop.EgressIpHash != ([32]byte{}) {
				provider(hop.ClientId).egress[hop.EgressIpHash] = true
			}
		}
	}
	for clientID, expected := range replayed {
		actual, ok := verified.Providers[clientID]
		if !ok {
			return fmt.Errorf("attempt provider %s is absent from release statistics", clientID)
		}
		if actual.Exposure != expected.assignments {
			return fmt.Errorf("provider %s assignments differ from signed attempts", clientID)
		}
		measurementProvider := measurement.Providers[sort.Search(len(measurement.Providers), func(index int) bool { return measurement.Providers[index].ClientID >= clientID.String() })]
		if measurementProvider.Confirmations != expected.confirmations || len(measurementProvider.LatencyBuckets) != len(expected.latencyBuckets) {
			return fmt.Errorf("provider %s confirmations differ from signed attempts", clientID)
		}
		for bucket := range expected.latencyBuckets {
			if measurementProvider.LatencyBuckets[bucket] != expected.latencyBuckets[bucket] {
				return fmt.Errorf("provider %s latency bucket %d differs from signed attempts", clientID, bucket)
			}
		}
		if len(actual.EgressIPHashes) != len(expected.egress) {
			return fmt.Errorf("provider %s egress hashes differ from signed attempts", clientID)
		}
		for hash := range expected.egress {
			if !actual.EgressIPHashes[hash] {
				return fmt.Errorf("provider %s egress hash is absent from signed attempts", clientID)
			}
		}
	}
	for clientID, actual := range verified.Providers {
		if expected := replayed[clientID]; expected == nil {
			measurementProvider := measurement.Providers[sort.Search(len(measurement.Providers), func(index int) bool { return measurement.Providers[index].ClientID >= clientID.String() })]
			if actual.Exposure != 0 || measurementProvider.Confirmations != 0 || len(actual.EgressIPHashes) != 0 {
				return fmt.Errorf("provider %s has raw statistics absent from signed attempts", clientID)
			}
		}
	}
	return nil
}

// releaseStatsMeasurementWithLock copies the complete integer scoring state.
// The caller must hold the engine state lock.
func (self *StatsEngine) releaseStatsMeasurementWithLock() ReleaseStatsMeasurement {
	clientIDSet := map[connect.Id]bool{}
	for clientID := range self.window {
		clientIDSet[clientID] = true
	}
	for clientID := range self.emaPPM {
		clientIDSet[clientID] = true
	}
	for clientID := range self.egress {
		clientIDSet[clientID] = true
	}
	clientIDs := make([]connect.Id, 0, len(clientIDSet))
	for clientID := range clientIDSet {
		clientIDs = append(clientIDs, clientID)
	}
	sort.Slice(clientIDs, func(i, j int) bool { return clientIDs[i].LessThan(clientIDs[j]) })
	measurement := ReleaseStatsMeasurement{
		Config: ReleaseStatsConfig{
			AMin: self.cfg.AMin, AlphaNumerator: self.cfg.AlphaNumerator,
			AlphaDenominator: self.cfg.AlphaDenominator, LatRefMillis: self.cfg.LatRefMillis,
		},
		Providers:            make([]ReleaseProviderMeasurement, 0, len(clientIDs)),
		SettlementTransition: self.settlementTransition,
	}
	for _, clientID := range clientIDs {
		provider := ReleaseProviderMeasurement{ClientID: clientID.String(), LatencyBuckets: make([]uint64, statsLatencyBuckets)}
		if window := self.window[clientID]; window != nil {
			provider.Assignments = window.Assignments
			provider.Confirmations = window.Confirmations
			copy(provider.LatencyBuckets, window.LatencyBuckets[:])
		}
		if prior, ok := self.emaPPM[clientID]; ok {
			provider.HasPriorQuality = true
			provider.PriorQualityPPM = prior
		}
		for hash := range self.egress[clientID] {
			provider.EgressIPHashHexes = append(provider.EgressIPHashHexes, "0x"+hex.EncodeToString(hash[:]))
		}
		sort.Strings(provider.EgressIPHashHexes)
		measurement.Providers = append(measurement.Providers, provider)
	}
	return measurement
}

// currentReleaseStatsMeasurement takes a coherent non-destructive statistics
// snapshot. It is used by compatibility helpers outside release journaling.
func (self *StatsEngine) currentReleaseStatsMeasurement() ReleaseStatsMeasurement {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.releaseStatsMeasurementWithLock()
}

// detachReleaseStatsMeasurement persists a write-ahead snapshot while holding
// the measurement lock and rotates egress evidence only after persistence
// succeeds. This deliberately trades a short disk-write pause for crash-safe
// native-window ownership; a failed write leaves all evidence in memory.
func (self *StatsEngine) detachReleaseStatsMeasurement(dir string, persist func(ReleaseStatsMeasurement, uint64) error) (ReleaseStatsMeasurement, error) {
	self.mu.Lock()
	hasAttemptLedger := self.attemptLedger != nil
	self.mu.Unlock()
	if hasAttemptLedger {
		return ReleaseStatsMeasurement{}, errors.New("attempt-backed release statistics require an exact cut boundary")
	}
	if persist == nil {
		return ReleaseStatsMeasurement{}, errors.New("release statistics persistence callback is nil")
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.egressGeneration == ^uint64(0) {
		return ReleaseStatsMeasurement{}, errors.New("release statistics egress generation overflow")
	}
	measurement := self.releaseStatsMeasurementWithLock()
	if _, err := VerifyReleaseStatsMeasurement(measurement); err != nil {
		return ReleaseStatsMeasurement{}, err
	}
	cutGeneration := self.egressGeneration
	if err := persist(measurement, cutGeneration); err != nil {
		return ReleaseStatsMeasurement{}, err
	}
	priorEgress := self.egress
	self.egress = map[connect.Id]map[[32]byte]bool{}
	self.egressGeneration++
	if err := self.saveWithLock(dir); err != nil {
		self.egress = priorEgress
		self.egressGeneration = cutGeneration
		return ReleaseStatsMeasurement{}, err
	}
	return measurement, nil
}

// detachReleaseStatsMeasurementWithAttemptCut publishes a signed replay whose
// counters start at the settlement boundary and whose egress claims start at
// the prior native cut. An active trail makes the cut retryable and blocks new
// attempts until the exact attempt has committed or aborted.
func (self *StatsEngine) detachReleaseStatsMeasurementWithAttemptCut(dir string, boundary AttemptBoundary, persist func(ReleaseStatsMeasurement, uint64) error) (ReleaseStatsMeasurement, error) {
	if persist == nil {
		return ReleaseStatsMeasurement{}, errors.New("release statistics persistence callback is nil")
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.attemptLedger == nil {
		return ReleaseStatsMeasurement{}, errors.New("release statistics attempt ledger is absent")
	}
	self.attemptCutPending = true
	if self.activeAttemptCount != 0 {
		return ReleaseStatsMeasurement{}, errAttemptCutPending
	}
	if !self.settlementEpochKnown || boundary.SettlementEpoch != self.settlementEpoch {
		return ReleaseStatsMeasurement{}, errors.New("attempt cut boundary differs from the active settlement epoch")
	}
	if self.egressGeneration == ^uint64(0) {
		return ReleaseStatsMeasurement{}, errors.New("release statistics egress generation overflow")
	}
	measurement := self.releaseStatsMeasurementWithLock()
	cut, err := self.attemptLedger.BuildCut(boundary, self.attemptSettlementFirstSequence, self.attemptEgressFirstSequence)
	if err != nil {
		return ReleaseStatsMeasurement{}, err
	}
	measurement.AttemptCut = cut
	if _, err := VerifyReleaseStatsMeasurement(measurement); err != nil {
		return ReleaseStatsMeasurement{}, err
	}
	cutGeneration := self.egressGeneration
	if err := persist(measurement, cutGeneration); err != nil {
		return ReleaseStatsMeasurement{}, err
	}
	priorEgress := self.egress
	priorEgressFirst := self.attemptEgressFirstSequence
	self.egress = map[connect.Id]map[[32]byte]bool{}
	self.egressGeneration++
	self.attemptEgressFirstSequence = cut.LastSequence + 1
	if err := self.saveWithLock(dir); err != nil {
		self.egress = priorEgress
		self.egressGeneration = cutGeneration
		self.attemptEgressFirstSequence = priorEgressFirst
		return ReleaseStatsMeasurement{}, err
	}
	self.attemptCutPending = false
	return measurement, nil
}

// reconcileReleaseStatsCut completes a journal-first egress cut after a
// process or disk failure. A snapshot already in a later generation is left
// untouched, preserving evidence recorded after the cut.
func (self *StatsEngine) reconcileReleaseStatsCut(dir string, cutGeneration uint64, attemptCuts ...*AttemptLedgerCut) error {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.egressGeneration < cutGeneration {
		return fmt.Errorf("statistics egress generation %d precedes journal generation %d", self.egressGeneration, cutGeneration)
	}
	if self.egressGeneration > cutGeneration {
		if self.attemptLedger != nil && (len(attemptCuts) != 1 || attemptCuts[0] == nil || self.attemptEgressFirstSequence < attemptCuts[0].LastSequence+1) {
			return errors.New("advanced release statistics lack the matching attempt cut cursor")
		}
		self.attemptCutPending = false
		return nil
	}
	if cutGeneration == ^uint64(0) {
		return errors.New("release statistics egress generation overflow")
	}
	priorEgress := self.egress
	priorEgressFirst := self.attemptEgressFirstSequence
	if self.attemptLedger != nil {
		if len(attemptCuts) != 1 || attemptCuts[0] == nil {
			return errors.New("attempt-backed release statistics reconciliation has no signed cut")
		}
		vpk, err := canonicalAttemptHex32("attempt cut validator vpk", attemptCuts[0].Identity.ValidatorVPK, false)
		if err != nil || verifyAttemptLedgerCut(attemptCuts[0], vpk[:], nil, false) != nil {
			return errors.New("attempt-backed release statistics reconciliation cut is invalid")
		}
		self.attemptEgressFirstSequence = attemptCuts[0].LastSequence + 1
	}
	self.egress = map[connect.Id]map[[32]byte]bool{}
	self.egressGeneration++
	if err := self.saveWithLock(dir); err != nil {
		self.egress = priorEgress
		self.egressGeneration = cutGeneration
		self.attemptEgressFirstSequence = priorEgressFirst
		return err
	}
	self.attemptCutPending = false
	return nil
}

// ExactPoolQualityFromReleaseStats returns the exposure-weighted pool quality.
// Bound head providers are excluded from both numerator and denominator.
func ExactPoolQualityFromReleaseStats(stats VerifiedReleaseStats, bound map[connect.Id]bool) (uint32, error) {
	var numerator, denominator uint64
	for clientID, provider := range stats.Providers {
		if bound[clientID] || !provider.HasQuality {
			continue
		}
		weight := provider.Exposure
		if weight == 0 {
			weight = 1
		}
		if provider.QualityPPM != 0 && weight > (^uint64(0)-numerator)/uint64(provider.QualityPPM) {
			return 0, errors.New("pool quality numerator overflows uint64")
		}
		numerator += uint64(provider.QualityPPM) * weight
		if ^uint64(0)-denominator < weight {
			return 0, errors.New("pool quality denominator overflows uint64")
		}
		denominator += weight
	}
	if denominator == 0 {
		return 0, nil
	}
	return uint32(numerator / denominator), nil
}

// PoolQualityFromReleaseStats preserves the legacy diagnostic API. Release
// artifact and runtime paths must use the error-returning exact variant.
func PoolQualityFromReleaseStats(stats VerifiedReleaseStats, bound map[connect.Id]bool) uint32 {
	quality, _ := ExactPoolQualityFromReleaseStats(stats, bound)
	return quality
}
