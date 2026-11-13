# Public mainnet RPC readback, 2026-10-01 13:56 UTC

The reproducible `sn-mainnet` binary from the SN `233ea2be` / server
`94229abb` release ran `finalized-snapshot --rpc
https://entrypoint-finney.opentensor.ai --retry-window 120s` and wrote its
complete JSON output to
`/mnt/data/sn-testnet/mainnet-public-snapshot-20261001T1400Z/finalized-snapshot.json`
(5,824,893 bytes; SHA-256
`9b4cc2971e341b72a42433b735c0c7ca4e5069620f1e94423725a4bf298a890b`).
The command exited 0. The output labels itself `unapproved_observation`.

At finalized native block 9,188,367, hash
`0xb94f3d0526974a1c67d4ee07ed7b3899fe3c3d54e9d97a6de016a35ab1e43493`,
the endpoint reported Bittensor genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
EVM chain ID 964, runtime spec/transaction version 470/1, code hash
`0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`
and metadata hash
`0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`.
The decoded Frontier post-log hash and same-height canonical EVM header both
equal `0xbe73c3f66cf5a34eafb341995c98f53db3810004b314ac70c4da4e0fa0108d3b`.
These runtime identities match the earlier public readback; the finalized
block advanced.

This is one endpoint's finalized RPC assertion, not an independent genesis,
runtime-source/Wasm, GRANDPA or storage-proof approval. It does not authorize
signing, deployment, a recycle-mode transition or activation. Repeat admission
against the exact approved route and independently reviewed source before any
mainnet action.
