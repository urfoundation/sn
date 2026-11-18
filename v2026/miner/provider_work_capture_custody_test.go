//go:build linux || darwin

// Real provider construction must reject incomplete prepared custody before
// allocating its SDK worker tree or publishing any replacement outbox state.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
	coreprotocol "github.com/urnetwork/connect/v2026/protocol"
	"github.com/urnetwork/sdk/v2026"
	"golang.org/x/sys/unix"
)

// A valid reviewed profile and retained key cannot authorize an empty rebirth.
// The nil downstream owners prove the constructor refuses at custody admission.
func TestProviderWholeWorkActualConstructorRefusesUnpreparedCustody(t *testing.T) {
	profile, seed := providerWorkCaptureFixture(t)
	path, digest := writeProviderWorkCaptureFixture(t, profile)
	loaded, err := ReadProviderWorkCaptureProfile(t.Context(), path, digest, true)
	if err != nil {
		t.Fatal(err)
	}
	directory := profile.Providers[0].OutboxDirectory
	for _, indexed := range []bool{false, true} {
		if indexed {
			file, err := os.OpenFile(filepath.Join(directory, connect.OriginalWorkOutboxIndexName), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		settings := ProviderDeviceSettings([32]byte{})
		settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, nil, nil)
		device, err := newProviderDeviceLocal(t.Context(), nil, nil, "", "synthetic incomplete outbox", settings, loaded, "direct", connect.Id(profile.Providers[0].ClientId))
		if device != nil || !errors.Is(err, connect.ErrOriginalWorkOutboxIdentity) {
			t.Fatal("actual constructor bypassed prepared original custody", indexed, device, err)
		}
		capture := settings.ContractManagerSettings.OriginalWorkCapture
		if capture == nil || capture.PublicKey != profile.Providers[0].PublicKey || capture.RequestPublicKey != profile.RequestPublicKey {
			t.Fatal("actual runtime capture settings lost independently approved signing keys", indexed, capture)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || !indexed && len(entries) != 0 || indexed && (len(entries) != 1 || entries[0].Name() != connect.OriginalWorkOutboxIndexName) {
			t.Fatal("failed launch created or replaced original custody", indexed, entries, err)
		}
		if _, err := unix.Getxattr(directory, connect.OriginalWorkOutboxAttribute, make([]byte, 4096)); err == nil {
			t.Fatal("failed launch minted an empty birth checkpoint", indexed)
		}
	}
}

// Cancellation belongs to the actual constructor and precedes any outbox read,
// transport or SDK construction. The original directory remains untouched.
func TestProviderWholeWorkActualConstructorPreservesCanceledCustody(t *testing.T) {
	profile, seed := providerWorkCaptureFixture(t)
	settings := ProviderDeviceSettings([32]byte{})
	settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	device, err := newProviderDeviceLocal(ctx, nil, nil, "", "synthetic canceled outbox", settings, &profile, "direct", connect.Id(profile.Providers[0].ClientId))
	if device != nil || !errors.Is(err, context.Canceled) || settings.ContractManagerSettings.OriginalWorkCapture != nil {
		t.Fatal("canceled actual constructor changed capture ownership", device, err)
	}
	entries, err := os.ReadDir(profile.Providers[0].OutboxDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatal("canceled constructor created original custody", entries, err)
	}
}

// A completed original under the old key cannot select its own authority when
// the independently reviewed launch profile now names a different provider key.
func TestProviderWholeWorkActualConstructorRefusesUnapprovedRetainedKey(t *testing.T) {
	fixture := newProviderWorkDeviceFixture(t)
	closeDevice := fixture.start(t)
	owner := providerWorkDeviceAwait(t, fixture.owners, fixture.failures)
	providerWorkDeviceAwaitGeneration(t, fixture.requestReads, fixture.failures, owner.Generation, [16]byte{})
	now := time.Now().Unix()
	requestKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{92}, ed25519.SeedSize))
	request, err := coreprotocol.SignOriginalWorkRequest(coreprotocol.OriginalWorkRequest{RequestId: [16]byte{106}, DomainHash: owner.DomainHash, ClientId: owner.ClientId, Generation: owner.Generation, PublicKey: owner.PublicKey, Epoch: 9, Kind: "start", Block: 200, BlockHash: [32]byte{107}, IssuedAtUnix: now - 10, ExpiresAtUnix: now + 600}, requestKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := request.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	fixture.requests <- [][]byte{raw}
	providerWorkDeviceAwait(t, fixture.cuts, fixture.failures)
	closeDevice()

	seed := bytes.Repeat([]byte{108}, ed25519.SeedSize)
	providerKey := ed25519.NewKeyFromSeed(seed)
	profile := fixture.profile
	profile.Providers = append([]ProviderWorkCaptureOwner(nil), fixture.profile.Providers...)
	copy(profile.Providers[0].PublicKey[:], providerKey[ed25519.SeedSize:])
	path, digest := writeProviderWorkCaptureFixture(t, profile)
	loaded, err := ReadProviderWorkCaptureProfile(t.Context(), path, digest, true)
	if err != nil {
		t.Fatal(err)
	}
	settings := ProviderDeviceSettings([32]byte{})
	settings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, nil, nil)
	device, err := newProviderDeviceLocal(t.Context(), nil, nil, "", "synthetic key rotation without history approval", settings, loaded, "direct", connect.Id(profile.Providers[0].ClientId))
	if device != nil || !errors.Is(err, connect.ErrOriginalWorkOutboxIdentity) {
		t.Fatal("retained leaf substituted its key for independent launch authority", device, err)
	}
}
