//go:build linux || darwin

// Activation history is an explicit empty origin or a complete sequence of
// genuine legacy closures. Replay authenticates the prefix and every EMA fold;
// local disk ownership and canonical chain boundaries remain separate checks.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"

	"github.com/urnetwork/connect/v2026"
)

const ReleaseEvidenceV2ActivationHistorySchema = "urnetwork-validator-activation-history-v2"

// Each byte string is one original canonical closure, including its newline.
// An empty, non-null list explicitly names a pristine origin. The list is flat:
// a legacy transition cannot embed another transition or select a new authority.
type ReleaseEvidenceV2ActivationHistory struct {
	Schema         string   `json:"schema"`
	LegacyClosures [][]byte `json:"legacy_closures"`
}

// Bound input before JSON's base64 expansion and reject ambiguous empty values.
// This is wire admission only, never a signature or prior-quality verdict.
func (self ReleaseEvidenceV2ActivationHistory) CanonicalJSON(maxBytes uint64) ([]byte, error) {
	if maxBytes == 0 || maxBytes >= uint64(^uint(0)>>1) || self.Schema != ReleaseEvidenceV2ActivationHistorySchema || self.LegacyClosures == nil || uint64(len(self.LegacyClosures)) > maxBytes {
		return nil, errors.New("activation history schema, list or byte bound is invalid")
	}
	remaining := maxBytes
	for _, closure := range self.LegacyClosures {
		if len(closure) == 0 || uint64(len(closure)) > remaining {
			return nil, errors.New("activation history closure bytes exceed the bound or are empty")
		}
		remaining -= uint64(len(closure))
	}
	encoded, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	if uint64(len(encoded)) >= maxBytes {
		return nil, errors.New("activation history canonical bytes exceed the bound")
	}
	return append(encoded, '\n'), nil
}

// All limits are the configured existing runtime capacities. Expected contexts
// and server keys come from the deployment/history owner, not decoded closures.
type releaseActivationHistoryV2Options struct {
	Expected     []AttemptCutV2Context
	Stats        ReleaseStatsConfig
	ServerKeys   map[uint64]map[byte]ed25519.PublicKey
	MaxBytes     uint64
	MaxOperators uint64
	MaxProviders uint64
	MaxRecords   uint64
	MaxTrails    uint64
}

// The last fully replayed closure is a local result, not a cached authority
// token. No Stats state is created, overwritten or initialized by this reader.
func replayReleaseActivationHistoryV2(ctx context.Context, encoded []byte, options releaseActivationHistoryV2Options) (last *AttemptSettlementClosure, resultErr error) {
	if ctx == nil {
		return nil, errors.New("activation history context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			last = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.MaxBytes == 0 || options.MaxBytes >= uint64(^uint(0)>>1) || options.MaxOperators == 0 || options.MaxProviders == 0 || options.MaxRecords == 0 || options.MaxTrails == 0 || options.MaxTrails > options.MaxRecords || len(options.Expected) == 0 || uint64(len(options.Expected)) > options.MaxOperators || uint64(len(encoded)) > options.MaxBytes {
		return nil, errors.New("activation history authority or capacities are incomplete")
	}
	expected := slices.Clone(options.Expected)
	if _, err := verifyReleaseStatsMeasurementWithCutVerifier(ReleaseStatsMeasurement{Config: options.Stats}, verifyAttemptLedgerCut); err != nil {
		return nil, err
	}
	keys := make(map[uint64]map[byte]ed25519.PublicKey, len(expected))
	for index, item := range expected {
		if err := item.Validate(); err != nil {
			return nil, err
		}
		if item.FirstSequence != item.Activation.FirstSequence || item.EgressFirstSequence != item.FirstSequence || item.PriorRoot != item.Activation.PriorRoot || item.Boundary.SettlementEpoch != item.Activation.Domain.ActivationEpoch || item.EgressGeneration == 0 {
			return nil, errors.New("activation history requires exact initial-cut authority")
		}
		if index > 0 && (item.Identity.NoID <= expected[index-1].Identity.NoID || !equalAttemptSettlementIdentity(item.Identity, expected[0].Identity) || !equalAttemptCutV2CommonDomain(item.Activation.Domain, expected[0].Activation.Domain) || item.Activation.Hotkey != expected[0].Activation.Hotkey || item.Boundary != expected[0].Boundary) {
			return nil, errors.New("activation history mixes operators, validator or activation boundaries")
		}
		operatorKeys := options.ServerKeys[item.Identity.NoID]
		keys[item.Identity.NoID] = make(map[byte]ed25519.PublicKey, len(operatorKeys))
		for id, key := range operatorKeys {
			if len(key) != ed25519.PublicKeySize {
				return nil, errors.New("activation history server key is malformed")
			}
			keys[item.Identity.NoID][id] = slices.Clone(key)
		}
	}
	var history ReleaseEvidenceV2ActivationHistory
	if err := decodeAttemptStreamV2JSON(encoded, options.MaxBytes, &history); err != nil {
		return nil, err
	}
	canonical, err := history.CanonicalJSON(options.MaxBytes)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return nil, errors.Join(errors.New("activation history bytes are not canonical"), err)
	}
	if len(history.LegacyClosures) == 0 {
		for _, item := range expected {
			if item.FirstSequence != 1 || item.PriorRoot != zeroAttemptHash() || item.EgressGeneration != 1 {
				return nil, errors.New("empty activation history cannot replace a retained prefix or generation")
			}
		}
		return nil, nil
	}
	// Lifetime counters and terminal IDs survive window boundaries. They use
	// the same limits as the actual disk importer, not one fresh allowance per cut.
	recordCounts := make(map[uint64]uint64, len(expected))
	trailTerminated := make(map[uint64]map[connect.Id]bool, len(expected))
	for _, item := range expected {
		if len(keys[item.Identity.NoID]) == 0 {
			return nil, errors.New("activation history server-key census is incomplete")
		}
		trailTerminated[item.Identity.NoID] = map[connect.Id]bool{}
	}
	for window, raw := range history.LegacyClosures {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Raw fields are nonrecursive. Refuse embedded transition authority
		// before the compatibility decoder can allocate a nested legacy tree.
		var header struct {
			Schema      string            `json:"schema"`
			Epoch       uint64            `json:"epoch"`
			Transitions []json.RawMessage `json:"transitions"`
		}
		if err := decodeAttemptStreamV2JSON(raw, options.MaxBytes, &header); err != nil {
			return nil, err
		}
		if len(header.Transitions) != len(expected) {
			return nil, errors.New("activation history omits a configured operator")
		}
		for _, transition := range header.Transitions {
			var fields map[string]json.RawMessage
			if err := decodeAttemptStreamV2JSON(transition, options.MaxBytes, &fields); err != nil {
				return nil, err
			}
			var stats struct {
				Config     ReleaseStatsConfig           `json:"config"`
				Providers  []ReleaseProviderMeasurement `json:"providers"`
				AttemptCut *AttemptLedgerCut            `json:"attempt_cut,omitempty"`
			}
			if err := decodeAttemptStreamV2JSON(fields["pre_fold"], options.MaxBytes, &stats); err != nil {
				return nil, err
			}
			if stats.AttemptCut == nil || stats.Config != options.Stats || uint64(len(stats.Providers)) > options.MaxProviders || uint64(len(stats.AttemptCut.Records)) > options.MaxRecords {
				return nil, errors.New("activation history scoring or record census differs")
			}
		}
		current, err := decodeAttemptSettlementClosureWithServerKeysAndVerifier(raw, keys, func(cut *AttemptLedgerCut, vpk ed25519.PublicKey, serverKeys map[byte]ed25519.PublicKey, requireKeys bool) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if cut == nil || cut.LastSequence == ^uint64(0) {
				return errors.New("activation history cut range overflows")
			}
			noID := cut.Identity.NoID
			if uint64(len(cut.Records)) > options.MaxRecords-recordCounts[noID] {
				return errors.New("activation history lifetime record bound exceeded")
			}
			recordCounts[noID] += uint64(len(cut.Records))
			if trailTerminated[noID] == nil {
				return errors.New("activation history cut has an unconfigured operator")
			}
			for _, record := range cut.Records {
				if err := ctx.Err(); err != nil {
					return err
				}
				terminated, exists := trailTerminated[noID][record.TrailID]
				if terminated {
					return errors.New("activation history reuses a terminated trail across windows")
				}
				// Charge pending identities before the legacy verifier builds its
				// lifecycle map. Repeated checkpoints reuse the same owned slot.
				if !exists && uint64(len(trailTerminated[noID])) == options.MaxTrails {
					return errors.New("activation history lifetime trail bound exceeded")
				}
				trailTerminated[noID][record.TrailID] = record.Disposition != AttemptDispositionPending
			}
			if err := verifyAttemptLedgerCut(cut, vpk, serverKeys, requireKeys); err != nil {
				return err
			}
			return ctx.Err()
		})
		if err != nil {
			return nil, err
		}
		if current.Epoch == ^uint64(0) || last != nil && current.Epoch != last.Epoch+1 {
			return nil, errors.New("activation history skips, duplicates or overflows a closed epoch")
		}
		for index, transition := range current.Transitions {
			item := expected[index]
			cut := transition.PreFold.AttemptCut
			if transition.Identity != item.Identity || transition.ToEpoch > item.Activation.Domain.ActivationEpoch || !releaseBlockAtOrBefore(transition.FromBoundary.EVMBlock, transition.FromBoundary.EVMBlockHash, item.Boundary.EVMBlock, item.Boundary.EVMBlockHash) || transition.FromBoundary.EVMBlock == item.Boundary.EVMBlock {
				return nil, errors.New("activation history differs from its independent initial context")
			}
			priorKVs := map[string]uint32{}
			if window == 0 {
				if cut.FirstSequence != 1 || cut.PriorRoot != zeroAttemptHash() {
					return nil, errors.New("activation history is missing its original signed prefix")
				}
			} else {
				prior := last.Transitions[index]
				priorCut := prior.PreFold.AttemptCut
				if cut.FirstSequence != priorCut.LastSequence+1 || cut.PriorRoot != priorCut.Root || transition.FromBoundary.EVMBlock <= prior.FromBoundary.EVMBlock {
					return nil, errors.New("activation history cut lineage is incomplete")
				}
				for _, quality := range prior.PostFold {
					priorKVs[quality.ClientID] = quality.QualityPPM
				}
			}
			seen := 0
			for _, provider := range transition.PreFold.Providers {
				value, exists := priorKVs[provider.ClientID]
				if provider.HasPriorQuality != exists || exists && provider.PriorQualityPPM != value || !exists && provider.PriorQualityPPM != 0 {
					return nil, errors.New("activation history prior EMA is not derived from its complete predecessor")
				}
				if exists {
					seen++
				}
			}
			if seen != len(priorKVs) {
				return nil, errors.New("activation history drops a prior EMA provider")
			}
		}
		last = current
	}
	for index, transition := range last.Transitions {
		item, cut := expected[index], transition.PreFold.AttemptCut
		if transition.ToEpoch != item.Activation.Domain.ActivationEpoch || cut.LastSequence+1 != item.FirstSequence || cut.Root != item.PriorRoot || item.EgressGeneration <= 1 {
			return nil, errors.New("activation history final signed prefix differs from activation")
		}
	}
	return last, nil
}

// Join all independently configured history references into one complete batch.
// Every operator must name the identical full history, even for pristine startup.
// This does not authenticate EVM ancestry, native-cut generations or disk images.
func replayReleaseEvidenceV2ActivationHistories(ctx context.Context, cfg *ReleaseConfig, inputs []releaseEvidenceV2ActivationInput, serverKeys map[uint64]map[byte]ed25519.PublicKey) (*AttemptSettlementClosure, error) {
	if ctx == nil || cfg == nil {
		return nil, errors.New("activation history bootstrap is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(inputs) != len(cfg.EvidenceV2.Operators) || len(inputs) == 0 {
		return nil, errors.New("activation history bootstrap census differs")
	}
	expected := make([]AttemptCutV2Context, len(inputs))
	for index, input := range inputs {
		if input.Config != cfg.EvidenceV2.Operators[index] || index > 0 && !bytes.Equal(input.HistoryBytes, inputs[0].HistoryBytes) {
			return nil, errors.New("activation history configured members disagree on the complete batch")
		}
		contextBytes, err := input.Context.CanonicalJSON(cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
		if err != nil {
			return nil, err
		}
		// A prior loader result is owned data, not a transferable authority
		// token. Rebind every consumed byte to the unchanged configured pins.
		if uint64(len(contextBytes)) != input.Config.Context.Bytes || attemptHex32(sha256.Sum256(contextBytes)) != input.Config.Context.SHA256 {
			return nil, errors.New("activation history context bytes differ from the configured reference")
		}
		if uint64(len(input.HistoryBytes)) != input.Config.History.Bytes || attemptHex32(sha256.Sum256(input.HistoryBytes)) != input.Config.History.SHA256 {
			return nil, errors.New("activation history bytes differ from the configured reference")
		}
		if err := validateReleaseMeasurementInputV2Context(cfg, input.Config.NoID, input.Context.InitialCut); err != nil {
			return nil, err
		}
		expected[index] = input.Context.InitialCut
	}
	bounds := cfg.EvidenceV2.Bounds
	return replayReleaseActivationHistoryV2(ctx, inputs[0].HistoryBytes, releaseActivationHistoryV2Options{
		Expected: expected, Stats: ReleaseStatsConfig{AMin: cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: releasePoolAlphaNumerator, AlphaDenominator: releasePoolAlphaDenominator, LatRefMillis: releasePoolLatRefMillis},
		ServerKeys: serverKeys, MaxBytes: bounds.MaxHistoryBytes, MaxOperators: bounds.MaxOperators, MaxProviders: bounds.MaxProviders, MaxRecords: bounds.Disk.MaxRecordCount, MaxTrails: bounds.Disk.MaxTrailCount,
	})
}

// A startup caller must compare the actual retained transition bytes, not paste
// this reader's post-fold values into a new Stats engine or skip local replay.
func matchReleaseActivationHistoryV2Transition(expected, actual *AttemptSettlementTransition) error {
	want, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	got, err := json.Marshal(actual)
	if err != nil {
		return err
	}
	if !bytes.Equal(want, got) {
		return errors.New("activation history differs from retained statistics transition")
	}
	return nil
}
