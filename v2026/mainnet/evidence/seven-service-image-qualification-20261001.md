# Seven-service offline image qualification, 2026-10-01

The seven Ubuntu-based server images were built from the frozen fresh-catalogue
candidate using the exact retained production recipes, a pinned local Ubuntu OCI
layout and forty checksum-pinned local package payloads. The original remote
sources were replaced only in a mechanically checked temporary build context;
the checked-in Dockerfiles were not rewritten. No registry push, deployment or
mainnet transaction was performed.

Source commits `f7c73999` and `74b1dd27` were independently qualified by Sol.
The focused 73 builder test roots passed normally and with race detection, vet
passed, and five causal controls refused the intended mutations. A fresh second
build produced all seven OCI archives byte-identically to the author build. The
independent reader authenticated the selected platform, complete OCI chains,
rootfs content and embedded binaries for all seven images.

The independent handoff is
`/mnt/data/sn-testnet/remaining-images-sol-qualification-20261001/evidence/HANDOFF.md`.
Its `SHA256SUMS` SHA256 is
`bfde36b07255d14de5bdd899cd032ed2becb41038b0651b3c51146e1dcf6b2d3`
(217 files; verification exit 0). The fresh image receipt SHA256 is
`91407bbd256cf8f086d1d1aaa67945ccdf92f9d7258c6323c1fcc6e988a8621d`
and its content seal is
`sha256:176884d9e9a21858934b8934059ad35e195bc5c915f7fe4761ffa18b9a4375bf`.
The author handoff is
`/mnt/data/sn-testnet/remaining-images-astra-20261001/evidence/HANDOFF.md`.

This qualifies local image reproducibility for that frozen source candidate.
It does not approve the aggregate release, published image identities, deployed
image readback or mainnet activation. Those approval flags remain false.
