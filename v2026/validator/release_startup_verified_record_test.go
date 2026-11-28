//go:build linux || darwin

package validator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

func releaseStartupVerifiedTestAuthority(t *testing.T) (*crv4.Keypair, releaseStartupVerifiedAuthority) {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{7})
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, 32)).Public().(ed25519.PublicKey)
	authority := releaseStartupVerifiedAuthority{chainID: 964, deploymentID: "sn25-test", validatorID: 1, hotkey: hotkey.PublicKey(), configHash: [32]byte{1}, lineage: map[[32]byte]bool{{1}: true},
		bridge: []ReleaseNativeGapBridgeEdge{{LastNativeEpoch: 25648, LastArtifactHash: "sha256:" + strings.Repeat("a", 64), FirstNativeEpoch: 25683}}}
	authority.serverKeys, err = releaseStartupVerifiedServerKeysForTest(map[uint64]map[byte]ed25519.PublicKey{1: {1: key}})
	if err != nil {
		t.Fatal(err)
	}
	return hotkey, authority
}

func releaseStartupVerifiedServerKeysForTest(keys map[uint64]map[byte]ed25519.PublicKey) ([]releaseStartupVerifiedServerKey, error) {
	var result []releaseStartupVerifiedServerKey
	for noID, versions := range keys {
		for version, key := range versions {
			if len(key) != ed25519.PublicKeySize {
				return nil, errors.New("invalid test key")
			}
			result = append(result, releaseStartupVerifiedServerKey{NoID: noID, Version: version, Key: fmt.Sprintf("%x", []byte(key))})
		}
	}
	sortReleaseStartupVerifiedServerKeys(result)
	return result, nil
}

// Writes one record from a process that verified two units and two entries.
func releaseStartupVerifiedTestRecord(t *testing.T, dir string, hotkey *crv4.Keypair, authority releaseStartupVerifiedAuthority) ([][32]byte, [][32]byte) {
	t.Helper()
	owner := newReleaseStartupVerified()
	var stderr bytes.Buffer
	owner.enableRecord(dir, hotkey, authority, &stderr)
	if !strings.Contains(stderr.String(), "no startup verified progress record yet") {
		t.Fatalf("a missing record was not reported: %q", stderr.String())
	}
	units := [][32]byte{{0xa1}, {0xa2}}
	for _, unit := range units {
		owner.add(unit)
	}
	intents := [][32]byte{{0xb1}, {0xb2}}
	owner.rememberIntents(intents, true)
	if err := owner.flush(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, releaseStartupVerifiedFileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("record is not a private file: %v", err)
	}
	return units, intents
}

// A later process under the same or a succeeding authority resumes every unit.
func TestReleaseStartupVerifiedRecordRoundTripsUnderItsAuthority(t *testing.T) {
	t.Parallel()
	hotkey, authority := releaseStartupVerifiedTestAuthority(t)
	dir := t.TempDir()
	units, intents := releaseStartupVerifiedTestRecord(t, dir, hotkey, authority)
	successor := authority
	successor.configHash = [32]byte{2}
	successor.lineage = map[[32]byte]bool{{2}: true, {1}: true}
	successor.bridge = append(append([]ReleaseNativeGapBridgeEdge(nil), authority.bridge...), ReleaseNativeGapBridgeEdge{LastNativeEpoch: 25800, LastArtifactHash: "sha256:" + strings.Repeat("b", 64), FirstNativeEpoch: 25803})
	added := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{4}, 32)).Public().(ed25519.PublicKey)
	successor.serverKeys = append(append([]releaseStartupVerifiedServerKey(nil), authority.serverKeys...), releaseStartupVerifiedServerKey{NoID: 1, Version: 2, Key: fmt.Sprintf("%x", []byte(added))})
	for _, current := range []releaseStartupVerifiedAuthority{authority, successor} {
		owner := newReleaseStartupVerified()
		var stderr bytes.Buffer
		owner.enableRecord(dir, hotkey, current, &stderr)
		if !strings.Contains(stderr.String(), "accepted: 2 units and 2 intents") {
			t.Fatalf("record under its own or a succeeding authority was refused: %q", stderr.String())
		}
		for _, unit := range units {
			if !owner.has(unit) {
				t.Fatal("an accepted record lost a unit")
			}
		}
		if owner.intentPrefix(intents) != 2 {
			t.Fatal("an accepted record lost its intent prefix")
		}
	}
	// The successor re-attests what it accepted under its own authority.
	owner := newReleaseStartupVerified()
	owner.enableRecord(dir, hotkey, successor, &bytes.Buffer{})
	if err := owner.flush(); err != nil {
		t.Fatal(err)
	}
	file, err := readReleaseStartupVerifiedFile(filepath.Join(dir, releaseStartupVerifiedFileName))
	if err != nil || file.ConfigHash != releaseHex32(successor.configHash) || len(file.Units) != 2 || len(file.BridgeEdges) != 2 || len(file.ServerKeys) != 2 {
		t.Fatalf("successor did not re-attest the accepted units: %+v %v", file, err)
	}
}

// Anything outside the stated trust, or any changed byte, verifies everything.
func TestReleaseStartupVerifiedRecordRefusesChangedAuthorityOrBytes(t *testing.T) {
	t.Parallel()
	hotkey, authority := releaseStartupVerifiedTestAuthority(t)
	other, err := crv4.KeypairFromSeed([32]byte{8})
	if err != nil {
		t.Fatal(err)
	}
	refused := func(name string, dir string, current releaseStartupVerifiedAuthority) {
		t.Helper()
		owner := newReleaseStartupVerified()
		var stderr bytes.Buffer
		owner.enableRecord(dir, hotkey, current, &stderr)
		if !strings.Contains(stderr.String(), "record ignored") || owner.has([32]byte{0xa1}) || owner.intentPrefix([][32]byte{{0xb1}, {0xb2}}) != 0 {
			t.Fatalf("%s: record was accepted: %q", name, stderr.String())
		}
	}
	mutated := func(t *testing.T, mutate func(*releaseStartupVerifiedFile), signer *crv4.Keypair) string {
		t.Helper()
		dir := t.TempDir()
		releaseStartupVerifiedTestRecord(t, dir, hotkey, authority)
		path := filepath.Join(dir, releaseStartupVerifiedFileName)
		file, err := readReleaseStartupVerifiedFile(path)
		if err != nil {
			t.Fatal(err)
		}
		mutate(file)
		digest, err := releaseStartupVerifiedSigningDigest(file)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := signer.Sign(digest[:])
		if err != nil {
			t.Fatal(err)
		}
		file.Signature = fmt.Sprintf("0x%x", signature)
		encoded, err := json.Marshal(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dir := t.TempDir()
	releaseStartupVerifiedTestRecord(t, dir, hotkey, authority)
	unrelated := authority
	unrelated.configHash, unrelated.lineage = [32]byte{3}, map[[32]byte]bool{{3}: true}
	refused("configuration outside the signed lineage", dir, unrelated)
	rotated := authority
	rotated.serverKeys = []releaseStartupVerifiedServerKey{{NoID: 1, Version: 1, Key: strings.Repeat("0", 64)}}
	refused("server key replaced", dir, rotated)
	rebridged := authority
	rebridged.bridge = []ReleaseNativeGapBridgeEdge{{LastNativeEpoch: 25649, LastArtifactHash: authority.bridge[0].LastArtifactHash, FirstNativeEpoch: 25683}}
	refused("bridge edge rewritten", dir, rebridged)
	unbridged := authority
	unbridged.bridge = nil
	refused("bridge removed", dir, unbridged)
	foreign := authority
	foreign.validatorID = 2
	refused("another validator", dir, foreign)
	refused("signed by another hotkey", mutated(t, func(*releaseStartupVerifiedFile) {}, other), authority)
	refused("another verifier", mutated(t, func(file *releaseStartupVerifiedFile) { file.Verifier++ }, hotkey), authority)
	path := filepath.Join(dir, releaseStartupVerifiedFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(raw, []byte("0xa1"), []byte("0xa3"), 1)
	if bytes.Equal(changed, raw) {
		t.Fatal("test record lacks its first unit")
	}
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	refused("changed unit", dir, authority)
	if err := os.WriteFile(path, append(bytes.TrimSuffix(raw, []byte("\n")), ' ', '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	refused("noncanonical bytes", dir, authority)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	refused("readable by others", dir, authority)
}

func TestReleaseStartupVerifiedProgressFlagParses(t *testing.T) {
	t.Parallel()
	opts, err := parseValidatorArgsForTest(t, []string{"run", "--config=/etc/sn-validator/validator.yml", "--startup-verified-progress"})
	if err != nil || !optBool(opts, "--startup-verified-progress") {
		t.Fatalf("the flag did not parse: %v", err)
	}
	plain, err := parseValidatorArgsForTest(t, []string{"run", "--config=/etc/sn-validator/validator.yml"})
	if err != nil || optBool(plain, "--startup-verified-progress") {
		t.Fatalf("the flag is set without being given: %v", err)
	}
	if _, err := parseValidatorArgsForTest(t, []string{"run", "--all-operators", "--startup-verified-progress"}); err == nil {
		t.Fatal("the flag parsed outside the release run")
	}
	if releaseStartupVerifiedPersistenceFrom(t.Context()) || !releaseStartupVerifiedPersistenceFrom(withReleaseStartupVerifiedPersistence(t.Context())) {
		t.Fatal("the flag is not carried by exactly its context")
	}
}

// The disk stage asks whether one entry was verified with its whole prefix: a
// chain digest match answers that, whatever the memo's reuse rules say.
func TestReleaseStartupVerifiedIntentEntryNeedsItsWholePrefix(t *testing.T) {
	t.Parallel()
	base := intentV2VerifiedHistoryTestEntries(10, 11, 12, 13, 14)
	owner := newReleaseStartupVerified()
	owner.rememberIntents(intentV2VerifiedHistoryTestDigests(t, base), true)
	middleChanged := intentV2VerifiedHistoryTestEntries(10, 11, 12, 13, 14)
	middleChanged[1].SettlementEpoch = 2
	for _, test := range []struct {
		name     string
		all      []*SteeringIntent
		verified int
	}{
		{"identical", base, 5},
		{"earlier entry changed", middleChanged, 1},
		{"last entry removed", base[:4], 4},
		{"entry inserted before", append([]*SteeringIntent{nativeGapBridgeTestIntent(9, "applied")}, base...), 0},
	} {
		digests := intentV2VerifiedHistoryTestDigests(t, test.all)
		for index := range digests {
			if want := index < test.verified; owner.intentVerified(digests, index) != want {
				t.Fatalf("%s: entry %d verified=%t", test.name, index, !want)
			}
		}
	}
	if (*releaseStartupVerified)(nil).intentVerified(intentV2VerifiedHistoryTestDigests(t, base), 0) {
		t.Fatal("an absent owner verified an entry")
	}
}

// The sealer's own public replay is the next start's replay of that journal at
// the same origin; the other origin is still replayed.
func TestReleaseStartupVerifiedSealedCutSkipsItsStartupReplay(t *testing.T) {
	t.Parallel()
	fixture := newReleaseStartupV2TestFixture(t, true)
	owner := newReleaseStartupVerified()
	ctx := withReleaseStartupVerified(t.Context(), owner)
	fixture.trail(t, 0)
	participant := fixture.disk.participants[0]
	options := fixture.sealOptions(t, 0)
	bounds := fixture.cfg.EvidenceV2.Bounds.Cut
	reader, err := newHttpAttemptStreamV2Reader(fixture.replicas[0].Origin, bounds, attemptStreamV2MetadataBytes(bounds))
	if err != nil {
		t.Fatal(err)
	}
	options.Stats.Replay.origin = reader.endpoint.String()
	point := fixture.nativePoint(t, 1)
	_, _, err = participant.Stats.detachReleaseStatsMeasurementV2(ctx, participant.StateDir, fixture.boundary, options, func(stats ReleaseStatsMeasurement, cut AttemptCutV2) error {
		journal := &releaseMeasurementInputJournal{Schema: releaseMeasurementInputV2Schema, DeploymentID: fixture.cfg.DeploymentID, ChainID: fixture.cfg.ChainID, GenesisHash: strings.ToLower(fixture.cfg.GenesisHash), Coordinator: fixture.cfg.Coordinator, ValidatorID: fixture.cfg.ValidatorID, Netuid: fixture.cfg.Netuid, SubnetEpoch: 1, PolicyHash: strings.ToLower(fixture.cfg.PolicyHash), MeasurementInput: ReleaseMeasurementInput{NoID: participant.NoID, SettlementEpoch: fixture.boundary.SettlementEpoch, CutNativeBlock: uint64(point.header.Number), CutNativeBlockHash: point.hash.Hex(), CutEVMSnapshotBlock: fixture.boundary.EVMBlock, CutEVMSnapshotHash: fixture.boundary.EVMBlockHash, EgressGeneration: cut.Context.EgressGeneration, Stats: stats, AttemptCutV2: &cut}}
		encoded, err := canonicalReleaseMeasurementInputBytes(journal)
		if err != nil {
			return err
		}
		input, err := acquireReleaseMeasurementInputV2Owner(t.Context(), releaseMeasurementInputV2Path(fixture.cfg.StateDir, 1, participant.NoID), fixture.cfg.EvidenceV2.Bounds.MaxInputJournalBytes, releaseMeasurementInputV2ReadHooks{}, true)
		if err != nil {
			return errors.Join(err, input.finish())
		}
		if _, readErr := input.read(); !input.initialMissing || !releaseMeasurementInputV2OnlyMissing(readErr) {
			return errors.Join(errors.New("test ordinary path was not initially absent"), readErr, input.finish())
		}
		return errors.Join(input.write(encoded), input.finish())
	})
	if err != nil {
		t.Fatalf("actual sealed ordinary: %v", err)
	}
	fixture.reopen(t)
	var before [2]int
	for index, store := range fixture.stores {
		_, _, before[index] = store.snapshot()
	}
	if err := fixture.start(ctx, attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	_, _, first := fixture.stores[0].snapshot()
	_, _, second := fixture.stores[1].snapshot()
	if first != before[0] || second <= before[1] {
		t.Fatalf("startup did not reuse exactly the sealed origin's replay: origin 0 %d->%d, origin 1 %d->%d", before[0], first, before[1], second)
	}
}

// A new process that accepts the record starts without any public replay.
func TestReleaseStartupVerifiedRecordResumesAnotherProcess(t *testing.T) {
	t.Parallel()
	hotkey, authority := releaseStartupVerifiedTestAuthority(t)
	fixture := newReleaseStartupV2TestFixture(t, true)
	fixture.trail(t, 0)
	fixture.ordinary(t, 0, 1, false)
	reads := func() int {
		total := 0
		for _, store := range fixture.stores {
			_, _, count := store.snapshot()
			total += count
		}
		return total
	}
	dir := t.TempDir()
	first := newReleaseStartupVerified()
	first.enableRecord(dir, hotkey, authority, &bytes.Buffer{})
	fixture.reopen(t)
	before := reads()
	if err := fixture.start(withReleaseStartupVerified(t.Context(), first), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	if err := first.flush(); err != nil || reads() == before {
		t.Fatalf("first process did not verify and keep its units: %v", err)
	}
	next := newReleaseStartupVerified()
	var stderr bytes.Buffer
	next.enableRecord(dir, hotkey, authority, &stderr)
	if !strings.Contains(stderr.String(), "record accepted") {
		t.Fatalf("the record was not accepted: %q", stderr.String())
	}
	fixture.reopen(t)
	before = reads()
	if err := fixture.start(withReleaseStartupVerified(t.Context(), next), attemptSettlementV2PhysicalIO()); err != nil {
		t.Fatal(err)
	}
	if resumed := reads() - before; resumed != 0 {
		t.Fatalf("the next process replayed %d public objects again", resumed)
	}
}

// The live service admits server-key versions after startup and verifies cuts
// under them, so the record names each one from the next flush on. A successor
// that no longer holds such a version, or holds other bytes under it, then
// verifies everything.
func TestReleaseStartupVerifiedRecordNamesRuntimeServerKeys(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	hotkey, authority := releaseStartupVerifiedTestAuthority(t)
	startupKeys, err := releaseStartupVerifiedServerKeysOf(fixture.startup.keys)
	if err != nil || len(startupKeys) == 0 {
		t.Fatalf("fixture server keys are not listable: %v", err)
	}
	authority.serverKeys = startupKeys
	dir := t.TempDir()
	owner := newReleaseStartupVerified()
	owner.enableRecord(dir, hotkey, authority, &bytes.Buffer{})
	owner.add([32]byte{0xa1})
	written := func() *releaseStartupVerifiedFile {
		t.Helper()
		if err := owner.flush(); err != nil {
			t.Fatal(err)
		}
		file, err := readReleaseStartupVerifiedFile(filepath.Join(dir, releaseStartupVerifiedFileName))
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	if !slices.Equal(written().ServerKeys, startupKeys) {
		t.Fatal("the first record does not name startup's server keys")
	}
	ctx := withReleaseStartupVerified(t.Context(), owner)
	if err := fixture.runtime.refreshServerKeys(ctx); err != nil {
		t.Fatal(err)
	}
	owner.stateLock.Lock()
	rewritten := owner.dirty
	owner.stateLock.Unlock()
	if rewritten {
		t.Fatal("a refresh that admitted nothing rewrote the record")
	}
	noID := fixture.startup.disk.participants[0].NoID
	const version = byte(0xee)
	if fixture.startup.keys[noID][version] != nil {
		t.Fatal("the test version is already published")
	}
	added := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32)).Public().(ed25519.PublicKey)
	fixture.startup.keys[noID][version] = added
	if err := fixture.runtime.refreshServerKeys(ctx); err != nil {
		t.Fatal(err)
	}
	named := releaseStartupVerifiedServerKey{NoID: noID, Version: version, Key: fmt.Sprintf("%x", []byte(added))}
	current := written().ServerKeys
	if len(current) != len(startupKeys)+1 || !slices.Contains(current, named) {
		t.Fatalf("the record does not name the admitted version: %+v", current)
	}
	replaced := slices.Clone(current)
	for index := range replaced {
		if replaced[index] == named {
			replaced[index].Key = strings.Repeat("0", 64)
		}
	}
	accepts := func(serverKeys []releaseStartupVerifiedServerKey) bool {
		t.Helper()
		successor := authority
		successor.serverKeys = serverKeys
		var stderr bytes.Buffer
		newReleaseStartupVerified().enableRecord(dir, hotkey, successor, &stderr)
		return strings.Contains(stderr.String(), "record accepted")
	}
	if !accepts(current) || accepts(startupKeys) || accepts(replaced) {
		t.Fatal("a successor's acceptance does not follow the admitted version")
	}
	// Other bytes under a listed version stop every later write.
	owner.observeServerKeys(map[uint64]map[byte]ed25519.PublicKey{noID: {version: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{10}, 32)).Public().(ed25519.PublicKey)}})
	owner.add([32]byte{0xa2})
	if file := written(); !slices.Equal(file.ServerKeys, current) || len(file.Units) != 1 {
		t.Fatalf("a record was written after a listed version changed: %+v", file)
	}
}

// Only this account's private record is read, and a temporary that a stopped
// process left behind is removed at the next start; nothing else is.
func TestReleaseStartupVerifiedRecordIsOwnedAndStaleTemporariesGo(t *testing.T) {
	t.Parallel()
	hotkey, authority := releaseStartupVerifiedTestAuthority(t)
	dir := t.TempDir()
	releaseStartupVerifiedTestRecord(t, dir, hotkey, authority)
	path := filepath.Join(dir, releaseStartupVerifiedFileName)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !releaseStartupVerifiedPrivateFile(info, os.Geteuid()) || releaseStartupVerifiedPrivateFile(info, os.Geteuid()+1) {
		t.Fatal("the record is not admitted for its owning account alone")
	}
	temporary := filepath.Join(dir, "."+releaseStartupVerifiedFileName+".")
	if err := os.WriteFile(temporary+"123456", []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(temporary+"directory", 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, temporary+"link"); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dir, "."+releaseStartupVerifiedFileName+"x")
	if err := os.WriteFile(unrelated, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	newReleaseStartupVerified().enableRecord(dir, hotkey, authority, &stderr)
	if !strings.Contains(stderr.String(), "record accepted") {
		t.Fatalf("the owned record was refused: %q", stderr.String())
	}
	if _, err := os.Lstat(temporary + "123456"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a stale temporary remains: %v", err)
	}
	for _, kept := range []string{temporary + "directory", temporary + "link", target, unrelated, path} {
		if _, err := os.Lstat(kept); err != nil {
			t.Fatalf("start removed %s: %v", kept, err)
		}
	}
}

// The two-edge native gap bridge the production validator runs with, byte for
// byte, and the pin its deploy settings carry.
const (
	nativeGapBridgeLiveEdges = "{\n" +
		"  \"schema\": \"urnetwork-validator-native-gap-bridge-v2\",\n" +
		"  \"chain_id\": 964,\n" +
		"  \"deployment_id\": \"sn25-mainnet-20261007-01\",\n" +
		"  \"validator_id\": 1,\n" +
		"  \"edges\": [\n" +
		"    {\n" +
		"      \"last_native_epoch\": 25648,\n" +
		"      \"last_artifact_hash\": \"sha256:510390579f2734da814406138580ad4d941885b5821d0ed3ad8899b132dd0a6d\",\n" +
		"      \"first_native_epoch\": 25682\n" +
		"    },\n" +
		"    {\n" +
		"      \"last_native_epoch\": 25714,\n" +
		"      \"last_artifact_hash\": \"sha256:08081f80d43f4b1b493a0b260dfcc4cf5c24fcb9d3b4de1b0d639dc56acca447\",\n" +
		"      \"first_native_epoch\": 25721\n" +
		"    }\n" +
		"  ]\n" +
		"}\n"
	nativeGapBridgeLiveEdgesPin = "sha256:d4d4934c6b0d771f5c67434de0115ff1fae666a84515f8e010865786c644c043"
)

// A record the production build's writer (3a428640) signed under
// releaseStartupVerifiedProductionTestAuthority with production's two bridge
// edges, after its live service admitted server-key version 2 of no_id 1:
// seven units and a five-entry intent prefix.
const releaseStartupVerifiedProductionRecord = `{"schema":"urnetwork-validator-startup-verified-v1","verifier":1,"chain_id":964,"deployment_id":"sn25-mainnet-20261007-01","validator_id":1,` +
	`"hotkey":"0x34c67402debfa14a039a116bf830a7e54faa2fe28162719b1eed7d3f8eb5ae61","config_hash":"0xb61e8566faa1cd236f356cdf095d72dd4f64825bc102492bfdee1cf168ab3451",` +
	`"bridge_edges":[{"last_native_epoch":25648,"last_artifact_hash":"sha256:510390579f2734da814406138580ad4d941885b5821d0ed3ad8899b132dd0a6d","first_native_epoch":25682},` +
	`{"last_native_epoch":25714,"last_artifact_hash":"sha256:08081f80d43f4b1b493a0b260dfcc4cf5c24fcb9d3b4de1b0d639dc56acca447","first_native_epoch":25721}],` +
	`"server_keys":[{"no_id":1,"version":1,"key":"ed4928c628d1c2c6eae90338905995612959273a5c63f93636c14614ac8737d1"},` +
	`{"no_id":1,"version":2,"key":"ca93ac1705187071d67b83c7ff0efe8108e8ec4530575d7726879333dbdabe7c"},` +
	`{"no_id":2,"version":1,"key":"6e7a1cdd29b0b78fd13af4c5598feff4ef2a97166e3ca6f2e4fbfccd80505bf1"}],` +
	`"intents":["0x94108813714299f58e1fa2df1fd1f9aff1f26e000cfd3db8410b8487660d894f","0xb9885ceb70b2a3f79948fb4f431a16d27312e0e735198495b881c1b0568ec11d",` +
	`"0x6c9d84fbea2e3ac8ecbdced1219e02383858dc69e4045d8c62ae8134b62ee476","0xbe13eb120eafa46327f08d4655d1b069bec6f918b84b5cc6096e849cf929e1bb",` +
	`"0x4e12430ec3a077a40a96e2cd3cc935175acfe5275f6740ddefbd37e352a59d40"],` +
	`"units":["0x6778b4130254c50a88ceaa228589669f1945cf0cafc4ad20f13bf9b399695e2d","0x7f722a754dc206dbb28190d65383cb6c5812153b4e3df4bce0441b1d3648b8bf",` +
	`"0x9104f98d750421bbe94af454514caa554ac4605ac2254665cfa6a133c2521278","0xa100000000000000000000000000000000000000000000000000000000000000",` +
	`"0xa200000000000000000000000000000000000000000000000000000000000000","0xb4f4eac0044cd8f775f977afedbcf168e06347148e9e0dd13256e6a5cca39a4c",` +
	`"0xfc48a068d0191551e34aada25beaf29f06b54ab851f45eebfa9ce03a7f43031f"],` +
	`"signature":"0x0a66209a6012047e76ef1f09db3a06807dfe6c89e532ef45db603b2e52dc15448826598b394d8c160246f751b43fb0752ffb4dc76f5b97716db792f327eb628e"}` + "\n"

// Production's chain, deployment, validator and the given bridge, with a test
// hotkey, a stand-in configuration hash and test server keys; runtimeKey adds
// the version the record's live service admitted.
func releaseStartupVerifiedProductionTestAuthority(t *testing.T, bridge []ReleaseNativeGapBridgeEdge, runtimeKey bool) (*crv4.Keypair, releaseStartupVerifiedAuthority) {
	t.Helper()
	hotkey, err := crv4.KeypairFromSeed([32]byte{7})
	if err != nil {
		t.Fatal(err)
	}
	key := func(seed byte) ed25519.PublicKey {
		return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32)).Public().(ed25519.PublicKey)
	}
	keys := map[uint64]map[byte]ed25519.PublicKey{1: {1: key(3)}, 2: {1: key(5)}}
	if runtimeKey {
		keys[1][2] = key(4)
	}
	serverKeys, err := releaseStartupVerifiedServerKeysOf(keys)
	if err != nil {
		t.Fatal(err)
	}
	configHash := sha256.Sum256([]byte("sn25 production config stand-in"))
	return hotkey, releaseStartupVerifiedAuthority{chainID: 964, deploymentID: "sn25-mainnet-20261007-01", validatorID: 1, hotkey: hotkey.PublicKey(),
		configHash: configHash, lineage: map[[32]byte]bool{configHash: true}, bridge: bridge, serverKeys: serverKeys}
}

// The first start of this build resumes the record the production build wrote
// under the same chain, deployment, validator, hotkey, configuration, bridge
// and server keys, with every unit and intent it names. A later bridge may
// only append edges after the record's.
func TestReleaseStartupVerifiedRecordAcceptsTheProductionRecord(t *testing.T) {
	t.Parallel()
	bridge, err := DecodeReleaseNativeGapBridge([]byte(nativeGapBridgeLiveEdges), nativeGapBridgeLiveEdgesPin)
	if err != nil {
		t.Fatal(err)
	}
	edges := bridge.Edges()
	live, err := DecodeReleaseNativeGapBridge([]byte(nativeGapBridgeLiveV1), nativeGapBridgeLiveV1Pin)
	second := ReleaseNativeGapBridgeEdge{LastNativeEpoch: 25714, LastArtifactHash: "sha256:08081f80d43f4b1b493a0b260dfcc4cf5c24fcb9d3b4de1b0d639dc56acca447", FirstNativeEpoch: 25721}
	if err != nil || len(edges) != 2 || !slices.Equal(live.Edges(), edges[:1]) || edges[1] != second {
		t.Fatalf("production bridge edges differ: %v %v", err, edges)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, releaseStartupVerifiedFileName)
	if err := os.WriteFile(path, []byte(releaseStartupVerifiedProductionRecord), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := readReleaseStartupVerifiedFile(path)
	if err != nil {
		t.Fatalf("this build cannot read the production record: %v", err)
	}
	if file.Verifier != releaseStartupVerifiedVerifier || !slices.Equal(file.BridgeEdges, edges) {
		t.Fatalf("the production record names verifier %d and edges %v", file.Verifier, file.BridgeEdges)
	}
	intents, err := decodeReleaseStartupVerifiedDigests(file.Intents)
	if err != nil {
		t.Fatal(err)
	}
	start := func(bridge []ReleaseNativeGapBridgeEdge, runtimeKey bool) (*releaseStartupVerified, string) {
		t.Helper()
		hotkey, authority := releaseStartupVerifiedProductionTestAuthority(t, bridge, runtimeKey)
		owner := newReleaseStartupVerified()
		var stderr bytes.Buffer
		owner.enableRecord(dir, hotkey, authority, &stderr)
		return owner, stderr.String()
	}
	appended := append(slices.Clone(edges), ReleaseNativeGapBridgeEdge{LastNativeEpoch: 25800, LastArtifactHash: "sha256:" + strings.Repeat("b", 64), FirstNativeEpoch: 25803})
	for _, current := range [][]ReleaseNativeGapBridgeEdge{edges, appended} {
		owner, notice := start(current, true)
		if !strings.Contains(notice, "record accepted: 7 units and 5 intents") || !owner.has([32]byte{0xa1}) || !owner.has([32]byte{0xa2}) || owner.intentPrefix(intents) != len(intents) {
			t.Fatalf("the production record was not resumed under %d bridge edges: %q", len(current), notice)
		}
	}
	// A bridge without the record's second edge, or a census without the
	// version its live service admitted, verifies everything.
	for _, refused := range []struct {
		bridge     []ReleaseNativeGapBridgeEdge
		runtimeKey bool
	}{{edges[:1], true}, {edges, false}} {
		if owner, notice := start(refused.bridge, refused.runtimeKey); !strings.Contains(notice, "record ignored") || owner.has([32]byte{0xa1}) {
			t.Fatalf("the production record was accepted under a narrower authority: %q", notice)
		}
	}
}
