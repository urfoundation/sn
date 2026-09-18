# Finalization handoff — stopped 2026-09-17 UTC

The user explicitly stopped finalization. Do not run further tests, build a
candidate, seal a receipt, bind a recovery request, make live RPC calls, or
modify live state until a new owner resumes the work.

## Current checkpoint

The partial Terra qualification capture is:

```text
/mnt/data/sn-testnet/qualification/runtime463-alpha-source-r5.Rp3qNv
```

It is a **test-stage checkpoint only**. No candidate build was started, and it
has no `RESULT.json` or `SHA256SUMS`. `candidate/admitted-before-build` exists,
but it is only a pre-build marker; it is not a candidate admission receipt.

A source-change stop was initially requested because
`sim-testnet/evidence_relay_horizon_runtime.go` appeared to change. Parent then
rechecked and established that this file was already part of the R5 frozen
source (its mtime is Sep 16) and that the R5 before/after test fence passed.
Thus R5 has no established source drift. The final stop is user-directed, not a
test or source-integrity failure.

Preserved R5 evidence:

| Lane | CRV4 roots | simulator-prefix roots | exact new root | combined test exit | no-test guard |
| --- | ---: | ---: | ---: | ---: | ---: |
| normal | 7 | 6 | 1 | 0 | 0 |
| race | 7 | 6 | 1 | 0 | 0 |

The exact files are:

```text
normal/body.exit                         race/body.exit
normal/no-tests.exit                     race/no-tests.exit
normal/crv4.r4.roots.{txt,count}         race/crv4.r4.roots.{txt,count}
normal/sim-testnet.r4.roots.{txt,count}  race/sim-testnet.r4.roots.{txt,count}
normal/sim-testnet.new.roots.{txt,count} race/sim-testnet.new.roots.{txt,count}
normal/{command.txt,stdout.log,stderr.log}
race/{command.txt,stdout.log,stderr.log}
meta/source-freeze-before.sha256
meta/source-freeze-after-tests.sha256
meta/source-tests.compare.exit=0
meta/git-tests.compare.exit=0
meta/dependency-tests.compare.exit=0
```

The frozen test selectors are:

```text
CRV4 prefix: ^TestProvisionalRuntimeCompatibility
exact: ^TestAuthenticatedRuntimeDefaultMinTransferBindingAcceptsApprovedProvisionalArtifact$
combined: ^(TestProvisionalRuntimeCompatibility.*|TestAuthenticatedRuntimeDefaultMinTransferBindingAcceptsApprovedProvisionalArtifact)$
```

Do not accept a future run that reports `[no tests to run]`. The required root
counts per lane are 7 CRV4, 6 simulator compatibility-prefix, and 1 exact
DefaultMinTransfer regression.

## Runtime-463 issue and fix under qualification

The retained-state recovery doctor previously rolled back before process start:
its finalized-alpha-source `DefaultMinTransfer` binding observed runtime-463
code hash
`0x9745e3f66053c3c7cb30ea45b88c66438b5076da78154f477e8660b0ded43869`
while it expected the legacy runtime hash. Sol's source fix in
`sim-testnet/substrate.go`, with regression coverage in
`sim-testnet/substrate_test.go`, accepts the authenticated provisional runtime
artifact for the sanctioned compatibility profile in both direct storage reads
and finalized setup facts. R5 is the focused test evidence for that fix.

## Invalid historical receipts

Do not use an earlier R4 candidate for recovery. In particular,
`/mnt/data/sn-testnet/qualification/runtime463-alpha-source-r4.Fm8sQr` is
invalid because its anchored prefix selector returned CRV4 `[no tests to run]`;
its build was interrupted (exit 143). Older R1/R2/R3 artifacts are preserved
only as failure history. R3 is invalid because a diagnostic altered tracked
`go.sum` after its source freeze.

The tracked `go.sum` was later restored to SHA-256:

```text
177b50d84935a29c36b1771816478bf3c97ac28c99bcb22be4922ad9ba1106a3
```

The offline module-cache repair proof is:

```text
/mnt/data/sn-testnet/qualification/runtime463-dependency-repair-20260917.AlhQrj
```

It proves `go list -m -json all` works offline. Do **not** run `go mod
download` in the primary tree.

## Resuming after explicit authorization

1. First read the R5 checkpoint and verify the current tree remains identical
to `meta/source-freeze-before.sha256`, Git snapshot, and offline dependency
fence. If it does, a future Terra agent may complete its post-test admission
and build; if it differs, create a new clean capture.
2. Use Terra medium for every test/gate. Use Sol max for failures and source
fixes. Keep `TMPDIR` and `GOCACHE` on `/mnt/data`, with:

   ```sh
   TMPDIR=/mnt/data/sn-testnet/qualification/<capture>/tmp \
   GOCACHE=/mnt/data/sn-testnet/gocache GOMODCACHE=/home/by/go/pkg/mod \
   GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local GOMAXPROCS=4
   ```

3. Only after all tests and fences pass under one frozen source set, build:

   ```sh
   go build -trimpath -buildvcs=true -o <capture>/candidate/sim-testnet ./sim-testnet
   ```

   Record binary SHA-256, `go version -m`, source/Git/dependency after-build
   fences, then write and verify the receipt manifest.
4. The required receipt schema is
   `runtime463-crv4-sim-final-qualification/v5`. In both `normal` and `race`,
   record these JSON **string** fields:

   ```text
   test_exit="0"
   crv4_root_count="7"
   sim_root_count="6"
   exact_selector="^TestAuthenticatedRuntimeDefaultMinTransferBindingAcceptsApprovedProvisionalArtifact$"
   exact_root_count="1"
   exact_test_exit="0"
   ```

   Generate `SHA256SUMS` after `RESULT.json` and verify it with
   `sha256sum -c SHA256SUMS`.
5. Do not start recovery merely because a candidate qualifies. The recovery
bundle must remain unbound (no `REQUEST.json`, no `outer.started-at`) unless a
later explicit authorization permits it.

## Live-state preservation

The retained topology has not been resumed. The prior real R5 resume attempt
failed its doctor before process start and rolled back. Preserve its process
and restart evidence, plan, successor, receipts, and validator trees. Do not
manually restore or delete them. The LAN archive endpoint is
`192.168.1.162:9944`; no live access is needed for the qualification work.
