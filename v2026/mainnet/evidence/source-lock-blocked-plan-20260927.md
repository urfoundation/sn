# Mainnet blocked-plan source candidate, 2026-09-27

Status: offline source candidate and signer-free negative control. This is not
an approved mainnet identity, source-to-Wasm attestation, complete production
release, executable bootstrap plan or authority to sign or deploy.

The clean detached [source lock](source-lock-blocked-plan-20260927.json) pins SN
`7175313de1ac25b6daaa84529b65ccf087bbe62e`, server
`9f86073104c56e7e7cca802b97853db14fa8e044`, Connect
`c68689c420e45bcf07ecd4713e5de6e6bab5437f` and every local Go
replacement. Its content hash is
`0x4cf42801ff944e9ee1805a0f9a3210f41723b2bed14a468379c82af1dfdb20b7`;
exact JSON SHA256 is
`0afc5d00b03b936a4de8aa43dc2dfbe4dad2511f62adde7df32c204f7c743634`.
The clean detached `sn-mainnet` binary SHA256 is
`739f4b174217d555eeedfb0f741c83201604a77fb1f611e3c0937657461b558e`.
Full build and negative-control files remain under
`/mnt/data/sn-testnet/evidence/mainnet-source-lock-v7-20260927/`.

The isolated code qualification reports 169/169 mainnet normal tests, 30/30
focused plan/snapshot/mapping/source-lock race tests, vet, build and diff-check
passing. Its sealed logs and exact command lines are in
`/mnt/data/sn-testnet/evidence/mainnet-plan-20260927/RESULT.md`. A separate
exploratory whole-validator package run hit Go's ten-minute package timeout
with 1,810 top-level tests and a large parallel corpus; that run is not a pass
and is not used to claim qualification. Earlier selected validator and receipt
suites remain separately evidenced in the preceding source composition.

A clean v7 contract-only Foundry build using Forge 1.7.1, solc 0.8.24 and
three pinned Forge/OpenZeppelin library commits passed. The exact local
toolchain and library observation is retained as
`forge-toolchain-observation.json` in the external evidence directory; this is
not source-to-bytecode proof. The four deployable artifacts were copied before
running the complete `forge test --root evm -q` suite, which passed all **226**
listed tests and regenerated those four artifact files byte-for-byte. Runtime
sizes are `STCoordinator` **24,564** bytes (12-byte margin under 24,576),
`STReserveSink` 1,558, `STSettlementVault` 9,486 and
`STValidatorEvidence` 12,192. The artifact JSON SHA256 values are, respectively,
`5a51b1f4a426cfe36e760eb5312947abc80fa9a7e4e4a517210d3da0e5278ba3`,
`8101eae965e845103079485f9a633dbe665e1932827b564a781efab5425e822f`,
`e57e61d44b1729b823f271add7f81cbd3616d404b8e46eb67c8ec71fcf86e4ec`
and `b744767e64f0bb2afd76344c0172db51d10affb1868f17a2c6133dbde72688d7`.
The external `SHA256SUMS` verifies the build/test logs, toolchain observation
and artifact files. A local build does not prove the selected mainnet EVM's
code-size rule or successful deployment/readback.

The [unbound outline](blocked-plan-outline-20260927.json) has status
`unbound_outline`, ten blocked non-executable actions and 24 missing
requirements; `apply_authority=false` and `activation_ready=false`. It carries
no network or block assertion. Exact outline JSON SHA256 is
`6770c6db8b254cbeea29fa4e0a819f41adacaff82192a7150fd8ca38707b804e`.

For a negative control, `plan --config` consumed the retained [same-block Snow
observation](finalized-snapshot-snow-20260927.json), this source lock and an
offline release-input file. The config deliberately required EVM ID **964** and
synthetic genesis `0x22…22`. It exited **3**, wrote **zero stdout bytes**, and
reported that the retained observation is actually testnet EVM ID **945** with
genesis
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`.
The synthetic hash is only a rejection control and is not a proposed mainnet
genesis. An independent read-only Snow inspection at 15:21 UTC also returned
EVM ID 945, spec 471 and finalized block 8,098,349.

The bounded planner verifies exact local file hashes, internal content seals,
runtime code/metadata bytes, the native header hash, Frontier digest and raw
EVM header RLP before emitting a review. It does not prove RPC finality,
source-to-Wasm provenance, authority, custody, live capability or any attached
manifest's semantics. Every action stays non-executable even if all review
files are supplied. Production activation still requires the independently
approved mainnet route/genesis, semantic capability and custody validation,
the complete release and executable action/receipt owner described in
[the gate tracker](../PRELAUNCH-FIXES.md).
