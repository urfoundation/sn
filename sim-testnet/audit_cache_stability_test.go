// Regression coverage for completed verification surviving unrelated rebuilds.
// Legacy fixtures are authenticated using their original schema and key domain.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Force the old incidental executable identity to change without building or
// sleeping. This sequential test restores it before parallel tests can start.
func replaceAuditCacheExecutableForTest(t *testing.T) {
	t.Helper()
	if _, err := historicalAuditExecutableSHA256(); err != nil {
		t.Fatal(err)
	}
	digest, err := historicalAuditExecutableIdentity.digest, historicalAuditExecutableIdentity.err
	historicalAuditExecutableIdentity.digest = strings.Repeat("de", 32)
	historicalAuditExecutableIdentity.err = errors.New("incidental executable identity unavailable")
	t.Cleanup(func() {
		historicalAuditExecutableIdentity.digest, historicalAuditExecutableIdentity.err = digest, err
	})
}

// Write an old proof using the exact original wire format, including its binary
// digest. The reader under test must validate this proof before promotion.
func writeLegacyHistoricalAuditCacheForTest(t *testing.T, executor *Executor, input any, change func(*historicalAuditCacheEnvelope)) string {
	t.Helper()
	entry, hit := executor.lookupHistoricalAuditCache(t.Context(), "native-receipt", input)
	if entry == nil || hit {
		t.Fatal("legacy fixture needs a cold prepared entry")
	}
	legacy := *entry
	legacy.key = derive32(executor.cfg, "historical-audit-cache/v1")
	legacy.proof.Schema = historicalAuditCacheLegacySchema
	legacy.proof.ExecutableSHA256 = strings.Repeat("ab", 32)
	envelope := historicalAuditCacheEnvelope{Proof: legacy.proof, MAC: hex.EncodeToString(legacy.authenticationTag(legacy.proof))}
	if change != nil {
		originalMac := envelope.MAC
		change(&envelope)
		if envelope.MAC == originalMac {
			envelope.MAC = hex.EncodeToString(legacy.authenticationTag(envelope.Proof))
		}
	}
	nameHash, err := canonicalHashHex(envelope.Proof)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(executor.stateDir, historicalAuditCacheLegacyDirectoryName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, strings.TrimPrefix(nameHash, "0x")+".json")
	if err := os.WriteFile(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHistoricalAuditCacheReusesSuccessAcrossExecutableChanges(t *testing.T) {
	executor := historicalAuditCacheTestExecutor(t)
	input := historicalAuditCacheTestInput()
	historicalAuditCacheTestSeed(t, executor, input)
	replaceAuditCacheExecutableForTest(t)
	reopened := &Executor{cfg: executor.cfg, plan: executor.plan, stateDir: executor.stateDir}
	hit, err := reopened.withHistoricalAuditCache(t.Context(), "native-receipt", input, func(context.Context) error {
		t.Error("rebuild repeated already completed immutable verification")
		return errors.New("historical source offline")
	})
	if err != nil || !hit {
		t.Fatalf("rebuild lost successful audit: hit=%t error=%v", hit, err)
	}
}

func TestHistoricalAuditCacheImportsCompatibleLegacyExecutable(t *testing.T) {
	executor := historicalAuditCacheTestExecutor(t)
	input := historicalAuditCacheTestInput()
	legacyPath := writeLegacyHistoricalAuditCacheForTest(t, executor, input, nil)
	entry, hit := executor.lookupHistoricalAuditCache(t.Context(), "native-receipt", input)
	if entry == nil || !hit || entry.proof.ExecutableSHA256 != "" || !entry.readSuccess() {
		t.Fatal("authenticated compatible legacy proof was not promoted")
	}
	if err := os.Remove(legacyPath); err != nil {
		t.Fatal(err)
	}
	replaceAuditCacheExecutableForTest(t)
	reopened := &Executor{cfg: executor.cfg, plan: executor.plan, stateDir: executor.stateDir}
	if _, hit := reopened.lookupHistoricalAuditCache(t.Context(), "native-receipt", input); !hit {
		t.Fatal("promoted proof still depended on the prior executable or legacy file")
	}
}

func TestHistoricalAuditCacheLegacyImportRejectsChangedIdentityAndMac(t *testing.T) {
	tests := []struct {
		name        string
		changeProof func(*historicalAuditCacheEnvelope)
		changeInput func(*Executor, map[string]any)
	}{
		{name: "mac", changeProof: func(envelope *historicalAuditCacheEnvelope) { envelope.MAC = strings.Repeat("00", 32) }},
		{name: "verifier", changeProof: func(envelope *historicalAuditCacheEnvelope) {
			envelope.Proof.VerifierVersion = "unknown-history-verifier"
		}},
		{name: "success", changeProof: func(envelope *historicalAuditCacheEnvelope) { envelope.Proof.Success = false }},
		{name: "context", changeInput: func(executor *Executor, _ map[string]any) { executor.plan.ResolvedInputsHash += "-changed" }},
		{name: "input", changeInput: func(_ *Executor, input map[string]any) { input["transaction"] = "different transaction" }},
		{name: "key", changeInput: func(executor *Executor, _ map[string]any) { executor.cfg.WalletMaterial += " changed" }},
	}
	for _, test := range tests {
		executor := historicalAuditCacheTestExecutor(t)
		input := historicalAuditCacheTestInput()
		writeLegacyHistoricalAuditCacheForTest(t, executor, input, test.changeProof)
		if test.changeInput != nil {
			test.changeInput(executor, input)
		}
		calls := 0
		hit, err := executor.withHistoricalAuditCache(t.Context(), "native-receipt", input, func(context.Context) error {
			calls++
			return nil
		})
		if hit || err != nil || calls != 1 {
			t.Errorf("%s: changed legacy proof bypassed verification: hit=%t calls=%d error=%v", test.name, hit, calls, err)
		}
	}
}

// Replaying a legitimately signed older contract is forbidden even when its
// context and input otherwise match and the optimization file is untouched.
func TestHistoricalAuditCacheLegacyImportHonorsRequestedVerifierVersion(t *testing.T) {
	executor := historicalAuditCacheTestExecutor(t)
	input := historicalAuditCacheTestInput()
	writeLegacyHistoricalAuditCacheForTest(t, executor, input, nil)
	contextHash, err := executor.historicalAuditContextHash(executor.cfg)
	if err != nil {
		t.Fatal(err)
	}
	entry := &historicalAuditCacheEntry{stateDir: executor.stateDir,
		proof: historicalAuditCacheProof{Schema: historicalAuditCacheSchema, VerifierVersion: "immutable-history-v2", ContextHash: contextHash, Success: true}}
	if entry.readCompatibleSuccess(t.Context(), derive32(executor.cfg, "historical-audit-cache/v1")) {
		t.Fatal("old verifier semantics crossed a version change")
	}
}
