// Typed evidence retry classification retains replica ownership without
// promoting local decoding failures or remote diagnostic text into authority.
package validator

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Only actual transport and capacity leaves survive complete-tree admission.
func TestRetryableEvidenceTransportRetainsStrictMixedFailureScope(t *testing.T) {
	t.Parallel()
	capacity := &attemptStreamHttpStatusError{status: 429}
	for _, err := range []error{
		context.DeadlineExceeded, capacity,
		net.ErrClosed, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET},
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		syscall.EPIPE, syscall.ETIMEDOUT,
		gethrpc.HTTPError{StatusCode: 429}, &gethrpc.HTTPError{StatusCode: 503},
		&url.Error{Op: "Get", URL: "https://replica.example", Err: io.ErrUnexpectedEOF},
		&attemptReplicaPublicationError{causes: []error{capacity, context.Canceled}},
		&attemptReplicaPublicationError{causes: []error{capacity, io.EOF, context.Canceled}},
		&attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, &attemptStreamHTTPIncompleteError{}}},
	} {
		if !RetryableEvidenceTransportError(err) {
			t.Errorf("actual transient failure refused: %v", err)
		}
	}
	for _, err := range []error{
		io.EOF, io.ErrUnexpectedEOF, context.Canceled,
		&os.PathError{Op: "read", Path: "synthetic.json", Err: context.DeadlineExceeded},
		errors.New("content hash differs: connection reset"),
		&attemptStreamHttpStatusError{status: 403, detail: "connection reset"},
		gethrpc.HTTPError{StatusCode: 403, Body: []byte("connection reset")},
		&gethrpc.HTTPError{StatusCode: 403, Body: []byte("connection reset")},
		errors.Join(capacity, errors.New("content hash differs")),
		&attemptReplicaPublicationError{causes: []error{capacity, errors.New("bad signature"), context.Canceled}},
		&attemptReplicaPublicationError{causes: []error{context.Canceled}},
		&attemptReplicaPublicationError{causes: []error{io.EOF, context.Canceled}},
	} {
		if RetryableEvidenceTransportError(err) {
			t.Errorf("hard or unowned failure admitted: %v", err)
		}
	}
}

// Only the replica owner's cancellations are neutral. An independently canceled
// parent or an integrity failure remains outside that recovery boundary.
func TestReplicaPublicationJoinPreservesParentAndIntegrityFailures(t *testing.T) {
	t.Parallel()
	capacity := &attemptStreamHttpStatusError{status: 429}
	for _, test := range []struct {
		parent error
		causes []error
		retry  bool
	}{
		{causes: []error{capacity, context.Canceled}, retry: true},
		{parent: context.Canceled, causes: []error{capacity, context.Canceled}, retry: false},
		{causes: []error{capacity, errors.New("signed payload differs"), context.Canceled}, retry: false},
		{causes: []error{io.EOF, context.Canceled}, retry: false},
	} {
		joined := joinReplicaPublicationErrors(test.parent, test.causes)
		if RetryableEvidenceTransportError(joined) != test.retry {
			t.Fatalf("replica cancellation scope changed: %v", joined)
		}
	}
	if err := joinReplicaPublicationErrors(nil, []error{nil, nil}); err != nil {
		t.Fatal("successful replicas manufactured an error", err)
	}
}
