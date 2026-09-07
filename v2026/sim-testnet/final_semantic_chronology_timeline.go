package main

// final_semantic_chronology_timeline.go derives executable identity from
// captured UUPS transitions. A proxy's end-of-block slot is not enough to
// identify the implementation that executed an upgrade transaction itself.

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const finalHistoricalCoordinatorTimelineArtifactSchema = "urnetwork-final-historical-coordinator-timeline-v2"

// Serializes the complete observed implementation graph independently from
// the top-level report so artifact verification can reject a substituted
// timeline before contacting a public archive endpoint.
type finalHistoricalCoordinatorTimelineArtifact struct {
	Schema       string                                            `json:"schema"`
	Timelines    []FinalHistoricalCoordinatorProxyTimelineEvidence `json:"timelines"`
	Baselines    []FinalCollectedCoordinatorBaseline               `json:"baselines"`
	UpgradedLogs []finalCanonicalEVMLog                            `json:"upgraded_logs"`
}

// Carries the source-only state needed to attach each retained proxy call to
// its execution and post-transaction implementations.
type finalHistoricalCoordinatorTimeline struct {
	proxies  map[string]FinalHistoricalCoordinatorProxyTimelineEvidence
	receipts map[string]finalHistoricalCoordinatorReceiptRuntime
}

// Retains the raw transition subset needed to independently rebuild the
// signed proxy graph. The source snapshot is the only authority for these
// logs; the derived object merely carries its immutable replay inputs.
func finalHistoricalCoordinatorTimelineArtifactFromSource(timeline *finalHistoricalCoordinatorTimeline, baselines []FinalCollectedCoordinatorBaseline, logsByTransaction map[string][]finalCanonicalEVMLog) (finalHistoricalCoordinatorTimelineArtifact, error) {
	if timeline == nil || len(baselines) == 0 || len(logsByTransaction) == 0 {
		return finalHistoricalCoordinatorTimelineArtifact{}, errors.New("historical coordinator timeline artifact source is incomplete")
	}
	timelines := timeline.evidence()
	allowed := make(map[string]bool, len(timelines))
	for _, value := range timelines {
		allowed[value.Proxy] = true
	}
	upgradedLogs := make([]finalCanonicalEVMLog, 0)
	for _, logs := range logsByTransaction {
		for _, log := range logs {
			if !allowed[log.Address] {
				continue
			}
			_, upgraded, err := finalHistoricalCoordinatorUpgradedLog(log)
			if err != nil {
				return finalHistoricalCoordinatorTimelineArtifact{}, err
			}
			if !upgraded {
				continue
			}
			copy := log
			copy.Topics = append([]string(nil), log.Topics...)
			upgradedLogs = append(upgradedLogs, copy)
		}
	}
	canonicalLogs, err := finalCanonicalizeLogs(upgradedLogs)
	if err != nil {
		return finalHistoricalCoordinatorTimelineArtifact{}, err
	}
	baselineCopy := append([]FinalCollectedCoordinatorBaseline(nil), baselines...)
	sort.Slice(baselineCopy, func(left, right int) bool { return baselineCopy[left].Proxy < baselineCopy[right].Proxy })
	return finalHistoricalCoordinatorTimelineArtifact{
		Schema:       finalHistoricalCoordinatorTimelineArtifactSchema,
		Timelines:    timelines,
		Baselines:    baselineCopy,
		UpgradedLogs: canonicalLogs,
	}, nil
}

// Holds one row's exact EVM order and both sides of a proxy state transition.
// Normal calls have equal execution and post identities; a UUPS activation
// deliberately has the old execution identity and the newly emitted post one.
type finalHistoricalCoordinatorReceiptRuntime struct {
	transactionIndex uint64
	executionHead    ChainHead
	execution        FinalHistoricalCoordinatorRuntimeIdentity
	post             FinalHistoricalCoordinatorRuntimeIdentity
	proxyRuntimeHash string
}

// Represents one captured Upgraded event after it has been joined to the
// exact finalized plan action that is allowed to produce it.
type finalHistoricalCoordinatorTransition struct {
	plan     *SetupPlan
	action   Action
	entry    JournalEntry
	position finalHistoricalCoordinatorTransactionPosition
	log      finalCanonicalEVMLog
	post     FinalHistoricalCoordinatorRuntimeIdentity
	initial  bool
}

// Parses one UUPS event only when its ABI shape, canonical coordinates, and
// indexed implementation value are complete. The generic capture index keeps
// raw logs, so this avoids trusting an event-name projection selected later.
func finalHistoricalCoordinatorUpgradedLog(value finalCanonicalEVMLog) (string, bool, error) {
	topic := strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex())
	if len(value.Topics) == 0 || !strings.EqualFold(value.Topics[0], topic) {
		return "", false, nil
	}
	if len(value.Topics) != 2 || value.Data != "0x" || !common.IsHexAddress(value.Address) || value.BlockNumber == 0 || requireFinalHex32("historical coordinator upgrade block", value.BlockHash) != nil || requireFinalHex32("historical coordinator upgrade transaction", value.TransactionHash) != nil {
		return "", false, errors.New("historical coordinator Upgraded log is malformed")
	}
	implementationTopic := common.HexToHash(value.Topics[1])
	implementation := common.BytesToAddress(implementationTopic.Bytes()[common.HashLength-common.AddressLength:])
	if implementation == (common.Address{}) {
		return "", false, errors.New("historical coordinator Upgraded implementation is zero")
	}
	return strings.ToLower(implementation.Hex()), true, nil
}

// Builds every pre-campaign UUPS transition from the authenticated journal,
// plan lineage, raw captured logs, and live baseline observations. Any extra
// proxy upgrade log is rejected instead of being treated as harmless history.
func finalHistoricalCoordinatorBuildTimeline(evidence *FinalSemanticEvidence, current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, logsByTransaction map[string][]finalCanonicalEVMLog, baselines []FinalCollectedCoordinatorBaseline) (*finalHistoricalCoordinatorTimeline, error) {
	if evidence == nil || current == nil || len(plans) == 0 || evidence.EVMCampaignStartHead.Number < 2 {
		return nil, errors.New("historical coordinator timeline inputs are incomplete")
	}
	allowed := current.allowedPlanHashes()
	proxies := make(map[string]bool, len(plans))
	for hash, plan := range plans {
		if plan == nil || !allowed[hash] || !strings.EqualFold(hash, plan.PlanHash) || plan.Deployment.CoordinatorProxy == (common.Address{}) {
			return nil, errors.New("historical coordinator timeline has an unapproved plan")
		}
		proxies[strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())] = true
	}
	transitions := make(map[string][]finalHistoricalCoordinatorTransition, len(proxies))
	byTransaction := make(map[string]finalHistoricalCoordinatorTransition)
	for _, entry := range entries {
		if entry.Stage != StageFinalized || entry.BlockNumber == 0 || entry.BlockNumber >= evidence.EVMCampaignStartHead.Number || entry.DeploymentID != evidence.DeploymentID {
			continue
		}
		plan := plans[entry.PlanHash]
		if plan == nil || !allowed[entry.PlanHash] || plan.PlanHash != entry.PlanHash {
			continue
		}
		action, actionErr := exactPlanActionByID(plan, entry.ActionID)
		if actionErr != nil || action.Kind != "evm-transaction" || !actionAcceptsIntent(action, entry.IntentHash) {
			return nil, stateMismatchError(actionErr, "historical coordinator transition action %s is not approved", entry.ActionID)
		}
		initial := action.ID == "evm.coordinator-proxy"
		activation := action.ID == "evm.coordinator-upgrade-activate"
		if !initial && !activation {
			continue
		}
		proxy := strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())
		if activation && (!common.IsHexAddress(action.Target) || !strings.EqualFold(action.Target, proxy)) {
			return nil, errors.New("historical coordinator upgrade action targets another proxy")
		}
		if _, duplicate := byTransaction[entry.TransactionHash]; duplicate {
			return nil, fmt.Errorf("historical coordinator transition transaction %s is duplicated", entry.TransactionHash)
		}
		logs := logsByTransaction[entry.TransactionHash]
		position, positionErr := finalHistoricalCoordinatorPositionFromLogs(entry, logs)
		if positionErr != nil {
			return nil, positionErr
		}
		var upgraded *finalCanonicalEVMLog
		for index := range logs {
			implementation, found, logErr := finalHistoricalCoordinatorUpgradedLog(logs[index])
			if logErr != nil {
				return nil, logErr
			}
			if !found || logs[index].Address != proxy {
				continue
			}
			if upgraded != nil {
				return nil, fmt.Errorf("historical coordinator transition %s has multiple Upgraded logs", action.ID)
			}
			copy := logs[index]
			copy.Topics = append([]string(nil), logs[index].Topics...)
			upgraded = &copy
			_ = implementation
		}
		if upgraded == nil {
			return nil, fmt.Errorf("historical coordinator transition %s has no Upgraded log", action.ID)
		}
		observed, _, observedErr := finalHistoricalCoordinatorUpgradedLog(*upgraded)
		if observedErr != nil {
			return nil, observedErr
		}
		post, postErr := finalHistoricalCoordinatorTransitionPostIdentity(plan, initial)
		if postErr != nil || !strings.EqualFold(post.Implementation, observed) {
			return nil, stateMismatchError(postErr, "historical coordinator transition %s implementation differs from its approved action", action.ID)
		}
		transition := finalHistoricalCoordinatorTransition{plan: plan, action: action, entry: entry, position: position, log: *upgraded, post: post, initial: initial}
		byTransaction[entry.TransactionHash] = transition
		transitions[proxy] = append(transitions[proxy], transition)
	}
	for transactionHash, logs := range logsByTransaction {
		for _, log := range logs {
			_, found, logErr := finalHistoricalCoordinatorUpgradedLog(log)
			if logErr != nil {
				return nil, logErr
			}
			if !found || !proxies[log.Address] {
				continue
			}
			transition, expected := byTransaction[transactionHash]
			if !expected || transition.log.LogIndex != log.LogIndex || transition.log.Address != log.Address {
				return nil, fmt.Errorf("historical coordinator proxy %s has an unapproved Upgraded log", log.Address)
			}
		}
	}
	baselineByProxy := make(map[string]FinalCollectedCoordinatorBaseline, len(baselines))
	for _, baseline := range baselines {
		if err := finalVerifyHistoricalCoordinatorBaseline(baseline); err != nil {
			return nil, err
		}
		if !proxies[baseline.Proxy] {
			return nil, fmt.Errorf("historical coordinator baseline proxy %s is outside the approved lineage", baseline.Proxy)
		}
		if _, duplicate := baselineByProxy[baseline.Proxy]; duplicate {
			return nil, fmt.Errorf("historical coordinator baseline proxy %s is duplicated", baseline.Proxy)
		}
		baselineByProxy[baseline.Proxy] = baseline
	}
	result := &finalHistoricalCoordinatorTimeline{proxies: make(map[string]FinalHistoricalCoordinatorProxyTimelineEvidence, len(proxies)), receipts: make(map[string]finalHistoricalCoordinatorReceiptRuntime)}
	for proxy := range proxies {
		items := transitions[proxy]
		if len(items) == 0 {
			return nil, fmt.Errorf("historical coordinator proxy %s has no approved initialization", proxy)
		}
		sort.Slice(items, func(left, right int) bool {
			return finalHistoricalCoordinatorTransitionLess(items[left], items[right])
		})
		if !items[0].initial {
			return nil, fmt.Errorf("historical coordinator proxy %s has no initial Upgraded transition", proxy)
		}
		if len(items) > 1 && items[1].position.block.Number == items[0].position.block.Number {
			return nil, fmt.Errorf("historical coordinator proxy %s initializes and upgrades in one block", proxy)
		}
		baseline, found := baselineByProxy[proxy]
		if !found || baseline.Head != items[0].position.block || !strings.EqualFold(baseline.Implementation, items[0].post.Implementation) || !strings.EqualFold(baseline.ImplementationRuntimeHash, items[0].post.RuntimeHash) {
			return nil, fmt.Errorf("historical coordinator proxy %s baseline is not its observed initialization", proxy)
		}
		initialization := FinalHistoricalCoordinatorUpgradeEvidence{
			PlanHash: items[0].plan.PlanHash, ActionID: items[0].action.ID, IntentHash: items[0].entry.IntentHash, TransactionHash: items[0].entry.TransactionHash,
			TransactionIndex: items[0].position.transactionIndex, LogIndex: items[0].log.LogIndex, Block: items[0].position.block,
			Execution: FinalHistoricalCoordinatorRuntimeIdentity{}, Post: items[0].post,
		}
		lineage := FinalHistoricalCoordinatorProxyTimelineEvidence{Proxy: proxy, Baseline: baseline.Head, ProxyRuntimeHash: baseline.ProxyRuntimeHash, Initialization: initialization, Upgrades: make([]FinalHistoricalCoordinatorUpgradeEvidence, 0, len(items)-1)}
		state := items[0].post
		stateHead := baseline.Head
		for index := 1; index < len(items); index++ {
			item := items[index]
			if item.initial || item.position.block.Number == stateHead.Number {
				return nil, fmt.Errorf("historical coordinator proxy %s has ambiguous same-block transitions", proxy)
			}
			if item.plan.CoordinatorUpgradeBaseline.isRepeated() && (!strings.EqualFold(item.plan.CoordinatorUpgradeBaseline.ActiveImplementation, state.Implementation) || !strings.EqualFold(item.plan.CoordinatorUpgradeBaseline.ActiveImplementationHash, state.RuntimeHash)) {
				return nil, fmt.Errorf("historical coordinator upgrade %s baseline does not chain from the preceding transition", item.action.ID)
			}
			lineage.Upgrades = append(lineage.Upgrades, FinalHistoricalCoordinatorUpgradeEvidence{
				PlanHash: item.plan.PlanHash, ActionID: item.action.ID, IntentHash: item.entry.IntentHash, TransactionHash: item.entry.TransactionHash,
				TransactionIndex: item.position.transactionIndex, LogIndex: item.log.LogIndex, Block: item.position.block,
				Execution: state, Post: item.post,
			})
			state, stateHead = item.post, item.position.block
		}
		result.proxies[proxy] = lineage
	}
	return result, nil
}

// Computes one transaction's unique EVM coordinate from its complete log
// group. All logs must agree, because a receipt cannot have two execution
// positions regardless of how many release contracts emitted from it.
func finalHistoricalCoordinatorPositionFromLogs(entry JournalEntry, logs []finalCanonicalEVMLog) (finalHistoricalCoordinatorTransactionPosition, error) {
	if entry.Stage != StageFinalized || entry.TransactionHash == "" || entry.BlockNumber == 0 || len(logs) == 0 {
		return finalHistoricalCoordinatorTransactionPosition{}, errors.New("historical coordinator transaction coordinate is incomplete")
	}
	position := finalHistoricalCoordinatorTransactionPosition{block: ChainHead{Number: entry.BlockNumber, Hash: entry.BlockHash}, transactionHash: entry.TransactionHash, transactionIndex: logs[0].TransactionIndex}
	for _, log := range logs {
		if log.BlockNumber != position.block.Number || !strings.EqualFold(log.BlockHash, position.block.Hash) || log.TransactionHash != position.transactionHash || log.TransactionIndex != position.transactionIndex {
			return finalHistoricalCoordinatorTransactionPosition{}, fmt.Errorf("historical coordinator transaction %s logs disagree on canonical position", entry.TransactionHash)
		}
	}
	return position, nil
}

// Calculates the exact accepted post-transition runtime identity from the
// plan action type. Constructor initialization uses the immutable deployment;
// a UUPS activation uses the separately content-addressed upgrade payload.
func finalHistoricalCoordinatorTransitionPostIdentity(plan *SetupPlan, initial bool) (FinalHistoricalCoordinatorRuntimeIdentity, error) {
	if plan == nil {
		return FinalHistoricalCoordinatorRuntimeIdentity{}, errors.New("historical coordinator transition plan is unavailable")
	}
	if initial {
		address := strings.ToLower(plan.Deployment.CoordinatorImplementation.Hex())
		hash := strings.ToLower(plan.Deployment.RuntimeHashes[plan.Deployment.CoordinatorImplementation.Hex()])
		if address == (common.Address{}).Hex() || requireFinalHex32("historical coordinator initial runtime", hash) != nil {
			return FinalHistoricalCoordinatorRuntimeIdentity{}, errors.New("historical coordinator initial implementation is incomplete")
		}
		return FinalHistoricalCoordinatorRuntimeIdentity{Implementation: address, RuntimeHash: hash}, nil
	}
	address := strings.ToLower(plan.CoordinatorUpgrade.Implementation.Hex())
	hash := strings.ToLower(plan.CoordinatorUpgrade.RuntimeCodeHash)
	if address == (common.Address{}).Hex() || requireFinalHex32("historical coordinator upgrade runtime", hash) != nil {
		return FinalHistoricalCoordinatorRuntimeIdentity{}, errors.New("historical coordinator upgrade implementation is incomplete")
	}
	return FinalHistoricalCoordinatorRuntimeIdentity{Implementation: address, RuntimeHash: hash}, nil
}

// Orders transitions by the only chain order that matters for execution:
// block, transaction index, then log index. Duplicate coordinates are caught
// by the caller instead of using an arbitrary transaction-hash tie breaker.
func finalHistoricalCoordinatorTransitionLess(left, right finalHistoricalCoordinatorTransition) bool {
	if left.position.block.Number != right.position.block.Number {
		return left.position.block.Number < right.position.block.Number
	}
	if left.position.transactionIndex != right.position.transactionIndex {
		return left.position.transactionIndex < right.position.transactionIndex
	}
	return left.log.LogIndex < right.log.LogIndex
}

// Validates the captured post-construction observation before it becomes a
// timeline baseline. It must remain an exact canonical EIP-1898 checkpoint.
func finalVerifyHistoricalCoordinatorBaseline(value FinalCollectedCoordinatorBaseline) error {
	proxy, proxyErr := finalCanonicalAddress(value.Proxy)
	implementation, implementationErr := finalCanonicalAddress(value.Implementation)
	if proxyErr != nil || implementationErr != nil || proxy != value.Proxy || implementation != value.Implementation || proxy == (common.Address{}).Hex() || implementation == (common.Address{}).Hex() {
		return stateMismatchError(errors.Join(proxyErr, implementationErr), "historical coordinator baseline address is not canonical")
	}
	if err := verifyFinalHead("historical coordinator baseline", value.Head); err != nil {
		return err
	}
	if err := requireFinalHex32("historical coordinator baseline implementation runtime", value.ImplementationRuntimeHash); err != nil {
		return err
	}
	return requireFinalHex32("historical coordinator baseline proxy runtime", value.ProxyRuntimeHash)
}

// Selects the exact execution/post identities for a carried coordinator row.
// A non-upgrade in any upgrade block is rejected: historical block-state RPC
// exposes only the end-of-block slot and cannot prove an earlier dispatch.
func (self *finalHistoricalCoordinatorTimeline) receiptRuntime(plan *SetupPlan, action Action, entry JournalEntry, logs []finalCanonicalEVMLog) (finalHistoricalCoordinatorReceiptRuntime, error) {
	if self == nil || plan == nil || entry.Stage != StageFinalized || len(logs) == 0 {
		return finalHistoricalCoordinatorReceiptRuntime{}, errors.New("historical coordinator receipt timeline inputs are incomplete")
	}
	proxy := strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())
	lineage, found := self.proxies[proxy]
	if !found {
		return finalHistoricalCoordinatorReceiptRuntime{}, fmt.Errorf("historical coordinator receipt proxy %s has no timeline", proxy)
	}
	position, err := finalHistoricalCoordinatorPositionFromLogs(entry, logs)
	if err != nil {
		return finalHistoricalCoordinatorReceiptRuntime{}, err
	}
	if entry.TransactionHash == lineage.Initialization.TransactionHash {
		if action.ID != lineage.Initialization.ActionID || entry.IntentHash != lineage.Initialization.IntentHash || position.transactionIndex != lineage.Initialization.TransactionIndex {
			return finalHistoricalCoordinatorReceiptRuntime{}, errors.New("historical coordinator initialization receipt differs from its transition")
		}
		return finalHistoricalCoordinatorReceiptRuntime{
			transactionIndex: position.transactionIndex,
			execution:        FinalHistoricalCoordinatorRuntimeIdentity{},
			post:             lineage.Initialization.Post,
			proxyRuntimeHash: lineage.ProxyRuntimeHash,
		}, nil
	}
	state := lineage.Initialization.Post
	stateHead := lineage.Baseline
	for _, transition := range lineage.Upgrades {
		if transition.Block.Number == position.block.Number && transition.TransactionIndex != position.transactionIndex {
			return finalHistoricalCoordinatorReceiptRuntime{}, fmt.Errorf("historical coordinator receipt %s shares an upgrade block and cannot be attributed", entry.TransactionHash)
		}
		if transition.TransactionHash == entry.TransactionHash {
			if action.ID != transition.ActionID || entry.IntentHash != transition.IntentHash || position.transactionIndex != transition.TransactionIndex {
				return finalHistoricalCoordinatorReceiptRuntime{}, errors.New("historical coordinator upgrade receipt differs from its transition")
			}
			return finalHistoricalCoordinatorReceiptRuntime{transactionIndex: position.transactionIndex, executionHead: stateHead, execution: transition.Execution, post: transition.Post, proxyRuntimeHash: lineage.ProxyRuntimeHash}, nil
		}
		transitionPosition := finalHistoricalCoordinatorTransactionPosition{block: transition.Block, transactionIndex: transition.TransactionIndex, transactionHash: transition.TransactionHash}
		before, orderErr := finalHistoricalCoordinatorPositionBefore(transitionPosition, position)
		if orderErr != nil {
			return finalHistoricalCoordinatorReceiptRuntime{}, orderErr
		}
		if !before {
			break
		}
		state, stateHead = transition.Post, transition.Block
	}
	return finalHistoricalCoordinatorReceiptRuntime{transactionIndex: position.transactionIndex, executionHead: stateHead, execution: state, post: state, proxyRuntimeHash: lineage.ProxyRuntimeHash}, nil
}

// Produces a stable evidence slice from proxy-indexed intermediate state.
// Maps are never serialized directly because their iteration order is not a
// consensus ordering for a signed final report.
func (self *finalHistoricalCoordinatorTimeline) evidence() []FinalHistoricalCoordinatorProxyTimelineEvidence {
	if self == nil {
		return nil
	}
	result := make([]FinalHistoricalCoordinatorProxyTimelineEvidence, 0, len(self.proxies))
	for _, value := range self.proxies {
		copy := value
		copy.Upgrades = append([]FinalHistoricalCoordinatorUpgradeEvidence(nil), value.Upgrades...)
		result = append(result, copy)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Proxy < result[right].Proxy })
	return result
}

// Verifies that serialized proxy timelines retain canonical order and do not
// duplicate a proxy coordinate before any artifact or public replay consumes
// their execution identities.
func verifyFinalHistoricalCoordinatorTimeline(values []FinalHistoricalCoordinatorProxyTimelineEvidence) error {
	if len(values) == 0 {
		return errors.New("historical coordinator implementation timeline is absent")
	}
	for index := range values {
		value := &values[index]
		proxy, err := finalCanonicalAddress(value.Proxy)
		if err != nil || proxy != value.Proxy || index > 0 && value.Proxy <= values[index-1].Proxy {
			return stateMismatchError(err, "historical coordinator implementation timeline is not canonical")
		}
		if err := verifyFinalHead("historical coordinator timeline baseline", value.Baseline); err != nil {
			return err
		}
		if err := requireFinalHex32("historical coordinator timeline proxy runtime", value.ProxyRuntimeHash); err != nil {
			return err
		}
		if err := finalVerifyHistoricalCoordinatorInitialization(value.Initialization, value.Baseline); err != nil {
			return err
		}
		prior := value.Initialization
		for upgradeIndex := range value.Upgrades {
			upgrade := &value.Upgrades[upgradeIndex]
			if err := finalVerifyHistoricalCoordinatorUpgradeEvidence(*upgrade); err != nil {
				return err
			}
			// Archive RPC can observe only a block's final storage. Requiring a
			// later block for each transition prevents a pre-upgrade row from
			// being attributed with the same block's post-upgrade slot.
			if upgrade.Block.Number <= prior.Block.Number {
				return errors.New("historical coordinator timeline transitions share or reverse a block")
			}
			if !finalHistoricalCoordinatorUpgradeBefore(prior, *upgrade) {
				return errors.New("historical coordinator timeline upgrades are not strictly ordered")
			}
			if upgrade.Execution != prior.Post {
				return errors.New("historical coordinator upgrade execution does not chain from the preceding post state")
			}
			prior = *upgrade
		}
	}
	return nil
}

// Checks the signed fields for one activation transition independently from
// timeline linkage. Its action and intent later bind the same journal row.
func finalVerifyHistoricalCoordinatorUpgradeEvidence(value FinalHistoricalCoordinatorUpgradeEvidence) error {
	if err := requireFinalHex32("historical coordinator upgrade plan", value.PlanHash); err != nil || value.ActionID != "evm.coordinator-upgrade-activate" || value.IntentHash == "" || value.TransactionHash == "" {
		return stateMismatchError(err, "historical coordinator upgrade identity is incomplete")
	}
	if err := requireFinalHex32("historical coordinator upgrade intent", value.IntentHash); err != nil {
		return err
	}
	if err := requireFinalHex32("historical coordinator upgrade transaction", value.TransactionHash); err != nil {
		return err
	}
	if err := verifyFinalHead("historical coordinator upgrade block", value.Block); err != nil {
		return err
	}
	if err := finalVerifyHistoricalCoordinatorRuntimeIdentity("historical coordinator upgrade execution", value.Execution); err != nil {
		return err
	}
	return finalVerifyHistoricalCoordinatorRuntimeIdentity("historical coordinator upgrade post", value.Post)
}

// Checks the constructor-emitted UUPS event without pretending that proxy
// creation dispatched through an already installed implementation. Its
// execution and post identities are intentionally equal to the initialized
// implementation, while the action ID distinguishes it from an activation.
func finalVerifyHistoricalCoordinatorInitialization(value FinalHistoricalCoordinatorUpgradeEvidence, baseline ChainHead) error {
	if err := requireFinalHex32("historical coordinator initialization plan", value.PlanHash); err != nil || value.ActionID != "evm.coordinator-proxy" || value.IntentHash == "" || value.TransactionHash == "" {
		return stateMismatchError(err, "historical coordinator initialization identity is incomplete")
	}
	if err := requireFinalHex32("historical coordinator initialization intent", value.IntentHash); err != nil {
		return err
	}
	if err := requireFinalHex32("historical coordinator initialization transaction", value.TransactionHash); err != nil {
		return err
	}
	if err := verifyFinalHead("historical coordinator initialization block", value.Block); err != nil || value.Block != baseline {
		return stateMismatchError(err, "historical coordinator initialization block differs from its baseline")
	}
	if err := finalVerifyHistoricalCoordinatorRuntimeIdentity("historical coordinator initialization post", value.Post); err != nil {
		return err
	}
	if value.Execution != (FinalHistoricalCoordinatorRuntimeIdentity{}) {
		return errors.New("historical coordinator initialization has a delegated execution identity")
	}
	return nil
}

// Validates an address/hash pair without permitting zero addresses, aliases,
// or an absent runtime hash in a claimed executable identity.
func finalVerifyHistoricalCoordinatorRuntimeIdentity(label string, value FinalHistoricalCoordinatorRuntimeIdentity) error {
	address, err := finalCanonicalAddress(value.Implementation)
	if err != nil || address != value.Implementation || address == (common.Address{}).Hex() {
		return stateMismatchError(err, "%s address is not canonical", label)
	}
	return requireFinalHex32(label+" runtime", value.RuntimeHash)
}

// Compares two transitions in actual EVM execution order, including log index
// only after distinct transaction indexes have already established order.
func finalHistoricalCoordinatorUpgradeBefore(left, right FinalHistoricalCoordinatorUpgradeEvidence) bool {
	if left.Block.Number != right.Block.Number {
		return left.Block.Number < right.Block.Number
	}
	if left.TransactionIndex != right.TransactionIndex {
		return left.TransactionIndex < right.TransactionIndex
	}
	return left.LogIndex < right.LogIndex
}

// Tests whether two signed timeline lists contain the same proxy graph while
// keeping equality comparisons explicit for future non-comparable additions.
func finalHistoricalCoordinatorTimelinesEqual(left, right []FinalHistoricalCoordinatorProxyTimelineEvidence) bool {
	return slices.EqualFunc(left, right, func(a, b FinalHistoricalCoordinatorProxyTimelineEvidence) bool {
		return finalJSONEqual(a, b)
	})
}

// Strict-decodes the separately content-addressed implementation graph and
// compares it byte-for-semantic-byte with the signed top-level projection.
// This makes a missing or substituted timeline fail before public RPC replay.
func verifyFinalHistoricalCoordinatorTimelineArtifact(evidence *FinalSemanticEvidence, current *SetupPlan, plans map[string]*SetupPlan, entries []JournalEntry, data []byte) error {
	if evidence == nil || current == nil || len(plans) == 0 || len(entries) == 0 || len(data) == 0 {
		return errors.New("historical coordinator timeline artifact is unavailable")
	}
	var artifact finalHistoricalCoordinatorTimelineArtifact
	if err := decodeStrictJSONBytes(data, &artifact); err != nil {
		return err
	}
	if artifact.Schema != finalHistoricalCoordinatorTimelineArtifactSchema || !finalHistoricalCoordinatorTimelinesEqual(artifact.Timelines, evidence.HistoricalCoordinatorTimeline) {
		return errors.New("historical coordinator timeline artifact differs from signed evidence")
	}
	if err := verifyFinalHistoricalCoordinatorTimeline(artifact.Timelines); err != nil {
		return err
	}
	if len(artifact.Baselines) != len(artifact.Timelines) {
		return errors.New("historical coordinator timeline artifact baseline count differs")
	}
	for index := range artifact.Baselines {
		baseline := artifact.Baselines[index]
		if err := finalVerifyHistoricalCoordinatorBaseline(baseline); err != nil {
			return err
		}
		if index > 0 && baseline.Proxy <= artifact.Baselines[index-1].Proxy {
			return errors.New("historical coordinator timeline artifact baselines are not canonical")
		}
		lineage := artifact.Timelines[index]
		if baseline.Proxy != lineage.Proxy || baseline.Head != lineage.Baseline || baseline.Implementation != lineage.Initialization.Post.Implementation || baseline.ImplementationRuntimeHash != lineage.Initialization.Post.RuntimeHash || baseline.ProxyRuntimeHash != lineage.ProxyRuntimeHash {
			return errors.New("historical coordinator timeline artifact baseline differs from its initialization")
		}
	}
	canonicalLogs, err := finalCanonicalizeLogs(artifact.UpgradedLogs)
	if err != nil || !slices.EqualFunc(canonicalLogs, artifact.UpgradedLogs, finalSemanticCanonicalLogEqual) {
		return stateMismatchError(err, "historical coordinator timeline artifact upgraded logs are not canonical")
	}
	allowedProxy := make(map[string]bool, len(artifact.Timelines))
	for _, timeline := range artifact.Timelines {
		allowedProxy[timeline.Proxy] = true
	}
	logsByTransaction := make(map[string][]finalCanonicalEVMLog)
	for _, log := range artifact.UpgradedLogs {
		if !allowedProxy[log.Address] {
			return errors.New("historical coordinator timeline artifact contains an unapproved proxy log")
		}
		logsByTransaction[log.TransactionHash] = append(logsByTransaction[log.TransactionHash], log)
	}
	reconstructed, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logsByTransaction, artifact.Baselines)
	if err != nil {
		return fmt.Errorf("rebuild historical coordinator timeline artifact: %w", err)
	}
	if !finalHistoricalCoordinatorTimelinesEqual(reconstructed.evidence(), artifact.Timelines) {
		return errors.New("historical coordinator timeline artifact differs from its plan and raw-log reconstruction")
	}
	return nil
}

// Reconstructs one retained row's runtime expectation from its signed proxy
// timeline. It deliberately rejects any other row in an upgrade block because
// archive block-state queries expose only post-block storage, not call traces.
func finalVerifyHistoricalCoordinatorReceiptTimeline(row *FinalHistoricalCoordinatorReceiptEvidence, timelines []FinalHistoricalCoordinatorProxyTimelineEvidence) error {
	if row == nil {
		return errors.New("historical coordinator receipt timeline row is unavailable")
	}
	var lineage *FinalHistoricalCoordinatorProxyTimelineEvidence
	for index := range timelines {
		if timelines[index].Proxy != row.CoordinatorProxy {
			continue
		}
		if lineage != nil {
			return errors.New("historical coordinator receipt timeline proxy is duplicated")
		}
		lineage = &timelines[index]
	}
	if lineage == nil {
		return errors.New("historical coordinator receipt has no proxy timeline")
	}
	if row.Receipt.TransactionHash == lineage.Initialization.TransactionHash {
		if row.ActionID != lineage.Initialization.ActionID || row.IntentHash != lineage.Initialization.IntentHash || row.PlanHash != lineage.Initialization.PlanHash || row.TransactionIndex != lineage.Initialization.TransactionIndex || row.ExecutionHead != (ChainHead{}) || row.ExecutionImplementation != "" || row.ExecutionImplementationRuntimeHash != "" || row.CoordinatorImplementation != lineage.Initialization.Post.Implementation || row.CoordinatorImplementationRuntimeHash != lineage.Initialization.Post.RuntimeHash || row.CoordinatorProxyRuntimeHash != lineage.ProxyRuntimeHash {
			return errors.New("historical coordinator initialization receipt differs from its proxy timeline")
		}
		return nil
	}
	state := lineage.Initialization.Post
	stateHead := lineage.Baseline
	for _, upgrade := range lineage.Upgrades {
		if upgrade.Block.Number == row.Receipt.Block.Number && upgrade.TransactionIndex != row.TransactionIndex {
			return errors.New("historical coordinator receipt shares an upgrade block and cannot be attributed")
		}
		if upgrade.TransactionHash == row.Receipt.TransactionHash {
			if row.ActionID != upgrade.ActionID || row.IntentHash != upgrade.IntentHash || row.PlanHash != upgrade.PlanHash || row.TransactionIndex != upgrade.TransactionIndex || row.ExecutionHead != stateHead || row.ExecutionImplementation != upgrade.Execution.Implementation || row.ExecutionImplementationRuntimeHash != upgrade.Execution.RuntimeHash || row.CoordinatorImplementation != upgrade.Post.Implementation || row.CoordinatorImplementationRuntimeHash != upgrade.Post.RuntimeHash || row.CoordinatorProxyRuntimeHash != lineage.ProxyRuntimeHash {
				return errors.New("historical coordinator upgrade receipt differs from its proxy timeline")
			}
			return nil
		}
		position := finalHistoricalCoordinatorTransactionPosition{block: upgrade.Block, transactionIndex: upgrade.TransactionIndex, transactionHash: upgrade.TransactionHash}
		rowPosition := finalHistoricalCoordinatorTransactionPosition{block: row.Receipt.Block, transactionIndex: row.TransactionIndex, transactionHash: row.Receipt.TransactionHash}
		before, err := finalHistoricalCoordinatorPositionBefore(position, rowPosition)
		if err != nil {
			return err
		}
		if !before {
			break
		}
		state, stateHead = upgrade.Post, upgrade.Block
	}
	if row.ExecutionHead != stateHead || row.ExecutionImplementation != state.Implementation || row.ExecutionImplementationRuntimeHash != state.RuntimeHash || row.CoordinatorImplementation != state.Implementation || row.CoordinatorImplementationRuntimeHash != state.RuntimeHash || row.CoordinatorProxyRuntimeHash != lineage.ProxyRuntimeHash {
		return errors.New("historical coordinator receipt differs from its proxy execution timeline")
	}
	return nil
}
