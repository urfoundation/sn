package miner

// swarm.go runs many production DeviceLocal providers inside one bounded
// process. It exists for large integration campaigns: every member retains an
// independent platform identity, state directory, wallet and source prefix,
// while process count and file descriptors remain operationally tractable.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/clientauth"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
)

const ProviderSwarmSchema = "urnetwork-provider-swarm-v1"

type ProviderSwarmMember struct {
	ID             string `json:"id"`
	APIURL         string `json:"api_url"`
	ConnectURL     string `json:"connect_url"`
	DNSPumpHost    string `json:"dns_pump_host"`
	StateDir       string `json:"state_dir"`
	Wallet         string `json:"wallet"`
	WalletSeedFile string `json:"wallet_seed_file"`
	SourceIP       string `json:"source_ip"`
}

type ProviderSwarmConfig struct {
	Schema        string                `json:"schema"`
	ListenAddress string                `json:"listen_address"`
	Members       []ProviderSwarmMember `json:"members"`
}

type providerSwarmStatus struct {
	Schema     string            `json:"schema"`
	Configured int               `json:"configured"`
	Running    int               `json:"running"`
	Disabled   []string          `json:"disabled,omitempty"`
	Failures   map[string]string `json:"failures,omitempty"`
}

// ProviderSwarm is safe for concurrent status reads. A member authentication
// rejection is terminal for the whole swarm so supervision cannot mistake a
// partially missing miner population for a healthy topology.
type ProviderSwarm struct {
	config             *ProviderSwarmConfig
	stateLock          sync.Mutex
	members            map[string]ProviderSwarmMember
	running            map[string]bool
	disabled           map[string]bool
	failures           map[string]string
	instances          map[string]*providerSwarmInstance
	memberOperations   map[string]*providerSwarmMemberOperation
	memberGenerations  map[string]*providerSwarmMemberOperation
	operationWaitGroup sync.WaitGroup
	stopping           bool
	startTimeout       time.Duration
	runCtx             context.Context
	runCancel          context.CancelFunc
	terminalErrors     chan error
	startMember        func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error)
}

func LoadProviderSwarmConfig(path string) (*ProviderSwarmConfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	decoder := json.NewDecoder(bufio.NewReader(f))
	decoder.DisallowUnknownFields()
	var config ProviderSwarmConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode provider swarm: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("provider swarm config contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode provider swarm trailing data: %w", err)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (self ProviderSwarmConfig) Validate() error {
	if self.Schema != ProviderSwarmSchema || len(self.Members) == 0 || len(self.Members) > 64 {
		return errors.New("provider swarm requires schema v1 and between 1 and 64 members")
	}
	listen, err := netip.ParseAddrPort(self.ListenAddress)
	if err != nil || !listen.Addr().IsLoopback() || listen.Port() == 0 {
		return errors.New("provider swarm status listener must be a nonzero loopback address")
	}
	seenIDs := map[string]bool{}
	seenStates := map[string]bool{}
	seenSources := map[string]bool{}
	for index, member := range self.Members {
		if member.ID == "" || seenIDs[member.ID] || strings.ContainsAny(member.ID, `/\\`) {
			return fmt.Errorf("member %d has an empty, duplicate or unsafe id", index)
		}
		seenIDs[member.ID] = true
		if !filepath.IsAbs(member.StateDir) || seenStates[filepath.Clean(member.StateDir)] {
			return fmt.Errorf("member %s state_dir must be absolute and unique", member.ID)
		}
		seenStates[filepath.Clean(member.StateDir)] = true
		if err := validateApiUrl(member.APIURL); err != nil {
			return fmt.Errorf("member %s: %w", member.ID, err)
		}
		if err := validateConnectUrl(member.ConnectURL); err != nil {
			return fmt.Errorf("member %s: %w", member.ID, err)
		}
		connectURL, _ := url.Parse(member.ConnectURL)
		pumpHost := strings.TrimSpace(member.DNSPumpHost)
		if pumpHost == "" {
			if isLoopbackURLHost(connectURL) {
				return fmt.Errorf("member %s loopback connect_url requires dns_pump_host on the same provisioned ingress", member.ID)
			}
		} else {
			pumpURL, pumpErr := url.Parse("dns://" + pumpHost)
			if pumpErr != nil || pumpURL.Hostname() == "" || pumpURL.Port() != "" ||
				pumpURL.User != nil || pumpURL.Path != "" || pumpURL.RawQuery != "" || pumpURL.Fragment != "" {
				return fmt.Errorf("member %s dns_pump_host must be a host without a port or path", member.ID)
			}
			if isLoopbackURLHost(connectURL) && !strings.EqualFold(
				strings.TrimSuffix(connectURL.Hostname(), "."),
				strings.TrimSuffix(pumpURL.Hostname(), "."),
			) {
				return fmt.Errorf("member %s loopback connect_url requires dns_pump_host on the same provisioned ingress", member.ID)
			}
		}
		if _, err := ss58.DecodeWithPrefix(member.Wallet, ss58.BittensorPrefix); err != nil {
			return fmt.Errorf("member %s wallet: %w", member.ID, err)
		}
		if _, err := swarmMemberWalletKey(member); err != nil {
			return fmt.Errorf("member %s wallet identity: %w", member.ID, err)
		}
		source, err := netip.ParseAddr(member.SourceIP)
		if err != nil || !source.Is4() || !source.IsLoopback() || seenSources[source.String()] {
			return fmt.Errorf("member %s source_ip must be a unique IPv4 loopback address", member.ID)
		}
		seenSources[source.String()] = true
		for _, name := range []string{"jwt", ".provider.jwt", ".provider.key"} {
			info, statErr := os.Stat(filepath.Join(member.StateDir, name))
			if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
				return fmt.Errorf("member %s state file %s is missing or not private", member.ID, name)
			}
		}
	}
	return nil
}

func testEgressDialContextForIP(raw string) (*connect.DialContextSettings, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || !addr.Is4() || !addr.IsLoopback() {
		return nil, errors.New("test egress source must be an IPv4 loopback address")
	}
	source := net.IP(append([]byte(nil), addr.AsSlice()...))
	return &connect.DialContextSettings{
		DialContext: func(ctx context.Context, network, destination string) (net.Conn, error) {
			dialer := &net.Dialer{}
			switch network {
			case "tcp", "tcp4":
				dialer.LocalAddr = &net.TCPAddr{IP: append(net.IP(nil), source...)}
			case "udp", "udp4":
				dialer.LocalAddr = &net.UDPAddr{IP: append(net.IP(nil), source...)}
			default:
				return nil, fmt.Errorf("test egress source %s does not support network %q", addr, network)
			}
			return dialer.DialContext(ctx, network, destination)
		},
		PacketConnFactory: func(ctx context.Context) (net.PacketConn, error) {
			listenConfig := &net.ListenConfig{}
			return listenConfig.ListenPacket(ctx, "udp4", net.JoinHostPort(addr.String(), "0"))
		},
	}, nil
}

func readProviderTLSState(stateDir string) ([]byte, []byte, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, ".provider.cert"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var certificatePEM []byte
	var keyPEM []byte
	for rest := b; len(rest) > 0; {
		block, next := pem.Decode(rest)
		if block == nil {
			return nil, nil, errors.New("provider TLS state contains invalid PEM")
		}
		encoded := pem.EncodeToMemory(block)
		if block.Type == "CERTIFICATE" {
			certificatePEM = append(certificatePEM, encoded...)
		} else if strings.Contains(block.Type, "PRIVATE KEY") {
			keyPEM = append(keyPEM, encoded...)
		}
		rest = next
	}
	return certificatePEM, keyPEM, nil
}

func writeProviderTLSState(stateDir string, certificatePEM, keyPEM []byte) error {
	if len(certificatePEM) == 0 || len(keyPEM) == 0 {
		return nil
	}
	return os.WriteFile(filepath.Join(stateDir, ".provider.cert"), append(append([]byte(nil), certificatePEM...), keyPEM...), 0o600)
}

// Loading never creates a wallet: the simulator provisions the existing payout
// role, and the address must match before any request can leave this process.
func swarmMemberWalletKey(member ProviderSwarmMember) (*crv4.Keypair, error) {
	if !filepath.IsAbs(member.WalletSeedFile) || filepath.Clean(member.WalletSeedFile) != member.WalletSeedFile {
		return nil, errors.New("wallet_seed_file must be an absolute canonical path")
	}
	seed, err := crv4.LoadSeedFile(member.WalletSeedFile)
	if err != nil {
		return nil, err
	}
	key, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return nil, err
	}
	if key.Address() != member.Wallet {
		return nil, errors.New("wallet seed differs from the configured payout address")
	}
	return key, nil
}

// Signs a fresh challenge and submits it once through the configured source
// and TLS policy. The temporary client releases its connections before return.
func setSwarmMemberWallet(ctx context.Context, member ProviderSwarmMember, settings *connect.ClientStrategySettings) error {
	key, err := swarmMemberWalletKey(member)
	if err != nil {
		return err
	}
	jwt, err := clientauth.ReadToken(filepath.Join(member.StateDir, "jwt"))
	if err != nil {
		return err
	}
	providerJWT, err := clientauth.ReadToken(filepath.Join(member.StateDir, ".provider.jwt"))
	if err != nil {
		return err
	}
	providerID, err := clientauth.ClientIdFromJwt(providerJWT)
	if err != nil {
		return fmt.Errorf("wallet provider identity: %w", err)
	}
	clientID, err := sdk.ParseId(providerID.String())
	if err != nil {
		return err
	}
	// A signed wallet write consumes its challenge even when its reply is
	// lost. Each operation gets one attempt, including at the transport layer.
	transport := &http.Transport{
		DialContext: settings.ConnectSettings.DialContext, TLSClientConfig: settings.TlsConfig,
		TLSHandshakeTimeout: settings.TlsTimeout, ResponseHeaderTimeout: settings.ConnectTimeout,
		IdleConnTimeout: settings.IdleConnTimeout, ForceAttemptHTTP2: true,
	}
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	maxResponseBytes := settings.MaxHttpResponseBodyBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = connect.DefaultMaxHttpResponseBodyBytes
	}
	httpPost := func(ctx context.Context, requestUrl string, requestBytes []byte, byJwt string) ([]byte, error) {
		requestCtx := ctx
		if 0 < settings.RequestTimeout {
			var cancel context.CancelFunc
			requestCtx, cancel = context.WithTimeout(ctx, settings.RequestTimeout)
			defer cancel()
		}
		request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, requestUrl, bytes.NewReader(requestBytes))
		if err != nil {
			return nil, err
		}
		request.GetBody = nil
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+byJwt)
		for name, values := range settings.ExtraHeaders {
			request.Header[name] = append([]string(nil), values...)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		return readSwarmWalletResponse(ctx, requestCtx, response, maxResponseBytes)
	}
	challenge, err := connect.HttpPostWithRawFunction(ctx, httpPost, member.APIURL+"/auth/wallet-challenge",
		&sdk.AuthWalletChallengeArgs{WalletAddress: member.Wallet, Blockchain: "TAO"}, jwt,
		&sdk.AuthWalletChallengeResult{}, connect.NewNoopApiCallback[*sdk.AuthWalletChallengeResult]())
	if err != nil {
		return fmt.Errorf("wallet challenge: %w", err)
	}
	if challenge == nil {
		return errors.New("wallet challenge returned no result")
	}
	if challenge.Error != nil {
		return fmt.Errorf("wallet challenge: %s", challenge.Error.Message)
	}
	if challenge.MessageTemplate == "" {
		return errors.New("wallet challenge returned an empty message")
	}
	// The server consumes this exact challenge once. Sign its bytes using the
	// coldkey's substrate context; never reuse a previous challenge/signature.
	signature, err := key.Sign([]byte(challenge.MessageTemplate))
	if err != nil {
		return err
	}
	result, err := connect.HttpPostWithRawFunction(ctx, httpPost, member.APIURL+"/sn/wallet", &sdk.SnSetWalletArgs{
		ColdkeySs58: member.Wallet, ClientId: clientID,
		Signature: "0x" + hex.EncodeToString(signature), Message: challenge.MessageTemplate,
	}, jwt, &sdk.SnSetWalletResult{}, connect.NewNoopApiCallback[*sdk.SnSetWalletResult]())
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("wallet set returned no result")
	}
	if result.Error != nil {
		return errors.New(result.Error.Message)
	}
	return nil
}

type providerSwarmInstance struct {
	networkSpace      *sdk.NetworkSpace
	device            *sdk.DeviceLocal
	refreshSub        sdk.Sub
	logoutSub         sdk.Sub
	cancel            context.CancelFunc
	connectedOverride func() bool
}

// Reports live carrier plus processed current-key readiness. The supervisor
// waits this boundary before starting validator demand. Tests may replace the
// complete readiness result without constructing the full SDK graph.
func (self *providerSwarmInstance) connected() bool {
	if self == nil {
		return false
	}
	if self.connectedOverride != nil {
		return self.connectedOverride()
	}
	return self.device != nil && self.device.GetProviderReady()
}

func (self *providerSwarmInstance) close() {
	if self == nil {
		return
	}
	if self.cancel != nil {
		self.cancel()
	}
	if self.device != nil {
		_ = self.device.CloseAndWait(context.Background())
	}
	if self.refreshSub != nil {
		self.refreshSub.Close()
	}
	if self.logoutSub != nil {
		self.logoutSub.Close()
	}
	if self.networkSpace != nil {
		self.networkSpace.Close()
	}
}

func startSwarmMember(ctx context.Context, member ProviderSwarmMember, failed func(error)) (*providerSwarmInstance, error) {
	dialSettings, err := testEgressDialContextForIP(member.SourceIP)
	if err != nil {
		return nil, err
	}
	strategySettings := connect.DefaultClientStrategySettings()
	// Testnet registration waits for a shared, rate-limited chain observation.
	// Its one-shot wallet write needs more than the normal 15-second budget.
	strategySettings.RequestTimeout = 120 * time.Second
	// ConnectTimeout also bounds response headers for the processed reply.
	strategySettings.ConnectTimeout = 45 * time.Second
	strategySettings.DialContextSettings = dialSettings
	if err := setSwarmMemberWallet(ctx, member, strategySettings); err != nil {
		return nil, fmt.Errorf("set wallet: %w", err)
	}
	byClientJWT, err := clientauth.ReadToken(filepath.Join(member.StateDir, ".provider.jwt"))
	if err != nil {
		return nil, err
	}
	seed, err := os.ReadFile(filepath.Join(member.StateDir, ".provider.key"))
	if err != nil || len(seed) != 32 {
		return nil, errors.New("provider identity seed is not exactly 32 bytes")
	}
	certificatePEM, keyPEM, err := readProviderTLSState(member.StateDir)
	if err != nil {
		return nil, err
	}
	memberCtx, memberCancel := context.WithCancel(ctx)
	networkSpace := sdk.NewNetworkSpaceWithUrls(memberCtx, member.APIURL, member.ConnectURL, strategySettings)
	api := networkSpace.GetApi()
	clientJWTPath := filepath.Join(member.StateDir, ".provider.jwt")
	refreshSub := api.AddJwtRefreshListener(clientauth.JwtRefreshListenerFunc(func(jwt string) {
		if err := clientauth.WriteToken(clientJWTPath, jwt); err != nil {
			failed(fmt.Errorf("persist refreshed client JWT: %w", err))
		}
	}))
	logoutSub := api.AddAuthLogoutListener(clientauth.AuthLogoutListenerFunc(func() {
		failed(errors.New("provider authentication was rejected"))
	}))
	deviceSettings := swarmMemberDeviceSettings(
		member, sdk.NewDeviceLocalKeyMaterial(seed, certificatePEM, keyPEM), dialSettings)
	device, err := sdk.NewDeviceLocal(networkSpace, byClientJWT, "provider swarm "+runtime.GOOS+" "+RequireVersion(), "", RequireVersion(), sdk.NewId(), deviceSettings)
	if err != nil {
		refreshSub.Close()
		logoutSub.Close()
		networkSpace.Close()
		memberCancel()
		return nil, err
	}
	device.SetProvideControlMode(sdk.ProvideControlModeAlways)
	keyMaterial := device.GetKeyMaterial()
	if err := writeProviderTLSState(member.StateDir, keyMaterial.GetProvideTlsCertificatePem(), keyMaterial.GetProvideTlsPrivateKeyPem()); err != nil {
		_ = device.CloseAndWait(context.Background())
		refreshSub.Close()
		logoutSub.Close()
		networkSpace.Close()
		memberCancel()
		return nil, err
	}
	return &providerSwarmInstance{networkSpace: networkSpace, device: device, refreshSub: refreshSub, logoutSub: logoutSub, cancel: memberCancel}, nil
}

// The device settings of one swarm member. Named rather than inlined so a test
// can read what the swarm asks of the sdk without standing up a member.
func swarmMemberDeviceSettings(
	member ProviderSwarmMember,
	keyMaterial *sdk.DeviceLocalKeyMaterial,
	dialSettings *connect.DialContextSettings,
) *sdk.DeviceLocalSettings {
	deviceSettings := sdk.DefaultDeviceLocalSettings()
	deviceSettings.ClientSettings.ClientKeyRegistrationRequired = true
	// One process runs every swarm member, and a host holds one extender
	// identity and binds the carrier ports once, so no member runs the
	// provider extender role (connect/EXTENDER.md G1, G2). A standalone
	// provider is the shape that becomes an extender.
	deviceSettings.ProvideExtenderEnabled = false
	deviceSettings.KeyMaterial = keyMaterial
	deviceSettings.ProviderDialContextSettings = dialSettings
	deviceSettings.DnsPumpHost = member.DNSPumpHost
	return deviceSettings
}

func NewProviderSwarm(config *ProviderSwarmConfig) (*ProviderSwarm, error) {
	if config == nil {
		return nil, errors.New("provider swarm config is nil")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	members := make(map[string]ProviderSwarmMember, len(config.Members))
	for _, member := range config.Members {
		members[member.ID] = member
	}
	return &ProviderSwarm{
		config: config, members: members, running: map[string]bool{}, disabled: map[string]bool{},
		failures: map[string]string{}, instances: map[string]*providerSwarmInstance{}, startMember: startSwarmMember,
		memberOperations: map[string]*providerSwarmMemberOperation{}, memberGenerations: map[string]*providerSwarmMemberOperation{},
		startTimeout: 3 * time.Minute,
	}, nil
}

func (self *ProviderSwarm) status() providerSwarmStatus {
	self.stateLock.Lock()
	failures := map[string]string{}
	for id, detail := range self.failures {
		failures[id] = detail
	}
	disabled := make([]string, 0, len(self.disabled))
	for id := range self.disabled {
		disabled = append(disabled, id)
	}
	instances := make([]*providerSwarmInstance, 0, len(self.running))
	for id := range self.running {
		if instance := self.instances[id]; instance != nil {
			instances = append(instances, instance)
		}
	}
	configured := len(self.config.Members)
	self.stateLock.Unlock()
	running := 0
	for _, instance := range instances {
		if instance.connected() {
			running++
		}
	}
	sort.Strings(disabled)
	return providerSwarmStatus{Schema: ProviderSwarmSchema, Configured: configured, Running: running, Disabled: disabled, Failures: failures}
}

func (self *ProviderSwarm) setFailure(id string, err error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	delete(self.running, id)
	self.failures[id] = err.Error()
}

// Runs the server and owns every admitted control operation until its cleanup
// has joined. Cancellation precedes both server shutdown and member teardown.
func (self *ProviderSwarm) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	terminalErrors := make(chan error, 1)
	self.stateLock.Lock()
	if self.runCtx != nil || self.stopping {
		self.stateLock.Unlock()
		cancel()
		return errors.New("provider swarm is already running or stopped")
	}
	self.runCtx = runCtx
	self.runCancel = cancel
	self.terminalErrors = terminalErrors
	for id := range self.members {
		self.disabled[id] = true
	}
	self.stateLock.Unlock()
	server := &http.Server{Addr: self.config.ListenAddress, Handler: self}
	serverDone := make(chan struct{})
	defer func() {
		self.stopMembers()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
		<-serverDone
	}()
	serverErrors := make(chan error, 1)
	go func() {
		defer close(serverDone)
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
			cancel()
		}
	}()
	members := append([]ProviderSwarmMember(nil), self.config.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	for _, member := range members {
		if err := self.controlMember(runCtx, member.ID, true); err != nil {
			select {
			case serverErr := <-serverErrors:
				return serverErr
			case terminalErr := <-terminalErrors:
				return terminalErr
			default:
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("start member %s: %w", member.ID, err)
		}
	}
	select {
	case err := <-terminalErrors:
		return err
	case err := <-serverErrors:
		return err
	case <-ctx.Done():
		return nil
	}
}

func RunProviderSwarm(ctx context.Context, configPath string) error {
	config, err := LoadProviderSwarmConfig(configPath)
	if err != nil {
		return err
	}
	swarm, err := NewProviderSwarm(config)
	if err != nil {
		return err
	}
	return swarm.Run(ctx)
}
