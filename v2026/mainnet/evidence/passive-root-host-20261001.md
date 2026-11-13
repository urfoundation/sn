# Passive-root static host qualification — 2026-10-01

This increment supplies the separately approved process owner for the existing
bounded passive netuid-0 observer. It retains original v4 preparation and both
UR inspections, verifies a fresh current root policy before an initial start,
and owns one exact static-unit invocation with durable recovery. No live unit
was installed or started; independent mainnet approvals and deployment remain open.

The source commit is `4c439e6e1ca999a29df9a680a630886c4f36b697`, tree
`a87726114b8435d2fddbbec526e010a49759902e`, based on SN `6c801a25`.
The final review tip `cbd7f5359ebb561050b3cf5ae86f395e61af8e77`, tree
`2cd336aab955969fa3c5cd11638ac9f586808214`, adds two tests only; production
bytes are unchanged. Qualification uses the external `author.go.mod` with clean
server `720e7c61182983dd2cd6de667787bb5b52f4d8a4`, tree
`4418c879d562fdf2ec2b9c5e10eebda3c6aede9b`, immutable Connect
`v0.0.0-20261001021459-e1b5d77b5029`, SDK
`v0.0.0-20261001021058-5d37be3876e5`, and this SN tree's GSRPC/npipe forks.
The default legacy server checkout was not the qualification dependency.

Author evidence is retained at
`/mnt/data/sn-testnet/mainnet-passive-root-host-astra-20261001/receipt.json`, SHA-256
`706abf53b9b20b15bf87aac5d99aa6ed6bf0f9110fdf5563221228ab27f063f9`.
Sibling `SHA256SUMS` binds the original JSONL streams, graph/modfile, source patch,
commands, causal overlays and audit helper. All 45 entries passed readback;
manifest SHA-256 is
`dadc39e398287a924723624570f3a8c2fa3dcc345ff25a9cb07570674dd4547e`. The
receipt records author qualification only; independent execution review remains
separate and must pass before integration.

| Gate | Result |
| --- | --- |
| Frozen-source normal | 39 passing top-level roots/package, 90.812 s; new host, passive runtime/preparation, existing validator repair and seven selected UR host roots |
| Frozen-source race | All 11 new top-level roots/package, 258.354 s |
| Test-only follow-up | Two roots/package pass normal 6.206 s and race 33.364 s |
| Vet | `./mainnet` exit 0 at both source and final test tip |
| Deterministic causal controls | Three normal and two selected race omissions fail at their intended assertions; no compilation/infrastructure failure is counted |

There are 54 positive root executions in the terminal author gates. Development
logs `initial` through `fourth` retain failed fixture/implementation diagnostics;
`fifth.normal.jsonl` is an initial positive run before the final source freeze.
They do not replace the terminal gates above.

Independent qualification on the exact final SN `cbd7f535` tree and server
`720e7c61` tree passed 20/20 selected roots in normal and race modes, with zero
failures or skips, and `go vet` exited 0. The selection includes all 13 new
passive-host roots and seven adjacent UR installation/recovery roots. Static
review found no custody, start, or sandbox blocker. The sealed receipt is
`/mnt/data/sn-testnet/sol-passive-root-host-independent-20261001/receipt.json`
(SHA-256 `75b9d8bcda1bad2f7661242fc7d121527afc8ff070aa86b20379106ddbead183`);
its `SHA256SUMS` is
`6fe117b0a25d6dbe85f0ad5b1ac19280cd125b59e2ba72ba8194e84d23237f82`
and all entries verify. This is source qualification, not a live service start.

The tests use real signed synthetic v4 inputs, private files and durable stores,
plus the actual passive RPC reader against local synthetic finalized state.
Only the manager transport and host filesystem root are fixture ports. The
positive path also runs the actual passive service with the exact original
runtime and dedicated checkpoint, while keeping preparation journals byte-for-byte
unchanged. It distinguishes manager acknowledgment, current process liveness,
historical status and continuing observer health.

Recovery tests retain an ambiguous consumed start across reopen and deletion,
reconcile exact partial installation, refuse conflicting files and changed
original authority, reject omitted/changed manager fields, and bind the same
invocation ID/PID/monotonic start. They refuse wrong runtime, checkpoint aliases,
hard links, symlinks and foreign directory entries. Repeated `resume` exhausts
its signed operation budget without issuing another start. Explicit post-rename
hooks force publication ambiguity and expiry after durable reservation.

The direct omission controls establish the causal boundaries:

- Removing the pre-start durable save reaches one synthetic systemd start after
  publication ambiguity; the corrected source records the consumed liability
  and makes zero starts.
- Removing hard-link checks admits a checkpoint alias to the original private
  runtime; the corrected source refuses it before claiming host state.
- Removing loaded sandbox checks admits an omitted `ProtectSystem` property;
  the corrected source refuses before consuming a start.

[ROOT-PASSIVE-SERVICE.md](../ROOT-PASSIVE-SERVICE.md#independently-approved-static-host-owner)
describes the exact independent host envelope and command sequence. The root-run
unit preserves private authority bytes and has an empty capability set plus a
read-only filesystem, with only a dedicated checkpoint directory writable.
Linux/systemd and privileged custody remain trusted. Current process readback
never sets `root_service_ready` or `activation_ready`, and historical `status`
never labels a process currently running.

Still external: independent source/runtime approval and the documented runtime
rebuild exception, a real approved existing root seat/stake/generation, fresh
v4 passive config/signature, the exact independent host signature/acceptance,
qualified successor binary, protected host paths/boot/systemctl, the approved
RPC route and live observer health evidence. The two UR current-admission and
custody approvals, complete original contract installation/history, operator
readiness and economic outcomes remain separate. The production release fenced
at SN `6c801a25` predates these source commits; its artifacts cannot attest this
implementation. No native signing, transaction submission, image publication or
service deployment occurred in this qualification.
