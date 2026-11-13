# Metadata and HTTP admission successor release

The frozen binary and image source is SN
`bab49e1c6d1bf9332178087e6f740c4416a8f097`, tree
`7c99392cf0d2970de4834d66ac62cec478fd0079`, and server
`a3e2e668354b32264d91d31f21cb8d84620a901f`, tree
`0c12418875591a7eed5f84db00485134b7754aa8`. This packages the
[bounded metadata decoder](runtime-metadata-bounds-20261001.md),
[shared native HTTP admission](http-rpc-response-bounds-20261001.md), and
[direct EVM HTTP admission](evm-http-response-admission-20261001.md), plus the
server's single-read contract metadata and optional PostgreSQL query sampler.
The sampler remains off without explicit bounded configuration. This increment
changes no production source; effective Connect/SDK module pins remain unchanged.

The local build root is
`/mnt/data/sn-testnet/mainnet-http-release-astra-20261001/final-bab49e1c-servera3e2`.
Ten clean physical checkouts bind all selected repository and Solidity library
commits/trees. Effective Connect/SCTP `e1b5d77b5029` and SDK `5d37be3876e5`
remain unchanged. Go 1.26.6, Forge, Solc 0.8.24, Git and buildx executable
hashes, forty package payloads and the selected local OCI base are checked.
Both builds use source epoch `1790882047`. Final readback confirms the ten
sources and four source-build tools remain unchanged.

The initial `final-bab49e1c-server6c39` preparation stopped before any build:
`git log` attempted to walk an absent parent of the pinned Forge library.
Its files remain preserved. The preparation helper now reads the timestamp
from the exact retained commit object. Selection advanced to current server
`a3e2e668` before any source build, using a fresh directory; no earlier output
was relabeled.

## Local construction and repeat

Builds A and B ran sequentially, with separate initially empty Go caches;
Forge's ignored output was never written by concurrent source builds. They
completed in 619 and 599 seconds. Each sealed source manifest retains
175 artifacts, all seventeen executable roles, five exact fresh contract
creation/runtime pairs, and all twelve required migration source inputs.
All seventeen binary hashes and ten bytecode hashes match across A/B.
Binary build information binds the expected clean VCS revision, Linux/amd64
and disabled CGO. Historical and selected contract bytes remain distinct.

| Artifact | A SHA-256 | B SHA-256 |
|---|---|---|
| Original source manifest | `c4521ff52c29583835169505e397c0f64f7237ac33e5ffcf9cb706548cf75a0a` | `f0031727e12c116bb87bf0c4fe81f4d82cfaa4696b995343cb7c4ddb55544ee5` |
| Complete image aggregate | `19462fb5fea2fd2fc99b83c84bb8f91d516474b549db12891aae10fd4a2c2835` | `7c54a79f957256db1185338dd7f5db013442e7aa73d40aef3ff64a2b4abd08ba` |

Each aggregate authenticates its own original parent and seven-package/one-scratch
supplements, then verifies 342 source/image artifacts. All eight OCI platform
manifests, configurations and archive hashes match between repeats. Package
rootfs, OCI descriptor/layer/diff-ID and embedded binary checks pass before
the local source-to-image claim is true. Each image repeat uses a separate
initially empty Docker/containerd store. Both owned build-daemon pairs stop
and join; system daemons and application services are untouched.

Compared with [SN `689938d6` / server `6c39d307`](release-689938d6-server6c39-20261001.md),
all seventeen binaries and eight image platform digests changed, while all
five contract bytecode pairs remained equal. The exact comparison is retained
in `evidence/audit-compare.json`. The old release keeps its original scope;
none of its attestations is inherited.
Manifests, timestamps and logs are not asserted byte-identical.

## Inventory and qualification limits

All 95 builder/source-graph roots pass normally and under race; both packages
pass vet and the builder compiles. The selected server sampler's thirteen
roots pass normal/race and its two packages pass vet. Eleven original
component receipts are copied after hash and source-ancestry checks; their
earlier server pins and behavioral scopes are preserved. This does not claim
an exhaustive composed application or database qualification on the new pair.

The migration inventory retains twenty-five database/monitor source files and
751 catalogue entries. The unsigned release inventory covers 61 files totaling
807,538,809 bytes and repeats byte-for-byte. No live database is inspected or migrated.
Policy and published/deployed image identity remain missing categories.

The manifests record incomplete module qualification: SN has 636 nodes, with 363 lacking
full retained qualification (349 lack go.mod metadata and fourteen have metadata
without source bodies); server has 647 nodes, with 372 incomplete (347/25).
Offline builds and exact selected pins do not prove exhaustive module-body or
compiler/package provenance. Those gaps remain in the original manifests.

Only local source-to-bytecode/source-to-image verification and the stated
same-host output equality are true. Independent builder reproduction,
`reproducibility_verified`, `release_complete` and `deployment_approved` stay
false. Independent compiler/package provenance, arm64, archive restore,
SBOM/scanner/attestation policy, production configuration/policy, migration and
rollout readiness, published identity and running-image readback remain gates.
No signer, transaction, publication, deployment or application service starts.

## Terminal seal

Under the local build root, `evidence/release-receipt.json` has SHA-256
`b788660e4b49e3c71eb9cfb3787cf6360062c18b063e1f93300da324e0d44ec7`.
The 966-entry `evidence/SHA256SUMS` has SHA-256
`30837a11c4e9b679a36a44dff6b8be3bab61d06a925bb6fbda2977a40a031ced`;
complete readback passes. Its log is
`evidence/check-SHA256SUMS.log`. The seal covers both original source manifests,
supplements, aggregates, retained artifacts, comparison, inventories, exact
source/tool checks, component receipts and daemon shutdown records; compiler
caches and mutable scratch are excluded.

A separate read-only verifier checked the sealed A/B source graphs, 175 artifacts
per candidate, embedded VCS identities in all seventeen binaries, ten raw
bytecode outputs, twelve migration copies and the eight-image OCI readback.
Its independent receipt at
`/mnt/data/sn-testnet/sol-mainnet-http-release-independent-20261001/receipt.json`
has SHA-256 `c5b7d1b49093936efe7fc227ea9946ee407885cec840d920cc8ffc18ec31d8ec`.
This is an independent artifact check using the same host and toolchain, not
independent compiler or dependency provenance.

Later reporting commits do not change the frozen binary source. The owner's
separate acceptance of the exact official v470 artifact and documented
reproducibility exception is for launch planning only; it grants no release,
deployment, finality-checkpoint or signing authority to this bundle.

Server `main` subsequently advanced to `64cde171` with internal-prober grant
locking changes. This release remains pinned to `a3e2e668`; selecting the later
server code for deployment requires its own exact-source build and qualification.
