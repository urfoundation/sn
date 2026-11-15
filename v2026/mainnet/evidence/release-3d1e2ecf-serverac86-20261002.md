# Readiness custody: qualified local release baseline

Date: 2026-10-02. This local successor packages the
[qualified five-marker/passive-root correction](bootstrap-readiness-custody-qualification-20261001.md)
at frozen SN `3d1e2ecfadf75e33341830a86b6e1871b050840a`, tree
`ccc2f792d04cd37bda57eaa3accd02128e58122e`, paired with server
`ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`, tree
`c5c35ead1c4c53f218b0c5c6f6455b5e516ea1dd`. It supersedes the
[SN `1d580d5e` local artifact selection](release-1d580d5e-serverac86-20261001.md),
whose receipts remain unchanged. Both the author and independent readiness
qualifications reached terminal scoped PASS before this release was sealed.
The initial preparation record's conditional status is historical. This is a
**qualified scoped baseline, superseded for launch** by the later owner-custody
source `21640419806f21f37a1f144bcb978770ac7ef962`, which requires its own exact
release composition. That source is absent from these frozen artifacts. **MG-02
and mainnet activation remain open.** No live signing, publication, chain submission,
deployment, application service start or database mutation occurred.

The release root is `/mnt/data/sn-testnet/mainnet-readiness-release-astra-20261002/final-3d1e2ecf-serverac86855d`. Nine exact source pins were freshly fetched from
their remotes. The initially unpublished SN commit was fetched by Git transport
from its isolated `/mnt/data` repository, as authorized by the release owner;
its exact commit/tree match the qualified source. A subsequent direct remote
fetch of main `e12850f0` confirms the frozen source is an ancestor and all Go/module
files are equal; this observation does not retarget the release. All ten
checkouts have physical Git directories and no object alternates. Source and tool fences pass before and
after construction. Subsequent main/reporting commits do not retarget this build.
All new source, cache, scratch, docs and OCI stores are on `/mnt/data`; no source
read error was observed. The host's recorded root-device media error remains a
separate qualification-host limitation.

## Source and image evidence

Both source builds ran sequentially with separate initially empty compiler
caches, taking 573 and
590 seconds. Each manifest retains
175 artifacts: seventeen executable roles, ten fresh creation/runtime bytecode
outputs and twelve migration inputs. All seventeen executable and ten bytecode
hashes match A/B. Embedded metadata records the exact clean source revision,
Linux/amd64 and disabled CGO. Source epoch is `1790897319`.

| Repeat | Source manifest SHA-256 | Complete image aggregate SHA-256 |
| --- | --- | --- |
| A | `b8cfa5ae12262cf92ba551cff60ef20b252ad9bb8d1663aa34dd1ba0bc3a69ff` | `b1e7cdac1b8bbd7b663d4b12cf230d203a16fb6f8fb69018de16b5762695d08f` |
| B | `fb0a82afe4bf603ef73f1951a014f8d2530fb88d17dbe3170bf3aa239903ef49` | `c861e3040792ca80f4d61ea730d475e458d80f47e0a17240ad44296dcaa576f0` |

Each repeat builds seven package images and one scratch image in its own
initially empty containerd/Docker store. Each aggregate authenticates 342
source/image artifacts through its parent and supplements. All eight platform
manifests, configurations and OCI archives match across A/B. Descriptor, layer,
diff-ID, package filesystem and final executable readbacks pass. Both owned
build-only daemon pairs stopped and joined; system daemons and application
services were untouched. Against the predecessor, 14
executable hashes and 8 platform digests changed;
all 5 selected contract pairs remain equal.
Logs and manifest timestamps are not claimed byte-identical.

All 95 builder/source-graph roots and thirteen selected sampler roots pass
normal/race; both package pairs pass vet. The sampler remains opt-in and bounded.
Go 1.26.6, Forge, Solc 0.8.24, Git and buildx hashes, forty package payloads and
the OCI base retain authenticated input identities. Connect/SCTP `e1b5d77b5029`
and SDK `5d37be3876e5` are unchanged. Reused module inputs were checked against
the sealed predecessor graphs: 274 directory
bodies match their Go `h1` sums, 298 metadata
files match SHA-256, and three selected module archives match. This authenticates
reuse; it does not establish independent compiler/dependency provenance.

The first added cache verifier rejected a missing sum in one project's graph
when the other project contained that module. Its failed invocation and script
are preserved. The corrected verifier keeps both qualification views and only
compares nonempty authenticated sums; no conflicting module bytes were found,
and no release build was restarted.

The first finalization wrapper exited 1 because it expected the predecessor
independent-addendum field `parent_receipt_sha256`; the new schema instead binds
direct A/B aggregate hashes. The failed adapter and terminal record remain in
`evidence/independent-binding-attempt-1.py` and
`evidence/finalization-attempt-1.json`. A checkpointed continuation resumed
independent binding and sealing, then ran the new planning commands. It did not
rerun source/image builds, comparisons, inventories or completed source fences.

The reviewer then requested explicit equality between the primary receipt's A
manifest/audit hashes and the addendum's A manifest/audit hashes. The base receipt
had already been written. Its owned seal helper was paused before the checksum
manifest was written; a separate guard passed both exact assertions before the
helper resumed unchanged. The completed external guard binds the unchanged
base receipt and full checksum readback, both immutable independent receipts,
the independent join audit, and the original failure. Read the base receipt
together with this guard; the base evidence itself was not rewritten to imply
those later checks had already run.

SN retains 636 module nodes with
363 incompletely qualified; server retains
647 with 372
incomplete. The original per-project missing metadata/body scopes remain open.
Twenty-five database/monitor source files retain 751 catalogue entries. The
61-file inventory totals
807,688,508 bytes and repeats exactly.
Policy and published/deployed image identity remain missing inventory categories.

## Component and independent scopes

Twenty historical component records keep their original exact-source,
failure and continuation scopes. Three separate readiness records add author
44 normal roots, 14 race roots and vet, independent eleven-root normal/race/vet, and independent binding of
the author evidence. All 55 receipt-listed author artifacts and the original
56-entry manifest, including the receipt itself, pass readback. Seven unchanged
baseline failures and the separate final-validator-check omission remain causal
evidence. The author receipt SHA-256 is
`5cb41808b745166dd2a7895e1a2dc33a7eb4eb57fb5505c543591068885def33`;
the original manifest is
`d6a958be1a17ce642cfcb0b9b68b6f62c22d9a84ddfd1b52a63930f5840a770a`.
The independent component receipt is
`eb083cf71318778ac3fcb4973fa4217f9fe134856a03a8904e1eb916e23ce18e`;
its author-binding receipt is
`48ea00c329a43e03efbe288cec8093d0ef902add7df74c3bdc4ed56f1a8ee7ad`.

This candidate deliberately changes 23 Go files from `1d580d5e`; the previous
claim of exact Go equality to diagnostic source `4e6b4a7e` is retained as
historical evidence only. No full current application/database qualification is
inferred from the component or builder checks. Legacy root/trim exclusive
writers still require P0 custody qualification before fresh native authority
is wired; the public production composition keeps those authority ports absent.

Independent release evidence is retained under `/mnt/data/sn-testnet/sol-mainnet-readiness-release-independent-20261002`:

| Receipt | SHA-256 | Scope |
| --- | --- | --- |
| `receipt.json` | `02ec51631d777c476bc4604921a95be8297495ce7b811c64e6e59e5bb79b9b51` | Both source candidates and eight A OCI images |
| `oci-ab-addendum.json` | `1497adeab741eb9781eee099dfb278d7ffb2414821da0add1af9a5eae7358b1d` | Eight B images and exact A/B OCI/archive/executable identity |
| `planning-independent.json` | `f1c92fcadab0e968a0c12b0149e9913889ffb0a53b718aac9ca0c212dec225ec` | Bound snapshot/config/plan bytes and ten blocked actions |
| `final-seal-readback.json` | `7d946bf71734daec9c235272c2f47291c40a6cf7013abba3b6b35d621cdcf97c` | All 1,060 base and four guard files rehashed; exact receipt joins, retained failure and planning bindings |

The first two report artifact PASS and launch
`NO_GO_PENDING_PROVENANCE_AND_LIVE_AUTHORITY_GATES`. Planning readback reports PASS_SCOPED.
These are same-host independent artifact audits, not independent compiler/build
reproduction. Planning readback does not independently corroborate the live RPC.
The earlier passive-root-source exclusion no longer applies to this source.
The separate finalization guard records the later owner-custody source exclusion
and `NO_GO_SUPERSEDED_BY_PENDING_OWNER_CUSTODY_SUCCESSOR`; it does not alter the
earlier independent verdicts. Production custody, provenance and authority gates
remain open.

## Terminal seals

The author `evidence/release-receipt.json` SHA-256 is
`ddda91172eae67935099b37d627d5c956e64d248a17b85b9ddd8dc991f564235`. Its 1060-entry
`evidence/SHA256SUMS` is
`2dbde6501e4959e51816fa1c37746a5e352723401f4de1912ae633cb841502fb`; every entry passes complete readback.
Mutable caches/scratch, the documentation worktree and the separate finalization
and planning supplements are outside that author seal.

The required external
[`finalization-supplement/finalization-guard-receipt.json`](/mnt/data/sn-testnet/mainnet-readiness-release-astra-20261002/final-3d1e2ecf-serverac86855d/finalization-supplement/finalization-guard-receipt.json)
is SHA-256
`e3425b2bb19768b6a5988812129a55f64ba2691a82b76680cfb83bab1a3de9dc`.
Its four-entry `finalization-supplement/SHA256SUMS` is
`77abc2d37e424652a73873f197a47e93f6c572a220bfe7762ce6e22dc4892d9b`,
with all entries read back. It preserves the base receipt above and independent
primary `02ec5163…`/addendum `1497adea…` byte-for-byte, directly checks both A
manifest and audit joins, and binds the separate independent join receipt
`483007281c457c19502346231e2f990e90f5244592700cd6dd7eb5ded1d0032a`.
It also records the later owner-custody source as excluded and requiring a new
release; it grants no deployment or compiler-provenance approval.
The independent `final-seal-readback.json` above separately rehashes all 1,064
base/guard entries and confirms the direct joins, retained failure, unchanged
independent receipts and separately sealed planning evidence.

Only local source-to-bytecode/source-to-image verification and stated same-host
repeat equality are true. Independent compiler/build reproduction,
`reproducibility_verified`, `release_complete` and `deployment_approved` remain
false. Arm64, archive restore, SBOM/scanner/attestation policy, actual production
configuration/policy, migration/rollout qualification and published/running
image readback remain open, alongside live custody, funding and acceptance.

## Fresh unsigned planning supplement

After the author seal, exact A `sn-mainnet` (SHA-256
`a421d1eec6b77b0428483c5ec81154460304181e096c8a911f1bdcfed484192a`) ran read-only
`finalized-snapshot --rpc https://archive.chain.opentensor.ai --retry-window 300s`.
No expected-network flags were used as approval. The combined observation pins
native/EVM finalized block **9,191,688**:

| Field | Retained value |
| --- | --- |
| Native finalized hash | `0xb2ecafc53c28637c3eb7b8123b731b17f27538bf67038ff3ce962d442c0de84c` |
| EVM hash | `0x40c61cdddf72050c847bfdddf5819b37ed1da3db140fc71879add4244f3c39b1` |
| Runtime tuple | `node-subtensor`, spec 470, transaction 1, state 1 |
| Runtime code Blake2b-256 | `0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47` |
| Runtime metadata Blake2b-256 | `0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34` |
| Snapshot file SHA-256 | `sha256:64cb76825e0e687be496db12268d3aa74ad71429beb18948b1ffb208b0f89052` |
| Snapshot content seal | `sha256:46475400753d47a59a8c6d491db29cb5ebf537e63f35091270d04a52be043bbc` |
| Plan file SHA-256 | `sha256:4150e896a02454ef1ec0adde6ecf271c99fa9535f6a51ffb25cfef964a9adc1d` |
| Plan content seal | `sha256:cdeeba26ca87f03cc91252e2793e338d344d16709f8f9fe9874da2e2a5bb6aad` |

The snapshot remains `unapproved_observation` with `runtime_source_proven=false`
and the public route's asserted finality. The source lock, release inventory
and author receipt are exact pinned review inputs. Two plan invocations exit
zero and produce identical bytes:

| Action | Status | Executable |
| --- | --- | --- |
| `qualify-release` | `blocked` | `false` |
| `review-authority` | `blocked` | `false` |
| `reset-miner-uids` | `blocked` | `false` |
| `install-contracts` | `blocked` | `false` |
| `register-subnet-roles` | `blocked` | `false` |
| `start-root-validator` | `blocked` | `false` |
| `start-ur-validators` | `blocked` | `false` |
| `admit-operations` | `blocked` | `false` |
| `activate-native-miner-emissions` | `blocked` | `false` |
| `accept-and-reconcile` | `blocked` | `false` |

A separate [two-route public observation](/mnt/data/sn-testnet/mainnet-public-identity-20261002-root.json) at 00:50:33 UTC
pins block 9,191,637, native hash
`0x4a5322d6b05f324615ec3f41d7904ef6604e12b398a1dc933ebd6715348a3ebf`,
and matching genesis/EVM964/runtime470/code hashes across the Rao archive and
public entrypoint. Its file SHA-256 is
`97ab2e8eb6386002815821c2409e6d15e772c0c6a098287f1ac784922e494e38`.
It is supporting public-RPC evidence, separate from the artifact-bound snapshot
above; it grants no independent finality, source or execution approval.

All thirty requirements remain unresolved: twenty-eight are missing and only
binary artifacts/production qualification are supplied-unvalidated.
`owned-rpc` and `runtime-authority` remain missing; `apply_authority` and
`activation_ready` are false. The accepted exception for the exact v470 artifact
is for planning only and grants no live authority or deployment approval.

The separate `planning/planning-receipt.json` SHA-256 is
`e0a6a037da8f485efcd160902566e4374a93cb538a8d54196c20bbb03e8ce347`; its 19-entry
`planning/SHA256SUMS` is
`17146614460855a736379124a932bf7a67ec5c1a36bcb89df6bbe4e2ff13b8bf`. All entries pass readback. This supplement
does not mutate or expand the earlier author release seal.
