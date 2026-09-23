// Real dual-key signatures and the production EVM/CRV4 readers share exact
// scripted historical views. No fixture returns an eligibility verdict.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Hooks and faults are frozen before the operation starts; counters are atomic.
// The native transcript is read only after the owning operation has joined.
type releaseActivationV2TestFixture struct {
	chain           *ChainClient
	native          *releaseNativeValidatorTestFixture
	nativeContext   *sync.Once
	authority       ReleaseActivationV2Authority
	vpkSignature    []byte
	hotkeySignature []byte
	block           uint64
	blockHash       [32]byte
	code            []byte
	fault           string
	calls           atomic.Uint64
	beforeFinalized func(context.Context) error
	beforeCode      func(context.Context) error
}

// A complete fixed call is matched by target, calldata and block hash.
type releaseActivationV2TestView struct {
	target common.Address
	data   []byte
	method string
	parsed *abi.ABI
	value  any
}

// Builds the fixed expected record from two real independent keys. Optional
// native fixture hotkeys change its SCALE storage keys before serving begins.
func newReleaseActivationV2TestFixture(t *testing.T, fault string) *releaseActivationV2TestFixture {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x31})
	if err != nil {
		t.Fatal(err)
	}
	native := newReleaseNativeValidatorTestFixture(t, hotkey.PublicKey())
	seed := [32]byte{0x41}
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	activation := protocol.ValidatorEvidenceActivation{
		Domain: protocol.ValidatorEvidenceActivationDomain{
			ChainID: 945, GenesisHash: [32]byte(native.genesis), Netuid: 521,
			Coordinator: [20]byte{0x12}, SettlementVault: [20]byte{0x13},
			DeploymentIDHash: sha256.Sum256([]byte("real-activation-reader-fixture")),
			PolicyHash:       [32]byte{0x15}, Epoch: 7,
		},
		Hotkey: hotkey.PublicKey(), NoID: 2, FirstSequence: 1,
		NativeBlock: 100, NativeHash: [32]byte(native.block), EVMBlock: 1000, EVMHash: [32]byte{0x25},
	}
	copy(activation.VPK[:], privateKey[ed25519.SeedSize:])
	vpkSignature, err := activation.SignVPK(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := activation.Digest()
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := activation.Verify(activation, vpkSignature, hotkeySignature); err != nil {
		t.Fatal(err)
	}
	fixture := &releaseActivationV2TestFixture{
		native: native, nativeContext: new(sync.Once), vpkSignature: vpkSignature, hotkeySignature: hotkeySignature,
		block: 1200, blockHash: [32]byte{0xa1}, code: []byte{0x60, 0x00, 0x00}, fault: fault,
		authority: ReleaseActivationV2Authority{
			Expected: activation, Journal: common.Address{0x33}, ValidatorUID: 1,
			NativeRuntime: native.expected,
		},
	}
	fixture.authority.RuntimeHash = [32]byte(crypto.Keccak256Hash(fixture.code))
	// The combined reader owns a bounded child context. Bind the scripted
	// native client to that exact first context and retain its existing check
	// that every later actual native call uses the same operation.
	original := native.chain.API.Client
	native.chain.API.Client = &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		fixture.nativeContext.Do(func() { native.ctx = ctx })
		return original.CallContext(ctx, result, method, args...)
	}}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", fixture); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() { httpServer.Close(); server.Stop() })
	fixture.chain, err = DialReleaseChainContext(t.Context(), []string{httpServer.URL}, common.Address(activation.Domain.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.chain.Close)
	return fixture
}

// This provider's chain ID is independent of candidate activation bytes.
func (self *releaseActivationV2TestFixture) ChainId(ctx context.Context) (hexutil.Uint64, error) {
	self.calls.Add(1)
	return hexutil.Uint64(945), ctx.Err()
}

// Explicit finalized observations never silently fall back to latest.
func (self *releaseActivationV2TestFixture) GetBlockByNumber(ctx context.Context, block gethrpc.BlockNumber, full bool) (map[string]any, error) {
	self.calls.Add(1)
	if block != gethrpc.FinalizedBlockNumber || full {
		return nil, errors.New("activation fixture requires finalized EVM observer")
	}
	if self.beforeFinalized != nil {
		if err := self.beforeFinalized(ctx); err != nil {
			return nil, err
		}
	}
	number, hash := self.block, common.Hash(self.blockHash)
	if self.fault == "nonfinalized" {
		number--
		hash = common.Hash{0xb1}
	}
	return map[string]any{"number": hexutil.EncodeUint64(number), "hash": hash}, ctx.Err()
}

// Historical and observation hashes have different fixed heights.
func (self *releaseActivationV2TestFixture) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (map[string]any, error) {
	self.calls.Add(1)
	if full {
		return nil, errors.New("activation fixture does not return full blocks")
	}
	number := self.block
	if hash == common.Hash(self.authority.Expected.EVMHash) {
		number = self.authority.Expected.EVMBlock
	} else if hash != common.Hash(self.blockHash) {
		return nil, errors.New("activation fixture unknown EVM hash")
	}
	return map[string]any{"number": hexutil.EncodeUint64(number), "hash": hash}, ctx.Err()
}

// One bounded synthetic runtime stands in for already-qualified compiler code;
// these tests do not claim a real deployment or chain inclusion certificate.
func (self *releaseActivationV2TestFixture) GetCode(ctx context.Context, target common.Address, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if target != self.authority.Journal || selector.BlockHash == nil || *selector.BlockHash != common.Hash(self.blockHash) || !selector.RequireCanonical || selector.BlockNumber != nil {
		return nil, errors.New("activation fixture code read is not canonical observer state")
	}
	if self.beforeCode != nil {
		if err := self.beforeCode(ctx); err != nil {
			return nil, err
		}
	}
	code := bytes.Clone(self.code)
	if self.fault == "foreign-runtime" {
		code = append(code, 0)
	}
	return hexutil.Bytes(code), ctx.Err()
}

// The real reader uses exact method arguments on both canonical snapshots.
func (self *releaseActivationV2TestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	self.calls.Add(1)
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical {
		return nil, errors.New("activation fixture view lost its canonical hash")
	}
	targetBytes, exists := call["to"]
	if !exists || len(targetBytes) != 20 {
		return nil, errors.New("activation fixture view target is invalid")
	}
	target := common.BytesToAddress(targetBytes)
	data, exists := call["input"]
	if !exists {
		return nil, errors.New("activation fixture view input is missing")
	}
	expected := self.authority.Expected
	coordinator := stabi.NewSTCoordinator()
	coordinatorABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	evidence := stabi.NewSTValidatorEvidence()
	evidenceABI, err := stabi.STValidatorEvidenceMetaData.ParseABI()
	if err != nil {
		return nil, err
	}
	var views []releaseActivationV2TestView
	if *selector.BlockHash == common.Hash(expected.EVMHash) {
		if self.fault == "historical-reorg" {
			return nil, errors.New("activation fixture historical hash is no longer canonical")
		}
		currentEpoch := new(big.Int).SetUint64(expected.Domain.Epoch - 1)
		if self.fault == "past-epoch" {
			currentEpoch.SetUint64(expected.Domain.Epoch + 1)
		}
		policy := stabi.STCoordinatorPolicySnapshot{
			PolicyHash: expected.Domain.PolicyHash, EffectiveEpoch: expected.Domain.Epoch,
			EffectiveBlock: expected.EVMBlock + 10, EpochBlocks: 100,
			EpochDepositCapRao: big.NewInt(1000), CampaignDepositCapRao: big.NewInt(5000),
		}
		if self.fault == "wrong-policy" {
			policy.PolicyHash[0] ^= 1
		}
		if self.fault == "future-policy" {
			policy.EffectiveEpoch++
		}
		operator := stabi.STCoordinatorOperatorVersion{Active: true, EffectiveEpoch: expected.Domain.Epoch}
		if self.fault == "inactive-operator" {
			operator.Active = false
		}
		if self.fault == "future-operator" {
			operator.EffectiveEpoch++
		}
		epoch := new(big.Int).SetUint64(expected.Domain.Epoch)
		views = []releaseActivationV2TestView{
			{target: common.Address(expected.Domain.Coordinator), data: coordinator.PackCurrentEpoch(), method: "currentEpoch", parsed: coordinatorABI, value: currentEpoch},
			{target: common.Address(expected.Domain.Coordinator), data: coordinator.PackPolicyAt(epoch), method: "policyAt", parsed: coordinatorABI, value: policy},
			{target: common.Address(expected.Domain.Coordinator), data: coordinator.PackOperatorAt(new(big.Int).SetUint64(expected.NoID), epoch), method: "operatorAt", parsed: coordinatorABI, value: operator},
		}
	} else if *selector.BlockHash == common.Hash(self.blockHash) {
		journal := self.authority.Journal
		anchor := journal
		if self.fault == "foreign-anchor" {
			anchor[1] ^= 1
		}
		publication := stabi.STValidatorEvidenceActivation{Record: stabi.ValidatorEvidenceActivationRecordFromProtocol(expected), PublishedBlock: expected.EVMBlock + 1}
		if self.fault == "absent-activation" {
			publication = stabi.STValidatorEvidenceActivation{}
		}
		digest, err := expected.Digest()
		if err != nil {
			return nil, err
		}
		views = []releaseActivationV2TestView{
			{target: common.Address(expected.Domain.Coordinator), data: coordinator.PackValidatorEvidence(), method: "validatorEvidence", parsed: coordinatorABI, value: anchor},
			{target: journal, data: evidence.PackCoordinator(), method: "coordinator", parsed: evidenceABI, value: common.Address(expected.Domain.Coordinator)},
			{target: journal, data: evidence.PackSettlementVault(), method: "settlementVault", parsed: evidenceABI, value: common.Address(expected.Domain.SettlementVault)},
			{target: journal, data: evidence.PackChainId(), method: "chainId", parsed: evidenceABI, value: expected.Domain.ChainID},
			{target: journal, data: evidence.PackNetuid(), method: "netuid", parsed: evidenceABI, value: expected.Domain.Netuid},
			{target: journal, data: evidence.PackGenesisHash(), method: "genesisHash", parsed: evidenceABI, value: expected.Domain.GenesisHash},
			{target: journal, data: evidence.PackDeploymentIdHash(), method: "deploymentIdHash", parsed: evidenceABI, value: expected.Domain.DeploymentIDHash},
			{target: journal, data: evidence.PackActivation(digest), method: "activation", parsed: evidenceABI, value: publication},
		}
	} else {
		return nil, errors.New("activation fixture view has an unknown canonical snapshot")
	}
	for _, view := range views {
		if view.target != target || !bytes.Equal(view.data, data) {
			continue
		}
		encoded, err := view.parsed.Methods[view.method].Outputs.Pack(view.value)
		if err != nil {
			return nil, err
		}
		if self.fault == view.method+"-trailing" {
			encoded = append(encoded, 0)
		}
		if self.fault == "policyAt-padding" && view.method == "policyAt" {
			encoded[32] = 1
		}
		if self.fault == "operatorAt-padding" && view.method == "operatorAt" {
			encoded[5*32] = 1
		}
		return hexutil.Bytes(encoded), ctx.Err()
	}
	return nil, fmt.Errorf("activation fixture unknown target/calldata at %s", selector.BlockHash.Hex())
}

// Every read runs through the same actual production operation.
func (self *releaseActivationV2TestFixture) read(ctx context.Context) (VerifiedReleaseActivationV2, error) {
	self.nativeContext = new(sync.Once)
	return self.chain.AuthenticateReleaseActivationV2Context(ctx, self.native.chain, self.authority, self.authority.Expected, self.vpkSignature, self.hotkeySignature, self.block, self.blockHash)
}

// Real native/EVM observations preserve different numeric clocks and permit
// future-epoch scheduling; no UID number or signature is a history shortcut.
func TestReleaseActivationV2AuthenticatesRealReadersAndBothSignatures(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	got, err := fixture.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Publication.Record != fixture.authority.Expected || got.Publication.PublishedBlock != 1001 || got.Native.Identity.Hotkey != fixture.authority.Expected.Hotkey || got.Native.Identity.BlockNumber != 100 || !got.Native.MeetsNonSelfStakeAndPermit() || got.ObservedEVMBlock != fixture.block || got.ObservedEVMHash != fixture.blockHash {
		t.Fatalf("activation exact observed authority differs: %+v", got)
	}
	priorCalls, priorNative := fixture.calls.Load(), len(fixture.native.calls)
	replayed, err := fixture.read(t.Context())
	if err != nil || replayed != got || fixture.calls.Load() <= priorCalls || len(fixture.native.calls) <= priorNative {
		t.Fatalf("repeat observation was not reauthenticated: %+v %v", replayed, err)
	}
}

// Structural observer and endpoint identity failures precede both RPC owners.
func TestReleaseActivationV2RejectsObserverAndNativeIdentityBeforeRPC(t *testing.T) {
	cases := []func(*releaseActivationV2TestFixture){
		func(f *releaseActivationV2TestFixture) { f.authority.Journal = common.Address{} },
		func(f *releaseActivationV2TestFixture) {
			f.authority.Journal = common.Address(f.authority.Expected.Domain.Coordinator)
		},
		func(f *releaseActivationV2TestFixture) {
			f.authority.Journal = common.Address(f.authority.Expected.Domain.SettlementVault)
		},
		func(f *releaseActivationV2TestFixture) { f.authority.RuntimeHash = [32]byte{} },
		func(f *releaseActivationV2TestFixture) { f.authority.ValidatorUID = ^uint16(0) },
		func(f *releaseActivationV2TestFixture) { f.block = f.authority.Expected.EVMBlock },
		func(f *releaseActivationV2TestFixture) { f.blockHash = [32]byte{} },
		func(f *releaseActivationV2TestFixture) { f.native.chain.GenesisHash[31] ^= 1 },
	}
	for index, edit := range cases {
		fixture := newReleaseActivationV2TestFixture(t, "")
		before := fixture.calls.Load()
		edit(fixture)
		got, err := fixture.read(t.Context())
		if err == nil || got != (VerifiedReleaseActivationV2{}) || fixture.calls.Load() != before || len(fixture.native.calls) != 0 {
			t.Fatalf("observer drift %d reached RPC: %+v %v", index, got, err)
		}
	}
}

// A candidate may not choose its own expected namespace, even with consent.
func TestReleaseActivationV2RejectsEveryCandidateDriftBeforeRPC(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	before := fixture.calls.Load()
	cases := []func(*protocol.ValidatorEvidenceActivation){
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.ChainID++ },
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.GenesisHash[31] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.Netuid++ },
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.Coordinator[19] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.SettlementVault[19] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.DeploymentIDHash[0] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.PolicyHash[0] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.Domain.Epoch++ },
		func(a *protocol.ValidatorEvidenceActivation) { a.Hotkey[0] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.NoID++ },
		func(a *protocol.ValidatorEvidenceActivation) { a.VPK[0] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.NativeBlock++ },
		func(a *protocol.ValidatorEvidenceActivation) { a.NativeHash[0] ^= 1 },
		func(a *protocol.ValidatorEvidenceActivation) { a.EVMBlock++ },
		func(a *protocol.ValidatorEvidenceActivation) { a.EVMHash[0] ^= 1 },
	}
	for index, edit := range cases {
		candidate := fixture.authority.Expected
		edit(&candidate)
		got, err := fixture.chain.AuthenticateReleaseActivationV2Context(t.Context(), fixture.native.chain, fixture.authority, candidate, fixture.vpkSignature, fixture.hotkeySignature, fixture.block, fixture.blockHash)
		if err == nil || !strings.Contains(err.Error(), "differs from expected authority") || got != (VerifiedReleaseActivationV2{}) {
			t.Fatalf("candidate drift %d accepted or misclassified: %+v %v", index, got, err)
		}
	}
	if fixture.calls.Load() != before || len(fixture.native.calls) != 0 {
		t.Fatal("candidate drift reached chain RPC")
	}
}

// Both independent signatures are required before any historical observation.
func TestReleaseActivationV2RejectsEitherSignatureBeforeRPC(t *testing.T) {
	for _, hotkey := range []bool{false, true} {
		fixture := newReleaseActivationV2TestFixture(t, "")
		before := fixture.calls.Load()
		if hotkey {
			fixture.hotkeySignature[0] ^= 1
		} else {
			fixture.vpkSignature[0] ^= 1
		}
		got, err := fixture.read(t.Context())
		if err == nil || got != (VerifiedReleaseActivationV2{}) || fixture.calls.Load() != before || len(fixture.native.calls) != 0 {
			t.Fatalf("invalid consent reached RPC: %v", err)
		}
	}
}

// Good signatures cannot hide a changed runtime, anchor, policy or inclusion.
func TestReleaseActivationV2RejectsHistoricalEVMDrift(t *testing.T) {
	for _, fault := range []string{"nonfinalized", "historical-reorg", "past-epoch", "wrong-policy", "future-policy", "inactive-operator", "future-operator", "foreign-runtime", "foreign-anchor", "absent-activation", "currentEpoch-trailing", "policyAt-trailing", "policyAt-padding", "operatorAt-trailing", "operatorAt-padding"} {
		fixture := newReleaseActivationV2TestFixture(t, fault)
		got, err := fixture.read(t.Context())
		if err == nil || got != (VerifiedReleaseActivationV2{}) {
			t.Fatalf("%s accepted partial history: %+v %v", fault, got, err)
		}
	}
}

// The actual runtime's weighted stake and permit predicates remain mandatory.
func TestReleaseActivationV2RejectsHistoricalNativeStakeAndPermit(t *testing.T) {
	for _, lowStake := range []bool{false, true} {
		fixture := newReleaseActivationV2TestFixture(t, "")
		if lowStake {
			fixture.native.total = fixture.native.threshold - 1
		} else {
			fixture.native.permit = false
		}
		got, err := fixture.read(t.Context())
		if err == nil || !strings.Contains(err.Error(), "historical hotkey lacks") || got != (VerifiedReleaseActivationV2{}) {
			t.Fatalf("native authority failure was hidden: %+v %v", got, err)
		}
	}
}

// A registered subnet owner's runtime-defined exception is not accidentally
// removed by a second validator eligibility model in activation startup.
func TestReleaseActivationV2PreservesRegisteredNativeOwner(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	fixture.native.owner = true
	fixture.native.permit = false
	fixture.native.total = 0
	got, err := fixture.read(t.Context())
	if err != nil || !got.Native.SubnetOwnerRegistered || got.Native.SubnetOwnerUID != fixture.authority.ValidatorUID {
		t.Fatalf("registered owner activation: %+v %v", got, err)
	}
}

// This two-sided rendezvous can finish only when native and EVM work overlap.
func TestReleaseActivationV2OverlapsIndependentChainReaders(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	evmEntered, nativeEntered := make(chan struct{}), make(chan struct{})
	fixture.beforeFinalized = func(ctx context.Context) error {
		close(evmEntered)
		select {
		case <-nativeEntered:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	var entered sync.Once
	fixture.native.beforeCall = func(ctx context.Context, _ string) error {
		entered.Do(func() { close(nativeEntered) })
		select {
		case <-evmEntered:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if _, err := fixture.read(t.Context()); err != nil {
		t.Fatalf("independent readers did not overlap: %v", err)
	}
}

// A failed native reader cancels an already-entered real HTTP EVM request.
func TestReleaseActivationV2NativeFailureCancelsAndJoinsEVM(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	evmEntered, evmJoined := make(chan struct{}), make(chan struct{})
	failure := errors.New("activation-test-native-failure")
	fixture.beforeFinalized = func(ctx context.Context) error {
		close(evmEntered)
		defer close(evmJoined)
		<-ctx.Done()
		return ctx.Err()
	}
	fixture.native.beforeCall = func(ctx context.Context, _ string) error {
		select {
		case <-evmEntered:
			return failure
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	got, err := fixture.read(t.Context())
	if !errors.Is(err, failure) || got != (VerifiedReleaseActivationV2{}) {
		t.Fatalf("native failure lost or partial output returned: %+v %v", got, err)
	}
	select {
	case <-evmJoined:
	case <-t.Context().Done():
		t.Fatal("EVM handler did not join cancellation")
	}
}

// A failed EVM reader must also cancel and join the actual native adapter.
func TestReleaseActivationV2EVMFailureCancelsAndJoinsNative(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	nativeEntered, nativeJoined := make(chan struct{}), make(chan struct{})
	fixture.native.beforeCall = func(ctx context.Context, _ string) error {
		close(nativeEntered)
		defer close(nativeJoined)
		<-ctx.Done()
		return ctx.Err()
	}
	fixture.beforeFinalized = func(ctx context.Context) error {
		select {
		case <-nativeEntered:
			return errors.New("activation-test-EVM-failure")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	got, err := fixture.read(t.Context())
	if err == nil || !strings.Contains(err.Error(), "activation-test-EVM-failure") || got != (VerifiedReleaseActivationV2{}) {
		t.Fatalf("EVM failure lost or partial output returned: %+v %v", got, err)
	}
	select {
	case <-nativeJoined:
	default:
		t.Fatal("native operation returned before its reader joined")
	}
}

// Parent cancellation ends both owned branches without retaining either result.
func TestReleaseActivationV2ParentCancellationReturnsNoPartialAuthority(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	entered, joined := make(chan struct{}), make(chan struct{})
	fixture.beforeCode = func(ctx context.Context) error {
		close(entered)
		defer close(joined)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		value VerifiedReleaseActivationV2
		err   error
	}
	done := make(chan outcome, 1)
	go func() { value, err := fixture.read(ctx); done <- outcome{value: value, err: err} }()
	<-entered
	cancel()
	result := <-done
	if !errors.Is(result.err, context.Canceled) || result.value != (VerifiedReleaseActivationV2{}) {
		t.Fatalf("parent cancellation exposed authority: %+v %v", result.value, result.err)
	}
	select {
	case <-joined:
	case <-t.Context().Done():
		t.Fatal("canceled real HTTP reader did not join")
	}
}

// Pre-cancellation never launches either native or EVM work.
func TestReleaseActivationV2PreCancellationSendsNoRPC(t *testing.T) {
	fixture := newReleaseActivationV2TestFixture(t, "")
	before := fixture.calls.Load()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := fixture.read(ctx)
	if !errors.Is(err, context.Canceled) || got != (VerifiedReleaseActivationV2{}) || fixture.calls.Load() != before || len(fixture.native.calls) != 0 {
		t.Fatalf("pre-cancellation reached RPC: %+v %v", got, err)
	}
}
