//go:build linux || darwin

package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Original approval and both consent signatures precede the upgrade. Only the
// reviewed protocol metadata is retained; native blocks and keys are synthetic.
func newRuntimeEvidenceHistoricalRuntimeTest(t *testing.T) *runtimeEvidenceActivationRpcV2TestFixture {
	t.Helper()
	fixture := newRuntimeEvidenceActivationConfiguredRpcV2TestFixture(t, func(cfg *ResolvedConfig) {
		cfg.Public.Chain.ExpectedRuntimeSpec, cfg.Public.Chain.ConfigIdentityRuntimeSpec = 455, 0
		cfg.Release = validatorEvidenceRuntime455TestLock(t)
	})
	encoded, err := os.ReadFile("../miner/testdata/runtime455-metadata.scale.gz.base64")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err := errors.Join(err, reader.Close()); err != nil {
		t.Fatal(err)
	}
	fixture.metadataHex = hexutil.Encode(raw)
	_, metadataHash, err := crv4.DecodeRuntimeMetadata(fixture.metadataHex)
	if err != nil || metadataHash != "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc" {
		t.Fatalf("historical protocol fixture: %v", err)
	}
	fixture.nativeRuntime = crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a", MetadataHash: metadataHash}
	fixture.base.cfg.Release = validatorEvidenceRuntime455TestLock(t)
	fixture.base.cfg.Public.Chain.ExpectedRuntimeSpec = 455
	if hash, err := releaseConfigHash(fixture.base.cfg.Config, fixture.base.cfg.Public, fixture.base.cfg.Hyperparameters); err != nil || hash != fixture.base.plan.ConfigHash {
		t.Fatalf("original config approval differs: %v", err)
	}
	prepared, wire, err := fixture.executor.prepareRuntimeEvidenceActivationsV2(t.Context(), fixture.chain)
	if err != nil {
		t.Fatalf("prepare original455 signed activation: %v", err)
	}
	fixture.base.stateDir, fixture.base.prepared, fixture.base.preparedBytes = fixture.executor.stateDir, prepared, wire
	fixture.base.completed.PreparedHash = fmt.Sprintf("0x%x", sha256.Sum256(wire))
	return fixture
}

func upgradeRuntimeEvidenceHistoricalTest(t *testing.T, fixture *runtimeEvidenceActivationRpcV2TestFixture) {
	t.Helper()
	cfg := fixture.base.cfg
	cfg.Release = testReleaseLockFixture(t)
	cfg.Public.Chain.ExpectedRuntimeSpec, cfg.Public.Chain.ConfigIdentityRuntimeSpec = 461, 455
	if hash, err := releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters); err != nil || hash != cfg.ConfigHash {
		t.Fatalf("explicit runtime migration changed original config identity: %v", err)
	}
}

// The actual failing preparation reader must retain its original source hash,
// pinned native uid, signatures and exact persisted bytes under current461.
func TestRuntimeEvidenceHistoricalRuntimeReplaysOriginalPreparation(t *testing.T) {
	fixture := newRuntimeEvidenceHistoricalRuntimeTest(t)
	upgradeRuntimeEvidenceHistoricalTest(t, fixture)
	before := append([]byte(nil), fixture.base.preparedBytes...)
	metadata, runtime := fixture.executor.substrate.chain.Meta, fixture.executor.substrate.chain.Runtime
	prepared, wire, err := fixture.executor.prepareRuntimeEvidenceActivationsV2(t.Context(), fixture.chain)
	if err != nil {
		t.Fatalf("original455 preparation replay under461: %v", err)
	}
	if prepared.PlanHash != fixture.base.plan.PlanHash || !bytes.Equal(before, wire) {
		t.Fatal("historical preparation changed signed original bytes or approval")
	}
	if fixture.executor.substrate.chain.Meta != metadata || fixture.executor.substrate.chain.Runtime != runtime {
		t.Fatal("historical preparation changed signing metadata")
	}
}

func TestRuntimeEvidenceHistoricalRuntimeRetainsEligibilityAndConsentChecks(t *testing.T) {
	fixture := newRuntimeEvidenceHistoricalRuntimeTest(t)
	upgradeRuntimeEvidenceHistoricalTest(t, fixture)
	for _, test := range []struct {
		name   string
		mutate func(*runtimeEvidenceActivationPreparedV2)
	}{
		{name: "plan", mutate: func(p *runtimeEvidenceActivationPreparedV2) { p.PlanHash = "0x" + strings.Repeat("11", 32) }},
		{name: "config", mutate: func(p *runtimeEvidenceActivationPreparedV2) { p.ConfigHash = "0x" + strings.Repeat("12", 32) }},
		{name: "uid", mutate: func(p *runtimeEvidenceActivationPreparedV2) { p.Members[0].ValidatorUid++ }},
		{name: "native hash", mutate: func(p *runtimeEvidenceActivationPreparedV2) { p.Native.Hash = "0x" + strings.Repeat("13", 32) }},
		{name: "consent", mutate: func(p *runtimeEvidenceActivationPreparedV2) { p.Members[0].VpkSignature[0] ^= 1 }},
	} {
		var candidate runtimeEvidenceActivationPreparedV2
		if err := json.Unmarshal(fixture.base.preparedBytes, &candidate); err != nil {
			t.Fatal(err)
		}
		test.mutate(&candidate)
		if err := fixture.executor.authenticateRuntimeEvidencePreparedV2(t.Context(), fixture.chain, &candidate); err == nil {
			t.Fatalf("changed %s became original source authority", test.name)
		}
	}
	fixture.stateLock.Lock()
	fixture.permits[1] = false
	fixture.stateLock.Unlock()
	if err := fixture.executor.authenticateRuntimeEvidencePreparedV2(t.Context(), fixture.chain, fixture.base.prepared); err == nil || !strings.Contains(err.Error(), "native eligibility or historical uid differs") {
		t.Fatalf("historical permit refusal: %v", err)
	}
}

// The outer carried-action route reconstructs its original approval and then
// reaches the same historical reader. An invalid later Evm checkpoint isolates
// that boundary without substituting a public activation acceptance verdict.
func TestRuntimeEvidenceHistoricalRuntimeCarriedActionUsesOriginalApproval(t *testing.T) {
	fixture := newRuntimeEvidenceHistoricalRuntimeTest(t)
	revised, entries := prepareRuntimeEvidenceSetupCarryV2Test(t, fixture.base)
	upgradeRuntimeEvidenceHistoricalTest(t, fixture)
	revised.ConfigIdentityRuntimeSpec = 455
	var err error
	revised.ReleaseLockHash, err = canonicalHashHex(fixture.base.cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	revised.PlanHash, err = revised.hash()
	if err != nil {
		t.Fatal(err)
	}
	source, err := runtimeEvidenceSetupSourcePlanV2(fixture.base.cfg, revised, fixture.base.stateDir, fixture.base.roles, fixture.base.prepared, fixture.base.preparedBytes, fixture.base.completed, entries)
	if err != nil || source.PlanHash != fixture.base.plan.PlanHash {
		t.Fatalf("original approved carry reconstruction: %v", err)
	}
	journal, err := OpenJournal(fixture.base.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	fixture.executor.plan, fixture.executor.journal = revised, journal
	fixture.executor.cfg.OperationalEVM = fixture.executor.substrate.chain.API.Client.URL()
	before := fixture.callCount("state_call")
	_, err = fixture.executor.runtimeEvidenceActivationPostStateV2(t.Context(), actionByID(t, revised, runtimeEvidenceActivationActionId(1, 1)), ChainHead{}, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "activation postcondition has no finalized checkpoint") || fixture.callCount("state_call") <= before {
		t.Fatalf("carried action bypassed original historical eligibility: %v", err)
	}
}

func TestRuntimeEvidenceHistoricalRuntimeHorizonSeparatesOriginalAndFreshReads(t *testing.T) {
	fixture := newRuntimeEvidenceHistoricalRuntimeTest(t)
	upgradeRuntimeEvidenceHistoricalTest(t, fixture)
	relay := &evidenceRelayRuntime{executor: fixture.executor}
	activation := fixture.base.prepared.Members[0].Activation
	if epoch, err := relay.readHorizonNative(t.Context(), activation, evidenceRelayNativeOriginalSnapshot); err != nil || epoch == 0 {
		t.Fatalf("original relay native anchor: epoch=%d error=%v", epoch, err)
	}
	for _, mode := range []evidenceRelayNativeReadMode{evidenceRelayNativeCurrentHead, evidenceRelayNativeCurrentSnapshot, 0} {
		if _, err := relay.readHorizonNative(t.Context(), activation, mode); err == nil {
			t.Fatalf("fresh or invalid mode %d admitted old455", mode)
		}
	}
	fixture.stateLock.Lock()
	fixture.permits[1] = false
	fixture.stateLock.Unlock()
	if _, err := relay.readHorizonNative(t.Context(), activation, evidenceRelayNativeOriginalSnapshot); err == nil {
		t.Fatal("historical horizon ignored original eligibility")
	}
}

func TestRuntimeEvidenceHistoricalRuntimeFreshPreparationStillRequires461(t *testing.T) {
	fixture := newRuntimeEvidenceHistoricalRuntimeTest(t)
	upgradeRuntimeEvidenceHistoricalTest(t, fixture)
	// Use a fresh private fixture directory; the preserved original preparation
	// is never removed or overwritten to test new mutation admission.
	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := ensurePrivateDir(fresh); err != nil {
		t.Fatal(err)
	}
	executor := *fixture.executor
	executor.stateDir = fresh
	if _, _, err := executor.prepareRuntimeEvidenceActivationsV2(t.Context(), fixture.chain); err == nil || !strings.Contains(err.Error(), "unreviewed identity") {
		t.Fatalf("fresh preparation used historical runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "evidence-v2-setup", "prepared.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused fresh preparation persisted consent: %v", err)
	}
}
