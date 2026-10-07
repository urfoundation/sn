// The provide command owns its long-lived status worker and diagnostic sink.
// Optional log failures never decide authentication, custody or cancellation.
package miner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026"
)

// Configuration is copied before workers start and is immutable for one run.
type providerRunSettings struct {
	wallet                  string
	walletProof             *snWalletProof
	apiUrl                  string
	connectUrl              string
	port                    int
	proxySettings           []*connect.ProxySettings
	memoryPlan              providerMemoryPlan
	testEgressDialer        *connect.DialContextSettings
	allowClientRegistration bool
	adoptLegacyProviderKey  bool
	closeReportDomainHash   [32]byte
	workCapturePath         string
	workCaptureSha256       string
	requireWorkCapture      bool
	contractCapturePath     string
	contractCaptureSha256   string
	requireContractCapture  bool
}

// This owner starts exactly one Serve goroutine after listener admission. Its
// Close interrupts network I/O and joins Serve before the exporter can close.
type providerStatusServer struct {
	server  *http.Server
	handler *providerStatusHandler
	done    chan error
}

// Every admitted Status call is owned until it returns. Admission and the
// transition to closing share one lock, so Wait never races a later Add.
type providerStatusHandler struct {
	stateLock  sync.Mutex
	closing    bool
	workers    sync.WaitGroup
	status     *Status
	afterAdmit func(context.Context)
}

// Hooks expose actual request admission, not a substitute HTTP/status result.
type providerStatusHooks struct {
	afterAdmit func(context.Context)
	progress   *providerProgressOwner
}

// A late request after closure does not invoke Status or begin response I/O.
func (self *providerStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	admitted := func() bool {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.closing {
			return false
		}
		self.workers.Add(1)
		return true
	}()
	if !admitted {
		return
	}
	defer self.workers.Done()
	if self.afterAdmit != nil {
		self.afterAdmit(r.Context())
	}
	self.status.ServeHTTP(w, r)
}

// Close admission before the network shutdown can release in-flight handlers.
func (self *providerStatusHandler) stopAdmission() {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.closing = true
}

// A disabled status port creates no worker. A failed bind is returned before
// starting providers, preserving the existing service-level shutdown decision.
func newProviderStatusServer(port int, output *providerDiagnostics, cancel context.CancelFunc, progress *providerProgressOwner) (*providerStatusServer, error) {
	if port <= 0 {
		return nil, nil
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return startProviderStatusServer(listener, output, cancel, providerStatusHooks{progress: progress}), nil
}

// The supplied listener transfers to this concrete HTTP owner. Tests can own
// an ephemeral loopback socket without racing a close/rebind port selection.
func startProviderStatusServer(listener net.Listener, output *providerDiagnostics, cancel context.CancelFunc, hooks ...providerStatusHooks) *providerStatusServer {
	handler := &providerStatusHandler{status: &Status{diagnostics: output}}
	if len(hooks) > 0 {
		handler.afterAdmit = hooks[0].afterAdmit
		handler.status.progress = hooks[0].progress
	}
	self := &providerStatusServer{
		server:  &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ErrorLog: log.New(providerStatusLogWriter{diagnostics: output}, "", 0)},
		handler: handler,
		done:    make(chan error, 1),
	}
	go func() {
		err := self.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		if err != nil {
			output.observe(providerStatusFailed, 0, false, err, 0, nil)
		}
		cancel()
		self.done <- err
	}()
	output.observe(providerStatusStarted, 0, false, nil, 0, nil)
	return self
}

// Only the enclosing run calls this once, after its provider workers join.
func (self *providerStatusServer) close() error {
	if self == nil {
		return nil
	}
	self.handler.stopAdmission()
	err := errors.Join(self.server.Close(), <-self.done)
	self.handler.workers.Wait()
	return err
}

// Standard HTTP diagnostics are acknowledged as a closed notice. Their raw
// request/error text is never retained or copied into the output queues.
type providerStatusLogWriter struct{ diagnostics *providerDiagnostics }

// The standard library owns incoming bytes; this callback borrows them only.
func (self providerStatusLogWriter) Write(raw []byte) (int, error) {
	self.diagnostics.observe(providerStatusNotice, 0, false, nil, 0, nil)
	return len(raw), nil
}
