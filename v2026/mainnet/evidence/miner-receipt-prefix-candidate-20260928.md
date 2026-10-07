# Miner receipt-prefix candidate — qualification pending

**Subsequent result:** [normal/race qualification and integration passed](receipt-recovery-qualification-20260928.md).
The original source handoff below is retained for reproduction.

Date: 2026-09-28 UTC. Base: corrected receipt source `ed412c6c`, following
`9618a1cb`. Worktree:
`/mnt/data/sn-testnet/worktrees/sn-miner-authenticated-receipt-prefix-20260928`.
No live chain, signer, process, deployment or retained production state was used.
This is a source candidate for Terra medium; Astra authored it and has not claimed
test success. Formatting and `git diff --check` pass.

The miner's native recovery loop accepted an explicit empty/truncated body as
absence and persisted `ScanNumber` beyond an unobserved original transaction.
It now uses the shared `crv4` header and complete-body admission; production trie
logic is not copied. Header hash/number, canonical finality and ancestry are
checked before a signed absence checkpoint. All entries must match the ordered
extrinsics commitment before membership is searched. Actual runtime/event and
postcondition authority remain unchanged. Read failures are returned before
adding any successful-observation contradiction; unresolved state retains the
original error and transaction bytes.

The new `scan_proof` field is covered by the existing record and inventory
signatures. Its exact semantic version is
`urnetwork-native-receipt-absence-v1`. Empty legacy values keep their old signature
bytes and never authorize prefix reuse. One rescan may replace the old unqualified
cursor with a smaller qualified one. A recognized proof cannot be rolled back,
downgraded or transferred to another signed attempt. Finalized outcomes are not
reopened. This migration is independent of binaries and current runtime versions.

The shared miner fixture now supplies real independently encoded header/body
commitments. Submitted bytes only change future receipt blocks; the prepared
block remains fixed. New tests run real commands, custody signatures, fsync,
close/reopen, receipt/event decoding and postconditions. They cover truncated
empty bodies, null/omitted fields, corrupt trailing entries, wrong headers,
semantic migration, genuine prefix reuse, and a native runtime-update digest
`0x08` in the scanned prefix. No admission-verdict callback is introduced.

Required qualification, under a physical task TMPDIR and the shared physical
`GOCACHE=/mnt/data/sn-testnet/gocache`, `GOWORK=off`, `GOMAXPROCS=2`:

```sh
go test ./crv4 -run '^TestReceipt' -count=1 -timeout=180s
go test -race ./crv4 -run '^TestReceipt' -count=1 -timeout=180s
go test ./miner -run '^TestFleet(Mainnet|Recovery)' -count=1 -timeout=600s
go test -race ./miner -run '^TestFleet(Mainnet|Recovery)' -count=1 -timeout=600s
go vet ./crv4 ./miner
git diff --check
```

The miner selector includes native/EVM command, shared fixture, inventory,
signature, crash/restart and runtime authority controls. Corrected receipt
validator normal/race/adjacency and vet were independently completed on
`ed412c6c`; unchanged validator source need not be repeated for this miner slice.

Causal control: overlay `ed412c6c:miner/fleet_recovery_native.go` while retaining
the corrected genuine-wire fixtures, new shared API and new store/tests. Run
`TestFleetRecoveryNativeTruncatedBodyNeverAdvancesCheckpoint` normal and race.
Require the named assertion that a truncated body advanced/replaced the original
durable attempt; build errors, unexpected setup errors, panics, races and timeouts
are not valid causal reproduction. The full original-body control and all
candidate results remain pending.

PH-03 production steering waits and validator retained receipt-prefix reuse are
separate unfinished work. This miner change grants no new signing authority,
automatic runtime admission, skipped-epoch success or live launch approval.
