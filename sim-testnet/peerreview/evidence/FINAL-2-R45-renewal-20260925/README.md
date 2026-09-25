# R45 round-7 renewal and retained continuation

These files are a portable snapshot from 2026-09-25. They support the renewal,
preparation and signed release-boundary claims in `sim-testnet/FINAL-2.md`;
they do not establish successful finalization.

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
- `publication-capacity-advisory.txt`: exact nonblocking provisional forecast
  advisory from the active R45 service journal at 13:54 UTC. The run's pinned
  publication limits remain unchanged; this is an open terminal risk.
- `relay-forecast-advisory.txt`: exact 13:58–13:59 UTC elapsed-horizon and
  pending-public-census advisories. The public census is deferred, not waived
  for final audit.
- `blob-early-census-summary.json`: complete bucket-listing summary at
  14:07 UTC. The 38,669,627-byte listing is retained externally at
  `/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/storage-census-early-20260925T1400-retry/blob-object-census.jsonl`
  with SHA-256 `aaa37e5cce7c6e965908cfa8f963da8c76de79a8ca996ea83f9509ed1efa00d0`.
  Its 13:38–14:00 window precedes renewed-binding activation and cannot prove
  the active-epoch storage margin.
- `governance-startup-advisory.txt` and `governance-readcut.json`: owner log
  showing the provisional startup skip and a later read-only observation that
  the expected public drill record was absent. This is a current acceptance
  gap, not an exception that makes the assertion pass.
- `public-census-audit-result.txt`: the active owner's 14:32 UTC completion
  log for its parallel read-only public evidence relay census. This is a
  preparation check; final acceptance remains false.
- `public-census-audit-readcut.json` and `public-census-audit.journal.jsonl`:
  exact systemd journal cursor/PID and read-only binding to the signed R45
  attempt, plan and binary. The pinned worker emits no standalone signed audit
  completion receipt, so this preserves the log observation without claiming
  an acceptance signature.
- `binding-activation-lan.json` and `binding-activation-{quota,storage,usage-cached}.json`:
  finalized LAN block 8,083,774 and contemporaneous read-only MinIO admin
  state. Bucket usage is explicitly cached; activation does not itself prove
  live fleet-binding validity or acceptance.
- `blob-activation-census-summary.json`: 14:50 UTC full bucket inventory. The
  underlying read-only 38+ MB listing is retained at
  `/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/storage-census-active-start-20260925T1450/blob-object-census.jsonl`
  with SHA-256 `2805ad102d5d5f9c85159785841ca054934eb7a8cb0fc263fcc384042bccaeb5`.
  Its first four activation minutes do not prove a complete active-epoch rate.
- `first-postactivation-observation.json`: exact owner observation line 11,
  observed at 14:50:01 UTC on finalized block 8,083,792. It reports healthy
  supervisor, all 808 bindings valid, and zero usage for complete epoch 627;
  it does not bind an acceptance interval.
- `epoch627-zero-root-block8083834.receipt.json`: read-only, exact canonical
  LAN-RPC calls at finalized block 8,083,834, after the normal root deadline.
  Both operator root commitments for epoch 627 are zero. This explains the
  provisional rate-deferral compatibility finding; it does not prove an
  acceptance start or amend R44's scoped exception.
- `epoch628-committed-roots-block8084091.receipt.json`: exact canonical
  LAN-RPC calls at finalized block 8,084,091. Both operator epoch-628 payout
  roots and artifact hashes are nonzero, committed at block 8,084,080. This
  does not independently establish the owner's rate threshold or acceptance.
- `acceptance-baseline-observation.json`: exact owner observation line 24 at
  finalized block 8,084,084, matching both epoch-628 artifact hashes to the
  commitments and recording a provisional low-usage deferral.
- `campaign-start.evidence.json`: owner-signed release-boundary envelope at
  15:52:25 UTC, binding that baseline to five measured epochs 630–634,
  start block 8,084,374 and terminal block 8,086,024. This is a start record,
  not a final result.
- `blob-epoch628-census-summary.json`: read-only full-bucket listing summary
  after the first complete renewed-binding epoch. It counts 428 retained new
  objects and 244,049,506 bytes during 14:46:05–15:51:00. The full listing
  remains at
  `/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/storage-census-full-epoch628-20260925T1552/blob-object-census.jsonl`
  with SHA-256 `f746220a9b5b0e1e48d329d382f89b292069dbfe99784026695cc6aaffcf4f95`.
  This retained-object census cannot count overwritten, deleted or refused
  writes and does not independently close request-rate acceptance.

`SHA256SUMS` hashes the portable JSON and text files. The live candidate, binary, scripts,
and service journals remain under
`/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/`.
