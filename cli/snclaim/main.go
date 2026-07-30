// Command snclaim is the thin CLI entry point for the on-chain submission
// library (github.com/urfoundation/sn/miner/onchain) — signs + broadcasts the
// miner's claimMiner / bind-head / unbind-head actions. Folded under sn/miner;
// all logic lives in the library so it is importable and testable. This
// executable forwards os.Args and injects the build version.
package main

import (
	"os"

	"github.com/urfoundation/sn/miner/onchain"
)

// Version is stamped into this binary by the build (-ldflags "-X main.Version=…").
// It lives in package main so the linker path is immune to the release module
// fork (main is always "main"); main hands it to the library at startup.
var Version string

func main() {
	if Version != "" {
		onchain.Version = Version
	}
	onchain.Run(os.Args[1:])
}
