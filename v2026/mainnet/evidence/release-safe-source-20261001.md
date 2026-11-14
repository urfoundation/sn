# Frozen Safe-source release build, 2026-10-01

The release source is SN `095a22083b98aa2ec4472181f3bd1e216dbcee51`
(tree `545dd613f6fe9b3c591f601cb2cf4d34804eedfc`) and server
`0b8e758db9ce5516de867e1b5d0a1c9660a0880b`
(tree `500391d3919ff8d3301455d7c735aa91cb74af5c`). This exact SN source includes
the public Safe-submission implementation and the artifact permission repair.
Later reporting/test commits are not silently substituted into this source
claim. No signed or deployed release is asserted.

Evidence is retained separately from the pre-Safe baseline under
`/mnt/data/sn-testnet/mainnet-safe-release-astra-20261001`.
Ten physical Git checkouts are clean at their exact manifest commits/trees.
The effective replacements remain Connect
`v0.0.0-20261001021459-e1b5d77b5029`, SDK
`v0.0.0-20261001021058-5d37be3876e5`, and Connect's SCTP submodule at the same
Connect pseudo-version. Both complete module graphs, linked dependency
materialization, source/tool hashes and final source state are checked.

The two source builds use separate empty Go/compiler caches, the same pinned
Go/Forge/Solc tools and `SOURCE_DATE_EPOCH=1790842924`, offline module access,
Linux/amd64, `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc` and `-trimpath`. They
complete in 560 and 647 seconds. Each sealed source manifest binds all 17
executables, five freshly selected deployment contracts, eight image contexts
and 170 artifacts. All
17 embedded VCS identities match clean selected source. The ten selected
creation/runtime bytecode hashes match fresh compilation; ABI, constructors,
storage layout and semantic immutable references remain checked.

The author readback rehashes both artifact sets and confirms identical bytes
for all 17 executables and ten contract creation/runtime outputs. Independent
builder reproducibility is a separate, still-open gate. The 89 release-builder
test roots pass normally and with race detection on exact SN `095a2208`, and
builder vet passes. These tests and compile results do not qualify every
application's production behavior.

| Exact retained input or result | SHA256 |
| --- | --- |
| `candidate-a/manifest.json` | `fe2b9b53dbc76056846c12fb79c1eff71dd5be9611027d7cba2d6fa7910bff29` |
| `candidate-b/manifest.json` | `cc59e1b8a27b4e934592387703b4eca87999c393c9e5ca36aeb07388c5d75a69` |
| `evidence/source-lock.json` | `6110b5ca451d2bcf627316c9805b85ce32cbf5ad0b6b5d8c502d64a6ddea3639` |
| `images-package-a/image-receipt.json` | `f7d9f2619c5dca2fff49a13d1b8739cb67d9e8a88079b843b13232e83329d0ff` |
| `images-scratch-a/image-receipt.json` | `84ea76be9c32594997b09149aeb24471ce574ca66bd5c0501f532aa5cdb5b936` |
| `aggregate-a/image-aggregate.json` | `c3d11978f4e0b2777a64e720bec6a7ce3ca0bfa8a198778080de264d7ad6a3ea` |
| `images-package-b/image-receipt.json` | `77d4ab8526eece18f1c2a5677f83bdb6b8d1e7a2e75cdf4d03280685b5b5acc8` |
| `images-scratch-b/image-receipt.json` | `87755dc3dc89706877e58eedfb258a8e9dedb1b3534830acbb12ff6e27a5b280` |
| `aggregate-b/image-aggregate.json` | `750ee717753cc6a23f683b89424d21aad3eb6d14d6231b9fd29df5362ee54f72` |
| `evidence/release-inventory.json` | `27aca1952c6569e4132992c152d4378a4f663f6f42e5c92995ab2e6eebc450c8` |
| `evidence/audit-compare.json` | `f74303be1b1f11984128289029b06ea99ad7d689d4ab0f0c927a07974556dbb3` |
| `evidence/release-receipt.json` | `24f9bde4bc5b96769dae3236652dad7f6a1e7060793a09f5987088673e19b205` |

The [frozen release receipt](/mnt/data/sn-testnet/mainnet-safe-release-astra-20261001/evidence/release-receipt.json)
and sibling `SHA256SUMS` retain the exact current local qualification. All
786 selected files pass the final checksum readback. The receipt advances only
the supported local source-to-image claim; signing, image publication,
deployment, release completion and independent reproducibility remain false.

The first seven-service image set passes exact offline source/base/package
and OCI readback. Its separate aggregate rehashes all 337 source/supplement
artifacts and replays all eight OCI/rootfs joins to the original binaries and
recipes. It has no missing images; only local `source_to_image_verified`
advances. Its content seal is
`sha256:f904e23f883f81bf51ba3d6350b73ece18b4a2569b1378d009ed68907c88e176`.

The sequential package-image repeat also passes, followed by a second complete
337-artifact aggregate against the second original source manifest. The final
comparison checks identical platform manifests, runtime configs and complete
OCI archives for all eight images, alongside all 17 executables and ten
contract bytecode outputs. Each repeated image command uses `--no-cache`,
offline inputs and the fixed source epoch. Both aggregates keep
`reproducibility_verified`, `release_complete` and `deployment_approved` false:
same-host byte equality does not establish independent-builder authority.

Both separate scratch-image builds pass strict OCI/rootfs/binary readback and
produce the same archive SHA256
`15b7cf7d8fd7b5ecd9a72fd050e11fe23265847b0892a220b1acfc24dda3c72f`.
The non-root executable retains exact `0755` permissions under an explicit
build-process `umask 077`.

The [pre-Safe baseline and causal repair](release-pre-safe-baseline-and-modes-20261001.md)
remain separate: SN `1806b3b3` / server `0b8e758d` artifacts, the refused API
export, the immutable correction receipt and the earlier eight-image aggregate
are preserved. The older SN `2d53e6f2` / server `ecbf3aad` source manifest and
aggregate are authenticated comparison inputs, never inherited attestations.
All 17 executable hashes and all eight platform-manifest hashes differ from
that older candidate; all five selected contract bytecode pairs remain equal.

Migration capture retains exact committed bytes of 19 server database and
signal-migration source files and finds 749 catalogue entries. This is source
inventory, not an observed live database version, applied migration or archive
restore. Actual production service configuration, signed policy selection,
published/deployed image identity, arm64 execution, independent builder and
compiler/package archive qualification, attestation/SBOM/scanner policy and
release approval remain open. No image is published, application deployed,
mainnet transaction signed or submitted by this build.

The unsigned release inventory selects 55 actual files totaling 806,371,165
bytes and is byte-identical on repeat. Its missing categories remain `policy`
and `image-identity`: local OCI evidence is retained as a dependency without
inventing a registry identity. Config entries describe the build inputs,
not approved production service configuration. `release_complete`,
`provenance_proven` and `deployment_approved` stay false in this generic file
inventory.
