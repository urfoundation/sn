// Package stabi provides abigen v2 Go bindings for the STSubnet contract
// (evm/src/STSubnet.sol).
//
// The bindings in stsubnet.go are generated — do not edit them by hand.
// Source of truth is the Foundry project in evm/: forge build exports the
// ABI to evm/abi/STSubnet.abi.json, and generate.sh runs abigen over it.
// See README.md for the exact regeneration steps.
package stabi

//go:generate ./generate.sh
