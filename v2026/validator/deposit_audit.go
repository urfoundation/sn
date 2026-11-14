package validator

// deposit_audit.go contains the pure, deterministic trust decision between a
// signed prior-epoch payout artifact and the current on-chain operator deposit.
// Any identity, boundary, commitment, tier, or amount mismatch yields zero pool
// weight and remains visible in the immutable steering intent.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/demand/usage"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

const (
	DepositAuditBootstrap          = "bootstrap"
	DepositAuditCompliant          = "compliant"
	DepositAuditMismatch           = "deposit_mismatch"
	DepositAuditUnavailablePending = "artifact_pending"
	DepositAuditUnavailable        = "artifact_unavailable"
	DepositAuditInvalid            = "artifact_invalid"
	DepositAuditEquivocation       = "artifact_equivocation"
	// DepositAuditZeroPrice is the only status under a zero-price policy: no
	// deposit is required or audited, so the pool is eligible from pinned chain
	// state alone (no payout artifact is fetched) in every epoch, including the
	// bootstrap epoch, whatever the operator voluntarily deposited.
	DepositAuditZeroPrice = "zero_price"

	DepositDispositionZeroPrice = "zero_price_no_deposit_required"
)

// DepositAudit is the exact validator evidence for one operator's demand
// signal. Decimal strings preserve uint256 conviction/deposit values. Users
// and the per-user rate are omitted when zero so audits made before the
// per-user rate existed keep their exact bytes.
type DepositAudit struct {
	HttpObservationHash     string `json:"http_observation_hash,omitempty"`
	NoID                    uint64 `json:"no_id"`
	Epoch                   uint64 `json:"epoch"`
	SourceEpoch             uint64 `json:"source_epoch"`
	ArtifactHash            string `json:"artifact_hash,omitempty"`
	CommittedArtifactHash   string `json:"committed_artifact_hash,omitempty"`
	PayoutRoot              string `json:"payout_root,omitempty"`
	ArtifactSigner          string `json:"artifact_signer,omitempty"`
	RootCommitter           string `json:"root_committer,omitempty"`
	RootSigner              string `json:"root_signer,omitempty"`
	SourceStartBlock        uint64 `json:"source_start_block,omitempty"`
	SourceStartHash         string `json:"source_start_hash,omitempty"`
	SourceEndBlock          uint64 `json:"source_end_block,omitempty"`
	SourceEndHash           string `json:"source_end_hash,omitempty"`
	RootCommitBlock         uint64 `json:"root_commit_block,omitempty"`
	ObservedAtBlock         uint64 `json:"observed_at_block"`
	ArtifactDeadlineBlock   uint64 `json:"artifact_deadline_block"`
	UsageBytes              uint64 `json:"usage_bytes"`
	Users                   uint64 `json:"users,omitempty"`
	ConvictionBeforeRao     string `json:"conviction_before_rao"`
	RateNumeratorRaoPerGiB  uint64 `json:"rate_numerator_rao_per_gib"`
	RateNumeratorRaoPerUser uint64 `json:"rate_numerator_rao_per_user,omitempty"`
	RateDenominator         uint64 `json:"rate_denominator"`
	RequiredDepositRao      string `json:"required_deposit_rao"`
	ObservedDepositRao      string `json:"observed_deposit_rao"`
	Status                  string `json:"status"`
	Compliant               bool   `json:"compliant"`
	Disposition             string `json:"disposition"`
	Error                   string `json:"error,omitempty"`
	// UsageEvidence names the operator's usage statement format when the source
	// epoch settled under settlement v2, whose ledger has no payout artifact:
	// usage.EvidenceKindV2 once the committed header gives the epoch an extender
	// component (WHITEPAPER.md §8.6.4), else usage.EvidenceKind. An audit of a
	// payout artifact omits it, so its bytes stay as they were.
	UsageEvidence string `json:"usage_evidence,omitempty"`
}

// DepositArtifactExpectation pins every artifact field that does not come from
// the artifact itself, including its finalized chain commitment.
type DepositArtifactExpectation struct {
	DeploymentID    string
	ChainID         uint64
	GenesisHash     string
	Netuid          uint16
	Coordinator     common.Address
	SettlementVault common.Address
	PolicyHash      string
	Epoch           uint64
	NoID            uint64
	Signer          common.Address
	Start           payoutartifact.Boundary
	End             payoutartifact.Boundary
	PayoutRoot      [32]byte
	ArtifactHash    [32]byte
	Committer       common.Address
	RootSigner      common.Address
	CommitBlock     uint64
}

func baseDepositAudit(epoch, sourceEpoch, noID uint64, observed, conviction *big.Int) DepositAudit {
	audit := DepositAudit{
		NoID: noID, Epoch: epoch, SourceEpoch: sourceEpoch,
		Status: DepositAuditInvalid, Disposition: "zero_pool_weight",
		RequiredDepositRao: "0", ObservedDepositRao: "0", ConvictionBeforeRao: "0",
	}
	if observed != nil {
		audit.ObservedDepositRao = observed.String()
	}
	if conviction != nil {
		audit.ConvictionBeforeRao = conviction.String()
	}
	return audit
}

// FailedDepositAudit records a transport/history failure without turning a
// dishonest or unavailable operator into a validator-wide outage.
func FailedDepositAudit(epoch, sourceEpoch, noID uint64, observed, conviction *big.Int, status string, failure error) DepositAudit {
	audit := baseDepositAudit(epoch, sourceEpoch, noID, observed, conviction)
	audit.Status = status
	if failure != nil {
		audit.Error = failure.Error()
	}
	return audit
}

// ZeroPriceDepositAudit is the audit every active operator receives while the
// policy's published price is zero: required deposit 0, pool weight eligible,
// the observed deposit and conviction recorded but not judged. sourceEpoch is
// the usage epoch the policy lag points at (0 in the bootstrap epoch), which
// keeps the audit reproducible from pinned chain state by any replay.
func ZeroPriceDepositAudit(epoch, sourceEpoch, noID uint64, observed, conviction *big.Int) DepositAudit {
	audit := baseDepositAudit(epoch, sourceEpoch, noID, observed, conviction)
	audit.Status = DepositAuditZeroPrice
	audit.Compliant = true
	audit.Disposition = DepositDispositionZeroPrice
	return audit
}

// depositAuditSourceEpoch is the usage epoch a deposit audit for epoch refers
// to under the policy lag, or 0 in the bootstrap epochs before one exists.
func depositAuditSourceEpoch(epoch uint64, policy protocol.DepositPolicy) uint64 {
	if epoch < policy.UsageLagEpochs {
		return 0
	}
	return epoch - policy.UsageLagEpochs
}

// EvaluateDepositArtifact reconstructs and validates an operator statement,
// then applies the shared exact floor-and-cap formula. Overdepositing is also a
// mismatch: it cannot buy weight beyond the usage the operator signed.
func EvaluateDepositArtifact(
	artifact *payoutartifact.Artifact,
	expectation DepositArtifactExpectation,
	epoch uint64,
	observedDeposit *big.Int,
	convictionBefore *big.Int,
	depositPolicy protocol.DepositPolicy,
) DepositAudit {
	audit := baseDepositAudit(epoch, expectation.Epoch, expectation.NoID, observedDeposit, convictionBefore)
	if observedDeposit == nil || observedDeposit.Sign() < 0 || convictionBefore == nil || convictionBefore.Sign() < 0 {
		audit.Error = "deposit or conviction snapshot is nil or negative"
		return audit
	}
	if err := payoutartifact.Verify(artifact); err != nil {
		audit.Error = err.Error()
		return audit
	}
	audit.ArtifactHash = artifact.ContentHash
	audit.ArtifactSigner = strings.ToLower(artifact.Signer.Hex())
	audit.SourceStartBlock = artifact.Start.Number
	audit.SourceStartHash = strings.ToLower(artifact.Start.Hash)
	audit.SourceEndBlock = artifact.End.Number
	audit.SourceEndHash = strings.ToLower(artifact.End.Hash)
	audit.CommittedArtifactHash = "0x" + hex.EncodeToString(expectation.ArtifactHash[:])
	audit.PayoutRoot = "0x" + hex.EncodeToString(expectation.PayoutRoot[:])
	audit.RootCommitter = strings.ToLower(expectation.Committer.Hex())
	audit.RootSigner = strings.ToLower(expectation.RootSigner.Hex())
	audit.RootCommitBlock = expectation.CommitBlock
	if artifact.DeploymentID != expectation.DeploymentID || artifact.ChainID != expectation.ChainID || artifact.Netuid != expectation.Netuid || artifact.Coordinator != expectation.Coordinator || artifact.SettlementVault != expectation.SettlementVault || artifact.Epoch != expectation.Epoch || artifact.NoID != expectation.NoID || artifact.Signer != expectation.Signer || !strings.EqualFold(artifact.GenesisHash, expectation.GenesisHash) || !strings.EqualFold(artifact.PolicyHash, expectation.PolicyHash) {
		audit.Error = "payout artifact deployment identity mismatch"
		return audit
	}
	if artifact.Start.Number != expectation.Start.Number || artifact.End.Number != expectation.End.Number || !strings.EqualFold(artifact.Start.Hash, expectation.Start.Hash) || !strings.EqualFold(artifact.End.Hash, expectation.End.Hash) {
		audit.Error = "payout artifact finalized boundary mismatch"
		return audit
	}
	artifactHash, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(artifact.ContentHash), "sha256:"))
	if err != nil || len(artifactHash) != 32 {
		audit.Error = "payout artifact content hash is invalid"
		return audit
	}
	var artifactHash32 [32]byte
	copy(artifactHash32[:], artifactHash)
	if expectation.CommitBlock == 0 || expectation.PayoutRoot == ([32]byte{}) || expectation.ArtifactHash == ([32]byte{}) || expectation.Committer == (common.Address{}) || expectation.Committer != expectation.RootSigner || artifact.PayoutRoot != expectation.PayoutRoot || artifactHash32 != expectation.ArtifactHash {
		audit.Error = "payout artifact does not match its on-chain root commitment"
		return audit
	}
	judgeDepositUsage(&audit, artifact.TotalUsageBytes, artifact.TotalUsers, observedDeposit, convictionBefore, depositPolicy)
	return audit
}

// judgeDepositUsage applies the shared exact floor-and-cap formula to audited
// usage: the deposit must equal what the usage owes at the operator's own tier.
func judgeDepositUsage(audit *DepositAudit, usageBytes, users uint64, observedDeposit, convictionBefore *big.Int, depositPolicy protocol.DepositPolicy) {
	required, tier, err := protocol.RequiredDepositRao(usageBytes, users, convictionBefore, depositPolicy)
	if err != nil {
		audit.Error = fmt.Sprintf("canonical deposit formula: %v", err)
		return
	}
	audit.UsageBytes = usageBytes
	audit.Users = users
	audit.RateNumeratorRaoPerGiB = tier.RateNumeratorRaoPerGiB
	audit.RateNumeratorRaoPerUser = tier.RateNumeratorRaoPerUser
	audit.RateDenominator = tier.RateDenominator
	audit.RequiredDepositRao = required.String()
	if observedDeposit.Cmp(required) != 0 {
		audit.Status = DepositAuditMismatch
		audit.Error = "observed deposit does not equal the signed-usage requirement"
		return
	}
	audit.Status = DepositAuditCompliant
	audit.Compliant = true
	audit.Disposition = "pool_weight_eligible"
}

// DepositUsageExpectation pins every field of a usage statement that does not
// come from the statement itself, including its epoch's committed v2 root. The
// event that committed the root names no sender, because STSettlement2 accepts a
// commit only from the source epoch's root signer.
type DepositUsageExpectation struct {
	DeploymentID string
	ChainID      uint64
	GenesisHash  string
	Netuid       uint16
	Coordinator  common.Address
	VaultV2      common.Address
	PolicyHash   string
	Epoch        uint64
	NoID         uint64
	Signer       common.Address
	Start        payoutartifact.Boundary
	End          payoutartifact.Boundary
	// ProviderChunks is the committed header's chunk list for Epoch.
	ProviderChunks []string
	// Epoch's committed extender component as its statement must carry it, read
	// from the header and its census and share-row chunks
	// (usage.CommittedExtenders); nil for an epoch without one, whose statement
	// is version 1.
	Extenders   *usage.Extenders
	Root        [32]byte
	LedgerHash  [32]byte
	RootSigner  common.Address
	CommitBlock uint64
}

// usageDepositAudit is the audit of a v2 source epoch before any statement is
// judged: the commit it found, if any, and zero weight.
func usageDepositAudit(epoch uint64, expectation *DepositUsageExpectation, observed, conviction *big.Int) DepositAudit {
	audit := baseDepositAudit(epoch, expectation.Epoch, expectation.NoID, observed, conviction)
	audit.UsageEvidence = usage.KindOf(expectation.Extenders != nil)
	audit.CommittedArtifactHash = "0x" + hex.EncodeToString(expectation.LedgerHash[:])
	audit.PayoutRoot = "0x" + hex.EncodeToString(expectation.Root[:])
	audit.RootCommitter = strings.ToLower(expectation.RootSigner.Hex())
	audit.RootSigner = strings.ToLower(expectation.RootSigner.Hex())
	audit.RootCommitBlock = expectation.CommitBlock
	return audit
}

// verifyDepositUsageStatement requires a statement to be signed by the operator's
// pinned artifact signer and to name exactly this deployment, policy and epoch.
func verifyDepositUsageStatement(statement *usage.Statement, expectation *DepositUsageExpectation) error {
	if err := statement.Verify(expectation.Signer); err != nil {
		return err
	}
	if statement.DeploymentID != expectation.DeploymentID || statement.ChainID != expectation.ChainID || statement.Netuid != expectation.Netuid || statement.Coordinator != expectation.Coordinator || statement.VaultV2 != expectation.VaultV2 || statement.Epoch != expectation.Epoch || statement.NoID != expectation.NoID || !strings.EqualFold(statement.GenesisHash, expectation.GenesisHash) || !strings.EqualFold(statement.PolicyHash, expectation.PolicyHash) {
		return errors.New("usage statement deployment identity mismatch")
	}
	if statement.Start.Number != expectation.Start.Number || statement.End.Number != expectation.End.Number || !strings.EqualFold(statement.Start.Hash, expectation.Start.Hash) || !strings.EqualFold(statement.End.Hash, expectation.End.Hash) {
		return errors.New("usage statement finalized boundary mismatch")
	}
	return nil
}

// selectDepositUsageStatement chooses the statement of the committed root by its
// chunk lists, because a rebuild can publish a second statement for other chunks:
// its provider chunks and, for an epoch with an extender component, its census and
// share-row chunks. Only a statement that verifies speaks for the operator, so one
// that does not can neither be chosen nor cause equivocation. Two that verify with
// different totals or extender pools for the same chunks are equivocation,
// whichever came first. status is empty for a chosen statement, and otherwise says
// why there is none; nil, "" and a nil error mean no statement names these chunks.
func selectDepositUsageStatement(statements []*usage.Statement, expectation *DepositUsageExpectation) (selected *usage.Statement, status string, err error) {
	var refused error
	for _, statement := range statements {
		if !slices.Equal(statement.ProviderChunks, expectation.ProviderChunks) || !depositUsageNamesExtenders(statement, expectation.Extenders) {
			continue
		}
		if err := verifyDepositUsageStatement(statement, expectation); err != nil {
			refused = errors.Join(refused, err)
			continue
		}
		if selected == nil {
			selected = statement
		} else if statement.TotalUsageBytes != selected.TotalUsageBytes || statement.TotalUsers != selected.TotalUsers || statement.ProviderRows != selected.ProviderRows {
			return nil, DepositAuditEquivocation, fmt.Errorf("usage statements for the committed chunks disagree: %d bytes and %d users, or %d bytes and %d users", selected.TotalUsageBytes, selected.TotalUsers, statement.TotalUsageBytes, statement.TotalUsers)
		} else if !sameDepositUsageExtenderPool(statement.ExtenderPool, selected.ExtenderPool) {
			return nil, DepositAuditEquivocation, errors.New("usage statements for the committed chunks disagree on the extender pool")
		}
	}
	if selected == nil && refused != nil {
		return nil, DepositAuditInvalid, refused
	}
	return selected, "", nil
}

// Reports whether a statement names an epoch's committed extender chunks: its
// census and share-row chunks, in order. Without a committed component every
// statement does: the version is checked as it is judged.
func depositUsageNamesExtenders(statement *usage.Statement, committed *usage.Extenders) bool {
	if committed == nil {
		return true
	}
	return statement.ExtenderPool != nil && slices.Equal(statement.ExtenderCensus, committed.Census) && slices.Equal(statement.ExtenderChunks, committed.Chunks)
}

// Reports two equal extender pools, both nil included.
func sameDepositUsageExtenderPool(a, b *usage.ExtenderPool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.IntraPlatformBytes == b.IntraPlatformBytes && a.IntraNetworkBytes == b.IntraNetworkBytes && a.ExtenderPoolBytes == b.ExtenderPoolBytes &&
		a.ExtenderPoolWeightBytes == b.ExtenderPoolWeightBytes && a.ExtenderPoolContracts == b.ExtenderPoolContracts && a.ExtenderPoolPayers == b.ExtenderPoolPayers &&
		slices.Equal(a.ExtenderCensus, b.ExtenderCensus) && a.ExtenderUnits == b.ExtenderUnits && a.EligibleExtenders == b.EligibleExtenders && slices.Equal(a.ExtenderChunks, b.ExtenderChunks)
}

// DepositUsageRows is what a source epoch's committed rows hold once the rules a
// verifier can check passed: their count and their usage summed by usage.SumRows.
type DepositUsageRows struct {
	Count      uint64
	UsageBytes uint64
}

// EvaluateDepositUsage judges one operator statement against what the committed
// rows of a source epoch settled under settlement v2 hold, then applies the same
// exact formula as a payout artifact. Bytes and the row count are checked against
// the rows. Users are the operator's attestation, as they are in a payout artifact.
// For an epoch with an extender component (expectation.Extenders), the statement
// must be version 2 and carry exactly what the component holds, and its bytes are
// the rows' usage plus all of Z, which the formula prices (WHITEPAPER.md §8.6.4);
// Z's parts and the contracts and payers behind it are the operator's attestation.
func EvaluateDepositUsage(statement *usage.Statement, rows DepositUsageRows, expectation *DepositUsageExpectation, epoch uint64, observedDeposit, convictionBefore *big.Int, depositPolicy protocol.DepositPolicy) DepositAudit {
	audit := usageDepositAudit(epoch, expectation, observedDeposit, convictionBefore)
	if observedDeposit == nil || observedDeposit.Sign() < 0 || convictionBefore == nil || convictionBefore.Sign() < 0 {
		audit.Error = "deposit or conviction snapshot is nil or negative"
		return audit
	}
	if statement == nil {
		audit.Error = "usage statement is absent"
		return audit
	}
	encoded, err := statement.Bytes()
	if err != nil {
		audit.Error = err.Error()
		return audit
	}
	audit.ArtifactHash = ReleaseMeasurementContentHash(encoded)
	audit.ArtifactSigner = strings.ToLower(statement.Signer.Hex())
	audit.SourceStartBlock = statement.Start.Number
	audit.SourceStartHash = strings.ToLower(statement.Start.Hash)
	audit.SourceEndBlock = statement.End.Number
	audit.SourceEndHash = strings.ToLower(statement.End.Hash)
	if err := verifyDepositUsageStatement(statement, expectation); err != nil {
		audit.Error = err.Error()
		return audit
	}
	if expectation.CommitBlock == 0 || expectation.Root == ([32]byte{}) || expectation.LedgerHash == ([32]byte{}) || expectation.RootSigner == (common.Address{}) || !slices.Equal(statement.ProviderChunks, expectation.ProviderChunks) {
		audit.Error = "usage statement does not match its on-chain root commitment"
		return audit
	}
	// The statement's version must be the one its header calls for, and a version
	// 2 statement must carry the committed extender component.
	if err := statement.CheckExtenders(expectation.Extenders); err != nil {
		audit.Error = err.Error()
		return audit
	}
	// usage.Statement.CheckRows over sums already taken, so a pass that reuses
	// verified rows judges exactly as one that read them.
	if expectation.Extenders == nil && (rows.UsageBytes != statement.TotalUsageBytes || rows.Count != statement.ProviderRows) {
		audit.Error = fmt.Sprintf("usage: the statement claims %d bytes in %d rows; the ledger holds %d in %d", statement.TotalUsageBytes, statement.ProviderRows, rows.UsageBytes, rows.Count)
		return audit
	}
	if expectation.Extenders != nil {
		if err := statement.CheckSums(rows.UsageBytes, rows.Count); err != nil {
			audit.Error = err.Error()
			return audit
		}
	}
	judgeDepositUsage(&audit, statement.TotalUsageBytes, statement.TotalUsers, observedDeposit, convictionBefore, depositPolicy)
	return audit
}
