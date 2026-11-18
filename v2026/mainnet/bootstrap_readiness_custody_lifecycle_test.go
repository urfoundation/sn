// A live reader distinguishes canceled observation from original integrity loss.
// Reopen always revalidates the retained independently approved signed intent.
package main

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A stopped parent can have all five completed child claims without its final
// preparation checkpoint. That valid interrupted phase remains resumable.
func TestBootstrapReadinessIncompletePreparationRemainsRecoverable(t *testing.T) {
	f := newBootstrapChainFixture(t)
	store, err := openBootstrapChainStore(f.preparation, true, nil, f.storageContext(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("synthetic interruption before final parent checkpoint")
	_, err = advanceBootstrapChain(f.storageContext(t.Context()), store, func(stage string) error {
		if stage == "root-retained" {
			return interrupted
		}
		return nil
	})
	if !errors.Is(err, interrupted) {
		t.Fatal("original parent interruption was not reached", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	before := f.journals(t)
	if _, err := openBootstrapChainReadinessState(f.storageContext(t.Context()), f.preparation); err == nil || errors.Is(err, errRpcIntegrity) {
		t.Fatal("valid interrupted preparation became complete or permanently invalid", err)
	}
	if !maps.Equal(before, f.journals(t)) {
		t.Fatal("read-only refusal changed pending original preparation")
	}
	f.result(t, "resume")
	state, err := openBootstrapChainReadinessState(f.storageContext(t.Context()), f.preparation)
	if err != nil {
		t.Fatal("original owner could not complete its interrupted preparation", err)
	}
	defer state.close()
}

// Every completed child is immutable for this owner. Even equivalent JSON
// whitespace is a changed original file; restoring it cannot reset a failure.
func TestBootstrapReadinessRetainsSignedIntentAcrossIntegrityFailure(t *testing.T) {
	f := newBootstrapChainReadinessFixture(t)
	if _, code, detail := f.contracts.command("resume", "--signed-transaction", f.contracts.signedPath, "--signed-transaction-hash", f.contracts.signedHash); code != 0 {
		t.Fatal(code, detail)
	}
	receipt := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.path), "synthetic-readiness-root-signature.json"), f.root.offline.receipt(t))
	f.root.result(t, "resume", "--signature-file", receipt.Path, "--signature-sha256", receipt.Sha256)
	f.result(t, "resume")
	original := f.journals(t)
	for index, path := range f.preparation.childPaths() {
		state, err := openBootstrapChainReadinessState(f.storageContext(t.Context()), f.preparation)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := state.checkpoint(ctx); !errors.Is(err, context.Canceled) || errors.Is(err, errRpcIntegrity) {
			t.Fatal("canceled observation became custody loss", err)
		}
		if err := state.checkpoint(t.Context()); err != nil {
			t.Fatal("canceled observation poisoned intact original custody", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(append([]byte(nil), raw...), '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		if err := state.checkpoint(t.Context()); !errors.Is(err, errRpcIntegrity) {
			t.Fatal("changed original journal was recoverable pending", index, err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := state.checkpoint(t.Context()); !errors.Is(err, errRpcIntegrity) {
			t.Fatal("restoring bytes renewed a failed reader", index, err)
		}
		if state.ContractTransactionHash != f.contracts.tx.Hash().Hex() || state.RootExtrinsicHash == "" || !maps.Equal(original, f.journals(t)) {
			t.Fatal("integrity refusal changed original signed intent", index)
		}
		if err := state.close(); err != nil {
			t.Fatal(err)
		}
		if err := state.checkpoint(t.Context()); err == nil {
			t.Fatal("closed reader retained authority")
		}
	}
	state, err := openBootstrapChainReadinessState(f.storageContext(t.Context()), f.preparation)
	if err != nil {
		t.Fatal("original restored fixture cannot reopen", err)
	}
	defer state.close()
}

// Trim's separate owner may retain pending work only while the original five
// remain borrowed, including after its own synced journal publication.
func TestOwnerTrimRejectsLostBorrowedCustodyAfterPublication(t *testing.T) {
	chain, f := ownerTrimPreparedTestFixture(t)
	store, err := openOwnerTrimStore(chain.storageContext(t.Context()), chain.preparation, f.config, f.key, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	var restore func()
	store.syncDirectory = func(directory *os.File) error {
		restore = bootstrapReadinessTestReplace(t, chain.preparation.childPaths()[3]+".lock")
		return directory.Sync()
	}
	if err := store.save(record); !errors.Is(err, errRpcIntegrity) || restore == nil {
		t.Fatal("post-publication original marker replacement retained trim authority", err)
	}
	restore()
	if _, err := store.load(); !errors.Is(err, errRpcIntegrity) {
		t.Fatal("restored source marker erased trim integrity refusal", err)
	}
}

// Current admission may finish before its last manager read. Its original
// preparation must still be owned immediately before issuing the counted start.
func TestValidatorCurrentRejectsBorrowedCustodyAfterFinalManagerRead(t *testing.T) {
	f := newValidatorActivationCurrentFixture(t)
	custody, err := openBootstrapChainReadinessState(f.f.chain.storageContext(t.Context()), f.f.chain.preparation)
	if err != nil {
		t.Fatal(err)
	}
	defer custody.close()
	f.authority.custody = custody.checkpoint
	original := f.f.host.host.execute
	var restore func()
	f.f.host.host.execute = func(ctx context.Context, path string, args []string) ([]byte, error) {
		raw, err := original(ctx, path, args)
		if err == nil && restore == nil && strings.Contains(strings.Join(args, " "), " show ") {
			journal, err := os.ReadFile(f.f.approval.Plan.StatePath)
			var record validatorActivationRecord
			if err == nil {
				err = decodePlanJson(journal, &record)
			}
			if err != nil {
				return nil, err
			}
			if record.Units[0].RecheckedReadinessHash != "" {
				restore = bootstrapReadinessTestReplace(t, f.f.chain.preparation.childPaths()[4]+".lock")
			}
		}
		return raw, err
	}
	result, code, detail := f.f.command(t.Context(), "start", f.authority)
	if restore != nil {
		restore()
	}
	if restore == nil || code != 3 || f.f.starts != [2]int{} || result.Units[0].StartAt.IsZero() || result.Units[0].Generation != nil || !result.Units[1].StartAt.IsZero() {
		t.Fatal("final manager read bypassed original custody before start", restore != nil, code, f.f.starts, result, detail)
	}
}
