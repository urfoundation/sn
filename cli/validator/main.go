// Command validator is the thin CLI entry point for the validator library
// (github.com/urnetwork/sn/validator). All logic lives in the library so it is
// importable and testable; this executable only forwards os.Args.
package main

import (
	"os"

	"github.com/urnetwork/sn/validator"
)

func main() {
	validator.Run(os.Args[1:])
}
