// Claim discovery and preflight GETs retain read deadlines independently of
// signing and submission. Caller admission deadlines always take precedence.
package miner

import (
	"context"
	"time"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Only these read-only API strategies receive a sixty-second request allowance.
// The existing finite read owner retains its original five-minute deadline.
func defaultClaimReadStrategySettings() *connect.ClientStrategySettings {
	settings := connect.DefaultClientStrategySettings()
	settings.RequestTimeout = 60 * time.Second
	return settings
}

// Capture the selected epoch before retry. Reconstruct only the GET arguments;
// no queue mutation, proof decision, signing or transaction send enters the loop.
func readClaimPoolWithRetry(ctx context.Context, api claimAPI, epoch int64, hooks claimReadRetryHooks) (*sdk.SnPoolClaimResult, error) {
	return retryClaimApiRead(ctx, hooks, func(readCtx context.Context) (*sdk.SnPoolClaimResult, error) {
		return api.SnPoolClaimSyncWithContext(readCtx, &sdk.SnPoolClaimArgs{Epoch: epoch})
	})
}
