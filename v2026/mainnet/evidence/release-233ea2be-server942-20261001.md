# Frozen successor release source, 2026-10-01

The selected candidate binds SN
`233ea2be26092fec2403cb99fad969db422caa97`
(tree `893a78524dc60921bf2f5543e7e8a47e309cc2a8`) and server
`94229abb02819cea97f972421972b4e79c1a32ab`
(tree `61a5f30830c44b99f14583a1c5e8ad88d6a70908`). It includes passive-root
host composition, the owner recycle-mode transition and the schema-751
mixed-writer guard. Two local builds reproduce all seventeen executables,
five contract bytecode pairs and eight OCI images. Independent reproduction
and release approval remain open.

Evidence is retained under
`/mnt/data/sn-testnet/mainnet-successor-release-astra-20261001/final-233ea2be-server942`.
Ten clean physical Git clones preserve the exact source closure. Effective
Connect and SCTP remain `v0.0.0-20261001021459-e1b5d77b5029`; SDK remains
`v0.0.0-20261001021058-5d37be3876e5`. Both effective module graphs are retained,
with archive/materialization checks for linked dependencies and explicit
unqualified status for incomplete lazy graph nodes. The selected candidate's
own `sn-mainnet` executable produces its fresh source lock. Later reporting
commit `7c5d964f` changes three Markdown files only; it is not substituted for
the actual binary/image VCS identity.

Source builds A and B complete in 601 and 603 seconds with separate empty Go
caches. Each retains 17 executables, five freshly selected contracts, eight
image contexts and 170 sealed artifacts. Every executable has the exact clean
VCS revision. Fresh compilation verifies selected creation/runtime bytecode,
ABI, constructors, storage layout and semantic immutable references while
preserving the historical contract catalogue. Pinned Go 1.26.6, Forge and
Solc 0.8.24 run with offline module access, Linux/amd64, `CGO_ENABLED=0`,
`GOEXPERIMENT=greenteagc`, `-trimpath` and source epoch `1790859936`.
`go mod verify` runs before and after each source build.

Separate byte readback finds all 17 executable hashes and ten selected
creation/runtime hashes identical between A and B. Compared with the earlier
SN `6c801a25` / server `720e7c61` packaging baseline, all seventeen executables
and all eight image platform manifests change; all five selected contract
bytecode pairs remain equal. No predecessor attestation is inherited, and no
production source or builder change was required for this candidate.

Both image sets pass all eight OCI/rootfs/binary joins: API, task worker,
proxy, connect, alt, gossip, MCP and competition worker. Each separate
aggregate rehashes 337 source and supplement artifacts (170 + 150 + 17), with
no missing image. Seven package images use the pinned local base and forty
package payloads; the scratch image contains its exact non-root executable
with `0755` permissions. Image construction uses `--no-cache`,
`--network=none`, denied external source acquisition and the fixed source
epoch. Package builds take 398 and 456 seconds, scratch builds 53 and 61
seconds, and aggregates 173 and 183 seconds. The final comparison finds
identical platform manifests, runtime configurations and entire OCI archives
for all eight roles. Only local `source_to_image_verified` becomes true;
`reproducibility_verified`, `release_complete` and `deployment_approved`
remain false.

The repeat uses the same host with separate initially empty containerd and
Docker stores. Both daemon pairs have explicit independent root, state and
socket paths on `/mnt/data`; bridging and firewall modification are disabled.
All build caches, large checkout bytes and exports stay on that volume. Both
temporary daemon pairs are stopped and joined after their image receipts
complete. Existing system daemons are untouched. No application service is
started, image published, transaction signed/submitted or deployment performed.

All 89 roots in `./scripts/mainnet-release-build` pass normally (0.980 seconds)
and under the race detector (7.230 seconds), with no failures or skips; builder
vet exits zero. Exact commands use `go test -mod=readonly -p=2 -count=1 -json`,
with `-race` for the second stream, and `go vet -mod=readonly` for that package.
These tests qualify the builder, including permission/input and incomplete
aggregate refusal, rather than every application's production behavior.

The seal also authenticates the owner's author 29-root and independent
24-root normal/race/vet receipts, including their 39-file and 24-file
manifests. They qualify original SN `b3880266` and server `a464bb3e` source
trees, which equal the selected merged trees above. Earlier diagnostic and
broader predecessor streams are not counted as new final-source passes. These
receipts establish no live policy, custody or signing authority.

The unsigned release inventory selects 56 files totaling 807,017,685 bytes
and is byte-identical on repeat. Missing categories remain `policy` and
`image-identity`; local image receipts do not invent a registry identity or
approved production configuration. Its `release_complete`,
`provenance_proven` and `deployment_approved` fields remain false. The migration
inventory retains twenty committed source files and counts 751 catalogue
entries. No production database is inspected or migrated, and no archive
restore, cutover or subscriber-v2 rollout is qualified.

Independent builder reproduction, arm64 execution, actual service/configuration
and policy qualification, migration/archive/restore, compiler/package
provenance, attestation/SBOM/scanner policy, publication/deployment identity and
release approval remain gates. Current live identity, global custody and
native outcomes require their separate authorized evidence. This packaging
result neither executes the owner transition nor starts either validator.

| Retained file | SHA256 |
| --- | --- |
| `candidate-a/manifest.json` | `5df023da7334fd3e0e7454fed327a8cdfb8828b9dc510a634befab24e34034c6` |
| `candidate-b/manifest.json` | `775fbdbdd9e8a4341f0b80da647d45495c9c03d250883211b89eeff86e2517ca` |
| `evidence/source-lock.json` | `7b75e47760a0589942427fea77ef405ab428e4be871088aebab07557b728651e` |
| `aggregate-a/image-aggregate.json` | `6e8d747735899b58733c4f42350749377a6d4d1cc26fe95e31ca1ece78e0d57a` |
| `aggregate-b/image-aggregate.json` | `1522120d96c936e0db2f76dfe85af0037a4f849863775a81f602778c3c845a60` |
| `evidence/release-inventory.json` | `eefbfe00ac61296176dcd605e984f56e9d8f0bbe93795d7f3e30ef3ff02e22d1` |
| `evidence/audit-compare.json` | `02661c8d490015c09acc347983dbfdf593266ec60b43295e6fa73be17a59a6df` |
| `evidence/builder-qualification.json` | `9e39f44a6069d467c3c74fb31debaf8f4e353eb2c4a8567d2ad547673a1b7fae` |
| `evidence/qualified-upstream-gates.json` | `99281dd1fb97e2682628b193ad64835030f27412dc4a58aac2cbf3473c395ea5` |
| `evidence/migration-inventory.json` | `52819e23c506bdc898a9bfa7e3a37d1af4e1b147e6e988a684f4fbf11ef3757c` |
| `evidence/release-receipt.json` | `1bf7f3befda508e8743c5aea8fbcdbe8a5b4da08c3929ac06f0bbbaec683112c` |
| `evidence/SHA256SUMS` | `254314bb5e2db1867f8bba860c27b0154c040d041c43d74130508ba42db8daae` |

The [sealed release receipt](/mnt/data/sn-testnet/mainnet-successor-release-astra-20261001/final-233ea2be-server942/evidence/release-receipt.json)
retains both builds, comparisons, exact qualification and approval limits.
All **859 selected files** pass final checksum readback, recorded in
`evidence/check-SHA256SUMS.log` with exit zero. Final source/tool readback confirms
all ten original clean repository identities and unchanged compiler/Git
executable hashes. This documentation commit is distinct from the frozen
binary/image source. The previous
[packaging baseline](release-6c801a25-server720-20261001.md) and
[pre-owner preparation](release-successor-preparation-20261001.md) retain their
original evidence and scope.
