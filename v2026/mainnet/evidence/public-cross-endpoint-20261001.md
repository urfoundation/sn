# Public mainnet cross-endpoint readback, October 1, 2026

At 18:16 UTC, a read-only probe selected Rao archive's finalized hash
`0x6cf21da4abcca261a023cf5ecef454948e85dcc291bcb025c9073bdb6b3adbe8`
(block 9,189,666) and queried that same immutable hash through
`https://archive.chain.opentensor.ai` and
`https://entrypoint-finney.opentensor.ai`. Both endpoints returned identical
genesis, pinned native header, complete runtime-version JSON, raw metadata
digest, raw `:code` digest and EVM chain ID. Their retained normalized response
files are byte-identical.

| Field | Both endpoint responses |
| --- | --- |
| Genesis | `0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03` |
| EVM chain ID | `0x3c4` (964) |
| Runtime | `node-subtensor`, spec 470, transaction 1, state 1 |
| Metadata | 354,056 bytes; BLAKE2b-256 `0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34` |
| `:code` | 2,556,358 bytes; BLAKE2b-256 `0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47` |

The retained probe and its complete normalized results are under
`/mnt/data/sn-testnet/mainnet-cross-endpoint-20261001T1815Z/`:

| File | SHA-256 |
| --- | --- |
| `readback.py` | `e20ecda993c68fe874107463cba8136e95f8b1c4b59be02d768eeab7fe927bcd` |
| `rao_archive.json` | `74737b2244b1eb64adcfdf25dfccd12cd1a4d93954064f6182135619b211d314` |
| `official_entrypoint.json` | `74737b2244b1eb64adcfdf25dfccd12cd1a4d93954064f6182135619b211d314` |
| `comparison.json` | `dae70ea80acd0e735d6679b64ae9e46313d70a3387736f3f3a555440be2d9239` |

The script retries transient transport failures up to three times and caps each
response at 20 MiB. It checks exact pinned-header equality, not just moving
heads. This strengthens the prior single-route observation but remains two RPC
assertions: the endpoints may share infrastructure, and this probe does not
independently verify GRANDPA finality, genesis source, deployed Wasm provenance,
or an operator-approved runtime identity. It grants no signing, deployment or
route-switch authority. Repeat the admission at the eventual approved boundary.
