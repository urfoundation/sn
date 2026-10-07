//go:build linux || darwin

// Registration retry faults enter only after the real SDK has completed an
// unavailable HTTP attempt with its original key, request and send anchor.
package miner

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Fixed text cannot recursively format a graph. Unwrap observations belong to
// this one provider run; foreign matching methods must never grant admission.
type providerRegistrationCauseGraph struct {
	causes      []error
	unwrapCount *atomic.Int32
}

// No wrapped error is rendered by this fixture.
func (self *providerRegistrationCauseGraph) Error() string {
	return "synthetic registration cause graph"
}

// Each physical edge inspection increments this run's observation count.
func (self *providerRegistrationCauseGraph) Unwrap() []error {
	self.unwrapCount.Add(1)
	return self.causes
}

// Foreign matches cannot convert graph shape into availability authority.
func (self *providerRegistrationCauseGraph) Is(error) bool {
	panic("registration retry invoked a foreign Is method")
}

// Foreign matches cannot convert graph shape into availability authority.
func (self *providerRegistrationCauseGraph) As(any) bool {
	panic("registration retry invoked a foreign As method")
}

// Only the four original custody files are sampled, after the actual POST has
// proved they existed. The fixture never supplies or repairs those bytes.
func providerRegistrationOriginalFiles(directory string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, name := range []string{".provider.key", ".provider.key.identity", ".provider.jwt.registration", ".provider.jwt.registration.started"} {
		raw, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		files[name] = raw
	}
	return files, nil
}

// A nil typed availability value is not a route owner's verdict. Both admitted
// concrete types are exercised after a real failed POST, before any replay.
func TestProviderRegistrationRunRefusesTypedNilAvailability(t *testing.T) {
	for _, cause := range []error{(*sdk.NetworkClientRegistrationUnavailableError)(nil), (*clientauth.RegistrationRefreshUnavailableError)(nil)} {
		fixture := newProviderRegistrationFixture(t)
		fixture.statuses = []int{http.StatusServiceUnavailable, 0}
		var original map[string][]byte
		var captureErr error
		var attempts, authenticated atomic.Int32
		var joined atomic.Bool
		err := fixture.run(t.Context(), true, providerRegistrationHooks{
			additionalAttemptError: func(attemptErr error) error {
				attempts.Add(1)
				original, captureErr = providerRegistrationOriginalFiles(fixture.dir)
				if captureErr != nil {
					return captureErr
				}
				if !providerRegistrationRetryable(attemptErr, 0) {
					return errors.New("synthetic fault did not follow real registration availability")
				}
				return cause
			},
			afterAuthenticated: func(string, connect.Id, []byte) error {
				authenticated.Add(1)
				return errors.New("synthetic unexpected registration authentication")
			},
			afterApiJoined: func() { joined.Store(true) },
		})
		posts, legacy, allocations, _ := fixture.counts()
		if err == nil || captureErr != nil || attempts.Load() != 1 || authenticated.Load() != 0 || !joined.Load() || posts != 1 || legacy != 0 || allocations != 1 {
			t.Fatal("typed nil availability retried or escaped the joined provider owner")
		}
		retained, readErr := providerRegistrationOriginalFiles(fixture.dir)
		if readErr != nil || len(original) != 4 || len(retained) != len(original) {
			t.Fatal("registration refusal lost original custody")
		}
		for name, raw := range original {
			if !bytes.Equal(raw, retained[name]) {
				t.Fatal("typed nil refusal changed original custody", name)
			}
		}
		if _, tokenErr := os.Stat(filepath.Join(fixture.dir, ".provider.jwt")); !errors.Is(tokenErr, os.ErrNotExist) {
			t.Fatal("typed nil refusal installed a client credential")
		}
	}
}

// Every individual branch fits the old local fanout/depth limits. The complete
// graph exceeds the shared work allowance and must stop the actual replay.
func TestProviderRegistrationRunBoundsWholeCauseGraph(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	fixture.statuses = []int{http.StatusServiceUnavailable, 0}
	var original map[string][]byte
	var captureErr error
	var attempts, authenticated, unwraps atomic.Int32
	var joined atomic.Bool
	err := fixture.run(t.Context(), true, providerRegistrationHooks{
		additionalAttemptError: func(attemptErr error) error {
			attempts.Add(1)
			original, captureErr = providerRegistrationOriginalFiles(fixture.dir)
			if captureErr != nil {
				return captureErr
			}
			if !providerRegistrationRetryable(attemptErr, 0) {
				return errors.New("synthetic graph did not follow real registration availability")
			}
			branches := make([]error, 32)
			for index := range branches {
				leaves := make([]error, 32)
				for leaf := range leaves {
					leaves[leaf] = attemptErr
				}
				branches[index] = &providerRegistrationCauseGraph{causes: leaves, unwrapCount: &unwraps}
			}
			return &providerRegistrationCauseGraph{causes: branches, unwrapCount: &unwraps}
		},
		afterAuthenticated: func(string, connect.Id, []byte) error {
			authenticated.Add(1)
			return errors.New("synthetic unexpected graph replay")
		},
		afterApiJoined: func() { joined.Store(true) },
	})
	posts, legacy, allocations, _ := fixture.counts()
	if err == nil || captureErr != nil || attempts.Load() != 1 || authenticated.Load() != 0 || !joined.Load() || posts != 1 || legacy != 0 || allocations != 1 || unwraps.Load() < 1 || unwraps.Load() > minerReadCauseMaximumNodes {
		t.Fatal("oversized registration graph retried or escaped its finite joined owner")
	}
	retained, readErr := providerRegistrationOriginalFiles(fixture.dir)
	if readErr != nil || len(original) != 4 || len(retained) != len(original) {
		t.Fatal("bounded registration refusal lost original custody")
	}
	for name, raw := range original {
		if !bytes.Equal(raw, retained[name]) {
			t.Fatal("bounded registration refusal changed original custody", name)
		}
	}
	if _, tokenErr := os.Stat(filepath.Join(fixture.dir, ".provider.jwt")); !errors.Is(tokenErr, os.ErrNotExist) {
		t.Fatal("bounded registration refusal installed a client credential")
	}
}

// A complete graph can still replay the one retained operation. The original
// server allocation and physical key/anchor survive a successful second POST.
func TestProviderRegistrationRunRetainsWrappedAvailabilityReplay(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	fixture.statuses = []int{http.StatusServiceUnavailable, 0}
	var original map[string][]byte
	var captureErr error
	var capturedId connect.Id
	var capturedSeed []byte
	var attempts, authenticated, unwraps atomic.Int32
	var joined atomic.Bool
	stop := errors.New("synthetic stop after original registration replay")
	err := fixture.run(t.Context(), true, providerRegistrationHooks{
		additionalAttemptError: func(attemptErr error) error {
			attempts.Add(1)
			original, captureErr = providerRegistrationOriginalFiles(fixture.dir)
			if captureErr != nil {
				return captureErr
			}
			return &providerRegistrationCauseGraph{causes: []error{attemptErr}, unwrapCount: &unwraps}
		},
		afterAuthenticated: func(_ string, id connect.Id, seed []byte) error {
			authenticated.Add(1)
			capturedId, capturedSeed = id, bytes.Clone(seed)
			return stop
		},
		afterApiJoined: func() { joined.Store(true) },
	})
	posts, legacy, allocations, _ := fixture.counts()
	if authenticated.Load() != 1 || captureErr != nil || attempts.Load() != 1 || !joined.Load() || posts != 2 || legacy != 0 || allocations != 1 || unwraps.Load() != 1 || !errors.Is(err, stop) {
		t.Fatal("complete registration availability did not replay its original operation")
	}
	retained, readErr := providerRegistrationOriginalFiles(fixture.dir)
	if readErr != nil || len(original) != 4 || len(retained) != len(original) {
		t.Fatal("successful registration replay lost original custody")
	}
	for _, name := range []string{".provider.key", ".provider.key.identity", ".provider.jwt.registration.started"} {
		if !bytes.Equal(original[name], retained[name]) {
			t.Fatal("successful registration replay changed original custody", name)
		}
	}
	var before, after providerRegistrationTestRecord
	if json.Unmarshal(original[".provider.jwt.registration"], &before) != nil || json.Unmarshal(retained[".provider.jwt.registration"], &after) != nil {
		t.Fatal("registration replay record cannot be decoded")
	}
	originalRequest, originalErr := sdk.EncodeNetworkClientRegistration(&before.Request)
	retainedRequest, retainedErr := sdk.EncodeNetworkClientRegistration(&after.Request)
	if originalErr != nil || retainedErr != nil || !bytes.Equal(originalRequest, retainedRequest) || before.ClientId != "" || after.ClientId != capturedId.String() || capturedId.String() != "00000000-0000-0000-0000-000000000101" || !bytes.Equal(capturedSeed, original[".provider.key"]) {
		t.Fatal("registration replay replaced its original request, client or key")
	}
	if token, tokenErr := clientauth.ReadToken(filepath.Join(fixture.dir, ".provider.jwt")); tokenErr != nil || token == "" {
		t.Fatal("successful registration replay did not retain its client credential")
	}
}

// A local filesystem operation stays a custody failure even when its retained
// child describes API availability. Neither concrete wrapper may admit replay.
func TestProviderRegistrationRunRefusesLocalCustodyWrappers(t *testing.T) {
	for _, link := range []bool{false, true} {
		fixture := newProviderRegistrationFixture(t)
		fixture.statuses = []int{http.StatusServiceUnavailable, 0}
		var original map[string][]byte
		var captureErr, attemptCause, localCause error
		var attempts, authenticated atomic.Int32
		var joined atomic.Bool
		err := fixture.run(t.Context(), true, providerRegistrationHooks{
			additionalAttemptError: func(attemptErr error) error {
				attempts.Add(1)
				attemptCause = attemptErr
				original, captureErr = providerRegistrationOriginalFiles(fixture.dir)
				if captureErr != nil {
					return captureErr
				}
				if !providerRegistrationRetryable(attemptErr, 0) {
					return errors.New("synthetic local fault did not follow real registration availability")
				}
				localCause = &os.PathError{Op: "read", Path: "synthetic-registration-custody", Err: attemptErr}
				if link {
					localCause = &os.LinkError{Op: "rename", Old: "synthetic-registration-old", New: "synthetic-registration-new", Err: attemptErr}
				}
				return localCause
			},
			afterAuthenticated: func(string, connect.Id, []byte) error {
				authenticated.Add(1)
				return errors.New("synthetic unexpected local-failure replay")
			},
			afterApiJoined: func() { joined.Store(true) },
		})
		posts, legacy, allocations, _ := fixture.counts()
		if err == nil || captureErr != nil || attemptCause == nil || localCause == nil || attempts.Load() != 1 || authenticated.Load() != 0 || !joined.Load() || posts != 1 || legacy != 0 || allocations != 1 {
			t.Fatal("local custody failure retried or escaped the joined provider owner", link)
		}
		if !errors.Is(err, attemptCause) || !errors.Is(err, localCause) {
			t.Fatal("provider refusal discarded the original local or API cause", link)
		}
		retained, readErr := providerRegistrationOriginalFiles(fixture.dir)
		if readErr != nil || len(original) != 4 || len(retained) != len(original) {
			t.Fatal("local registration refusal lost original custody", link)
		}
		for name, raw := range original {
			if !bytes.Equal(raw, retained[name]) {
				t.Fatal("local registration refusal changed original custody", link, name)
			}
		}
		if _, tokenErr := os.Stat(filepath.Join(fixture.dir, ".provider.jwt")); !errors.Is(tokenErr, os.ErrNotExist) {
			t.Fatal("local registration refusal installed a client credential", link)
		}
	}
}
