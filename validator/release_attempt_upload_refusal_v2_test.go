//go:build linux || darwin

// A reserved upload refused by one API instance is not refused by the deployment.
// Real writers and signers run over a deterministic transport, with no clock.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol"
)

// The refusal an api process with its reserved lane off answered until 2026-11.
const releaseUploadLaneOffDetail = "Validator reserved upload is unavailable or ambiguous."

// A reserved upload owner over a fake lb. Each request goes to the next of the
// transport's instances, as least_conn does when they are idle, and answers what
// that instance's lane would.
type releaseUploadRefusalFixture struct {
	owner   *releaseAttemptUploadV2
	signer  *validatorAttemptUploadSigner
	lock    sync.Mutex
	answers func(request int) (int, string)
	// requests in arrival order: Authorization and the signed consent
	authorizations []string
	consents       []string
	bodies         [][]byte
	waits          []time.Duration
	credentials    atomic.Int32
}

func newReleaseUploadRefusalFixture(t *testing.T, answers func(request int) (int, string)) *releaseUploadRefusalFixture {
	t.Helper()
	self := &releaseUploadRefusalFixture{answers: answers}
	key := ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...))
	var activation protocol.ValidatorEvidenceActivation
	activation.Domain.ChainID = 945
	activation.Domain.Netuid = 521
	activation.Domain.Epoch = 7
	activation.Domain.GenesisHash = [32]byte{1}
	activation.Domain.Coordinator = [20]byte{2}
	activation.Domain.SettlementVault = [20]byte{3}
	activation.Domain.DeploymentIDHash = sha256.Sum256([]byte("refusal-fixture"))
	activation.Domain.PolicyHash = [32]byte{4}
	activation.Hotkey = [32]byte{5}
	activation.NoID = 1
	activation.FirstSequence = 1
	activation.NativeBlock = 100
	activation.NativeHash = [32]byte{6}
	activation.EVMBlock = 1000
	activation.EVMHash = [32]byte{7}
	copy(activation.VPK[:], key[ed25519.SeedSize:])
	signer, err := newValidatorAttemptUploadSigner(activation, 1, 60, key)
	if err != nil {
		t.Fatal(err)
	}
	self.signer = signer
	owner, err := newReleaseAttemptUploadV2(t.Context(), OperatorConfig{NoID: 1, APIURL: "https://api.example"}, ReleaseEvidenceV2Bounds{Cut: attemptCutV2ReplicaTestBounds(), MaxTransitionBytes: 1024 * 1024}, func() string {
		self.credentials.Add(1)
		return "fixture-session"
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.close)
	owner.writer.client.Transport = attemptStreamV2HTTPTestTransport(self.roundTrip)
	owner.refusalWait = func(ctx context.Context, delay time.Duration) error {
		self.lock.Lock()
		defer self.lock.Unlock()
		self.waits = append(self.waits, delay)
		return ctx.Err()
	}
	self.owner = owner
	return self
}

func (self *releaseUploadRefusalFixture) roundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	self.lock.Lock()
	index := len(self.consents)
	self.authorizations = append(self.authorizations, request.Header.Get("Authorization"))
	self.consents = append(self.consents, request.Header.Get(protocol.ValidatorAttemptUploadHeader))
	self.bodies = append(self.bodies, body)
	self.lock.Unlock()
	status, detail := self.answers(index)
	header := http.Header{}
	if status == http.StatusNoContent {
		header.Set("ETag", `"`+attemptHex32(sha256.Sum256(body))+`"`)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(detail))}, nil
}

func (self *releaseUploadRefusalFixture) write(ctx context.Context, data []byte) error {
	return self.owner.writeWithSigner(ctx, "metadata", attemptHex32(sha256.Sum256(data)), data, self.signer)
}

func (self *releaseUploadRefusalFixture) requests() int {
	self.lock.Lock()
	defer self.lock.Unlock()
	return len(self.consents)
}

// The instance with its lane off refuses once; the next instance takes the same
// object. Each attempt is its own request: the current credential, a fresh
// consent signature and the same exact bytes.
func TestReservedUploadRefusalRetriesElsewhere(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(request int) (int, string) {
		if request == 0 {
			return http.StatusForbidden, releaseUploadLaneOffDetail
		}
		return http.StatusNoContent, ""
	})
	data := []byte("{\"object\":1}\n")
	if err := fixture.write(t.Context(), data); err != nil {
		t.Fatalf("one instance's refusal failed the upload: %v", err)
	}
	if got := fixture.requests(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	if got := fixture.credentials.Load(); got != 2 {
		t.Fatalf("credential reads = %d, want one per attempt", got)
	}
	if len(fixture.waits) != 1 || fixture.waits[0] != releaseReservedUploadRefusalDelay {
		t.Fatalf("waits = %v, want one of %s", fixture.waits, releaseReservedUploadRefusalDelay)
	}
	for index, consent := range fixture.consents {
		if _, err := protocol.VerifyValidatorAttemptUploadHeader(consent); err != nil {
			t.Fatalf("attempt %d carries no valid consent: %v", index, err)
		}
		if fixture.authorizations[index] != "Bearer fixture-session" || !bytes.Equal(fixture.bodies[index], data) {
			t.Fatalf("attempt %d differs: %q, %q", index, fixture.authorizations[index], fixture.bodies[index])
		}
	}
}

// Every instance refusing is a refusal of the deployment, which stays hard: the
// bound is the retry's, and the status reaches the classifiers unchanged.
func TestReservedUploadRefusalEverywhereStaysHard(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) {
		return http.StatusForbidden, "Validator staging authority refused."
	})
	err := fixture.write(t.Context(), []byte("{\"object\":2}\n"))
	var refusal *attemptStreamHttpStatusError
	if !errors.As(err, &refusal) || refusal.status != http.StatusForbidden || !refusal.upload {
		t.Fatalf("persistent refusal lost its status: %v", err)
	}
	if got := fixture.requests(); got != releaseReservedUploadRefusalAttempts {
		t.Fatalf("requests = %d, want the bound %d", got, releaseReservedUploadRefusalAttempts)
	}
	if got := len(fixture.waits); got != releaseReservedUploadRefusalAttempts-1 {
		t.Fatalf("waits = %d, want %d", got, releaseReservedUploadRefusalAttempts-1)
	}
	if transientReleaseSnapshotError(err) || retryableProductionSteeringRead(err) {
		t.Fatalf("a persistent refusal became retryable: %v", err)
	}
}

// Only a 403 says one instance may differ from its siblings, and only on the
// reserved lane. Every other answer keeps its one request and its own classifier.
func TestReservedUploadRefusalRetryIsOnlyForAReservedForbidden(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusRequestEntityTooLarge, http.StatusConflict, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable} {
		fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return status, "synthetic" })
		err := fixture.write(t.Context(), []byte("{\"object\":3}\n"))
		var refusal *attemptStreamHttpStatusError
		if !errors.As(err, &refusal) || refusal.status != status || fixture.requests() != 1 || len(fixture.waits) != 0 {
			t.Fatalf("status %d: requests=%d waits=%d err=%v", status, fixture.requests(), len(fixture.waits), err)
		}
	}
	// the ordinary lane has no reserved admission to differ between instances
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusForbidden, "synthetic" })
	data := []byte("{\"object\":4}\n")
	err := fixture.owner.write(t.Context(), "metadata", attemptHex32(sha256.Sum256(data)), data)
	if err == nil || fixture.requests() != 1 || len(fixture.waits) != 0 {
		t.Fatalf("ordinary lane: requests=%d waits=%d err=%v", fixture.requests(), len(fixture.waits), err)
	}
}

// A retried refusal stops with its owner, joined, with no further request.
func TestReservedUploadRefusalRetryEndsWithItsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusForbidden, releaseUploadLaneOffDetail })
	fixture.owner.refusalWait = func(ctx context.Context, _ time.Duration) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	err := fixture.write(ctx, []byte("{\"object\":5}\n"))
	if !errors.Is(err, context.Canceled) || fixture.requests() != 1 {
		t.Fatalf("requests=%d err=%v", fixture.requests(), err)
	}
}

// A 403 joined to anything else is not one instance's plain refusal. A body that
// could not be closed keeps the whole answer's verdict, as it does for a 429.
func TestReservedUploadRefusalJoinedFailureIsNotRetried(t *testing.T) {
	t.Parallel()
	refusal := &attemptStreamHttpStatusError{status: http.StatusForbidden, upload: true}
	if !releaseUploadForbidden(errors.Join(errors.Join(refusal, nil), nil)) {
		t.Fatal("a plain refusal, in the writer's joins, was not recognized")
	}
	for _, err := range []error{
		nil,
		errors.New("attempt upload response status is 403"),
		&attemptStreamHttpStatusError{status: http.StatusForbidden},
		&attemptStreamHttpStatusError{status: http.StatusConflict, upload: true},
		errors.Join(refusal, errors.New("late close failed")),
		errors.Join(refusal, context.Canceled),
	} {
		if releaseUploadForbidden(err) {
			t.Fatalf("recognized as a plain refusal: %v", err)
		}
	}
}

// The incident's shape. One release publishes many objects and stops at the first
// failure, so with one instance of five refusing, every submission met it and
// each counted toward the ten hard errors that panic the process. The refusal
// retried elsewhere leaves each submission complete, and the budget unspent.
func TestOneRefusingInstanceCannotExhaustTheSteeringFailureBudget(t *testing.T) {
	t.Parallel()
	const instances = 5
	const objectsPerSubmission = 12
	const submissions = 3 * releaseSteeringFailureLimit
	fixture := newReleaseUploadRefusalFixture(t, func(request int) (int, string) {
		if request%instances == 0 {
			return http.StatusForbidden, releaseUploadLaneOffDetail
		}
		return http.StatusNoContent, ""
	})
	data := []byte("{\"object\":6}\n")
	completed := 0
	polls := 0
	err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
		for object := 0; object < objectsPerSubmission; object++ {
			if err := fixture.write(t.Context(), data); err != nil {
				return err
			}
		}
		completed++
		return nil
	}, func() bool {
		polls++
		return polls < submissions
	}, nil, time.Now, releaseProductionCutWaitMinimumLimit)
	var limit *releaseSteeringFailureLimitError
	if errors.As(err, &limit) {
		t.Fatalf("one refusing instance killed the release after %d complete submissions: %v", completed, err)
	}
	if err != nil || completed != submissions {
		t.Fatalf("completed %d of %d submissions: %v", completed, submissions, err)
	}
}

// Every instance refusing still ends the release as before.
func TestARefusingDeploymentStillExhaustsTheSteeringFailureBudget(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) {
		return http.StatusForbidden, "Validator staging authority refused."
	})
	data := []byte("{\"object\":7}\n")
	polls := 0
	err := runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
		return fixture.write(t.Context(), data)
	}, func() bool {
		polls++
		return polls < 3*releaseSteeringFailureLimit
	}, nil, time.Now, releaseProductionCutWaitMinimumLimit)
	var limit *releaseSteeringFailureLimitError
	if !errors.As(err, &limit) || limit.failures != releaseSteeringFailureLimit {
		t.Fatalf("a refusing deployment did not reach the limit: polls=%d err=%v", polls, err)
	}
}

// What a server with the lane retry answers while its lane is off. This validator
// and every release since 2026-09-22 read a 503 as a wait: it costs no failure,
// and the settlement refresh keeps running.
func TestReservedUploadServiceUnavailableIsAWaitNotAFailure(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) {
		return http.StatusServiceUnavailable, "Validator reserved upload is not running on this instance."
	})
	err := fixture.write(t.Context(), []byte("{\"object\":8}\n"))
	if !transientReleaseSnapshotError(err) || !retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
		t.Fatalf("a 503 was not a retryable wait: %v", err)
	}
	polls := 0
	err = runReleaseProductionSteeringLoopWithCutWait(t.Context(), func() error {
		cause := observeProductionSteeringRead(fixture.write(t.Context(), []byte("{\"object\":8}\n")), false)
		return &productionSteeringReadWait{phase: productionReadPreparation, cause: cause}
	}, func() bool {
		polls++
		return polls < 3*releaseSteeringFailureLimit
	}, nil, time.Now, releaseProductionCutWaitMinimumLimit)
	if err != nil {
		t.Fatalf("waits spent the failure budget: polls=%d err=%v", polls, err)
	}
}

// The same answers through the replica publisher that seals and publishes on
// mainnet, whose one operator is its only replica. Whatever wraps the status on
// the way to the steering classifier must keep a 503 a wait and a 403 a refusal.
func TestReservedUploadAnswersThroughTheReplicaPublisher(t *testing.T) {
	t.Parallel()
	publish := func(t *testing.T, status int, detail string) error {
		t.Helper()
		fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return status, detail })
		write := func(ctx context.Context, hash string, raw []byte) error {
			return fixture.owner.writeWithSigner(ctx, "metadata", hash, raw, fixture.signer)
		}
		publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), []AttemptCutV2Replica{{Origin: "https://api.example", WriteRecords: write, WriteProofs: write, WriteMetadata: write}})
		if err != nil {
			t.Fatal(err)
		}
		data := []byte("{\"object\":9}\n")
		return publisher.writer("metadata")(t.Context(), attemptHex32(sha256.Sum256(data)), data)
	}
	wait := publish(t, http.StatusServiceUnavailable, "Validator reserved upload is not running on this instance.")
	if wait == nil || !retryableProductionSteeringRead(observeProductionSteeringRead(wait, false)) || !transientReleaseSnapshotError(wait) {
		t.Fatalf("a 503 through the publisher is not a wait: %v", wait)
	}
	refusal := publish(t, http.StatusForbidden, "Validator staging authority refused.")
	if refusal == nil || retryableProductionSteeringRead(observeProductionSteeringRead(refusal, false)) || transientReleaseSnapshotError(refusal) {
		t.Fatalf("a 403 through the publisher is not a refusal: %v", refusal)
	}
}
