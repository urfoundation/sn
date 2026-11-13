# Current dependency graph and guarded-reader progress

This is scoped source evidence, not release or deployment authorization.

Server `22e3c1ba438b7888dccfc04244bcafaae0bcad3d` / tree
`b6cb4a0dd35d22a909e17c599e6995fd873c3aa2` independently passes
12 selected local-blob tests normally and with race detection, plus vet.
The actual declared graph uses published Connect `e0d75562`, SCTP `6443417d`
and archived SN `69f4bbdd`. Root rehashed all 26 independent manifest bindings.
The [independent receipt](current-server-module-independent-20261002.json)
has SHA-256 `121dcefd3b72d92466c4db3f4028a73adf826eed987723679645e04b6ce37ff2`.
The earlier `cebf154f` graph failure remains retained; tests never ran on it.

Server upstream subsequently advanced to `87712b3b` in 28 other files.
Review composition `32196d57ab5253691f38fbc119991097b013df05` / tree
`5853f647b0ff8eefad8715c0dcb2cdf1d6eee7d7` preserves those changes and the
exact eight blob/module files from `22e3c1ba`. Its current-graph qualification
is pending. Neither an upstream merge nor unchanged selected files inherit
qualification of the complete composition. Server main has not adopted it.

Guarded spool reader `68a7be85502ed7a0fd139afcd2f212178cdec019` passes
15 author normal/15 race roots and validator vet. Root rehashed all 40
[author receipt](guarded-reader-author-20261002.json) bindings; its SHA-256 is
`1feef2e3d7b5299f737538369de81760339b24e51a2b3a21e20d5b01ce15b814`.
The earlier `261efc9f` correction preserved bare EOF but allowed a full buffer
count with a failed custody guard. ReadFull and JSON consumers could accept
that count despite the error. Four causal controls reproduce this on `261efc9f`
in each mode; four partial-read and production-descriptor positive controls
pass. The corrective reader returns zero admitted bytes when its post-read
custody guard fails, preserving the actual advanced descriptor position.
Independent qualification is queued. This source is not merged into SN main.
The production descriptor reader already checks the read error before decoding;
the consumer probe does not establish existing production corruption.

The current SN retained-member composition `5f1fe123` is independently testing
ten observer, preparation, retained-member and execution seams on its exact
qualified Server `10a8f4d8` / Connect `0a5cda0e` graph. It is separate from the
new published-module intake above. Full PH/MG acceptance remains open.


## Adjacent Server and preparation reader successors

Server `2c4e5dca72fff0ba505119ff59dfa2a0ac88c04e` / tree
`749eaf1778d6eb28c231cb89b16862540ef4edf9` is a separate successor of
current composition `32196d57`. Its actual public Get and capacity readers now
withhold bytes after a failed post-read guard and retain both EOF and cancellation
causes where both occurred. Author17 normal/17 race roots and vet pass. Seven
causal assertions fail on old321 read bodies in each mode; six positives pass.
Existing io.Copy publication paths already refuse these errors, so successful
store corruption is not asserted. Root verified all45 bindings of the
[author receipt](server-guarded-reader-author-20261002.json), SHA-256
`c2753adc14c964bbd895b7c1b14b898895c4804b7c1fe85813fe665dcce74715`.
Actual published Connecte0d/SCTP644 and local SN69 dependencies remain pinned.
Independent actual-current-graph qualification is queued; neither321 nor2c4
is promoted into Server main.

Preparation reader `f5c0707bed5374c976c31de54573c9c1501085a6` / tree
`073d31a965a05999a3d43851357503223e1a040e` separately corrects Read and ReadAt
admitted counts after cancellation. Four actual ReadFull/JSON/ReadAt controls
fail on f4 in both modes. Eleven affected validator roots and seven actual CLI
roots pass normal/race; three package vets pass. Root verified all29 bindings
of the [author receipt](fresh-ledger-reader-author-20261002.json), SHA-256
`554b2306bd98261859e73c10268e45316cda589bc196f9c4b53c61709c41f5c2`.
Unchanged Connectea827/core73 stays bound through its original sealed receipt.
Independent replay is pending. Native/snapshot public-entry controls now run
on the unchanged f5 baseline before their adapters are implemented; this still
does not close private-root creation, retained/restore or the full storage gate.


## Guarded spool reader integration completed

SN main merge `4cda804c5e23d3ae6f48971534108a51ffed52c0` integrates the three guarded-reader files from
`68a7be85502ed7a0fd139afcd2f212178cdec019`. Those files and the module manifests are byte-identical to the
independently qualified source; the newer retained-member mainnet changes are
preserved. Root verified the independent receipt, its manifest and all 22 bound
files before merging. The [independent receipt](guarded-reader-independent-20261002.json)
has SHA-256 `09b35b8d5c40725aefd57510c3e1f5854769730e0f05e472f70130ed318eb554`.
All 15 affected roots pass normally and with race detection; vet passes.
Four old-body controls fail for the expected complete-buffer admission defect
in each mode, while four positive controls pass. This qualifies the affected
reader scope, not the complete mainnet release or deployment.


## Server guarded-reader integration completed

Server main merge `1d72f577c083f402f7d61ca546d3d6067a4c2699` integrates
the nine qualified blob/module files from frozen `2c4e5dca`, preserving upstream
`d150e2f5`. Root verified the [independent receipt](server-guarded-reader-independent-20261002.json),
SHA-256 `403a7d8cd9c68f29c83a3e231452291b215c9ce13cdfda1bd9fcc9edf59e2c8d`,
and all30 manifest bindings. The17 reader roots plus nine disjoint public blob
consumers pass in both modes:26 unique affected roots, no failures/skips; vet0.
Seven old-body controls fail and six positives pass in each mode.

[Integration source evidence](server-reader-main-integration-20261002.json)
binds all nine changed files. Every other upstream tree entry is preserved.
The receipt qualifies frozen2c with published Connecte0d/SCTP644 and localSN69;
it does not claim the newer model/monitor/main graph or complete production
release is qualified. Final module/release composition remains required.
No live effect is authorized.
