// Package stabi provides abigen v2 Go bindings for the release coordinator,
// immutable settlement vault and reserve sink, plus the quarantined legacy
// STSubnet contract.
//
// The binding files are generated; do not edit them by hand. Source of truth
// is the Foundry project in evm/: forge build emits the artifacts and
// generate.sh exports their ABIs and runs abigen. Use generate.sh --check for
// a non-mutating freshness gate.
package stabi

//go:generate ./generate.sh
