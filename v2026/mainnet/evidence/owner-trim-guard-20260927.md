# Owner trim recheck and reconciliation qualification

The signer-free [guard commands](../OWNER-TRIM-GUARD.md) rebuild a retained
partial plan from its authenticated historical census, compare a fresh
finalized census, and reconcile exact old generations, protected survivors,
compressed UIDs and the excluded root census. All execution and full-reset
flags remain false. A matching observation is never a signing/broadcast token.

Qualification used an isolated branch from
`6dbbe4fecc632a7769e394d7c695295ce6b16fdc`. The complete
[external evidence bundle](/mnt/data/sn-testnet/evidence/mainnet-owner-trim-guard-20260927/RESULT.md)
retains commands, raw test logs, source hashes, exact reviewed runtime bytes,
the final commit and a checksum manifest. All 34 entries verify; the manifest
SHA256 is `7bfaa559c4a6ff4cff2069e1436e63069b5b0487911ee2719af32f2899eb3d66`.

| Check | Result |
| --- | --- |
| Final full mainnet package | 210/210 root tests and 41 subtests pass; 119.284s |
| Final identity/historical guard race coverage | 16/16 roots and 14 subtests pass; 94.359s |
| Final remaining subnet/storage race coverage | 8/8 roots pass; 32.358s |
| Final `go vet ./mainnet` | exit 0 |
| Integrated branch monitor/owner-trim/identity/storage/bootstrap selector and vet | pass; 50.373s for selected normal tests |
| Earlier broad race build | 85/93 roots passed before the default ten-minute package timeout; no assertion failure or race report. The eight unfinished roots are the completed final-source run above. |

The broad race build has separate source hashes. Final review reproduced and
fixed an omitted historical-header field inheriting a previously decoded
current-header value. All four causal negative cases failed before the fix and
pass in final normal/focused race qualification. This is not a claim that the
entire broad race selector was rerun on the final bytes.

Other deterministic cases cover changed emissions, natural immunity expiry,
stale head and metadata, owner/registration generations, protected validators
without permits, owner identities outside runtime immunity, custody roles,
concurrent registration, old-hotkey reentry, incorrect survivor compression,
lost residuals, root changes, tampered/rehashed plans and interrupted rechecks.
An expected old miner residual survives a successful partial reconciliation;
absence alone never establishes the transaction that removed an old generation.

No live RPC observation, signer, transaction, deployment, merge or push was
performed. The reviewed native call accepts only capacity and has no atomic
predicate enforcing the approved identity set at dispatch. That execution
gate remains open, as do live mainnet approval/source-to-Wasm provenance,
owner custody/submission, exact receipt/uncertain-attempt recovery, full custody
and economic effects/history audits, cooldown/reentry controls and approved
dispositions for residual miners. Root-owned status documents were not edited.
