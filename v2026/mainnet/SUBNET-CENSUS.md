# Authenticated SN25 census and owner trim planning

`subnet-preview` collects one complete, bounded, signer-free SN25 census and an
excluded netuid-0 membership baseline. It compares an explicit removal scope
with the reviewed source's owner-trim selection at that finalized block.
It never authorizes or executes a reset. `reset_ready` is always false.

Before an independently approved owner/generation/role policy exists, use the
separate signer-free `subnet-discover` command:

```sh
go run ./mainnet subnet-discover \
  --rpc https://rpc.example \
  --snapshot /secure/unapproved-finalized-snapshot.json \
  --retry-window 15m
```

It accepts an existing `runtime-snapshot` or combined `finalized-snapshot`
envelope with EVM ID 964 and the complete runtime 470 tuple. It checks the file
seal and retained code/metadata bytes, authenticates the same historical native
header through the selected route, and matches the retained runtime pins before
reading state. The v470 codec is tied to the reviewed source
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`; supplied artifact pins remain
observations, not proof of that source or an approved runtime.

The distinct `urnetwork-mainnet-subnet-discovery-v1` output retains complete
SN25 and excluded root forward/reverse membership maps, recorded owner and
registration generations, capacities, registration flags, activity, permits,
aggregate subnet alpha emissions (`emission_alpha_rao`) and raw storage evidence.
The existing 4096-seat bound, eight-worker limit, terminal empty pages and one
total 60-second to 15-minute deadline apply. Discovery fetches registration
values in sequential `state_queryStorageAt` batches of at most 128 exact keys,
requiring one matching-block change set with each key exactly once. Missing,
extra, duplicate or stale values fail closed; explicit null still passes through
the normal metadata default/optional and SCALE checks. Prepared values are
consumed once. Approved-policy previews retain their per-key transport.
Transient reads honor bounded HTTP/JSON retry hints without extending the
sample deadline. Final native/network/code checks
must still agree after all storage reads. Failures emit no partial artifact.

`membership_complete` covers these UID maps only. Every seat remains
`unclassified`; no immunity, custody, role or removal judgment is inferred.
`admission=unapproved_observation`, `finality_authority=rpc-assertion`,
`runtime_source_proven=false`, `reset_ready=false` and `apply_authority=false`
remain explicit. Exit zero means this observation completed. The file cannot
serve as `--policy` for `subnet-preview` or `owner-trim-plan`. Independent
genesis/runtime, owner/generation, protected-role and removal approvals, plus
custody/stake/lock/claim/history review, still precede any executable plan.

The [current-runtime qualification](evidence/runtime470-subnet-discovery-20261001.md)
keeps exact public metadata/live RPC evidence outside the repository and uses
an identity-free protocol projection with synthetic state in committed tests.
The retained public snapshot completed with 256 SN25 and 64 excluded root
registrations in 13.231 seconds after two per-key attempts exhausted their
15-minute windows under HTTP 429. This is operational discovery evidence at that
block; it does not close the independent approval or full launch-census gates.

```sh
go run ./mainnet subnet-preview \
  --rpc https://rpc.example \
  --policy /secure/sn25-census-policy.json \
  --retry-window 5m
```

The policy schema is `urnetwork-mainnet-subnet-census-policy-v1`. Its fields
are defined in [subnet_policy.go](subnet_policy.go). Supply independently
approved values for all of these inputs:

| Input | Required meaning |
| --- | --- |
| `netuid`, chain name, genesis and EVM chain ID | SN25 on the independently approved mainnet; EVM chain ID must be 964. A route name or observed endpoint cannot supply approval. |
| `runtime_version`, `runtime_code_hash`, `runtime_metadata_hash` | Complete exact runtime identity and artifact hashes at the sampled finalized block. No runtime version number is accepted as mainnet authority. |
| `runtime_source_commit`, `storage_profile` | Historical source `67dcf7f791dc495064c293f080a0702cb433e51e` or reviewed v470 source `923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`, with the unchanged compatible wire profile `subtensor-subnet-census-67dcf7f-v1`. Other sources require separate review. The supplied source assertion does not establish its source-to-Wasm build provenance. |
| `subnet_owner_coldkey_account_id` | Approved owner AccountId32, encoded as nonzero `0x` plus 64 hex digits. |
| `subnet_registration_block`, `subnet_generation` | Explicit current subnet incarnation, including a zero counter when that is the reviewed value. |
| `trim_maximum_uids` | Explicit capacity to preview, compared with live minimum/maximum and requested identities. |
| `remove` | Exact hotkey, coldkey and registration block for every requested removal. Numeric UID alone is insufficient. |
| `preserve` | The same complete identity plus explicit roles: `owner`, `ur-validator`, `third-party-validator`, `reserve`, `pool`, `escrow`, `custody` or `other`. |

Declare protected validators even when they currently lack a permit. Duplicate
hotkeys, missing generations and overlapping remove/preserve declarations are
refused before network reads. A file hash binds the supplied policy; it is not
a signature or evidence of operator approval.

The command verifies exact finalized runtime artifacts before decoding census
storage. Every storage and page request uses that same block hash. The final
canonical-height recheck must agree before output is published. Complete
`Keys` and `Uids` enumerations must match `SubnetworkN`, be contiguous in the
forward direction and agree with hotkey ownership, registration generation and
membership flags. Missing identity rows never inherit metadata defaults.
The netuid-0 census uses the same checks and is excluded from removal scope.

The output includes activity, permits, aggregate emissions, temporary immunity
and expiry, runtime owner immunity, owner identities, capacity, registration
settings, both subnet generation values, admin-window inputs and owner trim
history. Raw storage absence, effective metadata fallbacks and storage keys
are retained in the content-hashed envelope. This is authenticated against the
approved route and runtime pins; it is not a light-client storage-proof verifier.

Owner recognition and owner immunity are distinct. The source's immunity
routine sorts registered `OwnedHotkeys[SubnetOwner]` by registration block,
then UID, both ascending; takes `ImmuneOwnerUidsLimit`; inserts a registered
explicit `SubnetOwnerHotkey` at the front if absent; then truncates again.
All observed owner identities are conservatively protected by this preview,
including those outside the runtime's immune subset. Owner recognition for
recycled emissions is a different rule and is not reduced to this immunity
limit. See the [pinned registration implementation][registration].

The pinned trim algorithm stably sorts emissions descending and visits them
backward. It removes lower emission first, then higher UID on equal emissions,
skipping runtime owner and temporary immunity. Survivors are compressed in
old-UID order while retaining their identity/generation. The immunity threshold
comes from authenticated `MaxImmuneUidsPercentage` metadata. Its percentage is
floored and saturated as in the pinned SDK, and must be strictly below the
threshold. Capacity comes from actual storage, not nominal documentation
limits. See [trim source][trim] and [SDK percentage rounding][percent].

`verified_call_schema` appears only when authenticated metadata has a unique
`AdminUtils.sudo_trim_to_max_allowed_uids(netuid: u16, max_n: u16)` call. Missing,
ambiguous or changed call metadata produces no candidate selection. The source
permits subnet owner or chain root, with an owner rate check and an admin
window; reading this descriptor grants neither origin nor signing authority.
See the [admin entry point][admin].

Even an exact candidate-set match remains blocked because:

- This call accepts only netuid and capacity. It does not enforce the reviewed
  hotkey/generation set or finalized snapshot at execution. Later emission,
  immunity or registration changes can select different identities.
- The owner trim interval is a build-selected `prod_or_fast!` value that is not
  a metadata constant. A nonzero prior trim therefore reports
  `RESET_OWNER_TRIM_RATE_LIMIT_BUILD_VALUE_REQUIRES_REVIEW`; the preview never
  substitutes a nominal 30-day interval. A zero prior record is identified as
  the source's first-call bypass, without inventing an interval.
- Source-to-Wasm review, custody/collateral/stake/claim/history reconciliation,
  execution authority and post-reset census checks remain separate gates.
  This increment does not gather every mechanism's weights, commitments, EVM
  associations, collateral or stake position and cannot claim those audits.
- Runtime-selected removal of any protected or unresolved identity, or any
  difference from the approved complete removal set, is an explicit blocker.
  A permit bit alone never defines “all miners.”

The candidate equality flag concerns only the selected identity set at the
observed block. It does not claim the call currently passes all timing checks,
that a later call will select that set, or that literal reset is authorized.
The alternatives and exact postconditions remain in [MAINNET.md](MAINNET.md#exact-uid-census-and-reset).

Bounds are fixed per observation: 1 MiB input/ordinary RPC replies, 8 MiB metadata
replies, 4096 seats per subnet and owned-hotkey vector, 4096 total requested
identities, 128 keys per page, 82 bytes per enumerated key, and at most eight
concurrent storage workers. These are local refusal limits, not runtime capacity
claims. Every enumeration requires a terminal empty page. One complete sample
shares a 60-second through 15-minute deadline (default five minutes); transient
transport/overload reads retry their exact request. Integrity errors do not
retry into another block or route. Cancellation joins workers and publishes no
partial census.

Exit 0 means a complete census with no scope blockers; reset remains blocked.
Exit 3 means an identity/integrity refusal or a completed census with scope
blockers. Invalid input uses exit 2; transport/output failure uses exit 1.
An actual mainnet policy and live approved runtime remain operator inputs;
synthetic tests do not close that gate.

## Best-effort owner trim

SN25 launch has owner keys, without an assumed chain-root capability. Use
`owner-trim-plan` with the same independently approved policy to rank the
strongest safe partial trim the observed runtime permits:

```sh
go run ./mainnet owner-trim-plan \
  --rpc https://rpc.example \
  --policy /secure/sn25-census-policy.json \
  --retry-window 5m
```

This command uses the same authenticated reader once. It does not import a
caller-produced census or change the existing `subnet-preview` envelope.
Its separate `urnetwork-mainnet-owner-trim-plan-v1` output retains the complete
census, raw storage, exact policy-file hash and independent plan content hash.
The hash covers canonical Go JSON with an empty `content_hash`, prefixed by the
schema and a zero byte; it is reproducible for the same retained census.
Fresh samples have their own observed timestamps and are different evidence.

The planner evaluates each removal capacity from observed `MinimumUids` through
the lesser of `MaximumUids` and occupied count minus one. It keeps the reviewed
emission order, immunity, percentage rounding and call-metadata checks. It
rejects every candidate selecting a protected or unresolved generation, even
when an owner identity is outside the runtime's immune subset or a declared
validator has no permit. Missing or changed owner/subnet generations and
incomplete role scopes remain explicit blockers. The current census is bounded
to 4096 seats; capacities that remove nobody are represented by one compact
range, including a possible 65535 ceiling.

Candidates are ranked by the number of approved old generations safely removed,
then the least capacity reduction (larger capacity). Only the best candidate
retains the full removed-generation and survivor UID mapping. Temporary immunity
or minimum capacity may leave requested miners in place; each residual retains
its original hotkey, coldkey, registration block, observed UID and reason.
These residuals do not invalidate an otherwise safe partial selection. They
must remain visible in launch reporting; no full-reset success is inferred.

The best candidate can be conditional on a closed admin window or an unproved
owner cooldown. `runtime_checks_pass_at_observed_block` then remains false.
The unsigned method bytes contain only the authenticated pallet/call indices,
netuid 25 and selected capacity. They contain no signer, nonce, fee, era,
extrinsic signature or dispatch authorization, and are retained only for review.

`reset_ready`, `apply_authority` and `full_reset_completed` are always false.
The command does not supply an execution-time identity selection guard, audit
all collateral/stake/claim/history state, establish source-to-Wasm provenance,
or perform the mandatory post-trim subnet and root censuses. Open registration
also retains an explicit competing-registration/reentry gate. A successful
current selection cannot remove any of those execution blockers.

For this command, exit 0 means the best observed partial selection passes the
modeled source checks at the sampled block; it never means apply-ready. Exit 3
retains a blocked plan when there is no safe removal or timing is unresolved.
Identity/integrity refusal returns exit 3 without partial evidence. Invalid
input remains exit 2 and transport/output failure exit 1. There is no `--apply`
or signing-key option.

The signer-free [recheck and reconciliation guard](OWNER-TRIM-GUARD.md) now
rebuilds a retained plan from its authenticated historical census, compares a
fresh finalized census, and reconciles exact old generations, protected
survivors, UID compression and the excluded root census. It does not close the
runtime execution-time selection gate: the capacity-only owner call has no
atomic predicate for approved identities. Recheck/reconciliation results keep
all execution authority and full-reset flags false.

[registration]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/registration.rs#L242
[trim]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171
[admin]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L1853
[percent]: https://github.com/RaoFoundation/polkadot-sdk/blob/cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a/substrate/primitives/arithmetic/src/per_things.rs#L373
