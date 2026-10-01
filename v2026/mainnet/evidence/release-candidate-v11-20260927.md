# Local mainnet release candidate v11, 2026-09-27

This is an **offline, unapproved, incomplete** release candidate. It does not
authorize mainnet deployment, UID reset, signing, spending or acceptance.
Snow VPN `172.28.208.185:9944` still returned testnet EVM chain ID 945 and
testnet genesis at the last read.

The clean detached [source lock](source-lock-candidate-v11-20260927.json)
pins SN `265231f9`, server `77cb401e`, Connect `c68689c4` and every local Go
replacement. Two source-lock reads matched byte-for-byte. Its content hash is
`0x525268b289e59616eac1b64eb7b3854ff333f99f8001f9beb91a62a906d05f14`.
All seven selected Linux/amd64 binaries rebuilt byte-for-byte equal to v10.

The [actual-file inventory](release-inventory-candidate-v11-20260927.json)
hashes **85 files and 404,731,031 bytes**, adding all seven server image-build
Makefiles to the v10 binary, Dockerfile, package-lock, 40-payload, signed-index,
module, migration and copied-contract scope. A second inventory read matched
byte-for-byte. Its content hash is
`sha256:73b37f699eb3a4f197a5e93fb19c6b611b63b51f0188fd6de6ff958e2eb7d259`.
The exact input, binaries, replay files and checksums are in the
[v11 evidence bundle](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v11-20260927/RESULT.md).

Server `77cb401e` removes only two volatile package-install outputs and sets
a recorded source epoch with timestamp-rewriting image export. The
[image qualification](/mnt/data/sn-testnet/evidence/mainnet-server-image-repro-20260927/RESULT.md)
passed eight affected checks normally and under race plus vet and offline
smoke. Two uncached amd64 builds each of API and proxy produced identical
runnable platform manifests, configs and compressed layers. Their full OCI
indexes differed because retained provenance attestations describe distinct
build invocations; those attestations were not removed. The test used a
recorded source-epoch override from the parent commit, not v11's final commit
default. This proves a narrow same-builder, same-input platform result, not a
published or cross-builder release. Arm64 image execution, other service image
builds, full attestation/SBOM/scanner policy, owned package archive, service
startup and deployed-image readback remain open.

The EVM tree and copied contract artifacts remain unchanged. `policy` and
`image-identity` are still missing inventory categories. The inventory reports
`release_complete=false`, `provenance_proven=false` and
`deployment_approved=false`. Mainnet identity, validator/operator eligibility,
custody, activation, observed native 10%/90% outcome and rollout qualification
remain open under the [gate tracker](../PRELAUNCH-FIXES.md).
