# Original EVM custody successor: local release qualification

This successor packages the [original eight-action custody writer and borrowed-reader corrections](original-evm-custody-qualification-20261001.md),
including the preserved approval/predecessor diagnostic. Its frozen source is
SN `1d580d5e60e569c346cf2aaba2f7cf12eed8c032`, tree
`baeefa28ef6b9c3cd718404994159f2393e6b5f0`, and server
`ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`, tree
`c5c35ead1c4c53f218b0c5c6f6455b5e516ea1dd`. It replaces the
[SN `28ebfced` baseline](release-28ebfced-serverac86-20261001.md) for local
artifact selection. Later reporting commits do not advance this build's pins.
MG-02 and mainnet activation remain open. The separately pending five-marker
bootstrap readiness/passive-root custody correction is outside this frozen
source and requires another exact build if selected. This local artifact
baseline cannot qualify that P0 issue.

The release root is `/mnt/data/sn-testnet/mainnet-custody-release-astra-20261001/final-1d580d5e-serverac86855d`.
All ten repository and Solidity-library commits were freshly fetched directly
from their remotes into physical `/mnt/data` checkouts. Exact HEAD/tree and clean
status were checked before and after construction; no Git object alternates
point to the root device. No source read error was observed. Source checkouts,
copied module cache, compiler caches, scratch and isolated OCI stores remain on
the data volume. The existing compiler and executable tool paths remain pinned
inputs. Local cache copying and executable hashes do not establish independent
compiler or dependency provenance.

Connect/SCTP `e1b5d77b5029`, SDK `5d37be3876e5`, Go 1.26.6 and Solc 0.8.24
remain unchanged. Go, Forge, Solc, Git and buildx hashes, forty package payloads
and the OCI base were checked. Both builds use source epoch
`1790895488`. The server source pin is unchanged.

## Source, bytecode and OCI repeat

The two source builds ran sequentially with separate initially empty compiler
caches; they completed in 599 and 600 seconds. Shared ignored Forge
outputs were not written concurrently. Each manifest retains 175 artifacts:
all seventeen executable roles, five fresh creation/runtime contract pairs and
all twelve required migration inputs. All seventeen executable hashes and ten
bytecode outputs match across A/B. Embedded build information confirms the
expected clean VCS revision, Linux/amd64 and disabled CGO.

| Artifact | A SHA-256 | B SHA-256 |
| --- | --- | --- |
| Source manifest | `85363d87ad89fc5ba981be27966f8247831bccf6176f02bde385921908205ec2` | `303fb4ba8022217665c8caff44dd7c6d875690bf94a44f46ee716cca181d3f0b` |
| Complete image aggregate | `48a5572864bad9bc1bb8a4eaa7dcf86d0df2606b3e7b2dcbcb03034185ab006f` | `a2afac85d541d1f1e8178baafad7f1097adbb727b076dc45f49b49717247c1ba` |

Each image repeat uses separate initially empty containerd/Docker storage and
builds seven package images plus one scratch image. Each aggregate authenticates
its original source parent and both supplements, verifying
342 source/image artifacts. All eight OCI platform manifests,
configurations and archive hashes match across repeats. Descriptor, layer,
diff-ID, package rootfs and embedded executable readbacks pass. Both owned
build-daemon pairs stopped and joined; system daemons and application services
were untouched.

Compared with SN `28ebfced` / server `ac86855d`,
14 executable hashes and 8 platform digests changed;
all 5 contract bytecode pairs remain equal. The precise comparison is
retained in `evidence/audit-compare.json`. Predecessor receipts retain their
original scopes. Manifest timestamps and logs are not claimed byte-identical.

## Independent artifact readback

The separate [primary receipt](/mnt/data/sn-testnet/sol-mainnet-custody-successor-independent-20261001/receipt.json) verifies both 175-artifact source
candidates, all seventeen binaries, ten bytecodes, twelve migration inputs,
source/tool/module identities and all eight A OCI images. Its SHA-256 is
`99f58e12f7dcf401adfcfd68a089f0b96820951d46c12c29ed174a8c617c877e`.
The later [B-image addendum](/mnt/data/sn-testnet/sol-mainnet-custody-successor-independent-20261001/oci-ab-addendum.json) verifies all eight B archives, complete
OCI layers/rootfs and embedded executables, and exact A/B image equality. Its
SHA-256 is `797c9090ff00feb1f845ca2c44dd38543f899f1364f68be0e2c6ebe7db343cdf`; it binds the unchanged primary receipt separately.
Both report artifact `PASS` and launch `NO_GO_PENDING_PASSIVE_ROOT_CUSTODY_FIX`.
These are separate same-host byte audits, not an independent compiler/build
reproduction or deployment approval.

## Component and inventory scope

All 95 builder/source-graph roots and thirteen selected server sampler roots
pass normal and race modes. Both package pairs pass vet; the builder compiles.
The optional sampler remains off without its explicit bounded configuration.

The 20 retained component records have exact hash and source-ancestry checks.
The new custody inclusion record additionally verifies that all Go and module
files match diagnostic successor `4e6b4a7e`. The original 107-file custody author
manifest passes full readback. Writer `cb9f3aa2`, reader `1922981d` and diagnostic
successor `4e6b4a7e` remain distinct qualification sources. The writer's 71-root
race union is 38 + 22 + 3 + 8: the original 33-root timeout remains failed,
and only its eleven unpassed names are supplied by the disjoint continuations.
The reader security pair, adjacent readiness/recovery selections, causal
controls and independent checks retain their exact narrower scopes. The failed
adjacent diagnostic invocation and invalid initial baseline build are preserved.

Earlier native/EVM finality records likewise retain their 155+11 and 45+7 race
splits and failed package timeouts. No historical component receipt or fresh
builder test is reinterpreted as exhaustive current composed application,
database, grant-locking or production-service qualification. Older root/trim
stores and the separate five-marker readiness cohort remain explicit custody
follow-ups outside the corrected original eight-action scope.

Twenty-five database/monitor source files retain 751 catalogue entries. The
unsigned 61-file inventory totals 807,649,390 bytes and repeats byte-for-byte.
Neither a database inspection nor migration was performed. Policy and
published/deployed image identity remain missing inventory categories.

SN has 636 module graph nodes, including 363 without complete retained
qualification: 349 lack `go.mod` metadata and fourteen have metadata but no
source body. Server has 647 nodes, including 372 incomplete: 347 lack metadata
and twenty-five lack source bodies. Offline build success does not close these
module-body provenance gaps.

## Terminal evidence and remaining gates

`evidence/release-receipt.json` has SHA-256
`8fba264e5d1fcac41981252c57c55684b088713ebb99c969c75a6242347f229b`.
The 978-entry `evidence/SHA256SUMS` has SHA-256
`69cfb66647620bb8dc0e85bfa2d3980710ec224cee5f3e4cdf22932a04c08ec3`. Every entry passes full readback in
`evidence/check-SHA256SUMS.log`. The seal covers source manifests, OCI
supplements/aggregates and artifacts, comparison, inventories, component
records, final source/tool checks and owned-daemon shutdown records. Compiler
caches, mutable scratch and the documentation worktree are outside that seal.

Only local source-to-bytecode/source-to-image verification and the stated
same-host output equality are true. Independent builder reproduction,
`reproducibility_verified`, `release_complete` and `deployment_approved` remain
false. Independent compiler/package provenance, arm64, archive restore,
SBOM/scanner/attestation policy, actual production configuration/policy,
migration/rollout readiness, published identity and running-image readback
remain gates. No live signature, chain transaction, publication, deployment or
application service start occurred.

The exact official v470 artifact's accepted rebuild exception is for launch
planning only. It grants no live runtime authority, signing or deployment
approval to this local release. Independent network/checkpoint approval,
production custody, acceptance, funding and both validator roles remain open.

## Fresh unsigned planning supplement

After the release seal, its exact `candidate-a/binaries/sn-mainnet` executable
(SHA-256 `e50485dfd4b0396f74e083ef5f5216a7f70af9c0caa3743f1df9de4bd539598e`) ran
`finalized-snapshot --rpc https://archive.chain.opentensor.ai --retry-window 300s`
from 23:52:18 to 23:52:26 UTC. No expected-network flags were used as approval.
The combined observation pins native and EVM block **9,191,346**:

| Field | Retained value |
| --- | --- |
| Native finalized hash | `0xdfb60527f57a0291c6bdad2429562f8acbd8d0fae3aaee79b9c29d59b10e8739` |
| EVM hash | `0x9e3b71b06b5fdc13ad333ba8b22d6cc790e576726cbdd6a8ad64d4a5359941dc` |
| Runtime tuple | `node-subtensor`, spec 470, transaction 1, state 1 |
| Runtime code Blake2b-256 | `0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47` |
| Runtime metadata Blake2b-256 | `0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34` |
| Snapshot file SHA-256 | `911f8f2241d038d5c1c70664d2d035d8d8c7a9052ddc95c6771c9818fde36783` |
| Snapshot content seal | `sha256:ddb43735451efa2ecdea88072f0ce3b932142ec3343fe017aad4146cb8aa5056` |
| Blocked-plan file SHA-256 | `3e27b38c71f868a9655eaafd84204611d603de1009516ec6c363090a70a370cb` |
| Blocked-plan content seal | `sha256:72a6c90b90b6ef261e8f48f6cdf5ca6ce80cc88ac889c7be3521687e7d0aefb8` |

The snapshot stays `unapproved_observation`, with `runtime_source_proven=false`
and `finality_authority=owned-rpc-assertion`. The public route observation does
not independently approve network identity, the checkpoint or live runtime use.
The separately declared Bittensor/mainnet-genesis/EVM964 target remains a review
expectation. The exact v470 artifact planning exception grants no live gate.

The source lock and repeated 61-file inventory come from this sealed release.
The release receipt is supplied only as unvalidated production-qualification
review material. Two offline `plan --config planning/plan-config.json` runs exit
zero and produce identical bytes. All ten action statuses are preserved:

| Action | Status | Executable |
| --- | --- | --- |
| `qualify-release` | blocked | false |
| `review-authority` | blocked | false |
| `reset-miner-uids` | blocked | false |
| `install-contracts` | blocked | false |
| `register-subnet-roles` | blocked | false |
| `start-root-validator` | blocked | false |
| `start-ur-validators` | blocked | false |
| `admit-operations` | blocked | false |
| `activate-native-miner-emissions` | blocked | false |
| `accept-and-reconcile` | blocked | false |

`apply_authority=false` and `activation_ready=false`. Of thirty requirements,
twenty-eight remain `missing`; only `binary-artifacts` and
`production-qualification` are `supplied_unvalidated`. `owned-rpc` and
`runtime-authority` remain missing. The plan carries no signing, submission,
service-start or deployment authority.

The separately sealed [planning receipt](/mnt/data/sn-testnet/mainnet-custody-release-astra-20261001/final-1d580d5e-serverac86855d/planning/planning-receipt.json) has
SHA-256 `b2f77fe06e652c84b2ed72b2e5a5208ab5651b223256387effa60d8407af6318`. All nineteen entries in
[its checksum manifest](/mnt/data/sn-testnet/mainnet-custody-release-astra-20261001/final-1d580d5e-serverac86855d/planning/SHA256SUMS) pass; the manifest SHA-256 is
`4b9becdd6249e6a3068d10dafc2859bf4e1300b5fcd6cbcb1a077c1c9d73d141`. Commands, raw output, exact input references, both
plans and every requirement description are retained alongside it. This
supplement does not alter or extend the earlier build receipt's scope.

A separate [independent planning audit](/mnt/data/sn-testnet/sol-mainnet-custody-successor-independent-20261001/planning-independent.json) has SHA-256
`b66710620de534e8ea60182450020b4cf1ee40e0757b8c51ec930e44a800498f`.
It verifies all nineteen checksums, the exact binary/source-lock/config bindings,
snapshot and plan seals, common finalized identity, all ten blocked actions,
requirement statuses, repeated bytes and command records. It performs no fresh
RPC corroboration and grants no independent network or deployment authority.
