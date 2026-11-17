# Validator native prerequisite qualification

The independently tested Go/test source is frozen at
`75eae6edf7d3de4c69ab11bd2d81a6f0fde88e4a` (tree
`63b522b4ca01ae353e8884b72d3f44cfefb97212`). All six changed Go/test
files in the integrated cherry-pick `0cc285d2` are byte-identical to that
source. The documentation change is separate from the tested Go bytes.

Sol medium verified all **18 focused roots** normally and under race detection.
The **44 mainnet adjacent roots** passed normally and in package-pass race
shards with identical sorted membership. Another **40 validator producer and
runtime roots** passed normally and under race detection. Twelve mutation
controls were causal normally and five selected controls were causal under
race detection. `go vet ./mainnet ./validator` passed. The initial broad race
attempt was stopped after three passes because its fixture cost approached the
budget; it is retained but not counted as a package-pass result.

The [sealed result](/home/by/urnetwork/temp/sol-validator-qualification-20260930/evidence/RESULT.md)
and [108-entry checksum manifest](/home/by/urnetwork/temp/sol-validator-qualification-20260930/evidence/SHA256SUMS)
retain source/dependency hashes, raw test streams, binary and build identities,
control patches and exact exit outcomes. The manifest SHA-256 is
`60d496a60db7b9dac474a6802348cea0044528a77fc4bee23e6330e53c924fd5`;
the author handoff manifest SHA-256 is
`5c533e262c362180cb5e04836a8ef473d02552397e4f75beec03fd537f4fb095`.

This is fixture qualification of the native prerequisite observation. Public
fresh-start authority remains nil. It does not prove an approved live runtime,
chain finality/storage proofs, operator proof and client-key readiness,
deployed contracts, global signer exclusion, effective majority stake, native
inclusion, service activation or the 10/90 economic outcome. No live host,
chain, signer, credential, deployment or transaction was used.
