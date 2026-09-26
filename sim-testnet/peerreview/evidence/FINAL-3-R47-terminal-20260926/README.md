# R47 sealed partial result

The signed R47 release interval began at finalized block 8,090,674 and was
scheduled for five measured epochs 651–655, ending at block 8,092,174 with
terminal target **8,092,324**. The owner exited and sealed `result.json` at
16:02 UTC on 2026-09-26, reporting its last finalized observation at block
**8,091,300** (epoch 653). The chain had not reached the scheduled terminal.
`final_acceptance=false`, `result=fail`, six assertions were recorded and five
failed. The result explicitly says the acceptance observation was interrupted
before terminal evaluation; pending scenario, fault and adversary gates were
not evaluated. The owner-signed generation-47 attempt was then invalidated
with `execution-exited-before-completion`. These are **partial-run** records,
not a qualified release or a complete interval.

The immediate `scenario_context` error was the heartbeat process-log gate. Its
first blocking class was `operator-1-api/stderr/warning` count 2. Both exact
lines at 15:58:12 UTC report MinIO records-stream TCP reads reset by the peer
after roughly 2½ minutes. Operator 2 logged the same transport class at that
time. The `stop-warning-lines.json` receipt binds the raw lines to their
original byte offsets and SHA-256 hashes in the retained process logs. A
read-only MinIO health GET later returned HTTP 200; this establishes recovery
at that later instant, not the cause or duration of the earlier resets.
Other strict failures, including native steering gaps, adversary errors,
zero-funded measured entitlements and low epoch-652 usage, were already
retained. None was waived by the partial result.

The copied JSON files are byte-identical to the sealed run. The complete
76,069,209-byte `observations.jsonl` is stored as `observations.jsonl.gz`;
`stop-warning-lines.json` states its uncompressed SHA-256 and size. The two
published operator scenario bundles, fault ledger, adversary ledger, process
findings, assertions, anomalies, analysis and signed start/invalidation are
also included. Run `sha256sum -c SHA256SUMS` from this directory to verify
bundle files; decompress observations and compare its hash to the receipt.

The [on-chain zero-entitlement bundle](../FINAL-3-R47-zero-entitlement-20260926/README.md)
independently checks measured capture, entitlement and claim receipts against
the LAN archive RPC. The [R48 native recovery proposal](../../R48-NATIVE-RECOVERY-PROPOSAL.md)
retains separate native-state evidence. The next numbered final report must
state this failed, interrupted result explicitly and must not treat any
missing terminal check as a pass.
