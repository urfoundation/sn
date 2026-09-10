//go:build linux || darwin

// Real private files, two signing keys per operator and actual chain readers
// exercise complete-census admission; no callback returns an authority verdict.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Two records share one real provider endpoint and native signing identity.
// Every request still passes the strict canonical ABI/SCALE fixture readers.
type releaseBootstrapV2TestFixture struct {
	cfg                    ReleaseConfig
	contexts               [2]ReleaseEvidenceV2ActivationContext
	providers              [2]*releaseActivationV2TestFixture
	chain                  *ChainClient
	native                 *crv4.Chain
	beforeCode             func(context.Context) error
	secondPublicationBlock uint64
}

// Private input provisioning never repairs runtime permissions or references.
func writeReleaseBootstrapV2TestFile(t *testing.T, path string, data []byte) ReleaseEvidenceV2File {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(data)), SHA256: attemptHex32(sha256.Sum256(data))}
}

// The byte identities come from independent fixture construction, not from a
// candidate under test. Config keeps the reviewed production runtime pins.
func newReleaseBootstrapV2TestFixture(t *testing.T) *releaseBootstrapV2TestFixture {
	t.Helper()
	fixture := &releaseBootstrapV2TestFixture{cfg: validReleaseConfig(t)}
	fixture.providers = [2]*releaseActivationV2TestFixture{newReleaseActivationV2TestFixture(t, ""), newReleaseActivationV2TestFixture(t, "")}
	first := fixture.providers[0].authority.Expected
	fixture.cfg.GenesisHash, fixture.cfg.Netuid = attemptHex32(first.Domain.GenesisHash), first.Domain.Netuid
	fixture.cfg.Coordinator, fixture.cfg.SettlementVault = common.Address(first.Domain.Coordinator).Hex(), common.Address(first.Domain.SettlementVault).Hex()
	for index := range fixture.cfg.Operators {
		fixture.cfg.Operators[index].NoID = uint64(index + 2)
	}
	root := filepath.Dir(fixture.cfg.StateDir)
	fixture.cfg.EvidenceV2 = releaseEvidenceV2TestConfig(root, fixture.cfg.Operators)
	if err := fixture.cfg.normalize(root); err != nil {
		t.Fatal(err)
	}
	policyHash, err := fixture.cfg.Policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	for index, provider := range fixture.providers {
		operator := &fixture.cfg.EvidenceV2.Operators[index]
		seed := [32]byte{byte(0x41 + index)}
		key := ed25519.NewKeyFromSeed(seed[:])
		writeReleaseBootstrapV2TestFile(t, fixture.cfg.Operators[index].ClientKeySeedFile, seed[:])
		expected := provider.authority.Expected
		expected.NoID = operator.NoID
		expected.Domain.PolicyHash = policyHash
		expected.Domain.DeploymentIDHash = sha256.Sum256([]byte(fixture.cfg.DeploymentID))
		copy(expected.VPK[:], key[ed25519.SeedSize:])
		provider.authority.Expected = expected
		provider.vpkSignature, err = expected.SignVPK(key)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := expected.Digest()
		if err != nil {
			t.Fatal(err)
		}
		provider.hotkeySignature, err = hotkey.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		domain, err := expected.EvidenceDomain()
		if err != nil {
			t.Fatal(err)
		}
		fixture.contexts[index] = ReleaseEvidenceV2ActivationContext{
			Schema: ReleaseEvidenceV2ActivationContextSchema, Activation: expected,
			InitialCut: AttemptCutV2Context{
				Identity:      AttemptLedgerIdentity{DeploymentID: fixture.cfg.DeploymentID, ChainID: fixture.cfg.ChainID, GenesisHash: fixture.cfg.GenesisHash, Netuid: fixture.cfg.Netuid, ValidatorID: fixture.cfg.ValidatorID, ValidatorUID: provider.authority.ValidatorUID, NoID: operator.NoID, ValidatorVPK: attemptHex32(expected.VPK)},
				Activation:    AttemptCutV2Activation{Domain: domain, Hotkey: expected.Hotkey, FirstSequence: expected.FirstSequence, PriorRoot: attemptHex32(expected.PriorRoot)},
				Boundary:      AttemptBoundary{SettlementEpoch: expected.Domain.Epoch, EVMBlock: provider.block, EVMBlockHash: attemptHex32(provider.blockHash)},
				FirstSequence: 1, EgressFirstSequence: 1, EgressGeneration: 1, PriorRoot: zeroAttemptHash(),
			},
			ValidatorUID: provider.authority.ValidatorUID, Journal: [20]byte(provider.authority.Journal), RuntimeHash: provider.authority.RuntimeHash, ObservedEVMBlock: provider.block, ObservedEVMHash: provider.blockHash,
		}
		contextBytes, err := fixture.contexts[index].CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
		if err != nil {
			t.Fatal(err)
		}
		activationBytes, err := expected.Payload()
		if err != nil {
			t.Fatal(err)
		}
		operator.Context = writeReleaseBootstrapV2TestFile(t, operator.Context.Path, contextBytes)
		operator.Activation = writeReleaseBootstrapV2TestFile(t, operator.Activation.Path, activationBytes)
		operator.VPKSignature = writeReleaseBootstrapV2TestFile(t, operator.VPKSignature.Path, provider.vpkSignature)
		operator.HotkeySignature = writeReleaseBootstrapV2TestFile(t, operator.HotkeySignature.Path, provider.hotkeySignature)
		operator.History = writeReleaseBootstrapV2TestFile(t, operator.History.Path, []byte("unverified history must still be replayed\n"))
	}
	if err := fixture.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", fixture); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	fixture.chain, err = DialReleaseChainContext(t.Context(), []string{httpServer.URL}, common.HexToAddress(fixture.cfg.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.chain.Close)
	// Each actual operation has its own context and strict native transcript;
	// demultiplex that context without sharing mutable mock state across workers.
	native := *fixture.providers[0].native.chain
	api := *native.API
	firstClient, secondClient := fixture.providers[0].native.chain.API.Client, fixture.providers[1].native.chain.API.Client
	var stateLock sync.Mutex
	contextKVs := map[context.Context]int{}
	api.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		index := func() int {
			stateLock.Lock()
			defer stateLock.Unlock()
			index, exists := contextKVs[ctx]
			if !exists {
				index = len(contextKVs)
				contextKVs[ctx] = index
			}
			return index
		}()
		if index == 0 {
			return firstClient.CallContext(ctx, result, method, args...)
		}
		if index == 1 {
			return secondClient.CallContext(ctx, result, method, args...)
		}
		return errors.New("bootstrap fixture has more native owners than configured operators")
	}}
	native.API = &api
	fixture.native = &native
	return fixture
}

// Common deployment views are identical; only operator record selectors vary.
func (self *releaseBootstrapV2TestFixture) ChainId(ctx context.Context) (hexutil.Uint64, error) {
	return self.providers[0].ChainId(ctx)
}
func (self *releaseBootstrapV2TestFixture) GetBlockByNumber(ctx context.Context, block gethrpc.BlockNumber, full bool) (map[string]any, error) {
	return self.providers[0].GetBlockByNumber(ctx, block, full)
}
func (self *releaseBootstrapV2TestFixture) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (map[string]any, error) {
	return self.providers[0].GetBlockByHash(ctx, hash, full)
}
func (self *releaseBootstrapV2TestFixture) GetCode(ctx context.Context, target common.Address, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if self.beforeCode != nil {
		if err := self.beforeCode(ctx); err != nil {
			return nil, err
		}
	}
	return self.providers[0].GetCode(ctx, target, selector)
}
func (self *releaseBootstrapV2TestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if self.secondPublicationBlock != 0 {
		expected := self.providers[1].authority.Expected
		digest, err := expected.Digest()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(call["input"], stabi.NewSTValidatorEvidence().PackActivation(digest)) {
			if common.BytesToAddress(call["to"]) != self.providers[1].authority.Journal || selector.BlockHash == nil || *selector.BlockHash != common.Hash(self.providers[1].blockHash) || !selector.RequireCanonical {
				return nil, errors.New("late publication fixture lost exact observer authority")
			}
			parsed, err := stabi.STValidatorEvidenceMetaData.ParseABI()
			if err != nil {
				return nil, err
			}
			return parsed.Methods["activation"].Outputs.Pack(stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(expected), PublishedBlock: self.secondPublicationBlock})
		}
	}
	var failures []error
	for _, provider := range self.providers {
		data, err := provider.Call(ctx, call, selector)
		if err == nil {
			return data, nil
		}
		failures = append(failures, err)
	}
	return nil, errors.Join(failures...)
}

// The explicit test runtime authenticates actual fixture metadata; production
// entrypoint selection of reviewed pins has a separate negative control below.
func (self *releaseBootstrapV2TestFixture) read(ctx context.Context) ([]releaseEvidenceV2ActivationInput, error) {
	return readReleaseEvidenceV2ActivationInputs(ctx, &self.cfg, self.chain, self.native, self.providers[0].authority.Expected.Hotkey, self.providers[0].native.expected)
}

// Include every provider method; local refusal must make no RPC of either kind.
func (self *releaseBootstrapV2TestFixture) calls() uint64 {
	return self.providers[0].calls.Load() + self.providers[1].calls.Load()
}

func TestReleaseBootstrapV2ContextCanonicalRoundTripAndExactBound(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	want := fixture.contexts[0]
	encoded, err := want.CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeReleaseEvidenceV2ActivationContext(encoded, uint64(len(encoded)))
	if err != nil || got != want {
		t.Fatalf("canonical context round trip differs: %v", err)
	}
	if _, err := want.CanonicalJSON(uint64(len(encoded)) - 1); err == nil {
		t.Fatal("context accepted one byte beyond exact bound")
	}
}

func TestReleaseBootstrapV2ContextRejectsConflictingIdentities(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	for _, change := range []struct {
		name   string
		mutate func(*ReleaseEvidenceV2ActivationContext)
	}{
		{name: "schema", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.Schema += "-foreign" }},
		{name: "operator", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.InitialCut.Identity.NoID++ }},
		{name: "UID", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.ValidatorUID++ }},
		{name: "hotkey", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.InitialCut.Activation.Hotkey[0] ^= 1 }},
		{name: "activation hash", mutate: func(value *ReleaseEvidenceV2ActivationContext) {
			value.InitialCut.Activation.Domain.ActivationHash[0] ^= 1
		}},
		{name: "prefix", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.InitialCut.FirstSequence++ }},
		{name: "egress", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.InitialCut.EgressFirstSequence++ }},
		{name: "epoch", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.InitialCut.Boundary.SettlementEpoch++ }},
		{name: "journal", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.Journal = value.Activation.Domain.Coordinator }},
		{name: "runtime", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.RuntimeHash = [32]byte{} }},
		{name: "observer height", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.ObservedEVMBlock = value.Activation.EVMBlock }},
		{name: "observer hash", mutate: func(value *ReleaseEvidenceV2ActivationContext) { value.ObservedEVMHash = [32]byte{} }},
		{name: "boundary before snapshot", mutate: func(value *ReleaseEvidenceV2ActivationContext) {
			value.InitialCut.Boundary.EVMBlock = value.Activation.EVMBlock
		}},
		{name: "boundary after observation", mutate: func(value *ReleaseEvidenceV2ActivationContext) {
			value.InitialCut.Boundary.EVMBlock = value.ObservedEVMBlock + 1
		}},
		{name: "same height conflicting hash", mutate: func(value *ReleaseEvidenceV2ActivationContext) {
			value.InitialCut.Boundary.EVMBlockHash = attemptHex32([32]byte{0x90})
		}},
	} {
		changed := fixture.contexts[0]
		change.mutate(&changed)
		if encoded, err := changed.CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes); err == nil || encoded != nil {
			t.Errorf("%s produced canonical authority", change.name)
		}
	}
}

func TestReleaseBootstrapV2ContextRejectsNoncanonicalWire(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	raw, err := fixture.contexts[0].CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := json.Marshal(fixture.contexts[0].Journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, wire := range [][]byte{append(bytes.Clone(raw), ' '), bytes.Replace(raw, []byte("\"schema\":"), []byte("\"unknown\":0,\"schema\":"), 1), bytes.Replace(raw, []byte("\"validator_uid\":1"), []byte("\"validator_uid\":1,\"validator_uid\":1"), 1), append(bytes.Clone(raw), []byte("{}\n")...), bytes.Replace(raw, append([]byte("\"journal\":"), journal...), []byte("\"journal\":null"), 1)} {
		if got, err := decodeReleaseEvidenceV2ActivationContext(wire, fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes); err == nil || got != (ReleaseEvidenceV2ActivationContext{}) {
			t.Fatal("noncanonical context escaped the decoder")
		}
	}
}

func TestReleaseBootstrapV2LoadsCompleteCensusThroughRealReaders(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	var exactBytes uint64
	for _, operator := range fixture.cfg.EvidenceV2.Operators {
		for _, reference := range operator.Files() {
			exactBytes += reference.Bytes
		}
	}
	fixture.cfg.EvidenceV2.Bounds.MaxControlBytes = exactBytes
	got, err := fixture.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("operator census=%d", len(got))
	}
	for index, input := range got {
		if input.Context != fixture.contexts[index] || input.Config.NoID != uint64(index+2) || input.Observation.Publication.Record != fixture.contexts[index].Activation || !input.Observation.Native.MeetsNonSelfStakeAndPermit() {
			t.Fatalf("operator %d authority differs", index)
		}
		if string(input.HistoryBytes) != "unverified history must still be replayed\n" {
			t.Fatal("bootstrap rewrote history or pretended to replay it")
		}
		if !bytes.Equal(input.PrivateKey[ed25519.SeedSize:], input.Candidate.VPK[:]) {
			t.Fatal("actual VPK differs from retained signing key")
		}
		for _, name := range []string{"stats.json", "attempt-ledger.jsonl", "attempt-ledger-v2"} {
			if _, err := os.Lstat(filepath.Join(fixture.cfg.Operators[index].StateDir, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("read-only bootstrap acquired durable state %s: %v", name, err)
			}
		}
	}
}

func TestReleaseBootstrapV2RejectsEveryChangedReferenceBeforeRPC(t *testing.T) {
	for index := 0; index < 5; index++ {
		fixture := newReleaseBootstrapV2TestFixture(t)
		reference := fixture.cfg.EvidenceV2.Operators[1].Files()[index]
		data, err := os.ReadFile(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		data[0] ^= 1
		if err := os.WriteFile(reference.Path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		before := fixture.calls()
		if got, err := fixture.read(t.Context()); err == nil || got != nil {
			t.Fatalf("reference %d accepted", index)
		}
		if fixture.calls() != before || len(fixture.providers[0].native.calls) != 0 || len(fixture.providers[1].native.calls) != 0 {
			t.Fatalf("reference %d reached RPC before complete admission", index)
		}
	}
}

func TestReleaseBootstrapV2RejectsSigningAndDeploymentDriftBeforeRPC(t *testing.T) {
	for _, fault := range []string{"actual-key", "deployment", "validator", "hotkey", "uid"} {
		fixture := newReleaseBootstrapV2TestFixture(t)
		hotkey := fixture.providers[0].authority.Expected.Hotkey
		switch fault {
		case "actual-key":
			writeReleaseBootstrapV2TestFile(t, fixture.cfg.Operators[1].ClientKeySeedFile, bytes.Repeat([]byte{0x99}, 32))
		case "deployment":
			fixture.cfg.DeploymentID = "other-deployment"
		case "validator":
			fixture.cfg.ValidatorID++
		case "hotkey":
			hotkey[0] ^= 1
		case "uid":
			configured := fixture.contexts[1]
			configured.ValidatorUID++
			raw, err := json.Marshal(configured)
			if err != nil {
				t.Fatal(err)
			}
			reference := &fixture.cfg.EvidenceV2.Operators[1].Context
			*reference = writeReleaseBootstrapV2TestFile(t, reference.Path, append(raw, '\n'))
		}
		before := fixture.calls()
		got, err := readReleaseEvidenceV2ActivationInputs(t.Context(), &fixture.cfg, fixture.chain, fixture.native, hotkey, fixture.providers[0].native.expected)
		if err == nil || got != nil || fixture.calls() != before {
			t.Fatalf("%s crossed complete local authority admission: %v", fault, err)
		}
	}
}

func TestReleaseBootstrapV2AggregateBoundPrecedesFileReads(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	var total uint64
	for _, operator := range fixture.cfg.EvidenceV2.Operators {
		for _, reference := range operator.Files() {
			total += reference.Bytes
		}
	}
	fixture.cfg.EvidenceV2.Bounds.MaxControlBytes = total - 1
	if err := os.Remove(fixture.cfg.Operators[0].ClientKeySeedFile); err != nil {
		t.Fatal(err)
	}
	before := fixture.calls()
	got, err := fixture.read(t.Context())
	if err == nil || !strings.Contains(err.Error(), "aggregate input bytes") || got != nil || fixture.calls() != before {
		t.Fatalf("aggregate admission did not precede missing seed read: %v", err)
	}
}

func TestReleaseBootstrapV2ParallelOperatorsCancelWithoutPartialResult(t *testing.T) {
	priorProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(priorProcs)
	fixture := newReleaseBootstrapV2TestFixture(t)
	entered, exited := make(chan struct{}), make(chan struct{})
	var enters, exits atomic.Int32
	fixture.beforeCode = func(ctx context.Context) error {
		if enters.Add(1) == 2 {
			close(entered)
		}
		<-ctx.Done()
		if exits.Add(1) == 2 {
			close(exited)
		}
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		inputs []releaseEvidenceV2ActivationInput
		err    error
	}
	done := make(chan outcome, 1)
	go func() { inputs, err := fixture.read(ctx); done <- outcome{inputs: inputs, err: err} }()
	select {
	case <-entered:
	case result := <-done:
		t.Fatalf("both operators did not overlap: %v", result.err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	cancel()
	result := <-done
	if result.inputs != nil || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("canceled batch published partial authority: %v", result.err)
	}
	<-exited
}

func TestReleaseBootstrapV2LateChainFailureDiscardsEveryOperator(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	fixture.providers[1].fault = "absent-activation"
	got, err := fixture.read(t.Context())
	if err == nil || got != nil {
		t.Fatal("one missing on-chain activation became a partial configured census")
	}
}

func TestReleaseBootstrapV2PublicationCannotFollowInitialBoundary(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	fixture.contexts[1].InitialCut.Boundary.EVMBlock = 1100
	fixture.contexts[1].InitialCut.Boundary.EVMBlockHash = attemptHex32([32]byte{0x71})
	raw, err := fixture.contexts[1].CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
	if err != nil {
		t.Fatal(err)
	}
	reference := &fixture.cfg.EvidenceV2.Operators[1].Context
	*reference = writeReleaseBootstrapV2TestFile(t, reference.Path, raw)
	fixture.secondPublicationBlock = 1101
	got, err := fixture.read(t.Context())
	if got != nil || err == nil || !strings.Contains(err.Error(), "publication follows the configured initial cut boundary") {
		t.Fatalf("late activation escaped the boundary check: %v", err)
	}
}

func TestReleaseBootstrapV2ProductionEntryPinsReviewedNativeRuntime(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	if fixture.providers[0].native.expected.CodeHash == fixture.cfg.RuntimeCodeHash {
		t.Fatal("fixture no longer distinguishes reviewed and synthetic runtime")
	}
	got, err := loadReleaseEvidenceV2ActivationInputs(t.Context(), &fixture.cfg, fixture.chain, fixture.native, fixture.providers[0].authority.Expected.Hotkey)
	if err == nil || got != nil {
		t.Fatal("production bootstrap accepted synthetic metadata under reviewed runtime pins")
	}
}

func TestReleaseBootstrapV2PreCancellationHasNoIO(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	if err := os.Remove(fixture.cfg.Operators[0].ClientKeySeedFile); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := fixture.calls()
	got, err := fixture.read(ctx)
	if got != nil || !errors.Is(err, context.Canceled) || fixture.calls() != before {
		t.Fatalf("pre-canceled bootstrap reached IO: %v", err)
	}
}

// Re-registration changes the current UID, not the hotkey or the original
// ledger namespace. Both observations run the real bounded native reader.
func TestReleaseBootstrapV2HistoricalUIDSurvivesCurrentReregistration(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	current := newReleaseNativeValidatorUIDTestFixture(t, 2, fixture.providers[0].authority.Expected.Hotkey)
	current.ctx = t.Context()
	current.block = types.Hash{3}
	current.blockNumber = 101
	currentObservation, err := current.read()
	if err != nil {
		t.Fatalf("actual current registration: %v", err)
	}
	if currentObservation.Identity.UID != 2 || currentObservation.Identity.BlockNumber != 101 || !currentObservation.MeetsNonSelfStakeAndPermit() {
		t.Fatalf("current registration prerequisite differs: %+v", currentObservation.Identity)
	}
	if currentObservation.Identity.UID == fixture.contexts[0].ValidatorUID || currentObservation.Identity.BlockHash == types.Hash(fixture.contexts[0].Activation.NativeHash) {
		t.Fatal("fixture did not separate current and historical registrations")
	}
	got, err := readReleaseEvidenceV2ActivationInputs(t.Context(), &fixture.cfg, fixture.chain, fixture.native, currentObservation.Identity.Hotkey, fixture.providers[0].native.expected)
	if err != nil || len(got) != 2 {
		t.Fatalf("current re-registration displaced historical activation UID: %v", err)
	}
	for index, input := range got {
		if input.Context.InitialCut.Identity.ValidatorUID != 1 || input.Context.ValidatorUID != 1 || input.Observation.Native.Identity.UID != 1 || input.Observation.Native.Identity.BlockNumber != 100 || input.Context != fixture.contexts[index] {
			t.Fatalf("operator %d historical UID or initial ledger namespace changed", index)
		}
		if input.Observation.Native.Identity.Hotkey != currentObservation.Identity.Hotkey {
			t.Fatal("historical/current observations did not bind the same actual hotkey")
		}
	}
}

// A well-formed independently pinned context is still subject to the real
// historical Keys/Uids mapping; removing the current UID must not skip it.
func TestReleaseBootstrapV2RejectsWrongHistoricalUIDMapping(t *testing.T) {
	fixture := newReleaseBootstrapV2TestFixture(t)
	configured := fixture.contexts[1]
	configured.ValidatorUID = 2
	configured.InitialCut.Identity.ValidatorUID = 2
	raw, err := configured.CanonicalJSON(fixture.cfg.EvidenceV2.Bounds.Cut.MaxHeaderBytes)
	if err != nil {
		t.Fatalf("well-formed historical UID context: %v", err)
	}
	reference := &fixture.cfg.EvidenceV2.Operators[1].Context
	*reference = writeReleaseBootstrapV2TestFile(t, reference.Path, raw)
	got, err := fixture.read(t.Context())
	if err == nil || got != nil || !strings.Contains(err.Error(), "Keys") {
		t.Fatalf("wrong historical UID bypassed actual native mapping: %v", err)
	}
}
