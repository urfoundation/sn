# Root validator observation foundation

`root-preview` and `root-monitor` implement the read-only foundation for the
separate Bittensor **netuid-0** role. They do not instantiate the UR validator,
submit weights, load signing keys, stake, register, claim dividends or grant a
Substrate Root origin. **No mainnet activation or signing readiness is claimed.**

```
sn-mainnet root-preview --rpc "$OWNED_MAINNET_RPC" --policy /approved/root-observer.json
sn-mainnet root-monitor --rpc "$OWNED_MAINNET_RPC" --policy /approved/root-observer.json \
  --samples 120 --interval 30s --checkpoint /owned/root-observer/finalized.json \
  --metrics-file /owned/telemetry/root-primary.prom --metrics-role primary
```

The policy must be supplied independently of the node observation. There is no
default route, fallback node, mainnet genesis, signer, spend limit, root seat or
mainnet runtime artifact. EVM ID **964** is required; ID 945 is refused before
root storage is read. A previously testnet route is admissible only after its
observed identity matches the separately approved mainnet policy.

## Policy and supported observation profile

Use strict JSON with no duplicate or unknown fields. The policy is content
hashed in every sample; that checksum is not a signature or approval.

| Field | Required meaning |
| --- | --- |
| `schema` | `urnetwork-mainnet-root-observer-policy-v1` |
| `role`, `netuid` | `bittensor-root-validator`, `0`; never a UR subnet role |
| `native_chain`, `genesis_hash`, `evm_chain_id` | Independently approved network identity; nonzero 32-byte genesis and ID 964 |
| `storage_profile` | `subtensor-root-read-only-v1` |
| `runtime_source_commit` | The inspected profile source, `67dcf7f791dc495064c293f080a0702cb433e51e` |
| `runtime_version` | Exact `specName`, `specVersion`, `transactionVersion`, `stateVersion` |
| `runtime_code_hash`, `runtime_metadata_hash` | Approved nonzero `:code` storage hash and Blake2b-256 metadata hash |
| `hotkey_account_id`, `coldkey_account_id` | Explicit nonzero `0x` AccountId32 bytes; no key material |
| `expected_seat` | Approved `{ "uid": ..., "registration_block": ... }`; null produces a blocked adoption preview |
| `minimum_stake_rao` | Explicit positive canonical decimal string; total root hotkey stake, not a claim that all stake belongs to our coldkey |
| `expected_delegate_take_u16` | Explicit raw delegate take, including zero when intended |
| `basket_strategy`, `delegation_strategy` | `accumulate_in_place`, `none`; other strategies require a qualified successor |

The profile's source lock describes the semantics inspected for this adapter;
it is **not an assertion that current mainnet runs that source**. A reviewed
source-to-Wasm mapping remains an external activation gate. Another source needs
a separately reviewed compatible observation profile; changing only a version
number does not authorize different pruning or basket semantics. No live
mainnet pins are supplied by this change.

The reader authenticates exact metadata bytes before decoding storage, then
checks consumed map hashers, key and value types, query modifiers and bounded
defaults. Every runtime, storage and paged census request carries the same
finalized native hash. The hash is confirmed at its height before and after the
census. The owned RPC remains the state authority: this is not an independent
storage-trie proof verifier or a native/EVM finality mapping proof.

## Evidence and readiness scope

Finite `root-preview` retains `schema: urnetwork-mainnet-root-monitor-event-v1`
and a complete content-hashed snapshot when obtained. That snapshot uses
`urnetwork-mainnet-root-preview-v1` and keeps raw absence, metadata fallback,
storage key and effective SCALE bytes separately.

Long-lived `root-monitor` now emits compact
`urnetwork-mainnet-root-monitor-event-v2` records. Migrate log consumers before
changing the producer: `observation` replaces the potentially large `snapshot`
with its content hash, policy hash, finalized position and read-only result.
It is absent after incomplete or inconsistent reads. Retrieve a complete census
with finite `root-preview`; the diagnostic is not a replay archive. Closed
`read_phase`/`read_cause` preserve available sample/continuity and timeout,
transport, unavailable or integrity facts, without raw RPC error text.
`diagnostics` acknowledges earlier output only; `publication` describes optional
textfile publication, independently of chain observations. Activation stays false.

The observation includes:

- Complete root `Keys` and `Uids` enumerations, cardinality against
  `SubnetworkN`, both mapping directions, coldkey ownership, registration block,
  raw total root stake and immunity for every seat. Missing default-valued
  ownership or registration rows cannot fabricate a seat.
- The selected seat's approved generation, minimum stake and delegate take;
  observed stake rank and lowest non-immune pruning candidate. Ties follow the
  inspected source's registration age and UID order. The retention margin is
  relative to the current candidate, not a future survival guarantee or owned
  principal balance.
- Stored root weights, root-weight enablement, threshold, TAO weight, cap,
  last-update rows, rate limit and root stake unlock interval. An empty custom
  weight vector is required for the explicit accumulate strategy. A closed
  root-weight setter does not prevent read-only accumulation.
- Automatic parent delegation plus child, parent and pending child maps for
  every netuid retained in `NetworksAdded`, including stored false entries.
  Turning off automatic delegation never hides an existing assignment. Any
  nonempty assignment blocks the version-1 no-delegation policy.
- Basket shares, fixed-point rate and our configured coldkey's signed claim
  watermark as exact raw storage. This preserves evidence; it does not calculate
  NAV, all stakers' entitlements or a withdrawable balance.

`status: ready` and `read_only_ready: true` mean only that this complete snapshot
meets the **read-only** adoption policy. `activation_ready` is always false and
the explicit activation blockers remain present. No command exit code admits a
transaction. Exit 0 is a successful current read-only preview; exit 3 is blocked
policy/identity/finality; exit 1 is an unavailable observation or local I/O
failure; exit 2 is invalid command/configuration.

Effective custom-weight eligibility is deliberately
`not-qualified-for-custom-weight-submission`. Raw root stake is not substituted
for the runtime's parent/child attribution, fixed-point TAO weighting, owner
exception, destination diversity or concentration checks. The observed threshold
and limits are census inputs for that future adapter, not an authorization to
send UR UID weights to root.

## Bounded operation and recovery

The monitor has no background signing work or unbounded internal loop. `--samples`
is 1 through 10,000 (default 1); each sample has a total `--retry-window` of
300 seconds by default, configurable from 60 seconds through 15 minutes.
Transport failures and HTTP overload/timeouts use the existing read retry path
at the exact key/block. Independent reads use at most eight joined workers;
there is no artificial RPC request rate limiter. Key pages contain at most 128
entries; a sample refuses more than 4,096 seats or enumerated keys, unbounded
vectors, malformed counts and repeated/foreign keys. These are local resource
ceilings, not assumed chain capacities.

An unavailable sample is emitted and the next bounded sample proceeds; it never
reuses old ready data as the current result. Policy drift also remains visible
while monitoring continues. Identity, malformed evidence or finalized conflicts
stop this observer. A checkpoint write failure exits rather than losing the
declared durable continuity. Context cancellation joins workers and closes HTTP
idle connections. All failed GETs remain distinguishable from returned
contradictory evidence.

`--checkpoint` reuses the mainnet monitor's process lock, atomic durable write,
checksum, network binding and retained finalized clock. It stores chain
continuity only, not root-role approval or previous mutable seat state; every
sample rereads the root policy's full census. Use a separate path for this role.
Restarting with a stalled retained head cannot report ready. A retained progress
timestamp ahead of the host clock is conservatively stalled until genuine
finalized advancement resets it; clock rollback cannot extend readiness.
Unchanged finalized progress does not rewrite the checkpoint.

## Independent operational output

The daemon uses the shared [bounded diagnostic owner](../diagnostics/README.md)
from flag admission through joined cleanup. stdout/stderr aliases share one
writer. A physically full pipe, disconnected socket or refused regular-file
logger cannot stall the read loop or its shutdown. Queue admission is not
delivery; the final bounded drain may discard queued records. Use a supported
journal/socket/pipe, or an embedding with an actual context-aware write contract.
Ordinary synchronous file redirection is visibly unavailable to this daemon.
Finite `root-preview`, plans and bootstrap commands keep their ordinary output
contract. Required checkpoint writes, identity/finality contradictions and real
cleanup errors remain hard; optional log failures never grant action authority.

Optional `--metrics-file` reuses the existing exclusive atomic textfile owner
and Fluent Bit node-exporter collector path. Supply a unique `--metrics-role`
matching `[a-z][a-z0-9_-]{0,31}` from the independent host role census. Different
root observers on one collector target require distinct role names and file
paths. Identity, hashes and RPC errors never become metric labels. Families use
`sn_mainnet_root_monitor_`, keeping root observations separate from UR validator
and generic chain metrics; diagnostic series add only `events`/`diagnostics`.

Metrics separate sample time, current complete read, read-only result, last
successful read, retained finalized evidence/progress, prior textfile
acknowledgment and log delivery/drop counters. Status codes are 0 starting,
1 ready, 2 policy blocked, 3 unavailable, 4 stalled, 5 finality conflict,
6 RPC integrity and 7 checkpoint failure. Publication codes describe the
**previous** acknowledged state: 0 unconfigured, 1 starting, 2 published,
3 retrying, 4 ownership error and 5 unavailable. A directory-sync failure may
follow visible rename; the file cannot acknowledge that write itself. Ordinary
publication errors retry on the next bounded sample. Failed admission or changed
path ownership disables only that publisher, leaves the refused target untouched
and is visible in events as `unavailable`/`ownership-error`. Reopen after repairing
the owned path. The required continuity checkpoint remains independent.

Existing metrics are not refreshed on startup, and successful reads during a
log outage still publish independently. No configured metrics file means delivery
is **not independently observable** from this command: `unconfigured` is not
healthy output. Missing/refused files need an external expected host/role census;
stale files and silent process loss need the independent Mimir evaluator. Local
regular-file operations remain synchronous, size bounded and joined; this change
does not promise cancellation of a hung filesystem.

[Root alert examples](root-monitor-alerts.example.yml) use provisional thresholds.
The default 15-minute sample threshold accommodates the 300-second sample budget,
a separate bounded continuity check and ordinary cadence. Adjust it for a longer
configured retry window/cadence; do not shorten read retries to make telemetry
appear fresh. Quiet logs alone are not failed protocol progress. No collector
configuration, rule installation, notification delivery or deployment is claimed.

## Remaining deployment and signing gates

1. Obtain the owned route's independently approved mainnet genesis, exact
   runtime artifacts, reviewed source mapping, root hotkey/coldkey, generation,
   minimum stake, take and custody approval. This implementation performs no
   mainnet observation without those explicit inputs.
2. Adopt an approved existing seat with historical admission provenance, or
   qualify a registration path with actual price protection. In the inspected
   source, `root_register(hotkey)` and `rootRegister(bytes32)` have no maximum-burn
   argument, and `register_limit` rejects netuid 0. A fresh quote is not an atomic
   cap. A separately authorized external registration receipt or a separately
   qualified reverting custody wrapper remains necessary for new seats.
3. Connect the offline-qualified [one-action root owner](ROOT-ACTION.md) to
   separately qualified production authority, custody and canonical receipt
   adapters. The core supplies a mortal root basket encoder, durable exact-byte
   signing/recovery and nonce ownership; it is not a deployed signer or a hard
   native fee cap. Bounded staking, take, delegation, claim and re-registration
   remain unimplemented. Online observers must not acquire coldkeys or silently
   fund a retention gap. Accumulate-in-place needs no periodic transaction.
4. Qualify effective custom-weight eligibility and a selected strategy if
   accumulation is changed. Preserve existing child/basket rights; audit
   retired-network delegation history and the complete basket NAV/all-staker
   custody before an economic transition. Current raw counters do not settle
   those historical rights.
5. Deploy this read-only observer into the existing alerts/on-call stack, record
   its executable/policy hashes and separately qualify the UR validators. Root
   membership does not satisfy the requirement for a second UR validator.

Source references: [root admission and pruning](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/root.rs),
[root pruning order](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/registration.rs),
[root basket setter](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/weights.rs),
[stake attribution](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/staking/stake_utils.rs),
[storage/defaults](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/lib.rs).
