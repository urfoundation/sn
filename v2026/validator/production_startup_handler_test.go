//go:build linux || darwin

// A fixture barrier proves real request consumption and owns an independent
// cleanup release. Neither proof depends on client cancellation delivery.
package validator

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Handler publication synchronizes reads of the completed body lifecycle.
type productionStartupHandlerTestBody struct {
	*bytes.Reader
	closed   bool
	closeErr error
}

// The real close result remains observable before a handler may block.
func (self *productionStartupHandlerTestBody) Close() error {
	self.closed = true
	return self.closeErr
}

// Both blocking handlers use synthetic traffic and their actual implementation.
func productionStartupHandlerTestCases() []struct {
	name    string
	payload []byte
	barrier chan struct{}
	release chan struct{}
	handler http.Handler
} {
	api := &productionStartupApiTestFixture{seedRead: make(chan struct{}), release: make(chan struct{})}
	evm := &productionStartupEvmTestFixture{
		operator:  &recycleOperatorFixture{releaseDecisionV2TestFixture: &releaseDecisionV2TestFixture{blocks: map[uint64][32]byte{110: {0xa2}}}},
		freshRead: make(chan struct{}), release: make(chan struct{}),
	}
	return []struct {
		name    string
		payload []byte
		barrier chan struct{}
		release chan struct{}
		handler http.Handler
	}{
		{name: "seed", payload: []byte(`{"count":1}`), barrier: api.seedRead, release: api.release, handler: api},
		{name: "evm", payload: []byte(`{"method":"eth_call","params":[{}, {"blockHash":"0xa200000000000000000000000000000000000000000000000000000000000000","requireCanonical":true}]}`), barrier: evm.freshRead, release: evm.release, handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { evm.allowHttp(writer, request) })},
	}
}

// The request context stays live. Cleanup alone must join each blocked owner,
// and a published barrier must imply complete, closed physical request input.
func TestProductionStartupBlockedHandlersOwnRequestAndCleanup(t *testing.T) {
	for _, test := range productionStartupHandlerTestCases() {
		body := &productionStartupHandlerTestBody{Reader: bytes.NewReader(test.payload)}
		request := httptest.NewRequest(http.MethodPost, "/network/find-providers2", body)
		done := make(chan struct{})
		go func() { defer close(done); test.handler.ServeHTTP(httptest.NewRecorder(), request) }()
		select {
		case <-test.barrier:
		case <-done:
			t.Fatalf("%s fixture returned before its expected request barrier", test.name)
		}
		consumed := body.closed && body.Len() == 0
		close(test.release)
		<-done
		if !consumed {
			t.Errorf("%s fixture published a barrier before closing its complete request", test.name)
		}
		if request.Context().Err() != nil {
			t.Errorf("%s cleanup test depended on request cancellation", test.name)
		}
	}
}

// Oversized or unsuccessfully closed input never reaches the wait barrier.
func TestProductionStartupBlockedHandlersRejectIncompleteInput(t *testing.T) {
	for _, fault := range []struct {
		name     string
		payload  []byte
		closeErr error
	}{
		{name: "oversized", payload: bytes.Repeat([]byte{0x61}, 1024*1024+2)},
		{name: "close", payload: []byte(`{}`), closeErr: io.ErrUnexpectedEOF},
	} {
		for _, test := range productionStartupHandlerTestCases() {
			body := &productionStartupHandlerTestBody{Reader: bytes.NewReader(fault.payload), closeErr: fault.closeErr}
			request := httptest.NewRequest(http.MethodPost, "/network/find-providers2", body)
			response := httptest.NewRecorder()
			test.handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !body.closed {
				t.Errorf("%s/%s failed to close and reject incomplete input", test.name, fault.name)
			}
			select {
			case <-test.barrier:
				t.Errorf("%s/%s published an invalid readiness barrier", test.name, fault.name)
			default:
			}
			if fault.name == "oversized" && body.Len() != 1 {
				t.Errorf("%s fixture request consumption exceeded its finite bound", test.name)
			}
			if fault.closeErr != nil {
				request.Body = &productionStartupHandlerTestBody{Reader: bytes.NewReader(nil), closeErr: fault.closeErr}
				if _, err := productionStartupRequestTestBytes(request); !errors.Is(err, fault.closeErr) {
					t.Errorf("%s fixture lost the actual close cause", test.name)
				}
			}
		}
	}
}
