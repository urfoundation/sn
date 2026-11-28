//go:build linux || darwin

// Request body encoding between the upload writer and an api that may or may
// not take zstd: what an advertisement allows, what an instance's refusal
// undoes, and what the reserved consent signs. Real loopback HTTP; the test
// server decodes and checks every object the way the api does.
package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/urfoundation/sn/v2026/protocol"
)

// One upload as the test api saw it.
type attemptUploadEncodingRequest struct {
	encoding      string
	decodedLength string
	wireBytes     int
	consent       string
}

// An api that decodes zstd bodies, checks each object against its hash, and
// answers each request in turn with advertise and refuse.
type attemptUploadEncodingServer struct {
	endpoint  *httptest.Server
	stateLock sync.Mutex
	requests  []attemptUploadEncodingRequest
	// the Accept-Encoding of an acknowledgement; empty sends none
	advertise func(request int) string
	// the answer to an encoded body, 0 to take it
	refuse func(request int) int
}

func newAttemptUploadEncodingServer(t *testing.T, advertise func(int) string, refuse func(int) int) *attemptUploadEncodingServer {
	t.Helper()
	self := &attemptUploadEncodingServer{advertise: advertise, refuse: refuse}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(decoder.Close)
	self.endpoint = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		index := func() int {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.requests = append(self.requests, attemptUploadEncodingRequest{encoding: r.Header.Get("Content-Encoding"), decodedLength: r.Header.Get(attemptUploadDecodedLengthHeader), wireBytes: len(wire), consent: r.Header.Get(protocol.ValidatorAttemptUploadHeader)})
			return len(self.requests) - 1
		}()
		object := wire
		if encoding := r.Header.Get("Content-Encoding"); encoding != "" {
			if status := self.refuse(index); status != 0 {
				w.WriteHeader(status)
				return
			}
			length, err := strconv.Atoi(r.Header.Get(attemptUploadDecodedLengthHeader))
			if encoding != "zstd" || err != nil {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			object, err = decoder.DecodeAll(wire, nil)
			if err != nil || len(object) != length {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		if attemptHex32(sha256.Sum256(object)) != r.URL.Query().Get("hash") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if advertisement := self.advertise(index); advertisement != "" {
			w.Header().Set("Accept-Encoding", advertisement)
		}
		w.Header().Set("ETag", `"`+r.URL.Query().Get("hash")+`"`)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(self.endpoint.Close)
	return self
}

func (self *attemptUploadEncodingServer) seen() []attemptUploadEncodingRequest {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]attemptUploadEncodingRequest(nil), self.requests...)
}

// NDJSON records of about size bytes, shaped like a records chunk: they
// compress well.
func attemptUploadEncodingRecords(size int) []byte {
	var records bytes.Buffer
	for index := 0; records.Len() < size; index++ {
		records.WriteString(`{"sequence":` + strconv.Itoa(index) + `,"disposition":"complete","assignments":[{"trail":["0x00","0x01"],"binding":{"fleet_id":"0x0000000000000000000000000000000000000000000000000000000000000000"}}]}` + "\n")
	}
	return records.Bytes()
}

func attemptUploadEncodingWriter(t *testing.T, origin string) *HTTPAttemptStreamV2Writer {
	t.Helper()
	bounds, _ := attemptReplayV2TestBounds()
	bounds.Records.MaxChunkBytes = 4 * 1024 * 1024
	writer, err := NewHTTPAttemptStreamV2Writer(origin, bounds, func() string { return "synthetic-session" })
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

// Nothing goes compressed until the origin says it takes zstd; after that a
// compressible object goes as zstd, with its own length beside it, and the api
// decodes exactly the object its hash names.
func TestAttemptUploadSendsZstdOnlyAfterAnAdvertisement(t *testing.T) {
	t.Parallel()
	server := newAttemptUploadEncodingServer(t, func(int) string { return "zstd" }, func(int) int { return 0 })
	writer := attemptUploadEncodingWriter(t, server.endpoint.URL)
	data := attemptUploadEncodingRecords(256 * 1024)
	hash := attemptHex32(sha256.Sum256(data))
	for range 2 {
		if err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data); err != nil {
			t.Fatal(err)
		}
	}
	seen := server.seen()
	if len(seen) != 2 || seen[0].encoding != "" || seen[0].wireBytes != len(data) {
		t.Fatalf("the first upload went encoded before any advertisement: %+v", seen)
	}
	if seen[1].encoding != "zstd" || seen[1].decodedLength != strconv.Itoa(len(data)) || seen[1].wireBytes*4 > len(data) {
		t.Fatalf("the second upload did not go as compact zstd: %+v of %d bytes", seen[1], len(data))
	}
}

// An object zstd would not shrink goes as it is, advertisement or not.
func TestAttemptUploadKeepsIncompressibleObjectsAsTheyAre(t *testing.T) {
	t.Parallel()
	server := newAttemptUploadEncodingServer(t, func(int) string { return "zstd" }, func(int) int { return 0 })
	writer := attemptUploadEncodingWriter(t, server.endpoint.URL)
	// deterministic bytes no coder shrinks, seeded for a reproducible test
	random := rand.New(rand.NewPCG(1, 2))
	noise := make([]byte, 64*1024)
	for index := range noise {
		noise[index] = byte(random.Uint32())
	}
	for range 2 {
		if err := writer.Write(t.Context(), AttemptStreamV2Records, attemptHex32(sha256.Sum256(noise)), noise); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range server.seen() {
		if request.encoding != "" || request.wireBytes != len(noise) {
			t.Fatalf("an incompressible object went encoded: %+v", request)
		}
	}
}

// An instance that does not take zstd (400 before it learned it, 415 after)
// gets the same object as it is at once, in the same write, and the origin is
// asked again only after another advertisement.
func TestAttemptUploadFallsBackWhenAnInstanceRefusesTheEncoding(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadRequest, http.StatusUnsupportedMediaType} {
		server := newAttemptUploadEncodingServer(t, func(request int) string {
			if request == 0 || request == 3 {
				return "zstd"
			}
			return ""
		}, func(int) int { return status })
		writer := attemptUploadEncodingWriter(t, server.endpoint.URL)
		data := attemptUploadEncodingRecords(256 * 1024)
		hash := attemptHex32(sha256.Sum256(data))
		for range 3 {
			if err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data); err != nil {
				t.Fatalf("status %d: %v", status, err)
			}
		}
		seen := server.seen()
		encodings := []string{}
		for _, request := range seen {
			encodings = append(encodings, request.encoding)
		}
		// learn, refused, again as it is, then as it is until advertised again
		if strings.Join(encodings, ",") != ",zstd,," {
			t.Fatalf("status %d: encodings %q", status, encodings)
		}
	}
}

// q=0 names a coding to refuse; an acknowledgement without the header takes
// back an earlier advertisement.
func TestAttemptUploadAdvertisementReadsQuality(t *testing.T) {
	t.Parallel()
	writer := attemptUploadEncodingWriter(t, "https://api.example")
	for _, advertisement := range []struct {
		header   []string
		accepted bool
	}{
		{header: []string{"zstd"}, accepted: true},
		{header: []string{"gzip, zstd;q=0.5"}, accepted: true},
		{header: []string{"ZSTD"}, accepted: true},
		{header: []string{"zstd;q=0"}, accepted: false},
		{header: []string{"zstd; q=0.000"}, accepted: false},
		{header: []string{"gzip", "br"}, accepted: false},
		{header: nil, accepted: false},
	} {
		writer.learnEncodings(http.Header{"Accept-Encoding": advertisement.header})
		if writer.encoding.zstdAccepted.Load() != advertisement.accepted {
			t.Fatalf("Accept-Encoding %q read as %t", advertisement.header, !advertisement.accepted)
		}
	}
}

// The setting turns compression off whatever the origin advertises.
func TestAttemptUploadCompressionSettingTurnsItOff(t *testing.T) {
	t.Parallel()
	server := newAttemptUploadEncodingServer(t, func(int) string { return "zstd" }, func(int) int { return 0 })
	writer := attemptUploadEncodingWriter(t, server.endpoint.URL)
	writer.settings.Compress = false
	data := attemptUploadEncodingRecords(256 * 1024)
	hash := attemptHex32(sha256.Sum256(data))
	for range 2 {
		if err := writer.Write(t.Context(), AttemptStreamV2Records, hash, data); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range server.seen() {
		if request.encoding != "" {
			t.Fatalf("compression off sent %+v", request)
		}
	}
}

// A reserved upload's consent signs the object's own length, not its length on
// the wire, so the api's admission and budget count the bytes it stores.
func TestAttemptUploadReservedConsentSignsTheDecodedLength(t *testing.T) {
	t.Parallel()
	server := newAttemptUploadEncodingServer(t, func(int) string { return "zstd" }, func(int) int { return 0 })
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusNoContent, "" })
	writer := attemptUploadEncodingWriter(t, server.endpoint.URL)
	data := attemptUploadEncodingRecords(256 * 1024)
	hash := attemptHex32(sha256.Sum256(data))
	for range 2 {
		if err := writer.write(t.Context(), AttemptStreamV2Records, hash, data, fixture.signer); err != nil {
			t.Fatal(err)
		}
	}
	seen := server.seen()
	if len(seen) != 2 || seen[1].encoding != "zstd" {
		t.Fatalf("the reserved upload did not go encoded: %+v", seen)
	}
	for _, request := range seen {
		intent, err := protocol.VerifyValidatorAttemptUploadHeader(request.consent)
		if err != nil || intent.Size != uint64(len(data)) {
			t.Fatalf("consent for %d wire bytes signs %d, want %d: %v", request.wireBytes, intent.Size, len(data), err)
		}
	}
}

// The stored-object check asks for the object's head, and learns an
// advertisement from it. An api with no heads (405) answers the same in a
// GET's head instead.
func TestAttemptUploadStoredObjectCheckAsksForTheHead(t *testing.T) {
	t.Parallel()
	data := []byte("{\"object\":\"head\"}\n")
	for _, headUnsupported := range []bool{false, true} {
		var fixture *releaseUploadRefusalFixture
		fixture = newReleaseUploadRefusalFixture(t, func(int) (int, string) {
			stored := bytes.Clone(data)
			fixture.stored.Store(&stored)
			return http.StatusBadGateway, ""
		})
		fixture.headUnsupported.Store(headUnsupported)
		var stateLock sync.Mutex
		methods := []string{}
		fixture.owner.writer.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
			func() {
				stateLock.Lock()
				defer stateLock.Unlock()
				methods = append(methods, request.Method)
			}()
			response, err := fixture.roundTrip(request)
			if response != nil && request.Method == http.MethodHead && response.StatusCode == http.StatusOK {
				response.Header.Set("Accept-Encoding", "zstd")
			}
			return response, err
		})
		if err := fixture.write(t.Context(), data); err != nil {
			t.Fatalf("heads unsupported %t: %v", headUnsupported, err)
		}
		want := "POST,HEAD"
		if headUnsupported {
			want = "POST,HEAD,GET"
		}
		if strings.Join(methods, ",") != want || fixture.requests() != 1 {
			t.Fatalf("heads unsupported %t: methods %q, want %q", headUnsupported, methods, want)
		}
		if fixture.owner.writer.encoding.zstdAccepted.Load() == headUnsupported {
			t.Fatalf("heads unsupported %t: the head's advertisement was not learned", headUnsupported)
		}
	}
}

// A head that answers another length, or another type, is not the object.
func TestAttemptUploadStoredObjectCheckReadsTheHeadExactly(t *testing.T) {
	t.Parallel()
	data := []byte("{\"object\":\"exact\"}\n")
	for _, answer := range []func(*http.Response){
		func(response *http.Response) { response.ContentLength++ },
		func(response *http.Response) { response.Header.Set("Content-Type", "text/plain") },
		func(response *http.Response) { response.Header.Set("Content-Encoding", "zstd") },
		func(response *http.Response) { response.StatusCode = http.StatusNotFound },
		func(response *http.Response) { response.StatusCode = http.StatusInternalServerError },
	} {
		var fixture *releaseUploadRefusalFixture
		fixture = newReleaseUploadRefusalFixture(t, func(request int) (int, string) {
			if request == 0 {
				stored := bytes.Clone(data)
				fixture.stored.Store(&stored)
				return http.StatusBadGateway, ""
			}
			return http.StatusNoContent, ""
		})
		fixture.owner.writer.client.Transport = attemptStreamV2HTTPTestTransport(func(request *http.Request) (*http.Response, error) {
			response, err := fixture.roundTrip(request)
			if response != nil && request.Method == http.MethodHead {
				answer(response)
			}
			return response, err
		})
		if err := fixture.write(t.Context(), data); err != nil {
			t.Fatal(err)
		}
		if fixture.requests() != 2 {
			t.Fatalf("an inexact head ended the retry: requests=%d", fixture.requests())
		}
	}
}

// Heads and encoded uploads keep the caller's cancellation.
func TestAttemptUploadStoredObjectCheckEndsWithItsCaller(t *testing.T) {
	t.Parallel()
	fixture := newReleaseUploadRefusalFixture(t, func(int) (int, string) { return http.StatusNoContent, "" })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := fixture.owner.writer.head(ctx, "metadata", attemptHex32(sha256.Sum256([]byte("{}\n"))), 3); got != attemptObjectAbsent || fixture.probes.Load() != 0 {
		t.Fatalf("a canceled head answered %d after %d probes", got, fixture.probes.Load())
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("context was not canceled")
	}
}
