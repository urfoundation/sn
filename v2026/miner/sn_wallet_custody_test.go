//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

// Real private files, signed originals and a second process exercise wallet
// handoff custody. Public HTTP retry coverage lives with the command tests.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"golang.org/x/sys/unix"
)

const snWalletConsentTestApiUrl = "http://127.0.0.1:18443"

// Every fixture owns a fresh private namespace and synthetic credential bytes.
func snWalletConsentTestPath(t testing.TB) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "synthetic-provider.jwt")
	if err := os.WriteFile(path, []byte("synthetic provider credential, never sent"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Custody accepts either protocol schema; prospective runtime approval remains
// the command's concern. Nonzero first generation models an existing wallet.
func snWalletConsentTestStatement() protocol.WalletMappingStatement {
	return protocol.WalletMappingStatement{
		Schema: protocol.WalletMappingConsentSchema,
		Domain: protocol.ClientKeyHistoryDomain{ChainID: 12345, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6},
		UserId: [16]byte{7}, ClientId: [16]byte{8}, NetworkId: [16]byte{9},
		Generation: 5, PreviousHash: [32]byte{10}, Nonce: [32]byte{11}, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 20, ThroughEpoch: 200,
	}
}

// Real randomized sr25519 signatures make accidental reconstruction observable.
func snWalletConsentTestOriginal(t testing.TB, statement protocol.WalletMappingStatement, seed byte) (protocol.WalletMappingConsent, [32]byte) {
	t.Helper()
	key, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	statement.Coldkey = key.Public().Encode()
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := key.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte("<Bytes>"+message+"</Bytes>")))
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.WalletMappingConsent{Message: message, Signature: signature.Encode()}
	_, hash, err := protocol.VerifyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hash
}

// Restart preserves the same nonce/signature, and rotation retains its exact
// acknowledged predecessor without allowing a stale response to settle it.
func TestSnWalletConsentRetainsOriginalAcrossRestartAndRotation(t *testing.T) {
	path := snWalletConsentTestPath(t)
	owner, err := openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.close() }()
	statement := snWalletConsentTestStatement()
	first, firstHash := snWalletConsentTestOriginal(t, statement, 12)
	if err := owner.retain(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	owner, err = openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
	if err != nil {
		t.Fatal(err)
	}
	retained, acknowledged, err := owner.load(t.Context())
	if err != nil || retained == nil || *retained != first || acknowledged {
		t.Fatal("restart did not retain the exact pending original", acknowledged, err)
	}
	if err := owner.retain(t.Context(), first); err != nil {
		t.Fatal("identical retry was not idempotent", err)
	}
	statement.Generation++
	statement.PreviousHash, statement.FromEpoch, statement.Nonce = firstHash, 30, [32]byte{13}
	second, secondHash := snWalletConsentTestOriginal(t, statement, 14)
	if err := owner.retain(t.Context(), second); err == nil {
		t.Fatal("unacknowledged original was replaced")
	}
	if err := owner.acknowledge(t.Context(), firstHash, 6); err == nil {
		t.Fatal("wrong generation acknowledged the original")
	}
	if err := owner.acknowledge(t.Context(), secondHash, 5); err == nil {
		t.Fatal("wrong hash acknowledged the original")
	}
	if err := owner.acknowledge(t.Context(), firstHash, 5); err != nil {
		t.Fatal(err)
	}
	if err := owner.retain(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if err := owner.acknowledge(t.Context(), firstHash, 5); err == nil {
		t.Fatal("stale response acknowledged a successor")
	}
	raw, err := os.ReadFile(filepath.Join(path+".wallet-consent", snWalletConsentJournalName))
	if err != nil {
		t.Fatal(err)
	}
	journal, _, _, err := decodeSnWalletConsent(t.Context(), raw, snWalletConsentTestApiUrl)
	if err != nil || len(journal.Records) != 2 || journal.Records[0].Original != first || !journal.Records[0].Acknowledged || journal.Records[1].Original != second || journal.Records[1].Acknowledged {
		t.Fatal("rotation did not preserve original handoffs", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	owner, err = openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
	if err != nil {
		t.Fatal(err)
	}
	retained, acknowledged, err = owner.load(t.Context())
	if err != nil || retained == nil || *retained != second || acknowledged {
		t.Fatal("successor did not survive restart", acknowledged, err)
	}
	if err := owner.acknowledge(t.Context(), secondHash, 6); err != nil {
		t.Fatal(err)
	}
}

// An independently issued later generation can have missing local rotations.
// It still cannot change the selected identity or move earning epochs backward.
func TestSnWalletConsentAllowsSparseExternalRotations(t *testing.T) {
	path := snWalletConsentTestPath(t)
	owner, err := openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.close() }()
	statement := snWalletConsentTestStatement()
	first, firstHash := snWalletConsentTestOriginal(t, statement, 15)
	if err := owner.retain(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := owner.acknowledge(t.Context(), firstHash, statement.Generation); err != nil {
		t.Fatal(err)
	}
	statement.Generation, statement.FromEpoch, statement.PreviousHash, statement.Nonce = 9, 40, [32]byte{16}, [32]byte{17}
	for index, mutate := range []func(*protocol.WalletMappingStatement){
		func(s *protocol.WalletMappingStatement) { s.Generation = 5 },
		func(s *protocol.WalletMappingStatement) { s.Generation = 6 },
		func(s *protocol.WalletMappingStatement) { s.FromEpoch = 20 },
		func(s *protocol.WalletMappingStatement) { s.ClientId[0]++ },
		func(s *protocol.WalletMappingStatement) { s.NetworkId[0]++ },
		func(s *protocol.WalletMappingStatement) { s.UserId[0]++ },
		func(s *protocol.WalletMappingStatement) { s.Domain.PolicyHash[0]++ },
	} {
		changed := statement
		mutate(&changed)
		original, _ := snWalletConsentTestOriginal(t, changed, 18)
		if err := owner.retain(t.Context(), original); !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Fatal("changed handoff identity or predecessor admitted", index, err)
		}
	}
	second, _ := snWalletConsentTestOriginal(t, statement, 19)
	if err := owner.retain(t.Context(), second); err != nil {
		t.Fatal("external generation gap was mistaken for local corruption", err)
	}
}

// Both pending and acknowledged originals remain tied to the exact endpoint.
func TestSnWalletConsentRefusesChangedEndpoint(t *testing.T) {
	path := snWalletConsentTestPath(t)
	statement := snWalletConsentTestStatement()
	original, hash := snWalletConsentTestOriginal(t, statement, 20)
	for _, acknowledge := range []bool{false, true} {
		owner, err := openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.retain(t.Context(), original); err != nil {
			t.Fatal(err)
		}
		if acknowledge {
			if err := owner.acknowledge(t.Context(), hash, statement.Generation); err != nil {
				t.Fatal(err)
			}
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
		journalPath := filepath.Join(path+".wallet-consent", snWalletConsentJournalName)
		before, err := os.ReadFile(journalPath)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := openSnWalletConsent(t.Context(), path, "http://127.0.0.1:18444")
		if changed != nil {
			_ = changed.close()
		}
		if changed != nil || err == nil {
			t.Fatal("another endpoint borrowed retained original custody")
		}
		after, err := os.ReadFile(journalPath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("endpoint refusal modified the original journal", err)
		}
	}
}

// Struct decoding alone accepts aliases, missing fields and duplicate keys.
// None can turn retained signed bytes into a newly normalized original.
func TestSnWalletConsentRejectsNoncanonicalJournal(t *testing.T) {
	original, _ := snWalletConsentTestOriginal(t, snWalletConsentTestStatement(), 21)
	raw, err := json.Marshal(snWalletConsentJournal{Schema: snWalletConsentSchema, ApiUrl: snWalletConsentTestApiUrl, Records: []snWalletConsentRecord{{Original: original}}})
	if err != nil {
		t.Fatal(err)
	}
	for index, changed := range [][]byte{
		append([]byte(" "), raw...),
		append(append([]byte{}, raw...), '\n'),
		bytes.Replace(raw, []byte(`"schema":`), []byte(`"Schema":`), 1),
		bytes.Replace(raw, []byte(`"schema":`), []byte(`"unknown":0,"schema":`), 1),
		bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"duplicate","schema":`), 1),
		bytes.Replace(raw, []byte(`,"acknowledged":false`), nil, 1),
		bytes.Replace(raw, []byte(`"records":[`), []byte(`"records":null,"Records":[`), 1),
	} {
		if journal, _, _, err := decodeSnWalletConsent(t.Context(), changed, snWalletConsentTestApiUrl); journal != nil || err == nil {
			t.Fatal("ambiguous retained journal admitted", index, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := decodeSnWalletConsent(ctx, raw, snWalletConsentTestApiUrl); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled inventory verification succeeded", err)
	}
}

// Cancellation owns initial custody work, including the first journal birth.
func TestSnWalletConsentCancellationDoesNotCreateCustody(t *testing.T) {
	path := snWalletConsentTestPath(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	owner, err := openSnWalletConsent(ctx, path, snWalletConsentTestApiUrl)
	if owner != nil {
		_ = owner.close()
	}
	if owner != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled invocation acquired wallet custody", err)
	}
	if _, err := os.Lstat(path + ".wallet-consent"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled invocation created a custody directory", err)
	}
}

// No private file alias, fifo, missing journal or abandoned partial write may
// be interpreted as permission to create a replacement signed handoff.
func TestSnWalletConsentRefusesUnsafeRetainedResources(t *testing.T) {
	for _, fault := range []string{"permissions", "symlink", "hardlink", "fifo", "missing", "pending", "oversized", "directory-symlink", "credential-symlink"} {
		path := snWalletConsentTestPath(t)
		owner, err := openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
		if err != nil {
			t.Fatal(fault, err)
		}
		if err := owner.close(); err != nil {
			t.Fatal(fault, err)
		}
		journalPath := filepath.Join(path+".wallet-consent", snWalletConsentJournalName)
		switch fault {
		case "permissions":
			err = os.Chmod(journalPath, 0o644)
		case "symlink", "fifo":
			if err = os.Rename(journalPath, journalPath+".original"); err == nil {
				if fault == "symlink" {
					err = os.Symlink(journalPath+".original", journalPath)
				} else {
					err = unix.Mkfifo(journalPath, 0o600)
				}
			}
		case "hardlink":
			err = os.Link(journalPath, journalPath+".alias")
		case "missing":
			err = os.Remove(journalPath)
		case "pending":
			err = os.WriteFile(filepath.Join(path+".wallet-consent", snWalletConsentPendingName), []byte("partial original"), 0o600)
		case "oversized":
			err = os.Truncate(journalPath, snWalletConsentMaxBytes+1)
		case "directory-symlink":
			if err = os.Rename(path+".wallet-consent", path+".wallet-consent.original"); err == nil {
				err = os.Symlink(path+".wallet-consent.original", path+".wallet-consent")
			}
		case "credential-symlink":
			if err = os.Rename(path, path+".original"); err == nil {
				err = os.Symlink(path+".original", path)
			}
		}
		if err != nil {
			t.Fatal(fault, err)
		}
		changed, err := openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
		if changed != nil {
			_ = changed.close()
		}
		if changed != nil || err == nil {
			t.Fatal("unsafe resource was replaced or followed", fault, err)
		}
	}
}

// The lock is enforced by the kernel against an independent executable, and
// releasing this owner makes the same complete retained custody available.
func TestSnWalletConsentLeaseExcludesOtherProcesses(t *testing.T) {
	const childPathVariable = "SN_WALLET_CONSENT_TEST_CHILD_PATH"
	if path := os.Getenv(childPathVariable); path != "" {
		owner, err := openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
		if owner != nil {
			_ = owner.close()
		}
		if owner != nil || err == nil || !strings.Contains(err.Error(), "active owner") {
			t.Fatal("independent process did not encounter the actual wallet lease", err)
		}
		return
	}
	path := snWalletConsentTestPath(t)
	owner, err := openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.close() }()
	result := runSnWalletConsentTestChild(t.Context(), t.Name(), childPathVariable+"="+path)
	if result.contextErr != nil || result.outputExceeded {
		// A canceled or truncated child cannot supply an attributable assertion.
		t.Fatalf("second process lease observation incomplete: %v (context: %v, output exceeded: %v)", result.processErr, result.contextErr, result.outputExceeded)
	}
	if result.processErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(result.processErr, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("second process lease test did not exit with an assertion: %v", result.processErr)
		}
		// Preserve the child assertion's own file, line and exact cause. A setup
		// failure or panic must not earn credit from the generic parent exit.
		t.Fatalf("second process lease assertion failed: %v\n%s", result.processErr, result.output)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	owner, err = openSnWalletConsent(t.Context(), path, snWalletConsentTestApiUrl)
	if err != nil {
		t.Fatal("lease did not end with its owner", err)
	}
}

const snWalletConsentTestChildOutputLimit = 32 * 1024

// Drain both child streams while retaining a fixed prefix. Run joins their
// writers before the caller reads the result; overflow is always explicit.
type snWalletConsentTestChildOutput struct {
	mutex    sync.Mutex
	buffer   [snWalletConsentTestChildOutputLimit]byte
	size     int
	exceeded bool
}

func (self *snWalletConsentTestChildOutput) Write(value []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	count := copy(self.buffer[self.size:], value)
	self.size += count
	self.exceeded = self.exceeded || count != len(value)
	return len(value), nil
}

type snWalletConsentTestChildResult struct {
	output         string
	outputExceeded bool
	processErr     error
	contextErr     error
}

func runSnWalletConsentTestChild(parent context.Context, root string, childEnvironment string) snWalletConsentTestChildResult {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+root+"$", "-test.count=1")
	child.Env = append(os.Environ(), childEnvironment)
	child.WaitDelay = 5 * time.Second
	output := &snWalletConsentTestChildOutput{}
	child.Stdout, child.Stderr = output, output
	processErr := child.Run()
	output.mutex.Lock()
	defer output.mutex.Unlock()
	return snWalletConsentTestChildResult{
		output: string(output.buffer[:output.size]), outputExceeded: output.exceeded,
		processErr: processErr, contextErr: ctx.Err(),
	}
}

// This deliberately failing child checks the same bounded process owner used
// by the real lease assertion, including both independently written streams.
func TestSnWalletConsentChildAssertionDiagnosticsRemainObservable(t *testing.T) {
	const childVariable = "SN_WALLET_CONSENT_TEST_DIAGNOSTIC_CHILD"
	const stdoutMessage = "synthetic wallet child stdout"
	const stderrMessage = "synthetic wallet child stderr"
	const assertionMessage = "synthetic wallet child assertion"
	if os.Getenv(childVariable) == "1" {
		if _, err := os.Stdout.WriteString(stdoutMessage + "\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stderr.WriteString(stderrMessage + "\n"); err != nil {
			t.Fatal(err)
		}
		t.Fatal(assertionMessage)
	}
	result := runSnWalletConsentTestChild(t.Context(), t.Name(), childVariable+"=1")
	var exitErr *exec.ExitError
	if result.contextErr != nil || result.outputExceeded || !errors.As(result.processErr, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatal("diagnostic fixture child did not join its deliberate assertion", result.processErr, result.contextErr, result.outputExceeded)
	}
	for _, message := range []string{stdoutMessage, stderrMessage, assertionMessage} {
		if strings.Count(result.output, message) != 1 {
			t.Fatal("joined child diagnostic was lost or duplicated", message, result.output)
		}
	}
}

// Excess child output is drained without allocation growth and cannot be
// mistaken for a complete causal diagnostic by the owning lease test.
func TestSnWalletConsentChildAssertionDiagnosticsAreBounded(t *testing.T) {
	const childVariable = "SN_WALLET_CONSENT_TEST_LARGE_DIAGNOSTIC_CHILD"
	const payloadMarker = "\nsynthetic wallet child bounded payload\n"
	if os.Getenv(childVariable) == "1" {
		payload := payloadMarker + strings.Repeat("x", 2*snWalletConsentTestChildOutputLimit)
		if count, err := os.Stdout.WriteString(payload); err != nil || count != len(payload) {
			t.Fatal("synthetic child output did not reach its boundary", count, err)
		}
		t.Fatal("synthetic assertion after oversized output")
	}
	result := runSnWalletConsentTestChild(t.Context(), t.Name(), childVariable+"=1")
	var exitErr *exec.ExitError
	if result.contextErr != nil || !errors.As(result.processErr, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatal("bounded diagnostic fixture child did not join", result.processErr, result.contextErr)
	}
	if !result.outputExceeded || len(result.output) != snWalletConsentTestChildOutputLimit {
		t.Fatal("child output limit was not retained as an explicit incomplete observation", len(result.output), result.outputExceeded)
	}
	// Package initialization may write diagnostics before the selected test.
	// Retain that prefix and verify the actual payload at its explicit boundary.
	markerIndex := strings.Index(result.output, payloadMarker)
	if markerIndex < 0 || strings.Count(result.output, payloadMarker) != 1 {
		t.Fatal("bounded child observation did not retain its synthetic payload boundary")
	}
	tail := result.output[markerIndex+len(payloadMarker):]
	if len(tail) == 0 || tail != strings.Repeat("x", len(tail)) {
		t.Fatal("bounded child observation changed its retained payload suffix")
	}
}

// Exact-boundary admission retains the entire original prefix; later writes
// are drained, set sticky overflow and cannot replace any retained diagnostic.
func TestSnWalletConsentChildOutputRetainsExactPrefixAtBoundary(t *testing.T) {
	output := &snWalletConsentTestChildOutput{}
	preamble := "synthetic process initialization\n"
	if count, err := output.Write([]byte(preamble)); err != nil || count != len(preamble) {
		t.Fatal("child output recorder lost the initialization prefix", count, err)
	}
	payload := strings.Repeat("x", snWalletConsentTestChildOutputLimit-len(preamble))
	if count, err := io.Copy(output, strings.NewReader(payload)); err != nil || count != int64(len(payload)) {
		t.Fatal("child output recorder did not drain its exact-boundary payload", count, err)
	}
	expected := preamble + payload
	if output.size != snWalletConsentTestChildOutputLimit || output.exceeded || string(output.buffer[:output.size]) != expected {
		t.Fatal("exact child output boundary was truncated or changed")
	}
	for _, later := range []string{"synthetic first overflow", "", strings.Repeat("y", 2*snWalletConsentTestChildOutputLimit)} {
		if count, err := output.Write([]byte(later)); err != nil || count != len(later) {
			t.Fatal("child output recorder stopped draining after its fixed limit", count, err)
		}
		if output.size != snWalletConsentTestChildOutputLimit || !output.exceeded || string(output.buffer[:output.size]) != expected {
			t.Fatal("child output overflow changed or concealed its retained prefix")
		}
	}
}
