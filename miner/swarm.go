package miner

// swarm.go runs many production DeviceLocal providers inside one bounded
// process. It exists for large integration campaigns: every member retains an
// independent platform identity, state directory, wallet and source prefix,
// while process count and file descriptors remain operationally tractable.

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
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
	"github.com/urfoundation/sn/ss58"
)

const ProviderSwarmSchema = "urnetwork-provider-swarm-v1"

type ProviderSwarmMember struct {
	ID         string `json:"id"`
	APIURL     string `json:"api_url"`
	ConnectURL string `json:"connect_url"`
	StateDir   string `json:"state_dir"`
	Wallet     string `json:"wallet"`
	SourceIP   string `json:"source_ip"`
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
	config         *ProviderSwarmConfig
	stateLock      sync.Mutex
	members        map[string]ProviderSwarmMember
	running        map[string]bool
	disabled       map[string]bool
	starting       map[string]bool
	failures       map[string]string
	instances      map[string]*providerSwarmInstance
	runCtx         context.Context
	runCancel      context.CancelFunc
	terminalErrors chan error
	startMember    func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error)
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
		if _, err := ss58.DecodeWithPrefix(member.Wallet, ss58.BittensorPrefix); err != nil {
			return fmt.Errorf("member %s wallet: %w", member.ID, err)
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

func setSwarmMemberWallet(ctx context.Context, member ProviderSwarmMember, settings *connect.ClientStrategySettings) error {
	jwt, err := clientauth.ReadToken(filepath.Join(member.StateDir, "jwt"))
	if err != nil {
		return err
	}
	strategy := connect.NewClientStrategy(ctx, settings)
	defer strategy.Close()
	api := sdk.NewApi(ctx, strategy, member.APIURL)
	defer func() {
		_ = api.CloseAndWait(context.Background())
	}()
	api.SetByJwt(jwt)
	result, err := api.SnSetWalletSync(&sdk.SnSetWalletArgs{ColdkeySs58: member.Wallet})
	if err != nil {
		return err
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

// Reports live provider-carrier readiness. Tests may supply connectedOverride
// to force the state transition without constructing the full SDK graph.
func (self *providerSwarmInstance) connected() bool {
	if self == nil {
		return false
	}
	if self.connectedOverride != nil {
		return self.connectedOverride()
	}
	return self.device != nil && self.device.GetProviderConnected()
}

func (self *providerSwarmInstance) close() {
	if self == nil {
		return
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
	if self.cancel != nil {
		self.cancel()
	}
}

func startSwarmMember(ctx context.Context, member ProviderSwarmMember, failed func(error)) (*providerSwarmInstance, error) {
	dialSettings, err := testEgressDialContextForIP(member.SourceIP)
	if err != nil {
		return nil, err
	}
	strategySettings := connect.DefaultClientStrategySettings()
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
	deviceSettings := sdk.DefaultDeviceLocalSettings()
	deviceSettings.KeyMaterial = sdk.NewDeviceLocalKeyMaterial(seed, certificatePEM, keyPEM)
	deviceSettings.ProviderDialContextSettings = dialSettings
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
		config: config, members: members, running: map[string]bool{}, disabled: map[string]bool{}, starting: map[string]bool{},
		failures: map[string]string{}, instances: map[string]*providerSwarmInstance{}, startMember: startSwarmMember,
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

func (self *ProviderSwarm) memberFailed(id string, err error) {
	self.stateLock.Lock()
	if self.disabled[id] {
		self.stateLock.Unlock()
		return
	}
	delete(self.running, id)
	self.failures[id] = err.Error()
	terminalErrors := self.terminalErrors
	cancel := self.runCancel
	self.stateLock.Unlock()
	if terminalErrors != nil {
		select {
		case terminalErrors <- fmt.Errorf("member %s: %w", id, err):
		default:
		}
	}
	if cancel != nil {
		cancel()
	}
}

func (self *ProviderSwarm) disableMember(id string) error {
	self.stateLock.Lock()
	if _, ok := self.members[id]; !ok {
		self.stateLock.Unlock()
		return fmt.Errorf("unknown swarm member %q", id)
	}
	if self.disabled[id] {
		self.stateLock.Unlock()
		return nil
	}
	instance, running := self.instances[id]
	if !running || !self.running[id] {
		self.stateLock.Unlock()
		return fmt.Errorf("swarm member %q is not running", id)
	}
	self.disabled[id] = true
	delete(self.running, id)
	delete(self.instances, id)
	delete(self.failures, id)
	self.stateLock.Unlock()
	instance.close()
	return nil
}

func (self *ProviderSwarm) enableMember(id string) error {
	self.stateLock.Lock()
	member, ok := self.members[id]
	if !ok {
		self.stateLock.Unlock()
		return fmt.Errorf("unknown swarm member %q", id)
	}
	if self.running[id] && !self.disabled[id] {
		self.stateLock.Unlock()
		return nil
	}
	if !self.disabled[id] || self.starting[id] || self.runCtx == nil || self.runCtx.Err() != nil {
		self.stateLock.Unlock()
		return fmt.Errorf("swarm member %q cannot be enabled from its current state", id)
	}
	self.starting[id] = true
	delete(self.disabled, id)
	delete(self.failures, id)
	runCtx := self.runCtx
	startMember := self.startMember
	self.stateLock.Unlock()

	instance, err := startMember(runCtx, member, func(failure error) { self.memberFailed(id, failure) })
	self.stateLock.Lock()
	delete(self.starting, id)
	if err != nil {
		self.disabled[id] = true
		self.failures[id] = err.Error()
		self.stateLock.Unlock()
		return fmt.Errorf("enable swarm member %s: %w", id, err)
	}
	if runCtx.Err() != nil {
		self.disabled[id] = true
		self.stateLock.Unlock()
		instance.close()
		return fmt.Errorf("enable swarm member %s: swarm is stopping", id)
	}
	self.instances[id] = instance
	self.running[id] = true
	self.stateLock.Unlock()
	return nil
}

func (self *ProviderSwarm) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/status" && request.Method == http.MethodGet {
		status := self.status()
		writer.Header().Set("Content-Type", "application/json")
		if status.Running != status.Configured || len(status.Failures) != 0 {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(writer).Encode(status)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if request.Method != http.MethodPost || len(parts) != 3 || parts[0] != "control" {
		http.NotFound(writer, request)
		return
	}
	var err error
	switch parts[2] {
	case "disable":
		err = self.disableMember(parts[1])
	case "enable":
		err = self.enableMember(parts[1])
	default:
		http.NotFound(writer, request)
		return
	}
	if err != nil {
		http.Error(writer, err.Error(), http.StatusConflict)
		return
	}
	status := self.status()
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(status)
}

func (self *ProviderSwarm) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	terminalErrors := make(chan error, 1)
	self.stateLock.Lock()
	self.runCtx = runCtx
	self.runCancel = cancel
	self.terminalErrors = terminalErrors
	self.stateLock.Unlock()
	defer func() {
		self.stateLock.Lock()
		self.runCtx = nil
		self.runCancel = nil
		self.terminalErrors = nil
		instances := make([]*providerSwarmInstance, 0, len(self.instances))
		for _, instance := range self.instances {
			instances = append(instances, instance)
		}
		self.instances = map[string]*providerSwarmInstance{}
		self.running = map[string]bool{}
		self.stateLock.Unlock()
		for _, instance := range instances {
			instance.close()
		}
	}()
	server := &http.Server{Addr: self.config.ListenAddress, Handler: self}
	serverErrors := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	members := append([]ProviderSwarmMember(nil), self.config.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	for _, member := range members {
		instance, err := self.startMember(runCtx, member, func(failure error) { self.memberFailed(member.ID, failure) })
		if err != nil {
			self.setFailure(member.ID, err)
			return fmt.Errorf("start member %s: %w", member.ID, err)
		}
		self.stateLock.Lock()
		self.instances[member.ID] = instance
		self.running[member.ID] = true
		self.stateLock.Unlock()
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-serverErrors:
		return err
	case err := <-terminalErrors:
		return err
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
