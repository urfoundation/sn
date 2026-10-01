// Preserve configured native HTTP-read causes before the pinned GSRPC client
// flattens status errors or loses response-body origin during JSON decoding.
package crv4

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
)

// Events admit 16 MiB of SCALE bytes (32 MiB + 0x on JSON wire), larger than
// block/code responses. A separate finite allowance covers the JSON envelope.
const finalizedExtrinsicEventsBytes = 16 * 1024 * 1024
const substrateReadHttpEnvelopeBytes = 64 * 1024
const substrateReadHttpResponseLimit = 2 + 2*finalizedExtrinsicEventsBytes + substrateReadHttpEnvelopeBytes

// Only the allowlisted read owner sets this private marker on an attempt.
// Submissions and unknown methods retain their existing nonretrying transport.
type substrateReadHttpContextKey struct{}

// A status is created only at the configured physical native read boundary.
// Private fields prevent callers from turning arbitrary diagnostic text into it.
type SubstrateReadHttpStatusError struct {
	status int
}

// Diagnostics omit response bodies, credentials and endpoint query strings.
func (self *SubstrateReadHttpStatusError) Error() string {
	return fmt.Sprintf("crv4: native read HTTP status %d", self.status)
}

// Structured status remains available without parsing diagnostic strings.
func (self *SubstrateReadHttpStatusError) StatusCode() int { return self.status }

// Physical round-trip/body failures keep origin independently of JSON decoding.
// A successfully received nonempty malformed JSON body never receives this type.
type substrateReadHttpTransportError struct {
	cause error
}

// Preserve the original cause for diagnostics without reinterpreting its text.
func (self *substrateReadHttpTransportError) Error() string {
	return fmt.Sprintf("crv4: native HTTP read interrupted: %v", self.cause)
}

// Callers can still recognize cancellation or the exact physical read error.
func (self *substrateReadHttpTransportError) Unwrap() error { return self.cause }

// Body release failures retain their physical origin separately from local
// custody. Only pure connection failures may retry the allowlisted read.
type substrateReadHttpCloseError struct {
	cause error
}

// Keep response release diagnostics distinct from local custody failures.
func (self *substrateReadHttpCloseError) Error() string {
	return fmt.Sprintf("crv4: native HTTP body close failed: %v", self.cause)
}

// Preserve the original release cause; only its complete typed tree can retry.
func (self *substrateReadHttpCloseError) Unwrap() error { return self.cause }

// Exactly one owner closes each physical body before retry or JSON decoding.
func closeSubstrateReadHttpBody(body io.ReadCloser, transport http.RoundTripper) error {
	if err := body.Close(); err != nil {
		// Do not hand an uncertain connection back to a later observation.
		// The attempt owner also cancels the completed request before retry.
		if idle, ok := transport.(interface{ CloseIdleConnections() }); ok {
			idle.CloseIdleConnections()
		}
		return &substrateReadHttpCloseError{cause: err}
	}
	return nil
}

// Only transient status classes may reuse an existing allowlisted read budget.
func retryableSubstrateReadHttpStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500 && status <= 599
}

// Production continuation may preserve only physically originated native read
// failures. A bare EOF, timeout, status string or mixed integrity error is false.
func RetryableSubstrateReadTransportError(err error) bool {
	retryable, originated := classifySubstrateReadHttpError(err)
	return retryable && originated
}

// A consumer must not fall back to generic URL/EOF unwrapping after this owner
// rejects a native close or mixed error. Presence is separate from retryability.
func HasSubstrateReadTransportCause(err error) bool {
	if IsSubstrateReadTransportCause(err) {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if HasSubstrateReadTransportCause(cause) {
				return true
			}
		}
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return HasSubstrateReadTransportCause(wrapped.Unwrap())
	}
	return false
}

// A composed read owner treats each native transport boundary as an opaque
// subtree. It may combine independent transient readers, but must never unwrap
// a rejected native close or framing cause into a generic retryable deadline.
func IsSubstrateReadTransportCause(err error) bool {
	switch err.(type) {
	case *SubstrateReadHttpStatusError, *substrateReadHttpTransportError, *substrateReadHttpCloseError:
		return true
	}
	return false
}

// Every joined branch must be transient and at least one must retain this
// client's actual read origin. A joined owner deadline does not erase that cause.
func classifySubstrateReadHttpError(err error) (bool, bool) {
	if err == nil || err == context.Canceled || err == gsrpcgeth.ErrClientQuit {
		return false, false
	}
	if _, localFile := err.(*os.PathError); localFile {
		return false, false
	}
	if _, rpcError := err.(gsrpcgeth.Error); rpcError {
		return false, false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false, false
		}
		originated := false
		for _, cause := range causes {
			retryable, physical := classifySubstrateReadHttpError(cause)
			if !retryable {
				return false, false
			}
			originated = originated || physical
		}
		return true, originated
	}
	switch cause := err.(type) {
	case *SubstrateReadHttpStatusError:
		return retryableSubstrateReadHttpStatus(cause.status), true
	case *substrateReadHttpTransportError:
		return retryableSubstrateRpcReadTransport(cause.cause, true, false), true
	case *substrateReadHttpCloseError:
		return retryableSubstrateRpcReadTransport(cause.cause, true, false), true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return classifySubstrateReadHttpError(wrapped.Unwrap())
	}
	return err == context.DeadlineExceeded, false
}

// An immutable instance wraps only allowlisted read attempts. It owns and
// closes each physical body before GSRPC sees a finite in-memory response.
type substrateReadHttpTransport struct {
	base         http.RoundTripper
	maximumBytes int64
}

// No request is retried here. Body framing errors outrank a decodable JSON
// prefix, and a separate close/cancellation error cannot be hidden by status.
func (self *substrateReadHttpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || self == nil || self.base == nil || self.maximumBytes <= 0 || self.maximumBytes > substrateReadHttpResponseLimit {
		return nil, errors.New("crv4: bounded native HTTP transport is unavailable")
	}
	if marked, _ := request.Context().Value(substrateReadHttpContextKey{}).(bool); !marked {
		return self.base.RoundTrip(request)
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	response, err := self.base.RoundTrip(request)
	if err != nil {
		var closeErr error
		if response != nil && response.Body != nil {
			closeErr = closeSubstrateReadHttpBody(response.Body, self.base)
		}
		return nil, errors.Join(&substrateReadHttpTransportError{cause: err}, closeErr, request.Context().Err())
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("crv4: native HTTP response has no physical body")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.Join(&SubstrateReadHttpStatusError{status: response.StatusCode}, closeSubstrateReadHttpBody(response.Body, self.base), request.Context().Err())
	}
	if response.ContentLength > self.maximumBytes {
		return nil, errors.Join(errors.New("crv4: native HTTP response exceeds byte limit"), closeSubstrateReadHttpBody(response.Body, self.base), request.Context().Err())
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, self.maximumBytes+1))
	closeErr := closeSubstrateReadHttpBody(response.Body, self.base)
	var transportErr error
	if readErr != nil {
		transportErr = &substrateReadHttpTransportError{cause: readErr}
	} else if len(body) == 0 {
		transportErr = &substrateReadHttpTransportError{cause: io.EOF}
	}
	var sizeErr error
	if int64(len(body)) > self.maximumBytes {
		sizeErr = errors.New("crv4: native HTTP response exceeds byte limit")
	}
	if err := errors.Join(transportErr, sizeErr, closeErr, request.Context().Err()); err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Del("Content-Length")
	return response, nil
}

// Keep GSRPC's existing codecs and websocket lifecycle. Its public HTTP-client
// option is enough to preserve read causes without a dependency fork or globals.
func dialContextSubstrateClient(ctx context.Context, endpoint string) (*contextSubstrateClient, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	var transport *gsrpcgeth.Client
	var closeReadHttp func()
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		base := http.DefaultTransport
		if standard, ok := base.(*http.Transport); ok {
			owned := standard.Clone()
			base, closeReadHttp = owned, owned.CloseIdleConnections
		}
		client := &http.Client{Transport: &substrateReadHttpTransport{base: base, maximumBytes: substrateReadHttpResponseLimit}}
		transport, err = gsrpcgeth.DialHTTPWithClient(endpoint, client)
	} else {
		transport, err = gsrpcgeth.DialContext(ctx, endpoint)
	}
	if err != nil {
		if closeReadHttp != nil {
			closeReadHttp()
		}
		return nil, err
	}
	return &contextSubstrateClient{Client: transport, url: endpoint, closeReadHttp: closeReadHttp}, nil
}
