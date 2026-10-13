package main

// Exercises the real simulator key consumers with owned raw keys and genuine
// policy-depth signed proofs/closures, without a campaign or external service.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

// Owns a two-operator, one-epoch filesystem fixture; the configured proof depth
// is unchanged. The collector observer stops before its first artifact write.
type simulatorClientSeedTestFixture struct {
	cfg        *ResolvedConfig
	stateRoot  string
	runRoot    string
	root       string
	seedPath   string
	seed       []byte
	terminal   *ScenarioObservation
	window     *ScenarioAcceptanceWindow
	proofPaths []string
	closure    string
}

// Builds each authority through the actual append and all-operator settlement
// APIs. Every ledger owner is closed even if a later custody assertion fails.
func newSimulatorClientSeedTestFixture(t *testing.T) *simulatorClientSeedTestFixture {
	t.Helper()
	return newSimulatorClientSeedTestFixtureWithOperatorSeeds(t, testResolvedConfig(t), nil)
}

// Keeps the original shared-key fixture unchanged by default; explicit seeds
// reproduce the provisioner's independent key for each operator.
func newSimulatorClientSeedTestFixtureWithOperatorSeeds(t *testing.T, cfg *ResolvedConfig, operatorSeedKVs map[uint64][]byte) *simulatorClientSeedTestFixture {
	t.Helper()
	cfg.Config.Topology.Validators = 1
	stateRoot := t.TempDir()
	root := filepath.Join(stateRoot, "runtime", "validator-1", "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := &simulatorClientSeedTestFixture{
		cfg: cfg, stateRoot: stateRoot, runRoot: t.TempDir(), root: root,
		seedPath: filepath.Join(root, "operators", "no-1", "client.key"),
		seed:     bytes.Repeat([]byte{0x41}, ed25519.SeedSize),
		terminal: &ScenarioObservation{Validators: []ValidatorObservation{{ValidatorID: 1, SelfUID: 12}}},
		window:   &ScenarioAcceptanceWindow{FirstEpoch: 42, EpochCount: 1, StartBlock: 100, EpochBlocks: 10},
		closure:  validatorpkg.AttemptSettlementClosurePath(root, 42),
	}
	participants := make([]validatorpkg.AttemptSettlementParticipant, cfg.Config.Topology.Operators)
	ledgers := make([]*validatorpkg.AttemptLedger, len(participants))
	serverKeys := make([]ed25519.PrivateKey, len(participants))
	validatorKeys := make([]ed25519.PrivateKey, len(participants))
	for index := range participants {
		noID := index + 1
		seed := fixture.seed
		if operatorSeedKVs != nil {
			seed = operatorSeedKVs[uint64(noID)]
		}
		if len(seed) != ed25519.SeedSize {
			t.Fatalf("operator %d fixture seed has an invalid size", noID)
		}
		if index == 0 {
			fixture.seed = append([]byte(nil), seed...)
		}
		key := ed25519.NewKeyFromSeed(seed)
		validatorKeys[index] = key
		operatorRoot := filepath.Join(root, "operators", fmt.Sprintf("no-%d", noID))
		if err := os.MkdirAll(operatorRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(operatorRoot, "client.key"), seed, 0o600); err != nil {
			t.Fatal(err)
		}
		ledger, err := validatorpkg.NewAttemptLedger(operatorRoot, validatorpkg.AttemptLedgerIdentity{
			DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID,
			GenesisHash: strings.ToLower(cfg.Public.Chain.GenesisHash), Netuid: cfg.Netuid,
			ValidatorID: 1, ValidatorUID: 12, NoID: uint64(noID),
		}, key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := ledger.Close(); err != nil {
				t.Error(err)
			}
		})
		stats := validatorpkg.NewStatsEngine(validatorpkg.StatsConfig{AMin: 1})
		if err := stats.AttachAttemptLedger(ledger, operatorRoot); err != nil {
			t.Fatal(err)
		}
		participants[index] = validatorpkg.AttemptSettlementParticipant{NoID: uint64(noID), StateDir: operatorRoot, Stats: stats}
		ledgers[index] = ledger
		serverKeys[index] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x51 + index)}, ed25519.SeedSize))
		fixture.terminal.Operators = append(fixture.terminal.Operators, OperatorObservation{
			NoID: noID, VerifyKeys: []VerifyKeyObservation{{ServerKeyID: 1, PublicKey: serverKeys[index].Public().(ed25519.PublicKey)}},
		})
	}
	if err := validatorpkg.AdvanceAttemptSettlementEpoch(root, 42, validatorpkg.AttemptBoundary{}, participants); err != nil {
		t.Fatal(err)
	}
	boundary := validatorpkg.AttemptBoundary{SettlementEpoch: 42, EVMBlock: 100, EVMBlockHash: finalTestHex(42)}
	for index, participant := range participants {
		key := validatorKeys[index]
		vpk := key.Public().(ed25519.PublicKey)
		trailID := finalAttemptFixtureID(uint64(index + 1))
		nonce := bytes.Repeat([]byte{byte(index + 1)}, connect.VerifyNonceSize)
		trail := make([]connect.Id, cfg.Policy.Verify.TrailDepth)
		hops := make([]connect.VerifyProofHop, len(trail))
		for hopIndex := range trail {
			trail[hopIndex] = finalAttemptFixtureID(uint64((index+1)*100 + hopIndex))
			hops[hopIndex] = connect.VerifyProofHop{ClientId: trail[hopIndex], TimeMs: uint64(1000 + hopIndex)}
		}
		record := validatorpkg.AttemptRecord{Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: len(trail), Disposition: validatorpkg.AttemptDispositionPending}
		for hopIndex := 1; hopIndex < len(trail); hopIndex++ {
			assign, err := connect.BuildVerifyAssignMessage(1, trailID, nonce, vpk, byte(len(trail)), trail[:hopIndex+1])
			if err != nil {
				t.Fatal(err)
			}
			record.Assignments = append(record.Assignments, validatorpkg.AttemptAssignment{
				Trail: append([]connect.Id(nil), trail[:hopIndex]...), NextHop: trail[hopIndex], ServerKeyID: 1,
				AssignMessage: assign, AssignSignature: ed25519.Sign(serverKeys[index], assign),
				Binding: validatorpkg.AttemptBinding{ClientID: trail[hopIndex], FleetID: finalTestHex(0), Hotkey: finalTestHex(0)},
			})
			if _, err := ledgers[index].Append(record); err != nil {
				t.Fatal(err)
			}
			record.Assignments[hopIndex-1].Confirmed, record.Assignments[hopIndex-1].HasLatency = true, true
		}
		finalMessage, err := connect.BuildVerifyFinalMessage(1, trailID, nonce, vpk, byte(len(trail)), hops)
		if err != nil {
			t.Fatal(err)
		}
		extend, err := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(len(trail)), trail)
		if err != nil {
			t.Fatal(err)
		}
		digest, pathID := connect.VerifyFinalDigest(finalMessage), validatorpkg.TrailPathId(trailID, vpk, 1)
		record.Proof = &validatorpkg.ProofRecord{
			Version: 1, Epoch: 42, TrailId: trailID, ServerNonce: nonce, Vpk: vpk, M: len(trail), Hops: hops,
			ServerKeyId: 1, FinalSig: ed25519.Sign(serverKeys[index], finalMessage), VerifierSig: ed25519.Sign(key, extend),
			FinalDigest: digest[:], VpkSig: ed25519.Sign(key, finalMessage), Coverage: uint64(len(trail) - 1),
			PathId: pathID[:], CompleteTimeMs: hops[len(hops)-1].TimeMs,
		}
		record.Disposition = validatorpkg.AttemptDispositionComplete
		if _, err := ledgers[index].Append(record); err != nil {
			t.Fatal(err)
		}
		if err := participant.Stats.AttachAttemptLedger(ledgers[index], participant.StateDir); err != nil {
			t.Fatal(err)
		}
		proof, err := json.Marshal(record.Proof)
		if err != nil {
			t.Fatal(err)
		}
		proofPath := filepath.Join(participant.StateDir, "proofs.jsonl")
		if err := os.WriteFile(proofPath, append(proof, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.proofPaths = append(fixture.proofPaths, proofPath)
	}
	boundary.EVMBlock = 109
	if err := validatorpkg.AdvanceAttemptSettlementEpoch(root, 43, boundary, participants); err != nil {
		t.Fatal(err)
	}
	intents, err := json.Marshal(struct {
		Schema string `json:"schema"`
	}{Schema: validatorpkg.SteeringIntentSchema})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "steering-intents.json"), intents, 0o600); err != nil {
		t.Fatal(err)
	}
	writeFinalPathIdentityTestPublic(t, stateRoot, cfg.Config.Deployment.DeploymentID, map[uint64][]FinalOperatorPathIdentity{1: finalPathIdentityTestKeyVector(validatorKeys)})
	return fixture
}

// Uses the actual public proof/closure verifier, or the real collector read up
// to an explicit pre-write sentinel. A setup failure cannot count as refusal.
func (self *simulatorClientSeedTestFixture) read(t *testing.T, kind string) error {
	t.Helper()
	switch kind {
	case "proofs":
		counts, err := inspectValidatorPathProofs(self.cfg, self.stateRoot, 1, self.terminal.Operators)
		if err == nil && (len(counts) != 2 || counts[1] != 1 || counts[2] != 1) {
			t.Fatalf("signed proof fixture counts=%v", counts)
		}
		return err
	case "collection":
		stop := errors.New("owned collection key observation")
		observed := 0
		_, err := collectFinalValidatorInputsWithSeedObserver(self.cfg, self.stateRoot, self.runRoot, self.terminal, "unit", self.window, time.Unix(1, 0), time.Unix(2, 0), func(validatorID int, publicKey [ed25519.PublicKeySize]byte) error {
			observed++
			want := ed25519.NewKeyFromSeed(self.seed).Public().(ed25519.PublicKey)
			if validatorID != 1 || !bytes.Equal(publicKey[:], want) {
				t.Fatal("collector did not observe the actual fixture public key")
			}
			return stop
		})
		entries, readErr := os.ReadDir(self.runRoot)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("collector crossed the pre-write sentinel: entries=%d error=%v", len(entries), readErr)
		}
		if errors.Is(err, stop) && observed == 1 {
			return nil
		}
		if observed != 0 || err == nil {
			t.Fatalf("collector observer count=%d error=%v", observed, err)
		}
		return err
	case "closures":
		return waitFinalValidatorSettlementClosuresWithWait(context.Background(), self.cfg, self.stateRoot, self.terminal, self.window, time.Now().Add(time.Hour), time.Hour, func(context.Context, time.Duration) error {
			t.Error("fully published genuine closure unexpectedly needed a retry")
			return errors.New("unexpected fixture retry")
		})
	default:
		t.Fatalf("unknown simulator key consumer %q", kind)
		return nil
	}
}

// Confines negative custody changes to the test's actual key and immediate
// parent, preserving the same key bytes and all signed authority.
func (self *simulatorClientSeedTestFixture) makeUnsafe(t *testing.T, kind string) {
	t.Helper()
	parent := filepath.Dir(self.seedPath)
	var err error
	switch kind {
	case "leaf-symlink":
		err = os.Rename(self.seedPath, self.seedPath+".owned")
		if err == nil {
			err = os.Symlink("client.key.owned", self.seedPath)
		}
	case "hardlink":
		err = os.Link(self.seedPath, self.seedPath+".owned")
	case "ancestor-symlink":
		err = os.Rename(parent, parent+".owned")
		if err == nil {
			err = os.Symlink("no-1.owned", parent)
		}
	case "file-0644":
		err = os.Chmod(self.seedPath, 0o644)
	case "file-0660":
		err = os.Chmod(self.seedPath, 0o660)
	case "parent-0775":
		err = os.Chmod(parent, 0o775)
	case "parent-0777":
		err = os.Chmod(parent, 0o777)
	default:
		t.Fatalf("unknown custody mutation %q", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// Requires refusal at the caller's key boundary, not a later signature error
// or a missing fixture. The old caller-specific diagnostics remain stable.
func requireSimulatorSeedRefusal(t *testing.T, kind string, err error) {
	t.Helper()
	markers := map[string]string{
		"proofs":     "client seed is unavailable or invalid",
		"collection": "path identity seed is unavailable",
		"closures":   "terminal closure path identity is unavailable",
	}
	if err == nil || !strings.Contains(err.Error(), markers[kind]) {
		t.Fatalf("%s did not refuse at its seed boundary: %v", kind, err)
	}
}

// Keeps every raw byte, inode and mode stable across admission or refusal.
func preserveSimulatorSeedTestFiles(t *testing.T, fixture *simulatorClientSeedTestFixture) func() {
	t.Helper()
	paths := append([]string{fixture.seedPath, fixture.closure}, fixture.proofPaths...)
	infos := make([]os.FileInfo, len(paths))
	payloads := make([][]byte, len(paths))
	for index, path := range paths {
		var err error
		infos[index], err = os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		payloads[index], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	parent := filepath.Dir(fixture.seedPath)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		for index, path := range paths {
			info, err := os.Lstat(path)
			if err != nil || !os.SameFile(infos[index], info) || info.Mode() != infos[index].Mode() {
				t.Fatalf("consumer changed owned file identity/mode %s: %v", path, err)
			}
			payload, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(payload, payloads[index]) {
				t.Fatalf("consumer changed owned file bytes %s: %v", path, err)
			}
		}
		info, err := os.Lstat(parent)
		if err != nil || !os.SameFile(parentInfo, info) || info.Mode() != parentInfo.Mode() {
			t.Fatalf("consumer changed owned parent identity/mode: %v", err)
		}
	}
}

// Forces every alias/mode case at the real call site with a successful signed
// baseline first; aggregate literals bind each causal failure to its own root.
func checkSimulatorSeedCustodyRefusal(t *testing.T, consumer, label string) {
	t.Helper()
	var accepted []string
	for _, kind := range []string{"leaf-symlink", "hardlink", "ancestor-symlink", "file-0644", "file-0660", "parent-0775", "parent-0777"} {
		fixture := newSimulatorClientSeedTestFixture(t)
		if err := fixture.read(t, consumer); err != nil {
			t.Fatalf("%s private raw fixture: %v", consumer, err)
		}
		fixture.makeUnsafe(t, kind)
		unchanged := preserveSimulatorSeedTestFiles(t, fixture)
		err := fixture.read(t, consumer)
		unchanged()
		if err == nil {
			accepted = append(accepted, kind)
		} else {
			requireSimulatorSeedRefusal(t, consumer, err)
		}
	}
	if len(accepted) != 0 {
		t.Fatalf("simulator %s accepted unsafe client-key custody: %v", label, accepted)
	}
}

// Valid server and validator signatures do not excuse an aliased/shared key.
func TestSimulatorClientSeedCustodyProofInspectionRefusesUnsafeKey(t *testing.T) {
	checkSimulatorSeedCustodyRefusal(t, "proofs", "path-proof inspection")
}

// Collection must refuse before publishing any artifact or exposing the VPK.
func TestSimulatorClientSeedCustodyCollectionRefusesUnsafeKey(t *testing.T) {
	checkSimulatorSeedCustodyRefusal(t, "collection", "final collection")
}

// A genuine terminal closure is not permission to consume an unsafe key path.
func TestSimulatorClientSeedCustodySettlementWaitRefusesUnsafeKey(t *testing.T) {
	checkSimulatorSeedCustodyRefusal(t, "closures", "settlement wait")
}

// Read-only keys and non-writable readable parents remain valid raw authority.
func TestSimulatorClientSeedCustodyAcceptsPrivateRawKeys(t *testing.T) {
	fixture := newSimulatorClientSeedTestFixture(t)
	for _, parentMode := range []os.FileMode{0o700, 0o755} {
		if err := os.Chmod(filepath.Dir(fixture.seedPath), parentMode); err != nil {
			t.Fatal(err)
		}
		for _, fileMode := range []os.FileMode{0o400, 0o600} {
			if err := os.Chmod(fixture.seedPath, fileMode); err != nil {
				t.Fatal(err)
			}
			unchanged := preserveSimulatorSeedTestFiles(t, fixture)
			for _, consumer := range []string{"proofs", "collection", "closures"} {
				if err := fixture.read(t, consumer); err != nil {
					t.Fatalf("%s raw key mode=%o parent=%o: %v", consumer, fileMode, parentMode, err)
				}
			}
			unchanged()
		}
	}
}

// Simulator keys remain raw-only even though the release client separately
// permits bare hexadecimal and the generated hotkey parser has other grammar.
func TestSimulatorClientSeedCustodyKeepsRawOnlyGrammar(t *testing.T) {
	fixture := newSimulatorClientSeedTestFixture(t)
	hexSeed := hex.EncodeToString(fixture.seed)
	for _, encoded := range [][]byte{[]byte(hexSeed), []byte(" \t" + hexSeed + "\r\n"), []byte("0x" + hexSeed), bytes.Repeat([]byte{1}, 31), bytes.Repeat([]byte{1}, 33), bytes.Repeat([]byte{1}, 4097)} {
		if err := os.WriteFile(fixture.seedPath, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		unchanged := preserveSimulatorSeedTestFiles(t, fixture)
		for _, consumer := range []string{"proofs", "collection", "closures"} {
			requireSimulatorSeedRefusal(t, consumer, fixture.read(t, consumer))
		}
		unchanged()
	}
}

// Missing provisioned authority stays missing; readers must never generate it.
func TestSimulatorClientSeedCustodyMissingKeysNeverCreate(t *testing.T) {
	for _, missingParent := range []bool{false, true} {
		fixture := newSimulatorClientSeedTestFixture(t)
		path := fixture.seedPath
		if missingParent {
			path = filepath.Dir(path)
		}
		if err := os.Rename(path, path+".saved"); err != nil {
			t.Fatal(err)
		}
		for _, consumer := range []string{"proofs", "collection", "closures"} {
			requireSimulatorSeedRefusal(t, consumer, fixture.read(t, consumer))
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s recreated missing provisioned path: %v", consumer, err)
			}
		}
		savedSeed := path + ".saved"
		if missingParent {
			savedSeed = filepath.Join(savedSeed, "client.key")
		}
		data, err := os.ReadFile(savedSeed)
		if err != nil || !bytes.Equal(data, fixture.seed) {
			t.Fatalf("missing-path refusal altered preserved seed: %v", err)
		}
	}
}

// The custody fixtures retain actual cryptographic refusal after successful
// seed admission; neither proof inspection nor closure waiting is stubbed.
func TestSimulatorClientSeedCustodyControlsStillAuthenticate(t *testing.T) {
	fixture := newSimulatorClientSeedTestFixture(t)
	data, err := os.ReadFile(fixture.proofPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	var proof validatorpkg.ProofRecord
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	proof.FinalSig[0] ^= 1
	tampered, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.proofPaths[0], append(tampered, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.read(t, "proofs"); err == nil || !strings.Contains(err.Error(), "server FINAL signature") {
		t.Fatalf("proof inspection lost its real signature check: %v", err)
	}
	// Match the independent expected deployment before testing the unchanged
	// signed closure's domain; keep the original provisioned role keys.
	rewriteOperatorPathPublicTestFile(t, fixture, func(public *finalPublicIdentities) {
		public.DeploymentID = fixture.cfg.Config.Deployment.DeploymentID + "-foreign"
	})
	fixture.cfg.Config.Deployment.DeploymentID += "-foreign"
	if err := fixture.read(t, "closures"); err == nil || !strings.Contains(err.Error(), "signed validator/operator domain differs") {
		t.Fatalf("settlement wait lost its real signed domain check: %v", err)
	}
}
