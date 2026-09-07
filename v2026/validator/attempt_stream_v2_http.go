package validator

// Public typed transport preserves the signed stream's exact byte identity.
// It does not authenticate records, activate v2, publish objects or establish
// replication; callers still run the complete policy-aware cut replay.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"
)

// Safe for concurrent requests. The origin and limits must come from trusted
// deployment configuration, not from an unverified artifact or redirect.
type HTTPAttemptStreamV2Reader struct {
	endpoint      url.URL
	metadataBytes uint64
	recordBytes   uint64
	proofBytes    uint64
	client        *http.Client
}

// Uses a dedicated typed endpoint; generic evidence object limits are unchanged.
// Loopback HTTP is supported for the explicitly configured local testnet origins.
func NewHTTPAttemptStreamV2Reader(origin string, bounds AttemptCutV2Bounds) (*HTTPAttemptStreamV2Reader, error) {
	endpoint, err := url.Parse(origin)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Opaque != "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		(endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawPath != "" ||
		endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.RawFragment != "" {
		return nil, errors.New("attempt stream HTTP origin must be a bare HTTP(S) origin without credentials")
	}
	if err := bounds.Records.Validate(); err != nil {
		return nil, err
	}
	if err := bounds.Proofs.Validate(); err != nil {
		return nil, err
	}
	metadataBytes := max(bounds.Records.MaxManifestBytes, bounds.Records.MaxPageBytes,
		bounds.Proofs.MaxManifestBytes, bounds.Proofs.MaxPageBytes)
	maximumInt := uint64(^uint(0) >> 1)
	if metadataBytes > maximumInt/2 || bounds.Records.MaxChunkBytes >= uint64(1<<63-1) || bounds.Proofs.MaxChunkBytes >= uint64(1<<63-1) {
		return nil, errors.New("attempt stream HTTP bounds overflow bounded reads")
	}
	endpoint.Path = "/sn/attempt-artifact"
	return &HTTPAttemptStreamV2Reader{
		endpoint: *endpoint, metadataBytes: metadataBytes,
		recordBytes: bounds.Records.MaxChunkBytes, proofBytes: bounds.Proofs.MaxChunkBytes,
		client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

// Owns one bounded metadata object. Even a late close or cancellation failure
// clears the bytes, so callers cannot accidentally publish partial authority.
func (self *HTTPAttemptStreamV2Reader) ReadMetadata(ctx context.Context, contentHash string, size uint64) (value []byte, resultErr error) {
	reader, err := self.open(ctx, "metadata", contentHash, size, self.metadataBytes)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, reader.Close(), ctx.Err())
		if resultErr != nil {
			value = nil
		}
	}()
	return io.ReadAll(reader)
}

// Transfers one response body to the caller; EOF proves its hash and length,
// and Close is mandatory on every path. No whole data chunk is buffered here.
func (self *HTTPAttemptStreamV2Reader) OpenData(ctx context.Context, kind, contentHash string, size uint64) (io.ReadCloser, error) {
	var limit uint64
	switch kind {
	case AttemptStreamV2Records:
		limit = self.recordBytes
	case AttemptStreamV2Proofs:
		limit = self.proofBytes
	default:
		return nil, errors.New("attempt stream HTTP data kind is unsupported")
	}
	return self.open(ctx, kind, contentHash, size, limit)
}

// Refuses aliases, oversized requests and cancellation before network effects.
// The body receives only the authenticated length plus one EOF-probe byte.
func (self *HTTPAttemptStreamV2Reader) open(ctx context.Context, kind, contentHash string, size, limit uint64) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, errors.New("attempt stream HTTP context is missing")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected, err := canonicalAttemptHex32("attempt stream HTTP content hash", contentHash, false)
	if err != nil {
		return nil, err
	}
	if size == 0 || size > limit {
		return nil, errors.New("attempt stream HTTP object exceeds its typed byte bound")
	}
	endpoint := self.endpoint
	endpoint.RawQuery = url.Values{"kind": {kind}, "hash": {contentHash}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	contentType := "application/x-ndjson"
	if kind == "metadata" {
		contentType = "application/json"
	}
	request.Header.Set("Accept", contentType)
	request.Header.Set("Accept-Encoding", "identity")
	response, err := self.client.Do(request)
	if err != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	refuse := func(cause error) (io.ReadCloser, error) {
		return nil, errors.Join(cause, response.Body.Close(), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return refuse(err)
	}
	if response.StatusCode != http.StatusOK {
		return refuse(fmt.Errorf("attempt stream HTTP response status is %d", response.StatusCode))
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || len(response.Header.Values("Content-Type")) != 1 || mediaType != contentType {
		return refuse(errors.New("attempt stream HTTP response has the wrong typed content"))
	}
	encoding := response.Header.Get("Content-Encoding")
	if response.Uncompressed || len(response.Header.Values("Content-Encoding")) > 1 || (encoding != "" && encoding != "identity") || response.Header.Get("Content-Range") != "" {
		return refuse(errors.New("attempt stream HTTP response is transformed or partial"))
	}
	if response.ContentLength >= 0 && uint64(response.ContentLength) != size {
		return refuse(errors.New("attempt stream HTTP content length differs from its authenticated size"))
	}
	return &attemptStreamV2HTTPBody{ctx: ctx, body: response.Body, remaining: size, expected: expected, digest: sha256.New()}, nil
}

// One caller owns Read/Close; methods are not concurrent. Byte/hash checks stay
// streaming, and no synthetic EOF can hide a trailing byte or transport error.
type attemptStreamV2HTTPBody struct {
	ctx        context.Context
	body       io.ReadCloser
	remaining  uint64
	expected   [32]byte
	digest     hash.Hash
	verified   bool
	closed     bool
	fault      error
	closeErr   error
	emptyReads int
}

// Retains the first failure; bytes returned with an error are never authority.
func (self *attemptStreamV2HTTPBody) Read(value []byte) (int, error) {
	if self.closed {
		return 0, errors.New("attempt stream HTTP body is closed")
	}
	if self.fault != nil {
		return 0, self.fault
	}
	if err := self.ctx.Err(); err != nil {
		self.fault = err
		return 0, err
	}
	if self.verified {
		return 0, io.EOF
	}
	if len(value) == 0 {
		return 0, nil
	}
	value = value[:min(uint64(len(value)), self.remaining+1)]
	count, readErr := self.body.Read(value)
	if count < 0 || count > len(value) || uint64(count) > self.remaining {
		self.fault = errors.New("attempt stream HTTP body exceeds its authenticated size")
		return 0, self.fault
	}
	self.remaining -= uint64(count)
	_, _ = self.digest.Write(value[:count])
	if err := self.ctx.Err(); err != nil {
		self.fault = errors.Join(readErr, err)
		return count, self.fault
	}
	if readErr == io.EOF {
		var actual [32]byte
		copy(actual[:], self.digest.Sum(nil))
		if self.remaining != 0 || actual != self.expected {
			self.fault = errors.New("attempt stream HTTP body differs from its authenticated size or hash")
			return count, self.fault
		}
		self.verified = true
		return count, io.EOF
	}
	if readErr != nil {
		self.fault = readErr
	} else if count == 0 {
		self.emptyReads++
		if self.emptyReads >= 100 {
			self.fault = io.ErrNoProgress
			return 0, self.fault
		}
	} else {
		self.emptyReads = 0
	}
	return count, readErr
}

// Closing early is a failure, never proof of a complete object. Underlying
// close errors and cancellation remain visible after successful EOF as well.
func (self *attemptStreamV2HTTPBody) Close() error {
	if self.closed {
		return self.closeErr
	}
	self.closed = true
	var incomplete error
	if !self.verified {
		incomplete = errors.New("attempt stream HTTP body closed before complete authenticated EOF")
	}
	self.closeErr = errors.Join(self.fault, incomplete, self.body.Close(), self.ctx.Err())
	return self.closeErr
}
