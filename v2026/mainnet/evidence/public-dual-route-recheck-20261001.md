# Public mainnet runtime and identity recheck

At 21:43 UTC on 2026-10-01, a read-only probe selected Rao archive's finalized
block 9,190,703 (`0xc6c4a2fa6925175fe3bbf9110e77fb33b867badb05de86e24d15a60f669553e8`).
It queried both `https://archive.chain.opentensor.ai` and
`https://entrypoint-finney.opentensor.ai` at that exact hash. Both routes
reported the selected hash at number 9,190,703, the same header, genesis
`0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
EVM chain ID `0x3c4` (964), runtime spec 470/transaction version 1, and
`:code` storage hash
`0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`.
Each route reported the selected block as its current finalized head.

The raw response capture is
`/mnt/data/sn-testnet/mainnet-identity-recheck-20261001T2143Z/capture.json`,
SHA-256 `167aa0996d29dd6c37fdf30818e06465535efa6a60ffad3501ae952c4f84e0a4`.
The bounded, read-only probe is alongside it as `probe.py`, SHA-256
`c25defc121eaf332c490d4a0a2c9a76975b2e3b4e1f3dac348e8a48dafe00586`.
No signer, submission method, account secret or application service was used.

Rao's [v471 release](https://github.com/RaoFoundation/subtensor/releases/tag/v471)
was marked proposed at this check; this observation supports only that both
queried routes still served runtime 470 at the selected finalized block. The
routes may share upstream infrastructure. Their agreement is not an independent
GRANDPA proof, approval of the genesis/runtime source, owner custody, or
authorization to sign, deploy or activate. Recheck the finalized identity before
any separately approved live action.
