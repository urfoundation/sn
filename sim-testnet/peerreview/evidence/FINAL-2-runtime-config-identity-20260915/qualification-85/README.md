# Runtime configuration identity: retained failed 85c qualification

This portable subset records the closed failed candidate at 85c09583ce8a0728b4b33b04b19c9e7d7ea1272a, before its fixture-only successor. The exact 70-root selection contained 20 migration and 50 adjacent top-level roots; the seven existing parent roots declared 58 descendants.

Both normal and race compilations passed. In each mode, the adjacent 50-root body passed and the migration body had one actual failure: TestCoordinatorRepairCarryRuntimeIdentityAuthenticatesOriginalRepair, which reported setup migration changed original action or budget authority: <nil>. The other 69 top-level roots passed in each mode. This bundle does not relabel the candidate as passing and records that no p2/p3 confirmations were attempted.

The first compiler invocations refused before compilation because a capture-local snapshot operand was absent. The first descendant replay invocations also refused because they passed a numeric status instead of a canonical body.exit path. Both refusals are retained. The accepted replay tool was then run offline against the unchanged event streams with the canonical absolute receipt; it validated the exact observed normal/race outcomes without rerunning a body.

Excluded: compiled test binaries, caches, temporary directories, source worktrees, and the independently owned causal evidence. RAW-LOCATORS.tsv maps each copied record to its retained original.
