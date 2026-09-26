# Exact generation2 / epoch 648 plan — read-only review

Driver: `/mnt/data/sn-testnet/qualification/r47-live-preparation-20260926/sim-testnet-550bbaa6-stamped`
Driver commit: `550bbaa67c4313e1838c9612a00818e1ca439d38`
Driver SHA256: `4c2d4e8dd01fe0ef6be991a037ead37e42e9db94ed983395e097f792981521a5`
Rollover plan hash: `0xaecc96a8238d6309bb3d611627194a198390f3b458b4f9e830cacdada99fa120`
Rollover plan path: `/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/policy-rollover/plans/generation-00000000000000000002-epoch-00000000000000000648/plan.json`
Setup plan hash: `0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e`

At 10:02:48 UTC the LAN finalized EVM block was 8089557, settlement epoch 647. Approved cutoff 648 starts 8089774 and ends 8090074; first full epoch 649. The observed 12-second cadence gave 217 blocks / 43.4 minutes before cutoff,517 / 103.4 minutes before epoch end. Those are estimates, not a guaranteed deadline. Native scheduling is distinct: at 09:55:31 native epoch 1688 had last boundary 8089451, tempo 360, next boundary 8089811. Native and EVM hashes are separately recorded and the exact plan snapshots independently matched their canonical blocks.

Use the `r47_common` array in `commands.md`. Parent already rendered the following no-apply plan; this reviewer did not invoke it:

```bash
r47_binary=/mnt/data/sn-testnet/qualification/r47-live-preparation-20260926/sim-testnet-550bbaa6-stamped
r47_rollover_plan=/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/policy-rollover/plans/generation-00000000000000000002-epoch-00000000000000000648/plan.json
r47_rollover_hash=0xaecc96a8238d6309bb3d611627194a198390f3b458b4f9e830cacdada99fa120
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume \
  --plan-hash "$r47_plan" --rollover-generation 2 --rollover-epoch 648 \
  > "$r47_output/rollover-plan-648.stdout.json"
```

The exact same-plan publication command below is proposed only after successful independent budget review and current-state reconciliation. It is not executed by this read-only task.

```bash
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume \
  --plan-hash "$r47_plan" --rollover-plan "$r47_rollover_plan" \
  --rollover-plan-hash "$r47_rollover_hash" --apply \
  > "$r47_output/rollover-published-648.stdout.json"
```

If the four consents finalize before block 8089774, the command deliberately reports `resume after activation boundary block 8089774 is finalized`. That means the immutable publications are complete, but client/config staging awaits the finalized cutoff. Keep the retained supervisor running. After 8089774 is finalized, reapply the identical path/hash to reconcile those consents and complete staging. Do not change epoch 648 or its four randomized consent bytes on a retry. The activation boundary must precede 8090074. If another writer advances the reviewed deployment journal, preserve that refusal and obtain a separately reviewed plan; do not rewrite this plan.

After staging, follow `commands-successor.md`: official stop → exact generation2 handoff activation → plan/review/apply the distinct generation2 `--rollover-source-role` approval → retained resume. Both source slots were read independently: V1 was absent and V2's `0x798e599a4d388cc585da325be50d3197b75e4c5d0316e09d366d9c4a9e415a89` at block 8079823 matched its retained gen1 applied intent. Role activation must reverify this state. The source-role plan belongs under `policy-rollover/source-role/generation-00000000000000000002/plan.json`; its new hash is not the rollover activation hash.

No live apply, transaction, stop or restart was performed by this reviewer. A fresh interval and independently verified new evidence remain necessary for acceptance.
