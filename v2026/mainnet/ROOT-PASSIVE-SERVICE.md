# Current-runtime passive root service

The current root role is `passive_accumulate_in_place`: observe an independently
approved existing netuid-0 seat while its dividend basket accumulates. It runs
alongside both UR subnet validators. It never constructs, signs or broadcasts a
root transaction. Registration, root staking, claims, take changes, delegation
changes and optional basket trades need separately approved capabilities.

The [runtime470 review](../docs/spec/runtime-470-audit.md) verifies the exact
observed artifact and current source, retaining the documented Wasmi build-seed
exception and separate independent runtime approval. That runtime has neither `set_root_weights`
nor its old enable/cap storage. The official [migration][migration] removes the
root weight vector; [Root Reborn][root-reborn] describes dividend accumulation
without a target vector and optional coldkey/proxy basket trades. There is no
heartbeat transaction required for the selected passive strategy.

## Additive approval domains

A fresh unsigned launch composition uses these explicit versions:

| Input | Required value |
| --- | --- |
| Bootstrap chain config | `urnetwork-mainnet-bootstrap-chain-config-v4` |
| Root child config | `urnetwork-mainnet-bootstrap-root-config-v2` |
| Root child plan | `urnetwork-mainnet-bootstrap-root-passive-plan-v2` |
| Root service | `urnetwork-mainnet-root-passive-service-v1` |
| Embedded observation policy | `urnetwork-mainnet-root-observer-policy-v2` |
| Observation storage profile | `subtensor-root-passive-read-only-v2` |
| Root role implementation | `sn/mainnet/root-passive-service` |
| Root role strategy | `passive_accumulate_in_place` |
| Independent config approval | `urnetwork-mainnet-root-passive-service-approval-v2` |
| Service invocation config | `urnetwork-mainnet-root-passive-runtime-v1` |

The root child still references exact private service bytes by path and SHA-256.
Its `passive_service` contains the embedded policy, exact HTTP(S) `rpc_url`,
`read_retry_seconds` (60–900), private `checkpoint_path`, `maximum_samples`
(1–10,000), `interval_seconds` (1–3,600), and `stall_after_seconds` (120–3,600).
The policy requires the independently selected hotkey, coldkey, UID and nonzero
registration block; full approved genesis/runtime/source/code/metadata identity;
positive minimum root stake; expected delegate take; `accumulate_in_place`;
`observe_existing`; and positive finite native `valid_from_native_block` and
`valid_through_native_block`. All runtime source fields name immutable commit
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`. Observation does not change existing
automatic, current or pending delegation.

The root role has explicit netuid 0, its own identity/generation and independent
Ed25519 config approver. Its `action_approval_public_key_ed25519` is empty.
The approval binds deployment, complete root plan hash and complete service
config hash. Its signing message is the approval schema plus a NUL byte plus
canonical Go JSON with an empty signature. This is config approval only; no
native key or nonce is present. The two separately approved UR production
configs remain required, and the root hotkey cannot count toward their quorum.

Legacy bootstrap v1/v2/v3, root config v1, `explicit_root_weights`, original
signature domains and retained custody remain unchanged. They cannot acquire
passive authority by adding a field or renaming a strategy. Keep any already
signed legacy action and its liabilities; no conversion or new submission is
implied. Check for any externally held signed commitment before selecting a
fresh launch plan.

## Preparation and operation

Run the existing `bootstrap-chain plan`, then the independently accepted local
`apply`/`resume` workflow. A v4 preparation retains three real journals: chain,
contracts and passive root preparation. It creates no root signing-custody
journal, native signature, transaction or observer checkpoint. Contract and UR
admission paths accept this explicit v4 scope and preserve their own approvals.

The service invocation JSON has `schema`, the same exact `root_config` file
reference, and the complete independently selected `root_validator` role from
the v4 chain config. Review its content hash with:

```sh
mainnet root-passive-service plan --config /private/passive-runtime.json
mainnet root-passive-service run --config /private/passive-runtime.json \
  --accept-runtime-sha256 sha256:<exact-reviewed-runtime-config-digest>
```

Every invocation rereads and verifies all exact input bytes and the independent
service approval before opening the route or checkpoint. It then holds a shared
lock on complete original local preparation; missing/corrupt records or an
interrupted claim are refused, never repaired by the service. The existing
bounded root monitor owns the checkpoint and emits finalized observations.
The approved policy is handed to it in memory, avoiding a pathname reopen.
There are no signer, submission, signature import or cadence override flags.

The passive metadata profile checks both that required current read shapes
match and that the retired root call/gates are absent. The observer authenticates
the full runtime tuple and current canonical finalized state, ownership,
registration generation, stake/retention, delegate take, delegation and basket
storage. Any runtime or generation mismatch fails closed. `bootstrap-chain
readiness` observes UR and passive root state at the same finalized hash and
reports the root policy's finite approval window. `ready` remains observation
readiness; `activation_ready` stays false.

No production service has been installed or started by this change. Deployment
must pin the qualified binary and exact approvals, supervise the finite command,
retain its private checkpoint, and verify real observations and alerts. A new
window/config needs a new independent approval and preparation, without deleting
old records. Existing root registration and sufficient approved stake are launch
prerequisites; passive observation does not create them. Actual UR production
admission, source/runtime approval, owned custody, contract installation and
mainnet economic outcomes remain independent launch gates.

[migration]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/migrations/migrate_remove_root_weights.rs
[root-reborn]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/docs/guides/root-reborn.mdx
