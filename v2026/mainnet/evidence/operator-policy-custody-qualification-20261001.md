# Operator payout policy custody qualification, 2026-10-01

MG-05/PF-05 remains in progress. This source correction removes the first
successor-epoch deposit failure caused by checking an old signed payout against
the new configuration's policy hash. It also prevents a changed epoch length
from changing the inferred historical payout window, and refuses fresh artifact
issuance under an unproven or mismatched epoch policy.

Server commit `8eec13544a9f32ca4099135b192f1e26fd2e3fd4` is based on
`7929a658978f435f50122d6c45d847e1312d33c3`.
Its frozen source tree is `500391d3919ff8d3301455d7c735aa91cb74af5c`.
Qualification uses SN `46efd24c1d434147b686a7d113539c669805748a` and the server's
unchanged immutable Connect `e1b5d77b5029` / SDK `5d37be3876e5` module pins.
The sealed local handoff is
[`HANDOFF.md`](/mnt/data/sn-testnet/mainnet-operator-custody-astra-20261001/HANDOFF.md).

The real reader first authenticates the current operator, policy, native
genesis and coordinator/evidence/vault graph. It reads `policyAt(sourceEpoch)`,
`epochStartBlock` and `epochEndBlock` at that exact canonical hash, checks the
window against the retained policy's effective epoch/block and epoch length,
reads raw EVM block identities and timestamps, and repeats the native/network
and canonical boundary witness. Failover restarts the whole read; no successful
prefix or current-config fallback supplies historical policy authority.
Current deposit custody authorization remains current-policy scoped.

Deposit sizing uses the prior artifact's authenticated historical policy and
window with current configured pricing. Original artifact content, signatures,
operator identity and signer checks remain unchanged. Missing or contradictory
history blocks paid sizing. A free schedule still owes zero; unavailable
informational history is reported without inventing verified usage. A fresh
payout requires the configuration policy and exact window to match authenticated
epoch authority. A retained immutable payout is returned unchanged on retry.

The deterministic two-operator fixture starts with actual signed rows in the
populated pre-743 schema, applies the current migration catalog and switches
from a 100-block policy to a 50-block policy. It verifies exact retained bytes,
authenticated processed Connect registration, generation-one current policy
heads, restart/current-key projection, fresh HTTP validator proof verification,
stale-policy and partial/foreign cohort refusal, tombstones and deletion. Each
operator's retained two GiB/three-user artifact requires 29 rao at current
rates and 32 rao of source funding including the existing reserve/transfer
allowances. The free schedule reports the same authentic totals and owes zero.

Separate controls reject missing, foreign, zero, future or malformed policy
history; incorrect, absent or changed block windows; missing timestamps; and
wrong native genesis, companion graph, root signer or current policy. Fresh
issuance rejects backdating, while a new closed successor-policy epoch can
produce its own artifact and retry it without rewriting retained bytes.

Qualification passes 54 affected roots normally and under race: five migration
roots, 11 model roots and 38 controller roots. The three new roots were repeated
on the corrected final tree (13.800s normal, 23.507s race); independent review
also passes them (15.766s normal, 26.025s race). A read-only full server build
and affected-package vet pass. Four causal overlays restore the old
current-policy comparison, permit backdating, omit the final canonical witness
or mislabel the exclusive end epoch; each fails at its intended assertion.
The receipt preserves the initial fixture correction, private-generator umask
setup refusal and deliberately interrupted unrelated capacity scope. Those
attempts are not counted as completed qualification scopes.

The receipt records exact commands, source identities and independent review.
It does not claim a complete server suite, live migrations,
production capacity, transactions, signing-device custody, deployment or release
approval. MG-06 historical financial reconciliation, durable participant sweeps
and production NetEscrow cutover remain open. Both operators and validators
still need coordinated future-boundary activation and observed live readiness.
