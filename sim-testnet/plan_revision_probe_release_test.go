// Exercises a changed release approval at the untouched replacement boundary
// through authenticated plan history and one deterministic finalized view.
package main

import (
	"context"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Owns a synthetic older approval and the current release's empty CREATEs.
type unusedReplacementReleaseFixture struct {
	cfg      *ResolvedConfig
	prior    *SetupPlan
	payloads *DeploymentPayloads
	roles    *RoleSecrets
	current  SetupFacts
	stateDir string
	rpc      *replacementBoundaryRPC
	entries  []JournalEntry
}

// Archives different, entirely synthetic coordinator and probe bytes so the
// regression requires a fresh approval instead of a hash-check bypass.
func newUnusedReplacementReleaseFixture(t *testing.T) *unusedReplacementReleaseFixture {
	t.Helper()
	cfg, payloads, retained, baseline, _ := replacementPrecompileProbeFixture(t)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := buildDeploymentPayloads(cfg, roles, retained.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	publicRoles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	facts := *testSetupFacts()
	facts.DeployerNonce = retained.InitialNonce
	prior, err := buildPlan(cfg, &facts, publicRoles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := rebindPlanDeployment(prior, retained); err != nil {
		t.Fatal(err)
	}
	previousPayloads := *payloads
	previousPayloads.ExpectedRuntime = maps.Clone(payloads.ExpectedRuntime)
	previousPayloads.Manifest.RuntimeHashes = cloneStrings(payloads.Manifest.RuntimeHashes)
	previousPayloads.Manifest.RuntimeHashes[retained.CoordinatorImplementation.Hex()] = crypto.Keccak256Hash([]byte{0x60, 0x01}).Hex()
	previousPayloads.UpgradeImplementation = []byte{0x60, 0x02}
	previousPayloads.CoordinatorUpgrade.RuntimeCodeHash = crypto.Keccak256Hash([]byte{0x60, 0x03}).Hex()
	previousPayloads.PrecompileProbe = []byte{0x60, 0x04}
	previousPayloads.ExpectedRuntime[payloads.PrecompileProbeAddress] = []byte{0x60, 0x05}
	baseline.ReleaseDeploymentHash, err = contractDeploymentIdentityHash(previousPayloads.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	baseline.PrecompileProbeExecutableHash = crypto.Keccak256Hash([]byte{0x60, 0x06}).Hex()
	baseline.ReplacementPrecompileProbeHash = crypto.Keccak256Hash(previousPayloads.ExpectedRuntime[payloads.PrecompileProbeAddress]).Hex()
	prior.CoordinatorUpgradeBaseline = baseline
	if err := rebindPlanCoordinatorUpgrade(prior, &previousPayloads); err != nil {
		t.Fatal(err)
	}
	prior.PriorPlanHashes = []string{"0x" + strings.Repeat("71", 32)}
	stateDir := t.TempDir()
	if err := saveContractDeployment(stateDir, retained); err != nil {
		t.Fatal(err)
	}
	prior = writeReadPersistedV4ReplacementPlan(t, stateDir, prior)
	codes := make(map[common.Address][]byte)
	for _, address := range contractDeploymentAddresses(retained) {
		codes[address] = append([]byte(nil), initial.ExpectedRuntime[address]...)
	}
	codes[retained.PrecompileProbe] = []byte{1}
	active := common.HexToAddress(baseline.ActiveImplementation)
	codes[active] = append([]byte(nil), initial.ExpectedRuntime[active]...)
	current := *testSetupFacts()
	current.DeployerNonce = payloads.PrecompileProbeNonce
	current.EVMFinalizedBlock = 150
	return &unusedReplacementReleaseFixture{
		cfg: cfg, prior: prior, payloads: payloads, roles: roles, current: current,
		stateDir: stateDir,
		rpc:      &replacementBoundaryRPC{t: t, nonce: current.DeployerNonce, block: 200, codes: codes, activeImplementation: active},
	}
}

// The fixture rejects unplanned methods and owns its server until observation
// returns, exercising the same observer dispatch as a read-only live plan.
func (self *unusedReplacementReleaseFixture) observe(t *testing.T) (*coordinatorUpgradeMigration, error) {
	t.Helper()
	server := httptest.NewServer(self.rpc)
	defer server.Close()
	self.cfg.OperationalRPCMode = rpcModePrivateAuthority
	self.cfg.OperationalEVM = server.URL
	return observeCoordinatorUpgradeMigration(context.Background(), self.cfg, self.stateDir, self.prior, &self.current, self.entries, self.roles)
}

// A review of new bytes must keep custody and nonce identity while updating
// both executable approval and the later pure revision's action intents.
func TestUnusedReplacementProbeRefreshesAuthenticatedReleaseApproval(t *testing.T) {
	fixture := newUnusedReplacementReleaseFixture(t)
	priorHash, err := canonicalHashHex(fixture.prior)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := fixture.observe(t)
	if err != nil {
		t.Fatalf("untouched replacement release refresh refused: %v", err)
	}
	if migration == nil || migration.Upgrade != fixture.payloads.CoordinatorUpgrade {
		t.Fatalf("refresh did not approve the current coordinator: %+v", migration)
	}
	want := fixture.prior.CoordinatorUpgradeBaseline
	want.ReleaseDeploymentHash, err = contractDeploymentIdentityHash(fixture.payloads.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	want.PrecompileProbeExecutableHash, err = normalizedSolidityExecutableHash(fixture.payloads.ExpectedRuntime[fixture.payloads.PrecompileProbeAddress], TestnetPrecompileProbeArtifact)
	if err != nil {
		t.Fatal(err)
	}
	want.ReplacementPrecompileProbeHash = crypto.Keccak256Hash(fixture.payloads.ExpectedRuntime[fixture.payloads.PrecompileProbeAddress]).Hex()
	want.FinalizedBlock = fixture.rpc.block
	want.FinalizedBlockHash = "0x" + strings.Repeat("ab", 32)
	if migration.Baseline != want {
		t.Fatalf("refresh changed retained identity or omitted current release bytes: got=%+v want=%+v", migration.Baseline, want)
	}
	revised, err := buildPlanRevisionFromFactsWithMigration(fixture.cfg, fixture.stateDir, fixture.prior, &fixture.current, nil, time.Unix(2, 0), migration)
	if err != nil {
		t.Fatalf("refreshed replacement could not render a reviewable plan: %v", err)
	}
	if revised.PlanHash == fixture.prior.PlanHash || revised.CoordinatorUpgrade != migration.Upgrade || revised.CoordinatorUpgradeBaseline != migration.Baseline || !contractDeploymentAddressesEqual(revised.Deployment, fixture.prior.Deployment) {
		t.Fatal("refreshed plan lost its new approval or retained custody identity")
	}
	for _, actionId := range []string{"precompile.probe-deploy", "evm.coordinator-upgrade-implementation", "evm.coordinator-upgrade-activate"} {
		if actionByID(t, revised, actionId).IntentHash == actionByID(t, fixture.prior, actionId).IntentHash {
			t.Fatalf("new release action %s reused its older intent", actionId)
		}
	}
	stored, err := readValidatorEvidenceHistoricalPlan(fixture.stateDir, fixture.prior.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	storedHash, err := canonicalHashHex(stored)
	if err != nil || storedHash != priorHash {
		t.Fatalf("read-only refresh changed authenticated history: %v", err)
	}
	gotPriorHash, err := canonicalHashHex(fixture.prior)
	if err != nil || gotPriorHash != priorHash {
		t.Fatalf("read-only refresh mutated its prior plan: %v", err)
	}
}

// Every progress, custody and historical-authentication failure keeps the
// old approval closed; a refreshed digest alone never supplies authority.
func TestUnusedReplacementProbeRefreshRejectsProgressAndAuthorityDrift(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		mutate    func(*testing.T, *unusedReplacementReleaseFixture)
		wantError string
	}{
		{name: "consumed replacement nonce", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.current.DeployerNonce++
			fixture.rpc.nonce++
		}, wantError: "does not authenticate the release deployment"},
		{name: "nonce advanced after facts", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.rpc.nonce++
		}, wantError: "persisted replacement deployer nonce"},
		{name: "durable probe intent", mutate: func(t *testing.T, fixture *unusedReplacementReleaseFixture) {
			action := actionByID(t, fixture.prior, "precompile.probe-deploy")
			fixture.entries = []JournalEntry{{PlanHash: fixture.prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast}}
		}, wantError: "unconsumed replacement precompile probe"},
		{name: "future coordinator code", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.rpc.codes[fixture.payloads.CoordinatorUpgrade.Implementation] = []byte{1}
		}, wantError: "unconsumed coordinator implementation"},
		{name: "future batcher code", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.rpc.codes[fixture.payloads.FleetBatcherAddress] = []byte{1}
		}, wantError: "unconsumed fleet batcher"},
		{name: "immutable custody runtime", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.rpc.codes[fixture.prior.Deployment.SettlementVault] = []byte{1}
		}, wantError: "immutable runtime mismatch"},
		{name: "active coordinator changed", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.rpc.activeImplementation = fixture.payloads.CoordinatorUpgrade.Implementation
		}, wantError: "persisted replacement active coordinator"},
		{name: "conformance intent", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.entries = []JournalEntry{{PlanHash: fixture.prior.PlanHash, ActionID: "precompile.seed", Stage: StageIntent}}
		}, wantError: "entered the durable journal"},
		{name: "persisted conformance", mutate: func(t *testing.T, fixture *unusedReplacementReleaseFixture) {
			if err := atomicWrite(precompileEvidencePath(fixture.stateDir), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantError: "evidence was persisted"},
		{name: "in-memory approval changed", mutate: func(_ *testing.T, fixture *unusedReplacementReleaseFixture) {
			fixture.prior.CoordinatorUpgradeBaseline.ReleaseDeploymentHash = "0x" + strings.Repeat("72", 32)
		}, wantError: "differs from its authenticated prior approval"},
		{name: "archived bytes changed", mutate: func(t *testing.T, fixture *unusedReplacementReleaseFixture) {
			path := filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.prior.PlanHash)+".json")
			wire, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wire = []byte(strings.Replace(string(wire), fixture.prior.CoordinatorUpgradeBaseline.ReleaseDeploymentHash, "0x"+strings.Repeat("73", 32), 1))
			if err := atomicWrite(path, wire, 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantError: "persisted setup plan hash mismatch"},
	}
	for _, test := range cases {
		fixture := newUnusedReplacementReleaseFixture(t)
		test.mutate(t, fixture)
		migration, err := fixture.observe(t)
		if migration != nil || err == nil || !strings.Contains(err.Error(), test.wantError) {
			t.Errorf("%s: migration=%+v error=%v, want %q", test.name, migration, err, test.wantError)
		}
	}
}
