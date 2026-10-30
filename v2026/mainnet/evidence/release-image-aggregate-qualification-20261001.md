# Offline image receipt aggregation qualification

2026-10-01. The new `--aggregate-image-config` mode in
`scripts/mainnet-release-build` closes the missing composition between an
original source manifest and its seven-service and scratch-image supplements.
It reads existing local files and emits a separate integrity attestation; it
does not build, execute applications, contact Docker or mutate those inputs.

The exact current candidate uses SN
`2d53e6f2e44be3d08c0d150f836f163f0eb74442` and server
`ecbf3aadeb8d9a6d53bec36663e94855e64ec7cc`. Its three file pins are:

| Input | SHA256 |
| --- | --- |
| `candidate-f/manifest.json` | `1dfdabd436dc26c6e4bbfaf25c9ff9ca5477374fde85aaf0643403a768947f50` |
| `images-package-f/image-receipt.json` | `9a6c6840b992d86cd41f68c47dcec0799ae013b438e9f8651fd8b880aea97047` |
| `images-scratch-f/image-receipt.json` | `e0ba6bf45b7aad19c3af85dbe343aa7d7e74361253789037aa06d52555274820` |

These directories are beneath
`/mnt/data/sn-testnet/release-current-sol-20261001`. The aggregate verifies
170 parent artifacts, 150 package artifacts and 17 scratch artifacts, including
all seventeen executable source/module records and all eight independent OCI
readbacks. It checks original file/content seals and parent identity; all
artifact hashes and lengths; physical paths; exact recipes, locked packages and
retained base; fixed build command/environment and successful exits; and the
complete image, layer, rootfs and embedded-binary joins. A final input fence
rehashes the original evidence before sealing the new result.

The output has exactly eight images, `missing_images: []` and
`source_to_image_verified: true`. `reproducibility_verified`, `release_complete`
and `deployment_approved` remain false. All three original receipts retain
their original partial-coverage flags and bytes. The local integrity seal does
not independently attest a builder, runtime behavior, production configuration,
archive/restore, arm64, vulnerability policy or release authority.

All 88 builder test roots pass normally and with race detection; `go vet` passes.
Eleven new roots cover file/content authentication, resealed foreign-parent
refusal, complete artifact hashing and path custody, scratch/package replay
against parent binaries, complete census/authority, strict configuration,
non-mutating failure and exclusive output, cancellation, and retained command
and metadata integrity. Fixtures use synthetic identities and deterministic
mutations, with no Docker or network dependency.

Four source-overlay causal controls independently remove the mixed-parent
join, artifact-byte comparison, scratch readback comparison, and deployment
authority guard. Each named test then fails at its intended assertion. The
normal source is never modified by those controls. Full normal/race package
streams retain all 88 root PASS events with no failures or skips.

Detailed commands, original JSON streams, control overlays/results, the exact
verifier executable and the final aggregate/config are retained under
`/mnt/data/sn-testnet/mainnet-release-aggregate-astra-20261001`. The final
`aggregate/image-aggregate.json` and the external qualification receipt are
sealed by that directory's `SHA256SUMS`. The previously independent OCI audit
(`audit-oci-f.json`, SHA256
`d567716b89032c1074b27d0ee13a4a93d53707f9a8744d78d1261b89e81940e0`)
provides a separate comparison for all eight archive, platform, configuration
and executable identities. This is scoped MG-02 progress, not gate closure or
permission to sign, publish or deploy.
