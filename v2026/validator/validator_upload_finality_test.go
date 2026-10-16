//go:build linux || darwin

// Upload freshness uses genuine signed runtime authority and raw timestamp
// replies. Explicit response boundaries force head changes without timing races.
package validator

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Each synchronous fixture owns its wire script, context and transport counter.
type validatorUploadFinalityTestFixture struct {
	production   *validatorUploadProductionTestFixture
	owner        *ValidatorUploadAdmission
	ctx          context.Context
	client       *productionTransportTestClient
	timestampKey string
	timestamps   []types.Hash
	networkReads int
	headReads    int
	before       func(context.Context, any, string, ...any) (bool, error)
	after        func(string, ...any)
}

// The actual metadata decoder builds Timestamp.Now, and the public reader
// still authenticates each runtime artifact and decodes the returned SCALE u64.
func newValidatorUploadFinalityTestFixture(t *testing.T) *validatorUploadFinalityTestFixture {
	t.Helper()
	production := newValidatorUploadProductionTestFixture(t)
	parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	self := &validatorUploadFinalityTestFixture{production: production, owner: production.owner(t), ctx: withRuntimeFinalityOwner(parent)}
	metadata, _, err := crv4.DecodeRuntimeMetadata(production.production.rpc.metadata)
	if err != nil {
		t.Fatal(err)
	}
	key, err := types.CreateStorageKey(metadata, "Timestamp", "Now")
	if err != nil {
		t.Fatal(err)
	}
	self.timestampKey = key.Hex()
	native := production.production.rpc.native
	original := native.API.Client.(*validatorRuntimeIdentityTestClient)
	call := original.callContext
	self.client = &productionTransportTestClient{Client: original}
	self.client.generation.Store(1)
	native.API.Client = self.client
	original.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		deadline, finite := ctx.Deadline()
		parentDeadline, _ := self.ctx.Deadline()
		if !finite || deadline.After(parentDeadline) {
			return errors.New("upload finality read replaced its original finite owner")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if method == "system_chain" {
			self.networkReads++
		}
		if method == "chain_getFinalizedHead" {
			self.headReads++
		}
		if self.before != nil {
			if handled, err := self.before(ctx, result, method, args...); handled || err != nil {
				return err
			}
		}
		var err error
		if method == "state_getStorage" && len(args) == 2 && args[0] == self.timestampKey {
			hash, parseErr := types.NewHashFromHexString(fmt.Sprint(args[1]))
			if parseErr != nil {
				return parseErr
			}
			if _, parseErr := mainnetRuntimeTestNumber(hash); parseErr != nil {
				return parseErr
			}
			self.timestamps = append(self.timestamps, hash)
			err = setReleaseHistoricalTestResult(result, hexutil.Encode(binary.LittleEndian.AppendUint64(nil, production.upload.currentMillis)))
		} else {
			err = call(ctx, result, method, args...)
		}
		if err == nil && self.after != nil {
			self.after(method, args...)
		}
		return err
	}
	return self
}

// Every invocation enters the exported observer with the same parent owner.
func (self *validatorUploadFinalityTestFixture) read() (ValidatorUploadNativeObserver, error) {
	return ValidatorUploadNativeObserverContext(self.ctx, self.production.production.rpc.native, self.owner.config.Deployment)
}

// A changed transport forces replay; a lower replacement stays pending without
// selecting149. Recovery may advance to151 while the returned block remains150.
func TestValidatorUploadFinalityReconnectLowerHeadRecoversOriginalSelection(t *testing.T) {
	fixture := newValidatorUploadFinalityTestFixture(t)
	replaced := false
	fixture.after = func(method string, args ...any) {
		if !replaced && len(fixture.timestamps) == 1 && method == "chain_getBlockHash" && args[0] == uint64(150) {
			replaced = true
			fixture.client.generation.Add(1)
			fixture.production.production.rpc.head = 149
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		got, err := fixture.read()
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if !replaced || got != (ValidatorUploadNativeObserver{}) || !errors.As(err, &unavailable) || !retryableProductionSteeringRead(err) || len(fixture.timestamps) != 1 || fixture.networkReads != attempt+2 {
			t.Fatalf("lower reconnect attempt%d lost original selection or pending cause: %+v %v", attempt, got, err)
		}
	}
	fixture.production.production.rpc.head = 151
	got, err := fixture.read()
	if err != nil || got.Hash != mainnetRuntimeTestBlock(150) || got.Number != 150 || got.TimestampMillis != fixture.production.upload.currentMillis || len(fixture.timestamps) != 2 || fixture.timestamps[1] != got.Hash {
		t.Fatalf("reconnect recovery selected a replacement head: %+v %v", got, err)
	}
}

// Closing finality is observed after timestamp bytes, even without reconnect.
func TestValidatorUploadFinalityClosingLowerHeadRemainsPending(t *testing.T) {
	fixture := newValidatorUploadFinalityTestFixture(t)
	fixture.after = func(method string, _ ...any) {
		if method == "state_getStorage" {
			fixture.production.production.rpc.head = 149
		}
	}
	got, err := fixture.read()
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	if got != (ValidatorUploadNativeObserver{}) || !errors.As(err, &unavailable) || !retryableProductionSteeringRead(err) || len(fixture.timestamps) != 1 || fixture.headReads != 2 {
		t.Fatalf("closing lower head published freshness or became a fork: %+v %v", got, err)
	}
}

// Two authentic finalized headers at one height are already contradictory,
// even if a switching provider could separately report both as canonical.
func TestValidatorUploadFinalitySameHeightForkRemainsHard(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		fixture := newValidatorUploadFinalityTestFixture(t)
		if repeated {
			if _, err := fixture.read(); err != nil {
				t.Fatal(err)
			}
		}
		header, replacement := releaseReceiptTestHeader(t, types.Hash{0xa7}, 150)
		headerReads := 0
		fixture.before = func(_ context.Context, result any, method string, args ...any) (bool, error) {
			if len(fixture.timestamps) == 0 {
				return false, nil
			}
			if method == "chain_getFinalizedHead" {
				return true, setReleaseHistoricalTestResult(result, replacement.Hex())
			}
			if method == "chain_getHeader" && args[0] == replacement.Hex() {
				headerReads++
				return true, setReleaseHistoricalTestResult(result, releaseReceiptTestHeaderWire(header))
			}
			return false, nil
		}
		got, err := fixture.read()
		if got != (ValidatorUploadNativeObserver{}) || err == nil || retryableProductionSteeringRead(err) || headerReads != 1 || !strings.Contains(err.Error(), "same height") || len(fixture.timestamps) != 1 {
			t.Fatalf("same-height fork gained freshness repeated=%t: %+v %v", repeated, got, err)
		}
	}
}

// A lower current head cannot hide actual changed selected or retained hashes.
func TestValidatorUploadFinalityLowerHeadPreservesCanonicalConflict(t *testing.T) {
	fixture := newValidatorUploadFinalityTestFixture(t)
	fixture.after = func(method string, _ ...any) {
		if method == "state_getStorage" {
			fixture.production.production.rpc.head = 149
		}
	}
	changed := false
	fixture.before = func(_ context.Context, result any, method string, args ...any) (bool, error) {
		if len(fixture.timestamps) == 1 && method == "chain_getBlockHash" && args[0] == uint64(150) {
			changed = true
			return true, setReleaseHistoricalTestResult(result, types.Hash{0xa8}.Hex())
		}
		return false, nil
	}
	got, err := fixture.read()
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	if !changed || got != (ValidatorUploadNativeObserver{}) || err == nil || errors.As(err, &unavailable) || retryableProductionSteeringRead(err) || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("lower head concealed selected canonical contradiction: %+v %v", got, err)
	}
}

// Null/empty heads remain unavailable; malformed/zero hashes never gain authority.
func TestValidatorUploadFinalitySeparatesMissingAndInvalidHeads(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, response := range []any{nil, "", "0x01", types.Hash{}.Hex()} {
			fixture := newValidatorUploadFinalityTestFixture(t)
			injected := false
			fixture.before = func(_ context.Context, result any, method string, _ ...any) (bool, error) {
				if method == "chain_getFinalizedHead" && (!closing || len(fixture.timestamps) != 0) {
					injected = true
					return true, setReleaseHistoricalTestResult(result, response)
				}
				return false, nil
			}
			got, err := fixture.read()
			var unavailable *crv4.ReceiptEvidenceUnavailableError
			missing := response == nil || response == ""
			if !injected || got != (ValidatorUploadNativeObserver{}) || err == nil || errors.As(err, &unavailable) != missing || retryableProductionSteeringRead(err) != missing {
				t.Fatalf("head reply %v changed authority or cause closing=%t: %+v %v", response, closing, got, err)
			}
		}
	}
}

// An error without returned bytes is never a finality mismatch. A joined hard
// cause cannot acquire retry authority from its neighboring deadline.
func TestValidatorUploadFinalityClosingTimeoutPreservesCauseAndNoResult(t *testing.T) {
	for _, method := range []string{"chain_getFinalizedHead", "chain_getHeader", "chain_getBlockHash"} {
		for _, mixed := range []bool{false, true} {
			fixture := newValidatorUploadFinalityTestFixture(t)
			injected := false
			hard := errors.New("synthetic local read refusal")
			fixture.before = func(_ context.Context, _ any, actual string, _ ...any) (bool, error) {
				if !injected && len(fixture.timestamps) == 1 && actual == method {
					injected = true
					if mixed {
						return true, errors.Join(context.DeadlineExceeded, hard)
					}
					return true, context.DeadlineExceeded
				}
				return false, nil
			}
			got, err := fixture.read()
			if !injected || got != (ValidatorUploadNativeObserver{}) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, hard) != mixed || retryableProductionSteeringRead(err) == mixed || strings.Contains(err.Error(), "canonical") || strings.Contains(err.Error(), "regressed") {
				t.Fatalf("%s timeout mixed=%t lost a cause or published partial freshness: %+v %v", method, mixed, got, err)
			}
			got, err = fixture.read()
			if err != nil || got.Hash != mainnetRuntimeTestBlock(150) || len(fixture.timestamps) != 2 {
				t.Fatalf("%s timeout recovery moved original selection: %+v %v", method, got, err)
			}
		}
	}
}

// Actual production retries share the original300/shorter45-second owner and
// preserve selected150 through closing149, sustained149, then recovered151.
func TestValidatorUploadFinalityProductionRetryRetainsOriginalSelection(t *testing.T) {
	fixture := newValidatorUploadFinalityTestFixture(t)
	steerer := &ReleaseSteerer{cfg: fixture.production.production.cfg}
	attempts, waits := 0, 0
	fixture.after = func(method string, _ ...any) {
		if attempts == 1 && method == "state_getStorage" {
			fixture.production.production.rpc.head = 149
		}
	}
	steerer.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits == 2 {
			fixture.production.production.rpc.head = 151
		}
		if waits > 2 {
			return errors.New("upload finality exceeded its deterministic retry census")
		}
		return ctx.Err()
	}
	var observed ValidatorUploadNativeObserver
	err := steerer.productionRead(fixture.ctx, productionReadPreparation, nil, func(ctx context.Context) error {
		attempts++
		var err error
		observed, err = ValidatorUploadNativeObserverContext(ctx, fixture.production.production.rpc.native, fixture.owner.config.Deployment)
		if attempts < 3 && (observed != (ValidatorUploadNativeObserver{}) || !retryableProductionSteeringRead(err)) {
			t.Fatalf("retry%d replaced the original unavailable selection: %+v %v", attempts, observed, err)
		}
		return err
	})
	if err != nil || attempts != 3 || waits != 2 || observed.Number != 150 || observed.Hash != mainnetRuntimeTestBlock(150) || len(fixture.timestamps) != 2 {
		t.Fatalf("production retry lost original block/deadline: attempts=%d waits=%d result=%+v error=%v", attempts, waits, observed, err)
	}
	for _, hash := range fixture.timestamps {
		if hash != observed.Hash {
			t.Fatal("production retry read Timestamp.Now at a replacement block")
		}
	}
}

// Canonical conflicts remain hard at the exact callback boundary where a
// deadline/cancel arrives; matching bytes still cannot publish late success.
func TestValidatorUploadFinalityCompletedConflictDominatesLateContext(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, changed := range []bool{false, true} {
			fixture := newValidatorUploadFinalityTestFixture(t)
			late := &runtimeFinalityLateContext{Context: fixture.ctx, done: make(chan struct{}), cause: cause}
			fixture.ctx = late
			completed := false
			t.Cleanup(func() {
				if !completed {
					close(late.done)
				}
			})
			fixture.before = func(ctx context.Context, result any, method string, args ...any) (bool, error) {
				if len(fixture.timestamps) == 1 && method == "chain_getBlockHash" && args[0] == uint64(150) {
					hash := mainnetRuntimeTestBlock(150)
					if changed {
						hash = types.Hash{0xa9}
					}
					if err := setReleaseHistoricalTestResult(result, hash.Hex()); err != nil {
						return true, err
					}
					completed = true
					close(late.done)
					<-ctx.Done()
					return true, nil
				}
				return false, nil
			}
			got, err := fixture.read()
			if !completed || got != (ValidatorUploadNativeObserver{}) || !errors.Is(err, cause) || strings.Contains(err.Error(), "not canonical") != changed || retryableProductionSteeringRead(err) != (!changed && cause == context.DeadlineExceeded) {
				t.Fatalf("completed changed=%t canonical bytes lost cause%v: %+v %v", changed, cause, got, err)
			}
		}
	}
}

// Fresh identity remains a prerequisite; a completed mismatch dominates its
// simultaneous deadline and prevents all finalized-head or timestamp reads.
func TestValidatorUploadFinalityPreservesCompletedIdentityHardCause(t *testing.T) {
	fixture := newValidatorUploadFinalityTestFixture(t)
	late := &runtimeFinalityLateContext{Context: fixture.ctx, done: make(chan struct{}), cause: context.DeadlineExceeded}
	fixture.ctx = late
	completed := false
	t.Cleanup(func() {
		if !completed {
			close(late.done)
		}
	})
	fixture.before = func(ctx context.Context, result any, method string, _ ...any) (bool, error) {
		if method == "system_chain" {
			if err := setReleaseHistoricalTestResult(result, "synthetic-other-chain"); err != nil {
				return true, err
			}
			completed = true
			close(late.done)
			<-ctx.Done()
			return true, nil
		}
		return false, nil
	}
	got, err := fixture.read()
	if !completed || got != (ValidatorUploadNativeObserver{}) || !errors.Is(err, context.DeadlineExceeded) || retryableProductionSteeringRead(err) || !strings.Contains(err.Error(), "native chain name differs") || fixture.headReads != 0 || len(fixture.timestamps) != 0 {
		t.Fatalf("completed identity contradiction became unavailable freshness: %+v %v", got, err)
	}
}

// Closing advancement retains the earlier timestamp and does not bind signing state.
func TestValidatorUploadFinalityAcceptsStableAndAdvancedClosingHeads(t *testing.T) {
	for _, number := range []uint64{150, 151} {
		fixture := newValidatorUploadFinalityTestFixture(t)
		native := fixture.production.production.rpc.native
		metadata, runtime := native.Meta, native.Runtime
		fixture.after = func(method string, _ ...any) {
			if method == "state_getStorage" {
				fixture.production.production.rpc.head = number
			}
		}
		got, err := fixture.read()
		if err != nil || got.Hash != mainnetRuntimeTestBlock(150) || got.Number != 150 || got.TimestampMillis != fixture.production.upload.currentMillis || fixture.headReads != 2 || fixture.networkReads != 1 || native.Meta != metadata || native.Runtime != runtime {
			t.Fatalf("valid closing%d changed selected freshness or signing state: %+v %v", number, got, err)
		}
	}
}

// A later invocation cannot discard an already authenticated higher closing head.
func TestValidatorUploadFinalityRetainsHighestClosingWitness(t *testing.T) {
	fixture := newValidatorUploadFinalityTestFixture(t)
	fixture.after = func(method string, _ ...any) {
		if method == "state_getStorage" {
			fixture.production.production.rpc.head = 151
		}
	}
	if _, err := fixture.read(); err != nil {
		t.Fatal(err)
	}
	fixture.after = nil
	fixture.production.production.rpc.head = 150
	got, err := fixture.read()
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	if got != (ValidatorUploadNativeObserver{}) || !errors.As(err, &unavailable) || !retryableProductionSteeringRead(err) || len(fixture.timestamps) != 1 {
		t.Fatalf("public retry discarded prior closing151 witness: %+v %v", got, err)
	}
	fixture.production.production.rpc.head = 151
	got, err = fixture.read()
	if err != nil || got.Hash != mainnetRuntimeTestBlock(150) || len(fixture.timestamps) != 2 {
		t.Fatalf("higher witness recovery changed selected timestamp: %+v %v", got, err)
	}
}
