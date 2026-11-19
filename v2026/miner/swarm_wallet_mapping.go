// Swarm keeps its existing source-pinned, single-send HTTP owner for wallet
// POSTs. The shared SDK wire types carry the same canonical consent protocol.
package miner

import (
	"context"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

type swarmWalletMappingApi struct {
	apiUrl string
	token  string
	post   connect.HttpPostRawFunction
	get    connect.HttpGetRawFunction
}

func (self *swarmWalletMappingApi) SnEpochSyncWithContext(ctx context.Context) (*sdk.SnEpochResult, error) {
	return connect.HttpGetWithRawFunction(ctx, self.get, self.apiUrl+"/sn/epoch", self.token, &sdk.SnEpochResult{}, connect.NewNoopApiCallback[*sdk.SnEpochResult]())
}

func (self *swarmWalletMappingApi) SnWalletMappingChallengeSyncWithContext(ctx context.Context, args *sdk.SnWalletMappingChallengeArgs) (*sdk.SnWalletMappingChallengeResult, error) {
	return connect.HttpPostWithRawFunction(ctx, self.post, self.apiUrl+"/sn/wallet/consent", args, self.token, &sdk.SnWalletMappingChallengeResult{}, connect.NewNoopApiCallback[*sdk.SnWalletMappingChallengeResult]())
}

func (self *swarmWalletMappingApi) SnSetWalletSyncWithContext(ctx context.Context, args *sdk.SnSetWalletArgs) (*sdk.SnSetWalletResult, error) {
	return connect.HttpPostWithRawFunction(ctx, self.post, self.apiUrl+"/sn/wallet", args, self.token, &sdk.SnSetWalletResult{}, connect.NewNoopApiCallback[*sdk.SnSetWalletResult]())
}
