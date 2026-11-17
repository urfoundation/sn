# v11 taskworker image qualification

The exact v11 taskworker binary (`sha256:ad2c71503633651c878e29361069502061d3d0060fab91d38e37865f91924ecf`)
and server Dockerfile at `77cb401e2c535ec5d4bcaf982ef0fe363bde3c9c` were
assembled into one retained context. Two independent uncached BuildKit builds
with Linux/amd64, `warp_env=mainnet`, `SOURCE_DATE_EPOCH=1790530694`, maximum
provenance and timestamp rewriting produced the same runnable platform
manifest:

`sha256:9b6d516d778a67fa39dbd9ea2f311472e5c72fe9803872d73a1d4aaf1f833b2d`

The comparison verified every OCI blob hash and referenced size and matched
the config and all four compressed layers. The top-level indexes differ by
invocation provenance. An offline disposable container passed checks for the
exact embedded binary, installed packages, linker cache, CA bundle and empty
package audit. The two archives, logs, comparison, context, smoke output and
`SHA256SUMS` are retained in
[`RESULT.md`](/mnt/data/sn-testnet/evidence/mainnet-server-taskworker-image-20260927/RESULT.md).

This is same-builder, prebuilt-binary amd64 image evidence. It does not prove
source-to-binary provenance, independent or arm64 builds, publication or live
service readiness. MG-02 remains in progress.
