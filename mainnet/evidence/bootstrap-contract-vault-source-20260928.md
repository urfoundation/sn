# Settlement vault CREATE implementation handoff

Source base: `fac54271`. Candidate branch:
`codex/mainnet-vault-create-20260928`, isolated at
`/mnt/data/sn-testnet/worktrees/sn-mainnet-vault-create-20260928/sn`.

The new `--action vault-create` path executes only approved action one after an
exact successful retained reserve outcome. It rebuilds the release constructor,
requires the same deployer at the next nonce, derives its mapped coldkey, patches
all six immutable words and authenticates thirteen getters at canonical inclusion.
Constructor values are decoded from exact approved calldata and exposed in
preview/plan; the phase's signed schema and hash have no new fields.

The vault has a separate schema/marker/journal, bound to the exact completed
reserve record. Its owner holds both locks, re-audits reserve historical
postconditions before online vault progress, carries that retained native
checkpoint through subsequent admission/fresh-head continuity, retains original signed bytes and
consumes attempts before writes. It counts reserve and vault attempts together
against the same finite approved allowance, and preserves future same-sender
funding reservations. Old reserve journal JSON, content hashes, markers and
default result fields remain unchanged. Installation and activation stay false.

The candidate does not implement coordinator, registration, proxy, links,
evidence deployment or Safe anchoring. It does not touch the rejected Safe-inner
fixture correction or use the frozen unintegrated nine-action candidate. There
was no live signing, approval, RPC write or deployment.

Behavioral execution belongs to the Sol qualification handoff. No new test body
was executed during implementation. The new deterministic `TestEvmVaultCreate*`
roots exercise real release constructors in geth and the public CLI/owned local
HTTP path, including distinct historical states for both contracts. They cover
constructor/domain faults, each getter/runtime/address fault, exact predecessor
gating and re-audit, original graph attempts and funding, uncertain replies,
publication poisoning, claim recovery, lost child custody, genuine reverted
creates, wrong-action signature import, changed graph approval, output loss,
nonce movement, offline completion and old reserve wire compatibility.

The first compile-only `go test -c -p=2 ... ./mainnet` attempt used ordinary
workspace sibling links and failed on unrelated SDK API mismatch: missing
`RegisterNetworkClientArgs`, `NetworkClientRegistrationEndpoint`,
`ClientRefreshIntegrityNotice`, and adjacent APIs. That is an environment failure,
not a behavioral result. Sibling checkouts were not changed. Candidate-only links
were then pointed at the already qualified physical module graph under
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`:
Connect `b163f9dd`, SDK `516521fb`, server `5dc11761`. The aligned compile log is
`/mnt/data/sn-testnet/worktrees/sn-mainnet-vault-create-20260928/compile-aligned.log`;
that aligned compile and the final compile-only recheck completed with exit 0.
The recheck log is the adjacent `compile-aligned-final.log`. The binary was not run. Final
behavioral qualification still requires the separately frozen Sol module graph.

Qualification should run `TestEvmVaultCreate*` plus the adjacent
`TestEvmCreate*` and `TestEvmPhasePreview*` roots normally and under race on the
frozen candidate, then the package's static checks with exact physical module
provenance. Useful isolated causal controls remove the vault constructor equality,
skip its getter loop, bypass the prerequisite re-audit, or stop adding prior
attempts: each associated positive root must pass before a mutant failure can
count as discriminating. Retain failed positives and harness failures as failures;
do not report pending tests, compilation, or reserve-only prior qualification as
vault qualification. The additional deterministic checkpoint control should
remove the vault's two prerequisite-continuity checks in `evmOwnedChain.reconcile`;
`TestEvmVaultCreateKeepsReserveCheckpointDuringFreshAdmission` must distinguish
the passing original from the resulting admission across a changed reserve hash.
