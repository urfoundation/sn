// Command miner is the thin CLI entry point for the miner library
// (github.com/urnetwork/sn/miner) — the subnet's provider node (formerly
// connect/provider). All logic lives in the library so it is importable and
// testable; this executable only forwards os.Args.
package main

import (
	"os"

	"github.com/urnetwork/sn/miner"
)

func main() {
	miner.Run(os.Args[1:])
}
