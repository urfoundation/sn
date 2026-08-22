package main

// rpc_proxy.go provides a simulator-owned transport boundary in front of the
// shared private Subtensor node. Workload processes use this loopback proxy;
// sim-testnet's independent observations continue to use the configured
// private/public RPCs directly. This lets fault campaigns remove the workload
// RPC path without mutating host firewall state or interrupting a shared node.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	workloadRPCProxyAddress       = "127.0.0.10:19944"
	workloadRPCProxyHealthAddress = "127.0.0.10:19945"
	workloadRPCProxyProcessID     = "subtensor-rpc-proxy"
)

type rpcProxyConfig struct {
	ListenAddress string
	HealthAddress string
	Upstream      string
	TLSServerName string
}

func workloadRPCAuthority() string { return workloadRPCProxyAddress }

func rpcProxyConfigForAuthority(authority string) (rpcProxyConfig, error) {
	wsURL, _, err := authorityURLs(authority)
	if err != nil {
		return rpcProxyConfig{}, err
	}
	u, err := url.Parse(wsURL)
	if err != nil || u.Hostname() == "" || u.Port() == "" {
		return rpcProxyConfig{}, fmt.Errorf("private RPC authority %q has no explicit host and port", authority)
	}
	config := rpcProxyConfig{
		ListenAddress: workloadRPCProxyAddress,
		HealthAddress: workloadRPCProxyHealthAddress,
		Upstream:      u.Host,
	}
	if u.Scheme == "wss" {
		config.TLSServerName = u.Hostname()
	}
	if err := validateRPCProxyConfig(config); err != nil {
		return rpcProxyConfig{}, err
	}
	return config, nil
}

func validateRPCProxyConfig(config rpcProxyConfig) error {
	validateAddress := func(label, address string, loopback bool) error {
		host, port, err := net.SplitHostPort(address)
		if err != nil || strings.TrimSpace(port) == "" {
			return fmt.Errorf("%s address %q must have an explicit host and port", label, address)
		}
		if loopback {
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return fmt.Errorf("%s address %q must be an IP loopback address", label, address)
			}
		}
		return nil
	}
	if err := validateAddress("RPC proxy listen", config.ListenAddress, true); err != nil {
		return err
	}
	if err := validateAddress("RPC proxy health", config.HealthAddress, true); err != nil {
		return err
	}
	if config.ListenAddress == config.HealthAddress {
		return errors.New("RPC proxy data and health addresses must be distinct")
	}
	if err := validateAddress("RPC proxy upstream", config.Upstream, false); err != nil {
		return err
	}
	if config.Upstream == config.ListenAddress || config.Upstream == config.HealthAddress {
		return errors.New("RPC proxy upstream cannot point at the proxy itself")
	}
	return nil
}

func (config rpcProxyConfig) dial(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	if config.TLSServerName == "" {
		return dialer.DialContext(ctx, "tcp", config.Upstream)
	}
	tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{
		MinVersion: tls.VersionTLS12, ServerName: config.TLSServerName,
	}}
	return tlsDialer.DialContext(ctx, "tcp", config.Upstream)
}

type rpcProxyConnections struct {
	mu    sync.Mutex
	pairs map[net.Conn]net.Conn
}

func (connections *rpcProxyConnections) add(client, upstream net.Conn) {
	connections.mu.Lock()
	defer connections.mu.Unlock()
	connections.pairs[client] = upstream
}

func (connections *rpcProxyConnections) remove(client net.Conn) {
	connections.mu.Lock()
	defer connections.mu.Unlock()
	delete(connections.pairs, client)
}

func (connections *rpcProxyConnections) closeAll() {
	connections.mu.Lock()
	defer connections.mu.Unlock()
	for client, upstream := range connections.pairs {
		_ = client.Close()
		_ = upstream.Close()
	}
}

func proxyRPCConnection(ctx context.Context, config rpcProxyConfig, client net.Conn, connections *rpcProxyConnections) {
	defer client.Close()
	upstream, err := config.dial(ctx)
	if err != nil {
		return
	}
	defer upstream.Close()
	connections.add(client, upstream)
	defer connections.remove(client)

	copyDone := make(chan struct{}, 2)
	copyOne := func(destination, source net.Conn) {
		_, _ = io.Copy(destination, source)
		if tcp, ok := destination.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		copyDone <- struct{}{}
	}
	go copyOne(upstream, client)
	go copyOne(client, upstream)
	select {
	case <-ctx.Done():
	case <-copyDone:
	}
}

func runRPCProxy(ctx context.Context, config rpcProxyConfig) error {
	if err := validateRPCProxyConfig(config); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen RPC proxy: %w", err)
	}
	healthListener, err := net.Listen("tcp", config.HealthAddress)
	if err != nil {
		listener.Close()
		return fmt.Errorf("listen RPC proxy health: %w", err)
	}
	connections := &rpcProxyConnections{pairs: map[net.Conn]net.Conn{}}
	health := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/healthz" {
				http.NotFound(writer, request)
				return
			}
			probeCtx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
			defer cancel()
			connection, err := config.dial(probeCtx)
			if err != nil {
				http.Error(writer, "upstream unavailable", http.StatusServiceUnavailable)
				return
			}
			connection.Close()
			writer.WriteHeader(http.StatusNoContent)
		}),
	}
	healthErrors := make(chan error, 1)
	go func() {
		err := health.Serve(healthListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		healthErrors <- err
	}()
	go func() {
		<-ctx.Done()
		listener.Close()
		_ = health.Close()
		connections.closeAll()
	}()

	for {
		client, acceptErr := listener.Accept()
		if acceptErr != nil {
			select {
			case <-ctx.Done():
				return <-healthErrors
			default:
				return fmt.Errorf("accept RPC proxy connection: %w", acceptErr)
			}
		}
		go proxyRPCConnection(ctx, config, client, connections)
	}
}
