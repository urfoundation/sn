package main

// final_semantic_source.go owns the post-run construction boundary. The
// scenario runner calls this only after its terminal process-log scan and
// output write; no terminal evidence is accepted as a pre-run option.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/ss58"
	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
	"gopkg.in/yaml.v3"
)

// BuildFinalSemanticSourceFromCampaign reconstructs the typed semantic source
// from authenticated campaign/state artifacts. It intentionally has no
// best-effort mode: until every v1 input can be found and cross-bound, it
// returns an error and FINAL.md cannot be produced.
func BuildFinalSemanticSourceFromCampaign(ctx context.Context, cfg *ResolvedConfig, stateDir, runDir string, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation) (*FinalSemanticEvidence, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || stateDir == "" || runDir == "" || result == nil || terminal == nil || len(history) == 0 {
		return nil, errors.New("final semantic campaign source inputs are incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return buildFinalSemanticSourceFromCampaign(ctx, cfg, stateDir, runDir, result, terminal, history)
}

// NewFinalSemanticCampaignArtifactLoader resolves only closed campaign paths:
// run-relative files plus stateDir/receipts and stateDir/public. Network URLs
// are deliberately unavailable before the publication/readback boundary.
func NewFinalSemanticCampaignArtifactLoader(stateDir, runDir string) (FinalArtifactLoader, error) {
	stateRoot, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	runRoot, err := filepath.Abs(runDir)
	if err != nil {
		return nil, err
	}
	if stateRoot == runRoot || !pathWithinRoot(stateRoot, runRoot) {
		return nil, errors.New("final semantic run directory is outside the state directory")
	}
	return func(ctx context.Context, locator FinalArtifactLocator) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.Contains(locator.URI, "://") {
			if err := verifyFinalURI("campaign artifact", locator.URI); err != nil {
				return nil, err
			}
			return nil, errors.New("pre-publication semantic loader rejects network locators")
		}
		if err := verifyFinalArtifact("campaign artifact", locator, locator.Kind); err != nil {
			return nil, err
		}
		clean := filepath.Clean(filepath.FromSlash(locator.URI))
		root := runRoot
		if clean == "receipts" || strings.HasPrefix(clean, "receipts"+string(filepath.Separator)) || clean == "public" || strings.HasPrefix(clean, "public"+string(filepath.Separator)) {
			root = stateRoot
		}
		absolute, err := filepath.Abs(filepath.Join(root, clean))
		if err != nil || !pathWithinRoot(root, absolute) {
			return nil, errors.New("final semantic artifact escapes its allowed root")
		}
		if err := rejectFinalArtifactSymlinkComponents(root, absolute); err != nil {
			return nil, err
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("final semantic artifact %s is not a regular file", locator.URI)
		}
		return os.ReadFile(absolute)
	}, nil
}

func rejectFinalArtifactSymlinkComponents(root, candidate string) error {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("final semantic artifact escapes its allowed root")
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("final semantic artifact path component %s is a symlink", component)
		}
	}
	return nil
}

func pathWithinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// finalSemanticArchive is an authenticated, in-memory view of the closed
// collection graph. Files inside a bundle are addressed by the path they had
// at capture time. Direct run artifacts retain their manifest locator. Nothing
// in this type can fall through to a mutable state-directory read.
type finalSemanticArchive struct {
	ctx       context.Context
	cfg       *ResolvedConfig
	stateRoot string
	runRoot   string
	load      FinalArtifactLoader
	collected *FinalSemanticCollectedInputs
	files     map[string][]byte
	locators  map[string]FinalArtifactLocator
	// priorSemantic is decoded only from the owner-signed, content-addressed
	// semantic file carried by a production collection. It is never populated
	// from mutable state and lets later builders enforce exact phase continuity.
	priorSemantic *FinalSemanticEvidence
}

type finalSemanticEvent struct {
	Name string
	Log  finalCanonicalEVMLog
	Args map[string]any
}

type finalSemanticEventIndex struct {
	byName map[string][]finalSemanticEvent
	byTx   map[string][]finalCanonicalEVMLog
}

func buildFinalSemanticSourceFromCampaign(ctx context.Context, cfg *ResolvedConfig, stateDir, runDir string, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation) (*FinalSemanticEvidence, error) {
	archive, err := openFinalSemanticArchive(ctx, cfg, stateDir, runDir)
	if err != nil {
		return nil, err
	}
	return buildFinalSemanticSourceFromArchive(ctx, cfg, archive, result, terminal, history)
}

// buildFinalSemanticSourceFromArchive reconstructs exclusively from an
// already-opened immutable collection graph. The asynchronous producer calls
// this after comparing the graph with the owner-authenticated closure, avoiding
// a second manifest read and its accompanying TOCTOU window.
func buildFinalSemanticSourceFromArchive(ctx context.Context, cfg *ResolvedConfig, archive *finalSemanticArchive, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation) (*FinalSemanticEvidence, error) {
	if ctx == nil || cfg == nil || archive == nil || archive.collected == nil || result == nil || terminal == nil || len(history) == 0 {
		return nil, errors.New("final semantic closed archive inputs are incomplete")
	}
	if err := archive.bindCallInputs(result, terminal, history); err != nil {
		return nil, err
	}
	if result.AcceptanceWindow == nil {
		return nil, errors.New("closed scenario result has no acceptance window")
	}
	chain, err := archive.chainSnapshot()
	if err != nil {
		return nil, err
	}
	if terminal.Status == nil || terminal.Status.Contracts == nil || terminal.Status.Contracts.Deployment == nil {
		return nil, errors.New("closed terminal deployment is unavailable for live-chain verification")
	}
	if err := verifyFinalCollectedChainSnapshot(chain, terminal.Status.Contracts.Deployment); err != nil {
		return nil, fmt.Errorf("verify closed final chain snapshot: %w", err)
	}
	identityBytes := archive.files["public/identities.json"]
	if len(identityBytes) == 0 || chain.PublicIdentitiesHash != bytesSHA256(identityBytes) {
		return nil, errors.New("closed final chain reward positions do not bind the captured public identities")
	}
	if chain.DeploymentID != result.DeploymentID || chain.EVMHead != result.EndHead {
		return nil, errors.New("closed final chain snapshot does not bind the scenario deployment and terminal EVM head")
	}
	events, err := indexFinalSemanticEvents(chain)
	if err != nil {
		return nil, err
	}
	planHash, err := archive.planHash()
	if err != nil {
		return nil, err
	}
	planBytes, _, err := archive.file("launch-foundation/plan.json")
	if err != nil {
		return nil, err
	}
	planArtifact, err := archive.derivedBytes("setup-plan", "setup-plan.json", planBytes)
	if err != nil {
		return nil, fmt.Errorf("persist approved setup plan artifact: %w", err)
	}
	nativeStart, err := archive.nativeStartHead(chain)
	if err != nil {
		return nil, err
	}
	source := FinalSemanticEvidence{
		Phase: result.Name, RunID: result.RunID, ResultHash: result.EvidenceHash,
		CampaignStartedAt: result.StartedAt, CampaignCompletedAt: result.CompletedAt,
		DeploymentID: result.DeploymentID, PlanHash: planHash, ConfigHash: result.ConfigHash,
		PolicyHash: result.PolicyHash, GenesisHash: result.GenesisHash, ChainID: result.ChainID, Netuid: result.Netuid,
		PlanArtifact: planArtifact, PolicyArtifact: archive.collected.Policy, Window: *result.AcceptanceWindow,
		EVMCampaignStartHead: result.CampaignStartHead, NativeStartHead: nativeStart, NativeTerminalHead: chain.NativeHead, EVMTerminalHead: chain.EVMHead,
		ExpectedOperators: cfg.Config.Topology.Operators, ExpectedValidators: cfg.Config.Topology.Validators,
		ExpectedMiners: cfg.Config.Topology.Miners, ExpectedCandidates: cfg.Config.Topology.HeadFleets + cfg.Config.Topology.ChallengerFleets,
		ExpectedHeadSlots: cfg.Config.Topology.HeadSlots,
	}
	identities, err := archive.identities()
	if err != nil {
		return nil, err
	}
	if identities.DeploymentID != source.DeploymentID {
		return nil, errors.New("public identity document differs from the campaign deployment")
	}
	if err := archive.buildPriorPhase(&source, identities); err != nil {
		return nil, err
	}
	if err := archive.buildDeploymentAndReserve(&source, terminal, identities, events); err != nil {
		return nil, err
	}
	if err := archive.buildArchiveRetention(&source, chain); err != nil {
		return nil, err
	}
	if err := archive.buildCleanup(&source); err != nil {
		return nil, err
	}
	if err := archive.buildPools(&source, terminal, identities, chain, events); err != nil {
		return nil, err
	}
	if err := archive.buildFleetLifecycle(&source, result, terminal, identities, events); err != nil {
		return nil, err
	}
	if err := archive.buildTopology(&source, result, terminal, identities, chain); err != nil {
		return nil, err
	}
	if err := archive.buildValidators(&source, identities, chain, events); err != nil {
		return nil, err
	}
	if err := archive.buildFleetLifecycleAppliedDecisions(&source); err != nil {
		return nil, err
	}
	if err := archive.buildDishonestDeposit(&source, terminal, chain, events); err != nil {
		return nil, err
	}
	if err := archive.buildEpochs(&source, terminal, events); err != nil {
		return nil, err
	}
	if err := archive.buildRewards(&source, history, chain); err != nil {
		return nil, err
	}
	if err := archive.buildPathProofs(&source); err != nil {
		return nil, err
	}
	if err := archive.buildExitCriteria(&source, result, terminal, events); err != nil {
		return nil, err
	}
	return BuildFinalSemanticEvidence(source)
}

func openFinalSemanticArchive(ctx context.Context, cfg *ResolvedConfig, stateDir, runDir string) (*finalSemanticArchive, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stateRoot, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	runRoot, err := filepath.Abs(runDir)
	if err != nil || !pathWithinRoot(stateRoot, runRoot) {
		return nil, errors.New("final semantic run directory is outside the state directory")
	}
	manifestPath := filepath.Join(runRoot, "final-inputs", "manifest.json")
	if err := rejectFinalArtifactSymlinkComponents(runRoot, manifestPath); err != nil {
		return nil, err
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read final semantic input manifest: %w", err)
	}
	var collected FinalSemanticCollectedInputs
	if err := decodeStrictJSONBytes(manifestBytes, &collected); err != nil {
		return nil, fmt.Errorf("decode final semantic input manifest: %w", err)
	}
	if err := verifyFinalSemanticCollectedInputs(cfg, &collected); err != nil {
		return nil, fmt.Errorf("verify final semantic input manifest: %w", err)
	}
	load, err := NewFinalSemanticCampaignArtifactLoader(stateRoot, runRoot)
	if err != nil {
		return nil, err
	}
	archive := &finalSemanticArchive{ctx: ctx, cfg: cfg, stateRoot: stateRoot, runRoot: runRoot, load: load, collected: &collected, files: map[string][]byte{}, locators: map[string]FinalArtifactLocator{}}
	addDirect := func(locator FinalArtifactLocator) error {
		data, err := archive.loadChecked(locator)
		if err != nil {
			return err
		}
		if prior, ok := archive.files[locator.URI]; ok && !bytes.Equal(prior, data) {
			return fmt.Errorf("closed semantic graph has conflicting direct path %s", locator.URI)
		}
		archive.files[locator.URI] = data
		archive.locators[locator.URI] = locator
		return nil
	}
	for _, locator := range []FinalArtifactLocator{collected.Policy, collected.ScenarioResult, collected.TerminalObservation, collected.ObservationHistory} {
		if err := addDirect(locator); err != nil {
			return nil, err
		}
	}
	if collected.PriorPhase != nil {
		priorLocators := []FinalArtifactLocator{collected.PriorPhase.ScenarioResult, collected.PriorPhase.OwnerCompletion, collected.PriorPhase.EvidenceManifest, collected.PriorPhase.LifecycleHandoff, collected.PriorPhase.CaptureStatus, collected.PriorPhase.CollectedInputsManifest, collected.PriorPhase.SemanticSupplement}
		priorLocators = append(priorLocators, collected.PriorPhase.LiveChainBundles...)
		priorLocators = append(priorLocators, collected.PriorPhase.SemanticFileEnvelopes...)
		for _, locator := range priorLocators {
			if err := addDirect(locator); err != nil {
				return nil, err
			}
		}
	}
	for _, payout := range collected.Payouts {
		if err := addDirect(payout.Artifact); err != nil {
			return nil, err
		}
	}
	for _, payout := range collected.LifecyclePayouts {
		if err := addDirect(payout.Artifact); err != nil {
			return nil, err
		}
	}
	for _, validator := range collected.Validators {
		if err := addDirect(validator.IntentStore); err != nil {
			return nil, err
		}
		if validator.DishonestDepositIntent != nil {
			for _, locator := range []FinalArtifactLocator{validator.DishonestDepositIntent.Artifact, validator.DishonestDepositIntent.Measurement, validator.DishonestDepositIntent.Envelope} {
				if err := addDirect(locator); err != nil {
					return nil, err
				}
			}
		}
		for _, intent := range validator.Intents {
			for _, locator := range []FinalArtifactLocator{intent.Artifact, intent.Measurement, intent.Envelope} {
				if err := addDirect(locator); err != nil {
					return nil, err
				}
			}
		}
		for _, intent := range validator.LifecycleIntents {
			for _, locator := range []FinalArtifactLocator{intent.Artifact, intent.Measurement, intent.Envelope} {
				if err := addDirect(locator); err != nil {
					return nil, err
				}
			}
		}
		for _, attempt := range validator.Attempts {
			if err := addDirect(attempt.Artifact); err != nil {
				return nil, err
			}
		}
		for _, proof := range validator.PathProofs {
			if err := addDirect(proof.Artifact); err != nil {
				return nil, err
			}
		}
	}
	for _, locator := range collected.ClosedInputBundles {
		data, err := archive.loadChecked(locator)
		if err != nil {
			return nil, err
		}
		bundle, err := decodeFinalCollectedFileBundle(data)
		if err != nil {
			return nil, fmt.Errorf("decode closed semantic bundle %s: %w", locator.URI, err)
		}
		class := finalSemanticBundleClass(bundle.Name)
		for _, entry := range bundle.Files {
			qualified := class + "/" + entry.Path
			if prior, ok := archive.files[qualified]; ok && !bytes.Equal(prior, entry.Data) {
				return nil, fmt.Errorf("closed semantic graph has conflicting bundled path %s", qualified)
			}
			archive.files[qualified] = append([]byte(nil), entry.Data...)
		}
	}
	return archive, nil
}

func finalSemanticBundleClass(name string) string {
	for _, prefix := range []string{"public", "receipts", "launch-foundation", "plan-history", "miner-topology", "claim-runtime", "accepted-contract-cleanup", "live-chain"} {
		if name == prefix || strings.HasPrefix(name, prefix+"-") {
			return prefix
		}
	}
	return name
}

func (a *finalSemanticArchive) loadChecked(locator FinalArtifactLocator) ([]byte, error) {
	data, err := a.load(a.ctx, locator)
	if err != nil {
		return nil, fmt.Errorf("load closed semantic artifact %s: %w", locator.URI, err)
	}
	if uint64(len(data)) != locator.SizeBytes || bytesSHA256(data) != locator.ContentHash {
		return nil, fmt.Errorf("closed semantic artifact %s size or hash differs", locator.URI)
	}
	return data, nil
}

func (a *finalSemanticArchive) file(paths ...string) ([]byte, string, error) {
	for _, name := range paths {
		if data, ok := a.files[filepath.ToSlash(name)]; ok {
			return append([]byte(nil), data...), filepath.ToSlash(name), nil
		}
	}
	return nil, "", fmt.Errorf("closed semantic graph is missing %s", strings.Join(paths, " or "))
}

func (a *finalSemanticArchive) decode(path string, out any) error {
	data, _, err := a.file(path)
	if err != nil {
		return err
	}
	if err := decodeStrictJSONBytes(data, out); err != nil {
		return fmt.Errorf("decode closed semantic artifact %s: %w", path, err)
	}
	return nil
}

func (a *finalSemanticArchive) derived(kind, name string, value any) (FinalArtifactLocator, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return FinalArtifactLocator{}, err
	}
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return FinalArtifactLocator{}, errors.New("derived final semantic artifact is empty")
	}
	return persistFinalCollectedArtifact(a.runRoot, kind, filepath.ToSlash(filepath.Join("final-derived", name)), data)
}

func (a *finalSemanticArchive) derivedBytes(kind, name string, data []byte) (FinalArtifactLocator, error) {
	if len(data) == 0 {
		return FinalArtifactLocator{}, errors.New("derived final semantic artifact is empty")
	}
	return persistFinalCollectedArtifact(a.runRoot, kind, filepath.ToSlash(filepath.Join("final-derived", name)), data)
}

func (a *finalSemanticArchive) bindCallInputs(result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation) error {
	var capturedResult ScenarioResult
	if err := a.decode(a.collected.ScenarioResult.URI, &capturedResult); err != nil {
		return err
	}
	var capturedTerminal ScenarioObservation
	if err := a.decode(a.collected.TerminalObservation.URI, &capturedTerminal); err != nil {
		return err
	}
	var capturedHistory []*ScenarioObservation
	if err := a.decode(a.collected.ObservationHistory.URI, &capturedHistory); err != nil {
		return err
	}
	if result == nil || terminal == nil || len(history) == 0 || !finalJSONEqual(capturedResult, *result) || !finalJSONEqual(capturedTerminal, *terminal) || !finalJSONEqual(capturedHistory, history) {
		return errors.New("final semantic call inputs differ from the closed collection graph")
	}
	return nil
}

func (a *finalSemanticArchive) planHash() (string, error) {
	data, _, err := a.file("launch-foundation/plan.json")
	if err != nil {
		return "", err
	}
	var plan SetupPlan
	if err := decodeStrictJSONBytes(data, &plan); err != nil {
		return "", fmt.Errorf("decode closed setup plan: %w", err)
	}
	if plan.Schema != currentSetupPlanSchema {
		return "", fmt.Errorf("closed setup plan schema %q is not current", plan.Schema)
	}
	observedHash, err := persistedSetupPlanHash(data, plan.Schema)
	if err != nil || !strings.EqualFold(observedHash, plan.PlanHash) {
		return "", stateMismatchError(err, "closed setup plan hash %s does not authenticate its exact wire object", plan.PlanHash)
	}
	if err := requireFinalHex32("closed plan hash", plan.PlanHash); err != nil {
		return "", err
	}
	return strings.ToLower(plan.PlanHash), nil
}

func (a *finalSemanticArchive) identities() (*finalPublicIdentities, error) {
	var identities finalPublicIdentities
	if err := a.decode("public/identities.json", &identities); err != nil {
		return nil, err
	}
	if identities.Schema != "urnetwork-sim-public-identities-v1" || identities.DeploymentID == "" {
		return nil, errors.New("public identity document has an invalid identity")
	}
	if len(identities.Substrate) == 0 || len(identities.EVM) == 0 || len(identities.Clients) < 1000 {
		return nil, errors.New("public identity census is incomplete")
	}
	return &identities, nil
}

func (a *finalSemanticArchive) chainSnapshot() (*FinalCollectedChainSnapshot, error) {
	var snapshot FinalCollectedChainSnapshot
	if err := a.decode("live-chain/live-chain/final-chain-snapshot.json", &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Schema != finalCollectedChainSnapshotSchema || snapshot.Phase != a.collected.Phase || snapshot.RunID != a.collected.RunID {
		return nil, errors.New("closed final chain snapshot differs from the campaign")
	}
	return &snapshot, nil
}

// nativeStartHead is the earliest native boundary cryptographically bound by
// an applied acceptance-window intent. ScenarioResult heads are EVM heads and
// must never be reused as Substrate evidence. Snapshot hashes come from the
// signed intent; commit hashes are additionally pinned in the live-chain map.
func (a *finalSemanticArchive) nativeStartHead(chain *FinalCollectedChainSnapshot) (ChainHead, error) {
	if a == nil || a.collected == nil || chain == nil {
		return ChainHead{}, errors.New("native start construction context is incomplete")
	}
	var start ChainHead
	consider := func(head ChainHead) error {
		if head.Number == 0 || head.Number > chain.NativeHead.Number {
			return errors.New("accepted native boundary is outside the terminal native snapshot")
		}
		head.Hash = strings.ToLower(head.Hash)
		if err := requireFinalHex32("accepted native boundary", head.Hash); err != nil {
			return err
		}
		if start.Number == 0 || head.Number < start.Number {
			start = head
			return nil
		}
		if head.Number == start.Number && head.Hash != start.Hash {
			return fmt.Errorf("accepted native boundary block %d has conflicting hashes", head.Number)
		}
		return nil
	}
	lastEpoch := a.collected.Window.FirstEpoch + a.collected.Window.EpochCount
	for _, validator := range a.collected.Validators {
		items := append([]FinalCollectedValidatorIntent(nil), validator.Intents...)
		if validator.DishonestDepositIntent != nil {
			items = append(items, *validator.DishonestDepositIntent)
		}
		for _, item := range items {
			accepted := item.SettlementEpoch >= a.collected.Window.FirstEpoch && item.SettlementEpoch < lastEpoch
			penalty := a.collected.Phase == "production-soak" && item.SettlementEpoch+1 == a.collected.Window.FirstEpoch
			if item.Status != "applied" || !accepted && !penalty {
				continue
			}
			data, _, err := a.file(item.Artifact.URI)
			if err != nil {
				return ChainHead{}, err
			}
			var intent validatorpkg.SteeringIntent
			if err := decodeStrictJSONBytes(data, &intent); err != nil {
				return ChainHead{}, fmt.Errorf("decode accepted validator intent: %w", err)
			}
			if intent.Status != item.Status || intent.ValidatorID != validator.ValidatorID || intent.SettlementEpoch != item.SettlementEpoch || intent.SubnetEpoch != item.SubnetEpoch || !strings.EqualFold(intent.VectorHash, item.VectorHash) {
				return ChainHead{}, errors.New("accepted validator intent differs from its closed summary")
			}
			if err := consider(ChainHead{Number: intent.NativeSnapshotBlock, Hash: intent.NativeSnapshotHash}); err != nil {
				return ChainHead{}, err
			}
			commit, err := finalSemanticNativeHead(chain, intent.FinalizedBlock, intent.FinalizedBlockHash)
			if err != nil {
				return ChainHead{}, fmt.Errorf("accepted validator intent commit: %w", err)
			}
			if err := consider(commit); err != nil {
				return ChainHead{}, err
			}
		}
	}
	if start.Number == 0 {
		return ChainHead{}, errors.New("closed collection has no applied native acceptance boundary")
	}
	return start, nil
}

func indexFinalSemanticEvents(snapshot *FinalCollectedChainSnapshot) (*finalSemanticEventIndex, error) {
	if snapshot == nil {
		return nil, errors.New("final chain snapshot is nil")
	}
	contracts := make([]abi.ABI, 0, 3)
	for _, encoded := range []string{CoordinatorABI, SettlementVaultABI, ReserveSinkABI} {
		parsed, err := abi.JSON(strings.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, parsed)
	}
	eventsByTopic := map[common.Hash][]abi.Event{}
	for _, contract := range contracts {
		for _, event := range contract.Events {
			eventsByTopic[event.ID] = append(eventsByTopic[event.ID], event)
		}
	}
	index := &finalSemanticEventIndex{byName: map[string][]finalSemanticEvent{}, byTx: map[string][]finalCanonicalEVMLog{}}
	for _, log := range snapshot.EVMLogs {
		if len(log.Topics) == 0 {
			return nil, errors.New("captured final EVM log has no event topic")
		}
		topic := common.HexToHash(log.Topics[0])
		candidates := eventsByTopic[topic]
		if len(candidates) == 0 {
			return nil, fmt.Errorf("captured release-contract log has unknown topic %s", log.Topics[0])
		}
		var decoded finalSemanticEvent
		var decodeErr error
		for _, event := range candidates {
			args := map[string]any{}
			data, err := hex.DecodeString(strings.TrimPrefix(log.Data, "0x"))
			if err == nil {
				err = event.Inputs.NonIndexed().UnpackIntoMap(args, data)
			}
			if err == nil {
				topics := make([]common.Hash, len(log.Topics)-1)
				for i := 1; i < len(log.Topics); i++ {
					topics[i-1] = common.HexToHash(log.Topics[i])
				}
				err = abi.ParseTopicsIntoMap(args, indexedABIArguments(event.Inputs), topics)
			}
			if err == nil {
				decoded = finalSemanticEvent{Name: event.Name, Log: log, Args: args}
				decodeErr = nil
				break
			}
			decodeErr = err
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode captured event %s: %w", log.Topics[0], decodeErr)
		}
		index.byName[decoded.Name] = append(index.byName[decoded.Name], decoded)
		index.byTx[log.TransactionHash] = append(index.byTx[log.TransactionHash], log)
	}
	return index, nil
}

func indexedABIArguments(arguments abi.Arguments) abi.Arguments {
	result := make(abi.Arguments, 0, len(arguments))
	for _, argument := range arguments {
		if argument.Indexed {
			result = append(result, argument)
		}
	}
	return result
}

func finalSemanticUint(args map[string]any, name string) (uint64, bool) {
	value, ok := args[name]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case *big.Int:
		return typed.Uint64(), typed.Sign() >= 0 && typed.IsUint64()
	case uint64:
		return typed, true
	case uint32:
		return uint64(typed), true
	case uint16:
		return uint64(typed), true
	case uint8:
		return uint64(typed), true
	default:
		return 0, false
	}
}

func finalSemanticInteger(args map[string]any, name string) (*big.Int, bool) {
	value, ok := args[name]
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case *big.Int:
		return new(big.Int).Set(typed), typed.Sign() >= 0
	case uint64:
		return new(big.Int).SetUint64(typed), true
	case uint16:
		return new(big.Int).SetUint64(uint64(typed)), true
	default:
		return nil, false
	}
}

func finalSemanticObservedUint(values map[string]any, name string) (uint64, bool) {
	value, ok := values[name]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		if typed < 0 || typed != float64(uint64(typed)) {
			return 0, false
		}
		return uint64(typed), true
	case json.Number:
		parsed, err := strconv.ParseUint(string(typed), 10, 64)
		return parsed, err == nil
	case uint64:
		return typed, true
	default:
		return 0, false
	}
}

func finalSemanticHex32(args map[string]any, name string) (string, bool) {
	value, ok := args[name]
	if !ok {
		return "", false
	}
	switch typed := value.(type) {
	case [32]byte:
		return "0x" + hex.EncodeToString(typed[:]), true
	case common.Hash:
		return strings.ToLower(typed.Hex()), true
	default:
		return "", false
	}
}

func finalSemanticAddress(args map[string]any, name string) (string, bool) {
	value, ok := args[name]
	if !ok {
		return "", false
	}
	address, ok := value.(common.Address)
	return strings.ToLower(address.Hex()), ok && address != (common.Address{})
}

func (a *finalSemanticArchive) receiptFromIndex(index *finalSemanticEventIndex, event finalSemanticEvent, name string) (FinalEVMReceipt, error) {
	logs := index.byTx[event.Log.TransactionHash]
	if len(logs) == 0 {
		return FinalEVMReceipt{}, fmt.Errorf("semantic EVM receipt %s has no captured transaction group", name)
	}
	hash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil {
		return FinalEVMReceipt{}, err
	}
	proof, err := a.derived("evm-receipt", filepath.ToSlash(filepath.Join("evm-receipts", name+".json")), map[string]any{"status": "success", "transaction_hash": event.Log.TransactionHash, "block": ChainHead{Number: event.Log.BlockNumber, Hash: event.Log.BlockHash}, "logs": logs})
	if err != nil {
		return FinalEVMReceipt{}, err
	}
	return FinalEVMReceipt{TransactionHash: event.Log.TransactionHash, Block: ChainHead{Number: event.Log.BlockNumber, Hash: event.Log.BlockHash}, Status: "success", LogsHash: hash, Proof: proof}, nil
}

func (a *finalSemanticArchive) journalEntries() ([]JournalEntry, error) {
	data, _, err := a.file("launch-foundation/journal.jsonl")
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	entries := make([]JournalEntry, 0)
	for scanner.Scan() {
		var entry JournalEntry
		if err := decodeStrictJSONBytes(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("decode captured journal line %d: %w", len(entries)+1, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("captured setup journal is empty")
	}
	return entries, nil
}

func (a *finalSemanticArchive) actionFinalized(actionID string) (JournalEntry, error) {
	entries, err := a.journalEntries()
	if err != nil {
		return JournalEntry{}, err
	}
	var found JournalEntry
	for _, entry := range entries {
		if entry.ActionID == actionID && entry.Stage == StageFinalized && entry.TransactionHash != "" && entry.BlockNumber != 0 && entry.BlockHash != "" {
			found = entry
		}
	}
	if found.ActionID == "" {
		return JournalEntry{}, fmt.Errorf("captured setup journal has no finalized transaction for %s", actionID)
	}
	return found, nil
}

func (a *finalSemanticArchive) actionPostcondition(actionID string) (*ActionPostcondition, []byte, error) {
	// Revisions may retain earlier receipts. The latest verified journal entry
	// is authoritative, and its exact path and canonical hash authenticate one
	// v4 object. Directory scans would allow an unjournaled duplicate to win.
	entries, err := a.journalEntries()
	if err != nil {
		return nil, nil, err
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.ActionID != actionID || entry.Stage != StageVerified {
			continue
		}
		wantPath, pathErr := postconditionRelativePath(entry.PlanHash, entry.ActionID)
		if pathErr != nil || entry.PostconditionPath != wantPath {
			return nil, nil, stateMismatchError(pathErr, "captured v4 postcondition for %s has a noncanonical journal path", actionID)
		}
		data, ok := a.files[entry.PostconditionPath]
		if !ok {
			return nil, nil, fmt.Errorf("captured v4 postcondition for %s is absent at journal path %s", actionID, entry.PostconditionPath)
		}
		record, decodeErr := decodeFinalActionPostconditionV4(data)
		if decodeErr != nil {
			return nil, nil, fmt.Errorf("decode captured v4 postcondition for %s: %w", actionID, decodeErr)
		}
		if record.DeploymentID != entry.DeploymentID || record.PlanHash != entry.PlanHash || record.ActionID != entry.ActionID || record.IntentHash != entry.IntentHash {
			return nil, nil, fmt.Errorf("captured v4 postcondition for %s differs from its verified journal identity", actionID)
		}
		hash, hashErr := canonicalHashHex(record)
		if hashErr != nil || hash != entry.PostconditionHash {
			return nil, nil, stateMismatchError(hashErr, "captured v4 postcondition for %s has hash %s, journal requires %s", actionID, hash, entry.PostconditionHash)
		}
		return record, append([]byte(nil), data...), nil
	}
	return nil, nil, fmt.Errorf("captured postcondition for %s is not journal-authenticated", actionID)
}

func (a *finalSemanticArchive) nativeActionReceipt(actionID, name string) (FinalNativeReceipt, *ActionPostcondition, error) {
	entry, err := a.actionFinalized(actionID)
	if err != nil {
		return FinalNativeReceipt{}, nil, err
	}
	postcondition, data, err := a.actionPostcondition(actionID)
	if err != nil {
		return FinalNativeReceipt{}, nil, err
	}
	if entry.DeploymentID != postcondition.DeploymentID || entry.PlanHash != postcondition.PlanHash || entry.ActionID != postcondition.ActionID || entry.IntentHash != postcondition.IntentHash {
		return FinalNativeReceipt{}, nil, fmt.Errorf("captured finalized receipt for %s differs from its verified v4 postcondition", actionID)
	}
	if postcondition.SubstrateFinalized.Number < entry.BlockNumber {
		return FinalNativeReceipt{}, nil, fmt.Errorf("%s postcondition precedes transaction inclusion", actionID)
	}
	proof, err := a.derivedBytes("native-receipt", filepath.ToSlash(filepath.Join("native-receipts", name+".json")), data)
	if err != nil {
		return FinalNativeReceipt{}, nil, err
	}
	return FinalNativeReceipt{ExtrinsicHash: strings.ToLower(entry.TransactionHash), Block: ChainHead{Number: entry.BlockNumber, Hash: strings.ToLower(entry.BlockHash)}, Proof: proof}, postcondition, nil
}

// The remaining construction methods are kept independent so every evidence
// class has one auditable source join and deterministic tests can target a
// missing or contradictory class directly.
func (a *finalSemanticArchive) buildPriorPhase(source *FinalSemanticEvidence, identities *finalPublicIdentities) error {
	if a.collected.PriorPhase == nil {
		return nil
	}
	if source == nil || identities == nil {
		return errors.New("prior semantic lineage context is incomplete")
	}
	prior := a.collected.PriorPhase
	var result ScenarioResult
	if err := a.decode(prior.ScenarioResult.URI, &result); err != nil {
		return err
	}
	if result.AcceptanceWindow == nil || result.RunID != prior.RunID || result.EvidenceHash != prior.ResultHash || result.LifecycleHandoff == nil || result.PriorRelease != nil {
		return errors.New("closed prior release result differs from its manifest binding")
	}
	resultHash, err := canonicalScenarioResultHash(&result)
	if err != nil || resultHash != prior.ResultHash {
		return stateMismatchError(err, "closed prior release result hash differs from its canonical bytes")
	}
	completionData, _, err := a.file(prior.OwnerCompletion.URI)
	if err != nil {
		return err
	}
	manifestData, _, err := a.file(prior.EvidenceManifest.URI)
	if err != nil {
		return err
	}
	var completion, manifestEnvelope ReleaseEvidenceEnvelope
	owner := strings.ToLower(identities.EVM["testnet-owner"])
	verifyOwner := func(envelope *ReleaseEvidenceEnvelope, kind string) error {
		if !common.IsHexAddress(owner) || common.HexToAddress(owner) == (common.Address{}) || verifyEvidence(envelope, nil) != nil || !strings.EqualFold(envelope.Signer.Hex(), owner) || envelope.Kind != kind || envelope.RunID != prior.RunID || envelope.DeploymentID != source.DeploymentID || envelope.ChainID != source.ChainID || envelope.Netuid != source.Netuid || !strings.EqualFold(envelope.GenesisHash, source.GenesisHash) {
			return fmt.Errorf("prior %s owner envelope identity is invalid", kind)
		}
		created, err := time.Parse(time.RFC3339Nano, envelope.CreatedAt)
		started, startedErr := time.Parse(time.RFC3339Nano, source.CampaignStartedAt)
		if err != nil || startedErr != nil || envelope.CreatedAt != created.UTC().Format(time.RFC3339Nano) {
			return fmt.Errorf("prior %s owner envelope timestamp is not canonical", kind)
		}
		if (kind == "scenario-complete" || kind == campaignEvidenceManifestKind) && !created.Before(started) {
			return fmt.Errorf("prior %s owner envelope was not finalized before production", kind)
		}
		return nil
	}
	if err := decodeStrictJSONBytes(completionData, &completion); err != nil || verifyOwner(&completion, "scenario-complete") != nil {
		return errors.Join(err, errors.New("closed prior owner completion envelope is invalid"))
	}
	if err := decodeStrictJSONBytes(manifestData, &manifestEnvelope); err != nil || verifyOwner(&manifestEnvelope, campaignEvidenceManifestKind) != nil {
		return errors.Join(err, errors.New("closed prior evidence-manifest envelope is invalid"))
	}
	var completionPayload scenarioCompletePayload
	if err := decodeStrictJSONBytes(completion.Payload, &completionPayload); err != nil || !strings.EqualFold(completionPayload.ResultHash, prior.ResultHash) || !strings.EqualFold(completionPayload.EvidenceManifestHash, manifestEnvelope.ContentHash) || completionPayload.LifecycleHandoff == nil || *completionPayload.LifecycleHandoff != *result.LifecycleHandoff || completionPayload.PriorRelease != nil {
		return errors.Join(err, errors.New("closed prior completion does not bind its evidence manifest"))
	}
	handoffData, _, err := a.file(prior.LifecycleHandoff.URI)
	if err != nil {
		return err
	}
	if err := validateFinalCollectedPriorLifecycleHandoff(a.cfg, &result, &completionPayload, prior.LifecycleHandoff, handoffData); err != nil {
		return fmt.Errorf("verify closed prior lifecycle handoff: %w", err)
	}
	captureData, _, err := a.file(prior.CaptureStatus.URI)
	if err != nil {
		return err
	}
	inputsData, _, err := a.file(prior.CollectedInputsManifest.URI)
	if err != nil {
		return err
	}
	var capture FinalSemanticCaptureStatus
	var priorInputs FinalSemanticCollectedInputs
	if err := decodeStrictJSONBytes(captureData, &capture); err != nil || decodeStrictJSONBytes(inputsData, &priorInputs) != nil || capture.Phase != "release-1.0" || capture.RunID != prior.RunID || capture.ResultHash != prior.ResultHash || priorInputs.Phase != "release-1.0" || priorInputs.RunID != prior.RunID || priorInputs.ResultHash != prior.ResultHash {
		return stateMismatchError(err, "closed prior semantic capture lineage is invalid")
	}
	supplementData, _, err := a.file(prior.SemanticSupplement.URI)
	if err != nil {
		return err
	}
	var supplement ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(supplementData, &supplement); err != nil || verifyOwner(&supplement, finalSemanticSupplementKind) != nil {
		return stateMismatchError(err, "closed prior semantic_verified supplement is invalid")
	}
	var supplementPayload FinalSemanticSupplementPayload
	if err := decodeStrictJSONBytes(supplement.Payload, &supplementPayload); err != nil || supplementPayload.Schema != finalSemanticSupplementSchema || supplementPayload.Status != finalSemanticSupplementStatus || supplementPayload.Phase != "release-1.0" || supplementPayload.RunID != prior.RunID || supplementPayload.ResultHash != prior.ResultHash || supplementPayload.ScenarioCompleteHash != completion.ContentHash || supplementPayload.ScenarioEvidenceManifestHash != manifestEnvelope.ContentHash || supplementPayload.CaptureStatusHash != capture.EvidenceHash || supplementPayload.CollectedInputsHash != priorInputs.EvidenceHash {
		return stateMismatchError(err, "closed prior semantic_verified supplement does not bind the release closure")
	}
	fileEnvelopeByHash := make(map[string]struct {
		envelope ReleaseEvidenceEnvelope
		data     []byte
	}, len(prior.SemanticFileEnvelopes))
	for _, locator := range prior.SemanticFileEnvelopes {
		data, _, err := a.file(locator.URI)
		if err != nil {
			return err
		}
		var envelope ReleaseEvidenceEnvelope
		if err := decodeStrictJSONBytes(data, &envelope); err != nil || verifyOwner(&envelope, finalSemanticSupplementFileKind) != nil || fileEnvelopeByHash[envelope.ContentHash].data != nil {
			return stateMismatchError(err, "closed prior semantic file envelope is invalid or duplicated")
		}
		fileEnvelopeByHash[envelope.ContentHash] = struct {
			envelope ReleaseEvidenceEnvelope
			data     []byte
		}{envelope: envelope, data: data}
	}
	var semanticData, semanticEnvelopeData []byte
	for _, item := range supplementPayload.Files {
		carried, ok := fileEnvelopeByHash[item.EnvelopeHash]
		if !ok {
			return fmt.Errorf("prior semantic_verified file %s lacks its authenticated envelope", item.Path)
		}
		var payload finalSemanticSupplementFilePayload
		if err := decodeStrictJSONBytes(carried.envelope.Payload, &payload); err != nil || payload.Schema != finalSemanticSupplementFileSchema || payload.RunID != prior.RunID || payload.Path != item.Path || payload.ContentHash != item.ContentHash || payload.Size != item.Size || uint64(len(payload.Data)) != item.Size || bytesSHA256(payload.Data) != item.ContentHash {
			return stateMismatchError(err, "prior semantic_verified file %s differs", item.Path)
		}
		delete(fileEnvelopeByHash, item.EnvelopeHash)
		if item.Path == finalSemanticEvidenceFilename {
			if semanticData != nil {
				return errors.New("prior semantic_verified supplement repeats semantic evidence")
			}
			semanticData, semanticEnvelopeData = append([]byte(nil), payload.Data...), append([]byte(nil), carried.data...)
		}
	}
	if len(fileEnvelopeByHash) != 0 || semanticData == nil {
		return errors.New("prior semantic_verified file census is incomplete or contains extras")
	}
	completionCreated, _ := time.Parse(time.RFC3339Nano, completion.CreatedAt)
	manifestCreated, _ := time.Parse(time.RFC3339Nano, manifestEnvelope.CreatedAt)
	supplementCreated, _ := time.Parse(time.RFC3339Nano, supplement.CreatedAt)
	var semanticEnvelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(semanticEnvelopeData, &semanticEnvelope); err != nil {
		return fmt.Errorf("decode prior semantic evidence envelope timestamp: %w", err)
	}
	semanticCreated, _ := time.Parse(time.RFC3339Nano, semanticEnvelope.CreatedAt)
	if manifestCreated.After(completionCreated) || supplementCreated.Before(completionCreated) || semanticCreated.Before(completionCreated) || semanticCreated.After(supplementCreated) {
		return errors.New("prior owner completion, semantic files, and semantic_verified supplement have invalid causal ordering")
	}
	var semantic FinalSemanticEvidence
	if err := decodeStrictJSONBytes(semanticData, &semantic); err != nil || VerifyFinalSemanticEvidence(&semantic) != nil || semantic.PublicVerification == nil || semantic.Phase != "release-1.0" || semantic.RunID != prior.RunID || semantic.ResultHash != prior.ResultHash || semantic.EvidenceHash != supplementPayload.SemanticEvidenceHash || semantic.PublicVerification.TranscriptHash != supplementPayload.PublicTranscriptHash || semantic.Window != prior.Window {
		return stateMismatchError(err, "prior signed semantic evidence differs from its semantic_verified supplement")
	}
	terminalNative, err := a.collectedPriorNativeTerminalHead(prior, result.DeploymentID, result.EndHead)
	if err != nil {
		return err
	}
	if semantic.NativeTerminalHead != terminalNative || semantic.EVMTerminalHead != result.EndHead {
		return errors.New("prior signed semantic evidence terminal heads differ from the closed chain snapshot")
	}
	a.priorSemantic = &semantic
	completionArtifact, err := a.derivedBytes("scenario-complete", "prior-release/complete.json", completionData)
	if err != nil {
		return err
	}
	manifestArtifact, err := a.derivedBytes("campaign-evidence-manifest", "prior-release/campaign-evidence-manifest.json", manifestData)
	if err != nil {
		return err
	}
	supplementArtifact, err := a.derivedBytes("prior-semantic-supplement-envelope", "prior-release/semantic-verified.evidence.json", supplementData)
	if err != nil {
		return err
	}
	semanticEnvelopeArtifact, err := a.derivedBytes("prior-semantic-file-envelope", "prior-release/final-semantic-evidence.evidence.json", semanticEnvelopeData)
	if err != nil {
		return err
	}
	semanticArtifact, err := a.derivedBytes("prior-semantic-evidence", "prior-release/final-semantic-evidence.json", semanticData)
	if err != nil {
		return err
	}
	source.PriorPhase = &FinalPriorPhaseBinding{
		RunID: prior.RunID, ResultHash: prior.ResultHash, SemanticEvidenceHash: semantic.EvidenceHash, PublicTranscriptHash: semantic.PublicVerification.TranscriptHash,
		OwnerCompletionEnvelopeHash: strings.ToLower(completion.ContentHash), EvidenceManifestEnvelopeHash: strings.ToLower(manifestEnvelope.ContentHash), SemanticSupplementEnvelopeHash: strings.ToLower(supplement.ContentHash),
		Completion: completionArtifact, EvidenceManifest: manifestArtifact, SemanticSupplement: supplementArtifact, SemanticEvidenceEnvelope: semanticEnvelopeArtifact, SemanticEvidence: semanticArtifact, AcceptanceWindow: prior.Window,
		TerminalNativeHead: terminalNative, TerminalEVMHead: result.EndHead,
	}
	return nil
}

func (a *finalSemanticArchive) collectedPriorNativeTerminalHead(prior *FinalCollectedPriorPhaseInputs, deploymentID string, evmTerminal ChainHead) (ChainHead, error) {
	if a == nil || prior == nil || len(prior.LiveChainBundles) == 0 {
		return ChainHead{}, errors.New("collected prior live-chain terminal context is incomplete")
	}
	var terminal ChainHead
	found := false
	for _, locator := range prior.LiveChainBundles {
		data, _, err := a.file(locator.URI)
		if err != nil {
			return ChainHead{}, err
		}
		bundle, err := decodeFinalCollectedFileBundle(data)
		if err != nil || finalSemanticBundleClass(bundle.Name) != "live-chain" {
			return ChainHead{}, stateMismatchError(err, "decode collected prior live-chain bundle")
		}
		for _, entry := range bundle.Files {
			if entry.Path != "live-chain/final-chain-snapshot.json" {
				continue
			}
			if found {
				return ChainHead{}, errors.New("collected prior graph contains duplicate live-chain snapshots")
			}
			var snapshot FinalCollectedChainSnapshot
			if err := decodeStrictJSONBytes(entry.Data, &snapshot); err != nil {
				return ChainHead{}, fmt.Errorf("decode collected prior live-chain snapshot: %w", err)
			}
			if snapshot.Schema != finalCollectedChainSnapshotSchema || snapshot.Phase != "release-1.0" || snapshot.RunID != prior.RunID || snapshot.DeploymentID != deploymentID || snapshot.EVMHead != evmTerminal || snapshot.EVMHead.Number != prior.Window.TerminalBlock || len(snapshot.NativeHeads) == 0 || snapshot.NativeHeads[len(snapshot.NativeHeads)-1] != snapshot.NativeHead {
				return ChainHead{}, errors.New("collected prior live-chain snapshot differs from its campaign")
			}
			if err := verifyFinalHead("collected prior terminal native", snapshot.NativeHead); err != nil {
				return ChainHead{}, err
			}
			terminal, found = snapshot.NativeHead, true
		}
	}
	if !found {
		return ChainHead{}, errors.New("collected prior graph has no live-chain terminal snapshot")
	}
	return terminal, nil
}

// priorNativeTerminalHead follows the hash-bound prior collected-input
// manifest to its already-closed live-chain bundle. It never consults a live
// service or an unlisted state file.
func (a *finalSemanticArchive) priorNativeTerminalHead(prior *FinalCollectedPriorPhaseInputs, deploymentID string, evmTerminal ChainHead) (ChainHead, error) {
	if a == nil || a.cfg == nil || prior == nil || prior.RunID == "" || strings.ContainsAny(prior.RunID, "/\\\r\n\x00") {
		return ChainHead{}, errors.New("prior native terminal context is incomplete")
	}
	manifestData, _, err := a.file(prior.CollectedInputsManifest.URI)
	if err != nil {
		return ChainHead{}, err
	}
	var collected FinalSemanticCollectedInputs
	if err := decodeStrictJSONBytes(manifestData, &collected); err != nil {
		return ChainHead{}, fmt.Errorf("decode prior collected-input manifest: %w", err)
	}
	if err := verifyFinalSemanticCollectedInputs(a.cfg, &collected); err != nil {
		return ChainHead{}, fmt.Errorf("verify prior collected-input manifest: %w", err)
	}
	if collected.Phase != "release-1.0" || collected.RunID != prior.RunID || collected.ResultHash != prior.ResultHash || collected.Window != prior.Window {
		return ChainHead{}, errors.New("prior collected-input manifest differs from its lineage binding")
	}
	var terminal ChainHead
	found := false
	for _, locator := range collected.ClosedInputBundles {
		data, err := a.loadPriorRunArtifact(prior.RunID, locator)
		if err != nil {
			return ChainHead{}, err
		}
		bundle, err := decodeFinalCollectedFileBundle(data)
		if err != nil {
			return ChainHead{}, fmt.Errorf("decode prior closed bundle %s: %w", locator.URI, err)
		}
		if finalSemanticBundleClass(bundle.Name) != "live-chain" {
			continue
		}
		for _, entry := range bundle.Files {
			if entry.Path != "live-chain/final-chain-snapshot.json" {
				continue
			}
			if found {
				return ChainHead{}, errors.New("prior collected graph contains duplicate live-chain snapshots")
			}
			var snapshot FinalCollectedChainSnapshot
			if err := decodeStrictJSONBytes(entry.Data, &snapshot); err != nil {
				return ChainHead{}, fmt.Errorf("decode prior live-chain snapshot: %w", err)
			}
			if snapshot.Schema != finalCollectedChainSnapshotSchema || snapshot.Phase != "release-1.0" || snapshot.RunID != prior.RunID || snapshot.DeploymentID != deploymentID || snapshot.EVMHead != evmTerminal || snapshot.EVMHead.Number != prior.Window.TerminalBlock || len(snapshot.NativeHeads) == 0 || snapshot.NativeHeads[len(snapshot.NativeHeads)-1] != snapshot.NativeHead {
				return ChainHead{}, errors.New("prior live-chain snapshot differs from its campaign")
			}
			if err := verifyFinalHead("prior terminal native", snapshot.NativeHead); err != nil {
				return ChainHead{}, err
			}
			terminal, found = snapshot.NativeHead, true
		}
	}
	if !found {
		return ChainHead{}, errors.New("prior collected graph has no live-chain terminal snapshot")
	}
	return terminal, nil
}

func (a *finalSemanticArchive) loadPriorRunArtifact(runID string, locator FinalArtifactLocator) ([]byte, error) {
	if a == nil || a.stateRoot == "" || runID == "" || strings.ContainsAny(runID, "/\\\r\n\x00") {
		return nil, errors.New("prior run artifact context is invalid")
	}
	if err := verifyFinalArtifact("prior run closed artifact", locator, locator.Kind); err != nil {
		return nil, err
	}
	root := filepath.Join(a.stateRoot, "runs", runID)
	clean := filepath.Clean(filepath.FromSlash(locator.URI))
	absolute, err := filepath.Abs(filepath.Join(root, clean))
	if err != nil || !pathWithinRoot(root, absolute) {
		return nil, errors.New("prior run artifact escapes its authenticated run root")
	}
	if err := rejectFinalArtifactSymlinkComponents(root, absolute); err != nil {
		return nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("prior run artifact is not a regular file")
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) != locator.SizeBytes || bytesSHA256(data) != locator.ContentHash {
		return nil, fmt.Errorf("prior run artifact %s differs from its collected locator", locator.URI)
	}
	return data, nil
}

func (a *finalSemanticArchive) buildDeploymentAndReserve(source *FinalSemanticEvidence, terminal *ScenarioObservation, identities *finalPublicIdentities, events *finalSemanticEventIndex) error {
	if source == nil || terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil || terminal.Status.Contracts.Deployment == nil || identities == nil || events == nil {
		return errors.New("terminal contract deployment evidence is incomplete")
	}
	view := terminal.Status.Contracts
	deployment := view.Deployment
	var plan SetupPlan
	if err := a.decode("launch-foundation/plan.json", &plan); err != nil {
		return fmt.Errorf("decode closed setup plan for contract custody: %w", err)
	}
	planHash, err := plan.hash()
	if err != nil || !strings.EqualFold(planHash, source.PlanHash) || plan.DeploymentID != source.DeploymentID || plan.Netuid != source.Netuid || plan.LiveFacts.DefaultMinTransferRao == 0 {
		return stateMismatchError(err, "closed setup plan does not bind the contract custody inputs")
	}
	activeImplementation := view.CoordinatorUpgrade.Implementation
	if activeImplementation == (common.Address{}) {
		activeImplementation = deployment.CoordinatorImplementation
	}
	codeHash := func(address common.Address) (string, error) {
		for encoded, hash := range view.RuntimeCodeHashes {
			if common.IsHexAddress(encoded) && common.HexToAddress(encoded) == address {
				if err := requireFinalHex32("runtime code hash", strings.ToLower(hash)); err != nil {
					return "", err
				}
				return strings.ToLower(hash), nil
			}
		}
		return "", fmt.Errorf("terminal runtime hash for %s is absent", address.Hex())
	}
	proxyHash, err := codeHash(deployment.CoordinatorProxy)
	if err != nil {
		return err
	}
	implementationHash, err := codeHash(activeImplementation)
	if err != nil {
		return err
	}
	vaultHash, err := codeHash(deployment.SettlementVault)
	if err != nil {
		return err
	}
	reserveHash, err := codeHash(deployment.ReserveSink)
	if err != nil {
		return err
	}
	policyVersions := uint64(1)
	// Each PolicyScheduled log appends exactly one immutable policy version.
	if snapshot, snapshotErr := a.chainSnapshot(); snapshotErr == nil {
		if indexed, indexErr := indexFinalSemanticEvents(snapshot); indexErr == nil {
			policyVersions += uint64(len(indexed.byName["PolicyScheduled"]))
		}
	}
	owner := strings.ToLower(identities.EVM["testnet-owner"])
	if !common.IsHexAddress(owner) || common.HexToAddress(owner) == (common.Address{}) || !strings.EqualFold(view.CoordinatorOwner, owner) {
		return errors.New("terminal coordinator owner differs from the public testnet owner")
	}
	guardian := strings.ToLower(identities.EVM["guardian"])
	oracle := strings.ToLower(identities.EVM["commitment-oracle"])
	escrowIdentity, escrowOK := identities.Substrate[escrowHotkeyLabelForGeneration(deployment.RegistrationRoleGeneration)]
	reserveIdentity, reserveOK := identities.Substrate["reserve-hotkey"]
	custody := view.CustodyIdentity
	coordinatorMirror := ss58.EvmMirrorPubkey(deployment.CoordinatorProxy)
	vaultMirror := ss58.EvmMirrorPubkey(deployment.SettlementVault)
	reserveMirror := ss58.EvmMirrorPubkey(deployment.ReserveSink)
	minimumTTL, ttlOK := checkedMul(a.cfg.Policy.Settlement.EpochBlocks, a.cfg.Policy.Settlement.ClaimTTLEpochs)
	if !common.IsHexAddress(guardian) || common.HexToAddress(guardian) == (common.Address{}) ||
		!common.IsHexAddress(oracle) || common.HexToAddress(oracle) == (common.Address{}) || !escrowOK || !reserveOK || !ttlOK ||
		custody.CoordinatorNetuid != source.Netuid || custody.VaultNetuid != source.Netuid || custody.ReserveNetuid != source.Netuid ||
		!strings.EqualFold(custody.CoordinatorSelfColdkey, fmt.Sprintf("0x%x", coordinatorMirror)) ||
		!strings.EqualFold(custody.VaultSelfColdkey, fmt.Sprintf("0x%x", vaultMirror)) ||
		!strings.EqualFold(custody.ReserveSelfColdkey, fmt.Sprintf("0x%x", reserveMirror)) ||
		!strings.EqualFold(custody.CoordinatorVault, deployment.SettlementVault.Hex()) ||
		!strings.EqualFold(custody.CoordinatorReserve, deployment.ReserveSink.Hex()) ||
		!strings.EqualFold(custody.VaultCoordinator, deployment.CoordinatorProxy.Hex()) ||
		!strings.EqualFold(custody.ReserveRecorder, deployment.CoordinatorProxy.Hex()) ||
		!strings.EqualFold(custody.CoordinatorGuardian, guardian) || !strings.EqualFold(custody.CoordinatorActiveGuardian, guardian) || custody.CoordinatorPaused ||
		!strings.EqualFold(custody.CoordinatorCommitmentOracle, oracle) || !strings.EqualFold(custody.CoordinatorActiveCommitmentOracle, oracle) ||
		!strings.EqualFold(custody.VaultEscrowHotkey, escrowIdentity.PublicKey) || !custody.VaultEscrowRegistered ||
		custody.VaultMinimumClaimTTLBlocks != minimumTTL || custody.VaultMinimumTransferRao != plan.LiveFacts.DefaultMinTransferRao ||
		!strings.EqualFold(custody.ReserveHotkey, reserveIdentity.PublicKey) {
		return errors.New("terminal contract custody differs from the signed plan, identities, or immutable deployment")
	}
	artifact, err := a.derived("contract-deployment", "contract-deployment.json", map[string]any{
		"deployment": deployment, "upgrade": view.CoordinatorUpgrade, "terminal": view.FinalizedHead,
		"runtime_code_hashes": view.RuntimeCodeHashes, "policy": view.Policy, "custody": custody,
		"plan_hash": source.PlanHash, "plan_default_min_transfer_rao": plan.LiveFacts.DefaultMinTransferRao,
		"expected_guardian": guardian, "expected_commitment_oracle": oracle,
	})
	if err != nil {
		return err
	}
	source.Deployment = FinalContractDeploymentEvidence{
		CoordinatorProxy: strings.ToLower(deployment.CoordinatorProxy.Hex()), CoordinatorImplementation: strings.ToLower(activeImplementation.Hex()),
		SettlementVault: strings.ToLower(deployment.SettlementVault.Hex()), ReserveSink: strings.ToLower(deployment.ReserveSink.Hex()), GovernanceOwner: owner,
		CoordinatorNetuid: custody.CoordinatorNetuid, CoordinatorSelfColdkey: strings.ToLower(custody.CoordinatorSelfColdkey),
		CoordinatorSettlementVault: strings.ToLower(custody.CoordinatorVault), CoordinatorReserveSink: strings.ToLower(custody.CoordinatorReserve),
		CoordinatorGuardian: strings.ToLower(custody.CoordinatorGuardian), CoordinatorActiveGuardian: strings.ToLower(custody.CoordinatorActiveGuardian), CoordinatorPaused: custody.CoordinatorPaused,
		CoordinatorCommitmentOracle: strings.ToLower(custody.CoordinatorCommitmentOracle), CoordinatorActiveCommitmentOracle: strings.ToLower(custody.CoordinatorActiveCommitmentOracle),
		VaultCoordinator: strings.ToLower(custody.VaultCoordinator), VaultNetuid: custody.VaultNetuid, VaultSelfColdkey: strings.ToLower(custody.VaultSelfColdkey),
		VaultEscrowHotkey: strings.ToLower(custody.VaultEscrowHotkey), VaultEscrowRegistered: custody.VaultEscrowRegistered,
		VaultMinimumClaimTTLBlocks: custody.VaultMinimumClaimTTLBlocks, VaultMinimumTransferTaoRao: custody.VaultMinimumTransferRao,
		PlanDefaultMinTransferTaoRao: plan.LiveFacts.DefaultMinTransferRao,
		ReserveRecorder:              strings.ToLower(custody.ReserveRecorder), ReserveNetuid: custody.ReserveNetuid,
		ReserveSelfColdkey: strings.ToLower(custody.ReserveSelfColdkey), ReserveHotkey: strings.ToLower(custody.ReserveHotkey),
		CoordinatorProxyCodeHash: proxyHash, ImplementationCodeHash: implementationHash, SettlementVaultCodeHash: vaultHash, ReserveSinkCodeHash: reserveHash,
		ERC1967ImplementationSlot:  "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc",
		ObservedImplementationSlot: "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(activeImplementation.Hex()), "0x"),
		PolicyVersion:              policyVersions, PolicyEffectiveEpoch: view.Policy.EffectiveEpoch, PolicyEffectiveBlock: view.Policy.EffectiveBlock,
		Snapshot: view.FinalizedHead, Artifact: artifact,
	}
	var baseline *ScenarioObservation
	var history []*ScenarioObservation
	if err := a.decode(a.collected.ObservationHistory.URI, &history); err != nil {
		return err
	}
	for _, observation := range history {
		if observation != nil && observation.Status != nil && observation.Status.Contracts != nil && observation.Status.Contracts.FinalizedHead == source.Window.BaselineHead {
			baseline = observation
			break
		}
	}
	if baseline == nil || baseline.Status.Contracts.ReservePrincipal == "" || baseline.Status.Contracts.ReserveLiveStake == "" || view.ReservePrincipal == "" || view.ReserveLiveStake == "" {
		return errors.New("reserve baseline or terminal snapshot is absent from closed observations")
	}
	accounting, err := finalSemanticSettlementAccounting(baseline.Status.Contracts, view, source.Window.BaselineHead, source.EVMTerminalHead, deployment.SettlementVault, events)
	if err != nil {
		return err
	}
	principalBefore, ok := new(big.Int).SetString(baseline.Status.Contracts.ReservePrincipal, 10)
	if !ok || principalBefore.Sign() < 0 || principalBefore.String() != baseline.Status.Contracts.ReservePrincipal {
		return errors.New("reserve baseline principal is not a canonical nonnegative integer")
	}
	principalAfter, ok := new(big.Int).SetString(view.ReservePrincipal, 10)
	if !ok || principalAfter.Sign() < 0 || principalAfter.String() != view.ReservePrincipal || principalAfter.Cmp(principalBefore) < 0 {
		return errors.New("reserve terminal principal is not a canonical monotonic integer")
	}
	liveBefore, ok := new(big.Int).SetString(baseline.Status.Contracts.ReserveLiveStake, 10)
	if !ok || liveBefore.Sign() < 0 || liveBefore.String() != baseline.Status.Contracts.ReserveLiveStake || liveBefore.Cmp(principalBefore) <= 0 {
		return errors.New("reserve baseline live stake does not exceed principal")
	}
	liveAfter, ok := new(big.Int).SetString(view.ReserveLiveStake, 10)
	if !ok || liveAfter.Sign() < 0 || liveAfter.String() != view.ReserveLiveStake || liveAfter.Cmp(principalAfter) <= 0 || liveAfter.Cmp(liveBefore) < 0 {
		return errors.New("reserve terminal live stake is not canonical, monotonic, and above principal")
	}
	principalAdditions := make([]FinalReservePrincipalAddedEvidence, 0)
	principalAdded := new(big.Int)
	runningPrincipal := new(big.Int).Set(principalBefore)
	operatorPrincipalByNO := make(map[uint64]*big.Int, source.ExpectedOperators)
	for index, event := range events.byName["ReservePrincipalAdded"] {
		if event.Log.BlockNumber <= source.Window.BaselineHead.Number {
			continue
		}
		if event.Log.BlockNumber > source.EVMTerminalHead.Number || !strings.EqualFold(event.Log.Address, deployment.ReserveSink.Hex()) {
			return errors.New("ReservePrincipalAdded event is outside the acceptance interval or reserve sink")
		}
		epoch, epochOK := finalSemanticUint(event.Args, "epoch")
		noID, noOK := finalSemanticUint(event.Args, "noId")
		amount, amountOK := finalSemanticInteger(event.Args, "amount")
		operatorPrincipal, operatorOK := finalSemanticInteger(event.Args, "operatorPrincipal")
		totalPrincipal, totalOK := finalSemanticInteger(event.Args, "totalPrincipal")
		liveStake, liveOK := finalSemanticInteger(event.Args, "liveStake")
		if !epochOK || epoch < source.Window.FirstEpoch || epoch >= source.Window.FirstEpoch+source.Window.EpochCount || !noOK || noID == 0 || noID > uint64(source.ExpectedOperators) || !amountOK || amount.Sign() <= 0 || !operatorOK || operatorPrincipal.Sign() <= 0 || !totalOK || totalPrincipal.Sign() <= 0 || !liveOK || liveStake.Sign() <= 0 {
			return errors.New("ReservePrincipalAdded event has invalid accounting fields")
		}
		wantTotal := new(big.Int).Add(runningPrincipal, amount)
		priorOperator, seenOperator := operatorPrincipalByNO[noID]
		operatorConsistent := operatorPrincipal.Cmp(amount) >= 0
		if seenOperator {
			operatorConsistent = operatorPrincipal.Cmp(new(big.Int).Add(priorOperator, amount)) == 0
		}
		if totalPrincipal.Cmp(wantTotal) != 0 || !operatorConsistent || liveStake.Cmp(totalPrincipal) <= 0 {
			return errors.New("ReservePrincipalAdded cumulative operator/total/live accounting is inconsistent")
		}
		receipt, receiptErr := a.receiptFromIndex(events, event, fmt.Sprintf("reserve-principal-added-%03d", index+1))
		if receiptErr != nil {
			return receiptErr
		}
		principalAdded.Add(principalAdded, amount)
		runningPrincipal.Set(totalPrincipal)
		operatorPrincipalByNO[noID] = new(big.Int).Set(operatorPrincipal)
		principalAdditions = append(principalAdditions, FinalReservePrincipalAddedEvidence{
			Epoch: epoch, NoID: noID, AmountRao: amount.String(), OperatorPrincipalRao: operatorPrincipal.String(),
			TotalPrincipalRao: totalPrincipal.String(), LiveStakeRao: liveStake.String(), Receipt: receipt,
		})
	}
	principalDelta := new(big.Int).Sub(new(big.Int).Set(principalAfter), principalBefore)
	if len(principalAdditions) == 0 || principalAdded.Cmp(principalDelta) != 0 || runningPrincipal.Cmp(principalAfter) != 0 {
		return errors.New("ReservePrincipalAdded event sum differs from reserve principal delta")
	}
	reserveArtifact, err := a.derived("reserve-state", "reserve-state.json", map[string]any{
		"before": baseline.Status.Contracts, "after": view, "settlement_accounting": accounting, "principal_additions": principalAdditions,
	})
	if err != nil {
		return err
	}
	source.SettlementAccounting = accounting
	source.Reserve = FinalReserveEvidence{
		PrincipalBeforeRao: baseline.Status.Contracts.ReservePrincipal, PrincipalAfterRao: view.ReservePrincipal,
		PrincipalDeltaRao: principalDelta.String(), PrincipalAddedRao: principalAdded.String(), PrincipalAdditions: principalAdditions,
		LiveStakeBeforeRao: baseline.Status.Contracts.ReserveLiveStake, LiveStakeAfterRao: view.ReserveLiveStake,
		Before: source.Window.BaselineHead, After: source.EVMTerminalHead, Artifact: reserveArtifact,
	}
	return nil
}

func finalSemanticSettlementVaultState(view *ContractView, head ChainHead) (FinalSettlementVaultState, map[string]*big.Int, error) {
	if view == nil || view.FinalizedHead != head {
		return FinalSettlementVaultState{}, nil, errors.New("settlement-vault accounting snapshot is not bound to the requested head")
	}
	encoded := map[string]string{
		"total captured": view.TotalCaptured, "total paid": view.TotalPaid, "escrow accounted": view.EscrowAccounted,
		"pending funding": view.PendingFunding, "outstanding liability": view.Outstanding, "live escrow stake": view.LiveEscrowStake,
	}
	values := make(map[string]*big.Int, len(encoded))
	for name, value := range encoded {
		parsed, ok := new(big.Int).SetString(value, 10)
		if !ok || parsed.Sign() < 0 || parsed.String() != value {
			return FinalSettlementVaultState{}, nil, fmt.Errorf("settlement-vault %s is not a canonical nonnegative integer", name)
		}
		values[name] = parsed
	}
	if values["total captured"].Cmp(new(big.Int).Add(values["total paid"], values["escrow accounted"])) != 0 ||
		values["escrow accounted"].Cmp(new(big.Int).Add(values["pending funding"], values["outstanding liability"])) != 0 ||
		values["live escrow stake"].Cmp(values["escrow accounted"]) < 0 || !view.ConservationHolds {
		return FinalSettlementVaultState{}, nil, errors.New("settlement-vault snapshot violates exact global accounting")
	}
	return FinalSettlementVaultState{
		TotalCapturedRao: view.TotalCaptured, TotalPaidRao: view.TotalPaid, EscrowAccountedRao: view.EscrowAccounted,
		PendingFundingRao: view.PendingFunding, OutstandingLiabilityRao: view.Outstanding, LiveEscrowStakeRao: view.LiveEscrowStake, Block: head,
	}, values, nil
}

func finalSemanticSettlementAccounting(beforeView, afterView *ContractView, beforeHead, afterHead ChainHead, vault common.Address, events *finalSemanticEventIndex) (FinalSettlementVaultAccounting, error) {
	if events == nil || vault == (common.Address{}) || beforeHead.Number >= afterHead.Number {
		return FinalSettlementVaultAccounting{}, errors.New("settlement-vault accounting interval is incomplete")
	}
	before, beforeValues, err := finalSemanticSettlementVaultState(beforeView, beforeHead)
	if err != nil {
		return FinalSettlementVaultAccounting{}, fmt.Errorf("baseline %w", err)
	}
	after, afterValues, err := finalSemanticSettlementVaultState(afterView, afterHead)
	if err != nil {
		return FinalSettlementVaultAccounting{}, fmt.Errorf("terminal %w", err)
	}
	delta := func(name string) *big.Int { return new(big.Int).Sub(afterValues[name], beforeValues[name]) }
	capturedDelta := delta("total captured")
	paidDelta := delta("total paid")
	if capturedDelta.Sign() < 0 || paidDelta.Sign() < 0 {
		return FinalSettlementVaultAccounting{}, errors.New("settlement-vault cumulative counters decreased")
	}
	eventSum := func(name, amountName string) (*big.Int, error) {
		total := new(big.Int)
		for _, event := range events.byName[name] {
			if event.Log.BlockNumber <= beforeHead.Number {
				continue
			}
			if event.Log.BlockNumber > afterHead.Number || !strings.EqualFold(event.Log.Address, vault.Hex()) {
				return nil, fmt.Errorf("%s event is outside the accounting interval or settlement vault", name)
			}
			amount, ok := finalSemanticInteger(event.Args, amountName)
			if !ok || amount.Sign() <= 0 {
				return nil, fmt.Errorf("%s event amount is invalid", name)
			}
			total.Add(total, amount)
		}
		return total, nil
	}
	capturedEvents, err := eventSum("EmissionCaptured", "amount")
	if err != nil {
		return FinalSettlementVaultAccounting{}, err
	}
	paidEvents, err := eventSum("ClaimPaid", "amount")
	if err != nil {
		return FinalSettlementVaultAccounting{}, err
	}
	if capturedEvents.Cmp(capturedDelta) != 0 || paidEvents.Cmp(paidDelta) != 0 {
		return FinalSettlementVaultAccounting{}, errors.New("settlement-vault cumulative deltas differ from EmissionCaptured/ClaimPaid event sums")
	}
	return FinalSettlementVaultAccounting{
		Before: before, After: after, TotalCapturedDeltaRao: capturedDelta.String(), TotalPaidDeltaRao: paidDelta.String(),
		EscrowAccountedDeltaRao: delta("escrow accounted").String(), PendingFundingDeltaRao: delta("pending funding").String(),
		OutstandingLiabilityDeltaRao: delta("outstanding liability").String(), LiveEscrowStakeDeltaRao: delta("live escrow stake").String(),
		EmissionCapturedEventRao: capturedEvents.String(), ClaimPaidEventRao: paidEvents.String(),
	}, nil
}

func (a *finalSemanticArchive) buildArchiveRetention(source *FinalSemanticEvidence, chain *FinalCollectedChainSnapshot) error {
	if a == nil || a.cfg == nil || a.cfg.Config == nil || source == nil || chain == nil || source.CampaignStartedAt == "" {
		return errors.New("archive-retention semantic context is incomplete")
	}
	var public PublicDeploymentManifest
	if err := a.decode("launch-foundation/public.json", &public); err != nil {
		return fmt.Errorf("decode closed public deployment manifest: %w", err)
	}
	if public.Schema != "urnetwork-sim-public-deployment-v1" || public.Release != "1.0" || public.Contracts == nil || public.DeploymentID != source.DeploymentID || public.Contracts.DeploymentID != source.DeploymentID || public.ChainID != source.ChainID || !strings.EqualFold(public.GenesisHash, source.GenesisHash) || public.Netuid != source.Netuid || public.ConfigHash != source.ConfigHash || !strings.EqualFold(public.PolicyHash, source.PolicyHash) || public.PlanHash != source.PlanHash || validatePublicManifestRevision(&public) != nil {
		return errors.New("closed public deployment manifest differs from the semantic campaign identity")
	}
	if err := validatePublishedRuntimeIdentity(&public, a.cfg); err != nil {
		return fmt.Errorf("closed public deployment manifest runtime identity: %w", err)
	}
	wantTopologyHash, err := canonicalHashHex(a.cfg.Config.Topology)
	if err != nil {
		return err
	}
	publicTopologyHash, err := canonicalHashHex(public.Topology)
	if err != nil || publicTopologyHash != wantTopologyHash {
		return errors.New("closed public deployment manifest topology differs from release configuration")
	}
	publicImplementation := public.Contracts.CoordinatorImplementation
	if public.CoordinatorUpgrade.Implementation != (common.Address{}) {
		publicImplementation = public.CoordinatorUpgrade.Implementation
	}
	if public.Contracts.DeployBlock == 0 || public.Contracts.DeployBlock != chain.EVMFromBlock || !strings.EqualFold(public.Contracts.CoordinatorProxy.Hex(), source.Deployment.CoordinatorProxy) || !strings.EqualFold(publicImplementation.Hex(), source.Deployment.CoordinatorImplementation) || !strings.EqualFold(public.Contracts.SettlementVault.Hex(), source.Deployment.SettlementVault) || !strings.EqualFold(public.Contracts.ReserveSink.Hex(), source.Deployment.ReserveSink) {
		return errors.New("closed public deployment manifest contract identity differs from captured chain evidence")
	}
	publicHash, err := canonicalHashHex(&public)
	if err != nil {
		return err
	}
	manifestGeneratedAt, err := time.Parse(time.RFC3339Nano, public.GeneratedAt)
	if err != nil || public.GeneratedAt != manifestGeneratedAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("closed public deployment manifest timestamp is not canonical UTC")
	}
	campaignStartedAt, err := time.Parse(time.RFC3339Nano, source.CampaignStartedAt)
	if err != nil || source.CampaignStartedAt != campaignStartedAt.UTC().Format(time.RFC3339Nano) {
		return errors.New("semantic campaign start timestamp is not canonical UTC")
	}
	plannedSpan, err := FinalCompositeArchiveSpan(a.cfg)
	if err != nil {
		return err
	}
	safetyMargin, err := finalArchiveReviewerSafetyMargin(a.cfg)
	if err != nil {
		return err
	}
	type archiveCandidate struct {
		value     FinalArchiveRetentionPreflight
		data      []byte
		generated time.Time
	}
	paths := make([]string, 0)
	for path := range a.files {
		if strings.HasPrefix(path, "receipts/archive-retention-preflight-") && strings.HasSuffix(path, ".json") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	candidates := make([]archiveCandidate, 0, len(paths))
	for _, path := range paths {
		data := a.files[path]
		var receipt FinalArchiveRetentionPreflight
		if err := decodeStrictJSONBytes(data, &receipt); err != nil {
			return fmt.Errorf("decode closed archive-retention receipt %s: %w", path, err)
		}
		if err := verifyFinalArchiveRetentionPreflight(&receipt); err != nil {
			return fmt.Errorf("verify closed archive-retention receipt %s: %w", path, err)
		}
		wantPath := "receipts/archive-retention-preflight-" + strings.TrimPrefix(strings.ToLower(receipt.EvidenceHash), "0x") + ".json"
		if path != wantPath {
			return fmt.Errorf("closed archive-retention receipt path %s differs from evidence hash", path)
		}
		generated, err := time.Parse(time.RFC3339Nano, receipt.GeneratedAt)
		if err != nil {
			return err
		}
		matches := receipt.DeploymentID == source.DeploymentID && strings.EqualFold(receipt.PublicManifestHash, publicHash) && receipt.PlannedSpanBlocks >= plannedSpan && receipt.SafetyMarginBlocks >= safetyMargin && !generated.Before(manifestGeneratedAt) && !generated.After(campaignStartedAt) && campaignStartedAt.Sub(generated) <= finalArchiveReviewerSafetyWindow
		matches = matches && receipt.Substrate.Endpoint == public.SubstrateRPC && receipt.EVM.Endpoint == public.EVMRPC && receipt.EVM.DeploymentHead.Number == public.Contracts.DeployBlock && strings.EqualFold(receipt.EVM.DeploymentHead.Hash, public.Contracts.DeployBlockHash)
		if matches {
			candidates = append(candidates, archiveCandidate{value: receipt, data: append([]byte(nil), data...), generated: generated})
		}
	}
	if len(candidates) == 0 {
		return errors.New("closed campaign has no fresh archive-retention receipt for its exact public manifest and composite span")
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].generated.Equal(candidates[j].generated) {
			return candidates[i].generated.After(candidates[j].generated)
		}
		return candidates[i].value.EvidenceHash < candidates[j].value.EvidenceHash
	})
	selected := candidates[0]
	artifact, err := a.derivedBytes("archive-retention-preflight", "archive-retention-preflight-"+strings.TrimPrefix(strings.ToLower(selected.value.EvidenceHash), "0x")+".json", selected.data)
	if err != nil {
		return err
	}
	source.ArchiveRetention = FinalArchiveRetentionEvidence{
		GeneratedAt: selected.value.GeneratedAt, DeploymentID: selected.value.DeploymentID, PublicManifestHash: strings.ToLower(selected.value.PublicManifestHash),
		PlannedSpanBlocks: selected.value.PlannedSpanBlocks, SafetyMarginBlocks: selected.value.SafetyMarginBlocks, RequiredDepthBlocks: selected.value.RequiredDepthBlocks,
		EvidenceHash: strings.ToLower(selected.value.EvidenceHash), Artifact: artifact,
	}
	return nil
}

func (a *finalSemanticArchive) buildCleanup(source *FinalSemanticEvidence) error {
	var supervisor SupervisorState
	if err := a.decode("launch-foundation/supervisor.state.json", &supervisor); err != nil {
		return err
	}
	cutoff, err := time.Parse(time.RFC3339Nano, supervisor.ContractCleanupCutoff)
	if err != nil {
		return errors.New("captured supervisor cleanup cutoff is invalid")
	}
	stateData, _, err := a.file("launch-foundation/supervisor.state.json")
	if err != nil {
		return err
	}
	stateLocator, err := a.derivedBytes("supervisor-cleanup-generation", "accepted-supervisor-cleanup.json", stateData)
	if err != nil {
		return err
	}
	cleanup := FinalContractCleanupEvidence{
		Schema: "urnetwork-sim-final-contract-cleanup-v1", Cutoff: supervisor.ContractCleanupCutoff, CutoffUnixNano: cutoff.UnixNano(),
		SupervisorManifestHash: supervisor.ManifestHash, SupervisorStartTimeTicks: supervisor.SupervisorStartTimeTicks,
		SupervisorStateArtifact: stateLocator,
	}
	processByID := map[string]ProcessState{}
	for _, process := range supervisor.Processes {
		processByID[process.ID] = process
	}
	for noID := 1; noID <= source.ExpectedOperators; noID++ {
		id := fmt.Sprintf("operator-%d-taskworker", noID)
		process, ok := processByID[id]
		if !ok || !process.Healthy || process.ExitError != "" {
			return fmt.Errorf("accepted cleanup taskworker %s is not healthy", id)
		}
		base := fmt.Sprintf("%s-contract-cleanup-%d", id, cutoff.UnixNano())
		resultData, _, err := a.file("accepted-contract-cleanup/processes/" + base + ".json")
		if err != nil {
			return err
		}
		logData, _, err := a.file("accepted-contract-cleanup/processes/" + base + ".log")
		if err != nil {
			return err
		}
		var result serverContractCleanupResult
		if err := decodeStrictJSONBytes(resultData, &result); err != nil {
			return err
		}
		resultLocator, err := a.derivedBytes("server-contract-cleanup-result", base+".json", resultData)
		if err != nil {
			return err
		}
		logLocator, err := a.derivedBytes("server-contract-cleanup-log", base+".log", logData)
		if err != nil {
			return err
		}
		cleanup.Operators = append(cleanup.Operators, FinalOperatorContractCleanupEvidence{NoID: uint64(noID), TaskworkerID: id, Passes: result.Passes, Closed: result.Closed, Converged: result.Converged, ResultArtifact: resultLocator, LogArtifact: logLocator})
		if result.Converged {
			cleanup.SuccessfulInvocations++
		} else {
			cleanup.FailedInvocations++
		}
	}
	source.ContractCleanup = cleanup
	return nil
}
func (a *finalSemanticArchive) buildPools(source *FinalSemanticEvidence, terminal *ScenarioObservation, identities *finalPublicIdentities, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) error {
	if terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil || identities == nil || chain == nil || events == nil {
		return errors.New("pool construction context is incomplete")
	}
	nativeByUID := map[uint16]FinalCollectedNativeUIDState{}
	for _, state := range chain.NativeUIDs {
		nativeByUID[state.UID] = state
	}
	observedByNO := map[int]OperatorObservation{}
	for _, operator := range terminal.Operators {
		observedByNO[operator.NoID] = operator
	}
	if !common.IsHexAddress(source.Deployment.SettlementVault) {
		return errors.New("settlement vault address is invalid while constructing pool custody")
	}
	vaultMirror := ss58.EvmMirrorPubkey(common.HexToAddress(source.Deployment.SettlementVault))
	vaultMirrorHex := "0x" + hex.EncodeToString(vaultMirror[:])
	vaultMirrorSS58, err := ss58.Encode(vaultMirror, ss58.BittensorPrefix)
	if err != nil {
		return fmt.Errorf("encode settlement-vault custody coldkey: %w", err)
	}
	for _, operator := range terminal.Status.Contracts.Operators {
		noID := operator.NoID
		if noID == 0 {
			return errors.New("terminal operator has zero identity")
		}
		state, ok := nativeByUID[operator.PoolUID]
		if !ok || !operator.PoolLive || !strings.EqualFold(state.HotkeyPublicKey, operator.PoolHotkey) || !strings.EqualFold(state.ColdkeyPublicKey, vaultMirrorHex) {
			return fmt.Errorf("operator %d pool UID ownership differs from terminal native state", noID)
		}
		var registration *finalSemanticEvent
		for i := range events.byName["PoolRegistered"] {
			event := &events.byName["PoolRegistered"][i]
			eventNO, noOK := finalSemanticUint(event.Args, "noId")
			eventUID, uidOK := finalSemanticUint(event.Args, "uid")
			eventHotkey, hotkeyOK := finalSemanticHex32(event.Args, "hotkey")
			if noOK && uidOK && hotkeyOK && eventNO == noID && uint16(eventUID) == operator.PoolUID && strings.EqualFold(eventHotkey, operator.PoolHotkey) {
				if registration != nil {
					return fmt.Errorf("operator %d has duplicate pool registration events", noID)
				}
				registration = event
			}
		}
		if registration == nil {
			return fmt.Errorf("operator %d pool registration event is absent", noID)
		}
		registrationReceipt, err := a.receiptFromIndex(events, *registration, fmt.Sprintf("pool-%d-registration", noID))
		if err != nil {
			return err
		}
		var conviction *finalSemanticEvent
		for _, name := range []string{"Deposit", "ConvictionAdded"} {
			for i := range events.byName[name] {
				event := &events.byName[name][i]
				eventNO, ok := finalSemanticUint(event.Args, "noId")
				if ok && eventNO == noID && (conviction == nil || event.Log.BlockNumber > conviction.Log.BlockNumber || (event.Log.BlockNumber == conviction.Log.BlockNumber && event.Log.LogIndex > conviction.Log.LogIndex)) {
					conviction = event
				}
			}
		}
		if conviction == nil {
			return fmt.Errorf("operator %d has no captured conviction-changing receipt", noID)
		}
		convictionReceipt, err := a.receiptFromIndex(events, *conviction, fmt.Sprintf("pool-%d-terminal-conviction", noID))
		if err != nil {
			return err
		}
		versionCount := uint64(0)
		for _, event := range events.byName["OperatorScheduled"] {
			if eventNO, ok := finalSemanticUint(event.Args, "noId"); ok && eventNO == noID {
				versionCount++
			}
		}
		observation, ok := observedByNO[int(noID)]
		if !ok || len(observation.VerifyKeys) == 0 {
			return fmt.Errorf("operator %d server verification key history is absent", noID)
		}
		keys := make([]FinalServerKey, 0, len(observation.VerifyKeys))
		for _, key := range observation.VerifyKeys {
			if len(key.PublicKey) != ed25519.PublicKeySize {
				return fmt.Errorf("operator %d server key %d has invalid size", noID, key.ServerKeyID)
			}
			keys = append(keys, FinalServerKey{KeyID: key.ServerKeyID, PublicKey: "0x" + hex.EncodeToString(key.PublicKey)})
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].KeyID < keys[j].KeyID })
		ownershipArtifact, err := a.derived("native-ownership", fmt.Sprintf("pool-%d-native-ownership.json", noID), map[string]any{"snapshot": chain.NativeHead, "state": state, "settlement_vault": source.Deployment.SettlementVault, "vault_mirror_coldkey": vaultMirrorSS58, "operator_registry_coldkey": operator.Coldkey})
		if err != nil {
			return err
		}
		poolIdentityLabel := operatorPoolHotkeyLabelForGeneration(int(noID), terminal.Status.Contracts.Deployment.RegistrationRoleGeneration)
		hotkeyIdentity := identities.Substrate[poolIdentityLabel]
		coldkeyIdentity := identities.Substrate[fmt.Sprintf("operator-%d-coldkey", noID)]
		depositIdentity := identities.Substrate[fmt.Sprintf("operator-%d-deposit-hotkey", noID)]
		if hotkeyIdentity.SS58 == "" || coldkeyIdentity.SS58 == "" || depositIdentity.SS58 == "" || !strings.EqualFold(hotkeyIdentity.PublicKey, operator.PoolHotkey) || !strings.EqualFold(coldkeyIdentity.PublicKey, operator.Coldkey) || !strings.EqualFold(depositIdentity.PublicKey, operator.DepositHotkey) {
			return fmt.Errorf("operator %d public identities differ from terminal operator", noID)
		}
		source.Pools = append(source.Pools, FinalPoolUIDEvidence{
			NoID: noID, UID: operator.PoolUID, Hotkey: hotkeyIdentity.SS58, Coldkey: vaultMirrorSS58, OperatorColdkey: coldkeyIdentity.SS58,
			Registered: operator.PoolLive, Registration: registrationReceipt, Snapshot: chain.NativeHead, FinalCarryRao: operator.CarryRao,
			DepositHotkey: depositIdentity.SS58, DepositSigner: strings.ToLower(operator.DepositSigner), PayoutRootSigner: strings.ToLower(operator.RootSigner),
			ConvictionReceipt: convictionReceipt, EffectiveEpoch: operator.EffectiveEpoch, VersionCount: versionCount, Active: operator.Active,
			ServerKeyHistory: keys, OwnershipArtifact: ownershipArtifact,
		})
	}
	sort.Slice(source.Pools, func(i, j int) bool { return source.Pools[i].NoID < source.Pools[j].NoID })
	return nil
}
func (a *finalSemanticArchive) latestAppliedMeasurement() (*validatorpkg.ReleaseMeasurementArtifact, *validatorpkg.VerifiedReleaseMeasurement, error) {
	if len(a.collected.Validators) == 0 {
		return nil, nil, errors.New("closed validator collection is empty")
	}
	for index := len(a.collected.Validators[0].Intents) - 1; index >= 0; index-- {
		item := a.collected.Validators[0].Intents[index]
		if item.Status != "applied" {
			continue
		}
		data, _, err := a.file(item.Measurement.URI)
		if err != nil {
			return nil, nil, err
		}
		artifact, verified, err := validatorpkg.DecodeReleaseMeasurementArtifact(data)
		if err != nil {
			return nil, nil, err
		}
		return artifact, verified, nil
	}
	return nil, nil, errors.New("closed validator collection has no applied measurement")
}

func finalSemanticClientKey(value string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err == nil && len(raw) == 16 {
		return hex.EncodeToString(raw), nil
	}
	id, err := connect.ParseId(value)
	if err != nil {
		return "", fmt.Errorf("client identity %q is neither fixed hex nor canonical UUID", value)
	}
	return hex.EncodeToString(id[:]), nil
}

func finalSemanticFaultAssertionPassed(result *ScenarioResult, faultID string) bool {
	if result == nil || faultID == "" {
		return false
	}
	want := "fault_" + faultID
	found := false
	for _, assertion := range result.Assertions {
		if assertion.ID != want {
			continue
		}
		if found || !assertion.Passed {
			return false
		}
		found = true
	}
	return found
}

func finalSemanticRestartFault(record ScenarioFaultRecord, result *ScenarioResult) (string, FaultProcessEvidence, FaultProcessEvidence, error) {
	if record.Kind != "process-restart" || record.ID == "" || strings.ContainsAny(record.ID, "/\\\r\n\x00") || record.Status != "restored" || record.Error != "" || len(record.Targets) != 1 || len(record.Processes) != 1 || len(record.RestoredProcesses) != 1 {
		return "", FaultProcessEvidence{}, FaultProcessEvidence{}, fmt.Errorf("process-restart fault %q is not an exact restored single-target record", record.ID)
	}
	target := record.Targets[0]
	before, after := record.Processes[0], record.RestoredProcesses[0]
	if target == "" || strings.ContainsAny(target, "/\\\r\n\x00") || before.ID != target || after.ID != target || before.Role == "" || before.Identity == "" || after.Role != before.Role || after.Identity != before.Identity || before.PID <= 1 || after.PID <= 1 || before.PID == after.PID {
		return "", FaultProcessEvidence{}, FaultProcessEvidence{}, fmt.Errorf("process-restart fault %s has an invalid process identity or PID transition", record.ID)
	}
	if record.TriggerBlock == 0 || record.AppliedBlock < record.TriggerBlock || record.RestoredBlock == 0 || record.RestoredBlock < record.AppliedBlock {
		return "", FaultProcessEvidence{}, FaultProcessEvidence{}, fmt.Errorf("process-restart fault %s has an invalid block transition", record.ID)
	}
	timingValid := record.RestoredBlock >= record.RestoreBlock
	if record.RestoreCondition != "" {
		minimumRestore, ok := checkedAdd(record.AppliedBlock, record.MinimumDurationBlocks)
		timingValid = ok && record.MinimumDurationBlocks != 0 && record.RestoreConditionMet && record.RestoreConditionBlock >= minimumRestore && record.RestoreConditionBlock <= record.RestoredBlock && (record.RestoredBlock >= record.RestoreBlock || record.RestoreConditionBlock >= minimumRestore)
	} else if record.MinimumDurationBlocks != 0 {
		timingValid = false
	}
	if !timingValid || requireFinalHex32("process-restart applied block hash", record.AppliedBlockHash) != nil || requireFinalHex32("process-restart restored block hash", record.RestoredBlockHash) != nil || !finalSemanticFaultAssertionPassed(result, record.ID) {
		return "", FaultProcessEvidence{}, FaultProcessEvidence{}, fmt.Errorf("process-restart fault %s does not have canonical timing, heads, and a passing assertion", record.ID)
	}
	return target, before, after, nil
}

func (a *finalSemanticArchive) restartCampaignResults(source *FinalSemanticEvidence, current *ScenarioResult) ([]*ScenarioResult, error) {
	if a == nil || a.collected == nil || source == nil || current == nil || current.Name != source.Phase || current.RunID != source.RunID || current.EvidenceHash != source.ResultHash || current.Result != "pass" || current.FailedAssertionCount != 0 {
		return nil, errors.New("process-restart attribution campaign context is incomplete")
	}
	results := make([]*ScenarioResult, 0, 2)
	switch source.Phase {
	case "release-1.0":
		if a.collected.PriorPhase != nil {
			return nil, errors.New("release process-restart attribution unexpectedly has a prior phase")
		}
	case "production-soak":
		prior := a.collected.PriorPhase
		if prior == nil {
			return nil, errors.New("production process-restart attribution has no authenticated release phase")
		}
		var priorResult ScenarioResult
		if err := a.decode(prior.ScenarioResult.URI, &priorResult); err != nil {
			return nil, err
		}
		if priorResult.Name != "release-1.0" || priorResult.RunID != prior.RunID || priorResult.EvidenceHash != prior.ResultHash || priorResult.Result != "pass" || priorResult.FailedAssertionCount != 0 || priorResult.AcceptanceWindow == nil || *priorResult.AcceptanceWindow != prior.Window {
			return nil, errors.New("authenticated prior release result is invalid for restart attribution")
		}
		results = append(results, &priorResult)
	default:
		return nil, fmt.Errorf("phase %q does not support final process-restart attribution", source.Phase)
	}
	return append(results, current), nil
}

func (a *finalSemanticArchive) addVerifyKeyRotationRestarts(source *FinalSemanticEvidence, expected map[string][]string, seenFaults map[string]bool, terminalByID map[string]ProcessState) error {
	if source.Phase != "production-soak" {
		return nil
	}
	var rotation verifyKeyRotationEvidence
	if err := a.decode("public/verify-key-rotation.json", &rotation); err != nil {
		return fmt.Errorf("decode closed verify-key rotation evidence: %w", err)
	}
	if rotation.Schema != "urnetwork-verify-key-rotation-v1" || rotation.DeploymentID != source.DeploymentID || !strings.EqualFold(rotation.PolicyHash, source.PolicyHash) || len(rotation.Operators) != source.ExpectedOperators {
		return errors.New("closed verify-key rotation evidence differs from the production campaign")
	}
	for index, operator := range rotation.Operators {
		if operator.NoID != index+1 || operator.BeforePID <= 1 || operator.AfterPID <= 1 {
			return fmt.Errorf("verify-key rotation operator %d has an invalid census or PID", operator.NoID)
		}
		oldKey, oldErr := finalEd25519PublicKey("old verify key", operator.OldPublicKey)
		newKey, newErr := finalEd25519PublicKey("new verify key", operator.NewPublicKey)
		if oldErr != nil || newErr != nil || bytes.Equal(oldKey, newKey) {
			return fmt.Errorf("verify-key rotation operator %d has invalid or unchanged keys", operator.NoID)
		}
		processID := fmt.Sprintf("operator-%d-api", operator.NoID)
		process, ok := terminalByID[processID]
		if !ok || process.Role != "operator-api" || process.Identity != fmt.Sprintf("no:%d", operator.NoID) {
			return fmt.Errorf("verify-key rotation operator %d does not bind its terminal API process", operator.NoID)
		}
		if operator.BeforePID == operator.AfterPID {
			continue
		}
		faultID := fmt.Sprintf("verify-key-rotation-%d", operator.NoID)
		if seenFaults[faultID] {
			return fmt.Errorf("process-restart fault identity %s is duplicated", faultID)
		}
		seenFaults[faultID] = true
		expected[processID] = append(expected[processID], faultID)
	}
	return nil
}

// processRestartEvidence attributes the absolute terminal supervisor counters
// only to hash-bound, successfully restored campaign records. Production also
// includes the closed pre-campaign verify-key rotation record, whose restart
// is deliberately outside ScenarioResult.Faults.
func (a *finalSemanticArchive) processRestartEvidence(source *FinalSemanticEvidence, current *ScenarioResult, terminal *ScenarioObservation) ([]FinalProcessRestartEvidence, error) {
	if terminal == nil || terminal.Status == nil || terminal.Status.Supervisor == nil || len(terminal.Status.Supervisor.Processes) == 0 {
		return nil, errors.New("terminal supervisor census is unavailable for process-restart attribution")
	}
	terminalByID := make(map[string]ProcessState, len(terminal.Status.Supervisor.Processes))
	for _, process := range terminal.Status.Supervisor.Processes {
		if process.ID == "" || strings.ContainsAny(process.ID, "/\\\r\n\x00") || process.Role == "" || process.Identity == "" || process.PID <= 1 || process.Restarts < 0 || terminalByID[process.ID].ID != "" {
			return nil, fmt.Errorf("terminal supervisor process %q is invalid or duplicated", process.ID)
		}
		terminalByID[process.ID] = process
	}
	results, err := a.restartCampaignResults(source, current)
	if err != nil {
		return nil, err
	}
	expected := make(map[string][]string, len(terminalByID))
	seenFaults := map[string]bool{}
	for _, result := range results {
		for _, record := range result.Faults {
			if record.Kind != "process-restart" {
				continue
			}
			target, before, after, err := finalSemanticRestartFault(record, result)
			if err != nil {
				return nil, err
			}
			terminalProcess, ok := terminalByID[target]
			if !ok || terminalProcess.Role != before.Role || terminalProcess.Identity != before.Identity || terminalProcess.Role != after.Role || terminalProcess.Identity != after.Identity {
				return nil, fmt.Errorf("process-restart fault %s differs from terminal process identity", record.ID)
			}
			if seenFaults[record.ID] {
				return nil, fmt.Errorf("process-restart fault identity %s is duplicated", record.ID)
			}
			seenFaults[record.ID] = true
			expected[target] = append(expected[target], record.ID)
		}
	}
	if err := a.addVerifyKeyRotationRestarts(source, expected, seenFaults, terminalByID); err != nil {
		return nil, err
	}
	rows := make([]FinalProcessRestartEvidence, 0, len(terminalByID))
	for processID, process := range terminalByID {
		faultIDs := append([]string(nil), expected[processID]...)
		sort.Strings(faultIDs)
		want := uint64(len(faultIDs))
		observed := uint64(process.Restarts)
		if want != observed {
			return nil, fmt.Errorf("terminal process %s restart count %d differs from %d authenticated restored faults", processID, observed, want)
		}
		rows = append(rows, FinalProcessRestartEvidence{ProcessID: processID, ExpectedRestarts: want, ObservedRestarts: observed, FaultIDs: faultIDs})
	}
	if len(seenFaults) == 0 {
		return nil, errors.New("campaign has no authenticated restored process-restart faults")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ProcessID < rows[j].ProcessID })
	return rows, nil
}

func (a *finalSemanticArchive) buildTopology(source *FinalSemanticEvidence, result *ScenarioResult, terminal *ScenarioObservation, identities *finalPublicIdentities, chain *FinalCollectedChainSnapshot) error {
	if source == nil || result == nil || terminal == nil || terminal.Status == nil || terminal.Status.Supervisor == nil || identities == nil || chain == nil {
		return errors.New("topology construction context is incomplete")
	}
	processRestarts, err := a.processRestartEvidence(source, result, terminal)
	if err != nil {
		return err
	}
	measurement, _, err := a.latestAppliedMeasurement()
	if err != nil {
		return err
	}
	bindingByClient := make(map[string]validatorpkg.ReleaseBindingMeasurement, len(measurement.Bindings))
	for _, binding := range measurement.Bindings {
		key, err := finalSemanticClientKey(binding.ClientID)
		if err != nil {
			return err
		}
		if _, exists := bindingByClient[key]; exists {
			return fmt.Errorf("release measurement repeats client binding %q", binding.ClientID)
		}
		bindingByClient[key] = binding
	}
	publicKeyByClient := make(map[string]string, len(identities.Clients))
	for label, identity := range identities.Clients {
		clientID, clientErr := finalSemanticClientKey(identity.ClientID)
		key, keyErr := finalEd25519PublicKey("public client verification key", identity.ClientKey)
		if label == "" || clientErr != nil || keyErr != nil {
			return fmt.Errorf("public client identity %q is invalid", label)
		}
		if _, duplicate := publicKeyByClient[clientID]; duplicate {
			return fmt.Errorf("public client identity %q reuses a client ID", label)
		}
		publicKeyByClient[clientID] = "0x" + hex.EncodeToString(key)
	}
	type capturedFleetManifest struct {
		manifest *protocol.FleetManifest
		data     []byte
	}
	lifecycleVariantForFleet := func(fleetID int) (*FinalFleetLifecycleVariantEvidence, fleetLifecycleVariant, bool, error) {
		if source.FleetLifecycle == nil || fleetID != fleetLifecycleTargetFleet && fleetID != fleetLifecycleCompanionFleet {
			return nil, fleetLifecycleVariant{}, false, nil
		}
		name := fleetLifecycleVariantProvider
		if fleetID == fleetLifecycleCompanionFleet {
			name = fleetLifecycleVariantTerminal
		}
		variant, err := fleetLifecycleVariantFor(name)
		if err != nil {
			return nil, fleetLifecycleVariant{}, false, err
		}
		indexed, err := finalLifecycleVariantByName(source.FleetLifecycle, name)
		if err != nil {
			return nil, fleetLifecycleVariant{}, false, err
		}
		return indexed, variant, true, nil
	}
	// The terminal generations reuse the original fleet-5/6 client sets after
	// their old bindings were cleaned. Reconstruct those exact generation-four
	// records from the authenticated lifecycle index; a measurement from an
	// earlier lifecycle milestone must never backdate terminal ownership.
	for _, fleetID := range []int{fleetLifecycleTargetFleet, fleetLifecycleCompanionFleet} {
		indexed, _, lifecycleFleet, lifecycleErr := lifecycleVariantForFleet(fleetID)
		if lifecycleErr != nil {
			return lifecycleErr
		}
		if !lifecycleFleet {
			continue
		}
		for _, binding := range indexed.Bindings {
			key, keyErr := finalSemanticClientKey(binding.ClientID)
			if keyErr != nil {
				return keyErr
			}
			prior := bindingByClient[key]
			if prior.NoID == 0 {
				return fmt.Errorf("terminal lifecycle client %s has no operator assignment in the signed measurement", binding.ClientID)
			}
			bindingByClient[key] = validatorpkg.ReleaseBindingMeasurement{
				NoID: prior.NoID, ClientID: binding.ClientID, Active: true, FleetID: binding.FleetID, Hotkey: binding.Hotkey,
				ClientKey: binding.ClientKey, LocalClientKey: binding.ClientKey, CommitmentHash: binding.CommitmentHash,
				Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch,
				RecordUID: binding.UID, LiveUIDFound: true, LiveUID: binding.UID,
			}
		}
	}
	manifestByFleet := make(map[int]capturedFleetManifest, source.ExpectedCandidates)
	fleetByClient := make(map[string]int, finalHeadCandidateCount*4)
	for fleetID := 1; fleetID <= source.ExpectedCandidates; fleetID++ {
		var matched []capturedFleetManifest
		paths := []string{fmt.Sprintf("public/fleet-%d.refresh.json", fleetID), fmt.Sprintf("public/fleet-%d.json", fleetID)}
		expectedHotkey := identities.Substrate[fmt.Sprintf("fleet-%d-hotkey", fleetID)].PublicKey
		if indexed, variant, lifecycleFleet, lifecycleErr := lifecycleVariantForFleet(fleetID); lifecycleErr != nil {
			return lifecycleErr
		} else if lifecycleFleet {
			paths = []string{"public/" + variant.ManifestName}
			expectedHotkey = indexed.Hotkey
		}
		for _, path := range paths {
			data, _, fileErr := a.file(path)
			if fileErr != nil {
				continue
			}
			manifest, parseErr := protocol.ParseFleetManifest(data)
			if parseErr != nil || manifest.ChainID != source.ChainID || manifest.Netuid != source.Netuid || len(manifest.Members) != 4 {
				continue
			}
			expectedCoordinator := common.HexToAddress(source.Deployment.CoordinatorProxy)
			if !bytes.Equal(manifest.Coordinator[:], expectedCoordinator.Bytes()) || !strings.EqualFold("0x"+hex.EncodeToString(manifest.Hotkey[:]), expectedHotkey) {
				continue
			}
			fleetHex := "0x" + hex.EncodeToString(manifest.FleetID[:])
			allActive := true
			for _, member := range manifest.Members {
				clientID := hex.EncodeToString(member.ClientID[:])
				memberKey := "0x" + hex.EncodeToString(member.ClientKey[:])
				binding, ok := bindingByClient[clientID]
				if !ok || !binding.Active || !binding.LiveUIDFound || binding.FleetID != fleetHex || binding.Generation != manifest.Generation || !strings.EqualFold(binding.Hotkey, expectedHotkey) || !strings.EqualFold(binding.ClientKey, memberKey) || !strings.EqualFold(binding.LocalClientKey, memberKey) || !strings.EqualFold(publicKeyByClient[clientID], memberKey) {
					allActive = false
					break
				}
			}
			if allActive {
				matched = append(matched, capturedFleetManifest{manifest: manifest, data: data})
			}
		}
		if len(matched) != 1 {
			return fmt.Errorf("fleet-%d has %d terminal manifests matching its four active bindings", fleetID, len(matched))
		}
		manifestByFleet[fleetID] = matched[0]
		for _, member := range matched[0].manifest.Members {
			key := hex.EncodeToString(member.ClientID[:])
			if prior := fleetByClient[key]; prior != 0 {
				return fmt.Errorf("fleets %d and %d reuse one client", prior, fleetID)
			}
			fleetByClient[key] = fleetID
		}
	}
	var supervisorManifest SupervisorFile
	if err := a.decode("launch-foundation/supervisor.json", &supervisorManifest); err != nil {
		return err
	}
	sdkHash := strings.ToLower(supervisorManifest.BinaryHash)
	if strings.HasPrefix(sdkHash, "sha256:") {
		sdkHash = "0x" + strings.TrimPrefix(sdkHash, "sha256:")
	}
	if err := requireFinalHex32("captured simulator binary hash", sdkHash); err != nil {
		return err
	}
	processByID := map[string]ProcessState{}
	for _, process := range terminal.Status.Supervisor.Processes {
		processByID[process.ID] = process
	}
	minerProcess := map[int]string{}
	for swarm := 1; swarm <= finalMinerSwarmProcessCount; swarm++ {
		var config struct {
			Schema  string `json:"schema"`
			Members []struct {
				ID string `json:"id"`
			} `json:"members"`
		}
		path := fmt.Sprintf("miner-topology/runtime/miner-swarm-%d/swarm.json", swarm)
		data, _, fileErr := a.file(path)
		if fileErr != nil {
			if swarm > finalMinerSwarmProcessCount {
				break
			}
			return fileErr
		}
		if err := decodeStrictJSONBytes(data, &config); err != nil {
			return err
		}
		for _, member := range config.Members {
			id, parseErr := strconv.Atoi(strings.TrimPrefix(member.ID, "miner-"))
			if parseErr != nil || id < 1 || id > source.ExpectedMiners || minerProcess[id] != "" {
				return fmt.Errorf("captured miner swarm %d has invalid member %q", swarm, member.ID)
			}
			minerProcess[id] = fmt.Sprintf("miner-swarm-%d", swarm)
		}
	}
	miners := make([]FinalMinerProcessEvidence, 0, source.ExpectedMiners)
	bindings := make([]FinalFleetMemberBindingEvidence, 0, source.ExpectedMiners)
	for minerID := 1; minerID <= source.ExpectedMiners; minerID++ {
		var config struct {
			MinerID      int    `yaml:"miner_id"`
			ClientID     string `yaml:"client_id"`
			OperatorNoID int    `yaml:"operator_no_id"`
		}
		data, _, err := a.file(fmt.Sprintf("miner-topology/runtime/miner-%d/miner.yml", minerID))
		if err != nil {
			return err
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(false)
		if err := decoder.Decode(&config); err != nil || config.MinerID != minerID || config.ClientID == "" || config.OperatorNoID < 1 {
			return fmt.Errorf("captured miner-%d config identity is invalid", minerID)
		}
		publicClient, ok := identities.Clients[fmt.Sprintf("miner-%d", minerID)]
		provider := identities.Substrate[fmt.Sprintf("miner-%d-payout", minerID)]
		processID := minerProcess[minerID]
		process, processOK := processByID[processID]
		configClientKey, keyErr := finalSemanticClientKey(config.ClientID)
		publicClientKey, publicKeyErr := finalSemanticClientKey(publicClient.ClientID)
		clientVPK, clientVPKErr := finalEd25519PublicKey("miner client verification key", publicClient.ClientKey)
		if !ok || keyErr != nil || publicKeyErr != nil || publicClientKey != configClientKey || clientVPKErr != nil || len(clientVPK) != ed25519.PublicKeySize || provider.SS58 == "" || !processOK || process.Role != "miner-swarm" || !process.Healthy || process.PID <= 1 || process.ExitError != "" {
			return fmt.Errorf("miner-%d public/process identity is incomplete", minerID)
		}
		miners = append(miners, FinalMinerProcessEvidence{MinerID: uint64(minerID), ProcessID: processID, ProcessGeneration: terminal.Status.Supervisor.SupervisorStartTimeTicks, ClientID: config.ClientID, ProviderID: provider.SS58, SDKSourceHash: sdkHash, Running: true})
		binding, found := bindingByClient[configClientKey]
		row := FinalFleetMemberBindingEvidence{MinerID: uint64(minerID), NoID: uint64(config.OperatorNoID), ClientID: config.ClientID, ProviderID: provider.SS58, Tier: "pool-tail"}
		if found && binding.Active {
			if binding.NoID != uint64(config.OperatorNoID) {
				return fmt.Errorf("miner-%d active binding operator differs from its runtime config", minerID)
			}
			row.Tier, row.HeadUID, row.Generation, row.BindingActive = "head-candidate", binding.LiveUID, binding.Generation, binding.LiveUIDFound
			row.FleetID = uint64(fleetByClient[configClientKey])
			if row.FleetID == 0 {
				return fmt.Errorf("miner-%d active binding does not map to a captured fleet manifest", minerID)
			}
		}
		bindings = append(bindings, row)
	}
	minerLocator, err := a.derived("miner-process-manifest", "miner-process-manifest.json", miners)
	if err != nil {
		return err
	}
	bindingLocator, err := a.derived("fleet-binding-manifest", "fleet-binding-manifest.json", bindings)
	if err != nil {
		return err
	}
	source.Topology = FinalTopologyEvidence{
		MinerSDKInstances: source.ExpectedMiners, MinerSwarmProcesses: finalMinerSwarmProcessCount, HeadCandidateFleets: source.ExpectedCandidates,
		HeadSlots: source.ExpectedHeadSlots, ValidatorProcesses: source.ExpectedValidators, OperatorPools: source.ExpectedOperators,
		MinerManifestHash: minerLocator.ContentHash, MinerManifest: minerLocator, BindingManifestHash: bindingLocator.ContentHash, BindingManifest: bindingLocator,
		ProcessRestarts: processRestarts,
	}
	for fleetID := 1; fleetID <= source.ExpectedCandidates; fleetID++ {
		captured, ok := manifestByFleet[fleetID]
		if !ok || captured.manifest == nil || len(captured.data) == 0 {
			return fmt.Errorf("captured fleet-%d manifest is invalid", fleetID)
		}
		manifest, manifestData := captured.manifest, captured.data
		uid := uint16(0)
		foundUID := false
		for _, binding := range bindings {
			if binding.FleetID == uint64(fleetID) {
				if foundUID && uid != binding.HeadUID {
					return fmt.Errorf("fleet-%d members disagree on UID", fleetID)
				}
				uid, foundUID = binding.HeadUID, true
			}
		}
		if !foundUID {
			return fmt.Errorf("fleet-%d has no active terminal member binding", fleetID)
		}
		var state FinalCollectedNativeUIDState
		foundState := false
		for _, candidate := range chain.NativeUIDs {
			if candidate.UID == uid {
				state, foundState = candidate, true
				break
			}
		}
		if !foundState {
			return fmt.Errorf("fleet-%d UID %d is absent from terminal native census", fleetID, uid)
		}
		hotkey := identities.Substrate[fmt.Sprintf("fleet-%d-hotkey", fleetID)]
		coldkey := identities.Substrate[fmt.Sprintf("fleet-%d-coldkey", fleetID)]
		registrationActionID := fmt.Sprintf("fleet.register.%d", fleetID)
		if indexed, variant, lifecycleFleet, lifecycleErr := lifecycleVariantForFleet(fleetID); lifecycleErr != nil {
			return lifecycleErr
		} else if lifecycleFleet {
			hotkeyRole, hotkeyErr := finalFleetLifecycleRole(source.FleetLifecycle, variant.HotkeyLabel)
			coldkeyRole, coldkeyErr := finalFleetLifecycleRole(source.FleetLifecycle, strings.Replace(variant.HotkeyLabel, "-hotkey", "-coldkey", 1))
			if hotkeyErr != nil || coldkeyErr != nil || !strings.EqualFold(indexed.Hotkey, hotkeyRole.PublicKey) {
				return stateMismatchError(errors.Join(hotkeyErr, coldkeyErr), "fleet-%d terminal lifecycle roles are invalid", fleetID)
			}
			hotkey = finalPublicIdentity{PublicKey: hotkeyRole.PublicKey, SS58: hotkeyRole.SS58}
			coldkey = finalPublicIdentity{PublicKey: coldkeyRole.PublicKey, SS58: coldkeyRole.SS58}
			registrationActionID, _ = fleetLifecycleRegistrationActionIDFor(indexed.Name)
		}
		if hotkey.SS58 == "" || coldkey.SS58 == "" || !strings.EqualFold(hotkey.PublicKey, state.HotkeyPublicKey) || !strings.EqualFold(coldkey.PublicKey, state.ColdkeyPublicKey) {
			return fmt.Errorf("fleet-%d native terminal ownership differs", fleetID)
		}
		registration, postcondition, err := a.nativeActionReceipt(registrationActionID, fmt.Sprintf("fleet-%d-registration", fleetID))
		if err != nil {
			return err
		}
		bindingArtifact, err := a.derived("head-fleet-binding", fmt.Sprintf("fleet-%d-binding.json", fleetID), map[string]any{"manifest": json.RawMessage(manifestData), "uid": uid, "snapshot": chain.NativeHead})
		if err != nil {
			return err
		}
		source.HeadFleets = append(source.HeadFleets, FinalHeadFleetEvidence{FleetID: uint64(fleetID), UID: uid, Hotkey: hotkey.SS58, Coldkey: coldkey.SS58, Generation: manifest.Generation, MemberCount: len(manifest.Members), Registered: true, Registration: registration, Snapshot: chain.NativeHead, BindingArtifact: bindingArtifact})
		if fleetID > source.ExpectedHeadSlots {
			replacedUID, uidOK := finalSemanticObservedUint(postcondition.Observed, "replaced_uid")
			replacedChurn, churnOK := finalSemanticObservedUint(postcondition.Observed, "replaced_churn")
			pruned := identities.Substrate[fmt.Sprintf("churn-%d-hotkey", replacedChurn)]
			if !uidOK || !churnOK || uint16(replacedUID) != uid || pruned.SS58 == "" {
				return fmt.Errorf("challenger fleet-%d replacement evidence is incomplete", fleetID)
			}
			artifactValue := finalHeadTournamentTransitionArtifact{
				Postcondition: postcondition,
				Pruned: finalHeadTournamentIdentity{
					Role: fmt.Sprintf("churn-%d-hotkey", replacedChurn), PublicKey: strings.ToLower(pruned.PublicKey), SS58: pruned.SS58,
				},
			}
			artifact, err := a.derived("head-tournament-transition", fmt.Sprintf("fleet-%d-transition.json", fleetID), artifactValue)
			if err != nil {
				return err
			}
			source.HeadTransitions = append(source.HeadTransitions, FinalHeadTournamentTransition{
				ChallengerFleetID: uint64(fleetID), PromotedUID: uid, PromotedHotkey: hotkey.SS58,
				PrunedUID: uint16(replacedUID), PrunedChurn: replacedChurn, PrunedHotkey: pruned.SS58,
				OperationalRPCMode: postcondition.OperationalRPCMode, IndependentRPC: postcondition.IndependentRPC,
				Registration: registration, Snapshot: postcondition.SubstrateFinalized,
				IndependentSnapshot: postcondition.IndependentSubstrateFinalized, EVMSnapshot: postcondition.EVMFinalized,
				IndependentEVMSnapshot: postcondition.IndependentEVMFinalized, Artifact: artifact,
			})
		}
	}
	return nil
}

func finalSemanticNativeHead(chain *FinalCollectedChainSnapshot, number uint64, expectedHash string) (ChainHead, error) {
	if chain == nil || number == 0 {
		return ChainHead{}, errors.New("native checkpoint is unavailable")
	}
	for _, head := range chain.NativeHeads {
		if head.Number != number {
			continue
		}
		if expectedHash != "" && !strings.EqualFold(head.Hash, expectedHash) {
			return ChainHead{}, fmt.Errorf("native checkpoint %d hash differs from signed intent", number)
		}
		return ChainHead{Number: number, Hash: strings.ToLower(head.Hash)}, nil
	}
	if chain.NativeHead.Number == number {
		if expectedHash != "" && !strings.EqualFold(chain.NativeHead.Hash, expectedHash) {
			return ChainHead{}, fmt.Errorf("terminal native checkpoint %d hash differs from signed intent", number)
		}
		return ChainHead{Number: number, Hash: strings.ToLower(chain.NativeHead.Hash)}, nil
	}
	// Commit/reveal/application hashes are owner-authenticated inside the
	// captured steering intent and measurement envelope. They need not happen
	// to coincide with a polling snapshot: construct the checkpoint only from
	// those closed bytes, then require the public archive replay to prove the
	// exact number/hash canonical. This is essential for bounded lifecycle tails
	// without reaching back into mutable RPC during source construction.
	if number <= chain.NativeHead.Number && requireFinalHex32("signed native checkpoint", strings.ToLower(expectedHash)) == nil {
		return ChainHead{Number: number, Hash: strings.ToLower(expectedHash)}, nil
	}
	return ChainHead{}, fmt.Errorf("closed chain snapshot has no native checkpoint %d", number)
}

func finalSemanticPayout(collected *FinalSemanticCollectedInputs, epoch, noID uint64) (FinalArtifactLocator, bool) {
	if collected == nil {
		return FinalArtifactLocator{}, false
	}
	for _, payout := range collected.Payouts {
		if payout.Epoch == epoch && payout.NoID == noID {
			return payout.Artifact, true
		}
	}
	for _, payout := range collected.LifecyclePayouts {
		if payout.Epoch == epoch && payout.NoID == noID {
			return payout.Artifact, true
		}
	}
	return FinalArtifactLocator{}, false
}

func finalSemanticValueByUID(intent *validatorpkg.SteeringIntent) (map[uint16]uint16, error) {
	if intent == nil || len(intent.UIDs) != len(intent.Values) || len(intent.UIDs) != len(intent.Scores) {
		return nil, errors.New("signed intent weight vectors have different lengths")
	}
	values := make(map[uint16]uint16, len(intent.UIDs))
	for index, uid := range intent.UIDs {
		if _, exists := values[uid]; exists {
			return nil, fmt.Errorf("signed intent repeats UID %d", uid)
		}
		values[uid] = intent.Values[index]
	}
	return values, nil
}

func (a *finalSemanticArchive) nativeIntentReceipt(intent *validatorpkg.SteeringIntent, kind string, chain *FinalCollectedChainSnapshot, artifact FinalArtifactLocator) (FinalNativeReceipt, error) {
	if intent == nil {
		return FinalNativeReceipt{}, errors.New("signed intent is nil")
	}
	var blockNumber uint64
	var expectedHash, extrinsicHash string
	switch kind {
	case "commit":
		blockNumber, expectedHash, extrinsicHash = intent.FinalizedBlock, intent.FinalizedBlockHash, intent.ExtrinsicHash
	case "reveal":
		blockNumber = intent.RevealBlock
	case "application":
		blockNumber, expectedHash = intent.ApplicationBlock, intent.ApplicationBlockHash
	default:
		return FinalNativeReceipt{}, fmt.Errorf("unsupported intent receipt kind %q", kind)
	}
	head, err := finalSemanticNativeHead(chain, blockNumber, expectedHash)
	if err != nil {
		return FinalNativeReceipt{}, err
	}
	proof, err := a.derived("native-receipt", filepath.ToSlash(filepath.Join("native-receipts", fmt.Sprintf("validator-%d-epoch-%d-%s.json", intent.ValidatorID, intent.SettlementEpoch, kind))), map[string]any{
		"kind": kind, "intent": artifact, "block": head, "extrinsic_hash": strings.ToLower(extrinsicHash),
	})
	if err != nil {
		return FinalNativeReceipt{}, err
	}
	return FinalNativeReceipt{ExtrinsicHash: strings.ToLower(extrinsicHash), Block: head, Proof: proof}, nil
}

// depositReceipt proves the particular prefix of same-epoch deposits observed
// by the signed validator audit. This matters for the production underpayment:
// the first receipt proves the dishonest prefix and a later receipt proves the
// recovery prefix without consulting mutable coordinator state.
func (a *finalSemanticArchive) depositReceipt(events *finalSemanticEventIndex, audit validatorpkg.DepositAudit, name string) (FinalEVMReceipt, error) {
	if events == nil {
		return FinalEVMReceipt{}, errors.New("captured EVM event index is unavailable")
	}
	want, ok := new(big.Int).SetString(audit.ObservedDepositRao, 10)
	if !ok || want.Sign() <= 0 {
		return FinalEVMReceipt{}, fmt.Errorf("operator %d epoch %d observed deposit is not positive", audit.NoID, audit.Epoch)
	}
	candidates := make([]finalSemanticEvent, 0)
	for _, event := range events.byName["Deposit"] {
		noID, noOK := finalSemanticUint(event.Args, "noId")
		epoch, epochOK := finalSemanticUint(event.Args, "epoch")
		if noOK && epochOK && noID == audit.NoID && epoch == audit.Epoch && event.Log.BlockNumber <= audit.ObservedAtBlock {
			candidates = append(candidates, event)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Log.BlockNumber != candidates[j].Log.BlockNumber {
			return candidates[i].Log.BlockNumber < candidates[j].Log.BlockNumber
		}
		if candidates[i].Log.TransactionIndex != candidates[j].Log.TransactionIndex {
			return candidates[i].Log.TransactionIndex < candidates[j].Log.TransactionIndex
		}
		return candidates[i].Log.LogIndex < candidates[j].Log.LogIndex
	})
	running := new(big.Int)
	for _, event := range candidates {
		amount, amountOK := finalSemanticInteger(event.Args, "amount")
		if !amountOK {
			return FinalEVMReceipt{}, errors.New("captured Deposit event amount is invalid")
		}
		running.Add(running, amount)
		if running.Cmp(want) == 0 {
			return a.receiptFromIndex(events, event, name)
		}
		if running.Cmp(want) > 0 {
			break
		}
	}
	return FinalEVMReceipt{}, fmt.Errorf("operator %d epoch %d captured Deposit prefix does not equal signed observed amount %s", audit.NoID, audit.Epoch, audit.ObservedDepositRao)
}

func (a *finalSemanticArchive) buildPoolWeight(cycle *FinalCRv4Cycle, epochDepositCap uint64, pool validatorpkg.VerifiedReleasePool, intent *validatorpkg.SteeringIntent, events *finalSemanticEventIndex) (FinalPoolWeightEvidence, error) {
	if cycle == nil || intent == nil || epochDepositCap == 0 {
		return FinalPoolWeightEvidence{}, errors.New("pool weight construction context is incomplete")
	}
	audit := pool.Audit
	payoutLocator, ok := finalSemanticPayout(a.collected, audit.SourceEpoch, audit.NoID)
	if !ok {
		return FinalPoolWeightEvidence{}, fmt.Errorf("operator %d source epoch %d payout artifact is absent", audit.NoID, audit.SourceEpoch)
	}
	payoutData, _, err := a.file(payoutLocator.URI)
	if err != nil {
		return FinalPoolWeightEvidence{}, err
	}
	payout, err := payoutartifact.Decode(payoutData)
	if err != nil {
		return FinalPoolWeightEvidence{}, fmt.Errorf("decode operator %d source epoch %d payout: %w", audit.NoID, audit.SourceEpoch, err)
	}
	if payout.NoID != audit.NoID || payout.Epoch != audit.SourceEpoch || payout.ContentHash != audit.ArtifactHash || payout.TotalUsageBytes != audit.UsageBytes {
		return FinalPoolWeightEvidence{}, fmt.Errorf("operator %d source epoch %d payout differs from signed deposit audit", audit.NoID, audit.SourceEpoch)
	}
	valueByUID, err := finalSemanticValueByUID(intent)
	if err != nil {
		return FinalPoolWeightEvidence{}, err
	}
	zero := FinalRational{Numerator: "0", Denominator: "1"}
	quality, implied, raw := zero, zero, zero
	qualityPPM := pool.QualityPPM
	if pool.Eligible {
		clamped := qualityPPM
		if clamped < cycle.QualityMinimumPPM {
			clamped = cycle.QualityMinimumPPM
		}
		if clamped > cycle.QualityMaximumPPM {
			clamped = cycle.QualityMaximumPPM
		}
		quality = finalRationalFromBig(new(big.Rat).SetFrac(new(big.Int).SetUint64(uint64(clamped)), big.NewInt(1_000_000)))
		observed, observedOK := new(big.Int).SetString(audit.ObservedDepositRao, 10)
		if !observedOK {
			return FinalPoolWeightEvidence{}, errors.New("signed deposit audit amount is invalid")
		}
		implied = finalRationalFromBig(new(big.Rat).SetFrac(new(big.Int).Mul(observed, new(big.Int).SetUint64(audit.RateDenominator)), new(big.Int).SetUint64(audit.RateNumeratorRaoPerGiB)))
		raw = finalRationalFromBig(pool.Score)
	}
	receipt, err := a.depositReceipt(events, audit, fmt.Sprintf("validator-%d-epoch-%d-pool-%d-deposit", intent.ValidatorID, intent.SettlementEpoch, audit.NoID))
	if err != nil {
		return FinalPoolWeightEvidence{}, err
	}
	return FinalPoolWeightEvidence{
		NoID: audit.NoID, UID: pool.UID, SourceEpoch: audit.SourceEpoch, UsageBytes: audit.UsageBytes,
		ConvictionBeforeRao: audit.ConvictionBeforeRao, RateNumeratorRaoPerGiB: audit.RateNumeratorRaoPerGiB, RateDenominator: audit.RateDenominator,
		EpochDepositCapRao: strconv.FormatUint(epochDepositCap, 10), RequiredDepositRao: audit.RequiredDepositRao, ObservedDepositRao: audit.ObservedDepositRao,
		QualityPPM: qualityPPM, QualityFactor: quality, ImpliedUsageGiB: implied, RawScore: raw, Formula: finalDepositFormula,
		AuditStatus: audit.Status, AuditCompliant: audit.Compliant, AuditDisposition: audit.Disposition, AuditError: audit.Error,
		ArtifactContentHash: audit.ArtifactHash, ArtifactHash: strings.ToLower(audit.CommittedArtifactHash), PayoutRoot: strings.ToLower(audit.PayoutRoot),
		ArtifactSigner: strings.ToLower(audit.ArtifactSigner), RootCommitter: strings.ToLower(audit.RootCommitter), RootSigner: strings.ToLower(audit.RootSigner),
		SourceStartBlock: audit.SourceStartBlock, SourceStartHash: strings.ToLower(audit.SourceStartHash), SourceEndBlock: audit.SourceEndBlock, SourceEndHash: strings.ToLower(audit.SourceEndHash),
		RootCommitBlock: audit.RootCommitBlock, ObservedAtBlock: audit.ObservedAtBlock, ArtifactDeadlineBlock: audit.ArtifactDeadlineBlock,
		PayoutArtifact: payoutLocator, DepositReceipt: receipt, AppliedWeight: valueByUID[pool.UID],
	}, nil
}

func (a *finalSemanticArchive) buildValidatorCycle(source *FinalSemanticEvidence, collected FinalCollectedValidatorIntent, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) (FinalCRv4Cycle, *validatorpkg.SteeringIntent, error) {
	intentData, _, err := a.file(collected.Artifact.URI)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	var intent validatorpkg.SteeringIntent
	if err := decodeStrictJSONBytes(intentData, &intent); err != nil {
		return FinalCRv4Cycle{}, nil, fmt.Errorf("decode collected validator intent: %w", err)
	}
	if collected.Status != "applied" || intent.Status != "applied" || intent.SettlementEpoch != collected.SettlementEpoch || intent.SubnetEpoch != collected.SubnetEpoch || intent.VectorHash != collected.VectorHash {
		return FinalCRv4Cycle{}, nil, errors.New("collected applied intent summary differs from its bytes")
	}
	measurementData, _, err := a.file(collected.Measurement.URI)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	measurement, verified, err := validatorpkg.DecodeReleaseMeasurementArtifact(measurementData)
	if err != nil {
		return FinalCRv4Cycle{}, nil, fmt.Errorf("decode collected release measurement: %w", err)
	}
	if err := validatorpkg.VerifyReleaseMeasurementIntent(&intent, measurement, verified); err != nil {
		return FinalCRv4Cycle{}, nil, fmt.Errorf("verify collected release measurement against intent: %w", err)
	}
	cycle := FinalCRv4Cycle{
		SettlementEpoch: intent.SettlementEpoch, SubnetEpoch: intent.SubnetEpoch,
		NativeSnapshot:    ChainHead{Number: intent.NativeSnapshotBlock, Hash: strings.ToLower(intent.NativeSnapshotHash)},
		EVMSnapshot:       ChainHead{Number: intent.EVMSnapshotBlock, Hash: strings.ToLower(intent.EVMSnapshotHash)},
		Theta:             FinalRational{Numerator: strconv.FormatUint(measurement.Policy.Steering.Theta.Numerator, 10), Denominator: strconv.FormatUint(measurement.Policy.Steering.Theta.Denominator, 10)},
		QualityMinimumPPM: measurement.Policy.Steering.QualityTransform.MinimumPPM, QualityMaximumPPM: measurement.Policy.Steering.QualityTransform.MaximumPPM,
		MaximumHeadFleets: measurement.Policy.Steering.MaximumHeadFleets, MaxWeightLimitU16: measurement.Policy.Steering.MaxWeightLimitU16,
		Formula: finalWeightFormula, MaskedUIDs: append([]uint16(nil), verified.MaskedUIDs...), IntentVectorHash: intent.VectorHash,
		IntentArtifact: collected.Artifact, MeasurementArtifact: collected.Measurement, MeasurementEnvelope: collected.Envelope,
	}
	if cycle.NativeSnapshot.Number == 0 || cycle.NativeSnapshot.Number > chain.NativeHead.Number {
		return FinalCRv4Cycle{}, nil, errors.New("signed native snapshot is outside the closed native terminal")
	}
	if err := requireFinalHex32("signed native snapshot", cycle.NativeSnapshot.Hash); err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	// If the snapshot block is also one of the receipt checkpoints captured
	// from chain, the hashes must agree. Otherwise the signed measurement
	// envelope supplies the immutable hash and public replay verifies it.
	for _, head := range chain.NativeHeads {
		if head.Number == cycle.NativeSnapshot.Number && !strings.EqualFold(head.Hash, cycle.NativeSnapshot.Hash) {
			return FinalCRv4Cycle{}, nil, errors.New("signed native snapshot conflicts with the captured native checkpoint")
		}
	}
	fleetByUID, err := finalSemanticFleetByUIDAt(source, intent.SettlementEpoch)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	selected := make(map[uint16]bool, len(verified.SelectedHead))
	for _, head := range verified.SelectedHead {
		selected[head.UID] = true
	}
	valueByUID, err := finalSemanticValueByUID(&intent)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	for rank, head := range verified.EligibleHead {
		fleetID := fleetByUID[head.UID]
		if fleetID == 0 {
			return FinalCRv4Cycle{}, nil, fmt.Errorf("validator %d epoch %d candidate UID %d has no fleet identity", intent.ValidatorID, intent.SettlementEpoch, head.UID)
		}
		cycle.Candidates = append(cycle.Candidates, FinalHeadCandidateEvidence{FleetID: fleetID, Rank: uint16(rank + 1), UID: head.UID, RawScore: finalRationalFromBig(head.Score), Selected: selected[head.UID], AppliedWeight: valueByUID[head.UID]})
	}
	for _, pool := range verified.Pools {
		row, err := a.buildPoolWeight(&cycle, measurement.Policy.Deposit.EpochCapRaoPerOperator, pool, &intent, events)
		if err != nil {
			return FinalCRv4Cycle{}, nil, err
		}
		cycle.Pools = append(cycle.Pools, row)
	}
	sort.Slice(cycle.Pools, func(i, j int) bool { return cycle.Pools[i].NoID < cycle.Pools[j].NoID })
	for index, uid := range intent.UIDs {
		cycle.Submitted = append(cycle.Submitted, FinalSubmittedWeight{UID: uid, Score: finalRationalFromBig(verified.Scores[index]), Value: intent.Values[index]})
	}
	for _, candidate := range cycle.Candidates {
		if candidate.Selected {
			cycle.RealizedHeadValue += uint64(candidate.AppliedWeight)
		}
	}
	for _, pool := range cycle.Pools {
		cycle.RealizedPoolValue += uint64(pool.AppliedWeight)
	}
	for _, submitted := range cycle.Submitted {
		cycle.RealizedTotalValue += uint64(submitted.Value)
	}
	encodedValues, err := json.Marshal(intent.Values)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	cycle.ValuesHash = bytesSHA256(encodedValues)
	cycle.Commit, err = a.nativeIntentReceipt(&intent, "commit", chain, collected.Artifact)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	cycle.Reveal, err = a.nativeIntentReceipt(&intent, "reveal", chain, collected.Artifact)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	cycle.Application, err = a.nativeIntentReceipt(&intent, "application", chain, collected.Artifact)
	if err != nil {
		return FinalCRv4Cycle{}, nil, err
	}
	return cycle, &intent, nil
}

// finalSemanticFleetByUIDAt resolves the logical candidate identity at the
// signed intent's coordinator settlement epoch. Terminal ownership is not a
// historical index: lifecycle UIDs 7, 8, and 9 are deliberately reused by
// different hotkeys over the campaign.
func finalSemanticFleetByUIDAt(source *FinalSemanticEvidence, settlementEpoch uint64) (map[uint16]uint64, error) {
	if source == nil || settlementEpoch == 0 {
		return nil, errors.New("validator candidate settlement identity is unavailable")
	}
	result := make(map[uint16]uint64, len(source.HeadFleets))
	for _, fleet := range source.HeadFleets {
		if fleet.FleetID == 0 || !fleet.Registered {
			return nil, errors.New("validator candidate fleet identity is incomplete")
		}
		uid, err := finalSemanticRewardUIDAt(source, fleet.FleetID, settlementEpoch, fleet.UID)
		if err != nil {
			return nil, fmt.Errorf("fleet %d at settlement epoch %d: %w", fleet.FleetID, settlementEpoch, err)
		}
		if prior, duplicate := result[uid]; duplicate {
			return nil, fmt.Errorf("settlement epoch %d ambiguously maps UID %d to fleets %d and %d", settlementEpoch, uid, prior, fleet.FleetID)
		}
		result[uid] = fleet.FleetID
	}
	return result, nil
}

func finalSemanticSelectedFleets(cycle FinalCRv4Cycle) map[uint64]bool {
	result := make(map[uint64]bool, finalHeadSlotCount)
	for _, candidate := range cycle.Candidates {
		if candidate.Selected {
			result[candidate.FleetID] = true
		}
	}
	return result
}

func finalSemanticSetDifference(left, right map[uint64]bool) []uint64 {
	result := make([]uint64, 0)
	for value := range left {
		if !right[value] {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (a *finalSemanticArchive) buildValidatorView(source *FinalSemanticEvidence) error {
	derived, err := deriveFinalValidatorViewTransition(source)
	if err != nil {
		return err
	}
	artifact, err := a.derived("validator-view-transition", "validator-view-transition.json", derived)
	if err != nil {
		return err
	}
	source.ValidatorView = FinalValidatorViewTransition{
		FaultEpoch: derived.FaultEpoch, RestoredEpoch: derived.RestoredEpoch,
		AffectedValidatorID: derived.AffectedValidatorID, ControlValidatorID: derived.ControlValidatorID,
		WithheldFleetID: derived.WithheldFleetID, ReplacementFleetID: derived.ReplacementFleetID, Artifact: artifact,
	}
	return nil
}

func (a *finalSemanticArchive) buildValidators(source *FinalSemanticEvidence, identities *finalPublicIdentities, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) error {
	if source == nil || identities == nil || chain == nil || events == nil {
		return errors.New("validator construction context is incomplete")
	}
	nativeByUID := make(map[uint16]FinalCollectedNativeUIDState, len(chain.NativeUIDs))
	for _, state := range chain.NativeUIDs {
		nativeByUID[state.UID] = state
	}
	for _, collected := range a.collected.Validators {
		if collected.ValidatorID == 0 {
			return errors.New("collected validator identity is zero")
		}
		var cycles []FinalCRv4Cycle
		var selfUID uint16
		haveSelfUID := false
		for _, item := range collected.Intents {
			if item.Status != "applied" || item.SettlementEpoch < source.Window.FirstEpoch || item.SettlementEpoch >= source.Window.FirstEpoch+source.Window.EpochCount {
				continue
			}
			cycle, intent, err := a.buildValidatorCycle(source, item, chain, events)
			if err != nil {
				return fmt.Errorf("validator %d epoch %d: %w", collected.ValidatorID, item.SettlementEpoch, err)
			}
			if haveSelfUID && selfUID != intent.SelfUID {
				return fmt.Errorf("validator %d self UID changed inside acceptance", collected.ValidatorID)
			}
			selfUID, haveSelfUID = intent.SelfUID, true
			cycles = append(cycles, cycle)
		}
		sort.Slice(cycles, func(i, j int) bool { return cycles[i].SettlementEpoch < cycles[j].SettlementEpoch })
		state, ok := nativeByUID[selfUID]
		if !haveSelfUID || !ok || state.StakeRao == "" || !state.ValidatorPermit || state.ValidatorTrustU16 == 0 {
			return fmt.Errorf("validator %d terminal native identity is incomplete", collected.ValidatorID)
		}
		hotkey := identities.Substrate[validatorHotkeyLabel(int(collected.ValidatorID))]
		coldkey := identities.Substrate[fmt.Sprintf("validator-%d-coldkey", collected.ValidatorID)]
		if hotkey.SS58 == "" || coldkey.SS58 == "" || !strings.EqualFold(hotkey.PublicKey, state.HotkeyPublicKey) || !strings.EqualFold(coldkey.PublicKey, state.ColdkeyPublicKey) {
			return fmt.Errorf("validator %d public identity differs from terminal native state", collected.ValidatorID)
		}
		registration, _, err := a.nativeActionReceipt(fmt.Sprintf("validator.register.%d", collected.ValidatorID), fmt.Sprintf("validator-%d-registration", collected.ValidatorID))
		if err != nil {
			return err
		}
		snapshot, err := a.derived("native-validator-state", fmt.Sprintf("validator-%d-native-state.json", collected.ValidatorID), map[string]any{"snapshot": chain.NativeHead, "state": state})
		if err != nil {
			return err
		}
		source.Validators = append(source.Validators, FinalValidatorIdentityEvidence{
			ValidatorID: collected.ValidatorID, UID: selfUID, Hotkey: hotkey.SS58, Coldkey: coldkey.SS58, Registered: true,
			Registration: registration, StakeRao: state.StakeRao, ValidatorPermit: state.ValidatorPermit, ValidatorTrustU16: state.ValidatorTrustU16,
			PathVPK: strings.ToLower(collected.PathVPK), Snapshot: chain.NativeHead, SnapshotArtifact: snapshot, Cycles: cycles,
		})
	}
	sort.Slice(source.Validators, func(i, j int) bool { return source.Validators[i].ValidatorID < source.Validators[j].ValidatorID })
	return a.buildValidatorView(source)
}

func (a *finalSemanticArchive) buildDishonestDeposit(source *FinalSemanticEvidence, terminal *ScenarioObservation, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) error {
	if source == nil || terminal == nil || chain == nil || events == nil {
		return errors.New("dishonest-deposit semantic construction context is incomplete")
	}
	if source.Phase == "release-1.0" {
		if terminal.DishonestDeposit != nil || terminal.DishonestDepositValid {
			return errors.New("release terminal unexpectedly contains dishonest-deposit evidence")
		}
		return nil
	}
	if source.Phase != "production-soak" || terminal.DishonestDeposit == nil || !terminal.DishonestDepositValid {
		return errors.New("production terminal lacks valid dishonest-deposit evidence")
	}
	receipts, err := a.dishonestDepositReceipts(source, terminal, events)
	if err != nil {
		return err
	}
	transaction := terminal.DishonestDeposit.Transaction
	var underpayment, recovery FinalEVMReceipt
	for _, receipt := range receipts {
		if strings.EqualFold(receipt.TransactionHash, transaction.TransactionHash) {
			underpayment = receipt
		} else {
			recovery = receipt
		}
	}
	if underpayment.TransactionHash == "" || recovery.TransactionHash == "" {
		return errors.New("dishonest-deposit receipt identities are incomplete")
	}
	validatorByID := make(map[uint64]FinalValidatorIdentityEvidence, len(source.Validators))
	for _, validator := range source.Validators {
		validatorByID[validator.ValidatorID] = validator
	}
	result := &FinalDishonestDepositEvidence{NoID: transaction.NoID, ObservedDepositRao: transaction.AmountRao, UnderpaymentReceipt: underpayment, RecoveryDepositReceipt: recovery}
	for _, collected := range a.collected.Validators {
		validator, ok := validatorByID[collected.ValidatorID]
		if !ok || collected.DishonestDepositIntent == nil {
			return fmt.Errorf("validator %d dishonest-deposit intent is absent", collected.ValidatorID)
		}
		cycle, intent, err := a.buildValidatorCycle(source, *collected.DishonestDepositIntent, chain, events)
		if err != nil {
			return fmt.Errorf("validator %d dishonest-deposit penalty cycle: %w", collected.ValidatorID, err)
		}
		if intent.ValidatorID != collected.ValidatorID || intent.SelfUID != validator.UID || cycle.SettlementEpoch != transaction.Epoch || cycle.SettlementEpoch+1 != source.Window.FirstEpoch {
			return fmt.Errorf("validator %d dishonest-deposit intent identity differs", collected.ValidatorID)
		}
		var recorded *DishonestDepositValidatorEvidence
		for index := range terminal.DishonestDeposit.Validators {
			candidate := &terminal.DishonestDeposit.Validators[index]
			if candidate.ValidatorID == int(collected.ValidatorID) {
				recorded = candidate
			}
		}
		var penaltyPool *FinalPoolWeightEvidence
		for index := range cycle.Pools {
			if cycle.Pools[index].NoID == transaction.NoID {
				penaltyPool = &cycle.Pools[index]
			}
		}
		auditIndex := finalSemanticDepositAuditIndex(intent.DepositAudits, transaction.NoID)
		if recorded == nil || penaltyPool == nil || auditIndex < 0 || recorded.PoolUID != penaltyPool.UID || recorded.SubnetEpoch != cycle.SubnetEpoch || recorded.VectorHash != cycle.IntentVectorHash || recorded.ApplicationBlock != cycle.Application.Block.Number || !strings.EqualFold(recorded.ApplicationBlockHash, cycle.Application.Block.Hash) || !finalJSONEqual(recorded.Audit, intent.DepositAudits[auditIndex]) || penaltyPool.AuditCompliant || penaltyPool.AppliedWeight != 0 {
			return fmt.Errorf("validator %d dishonest-deposit terminal decision differs from its signed cycle", collected.ValidatorID)
		}
		present, applied := finalSemanticSubmittedPool(cycle.Submitted, penaltyPool.UID)
		if present || applied != 0 || recorded.PoolPresent || recorded.PoolWeight != 0 {
			return fmt.Errorf("validator %d did not apply an absent/zero dishonest pool", collected.ValidatorID)
		}
		if result.PoolUID == 0 {
			result.PoolUID, result.RequiredDepositRao = penaltyPool.UID, penaltyPool.RequiredDepositRao
		} else if result.PoolUID != penaltyPool.UID || result.RequiredDepositRao != penaltyPool.RequiredDepositRao {
			return errors.New("validators disagree on dishonest-deposit pool demand")
		}
		result.Penalties = append(result.Penalties, FinalDishonestDepositDecision{ValidatorID: validator.ValidatorID, ValidatorUID: validator.UID, PoolUID: penaltyPool.UID, PoolPresent: present, PoolAppliedWeight: applied, Cycle: cycle})

		var recoveryCycle *FinalCRv4Cycle
		var recoveryPool *FinalPoolWeightEvidence
		for cycleIndex := range validator.Cycles {
			candidateCycle := &validator.Cycles[cycleIndex]
			if candidateCycle.SettlementEpoch <= cycle.SettlementEpoch {
				continue
			}
			for poolIndex := range candidateCycle.Pools {
				candidatePool := &candidateCycle.Pools[poolIndex]
				if candidatePool.NoID == transaction.NoID && candidatePool.AuditCompliant && candidatePool.RequiredDepositRao == candidatePool.ObservedDepositRao && candidatePool.AppliedWeight > 0 {
					recoveryCycle, recoveryPool = candidateCycle, candidatePool
					break
				}
			}
			if recoveryCycle != nil {
				break
			}
		}
		if recoveryCycle == nil || recoveryPool == nil || !finalSemanticReceiptIdentityEqual(recoveryPool.DepositReceipt, recovery) {
			return fmt.Errorf("validator %d lacks an exact positive-weight recovery cycle", collected.ValidatorID)
		}
		recoveryPresent, recoveryApplied := finalSemanticSubmittedPool(recoveryCycle.Submitted, recoveryPool.UID)
		if !recoveryPresent || recoveryApplied == 0 || recoveryApplied != recoveryPool.AppliedWeight {
			return fmt.Errorf("validator %d recovery pool is absent or zero", collected.ValidatorID)
		}
		if result.RecoveryRequiredDepositRao == "" {
			result.RecoveryRequiredDepositRao, result.RecoveryObservedDepositRao = recoveryPool.RequiredDepositRao, recoveryPool.ObservedDepositRao
		} else if result.RecoveryRequiredDepositRao != recoveryPool.RequiredDepositRao || result.RecoveryObservedDepositRao != recoveryPool.ObservedDepositRao {
			return errors.New("validators disagree on exact recovery demand")
		}
		result.Recoveries = append(result.Recoveries, FinalDishonestDepositDecision{ValidatorID: validator.ValidatorID, ValidatorUID: validator.UID, PoolUID: recoveryPool.UID, PoolPresent: recoveryPresent, PoolAppliedWeight: recoveryApplied, Cycle: *recoveryCycle})
	}
	sort.Slice(result.Penalties, func(i, j int) bool { return result.Penalties[i].ValidatorID < result.Penalties[j].ValidatorID })
	sort.Slice(result.Recoveries, func(i, j int) bool { return result.Recoveries[i].ValidatorID < result.Recoveries[j].ValidatorID })
	source.DishonestDeposit = result
	return nil
}

func finalSemanticDepositAuditIndex(audits []validatorpkg.DepositAudit, noID uint64) int {
	for index := range audits {
		if audits[index].NoID == noID {
			return index
		}
	}
	return -1
}

func finalSemanticSubmittedPool(submitted []FinalSubmittedWeight, uid uint16) (bool, uint16) {
	for _, item := range submitted {
		if item.UID == uid {
			return true, item.Value
		}
	}
	return false, 0
}

func finalSemanticReceiptIdentityEqual(left, right FinalEVMReceipt) bool {
	return left.TransactionHash == right.TransactionHash && left.Block == right.Block && left.Status == right.Status && left.LogsHash == right.LogsHash
}

func finalSemanticEventMatches(event finalSemanticEvent, epoch, noID uint64) bool {
	eventEpoch, epochOK := finalSemanticUint(event.Args, "epoch")
	eventNO, noOK := finalSemanticUint(event.Args, "noId")
	return epochOK && noOK && eventEpoch == epoch && eventNO == noID
}

func finalSemanticUniqueEvent(events *finalSemanticEventIndex, names []string, epoch, noID uint64, required bool) (*finalSemanticEvent, error) {
	if events == nil {
		return nil, errors.New("captured EVM event index is unavailable")
	}
	var matches []finalSemanticEvent
	for _, name := range names {
		for _, event := range events.byName[name] {
			if finalSemanticEventMatches(event, epoch, noID) {
				matches = append(matches, event)
			}
		}
	}
	if len(matches) == 0 && !required {
		return nil, nil
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("epoch %d operator %d has %d captured %s events", epoch, noID, len(matches), strings.Join(names, "/"))
	}
	return &matches[0], nil
}

func finalSemanticEpochView(terminal *ScenarioObservation, epoch, noID uint64) (EpochOperatorView, bool) {
	if terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil {
		return EpochOperatorView{}, false
	}
	for _, candidateEpoch := range terminal.Status.Contracts.Epochs {
		if candidateEpoch.Epoch != epoch {
			continue
		}
		for _, operator := range candidateEpoch.Operators {
			if operator.NoID == noID {
				return operator, true
			}
		}
	}
	return EpochOperatorView{}, false
}

func finalSemanticHexKey(value any) (string, bool) {
	switch typed := value.(type) {
	case [32]byte:
		return "0x" + hex.EncodeToString(typed[:]), true
	case common.Hash:
		return strings.ToLower(typed.Hex()), true
	default:
		return "", false
	}
}

func finalSemanticClaimOutput(events *finalSemanticEventIndex, claimed finalSemanticEvent, coldkey string) (*big.Int, *big.Int, error) {
	paid, deferred := new(big.Int), new(big.Int)
	paidCount, deferredCount := 0, 0
	for _, event := range events.byName["ClaimPaid"] {
		key, ok := finalSemanticHexKey(event.Args["coldkey"])
		if !ok || event.Log.TransactionHash != claimed.Log.TransactionHash || !strings.EqualFold(key, coldkey) {
			continue
		}
		amount, ok := finalSemanticInteger(event.Args, "amount")
		if !ok {
			return nil, nil, errors.New("captured ClaimPaid amount is invalid")
		}
		paid.Set(amount)
		paidCount++
	}
	for _, event := range events.byName["ClaimPaymentDeferred"] {
		key, ok := finalSemanticHexKey(event.Args["coldkey"])
		if !ok || event.Log.TransactionHash != claimed.Log.TransactionHash || !strings.EqualFold(key, coldkey) {
			continue
		}
		amount, ok := finalSemanticInteger(event.Args, "creditAlphaRao")
		if !ok {
			return nil, nil, errors.New("captured ClaimPaymentDeferred amount is invalid")
		}
		deferred.Set(amount)
		deferredCount++
	}
	if paidCount+deferredCount != 1 {
		return nil, nil, fmt.Errorf("claim transaction %s has %d payment dispositions", claimed.Log.TransactionHash, paidCount+deferredCount)
	}
	return paid, deferred, nil
}

func finalSemanticPayoutLeaf(artifact *payoutartifact.Artifact, coldkey string, shareBPS uint64) (uint64, bool) {
	if artifact == nil {
		return 0, false
	}
	for _, leaf := range artifact.Leaves {
		encoded := "0x" + hex.EncodeToString(leaf.Coldkey[:])
		if strings.EqualFold(encoded, coldkey) && leaf.ShareBPS == shareBPS {
			return leaf.Index, true
		}
	}
	return 0, false
}

func finalSemanticAddAmount(total *big.Int, encoded, label string) error {
	value, ok := new(big.Int).SetString(encoded, 10)
	if !ok || value.Sign() < 0 {
		return fmt.Errorf("%s is not a nonnegative decimal", label)
	}
	total.Add(total, value)
	return nil
}

func (a *finalSemanticArchive) buildEpochs(source *FinalSemanticEvidence, terminal *ScenarioObservation, events *finalSemanticEventIndex) error {
	if source == nil || terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil || events == nil {
		return errors.New("epoch construction context is incomplete")
	}
	totals := map[string]*big.Int{}
	for _, name := range []string{"captured", "carry_in", "funded", "claimed", "paid", "deferred", "outstanding", "carry_out"} {
		totals[name] = new(big.Int)
	}
	lastCarry := make(map[uint64]*big.Int, source.ExpectedOperators)
	poolCarry := make(map[uint64]*big.Int, len(source.Pools))
	for _, pool := range source.Pools {
		value, ok := new(big.Int).SetString(pool.FinalCarryRao, 10)
		if !ok || value.Sign() < 0 {
			return fmt.Errorf("operator %d terminal carry is invalid", pool.NoID)
		}
		poolCarry[pool.NoID] = value
	}
	// Infer carry entering the acceptance window from the first committed
	// entitlement (or terminal carry if every accepted root was missed). The
	// contract exposes carry only as current state, so this reverse equation is
	// the only immutable way to preserve pre-window carry without guessing zero.
	for noID := uint64(1); noID <= uint64(source.ExpectedOperators); noID++ {
		missedCaptured := new(big.Int)
		var initial *big.Int
		for epoch := source.Window.FirstEpoch; epoch < source.Window.FirstEpoch+source.Window.EpochCount; epoch++ {
			capture, err := finalSemanticUniqueEvent(events, []string{"EmissionCaptured", "EmissionDeferred"}, epoch, noID, true)
			if err != nil {
				return err
			}
			amount := new(big.Int)
			if capture.Name == "EmissionCaptured" {
				var ok bool
				amount, ok = finalSemanticInteger(capture.Args, "amount")
				if !ok {
					return fmt.Errorf("epoch %d operator %d capture amount is invalid", epoch, noID)
				}
			}
			root, err := finalSemanticUniqueEvent(events, []string{"OperatorRootCommitted"}, epoch, noID, false)
			if err != nil {
				return err
			}
			if root == nil {
				missedCaptured.Add(missedCaptured, amount)
				continue
			}
			view, ok := finalSemanticEpochView(terminal, epoch, noID)
			terminalTotal := new(big.Int)
			if !ok {
				return fmt.Errorf("terminal contract snapshot lacks epoch %d operator %d", epoch, noID)
			}
			if parsed, valid := terminalTotal.SetString(view.TotalRao, 10); !valid || parsed.Sign() < 0 {
				return fmt.Errorf("epoch %d operator %d terminal total is invalid", epoch, noID)
			}
			initial = new(big.Int).Sub(terminalTotal, amount)
			initial.Sub(initial, missedCaptured)
			break
		}
		if initial == nil {
			terminalCarry := poolCarry[noID]
			if terminalCarry == nil {
				return fmt.Errorf("operator %d terminal carry is absent", noID)
			}
			initial = new(big.Int).Sub(new(big.Int).Set(terminalCarry), missedCaptured)
		}
		if initial.Sign() < 0 {
			return fmt.Errorf("operator %d inferred acceptance carry-in is negative", noID)
		}
		lastCarry[noID] = initial
	}
	for epoch := source.Window.FirstEpoch; epoch < source.Window.FirstEpoch+source.Window.EpochCount; epoch++ {
		for noID := uint64(1); noID <= uint64(source.ExpectedOperators); noID++ {
			view, ok := finalSemanticEpochView(terminal, epoch, noID)
			if !ok || view.Status == 0 {
				return fmt.Errorf("terminal contract snapshot lacks finalized epoch %d operator %d", epoch, noID)
			}
			captureEvent, err := finalSemanticUniqueEvent(events, []string{"EmissionCaptured", "EmissionDeferred"}, epoch, noID, true)
			if err != nil {
				return err
			}
			captured := new(big.Int)
			if captureEvent.Name == "EmissionCaptured" {
				amount, amountOK := finalSemanticInteger(captureEvent.Args, "amount")
				if !amountOK {
					return fmt.Errorf("epoch %d operator %d capture amount is invalid", epoch, noID)
				}
				captured.Set(amount)
			}
			if viewFunded, valid := new(big.Int).SetString(view.FundedRao, 10); !valid || viewFunded.Cmp(captured) != 0 {
				return fmt.Errorf("epoch %d operator %d captured log differs from terminal funded state", epoch, noID)
			}
			captureReceipt, err := a.receiptFromIndex(events, *captureEvent, fmt.Sprintf("epoch-%d-pool-%d-capture", epoch, noID))
			if err != nil {
				return err
			}
			finalizeEvent, err := finalSemanticUniqueEvent(events, []string{"OperatorEpochFinalized"}, epoch, noID, true)
			if err != nil {
				return err
			}
			finalizeReceipt, err := a.receiptFromIndex(events, *finalizeEvent, fmt.Sprintf("epoch-%d-pool-%d-finalize", epoch, noID))
			if err != nil {
				return err
			}
			rootEvent, err := finalSemanticUniqueEvent(events, []string{"OperatorRootCommitted"}, epoch, noID, false)
			if err != nil {
				return err
			}
			missedEvent, err := finalSemanticUniqueEvent(events, []string{"RootMissed"}, epoch, noID, false)
			if err != nil {
				return err
			}
			row := FinalEpochOperatorEvidence{Epoch: epoch, NoID: noID, Capture: captureReceipt, Finalize: finalizeReceipt, Status: view.Status, Claims: []FinalClaimEvidence{}}
			carryIn := new(big.Int)
			if previous := lastCarry[noID]; previous != nil {
				carryIn.Set(previous)
			}
			// The vault records only funding captured at this boundary in
			// Entitlement.funded. Carry is a separate per-operator ledger and is
			// consumed into Entitlement.total only when a root is committed.
			funded := new(big.Int).Set(captured)
			total := new(big.Int).Set(funded)
			claimed, valid := new(big.Int).SetString(view.ClaimedRao, 10)
			if !valid || claimed.Sign() < 0 {
				return fmt.Errorf("epoch %d operator %d terminal claimed amount is invalid", epoch, noID)
			}
			paid, deferred := new(big.Int), new(big.Int)
			if rootEvent != nil {
				if missedEvent != nil {
					return fmt.Errorf("epoch %d operator %d has both committed and missed root events", epoch, noID)
				}
				if rootPresent, ok := finalizeEvent.Args["rootPresent"].(bool); !ok || !rootPresent {
					return fmt.Errorf("epoch %d operator %d finalize log denies its committed root", epoch, noID)
				}
				root, rootOK := finalSemanticHex32(rootEvent.Args, "payoutRoot")
				artifactHash, artifactOK := finalSemanticHex32(rootEvent.Args, "artifactHash")
				committer, committerOK := finalSemanticAddress(rootEvent.Args, "committer")
				if !rootOK || !artifactOK || !committerOK || !strings.EqualFold(root, view.PayoutRoot) || !strings.EqualFold(artifactHash, view.ArtifactHash) || !strings.EqualFold(committer, view.Committer) || rootEvent.Log.BlockNumber != view.CommitBlock {
					return fmt.Errorf("epoch %d operator %d root log differs from terminal commitment", epoch, noID)
				}
				rootReceipt, err := a.receiptFromIndex(events, *rootEvent, fmt.Sprintf("epoch-%d-pool-%d-root", epoch, noID))
				if err != nil {
					return err
				}
				payoutLocator, found := finalSemanticPayout(a.collected, epoch, noID)
				if !found {
					return fmt.Errorf("epoch %d operator %d committed payout artifact is absent", epoch, noID)
				}
				payoutData, _, err := a.file(payoutLocator.URI)
				if err != nil {
					return err
				}
				payout, err := payoutartifact.Decode(payoutData)
				if err != nil || payout.Epoch != epoch || payout.NoID != noID || !strings.EqualFold("0x"+strings.TrimPrefix(payout.ContentHash, "sha256:"), artifactHash) || !strings.EqualFold("0x"+hex.EncodeToString(payout.PayoutRoot[:]), root) {
					return fmt.Errorf("epoch %d operator %d payout artifact differs from committed root", epoch, noID)
				}
				terminalTotal, totalOK := new(big.Int).SetString(view.TotalRao, 10)
				if !totalOK || terminalTotal.Sign() < 0 {
					return fmt.Errorf("epoch %d operator %d terminal total is invalid", epoch, noID)
				}
				total.Add(funded, carryIn)
				if terminalTotal.Cmp(total) != 0 {
					return fmt.Errorf("epoch %d operator %d total does not consume exact carry-in", epoch, noID)
				}
				row.RootDisposition, row.Root, row.PayoutRoot, row.ArtifactHash, row.PayoutArtifact = "committed", &rootReceipt, strings.ToLower(root), strings.ToLower(artifactHash), &payoutLocator
				for _, claimEvent := range events.byName["Claimed"] {
					if !finalSemanticEventMatches(claimEvent, epoch, noID) {
						continue
					}
					coldkey, coldkeyOK := finalSemanticHex32(claimEvent.Args, "coldkey")
					shareBPS, shareOK := finalSemanticUint(claimEvent.Args, "shareBps")
					amount, amountOK := finalSemanticInteger(claimEvent.Args, "amount")
					if !coldkeyOK || !shareOK || !amountOK {
						return fmt.Errorf("epoch %d operator %d claim log is invalid", epoch, noID)
					}
					entitlement := new(big.Int).Mul(new(big.Int).SetUint64(shareBPS), terminalTotal)
					entitlement.Quo(entitlement, big.NewInt(10_000))
					if shareBPS == 0 || shareBPS > 10_000 || amount.Cmp(entitlement) != 0 {
						return fmt.Errorf("epoch %d operator %d claim amount does not equal floor(share_bps*total/10000)", epoch, noID)
					}
					leafIndex, leafOK := finalSemanticPayoutLeaf(payout, coldkey, shareBPS)
					if !leafOK {
						return fmt.Errorf("epoch %d operator %d claim is absent from payout Merkle leaves", epoch, noID)
					}
					claimPaid, claimDeferred, err := finalSemanticClaimOutput(events, claimEvent, coldkey)
					if err != nil || new(big.Int).Add(new(big.Int).Set(claimPaid), claimDeferred).Cmp(amount) != 0 {
						return fmt.Errorf("epoch %d operator %d claim payment mismatch: %w", epoch, noID, err)
					}
					receipt, err := a.receiptFromIndex(events, claimEvent, fmt.Sprintf("epoch-%d-pool-%d-claim-%d", epoch, noID, leafIndex))
					if err != nil {
						return err
					}
					row.Claims = append(row.Claims, FinalClaimEvidence{LeafIndex: leafIndex, Payee: strings.ToLower(coldkey), ShareBPS: shareBPS, ClaimedRao: amount.String(), PaidRao: claimPaid.String(), DeferredRao: claimDeferred.String(), Receipt: receipt})
					paid.Add(paid, claimPaid)
					deferred.Add(deferred, claimDeferred)
				}
				sort.Slice(row.Claims, func(i, j int) bool { return row.Claims[i].LeafIndex < row.Claims[j].LeafIndex })
				row.OutstandingRao = new(big.Int).Sub(new(big.Int).Set(total), claimed).String()
				row.CarryOutRao = "0"
				lastCarry[noID] = new(big.Int)
			} else {
				if missedEvent == nil {
					return fmt.Errorf("epoch %d operator %d has neither committed nor missed root event", epoch, noID)
				}
				if rootPresent, ok := finalizeEvent.Args["rootPresent"].(bool); !ok || rootPresent {
					return fmt.Errorf("epoch %d operator %d finalize log falsely reports a root", epoch, noID)
				}
				carried, carriedOK := finalSemanticInteger(missedEvent.Args, "carried")
				if !carriedOK || carried.Cmp(captured) != 0 || claimed.Sign() != 0 {
					return fmt.Errorf("epoch %d operator %d missed-root carry differs from capture", epoch, noID)
				}
				carryOut := new(big.Int).Add(new(big.Int).Set(carryIn), funded)
				row.RootDisposition, row.OutstandingRao, row.CarryOutRao = "missed", "0", carryOut.String()
				lastCarry[noID] = new(big.Int).Set(carryOut)
			}
			row.CapturedRao, row.CarryInRao, row.FundedRao, row.TotalRao = captured.String(), carryIn.String(), funded.String(), total.String()
			row.ClaimedRao, row.PaidRao, row.DeferredCreditRao = claimed.String(), paid.String(), deferred.String()
			if new(big.Int).Add(new(big.Int).Set(paid), deferred).Cmp(claimed) != 0 {
				return fmt.Errorf("epoch %d operator %d claim event totals differ from terminal claimed amount", epoch, noID)
			}
			for name, encoded := range map[string]string{"captured": row.CapturedRao, "carry_in": row.CarryInRao, "funded": row.FundedRao, "claimed": row.ClaimedRao, "paid": row.PaidRao, "deferred": row.DeferredCreditRao, "outstanding": row.OutstandingRao, "carry_out": row.CarryOutRao} {
				if err := finalSemanticAddAmount(totals[name], encoded, fmt.Sprintf("epoch %d operator %d %s", epoch, noID, name)); err != nil {
					return err
				}
			}
			source.Epochs = append(source.Epochs, row)
		}
	}
	source.Conservation = FinalPoolConservation{
		CapturedRao: totals["captured"].String(), CarryInRao: totals["carry_in"].String(), FundedRao: totals["funded"].String(),
		ClaimedRao: totals["claimed"].String(), PaidRao: totals["paid"].String(), DeferredCreditRao: totals["deferred"].String(),
		OutstandingRao: totals["outstanding"].String(), CarryOutRao: totals["carry_out"].String(),
	}
	return nil
}
func finalSemanticRewardExpectation(source *FinalSemanticEvidence, epoch uint64) (map[uint64]string, map[uint64]string) {
	headSelections := make(map[uint64]int, len(source.HeadFleets))
	poolEligible := make(map[uint64]bool, len(source.Pools))
	for _, validator := range source.Validators {
		for _, cycle := range validator.Cycles {
			if cycle.SettlementEpoch != epoch {
				continue
			}
			for _, candidate := range cycle.Candidates {
				if candidate.Selected {
					headSelections[candidate.FleetID]++
				}
			}
			for _, pool := range cycle.Pools {
				poolEligible[pool.NoID] = poolEligible[pool.NoID] || pool.AuditCompliant
			}
		}
	}
	heads := make(map[uint64]string, len(source.HeadFleets))
	for _, fleet := range source.HeadFleets {
		expected := "observed"
		switch headSelections[fleet.FleetID] {
		case 0:
			expected = "zero"
		case len(source.Validators):
			expected = "positive"
		}
		heads[fleet.FleetID] = expected
	}
	pools := make(map[uint64]string, len(source.Pools))
	for _, pool := range source.Pools {
		pools[pool.NoID] = "zero"
		if poolEligible[pool.NoID] {
			pools[pool.NoID] = "positive"
		}
	}
	return heads, pools
}

func finalSemanticRewardSnapshotValid(source *FinalSemanticEvidence, reward *NativeRewardObservation, epoch uint64) bool {
	if source == nil || reward == nil {
		return false
	}
	headExpected, poolExpected := finalSemanticRewardExpectation(source, epoch)
	valid := func(uid uint16, role, expected string) bool {
		emission, incentive, dividends, ok := nativeRewardAt(reward, uid)
		if !ok {
			return false
		}
		if _, stakeOK := nativeRewardStakeAt(reward, uid); !stakeOK {
			return false
		}
		score := incentive
		if role == "validator" {
			if incentive != 0 {
				return false
			}
			score = dividends
		} else if dividends != 0 {
			return false
		}
		switch expected {
		case "positive":
			return emission.Sign() > 0 && score > 0
		case "zero":
			return emission.Sign() == 0 && score == 0
		default:
			return (emission.Sign() > 0) == (score > 0)
		}
	}
	for _, fleet := range source.HeadFleets {
		uid, err := finalSemanticRewardUIDAt(source, fleet.FleetID, epoch, fleet.UID)
		if err != nil || !valid(uid, "head", headExpected[fleet.FleetID]) {
			return false
		}
	}
	for _, pool := range source.Pools {
		if !valid(pool.UID, "pool", poolExpected[pool.NoID]) {
			return false
		}
	}
	for _, validator := range source.Validators {
		if !valid(validator.UID, "validator", "positive") {
			return false
		}
	}
	return true
}

func finalSemanticRewardSnapshots(history []*ScenarioObservation, source *FinalSemanticEvidence) ([]*NativeRewardObservation, error) {
	byHead := make(map[uint64]*NativeRewardObservation)
	for _, observation := range history {
		if observation == nil || observation.NativeRewards == nil || observation.NativeRewardsError != "" {
			continue
		}
		reward := observation.NativeRewards
		if reward.FinalizedHead.Number < source.NativeStartHead.Number || reward.FinalizedHead.Number > source.NativeTerminalHead.Number || reward.FinalizedHead.Hash == "" {
			continue
		}
		if previous := byHead[reward.FinalizedHead.Number]; previous != nil {
			if previous.FinalizedHead.Hash != reward.FinalizedHead.Hash || !finalJSONEqual(previous, reward) {
				return nil, fmt.Errorf("native reward snapshots conflict at block %d", reward.FinalizedHead.Number)
			}
			continue
		}
		byHead[reward.FinalizedHead.Number] = reward
	}
	result := make([]*NativeRewardObservation, 0, len(byHead))
	for _, reward := range byHead {
		result = append(result, reward)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].FinalizedHead.Number < result[j].FinalizedHead.Number })
	if len(result) < 2 {
		return nil, errors.New("closed observation history has fewer than two native reward snapshots")
	}
	return result, nil
}

func finalSemanticApplicationBlock(source *FinalSemanticEvidence, epoch uint64) (uint64, error) {
	var maximum uint64
	for _, validator := range source.Validators {
		found := false
		for _, cycle := range validator.Cycles {
			if cycle.SettlementEpoch == epoch {
				found = true
				if cycle.Application.Block.Number > maximum {
					maximum = cycle.Application.Block.Number
				}
			}
		}
		if !found {
			return 0, fmt.Errorf("validator %d lacks cycle for reward epoch %d", validator.ValidatorID, epoch)
		}
	}
	return maximum, nil
}

func finalSemanticStakeSnapshotAt(chain *FinalCollectedChainSnapshot, head ChainHead) (*FinalCollectedRewardStakeSnapshot, error) {
	if chain == nil {
		return nil, errors.New("closed reward stake snapshot graph is unavailable")
	}
	for index := range chain.RewardStakeSnapshots {
		snapshot := &chain.RewardStakeSnapshots[index]
		if snapshot.NativeHead == head {
			return snapshot, nil
		}
	}
	return nil, fmt.Errorf("closed reward stake graph lacks native checkpoint %d/%s", head.Number, head.Hash)
}

func finalSemanticSS58Pair(label, hotkey, coldkey string) ([32]byte, [32]byte, error) {
	hotkeyBytes, hotkeyPrefix, err := ss58.Decode(hotkey)
	if err != nil || hotkeyPrefix != ss58.BittensorPrefix {
		return [32]byte{}, [32]byte{}, stateMismatchError(err, "%s hotkey is not canonical Bittensor SS58", label)
	}
	coldkeyBytes, coldkeyPrefix, err := ss58.Decode(coldkey)
	if err != nil || coldkeyPrefix != ss58.BittensorPrefix {
		return [32]byte{}, [32]byte{}, stateMismatchError(err, "%s coldkey is not canonical Bittensor SS58", label)
	}
	return hotkeyBytes, coldkeyBytes, nil
}

func finalSemanticRewardOwnerPair(source *FinalSemanticEvidence, role string, subjectID uint64) ([32]byte, [32]byte, error) {
	epoch := uint64(0)
	if source != nil {
		epoch = source.Window.FirstEpoch
	}
	return finalSemanticRewardOwnerPairAt(source, role, subjectID, epoch)
}

func finalSemanticRewardOwnerPairAt(source *FinalSemanticEvidence, role string, subjectID, epoch uint64) ([32]byte, [32]byte, error) {
	if source == nil {
		return [32]byte{}, [32]byte{}, errors.New("native reward owner evidence is unavailable")
	}
	switch role {
	case "head":
		for _, fleet := range source.HeadFleets {
			if fleet.FleetID == subjectID {
				if source.FleetLifecycle != nil && (subjectID == fleetLifecycleTargetFleet || subjectID == fleetLifecycleCompanionFleet) {
					_, hotkey, coldkey, err := finalFleetLifecycleHeadAt(source.FleetLifecycle, subjectID, epoch)
					if err != nil {
						return [32]byte{}, [32]byte{}, err
					}
					hotkeyBytes, err := decodeHex32(fmt.Sprintf("head fleet %d lifecycle hotkey", subjectID), hotkey)
					if err != nil {
						return [32]byte{}, [32]byte{}, err
					}
					coldkeyBytes, err := decodeHex32(fmt.Sprintf("head fleet %d lifecycle coldkey", subjectID), coldkey)
					return hotkeyBytes, coldkeyBytes, err
				}
				return finalSemanticSS58Pair(fmt.Sprintf("head fleet %d", subjectID), fleet.Hotkey, fleet.Coldkey)
			}
		}
	case "pool":
		for _, pool := range source.Pools {
			if pool.NoID == subjectID {
				return finalSemanticSS58Pair(fmt.Sprintf("pool %d", subjectID), pool.Hotkey, pool.Coldkey)
			}
		}
	case "validator":
		for _, validator := range source.Validators {
			if validator.ValidatorID == subjectID {
				return finalSemanticSS58Pair(fmt.Sprintf("validator %d", subjectID), validator.Hotkey, validator.Coldkey)
			}
		}
	}
	return [32]byte{}, [32]byte{}, fmt.Errorf("native reward %s %d has no exact owner pair", role, subjectID)
}

func finalSemanticRewardUIDAt(source *FinalSemanticEvidence, fleetID, epoch uint64, terminalUID uint16) (uint16, error) {
	if source != nil && source.FleetLifecycle != nil && (fleetID == fleetLifecycleTargetFleet || fleetID == fleetLifecycleCompanionFleet) {
		uid, _, _, err := finalFleetLifecycleHeadAt(source.FleetLifecycle, fleetID, epoch)
		return uid, err
	}
	return terminalUID, nil
}

func finalSemanticStakePosition(snapshot *FinalCollectedRewardStakeSnapshot, hotkey, coldkey [32]byte) (*big.Int, error) {
	if snapshot == nil {
		return nil, errors.New("closed reward stake checkpoint is unavailable")
	}
	hotkeyHex, coldkeyHex := strings.ToLower(fmt.Sprintf("0x%x", hotkey[:])), strings.ToLower(fmt.Sprintf("0x%x", coldkey[:]))
	var found *big.Int
	for _, position := range snapshot.Positions {
		if position.HotkeyPublicKey != hotkeyHex || position.ColdkeyPublicKey != coldkeyHex {
			continue
		}
		if found != nil {
			return nil, errors.New("closed reward stake graph duplicates an owner pair")
		}
		stake, ok := new(big.Int).SetString(position.StakeRao, 10)
		if !ok || stake.Sign() < 0 || stake.String() != position.StakeRao {
			return nil, errors.New("closed reward stake position is not canonical")
		}
		found = stake
	}
	if found == nil {
		return nil, fmt.Errorf("closed reward stake graph lacks owner pair %s/%s", hotkeyHex, coldkeyHex)
	}
	return found, nil
}

func (a *finalSemanticArchive) buildRewards(source *FinalSemanticEvidence, history []*ScenarioObservation, chain *FinalCollectedChainSnapshot) error {
	if source == nil || chain == nil || len(history) == 0 {
		return errors.New("native reward construction context is incomplete")
	}
	snapshots, err := finalSemanticRewardSnapshots(history, source)
	if err != nil {
		return err
	}
	previousAfter := uint64(0)
	for epoch := source.Window.FirstEpoch; epoch < source.Window.FirstEpoch+source.Window.EpochCount; epoch++ {
		applicationBlock, err := finalSemanticApplicationBlock(source, epoch)
		if err != nil {
			return err
		}
		var before, after *NativeRewardObservation
		for _, candidate := range snapshots {
			if candidate.FinalizedHead.Number < applicationBlock || candidate.FinalizedHead.Number <= previousAfter || !finalSemanticRewardSnapshotValid(source, candidate, epoch) {
				continue
			}
			after = candidate
			break
		}
		if after == nil {
			return fmt.Errorf("closed history has no post-application native reward snapshot for settlement epoch %d", epoch)
		}
		if source.FleetLifecycle != nil && epoch == source.FleetLifecycle.State.ProviderEffectiveEpoch {
			baseline := source.FleetLifecycle.State.PostRegistrationRewardBaseline
			for _, candidate := range snapshots {
				if candidate.FinalizedHead == baseline {
					before = candidate
					break
				}
			}
			if before == nil || before.FinalizedHead.Number < previousAfter || before.FinalizedHead.Number >= after.FinalizedHead.Number {
				return fmt.Errorf("closed history lacks the exact post-registration reward baseline %d/%s for settlement epoch %d", baseline.Number, baseline.Hash, epoch)
			}
		} else {
			for index := len(snapshots) - 1; index >= 0; index-- {
				candidate := snapshots[index]
				if candidate.FinalizedHead.Number < after.FinalizedHead.Number && (previousAfter == 0 || candidate.FinalizedHead.Number >= previousAfter) {
					before = candidate
					break
				}
			}
		}
		if before == nil {
			return fmt.Errorf("closed history has no pre-reward native snapshot for settlement epoch %d", epoch)
		}
		beforeStakeSnapshot, err := finalSemanticStakeSnapshotAt(chain, before.FinalizedHead)
		if err != nil {
			return err
		}
		afterStakeSnapshot, err := finalSemanticStakeSnapshotAt(chain, after.FinalizedHead)
		if err != nil {
			return err
		}
		snapshotArtifact, err := a.derived("native-reward-snapshot", fmt.Sprintf("native-reward-epoch-%d.json", epoch), map[string]any{"epoch": epoch, "application_block": applicationBlock, "before": before, "after": after, "before_owner_stakes": beforeStakeSnapshot, "after_owner_stakes": afterStakeSnapshot})
		if err != nil {
			return err
		}
		headExpected, poolExpected := finalSemanticRewardExpectation(source, epoch)
		appendReward := func(role string, subjectID uint64, uid uint16, expected string) error {
			beforeEmission, beforeIncentive, beforeDividends, ok := nativeRewardAt(before, uid)
			if !ok {
				return fmt.Errorf("native reward before snapshot lacks %s %d UID %d", role, subjectID, uid)
			}
			afterEmission, afterIncentive, afterDividends, ok := nativeRewardAt(after, uid)
			if !ok {
				return fmt.Errorf("native reward after snapshot lacks %s %d UID %d", role, subjectID, uid)
			}
			beforeStake, ok := nativeRewardStakeAt(before, uid)
			if !ok {
				return fmt.Errorf("native reward before snapshot lacks %s %d UID %d total-hotkey-alpha stake", role, subjectID, uid)
			}
			afterStake, ok := nativeRewardStakeAt(after, uid)
			if !ok {
				return fmt.Errorf("native reward after snapshot lacks %s %d UID %d total-hotkey-alpha stake", role, subjectID, uid)
			}
			hotkey, ownerColdkey, err := finalSemanticRewardOwnerPairAt(source, role, subjectID, epoch)
			if err != nil {
				return err
			}
			ownerBefore, err := finalSemanticStakePosition(beforeStakeSnapshot, hotkey, ownerColdkey)
			if err != nil {
				return fmt.Errorf("native reward before %s %d owner pair: %w", role, subjectID, err)
			}
			ownerAfter, err := finalSemanticStakePosition(afterStakeSnapshot, hotkey, ownerColdkey)
			if err != nil {
				return fmt.Errorf("native reward after %s %d owner pair: %w", role, subjectID, err)
			}
			row := FinalNativeRewardDelta{
				Epoch: epoch, Role: role, SubjectID: subjectID, UID: uid, Hotkey: strings.ToLower(fmt.Sprintf("0x%x", hotkey[:])),
				Before: before.FinalizedHead, After: after.FinalizedHead, BeforeRao: beforeEmission.String(), AfterRao: afterEmission.String(),
				DeltaRao: new(big.Int).Sub(afterEmission, beforeEmission).String(), StakeBeforeRao: beforeStake.String(), StakeAfterRao: afterStake.String(),
				StakeDeltaRao: new(big.Int).Sub(afterStake, beforeStake).String(), OwnerColdkey: strings.ToLower(fmt.Sprintf("0x%x", ownerColdkey[:])),
				OwnerStakeBeforeRao: ownerBefore.String(), OwnerStakeAfterRao: ownerAfter.String(), OwnerStakeDeltaRao: new(big.Int).Sub(ownerAfter, ownerBefore).String(),
				OwnerStakeBeforeEVM: beforeStakeSnapshot.EVMHead, OwnerStakeAfterEVM: afterStakeSnapshot.EVMHead,
				BeforeIncentiveU16: beforeIncentive, AfterIncentiveU16: afterIncentive,
				BeforeDividendsU16: beforeDividends, AfterDividendsU16: afterDividends, Expected: expected, SnapshotArtifact: snapshotArtifact,
			}
			if role == "validator" && subjectID == 1 {
				reserveColdkey, err := decodeHex32("reserve self coldkey", source.Deployment.ReserveSelfColdkey)
				if err != nil {
					return err
				}
				reserveBefore, err := finalSemanticStakePosition(beforeStakeSnapshot, hotkey, reserveColdkey)
				if err != nil {
					return fmt.Errorf("reserve validator sink stake before: %w", err)
				}
				reserveAfter, err := finalSemanticStakePosition(afterStakeSnapshot, hotkey, reserveColdkey)
				if err != nil {
					return fmt.Errorf("reserve validator sink stake after: %w", err)
				}
				row.ReserveColdkey = strings.ToLower(fmt.Sprintf("0x%x", reserveColdkey[:]))
				row.ReserveStakeBeforeRao, row.ReserveStakeAfterRao = reserveBefore.String(), reserveAfter.String()
				row.ReserveStakeDeltaRao = new(big.Int).Sub(reserveAfter, reserveBefore).String()
			}
			ownerDelta := new(big.Int).Sub(ownerAfter, ownerBefore)
			switch role {
			case "head":
				if ownerBefore.Cmp(beforeStake) != 0 || ownerAfter.Cmp(afterStake) != 0 || expected == "positive" && ownerDelta.Sign() <= 0 || expected == "zero" && ownerDelta.Sign() != 0 {
					return fmt.Errorf("native reward %s %d head owner-pair stake differs from TotalHotkeyAlpha or selection", role, subjectID)
				}
			case "pool":
				if ownerBefore.Cmp(beforeStake) != 0 || ownerAfter.Cmp(afterStake) != 0 {
					return fmt.Errorf("native reward pool %d custody position differs from TotalHotkeyAlpha", subjectID)
				}
			case "validator":
				if ownerDelta.Sign() <= 0 {
					return fmt.Errorf("native reward validator %d owner-pair stake did not grow", subjectID)
				}
				if subjectID == 1 {
					reserveBefore, _ := new(big.Int).SetString(row.ReserveStakeBeforeRao, 10)
					reserveAfter, _ := new(big.Int).SetString(row.ReserveStakeAfterRao, 10)
					if reserveAfter.Cmp(reserveBefore) <= 0 || beforeStake.Cmp(new(big.Int).Add(ownerBefore, reserveBefore)) != 0 || afterStake.Cmp(new(big.Int).Add(ownerAfter, reserveAfter)) != 0 {
						return errors.New("native reward reserve validator aggregate does not reconcile owner and reserve-sink positions")
					}
				} else if ownerBefore.Cmp(beforeStake) != 0 || ownerAfter.Cmp(afterStake) != 0 {
					return fmt.Errorf("native reward validator %d owner position differs from TotalHotkeyAlpha", subjectID)
				}
			}
			source.NativeRewards = append(source.NativeRewards, row)
			return nil
		}
		for _, fleet := range source.HeadFleets {
			uid, err := finalSemanticRewardUIDAt(source, fleet.FleetID, epoch, fleet.UID)
			if err != nil {
				return err
			}
			if err := appendReward("head", fleet.FleetID, uid, headExpected[fleet.FleetID]); err != nil {
				return err
			}
		}
		for _, pool := range source.Pools {
			if err := appendReward("pool", pool.NoID, pool.UID, poolExpected[pool.NoID]); err != nil {
				return err
			}
		}
		for _, validator := range source.Validators {
			if err := appendReward("validator", validator.ValidatorID, validator.UID, "positive"); err != nil {
				return err
			}
		}
		previousAfter = after.FinalizedHead.Number
	}
	sort.Slice(source.NativeRewards, func(i, j int) bool {
		if source.NativeRewards[i].Epoch != source.NativeRewards[j].Epoch {
			return source.NativeRewards[i].Epoch < source.NativeRewards[j].Epoch
		}
		if source.NativeRewards[i].Role != source.NativeRewards[j].Role {
			return source.NativeRewards[i].Role < source.NativeRewards[j].Role
		}
		return source.NativeRewards[i].SubjectID < source.NativeRewards[j].SubjectID
	})
	return nil
}
func (a *finalSemanticArchive) buildPathProofs(source *FinalSemanticEvidence) error {
	if source == nil {
		return errors.New("path-proof construction context is incomplete")
	}
	var policy protocol.Policy
	policyData, _, err := a.file(a.collected.Policy.URI)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(policyData, &policy); err != nil || policy.Verify.TrailDepth < 1 {
		return errors.New("closed canonical policy has no path-proof trail depth")
	}
	for _, validator := range a.collected.Validators {
		for _, proof := range validator.PathProofs {
			if proof.FirstEpoch != source.Window.FirstEpoch || proof.LastEpoch != source.Window.FirstEpoch+source.Window.EpochCount-1 || proof.ProofCount < source.Window.EpochCount {
				return fmt.Errorf("validator %d operator %d path-proof coverage differs from acceptance", validator.ValidatorID, proof.NoID)
			}
			source.PathProofs = append(source.PathProofs, FinalValidatorPathProofEvidence{
				ValidatorID: validator.ValidatorID, NoID: proof.NoID, FirstEpoch: proof.FirstEpoch, LastEpoch: proof.LastEpoch,
				ProofCount: proof.ProofCount, TrailDepth: policy.Verify.TrailDepth, ProofsHash: proof.Artifact.ContentHash, Artifact: proof.Artifact,
			})
		}
	}
	sort.Slice(source.PathProofs, func(i, j int) bool {
		if source.PathProofs[i].ValidatorID != source.PathProofs[j].ValidatorID {
			return source.PathProofs[i].ValidatorID < source.PathProofs[j].ValidatorID
		}
		return source.PathProofs[i].NoID < source.PathProofs[j].NoID
	})
	return nil
}
func finalSemanticAdversaryMetric(result *ScenarioResult, name string) (AdversaryMetricEvidence, bool) {
	if result == nil || result.Adversaries == nil {
		return AdversaryMetricEvidence{}, false
	}
	var combined AdversaryMetricEvidence
	found := false
	for _, actor := range result.Adversaries.Actors {
		metric, ok := actor.Metrics[name]
		if !ok || metric.Samples == 0 {
			continue
		}
		if !found {
			combined = metric
			found = true
			continue
		}
		combined.Samples += metric.Samples
		if metric.Minimum < combined.Minimum {
			combined.Minimum = metric.Minimum
		}
		if metric.Maximum > combined.Maximum {
			combined.Maximum = metric.Maximum
		}
		combined.Last = metric.Last
	}
	return combined, found
}

func finalSemanticAdversaryVectorPassed(result *ScenarioResult, id string) bool {
	if result == nil || result.Adversaries == nil {
		return false
	}
	for _, vector := range result.Adversaries.Vectors {
		if vector.ID == id {
			return vector.Status == "pass"
		}
	}
	return false
}

func finalSemanticAssertionPassed(result *ScenarioResult, id string) bool {
	if result == nil {
		return false
	}
	for _, assertion := range result.Assertions {
		if assertion.ID == id {
			return assertion.Passed
		}
	}
	return false
}

func finalSemanticProcessAnomalyCount(result *ScenarioResult, terminal *ScenarioObservation, restarts []FinalProcessRestartEvidence) uint64 {
	count := uint64(0)
	if result == nil || result.Anomalies == nil || result.Anomalies.Status != "clean" {
		count++
	}
	if result != nil && result.Anomalies != nil {
		count += uint64(len(result.Anomalies.Entries))
	}
	if terminal == nil || terminal.Status == nil || terminal.Status.Supervisor == nil {
		return count + 1
	}
	for _, finding := range terminal.ProcessLogFindings {
		if finding.Blocking {
			count += finding.Count
		}
	}
	restartByID := make(map[string]FinalProcessRestartEvidence, len(restarts))
	for _, restart := range restarts {
		if restart.ProcessID == "" || restartByID[restart.ProcessID].ProcessID != "" || restart.ExpectedRestarts != restart.ObservedRestarts {
			count++
		}
		restartByID[restart.ProcessID] = restart
	}
	for _, process := range terminal.Status.Supervisor.Processes {
		if !process.Healthy || process.ExitError != "" {
			count++
		}
		restart, ok := restartByID[process.ID]
		if !ok || process.Restarts < 0 || uint64(process.Restarts) != restart.ObservedRestarts {
			count++
		}
		delete(restartByID, process.ID)
	}
	count += uint64(len(restartByID))
	return count
}

func finalSemanticDepositEventIdentity(event *finalSemanticEvent, coordinator string, noID, epoch uint64, amount, nonce *big.Int, policyHash, funder string) error {
	if event == nil || event.Name != "Deposit" || !strings.EqualFold(event.Log.Address, coordinator) {
		return errors.New("captured Deposit event has the wrong contract identity")
	}
	eventNO, noOK := finalSemanticUint(event.Args, "noId")
	eventEpoch, epochOK := finalSemanticUint(event.Args, "epoch")
	eventAmount, amountOK := finalSemanticInteger(event.Args, "amount")
	eventNonce, nonceOK := finalSemanticInteger(event.Args, "nonce")
	eventPolicy, policyOK := finalSemanticHex32(event.Args, "policyHash")
	eventFunder, funderOK := finalSemanticAddress(event.Args, "funder")
	if !noOK || !epochOK || !amountOK || !nonceOK || !policyOK || !funderOK || eventNO != noID || eventEpoch != epoch || amount != nil && eventAmount.Cmp(amount) != 0 || nonce != nil && eventNonce.Cmp(nonce) != 0 || !strings.EqualFold(eventPolicy, policyHash) || !strings.EqualFold(eventFunder, funder) {
		return fmt.Errorf("captured Deposit event does not bind operator %d epoch %d amount/policy/nonce/funder", noID, epoch)
	}
	return nil
}

func finalSemanticCanonicalDecimal(label, value string, positive bool) (*big.Int, error) {
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok || parsed.Sign() < 0 || positive && parsed.Sign() == 0 || parsed.String() != value {
		return nil, fmt.Errorf("%s is not a canonical decimal integer", label)
	}
	return parsed, nil
}

func (a *finalSemanticArchive) dishonestDepositReceipts(source *FinalSemanticEvidence, terminal *ScenarioObservation, events *finalSemanticEventIndex) ([]FinalEVMReceipt, error) {
	if a == nil || a.cfg == nil || a.cfg.Config == nil || source == nil || terminal == nil || terminal.DishonestDeposit == nil || !terminal.DishonestDepositValid || events == nil {
		return nil, errors.New("terminal dishonest-deposit evidence is unavailable")
	}
	dishonest := terminal.DishonestDeposit
	transaction := dishonest.Transaction
	if dishonest.Schema != dishonestDepositEvidenceV1 || dishonest.DeploymentID != source.DeploymentID || dishonest.Netuid != source.Netuid || transaction.Schema != dishonestDepositTransactionV1 || transaction.DeploymentID != source.DeploymentID || transaction.NoID == 0 || transaction.AmountRao != strconv.FormatUint(a.cfg.Config.Scenarios.DishonestDepositRao, 10) || !strings.EqualFold(transaction.PolicyHash, source.PolicyHash) || len(dishonest.Validators) != source.ExpectedValidators {
		return nil, errors.New("terminal dishonest-deposit identity differs from the semantic campaign")
	}
	amount, err := finalSemanticCanonicalDecimal("dishonest deposit amount", transaction.AmountRao, true)
	if err != nil {
		return nil, err
	}
	nonce, err := finalSemanticCanonicalDecimal("dishonest deposit nonce", transaction.Nonce, false)
	if err != nil {
		return nil, err
	}
	if !common.IsHexAddress(transaction.Funder) || common.HexToAddress(transaction.Funder) == (common.Address{}) || requireFinalHex32("dishonest deposit transaction", transaction.TransactionHash) != nil || requireFinalHex32("dishonest deposit finalized block", transaction.FinalizedBlockHash) != nil || transaction.FinalizedBlock < source.EVMCampaignStartHead.Number || transaction.FinalizedBlock > source.EVMTerminalHead.Number {
		return nil, errors.New("dishonest deposit transaction boundary is invalid")
	}
	poolUID := uint16(0)
	depositSigner := ""
	for _, pool := range source.Pools {
		if pool.NoID == transaction.NoID {
			poolUID, depositSigner = pool.UID, pool.DepositSigner
			break
		}
	}
	if poolUID == 0 || !strings.EqualFold(depositSigner, transaction.Funder) {
		return nil, errors.New("dishonest deposit funder does not match the operator deposit signer")
	}
	seenValidators := map[int]bool{}
	unaffiliated := false
	var mismatchRequired *big.Int
	for _, validator := range dishonest.Validators {
		audit := validator.Audit
		required, requiredErr := finalSemanticCanonicalDecimal("dishonest required deposit", audit.RequiredDepositRao, true)
		if requiredErr != nil || validator.ValidatorID < 1 || validator.ValidatorID > source.ExpectedValidators || seenValidators[validator.ValidatorID] || validator.PoolUID != poolUID || validator.PoolPresent || validator.PoolWeight != 0 || audit.NoID != transaction.NoID || audit.Epoch != transaction.Epoch || audit.ObservedDepositRao != transaction.AmountRao || audit.Status != validatorpkg.DepositAuditMismatch || audit.Compliant || audit.Disposition != "zero_pool_weight" || required.Cmp(amount) <= 0 || validator.ApplicationBlock == 0 || validator.ApplicationBlock > source.NativeTerminalHead.Number || requireFinalHex32("dishonest validator vector", validator.VectorHash) != nil || requireFinalHex32("dishonest validator application block", validator.ApplicationBlockHash) != nil {
			return nil, fmt.Errorf("validator %d dishonest-deposit penalty evidence is invalid", validator.ValidatorID)
		}
		if mismatchRequired != nil && mismatchRequired.Cmp(required) != 0 {
			return nil, errors.New("dishonest-deposit validators disagree on required demand")
		}
		mismatchRequired = required
		seenValidators[validator.ValidatorID] = true
		unaffiliated = unaffiliated || !validator.PoolMasked
	}
	if len(seenValidators) != source.ExpectedValidators || !unaffiliated {
		return nil, errors.New("dishonest-deposit penalty lacks a complete validator census and an unaffiliated decision")
	}
	var underpayment *finalSemanticEvent
	for index := range events.byName["Deposit"] {
		event := &events.byName["Deposit"][index]
		if strings.EqualFold(event.Log.TransactionHash, transaction.TransactionHash) {
			if underpayment != nil {
				return nil, errors.New("dishonest deposit transaction emitted duplicate Deposit events")
			}
			underpayment = event
		}
	}
	if underpayment == nil || underpayment.Log.BlockNumber != transaction.FinalizedBlock || !strings.EqualFold(underpayment.Log.BlockHash, transaction.FinalizedBlockHash) || finalSemanticDepositEventIdentity(underpayment, source.Deployment.CoordinatorProxy, transaction.NoID, transaction.Epoch, amount, nonce, source.PolicyHash, transaction.Funder) != nil {
		return nil, errors.New("captured logs do not prove the exact dishonest underpayment")
	}
	underReceipt, err := a.receiptFromIndex(events, *underpayment, "dishonest-deposit-underpayment")
	if err != nil {
		return nil, err
	}
	var recoveryEpoch, recoveryObservedAt uint64
	var recoveryAmount string
	var recoveryReceipt FinalEVMReceipt
	positiveRecoveryWeight := false
	for _, validator := range source.Validators {
		var candidate *FinalPoolWeightEvidence
		var candidateEpoch uint64
		for cycleIndex := range validator.Cycles {
			cycle := &validator.Cycles[cycleIndex]
			if cycle.SettlementEpoch <= transaction.Epoch {
				continue
			}
			for poolIndex := range cycle.Pools {
				pool := &cycle.Pools[poolIndex]
				if pool.NoID != transaction.NoID || pool.AuditStatus != validatorpkg.DepositAuditCompliant || !pool.AuditCompliant || pool.AuditDisposition != "pool_weight_eligible" || pool.AuditError != "" || pool.RequiredDepositRao != pool.ObservedDepositRao {
					continue
				}
				if candidate == nil || cycle.SettlementEpoch < candidateEpoch {
					candidate, candidateEpoch = pool, cycle.SettlementEpoch
				}
			}
		}
		if candidate == nil {
			return nil, fmt.Errorf("validator %d has no compliant post-penalty recovery audit", validator.ValidatorID)
		}
		if _, err := finalSemanticCanonicalDecimal("recovery deposit", candidate.ObservedDepositRao, true); err != nil {
			return nil, err
		}
		if candidate.ObservedAtBlock < candidate.DepositReceipt.Block.Number {
			return nil, fmt.Errorf("validator %d recovery audit predates its deposit receipt", validator.ValidatorID)
		}
		if recoveryEpoch == 0 {
			recoveryEpoch, recoveryObservedAt, recoveryAmount, recoveryReceipt = candidateEpoch, candidate.ObservedAtBlock, candidate.ObservedDepositRao, candidate.DepositReceipt
		} else if candidateEpoch != recoveryEpoch || candidate.ObservedDepositRao != recoveryAmount || candidate.DepositReceipt.TransactionHash != recoveryReceipt.TransactionHash || candidate.DepositReceipt.Block != recoveryReceipt.Block || candidate.DepositReceipt.LogsHash != recoveryReceipt.LogsHash {
			return nil, errors.New("validators disagree on the operator recovery deposit")
		}
		positiveRecoveryWeight = positiveRecoveryWeight || candidate.AppliedWeight != 0
	}
	if recoveryEpoch <= transaction.Epoch || recoveryReceipt.TransactionHash == "" || strings.EqualFold(recoveryReceipt.TransactionHash, transaction.TransactionHash) || recoveryReceipt.Block.Number <= transaction.FinalizedBlock || !positiveRecoveryWeight {
		return nil, errors.New("operator recovery did not restore a later positive validator weight")
	}
	reconstructedRecovery, err := a.depositReceipt(events, validatorpkg.DepositAudit{NoID: transaction.NoID, Epoch: recoveryEpoch, ObservedDepositRao: recoveryAmount, ObservedAtBlock: recoveryObservedAt}, "dishonest-deposit-recovery")
	if err != nil || reconstructedRecovery.TransactionHash != recoveryReceipt.TransactionHash || reconstructedRecovery.Block != recoveryReceipt.Block || reconstructedRecovery.LogsHash != recoveryReceipt.LogsHash {
		return nil, stateMismatchError(err, "validator recovery receipt does not prove the exact compliant deposit prefix")
	}
	recoveryReceipt = reconstructedRecovery
	var recoveryEvent *finalSemanticEvent
	for index := range events.byName["Deposit"] {
		event := &events.byName["Deposit"][index]
		if !strings.EqualFold(event.Log.TransactionHash, recoveryReceipt.TransactionHash) {
			continue
		}
		if recoveryEvent != nil {
			return nil, errors.New("operator recovery transaction emitted duplicate Deposit events")
		}
		recoveryEvent = event
	}
	if recoveryEvent == nil || recoveryEvent.Log.BlockNumber != recoveryReceipt.Block.Number || !strings.EqualFold(recoveryEvent.Log.BlockHash, recoveryReceipt.Block.Hash) || finalSemanticDepositEventIdentity(recoveryEvent, source.Deployment.CoordinatorProxy, transaction.NoID, recoveryEpoch, nil, nil, source.PolicyHash, transaction.Funder) != nil {
		return nil, errors.New("captured logs do not bind the validators' recovery receipt")
	}
	receipts := []FinalEVMReceipt{underReceipt, recoveryReceipt}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].TransactionHash < receipts[j].TransactionHash })
	return receipts, nil
}

func finalSemanticMetricAssertions(values map[string]uint64) []FinalMetricAssertion {
	metrics := make([]string, 0, len(values))
	for metric := range values {
		metrics = append(metrics, metric)
	}
	sort.Strings(metrics)
	result := make([]FinalMetricAssertion, 0, len(metrics))
	for _, metric := range metrics {
		result = append(result, FinalMetricAssertion{Metric: metric, Expected: values[metric], Observed: values[metric]})
	}
	return result
}

func (a *finalSemanticArchive) buildExitCriteria(source *FinalSemanticEvidence, result *ScenarioResult, terminal *ScenarioObservation, events *finalSemanticEventIndex) error {
	if source == nil || result == nil || terminal == nil || events == nil || result.Result != "pass" || result.FailedAssertionCount != 0 {
		return errors.New("exit-criterion construction requires a passing closed scenario")
	}
	depositAudits := uint64(0)
	verifiedVectors := uint64(0)
	for _, validator := range source.Validators {
		for _, cycle := range validator.Cycles {
			depositAudits += uint64(len(cycle.Pools))
			verifiedVectors++
		}
	}
	invalidMerkle, invalidFound := finalSemanticAdversaryMetric(result, "live_invalid_merkle_proof_rejections")
	merkleMutations, mutationsFound := finalSemanticAdversaryMetric(result, "live_merkle_state_mutations")
	doubleClaims, doubleFound := finalSemanticAdversaryMetric(result, "double_claim_rejects")
	if !invalidFound || !mutationsFound || invalidMerkle.Minimum < 1 || merkleMutations.Maximum != 0 || !doubleFound || doubleClaims.Minimum < 1 {
		return errors.New("closed adversary evidence does not prove Merkle and double-claim rejection")
	}
	governanceAssertionPresent := false
	for _, assertion := range result.Assertions {
		if assertion.ID == "governance_adversarial_drill_complete" {
			governanceAssertionPresent = true
			break
		}
	}
	if !finalSemanticAdversaryVectorPassed(result, "malicious-upgrade-pause-role-compromise") || (governanceAssertionPresent && !finalSemanticAssertionPassed(result, "governance_adversarial_drill_complete")) || terminal.GovernanceDrill == nil || terminal.GovernanceDrill.Stage != "complete" || terminal.GovernanceDrill.After == nil {
		return errors.New("closed governance evidence does not prove unauthorized-upgrade rejection and restoration")
	}
	for name, succeeded := range terminal.GovernanceDrill.ProbeResults {
		if name == "" || succeeded {
			return errors.New("governance custody probe unexpectedly succeeded")
		}
	}
	if len(terminal.GovernanceDrill.ProbeResults) != 4 {
		return errors.New("governance custody probe census is incomplete")
	}
	anomalies := finalSemanticProcessAnomalyCount(result, terminal, source.Topology.ProcessRestarts)
	if anomalies != 0 {
		return fmt.Errorf("closed campaign contains %d process/log anomalies", anomalies)
	}
	assertions := map[string]map[string]uint64{
		"all-miner-tier-assignments": {
			"active_fleet_member_bindings": uint64(finalHeadCandidateCount * 4), "miner_tier_assignments": uint64(source.ExpectedMiners), "pool_tail_assignments": uint64(source.ExpectedMiners - finalHeadCandidateCount*4),
		},
		"deposit-conviction-receipts":   {"operator_epoch_deposit_audits": depositAudits, "operator_conviction_receipts": uint64(len(source.Pools))},
		"invalid-merkle-proof-rejected": {"invalid_merkle_attempts_rejected": 1},
		"no-process-log-anomalies":      {"error_warning_panic_restart_anomalies": 0},
		"payout-double-claim-rejected":  {"double_claim_attempts_rejected": 1},
		"reserve-one-way-backed":        {"reserve_backing_violations": 0},
		"theta-head-tail-realized":      {"verified_theta_weight_vectors": verifiedVectors},
		"unauthorized-upgrade-rejected": {"unauthorized_upgrade_attempts_rejected": 1},
	}
	if source.Phase == "production-soak" {
		if terminal.DishonestDeposit == nil || !terminal.DishonestDepositValid || len(terminal.DishonestDeposit.Validators) != source.ExpectedValidators || !finalSemanticAssertionPassed(result, "dishonest_operator_deposit_penalized_and_recovered") {
			return errors.New("closed production evidence does not prove dishonest-deposit penalty and recovery")
		}
		assertions["dishonest-deposit-recovery"] = map[string]uint64{"dishonest_underpayments_succeeded": 1, "recovery_topups_succeeded": 1}
	}
	for _, id := range finalRequiredExitCriteriaForPhase(source.Phase) {
		values := assertions[id]
		if len(values) == 0 {
			return fmt.Errorf("exit criterion %s has no typed assertion source", id)
		}
		evidenceObject := map[string]any{
			"criterion": id, "result_hash": result.EvidenceHash, "terminal_observation_hash": terminal.ObservationHash,
			"assertions": values, "checkpoint": source.EVMTerminalHead,
		}
		switch id {
		case "all-miner-tier-assignments":
			evidenceObject["miner_manifest"] = source.Topology.MinerManifest
			evidenceObject["binding_manifest"] = source.Topology.BindingManifest
		case "deposit-conviction-receipts":
			evidenceObject["pools"] = source.Pools
			evidenceObject["validators"] = source.Validators
		case "invalid-merkle-proof-rejected":
			evidenceObject["rejections"] = invalidMerkle
			evidenceObject["mutations"] = merkleMutations
		case "no-process-log-anomalies":
			evidenceObject["anomaly_ledger"] = result.Anomalies
			evidenceObject["process_findings"] = terminal.ProcessLogFindings
		case "payout-double-claim-rejected":
			evidenceObject["double_claim_rejects"] = doubleClaims
		case "reserve-one-way-backed":
			evidenceObject["reserve"] = source.Reserve
		case "theta-head-tail-realized":
			evidenceObject["validators"] = source.Validators
		case "unauthorized-upgrade-rejected":
			evidenceObject["governance"] = terminal.GovernanceDrill
		case "dishonest-deposit-recovery":
			evidenceObject["dishonest_deposit"] = source.DishonestDeposit
		}
		artifact, err := a.derived("exit-criterion", "exit-criterion-"+id+".json", evidenceObject)
		if err != nil {
			return err
		}
		criterion := FinalExitCriterionEvidence{ID: id, Expected: "release invariant holds", Observed: "release invariant observed", Passed: true, Checkpoint: source.EVMTerminalHead, Assertions: finalSemanticMetricAssertions(values), Artifacts: []FinalArtifactLocator{artifact}, PublicRequestHashes: []string{}}
		if id == "dishonest-deposit-recovery" {
			if source.DishonestDeposit == nil {
				return errors.New("typed dishonest-deposit evidence is absent")
			}
			criterion.EVMReceipts = []FinalEVMReceipt{source.DishonestDeposit.UnderpaymentReceipt, source.DishonestDeposit.RecoveryDepositReceipt}
		}
		source.ExitCriteria = append(source.ExitCriteria, criterion)
	}
	return nil
}
