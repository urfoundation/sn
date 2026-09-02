package validator

// transport.go — the production TrailTransport: for each hop, an
// egress-pinned tunnel is opened through exactly that provider and the
// /verify POST is dialed through it, so the request's source IP at the
// server is the hop's egress (the anchor of the whole proof, VALIDATOR.md
// §2/§8).
//
// Wiring (the connect stack, mirroring urnetwork/proxy/socks/main.go):
//
//	ProviderSpec{ClientId: hop}                     — pin the egress
//	  -> NewApiMultiClientGenerator                 — derived per-tunnel client
//	  -> NewRemoteUserNatMultiClient                — packet path to the hop
//	  <-> connect.Tun (gVisor netstack)             — userspace TCP/IP
//	  -> http.Transport{DialContext: tun.DialContext}
//
// DNS for the API host resolves through the tunnel too (the Tun's DoH cache
// dials through itself), so no bytes of the verify exchange leave outside
// the hop.
//
// A tunnel is built per PostVerify call and torn down after: each hop is
// used exactly once per trail, so there is nothing to pool per-trail; a
// cross-trail tunnel cache is a later optimization (TODO below).

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
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
	clientStrategy *connect.ClientStrategy
	cfg            TunnelTransportConfig
}

type tunnelAttemptGenerator interface {
	CloseAndWait(context.Context) error
}

type tunnelAttemptMultiClient interface {
	CloseAndWait(context.Context) error
}

type tunnelAttemptTun interface {
	Close() error
}

// Owns every per-hop object in dependency order. Cancellation alone is not
// completion: the packet pump must exit before the generator can retire its
// clients and return their message buffers.
type tunnelAttempt struct {
	cancel      context.CancelFunc
	generator   tunnelAttemptGenerator
	tun         tunnelAttemptTun
	multiClient tunnelAttemptMultiClient
	pumpDone    <-chan struct{}
}

// Stops packet production, joins the multi-client and pump, then retires all
// generated clients. Partial construction follows the same path.
func (self *tunnelAttempt) close() error {
	if self.cancel != nil {
		self.cancel()
	}
	var closeErrors []error
	if self.tun != nil {
		if err := self.tun.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close tunnel netstack: %w", err))
		}
	}
	if self.multiClient != nil {
		if err := self.multiClient.CloseAndWait(context.Background()); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close tunnel multi-client: %w", err))
		}
	}
	if self.pumpDone != nil {
		<-self.pumpDone
	}
	if self.generator != nil {
		if err := self.generator.CloseAndWait(context.Background()); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close tunnel generator: %w", err))
		}
	}
	return errors.Join(closeErrors...)
}

func NewTunnelTransport(ctx context.Context, clientStrategy *connect.ClientStrategy, cfg TunnelTransportConfig) *TunnelTransport {
	return &TunnelTransport{
		ctx:            ctx,
		clientStrategy: clientStrategy,
		cfg:            cfg,
	}
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
	return clientSettings
}

// PostVerify opens an egress-pinned tunnel through hop, POSTs the body to
// <ApiUrl>/verify through it, and tears the tunnel down. ctx bounds the
// whole attempt (the engine's StepTimeout).
//
// TODO(integration): reuse tunnels across trails keyed by hop with an
// idle-TTL LRU — saves the ~seconds of client auth + provide-ack per hop at
// the cost of a supervisor. The per-call construction below is the correct,
// simple v1.
func (self *TunnelTransport) PostVerify(ctx context.Context, hop connect.Id, jsonBody []byte) (responseBody []byte, returnErr error) {
	tunnelCtx, tunnelCancel := context.WithCancel(self.ctx)
	attempt := &tunnelAttempt{cancel: tunnelCancel}
	defer func() {
		returnErr = errors.Join(returnErr, attempt.close())
	}()

	hopId := hop
	byClientJwt, err := self.currentByClientJwt()
	if err != nil {
		return nil, err
	}
	specs := []*connect.ProviderSpec{
		{ClientId: &hopId},
	}
	generator := connect.NewApiMultiClientGenerator(
		tunnelCtx,
		specs,
		self.clientStrategy,
		// exclude self — a validator may not egress through itself
		[]connect.Id{self.cfg.SourceClientId},
		self.cfg.ApiUrl,
		byClientJwt,
		self.cfg.ConnectUrl,
		"validator",
		"validator",
		RequireVersion(),
		&self.cfg.SourceClientId,
		newTunnelClientSettings,
		connect.DefaultApiMultiClientGeneratorSettings(),
	)
	attempt.generator = generator

	tun, err := connect.CreateTunWithDefaults(tunnelCtx)
	if err != nil {
		return nil, fmt.Errorf("tunnel netstack: %w", err)
	}
	attempt.tun = tun

	multiClient := connect.NewRemoteUserNatMultiClientWithDefaults(
		tunnelCtx,
		generator,
		func(source connect.TransferPath, provideMode protocol.ProvideMode, ipPath *connect.IpPath, packet []byte) {
			if _, err := tun.Write(packet); err != nil {
				// netstack rejected the packet — drop; TCP retransmit or
				// the request timeout handles it.
			}
		},
		protocol.ProvideMode_Network,
	)
	attempt.multiClient = multiClient

	source := connect.SourceId(self.cfg.SourceClientId)
	pumpDone := make(chan struct{})
	attempt.pumpDone = pumpDone
	go connect.HandleError(func() {
		defer close(pumpDone)
		for {
			packet, err := tun.Read()
			if err != nil {
				return
			}
			multiClient.SendPacket(source, protocol.ProvideMode_Network, packet, 15*time.Second)
		}
	})

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext:       tun.DialContext,
			DisableKeepAlives: true,
			ForceAttemptHTTP2: false,
		},
	}

	request, err := http.NewRequestWithContext(ctx, "POST", self.cfg.ApiUrl+"/verify", bytes.NewReader(jsonBody))
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
