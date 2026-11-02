package miner

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Without --pprof nothing listens: the option is absent from a plain provide
// and an empty value starts no server.
func TestMinerPprofOffByDefault(t *testing.T) {
	for _, arguments := range [][]string{{"provide"}, {"auth-provide", "synthetic-code"}} {
		opts := parseArgsForTest(t, arguments)
		value := minerPprofOption(opts)
		if value != "" {
			t.Fatalf("parse %v: pprof option = %q; want off", arguments, value)
		}
		server, err := startMinerPprof(value)
		if server != nil || err != nil {
			t.Fatalf("parse %v: started %v, %v; want no server", arguments, server, err)
		}
		if err := server.close(); err != nil {
			t.Fatalf("closing no server: %v", err)
		}
	}
}

// A value without a host listens on 127.0.0.1, including ":port", which
// net.Listen alone would take as every interface. Only a typed host goes
// beyond loopback.
func TestMinerPprofListenAddressDefaultsToLoopback(t *testing.T) {
	for value, want := range map[string]string{
		"6060":           "127.0.0.1:6060",
		":6060":          "127.0.0.1:6060",
		"06060":          "127.0.0.1:6060",
		"0":              "127.0.0.1:0",
		"127.0.0.1:6060": "127.0.0.1:6060",
		"[::1]:6060":     "[::1]:6060",
		"localhost:6060": "localhost:6060",
		"0.0.0.0:6060":   "0.0.0.0:6060",
		"[::]:6060":      "[::]:6060",
	} {
		address, err := minerPprofListenAddress(value)
		if err != nil || address != want {
			t.Errorf("--pprof=%s listens on %q, %v; want %q", value, address, err, want)
		}
	}
	for _, value := range []string{"localhost", "::1", "[::1]", "127.0.0.1:", "http", "-1", "65536", "6060:", "1.2.3.4:6060:7"} {
		if address, err := minerPprofListenAddress(value); err == nil {
			t.Errorf("--pprof=%s was accepted as %q", value, address)
		}
	}
}

// The socket a bare port opens is bound to 127.0.0.1, not every interface.
func TestMinerPprofBareBindIsLoopbackOnly(t *testing.T) {
	for _, value := range []string{"0", ":0"} {
		server, err := startMinerPprof(value)
		if err != nil {
			t.Fatalf("--pprof=%s: %v", value, err)
		}
		address, ok := server.addr().(*net.TCPAddr)
		closeErr := server.close()
		if !ok || !address.IP.Equal(net.IPv4(127, 0, 0, 1)) {
			t.Fatalf("--pprof=%s bound %v; want 127.0.0.1", value, server.addr())
		}
		if closeErr != nil {
			t.Fatalf("--pprof=%s close: %v", value, closeErr)
		}
	}
}

// An explicit request never runs without its endpoint: a busy port is an
// error, as is a value that is not a port or host:port.
func TestMinerPprofRefusesBusyPortAndBadValue(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	_, port, _ := net.SplitHostPort(busy.Addr().String())
	if server, err := startMinerPprof(port); err == nil {
		server.close()
		t.Fatalf("--pprof=%s started on a port in use", port)
	}
	if server, err := startMinerPprof("not-a-port"); err == nil || !strings.Contains(err.Error(), "--pprof=not-a-port") {
		server.close()
		t.Fatalf("--pprof=not-a-port: %v", err)
	}
}

func pprofGet(t *testing.T, client *http.Client, url string) (int, http.Header, []byte) {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return response.StatusCode, response.Header, body
}

// When enabled, /debug/pprof/ serves the standard handlers, with the CPU
// profile, goroutine dumps and heap of this process, but not its command line.
func TestMinerPprofServesProfiles(t *testing.T) {
	server, err := startMinerPprof("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	client := &http.Client{Timeout: time.Minute, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	base := "http://" + server.addr().String()

	status, _, body := pprofGet(t, client, base+"/debug/pprof/")
	if status != http.StatusOK {
		t.Fatalf("index: %d %s", status, body)
	}
	for _, want := range []string{"goroutine", "heap", "allocs", "profile", "trace"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("index does not list %s:\n%s", want, body)
		}
	}
	status, _, body = pprofGet(t, client, base+"/debug/pprof/goroutine?debug=1")
	if status != http.StatusOK || !bytes.HasPrefix(body, []byte("goroutine profile: total ")) {
		t.Fatalf("goroutine?debug=1: %d %.200s", status, body)
	}
	// the full dump of this process includes this test, blocked in its GET
	status, _, body = pprofGet(t, client, base+"/debug/pprof/goroutine?debug=2")
	if status != http.StatusOK || !bytes.Contains(body, []byte("miner.TestMinerPprofServesProfiles")) {
		t.Fatalf("goroutine?debug=2: %d %.400s", status, body)
	}
	status, _, body = pprofGet(t, client, base+"/debug/pprof/heap?debug=1")
	if status != http.StatusOK || !bytes.HasPrefix(body, []byte("heap profile: ")) {
		t.Fatalf("heap?debug=1: %d %.200s", status, body)
	}
	// a CPU profile is a gzipped profile.proto
	status, header, body := pprofGet(t, client, base+"/debug/pprof/profile?seconds=1")
	if status != http.StatusOK || header.Get("Content-Type") != "application/octet-stream" || !bytes.HasPrefix(body, []byte{0x1f, 0x8b}) {
		t.Fatalf("profile: %d %q %d bytes", status, header.Get("Content-Type"), len(body))
	}
	for _, path := range []string{"/debug/pprof/cmdline", "/", "/debug/vars"} {
		if status, _, body := pprofGet(t, client, base+path); status != http.StatusNotFound || bytes.Contains(body, []byte(os.Args[0])) {
			t.Fatalf("%s: %d %.200s; want 404", path, status, body)
		}
	}
}

// Each signal value yields one dump, between marker lines, written once.
func TestGoroutineDumpsWriteOneDumpPerSignal(t *testing.T) {
	signals := make(chan os.Signal)
	writes := make(chan string, 4)
	stop := goroutineDumps(signals, "test", writerFunc(func(b []byte) (int, error) {
		writes <- string(b)
		return len(b), nil
	}))
	defer stop()
	for range 2 {
		signals <- os.Interrupt
		dump := <-writes
		if !strings.HasPrefix(dump, "==== goroutine dump (test) ") || !strings.Contains(dump, "\ngoroutine profile: total ") || !strings.HasSuffix(dump, "==== end goroutine dump ====\n") {
			t.Fatalf("dump:\n%.400s", dump)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (self writerFunc) Write(b []byte) (int, error) {
	return self(b)
}
