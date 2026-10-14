# Current-source offline release composition, 2026-10-01

The local source builder completed against exact integrated SN main
`2d53e6f2e44be3d08c0d150f836f163f0eb74442` and server main
`ecbf3aadeb8d9a6d53bec36663e94855e64ec7cc`. The server release graph
pins Connect `e1b5d77b` and SDK `5d37be38`. The ten physical source
repositories were clean at their manifest commits and trees. The build took
523.20 seconds and produced 17 Linux/amd64 binaries, five rebuilt selected
contracts, and eight image contexts.

The retained source manifest is
`/mnt/data/sn-testnet/release-current-sol-20261001/candidate-f/manifest.json`,
SHA256 `1dfdabd436dc26c6e4bbfaf25c9ff9ca5477374fde85aaf0643403a768947f50`,
internal seal `sha256:1b30ff7614cb2fb0364d240b340a7184f811c49263312b46bcc82d72a506cdab`.
Independent audit
`/mnt/data/sn-testnet/release-current-sol-20261001/evidence/audit-f.json`,
SHA256 `96bc75584978ed81079eaa29aaad47cf00472aff0f6e1b2c6d80d72c29d58aaf`,
checked all 170 artifact hashes, all 17 exact unmodified binary VCS stamps,
clean source revisions, and creation/runtime bytecode equality for the five
rebuilt contracts. The selected source graph still explicitly names nodes
without complete transitive source metadata.

Seven package-backed OCI images passed a fresh local build and readback from
this exact source manifest. Their supplement receipt is
`/mnt/data/sn-testnet/release-current-sol-20261001/images-package-f/image-receipt.json`,
SHA256 `9a6c6840b992d86cd41f68c47dcec0799ae013b438e9f8651fd8b880aea97047`,
content seal `sha256:4ecb24e798ee82e77ea94add8958563f9de0f8cdccb9a7d5ce7685fcf86ce258`.
The scratch competitionworker image passed separately; its receipt is
`/mnt/data/sn-testnet/release-current-sol-20261001/images-scratch-f/image-receipt.json`,
SHA256 `e0ba6bf45b7aad19c3af85dbe343aa7d7e74361253789037aa06d52555274820`,
content seal `sha256:794522cb7ec193fc89efe2c18f193bc986355473fb6e99a60b2970efe76c4f56`.
Independent OCI audit
`/mnt/data/sn-testnet/release-current-sol-20261001/evidence/audit-oci-f.json`,
SHA256 `d567716b89032c1074b27d0ee13a4a93d53707f9a8744d78d1261b89e81940e0`,
parsed all eight archives and verified their descriptors, config, layers,
rootfs diff IDs, installed binary bytes, and 40 per-image package-version
claims. Each supplement remains separate from the immutable source manifest.
The [sealed handoff](/mnt/data/sn-testnet/release-current-sol-20261001/evidence/HANDOFF-FINAL.md)
has SHA256 `4dfb56bd874ef6cbafdde7b2edd104666be505c5f3a6f917bd44ceacd65e96fe`;
its `SHA256SUMS-FINAL` has SHA256
`31381ec8a960b7a86c21f5595e28fce92e035ebf3e157119f6169459950c5c31`
and all listed entries verify.

The later [offline aggregation qualification](release-image-aggregate-qualification-20261001.md)
replayed all 337 parent and supplement artifacts and all eight OCI readbacks
against these immutable inputs. Its separate
`/mnt/data/sn-testnet/mainnet-release-aggregate-astra-20261001/aggregate/image-aggregate.json`
has SHA256 `1ae673a859d00232f420e8cb18ede08f142c2d24a2d3e5bc14575cdd5e6abdb3`
and content seal
`sha256:fabe268ca92b7c49197186d568273ba0edff2d69c8ddb157146bd435e9895620`.
It has eight images, no missing images, and local
`source_to_image_verified=true`; its remaining aggregate approval and
reproducibility flags are false.

This is an offline local composition, with no image publication, signing,
chain transaction, deployment, or launch approval. The original source manifest
and both partial image receipts retain false `source_to_image_verified`,
`reproducibility_verified`, `release_complete`, and `deployment_approved`
aggregate flags. They establish their stated local checks, not every
production release or rollout gate.
