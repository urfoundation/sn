# Owner trim recheck and census reconciliation

`owner-trim-recheck` checks whether a retained owner-only partial trim still has
the same protected identities, selection inputs and residuals at a fresh
finalized head. `owner-trim-reconcile` compares a later complete census with
that plan's exact expected generations and compressed survivor UIDs. Neither
command signs, submits or establishes that a trim executed. `reset_ready`,
`apply_authority` and `full_reset_completed` remain false in every result.

```sh
go run ./mainnet owner-trim-recheck \
  --rpc https://rpc.example \
  --policy /secure/sn25-census-policy.json \
  --plan /secure/owner-trim-plan.json \
  --plan-hash sha256:<retained-plan-content-digest> \
  --retry-window 5m

go run ./mainnet owner-trim-reconcile \
  --rpc https://rpc.example \
  --policy /secure/sn25-census-policy.json \
  --plan /secure/owner-trim-plan.json \
  --plan-hash sha256:<retained-plan-content-digest> \
  --retry-window 5m
```

Supply the original [owner-trim plan](SUBNET-CENSUS.md#best-effort-owner-trim),
its expected `content_hash`, and the exact original policy file. A changed
policy, including changed declared validator or custody roles, requires a new
reviewed plan. These hashes identify inputs; they do not prove independent
approval. A plan with no safe removal candidate is refused before RPC reads.

The guard rereads the plan's historical block through the same approved chain,
header, runtime and complete SN25/root census checks as the original planner.
It rebuilds the entire plan and requires its hash to match. The original
observation time, route spelling and node version are retained only as sampling
metadata. Rehashing an altered selection, census or residual cannot substitute
for the historical reread. The owned RPC must retain the required historical
state; an unavailable/pruned baseline is a failed observation, never permission
to trust imported facts.

It then obtains a new complete census. Both retained and current heights must
still resolve to their authenticated hashes, and a final finalized-head read
must equal the sampled current head. A moving head, changed metadata/code/full
runtime tuple, fork, rollback, missing data or interrupted recheck publishes no
partial result. Both censuses and all rechecks share one 60-second through
15-minute deadline. The existing eight-worker and 4096-seat bounds apply.
Archive reads may exhaust the default five minutes. Retry the complete command
with `--retry-window 15m` if needed; a transport/deadline failure remains exit 1
and publishes no partial evidence or execution token.
Policy files are bounded to 1 MiB and retained plans to 32 MiB; duplicate or
unknown JSON fields, trailing values, special files and final symlinks are
refused. Outputs have a separate `urnetwork-mainnet-owner-trim-guard-v1` schema
and a schema-prefixed canonical JSON SHA256 content seal with an empty
`content_hash`, as in the owner-trim plan.

Before a prospective dispatch, recheck refuses changed emissions even if the
same identities would still be selected. It also refuses changes to membership,
coldkeys, registration generations, owner identity, permits/protection,
immunity, capacity, timing or root census. Natural immunity expiry cannot
silently expand an approved partial selection. Registration and PoW
registration must be closed at both observations. The original best capacity,
removed generations, survivor mapping and residual set remain fixed; the
command never substitutes a newly ranked plan.

Reconciliation reports every original requested generation as retained,
absent, or a hotkey with a different current generation. It separately retains
absent old generations, missing protected identities, incorrect survivor
UID/generation mappings, unexpected registrations and the excluded root census
comparison. An immune old miner may remain as an explicit expected residual.
A new registration at the same hotkey is reported separately. A lower UID count
cannot hide a removed validator without a permit, owner identity outside the
runtime's immune subset, reserve/pool/escrow/custody identity, or lost residual.

`observation_matches` means that the relevant comparison passed. For recheck
it concerns the observed selection inputs; for reconciliation it concerns the
expected membership/capacity and root post-state. An absent old generation is
not proof of which transaction removed it. A canonical exact trim receipt,
dispatch phase, original pending-action recovery and custody/effect/history
audit remain unimplemented here. Later block state alone cannot establish
those facts. No matched reconciliation sets full-reset completion.

The [reviewed owner call][admin] accepts only `netuid` and `max_n`. Its
[selection algorithm][trim] reads emissions and immunity at dispatch and
compresses surviving UIDs. It has no parameter enforcing this plan's approved
hotkey/coldkey/generation set or census hash. The [runtime extensions][runtime]
do not make these client-side reads atomic with dispatch. This comparison
therefore retains `OWNER_TRIM_EXECUTION_TIME_SELECTION_SAFETY_NOT_ESTABLISHED`.
A successful recheck is not a reusable signing or broadcast token. Closing
registration does not freeze emissions, immunity expiry or privileged churn.
A separately qualified bounded safe-set invariant can establish selection
safety without an atomic hotkey predicate. The new signer-free
[`owner-trim-qualify`](OWNER-TRIM-BOUNDED.md) tests the observed predicates for
that narrower case while retaining explicit unproved external fences and all
execution gates. It neither relaxes this fixed-plan comparison nor reuses its
reconciliation for an arbitrary approved subset.

Exit 0 means a complete matching observation; exit 3 means comparison drift or
an integrity refusal; exit 2 means invalid input; exit 1 means a transport,
interruption or output failure. All results remain execution-blocked. No
`--apply`, key, signer or transaction-broadcast option exists.

The trust boundary remains the independently approved owned RPC and exact
runtime pins, with authenticated headers but without independent GRANDPA or
storage-proof verification. Mainnet approvals, source-to-Wasm provenance,
build-selected cooldown proof, custody/stake/collateral/claim/history effects,
receipt attribution and residual dispositions remain separate launch gates.
An unchanged latest-head assertion is not independent proof of chain liveness;
the finalized-progress monitor and approved route trust remain necessary.

[admin]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L1979
[trim]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171
[runtime]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/runtime/src/lib.rs#L1511
