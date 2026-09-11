// Typed uploads use the release owner's refreshed API client session, never
// operator artifact keys or MinIO credentials. A successful write is staging
// only; the existing public readers independently replay both replicas.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/urfoundation/sn/protocol"
)

// Safe for concurrent calls with independently owned immutable input bytes.
// The getter must be the release API session's live getter, not a startup JWT
// snapshot; shutdown/logout must make it refuse new uploads.
type HTTPAttemptStreamV2Writer struct {
	endpoint      url.URL
	metadataBytes uint64
	recordBytes   uint64
	proofBytes    uint64
	byJwt         func() string
	client        *http.Client
}

// Reuses typed reader origin/bound admission and its non-redirecting client.
// Credentials require TLS except literal loopback HTTP in the local simulator.
func NewHTTPAttemptStreamV2Writer(origin string, bounds AttemptCutV2Bounds, byJwt func() string) (*HTTPAttemptStreamV2Writer, error) {
	return newHttpAttemptStreamV2Writer(origin, bounds, attemptStreamV2MetadataBytes(bounds), byJwt)
}

// Typed payload admission is explicit; the public stream constructor keeps its
// original page allowance, credential policy and bounded acknowledgement owner.
func newHttpAttemptStreamV2Writer(origin string, bounds AttemptCutV2Bounds, metadataBytes uint64, byJwt func() string) (*HTTPAttemptStreamV2Writer, error) {
	reader, err := newHttpAttemptStreamV2Reader(origin, bounds, metadataBytes)
	if err != nil {
		return nil, err
	}
	if byJwt == nil {
		return nil, errors.New("attempt upload client session is missing")
	}
	if reader.endpoint.Scheme == "http" {
		address := net.ParseIP(reader.endpoint.Hostname())
		if address == nil || !address.IsLoopback() {
			return nil, errors.New("attempt upload credentials require TLS or literal loopback HTTP")
		}
	}
	return &HTTPAttemptStreamV2Writer{endpoint: reader.endpoint, metadataBytes: reader.metadataBytes, recordBytes: reader.recordBytes, proofBytes: reader.proofBytes, byJwt: byJwt, client: reader.client}, nil
}

// Owns and checks exact bytes before calling the live credential getter.
// No redirect, retry, transformed body or partial acknowledgement is accepted.
// Idempotent object retry is an outer lifecycle decision with a new budgeted
// request; it is not an excuse to hide a refusal or uncertain server result.
func (self *HTTPAttemptStreamV2Writer) Write(ctx context.Context, kind, contentHash string, data []byte) (resultErr error) {
	return self.write(ctx, kind, contentHash, data, nil)
}

// Reserved and ordinary paths share identical bounded body, cancellation and
// immutable acknowledgement handling. Only the fixed VPK consent is added.
func (self *HTTPAttemptStreamV2Writer) write(ctx context.Context, kind, contentHash string, data []byte, signer *validatorAttemptUploadSigner) (resultErr error) {
	if ctx == nil || self == nil || self.byJwt == nil || self.client == nil {
		return errors.New("attempt upload owner is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, ctx.Err()) }()
	limit, contentType := self.metadataBytes, "application/json"
	switch kind {
	case "metadata":
	case AttemptStreamV2Records:
		limit, contentType = self.recordBytes, "application/x-ndjson"
	case AttemptStreamV2Proofs:
		limit, contentType = self.proofBytes, "application/x-ndjson"
	default:
		return errors.New("attempt upload kind is unsupported")
	}
	expected, err := canonicalAttemptHex32("attempt upload hash", contentHash, false)
	if err != nil {
		return err
	}
	if len(data) == 0 || uint64(len(data)) > limit {
		return errors.New("attempt upload exceeds its typed byte bound")
	}
	owned := bytes.Clone(data)
	if sha256.Sum256(owned) != expected {
		return errors.New("attempt upload bytes differ from their content hash")
	}
	credential := self.byJwt()
	if credential == "" || len(credential) > 16*1024 || strings.TrimSpace(credential) != credential || strings.ContainsAny(credential, "\r\n") {
		return errors.New("attempt upload client session is unavailable or invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	endpoint := self.endpoint
	endpoint.RawQuery = url.Values{"kind": {kind}, "hash": {contentHash}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(owned))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept-Encoding", "identity")
	if signer != nil {
		header, err := signer.header(credential, kind, expected, uint64(len(owned)))
		if err != nil {
			return err
		}
		request.Header.Set(protocol.ValidatorAttemptUploadHeader, header)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := self.client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			err = errors.Join(err, response.Body.Close())
		}
		return err
	}
	if response == nil || response.Body == nil {
		return errors.New("attempt upload acknowledgement is absent")
	}
	defer func() { resultErr = errors.Join(resultErr, response.Body.Close()) }()
	if response.StatusCode != http.StatusNoContent {
		// The existing request owns this bounded diagnostic read. Never retry
		// an ambiguous write or include headers in its error message.
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1024))
		detail := strings.TrimSpace(string(raw))
		// Suppress even a truncated reflection of this request's session.
		if strings.Contains(detail, credential[:min(len(credential), 32)]) ||
			strings.Contains(strings.ToLower(detail), "bearer ") {
			detail = "[redacted client session]"
		}
		if detail == "" {
			return errors.Join(fmt.Errorf("attempt upload response status is %d", response.StatusCode), readErr)
		}
		return errors.Join(fmt.Errorf("attempt upload response status is %d: %q", response.StatusCode, detail), readErr)
	}
	if len(response.Header.Values("ETag")) != 1 || response.Header.Get("ETag") != `"`+contentHash+`"` || response.ContentLength > 0 || response.Uncompressed || len(response.Header.Values("Content-Encoding")) != 0 || len(response.Header.Values("Content-Range")) != 0 {
		return errors.New("attempt upload acknowledgement differs from exact immutable object")
	}
	acknowledgement, err := io.ReadAll(io.LimitReader(response.Body, 1))
	if err != nil || len(acknowledgement) != 0 {
		return errors.Join(errors.New("attempt upload acknowledgement contains unexpected bytes"), err)
	}
	return nil
}
