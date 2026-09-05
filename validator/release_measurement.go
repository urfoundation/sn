package validator

// release_measurement.go defines the public, canonical transcript from which
// an independent verifier reconstructs a validator's complete release weight
// decision. Declared head scores, pool quality and final vectors are outputs,
// never trusted inputs.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/protocol"
)

// ReleaseMeasurementSchema is the immutable release-1.0 transcript format.
const ReleaseMeasurementSchema = "urnetwork-validator-release-measurement-v1"

const releaseMeasurementInputSchema = "urnetwork-validator-release-measurement-input-v2"

const (
	releasePoolAlphaNumerator   = uint64(1)
	releasePoolAlphaDenominator = uint64(10)
	releasePoolLatRefMillis     = uint64(4000)
)

// ReleaseMeasurementInput is one operator-isolated atomic statistics cut.
type ReleaseMeasurementInput struct {
	NoID                uint64                  `json:"no_id"`
	SettlementEpoch     uint64                  `json:"settlement_epoch"`
	CutNativeBlock      uint64                  `json:"cut_native_block"`
	CutNativeBlockHash  string                  `json:"cut_native_block_hash"`
	CutEVMSnapshotBlock uint64                  `json:"cut_evm_snapshot_block"`
	CutEVMSnapshotHash  string                  `json:"cut_evm_snapshot_hash"`
	EgressGeneration    uint64                  `json:"egress_generation"`
	Stats               ReleaseStatsMeasurement `json:"stats"`
}

// releaseMeasurementInputJournal makes native-window rotation durable before
// the destructive in-memory cut. It is private validator state until embedded
// in the final public measurement artifact.
type releaseMeasurementInputJournal struct {
	Schema           string                  `json:"schema"`
	DeploymentID     string                  `json:"deployment_id"`
	ChainID          uint64                  `json:"chain_id"`
	GenesisHash      string                  `json:"genesis_hash"`
	Coordinator      string                  `json:"coordinator"`
	ValidatorID      uint64                  `json:"validator_id"`
	Netuid           uint16                  `json:"netuid"`
	SubnetEpoch      uint64                  `json:"subnet_epoch"`
	PolicyHash       string                  `json:"policy_hash"`
	MeasurementInput ReleaseMeasurementInput `json:"measurement_input"`
}

// ReleaseBindingMeasurement records the exact coordinator binding and native
// UID observation made for every provider in an input cut.
type ReleaseBindingMeasurement struct {
	NoID           uint64 `json:"no_id"`
	ClientID       string `json:"client_id"`
	Active         bool   `json:"active"`
	FleetID        string `json:"fleet_id"`
	Hotkey         string `json:"hotkey"`
	ClientKey      string `json:"client_key"`
	LocalClientKey string `json:"local_client_key"`
	CommitmentHash string `json:"commitment_hash"`
	Generation     uint64 `json:"generation"`
	ValidFromEpoch uint64 `json:"valid_from_epoch"`
	ValidToEpoch   uint64 `json:"valid_to_epoch"`
	CleanedAtEpoch uint64 `json:"cleaned_at_epoch"`
	RecordUID      uint16 `json:"record_uid"`
	Cleaned        bool   `json:"cleaned"`
	LiveUIDFound   bool   `json:"live_uid_found"`
	LiveUID        uint16 `json:"live_uid"`
}

// ReleasePoolMeasurement records the native pool UID observed for each active
// coordinator operator. Deposit evidence is kept separately and joined by id.
type ReleasePoolMeasurement struct {
	NoID       uint64 `json:"no_id"`
	UID        uint16 `json:"uid"`
	PoolHotkey string `json:"pool_hotkey"`
}

// ReleaseMeasurementArtifact contains only source observations and exact EMA
// transitions. Its verifier derives all candidate scores, exclusions, masks,
// rankings and the final rational vector.
type ReleaseMeasurementArtifact struct {
	Schema               string                      `json:"schema"`
	DeploymentID         string                      `json:"deployment_id"`
	ChainID              uint64                      `json:"chain_id"`
	GenesisHash          string                      `json:"genesis_hash"`
	Coordinator          string                      `json:"coordinator"`
	SettlementVault      string                      `json:"settlement_vault"`
	ValidatorID          uint64                      `json:"validator_id"`
	Netuid               uint16                      `json:"netuid"`
	SubnetEpoch          uint64                      `json:"subnet_epoch"`
	NativeSnapshotBlock  uint64                      `json:"native_snapshot_block"`
	NativeSnapshotHash   string                      `json:"native_snapshot_hash"`
	EVMSnapshotBlock     uint64                      `json:"evm_snapshot_block"`
	EVMSnapshotHash      string                      `json:"evm_snapshot_hash"`
	SettlementEpoch      uint64                      `json:"settlement_epoch"`
	PolicyHash           string                      `json:"policy_hash"`
	Policy               protocol.Policy             `json:"policy"`
	PreviousArtifactHash string                      `json:"previous_artifact_hash,omitempty"`
	ControlledNOIDs      []uint64                    `json:"controlled_no_ids"`
	Inputs               []ReleaseMeasurementInput   `json:"inputs"`
	Bindings             []ReleaseBindingMeasurement `json:"bindings"`
	HeadEMA              []HeadEMAMeasurement        `json:"head_ema"`
	Pools                []ReleasePoolMeasurement    `json:"pools"`
	DepositAudits        []DepositAudit              `json:"deposit_audits"`
	SelfUID              uint16                      `json:"self_uid"`
}

// VerifiedReleasePool is one independently derived pool decision. Ineligible
// pools remain present with a zero score so dishonest deposits are explicit.
type VerifiedReleasePool struct {
	NoID       uint64
	UID        uint16
	QualityPPM uint32
	Eligible   bool
	Controlled bool
	Score      *big.Rat
	Audit      DepositAudit
}

// VerifiedReleaseMeasurement is the complete independently reconstructed
// decision that must match the signed steering intent and applied chain vector.
type VerifiedReleaseMeasurement struct {
	EligibleHead   []ExactWeightInput
	SelectedHead   []ExactWeightInput
	RejectedHead   []ExactWeightInput
	StaleBindings  []StaleHeadBinding
	Pools          []VerifiedReleasePool
	MaskedUIDs     []uint16
	UIDs           []uint16
	Scores         []*big.Rat
	BoundProviders map[uint64]map[connect.Id]bool
	StatsByNO      map[uint64]VerifiedReleaseStats
}

// parseReleaseHex32 requires the unique lowercase 0x-prefixed representation.
func parseReleaseHex32(name, encoded string, zeroAllowed bool) ([32]byte, error) {
	var value [32]byte
	if encoded != strings.ToLower(encoded) || len(encoded) != 66 || !strings.HasPrefix(encoded, "0x") {
		return value, fmt.Errorf("%s is not canonical 32-byte hex", name)
	}
	decoded, err := hex.DecodeString(encoded[2:])
	if err != nil || len(decoded) != 32 {
		return value, fmt.Errorf("%s is not 32-byte hex", name)
	}
	copy(value[:], decoded)
	if !zeroAllowed && value == ([32]byte{}) {
		return value, fmt.Errorf("%s is zero", name)
	}
	return value, nil
}

// releaseHex32 renders the canonical public representation.
func releaseHex32(value [32]byte) string {
	return "0x" + hex.EncodeToString(value[:])
}

// verifyReleaseMeasurementIdentity validates chain, policy and snapshot pins.
func verifyReleaseMeasurementIdentity(artifact *ReleaseMeasurementArtifact) error {
	if artifact == nil || artifact.Schema != ReleaseMeasurementSchema || artifact.DeploymentID == "" || artifact.ValidatorID == 0 || artifact.ChainID == 0 || artifact.Netuid == 0 {
		return errors.New("release measurement identity is incomplete")
	}
	if artifact.Coordinator != strings.ToLower(artifact.Coordinator) || artifact.SettlementVault != strings.ToLower(artifact.SettlementVault) || !common.IsHexAddress(artifact.Coordinator) || common.HexToAddress(artifact.Coordinator) == (common.Address{}) || !common.IsHexAddress(artifact.SettlementVault) || common.HexToAddress(artifact.SettlementVault) == (common.Address{}) {
		return errors.New("release measurement contract identity is invalid")
	}
	if artifact.ControlledNOIDs == nil || artifact.Inputs == nil || artifact.Bindings == nil || artifact.HeadEMA == nil || artifact.Pools == nil || artifact.DepositAudits == nil {
		return errors.New("release measurement collections must use canonical arrays")
	}
	if _, err := parseReleaseHex32("genesis hash", artifact.GenesisHash, false); err != nil {
		return err
	}
	if artifact.NativeSnapshotBlock == 0 || artifact.EVMSnapshotBlock == 0 {
		return errors.New("release measurement snapshot block is zero")
	}
	if _, err := parseReleaseHex32("native snapshot hash", artifact.NativeSnapshotHash, false); err != nil {
		return err
	}
	if _, err := parseReleaseHex32("EVM snapshot hash", artifact.EVMSnapshotHash, false); err != nil {
		return err
	}
	if artifact.PreviousArtifactHash != "" {
		if _, err := parseReleaseContentHash(artifact.PreviousArtifactHash); err != nil {
			return fmt.Errorf("previous artifact hash: %w", err)
		}
	}
	if err := artifact.Policy.Validate(); err != nil {
		return fmt.Errorf("release measurement policy: %w", err)
	}
	policyHash, err := artifact.Policy.Hash()
	if err != nil {
		return err
	}
	configuredHash, err := parseReleaseHex32("policy hash", artifact.PolicyHash, false)
	if err != nil {
		return err
	}
	if policyHash != configuredHash {
		return errors.New("release measurement policy hash does not match policy bytes")
	}
	return nil
}

// releaseMeasurementStats validates canonical operator cuts and constructs
// lookup maps used by both pool and head reconstruction.
func releaseMeasurementStats(artifact *ReleaseMeasurementArtifact) (map[uint64]VerifiedReleaseStats, error) {
	statsByNO := make(map[uint64]VerifiedReleaseStats, len(artifact.Inputs))
	priorNOID := uint64(0)
	transitions := make([]*AttemptSettlementTransition, 0, len(artifact.Inputs))
	transitionCount := 0
	for index, input := range artifact.Inputs {
		if input.NoID == 0 || (index > 0 && input.NoID <= priorNOID) {
			return nil, errors.New("release measurement inputs are not strictly ordered")
		}
		if input.CutNativeBlock == 0 || !releaseBlockAtOrBefore(input.CutNativeBlock, input.CutNativeBlockHash, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash) {
			return nil, fmt.Errorf("operator %d native cut is outside the decision snapshot", input.NoID)
		}
		if _, err := parseReleaseHex32("native cut hash", input.CutNativeBlockHash, false); err != nil {
			return nil, fmt.Errorf("operator %d: %w", input.NoID, err)
		}
		if input.SettlementEpoch != artifact.SettlementEpoch || input.CutEVMSnapshotBlock == 0 || !releaseBlockAtOrBefore(input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash, artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash) {
			return nil, fmt.Errorf("operator %d EVM cut is outside the decision settlement snapshot", input.NoID)
		}
		if _, err := parseReleaseHex32("EVM cut hash", input.CutEVMSnapshotHash, false); err != nil {
			return nil, fmt.Errorf("operator %d: %w", input.NoID, err)
		}
		if input.Stats.Config.AMin != artifact.Policy.Verify.ReliabilityAMin || input.Stats.Config.AlphaNumerator != releasePoolAlphaNumerator || input.Stats.Config.AlphaDenominator != releasePoolAlphaDenominator || input.Stats.Config.LatRefMillis != releasePoolLatRefMillis {
			return nil, fmt.Errorf("operator %d statistics policy differs from release policy", input.NoID)
		}
		cut := input.Stats.AttemptCut
		if cut == nil {
			return nil, fmt.Errorf("operator %d statistics omit the signed attempt cut", input.NoID)
		}
		identity := cut.Identity
		if identity.DeploymentID != artifact.DeploymentID || identity.ChainID != artifact.ChainID || identity.GenesisHash != artifact.GenesisHash || identity.Netuid != artifact.Netuid || identity.ValidatorID != artifact.ValidatorID || identity.ValidatorUID != artifact.SelfUID || identity.NoID != input.NoID {
			return nil, fmt.Errorf("operator %d attempt cut identity differs from the release measurement", input.NoID)
		}
		if cut.Boundary.SettlementEpoch != input.SettlementEpoch || cut.Boundary.EVMBlock != input.CutEVMSnapshotBlock || cut.Boundary.EVMBlockHash != input.CutEVMSnapshotHash {
			return nil, fmt.Errorf("operator %d attempt cut boundary differs from the atomic statistics cut", input.NoID)
		}
		verified, err := VerifyReleaseStatsMeasurement(input.Stats)
		if err != nil {
			return nil, fmt.Errorf("operator %d statistics: %w", input.NoID, err)
		}
		if transition := input.Stats.SettlementTransition; transition != nil {
			transitionCount++
			if transition.Identity.NoID != input.NoID || transition.Identity.DeploymentID != artifact.DeploymentID || transition.Identity.ChainID != artifact.ChainID || transition.Identity.GenesisHash != artifact.GenesisHash || transition.Identity.Netuid != artifact.Netuid || transition.Identity.ValidatorID != artifact.ValidatorID || transition.Identity.ValidatorUID != artifact.SelfUID || transition.ToEpoch != artifact.SettlementEpoch || transition.FromBoundary.EVMBlock >= input.CutEVMSnapshotBlock {
				return nil, fmt.Errorf("operator %d settlement transition differs from the release measurement", input.NoID)
			}
			transitions = append(transitions, transition)
		}
		statsByNO[input.NoID] = verified
		priorNOID = input.NoID
	}
	if len(statsByNO) < artifact.Policy.Safety.MinimumHealthyNOCount {
		return nil, errors.New("release measurement has too few isolated operator inputs")
	}
	if transitionCount != 0 {
		if transitionCount != len(artifact.Inputs) {
			return nil, errors.New("release measurement settlement transition coverage is partial")
		}
		if err := VerifyAttemptSettlementBatch(transitions); err != nil {
			return nil, fmt.Errorf("release measurement settlement batch: %w", err)
		}
	}
	return statsByNO, nil
}

// releaseMeasurementBindings validates exact provider coverage, then derives
// fleet prefix sets, pool exclusions and stale-binding evidence.
func releaseMeasurementBindings(artifact *ReleaseMeasurementArtifact, statsByNO map[uint64]VerifiedReleaseStats) (map[FleetScoreKey]map[[32]byte]bool, map[uint64]map[connect.Id]bool, map[uint16][]releaseHeadMember, []StaleHeadBinding, error) {
	fleets := map[FleetScoreKey]map[[32]byte]bool{}
	bound := map[uint64]map[connect.Id]bool{}
	membersByUID := map[uint16][]releaseHeadMember{}
	activeFleetByProvider := map[string]FleetScoreKey{}
	seenProvider := map[string]bool{}
	providerOwner := map[string]uint64{}
	priorKey := ""
	for index, binding := range artifact.Bindings {
		key := fmt.Sprintf("%020d:%s", binding.NoID, binding.ClientID)
		if priorKey != "" && key <= priorKey {
			return nil, nil, nil, nil, errors.New("release binding observations are not strictly ordered")
		}
		stats, ok := statsByNO[binding.NoID]
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("binding %d references unknown no_id %d", index, binding.NoID)
		}
		clientID, err := connect.ParseId(binding.ClientID)
		if err != nil || clientID.String() != binding.ClientID {
			return nil, nil, nil, nil, fmt.Errorf("binding %d client id is not canonical", index)
		}
		if _, ok := stats.Providers[clientID]; !ok {
			return nil, nil, nil, nil, fmt.Errorf("binding %d references a provider absent from its statistics cut", index)
		}
		providerKey := fmt.Sprintf("%020d:%s", binding.NoID, binding.ClientID)
		if seenProvider[providerKey] {
			return nil, nil, nil, nil, fmt.Errorf("provider %s has duplicate binding observations", binding.ClientID)
		}
		if owner, exists := providerOwner[binding.ClientID]; exists && owner != binding.NoID {
			return nil, nil, nil, nil, fmt.Errorf("provider %s appears in more than one operator context", binding.ClientID)
		}
		providerOwner[binding.ClientID] = binding.NoID
		seenProvider[providerKey] = true
		fleetID, err := parseReleaseHex32("fleet id", binding.FleetID, true)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		hotkey, err := parseReleaseHex32("hotkey", binding.Hotkey, true)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		clientKey, err := parseReleaseHex32("client key", binding.ClientKey, true)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		localClientKey, err := parseReleaseHex32("local client key", binding.LocalClientKey, true)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		commitmentHash, err := parseReleaseHex32("commitment hash", binding.CommitmentHash, true)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if !binding.Active {
			if localClientKey != ([32]byte{}) || binding.LiveUIDFound || binding.LiveUID != 0 {
				return nil, nil, nil, nil, fmt.Errorf("inactive binding %s contains live local observations", binding.ClientID)
			}
			priorKey = key
			continue
		}
		if fleetID == ([32]byte{}) || hotkey == ([32]byte{}) || clientKey == ([32]byte{}) || localClientKey != clientKey || binding.Generation == 0 || binding.Cleaned || binding.CleanedAtEpoch != 0 || binding.ValidFromEpoch > artifact.SettlementEpoch || binding.ValidToEpoch < artifact.SettlementEpoch || binding.ValidToEpoch < binding.ValidFromEpoch {
			return nil, nil, nil, nil, fmt.Errorf("active binding %s is incomplete, mismatched or outside its epoch", binding.ClientID)
		}
		if artifact.Policy.Binding.CommitmentsRequired && commitmentHash == ([32]byte{}) {
			return nil, nil, nil, nil, fmt.Errorf("active binding %s has no required commitment", binding.ClientID)
		}
		if !binding.LiveUIDFound || binding.LiveUID != binding.RecordUID {
			priorKey = key
			continue
		}
		fleetKey := FleetScoreKey{FleetID: fleetID, Hotkey: hotkey, Generation: binding.Generation, UID: binding.LiveUID}
		if fleets[fleetKey] == nil {
			fleets[fleetKey] = map[[32]byte]bool{}
		}
		activeFleetByProvider[providerKey] = fleetKey
		if bound[binding.NoID] == nil {
			bound[binding.NoID] = map[connect.Id]bool{}
		}
		bound[binding.NoID][clientID] = true
		membersByUID[binding.LiveUID] = append(membersByUID[binding.LiveUID], releaseHeadMember{NoID: binding.NoID, ClientID: clientID})
		priorKey = key
	}
	for noID, stats := range statsByNO {
		if bound[noID] == nil {
			bound[noID] = map[connect.Id]bool{}
		}
		for clientID := range stats.Providers {
			providerKey := fmt.Sprintf("%020d:%s", noID, clientID.String())
			if !seenProvider[providerKey] {
				return nil, nil, nil, nil, fmt.Errorf("provider %s has no binding observation", clientID)
			}
		}
	}
	// Only a server-attested prefix captured while this exact fleet generation
	// was active can feed its head score. The provider-level detached map is
	// still checked against the complete signed attempt cut, but deliberately
	// is not joined to the provider's current binding: doing that would let a
	// rebind inherit work measured for its prior owner/generation.
	for _, input := range artifact.Inputs {
		claims, err := AttemptCutEgressClaims(input.Stats.AttemptCut)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("operator %d attempt egress claims: %w", input.NoID, err)
		}
		for _, claim := range claims {
			providerKey := fmt.Sprintf("%020d:%s", input.NoID, claim.Binding.ClientID.String())
			fleetKey, active := activeFleetByProvider[providerKey]
			if !active || !claim.Binding.Active || !claim.Binding.UIDFound || claim.Binding.FleetID != releaseHex32(fleetKey.FleetID) || claim.Binding.Hotkey != releaseHex32(fleetKey.Hotkey) || claim.Binding.Generation != fleetKey.Generation || claim.Binding.UID != fleetKey.UID {
				continue
			}
			egressHash, err := parseReleaseHex32("attempt egress hash", claim.EgressIPHash, false)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("operator %d provider %s: %w", input.NoID, claim.Binding.ClientID, err)
			}
			fleets[fleetKey][egressHash] = true
		}
	}
	stale := make([]StaleHeadBinding, 0)
	for _, binding := range artifact.Bindings {
		if binding.Active && (!binding.LiveUIDFound || binding.LiveUID != binding.RecordUID) {
			stale = append(stale, StaleHeadBinding{NoID: binding.NoID, ClientID: binding.ClientID, RecordUID: binding.RecordUID, LiveUID: binding.LiveUID, Found: binding.LiveUIDFound})
		}
	}
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].NoID != stale[j].NoID {
			return stale[i].NoID < stale[j].NoID
		}
		return stale[i].ClientID < stale[j].ClientID
	})
	return fleets, bound, membersByUID, stale, nil
}

// releaseRawHeadScores applies exact shared-prefix splitting by fleet identity.
func releaseRawHeadScores(fleets map[FleetScoreKey]map[[32]byte]bool) map[FleetScoreKey]*big.Rat {
	claims := map[[32]byte]uint64{}
	for _, hashes := range fleets {
		for hash := range hashes {
			claims[hash]++
		}
	}
	raw := make(map[FleetScoreKey]*big.Rat, len(fleets))
	for key, hashes := range fleets {
		score := new(big.Rat)
		for hash := range hashes {
			score.Add(score, new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).SetUint64(claims[hash])))
		}
		raw[key] = score
	}
	return raw
}

// releaseMeasurementHead verifies the EMA transcript against reconstructed
// prefix scores and applies the canonical top-N boundary.
func releaseMeasurementHead(artifact *ReleaseMeasurementArtifact, fleets map[FleetScoreKey]map[[32]byte]bool) ([]ExactWeightInput, HeadSelection, error) {
	if err := verifyHeadEMAFold(artifact.HeadEMA, artifact.Policy.Steering.HeadScoreEMA); err != nil {
		return nil, HeadSelection{}, err
	}
	raw := releaseRawHeadScores(fleets)
	rawCount := 0
	for index, record := range artifact.HeadEMA {
		if !record.HasRaw {
			continue
		}
		rawCount++
		want, ok := raw[record.Key]
		got, err := decodeRationalJSON(record.Raw)
		if err != nil || !ok || got.Cmp(want) != 0 {
			return nil, HeadSelection{}, fmt.Errorf("head EMA record %d raw score does not match routable-prefix evidence", index)
		}
	}
	if rawCount != len(raw) {
		return nil, HeadSelection{}, errors.New("head EMA transcript omits a live fleet")
	}
	ema, err := headEMAOutput(artifact.HeadEMA)
	if err != nil {
		return nil, HeadSelection{}, err
	}
	uids := make([]uint16, 0, len(ema))
	for uid := range ema {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	eligible := make([]ExactWeightInput, 0, len(uids))
	for _, uid := range uids {
		eligible = append(eligible, ExactWeightInput{UID: uid, Score: new(big.Rat).Set(ema[uid])})
	}
	selection, err := selectHeadFleets(eligible, artifact.Policy.Steering.MaximumHeadFleets)
	if err != nil {
		return nil, HeadSelection{}, err
	}
	ranked := append(append([]ExactWeightInput(nil), selection.Selected...), selection.Rejected...)
	return ranked, selection, nil
}

func parseCanonicalDepositAmount(name, encoded string) (*big.Int, error) {
	value, ok := new(big.Int).SetString(encoded, 10)
	if !ok || value.Sign() < 0 || value.String() != encoded {
		return nil, fmt.Errorf("%s is not a canonical non-negative decimal", name)
	}
	return value, nil
}

func verifyCanonicalDepositAddress(name, encoded string) error {
	if encoded != strings.ToLower(encoded) || !common.IsHexAddress(encoded) || common.HexToAddress(encoded) == (common.Address{}) {
		return fmt.Errorf("%s is not a canonical nonzero address", name)
	}
	return nil
}

func verifyDepositCommitmentEvidence(audit DepositAudit, conviction, required *big.Int, artifact *ReleaseMeasurementArtifact) error {
	if _, err := parseReleaseContentHash(audit.ArtifactHash); err != nil {
		return fmt.Errorf("payout artifact hash: %w", err)
	}
	contentHash, _ := parseReleaseContentHash(audit.ArtifactHash)
	committedHash, err := parseReleaseHex32("committed payout artifact hash", audit.CommittedArtifactHash, false)
	if err != nil || committedHash != contentHash {
		return errors.New("payout artifact hash does not equal its on-chain commitment")
	}
	if _, err := parseReleaseHex32("payout root", audit.PayoutRoot, false); err != nil {
		return err
	}
	for _, address := range []struct {
		name  string
		value string
	}{{"artifact signer", audit.ArtifactSigner}, {"root committer", audit.RootCommitter}, {"root signer", audit.RootSigner}} {
		if err := verifyCanonicalDepositAddress(address.name, address.value); err != nil {
			return err
		}
	}
	if audit.RootCommitter != audit.RootSigner {
		return errors.New("payout root committer is not the operator root signer")
	}
	if audit.SourceStartBlock == 0 || audit.SourceEndBlock <= audit.SourceStartBlock || audit.SourceEndBlock > audit.ObservedAtBlock || audit.RootCommitBlock < audit.SourceEndBlock || audit.RootCommitBlock > audit.ObservedAtBlock || audit.RootCommitBlock > audit.ArtifactDeadlineBlock {
		return errors.New("payout artifact or commitment block boundaries are invalid")
	}
	if _, err := parseReleaseHex32("payout source start hash", audit.SourceStartHash, false); err != nil {
		return err
	}
	if _, err := parseReleaseHex32("payout source end hash", audit.SourceEndHash, false); err != nil {
		return err
	}
	want, tier, err := protocol.RequiredDepositRao(audit.UsageBytes, conviction, artifact.Policy.Deposit)
	if err != nil {
		return fmt.Errorf("reconstruct required deposit: %w", err)
	}
	if want.Cmp(required) != 0 || audit.RateNumeratorRaoPerGiB != tier.RateNumeratorRaoPerGiB || audit.RateDenominator != tier.RateDenominator {
		return errors.New("deposit requirement or conviction tier was not reconstructed exactly")
	}
	return nil
}

// verifyDepositAuditShape independently recomputes the demand signal and
// enforces the fail-closed status, deadline and commitment contract.
func verifyDepositAuditShape(audit DepositAudit, artifact *ReleaseMeasurementArtifact) error {
	if artifact == nil || audit.NoID == 0 || audit.Epoch != artifact.SettlementEpoch || audit.Status == "" || audit.Disposition == "" || audit.ObservedAtBlock != artifact.EVMSnapshotBlock || audit.ArtifactDeadlineBlock == 0 {
		return errors.New("deposit audit identity is incomplete")
	}
	observed, err := parseCanonicalDepositAmount("observed deposit", audit.ObservedDepositRao)
	if err != nil {
		return err
	}
	required, err := parseCanonicalDepositAmount("required deposit", audit.RequiredDepositRao)
	if err != nil {
		return err
	}
	conviction, err := parseCanonicalDepositAmount("conviction snapshot", audit.ConvictionBeforeRao)
	if err != nil {
		return err
	}
	usageLag := artifact.Policy.Deposit.UsageLagEpochs
	bootstrap := artifact.SettlementEpoch < usageLag
	wantSource := uint64(0)
	if !bootstrap {
		wantSource = artifact.SettlementEpoch - usageLag
	}
	if audit.SourceEpoch != wantSource {
		return errors.New("deposit audit source epoch does not match policy lag")
	}
	switch audit.Status {
	case DepositAuditCompliant:
		if bootstrap || !audit.Compliant || audit.Disposition != "pool_weight_eligible" || audit.Error != "" || observed.Cmp(required) != 0 {
			return errors.New("compliant deposit audit is inconsistent")
		}
		if err := verifyDepositCommitmentEvidence(audit, conviction, required, artifact); err != nil {
			return err
		}
	case DepositAuditBootstrap:
		if !bootstrap || !audit.Compliant || audit.Disposition != "zero_pool_weight_bootstrap" || audit.Error != "" || observed.Sign() != 0 || required.Sign() != 0 || audit.UsageBytes != 0 || audit.ArtifactHash != "" {
			return errors.New("bootstrap deposit audit disposition is inconsistent")
		}
	case DepositAuditMismatch:
		if audit.Compliant || audit.Disposition != "zero_pool_weight" || audit.Error == "" || observed.Cmp(required) == 0 {
			return errors.New("mismatched deposit audit is inconsistent")
		}
		if !bootstrap {
			if err := verifyDepositCommitmentEvidence(audit, conviction, required, artifact); err != nil {
				return err
			}
		} else if required.Sign() != 0 || audit.UsageBytes != 0 || audit.ArtifactHash != "" {
			return errors.New("bootstrap mismatch contains payout artifact evidence")
		}
	case DepositAuditUnavailablePending:
		if bootstrap || audit.Compliant || audit.Disposition != "zero_pool_weight" || audit.Error == "" || audit.ObservedAtBlock > audit.ArtifactDeadlineBlock {
			return errors.New("pending deposit audit is inconsistent with its deadline")
		}
	case DepositAuditUnavailable:
		if bootstrap || audit.Compliant || audit.Disposition != "zero_pool_weight" || audit.Error == "" || audit.ObservedAtBlock <= audit.ArtifactDeadlineBlock {
			return errors.New("unavailable deposit audit is inconsistent with its deadline")
		}
	case DepositAuditInvalid, DepositAuditEquivocation:
		if bootstrap || audit.Compliant || audit.Disposition != "zero_pool_weight" || audit.Error == "" {
			return errors.New("failed deposit audit is not zero-weight")
		}
	default:
		return fmt.Errorf("unknown deposit audit status %q", audit.Status)
	}
	return nil
}

// releaseMeasurementPools reconstructs every pool's quality, eligibility and
// score while retaining rejected operators as explicit zero-score decisions.
func releaseMeasurementPools(artifact *ReleaseMeasurementArtifact, statsByNO map[uint64]VerifiedReleaseStats, bound map[uint64]map[connect.Id]bool, controlled map[uint64]bool) ([]VerifiedReleasePool, []ExactWeightInput, map[uint16]bool, error) {
	audits := make(map[uint64]DepositAudit, len(artifact.DepositAudits))
	priorNOID := uint64(0)
	for index, audit := range artifact.DepositAudits {
		if index > 0 && audit.NoID <= priorNOID {
			return nil, nil, nil, errors.New("deposit audits are not strictly ordered")
		}
		if err := verifyDepositAuditShape(audit, artifact); err != nil {
			return nil, nil, nil, fmt.Errorf("deposit audit no_id %d: %w", audit.NoID, err)
		}
		audits[audit.NoID] = audit
		priorNOID = audit.NoID
	}
	decisions := make([]VerifiedReleasePool, 0, len(artifact.Pools))
	weights := make([]ExactWeightInput, 0, len(artifact.Pools))
	masked := map[uint16]bool{}
	seenUID := map[uint16]bool{}
	priorNOID = 0
	for index, pool := range artifact.Pools {
		if pool.NoID == 0 || (index > 0 && pool.NoID <= priorNOID) {
			return nil, nil, nil, errors.New("pool observations are not strictly ordered")
		}
		if seenUID[pool.UID] {
			return nil, nil, nil, errors.New("multiple active pools share one native UID")
		}
		seenUID[pool.UID] = true
		if _, err := parseReleaseHex32("pool hotkey", pool.PoolHotkey, false); err != nil {
			return nil, nil, nil, err
		}
		stats, ok := statsByNO[pool.NoID]
		if !ok {
			return nil, nil, nil, fmt.Errorf("active pool no_id %d has no statistics input", pool.NoID)
		}
		audit, ok := audits[pool.NoID]
		if !ok {
			return nil, nil, nil, fmt.Errorf("active pool no_id %d has no deposit audit", pool.NoID)
		}
		qualityPPM, err := ExactPoolQualityFromReleaseStats(stats, bound[pool.NoID])
		if err != nil {
			return nil, nil, nil, fmt.Errorf("pool no_id %d quality: %w", pool.NoID, err)
		}
		decision := VerifiedReleasePool{NoID: pool.NoID, UID: pool.UID, QualityPPM: qualityPPM, Controlled: controlled[pool.NoID], Score: new(big.Rat), Audit: audit}
		if decision.Controlled {
			masked[pool.UID] = true
		} else if audit.Compliant && audit.Status != DepositAuditBootstrap {
			deposit, _ := new(big.Int).SetString(audit.ObservedDepositRao, 10)
			conviction, _ := new(big.Int).SetString(audit.ConvictionBeforeRao, 10)
			score, err := impliedUsageQuality(deposit, conviction, qualityPPM, artifact.Policy)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("pool no_id %d score: %w", pool.NoID, err)
			}
			decision.Eligible = true
			decision.Score = score
			weights = append(weights, ExactWeightInput{UID: pool.UID, Score: new(big.Rat).Set(score)})
		}
		decisions = append(decisions, decision)
		priorNOID = pool.NoID
	}
	if len(artifact.Pools) != len(artifact.DepositAudits) {
		return nil, nil, nil, errors.New("active pool and deposit audit coverage differs")
	}
	return decisions, weights, masked, nil
}

// VerifyReleaseMeasurementArtifact reconstructs every derived release weight
// decision from canonical source observations.
func VerifyReleaseMeasurementArtifact(artifact *ReleaseMeasurementArtifact) (*VerifiedReleaseMeasurement, error) {
	if err := verifyReleaseMeasurementIdentity(artifact); err != nil {
		return nil, err
	}
	controlled := map[uint64]bool{}
	for index, noID := range artifact.ControlledNOIDs {
		if noID == 0 || (index > 0 && noID <= artifact.ControlledNOIDs[index-1]) {
			return nil, errors.New("controlled no_ids are not strictly ordered")
		}
		controlled[noID] = true
	}
	statsByNO, err := releaseMeasurementStats(artifact)
	if err != nil {
		return nil, err
	}
	for noID := range controlled {
		if _, ok := statsByNO[noID]; !ok {
			return nil, fmt.Errorf("controlled no_id %d has no isolated input", noID)
		}
	}
	fleets, bound, membersByUID, stale, err := releaseMeasurementBindings(artifact, statsByNO)
	if err != nil {
		return nil, err
	}
	eligible, selection, err := releaseMeasurementHead(artifact, fleets)
	if err != nil {
		return nil, err
	}
	pools, poolWeights, masked, err := releaseMeasurementPools(artifact, statsByNO, bound, controlled)
	if err != nil {
		return nil, err
	}
	for uid, members := range membersByUID {
		for _, member := range members {
			if controlled[member.NoID] {
				masked[uid] = true
			}
		}
	}
	masked[artifact.SelfUID] = true
	maskedUIDs := make([]uint16, 0, len(masked))
	for uid := range masked {
		maskedUIDs = append(maskedUIDs, uid)
	}
	sort.Slice(maskedUIDs, func(i, j int) bool { return maskedUIDs[i] < maskedUIDs[j] })
	uids, scores, err := BuildWeightVectorExact(poolWeights, selection.Selected, artifact.Policy.Steering.Theta, masked)
	if err != nil {
		return nil, err
	}
	return &VerifiedReleaseMeasurement{
		EligibleHead: eligible, SelectedHead: selection.Selected, RejectedHead: selection.Rejected,
		StaleBindings: stale, Pools: pools, MaskedUIDs: maskedUIDs, UIDs: uids,
		Scores: scores, BoundProviders: bound, StatsByNO: statsByNO,
	}, nil
}

// canonicalReleaseMeasurementBytes is the unique persisted representation.
func canonicalReleaseMeasurementBytes(artifact *ReleaseMeasurementArtifact) ([]byte, error) {
	encoded, err := json.Marshal(artifact)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// ReleaseMeasurementContentHash returns the SHA-256 content address.
func ReleaseMeasurementContentHash(encoded []byte) string {
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// parseReleaseContentHash validates a canonical SHA-256 content address.
func parseReleaseContentHash(encoded string) ([32]byte, error) {
	var value [32]byte
	if encoded != strings.ToLower(encoded) || len(encoded) != 71 || !strings.HasPrefix(encoded, "sha256:") {
		return value, errors.New("content hash is not canonical SHA-256")
	}
	decoded, err := hex.DecodeString(encoded[7:])
	if err != nil || len(decoded) != 32 {
		return value, errors.New("content hash is invalid")
	}
	copy(value[:], decoded)
	return value, nil
}

// SealReleaseMeasurementArtifact verifies and encodes an artifact for durable
// content-addressed storage.
func SealReleaseMeasurementArtifact(artifact *ReleaseMeasurementArtifact) ([]byte, string, *VerifiedReleaseMeasurement, error) {
	verified, err := VerifyReleaseMeasurementArtifact(artifact)
	if err != nil {
		return nil, "", nil, err
	}
	encoded, err := canonicalReleaseMeasurementBytes(artifact)
	if err != nil {
		return nil, "", nil, err
	}
	return encoded, ReleaseMeasurementContentHash(encoded), verified, nil
}

// DecodeReleaseMeasurementArtifact requires strict fields and canonical bytes,
// then performs the full independent reconstruction.
func DecodeReleaseMeasurementArtifact(encoded []byte) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var artifact ReleaseMeasurementArtifact
	if err := decoder.Decode(&artifact); err != nil {
		return nil, nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, nil, errors.New("release measurement contains trailing JSON")
		}
		return nil, nil, err
	}
	canonical, err := canonicalReleaseMeasurementBytes(&artifact)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(encoded, canonical) {
		return nil, nil, errors.New("release measurement bytes are not canonical")
	}
	verified, err := VerifyReleaseMeasurementArtifact(&artifact)
	if err != nil {
		return nil, nil, err
	}
	return &artifact, verified, nil
}

// VerifyReleaseMeasurementIntent requires the signed intent to equal every
// decision independently reconstructed from its content-addressed artifact.
func VerifyReleaseMeasurementIntent(intent *SteeringIntent, artifact *ReleaseMeasurementArtifact, verified *VerifiedReleaseMeasurement) error {
	if intent == nil || artifact == nil || verified == nil {
		return errors.New("measurement intent verification input is nil")
	}
	if intent.ValidatorID != artifact.ValidatorID || intent.Netuid != artifact.Netuid || intent.SubnetEpoch != artifact.SubnetEpoch || intent.NativeSnapshotBlock != artifact.NativeSnapshotBlock || !strings.EqualFold(intent.NativeSnapshotHash, artifact.NativeSnapshotHash) || intent.EVMSnapshotBlock != artifact.EVMSnapshotBlock || !strings.EqualFold(intent.EVMSnapshotHash, artifact.EVMSnapshotHash) || intent.SettlementEpoch != artifact.SettlementEpoch || !strings.EqualFold(intent.PolicyHash, artifact.PolicyHash) || intent.SelfUID != artifact.SelfUID {
		return errors.New("steering intent identity does not match its measurement artifact")
	}
	eligibleUIDs := headSelectionUIDs(verified.EligibleHead)
	eligibleScores := make([]*big.Rat, len(verified.EligibleHead))
	for index := range verified.EligibleHead {
		eligibleScores[index] = verified.EligibleHead[index].Score
	}
	encodedEligible, err := rationalJSON(eligibleScores)
	if err != nil {
		return err
	}
	encodedScores, err := rationalJSON(verified.Scores)
	if err != nil {
		return err
	}
	if !slices.Equal(intent.MaskedUIDs, verified.MaskedUIDs) || !slices.Equal(intent.EligibleHeadUIDs, eligibleUIDs) || !slices.Equal(intent.EligibleHeadScores, encodedEligible) || !slices.Equal(intent.SelectedHeadUIDs, headSelectionUIDs(verified.SelectedHead)) || !slices.Equal(intent.RejectedHeadUIDs, headSelectionUIDs(verified.RejectedHead)) || !slices.Equal(intent.StaleHeadBindings, verified.StaleBindings) || !slices.Equal(intent.DepositAudits, artifact.DepositAudits) || !slices.Equal(intent.UIDs, verified.UIDs) || !slices.Equal(intent.Scores, encodedScores) {
		return errors.New("steering intent decision differs from reconstructed measurement evidence")
	}
	return nil
}

func releasePriorQualityState(stats ReleaseStatsMeasurement) map[string]uint32 {
	state := map[string]uint32{}
	for _, provider := range stats.Providers {
		if provider.HasPriorQuality {
			state[provider.ClientID] = provider.PriorQualityPPM
		}
	}
	return state
}

// Cumulative cuts retain one settlement range and a nonregressing finalized
// view. A same-native-epoch retry may keep the exact records and block hash.
func releaseAttemptCutExtends(earlier, later *AttemptLedgerCut) error {
	if earlier == nil || later == nil || earlier.Identity != later.Identity || earlier.FirstSequence != later.FirstSequence || earlier.PriorRoot != later.PriorRoot || later.LastSequence < earlier.LastSequence || len(later.Records) < len(earlier.Records) || earlier.Boundary.SettlementEpoch != later.Boundary.SettlementEpoch || !releaseBlockAtOrBefore(earlier.Boundary.EVMBlock, earlier.Boundary.EVMBlockHash, later.Boundary.EVMBlock, later.Boundary.EVMBlockHash) {
		return errors.New("attempt cut does not extend its prior signed range")
	}
	for index := range earlier.Records {
		if earlier.Records[index].RecordHash != later.Records[index].RecordHash {
			return errors.New("attempt cut rewrites its prior signed range")
		}
	}
	return nil
}

// VerifyReleaseMeasurementLineage proves consecutive validator-local head EMA
// state. Every still-positive prior identity must appear in the next fold,
// including identities missing from the new raw window so their decay remains
// public rather than being silently reset.
func VerifyReleaseMeasurementLineage(previousEncoded []byte, current *ReleaseMeasurementArtifact) error {
	previous, _, err := DecodeReleaseMeasurementArtifact(previousEncoded)
	if err != nil {
		return fmt.Errorf("previous release measurement: %w", err)
	}
	if current == nil {
		return errors.New("current release measurement is nil")
	}
	if _, err := VerifyReleaseMeasurementArtifact(current); err != nil {
		return fmt.Errorf("current release measurement: %w", err)
	}
	if current.PreviousArtifactHash != ReleaseMeasurementContentHash(previousEncoded) {
		return errors.New("release measurement previous content address differs")
	}
	if current.DeploymentID != previous.DeploymentID || current.ChainID != previous.ChainID || current.ValidatorID != previous.ValidatorID || current.Netuid != previous.Netuid || !strings.EqualFold(current.GenesisHash, previous.GenesisHash) || !strings.EqualFold(current.Coordinator, previous.Coordinator) || !strings.EqualFold(current.SettlementVault, previous.SettlementVault) {
		return errors.New("release measurement identity changed across lineage")
	}
	if current.SubnetEpoch != previous.SubnetEpoch && (previous.SubnetEpoch == ^uint64(0) || current.SubnetEpoch != previous.SubnetEpoch+1) {
		return errors.New("release measurement lineage is not consecutive by native epoch")
	}
	if current.SettlementEpoch < previous.SettlementEpoch || current.SettlementEpoch > previous.SettlementEpoch+1 {
		return errors.New("release measurement lineage is not consecutive by settlement epoch")
	}
	previousInputs := make(map[uint64]ReleaseMeasurementInput, len(previous.Inputs))
	for _, input := range previous.Inputs {
		previousInputs[input.NoID] = input
	}
	if len(previousInputs) != len(current.Inputs) {
		return errors.New("release measurement operator coverage changed across lineage")
	}
	for _, input := range current.Inputs {
		prior, exists := previousInputs[input.NoID]
		if !exists {
			return fmt.Errorf("operator %d is absent from prior measurement", input.NoID)
		}
		if current.SettlementEpoch == previous.SettlementEpoch {
			if !maps.Equal(releasePriorQualityState(prior.Stats), releasePriorQualityState(input.Stats)) {
				return fmt.Errorf("operator %d pool EMA prior state changed within a settlement epoch", input.NoID)
			}
			if err := releaseAttemptCutExtends(prior.Stats.AttemptCut, input.Stats.AttemptCut); err != nil {
				return fmt.Errorf("operator %d: %w", input.NoID, err)
			}
			continue
		}
		transition := input.Stats.SettlementTransition
		if transition == nil || transition.FromBoundary.SettlementEpoch != previous.SettlementEpoch || transition.ToEpoch != current.SettlementEpoch {
			return fmt.Errorf("operator %d omits its settlement transition", input.NoID)
		}
		if !maps.Equal(releasePriorQualityState(prior.Stats), releasePriorQualityState(transition.PreFold)) {
			return fmt.Errorf("operator %d settlement transition changed its prior pool EMA", input.NoID)
		}
		if err := releaseAttemptCutExtends(prior.Stats.AttemptCut, transition.PreFold.AttemptCut); err != nil {
			return fmt.Errorf("operator %d terminal settlement cut: %w", input.NoID, err)
		}
		terminal := transition.PreFold.AttemptCut
		currentCut := input.Stats.AttemptCut
		if currentCut.FirstSequence != terminal.LastSequence+1 || currentCut.PriorRoot != terminal.Root {
			return fmt.Errorf("operator %d current settlement cut is not rooted in its transition", input.NoID)
		}
	}
	priorNext := make(map[string]RationalJSON, len(previous.HeadEMA))
	if current.SubnetEpoch == previous.SubnetEpoch {
		// A failed submission may be rebuilt inside the same native epoch. It
		// must recompute from the same pre-epoch EMA base, not fold the failed
		// artifact's output a second time. Raw evidence may legitimately change
		// while the retry is prepared.
		for _, record := range previous.HeadEMA {
			if record.HasPrior {
				priorNext[record.Key.String()] = record.Prior
			}
		}
	} else {
		for _, record := range previous.HeadEMA {
			next, decodeErr := decodeRationalJSON(record.Next)
			if decodeErr != nil {
				return decodeErr
			}
			if next.Sign() > 0 {
				priorNext[record.Key.String()] = record.Next
			}
		}
	}
	seenPrior := map[string]bool{}
	for _, record := range current.HeadEMA {
		key := record.Key.String()
		prior, existed := priorNext[key]
		if record.HasPrior != existed || (existed && record.Prior != prior) {
			return fmt.Errorf("head EMA prior state changed for %s", key)
		}
		if existed {
			seenPrior[key] = true
		}
	}
	if len(seenPrior) != len(priorNext) {
		return errors.New("release measurement lineage omits prior head EMA state")
	}
	return nil
}

// canonicalReleaseMeasurementInputBytes is the unique private journal form.
func canonicalReleaseMeasurementInputBytes(journal *releaseMeasurementInputJournal) ([]byte, error) {
	encoded, err := json.Marshal(journal)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// releaseMeasurementInputPath deterministically separates native epochs and
// operator contexts without accepting caller-controlled path components.
func releaseMeasurementInputPath(stateDir string, subnetEpoch, noID uint64) string {
	return filepath.Join(stateDir, "measurements", "inputs", fmt.Sprintf("subnet-%020d-no-%020d.json", subnetEpoch, noID))
}

// decodeReleaseMeasurementInput requires canonical, private regular-file data.
func decodeReleaseMeasurementInput(encoded []byte) (*releaseMeasurementInputJournal, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var journal releaseMeasurementInputJournal
	if err := decoder.Decode(&journal); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("measurement input journal contains trailing JSON")
		}
		return nil, err
	}
	canonical, err := canonicalReleaseMeasurementInputBytes(&journal)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(encoded, canonical) {
		return nil, errors.New("measurement input journal bytes are not canonical")
	}
	if journal.Schema != releaseMeasurementInputSchema || journal.MeasurementInput.NoID == 0 || journal.MeasurementInput.CutNativeBlock == 0 || journal.MeasurementInput.CutEVMSnapshotBlock == 0 {
		return nil, errors.New("measurement input journal identity is incomplete")
	}
	if _, err := parseReleaseHex32("measurement input native hash", journal.MeasurementInput.CutNativeBlockHash, false); err != nil {
		return nil, err
	}
	if _, err := parseReleaseHex32("measurement input EVM hash", journal.MeasurementInput.CutEVMSnapshotHash, false); err != nil {
		return nil, err
	}
	if _, err := VerifyReleaseStatsMeasurement(journal.MeasurementInput.Stats); err != nil {
		return nil, err
	}
	return &journal, nil
}

// loadOrDetachReleaseMeasurementInput recovers an existing epoch cut or
// persists a new one before its egress map is rotated. Same-epoch retries and
// process restarts therefore consume identical inputs.
func (s *ReleaseSteerer) loadOrDetachReleaseMeasurementInput(noID, subnetEpoch, nativeBlock uint64, nativeHash string, snapshot *ReleaseSnapshot) (ReleaseMeasurementInput, error) {
	measurement := s.contexts[noID]
	if measurement == nil || measurement.Stats == nil || snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() {
		return ReleaseMeasurementInput{}, fmt.Errorf("no_id %d measurement context is absent", noID)
	}
	path := releaseMeasurementInputPath(s.cfg.StateDir, subnetEpoch, noID)
	encoded, err := os.ReadFile(path)
	if err == nil {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return ReleaseMeasurementInput{}, fmt.Errorf("measurement input journal %s is not a private regular file", path)
		}
		journal, decodeErr := decodeReleaseMeasurementInput(encoded)
		if decodeErr != nil {
			return ReleaseMeasurementInput{}, fmt.Errorf("decode measurement input journal %s: %w", path, decodeErr)
		}
		input := journal.MeasurementInput
		if journal.DeploymentID != s.cfg.DeploymentID || journal.ChainID != s.cfg.ChainID || !strings.EqualFold(journal.GenesisHash, s.cfg.GenesisHash) || !strings.EqualFold(journal.Coordinator, s.cfg.Coordinator) || journal.ValidatorID != s.cfg.ValidatorID || journal.Netuid != s.cfg.Netuid || journal.SubnetEpoch != subnetEpoch || !strings.EqualFold(journal.PolicyHash, s.cfg.PolicyHash) || input.NoID != noID || input.SettlementEpoch != snapshot.Epoch.Uint64() || !releaseBlockAtOrBefore(input.CutNativeBlock, input.CutNativeBlockHash, nativeBlock, strings.ToLower(nativeHash)) || !releaseBlockAtOrBefore(input.CutEVMSnapshotBlock, input.CutEVMSnapshotHash, snapshot.BlockNumber, releaseHex32(snapshot.BlockHash)) {
			return ReleaseMeasurementInput{}, errors.New("measurement input journal does not match the active release decision")
		}
		if err := measurement.Stats.reconcileReleaseStatsCut(measurementStateDir(s.cfg, noID), input.EgressGeneration, input.Stats.AttemptCut); err != nil {
			return ReleaseMeasurementInput{}, fmt.Errorf("reconcile measurement input egress cut: %w", err)
		}
		return input, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ReleaseMeasurementInput{}, err
	}
	input := ReleaseMeasurementInput{
		NoID: noID, SettlementEpoch: snapshot.Epoch.Uint64(),
		CutNativeBlock: nativeBlock, CutNativeBlockHash: strings.ToLower(nativeHash),
		CutEVMSnapshotBlock: snapshot.BlockNumber, CutEVMSnapshotHash: releaseHex32(snapshot.BlockHash),
	}
	boundary := AttemptBoundary{
		SettlementEpoch: snapshot.Epoch.Uint64(),
		EVMBlock:        snapshot.BlockNumber,
		EVMBlockHash:    releaseHex32(snapshot.BlockHash),
	}
	_, err = measurement.Stats.detachReleaseStatsMeasurementWithAttemptCut(measurementStateDir(s.cfg, noID), boundary, func(stats ReleaseStatsMeasurement, generation uint64) error {
		input.Stats = stats
		input.EgressGeneration = generation
		journal := releaseMeasurementInputJournal{
			Schema: releaseMeasurementInputSchema, DeploymentID: s.cfg.DeploymentID,
			ChainID: s.cfg.ChainID, GenesisHash: strings.ToLower(s.cfg.GenesisHash),
			Coordinator: s.cfg.Coordinator, ValidatorID: s.cfg.ValidatorID, Netuid: s.cfg.Netuid,
			SubnetEpoch: subnetEpoch, PolicyHash: strings.ToLower(s.cfg.PolicyHash), MeasurementInput: input,
		}
		journalBytes, encodeErr := canonicalReleaseMeasurementInputBytes(&journal)
		if encodeErr != nil {
			return encodeErr
		}
		return atomicStateWrite(path, journalBytes, 0o600)
	})
	if err != nil {
		return ReleaseMeasurementInput{}, err
	}
	return input, nil
}

// persistReleaseMeasurementArtifact stores immutable content-addressed bytes
// and returns the validator-state-relative locator bound by the intent.
func persistReleaseMeasurementArtifact(stateDir string, encoded []byte, contentHash string) (string, uint64, error) {
	if _, err := parseReleaseContentHash(contentHash); err != nil {
		return "", 0, err
	}
	relativePath := filepath.ToSlash(filepath.Join("measurements", strings.TrimPrefix(contentHash, "sha256:")+".json"))
	absolutePath := filepath.Join(stateDir, filepath.FromSlash(relativePath))
	if existing, err := os.ReadFile(absolutePath); err == nil {
		info, statErr := os.Lstat(absolutePath)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || !bytes.Equal(existing, encoded) {
			return "", 0, errors.New("existing measurement content address is not the expected private regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", 0, err
	} else if err := atomicStateWrite(absolutePath, encoded, 0o600); err != nil {
		return "", 0, err
	}
	return relativePath, uint64(len(encoded)), nil
}
