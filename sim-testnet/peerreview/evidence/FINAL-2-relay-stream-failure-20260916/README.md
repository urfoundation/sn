# Relay continuation capture failure

The native capture ran 2026-09-16 04:35:04–04:48:52 UTC. Its body, wrapper
and joined session all exited 1. `stdout.json` is empty: no continuation plan
was emitted or imported. All six watched state files, the executable and the
release lock are byte-identical before and after the command. No transaction
was submitted; the preceding successful setup/adoption remains retained.

`capture/stderr` records request deadlines, incomplete authenticated attempt
stream EOF and cancellation. Both operator APIs reported the same metadata
object at 04:48:52 UTC:

`0x74d265dccdbcee7a218e229abd199708e15f0035481cc5ec8d0b55f2bb5ed273`

The API logged 65,536 bytes written for operator 1 and 32,768 for operator 2,
each after approximately 30.0025 seconds. `operator-observations/` contains
the exact four-line failure excerpts from each helper's log. These counters
are server observations, not proof that the client authenticated those bytes.
The excerpts do not establish whether the delay originated in artifact
storage, transport or consumption.

Terra then performed one bounded direct read from each live localhost API.
Both returned HTTP 200 and the exact 157,602-byte object: operator 1 in
0.683512 seconds and operator 2 in 0.386878 seconds. The bodies are identical
and their SHA-256 matches the requested hash. `diagnostic/RESULT.json`, raw
headers, bodies, timing, exit files and unchanged source records preserve
that observation. The diagnostic's original absolute-path hashes remain
original records; this bundle's outer manifest checks all copied files.

Two source reviews found no deterministic caller, body-ownership or connection
capacity defect. Metadata is consumed immediately; each object has a fresh
30-second I/O budget. The server's streaming deadline is ten minutes, and its
reader-slot limit would reject with HTTP 429 before streaming. Those findings
do not identify the original stall's underlying cause. No source patch,
timeout increase or authentication bypass was introduced. A fresh-window
capture using the same qualified executable started at 04:59:42 UTC; its
outcome is separate and is not included in this failure bundle.

The failed request selected end block 8,023,974 under the existing full
7,570-block work budget. It does not authorize a new plan or prove that a
later retry has sufficient time remaining. Capture a fresh window late after
the cause is resolved. Saved plan
`0x0d24a3f1dfc8ea5bc6a2f59c80a7580a761d9e3410dd4833bda3843304ce6f86`
and all approved limits remain unchanged. The supervisor and soak are stopped.

Actual chain RPC used `192.168.1.162:9944`; `independent_rpc=false`.
These are local failure receipts, not new on-chain acceptance. No private
keys, private helper configuration or complete private logs are included.
Run `sha256sum -c SHA256SUMS` from this directory to verify the bundled bytes.
