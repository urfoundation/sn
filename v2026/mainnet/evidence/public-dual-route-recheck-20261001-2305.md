# Public mainnet identity readback at 23:05 UTC

At 23:05 UTC on 2026-10-01, a signer-free probe selected Rao archive's
finalized block 9,191,112
(`0xf6b417e2be09bcf68185cfaaf2e93bf7a7d4a6ac60fddb85116b26eeed61cd71`).
It queried that exact block on both `https://archive.chain.opentensor.ai` and
`https://entrypoint-finney.opentensor.ai`. Both routes returned the same pinned
header, genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
EVM chain ID `0x3c4` (964), runtime spec 470, and `:code` storage hash
`0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`.
The two routes' independently sampled current finalized heads differed during
the probe; the comparison uses the single archive-selected block instead.

The raw JSON-RPC response capture is
`/mnt/data/sn-testnet/mainnet-public-identity-20261001-latest.json`, SHA-256
`12251424ad4873127de13de91e95a5125f2fec3a954a4801db3c6e96b32941d9`.
The probe used `chain_getFinalizedHead`, `chain_getBlockHash`, `chain_getHeader`,
`state_getRuntimeVersion`, `state_getStorage`, `state_getStorageHash`, and
`eth_chainId`. Its `:code` byte SHA-256 is
`e5abec692e3988352da818823d9729f139820e205ea17048f93816974106c005`
on both routes. No signer, secret, submission method or application service was
used.

Rao's [v471 release](https://github.com/RaoFoundation/subtensor/releases/tag/v471)
remained marked proposed at this check. These public routes may share upstream
infrastructure. Their agreement is not an independent GRANDPA proof, approval
of genesis/runtime source, or authority to sign, deploy or activate. Repeat
current finalized checks before a separately approved live action.
