// Scheduled API outages admit only typed request absence. Real signed-walk
// regressions force source/signature failures during the same active window.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// Exercise the real request/assignment path: an active fault cannot hide a
// wrong source, malformed success, or a cryptographically invalid assignment.
func TestVerifyFaultAttributionKeepsSignedWalkSemanticsBlocking(t *testing.T) {
	validatorSeed := make([]byte, ed25519.SeedSize)
	validatorSeed[0] = 31
	validatorPrivate := ed25519.NewKeyFromSeed(validatorSeed)
	validatorPublic := validatorPrivate.Public().(ed25519.PublicKey)
	serverSeed := make([]byte, ed25519.SeedSize)
	serverSeed[0] = 47
	serverPrivate := ed25519.NewKeyFromSeed(serverSeed)
	serverPublic := serverPrivate.Public().(ed25519.PublicKey)
	provider, next, trailId := connect.Id{1}, connect.Id{2}, connect.Id{3}
	nonce := make([]byte, connect.VerifyNonceSize)
	message, err := connect.BuildVerifyAssignMessage(7, trailId, nonce, validatorPublic, connect.VerifyMMin, []connect.Id{provider, next})
	if err != nil {
		t.Fatal(err)
	}
	canonical := connect.VerifyAssignResult{TrailId: trailId, ServerNonce: nonce, Trail: []connect.Id{provider}, NextHop: next, M: connect.VerifyMMin, ServerKeyId: 7, AssignSig: ed25519.Sign(serverPrivate, message)}
	keys, err := json.Marshal(map[string]any{"keys": []map[string]any{{"server_key_id": 7, "public_key": serverPublic}}})
	if err != nil {
		t.Fatal(err)
	}
	window := newAdversaryFaultWindow(time.Minute)
	window.Update([]string{"operator-1-api"})
	cfg := testResolvedConfig(t)
	cfg.Policy.Verify.TrailDepth = connect.VerifyMMin
	for _, test := range []struct {
		name      string
		mutate    func(*connect.VerifyAssignResult)
		malformed bool
		want      string
	}{
		{name: "source", mutate: func(value *connect.VerifyAssignResult) { value.Trail = []connect.Id{next} }, want: "wrong source hop"},
		{name: "signature", mutate: func(value *connect.VerifyAssignResult) {
			value.AssignSig = append([]byte(nil), value.AssignSig...)
			value.AssignSig[0] ^= 1
		}, want: "signature is invalid"},
		{name: "repeated hop", mutate: func(value *connect.VerifyAssignResult) { value.NextHop = provider }, want: "repeats an existing hop"},
		{name: "malformed success", malformed: true, want: "wrong source hop"},
	} {
		assign := canonical
		if test.mutate != nil {
			test.mutate(&assign)
		}
		body, err := json.Marshal(assign)
		if err != nil {
			t.Fatal(err)
		}
		if test.malformed {
			body = []byte("malformed synthetic response")
		}
		actor := &verifyAdversary{
			cfg: cfg, faults: window, validators: map[int]verifyAdversaryIdentity{1: {clientID: connect.Id{4}, private: validatorPrivate, public: validatorPublic}},
			seedProviders: map[int][]connect.Id{1: {provider}}, providerSources: map[int]map[connect.Id]string{1: {provider: "192.0.2.1"}},
			http: adversaryGetTestClient(func(request *http.Request) (*http.Response, error) {
				if request.Method == http.MethodGet && request.URL.Path == "/verify/keys" {
					return adversaryGetTestResponse(http.StatusOK, string(keys)), nil
				}
				if request.Method != http.MethodPost || request.URL.Path != "/verify" {
					t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
				}
				return adversaryGetTestResponse(http.StatusOK, string(body)), nil
			}),
		}
		_, requests, _, err := actor.walk(t.Context(), 1, 0, false)
		if err == nil || requests != 2 || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s did not reach its semantic failure: requests=%d err=%v", test.name, requests, err)
		}
		if result := actor.sampleError(1, err, requests, 1); result.Outcome != adversaryOutcomeError {
			t.Fatalf("active API fault hid %s: %+v", test.name, result)
		}
	}
}

// Typed request failure remains scoped to the exact target and bounded grace;
// error text, semantic statuses, and mixed causes cannot manufacture attribution.
func TestVerifyFaultAttributionRequiresTypedUnavailability(t *testing.T) {
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	window := newAdversaryFaultWindow(time.Second)
	window.now = func() time.Time { return at }
	window.Update([]string{"operator-1-api"})
	actor := &verifyAdversary{faults: window}
	timeout := adversaryVerifyHttpFailure("synthetic post", 0, context.DeadlineExceeded)
	unavailable := adversaryVerifyHttpFailure("synthetic post", http.StatusServiceUnavailable, nil)
	for _, err := range []error{timeout, unavailable, fmt.Errorf("wrapped: %w", timeout), errors.Join(timeout, unavailable)} {
		if result := actor.sampleError(1, err, 2, 1); result.Outcome != adversaryOutcomeExpectedRejection {
			t.Fatalf("typed unavailable request rejected: %+v", result)
		}
		if result := actor.sampleError(2, err, 2, 1); result.Outcome != adversaryOutcomeError {
			t.Fatalf("unrelated target was attributed: %+v", result)
		}
	}
	for _, err := range []error{
		errors.New("connection reset timeout"), context.DeadlineExceeded,
		adversaryVerifyHttpFailure("semantic status", http.StatusForbidden, context.DeadlineExceeded),
		adversaryVerifyHttpFailure("canceled request", 0, context.Canceled),
		adversaryVerifyHttpFailure("mixed request", 0, errors.Join(io.ErrUnexpectedEOF, errors.New("invalid signature"))),
		errors.Join(unavailable, errors.New("wrong source hop")),
		&adversaryReadIntegrityError{cause: timeout},
		validateAdversaryFinal(nil, connect.Id{}, nil, 0, nil, nil, nil, nil),
	} {
		if result := actor.sampleError(1, err, 2, 1); result.Outcome != adversaryOutcomeError {
			t.Fatalf("fault hid semantic/unowned cause %v: %+v", err, result)
		}
	}
	window.Update(nil)
	if actor.sampleError(1, unavailable, 2, 1).Outcome != adversaryOutcomeExpectedRejection {
		t.Fatal("bounded in-flight grace was lost")
	}
	at = at.Add(2 * time.Second)
	if actor.sampleError(1, unavailable, 2, 1).Outcome != adversaryOutcomeError {
		t.Fatal("restored target remained attributed after grace")
	}
}

// Key and proof GETs preserve request-origin markers through exhausted retries.
func TestVerifyFaultAttributionRetainsUnavailableReadBoundary(t *testing.T) {
	window := newAdversaryFaultWindow(time.Second)
	window.Update([]string{"operator-1-api"})
	actor := &verifyAdversary{faults: window, http: adversaryGetTestClient(func(*http.Request) (*http.Response, error) {
		return adversaryGetTestResponse(http.StatusServiceUnavailable, "synthetic outage"), nil
	})}
	_, requests, err := actor.serverKeys(t.Context(), 1)
	if requests != 3 || err == nil || actor.sampleError(1, err, requests, 1).Outcome != adversaryOutcomeExpectedRejection {
		t.Fatalf("key boundary lost outage: requests=%d err=%v", requests, err)
	}
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	requests, err = actor.requireUniqueProof(t.Context(), 1, connect.Id{}, at, at.Add(time.Second))
	if requests != 3 || err == nil || actor.sampleError(1, err, requests, 1).Outcome != adversaryOutcomeExpectedRejection {
		t.Fatalf("proof boundary lost outage: requests=%d err=%v", requests, err)
	}
}
