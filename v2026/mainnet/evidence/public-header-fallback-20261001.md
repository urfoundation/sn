# Public archive finalized-header fallback, 2026-10-01

The Rao archive does not expose `debug_getRawHeader`. Source commits
`89b17cd1` and `91df126f` add a bounded fallback only for the exact finalized
native/EVM mapping read: use `eth_getBlockByHash`, reconstruct the reviewed
Frontier RLP15 header and accept it only when Keccak-256 of its complete bytes
matches the EVM digest committed in the native finalized block. The raw method
remains primary. Missing, corrupt or unexpected raw replies do not invoke the
fallback, and Safe history still requires its separate raw-header/body/receipt
capabilities.

Independent Sol qualification of the successor passed 38 focused roots in
normal and race modes, vet, and three causal refusal controls. The initial
candidate's one failing fixture-order test and logs were retained; the
successor changes only that fixture. The evidence handoff is
`/mnt/data/sn-testnet/public-rpc-fallback-sol-qualification-20261001/evidence/HANDOFF.md`;
its 77-file `SHA256SUMS` has SHA256
`91f5574faa41b6d8908f56b38fa4c1998cd39e1b001e9326d59604ea1ea833d5`
and verified with exit 0.

A live read-only `finalized-snapshot` against
`https://archive.chain.opentensor.ai` exited 0. Its final JSON SHA256 is
`7691e41ff840f14c84321f1b9880eb3f7c6fadd35aa5f3b13dc4ac6d60204e93`,
content seal is
`sha256:94e394a80b6a989e0733baa5c3d66338d42deef25b9dc94cb971bf7850fb18ee`,
and native finalized block is 9,184,711. Independent replay checked the seal,
runtime hashes, exact RLP hash and identity. This is a route/capture result,
not approval of observed runtime spec 470 or permission to sign or submit.
