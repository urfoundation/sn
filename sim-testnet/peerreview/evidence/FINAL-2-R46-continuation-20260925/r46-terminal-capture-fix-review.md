# R46 diagnostic stream capture progress

The sealed v2 diagnostic exhausted its independent 60-minute validator-2 source budget at 2026-09-26T01:24:30Z. It had reached cut 70/96, operator 2, the second origin, records chunk 3 (3,162,606 bytes), then reported context deadline exceeded and no authenticated EOF. The original outcome remains a hard failure. The subsequent process-log, fault-timing, companion and ordinary payout checks continued; companion and payouts passed. No live/sealed state was changed.

The historical v1 stack proves the slow path was the attempt-stream HTTP body read, not streamed assignment signature replay. Large overlapping immutable cuts and serial origin custody are established; disk presence from one origin does not authenticate custody at another. R46's 35 RPC and six artifact actor errors remain unattributed because per-error chronology was absent.

## Isolated change

Base: `517001c22c60bee02025f1f78d4c85609d806103`. Branch: `codex/r46-terminal-capture-progress-20260926`. The exact eight-file fence is `source-final.sha256` beside this review.

Diagnostic capture opts into exactly two independent origin readers, each sequential within its signed cuts. Workers lend at most one bounded body through an unbuffered handoff; the calling goroutine alone executes the durable sink and unchanged shared byte/object budget. Both workers are joined. A failed origin does not erase useful capture from its independent peer; the overall result still fails. Sink errors and panics cancel and join both workers.

Immutable metadata and data reads receive a finite 300-second operation with fresh 60-second attempt contexts. Only the existing all-leaves typed transport classifier allows retry; response-integrity, close, sink and mixed errors remain hard failures. Retry pacing honors the existing Retry-After policy. A success witness requires exact origin/kind/hash/size, authenticated EOF, successful close, durable sink acceptance and live owner context. Witnesses are bounded and invocation-local; on-disk or other-origin presence is not sufficient.

All cut headers and activation identities are authenticated before workers start. Strict/default capture keeps serial, single-attempt behavior. The diagnostic collector now rejects a check that returns a nominal success after its own deadline, retains its evidence prefix, and continues the next independent check under a fresh child context. It never sets final acceptance.

## Deterministic verification

Pinned module file: `/mnt/data/sn-testnet/qualification/r45-strict-successor-gates-20260925/review.mod`.

```
GOMAXPROCS=4 go test [-race] -modfile=<pinned-module-file> -p 2 ./validator ./sim-testnet -run 'Test(ReleaseCapture|TerminalDiagnostic)' -count=1 -v
```

- Composed normal: 50 top-level tests pass; validator 4.167s, simulator 8.842s (`focused-normal-final.log`).
- Composed race: same 50 pass; validator 8.057s, simulator 31.569s (`focused-race-final.log`).
- Final test-only addition covers a real body-close failure. All final 31 validator tests pass normal 4.020s and race 7.564s (`validator-normal-final.log`, `validator-race-final.log`). No production change followed composed testing.
- `causal-old-behavior.log`: exact old-decision overlays are RED at five tests: serial origin starvation, discarded independent source, no retry after truncated body, absence of finite fresh attempts, and late check incorrectly passing. New interfaces are retained in these controls; this is not described as an unmodified old checkout.
- `causal-witness.log`: disabling the completed-body witness is RED: repeated cuts emit 10 bodies instead of four and reopen completed prefixes.
- Adjacent negatives cover hash/mixed/close/sink failures, typed pacing, owner cancellation and joined workers, shared archive limits, sink panic, distinct origins, and strict defaults.
- `git diff --check` and all eight source hashes pass.

## Bounds and remaining findings

Parallel reads reduce serial origin waiting but do not guarantee completion within 60 minutes against a shared slow store. The fixed parent budget remains authoritative. Retained chunks may be reused only within this authenticated invocation and same origin; no cross-run cache or missing-source waiver is introduced. A cooperative bounded transport is still required for cancellation, as in the existing production reader.

The live sealed diagnostic separately exposed a wrong output-directory lookup for its adversarial campaign. That will receive a separate narrow patch and test; this capture patch does not modify it. No claims are made that these changes repair sealed R46 acceptance failures, the native history gap, missing validator-1 evidence, or empty payout census.

Read-only transition receipt: `/mnt/data/sn-testnet/qualification/r46-rpc-artifact-capture-triage-20260926/diagnostic-progress-20260926T013318Z.receipt.json` (SHA256 `ebea67dc3525d7526152c945437d32219154f04461075291ca3515d6f9905360`). Source-timeout progress copy: `diagnostic-progress-after-capture.json` in that directory, SHA256 `e49e51834ca93acc497369c5b188264750890f40b57773ae027e1db62d95d283`.
