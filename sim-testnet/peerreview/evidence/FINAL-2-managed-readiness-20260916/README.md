# Managed semantic-readiness failure, September 16, 2026

The strict managed resume ran **09:41:17–10:20:25 UTC** and failed with:

> release topology semantic readiness timeout: every validator must complete a fresh verified trail through every operator

Its [body](strict-resume/body.exit), [outer](strict-resume/outer.exit) and
[join](strict-resume/join.exit) exits are all **1**. The complete
[stderr](strict-resume/stderr) preserves completion of all 1,000 historical
fleet checks and 4,673 carried-action checks before the timeout. The result
stdout is empty. This is a failed startup, not a failed preparation audit.

The new supervisor was PID 3676320, start ticks 184001647, manifest
`0xe945dd691a00ee8fd09fed5a2ed74d550cb67250f2ca8bb3e17ca6c80903a073`.
The [closed supervisor state](closed-state/supervisor.state.json), updated at
10:20:25, records all 33 children stopped with zero restarts. The
[process-log gate](closed-state/process-log-gate.json) has no findings through
its last scan at 10:20:10. This does not assert that later shutdown log lines
were scanned or that the semantic-readiness gate passed.

[Watched-state verification](VERIFICATION.json) confirms that only the
supervisor manifest and supervisor state changed. The saved plan, simulator
journal, redacted configuration and public identities retained their exact
hashes. Executable and release-lock hashes are unchanged. The nonzero state
comparison records those supervisor changes; the command independently failed
its semantic-readiness requirement.

The current plan remains
`0x17e49d00a7ce6aafac856e81a4ccf9eb37e4714ba7570a24cfa1c97a2d941f37`.
The [request](strict-resume/REQUEST.json) binds the qualified source and exact
operands. The [earlier launch handoff](../FINAL-2-launch-handoff-r3-20260916/README.md)
retains the completed import, epoch-1488 history bundle and helper teardown.
The release-candidate campaign did not start; final acceptance remains false.

Operator transactions and validator history require separate reconciliation.
An unchanged simulator journal does not establish that managed operators made
no chain writes. No funding, replacement transaction, database-status edit or
history rewrite was performed while packaging this evidence.

[SOURCE-FILES.json](SOURCE-FILES.json) maps every byte-identical copy to its
closed source and hash. Large/private deployment state and signing material
are omitted; watched files remain identified by their original hash receipts.
Original command files are inert evidence and must not be rerun from this
package. SHA256SUMS covers every payload except itself and its own seal.
