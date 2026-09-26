# R46 restart readiness and empty payout census

SN source: `089efa25acf4023219c2c2c7e7e6d8d9137e66e4`, branch `codex/r46-validator-restart-readiness-20260926`, based on qualified cache `166242a284292e0c1bdea808936f34c8355ed9ca`. Worktree `/mnt/data/sn-testnet/worktrees/r46-validator-restart-readiness-20260926`. Exact five-file fence: `restart-source-candidate.sha256`; clean committed worktree.

Server test-only source: `6a5c2f7a299690468694312652bb03ada00bb672`, branch `codex/r46-empty-payout-census-tests-20260926`, based on exact supervised server `4b2c45872ea0e2baef3db4012df8c47e280015dd`. Worktree `/mnt/data/sn-testnet/worktrees/r46-empty-payout-census-tests-20260926`. Exact one-file fence: `server-source-final.sha256`. No server production code changed.

## Confirmed restart defect

The old live restore path required a changed healthy PID and signal-zero existence only. It removed the original durable restart intent while startup replay still preceded operator proof workers. The existing sequential scheduler then allowed the next validator restart. The fixed path retains the original active fault until the replacement produces one fresh fully signed trail through every reviewed operator. It selects the activated VPK/source namespace, verifies server FINAL, validator co-signature and EXTEND, and requires the first signed hop after the complete second of the recorded supervisor start. It independently checks replacement kernel start ticks before and after reads, and rereads supervisor identity/health. The timestamp condition is an operational readiness check for this simulator's supervised clocks; it does not replace independent chain/time/proof history validation at final acceptance.

The reader examines at most two policy-bounded rows and rejects symlinks/non-regular files, oversize, malformed complete data, truncation/replacement or changed readback. Partial append bytes contribute nothing. Reentry rechecks the same retained fault and actual replacement; it cannot signal again or accept the frozen old proof namespace. Non-validator restart behavior is unchanged. No native intent/EMA history, signed plan, live fleet or sealed R46 evidence changed.

## Qualification

SN command (normal and race):
```
env GOMAXPROCS=4 TMPDIR=/mnt/data/sn-testnet/qualification/r46-terminal-replay-hardening-20260926/tmp go test -p 2 [-race] -modfile=/mnt/data/sn-testnet/qualification/r45-strict-successor-gates-20260925/review.mod ./sim-testnet -run '^Test(ValidatorRestart|ProcessRestart|FaultCompletion|FaultStateMachine)' -count=1 -v
```

- Final32-test normal PASS20.654s (`restart-normal-final.log`); race PASS94.506s (`restart-race-final.log`). Five new readiness tests cover real Restore and scheduler, reentry, all operator paths, signed authority, active namespace, first-hop boundary, file bounds and cancellation.
- Old PID-only decision RED in two new tests: it removes intent without proofs and authorizes the next restart (`causal/pid-only-restart.log`).
- Final-hop-only substitution RED: it credits an old trail whose last hop crossed the boundary (`causal/final-hop-only.log`).
- Solidity: `forge test --match-contract ReleaseSettlementTest --match-test 'test_(emptyPayoutCensus|missedRootCarries)' -vv`; PASS2/2 (`empty-carry-solidity-final.log`). Tests use solc0.8.24 and existing pinned libraries copied into the isolated worktree; no contract production changes.
- Server: `env GOMAXPROCS=4 TMPDIR=... go test -p 2 [-race] -modfile=/mnt/data/sn-testnet/qualification/r46-terminal-replay-hardening-20260926/server.mod ./controller ./startifact -run '^Test(StBuildReleaseProviderInputs|StBuildPayoutTreeSingleLeaf|EmptyEligibleSetPublishesAuditableMissedRootArtifact)' -count=1 -v`; normal PASS controller0.108s/artifact0.026s, race PASS controller1.398s/artifact1.081s (`empty-census-{normal,race}.log`).

Zero-leaf disposition is expected existing behavior, so no pre-fix failure is claimed for those test-only additions. The new server test proves nonzero usage plus an excluded head and proof-starved pool can produce an authenticated empty artifact. Assignment alone is insufficient; confirmed exposure restores one real pool leaf. The Solidity test proves zero-root refusal, exact operator-scoped carry, later valid claims and immutable prior RootMissed status. Neither test proves the exact historical provider-by-provider cause of NO2's empty census.

## Evidence and remaining blockers

`epoch634-empty-payout-census.receipt.json` SHA256 `0e32519ae98e26f137b73cb851624f89d9670c2a3c9a6ed157bd5096aebffe7e` binds NO2 zero leaves at21:46:43.944581/raw offset158414677, NO1 four leaves at21:46:44.593786/159197105 and NO1 commit at21:47:16.070548/159204242, the exact pinned no-leaf skip, and the historical RootMissed receipt. This was an empty payout census, not evidence of a failed NO2 root submission. Restart proof starvation is plausible upstream causality, not exclusive proof.

`retained-gap-native-roots.receipt.json` SHA256 `a5e4a466e3e20a23db821b6bdfe6b9c6c932e6cda09c6616f6d7146811f6aa8d` preserves all18 exact gap rows across10 acceptance-scoped swarm classes. The120s receive /300s send idle mismatch and three matching sender idle intervals support a recovery hypothesis; the missing-contract ACK/head/route failure is not yet proven. Test the complete real idle retirement/reformation path before selecting a production fix. These are sequence exits, not miner process exits.

`native-steering-rows.receipt.json` binds current-scoped first/last raw validator failures and continuity lines. Native1674's timed-out immutable record was later present on both replicas, and both input cuts were durable, but the applied intent and EMA remained1661. Publication readback cannot erase this13-native-epoch gap. Next-run admission must reject missing authenticated native history; require complete historical proof or an explicitly authorized fresh generation, never a gap waiver/reset. Signed-source diagnostic capture does custody and RPC/HTTP reads, not ASSIGN replay, so the cache patch cannot be credited with shortening that capture stage.

All fixes and tests remain isolated. The two code fixes improve future progress; sealed R46 remains failed. The full86-failure grouping is retained in `failure-clusters.receipt.json` and includes independent adversary/API, inherited lifecycle, governance and terminal-publication clusters.
