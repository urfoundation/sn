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
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/connect/protocol"
)

// TunnelTransportConfig configures the production transport.
type TunnelTransportConfig struct {
	ApiUrl     string
	ConnectUrl string
	// ByClientJwt is the validator identity client's JWT; the generator
	// derives per-tunnel clients from it (SourceClientId set).
	ByClientJwt string
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

func NewTunnelTransport(ctx context.Context, clientStrategy *connect.ClientStrategy, cfg TunnelTransportConfig) *TunnelTransport {
	return &TunnelTransport{
		ctx:            ctx,
		clientStrategy: clientStrategy,
		cfg:            cfg,
	}
}

// PostVerify opens an egress-pinned tunnel through hop, POSTs the body to
// <ApiUrl>/verify through it, and tears the tunnel down. ctx bounds the
// whole attempt (the engine's StepTimeout).
//
// TODO(integration): reuse tunnels across trails keyed by hop with an
// idle-TTL LRU — saves the ~seconds of client auth + provide-ack per hop at
// the cost of a supervisor. The per-call construction below is the correct,
// simple v1.
func (self *TunnelTransport) PostVerify(ctx context.Context, hop connect.Id, jsonBody []byte) ([]byte, error) {
	tunnelCtx, tunnelCancel := context.WithCancel(self.ctx)
	defer tunnelCancel()

	hopId := hop
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
		self.cfg.ByClientJwt,
		self.cfg.ConnectUrl,
		"validator",
		"validator",
		RequireVersion(),
		&self.cfg.SourceClientId,
		connect.DefaultClientSettings,
		connect.DefaultApiMultiClientGeneratorSettings(),
	)

	tun, err := connect.CreateTunWithDefaults(tunnelCtx)
	if err != nil {
		return nil, fmt.Errorf("tunnel netstack: %w", err)
	}

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
	defer multiClient.Close()

	source := connect.SourceId(self.cfg.SourceClientId)
	go connect.HandleError(func() {
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
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
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
func NewFindProvidersSeedPicker(api *connect.BringYourApi, selfClientId connect.Id) SeedPicker {
	return func(ctx context.Context) (connect.Id, error) {
		result, err := api.FindProviders2Sync(&connect.FindProviders2Args{
			Specs: []*connect.ProviderSpec{
				{BestAvailable: true},
			},
			Count:            8,
			ExcludeClientIds: []connect.Id{selfClientId},
			RankMode:         "quality",
		})
		if err != nil {
			return connect.Id{}, err
		}
		candidates := []connect.Id{}
		for _, provider := range result.Providers {
			if provider.ClientId == selfClientId {
				continue
			}
			candidates = append(candidates, provider.ClientId)
		}
		if len(candidates) == 0 {
			return connect.Id{}, fmt.Errorf("no seed providers available")
		}
		return candidates[mathrand.Intn(len(candidates))], nil
	}
}

// NewApiServerKeyRing builds a ServerKeyRing backed by the unauthenticated
// control-plane `GET /verify/keys` binding (VALIDATOR.md §3.5).
func NewApiServerKeyRing(api *connect.BringYourApi) *ServerKeyRing {
	return NewServerKeyRing(func() (map[byte]ed25519.PublicKey, error) {
		result, err := api.VerifyKeysSync()
		if err != nil {
			return nil, err
		}
		keys := map[byte]ed25519.PublicKey{}
		for _, key := range result.Keys {
			if len(key.PublicKey) == ed25519.PublicKeySize {
				keys[key.ServerKeyId] = ed25519.PublicKey(key.PublicKey)
			}
		}
		return keys, nil
	})
}
