# Historical proof replay backend

`runtime-historical-proof --historical-proof-replay-v1` reads one bounded JSON
job from stdin and writes a report only after complete execution and post-state
comparison. The process owner must impose a finite total deadline (300 seconds
by default), bounded stdout/stderr capture, and kill/join on cancellation. This
worker's subprocess lifecycle is owned by the native producer's Go supervisor.

The job binds canonical SCALE parent/child headers, their exact hashes, the
complete extrinsic vector, exact parent runtime code, and raw `StorageProof`
nodes. Parent linkage, child number, extrinsics root, proved `:code`, proved
`:heappages`, executing state version, and reproduced child state root are
separate checks. Consensus seals are retained in the original header identity
and removed only from the runtime execution input. No seal/finality verifier or
source-to-code admission is supplied by this backend.

The raw admitted system version remains zero or one and must equal original
`Core_version`. Its storage layout is independent of the SDK's extrinsics-root
layout: system version one uses storage V1 and extrinsics V0. Both body checks
preserve complete SCALE-encoded opaque extrinsics and reject the wrong layout;
there is no accept-either fallback.

Capture executes `Core_execute_block` in the same onchain context as strict
replay. In the pinned SDK, an explicit proved `:heappages` value selects static
memory sizing only in that context; the offchain proof helper ignores it.
Absent heap overrides retain the bounded dynamic strategy. Heap parity and
growth regressions are authored but await independent execution.

Incremental refill regressions exercise the public directory/feed capture
entry from an empty node directory. Each response retains only the requested
node after proving membership with the pinned SDK's exact top or child read
proof at the requested prefix. Six authored roots cover ordinary reads/writes,
unread siblings needed by deletion, top/child ranges and odd nibbles, missing
iterator/root nodes, and a foreign acknowledgement followed by exact restart.
They await independent compilation and execution. These in-process Rust tests
do not replace qualification of the Go broker, process deadline or durable
producer cursor.

The executor and trie implementation are pinned to SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`. A strict adapter runs fallible
`delta_trie_root` and `child_delta_trie_root` before the SDK root writer. This is
necessary because the pinned `trie_backend_essence.rs` writer logs an incomplete
trie error and can return the old root. Root equality alone would therefore not
prove that all required write paths were present. Missing reads, child roots,
child nodes and write paths refuse; authenticated absence remains distinct.

Current resource limits are 96 MiB JSON, 8192 distinct proof nodes / 24 MiB total
decoded proof, 8 MiB runtime code before / 32 MiB after decompression, 8 MiB block
body, 16384 extrinsics, 64 KiB headers, 64 digest items, 64 MiB Wasm memory,
65536 metered host/argument/iterator steps and 64 MiB cumulative metered I/O.
The v2 host profile retains the original `storage_calls` and `storage_io_bytes`
wire fields for these stricter cumulative totals. Rolled-back work
still counts. A bounded original job is never retried with a skipped state or a
replacement root when a limit is reached.

The `substrate-proof-bounded-hosts-v2` profile supports bounded top/child
reads, writes, individual and prefix deletion, iteration, append, roots and
balanced transactions, plus allocator, logging, hash and trie helpers. Prefix
and child-kill calls delegate the exact SDK overlay/backend limit semantics:
zero limits still clear overlay entries and repeated limited calls do not
invent cursor progress. Every backend iterator step shares the job's meter;
incomplete iterator creation or traversal traps instead of inheriting the
SDK bulk-delete helper's logged-error/partial-success result.

A filtered host registry reuses exact pinned SDK ed25519 verification,
sr25519 verification v1/v2, and secp256k1 recovery/compressed recovery v1/v2.
Its static and dynamic dispatch both charge work and bound memory reads before
argument allocation. No key generation, signing or keystore functions are
registered. Proof-size observation follows the exact SDK no-recorder sentinel
`u64::MAX`; serialized proof length is never substituted for dynamic usage.
Other imports trap if invoked. BLS host calls, offchain I/O, runtime spawning
and runtime-version extension semantics remain outside this host profile.
The synthetic controls are not evidence that a production runtime is admitted
or that its complete invoked host surface is supported.

The retained official v470 artifact's read-only structural census found
original named fee-handler functions and imports for this host increment.
Names alone do not establish callsite meanings, a source build match or
authority for currently observed spec472. The original compressed code and
every function body remain unchanged by the host registry.

Every successful report states `anchor_authority=caller-supplied-unapproved`,
`runtime_admitted=false`, `production_selection=false`,
`native_fee_withdrawal_refund_observed=false`, and `native_fee_debit=null`.
Historical fee verification still requires independently admitted original
code and a fee-specific execution observation mechanism proving actual payer
withdrawal and actual refund, including zero, partial and failed refunds.
Receipt gas, generic same-phase balance events and balance deltas do not supply
that witness. Full historical replay and MG03 remain open beyond this milestone.

The deterministic tests construct complete parent state using SDK
`TestExternalities`, retain every raw top/child/value node, and independently
construct the expected child map/root. They exercise the public JSON decoder,
real Wasm storage hosts, missing read/write/child paths, unchanged-root fallback,
body/header/code substitution, unsupported hosts, transactions and bounds.
They do not use an actual admitted Subtensor runtime or claim fee attribution.

An optional `observation_profile` now binds the exact original code digest and
function-body digests/ranges. The registry wrapper reads the real Wasmtime
function-relative call stack at host entry; it inserts no runtime instructions.
Before executing, it proves that every code body remains byte-identical through
the pinned SDK's memory normalization. The job owns its trace, transaction
stack and work limits. Rollback discards effects while retaining cumulative
work; incomplete execution or changed post-state emits no trace.

The trace permits at most 32 nonoverlapping rules, 64 stack frames, 4096 retained
records and 2 MiB cumulative encoded records. Labels such as `fee-withdraw` are
supplied review references, not proof that a function implements that role.
`hook_observations.authority` therefore remains
`caller-supplied-unapproved-callsite-profile`; all native fee fields stay
unknown. Complete refund branch coverage, an admitted original-code callsite
map and finality still need to be joined before this can establish an actual fee.
The new controls exercise original-code frames, rollback, ambiguous nested
labels, range/body substitution, cumulative limits and post-state refusal.

When `observation_profile.metadata_sha256` is present, the executor calls
`Metadata_metadata` on the same original Wasm using stateless hosts. It requires
that exact digest, canonical metadata v14/v15, unique pallet/event indices,
32-byte payer and native u64 amount layouts. A caller cannot replace metadata
with a supplied decoder table. The bounded decoder retains exact event bytes,
`ApplyExtrinsic` placement and `Ethereum.Executed` transaction identity from the
selected original callsite trace. Its current payer mapping is the reviewed
Subtensor `blake2_256("evm:" || H160)` form; admitting another runtime or mapping
requires its own explicit implementation and review.

`hook_observations.fee_events` reports candidate withdrawal/refund pairs with
authority `original-runtime-metadata-and-unapproved-callsite-profile`. An actual
zero Deposit differs from an absent refund; missing withdrawal/refund or
initialization/finalization placement leaves debit unknown. Late unmatched
events remain counted and retained, and conflicting payer, amount width,
duplicate transaction/effect, ordering, topic grammar or refund-over-withdrawal
refuses attribution. Unlabelled same-phase balance events are not selected as
gas. These facts still do not prove a complete failed-refund branch or establish
cryptographic source review/finality. The top-level native fee authority remains
false/null, including when a candidate pair is present.

## Constructing an original callsite profile

`runtime-observation-profile inspect WASM 0xSHA256 [FUNCTION_INDEX ...]` reads the
exact original artifact, decompresses it within the existing 32 MiB bound, and
emits a function/import/global census. Up to 32 selected functions also retain
instruction bytes, function-relative offsets, and complete direct/indirect call
operands (tail calls are distinct). This command does not instantiate Wasm. A
name is a navigation aid; it is never evidence that a function has an economic
meaning.

`runtime-observation-profile assemble WASM PROPOSAL_JSON 0xPROPOSAL_SHA256` emits
the existing compact `ObservationProfile` bytes, without a wrapper or trailing
newline. The proposal has schema `urnetwork-original-wasm-profile-proposal-v1`,
`profile` containing the intended original profile, and `reviewed_calls` with
one entry per rule in the same order. Each entry repeats `function_index`,
`offset_start`, `offset_end` and every call in that range from the inspection.
Ranges must start and end at decoded instruction boundaries; code and body
hashes, callees, original exported i32 memory bases, replay memory limits and
all existing capture bounds must match. The output retains the separately
supplied review digest; it does not create a reviewer signature or admission.

The original runtime473 artifact has an unexported mutable i32 global0 and only
`__data_end` and `__heap_base` global exports. The compiler therefore refuses a
recipe that invents an exported `__stack_pointer`. Its instruction listing can
support review of the actual stack/heap layouts, but does not derive a memory
recipe, a financial label, or an approved production profile automatically.
Original economic call ranges and layouts, complete principal storage/cause
coverage, original-block replay and independent signed producer authority are
still required before production observations can be admitted. No synthetic
fixture address or whole-function name match substitutes for those inputs.

A profile may now explicitly declare up to four `original_globals`, appended to
its existing JSON field order. Each entry has `global_index` and the exact name
`__urnetwork_observe_global_INDEX`; indices must be increasing. The engine
permits only a defined original i32 global and an unused export name. It appends
that export while proving every non-export section byte-identical and every
original export unchanged and in order. Its execution-cache key hashes this
export view, separately from the authenticated original `:code` and job hashes.
The observer only reads the alias. Empty declarations preserve old wire bytes
and the old executor identity. Capture and strict replay still require the same
complete post-state result.

This makes a reviewed original stack global accessible without inserting or
changing instructions. It does not identify a selected caller's stack frame:
a layout must account for the actual nested-call frames at that exact original
host call. The focused Wasmtime regression checks a precise decoded call range
and live original memory; actual runtime473 ranges and field layouts remain a
separate review and execution obligation.

A native epoch rule may explicitly set `host_snapshot` to
`ext_allocator_malloc_version_1` or `ext_allocator_free_version_1`. The original
range must be exactly one decoded direct call to that `env` import, with the
original allocator ABI. It cannot select an indirect call, a wrapper function,
or an ancestor frame. At that boundary the observer reads the selected memory
before invoking the unchanged SDK allocator once. The resulting `host` record
names the original host in `key_hex` and contains neither storage value nor
storage-return fields. Existing transactions discard these records on rollback,
cumulative evidence bounds remain charged, and an incomplete replay releases
no report. Without this optional field, storage-only profiles retain their old
serialization and behavior.

This permits review of an original epoch vector that remains live after the
last storage callback. It supplies no allocation amounts, UID mapping, epoch
identity, or economic interpretation beyond the explicitly captured bytes.


An explicit `epoch_layout: "single-mechanism-original-keys-v1"` joins these
observations without inventing stack fields. The decoder requires the original
mechanism-count option to select exactly one mechanism, then uses the saturated
sum of the three original pending-tranche returns. `native-uid-census` rules
retain original `Keys(netuid, uid)` gets; exact metadata types, dense unique UIDs,
unique AccountIds and pre-epoch ordinals supply the roster. `native-epoch-index`
retains the actual `SubnetEpochIndex` write. Neither a BTreeMap's order nor a
later chain query supplies a missing UID or epoch. Yuma consumes the explicit,
hash-bound joined total and identities while still requiring every original
allocation stage. Legacy profiles omit this field and retain their old bytes.

A selected drain may declare exactly two canonical `state_reads` keys (at most
64 bytes each). They are observer reads of the current proof-backed execution
overlay at that callback, retained separately as `execution_state` with explicit
presence/absence; they are never labeled as Wasm memory or original runtime
gets. Capture retains those parent inclusion/absence paths and executes the
same bounded observer against its recorder, including the original execution
phase. Strict replay independently samples the actual overlay. Replay charges
key/value reads to the shared work budget, bounds each returned value to eight
bytes, and preserves transaction rollback. Registration must be observed present;
generation absence may use only the independently authenticated metadata default.
A missing proof path or observation is not absence.

The operational lesson is to adapt to the actual compiled layout with explicit
provenance: a source variable need not survive in a stack slot, and a pure late
vector need not coincide with a storage callback. These capabilities and their
synthetic tests do not supply a complete runtime-473 economic profile, successful
original-block replay or independent authority approval.


A rule may restrict its semantic caller with `storage_call`: `operation` is
`get`, `set` or `append`, and `path` is an innermost-first prefix of at most
eight original call frames. Every frame pins `function_index`,
`function_body_sha256`, `offset_start` and `offset_end`. The final frame equals
the rule's semantic caller; each range is exactly one decoded direct call to
the next inner function, ending at the exact original `env` host and ABI.
The observer matches the full prefix before reading any memory. Sibling hosts
with different stack frames cannot borrow a capture. Intersecting prefixes are
refused even when their semantic callers have different depths; disjoint
get/set prefixes may share one semantic caller. Unselected writes still enter
the independent complete principal mutation census.

`recipient_layout: "original-storage-credit-pairs-v1"` joins actual recipient
storage reads and writes. Collateral and liquid amounts are separate causal
pool deltas; fully captured recipients need no invented liquid callback.
Original Owner, SubnetOwnerHotkey and AutoStakeDestination gets preserve actual
presence and ordinals, including a zero-entitlement Owner read without a fake
write. The owner-recycle branch skips its ordinary Owner get, so its exact
get/set rules must explicitly set `recipient_owner: true`. This one fixed
recipe reads `Owner(original captured hotkey)` through the live overlay and
retains one `execution_state` key with null or exactly 32 bytes. It cannot
select arbitrary keys or borrow a stale coldkey stack slot. Initial capture
runs the same observer to retain this dynamic proof path; its temporary trace
is discarded after the original post-state agrees. Only independent strict
replay can publish observations. Missing proof remains refusal, not absence.

An original `metadata_sha256` pin validates the runtime-generated layout even
when fee accounting is unselected. Only explicit fee event callsite rules
request fee candidate decoding; complete fee authority still requires the
metadata pin. Full Yuma input capture is a separate optional profile extension,
requiring authenticated settings, effective stake relations, weight/bond rows
and commit queues when selected. Output vectors alone do not provide it.
