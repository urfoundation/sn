# R45 round-7 renewal and retained continuation

These files are a portable snapshot from 2026-09-25. They support the renewal
and preparation claims in `sim-testnet/FINAL-2.md`; they do not establish a
release acceptance interval or a successful finalization.

- `round7-apply-result.json`: exact successful renewal result for plan
  `0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e`,
  202 fleets, validity epochs 628–659.
- `round7-journal-summary.json`: 1,212 distinct actions, each with intent,
  broadcast, included, finalized and postcondition-verified entries. It records
  the source journal prefix through sequence 59,907 and its SHA-256
  `a3e9d7dbba6d2c34a257742afba265ffe82015a29ccf175f31268ae574eb7fa8`.
  This is a local journal summary, not 1,212 independent RPC receipts.
- `mirror-1-receipt-rpc.json`: direct `eth_getTransactionReceipt` response
  from owned LAN RPC `192.168.1.162:9944` for transaction
  `0xc424bf210d337f82b70c5e3fb0de42868288797b80a0380ab9324c58e578d044`;
  status `0x1`, block 8,083,027, hash
  `0x56c58bcef74e87e6e9b6447ef88c5ee4f23672b9feb72011f78c0255d4d4fd8f`.
- `resume-result.json`: retained runtime, zero setup actions dispatched,
  provisional and `final_acceptance=false`.
- `recovery-45.evidence.json`: signed recovery-attempt envelope with run ID
  `20260925T134346.277250758Z-release-1.0`. This is a checkpoint before
  acceptance, not a completion record.
- `operator{1,2}-publication-preflight.json`: separate, signed, nonaccepting
  preflight receipts against the pinned HTTP handler with the resumed runtime
  configuration. Each records two POSTs and two exact content/history readbacks.
  The first local invocation lacked the storage hostname variable and reached
  no POST; these are the successful corrected invocations. They do not assert
  acceptance or repair R44's original failed publication assertion.
- `blob-quota-readback.json`: fresh admin read of the 64 GiB hard quota. The
  separate data-usage response remained cached at 13:39 UTC and cannot serve
  as an active-binding growth measurement.

`SHA256SUMS` hashes the portable JSON files. The live candidate, binary, scripts,
and service journals remain under
`/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/`.
