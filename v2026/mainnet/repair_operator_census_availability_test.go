package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/urnetwork/server/v2026/strecovery"
)

// A pure transport failure remains retryable at the public command boundary,
// both before claim and after the same original process has acknowledged start.
func TestRepairOperatorPublicControllerRetriesUnavailableCensus(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		f := newRepairOperatorFixture(t)
		f.base.host.operator = &repairOperatorTransports{reader: f.reader, procRoot: f.envelope.procRoot}
		approvalRaw, err := os.ReadFile(f.approvalPath)
		if err != nil {
			t.Fatal(err)
		}
		manifest := repairControllerManifest{Schema: repairControllerSchema, MaximumParallel: 1, Entries: []repairControllerEntry{{Id: "synthetic-census-recovery", Kind: "operator", Approval: planFileReference{Path: f.approvalPath, Sha256: monitorReadDigest(approvalRaw)}, PublicKey: f.key}}}
		manifestRaw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(f.base.directory, "operator-census-controller-manifest.json")
		repairValidatorTestWrite(t, manifestPath, manifestRaw, 0600)
		checkpoint := filepath.Join(f.base.directory, "operator-census-controller.json")
		prepareMainnetSnapshotTest(t, checkpoint, "mainnet-host-action", 64*1024)
		args := []string{"--manifest", manifestPath, "--manifest-sha256", monitorReadDigest(manifestRaw), "--checkpoint", checkpoint, "--metrics-file", filepath.Join(f.base.directory, "operator-census-controller.prom")}
		run := func() repairControllerRecord {
			t.Helper()
			var out, diagnostic bytes.Buffer
			if code := runRepairControllerCommandWithHost(f.ctx(), args, &out, &diagnostic, func() time.Time { return f.base.now }, f.base.host); code != 0 {
				t.Fatal("public operator census controller failed", acknowledged, code, diagnostic.String())
			}
			var record repairControllerRecord
			if err := json.Unmarshal(out.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if err := record.validate(manifest, monitorReadDigest(manifestRaw)); err != nil {
				t.Fatal(err)
			}
			return record
		}
		readProcess := func() repairProcessRecord {
			t.Helper()
			raw, err := os.ReadFile(f.envelope.approval.Plan.StatePath)
			if err != nil {
				t.Fatal(err)
			}
			var record repairProcessRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			if err := record.validate(f.envelope, f.key); err != nil {
				t.Fatal(err)
			}
			return record
		}
		var prior repairProcessRecord
		expectedStarts := 0
		if acknowledged {
			expectedStarts = 1
			f.status.Store("error not ready: retained queue unavailable")
			first := run()
			if first.Entries[0].Status != "pending" || f.base.starts != 1 {
				t.Fatal("fixture did not reach acknowledged pending generation", first, f.base.starts)
			}
			prior = readProcess()
			if prior.Generation == nil || prior.StartAt.IsZero() {
				t.Fatal("fixture lost original start acknowledgement", prior)
			}
			f.status.Store("ok")
		}
		original, err := os.ReadFile(f.envelope.approval.Plan.OriginalCensus.Path)
		if err != nil {
			t.Fatal(err)
		}
		f.reader.err = &strecovery.CensusReadError{Source: "operator-one", Stage: "connection", Cause: syscall.ECONNREFUSED}
		failed := run()
		if failed.Entries[0].Status != "pending" || failed.Entries[0].Attempted != acknowledged || f.base.starts != expectedStarts {
			t.Fatal("temporary census read permanently held or renewed the original incident", acknowledged, failed, f.base.starts)
		}
		if acknowledged {
			current := readProcess()
			if current.Generation == nil || *current.Generation != *prior.Generation || current.StartAt != prior.StartAt || current.StartMonotonicUsec != prior.StartMonotonicUsec || current.Observations != prior.Observations+1 || !current.CompletedAt.IsZero() {
				t.Fatal("census failure lost the acknowledged generation or refunded its observation", prior, current)
			}
		} else if _, err := os.Lstat(f.envelope.approval.Plan.StatePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("preclaim census failure created a process journal", err)
		}
		f.reader.err = nil
		completed := run()
		if completed.Entries[0].Status != "completed" || f.base.starts != 1 {
			t.Fatal("same original controller entry could not recover its census", acknowledged, completed, f.base.starts)
		}
		current := readProcess()
		if acknowledged && (current.Generation == nil || *current.Generation != *prior.Generation || current.StartAt != prior.StartAt) {
			t.Fatal("healthy census adopted a replacement generation", prior, current)
		}
		retained, err := os.ReadFile(f.envelope.approval.Plan.OriginalCensus.Path)
		if err != nil || !bytes.Equal(original, retained) {
			t.Fatal("census retry rewrote original signed-attempt authority", err)
		}
	}
}

// The Server classifier admits only wholly transient read causes. Credentials,
// mixed hard failures and completed source contradictions retain refusal.
func TestRepairOperatorCensusAvailabilityPreservesRefusals(t *testing.T) {
	credential := &pgconn.PgError{Code: "28P01", Message: "synthetic private diagnostic"}
	refusal := &strecovery.Refusal{Source: "operator-one", Cause: "original signed bytes differ"}
	for _, test := range []struct {
		name   string
		cause  error
		status string
	}{
		{"connection refused", syscall.ECONNREFUSED, "pending"},
		{"disk read", syscall.EIO, "pending"},
		{"short read", io.ErrUnexpectedEOF, "pending"},
		{"serialization retry", &pgconn.PgError{Code: "40001"}, "pending"},
		{"pure transient join", errors.Join(syscall.EIO, io.EOF), "pending"},
		{"caller cancellation", context.Canceled, "pending"},
		{"credentials", credential, "held"},
		{"mixed hard read", errors.Join(syscall.EIO, credential), "held"},
		{"unknown read", errors.New("synthetic row decoding refusal"), "held"},
		{"original contradiction", errors.Join(syscall.EIO, refusal), "held"},
	} {
		original := &strecovery.CensusReadError{Source: "operator-one", Stage: "complete read-only snapshot", Cause: test.cause}
		got := repairOperatorCensusError(original)
		status, _ := repairControllerCause(got)
		if !errors.Is(got, original) || !errors.Is(got, test.cause) || status != test.status {
			t.Errorf("%s: original read cause or controller disposition differs: %s, %v", test.name, status, got)
		}
		if errors.Is(test.cause, refusal) != errors.Is(got, errRpcIntegrity) {
			t.Errorf("%s: availability changed semantic refusal authority: %v", test.name, got)
		}
	}
}
