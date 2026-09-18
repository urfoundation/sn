package validator

// Client seed custody and runtime identity continuity are distinct boundaries:
// two individually valid private files must not bind one live operator to a
// different key from its already prepared ledger. All keys here are test-owned.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Each fixture owns a fully validated local release config and the actual
// production stats/ledger/proof preparation, without external service access.
type releaseClientSeedContinuityFixture struct {
	cfg       *ReleaseConfig
	op        OperatorConfig
	state     *releaseAttemptState
	seed      [32]byte
	publicKey [32]byte
}

// Existing full policy/config fixtures remain unchanged; no policy threshold
// or runtime identity is weakened to reach the key boundary.
func newReleaseClientSeedContinuityFixture(t *testing.T) releaseClientSeedContinuityFixture {
	t.Helper()
	cfg, err := LoadReleaseConfig(writeReleaseConfig(t, validReleaseConfig(t)))
	if err != nil {
		t.Fatal(err)
	}
	op := cfg.Operators[0]
	if err := os.MkdirAll(op.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var seed [32]byte
	for index := range seed {
		seed[index] = byte(index + 17)
	}
	if err := os.WriteFile(op.ClientKeySeedFile, seed[:], 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := loadReleaseAttemptState(cfg, op, 7)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := state.ledger.Close(); err != nil {
			t.Errorf("close prepared fixture ledger: %v", err)
		}
	})
	var publicKey [32]byte
	copy(publicKey[:], ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey))
	if !bytes.Equal(state.ledger.vpk, publicKey[:]) || state.ledger.identity.ValidatorVPK != attemptHex32(publicKey) {
		t.Fatal("actual first seed load did not bind the prepared ledger identity")
	}
	if err := validateAttemptLedgerIdentity(state.ledger.identity, publicKey[:]); err != nil {
		t.Fatal(err)
	}
	return releaseClientSeedContinuityFixture{cfg: cfg, op: op, state: state, seed: seed, publicKey: publicKey}
}

// An exact sentinel proves callback admission without continuing into API/JWT
// setup. Both key-file contents and the prepared ledger remain unchanged.
func observeReleaseClientSeedRuntimeAdmission(t *testing.T, fixture releaseClientSeedContinuityFixture) (observed [32]byte, entries int, resultErr error) {
	t.Helper()
	before, err := os.ReadFile(fixture.op.ClientKeySeedFile)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(fixture.op.ClientKeySeedFile)
	if err != nil {
		t.Fatal(err)
	}
	head, err := fixture.state.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("test-owned release client-key runtime admission stop")
	runtime, resultErr := startReleaseOperatorWithAdmission(context.Background(), fixture.cfg, fixture.op, func() uint64 { return 42 },
		func(context.Context, *AttemptBoundary, []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, errors.New("unexpected boundary resolution before runtime admission")
		}, fixture.state, func(publicKey [32]byte) error {
			observed, entries = publicKey, entries+1
			return stop
		})
	if runtime != nil {
		if runtime.close != nil {
			runtime.close()
		}
		t.Fatal("neutral runtime observer did not stop before resource publication")
	}
	if entries > 1 || entries == 1 && resultErr != stop {
		t.Fatalf("runtime admission lost exact call-local sentinel: entries=%d error=%v", entries, resultErr)
	}
	after, readErr := os.ReadFile(fixture.op.ClientKeySeedFile)
	afterInfo, statErr := os.Lstat(fixture.op.ClientKeySeedFile)
	afterHead, headErr := fixture.state.ledger.Head()
	if readErr != nil || statErr != nil || headErr != nil || !bytes.Equal(before, after) || !os.SameFile(info, afterInfo) || info.Mode() != afterInfo.Mode() || head != afterHead || !bytes.Equal(fixture.state.ledger.vpk, fixture.publicKey[:]) || fixture.state.ledger.identity.ValidatorVPK != attemptHex32(fixture.publicKey) {
		t.Fatal("runtime admission mutated key bytes, namespace or the prepared ledger")
	}
	return observed, entries, resultErr
}

// Replacing the filename between complete loads cannot change the authority
// while retaining stats and an attempt ledger prepared under the original key.
func TestReleaseClientSeedContinuityRejectsReplacementBeforeRuntime(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	before, err := os.Lstat(fixture.op.ClientKeySeedFile)
	if err != nil {
		t.Fatal(err)
	}
	replacementSeed := fixture.seed
	replacementSeed[0] ^= 0x7f
	var replacementPublic [32]byte
	copy(replacementPublic[:], ed25519.NewKeyFromSeed(replacementSeed[:]).Public().(ed25519.PublicKey))
	if replacementPublic == fixture.publicKey {
		t.Fatal("replacement fixture did not establish a different public identity")
	}
	replacementPath := filepath.Join(fixture.op.StateDir, "replacement.key")
	if err := os.WriteFile(replacementPath, replacementSeed[:], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacementPath, fixture.op.ClientKeySeedFile); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(fixture.op.ClientKeySeedFile)
	if err != nil || os.SameFile(before, after) || after.Mode() != 0o600 || after.Size() != ed25519.SeedSize {
		t.Fatalf("fixture did not establish a valid private replacement: %v", err)
	}
	observed, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
	if entries != 0 {
		if observed != replacementPublic {
			t.Fatal("replacement fixture reached runtime under an unexpected identity")
		}
		t.Fatal("release client key replacement crossed runtime admission with a different prepared ledger VPK")
	}
	if err == nil || !strings.Contains(err.Error(), "client key differs from prepared attempt ledger") {
		t.Fatalf("replacement did not receive the identity-continuity refusal: %v", err)
	}
}

// Inode equality is not key identity. A complete same-size rewrite between
// loads is separately valid custody and still cannot change the runtime key.
func TestReleaseClientSeedContinuityRejectsRewriteBeforeRuntime(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	before, err := os.Lstat(fixture.op.ClientKeySeedFile)
	if err != nil {
		t.Fatal(err)
	}
	replacementSeed := fixture.seed
	replacementSeed[0] ^= 0x3f
	var replacementPublic [32]byte
	copy(replacementPublic[:], ed25519.NewKeyFromSeed(replacementSeed[:]).Public().(ed25519.PublicKey))
	if replacementPublic == fixture.publicKey {
		t.Fatal("rewrite fixture did not establish a different public identity")
	}
	if err := os.WriteFile(fixture.op.ClientKeySeedFile, replacementSeed[:], 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(fixture.op.ClientKeySeedFile)
	if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() || after.Size() != before.Size() {
		t.Fatalf("fixture did not retain the same private inode and size: %v", err)
	}
	observed, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
	if entries != 0 {
		if observed != replacementPublic {
			t.Fatal("rewrite fixture reached runtime under an unexpected identity")
		}
		t.Fatal("release client key rewrite crossed runtime admission with a different prepared ledger VPK")
	}
	if err == nil || !strings.Contains(err.Error(), "client key differs from prepared attempt ledger") {
		t.Fatalf("rewrite did not receive the identity-continuity refusal: %v", err)
	}
}

// An unchanged valid seed admits exactly once using the actual prepared
// ledger's public identity; no service error is accepted as a positive result.
func TestReleaseClientSeedContinuityAcceptsExactPreparedKey(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	observed, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
	if entries != 1 || observed != fixture.publicKey {
		t.Fatalf("matching prepared client key did not reach exact admission: entries=%d error=%v", entries, err)
	}
}

// Raw and bare hexadecimal plus the existing four ASCII whitespace bytes can
// represent one identity. Continuity does not invent a wire-format pin.
func TestReleaseClientSeedContinuityAcceptsEquivalentExistingEncoding(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	encoded := []byte(" \t\r\n" + hex.EncodeToString(fixture.seed[:]) + "\n\r\t ")
	if err := os.WriteFile(fixture.op.ClientKeySeedFile, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	observed, entries, err := observeReleaseClientSeedRuntimeAdmission(t, fixture)
	if entries != 1 || observed != fixture.publicKey {
		t.Fatalf("equivalent existing client key encoding did not reach exact admission: entries=%d error=%v", entries, err)
	}
}

// Prepared-state prerequisites already exist later in startup. They must refuse
// before API/JWT/transport ownership, independently of a valid provisioned key.
func TestReleaseClientSeedContinuityRejectsIncompleteStateBeforeRuntime(t *testing.T) {
	fixture := newReleaseClientSeedContinuityFixture(t)
	beforeHead, err := fixture.state.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	var admitted []string
	for _, missing := range []string{"nil-state", "nil-stats", "nil-ledger", "nil-store", "nil-epoch", "nil-resolver"} {
		state := *fixture.state
		prepared := &state
		epochFn := func() uint64 { return 42 }
		var resolver AttemptBoundaryResolver = func(context.Context, *AttemptBoundary, []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
			return AttemptBoundary{}, nil, errors.New("unexpected boundary resolution before prepared-state admission")
		}
		switch missing {
		case "nil-state":
			prepared = nil
		case "nil-stats":
			state.stats = nil
		case "nil-ledger":
			state.ledger = nil
		case "nil-store":
			state.store = nil
		case "nil-epoch":
			epochFn = nil
		case "nil-resolver":
			resolver = nil
		}
		entries := 0
		stop := errors.New("test-owned incomplete-state admission stop")
		runtime, startErr := startReleaseOperatorWithAdmission(context.Background(), fixture.cfg, fixture.op, epochFn, resolver, prepared, func(publicKey [32]byte) error {
			if publicKey != fixture.publicKey {
				return errors.New("incomplete-state fixture changed the genuine client key")
			}
			entries++
			return stop
		})
		if runtime != nil {
			if runtime.close != nil {
				runtime.close()
			}
			t.Fatal("incomplete-state observer published runtime ownership")
		}
		if entries == 1 && startErr == stop {
			admitted = append(admitted, missing)
		} else if entries != 0 || startErr == nil || !strings.Contains(startErr.Error(), "prepared attempt state is incomplete") {
			t.Fatalf("%s did not retain its exact prepared-state refusal: %v", missing, startErr)
		}
	}
	afterHead, err := fixture.state.ledger.Head()
	if err != nil || afterHead != beforeHead || !bytes.Equal(fixture.state.ledger.vpk, fixture.publicKey[:]) {
		t.Fatalf("incomplete-state admission changed prepared ledger authority: %v", err)
	}
	if len(admitted) != 0 {
		t.Fatalf("release client-key runtime admitted incomplete prepared state: %v", admitted)
	}
}
