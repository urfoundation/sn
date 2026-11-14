//go:build linux || darwin

// The deposit audit of a source epoch settled under settlement v2, whose ledger
// has no payout artifact. Under a signed price, the audit finds the epoch's
// DistributionCommitted event, so a refused or deleted root changes nothing; reads
// the committed header and the epoch's provider rows through the ledger custody;
// selects the operator's signed usage statement by the chunks the header lists;
// and judges the deposit against it.
//
// The event is a chain fact, read again at the pinned window by replay. Ledger
// objects are content addressed and stay in custody. Everything else the operator's
// API said, including a failure to obtain an object, is retained in one signed
// observation beside the payout artifact observations, so replay never asks
// today's endpoint for a past fact. Provider chunks are too large for an
// observation or for the history byte allowance, so replay reads them from custody
// and neither charges them.
//
// An epoch whose committed header carries an extender component (WHITEPAPER.md
// §8.6.4, from the extender pool amendment's first epoch E_x) has a version 2
// statement, read under its own evidence kind: the audit also reads the census and
// share-row chunks the header lists for it, retained like provider chunks, and
// holds the statement to what they and the header hold. Its total_usage_bytes,
// which the deposit prices, is the provider rows' usage plus all of Z. Every other
// epoch is audited exactly as before the amendment, reading the same objects and
// the same version 1 statement.
//
// Live steering verifies an epoch's committed rows once per process and reuses what
// they hold on later passes, which still read and judge the statement and compare
// the deposit. All of one audit's reads of the operator's answers share one bounded
// time, and a root's ledger objects one byte budget kept across passes. Running out
// of either leaves the pool without weight, retained like any failed read. Replay
// keeps neither: it verifies from its evidence, one chunk at a time.
//
// Nothing an operator controls fails the pass. Each such input is a verdict for that
// pool alone, the same live and in replay.
//
// Two limits are deliberate. A chunk not fetched within one pass's share is fetched
// from its start again on the next pass: nothing resumes a partial object, so each
// chunk must arrive within one share (about 42 KB/s for a full chunk in three
// minutes). And the audit keeps the objects replay can reach for as long as the
// intent history keeps the decisions that name them (pruneDepositUsageObjectsV2).
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/demand/usage"
	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

const (
	releaseUsageHttpObservationSchemaV2 = "urnetwork-validator-usage-http-observation-v2"
	// The capture source a replay reads a ledger object from when it holds no
	// validator state: it counts as data, not control.
	releaseUsageLedgerObjectCaptureKind = "settlement-v2-ledger-object"

	releaseUsageHistorySchema  = "urnetwork-release-evidence-history-v1"
	releaseUsageEnvelopeSchema = "urnetwork-release-evidence-v1"
	// A history of the most objects the audit reads, and one over to show there
	// are more. Each is one line of about 200 bytes.
	maximumUsageHistoryBytes = 16 * 1024
	// A statement names about 800 chunks at this size, over 100 million rows. An
	// operator's API that answers with more cannot make a validator retain more.
	maximumUsageEnvelopeBytes = 60 * 1024
	// One history and each of its objects. A failed ledger object read ends the
	// audit before any of them.
	maximumUsageExchanges = 1 + maximumExactArtifactObjects
	// What a public audit payload holds besides its observation.
	usageObservationPayloadReserve = 16 * 1024
	// A retained label: an exchange's, or an audit's error. It holds operator text,
	// and decodeArtifactHttpObservationV2 admits up to 4096 bytes.
	maximumUsageExchangeLabel = 1024
	// What an observation holds besides its bodies: each exchange's three labels,
	// which JSON can escape to six times their bytes, its URL and field names, and
	// the observation's own fields. So no answers can outgrow its allowance.
	usageObservationFramingReserve = maximumUsageExchanges*(3*6*maximumUsageExchangeLabel+2048) + 2048

	// The longest one audit reads the operator's answers: its ledger objects,
	// statement history and statements, with retries and the operator's requested
	// waits. It is well inside the steering operation
	// (DefaultReleaseSteeringSettings), and cuts short one GET's five-minute retry
	// window.
	depositUsageReadTimeLimit = 3 * time.Minute
	// The source epochs whose verified rows a process keeps.
	maximumDepositUsageMemoEntries = 16
	// Every chunk of a provider row list but its last holds this many bytes.
	depositUsageFullChunkBytes = ledger.ChunkRows * ledger.ProviderRowBytes

	// What a retained read says when the audit stopped it: the audit's read time ran
	// out, this root's ledger byte budget ran out, or an object already held is
	// larger than its role allows.
	depositUsageReadTimeSpent   = "the usage audit's read time budget is spent"
	depositUsageByteBudgetSpent = "this root's ledger byte budget is spent"
	depositUsageHeldOversize    = "the held object exceeds its bound"
	// Only a success's body is used. A refusal's is read just this far, so that a
	// retried error cannot make an attempt read a whole object's worth.
	depositUsageRefusalBodyBytes = 4096
)

var (
	errDepositUsageReadTimeSpent   = errors.New(depositUsageReadTimeSpent)
	errDepositUsageByteBudgetSpent = errors.New(depositUsageByteBudgetSpent)
	errDepositUsageHeldOversize    = errors.New(depositUsageHeldOversize)
)

// depositUsageReadShare is each operator's share of a pass's time for reading its
// answers: the limit, or depositUsageReadTimeLimit for none, and never more than an
// equal part of half of what the pass has left. It is fixed before any operator is
// audited, so no operator's slowness can spend another's time.
func depositUsageReadShare(ctx context.Context, limit time.Duration, operators int) time.Duration {
	if limit <= 0 {
		limit = depositUsageReadTimeLimit
	}
	if deadline, ok := ctx.Deadline(); ok {
		limit = min(limit, time.Until(deadline)/2/time.Duration(max(operators, 1)))
	}
	return limit
}

// depositUsageVerified is what a source epoch's committed rows hold once verified:
// the header's chunks for the epoch, in order, and the rows' count and usage, with
// the epoch's extender component when its header carries one.
type depositUsageVerified struct {
	chunks []string
	rows   DepositUsageRows
	// The committed extender component as its statement must carry it
	// (usage.CommittedExtenders), nil for an epoch without one.
	extenders *usage.Extenders
}

// Copies a verified value, so a memo entry shares nothing with its readers.
func (self depositUsageVerified) clone() depositUsageVerified {
	self.chunks = slices.Clone(self.chunks)
	if self.extenders != nil {
		extenders := *self.extenders
		extenders.Census, extenders.Chunks = slices.Clone(extenders.Census), slices.Clone(extenders.Chunks)
		self.extenders = &extenders
	}
	return self
}

type depositUsageMemoKey struct {
	noID       uint64
	epoch      uint64
	ledgerHash [32]byte
}

// releaseDepositUsageMemo keeps in-process what live steering verified of each source
// epoch's committed rows. The commit's ledger hash fixes the header, and so every
// content-addressed chunk, so an entry is a pure function of its key.
type releaseDepositUsageMemo struct {
	lock    sync.Mutex
	entries map[depositUsageMemoKey]depositUsageVerified
	// fills counts the epochs whose rows were read and verified.
	fills uint64
}

func newReleaseDepositUsageMemo() *releaseDepositUsageMemo {
	return &releaseDepositUsageMemo{entries: map[depositUsageMemoKey]depositUsageVerified{}}
}

func (self *releaseDepositUsageMemo) get(key depositUsageMemoKey) (depositUsageVerified, bool) {
	if self == nil {
		return depositUsageVerified{}, false
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	value, found := self.entries[key]
	return value.clone(), found
}

func (self *releaseDepositUsageMemo) put(key depositUsageMemoKey, value depositUsageVerified) {
	if self == nil {
		return
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	if _, found := self.entries[key]; !found && len(self.entries) >= maximumDepositUsageMemoEntries {
		// The oldest source epoch goes first.
		first, oldest := true, depositUsageMemoKey{}
		for existing := range self.entries {
			if first || existing.epoch < oldest.epoch {
				first, oldest = false, existing
			}
		}
		delete(self.entries, oldest)
	}
	self.entries[key] = value.clone()
	self.fills++
}

// releaseDepositUsageCommit is the DistributionCommitted event of a source epoch.
type releaseDepositUsageCommit struct {
	seq        uint64
	root       [32]byte
	ledgerHash [32]byte
	block      uint64
}

// releaseUsageSettledV2Context reports whether noID's epoch settled under v2:
// vault2 registered the pool and the epoch is at or after its first. As with the
// weight destination (settlement_v2_resolver.go), the registration is read at the
// finalized head, because it is immutable and may postdate the decision block. It
// is not read from the v1 root, which a phase C sweep can commit on a v2 epoch.
func (self *ChainClient) releaseUsageSettledV2Context(ctx context.Context, v2 SettlementV2Addresses, headBlock uint64, headHash [32]byte, noID, epoch uint64) (bool, error) {
	values, err := self.settlementV2ViewAtHashContext(ctx, v2.Vault, &stabi.STSettlementVault2MetaData, "pools", []any{new(big.Int).SetUint64(noID)}, headBlock, headHash)
	if err != nil {
		return false, fmt.Errorf("no_id %d vault2 pool: %w", noID, err)
	}
	pool := *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Pool)).(*stabi.ISTSettlementVault2Pool)
	return pool.Registered && epoch >= pool.FirstEpoch, nil
}

// readDepositUsageCommitV2 scans the pool's DistributionCommitted events over
// [from, to], the source epoch's commit window up to the decision, for the latest
// that commits epoch. It is the event, and neither pending nor roots, so a refused
// or deleted root does not change the pool's weight. vault2
// spaces commits by its challenge window, so a mainnet window holds at most one.
func (self *ChainClient) readDepositUsageCommitV2(ctx context.Context, vault common.Address, noID, epoch, from, to uint64) (*releaseDepositUsageCommit, error) {
	if to < from {
		return nil, fmt.Errorf("usage root scan range [%d,%d] is empty", from, to)
	}
	// A node serves a block's logs once it serves the block, and every block before
	// it, so a node whose log index lags fails here instead of answering short.
	if _, err := self.BlockHashContext(ctx, to); err != nil {
		return nil, fmt.Errorf("block %d before its usage root scan: %w", to, err)
	}
	topics := [][]common.Hash{{settlementV2VaultABI.Events["DistributionCommitted"].ID}, {common.BigToHash(new(big.Int).SetUint64(noID))}}
	var found *releaseDepositUsageCommit
	var foundIndex uint
	for start := from; start <= to; start += getLogsChunkBlocks {
		logs, err := readSettlementV2Logs(ctx, self, vault, topics, start, min(start+getLogsChunkBlocks-1, to))
		if err != nil {
			return nil, fmt.Errorf("usage root scan: %w", err)
		}
		for i := range logs {
			event, err := settlementV2Vault.UnpackDistributionCommittedEvent(&logs[i])
			if err != nil {
				return nil, fmt.Errorf("vault2 log %d of block %d: %w", logs[i].Index, logs[i].BlockNumber, err)
			}
			if event.NoId == nil || !event.NoId.IsUint64() || event.NoId.Uint64() != noID {
				return nil, fmt.Errorf("vault2 DistributionCommitted log %d of block %d names another pool", logs[i].Index, logs[i].BlockNumber)
			}
			if event.Epoch != epoch {
				continue
			}
			hash, err := self.BlockHashContext(ctx, logs[i].BlockNumber)
			if err != nil {
				return nil, err
			}
			if common.Hash(hash) != logs[i].BlockHash {
				return nil, fmt.Errorf("vault2 DistributionCommitted log %d of block %d is not canonical", logs[i].Index, logs[i].BlockNumber)
			}
			if found == nil || logs[i].BlockNumber > found.block || logs[i].BlockNumber == found.block && logs[i].Index > foundIndex {
				found, foundIndex = &releaseDepositUsageCommit{seq: event.Seq, root: event.Root, ledgerHash: event.LedgerHash, block: logs[i].BlockNumber}, logs[i].Index
			}
		}
	}
	return found, nil
}

// pruneDepositUsageV2 runs the usage audit's retention once a source epoch, between
// passes: a pass calls it before it audits anything. A deployment that never kept an
// object reads and writes nothing. A failure is logged, never the pass's.
func (self *ReleaseSteerer) pruneDepositUsageV2(ctx context.Context, snapshot *ReleaseSnapshot) {
	lag := self.cfg.Policy.Deposit.UsageLagEpochs
	if snapshot == nil || snapshot.Epoch == nil || !snapshot.Epoch.IsUint64() || snapshot.Epoch.Uint64() < lag {
		return
	}
	source := snapshot.Epoch.Uint64() - lag
	if self.depositUsagePruned && self.depositUsagePrunedEpoch == source {
		return
	}
	if _, err := os.Stat(filepath.Join(self.cfg.StateDir, "deposit-usage-ledger")); err != nil {
		return
	}
	if err := pruneDepositUsageObjectsV2(ctx, self.cfg.StateDir, source); err != nil {
		fmt.Printf("release steer: deposit usage retention: %v\n", err)
		return
	}
	self.depositUsagePruned, self.depositUsagePrunedEpoch = true, source
}

// releaseUsageRootDeadlineContext is the last block STSettlement2 accepts a source
// epoch's commit at: end(e) + policyAt(e).rootCommitWindowBlocks
// (STSettlement2._requireRootWindow), read at the pinned decision block.
func (self *ChainClient) releaseUsageRootDeadlineContext(ctx context.Context, block uint64, hash [32]byte, sourceEpoch, sourceEnd uint64) (uint64, error) {
	policy, err := chainViewAtHashContext(ctx, self, block, hash, self.coordinator.PackPolicyAt(new(big.Int).SetUint64(sourceEpoch)), self.coordinator.UnpackPolicyAt)
	if err != nil {
		return 0, fmt.Errorf("source epoch %d policy: %w", sourceEpoch, err)
	}
	if policy.RootCommitWindowBlocks > math.MaxUint64-sourceEnd {
		return 0, fmt.Errorf("source epoch %d root window overflows", sourceEpoch)
	}
	return sourceEnd + policy.RootCommitWindowBlocks, nil
}

// readReleaseDecisionUsageCommitsV2 is replay's reading of what live steering read
// about a v2 source epoch. A decision with no signed priced entry reads nothing.
func readReleaseDecisionUsageCommitsV2(ctx context.Context, chain *ChainClient, query releaseDecisionChainV2Query, observed *releaseDecisionChainV2Observation) error {
	usageV2 := query.settlementV2
	if query.priceEntry == nil || !usageV2.configured() || releaseEffectiveDepositPolicy(query.priceEntry, query.policy.Deposit).IsZeroPrice() {
		return ctx.Err()
	}
	for index := range observed.operators {
		operator := &observed.operators[index]
		if !operator.version.Active {
			continue
		}
		settled, err := chain.releaseUsageSettledV2Context(ctx, usageV2, observed.finalized, observed.finalizedHash, operator.noID, observed.sourceEpoch)
		if err != nil {
			return err
		}
		if !settled {
			continue
		}
		operator.usageSettled, operator.usageVault = true, usageV2.Vault
		decisionHash, err := parseReleaseHex32("decision block hash", query.boundary.EVMBlockHash, false)
		if err != nil {
			return err
		}
		if operator.usageDeadline, err = chain.releaseUsageRootDeadlineContext(ctx, query.boundary.EVMBlock, decisionHash, observed.sourceEpoch, observed.sourceEnd); err != nil {
			return err
		}
		operator.usageCommit, err = chain.readDepositUsageCommitV2(ctx, usageV2.Vault, operator.noID, observed.sourceEpoch, observed.sourceEnd, min(operator.usageDeadline, query.boundary.EVMBlock))
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// depositUsageUnavailableStatus is the status of what has not appeared yet: pending
// until the root window closes, then unavailable.
func depositUsageUnavailableStatus(observedAt, deadline uint64) string {
	if observedAt > deadline {
		return DepositAuditUnavailable
	}
	return DepositAuditUnavailablePending
}

// uncommittedDepositUsageAudit is the audit of a v2 source epoch with no commit.
func uncommittedDepositUsageAudit(epoch, sourceEpoch, noID uint64, observed, conviction *big.Int, status string) DepositAudit {
	audit := FailedDepositAudit(epoch, sourceEpoch, noID, observed, conviction, status, errors.New("source ledger root is not committed on chain"))
	audit.UsageEvidence = usage.EvidenceKind
	return audit
}

// depositUsageRequest is what one audit of a committed v2 source epoch is given.
type depositUsageRequest struct {
	expectation       DepositUsageExpectation
	epoch             uint64
	observedDeposit   *big.Int
	convictionBefore  *big.Int
	policy            protocol.DepositPolicy
	unavailableStatus string
}

func newDepositUsageRequest(cfg *ReleaseConfig, vault, signer, rootSigner common.Address, commit *releaseDepositUsageCommit, noID, epoch, sourceEpoch, sourceStart uint64, sourceStartHash [32]byte, sourceEnd uint64, sourceEndHash [32]byte, observed, conviction *big.Int, policy protocol.DepositPolicy, unavailableStatus string) depositUsageRequest {
	return depositUsageRequest{
		expectation: DepositUsageExpectation{
			DeploymentID: cfg.DeploymentID, ChainID: cfg.ChainID, GenesisHash: cfg.GenesisHash, Netuid: cfg.Netuid,
			Coordinator: common.HexToAddress(cfg.Coordinator), VaultV2: vault, PolicyHash: cfg.PolicyHash,
			Epoch: sourceEpoch, NoID: noID, Signer: signer,
			Start: payoutartifact.Boundary{Number: sourceStart, Hash: releaseHex32(sourceStartHash)},
			End:   payoutartifact.Boundary{Number: sourceEnd, Hash: releaseHex32(sourceEndHash)},
			Root:  commit.root, LedgerHash: commit.ledgerHash, RootSigner: rootSigner, CommitBlock: commit.block,
		},
		epoch: epoch, observedDeposit: observed, convictionBefore: conviction, policy: policy, unavailableStatus: unavailableStatus,
	}
}

// releaseDepositUsageGather is one operator's pass through gatherDepositUsageAudit.
type releaseDepositUsageGather struct {
	snapshot         *ReleaseSnapshot
	measurement      *ReleaseMeasurementContext
	vault            common.Address
	noID             uint64
	epoch            uint64
	sourceEpoch      uint64
	sourceStart      uint64
	sourceEnd        uint64
	sourceStartHash  [32]byte
	sourceEndHash    [32]byte
	deposit          *big.Int
	convictionBefore *big.Int
	policy           protocol.DepositPolicy
}

// gatherDepositUsageAudit is live steering's audit of a v2 source epoch. Its window
// is the source epoch's own, as STSettlement2 applies it.
func (self *ReleaseSteerer) gatherDepositUsageAudit(ctx context.Context, in releaseDepositUsageGather) (DepositAudit, error) {
	block, hash := in.snapshot.BlockNumber, in.snapshot.BlockHash
	deadline, err := self.chain.releaseUsageRootDeadlineContext(ctx, block, hash, in.sourceEpoch, in.sourceEnd)
	if err != nil {
		return DepositAudit{}, err
	}
	status := depositUsageUnavailableStatus(block, deadline)
	commit, err := self.chain.readDepositUsageCommitV2(ctx, in.vault, in.noID, in.sourceEpoch, in.sourceEnd, min(deadline, block))
	if err != nil {
		return DepositAudit{}, err
	}
	if commit == nil {
		audit := uncommittedDepositUsageAudit(in.epoch, in.sourceEpoch, in.noID, in.deposit, in.convictionBefore, status)
		audit.ArtifactDeadlineBlock = deadline
		return audit, nil
	}
	if in.measurement.depositUsage == nil {
		return DepositAudit{}, errors.New("a usage statement can only be retained by the compact pool gather")
	}
	source, err := self.chain.ReleaseOperatorAtHashContext(ctx, block, hash, new(big.Int).SetUint64(in.noID), new(big.Int).SetUint64(in.sourceEpoch))
	if err != nil {
		return DepositAudit{}, fmt.Errorf("source operator version: %w", err)
	}
	operator, ok := self.operators[in.noID]
	if !ok {
		return DepositAudit{}, errors.New("usage statement signer is not configured")
	}
	audit, err := in.measurement.depositUsage(ctx, newDepositUsageRequest(self.cfg, in.vault, common.HexToAddress(operator.ArtifactSigner), source.RootSigner, commit, in.noID, in.epoch, in.sourceEpoch, in.sourceStart, in.sourceStartHash, in.sourceEnd, in.sourceEndHash, in.deposit, in.convictionBefore, in.policy, status))
	if err != nil {
		return DepositAudit{}, err
	}
	audit.ArtifactDeadlineBlock = deadline
	return audit, nil
}

// depositUsageObjectRead returns a ledger object of at most limit bytes. ahead is the
// fewest bytes it and the rest of its list must hold, which live steering checks
// against the root's byte budget before reading anything of the list. observed says
// why the object was not obtained, a fact about the operator; err is the validator's
// own failure.
type depositUsageObjectRead func(ctx context.Context, ref ledger.Ref, limit, ahead uint64) (data []byte, observed error, err error)

// depositUsageChunksAhead is the fewest bytes chunks index to count-1 of a valid list
// hold: every chunk but the last is full, and the last holds a row.
func depositUsageChunksAhead(index, count int) uint64 {
	return uint64(count-1-index)*depositUsageFullChunkBytes + ledger.ProviderRowBytes
}

// checkDepositUsageChunk applies the list shape a verifier checks (ledger's
// DecodeProviderChunks) to chunk index of count.
func checkDepositUsageChunk(length, index, count int) error {
	return checkDepositUsageChunkOf(length, ledger.ProviderRowBytes, index, count)
}

// Every census chunk but a list's last.
const depositUsageFullCensusChunkBytes = ledger.ChunkRows * ledger.CensusRowBytes

// The fewest bytes census chunks index to count-1 of a valid list hold, as
// depositUsageChunksAhead counts a provider chunk list.
func depositUsageCensusChunksAhead(index, count int) uint64 {
	return uint64(count-1-index)*depositUsageFullCensusChunkBytes + ledger.CensusRowBytes
}

// Applies the list shape a verifier checks to chunk index of count, of rows
// rowBytes long: whole rows, full chunks but a non-empty last.
func checkDepositUsageChunkOf(length, rowBytes, index, count int) error {
	full := ledger.ChunkRows * rowBytes
	if length%rowBytes != 0 || length > full {
		return fmt.Errorf("%w: chunk %d has %d bytes", ledger.ErrChunkLength, index, length)
	}
	if index < count-1 && length != full {
		return fmt.Errorf("%w: chunk %d of %d has %d rows", ledger.ErrChunkShort, index, count, length/rowBytes)
	}
	if index == count-1 && length == 0 {
		return fmt.Errorf("%w: chunk %d", ledger.ErrChunkEmpty, index)
	}
	return nil
}

// depositUsageRowSums applies the rules a verifier can check (VERSION2-FINAL.md §6.2)
// to an epoch's rows as they arrive, a chunk at a time, and sums them. Rows that
// break them are not the epoch's usage. eligible is W_k, the usage of the rows
// with a coldkey that are not head excluded (ledger.EligibleUsage); it never
// exceeds the total, so it cannot overflow.
type depositUsageRowSums struct {
	last     [16]byte
	rows     DepositUsageRows
	eligible uint64
}

func (self *depositUsageRowSums) add(rows []protocol.LedgerUsageRow) error {
	for i := range rows {
		row, index := &rows[i], self.rows.Count
		switch {
		case row.ClientID == ([16]byte{}):
			return fmt.Errorf("%w: row %d", ledger.ErrRowZeroClientID, index)
		case row.UsageBytes == 0:
			return fmt.Errorf("%w: row %d", ledger.ErrRowZeroUsage, index)
		case index > 0 && bytes.Equal(self.last[:], row.ClientID[:]):
			return fmt.Errorf("%w: row %d", ledger.ErrRowDuplicate, index)
		case index > 0 && bytes.Compare(self.last[:], row.ClientID[:]) > 0:
			return fmt.Errorf("%w: row %d", ledger.ErrRowsUnsorted, index)
		case row.UsageBytes > math.MaxUint64-self.rows.UsageBytes:
			return fmt.Errorf("%w: row %d", ledger.ErrUsageOverflow, index)
		}
		self.last = row.ClientID
		self.rows.Count++
		self.rows.UsageBytes += row.UsageBytes
		if row.Coldkey != ([32]byte{}) && !row.HeadExcluded {
			self.eligible += row.UsageBytes
		}
	}
	return nil
}

// depositUsageVerdict is why an audit has no usage to judge: its status and cause.
// extenders says the committed header it came from carries an extender component,
// so its statement would have been version 2.
type depositUsageVerdict struct {
	status    string
	cause     error
	extenders bool
}

// depositUsageObjectVerdict is the verdict of an object that was not obtained. One
// already held that is larger than its role allows is the operator's malformed
// ledger; any other failure is pending until the root window closes.
func depositUsageObjectVerdict(role string, observed error, unavailable string) *depositUsageVerdict {
	if errors.Is(observed, errDepositUsageHeldOversize) {
		return &depositUsageVerdict{status: DepositAuditInvalid, cause: fmt.Errorf("%s: %w", role, observed)}
	}
	return &depositUsageVerdict{status: unavailable, cause: fmt.Errorf("%s: %w", role, observed)}
}

// readDepositUsageRows reads and verifies a source epoch's committed rows: the header
// the commit names, its chunks for the epoch, and the rules a verifier can check, and,
// when the header gives the epoch an extender component, its census and share-row
// chunks (readDepositUsageExtenders). It holds one provider chunk at a time. A verdict
// is the operator's; an error is the validator's own failure.
func readDepositUsageRows(ctx context.Context, expectation *DepositUsageExpectation, objects depositUsageObjectRead, unavailable string) (depositUsageVerified, *depositUsageVerdict, error) {
	var none depositUsageVerified
	invalid := func(cause error) *depositUsageVerdict {
		return &depositUsageVerdict{status: DepositAuditInvalid, cause: cause}
	}
	encoded, observed, err := objects(ctx, ledger.Ref(expectation.LedgerHash), ledger.MaxHeaderBytes, 0)
	if err != nil {
		return none, nil, err
	}
	if observed != nil {
		return none, depositUsageObjectVerdict("committed ledger header", observed, unavailable), nil
	}
	header, err := ledger.DecodeHeader(encoded)
	if err != nil {
		return none, invalid(fmt.Errorf("committed ledger header: %w", err)), nil
	}
	if header.NoID != expectation.NoID || header.Epoch != expectation.Epoch {
		return none, invalid(errors.New("committed ledger header names another pool or epoch")), nil
	}
	var covered *ledger.HeaderEpoch
	for index := range header.Epochs {
		if header.Epochs[index].Epoch != expectation.Epoch {
			continue
		}
		if covered != nil {
			return none, invalid(errors.New("committed ledger header lists the source epoch more than once")), nil
		}
		covered = &header.Epochs[index]
	}
	if covered == nil {
		return none, invalid(errors.New("committed ledger header does not cover the source epoch")), nil
	}
	if covered.Null {
		return none, invalid(errors.New("committed ledger header declares the source epoch unrecoverable")), nil
	}
	// From here a verdict is about an epoch whose header says which statement it
	// has.
	extenders := covered.Extenders != nil
	verdict := func(verdict *depositUsageVerdict) *depositUsageVerdict {
		verdict.extenders = extenders
		return verdict
	}
	verified := depositUsageVerified{chunks: make([]string, len(covered.Providers))}
	for index, ref := range covered.Providers {
		verified.chunks[index] = ref.String()
	}
	var sums depositUsageRowSums
	for index, ref := range covered.Providers {
		chunk, observed, err := objects(ctx, ref, depositUsageFullChunkBytes, depositUsageChunksAhead(index, len(covered.Providers)))
		if err != nil {
			return none, nil, err
		}
		if observed != nil {
			return none, verdict(depositUsageObjectVerdict("committed provider chunk", observed, unavailable)), nil
		}
		if err := checkDepositUsageChunk(len(chunk), index, len(covered.Providers)); err != nil {
			return none, verdict(invalid(fmt.Errorf("committed provider rows: %w", err))), nil
		}
		rows, err := ledger.DecodeProviderChunks([][]byte{chunk})
		if err != nil {
			return none, verdict(invalid(fmt.Errorf("committed provider rows: chunk %d: %w", index, err))), nil
		}
		if err := sums.add(rows); err != nil {
			return none, verdict(invalid(fmt.Errorf("committed provider rows: %w", err))), nil
		}
	}
	verified.rows = sums.rows
	if extenders {
		committed, failed, err := readDepositUsageExtenders(ctx, covered, sums.eligible, objects, unavailable)
		if err != nil {
			return none, nil, err
		}
		if failed != nil {
			return none, verdict(failed), nil
		}
		verified.extenders = committed
	}
	return verified, nil, nil
}

// Reads and verifies a covered epoch's extender component (WHITEPAPER.md
// §8.6.4) as its statement must carry it, after its provider rows: the census
// chunks, their shape and the census rules (ledger.CheckCensusRows), then the
// share-row chunks, their shape and the rules provider rows follow. The share
// rows must sum to Z' = min(Z, W_k) with eligible as W_k, and to 0 with no
// eligible extender: the cap the ledger's split applies. Whether they are
// exactly the split is check 11 of the settlement v2 audit. A verdict is the
// operator's; an error is the validator's own failure.
func readDepositUsageExtenders(ctx context.Context, covered *ledger.HeaderEpoch, eligible uint64, objects depositUsageObjectRead, unavailable string) (*usage.Extenders, *depositUsageVerdict, error) {
	invalid := func(cause error) *depositUsageVerdict {
		return &depositUsageVerdict{status: DepositAuditInvalid, cause: cause}
	}
	component := covered.Extenders
	var census []ledger.CensusRow
	for index, ref := range component.Census {
		chunk, observed, err := objects(ctx, ref, depositUsageFullCensusChunkBytes, depositUsageCensusChunksAhead(index, len(component.Census)))
		if err != nil {
			return nil, nil, err
		}
		if observed != nil {
			return nil, depositUsageObjectVerdict("committed census chunk", observed, unavailable), nil
		}
		if err := checkDepositUsageChunkOf(len(chunk), ledger.CensusRowBytes, index, len(component.Census)); err != nil {
			return nil, invalid(fmt.Errorf("committed census rows: %w", err)), nil
		}
		rows, err := ledger.DecodeCensusChunks([][]byte{chunk})
		if err != nil {
			return nil, invalid(fmt.Errorf("committed census rows: chunk %d: %w", index, err)), nil
		}
		census = append(census, rows...)
	}
	if err := ledger.CheckCensusRows(census); err != nil {
		return nil, invalid(fmt.Errorf("committed census rows: %w", err)), nil
	}
	var shares []protocol.LedgerUsageRow
	var sums depositUsageRowSums
	for index, ref := range component.Rows {
		chunk, observed, err := objects(ctx, ref, depositUsageFullChunkBytes, depositUsageChunksAhead(index, len(component.Rows)))
		if err != nil {
			return nil, nil, err
		}
		if observed != nil {
			return nil, depositUsageObjectVerdict("committed extender share chunk", observed, unavailable), nil
		}
		if err := checkDepositUsageChunk(len(chunk), index, len(component.Rows)); err != nil {
			return nil, invalid(fmt.Errorf("committed extender share rows: %w", err)), nil
		}
		rows, err := ledger.DecodeProviderChunks([][]byte{chunk})
		if err != nil {
			return nil, invalid(fmt.Errorf("committed extender share rows: chunk %d: %w", index, err)), nil
		}
		if err := sums.add(rows); err != nil {
			return nil, invalid(fmt.Errorf("committed extender share rows: %w", err)), nil
		}
		shares = append(shares, rows...)
	}
	committed, err := usage.CommittedExtenders(component, census, shares)
	if err != nil {
		return nil, invalid(fmt.Errorf("committed extender share rows: %w", err)), nil
	}
	var weight uint64
	if committed.Eligible != 0 {
		weight = min(committed.Bytes, eligible)
	}
	if committed.Weight != weight {
		return nil, invalid(fmt.Errorf("committed extender share rows sum to %d bytes, not the %d of the capped extender pool", committed.Weight, weight)), nil
	}
	return committed, nil, ctx.Err()
}

// evaluateDepositUsageSources is one audit of a committed v2 source epoch, the same
// whether its sources are live or retained. Its only errors are the validator's
// own; everything the operator did or failed to do is the audit's verdict. Live
// steering passes its memo, so only an epoch's first pass reads the committed rows;
// replay passes none and verifies them from its evidence.
func evaluateDepositUsageSources(ctx context.Context, request depositUsageRequest, objects depositUsageObjectRead, reader *HTTPArtifactReader, memo *releaseDepositUsageMemo) (DepositAudit, error) {
	expectation := request.expectation
	// The kind of statement the epoch has follows its committed header: version 2
	// once the header shows an extender component, else version 1.
	extenders := false
	failed := func(status string, cause error) (DepositAudit, error) {
		audit := usageDepositAudit(request.epoch, &expectation, request.observedDeposit, request.convictionBefore)
		audit.Status, audit.Error, audit.UsageEvidence = status, depositUsageLabel(cause.Error()), usage.KindOf(extenders)
		return audit, ctx.Err()
	}
	key := depositUsageMemoKey{noID: expectation.NoID, epoch: expectation.Epoch, ledgerHash: expectation.LedgerHash}
	verified, known := memo.get(key)
	if !known {
		read, verdict, err := readDepositUsageRows(ctx, &expectation, objects, request.unavailableStatus)
		if err != nil {
			return DepositAudit{}, err
		}
		if verdict != nil {
			extenders = verdict.extenders
			return failed(verdict.status, verdict.cause)
		}
		verified = read
		memo.put(key, verified)
	}
	expectation.ProviderChunks, expectation.Extenders = verified.chunks, verified.extenders
	extenders = verified.extenders != nil
	statements, status, err := reader.readDepositUsageStatements(ctx, expectation.Epoch, expectation.NoID, usage.KindOf(extenders), request.unavailableStatus)
	if status != "" {
		return failed(status, err)
	}
	statement, status, err := selectDepositUsageStatement(statements, &expectation)
	if status != "" {
		return failed(status, err)
	}
	if statement == nil {
		return failed(request.unavailableStatus, errors.New("no usage statement names the committed provider chunks"))
	}
	return EvaluateDepositUsage(statement, verified.rows, &expectation, request.epoch, request.observedDeposit, request.convictionBefore, request.policy), ctx.Err()
}

type depositUsageHistoryObject struct {
	Key  string `json:"key"`
	Size int64  `json:"size"`
}

type depositUsageHistory struct {
	Schema    string                      `json:"schema"`
	Objects   []depositUsageHistoryObject `json:"objects"`
	More      bool                        `json:"more"`
	NextAfter string                      `json:"next_after"`
}

// depositUsageEnvelope is the operator's public evidence container. The statement's
// own signature authenticates what the audit uses; the envelope only routes it.
type depositUsageEnvelope struct {
	Schema       string          `json:"schema"`
	DeploymentID string          `json:"deployment_id"`
	ChainID      uint64          `json:"chain_id"`
	GenesisHash  string          `json:"genesis_hash"`
	Netuid       uint16          `json:"netuid"`
	Kind         string          `json:"kind"`
	RunID        string          `json:"run_id,omitempty"`
	CreatedAt    string          `json:"created_at"`
	Payload      json.RawMessage `json:"payload"`
	Signer       common.Address  `json:"signer"`
	ContentHash  string          `json:"content_hash"`
	Signature    string          `json:"signature"`
}

// Reads the operator's statements of one epoch under one evidence kind,
// distinct by their bytes, in the order of its history. A nonempty status is
// why there are none to choose from, with its cause.
func (self *HTTPArtifactReader) readDepositUsageStatements(ctx context.Context, epoch, noId uint64, kind, unavailable string) ([]*usage.Statement, string, error) {
	runId := usage.RunID(noId, epoch)
	endpoint := self.endpoint("/sn/evidence/history", url.Values{
		"deployment_id": []string{self.deploymentID},
		"netuid":        []string{fmt.Sprint(self.netuid)},
		"kind":          []string{kind},
		"run_id":        []string{runId},
		"limit":         []string{fmt.Sprint(maximumExactArtifactObjects + 1)},
	})
	value, err := self.get(ctx, endpoint, maximumUsageHistoryBytes)
	if err != nil {
		return nil, unavailable, fmt.Errorf("usage statement history: %w", err)
	}
	var history depositUsageHistory
	if err := decodeAttemptStreamV2JSON(value, maximumUsageHistoryBytes, &history); err != nil {
		return nil, DepositAuditInvalid, fmt.Errorf("usage statement history integrity: %w", err)
	}
	if history.Schema != releaseUsageHistorySchema {
		return nil, DepositAuditInvalid, fmt.Errorf("unsupported usage statement history schema %q", history.Schema)
	}
	if history.More || len(history.Objects) > maximumExactArtifactObjects {
		return nil, DepositAuditEquivocation, fmt.Errorf("usage statement history holds more than %d objects", maximumExactArtifactObjects)
	}
	if history.NextAfter != "" {
		return nil, DepositAuditInvalid, errors.New("usage statement history has an unexpected continuation cursor")
	}
	if len(history.Objects) == 0 {
		return nil, unavailable, errors.New("no usage statement is published")
	}
	suffix := fmt.Sprintf("/st/v1/evidence/history/%s/%d/%s/%s/", self.deploymentID, self.netuid, kind, runId)
	seen := make(map[string]bool, len(history.Objects))
	statements := make([]*usage.Statement, 0, len(history.Objects))
	for _, object := range history.Objects {
		name := strings.TrimSuffix(filepath.Base(object.Key), ".json")
		contentHash := "sha256:" + name
		if _, err := parseReleaseContentHash(contentHash); err != nil || object.Size < 0 || !strings.HasSuffix("/"+filepath.ToSlash(object.Key), suffix+name+".json") {
			return nil, DepositAuditInvalid, errors.New("usage statement history object is outside the requested identity")
		}
		value, err := self.get(ctx, self.endpoint("/sn/evidence", url.Values{"hash": []string{contentHash}}), maximumUsageEnvelopeBytes)
		if err != nil {
			return nil, unavailable, fmt.Errorf("usage statement: %w", err)
		}
		if object.Size != int64(len(value)) {
			return nil, DepositAuditInvalid, errors.New("usage statement size does not match its history")
		}
		statement, err := self.decodeDepositUsageEnvelope(value, contentHash, kind, runId)
		if err != nil {
			return nil, DepositAuditInvalid, err
		}
		encoded, err := statement.Bytes()
		if err != nil {
			return nil, DepositAuditInvalid, err
		}
		if identity := ReleaseMeasurementContentHash(encoded); !seen[identity] {
			seen[identity] = true
			statements = append(statements, statement)
		}
	}
	return statements, "", nil
}

// Reads a statement published under kind, which must be the statement's own
// version: a version 1 statement under the version 2 kind, or the reverse, is
// the operator's invalid evidence.
func (self *HTTPArtifactReader) decodeDepositUsageEnvelope(value []byte, contentHash, kind, runId string) (*usage.Statement, error) {
	var envelope depositUsageEnvelope
	if err := decodeAttemptStreamV2JSON(value, maximumUsageEnvelopeBytes, &envelope); err != nil {
		return nil, fmt.Errorf("usage statement envelope: %w", err)
	}
	if envelope.Schema != releaseUsageEnvelopeSchema || envelope.DeploymentID != self.deploymentID || envelope.Netuid != self.netuid || envelope.Kind != kind || envelope.RunID != runId || !strings.EqualFold(envelope.ContentHash, contentHash) {
		return nil, errors.New("usage statement envelope is outside the requested identity")
	}
	statement, err := usage.Decode(envelope.Payload)
	if err != nil {
		return nil, err
	}
	if statement.Kind() != kind {
		return nil, fmt.Errorf("usage statement of schema %s is published as %s", statement.Schema, kind)
	}
	return statement, nil
}

// depositUsageLabel makes operator-controlled text fit a retained exchange, so a
// hostile response is recorded and judged, and cannot stop the validator.
func depositUsageLabel(text string) string {
	text = strings.ToValidUTF8(text, "?")
	if len(text) > maximumUsageExchangeLabel {
		text = strings.ToValidUTF8(text[:maximumUsageExchangeLabel], "")
	}
	return text
}

// depositUsageCountingReader reports every byte read through it.
type depositUsageCountingReader struct {
	reader  io.Reader
	counted func(int)
}

func (self depositUsageCountingReader) Read(p []byte) (int, error) {
	n, err := self.reader.Read(p)
	self.counted(n)
	return n, err
}

// attemptDepositUsageExchange makes one bounded GET. The returned cause is what
// retry judges; the exchange records the response for retention. counted, if set,
// sees every body byte read.
func attemptDepositUsageExchange(ctx context.Context, client *http.Client, endpoint string, maximum int64, counted func(int)) (releaseArtifactHttpExchangeV2, []byte, error) {
	value := releaseArtifactHttpExchangeV2{Url: endpoint, MaximumBytes: maximum, Body: []byte{}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		value.TransportError = depositUsageLabel(err.Error())
		return value, nil, err
	}
	response, err := doReleaseApiHttpRequest(client, request)
	if err != nil {
		value.TransportError = depositUsageLabel(err.Error())
		if response != nil && response.Body != nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				value.CloseError = depositUsageLabel(closeErr.Error())
				err = errors.Join(err, closeErr)
			}
		}
		return value, nil, errors.Join(err, ctx.Err())
	}
	if response == nil || response.Body == nil {
		value.TransportError = "response has no body"
		return value, nil, errors.New("usage exchange response has no body")
	}
	value.Status, value.ContentLength = response.StatusCode, response.ContentLength
	value.ContentType = depositUsageLabel(response.Header.Get("Content-Type"))
	body := io.Reader(response.Body)
	if counted != nil {
		body = depositUsageCountingReader{reader: response.Body, counted: counted}
	}
	limit := maximum
	if response.StatusCode != http.StatusOK {
		limit = min(limit, depositUsageRefusalBodyBytes)
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	var readErr, statusErr error
	if err != nil {
		value.BodyError = depositUsageLabel(err.Error())
		readErr = &url.Error{Op: "Read", URL: endpoint, Err: err}
		data = nil
	}
	closeErr := response.Body.Close()
	if closeErr != nil {
		value.CloseError = depositUsageLabel(closeErr.Error())
	}
	if response.StatusCode != http.StatusOK {
		statusErr = &releaseHttpGetStatusError{endpoint: endpoint, status: response.StatusCode, retryAfter: attemptStreamHttpRetryAfter(response.Header)}
		data = nil
	} else if readErr == nil && int64(len(data)) > maximum {
		value.BodyError = depositUsageResponseExceeds(maximum)
		data = nil
	}
	if ctx.Err() != nil && value.TransportError == "" && value.BodyError == "" {
		value.TransportError = ctx.Err().Error()
	}
	return value, data, errors.Join(readErr, closeErr, statusErr, ctx.Err())
}

func depositUsageResponseExceeds(maximum int64) string {
	return fmt.Sprintf("response exceeds %d bytes", maximum)
}

// depositUsageReadClock bounds the time one audit spends on the operator's answers:
// its ledger objects, statement history and statements, with retries and the
// operator's requested waits.
type depositUsageReadClock struct {
	reads    context.Context
	cancel   context.CancelFunc
	deadline time.Time
	hooks    releaseHttpGetRetryHooks
}

// newDepositUsageReadClock gives one audit its share, within what the pass has left.
func newDepositUsageReadClock(ctx context.Context, share time.Duration, hooks releaseHttpGetRetryHooks) *depositUsageReadClock {
	if share <= 0 {
		share = depositUsageReadTimeLimit
	}
	reads, cancel := context.WithTimeout(ctx, share)
	deadline, _ := reads.Deadline()
	clock := &depositUsageReadClock{reads: reads, cancel: cancel, deadline: deadline}
	wait := hooks.wait
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	clock.hooks = releaseHttpGetRetryHooks{withTimeout: hooks.withTimeout, wait: func(ctx context.Context, delay time.Duration) error {
		// The operator's pacing is honoured only within the budget: a wait the
		// budget cannot hold ends the read now.
		if delay >= time.Until(clock.deadline) {
			return errDepositUsageReadTimeSpent
		}
		return wait(ctx, delay)
	}}
	return clock
}

func (self *depositUsageReadClock) spent() bool {
	return evidenceReadContextError(self.reads) != nil
}

// depositUsageReadStop says what ended a read before its answer: the audit's time,
// or the root's bytes. Neither is a fact about the operator's answer.
type depositUsageReadStop uint8

const (
	depositUsageReadAnswered depositUsageReadStop = iota
	depositUsageReadOutOfTime
	depositUsageReadOutOfBytes
)

// get is one operator read within the audit's budget, retrying transient failures.
// maximum is each attempt's byte bound, asked again before every attempt, so bytes
// earlier attempts read count against later ones; none left ends the read. The
// pass's own stop is an error. counted, if set, sees every body byte read.
func (self *depositUsageReadClock) get(pass context.Context, client *http.Client, endpoint string, maximum func() int64, counted func(int)) (releaseArtifactHttpExchangeV2, []byte, depositUsageReadStop, error) {
	if err := pass.Err(); err != nil {
		return releaseArtifactHttpExchangeV2{}, nil, depositUsageReadAnswered, err
	}
	if self.spent() {
		return releaseArtifactHttpExchangeV2{}, nil, depositUsageReadOutOfTime, nil
	}
	var value releaseArtifactHttpExchangeV2
	var data []byte
	var lastCause error
	outOfBytes := false
	err := retryReleaseHttpGet(self.reads, func(attemptCtx context.Context) error {
		limit := maximum()
		if limit <= 0 {
			outOfBytes = true
			return errDepositUsageByteBudgetSpent
		}
		value, data, lastCause = attemptDepositUsageExchange(attemptCtx, client, endpoint, limit, counted)
		return lastCause
	}, self.hooks)
	if passErr := pass.Err(); passErr != nil {
		return releaseArtifactHttpExchangeV2{}, nil, depositUsageReadAnswered, errors.Join(err, passErr)
	}
	if outOfBytes {
		return releaseArtifactHttpExchangeV2{}, nil, depositUsageReadOutOfBytes, nil
	}
	if errors.Is(err, errDepositUsageReadTimeSpent) || self.spent() {
		return releaseArtifactHttpExchangeV2{}, nil, depositUsageReadOutOfTime, nil
	}
	if value.Url == "" || !releaseOnlyErrors(err, lastCause, context.DeadlineExceeded) {
		return value, nil, depositUsageReadAnswered, err
	}
	return value, data, depositUsageReadAnswered, nil
}

// interpretDepositUsageObjectExchange says why a retained object read failed. Only
// a failed read is retained: a held object is its own evidence.
func interpretDepositUsageObjectExchange(value releaseArtifactHttpExchangeV2) error {
	switch {
	case value.TransportError != "":
		return errors.New(value.TransportError)
	case value.Status != http.StatusOK && value.Status != 0:
		return fmt.Errorf("%s returned HTTP %d", value.Url, value.Status)
	case value.BodyError == depositUsageHeldOversize:
		return fmt.Errorf("%s: %w", value.Url, errDepositUsageHeldOversize)
	case value.BodyError != "":
		return fmt.Errorf("%s: %s", value.Url, value.BodyError)
	case value.CloseError != "":
		return errors.New(value.CloseError)
	}
	return fmt.Errorf("%s was not obtained", value.Url)
}

// objectURL is the address Fetch reads ref at.
func (self *settlementV2Objects) objectURL(ref ledger.Ref) string {
	endpoint := *self.endpoint
	endpoint.RawQuery = url.Values{"hash": {ref.String()}}.Encode()
	return endpoint.String()
}

// releaseUsageObservationMaximum bounds a retained observation so its public audit
// payload always fits. The payload encodes it again as base64, so an oversized
// response from one operator can never stop publication for every operator.
func releaseUsageObservationMaximum(bounds ReleaseEvidenceV2Bounds) uint64 {
	var public uint64
	if bounds.MaxTransitionBytes > usageObservationPayloadReserve {
		public = (bounds.MaxTransitionBytes - usageObservationPayloadReserve) / 4 * 3
	}
	return min(bounds.MaxArtifactBytes, bounds.MaxControlBytes/8, public)
}

// releaseUsageHttpRequestV2 is the slot of one retained usage observation, beside
// the payout artifact observations and bounded with them.
func releaseUsageHttpRequestV2(cfg *ReleaseConfig, hotkey [32]byte, decision *ReleaseMeasurementArtifact, noID, sourceEpoch uint64, reader *HTTPArtifactReader) (releaseArtifactHttpObservationV2, string, error) {
	value, _, err := releaseArtifactHttpRequestV2(cfg, hotkey, decision, noID, sourceEpoch, reader)
	if err != nil {
		return value, "", err
	}
	value.Schema = releaseUsageHttpObservationSchemaV2
	encoded, err := json.Marshal(value)
	if err != nil {
		return value, "", err
	}
	hash := sha256.Sum256(append([]byte("urnetwork-validator-usage-http-slot-v2\x00"), encoded...))
	return value, filepath.Join(cfg.StateDir, "artifact-http-observations", hex.EncodeToString(hash[:])+".json"), nil
}

// releaseDepositObservationRequestV2 selects the slot of an audit's retained
// observation: a usage statement's or a payout artifact's.
func releaseDepositObservationRequestV2(cfg *ReleaseConfig, hotkey [32]byte, decision *ReleaseMeasurementArtifact, audit DepositAudit, reader *HTTPArtifactReader) (releaseArtifactHttpObservationV2, string, error) {
	if audit.UsageEvidence != "" {
		return releaseUsageHttpRequestV2(cfg, hotkey, decision, audit.NoID, audit.SourceEpoch, reader)
	}
	return releaseArtifactHttpRequestV2(cfg, hotkey, decision, audit.NoID, audit.SourceEpoch, reader)
}

// depositUsageObjects is where the usage audit finds ledger objects: those the ledger
// auditor keeps, and those this audit fetched. It keeps its own apart, so they are
// never held objects to the auditor, whose per-root budget counts what it fetches.
type depositUsageObjects struct {
	shared settlementV2Custody
	own    settlementV2Custody
}

func depositUsageObjectsAt(stateDir string) *depositUsageObjects {
	return &depositUsageObjects{
		shared: settlementV2Custody{directory: filepath.Join(stateDir, "settlement-v2-ledger")},
		own:    settlementV2Custody{directory: filepath.Join(stateDir, "deposit-usage-ledger")},
	}
}

// Object returns an object either custody holds, checked against its reference. The
// auditor's copy is read first, and a copy of this audit's that retention pruned
// because the auditor came to hold it is found there.
func (self *depositUsageObjects) Object(ctx context.Context, ref ledger.Ref) ([]byte, bool, error) {
	data, held, err := self.shared.Object(ctx, ref)
	if err != nil || held {
		return data, held, err
	}
	if data, held, err = self.own.Object(ctx, ref); err != nil || held {
		return data, held, err
	}
	return self.shared.Object(ctx, ref)
}

// Keep stores an object the audit fetched and checked against its reference.
func (self *depositUsageObjects) Keep(ctx context.Context, ref ledger.Ref, data []byte) error {
	return self.own.Keep(ctx, ref, data)
}

// held reads an object a retained audit found held. Replay reads nothing else: an
// object the audit did not obtain was retained as an exchange.
func (self *depositUsageObjects) held(ctx context.Context, ref ledger.Ref, limit uint64) ([]byte, error) {
	data, held, err := self.Object(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !held || uint64(len(data)) > limit {
		return nil, fmt.Errorf("retained ledger object %s is missing or exceeds its bound", ref)
	}
	return data, ctx.Err()
}

// depositUsageRootBytes is one audited root's record. WastedBytes is what its
// fetches read that never became a kept object; kept objects count again on every
// pass as held bytes, so the two carry the root's byte budget across passes and
// restarts. Referenced says a retained observation names the root, so replay can
// reach its objects.
type depositUsageRootBytes struct {
	WastedBytes uint64 `json:"wasted_bytes"`
	Referenced  bool   `json:"referenced,omitempty"`
}

func (self *depositUsageObjects) rootBytesPath(noID, epoch uint64, ledgerHash [32]byte) string {
	return self.own.path("roots", fmt.Sprintf("%d-%d-%s.json", noID, epoch, hex.EncodeToString(ledgerHash[:])))
}

func parseDepositUsageRootName(name string) (epoch uint64, ledgerHash [32]byte, ok bool) {
	var noID uint64
	var encoded string
	if n, err := fmt.Sscanf(strings.TrimSuffix(name, ".json"), "%d-%d-%s", &noID, &epoch, &encoded); err != nil || n != 3 || !strings.HasSuffix(name, ".json") {
		return 0, ledgerHash, false
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil || len(raw) != len(ledgerHash) {
		return 0, ledgerHash, false
	}
	copy(ledgerHash[:], raw)
	return epoch, ledgerHash, true
}

// Adds to keep a root's header and every chunk its header lists for epoch, its
// extender component's census and share-row chunks included: all replay of an
// audit of the root can read.
func (self *depositUsageObjects) listed(ctx context.Context, epoch uint64, ledgerHash [32]byte, keep map[ledger.Ref]bool) error {
	keep[ledger.Ref(ledgerHash)] = true
	data, held, err := self.Object(ctx, ledger.Ref(ledgerHash))
	if err != nil || !held || len(data) > ledger.MaxHeaderBytes {
		return err
	}
	header, err := ledger.DecodeHeader(data)
	if err != nil {
		return ctx.Err()
	}
	for _, covered := range header.Epochs {
		if covered.Epoch == epoch {
			for _, ref := range covered.Providers {
				keep[ref] = true
			}
			if covered.Extenders != nil {
				for _, ref := range covered.Extenders.Census {
					keep[ref] = true
				}
				for _, ref := range covered.Extenders.Rows {
					keep[ref] = true
				}
			}
		}
	}
	return ctx.Err()
}

// pruneDepositUsageObjectsV2 deletes what neither a retained decision nor a later
// pass can reach: copies of objects the ledger auditor now holds, which every reader
// finds in its custody; objects no recorded root lists; and the records of roots of
// past source epochs that no retained observation names. A root a retained
// observation names keeps its record and every object its header lists for its
// epoch for as long as the intent history keeps the decisions, and a root of the
// current source epoch keeps them while its epoch is audited. It runs between
// passes, never during one.
func pruneDepositUsageObjectsV2(ctx context.Context, stateDir string, currentSourceEpoch uint64) error {
	store := depositUsageObjectsAt(stateDir)
	roots, err := os.ReadDir(filepath.Join(store.own.directory, "roots"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	keep := map[ledger.Ref]bool{}
	for _, entry := range roots {
		epoch, ledgerHash, ok := parseDepositUsageRootName(entry.Name())
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(store.own.directory, "roots", entry.Name())
		var record depositUsageRootBytes
		if _, err := store.own.readJSON(path, &record); err != nil {
			return err
		}
		if !record.Referenced && epoch < currentSourceEpoch {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := store.listed(ctx, epoch, ledgerHash, keep); err != nil {
			return err
		}
	}
	objects, err := os.ReadDir(filepath.Join(store.own.directory, "objects"))
	if errors.Is(err, os.ErrNotExist) {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	for _, entry := range objects {
		raw, err := hex.DecodeString(entry.Name())
		if err != nil || len(raw) != len(ledger.Ref{}) || !entry.Type().IsRegular() {
			continue
		}
		ref := ledger.Ref(raw)
		_, auditorHolds, err := store.shared.Object(ctx, ref)
		if err != nil {
			return err
		}
		if auditorHolds || !keep[ref] {
			if err := os.Remove(filepath.Join(store.own.directory, "objects", entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return ctx.Err()
}

// depositUsageObject reads a held ledger object for replay. It is not charged to the
// history byte allowance: provider chunks are far larger than any control document
// and content addressed, so a changed byte is an error, never another audit.
func (self *releaseEvidenceV2StartupReferences) depositUsageObject(ctx context.Context, stateDir string, ref ledger.Ref, limit uint64) ([]byte, error) {
	if self.closed {
		return nil, errors.New("startup reference owner is closed")
	}
	if self.archive != nil {
		data, err := self.archive.read(ctx, ReleaseEvidenceV2CaptureSource{Kind: releaseUsageLedgerObjectCaptureKind, Name: ref.String()}, limit)
		if err != nil {
			return nil, err
		}
		if ledger.RefOf(data) != ref {
			return nil, fmt.Errorf("archived ledger object %s does not match its reference", ref)
		}
		return data, nil
	}
	return depositUsageObjectsAt(stateDir).held(ctx, ref, limit)
}

// replayDepositUsageObservationV2 runs the same audit over one retained observation.
// Every exchange must be consumed in order, and there is no live fallback.
func replayDepositUsageObservationV2(ctx context.Context, reader *HTTPArtifactReader, objects *settlementV2Objects, value releaseArtifactHttpObservationV2, request depositUsageRequest, held func(context.Context, ledger.Ref, uint64) ([]byte, error)) (DepositAudit, error) {
	owned := *reader
	index := 0
	var mismatch error
	owned.observedGet = func(ctx context.Context, endpoint string, maximum int64) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			mismatch = err
			return nil, err
		}
		if index >= len(value.Exchanges) || value.Exchanges[index].Url != endpoint || value.Exchanges[index].MaximumBytes > maximum {
			mismatch = errors.New("retained usage request order or byte bound differs")
			return nil, mismatch
		}
		exchange := value.Exchanges[index]
		index++
		return interpretArtifactHttpExchangeV2(exchange)
	}
	object := func(ctx context.Context, ref ledger.Ref, limit, _ uint64) ([]byte, error, error) {
		if index < len(value.Exchanges) && value.Exchanges[index].Url == objects.objectURL(ref) {
			exchange := value.Exchanges[index]
			index++
			return nil, interpretDepositUsageObjectExchange(exchange), nil
		}
		data, err := held(ctx, ref, limit)
		return data, nil, err
	}
	audit, err := evaluateDepositUsageSources(ctx, request, object, &owned, nil)
	if err != nil {
		return DepositAudit{}, err
	}
	if mismatch != nil || index != len(value.Exchanges) {
		return DepositAudit{}, errors.Join(errors.New("retained usage exchange census differs"), mismatch)
	}
	return audit, ctx.Err()
}

// releaseDepositUsageCaptureV2 retains one operator's usage observation at one
// decision boundary. A retry at that boundary replays it, and manufactures no new
// fact about the operator.
type releaseDepositUsageCaptureV2 struct {
	cfg      *ReleaseConfig
	hotkey   *crv4.Keypair
	decision *ReleaseMeasurementArtifact
	reader   *HTTPArtifactReader
	// What earlier passes verified, and the time this pass's ledger reads may take.
	memo      *releaseDepositUsageMemo
	readLimit time.Duration
}

func (self *releaseDepositUsageCaptureV2) audit(ctx context.Context, request depositUsageRequest) (result DepositAudit, resultErr error) {
	expectation := &request.expectation
	value, path, err := releaseUsageHttpRequestV2(self.cfg, self.hotkey.PublicKey(), self.decision, expectation.NoID, expectation.Epoch, self.reader)
	if err != nil {
		return result, err
	}
	bounds := self.cfg.EvidenceV2.Bounds
	maximum := releaseUsageObservationMaximum(bounds)
	if maximum < usageObservationFramingReserve+32768 {
		return result, errors.New("usage observation has insufficient admitted public bytes")
	}
	objects, err := newSettlementV2Objects(self.reader.baseURL.String())
	if err != nil {
		return result, err
	}
	if !filepath.IsAbs(self.cfg.StateDir) {
		return result, errors.New("usage audit needs an absolute state directory")
	}
	store := depositUsageObjectsAt(self.cfg.StateDir)
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, maximum, releaseMeasurementInputV2ReadHooks{}, true)
	defer func() {
		resultErr = errors.Join(resultErr, owner.finish(), ctx.Err())
		if resultErr != nil {
			result = DepositAudit{}
		}
	}()
	if err != nil {
		return result, err
	}
	if err := unix.Flock(int(owner.directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return result, err
	}
	used, count, err := censusReleaseEvidenceCapturesV2(ctx, owner.directory, bounds.MaxHistoryBytes, bounds.CaptureFileLimit())
	if err != nil {
		return result, err
	}
	encoded, err := owner.read()
	if err == nil {
		value, err = decodeArtifactHttpObservationV2(ctx, encoded, maximum, value)
		if err != nil {
			return result, err
		}
		result, err = replayDepositUsageObservationV2(ctx, self.reader, objects, value, request, store.held)
		if err != nil {
			return result, err
		}
		result.HttpObservationHash = ReleaseMeasurementContentHash(encoded)
		return result, owner.check()
	}
	if !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(err) {
		return result, err
	}
	if count >= bounds.CaptureFileLimit() || used >= bounds.MaxHistoryBytes {
		return result, errors.New("usage observation namespace exceeds its finite history allowance")
	}
	// The history the validator keeps may leave less than a full observation. That
	// is this validator's limit, never the operator's: a read it cuts short stops
	// the pass, as a payout artifact observation's does.
	full := maximum
	maximum = min(maximum, bounds.MaxHistoryBytes-used)
	shrunk := maximum < full
	// Bodies take what the framing leaves. An observation encodes them as base64.
	var remaining uint64
	if maximum > usageObservationFramingReserve {
		remaining = (maximum - usageObservationFramingReserve) / 4 * 3
	}
	clock := newDepositUsageReadClock(ctx, self.readLimit, self.reader.retryHooks)
	defer clock.cancel()
	var captureErr error
	ownedReader := *self.reader
	ownedReader.observedGet = func(_ context.Context, endpoint string, limit int64) ([]byte, error) {
		if len(value.Exchanges) >= maximumUsageExchanges {
			captureErr = errors.New("usage observation exceeds its exact request census")
			return nil, captureErr
		}
		bodyLimit := min(uint64(limit), remaining)
		if bodyLimit < uint64(limit) && shrunk {
			captureErr = errors.New("usage observation: this validator's evidence history cannot hold the operator's answers")
			return nil, captureErr
		}
		var exchange releaseArtifactHttpExchangeV2
		if bodyLimit == 0 {
			exchange = releaseArtifactHttpExchangeV2{Url: endpoint, MaximumBytes: limit, Body: []byte{}, BodyError: "the retained usage observation allowance is spent"}
		} else {
			got, data, stop, err := clock.get(ctx, self.reader.client, endpoint, func() int64 { return int64(bodyLimit) }, nil)
			if err != nil {
				captureErr = err
				return nil, err
			}
			if stop != depositUsageReadAnswered {
				exchange = releaseArtifactHttpExchangeV2{Url: endpoint, MaximumBytes: int64(bodyLimit), Body: []byte{}, TransportError: depositUsageReadTimeSpent}
			} else {
				exchange = got
				if data != nil {
					exchange.Body = data
				}
				remaining -= min(uint64(len(exchange.Body)), remaining)
			}
		}
		value.Exchanges = append(value.Exchanges, exchange)
		return interpretArtifactHttpExchangeV2(exchange)
	}
	// The root's byte budget counts every object of it read, held or fetched, and
	// every byte fetched that never became an object, so a root past it is refused
	// on every pass and fetches nothing more.
	budget := SettlementV2RuntimeOptionsFrom(ctx).LedgerMaxBytes
	if budget == 0 {
		budget = DefaultLedgerMaxBytes
	}
	rootPath := store.rootBytesPath(expectation.NoID, expectation.Epoch, expectation.LedgerHash)
	var rootBytes depositUsageRootBytes
	known, err := store.own.readJSON(rootPath, &rootBytes)
	if err != nil {
		return result, err
	}
	// The record names the root to retention while its epoch is audited.
	if !known {
		if err := store.own.writeJSON(rootPath, rootBytes); err != nil {
			return result, err
		}
	}
	spentBytes := rootBytes.WastedBytes
	waste := func(read uint64) error {
		if read == 0 {
			return nil
		}
		rootBytes.WastedBytes += read
		spentBytes += read
		return store.own.writeJSON(rootPath, rootBytes)
	}
	object := func(_ context.Context, ref ledger.Ref, limit, ahead uint64) ([]byte, error, error) {
		endpoint := objects.objectURL(ref)
		retain := func(exchange releaseArtifactHttpExchangeV2) ([]byte, error, error) {
			value.Exchanges = append(value.Exchanges, exchange)
			return nil, interpretDepositUsageObjectExchange(exchange), nil
		}
		stopped := func(label string) releaseArtifactHttpExchangeV2 {
			return releaseArtifactHttpExchangeV2{Url: endpoint, MaximumBytes: int64(limit), Body: []byte{}, BodyError: label}
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		left := budget - min(spentBytes, budget)
		// What the rest of the list must hold has to fit before any of it is read.
		if ahead > left {
			return retain(stopped(depositUsageByteBudgetSpent))
		}
		data, held, err := store.Object(ctx, ref)
		if err != nil {
			return nil, nil, err
		}
		if held {
			switch {
			case uint64(len(data)) > limit:
				return retain(stopped(depositUsageHeldOversize))
			case uint64(len(data)) > left:
				return retain(stopped(depositUsageByteBudgetSpent))
			}
			spentBytes += uint64(len(data))
			return data, nil, nil
		}
		fetchLimit := min(limit, left)
		if fetchLimit == 0 {
			return retain(stopped(depositUsageByteBudgetSpent))
		}
		// Every byte read, on every attempt, counts against what the root has left.
		var read uint64
		attemptLimit := func() int64 { return int64(min(fetchLimit, left-min(read, left))) }
		exchange, data, stop, err := clock.get(ctx, objects.client, endpoint, attemptLimit, func(n int) { read += uint64(n) })
		if err != nil {
			return nil, nil, errors.Join(err, waste(read))
		}
		if stop == depositUsageReadAnswered && data != nil && ledger.RefOf(data) == ref {
			if err := errors.Join(waste(read-uint64(len(data))), store.Keep(ctx, ref, data)); err != nil {
				return nil, nil, err
			}
			spentBytes += uint64(len(data))
			return data, nil, nil
		}
		if err := waste(read); err != nil {
			return nil, nil, err
		}
		switch {
		case stop == depositUsageReadOutOfTime:
			exchange = releaseArtifactHttpExchangeV2{Url: endpoint, MaximumBytes: int64(limit), Body: []byte{}, TransportError: depositUsageReadTimeSpent}
		case stop == depositUsageReadOutOfBytes:
			exchange = stopped(depositUsageByteBudgetSpent)
		case data != nil:
			exchange.BodyError = "response does not match its reference"
		case exchange.MaximumBytes < int64(limit) && exchange.BodyError == depositUsageResponseExceeds(exchange.MaximumBytes):
			// The budget, not the role's bound, stopped this read.
			exchange = stopped(depositUsageByteBudgetSpent)
		}
		return retain(exchange)
	}
	result, err = evaluateDepositUsageSources(ctx, request, object, &ownedReader, self.memo)
	if err := errors.Join(err, captureErr, ctx.Err()); err != nil {
		return result, err
	}
	digest, err := releaseArtifactHttpObservationDigestV2(value)
	if err != nil {
		return result, err
	}
	signature, err := self.hotkey.Sign(digest[:])
	if err != nil {
		return result, err
	}
	value.Signature = hex.EncodeToString(signature)
	encoded, err = json.Marshal(value)
	if err != nil || uint64(len(encoded)) > maximum {
		return result, errors.Join(errors.New("usage observation exceeds its canonical wire allowance"), err)
	}
	if _, err := decodeArtifactHttpObservationV2(ctx, encoded, maximum, value); err != nil {
		return result, err
	}
	// From here replay can reach this root's objects, so retention keeps them.
	if !rootBytes.Referenced {
		rootBytes.Referenced = true
		if err := store.own.writeJSON(rootPath, rootBytes); err != nil {
			return result, err
		}
	}
	if err := owner.write(encoded); err != nil {
		return result, err
	}
	result.HttpObservationHash = ReleaseMeasurementContentHash(encoded)
	return result, owner.check()
}

// historicalDepositUsageAuditV2 is replay of gatherDepositUsageAudit: the same event
// from the chain, the same objects from custody and the same operator answers from
// the retained observation.
func (self *releaseEvidenceV2StartupHistory) historicalDepositUsageAuditV2(ctx context.Context, observed *releaseDecisionChainV2Observation, operator releaseDecisionChainV2Operator, claimed DepositAudit, decision *ReleaseMeasurementArtifact, custody *releaseEvidenceV2StartupReferences) (DepositAudit, error) {
	epoch := observed.boundary.SettlementEpoch
	status := depositUsageUnavailableStatus(observed.boundary.EVMBlock, operator.usageDeadline)
	if operator.usageCommit == nil {
		if claimed.HttpObservationHash != "" {
			return DepositAudit{}, errors.New("usage audit retains a request for a root the chain never committed")
		}
		audit := uncommittedDepositUsageAudit(epoch, observed.sourceEpoch, operator.noID, operator.deposit, operator.convictionBefore, status)
		audit.ObservedAtBlock, audit.ArtifactDeadlineBlock = observed.boundary.EVMBlock, operator.usageDeadline
		return audit, ctx.Err()
	}
	if custody == nil {
		return DepositAudit{}, errors.New("usage audit replay has no retained custody")
	}
	auditOwnerIndex := len(custody.owners)
	var config *OperatorConfig
	for index := range self.cfg.Operators {
		if self.cfg.Operators[index].NoID == operator.noID {
			config = &self.cfg.Operators[index]
			break
		}
	}
	if config == nil || !common.IsHexAddress(config.ArtifactSigner) {
		return DepositAudit{}, errors.New("retained usage origin or signer is not configured")
	}
	reader, err := NewHTTPArtifactReader(config.APIURL, self.cfg.DeploymentID, self.cfg.Netuid)
	if err != nil {
		return DepositAudit{}, err
	}
	objects, err := newSettlementV2Objects(config.APIURL)
	if err != nil {
		return DepositAudit{}, err
	}
	first := self.initial[self.participants[0].NoID].InitialCut
	expected, path, err := releaseUsageHttpRequestV2(&self.cfg, first.Activation.Hotkey, decision, operator.noID, observed.sourceEpoch, reader)
	if err != nil {
		return DepositAudit{}, err
	}
	maximum := min(self.cfg.EvidenceV2.Bounds.MaxArtifactBytes, self.cfg.EvidenceV2.Bounds.MaxControlBytes/8)
	encoded, err := custody.read(ctx, path, maximum, false)
	if err := releaseRpcObservationError(err, ReleaseMeasurementContentHash(encoded) == claimed.HttpObservationHash, errors.New("retained usage observation hash or custody differs")); err != nil {
		return DepositAudit{}, err
	}
	value, err := decodeArtifactHttpObservationV2(ctx, encoded, maximum, expected)
	if err != nil {
		return DepositAudit{}, err
	}
	request := newDepositUsageRequest(&self.cfg, operator.usageVault, common.HexToAddress(config.ArtifactSigner), operator.sourceVersion.RootSigner, operator.usageCommit, operator.noID, epoch, observed.sourceEpoch, observed.sourceStart, observed.sourceStartHash, observed.sourceEnd, observed.sourceEndHash, operator.deposit, operator.convictionBefore, releaseEffectiveDepositPolicy(observed.priceEntry, self.cfg.Policy.Deposit), status)
	audit, err := replayDepositUsageObservationV2(ctx, reader, objects, value, request, func(ctx context.Context, ref ledger.Ref, limit uint64) ([]byte, error) {
		return custody.depositUsageObject(ctx, self.cfg.StateDir, ref, limit)
	})
	if err != nil {
		return DepositAudit{}, err
	}
	audit.ObservedAtBlock, audit.ArtifactDeadlineBlock, audit.HttpObservationHash = observed.boundary.EVMBlock, operator.usageDeadline, claimed.HttpObservationHash
	return audit, custody.checkSince(auditOwnerIndex)
}

// emitDepositUsageObjectsV2 adds to a capture exactly the ledger objects replay of a
// usage audit reads: it walks the committed rows as replay does, one chunk at a time,
// and stops where replay stops, at the read the audit's retained exchanges say
// failed or at the first rows that break the rules.
func emitDepositUsageObjectsV2(ctx context.Context, stateDir, apiURL string, audit DepositAudit, exchanges []releaseArtifactHttpExchangeV2, emit func(ReleaseEvidenceV2CaptureSource, []byte) error) error {
	if audit.CommittedArtifactHash == "" {
		return ctx.Err()
	}
	ledgerHash, err := parseReleaseHex32("committed ledger hash", audit.CommittedArtifactHash, false)
	if err != nil {
		return err
	}
	objects, err := newSettlementV2Objects(apiURL)
	if err != nil {
		return err
	}
	store := depositUsageObjectsAt(stateDir)
	index := 0
	read := func(ctx context.Context, ref ledger.Ref, limit, _ uint64) ([]byte, error, error) {
		if index < len(exchanges) && exchanges[index].Url == objects.objectURL(ref) {
			exchange := exchanges[index]
			index++
			return nil, interpretDepositUsageObjectExchange(exchange), nil
		}
		data, err := store.held(ctx, ref, limit)
		if err != nil {
			return nil, nil, err
		}
		if err := emit(ReleaseEvidenceV2CaptureSource{Kind: releaseUsageLedgerObjectCaptureKind, Name: ref.String()}, data); err != nil {
			return nil, nil, err
		}
		return data, nil, nil
	}
	expectation := DepositUsageExpectation{NoID: audit.NoID, Epoch: audit.SourceEpoch, LedgerHash: ledgerHash}
	if _, _, err := readDepositUsageRows(ctx, &expectation, read, DepositAuditUnavailablePending); err != nil {
		return err
	}
	return ctx.Err()
}
