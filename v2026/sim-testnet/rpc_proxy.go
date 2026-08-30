package main

// rpc_proxy.go provides simulator-owned transport boundaries in front of the
// selected Substrate and EVM RPCs. Workload processes use these loopback
// proxies; sim-testnet's independent observations use the selected operational
// RPCs directly. This lets fault campaigns remove workload RPC paths without
// mutating host firewall state or interrupting a shared node.

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	workloadRPCProxyAddress        = "127.0.0.10:19944"
	workloadRPCProxyHealthAddress  = "127.0.0.10:19945"
	workloadRPCProxyProcessID      = "subtensor-rpc-proxy"
	workloadSubstrateProxyAddress  = "127.0.0.10:19946"
	workloadSubstrateHealthAddress = "127.0.0.10:19947"
	workloadSubstrateProcessID     = "subtensor-substrate-rpc-proxy"
)

type rpcProxyConfig struct {
	ListenAddress            string
	HealthAddress            string
	Upstream                 string
	TLSServerName            string
	HTTP                     bool
	MaximumRequestsPerMinute int
}

func workloadRPCAuthority() string { return workloadRPCProxyAddress }

func workloadSubstrateRPCAuthority() string { return workloadSubstrateProxyAddress }

func rpcProxyConfigForAuthority(authority string) (rpcProxyConfig, error) {
	wsURL, _, err := authorityURLs(authority)
	if err != nil {
		return rpcProxyConfig{}, err
	}
	return rpcProxyConfigForEndpoint(wsURL, workloadRPCProxyAddress, workloadRPCProxyHealthAddress)
}

// Terminate optional upstream TLS while preserving a plain loopback endpoint
// for release components. Default scheme ports keep official public URLs bare.
func rpcProxyConfigForEndpoint(endpoint, listenAddress, healthAddress string) (rpcProxyConfig, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return rpcProxyConfig{}, fmt.Errorf("RPC endpoint %q has no host", endpoint)
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "http", "ws":
			port = "80"
		case "https", "wss":
			port = "443"
		default:
			return rpcProxyConfig{}, fmt.Errorf("RPC endpoint %q has unsupported scheme", endpoint)
		}
	}
	config := rpcProxyConfig{
		ListenAddress: listenAddress,
		HealthAddress: healthAddress,
		Upstream:      net.JoinHostPort(u.Hostname(), port),
		HTTP:          u.Scheme == "http" || u.Scheme == "https",
	}
	if u.Scheme == "https" || u.Scheme == "wss" {
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
	if config.MaximumRequestsPerMinute < 0 || config.MaximumRequestsPerMinute > 60 {
		return errors.New("RPC proxy request ceiling must be in [0,60] requests per minute")
	}
	if config.MaximumRequestsPerMinute > 0 && !config.HTTP {
		return errors.New("RPC proxy request ceiling requires HTTP mode")
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
	if config.HTTP {
		return runHTTPRPCProxy(ctx, config)
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

// EVM workloads use a source-wide provider quota. Terminate HTTP locally so
// every operator, miner, and validator shares one bounded gate and the same
// Retry-After behavior as the setup executor. Substrate WebSockets retain the
// raw fault-injection proxy above.
func runHTTPRPCProxy(ctx context.Context, config rpcProxyConfig) error {
	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen HTTP RPC proxy: %w", err)
	}
	healthListener, err := net.Listen("tcp", config.HealthAddress)
	if err != nil {
		listener.Close()
		return fmt.Errorf("listen HTTP RPC proxy health: %w", err)
	}
	scheme := "http"
	if config.TLSServerName != "" {
		scheme = "https"
	}
	target := &url.URL{Scheme: scheme, Host: config.Upstream}
	proxy := httputil.NewSingleHostReverseProxy(target)
	direct := proxy.Director
	proxy.Director = func(request *http.Request) {
		direct(request)
		request.Host = target.Host
	}
	if config.MaximumRequestsPerMinute > 0 {
		gate, gateErr := newRPCRequestGate(config.MaximumRequestsPerMinute)
		if gateErr != nil {
			listener.Close()
			healthListener.Close()
			return gateErr
		}
		proxy.Transport = &rateLimitedRetryTransport{
			base: http.DefaultTransport, gate: gate, maximum429Retries: publicEVMMaximum429Retries,
			defaultRetryAfter: 5 * time.Second, maximumRetryAfter: publicEVMMaximumRetryAfter,
		}
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
		http.Error(writer, proxyErr.Error(), http.StatusBadGateway)
	}
	dataServer := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	healthServer := &http.Server{ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			http.NotFound(writer, request)
			return
		}
		probeCtx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
		defer cancel()
		connection, dialErr := config.dial(probeCtx)
		if dialErr != nil {
			http.Error(writer, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		connection.Close()
		writer.WriteHeader(http.StatusNoContent)
	})}
	errorsOut := make(chan error, 2)
	serve := func(server *http.Server, listener net.Listener) {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errorsOut <- err
	}
	go serve(dataServer, listener)
	go serve(healthServer, healthListener)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		dataErr := dataServer.Shutdown(shutdownCtx)
		healthErr := healthServer.Shutdown(shutdownCtx)
		if dataErr != nil {
			return dataErr
		}
		return healthErr
	case serveErr := <-errorsOut:
		_ = dataServer.Close()
		_ = healthServer.Close()
		return serveErr
	}
}
