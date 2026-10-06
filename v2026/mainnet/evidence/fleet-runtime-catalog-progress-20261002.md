# Fleet runtime catalog qualification

Frozen candidate `119717d69ecc81858d1c92fa532db7f4488f14d5`, tree
`c040540908eaa0eebe5a7db795e4ff18b8706c25`, adds purpose-scoped selection
of exact reviewed runtime artifacts for fleet reads, historical replay and write
admission. Historical execution-parent decoding and inclusion post-state remain
separate. A compatible current read cannot authorize a write under an old
signing domain; retained recovery authority and signed bytes are preserved.
Public status and event callbacks retain caller cancellation.

The [author receipt](fleet-runtime-catalog-author-20261002.json), SHA-256
`76b3af304f18c56a6c86ba74f649da4c779fef7c0aa2b939cc9be725bff9ce95`,
records 23 normal and 23 race tests, three package vets, and six expected causal
failures per mode. Fourteen new tests join nine legacy public read/sign/recovery
controls. Root rehashed all117 bound files. The original two parser/cancellation
failures and the seal-only filename setup failure remain retained.

This scope uses synthetic runtimes8001/8002, exact Server10a/Connect0a5/SDK5d37
and a physically checked636-module graph. Unknown semantic purposes/artifacts
remain refused. No live signature, broadcast, source rebuild or mainnet runtime
approval is performed. Independent qualification is queued. Root's clean merge
preview preserves current main; this source is not merged or deployed.

The observed mainnet472 artifact remains unapproved. The earlier470 exception
is planning-only and supplies no472 authority. Broader current-module/release
composition and all remaining PH/MG work remain open.


## Independent qualification and main integration

Main merge `9671f4568b92e168d1db86d539d52b00f361a309` now integrates exact twelve fleet/chain/crv4 files
from119. Module manifests also match119; current finite-claim read recovery
and retained-member/spool fixes are preserved. The [independent receipt](fleet-runtime-catalog-independent-20261002.json),
SHA-256 `3d7b9bafdf52759d4de3081010b266454ba276c4c13e2dbe9196ec9d7e50ed91`,
records23 normal/race tests, three package vets and six intended causal failures
in each mode. Root rehashed its manifest and all55 bound files before merging.

The exact636-module test graph remains Server10a/Connect0a5. This main merge
does not inherit a complete composed release verdict; current Server/module and
all remaining required source changes still need the final composition gate.
No runtime472 artifact approval, signature, deployment or activation is implied.
