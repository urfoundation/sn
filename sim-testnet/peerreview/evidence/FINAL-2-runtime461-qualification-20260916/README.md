# Runtime 461 qualification

The correction is qualified at `8edb3167a6261bfd82ecbc5f3c0ac2c787beec7c`.
All **103 affected roots pass normally and under race detection**: 15 CRv4,
10 miner, 34 validator and 44 simulator. Four separate causal restorations
produce exactly **nine expected failures and nine passing controls**. No test
was skipped. All 15 compiler invocations exited zero; the eight positive bodies
and their wrappers/joins exited zero. The seven causal bodies and their
wrappers/joins exited one, as expected.

[Root's raw-event review](ROOT-RUNTIME461-QUALIFICATION-REVIEW.json) verifies each
selected root ran once, exact compiled membership, terminal package outcomes,
before/after source/dependency/selector/binary comparisons and current binary
hashes. The [final selection](source/affected-test-selection.final.json) and
[causal definitions](source/CAUSAL-HANDOFF.json) define the scope independently
of the results. All raw captures are under `terra/positive` and `terra/causal`.
This verifies the affected correction, not the live campaign or all release
gates. Earlier unaffected gate results remain retained.

The correction admits the exact on-chain 461 artifact, checks the consumed
native encoding and stake layouts, preserves the original runtime-455 config
identity, and retains historical runtime-460 companion approvals. Regressions
exercise actual 461 metadata, signature-domain separation, current versus
historical authority, persisted approval bytes, budgets and adjacent invalid
inputs. The source is based on published `1860261`; formatter-only descendant
`b99ec97b` precedes the final one-line doctor fixture correction. That stale
positive fixture was caught before compilation, so no failing test was omitted.

The [exact-Wasm probe](offline-probe/RESULT.json) passed with the retained SDK,
matching the code and metadata identity and sizes from the LAN observation.
Probe, wrapper and actual joined exits are zero. Root separately verified its
36 sealed payloads and unchanged inputs. The [source review](source/READ-ONLY-REVIEW.md)
and [provenance](source-evidence/SOURCE-PROVENANCE.json) distinguish upstream
root-basket changes from the non-root interfaces consumed by this subnet.
[Root verified](ROOT-SOURCE-MANIFEST-REVIEW.json) all 94 upstream source witnesses
and the three new static metadata-source rows against the captured exact source.
Fresh chain evidence uses `192.168.1.162:9944`, `independent_rpc=false`.

The candidate renderer CLI built successfully at the qualified source, with
SHA-256 `6f89a919b4e092b98c0f416854c6baf46a4aca1f3b454b4aaee8f3ddf27d3e97`.
The subsequent read-only [release-lock preview](release-lock-preview/ROOT-REVIEW.json)
passed and changed only the observed protocol and SN Go source hashes. All
watched deployment state, executable, source and input lock bytes remained
unchanged during rendering. Candidate lock SHA-256 is
`aad35e8488e48190071889b3dec47c184ed9d2deedc30c44e3b52ee6f17afd84`.
The protocol hash covers runtime source manifests and enforcement scripts.
This CLI is a renderer; publication still requires a matching final executable.

The setup failure that prompted this work is preserved in the
[preparation bundle](../FINAL-2-runtime461-preparation-20260916/README.md).
No native setup, continuation import, managed startup or campaign pass is
claimed by this qualification bundle. No new funding or renewal was performed.

[SOURCE-FILES.json](SOURCE-FILES.json) identifies every byte-identical copied
receipt. Binaries and temporary compiler directories are excluded; their
hashes, build metadata, commands and actual exits are retained. Raw command
files are inert evidence and must not be rerun from this package. SHA256SUMS
covers every payload except itself and its seal.
