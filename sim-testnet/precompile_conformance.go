package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gsrpcTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/urfoundation/sn/crv4"
)

// PrecompileConformanceEvidence is the durable M0B record. Each mutating
// phase is one independently journaled action/transaction; dynamic alpha
// amounts are derived from the prior finalized observation and never exceed
// the single approved TAO input.
type PrecompileConformanceEvidence struct {
	Schema          string `json:"schema"`
	DeploymentID    string `json:"deployment_id"`
	ConfigHash      string `json:"config_hash"`
	PolicyHash      string `json:"policy_hash"`
	ChainID         uint64 `json:"chain_id"`
	GenesisHash     string `json:"genesis_hash"`
	Netuid          uint16 `json:"netuid"`
	ProbeAddress    string `json:"probe_address"`
	ProbeColdkey    string `json:"probe_coldkey"`
	Owner           string `json:"owner"`
	SampleHotkey    string `json:"sample_hotkey"`
	SampleUID       uint16 `json:"sample_uid"`
	AbsentHotkey    string `json:"absent_hotkey"`
	MoveHotkey      string `json:"move_hotkey"`
	RecoveryColdkey string `json:"recovery_provider_coldkey"`

	Commitment PrecompileCommitmentEvidence `json:"commitment"`
	Battery    PrecompileBatteryEvidence    `json:"battery"`
	Seed       PrecompileValueStep          `json:"seed"`
	Forward    PrecompileMoveStep           `json:"move_forward"`
	Back       PrecompileMoveStep           `json:"move_back"`
	Snapshot   PrecompileSnapshotStep       `json:"snapshot"`
	Dividend   PrecompileDividendStep       `json:"dividend"`
	Transfer   PrecompileTransferStep       `json:"transfer_out"`

	Complete     bool   `json:"complete"`
	EvidenceHash string `json:"evidence_hash"`
}

type PrecompileCommitmentEvidence struct {
	ProbeHash              string    `json:"probe_hash"`
	CanonicalHash          string    `json:"canonical_hash"`
	CanonicalGeneration    uint64    `json:"canonical_generation"`
	EncodedProbeBytes      int       `json:"encoded_probe_bytes"`
	WriteTransactionHash   string    `json:"write_transaction_hash"`
	WriteFinalizedHead     ChainHead `json:"write_finalized_head"`
	WriteCommitmentBlock   uint64    `json:"write_commitment_block"`
	RestoreTransactionHash string    `json:"restore_transaction_hash"`
	RestoreFinalizedHead   ChainHead `json:"restore_finalized_head"`
	RestoreCommitmentBlock uint64    `json:"restore_commitment_block"`
	Restored               bool      `json:"restored"`
}

const precompileCanonicalFleetGeneration uint64 = 2

type PrecompileBatteryEvidence struct {
	FinalizedHead      ChainHead `json:"finalized_head"`
	BlakeOK            bool      `json:"blake_ok"`
	MirrorKAT          string    `json:"mirror_kat"`
	MirrorKATMatch     bool      `json:"mirror_kat_match"`
	SelfColdkey        string    `json:"self_coldkey"`
	Ed25519OK          bool      `json:"ed25519_ok"`
	Ed25519Good        bool      `json:"ed25519_good"`
	Ed25519BadRejected bool      `json:"ed25519_bad_rejected"`
	Sr25519OK          bool      `json:"sr25519_ok"`
	Sr25519Good        bool      `json:"sr25519_good"`
	Sr25519BadRejected bool      `json:"sr25519_bad_rejected"`
	MetagraphOK        bool      `json:"metagraph_ok"`
	UIDCount           uint16    `json:"uid_count"`
	UID0Hotkey         string    `json:"uid0_hotkey"`
	UID0Coldkey        string    `json:"uid0_coldkey"`
	NeuronOK           bool      `json:"neuron_ok"`
	SampleExists       bool      `json:"sample_exists"`
	ObservedSampleUID  uint16    `json:"observed_sample_uid"`
	AbsentRejected     bool      `json:"absent_rejected"`
	StakeViewOK        bool      `json:"stake_view_ok"`
	SampleSelfStake    string    `json:"sample_self_stake_rao"`
	NominatorMinimum   string    `json:"nominator_minimum_rao"`
	Passed             bool      `json:"passed"`
}

type PrecompileValueStep struct {
	TransactionHash string `json:"transaction_hash"`
	BlockNumber     uint64 `json:"block_number"`
	BlockHash       string `json:"block_hash"`
	TAORao          uint64 `json:"tao_rao"`
	ValueWei        string `json:"value_wei"`
	BeforeRao       uint64 `json:"before_alpha_rao"`
	AfterRao        uint64 `json:"after_alpha_rao"`
	DeltaRao        uint64 `json:"delta_alpha_rao"`
}

type PrecompileMoveStep struct {
	TransactionHash string `json:"transaction_hash"`
	BlockNumber     uint64 `json:"block_number"`
	BlockHash       string `json:"block_hash"`
	AmountRao       uint64 `json:"amount_rao"`
	FromBeforeRao   uint64 `json:"from_before_rao"`
	FromAfterRao    uint64 `json:"from_after_rao"`
	ToBeforeRao     uint64 `json:"to_before_rao"`
	ToAfterRao      uint64 `json:"to_after_rao"`
}

type PrecompileSnapshotStep struct {
	TransactionHash string `json:"transaction_hash"`
	BlockNumber     uint64 `json:"block_number"`
	BlockHash       string `json:"block_hash"`
	BaselineRao     uint64 `json:"baseline_rao"`
	SinceBlock      uint64 `json:"since_block"`
}

type PrecompileDividendStep struct {
	FinalizedHead ChainHead `json:"finalized_head"`
	BaselineRao   uint64    `json:"baseline_rao"`
	CurrentRao    uint64    `json:"current_rao"`
	DeltaRao      uint64    `json:"delta_rao"`
	SinceBlock    uint64    `json:"since_block"`
}

type PrecompileTransferStep struct {
	TransactionHash   string `json:"transaction_hash"`
	BlockNumber       uint64 `json:"block_number"`
	BlockHash         string `json:"block_hash"`
	AmountRao         uint64 `json:"amount_rao"`
	ProbeBeforeRao    uint64 `json:"probe_before_rao"`
	ProbeAfterRao     uint64 `json:"probe_after_rao"`
	ProviderBeforeRao uint64 `json:"provider_before_rao"`
	ProviderAfterRao  uint64 `json:"provider_after_rao"`
}

// ABI tuple shape returned by STSubnetProbe.readBattery. Field order and
// names are checked by abi.ConvertType against the generated reviewed ABI.
type precompileBatteryTuple struct {
	BlakeOk          bool
	MirrorKat        [32]byte
	BlakeKatMatch    bool
	SelfColdkey      [32]byte
	EdOk             bool
	EdVerifyGood     bool
	EdVerifyBad      bool
	SrOk             bool
	SrVerifyGood     bool
	SrVerifyBad      bool
	MgOk             bool
	UidCount         uint16
	Uid0Hotkey       [32]byte
	Uid0Coldkey      [32]byte
	NeuronOk         bool
	SampleExists     bool
	SampleUid        uint16
	AbsentRejected   bool
	StakeViewOk      bool
	SampleSelfStake  *big.Int
	NominatorMinimum *big.Int
}

func precompileEvidencePath(stateDir string) string {
	return filepath.Join(stateDir, "public", "precompile-conformance.json")
}

func writePrecompileEvidence(stateDir string, evidence *PrecompileConformanceEvidence) error {
	evidence.EvidenceHash = ""
	hash, err := canonicalHashHex(evidence)
	if err != nil {
		return err
	}
	evidence.EvidenceHash = hash
	return writePublicJSON(precompileEvidencePath(stateDir), evidence)
}

func loadPrecompileEvidence(stateDir string) (*PrecompileConformanceEvidence, error) {
	var evidence PrecompileConformanceEvidence
	if err := readJSONFile(precompileEvidencePath(stateDir), &evidence); err != nil {
		return nil, err
	}
	want := evidence.EvidenceHash
	evidence.EvidenceHash = ""
	got, err := canonicalHashHex(&evidence)
	evidence.EvidenceHash = want
	if err != nil || want == "" || got != want {
		return nil, stateMismatchError(err, "precompile evidence hash mismatch: got %s want %s", got, want)
	}
	return &evidence, nil
}

func receiptFields(receipt *ethTypes.Receipt) (string, uint64, string) {
	if receipt == nil || receipt.BlockNumber == nil {
		return "", 0, ""
	}
	return receipt.TxHash.Hex(), receipt.BlockNumber.Uint64(), receipt.BlockHash.Hex()
}

func conformanceMismatch(message string, cause error) error {
	if cause != nil {
		return fmt.Errorf("%s: %w", message, cause)
	}
	return errors.New(message)
}

func (e *Executor) precompileABI() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(SubnetProbeABI))
}

func (e *Executor) precompileIdentities(ctx context.Context) (*PrecompileConformanceEvidence, [32]byte, [32]byte, [32]byte, error) {
	var sample, move, recovery [32]byte
	if err := e.ensurePayloads(ctx); err != nil {
		return nil, sample, move, recovery, err
	}
	var err error
	if sample, err = roleBytes32(e.roles, validatorHotkeyLabel(1)); err != nil {
		return nil, sample, move, recovery, err
	}
	if move, err = roleBytes32(e.roles, validatorHotkeyLabel(2)); err != nil {
		return nil, sample, move, recovery, err
	}
	if recovery, err = roleBytes32(e.roles, fleetColdkeyLabel(1)); err != nil {
		return nil, sample, move, recovery, err
	}
	uid, found, err := e.substrate.UID(sample)
	if err != nil || !found {
		return nil, sample, move, recovery, stateMismatchError(err, "precompile sample hotkey has no finalized UID")
	}
	absent := derive32(e.cfg, "precompile/absent-hotkey")
	if _, found, err := e.substrate.UID(absent); err != nil || found {
		return nil, sample, move, recovery, stateMismatchError(err, "precompile absent-hotkey control is registered or unreadable: found=%t", found)
	}
	deployer, err := e.roles.EVMAddress("deployer")
	if err != nil {
		return nil, sample, move, recovery, err
	}
	probeColdkey := ss58Mirror(e.payloads.Manifest.PrecompileProbe)
	evidence := &PrecompileConformanceEvidence{
		Schema: "urnetwork-precompile-conformance-v1", DeploymentID: e.cfg.Config.Deployment.DeploymentID,
		ConfigHash: e.cfg.ConfigHash, PolicyHash: e.cfg.PolicyHash, ChainID: testnetChainID,
		GenesisHash: testnetGenesis, Netuid: e.cfg.Netuid, ProbeAddress: e.payloads.Manifest.PrecompileProbe.Hex(),
		ProbeColdkey: hexBytesValue(probeColdkey[:]), Owner: deployer.Hex(), SampleHotkey: hexBytesValue(sample[:]), SampleUID: uid,
		AbsentHotkey: hexBytesValue(absent[:]), MoveHotkey: hexBytesValue(move[:]), RecoveryColdkey: hexBytesValue(recovery[:]),
	}
	return evidence, sample, move, recovery, nil
}

func (e *Executor) executePrecompileConformance(ctx context.Context, action Action) error {
	parsed, err := e.precompileABI()
	if err != nil {
		return err
	}
	identity, sample, move, recovery, err := e.precompileIdentities(ctx)
	if err != nil {
		return err
	}
	evidence, err := loadPrecompileEvidence(e.stateDir)
	if errors.Is(err, os.ErrNotExist) {
		evidence = identity
	} else if err != nil {
		return err
	} else if err := validatePrecompileEvidenceIdentity(e.cfg, &e.payloads.Manifest, evidence); err != nil {
		return err
	}
	probe := e.payloads.Manifest.PrecompileProbe
	probeColdkey := ss58Mirror(probe)

	switch action.ID {
	case "precompile.commitment-write", "precompile.commitment-restore":
		if action.Parameters["canonical_generation"] != strconv.FormatUint(precompileCanonicalFleetGeneration, 10) {
			return errors.New("precompile commitment action does not bind generation 2")
		}
		_, canonical, canonicalEvidence, _, err := e.validatedFleetCommitmentGeneration(1, precompileCanonicalFleetGeneration)
		if err != nil {
			return err
		}
		probeHash := derive32(e.cfg, "precompile/commitment-replace")
		encoded, err := crv4.EncodeFleetCommitmentInfo(probeHash)
		if err != nil || len(encoded) != 34 {
			return conformanceMismatch(fmt.Sprintf("precompile commitment encoding length=%d, want 34", len(encoded)), err)
		}
		evidence.Commitment.ProbeHash = hexBytesValue(probeHash[:])
		evidence.Commitment.CanonicalHash = hexBytesValue(canonical[:])
		evidence.Commitment.CanonicalGeneration = precompileCanonicalFleetGeneration
		evidence.Commitment.EncodedProbeBytes = len(encoded)
		hotkey, err := roleBytes32(e.roles, fleetHotkeyLabel(1))
		if err != nil {
			return err
		}
		signer, err := e.substrate.RoleSigner(e.roles, fleetHotkeyLabel(1))
		if err != nil {
			return err
		}
		if action.ID == "precompile.commitment-write" {
			if _, prior := e.journal.LatestTransaction(e.plan.PlanHash, action.ID, action.IntentHash); !prior {
				current, readErr := e.substrate.fleetCommitmentFinalized(hotkey)
				if readErr != nil || current.Hash != canonical || current.CommitmentBlock != canonicalEvidence.CommitmentBlock {
					return conformanceMismatch("canonical fleet commitment is unavailable before replacement", readErr)
				}
			}
			call, err := e.substrate.chain.NewSetFleetCommitmentCall(e.cfg.Netuid, probeHash)
			if err != nil {
				return err
			}
			txHash, transactionBlock, err := e.substrate.SendAs(ctx, e.plan.PlanHash, action, call, signer)
			if err != nil {
				return err
			}
			transactionBlockHash, err := e.substrate.chain.API.RPC.Chain.GetBlockHash(transactionBlock)
			if err != nil {
				return err
			}
			observed, err := e.substrate.fleetCommitmentAt(hotkey, transactionBlockHash)
			if err != nil {
				return conformanceMismatch("replacement commitment finalized mismatch", err)
			}
			if err := crv4.ValidateFleetCommitmentWrite(probeHash, transactionBlock, observed); err != nil {
				return conformanceMismatch("replacement commitment finalized mismatch", err)
			}
			evidence.Commitment.WriteTransactionHash = txHash.Hex()
			evidence.Commitment.WriteFinalizedHead = ChainHead{Number: observed.FinalizedAt, Hash: observed.FinalizedHash.Hex()}
			evidence.Commitment.WriteCommitmentBlock = observed.CommitmentBlock
			return writePrecompileEvidence(e.stateDir, evidence)
		}
		if _, prior := e.journal.LatestTransaction(e.plan.PlanHash, action.ID, action.IntentHash); !prior {
			current, readErr := e.substrate.fleetCommitmentFinalized(hotkey)
			if readErr != nil || current.Hash != probeHash || evidence.Commitment.WriteTransactionHash == "" {
				return conformanceMismatch("replacement commitment is unavailable before restore", readErr)
			}
		}
		call, err := e.substrate.chain.NewSetFleetCommitmentCall(e.cfg.Netuid, canonical)
		if err != nil {
			return err
		}
		txHash, transactionBlock, err := e.substrate.SendAs(ctx, e.plan.PlanHash, action, call, signer)
		if err != nil {
			return err
		}
		transactionBlockHash, err := e.substrate.chain.API.RPC.Chain.GetBlockHash(transactionBlock)
		if err != nil {
			return err
		}
		observed, err := e.substrate.fleetCommitmentAt(hotkey, transactionBlockHash)
		if err != nil {
			return conformanceMismatch("canonical commitment restore finalized mismatch", err)
		}
		if err := crv4.ValidateFleetCommitmentWrite(canonical, transactionBlock, observed); err != nil {
			return conformanceMismatch("canonical commitment restore finalized mismatch", err)
		}
		if observed.CommitmentBlock <= evidence.Commitment.WriteCommitmentBlock {
			return conformanceMismatch("canonical commitment restore did not advance the registration block", nil)
		}
		evidence.Commitment.RestoreTransactionHash = txHash.Hex()
		evidence.Commitment.RestoreFinalizedHead = ChainHead{Number: observed.FinalizedAt, Hash: observed.FinalizedHash.Hex()}
		evidence.Commitment.RestoreCommitmentBlock = observed.CommitmentBlock
		evidence.Commitment.Restored = true
		return writePrecompileEvidence(e.stateDir, evidence)

	case "precompile.read-battery":
		absent, err := decodeHex32("absent hotkey", evidence.AbsentHotkey)
		if err != nil {
			return err
		}
		head, err := finalizedEVMHead(ctx, e.deployer.client)
		if err != nil {
			return err
		}
		values, err := contractCallAt(ctx, e.deployer.client, probe, parsed, "readBattery", head.Number, sample, absent)
		if err != nil || len(values) != 1 {
			return conformanceMismatch(fmt.Sprintf("read precompile battery returned %d values, want 1", len(values)), err)
		}
		converted, ok := abi.ConvertType(values[0], new(precompileBatteryTuple)).(*precompileBatteryTuple)
		if !ok || converted == nil || converted.SampleSelfStake == nil || converted.NominatorMinimum == nil || !converted.SampleSelfStake.IsUint64() || !converted.NominatorMinimum.IsUint64() {
			return fmt.Errorf("readBattery returned incompatible tuple %T", values[0])
		}
		nativeUID, found, err := e.substrate.UID(sample)
		passed := err == nil && found && nativeUID == converted.SampleUid && converted.BlakeOk && converted.BlakeKatMatch && converted.SelfColdkey == probeColdkey && converted.EdOk && converted.EdVerifyGood && converted.EdVerifyBad && converted.SrOk && converted.SrVerifyGood && converted.SrVerifyBad && converted.MgOk && converted.UidCount > 0 && converted.Uid0Hotkey != ([32]byte{}) && converted.Uid0Coldkey != ([32]byte{}) && converted.NeuronOk && converted.SampleExists && converted.AbsentRejected && converted.StakeViewOk && converted.NominatorMinimum.Uint64() == e.plan.LiveFacts.NominatorMinimumRao
		evidence.Battery = PrecompileBatteryEvidence{
			FinalizedHead: head, BlakeOK: converted.BlakeOk, MirrorKAT: hexBytesValue(converted.MirrorKat[:]), MirrorKATMatch: converted.BlakeKatMatch,
			SelfColdkey: hexBytesValue(converted.SelfColdkey[:]), Ed25519OK: converted.EdOk, Ed25519Good: converted.EdVerifyGood,
			Ed25519BadRejected: converted.EdVerifyBad, Sr25519OK: converted.SrOk, Sr25519Good: converted.SrVerifyGood,
			Sr25519BadRejected: converted.SrVerifyBad, MetagraphOK: converted.MgOk, UIDCount: converted.UidCount,
			UID0Hotkey: hexBytesValue(converted.Uid0Hotkey[:]), UID0Coldkey: hexBytesValue(converted.Uid0Coldkey[:]), NeuronOK: converted.NeuronOk,
			SampleExists: converted.SampleExists, ObservedSampleUID: converted.SampleUid, AbsentRejected: converted.AbsentRejected,
			StakeViewOK: converted.StakeViewOk, SampleSelfStake: converted.SampleSelfStake.String(), NominatorMinimum: converted.NominatorMinimum.String(), Passed: passed,
		}
		if !passed {
			_ = writePrecompileEvidence(e.stateDir, evidence)
			return errors.New("runtime-453 precompile battery failed closed")
		}
		return writePrecompileEvidence(e.stateDir, evidence)

	case "precompile.seed":
		if evidence.Seed.TAORao == 0 {
			before, err := e.readStakeFinalized(ctx, sample, probeColdkey)
			if err != nil {
				return err
			}
			if before != 0 {
				return fmt.Errorf("fresh disposable probe already owns %d alpha rao", before)
			}
			evidence.Seed.TAORao = e.plan.LiveFacts.ProbeTAORao
			evidence.Seed.ValueWei = new(big.Int).Mul(new(big.Int).SetUint64(evidence.Seed.TAORao), big.NewInt(1_000_000_000)).String()
			evidence.Seed.BeforeRao = before
			if err := writePrecompileEvidence(e.stateDir, evidence); err != nil {
				return err
			}
		}
		value, ok := new(big.Int).SetString(evidence.Seed.ValueWei, 10)
		if !ok || evidence.Seed.TAORao != e.plan.LiveFacts.ProbeTAORao || value.Cmp(new(big.Int).Mul(new(big.Int).SetUint64(evidence.Seed.TAORao), big.NewInt(1_000_000_000))) != 0 {
			return errors.New("precompile seed intent does not match the approved TAO/EVM unit conversion")
		}
		data, err := parsed.Pack("seedFromTao", sample, new(big.Int).SetUint64(evidence.Seed.TAORao))
		if err != nil {
			return err
		}
		receipt, err := e.deployer.Send(ctx, e.plan.PlanHash, action, &probe, value, data)
		if err != nil {
			return err
		}
		seedEvent, err := conformanceEventValues(parsed, "Seeded", receipt, sample)
		if err != nil {
			return err
		}
		amountArg, amountOK := conformanceEventUint64(seedEvent, "amountArg")
		valueSent, valueOK := conformanceEventBigInt(seedEvent, "valueSent")
		after, afterOK := conformanceEventUint64(seedEvent, "stakeAfter")
		if !amountOK || !valueOK || !afterOK || amountArg != evidence.Seed.TAORao || valueSent.Cmp(value) != 0 || after <= evidence.Seed.BeforeRao {
			return fmt.Errorf("probe Seeded event is inconsistent: amount=%d value=%v after=%d", amountArg, valueSent, after)
		}
		evidence.Seed.AfterRao = after
		evidence.Seed.DeltaRao = after - evidence.Seed.BeforeRao
		evidence.Seed.TransactionHash, evidence.Seed.BlockNumber, evidence.Seed.BlockHash = receiptFields(receipt)
		return writePrecompileEvidence(e.stateDir, evidence)

	case "precompile.move-forward":
		if evidence.Seed.DeltaRao < 2 {
			return errors.New("probe seed alpha is too small for a round trip")
		}
		return e.executePrecompileMove(ctx, action, parsed, evidence, sample, move, &evidence.Forward, evidence.Seed.DeltaRao/2)

	case "precompile.move-back":
		if evidence.Forward.AmountRao == 0 {
			return errors.New("forward move evidence is unavailable")
		}
		return e.executePrecompileMove(ctx, action, parsed, evidence, move, sample, &evidence.Back, evidence.Forward.AmountRao)

	case "precompile.snapshot":
		if evidence.Snapshot.BaselineRao == 0 {
			before, err := e.readStakeFinalized(ctx, sample, probeColdkey)
			if err != nil || before < evidence.Seed.BeforeRao+evidence.Seed.DeltaRao {
				return conformanceMismatch(fmt.Sprintf("round-trip stake not restored: stake=%d", before), err)
			}
			evidence.Snapshot.BaselineRao = before
			if err := writePrecompileEvidence(e.stateDir, evidence); err != nil {
				return err
			}
		}
		data, err := parsed.Pack("snapshot", sample)
		if err != nil {
			return err
		}
		receipt, err := e.deployer.Send(ctx, e.plan.PlanHash, action, &probe, big.NewInt(0), data)
		if err != nil {
			return err
		}
		snapshotEvent, err := conformanceEventValues(parsed, "DividendSnapshot", receipt, sample)
		if err != nil {
			return err
		}
		baseline, baselineOK := conformanceEventUint64(snapshotEvent, "baseline")
		since, sinceOK := conformanceEventUint64(snapshotEvent, "blockNumber")
		if !baselineOK || !sinceOK || baseline != evidence.Snapshot.BaselineRao || since == 0 {
			return fmt.Errorf("invalid DividendSnapshot event baseline=%d since=%d", baseline, since)
		}
		evidence.Snapshot.SinceBlock = since
		evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockNumber, evidence.Snapshot.BlockHash = receiptFields(receipt)
		return writePrecompileEvidence(e.stateDir, evidence)

	case "precompile.dividend":
		if evidence.Snapshot.SinceBlock == 0 {
			return errors.New("precompile dividend snapshot is unavailable")
		}
		tempoValue, err := e.substrate.ReadHyper("tempo")
		if err != nil {
			return err
		}
		tempo, ok := numericUint64(tempoValue)
		if !ok || tempo == 0 {
			return fmt.Errorf("invalid finalized tempo %v", tempoValue)
		}
		deadline := time.Now().Add(time.Duration((tempo*2+20)*e.cfg.Public.Chain.ExpectedBlockSeconds) * time.Second)
		for {
			baseline, current, since, readErr := readDividendAtFinalized(ctx, e.deployer.client, probe, parsed, sample)
			head, headErr := finalizedEVMHead(ctx, e.deployer.client)
			if readErr == nil && headErr == nil && baseline == evidence.Snapshot.BaselineRao && since == evidence.Snapshot.SinceBlock && head.Number >= since+tempo && current > baseline {
				evidence.Dividend = PrecompileDividendStep{FinalizedHead: head, BaselineRao: baseline, CurrentRao: current, DeltaRao: current - baseline, SinceBlock: since}
				return writePrecompileEvidence(e.stateDir, evidence)
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("no finalized probe dividend after two tempo windows: baseline=%d current=%d read_err=%v head_err=%v", baseline, current, readErr, headErr)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}

	case "precompile.transfer-out":
		if evidence.Transfer.AmountRao == 0 {
			probeBefore, err := e.readStakeFinalized(ctx, sample, probeColdkey)
			if err != nil || probeBefore <= evidence.Seed.BeforeRao {
				return conformanceMismatch(fmt.Sprintf("no probe-attributable alpha to recover: stake=%d baseline=%d", probeBefore, evidence.Seed.BeforeRao), err)
			}
			providerBefore, err := e.readStakeFinalized(ctx, sample, recovery)
			if err != nil {
				return err
			}
			evidence.Transfer.AmountRao = probeBefore - evidence.Seed.BeforeRao
			evidence.Transfer.ProbeBeforeRao = probeBefore
			evidence.Transfer.ProviderBeforeRao = providerBefore
			if err := writePrecompileEvidence(e.stateDir, evidence); err != nil {
				return err
			}
		}
		amount := evidence.Transfer.AmountRao
		data, err := parsed.Pack("transferOut", recovery, sample, new(big.Int).SetUint64(amount))
		if err != nil {
			return err
		}
		receipt, err := e.deployer.Send(ctx, e.plan.PlanHash, action, &probe, big.NewInt(0), data)
		if err != nil {
			return err
		}
		transferEvent, err := conformanceEventValues(parsed, "TransferredOut", receipt, recovery, sample)
		if err != nil {
			return err
		}
		eventAmount, amountOK := conformanceEventUint64(transferEvent, "amount")
		probeBefore, probeBeforeOK := conformanceEventUint64(transferEvent, "sourceBefore")
		probeAfter, probeAfterOK := conformanceEventUint64(transferEvent, "sourceAfter")
		providerBefore, providerBeforeOK := conformanceEventUint64(transferEvent, "destinationBefore")
		providerAfter, providerAfterOK := conformanceEventUint64(transferEvent, "destinationAfter")
		if !amountOK || !probeBeforeOK || !probeAfterOK || !providerBeforeOK || !providerAfterOK || eventAmount != amount || probeBefore != evidence.Transfer.ProbeBeforeRao || providerBefore != evidence.Transfer.ProviderBeforeRao || probeAfter != evidence.Seed.BeforeRao || !exactIncrease(providerBefore, providerAfter, amount) {
			return fmt.Errorf("probe recovery event mismatch: probe=%d/%d provider=%d/%d amount=%d", probeBefore, probeAfter, providerBefore, providerAfter, amount)
		}
		evidence.Transfer.ProbeAfterRao = probeAfter
		evidence.Transfer.ProviderAfterRao = providerAfter
		evidence.Transfer.TransactionHash, evidence.Transfer.BlockNumber, evidence.Transfer.BlockHash = receiptFields(receipt)
		evidence.Complete = true
		return writePrecompileEvidence(e.stateDir, evidence)
	default:
		return fmt.Errorf("unknown precompile conformance action %s", action.ID)
	}
}

func (e *Executor) executePrecompileMove(ctx context.Context, action Action, parsed abi.ABI, evidence *PrecompileConformanceEvidence, from, to [32]byte, step *PrecompileMoveStep, amount uint64) error {
	probe := e.payloads.Manifest.PrecompileProbe
	coldkey := ss58Mirror(probe)
	if step.AmountRao == 0 {
		fromBefore, err := e.readStakeFinalized(ctx, from, coldkey)
		if err != nil {
			return err
		}
		toBefore, err := e.readStakeFinalized(ctx, to, coldkey)
		if err != nil {
			return err
		}
		step.AmountRao, step.FromBeforeRao, step.ToBeforeRao = amount, fromBefore, toBefore
		if err := writePrecompileEvidence(e.stateDir, evidence); err != nil {
			return err
		}
	}
	if step.AmountRao != amount || step.FromBeforeRao < amount {
		return errors.New("persisted precompile move intent is invalid")
	}
	data, err := parsed.Pack("moveRoundTrip", from, to, new(big.Int).SetUint64(amount))
	if err != nil {
		return err
	}
	receipt, err := e.deployer.Send(ctx, e.plan.PlanHash, action, &probe, big.NewInt(0), data)
	if err != nil {
		return err
	}
	moveEvent, err := conformanceEventValues(parsed, "MoveRoundTrip", receipt, from, to)
	if err != nil {
		return err
	}
	eventAmount, amountOK := conformanceEventUint64(moveEvent, "amount")
	fromBefore, fromBeforeOK := conformanceEventUint64(moveEvent, "fromBefore")
	fromAfter, fromAfterOK := conformanceEventUint64(moveEvent, "fromAfter")
	toBefore, toBeforeOK := conformanceEventUint64(moveEvent, "toBefore")
	toAfter, toAfterOK := conformanceEventUint64(moveEvent, "toAfter")
	if !amountOK || !fromBeforeOK || !fromAfterOK || !toBeforeOK || !toAfterOK || eventAmount != amount || fromBefore != step.FromBeforeRao || toBefore != step.ToBeforeRao || !exactDecrease(fromBefore, fromAfter, amount) || !exactIncrease(toBefore, toAfter, amount) {
		return fmt.Errorf("precompile move event is not exact: from=%d->%d to=%d->%d amount=%d", fromBefore, fromAfter, toBefore, toAfter, amount)
	}
	step.FromAfterRao, step.ToAfterRao = fromAfter, toAfter
	step.TransactionHash, step.BlockNumber, step.BlockHash = receiptFields(receipt)
	return writePrecompileEvidence(e.stateDir, evidence)
}

func readDividendAtFinalized(ctx context.Context, client *ethclient.Client, probe common.Address, parsed abi.ABI, hotkey [32]byte) (uint64, uint64, uint64, error) {
	head, err := finalizedEVMHead(ctx, client)
	if err != nil {
		return 0, 0, 0, err
	}
	values, err := contractCallAt(ctx, client, probe, parsed, "dividendDelta", head.Number, hotkey)
	if err != nil || len(values) != 3 {
		return 0, 0, 0, stateMismatchError(err, "dividendDelta returned %d values", len(values))
	}
	baseline, ok := values[0].(*big.Int)
	if !ok || !baseline.IsUint64() {
		return 0, 0, 0, fmt.Errorf("dividend baseline has type %T", values[0])
	}
	current, ok := values[1].(*big.Int)
	if !ok || !current.IsUint64() {
		return 0, 0, 0, fmt.Errorf("dividend current has type %T", values[1])
	}
	since, ok := numericUint64(values[2])
	if !ok {
		return 0, 0, 0, fmt.Errorf("dividend block has type %T", values[2])
	}
	return baseline.Uint64(), current.Uint64(), since, nil
}

func numericUint64(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint8:
		return uint64(typed), true
	case uint16:
		return uint64(typed), true
	case uint32:
		return uint64(typed), true
	case uint64:
		return typed, true
	case *big.Int:
		if typed != nil && typed.IsUint64() {
			return typed.Uint64(), true
		}
	}
	return 0, false
}

func conformanceEventValues(parsed abi.ABI, name string, receipt *ethTypes.Receipt, indexed ...[32]byte) (map[string]any, error) {
	event, ok := parsed.Events[name]
	if !ok || receipt == nil {
		return nil, fmt.Errorf("conformance event %s or receipt is unavailable", name)
	}
	for _, log := range receipt.Logs {
		if log == nil || len(log.Topics) != len(indexed)+1 || log.Topics[0] != event.ID {
			continue
		}
		matches := true
		for i := range indexed {
			if log.Topics[i+1] != common.BytesToHash(indexed[i][:]) {
				matches = false
			}
		}
		if !matches {
			continue
		}
		values := map[string]any{}
		if err := event.Inputs.NonIndexed().UnpackIntoMap(values, log.Data); err != nil {
			return nil, fmt.Errorf("decode %s: %w", name, err)
		}
		return values, nil
	}
	return nil, fmt.Errorf("finalized receipt %s has no matching %s event", receipt.TxHash, name)
}

func conformanceEventUint64(values map[string]any, name string) (uint64, bool) {
	if values == nil {
		return 0, false
	}
	return numericUint64(values[name])
}

func conformanceEventBigInt(values map[string]any, name string) (*big.Int, bool) {
	if values == nil {
		return nil, false
	}
	value, ok := values[name].(*big.Int)
	return value, ok && value != nil && value.Sign() >= 0
}

func exactDecrease(before, after, amount uint64) bool {
	return before >= after && before-after == amount
}

func exactIncrease(before, after, amount uint64) bool {
	return after >= before && after-before == amount
}

func hexBytesValue(value []byte) string { return "0x" + hex.EncodeToString(value) }

func validatePrecompileEvidenceIdentity(cfg *ResolvedConfig, deployment *ContractDeployment, evidence *PrecompileConformanceEvidence) error {
	if evidence == nil || deployment == nil {
		return errors.New("precompile conformance evidence/deployment is unavailable")
	}
	if evidence.Schema != "urnetwork-precompile-conformance-v1" || evidence.DeploymentID != cfg.Config.Deployment.DeploymentID || evidence.ConfigHash != cfg.ConfigHash || evidence.PolicyHash != cfg.PolicyHash || evidence.ChainID != testnetChainID || strings.ToLower(evidence.GenesisHash) != testnetGenesis || evidence.Netuid != cfg.Netuid || !strings.EqualFold(evidence.ProbeAddress, deployment.PrecompileProbe.Hex()) {
		return errors.New("precompile conformance evidence identity does not match the approved deployment")
	}
	probeColdkey := ss58Mirror(deployment.PrecompileProbe)
	if !strings.EqualFold(evidence.ProbeColdkey, hexBytesValue(probeColdkey[:])) {
		return errors.New("precompile conformance probe coldkey does not match mirror(probe)")
	}
	if evidence.Commitment.CanonicalGeneration != 0 && evidence.Commitment.CanonicalGeneration != precompileCanonicalFleetGeneration {
		return errors.New("precompile conformance evidence names a foreign canonical fleet generation")
	}
	return nil
}

func validConformanceTransaction(hash, blockHash string, block uint64) bool {
	_, txOK := evidenceFixedHex(hash, 32)
	_, blockOK := evidenceFixedHex(blockHash, 32)
	return txOK && blockOK && block > 0
}

func precompileEvidenceComplete(evidence *PrecompileConformanceEvidence) bool {
	if evidence == nil || !evidence.Complete || !evidence.Commitment.Restored || evidence.Commitment.CanonicalGeneration != precompileCanonicalFleetGeneration || evidence.Commitment.EncodedProbeBytes != 34 || evidence.Commitment.ProbeHash == evidence.Commitment.CanonicalHash || !evidence.Battery.Passed || evidence.Seed.DeltaRao == 0 || evidence.Forward.AmountRao == 0 || evidence.Back.AmountRao != evidence.Forward.AmountRao || evidence.Snapshot.BaselineRao == 0 || evidence.Dividend.DeltaRao == 0 || evidence.Transfer.AmountRao == 0 {
		return false
	}
	if probeHash, ok := evidenceFixedHex(evidence.Commitment.ProbeHash, 32); !ok || new(big.Int).SetBytes(probeHash).Sign() == 0 {
		return false
	}
	if canonicalHash, ok := evidenceFixedHex(evidence.Commitment.CanonicalHash, 32); !ok || new(big.Int).SetBytes(canonicalHash).Sign() == 0 {
		return false
	}
	if !evidence.Battery.BlakeOK || !evidence.Battery.MirrorKATMatch || !evidence.Battery.Ed25519OK || !evidence.Battery.Ed25519Good || !evidence.Battery.Ed25519BadRejected || !evidence.Battery.Sr25519OK || !evidence.Battery.Sr25519Good || !evidence.Battery.Sr25519BadRejected || !evidence.Battery.MetagraphOK || evidence.Battery.UIDCount == 0 || !evidence.Battery.NeuronOK || !evidence.Battery.SampleExists || !evidence.Battery.AbsentRejected || !evidence.Battery.StakeViewOK {
		return false
	}
	valueWei, ok := new(big.Int).SetString(evidence.Seed.ValueWei, 10)
	wantWei := new(big.Int).Mul(new(big.Int).SetUint64(evidence.Seed.TAORao), big.NewInt(1_000_000_000))
	if !ok || evidence.Seed.TAORao == 0 || valueWei.Cmp(wantWei) != 0 || !exactIncrease(evidence.Seed.BeforeRao, evidence.Seed.AfterRao, evidence.Seed.DeltaRao) {
		return false
	}
	if !exactDecrease(evidence.Forward.FromBeforeRao, evidence.Forward.FromAfterRao, evidence.Forward.AmountRao) || !exactIncrease(evidence.Forward.ToBeforeRao, evidence.Forward.ToAfterRao, evidence.Forward.AmountRao) || !exactDecrease(evidence.Back.FromBeforeRao, evidence.Back.FromAfterRao, evidence.Back.AmountRao) || !exactIncrease(evidence.Back.ToBeforeRao, evidence.Back.ToAfterRao, evidence.Back.AmountRao) {
		return false
	}
	if evidence.Forward.FromBeforeRao != evidence.Seed.AfterRao || evidence.Back.FromBeforeRao != evidence.Forward.ToAfterRao || evidence.Back.ToBeforeRao != evidence.Forward.FromAfterRao || evidence.Back.FromAfterRao != evidence.Forward.ToBeforeRao || evidence.Back.ToAfterRao != evidence.Forward.FromBeforeRao {
		return false
	}
	if evidence.Snapshot.BaselineRao < evidence.Back.ToAfterRao || evidence.Dividend.BaselineRao != evidence.Snapshot.BaselineRao || evidence.Dividend.SinceBlock != evidence.Snapshot.SinceBlock || !exactIncrease(evidence.Dividend.BaselineRao, evidence.Dividend.CurrentRao, evidence.Dividend.DeltaRao) {
		return false
	}
	if evidence.Transfer.ProbeBeforeRao != evidence.Dividend.CurrentRao || evidence.Transfer.ProbeAfterRao != evidence.Seed.BeforeRao || evidence.Transfer.AmountRao != evidence.Transfer.ProbeBeforeRao-evidence.Seed.BeforeRao || !exactDecrease(evidence.Transfer.ProbeBeforeRao, evidence.Transfer.ProbeAfterRao, evidence.Transfer.AmountRao) || !exactIncrease(evidence.Transfer.ProviderBeforeRao, evidence.Transfer.ProviderAfterRao, evidence.Transfer.AmountRao) {
		return false
	}
	return validConformanceTransaction(evidence.Commitment.WriteTransactionHash, evidence.Commitment.WriteFinalizedHead.Hash, evidence.Commitment.WriteFinalizedHead.Number) &&
		validConformanceTransaction(evidence.Commitment.RestoreTransactionHash, evidence.Commitment.RestoreFinalizedHead.Hash, evidence.Commitment.RestoreFinalizedHead.Number) &&
		evidence.Commitment.RestoreCommitmentBlock > evidence.Commitment.WriteCommitmentBlock &&
		validConformanceTransaction(evidence.Seed.TransactionHash, evidence.Seed.BlockHash, evidence.Seed.BlockNumber) &&
		validConformanceTransaction(evidence.Forward.TransactionHash, evidence.Forward.BlockHash, evidence.Forward.BlockNumber) &&
		validConformanceTransaction(evidence.Back.TransactionHash, evidence.Back.BlockHash, evidence.Back.BlockNumber) &&
		validConformanceTransaction(evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockHash, evidence.Snapshot.BlockNumber) &&
		validConformanceTransaction(evidence.Transfer.TransactionHash, evidence.Transfer.BlockHash, evidence.Transfer.BlockNumber) &&
		evidence.Commitment.WriteFinalizedHead.Number <= evidence.Commitment.RestoreFinalizedHead.Number &&
		evidence.Seed.BlockNumber <= evidence.Forward.BlockNumber && evidence.Forward.BlockNumber <= evidence.Back.BlockNumber &&
		evidence.Back.BlockNumber <= evidence.Snapshot.BlockNumber && evidence.Snapshot.BlockNumber <= evidence.Dividend.FinalizedHead.Number &&
		evidence.Dividend.FinalizedHead.Number <= evidence.Transfer.BlockNumber
}

func (e *Executor) verifySubstrateTransactionEvidence(recorded ChainHead, transactionHash string) error {
	if e.substrate == nil || !validConformanceTransaction(transactionHash, recorded.Hash, recorded.Number) {
		return errors.New("Substrate transaction evidence is incomplete")
	}
	finalizedHash, finalizedNumber, err := e.substrate.finalizedHead()
	if err != nil {
		return err
	}
	canonical, err := e.substrate.chain.API.RPC.Chain.GetBlockHash(recorded.Number)
	if err != nil {
		return err
	}
	ready, err := checkpointVisibility(recorded, ChainHead{Number: finalizedNumber, Hash: finalizedHash.Hex()}, canonical.Hex())
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("recorded Substrate block %d is not finalized (head %d)", recorded.Number, finalizedNumber)
	}
	blockHash, err := gsrpcTypes.NewHashFromHexString(recorded.Hash)
	if err != nil {
		return err
	}
	txHash, err := gsrpcTypes.NewHashFromHexString(transactionHash)
	if err != nil {
		return err
	}
	return verifyReleaseHistoryFinalizedExtrinsic(e.substrate.chain, e.cfg, blockHash, txHash)
}

func batteryTupleCompatible(evidence *PrecompileConformanceEvidence, tuple *precompileBatteryTuple, nominatorMinimum uint64) bool {
	if evidence == nil || tuple == nil || tuple.SampleSelfStake == nil || tuple.NominatorMinimum == nil || !tuple.SampleSelfStake.IsUint64() || !tuple.NominatorMinimum.IsUint64() {
		return false
	}
	return tuple.BlakeOk && tuple.BlakeKatMatch && strings.EqualFold(hexBytesValue(tuple.MirrorKat[:]), evidence.Battery.MirrorKAT) &&
		strings.EqualFold(hexBytesValue(tuple.SelfColdkey[:]), evidence.ProbeColdkey) && tuple.EdOk && tuple.EdVerifyGood && tuple.EdVerifyBad &&
		tuple.SrOk && tuple.SrVerifyGood && tuple.SrVerifyBad && tuple.MgOk && tuple.UidCount > 0 && tuple.Uid0Hotkey != ([32]byte{}) &&
		tuple.Uid0Coldkey != ([32]byte{}) && tuple.NeuronOk && tuple.SampleExists && tuple.SampleUid == evidence.SampleUID && tuple.AbsentRejected &&
		tuple.StakeViewOk && tuple.NominatorMinimum.Uint64() == nominatorMinimum
}

func (e *Executor) verifyCurrentPrecompileBattery(ctx context.Context, head ChainHead, evidence *PrecompileConformanceEvidence) error {
	parsed, err := e.precompileABI()
	if err != nil {
		return err
	}
	sample, err := decodeHex32("sample hotkey", evidence.SampleHotkey)
	if err != nil {
		return err
	}
	absent, err := decodeHex32("absent hotkey", evidence.AbsentHotkey)
	if err != nil {
		return err
	}
	values, err := contractCallAt(ctx, e.deployer.client, e.payloads.Manifest.PrecompileProbe, parsed, "readBattery", head.Number, sample, absent)
	if err != nil || len(values) != 1 {
		return stateMismatchError(err, "independent readBattery returned %d values", len(values))
	}
	tuple, ok := abi.ConvertType(values[0], new(precompileBatteryTuple)).(*precompileBatteryTuple)
	if !ok || !batteryTupleCompatible(evidence, tuple, e.plan.LiveFacts.NominatorMinimumRao) {
		return errors.New("independent runtime-453 precompile battery is incompatible")
	}
	uid, found, err := e.substrate.UID(sample)
	if err != nil || !found || uid != evidence.SampleUID {
		return stateMismatchError(err, "independent native sample UID=%d found=%t, want %d", uid, found, evidence.SampleUID)
	}
	return nil
}

func (e *Executor) verifyPrecompileChainEvidence(ctx context.Context, action Action, head ChainHead, evidence *PrecompileConformanceEvidence) error {
	var transactionHash, blockHash string
	var blockNumber uint64
	switch action.ID {
	case "precompile.commitment-write":
		return e.verifySubstrateTransactionEvidence(evidence.Commitment.WriteFinalizedHead, evidence.Commitment.WriteTransactionHash)
	case "precompile.commitment-restore":
		if err := e.verifySubstrateTransactionEvidence(evidence.Commitment.RestoreFinalizedHead, evidence.Commitment.RestoreTransactionHash); err != nil {
			return err
		}
		canonical, decodeErr := decodeHex32("precompile canonical commitment", evidence.Commitment.CanonicalHash)
		if decodeErr != nil || evidence.Commitment.CanonicalGeneration != precompileCanonicalFleetGeneration {
			return errors.New("precompile restore has no canonical generation-2 commitment")
		}
		hotkey, err := roleBytes32(e.roles, fleetHotkeyLabel(1))
		if err != nil {
			return err
		}
		current, err := e.substrate.fleetCommitmentFinalized(hotkey)
		if err != nil || current.Hash != canonical || current.CommitmentBlock != evidence.Commitment.RestoreCommitmentBlock {
			return conformanceMismatch("restored generation-2 commitment is not current finalized state", err)
		}
		return nil
	case "precompile.read-battery":
		if err := verifyEVMCheckpoint(ctx, e.deployer.client, head, evidence.Battery.FinalizedHead); err != nil {
			return err
		}
		return e.verifyCurrentPrecompileBattery(ctx, head, evidence)
	case "precompile.seed":
		transactionHash, blockNumber, blockHash = evidence.Seed.TransactionHash, evidence.Seed.BlockNumber, evidence.Seed.BlockHash
	case "precompile.move-forward":
		transactionHash, blockNumber, blockHash = evidence.Forward.TransactionHash, evidence.Forward.BlockNumber, evidence.Forward.BlockHash
	case "precompile.move-back":
		transactionHash, blockNumber, blockHash = evidence.Back.TransactionHash, evidence.Back.BlockNumber, evidence.Back.BlockHash
	case "precompile.snapshot":
		transactionHash, blockNumber, blockHash = evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockNumber, evidence.Snapshot.BlockHash
	case "precompile.dividend":
		if err := verifyEVMCheckpoint(ctx, e.deployer.client, head, evidence.Dividend.FinalizedHead); err != nil {
			return err
		}
		parsed, err := e.precompileABI()
		if err != nil {
			return err
		}
		sample, err := decodeHex32("sample hotkey", evidence.SampleHotkey)
		if err != nil {
			return err
		}
		baseline, current, since, err := readDividendAtFinalized(ctx, e.deployer.client, e.payloads.Manifest.PrecompileProbe, parsed, sample)
		if err != nil || baseline != evidence.Dividend.BaselineRao || since != evidence.Dividend.SinceBlock || current < evidence.Dividend.CurrentRao {
			return stateMismatchError(err, "independent dividend state baseline=%d current=%d since=%d", baseline, current, since)
		}
		return nil
	case "precompile.transfer-out":
		transactionHash, blockNumber, blockHash = evidence.Transfer.TransactionHash, evidence.Transfer.BlockNumber, evidence.Transfer.BlockHash
	default:
		return fmt.Errorf("unknown precompile chain evidence %s", action.ID)
	}
	_, err := verifyFinalizedEVMReceipt(ctx, e.deployer.client, head, transactionHash, blockNumber, blockHash)
	return err
}

func (e *Executor) verifyPrecompileConformancePostState(ctx context.Context, action Action, head ChainHead, state map[string]any) (map[string]any, error) {
	if e.payloads == nil {
		return nil, errors.New("precompile deployment payloads are unavailable")
	}
	evidence, err := loadPrecompileEvidence(e.stateDir)
	if err != nil {
		return nil, err
	}
	if err := validatePrecompileEvidenceIdentity(e.cfg, &e.payloads.Manifest, evidence); err != nil {
		return nil, err
	}
	passed := false
	switch action.ID {
	case "precompile.commitment-write":
		passed = evidence.Commitment.CanonicalGeneration == precompileCanonicalFleetGeneration && evidence.Commitment.EncodedProbeBytes == 34 && evidence.Commitment.ProbeHash != evidence.Commitment.CanonicalHash && validConformanceTransaction(evidence.Commitment.WriteTransactionHash, evidence.Commitment.WriteFinalizedHead.Hash, evidence.Commitment.WriteFinalizedHead.Number) && evidence.Commitment.WriteCommitmentBlock > 0
	case "precompile.commitment-restore":
		passed = evidence.Commitment.CanonicalGeneration == precompileCanonicalFleetGeneration && evidence.Commitment.Restored && evidence.Commitment.RestoreCommitmentBlock > evidence.Commitment.WriteCommitmentBlock && validConformanceTransaction(evidence.Commitment.RestoreTransactionHash, evidence.Commitment.RestoreFinalizedHead.Hash, evidence.Commitment.RestoreFinalizedHead.Number)
	case "precompile.read-battery":
		passed = evidence.Battery.Passed && evidence.Battery.FinalizedHead.Number > 0
	case "precompile.seed":
		passed = evidence.Seed.TAORao == e.plan.LiveFacts.ProbeTAORao && exactIncrease(evidence.Seed.BeforeRao, evidence.Seed.AfterRao, evidence.Seed.DeltaRao) && evidence.Seed.DeltaRao > 0 && validConformanceTransaction(evidence.Seed.TransactionHash, evidence.Seed.BlockHash, evidence.Seed.BlockNumber)
	case "precompile.move-forward":
		passed = evidence.Forward.AmountRao > 0 && exactDecrease(evidence.Forward.FromBeforeRao, evidence.Forward.FromAfterRao, evidence.Forward.AmountRao) && exactIncrease(evidence.Forward.ToBeforeRao, evidence.Forward.ToAfterRao, evidence.Forward.AmountRao) && validConformanceTransaction(evidence.Forward.TransactionHash, evidence.Forward.BlockHash, evidence.Forward.BlockNumber)
	case "precompile.move-back":
		passed = evidence.Back.AmountRao == evidence.Forward.AmountRao && evidence.Back.AmountRao > 0 && exactDecrease(evidence.Back.FromBeforeRao, evidence.Back.FromAfterRao, evidence.Back.AmountRao) && exactIncrease(evidence.Back.ToBeforeRao, evidence.Back.ToAfterRao, evidence.Back.AmountRao) && validConformanceTransaction(evidence.Back.TransactionHash, evidence.Back.BlockHash, evidence.Back.BlockNumber)
	case "precompile.snapshot":
		passed = evidence.Snapshot.BaselineRao >= evidence.Seed.AfterRao && evidence.Snapshot.SinceBlock > 0 && validConformanceTransaction(evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockHash, evidence.Snapshot.BlockNumber)
	case "precompile.dividend":
		passed = evidence.Dividend.BaselineRao == evidence.Snapshot.BaselineRao && exactIncrease(evidence.Dividend.BaselineRao, evidence.Dividend.CurrentRao, evidence.Dividend.DeltaRao) && evidence.Dividend.DeltaRao > 0 && evidence.Dividend.FinalizedHead.Number >= evidence.Dividend.SinceBlock
	case "precompile.transfer-out":
		passed = precompileEvidenceComplete(evidence)
	default:
		return nil, fmt.Errorf("unknown precompile postcondition %s", action.ID)
	}
	if !passed {
		return nil, fmt.Errorf("precompile conformance postcondition %s is incomplete", action.ID)
	}
	if err := e.verifyPrecompileChainEvidence(ctx, action, head, evidence); err != nil {
		return nil, fmt.Errorf("precompile conformance postcondition %s chain evidence: %w", action.ID, err)
	}
	state["probe"] = evidence.ProbeAddress
	state["evidence_hash"] = evidence.EvidenceHash
	state["complete"] = evidence.Complete
	state["canonical_chain_evidence"] = true
	return state, nil
}
