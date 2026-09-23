// A second immutable probe can replace a deployed first successor only after
// its exact CREATE and battery, followed by a seed failure before broadcast.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Freezes the first successor and its complete journal prefix. Original native
// commitment evidence remains in the outer descriptor and is never relabelled.
type PrecompileProbeRetirement struct {
	SourcePlanHash  string                        `json:"source_plan_hash"`
	Predecessor     *PrecompileProbeSuccessor     `json:"predecessor"`
	JournalSequence uint64                        `json:"journal_sequence"`
	JournalHash     string                        `json:"journal_hash"`
	Create          JournalEntry                  `json:"create"`
	Battery         JournalEntry                  `json:"battery"`
	SeedFailure     JournalEntry                  `json:"seed_failure"`
	Evidence        PrecompileConformanceEvidence `json:"evidence"`
}

// The new nonce prefix starts after the complete retired generation, while the
// original prefix remains available for authenticating the native drill.
func precompileProbeSuccessorJournalBoundary(successor *PrecompileProbeSuccessor) uint64 {
	if successor.Retirement != nil {
		return successor.Retirement.JournalSequence
	}
	return successor.JournalSequence
}

// Accepts only the bounded v1-to-v2 transition, preserving every native-source
// field and the predecessor's complete descriptor, including its old bytes.
func validatePrecompileProbeRetirement(plan *SetupPlan) error {
	successor := plan.PrecompileProbeSuccessor
	retirement := successor.Retirement
	if successor.Schema == "urnetwork-precompile-probe-successor-v1" {
		if retirement != nil {
			return errors.New("first precompile probe successor cannot contain a retirement")
		}
		return nil
	}
	if retirement == nil || retirement.Predecessor == nil || retirement.Predecessor.Schema != "urnetwork-precompile-probe-successor-v1" || retirement.Predecessor.Retirement != nil || !validCanonicalHashHex(retirement.SourcePlanHash) || retirement.SourcePlanHash == plan.PlanHash || !plan.allowedPlanHashes()[retirement.SourcePlanHash] {
		return errors.New("second precompile probe successor has no exact first-generation source")
	}
	predecessor := retirement.Predecessor
	previous := *plan
	previous.PrecompileProbeSuccessor = predecessor
	if err := validatePrecompileProbeSuccessor(&previous); err != nil {
		return err
	}
	native := *successor
	native.Schema, native.Probe, native.DeployerNonce = predecessor.Schema, predecessor.Probe, predecessor.DeployerNonce
	native.RuntimeHash, native.CreationHash, native.FinalizedHead = predecessor.RuntimeHash, predecessor.CreationHash, predecessor.FinalizedHead
	native.Retirement = nil
	if !reflect.DeepEqual(&native, predecessor) || successor.RuntimeHash == predecessor.RuntimeHash || successor.CreationHash == predecessor.CreationHash || strings.EqualFold(successor.Probe, predecessor.Probe) || successor.FinalizedHead != retirement.Evidence.Battery.FinalizedHead || successor.FinalizedHead.Number <= predecessor.FinalizedHead.Number {
		return errors.New("second precompile probe successor changed native lineage or reused deployed identity")
	}
	if retirement.JournalSequence <= predecessor.JournalSequence || !validCanonicalHashHex(retirement.JournalHash) || retirement.Create.Sequence <= predecessor.JournalSequence || retirement.Battery.Sequence <= retirement.Create.Sequence || retirement.SeedFailure.Sequence <= retirement.Battery.Sequence || retirement.SeedFailure.Sequence > retirement.JournalSequence {
		return errors.New("precompile probe retirement reordered its completed phases")
	}
	for index, entry := range []JournalEntry{retirement.Create, retirement.Battery, retirement.SeedFailure} {
		want := []string{"precompile.probe-deploy", "precompile.read-battery", "precompile.seed"}[index]
		if entry.ActionID != want || entry.DeploymentID != plan.DeploymentID || !plan.allowedPlanHashes()[entry.PlanHash] || !validCanonicalHashHex(entry.IntentHash) {
			return errors.New("precompile probe retirement changed phase identity")
		}
		if index < 2 && (entry.Stage != StageVerified || !validCanonicalHashHex(entry.PostconditionHash)) || index == 2 && (entry.Stage != StageFailed || entry.TransactionHash != "" || entry.BlockNumber != 0 || entry.BlockHash != "" || entry.Nonce != "" || entry.Signer != "") {
			return errors.New("precompile probe retirement lacks verified history or contains a seed broadcast")
		}
	}
	return validatePrecompileProbeSeedFailureEvidence(plan, predecessor, &retirement.Evidence)
}

// Saved seed input is allowed; any recorded value effect requires recovery on
// that same probe. The evidence hash and all original role identities are exact.
func validatePrecompileProbeSeedFailureEvidence(plan *SetupPlan, predecessor *PrecompileProbeSuccessor, evidence *PrecompileConformanceEvidence) error {
	if plan == nil || predecessor == nil || evidence == nil {
		return errors.New("precompile seed retirement evidence is absent")
	}
	copy := *evidence
	want := copy.EvidenceHash
	copy.EvidenceHash = ""
	hash, err := canonicalHashHex(&copy)
	if err != nil || hash != want || !validCanonicalHashHex(want) {
		return errors.New("precompile seed retirement evidence hash differs")
	}
	original := predecessor.Evidence
	coldkey := ss58Mirror(common.HexToAddress(predecessor.Probe))
	if evidence.Schema != original.Schema || evidence.ProbeAddress != predecessor.Probe || evidence.ProbeColdkey != hexBytesValue(coldkey[:]) || evidence.DeploymentID != plan.DeploymentID || evidence.ChainID != plan.ChainID || evidence.GenesisHash != plan.GenesisHash || evidence.Netuid != plan.Netuid || evidence.PolicyHash != plan.PolicyHash || evidence.Owner != original.Owner || evidence.SampleHotkey != original.SampleHotkey || evidence.SampleUID != original.SampleUID || evidence.AbsentHotkey != original.AbsentHotkey || evidence.MoveHotkey != original.MoveHotkey || evidence.RecoveryColdkey != original.RecoveryColdkey || evidence.Commitment != original.Commitment || !reflect.DeepEqual(evidence.CommitmentSource, precompileProbeCommitmentSource(predecessor)) {
		return errors.New("precompile seed retirement changed original proof or custody roles")
	}
	value := new(big.Int).Mul(new(big.Int).SetUint64(plan.LiveFacts.ProbeTAORao), big.NewInt(1_000_000_000)).String()
	wantSeed := PrecompileValueStep{TAORao: plan.LiveFacts.ProbeTAORao, ValueWei: value}
	if plan.LiveFacts.ProbeTAORao == 0 || evidence.Seed != wantSeed || evidence.Complete || evidence.Recovery != nil || evidence.RoundTripCredits != nil || !evidence.Battery.Passed || evidence.Battery.SampleSelfStake != "0" || evidence.Battery.FinalizedHead.Number == 0 || !validCanonicalHashHex(evidence.Battery.FinalizedHead.Hash) || evidence.Forward != (PrecompileMoveStep{}) || evidence.Back != (PrecompileMoveStep{}) || evidence.Snapshot != (PrecompileSnapshotStep{}) || evidence.Dividend != (PrecompileDividendStep{}) || evidence.Transfer != (PrecompileTransferStep{}) {
		return errors.New("precompile seed retirement contains value progress or lacks its successful battery")
	}
	return nil
}

// Authenticates every probe entry after v1 admission, including older gas-only
// approvals. A failed write with a broadcast hash is never safe to abandon.
func precompileProbeRetirementEntries(plan *SetupPlan, entries []JournalEntry, readSource func(string) (*SetupPlan, error)) (JournalEntry, JournalEntry, JournalEntry, error) {
	var create, battery, failure JournalEntry
	if err := validatePrecompileProbeSuccessorActions(plan); err != nil || plan.PrecompileProbeSuccessor == nil || plan.PrecompileProbeSuccessor.Retirement != nil {
		return create, battery, failure, errors.Join(errors.New("precompile retirement requires the first successor"), err)
	}
	allowed := plan.allowedPlanHashes()
	for _, entry := range entries {
		if entry.Sequence <= plan.PrecompileProbeSuccessor.JournalSequence || !strings.HasPrefix(entry.ActionID, "precompile.") {
			continue
		}
		if entry.DeploymentID != plan.DeploymentID || !allowed[entry.PlanHash] {
			return create, battery, failure, errors.New("precompile retirement found foreign journal history")
		}
		source, err := readSource(entry.PlanHash)
		if err != nil {
			return create, battery, failure, err
		}
		if err := validatePrecompileEvidenceCarryScope(plan, source, approvedPrecompileProbe(plan)); err != nil {
			return create, battery, failure, err
		}
		action, err := exactPlanActionByID(source, entry.ActionID)
		if err != nil || action.IntentHash != entry.IntentHash {
			return create, battery, failure, errors.New("precompile retirement journal differs from its source action")
		}
		switch entry.ActionID {
		case "precompile.probe-deploy":
			if entry.Stage == StageVerified {
				create = entry
			}
		case "precompile.read-battery":
			if entry.TransactionHash != "" || entry.Stage != StageIntent && entry.Stage != StageVerified {
				return create, battery, failure, errors.New("precompile retirement has an unsupported battery attempt")
			}
			if entry.Stage == StageVerified {
				battery = entry
			}
		case "precompile.seed":
			if entry.Stage != StageIntent && entry.Stage != StageFailed || entry.TransactionHash != "" || entry.BlockNumber != 0 || entry.BlockHash != "" || entry.Nonce != "" || entry.Signer != "" {
				return create, battery, failure, errors.New("precompile retirement cannot abandon a broadcast or verified seed")
			}
			if entry.Stage == StageFailed {
				failure = entry
			} else {
				failure = JournalEntry{}
			}
		default:
			return create, battery, failure, fmt.Errorf("precompile retirement has later phase progress: %s", entry.ActionID)
		}
	}
	if create.Sequence == 0 || battery.Sequence <= create.Sequence || failure.Sequence <= battery.Sequence {
		return create, battery, failure, errors.New("precompile retirement requires CREATE, battery and unbroadcast seed failure")
	}
	return create, battery, failure, nil
}

// Loads source approvals once per verification; the archive reader checks each
// content hash before cached content is used by another entry in that prefix.
func precompileProbeRetirementPlanReader(stateDir string, plan *SetupPlan) func(string) (*SetupPlan, error) {
	plans := map[string]*SetupPlan{plan.PlanHash: plan}
	return func(hash string) (*SetupPlan, error) {
		if source, ok := plans[hash]; ok {
			return source, nil
		}
		source, err := readValidatorEvidenceHistoricalPlan(stateDir, hash)
		if err == nil {
			plans[hash] = source
		}
		return source, err
	}
}

// Preserves the original native source archive and independently authenticates
// the first deployed successor, its exact receipts, and its failed seed prefix.
func readPrecompileProbeRetirementSource(cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry) (*SetupPlan, error) {
	retirement := plan.PrecompileProbeSuccessor.Retirement
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, retirement.SourcePlanHash)
	if err != nil {
		return nil, err
	}
	if err := validatePrecompileProbeRetirementSource(cfg, stateDir, plan, source, entries); err != nil {
		return nil, err
	}
	return source, nil
}

// Only the probe generation changes: retained custody, repair, policies and
// value inputs must still match the authenticated predecessor approval.
func validatePrecompileProbeRetirementSource(cfg *ResolvedConfig, stateDir string, plan, source *SetupPlan, entries []JournalEntry) error {
	if err := validatePrecompileProbeSuccessor(plan); err != nil {
		return err
	}
	retirement := plan.PrecompileProbeSuccessor.Retirement
	if retirement == nil || source == nil || source.PlanHash != retirement.SourcePlanHash || !reflect.DeepEqual(source.PrecompileProbeSuccessor, retirement.Predecessor) {
		return errors.New("precompile retirement changed its deployed predecessor descriptor")
	}
	baseline := source.CoordinatorUpgradeBaseline
	baseline.ReleaseDeploymentHash = plan.CoordinatorUpgradeBaseline.ReleaseDeploymentHash
	if source.DeploymentID != plan.DeploymentID || source.ChainID != plan.ChainID || source.GenesisHash != plan.GenesisHash || source.Netuid != plan.Netuid || source.PolicyHash != plan.PolicyHash || source.Owner != plan.Owner || !reflect.DeepEqual(source.Roles, plan.Roles) || !contractDeploymentAddressesEqual(source.Deployment, plan.Deployment) || !contractDeploymentRuntimeHashesCompatible(source.Deployment, plan.Deployment) || source.CoordinatorUpgrade != plan.CoordinatorUpgrade || baseline != plan.CoordinatorUpgradeBaseline || !reflect.DeepEqual(source.CoordinatorRepairCarry, plan.CoordinatorRepairCarry) || source.LiveFacts.ProbeTAORao != plan.LiveFacts.ProbeTAORao || source.LiveFacts.NominatorMinimumRao != plan.LiveFacts.NominatorMinimumRao {
		return errors.New("precompile retirement changed retained deployment, policy or value input")
	}
	var prefix []JournalEntry
	for _, entry := range entries {
		if entry.Sequence <= retirement.JournalSequence {
			prefix = append(prefix, entry)
		}
	}
	if len(prefix) == 0 || prefix[len(prefix)-1].Sequence != retirement.JournalSequence || prefix[len(prefix)-1].EntryHash != retirement.JournalHash {
		return errors.New("precompile retirement changed its complete journal prefix")
	}
	readSource := precompileProbeRetirementPlanReader(stateDir, source)
	create, battery, failure, err := precompileProbeRetirementEntries(source, prefix, readSource)
	if err != nil || !reflect.DeepEqual(create, retirement.Create) || !reflect.DeepEqual(battery, retirement.Battery) || !reflect.DeepEqual(failure, retirement.SeedFailure) {
		return errors.Join(errors.New("precompile retirement changed exact phase references"), err)
	}
	owner := &Executor{cfg: historicalPlanConfig(cfg, source), stateDir: stateDir, plan: source, journal: &Journal{entries: prefix}}
	if err := owner.validatePrecompileEvidence(approvedPrecompileProbe(source), &retirement.Evidence); err != nil {
		return err
	}
	for _, entry := range []JournalEntry{create, battery} {
		record, err := owner.readPersistedPostcondition(entry)
		if err != nil {
			return err
		}
		action, err := exactPlanActionByID(source, entry.ActionID)
		if err != nil {
			return err
		}
		for _, observed := range []map[string]any{record.Observed, record.IndependentObserved} {
			if entry.ActionID == "precompile.probe-deploy" {
				want := map[string]any{"kind": action.Kind, "target": action.Target, "address": retirement.Predecessor.Probe, "runtime_hash": retirement.Predecessor.RuntimeHash}
				if err := observedPostconditionMatches(observed, want); err != nil {
					return err
				}
			} else {
				copy := *record
				copy.Observed = observed
				if _, err := precompileBatteryHistoricalEvidence(action, &retirement.Evidence, &copy); err != nil {
					return err
				}
			}
		}
	}
	_, err = precompileProbeSuccessorPrefix(source, prefix, retirement.Predecessor.DeployerNonce+1)
	return err
}

// The fixed, authenticated battery block anchors approval; advancing heads only
// trigger fresh custody checks and cannot silently rewrite the candidate hash.
func newPrecompileProbeSeedSuccessor(cfg *ResolvedConfig, stateDir string, prior *SetupPlan, built *DeploymentPayloads, entries []JournalEntry, head ChainHead, nonce uint64) (*PrecompileProbeSuccessor, error) {
	if prior == nil || prior.PrecompileProbeSuccessor == nil || prior.PrecompileProbeSuccessor.Retirement != nil || built == nil || len(entries) == 0 || prior.PrecompileProbeSuccessor.DeployerNonce == ^uint64(0) || nonce != prior.PrecompileProbeSuccessor.DeployerNonce+1 {
		return nil, errors.New("precompile seed successor requires the exact first deployed probe nonce")
	}
	evidence, err := loadPrecompileEvidence(stateDir)
	if err != nil {
		return nil, err
	}
	create, battery, failure, err := precompileProbeRetirementEntries(prior, entries, precompileProbeRetirementPlanReader(stateDir, prior))
	if err != nil {
		return nil, err
	}
	if err := configurePrecompileProbeNonce(built, nonce); err != nil {
		return nil, err
	}
	predecessor := *prior.PrecompileProbeSuccessor
	successor := predecessor
	successor.Schema = "urnetwork-precompile-probe-successor-v2"
	successor.Probe, successor.DeployerNonce = built.PrecompileProbeAddress.Hex(), nonce
	successor.RuntimeHash, successor.CreationHash = crypto.Keccak256Hash(built.ExpectedRuntime[built.PrecompileProbeAddress]).Hex(), crypto.Keccak256Hash(built.PrecompileProbe).Hex()
	successor.FinalizedHead = evidence.Battery.FinalizedHead
	successor.Retirement = &PrecompileProbeRetirement{SourcePlanHash: prior.PlanHash, Predecessor: &predecessor, JournalSequence: failure.Sequence, JournalHash: failure.EntryHash, Create: create, Battery: battery, SeedFailure: failure, Evidence: *evidence}
	if head.Number < successor.FinalizedHead.Number || head.Number == successor.FinalizedHead.Number && head.Hash != successor.FinalizedHead.Hash {
		return nil, errors.New("precompile seed successor battery is not finalized")
	}
	candidate := *prior
	candidate.PlanHash = ""
	candidate.PriorPlanHashes = append(append([]string(nil), prior.PriorPlanHashes...), prior.PlanHash)
	candidate.PrecompileProbeSuccessor = &successor
	if err := validatePrecompileProbeRetirementSource(cfg, stateDir, &candidate, prior, entries); err != nil {
		return nil, err
	}
	return &successor, nil
}

// Both retired and new probe custody is empty at the stable admission block
// and again before CREATE. Later recovery authenticates the historical boundary.
func verifyPrecompileProbeRetirementCustody(ctx context.Context, successor *PrecompileProbeSuccessor, head ChainHead, nonce uint64, balanceAt func(context.Context, common.Address, *big.Int) (*big.Int, error), stakeAt func(context.Context, uint64, [32]byte, [32]byte) (uint64, error)) error {
	retirement := successor.Retirement
	if retirement == nil {
		return nil
	}
	for _, address := range []string{retirement.Predecessor.Probe, successor.Probe} {
		check := *successor
		check.RetiredProbe = address
		if err := verifyPrecompileProbeSuccessorUnfunded(ctx, &check, head, nonce, balanceAt, stakeAt); err != nil {
			return err
		}
	}
	return nil
}

// Replays the predecessor's canonical CREATE and exact runtime, then verifies
// its battery on-chain at the retained anchor without resending any operation.
func verifyPrecompileProbeRetirementAt(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, client *ethclient.Client, head ChainHead, nonce uint64) error {
	successor := plan.PrecompileProbeSuccessor
	if successor.Retirement == nil {
		return nil
	}
	source, err := readPrecompileProbeRetirementSource(cfg, stateDir, plan, entries)
	if err != nil {
		return err
	}
	retirement := successor.Retirement
	var prefix []JournalEntry
	for _, entry := range entries {
		if entry.Sequence <= retirement.JournalSequence {
			prefix = append(prefix, entry)
		}
	}
	if err := verifyPrecompileProbeSuccessorAt(ctx, historicalPlanConfig(cfg, source), stateDir, source, prefix, client, head, retirement.Predecessor.DeployerNonce+1); err != nil {
		return err
	}
	reader := &Executor{cfg: cfg, deployer: &EvmTxManager{client: client}}
	if err := verifyPrecompileProbeRetirementCustody(ctx, successor, head, nonce, client.BalanceAt, reader.readStakeAt); err != nil {
		return err
	}
	parsed, err := abi.JSON(strings.NewReader(SubnetProbeABI))
	if err != nil {
		return err
	}
	evidence := &retirement.Evidence
	sample, err := decodeHex32("retired probe sample", evidence.SampleHotkey)
	if err != nil {
		return err
	}
	absent, err := decodeHex32("retired probe absent control", evidence.AbsentHotkey)
	if err != nil {
		return err
	}
	values, err := contractCallAt(ctx, client, common.HexToAddress(evidence.ProbeAddress), parsed, "readBattery", evidence.Battery.FinalizedHead.Number, sample, absent)
	if err != nil || len(values) != 1 {
		return stateMismatchError(err, "retired precompile battery cannot be reproduced")
	}
	tuple, ok := abi.ConvertType(values[0], new(precompileBatteryTuple)).(*precompileBatteryTuple)
	if !ok || !batteryTupleCompatible(evidence, tuple, source.LiveFacts.NominatorMinimumRao) || tuple.UidCount != evidence.Battery.UIDCount || hexBytesValue(tuple.Uid0Hotkey[:]) != evidence.Battery.UID0Hotkey || hexBytesValue(tuple.Uid0Coldkey[:]) != evidence.Battery.UID0Coldkey || tuple.SampleSelfStake.String() != evidence.Battery.SampleSelfStake {
		return errors.New("retired precompile battery differs from its finalized evidence")
	}
	return nil
}
