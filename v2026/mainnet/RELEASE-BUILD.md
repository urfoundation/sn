# Current production release composition

`go run -mod=readonly ./scripts/mainnet-release-build --config /absolute/config.json`
builds a local Linux/amd64 candidate from explicit clean Git commits and physical
checkout paths. It never starts an application, contacts a chain, signs, pushes
an image or deploys. The output directory must be new and outside the source
workspace. Keep scratch, compiler caches and output on a capacity-checked build
volume, such as `/mnt/data`; do not consume the small system volume implicitly.

For the pinned Go 1.26.6 toolchain, use physical clones with regular `.git`
directories. Its VCS discovery omits stamps for linked worktrees whose `.git`
is a file, even with `-buildvcs=true`; the release verifier correctly refuses
the resulting executable. Preserve that failed attempt and build the same
commits in a fresh output directory after correcting the checkout layout.
Do not disable embedded VCS checks. Keep Docker/containerd build storage on
the capacity-checked volume as well as the exported OCI files. With the
containerd image store, `--data-root` alone is insufficient: explicitly select
a separate containerd socket, root/state directories and namespaces, and verify
the daemon's actual selected socket before building. Do not reconfigure or prune
an existing daemon to make room for a release build.

This is the current role census, rather than the historical v11 seven-binary
selection:

| Source | Command packages | Retained outputs |
| --- | --- | --- |
| SN | `mainnet`, `cli/miner`, `cli/validator`, `cli/snclaim` | Four executables, their build settings, hashes and command receipts. The mainnet executable also contains root-service, root-monitor, operator-monitor, owner-signing, bootstrap-chain, bootstrap-contracts, activate-validators and repair-validator. |
| Server services | `cli/api`, `cli/taskworker`, `cli/proxy`, `cli/connect`, `cli/alt`, `cli/gossip`, `cli/mcp`, `cli/competitionworker` | Eight executables and eight binary-bearing image contexts. |
| Server maintenance | `cli/monitor`, `cli/strecovery`, `cli/competitiondbinit`, `cli/competitionpatch`, `cli/geolite2export` | Five executables and their build receipts. |
| Contracts | ReserveSink, SettlementVault, Coordinator, ValidatorEvidence, ERC1967Proxy | All five selected deployment artifacts beside freshly compiled Foundry artifacts and the unchanged historical catalogue; probe/drill dependencies are checked by the existing generator. |

The config schema is `urnetwork-mainnet-release-build-v1`. Required fields are
`candidate_id`, `workspace`, `output`, `version`, positive `source_date_epoch`,
the `go`, `forge`, `solc` and `git` tool objects (`path`, `sha256`), and
`repositories`. Each repository object supplies `name`, workspace-relative
`path`, full Git `commit` and `tree`. The exact repository census is SN, server,
proxy, glog, goidenticons, userwireguard, warp, and the three Solidity libraries
forge-std, openzeppelin-contracts and openzeppelin-contracts-upgradeable. The
first seven use their repository name as path; libraries use
`sn/evm/lib/<name>`. Tool paths must identify real executable files, not symlinks.
The caller must provision reviewed module/compiler dependencies before the
offline build. The manifest retains the complete config, input hashes, tool
version logs, compiler commands and exits.

`contract_catalog` selects the contract bytes explicitly. Omission or `retained`
keeps the checked-in `sim-testnet/contracts_gen.go` catalogue. `fresh` exports
the exact current compiler bytes for review as the first unsigned mainnet plan's
catalogue. Unknown values fail closed. Both modes keep the existing schema
`urnetwork-contract-release-artifacts-v1` and select the same five contracts.

Fresh mode retains `inputs/contracts_gen.go` and
`inputs/contracts-retained.json`, generates a separate
`inputs/contracts-fresh-binding.go`, and writes the selected
`inputs/contracts-release.json`. All four files are hashed in the manifest.
The generator's complete semantic check still runs against the checked-in
catalogue before selection. Fresh capture additionally requires unchanged ABI
(including constructors), normalized storage-layout hash and semantic immutable
references, and exact creation/runtime equality with every Foundry artifact.
Source files and historical output directories are never rewritten.

Each contract entry records `retained_*`, `selected_*` and `rebuilt_*` hashes.
`exact_bytes` and aggregate `source_to_bytecode_exact` compare selected bytes
with compiled bytes. Fresh mode refuses any nonexact contract; retained mode
refuses any selected identity that differs from the historical catalogue. The
manifest's `contract_catalog` is always explicit, including when config omits it.

The September 30 server API mismatch was reproduced before repair: four of
thirteen server commands compiled and nine failed. SN's four commands compiled.
Server `898dc8f3b211d1e2fca1b0a0c970f7673b36fd7b` first replaced stale sibling
SDK/Connect overrides. The October 1 source capture at SN `de0823ce` and server
`0b8e758d` confirms the following effective replacements in both module graphs
and in the builder's admission checks:

| Module | Effective immutable version |
| --- | --- |
| SDK | `v0.0.0-20261001021058-5d37be3876e5` |
| Connect | `v0.0.0-20261001021459-e1b5d77b5029` |
| `github.com/pion/sctp` | `github.com/urnetwork/connect/sctp v0.0.0-20261001021459-e1b5d77b5029` |

The [current unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
records newer observed Connect/SDK main heads separately; they are not consumed
or implicitly qualified. The earlier full candidate and eight-image aggregate
bind SN `2d53e6f2` / server `ecbf3aad`. Preserve those exact source claims; they
do not prove a successor binary or image built from the later server custody
and SN validator changes. That preparation inventory is deliberately partial
and keeps source-to-image provenance and release approval false. The later
[pre-Safe baseline](evidence/release-pre-safe-baseline-and-modes-20261001.md)
binds SN `1806b3b3` / server `0b8e758d`, repeats all seventeen executable and ten
contract bytecode outputs, and verifies its own eight-image aggregate after
the permission repair. The subsequent
[frozen Safe-source release](evidence/release-safe-source-20261001.md) binds SN
`095a2208` / server `0b8e758d` with its own repeated source builds, fresh OCI
receipts and complete local source-to-image aggregate. No historical
attestation is inherited; independent reproducibility and release/deployment
approval remain separate gates.

The [historical successor release](evidence/release-233ea2be-server942-20261001.md)
binds SN `233ea2be` / server `94229abb`, including passive-root host, owner
recycle transition and schema 751. Two fresh-cache builds match all seventeen
executables and five selected contract bytecode pairs; both complete
eight-image aggregates pass, with identical platform/configuration/archive
bytes. Its own source lock and repeated unsigned 56-file inventory preserve
the exact source identity. All 89 builder normal/race roots and vet pass.
This local repeat does not grant independent reproducibility or approval;
later reporting commits and predecessor attestations remain separate.

The [selected finality/migration successor](evidence/release-689938d6-server6c39-20261001.md)
binds SN `689938d6` / server `6c39d307`, preserving the effective dependency pins.
Two sequential builds with initially empty Go caches match all seventeen
executables and ten contract bytecode outputs. Each retains 175 artifacts,
including all twelve current migration implementations/catalogs. Both
eight-image aggregates verify 342 parent/supplement artifacts each, and all
platform/configuration/archive bytes match across separate initially empty
image stores. The 61-file unsigned inventory repeats exactly. All 95
builder/source-graph roots pass normal/race,
both packages pass vet, and the builder compiles. The superseded e4 attempt
retains its eight-input omission and interrupted repeats without qualification
being inherited. Independent reproducibility and release/deployment approval
remain false.

Both main modules resolve independently. The builder records their effective
module graphs, exact module/go.mod sums, local module Git ownership and module
file hashes. It retains the three API-bearing module zip files and SHA256
hashes, and runs `go mod verify` before and after compilation. `GOWORK=off`,
empty `GOFLAGS`, `GOENV=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`
and fixed compiler/platform settings prevent ambient workspace overrides or
automatic toolchain/module downloads. Binary build info must independently
match the selected source revision, clean state, package and Linux/amd64 target.
Source identities, module graphs, tool hashes and output bytes are rechecked
before the final domain-separated manifest seal.

Artifact copies set their declared permissions through the owned output
descriptor after private exclusive creation. Executable image inputs retain
`0755` and private inputs retain `0600` even under a restrictive build-host
umask; copying does not change the source file. The strict OCI readback still
requires the exact executable mode. The
[pre-Safe baseline and causal repair](evidence/release-pre-safe-baseline-and-modes-20261001.md)
retain the refused `0700` export, separate fixed outputs and exact source
boundaries. Historical output directories must not be repaired in place.

The first builder invocation is retained as a failed attempt: Go's lazy module
graph includes unused tool dependencies without `GoMod`/`GoModSum` fields. The
complete follow-up census found 634 SN nodes and 644 server nodes. Both graphs
have 347 nodes missing go.mod metadata (346 entirely lazy plus one unused cached
body); another fourteen SN and twenty-five server nodes have metadata without a
source body. The successor retains every node, explicit unqualified fields and
these counts. Every dependency actually linked into any of the seventeen
executables must have authenticated materialized source, matching identity and
body/go.mod sums; a graph-only node cannot qualify. Local build-info versions
use `(devel)`, normalized only for an exact pinned Git repository. The original
failed exit and its config are not replaced by the successor capture.

The [independent server qualification](/mnt/data/sn-testnet/mainnet-release-composition-sol-20260930/evidence/RESULT.md)
reproduced all thirteen current server builds and all three source-graph roots
in normal and race modes, with vet. Its SHA256SUMS digest is
`16f069e0a8c62785a1e54c6a206454b6c2c9f4ff5e9d4d61daa2a0ab32f1aed8`.
That receipt covers server `898dc8f3`, not this builder or an approved composed
release. The author's corrected seventeen-command census is separate compile
evidence. The subsequent [fresh-catalogue qualification](evidence/fresh-contract-catalogue-build-20260930.md)
records all 31 builder roots passing normal/race, vet, independent causal controls
and a complete sealed output with ten exact contract byte pairs. That receipt
qualifies the builder and local composition; it does not infer application
behavioral qualification from successful executable compilation.

Every image context retains its production Dockerfile verbatim and a fresh copy
of the selected binary, with both copies' hashes joined to the binary manifest.
The package lock, all eight original image Makefiles, and database/signal
migration source files are retained. Proposed local build arguments use a
fixed source epoch and timestamp rewriting for all eight services, including
the scratch competition worker. The source-composition `--config` mode does not
invoke Docker. Its contexts have no runnable OCI digest or embedded-binary/rootfs verification;
`missing_images` lists all eight and `source_to_image_verified` stays false.
Image package-archive/attestation policy and environment selection remain open.

`go run -mod=readonly ./scripts/mainnet-release-build --image-config /absolute/image-config.json`
adds a separate, bounded scratch-image supplement to a frozen composition. It
requires schema `urnetwork-mainnet-scratch-image-build-v1`, `candidate_manifest`
(`path`, full file `sha256`), a new disjoint `output` directory, a pinned `buildx`
executable (`path`, `sha256`) and a physical local `docker_socket` path. The
original manifest and its domain-separated seal are checked without rewriting
them. All eight selected binaries, Dockerfiles and context copies are rehashed.

This mode admits only the exact existing competition-worker scratch Dockerfile.
It copies its two inputs into the new output and uses internally constructed
arguments, a private empty Docker configuration, the local default builder,
`--no-cache`, `--network=none`, the original epoch, timestamp rewriting and a
remote-source DENY policy. SBOM and provenance collectors are explicitly disabled
to avoid additional image resolution; their policy gates remain open. The
offline boundary is the fixed scratch/COPY source graph and source policy, not
an attestation of daemon-wide network isolation. No application runs, no image
is loaded or tagged, and no image is pushed or deployed.

The OCI archive is verified as data without extraction. The reader checks every
referenced blob's SHA256 and length, one Linux/amd64 platform manifest, its exact
runtime configuration, one compressed layer and its full uncompressed diff ID.
The rootfs must contain exactly one root-owned mode-0755 regular file at
`/competitionworker`, equal in length and SHA256 to the parent's binary. Links,
whiteouts, duplicate or extra paths, ambiguous JSON and oversized decompression
fail closed. Builder metadata must independently agree with the read-back
platform/config digests. Input, executable and archive hashes are rechecked
before writing the new domain-separated `image-receipt.json`; failed output and
command exits are retained and cannot be overwritten by a retry.

The supplement can mark only that image's `source_to_image_verified` true. Its
aggregate `source_to_image_verified`, `reproducibility_verified`,
`release_complete` and `deployment_approved` stay false, and `missing_images`
contains the other seven roles. Those production Dockerfiles use remote `ADD`
and an Ubuntu base: `--network=none` fences `RUN`, not source fetching. The
scratch mode cannot build them. Runtime behavior, attestations, independent
builder reproduction and arm64 remain open.

`go run -mod=readonly ./scripts/mainnet-release-build --package-image-config /absolute/package-image-config.json`
adds a separate bounded supplement for all seven Ubuntu service recipes. Its
schema is `urnetwork-mainnet-package-image-build-v1`. The required
`candidate_manifest`, `output`, `buildx` and `docker_socket` fields follow the
scratch config. `base_layout` names a physical local OCI layout directory;
`packages` maps all forty IDs from the exact retained
`runtime-packages.lock.json` to local `path` and `sha256` pins. Missing,
modified or aliased inputs fail without a download or package-resolution fallback.
The base layout and output must be disjoint, and each attempt needs a new output.

The base is independently authenticated from the original production
multi-platform index digest through its unique Linux/amd64 descriptor, config
and compressed layer. Only that selected chain and the original index blob are
copied into a private local OCI layout. Other architectures and base attestations
are not claimed. The builder admits only the seven exact reviewed production
Dockerfile hashes and the exact package-lock hash. It retains every original
recipe and mechanically replaces its two pinned `FROM` locations with the
`offline-ubuntu` named OCI context and each literal checksum-pinned remote `ADD`
with a verified local `COPY --chmod=0600`. Package installation, application
binary paths, command arguments, environment, stop signal and credential symlink
instructions remain byte-for-byte unchanged. All forty package payloads remain
in the build-stage input census; only the selected architecture is installed.
The shared server source recipes are never edited.

The fixed build invocation uses the private local OCI context, fresh local
package and binary copies, the original source epoch, timestamp rewriting,
no cache, network-none build steps and the same remote-source DENY policy.
Every copied source is rehashed before each image invocation and before sealing.
SBOM and provenance collectors remain disabled. This closes the selected source
graph; it does not attest daemon-wide network isolation. The original pinned
Ubuntu base may already be cached, but cache presence alone is not accepted as
proof of its identity or as the offline boundary.

OCI readback authenticates every descriptor, compressed layer and complete
uncompressed diff ID. The output must have the exact original base layer plus
installation, executable and credential-link layers, one Linux/amd64 platform
and the exact reviewed runtime config. A bounded logical rootfs reader handles
directories, regular files, symlinks, existing-target hardlinks and OCI
whiteouts without filesystem extraction. It rejects path traversal, duplicate
paths within a layer, writes through link parents, unsupported inode types,
extended metadata other than decoded PAX path/linkpath, malformed archive tails,
external descriptors and decompression/count limits. Directory type replacement
and later changes to hardlink targets are refused rather than approximated.
It verifies the final
root-owned mode-0755 executable against the parent's bytes, all selected package
versions and installed states, the linker cache, CA bundle, OpenSSL, proxy's curl,
and the credential symlink. Build-only package payloads and the two removed
volatile files must be absent. A sorted content/inode census binds the entire
logical final rootfs, while builder metadata must independently match the platform
and config digests.

The new receipt retains original and localized recipes, local base closure,
package copies, input pins, command logs/exits and all seven OCI archives and
readbacks. The parent remains unchanged. Per-image verification can become true
only after readback; aggregate source-to-image, reproducibility, release completion
and deployment approval remain false. `missing_images` contains the scratch
worker because this supplement does not merge the earlier scratch receipt.
Successful local construction does not qualify application runtime behavior,
production configuration, vulnerability or attestation policy, arm64, a durable
input archive and restore process, or an independent builder. No image is loaded,
run, tagged, pushed or deployed by this mode.

`go run -mod=readonly ./scripts/mainnet-release-build --aggregate-image-config /absolute/aggregate-config.json`
verifies and combines those original receipts without building anything. Its
schema is `urnetwork-mainnet-image-aggregate-v1`; the config contains a pinned
`candidate_manifest`, exactly two pinned `supplements` (each a `path` and full
file `sha256`), and a fresh `output` directory disjoint from every input directory.
One supplement must use the scratch schema and one the seven-service schema.
There are no tool, socket, network, signing or approval configuration fields.

The aggregate reader authenticates the parent and supplement file hashes and
their original domain-separated content seals, then requires the same candidate,
parent file hash and parent content hash throughout. It rehashes every inventoried
artifact, including non-image binaries, contracts and logs; checks all seventeen
binaries' source/module build information against the original parent; and
replays each OCI/rootfs inspection against the parent's binary. It also verifies
the retained base, package lock and payloads, exact localized recipes, fixed
build command/environment, successful exits and builder metadata. Physical paths
cannot contain symlinks. Duplicate or missing image coverage, stale seals,
mixed parents, changed bytes, ambiguous JSON and unsupported claims fail closed.
Every original receipt and artifact is checked again before the result is sealed.

The new `image-aggregate.json` retains the original source identities, eight
source/binary/recipe/archive/platform joins, all three input file/content pins,
and hashes of verbatim metadata copies in its own `inputs/` directory. Only its
`source_to_image_verified` becomes true and `missing_images` becomes empty.
The original manifest and supplements remain unchanged, with their original
partial-coverage flags. The aggregate always leaves `reproducibility_verified`,
`release_complete` and `deployment_approved` false. It is a local integrity
attestation, not a signature or authorization. It neither rebuilds nor copies
large artifacts, so the three pinned input directories must remain available
for later re-verification. A failed attempt cannot overwrite an earlier output.
The [aggregation qualification](evidence/release-image-aggregate-qualification-20261001.md)
records the current candidate's complete local linkage and the remaining gates.

The September 30 retained-catalogue full-source Foundry build used solc 0.8.24, Cancun, optimizer
200 and via IR, and completed successfully. ReserveSink, SettlementVault and
ERC1967Proxy match retained creation/runtime bytes exactly. Coordinator and
ValidatorEvidence retain their prior deployment bytes, while current coherent
source compilation changes their metadata digest. Comparing available compiler
metadata identifies the changed imported `src/STSettlementVault.sol` content
from `3b39de98`; settings and paths match. The generator's complete semantic
check passes: executable and constructor bytes outside the narrowly recognized
Solidity metadata digest, ABI, normalized storage layout and semantic immutable
offsets match. This is not byte-for-byte source-to-deployment reproduction.
Coordinator runtime is 24,564 bytes, twelve below the 24,576-byte deployment
limit; retained and fresh sizes remain checked. The retained-mode manifest
records both compiled and historical hashes and cannot turn metadata tolerance
into exact equality. Fresh mode selects all five current compiler artifacts
explicitly and records their exact equality separately from historical bytes.

No signed mainnet deployment plan has been evidenced. The checked-in catalogue
is historical release/testnet material, not proof of an immutable mainnet
commitment. The preferred first-plan path is explicit fresh selection, review
and independent qualification of that catalogue, then binding its exact path
and SHA256 in the unsigned bootstrap contract specification's `artifacts`
reference. The
existing loader in `mainnet/contract_artifacts.go` already consumes that schema;
it does not require a source-code catalogue replacement.

Before signing the first mainnet plan, the release owner must check for any
externally held signed artifact, plan or transaction commitment. Absence from
this repository is not proof that none exists. Any such commitment must remain
unchanged and be reconciled under its actual authority before a different
catalogue is selected. Historical source reconstruction or a separately
approved metadata-equivalence exception remains a conditional fallback; this
fresh path selects neither exception nor authority to change signed bytes.
No plan is signed and no contract is deployed by the builder.

`source_to_bytecode_exact` is calculated per artifact and in aggregate;
`reproducibility_verified`, `release_complete` and `deployment_approved` remain
false. Further gates include an independent second build, complete compiler
installation attestation, current OCI builds and readback, arm64 qualification,
approved runtime config/policy and migrations, and production-path qualification.
This increment supplies reviewable current source and binary evidence; MG-02
remains open.
