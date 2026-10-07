// The public adapter fixtures use the existing HTTP body/receipt/getter reader
// and the actual owned subprocess boundary. Synthetic Go trace peers are not
// a claim about live Wasm, chain finality or approved runtime call sites.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	substrate "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/urfoundation/sn/v2026/protocol"
)

type economicConservationFixture struct {
	native              *economicEmissionFixture
	vault               *monitorEvmFixture
	policy              economicConservationPolicy
	path                string
	checkpoint          string
	now                 time.Time
	claimReads          atomic.Uint64
	claimFault          atomic.Bool
	claimPaymentUnknown atomic.Bool
}

// Mutated synthetic ABI values must update receipt tries, all descendant
// headers and the native Frontier commitment before a real reader sees them.
func economicConservationTestEvmRehash(t *testing.T, f *monitorEvmFixture, mutate func(uint64, *types.Receipt)) {
	t.Helper()
	f.byHash, f.byTransaction = map[string]*monitorEvmFixtureBlock{}, map[string]map[string]any{}
	parent := common.Hash{}
	for _, number := range []uint64{0, 10, 11, 12, 13} {
		block := f.blocks[number]
		block.header.ParentHash = parent
		if len(block.receipts) != 0 {
			receipts := make(types.Receipts, len(block.receipts))
			block.header.Bloom = types.Bloom{}
			for index, fields := range block.receipts {
				raw, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				var receipt types.Receipt
				if err := json.Unmarshal(raw, &receipt); err != nil {
					t.Fatal(err)
				}
				if mutate != nil {
					mutate(number, &receipt)
				}
				receipt.Bloom = types.CreateBloom(&receipt)
				for word := range block.header.Bloom {
					block.header.Bloom[word] |= receipt.Bloom[word]
				}
				receipts[index] = &receipt
			}
			block.header.ReceiptHash = types.DeriveSha(receipts, trie.NewStackTrie(nil))
			logIndex := uint(0)
			for index, receipt := range receipts {
				receipt.BlockHash, receipt.TransactionIndex = block.header.Hash(), uint(index)
				for _, log := range receipt.Logs {
					log.BlockHash, log.Index, log.BlockNumber, log.TxHash, log.TxIndex = receipt.BlockHash, logIndex, number, receipt.TxHash, uint(index)
					logIndex++
				}
				raw, err := json.Marshal(receipt)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				fields["from"], fields["to"], fields["contractAddress"] = block.receipts[index]["from"], block.receipts[index]["to"], nil
				block.receipts[index] = fields
				f.byTransaction[receipt.TxHash.Hex()] = fields
			}
		}
		f.byHash[block.header.Hash().Hex()] = block
		parent = block.header.Hash()
	}
	f.policy.From.Hash, f.policy.EvmGenesisHash = f.blocks[10].header.Hash().Hex(), f.blocks[0].header.Hash().Hex()
	f.mapping.evmHeader, f.mapping.evmHash = f.blocks[13].header, f.blocks[13].header.Hash().Hex()
	raw, err := rlp.EncodeToBytes(f.mapping.evmHeader)
	if err != nil {
		t.Fatal(err)
	}
	f.mapping.rawEvmHeader = hexutil.Encode(raw)
	f.mapping.replaceNativeLogs(t, []string{mappingTestDigest(t, 3, f.mapping.evmHash, nil)})
}

func economicConservationTestNativeMapping(t *testing.T, source *economicEmissionFixture, vault *monitorEvmFixture) {
	t.Helper()
	parent := source.chain.byHeight[100]
	for number := uint64(101); number <= 102; number++ {
		old := source.chain.byHeight[number]
		header := source.chain.headers[old]
		header.ParentHash = parent
		header.Digest.Logs = []string{mappingTestDigest(t, 3, vault.blocks[number-90].header.Hash().Hex(), nil)}
		parentHash, err := substrate.NewHashFromHexString(parent)
		if err != nil {
			t.Fatal(err)
		}
		stateHash, err := substrate.NewHashFromHexString(header.StateRoot)
		if err != nil {
			t.Fatal(err)
		}
		bodyHash, err := substrate.NewHashFromHexString(header.ExtrinsicsRoot)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := codec.Encode(substrate.Header{ParentHash: parentHash, Number: substrate.BlockNumber(number), StateRoot: stateHash, ExtrinsicsRoot: bodyHash, Digest: substrate.Digest{}})
		if err != nil {
			t.Fatal(err)
		}
		digest, err := hex.DecodeString(strings.TrimPrefix(header.Digest.Logs[0], "0x"))
		if err != nil {
			t.Fatal(err)
		}
		raw = append(append(raw[:len(raw)-1], 4), digest...)
		hash := rootExtrinsicHash(raw)
		source.chain.headers[hash], source.chain.bodies[hash], source.storageKVs[hash] = header, source.chain.bodies[old], source.storageKVs[old]
		delete(source.chain.headers, old)
		delete(source.chain.bodies, old)
		delete(source.storageKVs, old)
		source.chain.byHeight[number], parent = hash, hash
	}
	source.chain.finalized, source.policy.Through.Hash = parent, parent
}

func newEconomicConservationFixture(t *testing.T, twoProviders bool, configureVault ...func(*monitorEvmFixture)) *economicConservationFixture {
	t.Helper()
	f := &economicConservationFixture{native: newEconomicEmissionFixture(t), vault: newMonitorEvmFixture(t, "settlement-vault", false)}
	f.now = f.vault.services.clock.now().UTC()
	economicConservationTestEvmRehash(t, f.vault, func(number uint64, receipt *types.Receipt) {
		if number != 11 {
			return
		}
		receipt.Logs[0] = monitorEvmTestLog(t, f.vault.contract, common.HexToAddress(f.vault.policy.Address), "EmissionCaptured", mustMonitorEvmInteger("2"), mustMonitorEvmInteger("1"), [32]byte(common.HexToHash("0x"+strings.Repeat("11", 32))), mustMonitorEvmInteger("20"))
	})
	for _, configure := range configureVault {
		configure(f.vault)
	}
	economicConservationTestNativeMapping(t, f.native, f.vault)
	f.native.policy.Network.NativeChain, f.native.chain.action.Scope.NativeChain = "fixture-mainnet", "fixture-mainnet"
	f.native.set(t, 100, "PendingServerEmission", make([]byte, 8))
	nativeExecutionTestConfigure(t, f.native, func(number uint64, trace *historicalReplayObservations) {
		if !twoProviders || number != 101 {
			return
		}
		raw, err := json.Marshal(trace.Observations[nativeTestProvider])
		if err != nil {
			t.Fatal(err)
		}
		var second historicalReplayObservation
		if err := json.Unmarshal(raw, &second); err != nil {
			t.Fatal(err)
		}
		nativeExecutionTestChange(t, &second, "hotkey", bytes.Repeat([]byte{0x22}, 32))
		nativeExecutionTestChange(t, &second, "coldkey", bytes.Repeat([]byte{0x34}, 32))
		nativeExecutionTestChange(t, &second, "gross", nativeExecutionTestWords(89))
		nativeExecutionTestChange(t, &second, "captured", nativeExecutionTestWords(0))
		nativeExecutionTestChange(t, &second, "liquid", nativeExecutionTestWords(89))
		trace.Observations[nativeTestOwner] = second
	})
	if twoProviders {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x21}, ed25519.SeedSize))
		path := filepath.Join(f.native.policy.Execution.Directory, strings.TrimPrefix(f.native.chain.byHeight[101], "0x")+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var admission nativeExecutionAdmission
		if err := decodePlanJson(raw, &admission); err != nil {
			t.Fatal(err)
		}
		admission.Providers = append(admission.Providers, nativeExecutionRecipient{Uid: 1, Hotkey: "0x" + strings.Repeat("22", 32), Registered: 21, Coldkey: "0x" + strings.Repeat("34", 32)})
		message, err := admission.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		admission.Signature = hex.EncodeToString(ed25519.Sign(key, message))
		raw, err = json.Marshal(admission)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.policy = economicConservationPolicy{Schema: economicConservationPolicySchema, Native: monitorEconomicNativePolicy{Role: "native-conservation", Observation: f.native.policy, BatchBlocks: 2, HistoryEntries: 64, StallSeconds: 60, HistoricalFinality: "owned-rpc-assertion"}, Vault: f.vault.policy, Routes: []economicConservationRoute{{Hotkey: "0x" + strings.Repeat("11", 32), Coldkey: "0x" + strings.Repeat("33", 32), Kind: "tail-pool", PoolId: "1"}}, MaximumFacts: 256}
	if twoProviders {
		f.policy.Routes = append(f.policy.Routes, economicConservationRoute{Hotkey: "0x" + strings.Repeat("22", 32), Coldkey: "0x" + strings.Repeat("34", 32), Kind: "direct-head"})
	}
	f.policy.Vault.BatchBlocks = 1
	pool := protocol.ClaimProgressPool{ChainId: 964, Vault: strings.ToLower(f.vault.policy.Address), NoId: "1", Coldkey: f.vault.policy.Coldkeys[0]}
	// Publication time advances; one instance retains its original birth time.
	startedAt := f.now.Add(-time.Hour).Format(time.RFC3339Nano)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		sequence := f.claimReads.Add(1)
		if request.Method != http.MethodGet || request.URL.Query().Get("id") != "synthetic-claim" {
			http.Error(w, "source request differs", 400)
			return
		}
		observation := &protocol.ClaimObservation{Schema: protocol.ClaimObservationSchema, EvidenceKind: "signed-receipt", Authority: "configured-rpc-assertion", GenesisStatus: "unverified", Epoch: 1, ObservedAt: f.now.Format(time.RFC3339Nano), Pool: pool, ShareBps: 700, ProofStatus: "contract-accepted", BlockNumber: 11, BlockHash: f.vault.blocks[11].header.Hash().Hex(), TransactionHash: f.vault.blocks[11].transactions[0].Hash().Hex(), Relayer: strings.ToLower(f.vault.blocks[11].receipts[0]["from"].(string)), AcceptedAmountRao: "7", PaymentStatus: "deferred", UnpaidCreditRao: "12"}
		if f.claimFault.Load() {
			observation.AcceptedAmountRao = "8"
		}
		if f.claimPaymentUnknown.Load() {
			observation.PaymentStatus, observation.UnpaidCreditRao = "unknown", ""
		}
		value := protocol.ClaimProgress{Schema: protocol.ClaimProgressSchema, Member: "synthetic-claim", Status: "active", InstanceId: strings.Repeat("6", 32), StartedAt: startedAt, PublishedAt: f.now.Format(time.RFC3339Nano), Sequence: sequence, QueueSha256: strings.Repeat("7", 64), DeclaredPool: &pool, TotalEntries: 1, FinalizedEntries: 1, Entries: []protocol.ClaimProgressEntry{{Epoch: 1, QueueStatus: "finalized", ObservationStatus: "retained", DomainStatus: "match", Observation: observation}}}
		if err := value.Validate(); err != nil {
			http.Error(w, fmt.Sprint(err), 400)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)
	f.policy.Claims = []monitorClaimPolicy{{Role: "claim-conservation", Endpoint: server.URL + "/claim-progress", ExpectedMember: "synthetic-claim", ExpectedPool: pool, FreshnessSeconds: 60, Epochs: []monitorClaimEpochPolicy{{Epoch: 1, ShareBps: 700, AcceptBy: f.now.Add(-time.Minute).Format(time.RFC3339Nano)}}}}
	root := t.TempDir()
	protectFreshEconomicConservationTestRoot(t, root)
	f.path, f.checkpoint = filepath.Join(root, "policy.json"), filepath.Join(root, "checkpoint.json")
	f.writePolicy(t)
	return f
}

// Only the fresh test-owned root is provisioned. Runtime constructors still
// refuse an unprotected or replaced root, regardless of the process umask.
func protectFreshEconomicConservationTestRoot(t *testing.T, root string) {
	t.Helper()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
}

func (self *economicConservationFixture) writePolicy(t *testing.T) {
	t.Helper()
	if err := self.policy.validate(); err != nil {
		t.Fatal("complete synthetic policy", err)
	}
	raw, err := json.Marshal(self.policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(self.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func (self *economicConservationFixture) args(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(self.path)
	if err != nil {
		t.Fatal(err)
	}
	return []string{"observe-economic-conservation", "--policy", self.path, "--policy-sha256", monitorReadDigest(raw), "--native-rpc", self.native.client.url, "--evm-rpc", self.vault.url, "--checkpoint", self.checkpoint}
}

func (self *economicConservationFixture) run(t *testing.T, hooks monitorServiceHooks) (economicConservationSummary, int, string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, t.Context(), self.args(t), &output, &diagnostic, func() time.Time { return self.now }, hooks)
	var summary economicConservationSummary
	if output.Len() != 0 {
		if err := decodePlanJson(output.Bytes(), &summary); err != nil {
			t.Fatal(err)
		}
	}
	return summary, code, diagnostic.String()
}

func (self *economicConservationFixture) state(t *testing.T) economicConservationState {
	t.Helper()
	raw, err := os.ReadFile(self.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var value economicConservationState
	if err := decodePlanJson(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := value.validate(t.Context(), self.policy); err != nil {
		t.Fatal("retained actual checkpoint", err)
	}
	return value
}

func economicConservationTestWait(ctx context.Context, _ string, _ time.Duration) error {
	return context.DeadlineExceeded
}
