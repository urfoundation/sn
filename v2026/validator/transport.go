package validator

// transport.go — the production TrailTransport: for each hop, an
// egress-pinned tunnel is opened through exactly that provider and the
// /verify POST is dialed through it, so the request's source IP at the
// server is the hop's egress (the anchor of the whole proof, VALIDATOR.md
// §2/§8).
//
// Wiring (the connect stack, mirroring urnetwork/proxy/socks/main.go):
//
//	NewApiMultiClientGenerator                     — one registered client
//	  -> NewRemoteUserNatClient                     — packet path to the hop
//	  <-> connect.Tun (gVisor netstack)             — userspace TCP/IP
//	  -> http.Transport{DialContext: tun.DialContext}
//
// DNS for the API host resolves through the tunnel too (the Tun's DoH cache
// dials through itself), so no bytes of the verify exchange leave outside
// the hop.
//
// Each request owns a fresh tunnel pinned to its exact hop. Each exclusively
// leased client retains processed key registration across sequential requests;
// operator shutdown joins every final transport and identity retirement.
//
// IPv4 and IPv6 get equal attempts: the transport's requests alternate the
// family their tunnel tries first, IPv4 for the first request, and each falls
// back to the other family (connect IPV6.md C7, VALIDATOR.md §8.2).

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/connect/v2026/protocol"
	"github.com/urnetwork/sdk/v2026"
)

// TunnelTransportConfig configures the production transport.
type TunnelTransportConfig struct {
	CloseReportDomainHash [32]byte
	ApiUrl                string
	ConnectUrl            string
	// ByClientJwt returns the validator identity client's current JWT. The SDK
	// API rotates it in place; every newly created tunnel snapshots the latest
	// value while an in-flight tunnel may finish on the prior still-valid JWT.
	ByClientJwt func() string
	// SourceClientId is the validator's own client id — excluded from
	// provider selection and used as the packet source.
	SourceClientId connect.Id
	// Clients is how many registered tunnel clients serve requests at once.
	// Each request leases one exclusively, and waiting for a lease spends the
	// request's step deadline, so a trail engine sets this to its concurrency.
	// Zero means one.
	Clients int
}

// TunnelTransport implements TrailTransport over real per-hop tunnels.
// Every call is bounded by the caller's ctx (the engine's StepTimeout).
// Each client's initial registration has one bounded transport-owned lifetime
// across calls.
type TunnelTransport struct {
	ctx            context.Context
	cancel         context.CancelFunc
	clientStrategy *connect.ClientStrategy
	cfg            TunnelTransportConfig
	// Free leases; clientCount of them exist for the transport's lifetime.
	clientLeases chan *tunnelClientLease
	clientCount  int
	newClient    func(context.Context, connect.Id) (tunnelTransportClient, error)
	createTun    func(context.Context, *connect.TunSettings) (*connect.Tun, error)
	closed       chan struct{}
	closeErr     error
}

type tunnelAttemptGenerator interface {
	CloseAndWait(context.Context) error
}

type tunnelAttemptPacketClient interface {
	CloseAndWait(context.Context) error
}

type tunnelAttemptTun interface {
	Close() error
}

// Owns every per-hop object in dependency order. Cancellation alone is not
// completion: the packet pump must exit before the generator can retire its
// clients and return their message buffers.
type tunnelAttempt struct {
	cancel       context.CancelFunc
	generator    tunnelAttemptGenerator
	tun          tunnelAttemptTun
	packetClient tunnelAttemptPacketClient
	// Closes the contracts a joined hop leaves open on its retained identity.
	closeContracts func()
	retireClient   func(context.Context) error
	removeIdentity func(context.Context) error
	pumpDone       <-chan struct{}
	transportLock  sync.Mutex
	transports     []tunnelAttemptPacketClient
}

func (self *tunnelAttempt) addTransport(transport tunnelAttemptPacketClient) {
	self.transportLock.Lock()
	defer self.transportLock.Unlock()
	self.transports = append(self.transports, transport)
}

// Stops packet production, joins the packet client and pump, then retires all
// generated clients. Partial construction follows the same path.
func (self *tunnelAttempt) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return self.closeAndWait(ctx)
}

func (self *tunnelAttempt) closeAndWait(ctx context.Context) error {
	if self.cancel != nil {
		self.cancel()
	}
	var closeErrors []error
	if self.tun != nil {
		if err := self.tun.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close tunnel netstack: %w", err))
		}
	}
	if self.packetClient != nil {
		if err := self.packetClient.CloseAndWait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close tunnel packet client: %w", err))
		}
	}
	if self.pumpDone != nil {
		select {
		case <-self.pumpDone:
		case <-ctx.Done():
			closeErrors = append(closeErrors, fmt.Errorf("join tunnel packet pump: %w", ctx.Err()))
		}
	}
	// A joined packet path sends nothing more for this hop, so its contracts
	// close now and return their escrow instead of waiting out the send
	// sequence's idle or ack timeout. An unjoined path retires the identity.
	if self.closeContracts != nil && len(closeErrors) == 0 {
		self.closeContracts()
	}
	if self.retireClient != nil {
		if err := self.retireClient(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("retire tunnel client: %w", err))
		}
	}
	if self.generator != nil {
		if err := self.generator.CloseAndWait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close tunnel generator: %w", err))
		}
	}
	self.transportLock.Lock()
	transports := append([]tunnelAttemptPacketClient(nil), self.transports...)
	self.transportLock.Unlock()
	for _, transport := range transports {
		if err := transport.CloseAndWait(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close tunnel platform transport: %w", err))
		}
	}
	// The generator's final RemoveClientArgs uses a fire-and-forget API call.
	// Own one idempotent completion after all identity users have joined, so
	// closing the generator API cannot silently leave this derived identity.
	if self.removeIdentity != nil && len(closeErrors) == 0 {
		if err := self.removeIdentity(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("remove tunnel identity: %w", err))
		}
	}
	return errors.Join(closeErrors...)
}

// Decides when a retained identity's joined attempts close their contracts.
// Every joined attempt closes them, as a hop that has ended does, except one
// answered source-egress-unresolved: the step resends through the same hop at
// once, pinned to the other family (VALIDATOR.md §8.2), and the resend rides
// the identity's open transfer sequence to the provider. Closing it would make
// the resend open a new one, and the provider replaces the receive sequence it
// holds for this identity only once the old one has exited: on a carrier it
// does not know to be reliable it drops the new sequence's first packet
// meanwhile (connect ReceiveBuffer.Pack), so the resend would wait out a
// transfer resend, a second or more, inside the latency the server attributes
// to the hop. The identity's next attempt through another hop closes what the
// answered attempt kept. Each retained identity keeps its own: a resend that
// leases another of the transport's clients rides that client's sequences.
type tunnelHopContracts struct {
	// the client's Flush: cancels the identity's transfer sequences, which
	// close their contracts
	flush func()
	// the hop an answered attempt kept the contracts open for, or zero
	kept connect.Id
}

// Runs before each attempt through hop.
func (self *tunnelHopContracts) begin(hop connect.Id) {
	if self.kept != (connect.Id{}) && self.kept != hop {
		self.flush()
	}
	self.kept = connect.Id{}
}

// Returns what the attempt through hop, which ended with err, runs once its
// packet path has joined.
func (self *tunnelHopContracts) joined(hop connect.Id, err error) func() {
	if errors.Is(err, errSourceEgressUnresolved) {
		return func() { self.kept = hop }
	}
	return self.flush
}

func NewTunnelTransport(ctx context.Context, clientStrategy *connect.ClientStrategy, cfg TunnelTransportConfig) *TunnelTransport {
	ownerCtx, cancel := context.WithCancel(ctx)
	clientCount := max(cfg.Clients, 1)
	self := &TunnelTransport{
		ctx:            ownerCtx,
		cancel:         cancel,
		clientStrategy: clientStrategy,
		cfg:            cfg,
		clientLeases:   make(chan *tunnelClientLease, clientCount),
		clientCount:    clientCount,
		closed:         make(chan struct{}),
	}
	self.newClient = self.newRegisteredClient
	self.createTun = connect.CreateTun
	for range clientCount {
		self.clientLeases <- &tunnelClientLease{}
	}
	go self.run()
	return self
}

func (self *TunnelTransport) currentByClientJwt() (string, error) {
	if self.cfg.ByClientJwt == nil {
		return "", fmt.Errorf("validator client JWT source is not configured")
	}
	byClientJwt := self.cfg.ByClientJwt()
	if byClientJwt == "" {
		return "", fmt.Errorf("validator client JWT is empty")
	}
	return byClientJwt, nil
}

// A hop carries one /verify exchange of a few kilobytes, so every contract it
// requests is this one small size. connect's defaults open at 1 MiB and size
// each prefetched successor toward 128 MiB (32.75 MiB for the second contract),
// and the platform escrows every grant from the validator's balance until the
// contract closes. The platform raises a request to its own minimum grant. The
// size stays well above the 16 KiB transport message ceiling a contract must fit.
const tunnelContractTransferByteCount = connect.ByteCount(256 << 10)

// Returns fresh settings for every derived tunnel client. Provider clients
// advertise opportunistic encryption and may initiate the TLS session on their
// return sequence. An encryption-off validator can carry the plaintext proof,
// but leaves that provider handshake alive until its full TLS timeout. Matching
// the provider's opportunistic policy supplies the responder capability while
// retaining plaintext compatibility with peers that cannot establish a session.
func newTunnelClientSettings() *connect.ClientSettings {
	clientSettings := connect.DefaultClientSettings()
	clientSettings.EncryptionSettings.Mode = connect.EncryptionModeOpportunistic
	// The reply to a provider-initiated handshake rides the validator's own
	// regular contract to that provider. The default asks for a companion
	// contract instead, which the platform refuses because public providers
	// publish no stream key, so each such request waited out its timeout.
	clientSettings.EncryptionSettings.EncryptionControlUseCompanion = false
	// Providers must be able to read the derived client's identity key before
	// the generator admits tunnel traffic, not merely after a delivery ack.
	clientSettings.ClientKeyRegistrationRequired = true
	// Equal initial and standard sizes keep every contract of a send sequence,
	// including its prefetched successor, at the one small size.
	contractSettings := clientSettings.ContractManagerSettings
	contractSettings.InitialContractTransferByteCount = tunnelContractTransferByteCount
	contractSettings.InitialNetworkPeerContractTransferByteCount = tunnelContractTransferByteCount
	contractSettings.StandardContractTransferByteCount = tunnelContractTransferByteCount
	return clientSettings
}

// Each lease constructs and joins its own exact-hop packet path. The
// registered identity remains alive after request cancellation or rejection.
// The tunnel's hostname dial tries family.first first, and only it when the
// family is pinned.
func (self *registeredTunnelClient) postVerify(ctx context.Context, hop connect.Id, jsonBody []byte, family hopIpFamily) (responseBody []byte, returnErr error) {
	byClientJwt, err := self.owner.currentByClientJwt()
	if err != nil {
		return nil, err
	}
	self.generator.SetByJwt(byClientJwt)
	if err := self.client.ClientKeyManager().WaitForRegistration(ctx); err != nil {
		return nil, fmt.Errorf("tunnel client registration: %w", err)
	}
	destination, err := connect.NewMultiHopId(hop)
	if err != nil {
		return nil, fmt.Errorf("tunnel destination: %w", err)
	}
	tunnelCtx, tunnelCancel := context.WithCancel(ctx)
	attempt := &tunnelAttempt{cancel: tunnelCancel}
	// The exclusive lease runs one hop at a time on this identity. Once this
	// attempt's packet path has joined, the only data sequences left on the
	// client belong to finished attempts, so flushing the client closes their
	// contracts, except for the resend through this hop that a 421 leads to
	// (tunnelHopContracts). The flush also restarts the client's control
	// sequence; contract close reports retry until the platform acknowledges
	// them.
	self.contracts.begin(hop)
	self.packetCleanup = attempt
	defer func() {
		attempt.closeContracts = self.contracts.joined(hop, returnErr)
		closeErr := attempt.close()
		if closeErr != nil {
			// An unjoined packet path cannot share this identity with another hop.
			self.cleanup.cancel()
		} else {
			self.packetCleanup = nil
		}
		returnErr = errors.Join(returnErr, closeErr)
	}()

	tunSettings := connect.DefaultTunSettings()
	tunSettings.DialFirstIpFamily = family.first
	tun, err := self.owner.createTun(tunnelCtx, tunSettings)
	if err != nil {
		return nil, fmt.Errorf("tunnel netstack: %w", err)
	}
	attempt.tun = tun

	packetClient := connect.NewRemoteUserNatClient(
		self.client,
		newTunnelPacketReceiver(tunnelCtx, hop, tun),
		[]connect.MultiHopId{destination},
		protocol.ProvideMode_Network,
	)
	attempt.packetClient = packetClient

	source := connect.SourceId(self.owner.cfg.SourceClientId)
	pumpDone := make(chan struct{})
	attempt.pumpDone = pumpDone
	go connect.HandleError(func() {
		defer close(pumpDone)
		for {
			packet, err := tun.Read()
			if err != nil {
				return
			}
			if !packetClient.SendPacket(source, protocol.ProvideMode_Network, packet, time.Second) {
				connect.MessagePoolReturn(packet)
			}
		}
	})

	// the family the request's one connection reached the server on, which a
	// source-egress-unresolved answer rules out for the rest of the step
	var landedIpFamily atomic.Int32
	httpTransport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			conn, err := tun.DialContext(ctx, hopDialNetwork(network, family), address)
			if err == nil {
				landedIpFamily.Store(int32(connIpFamily(conn)))
			}
			return conn, err
		},
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
	}
	defer httpTransport.CloseIdleConnections()
	httpClient := &http.Client{Transport: httpTransport}

	request, err := http.NewRequestWithContext(ctx, "POST", self.owner.cfg.ApiUrl+"/verify", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("verify post via %s: %w", hop, err)
	}
	defer response.Body.Close()
	responseBody, err = io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		return nil, tunnelVerifyStatusFailure(response.StatusCode, verifyPostError(hop, response.StatusCode, responseBody, int(landedIpFamily.Load())))
	}
	return responseBody, nil
}

// verifyPostError is the error for a /verify answer other than 200: the
// server's retryable 421 is errSourceEgressUnresolved, for the family the
// request reached the server on, only with exactly its body; every other
// answer, 421 with any other body included, is an ordinary failed attempt.
func verifyPostError(hop connect.Id, statusCode int, body []byte, ipFamily int) error {
	if statusCode == http.StatusMisdirectedRequest && string(body) == verifySourceEgressUnresolvedBody {
		return &sourceEgressUnresolvedError{hop: hop, ipFamily: ipFamily}
	}
	return fmt.Errorf("verify post via %s: http %d: %s", hop, statusCode, truncateForLog(body))
}

// hopDialNetwork narrows a hop request's dial to its family when the request
// is pinned to it: "tcp" becomes "tcp4" or "tcp6", so the tunnel resolves and
// dials that family alone, never falling back to the other.
func hopDialNetwork(network string, family hopIpFamily) string {
	if !family.pinned || network != "tcp" {
		return network
	}
	switch family.first {
	case 4:
		return "tcp4"
	case 6:
		return "tcp6"
	default:
		return network
	}
}

// connIpFamily is the family of the address a tunnel connection reached, or
// 0 when it cannot tell.
func connIpFamily(conn net.Conn) int {
	addrPort, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil {
		return 0
	}
	if addrPort.Addr().Unmap().Is4() {
		return 4
	}
	return 6
}

// Shared identities may still receive a previous hop's delayed packets. Only
// the authenticated requested source can write into this request's netstack.
func newTunnelPacketReceiver(ctx context.Context, hop connect.Id, writer io.Writer) connect.ReceivePacketFunction {
	return func(source connect.TransferPath, _ protocol.ProvideMode, _ *connect.IpPath, packet []byte) {
		if ctx.Err() != nil || source.SourceId != hop {
			return
		}
		_, _ = writer.Write(packet)
	}
}

func truncateForLog(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}

// NewFindProvidersSeedPicker returns a SeedPicker that samples the
// validator-chosen entry hop from FindProviders2 (§4.1) — best-available
// ranking, excluding the validator itself, choosing uniformly among the
// returned candidates so consecutive trails spread their entry points.
// The online rank mode samples every online provider: a validator measures the
// whole fleet, and Quality's per-request subscriber validation is end-user
// access policy that a seed hop does not need. ForceMinimum deliberately
// includes connected providers before they have latency/speed history: trails
// are the measurement traffic that creates that history, so strict discovery
// here would deadlock a freshly started operator.
func NewFindProvidersSeedPicker(api *sdk.Api, selfClientId connect.Id) SeedPicker {
	return func(ctx context.Context) (connect.Id, error) {
		selfId, err := sdk.ParseId(selfClientId.String())
		if err != nil {
			return connect.Id{}, err
		}
		specs := sdk.NewProviderSpecList()
		specs.Add(&sdk.ProviderSpec{BestAvailable: true})
		excludeClientIds := sdk.NewIdList()
		excludeClientIds.Add(selfId)
		result, err := api.FindProviders2SyncWithContext(ctx, &sdk.FindProviders2Args{
			Specs:            specs,
			Count:            8,
			ExcludeClientIds: excludeClientIds,
			RankMode:         "online",
			ForceMinimum:     true,
		})
		if err != nil {
			return connect.Id{}, err
		}
		candidates := []connect.Id{}
		if result.ProviderStats == nil {
			return connect.Id{}, fmt.Errorf("no seed providers available")
		}
		for i := 0; i < result.ProviderStats.Len(); i += 1 {
			provider := result.ProviderStats.Get(i)
			if provider == nil || provider.ClientId == nil {
				continue
			}
			providerId, err := connect.ParseId(provider.ClientId.String())
			if err != nil || providerId == selfClientId {
				continue
			}
			candidates = append(candidates, providerId)
		}
		if len(candidates) == 0 {
			return connect.Id{}, fmt.Errorf("no seed providers available")
		}
		return candidates[mathrand.Intn(len(candidates))], nil
	}
}

// NewApiServerKeyRing builds a ServerKeyRing backed by the unauthenticated
// control-plane `GET /verify/keys` binding (VALIDATOR.md §3.5).
func NewApiServerKeyRing(api *sdk.Api) *ServerKeyRing {
	return NewServerKeyRing(func() (map[byte]ed25519.PublicKey, error) {
		result, err := api.VerifyKeysSync()
		if err != nil {
			return nil, err
		}
		keys := map[byte]ed25519.PublicKey{}
		for _, key := range result.Keys {
			if len(key.PublicKey) == ed25519.PublicKeySize {
				if key.ServerKeyId < 0 || 255 < key.ServerKeyId {
					return nil, fmt.Errorf("server key id %d is outside the byte range", key.ServerKeyId)
				}
				keys[byte(key.ServerKeyId)] = ed25519.PublicKey(key.PublicKey)
			}
		}
		return keys, nil
	})
}
