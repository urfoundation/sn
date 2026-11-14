// Package rehearsal is the runtime rehearsal (VERSION2-FINAL.md §14 gate 2,
// §15.8): tests that run against a node built from a reviewed runtime's
// source by scripts/runtime-rehearsal/build.sh and started by run-node.sh. The
// harness and its tests are test files behind the runtime_rehearsal build tag:
//
//	RUNTIME_REHEARSAL_NODE=/path/to/node-subtensor go test -tags runtime_rehearsal ./rehearsal/
//
// scripts/runtime-rehearsal/README.md records the build and the measured facts.
package rehearsal
