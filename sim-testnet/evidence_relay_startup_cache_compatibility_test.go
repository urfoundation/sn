//go:build linux || darwin

// Regression tests for retaining verified relay prefixes across runner builds.
package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Materialize the old executable-bound envelope with its original context and
// authentication domain; v2 must prove compatibility before consuming it.
func writeLegacyRelayStartupCacheForTest(t *testing.T, fixture *evidenceRelayStartupCacheTestFixture, change func(*evidenceRelayStartupCacheEnvelope)) string {
	t.Helper()
	proof := fixture.proof
	proof.Schema, proof.VerifierVersion = evidenceRelayStartupCacheLegacySchema, evidenceRelayStartupCacheLegacyVerifierVersion
	proof.ExecutableSha256 = strings.Repeat("ac", 32)
	contextHash, err := fixture.runtime.evidenceRelayStartupContextHashFor(proof.Schema, proof.VerifierVersion, proof.ExecutableSha256)
	if err != nil {
		t.Fatal(err)
	}
	proof.ContextHash = contextHash
	legacy := evidenceRelayStartupCacheEntry{key: derive32(fixture.runtime.executor.cfg, "relay-startup-cache/v1")}
	envelope := evidenceRelayStartupCacheEnvelope{Proof: proof, Mac: hex.EncodeToString(legacy.authenticationTag(proof))}
	if change != nil {
		originalMac := envelope.Mac
		change(&envelope)
		if envelope.Mac == originalMac {
			envelope.Mac = hex.EncodeToString(legacy.authenticationTag(envelope.Proof))
		}
	}
	directory := filepath.Join(fixture.entry.stateDir, evidenceRelayStartupCacheLegacyDirectoryName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, strings.TrimPrefix(envelope.Proof.ContextHash, "0x")+".json")
	if err := os.WriteFile(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEvidenceRelayStartupCacheReusesPrefixAcrossExecutableChanges(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	if !fixture.entry.save(t.Context(), fixture.proof) {
		t.Fatal("could not persist completed prefix")
	}
	replaceAuditCacheExecutableForTest(t)
	session, err := fixture.runtime.newEvidenceRelayStartupSession(t.Context(), fixture.entries, fixture.inventories)
	if err != nil || session == nil || !session.hit || fixture.runtime.sources[0].nextEpoch != 9 {
		t.Fatalf("binary rebuild lost completed prefix: hit=%v error=%v", session != nil && session.hit, err)
	}
}

func TestEvidenceRelayStartupCacheImportsCompatibleLegacyExecutable(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	legacyPath := writeLegacyRelayStartupCacheForTest(t, fixture, nil)
	session, err := fixture.runtime.newEvidenceRelayStartupSession(t.Context(), fixture.entries, fixture.inventories)
	if err != nil || session == nil || !session.hit || fixture.runtime.sources[0].nextEpoch != 9 || session.entry.fixed.ExecutableSha256 != "" {
		t.Fatalf("compatible legacy prefix was not restored: hit=%v error=%v", session != nil && session.hit, err)
	}
	if _, hit := session.entry.read(t.Context()); !hit {
		t.Fatal("compatible legacy prefix was not promoted to durable v2 identity")
	}
	if err := os.Remove(legacyPath); err != nil {
		t.Fatal(err)
	}
	fixture.runtime.sources[0].nextEpoch = 7
	replaceAuditCacheExecutableForTest(t)
	session, err = fixture.runtime.newEvidenceRelayStartupSession(t.Context(), fixture.entries, fixture.inventories)
	if err != nil || session == nil || !session.hit || fixture.runtime.sources[0].nextEpoch != 9 {
		t.Fatalf("promoted cache still depended on old binary/file: hit=%v error=%v", session != nil && session.hit, err)
	}
}

func TestEvidenceRelayStartupCacheLegacyImportRejectsChangedInputsAndMac(t *testing.T) {
	tests := []struct {
		name        string
		changeProof func(*evidenceRelayStartupCacheEnvelope)
		changeInput func(*evidenceRelayStartupCacheTestFixture) error
	}{
		{name: "mac", changeProof: func(envelope *evidenceRelayStartupCacheEnvelope) { envelope.Mac = strings.Repeat("00", 32) }},
		{name: "verifier", changeProof: func(envelope *evidenceRelayStartupCacheEnvelope) {
			envelope.Proof.VerifierVersion = "unknown-prefix-verifier"
		}},
		{name: "plan", changeInput: func(fixture *evidenceRelayStartupCacheTestFixture) error {
			fixture.runtime.executor.plan.ResolvedInputsHash += "-changed"
			return nil
		}},
		{name: "config", changeInput: func(fixture *evidenceRelayStartupCacheTestFixture) error {
			fixture.runtime.executor.cfg.ConfigHash += "-changed"
			fixture.runtime.executor.cfg.provisionalResume.Record.ConfigHash = fixture.runtime.executor.cfg.ConfigHash
			fixture.runtime.executor.plan.ConfigHash = fixture.runtime.executor.cfg.ConfigHash
			return nil
		}},
		{name: "source-identity", changeInput: func(fixture *evidenceRelayStartupCacheTestFixture) error {
			fixture.runtime.sources[0].bounds.MaxClosureBytes++
			return nil
		}},
		{name: "receipt", changeInput: func(fixture *evidenceRelayStartupCacheTestFixture) error {
			return os.WriteFile(fixture.receiptPath, []byte("changed\n"), 0o600)
		}},
		{name: "manifest", changeInput: func(fixture *evidenceRelayStartupCacheTestFixture) error {
			return os.WriteFile(fixture.closedPaths[0], []byte{99}, 0o600)
		}},
		{name: "journal", changeInput: func(fixture *evidenceRelayStartupCacheTestFixture) error {
			fixture.entries[0].ActionID = "changed-action"
			return nil
		}},
		{name: "strict", changeInput: func(fixture *evidenceRelayStartupCacheTestFixture) error {
			fixture.runtime.executor.cfg.provisionalResume = nil
			return nil
		}},
	}
	for _, test := range tests {
		fixture := newEvidenceRelayStartupCacheTestFixture(t)
		writeLegacyRelayStartupCacheForTest(t, fixture, test.changeProof)
		if test.changeInput != nil {
			if err := test.changeInput(fixture); err != nil {
				t.Fatalf("%s: %v", test.name, err)
			}
		}
		session, err := fixture.runtime.newEvidenceRelayStartupSession(t.Context(), fixture.entries, fixture.inventories)
		if err != nil || session != nil && session.hit || fixture.runtime.sources[0].nextEpoch != 7 {
			t.Errorf("%s: changed inputs restored legacy prefix: hit=%v error=%v", test.name, session != nil && session.hit, err)
		}
		if _, hit := fixture.entry.read(t.Context()); hit {
			t.Errorf("%s: invalid legacy checkpoint was promoted", test.name)
		}
	}
}

func TestEvidenceRelayStartupCacheLegacyImportHonorsRequestedVerifierVersion(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	writeLegacyRelayStartupCacheForTest(t, fixture, nil)
	entry := *fixture.entry
	entry.fixed.VerifierVersion = "exact-plan-prefix-v2"
	if _, hit := fixture.runtime.readCompatibleEvidenceRelayStartupProof(t.Context(), &entry,
		derive32(fixture.runtime.executor.cfg, "relay-startup-cache/v1"), fixture.entries, fixture.inventories); hit {
		t.Fatal("old verifier contract crossed the requested version change")
	}
}
