//go:build linux || darwin

// The upload owner's retry of uploads a lossy path cut short: which answers go
// again, how often, after what wait, and what reaches the steering classifiers
// when the bound runs out. Real writers and signers over a deterministic transport.
package validator

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every answer of a path that lost the upload goes again, a bounded number of
// times, with a read back before each repeat. When the bound runs out the last
// answer reaches the classifiers unchanged: a 503 is still a steering wait.
func TestAttemptUploadTransientAnswersGoAgainWithinTheBound(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return status, "synthetic" })
		err := fixture.write(t.Context(), []byte("{\"object\":10}\n"))
		var answer *attemptStreamHttpStatusError
		if !errors.As(err, &answer) || answer.status != status || !answer.upload {
			t.Fatalf("status %d: the last answer was lost: %v", status, err)
		}
		if fixture.requests() != releaseAttemptUploadAttempts || len(fixture.waits) != releaseAttemptUploadAttempts-1 || int(fixture.probes.Load()) != releaseAttemptUploadAttempts-1 {
			t.Fatalf("status %d: requests=%d waits=%d probes=%d, want %d attempts", status, fixture.requests(), len(fixture.waits), fixture.probes.Load(), releaseAttemptUploadAttempts)
		}
		if !transientReleaseSnapshotError(err) || !retryableProductionSteeringRead(observeProductionSteeringRead(err, false)) {
			t.Fatalf("status %d: an exhausted transient answer became hard: %v", status, err)
		}
	}
}

// A refusal or a malformed request keeps its one request, its own verdict, and
// costs no read back: sending it again would not change the answer.
func TestAttemptUploadVerdictsAreNotSentAgain(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return status, "synthetic" })
		err := fixture.write(t.Context(), []byte("{\"object\":11}\n"))
		var answer *attemptStreamHttpStatusError
		if !errors.As(err, &answer) || answer.status != status || fixture.requests() != 1 || len(fixture.waits) != 0 || fixture.probes.Load() != 0 {
			t.Fatalf("status %d: requests=%d waits=%d probes=%d err=%v", status, fixture.requests(), len(fixture.waits), fixture.probes.Load(), err)
		}
	}
	// a malformed acknowledgement is the api's answer too
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusOK, "" })
	if err := fixture.write(t.Context(), []byte("{\"object\":12}\n")); err == nil || fixture.requests() != 1 || fixture.probes.Load() != 0 {
		t.Fatalf("malformed acknowledgement: requests=%d probes=%d err=%v", fixture.requests(), fixture.probes.Load(), err)
	}
}

// The ordinary lane has no reserved admission to differ between instances, so
// its 403 stays one request; its transient answers go again like the reserved lane's.
func TestAttemptUploadOrdinaryLaneRetriesOnlyTransientAnswers(t *testing.T) {
	t.Parallel()
	data := []byte("{\"object\":13}\n")
	hash := attemptHex32(sha256.Sum256(data))
	refused := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusForbidden, "synthetic" })
	if err := refused.owner.write(t.Context(), "metadata", hash, data); err == nil || refused.requests() != 1 || len(refused.waits) != 0 {
		t.Fatalf("ordinary 403: requests=%d waits=%d err=%v", refused.requests(), len(refused.waits), err)
	}
	unavailable := newReleaseUploadRefusalFixture(t, func(request int) (int, string) {
		if request == 0 {
			return http.StatusServiceUnavailable, "synthetic"
		}
		return http.StatusNoContent, ""
	})
	if err := unavailable.owner.write(t.Context(), "metadata", hash, data); err != nil || unavailable.requests() != 2 || len(unavailable.waits) != 1 {
		t.Fatalf("ordinary 503: requests=%d waits=%d err=%v", unavailable.requests(), len(unavailable.waits), err)
	}
	for index, consent := range unavailable.consents {
		if consent != "" {
			t.Fatalf("ordinary attempt %d carried a reserved consent", index)
		}
	}
}

// Refusals and lost uploads keep separate bounds: a refusing instance does
// not spend the lossy path's attempts, nor the other way around.
func TestAttemptUploadRefusalAndLossKeepSeparateBounds(t *testing.T) {
	t.Parallel()
	answers := []int{http.StatusForbidden, http.StatusBadGateway, http.StatusForbidden, http.StatusServiceUnavailable, http.StatusForbidden, http.StatusGatewayTimeout, http.StatusNoContent}
	fixture := newReleaseUploadRefusalFixture(t, func(request int) (int, string) { return answers[min(request, len(answers)-1)], "" })
	if err := fixture.write(t.Context(), []byte("{\"object\":14}\n")); err != nil {
		t.Fatalf("three refusals and three losses spent a shared bound: %v", err)
	}
	if fixture.requests() != len(answers) || len(fixture.waits) != len(answers)-1 || fixture.probes.Load() != 3 {
		t.Fatalf("requests=%d waits=%d probes=%d", fixture.requests(), len(fixture.waits), fixture.probes.Load())
	}
	for index, wait := range fixture.waits {
		if answers[index] == http.StatusForbidden && wait != releaseReservedUploadRefusalDelay {
			t.Fatalf("refusal %d waited %s", index, wait)
		}
	}
}

// Waits grow from the base and are drawn in the upper half of each ceiling, so
// validators that lost the same path do not come back at once; a Retry-After
// longer than the draw is honored up to the cap.
func TestAttemptUploadBackoffGrowsWithJitterAndHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusBadGateway, "" })
	for _, draw := range []struct {
		name string
		draw func(int64) int64
		want []time.Duration
	}{
		{name: "lowest", draw: func(int64) int64 { return 0 }, want: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}},
		{name: "highest", draw: func(n int64) int64 { return n - 1 }, want: []time.Duration{2*time.Second - 1, 4*time.Second - 1, 8*time.Second - 1, 16*time.Second - 1, 32*time.Second - 1, time.Minute - 1, time.Minute - 1}},
	} {
		fixture.owner.jitter = draw.draw
		for index, want := range draw.want {
			if got := fixture.owner.backoff(uint64(index+1), errors.New("lost")); got != want {
				t.Fatalf("%s draw, failure %d: wait %s, want %s", draw.name, index+1, got, want)
			}
		}
	}
	fixture.owner.jitter = nil
	for failures := uint64(1); failures <= 12; failures++ {
		ceiling := min(releaseAttemptUploadBackoffBase<<min(failures-1, 8), releaseAttemptUploadBackoffCap)
		for range 64 {
			if got := fixture.owner.backoff(failures, errors.New("lost")); got < ceiling/2 || got >= ceiling {
				t.Fatalf("failure %d: wait %s is outside [%s, %s)", failures, got, ceiling/2, ceiling)
			}
		}
	}
	fixture.owner.jitter = func(int64) int64 { return 0 }
	for _, hint := range []struct {
		seconds string
		want    time.Duration
	}{{"7", 7 * time.Second}, {"3600", releaseAttemptUploadBackoffCap}} {
		header := http.Header{"Retry-After": {hint.seconds}}
		answer := &attemptStreamHttpStatusError{status: http.StatusServiceUnavailable, upload: true, retryAfter: attemptStreamHttpRetryAfter(header)}
		if got := fixture.owner.backoff(1, errors.Join(answer, nil)); got != hint.want {
			t.Fatalf("Retry-After %s: wait %s, want %s", hint.seconds, got, hint.want)
		}
	}
}

// A Retry-After the answer carries is the wait the owner takes.
func TestAttemptUploadWaitsTheAnsweredRetryAfter(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(request int) (int, string) {
		if request == 0 {
			return http.StatusTooManyRequests, ""
		}
		return http.StatusNoContent, ""
	})
	fixture.owner.writer.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if response != nil && response.StatusCode == http.StatusTooManyRequests {
			response.Header.Set("Retry-After", strconv.Itoa(9))
		}
		return response, err
	})
	if err := fixture.write(t.Context(), []byte("{\"object\":15}\n")); err != nil {
		t.Fatal(err)
	}
	if len(fixture.waits) != 1 || fixture.waits[0] != 9*time.Second {
		t.Fatalf("waits = %v, want the answered 9s", fixture.waits)
	}
}

// A retry stops with its owner, joined, with no further request or read back.
func TestAttemptUploadRetryEndsWithItsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusServiceUnavailable, "" })
	fixture.owner.refusalWait = func(ctx context.Context, _ time.Duration) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	err := fixture.write(ctx, []byte("{\"object\":16}\n"))
	if !errors.Is(err, context.Canceled) || fixture.requests() != 1 || fixture.probes.Load() != 1 {
		t.Fatalf("requests=%d probes=%d err=%v", fixture.requests(), fixture.probes.Load(), err)
	}
}

// The read back before a repeat takes only the answer's head. An answer of
// another length is not the object, so the object goes again.
func TestAttemptUploadReadBackChecksTheAnswer(t *testing.T) {
	t.Parallel()
	data := []byte("{\"object\":17}\n")
	other := []byte("{\"object\":1700}\n")
	var fixture *releaseUploadRefusalFixture
	fixture = newReleaseUploadRefusalFixture(t, func(request int) (int, string) {
		if request == 0 {
			fixture.stored.Store(&other)
			return http.StatusBadGateway, ""
		}
		return http.StatusNoContent, ""
	})
	if err := fixture.write(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	if fixture.requests() != 2 || fixture.probes.Load() != 1 {
		t.Fatalf("an answer of another length ended the retry: requests=%d probes=%d", fixture.requests(), fixture.probes.Load())
	}
}

// Other bytes of the same length under the address pass the head-only read
// back, and the publisher's full read back still refuses them: nothing is
// published that does not hash to its address.
func TestAttemptUploadReadBackLeavesTheHashToThePublisher(t *testing.T) {
	t.Parallel()
	data := []byte("{\"object\":17}\n")
	other := []byte("{\"object\":18}\n")
	var fixture *releaseUploadRefusalFixture
	fixture = newReleaseUploadRefusalFixture(t, func(int) (int, string) {
		fixture.stored.Store(&other)
		return http.StatusBadGateway, ""
	})
	write := func(ctx context.Context, hash string, raw []byte) error {
		return fixture.owner.writeWithSigner(ctx, "metadata", hash, raw, fixture.signer)
	}
	publisher, err := newAttemptCutV2Replicas(attemptCutV2ReplicaTestBounds(), []AttemptCutV2Replica{{Origin: "https://api.example", WriteRecords: write, WriteProofs: write, WriteMetadata: write}})
	if err != nil {
		t.Fatal(err)
	}
	publisher.readers[0].client.Transport = attemptStreamV2HTTPTestTransport(fixture.roundTrip)
	if err := publisher.writer("metadata")(t.Context(), attemptHex32(sha256.Sum256(data)), data); err == nil {
		t.Fatal("other bytes under the address were published")
	}
	if fixture.requests() != 1 || fixture.probes.Load() != 2 {
		t.Fatalf("requests=%d reads=%d, want one upload, one head read and one full read", fixture.requests(), fixture.probes.Load())
	}
}

// The production seal uploads its data objects side by side, through the
// memo and replica publisher, which are safe for concurrent calls.
func TestReleaseSealUploadsDataObjectsInFlight(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runtime := &releaseRuntimeV2{history: &releaseEvidenceV2StartupHistory{keys: map[uint64]map[byte]ed25519.PublicKey{1: {}}}}
	runtime.cfg.EvidenceV2.Bounds.Cut = attemptCutV2ReplicaTestBounds()
	runtime.cfg.EvidenceV2.Operators = []ReleaseEvidenceV2OperatorConfig{{NoID: 1, ReplayScratchRoot: filepath.Join(root, "replay"), SealScratchRoot: filepath.Join(root, "seal")}}
	write := func(context.Context, string, []byte) error { return nil }
	for _, replicas := range [][]AttemptCutV2Replica{
		{{Origin: "https://one.example", WriteRecords: write, WriteProofs: write, WriteMetadata: write}},
		{{Origin: "https://one.example", WriteRecords: write, WriteProofs: write, WriteMetadata: write}, {Origin: "https://two.example", WriteRecords: write, WriteProofs: write, WriteMetadata: write}},
	} {
		options, err := runtime.seal(t.Context(), 1, replicas, "seal")
		if err != nil {
			t.Fatal(err)
		}
		if options.UploadsInFlight != attemptStreamV2WriteInFlight || attemptStreamV2WriteInFlight < 2 {
			t.Fatalf("%d replicas: %d uploads in flight", len(replicas), options.UploadsInFlight)
		}
	}
}

// A reserved upload never runs past its signed consent: the api would refuse
// it there. An ordinary one runs to its size-scaled cap.
func TestAttemptUploadDeadlineStopsAtTheSignedConsent(t *testing.T) {
	t.Parallel()
	var remaining []time.Duration
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusNoContent, "" })
	// shorter than the object's cap
	fixture.signer.maximumIntentSeconds = 10
	fixture.owner.writer.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
		deadline, bounded := request.Context().Deadline()
		if !bounded {
			return nil, errors.New("upload has no deadline")
		}
		remaining = append(remaining, time.Until(deadline))
		return fixture.roundTrip(request)
	})
	data := []byte(strings.Repeat("{\"object\":19}\n", 4096))
	hash := attemptHex32(sha256.Sum256(data))
	if err := fixture.owner.writeWithSigner(t.Context(), AttemptStreamV2Records, hash, data, fixture.signer); err != nil {
		t.Fatal(err)
	}
	if err := fixture.owner.write(t.Context(), AttemptStreamV2Records, hash, data); err != nil {
		t.Fatal(err)
	}
	consent := time.Duration(fixture.signer.maximumIntentSeconds) * time.Second
	span := attemptUploadTiming{}.cap(len(data))
	if len(remaining) != 2 || remaining[0] > consent || remaining[0] < consent-5*time.Second || remaining[1] > span || remaining[1] < span-5*time.Second || span <= consent {
		t.Fatalf("deadlines %v, want the consent's %s then the cap's %s", remaining, consent, span)
	}
}
