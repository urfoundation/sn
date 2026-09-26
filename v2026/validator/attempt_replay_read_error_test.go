//go:build linux || darwin

// Real signed terminal and ordinary streams traverse the production Http body,
// measurement verifier and steering loop under deterministic interruptions.
package validator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Retained envelopes are read even after a native intent exists, outside the
// fresh-generation retry permission. Each attempt owns fresh replay scratch.
func testAttemptReplayReadRecovery(t *testing.T, terminal bool, getFailure bool, interruption error) {
	t.Helper()
	fixture := newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
	_, envelope := fixture.seal(t)
	_, want, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, fixture.options())
	if err != nil {
		t.Fatal(err)
	}
	attempts, opened, closed := 0, 0, 0
	var got VerifiedReleaseMeasurementV2
	err = runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 21, nil }, func() error {
		attempts++
		options := fixture.options()
		noId := fixture.artifact.Inputs[0].NoID
		operator := options.Operators[noId].Measurement
		bounds := options.Operators[noId].Bounds
		if terminal {
			operator = options.Settlement.Operators[noId].Measurement
			bounds = options.Settlement.Operators[noId].Bounds
		}
		open := operator.Replay.OpenData
		fail := attempts <= releaseSteeringFailureLimit+2
		operator.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reader, err := NewHTTPAttemptStreamV2Reader("https://compact-replay.example", bounds)
			if err != nil {
				return nil, err
			}
			reader.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet || request.URL.Query().Get("hash") != hash || request.URL.Query().Get("kind") != kind {
					t.Fatal("replay changed the immutable Get identity")
				}
				if fail && getFailure {
					fail = false
					return nil, interruption
				}
				source, err := open(ctx, kind, hash, size)
				if err != nil {
					return nil, err
				}
				opened++
				body := &attemptStreamV2HTTPTestBody{read: source.Read, close: func() error { closed++; return source.Close() }}
				if fail {
					fail = false
					body.read = func(buffer []byte) (int, error) {
						count, readErr := source.Read(buffer[:min(len(buffer), 1)])
						return count, errors.Join(readErr, interruption)
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, ContentLength: int64(size), Body: body}, nil
			})
			return reader.OpenData(ctx, kind, hash, size)
		}
		if terminal {
			value := options.Settlement.Operators[noId]
			value.Measurement = operator
			options.Settlement.Operators[noId] = value
		} else {
			value := options.Operators[noId]
			value.Measurement = operator
			options.Operators[noId] = value
		}
		var replayErr error
		_, got, replayErr = VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
		if attempts <= releaseSteeringFailureLimit+2 {
			assertReleaseMeasurementV2Empty(t, got)
			if !errors.Is(replayErr, interruption) {
				t.Fatalf("failed read lost its original cause: %v", replayErr)
			}
		}
		return replayErr
	}, func() bool { return attempts < releaseSteeringFailureLimit+3 }, false, false)
	if err != nil || attempts != releaseSteeringFailureLimit+3 || opened == 0 || opened != closed || !reflect.DeepEqual(got, want) {
		t.Fatalf("retained compact read consumed native failures or changed replay: terminal=%t get=%t attempts=%d opened=%d closed=%d err=%v", terminal, getFailure, attempts, opened, closed, err)
	}
}

// The exact settlement replay path recovers after more than the native budget.
func TestAttemptReplayReadRetriesRetainedSettlementBody(t *testing.T) {
	testAttemptReplayReadRecovery(t, true, false, context.DeadlineExceeded)
}

// Ordinary operator replay has the same read ownership as terminal closure.
func TestAttemptReplayReadRetriesRetainedOperatorBody(t *testing.T) {
	testAttemptReplayReadRecovery(t, false, false, context.DeadlineExceeded)
}

// Refusal before any body exists still belongs to the immutable read attempt.
func TestAttemptReplayReadRetriesRetainedSettlementGet(t *testing.T) {
	testAttemptReplayReadRecovery(t, true, true, context.DeadlineExceeded)
}

// A broken Http transfer supplies transport provenance for unexpected eof;
// truncated canonical rows from a complete transfer keep their hard failure.
func TestAttemptReplayReadRetriesRetainedSettlementUnexpectedEof(t *testing.T) {
	testAttemptReplayReadRecovery(t, true, false, io.ErrUnexpectedEOF)
}

// The marker cannot soften a later integrity/close failure, forgive an earlier
// native failure, cancel the service, or authorize a strict native epoch gap.
func TestAttemptReplayReadPreservesFailureAndEpochGuards(t *testing.T) {
	interrupted := classifyAttemptReplayRead(context.DeadlineExceeded)
	broken := errors.New("synthetic intent custody failure")
	for _, cause := range []error{errors.Join(interrupted, broken), errors.Join(interrupted, context.Canceled), context.DeadlineExceeded} {
		attempts := 0
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return 21, nil }, func() error { attempts++; return cause }, func() bool { return true }, false, false)
		if err == nil || attempts != releaseSteeringFailureLimit || !errors.Is(err, cause) {
			t.Fatalf("unowned or mixed failure escaped its budget: attempts=%d err=%v", attempts, err)
		}
	}
	for _, test := range []struct {
		priorFailure bool
		allowFresh   bool
	}{
		{}, {allowFresh: true}, {priorFailure: true, allowFresh: true},
	} {
		reads, attempts := 0, 0
		err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) {
			reads++
			if reads > releaseSteeringFailureLimit+2 {
				return 22, nil
			}
			return 21, nil
		}, func() error {
			attempts++
			if test.priorFailure && attempts == 1 {
				return broken
			}
			return interrupted
		}, func() bool { return true }, false, test.allowFresh)
		if err == nil || !strings.Contains(err.Error(), "incomplete epoch 21") || attempts != releaseSteeringFailureLimit+2 || test.priorFailure && !errors.Is(err, broken) {
			t.Fatalf("read retry erased strict continuity or a prior failure: attempts=%d err=%v", attempts, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	attempts := 0
	err := runReleaseSteeringLoopWithWaitAndPermissions(ctx, func() (uint64, error) { return 21, nil }, func() error { attempts++; return interrupted }, func() bool { cancel(); return true }, false, false)
	if err != nil || attempts != 1 {
		t.Fatalf("service cancellation started another replay: attempts=%d err=%v", attempts, err)
	}
}

// Only existing explicit provisional deferral can carry the generic retained
// replay to a later native epoch; it still publishes no successful decision.
func TestAttemptReplayReadKeepsExplicitProvisionalDeferral(t *testing.T) {
	attempts := 0
	err := runReleaseSteeringLoopWithWaitAndPermissions(t.Context(), func() (uint64, error) { return uint64(21 + attempts), nil }, func() error {
		attempts++
		if attempts <= releaseSteeringFailureLimit+2 {
			return classifyAttemptReplayRead(context.DeadlineExceeded)
		}
		return nil
	}, func() bool { return attempts < releaseSteeringFailureLimit+3 }, true, false)
	if err != nil || attempts != releaseSteeringFailureLimit+3 {
		t.Fatalf("explicit provisional deferral lost its replay retry: attempts=%d err=%v", attempts, err)
	}
}
