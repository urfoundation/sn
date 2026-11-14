# Mainnet identity review input, October 2

This is a read-only review packet for MG-01. It does not approve a genesis,
finality checkpoint, runtime, signer, deployment, or transaction.

At 02:36 UTC, the Rao archive selected finalized block **9,192,166** at
`0xfbedfb1a383b97491541c288727fc248886e1fcf1827971f3da7a8e5b4dd3520`.
The archive and public entrypoint returned identical responses for that exact
hash: Bittensor genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
EVM chain ID **964**, runtime spec **470**, metadata BLAKE2b-256
`0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`,
and runtime-code BLAKE2b-256
`0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`.
All six same-block comparison fields in the [normalized readback](public-identity-readback-20261002.json)
are true. Its SHA-256 is
`e8d88f27d2c7b9976022dcccdf7ff722a0914a6018cb2236fab3e21407153fc2`.
Both RPC routes are operated in the same ecosystem; equality is corroboration,
not independent finality.

The exact [official v470 source commit](https://github.com/RaoFoundation/subtensor/tree/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d)
names this value as `FINNEY_GENESIS_HASH` in
[`sdk/python/bittensor/settings.py`](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/sdk/python/bittensor/settings.py)
and as `FINNEY_GENESIS` in
[`node/src/service/grandpa_warp_sync.rs`](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/node/src/service/grandpa_warp_sync.rs).
The locally reviewed copies have SHA-256
`53dba1c0e1b131c5ca25d41137e1533b68fb640e25fb2c59303e43fa0106a2cd`
and `2cb725aba2d8bfb789bb67ed316523955721c989f5748c8625f33244108041d0`
respectively. This source cross-check corroborates the network name and genesis
without deriving a genesis header from the chain spec or verifying a GRANDPA
certificate. The [v470 artifact review](../../docs/spec/runtime-470-audit.md)
records a planning-only approval for the exact observed Wasm and its
reproducibility exception. That approval does not extend to activation.

At 02:35 UTC, Snow VPN `172.28.208.185:9944` returned HTTP 502 for genesis,
finalized head and EVM chain ID. The [route response](snow-route-readback-20261002.json)
has SHA-256 `036edc1673ea5b004d7e66500e358222d7ba606cb9f7c693572f1878e3de6774`.
The local xops `main` source at `42bfe0be2a7a7c51bbda87fb44886424604f509e`
still configures Snow's Subtensor chain as `testfinney`, with testnet genesis
and EVM chain ID 945. The HTTP 502 prevents an inference about Snow's deployed
chain or synchronization state from this sample.

The Rao archive's [02:45 UTC method catalogue](public-methods-20261002.json)
advertises `chain_getBlock` and `state_getReadProof`, but omits
`grandpa_proveFinality` and `grandpa_roundState`. Its SHA-256 is
`0b155ea3cc2bec657939b4b443646a11b1d4e9aebadb0316d3cff15229e825a8`.
The readback does not establish a trusted
finality certificate or independently approved checkpoint. Continue read-only
preparation on the public archive with finite retries and recheck an exact
finalized block before any reviewed signing. MG-01 remains open for independent
genesis/checkpoint/runtime authority and the eventual owned-node cutover.
