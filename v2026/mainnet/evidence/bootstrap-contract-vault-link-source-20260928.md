# Provisional vault coordinator binding source handoff

Source base: `e27addf6d6057751eec01387676b5947f141d9c2`, inheriting reserve binding
`14afe75d951b1ecc464f84d6ebc2ecb3fb86cfc6`. Candidate branch:
`codex/mainnet-vault-link-20260928`, isolated at
`/home/by/urnetwork/temp/sn-mainnet-vault-link-20260928`. The containing commit
is a **provisional source freeze**, with no integration or qualification claim.
At freeze, Sol reported the correction's two targeted normal roots passing and
both causal controls discriminating; broader correction/reserve-binding gates
remained active. This candidate can be queued for independent tests but must
inherit a successor if predecessor qualification identifies another defect.
The implementation owner executed no behavioral test body. Proxy/reserve-binding
qualification is separately owned by Sol. The base includes the narrow shared getter-gas fixture correction
documented in `bootstrap-contract-read-gas-fixture-correction-20260928.md`;
the original positive failure and exact reproduction remain preserved. That
correction changes no production code or approved transaction execution budget.

`--action vault-link` selects the original approved index six. The nine-action
graph and reviewed `evm/script/Deploy.s.sol` place
`vault.setCoordinatorOnce(address(proxy))` immediately after reserve recorder
binding. The target is the derived vault, the argument is the already initialized
proxy, the value is exactly zero, and the same bootstrap deployer consumes its
next nonce. The generated `stabi/stsettlementvault.go` ABI and reviewed artifact
agree on selector `0xf406b76b`, the exact 36-byte canonical call, and the indexed
`CoordinatorFixed(address)` event. No alternative target, implementation address,
selector, high address bits, suffix, sender, nonce or value is admitted. The
signed graph, original input bytes, approval key and approval message are unchanged.

Reviewed `evm/src/STSettlementVault.sol` permits only the immutable bootstrap,
requires coordinator zero and nonempty new-coordinator code, writes coordinator
once, and emits one event with no external calls. Its storage layout requires
special care: coordinator occupies the low twenty bytes of slot zero while
`escrowRegistered` is a boolean at byte offset twenty in that same slot. The
registration flag is already one after the completed escrow prerequisite. Exact
precondition and postcondition words preserve this flag independently from the
coordinator getter.

| Slot | Reviewed field | Binding inclusion expectation |
| --- | --- | --- |
| 0 | Coordinator plus escrowRegistered | Eleven zero bytes, `01`, twenty proxy-address bytes |
| 1 | `_entered` reentrancy guard | Zero |
| 8 | `totalCaptured` | Zero |
| 9 | `totalPaid` | Zero |
| 10 | `pendingFunding` | Zero |
| 11 | `outstandingLiability` | Zero |
| 12 | `escrowAccounted` | Zero |

Constructor immutables occupy no storage slots. Mapping roots at slots two
through seven are not presented as enumerable live mapping evidence. The setter
does not write them, registration or accounting. The candidate checks the seven
explicit words above and all thirteen vault getters: six immutable identities
and bounds, coordinator, registration, and five accounting counters.

Current canonical and pending admission are separate from historical inclusion
checks. Both must show exact vault runtime, registered-but-unbound getters and
the exact seven precondition words. An observed nonzero coordinator prevents
another send, even if it is the intended proxy. Malformed coordinator output
remains unresolved; unavailable getter/storage reads remain read errors and do
not publish success, a collision or an attempt. Current observed liability
cannot silently replace or release the original signed transaction.

Successful inclusion additionally requires original receipt sender and target,
explicit null CREATE address, exact original transaction position and approved
gas/fee bounds. Exactly one log must identify `CoordinatorFixed` from the vault,
with one canonically padded proxy-address topic, no data, exact receipt
transaction/block/position fields, explicit log index and `removed: false`.
Reverted receipts must have no logs. Exact patched vault runtime, all thirteen
postcondition getters and the seven words are read at the same authenticated
inclusion. Getter/storage hashes plus `vault_binding_hash` retain that bounded
outcome. The binding digest includes derived vault/proxy addresses and event
log position. These remain owned-RPC observations, not an independent
receipt-trie, storage or consensus proof.

All six predecessors are reauthenticated at their original inclusions before
online progress. Current canonical/pending and vault inclusion then independently
recheck the already-bound reserve's runtime, six getters and two storage words,
followed by initialized proxy runtime, 23 stable getters, all five distinct
implementation/admin/beacon/Initializable/Ownable words, and implementation
runtime/two constructor words at that same block selector. The original proxy
EVM inclusion height remains the source of its normalized policy and epoch-zero
boundaries; the later vault block, native height, latest clock and ignored input
height cannot replace it. The later stable projection excludes only the
time-varying `currentEpoch()` getter. Existing historical proxy completion still
requires epoch zero at initialization. No live Safe authority is inferred from
the configured owner.

`vault-link.json` uses `urnetwork-mainnet-evm-vault-link-state-v1` and seals exact
completed reserve-binding custody. All seven journals and locks remain held for
an operation. Six predecessor hashes and checkpoints fence initial and refreshed
admission; every historical journal and marker keeps its bytes. The new optional
`vault_binding_hash` and `coordinator_log_index` receipt fields are appended and
omitted from old outcomes. Index zero is valid and remains bound by the digest.
Non-vault-binding and reverted receipts reject these additive fields. Original
schemas, signature custody, graph approval hash and marker rules remain intact.

Attempt allowance still counts the whole executed prefix, including ambiguous
publications, against the original maximum. All later same-sender value plus
maximum gas liability remains reserved. The existing maximum-eight bound is
unchanged; a complete nine-action execution policy remains outside scope.
Lost replies retain original bytes and nonce. Failed durable publication poisons
the owner; reopening reconciles actual retained state. Initial-claim recovery,
missing-child refusal, changed-ancestor refusal, consumed-nonce preservation and
output-loss recovery apply without new signing or replacement authority.

Completion is `vault-coordinator-bound`; canonical failure is
`vault-binding-reverted-nonce-consumed`. Validator evidence CREATE and separately
authorized Safe-inner anchoring remain unfinished. Installation and activation
flags remain false. No live RPC access/write, signing, chain mutation, Safe-inner
or DB operation was performed. External producer/source approval, authenticated
native-to-EVM mapping, custody exclusivity and owned-RPC state/finality trust
remain open. Source-supported setter behavior does not establish live evidence.

Twenty-nine deterministic top-level `TestEvmVaultLink*` roots were authored.
The fixture executes all six real EVM predecessor effects and the genuine vault
setter with synthetic identities and the existing local escrow-precompile model.
The inherited read-gas correction keeps historical/current/pending getter
simulation independent of the selected transaction's execution budget, including
this candidate's reverted-reserve prerequisite and low-gas vault call cases.
The new vault fixture opts into geth's intrinsic calldata-gas calculation and
data floor when executing its call. Earlier fixture behavior is unchanged by
the default-false instance flag. Because slot zero is already nonzero after
escrow, this setter is cheaper than reserve binding; the authored 23,000-gas
failure case subtracts intrinsic gas before execution to exercise actual EVM
exhaustion while keeping the registration word. It does not inject a fake failed
receipt. Tests were compiled but that behavioral assertion awaits qualification.

Coverage includes exact envelope/event/fee identity, each vault getter and raw
word, explicit cleared/noncanonical registration bytes, unused packed padding,
wrong coordinator and every accounting counter, distinct current coordinator
collision, unavailable reads, same-block reserve/proxy/implementation state,
original policy height, unchanged ancestor bytes, whole-graph preview, completed
and reverted reserve prerequisites, six reaudits and twelve checkpoint barriers,
seven locks and all custody aliases, cumulative attempts/future funding, lost
reply, ambiguous publication, both initial-claim boundaries, changed lineage,
call gas failure, output loss, expiry/runtime/nonce changes, historical runtime
changes and retained event/getter/storage digests. Positive tests use no `t.Run`
or sleep barriers. The checkpoint barrier waits until the reserve-binding
receipt's own proxy checks finish, so earlier proxy construction cannot satisfy
the six-predecessor causal assertion.

Compile-only `go test -c -p=2` passed for product, authored fixtures, final source
and the inherited read-gas correction. Gofmt and `git diff --check` passed as
source checks only. Logs,
external `compile.mod`/`compile.sum`, and the unexecuted `mainnet-static.test`
binary are under `/mnt/data/sn-testnet/worktrees/sn-mainnet-vault-link-20260928/`:
`compile-initial.log`, `compile-tests.log`, `compile-final.log` and
`compile-after-read-gas-fix.log`. No behavioral
root was run by the author. Committed module files, shared siblings and generated
contract artifacts/bindings were not changed.

The physical compile graph is the same separately pinned dependency graph under
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`:

| Module | Commit |
| --- | --- |
| Connect | `b163f9dd9ac374942fe97331f26631248a9c1f81` |
| Server | `5dc11761373580b5a0ddd9757cd6e4eb94140e27` |
| SDK | `516521fb16da46c9f4bff0b58221e1941694f616` |
| Proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` |
| UserWireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` |
| Warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` |
| Glog | `892ade4a6be396b32ea82a550f243190b5992180` |
| Goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` |

Sol should fence the containing exact commit, run `^TestEvmVaultLink` normally
and under race, then adjacent
`^TestEvm(Create|VaultCreate|CoordinatorCreate|EscrowRegister|ProxyCreate|ReserveLink|PhasePreview)`
roots and appropriate full mainnet/static gates. Exact shards may bound costly
scopes while preserving aggregate timeout evidence. Establish a passing original
root before each isolated causal control:

| Control | Root expected to reject its mutant |
| --- | --- |
| Remove exact vault-link calldata comparison | `TestEvmVaultLinkRejectsChangedEnvelope` |
| Bypass `evmVaultLinkReceiptEvent` | `TestEvmVaultLinkRequiresExactReceiptAndEvent` |
| Skip action-six inclusion vault getters | `TestEvmVaultLinkRequiresEveryVaultPostcondition` |
| Skip action-six inclusion vault storage | `TestEvmVaultLinkRequiresEveryVaultPostcondition` |
| Omit the registration byte from slot-zero expectation | `TestEvmVaultLinkPreservesPackedRegistrationAndAccounting` |
| Omit accounting slots eight through twelve | `TestEvmVaultLinkPreservesPackedRegistrationAndAccounting` |
| Skip `admitVaultLinkTarget` | `TestEvmVaultLinkRejectsCurrentCoordinatorCollisions` |
| Skip same-block bound-reserve observations | `TestEvmVaultLinkRequiresBoundReserveAtCurrentAndInclusion` |
| Skip proxy observations at vault inclusion | `TestEvmVaultLinkRechecksProxyAtBindingInclusion` |
| Substitute native height for original proxy EVM policy height | `TestEvmVaultLinkPreservesOriginalProxyPolicyHeight` |
| Skip online predecessor audits | `TestEvmVaultLinkReauditsSixPredecessors` |
| Remove both predecessor continuity loops in reconciliation | `TestEvmVaultLinkFencesSixPredecessorCheckpoints` |
| Omit predecessor attempts from the send allowance | `TestEvmVaultLinkKeepsCumulativeGraphAttempts` |
| Ignore later same-sender value/gas liability | `TestEvmVaultLinkPreservesFutureValueAndGasFunding` |

Retain exact mutant provenance and assertion output. A fixture, positive-test or
causal failure requires diagnosis; prior gates do not qualify this seventh action.
