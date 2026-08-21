package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func TestRPCProxyCarriesWorkloadTrafficAndHealthChecksUpstream(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		for {
			connection, acceptErr := upstream.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()

	config := rpcProxyConfig{
		ListenAddress: freeLoopbackAddress(t), HealthAddress: freeLoopbackAddress(t),
		Upstream: upstream.Addr().String(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runRPCProxy(ctx, config) }()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, requestErr := client.Get("http://" + config.HealthAddress + "/healthz")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusNoContent {
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("RPC proxy did not become healthy: %v", requestErr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	connection, err := net.DialTimeout("tcp", config.ListenAddress, time.Second)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("bounded-rpc-proxy-round-trip")
	if _, err := connection.Write(payload); err != nil {
		t.Fatal(err)
	}
	returned := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, returned); err != nil {
		t.Fatal(err)
	}
	if string(returned) != string(payload) {
		t.Fatalf("proxy returned %q, want %q", returned, payload)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RPC proxy did not stop after cancellation")
	}
}

func TestRPCProxyConfigIsLoopbackScopedAndPreservesTLSIdentity(t *testing.T) {
	for _, config := range []rpcProxyConfig{
		{ListenAddress: "0.0.0.0:1", HealthAddress: "127.0.0.1:2", Upstream: "example.com:443"},
		{ListenAddress: "127.0.0.1:1", HealthAddress: "0.0.0.0:2", Upstream: "example.com:443"},
		{ListenAddress: "127.0.0.1:1", HealthAddress: "127.0.0.1:1", Upstream: "example.com:443"},
		{ListenAddress: "127.0.0.1:1", HealthAddress: "127.0.0.1:2", Upstream: "127.0.0.1:1"},
	} {
		if err := validateRPCProxyConfig(config); err == nil {
			t.Fatalf("unsafe proxy config was accepted: %+v", config)
		}
	}
	config, err := rpcProxyConfigForAuthority("wss://private-rpc.example:443")
	if err != nil {
		t.Fatal(err)
	}
	if config.Upstream != "private-rpc.example:443" || config.TLSServerName != "private-rpc.example" || config.ListenAddress != workloadRPCProxyAddress {
		t.Fatalf("TLS proxy config=%+v", config)
	}
}
