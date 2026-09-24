The `terminal-diagnostics` command inventories an exact retained campaign without
restarting its fleet, taking its writer lock, publishing evidence, or modifying
deployment files. It requires the same authenticated diagnostic executable and
explicit provisional testnet plan used by retained readers. Its output directory
must be new, canonical, and outside the deployment state.

```sh
/absolute/qualified/sim-testnet terminal-diagnostics \
  --config /absolute/approved/testnet.yml \
  --state-dir /absolute/deployment-state \
  --plan-hash <exact-current-plan-hash> \
  --name release-1.0 --run-id <exact-signed-run-id> \
  --provisional-resume --owned-rpc-authority <approved-owned-authority> \
  --diagnostic-output /absolute/external-diagnostics/new-invocation \
  --wait-for-terminal --format json
```

Supply the ordinary repository override flags when the approved configuration
requires them. Run this command as a separate supervised oneshot to survive a
terminal disconnect; it does not own or stop the release service. Waiting reads
the finalized owned-node head until the original signed window's terminal block,
with a 24-hour ceiling, then allows up to 15 minutes for the runner's original
result. Cancellation remains available. Omitting `--wait-for-terminal` gives an
immediate inventory, with future checks explicitly unavailable.

`report.json` is the authoritative diagnostic. `progress.json` retains completed
checks if a later read is interrupted. Exact source bytes are copied into
`originals/` with hashes; original result labels, signatures, process-log fences,
fault timings, and lifecycle exceptions are preserved. Current generation reads
are separate observations and cannot replace signed campaign history. Typed
operator transport failures use bounded retries with separate retry records.

The command attempts independent identity/history, operator, validator, proof,
process-log, fault, adversary, receipt, payout, native-coverage, and semantic
source checks. Missing dependencies remain unavailable; a complete strict source
graph can still fail its independent verifier. The diagnostic does not fabricate
that graph, a passing result, or an owner completion when the original run lacks
them. Inherited lifecycle bypasses remain explicit exceptions. Every report has
`read_only=true` and `final_acceptance=false`, including when individual checks
pass. A zero exit status means the diagnostic inventory was written; inspect
each check's `pass`, `fail`, `unavailable`, `finding`, or `exception` status.
