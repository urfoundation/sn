# Preserved progress after managed-readiness failure

The [failed strict resume](../FINAL-2-managed-readiness-20260916/README.md)
finished at 2026-09-16 10:20:25 UTC with body, outer and join exits **1**.
This package preserves its closed recovery census, root's subsequent
original-byte restoration, and the validator-startup diagnosis. It does not
claim a successful startup or final campaign acceptance.

During startup, the operators submitted **14 new transactions**, all successful
and canonical below finalized native/EVM block **8017664**. Operator 1 used
nonces 107–114; operator 2 used 107–112. The
[canonical reconciliation](census/CANONICAL-RECONCILIATION.json) records resulting
finalized/latest/pending nonces 115 and 113, with the other 11 roles unchanged.
[Decoded methods and events](census/DECODED-METHODS-EVENTS.json) identify six
`deferMissedEmission` calls and eight `finalizeOperatorEpoch` calls. These
produce deferred/missed-root finalization events; **no payout root was created**.
Their complete hash/intent bindings and contract-source digests are retained.

The [complete census](census/README.md) now covers 244 operator signed attempts,
including all 230 earlier attempts and their canceled, replaced and nonterminal
siblings. Maximum new signed gas exposure is 90,515,040,996,642,416 wei; actual
finalized fees are 29,033,073,172,513,564 wei. These are liabilities already
incurred during the failed startup, not new funding or expanded approval.

Neither validator created a new steering decision or preparation for N1488.
Validator 1's approved intent prefix remains empty; validator 2 retains its
exact three-intent prefix ending at N1405. Session credentials and ledger
storage were reopened/refreshed, so this is not a claim that every runtime file
was unchanged. All 1,000 claim queues were examined: 283,272 entries were
updated, but none gained signed bytes, a transaction hash or a submission
attempt. The four older signed claim entries remain. There is no complete
pre-start queue-byte baseline; [the census](census/CLAIM-QUEUE-CENSUS.json)
explicitly limits that conclusion to durable fields and timestamps.

Root's separate restoration session **44035** ran
**10:35:51.755191–10:35:53.574936 UTC**, with
[body](restoration/body.exit) and [joined](restoration/join.exit) exits **0**.
It exclusively created **14 files of 178 bytes** using the exact original DB
signatures already tied to canonical successful receipts. The
[result](restoration/RESULT.json) records **zero chain transactions**, zero
new signatures, and no database or journal mutation. All **2,272** existing
transaction files and all **six** watched files retained their hashes; the
store now contains **2,286** files. [Independent manifest comparison](VERIFICATION.json)
confirms that exactly the approved 14 names/digests were added.

The [complete evidence union](census/COMPLETE-UNION-DELTA.json), including
unchanged renewal and claim sources, grows from **2,518** to **2,532** unique
transactions. Its restored digest is
`sha256:f07fe41eb13cd653c0cd9bba83012a7bcf9084d47819d0b9ccfd12d12c6d5773`.
The saved plan, original identities, completed preparation, both renewals and
approval limits remain retained: 205 EVM TAO / 225 total TAO, 37,250 lifetime
alpha and 6,000 alpha per repair. The census was sealed before restoration;
its original text correctly describes restoration as a subsequent step.

The [closed cause note](cause/README.md) and exact safe fresh stderr slices
show both validators still authenticating retained semantic history when the
five-minute LAN startup wait cancelled them. The gate had no blocking findings
through its last scan; the slices include the subsequent cancellation tail.
At source `aeda6abbd2dc0abc92bb0f60975cf89b509e8017`,
[validator startup](../../../../validator/release_run.go) performs that replay
before starting proof workers, while the simulator's
[startup timeout selector](../../../process.go) distinguished RPC routes.
The proposed bounded correction and its qualification are separate evidence;
this package does not assert replay completes within thirty minutes.

## Exact provenance and omissions

All **63 copied originals** are byte-identical to their closed local sources;
[SOURCE-FILES.tsv](SOURCE-FILES.tsv) records paths, hashes, sizes and original
file modes. Three complete original payload manifests and seals are preserved:

| Original group | Payloads | Manifest SHA256 |
| --- | ---: | --- |
| [Recovery census](census/CENSUS-MANIFEST.sha256) | 35 | `b9070c8ee744b2cfc54d8d24ba8baa38144a3f4aadd1306300b1edc0e5c9da18` |
| [Original-byte restoration](restoration/SHA256SUMS) | 16 | `de3e77ec83fa8b3abc929492a33c23c97f2e8d3896a97c951c4db340ed9f1c95` |
| [Startup cause](cause/SHA256SUMS) | 6 | `e63178a2240adc3033f771c5f34f20b44bb55f1c2d3fa5c691ab95275bdab0cc` |

The [17 private references](OMITTED-PRIVATE-REFERENCES.json) identify the
omitted original signatures, private export rows and credential-related file
metadata by hash only. Raw RPC responses were not persisted by the census;
their exact hashes and lengths remain in the two RPC receipt JSON files.
Included safe projections omit transaction calldata/signature fields and
native consensus digest. Full DB/config contents, secret material, large
plans, private RLP payloads, and active native captures are excluded.

The exact validator slices contain only empty environment settings, public
local artifact locators and cancellation errors. Their original inode and
initial/gate/EOF boundaries remain in
[CURSOR-SLICE-SUMMARY.tsv](cause/CURSOR-SLICE-SUMMARY.tsv). Larger private logs
are excluded. Manifests named `PRIVATE-ARTIFACTS.sha256`, `CANDIDATES.sha256`,
`inputs.sha256`, `store.*.sha256`, `state.*.sha256` and
`GENERATION-SOURCE-SHA256SUMS` preserve original references; they do not claim
those private or absolute-path payloads are present in this package.

Original SQL and the already-executed restoration script are inert provenance,
not commands to rerun. No RPC, native command, test, build, transaction or
runtime-state mutation was performed while packaging. The top-level
`SHA256SUMS` covers every payload except itself and its own seal; each original
group can also be verified from its respective subdirectory.
