//go:build linux || darwin

package validator

import (
	"context"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// An old status string is not evidence of application. Resolve the actual
// hotkey/UID and canonical finalized application block, then compare the entire
// row. This is in addition to ordinary prepared-source and inclusion checks.
func authenticateAdoptedIntentApplicationV2(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, intent *SteeringIntent) error {
	if ctx == nil || native == nil || cfg == nil || intent == nil || intent.Prepared == nil || intent.Status != "applied" || intent.ApplicationBlock == 0 || intent.ApplicationBlock < intent.RevealBlock {
		return errors.New("adopted intent has no complete application receipt")
	}
	hash, err := types.NewHashFromHexString(intent.ApplicationBlockHash)
	if err != nil {
		return err
	}
	hotkey, err := canonicalAttemptHex32("adopted application hotkey", intent.Prepared.HotkeyHex, false)
	if err != nil {
		return err
	}
	own := *native
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, &own, cfg, hash); err != nil {
		return err
	}
	observed, err := crv4.ReadValidatorScheduleAtContext(ctx, &own, crv4.ValidatorScheduleQuery{GenesisHash: own.GenesisHash, BlockHash: hash, BlockNumber: intent.ApplicationBlock, Netuid: cfg.Netuid, Hotkey: hotkey, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, releaseRuntimeIdentityV2(cfg))
	if err != nil || observed.Stake.Identity.UID != intent.SelfUID {
		return errors.Join(errors.New("adopted application lacks its actual finalized signing identity"), err)
	}
	row, err := own.WeightsAtContext(ctx, cfg.Netuid, observed.Stake.Identity.UID, hash)
	if err != nil {
		return err
	}
	if err := matchAdoptedApplicationRowV2(intent, row); err != nil {
		return err
	}
	return ctx.Err()
}

func matchAdoptedApplicationRowV2(intent *SteeringIntent, row []crv4.WeightPair) error {
	if len(intent.UIDs) != len(intent.Values) || len(row) != len(intent.UIDs) {
		return errors.New("adopted application weight census differs")
	}
	want := make(map[uint16]uint16, len(intent.UIDs))
	for index, uid := range intent.UIDs {
		if _, found := want[uid]; found {
			return errors.New("adopted application repeats a target UID")
		}
		want[uid] = intent.Values[index]
	}
	for _, pair := range row {
		value, found := want[uint16(pair.UID)]
		if !found || value != uint16(pair.Value) {
			return errors.New("adopted application row differs from the retained receipt")
		}
		delete(want, uint16(pair.UID))
	}
	return nil
}
