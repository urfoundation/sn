//go:build linux || darwin

// Real SDK devices created by the production provider constructor exercise
// HTTPS enrollment, independently signed requests, retained cuts and teardown.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/urnetwork/connect/v2026"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"github.com/urnetwork/sdk/v2026"
)

// The HTTP custodian never has the request signing key. The test's independent
// window owner supplies requests only after verifying actual signed enrollment.
type providerWorkDeviceFixture struct {
	profile       ProviderWorkCaptureProfile
	seed          []byte
	profilePath   string
	profileSha256 string
	server        *httptest.Server
	owners        chan coreprotocol.OriginalWorkOwnerEnrollment
	requests      chan [][]byte
	requestReads  chan [16]byte
	requestClosed chan [16]byte
	cuts          chan []byte
	failures      chan error
	other         http.Handler
}

// Every route and trust root is local to one fixture and no external egress is
// allowed. Buffered channels observe real boundaries without scheduling polls.
func newProviderWorkDeviceFixture(t *testing.T) *providerWorkDeviceFixture {
	t.Helper()
	profile, seed := providerWorkCaptureFixture(t)
	// The stopped synthetic owner explicitly prepares fresh custody before any
	// SDK exists. Runtime startup cannot reinterpret an empty directory as birth.
	directory := profile.Providers[0].OutboxDirectory
	index, err := os.OpenFile(filepath.Join(directory, connect.OriginalWorkOutboxIndexName), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(index.Sync(), index.Close()); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := connect.BuildFreshOriginalWorkOutboxCheckpoint(t.Context(), root)
	if err != nil {
		t.Fatal(errors.Join(err, root.Close()))
	}
	if err := unix.Fsetxattr(int(root.Fd()), connect.OriginalWorkOutboxAttribute, checkpoint, unix.XATTR_CREATE); err != nil {
		t.Fatal(errors.Join(err, root.Close()))
	}
	if err := errors.Join(root.Sync(), root.Close()); err != nil {
		t.Fatal(err)
	}
	self := &providerWorkDeviceFixture{profile: profile, seed: seed, owners: make(chan coreprotocol.OriginalWorkOwnerEnrollment, 8), requests: make(chan [][]byte, 8), requestReads: make(chan [16]byte, 8), requestClosed: make(chan [16]byte, 8), cuts: make(chan []byte, 8), failures: make(chan error, 8)}
	self.server = httptest.NewTLSServer(http.HandlerFunc(self.serveHttp))
	t.Cleanup(self.server.Close)
	self.profile.ApiUrl = self.server.URL
	self.profilePath, self.profileSha256 = writeProviderWorkCaptureFixture(t, self.profile)
	t.Setenv("WARP_VERSION", "synthetic-test")
	return self
}

// Only original public bytes enter the handler. Failures are reported to the
// owner, so a network callback never substitutes a testing-goroutine verdict.
func (self *providerWorkDeviceFixture) serveHttp(writer http.ResponseWriter, request *http.Request) {
	fail := func(err error) {
		select {
		case self.failures <- err:
		default:
		}
		http.Error(writer, "synthetic refusal", http.StatusBadRequest)
	}
	writer.Header().Set("Content-Type", "application/json")
	switch request.URL.Path {
	case "/provider-work/v1/owners":
		raw, err := io.ReadAll(io.LimitReader(request.Body, coreprotocol.MaximumOriginalWorkOwnerBytes+1))
		if err != nil {
			fail(err)
			return
		}
		owner, err := coreprotocol.DecodeOriginalWorkOwnerEnrollment(request.Context(), raw)
		domain, _ := self.profile.Providers[0].Domain.Digest()
		if err != nil || owner.DomainHash != domain || owner.ClientId != self.profile.Providers[0].ClientId || owner.PublicKey != self.profile.Providers[0].PublicKey {
			fail(errors.Join(errors.New("actual provider enrollment differs from retained launch identity"), err))
			return
		}
		_ = json.NewEncoder(writer).Encode(coreprotocol.OriginalWorkOwnerReceipt{Schema: coreprotocol.OriginalWorkOwnerReceiptSchema, OwnerHash: sha256.Sum256(raw)})
		self.owners <- owner
	case "/provider-work/v1/requests":
		var generation [16]byte
		raw, err := hex.DecodeString(request.URL.Query().Get("generation"))
		if err != nil || len(raw) != len(generation) {
			fail(errors.New("actual whole-work request query lost generation"))
			return
		}
		copy(generation[:], raw)
		self.requestReads <- generation
		select {
		case requests := <-self.requests:
			_ = json.NewEncoder(writer).Encode(coreprotocol.OriginalWorkRequests{Schema: coreprotocol.OriginalWorkRequestsSchema, Requests: requests})
		case <-request.Context().Done():
			self.requestClosed <- generation
		}
	case "/provider-work/v1/cuts":
		raw, err := io.ReadAll(io.LimitReader(request.Body, coreprotocol.MaximumOriginalWorkSubmissionBytes+1))
		if err != nil {
			fail(err)
			return
		}
		var submission coreprotocol.OriginalWorkCutSubmission
		if err := json.Unmarshal(raw, &submission); err != nil {
			fail(err)
			return
		}
		receipt, err := coreprotocol.VerifyOriginalWorkSubmission(request.Context(), submission, self.profile.RequestPublicKey)
		if err != nil {
			fail(err)
			return
		}
		_ = json.NewEncoder(writer).Encode(receipt)
		self.cuts <- raw
	default:
		if self.other != nil {
			self.other.ServeHTTP(writer, request)
			return
		}
		// Other device services receive a transient response from this local
		// fixture. They cannot publish whole-work evidence on another route.
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
}

// The shared production constructor creates the real manager and worker. Only
// this owner's TLS roots, finite dial boundary and unrelated extender opt-out
// differ from ordinary launch settings.
func (self *providerWorkDeviceFixture) start(t *testing.T) func() {
	_, close := self.startDevice(t, nil)
	return close
}

// Original source tests use the same real constructor and transport owner.
func (self *providerWorkDeviceFixture) startDevice(t *testing.T, original *ProviderContractCaptureProfile) (*sdk.DeviceLocal, func()) {
	t.Helper()
	profile, err := ReadProviderWorkCaptureProfile(t.Context(), self.profilePath, self.profileSha256, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.validateRole(self.server.URL, []string{"direct"}, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	strategy := connect.DefaultClientStrategySettings()
	strategy.EnableResilient = false
	strategy.TlsConfig = self.server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	strategy.DialContextSettings = &connect.DialContextSettings{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != self.server.Listener.Addr().String() {
			return nil, errors.New("synthetic provider work fixture refuses nonlocal egress")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	space := sdk.NewNetworkSpaceWithUrls(ctx, self.server.URL, "wss"+strings.TrimPrefix(self.server.URL, "https"), strategy)
	settings := ProviderDeviceSettings([32]byte{})
	settings.ProvideExtenderEnabled = false
	settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(self.seed, nil, nil)
	clientId := connect.Id(profile.Providers[0].ClientId)
	token := providerRegistrationTestToken(t, clientId.String(), "retained-whole-work")
	device, err := newProviderDeviceLocal(ctx, space, strategy, token, "synthetic original-work provider", settings, profile, "direct", clientId, original)
	if err != nil {
		cancel()
		space.Close()
		t.Fatal(err)
	}
	closed := false
	close := func() {
		if closed {
			return
		}
		closed = true
		cancel()
		join, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if err := device.CloseAndWait(join); err != nil {
			t.Error("actual provider device did not join whole-work custody", err)
		}
		space.Close()
	}
	t.Cleanup(close)
	return device, close
}

// Named positive barriers decide ordering; the deadline only detects deadlock.
func providerWorkDeviceAwait[T any](t *testing.T, values <-chan T, failures <-chan error) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case err := <-failures:
		t.Fatal(err)
	case <-time.After(20 * time.Second):
		t.Fatal("actual provider whole-work boundary did not complete")
	}
	var empty T
	return empty
}

// A late observation from the joined old lifecycle cannot satisfy a current
// generation's read or cancellation barrier. The bounded fixture retains both.
func providerWorkDeviceAwaitGeneration(t *testing.T, values <-chan [16]byte, failures <-chan error, current, retired [16]byte) {
	t.Helper()
	for range 8 {
		generation := providerWorkDeviceAwait(t, values, failures)
		if generation == current {
			return
		}
		if generation != retired || retired == ([16]byte{}) {
			t.Fatal("provider HTTP observation belongs to an unrecognized generation")
		}
	}
	t.Fatal("provider current generation never reached its own HTTP boundary")
}

// The actual owner signs and retains its first cut, then a newly constructed
// provider replays exactly those bytes before enrolling a different generation.
func TestProviderWholeWorkActualDeviceCaptureAndRestartPreserveOriginal(t *testing.T) {
	fixture := newProviderWorkDeviceFixture(t)
	closeFirst := fixture.start(t)
	owner := providerWorkDeviceAwait(t, fixture.owners, fixture.failures)
	if generation := providerWorkDeviceAwait(t, fixture.requestReads, fixture.failures); generation != owner.Generation {
		t.Fatal("actual request poll differs from signed enrolled generation")
	}
	now := time.Now().Unix()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{92}, ed25519.SeedSize))
	request, err := coreprotocol.SignOriginalWorkRequest(coreprotocol.OriginalWorkRequest{RequestId: [16]byte{97}, DomainHash: owner.DomainHash, ClientId: owner.ClientId, Generation: owner.Generation, PublicKey: owner.PublicKey, Epoch: 9, Kind: "start", Block: 200, BlockHash: [32]byte{98}, IssuedAtUnix: now - 10, ExpiresAtUnix: now + 600}, key)
	if err != nil {
		t.Fatal(err)
	}
	requestRaw, err := request.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	fixture.requests <- [][]byte{requestRaw}
	first := providerWorkDeviceAwait(t, fixture.cuts, fixture.failures)
	closeFirst()
	var submission coreprotocol.OriginalWorkCutSubmission
	if json.Unmarshal(first, &submission) != nil {
		t.Fatal("actual provider cut did not retain canonical submission")
	}
	cut, err := coreprotocol.DecodeOriginalWorkCut(t.Context(), submission.Cut)
	if err != nil || !cut.Complete || !request.Matches(cut) || len(cut.Contracts) != 0 {
		t.Fatal("actual provider lost signed whole-work identity or known-empty cut", cut, err)
	}
	closeSecond := fixture.start(t)
	replayed := providerWorkDeviceAwait(t, fixture.cuts, fixture.failures)
	if !bytes.Equal(first, replayed) {
		t.Fatal("actual restarted provider recaptured its original signed cut")
	}
	var next coreprotocol.OriginalWorkOwnerEnrollment
	for range 8 {
		next = providerWorkDeviceAwait(t, fixture.owners, fixture.failures)
		if next.Generation != owner.Generation {
			break
		}
	}
	if next.Generation == owner.Generation || next.ClientId != owner.ClientId || next.PublicKey != owner.PublicKey {
		t.Fatal("actual restart reused generation or replaced original key")
	}
	providerWorkDeviceAwaitGeneration(t, fixture.requestReads, fixture.failures, next.Generation, owner.Generation)
	closeSecond()
	providerWorkDeviceAwaitGeneration(t, fixture.requestClosed, fixture.failures, next.Generation, owner.Generation)
	entries, err := os.ReadDir(fixture.profile.Providers[0].OutboxDirectory)
	if err != nil {
		t.Fatal(err)
	}
	retained := 0
	for _, entry := range entries {
		if entry.Name() == ".owner.lock" {
			continue
		}
		retained++
		raw, err := os.ReadFile(filepath.Join(fixture.profile.Providers[0].OutboxDirectory, entry.Name()))
		if err != nil || !bytes.Equal(first, raw) {
			t.Fatal("actual restart changed retained original custody", err)
		}
	}
	if retained != 1 {
		t.Fatal("actual restart invented or dropped an original cut", retained)
	}
}

// Hold a real HTTPS read open, prove the lifecycle lease is exclusive, cancel
// the provider, and require both HTTP ownership and the file lease to release.
func TestProviderWholeWorkActualDeviceCancellationJoinsHttpAndOutbox(t *testing.T) {
	fixture := newProviderWorkDeviceFixture(t)
	closeDevice := fixture.start(t)
	owner := providerWorkDeviceAwait(t, fixture.owners, fixture.failures)
	providerWorkDeviceAwaitGeneration(t, fixture.requestReads, fixture.failures, owner.Generation, [16]byte{})
	lock, err := os.OpenFile(filepath.Join(fixture.profile.Providers[0].OutboxDirectory, ".owner.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
		t.Fatal("actual running provider did not own its outbox lease", err)
	}
	closeDevice()
	providerWorkDeviceAwaitGeneration(t, fixture.requestClosed, fixture.failures, owner.Generation, [16]byte{})
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("actual provider join left the original outbox lease owned", err)
	}
}
