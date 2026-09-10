// Bound public HTTP replies before geth allocates JSON values, including error
// bodies and decompressed payloads. This transport does not retry or truncate.
package validator

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Transport admission policy, not an on-chain cardinality or evidence limit.
// Fixed release replies need at most 22 ABI words per view, batches contain at
// most 50 views, and runtime code needs 48 KiB of hex. Four MiB also leaves room
// for ordinary headers/receipts/log pages; larger variable replies fail closed.
// The same ceiling already bounds simulator public read-only RPC responses.
const chainHTTPResponseLimit int64 = 4 * 1024 * 1024

var errChainHTTPResponseTooLarge = errors.New("EVM HTTP response exceeds byte limit")

// Immutable after construction and safe for concurrent calls when base is.
// Tests may select a smaller limit without changing a global transport owner.
type chainHTTPTransport struct {
	base             http.RoundTripper
	maxResponseBytes int64
}

// Own and close the actual response before handing a finite byte reader to
// geth. Its success decoder and non-2xx error reader otherwise read unbounded
// bodies, and its cleanup drains whatever remains after the first JSON value.
func (self *chainHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if self == nil || request == nil || self.base == nil || self.maxResponseBytes <= 0 || self.maxResponseBytes > chainHTTPResponseLimit {
		return nil, errors.New("bounded EVM HTTP transport is unavailable")
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	response, err := self.base.RoundTrip(request)
	if err != nil {
		if response != nil && response.Body != nil {
			err = errors.Join(err, response.Body.Close())
		}
		return nil, errors.Join(err, request.Context().Err())
	}
	if response == nil || response.Body == nil {
		return nil, errors.New("EVM HTTP response has no body")
	}
	refuse := func(err error) (*http.Response, error) {
		return nil, errors.Join(err, response.Body.Close(), request.Context().Err())
	}
	if response.ContentLength > self.maxResponseBytes {
		return refuse(fmt.Errorf("%w: content length %d, limit %d", errChainHTTPResponseTooLarge, response.ContentLength, self.maxResponseBytes))
	}
	if err := request.Context().Err(); err != nil {
		return refuse(err)
	}
	var reader io.Reader = response.Body
	var compressed *gzip.Reader
	encoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	switch {
	case encoding == "", strings.EqualFold(encoding, "identity"):
	case strings.EqualFold(encoding, "gzip"):
		// DefaultTransport normally decompresses gzip itself. Also handle an
		// explicit encoding from a transport with automatic decoding disabled.
		compressed, err = gzip.NewReader(io.LimitReader(response.Body, self.maxResponseBytes+1))
		if err != nil {
			return refuse(fmt.Errorf("decode EVM HTTP gzip header: %w", err))
		}
		reader = compressed
	default:
		return refuse(errors.New("EVM HTTP response has an unsupported content encoding"))
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, self.maxResponseBytes+1))
	if compressed != nil {
		readErr = errors.Join(readErr, compressed.Close())
	}
	closeErr := response.Body.Close()
	if int64(len(body)) > self.maxResponseBytes {
		readErr = errors.Join(readErr, fmt.Errorf("%w: limit %d", errChainHTTPResponseTooLarge, self.maxResponseBytes))
	}
	if err := errors.Join(readErr, closeErr, request.Context().Err()); err != nil {
		return nil, err
	}
	if compressed != nil {
		response.Header.Del("Content-Encoding")
		response.Uncompressed = true
	}
	response.Header.Del("Content-Length")
	response.ContentLength = int64(len(body))
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}
