# Validator committed-prefix qualification, September 30

The read-only `admit-committed` increment is integrated at `0c49973f`; its
test-only startup fixture repair is integrated at `49c55858`. It authenticates
each service UID's existing committed control history against both original
operator origins and retains completed checkpoints across later read failures.
It does not start either validator or authorize a live transaction.

The independent frozen-parent receipt is
`/mnt/data/sn-testnet/validator-mutable-prefix-sol-20260930/evidence/RESULT.md`
(SHA256SUMS SHA256 `083125c7a4947ea5b7d8bc7facedb60fdab48e6198355ff7f2ddbab372c3b33c`).
All 14 new roots passed normal and race under both umasks, including an actual
foreign-UID read. Three causal mutations failed as intended and `go vet` passed.
Its adjacent validator shard had seven startup-fixture failures. The same seven
failed on the clean base with the same native-observation diagnostic, so this
failed package remains preserved as evidence rather than treated as a pass.

The independently qualified fixture successor receipt is
`/mnt/data/sn-testnet/validator-startup-fixture-sol-20260930/evidence/RESULT.md`
(SHA256SUMS SHA256 `b8f4939f84821f381ae88e8ec288e082d0b06e355575061a93ee0784911aa7a0`).
The successor changes only five test files; production bytes are unchanged.
It passed 216 unique selected roots, including the 133-root adjacent validator
normal/race shards and 78-root transitive normal/race shards. Four foreign-UID
cells passed; three causal mutations failed as intended; `go vet` passed.
The original broad race process emitted 133 root PASS events and a terminal
package PASS with empty stderr. Its detached shell exit code was not observed;
the receipt records this explicitly and does not infer one.

This qualifies the committed-control-prefix subgate. Unsealed ledger/intent
state, per-operator live worker attestation, global signer custody, applied
weights influence and signed launch authority remain open. Public starts remain
closed. The source and receipt are local qualification evidence, not a deployed
mainnet observation.
