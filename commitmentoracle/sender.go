package commitmentoracle

// The oracle's transaction sender. One transaction is in flight at a time, so
// the nonce is always the account's finalized nonce: a send requires the
// finalized, latest and pending nonces to agree and no unfinished record of
// its own. Before its first broadcast every signed transaction is written as a
// durable record under <state_dir>/mirror-sends, so a restart reconciles that
// exact transaction (rebroadcasting the same bytes) and never signs a second
// one for the same nonce. A record is finished when its receipt is final and
// canonical; a final revert, or a nonce another transaction consumed, sets the
// record aside as dropped and the commitment is evaluated afresh from chain
// state. Not safe for concurrent use; the oracle loop is its only caller.

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/urfoundation/sn/internal/durablesync"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/stabi"
)

const sendRecordSchema = "urnetwork-sn25-commitment-mirror-send-v1"

var (
	// The key's address is not the coordinator's active commitment oracle.
	errNotActiveOracle = errors.New("the oracle key is not the coordinator's active commitment oracle")
	// A transaction of this account that the oracle has no record of is
	// pending or not yet final; sending now could collide with its nonce.
	errForeignTransaction = errors.New("the oracle account has a transaction the oracle did not record that is pending or not final")
	errGasPriceTooHigh    = errors.New("the suggested gas price is above the configured maximum")
	errGasLimitTooHigh    = errors.New("the gas estimate is above the configured maximum gas limit")
	errInsufficientFunds  = errors.New("the oracle balance cannot pay the bounded transaction fee")
	errCoordinatorPaused  = errors.New("the coordinator is paused")
)

// The outcome of one mirror attempt.
type sendOutcome string

const (
	// Signed and broadcast; the record is pending until its receipt is final.
	outcomeSent sendOutcome = "sent"
	// Dry run: preflight and estimate succeeded and nothing was signed.
	outcomeDryRun sendOutcome = "dry_run"
	// The preflight reverted StaleCommitment: the coordinator holds this record
	// or a newer one.
	outcomeStale sendOutcome = "stale"
	// The preflight reverted InvalidConfiguration (zero argument or a block
	// ahead of the EVM head).
	outcomeInvalid sendOutcome = "invalid"
)

// The state of a reconciled record.
type recordState string

const (
	recordPending recordState = "pending"
	recordDone    recordState = "done"
	recordDropped recordState = "dropped"
)

// One signed mirror transaction, durable before its first broadcast.
type sendRecord struct {
	Schema             string `json:"schema"`
	Hotkey             string `json:"hotkey"`
	CommitmentHash     string `json:"commitment_hash"`
	FinalizedBlock     uint64 `json:"finalized_block"`
	FinalizedBlockHash string `json:"finalized_block_hash"`
	Nonce              uint64 `json:"nonce"`
	TransactionHash    string `json:"transaction_hash"`
	Transaction        string `json:"transaction"`
	SignedUnix         int64  `json:"signed_unix"`
	Finalized          bool   `json:"finalized"`
	ReceiptBlock       uint64 `json:"receipt_block,omitempty"`
}

// A record with its decoded transaction and path.
type pendingSend struct {
	path        string
	record      sendRecord
	args        mirrorArgs
	transaction *types.Transaction
}

// The fee and gas bounds of every transaction.
type feeSettings struct {
	maxGasPrice *big.Int
	maxGasLimit uint64
}

type sender struct {
	client      evmClient
	chainId     *big.Int
	coordinator common.Address
	oracle      common.Address
	// nil in a dry run without a key; never logged.
	key       *ecdsa.PrivateKey
	dryRun    bool
	fees      feeSettings
	directory string
	now       func() time.Time
	logf      func(format string, args ...any)
}

func (self *sender) recordPath(args mirrorArgs) string {
	return filepath.Join(self.directory, fmt.Sprintf("%x-%d.json", args.hotkey, args.finalizedBlock))
}

func mirrorCalldata(args mirrorArgs) []byte {
	return stabi.NewSTCoordinator().PackMirrorCommitment(args.hotkey, args.commitmentHash, args.finalizedBlock, args.finalizedBlockHash)
}

// Unfinished records in nonce order. A record that is not this oracle's
// mirrorCommitment transaction to this coordinator with exactly its recorded
// arguments is refused rather than reconciled.
func (self *sender) unfinished() ([]pendingSend, error) {
	entries, err := os.ReadDir(self.directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sends := []pendingSend{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		send, err := self.readRecord(filepath.Join(self.directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if !send.record.Finalized {
			sends = append(sends, send)
		}
	}
	slices.SortFunc(sends, func(a pendingSend, b pendingSend) int {
		return int(a.transaction.Nonce()) - int(b.transaction.Nonce())
	})
	return sends, nil
}

func (self *sender) readRecord(path string) (pendingSend, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return pendingSend{}, err
	}
	invalid := func(reason string) (pendingSend, error) {
		return pendingSend{}, fmt.Errorf("send record %s is not this oracle's mirror transaction: %s", path, reason)
	}
	var record sendRecord
	if err := json.Unmarshal(encoded, &record); err != nil || record.Schema != sendRecordSchema {
		return invalid("schema")
	}
	hotkey, err1 := decodeHash(record.Hotkey)
	commitmentHash, err2 := decodeHash(record.CommitmentHash)
	blockHash, err3 := decodeHash(record.FinalizedBlockHash)
	if err := errors.Join(err1, err2, err3); err != nil {
		return invalid(err.Error())
	}
	args := mirrorArgs{hotkey: hotkey, commitmentHash: commitmentHash, finalizedBlock: record.FinalizedBlock, finalizedBlockHash: blockHash}
	raw, err := hexutil.Decode(record.Transaction)
	if err != nil {
		return invalid("transaction encoding")
	}
	transaction := new(types.Transaction)
	if err := transaction.UnmarshalBinary(raw); err != nil {
		return invalid("transaction encoding")
	}
	from, err := types.Sender(types.LatestSignerForChainID(self.chainId), transaction)
	if err != nil || from != self.oracle {
		return invalid("sender")
	}
	if transaction.To() == nil || *transaction.To() != self.coordinator || transaction.Value().Sign() != 0 {
		return invalid("destination")
	}
	if string(transaction.Data()) != string(mirrorCalldata(args)) || transaction.Nonce() != record.Nonce || transaction.Hash().Hex() != record.TransactionHash {
		return invalid("arguments")
	}
	if filepath.Base(path) != filepath.Base(self.recordPath(args)) {
		return invalid("file name")
	}
	return pendingSend{path: path, record: record, args: args, transaction: transaction}, nil
}

func decodeHash(value string) ([32]byte, error) {
	decoded, err := hexutil.Decode(value)
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, fmt.Errorf("%q is not a 32-byte hex value", value)
	}
	return [32]byte(decoded), nil
}

func writeSendRecord(path string, record sendRecord) error {
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(encoded, '\n'), 0o600)
}

// Resolves one record against the EVM finalized block. A pending record whose
// transaction is unknown is rebroadcast with its exact bytes.
func (self *sender) reconcile(ctx context.Context, send pendingSend) (recordState, error) {
	finalized, _, err := self.client.finalizedBlock(ctx)
	if err != nil {
		return recordPending, err
	}
	receipt, err := self.client.transactionReceipt(ctx, send.transaction.Hash())
	if errors.Is(err, ethereum.NotFound) || (err == nil && receipt == nil) {
		nonce, err := self.client.nonceAt(ctx, self.oracle, new(big.Int).SetUint64(finalized))
		if err != nil {
			return recordPending, err
		}
		if nonce > send.transaction.Nonce() {
			// Another transaction finalized this nonce. The commitment is
			// re-evaluated from the coordinator's finalized state.
			self.logf("send %s nonce %d was consumed by another transaction; re-evaluating %s", send.record.TransactionHash, send.transaction.Nonce(), send.args)
			return recordDropped, os.Rename(send.path, fmt.Sprintf("%s.dropped-%d", send.path, send.transaction.Nonce()))
		}
		if err := self.client.sendTransaction(ctx, send.transaction); err != nil && !alreadyKnown(err) {
			self.logf("rebroadcast %s: %v", send.record.TransactionHash, err)
		}
		return recordPending, nil
	}
	if err != nil {
		return recordPending, err
	}
	if receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Uint64() > finalized {
		return recordPending, nil
	}
	canonical, err := self.client.blockHash(ctx, receipt.BlockNumber.Uint64())
	if err != nil {
		return recordPending, err
	}
	if canonical != receipt.BlockHash {
		return recordPending, nil
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		self.logf("send %s reverted in finalized block %d; re-evaluating %s", send.record.TransactionHash, receipt.BlockNumber.Uint64(), send.args)
		return recordDropped, os.Rename(send.path, fmt.Sprintf("%s.dropped-%d", send.path, send.transaction.Nonce()))
	}
	send.record.Finalized = true
	send.record.ReceiptBlock = receipt.BlockNumber.Uint64()
	return recordDone, writeSendRecord(send.path, send.record)
}

func alreadyKnown(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "already known") || strings.Contains(message, "already imported") || strings.Contains(message, "known transaction")
}

// Preflights, bounds and, outside a dry run, signs, records and broadcasts one
// mirror. It returns the record of a sent transaction.
func (self *sender) send(ctx context.Context, args mirrorArgs) (sendOutcome, *pendingSend, error) {
	calldata := mirrorCalldata(args)
	coordinator := self.coordinator
	msg := ethereum.CallMsg{From: self.oracle, To: &coordinator, Data: calldata, Value: new(big.Int)}
	classify := func(err error) (sendOutcome, *pendingSend, error) {
		selector, ok := revertSelector(err)
		switch {
		case ok && selector == selectorStaleCommitment:
			return outcomeStale, nil, nil
		case ok && selector == selectorInvalidConfiguration:
			return outcomeInvalid, nil, nil
		case ok && selector == selectorUnauthorized:
			return "", nil, errNotActiveOracle
		case ok && selector == selectorPaused:
			return "", nil, errCoordinatorPaused
		}
		return "", nil, fmt.Errorf("mirrorCommitment preflight for %s: %w", args, err)
	}
	if _, err := self.client.callContract(ctx, msg, nil); err != nil {
		return classify(err)
	}
	estimate, err := self.client.estimateGas(ctx, msg)
	if err != nil {
		return classify(err)
	}
	gasLimit := estimate + estimate/5
	if gasLimit > self.fees.maxGasLimit {
		return "", nil, fmt.Errorf("%w: %d with headroom, maximum %d", errGasLimitTooHigh, gasLimit, self.fees.maxGasLimit)
	}

	finalized, _, err := self.client.finalizedBlock(ctx)
	if err != nil {
		return "", nil, err
	}
	finalizedNonce, err := self.client.nonceAt(ctx, self.oracle, new(big.Int).SetUint64(finalized))
	if err != nil {
		return "", nil, err
	}
	latestNonce, err := self.client.nonceAt(ctx, self.oracle, nil)
	if err != nil {
		return "", nil, err
	}
	pendingNonce, err := self.client.pendingNonceAt(ctx, self.oracle)
	if err != nil {
		return "", nil, err
	}
	if finalizedNonce != latestNonce || latestNonce != pendingNonce {
		return "", nil, fmt.Errorf("%w: nonces finalized %d, latest %d, pending %d", errForeignTransaction, finalizedNonce, latestNonce, pendingNonce)
	}
	gasPrice, err := self.client.suggestGasPrice(ctx)
	if err != nil {
		return "", nil, err
	}
	if gasPrice.Sign() <= 0 || gasPrice.Cmp(self.fees.maxGasPrice) > 0 {
		return "", nil, fmt.Errorf("%w: %s wei, maximum %s wei", errGasPriceTooHigh, gasPrice, self.fees.maxGasPrice)
	}
	fee := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(gasLimit))
	balance, err := self.client.balanceAt(ctx, self.oracle, nil)
	if err != nil {
		return "", nil, err
	}
	funded := balance.Cmp(fee) >= 0
	if self.dryRun {
		self.logf("dry run: would send mirrorCommitment(%s) to %s from %s: nonce %d, gas limit %d (estimate %d), gas price %s wei, fee up to %s wei, balance %s wei (funded %t), calldata 0x%x",
			args, self.coordinator.Hex(), self.oracle.Hex(), finalizedNonce, gasLimit, estimate, gasPrice, fee, balance, funded, calldata)
		return outcomeDryRun, nil, nil
	}
	if !funded {
		return "", nil, fmt.Errorf("%w: balance %s wei, fee up to %s wei", errInsufficientFunds, balance, fee)
	}
	if self.key == nil {
		return "", nil, errors.New("no oracle key outside a dry run")
	}
	transaction, err := types.SignTx(types.NewTx(&types.LegacyTx{
		Nonce:    finalizedNonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &coordinator,
		Value:    new(big.Int),
		Data:     calldata,
	}), types.LatestSignerForChainID(self.chainId), self.key)
	if err != nil {
		return "", nil, err
	}
	raw, err := transaction.MarshalBinary()
	if err != nil {
		return "", nil, err
	}
	send := pendingSend{
		path: self.recordPath(args),
		args: args,
		record: sendRecord{
			Schema:             sendRecordSchema,
			Hotkey:             hexutil.Encode(args.hotkey[:]),
			CommitmentHash:     hexutil.Encode(args.commitmentHash[:]),
			FinalizedBlock:     args.finalizedBlock,
			FinalizedBlockHash: hexutil.Encode(args.finalizedBlockHash[:]),
			Nonce:              transaction.Nonce(),
			TransactionHash:    transaction.Hash().Hex(),
			Transaction:        hexutil.Encode(raw),
			SignedUnix:         self.now().Unix(),
		},
		transaction: transaction,
	}
	if _, err := os.Stat(send.path); err == nil {
		return "", nil, fmt.Errorf("send record %s already exists", send.path)
	}
	if err := writeSendRecord(send.path, send.record); err != nil {
		return "", nil, err
	}
	self.logf("sending mirrorCommitment(%s): transaction %s, nonce %d, gas limit %d, gas price %s wei", args, send.record.TransactionHash, transaction.Nonce(), gasLimit, gasPrice)
	if err := self.client.sendTransaction(ctx, transaction); err != nil && !alreadyKnown(err) {
		// The record stays; reconciliation rebroadcasts the same bytes or
		// drops it once another transaction consumed the nonce.
		self.logf("broadcast %s: %v", send.record.TransactionHash, err)
	}
	return outcomeSent, &send, nil
}

// Writes data to path through a synced temporary file and a rename.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".tmp-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	if handle, err := os.Open(directory); err == nil {
		// best-effort as before; the flush routes through the shared helper so
		// the platform without a directory flush proceeds instead of failing
		_ = durablesync.Sync(handle)
		_ = handle.Close()
	}
	return nil
}

// The address of a key, for comparisons and logs; the key itself never leaves
// the sender.
func keyAddress(key *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(key.PublicKey)
}
