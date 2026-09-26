# R47 measured-interval on-chain checkpoint

R47's signed measured interval began at finalized Subtensor block 8,090,674. The two epoch-650 `RootCommitted` receipts finalized at block 8,090,680 on chain 945, while the first measured epoch (651) was running. `onchain-epoch650.json` preserves the full receipts and coordinator logs read from the LAN archive RPC. The collection checked receipt status, epoch and operator event topics, and the event's payout root and artifact hash against the owner observation.

| Operator | Transaction | Payout root | Artifact hash |
| --- | --- | --- | --- |
| 1 | `0xc355046e79838c6f60ac9cb1fa79f9e38fd817cdf8365f8ea25e4ca963a68282` | `0x599c1a61d41cbfdbf4871c182f87bd7905b079761787fd6b5e74df4d1ed4e0d1` | `0x97321c15c64df89f287a643f4aef0773f405c9dba6c1b4e5cb615ea8bf88cf94` |
| 2 | `0xcf487e50bc4021013793bef165097e5ccb8ec766bfa0bd4445c0ca8cfe1beac3` | `0xb5c72ff4240e7bff54070d53a5b10b7a82f47bbde2f14f2d72eaea02fd5dd38e` | `0x9f20f608ba1b00cfdc8d949567d25de75d51614638aadb7a9d3ac07f78dae180` |

The Substrate block hash and EVM block hash for height 8,090,680 are different chain-view identities; both are retained in the JSON. A reviewer can independently call `eth_getTransactionReceipt` with either transaction hash and `eth_getLogs` for coordinator proxy `0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8` over block `0x7b7438`. The `RootCommitted` event topic is `0xeca8a9321e98d9973e8f088688c773bd76fbd6b6cd7212fbd5f2f9128eda8805`; its indexed values are epoch 650 and operator 1 or 2. The first two data words are the payout root and artifact hash.

These receipts prove commitments only. Source usage of 52,450,233 and 50,903,383 bytes, the native-margin check, fault execution, funding, settlement, and terminal acceptance need separate evidence in `FINAL-3.md`.

The later `epoch651-rolling-observation.json` is the exact owner JSONL row at
byte offset 50,551,522, observed 2026-09-26 14:21:06 UTC. It reports epoch
651, finalized block 8,090,848, rate readiness, valid fleet bindings, and
the first rolling restart among expected faults. Its SHA-256 and owner
observation hash are in `epoch651-rolling-onchain-receipt.json`. That receipt
also retains the independent LAN `eth_getBlockByNumber` response for
`0x7b74e0`; the EVM block hash matches the owner row exactly. The RPC proves
the block identity, while the rate, fleet and fault fields remain owner
observations requiring terminal cross-checks.
