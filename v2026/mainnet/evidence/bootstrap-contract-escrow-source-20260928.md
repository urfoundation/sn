# Escrow registration source handoff

Source base: `664bea6096f31a9437d743a818b61ac6a61e0792`. Candidate branch:
`codex/mainnet-escrow-register-20260928`, isolated at
`/home/by/urnetwork/temp/sn-mainnet-escrow-register-20260928`. The containing
source commit is the frozen qualification candidate. This is an implementation
handoff; no behavioral test body was executed by the implementation owner.

`--action escrow-register` selects only original approved index three. The call
target is the vault derived from index one's deployer/nonce, verified against
the reviewed `STSettlementVault.sol`, generated `stabi/stsettlementvault.go`,
release ABI and runtime projection. It is not the coordinator implementation.
The same deployer's next nonce must call exact 36-byte
`registerEscrow(uint64)` calldata. Nonzero uint64 cap and exact
`cap * 1_000_000_000` wei value are decoded from the already approved envelope;
full-width arithmetic prevents narrowing. No new config field or approval is
invented, and the original action-three bytes and whole signed graph survive.

Inspected native source is the exact pinned commit
`67dcf7f791dc495064c293f080a0702cb433e51e`: `runtime/src/lib.rs` defines the
rao-to-wei factor in `SubtensorEvmBalanceConverter`; `precompiles/src/neuron.rs`
dispatches `register_limit` using the mapped caller and reads `Uids` through
`getUid`; `precompiles/src/metagraph.rs` reads `Keys` through `getHotkey` and
`Keys` plus `Owner` through `getColdkey`. Inspection used `git show` of that
commit, not the unrelated current checkout HEAD. Solidity's reviewed interfaces
place neuron at `0x804` and metagraph at `0x802`.

Success requires the authenticated original transaction and gas tuple, exact
receipt sender/target, null `contractAddress`, and one vault
`EscrowRegistered(bytes32 indexed hotkey,uint16 uid)` event. Log identities,
explicit positions, `removed: false`, unique index, exact hotkey and canonical
uint16 word are mandatory. Zero UID is valid. At that same inclusion the
adapter checks patched vault runtime and thirteen getters (only the one-shot
flag changes), then `getUid(uint16,bytes32)`, `getHotkey(uint16,uint16)` and
`getColdkey(uint16,uint16)` against the event and immutable identities. All calls
use canonical EIP-1898 block objects. Existing native ancestry, Frontier raw
header, canonical EVM lookup, historical execution/parent runtime and native
first-insertion mapping checks remain in force, including final rechecks.
These remain owned-route observations under separately approved source/runtime
and finality trust, not independent consensus or storage proofs.

Fresh admission checks canonical and pending vault runtime, all unregistered
constructor getters and absent neuron UID, after all three ancestors have been
reauthenticated. Each ancestor's checkpoint remains required through selected
reconciliation and refreshed-head admission. `escrow-register.json` has a
separate schema and marker sealing the exact completed coordinator record,
which seals vault and reserve. All four locks and journals are held/required;
ancestor files are never rewritten. Attempts include all three predecessors
against the unchanged maximum-eight graph policy. Funding includes exact call
value plus maximum gas and later same-sender reservations. Ambiguous send,
publication failure, initial-claim recovery, restart, output loss and consumed
nonce reuse the durable original-signature owner mechanics.

The additive receipt registration digest binds cap, funding, hotkey, coldkey,
UID and log index. Zero UID/log index fields are omitted but still hashed.
All new fields remain omitted from historical CREATE receipts, preserving
serialized bytes/content hashes and existing signed plan/approval bytes.
Status is `escrow-registered` or
`escrow-registration-reverted-nonce-consumed`; installation and activation stay
false with five actions remaining. Proxy construction, links, evidence and
Safe-inner work are outside this change.

Reviewed vault source enforces bootstrap-only one-shot registration, dispatches
the capped native call and refunds surplus above the previous reducible
balance. Failed execution rolls those effects back. These are source-supported
bounded semantics; this owner does not measure actual native burn, existential
deposit, refund or native balance from receipt gas. No live approval, signing,
RPC write, deployment or native execution evidence is supplied here.

Twenty-four top-level `TestEvmEscrowRegister*` roots have been authored. They use
real reviewed reserve/vault/coordinator constructors, the real payable vault
method, public commands, private journals and local HTTP. Instance-local EVM
contracts explicitly model neuron registration/UID and metagraph identities,
with zero burn, so the real vault's surplus-return path executes. That model is
not a runtime qualification or a proof of native burn/ED behavior. No process
global precompile map or mutable policy is changed. The shared fixture's call
branch retains genuine events and separately models transaction nonce
consumption around geth's lower-level `runtime.Call`; CREATE behavior stays in
its prior branch.

Coverage includes zero/max UID, full-width uint64 cap and total liability,
changed selector/width/cap/value/target/nonce/sender, unsigned preview, call/event
faults, every canonical vault getter and runtime, every mapping and archive
absence, every canonical/pending precondition, completed/reverted predecessor
admission, all three re-audits/checkpoints, cumulative attempts and future
funding, lost reply, poisoned publication, both initial-claim boundaries, all
four locks, missing child custody, changed transitive lineage, genuine gas
failure, wrong signature/aliases, output loss, expiry/nonce movement, later
runtime change and retained registration digest validation. Ancestor file bytes
and legacy outputs are checked in the full execution root.

Compile-only `go test -c -p=2` passed with an external `-modfile` that replaces
sibling modules by absolute paths into the already qualified physical graph
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`.
No active sibling checkout or committed module file was changed. Pins:

| Module | Commit |
| --- | --- |
| Connect | `b163f9dd9ac374942fe97331f26631248a9c1f81` |
| SDK | `516521fb16da46c9f4bff0b58221e1941694f616` |
| Server | `5dc11761373580b5a0ddd9757cd6e4eb94140e27` |
| Proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` |
| UserWireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` |
| Warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` |
| Glog | `892ade4a6be396b32ea82a550f243190b5992180` |
| Goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` |

Compile log and unexecuted binary are under
`/mnt/data/sn-testnet/worktrees/sn-mainnet-escrow-register-20260928/` as
`compile-aligned.log` and `mainnet-static.test`. Gofmt and `git diff --check`
passed as source checks. A final compile-only recheck also exited zero; its
adjacent log is `compile-aligned-final.log`. The binary was not run.
Compilation and unexecuted tests do not qualify behavior.

Sol should freeze the containing source commit in its own fenced physical graph
and run `^TestEvmEscrowRegister` normally and under race. Adjacent selectors are
`^TestEvm(Create|VaultCreate|CoordinatorCreate|PhasePreview)` plus package static
checks/full mainnet normal and race gates as appropriate. Suggested isolated
causal controls, with a passing original root established first:

| Control | Original root expected to reject its mutant |
| --- | --- |
| Remove exact `action.ValueWei != funding.String()` refusal | `TestEvmEscrowRegisterRejectsChangedEnvelope` |
| Bypass `evmEscrowReceiptEvent` | `TestEvmEscrowRegisterRequiresExactReceiptAndEvent` |
| Skip the three precompile postcondition reads | `TestEvmEscrowRegisterRequiresCanonicalNativeMappings` |
| Skip `admitEscrowTarget` | `TestEvmEscrowRegisterAdmitsOnlyUnregisteredCurrentVault` |
| Skip ancestor online receipt re-audits | `TestEvmEscrowRegisterReauditsThreePredecessors` |
| Remove both predecessor continuity loops in `evmOwnedChain.reconcile` | `TestEvmEscrowRegisterFencesThreePredecessorCheckpoints` |
| Remove prior-attempt accumulation from the send allowance | `TestEvmEscrowRegisterKeepsCumulativeGraphAttempts` |
| Ignore later same-sender value/gas funding | `TestEvmEscrowRegisterPreservesCurrentAndFutureFunding` |

Retain exact mutant provenance and failure output. A failing positive or fixture
is a qualification failure requiring source diagnosis; prior CREATE gates do
not qualify this fourth action.
