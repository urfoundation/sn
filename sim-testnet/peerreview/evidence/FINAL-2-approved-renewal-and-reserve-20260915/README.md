# Approved renewal allowance and finalized reserve repair

This bundle separates public chain observations from local preparation
receipts. All new RPC observations use `http://192.168.1.162:9944` with no
RPC pacing; `independent_rpc=false`. No pending signed transaction, secret,
database dump or complete private renewal plan is included.

The user approved **205 EVM TAO within 225 total TAO**, permitting one fleet
renewal capped at **13.13 EVM plus 0.606 native TAO**. Alpha remains bounded by
**37,250 lifetime / 6,000 per repair**. [approval.json](approval.json) records
that authorization before adoption; its `plan_adopted=false` is historical.
The later [allowance-adoption.json](allowance-adoption.json) records the
successful native adoption of plan
`0x922e280318f33cb20f5b15082bb6329890e9d8778f1effabf8baae521a57f4ea`.
Vault commit `9651a13062af2fd25dcd9e98db8c8871d148d114` publishes the setting.

## On-chain repair and reserve

[repair/README.md](repair/README.md) identifies the finalized repair
extrinsic, native block and parent, plus the separate probe deployment's EVM
receipt. Hash the repair's exact SCALE extrinsic bytes with BLAKE2b-256 to
reproduce `0xb6468a8c03886ef4d3c207ba08348b3219267b90d7e569b82d9a81b9da96ebed`.
Its inclusion index is 6 in native block 8,009,634. Parent/block storage gives
an equal source debit and reserve credit of 6,000,000,000,000 alpha-rao.
The aggregate hotkey stake and the destination coldkey's stake are different
quantities; retain their separate labels. Native and EVM block hashes also
have distinct domains, including when their block numbers are equal.

[reserve-census/SUMMARY.json](reserve-census/SUMMARY.json) records the complete
256-UID census at native block 8,010,632, hash
`0xf8091ff2ce7a4401b4dcbd312de593e174022ade3d4804870b77553446a0686e`:

`100 × 59254248215327 / 90326976937105 = 65.59972471633357868307494040536747262997%`

The 248 absent `TotalHotkeyAlpha` values are zero under the pinned runtime's
`ValueQuery`/`DefaultZeroAlpha` storage definition. The raw key list and RPC
responses are included. This is a passing preparation snapshot for the 65%
target and 60% floor, not proof of the future campaign's final reserve share.

## Local preparation and its limits

- [allowance-adoption.json](allowance-adoption.json): all nine hard setup
  preparation checks passed; all 3,449 carried actions were authenticated.
  Only the approved plan and redacted configuration changed among the six
  recorded campaign-state files. No chain action was dispatched.
- [resume-preparation.json](resume-preparation.json): complete strict resume
  preparation passed all 18 hard checks at 13:58:20 UTC, including 1,000 fleet
  records, all 3,449 carried actions, both validator namespaces and host/runtime
  prerequisites. All six recorded state hashes, executable and release-lock
  hashes were unchanged. It stopped before action dispatch or topology startup.
- [renewal-preview.json](renewal-preview.json): 202 fleets and 2,020 new
  actions fit the approved 13.13 EVM / 0.606 native ceiling. Total native
  planning bound is 211.960236 TAO; EVM maximum 192.2495 plus superseded
  allowance 12.7505 equals 205. These are planning bounds, not actual spend.
  Its F388/T419 window is diagnostic and must not be adopted as the final
  renewal window. [renewal-existing-balances.json](renewal-existing-balances.json)
  records the separate point-in-time keeper/oracle balance observation.
- [continuation-preparation.json](continuation-preparation.json): the earlier
  current-build retained-evidence and nonce preparation completed successfully
  on its stated source plan. Its end block 8,019,194 is diagnostic only. Actual
  final continuation and native history requests remain late-bound after the
  real renewal.

The local receipts are asserted preparation evidence. They do not claim
independent chain reproduction, a passing full gate, a running soak or final
acceptance. The source directories named in receipts are provenance locators;
the public RPC requests and responses needed for the chain checks are copied
into this bundle. The complete private plans remain in their original custody.

## Reproduction

Check the portable checksums from this directory:

```sh
sha256sum -c SHA256SUMS
```

An archive node is required for the historical state. Replay each included
request against the approved LAN node; compare responses by JSON-RPC id:

```sh
SN_RPC_URL=http://192.168.1.162:9944
curl --fail-with-body -sS "$SN_RPC_URL" -H 'Content-Type: application/json' \
  --data-binary @repair/anchors.request.json
curl --fail-with-body -sS "$SN_RPC_URL" -H 'Content-Type: application/json' \
  --data-binary @repair/transfer.request.json
curl --fail-with-body -sS "$SN_RPC_URL" -H 'Content-Type: application/json' \
  --data-binary @reserve-census/census.request.json
```

The saved `chain_getFinalizedHead` response records the original observation
time. A new head query will naturally differ; the historical requests pin their
actual block hashes. Keep freshness observations separate from those pins.
