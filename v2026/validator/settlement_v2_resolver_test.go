//go:build linux || darwin

// The settlement v2 weight destination is observed through real geth HTTP
// clients. vault2 and STSettlement2 answers are encoded by their ABI and exist
// only from their deployment block; faults change raw answers, never a verdict.
package validator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// Operator 1's v2 pool hotkey: a fresh registration with its own UID.
var settlementV2TestPoolHotkey = chainBatchHotkey(3)

// The decision fixture's coordinator and metagraph, plus the two v2
// contracts. The finalized head may lie above the decision block.
type settlementV2ResolverTestFixture struct {
	*releaseDecisionV2TestFixture
	addresses  SettlementV2Addresses
	head       uint64
	deployed   uint64
	metagraph  [][32]byte
	pools      map[uint64]stabi.ISTSettlementVault2Pool
	identities map[uint64]stabi.SettlementAtOutput
	pending    stabi.ISTSettlementVault2Pending
	raw        map[string][]byte
	v2Calls    map[string][]uint64
}

// Both operators are active in epoch 1; neither pool is registered in vault2
// until a test registers one. Zero source commitments keep live steering from
// reading payout artifacts.
func newSettlementV2ResolverTestFixture(t *testing.T) *settlementV2ResolverTestFixture {
	t.Helper()
	base := newReleaseDecisionV2TestFixture(t)
	fixture := &settlementV2ResolverTestFixture{releaseDecisionV2TestFixture: base,
		addresses: SettlementV2Addresses{Settlement: common.HexToAddress("0x5e77100000000000000000000000000000000002"), Vault: common.HexToAddress("0x7a17000000000000000000000000000000000002")},
		head:      base.finalized, pools: map[uint64]stabi.ISTSettlementVault2Pool{}, identities: map[uint64]stabi.SettlementAtOutput{}, raw: map[string][]byte{}, v2Calls: map[string][]uint64{}}
	fixture.setMetagraph(chainBatchHotkey(0), chainBatchHotkey(1), chainBatchHotkey(2), settlementV2TestPoolHotkey)
	coordinator := base.chain.coordinator
	for _, operator := range base.query.operators {
		base.set(t, "rootCommitments", coordinator.PackRootCommitments(big.NewInt(0), new(big.Int).SetUint64(operator.noID)), [32]byte{}, [32]byte{}, common.Address{}, uint64(0))
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
	base.chain = &ChainClient{client: client, coordinator: coordinator, contractAddr: base.chain.contractAddr, chainId: base.chain.chainId, release: true}
	return fixture
}

// UID index i holds hotkeys[i]; the binding hotkey stays at UID 2.
func (self *settlementV2ResolverTestFixture) setMetagraph(hotkeys ...[32]byte) {
	selector, netuid := evmSelector("getUidCount(uint16)"), evmUint16Word(self.query.domain.Netuid)
	self.views[fmt.Sprintf("%x", append(selector[:], netuid[:]...))] = releaseDecisionV2TestView{method: "getUidCount", data: new(big.Int).SetUint64(uint64(len(hotkeys))).FillBytes(make([]byte, 32))}
	for index, hotkey := range hotkeys {
		selector, uid := evmSelector("getHotkey(uint16,uint16)"), evmUint16Word(uint16(index))
		data := append(append(slices.Clone(selector[:]), netuid[:]...), uid[:]...)
		self.views[fmt.Sprintf("%x", data)] = releaseDecisionV2TestView{method: "getHotkey", data: slices.Clone(hotkey[:])}
	}
	self.metagraph = hotkeys
}

func (self *settlementV2ResolverTestFixture) hotkeyUIDs() map[[32]byte]uint16 {
	uids := map[[32]byte]uint16{}
	for index, hotkey := range self.metagraph {
		uids[hotkey] = uint16(index)
	}
	return uids
}

// registerOperatorPool's outcome: vault2's pool and STSettlement2's answer.
func (self *settlementV2ResolverTestFixture) registerPool(noID, firstEpoch uint64) {
	self.pools[noID] = stabi.ISTSettlementVault2Pool{Hotkey: settlementV2TestPoolHotkey, FirstEpoch: firstEpoch, Registered: true}
	self.identities[noID] = stabi.SettlementAtOutput{Vault: self.addresses.Vault, Hotkey: settlementV2TestPoolHotkey, Version: 2}
}

// The blocks at which a v2 method was read, in order.
func (self *settlementV2ResolverTestFixture) v2Reads(method string) []uint64 {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return slices.Clone(self.v2Calls[method])
}

// v2 contracts answer only from their deployment block; earlier, an eth_call
// to an address without code returns no bytes. Everything else is the base.
func (self *settlementV2ResolverTestFixture) Call(ctx context.Context, call map[string]hexutil.Bytes, selector gethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	var metadata *bind.MetaData
	switch common.BytesToAddress(call["to"]) {
	case self.addresses.Vault:
		metadata = &stabi.STSettlementVault2MetaData
	case self.addresses.Settlement:
		metadata = &stabi.STSettlement2MetaData
	default:
		return self.releaseDecisionV2TestFixture.Call(ctx, call, selector)
	}
	if selector.BlockHash == nil || selector.BlockNumber != nil || !selector.RequireCanonical {
		return nil, errors.New("settlement v2 test requires the exact canonical hash selector")
	}
	number, found := uint64(0), false
	for candidate, hash := range self.blocks {
		if common.Hash(hash) == *selector.BlockHash {
			number, found = candidate, true
		}
	}
	contractABI, err := metadata.ParseABI()
	if err != nil || !found {
		return nil, errors.Join(errors.New("settlement v2 test block or ABI is unknown"), err)
	}
	method, err := contractABI.MethodById(call["input"])
	if err != nil {
		return nil, err
	}
	arguments, err := method.Inputs.Unpack(call["input"][4:])
	if err != nil {
		return nil, err
	}
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.v2Calls[method.Name] = append(self.v2Calls[method.Name], number)
	}()
	if number < self.deployed {
		return hexutil.Bytes{}, ctx.Err()
	}
	if raw, found := self.raw[method.Name]; found {
		return slices.Clone(raw), ctx.Err()
	}
	noID := arguments[0].(*big.Int).Uint64()
	var encoded []byte
	switch method.Name {
	case "pools":
		encoded, err = method.Outputs.Pack(self.pools[noID])
	case "settlementAt":
		identity := self.identities[noID]
		encoded, err = method.Outputs.Pack(identity.Vault, identity.Hotkey, identity.Version)
	case "pending":
		encoded, err = method.Outputs.Pack(self.pending)
	case "approvalCount":
		encoded, err = method.Outputs.Pack(big.NewInt(0))
	default:
		return nil, fmt.Errorf("settlement v2 test has no %s view", method.Name)
	}
	return encoded, errors.Join(err, ctx.Err())
}

// The finalized head is the fixture's head; numbered blocks are the base's.
func (self *settlementV2ResolverTestFixture) GetBlockByNumber(ctx context.Context, number gethrpc.BlockNumber, full bool) (map[string]any, error) {
	if number == gethrpc.FinalizedBlockNumber {
		number = gethrpc.BlockNumber(self.head)
	}
	return self.releaseDecisionV2TestFixture.GetBlockByNumber(ctx, number, full)
}

func (self *settlementV2ResolverTestFixture) snapshot() *ReleaseSnapshot {
	block := self.query.boundary.EVMBlock
	return &ReleaseSnapshot{BlockNumber: block, BlockHash: self.blocks[block], Epoch: new(big.Int).SetUint64(self.query.boundary.SettlementEpoch), Policy: stabi.STCoordinatorPolicySnapshot{RootCommitWindowBlocks: self.query.policy.Settlement.RootCommitWindowBlocks}}
}

// Live steering for the decision epoch. Both operators are controlled, so the
// census needs no payout artifact or statistics input.
func (self *settlementV2ResolverTestFixture) steer(t *testing.T, addresses SettlementV2Addresses) ([]ReleasePoolMeasurement, error) {
	t.Helper()
	cfg := validReleaseConfig(t)
	cfg.ControlledNOIDs = []uint64{1, 2}
	steerer := &ReleaseSteerer{cfg: &cfg, chain: self.chain, contexts: map[uint64]*ReleaseMeasurementContext{1: {NoID: 1}, 2: {NoID: 2}}, settlementV2: addresses}
	var pools []ReleasePoolMeasurement
	_, _, _, err := steerer.gatherPools(t.Context(), self.snapshot(), map[uint64]map[connect.Id]bool{}, self.hotkeyUIDs(), &pools)
	return pools, err
}

// Replay, audits and the production row read this same observation.
func (self *settlementV2ResolverTestFixture) replay(t *testing.T, addresses SettlementV2Addresses) ([]ReleasePoolMeasurement, error) {
	t.Helper()
	query := self.query
	query.settlementV2 = addresses
	observed, err := self.chain.readReleaseDecisionChainV2Context(t.Context(), query)
	if err != nil {
		return nil, err
	}
	return observed.pools, nil
}

func settlementV2TestV1Pools() []ReleasePoolMeasurement {
	return []ReleasePoolMeasurement{{NoID: 1, UID: 0, PoolHotkey: releaseHex32(chainBatchHotkey(0))}, {NoID: 2, UID: 1, PoolHotkey: releaseHex32(chainBatchHotkey(1))}}
}

// VA-1: live steering and replay resolve the same destination on both sides
// of a pool's first v2 epoch, so the production row's equality of the two pool
// censuses (recycle_operators.go) holds. Operator 2 is an unregistered pool and
// keeps operatorAt; settlementAt is read only from the first v2 epoch.
func TestSettlementV2DestinationAgreesAcrossSwitch(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name       string
		firstEpoch uint64
		v2         bool
	}{
		{name: "last v1 epoch", firstEpoch: 2},
		{name: "first v2 epoch", firstEpoch: 1, v2: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2ResolverTestFixture(t)
			fixture.registerPool(1, testCase.firstEpoch)
			live, err := fixture.steer(t, fixture.addresses)
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := fixture.replay(t, fixture.addresses)
			if err != nil {
				t.Fatal(err)
			}
			want, settlementReads := settlementV2TestV1Pools(), 0
			if testCase.v2 {
				want[0], settlementReads = ReleasePoolMeasurement{NoID: 1, UID: 3, PoolHotkey: releaseHex32(settlementV2TestPoolHotkey)}, 2
			}
			if !slices.Equal(live, want) || !slices.Equal(replayed, live) {
				t.Fatalf("live %+v and replayed %+v destinations differ from %+v", live, replayed, want)
			}
			block := fixture.query.boundary.EVMBlock
			if reads := fixture.v2Reads("pools"); !slices.Equal(reads, []uint64{block, block, block, block}) {
				t.Fatalf("vault2 pool reads %v", reads)
			}
			if reads := fixture.v2Reads("settlementAt"); len(reads) != settlementReads {
				t.Fatalf("settlementAt reads %v, want %d", reads, settlementReads)
			}
		})
	}
}

// VA-1: replay of an epoch before the switch reads the registration at the
// finalized head and never calls a contract absent at the decision block.
func TestSettlementV2ReplayBeforeDeploymentKeepsOperatorAt(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	fixture.head = fixture.query.boundary.EVMBlock + 7
	fixture.blocks[fixture.head] = [32]byte{0x3f}
	fixture.deployed = fixture.head
	fixture.registerPool(1, 2)
	replayed, err := fixture.replay(t, fixture.addresses)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(replayed, settlementV2TestV1Pools()) {
		t.Fatalf("replayed destinations %+v", replayed)
	}
	if reads := fixture.v2Reads("pools"); !slices.Equal(reads, []uint64{fixture.head, fixture.head}) || len(fixture.v2Reads("settlementAt")) != 0 {
		t.Fatalf("v2 reads pools=%v settlementAt=%v", reads, fixture.v2Reads("settlementAt"))
	}
}

// VA-1: from the switch a pruned v1 pool hotkey is not consulted. The same
// chain state without v2 addresses keeps today's refusals at both sites.
func TestSettlementV2PrunedV1PoolUIDCausesNoError(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	fixture.setMetagraph(chainBatchHotkey(9), chainBatchHotkey(1), chainBatchHotkey(2), settlementV2TestPoolHotkey)
	fixture.registerPool(1, 1)
	live, err := fixture.steer(t, fixture.addresses)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := fixture.replay(t, fixture.addresses)
	if err != nil {
		t.Fatal(err)
	}
	want := settlementV2TestV1Pools()
	want[0] = ReleasePoolMeasurement{NoID: 1, UID: 3, PoolHotkey: releaseHex32(settlementV2TestPoolHotkey)}
	if !slices.Equal(live, want) || !slices.Equal(replayed, want) {
		t.Fatalf("live %+v replayed %+v", live, replayed)
	}
	if _, err := fixture.steer(t, SettlementV2Addresses{}); err == nil || !strings.Contains(err.Error(), "active no_id 1 pool hotkey has no live UID") {
		t.Fatalf("v1 steering accepted a pruned pool hotkey: %v", err)
	}
	if _, err := fixture.replay(t, SettlementV2Addresses{}); err == nil || !strings.Contains(err.Error(), "decision active operator identity or unique pool UID is absent") {
		t.Fatalf("v1 replay accepted a pruned pool hotkey: %v", err)
	}
}

// VA-1: a settlementAt answer that differs from vault2's immutable pool in
// any field is refused by live steering and by replay.
func TestSettlementV2RefusesDifferentSettlementAt(t *testing.T) {
	t.Parallel()
	vault := common.HexToAddress("0x7a17000000000000000000000000000000000002")
	for name, identity := range map[string]stabi.SettlementAtOutput{
		"hotkey":  {Vault: vault, Hotkey: chainBatchHotkey(1), Version: 2},
		"vault":   {Vault: common.HexToAddress("0x7a17000000000000000000000000000000000003"), Hotkey: settlementV2TestPoolHotkey, Version: 2},
		"version": {Vault: vault, Hotkey: settlementV2TestPoolHotkey, Version: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2ResolverTestFixture(t)
			fixture.registerPool(1, 1)
			fixture.identities[1] = identity
			if _, err := fixture.steer(t, fixture.addresses); err == nil || !strings.Contains(err.Error(), "no_id 1 epoch 1 settlementAt differs from vault2's immutable pool") {
				t.Fatalf("live steering accepted %s: %v", name, err)
			}
			if _, err := fixture.replay(t, fixture.addresses); err == nil || !strings.Contains(err.Error(), "no_id 1 epoch 1 settlementAt differs from vault2's immutable pool") {
				t.Fatalf("replay accepted %s: %v", name, err)
			}
		})
	}
}

// VA-1: without the settlement_v2 block no v2 contract is read, and both
// sites keep operatorAt even for a pool vault2 registered.
func TestSettlementV2AbsentKeepsOperatorAt(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	fixture.registerPool(1, 1)
	live, err := fixture.steer(t, SettlementV2Addresses{})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := fixture.replay(t, SettlementV2Addresses{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(live, settlementV2TestV1Pools()) || !slices.Equal(replayed, live) || len(fixture.v2Reads("pools"))+len(fixture.v2Reads("settlementAt")) != 0 {
		t.Fatalf("live %+v replayed %+v", live, replayed)
	}
	if releaseSettlementV2Addresses(&ReleaseConfig{}).configured() {
		t.Fatal("a configuration without the settlement_v2 block names v2 contracts")
	}
}

// VA-1: an absent, short or noncanonical vault2 or STSettlement2 answer is an
// error, never a fallback to operatorAt.
func TestSettlementV2ResolverRefusesUnreadableViews(t *testing.T) {
	t.Parallel()
	canonical := func(method string, values ...any) []byte {
		contractABI, err := stabi.STSettlementVault2MetaData.ParseABI()
		if method == "settlementAt" {
			contractABI, err = stabi.STSettlement2MetaData.ParseABI()
		}
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := contractABI.Methods[method].Outputs.Pack(values...)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	pool := canonical("pools", stabi.ISTSettlementVault2Pool{Hotkey: settlementV2TestPoolHotkey, FirstEpoch: 1, Registered: true})
	identity := canonical("settlementAt", common.HexToAddress("0x7a17000000000000000000000000000000000002"), settlementV2TestPoolHotkey, uint8(2))
	dirty := slices.Clone(identity)
	dirty[0] = 1
	for _, testCase := range []struct {
		name, method, message string
		raw                   []byte
	}{
		{name: "pools absent", method: "pools", raw: []byte{}, message: "no_id 1 vault2 pool: eth_call at canonical block"},
		{name: "pools short", method: "pools", raw: pool[:64], message: "no_id 1 vault2 pool: settlement v2 pools view:"},
		{name: "pools trailing", method: "pools", raw: append(slices.Clone(pool), make([]byte, 32)...), message: "settlement v2 pools view is noncanonical"},
		{name: "settlementAt absent", method: "settlementAt", raw: []byte{}, message: "no_id 1 epoch 1 settlementAt: eth_call at canonical block"},
		{name: "settlementAt dirty padding", method: "settlementAt", raw: dirty, message: "settlement v2 settlementAt view is noncanonical"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fixture := newSettlementV2ResolverTestFixture(t)
			fixture.registerPool(1, 1)
			fixture.raw[testCase.method] = testCase.raw
			block := fixture.query.boundary.EVMBlock
			hotkey, err := fixture.chain.releasePoolHotkeyContext(t.Context(), fixture.addresses, block, fixture.blocks[block], block, fixture.blocks[block], 1, 1, chainBatchHotkey(0))
			if err == nil || hotkey != ([32]byte{}) || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("hotkey %x error %v", hotkey, err)
			}
		})
	}
}

// VA-1: the view reader refuses an unparsable ABI or arguments it cannot
// encode before any RPC.
func TestSettlementV2ViewReaderRefusesBadRequest(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	block := fixture.query.boundary.EVMBlock
	if _, err := fixture.chain.settlementV2ViewAtHashContext(t.Context(), fixture.addresses.Vault, &bind.MetaData{ABI: "not an abi"}, "pools", []any{big.NewInt(1)}, block, fixture.blocks[block]); err == nil {
		t.Fatal("an unparsable ABI was accepted")
	}
	if _, err := fixture.chain.settlementV2ViewAtHashContext(t.Context(), fixture.addresses.Vault, &stabi.STSettlementVault2MetaData, "pools", []any{"one"}, block, fixture.blocks[block]); err == nil {
		t.Fatal("an unencodable argument was accepted")
	}
	if len(fixture.v2Reads("pools")) != 0 {
		t.Fatal("a refused request reached RPC")
	}
}

// VA-5: pool weight inputs read no root lifecycle state. With one operator,
// its pool keeps the whole pool channel whether vault2 holds a pending root
// the validator refused to approve or the root was deleted.
func TestSettlementV2RefusedOrDeletedRootKeepsPoolWeight(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	fixture.registerPool(1, 1)
	fixture.set(t, "operatorCount", fixture.chain.coordinator.PackOperatorCount(), big.NewInt(1))
	cfg := validReleaseConfig(t)
	cfg.Policy = zeroPricePolicy(t)
	cfg.Policy.Safety.MinimumHealthyNOCount = 1
	stats := ReleaseStatsMeasurement{Config: ReleaseStatsConfig{AMin: 8, AlphaNumerator: 1, AlphaDenominator: 2, LatRefMillis: 100},
		Providers: []ReleaseProviderMeasurement{{ClientID: releaseMeasurementTestID(1).String(), LatencyBuckets: make([]uint64, statsLatencyBuckets), HasPriorQuality: true, PriorQualityPPM: 900_000}}}
	for _, pending := range []stabi.ISTSettlementVault2Pending{
		{Seq: 4, Root: [32]byte{1}, Total: big.NewInt(9), LeafCount: 1, Epoch: 1, LedgerHash: [32]byte{2}, CommittedBlock: 5},
		{Total: big.NewInt(0)},
	} {
		fixture.pending = pending
		steerer := &ReleaseSteerer{cfg: &cfg, chain: fixture.chain, contexts: map[uint64]*ReleaseMeasurementContext{1: {NoID: 1}}, headInputsByNO: map[uint64]ReleaseMeasurementInput{1: {NoID: 1, Stats: stats}}, settlementV2: fixture.addresses}
		var observations []ReleasePoolMeasurement
		pools, masked, _, err := steerer.gatherPools(t.Context(), fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), &observations)
		if err != nil {
			t.Fatal(err)
		}
		if len(pools) != 1 || pools[0].UID != 3 || pools[0].Score.Cmp(big.NewRat(9, 10)) != 0 || len(masked) != 0 {
			t.Fatalf("pool weight inputs %+v masked %v", pools, masked)
		}
		uids, scores, err := BuildWeightVectorExact(pools, nil, cfg.Policy.Steering.Theta, masked)
		if err != nil || !slices.Equal(uids, []uint16{3}) || scores[0].Cmp(big.NewRat(1, 1)) != 0 {
			t.Fatalf("weights %v %v: %v", uids, scores, err)
		}
	}
	if len(fixture.v2Reads("pending"))+len(fixture.v2Reads("approvalCount")) != 0 {
		t.Fatal("pool weight read the v2 root lifecycle")
	}
}

// VA-1: the shared resolver at the finalized head, as live steering reads it:
// the operator's hotkey before the pool's first v2 epoch and vault2's pool
// hotkey from it, both read at the head; an unreadable head refuses.
func TestSettlementV2PoolHotkeyAtFinalizedHead(t *testing.T) {
	t.Parallel()
	fixture := newSettlementV2ResolverTestFixture(t)
	fixture.registerPool(1, 2)
	operator := chainBatchHotkey(0)
	for _, test := range []struct {
		epoch uint64
		want  [32]byte
	}{{1, operator}, {2, settlementV2TestPoolHotkey}} {
		if hotkey, err := fixture.chain.SettlementV2PoolHotkeyContext(t.Context(), fixture.addresses, 1, test.epoch, operator); err != nil || hotkey != test.want {
			t.Fatalf("epoch %d resolves to 0x%x: %v", test.epoch, hotkey, err)
		}
	}
	if pools, identities := fixture.v2Reads("pools"), fixture.v2Reads("settlementAt"); !slices.Equal(pools, []uint64{fixture.head, fixture.head}) || !slices.Equal(identities, []uint64{fixture.head}) {
		t.Fatalf("v2 reads pools=%v settlementAt=%v", pools, identities)
	}
	fixture.head++
	if _, err := fixture.chain.SettlementV2PoolHotkeyContext(t.Context(), fixture.addresses, 1, 2, operator); err == nil || !strings.Contains(err.Error(), "finalized EVM head") {
		t.Fatalf("an unreadable finalized head resolved: %v", err)
	}
}
