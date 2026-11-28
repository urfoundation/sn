// Settlement v2 runs end to end on the in-process EVM: the reviewed vault2,
// its escrow registration against fake neuron, metagraph and staking
// precompiles, the STSettlement2 implementation, the initialized proxy and
// the coordinator binding, each completed only after its readback.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ss58"
)

// State is retained at every inclusion. The fake precompiles register the
// escrow hotkey at UID 7 under vault2's mapped coldkey and report no stake
// and no collateral, and the v1 coordinator address carries code, as
// initialize needs.
func newEvmV2ChainFixture(t *testing.T) *evmCreateFixture {
	t.Helper()
	return newEvmV2ChainFixtureOf(t, newEvmV2Fixture(t))
}

// The chain fixture over a given v2 fixture's approval.
func newEvmV2ChainFixtureOf(t *testing.T, f *evmCreateFixture) *evmCreateFixture {
	t.Helper()
	f.history = &evmCreateHistory{receipts: map[string]map[string]any{}, transactions: map[string]*types.Transaction{}, blocks: map[string]types.Transactions{}, states: map[string]*state.StateDB{}, storage: map[string]map[string]string{}}
	sender := f.config.Plan.Actions[0].Sender
	f.installRegistrationPrecompiles(7, evmV2TestHotkey, ss58.EvmMirrorPubkey(crypto.CreateAddress(sender, 0)))
	staking, collateralJump := evmTestPrecompileDispatch(nil, "getMinerCollateral(uint16,bytes32,bytes32)")
	staking, stakeJump := evmTestPrecompileDispatch(staking, "getStake(bytes32,bytes32,uint256)")
	staking = append(staking, 0x60, 0x00, 0x60, 0x00, 0xfd)
	// Zeroed memory answers five collateral words or one stake word.
	for _, branch := range []struct {
		jump int
		size byte
	}{{jump: collateralJump, size: 0xa0}, {jump: stakeJump, size: 0x20}} {
		staking[branch.jump] = byte(len(staking))
		staking = append(staking, 0x5b, 0x60, branch.size, 0x60, 0x00, 0xf3)
	}
	f.state.SetCode(common.HexToAddress(contractStakingPrecompile), staking, tracing.CodeChangeUnspecified)
	f.state.SetCode(evmV2TestV1, []byte{0x00}, tracing.CodeChangeUnspecified)
	f.state.SetBalance(sender, uint256.NewInt(10_000_000_000_000_000), tracing.BalanceChangeUnspecified)
	f.refreshV2History()
	return f
}

// Retained historical states follow direct fixture state edits.
func (self *evmCreateFixture) refreshV2History() {
	for hash := range self.evmHeaders {
		self.history.states[hash] = self.state.Copy()
	}
}

// Back-to-back original transactions execute in order into one block, each
// with its own receipt, index and logs, and the native commitment lists them
// all, the way a chain includes two sends broadcast together.
func (self *evmCreateFixture) executeBlock(entries ...evmCreateFollow) error {
	next := uint64(0)
	var parentHash string
	for hash, header := range self.evmHeaders {
		if number := header.Number.Uint64(); number >= next {
			next = number + 1
			parentHash = hash
		}
	}
	self.vm.BlockNumber = new(big.Int).SetUint64(next)
	transactions, receipts, used := types.Transactions{}, []map[string]any{}, uint64(0)
	for index, entry := range entries {
		config := self.vm
		config.GasLimit, config.Value = entry.tx.Gas(), entry.tx.Value()
		self.state.SetTxContext(entry.tx.Hash(), index)
		receipt := map[string]any{"transactionHash": entry.tx.Hash().Hex(), "transactionIndex": fmt.Sprintf("0x%x", index), "status": "0x1", "effectiveGasPrice": "0x2"}
		var left uint64
		var err error
		if entry.tx.To() == nil {
			var code []byte
			var address common.Address
			code, address, left, err = runtime.Create(entry.tx.Data(), &config)
			if err == nil && (address != entry.plan.Address || !bytes.Equal(code, entry.plan.Runtime)) {
				err = errors.New("genuine EVM constructor differs from approved immutable projection")
			}
			receipt["contractAddress"] = strings.ToLower(address.Hex())
		} else {
			self.state.SetNonce(self.vm.Origin, entry.tx.Nonce()+1, tracing.NonceChangeUnspecified)
			_, left, err = runtime.Call(*entry.tx.To(), entry.tx.Data(), &config)
			receipt["contractAddress"], receipt["to"], receipt["from"] = nil, entry.tx.To().Hex(), self.vm.Origin.Hex()
		}
		if err != nil {
			return err
		}
		receipt["gasUsed"], used = fmt.Sprintf("0x%x", config.GasLimit-left), used+config.GasLimit-left
		transactions, receipts = append(transactions, entry.tx), append(receipts, receipt)
	}
	header := &types.Header{ParentHash: common.HexToHash(parentHash), UncleHash: types.EmptyUncleHash, Root: self.state.IntermediateRoot(true), TxHash: types.DeriveSha(transactions, trie.NewStackTrie(nil)), ReceiptHash: types.EmptyReceiptsHash, Difficulty: big.NewInt(0), Number: new(big.Int).SetUint64(next), GasLimit: 75_000_000, GasUsed: used, Time: 1700000001001}
	raw, err := rlp.EncodeToBytes(header)
	if err != nil {
		return err
	}
	hash := header.Hash().Hex()
	self.evmHeaders[hash], self.rawHeaders[hash] = header, "0x"+hex.EncodeToString(raw)
	hashes := []string{}
	for _, transaction := range transactions {
		hashes = append(hashes, transaction.Hash().Hex())
	}
	nativeHeader, nativeHash := evmTestNativeHeader(self.t, self.hashes[self.head], self.head+1, []string{mappingTestDigest(self.t, 1, hash, hashes)})
	self.headers[nativeHash], self.hashes[self.head+1], self.head = nativeHeader, nativeHash, self.head+1
	metadata, _, err := crv4.DecodeRuntimeMetadata("0x" + hex.EncodeToString(self.metadata))
	if err != nil {
		return err
	}
	arg := make([]byte, 32)
	binary.LittleEndian.PutUint64(arg, next)
	key, err := native.CreateStorageKey(metadata, "Ethereum", "BlockHash", arg)
	if err != nil {
		return err
	}
	self.storageKey = key.Hex()
	for index, receipt := range receipts {
		receipt["blockHash"], receipt["blockNumber"] = hash, fmt.Sprintf("0x%x", next)
		if transactions[index].To() != nil {
			logs := []map[string]any{}
			for _, log := range self.state.GetLogs(transactions[index].Hash(), next, header.Hash(), header.Time) {
				topics := []string{}
				for _, topic := range log.Topics {
					topics = append(topics, topic.Hex())
				}
				logs = append(logs, map[string]any{"address": log.Address.Hex(), "topics": topics, "data": "0x" + hex.EncodeToString(log.Data), "transactionHash": log.TxHash.Hex(), "blockHash": log.BlockHash.Hex(), "blockNumber": fmt.Sprintf("0x%x", log.BlockNumber), "transactionIndex": fmt.Sprintf("0x%x", log.TxIndex), "logIndex": fmt.Sprintf("0x%x", log.Index), "removed": log.Removed})
			}
			receipt["logs"] = logs
		}
		self.history.receipts[transactions[index].Hash().Hex()] = receipt
	}
	self.history.transactions[hash], self.history.blocks[hash] = transactions[0], transactions
	self.history.states[hash] = self.state.Copy()
	self.history.storage[self.storageKey] = map[string]string{nativeHash: hash}
	self.receipt = receipts[len(receipts)-1]
	return nil
}

// The deployer's original signature over one approved envelope, in its own
// pinned file, without changing the fixture's selected transaction.
func (self *evmCreateFixture) v2SignedFile(index int) (*types.Transaction, []byte, string, string) {
	self.t.Helper()
	private, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		self.t.Fatal(err)
	}
	action := self.config.Plan.Actions[index]
	unsigned, err := action.unsigned()
	if err != nil {
		self.t.Fatal(err)
	}
	tx, err := types.SignTx(unsigned, types.LatestSignerForChainID(big.NewInt(964)), private)
	if err != nil {
		self.t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		self.t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(self.configPath), action.Id+".signed.bin")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		self.t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return tx, raw, path, "sha256:" + hex.EncodeToString(digest[:])
}

// Select one approved action and sign it for the fixture's next execution.
func (self *evmCreateFixture) selectV2Action(id string) {
	self.t.Helper()
	plan, err := selectEvmCreatePlan(context.Background(), self.plan, id, self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	self.plan, self.receipt = plan, nil
	self.signSelectedAction()
}

// Offline preparation and import of the selected action's original bytes.
func (self *evmCreateFixture) prepareV2Action(id string, extra ...string) {
	self.t.Helper()
	if result, code, diagnostic := self.command("apply", "--action", id); code != 0 || result.Status != "signature-awaiting-import" || result.ExecutableAction != id {
		self.t.Fatalf("%s preparation: %+v %d %s", id, result, code, diagnostic)
	}
	args := append([]string{"--action", id, "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash}, extra...)
	if result, code, diagnostic := self.command("resume", args...); code != 0 || result.Status != "signed-custody-complete" {
		self.t.Fatalf("%s import: %+v %d %s", id, result, code, diagnostic)
	}
}

// Complete one action online: its receipt is retained only after readback.
func (self *evmCreateFixture) finishV2Action(id string) evmCreateResult {
	self.t.Helper()
	result, code, diagnostic := self.command("resume", "--action", id, "--online")
	plan, err := selectEvmCreatePlan(context.Background(), self.plan, id, self.configPath)
	if err != nil || code != 0 || result.Status != plan.completedStatus() || result.Receipt == nil || result.Receipt.Status != 1 || result.ReceiptObservation != "revalidated-online" {
		self.t.Fatalf("%s completion: %+v %d %s %v", id, result, code, diagnostic, err)
	}
	return result
}

// The ordinary path: prepare, import, one submission and completion.
func (self *evmCreateFixture) completeV2Action(id string) evmCreateResult {
	self.t.Helper()
	self.selectV2Action(id)
	self.prepareV2Action(id)
	if result, code, diagnostic := self.command("resume", "--action", id, "--online", "--submit"); code != 0 || result.Status != "submitted-awaiting-canonical-receipt" {
		self.t.Fatalf("%s submission: %+v %d %s", id, result, code, diagnostic)
	}
	return self.finishV2Action(id)
}

// A7: vault2's send broadcasts the escrow registration's original bytes at the
// next nonce in the same invocation; escrow2's journal then adopts them. The
// chain includes the two in one block, or the registration in the next one.
func (self *evmCreateFixture) completeV2BackToBack(sameBlock bool) (evmCreateResult, evmCreateResult) {
	self.t.Helper()
	self.selectV2Action("vault2-create")
	escrowPlan, err := selectEvmCreatePlan(context.Background(), self.plan, "escrow2-register", self.configPath)
	if err != nil {
		self.t.Fatal(err)
	}
	escrowTx, escrowRaw, escrowPath, escrowHash := self.v2SignedFile(1)
	self.follow = &evmCreateFollow{plan: escrowPlan, tx: escrowTx, raw: escrowRaw, sameBlock: sameBlock}
	self.prepareV2Action("vault2-create", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash)
	writes := len(self.writes)
	result, code, diagnostic := self.command("resume", "--action", "vault2-create", "--online", "--submit")
	if code != 0 || result.Status != "submitted-with-escrow-registration-awaiting-canonical-receipts" || result.Attempts != 1 || result.EscrowAttempts != 1 || result.EscrowTransactionHash != escrowTx.Hash().Hex() || len(self.writes) != writes+2 || hex.EncodeToString(self.writes[writes+1]) != hex.EncodeToString(escrowRaw) {
		self.t.Fatalf("back-to-back submission: %+v %d %s", result, code, diagnostic)
	}
	vault := self.finishV2Action("vault2-create")
	self.plan, self.signedPath, self.signedHash = escrowPlan, escrowPath, escrowHash
	self.prepareV2Action("escrow2-register")
	escrow := self.finishV2Action("escrow2-register")
	if escrow.Attempts != 0 || (escrow.Receipt.BlockHash == vault.Receipt.BlockHash) != sameBlock || !sameBlock && escrow.Receipt.BlockNumber != vault.Receipt.BlockNumber+1 {
		self.t.Fatalf("escrow registration did not follow vault2 in its block or the next: %+v %+v", vault.Receipt, escrow.Receipt)
	}
	return vault, escrow
}

// SV-8, A7, A3: the complete phase on the EVM, vault2 and its escrow
// registration broadcast back to back and included in one block, every
// completion read back, and the installation complete only with the binding.
// An offline reopen keeps the record; an online one reads everything back
// again.
func TestEvmV2PhaseInstallsBackToBackWithReadback(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	vault, escrow := f.completeV2BackToBack(true)
	sender := f.config.Plan.Actions[0].Sender
	if vault.Vault2Address != crypto.CreateAddress(sender, 0).Hex() || vault.InstallationComplete || vault.EscrowCollateral != nil || !slices.Equal(vault.RemainingActions, evmV2ActionIds()[1:]) {
		t.Fatalf("vault2 completion: %+v", vault)
	}
	if escrow.Receipt.EscrowUid != 7 || escrow.Receipt.RegistrationHash == "" || escrow.EscrowCollateral == nil || *escrow.EscrowCollateral != (contractEscrowCollateral{DrainRatio: "0"}) {
		t.Fatalf("escrow completion: %+v %+v", escrow, escrow.Receipt)
	}
	f.completeV2Action("settlement2-create")
	proxy := f.completeV2Action("proxy2-create")
	link := f.completeV2Action("vault2-link")
	if proxy.Settlement2Address != crypto.CreateAddress(sender, 3).Hex() || link.Settlement2ImplementationAddress != crypto.CreateAddress(sender, 2).Hex() || !link.InstallationComplete || link.ActivationReady || len(link.RemainingActions) != 0 || link.Receipt.VaultBindingHash == "" || link.EscrowCollateral == nil || link.VaultBinding == nil || link.VaultBinding.Coordinator.Hex() != proxy.Settlement2Address {
		t.Fatalf("installation completion: %+v", link)
	}
	counts := len(f.counts)
	offline, code, diagnostic := f.command("resume", "--action", "vault2-link")
	if code != 0 || offline.Status != "vault2-coordinator-bound" || offline.ReceiptObservation != "retained" || !offline.InstallationComplete || *offline.Receipt != *link.Receipt || len(f.counts) != counts {
		t.Fatalf("offline reopen: %+v %d %s", offline, code, diagnostic)
	}
	f.finishV2Action("vault2-link")
	raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json"))
	var record evmActionRecord
	if err != nil || json.Unmarshal(raw, &record) != nil || record.EscrowAttempts != 1 || record.EscrowTransactionHash != escrow.TransactionHash || record.Attempts != 1 {
		t.Fatalf("vault2 journal lost its back-to-back escrow custody: %+v %v", record, err)
	}
}

// SV-8, A7: a back-to-back registration that the chain includes in the block
// after vault2's completes both actions with the same readback.
func TestEvmV2BackToBackAcrossBlocksCompletes(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	if vault, escrow := f.completeV2BackToBack(false); vault.EscrowCollateral != nil || escrow.EscrowCollateral == nil || escrow.Receipt.EscrowUid != 7 {
		t.Fatalf("back-to-back completions across blocks: %+v %+v", vault, escrow)
	}
}

// SV-8: without the back-to-back bytes, the escrow registration is admitted
// and sent by its own journal once vault2 is complete.
func TestEvmV2PhaseInstallsSequentially(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	for _, id := range evmV2ActionIds() {
		result := f.completeV2Action(id)
		if result.InstallationComplete != (id == "vault2-link") {
			t.Fatalf("%s installation flag: %+v", id, result)
		}
	}
}

// A second valid signature over the same approved envelope: a random nonce
// instead of RFC 6979, normalized to low s, with its recovered parity.
func evmV2AlternateSignature(t *testing.T, original *types.Transaction) []byte {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		t.Fatal(err)
	}
	signer := types.LatestSignerForChainID(big.NewInt(964))
	hash := signer.Hash(original)
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	order := crypto.S256().Params().N
	if s.Cmp(new(big.Int).Rsh(order, 1)) > 0 {
		s.Sub(order, s)
	}
	signature := append(append(common.LeftPadBytes(r.Bytes(), 32), common.LeftPadBytes(s.Bytes(), 32)...), 0)
	for parity := byte(0); parity < 2; parity++ {
		signature[64] = parity
		public, err := crypto.SigToPub(hash[:], signature)
		if err == nil && crypto.PubkeyToAddress(*public) == crypto.PubkeyToAddress(key.PublicKey) {
			unsigned := types.NewTx(&types.DynamicFeeTx{ChainID: original.ChainId(), Nonce: original.Nonce(), To: original.To(), Data: original.Data(), Value: original.Value(), Gas: original.Gas(), GasFeeCap: original.GasFeeCap(), GasTipCap: original.GasTipCap(), AccessList: types.AccessList{}})
			signed, err := unsigned.WithSignature(signer, signature)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := signed.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}
	}
	t.Fatal("alternate signature has no recoverable parity")
	return nil
}

// Write public bytes to their own pinned file.
func (self *evmCreateFixture) v2PinnedFile(name string, raw []byte) (string, string) {
	self.t.Helper()
	path := filepath.Join(filepath.Dir(self.configPath), name)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		self.t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return path, "sha256:" + hex.EncodeToString(digest[:])
}

// SV-8, A7: vault2's journal accepts the escrow registration's bytes only
// after its own and keeps the first ones; escrow2's journal must adopt exactly
// the bytes broadcast back to back.
func TestEvmV2EscrowFollowKeepsExactBytes(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	f.selectV2Action("vault2-create")
	escrowTx, escrowRaw, escrowPath, escrowHash := f.v2SignedFile(1)
	if result, code, diagnostic := f.command("apply", "--action", "vault2-create"); code != 0 {
		t.Fatalf("vault2 preparation: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash); code != 1 || !strings.Contains(diagnostic, "only vault2's signed custody accepts its back-to-back escrow registration") {
		t.Fatalf("escrow bytes preceded vault2's own: %d %s", code, diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--signed-transaction", f.signedPath, "--signed-transaction-hash", f.signedHash, "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash); code != 0 {
		t.Fatalf("vault2 import with escrow bytes: %d %s", code, diagnostic)
	}
	journal := filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json")
	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash); code != 0 {
		t.Fatalf("same escrow bytes were not idempotent: %d %s", code, diagnostic)
	}
	if after, err := os.ReadFile(journal); err != nil || string(after) != string(before) {
		t.Fatalf("idempotent escrow import rewrote custody: %v", err)
	}
	alternatePath, alternateHash := f.v2PinnedFile("escrow-alternate.signed.bin", evmV2AlternateSignature(t, escrowTx))
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--escrow-signed-transaction", alternatePath, "--escrow-signed-transaction-hash", alternateHash); code != 1 || !strings.Contains(diagnostic, "vault2 custody already owns different escrow registration bytes") {
		t.Fatalf("vault2 custody replaced its escrow bytes: %d %s", code, diagnostic)
	}
	for _, args := range [][]string{{"--action", "escrow2-register", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash}, {"--action", "vault2-create", "--escrow-signed-transaction", escrowPath}} {
		if _, code, diagnostic := f.command("resume", args...); code != 2 || !strings.Contains(diagnostic, "only resume accepts pinned signed bytes") {
			t.Fatalf("escrow bytes outside vault2 resume: %v %d %s", args, code, diagnostic)
		}
	}
	escrowPlan, err := selectEvmCreatePlan(context.Background(), f.plan, "escrow2-register", f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	f.follow = &evmCreateFollow{plan: escrowPlan, tx: escrowTx, raw: escrowRaw}
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	f.finishV2Action("vault2-create")
	f.plan = escrowPlan
	if result, code, diagnostic := f.command("apply", "--action", "escrow2-register"); code != 0 {
		t.Fatalf("escrow2 preparation: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("resume", "--action", "escrow2-register", "--signed-transaction", alternatePath, "--signed-transaction-hash", alternateHash); code != 1 || !strings.Contains(diagnostic, "escrow registration custody must keep the bytes sent back to back with vault2") {
		t.Fatalf("escrow2 adopted different bytes: %d %s", code, diagnostic)
	}
	f.signedPath, f.signedHash = escrowPath, escrowHash
	f.prepareV2ActionImportOnly("escrow2-register")
	f.finishV2Action("escrow2-register")
}

// Import alone, for a journal that apply already opened.
func (self *evmCreateFixture) prepareV2ActionImportOnly(id string) {
	self.t.Helper()
	if result, code, diagnostic := self.command("resume", "--action", id, "--signed-transaction", self.signedPath, "--signed-transaction-hash", self.signedHash); code != 0 || result.Status != "signed-custody-complete" {
		self.t.Fatalf("%s import: %+v %d %s", id, result, code, diagnostic)
	}
}

// The deployer's signature over an envelope a command printed, as the
// external signer returns it: canonical binary EIP-1559 bytes.
func evmV2SignPrinted(t *testing.T, unsigned string) []byte {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("17", 32))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := hex.DecodeString(strings.TrimPrefix(unsigned, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope types.Transaction
	if err := envelope.UnmarshalBinary(encoded); err != nil {
		t.Fatal(err)
	}
	signed, err := types.SignTx(&envelope, types.LatestSignerForChainID(big.NewInt(964)), key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// SV-8, A7: vault2's apply prints the escrow registration's envelope, the
// approved action 1, before that journal opens. The deployer signs both
// printed envelopes before any send, vault2's journal sends them back to
// back, and escrow2's journal later prints byte-identical bytes and records
// and accepts the same signature.
func TestEvmV2Vault2ApplyPrintsEscrowEnvelope(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	f.selectV2Action("vault2-create")
	escrowPlan, err := selectEvmCreatePlan(context.Background(), f.plan, "escrow2-register", f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	escrowTx, escrowRaw, _, _ := f.v2SignedFile(1)
	escrowJournal := filepath.Join(f.config.Plan.RunDirectory, "escrow2-register.json")
	vault, code, diagnostic := f.command("apply", "--action", "vault2-create")
	if _, err := os.Lstat(escrowJournal); code != 0 || vault.Status != "signature-awaiting-import" || vault.EscrowSigningDigest != types.LatestSignerForChainID(big.NewInt(964)).Hash(escrowTx).Hex() || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("vault2 preparation without the escrow envelope, or with escrow2's journal open: %+v %d %s %v", vault, code, diagnostic, err)
	}
	// The signatures over the printed envelopes are those over the approved actions.
	vaultSigned, escrowSigned := evmV2SignPrinted(t, vault.UnsignedTransaction), evmV2SignPrinted(t, vault.EscrowUnsignedTransaction)
	if !bytes.Equal(vaultSigned, f.raw) || !bytes.Equal(escrowSigned, escrowRaw) {
		t.Fatal("a printed envelope differs from its approved action")
	}
	vaultPath, vaultHash := f.v2PinnedFile("vault2-printed.signed.bin", vaultSigned)
	escrowPath, escrowHash := f.v2PinnedFile("escrow2-printed.signed.bin", escrowSigned)
	imported, code, diagnostic := f.command("resume", "--action", "vault2-create", "--signed-transaction", vaultPath, "--signed-transaction-hash", vaultHash, "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash)
	if code != 0 || imported.Status != "signed-custody-complete" || imported.EscrowUnsignedTransaction != vault.EscrowUnsignedTransaction || imported.EscrowTransactionHash != escrowTx.Hash().Hex() {
		t.Fatalf("vault2 import of both printed signatures: %+v %d %s", imported, code, diagnostic)
	}
	f.follow = &evmCreateFollow{plan: escrowPlan, tx: escrowTx, raw: escrowRaw, sameBlock: true}
	if result, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online", "--submit"); code != 0 || result.Status != "submitted-with-escrow-registration-awaiting-canonical-receipts" {
		t.Fatalf("back-to-back send: %+v %d %s", result, code, diagnostic)
	}
	f.finishV2Action("vault2-create")
	f.plan, f.signedPath, f.signedHash = escrowPlan, escrowPath, escrowHash
	escrow, code, diagnostic := f.command("apply", "--action", "escrow2-register")
	if code != 0 || escrow.Status != "signature-awaiting-import" || escrow.UnsignedTransaction != vault.EscrowUnsignedTransaction || escrow.SigningDigest != vault.EscrowSigningDigest || escrow.EscrowUnsignedTransaction != "" || escrow.EscrowSigningDigest != "" {
		t.Fatalf("escrow2's envelope differs from the one vault2 printed: %+v %d %s", escrow, code, diagnostic)
	}
	f.prepareV2ActionImportOnly("escrow2-register")
	f.finishV2Action("escrow2-register")
	raw, err := os.ReadFile(escrowJournal)
	var record evmActionRecord
	if err != nil || json.Unmarshal(raw, &record) != nil || record.Signed != "0x"+hex.EncodeToString(escrowSigned) || record.TransactionHash != escrowTx.Hash().Hex() {
		t.Fatalf("escrow2's journal does not hold the signature over the printed envelope: %+v %v", record, err)
	}
}

// SV-8, A7: a vault2 whose escrow hotkey already has a UID is never created,
// and a registration found registered after vault2 is never sent.
func TestEvmV2RegisteredEscrowHotkeyRefusesSends(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	f.selectV2Action("vault2-create")
	f.prepareV2Action("vault2-create")
	neuron := common.HexToAddress("0x804")
	writes := len(f.writes)
	f.override = func(method string, params []any, result any) any {
		if method == "eth_call" && len(params) != 0 && common.HexToAddress(params[0].(map[string]any)["to"].(string)) == neuron {
			return mappingFixtureRpcError{code: -32602}
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "eth_call") || len(f.writes) != writes {
		t.Fatalf("unresolved escrow hotkey admitted vault2: %d %s", code, diagnostic)
	}
	f.override = nil
	f.state.SetState(neuron, common.Hash{}, common.BigToHash(big.NewInt(1)))
	f.refreshV2History()
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "vault2 escrow hotkey is already registered or its absence is unresolved") || len(f.writes) != writes {
		t.Fatalf("vault2 created over a registered escrow hotkey: %d %s", code, diagnostic)
	}
	f.state.SetState(neuron, common.Hash{}, common.Hash{})
	f.refreshV2History()
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	f.finishV2Action("vault2-create")
	f.selectV2Action("escrow2-register")
	f.prepareV2Action("escrow2-register")
	f.state.SetState(neuron, common.Hash{}, common.BigToHash(big.NewInt(1)))
	writes = len(f.writes)
	if _, code, diagnostic := f.command("resume", "--action", "escrow2-register", "--online", "--submit"); code != 1 || !strings.Contains(diagnostic, "escrow hotkey is already registered or its absence is unresolved") || len(f.writes) != writes {
		t.Fatalf("escrow registration sent for a taken hotkey: %d %s", code, diagnostic)
	}
}

// SV-8, A7: the back-to-back send counts against the one graph allowance and
// is durably counted before its write, which can be uncertain.
func TestEvmV2EscrowFollowAllowanceAndUncertainWrite(t *testing.T) {
	for _, fault := range []string{"exhausted", "uncertain"} {
		f := newEvmV2ChainFixture(t)
		if fault == "exhausted" {
			f.config.Plan.MaximumAttempts = 1
			f.publishConfig()
		}
		f.selectV2Action("vault2-create")
		escrowPlan, err := selectEvmCreatePlan(context.Background(), f.plan, "escrow2-register", f.configPath)
		if err != nil {
			t.Fatal(err)
		}
		escrowTx, escrowRaw, escrowPath, escrowHash := f.v2SignedFile(1)
		f.follow = &evmCreateFollow{plan: escrowPlan, tx: escrowTx, raw: escrowRaw}
		f.prepareV2Action("vault2-create", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash)
		if fault == "uncertain" {
			f.override = func(method string, params []any, result any) any {
				if method == "eth_sendRawTransaction" && params[0] == "0x"+hex.EncodeToString(escrowRaw) {
					return mappingFixtureRpcError{code: -32603}
				}
				return result
			}
		}
		result, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online", "--submit")
		raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json"))
		var record evmActionRecord
		if err != nil || json.Unmarshal(raw, &record) != nil {
			t.Fatal(err)
		}
		if fault == "exhausted" && (code != 0 || result.Status != "escrow-registration-allowance-exhausted" || record.EscrowAttempts != 0 || record.Attempts != 1) {
			t.Fatalf("escrow follow exceeded the allowance: %+v %d %s %+v", result, code, diagnostic, record)
		}
		if fault == "uncertain" && (code != 1 || !strings.Contains(diagnostic, "contract phase stopped") || record.EscrowAttempts != 1 || record.Attempts != 1) {
			t.Fatalf("uncertain escrow follow lost its durable attempt: %d %s %+v", code, diagnostic, record)
		}
	}
}

// SV-8: reverted outcomes consume their nonce and never complete; a later
// action needs its predecessor's successful readback.
func TestEvmV2RevertedActionsRetainConsumedNonce(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	f.selectV2Action("vault2-create")
	f.prepareV2Action("vault2-create")
	f.vm.GasLimit, f.gasFailure = 300_000, true
	if _, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	result, code, diagnostic := f.command("resume", "--action", "vault2-create", "--online")
	if code != 0 || result.Status != "create-reverted-nonce-consumed" || result.Receipt == nil || result.Receipt.Status != 0 || result.InstallationComplete {
		t.Fatalf("reverted vault2: %+v %d %s", result, code, diagnostic)
	}
	if _, code, diagnostic := f.command("apply", "--action", "escrow2-register"); code != 3 || !strings.Contains(diagnostic, "action requires the exact retained successful vault2 postconditions") {
		t.Fatalf("escrow registration opened after reverted vault2: %d %s", code, diagnostic)
	}
	g := newEvmV2ChainFixture(t)
	g.completeV2Action("vault2-create")
	g.selectV2Action("escrow2-register")
	g.prepareV2Action("escrow2-register")
	// Below the guard's first write: registerEscrow runs out of gas setting
	// the reentrancy flag beside the still zero coordinator.
	g.vm.GasLimit, g.gasFailure = 20_000, true
	if _, code, diagnostic := g.command("resume", "--action", "escrow2-register", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	if result, code, diagnostic := g.command("resume", "--action", "escrow2-register", "--online"); code != 0 || result.Status != "escrow-registration-reverted-nonce-consumed" || result.Receipt.Status != 0 || result.EscrowCollateral != nil {
		t.Fatalf("reverted escrow registration: %+v %d %s", result, code, diagnostic)
	}
}

// SV-8: the binding completes only after reading back vault2, the proxy and
// the implementation at its own canonical inclusion. Each kind of mismatch
// there refuses completion and retains no receipt.
func TestEvmV2CompletionRefusesReadbackMismatch(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	for _, id := range evmV2ActionIds()[:4] {
		f.completeV2Action(id)
	}
	f.selectV2Action("vault2-link")
	f.prepareV2Action("vault2-link")
	if _, code, diagnostic := f.command("resume", "--action", "vault2-link", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	block := f.receipt["blockHash"].(string)
	graph := f.v2Graph()
	vault, proxy, implementation := graph[4], graph[3], graph[2]
	collateral := vault.Getters[len(vault.Getters)-1]
	for _, fault := range []struct {
		method   string
		address  common.Address
		item     string
		expected string
	}{
		{"eth_getCode", vault.Address, "", "created contract runtime differs from exact patched release"},
		{"eth_call", vault.Address, vault.Getters[6].Data, "created contract constructor getter differs"},
		{"eth_call", common.HexToAddress(collateral.To), collateral.Data, "created contract constructor getter differs"},
		{"eth_getStorageAt", vault.Address, vault.Storage[0].Slot, "created implementation constructor storage differs"},
		{"eth_getCode", proxy.Address, "", "initialized settlement2 proxy readback runtime differs"},
		{"eth_call", proxy.Address, proxy.Getters[2].Data, "initialized settlement2 proxy readback getter " + proxy.Getters[2].Data + " differs"},
		{"eth_getStorageAt", proxy.Address, contractImplementationSlot, "initialized settlement2 proxy readback storage slot " + contractImplementationSlot + " differs"},
		{"eth_getCode", implementation.Address, "", "settlement2 implementation readback runtime differs"},
		{"eth_call", implementation.Address, implementation.Getters[7].Data, "settlement2 implementation readback getter " + implementation.Getters[7].Data + " differs"},
		{"eth_getStorageAt", implementation.Address, contractInitializableSlot, "settlement2 implementation readback storage slot " + contractInitializableSlot + " differs"},
	} {
		f.override = func(method string, params []any, result any) any {
			if method != fault.method || len(params) == 0 {
				return result
			}
			if selector, ok := params[len(params)-1].(map[string]any); !ok || selector["blockHash"] != block {
				return result
			}
			address, item := params[0], ""
			if method == "eth_call" {
				address, item = params[0].(map[string]any)["to"], params[0].(map[string]any)["data"].(string)
			}
			if method == "eth_getStorageAt" {
				item = params[1].(string)
			}
			if common.HexToAddress(address.(string)) != fault.address || item != fault.item {
				return result
			}
			if method == "eth_getCode" {
				return "0x"
			}
			return evmV2ReadbackFlip(result.(string))
		}
		if _, code, diagnostic := f.command("resume", "--action", "vault2-link", "--online"); code != 1 || !strings.Contains(diagnostic, fault.expected) {
			t.Fatalf("%s %s %s mismatch completed the binding: %d %s", fault.method, fault.address.Hex(), fault.item, code, diagnostic)
		}
		raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "vault2-link.json"))
		var record evmActionRecord
		if err != nil || json.Unmarshal(raw, &record) != nil || record.Receipt != nil {
			t.Fatalf("refused readback retained a receipt: %v", err)
		}
	}
	f.override = nil
	if result := f.finishV2Action("vault2-link"); !result.InstallationComplete {
		t.Fatalf("exact readback did not complete installation: %+v", result)
	}
}

// SV-8, A7: only vault2's signed journal carries escrow registration bytes,
// exactly the approved second envelope, within the one allowance.
func TestEvmV2EscrowFollowRecordRules(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	f.selectV2Action("vault2-create")
	escrowTx, _, escrowPath, escrowHash := f.v2SignedFile(1)
	f.prepareV2Action("vault2-create", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash)
	raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json"))
	var retained evmActionRecord
	if err != nil || json.Unmarshal(raw, &retained) != nil || retained.validateForAction(f.config, 0) != nil || retained.EscrowTransactionHash != escrowTx.Hash().Hex() {
		t.Fatalf("retained vault2 custody: %+v %v", retained, err)
	}
	holder, signature := "only vault2's signed custody holds its back-to-back escrow registration", "retained escrow registration signature differs"
	for _, fault := range []struct {
		name, expected string
		index          int
		edit           func(*evmActionRecord)
	}{
		{"unsigned", holder, 0, func(record *evmActionRecord) { record.Signed, record.TransactionHash = "", "" }},
		{"escrow-journal", holder, 1, func(record *evmActionRecord) {
			record.Schema, record.PredecessorHash = evmV2Actions[1].schema, "sha256:"+strings.Repeat("ab", 32)
		}},
		{"malformed", "root receipt hex", 0, func(record *evmActionRecord) { record.EscrowSigned = "0xzz" }},
		{"vault2-bytes", signature, 0, func(record *evmActionRecord) { record.EscrowSigned = record.Signed }},
		{"hash", signature, 0, func(record *evmActionRecord) { record.EscrowTransactionHash = common.Hash{1}.Hex() }},
		{"allowance", "EVM journal identity, continuity or allowance differs", 0, func(record *evmActionRecord) { record.EscrowAttempts = f.config.Plan.MaximumAttempts }},
	} {
		record := retained
		fault.edit(&record)
		if fault.name == "allowance" {
			record.Attempts = 1
		}
		record.ContentHash = ""
		record.ContentHash = rootObjectHash(record)
		if err := record.validateForAction(f.config, fault.index); err == nil || !strings.Contains(err.Error(), fault.expected) {
			t.Fatalf("%s escrow custody admitted: %v", fault.name, err)
		}
	}
	original := newEvmCreateFixture(t)
	if result, code, diagnostic := original.command("apply"); code != 0 {
		t.Fatalf("original reserve preparation: %+v %d %s", result, code, diagnostic)
	}
	reserveRaw, err := os.ReadFile(filepath.Join(original.config.Plan.RunDirectory, evmCreateStateFile))
	var reserve evmActionRecord
	if err != nil || json.Unmarshal(reserveRaw, &reserve) != nil {
		t.Fatal(err)
	}
	reserve.EscrowAttempts = 1
	reserve.ContentHash = ""
	reserve.ContentHash = rootObjectHash(reserve)
	if err := reserve.validateForAction(original.config, 0); err == nil || !strings.Contains(err.Error(), holder) {
		t.Fatalf("original journal carried escrow custody: %v", err)
	}
}

// SV-8, A7: the escrow write needs the approved route scope, the vault2
// selection, a retained attempt and a valid journal; an owner outside vault2
// or invalid bytes never retain escrow custody.
func TestEvmV2EscrowFollowWriteScope(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	f.selectV2Action("vault2-create")
	_, escrowRaw, escrowPath, escrowHash := f.v2SignedFile(1)
	f.prepareV2Action("vault2-create", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash)
	raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "vault2-create.json"))
	var record evmActionRecord
	if err != nil || json.Unmarshal(raw, &record) != nil {
		t.Fatal(err)
	}
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	escrowPlan := *f.v2Graph()[1]
	attempted := record
	attempted.EscrowAttempts = 1
	attempted.ContentHash = ""
	attempted.ContentHash = rootObjectHash(attempted)
	scope := "escrow registration write lacks vault2 scope or a retained attempt"
	for _, fault := range []struct {
		name    string
		chain   *evmOwnedChain
		plan    evmCreatePlan
		record  evmActionRecord
		message string
	}{
		{"route", &evmOwnedChain{client: chain.client, configHash: "sha256:" + strings.Repeat("00", 32)}, f.plan, attempted, scope},
		{"action", chain, escrowPlan, attempted, scope},
		{"attempt", chain, f.plan, record, scope},
		{"journal", chain, f.plan, func() evmActionRecord { changed := attempted; changed.Attempts = 99; return changed }(), "EVM journal identity, continuity or allowance differs"},
	} {
		if err := fault.chain.submitEscrow(t.Context(), fault.plan, fault.record); err == nil || !strings.Contains(err.Error(), fault.message) {
			t.Fatalf("%s escrow write admitted: %v", fault.name, err)
		}
	}
	if len(f.writes) != 0 {
		t.Fatal("refused escrow write reached the route")
	}
	store, err := openEvmV2ActionStore(f.plan, nil, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newEvmSelectedCreateOwner(f.plan, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.advanceWithEscrow(t.Context(), nil, escrowRaw[:len(escrowRaw)-1], false, false); err == nil {
		t.Fatal("malformed escrow bytes were retained")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	original := newEvmCreateFixture(t)
	original.prepareSigned()
	reserveStore, err := openEvmActionStore(original.config, false, nil, original.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reserveStore.close() })
	reserveOwner, err := newEvmCreateOwner(original.plan, reserveStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reserveOwner.advanceWithEscrow(t.Context(), nil, escrowRaw, false, false); err == nil || !strings.Contains(err.Error(), "only vault2's signed custody accepts its back-to-back escrow registration") {
		t.Fatalf("original reserve owner retained escrow bytes: %v", err)
	}
}

// SV-8, A7: an interrupted publication of the escrow attempt poisons the owner
// before the registration's write; vault2's own send stays the only write.
func TestEvmV2EscrowFollowAmbiguousAttemptStopsBeforeWrite(t *testing.T) {
	f := newEvmV2ChainFixture(t)
	f.selectV2Action("vault2-create")
	escrowPlan, err := selectEvmCreatePlan(context.Background(), f.plan, "escrow2-register", f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	escrowTx, escrowRaw, escrowPath, escrowHash := f.v2SignedFile(1)
	f.follow = &evmCreateFollow{plan: escrowPlan, tx: escrowTx, raw: escrowRaw}
	f.prepareV2Action("vault2-create", "--escrow-signed-transaction", escrowPath, "--escrow-signed-transaction-hash", escrowHash)
	store, err := openEvmV2ActionStore(f.plan, nil, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmSelectedCreateOwner(f.plan, store, nil, chain)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	store.syncDirectory = func(*os.File) error {
		count++
		if count == 3 {
			return errors.New("synthetic escrow attempt directory-sync interruption")
		}
		return nil
	}
	writes := len(f.writes)
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != writes+1 || f.follow == nil {
		t.Fatalf("escrow attempt sync failure was acknowledged or sent: %v %d", err, len(f.writes)-writes)
	}
	if _, err := owner.advance(context.Background(), nil, true, true); err == nil || len(f.writes) != writes+1 {
		t.Fatal("poisoned vault2 owner sent or reused custody")
	}
}
