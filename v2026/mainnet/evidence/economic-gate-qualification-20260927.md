# Owner-recycle gate source qualification — 2026-09-27

The signer-free economic commands in SN commit `d87ec16c` (source transplant of
`61a95025`) pass focused qualification. This is source evidence, not a mainnet
mode readback or 10%/90% payout result.

The isolated source worktree ran `go test -p 2 -parallel 2 -count=1 -timeout 5m
-v ./mainnet` normally and with `-race`: **37 passing tests per mode, zero
skips/failures**. `go vet ./mainnet` and `git diff --check` passed. After the
worktree was integrated, the root SN branch independently passed `go test
./mainnet -count=1`, `go test -race ./mainnet -count=1` and `go vet ./mainnet`.

Three synthetic causal overlays retained failing-before evidence for treating
absent mode storage as Recycle, dividing each interval independently instead of
carrying rounding, and allowing changed metadata to reach storage despite a
reviewed hash. The current tests pass these controls, plus exact finalized-hash
binding, Burn default and stored Burn, malformed enum bytes, changed runtime
identity, bounded RPC replies, and mode evidence that never sets
`activation_ready`.

At the owned Snow testnet route, runtime-471 metadata was 354,056 SCALE bytes
and 708,150 JSON-RPC reply bytes; retained runtime-455 and runtime-467 SCALE
metadata were 334,642 and 347,304 bytes. This was a read-only size check on
testnet, not mainnet approval. The metadata reply has an 8 MiB bound; ordinary
identity/storage replies retain the 1 MiB bound.

At 07:21 UTC the test-data USB SSD disconnected during preparation. The kernel
aborted its ext4 journal; the drive reappeared with a new device name. By the
configured UUID, `e2fsck -p` replayed the journal and reported clean, then
`/mnt/data` was remounted. The uncommitted worktree and causal logs were
present, but that alone did not count interrupted work as complete. Final
normal/race tests and vet were rerun after recovery, before commit/push.

Detailed local logs and overlays are under
`/mnt/data/sn-testnet/qualification/mainnet-owner-recycle-20260927/`.
The complete [command contract](../ECONOMIC-GATE.md) identifies the remaining
source-to-code, chain identity, owner-census, runtime outcome and activation
gates. No transaction was sent.
