# Runtime 473 and 475 successor-admission fixtures

`runtime473-metadata.scale.gz.base64` and `runtime475-metadata.scale.gz.base64`
are the exact public `state_getMetadata` bytes of two consecutive Bittensor
mainnet runtimes, each encoded as gzip (level 9, zero timestamp) and then
base64. They contain no account state, endpoint, request, key or signature.
Tests use them only for scripted successor-admission transcripts.

Both were read on 2026-10-07 from the snow Finney archive
(`http://172.28.208.185:9944`, genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`) with
read-only RPCs; no transaction was signed or submitted.

| Runtime | Block | Raw bytes | Metadata BLAKE2b-256 | Metadata SHA-256 | `:code` BLAKE2b-256 |
| --- | --- | --- | --- | --- | --- |
| `node-subtensor/473/1/1` | 9,233,529 `0x396606cfbca47c969b45ded222d6726da65acda8805b753bf98134aa233f7724` | 354,718 | `0xa97219740ed3b034a06463c783cd5b794788652e0692c1edb03eed34c8968172` | `054f253ec4e57a441ef79cddb7a9c0d9cd98efffbb8384add69b31315013632a` | `0x7773f5c0a6d6e9ea9ff347edcc491246eec08a5cf441d964ee96f40d7fa65a08` |
| `node-subtensor/475/1/1` | 9,235,045 `0x95438b03739e1740350ebdf9acc52a61d7e39d00f0d71d336aea90b45b2414d6` | 357,468 | `0x983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff` | `e181ddacd13d1050e82a2afea8e58ba5d3fc84504fa92d2884c91df8c9dd8020` | `0x557634c8c31bc639ea6552e297dcfb781cd7d9bdd491a5352c151305db33d3a0` |

The 473 identity matches the [runtime 473 review](../../docs/spec/runtime-473-audit.md).
The 475 bytes are an observation only: this fixture makes no source, build or
review claim about runtime 475 and approves no runtime.
