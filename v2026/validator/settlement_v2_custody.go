//go:build linux || darwin

// The settlement v2 audit keeps what it audits in durable custody
// (VERSION2-FINAL.md §9.2): every ledger object it fetched, by sha256; per
// pool epoch, the chunks of the provider rows it found valid and every null
// declaration it accepted; and one record per pending root with the finding
// and the decision. Files live under <state_dir>/settlement-v2-ledger and are
// written like the rest of the validator's state: atomically, owner-only.
package validator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

const settlementV2CustodySchema = "urnetwork-settlement-v2-custody-v1"

// settlementV2Custody is the validator's durable ledger.Custody.
type settlementV2Custody struct {
	directory string
}

// The record of one epoch's valid rows, and of one null declaration.
type settlementV2RowsRecord struct {
	Schema string   `json:"schema"`
	NoID   uint64   `json:"no_id"`
	Epoch  uint64   `json:"epoch"`
	Chunks []string `json:"chunks"`
}

type settlementV2NullRecord struct {
	Schema string `json:"schema"`
	NoID   uint64 `json:"no_id"`
	Epoch  uint64 `json:"epoch"`
	Seq    uint64 `json:"seq"`
}

// settlementV2AuditRoot identifies the pending root an audit record is about.
type settlementV2AuditRoot struct {
	Schema         string `json:"schema"`
	NoID           uint64 `json:"no_id"`
	Seq            uint64 `json:"seq"`
	Epoch          uint64 `json:"epoch"`
	Root           string `json:"root"`
	Total          string `json:"total"`
	LeafCount      uint64 `json:"leaf_count"`
	LedgerHash     string `json:"ledger_hash"`
	CommittedBlock uint64 `json:"committed_block"`
}

// settlementV2AuditRecord is the audit's finding and decision for one pending
// root.
type settlementV2AuditRecord struct {
	settlementV2AuditRoot
	// FetchedBytes counts the object bytes fetched for the root over every
	// attempt, against --ledger_max_bytes.
	FetchedBytes uint64 `json:"fetched_bytes"`
	// The latest audit: its pinned block, the first check that failed and
	// its error, the objects it could not obtain, and the accounts of the
	// protected finalized root that the published rows fault.
	Block   uint64                   `json:"block"`
	Check   string                   `json:"check"`
	Error   string                   `json:"error,omitempty"`
	Missing []string                 `json:"missing,omitempty"`
	Faults  []settlementV2AuditFault `json:"faults,omitempty"`
	// Decision is empty while the audit waits for its objects, then approve,
	// challenge, demand or refuse. A challenge or demand proves accounts of
	// the finalized root RootIndex; a demand's set is drawn once and sent in
	// its order.
	Decision  string   `json:"decision,omitempty"`
	RootIndex uint64   `json:"root_index"`
	Doublings uint     `json:"doublings,omitempty"`
	Demands   []uint64 `json:"demands,omitempty"`
	// Done: nothing is left to send, for the reason in Outcome.
	Done    bool   `json:"done,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

type settlementV2AuditFault struct {
	Index uint64 `json:"index"`
	Kind  string `json:"kind"`
}

// observe records an audit's finding at the pinned block.
func (self *settlementV2AuditRecord) observe(block uint64, res *ledger.Result) {
	self.Block, self.Check, self.Error, self.Missing, self.Faults = block, res.Check.String(), "", nil, settlementV2AuditFaults(res.Faults)
	if res.Err != nil {
		self.Error = res.Err.Error()
	}
	for _, ref := range res.Missing {
		self.Missing = append(self.Missing, ref.String())
	}
}

func settlementV2AuditFaults(faults []ledger.Fault) []settlementV2AuditFault {
	var out []settlementV2AuditFault
	for _, fault := range faults {
		out = append(out, settlementV2AuditFault{Index: fault.Index, Kind: fault.Kind.String()})
	}
	return out
}

// openSettlementV2Custody keeps custody under stateDir/settlement-v2-ledger.
func openSettlementV2Custody(stateDir string) (*settlementV2Custody, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("settlement v2 custody needs an absolute state directory")
	}
	directory := filepath.Join(stateDir, "settlement-v2-ledger")
	if err := ensurePrivateStateDir(directory); err != nil {
		return nil, err
	}
	return &settlementV2Custody{directory: directory}, nil
}

func (self *settlementV2Custody) path(kind, name string) string {
	return filepath.Join(self.directory, kind, name)
}

// readJSON decodes a custody record; ok is false when it does not exist.
func (self *settlementV2Custody) readJSON(path string, value any) (bool, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err == nil {
		err = json.Unmarshal(encoded, value)
	}
	if err != nil {
		return false, fmt.Errorf("settlement v2 custody %s: %w", path, err)
	}
	return true, nil
}

// Records hold strings, integers, booleans and slices of them, which always
// encode.
func (self *settlementV2Custody) writeJSON(path string, value any) error {
	encoded, _ := json.Marshal(value)
	return atomicStateWrite(path, append(encoded, '\n'), 0o600)
}

// Object returns a held object. Held objects are trusted by the audit, so
// bytes that no longer match their reference are an error, never an object.
func (self *settlementV2Custody) Object(ctx context.Context, ref ledger.Ref) ([]byte, bool, error) {
	path := self.path("objects", hex.EncodeToString(ref[:]))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if ledger.RefOf(data) != ref {
		return nil, false, fmt.Errorf("settlement v2 custody object %s does not match its reference", path)
	}
	return data, true, nil
}

// Keep stores an object the audit checked against its reference. Objects are
// immutable, so one already held is not written again.
func (self *settlementV2Custody) Keep(ctx context.Context, ref ledger.Ref, data []byte) error {
	path := self.path("objects", hex.EncodeToString(ref[:]))
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return atomicStateWrite(path, data, 0o600)
}

// ValidRows returns the chunks of the epoch's rows that an audit found valid.
func (self *settlementV2Custody) ValidRows(ctx context.Context, noID, epoch uint64) ([]ledger.Ref, bool, error) {
	var record settlementV2RowsRecord
	path := self.path("rows", fmt.Sprintf("%d-%d.json", noID, epoch))
	ok, err := self.readJSON(path, &record)
	if !ok {
		return nil, false, err
	}
	if record.Schema != settlementV2CustodySchema || record.NoID != noID || record.Epoch != epoch || record.Chunks == nil {
		return nil, false, fmt.Errorf("settlement v2 custody %s is not pool %d epoch %d's rows", path, noID, epoch)
	}
	chunks := make([]ledger.Ref, len(record.Chunks))
	for i, text := range record.Chunks {
		if chunks[i], err = ledger.ParseRef(text); err != nil {
			return nil, false, fmt.Errorf("settlement v2 custody %s: %w", path, err)
		}
	}
	return chunks, true, nil
}

// KeepValidRows records the epoch's rows, listed by chunks, as valid.
func (self *settlementV2Custody) KeepValidRows(ctx context.Context, noID, epoch uint64, chunks []ledger.Ref) error {
	record := settlementV2RowsRecord{Schema: settlementV2CustodySchema, NoID: noID, Epoch: epoch, Chunks: make([]string, len(chunks))}
	for i, ref := range chunks {
		record.Chunks[i] = ref.String()
	}
	return self.writeJSON(self.path("rows", fmt.Sprintf("%d-%d.json", noID, epoch)), record)
}

// KeepNull records that pending root seq declared the epoch unrecoverable.
func (self *settlementV2Custody) KeepNull(ctx context.Context, noID, epoch, seq uint64) error {
	record := settlementV2NullRecord{Schema: settlementV2CustodySchema, NoID: noID, Epoch: epoch, Seq: seq}
	return self.writeJSON(self.path("nulls", fmt.Sprintf("%d-%d-%d.json", noID, epoch, seq)), record)
}

// auditRecord returns the record of the pending root, or a new one. vault2
// never reuses a seq, so a record of another root under its seq is refused.
func (self *settlementV2Custody) auditRecord(noID uint64, pending stabi.ISTSettlementVault2Pending) (*settlementV2AuditRecord, error) {
	fresh := &settlementV2AuditRecord{settlementV2AuditRoot: settlementV2AuditRoot{Schema: settlementV2CustodySchema, NoID: noID, Seq: pending.Seq, Epoch: pending.Epoch,
		Root: hexutil.Encode(pending.Root[:]), Total: pending.Total.String(), LeafCount: pending.LeafCount, LedgerHash: ledger.Ref(pending.LedgerHash).String(), CommittedBlock: pending.CommittedBlock}}
	var record settlementV2AuditRecord
	path := self.path("audits", fmt.Sprintf("%d-%d.json", noID, pending.Seq))
	ok, err := self.readJSON(path, &record)
	if !ok {
		return fresh, err
	}
	if record.settlementV2AuditRoot != fresh.settlementV2AuditRoot {
		return nil, fmt.Errorf("settlement v2 custody %s is not the audit of pool %d's pending root seq %d", path, noID, pending.Seq)
	}
	return &record, nil
}

// accounts returns a finalized root's accounts and their tree from custody
// alone, or nil when custody does not hold every object of the root or they
// do not rebuild it.
func (self *settlementV2Custody) accounts(ctx context.Context, root ledger.Root) ([]merkle.LedgerAccount, *merkle.LedgerSumTree, error) {
	data, ok, err := self.Object(ctx, root.LedgerHash)
	if err != nil || !ok {
		return nil, nil, err
	}
	header, err := ledger.DecodeHeader(data)
	if err != nil {
		return nil, nil, nil
	}
	chunks := make([][]byte, len(header.Accounts))
	for j, ref := range header.Accounts {
		if chunks[j], ok, err = self.Object(ctx, ref); err != nil || !ok {
			return nil, nil, err
		}
	}
	accounts, err := ledger.DecodeAccountChunks(chunks)
	if err != nil {
		return nil, nil, nil
	}
	tree := settlementV2Rebuild(ctx, accounts, root)
	if tree == nil {
		return nil, nil, ctx.Err()
	}
	return accounts, tree, nil
}

// settlementV2Rebuild returns the tree of accounts when it is root, with its
// total and leaf count; nil when it is not, or ctx ended.
func settlementV2Rebuild(ctx context.Context, accounts []merkle.LedgerAccount, root ledger.Root) *merkle.LedgerSumTree {
	tree, err := merkle.BuildLedgerSumTree(ctx, accounts)
	if err != nil {
		return nil
	}
	if hash, total := tree.Root(); hash != root.Root || !total.Eq(&root.Total) || tree.LeafCount() != root.LeafCount {
		return nil
	}
	return tree
}

func (self *settlementV2Custody) keepAuditRecord(record *settlementV2AuditRecord) error {
	return self.writeJSON(self.path("audits", fmt.Sprintf("%d-%d.json", record.NoID, record.Seq)), record)
}
