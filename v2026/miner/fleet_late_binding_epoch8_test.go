package miner

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/protocol"
)

// `provider fleet bind` tells a miner a binding that landed in the epoch 8
// late-binding window counts from epoch 8, and any other binding from its
// signed first epoch.
func TestFleetBindingCountsFromEpoch8Window(t *testing.T) {
	mainnet := &protocol.FleetManifest{ChainID: 964, Coordinator: [20]byte(common.HexToAddress("0xC18925925E2B7bb9059b7d696b8c92762AE86406"))}
	for _, c := range []struct {
		manifest *protocol.FleetManifest
		from     uint64
		landed   uint64
		want     string
	}{
		{mainnet, 9, 9_600_029, "fleet binding counts from epoch 8: it landed in block 9600029, inside the epoch 8 late-binding window (blocks 9600028 to 9615060), so it counts for epoch 8 from that block and then for its signed epochs [9,20]"},
		{mainnet, 9, 9_615_060, "fleet binding counts from epoch 8: it landed in block 9615060"},
		{mainnet, 9, 9_615_061, "fleet binding counts from epoch 9, its signed first epoch (it landed in block 9615061)"},
		{mainnet, 9, 9_600_027, "fleet binding counts from epoch 9, its signed first epoch"},
		{mainnet, 10, 9_650_500, "fleet binding counts from epoch 10, its signed first epoch"},
		{&protocol.FleetManifest{ChainID: 945, Coordinator: mainnet.Coordinator}, 9, 9_600_029, "fleet binding counts from epoch 9, its signed first epoch"},
	} {
		if got := fleetBindingCountsFrom(c.manifest, c.from, 20, c.landed); !strings.HasPrefix(got, c.want) {
			t.Errorf("signed from %d, landed in %d: %q, want %q", c.from, c.landed, got, c.want)
		}
	}
}
