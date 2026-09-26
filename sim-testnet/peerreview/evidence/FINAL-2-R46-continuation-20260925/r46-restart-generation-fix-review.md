# Validator replacement during restart readiness

Base: composed R46 `128884072562462db56f8f0eaa04330a1c1080ac`. Isolated branch `codex/astra-r46-validator-readiness-replacement-20260926`, worktree `/mnt/data/sn-testnet/worktrees/astra-r46-validator-readiness-replacement-20260926`. No live/sealed run, chain state, main checkout or integration worktree was modified. Six-file final source fence: `source-final.sha256`. Qualified commit: `32e3e8d1a5a6bed31e445fcf31ff8c7c376c4ddf`.

Two deterministic defects share a generation-lifetime boundary:

1. The controller already treats an exited replacement as pending before proof validation. Its later kernel check instead hard-failed if the same child exited during authenticated path/proof reads, even when the supervisor had published a valid stopped state or a newer approved child. That error escaped the pending branch and failed the fault. Tests use real test-owned children with pipe-controlled exit and a barrier after a real signed proof-tail read. Original source fails with `validator replacement kernel generation changed` and an absent `/proc/.../stat`. No sleep or scheduler timing proves the ordering.
2. After successful proof readiness, `captureFaultCompletion` may wait up to 300 seconds for a finalized head. The old caller then deleted the active intent without rechecking its ready child. The deterministic completion-reader callback replaces that child; old code returns its stale PID and removes the intent while stamping head 161. The fresh replacement has not yet produced its own trails.

The repair rechecks exact manifest identity, monotonic restart count/start time, current kernel start ticks, health, and cancellation before/after proof reads and after the finalized-head read. A missing process, valid stopped state, or newer approved kernel-matched generation keeps the fault pending. No generation inherits an earlier child's proof freshness. The next Restore must read signed trails after the new start and obtain a fresh completion head. Original restart intent bytes remain unchanged while pending; an invalidated completion stamp is cleared. No extra termination, signal authority, acceptance waiver, timeout increase or wire change was added.

Fourteen proof-read negative controls cover foreign ID/role/identity, missing/wrong ticks, inconsistent/backward restart history, malformed stopped state and manifest mismatch; cancellation remains the owner's error. Completion-read controls independently reject wrong identity/ticks and cancellation. Existing signed source namespace, old whole-trail, starvation/next-fault, tampered signatures, bounded-tail, pending-controller, completion chronology and incomplete-terminal tests run in the same selector. Non-validator process, container and view restoration behavior is unchanged. A last-moment observation cannot guarantee future process lifetime; this patch closes the two unbounded read windows and does not claim that guarantee.

## Exact commands

Run from the isolated worktree, with all temporary files on `/mnt/data`:

```bash
TMPDIR=/mnt/data/sn-testnet/qualification/r46-validator-readiness-replacement-20260926/tmp go test -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod ./sim-testnet -run '^(TestValidatorRestart|TestProcessRestartRestore|TestFaultCompletion(ProcessRestore|RestoreExpiry)|TestScenarioFailedTerminalRejectsIncomplete)' -count=1 -timeout=15m
TMPDIR=/mnt/data/sn-testnet/qualification/r46-validator-readiness-replacement-20260926/tmp go test -race -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod ./sim-testnet -run '^(TestValidatorRestart|TestProcessRestartRestore|TestFaultCompletion(ProcessRestore|RestoreExpiry)|TestScenarioFailedTerminalRejectsIncomplete)' -count=1 -timeout=25m
TMPDIR=/mnt/data/sn-testnet/qualification/r46-validator-readiness-replacement-20260926/tmp go test -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod -overlay=/mnt/data/sn-testnet/qualification/r46-validator-readiness-replacement-20260926/old-proof-read-final.overlay.json ./sim-testnet -run '^TestValidatorRestartReplacement(ExitDuringProofRead|DuringProofRead)' -count=1 -timeout=15m
TMPDIR=/mnt/data/sn-testnet/qualification/r46-validator-readiness-replacement-20260926/tmp go test -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod -overlay=/mnt/data/sn-testnet/qualification/r46-validator-readiness-replacement-20260926/old-completion-read-final.overlay.json ./sim-testnet -run '^TestValidatorRestartReplacementDuringCompletionRead' -count=1 -timeout=15m
```

## Qualification

- Final focused+adjacent normal: PASS, 43.433s, exit 0 (`final-focused-adjacent-normal.log`).
- Final focused+adjacent race: PASS, 223.430s, exit 0, no race findings (`final-focused-adjacent-race.log`).
- Exact old proof-read body, with the inert test barrier and final head guard retained: RED, 7.539s, exit 1; both root tests report the absent old kernel process (`causal-final-old-proof-read.log`).
- Only final completion guard removed, with the new signature retained inertly for compilation: RED, 3.075s, exit 1; stale PID returned and original intent removed (`causal-final-old-completion-read.log`).
- Before adding the completion-boundary repair, its new test also failed directly on the previous source, 3.081s, exit 1 (`pre-completion-fix-normal.log`).
- Earlier proof-read snapshot focused+adjacent normal/race passed 34.408s/178.417s; these are preliminary, not substitutes for final six-file qualification.

The first completion overlay compile failed solely because its removed consumer left an unused return variable (`causal-final-old-completion-read.compile.log`). That is not counted as a causal result; the corrected, compilable narrow overlay produced the behavioral RED above. Logs and `qualification.json` retain exact provenance.
