# Local mainnet release candidate v10, 2026-09-27

This is an **offline, unapproved, incomplete** release candidate. It does not
authorize mainnet deployment, UID reset, signing, spending or acceptance.
Snow VPN `172.28.208.185:9944` still returned testnet EVM chain ID 945 and
testnet genesis when checked for this candidate.

The clean detached [source lock](source-lock-candidate-v10-20260927.json) pins
SN `265231f9`, server `a211d56c`, Connect `c68689c4` and every local Go
replacement. Two reads matched byte-for-byte. Its content hash is
`0x95adbd72593b258c9fb9ab1cda303df214231a61ede540381777a5fcc28e4737`.
Seven static Linux/amd64 binaries built from this composition. The six files
already selected in v9 rebuilt byte-for-byte equal; proxy adds the seventh.

The [actual-file inventory](release-inventory-candidate-v10-20260927.json)
hashes **81 files and 404,724,018 bytes**, including the binaries, all eight
server Dockerfiles, the runtime package lock, all 40 checksum-pinned `.deb`
payloads, six signed-index inputs, selected build/module/migration sources and
four unchanged copied contract artifacts. A second inventory read matched
byte-for-byte. Its content hash is
`sha256:7449aced83d1e7c46ec1807aaf615076119b12e57246fc820f74d0650cebe65e`.
The exact input, binaries, replay files and checksums are in the
[v10 evidence bundle](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v10-20260927/RESULT.md).

Seven Ubuntu service Dockerfiles now use the same pinned base plus literal
checksum-locked package payloads, installed offline. Proxy retains its full
`curl` closure. The [package qualification](/mnt/data/sn-testnet/evidence/mainnet-server-package-pins-20260927/RESULT.md)
verified signed Ubuntu indexes and all 40 payloads, passed three normal and
race contract tests plus vet, and simulated complete amd64/arm64 dependency
sets against the pinned bases. Independent local [API and proxy image
probes](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md)
built and extracted the selected binaries exactly. Those local image IDs are
not published manifests or deployed-image evidence. Actual arm64 image/script
execution, durable owned package archive, byte-identical OCI rebuild,
source-to-image provenance and full service qualification remain open.
An explicit [forced API rebuild](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md#forced-rebuild-result)
confirmed that OCI bytes still drift: the same Dockerfile, binary and pinned
packages produced a different digest. Exported rootfs comparison isolated
wall-clock package logs/cache and file timestamps; package pinning alone did
not make the image reproducible.

The EVM tree and four copied contract artifacts are unchanged from v9; their
new source-lock binding is not a new source-to-bytecode proof. `policy` and
`image-identity` remain missing inventory categories. The inventory reports
`release_complete=false`, `provenance_proven=false` and
`deployment_approved=false`. Mainnet identity, validator and operator
eligibility, custody, activation, native 10%/90% outcome, images and rollout
qualification remain open under the [gate tracker](../PRELAUNCH-FIXES.md).
