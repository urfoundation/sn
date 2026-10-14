//go:build linux || darwin

package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/crv4"
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
