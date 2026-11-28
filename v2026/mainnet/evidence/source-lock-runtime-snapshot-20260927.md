# Mainnet composed runtime-snapshot candidate, 2026-09-27

Status: offline source candidate and read-only owned-RPC testnet probe. This is not mainnet runtime approval, source-to-Wasm attestation, a complete release manifest, or authorization to sign or deploy.

The clean detached SN source is `55469798` with server `9f860731` and Connect `c68689c4`. `qualified-source.json` records nine clean Git repositories, ten local replacements, Go 1.26.6, module hashes and the command binary. Its content hash is `0x3be201c1d90a74178766ea1bf341fe1e1d15e227d277ff1935a8c3fde3e7941e`; JSON SHA256 is `810c9c766f1fdb59f9bd0e081c91e12b030f0978827d1a5a2d5a22b924259bcc`; binary SHA256 is `f4e8e107b74c1e9a491d6ba56abec7bedbe2c555762ba4e37cb1ef59cbb2f9f6`.

`runtime-snapshot` captures the complete runtime version and raw `:code`/metadata at one finalized native block. It verifies code bytes against `state_getStorageHash` and repeats the genesis, finalized-height, native chain and EVM ID reads after artifact collection. An optional independently supplied expected identity fails before artifact download when the route differs. Unknown metadata is preserved as raw evidence for review, not admitted as a compatible execution interface. The emitted `admission` value is always `unapproved_observation`.

## Qualification

| Check | Result | Log |
| --- | --- | --- |
| Synthetic exact-artifact, wrong-chain early rejection, code mismatch, route retarget and unfamiliar metadata tests | PASS | `runtime-race.log` and full normal package |
| Full mainnet normal package | PASS, 78.702s | `mainnet-normal.log` |
| Focused identity/runtime snapshot race | PASS | `runtime-race.log` |
| Mainnet vet | PASS | `vet.log` |
| Cross-module compile: miner, validator, mainnet, chain, CRv4 and sim-testnet | PASS, all six | `compile.log` |

The clean-source tool also captured `snow-unapproved.json` from Snow VPN at finalized native block 8,097,226. It remains **testnet**: EVM ID 945, genesis `0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`, runtime spec 471. Code length is 2,559,437 bytes; metadata length is 354,056 bytes. Independent Blake2b-256 calculations reproduce both artifact hashes, and independent SHA256 reproduces the snapshot content hash. Raw snapshot SHA256 is `5b9a7dc76f209d66d6141be12864e5ce3d279a87443219c58a966f1549e12934` and content hash is `sha256:15ba0f0cea359802c0ed71016db6eb0ac496bb9e8cbcfee4b94ee883f3e12831`. The repository carries a redacted summary; raw bytes remain in this evidence directory.

The snapshot trusts the selected RPC for finalized headers and state, and does not verify GRANDPA, state proofs, source-to-Wasm build identity, metadata call/storage semantics, precompile behavior or native/EVM finality mapping. Mainnet activation stays closed until the operator switches Snow to mainnet and independent approval and all remaining MG-01/MG-02/MG-04 gates close. The earlier composed candidate at SN `f321ba7c` remains separately retained; this successor supersedes it only for the listed checks.
