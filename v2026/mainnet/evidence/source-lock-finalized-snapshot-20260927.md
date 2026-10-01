# Mainnet composed same-block finalized candidate, 2026-09-27

Status: offline source candidate and read-only owned-RPC **testnet** observation. This is not mainnet identity approval, a source-to-Wasm attestation, a complete release manifest, or authority to sign or deploy.

Clean detached SN source `468f79cd`, server `9f860731`, Connect `c68689c4`. The [source lock](source-lock-finalized-snapshot-20260927.json) binds nine clean repositories, ten local replacements, module hashes, Go version and command binary. Source-lock content hash `0x6d117f48b01daf825f67957cf00c7a85e3d2324d6e294a31ab8f7f890ec1dc97`; JSON SHA256 `726595b4db04a5e5185e6b39ea62b8b0d7c7b00c5af839e6146aceaa6d2b740b`; binary SHA256 `51653cabc5991e5b2f399e2b9465d8ab6327fe008ff698cf7e4c73a6e564b9ef`.

The signer-free [`finalized-snapshot` command](../FINALIZED-SNAPSHOT.md) selects one authenticated finalized native hash, captures raw runtime code/metadata at that hash, and follows its Frontier digest to raw canonical EVM RLP. Its final native/network/EVM checks occur after both components. Standalone command schemas remain unchanged.

| Qualification | Result |
| --- | --- |
| Agent full mainnet normal suite on the same source/dependency code | PASS, 161/161, 79.209s |
| Agent focused combined/runtime/mapping race suite on the same code | PASS, 26/26, 42.554s |
| Agent mainnet vet and build | PASS |
| Clean detached v6 binary build and source lock | PASS |
| Cross-module compile: miner, validator, mainnet, chain, CRv4, sim-testnet | PASS (`compile.log`) |
| Clean detached v6 read-only Snow `finalized-snapshot` | PASS |

The [redacted observation](finalized-snapshot-snow-20260927.json) retains native finalized block **8098308**, hash `0x05341c54113ce687bccb75b6daed536ac57c8c36bfcfa3c2aa836a4bbef6ea2a`, for both runtime artifacts and mapping. It links to EVM block **8098308**, hash `0x4771e1b2c882f13823812d8f574a60bf2d8ab0c46deb679cdae77f6dab162f8a`. Independent Python SCALE/Blake2b-256 checks reproduce the 2,559,437-byte code hash, 354,056-byte metadata hash, native header hash and domain-separated combined content hash `sha256:8db4851da152c8928b18d108bf16a16f0962e2b9939e49c9eaee92cc0e44713e`. Independent Go RLP/Keccak-256 reproduces the 509-byte EVM header hash and number. Raw JSON SHA256 `f1d43cc0a80c5776402d62b22bb07a7f46645dc8fbb8bc74353d855b0da5ba6e`; full bytes remain at the external path in the summary.

Snow still reports **testnet EVM ID 945**, genesis `0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`, runtime spec 471. `runtime_source_proven=false` and `finality_authority=owned-rpc-assertion`: the linked header commitments and same-block runtime bytes are reproducible, but GRANDPA finality, source-to-Wasm provenance and mainnet identity are not independently established. Mainnet activation remains closed pending MG-01/MG-02/MG-04 and the other production gates. The [prior separate mapping candidate](source-lock-finalized-mapping-20260927.md) is retained as historical evidence and superseded for same-block launch evidence.
