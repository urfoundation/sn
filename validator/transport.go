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
// Each request owns a fresh tunnel pinned to its exact hop. One exclusively
// leased client retains processed key registration across sequential requests;
// operator shutdown joins its final transport and identity retirement.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/connect/protocol"
	"github.com/urnetwork/sdk"
)

// TunnelTransportConfig configures the production transport.
type TunnelTransportConfig struct {
	ApiUrl     string
	ConnectUrl string
	// ByClientJwt returns the validator identity client's current JWT. The SDK
	// API rotates it in place; every newly created tunnel snapshots the latest
	// value while an in-flight tunnel may finish on the prior still-valid JWT.
	ByClientJwt func() string
	// SourceClientId is the validator's own client id — excluded from
	// provider selection and used as the packet source.
	SourceClientId connect.Id
}

// TunnelTransport implements TrailTransport over real per-hop tunnels.
// Every call — tunnel establishment included — is bounded by the caller's
// ctx (the engine's StepTimeout).
type TunnelTransport struct {
	ctx            context.Context
	cancel         context.CancelFunc
	clientStrategy *connect.ClientStrategy
	cfg            TunnelTransportConfig
	clientLease    chan struct{}
	client         tunnelTransportClient
	newClient      func(context.Context, connect.Id) (tunnelTransportClient, error)
	closed         chan struct{}
	closeErr       error
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
	cancel         context.CancelFunc
	generator      tunnelAttemptGenerator
	tun            tunnelAttemptTun
	packetClient   tunnelAttemptPacketClient
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

func NewTunnelTransport(ctx context.Context, clientStrategy *connect.ClientStrategy, cfg TunnelTransportConfig) *TunnelTransport {
	ownerCtx, cancel := context.WithCancel(ctx)
	self := &TunnelTransport{
		ctx:            ownerCtx,
		cancel:         cancel,
		clientStrategy: clientStrategy,
		cfg:            cfg,
		clientLease:    make(chan struct{}, 1),
		closed:         make(chan struct{}),
	}
	self.newClient = self.newRegisteredClient
	self.clientLease <- struct{}{}
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

// Returns fresh settings for every derived tunnel client. Provider clients
// advertise opportunistic encryption and may initiate the TLS session on their
// return sequence. An encryption-off validator can carry the plaintext proof,
// but leaves that provider handshake alive until its full TLS timeout. Matching
// the provider's opportunistic policy supplies the responder capability while
// retaining plaintext compatibility with peers that cannot establish a session.
func newTunnelClientSettings() *connect.ClientSettings {
	clientSettings := connect.DefaultClientSettings()
	clientSettings.EncryptionSettings.Mode = connect.EncryptionModeOpportunistic
	// Providers must be able to read the derived client's identity key before
	// the generator admits tunnel traffic, not merely after a delivery ack.
	clientSettings.ClientKeyRegistrationRequired = true
	return clientSettings
}

// Each lease constructs and joins its own exact-hop packet path. The
// registered identity remains alive after request cancellation or rejection.
func (self *registeredTunnelClient) postVerify(ctx context.Context, hop connect.Id, jsonBody []byte) (responseBody []byte, returnErr error) {
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
	self.packetCleanup = attempt
	defer func() {
		closeErr := attempt.close()
		if closeErr != nil {
			// An unjoined packet path cannot share this identity with another hop.
			self.cleanup.cancel()
		} else {
			self.packetCleanup = nil
		}
		returnErr = errors.Join(returnErr, closeErr)
	}()

	tun, err := connect.CreateTunWithDefaults(tunnelCtx)
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

	httpTransport := &http.Transport{
		DialContext:       tun.DialContext,
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
		return nil, fmt.Errorf("verify post via %s: http %d: %s", hop, response.StatusCode, truncateForLog(responseBody))
	}
	return responseBody, nil
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
// ForceMinimum deliberately includes connected providers before they have
// latency/speed history: trails are the measurement traffic that creates that
// history, so strict discovery here would deadlock a freshly started operator.
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
			RankMode:         "quality",
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
