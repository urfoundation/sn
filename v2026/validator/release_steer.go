package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/crv4"
)

type ReleaseMeasurementContext struct {
	NoID      uint64
	Stats     *StatsEngine
	ClientKey ClientKeyFunc
}

type ReleaseSteerer struct {
	cfg       *ReleaseConfig
	chain     *ChainClient
	native    *crv4.Chain
	hotkey    *crv4.Keypair
	contexts  map[uint64]*ReleaseMeasurementContext
	intents   *IntentStore
	headEMA   *HeadEMAStore
	lastFold  uint64
	foldKnown bool
}

func NewReleaseSteerer(cfg *ReleaseConfig, chain *ChainClient, native *crv4.Chain, hotkey *crv4.Keypair, contexts []*ReleaseMeasurementContext) (*ReleaseSteerer, error) {
	if cfg == nil || chain == nil || native == nil || hotkey == nil {
		return nil, errors.New("release steerer requires config, EVM chain, native chain and hotkey")
	}
	if !chain.release {
		return nil, errors.New("release steerer refuses a legacy contract binding")
	}
	byNO := map[uint64]*ReleaseMeasurementContext{}
	for _, measurement := range contexts {
		if measurement == nil || measurement.NoID == 0 || measurement.Stats == nil || measurement.ClientKey == nil {
			return nil, errors.New("release measurement context is incomplete")
		}
		if byNO[measurement.NoID] != nil {
			return nil, fmt.Errorf("duplicate measurement context for no_id %d", measurement.NoID)
		}
		byNO[measurement.NoID] = measurement
	}
	if len(byNO) != len(cfg.Operators) {
		return nil, fmt.Errorf("measurement context count %d, configured operators %d", len(byNO), len(cfg.Operators))
	}
	intents, err := NewIntentStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	headEMA, err := NewHeadEMAStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	return &ReleaseSteerer{cfg: cfg, chain: chain, native: native, hotkey: hotkey, contexts: byNO, intents: intents, headEMA: headEMA}, nil
}

func (s *ReleaseSteerer) validatePinnedChains(snapshot *ReleaseSnapshot, nativeState *crv4.EpochScheduleState, nativeHash types.Hash) error {
	if snapshot == nil || nativeState == nil || nativeHash == (types.Hash{}) {
		return errors.New("finalized snapshot is incomplete")
	}
	if s.chain.ChainId().Uint64() != s.cfg.ChainID {
		return fmt.Errorf("EVM chain id %s, configured %d", s.chain.ChainId(), s.cfg.ChainID)
	}
	if !bytes.Equal(s.native.GenesisHash[:], commonHashBytes(s.cfg.GenesisHash)) {
		return fmt.Errorf("native genesis %s does not match configured %s", s.native.GenesisHash.Hex(), s.cfg.GenesisHash)
	}
	if uint32(s.native.Runtime.SpecVersion) != s.cfg.RuntimeSpec {
		return fmt.Errorf("native runtime spec %d, configured %d", s.native.Runtime.SpecVersion, s.cfg.RuntimeSpec)
	}
	netuid, err := s.chain.ReleaseNetuidAt(snapshot.BlockNumber)
	if err != nil {
		return err
	}
	if netuid != s.cfg.Netuid {
		return fmt.Errorf("coordinator netuid %d, configured %d", netuid, s.cfg.Netuid)
	}
	policyHash, err := s.cfg.Policy.Hash()
	if err != nil {
		return err
	}
	if snapshot.Policy.PolicyHash != policyHash {
		return fmt.Errorf("coordinator policy 0x%x does not match local policy 0x%x", snapshot.Policy.PolicyHash, policyHash)
	}
	if snapshot.Policy.EpochDepositCapRao == nil || snapshot.Policy.CampaignDepositCapRao == nil {
		return errors.New("coordinator policy contains nil deposit caps")
	}
	initialCadence := snapshot.Policy.EpochBlocks == s.cfg.Policy.Settlement.EpochBlocks &&
		snapshot.Policy.RootCommitWindowBlocks == s.cfg.Policy.Settlement.RootCommitWindowBlocks &&
		snapshot.Policy.FinalizeOffsetBlocks == s.cfg.Policy.Settlement.FinalizeOffsetBlocks &&
		snapshot.Policy.CloseGraceBlocks == s.cfg.Policy.Settlement.CloseGraceBlocks
	productionCadence := snapshot.Policy.EpochBlocks == s.cfg.Policy.ProductionCadence.EpochBlocks &&
		snapshot.Policy.RootCommitWindowBlocks == s.cfg.Policy.ProductionCadence.RootCommitWindowBlocks &&
		snapshot.Policy.FinalizeOffsetBlocks == s.cfg.Policy.ProductionCadence.FinalizeOffsetBlocks &&
		snapshot.Policy.CloseGraceBlocks == s.cfg.Policy.ProductionCadence.CloseGraceBlocks &&
		snapshot.Policy.EffectiveEpoch >= s.cfg.Policy.ProductionCadence.AfterAcceleratedEpochs
	if (!initialCadence && !productionCadence) ||
		snapshot.Policy.ClaimTTLEpochs != s.cfg.Policy.Settlement.ClaimTTLEpochs ||
		snapshot.Policy.ClaimGraceEpochs != s.cfg.Policy.Settlement.ClaimGraceEpochs ||
		snapshot.Policy.MaximumBindingValidityEpochs != s.cfg.Policy.Binding.MaximumValidityEpochs ||
		snapshot.Policy.EpochDepositCapRao.Cmp(new(big.Int).SetUint64(s.cfg.Policy.Deposit.EpochCapRaoPerOperator)) != 0 ||
		snapshot.Policy.CampaignDepositCapRao.Cmp(new(big.Int).SetUint64(s.cfg.Policy.Deposit.TotalTestCampaignCapRao)) != 0 {
		return errors.New("coordinator policy fields do not match the canonical local policy")
	}
	lag := snapshot.BlockNumber
	if nativeState.CurrentBlock > lag {
		lag = nativeState.CurrentBlock - lag
	} else {
		lag -= nativeState.CurrentBlock
	}
	if lag > uint64(s.cfg.Policy.Safety.MaximumFinalizedHeadLagBlocks) {
		return fmt.Errorf("EVM/native finalized head lag %d exceeds policy maximum %d", lag, s.cfg.Policy.Safety.MaximumFinalizedHeadLagBlocks)
	}
	return nil
}

func commonHashBytes(value string) []byte {
	h, err := parseHash32("hash", value)
	if err != nil {
		return nil
	}
	return h[:]
}

// Pin the policy cap explicitly because runtime 447's native getter is
// hardcoded to u16::MAX even though legacy storage metadata still exists.
func releaseSubmitOptions(cfg *ReleaseConfig) crv4.SubmitOptions {
	maxWeightLimit := cfg.Policy.Steering.MaxWeightLimitU16
	return crv4.SubmitOptions{VersionKey: cfg.VersionKey, MaxWeightLimit: &maxWeightLimit}
}

func (s *ReleaseSteerer) gatherHead(snapshot *ReleaseSnapshot) ([]ExactWeightInput, map[uint64]map[connect.Id]bool, map[uint16]bool, error) {
	bound := map[uint64]map[connect.Id]bool{}
	controlledHead := map[uint16]bool{}
	controlledNO := map[uint64]bool{}
	for _, noID := range s.cfg.ControlledNOIDs {
		controlledNO[noID] = true
	}
	fleets := map[FleetScoreKey]map[[32]byte]bool{}
	for noID, measurement := range s.contexts {
		bound[noID] = map[connect.Id]bool{}
		egress := measurement.Stats.EgressIpHashes()
		for _, clientID := range measurement.Stats.ProviderIDs() {
			hashes := egress[clientID]
			binding, err := s.chain.ReleaseBindingAt(snapshot.BlockNumber, [16]byte(clientID), snapshot.Epoch)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("bindingAt no_id %d client %s: %w", noID, clientID, err)
			}
			if !binding.Active {
				continue
			}
			clientKey, ok, err := measurement.ClientKey(clientID)
			if err != nil || !ok {
				if err == nil {
					err = errors.New("client key absent")
				}
				return nil, nil, nil, fmt.Errorf("active binding client key no_id %d client %s: %w", noID, clientID, err)
			}
			if clientKey != binding.Record.ClientKey {
				return nil, nil, nil, fmt.Errorf("active binding client key mismatch for no_id %d client %s", noID, clientID)
			}
			uid, found, err := s.chain.FindUidByHotkeyAt(snapshot.BlockNumber, s.cfg.Netuid, binding.Record.Hotkey)
			if err != nil || !found || uid != binding.Record.Uid {
				return nil, nil, nil, fmt.Errorf("stale head UID for client %s (record %d, live %d, found %t): %w", clientID, binding.Record.Uid, uid, found, err)
			}
			if controlledNO[noID] {
				controlledHead[uid] = true
			}
			key := FleetScoreKey{FleetID: binding.Record.FleetId, Hotkey: binding.Record.Hotkey, Generation: binding.Record.Generation, UID: uid}
			if fleets[key] == nil {
				fleets[key] = map[[32]byte]bool{}
			}
			for hash := range hashes {
				fleets[key][hash] = true
			}
			bound[noID][clientID] = true
		}
	}
	claims := map[[32]byte]uint64{}
	for _, hashes := range fleets {
		for hash := range hashes {
			claims[hash]++
		}
	}
	raw := map[FleetScoreKey]*big.Rat{}
	for key, hashes := range fleets {
		score := new(big.Rat)
		for hash := range hashes {
			if claims[hash] != 0 {
				score.Add(score, new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).SetUint64(claims[hash])))
			}
		}
		raw[key] = score
	}
	ema, err := s.headEMA.Fold(raw, s.cfg.Policy.Steering.HeadScoreEMA)
	if err != nil {
		return nil, nil, nil, err
	}
	uids := make([]uint16, 0, len(ema))
	for uid := range ema {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	head := make([]ExactWeightInput, 0, len(uids))
	for _, uid := range uids {
		head = append(head, ExactWeightInput{UID: uid, Score: ema[uid]})
	}
	return head, bound, controlledHead, nil
}

func (s *ReleaseSteerer) gatherPools(snapshot *ReleaseSnapshot, bound map[uint64]map[connect.Id]bool) ([]ExactWeightInput, map[uint16]bool, error) {
	count, err := s.chain.ReleaseOperatorCountAt(snapshot.BlockNumber)
	if err != nil {
		return nil, nil, err
	}
	if !count.IsInt64() || count.Sign() < 0 {
		return nil, nil, errors.New("operator count is out of range")
	}
	controlled := map[uint64]bool{}
	for _, noID := range s.cfg.ControlledNOIDs {
		controlled[noID] = true
	}
	masked := map[uint16]bool{}
	active := 0
	var pools []ExactWeightInput
	for index := int64(0); index < count.Int64(); index++ {
		noIDBig, err := s.chain.ReleaseOperatorIDAt(snapshot.BlockNumber, big.NewInt(index))
		if err != nil || !noIDBig.IsUint64() {
			return nil, nil, fmt.Errorf("operator id at %d: %w", index, err)
		}
		noID := noIDBig.Uint64()
		op, err := s.chain.ReleaseOperatorAt(snapshot.BlockNumber, noIDBig, snapshot.Epoch)
		if err != nil {
			return nil, nil, err
		}
		if !op.Active {
			continue
		}
		active++
		measurement := s.contexts[noID]
		if measurement == nil {
			return nil, nil, fmt.Errorf("active no_id %d has no isolated measurement context", noID)
		}
		uid, found, err := s.chain.FindUidByHotkeyAt(snapshot.BlockNumber, s.cfg.Netuid, op.PoolHotkey)
		if err != nil || !found {
			return nil, nil, fmt.Errorf("active no_id %d pool hotkey has no live UID: %w", noID, err)
		}
		if controlled[noID] {
			masked[uid] = true
			continue
		}
		deposit, err := s.chain.ReleaseEpochDepositAt(snapshot.BlockNumber, snapshot.Epoch, noIDBig)
		if err != nil {
			return nil, nil, err
		}
		conviction, err := s.chain.ReleaseConvictionAt(snapshot.BlockNumber, noIDBig)
		if err != nil {
			return nil, nil, err
		}
		convictionAdded, err := s.chain.ReleaseEpochConvictionAddedAt(snapshot.BlockNumber, snapshot.Epoch, noIDBig)
		if err != nil {
			return nil, nil, err
		}
		convictionBefore := new(big.Int).Sub(new(big.Int).Set(conviction), deposit)
		convictionBefore.Sub(convictionBefore, convictionAdded)
		if convictionBefore.Sign() < 0 {
			return nil, nil, fmt.Errorf("no_id %d conviction snapshot underflow", noID)
		}
		quality := PoolQualityPPM(measurement.Stats, bound[noID])
		score, err := impliedUsageQuality(deposit, convictionBefore, quality, s.cfg.Policy)
		if err != nil {
			return nil, nil, fmt.Errorf("no_id %d weight: %w", noID, err)
		}
		pools = append(pools, ExactWeightInput{UID: uid, Score: score})
	}
	if active < s.cfg.Policy.Safety.MinimumHealthyNOCount {
		return nil, nil, fmt.Errorf("active operator count %d below policy minimum %d", active, s.cfg.Policy.Safety.MinimumHealthyNOCount)
	}
	return pools, masked, nil
}

func (s *ReleaseSteerer) foldSettlementEpoch(epoch uint64) error {
	if !s.foldKnown {
		s.lastFold, s.foldKnown = epoch, true
		return nil
	}
	if epoch <= s.lastFold {
		return nil
	}
	for noID, measurement := range s.contexts {
		measurement.Stats.Fold()
		if err := measurement.Stats.Save(measurementStateDir(s.cfg, noID)); err != nil {
			return err
		}
	}
	s.lastFold = epoch
	return nil
}

func measurementStateDir(cfg *ReleaseConfig, noID uint64) string {
	for _, op := range cfg.Operators {
		if op.NoID == noID {
			return op.StateDir
		}
	}
	return cfg.StateDir
}

func (s *ReleaseSteerer) checkApplication(snapshot *ReleaseSnapshot) error {
	current, err := s.intents.Current()
	if err != nil || current == nil || current.Status != "finalized" {
		return err
	}
	uid, found, err := s.chain.FindUidByHotkeyAt(snapshot.BlockNumber, s.cfg.Netuid, s.hotkey.PublicKey())
	if err != nil || !found {
		return fmt.Errorf("cannot track applied weights without live validator UID: %w", err)
	}
	row, block, hash, err := s.native.WeightsAtFinalized(s.cfg.Netuid, uid)
	if err != nil {
		return err
	}
	if block < current.RevealBlock {
		return nil
	}
	want := map[uint16]uint16{}
	for i, targetUID := range current.UIDs {
		if i < len(current.Values) {
			want[targetUID] = current.Values[i]
		}
	}
	got := map[uint16]uint16{}
	for _, pair := range row {
		got[uint16(pair.UID)] = uint16(pair.Value)
	}
	if len(want) != len(got) {
		return nil
	}
	for targetUID, value := range want {
		if got[targetUID] != value {
			return nil
		}
	}
	return s.intents.MarkApplied(current.VectorHash, block, hash.Hex())
}

// reconcilePending proves whether the exact write-ahead extrinsic finalized,
// or replays those same bytes while its finalized nonce remains usable. It
// never regenerates ciphertext and never allocates a replacement nonce under
// uncertainty.
func (s *ReleaseSteerer) reconcilePending(ctx context.Context, current *SteeringIntent, nativeState *crv4.EpochScheduleState) (bool, error) {
	if current == nil || current.Status != "pending" || current.Prepared == nil {
		return false, errors.New("cannot reconcile a non-pending steering intent")
	}
	if _, err := current.Prepared.Validate(); err != nil {
		return false, fmt.Errorf("validate pending steering submission: %w", err)
	}
	hash, err := types.NewHashFromHexString(current.Prepared.ExtrinsicHash)
	if err != nil {
		return false, fmt.Errorf("pending steering extrinsic hash: %w", err)
	}
	receipt, found, err := s.native.FindFinalizedExtrinsic(ctx, hash, current.Prepared.PreparedAtBlock)
	if err != nil {
		return false, fmt.Errorf("reconcile pending steering finality: %w", err)
	}
	if found {
		if err := s.intents.MarkFinalized(current.VectorHash, receipt.ExtrinsicHash.Hex(), receipt.BlockNumber, receipt.BlockHash.Hex(), current.Prepared.RevealBlock, current.Prepared.Values); err != nil {
			return false, err
		}
		return true, nil
	}
	if current.SubnetEpoch < nativeState.SubnetEpochIndex {
		err := fmt.Errorf("unfinalized steering submission expired at subnet epoch %d", current.SubnetEpoch)
		if markErr := s.intents.MarkFailed(current.VectorHash, err); markErr != nil {
			return false, markErr
		}
		return false, nil
	}
	if current.SubnetEpoch > nativeState.SubnetEpochIndex {
		return false, fmt.Errorf("pending steering epoch %d is ahead of finalized epoch %d", current.SubnetEpoch, nativeState.SubnetEpochIndex)
	}
	finalizedNonce, _, _, err := s.native.FinalizedAccountNonce(s.hotkey.PublicKey())
	if err != nil {
		return false, err
	}
	if finalizedNonce > current.Prepared.AccountNonce {
		err := fmt.Errorf("steering nonce %d was consumed by a different finalized extrinsic", current.Prepared.AccountNonce)
		if markErr := s.intents.MarkFailed(current.VectorHash, err); markErr != nil {
			return false, markErr
		}
		return false, nil
	}
	if finalizedNonce < current.Prepared.AccountNonce {
		return false, fmt.Errorf("steering nonce gap: finalized %d, prepared %d", finalizedNonce, current.Prepared.AccountNonce)
	}
	result, err := crv4.SubmitPrepared(ctx, s.native, current.Prepared)
	if err != nil {
		_ = s.intents.update(current.VectorHash, "pending", func(i *SteeringIntent) error { i.Error = err.Error(); return nil })
		return false, err
	}
	if err := s.intents.MarkFinalized(current.VectorHash, result.TxHash.Hex(), result.FinalizedBlock, result.FinalizedBlockHash.Hex(), result.RevealBlock, result.Values); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ReleaseSteerer) SubmitOnce(ctx context.Context) error {
	nativeState, nativeHash, err := s.native.EpochScheduleStateFinalized(s.cfg.Netuid)
	if err != nil {
		return err
	}
	snapshot, err := s.chain.ReleaseSnapshot()
	if err != nil {
		return err
	}
	if err := s.validatePinnedChains(snapshot, nativeState, nativeHash); err != nil {
		return err
	}
	if err := s.checkApplication(snapshot); err != nil {
		return err
	}
	if current, err := s.intents.Current(); err != nil {
		return err
	} else if current != nil {
		if current.Status == "pending" {
			resolved, err := s.reconcilePending(ctx, current, nativeState)
			if err != nil {
				return err
			}
			if resolved {
				return ErrSteeringAlreadyFinal
			}
			current, err = s.intents.Current()
			if err != nil {
				return err
			}
		}
		if current.SubnetEpoch == nativeState.SubnetEpochIndex {
			if current.Status == "finalized" || current.Status == "applied" {
				return ErrSteeringAlreadyFinal
			}
		}
		if current.SubnetEpoch < nativeState.SubnetEpochIndex && current.Status != "applied" && current.Status != "failed" {
			return fmt.Errorf("prior subnet epoch %d intent is %s; refusing a new commit", current.SubnetEpoch, current.Status)
		}
	}
	if err := s.foldSettlementEpoch(snapshot.Epoch.Uint64()); err != nil {
		return err
	}
	head, bound, controlledHead, err := s.gatherHead(snapshot)
	if err != nil {
		return err
	}
	pools, masked, err := s.gatherPools(snapshot, bound)
	if err != nil {
		return err
	}
	for uid := range controlledHead {
		masked[uid] = true
	}
	selfUID, found, err := s.chain.FindUidByHotkeyAt(snapshot.BlockNumber, s.cfg.Netuid, s.hotkey.PublicKey())
	if err != nil {
		return fmt.Errorf("self-mask: %w", err)
	}
	if !found {
		return fmt.Errorf("self-mask: validator hotkey has no live UID on netuid %d", s.cfg.Netuid)
	}
	masked[selfUID] = true
	maskedUIDs := make([]uint16, 0, len(masked))
	for uid := range masked {
		maskedUIDs = append(maskedUIDs, uid)
	}
	sort.Slice(maskedUIDs, func(i, j int) bool { return maskedUIDs[i] < maskedUIDs[j] })
	uids, scores, err := BuildWeightVectorExact(pools, head, s.cfg.Policy.Steering.Theta, masked)
	if err != nil {
		return err
	}
	encodedScores, err := rationalJSON(scores)
	if err != nil {
		return err
	}
	prepared, err := crv4.PrepareWeightsCRv4Exact(ctx, s.native, s.hotkey, s.cfg.Netuid, uids, scores, releaseSubmitOptions(s.cfg))
	if err != nil {
		return err
	}
	if prepared.SubnetEpoch != nativeState.SubnetEpochIndex {
		return fmt.Errorf("native epoch crossed during steering snapshot: started %d prepared %d", nativeState.SubnetEpochIndex, prepared.SubnetEpoch)
	}
	intent, err := s.intents.Begin(SteeringIntent{
		ValidatorID:         s.cfg.ValidatorID,
		Netuid:              s.cfg.Netuid,
		SubnetEpoch:         nativeState.SubnetEpochIndex,
		NativeSnapshotBlock: nativeState.CurrentBlock,
		NativeSnapshotHash:  nativeHash.Hex(),
		EVMSnapshotBlock:    snapshot.BlockNumber,
		EVMSnapshotHash:     fmt.Sprintf("0x%x", snapshot.BlockHash),
		SettlementEpoch:     snapshot.Epoch.Uint64(),
		PolicyHash:          s.cfg.PolicyHash,
		SelfUID:             selfUID,
		MaskedUIDs:          maskedUIDs,
		UIDs:                uids,
		Scores:              encodedScores,
		Prepared:            prepared,
	})
	if err != nil {
		return err
	}
	result, err := crv4.SubmitPrepared(ctx, s.native, prepared)
	if err != nil {
		// The error can occur after broadcast but before finality was observed.
		// Preserve an uncertain pending state so a restart cannot double-submit.
		_ = s.intents.update(intent.VectorHash, "pending", func(i *SteeringIntent) error { i.Error = err.Error(); return nil })
		return err
	}
	return s.intents.MarkFinalized(intent.VectorHash, result.TxHash.Hex(), result.FinalizedBlock, result.FinalizedBlockHash.Hex(), result.RevealBlock, result.Values)
}

func (s *ReleaseSteerer) Run(ctx context.Context) {
	poll := time.NewTicker(time.Duration(s.cfg.PollSeconds) * time.Second)
	defer poll.Stop()
	var observed uint64
	known := false
	for {
		state, _, err := s.native.EpochScheduleStateFinalized(s.cfg.Netuid)
		if err != nil {
			fmt.Printf("release steer: finalized scheduler: %v\n", err)
		} else {
			shouldSubmit := !known || state.SubnetEpochIndex > observed
			if current, currentErr := s.intents.Current(); currentErr != nil {
				fmt.Printf("release steer: pending intent: %v\n", currentErr)
			} else if current != nil && current.Status == "pending" {
				shouldSubmit = true
			}
			observed, known = state.SubnetEpochIndex, true
			if shouldSubmit {
				if err := s.SubmitOnce(ctx); err != nil && !errors.Is(err, ErrSteeringAlreadyFinal) && !errors.Is(err, ErrSteeringIntentPending) {
					fmt.Printf("release steer: subnet epoch %d: %v\n", observed, err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		}
	}
}
