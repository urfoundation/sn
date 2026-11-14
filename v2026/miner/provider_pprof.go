// Opt-in diagnostics for a running provide (urnetwork/connect#227): Go
// profiles over HTTP with --pprof=<addr>, and a goroutine dump on SIGUSR1
// (provider_pprof_unix.go). SIGQUIT is a graceful shutdown here, so Go's own
// SIGQUIT stack dump is not available, and the release binary has no
// profiling endpoint unless --pprof asks for one.
package miner

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	httppprof "net/http/pprof"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
)

// minerPprofLoopbackHost is where --pprof listens when its value names no host.
const minerPprofLoopbackHost = "127.0.0.1"

// minerPprofOption is the value of --pprof, or "" (off) when it is absent.
func minerPprofOption(opts docopt.Opts) string {
	value, _ := opts.String("--pprof")
	return strings.TrimSpace(value)
}

// minerPprofListenAddress is the address --pprof=<addr> listens on. A bare
// port, or host:port with an empty host (":6060", which net.Listen would take
// as every interface), listens on 127.0.0.1 only. Any other host is used as
// given, so 0.0.0.0 or [::] listens beyond this machine only when it is typed.
func minerPprofListenAddress(value string) (string, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		host, port = "", value
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return "", fmt.Errorf("--pprof=%s: give a port, such as 6060, or host:port", value)
	}
	if host == "" {
		host = minerPprofLoopbackHost
	}
	return net.JoinHostPort(host, strconv.FormatUint(number, 10)), nil
}

// minerPprofHandler serves the standard net/http/pprof handlers on a mux of
// its own; nothing else in the process serves http.DefaultServeMux, where the
// package also registers them. /debug/pprof/ serves the index and every named
// profile: goroutine, heap, allocs, block, mutex and threadcreate.
// /debug/pprof/cmdline is left out, because the command line can hold an auth
// code or a --password (auth-provide); Index answers it "Unknown profile" (404).
func minerPprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/", httppprof.Index)
	mux.HandleFunc("GET /debug/pprof/profile", httppprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", httppprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", httppprof.Trace)
	return mux
}

// minerPprofServer owns the listener and Serve goroutine of --pprof.
type minerPprofServer struct {
	listener net.Listener
	server   *http.Server
	done     chan struct{}
}

// startMinerPprof serves Go profiles at value (see minerPprofListenAddress).
// An empty value is off: it returns nil and listens on nothing. A refused
// address or a failed bind is an error, so an explicit request never runs
// silently without its endpoint.
func startMinerPprof(value string) (*minerPprofServer, error) {
	if value == "" {
		return nil, nil
	}
	address, err := minerPprofListenAddress(value)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("--pprof=%s: %w", value, err)
	}
	self := &minerPprofServer{
		listener: listener,
		// No write timeout: a CPU profile or trace streams for as long as it asks.
		server: &http.Server{Handler: minerPprofHandler(), ReadHeaderTimeout: 10 * time.Second},
		done:   make(chan struct{}),
	}
	go func() {
		defer close(self.done)
		_ = self.server.Serve(listener)
	}()
	return self, nil
}

// addr is the address the endpoint actually listens on.
func (self *minerPprofServer) addr() net.Addr {
	return self.listener.Addr()
}

// close stops the endpoint and joins its Serve goroutine; nil is a no-op.
func (self *minerPprofServer) close() error {
	if self == nil {
		return nil
	}
	err := self.server.Close()
	<-self.done
	return err
}

// startProvidePprof starts --pprof for a provide, or exits when its value is
// refused or its address can't be bound. It runs before provider start can
// move stderr to miner.log, so the address prints where it was asked for.
func startProvidePprof(opts docopt.Opts) *minerPprofServer {
	server, err := startMinerPprof(minerPprofOption(opts))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if server != nil {
		fmt.Fprintf(os.Stderr, "pprof: serving Go profiles at http://%s/debug/pprof/\n", server.addr())
	}
	return server
}

// writeGoroutineDump writes every goroutine, grouped by identical stack with
// a count (the debug=1 goroutine profile, compact even with thousands of
// providers), between two marker lines. It is one Write, so log lines from
// other goroutines can't interleave with it.
func writeGoroutineDump(w io.Writer, reason string) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "==== goroutine dump (%s) %s ====\n", reason, time.Now().UTC().Format(time.RFC3339))
	if err := pprof.Lookup("goroutine").WriteTo(&b, 1); err != nil {
		return err
	}
	fmt.Fprintf(&b, "==== end goroutine dump ====\n")
	_, err := w.Write(b.Bytes())
	return err
}

// goroutineDumps writes a goroutine dump to w for each value received on
// signals, until the returned stop runs. stop returns once no dump is being
// written.
func goroutineDumps(signals <-chan os.Signal, reason string, w io.Writer) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			case <-signals:
				_ = writeGoroutineDump(w, reason)
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}
