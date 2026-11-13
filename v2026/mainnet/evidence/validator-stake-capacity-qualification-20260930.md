# Validator stake-capacity admission qualification

The frozen source is `4eec6106063f6f416fd784b229aa9b93af2fe6fb`
(tree `675c15a5f17071d9923ff9521e97a4461e85ac74`). Its integration into
the current validator evidence path is the exact tested merge
`2eb7d125403f82fd4fe37ddd545703fb5d03cdf4` (tree
`ceee175e9afb8e9f8b33d774412d42dbec9e6916`). Three activation files
needed composition; all other majority and previously qualified files retain
their original bytes. Two integration-only roots cover alternating read-only
admission modes and retention of both projections.

Sol medium independently verified the frozen candidate across **20
package-pass cells**, with **422/422 named executions** passing. All 17
focused roots passed normally and with race detection under umask 0002 and
0077; all 138 adjacent roots passed normally. Selected adjacent race coverage
passed 12 mainnet, 15 CRv4 and nine validator roots. Repeated arithmetic,
cancellation and canonical-change controls passed. A mutation that excluded
non-permitted peers before the first normalization failed its intended
majority/quantization assertion. Vet and source fences passed.

The merged build separately passed **134/134 named executions** across ten
package-pass cells: 23 focused and 12 adjacent roots normally and under race,
both new integration roots in normal/race under umask 0077, and bounded
composition/owned-read repetitions in both modes. Two isolated negative
patches failed at the intended retained-projection and evidence-dispatch
assertions. Vet, exact parent/path/blob lineage, source/dependency checks and
clean worktree checks passed.

The [frozen receipt](/home/by/urnetwork/temp/sol-validator-majority-20260930/evidence/RESULT.md)
and [merge receipt](/home/by/urnetwork/temp/sol-validator-majority-integration-20260930/evidence/RESULT.md)
retain raw logs, executed binaries, test selectors, exact exits, source and
dependency fences and control patches. Their manifest SHA-256 values are
`3d2b25da3b09dcabe6d018a7f3b020db05c9d3e7cf38b6e006efe17cd06c1f5a`
and `e0e516ef240a7b027072fd11fd86d8cb8419be6bae133d44df1b426d9e2fbe28`.
The author and integration handoff manifest SHA-256 values are
`0de7b8c376f7b39865281a085fd67ef538c0a5515b87467f9c1b2f4a90b76f3b`
and `cab57f662d05aeceb7ac22eedc54605d9425dde4eb313d6d2f7d1088fb70947e`.

The read-only `admit-stake` command authenticates a complete original-plan
native census and pinned runtime calculation, including stake floors,
registered-owner/threshold rules, quantization and current activity. It
reports a conservative stake-capacity bound separately from active share.
It does **not** prove applied weight influence, actual majority control,
producer health, global signer exclusion, or the 10/90 economic outcome.
Public fresh starts remain closed. No live RPC, signer, transaction,
deployment or service start was used.
