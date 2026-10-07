// Real protected-file controls retain completed custody refusals at the
// operator caller. They need no database, signer or external credentials.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// This valid protected-file baseline is read before any refusal is injected.
// The synthetic DSN is intentionally never offered to a database connection.
func monitorOperatorCredentialReadFixture(t *testing.T) (monitorOperatorPolicy, []byte) {
	t.Helper()
	policy := monitorOperatorTestPolicy()
	policy.DatabaseFile = filepath.Join(monitorMetricsTestDir(t), "operator.url")
	raw := []byte("synthetic credential bytes; no database endpoint\n")
	policy.DatabaseSha256 = monitorReadDigest(raw)
	if err := os.WriteFile(policy.DatabaseFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	known, err := readMonitorServiceFile(t.Context(), policy.DatabaseFile, 8192, true, monitorServiceReadHooks{})
	if err != nil || !bytes.Equal(known, raw) {
		t.Fatal("original protected-file baseline was not admitted", err)
	}
	return policy, raw
}

func TestMonitorOperatorCredentialReadPreservesCompletedFileRefusals(t *testing.T) {
	for _, fault := range []string{"permissions", "symlink", "oversized"} {
		t.Run(fault, func(t *testing.T) {
			policy, raw := monitorOperatorCredentialReadFixture(t)
			switch fault {
			case "permissions":
				if err := os.Chmod(policy.DatabaseFile, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(policy.DatabaseFile, policy.DatabaseFile+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(policy.DatabaseFile+".original", policy.DatabaseFile); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(policy.DatabaseFile, bytes.Repeat(raw, 8193/len(raw)+1), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if value, code := readMonitorOperator(t.Context(), policy, monitorServiceReadHooks{}); value != nil || code != "invalid" {
				t.Fatal("completed credential-file refusal became a read outage", fault, code)
			}
		})
	}
}

// A hard observed generation change wins even when the joined close also
// reports EIO. The old and replacement bytes are equal; only custody changed.
func TestMonitorOperatorCredentialChangedInodeDominatesCloseIo(t *testing.T) {
	policy, raw := monitorOperatorCredentialReadFixture(t)
	var replaced, closed int
	hooks := monitorServiceReadHooks{afterRead: func(file *os.File) error {
		if err := os.Rename(policy.DatabaseFile, policy.DatabaseFile+".original"); err != nil {
			return err
		}
		if err := os.WriteFile(policy.DatabaseFile, raw, 0600); err != nil {
			return err
		}
		replaced++
		return nil
	}, afterClose: func(file *os.File) error {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			return errors.New("actual credential descriptor was not closed")
		}
		closed++
		return syscall.EIO
	}}
	value, code := readMonitorOperator(t.Context(), policy, hooks)
	if value != nil || code != "changed" || replaced != 1 || closed != 1 {
		t.Fatal("observed credential generation loss borrowed a transient close cause", code, replaced, closed)
	}
}

func TestMonitorOperatorCredentialReadKeepsMissingIoAndCancellationUnavailable(t *testing.T) {
	for _, fault := range []string{"missing", "read-io", "close-io", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			policy, _ := monitorOperatorCredentialReadFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			hooks := monitorServiceReadHooks{}
			switch fault {
			case "missing":
				if err := os.Remove(policy.DatabaseFile); err != nil {
					t.Fatal(err)
				}
			case "read-io":
				hooks.afterRead = func(*os.File) error { return syscall.EIO }
			case "close-io":
				hooks.afterClose = func(file *os.File) error {
					if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
						return errors.New("actual descriptor was not closed")
					}
					return syscall.EIO
				}
			case "canceled":
				cancel()
			}
			if value, code := readMonitorOperator(ctx, policy, hooks); value != nil || code != "unavailable" {
				t.Fatal("incomplete credential observation became an integrity conclusion", fault, code)
			}
		})
	}
}

// These are the exact owned error shapes emitted by the shared post-read
// Stat/Lstat branch. An unavailable recheck supplies no replacement proof.
func TestMonitorOperatorCredentialIncompleteRecheckCannotProveReplacement(t *testing.T) {
	for _, cause := range []error{syscall.EIO, syscall.ETIMEDOUT, context.Canceled, os.ErrNotExist} {
		err := errors.Join(&monitorServiceReadError{code: "changed", cause: &os.PathError{Op: "lstat", Path: "/synthetic/credential", Err: cause}}, &monitorServiceReadError{code: "unavailable", cause: syscall.EIO})
		if code := monitorOperatorFileReadCode(err); code != "unavailable" {
			t.Fatal("incomplete final metadata read became a proved replacement", cause, code)
		}
	}
	observed := errors.Join(&monitorServiceReadError{code: "changed"}, &monitorServiceReadError{code: "unavailable", cause: syscall.EIO})
	if code := monitorOperatorFileReadCode(observed); code != "changed" {
		t.Fatal("separate close failure hid a completed original generation mismatch", code)
	}
}

// The actual public multi-role command durably publishes the contradiction
// without stopping a healthy validator. Restart retains the same incident.
func TestMonitorOperatorCredentialContradictionPublishesAndPreservesPeer(t *testing.T) {
	fixture := newMonitorServicesFixture(t, "alpha")
	policy := monitorOperatorTestPolicy()
	policy.DatabaseFile = filepath.Join(fixture.directory, "operator.url")
	raw := []byte("synthetic credential bytes; no database endpoint\n")
	policy.DatabaseSha256 = monitorReadDigest(raw)
	if err := os.WriteFile(policy.DatabaseFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(policy.DatabaseFile, 0644); err != nil {
		t.Fatal(err)
	}
	fixture.policy.Operators = []monitorOperatorPolicy{policy}
	fixture.writePolicy(t)
	url, entered, left := monitorServicesBlockedChain(t)
	cancel, done, sink, _ := startMonitorOperatorTest(t, fixture, url, monitorServiceHooks{})
	<-entered
	first, peer := <-sink.events, <-sink.validators
	cancel()
	code := <-done
	<-left
	if code != 0 || peer != "alpha" || first.Publication != "published" || first.State == nil || first.State.ReadStatus != "invalid" || first.State.Read.Latest == nil || first.State.Read.Latest.FirstCode != "invalid" || first.State.Record != nil {
		t.Fatal("public credential refusal lost severity or stopped its peer", code, peer, first)
	}
	_, metrics := monitorOperatorPaths(fixture.checkpointPath, fixture.metricsPath, policy.Role)
	metricBytes, err := os.ReadFile(metrics)
	if err != nil || !strings.Contains(string(metricBytes), "sn_mainnet_operator_read_severity{role=\"operator-a\"} 2\n") || !strings.Contains(string(metricBytes), "sn_mainnet_operator_read_current{role=\"operator-a\"} 0\n") {
		t.Fatal("actual metrics concealed the completed credential refusal", err)
	}
	cancel, done, sink, _ = startMonitorOperatorTest(t, fixture, url, monitorServiceHooks{})
	second := <-sink.events
	cancel()
	code = <-done
	if code != 0 || second.State == nil || second.State.Read.Latest == nil || second.State.Read.Latest.Id != first.State.Read.Latest.Id || second.State.Read.Latest.Observations != 2 || second.State.Read.Latest.LastCode != "invalid" {
		t.Fatal("restart erased or downgraded the original credential incident", code, second)
	}
}
