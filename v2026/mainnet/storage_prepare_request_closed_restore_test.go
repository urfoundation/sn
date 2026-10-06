//go:build linux

// Original signed close fences remain authority after complete physical
// request-owner restoration, including the next original request window.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Export, copy and public plan/apply must retain the first signed close. The
// reopened real owner refuses the closed epoch and continues its successor.
func TestStoragePreparationRequestClosedWindowKeepsActualContinuation(t *testing.T) {
	var window protocol.ValidatorEvidenceWindow
	var originalCut []byte
	f, scope, _ := storageRequestRestoreCommandFixture(t, false, func(journal *validator.ProviderAttemptRequestJournal, preparation validator.ProviderAttemptRequestPreparation) {
		window = protocol.ValidatorEvidenceWindow{Epoch: preparation.Birth.SettlementEpoch, StartBlock: preparation.Birth.EVMBlock, EndBlock: preparation.Birth.EVMBlock + 1, FinalizedBlock: preparation.Birth.EVMBlock + 1}
		cut, err := journal.SealWindow(t.Context(), window, preparation.Limits.MaxJournalBytes)
		if err != nil || cut == nil || len(cut.Records) != 1 {
			t.Fatal("actual original request window did not close", err)
		}
		originalCut, err = json.Marshal(cut)
		if err != nil {
			t.Fatal(err)
		}
	})
	readCheckpoint := func(path string) validator.ProviderAttemptRequestCheckpoint {
		t.Helper()
		raw := make([]byte, 4096)
		count, err := unix.Getxattr(path, validator.ProviderAttemptRequestAttribute, raw)
		if err != nil {
			t.Fatal("physical request checkpoint is missing", err)
		}
		var checkpoint validator.ProviderAttemptRequestCheckpoint
		if err := json.Unmarshal(raw[:count], &checkpoint); err != nil || checkpoint.Closed == nil {
			t.Fatal("restored checkpoint lost original signed close", err)
		}
		return checkpoint
	}
	original := readCheckpoint(f.heldSource)
	plan, hash := storagePreparationFreezeOwnerPlan(t, f.target, "storage-prepare")
	var output, diagnostic bytes.Buffer
	if code := runMain(f.target.ctx, []string{"storage-prepare", "apply", "--plan", plan, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public closed request restore refused original custody", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("closed request restore gained restart authority", err)
	}
	rebound := readCheckpoint(f.target.root)
	if rebound.DirectoryInode == original.DirectoryInode || rebound.FileInode == original.FileInode {
		t.Fatal("closed request restore retained stale physical coordinates")
	}
	rebound.DirectoryInode, rebound.FileInode = original.DirectoryInode, original.FileInode
	if !reflect.DeepEqual(rebound, original) {
		t.Fatal("restored checkpoint changed original signed close")
	}
	ctx := durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), f.target.storage.Host)
	preparation := scope.Requests.Preparation
	journal, err := validator.OpenProviderAttemptRequestJournal(ctx, f.target.root, preparation, f.target.key)
	if err != nil {
		t.Fatal("restored closed request owner could not reopen", err)
	}
	defer journal.Close()
	cut, err := journal.SealWindow(t.Context(), window, preparation.Limits.MaxJournalBytes)
	if err != nil || cut == nil {
		t.Fatal("restored owner lost its original close retry", err)
	}
	replayed, err := json.Marshal(cut)
	if err != nil || !bytes.Equal(replayed, originalCut) {
		t.Fatal("restore changed original signed window bytes", err)
	}
	public := f.target.key.Public().(ed25519.PublicKey)
	nonce := bytes.Repeat([]byte{35}, 32)
	message, err := connect.BuildVerifySeedMessage(public, nonce, 8)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(f.target.key, message)
	body, err := json.Marshal(connect.VerifySeedArgs{ClientId: preparation.Identity.ClientId, Vpk: public, ClientNonce: nonce, M: 8, SeedSig: signature})
	if err != nil {
		t.Fatal(err)
	}
	if record, err := journal.Append(t.Context(), preparation.Birth, connect.Id{23}, body, message, signature); !errors.Is(err, protocol.ErrProviderAttemptsUnavailable) || record != nil {
		t.Fatal("restored closed epoch accepted another original send", err)
	}
	head, birth, err := journal.Head(t.Context())
	if err != nil || head != scope.Requests.ExpectedHead || birth != preparation.Birth {
		t.Fatal("refused closed send changed original request authority", err)
	}
	nextBoundary := validator.AttemptBoundary{SettlementEpoch: window.Epoch + 1, EVMBlock: window.EndBlock, EVMBlockHash: "0x" + strings.Repeat("32", 32)}
	record, err := journal.Append(t.Context(), nextBoundary, connect.Id{23}, body, message, signature)
	if err != nil || record == nil || record.Sequence != head.Sequence+1 || record.PreviousHash != head.Hash {
		t.Fatal("restored request owner failed actual next-window continuation", err)
	}
	previous, err := cut.Header.Hash()
	if err != nil {
		t.Fatal(err)
	}
	nextWindow := protocol.ValidatorEvidenceWindow{Epoch: nextBoundary.SettlementEpoch, StartBlock: window.EndBlock, EndBlock: window.EndBlock + 1, FinalizedBlock: window.EndBlock + 1}
	next, err := journal.SealWindow(t.Context(), nextWindow, preparation.Limits.MaxJournalBytes)
	if err != nil || next == nil || next.Header.PreviousCutHash != previous || next.Header.Begin != cut.Header.End || len(next.Records) != 1 {
		t.Fatal("restored successor lost its original signed close predecessor", err)
	}
}
