# Runtime 459 adoption

The owned archive returned `node-subtensor/459/1/1` at finalized block
8,013,770, hash
`0xab776262d2a5ac8ac3acf0fad5e33ac3be431628d386457f903d85eb4e8d9225`.
The genesis remains
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`.
Every new blockchain request used the approved owned LAN endpoint directly,
without pacing, redirects or a public fallback. New provenance records
`independent_rpc=false`. Earlier public observations and the exact458 artifact
retain their original provenance.

| Artifact | Bytes | SHA256 | BLAKE2b256 |
|---|---:|---|---|
| On-chain code | 2,524,988 | `c78bef5489149655254d5fb01a0e8c5c61846b0b322a54cb9ca2c86a14df8284` | `558275958401c026fa4a4159466d49eabd08c761f0c801390593fcba91dee69b` |
| SCALE metadata v14 | 336,358 | `52256b0b4a5c682e94e1d68a7b7a5dc1ba4114cfde4fb39057be808a8443673d` | `cf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef` |

The code BLAKE2b256 equals the storage hash read at that same explicit block.
Terra's existing exact-Wasm probe passed on these bytes at 21:36:26 UTC on
2026-09-15. It reproduced the full version tuple and exact metadata under the
unchanged Polkadot SDK revision
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`, with no stateful host access or
blockchain request. This qualifies metadata reuse for this exact artifact.

## Source linkage

GitHub's [runtime459 artifact record](https://api.github.com/repos/RaoFoundation/subtensor/actions/artifacts/10418002729)
links artifact10418002729 to
[workflow35022000542](https://github.com/RaoFoundation/subtensor/actions/runs/35022000542)
and source commit
[`70378404b56c12a85bc8cd163aca2f32cf4d1b80`](https://github.com/RaoFoundation/subtensor/commit/70378404b56c12a85bc8cd163aca2f32cf4d1b80).
The unauthenticated API archive download returned HTTP401. The public artifact
mirror returned a ZIP whose SHA256 exactly matched the authoritative API digest
`8c77e9d3d82384b85b0f2f21839207dd678c700966f500edeaae44821fd40024`.
The included srtool digest identifies that commit, production profile,
srtool0.18.3 and Rust1.89.0. The extracted CI Wasm is byte-identical to the LAN
Wasm, including its compressed size and both code hashes. No mutable branch,
invented tag or mainnet multisig proposal is used as admission authority.

## Compatibility scope

The comparison starts at the reviewed458 commit
`a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7`. The
[459 source manifest](runtime-v459-source.sha256) preserves all62 previously
attested paths and adds nine paths changed by459: Cargo.lock, the new root-pot
migration and its module declaration, delegate-info valuation, move/remove
stake, childkey handling, registration and destroy-alpha regression tests.
Its71 rows cover all25 changed native Rust files and Cargo.lock. Generated SDK
bindings, generated documentation and website catalogs do not expand this
native source scope. Historical454/455/458 manifests are unchanged. The static
metadata source manifest adds three459 rows, preserving all18 historical rows.

The unchanged native interfaces are `precompiles/src`, the subtensor dispatch
definitions, selective metagraph runtime API and metadata layout, and weights
submission code. `runtime/src/lib.rs` changes the spec and migration tuple;
its signed extensions and pallet call indices retain their existing layout.
The two CRv4 timelock variants and `Utility.batch_all` therefore retain their
encoding. Actual459 metadata tests independently compare those calls with the
signed encoder and reject signature relabeling across454/455/458/459.

Runtime459 introduces substantive chain behavior changes:

- Share-pool denominator updates follow the stored-share delta. Closing a
  pool advances its epoch, and retired share rows cannot claim later deposits.
  Staking, delegate valuation and dissolution apply these retirement checks.
- Pending child relations recheck the stake threshold at maturation. Stake
  changes queue bounded checks that suspend or resume live relations; the
  subnet-owner exemption stays local to its owned subnet. The runtime's
  parent/child accessors filter suspended relations.
- The one-time root-pot migration reconciles historical root holdings and
  backing. Root-seat eviction now checks the incoming root stake, and root
  registration counters use a moving tempo boundary.
- NonFungible/NonTransfer proxy filters exclude additional value-moving and
  sudo calls. Cargo.lock updates rustls/webpki; the SDK revision is unchanged.

Production SN does not value raw Alpha/AlphaV2 shares or raw ParentKeys itself.
Its native stake reader authenticates the exact block/artifact and calls the
runtime's selective metagraph API. Thus current weighted stake reflects459's
own suspension and share semantics. Existing exact custody, threshold, permit,
subnet-owner and before/after balance checks remain mandatory. Admission does
not assert unchanged economics or waive a refusal caused by current state.

## Admission and preserved progress

Current simulator, miner and validator reads require only the exact459 tuple.
The setup history set now holds459 plus451–455 and458, bounded at seven cached
artifacts. Companion history retains455/458; an original458 owner preserves its
original455 replay domain. Current signing still uses one exact artifact.

The explicit configuration migration retains original455 identity for both
the historical458 and current459 public manifests. Operational plan admission
requires current459 expectation and lock. Archived plans and source locks
retain their original bytes, hash, runtime and signatures. Regression coverage
reopens a459 approval carrying an original458 companion and checks unchanged
intents, budgets and lineage. Cross-artifact provenance remains refused.

`deploy/testnet/public.yml` advances to459 and retains
`config_identity_runtime_spec: 455`. The fixture lock's runtime stanza advances
to the reviewed459 tuple; native integration must render its final source
hashes from the combined physical checkout. Private vault, allowances, custody,
journals, finalized receipts and retained generation identities are unchanged.

Restricted raw evidence lives under
`/mnt/data/sn-testnet/qualification/runtime459-20260915-r1`: `lan-artifacts`
contains pinned requests/responses; `upstream-records` contains the API record,
verified ZIP, digest, source comparison and scope receipt; `terra-offline-probe`
contains the passing exact-Wasm capture. This source review and offline probe
do not establish completion of the live campaign or finalization.
