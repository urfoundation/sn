package main

// rpc_proxy.go provides simulator-owned transport boundaries in front of the
// selected Substrate and EVM RPCs. Workload processes use these loopback
// proxies; one non-faulted egress proxy owns the public-provider quota for both
// workloads and live observations. This lets fault campaigns remove workload
// RPC paths without mutating host firewall state, interrupting a shared node,
// or accidentally doubling the host-wide public request ceiling.

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
	publicEVMEgressAddress         = "127.0.0.10:19948"
	publicEVMEgressHealthAddress   = "127.0.0.10:19949"
	publicEVMEgressProcessID       = "subtensor-evm-egress"
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

// campaignEVMAuthority is the non-faulted, source-wide public-provider gate
// used by the harness while the supervised topology is live.
func campaignEVMAuthority() string { return publicEVMEgressAddress }

// campaignRPCConfig returns an I/O-only copy which routes every campaign EVM
// client through the central egress proxy. Canonical config/policy hashes stay
// unchanged, and the caller's resolved configuration is never mutated.
func campaignRPCConfig(cfg *ResolvedConfig) (*ResolvedConfig, error) {
	if cfg == nil || cfg.Config == nil || cfg.Public == nil {
		return nil, errors.New("campaign RPC configuration is incomplete")
	}
	resolved := *cfg
	harness := *cfg.Config
	public := *cfg.Public
	endpoint := "http://" + campaignEVMAuthority()
	resolved.Config = &harness
	resolved.Public = &public
	resolved.OperationalEVM = endpoint
	if cfg.OperationalRPCMode == rpcModePublicOverride {
		harness.LaunchInputs.PublicEVMMaximumRequestsPerMinute = 0
		resolved.Public.Chain.EVMPublicReadEndpoint = endpoint
	}
	return &resolved, nil
}

// validateCampaignRPCTransport accepts only the canonical route or the exact
// central-egress derivative produced above; no caller may supply an arbitrary
// local writer endpoint while retaining an approved plan hash.
func validateCampaignRPCTransport(authorized, runtime *ResolvedConfig) error {
	if authorized == nil || runtime == nil || authorized.Config == nil || runtime.Config == nil || authorized.Public == nil || runtime.Public == nil {
		return errors.New("campaign RPC transport identity is incomplete")
	}
	if runtime == authorized {
		return nil
	}
	want, err := campaignRPCConfig(authorized)
	if err != nil {
		return err
	}
	if runtime.ConfigHash != want.ConfigHash || runtime.PolicyHash != want.PolicyHash || runtime.ChainID != want.ChainID || runtime.Netuid != want.Netuid || runtime.OperationalRPCMode != want.OperationalRPCMode || runtime.OperationalSubstrate != want.OperationalSubstrate || runtime.OperationalEVM != want.OperationalEVM || runtime.Public.Chain.SubstratePublicReadEndpoint != want.Public.Chain.SubstratePublicReadEndpoint || runtime.Public.Chain.EVMPublicReadEndpoint != want.Public.Chain.EVMPublicReadEndpoint || runtime.Config.LaunchInputs.PublicEVMMaximumRequestsPerMinute != want.Config.LaunchInputs.PublicEVMMaximumRequestsPerMinute {
		return errors.New("campaign RPC transport is not the exact authorized egress derivative")
	}
	return nil
}

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
			base: http.DefaultTransport, gate: gate, maximumRetries: publicEVMMaximumRetries,
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
