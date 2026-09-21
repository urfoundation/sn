# Sharded qualification, 21 September 2026

The combined broad/focused selection passed all **147 roots and 54 legacy
subtests in each of normal and race modes**. All 74 bodies exited 0, their owners
exited 0, and child cleanup was proved. The final source fence was unchanged.
This is a selected qualification union, not a complete release gate.

The prior broad race command exhausted its 600-second package budget with late
roots still queued at `testing.T.Parallel`. The new runner schedules four-root
shards, with two jobs and the original 600-second body timeout, `parallel=4`,
and 660-second outer timeout. It compiles once per mode and retains completed
stage receipts for exact-source continuation. No test or deadline was removed.

The original root-only metadata omitted existing `t.Run` descendants. Its raw
collector honestly exited 1 with 14 checker refusals. Source-declared metadata
was corrected separately; all 74 saved event streams then matched. No successful
body was rerun. `outcomes.tsv` records the complete identities for either mode;
`obligations.json` preserves broad 143 / focused 18 / overlap 14 / union 147.

Final harness package checks passed in 1.338 seconds normally and 7.011 seconds
under race detection, including the missing-descendant replay regression. The
complete Go-source and README manifest was unchanged across both checks.
Deterministic negative controls independently reproduced loss of partitioning,
completed-stage reuse, and inherited child ownership locks.

Raw evidence remains under
`/mnt/data/sn-testnet/qualification/sharded-qualification-20260921/`:

- `union147/report.json`, `source.before.json`, `source.after.json`, and original
  per-stage command/result/event/owner artifacts preserve the collector outcome.
- `union147.corrected-plan.json`, `corrected-metadata/`, and `replay-initial/`
  contain corrected declarations and all 74 checker-only results.
- `union147-retained-body-events.sha256` binds 296 retained artifacts; its digest
  is `bae67580ab087dfd830b0efba766cf81d43b411d9b70dbebfffb70bf0dee1625`.
- `final-package/` retains normal/race logs and equal source manifests; their
  manifest digest is `32162699fa799343fc1a4218a439131fde6515402d9e76087b9d5f3f60d95040`.
- `causal-overlays/` retains each isolated mutation and expected failing result.

`summary.json` records independently checked counts, original refusals, limits
and digests. The raw test run captured the implementation before the additional
metadata-replay regression was added; the final package checks cover that test.
No live simulator process, deployment state, or on-chain transaction was changed.
