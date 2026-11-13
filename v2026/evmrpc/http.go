// Admit and release the physical HTTP body before geth can decode, retain a
// status diagnostic or drain bytes after a successful JSON prefix.
package evmrpc

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/rpc"
)

// A transport bound, independent of chain validity. Fleet clients also carry
// native metadata/events: allow one 16 MiB field after hex expansion. Batches
// share the aggregate bound; larger read pages must be split by their owner.
const maximumResponseBytes int64 = (2 * 16 * 1024 * 1024) + (64 * 1024) + 2

// Resource refusal cannot be mistaken for a JSON-RPC revert or known send.
var ErrResponseLimit = errors.New("EVM HTTP response exceeds byte limit")

// Immutable after construction; safe for concurrent requests when base is.
// The smaller test limit exercises exact boundaries without global mutation.
type responseTransport struct {
	base         http.RoundTripper
	maximumBytes int64
}

// Acquire, bound and close exactly one physical response; never retry or
// redirect a request, especially one carrying an existing signed transaction.
func (self *responseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if self == nil || self.base == nil || self.maximumBytes <= 0 || self.maximumBytes > maximumResponseBytes || request == nil {
		return nil, errors.New("EVM HTTP response admission is unavailable")
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	// Request gzip explicitly so DefaultTransport leaves its encoded body
	// intact. This owner can then bound both wire and expanded bytes.
	physicalRequest := request.Clone(request.Context())
	if physicalRequest.Header.Get("Accept-Encoding") == "" {
		physicalRequest.Header.Set("Accept-Encoding", "gzip")
	}
	response, err := self.base.RoundTrip(physicalRequest)
	if err != nil {
		if response != nil && response.Body != nil {
			err = errors.Join(err, response.Body.Close())
		}
		return nil, errors.Join(err, request.Context().Err())
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("EVM HTTP response has no body")
	}
	encoded, err := self.admit(request, response)
	if err != nil {
		return nil, err
	}
	response.Header = response.Header.Clone()
	response.Uncompressed = response.Uncompressed || strings.EqualFold(strings.TrimSpace(response.Header.Get("Content-Encoding")), "gzip")
	response.Header.Del("Content-Length")
	response.Header.Del("Content-Encoding")
	response.ContentLength = int64(len(encoded))
	response.Body = io.NopCloser(bytes.NewReader(encoded))
	return response, nil
}

// Own the actual body until complete framing, physical release and caller
// cancellation have all been checked. Only admitted bytes reach geth cleanup.
func (self *responseTransport) admit(request *http.Request, response *http.Response) (encoded []byte, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, response.Body.Close(), request.Context().Err())
		if resultErr != nil {
			encoded = nil
		}
	}()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// Retain the typed status for existing read retry classification, but
		// never retain an untrusted diagnostic or follow a signed POST redirect.
		return nil, rpc.HTTPError{StatusCode: response.StatusCode, Status: fmt.Sprintf("%d %s", response.StatusCode, http.StatusText(response.StatusCode))}
	}
	if response.ContentLength > self.maximumBytes {
		return nil, fmt.Errorf("%w: content length %d, limit %d", ErrResponseLimit, response.ContentLength, self.maximumBytes)
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	wire := &io.LimitedReader{R: response.Body, N: self.maximumBytes + 1}
	var reader io.Reader = wire
	var compressed *gzip.Reader
	encoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	switch {
	case encoding == "", strings.EqualFold(encoding, "identity"):
	case strings.EqualFold(encoding, "gzip"):
		// Both compressed bytes and expanded bytes have independent caps.
		var err error
		compressed, err = gzip.NewReader(wire)
		if err != nil {
			if wire.N == 0 {
				err = errors.Join(err, ErrResponseLimit)
			}
			return nil, fmt.Errorf("decode EVM HTTP gzip header: %w", err)
		}
		reader = compressed
	default:
		return nil, errors.New("EVM HTTP response content encoding is unsupported")
	}
	encoded, resultErr = io.ReadAll(io.LimitReader(reader, self.maximumBytes+1))
	if compressed != nil {
		resultErr = errors.Join(resultErr, compressed.Close())
	}
	if int64(len(encoded)) > self.maximumBytes || wire.N == 0 {
		resultErr = errors.Join(resultErr, ErrResponseLimit)
	}
	if resultErr != nil {
		return nil, resultErr
	}
	if err := validateEnvelope(request, encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}
