# Public mainnet finalized snapshot, 2026-10-01

The unmodified `sn-mainnet finalized-snapshot` command from SN
`2d53e6f2e44be3d08c0d150f836f163f0eb74442` completed a read-only query
against `https://archive.chain.opentensor.ai` at 03:58:30 UTC. It observed
native finalized block 9,185,377 (`0x073dbfa5b601deb349997b8cd44e247ec76aafc8c4476923c8954bc43dbd97a4`),
mainnet genesis `0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03`,
EVM chain ID 964, and runtime spec 470. The native digest's Frontier hash
`0xd08b443f746a859b54371846348601adf3a5e7b493596471643bd2aa1f075cdf`
matched the reconstructed 15-field EVM header hash. The command exited zero
in seven seconds with no stderr.

The retained JSON is
`/mnt/data/sn-testnet/release-current-sol-20261001/final-rpc-snapshot.json`:
SHA256 `72253d053d9293d7fe3d6f411a7b80d9357d1d7eda1682f58d66490279e5b3d1`,
internal content hash
`sha256:916b157f8b6d18f8a82e68f677290360c9299ee7f6fd6ebc2783e2b61f038eaf`.
Its admission is `unapproved_observation` and its
`mapping.runtime_source_proven` is false. This establishes the public RPC
fallback's operational read, not independent runtime-source approval, a
production deployment, or transaction authority.
