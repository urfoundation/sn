# Receipt admission candidate — qualification pending

**Subsequent result:** the corrected candidate and miner adjacency were
[qualified and integrated](receipt-recovery-qualification-20260928.md).
The candidate notes below retain the original handoff and failed first capture.

Date: 2026-09-28 UTC. Base source: `a7e0156849f0c8c96ce278400f83a69d2ff29d8a`.
Worktree: `/mnt/data/sn-testnet/worktrees/sn-mainnet-production-reconciliation-20260928`.
This candidate has not been tested or integrated. It is sealed for the requested
Terra medium test lane; Astra owns diagnosis and corrections. No live chain,
custody, signing, submission, release or process state was accessed.

The existing finalized-extrinsic scan decoded successful null/incomplete RPC
results into value structs and could call an absent body an absent transaction.
Checking header identity alone would still let a shortened explicit extrinsics
vector supply false absence. The candidate requires complete header fields,
reproduces the complete native SCALE header hash, checks its canonical finalized
height and consecutive scanned parent links, and verifies the full ordered
extrinsics trie commitment before accepting inclusion or absence. Direct
finalized-event verification uses the same block admission.

Known layout-0/1 commitments are recognized independently of runtime execution
approval. Actual events still require the caller's exact approved block runtime.
Modern digest tag 8 is decoded explicitly so a runtime-update header does not
inherit the older SDK's missing-variant failure. The owned RPC remains the
canonical finality authority; this does not add GRANDPA or event-storage proofs.
No receipt-prefix reuse or production retry-policy change is included yet.

Tests supply genuine fixture header/body commitments before source signing.
Legacy synthetic zero-root fixtures are updated accordingly. The original
finalized-source event canary now installs inclusion in the next block, avoiding
an impossible circular claim that a prepared transaction belongs to the block
whose hash it already signed. All fixture identities and keys are synthetic.

Required qualification (normal, then race for the first two selections):

```sh
export GOCACHE=/mnt/data/sn-testnet/gocache
export TMPDIR=/mnt/data/sn-testnet/qualification/production-reconciliation-20260928/tmp
export GOWORK=off
export GOMAXPROCS=2
go test ./crv4 -run '^Test(Receipt|LocateFinalizedExtrinsic|VerifyFinalizedExtrinsic|ExtrinsicIndex)' -count=1 -timeout=180s
go test -race ./crv4 -run '^Test(Receipt|LocateFinalizedExtrinsic|VerifyFinalizedExtrinsic|ExtrinsicIndex)' -count=1 -timeout=180s
go test ./validator -run '^Test(ProductionAuthorityHistory(Pending|Finality)|ReleaseEvidenceV2HistoricalRuntime|ReleaseProvisionalRuntime|OwnerRecycleMainnetPendingReplay|OwnerRecycleAdmissionPreservesOriginalReceiptRecovery)' -count=1 -timeout=600s
go test -race ./validator -run '^Test(ProductionAuthorityHistory(Pending|Finality)|ReleaseEvidenceV2HistoricalRuntime|ReleaseProvisionalRuntime|OwnerRecycleMainnetPendingReplay|OwnerRecycleAdmissionPreservesOriginalReceiptRecovery)' -count=1 -timeout=600s
go test ./validator -run '^Test(OwnerRecycle(Admission|Approval|Production)|AuthenticatePinnedNativeRuntimeValidatorStake|ProductionRuntime)' -count=1 -timeout=600s
go vet ./crv4 ./validator
git diff --check
```

The shared-fixture adjacency selection is normal-only unless a new failure
requires further race qualification. The preceding original-authority batch's
unmodified source is already qualified in
[its receipt](production-authority-history-qualification-20260928.md); its broad
archive and loop suites need not be repeated for this candidate.

Causal control: overlay the complete predecessor `a7e01568:crv4/chain.go` while
retaining new tests, then run `TestReceiptScanRejectsIncompleteBodyEvidence` in
normal and race modes. A named assertion showing incomplete body acceptance is
required; build errors, panics, timeouts or races do not count as reproduction.
All results remain pending until the exact candidate finishes qualification.
# Fixture correction awaiting qualification

The first frozen run at `9618a1cb` exposed a fixture serialization defect:
the pinned GSRPC `types.BlockNumber.MarshalJSON` emits unprefixed hexadecimal.
Substrate's [native header serializer and independent quantity tests](https://github.com/paritytech/polkadot-sdk/blob/master/substrate/primitives/runtime/src/generic/header.rs)
use `0x`-prefixed quantities. The source admission requirement remains unchanged.
Corrected fixtures explicitly emit the real wire spelling while retaining their
SDK-generated SCALE commitments. The malformed-body regression now also proves
it reaches `chain_getBlock`, so a prior finality rejection cannot satisfy it.

This correction changes test fixtures only. Normal/race qualification and the
original-source causal control must be repeated on the corrected source; no pass
is claimed here. The initial failed logs remain under
`/mnt/data/sn-testnet/qualification/production-reconciliation-20260928/`.
