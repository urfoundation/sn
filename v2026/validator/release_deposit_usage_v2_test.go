//go:build linux || darwin

// Priced epochs under settlement v2, through real geth HTTP clients and a real
// operator HTTP endpoint. Faults change chain logs, ledger objects and published
// statements, never a verdict. Every run checks that the audit survives the pure
// measurement verifier and that replay reproduces it without the operator's API.
package validator

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/demand/pricesheet"
	"github.com/urfoundation/sn/v2026/demand/usage"
	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

const depositPriceTestChainID uint64 = 945

var depositPriceTestKey = func() *ecdsa.PrivateKey {
	key, err := crypto.HexToECDSA(strings.Repeat("a1", 32))
	if err != nil {
		panic(err)
	}
	return key
}()

// Test deployments sign with a test key, and nothing here reaches the real site.
// Set once, before any test runs; production keeps the pinned owner key.
func init() {
	signer := crypto.PubkeyToAddress(depositPriceTestKey.PublicKey)
	releaseDepositPriceSigner = func(chainID uint64, netuid uint16) (common.Address, bool) {
		if chainID == depositPriceTestChainID && (netuid == 7 || netuid == 521) {
			return signer, true
		}
		return pricesheet.SignerFor(chainID, netuid)
	}
	releaseDepositPriceURL = "http://127.0.0.1:9/price.yml"
}

func depositPriceTestSign(t testing.TB, netuid uint16, key *ecdsa.PrivateKey, entry *pricesheet.Entry) {
	t.Helper()
	message, err := entry.Message(depositPriceTestChainID, netuid)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(crypto.Keccak256(fmt.Appendf(nil, "\x19Ethereum Signed Message:\n%d%s", len(message), message)), key)
	if err != nil {
		t.Fatal(err)
	}
	signature[crypto.RecoveryIDOffset] += 27
	entry.Signature = signature
}

// Two tiers: half price from 1 alpha of conviction.
func depositPriceTestEntry(t testing.TB, netuid uint16, effective, capRao uint64) *pricesheet.Entry {
	t.Helper()
	entry := &pricesheet.Entry{EffectiveEpoch: effective, EpochCapRao: capRao, Tiers: []protocol.DepositTier{
		{MinConvictionRao: 0, RateNumeratorRaoPerGiB: 40_000_000, RateNumeratorRaoPerUser: 1_000_000, RateDenominator: 1},
		{MinConvictionRao: 1_000_000_000, RateNumeratorRaoPerGiB: 20_000_000, RateNumeratorRaoPerUser: 500_000, RateDenominator: 1},
	}}
	depositPriceTestSign(t, netuid, depositPriceTestKey, entry)
	return entry
}

func depositPriceTestZeroEntry(t testing.TB, netuid uint16, effective uint64) *pricesheet.Entry {
	t.Helper()
	entry := &pricesheet.Entry{EffectiveEpoch: effective, EpochCapRao: 1_000_000_000, Tiers: []protocol.DepositTier{
		{MinConvictionRao: 0, RateDenominator: 1}, {MinConvictionRao: 1_000_000_000, RateDenominator: 1},
	}}
	depositPriceTestSign(t, netuid, depositPriceTestKey, entry)
	return entry
}

func depositPriceTestPrices(netuid uint16, entries ...*pricesheet.Entry) *releaseDepositPrices {
	sheet := &pricesheet.Sheet{Netuid: netuid}
	for _, entry := range entries {
		sheet.Entries = append(sheet.Entries, *entry)
	}
	return &releaseDepositPrices{sheet: sheet, now: time.Now, refreshed: time.Now(), timeout: time.Second}
}

// depositUsageTestChain adds vault2's events to the resolver fixture's contracts.
type depositUsageTestChain struct {
	*settlementV2ResolverTestFixture
	logLock    sync.Mutex
	logs       []types.Log
	logQueries [][2]uint64
}

type depositUsageLogQuery struct {
	FromBlock hexutil.Uint64   `json:"fromBlock"`
	ToBlock   hexutil.Uint64   `json:"toBlock"`
	Address   []common.Address `json:"address"`
	Topics    [][]common.Hash  `json:"topics"`
}

func newDepositUsageTestChain(t *testing.T, resolver *settlementV2ResolverTestFixture) *depositUsageTestChain {
	t.Helper()
	self := &depositUsageTestChain{settlementV2ResolverTestFixture: resolver}
	server := gethrpc.NewServer()
	if err := server.RegisterName("eth", self); err != nil {
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
	previous := resolver.chain
	resolver.chain = &ChainClient{client: client, coordinator: previous.coordinator, contractAddr: previous.contractAddr, chainId: previous.chainId, release: true}
	return self
}

// eth_getLogs, within the public endpoint's span.
func (self *depositUsageTestChain) GetLogs(ctx context.Context, query depositUsageLogQuery) ([]types.Log, error) {
	self.logLock.Lock()
	defer self.logLock.Unlock()
	if uint64(query.ToBlock)-uint64(query.FromBlock) >= getLogsChunkBlocks {
		return nil, errors.New("eth_getLogs span exceeds the public limit")
	}
	self.logQueries = append(self.logQueries, [2]uint64{uint64(query.FromBlock), uint64(query.ToBlock)})
	logs := []types.Log{}
	for _, log := range self.logs {
		if log.BlockNumber < uint64(query.FromBlock) || log.BlockNumber > uint64(query.ToBlock) || !slices.Contains(query.Address, log.Address) {
			continue
		}
		matches := true
		for index, topics := range query.Topics {
			if len(topics) != 0 && (index >= len(log.Topics) || !slices.Contains(topics, log.Topics[index])) {
				matches = false
			}
		}
		if matches {
			logs = append(logs, log)
		}
	}
	return logs, ctx.Err()
}

func (self *depositUsageTestChain) queries() [][2]uint64 {
	self.logLock.Lock()
	defer self.logLock.Unlock()
	return slices.Clone(self.logQueries)
}

// A DistributionCommitted event, as vault2 encodes it.
func (self *depositUsageTestChain) commit(t *testing.T, noID, seq, epoch uint64, root, ledgerHash [32]byte, block uint64) {
	t.Helper()
	event := settlementV2VaultABI.Events["DistributionCommitted"]
	data, err := event.Inputs.NonIndexed().Pack(epoch, root, big.NewInt(9), uint64(1), ledgerHash, uint64(0))
	if err != nil {
		t.Fatal(err)
	}
	hash, found := self.blocks[block]
	if !found {
		hash = [32]byte{0x51, byte(block >> 8), byte(block)}
		self.blocks[block] = hash
	}
	self.logLock.Lock()
	defer self.logLock.Unlock()
	self.logs = append(self.logs, types.Log{Address: self.addresses.Vault, Topics: []common.Hash{event.ID, common.BigToHash(new(big.Int).SetUint64(noID)), common.BigToHash(new(big.Int).SetUint64(seq))}, Data: data, BlockNumber: block, BlockHash: common.Hash(hash), TxHash: common.Hash{0x52}, Index: uint(len(self.logs))})
}

// depositUsageTestOperator is an operator's public API: ledger objects, evidence
// and payout artifacts.
type depositUsageTestOperator struct {
	deploymentID string
	netuid       uint16
	// The run an envelope is published under, whatever its statement claims.
	runID    string
	requests atomic.Int64

	lock      sync.Mutex
	objects   map[string][]byte
	envelopes map[string][]byte
	// Each envelope's evidence kind; history lists those of the kind asked.
	contentHashKinds map[string]string
	historyKinds     []string
	historyRequest   int
	historyStatus    int
	objectStatus     int
	stalled          map[string]bool
	contentType      string
	before           map[string]func()
	payoutHistory    []byte
	payoutBody       []byte
}

func newDepositUsageTestOperator(deploymentID string, netuid uint16) *depositUsageTestOperator {
	return &depositUsageTestOperator{deploymentID: deploymentID, netuid: netuid, objects: map[string][]byte{}, envelopes: map[string][]byte{}, contentHashKinds: map[string]string{}}
}

func (self *depositUsageTestOperator) serve(writer http.ResponseWriter, request *http.Request) bool {
	self.lock.Lock()
	defer self.lock.Unlock()
	query := request.URL.Query()
	if before := self.before[request.URL.Path]; before != nil {
		before()
	}
	contentType := "application/json"
	if self.contentType != "" {
		contentType = self.contentType
	}
	switch request.URL.Path {
	case "/sn/ledger/object":
		self.requests.Add(1)
		if self.objectStatus != 0 {
			http.Error(writer, "Object unavailable.", self.objectStatus)
			return true
		}
		if self.stalled[query.Get("hash")] {
			// Held until its reader gives up, without holding the rest of the API.
			self.lock.Unlock()
			select {
			case <-request.Context().Done():
			case <-time.After(time.Minute):
			}
			self.lock.Lock()
			http.Error(writer, "Object stalled.", http.StatusServiceUnavailable)
			return true
		}
		data, found := self.objects[query.Get("hash")]
		if !found {
			http.Error(writer, "Object not found.", http.StatusNotFound)
			return true
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write(data)
	case "/sn/evidence/history":
		self.requests.Add(1)
		self.historyRequest++
		if self.historyStatus != 0 {
			http.Error(writer, "History unavailable.", self.historyStatus)
			return true
		}
		kind := query.Get("kind")
		if query.Get("deployment_id") != self.deploymentID || query.Get("netuid") != fmt.Sprint(self.netuid) || (kind != usage.EvidenceKind && kind != usage.EvidenceKindV2) || query.Get("run_id") == "" {
			http.Error(writer, "Bad evidence history identity.", http.StatusBadRequest)
			return true
		}
		self.historyKinds = append(self.historyKinds, kind)
		names := make([]string, 0, len(self.envelopes))
		for hash := range self.envelopes {
			if self.contentHashKinds[hash] == kind {
				names = append(names, hash)
			}
		}
		sort.Strings(names)
		limit := len(names)
		if raw := query.Get("limit"); raw != "" {
			fmt.Sscan(raw, &limit)
		}
		more := len(names) > limit
		names = names[:min(limit, len(names))]
		objects := make([]map[string]any, 0, len(names))
		for _, hash := range names {
			// The real store's object spells its fields with capitals.
			objects = append(objects, map[string]any{"Key": fmt.Sprintf("store/st/v1/evidence/history/%s/%d/%s/%s/%s.json", self.deploymentID, self.netuid, kind, query.Get("run_id"), strings.TrimPrefix(hash, "sha256:")), "Size": len(self.envelopes[hash])})
		}
		writer.Header().Set("Content-Type", contentType)
		_ = json.NewEncoder(writer).Encode(map[string]any{"schema": releaseUsageHistorySchema, "objects": objects, "more": more, "next_after": ""})
	case "/sn/evidence":
		self.requests.Add(1)
		envelope, found := self.envelopes[query.Get("hash")]
		if !found {
			http.Error(writer, "Evidence not found.", http.StatusNotFound)
			return true
		}
		writer.Header().Set("Content-Type", contentType)
		_, _ = writer.Write(envelope)
	case "/sn/artifacts":
		self.requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(self.payoutHistory)
	case "/sn/artifact":
		self.requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(self.payoutBody)
	default:
		return false
	}
	return true
}

func (self *depositUsageTestOperator) putObject(data []byte) ledger.Ref {
	self.lock.Lock()
	defer self.lock.Unlock()
	ref := ledger.RefOf(data)
	self.objects[ref.String()] = data
	return ref
}

// stall makes reads of refs wait until their reader gives up. None clears it.
func (self *depositUsageTestOperator) stall(refs ...ledger.Ref) {
	self.lock.Lock()
	defer self.lock.Unlock()
	self.stalled = map[string]bool{}
	for _, ref := range refs {
		self.stalled[ref.String()] = true
	}
}

func (self *depositUsageTestOperator) dropObject(ref ledger.Ref) {
	self.lock.Lock()
	defer self.lock.Unlock()
	delete(self.objects, ref.String())
}

// The signed statement as the JSON payload of an evidence envelope, under the kind
// of its version. identity separates envelopes that hold equal statements.
func (self *depositUsageTestOperator) publish(t *testing.T, statement *usage.Statement, identity string) string {
	t.Helper()
	return self.publishAs(t, statement, identity, statement.Kind())
}

// Publishes a statement under any evidence kind.
func (self *depositUsageTestOperator) publishAs(t *testing.T, statement *usage.Statement, identity, kind string) string {
	t.Helper()
	payload, err := statement.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(append(slices.Clone(payload), identity...))
	contentHash := "sha256:" + hex.EncodeToString(hash[:])
	runID := self.runID
	if runID == "" {
		runID = usage.RunID(statement.NoID, statement.Epoch)
	}
	envelope, err := json.Marshal(depositUsageEnvelope{Schema: releaseUsageEnvelopeSchema, DeploymentID: self.deploymentID, ChainID: statement.ChainID, GenesisHash: statement.GenesisHash, Netuid: self.netuid, Kind: kind, RunID: runID, CreatedAt: "2026-11-06T18:25:00Z", Payload: payload, Signer: statement.Signer, ContentHash: contentHash, Signature: "0x00"})
	if err != nil {
		t.Fatal(err)
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	self.envelopes[contentHash] = envelope
	self.contentHashKinds[contentHash] = kind
	return contentHash
}

func (self *depositUsageTestOperator) history() int {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.historyRequest
}

// depositUsageTestFixture runs one validator's audit of operator 1 at epoch 1,
// whose source epoch 0 is [10, 310] with a root window closing at block 360.
// Operator 2 is an unregistered v1 pool with no root. The document is the mainnet
// shape: zero price, deposit caps of 10 alpha and 196 alpha.
type depositUsageTestFixture struct {
	*depositUsageTestChain
	cfg      ReleaseConfig
	key      *ecdsa.PrivateKey
	steerer  *ReleaseSteerer
	stats    ReleaseStatsMeasurement
	operator *depositUsageTestOperator
	server   *httptest.Server
	rows     []protocol.LedgerUsageRow
	chunks   []ledger.Ref
	header   ledger.Ref
	caps     [2]uint64
}

const depositUsageTestUsers = uint64(40)

func newDepositUsageTestFixture(t *testing.T) *depositUsageTestFixture {
	t.Helper()
	chain := newDepositUsageTestChain(t, newSettlementV2ResolverTestFixture(t))
	cfg := validReleaseConfig(t)
	domain := chain.query.domain
	cfg.Netuid = domain.Netuid
	cfg.GenesisHash = releaseHex32(domain.GenesisHash)
	cfg.Coordinator = strings.ToLower(common.Address(domain.Coordinator).Hex())
	cfg.SettlementVault = strings.ToLower(common.Address(domain.SettlementVault).Hex())
	cfg.SettlementV2 = &ReleaseSettlementV2Config{Settlement: strings.ToLower(chain.addresses.Settlement.Hex()), Vault: strings.ToLower(chain.addresses.Vault.Hex())}
	cfg.StateDir = newAttemptSettlementRuntimeV2TestStateDir(t)
	key, err := crypto.HexToECDSA(strings.Repeat("b2", 32))
	if err != nil {
		t.Fatal(err)
	}
	operator := newDepositUsageTestOperator(cfg.DeploymentID, cfg.Netuid)
	operator.runID = usage.RunID(1, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !operator.serve(writer, request) {
			http.Error(writer, "unknown route", http.StatusTeapot)
		}
	}))
	t.Cleanup(server.Close)
	for index := range cfg.Operators {
		cfg.Operators[index].APIURL = server.URL
		cfg.Operators[index].ArtifactSigner = crypto.PubkeyToAddress(key.PublicKey).Hex()
	}
	self := &depositUsageTestFixture{depositUsageTestChain: chain, cfg: cfg, key: key, operator: operator, server: server}
	self.setPolicy(t, zeroPricePolicy(t))
	self.stats = ReleaseStatsMeasurement{Config: ReleaseStatsConfig{AMin: 8, AlphaNumerator: 1, AlphaDenominator: 2, LatRefMillis: 100},
		Providers: []ReleaseProviderMeasurement{{ClientID: releaseMeasurementTestID(1).String(), LatencyBuckets: make([]uint64, statsLatencyBuckets), HasPriorQuality: true, PriorQualityPPM: 900_000}}}
	hotkey, err := crv4.KeypairFromSeed([32]byte{67})
	if err != nil {
		t.Fatal(err)
	}
	contexts, operators := map[uint64]*ReleaseMeasurementContext{}, map[uint64]OperatorConfig{}
	for _, config := range self.cfg.Operators {
		reader, err := NewHTTPArtifactReader(config.APIURL, self.cfg.DeploymentID, self.cfg.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		contexts[config.NoID] = &ReleaseMeasurementContext{NoID: config.NoID, Artifacts: reader}
		operators[config.NoID] = config
	}
	self.steerer = &ReleaseSteerer{cfg: &self.cfg, chain: self.chain, hotkey: hotkey, contexts: contexts, operators: operators, settlementV2: self.addresses, depositUsageMemo: newReleaseDepositUsageMemo(),
		headInputsByNO: map[uint64]ReleaseMeasurementInput{1: {NoID: 1, Stats: self.stats}, 2: {NoID: 2, Stats: self.stats}}}
	self.registerPool(1, 0)
	return self
}

// The document in force, with its own caps on chain.
func (self *depositUsageTestFixture) setPolicy(t *testing.T, policy protocol.Policy) {
	t.Helper()
	hash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	self.cfg.Policy = policy
	if self.cfg.PolicyHash, err = policy.HashHex(); err != nil {
		t.Fatal(err)
	}
	self.query.policy, self.query.domain.PolicyHash = policy, hash
	self.setCaps(t, policy.Deposit.EpochCapRaoPerOperator, policy.Deposit.TotalTestCampaignCapRao)
}

func (self *depositUsageTestFixture) setCaps(t *testing.T, epochCap, campaignCap uint64) {
	t.Helper()
	self.caps = [2]uint64{epochCap, campaignCap}
	snapshot := self.policySnapshot(self.query.policy.Settlement.RootCommitWindowBlocks)
	self.set(t, "policyAt", self.chain.coordinator.PackPolicyAt(big.NewInt(1)), snapshot)
	// The source epoch's own policy gives its commit window.
	self.set(t, "policyAt", self.chain.coordinator.PackPolicyAt(big.NewInt(0)), snapshot)
}

func (self *depositUsageTestFixture) policySnapshot(window uint64) stabi.STCoordinatorPolicySnapshot {
	policy := self.query.policy
	return stabi.STCoordinatorPolicySnapshot{PolicyHash: self.query.domain.PolicyHash, EffectiveEpoch: 0, EffectiveBlock: 10, EpochBlocks: policy.Settlement.EpochBlocks, RootCommitWindowBlocks: window, FinalizeOffsetBlocks: policy.Settlement.FinalizeOffsetBlocks, CloseGraceBlocks: policy.Settlement.CloseGraceBlocks, ClaimTTLEpochs: policy.Settlement.ClaimTTLEpochs, ClaimGraceEpochs: policy.Settlement.ClaimGraceEpochs, MaximumBindingValidityEpochs: policy.Binding.MaximumValidityEpochs, CommitmentMaxAgeBlocks: 10, EpochDepositCapRao: new(big.Int).SetUint64(self.caps[0]), CampaignDepositCapRao: new(big.Int).SetUint64(self.caps[1])}
}

// A source epoch whose own policy has another commit window than the decision's.
func (self *depositUsageTestFixture) setSourceWindow(t *testing.T, window uint64) {
	t.Helper()
	self.set(t, "policyAt", self.chain.coordinator.PackPolicyAt(big.NewInt(0)), self.policySnapshot(window))
}

func (self *depositUsageTestFixture) price(entries ...*pricesheet.Entry) {
	self.steerer.depositPrices = depositPriceTestPrices(self.cfg.Netuid, entries...)
}

// Operator 1's deposit this epoch and its conviction before it.
func (self *depositUsageTestFixture) fund(t *testing.T, deposit, before *big.Int) {
	t.Helper()
	coordinator := self.chain.coordinator
	id, epoch := big.NewInt(1), big.NewInt(1)
	self.set(t, "epochDeposits", coordinator.PackEpochDeposits(epoch, id), deposit)
	self.set(t, "epochConvictionAdded", coordinator.PackEpochConvictionAdded(epoch, id), big.NewInt(0))
	self.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(id), new(big.Int).Add(before, deposit))
}

// A later finalized decision block, with the window's last block known.
func (self *depositUsageTestFixture) advance(block uint64) {
	hash := [32]byte{0x61, byte(block >> 8), byte(block)}
	self.blocks[block] = hash
	if _, known := self.blocks[360]; !known {
		self.blocks[360] = [32]byte{0x62}
	}
	self.finalized, self.head = block, block
	self.query.boundary = AttemptBoundary{SettlementEpoch: 1, EVMBlock: block, EVMBlockHash: releaseHex32(hash)}
}

// Three GiB over two sorted rows, one of them head excluded.
func depositUsageTestRows() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 2 << 30}, {ClientID: [16]byte{2}, Coldkey: [32]byte{2}, UsageBytes: 1 << 30, HeadExcluded: true}}
}

// Publishes the chunks and header of rows for source epoch 0.
func (self *depositUsageTestFixture) publishLedger(t *testing.T, rows []protocol.LedgerUsageRow) {
	t.Helper()
	encoded := ledger.EncodeProviderChunks(rows)
	self.rows, self.chunks = rows, make([]ledger.Ref, len(encoded))
	for index, chunk := range encoded {
		self.chunks[index] = self.operator.putObject(chunk)
	}
	self.publishHeader(t, self.chunks)
}

func (self *depositUsageTestFixture) publishHeader(t *testing.T, chunks []ledger.Ref) {
	t.Helper()
	header, err := ledger.EncodeHeader(&ledger.Header{NoID: 1, Epoch: 0, Epochs: []ledger.HeaderEpoch{{Epoch: 0, Providers: chunks}}, Accounts: []ledger.Ref{}})
	if err != nil {
		t.Fatal(err)
	}
	self.header = self.operator.putObject(header)
}

func (self *depositUsageTestFixture) commitHeader(t *testing.T) {
	t.Helper()
	self.commit(t, 1, 5, 0, [32]byte{0xab}, self.header, 312)
}

func (self *depositUsageTestFixture) statement(t *testing.T, mutate func(*usage.Statement)) *usage.Statement {
	t.Helper()
	total, count, err := usage.SumRows(self.rows)
	if err != nil {
		t.Fatal(err)
	}
	chunks := make([]string, len(self.chunks))
	for index, ref := range self.chunks {
		chunks[index] = ref.String()
	}
	statement := &usage.Statement{DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: self.cfg.GenesisHash, Netuid: self.cfg.Netuid, Coordinator: common.HexToAddress(self.cfg.Coordinator), VaultV2: self.addresses.Vault, PolicyHash: self.cfg.PolicyHash, NoID: 1, Epoch: 0,
		Start: usage.Boundary{Number: 10, Hash: releaseHex32(self.blocks[10])}, End: usage.Boundary{Number: 310, Hash: releaseHex32(self.blocks[310])},
		ProviderChunks: chunks, ProviderRows: count, TotalUsageBytes: total, TotalUsers: depositUsageTestUsers}
	if mutate != nil {
		mutate(statement)
	}
	if err := statement.Seal(self.key); err != nil {
		t.Fatal(err)
	}
	return statement
}

// Everything a compliant operator does for source epoch 0 but its deposit.
func (self *depositUsageTestFixture) settle(t *testing.T) {
	t.Helper()
	self.publishLedger(t, depositUsageTestRows())
	self.commitHeader(t)
	self.operator.publish(t, self.statement(t, nil), "")
}

// The deposit the settled usage owes at a conviction.
func (self *depositUsageTestFixture) owed(t *testing.T, entry *pricesheet.Entry, conviction *big.Int) *big.Int {
	t.Helper()
	total, _, err := usage.SumRows(self.rows)
	if err != nil {
		t.Fatal(err)
	}
	required, _, err := protocol.RequiredDepositRao(total, depositUsageTestUsers, conviction, entry.DepositPolicy(self.cfg.Policy.Deposit))
	if err != nil {
		t.Fatal(err)
	}
	return required
}

func (self *depositUsageTestFixture) decision() *ReleaseMeasurementArtifact {
	block := self.query.boundary.EVMBlock
	decision := releaseDepositAuditV2Artifact(ReleaseMeasurementV2Decision{DeploymentID: self.cfg.DeploymentID, ChainID: self.cfg.ChainID, GenesisHash: self.cfg.GenesisHash, Coordinator: self.cfg.Coordinator, SettlementVault: self.cfg.SettlementVault, ValidatorID: self.cfg.ValidatorID, Netuid: self.cfg.Netuid,
		SubnetEpoch: 90, NativeSnapshotBlock: block, NativeSnapshotHash: releaseHex32([32]byte{0x63}), EVMSnapshotBlock: block, EVMSnapshotHash: self.query.boundary.EVMBlockHash, SettlementEpoch: 1, PolicyHash: self.cfg.PolicyHash, SelfUID: 2}, nil, self.cfg.Policy)
	decision.DepositPriceEntry = newReleaseDepositPriceEntry(self.steerer.depositPrices.entryAt(1))
	return decision
}

// One decision's result for operator 1, checked three ways.
type depositUsageTestRun struct {
	audit    DepositAudit
	score    *big.Rat
	decision *ReleaseMeasurementArtifact
}

// run gathers live, verifies the sealed audits with the pure measurement verifier,
// which must reach the same weights, and replays them without the operator's API.
func (self *depositUsageTestFixture) run(t *testing.T) depositUsageTestRun {
	t.Helper()
	decision := self.decision()
	weights, pools, audits, err := self.steerer.gatherPoolWeightsV2(t.Context(), self.snapshot(), map[uint64]map[connect.Id]bool{}, self.hotkeyUIDs(), decision)
	if err != nil {
		t.Fatal(err)
	}
	decision.Pools, decision.DepositAudits = pools, audits
	verified, err := VerifyReleaseStatsMeasurement(self.stats)
	if err != nil {
		t.Fatal(err)
	}
	_, verifiedWeights, _, err := releaseMeasurementPools(decision, map[uint64]VerifiedReleaseStats{1: verified, 2: verified}, map[uint64]map[connect.Id]bool{}, map[uint64]bool{})
	if err != nil {
		t.Fatalf("pure measurement verification refused the live audit %+v: %v", audits[0], err)
	}
	if len(weights) != len(verifiedWeights) {
		t.Fatalf("live weights %v differ from verified weights %v", weights, verifiedWeights)
	}
	result := depositUsageTestRun{audit: audits[0], decision: decision}
	for index := range weights {
		if weights[index].UID != verifiedWeights[index].UID || weights[index].Score.Cmp(verifiedWeights[index].Score) != 0 {
			t.Fatalf("live weights %v differ from verified weights %v", weights, verifiedWeights)
		}
		if weights[index].UID == pools[0].UID {
			result.score = weights[index].Score
		}
	}
	before := self.operator.requests.Load()
	replayed := self.replay(t, decision)
	if !slices.Equal(replayed, audits) {
		t.Fatalf("replay %+v differs from live %+v", replayed, audits)
	}
	if self.operator.requests.Load() != before {
		t.Fatal("replay asked the operator's API for a past fact")
	}
	return result
}

func (self *depositUsageTestFixture) replay(t *testing.T, decision *ReleaseMeasurementArtifact) []DepositAudit {
	t.Helper()
	observed, err := self.observe(t, decision)
	if err != nil {
		t.Fatal(err)
	}
	history := self.history()
	custody := &releaseEvidenceV2StartupReferences{remaining: self.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	defer func() {
		if err := custody.close(); err != nil {
			t.Error(err)
		}
	}()
	audits := make([]DepositAudit, len(observed.operators))
	for index, operator := range observed.operators {
		if audits[index], err = history.historicalDepositAuditWithCaptureV2(t.Context(), observed, operator, decision.DepositAudits[index], decision, custody); err != nil {
			t.Fatal(err)
		}
	}
	return audits
}

func (self *depositUsageTestFixture) observe(t *testing.T, decision *ReleaseMeasurementArtifact) (*releaseDecisionChainV2Observation, error) {
	t.Helper()
	entry, err := verifiedReleaseDepositPriceEntry(decision.DepositPriceEntry, decision.ChainID, decision.Netuid, decision.SettlementEpoch)
	if err != nil {
		return nil, err
	}
	query := self.query
	query.settlementV2, query.priceEntry = self.addresses, entry
	return self.chain.readReleaseDecisionChainV2Context(t.Context(), query)
}

func (self *depositUsageTestFixture) history() *releaseEvidenceV2StartupHistory {
	initial := map[uint64]ReleaseEvidenceV2ActivationContext{}
	var participants []AttemptSettlementRuntimeV2Participant
	for _, operator := range self.query.operators {
		initial[operator.noID] = ReleaseEvidenceV2ActivationContext{InitialCut: AttemptCutV2Context{Activation: AttemptCutV2Activation{Domain: self.query.domain, Hotkey: self.steerer.hotkey.PublicKey()}}}
		participants = append(participants, AttemptSettlementRuntimeV2Participant{NoID: operator.noID})
	}
	return &releaseEvidenceV2StartupHistory{cfg: self.cfg, initial: initial, participants: participants}
}

func requireDepositUsageRefused(t *testing.T, run depositUsageTestRun, status, message string) {
	t.Helper()
	audit := run.audit
	if audit.Status != status || audit.Compliant || audit.Disposition != "zero_pool_weight" || run.score != nil || audit.UsageEvidence != usage.EvidenceKind || !strings.Contains(audit.Error, message) {
		t.Fatalf("want %s with %q and no weight, got score %v and %+v", status, message, run.score, audit)
	}
}

// The compliant audit: the shared formula prices the rows' bytes and the statement's
// users at the operator's tier, and the fields hold the commit and the statement.
func TestDepositUsageCompliantAuditEarnsWeight(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	required := fixture.owed(t, entry, big.NewInt(0))
	if required.Cmp(big.NewInt(160_000_000)) != 0 {
		t.Fatalf("3 GiB and 40 users at 0.04 and 0.001 alpha owe %s", required)
	}
	fixture.fund(t, required, big.NewInt(0))
	run := fixture.run(t)
	audit := run.audit
	statementBytes, err := fixture.statement(t, nil).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != DepositAuditCompliant || !audit.Compliant || audit.Disposition != "pool_weight_eligible" || audit.Error != "" || audit.UsageBytes != 3<<30 || audit.Users != depositUsageTestUsers || audit.RateNumeratorRaoPerGiB != 40_000_000 || audit.RateNumeratorRaoPerUser != 1_000_000 || audit.RateDenominator != 1 ||
		audit.RequiredDepositRao != "160000000" || audit.ObservedDepositRao != "160000000" || audit.UsageEvidence != usage.EvidenceKind || audit.HttpObservationHash == "" ||
		audit.ArtifactHash != ReleaseMeasurementContentHash(statementBytes) || audit.CommittedArtifactHash != releaseHex32(fixture.header) || audit.PayoutRoot != releaseHex32([32]byte{0xab}) || audit.RootCommitBlock != 312 ||
		audit.ArtifactSigner != strings.ToLower(crypto.PubkeyToAddress(fixture.key.PublicKey).Hex()) || audit.RootCommitter != strings.ToLower(fixture.versions[0].RootSigner.Hex()) || audit.RootSigner != audit.RootCommitter ||
		audit.SourceStartBlock != 10 || audit.SourceEndBlock != 310 || audit.SourceStartHash != releaseHex32(fixture.blocks[10]) || audit.SourceEndHash != releaseHex32(fixture.blocks[310]) {
		t.Fatalf("compliant audit %+v", audit)
	}
	if want := big.NewRat(9*160_000_000, 10); run.score == nil || run.score.Cmp(want) != 0 {
		t.Fatalf("weight %v, want %v", run.score, want)
	}
	if !slices.Equal(fixture.v2Reads("pending"), nil) || !slices.Equal(fixture.v2Reads("roots"), nil) {
		t.Fatal("the audit read the root lifecycle")
	}
}

// Short and over both fail the same exact equality, on every pass.
func TestDepositUsageShortAndOverDepositsEarnNoWeight(t *testing.T) {
	for name, delta := range map[string]int64{"short": -1, "over": 1} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.settle(t)
			required := fixture.owed(t, entry, big.NewInt(0))
			fixture.fund(t, new(big.Int).Add(required, big.NewInt(delta)), big.NewInt(0))
			run := fixture.run(t)
			requireDepositUsageRefused(t, run, DepositAuditMismatch, "does not equal")
			if run.audit.RequiredDepositRao != required.String() || run.audit.ArtifactHash == "" {
				t.Fatalf("a mismatch keeps its evidence: %+v", run.audit)
			}
		})
	}
}

// No commit is pending until the window closes, then unavailable. A commit after
// the window, which STSettlement2 refuses anyway, changes nothing, and no statement
// or object is read without a commit.
func TestDepositUsageNoCommitIsPendingThenUnavailable(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.publishLedger(t, depositUsageTestRows())
	fixture.operator.publish(t, fixture.statement(t, nil), "")
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	requireDepositUsageRefused(t, run, DepositAuditUnavailablePending, "not committed")
	if run.audit.HttpObservationHash != "" || fixture.operator.requests.Load() != 0 {
		t.Fatal("an absent commit reached the operator's API")
	}
	if queries := fixture.queries(); len(queries) != 2 || queries[0] != [2]uint64{310, 315} {
		t.Fatalf("the scan covers [end, the decision]: %v", queries)
	}
	fixture.advance(365)
	fixture.commit(t, 1, 5, 0, [32]byte{0xab}, fixture.header, 361)
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailable, "not committed")
	if fixture.operator.requests.Load() != 0 {
		t.Fatal("a commit outside the window reached the operator's API")
	}
	for _, query := range fixture.queries() {
		if query[0] != 310 || query[1] > 360 {
			t.Fatalf("the scan left the root window: %v", query)
		}
	}
}

// Only the source epoch's commit is its commit. Another epoch's in the same window
// is not, even when it is later.
func TestDepositUsageIgnoresAnotherEpochsCommit(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.commit(t, 1, 6, 7, [32]byte{0xcd}, [32]byte{0xef}, 314)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	if !run.audit.Compliant || run.audit.CommittedArtifactHash != releaseHex32(fixture.header) || run.audit.PayoutRoot != releaseHex32([32]byte{0xab}) || run.audit.RootCommitBlock != 312 {
		t.Fatalf("another epoch's commit was taken: %+v", run.audit)
	}
}

// With no statement yet, the commit's evidence stays on the audit, and it is pending
// and then unavailable.
func TestDepositUsageNoStatementIsPendingThenUnavailable(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.publishLedger(t, depositUsageTestRows())
	fixture.commitHeader(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	requireDepositUsageRefused(t, run, DepositAuditUnavailablePending, "no usage statement is published")
	if run.audit.CommittedArtifactHash != releaseHex32(fixture.header) || run.audit.HttpObservationHash == "" {
		t.Fatalf("the negative is retained with its commit: %+v", run.audit)
	}
	fixture.advance(365)
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailable, "no usage statement is published")
}

// A deleted or refused root leaves its event, and its usage is still the epoch's.
func TestDepositUsageDeletedRootKeepsWeight(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	fixture.pending = stabi.ISTSettlementVault2Pending{Total: big.NewInt(0)}
	run := fixture.run(t)
	if !run.audit.Compliant || run.score == nil {
		t.Fatalf("the deleted root changed the pool's weight: %+v", run.audit)
	}
	if len(fixture.v2Reads("pending")) != 0 || len(fixture.v2Reads("roots")) != 0 || len(fixture.v2Reads("rootCount")) != 0 {
		t.Fatal("the audit read the root lifecycle")
	}
}

// Rows that break the ledger's rules, or do not decode, are artifact_invalid.
func TestDepositUsageMalformedRowsAreInvalid(t *testing.T) {
	good := depositUsageTestRows()
	for name, test := range map[string]struct {
		chunk   []byte
		message string
	}{
		"bad head flag": {chunk: append(slices.Clone(ledger.EncodeProviderChunks(good)[0][:ledger.ProviderRowBytes-1]), 2), message: ledger.ErrRowEncoding.Error()},
		"partial row":   {chunk: ledger.EncodeProviderChunks(good)[0][:ledger.ProviderRowBytes+1], message: ledger.ErrChunkLength.Error()},
		"unsorted":      {chunk: ledger.EncodeProviderChunks([]protocol.LedgerUsageRow{good[1], good[0]})[0], message: ledger.ErrRowsUnsorted.Error()},
		"duplicate":     {chunk: ledger.EncodeProviderChunks([]protocol.LedgerUsageRow{good[0], good[0]})[0], message: ledger.ErrRowDuplicate.Error()},
		"zero usage":    {chunk: ledger.EncodeProviderChunks([]protocol.LedgerUsageRow{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}}})[0], message: ledger.ErrRowZeroUsage.Error()},
		"zero client":   {chunk: ledger.EncodeProviderChunks([]protocol.LedgerUsageRow{{Coldkey: [32]byte{1}, UsageBytes: 1}})[0], message: ledger.ErrRowZeroClientID.Error()},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.rows = good
			ref := fixture.operator.putObject(test.chunk)
			fixture.chunks = []ledger.Ref{ref}
			fixture.publishHeader(t, fixture.chunks)
			fixture.commitHeader(t)
			fixture.operator.publish(t, fixture.statement(t, nil), "")
			fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
			requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, test.message)
		})
	}
	t.Run("malformed header", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
		fixture.header = fixture.operator.putObject([]byte(`{"schema":"not a header"}`))
		fixture.commitHeader(t)
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		run := fixture.run(t)
		requireDepositUsageRefused(t, run, DepositAuditInvalid, "committed ledger header")
		if fixture.operator.history() != 0 {
			t.Fatal("a malformed header reached the statement history")
		}
	})
	t.Run("unrecoverable epoch", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
		header, err := ledger.EncodeHeader(&ledger.Header{NoID: 1, Epoch: 0, Epochs: []ledger.HeaderEpoch{{Epoch: 0, Null: true}}, Accounts: []ledger.Ref{}})
		if err != nil {
			t.Fatal(err)
		}
		fixture.header = fixture.operator.putObject(header)
		fixture.commitHeader(t)
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, "unrecoverable")
	})
	t.Run("another pool's header", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
		header, err := ledger.EncodeHeader(&ledger.Header{NoID: 2, Epoch: 0, Epochs: []ledger.HeaderEpoch{{Epoch: 0, Providers: []ledger.Ref{}}}, Accounts: []ledger.Ref{}})
		if err != nil {
			t.Fatal(err)
		}
		fixture.header = fixture.operator.putObject(header)
		fixture.commitHeader(t)
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, "another pool")
	})
}

// The bytes are the rows': a statement that claims less cannot owe less, and one
// that claims more cannot buy more.
func TestDepositUsageStatementLyingAboutBytesIsInvalid(t *testing.T) {
	for name, delta := range map[string]int64{"less": -1, "more": 1} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.publishLedger(t, depositUsageTestRows())
			fixture.commitHeader(t)
			fixture.operator.publish(t, fixture.statement(t, func(statement *usage.Statement) {
				statement.TotalUsageBytes = uint64(int64(statement.TotalUsageBytes) + delta)
			}), "")
			fixture.fund(t, big.NewInt(100), big.NewInt(0))
			run := fixture.run(t)
			requireDepositUsageRefused(t, run, DepositAuditInvalid, "the ledger holds")
			if run.audit.ArtifactHash == "" {
				t.Fatal("the invalid audit lost the statement it refused")
			}
		})
	}
}

// Each identity a statement names must be this deployment's, and its signature the
// pinned artifact signer's.
func TestDepositUsageStatementThatFailsToVerifyIsInvalid(t *testing.T) {
	other, err := crypto.HexToECDSA(strings.Repeat("c3", 32))
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		mutate  func(*usage.Statement)
		key     *ecdsa.PrivateKey
		message string
	}{
		"wrong signer":     {key: other, message: "signed by"},
		"deployment":       {mutate: func(s *usage.Statement) { s.DeploymentID = "another" }, message: "deployment identity"},
		"chain":            {mutate: func(s *usage.Statement) { s.ChainID++ }, message: "deployment identity"},
		"netuid":           {mutate: func(s *usage.Statement) { s.Netuid++ }, message: "deployment identity"},
		"genesis":          {mutate: func(s *usage.Statement) { s.GenesisHash = releaseHex32([32]byte{9}) }, message: "deployment identity"},
		"coordinator":      {mutate: func(s *usage.Statement) { s.Coordinator = common.Address{9} }, message: "deployment identity"},
		"vault":            {mutate: func(s *usage.Statement) { s.VaultV2 = common.Address{9} }, message: "deployment identity"},
		"policy hash":      {mutate: func(s *usage.Statement) { s.PolicyHash = releaseHex32([32]byte{9}) }, message: "deployment identity"},
		"no id":            {mutate: func(s *usage.Statement) { s.NoID = 2 }, message: "deployment identity"},
		"epoch":            {mutate: func(s *usage.Statement) { s.Epoch = 7 }, message: "deployment identity"},
		"start boundary":   {mutate: func(s *usage.Statement) { s.Start.Number = 11 }, message: "boundary"},
		"end boundary":     {mutate: func(s *usage.Statement) { s.End.Hash = releaseHex32([32]byte{9}) }, message: "boundary"},
		"tampered payload": {key: nil, mutate: nil, message: "signature"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.publishLedger(t, depositUsageTestRows())
			fixture.commitHeader(t)
			statement := fixture.statement(t, test.mutate)
			if test.key != nil {
				if err := statement.Seal(test.key); err != nil {
					t.Fatal(err)
				}
			}
			if name == "tampered payload" {
				statement.TotalUsers++
			}
			fixture.operator.publish(t, statement, "")
			fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
			requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, test.message)
		})
	}
}

// A statement for other chunks is not this root's. Nothing names the committed
// chunks yet, as after a rebuild the operator has not republished.
func TestDepositUsageWrongChunksIsPendingThenUnavailable(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.publishLedger(t, depositUsageTestRows())
	fixture.commitHeader(t)
	fixture.operator.publish(t, fixture.statement(t, func(statement *usage.Statement) {
		statement.ProviderChunks = []string{ledger.RefOf([]byte("another build")).String()}
	}), "")
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailablePending, "no usage statement names")
	fixture.advance(365)
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailable, "no usage statement names")
}

// The order of a chunk list is its identity.
func TestDepositUsageChunkOrderIsCompared(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	rows := make([]protocol.LedgerUsageRow, ledger.ChunkRows+1)
	for index := range rows {
		var id [16]byte
		binary.BigEndian.PutUint32(id[12:], uint32(index+1))
		rows[index] = protocol.LedgerUsageRow{ClientID: id, Coldkey: [32]byte{1}, UsageBytes: 1}
	}
	fixture.publishLedger(t, rows)
	if len(fixture.chunks) != 2 {
		t.Fatalf("%d chunks", len(fixture.chunks))
	}
	fixture.commitHeader(t)
	fixture.operator.publish(t, fixture.statement(t, func(statement *usage.Statement) {
		slices.Reverse(statement.ProviderChunks)
	}), "")
	fixture.fund(t, big.NewInt(1), big.NewInt(0))
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailablePending, "no usage statement names")
}

// A rebuild can publish a second statement: the one that names the committed chunks
// counts, whatever the other says. Equal statements in two envelopes are one.
func TestDepositUsageSelectsTheStatementOfTheCommittedChunks(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.operator.publish(t, fixture.statement(t, nil), "the same statement again")
	fixture.operator.publish(t, fixture.statement(t, func(statement *usage.Statement) {
		statement.ProviderChunks, statement.TotalUsageBytes, statement.TotalUsers = []string{ledger.RefOf([]byte("first build")).String()}, 1, 1
	}), "")
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	if !run.audit.Compliant || run.audit.UsageBytes != 3<<30 || run.audit.Users != depositUsageTestUsers {
		t.Fatalf("the rebuilt statement was not selected: %+v", run.audit)
	}
}

// Users are the operator's attestation, so two verified statements of one set of
// chunks that disagree cannot both be: it has said two things.
func TestDepositUsageEquivocationOnUsers(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.operator.publish(t, fixture.statement(t, func(statement *usage.Statement) { statement.TotalUsers++ }), "")
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditEquivocation, "disagree")
}

// A forged statement speaks for no one: it can neither cause equivocation nor stand
// in for the operator's own.
func TestDepositUsageForgedStatementCannotCauseEquivocation(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	other, err := crypto.HexToECDSA(strings.Repeat("c3", 32))
	if err != nil {
		t.Fatal(err)
	}
	forged := fixture.statement(t, func(statement *usage.Statement) { statement.TotalUsers++ })
	if err := forged.Seal(other); err != nil {
		t.Fatal(err)
	}
	fixture.operator.publish(t, forged, "")
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	if run := fixture.run(t); !run.audit.Compliant || run.audit.Users != depositUsageTestUsers {
		t.Fatalf("a forged statement changed the audit: %+v", run.audit)
	}
}

// More statements than the audit will read is equivocation by the history alone,
// as for a payout artifact, and no envelope is fetched.
func TestDepositUsageTooManyStatementsAreEquivocation(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	for index := range maximumExactArtifactObjects {
		fixture.operator.publish(t, fixture.statement(t, nil), fmt.Sprint(index))
	}
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditEquivocation, "holds more than")
}

// Whatever an operator's API answers becomes this one audit's verdict and is
// retained within its bound. It can never stop the validator or its publication.
func TestDepositUsageHostileResponsesAreVerdicts(t *testing.T) {
	t.Run("oversized envelope", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.publishLedger(t, depositUsageTestRows())
		fixture.commitHeader(t)
		hash := fixture.operator.publish(t, fixture.statement(t, nil), "")
		fixture.operator.lock.Lock()
		fixture.operator.envelopes[hash] = bytes.Repeat([]byte("x"), 2*maximumUsageEnvelopeBytes)
		fixture.operator.lock.Unlock()
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailablePending, "exceeds")
	})
	t.Run("malformed envelope", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.publishLedger(t, depositUsageTestRows())
		fixture.commitHeader(t)
		hash := fixture.operator.publish(t, fixture.statement(t, nil), "")
		fixture.operator.lock.Lock()
		fixture.operator.envelopes[hash] = []byte(`{"schema":"another"}`)
		fixture.operator.lock.Unlock()
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, "envelope")
	})
	t.Run("history refused", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.publishLedger(t, depositUsageTestRows())
		fixture.commitHeader(t)
		fixture.operator.historyStatus = http.StatusForbidden
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailablePending, "returned HTTP 403")
	})
}

// A transient failure is retried for the ordinary window before one negative is
// retained, for a statement and for a ledger object alike.
func TestDepositUsageRetriesBeforeRetainingANegative(t *testing.T) {
	for name, fail := range map[string]func(*depositUsageTestFixture){
		"statement history": func(fixture *depositUsageTestFixture) { fixture.operator.historyStatus = http.StatusServiceUnavailable },
		"ledger object":     func(fixture *depositUsageTestFixture) { fixture.operator.objectStatus = http.StatusServiceUnavailable },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.settle(t)
			fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
			fail(fixture)
			var elapsed time.Duration
			fixture.steerer.contexts[1].Artifacts.(*HTTPArtifactReader).retryHooks = releaseHttpGetTestDeadlineHooks(t, &elapsed, 5*time.Minute)
			requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailablePending, "returned HTTP 503")
			if elapsed != 5*time.Minute {
				t.Fatalf("the failure was retained after %s", elapsed)
			}
		})
	}
}

// A caller's own stop is never published as a fact about the operator: nothing is
// retained, and the next pass at the boundary observes afresh.
func TestDepositUsageCancellationRetainsNothing(t *testing.T) {
	for _, route := range []string{"/sn/ledger/object", "/sn/evidence/history", "/sn/evidence"} {
		t.Run(route, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.settle(t)
			fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture.operator.before = map[string]func(){route: cancel}
			decision := fixture.decision()
			if _, _, _, err := fixture.steerer.gatherPoolWeightsV2(ctx, fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), decision); !errors.Is(err, context.Canceled) {
				t.Fatalf("a canceled pass returned %v", err)
			}
			reader, err := NewHTTPArtifactReader(fixture.cfg.Operators[0].APIURL, fixture.cfg.DeploymentID, fixture.cfg.Netuid)
			if err != nil {
				t.Fatal(err)
			}
			_, path, err := releaseUsageHttpRequestV2(&fixture.cfg, fixture.steerer.hotkey.PublicKey(), decision, 1, 0, reader)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a canceled pass retained an observation: %v", err)
			}
			fixture.operator.lock.Lock()
			fixture.operator.before = nil
			fixture.operator.lock.Unlock()
			if run := fixture.run(t); !run.audit.Compliant {
				t.Fatalf("the next pass kept a canceled read: %+v", run.audit)
			}
		})
	}
}

// However much an operator answers, and whatever its answers make the validator
// repeat, the retained observation and the audit fit their public payload, so one
// operator's answers can never stop a validator's publication.
func TestDepositUsageLargestObservationFitsItsPublicPayload(t *testing.T) {
	retained := func(t *testing.T, fixture *depositUsageTestFixture, run depositUsageTestRun) int {
		t.Helper()
		reader, err := NewHTTPArtifactReader(fixture.cfg.Operators[0].APIURL, fixture.cfg.DeploymentID, fixture.cfg.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		_, path, err := releaseDepositObservationRequestV2(&fixture.cfg, fixture.steerer.hotkey.PublicKey(), run.decision, run.audit, reader)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		payload := ValidatorEvidenceDepositAuditV2Payload{Schema: ValidatorEvidenceDepositAuditV2Schema, Decision: releaseMeasurementV2Decision(run.decision), Audit: run.audit, HttpObservation: observation, DepositPriceEntry: run.decision.DepositPriceEntry}
		if _, err := validatorEvidenceDepositAuditV2PayloadBytes(t.Context(), payload, fixture.cfg.EvidenceV2.Bounds.MaxTransitionBytes); err != nil {
			t.Fatalf("an observation of %d bytes does not fit its public payload: %v", len(observation), err)
		}
		return len(observation)
	}
	prepared := func(t *testing.T) *depositUsageTestFixture {
		t.Helper()
		fixture := newDepositUsageTestFixture(t)
		fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
		fixture.publishLedger(t, depositUsageTestRows())
		fixture.commitHeader(t)
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		return fixture
	}
	publishMost := func(t *testing.T, fixture *depositUsageTestFixture) {
		t.Helper()
		fixture.operator.contentType = "application/json;" + strings.Repeat("<", 7000)
		for index := range maximumExactArtifactObjects {
			fixture.operator.publish(t, fixture.statement(t, func(statement *usage.Statement) {
				statement.ProviderChunks = make([]string, 760)
				for chunk := range statement.ProviderChunks {
					statement.ProviderChunks[chunk] = ledger.RefOf(fmt.Appendf(nil, "another build %d %d", index, chunk)).String()
				}
			}), "")
		}
	}
	t.Run("the most statements the audit reads", func(t *testing.T) {
		fixture := prepared(t)
		publishMost(t, fixture)
		run := fixture.run(t)
		requireDepositUsageRefused(t, run, DepositAuditUnavailablePending, "")
		if size := retained(t, fixture, run); size < 500*1024 {
			t.Fatalf("the operator's answers were not retained up to the allowance: %d bytes", size)
		}
	})
	t.Run("more than a smaller allowance holds", func(t *testing.T) {
		fixture := prepared(t)
		fixture.cfg.EvidenceV2.Bounds.MaxTransitionBytes = 512 * 1024
		publishMost(t, fixture)
		run := fixture.run(t)
		requireDepositUsageRefused(t, run, DepositAuditUnavailablePending, "exceeds")
		retained(t, fixture, run)
	})
	t.Run("an error that repeats the operator's text", func(t *testing.T) {
		fixture := prepared(t)
		hash := fixture.operator.publish(t, fixture.statement(t, nil), "")
		var envelope depositUsageEnvelope
		if err := json.Unmarshal(fixture.operator.envelopes[hash], &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.Payload = fmt.Appendf(nil, "{%q:1}", strings.Repeat("<", 8000))
		hostile, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		fixture.operator.envelopes[hash] = hostile
		run := fixture.run(t)
		requireDepositUsageRefused(t, run, DepositAuditInvalid, "unknown field")
		if len(run.audit.Error) > maximumUsageExchangeLabel {
			t.Fatalf("the audit repeats %d bytes of the operator's text", len(run.audit.Error))
		}
		retained(t, fixture, run)
	})
}

// Live steering verifies an epoch's committed rows on its first pass only. Later
// passes reuse what they hold, still judge the statement and compare the deposit,
// and replay still verifies from its evidence.
func TestDepositUsageVerifiedRowsAreReadOncePerEpoch(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	required := fixture.owed(t, entry, big.NewInt(0))
	fixture.fund(t, required, big.NewInt(0))
	memo := fixture.steerer.depositUsageMemo
	fills := func() uint64 {
		memo.lock.Lock()
		defer memo.lock.Unlock()
		return memo.fills
	}
	if run := fixture.run(t); !run.audit.Compliant || fills() != 1 {
		t.Fatalf("first pass: %d reads of the rows, %+v", fills(), run.audit)
	}
	fixture.advance(330)
	if run := fixture.run(t); !run.audit.Compliant || run.score == nil || fills() != 1 {
		t.Fatalf("a later pass read the rows again: %d reads, %+v", fills(), run.audit)
	}
	fixture.fund(t, new(big.Int).Sub(required, big.NewInt(1)), big.NewInt(0))
	fixture.advance(340)
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditMismatch, "does not equal")
	if fills() != 1 {
		t.Fatalf("the deposit check read the rows again: %d reads", fills())
	}
}

// A ledger read that outlasts the pass's budget leaves that pass pending, retained
// so replay agrees, instead of using up the steering operation. The next pass goes
// on from what custody holds.
func TestDepositUsageSlowLedgerReadIsPendingForThatPass(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	fixture.steerer.depositUsageReadLimit = 300 * time.Millisecond
	fixture.operator.stall(fixture.chunks[0])
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	decision := fixture.decision()
	_, pools, audits, err := fixture.steerer.gatherPoolWeightsV2(ctx, fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), decision)
	if err != nil {
		t.Fatalf("a slow ledger read failed the pass: %v", err)
	}
	decision.Pools, decision.DepositAudits = pools, audits
	if audit := audits[0]; audit.Status != DepositAuditUnavailablePending || audit.Compliant || !strings.Contains(audit.Error, "time budget is spent") || audit.HttpObservationHash == "" {
		t.Fatalf("a slow ledger read: %+v", audit)
	}
	if replayed := fixture.replay(t, decision); !slices.Equal(replayed, audits) {
		t.Fatalf("replay %+v differs from live %+v", replayed, audits)
	}
	fixture.operator.stall()
	fixture.advance(320)
	if run := fixture.run(t); !run.audit.Compliant {
		t.Fatalf("the next pass did not go on: %+v", run.audit)
	}
}

// Each operator's read time is an equal share of half of what the pass has left,
// fixed before any is audited, and each audit gets its whole share whatever the
// audits before it spent.
func TestDepositUsageReadShareIsFixedPerOperator(t *testing.T) {
	if got := depositUsageReadShare(context.Background(), 0, 2); got != depositUsageReadTimeLimit || got > DefaultReleaseSteeringSettings().OperationTimeout/10 {
		t.Fatalf("share %s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if got := depositUsageReadShare(ctx, 0, 2); got > 15*time.Second || got < 14*time.Second {
		t.Fatalf("share %s of a minute between two operators", got)
	}
	if got := depositUsageReadShare(ctx, time.Second, 2); got != time.Second {
		t.Fatalf("share %s", got)
	}
	// Half the pass left is less than this audit's share: it still gets all of it.
	short, cancelShort := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelShort()
	clock := newDepositUsageReadClock(short, 20*time.Second, releaseHttpGetRetryHooks{})
	defer clock.cancel()
	if left := time.Until(clock.deadline); left < 19*time.Second {
		t.Fatalf("an audit got %s of its 20s share", left)
	}
}

// An operator's commit can name, as its ledger header, an object larger than a
// header: fetched, it is a failed read; already held, it is the operator's malformed
// ledger. Either is that pool's verdict, and replay agrees.
func TestDepositUsageOversizedHeaderIsThePoolsVerdict(t *testing.T) {
	rows := make([]protocol.LedgerUsageRow, 5000)
	for index := range rows {
		var id [16]byte
		binary.BigEndian.PutUint32(id[12:], uint32(index+1))
		rows[index] = protocol.LedgerUsageRow{ClientID: id, Coldkey: [32]byte{1}, UsageBytes: 1}
	}
	chunk := ledger.EncodeProviderChunks(rows)[0]
	if len(chunk) <= ledger.MaxHeaderBytes {
		t.Fatalf("a chunk of %d bytes is not over the header bound", len(chunk))
	}
	for name, test := range map[string]struct {
		held    bool
		status  string
		message string
	}{
		"fetched": {status: DepositAuditUnavailablePending, message: "response exceeds"},
		"held":    {held: true, status: DepositAuditInvalid, message: depositUsageHeldOversize},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
			ref := fixture.operator.putObject(chunk)
			if test.held {
				custody, err := openSettlementV2Custody(fixture.cfg.StateDir)
				if err != nil {
					t.Fatal(err)
				}
				if err := custody.Keep(t.Context(), ref, chunk); err != nil {
					t.Fatal(err)
				}
			}
			fixture.commit(t, 1, 5, 0, [32]byte{0xab}, ref, 312)
			fixture.fund(t, big.NewInt(0), big.NewInt(0))
			requireDepositUsageRefused(t, fixture.run(t), test.status, test.message)
		})
	}
}

// The audit's own earlier pass keeps an operator's provider chunk. A later commit
// naming that chunk as its header is that pool's verdict, never the pass's error.
func TestDepositUsageKeptChunkNamedAsHeaderIsThePoolsVerdict(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	rows := make([]protocol.LedgerUsageRow, 5000)
	for index := range rows {
		var id [16]byte
		binary.BigEndian.PutUint32(id[12:], uint32(index+1))
		rows[index] = protocol.LedgerUsageRow{ClientID: id, Coldkey: [32]byte{1}, UsageBytes: 1 << 20}
	}
	fixture.publishLedger(t, rows)
	fixture.commitHeader(t)
	fixture.operator.publish(t, fixture.statement(t, nil), "")
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	if run := fixture.run(t); !run.audit.Compliant {
		t.Fatalf("honest pass: %+v", run.audit)
	}
	fixture.commit(t, 1, 6, 0, [32]byte{0xcd}, fixture.chunks[0], 330)
	fixture.advance(335)
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, depositUsageHeldOversize)
}

// An operator that answers every read with a long Retry-After cannot hold the pass:
// the audit's one read budget caps its pacing, and the audit is pending for that
// pass, retained so replay agrees.
func TestDepositUsageSlowStatementReadsStayInsideTheBudget(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	for index := range maximumExactArtifactObjects - 1 {
		fixture.operator.publish(t, fixture.statement(t, nil), fmt.Sprint("copy ", index))
	}
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	var lock sync.Mutex
	first := map[string]bool{}
	slow := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/sn/evidence") {
			lock.Lock()
			seen := first[request.URL.String()]
			first[request.URL.String()] = true
			lock.Unlock()
			if !seen {
				writer.Header().Set("Retry-After", "290")
				http.Error(writer, "busy", http.StatusServiceUnavailable)
				return
			}
		}
		if !fixture.operator.serve(writer, request) {
			http.Error(writer, "unknown route", http.StatusTeapot)
		}
	}))
	t.Cleanup(slow.Close)
	var waited time.Duration
	for index := range fixture.cfg.Operators {
		fixture.cfg.Operators[index].APIURL = slow.URL
		reader, err := NewHTTPArtifactReader(slow.URL, fixture.cfg.DeploymentID, fixture.cfg.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		// The waits are counted, not slept: the count is what a real pass would spend.
		reader.retryHooks = releaseHttpGetRetryHooks{wait: func(ctx context.Context, delay time.Duration) error {
			waited += delay
			return ctx.Err()
		}}
		fixture.steerer.contexts[fixture.cfg.Operators[index].NoID].Artifacts = reader
	}
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailablePending, depositUsageReadTimeSpent)
	if waited > depositUsageReadTimeLimit {
		t.Fatalf("one operator's reads waited %s, beyond the audit's %s", waited, depositUsageReadTimeLimit)
	}
}

// --ledger_max_bytes bounds a root across passes, held objects included: a header
// whose chunks cannot fit is refused before any chunk is fetched, on every pass,
// and the root never grows past it.
func TestDepositUsageLedgerBytesArePerRootAcrossPasses(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
	const chunks, size, budget = 8, 1000, 2500
	refs := make([]ledger.Ref, chunks)
	for index := range refs {
		refs[index] = fixture.operator.putObject(bytes.Repeat([]byte{byte(index + 1)}, size))
	}
	fixture.publishHeader(t, refs)
	fixture.commitHeader(t)
	fixture.fund(t, big.NewInt(0), big.NewInt(0))
	ctx := WithSettlementV2RuntimeOptions(t.Context(), SettlementV2RuntimeOptions{LedgerMaxBytes: budget})
	store := depositUsageObjectsAt(fixture.cfg.StateDir)
	held := func() int {
		total := 0
		for _, ref := range append([]ledger.Ref{fixture.header}, refs...) {
			data, found, err := store.Object(t.Context(), ref)
			if err != nil {
				t.Fatal(err)
			}
			if found {
				total += len(data)
			}
		}
		return total
	}
	for pass := range 4 {
		fixture.advance(320 + uint64(pass))
		decision := fixture.decision()
		_, pools, audits, err := fixture.steerer.gatherPoolWeightsV2(ctx, fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), decision)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		decision.Pools, decision.DepositAudits = pools, audits
		requireDepositUsageRefused(t, depositUsageTestRun{audit: audits[0]}, DepositAuditUnavailablePending, depositUsageByteBudgetSpent)
		if replayed := fixture.replay(t, decision); !slices.Equal(replayed, audits) {
			t.Fatalf("pass %d: replay %+v differs from live %+v", pass, replayed, audits)
		}
		if requests := fixture.operator.requests.Load(); requests != 1 {
			t.Fatalf("pass %d: %d reads of the operator's API, beyond its one header", pass, requests)
		}
	}
	if total := held(); total > budget {
		t.Fatalf("one root kept %d bytes under a %d-byte --ledger_max_bytes", total, budget)
	}
}

// Bytes fetched that never became an object count against the root on every later
// pass too, so an operator serving the wrong bytes is cut off, and stays cut off.
func TestDepositUsageWastedLedgerBytesCountAcrossPasses(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
	fixture.settle(t)
	fixture.fund(t, big.NewInt(0), big.NewInt(0))
	fixture.operator.lock.Lock()
	headerBytes := uint64(len(fixture.operator.objects[fixture.header.String()]))
	fixture.operator.objects[fixture.chunks[0].String()] = bytes.Repeat([]byte{7}, 200)
	fixture.operator.lock.Unlock()
	ctx := WithSettlementV2RuntimeOptions(t.Context(), SettlementV2RuntimeOptions{LedgerMaxBytes: headerBytes + 300})
	for pass, want := range []struct {
		message  string
		requests int64
	}{
		{message: "does not match its reference", requests: 2},
		{message: depositUsageByteBudgetSpent, requests: 3},
		{message: depositUsageByteBudgetSpent, requests: 3},
		{message: depositUsageByteBudgetSpent, requests: 3},
	} {
		fixture.advance(320 + uint64(pass))
		decision := fixture.decision()
		_, pools, audits, err := fixture.steerer.gatherPoolWeightsV2(ctx, fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), decision)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		decision.Pools, decision.DepositAudits = pools, audits
		requireDepositUsageRefused(t, depositUsageTestRun{audit: audits[0]}, DepositAuditUnavailablePending, want.message)
		if replayed := fixture.replay(t, decision); !slices.Equal(replayed, audits) {
			t.Fatalf("pass %d: replay %+v differs from live %+v", pass, replayed, audits)
		}
		if requests := fixture.operator.requests.Load(); requests != want.requests {
			t.Fatalf("pass %d: %d reads of the operator's API, want %d", pass, requests, want.requests)
		}
	}
}

// Rows are read and judged one chunk at a time: a malformed first chunk is the
// verdict, and the chunks after it are never read.
func TestDepositUsageRowsAreReadOneChunkAtATime(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
	first := fixture.operator.putObject(bytes.Repeat([]byte{1}, 1000))
	second := ledger.RefOf([]byte("never published"))
	fixture.publishHeader(t, []ledger.Ref{first, second})
	fixture.commitHeader(t)
	fixture.fund(t, big.NewInt(0), big.NewInt(0))
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, ledger.ErrChunkLength.Error())
	if requests := fixture.operator.requests.Load(); requests != 2 {
		t.Fatalf("%d reads: the chunk after a malformed one was read", requests)
	}
}

// The commit window is the source epoch's own, end(e) + policyAt(e)'s window, as
// STSettlement2 applies it, not the decision epoch's.
func TestDepositUsageCommitWindowIsTheSourceEpochs(t *testing.T) {
	t.Run("longer", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.setSourceWindow(t, 80)
		fixture.publishLedger(t, depositUsageTestRows())
		fixture.commit(t, 1, 5, 0, [32]byte{0xab}, fixture.header, 370)
		fixture.operator.publish(t, fixture.statement(t, nil), "")
		fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
		fixture.advance(375)
		run := fixture.run(t)
		if !run.audit.Compliant || run.audit.RootCommitBlock != 370 || run.audit.ArtifactDeadlineBlock != 390 {
			t.Fatalf("a commit inside the source epoch's own window: %+v", run.audit)
		}
	})
	t.Run("shorter", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
		fixture.setSourceWindow(t, 30)
		fixture.blocks[340] = [32]byte{0x64}
		fixture.publishLedger(t, depositUsageTestRows())
		fixture.commit(t, 1, 5, 0, [32]byte{0xab}, fixture.header, 350)
		fixture.fund(t, big.NewInt(0), big.NewInt(0))
		fixture.advance(355)
		run := fixture.run(t)
		requireDepositUsageRefused(t, run, DepositAuditUnavailable, "not committed")
		if run.audit.ArtifactDeadlineBlock != 340 {
			t.Fatalf("the window closes at %d", run.audit.ArtifactDeadlineBlock)
		}
	})
}

// A history allowance that leaves less than a full observation is this validator's
// limit, never the operator's: a read it cuts short stops the pass.
func TestDepositUsageShortHistoryIsTheValidatorsLimit(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes = 200 * 1024
	_, _, _, err := fixture.steerer.gatherPoolWeightsV2(t.Context(), fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), fixture.decision())
	if err == nil || !strings.Contains(err.Error(), "evidence history cannot hold") {
		t.Fatalf("a short history became a verdict: %v", err)
	}
	fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes = 1024 * 1024
	if run := fixture.run(t); !run.audit.Compliant {
		t.Fatalf("a full history: %+v", run.audit)
	}
}

// Objects this audit fetches are its own, never held objects to the ledger auditor,
// whose per-root byte budget counts only what it fetches itself.
func TestDepositUsageObjectsStayOutOfTheAuditorsCustody(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	if run := fixture.run(t); !run.audit.Compliant {
		t.Fatalf("%+v", run.audit)
	}
	auditor, err := openSettlementV2Custody(fixture.cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	store := depositUsageObjectsAt(fixture.cfg.StateDir)
	for _, ref := range append([]ledger.Ref{fixture.header}, fixture.chunks...) {
		if _, held, err := auditor.Object(t.Context(), ref); err != nil || held {
			t.Fatalf("the auditor holds %s: %t %v", ref, held, err)
		}
		if _, held, err := store.own.Object(t.Context(), ref); err != nil || !held {
			t.Fatalf("the audit does not keep %s: %t %v", ref, held, err)
		}
	}
}

// A slow sheet fetch does not hold the sheet: the steering pass reads its price
// while the fetch waits.
func TestDepositPriceRefreshDoesNotHoldTheSheet(t *testing.T) {
	entry := depositPriceTestEntry(t, 521, 1, 1_000_000_000)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	site := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	t.Cleanup(site.Close)
	prices := &releaseDepositPrices{source: pricesheet.NewSource(site.URL, depositPriceTestChainID, 521, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), nil), now: time.Now, timeout: time.Minute, sheet: &pricesheet.Sheet{Netuid: 521, Entries: []pricesheet.Entry{*entry}}}
	done := make(chan struct{})
	go func() {
		prices.refresh(context.Background())
		close(done)
	}()
	<-entered
	read := make(chan *pricesheet.Entry, 1)
	go func() { read <- prices.entryAt(1) }()
	select {
	case got := <-read:
		if got == nil {
			t.Fatal("the kept entry was not read")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading the sheet waited for its fetch")
	}
	close(release)
	<-done
}

// The coordinator's epoch is needed only to look up an entry. With none kept, no
// epoch can stop the pass.
func TestDepositPriceEpochIsNeededOnlyForAnEntry(t *testing.T) {
	var none *releaseDepositPrices
	if entry, err := none.entryAtEpoch(nil); entry != nil || err != nil {
		t.Fatal(entry, err)
	}
	empty := &releaseDepositPrices{sheet: &pricesheet.Sheet{Netuid: 521}}
	if entry, err := empty.entryAtEpoch(nil); entry != nil || err != nil {
		t.Fatal(entry, err)
	}
	priced := depositPriceTestPrices(521, depositPriceTestEntry(t, 521, 1, 1_000_000_000))
	if _, err := priced.entryAtEpoch(nil); err == nil {
		t.Fatal("a missing epoch looked up an entry")
	}
	if _, err := priced.entryAtEpoch(new(big.Int).Lsh(big.NewInt(1), 64)); err == nil {
		t.Fatal("an epoch past uint64 looked up an entry")
	}
	if entry, err := priced.entryAtEpoch(big.NewInt(1)); entry == nil || err != nil {
		t.Fatal(entry, err)
	}
}

// A refusal's body is read only in part, and every byte every attempt reads counts
// against what the root has left, so retried errors cannot read past the budget.
func TestDepositUsageRefusalsAreReadInPartAcrossAttempts(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	junk := bytes.Repeat([]byte{'x'}, depositUsageFullChunkBytes)
	var refusals atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/sn/ledger/object" && request.URL.Query().Get("hash") == fixture.chunks[0].String() {
			refusals.Add(1)
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write(junk)
			return
		}
		if !fixture.operator.serve(writer, request) {
			http.Error(writer, "unknown route", http.StatusTeapot)
		}
	}))
	t.Cleanup(server.Close)
	for index := range fixture.cfg.Operators {
		fixture.cfg.Operators[index].APIURL = server.URL
		reader, err := NewHTTPArtifactReader(server.URL, fixture.cfg.DeploymentID, fixture.cfg.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		// Retries are immediate: only the bytes bound them.
		reader.retryHooks = releaseHttpGetRetryHooks{wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }}
		fixture.steerer.contexts[fixture.cfg.Operators[index].NoID].Artifacts = reader
	}
	fixture.steerer.depositUsageReadLimit = 30 * time.Second
	fixture.operator.lock.Lock()
	headerBytes := uint64(len(fixture.operator.objects[fixture.header.String()]))
	fixture.operator.lock.Unlock()
	const left = 10 * 1024
	ctx := WithSettlementV2RuntimeOptions(t.Context(), SettlementV2RuntimeOptions{LedgerMaxBytes: headerBytes + left})
	decision := fixture.decision()
	_, pools, audits, err := fixture.steerer.gatherPoolWeightsV2(ctx, fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), decision)
	if err != nil {
		t.Fatal(err)
	}
	decision.Pools, decision.DepositAudits = pools, audits
	requireDepositUsageRefused(t, depositUsageTestRun{audit: audits[0]}, DepositAuditUnavailablePending, depositUsageByteBudgetSpent)
	if replayed := fixture.replay(t, decision); !slices.Equal(replayed, audits) {
		t.Fatalf("replay %+v differs from live %+v", replayed, audits)
	}
	store := depositUsageObjectsAt(fixture.cfg.StateDir)
	var root depositUsageRootBytes
	if _, err := store.own.readJSON(store.rootBytesPath(1, 0, fixture.header), &root); err != nil {
		t.Fatal(err)
	}
	if root.WastedBytes > left+1 || root.WastedBytes > uint64(refusals.Load())*(depositUsageRefusalBodyBytes+1) {
		t.Fatalf("%d refusals read %d bytes with %d left of the root", refusals.Load(), root.WastedBytes, left)
	}
}

// Retention keeps what replay can reach: every object a root a retained observation
// names lists for its epoch, unless the ledger auditor holds it, whose copy every
// reader finds. Objects no root lists, and records of past roots nothing names, go.
func TestDepositUsageRetentionKeepsWhatReplayCanReach(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	if !run.audit.Compliant {
		t.Fatalf("%+v", run.audit)
	}
	store := depositUsageObjectsAt(fixture.cfg.StateDir)
	orphan := []byte("an object no root lists")
	if err := store.Keep(t.Context(), ledger.RefOf(orphan), orphan); err != nil {
		t.Fatal(err)
	}
	unnamed := store.rootBytesPath(1, 0, [32]byte{0xee})
	if err := store.own.writeJSON(unnamed, depositUsageRootBytes{WastedBytes: 7}); err != nil {
		t.Fatal(err)
	}
	current := store.rootBytesPath(1, 1, [32]byte{0xef})
	if err := store.own.writeJSON(current, depositUsageRootBytes{}); err != nil {
		t.Fatal(err)
	}
	held := func(ref ledger.Ref) bool {
		_, found, err := store.own.Object(t.Context(), ref)
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	// The decision moves on: source epoch 1 is now audited.
	if err := pruneDepositUsageObjectsV2(t.Context(), fixture.cfg.StateDir, 1); err != nil {
		t.Fatal(err)
	}
	if held(ledger.RefOf(orphan)) {
		t.Fatal("an object no root lists was kept")
	}
	if _, err := os.Stat(unnamed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a past root nothing names kept its record: %v", err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("a root of the current source epoch lost its record: %v", err)
	}
	for _, ref := range append([]ledger.Ref{fixture.header}, fixture.chunks...) {
		if !held(ref) {
			t.Fatalf("the retained decision's object %s was pruned", ref)
		}
	}
	if replayed := fixture.replay(t, run.decision); !slices.Equal(replayed, run.decision.DepositAudits) {
		t.Fatalf("replay after retention: %+v", replayed)
	}
	// The ledger auditor comes to hold the header: the audit's copy goes, and replay
	// reads the auditor's.
	auditor, err := openSettlementV2Custody(fixture.cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	fixture.operator.lock.Lock()
	header := fixture.operator.objects[fixture.header.String()]
	fixture.operator.lock.Unlock()
	if err := auditor.Keep(t.Context(), fixture.header, header); err != nil {
		t.Fatal(err)
	}
	if err := pruneDepositUsageObjectsV2(t.Context(), fixture.cfg.StateDir, 1); err != nil {
		t.Fatal(err)
	}
	if held(fixture.header) {
		t.Fatal("the audit kept a copy of what the auditor holds")
	}
	if replayed := fixture.replay(t, run.decision); !slices.Equal(replayed, run.decision.DepositAudits) {
		t.Fatalf("replay from the auditor's copy: %+v", replayed)
	}
}

// A header that lists the source epoch twice is not that epoch's ledger, whichever
// entry a reader would take.
func TestDepositUsageHeaderListingTheEpochTwiceIsInvalid(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
	fixture.publishLedger(t, depositUsageTestRows())
	header, err := ledger.EncodeHeader(&ledger.Header{NoID: 1, Epoch: 0, Epochs: []ledger.HeaderEpoch{{Epoch: 0, Providers: fixture.chunks}, {Epoch: 0, Providers: fixture.chunks}}, Accounts: []ledger.Ref{}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.header = fixture.operator.putObject(header)
	fixture.commitHeader(t)
	fixture.operator.publish(t, fixture.statement(t, nil), "")
	fixture.fund(t, big.NewInt(0), big.NewInt(0))
	requireDepositUsageRefused(t, fixture.run(t), DepositAuditInvalid, "lists the source epoch more than once")
}

// While a signed price is in force, a configuration with no settlement_v2 is an error
// on every steering pass. Without a price nothing is logged.
func TestDepositPriceWithoutSettlementV2IsAnErrorEveryPass(t *testing.T) {
	authority, fixture := newReleaseMainnetCadenceTestFixture(t)
	steerer := &ReleaseSteerer{cfg: authority.cfg, chain: fixture.chain, native: authority.rpc.native}
	validate := func() string {
		t.Helper()
		snapshot, err := fixture.chain.ReleaseSnapshotContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout := os.Stdout
		os.Stdout = writer
		err = steerer.validatePinnedChains(t.Context(), snapshot, &crv4.EpochScheduleState{CurrentBlock: 150}, mainnetRuntimeTestBlock(150))
		os.Stdout = stdout
		if closeErr := writer.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		output, readErr := io.ReadAll(reader)
		if err != nil || readErr != nil {
			t.Fatal(err, readErr)
		}
		return string(output)
	}
	if output := validate(); strings.Contains(output, "settlement_v2") {
		t.Fatalf("an unpriced pass logged: %s", output)
	}
	sheet := &pricesheet.Sheet{Netuid: authority.cfg.Netuid, Entries: []pricesheet.Entry{{EffectiveEpoch: 0, EpochCapRao: authority.cfg.Policy.Deposit.EpochCapRaoPerOperator, Tiers: []protocol.DepositTier{{RateNumeratorRaoPerGiB: 1, RateDenominator: 1}}}}}
	steerer.depositPrices = &releaseDepositPrices{sheet: sheet, now: time.Now, refreshed: time.Now(), timeout: time.Second}
	if authority.cfg.SettlementV2 != nil {
		t.Fatal("the fixture's configuration names settlement_v2")
	}
	for pass := range 2 {
		if output := validate(); !strings.Contains(output, releaseDepositPriceWithoutSettlementV2) {
			t.Fatalf("pass %d did not log the missing settlement_v2: %q", pass, output)
		}
	}
}

// An object it cannot obtain is pending, then unavailable. The failed read is
// retained, so replay repeats it even after custody later holds the object.
func TestDepositUsageMissingObjectsArePendingAndReplayAsObserved(t *testing.T) {
	for name, drop := range map[string]func(*depositUsageTestFixture) ledger.Ref{
		"header": func(fixture *depositUsageTestFixture) ledger.Ref { return fixture.header },
		"chunk":  func(fixture *depositUsageTestFixture) ledger.Ref { return fixture.chunks[0] },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.settle(t)
			fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
			ref := drop(fixture)
			data := fixture.operator.objects[ref.String()]
			fixture.operator.dropObject(ref)
			run := fixture.run(t)
			requireDepositUsageRefused(t, run, DepositAuditUnavailablePending, "returned HTTP 404")
			if run.audit.HttpObservationHash == "" || fixture.operator.history() != 0 {
				t.Fatalf("the failed read was not retained or the audit went on: %+v", run.audit)
			}
			custody, err := openSettlementV2Custody(fixture.cfg.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := custody.Keep(t.Context(), ref, data); err != nil {
				t.Fatal(err)
			}
			if replayed := fixture.replay(t, run.decision); replayed[0] != run.audit {
				t.Fatalf("replay used an object the audit did not have: %+v", replayed[0])
			}
			// A later boundary is a new observation, and custody now holds the object.
			fixture.advance(365)
			if later := fixture.run(t); !later.audit.Compliant {
				t.Fatalf("a later boundary kept the earlier failure: %+v", later.audit)
			}
		})
	}
	t.Run("bytes that are not the object", func(t *testing.T) {
		fixture := newDepositUsageTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.settle(t)
		fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
		fixture.operator.lock.Lock()
		fixture.operator.objects[fixture.header.String()] = []byte("other bytes")
		fixture.operator.lock.Unlock()
		requireDepositUsageRefused(t, fixture.run(t), DepositAuditUnavailablePending, "does not match its reference")
		if _, held, err := depositUsageObjectsAt(fixture.cfg.StateDir).Object(t.Context(), fixture.header); err != nil || held {
			t.Fatalf("custody kept bytes that are not the object: %t %v", held, err)
		}
	})
}

// The deposit above the cap is the cap, and the demand is truncated by the same
// factor at every tier, so usage the operator did not pay for buys no weight.
func TestDepositUsageCappedDeposit(t *testing.T) {
	const capRao = 50_000_000
	for name, test := range map[string]struct {
		conviction *big.Int
		owed       int64
	}{
		"tier zero": {conviction: big.NewInt(0), owed: 160_000_000},
		"tier one":  {conviction: big.NewInt(1_000_000_000), owed: 80_000_000},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, capRao)
			fixture.price(entry)
			fixture.settle(t)
			fixture.fund(t, big.NewInt(capRao), test.conviction)
			run := fixture.run(t)
			if !run.audit.Compliant || run.audit.RequiredDepositRao != fmt.Sprint(capRao) {
				t.Fatalf("capped audit %+v", run.audit)
			}
			want := new(big.Rat).Mul(big.NewRat(160_000_000, 1), big.NewRat(capRao, test.owed))
			want.Mul(want, big.NewRat(9, 10))
			if run.score == nil || run.score.Cmp(want) != 0 {
				t.Fatalf("capped weight %v, want %v", run.score, want)
			}
		})
	}
}

// Conviction discounts the deposit and not the weight: the rates in the audit are
// the operator's tier's, its demand is priced at tier zero, and both tiers score
// the same below the cap.
func TestDepositUsageTierDiscountKeepsTierZeroWeight(t *testing.T) {
	scores := map[string]*big.Rat{}
	for name, conviction := range map[string]*big.Int{"tier zero": big.NewInt(0), "tier one": big.NewInt(2_000_000_000)} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.settle(t)
			required := fixture.owed(t, entry, conviction)
			fixture.fund(t, required, conviction)
			run := fixture.run(t)
			want, gib := "160000000", uint64(40_000_000)
			if conviction.Sign() != 0 {
				want, gib = "80000000", 20_000_000
			}
			if !run.audit.Compliant || run.audit.RequiredDepositRao != want || run.audit.RateNumeratorRaoPerGiB != gib || run.audit.ConvictionBeforeRao != conviction.String() {
				t.Fatalf("discounted audit %+v", run.audit)
			}
			scores[name] = run.score
		})
	}
	if scores["tier zero"] == nil || scores["tier one"] == nil || scores["tier zero"].Cmp(scores["tier one"]) != 0 || scores["tier zero"].Cmp(big.NewRat(9*160_000_000, 10)) != 0 {
		t.Fatalf("tier scores %v", scores)
	}
}

// The latest entry in effect for the decision epoch prices the source epoch's usage,
// not the entry in effect when that usage happened.
func TestDepositUsageRatesComeFromTheDecisionEpoch(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	source := depositPriceTestEntry(t, fixture.cfg.Netuid, 0, 1_000_000_000)
	decision := &pricesheet.Entry{EffectiveEpoch: 1, EpochCapRao: 1_000_000_000, Tiers: []protocol.DepositTier{{MinConvictionRao: 0, RateNumeratorRaoPerGiB: 80_000_000, RateDenominator: 1}}}
	depositPriceTestSign(t, fixture.cfg.Netuid, depositPriceTestKey, decision)
	later := &pricesheet.Entry{EffectiveEpoch: 2, EpochCapRao: 1_000_000_000, Tiers: []protocol.DepositTier{{MinConvictionRao: 0, RateNumeratorRaoPerGiB: 1, RateDenominator: 1}}}
	depositPriceTestSign(t, fixture.cfg.Netuid, depositPriceTestKey, later)
	fixture.price(source, decision, later)
	fixture.settle(t)
	fixture.fund(t, big.NewInt(240_000_000), big.NewInt(0))
	run := fixture.run(t)
	if !run.audit.Compliant || run.audit.RateNumeratorRaoPerGiB != 80_000_000 || run.audit.Users != depositUsageTestUsers || run.decision.DepositPriceEntry.EffectiveEpoch != 1 {
		t.Fatalf("audit %+v under %+v", run.audit, run.decision.DepositPriceEntry)
	}
}

// An entry effective after the epoch leaves the document in force: zero price, the
// artifact carrying nothing, and no chain or API read beyond today's.
func TestDepositPriceEntryEffectiveLaterLeavesTheDocument(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 2, 1_000_000_000))
	fixture.settle(t)
	fixture.fund(t, big.NewInt(15), big.NewInt(0))
	run := fixture.run(t)
	if run.audit.Status != DepositAuditZeroPrice || run.decision.DepositPriceEntry != nil || run.score == nil || run.score.Cmp(big.NewRat(9, 10)) != 0 {
		t.Fatalf("audit %+v under %+v", run.audit, run.decision.DepositPriceEntry)
	}
	if len(fixture.queries()) != 0 || fixture.operator.requests.Load() != 0 {
		t.Fatal("an entry not yet in force read the usage audit's sources")
	}
}

// A zero entry is the zero-price mode, whatever the document says. The artifact
// carries it, since the document alone would not reproduce the decision.
func TestDepositPriceZeroEntry(t *testing.T) {
	for name, policy := range map[string]func(*testing.T) protocol.Policy{
		"over a priced document": func(t *testing.T) protocol.Policy { return exactPolicy(t) },
		"over a zero document":   func(t *testing.T) protocol.Policy { return zeroPricePolicy(t) },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositUsageTestFixture(t)
			fixture.setPolicy(t, policy(t))
			fixture.price(depositPriceTestZeroEntry(t, fixture.cfg.Netuid, 1))
			fixture.settle(t)
			fixture.fund(t, big.NewInt(15), big.NewInt(0))
			run := fixture.run(t)
			if run.audit.Status != DepositAuditZeroPrice || !run.audit.Compliant || run.audit.UsageEvidence != "" || run.audit.HttpObservationHash != "" || run.decision.DepositPriceEntry == nil || run.score == nil || run.score.Cmp(big.NewRat(9, 10)) != 0 {
				t.Fatalf("audit %+v under %+v", run.audit, run.decision.DepositPriceEntry)
			}
			if len(fixture.queries()) != 0 || fixture.operator.requests.Load() != 0 {
				t.Fatal("a zero price read the usage audit's sources")
			}
		})
	}
}

// Before the pool's first v2 epoch the deposit still prices a v1 source epoch, by
// the payout artifact path and the decision epoch's signed rates. Its bytes are the
// payout audit's: no usage evidence, and its hash is the committed one.
func TestDepositUsagePricedV1SourceUsesTheArtifactPath(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.registerPool(1, 1)
	artifact, err := payoutartifact.Build(payoutartifact.BuildInput{DeploymentID: fixture.cfg.DeploymentID, GenesisHash: fixture.cfg.GenesisHash, PolicyHash: fixture.cfg.PolicyHash, ChainID: fixture.cfg.ChainID, Netuid: fixture.cfg.Netuid, Coordinator: common.HexToAddress(fixture.cfg.Coordinator), SettlementVault: common.HexToAddress(fixture.cfg.SettlementVault), Epoch: 0, NoID: 1,
		Start: payoutartifact.Boundary{Number: 10, Hash: releaseHex32(fixture.blocks[10])}, End: payoutartifact.Boundary{Number: 310, Hash: releaseHex32(fixture.blocks[310])}, OperatorSnapshotHash: "sha256:" + strings.Repeat("10", 32), FleetSnapshotHash: "sha256:" + strings.Repeat("20", 32),
		Providers: []payoutartifact.ProviderInput{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 3 << 30, Assignments: 8, Confirmations: 8, Eligible: true}}, ReliabilityAMin: fixture.cfg.Policy.Verify.ReliabilityAMin, CreatedAt: time.Unix(1_700_000_000, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := payoutartifact.Sign(artifact, fixture.key); err != nil {
		t.Fatal(err)
	}
	encoded, err := payoutartifact.Bytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	contentHash, err := parseReleaseContentHash(artifact.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	fixture.set(t, "rootCommitments", fixture.chain.coordinator.PackRootCommitments(big.NewInt(0), big.NewInt(1)), artifact.PayoutRoot, contentHash, fixture.versions[0].RootSigner, uint64(312))
	history, err := json.Marshal(artifactHistoryResponse{Schema: "urnetwork-payout-artifact-history-v1", Objects: []artifactHistoryObject{{Key: fmt.Sprintf("store/st/v1/history/%s/%d/%d/%d/%s.json", fixture.cfg.DeploymentID, fixture.cfg.Netuid, 0, 1, strings.TrimPrefix(artifact.ContentHash, "sha256:")), Size: int64(len(encoded)), ContentHash: artifact.ContentHash}}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.operator.payoutHistory, fixture.operator.payoutBody = history, encoded
	required, _, err := protocol.RequiredDepositRao(artifact.TotalUsageBytes, artifact.TotalUsers, big.NewInt(0), entry.DepositPolicy(fixture.cfg.Policy.Deposit))
	if err != nil || required.Sign() <= 0 {
		t.Fatalf("%v %v", required, err)
	}
	fixture.fund(t, required, big.NewInt(0))
	run := fixture.run(t)
	if run.audit.Status != DepositAuditCompliant || run.audit.UsageEvidence != "" || run.audit.ArtifactHash != artifact.ContentHash || run.audit.RateNumeratorRaoPerGiB != 40_000_000 || run.audit.RequiredDepositRao != required.String() || run.score == nil {
		t.Fatalf("v1 source audit %+v", run.audit)
	}
	if len(fixture.queries()) != 0 {
		t.Fatal("a v1 source epoch scanned for a v2 root")
	}
	if fixture.steerer.hotkey == nil || run.decision.Pools[0].PoolHotkey != releaseHex32(settlementV2TestPoolHotkey) {
		t.Fatalf("the weight destination of the first v2 epoch is vault2's: %+v", run.decision.Pools[0])
	}
}

// With no signed entry in force a priced document keeps today's path, and a
// registered v2 pool does not change it.
func TestDepositUsagePricedDocumentWithoutEntryKeepsTheArtifactPath(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	fixture.setPolicy(t, exactPolicy(t))
	fixture.settle(t)
	fixture.fund(t, big.NewInt(15), big.NewInt(0))
	run := fixture.run(t)
	if run.audit.Status != DepositAuditUnavailablePending || run.audit.UsageEvidence != "" || !strings.Contains(run.audit.Error, "source payout root is not committed") || len(fixture.queries()) != 0 || fixture.operator.requests.Load() != 0 {
		t.Fatalf("audit %+v", run.audit)
	}
}

// A retry at one decision boundary replays what was observed and manufactures no
// new fact, even with the operator gone. Live and replay agree after a restart.
func TestDepositUsageLiveAndReplayAgreeAtOneBoundaryOffline(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	first := fixture.run(t)
	requests := fixture.operator.requests.Load()
	if requests != 4 {
		t.Fatalf("one audit reads a header, its one chunk, a history and a statement: %d", requests)
	}
	fixture.server.Close()
	second := fixture.run(t)
	if second.audit != first.audit || second.score.Cmp(first.score) != 0 || fixture.operator.requests.Load() != requests {
		t.Fatalf("a retry made a new observation: %+v", second.audit)
	}
}

// A retained observation that was changed is refused, not read as another audit.
func TestDepositUsageRetainedObservationCannotBeChanged(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	reader, err := NewHTTPArtifactReader(fixture.cfg.Operators[0].APIURL, fixture.cfg.DeploymentID, fixture.cfg.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	expected, path, err := releaseDepositObservationRequestV2(&fixture.cfg, fixture.steerer.hotkey.PublicKey(), run.decision, run.audit, reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	maximum := releaseUsageObservationMaximum(fixture.cfg.EvidenceV2.Bounds)
	if _, err := decodeArtifactHttpObservationV2(t.Context(), encoded, maximum, expected); err != nil {
		t.Fatal(err)
	}
	var value releaseArtifactHttpObservationV2
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	value.Exchanges[0].Status = http.StatusNotFound
	changed, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeArtifactHttpObservationV2(t.Context(), changed, maximum, expected); err == nil {
		t.Fatal("a changed observation kept its signature")
	}
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := fixture.observe(t, run.decision)
	if err != nil {
		t.Fatal(err)
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	defer custody.close()
	if _, err := fixture.history().historicalDepositAuditWithCaptureV2(t.Context(), observed, observed.operators[0], run.audit, run.decision, custody); err == nil {
		t.Fatal("replay read a changed observation")
	}
}

// Replay does not take an audit's word for which path it took: the chain decides.
func TestDepositUsageReplayRefusesAClaimOfTheOtherPath(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	fixture.pools[1] = stabi.ISTSettlementVault2Pool{Hotkey: settlementV2TestPoolHotkey, FirstEpoch: 1, Registered: true}
	observed, err := fixture.observe(t, run.decision)
	if err != nil {
		t.Fatal(err)
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes}
	defer custody.close()
	if _, err := fixture.history().historicalDepositAuditWithCaptureV2(t.Context(), observed, observed.operators[0], run.audit, run.decision, custody); err == nil {
		t.Fatal("a usage audit replayed on an epoch the chain says settled on v1")
	}
}

// The site's being down never fails steering. The last verified sheet stays in
// force across a restart, and the audit uses its rates.
func TestDepositPriceSiteDownKeepsTheLastGoodSheet(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	var up atomic.Bool
	var reads atomic.Int64
	up.Store(true)
	sheet := depositPriceTestSheetFile(fixture.cfg.Netuid, entry)
	site := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		if !up.Load() {
			http.Error(writer, "down", http.StatusBadGateway)
			return
		}
		_, _ = writer.Write([]byte(sheet))
	}))
	t.Cleanup(site.Close)
	clock := time.Unix(1_800_000_000, 0)
	store := &releaseDepositPriceStore{path: filepath.Join(fixture.cfg.StateDir, "price-sheet", "price.yml")}
	open := func() *releaseDepositPrices {
		return &releaseDepositPrices{source: pricesheet.NewSource(site.URL, fixture.cfg.ChainID, fixture.cfg.Netuid, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), store), now: func() time.Time { return clock }, timeout: time.Minute}
	}
	prices := open()
	prices.refresh(t.Context())
	if got := prices.entryAt(1); got == nil || got.EffectiveEpoch != 1 || reads.Load() != 1 {
		t.Fatalf("the published sheet was not taken: %+v", got)
	}
	prices.refresh(t.Context())
	if reads.Load() != 1 {
		t.Fatal("a second pass within the interval read the site")
	}
	if info, err := os.Stat(store.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the verified sheet was not kept privately: %v", err)
	}
	up.Store(false)
	clock = clock.Add(releaseDepositPriceCacheTTL)
	prices.refresh(t.Context())
	if prices.entryAt(1) == nil || reads.Load() != 2 {
		t.Fatal("an outage lost the running process's verified sheet")
	}
	restarted := open()
	restarted.refresh(t.Context())
	if restarted.entryAt(1) == nil {
		t.Fatal("an outage lost the stored sheet across a restart")
	}
	fixture.steerer.depositPrices = restarted
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	if run := fixture.run(t); !run.audit.Compliant || run.decision.DepositPriceEntry == nil {
		t.Fatalf("audit %+v", run.audit)
	}
}

// A published sheet of entries made by depositPriceTestEntry, in their order.
func depositPriceTestSheetFile(netuid uint16, entries ...*pricesheet.Entry) string {
	var schedule strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&schedule, `  %d:
    epoch_cap_alpha: %d
    signature: "0x%s"
    tiers:
      0:
        alpha_per_gib: 0.04
        alpha_per_user: 0.001
      1:
        alpha_per_gib: 0.02
        alpha_per_user: 0.0005
`, entry.EffectiveEpoch, entry.EpochCapRao/1_000_000_000, hex.EncodeToString(entry.Signature))
	}
	return fmt.Sprintf(`sn: %d
tiered_per_block_demand_deposits:
  0:
    alpha_per_gib: 0.04
    alpha_per_user: 0.001
signed_schedule:
%s`, netuid, schedule.String())
}

// With nothing ever verified the document's schedule is in force. Steering goes on,
// and a tampered or unsigned file is not used.
func TestDepositPriceNothingVerifiedLeavesTheDocument(t *testing.T) {
	entry := depositPriceTestEntry(t, 521, 1, 1_000_000_000)
	forged := *entry
	forged.Signature = slices.Clone(entry.Signature)
	forged.Signature[3] ^= 1
	for name, handler := range map[string]http.HandlerFunc{
		"down":      func(writer http.ResponseWriter, _ *http.Request) { http.Error(writer, "down", http.StatusBadGateway) },
		"unsigned":  func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("sn: 521\n")) },
		"not yaml":  func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("\t{")) },
		"other net": func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("sn: 7\n")) },
		"bad signer": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(depositPriceTestSheetFile(521, &forged)))
		},
		"redirecting": func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "/elsewhere", http.StatusFound)
		},
	} {
		t.Run(name, func(t *testing.T) {
			site := httptest.NewServer(handler)
			t.Cleanup(site.Close)
			prices := &releaseDepositPrices{source: pricesheet.NewSource(site.URL, depositPriceTestChainID, 521, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), &releaseDepositPriceStore{path: filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "price-sheet", "price.yml")}), now: time.Now, timeout: time.Minute}
			prices.refresh(t.Context())
			if prices.entryAt(1) != nil {
				t.Fatalf("an unverified sheet priced an epoch: %+v", prices.entryAt(1))
			}
		})
	}
}

// A published sheet that gives up a signed entry is a rollback, and even with its
// store lost a running process keeps what it verified.
func TestDepositPriceRollbackKeepsTheSignedEntry(t *testing.T) {
	entry := depositPriceTestEntry(t, 521, 1, 1_000_000_000)
	var body atomic.Value
	body.Store(depositPriceTestSheetFile(521, entry))
	site := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(site.Close)
	clock := time.Unix(1_800_000_000, 0)
	path := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "price-sheet", "price.yml")
	prices := &releaseDepositPrices{source: pricesheet.NewSource(site.URL, depositPriceTestChainID, 521, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), &releaseDepositPriceStore{path: path}), now: func() time.Time { return clock }, timeout: time.Minute}
	prices.refresh(t.Context())
	if prices.entryAt(1) == nil {
		t.Fatal("the signed entry was not taken")
	}
	body.Store("sn: 521\n")
	clock = clock.Add(releaseDepositPriceCacheTTL)
	prices.refresh(t.Context())
	if prices.entryAt(1) == nil {
		t.Fatal("a rollback dropped the signed entry")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(releaseDepositPriceCacheTTL)
	prices.refresh(t.Context())
	if prices.entryAt(1) == nil {
		t.Fatal("a rollback with a lost store dropped the running process's signed entry")
	}
}

// An entry added before one already verified would reprice epochs already decided.
// The stored sheet refuses it, and so does a running process whose store is lost.
func TestDepositPriceAnEarlierEntryIsRefused(t *testing.T) {
	held := depositPriceTestEntry(t, 521, 1, 1_000_000_000)
	earlier := depositPriceTestEntry(t, 521, 0, 1_000_000_000)
	var body atomic.Value
	body.Store(depositPriceTestSheetFile(521, held))
	site := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(site.Close)
	clock := time.Unix(1_800_000_000, 0)
	path := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "price-sheet", "price.yml")
	prices := &releaseDepositPrices{source: pricesheet.NewSource(site.URL, depositPriceTestChainID, 521, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), &releaseDepositPriceStore{path: path}), now: func() time.Time { return clock }, timeout: time.Minute}
	prices.refresh(t.Context())
	if prices.entryAt(1) == nil || prices.entryAt(0) != nil {
		t.Fatal("the signed entry was not taken")
	}
	body.Store(depositPriceTestSheetFile(521, earlier, held))
	for _, lost := range []bool{false, true} {
		if lost {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		clock = clock.Add(releaseDepositPriceCacheTTL)
		prices.refresh(t.Context())
		if prices.entryAt(0) != nil || prices.entryAt(1) == nil {
			t.Fatalf("store lost %t: an entry before the held one priced epoch 0", lost)
		}
	}
}

// A stored copy that is corrupt does not hold the validator on the document: the
// published sheet, once verified, replaces it. One that cannot be read is never
// overwritten, and the published sheet is still taken.
func TestDepositPriceUnusableStoreDoesNotHoldTheDocument(t *testing.T) {
	published := depositPriceTestSheetFile(521, depositPriceTestEntry(t, 521, 1, 1_000_000_000))
	site := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(published))
	}))
	t.Cleanup(site.Close)
	for name, prepare := range map[string]func(t *testing.T, path string){
		"corrupt": func(t *testing.T, path string) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("sn: [not a sheet"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"unreadable": func(t *testing.T, path string) {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "price-sheet", "price.yml")
			prepare(t, path)
			prices := &releaseDepositPrices{source: pricesheet.NewSource(site.URL, depositPriceTestChainID, 521, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), &releaseDepositPriceStore{path: path}), now: time.Now, timeout: time.Minute}
			prices.refresh(t.Context())
			if prices.entryAt(1) == nil {
				t.Fatal("an unusable store held the validator on the document")
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if name == "unreadable" {
				if !info.IsDir() {
					t.Fatal("an unreadable store was overwritten")
				}
				return
			}
			if stored, err := os.ReadFile(path); err != nil || string(stored) != published {
				t.Fatalf("the verified sheet did not replace the corrupt copy: %v", err)
			}
		})
	}
}

// A site that hangs takes no longer than the bound and is not asked again within
// the interval, so it cannot use up the steering budget.
func TestDepositPriceRefreshIsBoundedAndCached(t *testing.T) {
	release := make(chan struct{})
	var reads atomic.Int64
	site := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		reads.Add(1)
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); site.Close() })
	prices := &releaseDepositPrices{source: pricesheet.NewSource(site.URL, depositPriceTestChainID, 521, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), nil), now: time.Now, timeout: 50 * time.Millisecond}
	started := time.Now()
	prices.refresh(t.Context())
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("refresh took %s", elapsed)
	}
	prices.refresh(t.Context())
	if reads.Load() != 1 || prices.entryAt(1) != nil {
		t.Fatalf("a hung site was asked %d times", reads.Load())
	}
	if releaseDepositPriceRefreshTimeout > productionSteeringReadAttemptTimeout/10 || releaseDepositPriceCacheTTL < time.Minute {
		t.Fatal("the refresh can use up the steering budget")
	}
	// A caller's own stop says nothing about the site, so the next pass asks again.
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	fresh := &releaseDepositPrices{source: prices.source, now: time.Now, timeout: time.Second}
	fresh.refresh(canceled)
	if !fresh.refreshed.IsZero() {
		t.Fatal("a canceled refresh started the interval")
	}
}

// A deployment with no pinned signer fetches nothing, and a nil owner is today's.
func TestDepositPriceNoPinnedSignerNeverFetches(t *testing.T) {
	cfg := validReleaseConfig(t)
	cfg.ChainID, cfg.Netuid = 945, 99
	prices := newReleaseDepositPrices(&cfg)
	if prices != nil {
		t.Fatal("a deployment with no pinned signer has a price source")
	}
	prices.refresh(t.Context())
	if prices.entryAt(5) != nil {
		t.Fatal("a nil owner priced an epoch")
	}
	if signer, pinned := pricesheet.SignerFor(964, 25); !pinned || signer != pricesheet.MainnetSigner {
		t.Fatal("mainnet's signer is not pinned")
	}
	if _, pinned := pricesheet.SignerFor(945, 7); pinned {
		t.Fatal("testnet has a pinned signer in production")
	}
	cfg.ChainID, cfg.Netuid = 964, 25
	if mainnet := newReleaseDepositPrices(&cfg); mainnet == nil {
		t.Fatal("mainnet has no price source")
	}
}

// With no entry in force and no price, nothing beyond today's reads happens, and the
// audit, weight and observed bytes are today's.
func TestDepositUsageZeroPriceReadsNothingNew(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	fixture.price()
	fixture.settle(t)
	fixture.fund(t, big.NewInt(15), big.NewInt(0))
	run := fixture.run(t)
	if run.audit.Status != DepositAuditZeroPrice || run.score == nil || run.score.Cmp(big.NewRat(9, 10)) != 0 || run.decision.DepositPriceEntry != nil {
		t.Fatalf("audit %+v", run.audit)
	}
	if len(fixture.queries()) != 0 || fixture.operator.requests.Load() != 0 {
		t.Fatal("zero price read the usage audit's sources")
	}
	encoded, err := canonicalReleaseMeasurementBytes(run.decision)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("deposit_price_entry")) || bytes.Contains(encoded, []byte("usage_evidence")) {
		t.Fatalf("a zero-price artifact grew a field: %s", encoded)
	}
	// Live: two operators read the destination; replay: the same again.
	if reads := fixture.v2Reads("pools"); len(reads) != 4 {
		t.Fatalf("vault2 pool reads %v", reads)
	}
}

// The exact audit bytes at zero price with no signed entry.
func TestZeroPriceDepositAuditBytesArePinned(t *testing.T) {
	audit := ZeroPriceDepositAudit(5, 4, 1, big.NewInt(0), big.NewInt(0))
	audit.ObservedAtBlock, audit.ArtifactDeadlineBlock = 9448900, 9450028
	encoded, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"no_id":1,"epoch":5,"source_epoch":4,"observed_at_block":9448900,"artifact_deadline_block":9450028,"usage_bytes":0,"conviction_before_rao":"0","rate_numerator_rao_per_gib":0,"rate_denominator":0,"required_deposit_rao":"0","observed_deposit_rao":"0","status":"zero_price","compliant":true,"disposition":"zero_price_no_deposit_required"}`
	if string(encoded) != want {
		t.Fatalf("zero-price audit bytes changed:\n%s\n%s", encoded, want)
	}
}

// The epoch cap must hold the cap in force, the signed entry's or else the
// document's, and the campaign cap the document's and one epoch cap. Caps equal to
// the document's pass as before, and a raise may land before its entry. Both
// consumers use one rule, and a refused cap stops the decision.
func TestDepositPriceCapsAgainstTheChain(t *testing.T) {
	entry := &pricesheet.Entry{EffectiveEpoch: 1, EpochCapRao: 50, Tiers: []protocol.DepositTier{{RateNumeratorRaoPerGiB: 1, RateDenominator: 1}}}
	base := protocol.DepositPolicy{EpochCapRaoPerOperator: 10, TotalTestCampaignCapRao: 196}
	lower := &pricesheet.Entry{EffectiveEpoch: 1, EpochCapRao: 5, Tiers: entry.Tiers}
	for name, test := range map[string]struct {
		entry                 *pricesheet.Entry
		epochCap, campaignCap int64
		want                  bool
	}{
		"document equal":                {epochCap: 10, campaignCap: 196, want: true},
		"document epoch higher":         {epochCap: 11, campaignCap: 196, want: true},
		"document epoch lower":          {epochCap: 9, campaignCap: 196},
		"document campaign higher":      {epochCap: 10, campaignCap: 197, want: true},
		"document campaign lower":       {epochCap: 10, campaignCap: 195},
		"epoch above the campaign":      {epochCap: 300, campaignCap: 299},
		"campaign holds the epoch":      {epochCap: 300, campaignCap: 300, want: true},
		"entry equal":                   {entry: entry, epochCap: 50, campaignCap: 196, want: true},
		"entry higher":                  {entry: entry, epochCap: 60, campaignCap: 196, want: true},
		"entry epoch too small":         {entry: entry, epochCap: 49, campaignCap: 196},
		"entry campaign too low":        {entry: entry, epochCap: 60, campaignCap: 59},
		"entry campaign below document": {entry: entry, epochCap: 50, campaignCap: 195},
		"entry document caps low":       {entry: entry, epochCap: 10, campaignCap: 196},
		"entry below the document":      {entry: lower, epochCap: 5, campaignCap: 196, want: true},
	} {
		if got := releaseDepositCapsSuffice(big.NewInt(test.epochCap), big.NewInt(test.campaignCap), base, test.entry); got != test.want {
			t.Errorf("%s: %t", name, got)
		}
	}
	fixture := newDepositUsageTestFixture(t)
	signed := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 20_000_000_000)
	query := fixture.query
	read := func() error {
		_, err := fixture.chain.readReleaseDecisionChainV2Context(t.Context(), query)
		return err
	}
	if err := read(); err != nil {
		t.Fatalf("the document's caps: %v", err)
	}
	query.priceEntry = signed
	if err := read(); err == nil || !strings.Contains(err.Error(), "differ from canonical configuration") {
		t.Fatalf("an on-chain cap below the entry's was accepted: %v", err)
	}
	fixture.setCaps(t, 20_000_000_000, 196_000_000_000)
	if err := read(); err != nil {
		t.Fatalf("raised caps under the entry: %v", err)
	}
	query.priceEntry = nil
	if err := read(); err != nil {
		t.Fatalf("a raise before its entry: %v", err)
	}
	fixture.setCaps(t, 9_000_000_000, 196_000_000_000)
	if err := read(); err == nil {
		t.Fatal("no entry: an on-chain cap below the document's was accepted")
	}
	query.priceEntry = signed
	for _, caps := range [][2]uint64{{20_000_000_000, 19_000_000_000}, {20_000_000_000, 195_000_000_000}} {
		fixture.setCaps(t, caps[0], caps[1])
		if err := read(); err == nil {
			t.Fatalf("campaign cap %d under an epoch cap of %d was accepted", caps[1], caps[0])
		}
	}
}

// Live steering applies the same rule at its pinned snapshot, under the sheet's
// entry for the snapshot's epoch, and a refreshed sheet is the one that counts.
func TestDepositPriceCapsAgainstLiveSteering(t *testing.T) {
	authority, fixture := newReleaseMainnetCadenceTestFixture(t)
	steerer := &ReleaseSteerer{cfg: authority.cfg, chain: fixture.chain, native: authority.rpc.native}
	validate := func(epochCap, campaignCap uint64) error {
		snapshot := releaseRateAmendmentTestSnapshot(t, authority.cfg.Policy, 0, 10)
		snapshot.EpochDepositCapRao, snapshot.CampaignDepositCapRao = new(big.Int).SetUint64(epochCap), new(big.Int).SetUint64(campaignCap)
		fixture.set(t, "policyAt", fixture.chain.coordinator.PackPolicyAt(big.NewInt(0)), snapshot)
		actual, err := fixture.chain.ReleaseSnapshotContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return steerer.validatePinnedChains(t.Context(), actual, &crv4.EpochScheduleState{CurrentBlock: 150}, mainnetRuntimeTestBlock(150))
	}
	deposit := authority.cfg.Policy.Deposit
	if err := validate(deposit.EpochCapRaoPerOperator, deposit.TotalTestCampaignCapRao); err != nil {
		t.Fatal(err)
	}
	if err := validate(deposit.EpochCapRaoPerOperator+1, deposit.TotalTestCampaignCapRao); err != nil {
		t.Fatalf("no signed entry: a raise before its entry: %v", err)
	}
	if err := validate(deposit.EpochCapRaoPerOperator-1, deposit.TotalTestCampaignCapRao); err == nil {
		t.Fatal("no signed entry: an on-chain cap below the document's was accepted")
	}
	if err := validate(deposit.EpochCapRaoPerOperator, deposit.TotalTestCampaignCapRao-1); err == nil {
		t.Fatal("no signed entry: a campaign cap below the document's was accepted")
	}
	capRao := deposit.EpochCapRaoPerOperator * 2
	sheet := &pricesheet.Sheet{Netuid: authority.cfg.Netuid, Entries: []pricesheet.Entry{{EffectiveEpoch: 0, EpochCapRao: capRao, Tiers: []protocol.DepositTier{{RateNumeratorRaoPerGiB: 1, RateDenominator: 1}}}}}
	steerer.depositPrices = &releaseDepositPrices{sheet: sheet, now: time.Now, refreshed: time.Now(), timeout: time.Second}
	if err := validate(deposit.EpochCapRaoPerOperator, deposit.TotalTestCampaignCapRao); err == nil || !strings.Contains(err.Error(), "coordinator policy fields do not match") {
		t.Fatalf("an on-chain cap below the entry's was accepted: %v", err)
	}
	if err := validate(capRao, deposit.TotalTestCampaignCapRao); err != nil {
		t.Fatalf("raised caps under the entry: %v", err)
	}
	if err := validate(capRao, deposit.TotalTestCampaignCapRao-1); err == nil {
		t.Fatal("a campaign cap below the document's was accepted")
	}
	high := deposit.TotalTestCampaignCapRao + 1
	if err := validate(high, high); err != nil {
		t.Fatalf("an epoch cap above the document's campaign cap: %v", err)
	}
	if err := validate(high, high-1); err == nil {
		t.Fatal("a campaign cap below the epoch cap was accepted")
	}
	if hash, err := authority.cfg.Policy.HashHex(); err != nil || hash != authority.cfg.PolicyHash {
		t.Fatalf("the signed document's hash moved: %s %v", hash, err)
	}
}

// Live steering reads the published sheet through its one bounded refresh: once per
// interval however often it retries, the sheet it verifies governs the pass, and an
// outage then leaves that sheet in force and never fails steering.
func TestDepositPriceSteeringRefreshesOncePerInterval(t *testing.T) {
	authority, fixture := newReleaseMainnetCadenceTestFixture(t)
	policy := authority.cfg.Policy
	netuid := authority.cfg.Netuid
	entry := depositPriceTestEntry(t, netuid, 0, 1_000_000_000)
	var reads atomic.Int64
	var down atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		if down.Load() {
			http.Error(writer, "down", http.StatusBadGateway)
			return
		}
		_, _ = writer.Write([]byte(depositPriceTestSheetFile(netuid, entry)))
	}))
	t.Cleanup(site.Close)
	clock := time.Unix(1_800_000_000, 0)
	prices := &releaseDepositPrices{source: pricesheet.NewSource(site.URL, depositPriceTestChainID, netuid, crypto.PubkeyToAddress(depositPriceTestKey.PublicKey), nil), now: func() time.Time { return clock }, timeout: time.Minute}
	steerer := &ReleaseSteerer{cfg: authority.cfg, chain: fixture.chain, native: authority.rpc.native, depositPrices: prices}
	// An on-chain epoch cap below the document's, so only the sheet's entry passes it.
	if policy.Deposit.EpochCapRaoPerOperator <= 1_000_000_000 {
		t.Fatal("the fixture's document cap leaves no room below it")
	}
	snapshot := releaseRateAmendmentTestSnapshot(t, policy, 0, 10)
	snapshot.EpochDepositCapRao = big.NewInt(1_000_000_000)
	fixture.set(t, "policyAt", fixture.chain.coordinator.PackPolicyAt(big.NewInt(0)), snapshot)
	validate := func() error {
		actual, err := fixture.chain.ReleaseSnapshotContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return steerer.validatePinnedChains(t.Context(), actual, &crv4.EpochScheduleState{CurrentBlock: 150}, mainnetRuntimeTestBlock(150))
	}
	if err := validate(); err != nil || reads.Load() != 1 {
		t.Fatalf("first pass: %d reads: %v", reads.Load(), err)
	}
	if err := validate(); err != nil || reads.Load() != 1 {
		t.Fatalf("a retry within the interval: %d reads: %v", reads.Load(), err)
	}
	down.Store(true)
	clock = clock.Add(releaseDepositPriceCacheTTL)
	if err := validate(); err != nil || reads.Load() != 2 {
		t.Fatalf("an outage failed steering or was not asked: %d reads: %v", reads.Load(), err)
	}
	if prices.entryAt(0) == nil {
		t.Fatal("the outage dropped the last verified sheet")
	}
}

// The verifier authenticates the carried entry and checks the audit's rates against
// it; the signed document and its hash are untouched by any of it.
func TestDepositAuditMeasurementRejectsForgedEntry(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	if err := verifyReleaseMeasurementCommonIdentityForTest(run.decision); err != nil {
		t.Fatalf("the document's hash no longer verifies under a signed entry: %v", err)
	}
	verified, err := VerifyReleaseStatsMeasurement(fixture.stats)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(artifact *ReleaseMeasurementArtifact) error {
		_, _, _, err := releaseMeasurementPools(artifact, map[uint64]VerifiedReleaseStats{1: verified, 2: verified}, map[uint64]map[connect.Id]bool{}, map[uint64]bool{})
		return err
	}
	if err := verify(run.decision); err != nil {
		t.Fatal(err)
	}
	other, err := crypto.HexToECDSA(strings.Repeat("c3", 32))
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		change  func(*ReleaseMeasurementArtifact)
		message string
	}{
		"raised rate": {func(artifact *ReleaseMeasurementArtifact) {
			artifact.DepositPriceEntry.Tiers[0].RateNumeratorRaoPerGiB++
		}, "signed by"},
		"lowered cap": {func(artifact *ReleaseMeasurementArtifact) { artifact.DepositPriceEntry.EpochCapRao-- }, "signed by"},
		"another signer": {func(artifact *ReleaseMeasurementArtifact) {
			forged := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			depositPriceTestSign(t, fixture.cfg.Netuid, other, forged)
			artifact.DepositPriceEntry = newReleaseDepositPriceEntry(forged)
		}, "signed by"},
		"not canonical": {func(artifact *ReleaseMeasurementArtifact) {
			artifact.DepositPriceEntry.Signature = strings.ToUpper(artifact.DepositPriceEntry.Signature)
		}, "not canonical"},
		"short signature": {func(artifact *ReleaseMeasurementArtifact) { artifact.DepositPriceEntry.Signature = "0x00" }, "byte signature"},
		"effective later": {func(artifact *ReleaseMeasurementArtifact) {
			artifact.DepositPriceEntry = newReleaseDepositPriceEntry(depositPriceTestEntry(t, fixture.cfg.Netuid, 2, 1_000_000_000))
		}, "takes effect"},
		"no pinned signer": {func(artifact *ReleaseMeasurementArtifact) { artifact.ChainID = 1 }, "no pinned signer"},
		"another price": {func(artifact *ReleaseMeasurementArtifact) {
			forged := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 2_000_000_000)
			forged.Tiers = []protocol.DepositTier{{RateNumeratorRaoPerGiB: 1, RateDenominator: 1}}
			depositPriceTestSign(t, fixture.cfg.Netuid, depositPriceTestKey, forged)
			artifact.DepositPriceEntry = newReleaseDepositPriceEntry(forged)
		}, "not reconstructed"},
		"a zero price under priced audits": {func(artifact *ReleaseMeasurementArtifact) {
			artifact.DepositPriceEntry = newReleaseDepositPriceEntry(depositPriceTestZeroEntry(t, fixture.cfg.Netuid, 1))
		}, "does not match the policy price"},
		"entry removed": {func(artifact *ReleaseMeasurementArtifact) { artifact.DepositPriceEntry = nil }, "does not match the policy price"},
		"audit rate": {func(artifact *ReleaseMeasurementArtifact) {
			artifact.DepositAudits[0].RateNumeratorRaoPerGiB--
		}, "not reconstructed"},
		"audit required": {func(artifact *ReleaseMeasurementArtifact) {
			artifact.DepositAudits[0].RequiredDepositRao, artifact.DepositAudits[0].ObservedDepositRao = "1", "1"
		}, "not reconstructed"},
		"unknown evidence":              {func(artifact *ReleaseMeasurementArtifact) { artifact.DepositAudits[0].UsageEvidence = "other" }, "usage evidence"},
		"no observation":                {func(artifact *ReleaseMeasurementArtifact) { artifact.DepositAudits[0].HttpObservationHash = "" }, "observation hash"},
		"payout claim of a ledger hash": {func(artifact *ReleaseMeasurementArtifact) { artifact.DepositAudits[0].UsageEvidence = "" }, "does not equal its on-chain commitment"},
	} {
		t.Run(name, func(t *testing.T) {
			artifact := cloneReleaseMeasurementArtifact(t, run.decision)
			test.change(artifact)
			if err := verify(artifact); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("want %q: %v", test.message, err)
			}
		})
	}
	// No signed price and no entry: usage evidence has no standing.
	t.Run("usage evidence under the document", func(t *testing.T) {
		artifact := cloneReleaseMeasurementArtifact(t, run.decision)
		artifact.Policy = exactPolicy(t)
		artifact.DepositPriceEntry = nil
		if err := verifyDepositAuditShape(artifact.DepositAudits[0], artifact, artifact.Policy.Deposit); err == nil || !strings.Contains(err.Error(), "usage evidence") {
			t.Fatalf("usage evidence accepted without a signed price: %v", err)
		}
	})
	t.Run("legacy measurement", func(t *testing.T) {
		artifact := cloneReleaseMeasurementArtifact(t, run.decision)
		artifact.Schema = ReleaseMeasurementSchema
		if err := verifyReleaseMeasurementIdentity(artifact); err == nil || !strings.Contains(err.Error(), "signed deposit price") {
			t.Fatalf("a legacy measurement took a signed price: %v", err)
		}
	})
	t.Run("the compact wire holds the entry", func(t *testing.T) {
		encoded, err := canonicalReleaseMeasurementBytes(run.decision)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeReleaseMeasurementV2Bytes(t.Context(), encoded, fixture.cfg.EvidenceV2.Bounds.MaxArtifactBytes, 4)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded.DepositPriceEntry, run.decision.DepositPriceEntry) || decoded.DepositAudits[0] != run.decision.DepositAudits[0] {
			t.Fatalf("decoded %+v", decoded.DepositPriceEntry)
		}
		remaining := uint64(1 << 20)
		if err := releaseMeasurementV2ControlStorage(t.Context(), reflect.ValueOf(decoded), &remaining); err != nil {
			t.Fatalf("the control walk refuses the new fields: %v", err)
		}
	})
}

// The identity check of the signed document, exactly as compact artifacts run it. It
// hashes the document, and a copy with the signed price laid over it would not match.
func verifyReleaseMeasurementCommonIdentityForTest(artifact *ReleaseMeasurementArtifact) error {
	owned := *artifact
	owned.ControlledNOIDs, owned.Inputs, owned.Bindings, owned.HeadEMA = []uint64{}, []ReleaseMeasurementInput{}, []ReleaseBindingMeasurement{}, []HeadEMAMeasurement{}
	deposit, err := releaseMeasurementDepositPolicy(&owned)
	if err != nil {
		return err
	}
	if scored := releaseScoringPolicy(owned.Policy, deposit); scored.IsZeroPrice() == owned.Policy.IsZeroPrice() {
		return errors.New("the signed price is not in force in the scoring copy")
	}
	return verifyReleaseMeasurementCommonIdentity(&owned)
}

// The owner-recycle production row re-reads the same commit, so a production
// decision cannot carry a usage audit the chain does not bear out.
func TestOwnerRecycleUsageAuditMatchesItsCommit(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	observed, err := fixture.observe(t, run.decision)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.operators[0].usageSettled || observed.operators[0].usageCommit == nil || observed.operators[1].usageSettled {
		t.Fatalf("observed %+v", observed.operators)
	}
	if err := verifyOwnerRecycleDepositAuditSource(run.audit, observed.operators[0], observed, run.decision); err != nil {
		t.Fatal(err)
	}
	if err := verifyOwnerRecycleDepositAuditSource(run.decision.DepositAudits[1], observed.operators[1], observed, run.decision); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*DepositAudit){
		"ledger hash":  func(audit *DepositAudit) { audit.CommittedArtifactHash = releaseHex32([32]byte{9}) },
		"root":         func(audit *DepositAudit) { audit.PayoutRoot = releaseHex32([32]byte{9}) },
		"commit block": func(audit *DepositAudit) { audit.RootCommitBlock++ },
		"committer":    func(audit *DepositAudit) { audit.RootCommitter = "0x" + strings.Repeat("99", 20) },
		"boundary":     func(audit *DepositAudit) { audit.SourceEndBlock++ },
		"other path":   func(audit *DepositAudit) { audit.UsageEvidence = "" },
	} {
		changed := run.audit
		change(&changed)
		if err := verifyOwnerRecycleDepositAuditSource(changed, observed.operators[0], observed, run.decision); err == nil {
			t.Errorf("%s: production accepted a changed usage audit", name)
		}
	}
	claimed := run.audit
	if err := verifyOwnerRecycleDepositAuditSource(claimed, observed.operators[1], observed, run.decision); err == nil {
		t.Error("production accepted a usage audit on a v1 source epoch")
	}
}

// depositAuditV2UsageChain gives a publication fixture vault2 and its events.
func depositAuditV2UsageChain(t *testing.T, fixture *depositAuditPublicationV2TestFixture) *depositUsageTestChain {
	t.Helper()
	chain := fixture.chain
	resolver := &settlementV2ResolverTestFixture{releaseDecisionV2TestFixture: chain,
		addresses: SettlementV2Addresses{Settlement: common.HexToAddress("0x5e77100000000000000000000000000000000002"), Vault: common.HexToAddress("0x7a17000000000000000000000000000000000002")},
		head:      chain.finalized, pools: map[uint64]stabi.ISTSettlementVault2Pool{}, identities: map[uint64]stabi.SettlementAtOutput{}, raw: map[string][]byte{}, v2Calls: map[string][]uint64{}}
	wrapped := newDepositUsageTestChain(t, resolver)
	runtime := fixture.base.runtime
	runtime.chain = chain.chain
	runtime.cfg.SettlementV2 = &ReleaseSettlementV2Config{Settlement: strings.ToLower(resolver.addresses.Settlement.Hex()), Vault: strings.ToLower(resolver.addresses.Vault.Hex())}
	runtime.history.cfg = runtime.cfg
	// The source epoch's own policy, read for its commit window: the decision's.
	coordinator := chain.chain.coordinator
	decision := chain.query.boundary.SettlementEpoch
	chain.views[fmt.Sprintf("%x", coordinator.PackPolicyAt(new(big.Int).SetUint64(decision-1)))] = chain.views[fmt.Sprintf("%x", coordinator.PackPolicyAt(new(big.Int).SetUint64(decision)))]
	return wrapped
}

// The exchanges an audit's retained observation holds.
func depositUsageTestExchanges(t *testing.T, observation []byte) []releaseArtifactHttpExchangeV2 {
	t.Helper()
	var value releaseArtifactHttpObservationV2
	if err := json.Unmarshal(observation, &value); err != nil {
		t.Fatal(err)
	}
	return value.Exchanges
}

// Today's metagraph but for operator 1's v1 pool UID, and the v2 pool's UID.
func depositAuditV2Metagraph(chain *depositUsageTestChain, fixture *depositAuditPublicationV2TestFixture, v1Pool [32]byte) {
	chain.setMetagraph(v1Pool, chainBatchHotkey(1), fixture.base.hotkey.PublicKey(), settlementV2TestPoolHotkey)
}

// The bug: publishing the deposit audit read every active pool without the
// settlement v2 contracts, so a v1 pool hotkey pruned after the switch refused the
// whole decision. The reader without them shows what used to happen, and with them
// the decision publishes at the same pinned block. Nothing but the reader changes.
func TestDepositAuditPublicationSurvivesAPrunedV1PoolUID(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "no-payout")
	chain := depositAuditV2UsageChain(t, fixture)
	depositAuditV2Metagraph(chain, fixture, chainBatchHotkey(9))
	chain.registerPool(fixture.sourceNoIds[0], 1)
	block := chain.query.boundary.EVMBlock
	chain.identities[fixture.sourceNoIds[0]] = stabi.SettlementAtOutput{Vault: chain.addresses.Vault, Hotkey: settlementV2TestPoolHotkey, Version: 2}
	withoutContracts := chain.query
	if _, err := chain.chain.readReleaseDecisionChainV2Context(t.Context(), withoutContracts); err == nil || !strings.Contains(err.Error(), "decision active operator identity or unique pool UID is absent") {
		t.Fatalf("the v1 reader accepted a pruned pool hotkey: %v", err)
	}
	withContracts := chain.query
	withContracts.settlementV2 = chain.addresses
	observed, err := chain.chain.readReleaseDecisionChainV2Context(t.Context(), withContracts)
	if err != nil {
		t.Fatal(err)
	}
	if observed.pools[0].UID != 3 || observed.pools[0].PoolHotkey != releaseHex32(settlementV2TestPoolHotkey) {
		t.Fatalf("pools %+v", observed.pools)
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatalf("a pruned v1 pool UID refused the deposit audit: %v", err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	if len(publication.Members) != 2 || len(chain.v2Reads("settlementAt")) == 0 || !slices.Contains(chain.v2Reads("pools"), block) {
		t.Fatalf("publication or reads differ: %d %v", len(publication.Members), chain.v2Reads("pools"))
	}
	// A restart reopens the same bundle at the same chain state.
	fixture.restart()
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatalf("a retained publication refused the pruned UID: %v", err)
	}
}

// At zero price the published payload is exactly what it was: today's fields and
// no others, with the contracts configured and the v1 pool UID gone.
func TestDepositAuditZeroPricePayloadBytesAreUnchanged(t *testing.T) {
	zero := func(cfg *ReleaseConfig) {
		cfg.Policy = zeroPricePolicy(t)
		hash, err := cfg.Policy.HashHex()
		if err != nil {
			t.Fatal(err)
		}
		cfg.PolicyHash = hash
	}
	for _, name := range []string{"without settlement v2", "with settlement v2 and a pruned v1 pool"} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositAuditPublicationV2TestFixtureWithBounds(t, "no-payout", nil, 1, zero)
			if name != "without settlement v2" {
				chain := depositAuditV2UsageChain(t, fixture)
				depositAuditV2Metagraph(chain, fixture, chainBatchHotkey(9))
				chain.registerPool(fixture.sourceNoIds[0], 1)
			}
			if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
				t.Fatal(err)
			}
			manifest, _ := fixture.retained(t)
			publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := json.Marshal(manifest.Decision)
			if err != nil {
				t.Fatal(err)
			}
			for index, member := range publication.Members {
				audit := fixture.artifact.DepositAudits[index]
				want := fmt.Sprintf(`{"schema":"urnetwork-validator-deposit-audit-v2","decision":%s,"audit":{"no_id":%d,"epoch":%d,"source_epoch":%d,"observed_at_block":%d,"artifact_deadline_block":%d,"usage_bytes":0,"conviction_before_rao":%q,"rate_numerator_rao_per_gib":0,"rate_denominator":0,"required_deposit_rao":"0","observed_deposit_rao":"15","status":"zero_price","compliant":true,"disposition":"zero_price_no_deposit_required"}}`+"\n", decision, audit.NoID, audit.Epoch, audit.SourceEpoch, audit.ObservedAtBlock, audit.ArtifactDeadlineBlock, fixture.chain.bigConviction.String())
				if string(member.Payload) != want {
					t.Fatalf("zero-price payload bytes changed:\n%s\n%s", member.Payload, want)
				}
			}
		})
	}
}

// The whole path a usage audit takes to the public: live gather, durable intent's
// publish, retained restart, and capture for an archive.
func TestDepositUsagePublicationCarriesItsObservationAndEntry(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "no-payout")
	chain := depositAuditV2UsageChain(t, fixture)
	depositAuditV2Metagraph(chain, fixture, chainBatchHotkey(0))
	runtime := fixture.base.runtime
	window := fixture.options.Window
	noID := fixture.sourceNoIds[0]
	chain.registerPool(noID, window.Epoch)
	cfg := runtime.cfg
	operator := newDepositUsageTestOperator(cfg.DeploymentID, cfg.Netuid)
	fixture.usageRoutes.Store(operator.serve)
	rows := depositUsageTestRows()
	var chunks []string
	var refs []ledger.Ref
	for _, chunk := range ledger.EncodeProviderChunks(rows) {
		ref := operator.putObject(chunk)
		refs, chunks = append(refs, ref), append(chunks, ref.String())
	}
	header, err := ledger.EncodeHeader(&ledger.Header{NoID: noID, Epoch: window.Epoch, Epochs: []ledger.HeaderEpoch{{Epoch: window.Epoch, Providers: refs}}, Accounts: []ledger.Ref{}})
	if err != nil {
		t.Fatal(err)
	}
	headerRef := operator.putObject(header)
	chain.commit(t, noID, 5, window.Epoch, [32]byte{0xab}, headerRef, window.EndBlock+1)
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	total, count, err := usage.SumRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	statement := &usage.Statement{DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash, Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), VaultV2: chain.addresses.Vault, PolicyHash: cfg.PolicyHash, NoID: noID, Epoch: window.Epoch,
		Start: usage.Boundary{Number: window.StartBlock, Hash: attemptHex32(chain.blocks[window.StartBlock])}, End: usage.Boundary{Number: window.EndBlock, Hash: attemptHex32(chain.blocks[window.EndBlock])},
		ProviderChunks: chunks, ProviderRows: count, TotalUsageBytes: total, TotalUsers: depositUsageTestUsers}
	if err := statement.Seal(key); err != nil {
		t.Fatal(err)
	}
	operator.publish(t, statement, "")
	entry := depositPriceTestEntry(t, cfg.Netuid, 1, 5_000_000_000)
	required, _, err := protocol.RequiredDepositRao(total, depositUsageTestUsers, chain.bigConviction, entry.DepositPolicy(cfg.Policy.Deposit))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := chain.chain.coordinator
	epoch, id := new(big.Int).SetUint64(chain.query.boundary.SettlementEpoch), new(big.Int).SetUint64(noID)
	chain.set(t, "epochDeposits", coordinator.PackEpochDeposits(epoch, id), required)
	chain.set(t, "cumulativeConviction", coordinator.PackCumulativeConviction(id), new(big.Int).Add(new(big.Int).Add(new(big.Int).Set(chain.bigConviction), required), big.NewInt(7)))
	contexts, operators := map[uint64]*ReleaseMeasurementContext{}, map[uint64]OperatorConfig{}
	for _, config := range cfg.Operators {
		reader, err := NewHTTPArtifactReader(config.APIURL, cfg.DeploymentID, cfg.Netuid)
		if err != nil {
			t.Fatal(err)
		}
		contexts[config.NoID] = &ReleaseMeasurementContext{NoID: config.NoID, Artifacts: reader}
		operators[config.NoID] = config
	}
	poolCfg := cfg
	poolCfg.ControlledNOIDs = slices.Clone(fixture.sourceNoIds[:])
	steerer := &ReleaseSteerer{cfg: &poolCfg, chain: chain.chain, hotkey: fixture.base.hotkey, contexts: contexts, operators: operators, settlementV2: chain.addresses, depositPrices: depositPriceTestPrices(cfg.Netuid, entry)}
	observed, err := chain.chain.readReleaseDecisionChainV2Context(t.Context(), chain.query)
	if err != nil {
		t.Fatal(err)
	}
	block := chain.query.boundary.EVMBlock
	snapshot := &ReleaseSnapshot{BlockNumber: block, BlockHash: chain.blocks[block], Epoch: epoch, Policy: stabi.STCoordinatorPolicySnapshot{RootCommitWindowBlocks: cfg.Policy.Settlement.RootCommitWindowBlocks}}
	fixture.artifact.DepositPriceEntry = newReleaseDepositPriceEntry(entry)
	fixture.artifact.Pools, fixture.artifact.DepositAudits, err = steerer.gatherPoolsV2(t.Context(), snapshot, map[uint64]map[connect.Id]bool{}, observed.hotkeyUIDs, fixture.artifact)
	if err != nil {
		t.Fatal(err)
	}
	audit := fixture.artifact.DepositAudits[0]
	if audit.Status != DepositAuditCompliant || audit.UsageEvidence != usage.EvidenceKind || audit.HttpObservationHash == "" || audit.RequiredDepositRao != required.String() {
		t.Fatalf("usage audit %+v", audit)
	}
	if err := runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := decodeValidatorEvidenceDepositAuditV2Payload(t.Context(), publication.Members[0].Payload, fixture.options.Bounds.MaxTransitionBytes)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Audit != audit || !reflect.DeepEqual(payload.DepositPriceEntry, fixture.artifact.DepositPriceEntry) || len(payload.HttpObservation) == 0 {
		t.Fatalf("payload %+v", payload.Audit)
	}
	var retained releaseArtifactHttpObservationV2
	if err := json.Unmarshal(payload.HttpObservation, &retained); err != nil {
		t.Fatal(err)
	}
	if retained.Schema != releaseUsageHttpObservationSchemaV2 || len(retained.Exchanges) != 2 {
		t.Fatalf("observation %s with %d exchanges", retained.Schema, len(retained.Exchanges))
	}
	// A restart replays from the original bundle: no operator read, and a later
	// artifact's price does not change what the original decision was judged by.
	fixture.restart()
	requests := operator.requests.Load()
	moved := *fixture.artifact
	moved.DepositPriceEntry = newReleaseDepositPriceEntry(depositPriceTestEntry(t, cfg.Netuid, 1, 9_000_000_000))
	if err := runtime.publishDepositAuditV2(t.Context(), &moved); err != nil {
		t.Fatalf("a retained publication: %v", err)
	}
	if operator.requests.Load() != requests {
		t.Fatal("a retained publication asked the operator's API")
	}
	// A tampered entry in a public payload is refused by its readers.
	forged := payload
	forged.DepositPriceEntry = newReleaseDepositPriceEntry(entry)
	forged.DepositPriceEntry.EpochCapRao++
	if _, err := verifiedReleaseDepositPriceEntry(forged.DepositPriceEntry, cfg.ChainID, cfg.Netuid, chain.query.boundary.SettlementEpoch); err == nil {
		t.Fatal("a tampered public entry verified")
	}
	// An archive reads the same objects from its capture, and refuses another's bytes.
	sources := map[ReleaseEvidenceV2CaptureSource][]byte{}
	if err := emitDepositUsageObjectsV2(t.Context(), cfg.StateDir, cfg.Operators[0].APIURL, audit, depositUsageTestExchanges(t, payload.HttpObservation), func(source ReleaseEvidenceV2CaptureSource, encoded []byte) error {
		sources[source] = encoded
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1+len(refs) || !bytes.Equal(sources[ReleaseEvidenceV2CaptureSource{Kind: releaseUsageLedgerObjectCaptureKind, Name: headerRef.String()}], header) {
		t.Fatalf("captured %d sources", len(sources))
	}
	archiveSources := map[ReleaseEvidenceV2CaptureSource]ReleaseEvidenceV2ArchiveSource{}
	for source, encoded := range sources {
		archiveSources[source] = ReleaseEvidenceV2ArchiveSource{Source: source, SizeBytes: uint64(len(encoded)), ContentHash: ReleaseMeasurementContentHash(encoded)}
	}
	root := newAttemptSettlementRuntimeV2TestStateDir(t)
	owner := &releaseEvidenceV2ArchiveOwner{ctx: t.Context(), cfg: cfg, root: root, sources: archiveSources, readSource: func(_ context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		return sources[source], nil
	}}
	if owner.anchor, err = os.Lstat(root); err != nil {
		t.Fatal(err)
	}
	references := &releaseEvidenceV2StartupReferences{remaining: 1, archive: owner}
	data, err := references.depositUsageObject(t.Context(), cfg.StateDir, headerRef, ledger.MaxHeaderBytes)
	if err != nil || !bytes.Equal(data, header) {
		t.Fatalf("archive read: %v", err)
	}
	if references.remaining != 1 {
		t.Fatal("a ledger object was charged to the history allowance")
	}
	if _, err := references.depositUsageObject(t.Context(), cfg.StateDir, ledger.RefOf([]byte("absent")), ledger.MaxHeaderBytes); err == nil {
		t.Fatal("an archive read an object its capture does not hold")
	}
}

// The real compact verifier, its strict wire and its full replay, under a signed
// price: the document in the artifact and its hash are untouched, the decision is
// the same when the entry's rates are the document's, and a usage audit's different
// hashes pass only as usage evidence.
func TestReleaseMeasurementV2VerifiesASignedPrice(t *testing.T) {
	shared := newReleaseMeasurementV2TestFixture(t, 15)
	signed := func(t *testing.T, tiers []protocol.DepositTier, effective uint64) *ReleaseDepositPriceEntry {
		t.Helper()
		entry := &pricesheet.Entry{EffectiveEpoch: effective, EpochCapRao: shared.artifact.Policy.Deposit.EpochCapRaoPerOperator, Tiers: slices.Clone(tiers)}
		depositPriceTestSign(t, shared.artifact.Netuid, depositPriceTestKey, entry)
		return newReleaseDepositPriceEntry(entry)
	}
	fixture := func(t *testing.T) *releaseMeasurementV2TestFixture {
		t.Helper()
		return copyReleaseMeasurementTestProvider(t, shared)
	}
	tiers := shared.artifact.Policy.Deposit.Tiers
	t.Run("the document's rates", func(t *testing.T) {
		fixture := fixture(t)
		fixture.artifact.DepositPriceEntry = signed(t, tiers, 1)
		encoded, _, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
		if err != nil {
			t.Fatal(err)
		}
		artifact, result, err := DecodeReleaseMeasurementArtifactV2(t.Context(), encoded, fixture.options(t))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Decision, shared.want) || !reflect.DeepEqual(artifact.DepositPriceEntry, fixture.artifact.DepositPriceEntry) || !reflect.DeepEqual(artifact.Policy, shared.artifact.Policy) {
			t.Fatal("a signed entry at the document's rates changed the decision or the document")
		}
		if !bytes.Contains(encoded, []byte(`"deposit_price_entry":{"effective_epoch":1,`)) {
			t.Fatalf("the sealed artifact does not carry its entry: %s", encoded[max(0, len(encoded)-400):])
		}
	})
	t.Run("usage evidence", func(t *testing.T) {
		fixture := fixture(t)
		fixture.artifact.DepositPriceEntry = signed(t, tiers, 1)
		audit := &fixture.artifact.DepositAudits[0]
		audit.UsageEvidence, audit.HttpObservationHash = usage.EvidenceKind, "sha256:"+strings.Repeat("ab", 32)
		audit.CommittedArtifactHash = releaseHex32([32]byte{0xcd})
		if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t)); err != nil {
			t.Fatal(err)
		}
	})
	for name, change := range map[string]func(*testing.T, *ReleaseMeasurementArtifact){
		"a forged rate": func(t *testing.T, artifact *ReleaseMeasurementArtifact) {
			artifact.DepositPriceEntry = signed(t, tiers, 1)
			artifact.DepositPriceEntry.Tiers[0].RateNumeratorRaoPerGiB--
		},
		"an entry not yet in effect": func(t *testing.T, artifact *ReleaseMeasurementArtifact) {
			artifact.DepositPriceEntry = signed(t, tiers, artifact.SettlementEpoch+1)
		},
		"rates the audits were not judged by": func(t *testing.T, artifact *ReleaseMeasurementArtifact) {
			halved := slices.Clone(tiers)
			for index := range halved {
				halved[index].RateNumeratorRaoPerGiB /= 2
			}
			artifact.DepositPriceEntry = signed(t, halved, 1)
		},
		"a zero price": func(t *testing.T, artifact *ReleaseMeasurementArtifact) {
			artifact.DepositPriceEntry = newReleaseDepositPriceEntry(depositPriceTestZeroEntry(t, artifact.Netuid, 1))
		},
		"usage evidence without a signed price": func(t *testing.T, artifact *ReleaseMeasurementArtifact) {
			audit := &artifact.DepositAudits[0]
			audit.UsageEvidence, audit.HttpObservationHash = usage.EvidenceKind, "sha256:"+strings.Repeat("ab", 32)
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := fixture(t)
			change(t, fixture.artifact)
			if _, err := VerifyReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t)); err == nil {
				t.Fatal("the compact verifier accepted it")
			}
		})
	}
}

// The fixed rules of the audit stay the document's, so the lag every other reader
// takes from the document is the lag in force, and the helper is the package's rule.
func TestDepositPriceEntryKeepsTheDocumentsFixedRules(t *testing.T) {
	policy := zeroPricePolicy(t)
	hash, err := policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	entry := depositPriceTestEntry(t, 521, 1, 1_000_000_000)
	sheet := &pricesheet.Sheet{Netuid: 521, Entries: []pricesheet.Entry{*entry}}
	for _, epoch := range []uint64{0, 1, 2} {
		effective := releaseEffectiveDepositPolicy(sheet.At(epoch), policy.Deposit)
		if !reflect.DeepEqual(effective, pricesheet.Effective(sheet, epoch, policy.Deposit)) {
			t.Fatalf("epoch %d: %+v", epoch, effective)
		}
		if effective.UsageLagEpochs != policy.Deposit.UsageLagEpochs || effective.TierSnapshot != policy.Deposit.TierSnapshot || effective.MismatchAction != policy.Deposit.MismatchAction || effective.UnavailableAction != policy.Deposit.UnavailableAction {
			t.Fatalf("epoch %d changed a fixed rule: %+v", epoch, effective)
		}
	}
	scored := releaseScoringPolicy(policy, releaseEffectiveDepositPolicy(entry, policy.Deposit))
	if scored.IsZeroPrice() || !policy.IsZeroPrice() {
		t.Fatal("the scoring copy shares the document's deposit")
	}
	if after, err := policy.HashHex(); err != nil || after != hash {
		t.Fatalf("building the scoring copy changed the document: %s %v", after, err)
	}
}

// An archive replays a usage audit from its captured sources alone: the retained
// observation and the ledger objects capture emitted, with no validator state.
func TestDepositUsageReplaysFromAnArchiveWithoutValidatorState(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	reader, err := NewHTTPArtifactReader(fixture.cfg.Operators[0].APIURL, fixture.cfg.DeploymentID, fixture.cfg.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	_, path, err := releaseDepositObservationRequestV2(&fixture.cfg, fixture.steerer.hotkey.PublicKey(), run.decision, run.audit, reader)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[ReleaseEvidenceV2CaptureSource][]byte{}
	if err := emitDepositUsageObjectsV2(t.Context(), fixture.cfg.StateDir, fixture.cfg.Operators[0].APIURL, run.audit, depositUsageTestExchanges(t, observation), func(source ReleaseEvidenceV2CaptureSource, encoded []byte) error {
		sources[source] = encoded
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	name, err := filepath.Rel(fixture.cfg.StateDir, path)
	if err != nil {
		t.Fatal(err)
	}
	sources[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: filepath.ToSlash(name)}] = observation
	archiveSources := map[ReleaseEvidenceV2CaptureSource]ReleaseEvidenceV2ArchiveSource{}
	for source, encoded := range sources {
		archiveSources[source] = ReleaseEvidenceV2ArchiveSource{Source: source, SizeBytes: uint64(len(encoded)), ContentHash: ReleaseMeasurementContentHash(encoded)}
	}
	observed, err := fixture.observe(t, run.decision)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture.cfg.StateDir); err != nil {
		t.Fatal(err)
	}
	root := newAttemptSettlementRuntimeV2TestStateDir(t)
	owner := &releaseEvidenceV2ArchiveOwner{ctx: t.Context(), cfg: fixture.cfg, root: root, sources: archiveSources, readSource: func(_ context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		return sources[source], nil
	}}
	if owner.anchor, err = os.Lstat(root); err != nil {
		t.Fatal(err)
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes, archive: owner}
	replayed, err := fixture.history().historicalDepositAuditWithCaptureV2(t.Context(), observed, observed.operators[0], run.audit, run.decision, custody)
	if err != nil || replayed != run.audit {
		t.Fatalf("archive replay %+v differs from %+v: %v", replayed, run.audit, err)
	}
}

// A capture counts ledger objects as data, so a provider chunk cannot spend the much
// smaller control allowance.
func TestDepositUsageCaptureCountsObjectsAsData(t *testing.T) {
	var retained []ReleaseEvidenceV2CaptureSource
	budget, err := newReleaseEvidenceCaptureBudgetV2(t.Context(), ReleaseEvidenceV2CaptureOptions{Hotkey: [32]byte{1}, MaximumBytes: 1024, MaximumObjects: 8, MaximumDataBytes: 1024, MaximumControlBytes: 8}, func(_ context.Context, source ReleaseEvidenceV2CaptureSource, _ []byte) error {
		retained = append(retained, source)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte{1}, 512)
	if err := budget.emit(ReleaseEvidenceV2CaptureSource{Kind: releaseUsageLedgerObjectCaptureKind, Name: ledger.RefOf(chunk).String()}, chunk); err != nil {
		t.Fatalf("a ledger object spent control bytes: %v", err)
	}
	if err := budget.emit(ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "control"}, bytes.Repeat([]byte{2}, 512)); err == nil {
		t.Fatal("a control source took the data allowance")
	}
	if len(retained) != 1 {
		t.Fatalf("retained %v", retained)
	}
}

// A payout artifact observation keeps exactly its old finite shape: the usage
// schema's larger census is not its to use.
func TestArtifactHttpObservationKeepsItsExchangeCensus(t *testing.T) {
	hotkey, err := crv4.KeypairFromSeed([32]byte{67})
	if err != nil {
		t.Fatal(err)
	}
	seal := func(schema string, exchanges int) (releaseArtifactHttpObservationV2, []byte) {
		value := releaseArtifactHttpObservationV2{Schema: schema, ValidatorHotkey: releaseHex32(hotkey.PublicKey()), NoId: 1, Origin: "https://one.example", Exchanges: []releaseArtifactHttpExchangeV2{}}
		for range exchanges {
			value.Exchanges = append(value.Exchanges, releaseArtifactHttpExchangeV2{Url: "https://one.example/x", MaximumBytes: 1, Status: http.StatusOK, Body: []byte{}})
		}
		digest, err := releaseArtifactHttpObservationDigestV2(value)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := hotkey.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		value.Signature = hex.EncodeToString(signature)
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return value, encoded
	}
	for _, test := range []struct {
		schema    string
		exchanges int
		want      bool
	}{
		{releaseArtifactHttpObservationSchemaV2, 0, false}, {releaseArtifactHttpObservationSchemaV2, 1, true}, {releaseArtifactHttpObservationSchemaV2, 2, true}, {releaseArtifactHttpObservationSchemaV2, 3, false},
		{releaseUsageHttpObservationSchemaV2, 0, true}, {releaseUsageHttpObservationSchemaV2, maximumUsageExchanges, true}, {releaseUsageHttpObservationSchemaV2, maximumUsageExchanges + 1, false},
	} {
		value, encoded := seal(test.schema, test.exchanges)
		value.Exchanges, value.Signature = nil, ""
		_, err := decodeArtifactHttpObservationV2(t.Context(), encoded, 1<<20, value)
		if (err == nil) != test.want {
			t.Errorf("%s with %d exchanges: %v", test.schema, test.exchanges, err)
		}
	}
}

// An archive's payout reader never mistakes a usage statement for a payout artifact.
func TestArchivePayoutRefusesAUsageObservation(t *testing.T) {
	fixture := newDepositUsageTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settle(t)
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	root := newAttemptSettlementRuntimeV2TestStateDir(t)
	owner := &releaseEvidenceV2ArchiveOwner{ctx: t.Context(), cfg: fixture.cfg, root: root, sources: map[ReleaseEvidenceV2CaptureSource]ReleaseEvidenceV2ArchiveSource{}}
	anchor, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	owner.anchor = anchor
	archive := &ReleaseEvidenceV2Archive{owner: owner, inputs: []releaseEvidenceV2ActivationInput{{}}}
	if _, err := archive.payoutSourceForMeasurementV2(t.Context(), run.decision, 1); err == nil || !strings.Contains(err.Error(), "no original signed HTTP observation") {
		t.Fatalf("a usage audit has a payout source: %v", err)
	}
}

// The evaluator's own refusals, independent of any source.
func TestEvaluateDepositUsageRefusesIncompleteInputs(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("b2", 32))
	if err != nil {
		t.Fatal(err)
	}
	expectation := &DepositUsageExpectation{DeploymentID: "d", ChainID: 945, GenesisHash: releaseHex32([32]byte{1}), Netuid: 521, Coordinator: common.Address{1}, VaultV2: common.Address{2}, PolicyHash: releaseHex32([32]byte{3}), Epoch: 4, NoID: 1, Signer: crypto.PubkeyToAddress(key.PublicKey),
		Start: payoutartifact.Boundary{Number: 10, Hash: releaseHex32([32]byte{4})}, End: payoutartifact.Boundary{Number: 20, Hash: releaseHex32([32]byte{5})}, ProviderChunks: []string{}, Root: [32]byte{6}, LedgerHash: [32]byte{7}, RootSigner: common.Address{8}, CommitBlock: 21}
	statement := &usage.Statement{DeploymentID: "d", ChainID: 945, GenesisHash: expectation.GenesisHash, Netuid: 521, Coordinator: common.Address{1}, VaultV2: common.Address{2}, PolicyHash: expectation.PolicyHash, NoID: 1, Epoch: 4, Start: usage.Boundary{Number: 10, Hash: expectation.Start.Hash}, End: usage.Boundary{Number: 20, Hash: expectation.End.Hash}, ProviderChunks: []string{}}
	if err := statement.Seal(key); err != nil {
		t.Fatal(err)
	}
	policy := protocol.DepositPolicy{EpochCapRaoPerOperator: 10, Tiers: []protocol.DepositTier{{RateNumeratorRaoPerGiB: 1, RateDenominator: 1}}}
	if audit := EvaluateDepositUsage(statement, DepositUsageRows{}, expectation, 5, big.NewInt(0), big.NewInt(0), policy); !audit.Compliant || audit.RequiredDepositRao != "0" {
		t.Fatalf("an empty epoch owes nothing: %+v", audit)
	}
	for name, test := range map[string]func() DepositAudit{
		"negative deposit": func() DepositAudit {
			return EvaluateDepositUsage(statement, DepositUsageRows{}, expectation, 5, big.NewInt(-1), big.NewInt(0), policy)
		},
		"nil conviction": func() DepositAudit {
			return EvaluateDepositUsage(statement, DepositUsageRows{}, expectation, 5, big.NewInt(0), nil, policy)
		},
		"no statement": func() DepositAudit {
			return EvaluateDepositUsage(nil, DepositUsageRows{}, expectation, 5, big.NewInt(0), big.NewInt(0), policy)
		},
		"no commit": func() DepositAudit {
			changed := *expectation
			changed.CommitBlock = 0
			return EvaluateDepositUsage(statement, DepositUsageRows{}, &changed, 5, big.NewInt(0), big.NewInt(0), policy)
		},
		"other chunks": func() DepositAudit {
			changed := *expectation
			changed.ProviderChunks = []string{"sha256:" + strings.Repeat("00", 32)}
			return EvaluateDepositUsage(statement, DepositUsageRows{}, &changed, 5, big.NewInt(0), big.NewInt(0), policy)
		},
		"bad formula": func() DepositAudit {
			return EvaluateDepositUsage(statement, DepositUsageRows{}, expectation, 5, big.NewInt(0), big.NewInt(0), protocol.DepositPolicy{})
		},
	} {
		if audit := test(); audit.Compliant || audit.Status != DepositAuditInvalid || audit.Error == "" || audit.UsageEvidence != usage.EvidenceKind {
			t.Errorf("%s: %+v", name, audit)
		}
	}
}
