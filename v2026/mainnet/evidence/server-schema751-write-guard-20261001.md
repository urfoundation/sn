# Subscriber mixed-writer correction, schema 751

Author qualification on 2026-10-01 binds server
`a464bb3e9a71d44dc315d671ea43a9945ebb8917`, based on
`720e7c61182983dd2cd6de667787bb5b52f4d8a4`. The isolated branch is
`codex/astra-arin-mixed-writer-fail-closed-20261001` at
`/home/by/urnetwork/temp/astra-arin-mixed-writer-20261001/server`.
The source branch was merged into server `main` at `94229abb`; the exact tested
source is the first parent-side topic commit `a464bb3e`. No image rebuild,
production migration, active classifier change or v2 policy activation is
included.

This is a separate successor to the server-720 release. Its evidence cannot be
attached to the unchanged 720 binary/image as if the correction were present.
Independent source qualification passed; a successor composed-source/image
qualification remains required. Keep launch `subscriber_quality_policy_version` absent/`0` and the v2
candidate classifier/MMDB outside active inputs.

## Cause and correction

The [schema-750 independent control](server-schema750-qualification-20261001.md)
proved that an old server-0b8e UPSERT omits `arin_quality_verified` on conflict.
The column's default affects an insert; an update retains a previously stored
`true`. Changing the location with favorable risk/non-quality flags can
therefore preserve a positive which the strict Quality guard accepts.

The successor appends migration 751 (index 750), preserving every earlier
migration identity. In one transaction it adds nullable
`arin_quality_write_token` without a default, revokes all existing positives,
and installs a row trigger. Each corrected `SetConnectionLocation` statement
supplies a fresh token with the complete location/classification facts on both
insert and conflict-update paths. An insert lacking a token or an update with
a missing/unchanged token forces `arin_quality_verified=false`. Invalidating
updates retain the last token, so immediate reuse cannot restore a positive.
This is a trusted-writer protocol, not a historical anti-replay ledger.

Old 0b8e statements, schema-750 statements that explicitly supply the boolean,
identical legacy updates and direct updates remain SQL-compatible and cannot
retain or create positive evidence. A subsequent corrected classification can
re-attest with a fresh token. Existing live guards and rollups read the same
boolean and observe revocation without a reader-cache flush.

## Executed qualification

Only owned disposable PostgreSQL 18 and Redis 8 services on loopback were used,
with generated private fixture resources. Both exact container IDs were removed
and verified absent after all checks joined.

| Check | Result |
| --- | --- |
| Selected server/model/controller/router roots | 27 normal, 27 race; zero failures and zero skips |
| Vet for the same four packages | Exit 0 |
| Schema transitions | 749 and 750 to 751; contaminated-750 positive revoked; catalog/restart preserves fresh post-upgrade facts |
| Write protocol | Missing/null/unchanged tokens, immediate replay and collision, schema-750 inserts/upserts, repeated new-writer facts, identical and changed old upserts |
| Serving behavior | Real live-connection guard, warm native/fallback/named Quality, rollup, independent Speed behavior and default-off activation |
| Transaction ordering | Old transaction reads unknown, new writer commits positive, old conflict update commits and revokes; later new writer re-attests |
| Causal counterfactual | Removing only the row trigger makes both new model tests fail: retained `verified=true` and live-join `excluded=false` |
| Query-plan fixture | Pass on 20,000 providers, 40,000 current and 200,000 historical connections, with 239,000 location rows at schema 751 |

The original broader normal/race attempts are retained as diagnostics: the
generated fixture lacked the reserved `203.0.113.0/24` override used by the
existing coverage test and reached an absent GeoLite resource before coverage
assertions. They also selected and skipped an unrelated opt-in candidate-resource
test. Final successful runs use a fresh fixture covering all documentation
ranges and a precise selector. The server source needed no change for this
fixture correction. Query-plan timing is a local observation, not production
capacity or migration-lock qualification.

Independent qualification of the exact server `a464bb3e` tree passed 11
selected top-level roots (18 pass events including subtests) across the root,
model and router packages in normal and race modes, with no failures or skips;
`go vet` exited 0. Removing only the trigger in a test overlay made both
mixed-writer roots fail at the intended stale-attestation assertions in normal
and race modes. Disposable PostgreSQL and Redis were removed and verified
absent. The sealed independent receipt is
`/mnt/data/sn-testnet/sol-server751-independent-20261001/receipt.json`
(SHA-256 `a69645f0a59606c88d3a571c21c5d2523de45b4dcf1e42c79c845667019b4fff`);
its `SHA256SUMS` is
`f785cd77445eecd43e07ecb6721eabd89b39a0e14530f5d2d04a855229dee06e`
and all entries verify.

## Rollout boundary

Use a successor release containing both schema 751 and its token writer. Keep
v2 disabled, apply 751 with that successor's migration tool, then deploy the
serving/Connect/writer fleet. The corrected binary refuses schema 750 at startup.
Readiness is a lower-bound check: older binaries requiring 749 or 750 still pass
against 751; this is not a policy-activation test. An older migration tool rejects
a catalog newer than its local source and must not be used for this transition.

The migration's reset scans the location table and updates its positive rows
under the schema-change table lock. Qualify the intended database's row volume
and lock duration before scheduling it. Do not remove the trigger for a binary
rollback. Complete the guarded API fleet and capable writers, actual lookup
re-attestation, affirmative coverage, provider shadow diff, native-index/rollup
refresh and both operators' real miner-trail/FP2 load canaries before any v2
activation. The synthetic tests do not establish those production gates.

## Retained evidence

The sealed archive is `/mnt/data/sn-testnet/astra-arin-mixed-writer-20261001`.
It contains exact command selections, normal/race/vet streams, the trigger-free
overlay and its failures, source-file hashes, fixture diagnostics, and service
cleanup checks. `sha256sum -c SHA256SUMS` passes for all 36 entries.

- `receipt.json`: `bf016007d3dff3b2730d92b30d35d252039e9b6fc139fb842dd2ee5c9c3810d9`
- `SHA256SUMS`: `3be0a0764ff1aa74b5351b6745478c4439851c4996cdd332e332fcf22d71b589`
