//go:build linux || darwin

// The auditor sends approvals, challenges and demands to the settlement v2
// contracts (VERSION2-FINAL.md §9, §9.3). It decides nothing: the ledger audit
// chooses the action. A durable record per (noId, seq, action, rootIndex,
// index) keeps the signed transaction from before its first broadcast, so a
// restart reconciles that exact transaction and never signs a second one.
package validator

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urfoundation/sn/v2026/stabi"
)

var (
	// The action's transaction is signed but not finalized. Send the same
	// action again later; it is never signed twice.
	ErrSettlementV2SendPending = errors.New("settlement v2 auditor transaction is not finalized yet")
	// The root the audit read is no longer the pool's pending root.
	ErrSettlementV2RootNotPending = errors.New("settlement v2 root is no longer pending")
	// An approval can no longer be included before end(e+1).
	ErrSettlementV2ApprovalClosed = errors.New("settlement v2 approval deadline end(e+1) has passed")
	// A demand can no longer be included before commit + CHALLENGE_BLOCKS.
	ErrSettlementV2ChallengeWindowClosed = errors.New("settlement v2 challenge window has closed")
	// The challenged root was finalized, so no challenge can delete it.
	ErrSettlementV2RootFinalized = errors.New("settlement v2 challenged root was finalized")
	// The challenge transaction answered an open demand: its opening showed
	// no fault, and the root is still pending.
	ErrSettlementV2ChallengeAnswered = errors.New("settlement v2 challenge answered a demand instead of deleting the root")
)

const settlementV2SendSchema = "urnetwork-settlement-v2-send-v1"

// vault2's DemandState None: the index of the pending root was never demanded.
const settlementV2DemandNone uint8 = 0

// SettlementV2Sender sends the auditor's transactions through the validator's
// EVM submission: an eth_call preflight, an estimated gas limit, the pending
// nonce, the suggested gas price, and a wait for a finalized receipt. Sends
// are serialized, so one key never signs two transactions at once.
type SettlementV2Sender struct {
	lock      sync.Mutex
	chain     *ChainClient
	addresses SettlementV2Addresses
	rpcs      []string
	chainID   *big.Int
	key       *ecdsa.PrivateKey
	auditor   common.Address
	directory string
}

// The durable record of one action. Transaction is the exact signed RLP,
// written before its first broadcast.
type settlementV2SendRecord struct {
	Schema      string `json:"schema"`
	NoID        uint64 `json:"no_id"`
	Seq         uint64 `json:"seq"`
	Action      string `json:"action"`
	RootIndex   uint64 `json:"root_index"`
	Index       uint64 `json:"index"`
	Transaction string `json:"transaction"`
	Finalized   bool   `json:"finalized"`
}

// One action against one pending root. evaluate reads chain state at the
// finalized head: done means nothing is left to send; landed reports that the
// action's own transaction is final and succeeded. A preflight that reverts
// with the custom error done means another sender got there first (SPEC A6).
type settlementV2Send struct {
	noID      uint64
	seq       uint64
	action    string
	rootIndex uint64
	index     uint64
	to        common.Address
	calldata  []byte
	value     *big.Int
	done      [4]byte
	evaluate  func(ctx context.Context, landed bool) (done bool, err error)
}

// NewSettlementV2Sender signs with the key of --ledger_auditor_key_file and
// keeps its records under stateDir/settlement-v2-sends.
func NewSettlementV2Sender(chain *ChainClient, rpcs []string, chainID uint64, addresses SettlementV2Addresses, keyFile, stateDir string) (*SettlementV2Sender, error) {
	if chain == nil || len(rpcs) == 0 || chainID == 0 || addresses.Settlement == (common.Address{}) || addresses.Vault == (common.Address{}) || !filepath.IsAbs(stateDir) {
		return nil, errors.New("settlement v2 sender needs an EVM chain, RPC endpoints, a chain id, both v2 addresses and an absolute state directory")
	}
	key, err := onchain.LoadKeyFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("--ledger_auditor_key_file: %w", err)
	}
	directory := filepath.Join(stateDir, "settlement-v2-sends")
	if err := ensurePrivateStateDir(directory); err != nil {
		return nil, err
	}
	return &SettlementV2Sender{chain: chain, addresses: addresses, rpcs: slices.Clone(rpcs), chainID: new(big.Int).SetUint64(chainID), key: key, auditor: crypto.PubkeyToAddress(key.PublicKey), directory: directory}, nil
}

// Approve sends approveDistribution(noId, seq, root, ledgerHash) for the
// pending root the audit verified. It returns nil once the auditor's approval
// of that root counts at the current auditor set version, which is the
// record's index (an approval names no finalized root: its rootIndex is 0),
// including when a preflight finds it AlreadyApproved;
// ErrSettlementV2ApprovalClosed when the approval could not be included before
// end(e+1); ErrSettlementV2RootNotPending when the root was replaced, deleted
// or finalized.
func (self *SettlementV2Sender) Approve(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending) error {
	id := new(big.Int).SetUint64(noID)
	head, headHash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	values, err := self.chain.settlementV2ViewAtHashContext(ctx, self.addresses.Settlement, &stabi.STSettlement2MetaData, "auditorSetVersion", nil, head, headHash)
	if err != nil {
		return err
	}
	calldata := stabi.NewSTSettlement2().PackApproveDistribution(id, pending.Seq, pending.Root, pending.LedgerHash)
	return self.send(ctx, settlementV2Send{noID: noID, seq: pending.Seq, action: "approve", index: values[0].(uint64), to: self.addresses.Settlement, calldata: calldata, value: new(big.Int), done: evmSelector("AlreadyApproved()"),
		evaluate: func(ctx context.Context, landed bool) (bool, error) {
			if landed {
				return true, nil
			}
			head, headHash, err := self.pendingRootAt(ctx, id, pending)
			if err != nil {
				return false, err
			}
			values, err := self.chain.settlementV2ViewAtHashContext(ctx, self.addresses.Settlement, &stabi.STSettlement2MetaData, "hasApproved", []any{id, self.auditor}, head, headHash)
			if err != nil || values[0].(bool) {
				return err == nil, err
			}
			end, err := self.chain.ReleaseEpochEndBlockAtHashContext(ctx, head, headHash, new(big.Int).SetUint64(pending.Epoch+1))
			if err != nil {
				return false, err
			}
			return false, self.requireNextBlockBefore(ctx, end, ErrSettlementV2ApprovalClosed)
		}})
}

// Demand sends demandPath(noId, rootIndex, account, oldProof) with exactly
// demandFeeWei, where account and oldProof prove an account of the finalized
// root rootIndex. The demand blocks the pending root's finalization until the
// operator opens that account's path. It returns nil once the demand is final
// or (rootIndex, index) was already demanded, open or answered, including when
// a preflight finds it AlreadyDemanded; ErrSettlementV2ChallengeWindowClosed
// when the demand could not be included before commit + CHALLENGE_BLOCKS;
// ErrSettlementV2RootNotPending when the root was replaced, deleted or
// finalized.
func (self *SettlementV2Sender) Demand(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending, rootIndex uint64, account stabi.LedgerTreeAccount, oldProof []stabi.LedgerTreeProofNode) error {
	if !settlementV2PathEncodable(account, oldProof, nil) {
		return errors.New("settlement v2 demand account or proof has a missing or oversized integer")
	}
	id := new(big.Int).SetUint64(noID)
	head, headHash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	fee, err := self.chain.settlementV2ViewAtHashContext(ctx, self.addresses.Vault, &stabi.STSettlementVault2MetaData, "demandFeeWei", nil, head, headHash)
	if err != nil {
		return err
	}
	root := new(big.Int).SetUint64(rootIndex)
	return self.send(ctx, settlementV2Send{noID: noID, seq: pending.Seq, action: "demand", rootIndex: rootIndex, index: account.Index.Uint64(), to: self.addresses.Vault, calldata: stabi.NewSTSettlementVault2().PackDemandPath(id, root, account, oldProof), value: fee[0].(*big.Int), done: evmSelector("AlreadyDemanded()"),
		evaluate: func(ctx context.Context, landed bool) (bool, error) {
			if landed {
				return true, nil
			}
			head, headHash, err := self.pendingRootAt(ctx, id, pending)
			if err != nil {
				return false, err
			}
			values, err := self.chain.settlementV2ViewAtHashContext(ctx, self.addresses.Vault, &stabi.STSettlementVault2MetaData, "demanded", []any{id, pending.Seq, root, account.Index}, head, headHash)
			if err != nil || values[0].(uint8) != settlementV2DemandNone {
				return err == nil, err
			}
			window, err := self.chain.settlementV2ViewAtHashContext(ctx, self.addresses.Vault, &stabi.STSettlementVault2MetaData, "CHALLENGE_BLOCKS", nil, head, headHash)
			if err != nil {
				return false, err
			}
			return false, self.requireNextBlockBefore(ctx, pending.CommittedBlock+window[0].(uint64), ErrSettlementV2ChallengeWindowClosed)
		}})
}

// Challenge sends checkPath(noId, rootIndex, account, oldProof, newPath),
// proving the fault the audit found at account.Index of the finalized root
// rootIndex. It
// returns nil once the challenged root is no longer pending and was not
// finalized: deleted by this or another challenge, or replaced (VA-8),
// including when a preflight finds NoPendingRoot; ErrSettlementV2RootFinalized
// when it was finalized; and ErrSettlementV2ChallengeAnswered when the opening
// showed no fault.
func (self *SettlementV2Sender) Challenge(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending, rootIndex uint64, account stabi.LedgerTreeAccount, oldProof []stabi.LedgerTreeProofNode, newPath []stabi.LedgerTreePathNode) error {
	if !settlementV2PathEncodable(account, oldProof, newPath) {
		return errors.New("settlement v2 challenge account or path has a missing or oversized integer")
	}
	id := new(big.Int).SetUint64(noID)
	calldata := stabi.NewSTSettlementVault2().PackCheckPath(id, new(big.Int).SetUint64(rootIndex), account, oldProof, newPath)
	return self.send(ctx, settlementV2Send{noID: noID, seq: pending.Seq, action: "challenge", rootIndex: rootIndex, index: account.Index.Uint64(), to: self.addresses.Vault, calldata: calldata, value: new(big.Int), done: evmSelector("NoPendingRoot()"),
		evaluate: func(ctx context.Context, landed bool) (bool, error) {
			_, _, err := self.pendingRootAt(ctx, id, pending)
			switch {
			case err == nil && landed:
				return false, ErrSettlementV2ChallengeAnswered
			case err == nil:
				return false, nil
			case !errors.Is(err, ErrSettlementV2RootNotPending):
				return false, err
			}
			finalized, err := self.rootFinalized(ctx, id, pending)
			if err == nil && finalized {
				err = ErrSettlementV2RootFinalized
			}
			return err == nil, err
		}})
}

// The ABI encoder cannot pack a missing integer, and the record key needs the
// account index as a uint64.
func settlementV2PathEncodable(account stabi.LedgerTreeAccount, oldProof []stabi.LedgerTreeProofNode, newPath []stabi.LedgerTreePathNode) bool {
	if account.Index == nil || !account.Index.IsUint64() || account.Cumulative == nil {
		return false
	}
	for _, node := range oldProof {
		if node.Sum == nil {
			return false
		}
	}
	for _, node := range newPath {
		if node.LeftSum == nil || node.RightSum == nil {
			return false
		}
	}
	return true
}

// Sends one action at most once. A record not yet finalized is reconciled
// first; a fresh send writes its record before the first broadcast.
func (self *SettlementV2Sender) send(ctx context.Context, request settlementV2Send) error {
	if request.seq == 0 {
		return errors.New("settlement v2 audited root has no commit sequence")
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	path := filepath.Join(self.directory, fmt.Sprintf("%d-%d-%s-%d-%d.json", request.noID, request.seq, request.action, request.rootIndex, request.index))
	record, transaction, err := self.readRecord(path, request)
	if err != nil {
		return err
	}
	if record != nil && !record.Finalized {
		if record, err = self.reconcile(ctx, path, record, transaction); err != nil {
			return err
		}
	}
	if record != nil {
		_, err := request.evaluate(ctx, true)
		return err
	}
	done, err := request.evaluate(ctx, false)
	if err != nil || done {
		return err
	}
	var prepared settlementV2SendRecord
	_, err = onchain.SubmitWithHooks(ctx, onchain.SubmitParams{Contract: request.to, Rpcs: self.rpcs, Key: self.key, Calldata: request.calldata, ChainID: self.chainID, Value: request.value}, onchain.SubmitHooks{
		Prepared: func(_ common.Hash, raw []byte) error {
			prepared = settlementV2SendRecord{Schema: settlementV2SendSchema, NoID: request.noID, Seq: request.seq, Action: request.action, RootIndex: request.rootIndex, Index: request.index, Transaction: hexutil.Encode(raw)}
			return writeSettlementV2SendRecord(path, prepared)
		},
	})
	if settlementV2Reverted(err, request.done) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("settlement v2 %s no_id %d seq %d root %d index %d: %w", request.action, request.noID, request.seq, request.rootIndex, request.index, err)
	}
	prepared.Finalized = true
	if err := writeSettlementV2SendRecord(path, prepared); err != nil {
		return err
	}
	_, err = request.evaluate(ctx, true)
	return err
}

// Resolves a signed transaction from an earlier attempt. A final successful
// receipt finalizes the record. A final revert, or a nonce another transaction
// consumed, removes it so the action is evaluated afresh. Otherwise the exact
// bytes are broadcast again and the action stays pending.
func (self *SettlementV2Sender) reconcile(ctx context.Context, path string, record *settlementV2SendRecord, transaction *types.Transaction) (*settlementV2SendRecord, error) {
	head, _, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	var receipt *types.Receipt
	err = self.chain.retryChainRead(ctx, func(callCtx context.Context) error {
		var err error
		receipt, err = self.chain.client.TransactionReceipt(callCtx, transaction.Hash())
		return err
	})
	if errors.Is(err, ethereum.NotFound) {
		var nonce uint64
		if err := self.chain.retryChainRead(ctx, func(callCtx context.Context) error {
			var err error
			nonce, err = self.chain.client.NonceAt(callCtx, self.auditor, new(big.Int).SetUint64(head))
			return err
		}); err != nil {
			return nil, err
		}
		if nonce > transaction.Nonce() {
			return nil, os.Remove(path)
		}
		return nil, errors.Join(ErrSettlementV2SendPending, self.chain.client.SendTransaction(ctx, transaction))
	}
	if err != nil {
		return nil, err
	}
	if receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Uint64() > head {
		return nil, ErrSettlementV2SendPending
	}
	canonical, err := self.chain.BlockHashContext(ctx, receipt.BlockNumber.Uint64())
	if err != nil {
		return nil, err
	}
	if canonical != receipt.BlockHash {
		return nil, ErrSettlementV2SendPending
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, os.Remove(path)
	}
	record.Finalized = true
	return record, writeSettlementV2SendRecord(path, *record)
}

// Reads the action's record: nil when the action was never signed. A record
// that does not hold this auditor's transaction to the action's contract is
// refused rather than reconciled.
func (self *SettlementV2Sender) readRecord(path string, request settlementV2Send) (*settlementV2SendRecord, *types.Transaction, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var record settlementV2SendRecord
	transaction := new(types.Transaction)
	var raw []byte
	var sender common.Address
	err = json.Unmarshal(encoded, &record)
	if err == nil {
		raw, err = hexutil.Decode(record.Transaction)
	}
	if err == nil {
		err = transaction.UnmarshalBinary(raw)
	}
	if err == nil {
		sender, err = types.Sender(types.LatestSignerForChainID(self.chainID), transaction)
	}
	if err != nil || record.Schema != settlementV2SendSchema || record.NoID != request.noID || record.Seq != request.seq || record.Action != request.action || record.RootIndex != request.rootIndex || record.Index != request.index ||
		sender != self.auditor || transaction.To() == nil || *transaction.To() != request.to {
		return nil, nil, errors.Join(fmt.Errorf("settlement v2 send record %s is not this auditor's %s transaction", path, request.action), err)
	}
	return &record, transaction, nil
}

// Reports a revert whose data is exactly the custom error selector. Only a
// preflight or gas estimate carries revert data; a mined revert does not.
func settlementV2Reverted(err error, selector [4]byte) bool {
	var reverted gethrpc.DataError
	if !errors.As(err, &reverted) {
		return false
	}
	encoded, _ := reverted.ErrorData().(string)
	data, decodeErr := hexutil.Decode(encoded)
	return decodeErr == nil && len(data) == 4 && [4]byte(data) == selector
}

// A flat record of strings, integers and a bool always encodes.
func writeSettlementV2SendRecord(path string, record settlementV2SendRecord) error {
	encoded, _ := json.Marshal(record)
	return atomicStateWrite(path, append(encoded, '\n'), 0o600)
}

// Reads the pool's pending root at the finalized head and returns that head
// when it is still the audited root; any other root, or none, is
// ErrSettlementV2RootNotPending.
func (self *SettlementV2Sender) pendingRootAt(ctx context.Context, id *big.Int, audited stabi.ISTSettlementVault2Pending) (uint64, [32]byte, error) {
	head, headHash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return 0, [32]byte{}, err
	}
	values, err := self.chain.settlementV2ViewAtHashContext(ctx, self.addresses.Vault, &stabi.STSettlementVault2MetaData, "pending", []any{id}, head, headHash)
	if err != nil {
		return 0, [32]byte{}, err
	}
	current := *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Pending)).(*stabi.ISTSettlementVault2Pending)
	if current.Seq == 0 || current.Seq != audited.Seq || current.Root != audited.Root || audited.Total == nil || current.Total.Cmp(audited.Total) != 0 || current.LeafCount != audited.LeafCount ||
		current.Epoch != audited.Epoch || current.LedgerHash != audited.LedgerHash || current.CommittedBlock != audited.CommittedBlock {
		return 0, [32]byte{}, ErrSettlementV2RootNotPending
	}
	return head, headHash, nil
}

// Reports whether vault2 finalized the audited root. Finalized roots are
// appended with increasing epochs, so the scan stops below the root's epoch.
func (self *SettlementV2Sender) rootFinalized(ctx context.Context, id *big.Int, audited stabi.ISTSettlementVault2Pending) (bool, error) {
	head, headHash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return false, err
	}
	read := func(method string, arguments ...any) ([]any, error) {
		return self.chain.settlementV2ViewAtHashContext(ctx, self.addresses.Vault, &stabi.STSettlementVault2MetaData, method, arguments, head, headHash)
	}
	count, err := read("rootCount", id)
	if err != nil {
		return false, err
	}
	for index := new(big.Int).Set(count[0].(*big.Int)); index.Sign() > 0; {
		index.Sub(index, big.NewInt(1))
		values, err := read("roots", id, index)
		if err != nil {
			return false, err
		}
		root := *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Root)).(*stabi.ISTSettlementVault2Root)
		if root.Epoch < audited.Epoch {
			break
		}
		if root.Root == audited.Root && audited.Total != nil && root.Total.Cmp(audited.Total) == 0 && root.LeafCount == audited.LeafCount && root.Epoch == audited.Epoch && root.LedgerHash == audited.LedgerHash {
			return true, nil
		}
	}
	return false, nil
}

// A transaction sent now lands in the block after the latest; refuses it
// with closed unless that block is before end.
func (self *SettlementV2Sender) requireNextBlockBefore(ctx context.Context, end uint64, closed error) error {
	var latest uint64
	if err := self.chain.retryChainRead(ctx, func(callCtx context.Context) error {
		var err error
		latest, err = self.chain.client.BlockNumber(callCtx)
		return err
	}); err != nil {
		return err
	}
	if latest+1 >= end {
		return closed
	}
	return nil
}
