// The public observation entrypoints must pass their complete read allowance
// to actual runtime RPC calls while retaining a shorter caller deadline.
package validator

import (
	"context"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Reads are synchronous in these approved transcript fixtures. Bounds use the
// real context deadline interval, without sleeping or substituting a verdict.
type runtimeReadBudgetRecorder struct {
	t              *testing.T
	started        time.Time
	parentDeadline time.Time
	deadline       time.Time
	calls          int
}

// Parent limits are configured before the synchronous observation begins.
func newRuntimeReadBudgetRecorder(t *testing.T, parentLimit time.Duration) (context.Context, *runtimeReadBudgetRecorder, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	if parentLimit != 0 {
		cancel()
		ctx, cancel = context.WithTimeout(t.Context(), parentLimit)
	}
	deadline, _ := ctx.Deadline()
	return ctx, &runtimeReadBudgetRecorder{t: t, started: time.Now(), parentDeadline: deadline}, cancel
}

// Record the context which reaches the existing raw native call boundary.
func (self *runtimeReadBudgetRecorder) observe(ctx context.Context) {
	self.t.Helper()
	deadline, found := ctx.Deadline()
	if !found {
		self.t.Fatal("runtime observation lost its finite deadline")
	}
	if !self.parentDeadline.IsZero() {
		if deadline != self.parentDeadline {
			self.t.Fatal("runtime observation changed its shorter caller deadline")
		}
	} else if deadline.Before(self.started.Add(300*time.Second)) || deadline.After(time.Now().Add(300*time.Second)) {
		self.t.Fatal("runtime observation did not receive the complete 300-second read budget")
	}
	if self.calls != 0 && deadline != self.deadline {
		self.t.Fatal("nested runtime read renewed its original operation deadline")
	}
	self.deadline = deadline
	self.calls++
}

// A configuration-only refusal cannot satisfy a read-budget regression.
func (self *runtimeReadBudgetRecorder) completed(err error) {
	self.t.Helper()
	if err != nil || self.calls == 0 {
		self.t.Fatalf("approved runtime observation did not complete its actual reads: calls=%d error=%v", self.calls, err)
	}
}

// Current finalized selection still returns the original approved block.
func TestMainnetRuntimeReadBudgetReachesOriginalRpc(t *testing.T) {
	for _, parentLimit := range []time.Duration{0, 45 * time.Second} {
		f := newMainnetRuntimeTestFixture(t)
		cfg, err := LoadMainnetRuntimeObservationConfig(f.path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, parentLimit)
		client := f.native.API.Client.(*validatorRuntimeIdentityTestClient)
		call := client.callContext
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			recorder.observe(ctx)
			return call(ctx, target, method, args...)
		}
		result, err := ObserveMainnetRuntimeAtContext(ctx, f.native, cfg, types.Hash{})
		cancel()
		recorder.completed(err)
		if result == nil || result.BlockHash != mainnetRuntimeTestBlock(150) {
			t.Fatal("read budget changed the original approved runtime block")
		}
	}
}

// Artifact inspection and owned view binding share the same read envelope.
func TestProductionRuntimeReadBudgetReachesOriginalRpc(t *testing.T) {
	for _, parentLimit := range []time.Duration{0, 45 * time.Second} {
		for _, bindView := range []bool{false, true} {
			f := newProductionRuntimeTestFixture(t, false)
			ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, parentLimit)
			client := f.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
			call := client.callContext
			client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
				recorder.observe(ctx)
				return call(ctx, target, method, args...)
			}
			var err error
			if bindView {
				err = authenticateOwnerRecycleProductionRuntimeAtContext(ctx, f.rpc.native, f.cfg, mainnetRuntimeTestBlock(150))
			} else {
				_, _, err = authenticateOwnerRecycleProductionArtifactAtContext(ctx, f.rpc.native, f.cfg, mainnetRuntimeTestBlock(150), false)
			}
			cancel()
			recorder.completed(err)
		}
	}
}

// Retention finishes first; only the following original census read is timed.
func TestRecycleAdmissionReadBudgetReachesOriginalRpc(t *testing.T) {
	for _, parentLimit := range []time.Duration{0, 45 * time.Second} {
		f := newRecycleAdmissionFixture(t, nil)
		f.retain(t)
		ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, parentLimit)
		f.before = func(ctx context.Context, _ string, _ []any) error {
			recorder.observe(ctx)
			return nil
		}
		result, err := ObserveOwnerRecycleAdmission(ctx, f.cfg, f.chain)
		cancel()
		recorder.completed(err)
		if result == nil || result.Snapshot.FinalizedHash != [32]byte(f.finalized) {
			t.Fatal("read budget changed the original owner census block")
		}
	}
}

// The candidate remains an observation, with no durable production selection.
func TestProductionContinuityReadBudgetReachesOriginalRpc(t *testing.T) {
	for _, parentLimit := range []time.Duration{0, 45 * time.Second} {
		f := newProductionContinuityPolicyTestFixture(t)
		ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, parentLimit)
		f.fault = func(ctx context.Context, _ any, _ string, _ ...any) (bool, error) {
			recorder.observe(ctx)
			return false, nil
		}
		result, err := InspectProductionRuntimeContinuityContext(ctx, f.owner.rpc.native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
		cancel()
		recorder.completed(err)
		if result == nil || result.NativeHash != f.hashes[150] || result.ProductionSelectionInstalled || result.SigningAuthority {
			t.Fatal("read budget changed the original continuity block or granted signing")
		}
	}
}
