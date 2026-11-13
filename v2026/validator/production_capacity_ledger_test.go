//go:build linux || darwin

// Real retained signed records survive the new resource admission. The
// capacity check cannot reset a ledger or replace its reviewed prefix.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

func TestProductionCapacityRuntimeRetainsSignedPrefixAndLaterProgress(t *testing.T) {
	original := newProductionRuntimeTestFixture(t, false)
	fixture := newAttemptRecordStoreTestFixture(t, 2)
	identity := AttemptLedgerIdentity{DeploymentID: original.cfg.DeploymentID, ChainID: original.cfg.ChainID, GenesisHash: original.cfg.GenesisHash,
		Netuid: original.cfg.Netuid, ValidatorID: original.cfg.ValidatorID, ValidatorUID: 0, NoID: original.cfg.Operators[0].NoID,
		ValidatorVPK: "0x" + hex.EncodeToString(fixture.validatorKey[32:])}
	// Assign the synthetic mainnet domain before producing any retained bytes.
	// The actual server proof payloads stay exact; the fixture validator signs
	// the complete new local record chain once before the test opens its owner.
	previous := zeroAttemptHash()
	var prefix AttemptLedgerHead
	for index := range fixture.recordTs {
		record := fixture.recordTs[index]
		record.Identity, record.PreviousHash = identity, previous
		record = resignAttemptRecordStoreTest(t, record, fixture.validatorKey)
		fixture.recordTs[index], previous = record, record.RecordHash
		if index < 8 {
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			prefix.LastSequence, prefix.Root = record.Sequence, record.RecordHash
			prefix.RecordBytes += uint64(len(raw))
		}
	}
	prefix.TrailCount = 1
	root := original.cfg.Operators[0].StateDir
	if err := os.Chmod(filepath.Dir(filepath.Dir(root)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	storage := durablefixture.New(t, t.Context(), root)
	raw := attemptLedgerDiskTestJSONL(t, fixture.recordTs)
	legacy := filepath.Join(root, attemptLedgerLegacyName)
	if err := os.WriteFile(legacy, raw, 0600); err != nil {
		t.Fatal(err)
	}
	prepareAttemptLedgerCustodyTest(t, storage.Context, root, identity, fixture.validatorKey)
	cfg, approval := productionCapacityTestDraft(t, original.cfg, original.approval)
	cfg.ProductionCapacityRevision.Sources[0].Identity = identity
	cfg.ProductionCapacityRevision.Sources[0].Head = prefix
	signed := productionCapacityTestSign(t, cfg, approval, original.private)
	current, err := LoadReleaseConfig(signed.path)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := NewDiskAttemptLedger(storage.Context, root, identity, original.cfg.Coordinator, fixture.validatorKey, current.EvidenceV2.Bounds.Disk)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	before, err := ledger.Head()
	if err != nil || before.LastSequence != 16 {
		t.Fatal("actual later signed prefix was not retained", err)
	}
	for range 3 {
		if err := validateProductionCapacityLedger(t.Context(), current, identity.NoID, ledger); err != nil {
			t.Fatal("reviewed original prefix rejected its exact later signed progress", err)
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := validateProductionCapacityLedger(canceled, current, identity.NoID, ledger); !errors.Is(err, context.Canceled) {
		t.Fatal("capacity prefix read ignored cancellation", err)
	}
	for _, fault := range []string{"hash", "lost-count", "identity"} {
		cfg, approval := productionCapacityTestDraft(t, original.cfg, original.approval)
		source := &cfg.ProductionCapacityRevision.Sources[0]
		source.Identity, source.Head = identity, prefix
		switch fault {
		case "hash":
			source.Head.Root = attemptHex32([32]byte{0x93})
		case "lost-count":
			source.Head.LastSequence = before.LastSequence + 1
		case "identity":
			source.Identity.ValidatorUID++
		}
		signed := productionCapacityTestSign(t, cfg, approval, original.private)
		changed, err := LoadReleaseConfig(signed.path)
		if err != nil {
			t.Fatal("fault did not reach actual retained runtime prefix boundary", fault, err)
		}
		if err := validateProductionCapacityLedger(t.Context(), changed, identity.NoID, ledger); err == nil {
			t.Fatal("signed capacity request replaced actual retained prefix", fault)
		}
	}
	if after, err := ledger.Head(); err != nil || after != before {
		t.Fatal("capacity admission reset or advanced original progress", err)
	}
	retained, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(retained, raw) {
		t.Fatal("capacity admission changed original signed bytes", err)
	}
}
