# Local mainnet release candidate v9, 2026-09-27

This is an **offline, unapproved, incomplete** source and file inventory. It
does not authorize deployment, UID reset, signing, spending or acceptance. Snow
VPN `172.28.208.185:9944` still returned testnet EVM chain ID 945 when checked
for this candidate; mainnet ID 964 and an independently approved genesis remain
required.

The clean detached [source lock](source-lock-candidate-v9-20260927.json) pins
SN `265231f9`, server `969d6c74`, Connect `c68689c4` and every local Go
replacement. Its content hash is
`0x201aad1f419c2bc8ece1c22784270dde72279e93cbcbdc3ddcdc6ba89ce208b9`.
Two source-lock reads compared byte-for-byte. Six static Linux/amd64 binaries
were built from this composition with recorded production-style profiles: SN
mainnet, miner, validator and claim; server API and taskworker. The SN service
CLIs report candidate version `0.0.0-mainnet-candidate.265231f9`. These are
local binaries, not selected OCI images or deployed processes.

The [actual-file inventory](release-inventory-candidate-v9-20260927.json)
hashes **32** files and **299,265,038** bytes, including all six binaries,
four copied contract artifacts, eight pinned/scratch server Dockerfiles,
selected build/module files and six server migration/catalog sources. It
replayed byte-for-byte. Its content hash is
`sha256:a761ee1b2f2961940c9a1503ab37c2582613c9aba08401750c16ff4d02e70916`.
The complete input, exact binaries and replay outputs are retained in the
[external build and inventory record](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v9-20260927/RESULT.md).
Both server images also built locally from the pinned Dockerfiles; extracted
executables matched the inventoried binaries byte-for-byte. The
[image probe](/mnt/data/sn-testnet/evidence/mainnet-images-v9-20260927/RESULT.md)
retains their local digests, inspection and exact build logs. Its `apt-get`
step reads moving Ubuntu repositories, so this probe does not close image
reproducibility, registry publication or deployed identity.

The new [owner-recycle measurement](../../validator/OWNER-RECYCLE-MEASUREMENT.md)
replays original provider proofs against the signed approval and exact native
owner census. Its only resulting intent is blocked: it does not enable native
submission or prove 10%/90% realized emissions. The affected selector passed
139/139 race tests and 58/58 final-source focused normal tests, plus vet and
Darwin/arm64 build. The [qualification evidence](/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-decision-20260927/RESULT.md)
retains exact scope and limitations.

The EVM tree is unchanged from v8; copied contract artifacts are byte-identical
to the earlier pinned Foundry build. Their new source-lock binding is not a new
source-to-bytecode proof. `policy` and `image-identity` inventory categories
remain missing. The inventory deliberately reports `release_complete=false`,
`provenance_proven=false`, and `deployment_approved=false`. Live mainnet
identity, validator eligibility, full history, custody, activation, native
economic outcome, contract creation, images and deployment qualification are
still open under the [gate tracker](../PRELAUNCH-FIXES.md).
