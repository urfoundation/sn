// Owned HTTP reads recover from bounded transport failures without request
// pacing. Submissions and complete JSON-RPC responses are never replayed here.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
)

// An outer RPC retry loop owns its whole budget; its individual HTTP attempts
// must not multiply that budget by invoking another retry loop underneath it.
type ownedEvmRpcRetryBudgetKey struct{}

// Configuration is immutable after construction, so independent concurrent
// reads have independent budgets and never share a gate or cooldown.
type ownedEvmRetryTransport struct {
	base   http.RoundTripper
	policy finalSemanticRPCRetryPolicy
}

func newOwnedEvmRetryTransport(base http.RoundTripper) *ownedEvmRetryTransport {
	return &ownedEvmRetryTransport{base: base, policy: defaultFinalSemanticRPCRetryPolicy()}
}

// Only typed transport failures qualify. A joined integrity/transport failure
// is permanent unless every cause is independently a transient transport error.
func ownedEvmTransportErrorIsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if _, fileError := err.(*os.PathError); fileError {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !ownedEvmTransportErrorIsTransient(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return ownedEvmTransportErrorIsTransient(cause)
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

// Replay only the exact bytes originally supplied, not a potentially different
// GetBody implementation. Oversized requests retain their original byte stream
// and run once, rather than imposing a new request-size limit on the owned node.
func ownedEvmReplayRequest(request *http.Request) (*http.Request, bool, error) {
	if request == nil {
		return nil, false, errors.New("owned EVM HTTP request is nil")
	}
	if request.Body == nil {
		return request, false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, publicEVMRPCBodyReplayLimit+1))
	if err != nil {
		request.Body.Close()
		return nil, false, err
	}
	copy := request.Clone(request.Context())
	if len(raw) > publicEVMRPCBodyReplayLimit {
		copy.Body = &ownedEvmReplayBody{Reader: io.MultiReader(bytes.NewReader(raw), request.Body), source: request.Body}
		return copy, false, nil
	}
	request.Body.Close()
	copy.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	copy.Body, _ = copy.GetBody()
	return copy, publicEVMRPCRequestIsReadOnly(copy), nil
}

// A large successful response remains streaming with its original deadline.
// Its prefix is restored exactly; closing releases both body and attempt timer.
type ownedEvmReplayBody struct {
	io.Reader
	source io.Closer
	cancel context.CancelFunc
}

func (self *ownedEvmReplayBody) Close() error {
	if self.cancel != nil {
		defer self.cancel()
	}
	return self.source.Close()
}

// Each retry has a fresh bounded context covering headers and buffered body.
// The enclosing operation context owns the complete retry budget.
func (self *ownedEvmRetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request, readOnly, err := ownedEvmReplayRequest(request)
	if err != nil {
		return nil, err
	}
	base := self.base
	if base == nil {
		base = http.DefaultTransport
	}
	if err := request.Context().Err(); err != nil {
		if request.Body != nil {
			request.Body.Close()
		}
		return nil, err
	}
	if !readOnly {
		return base.RoundTrip(request)
	}
	if err := self.policy.validate(); err != nil {
		request.Body.Close()
		return nil, err
	}
	maximumAttempts := self.policy.maximumAttempts
	if managed, _ := request.Context().Value(ownedEvmRpcRetryBudgetKey{}).(bool); managed {
		maximumAttempts = 1
	}
	delay := self.policy.initialRetryDelay
	for attempt := 1; attempt <= maximumAttempts; attempt++ {
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(request.Context(), self.policy.attemptTimeout)
		current := request.Clone(attemptCtx)
		current.Body, err = request.GetBody()
		if err != nil {
			cancel()
			return nil, err
		}
		response, callErr := base.RoundTrip(current)
		if callErr == nil && response != nil && response.Body != nil {
			// HTTP semantic errors keep their original response, without retries.
			if response.StatusCode != http.StatusOK {
				response.Body = &ownedEvmReplayBody{Reader: response.Body, source: response.Body, cancel: cancel}
				return response, nil
			}
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, publicEVMRPCResponseReadLimit+1))
			if readErr == nil && len(raw) > publicEVMRPCResponseReadLimit {
				response.Body = &ownedEvmReplayBody{Reader: io.MultiReader(bytes.NewReader(raw), response.Body), source: response.Body, cancel: cancel}
				return response, nil
			}
			response.Body.Close()
			if readErr == nil {
				cancel()
				response.Body = io.NopCloser(bytes.NewReader(raw))
				return response, nil
			}
			callErr = readErr
		} else if callErr == nil {
			callErr = errors.New("owned EVM RPC returned an empty HTTP response")
		} else if response != nil && response.Body != nil {
			response.Body.Close()
		}
		cancel()
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		if attempt == maximumAttempts || !ownedEvmTransportErrorIsTransient(callErr) {
			return nil, callErr
		}
		if err := self.policy.wait(request.Context(), delay); err != nil {
			return nil, err
		}
		delay = min(delay*2, self.policy.maximumRetryDelay)
	}
	return nil, errors.New("owned EVM RPC retry budget exhausted")
}
