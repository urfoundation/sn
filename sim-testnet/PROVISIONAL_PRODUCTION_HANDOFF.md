# Provisional production handoff

An explicitly authorized testnet continuation can run the complete production
soak after a failed release interval. It keeps the release result as `fail`,
retains every failed assertion and source file, and records
`provisional: true, final_acceptance: false`. It cannot satisfy strict release
acceptance.

After the release writer exits, use the same approved configuration, deployment
state, plan hash, repository paths, and owned RPC route:

```sh
sim-testnet scenario --name production-soak \
  --config /path/to/approved/testnet.yml --state-dir /path/to/deployment \
  --plan-hash 0xAPPROVED_PLAN_HASH --owned-rpc-authority HOST:PORT \
  --provisional-resume \
  --provisional-release-run-id EXACT_RELEASE_RUN_ID \
  --apply --format json
```

The source must be the current owner-signed release attempt with a terminal
failed result and an invalidated acceptance boundary. Its authenticated
observation prefix must reach the full five-epoch window and finalization block;
its result must match that signed terminal head. The writer lock prevents a
concurrent release writer. Reaching the terminal block or producing an external
diagnostic report alone does not authorize this handoff. The normal runner can
continue until its watchdog when failures persist; this command does not shorten
the source interval or stop its process.

Every scheduled release fault must be `restored` in both the signed checkpoint
and terminal result, with restoration no later than the signed terminal head.
The active-fault recovery ledger must also be empty before the first production
attempt. An unsigned cleanup claim or an active/pending release fault cannot
authorize production, including when a lifecycle tail extends beyond the five
accepted epochs.

The command creates an immutable owner-signed
`runs/EXACT_RELEASE_RUN_ID/provisional-production-handoff.evidence.json` before
creating the production attempt. It binds the original signed attempt,
campaign-start marker, observation log, result, assertions, anomalies, faults,
adversaries, process log evidence, and inherited lifecycle handoff by exact
content hashes. Retries validate those bytes again. It creates no release
`complete.json`. Production receipts retain the distinct provisional gate and
the original lifecycle plan/run identities.

Changed signatures, source hashes, deployment/plan/policy/chain identities,
ownership, custody, source interval geometry, or lifecycle provenance still
reject the transition. The source assertions `contracts_installed`,
`native_custody_hotkeys`, `policy_hash_matches`, `runtime_code_matches`,
`public_identities`, `rao_conservation`, and `runtime_transfer_minimum_bound`
must pass. Other failures remain recorded findings, never substituted passes.

Production still executes its approved cadence and hyperparameter actions,
operator key rotation, dishonest-deposit phase, discarded preparation epoch,
three full 360-block epochs, 180-block finalization offset, and rolling faults.
Ordinary transaction, budget, ownership, readiness, and recovery gates remain in
force. Strict production uses the existing clean release-completion path and
rejects this provisional predecessor.
