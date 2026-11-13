# Coordinator implementation CREATE source handoff

Source base: `a9b1a1c631daae507bda7423eff5c5f816787d22`. Candidate branch:
`codex/mainnet-coordinator-create-20260928`, isolated at
`/mnt/data/sn-testnet/worktrees/sn-mainnet-coordinator-create-20260928/sn`.
The containing source commit is the frozen qualification candidate; this record
is an implementation handoff, not behavioral qualification.

`--action coordinator-create` selects only approved action two after exact
successful reserve and vault completion. It requires the same deployer's next
zero-value CREATE, exact release creation bytes without constructor arguments,
and the predicted address in the UUPS `__self` immutable. The builder recreates
eighteen direct getter observations and two reviewed constructor storage words.
The implementation remains distinct from the future atomically initialized
proxy. Installation and activation remain false; six actions remain.

The storage schema comes from the imported OpenZeppelin sources under
`evm/lib/openzeppelin-contracts/contracts/proxy`: `utils/Initializable.sol`,
`utils/UUPSUpgradeable.sol` and `ERC1967/ERC1967Utils.sol`, together with
`evm/src/STCoordinator.sol`'s `_disableInitializers()` constructor. The namespaced
initializer slot is
`0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00`.
Its packed `uint64` initialized version is maximum and the following bool is
false: 24 zero bytes and eight `ff` bytes. Zero is rejected because it leaves
initialization enabled. The ERC1967 slot returned by `proxiableUUID()` is
`0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc`;
the implementation's own value in that slot is zero. The version getter is
`5.0.0`. Release-file and generated-binding checks still pin the exact creation
and runtime bytes; slot constants do not supply independent deployment authority.

Both `eth_getStorageAt` reads use the canonical inclusion block object already
authenticated by the receipt's native/Frontier/EVM mapping, with
`requireCanonical: true`. Code, getters, storage and original transaction share
that historical identity; the existing final mapping rechecks remain in place.
Archive errors do not become value mismatches. Successful implementation receipts
retain `storage_hash`, omitted from legacy receipts, preserving the original
reserve/vault wire fields, content hashes and markers.

`coordinator-create.json` has its own schema and marker bound to the exact
completed vault record. That record transitively binds the reserve; all three
locks and original journals are required. Each online operation reauthenticates
both ancestors at their original inclusions and carries both native checkpoints
through selected-action reconciliation and refreshed-head admission. Historical
ancestor files are never rewritten by coordinator progress. The owner counts all
three actions' attempts against the original approval and preserves later
same-sender value plus gas reservations. Signed bytes, ambiguous writes, failed
publication, initial-claim recovery, consumed nonces and retained receipts use
the existing durable owner mechanics. The maximum-eight attempt policy remains
unchanged and does not claim to implement all nine sends.

No live signing, RPC write or deployment was performed. This change does not
touch Safe-inner work, the rejected Safe fixture correction, the unintegrated
nine-action candidate, escrow registration, proxy initialization, contract links
or evidence installation.

Twenty deterministic top-level `TestEvmCoordinatorCreate*` roots have been added.
They run real release constructors in geth and the public command/owned local
HTTP path, including actual `StateDB` words read at separate historical states.
They cover exact creation/domain rejection, unsigned preview, all eighteen
getter faults and both storage faults, archive error versus disabled-state
mismatch, incomplete/reverted predecessors, both predecessor re-audits and
post-audit checkpoint changes, cumulative attempts and future value funding,
lost write reply, ambiguous attempt publication, both initial-claim boundaries,
lost child custody, changed transitive lineage, genuine revert, wrong-action
signature import, input aliasing, output loss, expiry, nonce movement and later
runtime change. The completion root also checks unchanged ancestor file bytes
and legacy outputs.

No behavioral test body was executed during implementation. Compile-only
`go test -c -p=2 -o ../mainnet-static.test ./mainnet` passed using candidate-only
sibling symlinks to the already qualified physical graph under
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`:
Connect `b163f9dd`, SDK `516521fb`, server `5dc11761`. Active sibling checkouts
were not changed. The aligned compile log is
`/mnt/data/sn-testnet/worktrees/sn-mainnet-coordinator-create-20260928/compile-aligned.log`.
The final compile-only recheck also passed; its adjacent log is
`compile-aligned-final.log`. The binary was not run. Gofmt and `git diff --check`
are source checks only.

Sol qualification should freeze the containing commit in its own physical
module graph, then run the new coordinator roots and adjacent reserve, vault and
preview roots normally and under race, plus relevant package static checks.
Useful isolated causal controls remove the exact creation equality, the storage
word loop, predecessor re-audits, either predecessor-continuity loop or the prior
attempt accumulation. For the checkpoint control, remove both continuity loops
over `plan.Prerequisites` in `evmOwnedChain.reconcile`; the dedicated checkpoint
root forces the ancestor change after both historical audits. For each control,
first establish a passing original root, then retain the mutant's failure and
exact source/provenance. Failures of a positive or its harness remain failures;
prior reserve/vault qualification, compilation and unexecuted test bodies do
not qualify the coordinator path.
