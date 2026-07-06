// Command snclaim is the thin CLI entry point for the on-chain submission
// library (github.com/urfoundation/sn/miner/onchain) — signs + broadcasts the
// miner's claimMiner / bind-head / unbind-head actions. Folded under sn/miner;
// all logic lives in the library so it is importable and testable.
package main

import (
	"os"

	"github.com/urfoundation/sn/miner/onchain"
)

func main() {
	onchain.Run(os.Args[1:])
}
