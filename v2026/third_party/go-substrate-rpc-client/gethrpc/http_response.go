// HTTP response admission is shared by ordinary calls, notifications and
// batches, independently of a caller's higher-level native read/retry policy.
package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Preserve the native event ceiling: 16 MiB raw bytes use twice that on the
// JSON hex wire, plus 0x and a finite envelope. Metadata's 8 MiB raw ceiling
// and the existing bounded native read adapter fit within this response bound.
// The cap covers an aggregate batch response too; it never scales with batch
// cardinality. An owner must split larger read batches before issuing them.
const maximumHttpResponseBytes int64 = (32 * 1024 * 1024) + (64 * 1024) + 2

// This is local response admission, never proof of a transient RPC failure.
var ErrHttpResponseLimit = errors.New("HTTP RPC response exceeds byte limit")

// Own and close the physical body exactly once before any JSON result can be
// published. Read at most one byte beyond the ceiling, including automatic
// HTTP decompression and unknown/chunked lengths. A valid prefix cannot hide
// a late body, close or cancellation failure. Status errors do not read bodies.
func readHttpResponse(ctx context.Context, response *http.Response, maximumBytes int64) (body []byte, err error) {
	if response == nil || response.Body == nil {
		return nil, errors.New("HTTP RPC response has no body")
	}
	defer func() {
		err = errors.Join(err, response.Body.Close(), ctx.Err())
		if err != nil {
			body = nil
		}
	}()
	if maximumBytes <= 0 || maximumBytes > maximumHttpResponseBytes {
		return nil, errors.New("invalid HTTP RPC response byte limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP RPC status %d", response.StatusCode)
	}
	if response.ContentLength > maximumBytes {
		return nil, ErrHttpResponseLimit
	}
	body, err = io.ReadAll(io.LimitReader(response.Body, maximumBytes+1))
	if int64(len(body)) > maximumBytes {
		err = errors.Join(err, ErrHttpResponseLimit)
	}
	return body, err
}
