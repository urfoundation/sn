# Source-finality read candidate — qualification pending

The [subsequent qualification receipt](source-read-cause-qualification-20260928.md)
records completed tests, the corrected predecessor control and integration.
The original author handoff below retains its earlier status.

Date: 2026-09-28 UTC. Worktree:
`/mnt/data/sn-testnet/worktrees/sn-mainnet-production-read-waits-20260928`.
Base `6720f152` contains the qualified receipt/miner prerequisites `9618a1cb`,
`ed412c6c` and `de034a4b` (the latter two are local cherry-picks). This batch is
source, deterministic fixtures and documentation only. Astra authored it;
Terra medium qualification is pending. No live chain, keys, retained run state,
signing, submission, release or deployment was used.

The source-finality reader previously joined a header/canonical-read timeout
with an assertion that the receipt was not finalized/canonical, before the
read had returned evidence. The strict retry classifier correctly rejected
that mixed error. The adjacent prepared-source schedule/control readers and
absent-slot `LastCommitment` reader had the same mistake. These paths now preserve the actual read error separately
from a successfully observed contradiction.

The source verifier also fetched its body and event vector a second time,
after dispatch proof had already authenticated the complete body. It now
reuses a private result containing the authenticated block number, extrinsic
index and decoded events. Source event matching and dispatch success consume
one exact body and one event read. Every new invocation still reads and proves
its evidence afresh; this is not a cross-call cache or new signing authority.
The original runtime, signature, event order/ciphertext, historical commitment
readback and actual dispatch failure checks stay required.

Missing finalized hashes, headers, blocks, required fields or event storage
produce `ReceiptEvidenceUnavailableError` at the physical reader. An unknown
outcome never proves absence. Explicit empty/truncated vectors that contradict
their committed body, malformed values, wrong identities, missing success in
an actual decoded event vector, dispatch failures and known pruned RPC errors
remain separate hard errors. Event storage has a finite 16 MiB decode bound.
Typed missing evidence is not yet wired into the production phase-wait loop;
that owner is the next PH-03 slice.

New tests use a real original-runtime signed source batch, real metadata and
SCALE event encoding, genuine header/body commitments and physical RPC fault
injection. They verify read recovery with unchanged signed bytes, no submission,
single-body/event reuse, missing-data distinctions, hard and joined-error
controls, cancellation, adjacent preparation reads, and absent-slot timeout →
recovered complete absence → actual occupied-slot refusal. The existing actual
V2 reconcile test remains stopped by its named publication fault; it does not
claim a successful nonempty durable V2 store transition.

Required qualification uses a physical task `TMPDIR`,
`GOCACHE=/mnt/data/sn-testnet/gocache`, `GOWORK=off`, `GOMAXPROCS=2`:

```sh
go test ./crv4 -run '^Test(Receipt|VerifyFinalizedExtrinsicContext|LocateFinalizedExtrinsic|SourceCommitment|SourceRuntimeCapability)' -count=1 -timeout=300s
go test -race ./crv4 -run '^Test(Receipt|VerifyFinalizedExtrinsicContext|LocateFinalizedExtrinsic|SourceCommitment|SourceRuntimeCapability)' -count=1 -timeout=600s
go test ./validator -run '^Test(ReleaseSource|ReleaseEvidenceV2HistoricalRuntime)' -count=1 -timeout=600s
go test -race ./validator -run '^Test(ReleaseSource|ReleaseEvidenceV2HistoricalRuntime)' -count=1 -timeout=900s
go test ./miner -run '^TestFleet(MainnetNative|RecoveryNative|RecoveryUpgradeAtReceipt)' -count=1 -timeout=300s
go test -race ./miner -run '^TestFleet(MainnetNative|RecoveryNative|RecoveryUpgradeAtReceipt)' -count=1 -timeout=600s
go vet ./crv4 ./validator ./miner
git diff --check
```

The miner selection covers the shared changed dispatch/event reader and native
recovery while reusing the broader 63-root miner normal/race qualification on
the unchanged predecessor. The validator selection includes actual original
source/reconcile callers in addition to the new physical-read controls. No
monitor candidate is imported or claimed qualified by this batch.

Causal control: overlay only `6720f152:crv4/source_commitment.go`, retaining the
new private receipt helper, exact wire fixtures and new tests. Normal and race
runs of `^TestReleaseSource(FinalityReadPreservesTransportCause|FinalityReadUsesOneAdmittedBodyAndEvents|PreparationReadPreservesTransportCause|PreparationControlReadPreservesTransportCause|SlotReadPreservesTransportCause)$`
must fail the named timeout-as-contradiction or repeated-admitted-read assertion.
Build errors, setup failure, panic, race report or timeout are not a valid causal
reproduction. All candidate and predecessor-control results remain pending.

Remaining PH-03 work is explicit: retained-intent observation before unrelated
current snapshots; production phase-specific read waits across epoch changes;
real nonempty durable V2 begin/update/restart coverage; authenticated bounded
receipt-prefix reuse; and nonce/expiry observation at the exact scan terminal.
The miner also needs bounded subrange persistence so a late timeout does not
discard thousands of admitted reads. No missing epoch is backdated or marked
successful, no uncertain original signature is replaced, and no approval,
policy, custody or economic condition is waived.
