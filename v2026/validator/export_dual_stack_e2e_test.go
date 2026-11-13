//go:build dualstacke2e

package validator

import (
	"context"

	"github.com/urnetwork/connect/v2026"
)

// SetTunnelTransportE2EHooks replaces the two seams the dual-stack end-to-end
// test needs: each hop's tun constructor, and a callback that receives every
// registered client so the test can route it to an in-process provider
// instead of the platform. Call before the first request.
func SetTunnelTransportE2EHooks(
	transport *TunnelTransport,
	createTun func(context.Context, *connect.TunSettings) (*connect.Tun, error),
	clientCreated func(*connect.Client),
) {
	transport.createTun = createTun
	newClient := transport.newClient
	transport.newClient = func(ctx context.Context, firstHop connect.Id) (tunnelTransportClient, error) {
		owned, err := newClient(ctx, firstHop)
		if registered, ok := owned.(*registeredTunnelClient); ok && err == nil && registered.client != nil {
			clientCreated(registered.client)
		}
		return owned, err
	}
}

// E2EPostHop sends one hop's body through the transport as the trail engine's
// step does for the hop at position (0 for the SEED) of its trail-th trail:
// trying first the family the hop's position gives, with the engine's retries,
// within the default step timeout.
func E2EPostHop(ctx context.Context, transport *TunnelTransport, hop connect.Id, body []byte, trail uint64, position int) ([]byte, error) {
	engine := &TrailEngine{transport: transport, cfg: TrailEngineConfig{}.withDefaults()}
	response, _, err := engine.postStep(ctx, hop, body, hopFirstIpFamily(trail, position), nil)
	return response, err
}

// E2EHopFirstIpFamily is the family the hop at position of the trail-th trail
// tries first.
func E2EHopFirstIpFamily(trail uint64, position int) int {
	return hopFirstIpFamily(trail, position)
}

// E2EDefaultExtendAttempts is how many attempts a production step holds.
func E2EDefaultExtendAttempts() int {
	return TrailEngineConfig{}.withDefaults().ExtendAttempts
}

// E2EServerVerifyUnattributedAttemptLimit is the unit tests' mirror of the
// server's limit.
const E2EServerVerifyUnattributedAttemptLimit = serverVerifyUnattributedAttemptLimit
