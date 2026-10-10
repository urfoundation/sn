//go:build linux || darwin

// The public fixed dispatcher enrolls only explicit fresh request custody and
// later restores its complete original bytes before the real runtime reopens.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Birth and capacity are reviewed public inputs. The synthetic key remains
// outside every request and plan and is used only by the actual runtime owner.
func storageRequestRestoreCommandFixture(t *testing.T, loseOriginal bool, configure ...func(*validator.ProviderAttemptRequestJournal, validator.ProviderAttemptRequestPreparation)) (*storageSnapshotRestoreFixture, validator.AttemptLedgerPreparationScope, []byte) {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	raw, err := os.ReadFile(source.requestPath)
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
	preparation := validator.ProviderAttemptRequestPreparation{Identity: validator.ProviderAttemptRequestIdentity{Ledger: source.identity, Coordinator: scope.Coordinator, ClientId: connect.Id{17}, PolicyHash: [32]byte{19}},
		Limits: validator.ProviderAttemptRequestLimits{MaxRecords: 8, MaxRecordBytes: 8192, MaxJournalBytes: 64 * 1024}, Birth: validator.AttemptBoundary{SettlementEpoch: 42, EVMBlock: 100, EVMBlockHash: "0x" + strings.Repeat("31", 32)}}
	scope.Requests = &validator.AttemptLedgerRequestPreparationScope{Preparation: preparation}
	owner.Inputs, err = json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	storagePreparationOwnerRequest(t, source, "daemon", []durablevolume.PreparationOwner{owner})
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	journal, err := validator.OpenProviderAttemptRequestJournal(ctx, source.root, preparation, source.key)
	if err != nil {
		t.Fatal("public fresh preparation omitted original request custody", err)
	}
	defer journal.Close()
	head, birth, err := journal.Head(t.Context())
	if err != nil || head != (validator.ProviderAttemptRequestHead{}) || birth != preparation.Birth {
		t.Fatal("public preparation changed original birth or created request history", head, err)
	}
	public := source.key.Public().(ed25519.PublicKey)
	nonce := bytes.Repeat([]byte{33}, 32)
	message, err := connect.BuildVerifySeedMessage(public, nonce, 8)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(source.key, message)
	body, err := json.Marshal(connect.VerifySeedArgs{ClientId: preparation.Identity.ClientId, Vpk: public, ClientNonce: nonce, M: 8, SeedSig: signature})
	if err != nil {
		t.Fatal(err)
	}
	original, err := journal.Append(t.Context(), preparation.Birth, connect.Id{23}, body, message, signature)
	if err != nil {
		t.Fatal(err)
	}
	for _, configure := range configure {
		configure(journal, preparation)
	}
	head, _, err = journal.Head(t.Context())
	if err := errors.Join(err, journal.Close()); err != nil || head.Sequence != 1 {
		t.Fatal("actual original request did not join with its retained head", err)
	}
	scope.Requests.ExpectedHead = head
	owner.Inputs, err = json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	originalRaw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if loseOriginal {
		if err := unix.Removexattr(source.root, validator.ProviderAttemptRequestAttribute); err != nil {
			t.Fatal(err)
		}
	}
	f := storageSnapshotRestoreTarget(t, source, ctx, owner, false)
	f.target.key = source.key
	return f, scope, originalRaw
}

// This reaches fresh plan/apply, actual signed append, physical export, copied
// plan/apply, and runtime reopen. No test initializes the restored checkpoint.
func TestStoragePreparationRequestJournalRoundTripKeepsActualOriginal(t *testing.T) {
	f, scope, original := storageRequestRestoreCommandFixture(t, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, f.target, "storage-prepare")
	var output, diagnostic bytes.Buffer
	if code := runMain(f.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public request restore refused original custody", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("public request restore gained restart authority", err)
	}
	before, err := os.ReadFile(filepath.Join(f.archive, validator.ProviderAttemptRequestJournalName))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(f.target.root, validator.ProviderAttemptRequestJournalName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("public restore changed original signed request bytes", err)
	}
	ctx := durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), f.target.storage.Host)
	journal, err := validator.OpenProviderAttemptRequestJournal(ctx, f.target.root, scope.Requests.Preparation, f.target.key)
	if err != nil {
		t.Fatal("publicly restored request owner could not reopen", err)
	}
	defer journal.Close()
	head, birth, err := journal.Head(t.Context())
	if err != nil || head != scope.Requests.ExpectedHead || birth != scope.Requests.Preparation.Birth {
		t.Fatal("restored runtime replaced original request head or birth", head, err)
	}
	count := 0
	if err := journal.Walk(t.Context(), func(record validator.ProviderAttemptRequestRecord) error {
		count++
		raw, err := json.Marshal(record)
		if err != nil || !bytes.Equal(raw, original) {
			return errors.Join(err, errors.New("restored request lost its original signed transport bytes"))
		}
		return nil
	}); err != nil || count != 1 {
		t.Fatal("restored runtime omitted or duplicated an unanswered original request", count, err)
	}
}

// The physical exporter can report loss; that report cannot grant a new birth
// or authorize a replacement empty request journal at the retained pathname.
func TestStoragePreparationRequestRestoreRefusesLostOriginalBirth(t *testing.T) {
	f, _, _ := storageRequestRestoreCommandFixture(t, true)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.target.ctx, []string{"storage-prepare", "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "original request journal or checkpoint") {
		t.Fatal("public restore recreated lost original request birth", code, diagnostic.String())
	}
	entries, err := os.ReadDir(f.target.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused request restore created replacement target bytes", err)
	}
}
