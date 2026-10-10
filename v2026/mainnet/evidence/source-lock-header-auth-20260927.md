# Mainnet composed header-authentication candidate, 2026-09-27

Status: offline source candidate and read-only owned-RPC **testnet** observation. This is neither mainnet runtime approval, a source-to-Wasm attestation, a complete release manifest, nor authority to sign or deploy.

Clean detached SN source `dce2cee1`, server `9f860731`, Connect `c68689c4`. The [source lock](source-lock-header-auth-20260927.json) records all nine clean repositories, ten local replacements, module hashes, Go version and command binary. Source-lock content hash `0x136639cb51edd01fc3b880412280da8cdf52633247204efda6fd88782749736f`; JSON SHA256 `0d229f8fe3e68cc83dd3981c7ff3ecbb0f20f5940045cb55c381cd5aa582e5bc`; binary SHA256 `c56c1e4ad47a80a71828180d1f5863fe044aec5e4f707544c5dba5338a4683df`.

The command now authenticates the complete finalized SCALE header against its announced hash before reading state, decodes the full runtime tuple with contradictory state aliases rejected, compares both runtime reads, verifies raw `:code` against its reported storage hash, and retains raw code and metadata bytes. Raw code up to 8 MiB and metadata up to 4 MiB fit within separate JSON reply bounds. Unknown metadata remains an unapproved artifact for review.

| Qualification | Result |
| --- | --- |
| Full mainnet normal package | PASS, 83.588s (`mainnet-normal.log`) |
| Focused identity, snapshot and digest race selection | PASS, 41.583s (`runtime-race.log`) |
| Mainnet vet | PASS (`vet.log`) |
| Cross-module compile: miner, validator, mainnet, chain, CRv4, sim-testnet | PASS, all six (`compile.log`) |
| Read-only Snow `inspect` and `runtime-snapshot` | PASS against real finalized headers; testnet observation only |

The [redacted observation](runtime-snapshot-header-auth-snow-20260927.json) is finalized native block 8098046 at 2026-09-27T14:20:29.292879132Z, hash `0x78e79a6f06695ef8d9fc9a1e9ce67fbdb2307505c328a499f987831511d06ed9`. It still reports **testnet EVM ID 945**, genesis `0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`, runtime spec 471. Independent Blake2b-256 calculations reproduce the 2,559,437-byte code and 354,056-byte metadata hashes; independent SHA256 reproduces the domain-separated snapshot content hash `sha256:036a214cce2b55d16cda5767957400be2de973d6c8abe722c659a1c2a0c6027b`. Raw JSON SHA256 `43309679eb4fbbd2637d38de40d1e3f63852cba6380c630eb4b990642332068b`; full raw bytes are retained at the external path in the summary.

The selected RPC remains the trust boundary for finalized-head and state responses; this command does not verify GRANDPA, state proofs, source-to-Wasm provenance, metadata semantics, precompile behavior or native/EVM finality mapping. `inspect` still serializes spec/transaction numbers in its v1 output; its full runtime tuple is checked internally and appears in `runtime-snapshot`. Unknown future digest variants require review before the generic header reader accepts them. Mainnet activation remains closed until Snow serves independently approved mainnet identity and the remaining MG-01/MG-02/MG-04 gates close. The [previous snapshot candidate](source-lock-runtime-snapshot-20260927.md) is retained as historical evidence and superseded for this code path.
