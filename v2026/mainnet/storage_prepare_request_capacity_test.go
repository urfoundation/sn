//go:build linux

// The public preparation command refuses impossible combined allowances
// before it creates either original request files or any target authority.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A full-size request journal plus a nonempty assignment allowance cannot
// fit the complete-root export profile even when this fresh target is empty.
func TestStoragePreparationRequestCapacityRefusesBeforeOriginalEffects(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	raw, err := os.ReadFile(f.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	owner := request.Owners[0]
	var scope validator.AttemptLedgerPreparationScope
	if err := json.Unmarshal(owner.Inputs, &scope); err != nil {
		t.Fatal(err)
	}
	scope.Requests = &validator.AttemptLedgerRequestPreparationScope{Preparation: validator.ProviderAttemptRequestPreparation{Identity: validator.ProviderAttemptRequestIdentity{Ledger: f.identity, Coordinator: scope.Coordinator, ClientId: connect.Id{17}, PolicyHash: [32]byte{19}},
		Limits: validator.ProviderAttemptRequestLimits{MaxRecords: 8, MaxRecordBytes: 8192, MaxJournalBytes: 1024 * 1024 * 1024 * 1024}, Birth: validator.AttemptBoundary{SettlementEpoch: 42, EVMBlock: 100, EVMBlockHash: "0x" + strings.Repeat("31", 32)}}}
	owner.Inputs, err = json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{owner})
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{"storage-prepare", "plan", "--request", f.requestPath, "--request-sha256", f.requestHash}, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "complete-root byte profile") {
		t.Fatal("public preparation accepted an unrestorable combined allowance", code, diagnostic.String())
	}
	entries, err := os.ReadDir(f.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("capacity refusal changed original target namespace", err)
	}
	if _, err := os.Lstat(filepath.Join(f.root, validator.ProviderAttemptRequestJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("capacity refusal created a request journal", err)
	}
	if _, err := unix.Getxattr(f.root, validator.ProviderAttemptRequestAttribute, nil); !errors.Is(err, unix.ENODATA) {
		t.Fatal("capacity refusal created original request authority", err)
	}
}
