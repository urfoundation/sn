# Composed R46 review and terminal scanner hash repair

Reviewed clean integration commit `128884072562462db56f8f0eaa04330a1c1080ac` without editing that worktree. The isolated repair is based on the same commit at `/mnt/data/sn-testnet/worktrees/astra-r46-terminal-log-hash-20260926`.

Confirmed defect: `hashProcessLogLine` publishes bare 64-digit hex in `FirstLineSHA256` and `LastLineSHA256`, whereas the new failed-terminal consumer called the archive content-hash validator, which requires `sha256:`. Thus real repeated exit-gap and TLS findings never ended otherwise complete failed observation. The original synthetic consumer test accidentally used the archive shape and missed the producer/consumer mismatch. RootMissed and invalid committed tier artifact stopping paths are independent and were unaffected.

The repair adds the validator prefix only during the predicate. It does not change any persisted scanner bytes, classifications, isolated-event allowances, acceptance scope, fault cleanup, strict assertions or successful completion. Prefixed input is still refused because it is not the scanner wire format. New tests run the real scanner, persistence/reload and final strict gate with repeated exit-gap and TLS bytes; isolated counts remain ineligible and repeated counts remain strict failures. Existing negatives now use the real wire shape and include nonhex, short and already-prefixed hashes plus foreign scope, expected fault, recovery and unknown classes.

Pre-fix deterministic control: the new scanner test and corrected original consumer test both failed, package `0.177s`, exit 1. The real scanner retained exit-gap count 2 and TLS count 3 as blocking but the failed-completion reason was empty, including after persisted reload. Log: `pre-fix-normal.log`. No sleeps or scheduler luck establish the failure.

Exact focused/adjacent selector:

```text
^(TestScenarioFailedTerminal|TestProcessLogTlsTransient|TestProcessLogIsolatedExitGap|TestScenarioContinuesAfterIsolatedTls)
```

Commands use private `TMPDIR=/mnt/data/sn-testnet/qualification/r46-terminal-log-hash-20260926/tmp` and `-modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod` with `go test ./sim-testnet -count=1`, and the same selector under `-race`. Combined focused/adjacent normal PASS 7.185s; combined focused/adjacent race PASS 42.537s, exit 0, no race findings. Exact commands/results are in `qualification.json`. Two-file fence `source.sha256` matched after both completed; `git diff --check` passed.

Adjacent composed review:

- Cleanup: cumulative endpoint reads authenticate the owner checkpoint, unchanged signed start identity, exact observation prefix, requested/completed cuts, non-mutating lifecycle handoff and process census. The live writer still requires the prior request-only signed transition. No cleanup authority was broadened for normal writers or strict successful completion.
- Operator windows: fixed stats range and moving proof range match the server's overlap/create-time predicates; per-read retries preserve both exact bounds and completed independent surfaces. A saturated page remains incomplete. Probe state has one sequential caller in both live snapshots and independent diagnostics.
- Adversary errors: chronology is initialized before actors start, appends after releasing campaign state, and finishes only after Stop joins workers. It changes no error totals or fault-attribution authority. API identity uses durable ID/role/identity rather than unrelated health. Verify attribution requires every joined cause to be typed request unavailability; successful semantic violations remain blocking.
- Replay: assignment authentication cache is bounded and invocation-local, owns exact key/message/signature bytes, and retains successes only. Canonical record context, validator signatures, lifecycle ordering, proof projection and final roots still run for every record. No cross-generation or persistent cache authority was introduced.
- Restart readiness: every operator must supply a signed fresh trail under the active source namespace and reviewed keys, with kernel generation checked before and after. An additional unproven race was identified for separate deterministic follow-up: a legitimate supervisor replacement during the read currently produces a hard error, whereas the earlier liveness read treats disappearance as pending. Parent authorized a separate barrier regression after this hash repair qualifies. Wrong identity/ticks and cancellation must remain strict.

These changes do not repair or waive R46's immutable missed root, native/vector shortcomings, uncertain claim cut or historical process failures. Those remain failed evidence; early completion only avoids waiting for an outcome that can no longer pass.
