# Safe archive census qualification

This paired candidate implements the [bounded read-only archive capture](../SAFE-HISTORY-CAPTURE.md): complete native block bodies, native Frontier digest binding, complete EVM transaction/receipt commitments, direct Safe call and committed-log projections, and private create-only durable witness publication. The exact source pair is:

| Repository | Commit | Tree |
| --- | --- | --- |
| SN | `36fea176ba1ad54d9fb5a4835612d8fcd4503a00` | `fc0f59cece81323cd9578d68897a3401346dbab9` |
| Server | `d21492c319315370ee2c4ec17b7605acd3a50ca8` | `485a841291cd39556e777f55d233042db65b1c7a` |

This documentation child changes only Markdown beyond the qualified SN source. Server source is unchanged. The original eight receipts, independent authority, signed transaction bytes, nonce claims, counted attempts and liabilities are not opened or changed by the capture command. No live RPC, signing, transaction, service action or activation was performed.

Astra max owns implementation, debugging, fixes, compile/vet and independent audit. Sol medium owns all behavioral executions. Both source packages and all ten exact mutations compiled and passed vet; mutation checkouts were restored clean. The author ran no behavioral tests.

## Independent qualification

Independent offline qualification and the author raw-stream audit are sealed. All 68 selected root executions passed: ten SN and four server focused roots, plus twelve SN and eight server adjacent roots, each normal and race. Exact top-level RUN/PASS census, package PASS and exit 0 were verified for every positive stream, with no skips, root failures, race reports or setup/runtime confounders. No whole-package or live deployment qualification is claimed.

The fixtures retain unrelated native/EVM traffic, exact failed direct calls and unknown Safe events, both Frontier hash-vector variants, explicit empty EVM vectors, native-body omissions/reordering, receipt/header/digest mismatches, parent gaps, canonical changes, ordinary advancing finalized heads, independent per-read deadlines, cancellation, unavailable archive methods, input bounds and private create-only publication. Native ordered-trie behavior is checked against the retained pinned Rust vectors. Adjacent coverage includes the original native receipt/mapping paths, server receipt collection/commitments and actual Safe orphan-authority/public-submit refusal.

A separate root executes the actual pinned 1.4.1 Safe proxy and singleton. It traces a real delegatecall, observes a transient Safe storage value before LOG0, and verifies that simulation rollback removes both the slot and log. The resulting exact receipt census keeps internal execution, deployment history and send authority unproven. Synthetic fixture transaction fields are not relabeled as live execution evidence.

All ten normal controls and five selected race controls reached their exact named assertion, selected root and package FAIL, exit 1, without a build/setup/panic/timeout/race confounder. Nine are production-barrier mutations; one is explicitly a fixture-oracle control. The independent author audit reread every raw stream and verified the exact mutation digests and restored source identity.

| Control | Normal | Selected race |
| --- | --- | --- |
| Complete native body root | Causal | Causal |
| Complete receipt root | Causal | Causal |
| Frontier transaction vector | Causal | Not selected |
| Retained canonical boundary | Causal | Causal |
| EVM parent continuity | Causal | Not selected |
| Independent per-read deadline | Causal | Causal |
| Create-only output publication | Causal | Causal |
| Interval admission bound | Causal | Not selected |
| Unresolved internal-history verdict | Causal | Not selected |
| Actual transient SSTORE fixture oracle | Causal | Not selected |

The fixture-oracle mutation replaces the synthetic delegate target's SSTORE with POP; the actual published Safe execution must fail the transient-storage assertion. The production history-verdict mutation reaches the same published-Safe root and rejects promotion of receipt bytes to complete internal history. The shared positive root runs in both modes; these two controls run normally only. Selected race controls cover the commitment, canonicality, deadline and durable publication boundaries.

## Sealed artifacts

The independent evidence is retained at `/home/by/urnetwork/temp/safe-history-validation-20260930/evidence`:

- [Manifest](/home/by/urnetwork/temp/safe-history-validation-20260930/evidence/manifest.json): `2c5f41292d3d3e98bf59153d198d79546115c16268080af269696c3c6266148d`.
- [41-file SHA256SUMS](/home/by/urnetwork/temp/safe-history-validation-20260930/evidence/SHA256SUMS): `1576ac8a486dc4c31a5f728718254c894c6cbeecb32364eb676d0ee1e198a9b6`; every entry verified.
- [Independent author audit](/home/by/urnetwork/temp/safe-history-author-audit-20260930/final-audit.json): `653d5bc70f705fa1a260e00e3624bfceff401cca3a7716c3e69cf13b67140e9c`.

The audit checks all eight positive streams and fifteen raw control streams, the original author pair and both independent positive/control pairs, exact module files/graphs and all fourteen replacement declarations across those three layouts, source/oracle file digests and all four test binaries/build-info records. This is 42 replacement-state checks, not 42 different dependencies. All source checkouts and replacements were clean and matched their sealed commits/trees.

The immutable author handoff is retained at `/home/by/urnetwork/temp/safe-history-handoff-20260930`:

- [Matrix](/home/by/urnetwork/temp/safe-history-handoff-20260930/matrix.json): `84cf91370e1b20bc13ffc4812f8ab8b8c7caac7c0c2a0b62f648faa7e87df11d`.
- [Source/dependency fence](/home/by/urnetwork/temp/safe-history-handoff-20260930/fence.json): `245f1599a5ea5dc83f855eee50614cba197861098e916ea8864b17b5c7ed9e6a`.
- [21-entry static seal](/home/by/urnetwork/temp/safe-history-handoff-20260930/STATIC-SHA256SUMS): `09de721048ae3bda520867901f86913f8a33a778d250e359444856d942bcd259`.
- [Control compile/vet supplement](/home/by/urnetwork/temp/safe-history-handoff-20260930/control-static/supplement.json): `d017ba5e98213d62af930ba4d240539a28c88b4a1ffc03c4fa53ec39a0d52b75`.
- [44-entry full handoff seal](/home/by/urnetwork/temp/safe-history-handoff-20260930/SHA256SUMS): `22af7dc7da86376e478bc57fdfc56075dd91db8bf29a59f93c28422277295a3e`.

The source/dependency fence includes exact module bytes and graphs, six SN and eight server local replacement declarations, and published Safe archive/profile hashes. The replacement declarations include the paired repositories and bundled npipe source; they are not fourteen independent external repositories. Neither module file changed.

## Remaining mainnet gates

MG-08 remains open. This command authenticates committed bytes and their positions; it does not establish full internal/reverted EVM execution, native-hook effects, source-to-deployed-runtime identity, clean initialization, owner/module mapping history or complete pending state. Finality and canonicality remain assertions of the selected owned RPC. It is not `bootstrapSuccessorSafeProvenanceAuthenticator`, cannot discharge the unchanged independently signed complete-history policy, and leaves public successor submission closed.

A qualified full execution-history adapter requires authenticated parent state, exact reviewed execution artifacts, native hooks and internal/reverted EVM traces, followed by final pending re-admission. No such production adapter is supplied. The separate current-storage policy proposal remains a distinct approval path and has no newly installed public route.

Snow remains operator-reported syncing/preparing for the intended mainnet route. The retained observations are still the earlier testnet ID 945 and later HTTP 502 responses; this qualification supplies no new live chain observation. Approved mainnet genesis, ID 964, runtime/source identity and working archive capabilities must be established before live use. Evidence anchor completion, Safe authority, funding and activation remain open.
