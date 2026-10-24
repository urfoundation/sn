# Runtime 482 selective-metagraph API review

The public finalized Snow RPC observation at block 9,341,171, hash
`0x4f9b6a9d9f9b5e598d912387872c8c943892bf3638cd4c59aa9d9b49c0ee41f0`,
reports `node-subtensor/482/1/1` and the same 25 API version pairs as 481.
The exact captured metadata is 367,676 bytes, SHA256
`937fc71c0783462556cc26fab9da9d771e0c96b657b1cd749fb0d4e4b28d9281`,
BLAKE2b-256 `512a7c0c9d87763428a4c927670f7a01f014ab9b1166d0709cfb08cef280be43`.
Captured code is 2,665,898 bytes, SHA256
`4fd14869c13661c0ea29738a4bdf9181b9a3d7a7149b99c83f4a04e77bcef356`.

Official source tag [v482](https://github.com/RaoFoundation/subtensor/releases/tag/v482)
is commit `8cf229f8ef969389d054a4c161b53ffd164a041f`, tree
`4cfd29d8d12499775e2b1cbabf64e6fa41f1f7cf`. Compared with reviewed481
`6a972693ae9580541225bd37696ac2fee26b8ae9`, selective-metagraph's declaration,
delegate and complete 77-field source layout are unchanged. The existing211-path
source manifest has 38 changed paths; that comparison is not an exhaustive
source qualification.

The API3 exception now admits exactly 481/1/1 and 482/1/1 for node-subtensor.
API2 behavior is unchanged. A deterministic test using the captured482 version
reply reproduced the previous production refusal, then passed after this change.
Tests retain rejection of other names, future specs and transaction/state
versions. Captured482 metadata passes provisional and all seven fleet consumed
interface purposes; selected479/481 API and metadata regressions also pass.
Tests ran in Docker with Go1.27.3 linux/arm64, count1, readonly sources and
separate caches. This is focused interface evidence, not full release qualification.

This change adds no reviewed-native-owner source capability, runtime artifact
authority, economic equivalence, successor opt-in, deployment or signing approval.
Runtime482 also changes miner payout allocation, Null-epoch scheduling, coldkey
swap reservations, proxy handling and new-subnet emission defaults. Those
changes still require broader review, captured-Wasm execution and the applicable
source-bound rehearsals before operational approval.

The rehearsal build entrypoint now pins v482 at the exact commit/tree above,
retains metadata-hash/fast-runtime features, and verifies a217-path source
manifest containing every predecessor entry plus six additional changed paths.
Four deterministic controls execute the actual script through its pre-build
checks: exact candidate reaches only a Cargo sentinel; old481 tag/tree, wrong
spec and changed source bytes are refused before Cargo. No482 native-node build
or full rehearsal result is established by these controls.

The exact captured482 Wasm was also executed with networking disabled on the
previously built481 native host. Code hash, full version/API reply and metadata
bytes matched the capture. Both owned commands joined zero and closed; all20
raw evidence files (12,204,606 bytes) were independently rehashed. This proves
local captured-Wasm interface identity, not economic equivalence or a482 native
build. Evidence is retained in `runtime482-wasm-interface-v1` in the deployment
workspace. No native-owner source capability has been added.

## Owner qualification waiver

On2026-10-22 the owner instructed: “I want to skip the qualification entirely.
Just move forward.” Full release qualification, the482 node build and rehearsal
battery are waived, not passed. The source capability catalog now admits exactly
482 source `8cf229f8ef969389d054a4c161b53ffd164a041f`, with the corresponding
spec binding; other sources and future specs retain their refusals. Added source
binding regression tests are included but are not represented as run after the
waiver. Earlier focused API/metadata and captured-Wasm results remain scoped to
the versions actually executed. Cryptographic artifact/configuration approvals
and transaction signatures are not fabricated or bypassed by this decision.
