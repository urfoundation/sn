package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/stabi"
)

type ReleaseMeasurementContext struct {
	NoID      uint64
	Stats     *StatsEngine
	ClientKey ClientKeyFunc
	Artifacts ArtifactReader
}

type ReleaseSteerer struct {
	cfg       *ReleaseConfig
	chain     *ChainClient
	native    *crv4.Chain
	hotkey    *crv4.Keypair
	contexts  map[uint64]*ReleaseMeasurementContext
	operators map[uint64]OperatorConfig
	intents   *IntentStore
	headEMA   *HeadEMAStore

	// A native-tempo egress window is detached exactly once and then reused for
	// retries in that epoch. Without this cache, a transient EVM read failure
	// after rotation could make the retry score a new, nearly empty window.
	headWindowEpoch uint64
	headWindowKnown bool
	headInputsByNO  map[uint64]ReleaseMeasurementInput
}

func releaseAttemptClaimMatchesBinding(claim AttemptEgressClaim, binding stabi.STCoordinatorBindingRecord, uid uint16) bool {
	return claim.Binding.Active && claim.Binding.UIDFound &&
		claim.Binding.FleetID == releaseHex32(binding.FleetId) &&
		claim.Binding.Hotkey == releaseHex32(binding.Hotkey) &&
		claim.Binding.Generation == binding.Generation && claim.Binding.UID == uid
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
		if measurement == nil || measurement.NoID == 0 || measurement.Stats == nil || measurement.ClientKey == nil || measurement.Artifacts == nil {
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
	operators := make(map[uint64]OperatorConfig, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		operators[operator.NoID] = operator
	}
	intents, err := NewIntentStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	headEMA, err := NewHeadEMAStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	return &ReleaseSteerer{cfg: cfg, chain: chain, native: native, hotkey: hotkey, contexts: byNO, operators: operators, intents: intents, headEMA: headEMA}, nil
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
	if s.native.Runtime == nil ||
		uint32(s.native.Runtime.SpecVersion) != s.cfg.RuntimeSpec ||
		uint32(s.native.Runtime.TransactionVersion) != s.cfg.TransactionVersion {
		return errors.New("native signing runtime is not bound to the configured spec and transaction versions")
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

// Pin the policy cap explicitly because runtime 453 retains the u16::MAX
// native getter even though legacy storage metadata still exists.
func releaseSubmitOptions(cfg *ReleaseConfig) crv4.SubmitOptions {
	maxWeightLimit := cfg.Policy.Steering.MaxWeightLimitU16
	return crv4.SubmitOptions{VersionKey: cfg.VersionKey, MaxWeightLimit: &maxWeightLimit}
}

type releaseHeadMember struct {
	NoID     uint64
	ClientID connect.Id
}

type releaseHeadResult struct {
	Weights       []ExactWeightInput
	Eligible      []ExactWeightInput
	Bound         map[uint64]map[connect.Id]bool
	Controlled    map[uint16]bool
	EligibleUIDs  []uint16
	SelectedUIDs  []uint16
	RejectedUIDs  []uint16
	StaleBindings []StaleHeadBinding
	Inputs        []ReleaseMeasurementInput
	Bindings      []ReleaseBindingMeasurement
	HeadEMA       []HeadEMAMeasurement
}

// Every live, valid fleet binding is promoted out of its NO's pool accounting
// for as long as that fleet owns a UID. Selection decides native head weight;
// it does not put an unselected-but-still-registered fleet back into the pool.
// Pool fallback begins only after deregistration makes the binding stale.
func excludeLiveHeadMembers(bound map[uint64]map[connect.Id]bool, controlledNO map[uint64]bool, membersByUID map[uint16][]releaseHeadMember) map[uint16]bool {
	controlledHead := map[uint16]bool{}
	for uid, members := range membersByUID {
		for _, member := range members {
			if bound[member.NoID] == nil {
				bound[member.NoID] = map[connect.Id]bool{}
			}
			bound[member.NoID][member.ClientID] = true
			if controlledNO[member.NoID] {
				controlledHead[uid] = true
			}
		}
	}
	return controlledHead
}

func (s *ReleaseSteerer) takeHeadEvidence(subnetEpoch, nativeBlock uint64, nativeHash string, snapshot *ReleaseSnapshot) (map[uint64]ReleaseMeasurementInput, error) {
	if snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() {
		return nil, errors.New("head evidence settlement snapshot is invalid")
	}
	if s.headWindowKnown {
		if subnetEpoch < s.headWindowEpoch {
			return nil, fmt.Errorf("native head window epoch regressed from %d to %d", s.headWindowEpoch, subnetEpoch)
		}
		if subnetEpoch == s.headWindowEpoch {
			for noID, input := range s.headInputsByNO {
				if input.SettlementEpoch != snapshot.Epoch.Uint64() {
					return nil, fmt.Errorf("native head window no_id %d crossed settlement epoch %d to %d", noID, input.SettlementEpoch, snapshot.Epoch.Uint64())
				}
			}
			return s.headInputsByNO, nil
		}
	}
	noIDs := make([]uint64, 0, len(s.contexts))
	for noID := range s.contexts {
		noIDs = append(noIDs, noID)
	}
	sort.Slice(noIDs, func(i, j int) bool { return noIDs[i] < noIDs[j] })
	inputsByNO := make(map[uint64]ReleaseMeasurementInput, len(noIDs))
	for _, noID := range noIDs {
		input, err := s.loadOrDetachReleaseMeasurementInput(noID, subnetEpoch, nativeBlock, nativeHash, snapshot)
		if err != nil {
			return nil, err
		}
		inputsByNO[noID] = input
	}
	s.headWindowEpoch, s.headWindowKnown = subnetEpoch, true
	s.headInputsByNO = inputsByNO
	return inputsByNO, nil
}

func (s *ReleaseSteerer) gatherHead(snapshot *ReleaseSnapshot, subnetEpoch, nativeBlock uint64, nativeHash string) (releaseHeadResult, error) {
	bound := map[uint64]map[connect.Id]bool{}
	controlledNO := map[uint64]bool{}
	for _, noID := range s.cfg.ControlledNOIDs {
		controlledNO[noID] = true
	}
	fleets := map[FleetScoreKey]map[[32]byte]bool{}
	membersByUID := map[uint16][]releaseHeadMember{}
	var staleBindings []StaleHeadBinding
	// Rotate every operator's head window before doing any remote lookup. This
	// gives one native decision a coherent cut across NOs; a slow binding read
	// cannot move only the later operators into the following tempo.
	inputsByNO, err := s.takeHeadEvidence(subnetEpoch, nativeBlock, nativeHash, snapshot)
	if err != nil {
		return releaseHeadResult{}, err
	}
	noIDs := make([]uint64, 0, len(s.contexts))
	for noID := range s.contexts {
		noIDs = append(noIDs, noID)
	}
	sort.Slice(noIDs, func(i, j int) bool { return noIDs[i] < noIDs[j] })
	inputs := make([]ReleaseMeasurementInput, 0, len(noIDs))
	bindings := make([]ReleaseBindingMeasurement, 0)
	for _, noID := range noIDs {
		measurement := s.contexts[noID]
		bound[noID] = map[connect.Id]bool{}
		input := inputsByNO[noID]
		inputs = append(inputs, input)
		verifiedStats, verifyErr := VerifyReleaseStatsMeasurement(input.Stats)
		if verifyErr != nil {
			return releaseHeadResult{}, fmt.Errorf("no_id %d measurement input: %w", noID, verifyErr)
		}
		if input.Stats.AttemptCut == nil {
			return releaseHeadResult{}, fmt.Errorf("no_id %d measurement input has no signed attempt cut", noID)
		}
		egressClaims, err := AttemptCutEgressClaims(input.Stats.AttemptCut)
		if err != nil {
			return releaseHeadResult{}, fmt.Errorf("no_id %d attempt egress claims: %w", noID, err)
		}
		claimsByClient := map[connect.Id][]AttemptEgressClaim{}
		for _, claim := range egressClaims {
			claimsByClient[claim.Binding.ClientID] = append(claimsByClient[claim.Binding.ClientID], claim)
		}
		providerIDs := make([]connect.Id, 0, len(verifiedStats.Providers))
		for clientID := range verifiedStats.Providers {
			providerIDs = append(providerIDs, clientID)
		}
		sort.Slice(providerIDs, func(i, j int) bool { return providerIDs[i].LessThan(providerIDs[j]) })
		for _, clientID := range providerIDs {
			binding, err := s.chain.ReleaseBindingAt(snapshot.BlockNumber, [16]byte(clientID), snapshot.Epoch)
			if err != nil {
				return releaseHeadResult{}, fmt.Errorf("bindingAt no_id %d client %s: %w", noID, clientID, err)
			}
			observation := ReleaseBindingMeasurement{
				NoID: noID, ClientID: clientID.String(), Active: binding.Active,
				FleetID: releaseHex32(binding.Record.FleetId), Hotkey: releaseHex32(binding.Record.Hotkey),
				ClientKey: releaseHex32(binding.Record.ClientKey), LocalClientKey: releaseHex32([32]byte{}),
				CommitmentHash: releaseHex32(binding.Record.CommitmentHash), Generation: binding.Record.Generation,
				ValidFromEpoch: binding.Record.ValidFromEpoch, ValidToEpoch: binding.Record.ValidToEpoch,
				CleanedAtEpoch: binding.Record.CleanedAtEpoch, RecordUID: binding.Record.Uid, Cleaned: binding.Record.Cleaned,
			}
			if !binding.Active {
				bindings = append(bindings, observation)
				continue
			}
			clientKey, ok, err := measurement.ClientKey(clientID)
			if err != nil || !ok {
				if err == nil {
					err = errors.New("client key absent")
				}
				return releaseHeadResult{}, fmt.Errorf("active binding client key no_id %d client %s: %w", noID, clientID, err)
			}
			if clientKey != binding.Record.ClientKey {
				return releaseHeadResult{}, fmt.Errorf("active binding client key mismatch for no_id %d client %s", noID, clientID)
			}
			observation.LocalClientKey = releaseHex32(clientKey)
			uid, found, err := s.chain.FindUidByHotkeyAt(snapshot.BlockNumber, s.cfg.Netuid, binding.Record.Hotkey)
			if err != nil {
				return releaseHeadResult{}, fmt.Errorf("live head UID for client %s: %w", clientID, err)
			}
			observation.LiveUIDFound = found
			observation.LiveUID = uid
			bindings = append(bindings, observation)
			if !found || uid != binding.Record.Uid {
				staleBindings = append(staleBindings, StaleHeadBinding{NoID: noID, ClientID: clientID.String(), RecordUID: binding.Record.Uid, LiveUID: uid, Found: found})
				continue
			}
			key := FleetScoreKey{FleetID: binding.Record.FleetId, Hotkey: binding.Record.Hotkey, Generation: binding.Record.Generation, UID: uid}
			if fleets[key] == nil {
				fleets[key] = map[[32]byte]bool{}
			}
			for _, claim := range claimsByClient[clientID] {
				if !releaseAttemptClaimMatchesBinding(claim, binding.Record, uid) {
					continue
				}
				hash, err := parseReleaseHex32("attempt egress hash", claim.EgressIPHash, false)
				if err != nil {
					return releaseHeadResult{}, fmt.Errorf("no_id %d client %s: %w", noID, clientID, err)
				}
				fleets[key][hash] = true
			}
			membersByUID[uid] = append(membersByUID[uid], releaseHeadMember{NoID: noID, ClientID: clientID})
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
	ema, headEMA, err := s.headEMA.PreviewForEpoch(subnetEpoch, raw, s.cfg.Policy.Steering.HeadScoreEMA)
	if err != nil {
		return releaseHeadResult{}, err
	}
	uids := make([]uint16, 0, len(ema))
	for uid := range ema {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	eligible := make([]ExactWeightInput, 0, len(uids))
	for _, uid := range uids {
		eligible = append(eligible, ExactWeightInput{UID: uid, Score: ema[uid]})
	}
	selection, err := selectHeadFleets(eligible, s.cfg.Policy.Steering.MaximumHeadFleets)
	if err != nil {
		return releaseHeadResult{}, err
	}
	controlledHead := excludeLiveHeadMembers(bound, controlledNO, membersByUID)
	sort.Slice(staleBindings, func(i, j int) bool {
		if staleBindings[i].NoID != staleBindings[j].NoID {
			return staleBindings[i].NoID < staleBindings[j].NoID
		}
		return staleBindings[i].ClientID < staleBindings[j].ClientID
	})
	return releaseHeadResult{
		Weights:       selection.Selected,
		Eligible:      append(append([]ExactWeightInput(nil), selection.Selected...), selection.Rejected...),
		Bound:         bound,
		Controlled:    controlledHead,
		EligibleUIDs:  append(headSelectionUIDs(selection.Selected), headSelectionUIDs(selection.Rejected)...),
		SelectedUIDs:  headSelectionUIDs(selection.Selected),
		RejectedUIDs:  headSelectionUIDs(selection.Rejected),
		StaleBindings: staleBindings,
		Inputs:        inputs,
		Bindings:      bindings,
		HeadEMA:       headEMA,
	}, nil
}

func (s *ReleaseSteerer) gatherPools(ctx context.Context, snapshot *ReleaseSnapshot, bound map[uint64]map[connect.Id]bool, poolObservations *[]ReleasePoolMeasurement) ([]ExactWeightInput, map[uint16]bool, []DepositAudit, error) {
	if poolObservations == nil {
		return nil, nil, nil, errors.New("pool observation destination is nil")
	}
	count, err := s.chain.ReleaseOperatorCountAt(snapshot.BlockNumber)
	if err != nil {
		return nil, nil, nil, err
	}
	if !count.IsInt64() || count.Sign() < 0 {
		return nil, nil, nil, errors.New("operator count is out of range")
	}
	if snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() {
		return nil, nil, nil, errors.New("settlement epoch is out of range")
	}
	currentEpoch := snapshot.Epoch.Uint64()
	currentStartBlock, err := s.chain.ReleaseEpochStartBlockAt(snapshot.BlockNumber, snapshot.Epoch)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("current epoch start block: %w", err)
	}
	if snapshot.Policy.RootCommitWindowBlocks > ^uint64(0)-currentStartBlock {
		return nil, nil, nil, errors.New("artifact deadline overflows uint64")
	}
	artifactDeadline := currentStartBlock + snapshot.Policy.RootCommitWindowBlocks
	controlled := map[uint64]bool{}
	for _, noID := range s.cfg.ControlledNOIDs {
		controlled[noID] = true
	}
	masked := map[uint16]bool{}
	active := 0
	var pools []ExactWeightInput
	var audits []DepositAudit
	for index := int64(0); index < count.Int64(); index++ {
		noIDBig, err := s.chain.ReleaseOperatorIDAt(snapshot.BlockNumber, big.NewInt(index))
		if err != nil || !noIDBig.IsUint64() {
			return nil, nil, nil, fmt.Errorf("operator id at %d: %w", index, err)
		}
		noID := noIDBig.Uint64()
		op, err := s.chain.ReleaseOperatorAt(snapshot.BlockNumber, noIDBig, snapshot.Epoch)
		if err != nil {
			return nil, nil, nil, err
		}
		if !op.Active {
			continue
		}
		active++
		measurement := s.contexts[noID]
		if measurement == nil {
			return nil, nil, nil, fmt.Errorf("active no_id %d has no isolated measurement context", noID)
		}
		uid, found, err := s.chain.FindUidByHotkeyAt(snapshot.BlockNumber, s.cfg.Netuid, op.PoolHotkey)
		if err != nil || !found {
			return nil, nil, nil, fmt.Errorf("active no_id %d pool hotkey has no live UID: %w", noID, err)
		}
		*poolObservations = append(*poolObservations, ReleasePoolMeasurement{NoID: noID, UID: uid, PoolHotkey: releaseHex32(op.PoolHotkey)})
		deposit, err := s.chain.ReleaseEpochDepositAt(snapshot.BlockNumber, snapshot.Epoch, noIDBig)
		if err != nil {
			return nil, nil, nil, err
		}
		conviction, err := s.chain.ReleaseConvictionAt(snapshot.BlockNumber, noIDBig)
		if err != nil {
			return nil, nil, nil, err
		}
		convictionAdded, err := s.chain.ReleaseEpochConvictionAddedAt(snapshot.BlockNumber, snapshot.Epoch, noIDBig)
		if err != nil {
			return nil, nil, nil, err
		}
		convictionBefore := new(big.Int).Sub(new(big.Int).Set(conviction), deposit)
		convictionBefore.Sub(convictionBefore, convictionAdded)
		if convictionBefore.Sign() < 0 {
			return nil, nil, nil, fmt.Errorf("no_id %d conviction snapshot underflow", noID)
		}

		var audit DepositAudit
		if currentEpoch < s.cfg.Policy.Deposit.UsageLagEpochs {
			audit = baseDepositAudit(currentEpoch, 0, noID, deposit, convictionBefore)
			audit.Status = DepositAuditBootstrap
			audit.Disposition = "zero_pool_weight_bootstrap"
			if deposit.Sign() == 0 {
				audit.Compliant = true
			} else {
				audit.Status = DepositAuditMismatch
				audit.Disposition = "zero_pool_weight"
				audit.Error = "bootstrap epoch deposit must be zero without a prior usage artifact"
			}
		} else {
			sourceEpoch := currentEpoch - s.cfg.Policy.Deposit.UsageLagEpochs
			sourceEpochBig := new(big.Int).SetUint64(sourceEpoch)
			startBlock, startErr := s.chain.ReleaseEpochStartBlockAt(snapshot.BlockNumber, sourceEpochBig)
			if startErr != nil {
				return nil, nil, nil, fmt.Errorf("no_id %d source epoch start: %w", noID, startErr)
			}
			endBlock, endErr := s.chain.ReleaseEpochEndBlockAt(snapshot.BlockNumber, sourceEpochBig)
			if endErr != nil {
				return nil, nil, nil, fmt.Errorf("no_id %d source epoch end: %w", noID, endErr)
			}
			if startBlock == 0 || endBlock <= startBlock || endBlock != currentStartBlock || endBlock > snapshot.BlockNumber {
				return nil, nil, nil, fmt.Errorf("source epoch %d has inconsistent finalized boundaries [%d,%d], current start %d, snapshot %d", sourceEpoch, startBlock, endBlock, currentStartBlock, snapshot.BlockNumber)
			}
			startHash, hashErr := s.chain.BlockHash(startBlock)
			if hashErr != nil {
				return nil, nil, nil, fmt.Errorf("source epoch %d start hash: %w", sourceEpoch, hashErr)
			}
			endHash, hashErr := s.chain.BlockHash(endBlock)
			if hashErr != nil {
				return nil, nil, nil, fmt.Errorf("source epoch %d end hash: %w", sourceEpoch, hashErr)
			}
			commitment, commitmentErr := s.chain.ReleaseRootCommitmentAt(snapshot.BlockNumber, sourceEpochBig, noIDBig)
			if commitmentErr != nil {
				return nil, nil, nil, fmt.Errorf("no_id %d source root commitment: %w", noID, commitmentErr)
			}
			if commitment.CommitBlock == 0 {
				status := DepositAuditUnavailablePending
				if snapshot.BlockNumber > artifactDeadline {
					status = DepositAuditUnavailable
				}
				audit = FailedDepositAudit(currentEpoch, sourceEpoch, noID, deposit, convictionBefore, status, errors.New("source payout root is not committed on chain"))
			} else {
				artifact, artifactErr := measurement.Artifacts.Read(ctx, sourceEpoch, noID)
				if artifactErr != nil {
					status := DepositAuditInvalid
					if errors.Is(artifactErr, ErrArtifactEquivocation) {
						status = DepositAuditEquivocation
					} else if errors.Is(artifactErr, ErrArtifactUnavailable) {
						status = DepositAuditUnavailablePending
						if snapshot.BlockNumber > artifactDeadline {
							status = DepositAuditUnavailable
						}
					}
					audit = FailedDepositAudit(currentEpoch, sourceEpoch, noID, deposit, convictionBefore, status, artifactErr)
				} else {
					sourceOperator, sourceOperatorErr := s.chain.ReleaseOperatorAt(snapshot.BlockNumber, noIDBig, sourceEpochBig)
					if sourceOperatorErr != nil {
						return nil, nil, nil, fmt.Errorf("no_id %d source operator version: %w", noID, sourceOperatorErr)
					}
					operatorCfg, ok := s.operators[noID]
					if !ok {
						return nil, nil, nil, fmt.Errorf("no_id %d artifact signer is not configured", noID)
					}
					audit = EvaluateDepositArtifact(artifact, DepositArtifactExpectation{
						DeploymentID: s.cfg.DeploymentID, ChainID: s.cfg.ChainID, GenesisHash: s.cfg.GenesisHash,
						Netuid: s.cfg.Netuid, Coordinator: common.HexToAddress(s.cfg.Coordinator), SettlementVault: common.HexToAddress(s.cfg.SettlementVault), PolicyHash: s.cfg.PolicyHash,
						Epoch: sourceEpoch, NoID: noID, Signer: common.HexToAddress(operatorCfg.ArtifactSigner),
						Start:      payoutartifact.Boundary{Number: startBlock, Hash: fmt.Sprintf("0x%x", startHash)},
						End:        payoutartifact.Boundary{Number: endBlock, Hash: fmt.Sprintf("0x%x", endHash)},
						PayoutRoot: commitment.PayoutRoot, ArtifactHash: commitment.ArtifactHash,
						Committer: commitment.Committer, RootSigner: sourceOperator.RootSigner, CommitBlock: commitment.CommitBlock,
					}, currentEpoch, deposit, convictionBefore, s.cfg.Policy.Deposit)
				}
			}
		}
		audit.ObservedAtBlock = snapshot.BlockNumber
		audit.ArtifactDeadlineBlock = artifactDeadline
		audits = append(audits, audit)
		if controlled[noID] {
			masked[uid] = true
			continue
		}
		if !audit.Compliant || audit.Status == DepositAuditBootstrap {
			continue
		}
		input, inputOK := s.headInputsByNO[noID]
		if !inputOK {
			return nil, nil, nil, fmt.Errorf("no_id %d has no atomic statistics input", noID)
		}
		verifiedStats, verifyErr := VerifyReleaseStatsMeasurement(input.Stats)
		if verifyErr != nil {
			return nil, nil, nil, fmt.Errorf("no_id %d statistics input: %w", noID, verifyErr)
		}
		quality, qualityErr := ExactPoolQualityFromReleaseStats(verifiedStats, bound[noID])
		if qualityErr != nil {
			return nil, nil, nil, fmt.Errorf("no_id %d pool quality: %w", noID, qualityErr)
		}
		score, err := impliedUsageQuality(deposit, convictionBefore, quality, s.cfg.Policy)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("no_id %d weight: %w", noID, err)
		}
		pools = append(pools, ExactWeightInput{UID: uid, Score: score})
	}
	if active < s.cfg.Policy.Safety.MinimumHealthyNOCount {
		return nil, nil, nil, fmt.Errorf("active operator count %d below policy minimum %d", active, s.cfg.Policy.Safety.MinimumHealthyNOCount)
	}
	sort.Slice(audits, func(i, j int) bool { return audits[i].NoID < audits[j].NoID })
	sort.Slice(*poolObservations, func(i, j int) bool { return (*poolObservations)[i].NoID < (*poolObservations)[j].NoID })
	return pools, masked, audits, nil
}

func (s *ReleaseSteerer) foldSettlementEpoch(snapshot *ReleaseSnapshot) error {
	if snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() {
		return errors.New("settlement snapshot is invalid")
	}
	epoch := snapshot.Epoch.Uint64()
	noIDs := make([]uint64, 0, len(s.contexts))
	for noID := range s.contexts {
		noIDs = append(noIDs, noID)
	}
	sort.Slice(noIDs, func(i, j int) bool { return noIDs[i] < noIDs[j] })
	participants := make([]AttemptSettlementParticipant, 0, len(noIDs))
	var terminalBoundary AttemptBoundary
	for _, noID := range noIDs {
		stats := s.contexts[noID].Stats
		if stats == nil {
			return fmt.Errorf("no_id %d has no settlement statistics", noID)
		}
		participants = append(participants, AttemptSettlementParticipant{NoID: noID, StateDir: measurementStateDir(s.cfg, noID), Stats: stats})
		if terminalBoundary == (AttemptBoundary{}) && stats.requiresSettlementAdvance(epoch) {
			boundary, err := releasePriorSettlementBoundary(s.chain, snapshot)
			if err != nil {
				return err
			}
			terminalBoundary = boundary
		}
	}
	return AdvanceAttemptSettlementEpoch(s.cfg.StateDir, epoch, terminalBoundary, participants)
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
	hash, err := authenticatePinnedNativeRuntime(s.native, s.cfg)
	if err != nil {
		return err
	}
	header, err := s.native.API.RPC.Chain.GetHeader(hash)
	if err != nil {
		return fmt.Errorf("read applied-weight finalized header at %s: %w", hash.Hex(), err)
	}
	if header == nil {
		return fmt.Errorf("applied-weight finalized header at %s is unavailable", hash.Hex())
	}
	row, err := s.native.WeightsAt(s.cfg.Netuid, uid, hash)
	if err != nil {
		return err
	}
	block := uint64(header.Number)
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

// restoreHeadEMA completes the second half of the intent/EMA write protocol.
// The intent is the durable authority: if a crash lands after Begin but before
// the EMA state write, its verified artifact deterministically replays the
// missing commit before any pending submission or later epoch is processed.
func (s *ReleaseSteerer) restoreHeadEMA(intent *SteeringIntent) error {
	if intent == nil {
		return nil
	}
	artifact, verified, err := s.intents.MeasurementArtifact(intent)
	if err != nil {
		return fmt.Errorf("restore head EMA measurement: %w", err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, artifact, verified); err != nil {
		return fmt.Errorf("restore head EMA intent: %w", err)
	}
	if err := s.headEMA.CommitForEpoch(artifact.SubnetEpoch, artifact.HeadEMA, artifact.Policy.Steering.HeadScoreEMA); err != nil {
		return fmt.Errorf("restore head EMA state: %w", err)
	}
	return nil
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
	preparedRuntimeHash, err := types.NewHashFromHexString(current.Prepared.PreparedAtBlockHash)
	if err != nil {
		return false, fmt.Errorf("pending steering preparation hash: %w", err)
	}
	if err := authenticatePinnedNativeRuntimeAt(s.native, s.cfg, preparedRuntimeHash); err != nil {
		return false, fmt.Errorf("authenticate pending steering preparation runtime at %s: %w", preparedRuntimeHash.Hex(), err)
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
		if err := authenticatePinnedNativeRuntimeAt(s.native, s.cfg, receipt.BlockHash); err != nil {
			return false, fmt.Errorf("authenticate recovered steering finality at %s: %w", receipt.BlockHash.Hex(), err)
		}
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
	nonceHash, err := authenticatePinnedNativeRuntime(s.native, s.cfg)
	if err != nil {
		return false, fmt.Errorf("authenticate steering nonce runtime: %w", err)
	}
	finalizedNonce, err := s.native.AccountNonceAt(s.hotkey.PublicKey(), nonceHash)
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
	if _, err := authenticatePinnedNativeRuntime(s.native, s.cfg); err != nil {
		return false, fmt.Errorf("authenticate native runtime before pending replay: %w", err)
	}
	result, err := crv4.SubmitPrepared(ctx, s.native, current.Prepared)
	if err != nil {
		_ = s.intents.update(current.VectorHash, "pending", func(i *SteeringIntent) error { i.Error = err.Error(); return nil })
		return false, err
	}
	if err := authenticatePinnedNativeRuntimeAt(s.native, s.cfg, result.FinalizedBlockHash); err != nil {
		_ = s.intents.update(current.VectorHash, "pending", func(i *SteeringIntent) error { i.Error = err.Error(); return nil })
		return false, fmt.Errorf("authenticate replayed steering finality at %s: %w", result.FinalizedBlockHash.Hex(), err)
	}
	if err := s.intents.MarkFinalized(current.VectorHash, result.TxHash.Hex(), result.FinalizedBlock, result.FinalizedBlockHash.Hex(), result.RevealBlock, result.Values); err != nil {
		return false, err
	}
	return true, nil
}

func (s *ReleaseSteerer) SubmitOnce(ctx context.Context) error {
	nativeHash, err := authenticatePinnedNativeRuntime(s.native, s.cfg)
	if err != nil {
		return fmt.Errorf("authenticate native runtime before steering snapshot: %w", err)
	}
	nativeState, err := s.native.EpochScheduleStateAt(s.cfg.Netuid, nativeHash)
	if err != nil {
		return err
	}
	snapshot, err := s.chain.ReleaseSnapshotContext(ctx)
	if err != nil {
		return err
	}
	if err := s.validatePinnedChains(snapshot, nativeState, nativeHash); err != nil {
		return err
	}
	current, err := s.intents.Current()
	if err != nil {
		return err
	}
	if err := s.restoreHeadEMA(current); err != nil {
		return err
	}
	if err := s.checkApplication(snapshot); err != nil {
		return err
	}
	if current, err = s.intents.Current(); err != nil {
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
	if err := s.foldSettlementEpoch(snapshot); err != nil {
		return err
	}
	head, err := s.gatherHead(snapshot, nativeState.SubnetEpochIndex, nativeState.CurrentBlock, nativeHash.Hex())
	if err != nil {
		return err
	}
	var poolObservations []ReleasePoolMeasurement
	_, _, depositAudits, err := s.gatherPools(ctx, snapshot, head.Bound, &poolObservations)
	if err != nil {
		return err
	}
	selfUID, found, err := s.chain.FindUidByHotkeyAt(snapshot.BlockNumber, s.cfg.Netuid, s.hotkey.PublicKey())
	if err != nil {
		return fmt.Errorf("self-mask: %w", err)
	}
	if !found {
		return fmt.Errorf("self-mask: validator hotkey has no live UID on netuid %d", s.cfg.Netuid)
	}
	previousArtifactHash := ""
	if current, currentErr := s.intents.Current(); currentErr != nil {
		return currentErr
	} else if current != nil {
		previousArtifactHash = current.MeasurementArtifactHash
	}
	controlledNOIDs := append(make([]uint64, 0, len(s.cfg.ControlledNOIDs)), s.cfg.ControlledNOIDs...)
	sort.Slice(controlledNOIDs, func(i, j int) bool { return controlledNOIDs[i] < controlledNOIDs[j] })
	measurementArtifact := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchema, DeploymentID: s.cfg.DeploymentID, ChainID: s.cfg.ChainID,
		GenesisHash: strings.ToLower(s.cfg.GenesisHash), Coordinator: s.cfg.Coordinator,
		SettlementVault: s.cfg.SettlementVault, ValidatorID: s.cfg.ValidatorID, Netuid: s.cfg.Netuid,
		SubnetEpoch: nativeState.SubnetEpochIndex, NativeSnapshotBlock: nativeState.CurrentBlock,
		NativeSnapshotHash: strings.ToLower(nativeHash.Hex()), EVMSnapshotBlock: snapshot.BlockNumber,
		EVMSnapshotHash: releaseHex32(snapshot.BlockHash), SettlementEpoch: snapshot.Epoch.Uint64(),
		PolicyHash: strings.ToLower(s.cfg.PolicyHash), Policy: s.cfg.Policy,
		PreviousArtifactHash: previousArtifactHash, ControlledNOIDs: controlledNOIDs,
		Inputs: head.Inputs, Bindings: head.Bindings, HeadEMA: head.HeadEMA,
		Pools: poolObservations, DepositAudits: depositAudits, SelfUID: selfUID,
	}
	measurementBytes, measurementHash, verifiedMeasurement, err := SealReleaseMeasurementArtifact(measurementArtifact)
	if err != nil {
		return err
	}
	measurementPath, measurementSize, err := persistReleaseMeasurementArtifact(s.cfg.StateDir, measurementBytes, measurementHash)
	if err != nil {
		return err
	}
	uids := verifiedMeasurement.UIDs
	scores := verifiedMeasurement.Scores
	encodedScores, err := rationalJSON(scores)
	if err != nil {
		return err
	}
	eligibleScores := make([]*big.Rat, len(verifiedMeasurement.EligibleHead))
	for index := range verifiedMeasurement.EligibleHead {
		eligibleScores[index] = verifiedMeasurement.EligibleHead[index].Score
	}
	encodedEligibleScores, err := rationalJSON(eligibleScores)
	if err != nil {
		return err
	}
	if _, err := authenticatePinnedNativeRuntime(s.native, s.cfg); err != nil {
		return fmt.Errorf("authenticate native runtime before preparing steering: %w", err)
	}
	prepared, err := crv4.PrepareWeightsCRv4Exact(ctx, s.native, s.hotkey, s.cfg.Netuid, uids, scores, releaseSubmitOptions(s.cfg))
	if err != nil {
		return err
	}
	preparedHash, err := types.NewHashFromHexString(prepared.PreparedAtBlockHash)
	if err != nil {
		return fmt.Errorf("decode prepared steering runtime hash: %w", err)
	}
	if err := authenticatePinnedNativeRuntimeAt(s.native, s.cfg, preparedHash); err != nil {
		return fmt.Errorf("authenticate prepared steering runtime at %s: %w", preparedHash.Hex(), err)
	}
	if prepared.SubnetEpoch != nativeState.SubnetEpochIndex {
		return fmt.Errorf("native epoch crossed during steering snapshot: started %d prepared %d", nativeState.SubnetEpochIndex, prepared.SubnetEpoch)
	}
	envelopeBytes, envelopeHash, _, err := SealReleaseMeasurementEnvelope(measurementBytes, selfUID, s.hotkey, strings.ToLower(prepared.ExtrinsicHash), time.Now().UTC())
	if err != nil {
		return err
	}
	envelopePath, envelopeSize, err := persistReleaseMeasurementEnvelope(s.cfg.StateDir, envelopeBytes, envelopeHash)
	if err != nil {
		return err
	}
	intent, err := s.intents.Begin(SteeringIntent{
		ValidatorID:             s.cfg.ValidatorID,
		Netuid:                  s.cfg.Netuid,
		SubnetEpoch:             nativeState.SubnetEpochIndex,
		NativeSnapshotBlock:     nativeState.CurrentBlock,
		NativeSnapshotHash:      nativeHash.Hex(),
		EVMSnapshotBlock:        snapshot.BlockNumber,
		EVMSnapshotHash:         fmt.Sprintf("0x%x", snapshot.BlockHash),
		SettlementEpoch:         snapshot.Epoch.Uint64(),
		PolicyHash:              s.cfg.PolicyHash,
		MeasurementArtifactPath: measurementPath,
		MeasurementArtifactHash: measurementHash,
		MeasurementArtifactSize: measurementSize,
		MeasurementEnvelopePath: envelopePath,
		MeasurementEnvelopeHash: envelopeHash,
		MeasurementEnvelopeSize: envelopeSize,
		SelfUID:                 selfUID,
		MaskedUIDs:              verifiedMeasurement.MaskedUIDs,
		EligibleHeadUIDs:        headSelectionUIDs(verifiedMeasurement.EligibleHead),
		EligibleHeadScores:      encodedEligibleScores,
		SelectedHeadUIDs:        headSelectionUIDs(verifiedMeasurement.SelectedHead),
		RejectedHeadUIDs:        headSelectionUIDs(verifiedMeasurement.RejectedHead),
		StaleHeadBindings:       verifiedMeasurement.StaleBindings,
		DepositAudits:           depositAudits,
		UIDs:                    uids,
		Scores:                  encodedScores,
		Prepared:                prepared,
	})
	if err != nil {
		return err
	}
	if err := s.headEMA.CommitForEpoch(measurementArtifact.SubnetEpoch, measurementArtifact.HeadEMA, measurementArtifact.Policy.Steering.HeadScoreEMA); err != nil {
		return fmt.Errorf("commit head EMA after steering intent: %w", err)
	}
	if _, err := authenticatePinnedNativeRuntime(s.native, s.cfg); err != nil {
		return fmt.Errorf("authenticate native runtime before steering broadcast: %w", err)
	}
	result, err := crv4.SubmitPrepared(ctx, s.native, prepared)
	if err != nil {
		// The error can occur after broadcast but before finality was observed.
		// Preserve an uncertain pending state so a restart cannot double-submit.
		_ = s.intents.update(intent.VectorHash, "pending", func(i *SteeringIntent) error { i.Error = err.Error(); return nil })
		return err
	}
	if err := authenticatePinnedNativeRuntimeAt(s.native, s.cfg, result.FinalizedBlockHash); err != nil {
		_ = s.intents.update(intent.VectorHash, "pending", func(i *SteeringIntent) error { i.Error = err.Error(); return nil })
		return fmt.Errorf("authenticate steering finality at %s: %w", result.FinalizedBlockHash.Hex(), err)
	}
	return s.intents.MarkFinalized(intent.VectorHash, result.TxHash.Hex(), result.FinalizedBlock, result.FinalizedBlockHash.Hex(), result.RevealBlock, result.Values)
}

const releaseSteeringFailureLimit = 10

// runReleaseSteeringLoop retries an incomplete native epoch until the exact
// intent is finalized. Merely observing an epoch never suppresses a retry.
// Repeated failures become process-visible, and advancing past an incomplete
// epoch fails closed instead of silently creating a gap in public lineage.
func runReleaseSteeringLoop(ctx context.Context, poll time.Duration, epoch func() (uint64, error), submit func() error) error {
	if ctx == nil || poll <= 0 || epoch == nil || submit == nil {
		return errors.New("release steering loop configuration is incomplete")
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	var targetEpoch uint64
	targetKnown := false
	completed := false
	failures := 0
	for {
		currentEpoch, err := epoch()
		if err == nil {
			if targetKnown && currentEpoch < targetEpoch {
				return fmt.Errorf("release steering epoch regressed from %d to %d", targetEpoch, currentEpoch)
			}
			if !targetKnown || currentEpoch > targetEpoch {
				if targetKnown && !completed {
					return fmt.Errorf("release steering advanced from incomplete epoch %d to %d", targetEpoch, currentEpoch)
				}
				targetEpoch, targetKnown, completed, failures = currentEpoch, true, false, 0
			}
			if !completed {
				err = submit()
				if err == nil || errors.Is(err, ErrSteeringAlreadyFinal) {
					completed, failures = true, 0
				} else {
					failures++
					fmt.Printf("release steer: subnet epoch %d attempt %d: %v\n", targetEpoch, failures, err)
				}
			}
		} else {
			failures++
			fmt.Printf("release steer: finalized scheduler attempt %d: %v\n", failures, err)
		}
		if failures >= releaseSteeringFailureLimit {
			return fmt.Errorf("release steering failed %d consecutive attempts: %w", failures, err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Run supervises release steering until cancellation or a process-fatal state
// error. The caller must propagate a non-nil result to its service supervisor.
func (s *ReleaseSteerer) Run(ctx context.Context) error {
	poll := time.Duration(s.cfg.PollSeconds) * time.Second
	return runReleaseSteeringLoop(ctx, poll, func() (uint64, error) {
		finalized, err := authenticatePinnedNativeRuntime(s.native, s.cfg)
		if err != nil {
			return 0, err
		}
		state, err := s.native.EpochScheduleStateAt(s.cfg.Netuid, finalized)
		if err != nil {
			return 0, err
		}
		return state.SubnetEpochIndex, nil
	}, func() error {
		return s.SubmitOnce(ctx)
	})
}
