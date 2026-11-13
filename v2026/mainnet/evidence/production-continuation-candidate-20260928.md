# Production continuation candidate — 2026-09-28

Status: **component qualification complete** on frozen author
`18ff77cb33a7fc91c49e5d03040c3db382e23ff1`, following core `89ebd498`.
Root integrated the core as `c8330bfd` and its receiver-only correction as
`459510af`; the later native HTTP integration is `05b88228`. No mainnet keys,
submission, deployment, active simulator or release lock was changed.
Astra authored the source and deterministic fixtures; Terra executed the bodies.
Full public startup and durable partial-scan recovery remain open below.

## Concrete failures addressed

1. `ReleaseSteerer.Run` asked for fresh signing/runtime/scheduler state before
   entering `SubmitOnce`; `submitOnceV2` asked for fresh EVM state before reopening
   retained work. Either could strand a valid pending original signature.
2. Production lacked the provisional loop's deferrals. Pure read exhaustion or
   missing receipt evidence could spend the fatal budget or let a new native
   epoch turn unfinished work into a startup failure.
3. Receipt search returned only found/not-found. Comparing that older negative
   scan with a newer finalized nonce could misclassify our own intervening
   inclusion as a foreign transaction.
4. A returned finalized head behind an already retained receipt is unavailable
   evidence, distinct from a successfully returned contradictory canonical hash.
5. The existing geth HTTP status classifier omitted ordinary HTTP 500 although
   the artifact HTTP reader already recognized all 5xx statuses.

## Source behavior

The production outer loop invokes the existing public `SubmitOnce` owner before
fresh scheduling. Compact submission first reopens the actual authenticated V2
intent. Pending receipts and finalized application rows are reconciled before
unrelated current decision reads. No production startup fence is weakened.

Read owners use 300-second total / 60-second attempt budgets and interruptible
delays. Pure typed transport/missing-result exhaustion is an observable wait;
malformed data, permanent application failures, local custody, real contradictory
identities, mixed errors and service cancellation retain their separate outcomes.
The real owner emits native/steering progress after classified operations. Store
begin/update/current observations use the qualified after-close/after-release
hooks. Waits do not clear a previously observed hard defect.

`FinalizedExtrinsicScan` retains a private witness for the exact complete-body
coverage boundary. Production reads canonical nonce and native epoch at that
same block. If finality advances before exact-byte rebroadcast admission, the
next poll extends the scan first. A newer head cannot retroactively widen absence.
Rebroadcast also retains the original deposit-audit publication/EMA duties.

Native prepared transactions currently have an **immortal era**. Passing an
epoch or local approval interval cannot revoke signed bytes. Later unresolved
epochs remain missed/pending; no replacement signature or manufactured native
success is authorized. Receipt or authenticated foreign nonce use resolves the
old liability. Original config-history authority stays distinct from current
signing authority. Existing historical-only renewal grants remain observation-only.
That historical-only branch currently returns pending after complete receipt
absence, before nonce reconciliation: foreign-nonce resolution under a renewed
historical grant remains open. Current-authority pending work uses the exact
same-boundary nonce proof implemented in this candidate.

## Qualified scope

All commands use `GOWORK=off GOMAXPROCS=2`, physical
`GOCACHE=/mnt/data/sn-testnet/gocache`, and a capture-specific physical `TMPDIR`.
These selectors describe frozen `18ff77cb`; use its retained exact root lists
when reproducing the scope, rather than counting later additions as old evidence.

- `go test ./crv4 -run '^Test(ReceiptScan|ReceiptBlock|ReceiptHeader|LocateFinalizedExtrinsic)' -count=1`
- `go test ./validator -run '^Test(ProductionContinuation|ProductionSteering|ProductionAuthorityHistoryPending|ProductionAuthorityHistoryReceipt|ReleaseSourceFinalityRead)' -count=1`
- The same affected selectors with `-race`, plus `go vet ./crv4 ./validator`.
- Enumerate the matching roots before execution; preserve each source HEAD,
  physical path, raw JSON log, exit code and source fence. Adjust timeout to the
  real M8/full-replay workload without lowering policy thresholds.

The nonempty fixture retains actual Stats detach journals, signed compact M8
records, deposit/binding replay, exact production sidecar, cryptographic native
preparation, envelope bytes, real private intent begin/current/update and reopen.
Its synthetic zero-price policy is independently selected before signing. It
does **not** qualify paid captures, 10% economic acceptance or live Yuma outcomes.
The lost-ack adapter records one actual original `SubmitPrepared` subscription;
later inclusion comes from complete canonical block bodies and original metadata
events, not an injected receipt verdict.

Required assertions include lost acknowledgement → read outage → native epoch
advance → fresh owner → original receipt → exact applied row; no new signature,
one broadcast, unchanged original age; intervening self-inclusion between scan
head and newer advertised head; real closed-descriptor error joined with a
timeout staying hard; behind-node recovery versus canonical contradiction; and
actual configured EVM HTTP 500/502 recovery.

The actual `Run` test proves routing to the startup owner before fresh scheduling.
It deliberately retains the startup refusal because this fixture does not model
full activation publication, normalized config and dual authenticated upload
sessions. Separate loop callback controls qualify classification only and never
count as restart/inclusion evidence.

## Causal controls

Freeze source before Terra runs. In a separate overlay, restore the prior
fresh-runtime/EVM-before-intent order: the retained receipt test must fail at its
named fresh-runtime/continuation assertion, not compile or setup. Restore nonce
selection from the newer advertised head: the intervening self-inclusion test
must reject that false foreign-nonce result. Restore the old production `Run`
scheduler branch: the actual routing test must observe the forbidden fresh
runtime read. Restore the behind-receipt hard error and HTTP500 omission for their
two focused controls. Keep successful full test bodies unchanged while correcting
an independent failed fixture.

## Completed qualification

The completed qualification is retained at
`/mnt/data/sn-testnet/qualification/production-continuation-20260928/style-18ff77cb`.
The source remained at clean `18ff77cb` before and after; the resolved module
graph was byte-identical. Vet passed for `crv4` and `validator`.

| Frozen scope | Normal | Race |
| --- | --- | --- |
| CRv4, 10 roots | 10 passed, 0.017 s | 10 passed, 1.116 s |
| Validator, 22 roots | 22 passed, 120.755 s | 16 passed before the original 600.190 s package timeout; only the interrupted root and five unstarted roots reran, all six passing in 182.462 s |
| Six decision-control families | Six intended assertion failures | Six intended assertion failures |

Race coverage reaches all 22 validator roots across the two retained captures.
The original package timeout remains an exit-1 failure, not a passing full
invocation. The continuation selectors initially used an erroneous terminal `$`
on family prefixes; enumeration caught the zero-root selection before bodies.
The corrected prefixes enumerated exactly 10 and 22 roots. No positive result
depends on a zero-root invocation, timeout or failed fixture setup.

| Retained file | SHA-256 |
| --- | --- |
| `crv4-normal.jsonl` | `e2024a325533cdc898ab6eef6480bd85bf9588b0f399da119d8bc2b2caf6aea1` |
| `crv4-race.jsonl` | `7c8a6b9c0ac7f1d24a0b04855007b5bd33c48a5f4ef3fe2d006ca69639e13035` |
| `validator-normal.jsonl` | `8b0976a3c52f57861941420dcf0535ae89dd4cb36417ae014c98f8bec8842818` |
| Original `validator-race.jsonl` | `ed0c9c8c3f472fc4758d581ec4428cd17ab6d4e83b0560fc0167ec12f09c1cfb` |
| `validator-race-resume.jsonl` | `5d20402d698962fb3858c2c83acb57d64424be4a595ed82ef1385379b6780b57` |
| Resolved module graph | `85d5eef5512f1cd3f4f9d0d89fa77f602d80b5fe2bfd7d1339d80a6c0b41a765` |
| `causal/controls.tsv` | `96ff62a99b4ce2fa70737439d83f2669c9de546c1176acdf050493f75ae74306` |
| `causal/summary.txt` | `8e6975123a17a310b3f6ec76a2b065857d649cd030b62ccd73d6faedddd51684` |

The later [native HTTP composition](production-native-http-integration-20260928.md)
has its own affected qualification. At integrated `05b88228`, all Go source in
`crv4`, `validator` and `protocol` matches its qualified author `a0249f6a`
byte-for-byte. Integration conflicts were confined to documentation, where
newer source findings and qualification results were preserved. This source
comparison does not claim a complete mainnet release/dependency attestation.

## Fixture diagnostics retained

Terra's early single-root diagnostics are under
`/mnt/data/sn-testnet/qualification/production-continuation-owner-20260928`.
They exposed incomplete zero-price audit identity/deadline, a standalone seal's
generation differing from the real Stats cursor, a nonzero committer in an absent
source slot, and re-signing a randomized envelope after its hash was pinned.
Corrections retained the real physical cursor, entirely zero absent ABI tuple
and exact first signed envelope bytes. No acceptance rule was relaxed.
The corrected single-root `TestProductionContinuationDurableIntentOwner` passed
normally at fixture correction `28380c5f` (Terra, 18.089 seconds; isolated
cherry-pick `1d4c73eb`, `corrected-28380/normal.jsonl`). This is the fixture's
begin/current/update/restart result only; the completed continuation and race
scopes are recorded separately above.

## Explicit remaining work

- Full actual `RunRelease` fresh activation, normalized config, dual-upload and
  durable continuation composition is still required.
- Persist authenticated bounded receipt prefixes with a semantic proof version
  and original attempt binding. Current scan retains the terminal boundary only;
  a later failed read can still require prefix replay. Avoid per-block full
  journal writes and remove duplicate authenticated body/header reads where safe.
- Resolve the historical-only renewal branch's foreign nonce outcome without
  granting it fresh signing authority. Its present receipt-only wait is explicit.
- Miner recovery also needs bounded subrange checkpoints within its 4096-block
  range so a late outage cannot repeatedly discard thousands of successful reads.
- Configured native HTTP status/EOF propagation and response/close bounds are
  qualified in the separate integration linked above. Composition of independent
  native/EVM failure branches in public startup remains a later affected scope.
- Automatic compatible runtime policy, skipped-epoch authority and real mainnet
  inputs/economic observations are not supplied by this patch.

The header fixture inventory is retained at
`/mnt/data/sn-testnet/qualification/production-continuation-20260928/header-fixture-inventory.txt`.
Complete-receipt consumers use the genuine full header/hash/wire helper. Remaining
SDK/number-only emitters in mainnet-runtime-history, production-runtime-history,
upload-authority, native identity/account/context and miner claim/capability tests
currently exercise different readers; deliberate malformed cases remain intact.
When those paths adopt complete receipt admission, update their independent
canonical fixture chain rather than weakening the production wire decoder.
