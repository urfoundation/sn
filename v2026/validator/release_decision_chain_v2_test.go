//go:build linux || darwin

// Actual geth HTTP clients consume independently encoded coordinator state.
// Faults alter real RPC bytes, census members or finality, not a verifier result.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// One independently encoded fixed-width coordinator or metagraph response.
type releaseDecisionV2TestView struct {
	method string
	data   []byte
}

// View bytes are frozen before a read. Counters and finality-step selection
// use one fixture mutex; no reader callback runs under that mutex.
type releaseDecisionV2TestFixture struct {
	query           releaseDecisionChainV2Query
	chain           *ChainClient
	views           map[string]releaseDecisionV2TestView
	blocks          map[uint64][32]byte
	finalized       uint64
	regressFinality bool
	retargetSource  bool
	before          func(context.Context, string) error
	stateLock       sync.Mutex
	calls           map[string]int
	finalizedReads  int
	sourceReads     int
	commitments     []stabi.RootCommitmentsOutput
	versions        []stabi.STCoordinatorOperatorVersion
	bigConviction   *big.Int
}

// All typed tuples are encoded by the checked-in ABI. Huge conviction values
// exercise exact uint256 arithmetic without manufacturing a weight decision.
func newReleaseDecisionV2TestFixture(t *testing.T) *releaseDecisionV2TestFixture {
	t.Helper()
	cfg := validReleaseConfig(t)
	hash, err := cfg.Policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	start := uint64(10) + cfg.Policy.Settlement.EpochBlocks
	block := start + 5
	fixture := &releaseDecisionV2TestFixture{views: map[string]releaseDecisionV2TestView{}, blocks: map[uint64][32]byte{10: {0x10}, start: {0x20}, block: {0x30}, block - 1: {0x31}}, finalized: block, calls: map[string]int{}, bigConviction: new(big.Int).Lsh(big.NewInt(1), 200)}
	fixture.query = releaseDecisionChainV2Query{domain: protocol.ValidatorEvidenceDomain{ChainID: cfg.ChainID, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: [20]byte(common.HexToAddress(cfg.Coordinator)), SettlementVault: [20]byte(common.HexToAddress(cfg.SettlementVault)), DeploymentIDHash: [32]byte{0x11}, PolicyHash: hash, ActivationHash: [32]byte{0x12}}, boundary: AttemptBoundary{SettlementEpoch: 1, EVMBlock: block, EVMBlockHash: releaseHex32(fixture.blocks[block])}, policy: cfg.Policy, maxOperators: 2, maxProviders: 8, maxControlBytes: 1024 * 1024,
		operators: []releaseDecisionChainV2OperatorQuery{{noID: 1, providerIDs: []connect.Id{releaseMeasurementTestID(1), releaseMeasurementTestID(2)}}, {noID: 2, providerIDs: []connect.Id{releaseMeasurementTestID(3)}}}}
	coordinator := stabi.NewSTCoordinator()
	fixture.set(t, "netuid", coordinator.PackNetuid(), fixture.query.domain.Netuid)
	fixture.set(t, "currentEpoch", coordinator.PackCurrentEpoch(), big.NewInt(1))
	policy := cfg.Policy
	fixture.set(t, "policyAt", coordinator.PackPolicyAt(big.NewInt(1)), stabi.STCoordinatorPolicySnapshot{PolicyHash: hash, EffectiveEpoch: 0, EffectiveBlock: 10, EpochBlocks: policy.Settlement.EpochBlocks, RootCommitWindowBlocks: policy.Settlement.RootCommitWindowBlocks, FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: policy.Settlement.CloseGraceBlocks, ClaimTTLEpochs: policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: policy.Settlement.ClaimGraceEpochs, MaximumBindingValidityEpochs: policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: 10, EpochDepositCapRao: new(big.Int).SetUint64(policy.Deposit.EpochCapRaoPerOperator), CampaignDepositCapRao: new(big.Int).SetUint64(policy.Deposit.TotalTestCampaignCapRao)})
	fixture.set(t, "operatorCount", coordinator.PackOperatorCount(), big.NewInt(2))
	fixture.set(t, "epochStartBlock", coordinator.PackEpochStartBlock(big.NewInt(0)), big.NewInt(10))
	fixture.set(t, "epochStartBlock", coordinator.PackEpochStartBlock(big.NewInt(1)), new(big.Int).SetUint64(start))
	fixture.set(t, "epochEndBlock", coordinator.PackEpochEndBlock(big.NewInt(0)), new(big.Int).SetUint64(start))
	for index, operator := range fixture.query.operators {
		id := new(big.Int).SetUint64(operator.noID)
		fixture.set(t, "operatorIdAt", coordinator.PackOperatorIdAt(big.NewInt(int64(index))), id)
		version := stabi.STCoordinatorOperatorVersion{Coldkey: [32]byte{byte(0x40 + index)}, PoolHotkey: chainBatchHotkey(uint16(index)), DepositHotkey: [32]byte{byte(0x50 + index)}, DepositSigner: common.Address{byte(0x60 + index)}, RootSigner: common.Address{byte(0x70 + index)}, Active: true}
		fixture.versions = append(fixture.versions, version)
		fixture.set(t, "operatorAt", coordinator.PackOperatorAt(id, big.NewInt(1)), version)
		fixture.set(t, "operatorAt", coordinator.PackOperatorAt(id, big.NewInt(0)), version)
		fixture.set(t, "epochDeposits", coordinator.PackEpochDeposits(big.NewInt(1), id), big.NewInt(15))
		fixture.set(t, "epochConvictionAdded", coordinator.PackEpochConvictionAdded(big.NewInt(1), id), big.NewInt(7))
		fixture.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(id), new(big.Int).Add(new(big.Int).Set(fixture.bigConviction), big.NewInt(22)))
		commitment := stabi.RootCommitmentsOutput{PayoutRoot: [32]byte{byte(0x80 + index)}, ArtifactHash: [32]byte{byte(0x90 + index)}, Committer: version.RootSigner, CommitBlock: start + 1}
		fixture.commitments = append(fixture.commitments, commitment)
		fixture.set(t, "rootCommitments", coordinator.PackRootCommitments(big.NewInt(0), id), commitment.PayoutRoot, commitment.ArtifactHash, commitment.Committer, commitment.CommitBlock)
		for _, clientID := range operator.providerIDs {
			record := stabi.STCoordinatorBindingRecord{FleetId: [32]byte{0xa1}, Hotkey: chainBatchHotkey(2), ClientKey: [32]byte{0xa2}, CommitmentHash: [32]byte{0xa3}, Generation: 4, ValidFromEpoch: 0, ValidToEpoch: 2, Uid: 2}
			fixture.set(t, "bindingAt", coordinator.PackBindingAt([16]byte(clientID), big.NewInt(1)), true, record)
		}
	}
	selector, netuid := evmSelector("getUidCount(uint16)"), evmUint16Word(fixture.query.domain.Netuid)
	fixture.views[fmt.Sprintf("%x", append(selector[:], netuid[:]...))] = releaseDecisionV2TestView{method: "getUidCount", data: new(big.Int).SetUint64(3).FillBytes(make([]byte, 32))}
	for index := uint16(0); index < 3; index++ {
		selector, uid := evmSelector("getHotkey(uint16,uint16)"), evmUint16Word(index)
		data := append(append(slices.Clone(selector[:]), netuid[:]...), uid[:]...)
		hotkey := chainBatchHotkey(index)
		fixture.views[fmt.Sprintf("%x", data)] = releaseDecisionV2TestView{method: "getHotkey", data: slices.Clone(hotkey[:])}
	}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", fixture); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	client, err := ethclient.Dial(httpServer.URL)
	if err != nil {
		httpServer.Close()
		server.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); httpServer.Close(); server.Stop() })
	fixture.chain = &ChainClient{client: client, coordinator: coordinator, contractAddr: common.Address(fixture.query.domain.Coordinator), chainId: new(big.Int).SetUint64(fixture.query.domain.ChainID), release: true}
	return fixture
}

// This changes only frozen server-owned bytes before the tested call begins.
func (self *releaseDecisionV2TestFixture) set(t *testing.T, method string, calldata []byte, values ...any) {
	t.Helper()
	parsed, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := parsed.Methods[method].Outputs.Pack(values...)
	if err != nil {
		t.Fatal(err)
	}
	self.views[fmt.Sprintf("%x", calldata)] = releaseDecisionV2TestView{method: method, data: encoded}
}

// The real EIP-1898 selector must identify the independent fixture boundary.
func (self *releaseDecisionV2TestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical || *selector.BlockHash != common.Hash(self.blocks[self.finalized]) {
		return nil, errors.New("decision test requires the exact canonical hash selector")
	}
	calldata := call["input"]
	if len(calldata) == 0 {
		calldata = call["data"]
	}
	// Hexutil bytes format through String(), so format verbs do not identify
	// the original calldata bytes used by the independently encoded views.
	view, found := self.views[hex.EncodeToString(calldata)]
	if !found {
		return nil, fmt.Errorf("decision test has no independently defined view: target=%s calldata_bytes=%d prefix=%s", common.BytesToAddress(call["to"]).Hex(), len(calldata), hexutil.Encode(calldata[:min(len(calldata), 100)]))
	}
	want := common.Address(self.query.domain.Coordinator)
	if strings.HasPrefix(view.method, "get") {
		want = metagraphAddress
	}
	if common.BytesToAddress(call["to"]) != want {
		return nil, errors.New("decision test contract target differs")
	}
	func() { self.stateLock.Lock(); defer self.stateLock.Unlock(); self.calls[view.method]++ }()
	if self.before != nil {
		if err := self.before(ctx, view.method); err != nil {
			return nil, err
		}
	}
	return slices.Clone(view.data), ctx.Err()
}

// The second finalized read can deterministically regress without sleeps.
func (self *releaseDecisionV2TestFixture) GetBlockByNumber(ctx context.Context, number gethrpc.BlockNumber, full bool) (map[string]any, error) {
	block := uint64(number)
	if number == gethrpc.FinalizedBlockNumber {
		block = func() uint64 {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.finalizedReads++
			if self.regressFinality && self.finalizedReads > 1 {
				return self.finalized - 1
			}
			return self.finalized
		}()
	}
	hash, found := self.blocks[block]
	if !found || full {
		return nil, errors.New("decision test requested an unknown block")
	}
	if block == 10 {
		hash = func() [32]byte {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.sourceReads++
			if self.retargetSource && self.sourceReads > 1 {
				hash[0] ^= 1
			}
			return hash
		}()
	}
	return map[string]any{"number": hexutil.EncodeUint64(block), "hash": common.Hash(hash)}, ctx.Err()
}

// Height/hash identity comes from the actual RPC response, never test cache.
func (self *releaseDecisionV2TestFixture) GetBlockByHash(ctx context.Context, hash common.Hash, full bool) (map[string]any, error) {
	for number, expected := range self.blocks {
		if hash == common.Hash(expected) {
			return self.GetBlockByNumber(ctx, gethrpc.BlockNumber(number), full)
		}
	}
	return nil, errors.New("decision test requested an unknown hash")
}

// Requests and terminal results are inspected only after actual readers join.
func (self *releaseDecisionV2TestFixture) count(method string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.calls[method]
}

// Both actual Json calldata fields preserve raw selectors across geth's
// named byte type, including a precompile target and parameterized method.
func TestReleaseEvidenceV2DecisionDispatchPreservesActualHttpCalldata(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	coordinator := fixture.chain.coordinator
	selector, netuid := evmSelector("getUidCount(uint16)"), evmUint16Word(fixture.query.domain.Netuid)
	target := common.Address(fixture.query.domain.Coordinator)
	hash := common.Hash(fixture.blocks[fixture.finalized])
	for _, field := range []string{"input", "data"} {
		for _, input := range []struct {
			target common.Address
			data   []byte
			value  uint64
		}{
			{target: target, data: coordinator.PackNetuid(), value: uint64(fixture.query.domain.Netuid)},
			{target: target, data: coordinator.PackCurrentEpoch(), value: 1},
			{target: target, data: coordinator.PackOperatorIdAt(big.NewInt(1)), value: 2},
			{target: metagraphAddress, data: append(slices.Clone(selector[:]), netuid[:]...), value: 3},
		} {
			var encoded hexutil.Bytes
			call := map[string]any{"to": input.target, field: hexutil.Bytes(input.data)}
			err := fixture.chain.client.Client().CallContext(t.Context(), &encoded, "eth_call", call, gethrpc.BlockNumberOrHashWithHash(hash, true))
			expected := new(big.Int).SetUint64(input.value).FillBytes(make([]byte, 32))
			if err != nil || !bytes.Equal(encoded, expected) {
				t.Fatalf("%s selector %s differs from independent encoded value: %v", field, hexutil.Encode(input.data), err)
			}
		}
	}
	for _, method := range []string{"netuid", "currentEpoch", "operatorIdAt", "getUidCount"} {
		if fixture.count(method) != 2 {
			t.Fatalf("%s actual request census differs: %d", method, fixture.count(method))
		}
	}
}

// Exact byte dispatch still refuses changed selectors, appended calldata and
// a valid selector aimed at the wrong contract; refusals cannot poison a read.
func TestReleaseEvidenceV2DecisionDispatchRejectsForeignActualHttpCalls(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	calldata := fixture.chain.coordinator.PackNetuid()
	target := common.Address(fixture.query.domain.Coordinator)
	hash := fixture.blocks[fixture.finalized]
	for _, fault := range []string{"selector", "trailing-data", "target"} {
		changed, changedTarget := slices.Clone(calldata), target
		message := "decision test has no independently defined view:"
		switch fault {
		case "selector":
			changed[0] ^= 1
		case "trailing-data":
			changed = append(changed, 0)
		case "target":
			changedTarget[0] ^= 1
			message = "decision test contract target differs"
		}
		encoded, err := fixture.chain.ethCallAtHashContext(t.Context(), changedTarget, changed, fixture.finalized, hash)
		if err == nil || encoded != nil || !strings.Contains(err.Error(), message) {
			t.Fatalf("%s call did not retain its exact refusal: %v", fault, err)
		}
		if fault != "target" && !strings.Contains(err.Error(), "prefix="+hexutil.Encode(changed)) {
			t.Fatalf("%s diagnostic lost the actual calldata bytes: %v", fault, err)
		}
		encoded, err = fixture.chain.ethCallAtHashContext(t.Context(), target, calldata, fixture.finalized, hash)
		expected := new(big.Int).SetUint64(uint64(fixture.query.domain.Netuid)).FillBytes(make([]byte, 32))
		if err != nil || !bytes.Equal(encoded, expected) {
			t.Fatalf("%s refusal poisoned the actual production reader: %v", fault, err)
		}
	}
	if fixture.count("netuid") != 3 {
		t.Fatalf("foreign calls entered the authenticated census: %d", fixture.count("netuid"))
	}
}

// Every configured member and exact financial/source value survives the join.
func TestReleaseEvidenceV2DecisionReadsCompletePinnedCensuses(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.hotkeyUIDs) != 3 || len(observed.operators) != 2 || len(observed.pools) != 2 || len(observed.bindings) != 3 || fixture.count("getHotkey") != 3 || fixture.count("operatorIdAt") != 2 || fixture.count("bindingAt") != 3 || fixture.count("rootCommitments") != 2 {
		t.Fatal("actual complete provider/miner/operator/source census differs")
	}
	for index, operator := range observed.operators {
		if operator.noID != uint64(index+1) || operator.convictionBefore.Cmp(fixture.bigConviction) != 0 || operator.commitment != fixture.commitments[index] || operator.sourceVersion != fixture.versions[index] {
			t.Fatal("pinned financial source lost exact uint256 or historical identity")
		}
	}
	for _, binding := range observed.bindings {
		if binding.LocalClientKey != releaseHex32([32]byte{}) || binding.ClientKey != releaseHex32([32]byte{0xa2}) || !binding.LiveUIDFound || binding.LiveUID != 2 {
			t.Fatal("chain observation fabricated a historical API key or lost binding identity")
		}
	}
}

// Caller-side census and allowance faults fail before the first actual RPC.
func TestReleaseEvidenceV2DecisionRejectsConfiguredCensusDriftBeforeRPC(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"operator-order", "provider-order", "cross-owner", "provider-bound", "control-bound", "domain"} {
		fixture := newReleaseDecisionV2TestFixture(t)
		query := fixture.query
		switch fault {
		case "operator-order":
			query.operators[1].noID = query.operators[0].noID
		case "provider-order":
			query.operators[0].providerIDs[1] = query.operators[0].providerIDs[0]
		case "cross-owner":
			query.operators[1].providerIDs[0] = query.operators[0].providerIDs[0]
		case "provider-bound":
			query.maxProviders = 1
		case "control-bound":
			query.maxControlBytes = 1
		case "domain":
			query.domain.PolicyHash[0] ^= 1
		}
		if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), query); err == nil || observed != nil || fixture.count("currentEpoch") != 0 || fixture.finalizedReads != 0 {
			t.Fatalf("%s reached actual RPC or published observations: %v", fault, err)
		}
	}
}

// The registry cannot substitute, repeat or add a configured operator.
func TestReleaseEvidenceV2DecisionRejectsRegistrySubstitution(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"count", "duplicate", "foreign"} {
		fixture := newReleaseDecisionV2TestFixture(t)
		coordinator := fixture.chain.coordinator
		if fault == "count" {
			fixture.set(t, "operatorCount", coordinator.PackOperatorCount(), big.NewInt(3))
		} else {
			value := int64(1)
			if fault == "foreign" {
				value = 9
			}
			fixture.set(t, "operatorIdAt", coordinator.PackOperatorIdAt(big.NewInt(1)), big.NewInt(value))
		}
		if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || observed != nil || fixture.count("getHotkey") != 0 {
			t.Fatalf("%s registry drift reached miner allocation or publication: %v", fault, err)
		}
	}
}

// Real coordinator bytes with a valid prefix still need one exact ABI shape.
func TestReleaseEvidenceV2DecisionRejectsNoncanonicalActualABI(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"policyAt", "bindingAt", "operatorAt", "rootCommitments"} {
		fixture := newReleaseDecisionV2TestFixture(t)
		for key, view := range fixture.views {
			if view.method == method {
				view.data = append(slices.Clone(view.data), make([]byte, 32)...)
				fixture.views[key] = view
			}
		}
		if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || observed != nil {
			t.Fatalf("%s noncanonical actual ABI reached publication: %v", method, err)
		}
	}
}

// Current arithmetic and historical root identity fail closed independently.
func TestReleaseEvidenceV2DecisionRejectsFinancialAndSourceDrift(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"underflow", "occupied-absence", "source-end", "source-signer", "future-commit"} {
		fixture := newReleaseDecisionV2TestFixture(t)
		coordinator := fixture.chain.coordinator
		switch fault {
		case "underflow":
			fixture.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(big.NewInt(1)), big.NewInt(21))
		case "source-end":
			fixture.set(t, "epochEndBlock", coordinator.PackEpochEndBlock(big.NewInt(0)), big.NewInt(11))
		default:
			commitment := fixture.commitments[0]
			if fault == "occupied-absence" {
				commitment.CommitBlock = 0
			}
			if fault == "source-signer" {
				commitment.Committer[0] ^= 1
			}
			if fault == "future-commit" {
				commitment.CommitBlock = fixture.finalized + 1
			}
			fixture.set(t, "rootCommitments", coordinator.PackRootCommitments(big.NewInt(0), big.NewInt(1)), commitment.PayoutRoot, commitment.ArtifactHash, commitment.Committer, commitment.CommitBlock)
		}
		if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || observed != nil {
			t.Fatalf("%s financial/source drift reached publication: %v", fault, err)
		}
	}
}

// Count admission must precede every proportional miner request allocation.
func TestReleaseEvidenceV2DecisionRejectsMinerCountBeforeMemberAllocation(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	for key, view := range fixture.views {
		if view.method == "getUidCount" {
			view.data = new(big.Int).SetUint64(uint64(releaseNativeValidatorMaximumUIDs)).FillBytes(make([]byte, 32))
			fixture.views[key] = view
		}
	}
	if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || observed != nil || fixture.count("getHotkey") != 0 {
		t.Fatalf("unfunded complete miner census reached member allocation: %v", err)
	}
}

// A real RPC callback cannot retarget the retained request's borrowed slices.
func TestReleaseEvidenceV2DecisionOwnsCensusBeforeActualRPC(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	query := fixture.query
	fixture.before = func(_ context.Context, method string) error {
		if method == "netuid" {
			query.operators[0].noID = 99
			query.operators[0].providerIDs[0] = connect.Id{0xff}
			query.policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB++
		}
		return nil
	}
	observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), query)
	if err != nil || observed == nil || observed.operators[0].noID != 1 || observed.bindings[0].ClientID != releaseMeasurementTestID(1).String() {
		t.Fatalf("actual RPC callback retargeted owned decision input: %v", err)
	}
}

// Late real finality or cancellation discards the complete partial observation.
func TestReleaseEvidenceV2DecisionLateFinalityAndCancellationDiscardAll(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	fixture.regressFinality = true
	if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || observed != nil || fixture.count("rootCommitments") != 2 {
		t.Fatalf("late real finality regression retained partial authority: %v", err)
	}
	canceled := newReleaseDecisionV2TestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	canceled.before = func(_ context.Context, method string) error {
		if method == "rootCommitments" {
			cancel()
		}
		return nil
	}
	if observed, err := canceled.chain.readReleaseDecisionChainV2Context(ctx, canceled.query); !errors.Is(err, context.Canceled) || observed != nil {
		t.Fatalf("late real reader cancellation retained decision observations: %v", err)
	}
}

// The helper must neither sort an adversarial request nor use an incomplete
// provider subset. The same API now backs actual live compact head collection.
func TestReleaseEvidenceV2DecisionBindingAdmissionPrecedesActualRPC(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	ids := [][16]byte{[16]byte(releaseMeasurementTestID(2)), [16]byte(releaseMeasurementTestID(1))}
	before := slices.Clone(ids)
	if rows, err := fixture.chain.readReleaseProviderBindingsV2Context(t.Context(), fixture.query.boundary.EVMBlock, common.HexToHash(fixture.query.boundary.EVMBlockHash), ids, 1, 8); err == nil || rows != nil || fixture.count("bindingAt") != 0 {
		t.Fatalf("noncanonical binding caller reached real RPC: %v", err)
	}
	if !slices.Equal(ids, before) {
		t.Fatal("binding admission mutated the original census")
	}
}

// A full count does not establish complete membership when real identities
// are repeated, absent or encoded with extra bytes.
func TestReleaseEvidenceV2DecisionRejectsMinerIdentitySubstitution(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"duplicate", "zero", "suffix"} {
		fixture := newReleaseDecisionV2TestFixture(t)
		selector, netuid, uid := evmSelector("getHotkey(uint16,uint16)"), evmUint16Word(fixture.query.domain.Netuid), evmUint16Word(2)
		key := fmt.Sprintf("%x", append(append(slices.Clone(selector[:]), netuid[:]...), uid[:]...))
		view := fixture.views[key]
		switch fault {
		case "duplicate":
			hotkey := chainBatchHotkey(0)
			view.data = slices.Clone(hotkey[:])
		case "zero":
			view.data = make([]byte, 32)
		case "suffix":
			view.data = append(slices.Clone(view.data), make([]byte, 32)...)
		}
		fixture.views[key] = view
		if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || observed != nil || fixture.count("bindingAt") != 0 {
			t.Fatalf("%s miner identity reached binding authority or publication: %v", fault, err)
		}
	}
}

// The second actual source-header read must agree even when the current
// decision hash and every canonical coordinator view remain unchanged.
func TestReleaseEvidenceV2DecisionRejectsLateSourceHeaderSubstitution(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	fixture.retargetSource = true
	if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || !strings.Contains(err.Error(), "source boundary hash changed") || observed != nil || fixture.count("rootCommitments") != 2 {
		t.Fatalf("late source-header substitution retained a decision: %v", err)
	}
}

// The original allowance covers every owned provider copy and the complete
// miner census. One byte less is rejected before any miner-member request.
func TestReleaseEvidenceV2DecisionControlAllowanceHasExactBoundary(t *testing.T) {
	t.Parallel()
	fixture := newReleaseDecisionV2TestFixture(t)
	_, budget, err := ownReleaseDecisionChainV2Query(t.Context(), fixture.query)
	if err != nil {
		t.Fatal(err)
	}
	width := uint64(reflect.TypeFor[chainBatchCall]().Size()) + 68 + uint64(reflect.TypeFor[[]byte]().Size()) + 32 + 32 + 2
	fixture.query.maxControlBytes = budget.used + 3*width
	if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err != nil || observed == nil {
		t.Fatalf("exact admitted decision payload was refused: %v", err)
	}
	fixture.query.maxControlBytes--
	before := fixture.count("getHotkey")
	if observed, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), fixture.query); err == nil || observed != nil || fixture.count("getHotkey") != before {
		t.Fatalf("one-byte-deficient decision allowance reached miner requests: %v", err)
	}
}
