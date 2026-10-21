# MG-06 observer and signed scheduling: qualified scoped release

Date: 2026-10-02. The selected local release now includes both qualified
[native observer and signed schedule corrections](native-economics-context-and-schedule-20261002.md)
alongside the prior owner, installation, recycle and server schema-752
composition. Its frozen source is SN `258e25b4dd2c8b210bd8932b415e7d39776e23a0`,
tree `253bc2b79016c06d4d7d5f06ce828e22660b8bb6`, paired with server
`0aa1e2449b92fe62b10c6a287e5492dd5a19b792`, tree
`c2f5649fda780e8371e0f314a2da1356549a8ae7`. This resolves the source exclusion in
the [132 baseline](release-1320845d-server0aa1-20261002.md) for this new candidate.
Every predecessor artifact and receipt remains unchanged. **MG-02 stays open;
launch remains NO-GO.**

Later server `025802a50e2dc56dc56c3cb749db7375aa9f72be`, tree
`adf05777aca3ad4fb97c4befd2163a74064d635b`, is outside these frozen artifacts.
Its retention-debt guard and prober cleanup retry cap await separate static
review. A clean remote fetch records the eleven-file delta (665 added and
36 removed lines) in a separate [source-intake receipt](/mnt/data/sn-testnet/mainnet-mg06-release-astra-20261002/final-258e25b4-server0aa1e244/successor-intake/receipt.json),
SHA256 `c93ddacc23da7dd7f7e8e706ad6caa671b095b44b3c0e89a1f757084b59e8eeb`; the six-file checksum manifest is
`638e3680030ed495823ac0567efa152c0420717d900557d674870e276976e3fa`. This intake inherits no qualification and has not been
determined to require a P0 successor. Any later source selection needs its own
qualification and exact release composition; the author base seal stays unchanged.

The release root is
`/mnt/data/sn-testnet/mainnet-mg06-release-astra-20261002/final-258e25b4-server0aa1e244`.
All ten checkouts are clean physical Git repositories without object alternates.
Nine were fetched from their remotes. SN258 was initially unpublished and was
fetched from the independent verifier's exact physical data-volume clone.
A later exact remote fetch verifies that integrated main `6cfc4773`, tree
`89ba29e90b731e5e344fbef539d3be9497fa0ba4`, contains it and changes no Go,
Solidity, module or Foundry configuration inputs. The selected checkout never
retargeted. Initial preparation's provisional status remains an honest historical
record; component qualification and publication are terminal before this seal.
All new checkouts, caches, temporary files, OCI stores and reporting are on
`/mnt/data`. No release source read error was observed.

## Exact repeated artifacts

Two sequential source builds used distinct initially empty compiler caches and
took 574 and 555 seconds. Each retains 175 artifacts,
including 17 executables, ten contract creation/runtime outputs and twelve
builder-selected migration files. Independent readback confirms all 81 non-log
artifacts match. Source epoch is `1790911539`; binaries retain clean exact VCS
revisions, Linux/amd64 and disabled CGO.

| Repeat | Source manifest SHA-256 | Complete OCI aggregate SHA-256 |
| --- | --- | --- |
| A | `49eef0f94b9066c2b0d6bd730933c4e69a018aa45a388ce7db160f228d8dd9d0` | `c684ac3e51981b3ee5ca785e783ac2ceab035163fb5c29efeead433465dd78f0` |
| B | `b2251e3cd9271c3529ebaa88ee0c9f508d431e7dde9a28ce3465a24da5cb84ec` | `1a81a942766721e1abb210104bd1bf16d385799c4e44d8169bec570a13e64bd6` |

Both image repeats construct seven package images and one scratch image using
separate initially empty owned containerd/Docker stores. Each aggregate verifies
342 source/image inputs. All eight OCI archive bytes, platform/configuration
identities, layers, diff-IDs and final executable bytes pass independent readback
and match A/B. All four owned build daemons stop and join; resident daemons and
containers are untouched. Logs and timestamp-bearing manifests are not asserted
byte-identical. No image is published or deployed.

Go 1.26.6, Forge, Solc 0.8.24, Git and buildx remain exactly hash-pinned, as do
forty package payloads and the base OCI. Reused module inputs are authenticated
against the sealed predecessor: 274 source bodies match `h1`, 298 metadata files
match their hashes, and three selected archives match. Effective module graphs
agree after only the exact SN revision and physical path changes. SN retains
363 incompletely qualified nodes of 636; server retains 372 of 647. This is
authenticated input reuse and local repeat equality, not independent compiler
or dependency provenance.

## Current qualification and original scopes

Fresh tests on exact SN258/server0aa pass 83 normal and 83 race roots: 23 CRv4,
41 mainnet and 19 validator. Vet passes all three packages; no root or subtest
is skipped. The actual frozen declarations define the selected root census.
There is no source overlay. The separate full observed v470 metadata probe from
the observer author's 29-root normal run is preserved under its original scope,
not counted among these 83 committed roots.

The independent verifier separately executes nine current-pair seam roots
normally and under race, plus vet. They cross the observer's two causal cases,
tempo/commit phase/round scheduling, monitor command/checkpoint behavior and
fresh versus historical signed production intent. The independent 9-root scope
overlaps the author's 83 roots; they are not added into an invented census.
Fresh builder qualification also passes all 95 normal and 95 race roots, plus
vet, using a short private TMPDIR and `umask 077` from the start.

Thirty-six component receipts retain their original scopes: 32 predecessor
records and four observer/scheduler author/independent records. All eight
observer and thirteen scheduling Go source/test files match their frozen source
commits. Original two observer and five schedule baseline causal failures,
four schedule omission failures, the author validator census correction and
earlier excluded development controls are retained and hash-bound. Those
original MG-06 author/independent scopes used server `ac86855d`; they are not
silently relabeled as current-pair execution.

Nine dependency source pins and both projects' module declarations are unchanged
from the 132 baseline. Its original server 46 normal/race roots and four-package
vet, sampler 13 normal/race roots, owner/installation/recycle 42 normal/race
roots, and independent role 22 normal/10 race/two passive-host roots remain
authenticated original evidence. They were not rerun as a complete SN258 suite.
The prior private 751→752 database migration, PostgreSQL/Redis image IDs,
pre-test setup refusals and owned fixture cleanup remain under that original
receipt. No private database fixtures are started by this successor release.
Broader migration inventory still retains 26 source files and 752 AST catalogue
entries; this is distinct from the builder's narrower twelve-file selection.
No production database migration or restore is claimed.

## Seals and independent readback

The [author receipt](/mnt/data/sn-testnet/mainnet-mg06-release-astra-20261002/final-258e25b4-server0aa1e244/evidence/release-receipt.json) is SHA256
`57b87ca18a03fd62171641ab64a24d72c885a47af861dd7831365cbf126287bd`. The [base checksum manifest](/mnt/data/sn-testnet/mainnet-mg06-release-astra-20261002/final-258e25b4-server0aa1e244/evidence/SHA256SUMS)
contains 1359 files, SHA256 `3f6b13c2e0146b8654a90b1a4bd10f5e40eca2e511daeff464fcc2c97b81d6d0`; author full readback
passes. The repeated 62-file release inventory totals 807590251 bytes
and still lacks `policy` and `image-identity` categories.

| Separate evidence | SHA-256 |
| --- | --- |
| [Independent source A/B](/mnt/data/sn-testnet/sol-mainnet-mg06-release-independent-20261002/source-ab-receipt.json) | `9879de09a776cc274e9f197f54165d04629fa5a91dfc910de61e5112443656f6` |
| [Independent primary](/mnt/data/sn-testnet/sol-mainnet-mg06-release-independent-20261002/receipt.json) | `273e865992334f3277fb5975c5b4ae9073022fcb24f1f01ef93e59f8daf47450` |
| [Independent OCI A/B](/mnt/data/sn-testnet/sol-mainnet-mg06-release-independent-20261002/oci-ab-addendum.json) | `186a93e4900f608e65854845177a18402fe0b5a7e059a22bde5c6cbc4e350e68` |
| [Independent current-pair nine-root scope](/mnt/data/sn-testnet/sol-mainnet-mg06-release-independent-20261002/current-pair/receipt.json) | `594907b53502594612dc0856a86e6e5aa2e000a4643a5b38b0a5860cd3600368` |
| [Author current-pair 83-root scope](/mnt/data/sn-testnet/mainnet-mg06-release-astra-20261002/final-258e25b4-server0aa1e244/mg06-qualification/receipt.json) | `10563d27e1dfffca1ef067163a43e0d33d0fd324e12adde4cd4e068ad2b9a9ab` |
| [Independent readback of author 83-root logs](/mnt/data/sn-testnet/sol-mainnet-mg06-release-independent-20261002/author-mg06-readback.json) | `cf7b6b8e76ba66e0074e87445b82d46ca3b9adb59aa3d2986fbb8049a4526833` |
| [Strict independent binding](/mnt/data/sn-testnet/mainnet-mg06-release-astra-20261002/final-258e25b4-server0aa1e244/evidence/independent-artifact-binding.json) | `686774cc0dfc84fad74d1eca6fa57fca1be7d692526f634b1fd7aa55a2a3d4e2` |

The strict binding checks both receipt file hashes, directly joins primary and
addendum A manifest/audit hashes, checks each candidate and aggregate against
its actual retained file, and checks image arrays and the current-pair receipt.
These joins precede the author seal. Independent artifact readback is not a
separate compiler/source build or deployment approval.

The [separate independent final-seal readback](/mnt/data/sn-testnet/sol-mainnet-mg06-release-independent-20261002/final-seal-readback.json), SHA256 `c023d17904414f10cf4aaf98cf10927ef162475c77e7f74cb7ce0656664c7d6d`, rehashes the author base manifest and joins its final receipt, independent artifact binding and blocked planning. This is a readback of retained bytes; independent compiler reproduction and production authority remain unproven.

## Fresh unsigned plan

The exact candidate-A `sn-mainnet` binary, SHA256 `e5eaffcb860c177ff205a3fe7cfaab0d9a3586312add729b9ded9fac9d5a57c2`, captures a new
combined public finalized native/EVM snapshot at native block 9192802,
hash `0x4ea36b2a87ca41daad6f4f3f06753444ef54662bebd13a57462374d5879e2329`, and EVM hash `0xee08392773bcf074dcbaf3bc540698b66ef4a27d982760c813dc1bdaddbb2884`. It observes runtime spec470,
EVM ID964 and the expected genesis; code hash remains
`0x5675b684d69a07f6f224c2ba9cabef719804911fba40fbe1a2295198c9cb7c47`.
This public RPC observation does not establish independent GRANDPA, source/Wasm,
checkpoint or deployment authority.

The [planning receipt](/mnt/data/sn-testnet/mainnet-mg06-release-astra-20261002/final-258e25b4-server0aa1e244/planning/planning-receipt.json) is SHA256
`f84bada3be92b9e282d940608f5f3c5405b1b014004592779c3df3f536f4883c`; its nineteen-file checksum manifest is
`a53148898ba4bca707efb0e384ad10b8b085d994bb5b24370c636b5e60d8b15d`. Both generated plans are byte-identical, with content
seal `sha256:6720779f4449eccfd741226c845b15093586250f1525ba4fa87349824ed6b9d0`. All ten actions remain blocked and non-executable;
28 requirements are missing and two supplied release inputs remain semantically
unvalidated. The [independent planning readback](/mnt/data/sn-testnet/sol-mainnet-mg06-release-independent-20261002/planning-independent.json)
is SHA256 `8b577845047aacc140a0f3f3302f67ca5e783605fd2f2dd77bc5c477e85b8c45`. The exact v470 artifact exception remains
planning-only and changes none of these statuses.

## Remaining release and capacity gates

This successor packages the two MG-06 corrections; it does not establish the
native miner denominator, quantization tolerance, recipient generation,
provider entitlement, actual owner recycling or the 10%/90% outcome. The schedule
still assumes successful epochs and the reviewed inclusion model; actual
inclusion, reveal/application and economic observations remain required.

Independent compiler/module/package provenance, arm64, accepted production
configuration and policy, full service behavior, rollout/migration/restore,
attestation/SBOM/scanner policy, published/running image identity and release
approval remain open. Owned mainnet RPC and independent runtime/finality
authority, owner/device and residual-risk acceptance, Safe/evidence custody,
actual installation and role registration, eligible independent validators,
passive root-host approval and economic activation remain separate gates.

The image queue waits for both source repeats and requires 118 GiB available
before each serial repeat to preserve the 110 GiB floor with margin. Root's
separate cleanup removes only verified inactive caches and preserves receipts;
its exact timestamped log is retained in support evidence. Earlier root-disk
media errors and an unreadable old source tree are not erased by successful
release readback. Sustained production capacity, media integrity, full-volume
behavior, backup and restoration remain MG-09 work. No signing, broadcast,
publication, deployment or production application start occurs.
