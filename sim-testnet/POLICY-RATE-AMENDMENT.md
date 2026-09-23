# Future testnet rate amendment

`deploy/testnet/policy-v2.yml` changes policy ID 1 to 2 and only the three
alpha-rao/GiB rates: 40,000,000,000; 32,000,000,000; and 24,000,000,000.
Conviction thresholds, quality, one-epoch usage lag, 10-alpha per-operator epoch
cap and 196-alpha campaign cap remain exact. `policy-v1.yml` is retained.
`protocol.ValidateTestnetRateAmendment` is the shared admission rule.

The revised setup approval embeds both complete policies and its exact source
plan hash. The ordinary canonical plan hash covers that proof. Historical
funding, contract deployment and V2 activation transactions retain their original
intents; archived policies, signed failed attempts and payout artifacts are never
rewritten. The one new `policy.schedule-bootstrap` intent schedules the new hash
at a future coordinator epoch. A foreign pending policy, changed cap, exhausted
policy inventory or nonfuture activation refuses the operation. Normal journal,
nonce recovery, gas authorization and pre-sign checks remain mandatory.

The operator must follow this order with the approved owned RPC authority
unchanged at every step:

1. Preserve the predecessor's signed terminal evidence and immutable capture.
   Build and qualify one clean candidate with the payment-independent server
   usage ledger, validator policy bridge, and this harness amendment.
2. Before service replacement, run the candidate's `__server_db_migrate` for
   **both** operators using their exact retained private supervisor environments.
   Provisional supervisor adoption does not execute `LaunchDeployment`, so it
   cannot be relied upon to run migrations. Retain the reviewed migration
   helper, exact binary hash, migration output hashes and schema postconditions.
3. Start the ledger candidate under the retained old policy, and prove service
   health. Drain or expire contracts created without immutable provider-usage
   snapshots. Use a read-only census; do not infer same-network companion roles
   or backfill historical usage. Keep all prior signed zero artifacts.
4. Produce and review the new setup plan from the two authenticated rendered
   old policies and pinned finalized coordinator state. Apply only its emitted
   exact hash. Schedule policy v2 for a future epoch and wait until the exact
   hash is active. The source policy file's `effective_epoch: 0` is a hash-domain
   field; the coordinator's independently authenticated effective epoch is the
   operational activation boundary.
5. Render the current runtime configuration with the exact `previous_policy`
   bridge and restart/adopt that reviewed service generation. A new-policy
   configuration must not be claimed healthy while on-chain policy is old.
   If service replacement crosses activation, discard the transition epoch.
6. Keep the background fleet running until both operators publish a complete
   current-policy usage source. The harness waits up to three configured epochs
   before opening acceptance, retaining ordinary observations. At one finalized
   EVM head it checks the active policy, immutable native minimum and alpha
   price; each operator's signed last-complete source must clear **twice** the
   minimum at every conviction tier. The source must be within the new-policy
   epoch range. The accepted interval starts at the next complete boundary,
   no earlier than activation plus two epochs. Incomplete legacy usage remains
   unavailable rather than silently contributing zero.

This admission is a conservative readiness check, not a deposit receipt. Every
actual deposit remains exactly the governed floor/cap calculation from its
authenticated prior-epoch artifact, with its original nonce and independent
native pre-sign minimum check. Do not enlarge a too-small deposit, count old
usage under a new policy, or accept a root/claim merely because readiness passed.

At a synthetic price of 0.0005 TAO/alpha, 24 MiB yields 0.9375, 0.75 and
0.5625 alpha across the three tiers and clears a 100,000 TAO-rao movement floor
by more than twice in every tier. Production admission re-reads the actual
price and minimum; this example does not pin or predict them.
