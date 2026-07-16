// Command validator is the thin CLI entry point for the validator library
// (github.com/urfoundation/sn/validator). All logic lives in the library so it is
// importable and testable; this executable forwards os.Args and injects the
// build version.
package main

import (
	"os"

	"github.com/urfoundation/sn/v2026/validator"
)

// Version is stamped into this binary by the build (-ldflags "-X main.Version=…").
// It lives in package main so the linker path is immune to the release module
// fork (main is always "main"); main hands it to the library at startup.
var Version string

func main() {
	if Version != "" {
		validator.Version = Version
	}
	validator.Run(os.Args[1:])
}
