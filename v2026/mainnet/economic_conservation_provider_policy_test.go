// Original policy serialization and the protected validator reader keep their
// distinct digest grammars joined by one explicit, nonmutating adapter.
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
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
)

// This is a real private descriptor, with no supplied verified measurement.
// Opening it exercises actual reference validation, protected I/O and decoding.
func economicProviderAttemptPolicyFixture(t *testing.T) (*economicProviderMeasurementPolicy, []byte) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	authority := validator.ProviderAttemptAuthority{
		Schema: validator.ProviderAttemptAuthoritySchema,
		Registry: protocol.ProviderAttemptRegistryExpectation{
			Domain:   protocol.ProviderAttemptDomain{ChainId: 945, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: [20]byte{2}, SettlementVault: [20]byte{3}, DeploymentIdHash: [32]byte{4}, PolicyHash: [32]byte{5}},
			RootHash: [32]byte{6}, Signer: [32]byte{7}, MaxRevisions: 4, MaxOwners: 8, MaxOperatorLanes: 16, MaxBytes: 64 * 1024,
		},
		WindowSigner: [32]byte{8}, WindowEndpoint: "https://synthetic-attempt.example/windows", ScratchRoot: directory,
		MaxWindowBytes: 1024, MaxOriginalBytes: 64 * 1024, MaxObjects: 16, MaxProviders: 16, MaxMetadataBytes: 1024, MaxRecordBytes: 1024, MaxRecords: 16,
	}
	raw, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "synthetic-attempt-authority.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return &economicProviderMeasurementPolicy{Schema: economicProviderMeasurementPolicySchema, WholeWorkAuthoritySigner: "0x0000000000000000000000000000000000007890", WalletEndpoint: "https://synthetic-wallet.example", AttemptAuthority: validator.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(raw)), SHA256: monitorReadDigest(raw)}, MaximumOriginalBytes: 64 * 1024}, raw
}

// One serialized policy reaches the actual authority constructor unchanged;
// live and retained consumers use the same digest and physical reference.
func TestEconomicProviderAttemptAuthorityOpensOriginalPolicyReference(t *testing.T) {
	policy, raw := economicProviderAttemptPolicyFixture(t)
	before, err := json.Marshal(policy)
	if err != nil || policy.validate(economicConservationPolicy{}) != nil {
		t.Fatal("original policy admission failed", err)
	}
	reference, digest, err := policy.attemptReference()
	if err != nil || digest != sha256.Sum256(raw) || reference.Path != policy.AttemptAuthority.Path || reference.Bytes != policy.AttemptAuthority.Bytes || reference.SHA256 != "0x"+hex.EncodeToString(digest[:]) {
		t.Fatal("original authority reference changed at adapter", reference, digest, err)
	}
	source, err := policy.openAttemptSource(t.Context())
	if err != nil || source == nil {
		t.Fatal("canonical economic reference could not open actual authority", err)
	}
	after, err := json.Marshal(policy)
	if err != nil || !bytes.Equal(before, after) || !bytes.Contains(after, []byte(`"sha256":"sha256:`)) {
		t.Fatal("adapter rewrote original policy serialization", err)
	}
}

// The adapter accepts one economic grammar; it does not broaden either file
// reader to aliases or permit zero, oversized or path-relabelled references.
func TestEconomicProviderAttemptAuthorityRefusesReferenceAliases(t *testing.T) {
	policy, _ := economicProviderAttemptPolicyFixture(t)
	original := policy.AttemptAuthority
	for _, invalid := range []validator.ReleaseEvidenceV2File{
		{Path: original.Path, Bytes: original.Bytes, SHA256: strings.TrimPrefix(original.SHA256, "sha256:")},
		{Path: original.Path, Bytes: original.Bytes, SHA256: "0x" + strings.TrimPrefix(original.SHA256, "sha256:")},
		{Path: original.Path, Bytes: original.Bytes, SHA256: strings.ToUpper(original.SHA256)},
		{Path: original.Path, Bytes: original.Bytes, SHA256: "sha256:" + strings.Repeat("0", 64)},
		{Path: original.Path, Bytes: 16*1024*1024 + 1, SHA256: original.SHA256},
		{Path: "relative-authority.json", Bytes: original.Bytes, SHA256: original.SHA256},
	} {
		policy.AttemptAuthority = invalid
		if _, _, err := policy.attemptReference(); err == nil {
			t.Fatal("noncanonical authority reference accepted", invalid)
		}
		if err := policy.validate(economicConservationPolicy{}); err == nil {
			t.Fatal("noncanonical authority policy admitted", invalid)
		}
	}
}

// A correct digest conversion cannot conceal changed original bytes or extend
// a canceled read owner; both reach the production protected file reader.
func TestEconomicProviderAttemptAuthorityKeepsByteAndCancellationGuards(t *testing.T) {
	policy, raw := economicProviderAttemptPolicyFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if source, err := policy.openAttemptSource(ctx); source != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled authority read acquired a source", source, err)
	}
	changed := bytes.Clone(raw)
	changed[len(changed)-1] = ' '
	if err := os.WriteFile(policy.AttemptAuthority.Path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if source, err := policy.openAttemptSource(t.Context()); source != nil || err == nil {
		t.Fatal("changed original authority bytes accepted", source, err)
	}
	if err := os.WriteFile(policy.AttemptAuthority.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if source, err := policy.openAttemptSource(t.Context()); source == nil || err != nil {
		t.Fatal("identical original authority could not be retried", source, err)
	}
}
