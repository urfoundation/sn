# Successor activation addendum — proposed, not executed

This addendum supersedes the fresh-generation activation sequence in `commands.md` section C. The original sealed receipt remains the 09:09 UTC observation of R46; this addendum describes the isolated successor fix based on `1b4751be`. It grants no acceptance and records no new live-state observation.

Use the qualified successor commit/binary named in `receipt-successor.json`, the exact original `r47_common` arguments from `commands.md`, and a reviewed unused generation, future/current activation epoch, archived rollover plan path and its exact hash. All commands below are conditional execution instructions; none were run by this review.

Before the first `--apply`, finish the complete plan and allowance review: exact previous handoff path/hash/size; advancing generation/cutoff; both unchanged native hotkeys and four fresh VPK consents; canonical native/EVM snapshots and eligibility; current deployment journal; full retained/superseded spend, pending signed liabilities, four bounded activation attempts, fleet lifetime through the new release window, and relay capacity/allowance. `validatePolicyRolloverBudgetV2` remains mandatory. A fresh generation does not increase caps or excuse a missing allowance. If another actor advances the deployment journal, obtain a new exact plan instead of editing the old one. Do not activate or spend while qualification or this review is incomplete.

While the retained supervisor remains live, a qualified driver may prepare the exact rollover plan and, once reviewed/authorized, publish its four consents and stage independent generation2 files. Staging must report `staged-awaiting-stopped-topology`; generation1 remains selected. No new native source-role overlay is prepared until the actual generation2 handoff is selected.

```bash
: "${r47_binary:?qualified successor driver required}"
: "${r47_rollover_plan:?exact reviewed rollover plan path required}"
: "${r47_rollover_hash:?exact reviewed rollover hash required}"
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume \
  --plan-hash "$r47_plan" --rollover-plan "$r47_rollover_plan" \
  --rollover-plan-hash "$r47_rollover_hash" --apply \
  > "$r47_output/rollover-staged.json"
```

Stop becomes unavoidable when selecting the successor and its worker configs. Finish the current owner/diagnostic first, use the harness's official stopped-topology path, and reapply the identical plan. Both supervisor ownership and validator process records must be stopped. Preserve every original file and journal row.

```bash
"$r47_binary" stop "${r47_common[@]}" > "$r47_output/stop.json"
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume \
  --plan-hash "$r47_plan" --rollover-plan "$r47_rollover_plan" \
  --rollover-plan-hash "$r47_rollover_hash" --apply \
  > "$r47_output/rollover-activated.json"
```

Before `resume`, capture the generation2 native source-role continuation. This is a distinct immutable plan and approval, with no replacement epoch/generation flags. It retains gen2's fresh evidence ledger while authenticating the exact previously occupied native source slot.

```bash
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume \
  --plan-hash "$r47_plan" --rollover-source-role \
  > "$r47_output/rollover-source-role-plan.json"
```

Review that output's `plan_hash`, `generation`, `rollover_plan_hash`, `original_handoff_sha256`, both validator configs, source-intent hash/size and predecessor references. Use its exact archived `policy-rollover/source-role/generation-00000000000000000002/plan.json` only if generation2 is the selected unused generation; derive the path from the actual output for any other approved generation. The command has written only new local immutable planning files. Do not substitute the older generation1 source-role plan or the rollover activation hash.

- V1 may have a nil predecessor only when its retained gen1 store has never had an intent and independent current native-slot verification finds no occupied slot. A missing local intent plus an occupied slot is a hard activation blocker; do not synthesize or skip its evidence.
- V2's gen1 current intent must be finalized/applied, preserve its exact signed measurement/envelope/native inclusion, and match the current native slot. Unresolved, malformed, moved or mismatching history blocks role activation. Historical proof is not fresh gen2 measurement evidence.
- At least one predecessor must exist for this role-only plan; if neither exists, there is no role overlay to approve, and both empty-slot conditions still require verification before running fresh workers.

```bash
: "${r47_source_role_plan:?exact reviewed source-role plan path required}"
: "${r47_source_role_hash:?exact reviewed source-role plan hash required}"
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume \
  --plan-hash "$r47_plan" --rollover-source-role \
  --rollover-plan "$r47_source_role_plan" \
  --rollover-plan-hash "$r47_source_role_hash" --apply \
  > "$r47_output/rollover-source-role-activated.json"
"$r47_binary" resume "${r47_common[@]}" --provisional-resume \
  --apply --plan-hash "$r47_plan" > "$r47_output/resume.json"
```

Require zero setup actions on retained resume, both selected generation2 configs, unchanged original manifest inventory, both fresh validator/native decisions and signed completed-work trails, then launch the new `release-1.0` owner interval using section A of the original command receipt. Its earliest full acceptance epoch is the latest approved cutoff plus one. The relay retains the original/gen1/gen2 namespaces and historical policy-gap authority; no old gaps or R46 assertions are waived. A provisional owner can run despite missing acceptance evidence, but clean terminal sealing and final acceptance require fresh evidence and independently passing assertions.
