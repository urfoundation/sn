//go:build linux || darwin

// Synthetic signed approvals force the obsolete-config boundary and the
// receipt-before-pointer crash without changing any live identity or state.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	validatorcomponent "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

// The old timeout is historical data only; all current loaders stay strict.
func campaignConfigMigrationOldTestConfig(cfg *ResolvedConfig) {
	cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds = 10_000
	cfg.Config.Scenarios.Adversaries.MaximumP99LatencyMilliseconds = 15_000
}

// Archive genuine old and new plans while retaining byte-identical old yaml.
func prepareCampaignConfigMigrationTest(t *testing.T, f *runtimeEvidenceProvisionV2TestFixture) (*ResolvedConfig, *campaignConfigMigrationRequest, *SetupPlan, []byte, []byte) {
	t.Helper()
	sourceBytes, err := json.Marshal(f.plan)
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes, err = archiveReviewedSetupPlanBytes(f.stateDir, f.plan.PlanHash, sourceBytes)
	if err != nil {
		t.Fatal(err)
	}
	oldYaml, err := yaml.Marshal(f.cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	oldYaml = append([]byte("# synthetic archived campaign config\n"), oldYaml...)
	newConfig, config := *f.cfg, *f.cfg.Config
	config.Scenarios.Adversaries.RequestTimeoutMilliseconds = 60_000
	config.Scenarios.Adversaries.MaximumP99LatencyMilliseconds = 60_000
	newConfig.Config = &config
	newConfig.ConfigHash, err = releaseConfigHash(&config, newConfig.Public, newConfig.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	newYaml, err := yaml.Marshal(&config)
	if err != nil {
		t.Fatal(err)
	}
	request, plan, err := prepareCampaignConfigMigration(&newConfig, f.stateDir, sourceBytes, oldYaml, newYaml)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = archiveReviewedSetupPlan(f.stateDir, plan)
	if err != nil {
		t.Fatal(err)
	}
	reviewedBytes, err := readSetupPlanBytes(f.stateDir, "plans/"+stringsTrim0x(plan.PlanHash)+".json")
	if err != nil {
		t.Fatal(err)
	}
	return &newConfig, request, plan, sourceBytes, reviewedBytes
}

// Exercise the production signature and immutable receipt writer with only
// generated test role keys, then return exact bytes for corruption/retry tests.
func signCampaignConfigMigrationTest(t *testing.T, f *runtimeEvidenceProvisionV2TestFixture, request campaignConfigMigrationRequest, plan *SetupPlan) (string, []byte) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.TrimPrefix(f.roles.EVM["testnet-owner"].PrivateKeyHex, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	receipt := campaignConfigMigrationReceipt{Request: request, PlanHash: plan.PlanHash}
	receipt.Hash, receipt.Signature, err = coordinatorRepairSignature(receipt, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(campaignConfigMigrationRoot(f.stateDir, plan.CampaignConfigMigrationHash), "receipt.json")
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), path, &receipt, campaignConfigMigrationMaximumBytes); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, raw
}

// The unsigned two-field edit fails both persisted and retained reads. Its
// separately signed exact migration admits only the genuine archived config.
func TestCampaignConfigMigrationRestoresExactHistoricalContext(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, campaignConfigMigrationOldTestConfig)
	if err := f.cfg.Config.Validate(); err == nil {
		t.Fatal("obsolete timeout became valid for a fresh config")
	}
	cfg, request, current, oldPlanBytes, newPlanBytes := prepareCampaignConfigMigrationTest(t, f)
	if _, err := loadPlanIdentityBytes(cfg, oldPlanBytes, true); err == nil {
		t.Fatal("unsigned config edit retained old plan authority")
	}
	if _, err := retainedPolicyRolloverSourceV2(t.Context(), cfg, f.stateDir, current, f.plan.PlanHash); err == nil {
		t.Fatal("unissued receipt authorized retained old config")
	}
	_, receiptBytes := signCampaignConfigMigrationTest(t, f, *request, current)
	for range 2 {
		sourceCfg, source, receipt, err := authenticatedCampaignConfigMigrationSource(t.Context(), cfg, f.stateDir, current, f.plan.PlanHash)
		if err != nil {
			t.Fatal(err)
		}
		actualHash, err := releaseConfigHash(sourceCfg.Config, sourceCfg.Public, sourceCfg.Hyperparameters)
		if err != nil || actualHash != source.ConfigHash || actualHash != f.cfg.ConfigHash || sourceCfg.ConfigHash != actualHash || sourceCfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds != 10_000 || !bytes.Equal(receipt.Request.SourceConfigBytes, request.SourceConfigBytes) || receipt.PlanHash != current.PlanHash {
			t.Fatal("historical resolution substituted a hash or lost source bytes", err)
		}
		if _, err := retainedPolicyRolloverSourceV2(t.Context(), cfg, f.stateDir, current, f.plan.PlanHash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadPlanIdentityBytes(cfg, newPlanBytes, false); err == nil {
		t.Fatal("provisional config migration authorized strict acceptance")
	}
	if _, err := loadPlanIdentityBytes(cfg, newPlanBytes, true); err != nil {
		t.Fatal("new real config hash could not load its explicit successor", err)
	}
	for index, action := range current.Actions {
		old := f.plan.Actions[index]
		if action.ID == "config.render" || action.ID == "topology.launch" {
			if action.Parameters["config_hash"] != cfg.ConfigHash || !actionAcceptsIntent(action, old.IntentHash) {
				t.Fatal("local intent lost explicit retained runtime authority")
			}
		} else if !evidenceRelayContinuationSameJSON(action, old) {
			t.Fatal("migration changed a non-runtime action")
		}
	}
	archived, err := readSetupPlanBytes(f.stateDir, "plans/"+stringsTrim0x(f.plan.PlanHash)+".json")
	if err != nil || !bytes.Equal(archived, oldPlanBytes) || len(receiptBytes) == 0 || f.cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds != 10_000 || cfg.Config.Scenarios.Adversaries.RequestTimeoutMilliseconds != 60_000 {
		t.Fatal("migration rewrote old plan or source config", err)
	}
}

// The sealed render retains its old config identity through a verified
// migration only; strict readers and missing/corrupt receipts remain closed.
func TestCampaignConfigMigrationRetainsOriginalRuntimeManifest(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, campaignConfigMigrationOldTestConfig)
	if _, err := expectedRuntimeConfigFiles(f.cfg, f.stateDir); err == nil || !validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
		t.Fatal("missing original activation completion was accepted or misclassified", err)
	}
	retainRuntimeEvidenceSetupCarryV2Test(t, f)
	completionPath := filepath.Join(f.stateDir, "evidence-v2-setup", "completed.json")
	completion, err := os.ReadFile(completionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{nil, []byte("{malformed synthetic completion")} {
		if err := atomicWrite(completionPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := expectedRuntimeConfigFiles(f.cfg, f.stateDir); err == nil || validatorcomponent.ReleaseEvidenceV2SetupFileInitiallyMissing(err) {
			t.Fatal("empty/malformed completion was accepted or treated as initial absence", err)
		}
	}
	if err := atomicWrite(completionPath, completion, 0o600); err != nil {
		t.Fatal(err)
	}
	f.cfg.Repos.Vault = t.TempDir()
	if err := os.MkdirAll(filepath.Join(f.cfg.Repos.Vault, "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	paths, err := expectedRuntimeConfigFiles(f.cfg, f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for relative, mode := range paths {
		path := filepath.Join(f.stateDir, filepath.FromSlash(relative))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if err := atomicWrite(path, []byte("synthetic retained input\n"), mode); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if err := writeRuntimeConfigManifest(f.cfg, f.stateDir); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(runtimeConfigManifestPath(f.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	cfg, request, current, _, reviewed := prepareCampaignConfigMigrationTest(t, f)
	path, _ := signCampaignConfigMigrationTest(t, f, *request, current)
	if err := atomicWrite(filepath.Join(f.stateDir, "plan.json"), reviewed, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true, PlanHash: current.PlanHash}}
	if _, _, err := authenticatedRuntimeConfigManifest(cfg, f.stateDir); err == nil {
		t.Fatal("strict manifest reader accepted archived config identity")
	}
	manifest, gotPaths, err := authenticatedRetainedRuntimeConfigManifest(cfg, f.stateDir, current)
	if err != nil || manifest.ConfigHash != f.cfg.ConfigHash || cfg.ConfigHash != current.ConfigHash || !reflect.DeepEqual(paths, gotPaths) {
		t.Fatal("signed migration lost original manifest ownership", err)
	}
	after, err := os.ReadFile(runtimeConfigManifestPath(f.stateDir))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retained manifest authentication changed its bytes", err)
	}
	changed := *manifest
	changed.ConfigHash = cfg.ConfigHash
	changed.ManifestHash, err = runtimeConfigManifestHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	changedBytes, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(runtimeConfigManifestPath(f.stateDir), changedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := authenticatedRetainedRuntimeConfigManifest(cfg, f.stateDir, current); err == nil {
		t.Fatal("signed retained migration allowed replacement render identity")
	}
	if err := atomicWrite(runtimeConfigManifestPath(f.stateDir), before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := authenticatedRetainedRuntimeConfigManifest(cfg, f.stateDir, current); err == nil {
		t.Fatal("retained runtime identity survived removal of its signed authority")
	}
}

// Retained startup authenticates the original topology receipt under the new
// signed campaign approval without running any pending setup or render action.
func TestCampaignConfigMigrationRetainedStartupKeepsTopologyReceipt(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, campaignConfigMigrationOldTestConfig)
	cfg, request, current, _, reviewed := prepareCampaignConfigMigrationTest(t, f)
	journal := openCampaignTestJournal(t, f.stateDir)
	executor := &Executor{cfg: f.cfg, plan: f.plan, stateDir: f.stateDir, journal: journal}
	persistProvisionalAdoptionTopologyTest(t, executor)
	entries := journal.Entries()
	signCampaignConfigMigrationTest(t, f, *request, current)
	if err := atomicWrite(filepath.Join(f.stateDir, "plan.json"), reviewed, 0o600); err != nil {
		t.Fatal(err)
	}
	provenanceCfg, _, _, _ := provisionalResumeTestContext(t)
	cfg.provisionalResume = provenanceCfg.provisionalResume
	options := cliOptions{Apply: true, ProvisionalResume: true, PlanHash: current.PlanHash}
	if err := prepareProvisionalResume(t.Context(), cfg, f.stateDir, "resume", options, current); err != nil {
		t.Fatal(err)
	}
	executor.cfg, executor.plan = cfg, current
	for range 2 {
		matched, err := executor.authenticateProvisionalRetainedPlan(t.Context(), reviewed)
		if err != nil || !matched {
			t.Fatal("signed campaign migration could not retain original topology receipt", matched, err)
		}
	}
	if !reflect.DeepEqual(entries, journal.Entries()) {
		t.Fatal("retained startup fabricated journal progress")
	}
}

// Exact values, all neighboring fields, snapshot bytes and signatures are
// independently bound; ancestry or a copied hash cannot authorize a variant.
func TestCampaignConfigMigrationRejectsAdjacentConfigAndReceiptChanges(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, campaignConfigMigrationOldTestConfig)
	cfg, request, current, _, _ := prepareCampaignConfigMigrationTest(t, f)
	path, original := signCampaignConfigMigrationTest(t, f, *request, current)
	for _, fault := range []string{"signature", "target-plan", "source-snapshot", "request-config", "acceptance"} {
		var receipt campaignConfigMigrationReceipt
		if err := json.Unmarshal(original, &receipt); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "signature":
			receipt.Signature = strings.Repeat("00", 65)
		case "target-plan":
			receipt.PlanHash = f.plan.PlanHash
		case "source-snapshot":
			receipt.Request.SourceConfigBytes = append(receipt.Request.SourceConfigBytes, '\n')
		case "request-config":
			receipt.Request.ConfigHash = f.cfg.ConfigHash
		case "acceptance":
			receipt.Request.FinalAcceptance = true
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := authenticatedCampaignConfigMigrationSource(t.Context(), cfg, f.stateDir, current, f.plan.PlanHash); err == nil {
			t.Fatalf("%s receipt mutation accepted", fault)
		}
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"timeout", "p99", "sample-rate", "route", "budget", "policy", "foreign-source"} {
		changedCfg, config, changedRequest := *cfg, *cfg.Config, *request
		switch fault {
		case "timeout":
			config.Scenarios.Adversaries.RequestTimeoutMilliseconds = 60_001
			config.Scenarios.Adversaries.MaximumP99LatencyMilliseconds = 60_001
		case "p99":
			config.Scenarios.Adversaries.MaximumP99LatencyMilliseconds = 60_001
		case "sample-rate":
			config.Scenarios.Adversaries.SampleIntervalMilliseconds++
		case "route":
			config.LaunchInputs.Authority = "synthetic-changed-route"
		case "budget":
			config.Budgets.MaximumRegistrationBurnRao++
		case "policy":
			changedRequest.PolicyHash = "0x" + strings.Repeat("aa", 32)
		case "foreign-source":
			changedRequest.SourceConfigHash = "0x" + strings.Repeat("bb", 32)
		}
		changedCfg.Config = &config
		var err error
		changedCfg.ConfigHash, err = releaseConfigHash(&config, cfg.Public, cfg.Hyperparameters)
		if err != nil {
			t.Fatal(err)
		}
		changedRequest.ConfigHash = changedCfg.ConfigHash
		changedRequest.ConfigBytes, err = yaml.Marshal(&config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := validateCampaignConfigMigrationConfigs(&changedCfg, &changedRequest); err == nil {
			t.Fatalf("%s neighboring config mutation accepted", fault)
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := authenticatedCampaignConfigMigrationSource(canceled, cfg, f.stateDir, current, f.plan.PlanHash); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled migration read succeeded", err)
	}
}

// Force the crash boundary exactly, then retry both the original active plan
// and the already-published successor. Neither retry may load a signing key.
func TestCampaignConfigMigrationReceiptBeforePointerRetry(t *testing.T) {
	f := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, campaignConfigMigrationOldTestConfig)
	cfg, request, current, _, reviewed := prepareCampaignConfigMigrationTest(t, f)
	active, err := readSetupPlanBytes(f.stateDir, "plan.json")
	if err != nil {
		t.Fatal(err)
	}
	signingLoads := 0
	loadRoles := func() (*RoleSecrets, error) { signingLoads++; return f.roles, nil }
	interrupted := errors.New("synthetic interruption after durable signature")
	_, err = publishCampaignConfigMigration(t.Context(), cfg, f.stateDir, current, *request, active, reviewed, loadRoles, func() error { return interrupted })
	if !errors.Is(err, interrupted) || signingLoads != 1 {
		t.Fatal("did not reach exact receipt-before-pointer interruption", err)
	}
	path := filepath.Join(campaignConfigMigrationRoot(f.stateDir, current.CampaignConfigMigrationHash), "receipt.json")
	receiptBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stillActive, err := readSetupPlanBytes(f.stateDir, "plan.json")
	if err != nil || !bytes.Equal(stillActive, active) {
		t.Fatal("interrupted migration advanced active plan", err)
	}
	for range 2 {
		_, err := publishCampaignConfigMigration(t.Context(), cfg, f.stateDir, current, *request, stillActive, reviewed, func() (*RoleSecrets, error) {
			t.Fatal("retry requested signing authority")
			return nil, errors.New("unexpected signing")
		}, func() error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		stillActive, err = readSetupPlanBytes(f.stateDir, "plan.json")
		if err != nil || !bytes.Equal(stillActive, reviewed) {
			t.Fatal("retry did not publish reviewed successor", err)
		}
		retained, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(retained, receiptBytes) {
			t.Fatal("retry replaced original signed receipt", err)
		}
	}
}

// Capture and signing require separate explicit approvals with no launch path.
func TestCampaignConfigMigrationCliRequiresExactReview(t *testing.T) {
	base := cliOptions{ProvisionalResume: true, PlanHash: "0x" + strings.Repeat("11", 32), ConfigMigrationSourceConfig: "/synthetic/original.yml"}
	if err := validateCampaignConfigMigrationOptions("campaign-config-migration", base); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*cliOptions){
		func(o *cliOptions) { o.ProvisionalResume = false },
		func(o *cliOptions) { o.Apply = true },
		func(o *cliOptions) { o.ConfigMigrationSourceConfig = "relative.yml" },
		func(o *cliOptions) { o.ThenReleaseCandidate = true },
		func(o *cliOptions) { o.Detach = true },
	} {
		changed := base
		alter(&changed)
		if err := validateCampaignConfigMigrationOptions("campaign-config-migration", changed); err == nil {
			t.Fatal("unreviewed or accepting migration options admitted")
		}
	}
	if err := validateCampaignConfigMigrationOptions("resume", base); err == nil {
		t.Fatal("migration flags accepted by runtime mutation")
	}
	base.Apply, base.ConfigMigrationHash, base.ConfigMigrationSourceConfig = true, base.PlanHash, ""
	if err := validateCampaignConfigMigrationOptions("campaign-config-migration", base); err != nil {
		t.Fatal(err)
	}
}

// Real generation receipts and source-role signatures must survive the new
// campaign identity without rewriting validator configs, ledgers or manifest.
func TestCampaignConfigMigrationRetainsGenerationAndSourceRole(t *testing.T) {
	g, rollover, handoff, journal := newPolicyRolloverHandoffConfigTestV2(t, campaignConfigMigrationOldTestConfig, func(g *policyRolloverGenerationTestV2) {
		// Approve the fixture's changed policy before generation two, then
		// keep it unchanged across the separate timeout migration.
		f := g.fixture
		if _, err := archiveReviewedSetupPlan(f.stateDir, f.plan); err != nil {
			t.Fatal(err)
		}
		source := f.plan
		var err error
		f.plan, err = buildPlan(f.cfg, &source.LiveFacts, source.Roles, time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		f.plan.PriorPlanHashes = append(append([]string(nil), source.PriorPlanHashes...), source.PlanHash)
		f.plan.PlanHash, err = f.plan.hash()
		if err != nil {
			t.Fatal(err)
		}
		planBytes, err := json.Marshal(f.plan)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(f.stateDir, "plan.json"), planBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		g.generation = 2
		g.roles, err = policyRolloverGenerationRolesV2(f.cfg, f.roles, g.generation)
		if err != nil {
			t.Fatal(err)
		}
		policyHash, err := decodeHex32("synthetic policy", f.cfg.PolicyHash)
		if err != nil {
			t.Fatal(err)
		}
		for index := range g.members {
			member := &g.members[index]
			hotkey, key, err := runtimeEvidenceActivationKeysV2(g.roles, member.ValidatorId, member.NoId)
			if err != nil {
				t.Fatal(err)
			}
			member.Activation.VPK = [32]byte(key[32:])
			member.Activation.Domain.PolicyHash = policyHash
			member.VpkSignature, err = member.Activation.SignVPK(key)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := member.Activation.Digest()
			if err != nil {
				t.Fatal(err)
			}
			member.HotkeySignature, err = hotkey.Sign(digest[:])
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	activatePolicyRolloverHandoffTestV2(t, g, rollover, handoff, journal)
	f := g.fixture
	handoff, err := readBasePolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	sourceRoleIo := policyRolloverSourceRoleTestIoV2(t, g, handoff)
	sourceRole, err := preparePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, handoff, sourceRoleIo)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := activatePolicyRolloverSourceRoleV2(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, handoff, sourceRole, sourceRoleIo, func(context.Context, *validatorcomponent.ReleaseConfig) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	namespaces := []string{"runtime", "policy-rollover", "evidence-generations", "evidence-v2-setup"}
	before := map[string]map[string]string{}
	for _, namespace := range namespaces {
		before[namespace] = validatorNamespaceTreeSnapshot(t, filepath.Join(f.stateDir, namespace))
	}
	cfg, request, current, _, reviewed := prepareCampaignConfigMigrationTest(t, f)
	signCampaignConfigMigrationTest(t, f, *request, current)
	if err := atomicWrite(filepath.Join(f.stateDir, "plan.json"), reviewed, 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		retained, err := readBasePolicyRolloverHandoffV2(t.Context(), cfg, f.stateDir, current)
		if err != nil || !reflect.DeepEqual(retained, handoff) {
			t.Fatal("migration lost original generation receipt", err)
		}
		got, err := readPolicyRolloverSourceRoleOverlayWithV2(t.Context(), cfg, f.stateDir, current, retained, sourceRoleIo)
		if err != nil || !reflect.DeepEqual(got, selected) {
			t.Fatal("migration lost exact signed source-role approval", err)
		}
		paths := map[string]os.FileMode{}
		if err := addPolicyRolloverGenerationRuntimeInputsV2(t.Context(), cfg, f.stateDir, current, paths, sourceRoleIo); err != nil {
			t.Fatal("migration lost immutable generation inventory", err)
		}
	}
	if _, err := readPolicyRolloverPlanV2(t.Context(), cfg, current, f.stateDir, policyRolloverPlanPathV2(f.stateDir, rollover.Generation, rollover.Epoch)); err == nil {
		t.Fatal("retained migration reader authorized fresh historical rollover mutation")
	}
	if err := validatePolicyRolloverSourceRoleV2(t.Context(), cfg, f.stateDir, current, handoff, sourceRole, sourceRoleIo); err == nil {
		t.Fatal("retained migration reader authorized fresh old source-role mutation")
	}
	for _, namespace := range namespaces {
		if !reflect.DeepEqual(before[namespace], validatorNamespaceTreeSnapshot(t, filepath.Join(f.stateDir, namespace))) {
			t.Fatalf("migration changed original %s namespace bytes", namespace)
		}
	}
}
