//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

const finalValidatorReplayV2Schema = "urnetwork-final-validator-replay-v2"

// The main final object contains only small manifest locators. Every source
// object remains separately bounded and streamed through its exact provenance.
type FinalValidatorReplayV2 struct {
	ValidatorID uint64                 `json:"validator_id"`
	Manifest    FinalArtifactLocator   `json:"manifest"`
	Coverage    *FinalNativeCoverageV2 `json:"native_coverage"`
}

type finalValidatorReplayManifestV2 struct {
	Schema           string                            `json:"schema"`
	DeploymentID     string                            `json:"deployment_id"`
	PlanHash         string                            `json:"plan_hash"`
	ValidatorID      uint64                            `json:"validator_id"`
	Capture          FinalCollectedValidatorEvidenceV2 `json:"capture"`
	SourcePlan       FinalArtifactLocator              `json:"original_source_plan"`
	ActiveSourcePlan *FinalArtifactLocator             `json:"active_source_plan,omitempty"`
	RuntimeInventory FinalArtifactLocator              `json:"runtime_inventory"`
	PublicDeployment FinalArtifactLocator              `json:"public_deployment"`
	PublicIdentities FinalArtifactLocator              `json:"public_identities"`
}

type finalValidatorReplayOwnerV2 struct {
	ctx           context.Context
	archive       *validatorpkg.ReleaseEvidenceV2Archive
	config        *validatorpkg.ReleaseConfig
	paths         *finalOperatorPathAuthority
	manifest      finalValidatorReplayManifestV2
	observations  []validatorpkg.ReleaseEvidenceV2DecisionObservation
	intents       []validatorpkg.SteeringIntent
	coverage      *FinalNativeCoverageV2
	nativeRewards map[ChainHead]*NativeRewardObservation
	root          string
	anchor        os.FileInfo
}

func (self *finalValidatorReplayOwnerV2) Close() error {
	if self == nil {
		return nil
	}
	var err error
	if self.archive != nil {
		err = self.archive.Close()
	}
	if self.root != "" {
		current, statErr := os.Lstat(self.root)
		if statErr != nil || self.anchor == nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(self.anchor, current) {
			return errors.Join(err, errors.New("final V2 scratch identity changed before retirement"), statErr)
		}
		err = errors.Join(err, os.RemoveAll(self.root))
		self.root = ""
	}
	return err
}

func closeFinalValidatorReplayOwnersV2(owners map[uint64]*finalValidatorReplayOwnerV2) error {
	ids := make([]uint64, 0, len(owners))
	for id := range owners {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var err error
	for _, id := range ids {
		err = errors.Join(err, owners[id].Close())
	}
	return err
}

func verifyFinalValidatorReplayShapeV2(evidence *FinalSemanticEvidence) error {
	if len(evidence.ValidatorReplayV2) == 0 {
		return nil
	}
	if len(evidence.ValidatorReplayV2) != evidence.ExpectedValidators {
		return errors.New("final V2 replay must cover every validator")
	}
	for index, entry := range evidence.ValidatorReplayV2 {
		if entry.ValidatorID != uint64(index+1) || entry.Coverage == nil {
			return errors.New("final V2 replay validator census differs")
		}
		if err := verifyFinalArtifact("final validator replay", entry.Manifest, "validator-replay-v2"); err != nil {
			return err
		}
	}
	return nil
}

func openFinalValidatorReplayOwnersV2(ctx context.Context, evidence *FinalSemanticEvidence, load FinalArtifactLoader) (owners map[uint64]*finalValidatorReplayOwnerV2, resultErr error) {
	if err := verifyFinalValidatorReplayShapeV2(evidence); err != nil {
		return nil, err
	}
	owners = map[uint64]*finalValidatorReplayOwnerV2{}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, closeFinalValidatorReplayOwnersV2(owners))
			owners = nil
		}
	}()
	for _, entry := range evidence.ValidatorReplayV2 {
		owner, err := openFinalValidatorReplayV2(ctx, evidence, entry, load)
		if err != nil {
			return owners, err
		}
		owners[entry.ValidatorID] = owner
	}
	return owners, nil
}

func loadFinalV2Source(ctx context.Context, load FinalArtifactLoader, locator FinalArtifactLocator, limit uint64) ([]byte, error) {
	if ctx == nil || load == nil || limit == 0 || locator.SizeBytes > limit {
		return nil, errors.New("final V2 source exceeds its independently selected byte bound")
	}
	if err := verifyFinalArtifact("final V2 source", locator, locator.Kind); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := load(ctx, locator)
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) != locator.SizeBytes || bytesSHA256(raw) != locator.ContentHash {
		return nil, errors.New("final V2 source changed its exact content address")
	}
	return raw, ctx.Err()
}

// Initial replay and final rehash share the same independently owned capacity.
// Only the exact journal source and content-addressed path get its larger bound.
func loadFinalCapturedSourceV2(ctx context.Context, load FinalArtifactLoader, source FinalCollectedValidatorSourceV2, intentLimit uint64) ([]byte, error) {
	if err := validateFinalJournalCaptureSourceV2(source); err != nil {
		return nil, err
	}
	maximum := max(maximumCampaignEvidenceRawFileBytes, intentLimit)
	if finalJournalCapturePathV2(source.Artifact.URI) {
		maximum = maximumFinalJournalBytes
	}
	return loadFinalV2Source(ctx, load, source.Artifact, maximum)
}

// Re-read each original object before successful replay can return. Earlier
// reads or artifact cache hits never replace this exact content-address check.
func rehashFinalCapturedSourcesV2(ctx context.Context, load FinalArtifactLoader, sources []FinalCollectedValidatorSourceV2, intentLimit uint64) error {
	for _, source := range sources {
		if _, err := loadFinalCapturedSourceV2(ctx, load, source, intentLimit); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func openFinalValidatorReplayV2(ctx context.Context, evidence *FinalSemanticEvidence, entry FinalValidatorReplayV2, load FinalArtifactLoader) (result *finalValidatorReplayOwnerV2, resultErr error) {
	if ctx == nil || evidence == nil || entry.ValidatorID == 0 || entry.ValidatorID > uint64(evidence.ExpectedValidators) || entry.Manifest.Kind != "validator-replay-v2" {
		return nil, errors.New("final V2 replay owner is incomplete")
	}
	raw, err := loadFinalV2Source(ctx, load, entry.Manifest, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	owner := &finalValidatorReplayOwnerV2{ctx: ctx}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owner.Close())
			result = nil
		}
	}()
	if err := decodeStrictJSONBytes(raw, &owner.manifest); err != nil {
		return nil, err
	}
	manifest := &owner.manifest
	if manifest.Schema != finalValidatorReplayV2Schema || manifest.DeploymentID != evidence.DeploymentID || manifest.PlanHash != evidence.PlanHash || manifest.ValidatorID != entry.ValidatorID || manifest.Capture.Schema != finalCollectedValidatorEvidenceV2Schema || manifest.Capture.SemanticStatus != "pending_offline_verification" {
		return nil, errors.New("final V2 manifest differs from its containing authority")
	}
	readControl := func(locator FinalArtifactLocator) ([]byte, error) {
		return loadFinalV2Source(ctx, load, locator, maximumCampaignEvidenceRawFileBytes)
	}
	planBytes, err := loadFinalV2Source(ctx, load, evidence.PlanArtifact, maximumSetupPlanFileBytes)
	if err != nil {
		return nil, err
	}
	current, err := decodeFinalHistoricalPlanBytes(planBytes)
	if err != nil {
		return nil, err
	}
	sourceBytes, err := loadFinalV2Source(ctx, load, manifest.SourcePlan, maximumSetupPlanFileBytes)
	if err != nil {
		return nil, err
	}
	source, err := decodeFinalHistoricalPlanBytes(sourceBytes)
	if err != nil {
		return nil, err
	}
	controls := make(map[string][]byte)
	for name, locator := range map[string]FinalArtifactLocator{"inventory": manifest.RuntimeInventory, "deployment": manifest.PublicDeployment, "identities": manifest.PublicIdentities, "policy": evidence.PolicyArtifact, "release": evidence.ReleaseLockArtifact} {
		controls[name], err = readControl(locator)
		if err != nil {
			return nil, err
		}
	}
	census := map[validatorpkg.ReleaseEvidenceV2CaptureSource]FinalArtifactLocator{}
	for index, source := range manifest.Capture.Sources {
		if index > 0 && finalCaptureSourceV2Key(source.Source) <= finalCaptureSourceV2Key(manifest.Capture.Sources[index-1].Source) || source.Artifact.Kind != "validator-evidence-v2-source" {
			return nil, errors.New("final V2 source census is not complete and canonical")
		}
		if err := validateFinalJournalCaptureSourceV2(source); err != nil {
			return nil, err
		}
		census[source.Source] = source.Artifact
	}
	readNamed := func(kind, name string, limit uint64) ([]byte, error) {
		locator, found := census[validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: kind, Name: name}]
		if !found {
			return nil, fmt.Errorf("final V2 original %s/%s source is missing", kind, name)
		}
		return loadFinalV2Source(ctx, load, locator, limit)
	}
	authorityBytes, err := readNamed("setup", "simulator-authority", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	runtimeBytes, err := readNamed("setup", "runtime-config", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	var active []finalValidatorGenerationReplayV2
	if current.EvidenceRelayContinuation != nil && current.EvidenceRelayContinuation.ActiveGeneration != nil {
		if manifest.ActiveSourcePlan == nil {
			return nil, errors.New("final active generation source plan is missing")
		}
		raw, err := loadFinalV2Source(ctx, load, *manifest.ActiveSourcePlan, maximumSetupPlanFileBytes)
		if err != nil {
			return nil, err
		}
		activeSource, err := decodeFinalHistoricalPlanBytes(raw)
		if err != nil {
			return nil, err
		}
		journal, err := readNamed("relay-journal", "journal.jsonl", maximumFinalJournalBytes)
		if err != nil {
			return nil, err
		}
		entries, err := decodeFinalSemanticJournalBytes(journal)
		if err != nil {
			return nil, err
		}
		active = append(active, finalValidatorGenerationReplayV2{source: activeSource, entries: entries, read: func(name string, maximum uint64) ([]byte, error) { return readNamed("setup", name, maximum) }})
	} else if manifest.ActiveSourcePlan != nil {
		return nil, errors.New("final original generation has an unexpected source branch")
	}
	release, authority, err := finalValidatorConfigAuthorityV2(ctx, evidence, current, source, authorityBytes, runtimeBytes, controls["inventory"], controls["deployment"], controls["identities"], controls["policy"], controls["release"], entry.ValidatorID, active...)
	if err != nil {
		return nil, err
	}
	owner.config = release
	var adoption *validatorpkg.ReleaseHistoryAdoptionV2
	if locator, found := census[validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "strict-history-adoption"}]; found {
		raw, err := loadFinalV2Source(ctx, load, locator, strictHistoryAdoptionMaximumBytes)
		if err != nil {
			return nil, err
		}
		adoption, err = validatorpkg.DecodeReleaseHistoryAdoptionV2(raw, locator.ContentHash)
		if err != nil {
			return nil, err
		}
		if err := verifyFinalHistoryAdoptionV2(current, authority.StateRoot, release, runtimeBytes, adoption); err != nil {
			return nil, err
		}
		release.StateDir = adoption.CoordinatorStateDir
	}
	if err := finalValidatorAdoptionSourceV2(source, active, adoption); err != nil {
		return nil, err
	}
	hotkey, err := decodeHex32("final V2 original validator hotkey", manifest.Capture.Hotkey)
	if err != nil {
		return nil, err
	}
	paths, err := decodeFinalOperatorPathAuthority(controls["identities"], evidence.DeploymentID, evidence.ExpectedValidators, evidence.ExpectedOperators)
	if err != nil {
		return nil, err
	}
	paths, err = finalGenerationPathAuthorityV2(paths, authority)
	if err != nil {
		return nil, err
	}
	owner.paths = paths
	if manifest.Capture.Hotkey != paths.identities.Substrate[validatorHotkeyLabel(int(entry.ValidatorID))].PublicKey {
		return nil, errors.New("final V2 hotkey differs from original public custody")
	}
	limits, err := campaignValidatorLimitsV2(release.EvidenceV2.Bounds, uint64(len(release.EvidenceV2.Operators)), authority.Config.ValidatorEvidenceRelay.MaxSlots)
	if err != nil {
		return nil, err
	}
	owner.root, err = os.MkdirTemp("", "sn-final-validator-v2-")
	if err != nil {
		return nil, err
	}
	owner.anchor, err = os.Lstat(owner.root)
	if err != nil {
		return nil, err
	}
	sources := make([]validatorpkg.ReleaseEvidenceV2ArchiveSource, 0, len(census))
	for _, source := range manifest.Capture.Sources {
		sources = append(sources, validatorpkg.ReleaseEvidenceV2ArchiveSource{Source: source.Source, SizeBytes: source.Artifact.SizeBytes, ContentHash: source.Artifact.ContentHash})
	}
	owner.archive, err = validatorpkg.OpenReleaseEvidenceV2Archive(ctx, validatorpkg.ReleaseEvidenceV2ArchiveOptions{Config: release, Hotkey: hotkey, Origins: manifest.Capture.Origins, Sources: sources, ReadSource: func(ctx context.Context, source validatorpkg.ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		locator, found := census[source]
		if !found {
			return nil, errors.New("final V2 source escaped its closed census")
		}
		return loadFinalCapturedSourceV2(ctx, load, FinalCollectedValidatorSourceV2{Source: source, Artifact: locator}, release.EvidenceV2.Bounds.IntentFileLimit())
	}, ScratchRoot: owner.root, MaximumBytes: limits.dataBytes + limits.controlBytes, MaximumObjects: limits.maximumObjects, Adoption: adoption})
	if err != nil {
		return nil, err
	}
	store, err := readNamed("private", "steering-intents.json", release.EvidenceV2.Bounds.IntentFileLimit())
	if err != nil {
		return nil, err
	}
	owner.intents, err = decodeValidatorIntentBytes(store, int(entry.ValidatorID))
	if err != nil {
		return nil, err
	}
	for _, intent := range owner.intents {
		raw, err := readNamed("decision-observation", intent.MeasurementArtifactHash, release.EvidenceV2.Bounds.MaxControlBytes)
		if err != nil {
			return nil, err
		}
		var observation validatorpkg.ReleaseEvidenceV2DecisionObservation
		if err := decodeStrictJSONBytes(raw, &observation); err != nil {
			return nil, err
		}
		owner.observations = append(owner.observations, observation)
	}
	if err := owner.archive.ReplayDecisions(ctx, owner.observations); err != nil {
		return nil, err
	}
	if err := owner.archive.ReplayPublicationsV2(ctx, evidence.Window.FirstEpoch, evidence.Window.EpochCount, evidence.Window.StartBlock, evidence.Window.EpochBlocks, evidence.EVMTerminalHead.Number); err != nil {
		return nil, err
	}
	for _, checkpoint := range manifest.Capture.NativeCheckpoints {
		raw, err := readNamed("native-coverage", fmt.Sprintf("evm-%020d", checkpoint.Mapping.Query.EVMNumber), release.EvidenceV2.Bounds.MaxControlBytes)
		if err != nil {
			return nil, err
		}
		var original FinalNativeCheckpointV2
		if err := decodeStrictJSONBytes(raw, &original); err != nil || !finalJSONEqual(original, checkpoint) {
			return nil, errors.Join(errors.New("native coverage checkpoint differs from its original captured source"), err)
		}
	}
	owner.nativeRewards = map[ChainHead]*NativeRewardObservation{}
	if len(manifest.Capture.NativeCheckpoints) < 2 {
		return nil, errors.New("native payout capture has no complete endpoints")
	}
	for _, checkpoint := range manifest.Capture.NativeCheckpoints[1:] {
		for _, head := range []ChainHead{checkpoint.PayoutParent, checkpoint.PayoutHead} {
			if owner.nativeRewards[head] != nil {
				continue
			}
			raw, err := readNamed("native-reward", fmt.Sprintf("native-%020d", head.Number), release.EvidenceV2.Bounds.MaxControlBytes)
			if err != nil {
				return nil, err
			}
			var original NativeRewardObservation
			if err := decodeStrictJSONBytes(raw, &original); err != nil || original.FinalizedHead != head {
				return nil, errors.Join(errors.New("native payout observation differs from its exact source checkpoint"), err)
			}
			owner.nativeRewards[head] = &original
		}
	}
	owner.coverage, err = deriveFinalNativeCoverageV2(owner.intents, owner.observations, manifest.Capture.NativeCheckpoints)
	if err != nil {
		return nil, err
	}
	var rewardHeads []ChainHead
	for head := range owner.nativeRewards {
		rewardHeads = append(rewardHeads, head)
	}
	sort.Slice(rewardHeads, func(i, j int) bool { return rewardHeads[i].Number < rewardHeads[j].Number })
	for _, head := range rewardHeads {
		raw, err := readNamed("native-reward-mapping", fmt.Sprintf("native-%020d", head.Number), release.EvidenceV2.Bounds.MaxControlBytes)
		if err != nil {
			return nil, err
		}
		var mapping crv4.EVMCheckpointObservation
		if err := decodeStrictJSONBytes(raw, &mapping); err != nil || mapping.Query.NativeNumber != head.Number || mapping.Query.NativeHash.Hex() != head.Hash {
			return nil, errors.Join(errors.New("native reward mapping differs from its original payout source"), err)
		}
		owner.coverage.RewardMappings = append(owner.coverage.RewardMappings, mapping)
	}
	if entry.Coverage != nil && !finalJSONEqual(entry.Coverage, owner.coverage) {
		return nil, errors.New("final native coverage differs from complete original signed history")
	}
	// Retained source classes also contain relay/native request bytes that
	// replay does not use as a verdict. Hash every original object before any
	// successful artifact cache hit; stream one object at a time.
	if err := rehashFinalCapturedSourcesV2(ctx, load, manifest.Capture.Sources, release.EvidenceV2.Bounds.IntentFileLimit()); err != nil {
		return nil, err
	}
	return owner, nil
}

func (a *finalSemanticArchive) buildValidatorReplayV2(evidence *FinalSemanticEvidence) error {
	if len(a.collected.Validators) == 0 || a.collected.Validators[0].EvidenceV2 == nil {
		return nil
	}
	shared := map[string]FinalArtifactLocator{}
	for name, kind := range map[string]string{"launch-foundation/runtime-config-manifest.json": "validator-runtime-inventory-v2", "launch-foundation/public.json": "validator-public-deployment-v2", "public/identities.json": "validator-public-identities-v2"} {
		raw, _, err := a.file(name)
		if err != nil {
			return err
		}
		locator, err := a.derivedBytes(kind, filepath.Base(name)+".v2", raw)
		if err != nil {
			return err
		}
		shared[name] = locator
	}
	a.validatorReplayV2 = map[uint64]*finalValidatorReplayOwnerV2{}
	for _, collected := range a.collected.Validators {
		if collected.EvidenceV2 == nil {
			return errors.New("final validator census mixes V2 and legacy campaign authorities")
		}
		var authority finalValidatorAuthorityV2
		found := false
		for _, source := range collected.EvidenceV2.Sources {
			if source.Source == (validatorpkg.ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "simulator-authority"}) {
				raw, err := a.loadChecked(source.Artifact)
				if err != nil {
					return err
				}
				if err := decodeStrictJSONBytes(raw, &authority); err != nil {
					return err
				}
				found = true
			}
		}
		if !found {
			return errors.New("final V2 capture lacks approved source authority")
		}
		var prepared runtimeEvidenceActivationPreparedV2
		if err := decodeStrictJSONBytes(authority.Prepared, &prepared); err != nil {
			return err
		}
		path := "launch-foundation/plan.json"
		if prepared.PlanHash != evidence.PlanHash {
			path = "plan-history/" + stringsTrim0x(prepared.PlanHash) + ".json"
		}
		raw, _, err := a.file(path)
		if err != nil {
			return err
		}
		sourcePlan, err := a.derivedBytes("validator-activation-source-plan-v2", "validator-activation-plan-"+stringsTrim0x(prepared.PlanHash)+".json", raw)
		if err != nil {
			return err
		}
		manifest := finalValidatorReplayManifestV2{Schema: finalValidatorReplayV2Schema, DeploymentID: evidence.DeploymentID, PlanHash: evidence.PlanHash, ValidatorID: collected.ValidatorID, Capture: *collected.EvidenceV2, SourcePlan: sourcePlan, RuntimeInventory: shared["launch-foundation/runtime-config-manifest.json"], PublicDeployment: shared["launch-foundation/public.json"], PublicIdentities: shared["public/identities.json"]}
		if authority.ActiveGeneration != nil {
			var handoff policyRolloverHandoffV2
			if err := decodeStrictJSONBytes(authority.ActiveGeneration.Handoff, &handoff); err != nil {
				return err
			}
			if !validCanonicalHashHex(handoff.SourcePlanHash) {
				return errors.New("final active generation source hash is invalid")
			}
			raw, _, err := a.file("plan-history/" + stringsTrim0x(handoff.SourcePlanHash) + ".json")
			if err != nil {
				return err
			}
			locator, err := a.derivedBytes("validator-activation-source-plan-v2", "validator-activation-plan-"+stringsTrim0x(handoff.SourcePlanHash)+".json", raw)
			if err != nil {
				return err
			}
			manifest.ActiveSourcePlan = &locator
		}
		locator, err := a.derived("validator-replay-v2", fmt.Sprintf("validator-%d-replay-v2.json", collected.ValidatorID), manifest)
		if err != nil {
			return err
		}
		entry := FinalValidatorReplayV2{ValidatorID: collected.ValidatorID, Manifest: locator}
		owner, err := openFinalValidatorReplayV2(a.ctx, evidence, entry, a.load)
		if err != nil {
			return err
		}
		entry.Coverage = owner.coverage
		a.validatorReplayV2[collected.ValidatorID] = owner
		evidence.ValidatorReplayV2 = append(evidence.ValidatorReplayV2, entry)
	}
	return nil
}

func finalDecodeMeasurementWithReplayV2(raw []byte, owners map[uint64]*finalValidatorReplayOwnerV2) (*validatorpkg.ReleaseMeasurementArtifact, *validatorpkg.VerifiedReleaseMeasurement, error) {
	var routing struct {
		Schema      string `json:"schema"`
		ValidatorID uint64 `json:"validator_id"`
	}
	if err := json.Unmarshal(raw, &routing); err != nil {
		return nil, nil, err
	}
	if routing.Schema != validatorpkg.ReleaseMeasurementSchemaV2 {
		return validatorpkg.DecodeReleaseMeasurementArtifact(raw)
	}
	owner := owners[routing.ValidatorID]
	if owner == nil {
		return nil, nil, errors.New("V2 measurement has no independently replayed original source owner")
	}
	return owner.archive.Measurement(validatorpkg.ReleaseMeasurementContentHash(raw))
}

func finalOriginalMeasurementV2(owner *finalValidatorReplayOwnerV2, intent *validatorpkg.SteeringIntent, measurement, envelope []byte) (*validatorpkg.ReleaseMeasurementArtifact, *validatorpkg.VerifiedReleaseMeasurement, string, error) {
	if owner == nil || intent == nil {
		return nil, nil, "", errors.New("final V2 original decision owner is absent")
	}
	matched := 0
	for index := range owner.intents {
		if owner.intents[index].VectorHash == intent.VectorHash {
			if !finalJSONEqual(owner.intents[index], *intent) {
				return nil, nil, "", errors.New("final V2 selected intent differs from its original complete store")
			}
			matched++
		}
	}
	if matched != 1 || bytesSHA256(measurement) != intent.MeasurementArtifactHash || uint64(len(measurement)) != intent.MeasurementArtifactSize || bytesSHA256(envelope) != intent.MeasurementEnvelopeHash || uint64(len(envelope)) != intent.MeasurementEnvelopeSize {
		return nil, nil, "", errors.New("final V2 selected decision bytes differ from original history")
	}
	control, err := validatorpkg.DecodeReleaseMeasurementEnvelopeV2(owner.archiveContext(), envelope, owner.config.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil {
		return nil, nil, "", err
	}
	artifact, verified, err := owner.archive.Measurement(intent.MeasurementArtifactHash)
	if err != nil {
		return nil, nil, "", err
	}
	return artifact, verified, control.SignedAt, nil
}

// The source operation owns this context until all joined verification tasks
// complete; it is never replaced with a background context for replay.
func (self *finalValidatorReplayOwnerV2) archiveContext() context.Context { return self.ctx }

func verifyFinalMeasurementLineageWithReplayV2(owner *finalValidatorReplayOwnerV2, previousHash, currentHash string, legacy func() error) error {
	if owner == nil {
		return legacy()
	}
	previous, current := -1, -1
	for index, intent := range owner.intents {
		if intent.MeasurementArtifactHash == previousHash {
			previous = index
		}
		if intent.MeasurementArtifactHash == currentHash {
			current = index
		}
	}
	if previous < 0 || current <= previous {
		return errors.New("final V2 selected lineage differs from original authenticated history order")
	}
	if _, _, err := owner.archive.Measurement(previousHash); err != nil {
		return err
	}
	if _, _, err := owner.archive.Measurement(currentHash); err != nil {
		return err
	}
	return owner.ctx.Err()
}

// Approval/custody validation precedes this routing check. An active request
// belongs to its separately verified source plan, never the initial activation.
func finalValidatorAdoptionSourceV2(original *SetupPlan, active []finalValidatorGenerationReplayV2, adoption *validatorpkg.ReleaseHistoryAdoptionV2) error {
	if original == nil || len(active) > 1 {
		return errors.New("final history source owner is invalid")
	}
	expected := original.PlanHash
	if len(active) == 1 {
		if active[0].source == nil || adoption == nil {
			return errors.New("final active generation lacks its exact strict history adoption")
		}
		expected = active[0].source.PlanHash
	}
	if adoption != nil && adoption.SourcePlanHash != expected {
		return errors.New("final V2 adoption selects another original activation source")
	}
	return nil
}
