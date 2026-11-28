# Bootstrap finality local baseline

The frozen binary and image source is SN
`28ebfcedd2706ca7e254b277e55b5128de5df458`, tree
`a5a04f2b53e5fbcfd075b3e76290c61a7b9feb8c`, and server
`ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`, tree
`c5c35ead1c4c53f218b0c5c6f6455b5e516ea1dd`. This packages the
[native bootstrap finality and refreshed EVM admission corrections](bootstrap-finality-qualification-20261001.md),
including the final fixture follow-up, and the server's selected-grant locking
correction and diagnostic/test follow-ups. The previous metadata and HTTP
admission fixes remain included. Later documentation and server commits do not
advance this selected source pair. This baseline is **superseded for launch** by
the original-authority EVM action custody successor, which is not packaged here.
A successor exact source/image build and qualification are required before
launch; this baseline does not close its custody issue.

The local build root is
`/mnt/data/sn-testnet/mainnet-bootstrap-finality-release-astra-20261001/final-28ebfced-serverac86855d`.
Ten clean physical checkouts bind all selected repository and Solidity library
commits and trees. Effective Connect/SCTP `e1b5d77b5029` and SDK `5d37be3876e5`
remain unchanged. Go 1.26.6, Forge, Solc 0.8.24, Git and buildx executable
hashes, forty package payloads and the selected local OCI base are checked.
Both builds use source epoch `1790890833`. Checkouts, copied module
cache, fresh compiler caches, scratch and OCI stores stay on `/mnt/data`.
The copied module cache is retained input; it is not an independent dependency
acquisition. Final readback confirms ten clean source identities and the four
source-build tools are unchanged.

## Local construction and repeat

Builds A and B ran sequentially, with separate initially empty compiler caches.
Forge's ignored output was never written by concurrent source builds. They
completed in 647 and 660 seconds.
Each original sealed manifest retains 175 artifacts, all seventeen
executable roles, five exact fresh contract creation/runtime pairs, and all
twelve required migration source inputs. All seventeen binary hashes and ten
bytecode hashes match across A/B. Embedded build information binds each binary
to the expected clean VCS revision, Linux/amd64 and disabled CGO. Historical
and selected contract bytes remain distinct.

| Artifact | A SHA-256 | B SHA-256 |
| --- | --- | --- |
| Original source manifest | `00c8c5b8b648fd3154facdad1843dc6850eb52d6afdfa48c9d9af01a10f08eb1` | `ec2095935ef5f248ff06ddcefd92862224dbcebd196b917313951e1c989fa533` |
| Complete image aggregate | `1b0be30486385aeb55fa685858f6475e99f87faee9d4ff50e0be6a8021963e8c` | `7626f827be3b0fd5b15f06da4123b56f17f90ada2b7729f324c2d0dd4075415a` |

Each aggregate authenticates its original parent and seven-package/one-scratch
supplements, then verifies 342 source/image artifacts. All eight OCI platform
manifests, configurations and archive hashes match between repeats. Package
rootfs, OCI descriptor/layer/diff-ID and embedded binary checks pass before
the local source-to-image claim is true. Each image repeat uses a separate
initially empty Docker/containerd store. Both owned build-daemon pairs stop
and join; system daemons and application services are untouched.

Compared with [SN `bab49e1c` / server `a3e2e668`](release-bab49e1c-servera3e2-20261001.md),
17 binary hashes and 8 image platform digests changed, while
5 contract bytecode pairs remained equal. The exact comparison is retained
in `evidence/audit-compare.json`. The old release keeps its original scope;
none of its attestations is inherited. Manifests, timestamps and logs are not
asserted byte-identical.

## Inventory and qualification limits

All 95 builder/source-graph roots pass normally and under race; both packages
pass vet and the builder compiles. The selected server sampler's thirteen roots
pass normal/race and its two packages pass vet. The optional sampler stays off
without explicit bounded configuration.

Sixteen original component records are copied after hash and source-ancestry
checks. Their earlier server pins and behavioral scopes remain explicit. The
native bootstrap author record combines 155 completed race bodies from its
preserved timeout with the separate eleven-root passing continuation; the EVM
record similarly preserves the 45+7 split. Neither original timeout is relabeled
as a successful invocation. Independent component reports retain their narrower
scopes. These records and fresh build-tool tests do not establish exhaustive
composed application, grant-locking, database or production-service qualification
on this release pair.

The migration inventory retains twenty-five database/monitor source files and
751 catalogue entries. The unsigned release inventory covers 61 files totaling
807,641,307 bytes and repeats byte-for-byte. No live database is inspected or migrated.
Policy and published/deployed image identity remain missing categories.

Module qualification remains incomplete: SN has 636 graph nodes, of which
363 lack full retained qualification (349 lack `go.mod` metadata and
14 have metadata without source bodies); server has 647 nodes, with
372 incomplete (347/25). Offline builds and exact selected pins do not prove
exhaustive module-body or compiler/package provenance. These gaps remain in
the original manifests.

Only local source-to-bytecode/source-to-image verification and the stated
same-host output equality are true. Independent builder reproduction,
`reproducibility_verified`, `release_complete` and `deployment_approved` stay
false. Independent compiler/package provenance, arm64, archive restore,
SBOM/scanner/attestation policy, production configuration/policy, migration and
rollout readiness, published identity and running-image readback remain gates.
No signer, transaction, publication, deployment or application service starts.

## Terminal seal

Under the local build root, `evidence/release-receipt.json` has SHA-256
`46f4497e19a7487b4d23b2f3bd70c391c27ac2a1fcc72fcb172e66e463c0876f`.
The 961-entry `evidence/SHA256SUMS` has SHA-256
`2a0fa16593227cf4230c081d45f21b79970747487cd56d58dd59564d088aa40b`;
complete readback passes. Its log is `evidence/check-SHA256SUMS.log`. The seal
covers both original source manifests, supplements, aggregates, retained
artifacts, comparison, inventories, exact source/tool checks, component records
and daemon shutdown records; compiler caches and mutable scratch are excluded.

A separate read-only verifier checked both exact source graphs, all 175 artifacts
per candidate, the seventeen embedded VCS identities, ten raw bytecode outputs
and twelve migration inputs. Its primary receipt also checks the eight A OCI
archives, indices, configurations, layer/diff-ID commitments and final executable
bytes. The later addendum checks all eight B images and their equality with A.
Both receipts retain an artifact PASS and a launch NO-GO because the pending
EVM custody successor is outside this frozen source. Their shared host and local
toolchain do not establish independent compiler or dependency provenance.

Under `/mnt/data/sn-testnet/sol-mainnet-mg02-release-independent-20261001`,
`receipt.json` has SHA-256
`447429a5c0b2e51ff29653268456b44333e31e82eef9b3075672bb85ed89159f`;
`oci-ab-addendum.json` has SHA-256
`21581cb9a52b58fa8e6e6af16e87d3ae1d605b68e8046595383973c878bffde4`.
The addendum binds the original receipt without rewriting its A-only image scope.

The owner's separate acceptance of the exact official v470 artifact and its
documented rebuild exception is for launch planning only; it grants no release,
deployment, finality-checkpoint or signing authority to this bundle. Any later
source selected for deployment requires its own exact build and qualification.
