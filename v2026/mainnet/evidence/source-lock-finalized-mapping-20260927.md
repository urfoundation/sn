# Mainnet composed finalized-mapping candidate, 2026-09-27

Status: offline source candidate and read-only owned-RPC **testnet** observation. This is not mainnet identity approval, a source-to-Wasm attestation, a complete release manifest, or authority to sign or deploy.

Clean detached SN source `b571fd04`, server `9f860731`, Connect `c68689c4`. The [source lock](source-lock-finalized-mapping-20260927.json) binds nine clean repositories, ten local replacements, module hashes, Go version and command binary. Source-lock content hash `0x9e6b129c82debece35f33d11a0f526620fc63d0773019d208f5483bf5d1bc833`; JSON SHA256 `17aad8cc5b8d7bfe9bd54a56ed9836c66c41438d8704458f50652c32ca27172b`; binary SHA256 `559285f73daaa93a4ee0b24c3247e8ca4282ab7c57a552869da032e7696264e8`.

The signer-free [`finalized-mapping` command](../FINALIZED-MAPPING.md) authenticates a complete finalized native header, decodes its reviewed Frontier PostLog hash, selects a canonical raw EVM header by that hash, verifies Keccak-256 of the retained RLP, and corroborates canonicality by the EVM header's own decoded number. It never infers EVM height from native height. Exact native digest, raw RLP, full observed runtime tuple and a domain-separated result hash are retained.

| Qualification | Result |
| --- | --- |
| Full mainnet normal package | PASS, 83.406s (`mainnet-normal.log`) |
| Focused mapping/identity/snapshot/digest race selection | PASS, 42.500s (`mapping-race.log`) |
| Mainnet vet | PASS (`vet.log`) |
| Cross-module compile: miner, validator, mainnet, chain, CRv4, sim-testnet | PASS, all six (`compile.log`) |
| Read-only Snow `finalized-mapping` | PASS against one real finalized native/EVM pair |

The [redacted observation](finalized-mapping-snow-20260927.json) at 2026-09-27T14:55:22.13146499Z retains native finalized block **8098220**, hash `0xa7844d718917809e3f2f7cce87de285518321b5bde66ffd73c80185f0b271b94`, and EVM block **8098220**, hash `0xb41f72f67971b2f9924228c09330575fff5d3fcd2bf1ee6729c5c3324dc8e2d8`. Frontier PostLog variant 1 carries 1 ordered transaction hash(es). Independent Python SCALE/Blake2b-256 reconstruction reproduces the native header hash. Independent Go RLP/Keccak-256 reconstruction reproduces the 511-byte EVM header hash and decoded number. Raw mapping JSON SHA256 `5dc449e51b1a6acfdaa5a27fbb790dbc4272fe68fcc1468c4e601b86dc99cb2e`; full bytes remain at the external path in the summary. The result content hash is `sha256:97bb8dc7dce300c0a76e02b5af6b97bbfbab03aacfc7eac2ae83ef79427f3b95`.

Snow still reports **testnet EVM ID 945**, genesis `0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`, runtime spec 471. `runtime_source_proven=false` and `finality_authority=owned-rpc-assertion`: these linked header commitments are reproducible, but GRANDPA finality, source-to-Wasm provenance and mainnet identity are not independently established. Mainnet activation remains closed pending MG-01/MG-02/MG-04 and the other production gates. The [prior header-authenticated runtime candidate](source-lock-header-auth-20260927.md) is retained and superseded for this code path.
