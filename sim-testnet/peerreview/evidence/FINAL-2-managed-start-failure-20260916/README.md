# Managed-start failure, 2026-09-16

The strict resume ran from 05:47:36 to 06:29:05 UTC and exited 1. Body, outer
and joined exits are preserved in `native/`. It processed 4,673 carried-action
audits and reached managed startup; this was not a passing campaign or soak.
The dependent release-candidate request was not run.

The new supervisor generation began at 06:28:13.10245008Z. Its process-log gate
recorded four blocking classes at 06:28:53.288449177Z: each operator taskworker
emitted two errors and one warning. The six lines concern geolocation
certificate-pin rotations and fiat-payment notices for synthetic accounts.
The proposed fix selects the simulator's required operator workload, including
retained queue handling. It does not change certificate validation or the log
gate. Qualification and successful startup remain separate requirements.

`safe/SAFE-FINDINGS.json` preserves counts, roles, offsets and exact line hashes
without raw log text. `safe/STOPPED-STATE.json` records all 33 managed process
PIDs as zero, all unhealthy, and the original supervisor PID absent at its
recorded observation time. No process was signaled by that inspection.

The original byte comparisons in `native/state.diff` show only
`supervisor.state.json` and `supervisor.json` changed among the watched files.
Plan, deployment journal, redacted configuration, public identities, executable
and release lock were unchanged. This limited comparison does not establish
that all actor state was unchanged or that no background transaction occurred.
Partial-start reconciliation is recorded separately when completed.

The retained derived `safe/ORIGINAL-FENCE-SUMMARY.json` incorrectly lists an
empty string in `changed_watched_paths`. Use the original before/after hash
records and full diff for path attribution. The original summary is retained
unchanged; its matching hashes and exit fields are not a fresh live-state check.

The launcher input-hash warning and subsequent supplement are retained with
their original timestamps. A relative wrapper invocation caused its command
hash lookup to fail after changing directory; the native command continued.
The later supplement does not replace the original partial manifest.

Raw generation logs and runtime material remain in the private local bundle
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r2/strict-resume-failure-evidence`.
Do not publish that directory. Its exact gate SHA-256 is
`b7f596fb805f87a3fa08b36581b5e842952dfc4941f67bf70fc1667f8c1285d5`;
its manifest SHA-256 is
`abd128deba330577dd1dc28df45cc594d210e0903b75fd461fc1d5555daba0ad`;
its seal SHA-256 is
`226c83b86519bed330289ce7580f01b6b6e3a3c220430eed0222fc05911ae10b`.
Root independently verified all 115 sealed entries. All 66 generation-bounded
log slices were captured; eight post-gate additions totaling 6,431 bytes remain
private and were not treated as a new passing gate.

These are local process and preparation receipts, not independent RPC or
on-chain acceptance evidence. Verify this public projection with
`sha256sum -c SHA256SUMS` from this directory.
