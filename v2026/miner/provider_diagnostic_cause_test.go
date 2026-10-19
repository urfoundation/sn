// Optional classification cannot hold provider callbacks or required shutdown.
package miner

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/clientauth"
)

// A callback entry, rather than a short timeout, exposes forbidden traversal.
type providerDiagnosticMethodProbe struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Uint64
}

// Both method outcomes and the observing goroutine remain fixture-owned.
func (self *providerDiagnosticMethodProbe) enteredMethod() {
	self.calls.Add(1)
	select {
	case self.entered <- struct{}{}:
	default:
	}
	<-self.release
}

// Raw formatting is also forbidden even when the method eventually returns.
func (self *providerDiagnosticMethodProbe) Error() string {
	self.enteredMethod()
	return "synthetic opaque cause"
}

// Returning a real timeout does not authorize calling an arbitrary method.
func (self *providerDiagnosticMethodProbe) Unwrap() error {
	self.enteredMethod()
	return context.DeadlineExceeded
}

// Is is user-defined code too, independent of Error and Unwrap behavior.
func (self *providerDiagnosticMethodProbe) Is(error) bool { self.enteredMethod(); return false }

// As must not be used as a diagnostic type-inspection fallback.
func (self *providerDiagnosticMethodProbe) As(any) bool { self.enteredMethod(); return false }

// The real scalar serializer/exporter finishes and leaves the cause untouched.
func TestProviderDiagnosticsForeignMethodsCannotHoldObservation(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		probe := &providerDiagnosticMethodProbe{entered: make(chan struct{}, 1), release: make(chan struct{})}
		var cause error = probe
		if wrapped {
			cause = &url.Error{Op: "Get", URL: "https://synthetic.example", Err: probe}
		}
		sink := &providerDiagnosticRecorder{records: make(chan []byte, 1)}
		owner := newProviderDiagnosticTestOwner(t, sink)
		ctx, cancel := context.WithCancel(t.Context())
		finished := make(chan struct{})
		go func() {
			owner.observe(providerWorkerFailed, 3, true, cause, 0, nil)
			cancel()
			close(finished)
		}()
		select {
		case <-finished:
			close(probe.release)
		case <-probe.entered:
			close(probe.release)
			<-finished
		}
		if probe.calls.Load() != 0 || ctx.Err() != context.Canceled {
			t.Fatal("provider optional cause traversal held required continuation")
		}
		raw := sink.next(t)
		if !bytes.Contains(raw, []byte(`"cause":"unknown"`)) || !bytes.Contains(raw, []byte(`"event":"provider_worker_failed"`)) || bytes.Contains(raw, []byte("synthetic opaque")) {
			t.Fatal("provider opaque cause changed its bounded scalar wire")
		}
	}
}

// Real rename/tombstone failures finish before cancellation. The cancellation
// barrier proves no optional event was even offered before that required step.
func TestProviderDiagnosticsCancelBeforeOptionalFailureOffer(t *testing.T) {
	for _, logout := range []bool{false, true} {
		writer := &providerRefusedWriter{}
		owner := newProviderDiagnosticTestOwner(t, writer)
		ctx, cancel := context.WithCancel(t.Context())
		path := filepath.Join(t.TempDir(), "client.jwt")
		networkPath := filepath.Join(filepath.Dir(path), "network.jwt")
		if err := clientauth.WriteToken(networkPath, "synthetic-bootstrap"); err != nil {
			t.Fatal(err)
		}
		if logout {
			if err := clientauth.WriteToken(path, "synthetic-client"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path+".rejected", 0700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		callbacks := &providerAuthenticationCallbacks{diagnostics: owner, provider: 5, clientJwtPath: path, networkJwtPath: networkPath, cancel: func() {
			cancel()
			close(entered)
			<-release
		}}
		go func() {
			if logout {
				callbacks.AuthLogout()
			} else {
				callbacks.JwtRefreshed("synthetic-replacement")
			}
			close(finished)
		}()
		t.Cleanup(func() { cancel(); unblock(); <-finished })
		select {
		case <-entered:
		case <-finished:
			t.Fatal("provider callback omitted required cancellation")
		}
		original := callbacks.failure()
		if fault, ok := original.(*os.LinkError); !ok || fault == nil || ctx.Err() != context.Canceled {
			t.Fatal("provider cancellation lost the original completed file failure")
		}
		if owner.snapshot().Authentication.Dropped != 0 || writer.calls.Load() != 0 {
			t.Fatal("provider offered optional output before required cancellation")
		}
		if logout {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rejected token remained installed at cancellation")
			}
		} else if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatal("failed token replacement changed the refused target")
		}
		if token, err := clientauth.ReadToken(networkPath); err != nil || token != "synthetic-bootstrap" {
			t.Fatal("optional output changed the bootstrap file")
		}
		unblock()
		<-finished
		wanted := uint64(1)
		if logout {
			wanted = 2
		}
		if callbacks.failure() != original || owner.snapshot().Authentication.Dropped != wanted || writer.calls.Load() != 0 {
			t.Fatal("completed callback replaced custody or fabricated delivery")
		}
	}
}
