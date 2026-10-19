//go:build linux || darwin

// Original signed source receipts need authenticated current finality, even
// when their body, events and historical commitment remain unchanged.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Real native header encoding supplies an opening descendant and one later
// descendant. Only the selected RPC witness can change; the receipt is fixed.
type releaseSourceFinalityWitnessFixture struct {
	*releaseSourceFinalityReadFixture
	openingHeader types.Header
	opening       types.Hash
	closingHeader types.Header
	closing       types.Hash
	heads         int
	fault         string
	failure       error
	injected      bool
}

// The original-runtime signer and event decoder remain those of the existing
// receipt fixture. Returned header bytes authenticate their independent hashes.
func newReleaseSourceFinalityWitnessFixture(t *testing.T, fault string) *releaseSourceFinalityWitnessFixture {
	t.Helper()
	self := &releaseSourceFinalityWitnessFixture{releaseSourceFinalityReadFixture: newReleaseSourceFinalityReadFixture(t), fault: fault}
	self.openingHeader, self.opening = releaseReceiptTestHeader(t, self.receipt.hash, self.receipt.number+1)
	self.closingHeader, self.closing = releaseReceiptTestHeader(t, self.opening, self.receipt.number+2)
	self.releaseSourceFinalityReadFixture.fault = self.read
	return self
}

// Faults act on one actual wire read after successful signed-source admission;
// no test callback can provide a verified result or bypass the real decoder.
func (self *releaseSourceFinalityWitnessFixture) read(_ context.Context, target any, method string, args ...any) (bool, error) {
	if method == "chain_getFinalizedHead" {
		self.heads++
	}
	if self.failure != nil && !self.injected && self.heads >= 2 {
		matches := self.fault == "head timeout" && method == "chain_getFinalizedHead" ||
			self.fault == "header timeout" && method == "chain_getHeader" ||
			self.fault == "witness timeout" && method == "chain_getBlockHash" && args[0] == self.receipt.number+1 ||
			self.fault == "receipt timeout" && method == "chain_getBlockHash" && args[0] == self.receipt.number
		if matches {
			self.injected = true
			return true, self.failure
		}
	}
	switch method {
	case "chain_getFinalizedHead":
		hash := self.opening
		if self.fault == "opening lower" || self.fault == "opening lower changed receipt" {
			hash = self.source.native.block
		}
		if self.heads >= 2 {
			switch self.fault {
			case "advanced", "closing orphan", "lost opening canonical", "lost receipt canonical":
				hash = self.closing
			case "regressed", "regressed changed opening", "regressed changed receipt":
				hash = self.receipt.hash
			}
		}
		return true, setReleaseHistoricalTestResult(target, hash.Hex())
	case "chain_getHeader":
		if args[0] == self.opening.Hex() {
			return true, setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(self.openingHeader))
		}
		if args[0] == self.closing.Hex() {
			return true, setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(self.closingHeader))
		}
	case "chain_getBlockHash":
		hash := types.Hash{}
		if args[0] == self.receipt.number+1 {
			hash = self.opening
			if self.fault == "opening orphan" || (self.fault == "lost opening canonical" || self.fault == "regressed changed opening") && self.heads >= 2 {
				hash = types.Hash{99}
			}
		} else if args[0] == self.receipt.number+2 {
			hash = self.closing
			if self.fault == "closing orphan" {
				hash = types.Hash{99}
			}
		} else if args[0] == self.receipt.number && (self.fault == "opening lower changed receipt" || (self.fault == "lost receipt canonical" || self.fault == "regressed changed receipt") && self.heads >= 2) {
			hash = types.Hash{99}
		}
		if hash != (types.Hash{}) {
			return true, setReleaseHistoricalTestResult(target, hash.Hex())
		}
	}
	return false, nil
}

// An authentic orphan header is insufficient finality. Closing regression or
// replacement cannot publish a previously successful source receipt either.
func TestReleaseSourceFinalityAuthenticatesOpeningAndClosingWitnesses(t *testing.T) {
	for _, fault := range []string{"unchanged", "advanced", "opening orphan", "closing orphan", "regressed", "lost opening canonical", "lost receipt canonical"} {
		fixture := newReleaseSourceFinalityWitnessFixture(t, fault)
		before, err := json.Marshal(fixture.source.intent.Prepared)
		if err != nil {
			t.Fatal(err)
		}
		err = fixture.verify(t.Context())
		wantSuccess := fault == "unchanged" || fault == "advanced"
		if (err == nil) != wantSuccess || err != nil && RetryableEvidenceTransportError(err) {
			t.Fatalf("%s original source finality success=%t heads=%d error=%v", fault, wantSuccess, fixture.heads, err)
		}
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if errors.As(err, &unavailable) != (fault == "regressed") || retryableProductionSteeringRead(err) != (fault == "regressed") {
			t.Fatalf("%s finality changed pending/canonical class: %v", fault, err)
		}
		if fault == "opening orphan" && (fixture.receipt.blocks != 0 || fixture.receipt.eventReads != 0 || fixture.receipt.commitmentReads != 0) {
			t.Fatalf("orphan finality reached receipt interpretation: blocks=%d events=%d commitments=%d", fixture.receipt.blocks, fixture.receipt.eventReads, fixture.receipt.commitmentReads)
		}
		after, encodeErr := json.Marshal(fixture.source.intent.Prepared)
		if encodeErr != nil || !bytes.Equal(before, after) || fixture.receipt.submissions != 0 || fixture.receipt.subscriptions != 0 {
			t.Fatalf("%s changed or submitted original signed source: %v", fault, encodeErr)
		}
	}
}

// Failed reads retain their transport identity and recover on the same original
// signed receipt. No missing response supplies a fork, absence or replacement.
func TestReleaseSourceFinalityClosingTimeoutRetainsOriginalReceipt(t *testing.T) {
	for _, fault := range []string{"head timeout", "header timeout", "witness timeout", "receipt timeout"} {
		fixture := newReleaseSourceFinalityWitnessFixture(t, fault)
		fixture.failure = context.DeadlineExceeded
		before, err := json.Marshal(fixture.source.intent.Prepared)
		if err != nil {
			t.Fatal(err)
		}
		err = fixture.verify(t.Context())
		if !fixture.injected || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) || strings.Contains(err.Error(), "regressed") || strings.Contains(err.Error(), "not the canonical") {
			t.Fatalf("%s manufactured contradictory evidence: %v", fault, err)
		}
		fixture.fault = "advanced"
		if err := fixture.verify(t.Context()); err != nil {
			t.Fatalf("%s failed to recover original source under advancing finality: %v", fault, err)
		}
		after, encodeErr := json.Marshal(fixture.source.intent.Prepared)
		if encodeErr != nil || !bytes.Equal(before, after) || fixture.receipt.blocks != 2 || fixture.receipt.eventReads != 2 || fixture.receipt.commitmentReads != 4 || fixture.receipt.submissions != 0 || fixture.receipt.subscriptions != 0 {
			t.Fatalf("%s recovery replaced/repeated signed work: blocks=%d events=%d commitments=%d error=%v", fault, fixture.receipt.blocks, fixture.receipt.eventReads, fixture.receipt.commitmentReads, encodeErr)
		}
	}
}

// All source witness, body and state reads share one finite deadline; an
// existing caller deadline remains the sole earlier boundary.
func TestReleaseSourceFinalityReadSharesOriginalDeadline(t *testing.T) {
	for _, shorter := range []bool{false, true} {
		fixture := newReleaseSourceFinalityReadFixture(t)
		ctx := t.Context()
		var expected time.Time
		if shorter {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 37*time.Second)
			defer cancel()
			expected, _ = ctx.Deadline()
		}
		latest := time.Now().Add(300 * time.Second)
		fixture.fault = func(readCtx context.Context, _ any, _ string, _ ...any) (bool, error) {
			deadline, present := readCtx.Deadline()
			if !present || deadline.After(latest.Add(time.Second)) {
				t.Fatal("source receipt read lost its finite operation deadline")
			}
			if expected.IsZero() {
				expected = deadline
			}
			if deadline != expected {
				t.Fatal("source receipt read renewed its original deadline")
			}
			return false, nil
		}
		if err := fixture.verify(ctx); err != nil {
			t.Fatal(err)
		}
		if expected.IsZero() {
			t.Fatal("source receipt deadline control performed no reads")
		}
	}
}

func TestReleaseSourceLowerFinalityChecksOriginalCanonicalHashes(t *testing.T) {
	for _, fault := range []string{"opening lower", "opening lower changed receipt", "regressed", "regressed changed opening", "regressed changed receipt"} {
		fixture := newReleaseSourceFinalityWitnessFixture(t, fault)
		err := fixture.verify(t.Context())
		pending := fault == "opening lower" || fault == "regressed"
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if err == nil || errors.As(err, &unavailable) != pending || retryableProductionSteeringRead(err) != pending {
			t.Fatalf("%s hid its completed canonical evidence: %v", fault, err)
		}
		if strings.HasPrefix(fault, "opening lower") && fixture.receipt.blocks != 0 {
			t.Fatal("unfinalized opening receipt reached its body")
		}
	}
}

func TestReleaseSourceFinalityOwnerRetainsSustainedLowerHead(t *testing.T) {
	fixture := newReleaseSourceFinalityWitnessFixture(t, "regressed")
	parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	ctx := crv4.WithFinalityReadOwnerContext(parent)
	for attempt := 0; attempt < 2; attempt++ {
		err := fixture.verify(ctx)
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if !errors.As(err, &unavailable) || !retryableProductionSteeringRead(err) || unavailable.BlockHash != fixture.opening {
			t.Fatalf("attempt%d forgot original source finality: %v", attempt, err)
		}
	}
	if fixture.receipt.blocks != 1 {
		t.Fatal("second lower opening reset the retained owner and reread the body")
	}
	fixture.fault = "advanced"
	if err := fixture.verify(ctx); err != nil || fixture.receipt.blocks != 2 || fixture.receipt.submissions != 0 {
		t.Fatalf("advancing finality did not recover the original signed receipt: %v", err)
	}
}

func TestReleaseSourceFinalityOwnerCompletedForkDominatesLateBoundary(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, fork := range []bool{false, true} {
			fixture := newReleaseSourceFinalityWitnessFixture(t, "unchanged")
			parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			late := &runtimeFinalityLateContext{Context: parent, done: make(chan struct{}), cause: cause}
			expired := false
			defer func() {
				if !expired {
					close(late.done)
				}
			}()
			ctx := crv4.WithFinalityReadOwnerContext(late)
			if err := fixture.verify(ctx); err != nil {
				t.Fatal(err)
			}
			if fork {
				fixture.openingHeader, fixture.opening = releaseReceiptTestHeader(t, types.Hash{99}, fixture.receipt.number+1)
			}
			fixture.releaseSourceFinalityReadFixture.fault = func(ctx context.Context, target any, method string, args ...any) (bool, error) {
				if err := ctx.Err(); err != nil {
					return true, err
				}
				handled, err := fixture.read(ctx, target, method, args...)
				if method == "chain_getBlockHash" && args[0] == fixture.receipt.number+1 && err == nil {
					expired = true
					close(late.done)
					<-ctx.Done()
				}
				return handled, err
			}
			err := fixture.verify(ctx)
			wantRetry := !fork && errors.Is(cause, context.DeadlineExceeded)
			if !expired || !errors.Is(err, cause) || strings.Contains(err.Error(), "same height") != fork || retryableProductionSteeringRead(err) != wantRetry || fixture.receipt.blocks != 1 {
				t.Fatalf("fork=%t cause=%v lost its completed source evidence: %v", fork, cause, err)
			}
		}
	}
}
