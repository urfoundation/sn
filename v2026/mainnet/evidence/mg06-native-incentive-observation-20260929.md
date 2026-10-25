# MG06 native incentive observation candidate — 2026-09-29

This increment closes the CLI's missing bounded native epoch-event ingestion
step. It does **not** establish the 10% provider / 90% owner-recycle outcome,
the pre-withholding miner denominator M, quantization tolerance Q, or live
activation. Source is isolated on `codex/mainnet-economic-observation-20260929`,
based on SN `3fcec46e9e0e60e46c6f56040122e941bc338815`.

`observe-native-miner-emission` reads an explicit 1–128 block window, approved
mainnet identity/runtime tuple and exact subnet generation. It retains linked
headers, exact metadata, complete raw events and pending-budget facts. Parent
execution metadata, full body commitments, event envelope/phase/field shapes,
mechanism storage indices, epoch correspondence, closing ancestry and canonical
boundaries are checked. Unknown profiles, malformed/missing results or semantic
conflicts stop completeness while preserving the completed prefix and attempted
block. Explicit storage null is distinct from an omitted JSON result.

Only the existing single-mechanism policy is admitted. Event UIDs remain slots;
they do not acquire hotkey, provider or role-generation identity. An event sum
uses arbitrary-precision alpha atomic integers and remains separate from M.
Positive pending server emission plus a zero incentive vector cannot establish
M=0: the reviewed runtime can redirect the miner tranche to dividends. Runtime
I32F32 normalization, I96F32 multiplication and per-UID u64 truncation require
complete native inputs to establish Q. Stored block alpha-out can be stale, and
the current block's accrual may already have drained before post-state is read.

Every artifact keeps economic amounts/outcome null and live authority flags
false. `complete` means only that the explicit archive range was read and
rechecked. Finality remains the owned RPC's assertion; no independent GRANDPA
justification, trie storage proof or source-to-Wasm provenance is synthesized.
Cross-window exact-once accounting, activation drain, signed validator policy,
recipient generations, collateral capture/claim and actual recycling remain
separate gates. The command accepts no signer, secret or submission option.

The semantic review is pinned to Subtensor
`67dcf7f791dc495064c293f080a0702cb433e51e`. Exact downloaded GitHub source blobs
were verified against that commit's complete git tree. The retained manifest
covers `lib.rs`, `macros/events.rs`, `subnets/mechanism.rs`,
`epoch/run_epoch.rs`, `coinbase/run_coinbase.rs` and `coinbase/alpha.rs` at:

`/mnt/data/sn-testnet/qualification/mg06-economic-observation-20260929/astra/reviewed-source-manifest.json`.

Eighteen new deterministic top-level test roots cover complete and partial local
RPC reads, zero-incentive uncertainty, exact sums beyond u64, compatible event
reindexing, changed metadata semantics, wrong phases, extra mechanisms, duplicate
and malformed events, skipped/deferred slots, null versus omitted results,
generation/mode changes, body/epoch/canonical conflicts, parent runtime upgrades,
omitted terminal events, resource bounds, cancellation and CLI exit semantics.
The proposed adjacent selector has 170 roots including those 18; exact static
enumerations and causal-control edits are retained in the external handoff.

Astra performed formatting, compile-only `go test -c`, static test enumeration,
source review and vet. **No behavioral test was run by Astra.** Sol medium owns
the normal/race qualification and causal mutants; this candidate note does not
assert that they passed. Exact frozen source, command exits, file digests and
physical dependency fences are in the external `astra/HANDOFF.md`.

During development, shared server root advanced from `b7c8c743` to `44636e5e`.
Initial compile successes are unfenced environment evidence only. Final
compile/vet use the frozen server `44636e5ee33a2e481634a6391f74655b99c43923`
worktree through an **external** absolute-replacement modfile, with Connect
`b163f9dd9ac374942fe97331f26631248a9c1f81` and SDK
`516521fb16da46c9f4bff0b58221e1941694f616`. Tracked SN module files and shared
worktrees are unchanged by this candidate. Later SDK/composed-release graph
qualification is separate and must not be inferred from this compile.

No live RPC, signing, key custody, submission or deployment occurred. Independent
mainnet genesis/runtime approval and complete economic outcome evidence remain
unresolved. Readiness or qualification cannot manufacture those approvals.
