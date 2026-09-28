# Reserve recorder binding source handoff

Source base: `a42a2ff351abc0ce9ba263be2393c5fb90297822`. Candidate branch:
`codex/mainnet-reserve-link-20260928`, isolated at
`/home/by/urnetwork/temp/sn-mainnet-reserve-link-20260928`. The containing commit
is the frozen qualification candidate. No behavioral test body was executed by
the implementation owner. Escrow qualification reported complete success;
proxy qualification was still active without a reported product defect during
this source freeze. This source handoff does not substitute for either gate.

`--action reserve-link` selects original approved index five. The nine-action
graph in `evm_phase.go` and reviewed `evm/script/Deploy.s.sol` put
`reserve.setRecorderOnce(address(proxy))` directly after initialized proxy CREATE
and before vault coordinator binding. The target is the derived reserve address,
the argument is the completed initialized proxy, and the sender is the same
bootstrap deployer at its next nonce. Value must be exactly zero. The generated
`stabi/streservesink.go` binding encodes exactly 36 bytes with selector
`0xc00d1252`. The selector and indexed `RecorderFixed(address)` event agree with
`evm/src/STReserveSink.sol` and the reviewed artifact ABI. Exact re-encoding
rejects a different target, implementation argument, noncanonical address word,
selector, trailing bytes, nonce/sender or value. The original graph payload and
approval bytes are never rewritten.

The reserve source permits only its immutable bootstrap caller, requires recorder
zero and nonempty new-recorder code, writes recorder once, and emits exactly one
event without external calls. Its reviewed compiler storage layout has address
`recorder` at slot zero, uint256 `principal` at slot one, and the operator-principal
mapping at slot two. Constructor immutables occupy no storage slots. The new
path checks the first two words; it does not invent a mapping enumeration or
native stake observation.

Before submission, current canonical and pending reads independently require
exact reserve runtime, its five constructor getters with recorder still zero,
zero principal, and both zero storage words. An observed nonzero recorder is a
distinct one-shot collision even when it names the intended proxy. A malformed
address word stays unresolved. Unavailable getter/storage reads return the read
error without publishing an attempt, collision or success. A consumed nonce
without the original receipt remains unresolved under existing recovery rules.

Successful binding receipt authentication requires original sender/target,
explicit null CREATE address, bounded original gas/fee tuple and the original
signed transaction at the authenticated position. Exactly one log must be
`RecorderFixed` from the reserve, with only the padded proxy address as its
indexed argument, empty data, exact transaction/block/position fields, explicit
log index and explicit `removed: false`. Reverted calls must retain no logs.
The event is observed under the existing owned-RPC receipt trust, not an
independent receipt-trie proof.

At the authenticated inclusion, the adapter checks exact reserve runtime and
six getters: netuid, reserve hotkey, mapped coldkey, bootstrap, recorder equal to
the approved proxy, and zero principal. Recorder slot zero equals the same
padded address; principal slot one stays zero. Getter/storage hashes and a new
`recorder_binding_hash` bind the retained observation. The latter seals the
graph-derived reserve/proxy pair and observed event log index.

Historical proxy initialization and present state are separate observations.
All five predecessor receipts are first reauthenticated at their original
inclusions. Current canonical/pending admission and binding inclusion then
independently check proxy runtime, its 23 stable initialized getters, the five
distinct implementation/admin/beacon/Initializable/Ownable words, and the
implementation runtime plus its two constructor slots at that same selected
block. Expected policy and epoch-zero boundaries retain the proxy's **original
EVM inclusion height** from the exact predecessor receipt. They cannot normalize
to native height, the approved input block, a latest clock or the binding block.
`currentEpoch()` is time-varying and is excluded from this later stable-state
projection; the original proxy receipt still requires its initial epoch zero.
This permits healthy clock advancement without accepting a changed initial
policy, owner, guardian, oracle or implementation. No Safe authority is inferred
from the configured owner address.

`reserve-link.json` has schema `urnetwork-mainnet-evm-reserve-link-state-v1` and a
marker sealing exact completed proxy custody. All six original journals/locks
remain held during each operation. Five predecessor hashes/checkpoints remain
required through initial reconciliation and refreshed-head admission; historical
files are never rewritten. The reserve CREATE journal therefore still describes
its original unbound state. `recorder_binding_hash` and `recorder_log_index` are
appended optional receipt fields, omitted from every old receipt. Index zero is
valid and is committed by the digest even when its numeric field is omitted.
Old schemas, JSON field order, hashes, signatures and markers remain unchanged.
Non-binding and reverted outcomes reject these additive fields.

The owner preserves the original graph's cumulative attempt allowance and all
later same-sender value plus maximum gas liability. A lost HTTP reply keeps the
same signed transaction; an ambiguous durable publication poisons the owner and
consumes its attempt on reopen. Exact initial-claim recovery, changed-ancestor
refusal, missing-child refusal, consumed-nonce recovery and output-loss recovery
continue under the existing bounded rules. The maximum-eight attempt policy is
unchanged; this is not a complete nine-action execution policy.

Completion is `reserve-recorder-bound`; a canonical revert is
`reserve-binding-reverted-nonce-consumed`. Three installation actions remain:
vault coordinator binding, validator evidence CREATE and separately authorized
Safe-inner anchoring. Installation and activation flags stay false. No live
signing, RPC access/write, chain mutation, Safe-inner action or DB operation was
performed. External producer/source approval, authenticated native-to-EVM
mapping and owned-RPC finality/state trust remain open; the local fixture does
not qualify live deployment, custody exclusivity, Safe authority or consensus.

Twenty-seven deterministic top-level `TestEvmReserveLink*` roots were authored.
The new fixture extends genuine predecessor EVM execution and executes the real
reserve binding bytecode; it introduces no shared fixture or process-global
policy change. The existing escrow native-precompile model remains synthetic.
Tests distinguish historical, current canonical, pending and binding-inclusion
state explicitly. They cover original envelope/event identity, each reserve
postcondition, each stable proxy getter/namespace and implementation word,
current one-shot collisions, unavailable reads, original proxy policy height,
whole-graph preview, completed/reverted proxy prerequisites, five online audits
and ten checkpoint barriers, six locks, cumulative attempts/future funding,
lost reply, ambiguous publication, initial-claim boundaries, changed lineage,
genuine call gas exhaustion, wrong signature/aliases, output failure,
expiry/runtime/nonce movement, later historical runtime change and retained
digest validation. Completion also checks ancestor journal/marker bytes and
legacy output-field absence. Positive tests use no `t.Run` or sleep barriers.

Compile-only `go test -c -p=2` passed for product and all authored tests using an
external absolute-path `-modfile`. The unexecuted binary and compile logs are at
`/mnt/data/sn-testnet/worktrees/sn-mainnet-reserve-link-20260928/`:
`compile-initial.log`, `compile-tests.log`, `compile-final.log`, and
`mainnet-static.test`. Gofmt and `git diff --check` are source checks. Compilation
and authored-but-unexecuted tests do not qualify behavior. No committed module
file or shared dependency was changed.

The same physical dependency graph used for the proxy compile is under
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

Sol should fence the containing exact source commit, run
`^TestEvmReserveLink` normally and under race, then adjacent
`^TestEvm(Create|VaultCreate|CoordinatorCreate|EscrowRegister|ProxyCreate|PhasePreview)`
roots and appropriate full mainnet/static gates. Split costly scopes into exact
root shards if needed, retaining bounded aggregate timeouts as evidence. The
following causal controls require a passing original root first:

| Control | Root expected to reject its mutant |
| --- | --- |
| Remove exact reserve-link calldata comparison | `TestEvmReserveLinkRejectsChangedEnvelope` |
| Bypass `evmReserveLinkReceiptEvent` | `TestEvmReserveLinkRequiresExactReceiptAndEvent` |
| Skip action-five inclusion reserve getters | `TestEvmReserveLinkRequiresEveryReservePostcondition` |
| Skip action-five inclusion reserve storage | `TestEvmReserveLinkRequiresEveryReservePostcondition` |
| Skip `admitReserveLinkTarget` | `TestEvmReserveLinkRejectsCurrentRecorderCollisions` |
| Skip same-binding-inclusion `authenticateReserveRecorder` | `TestEvmReserveLinkRechecksRecorderAtBindingInclusion` |
| Replace original proxy EVM policy height with native height | `TestEvmReserveLinkPreservesOriginalProxyPolicyHeight` |
| Skip online predecessor receipt audits | `TestEvmReserveLinkReauditsFivePredecessors` |
| Remove both predecessor continuity loops in `evmOwnedChain.reconcile` | `TestEvmReserveLinkFencesFivePredecessorCheckpoints` |
| Omit prior attempts from the send allowance | `TestEvmReserveLinkKeepsCumulativeGraphAttempts` |
| Ignore later same-sender value/gas liability | `TestEvmReserveLinkPreservesFutureValueAndGasFunding` |

Retain exact mutant provenance and assertion output. A positive-test, fixture or
causal failure requires source diagnosis; predecessor gates do not qualify this
sixth installation action.
