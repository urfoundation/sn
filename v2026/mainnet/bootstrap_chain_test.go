// Synthetic command fixtures compose real trim decoding, signed child plans
// and durable custody owners. No production identity, key or route is used.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Mutable fixture inputs are changed only between complete command invocations.
// The root and EVM native signature bytes remain owned by their original tests.
type bootstrapChainFixture struct {
	path        string
	config      bootstrapChainConfig
	preparation bootstrapChainPreparation
	root        *bootstrapRootFixture
	contracts   *evmCreateFixture
	validators  []*bootstrapChainValidatorFixture
	rootRole    *bootstrapChainRootFixture
	client      *rpcClient
	census      *rootRpcFixture
}

// The test builds a common synthetic runtime/domain from actual census readers,
// then independently reapproves the exact child scopes and private state paths.
func newBootstrapChainFixture(t *testing.T, ownerOverride ...[]byte) *bootstrapChainFixture {
	t.Helper()
	return newBootstrapChainFixtureWithCensus(t, nil, ownerOverride...)
}

// A selected fixture profile is fixed before any independently signed input or
// retained journal exists; no test rewrites original custody to add authority.
func newBootstrapChainFixtureWithCensus(t *testing.T, configure func(*rootRpcFixture, *subnetCensusPolicy), ownerOverride ...[]byte) *bootstrapChainFixture {
	t.Helper()
	client, census, policy := newSubnetFixture(t, ownerOverride...)
	policy.RuntimeVersion.SpecName, policy.RuntimeVersion.TransactionVersion = "node-subtensor", 1
	census.version = policy.RuntimeVersion
	if configure != nil {
		configure(census, &policy)
	}
	root := newBootstrapRootFixture(t)
	contracts := newEvmCreateFixture(t)
	network := planNetwork{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}
	action := copyRootAction(root.offline.packet.Action)
	action.Scope.GenesisHash, action.Scope.NativeChain = policy.GenesisHash, policy.NativeChain
	action.Scope.RuntimeVersion, action.Scope.RuntimeCodeHash, action.Scope.RuntimeMetadataHash = policy.RuntimeVersion, policy.RuntimeCodeHash, policy.RuntimeMetadataHash
	action.Scope.Seat = rootSeatExpectation{Uid: 0, RegistrationBlock: 40}
	var err error
	action, err = prepareRootAction(action, census.metadataHex)
	if err != nil {
		t.Fatal(err)
	}
	root.offline.trust.GenesisHash, root.offline.trust.NativeChain = policy.GenesisHash, policy.NativeChain
	root.offline.packet = rootOfflineApprove(t, root.offline.trust, action, root.offline.approvalKey)
	root.config.Network = network
	service := rootServiceConfig{Schema: rootServiceConfigSchema, CustodyTrust: root.offline.trust, Packet: root.offline.packet, MaximumObservations: 3}
	root.config.RootService = bootstrapRootTestWrite(t, root.config.RootService.Path, service)
	bootstrapRootTestWrite(t, root.configPath, root.config)
	root.plan, err = loadBootstrapRootPlan(t.Context(), root.configPath)
	if err != nil {
		t.Fatal(err)
	}
	oldHotkey := bytes.Repeat([]byte{0x11}, 32)
	oldKey, err := native.CreateStorageKey(census.metadata, "SubtensorModule", "Uids", []byte{0, 0}, oldHotkey)
	if err != nil {
		t.Fatal(err)
	}
	delete(census.storageKVs, oldKey.Hex())
	hotkey, _ := hex.DecodeString(action.Scope.Hotkey[2:])
	coldkey, _ := hex.DecodeString(action.Scope.Coldkey[2:])
	census.set(t, "Keys", hotkey, []byte{0, 0}, []byte{0, 0})
	census.set(t, "Uids", []byte{0, 0}, []byte{0, 0}, hotkey)
	census.set(t, "Owner", coldkey, hotkey)
	census.set(t, "IsNetworkMember", []byte{1}, hotkey, []byte{0, 0})
	configDirectory := filepath.Dir(root.configPath)
	policyRef := bootstrapRootTestWrite(t, filepath.Join(configDirectory, "trim-policy.json"), policy)
	preview, err := client.readSubnetPreview(t.Context(), policy, policyRef.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	trim, err := buildOwnerTrimPlan(t.Context(), policy, preview)
	if err != nil {
		t.Fatal(err)
	}
	contracts.config.Plan.Network = network
	contracts.config.Plan.DeploymentId = root.config.DeploymentId
	contracts.config.Plan.RunDirectory = root.config.RunDirectory
	contracts.config.Plan.Runtime = rootReceiptProfile{RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion, RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash}
	contracts.publishConfig()
	f := &bootstrapChainFixture{path: filepath.Join(configDirectory, "chain.json"), root: root, contracts: contracts, client: client, census: census,
		config: bootstrapChainConfig{Schema: bootstrapChainConfigSchema, DeploymentId: root.config.DeploymentId, Netuid: 25, Network: network, RunDirectory: root.config.RunDirectory,
			OwnerTrimPolicy: policyRef, OwnerTrimPlan: bootstrapRootTestWrite(t, filepath.Join(configDirectory, "trim-plan.json"), trim),
			Contracts: bootstrapRootTestWrite(t, contracts.configPath, contracts.config), Root: bootstrapRootTestWrite(t, root.configPath, root.config)}}
	for i, role := range policy.Preserve {
		fixture := newBootstrapChainValidatorFixture(t, f, policy, i)
		f.validators = append(f.validators, fixture)
		f.config.Validators = append(f.config.Validators, bootstrapChainValidator{ValidatorId: uint64(i + 1), subnetIdentityExpectation: role.subnetIdentityExpectation,
			Config: fixture.publish(t), Role: []string{"majority", "secondary"}[i], Implementation: "sn/validator", ApprovalPublicKey: fixture.config.OwnerRecycleApproval.Signer})
	}
	f.rootRole = newBootstrapChainRootFixture(t, f)
	bootstrapRootTestWrite(t, f.path, f.config)
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// The public dispatcher receives only the exact local preparation acceptance.
func (self *bootstrapChainFixture) command(ctx context.Context, command string, stdout, stderr io.Writer, extra ...string) int {
	args := []string{"bootstrap-chain", command, "--config", self.path}
	if command != "plan" {
		args = append(args, "--run-dir", self.config.RunDirectory, "--accept-plan-hash", self.preparation.Plan.ContentHash)
	}
	return runMain(ctx, append(args, extra...), stdout, stderr)
}

// Strict result decoding catches partial JSON and accidental authority fields.
func (self *bootstrapChainFixture) result(t *testing.T, command string) bootstrapChainResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := self.command(t.Context(), command, &stdout, &stderr); code != 0 {
		t.Fatalf("%s exit %d: %s", command, code, stderr.String())
	}
	var result bootstrapChainResult
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// Snapshot all authoritative files and markers, not only public result counters.
func (self *bootstrapChainFixture) journals(t *testing.T) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, path := range self.preparation.childPaths() {
		for _, candidate := range []string{path, path + ".lock"} {
			raw, err := os.ReadFile(candidate)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			result[candidate] = string(raw)
		}
	}
	return result
}

// Real custody owners are prepared once while every chain phase stays pending.
func TestBootstrapChainCommandPreparesExistingCustodyOffline(t *testing.T) {
	f := newBootstrapChainFixture(t)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "plan", &stdout, &stderr); code != 0 {
		t.Fatalf("plan exit %d: %s", code, stderr.String())
	}
	var plan bootstrapChainPlan
	if err := decodePlanJson(stdout.Bytes(), &plan); err != nil || !reflect.DeepEqual(plan, f.preparation.Plan) {
		t.Fatal("plan changed its exact prepared inputs", err)
	}
	if len(f.journals(t)) != 0 {
		t.Fatal("read-only planning opened custody")
	}
	result := f.result(t, "apply")
	if !result.LocalPreparationComplete || result.NetworkEffects || result.ActivationReady || len(result.PendingChainPhases) != 5 ||
		result.OwnerTrimStatus != "retained-review-execution-blocked" || result.UrValidatorsStatus != "two-signed-production-configs-verified-live-admission-pending" || !result.UrValidatorConfigsVerified ||
		result.Contracts.Status != "signature-awaiting-import" || result.Contracts.Attempts != 0 || result.Contracts.InstallationComplete ||
		!result.Root.LocalCustodyComplete || result.Root.SignatureStatus != "awaiting-import" || result.Root.Observations != 0 || result.Root.Broadcasts != 0 || result.Root.ActivationReady {
		t.Fatalf("local preparation hid pending authority or effects: %+v", result)
	}
	original := f.journals(t)
	if len(original) != 10 {
		t.Fatalf("did not reach all five real journal owners: %d", len(original))
	}
	if repeated := f.result(t, "resume"); !reflect.DeepEqual(result, repeated) || !reflect.DeepEqual(original, f.journals(t)) {
		t.Fatal("idempotent resume changed original custody or its checked preparation")
	}
	if len(f.contracts.counts) != 0 || len(f.contracts.writes) != 0 {
		t.Fatal("offline composition invoked a configured RPC")
	}
	if code := f.command(t.Context(), "apply", io.Discard, &stderr); code != 3 || !reflect.DeepEqual(original, f.journals(t)) {
		t.Fatal("duplicate apply acquired another allowance", code, stderr.String())
	}
}

// Existing external public-signature commands remain the sole import surfaces.
// Their original signed bytes survive preparation recovery with no new attempt.
func TestBootstrapChainResumePreservesOriginalSignedCustody(t *testing.T) {
	f := newBootstrapChainFixture(t)
	f.result(t, "apply")
	if result, code, diagnostic := f.contracts.command("resume", "--signed-transaction", f.contracts.signedPath, "--signed-transaction-hash", f.contracts.signedHash); code != 0 || result.Status != "signed-custody-complete" {
		t.Fatalf("existing contract import: %d %s %+v", code, diagnostic, result)
	}
	receipt := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "native-signature.json"), f.root.offline.receipt(t))
	f.root.result(t, "resume", "--signature-file", receipt.Path, "--signature-sha256", receipt.Sha256)
	original := f.journals(t)
	result := f.result(t, "resume")
	if result.Contracts.TransactionHash != f.contracts.tx.Hash().Hex() || result.Contracts.Attempts != 0 || result.Root.ExtrinsicHash == "" || result.Root.SignatureStatus != "retained" || result.Root.Broadcasts != 0 {
		t.Fatalf("original signature custody changed: %+v", result)
	}
	for path, raw := range original {
		if path != filepath.Join(f.config.RunDirectory, bootstrapChainStateFile) && f.journals(t)[path] != raw {
			t.Fatalf("preparation changed a child's original signed bytes: %s", path)
		}
	}
	stable := f.journals(t)
	if repeated := f.result(t, "resume"); !reflect.DeepEqual(result, repeated) || !reflect.DeepEqual(stable, f.journals(t)) || len(f.contracts.counts) != 0 {
		t.Fatal("signed preparation resume consumed another allowance")
	}
}

// An interrupted initial claim is recoverable only in its exact pre-child state.
func TestBootstrapChainInterruptedInitialClaim(t *testing.T) {
	for _, boundary := range []string{"marker-synced", "progress-synced"} {
		t.Run(boundary, func(t *testing.T) {
			f := newBootstrapChainFixture(t)
			injected := errors.New("synthetic initial claim interruption")
			store, err := openBootstrapChainStore(f.preparation, true, func(observed string) error {
				if observed == boundary {
					return injected
				}
				return nil
			})
			if store != nil || !errors.Is(err, injected) {
				t.Fatal("claim did not stop at the durable boundary", err)
			}
			for _, path := range f.preparation.childPaths()[1:] {
				if _, err := os.Lstat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("interrupted initial claim opened a child", err)
				}
			}
			f.result(t, "resume")
		})
	}
}

// Barriers stop after actual child durability but before parent acknowledgement.
// Reopening reconciles the same child records and never repeats their creation.
func TestBootstrapChainInterruptedChildProgress(t *testing.T) {
	for _, boundary := range []string{"contracts-retained", "root-retained"} {
		t.Run(boundary, func(t *testing.T) {
			f := newBootstrapChainFixture(t)
			store, err := openBootstrapChainStore(f.preparation, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("synthetic child acknowledgement interruption")
			_, err = advanceBootstrapChain(t.Context(), store, func(observed string) error {
				if observed == boundary {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatal("child boundary was not reached", err)
			}
			if err := store.close(); err != nil {
				t.Fatal(err)
			}
			original := f.journals(t)
			f.result(t, "resume")
			for path, raw := range original {
				if path != filepath.Join(f.config.RunDirectory, bootstrapChainStateFile) && f.journals(t)[path] != raw {
					t.Fatalf("recovery replaced a retained child: %s", path)
				}
			}
		})
	}
}

// Failure after rename is deliberately ambiguous: the current instance stops,
// and only a new exclusive owner may inspect the durable record and continue.
func TestBootstrapChainAmbiguousProgressRequiresReopen(t *testing.T) {
	f := newBootstrapChainFixture(t)
	store, err := openBootstrapChainStore(f.preparation, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	injected := errors.New("synthetic directory sync failure after rename")
	store.syncDirectory = func(*os.File) error { return injected }
	if _, err := advanceBootstrapChain(t.Context(), store, nil); !errors.Is(err, injected) {
		t.Fatal("did not stop at actual post-rename sync", err)
	}
	if _, err := advanceBootstrapChain(t.Context(), store, nil); !errors.Is(err, injected) {
		t.Fatal("ambiguous owner reused its allowance", err)
	}
	if _, err := os.Lstat(filepath.Join(f.config.RunDirectory, bootstrapRootProgressFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ambiguous parent progressed into root custody", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	f.result(t, "resume")
}

// Lost stdout cannot roll back already durable child state or phase completion.
func TestBootstrapChainOutputFailureAndExclusiveOwnership(t *testing.T) {
	f := newBootstrapChainFixture(t)
	var stderr bytes.Buffer
	if code := f.command(t.Context(), "apply", bootstrapRootFailedWriter{}, &stderr); code != 1 {
		t.Fatal("stdout failure did not follow real preparation", code, stderr.String())
	}
	original := f.journals(t)
	store, err := openBootstrapChainStore(f.preparation, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code := f.command(t.Context(), "resume", io.Discard, &stderr); code != 3 || !strings.Contains(stderr.String(), "already has a preparation owner") {
		t.Fatal("second invocation entered owned preparation", code, stderr.String())
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	f.result(t, "resume")
	if !reflect.DeepEqual(original, f.journals(t)) {
		t.Fatal("output recovery rewrote authoritative custody")
	}
}

// A missing completed journal or lock is not proof that its action never ran.
func TestBootstrapChainMissingCompletedStateNeverRecreates(t *testing.T) {
	for index := range 5 {
		for _, suffix := range []string{"", ".lock"} {
			t.Run([]string{"chain", "contract", "root-progress", "root-custody", "root-service"}[index]+suffix, func(t *testing.T) {
				f := newBootstrapChainFixture(t)
				f.result(t, "apply")
				path := f.preparation.childPaths()[index] + suffix
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				original := f.journals(t)
				var stdout, stderr bytes.Buffer
				if code := f.command(t.Context(), "resume", &stdout, &stderr); code == 0 || stdout.Len() != 0 || !reflect.DeepEqual(original, f.journals(t)) {
					t.Fatal("lost completed state was recreated", code, stderr.String())
				}
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing path reappeared", err)
				}
			})
		}
	}
}

// Exact raw pins include whitespace and transitive signed-input references.
// Rechecking before ownership must leave every existing journal untouched.
func TestBootstrapChainStaleInputsRefuseBeforeMutation(t *testing.T) {
	for index := range 9 {
		t.Run([]string{"config", "trim-policy", "trim-plan", "contracts", "root", "ur-one", "ur-two", "artifacts", "root-service"}[index], func(t *testing.T) {
			f := newBootstrapChainFixture(t)
			f.result(t, "apply")
			original := f.journals(t)
			paths := []string{f.path, f.config.OwnerTrimPolicy.Path, f.config.OwnerTrimPlan.Path, f.config.Contracts.Path, f.config.Root.Path, f.config.Validators[0].Config.Path, f.config.Validators[1].Config.Path, f.contracts.config.Plan.Artifacts.Path, f.root.config.RootService.Path}
			file, err := os.OpenFile(paths[index], os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := file.WriteString("\n")
			if err := errors.Join(writeErr, file.Close()); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := f.command(t.Context(), "resume", &stdout, &stderr); code == 0 || stdout.Len() != 0 || !reflect.DeepEqual(original, f.journals(t)) {
				t.Fatal("stale preparation reached custody", code, stderr.String())
			}
		})
	}
}

// Hash recomputation is not approval, nor can a root seat count as a UR role.
func TestBootstrapChainRejectsConflictingRolesAndChildApprovals(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *bootstrapChainFixture)
	}{
		{"testnet-network", func(t *testing.T, f *bootstrapChainFixture) { f.config.Network.EvmChainId = 945 }},
		{"deployment", func(t *testing.T, f *bootstrapChainFixture) { f.config.DeploymentId = "another-synthetic-deployment" }},
		{"duplicate-role-id", func(t *testing.T, f *bootstrapChainFixture) {
			f.config.Validators[1].ValidatorId = f.config.Validators[0].ValidatorId
		}},
		{"duplicate-hotkey", func(t *testing.T, f *bootstrapChainFixture) {
			f.config.Validators[1].subnetIdentityExpectation = f.config.Validators[0].subnetIdentityExpectation
		}},
		{"duplicate-config", func(t *testing.T, f *bootstrapChainFixture) {
			f.config.Validators[1].Config = f.config.Validators[0].Config
		}},
		{"copied-config", func(t *testing.T, f *bootstrapChainFixture) {
			raw, err := os.ReadFile(f.config.Validators[0].Config.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.config.Validators[1].Config.Path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			f.config.Validators[1].Config.Sha256 = f.config.Validators[0].Config.Sha256
		}},
		{"root-counted-as-ur", func(t *testing.T, f *bootstrapChainFixture) {
			f.config.Validators[0].Hotkey = f.root.offline.trust.Hotkey
		}},
		{"ur-generation", func(t *testing.T, f *bootstrapChainFixture) {
			value := *f.config.Validators[0].RegistrationBlock + 1
			f.config.Validators[0].RegistrationBlock = &value
		}},
		{"contract-signature", func(t *testing.T, f *bootstrapChainFixture) {
			f.contracts.config.Signature = strings.Repeat("00", 64)
			f.config.Contracts = bootstrapRootTestWrite(t, f.contracts.configPath, f.contracts.config)
		}},
		{"root-signature", func(t *testing.T, f *bootstrapChainFixture) {
			service := copyRootServiceConfig(f.root.plan.Service)
			service.Packet.Approval.Signature = strings.Repeat("00", 64)
			f.root.config.RootService = bootstrapRootTestWrite(t, f.root.config.RootService.Path, service)
			f.config.Root = bootstrapRootTestWrite(t, f.root.configPath, f.root.config)
		}},
	}
	for _, candidate := range cases {
		t.Run(candidate.name, func(t *testing.T) {
			f := newBootstrapChainFixture(t)
			candidate.change(t, f)
			bootstrapRootTestWrite(t, f.path, f.config)
			var stdout, stderr bytes.Buffer
			if code := f.command(t.Context(), "plan", &stdout, &stderr); code != 2 || stdout.Len() != 0 || len(f.journals(t)) != 0 {
				t.Fatal("conflicting identity or unsigned authority was admitted", code, stderr.String())
			}
		})
	}
}

// Local acceptance has a separate domain; online and signing switches do not
// exist on this dispatcher even when the supplied child plan is approved.
func TestBootstrapChainRejectsAlternateAuthorityAndOnlineFlags(t *testing.T) {
	f := newBootstrapChainFixture(t)
	for _, extra := range [][]string{{"--accept-plan-hash", f.preparation.Root.ContentHash}, {"--accept-plan-hash", f.contracts.config.Plan.hash()}, {"--online"}, {"--submit"}, {"--signature-file", f.contracts.signedPath}} {
		var stdout, stderr bytes.Buffer
		if code := f.command(t.Context(), "apply", &stdout, &stderr, extra...); code == 0 || stdout.Len() != 0 || len(f.journals(t)) != 0 {
			t.Fatal("alternate authority or network surface acquired custody", code, stderr.String())
		}
	}
}

// The public root loader keeps its behavior, while composed decoding consumes
// the pinned read even if its pathname is atomically replaced before decoding.
func TestBootstrapChainRootDecodeUsesPinnedBytes(t *testing.T) {
	f := newBootstrapRootFixture(t)
	raw, digest, err := readBootstrapRootFile(t.Context(), f.configPath, rootServiceStoreLimit)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeBootstrapRootPlan(t.Context(), f.configPath, raw, digest)
	if err != nil || !reflect.DeepEqual(decoded, f.plan) {
		t.Fatal("byte-based decode changed ordinary root plan behavior", err)
	}
	if _, err := decodeBootstrapRootPlan(t.Context(), f.configPath, raw, rootObjectHash("synthetic unrelated input")); err == nil {
		t.Fatal("root byte decode accepted an unrelated raw-file pin")
	}
	changed := f.config
	changed.DeploymentId = "synthetic-substituted-deployment"
	replacement := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.configPath), "replacement.json"), changed)
	if err := os.Rename(replacement.Path, f.configPath); err != nil {
		t.Fatal(err)
	}
	pinned, err := decodeBootstrapRootPlan(t.Context(), f.configPath, raw, digest)
	if err != nil || !reflect.DeepEqual(pinned, f.plan) {
		t.Fatal("pinned root decode reopened a substituted pathname", err)
	}
	reopened, err := loadBootstrapRootPlan(t.Context(), f.configPath)
	if err != nil || reopened.DeploymentId != changed.DeploymentId || reopened.ConfigSha256 == pinned.ConfigSha256 || reopened.ContentHash == pinned.ContentHash {
		t.Fatal("causal control did not expose the distinct replacement file", err)
	}
}

// Recomputing imported content hashes cannot substitute a different selected
// removal set for the one rebuilt from the retained complete census.
func TestBootstrapChainRejectsResealedTrimSelection(t *testing.T) {
	f := newBootstrapChainFixture(t)
	raw, err := os.ReadFile(f.config.OwnerTrimPlan.Path)
	if err != nil {
		t.Fatal(err)
	}
	var trim ownerTrimPlan
	if err := decodePlanJson(raw, &trim); err != nil {
		t.Fatal(err)
	}
	trim.Best.ExpectedRemoved[0].Hotkey = f.config.Validators[0].Hotkey
	trim.ContentHash, err = ownerTrimPlanHash(trim)
	if err != nil {
		t.Fatal(err)
	}
	f.config.OwnerTrimPlan = bootstrapRootTestWrite(t, f.config.OwnerTrimPlan.Path, trim)
	bootstrapRootTestWrite(t, f.path, f.config)
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "plan", &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "trim selection differs") || len(f.journals(t)) != 0 {
		t.Fatal("resealed removal selection bypassed reconstruction", code, stderr.String())
	}
}

// Initial recovery refuses both an incomplete marker and any evidence that a
// child might already own state before the parent claim was acknowledged.
func TestBootstrapChainInitialRecoveryRefusesAmbiguousState(t *testing.T) {
	for _, change := range []string{"partial-marker", "child-state", "corrupt-progress"} {
		t.Run(change, func(t *testing.T) {
			f := newBootstrapChainFixture(t)
			injected := errors.New("synthetic interrupted preparation claim")
			store, err := openBootstrapChainStore(f.preparation, true, func(string) error { return injected })
			if store != nil || !errors.Is(err, injected) {
				t.Fatal("initial boundary was not reached", err)
			}
			path := filepath.Join(f.config.RunDirectory, bootstrapChainStateFile)
			switch change {
			case "partial-marker":
				path += ".lock"
			case "child-state":
				path = filepath.Join(f.config.RunDirectory, evmCreateStateFile)
			}
			if err := os.WriteFile(path, []byte("synthetic incomplete retained state\n"), 0600); err != nil {
				t.Fatal(err)
			}
			original := f.journals(t)
			var stdout, stderr bytes.Buffer
			if code := f.command(t.Context(), "resume", &stdout, &stderr); code != 3 || stdout.Len() != 0 || !reflect.DeepEqual(original, f.journals(t)) {
				t.Fatal("ambiguous claim renewed preparation", code, stderr.String())
			}
		})
	}
}

// Losing a completed child's entire path pair must not look like its first
// invocation. This also covers loss of the complete nested root custody tree.
func TestBootstrapChainLostWholeChildRefusesFreshAllowance(t *testing.T) {
	for _, child := range []string{"contracts", "root"} {
		t.Run(child, func(t *testing.T) {
			f := newBootstrapChainFixture(t)
			f.result(t, "apply")
			paths := f.preparation.childPaths()[1:2]
			if child == "root" {
				paths = f.preparation.childPaths()[2:]
			}
			for _, path := range paths {
				for _, candidate := range []string{path, path + ".lock"} {
					if err := os.Remove(candidate); err != nil {
						t.Fatal(err)
					}
				}
			}
			original := f.journals(t)
			var stdout, stderr bytes.Buffer
			if code := f.command(t.Context(), "resume", &stdout, &stderr); code != 1 || stdout.Len() != 0 || !reflect.DeepEqual(original, f.journals(t)) || !strings.Contains(stderr.String(), "completed child disappeared") {
				t.Fatal("lost complete custody tree became a fresh allowance", code, stderr.String())
			}
		})
	}
}
