# Read-only mainnet route check, 2026-10-01 11:24 UTC

The frozen SN `095a2208` release binary completed `finalized-snapshot` against
`https://entrypoint-finney.opentensor.ai` with a 120-second retry window. Its
result is an **unapproved observation** at finalized native block **9,187,604**,
hash `0x77973dd985a9317b266dea677afeb656ce4708c49dfd00d555990c2ecda4d4f1`.
The joined EVM header is block 9,187,604, hash
`0x2116f87c3a1b58e7ed984a67392e25316e1fa6be88c8858cfb4cd931e82e9332`.
The observation reports Bittensor genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
EVM chain ID **964**, runtime spec **470**, code hash
`0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`,
and metadata hash
`0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`.
These identity fields match the earlier pinned public observation; this does
not provide independent approval or source reproducibility.

The same read-only route check at 11:24:27 UTC returned HTTP 502 for
`system_chain`, `eth_chainId`, and `chain_getBlockHash(0)` on Snow's VPN route
`http://172.28.208.185:9944`. The public entrypoint returned HTTP 200 with
`Bittensor`, `0x3c4`, and the genesis above for those methods. A 502 is an
observed route failure, not a conclusion about Snow's chain state or sync.

Raw evidence is retained at
`/mnt/data/sn-testnet/mainnet-current-readonly-20261001/latest/`:

| File | SHA-256 |
| --- | --- |
| `snapshot.json` | `df344a35672f81de2fc97ed0aaca993c462ac26e9a73df5deae3b748e2779253` |
| `route-check.json` | `c1fad92efd7015b48490bbfa3995a69add2ba2379b5fb920e062421b58836592` |

At 11:40 UTC, `state_getStorage` at the same finalized hash for the reviewed
`RecycleOrBurn[25]` key
`0x658faa385070e074c85bf6b568cf055530823bc1353bfe6cb262413a5bd7a0231900`
returned `null`. Under the [reviewed metadata fallback](recycle-mode-observation-20261001.md),
that means **Burn** at this pinned block. The raw
`recycle-storage.json` has SHA-256
`584267c6150324736a6ac521c29c0d1192cf7a0bf3422c3752cb9fff1ef93661`.
This is still an unapproved observation, and the owner must select and verify
Recycle before the requested 90% recycling can operate.

This check involved no signing, transaction submission, or service change.
The public route remains an observation input; launch still requires separate
mainnet identity/runtime approval and a current-source release.
