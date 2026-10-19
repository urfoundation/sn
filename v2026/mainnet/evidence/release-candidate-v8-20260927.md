# Local mainnet release candidate v8, 2026-09-27

This is an **offline, unapproved and incomplete** inventory. It supplies no
authority to deploy, reset UIDs, sign, spend, start either validator or claim
mainnet acceptance. The owned Snow VPN route still returns testnet EVM chain ID
945; mainnet ID 964 and an independently approved mainnet genesis are required.

The clean detached [source lock](source-lock-candidate-v8-20260927.json) pins
SN `1862d927ffadcd07e744d48607c4d44ae6579977`, server
`9f86073104c56e7e7cca802b97853db14fa8e044`, Connect
`c68689c420e45bcf07ecd4713e5de6e6bab5437f` and all local Go
replacements. Its content hash is
`0xa9ba0c6d0088435d80ea7efb38e981731f7095fb592159dbb33e70097d1cdbd3`;
exact JSON SHA-256 is
`a44d329cf418a91699069db2bc8fade85d399cbd45f61176ff9d57a524d3c195`.
The clean `sn-mainnet` binary SHA-256 is
`59e1a9de54ede993e22630783b11bef40b649b6810639533e65974ac29a9f7cb`.
A fresh source-lock read reproduced the JSON byte-for-byte.
All four Go executables were rebuilt from this same clean checkout with a
**fresh Go build cache**, the same Go 1.26.6 toolchain and `GOWORK=off`;
each rebuilt file compared byte-for-byte equal to its first build. The retained
[fresh-build result](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v8-20260927/fresh-build/RESULT.md)
records commands, tool hashes and verified checksums. This is same-source,
same-toolchain repeatability, not independent toolchain provenance or a proof
about deployed images.
These four outputs used the local default Go build profile. The existing
miner and validator Makefiles use `CGO_ENABLED=0`, `-trimpath`, stripped
linker flags and a stamped `main.Version` for release builds. The v8 binaries
therefore prove local buildability and deterministic bytes under the recorded
profile; they are **not** selected production image binaries. The next
composed candidate must record and qualify the actual release profile.
A separate [production-style build probe](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v8-20260927/production-profile/RESULT.md)
successfully built static Linux/amd64 `CGO_ENABLED=0`, `-trimpath`, stripped
binaries. The three service CLIs reported their explicit candidate version.
These outputs are retained separately from the v8 inventory because no
production version, architecture set or image has been approved.

The [actual-file inventory](release-inventory-candidate-v8-20260927.json)
records **18** selected files, **156,328,101** bytes, and content hash
`sha256:218e316a720ace88ac389c03267147900b3b1fa620baea515ef9fb7298aceb99`.
It includes four locally built SN executables (`mainnet`, miner, validator,
claim), four copied exact Foundry contract artifacts, Foundry settings, six
selected server migration/catalog files, Go module/checksum files and an
unapproved local Forge/toolchain observation. The inventory was rerun and
reproduced byte-for-byte. The exact input, both reads, binaries, copied
artifacts and a verified `SHA256SUMS` are retained under
`/mnt/data/sn-testnet/evidence/mainnet-source-lock-v8-20260927/`.

`policy` and `image-identity` categories are **missing**. The six present
categories mean only that the listed files were read and hashed: server
migration selection is not the deployed migration state, the toolchain
observation is not build provenance, and no role/image/config coverage is
inferred from one file. The inventory deliberately reports
`release_complete=false`, `provenance_proven=false` and
`deployment_approved=false`. Rebinding a copied artifact to the new source
lock does not prove its origin.

The EVM tree in this source is byte-identical to the earlier v7 tree
`b19dcdde1046bc4d8d22978d129c11338251e598`. Its pinned Foundry build
and 226-test suite passed; all four copied artifacts match the v7 bytes. The
[exact contract size evidence](contract-size-candidate-20260927.md) records the
12-byte `STCoordinator` margin. This is local build evidence only; deployment
against the selected mainnet runtime has not been tested.

The mainnet inventory implementation passed 177 normal top-level tests and
all 177 race test bodies across a bounded run and exact 11-test continuation,
plus vet and build. The initial whole-package race process hit its ten-minute
timeout after 166 passes, so it is **not** reported as a single passing run.
The later test-only validator fix reproduced an umask-sensitive seed-fixture
failure and passed 87 affected validator/CRv4 tests normally and under race,
plus vet; production seed permission checks were unchanged. Its full
[qualification record](/mnt/data/sn-testnet/evidence/mainnet-seed-fixtures-20260927/RESULT.md)
retains the causal proof. Neither the full validator corpus nor a composed
production release was qualified by these selected results.

The current route, policy, image identity, source-to-runtime and
source-to-bytecode provenance, full server/deployment closure, custody,
economic 10%/90% outcome, reset capability, both validators and production
rehearsal remain open under [the gate tracker](../PRELAUNCH-FIXES.md).
