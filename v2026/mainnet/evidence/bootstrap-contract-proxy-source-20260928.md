# Atomic coordinator proxy source handoff

Source base: `7c9b2f5ae6e06159e89adf9a0121e43e0066999a`. Candidate branch:
`codex/mainnet-proxy-create-20260928`, isolated at
`/home/by/urnetwork/temp/sn-mainnet-proxy-create-20260928`. The containing commit
is the frozen qualification candidate. No behavioral test body was executed by
the implementation owner. Escrow's source candidate is the parent; its focused
normal and causal qualification had reported no defect during this work.

`--action proxy-create` selects original approved index four, the same deployer's
next zero-value CREATE after escrow. The builder requires the exact reviewed
ERC1967Proxy creation prefix and canonical `(implementation, bytes initializer)`
constructor encoding. The implementation must be the completed index-two
address. The full initializer must equal the generated binding's
`STCoordinator.initialize` call, with graph-derived netuid, reserve, vault and
predicted proxy-mapped coldkey. Original owner, guardian, commitment oracle and
policy are decoded only from the already approved payload. Repacking both ABI
layers rejects changed selectors, widths, offsets, trailing bytes and graph
identities. It does not rewrite any approved action or signed plan field.

The reviewed sources are `evm/src/STCoordinator.sol`'s `initialize`,
`_validatePolicy`, `_validateSettlementWindow` and public getters; the generated
`stabi/stcoordinator.go`; and the reviewed proxy ABI/runtime in
`sim-testnet/contracts_gen.go`. OpenZeppelin sources were read from the local
dependency checkouts whose HEADs match `deploy/testnet/release.lock.yml`:
contracts `5fd1781b1454fd1ef8e722282f86f9293cacf256` and upgradeable
`7bf4727aacdbfaa0f36cbd664654d0c9e1dc52bf`. ERC1967Proxy rejects an empty
initializer and delegates it in the constructor; the implementation constructor
disables its own initializer. Proxy initialization sets its independent
Initializable version to one and Ownable owner to the configured address.

Policy validation mirrors source bounds with full-width arithmetic. In
particular, `(claimTtlEpochs + claimGraceEpochs) * epochBlocks` must cover the
immutable vault minimum claim TTL plus finalize offset plus one block. Both
deposit caps retain all uint256 bits. Input effective epoch/block words are
preserved as approved review values, because the actual initializer overwrites
them with epoch zero and `block.number`. Expected receipt getters therefore
derive the observed policy from the authenticated **EVM inclusion height**.
Neither the native checkpoint, latest head nor the ignored input block can
replace that height. The retained getter digest uses the same deterministic
projection, including offline reopen.

Successful completion requires the original transaction at its authenticated
position, explicit original receipt sender, null call target, exact created
address and bounded original gas/fee tuple. At one canonical EIP-1898 block the
adapter reads exact proxy runtime and 24 getters: the initialized network,
mapped coldkey, reserve/vault links, owner, configured/active guardian and oracle,
one policy, zero pending roles/epochs, unpaused state, zero campaign reservation,
operators/evidence/current epoch, interface version, initial policy by index and
epoch, and epoch-zero start/end blocks. `proxiableUUID()` is intentionally not
called through the proxy because its UUPS context guard reverts there.

Five exact proxy words are distinct postconditions:

| Namespace | Slot | Expected word |
| --- | --- | --- |
| ERC1967 implementation | `0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc` | Completed implementation address |
| ERC1967 admin | `0xb53127684a568b3173ae13b9f8a6016e243e63b6e8ee1178d6a717850b5d6103` | Zero |
| ERC1967 beacon | `0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50` | Zero |
| Initializable | `0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00` | Version one, not initializing |
| Ownable | `0x9016d09d72d40fdae2fd8ceac6b6234c7706214fd39c1cd1e609a0528c199300` | Approved owner address |

Address words have twelve zero prefix bytes. The initialization word is 31 zero
bytes and `01`; zero would leave initialization enabled, while uint64-max is the
implementation's disabled state rather than the proxy's initialized state.
Admin/beacon zero does not imply owner zero. The implementation's runtime and
its two constructor storage words are also checked at this proxy inclusion,
independently from their earlier historical CREATE receipt. Original native
ancestry, Frontier/EVM mapping, historical runtime, first insertion and final
mapping rechecks remain in force. RPC state/finality and source-to-runtime
approval remain external trust requirements, not independent proofs.

`proxy-create.json` uses a separate schema/marker sealing exact completed escrow
custody, transitively preserving all four ancestors. All five journals/locks
are required. Every online operation reaudits all four predecessors, with their
checkpoints carried through reconciliation and refreshed admission. Historical
journal bytes and existing receipt fields/hashes are preserved. No new receipt
field is introduced: the proxy getter digest includes the observed initial
policy, and the storage digest includes all five expected proxy words.
The owner retains cumulative original attempts, later same-sender value/gas
funding, original signature identity, ambiguous-send liability, poisoned
publication, exact initial-claim recovery, consumed nonce and output-loss
recovery. The existing maximum-eight attempt policy remains unchanged.

Completion is `proxy-created-initialized`; four actions remain and installation
and activation stay false. Reserve recorder binding, vault coordinator binding,
validator evidence deployment and Safe-inner anchoring remain outside scope.
The configured owner address is checked as approved data; no live Safe owners,
threshold, signatures or authority are inferred. No live signing or RPC write
was performed, and no live deployment, native burn/refund/ED behavior, source
provenance or finality was qualified by this change.

Twenty-five deterministic top-level `TestEvmProxyCreate*` roots were authored.
The fixture executes real release constructors and real delegatecall
initialization after the earlier escrow fixture's explicit native-precompile
model. Historical `eth_call` now supplies the captured EVM header's block number
as well as state, allowing an advancing live clock to differ from initialization.
That fixture adjustment leaves prior contract behavior unchanged and adds no
process-global policy. The full-width cap test executes uint256-max policy caps
with an exactly sufficient immutable claim horizon, then rejects a one-block
deficit before custody.

Coverage includes exact constructor/initializer and governance identities,
source policy bounds, preview, all 24 getters/five proxy slots, implementation
runtime/storage at proxy inclusion, archive unavailability, receipt identity,
distinct EVM/native/latest/input heights, completed/reverted escrow admission,
four historical re-audits and continuity barriers, original graph attempts and
future funding, lost reply, ambiguous publication, both initial-claim boundaries,
five locks, missing child and changed ancestry custody, genuine constructor gas
failure, wrong signature/aliases, output failure, expiry/runtime/nonce/collision
admission, later historical runtime change and retained policy digest validation.
The completion root checks unchanged ancestor bytes and legacy output fields.

Compile-only `go test -c -p=2` passed twice using an external absolute-path
`-modfile` with the same eight dependency pins listed in the
[escrow source handoff](bootstrap-contract-escrow-source-20260928.md): Connect
`b163f9dd`, SDK `516521fb`, server `5dc11761`, and that handoff's pinned proxy,
UserWireguard, Warp, Glog and Goidenticons. Physical dependencies are under
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`.
Active siblings and committed module files were not changed. Logs and the
unexecuted binary are under
`/mnt/data/sn-testnet/worktrees/sn-mainnet-proxy-create-20260928/` as
`compile-aligned-initial.log`, `compile-aligned-final.log`, `mainnet-static.test`.
Gofmt and `git diff --check` are source checks only. Compilation and unexecuted
tests do not qualify behavior.

Sol should fence the containing exact source commit, run
`^TestEvmProxyCreate` normally and under race, then adjacent
`^TestEvm(Create|VaultCreate|CoordinatorCreate|EscrowRegister|PhasePreview)` roots
and full mainnet/static gates as appropriate. Suggested isolated causal controls
after establishing a passing original root:

| Control | Root expected to reject its mutant |
| --- | --- |
| Remove exact initializer/constructor re-encoding equality | `TestEvmProxyCreateRejectsChangedConstructorAndInitializer` |
| Remove the immutable claim-window comparison | `TestEvmProxyCreateRejectsInvalidInitialPolicy` |
| Skip proxy-only getter observations | `TestEvmProxyCreateRequiresEveryGetterAndStorageWord` |
| Skip proxy-only storage observations | `TestEvmProxyCreateRequiresEveryGetterAndStorageWord` |
| Derive policy effective block from native height or skip normalization | `TestEvmProxyCreateBindsInitialPolicyToEvmInclusion` |
| Skip same-inclusion implementation rechecks | `TestEvmProxyCreateRechecksImplementationAtProxyInclusion` |
| Skip ancestor online receipt audits | `TestEvmProxyCreateReauditsFourPredecessors` |
| Remove both predecessor continuity loops in selected reconciliation | `TestEvmProxyCreateFencesFourPredecessorCheckpoints` |
| Drop predecessor attempts from send allowance | `TestEvmProxyCreateKeepsCumulativeGraphAttempts` |
| Ignore future same-sender value/gas liability | `TestEvmProxyCreatePreservesLaterValueAndGasFunding` |

Record exact source/provenance and assertion output for each control. A failing
positive or fixture remains a qualification failure, not a waiver; earlier
CREATE and escrow results do not qualify initialized proxy behavior.
