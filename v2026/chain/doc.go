// Package chain is the native (Substrate) subnet toolkit shared by the release
// binaries and the sim-testnet harness. It owns the registration economics
// read, the register_limit / add_stake / add_stake_limit call construction,
// the payment_queryInfo fee approval, exact-block storage reads with the
// runtime's ValueQuery fallback semantics, signed-extrinsic encoding, and one
// journaled submit-and-watch-finalized flow. Every mutating helper is a dry
// run unless the request explicitly applies it, and every read/sign happens
// against a runtime artifact the caller pinned (crv4/runtime_identity.go).
package chain
