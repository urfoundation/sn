//go:build linux || darwin

// Explicit deadline transitions keep cancellation pending while the real
// steering read owner admits attempts, classifies outcomes and retains phase.
package validator

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Expired callers cannot create a budget, and expiry while a child is being
// created cannot enter a read. A live owner may pace an expired attempt only.
func TestProductionSteeringReadRefusesElapsedDeadlineBeforeAdmission(t *testing.T) {
	for _, scenario := range []struct {
		boundary string
		phase    productionSteeringReadPhase
		budgets  int
		waits    int
	}{
		{boundary: "caller", phase: productionReadIntent, budgets: 0, waits: 0},
		{boundary: "operation", phase: productionReadPreparation, budgets: 1, waits: 0},
		{boundary: "attempt", phase: productionReadReceipt, budgets: 2, waits: 1},
		{boundary: "caller during attempt", phase: productionReadApplication, budgets: 2, waits: 0},
		{boundary: "operation during attempt", phase: productionReadReceipt, budgets: 2, waits: 0},
	} {
		caller := &evidenceReadPendingDeadlineTestContext{Context: t.Context(), deadline: time.Now().Add(time.Hour)}
		if scenario.boundary == "caller" {
			caller.deadline = time.Unix(1, 0)
		}
		self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
		var operation *evidenceReadPendingDeadlineTestContext
		budgets, closed, reads, waits := 0, 0, 0, 0
		self.productionReadHooks.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			budgets++
			deadline := time.Now().Add(duration)
			if parentDeadline, found := parent.Deadline(); found && parentDeadline.Before(deadline) {
				deadline = parentDeadline
			}
			child := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: deadline}
			if operation == nil {
				operation = child
				if scenario.boundary == "operation" {
					operation.deadline = time.Unix(1, 0)
				}
			} else {
				switch scenario.boundary {
				case "attempt":
					child.deadline = time.Unix(1, 0)
				case "caller during attempt":
					caller.deadline = time.Unix(1, 0)
				case "operation during attempt":
					operation.deadline = time.Unix(1, 0)
				}
			}
			return child, func() { closed++ }
		}
		self.productionReadHooks.wait = func(context.Context, time.Duration) error {
			waits++
			operation.deadline = time.Unix(1, 0)
			return nil
		}
		intent := &SteeringIntent{SubnetEpoch: 23, Prepared: &crv4.PreparedSubmission{ExtrinsicHash: "synthetic-retained-steering"}}
		err := self.productionRead(caller, scenario.phase, intent, func(context.Context) error {
			reads++
			return nil
		})
		var wait *productionSteeringReadWait
		if caller.Err() != nil || operation != nil && operation.Err() != nil || reads != 0 || waits != scenario.waits || budgets != scenario.budgets || closed != budgets ||
			!errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &wait) || wait.phase != scenario.phase || !wait.epochKnown || wait.nativeEpoch != intent.SubnetEpoch || wait.extrinsicHash != intent.Prepared.ExtrinsicHash || !retryableProductionSteeringRead(err) {
			t.Fatalf("%s admitted elapsed work or changed its retained phase: reads=%d waits=%d budgets=%d closed=%d error=%v", scenario.boundary, reads, waits, budgets, closed, err)
		}
	}
}

// An owner that expires during a response cannot accept success or pace a
// retry. Expiry after pacing keeps the original cause without another attempt.
func TestProductionSteeringReadPreservesElapsedDeadlineOutcome(t *testing.T) {
	unavailable := &crv4.ReceiptEvidenceUnavailableError{Field: "synthetic retained block body"}
	hard := errors.New("synthetic retained commitment mismatch")
	for _, scope := range []string{"caller", "operation"} {
		for _, scenario := range []struct {
			name      string
			afterWait bool
			readErr   error
			waitErr   error
			hardErr   error
		}{
			{name: "success"},
			{name: "unavailable", readErr: unavailable},
			{name: "mixed hard", readErr: errors.Join(unavailable, hard), hardErr: hard},
			{name: "mixed cancellation", readErr: errors.Join(unavailable, context.Canceled), hardErr: context.Canceled},
			{name: "paced", afterWait: true, readErr: unavailable},
			{name: "paced hard", afterWait: true, readErr: unavailable, waitErr: hard, hardErr: hard},
			{name: "paced cancellation", afterWait: true, readErr: unavailable, waitErr: context.Canceled, hardErr: context.Canceled},
		} {
			caller := &evidenceReadPendingDeadlineTestContext{Context: t.Context(), deadline: time.Now().Add(time.Hour)}
			self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
			var operation *evidenceReadPendingDeadlineTestContext
			budgets, closed, reads, waits := 0, 0, 0, 0
			self.productionReadHooks.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
				budgets++
				deadline := time.Now().Add(duration)
				if parentDeadline, found := parent.Deadline(); found && parentDeadline.Before(deadline) {
					deadline = parentDeadline
				}
				child := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: deadline}
				if operation == nil {
					operation = child
				}
				return child, func() { closed++ }
			}
			expire := func() {
				if scope == "caller" {
					caller.deadline = time.Unix(1, 0)
				} else {
					operation.deadline = time.Unix(1, 0)
				}
			}
			self.productionReadHooks.wait = func(context.Context, time.Duration) error {
				waits++
				expire()
				return scenario.waitErr
			}
			intent := &SteeringIntent{SubnetEpoch: 23, Prepared: &crv4.PreparedSubmission{ExtrinsicHash: "synthetic-retained-steering"}}
			err := self.productionRead(caller, productionReadApplication, intent, func(context.Context) error {
				reads++
				if reads > 1 {
					t.Fatal("elapsed steering owner acquired another attempt")
				}
				if !scenario.afterWait {
					expire()
				}
				return scenario.readErr
			})
			wantWaits := 0
			if scenario.afterWait {
				wantWaits = 1
			}
			var wait *productionSteeringReadWait
			waiting := errors.As(err, &wait)
			if caller.Err() != nil || operation.Err() != nil || reads != 1 || waits != wantWaits || budgets != 2 || closed != budgets ||
				!errors.Is(err, context.DeadlineExceeded) || waiting != (scenario.hardErr == nil) || retryableProductionSteeringRead(err) != waiting ||
				scenario.readErr != nil && !errors.Is(err, unavailable) || scenario.hardErr != nil && !errors.Is(err, scenario.hardErr) {
				t.Fatalf("%s %s lost its original deadline or cause: reads=%d waits=%d budgets=%d closed=%d error=%v", scope, scenario.name, reads, waits, budgets, closed, err)
			}
			if waiting && (wait.phase != productionReadApplication || !wait.epochKnown || wait.nativeEpoch != intent.SubnetEpoch || wait.extrinsicHash != intent.Prepared.ExtrinsicHash) {
				t.Fatalf("%s %s changed its retained phase: %v", scope, scenario.name, err)
			}
		}
	}
}

// Only the elapsed attempt is replaceable. Both admission and response expiry
// retry within the same live operation, closing the first attempt before pacing.
func TestProductionSteeringReadRetriesElapsedAttemptWithinOriginalOwner(t *testing.T) {
	for _, boundary := range []string{"admission", "response"} {
		self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
		var operation, attempt *evidenceReadPendingDeadlineTestContext
		var durations []time.Duration
		attempts, closed, reads, waits := 0, 0, 0, 0
		self.productionReadHooks.withTimeout = func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
			durations = append(durations, duration)
			deadline := time.Now().Add(duration)
			if parentDeadline, found := parent.Deadline(); found && parentDeadline.Before(deadline) {
				deadline = parentDeadline
			}
			child := &evidenceReadPendingDeadlineTestContext{Context: parent, deadline: deadline}
			if operation == nil {
				operation = child
			} else {
				attempts++
				attempt = child
				if boundary == "admission" && attempts == 1 {
					attempt.deadline = time.Unix(1, 0)
				}
			}
			return child, func() { closed++ }
		}
		self.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			deadline, found := ctx.Deadline()
			if waits != 1 || closed != 1 || !found || deadline != operation.deadline || evidenceReadContextError(ctx) != nil || attempt.Err() != nil {
				t.Fatal("attempt expiry lost its live original owner or attempt close boundary")
			}
			return nil
		}
		err := self.productionRead(t.Context(), productionReadReceipt, nil, func(context.Context) error {
			reads++
			if boundary == "response" && attempts == 1 {
				attempt.deadline = time.Unix(1, 0)
			}
			return nil
		})
		wantReads := 1
		if boundary == "response" {
			wantReads = 2
		}
		if err != nil || operation.Err() != nil || reads != wantReads || waits != 1 || attempts != 2 || closed != 3 ||
			!slices.Equal(durations, []time.Duration{productionSteeringReadTimeout, productionSteeringReadAttemptTimeout, productionSteeringReadAttemptTimeout}) {
			t.Fatalf("%s expiry renewed the operation or accepted a late response: reads=%d waits=%d attempts=%d closed=%d budgets=%v error=%v", boundary, reads, waits, attempts, closed, durations, err)
		}
	}
}
