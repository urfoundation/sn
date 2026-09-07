package main

// final_semantic_chronology.go closes the gap between a receipt's immutable
// block and the executable that interpreted it. A current runtime census
// cannot prove an implementation which was replaced before the campaign.

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

const finalPublicChronologyAuditSchema = "urnetwork-final-public-chronology-audit-v4"

// Describes the exact coordinator proxy range whose upgrade logs must be
// empty unless an explicit future release authorizes a listed transition.
type FinalCoordinatorUpgradeRangeEvidence struct {
	From  ChainHead `json:"from"`
	To    ChainHead `json:"to"`
	Proxy string    `json:"proxy"`
}

// Retains a decoded UUPS upgrade log in the public verification projection.
// The current release permits no events, but a typed list avoids silently
// weakening the range check if a later campaign intentionally upgrades.
type FinalCoordinatorUpgradeEventEvidence struct {
	Proxy            string    `json:"proxy"`
	TransactionHash  string    `json:"transaction_hash"`
	TransactionIndex uint64    `json:"transaction_index"`
	LogIndex         uint64    `json:"log_index"`
	Block            ChainHead `json:"block"`
	Implementation   string    `json:"implementation"`
}

// Seals the two different runtime obligations: all current-campaign heads
// use the approved release graph, while older carried receipts use their
// archived plan identity. The projection hash prevents a transcript-only
// replay from changing either scope after evidence is signed.
type FinalPublicChronologyAudit struct {
	Schema                   string                                   `json:"schema"`
	HistoricalReceiptCount   uint64                                   `json:"historical_receipt_count"`
	CurrentRuntimeHeadCount  uint64                                   `json:"current_runtime_head_count"`
	OracleWindowArtifactHash string                                   `json:"oracle_window_artifact_hash"`
	OracleWindowCheckpoints  FinalFleetRefreshOracleWindowCheckpoints `json:"oracle_window_checkpoints"`
	UpgradeRange             FinalCoordinatorUpgradeRangeEvidence     `json:"upgrade_range"`
	AllowedUpgrades          []FinalCoordinatorUpgradeEventEvidence   `json:"allowed_upgrades"`
	HistoricalUpgradeRanges  []FinalCoordinatorUpgradeRangeEvidence   `json:"historical_upgrade_ranges"`
	HistoricalUpgrades       []FinalCoordinatorUpgradeEventEvidence   `json:"historical_upgrades"`
	ProjectionHash           string                                   `json:"projection_hash"`
}

// Locates every ordinary receipt with semantic economic meaning. Fleet
// generation writes deliberately stay outside this list because their own
// lineage replay proves the batcher/proxy generation relation independently.
func finalSemanticGenericEVMReceipts(evidence *FinalSemanticEvidence) []FinalEVMReceipt {
	if evidence == nil {
		return nil
	}
	result := make([]FinalEVMReceipt, 0)
	for _, pool := range evidence.Pools {
		result = append(result, pool.Registration, pool.ConvictionReceipt)
	}
	for _, addition := range evidence.Reserve.PrincipalAdditions {
		result = append(result, addition.Receipt)
	}
	for _, validator := range evidence.Validators {
		for _, cycle := range validator.Cycles {
			for _, pool := range cycle.Pools {
				result = append(result, pool.DepositReceipt)
			}
		}
	}
	if dishonest := evidence.DishonestDeposit; dishonest != nil {
		result = append(result, dishonest.UnderpaymentReceipt, dishonest.RecoveryDepositReceipt)
		for _, decisions := range [][]FinalDishonestDepositDecision{dishonest.Penalties, dishonest.Recoveries} {
			for _, decision := range decisions {
				for _, pool := range decision.Cycle.Pools {
					result = append(result, pool.DepositReceipt)
				}
			}
		}
	}
	for _, row := range evidence.Epochs {
		result = append(result, row.Capture, row.Finalize)
		if row.Root != nil {
			result = append(result, *row.Root)
		}
		for _, claim := range row.Claims {
			result = append(result, claim.Receipt)
		}
	}
	for _, payment := range evidence.ClaimPayments {
		result = append(result, payment.Receipt)
	}
	if lifecycle := evidence.FleetLifecycle; lifecycle != nil {
		for _, payout := range lifecycle.PayoutArtifacts {
			result = append(result, payout.Root)
		}
	}
	for _, criterion := range evidence.ExitCriteria {
		result = append(result, criterion.EVMReceipts...)
	}
	return result
}

// Returns one receipt per transaction from the ordinary evidence graph. A
// repeated reference is valid only when its immutable receipt identity is
// identical; otherwise a semantic row has attempted to equivocate about one
// transaction.
func finalSemanticUniqueCarriedEVMReceipts(evidence *FinalSemanticEvidence) (map[string]FinalEVMReceipt, error) {
	if evidence == nil {
		return nil, errors.New("historical coordinator receipt evidence is unavailable")
	}
	result := map[string]FinalEVMReceipt{}
	for _, receipt := range finalSemanticGenericEVMReceipts(evidence) {
		if receipt.Status != "success" || receipt.Block.Number >= evidence.EVMCampaignStartHead.Number {
			continue
		}
		key := strings.ToLower(receipt.TransactionHash)
		if prior, found := result[key]; found {
			if prior != receipt {
				return nil, fmt.Errorf("historical coordinator receipt %s has conflicting semantic references", receipt.TransactionHash)
			}
			continue
		}
		result[key] = receipt
	}
	return result, nil
}

// Validates the signed census before artifact loading. The plan bytes are
// checked later, after content-addressed locators have been loaded exactly.
func verifyFinalHistoricalCoordinatorReceipts(evidence *FinalSemanticEvidence) error {
	expected, err := finalSemanticUniqueCarriedEVMReceipts(evidence)
	if err != nil {
		return err
	}
	if finalHistoricalCoordinatorTimelineRequired(evidence) {
		if err := verifyFinalHistoricalCoordinatorTimeline(evidence.HistoricalCoordinatorTimeline); err != nil {
			return err
		}
		if err := verifyFinalArtifact("historical coordinator implementation timeline", evidence.HistoricalCoordinatorTimelineArtifact, "historical-coordinator-timeline"); err != nil {
			return err
		}
	}
	if (len(expected) != 0 || len(evidence.HistoricalCoordinatorReceipts) != 0) && evidence.EVMCampaignStartHead.Number < 2 {
		return errors.New("historical coordinator receipts require a campaign start after block one")
	}
	if len(evidence.HistoricalCoordinatorReceipts) < len(expected) {
		return fmt.Errorf("historical coordinator receipts=%d, want at least %d ordinary semantic receipts", len(evidence.HistoricalCoordinatorReceipts), len(expected))
	}
	seen := make(map[string]bool, len(evidence.HistoricalCoordinatorReceipts))
	for index := range evidence.HistoricalCoordinatorReceipts {
		row := &evidence.HistoricalCoordinatorReceipts[index]
		if err := verifyFinalEVMReceipt("historical coordinator receipt", row.Receipt, 1, evidence.EVMCampaignStartHead.Number-1); err != nil {
			return err
		}
		if err := verifyFinalArtifact("historical coordinator receipt", row.ReceiptArtifact, "historical-coordinator-receipt"); err != nil {
			return err
		}
		if prior, found := expected[strings.ToLower(row.Receipt.TransactionHash)]; found && prior != row.Receipt {
			return fmt.Errorf("historical coordinator receipt %s differs from the semantic graph", row.Receipt.TransactionHash)
		}
		if seen[row.Receipt.TransactionHash] || row.PlanHash == "" || row.ActionID == "" {
			return errors.New("historical coordinator receipt identity is incomplete or duplicated")
		}
		seen[row.Receipt.TransactionHash] = true
		if err := requireFinalHex32("historical coordinator plan hash", row.PlanHash); err != nil {
			return err
		}
		if err := requireFinalHex32("historical coordinator action intent", row.IntentHash); err != nil {
			return err
		}
		if err := verifyFinalArtifact("historical coordinator plan", row.PlanArtifact, "historical-setup-plan"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("historical coordinator journal", row.JournalArtifact, "historical-journal"); err != nil {
			return err
		}
		if err := verifyFinalArtifact("historical coordinator postcondition", row.PostconditionArtifact, "historical-action-postcondition"); err != nil {
			return err
		}
		for label, value := range map[string]string{
			"historical coordinator transaction sender": row.TransactionFrom,
			"historical coordinator transaction target": row.TransactionTo,
		} {
			canonical, addressErr := finalCanonicalAddress(value)
			if addressErr != nil || canonical != value {
				return stateMismatchError(addressErr, "%s is not canonical", label)
			}
		}
		if row.TransactionTo != row.CoordinatorProxy {
			return errors.New("historical coordinator transaction target differs from archived proxy")
		}
		input, inputErr := hexutil.Decode(row.TransactionInput)
		if inputErr != nil || len(input) < 4 || hexutil.Encode(input) != row.TransactionInput {
			return stateMismatchError(inputErr, "historical coordinator transaction calldata is not canonical")
		}
		value, valueOK := new(big.Int).SetString(row.TransactionValueWei, 10)
		if !valueOK || value.Sign() < 0 || value.String() != row.TransactionValueWei {
			return errors.New("historical coordinator transaction value is not a canonical nonnegative decimal")
		}
		emitters, emitterErr := finalCanonicalHistoricalCoordinatorEmitters(row.Emitters)
		if emitterErr != nil || !slices.Equal(emitters, row.Emitters) {
			return stateMismatchError(emitterErr, "historical coordinator receipt emitters are not canonical")
		}
		fields := map[string]string{
			"historical coordinator proxy":                  row.CoordinatorProxy,
			"historical coordinator implementation":         row.CoordinatorImplementation,
			"historical coordinator proxy runtime":          row.CoordinatorProxyRuntimeHash,
			"historical coordinator implementation runtime": row.CoordinatorImplementationRuntimeHash,
		}
		if row.ActionID == "evm.coordinator-proxy" {
			if row.ExecutionHead != (ChainHead{}) || row.ExecutionImplementation != "" || row.ExecutionImplementationRuntimeHash != "" {
				return errors.New("historical coordinator initialization claims a delegated execution identity")
			}
		} else {
			fields["historical coordinator execution implementation"] = row.ExecutionImplementation
			fields["historical coordinator execution runtime"] = row.ExecutionImplementationRuntimeHash
			if err := verifyFinalHead("historical coordinator execution head", row.ExecutionHead); err != nil {
				return err
			}
		}
		for label, value := range fields {
			if strings.Contains(label, "runtime") {
				if err := requireFinalHex32(label, value); err != nil {
					return err
				}
				continue
			}
			canonical, err := finalCanonicalAddress(value)
			if err != nil || canonical != value {
				return fmt.Errorf("%s is not canonical", label)
			}
		}
		if err := requireFinalHex32("historical coordinator implementation slot", row.CoordinatorImplementationSlot); err != nil || !strings.EqualFold(row.CoordinatorImplementationSlot, evidence.Deployment.ERC1967ImplementationSlot) {
			return stateMismatchError(err, "historical coordinator implementation slot differs from the release slot")
		}
		if err := finalVerifyHistoricalCoordinatorReceiptTimeline(row, evidence.HistoricalCoordinatorTimeline); err != nil {
			return err
		}
		if index > 0 {
			previous := evidence.HistoricalCoordinatorReceipts[index-1]
			if row.Receipt.Block.Number < previous.Receipt.Block.Number || row.Receipt.Block.Number == previous.Receipt.Block.Number && row.TransactionIndex < previous.TransactionIndex {
				return errors.New("historical coordinator receipts are not canonically ordered")
			}
			if row.Receipt.Block.Number == previous.Receipt.Block.Number && row.TransactionIndex == previous.TransactionIndex {
				return errors.New("historical coordinator receipts share a transaction coordinate")
			}
		}
	}
	for transactionHash := range expected {
		if !seen[transactionHash] {
			return fmt.Errorf("historical coordinator ordinary semantic receipt %s is absent", transactionHash)
		}
	}
	return nil
}

// Reports whether the retained predecessor graph has a proxy transition to
// seal and replay. The same predicate controls structural, artifact, and
// public checks so an empty optional history cannot create a phantom locator.
func finalHistoricalCoordinatorTimelineRequired(evidence *FinalSemanticEvidence) bool {
	return evidence != nil && (len(evidence.HistoricalCoordinatorReceipts) != 0 || len(evidence.HistoricalCoordinatorTimeline) != 0)
}

// Requires the v8 temporary-oracle proof locator before a final evidence
// object is considered structurally valid. Its exact source graph is replayed
// from bytes in the artifact verifier, while public chronology binds the same
// digest into its independently signed audit projection.
func verifyFinalFleetRefreshOracleWindowEvidence(evidence *FinalSemanticEvidence) error {
	if evidence == nil || evidence.FleetGeneration == nil {
		return errors.New("historical fleet refresh oracle evidence is unavailable")
	}
	if err := verifyFinalArtifact("historical fleet refresh oracle window", evidence.FleetRefreshOracleWindow.Artifact, "historical-fleet-refresh-oracle-window"); err != nil {
		return err
	}
	return verifyFinalFleetRefreshOracleWindowCheckpoints(evidence.FleetRefreshOracleWindow.Checkpoints)
}

// Names one checkpoint without serializing a caller-chosen order. The two
// active observations may share a head, as may the restored observations,
// but no head can claim two different active-oracle values.
type finalFleetRefreshOracleCheckpointRow struct {
	label string
	value FinalFleetRefreshOracleCheckpointEvidence
}

// Returns the fixed public replay order used by source, on-chain, and
// transcript verifiers. Keeping this order local avoids a map traversal from
// becoming part of the signed public evidence surface.
func finalFleetRefreshOracleCheckpointRows(value FinalFleetRefreshOracleWindowCheckpoints) []finalFleetRefreshOracleCheckpointRow {
	return []finalFleetRefreshOracleCheckpointRow{
		{label: "await-active operational", value: value.AwaitActiveOperational},
		{label: "await-active independent", value: value.AwaitActiveIndependent},
		{label: "await-restored operational", value: value.AwaitRestoredOperational},
		{label: "await-restored independent", value: value.AwaitRestoredIndependent},
	}
}

// Requires canonical, ordered observations for the temporary batcher and its
// restoration. A shared read may serve equivalent observer checkpoints, but
// it can never testify to two distinct oracle identities.
func verifyFinalFleetRefreshOracleWindowCheckpoints(value FinalFleetRefreshOracleWindowCheckpoints) error {
	proxy, proxyErr := finalCanonicalAddress(value.CoordinatorProxy)
	if proxyErr != nil || proxy != value.CoordinatorProxy || proxy == "0x0000000000000000000000000000000000000000" {
		return stateMismatchError(proxyErr, "historical fleet refresh coordinator proxy is not canonical")
	}
	rows := finalFleetRefreshOracleCheckpointRows(value)
	byNumber := make(map[uint64]ChainHead, len(rows))
	byHead := make(map[ChainHead]string, len(rows))
	for _, row := range rows {
		if err := verifyFinalHead("historical fleet refresh "+row.label+" checkpoint", row.value.Head); err != nil {
			return err
		}
		oracle, err := finalCanonicalAddress(row.value.Oracle)
		if err != nil || oracle != row.value.Oracle || oracle == "0x0000000000000000000000000000000000000000" {
			return stateMismatchError(err, "historical fleet refresh %s oracle is not canonical", row.label)
		}
		if prior, found := byNumber[row.value.Head.Number]; found && prior != row.value.Head {
			return errors.New("historical fleet refresh checkpoints disagree on a block hash")
		}
		byNumber[row.value.Head.Number] = row.value.Head
		if prior, found := byHead[row.value.Head]; found && prior != row.value.Oracle {
			return errors.New("historical fleet refresh checkpoint claims conflicting oracle identities")
		}
		byHead[row.value.Head] = row.value.Oracle
	}
	if value.AwaitActiveOperational.Oracle != value.AwaitActiveIndependent.Oracle || value.AwaitRestoredOperational.Oracle != value.AwaitRestoredIndependent.Oracle {
		return errors.New("historical fleet refresh observer pairs disagree on oracle identity")
	}
	if value.AwaitActiveOperational.Oracle == value.AwaitRestoredOperational.Oracle {
		return errors.New("historical fleet refresh restoration does not change the temporary oracle")
	}
	latestActive := value.AwaitActiveOperational.Head.Number
	if value.AwaitActiveIndependent.Head.Number > latestActive {
		latestActive = value.AwaitActiveIndependent.Head.Number
	}
	earliestRestored := value.AwaitRestoredOperational.Head.Number
	if value.AwaitRestoredIndependent.Head.Number < earliestRestored {
		earliestRestored = value.AwaitRestoredIndependent.Head.Number
	}
	if earliestRestored <= latestActive {
		return errors.New("historical fleet refresh restoration checkpoint does not follow the active checkpoint")
	}
	return nil
}

// Builds the sealed public projection without trusting the reader's result.
// Current runtime heads intentionally omit carried predecessors, whose plan
// identity is replayed one receipt at a time below.
func finalPublicChronologyAuditForEvidence(evidence *FinalSemanticEvidence, currentRuntimeHeads []ChainHead) (FinalPublicChronologyAudit, error) {
	if evidence == nil || len(currentRuntimeHeads) == 0 {
		return FinalPublicChronologyAudit{}, errors.New("public chronology evidence is incomplete")
	}
	if err := verifyFinalHistoricalCoordinatorReceipts(evidence); err != nil {
		return FinalPublicChronologyAudit{}, err
	}
	if err := verifyFinalFleetRefreshOracleWindowEvidence(evidence); err != nil {
		return FinalPublicChronologyAudit{}, err
	}
	proxy, err := finalCanonicalAddress(evidence.Deployment.CoordinatorProxy)
	if err != nil {
		return FinalPublicChronologyAudit{}, err
	}
	rangeEvidence := FinalCoordinatorUpgradeRangeEvidence{From: evidence.EVMCampaignStartHead, To: evidence.EVMTerminalHead, Proxy: proxy}
	if err := verifyFinalCoordinatorUpgradeRange(rangeEvidence); err != nil {
		return FinalPublicChronologyAudit{}, err
	}
	historicalRanges, historicalEvents, historyErr := finalHistoricalCoordinatorUpgradeRanges(evidence)
	if historyErr != nil {
		return FinalPublicChronologyAudit{}, historyErr
	}
	result := FinalPublicChronologyAudit{
		Schema: finalPublicChronologyAuditSchema, HistoricalReceiptCount: uint64(len(evidence.HistoricalCoordinatorReceipts)),
		CurrentRuntimeHeadCount: uint64(len(currentRuntimeHeads)), UpgradeRange: rangeEvidence,
		OracleWindowArtifactHash: evidence.FleetRefreshOracleWindow.Artifact.ContentHash,
		OracleWindowCheckpoints:  evidence.FleetRefreshOracleWindow.Checkpoints,
		AllowedUpgrades:          []FinalCoordinatorUpgradeEventEvidence{},
		HistoricalUpgradeRanges:  historicalRanges,
		HistoricalUpgrades:       historicalEvents,
	}
	projection := result
	projection.ProjectionHash = ""
	hash, err := canonicalHashHex(projection)
	if err != nil {
		return FinalPublicChronologyAudit{}, err
	}
	result.ProjectionHash = hash
	return result, nil
}

// Derives every historical proxy range from its exact constructor event and
// lists every constructor/activation event that the sealed timeline permits.
// A provider response cannot introduce an owner upgrade between two carried
// receipts because the range begins at proxy creation, not a later plan field.
func finalHistoricalCoordinatorUpgradeRanges(evidence *FinalSemanticEvidence) ([]FinalCoordinatorUpgradeRangeEvidence, []FinalCoordinatorUpgradeEventEvidence, error) {
	if evidence == nil {
		return nil, nil, errors.New("historical coordinator upgrade timeline is unavailable")
	}
	if len(evidence.HistoricalCoordinatorTimeline) == 0 {
		if len(evidence.HistoricalCoordinatorReceipts) == 0 {
			return []FinalCoordinatorUpgradeRangeEvidence{}, []FinalCoordinatorUpgradeEventEvidence{}, nil
		}
		return nil, nil, errors.New("historical coordinator upgrade timeline is unavailable")
	}
	ranges := make([]FinalCoordinatorUpgradeRangeEvidence, 0, len(evidence.HistoricalCoordinatorTimeline))
	events := make([]FinalCoordinatorUpgradeEventEvidence, 0)
	for _, timeline := range evidence.HistoricalCoordinatorTimeline {
		rangeEvidence := FinalCoordinatorUpgradeRangeEvidence{From: timeline.Initialization.Block, To: evidence.EVMCampaignStartHead, Proxy: timeline.Proxy}
		if err := verifyFinalCoordinatorUpgradeRange(rangeEvidence); err != nil {
			return nil, nil, err
		}
		ranges = append(ranges, rangeEvidence)
		events = append(events, finalCoordinatorUpgradeEventForTransition(timeline.Proxy, timeline.Initialization))
		for _, upgrade := range timeline.Upgrades {
			events = append(events, finalCoordinatorUpgradeEventForTransition(timeline.Proxy, upgrade))
		}
	}
	sort.Slice(ranges, func(left, right int) bool { return ranges[left].Proxy < ranges[right].Proxy })
	sort.Slice(events, func(left, right int) bool {
		if events[left].Proxy != events[right].Proxy {
			return events[left].Proxy < events[right].Proxy
		}
		if events[left].Block.Number != events[right].Block.Number {
			return events[left].Block.Number < events[right].Block.Number
		}
		if events[left].TransactionIndex != events[right].TransactionIndex {
			return events[left].TransactionIndex < events[right].TransactionIndex
		}
		return events[left].LogIndex < events[right].LogIndex
	})
	for index := range ranges {
		if index > 0 && ranges[index].Proxy <= ranges[index-1].Proxy {
			return nil, nil, errors.New("historical coordinator upgrade ranges are not canonical")
		}
	}
	if err := verifyFinalCoordinatorUpgradeEvents(ranges, events); err != nil {
		return nil, nil, err
	}
	if err := verifyFinalHistoricalCoordinatorUpgradeEventCoverage(ranges, events); err != nil {
		return nil, nil, err
	}
	return ranges, events, nil
}

// Projects one timeline transition into the smaller public-log comparison
// record while retaining true EVM execution position and proxy identity.
func finalCoordinatorUpgradeEventForTransition(proxy string, value FinalHistoricalCoordinatorUpgradeEvidence) FinalCoordinatorUpgradeEventEvidence {
	return FinalCoordinatorUpgradeEventEvidence{
		Proxy: proxy, TransactionHash: value.TransactionHash, TransactionIndex: value.TransactionIndex, LogIndex: value.LogIndex,
		Block: value.Block, Implementation: value.Post.Implementation,
	}
}

// Checks the exact range semantics, including an inclusive terminal head so
// a final-block upgrade cannot evade the terminal runtime observation.
func verifyFinalCoordinatorUpgradeRange(value FinalCoordinatorUpgradeRangeEvidence) error {
	if err := verifyFinalHead("coordinator upgrade range start", value.From); err != nil {
		return err
	}
	if err := verifyFinalHead("coordinator upgrade range terminal", value.To); err != nil {
		return err
	}
	if value.To.Number < value.From.Number {
		return errors.New("coordinator upgrade range is inverted")
	}
	canonical, err := finalCanonicalAddress(value.Proxy)
	if err != nil || canonical != value.Proxy {
		return errors.New("coordinator upgrade range proxy is not canonical")
	}
	return nil
}

// Rejects a public projection with a changed range, count, or permitted-event
// set before its transcript hash is accepted as a semantic proof.
func verifyFinalPublicChronologyAudit(evidence *FinalSemanticEvidence, value FinalPublicChronologyAudit, currentRuntimeHeads []ChainHead) error {
	want, err := finalPublicChronologyAuditForEvidence(evidence, currentRuntimeHeads)
	if err != nil {
		return err
	}
	if !finalJSONEqual(value, want) {
		return errors.New("public coordinator chronology audit differs from signed evidence")
	}
	return nil
}

// Validates the self-contained transcript projection before evidence context
// is available. The later evidence-bound comparison also checks its counts
// and exact endpoints, so neither verification phase trusts the other.
func verifyFinalPublicChronologyAuditShape(value FinalPublicChronologyAudit) error {
	if value.Schema != finalPublicChronologyAuditSchema || value.CurrentRuntimeHeadCount == 0 {
		return errors.New("public coordinator chronology audit is incomplete")
	}
	if err := requireFinalSHA256("public coordinator oracle window artifact", value.OracleWindowArtifactHash); err != nil {
		return err
	}
	if err := verifyFinalFleetRefreshOracleWindowCheckpoints(value.OracleWindowCheckpoints); err != nil {
		return err
	}
	if err := verifyFinalCoordinatorUpgradeRange(value.UpgradeRange); err != nil {
		return err
	}
	if err := verifyFinalCoordinatorUpgradeEvents([]FinalCoordinatorUpgradeRangeEvidence{value.UpgradeRange}, value.AllowedUpgrades); err != nil {
		return err
	}
	if value.HistoricalReceiptCount != 0 && len(value.HistoricalUpgradeRanges) == 0 {
		return errors.New("public coordinator historical upgrade ranges are absent")
	}
	if value.HistoricalReceiptCount == 0 && (len(value.HistoricalUpgradeRanges) != 0 || len(value.HistoricalUpgrades) != 0) {
		return errors.New("public coordinator chronology has unexpected historical upgrades")
	}
	for index := range value.HistoricalUpgradeRanges {
		rangeEvidence := value.HistoricalUpgradeRanges[index]
		if err := verifyFinalCoordinatorUpgradeRange(rangeEvidence); err != nil {
			return err
		}
		if index > 0 && rangeEvidence.Proxy <= value.HistoricalUpgradeRanges[index-1].Proxy {
			return errors.New("public coordinator historical upgrade ranges are not canonical")
		}
	}
	if err := verifyFinalCoordinatorUpgradeEvents(value.HistoricalUpgradeRanges, value.HistoricalUpgrades); err != nil {
		return err
	}
	if err := verifyFinalHistoricalCoordinatorUpgradeEventCoverage(value.HistoricalUpgradeRanges, value.HistoricalUpgrades); err != nil {
		return err
	}
	copy := value
	copy.ProjectionHash = ""
	hash, err := canonicalHashHex(copy)
	if err != nil || value.ProjectionHash != hash {
		return stateMismatchError(err, "public coordinator chronology audit hash differs")
	}
	return nil
}

// Enforces exact proxy membership and true transaction/log ordering for an
// allowed event projection. At least one event must remain for every retained
// historical proxy because its constructor Upgraded log is mandatory.
func verifyFinalCoordinatorUpgradeEvents(ranges []FinalCoordinatorUpgradeRangeEvidence, events []FinalCoordinatorUpgradeEventEvidence) error {
	byProxy := make(map[string]FinalCoordinatorUpgradeRangeEvidence, len(ranges))
	for _, rangeEvidence := range ranges {
		if _, duplicate := byProxy[rangeEvidence.Proxy]; duplicate {
			return errors.New("allowed coordinator upgrade ranges duplicate a proxy")
		}
		byProxy[rangeEvidence.Proxy] = rangeEvidence
	}
	for index, event := range events {
		if err := requireFinalHex32("allowed coordinator upgrade transaction", event.TransactionHash); err != nil {
			return err
		}
		rangeEvidence, found := byProxy[event.Proxy]
		if !found {
			return errors.New("allowed coordinator upgrade has no signed proxy range")
		}
		if err := verifyFinalHead("allowed coordinator upgrade block", event.Block); err != nil || event.Block.Number < rangeEvidence.From.Number || event.Block.Number > rangeEvidence.To.Number {
			return stateMismatchError(err, "allowed coordinator upgrade is outside its signed range")
		}
		for label, address := range map[string]string{
			"allowed coordinator upgrade proxy":          event.Proxy,
			"allowed coordinator upgrade implementation": event.Implementation,
		} {
			canonical, err := finalCanonicalAddress(address)
			if err != nil || canonical != address || address == "0x0000000000000000000000000000000000000000" {
				return fmt.Errorf("%s is not canonical", label)
			}
		}
		if index > 0 && event.Proxy == events[index-1].Proxy {
			previous := events[index-1]
			if event.Block.Number < previous.Block.Number || event.Block.Number == previous.Block.Number && (event.TransactionIndex < previous.TransactionIndex || event.TransactionIndex == previous.TransactionIndex && event.LogIndex <= previous.LogIndex) {
				return errors.New("allowed coordinator upgrades are not canonical")
			}
		} else if index > 0 && event.Proxy < events[index-1].Proxy {
			return errors.New("allowed coordinator upgrade proxies are not canonical")
		}
	}
	return nil
}

// Requires every retained proxy range to contain its constructor event. The
// current campaign range intentionally allows zero events, so this stronger
// coverage rule belongs only to the pre-campaign historical trust domain.
func verifyFinalHistoricalCoordinatorUpgradeEventCoverage(ranges []FinalCoordinatorUpgradeRangeEvidence, events []FinalCoordinatorUpgradeEventEvidence) error {
	counts := make(map[string]uint64, len(ranges))
	for _, event := range events {
		counts[event.Proxy]++
	}
	for _, rangeEvidence := range ranges {
		if counts[rangeEvidence.Proxy] == 0 {
			return errors.New("historical coordinator proxy range has no constructor upgrade event")
		}
	}
	return nil
}

// Sorts the signed records before hashing so source collection order cannot
// create a second valid evidence hash for the same receipt set.
func canonicalizeFinalHistoricalCoordinatorReceipts(rows []FinalHistoricalCoordinatorReceiptEvidence) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Receipt.Block.Number != rows[j].Receipt.Block.Number {
			return rows[i].Receipt.Block.Number < rows[j].Receipt.Block.Number
		}
		if rows[i].TransactionIndex != rows[j].TransactionIndex {
			return rows[i].TransactionIndex < rows[j].TransactionIndex
		}
		return rows[i].Receipt.TransactionHash < rows[j].Receipt.TransactionHash
	})
}

// Normalizes a complete release-emitter graph. A carried coordinator receipt
// may have multiple contracts emit in one transaction; accepting a subset
// would let a matching proxy event conceal a changed custody-side effect.
func finalCanonicalHistoricalCoordinatorEmitters(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("historical coordinator receipt has no release emitters")
	}
	result := make([]string, len(values))
	for index, value := range values {
		canonical, err := finalCanonicalAddress(value)
		if err != nil || canonical != value {
			return nil, stateMismatchError(err, "historical coordinator receipt emitter is not canonical")
		}
		if index > 0 && value <= values[index-1] {
			return nil, errors.New("historical coordinator receipt emitters are not strictly ordered")
		}
		result[index] = value
	}
	return result, nil
}

// Converts the exact expected code identity into a state comparison domain
// shared by public-RPC and deterministic fake readers.
type FinalHistoricalCoordinatorReceiptState struct {
	Receipt                              FinalEVMReceiptState `json:"receipt"`
	TransactionIndex                     uint64               `json:"transaction_index"`
	From                                 string               `json:"from"`
	To                                   string               `json:"to"`
	Input                                string               `json:"input"`
	ValueWei                             string               `json:"value_wei"`
	Emitters                             []string             `json:"emitters"`
	CoordinatorProxy                     string               `json:"coordinator_proxy"`
	ExecutionImplementation              string               `json:"execution_implementation"`
	ExecutionObservedImplementationSlot  string               `json:"execution_observed_implementation_slot"`
	ExecutionProxyRuntimeHash            string               `json:"execution_proxy_runtime_hash"`
	ExecutionImplementationRuntimeHash   string               `json:"execution_implementation_runtime_hash"`
	CoordinatorImplementation            string               `json:"coordinator_implementation"`
	ObservedImplementationSlot           string               `json:"observed_implementation_slot"`
	CoordinatorProxyRuntimeHash          string               `json:"coordinator_proxy_runtime_hash"`
	CoordinatorImplementationRuntimeHash string               `json:"coordinator_implementation_runtime_hash"`
}

// Describes the independently reread post-construction state for one proxy.
// It is kept separate from a carried receipt because constructor deployment
// has no delegated execution row from which an archive reader can infer it.
type FinalHistoricalCoordinatorBaselineState struct {
	Proxy                      string    `json:"proxy"`
	Implementation             string    `json:"implementation"`
	ObservedImplementationSlot string    `json:"observed_implementation_slot"`
	ProxyRuntimeHash           string    `json:"proxy_runtime_hash"`
	ImplementationRuntimeHash  string    `json:"implementation_runtime_hash"`
	Block                      ChainHead `json:"block"`
}

// Retains one direct active-oracle observation at a sealed archive head. The
// separate state prevents a source postcondition map from standing in for a
// public RPC read during the temporary batcher interval.
type FinalCoordinatorActiveCommitmentOracleState struct {
	CoordinatorProxy string    `json:"coordinator_proxy"`
	Oracle           string    `json:"oracle"`
	Block            ChainHead `json:"block"`
}

// Keeps an on-chain range event separate from its signed projection because
// an unexpected event is a verifier error rather than evidence which can be
// normalized into an allowed row.
type FinalCoordinatorUpgradeRangeState struct {
	Event FinalCoordinatorUpgradeEventEvidence `json:"event"`
}

// Contains only the historical EVM calls introduced by the chronology domain.
// A narrow capability prevents unrelated readers from accidentally skipping
// archive receipt or log-range replay.
type FinalSemanticChronologyChainReader interface {
	HistoricalCoordinatorBaseline(context.Context, FinalHistoricalCoordinatorProxyTimelineEvidence) (FinalHistoricalCoordinatorBaselineState, []FinalRPCExchange, error)
	HistoricalCoordinatorReceipt(context.Context, FinalHistoricalCoordinatorReceiptEvidence) (FinalHistoricalCoordinatorReceiptState, []FinalRPCExchange, error)
	CoordinatorUpgradeRange(context.Context, FinalCoordinatorUpgradeRangeEvidence) ([]FinalCoordinatorUpgradeRangeState, []FinalRPCExchange, error)
	CoordinatorActiveCommitmentOracle(context.Context, string, ChainHead) (FinalCoordinatorActiveCommitmentOracleState, []FinalRPCExchange, error)
}

// Narrows baseline replay to its one archive surface so focused readers do
// not need to implement unrelated receipt, range, or oracle capabilities.
type finalSemanticCoordinatorBaselineReader interface {
	HistoricalCoordinatorBaseline(context.Context, FinalHistoricalCoordinatorProxyTimelineEvidence) (FinalHistoricalCoordinatorBaselineState, []FinalRPCExchange, error)
}

// Limits temporary-oracle replay to one historical proxy call so focused
// tests cannot accidentally substitute the terminal deployment reader.
type finalSemanticFleetRefreshOracleReader interface {
	CoordinatorActiveCommitmentOracle(context.Context, string, ChainHead) (FinalCoordinatorActiveCommitmentOracleState, []FinalRPCExchange, error)
}

// Selects the runtime-census heads whose state must equal the final release
// graph. Older checkpoints remain canonicalized globally, but their runtime is
// deliberately replayed through the archived-plan receipt path instead.
func finalSemanticCurrentRuntimeHeads(evidence *FinalSemanticEvidence) ([]ChainHead, error) {
	if evidence == nil {
		return nil, errors.New("current coordinator runtime evidence is unavailable")
	}
	_, heads, err := finalSemanticHeads(evidence)
	if err != nil {
		return nil, err
	}
	result := make([]ChainHead, 0, len(heads))
	for _, head := range heads {
		if head.Number >= evidence.EVMCampaignStartHead.Number {
			result = append(result, head)
		}
	}
	if len(result) == 0 || result[0] != evidence.EVMCampaignStartHead || result[len(result)-1] != evidence.EVMTerminalHead {
		return nil, errors.New("current coordinator runtime heads do not cover the signed campaign")
	}
	return result, nil
}

// Preserves the reader's request order while assigning receipt and execution
// observations to their independently pinned canonical heads. A carried call
// may need the receipt-block post-state and an earlier execution-state read;
// merging them under either head would make the transcript unverifiable.
func appendFinalHistoricalCoordinatorReceiptExchanges(row FinalHistoricalCoordinatorReceiptEvidence, exchanges []FinalRPCExchange, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if len(exchanges) == 0 {
		return errors.New("historical coordinator receipt returned no transcript")
	}
	allowed := map[ChainHead]bool{row.Receipt.Block: true}
	if row.ActionID != "evm.coordinator-proxy" {
		allowed[row.ExecutionHead] = true
	}
	for start := 0; start < len(exchanges); {
		head := exchanges[start].PinnedHead
		if !allowed[head] {
			return fmt.Errorf("historical coordinator receipt %s transcript uses an unsealed head", row.Receipt.TransactionHash)
		}
		end := start + 1
		for end < len(exchanges) && exchanges[end].PinnedHead == head {
			end++
		}
		if err := appendExchanges("evm", head, exchanges[start:end]); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// Re-reads each constructor's exact observed post-state rather than relying
// on an ordinary carried receipt to happen to consume the baseline head.
// Every sealed baseline is therefore independently checked by public RPC.
func verifyFinalSemanticCoordinatorBaselinesOnChain(ctx context.Context, timelines []FinalHistoricalCoordinatorProxyTimelineEvidence, reader finalSemanticCoordinatorBaselineReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if ctx == nil || len(timelines) == 0 || reader == nil || appendExchanges == nil {
		return errors.New("public coordinator baseline replay inputs are incomplete")
	}
	for _, timeline := range timelines {
		state, exchanges, err := reader.HistoricalCoordinatorBaseline(ctx, timeline)
		if err != nil {
			return fmt.Errorf("historical coordinator baseline %s: %w", timeline.Proxy, err)
		}
		if err := appendExchanges("evm", timeline.Baseline, exchanges); err != nil {
			return err
		}
		wantSlot := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(timeline.Initialization.Post.Implementation, "0x")
		if state.Proxy != timeline.Proxy || state.Implementation != timeline.Initialization.Post.Implementation || state.ObservedImplementationSlot != wantSlot || state.ProxyRuntimeHash != timeline.ProxyRuntimeHash || state.ImplementationRuntimeHash != timeline.Initialization.Post.RuntimeHash || state.Block != timeline.Baseline {
			return fmt.Errorf("historical coordinator baseline %s differs from its sealed initialization", timeline.Proxy)
		}
	}
	return nil
}

// Replays the temporary batcher's live oracle state at each sealed observer
// checkpoint. Identical operational/independent observations share one exact
// RPC read, while conflicting aliases are rejected before a reader is called.
func verifyFinalSemanticFleetRefreshOracleWindowOnChain(ctx context.Context, value FinalFleetRefreshOracleWindowCheckpoints, reader finalSemanticFleetRefreshOracleReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if ctx == nil || reader == nil || appendExchanges == nil {
		return errors.New("public fleet refresh oracle replay inputs are incomplete")
	}
	if err := verifyFinalFleetRefreshOracleWindowCheckpoints(value); err != nil {
		return err
	}
	seen := make(map[ChainHead]string)
	for _, row := range finalFleetRefreshOracleCheckpointRows(value) {
		if prior, found := seen[row.value.Head]; found {
			if prior != row.value.Oracle {
				return errors.New("public fleet refresh oracle checkpoint aliases conflicting values")
			}
			continue
		}
		state, exchanges, err := reader.CoordinatorActiveCommitmentOracle(ctx, value.CoordinatorProxy, row.value.Head)
		if err != nil {
			return fmt.Errorf("public fleet refresh oracle %s: %w", row.label, err)
		}
		if err := appendExchanges("evm", row.value.Head, exchanges); err != nil {
			return err
		}
		if state.CoordinatorProxy != value.CoordinatorProxy || state.Block != row.value.Head || state.Oracle != row.value.Oracle {
			return fmt.Errorf("public fleet refresh oracle %s differs from sealed checkpoint", row.label)
		}
		seen[row.value.Head] = row.value.Oracle
	}
	return nil
}

// Replays the historical receipt/runtime identities and the entire signed
// campaign upgrade range. The current runtime census is intentionally called
// separately by the chain verifier so an old receipt cannot be judged against
// a newly upgraded implementation.
func verifyFinalSemanticChronologyOnChain(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, audit FinalPublicChronologyAudit, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if ctx == nil || evidence == nil || reader == nil || appendExchanges == nil {
		return errors.New("public coordinator chronology inputs are incomplete")
	}
	chronologyReader, ok := reader.(FinalSemanticChronologyChainReader)
	if !ok {
		return errors.New("public semantic reader does not expose coordinator chronology replay")
	}
	currentRuntimeHeads, err := finalSemanticCurrentRuntimeHeads(evidence)
	if err != nil {
		return err
	}
	if err := verifyFinalPublicChronologyAudit(evidence, audit, currentRuntimeHeads); err != nil {
		return err
	}
	if err := verifyFinalSemanticFleetRefreshOracleWindowOnChain(ctx, audit.OracleWindowCheckpoints, chronologyReader, appendExchanges); err != nil {
		return err
	}
	if finalHistoricalCoordinatorTimelineRequired(evidence) {
		if err := verifyFinalSemanticCoordinatorBaselinesOnChain(ctx, evidence.HistoricalCoordinatorTimeline, chronologyReader, appendExchanges); err != nil {
			return err
		}
	}
	for _, row := range evidence.HistoricalCoordinatorReceipts {
		state, exchanges, stateErr := chronologyReader.HistoricalCoordinatorReceipt(ctx, row)
		if stateErr != nil {
			return fmt.Errorf("historical coordinator receipt %s: %w", row.Receipt.TransactionHash, stateErr)
		}
		if err := appendFinalHistoricalCoordinatorReceiptExchanges(row, exchanges, appendExchanges); err != nil {
			return err
		}
		wantSlot := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(row.CoordinatorImplementation, "0x")
		if state.Receipt.TransactionHash != row.Receipt.TransactionHash || state.Receipt.Block != row.Receipt.Block || state.Receipt.Status != row.Receipt.Status || state.Receipt.LogsHash != row.Receipt.LogsHash || state.TransactionIndex != row.TransactionIndex ||
			state.From != row.TransactionFrom || state.To != row.TransactionTo || state.Input != row.TransactionInput || state.ValueWei != row.TransactionValueWei || !slices.Equal(state.Emitters, row.Emitters) ||
			state.To != row.CoordinatorProxy || state.CoordinatorProxy != row.CoordinatorProxy || state.CoordinatorImplementation != row.CoordinatorImplementation ||
			state.ObservedImplementationSlot != wantSlot || state.CoordinatorProxyRuntimeHash != row.CoordinatorProxyRuntimeHash || state.CoordinatorImplementationRuntimeHash != row.CoordinatorImplementationRuntimeHash {
			return fmt.Errorf("historical coordinator receipt %s runtime differs from its archived plan", row.Receipt.TransactionHash)
		}
		if row.ActionID == "evm.coordinator-proxy" {
			if state.ExecutionImplementation != "" || state.ExecutionObservedImplementationSlot != "" || state.ExecutionProxyRuntimeHash != "" || state.ExecutionImplementationRuntimeHash != "" {
				return fmt.Errorf("historical coordinator initialization %s claims delegated execution state", row.Receipt.TransactionHash)
			}
			continue
		}
		wantExecutionSlot := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(row.ExecutionImplementation, "0x")
		if state.ExecutionImplementation != row.ExecutionImplementation || state.ExecutionObservedImplementationSlot != wantExecutionSlot || state.ExecutionProxyRuntimeHash != row.CoordinatorProxyRuntimeHash || state.ExecutionImplementationRuntimeHash != row.ExecutionImplementationRuntimeHash {
			return fmt.Errorf("historical coordinator receipt %s execution runtime differs from its archived plan", row.Receipt.TransactionHash)
		}
	}
	if err := verifyFinalSemanticCoordinatorUpgradeRange(ctx, chronologyReader, audit.UpgradeRange, audit.AllowedUpgrades, appendExchanges); err != nil {
		return err
	}
	for _, rangeEvidence := range audit.HistoricalUpgradeRanges {
		expected := finalCoordinatorUpgradeEventsForRange(audit.HistoricalUpgrades, rangeEvidence)
		if err := verifyFinalSemanticCoordinatorUpgradeRange(ctx, chronologyReader, rangeEvidence, expected, appendExchanges); err != nil {
			return err
		}
	}
	return nil
}

// Filters a canonically proxy-sorted projection into the exact event stream
// that belongs to one signed range. The caller still compares full values, so
// sharing an implementation or transaction hash across proxies cannot alias.
func finalCoordinatorUpgradeEventsForRange(values []FinalCoordinatorUpgradeEventEvidence, rangeEvidence FinalCoordinatorUpgradeRangeEvidence) []FinalCoordinatorUpgradeEventEvidence {
	result := make([]FinalCoordinatorUpgradeEventEvidence, 0)
	for _, value := range values {
		if value.Proxy == rangeEvidence.Proxy {
			result = append(result, value)
		}
	}
	return result
}

// Replays one current or historical UUPS range and compares every provider
// event with the signed list after all provider-safe chunks have been joined.
func verifyFinalSemanticCoordinatorUpgradeRange(ctx context.Context, reader FinalSemanticChronologyChainReader, rangeEvidence FinalCoordinatorUpgradeRangeEvidence, expected []FinalCoordinatorUpgradeEventEvidence, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	states, exchanges, err := reader.CoordinatorUpgradeRange(ctx, rangeEvidence)
	if err != nil {
		return fmt.Errorf("coordinator upgrade range %s: %w", rangeEvidence.Proxy, err)
	}
	if err := appendExchanges("evm", rangeEvidence.To, exchanges); err != nil {
		return err
	}
	if len(states) != len(expected) {
		return fmt.Errorf("coordinator upgrade range %s events=%d, want %d", rangeEvidence.Proxy, len(states), len(expected))
	}
	for index := range states {
		if states[index].Event != expected[index] {
			return fmt.Errorf("coordinator upgrade range %s event %d differs from the signed projection", rangeEvidence.Proxy, index)
		}
	}
	return nil
}
