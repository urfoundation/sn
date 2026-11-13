# Successor release preparation, 2026-10-01

This preparatory snapshot is followed by the
[selected successor release](release-233ea2be-server942-20261001.md).
The pending-source state below records the earlier preparation only.

The provisional graph selects clean SN
`7ee916cc2243db2ac8dc6b4d9b2b3d74df6fe4c3`
(tree `ac36b4903c139cba71f96491523289e069b465f1`) and server
`94229abb02819cea97f972421972b4e79c1a32ab`
(tree `61a5f30830c44b99f14583a1c5e8ad88d6a70908`). This includes the
passive-root host and schema-751 mixed-writer guard. The owner recycle-mode
transition is still pending. **No successor source/image release is sealed.**

The [earlier packaging baseline](release-6c801a25-server720-20261001.md)
remains unchanged. Preparation uses ten new clean physical Git clones under
`/mnt/data/sn-testnet/mainnet-successor-release-astra-20261001/pre-owner/source`.
Only SN and server pins change. Effective Connect/SCTP remains
`v0.0.0-20261001021459-e1b5d77b5029`; SDK remains
`v0.0.0-20261001021058-5d37be3876e5`. The builder, contract sources, module
declarations, image recipes and runtime package lock are unchanged from the
baseline. The provisional maximum source commit epoch is `1790857447`;
the final source selection must recompute it.

Both offline module graphs resolve (636 SN and 647 server entries), and both
`go mod verify` commands pass. The unchanged release-builder package passes
all **89 normal roots** (0.956 seconds), all **89 race roots** (7.025 seconds)
and vet, without failures or skips. Its freshly built preparation executable
retains the exact clean SN revision. This checks the builder and preparation
graph; it does not qualify every application or migration behavior.

The four compiler/Git executable hashes and buildx hash remain exact. All
40 pinned package payload hashes and four local base OCI blob hashes match.
Full platform, descriptor, recipe and rootfs validation remains part of the
future image build. No build daemon or application is started in this phase.
The migration source inventory retains 20 committed files and counts 751
catalogue entries. No database is inspected or migrated.

The initial timestamp query fails on unavailable ancestry in a shallow
contract-library clone. The retained correction reads the exact pinned commit
object directly, without requiring unrelated history. All ten source identities
and clean working trees are rechecked after qualification. This preparation
changes no production code or provenance condition. The original setup failure
and all successful command records are retained.

Evidence is under
`/mnt/data/sn-testnet/mainnet-successor-release-astra-20261001/pre-owner`.
All 61 selected preparation files pass checksum readback. This checksum set
records preparation evidence; it is not a source/image release seal.

| Retained file | SHA256 |
| --- | --- |
| `evidence/preparation.json` | `aaceb8c36c53372d1e8c6144069d5ae542c72b042995906f9e8a7445199a8b64` |
| `evidence/preparation-receipt.json` | `ffcd60aeab22d3b44a4e813a9571c00482f19fc836e31d492b097d81d406c095` |
| `evidence/migration-inventory.json` | `3395a0748ba1e10af1e059b4f68eea004ae8875eb99fcb7f4e0b844df2158fa6` |
| `evidence/SHA256SUMS-preparation` | `234784c60c5aa2e52234d63764d7c9b8aee53d302f3ae40e1dc78ad5bc9d72c7` |

After the owner transition merges, select its exact final SN commit and tree
in a fresh workspace/configuration, retain the exact server pin, recompute the
epoch and rebuild the builder from that source. Then build and repeat all
17 executables and five fresh contracts, generate the source lock with the
candidate's own executable, and join all eight local OCI images to each exact
source manifest. Reuse only independently rechecked base/package inputs and
the explicit separate containerd/Docker storage configuration on `/mnt/data`;
never reuse predecessor manifests as current output identities.

Independent-builder reproduction, arm64 execution, archive/restore,
production configuration and policy, service qualification, compiler/package
provenance, SBOM/scanner/attestation, publication identity and release approval
remain gates. No image is published or deployed, no service is started and no
mainnet transaction is signed or submitted.
