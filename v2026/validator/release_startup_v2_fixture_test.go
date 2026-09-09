//go:build linux || darwin

// This fixture joins genuine bootstrap signatures, native metadata/stake RPC,
// hash-pinned coordinator views, disk ledgers, M8 trails and two HTTP replicas.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

type releaseStartupV2TestFixture struct {
	*releaseInitialBoundaryV2TestFixture
	inputs        []releaseEvidenceV2ActivationInput
	disk          *releaseEvidenceV2DiskState
	nativeFixture *releaseNativeValidatorTestFixture
	nativeEpoch   map[string]uint64
	blocks        map[uint64][32]byte
	finalized     uint64
	boundary      AttemptBoundary
	servers       []*mockVerifyServer
	engines       []*TrailEngine
	keys          map[uint64]map[byte]ed25519.PublicKey
	replicas      [2]AttemptCutV2Replica
	stores        [2]*attemptCutV2ReplicaTestStore
}

// Independent pins are chosen by the existing actual bootstrap before any
// candidate under test is read. Native UID 2 differs from historical UID 1.
func newReleaseStartupV2TestFixture(t *testing.T, active bool) *releaseStartupV2TestFixture {
	t.Helper()
	base, inputs := newReleaseEvidenceV2DiskTestFixture(t)
	fixture := &releaseStartupV2TestFixture{releaseInitialBoundaryV2TestFixture: base, inputs: inputs, nativeEpoch: map[string]uint64{}, blocks: map[uint64][32]byte{}, keys: map[uint64]map[byte]ed25519.PublicKey{}}
	fixture.boundary = inputs[0].Context.InitialCut.Boundary
	fixture.finalized = fixture.boundary.EVMBlock
	fixture.blocks[fixture.finalized] = base.contexts[0].ObservedEVMHash
	fixture.replicas, fixture.stores = newAttemptCutV2ReplicaTestStores(t)
	fixture.nativeFixture = newReleaseNativeValidatorUIDTestFixture(t, 2, inputs[0].Context.InitialCut.Activation.Hotkey)
	fixture.prepareNative(t)
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", fixture); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	chain, err := DialReleaseChainContext(t.Context(), []string{httpServer.URL}, common.HexToAddress(base.cfg.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	fixture.chain = chain
	t.Cleanup(chain.Close)
	for _, input := range inputs {
		transport, _, _ := newMockVerifyServer(t, 16)
		transport.validatorVpk = bytes.Clone(input.PrivateKey.Public().(ed25519.PublicKey))
		fixture.servers = append(fixture.servers, transport)
		fixture.keys[input.Config.NoID] = transport.serverPublicKeys()
	}
	fixture.disk, err = openReleaseEvidenceV2DiskState(t.Context(), &fixture.cfg, inputs, fixture.keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fixture.disk.close(); err != nil {
			t.Error(err)
		}
	})
	if active {
		if err := fixture.start(t.Context(), attemptSettlementV2PhysicalIO()); err != nil {
			t.Fatal(err)
		}
		fixture.prepareEngines(t)
	}
	return fixture
}

// Extend actual SDK metadata with the real schedule key. Every other call
// reaches the existing strict identity/stake fixture and real CRV4 readers.
func (self *releaseStartupV2TestFixture) prepareNative(t *testing.T) {
	t.Helper()
	fixture := self.nativeFixture
	original := fixture.chain.API.Client
	var encoded json.RawMessage
	if err := original.CallContext(fixture.ctx, &encoded, "state_getMetadata", fixture.block.Hex()); err != nil {
		t.Fatal(err)
	}
	var metadataHex string
	if err := json.Unmarshal(encoded, &metadataHex); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	metadata.AsMetadataV14.Pallets[0].Storage.Items = append(metadata.AsMetadataV14.Pallets[0].Storage.Items, types.StorageEntryMetadataV14{Name: "SubnetEpochIndex", Modifier: types.StorageFunctionModifierV0{IsDefault: true}, Type: types.StorageEntryTypeV14{IsMap: true, AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{{IsIdentity: true}}}}})
	metadataHex, err = codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	_, fixture.expected.MetadataHash, err = crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	key, err := types.CreateStorageKey(metadata, "SubtensorModule", "SubnetEpochIndex", binary.LittleEndian.AppendUint16(nil, self.cfg.Netuid))
	if err != nil {
		t.Fatal(err)
	}
	self.nativeEpoch[fixture.block.Hex()] = 1
	fixture.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		fixture.ctx = ctx
		for _, value := range args {
			if hash, ok := value.(string); ok {
				if epoch, exists := self.nativeEpoch[hash]; exists {
					parsed, err := types.NewHashFromHexString(hash)
					if err != nil {
						return err
					}
					fixture.block, fixture.blockNumber = parsed, 99+epoch
				}
			}
		}
		if method == "state_getMetadata" {
			if len(args) != 1 || args[0] != fixture.block.Hex() {
				return errors.New("startup schedule metadata is not block pinned")
			}
			return setValidatorRuntimeIdentityTestResult(result, metadataHex)
		}
		if method == "state_getStorage" && len(args) == 2 && args[0] == key.Hex() {
			if args[1] != fixture.block.Hex() {
				return errors.New("startup schedule index is not block pinned")
			}
			return setValidatorRuntimeIdentityTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint64(nil, self.nativeEpoch[fixture.block.Hex()])))
		}
		return original.CallContext(ctx, result, method, args...)
	}}
}

// The EVM endpoint serves each independently defined canonical historical
// block, including the exclusive rolled epoch end required for terminals.
func (self *releaseStartupV2TestFixture) GetBlockByNumber(ctx context.Context, block gethrpc.BlockNumber, full bool) (map[string]any, error) {
	number := uint64(block)
	if block == gethrpc.FinalizedBlockNumber {
		number = self.finalized
	}
	hash, exists := self.blocks[number]
	if !exists || full {
		return nil, errors.New("startup fixture requested an undefined canonical block")
	}
	return map[string]any{"number": hexutil.EncodeUint64(number), "hash": common.Hash(hash)}, ctx.Err()
}

func (self *releaseStartupV2TestFixture) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (map[string]any, error) {
	for number, value := range self.blocks {
		if common.Hash(value) == hash {
			return self.GetBlockByNumber(ctx, gethrpc.BlockNumber(number), full)
		}
	}
	return nil, errors.New("startup fixture requested an undefined block hash")
}

// Real generated ABI methods and canonical tuple encoders supply every view;
// candidate snapshots never choose currentEpoch, policy cadence or end block.
func (self *releaseStartupV2TestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical || common.BytesToAddress(call["to"]) != common.HexToAddress(self.cfg.Coordinator) {
		return nil, errors.New("startup fixture requires canonical hash-pinned coordinator views")
	}
	var block uint64
	for number, hash := range self.blocks {
		if common.Hash(hash) == *selector.BlockHash {
			block = number
			break
		}
	}
	if block < 1001 {
		return nil, errors.New("startup fixture view block is undefined")
	}
	epoch := uint64(7) + (block-1001)/500
	coordinator := stabi.NewSTCoordinator()
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	var method string
	var value any
	switch {
	case bytes.Equal(call["input"], coordinator.PackCurrentEpoch()):
		method, value = "currentEpoch", new(big.Int).SetUint64(epoch)
	case bytes.Equal(call["input"], coordinator.PackPolicyAt(new(big.Int).SetUint64(epoch))):
		method, value = "policyAt", stabi.STCoordinatorPolicySnapshot{PolicyHash: self.inputs[0].Context.InitialCut.Activation.Domain.PolicyHash, EffectiveEpoch: 7, EffectiveBlock: 1001, EpochBlocks: 500, EpochDepositCapRao: big.NewInt(1000), CampaignDepositCapRao: big.NewInt(5000)}
	default:
		for _, input := range self.inputs {
			if bytes.Equal(call["input"], coordinator.PackOperatorAt(new(big.Int).SetUint64(input.Config.NoID), new(big.Int).SetUint64(epoch))) {
				method, value = "operatorAt", stabi.STCoordinatorOperatorVersion{Active: true, EffectiveEpoch: 7}
			}
		}
		for closed := uint64(7); closed < epoch; closed++ {
			if bytes.Equal(call["input"], coordinator.PackEpochEndBlock(new(big.Int).SetUint64(closed))) {
				method, value = "epochEndBlock", new(big.Int).SetUint64(1501+500*(closed-7))
			}
		}
	}
	if method == "" {
		return nil, errors.New("startup fixture received undefined coordinator calldata")
	}
	return parsed.Methods[method].Outputs.Pack(value)
}

func (self *releaseStartupV2TestFixture) start(ctx context.Context, physical attemptSettlementV2IO) error {
	return startReleaseEvidenceV2DiskStateWithRuntime(ctx, &self.cfg, self.chain, self.nativeFixture.chain, self.inputs, self.keys, [2]string{self.replicas[0].Origin, self.replicas[1].Origin}, self.disk, self.nativeFixture.expected, physical)
}

func (self *releaseStartupV2TestFixture) reopen(t *testing.T) {
	t.Helper()
	if err := self.disk.close(); err != nil {
		t.Fatal(err)
	}
	disk, err := openReleaseEvidenceV2DiskState(t.Context(), &self.cfg, self.inputs, self.keys)
	if err != nil {
		t.Fatal(err)
	}
	self.disk = disk
}

// Genuine server/client keys and historical binding records flow through the
// production trail engine. No unsigned synthetic counter is used as a control.
func (self *releaseStartupV2TestFixture) prepareEngines(t *testing.T) {
	t.Helper()
	self.engines = nil
	for index, participant := range self.disk.participants {
		server := self.servers[index]
		engine := NewTrailEngine(server.validatorClientId, self.inputs[index].PrivateKey, server, NewStaticServerKeyRing(server.serverPublicKeys()), func(context.Context) (connect.Id, error) { return server.providers[0], nil }, participant.Stats, self.disk.states[participant.NoID].store, func() uint64 { return self.boundary.SettlementEpoch }, TrailEngineConfig{M: self.cfg.Policy.Verify.TrailDepth, StepTimeout: 2 * time.Second, ExtendAttempts: 3, Pace: time.Millisecond, AttemptLedger: participant.Ledger, AttemptBoundaryResolver: func(_ context.Context, pinned *AttemptBoundary, ids []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			if pinned != nil && *pinned != self.boundary {
				return AttemptBoundary{}, nil, errors.New("startup trail changed its finalized boundary")
			}
			bindings := make([]AttemptBinding, len(ids))
			for i, id := range ids {
				bindings[i] = attemptLedgerTestBinding(id, 1)
			}
			return self.boundary, bindings, nil
		}})
		self.engines = append(self.engines, engine)
	}
}

func (self *releaseStartupV2TestFixture) trail(t *testing.T, index int) {
	t.Helper()
	proof, err := self.engines[index].RunTrail(t.Context())
	if err != nil || proof == nil || proof.M != 8 || len(proof.Hops) != 8 {
		t.Fatalf("actual startup M8 trail: %v", err)
	}
}

func (self *releaseStartupV2TestFixture) sealOptions(t *testing.T, index int) releaseStatsV2Options {
	t.Helper()
	replicated, err := newAttemptCutV2Replicas(self.cfg.EvidenceV2.Bounds.Cut, self.replicas)
	if err != nil {
		t.Fatal(err)
	}
	bounds := self.cfg.EvidenceV2.Bounds
	keys := self.keys[self.disk.participants[index].NoID]
	return releaseStatsV2Options{Activation: self.inputs[index].Context.InitialCut.Activation, Policy: self.cfg.Policy, Bounds: bounds.Cut,
		Seal:  AttemptCutV2SealOptions{ReplayBounds: bounds.Replay, ScratchDirectory: filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "seal"), ServerKeys: keys, WriteRecords: replicated.writer(AttemptStreamV2Records), WriteProofs: replicated.writer(AttemptStreamV2Proofs), WriteMetadata: replicated.writer("metadata"), ReadMetadata: replicated.readers[0].ReadMetadata, OpenData: replicated.readers[0].OpenData},
		Stats: AttemptCutV2StatsOptions{ExpectedConfig: ReleaseStatsConfig{AMin: self.cfg.Policy.Verify.ReliabilityAMin, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000}, MaxProviders: bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes, Replay: AttemptCutV2ReplayOptions{Bounds: bounds.Replay, ScratchDirectory: filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "replay"), ServerKeys: keys, ReadMetadata: replicated.readers[0].ReadMetadata, OpenData: replicated.readers[0].OpenData}},
	}
}

// Failure after the genuine immutable write is precisely crash-before-Begin
// and before native snapshot rotation. The next startup must consume that cut.
func (self *releaseStartupV2TestFixture) ordinary(t *testing.T, index int, epoch uint64, crash bool) *releaseMeasurementInputJournal {
	t.Helper()
	participant := self.disk.participants[index]
	nativeHash := types.Hash{byte(epoch + 1)}
	self.nativeEpoch[nativeHash.Hex()] = epoch
	var journal *releaseMeasurementInputJournal
	stop := errors.New("actual ordinary journal durable before snapshot and Begin")
	_, _, err := participant.Stats.detachReleaseStatsMeasurementV2(t.Context(), participant.StateDir, self.boundary, self.sealOptions(t, index), func(stats ReleaseStatsMeasurement, cut AttemptCutV2) error {
		journal = &releaseMeasurementInputJournal{Schema: releaseMeasurementInputV2Schema, DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: strings.ToLower(self.cfg.GenesisHash), Coordinator: self.cfg.Coordinator, ValidatorID: self.cfg.ValidatorID, Netuid: self.cfg.Netuid, SubnetEpoch: epoch, PolicyHash: strings.ToLower(self.cfg.PolicyHash), MeasurementInput: ReleaseMeasurementInput{NoID: participant.NoID, SettlementEpoch: self.boundary.SettlementEpoch, CutNativeBlock: 99 + epoch, CutNativeBlockHash: nativeHash.Hex(), CutEVMSnapshotBlock: self.boundary.EVMBlock, CutEVMSnapshotHash: self.boundary.EVMBlockHash, EgressGeneration: cut.Context.EgressGeneration, Stats: stats, AttemptCutV2: &cut}}
		encoded, err := canonicalReleaseMeasurementInputBytes(journal)
		if err != nil {
			return err
		}
		owner, err := acquireReleaseMeasurementInputV2Owner(t.Context(), releaseMeasurementInputV2Path(self.cfg.StateDir, epoch, participant.NoID), self.cfg.EvidenceV2.Bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{}, true)
		if err != nil {
			return errors.Join(err, owner.finish())
		}
		_, readErr := owner.read()
		if !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(readErr) {
			return errors.Join(errors.New("test ordinary path was not initially absent"), readErr, owner.finish())
		}
		if err := errors.Join(owner.write(encoded), owner.finish()); err != nil {
			return err
		}
		if crash {
			return stop
		}
		return nil
	})
	if crash && !errors.Is(err, stop) || !crash && err != nil || journal == nil {
		t.Fatalf("actual ordinary persistence control: %v", err)
	}
	return journal
}

// Optional physical interruption is after the real journal has become durable.
// All stream sealing, transition signatures and complete batch replay are real.
func (self *releaseStartupV2TestFixture) terminal(t *testing.T, partial bool) *AttemptSettlementClosureV2 {
	t.Helper()
	epoch := self.boundary.SettlementEpoch
	end := uint64(1501) + 500*(epoch-7)
	self.blocks[end-1], self.blocks[end] = [32]byte{byte(epoch), 0x71}, [32]byte{byte(epoch + 1), 0x72}
	self.finalized = end
	boundary := AttemptBoundary{SettlementEpoch: epoch, EVMBlock: end - 1, EVMBlockHash: attemptHex32(self.blocks[end-1])}
	bounds := self.cfg.EvidenceV2.Bounds
	options := AttemptSettlementRuntimeV2Options{Persistence: bounds.Persistence, Authority: AttemptSettlementV2Options{Operators: map[uint64]AttemptSettlementV2OperatorOptions{}, MaxParticipants: bounds.MaxParticipants, MaxTransitionBytes: bounds.MaxTransitionBytes, MaxClosureBytes: bounds.MaxClosureBytes}, Seal: map[uint64]AttemptCutV2SealOptions{}, PrivateKeys: map[uint64]ed25519.PrivateKey{}}
	for index, participant := range self.disk.participants {
		setup := self.sealOptions(t, index)
		expected, err := participant.Stats.releaseStatsV2Context(t.Context(), boundary, setup)
		if err != nil {
			t.Fatal(err)
		}
		options.Authority.Operators[participant.NoID] = AttemptSettlementV2OperatorOptions{Expected: expected, Policy: self.cfg.Policy, Bounds: bounds.Cut, Measurement: AttemptCutV2MeasurementOptions{ExpectedConfig: setup.Stats.ExpectedConfig, MaxProviders: bounds.MaxProviders, MaxEgressHashes: bounds.MaxEgressHashes, MaxFleetPrefixes: bounds.MaxFleetPrefixes, Replay: setup.Stats.Replay}}
		options.Seal[participant.NoID], options.PrivateKeys[participant.NoID] = setup.Seal, self.inputs[index].PrivateKey
	}
	physical := attemptSettlementV2PhysicalIO()
	stop := errors.New("actual startup terminal interrupted after first snapshot")
	if partial {
		physical.writeSnapshot = func(root *attemptPrivateDirectory, name string, encoded []byte) error {
			if root.path == self.disk.participants[1].StateDir {
				return stop
			}
			return writeAttemptSettlementV2OwnedState(root, name, encoded)
		}
	}
	closure, err := advanceAttemptSettlementEpochV2(t.Context(), self.cfg.StateDir, self.disk.participants, epoch+1, boundary, options, physical, true)
	if partial {
		if !errors.Is(err, stop) || closure != nil {
			t.Fatalf("actual partial terminal: %v", err)
		}
		encoded, err := os.ReadFile(attemptSettlementTransactionV2Path(self.cfg.StateDir))
		if err != nil {
			t.Fatal(err)
		}
		journal, _, _, err := decodeAttemptSettlementTransactionV2(t.Context(), encoded, self.disk.participants, bounds.Persistence)
		if err != nil {
			t.Fatal(err)
		}
		closure, err = decodeAttemptSettlementClosureV2Bytes(t.Context(), journal.ClosureJSON, bounds.MaxClosureBytes, bounds.MaxParticipants)
		if err != nil {
			t.Fatal(err)
		}
	} else if err != nil || closure == nil {
		t.Fatalf("actual complete terminal: %v", err)
	}
	self.boundary = AttemptBoundary{SettlementEpoch: epoch + 1, EVMBlock: end, EVMBlockHash: attemptHex32(self.blocks[end])}
	return closure
}
