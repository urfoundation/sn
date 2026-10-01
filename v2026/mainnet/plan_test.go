// Plan tests retain real independently encoded SCALE/RLP fixture evidence and
// force corrupt, mismatched and incomplete inputs without keys or chain writes.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Synthetic snapshot, source lock and release are independently resealed when
// a test intentionally changes a deeper commitment while preserving the outer.
type planTestFixture struct {
	dir      string
	config   bootstrapPlanConfig
	snapshot finalizedSnapshotEnvelope
	lock     sourceLock
	release  bootstrapReleaseInput
}

// A whole captured fixture is serialized to files, exercising the production
// loader rather than substituting a mocked already-validated graph.
func newPlanTestFixture(t *testing.T) *planTestFixture {
	t.Helper()
	client, mapping := newFinalizedMappingFixture(t, 1, 1)
	mapping.fault = func(method string, _ []any, _ int) (any, bool) {
		if method == "eth_chainId" {
			return "0x3c4", true
		}
		return nil, false
	}
	snapshot, err := client.readFinalizedSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := sealFinalizedSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	lock := sourceLock{Schema: sourceLockSchema, Scope: "clean-git-sources-and-local-go-replacements-only", Module: "github.com/urfoundation/sn/v2026", GoVersion: "go1.26.6", ToolSha256: testGenesisHash, GoModSha256: testGenesisHash, GoSumSha256: testGenesisHash,
		Repositories:      []sourceLockRepository{{Path: ".", Commit: strings.Repeat("1a", 20)}, {Path: "../synthetic", Commit: strings.Repeat("2b", 20)}},
		LocalReplacements: []sourceLockReplacement{{Module: "example.invalid/synthetic", LocalPath: "../synthetic", Repository: "../synthetic"}}}
	fixture := &planTestFixture{
		dir: t.TempDir(), snapshot: envelope, lock: lock,
		config:  bootstrapPlanConfig{Schema: bootstrapPlanConfigSchema, DeploymentId: "synthetic-launch", Netuid: 25, Network: planNetwork{NativeChain: snapshot.Runtime.Identity.NativeChain, GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId}},
		release: bootstrapReleaseInput{Schema: bootstrapReleaseInputSchema, DeploymentId: "synthetic-launch", Netuid: 25, RuntimeVersion: snapshot.Runtime.Version, RuntimeCodeHash: snapshot.Runtime.CodeHash, RuntimeMetadataHash: snapshot.Runtime.MetadataHash},
	}
	fixture.resealLock(t)
	fixture.rebind(t)
	return fixture
}

// This encoder calculates expected fixture file hashes independently of the
// production reader and preserves the exact newline in every JSON file.
func planTestWrite(t *testing.T, dir, name string, value any) planFileReference {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return planFileReference{Path: name, Sha256: "sha256:" + hex.EncodeToString(digest[:])}
}

// Preserve a correct outer source-lock seal while testing semantic corruption.
func (self *planTestFixture) resealLock(t *testing.T) {
	t.Helper()
	self.lock.ContentHash = ""
	raw, err := json.Marshal(self.lock)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte(sourceLockSchema+"\x00"), raw...))
	self.lock.ContentHash = "0x" + hex.EncodeToString(digest[:])
}

// File binding can be updated independently of content seals so tests reach
// the exact boundary they are intended to break.
func (self *planTestFixture) rebind(t *testing.T) string {
	t.Helper()
	self.config.Snapshot = planTestWrite(t, self.dir, "snapshot.json", self.snapshot)
	self.config.SourceLock = planTestWrite(t, self.dir, "source.json", self.lock)
	self.release.SnapshotContentHash = self.snapshot.ContentHash
	self.release.SourceLockContentHash = self.lock.ContentHash
	self.config.Release = planTestWrite(t, self.dir, "release.json", self.release)
	return self.writeConfig(t)
}

// Persist only the config when a deliberately stale child hash must remain.
func (self *planTestFixture) writeConfig(t *testing.T) string {
	t.Helper()
	planTestWrite(t, self.dir, "config.json", self.config)
	return filepath.Join(self.dir, "config.json")
}

// Repeated byte-identical inputs produce byte-identical plans and a separately
// reproducible domain hash. Root readiness cannot replace either UR validator.
func TestBootstrapPlanDeterministicBlockedGraph(t *testing.T) {
	fixture := newPlanTestFixture(t)
	path := fixture.writeConfig(t)
	first, err := loadBootstrapPlan(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadBootstrapPlan(context.Background(), path)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("plan changed on identical retained inputs: %v", err)
	}
	if first.Status != "blocked" || first.ApplyAuthority || first.ActivationReady || first.FinalizedNumber != 100 || first.EvmNumber != 37 ||
		first.Economics.ProviderNumerator != 1 || first.Economics.FractionDenominator != 10 || first.Economics.RemainderNumerator != 9 ||
		first.Economics.Remainder != "owner-recycle" || first.Economics.ReserveCredit || len(first.Inputs) != 4 {
		t.Fatalf("plan invented readiness or changed requested economics: %+v", first)
	}
	requirements := map[string]bool{}
	for _, requirement := range first.Requirements {
		requirements[requirement.Id] = true
		if requirement.Status != "missing" {
			t.Fatal("empty review input satisfied a requirement")
		}
	}
	seen := map[string]bool{}
	for _, action := range first.Actions {
		if action.Executable || action.Status != "blocked" || seen[action.Id] {
			t.Fatalf("action is executable or duplicated: %+v", action)
		}
		for _, dependency := range action.DependsOn {
			if !seen[dependency] {
				t.Fatalf("non-topological dependency %q in %q", dependency, action.Id)
			}
		}
		for _, requirement := range action.Requirements {
			if !requirements[requirement] {
				t.Fatalf("unknown action requirement %q", requirement)
			}
		}
		seen[action.Id] = true
	}
	for _, required := range []string{"reset-miner-uids", "install-contracts", "start-root-validator", "start-ur-validators", "activate-native-miner-emissions", "accept-and-reconcile"} {
		if !seen[required] {
			t.Fatalf("requested outcome %q missing", required)
		}
	}
	wantHash := first.ContentHash
	first.ContentHash = ""
	raw, _ := json.Marshal(first)
	digest := sha256.Sum256(append([]byte(bootstrapPlanSchema+"\x00"), raw...))
	if wantHash != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatal("plan hash is not independently reproducible")
	}
}

// A valid outer seal cannot promote Snow945 or a different genesis into the
// separately configured mainnet. Mismatch produces no partial output.
func TestBootstrapPlanRejectsTestnetAndForeignGenesis(t *testing.T) {
	for _, mutation := range []string{"testnet", "foreign-genesis", "missing-approval", "wrong-netuid"} {
		fixture := newPlanTestFixture(t)
		switch mutation {
		case "testnet":
			fixture.snapshot.Runtime.Identity.EvmChainId = 945
			fixture.snapshot.Mapping.Identity.EvmChainId = 945
			fixture.snapshot, _ = sealFinalizedSnapshot(fixture.snapshot.finalizedSnapshot)
		case "foreign-genesis":
			fixture.config.Network.GenesisHash = "0x" + strings.Repeat("bd", 32)
		case "missing-approval":
			fixture.config.Network.GenesisHash = ""
		case "wrong-netuid":
			fixture.config.Netuid = 0
		}
		var stdout, stderr bytes.Buffer
		code := runMain(context.Background(), []string{"plan", "--config", fixture.rebind(t)}, &stdout, &stderr)
		if code == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("%s accepted: exit=%d stdout=%s", mutation, code, stdout.String())
		}
		if (mutation == "testnet" || mutation == "foreign-genesis") && code != 3 {
			t.Fatalf("%s lost explicit identity mismatch: exit=%d", mutation, code)
		}
	}
}

// Tampering with bytes fails before trusting their hashes; rehashing corrupted
// embedded commitments also fails the independent artifact/header checks.
func TestBootstrapPlanRejectsTamperedCommitments(t *testing.T) {
	for _, mutation := range []string{"file", "snapshot-seal", "code", "metadata", "evm-rlp", "post-log", "source-seal", "source-reference"} {
		fixture := newPlanTestFixture(t)
		path := fixture.writeConfig(t)
		switch mutation {
		case "file":
			if err := os.WriteFile(filepath.Join(fixture.dir, "source.json"), []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		case "snapshot-seal":
			fixture.snapshot.ContentHash = "sha256:" + strings.Repeat("ab", 32)
			path = fixture.rebind(t)
		case "source-seal":
			fixture.lock.ContentHash = testGenesisHash
			path = fixture.rebind(t)
		case "source-reference":
			fixture.lock.LocalReplacements[0].Repository = "../not-locked"
			fixture.resealLock(t)
			path = fixture.rebind(t)
		default:
			switch mutation {
			case "code":
				fixture.snapshot.Runtime.CodeHex = "0x010203"
			case "metadata":
				fixture.snapshot.Runtime.MetadataHex = "0x040506"
			case "evm-rlp":
				fixture.snapshot.Mapping.EvmHeader.HeaderRlp = "0xc0"
			case "post-log":
				fixture.snapshot.Mapping.PostLog.BlockHash = testGenesisHash
			}
			var err error
			fixture.snapshot, err = sealFinalizedSnapshot(fixture.snapshot.finalizedSnapshot)
			if err != nil {
				t.Fatal(err)
			}
			path = fixture.rebind(t)
		}
		if plan, err := loadBootstrapPlan(context.Background(), path); err == nil || plan.Schema != "" {
			t.Errorf("%s retained a plan after corrupt input: %v", mutation, err)
		}
	}
}

// Cross-release hashes and full runtime tuples are separate from file hashes;
// even a correctly rehashed release cannot bind a different source or runtime.
func TestBootstrapPlanRejectsCrossInputMismatch(t *testing.T) {
	for _, mutation := range []string{"snapshot", "source", "code", "version", "deployment", "netuid"} {
		fixture := newPlanTestFixture(t)
		switch mutation {
		case "snapshot":
			fixture.release.SnapshotContentHash = "sha256:" + strings.Repeat("a1", 32)
		case "source":
			fixture.release.SourceLockContentHash = testGenesisHash
		case "code":
			fixture.release.RuntimeCodeHash = testGenesisHash
		case "version":
			fixture.release.RuntimeVersion.StateVersion++
		case "deployment":
			fixture.release.DeploymentId = "different-deployment"
		case "netuid":
			fixture.release.Netuid = 0
		}
		fixture.config.Release = planTestWrite(t, fixture.dir, "release.json", fixture.release)
		if _, err := loadBootstrapPlan(context.Background(), fixture.writeConfig(t)); err == nil {
			t.Errorf("cross-input %s mismatch accepted", mutation)
		}
	}
}

// Review manifests are opaque, hash-bound evidence, never accepted predicates.
// Supplying every manifest must leave all actions and activation blocked.
func TestBootstrapPlanAllReviewInputsRemainUnvalidated(t *testing.T) {
	fixture := newPlanTestFixture(t)
	for _, requirement := range bootstrapRequirements() {
		reference := planTestWrite(t, fixture.dir, requirement.Id+".json", map[string]any{"approved": true, "activation_ready": true})
		fixture.release.ReviewInputs = append(fixture.release.ReviewInputs, planReviewInput{Requirement: requirement.Id, planFileReference: reference})
	}
	plan, err := loadBootstrapPlan(context.Background(), fixture.rebind(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, requirement := range plan.Requirements {
		if requirement.Status != "supplied_unvalidated" || !planSha256(requirement.InputSha256) {
			t.Fatalf("review file promoted to approval: %+v", requirement)
		}
	}
	if plan.ApplyAuthority || plan.ActivationReady || plan.Status != "blocked" {
		t.Fatal("all opaque supplied files enabled activation")
	}
	for _, action := range plan.Actions {
		if action.Executable || action.Status != "blocked" {
			t.Fatal("opaque manifest enabled action")
		}
	}
	fixture.release.ReviewInputs = append(fixture.release.ReviewInputs, fixture.release.ReviewInputs[0])
	if _, err := loadBootstrapPlan(context.Background(), fixture.rebind(t)); err == nil {
		t.Fatal("duplicate review requirement accepted")
	}
	fixture.release.ReviewInputs = fixture.release.ReviewInputs[:1]
	fixture.release.ReviewInputs[0].Requirement = "force-ready"
	if _, err := loadBootstrapPlan(context.Background(), fixture.rebind(t)); err == nil {
		t.Fatal("unknown review requirement accepted")
	}
}

// Strict decoding applies at every source of authority-shaped input, including
// case-insensitive duplicates, unknown fields and absent full runtime fields.
func TestBootstrapPlanRejectsAmbiguousJson(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"a","Schema":"b"}`,
		`{"schema":"a","force":true}`,
		`{"schema":"a"} {}`,
		`{"netuid":65536}`,
	} {
		var config bootstrapPlanConfig
		if err := decodePlanJson([]byte(raw), &config); err == nil {
			t.Errorf("ambiguous JSON accepted: %s", raw)
		}
	}
	fixture := newPlanTestFixture(t)
	raw, err := os.ReadFile(filepath.Join(fixture.dir, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var release map[string]any
	if err := json.Unmarshal(raw, &release); err != nil {
		t.Fatal(err)
	}
	delete(release["runtime_version"].(map[string]any), "stateVersion")
	fixture.config.Release = planTestWrite(t, fixture.dir, "release.json", release)
	if _, err := loadBootstrapPlan(context.Background(), fixture.writeConfig(t)); err == nil {
		t.Fatal("missing runtime stateVersion silently defaulted")
	}
}

// The loader refuses empty, oversized, linked and special files without
// blocking on a FIFO or expanding environment input. Cancellation is explicit.
func TestBootstrapPlanBoundsAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, _, err := readPlanFile(context.Background(), path, 1); err != nil || string(raw) != "x" {
		t.Fatalf("exact byte bound failed: %v", err)
	}
	if _, _, err := readPlanFile(context.Background(), path, 0); err == nil {
		t.Fatal("oversized input accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{link, fifo, dir, "$HOME/input"} {
		if _, _, err := readPlanFile(context.Background(), invalid, 1024); err == nil {
			t.Errorf("invalid local input %q accepted", invalid)
		}
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readPlanFile(context.Background(), path, 1024); err == nil {
		t.Fatal("empty input accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadBootstrapPlan(ctx, path); err == nil {
		t.Fatal("canceled plan proceeded")
	}
}

// Outline has no evidence authority; CLI bound planning is deterministic and
// emits JSON only on success. Unsupported mutation flags cannot slip through.
func TestBootstrapPlanCommandAndUnboundOutline(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runMain(context.Background(), []string{"plan", "--outline"}, &stdout, &stderr)
	var outline bootstrapPlan
	if err := json.Unmarshal(stdout.Bytes(), &outline); err != nil || code != 0 || outline.Status != "unbound_outline" || outline.Network.GenesisHash != "" || outline.FinalizedHash != "" || outline.ApplyAuthority || outline.ActivationReady || len(outline.Actions) == 0 {
		t.Fatalf("outline invented authority: exit=%d err=%v", code, err)
	}
	fixture := newPlanTestFixture(t)
	stdout.Reset()
	stderr.Reset()
	if code := runMain(context.Background(), []string{"plan", "--config", fixture.writeConfig(t)}, &stdout, &stderr); code != 0 {
		t.Fatalf("bound review command failed: %d %s", code, stderr.String())
	}
	var plan bootstrapPlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil || !planSha256(plan.ContentHash) || plan.Status != "blocked" {
		t.Fatal("bound command did not emit a sealed blocked review")
	}
	for _, args := range [][]string{{"plan"}, {"plan", "--outline", "--config", "x"}, {"plan", "--outline", "--force"}, {"plan", "--outline", "--accept-plan", "x"}} {
		stdout.Reset()
		stderr.Reset()
		if code := runMain(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Errorf("invalid command admitted: %v exit=%d", args, code)
		}
	}
	var nilWriter ioFailureWriter
	if code := runMain(context.Background(), []string{"plan", "--outline"}, nilWriter, &stderr); code != 1 {
		t.Fatal("output failure reported success")
	}
}

// A deterministic sink failure ensures a plan is not reported as emitted.
type ioFailureWriter struct{}

// All writes fail without affecting filesystem or network state.
func (ioFailureWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic output failure")
}
