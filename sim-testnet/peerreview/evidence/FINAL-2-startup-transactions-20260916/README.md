# Four transactions from the failed managed startup

The September 16 startup failed its process-log gate, but both operator
taskworkers had already submitted transactions. These receipts preserve that
partial progress for [report 2](../../../FINAL-2.md) and recovery.

All chain requests used `http://192.168.1.162:9944`; `independent_rpc=false`.
The read-only database projections completed at 06:58 UTC. Receipt queries
completed at 06:59:27 UTC and canonical/finality queries at 07:00:26 UTC.
Every recorded command exited zero. This collection submits no transaction
and does not update the retained operator databases.

| Transaction | Call | Block | Original intent / attempt status |
| --- | --- | --- | --- |
| `0xa62a69f38c314fce4efdc6cf9084ae5a2efa7178faca0ba25b0c63bd8e5368d9` | `finalizeOperatorEpoch(316,1)` | 8,016,489 | mined / mined |
| `0x50cc7f2745b22052f91a3ce124f18fcbbe0fb0d297f59a9f3f9d2bd33e702dd8` | `finalizeOperatorEpoch(316,2)` | 8,016,488 | finalized / finalized |
| `0x62a2464cf084c69c81040d1374abcc20c8a99bb34e4788bd6e8b34cbe804d1df` | `deferMissedEmission(402,2)` | 8,016,491 | broadcast / broadcast |
| `0xe95f3f78dad211d12ced0583a47d9af610b9699d68ab08f73b2fc5c3b7acabd8` | `deferMissedEmission(317,2)` | 8,016,491 | broadcast / broadcast |

The [receipt response](receipts/rpc-receipts.response.json) has `status=1` for
all four transactions. Each sends zero value to coordinator
`0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8`. The first two each use 105,299 gas;
the last two each use 99,930 gas, all at 20,134,283,587 wei per gas. The total
actual fee is **8,264,277,772,552,846 wei = 0.008264277772552846 EVM TAO**.

Both finalize receipts contain vault `RootMissed(316,noId,0)` and coordinator
`OperatorEpochFinalized(316,noId,false)`. They add zero to carry and create no
payout root. Both defer receipts contain vault `EmissionDeferred(epoch,2)`;
they record zero-funded missed boundaries while leaving pool stake for a later
timely capture. They do not capture a multi-epoch stake delta. See
[coordinator methods](../../../../evm/src/STCoordinator.sol) and
[vault transitions](../../../../evm/src/STSettlementVault.sol), unchanged from
SN source `0fd7ffc0f6aaad5a2988427c4acb47e1f79c3819` when decoded.

The [canonical response](receipts/rpc-canonical.response.json) verifies each
inclusion block by number and transaction membership. All are below native
finalized height **8,016,641**, hash
`0x7ee9498db1d26737986ceca77279f9de6a1a41c6780556e6a7d75421b64663fb`.
The corresponding EVM hash is
`0xa2657bf34431dbd71f5c1a75b3dbf93e513e3581c5b426cc615db8c66289d635`, also
bound by the native header's Frontier digest. Native and EVM hashes are kept
distinct. Chain ID is 945; native genesis is
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`.
Runtime versions are 460/1/1. Both signers' finalized and pending nonces are 107.

## Recovery and evidence limits

The [original reconciliation](receipts/README.md) contains the full source and
actor analysis, including validator history and claim queues. It is preserved
byte-for-byte, so its absolute paths identify the original local evidence;
they are not portable repository links. The SQL projection records only public
transaction identities and statuses, omitting signed bytes, credentials,
calldata and error text. The RPC responses retain public on-chain transaction
and log data.

The unchanged validator intent prefix and durable claim records support no new
native steering or claim transaction in this startup. There was no complete
pre-start claim-queue hash baseline; that conclusion uses durable records and
submission ordering, not a byte-for-byte comparison of every queue. It does
not negate the four operator transactions above.

Recovery must retain the original intents, nonces and signed attempts and let
the production reconciler adopt their finalized receipts. Some database
statuses lag the chain; this evidence does not claim those rows are already
updated. Do not allocate replacement actions or manually alter database status.
These transactions do not establish a completed campaign or acceptance epoch.

## Reproduction

From this directory, verify the copied evidence and replay the read-only chain
queries against the owned node:

```sh
sha256sum -c SHA256SUMS
curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary @receipts/rpc-receipts.request.json \
  http://192.168.1.162:9944
curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary @receipts/rpc-canonical.request.json \
  http://192.168.1.162:9944
```

Historical receipt, block and pinned-state results should reproduce; the
requests also include moving head/pending observations, which may advance.
Historical state queries require an archive-capable node. The original SQL
and its projections are retained in `receipts/`; rerunning SQL later observes
the database at that later time rather than recreating the original snapshot.
