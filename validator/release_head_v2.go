//go:build linux || darwin

package validator

// Live compact head collection queries the real pinned coordinator binding
// census and then replays every operator's complete signed record/proof stream.
// The caller first owns durable native cuts and independently authenticates
// their activation/history. This draft does not activate startup or submission.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"reflect"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// The owner supplies complete independently admitted cut contexts and fresh
// replay scratch, but no candidate-selected current bindings. These are
// collected below from the already authenticated exact EVM/native decision.
// Returned inputs/observations are owned data, not a cached acceptance token:
// the final artifact and lineage must still undergo complete verification.
func (self *ReleaseSteerer) gatherHeadV2(ctx context.Context, snapshot *ReleaseSnapshot, subnetEpoch, nativeBlock uint64, nativeHash string, hotkeyUIDs map[[32]byte]uint16, inputs []ReleaseMeasurementInput, options ReleaseMeasurementV2Options) (result releaseHeadResult, resultErr error) {
	if ctx == nil {
		return result, errors.New("compact live head context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil { result = releaseHeadResult{} }
	}()
	if err := ctx.Err(); err != nil { return result, err }
	if self == nil || self.cfg == nil || self.chain == nil || self.hotkey == nil || self.headEMA == nil || snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() || hotkeyUIDs == nil {
		return result, errors.New("compact live head owner or pinned snapshot is incomplete")
	}
	if !self.chain.release || self.chain.chainId == nil || !self.chain.chainId.IsUint64() || self.chain.chainId.Uint64() != self.cfg.ChainID || self.chain.contractAddr != common.HexToAddress(self.cfg.Coordinator) {
		return result, errors.New("compact live head chain differs from its configured coordinator")
	}
	if options.MaxOperators == 0 || options.MaxControlBytes == 0 || options.MaxControlBytes > maxReleaseMeasurementArtifactBytes || uint64(len(inputs)) > options.MaxOperators || len(hotkeyUIDs) > 65536 || len(inputs) != len(self.cfg.Operators) || len(inputs) != len(self.contexts) || len(options.Operators) != len(inputs) {
		return result, errors.New("compact live head operator or native census is incomplete")
	}
	if options.Settlement != nil || len(options.Bindings) != 0 || len(options.Pools) != 0 || len(options.DepositAudits) != 0 {
		return result, errors.New("compact live head admission cannot contain preselected chain observations")
	}
	if uint64(len(inputs)) > options.MaxControlBytes/(8+uint64(reflect.TypeFor[ClientKeyFunc]().Size())) {
		return result, errors.New("compact live head operator census exceeds its control bound")
	}
	_, hotkey, err := ownReleaseMeasurementEnvelopeV2Hotkey(self.hotkey)
	if err != nil { return result, err }
	selfUID, found := hotkeyUIDs[hotkey]
	if !found { return result, errors.New("compact live head validator has no current native uid") }
	expected := options.Expected
	if expected.DeploymentID != self.cfg.DeploymentID || expected.ChainID != self.cfg.ChainID || expected.GenesisHash != self.cfg.GenesisHash || expected.Coordinator != self.cfg.Coordinator || expected.SettlementVault != self.cfg.SettlementVault || expected.ValidatorID != self.cfg.ValidatorID || expected.Netuid != self.cfg.Netuid || expected.PolicyHash != self.cfg.PolicyHash || expected.SubnetEpoch != subnetEpoch || expected.NativeSnapshotBlock != nativeBlock || expected.NativeSnapshotHash != nativeHash || expected.EVMSnapshotBlock != snapshot.BlockNumber || expected.EVMSnapshotHash != releaseHex32(snapshot.BlockHash) || expected.SettlementEpoch != snapshot.Epoch.Uint64() || expected.SelfUID != selfUID {
		return result, errors.New("compact live head authority differs from the current release decision")
	}
	policyHash, err := self.cfg.Policy.Hash()
	if err != nil || snapshot.Policy.PolicyHash != policyHash {
		return result, errors.Join(errors.New("compact live head policy differs from the pinned coordinator"), err)
	}
	clientKeys := make(map[uint64]ClientKeyFunc, len(inputs))
	for _, operator := range self.cfg.Operators {
		measurement := self.contexts[operator.NoID]
		if operator.NoID == 0 || clientKeys[operator.NoID] != nil || measurement == nil || measurement.NoID != operator.NoID || measurement.ClientKey == nil {
			return result, errors.New("compact live head isolated operator context is incomplete")
		}
		admitted, exists := options.Operators[operator.NoID]
		if !exists || admitted.Expected.Activation.Hotkey != hotkey || len(admitted.Measurement.CurrentBindingKVs) != 0 {
			return result, errors.New("compact live head operator authority or current binding source differs")
		}
		clientKeys[operator.NoID] = measurement.ClientKey
	}
	// Use the common bounded ownership admission, without pretending that
	// an unfinished head draft is a verified or publishable measurement.
	draft := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchemaV2, DeploymentID: expected.DeploymentID, ChainID: expected.ChainID,
		GenesisHash: expected.GenesisHash, Coordinator: expected.Coordinator, SettlementVault: expected.SettlementVault,
		ValidatorID: expected.ValidatorID, Netuid: expected.Netuid, SubnetEpoch: subnetEpoch,
		NativeSnapshotBlock: nativeBlock, NativeSnapshotHash: nativeHash, EVMSnapshotBlock: snapshot.BlockNumber,
		EVMSnapshotHash: releaseHex32(snapshot.BlockHash), SettlementEpoch: snapshot.Epoch.Uint64(),
		PolicyHash: expected.PolicyHash, Policy: self.cfg.Policy, PreviousArtifactHash: expected.PreviousArtifactHash,
		ControlledNOIDs: self.cfg.ControlledNOIDs, Inputs: inputs, Bindings: []ReleaseBindingMeasurement{},
		HeadEMA: []HeadEMAMeasurement{}, Pools: []ReleasePoolMeasurement{}, DepositAudits: []DepositAudit{}, SelfUID: selfUID,
	}
	if draft.ControlledNOIDs == nil { draft.ControlledNOIDs = []uint64{} }
	draft, options, err = ownReleaseMeasurementV2(ctx, draft, options)
	if err != nil { return result, err }
	if err := admitReleaseHeadV2Controls(ctx, draft, uint64(len(hotkeyUIDs)), options.MaxControlBytes); err != nil { return result, err }
	hotkeyUIDs = maps.Clone(hotkeyUIDs)
	headEMA := self.headEMA
	chain := self.chain
	providerKVs := make(map[uint64]map[connect.Id]bool, len(draft.Inputs))
	for _, input := range draft.Inputs {
		operator := options.Operators[input.NoID]
		if err := input.AttemptCutV2.VerifyHeader(operator.Expected, operator.Bounds); err != nil {
			return result, fmt.Errorf("compact live head no_id %d header: %w", input.NoID, err)
		}
		providers, err := admitReleaseMeasurementV2ProviderCensus(ctx, input, operator)
		if err != nil { return result, err }
		providerKVs[input.NoID] = providers
	}
	// All identities, signatures, bounds and reader owners have been admitted.
	// Resolve every operator's bindings before any record/proof reader runs.
	for _, input := range draft.Inputs {
		providerIDs := make([]connect.Id, 0, len(providerKVs[input.NoID]))
		for clientID := range providerKVs[input.NoID] { providerIDs = append(providerIDs, clientID) }
		sort.Slice(providerIDs, func(i, j int) bool { return providerIDs[i].LessThan(providerIDs[j]) })
		clientIDs := make([][16]byte, len(providerIDs))
		for index, clientID := range providerIDs { clientIDs[index] = [16]byte(clientID) }
		bindings, err := chain.ReleaseBindingsAtHashContext(ctx, draft.EVMSnapshotBlock, common.HexToHash(draft.EVMSnapshotHash), clientIDs, new(big.Int).SetUint64(draft.SettlementEpoch))
		if err != nil { return result, fmt.Errorf("compact live head binding census no_id %d: %w", input.NoID, err) }
		if len(bindings) != len(providerIDs) { return result, errors.New("compact live head binding census length differs") }
		for index, clientID := range providerIDs {
			if err := ctx.Err(); err != nil { return result, err }
			binding := bindings[index]
			observation := ReleaseBindingMeasurement{
				NoID: input.NoID, ClientID: clientID.String(), Active: binding.Active,
				FleetID: releaseHex32(binding.Record.FleetId), Hotkey: releaseHex32(binding.Record.Hotkey),
				ClientKey: releaseHex32(binding.Record.ClientKey), LocalClientKey: releaseHex32([32]byte{}),
				CommitmentHash: releaseHex32(binding.Record.CommitmentHash), Generation: binding.Record.Generation,
				ValidFromEpoch: binding.Record.ValidFromEpoch, ValidToEpoch: binding.Record.ValidToEpoch,
				CleanedAtEpoch: binding.Record.CleanedAtEpoch, RecordUID: binding.Record.Uid, Cleaned: binding.Record.Cleaned,
			}
			if binding.Active {
				clientKey, found, err := clientKeys[input.NoID](clientID)
				if err != nil || !found || clientKey != binding.Record.ClientKey {
					return result, errors.Join(fmt.Errorf("compact live head active client key differs for no_id %d client %s", input.NoID, clientID), err)
				}
				observation.LocalClientKey = releaseHex32(clientKey)
				observation.LiveUID, observation.LiveUIDFound = hotkeyUIDs[binding.Record.Hotkey]
			}
			draft.Bindings = append(draft.Bindings, observation)
		}
	}
	fleets, bound, membersByUID, stale, current, err := releaseMeasurementBindingObservations(draft, providerKVs)
	if err != nil { return result, err }
	for _, input := range draft.Inputs {
		operator := options.Operators[input.NoID]
		operator.Measurement.CurrentBindingKVs = make(map[connect.Id]FleetScoreKey)
		for clientID := range providerKVs[input.NoID] {
			if key, found := current[fmt.Sprintf("%020d:%s", input.NoID, clientID.String())]; found {
				operator.Measurement.CurrentBindingKVs[clientID] = key
			}
		}
		verified, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(ctx, input.Stats, *input.AttemptCutV2, operator.Expected, draft.Policy, operator.Bounds, operator.Measurement)
		if err != nil { return result, fmt.Errorf("compact live head no_id %d complete replay: %w", input.NoID, err) }
		for key, prefixes := range verified.HeadPrefixes {
			if fleets[key] == nil { return result, errors.New("compact live head replay produced an unobserved fleet") }
			for prefix := range prefixes { fleets[key][prefix] = true }
		}
	}
	controlledNO := make(map[uint64]bool, len(draft.ControlledNOIDs))
	for _, noID := range draft.ControlledNOIDs { controlledNO[noID] = true }
	ema, head, err := headEMA.previewForEpochV2(ctx, subnetEpoch, releaseRawHeadScores(fleets), draft.Policy.Steering.HeadScoreEMA, options.MaxHeadEntries, options.MaxControlBytes)
	if err != nil { return result, err }
	return assembleReleaseHead(draft.Policy, ema, head, bound, controlledNO, membersByUID, stale, draft.Inputs, draft.Bindings)
}

// Reserve the draft's owned control values plus the fixed census payloads
// before copying native identities or allocating generated binding rows. This
// is control-payload accounting, not an estimate of Go map bucket/RSS overhead.
// Every generated observation has a 36-byte id and five 66-byte hex strings.
func admitReleaseHeadV2Controls(ctx context.Context, draft *ReleaseMeasurementArtifact, nativeCount, maximum uint64) error {
	if ctx == nil || draft == nil || maximum == 0 || maximum > maxReleaseMeasurementArtifactBytes { return errors.New("compact live head control admission is incomplete") }
	remaining := maximum
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(draft), &remaining); err != nil { return err }
	charge := func(count, width uint64) error {
		if width == 0 || count > remaining/width { return errors.New("compact live head census exceeds its control bound") }
		remaining -= count*width
		return nil
	}
	if err := charge(nativeCount, 32+2); err != nil { return err }
	if err := charge(uint64(len(draft.Inputs)), 8+uint64(reflect.TypeFor[ClientKeyFunc]().Size())); err != nil { return err }
	for _, input := range draft.Inputs {
		if err := ctx.Err(); err != nil { return err }
		// Provider membership and the returned canonical binding observations
		// are additional to the already-owned raw measurement's provider rows.
		width := uint64(reflect.TypeFor[ReleaseBindingMeasurement]().Size())+36+5*66+16+1
		if err := charge(uint64(len(input.Stats.Providers)), width); err != nil { return err }
	}
	return ctx.Err()
}

// Both persistent entries and the same-epoch transcript are admitted under
// one lock before the existing exact preview copies/parses them. The caller's
// limits also bound the union of retained and current fleet identities.
func (self *HeadEMAStore) previewForEpochV2(ctx context.Context, epoch uint64, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational, maxEntries, maxControlBytes uint64) (map[uint16]*big.Rat, []HeadEMAMeasurement, error) {
	if self == nil || ctx == nil || maxEntries == 0 || maxControlBytes == 0 || maxControlBytes > maxReleaseMeasurementArtifactBytes { return nil, nil, errors.New("compact head EMA owner or bounds are invalid") }
	if err := ctx.Err(); err != nil { return nil, nil, err }
	self.mu.Lock()
	defer self.mu.Unlock()
	if uint64(len(raw)) > maxEntries || uint64(len(self.values)) > maxEntries || uint64(len(self.lastFold)) > maxEntries { return nil, nil, errors.New("compact head EMA census exceeds its bound") }
	remaining, entries := maxControlBytes, uint64(len(self.values))
	for key, value := range self.values {
		if uint64(len(key)) > remaining { return nil, nil, errors.New("compact head EMA key exceeds its control bound") }
		remaining -= uint64(len(key))
		if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(&value), &remaining); err != nil { return nil, nil, err }
	}
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(self.lastFold), &remaining); err != nil { return nil, nil, err }
	for key, score := range raw {
		if err := ctx.Err(); err != nil { return nil, nil, err }
		if score == nil { return nil, nil, errors.New("compact head EMA raw score is nil") }
		words := uint64(len(score.Num().Bits()))+uint64(len(score.Denom().Bits()))
		wordBytes := uint64(reflect.TypeFor[big.Word]().Size())
		if words > remaining/wordBytes { return nil, nil, errors.New("compact head EMA raw rational exceeds its control bound") }
		remaining -= words*wordBytes
		if _, found := self.values[key.String()]; !found {
			if entries == maxEntries { return nil, nil, errors.New("compact head EMA current/history union exceeds its bound") }
			entries++
		}
	}
	if err := ctx.Err(); err != nil { return nil, nil, err }
	ema, head, err := self.previewForEpochWithLock(epoch, raw, alpha)
	if err = errors.Join(err, ctx.Err()); err != nil { return nil, nil, err }
	return ema, head, nil
}
