// Typed uploads use the release owner's refreshed API client session, never
// operator artifact keys or MinIO credentials. A successful write is staging
// only; the existing public readers independently replay both replicas.
//
// An origin that takes zstd request bodies says so with Accept-Encoding on its
// upload acknowledgements and object heads (RFC 7694). Until it does, objects
// go as they are. After it does, an object goes compressed when that is
// smaller, with its own length in X-Decoded-Content-Length; the api checks the
// hash and every bound against the decoded bytes. An instance that refuses the
// encoding (400 before it learned zstd, or 415) gets the object again as it
// is, and the origin is asked again only after another advertisement.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/urfoundation/sn/v2026/protocol"
)

// The request header that carries a compressed object's own length.
const attemptUploadDecodedLengthHeader = "X-Decoded-Content-Length"

// The one request body encoding an origin may advertise.
const attemptUploadEncodingZstd = "zstd"

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
	settings      AttemptUploadSettings
	encoding      *attemptUploadEncoding
}

// What the origin advertised for request bodies, and the encoder for them.
// Safe for concurrent use.
type attemptUploadEncoding struct {
	// whether the origin's last advertisement named zstd request bodies
	zstdAccepted atomic.Bool
	// built on the first compression; safe for concurrent EncodeAll
	encoderOnce sync.Once
	encoder     *zstd.Encoder
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
	// Stream readers pause their I/O budget while replay owns CPU work. An
	// upload instead owns one request and acknowledgement under a finite,
	// progress-measured budget even when its release context is long lived.
	// Keep expiry in the request context: Client.Timeout replaces the original
	// deadline with a private HTTP matcher that bounded capture must refuse.
	// Uploads use the HTTP/1.1 transport; the reader keeps HTTP/2.
	client := &http.Client{Transport: releaseApiUploadHttpTransport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return &HTTPAttemptStreamV2Writer{endpoint: reader.endpoint, metadataBytes: reader.metadataBytes, recordBytes: reader.recordBytes, proofBytes: reader.proofBytes, byJwt: byJwt, client: client, settings: DefaultAttemptUploadSettings(), encoding: &attemptUploadEncoding{}}, nil
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
	upload := attemptUploadRequest{kind: kind, contentHash: contentHash, contentType: contentType, expected: expected, object: owned, credential: credential, signer: signer}
	// zstd when the origin advertised it and that is smaller, or nil
	encoded := func() []byte {
		encoding := self.encoding
		if !self.settings.Compress || encoding == nil || !encoding.zstdAccepted.Load() {
			return nil
		}
		encoding.encoderOnce.Do(func() {
			// One goroutine per call keeps an encode inside its caller's budget.
			encoding.encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedDefault))
		})
		if encoding.encoder == nil {
			return nil
		}
		encoded := encoding.encoder.EncodeAll(owned, make([]byte, 0, len(owned)/2))
		if len(encoded) >= len(owned) {
			return nil
		}
		return encoded
	}()
	if encoded != nil {
		err := self.send(ctx, upload, encoded)
		if status := releaseUploadStatus(err); status == nil || status.status != http.StatusBadRequest && status.status != http.StatusUnsupportedMediaType {
			return err
		}
		// This instance does not take the encoding; ask again only after
		// another advertisement.
		self.encoding.zstdAccepted.Store(false)
	}
	return self.send(ctx, upload, nil)
}

// One object as the writer has checked it.
type attemptUploadRequest struct {
	kind, contentHash, contentType string
	expected                       [32]byte
	object                         []byte
	credential                     string
	signer                         *validatorAttemptUploadSigner
}

// Remembers what the origin's answer advertised for request bodies. An answer
// without the header is an instance that does not take an encoding.
func (self *HTTPAttemptStreamV2Writer) learnEncodings(header http.Header) {
	accepted := false
	for _, value := range header.Values("Accept-Encoding") {
		for _, item := range strings.Split(value, ",") {
			fields := strings.Split(item, ";")
			if !strings.EqualFold(strings.TrimSpace(fields[0]), attemptUploadEncodingZstd) {
				continue
			}
			// q=0 names a coding the origin refuses
			quality := 1.0
			for _, parameter := range fields[1:] {
				name, number, found := strings.Cut(strings.TrimSpace(parameter), "=")
				if found && strings.EqualFold(strings.TrimSpace(name), "q") {
					if parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64); err == nil {
						quality = parsed
					}
				}
			}
			accepted = 0 < quality
		}
	}
	if self.encoding != nil {
		self.encoding.zstdAccepted.Store(accepted)
	}
}

// One request for the object, as it is or as the given encoded bytes.
func (self *HTTPAttemptStreamV2Writer) send(ctx context.Context, upload attemptUploadRequest, encoded []byte) (resultErr error) {
	endpoint := self.endpoint
	endpoint.RawQuery = url.Values{"kind": {upload.kind}, "hash": {upload.contentHash}}.Encode()
	payload := upload.object
	if encoded != nil {
		payload = encoded
	}
	body := newAttemptUploadBody(payload)
	span := self.settings.requestCap(len(payload))
	if upload.signer != nil {
		// The api refuses a reserved upload whose signed consent expired
		// before its body arrived, so nothing is gained by sending past it.
		span = min(span, time.Duration(upload.signer.maximumIntentSeconds)*time.Second)
	}
	watch := watchAttemptUpload(ctx, body, span, self.settings)
	defer watch.close()
	requestCtx := watch.ctx
	diagnostic := newReleasePreparationHttpDiagnostic(requestCtx, releasePreparationHttpWrite)
	defer func() { diagnostic.finish(resultErr) }()
	// Observe the owner's own ending after the body closes and before it is
	// released. Whatever the torn-down connection reported, the answer is
	// this request's deadline, which the caller may retry. A status the api
	// or its load balancer answered keeps its own verdict.
	defer func() {
		if resultErr != nil && watch.expired() && releaseUploadStatus(resultErr) == nil {
			resultErr = watch.deadline(endpoint)
		}
	}()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(payload))
	request.GetBody = body.rewind(payload)
	request.Header.Set("Authorization", "Bearer "+upload.credential)
	request.Header.Set("Content-Type", upload.contentType)
	request.Header.Set("Accept-Encoding", "identity")
	if encoded != nil {
		request.Header.Set("Content-Encoding", attemptUploadEncodingZstd)
		request.Header.Set(attemptUploadDecodedLengthHeader, strconv.Itoa(len(upload.object)))
	}
	if upload.signer != nil {
		// The consent signs the object's own length, however it travels.
		header, err := upload.signer.header(upload.credential, upload.kind, upload.expected, uint64(len(upload.object)))
		if err != nil {
			return err
		}
		request.Header.Set(protocol.ValidatorAttemptUploadHeader, header)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := doReleaseApiHttpRequest(self.client, request)
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
		if strings.Contains(detail, upload.credential[:min(len(upload.credential), 32)]) ||
			strings.Contains(strings.ToLower(detail), "bearer ") {
			detail = "[redacted client session]"
		}
		return errors.Join(&attemptStreamHttpStatusError{status: response.StatusCode, upload: true, detail: detail, retryAfter: attemptStreamHttpRetryAfter(response.Header)}, readErr)
	}
	if len(response.Header.Values("ETag")) != 1 || response.Header.Get("ETag") != `"`+upload.contentHash+`"` || response.ContentLength > 0 || response.Uncompressed || len(response.Header.Values("Content-Encoding")) != 0 || len(response.Header.Values("Content-Range")) != 0 {
		return errors.New("attempt upload acknowledgement differs from exact immutable object")
	}
	acknowledgement, err := io.ReadAll(io.LimitReader(response.Body, 1))
	if err != nil || len(acknowledgement) != 0 {
		return errors.Join(errors.New("attempt upload acknowledgement contains unexpected bytes"), err)
	}
	self.learnEncodings(response.Header)
	return nil
}

// What a head of the object at its content address said.
type attemptObjectHead int

const (
	attemptObjectAbsent attemptObjectHead = iota
	attemptObjectStored
	// an api with no head of attempt objects (405 or 501)
	attemptObjectHeadUnsupported
)

// Asks the origin for the object's head: stored with its typed content and
// exact length, absent, or no answer for heads. Anything else is absent, and
// the object is sent again. The answer's advertisement is remembered.
func (self *HTTPAttemptStreamV2Writer) head(ctx context.Context, kind, contentHash string, size int) attemptObjectHead {
	if ctx.Err() != nil {
		return attemptObjectAbsent
	}
	contentType := "application/x-ndjson"
	if kind == "metadata" {
		contentType = "application/json"
	}
	endpoint := self.endpoint
	endpoint.RawQuery = url.Values{"kind": {kind}, "hash": {contentHash}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint.String(), nil)
	if err != nil {
		return attemptObjectAbsent
	}
	request.Header.Set("Accept", contentType)
	request.Header.Set("Accept-Encoding", "identity")
	response, err := doReleaseApiHttpRequest(self.client, request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return attemptObjectAbsent
	}
	if response == nil || response.Body == nil {
		return attemptObjectAbsent
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return attemptObjectHeadUnsupported
	default:
		return attemptObjectAbsent
	}
	self.learnEncodings(response.Header)
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != contentType || response.ContentLength != int64(size) || len(response.Header.Values("Content-Encoding")) != 0 {
		return attemptObjectAbsent
	}
	return attemptObjectStored
}
