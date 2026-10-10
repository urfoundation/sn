//go:build linux || darwin

// Public device sends reach the actual API control route only after retaining
// their original signed request under the independently prepared source owner.
package miner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"github.com/urnetwork/sdk/v2026"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

// Only an explicitly created synthetic empty root receives this first birth.
func prepareProviderContractCaptureFixture(t *testing.T, owner ProviderContractCaptureOwner) {
	t.Helper()
	lease, err := os.OpenFile(filepath.Join(owner.Directory, connect.OriginalContractStoreLeaseName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(lease.Sync(), lease.Close()); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(owner.Directory)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := owner.Domain.Digest()
	if err != nil {
		t.Fatal(errors.Join(err, root.Close()))
	}
	scope := connect.OriginalContractStoreScope{DomainHash: domain, ClientId: owner.ClientId, PublicKey: owner.PublicKey, SourceGeneration: owner.SourceGeneration}
	checkpoint, err := connect.BuildFreshOriginalContractStoreCheckpoint(t.Context(), root, scope)
	if err != nil {
		t.Fatal(errors.Join(err, root.Close()))
	}
	if err := unix.Fsetxattr(int(root.Fd()), connect.OriginalContractStoreAttribute, checkpoint, unix.XATTR_CREATE); err != nil {
		t.Fatal(errors.Join(err, root.Close()))
	}
	if err := errors.Join(root.Sync(), root.Close()); err != nil {
		t.Fatal(err)
	}
}

// A transport request supplies the real frame. The handler may only find its
// already retained original; it cannot manufacture a later capture observation.
func providerContractOriginalAtTransport(ctx context.Context, directory string, frame *coreprotocol.Frame) ([]byte, error) {
	frameRaw, err := proto.Marshal(frame)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "request-") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		original, err := coreprotocol.DecodeOriginalContractRequest(ctx, raw)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(original.RequestFrame, frameRaw) {
			return raw, nil
		}
	}
	return nil, errors.New("actual HTTP control took its request before original custody")
}

// Multiple pending creates from a joined old send owner cannot satisfy the
// restarted provider's observation. Every accepted request is generation tagged.
func providerContractAwaitGeneration(t *testing.T, originals <-chan []byte, failures <-chan error, current, retired [16]byte) ([]byte, coreprotocol.OriginalContractRequest) {
	t.Helper()
	for range 8 {
		raw := providerWorkDeviceAwait(t, originals, failures)
		original, err := coreprotocol.DecodeOriginalContractRequest(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		if original.Generation == current {
			return raw, original
		}
		if original.Generation != retired || retired == ([16]byte{}) {
			t.Fatal("actual original request belongs to an unknown SDK generation")
		}
	}
	t.Fatal("restarted SDK never reached its own original request HTTP boundary")
	return nil, coreprotocol.OriginalContractRequest{}
}

func TestProviderOriginalContractActualDeviceRetainsBeforeHTTPAndRestartsSource(t *testing.T) {
	fixture := newProviderWorkDeviceFixture(t)
	work := fixture.profile.Providers[0]
	original := ProviderContractCaptureProfile{Schema: ProviderContractCaptureSchema, ApiUrl: fixture.profile.ApiUrl,
		Providers: []ProviderContractCaptureOwner{{Slot: work.Slot, ClientId: work.ClientId, PublicKey: work.PublicKey, Domain: work.Domain, Directory: providerRegistrationPrivateDir(t), SourceGeneration: [16]byte{121}}}}
	prepareProviderContractCaptureFixture(t, original.Providers[0])
	path, digest := writeProviderContractCaptureFixture(t, original)
	approved, err := ReadProviderContractCaptureProfile(t.Context(), path, digest, true)
	if err != nil {
		t.Fatal(err)
	}
	originals, closed := make(chan []byte, 8), make(chan [16]byte, 8)
	fixture.other = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fail := func(err error) {
			select {
			case fixture.failures <- err:
			default:
			}
			http.Error(writer, "synthetic original source failure", http.StatusBadRequest)
		}
		if request.URL.Path != "/connect/control" {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var args connect.ConnectControlArgs
		if err := json.NewDecoder(io.LimitReader(request.Body, 128*1024)).Decode(&args); err != nil {
			fail(err)
			return
		}
		raw, err := base64.StdEncoding.DecodeString(args.Pack)
		var pack coreprotocol.Pack
		if err != nil || proto.Unmarshal(raw, &pack) != nil {
			fail(errors.New("actual control did not carry its original pack"))
			return
		}
		for _, frame := range pack.Frames {
			if frame.MessageType != coreprotocol.MessageType_TransferCreateContract {
				continue
			}
			retained, err := providerContractOriginalAtTransport(request.Context(), original.Providers[0].Directory, frame)
			if err != nil {
				fail(err)
				return
			}
			decoded, err := coreprotocol.DecodeOriginalContractRequest(request.Context(), retained)
			if err != nil {
				fail(err)
				return
			}
			select {
			case originals <- retained:
			case <-request.Context().Done():
				return
			}
			<-request.Context().Done()
			select {
			case closed <- decoded.Generation:
			default:
				fail(errors.New("synthetic original source cancellation census exceeded its bound"))
			}
			return
		}
		_ = json.NewEncoder(writer).Encode(connect.ConnectControlResult{Pack: base64.StdEncoding.EncodeToString(nil)})
	})
	firstDevice, closeFirst := fixture.startDevice(t, approved)
	firstOwner := providerWorkDeviceAwait(t, fixture.owners, fixture.failures)
	providerWorkDeviceAwaitGeneration(t, fixture.requestReads, fixture.failures, firstOwner.Generation, [16]byte{})
	if !firstDevice.SendSubprotocolBytes(4096, sdk.NewId(), []byte("synthetic first original")) {
		t.Fatal("actual device did not enqueue the first provider request")
	}
	firstRaw, first := providerContractAwaitGeneration(t, originals, fixture.failures, firstOwner.Generation, [16]byte{})
	if first.ClientId != work.ClientId || first.PublicKey != work.PublicKey || first.DomainHash != firstOwner.DomainHash || first.Generation == original.Providers[0].SourceGeneration {
		t.Fatal("actual first request substituted source scope for SDK generation", first)
	}
	closeFirst()
	providerWorkDeviceAwaitGeneration(t, closed, fixture.failures, first.Generation, [16]byte{})
	providerWorkDeviceAwaitGeneration(t, fixture.requestClosed, fixture.failures, first.Generation, [16]byte{})
	secondDevice, closeSecond := fixture.startDevice(t, approved)
	secondOwner := providerWorkDeviceAwait(t, fixture.owners, fixture.failures)
	providerWorkDeviceAwaitGeneration(t, fixture.requestReads, fixture.failures, secondOwner.Generation, first.Generation)
	if secondOwner.Generation == first.Generation || !secondDevice.SendSubprotocolBytes(4096, sdk.NewId(), []byte("synthetic restarted original")) {
		t.Fatal("actual restarted provider reused generation or lost the send owner")
	}
	_, second := providerContractAwaitGeneration(t, originals, fixture.failures, secondOwner.Generation, first.Generation)
	if second.PublicKey != first.PublicKey || second.ClientId != first.ClientId || second.DomainHash != first.DomainHash || second.Generation == original.Providers[0].SourceGeneration {
		t.Fatal("actual restarted request changed independent source authority", second)
	}
	closeSecond()
	providerWorkDeviceAwaitGeneration(t, closed, fixture.failures, second.Generation, first.Generation)
	providerWorkDeviceAwaitGeneration(t, fixture.requestClosed, fixture.failures, second.Generation, first.Generation)
	domain, _ := work.Domain.Digest()
	scope := connect.OriginalContractStoreScope{DomainHash: domain, ClientId: work.ClientId, PublicKey: work.PublicKey, SourceGeneration: original.Providers[0].SourceGeneration}
	if err := connect.ValidateOriginalContractStore(t.Context(), original.Providers[0].Directory, scope); err != nil {
		t.Fatal("joined lifecycles changed independently prepared source custody", err)
	}
	var frame coreprotocol.Frame
	if err := proto.Unmarshal(first.RequestFrame, &frame); err != nil {
		t.Fatal(err)
	}
	retained, err := providerContractOriginalAtTransport(t.Context(), original.Providers[0].Directory, &frame)
	if err != nil || !bytes.Equal(retained, firstRaw) {
		t.Fatal("restart regenerated or lost the original signed source request", err)
	}
}
