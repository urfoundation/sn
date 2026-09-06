//go:build linux || darwin

package validator

// Private aggregate rows are fixed-size and versioned independently of public
// measurement formats. Integer quality and legacy reporting EMA remain separate;
// neither is reconstructed from the rounded representation of the other.

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

const (
	statsAggregateSchema        = "urnetwork-private-stats-aggregate-v1"
	statsAggregateProviderBytes = 1 + 8 + 8 + statsLatencyBuckets*8 + 1 + 4 + 1 + 8
	statsAggregateClaimBytes    = 16 + 1 + 32 + 32 + 8 + 1 + 2
)

// Explicit storage ceilings are local resource admission, not protocol caps or
// permission to omit providers. Exhaustion aborts an unpublished generation.
type statsAggregateBounds struct {
	MaxHeaderBytes  uint64
	MaxProviders    uint64
	MaxEgressHashes uint64
	MaxEgressClaims uint64
	MaxStorageBytes uint64
	MaxStorageFiles uint64
}

// Constant-size header publication binds the complete private namespace and
// both clocks. JSON has an explicit byte bound; provider counts never affect
// its size. Initial prior-EMA migration is deliberately not implemented here.
type statsAggregateHead struct {
	Schema                  string
	Identity                AttemptLedgerIdentity
	Activation              AttemptCutV2Activation
	Config                  StatsConfig
	Generation              uint64
	SettlementEpoch         uint64
	SettlementFirstSequence uint64
	SettlementPriorRoot     string
	EgressGeneration        uint64
	EgressFirstSequence     uint64
	LastAppliedSequence     uint64
	LastAppliedRoot         string
	Boundary                AttemptBoundary
	ProviderCount           uint64
	EgressHashCount         uint64
	EgressClaimCount        uint64
}

// Every ever-observed provider retains its identity, including an unscoreable
// sparse row. Window presence and both prior-presence bits retain v1 semantics.
type statsAggregateProvider struct {
	ClientID        connect.Id
	WindowPresent   bool
	Window          ProviderWindow
	HasPriorQuality bool
	PriorQualityPPM uint32
	HasPriorEMA     bool
	PriorEMA        float64
}

// Independent controls request native rotation and a settlement fold explicitly.
// A terminal fold must also rotate native evidence, matching the existing
// attempt-backed settlement coordinator; an ordinary native cut never folds.
type statsAggregateAdvance struct {
	RotateNative      bool
	ToSettlementEpoch uint64
}

// Converts the exact configured integer transform without introducing defaults.
func statsAggregateReleaseConfig(config StatsConfig) ReleaseStatsConfig {
	return ReleaseStatsConfig{AMin: config.AMin, AlphaNumerator: config.AlphaNumerator, AlphaDenominator: config.AlphaDenominator, LatRefMillis: config.LatRefMillis}
}

// Validates both separately persisted transforms and their arithmetic domain.
func statsAggregateConfigValid(config StatsConfig) error {
	exact := statsAggregateReleaseConfig(config)
	if exact.AMin == 0 || exact.AlphaDenominator == 0 || exact.AlphaNumerator > exact.AlphaDenominator || exact.LatRefMillis == 0 || exact.LatRefMillis > ^uint64(0)/1_000_000 || exact.AlphaDenominator > ^uint64(0)/1_000_000 || math.IsNaN(config.Alpha) || math.IsInf(config.Alpha, 0) || config.Alpha < 0 || config.Alpha > 1 || math.IsNaN(config.Z) || math.IsInf(config.Z, 0) || config.Z <= 0 || math.IsNaN(config.LatRefMs) || math.IsInf(config.LatRefMs, 0) || config.LatRefMs <= 0 {
		return errors.New("private aggregate statistics config is invalid")
	}
	return nil
}

// Release admission binds decoded policy before touching a scratch directory.
func statsAggregatePolicyConfig(expected AttemptCutV2Context, policy protocol.Policy, config StatsConfig) error {
	if _, err := attemptCutV2PolicyDepth(expected, policy); err != nil {
		return err
	}
	if err := statsAggregateConfigValid(config); err != nil {
		return err
	}
	if config.AMin != policy.Verify.ReliabilityAMin || config.AlphaNumerator != releasePoolAlphaNumerator || config.AlphaDenominator != releasePoolAlphaDenominator || config.LatRefMillis != releasePoolLatRefMillis {
		return errors.New("private aggregate config differs from release policy")
	}
	return nil
}

// A bounded private schema is accepted only under its canonical domain.
func (self statsAggregateHead) validate(bounds statsAggregateBounds) error {
	if bounds.MaxHeaderBytes == 0 || bounds.MaxProviders == 0 || bounds.MaxEgressHashes == 0 || bounds.MaxEgressClaims == 0 || bounds.MaxStorageBytes <= attemptStoreMetadataReserve || bounds.MaxStorageFiles < 8 {
		return errors.New("private aggregate resource bounds are incomplete")
	}
	if self.Schema != statsAggregateSchema || self.Generation == ^uint64(0) || self.LastAppliedSequence == ^uint64(0) || self.ProviderCount > bounds.MaxProviders || self.EgressHashCount > bounds.MaxEgressHashes || self.EgressClaimCount > bounds.MaxEgressClaims {
		return errors.New("private aggregate header schema, generation or count is invalid")
	}
	if err := statsAggregateConfigValid(self.Config); err != nil {
		return err
	}
	expected := self.context()
	if err := expected.Validate(); err != nil {
		return err
	}
	if self.Boundary.SettlementEpoch > self.SettlementEpoch || self.SettlementEpoch-self.Boundary.SettlementEpoch > 1 || self.SettlementFirstSequence > self.LastAppliedSequence+1 || self.EgressFirstSequence > self.LastAppliedSequence+1 {
		return errors.New("private aggregate cursor exceeds its committed prefix")
	}
	root, err := canonicalAttemptHex32("private aggregate applied root", self.LastAppliedRoot, true)
	if err != nil || (self.LastAppliedSequence == 0) != (root == ([32]byte{})) || self.SettlementFirstSequence == self.LastAppliedSequence+1 && self.SettlementPriorRoot != self.LastAppliedRoot {
		return errors.Join(errors.New("private aggregate applied root differs from its prefix"), err)
	}
	raw, err := json.Marshal(self)
	if err != nil || uint64(len(raw)) > bounds.MaxHeaderBytes {
		return errors.Join(errors.New("private aggregate header exceeds its byte bound"), err)
	}
	return nil
}

// Returns an owned comparable context, never a pointer to mutable store state.
func (self statsAggregateHead) context() AttemptCutV2Context {
	boundary := self.Boundary
	boundary.SettlementEpoch = self.SettlementEpoch
	return AttemptCutV2Context{Identity: self.Identity, Activation: self.Activation, Boundary: boundary, FirstSequence: self.SettlementFirstSequence, EgressFirstSequence: self.EgressFirstSequence, EgressGeneration: self.EgressGeneration, PriorRoot: self.SettlementPriorRoot}
}

// Preview calls the same exact production math and preserves independent
// floating-point reporting history. No history-sized scratch is allocated.
func (self statsAggregateProvider) quality(config StatsConfig) (uint32, bool, float64, bool, error) {
	if err := statsAggregateConfigValid(config); err != nil {
		return 0, false, 0, false, err
	}
	provider := ReleaseProviderMeasurement{Assignments: self.Window.Assignments, Confirmations: self.Window.Confirmations, LatencyBuckets: self.Window.LatencyBuckets[:], HasPriorQuality: self.HasPriorQuality, PriorQualityPPM: self.PriorQualityPPM}
	ppm, hasPPM, err := releaseQualityPPM(statsAggregateReleaseConfig(config), provider)
	if err != nil {
		return 0, false, 0, false, err
	}
	ema, hasEMA := self.PriorEMA, self.HasPriorEMA
	if self.Window.Assignments >= config.AMin {
		mathEngine := StatsEngine{cfg: config}
		raw := mathEngine.qualityRawLocked(&self.Window)
		if hasEMA {
			ema = config.Alpha*raw + (1-config.Alpha)*ema
		} else {
			ema = raw
		}
		hasEMA = true
	}
	return ppm, hasPPM, ema, hasEMA, nil
}

// Rejects corrupt values before a row can participate in a preview or fold.
func (self statsAggregateProvider) validate(config StatsConfig) error {
	if self.ClientID == (connect.Id{}) || !self.WindowPresent && self.Window != (ProviderWindow{}) || self.PriorQualityPPM > 1_000_000 || !self.HasPriorQuality && self.PriorQualityPPM != 0 || !self.HasPriorEMA && math.Float64bits(self.PriorEMA) != 0 || math.IsNaN(self.PriorEMA) || math.IsInf(self.PriorEMA, 0) || self.PriorEMA < 0 || self.PriorEMA > 1 {
		return errors.New("private aggregate provider value is not canonical")
	}
	_, _, _, _, err := self.quality(config)
	return err
}

// Fixed-width fields keep point-update memory independent of provider churn.
func (self statsAggregateProvider) encode() []byte {
	raw := make([]byte, statsAggregateProviderBytes)
	if self.WindowPresent {
		raw[0] = 1
	}
	binary.BigEndian.PutUint64(raw[1:9], self.Window.Assignments)
	binary.BigEndian.PutUint64(raw[9:17], self.Window.Confirmations)
	offset := 17
	for _, count := range self.Window.LatencyBuckets {
		binary.BigEndian.PutUint64(raw[offset:offset+8], count)
		offset += 8
	}
	if self.HasPriorQuality {
		raw[offset] = 1
	}
	binary.BigEndian.PutUint32(raw[offset+1:offset+5], self.PriorQualityPPM)
	if self.HasPriorEMA {
		raw[offset+5] = 1
	}
	binary.BigEndian.PutUint64(raw[offset+6:], math.Float64bits(self.PriorEMA))
	return raw
}

// Decoding neither repairs corrupt bits nor infers one prior from the other.
func decodeStatsAggregateProvider(clientID connect.Id, raw []byte, config StatsConfig) (statsAggregateProvider, error) {
	row := statsAggregateProvider{ClientID: clientID}
	if len(raw) != statsAggregateProviderBytes || raw[0] > 1 {
		return row, errors.New("private aggregate provider row length or flags differ")
	}
	row.WindowPresent = raw[0] == 1
	row.Window.Assignments = binary.BigEndian.Uint64(raw[1:9])
	row.Window.Confirmations = binary.BigEndian.Uint64(raw[9:17])
	offset := 17
	for index := range row.Window.LatencyBuckets {
		row.Window.LatencyBuckets[index] = binary.BigEndian.Uint64(raw[offset : offset+8])
		offset += 8
	}
	if raw[offset] > 1 || raw[offset+5] > 1 {
		return row, errors.New("private aggregate provider prior flags are invalid")
	}
	row.HasPriorQuality = raw[offset] == 1
	row.PriorQualityPPM = binary.BigEndian.Uint32(raw[offset+1 : offset+5])
	row.HasPriorEMA = raw[offset+5] == 1
	row.PriorEMA = math.Float64frombits(binary.BigEndian.Uint64(raw[offset+6:]))
	return row, row.validate(config)
}

// The existing binding validator remains the authority for exact identities.
func encodeStatsAggregateClaim(binding AttemptBinding) ([]byte, error) {
	if err := validateAttemptBinding(binding, binding.ClientID); err != nil {
		return nil, err
	}
	fleet, err := canonicalAttemptHex32("aggregate claim fleet", binding.FleetID, true)
	if err != nil {
		return nil, err
	}
	hotkey, err := canonicalAttemptHex32("aggregate claim hotkey", binding.Hotkey, true)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, statsAggregateClaimBytes)
	copy(raw[:16], binding.ClientID[:])
	if binding.Active {
		raw[16] = 1
	}
	copy(raw[17:49], fleet[:])
	copy(raw[49:81], hotkey[:])
	binary.BigEndian.PutUint64(raw[81:89], binding.Generation)
	if binding.UIDFound {
		raw[89] = 1
	}
	binary.BigEndian.PutUint16(raw[90:92], binding.UID)
	return raw, nil
}

// Every active claim retains sequence and full original binding, not only a
// provider/hash pair that could be attributed to a later fleet generation.
func decodeStatsAggregateClaim(raw []byte) (AttemptBinding, error) {
	var binding AttemptBinding
	if len(raw) != statsAggregateClaimBytes || raw[16] > 1 || raw[89] > 1 {
		return binding, errors.New("private aggregate claim row is invalid")
	}
	copy(binding.ClientID[:], raw[:16])
	binding.Active = raw[16] == 1
	binding.FleetID = attemptHex32(*(*[32]byte)(raw[17:49]))
	binding.Hotkey = attemptHex32(*(*[32]byte)(raw[49:81]))
	binding.Generation = binary.BigEndian.Uint64(raw[81:89])
	binding.UIDFound = raw[89] == 1
	binding.UID = binary.BigEndian.Uint16(raw[90:92])
	return binding, validateAttemptBinding(binding, binding.ClientID)
}
