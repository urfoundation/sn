# MG-03 historical receipt/native fee-context qualification

**Independent source qualification passed; shared integration is complete.**
The exact qualified server candidate is
`4152738039880408ade55edec3e42c1ecf2e40ec`, tree
`c685d73209c643c4137a1c3fe0546809059b34af`, based on qualified native finality
capture `5ff7bf0264b775920049910896c23ec58862429f`. It was merged after qualified
StorageProof `6201504ec18cd42b54083681c5dc5ca62fdcdb41` as server
`1bccc3cd7138faefddaa3ab4df15c5cc77e1bd4a`, tree
`1fd1f1155abfcfa600bd1891ef7bc56081f6c346`. The non-force push and exact remote
head on `codex/mainnet-server-hardening-20260927` were independently verified.
This records source integration, not an admitted production release.

The [offline fee-context contract](https://github.com/urnetwork/server/blob/4152738039880408ade55edec3e42c1ecf2e40ec/strecovery/NATIVE-FEE-CONTEXTS.md)
replays the existing pinned archive, receipt collection, checkpoint and GRANDPA
proof before deriving an exact historical context for each proved receipt
block. It joins the EVM block hash to every supported native Frontier commitment
within the original checkpoint-to-collection interval. It preserves the EVM
intermediate root, native child root and exact linked parent root separately.
Native and EVM heights never select a mapping.

Missing native mappings, multiple matching commitments and a checkpoint with
an unseen parent remain explicitly unresolved. A V2 certified descendant can
establish the original boundary's finality; its later headers cannot supply an
earlier receipt's execution context. The original collection and every archived
original/replacement/cancellation signature, origin and nonce history survive.

The mapped-account output is only the reviewed source-profile computation
`blake2_256("evm:" || signed_sender_H160)`. It supplies no executing-runtime or
payer admission. Native extrinsic indices and actual withdrawal, refund and
gas debit amounts remain null, as do nested actual gas fees. Runtime, payer,
fee, checkpoint/genesis, finality, accounting and spending authority remain
false. The command reads pinned local files only; it adds no RPC, signer,
broadcast, source-custody write or self-approval interface.

## Qualification and integration boundaries

Sol qualified all ten new roots and all 122 affected recovery/CLI roots in
normal and race modes. Each affected mode comprises 106 non-database recovery
roots, 13 CLI roots and three existing database roots under separately owned
disposable PostgreSQL/Redis services. Every selected root started and passed,
with package PASS, process exit 0 and no missing, extra, failed or skipped root.
Both database runs report command, cleanup, source-graph and module-graph exits
of 0, with no owned containers remaining.

All six causal controls compiled and failed their exact named semantic
assertions in normal and race: 12/12 selected root FAIL, package FAIL and process
exit 1. They cover substituted parent roots, missing/ambiguous mappings,
checkpoint parent coverage, post-boundary descendant mappings and changed
account-derivation namespace. Each actual mutant diff byte-matches the frozen
expected diff. Raw JSON, stderr, command arguments and mutant checkouts survive.

| Retained receipt | SHA256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/mg03-native-fee-context-sol-20260929/frozen-4152/SOL-RESULT.md) | `6eef830f6a92161cb08532942987db00da65d224679a0e0cf1dd8dc4285dd5fc` |
| [95-file raw manifest](/mnt/data/sn-testnet/qualification/mg03-native-fee-context-sol-20260929/frozen-4152/SHA256SUMS) | `7d5ef1f079d1289f609291dfbe03e5c6fcf48c788e5159627b81e46b1ca1d1d8` |
| [31-file disposable-service manifest](/mnt/data/sn-testnet/qualification/mg03-native-fee-context-sol-20260929/fixture/FIXTURE-SHA256SUMS) | `56710f200f0f35e4f201c58e7a586891592490c7a0a987223d11dcacc0be051f` |
| [Independent receipt audit](/mnt/data/sn-testnet/qualification/mg03-fee-context-integration-preflight-20260929/INDEPENDENT-RECEIPT-AUDIT.json) | `d02e4b4fac2cca18c2f534090e62a72e4915ef770611b90d496cba997ae62469` |

Astra independently verified all 95 raw, 31 fixture and 69 author manifest
entries, exact per-root raw outcomes, all causal assertions and diffs, and
both owned-service cleanup receipts. The clean exact source and eight physical
local dependency heads/trees remain pinned. Resolved module JSON before and
after testing byte-matches the author graph, SHA256
`8ec05bbede7e1b78960e6423f7e7e70ab41116a225ceb23fa507b78187856632`.
No author behavioral rerun was used for this audit.

Author checks passed compile-only builds for both packages, vet, formatting,
source/module fences and compilation of all six causal mutants. No behavioral
test body was executed by the author. The [frozen handoff](/mnt/data/sn-testnet/qualification/mg03-native-fee-evidence-20260929/HANDOFF.md)
has SHA256 `a64fb0a83247f19f1e977c0c2ac793fe1b84bb69eb22086d37799ea624c5a33e`;
its [69-artifact manifest](/mnt/data/sn-testnet/qualification/mg03-native-fee-evidence-20260929/SHA256SUMS)
has SHA256 `b41fcb1a606eee311b522dab60073b823b8141a2a3994f401e41b53048c70815`.
These author artifacts are not an independent behavioral receipt.

Integration preserved that storage-first order and every qualified candidate
blob. The two candidates modify disjoint server paths; the actual merged tree
matches the metadata preflight and changes neither `go.mod` nor `go.sum`.
Sol separately qualified a 30-root normal/race smoke on the exact integrated
source and physical graph: 26 recovery roots and four CLI roots, each with
package PASS and process exit 0, with no missing, extra, failed or skipped roots.
All nine physical source entries stayed clean and the resolved module JSON
stayed byte-identical. Astra independently verified all 20 Sol and 12 author
manifest entries, all four raw package streams and the physical graph again.
Composed vet passed; Astra executed no behavioral test body.

The evidence retains three distinct coverage scopes:

| Source | Positive coverage per normal/race mode | Causal coverage |
| --- | --- | --- |
| StorageProof `6201504e` | 16 focused + 6 adjacent roots | 21 controls, 42 executions |
| Fee contexts `41527380` | All 122 affected roots, including 3 disposable-database roots | 6 controls, 12 executions |
| Composed `1bccc3cd` | 30 of 138 available roots | No new controls or database run |

The scoped composed smoke does not establish full composed-package, production
service or release qualification. Its source graph SHA256 is
`a3e76005e2db001b4dcde211ae2a5bc1342ed5a2afd16b17fec3b3ca330bc9e9`;
resolved module JSON SHA256 is
`4074d412032078ec0ea10d73581924887334a4f1cb76b9e7e110ce6d2abb34bb`.

| Composed receipt | SHA256 |
| --- | --- |
| [Sol composed result](/mnt/data/sn-testnet/qualification/mg03-native-state-fee-context-compose-sol-20260929/frozen-1bccc/SOL-RESULT.md) | `747401ef196437ceb0cbe771586caa01b9b6ca221c24470e709ef33ddccd0def` |
| [20-file composed manifest](/mnt/data/sn-testnet/qualification/mg03-native-state-fee-context-compose-sol-20260929/frozen-1bccc/SHA256SUMS) | `81a3aba04e86c2066b1ea4e532c9c2843b9635bc2aeb6af921679cddcb92acf1` |
| [Independent composed audit](/mnt/data/sn-testnet/qualification/mg03-fee-context-integration-preflight-20260929/COMPOSED-SMOKE-AUDIT.json) | `15998f499524af1260f9b2cb52f940b057ebc9504b4dc9294481ceda84bd5b98` |

## Historical storage-proof dependency and gate impact

The separate StorageProof candidate accepts raw reads only at the exact
original collection-boundary native root. An earlier receipt's native child
root and linked parent root can both differ from that boundary. Therefore the
two current slices do not yet supply complete historical fee-storage evidence.

A later bounded interface must replay qualified fee-context derivation and
bind each raw read to the exact certified native header identity, derived root
and purpose: child events/state or parent executing `:code` and runtime. A
witness-supplied root is not authority; ambiguous mappings cannot be resolved
by selecting a convenient root. Checkpoint-parent absence stays unresolved.
That extension requires its own implementation and qualification.

Proved raw reads still need exact execution Wasm, runtime version, admitted
metadata/source correspondence and native transaction placement. General
actual fee attribution then requires bounded historical execution replay or
an admitted dedicated runtime fee record. Generic balance events share phases
with native call effects, and best-effort refunds can fail or be partial;
receipt gas, reported prices and block balance deltas do not establish the
actual native gas debit. A future event cannot repair historical receipts.

MG-03/PF-03 remain in progress. Actual fees stay null. Independent
checkpoint/genesis/runtime admission, historical native reads and fee
attribution, account nonce authority, service adoption, release composition and
live custody/restart remain separate requirements. Offline source qualification
does not approve a live checkpoint, authorize a spend or close mainnet gates.
