package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHTTPRPCProxyTerminatesWorkloadJSONRPCAndStopsCleanly(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"eth_chainId","id":1}`)
	hostSeen := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil || !bytes.Equal(body, payload) {
			t.Errorf("upstream body=%q error=%v", body, err)
		}
		hostSeen <- request.Host
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","result":"0x3b1","id":1}`))
	}))
	defer upstream.Close()
	config, err := rpcProxyConfigForEndpoint(upstream.URL, freeLoopbackAddress(t), freeLoopbackAddress(t))
	if err != nil {
		t.Fatal(err)
	}
	if !config.HTTP {
		t.Fatal("HTTP EVM endpoint did not select terminating proxy mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runRPCProxy(ctx, config) }()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	var response *http.Response
	for {
		response, err = client.Post("http://"+config.ListenAddress, "application/json", bytes.NewReader(payload))
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("HTTP RPC proxy did not become ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(body), `"result":"0x3b1"`) {
		t.Fatalf("HTTP RPC proxy response = %q", body)
	}
	if host := <-hostSeen; host != config.Upstream {
		t.Fatalf("upstream Host header = %q, want %q", host, config.Upstream)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP RPC proxy did not stop after cancellation")
	}
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
		{ListenAddress: "127.0.0.1:1", HealthAddress: "127.0.0.1:2", Upstream: "example.com:443", MaximumRequestsPerMinute: 1},
		{ListenAddress: "127.0.0.1:1", HealthAddress: "127.0.0.1:2", Upstream: "example.com:443", HTTP: true, MaximumRequestsPerMinute: 61},
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

func TestRPCProxyConfigAddsDefaultPublicTLSPort(t *testing.T) {
	config, err := rpcProxyConfigForEndpoint("https://test.chain.opentensor.ai", workloadRPCProxyAddress, workloadRPCProxyHealthAddress)
	if err != nil {
		t.Fatal(err)
	}
	if config.Upstream != "test.chain.opentensor.ai:443" || config.TLSServerName != "test.chain.opentensor.ai" || !config.HTTP {
		t.Fatalf("public RPC proxy config = %+v", config)
	}
}
