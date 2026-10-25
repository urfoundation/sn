// Command commitment-oracle is the thin CLI entry point for the SN25 commitment
// oracle library (github.com/urfoundation/sn/commitmentoracle). All logic
// lives in the library so it is importable and testable; this executable
// forwards os.Args and injects the build version.
package main

import (
	"os"

	"github.com/urfoundation/sn/v2026/commitmentoracle"
)

// Version is stamped into this binary by the build (-ldflags "-X main.Version=…").
var Version string

func main() {
	if Version != "" {
		commitmentoracle.Version = Version
	}
	commitmentoracle.Run(os.Args[1:])
}
