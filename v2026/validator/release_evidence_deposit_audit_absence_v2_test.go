//go:build linux || darwin

// Real signed audit/closed publications distinguish initial absence from
// custody loss. Observers fault actual reads and Close, never replace evidence.
package validator

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

// Both names come from the actual authenticated fixture's fixed subject.
func depositAuditAbsenceV2TestPaths(t *testing.T, fixture *depositAuditPublicationV2TestFixture) (string, string) {
	t.Helper()
	path, err := ValidatorEvidenceDepositAuditV2ManifestPath(fixture.base.runtime.cfg.StateDir, fixture.options.Window.Epoch, fixture.options.Window.Subject)
	if err != nil {
		t.Fatal(err)
	}
	return path, filepath.Join(fixture.base.runtime.cfg.StateDir, "evidence-deposit-audit-prepared", filepath.Base(path))
}

// An existing private namespace with no leaf has the same fresh-publication
// authority as a missing namespace; only the completed writer creates bytes.
func TestValidatorEvidenceDepositAuditV2PublishesInitiallyMissingLeaf(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	path, _ := depositAuditAbsenceV2TestPaths(t, fixture)
	parent, err := openReleaseMeasurementInputV2Parents(t.Context(), filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(parent.check(), parent.close()); err != nil {
		t.Fatal(err)
	}
	if manifest, err := ReadValidatorEvidenceDepositAuditV2Manifest(t.Context(), path, fixture.options.Bounds.MaxClosureBytes, fixture.options.Bounds.MaxParticipants); manifest != nil || !releaseMeasurementInputV2InitialAbsence(err) {
		t.Fatalf("initially missing leaf lacks completed absence witness: %v", err)
	}
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	manifest, _ := fixture.retained(t)
	publication, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options)
	if err != nil || publication == nil || len(publication.Members) != len(fixture.options.Activations) {
		t.Fatalf("missing-leaf publication lost actual complete public consent: %v", err)
	}
}

// Actual closed and audit readers grant the same marker. A raw not-found or
// a later joined cancellation/Close error cannot inherit that permission.
func TestValidatorEvidenceDepositAuditV2InitialAbsenceRejectsMixedErrors(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "no-payout")
	path, _ := depositAuditAbsenceV2TestPaths(t, fixture)
	_, auditErr := ReadValidatorEvidenceDepositAuditV2Manifest(t.Context(), path, fixture.options.Bounds.MaxClosureBytes, fixture.options.Bounds.MaxParticipants)
	closedPath, err := ValidatorEvidencePublicationV2ManifestPath(fixture.base.runtime.cfg.StateDir, fixture.options.Window.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	_, closedErr := ReadValidatorEvidencePublicationV2Manifest(t.Context(), closedPath, fixture.options.Bounds.MaxClosureBytes, fixture.options.Bounds.MaxParticipants)
	_, rawErr := os.Lstat(path)
	failure := errors.New("synthetic post-close failure")
	for _, observed := range []error{auditErr, closedErr} {
		if !releaseMeasurementInputV2InitialAbsence(observed) {
			t.Fatalf("actual clean initial absence was refused: %v", observed)
		}
		for _, changed := range []error{nil, rawErr, errReleaseMeasurementInputV2InitiallyMissing, errors.Join(observed, context.Canceled), errors.Join(observed, failure)} {
			if releaseMeasurementInputV2InitialAbsence(changed) {
				t.Fatalf("unmarked or mixed error became initial publication authority: %v", changed)
			}
		}
	}
	if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("initial-absence admission created a discovery namespace: %v", err)
	}
}

// The final actual Close runs before cancellation/failure/retarget. No new
// prepared consent or upload may precede that initial-absence witness.
func TestValidatorEvidenceDepositAuditV2InitialCloseFailureCannotSign(t *testing.T) {
	for _, fault := range []string{"close", "cancel", "retarget"} {
		fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
		path, preparedPath := depositAuditAbsenceV2TestPaths(t, fixture)
		parentPath := filepath.Dir(path)
		parent, err := openReleaseMeasurementInputV2Parents(t.Context(), parentPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(parent.check(), parent.close()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		failure, closed := errors.New("synthetic actual-close failure"), 0
		err = fixture.base.runtime.publishDepositAuditV2WithReadHooks(ctx, fixture.artifact, releaseMeasurementInputV2ReadHooks{afterClose: func(file *os.File) error {
			if file.Name() != parentPath {
				return nil
			}
			closed++
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("locator observer preceded actual parent Close")
			}
			switch fault {
			case "close":
				return failure
			case "cancel":
				cancel()
			case "retarget":
				if err := os.Rename(parentPath, parentPath+"-preserved"); err != nil {
					return err
				}
				return os.Mkdir(parentPath, 0o700)
			}
			return nil
		}})
		cancel()
		if closed != 1 || err == nil || releaseMeasurementInputV2InitialAbsence(err) || fixture.posts.Load() != 0 {
			t.Fatalf("%s failure escaped pre-signing custody: closed=%d posts=%d error=%v", fault, closed, fixture.posts.Load(), err)
		}
		if fault == "close" && !errors.Is(err, failure) || fault == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatalf("%s failure lost its actual cause: %v", fault, err)
		}
		if _, err := os.Lstat(preparedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s failed witness produced replacement signatures: %v", fault, err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s failed witness published a completed locator: %v", fault, err)
		}
	}
}

// A once-published locator disappearing during a later read is not a fresh
// subject. Retained consent survives unchanged and no partial retry uploads.
func TestValidatorEvidenceDepositAuditV2LateLocatorDisappearanceDoesNotRepublish(t *testing.T) {
	for _, phase := range []string{"leaf-observed", "parent-closed"} {
		fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
		if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
			t.Fatal(err)
		}
		manifest, prepared := fixture.retained(t)
		if discovered, err := DiscoverValidatorEvidenceDepositAuditV2Manifests(t.Context(), fixture.base.runtime.cfg.StateDir, fixture.options.Bounds); err != nil || len(discovered) != 1 {
			t.Fatalf("original completed locator was not genuinely discoverable: %v", err)
		}
		path, preparedPath := depositAuditAbsenceV2TestPaths(t, fixture)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		posts, observed := fixture.posts.Load(), 0
		hooks := releaseMeasurementInputV2ReadHooks{
			step: func(operation string, _ *os.File, _ int) error {
				if phase != "leaf-observed" || operation != phase || observed != 0 {
					return nil
				}
				observed++
				return os.Rename(path, path+"-preserved")
			},
			afterClose: func(file *os.File) error {
				if phase != "parent-closed" || file.Name() != filepath.Dir(path) || observed != 0 {
					return nil
				}
				observed++
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					return errors.New("locator observer preceded actual parent Close")
				}
				return os.Rename(path, path+"-preserved")
			},
		}
		err = fixture.base.runtime.publishDepositAuditV2WithReadHooks(t.Context(), fixture.artifact, hooks)
		if observed != 1 || !errors.Is(err, os.ErrNotExist) || releaseMeasurementInputV2InitialAbsence(err) || fixture.posts.Load() != posts {
			t.Fatalf("%s disappearance authorized another publication: observed=%d posts=%d/%d error=%v", phase, observed, fixture.posts.Load(), posts, err)
		}
		if retained, err := os.ReadFile(preparedPath); err != nil || !bytes.Equal(retained, prepared) {
			t.Fatalf("%s disappearance changed original prepared signatures: %v", phase, err)
		}
		if retained, err := os.ReadFile(path + "-preserved"); err != nil || !bytes.Equal(retained, original) {
			t.Fatalf("%s disappearance lost original locator bytes: %v", phase, err)
		}
		if err := os.Rename(path+"-preserved", path); err != nil {
			t.Fatal(err)
		}
		if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil || fixture.posts.Load() != posts {
			t.Fatalf("restored original publication did not reuse exact consent: %v", err)
		}
		if got, err := ReadValidatorEvidenceDepositAuditV2(t.Context(), manifest, fixture.options); err != nil || got == nil {
			t.Fatalf("restored exact public publication failed: %v", err)
		}
	}
}

// A completed public slot cannot recreate its missing original private
// signatures just because its locator suffers an additional late disappearance.
func TestValidatorEvidenceDepositAuditV2LostPreparedCustodyAndLateLocatorCannotResign(t *testing.T) {
	fixture := newDepositAuditPublicationV2TestFixture(t, "positive")
	if err := fixture.base.runtime.publishDepositAuditV2(t.Context(), fixture.artifact); err != nil {
		t.Fatal(err)
	}
	_, prepared := fixture.retained(t)
	path, preparedPath := depositAuditAbsenceV2TestPaths(t, fixture)
	if err := os.Rename(preparedPath, preparedPath+"-preserved"); err != nil {
		t.Fatal(err)
	}
	posts, observed := fixture.posts.Load(), 0
	err := fixture.base.runtime.publishDepositAuditV2WithReadHooks(t.Context(), fixture.artifact, releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation != "leaf-observed" || observed != 0 {
			return nil
		}
		observed++
		return os.Rename(path, path+"-preserved")
	}})
	if observed != 1 || !errors.Is(err, os.ErrNotExist) || fixture.posts.Load() != posts {
		t.Fatalf("lost original custody and late locator absence authorized replacement: observed=%d error=%v", observed, err)
	}
	if _, err := os.Lstat(preparedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("late not-found recreated missing original signatures: %v", err)
	}
	if got, err := os.ReadFile(preparedPath + "-preserved"); err != nil || !bytes.Equal(got, prepared) {
		t.Fatalf("refusal changed preserved original signatures: %v", err)
	}
}

// The sibling closed-census path must obey the identical absence contract
// after real disk restart, before another signed public artifact is produced.
func TestReleaseRuntimeV2ClosedLocatorLateDisappearanceDoesNotResign(t *testing.T) {
	fixture := newReleaseRuntimeV2TestFixture(t)
	snapshot := &ReleaseSnapshot{Epoch: big.NewInt(9), BlockNumber: 2001, BlockHash: fixture.startup.blocks[2001]}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	before := [2]map[uint64]int{fixture.stores[0].counts(), fixture.stores[1].counts()}
	fixture.startup.reopen(t)
	fixture.start(t)
	path, err := ValidatorEvidencePublicationV2ManifestPath(fixture.startup.cfg.StateDir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadValidatorEvidencePublicationV2Manifest(t.Context(), path, fixture.startup.cfg.EvidenceV2.Bounds.MaxClosureBytes, 2); err != nil {
		t.Fatal(err)
	}
	observed := 0
	err = fixture.runtime.publishWithReadHooks(t.Context(), snapshot, releaseMeasurementInputV2ReadHooks{step: func(operation string, _ *os.File, _ int) error {
		if operation != "leaf-observed" || observed != 0 {
			return nil
		}
		observed++
		return os.Rename(path, path+"-preserved")
	}})
	if observed != 1 || !errors.Is(err, os.ErrNotExist) || releaseMeasurementInputV2InitialAbsence(err) {
		t.Fatalf("closed late disappearance became fresh signing authority: observed=%d error=%v", observed, err)
	}
	for index, store := range fixture.stores {
		if !maps.Equal(before[index], store.counts()) {
			t.Fatal("closed late disappearance uploaded a replacement public census")
		}
	}
	if err := os.Rename(path+"-preserved", path); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.advance(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	for index, store := range fixture.stores {
		if !maps.Equal(before[index], store.counts()) {
			t.Fatal("restored closed publication did not reuse original consents")
		}
	}
}
