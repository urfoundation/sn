# MG-08 offline contract prerequisites: focused qualification

Sol qualified SN `baa35de856236b3f50b0027f4135171ba89f1627`, tree
`f4cb21a26340f7cb225cba94896e0a7f9f25e5c3`. Merge
`79ff2c6e3d75c6d05d702748c2f80b3e050555d0`, tree
`b4bae252859276f58665a6da416d7a3cafb5d80a`, integrates that exact command,
custody-reader and test source onto the separately qualified MG-07 lineage.
Only `PRELAUNCH-FIXES.md` needed conflict resolution; both receipt paragraphs
and the SDK package-coverage gap were preserved. The integration adds no
successor proposal, signer or Safe executor.

All twelve new `TestBootstrapContract*` roots started and passed in normal and
race modes, with package PASS and process exit zero in both. Normal wall time
was 39.13 seconds; race was 294.57 seconds. No root failed or skipped. Each of
six isolated causal controls compiled and failed its intended named behavioral
assertion in both modes, giving twelve discriminating executions. The separate
[partial adjacent receipt](bootstrap-contract-successor-full-v3-qualification-20260929.md#separate-composed-smoke-and-partial-adjacent-coverage)
now records 150/270 roots passing in both modes, with 120 unrun. It does not
expand this focused receipt into whole-package or composed-release qualification.

The original handoff hashes verified before and after behavior. All eleven
physical source-fence entries stayed clean at their exact commits and trees.
Resolved module bytes remained identical, SHA-256
`fa7ad3963493cbfd59c7933c60f09f643cd85d18d31d3c922a6f26cc0254eae3`.
The qualification uses isolated Connect `b163f9dd`, SDK `516521fb` and server
`cfcbfcba`; the subsequently integrated server finality increment is outside
this behavioral graph. No live RPC, signing or transaction was performed.

| Retained receipt | SHA-256 |
| --- | --- |
| [Focused Sol result](/mnt/data/sn-testnet/qualification/mg08-contract-prerequisites-sol-20260929/FOCUSED-RESULT.md) | `67726b0f55e8ddbb6c0521134cdef47ff36214f35d15d53320b28d29c9d1f6ef` |
| [Focused manifest](/mnt/data/sn-testnet/qualification/mg08-contract-prerequisites-sol-20260929/FOCUSED-SHA256SUMS) | `6473ee202ddf6d97352acfde24090f9f1187bba2bbd2068dbe7d60ae33a30407` |
| [Twelve-root census](/mnt/data/sn-testnet/qualification/mg08-contract-prerequisites-sol-20260929/focused-roots.txt) | `d8ecbf6dae1dcacdbf1f52116e98e82888dc60122c277904e5b0fdb6d3c15cc0` |

Astra independently verified the manifest, exact clean candidate, package
result records, causal assertions and identical integrated Go source. This
qualifies offline prerequisite reporting. Historical receipts retain original
authority, attempts and lineage; eight completed actions leave only the
evidence anchor unfinished. An independently signed successor adoption owner,
Safe-inner authority and provenance, current canonical receipt audit, separate
relayer custody, full contract installation and native 10/90/role acceptance
remain open. The separate SDK 208-root package-coverage gap also remains open.
