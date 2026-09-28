# Executable contract bootstrap through reserve recorder binding

`bootstrap-contracts preview/plan/apply/resume` implements the first six installation
actions: exact `STReserveSink`, `STSettlementVault` and `STCoordinator` implementation CREATE, the vault's bounded `registerEscrow` call, atomic initialized `ERC1967Proxy` CREATE, then the reserve's one-shot recorder binding through retained
public EVM custody and the owned HTTP submission adapter. `--action reserve-create`
is the unchanged default; `--action vault-create`, `--action coordinator-create`,
`--action escrow-register`, `--action proxy-create` and `--action reserve-link` explicitly select actions one through five from
the **same already approved graph**.
It does not install the remaining contracts, anchor
evidence, register miners, change emissions, activate validators, or complete
mainnet bootstrap. The [reserve qualification receipt](evidence/bootstrap-contract-qualification-20260928.md)
records its passed normal/race scopes, retained fixture failures and composed
dependency check. The [vault qualification receipt](evidence/bootstrap-contract-vault-qualification-20260928.md)
records its separate normal/race and causal checks. The
[implementation source handoff](evidence/bootstrap-contract-coordinator-source-20260928.md)
records the coordinator scope and pending behavioral qualification. Prior reserve
and vault results do not qualify this new implementation path.
The [escrow source handoff](evidence/bootstrap-contract-escrow-source-20260928.md)
records its source scope and pending behavioral qualification. The local escrow
fixtures model native precompiles explicitly; they do not qualify live burn or
existential-deposit behavior.
The [proxy source handoff](evidence/bootstrap-contract-proxy-source-20260928.md)
records the atomic initializer scope and pending separate behavioral qualification.
The [reserve binding source handoff](evidence/bootstrap-contract-reserve-link-source-20260928.md)
records the one-shot call, five-predecessor custody and separate qualification scope.

The release catalog comes from the existing generator:

```sh
go run ./sim-testnet/gencontracts --release-json evm/out sim-testnet/contracts_gen.go /secure/ur-mainnet/contracts.json
```

This additive export keeps the established source/layout/retained-bytecode
checks, emits only the five production artifacts, and does not compile Solidity
or modify the committed bindings/catalog. `STValidatorEvidence` is included;
simulator adversaries, probes and fleet helpers are excluded. The independently
signed phase pins the exact exported file. The reserve builder independently
recreates its constructor, predicted CREATE address, address-derived mapped
coldkey, semantic immutable words and five constructor getter results. Vault
selection additionally checks the release creation prefix and exactly six
constructor words. Its escrow hotkey, minimum claim TTL in blocks and minimum
transfer in rao are decoded from already approved calldata and exported for
review. No new unsigned configuration field supplies those values. The builder
independently recreates netuid 25, the next CREATE address and mapped coldkey,
and the same deployer's bootstrap address. Both uint64 amounts must be nonzero,
the escrow hotkey must be nonzero and distinct from the reserve hotkey, and the
complete encoding must equal the generated binding's constructor encoding.

Coordinator selection requires the reviewed release creation bytes with no
constructor arguments, the same deployer's next zero-value CREATE after the
vault, and the predicted address in the UUPS `__self` immutable. Its constructor
only disables initialization. The implementation's owner, guardian, network,
vault, reserve and evidence getters remain zero; those are not initialized proxy
values. Proxy initialization is a separate explicitly selected action.

Escrow selection requires approved action three to call that exact vault at the
same deployer's next nonce. Its calldata must be the generated binding's exact
36-byte `registerEscrow(uint64)` encoding with a nonzero cap. Value must equal
`maximumBurnRao * 1_000_000_000` wei, using full-width arithmetic. The conversion
comes from the approved runtime source's `SubtensorEvmBalanceConverter`.
The cap, hotkey and mapped coldkey are derived review fields; the original
approved payload, signed plan schema and approval hash remain unchanged.

Proxy selection requires the next zero-value CREATE after escrow. The complete
release creation bytes and `(implementation, initializer)` constructor ABI must
repack exactly, including the nonempty generated `STCoordinator.initialize`
call. Implementation, netuid, reserve, vault and proxy-mapped coldkey derive from
the same graph. Nonzero owner, guardian, commitment oracle and the initial policy
are decoded only from approved calldata. The source's policy bounds and immutable
vault claim-window condition are checked with full-width arithmetic. No live
Safe authority is inferred from the configured owner address.

Reserve binding selection requires action five to call the derived reserve with
the generated binding's exact 36-byte `setRecorderOnce(address)` calldata
(selector `0xc00d1252`). The argument is the initialized proxy's derived address,
the sender is the same bootstrap deployer at its next nonce, and value is zero.
`evm/script/Deploy.s.sol` places this call immediately after proxy creation.
The decoded `reserve_binding` review fields add no new authority or payload.

## Approval and custody

The `urnetwork-mainnet-contract-phase-config-v1` config contains an independently
provisioned Ed25519 approval key, the complete typed plan, and its signature over
`urnetwork-mainnet-contract-phase-approval-v1` followed by a zero byte and the
canonical Go JSON plan. The config/key must come from the trusted approval
channel; a key supplied by an untrusted journal or node is not independent
authority. `plan` verifies the signature and exact release file before emitting
the plan hash. `apply` and `resume` require that same accepted hash and directory.

`approval_signature_ed25519` is exactly **128 unprefixed lowercase hexadecimal
characters** encoding the 64-byte signature. The public-key field is separately
encoded as `0x` plus 64 lowercase hexadecimal characters. Prefixing the signature
with `0x`, uppercasing it, or accepting another spelling is invalid. This uses the
existing root custody signature parser without changing its wire contract.

`preview` accepts the same configuration with an empty
`approval_signature_ed25519`. It validates the finite graph, public-key format,
route syntax, amount bounds, exact artifact file and reserve constructor, then
exports the typed plan, plan hash, predicted address/runtime hash, and exact
domain-separated approval bytes as `approval_signing_message_hex` (unprefixed
lowercase hexadecimal). `approval_signing_message_sha256` identifies those
bytes. The exported `approval_verified` value is always false. A supplied
signature is rejected by preview; `plan` is the signed approval review command.
With `--action vault-create`, preview validates both constructor projections and
exports both addresses/runtime hashes plus `vault_constructor`. It signs the
same complete graph bytes as reserve preview; selecting an action neither
extends the graph nor changes its approval hash.
With `--action coordinator-create`, preview additionally exports
`coordinator_implementation_address`, `expected_coordinator_runtime_hash` and
the two exact `coordinator_storage` slot/word expectations. The signed plan
schema, approval message and accepted hash remain unchanged.
With `--action escrow-register`, preview additionally exports
`escrow_registration`: the decoded cap in rao, exact funded wei, immutable
escrow hotkey and vault-mapped coldkey. It includes all three prior address and
runtime projections under the same complete graph hash.
With `--action proxy-create`, preview also exports
`coordinator_proxy_address`, `expected_proxy_runtime_hash`, `proxy_constructor`
and `proxy_storage`. `proxy_constructor.approved_policy` preserves the original
input fields, including the effective epoch/block words. Reviewed initialization
overwrites those two words to epoch zero and the actual EVM inclusion height;
the command preserves the signed payload and derives only the expected observed
policy from that authenticated height.
With `--action reserve-link`, preview preserves all those proxy review fields and
adds `reserve_binding` plus `reserve_binding_storage`: recorder in slot zero
must equal the approved proxy and principal in slot one must stay zero.

Preview reads only the draft and artifact files. It does not inspect or create
the future run directory, acquire its journal lock, load a key, or open a network
route. A valid preview does not authenticate externally supplied runtime,
custody, cutover or network claims. Provide those exact inputs through the
independent approval workflow; no live values are inferred by this command.

Prepare the bounded draft, review the exported plan and exact payload, then
have the independent approval signer sign the **decoded message bytes**, not
the SHA-256 digest or the text containing hexadecimal characters:

```sh
umask 077
sn-mainnet bootstrap-contracts preview --config /secure/ur-mainnet/contract-phase.unsigned.json > /secure/ur-mainnet/contract-phase.preview.json
jq -r '.approval_signing_message_hex' /secure/ur-mainnet/contract-phase.preview.json | xxd -r -p > /secure/ur-mainnet/contract-phase.approval.bin
```

Put the returned 64-byte signature's canonical 128-character encoding into the
same config's `approval_signature_ed25519` field. Preserve the exact typed plan
and independently provisioned public key. JSON whitespace may change; any
plan-field change requires a fresh review and signature. `plan`, `apply` and
`resume` all verify this signature before any journal or RPC owner can open.
Preview accepts no execution, signed-transaction, run-directory or accepted-hash
flags. Its successful exit only means local structural/artifact review passed.

The phase binds the mainnet EVM chain ID 964, independently approved native chain
and genesis, exact runtime version/code/metadata and inspected source commit,
native starting checkpoint and finite send window, fixed owned IP/port route,
source-lock/cutover/custody-fence evidence hashes, custody identifier, artifact
file, journal directory, finite attempts, total value plus maximum gas liability,
and every prepared action's sender, nonce, target, calldata, value, gas and fees.
HTTPS also requires normal certificate validation and the approved SPKI pin.
No DNS, proxy, redirect, endpoint fallback or automatic write retry is admitted.

The bounded graph may contain the ordered prefix of one through nine actions.
One approval covers all supplied action reservations; no per-step confirmation
is invented. Actions zero through five are executable in this candidate. Later actions
remain sealed reservations, not claims that their semantic builders or Safe
executor exist. Extending a prefix changes authority and cannot silently amend
an existing journal. A previously approved reserve-only prefix cannot gain vault
authority on reopen. Each prepared vault and implementation must be a zero-value
CREATE by the reserve deployer at exactly the next nonce. Later executors and any full-installation
attempt policy remain separate unfinished work.

For the six executable actions, this candidate conservatively counts all
their submission attempts together against the original `maximum_attempts`.
Child resume cannot renew any predecessor's spent allowance. The existing limit of
eight remains unchanged; this is not an implemented nine-action send policy.
`maximum_total_wei` still bounds the value plus maximum gas liability of every
approved action. Every child action's send admission also requires enough
pending balance for the selected action and all later sealed reservations
belonging to the same sender.

Only empty-access-list EIP-1559 envelopes are admitted here. A separate signer
returns the original **binary signed transaction**, not a key or a signing
callback. Import verifies canonical bytes, recovered sender, chain and every
approved unsigned field. Subsequent imports cannot substitute another signature.
Offline `apply/resume` perform no RPC reads or writes and request no private key.

```sh
sn-mainnet bootstrap-contracts plan --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/reserve.signed.bin --signed-transaction-hash "$SIGNED_FILE_HASH"
```

After canonical reserve completion has been retained under that same approval,
select the already prepared vault. `apply` exports its exact unsigned envelope;
the separate signer returns original binary bytes for `resume` import:

```sh
sn-mainnet bootstrap-contracts plan --action vault-create --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --action vault-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --action vault-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/vault.signed.bin --signed-transaction-hash "$VAULT_SIGNED_FILE_HASH"
```

After the vault's canonical completion has also been retained, prepare and
import the already approved implementation envelope under the same graph hash:

```sh
sn-mainnet bootstrap-contracts plan --action coordinator-create --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --action coordinator-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --action coordinator-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/coordinator.signed.bin --signed-transaction-hash "$COORDINATOR_SIGNED_FILE_HASH"
```

After all three exact CREATE outcomes are retained, select and import the
already approved funded call. The command never chooses a new burn cap:

```sh
sn-mainnet bootstrap-contracts plan --action escrow-register --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --action escrow-register --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --action escrow-register --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/escrow.signed.bin --signed-transaction-hash "$ESCROW_SIGNED_FILE_HASH"
```

After the exact escrow event/mapping outcome is retained, select the original
proxy CREATE and its already approved atomic initializer:

```sh
sn-mainnet bootstrap-contracts plan --action proxy-create --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --action proxy-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --action proxy-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/proxy.signed.bin --signed-transaction-hash "$PROXY_SIGNED_FILE_HASH"
```

After initialized proxy completion is retained, select the original reserve
binding from that same approval:

```sh
sn-mainnet bootstrap-contracts plan --action reserve-link --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --action reserve-link --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --action reserve-link --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/reserve-link.signed.bin --signed-transaction-hash "$RESERVE_LINK_SIGNED_FILE_HASH"
```

The result distinguishes `signature-awaiting-import`, `signed-custody-complete`,
uncertain/pending chain work, `reserve-created`, `vault-created`,
`coordinator-created`, `escrow-registered`, `proxy-created-initialized`, `reserve-recorder-bound`, and a reverted action with its
nonce consumed. The escrow failure status is
`escrow-registration-reverted-nonce-consumed`; reserve binding failure is
`reserve-binding-reverted-nonce-consumed`. `installation_complete` and
`activation_ready` remain false.
Vault results retain `reserve_address` and add `vault_address` and
`executable_action: "vault-create"`; seven installation actions remain. Vault
creation does not register its escrow or bind its coordinator.
Implementation results also carry `coordinator_implementation_address` and
`executable_action: "coordinator-create"`; six installation actions remain.
Escrow results retain those addresses and add the derived `escrow_registration`
review fields with `executable_action: "escrow-register"`; five actions remain.
Registration does not bind the coordinator or initialize the future proxy.
Proxy results add `coordinator_proxy_address` and `proxy_constructor` with
`executable_action: "proxy-create"`; four actions remain after proxy creation.
Reserve binding results retain those addresses and add `reserve_binding` with
`executable_action: "reserve-link"`; three actions remain. Vault coordinator
binding and validator evidence deployment/anchoring are still unfinished;
binding the recorder does not complete installation or activation.
An offline reopen preserves each completed status and the original receipt,
with `receipt_observation: "retained"`. A successful online receipt audit emits
`receipt_observation: "revalidated-online"`. Retained completion is historical
local evidence; it does not claim a new observation of canonical chain state.
An online observation requires explicit `--online` on `resume`; adding `--submit`
permits at most one originally approved attempt after reconciliation and fresh
admission. The command never loads a key, generates a transaction replacement,
changes nonce, reprices fees, or redeploys automatically.

## Durable recovery and canonical postconditions

`reserve-create.json` is authoritative for the original signed bytes/hash, finite
attempt count, scanned native checkpoint and completed receipt. Its private
regular-file marker is held with a local flock. Atomic publication syncs file
and parent directory before acknowledging state. A failed publication poisons
the owner; reopen reloads the actual complete record. Only the exact unfinished
initial marker with absent or untouched unsigned state can recover. A completed
marker with a missing/corrupt journal cannot become a new allowance.

`vault-create.json` uses a distinct schema and marker and seals the exact completed
reserve record in `predecessor_hash`, including its original signed bytes,
receipt, scan checkpoint and spent attempts. The reserve lock remains held while
vault custody is open; the vault path leaves the reserve file unchanged. Both
journals are required on resume. An absent, reverted, unauthenticated, changed
or unreadable reserve outcome cannot acquire or advance vault authority. The
old reserve schema, marker, content hash and default command output are preserved.

`coordinator-create.json` uses `urnetwork-mainnet-evm-coordinator-state-v1` and
binds the exact completed vault record in `predecessor_hash`. The vault in turn
binds the exact reserve record. All three journals and locks remain required;
neither ancestor file is rewritten by coordinator progress. A changed ancestor,
missing child after completed claim, ambiguous publication or process restart
cannot create a new graph allowance. Successful coordinator receipts also retain
`storage_hash`; the field is omitted from historical reserve/vault receipts so
their wire encoding and content hashes remain unchanged.

`escrow-register.json` uses `urnetwork-mainnet-evm-escrow-state-v1` and seals the
exact completed coordinator record, transitively including vault and reserve.
All four journals and locks remain required. All three ancestor files keep their
historical bytes. Successful call receipts add `registration_hash`, binding the
approved registration identities/cap and observed event UID/log index.
`escrow_uid` and `escrow_log_index` are omitted at zero; zero is valid and is
still bound by the digest. All new receipt fields are omitted from historical
CREATE receipts, preserving their serialized bytes and content hashes.

`proxy-create.json` uses `urnetwork-mainnet-evm-proxy-state-v1` and seals the
exact completed escrow record. Its four transitive ancestors remain held under
their own locks and keep their historical bytes. Every online proxy invocation
reauthenticates all four receipts, then fences their checkpoints through current
and refreshed admission. Existing signature custody, cumulative attempts,
publication poisoning and claim-recovery rules apply without new send authority.
No receipt field is added for proxy creation: `getter_hash` binds the initialized
policy at the authenticated EVM inclusion height and `storage_hash` binds the
five exact proxy storage words. Earlier actions keep their original hashes.

`reserve-link.json` uses `urnetwork-mainnet-evm-reserve-link-state-v1` and seals
exact initialized proxy custody. All six journals and locks are required, with
five historical receipts reauthenticated online and their checkpoints fenced
through current and refreshed admission. The reserve's original CREATE journal
still describes its earlier unbound state; the binding never rewrites it.
Successful binding receipts add `recorder_binding_hash`, committing the derived
reserve/proxy pair and event log index. `recorder_log_index` is omitted at zero,
which remains valid and bound by the digest. Both new fields are omitted from
all historical receipts; predecessor byte order, hashes and markers stay intact.

Reconciliation always precedes submission. A durable attempt is consumed before
the single HTTP write, including an interrupted or uncertain write. Later sends
use the same signed bytes and remaining attempt count. A pending or consumed
nonce without the canonical receipt remains unresolved; it never authorizes a
new nonce. Missing CLI output is recovered from the child journal.

For a receipt, the adapter authenticates its required fields, gas/fee bounds,
original transaction at the reported block/index, native header ancestry,
Frontier digest/raw EVM header/canonical EVM lookup, and the exact native
`Ethereum.BlockHash` first insertion using the independently approved runtime
at both execution and parent. Native and EVM heights are distinct. A successful
CREATE additionally requires byte-for-byte patched release runtime and constructor
getters at the canonical inclusion hash. Reserve has five getters; vault has
thirteen: its six immutable fields, zero coordinator, false escrow registration,
and zero total captured, total paid, pending funding, outstanding liability and
escrow accounted. Status 1 alone cannot complete an action. Every online vault
operation first reauthenticates the original reserve receipt and its historical
code/getters. Every online coordinator operation reauthenticates both completed
predecessors in order. Each retained native checkpoint also fences later
current-head admission, including a healthy head refresh; changed ancestor
ancestry cannot slip between those observations. Every online completed resume
rechecks the selected receipt too.
Every online escrow operation reauthenticates all three predecessors and fences
their checkpoints through selected-action reconciliation and refreshed-head
admission. Fresh escrow submission requires exact vault runtime, all thirteen
unregistered constructor getters and absent neuron UID in both canonical and
pending state. The target is an existing vault, so empty code is a refusal.

Coordinator completion checks eighteen getters: sixteen zero words for the
uninitialized implementation's public state and counts, the reviewed
`proxiableUUID()` implementation slot, and `UPGRADE_INTERFACE_VERSION()` equal
to `5.0.0`. Two `eth_getStorageAt` reads use that same already authenticated
canonical inclusion block hash with `requireCanonical: true`. Reviewed
OpenZeppelin `Initializable` packs `uint64 _initialized` and `bool _initializing`
at slot `0xf0c57e16840df040f15088dc2f81fe391c3923bec73e23a9662efc9c229c6a00`.
The expected word is 24 zero bytes followed by eight `ff` bytes: initialization
is disabled at `uint64` maximum and not in progress. A zero word would leave
initialization enabled and is rejected. The ERC1967 implementation slot
`0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc`
must itself contain zero because this address is the implementation, not the
future proxy. Native and EVM canonical mappings are checked again after these
observations. Missing archive support remains a read error, not completion.

Escrow success requires the original call transaction at its inclusion, exact
receipt sender and target, null `contractAddress`, and one vault-emitted
`EscrowRegistered(bytes32 indexed hotkey,uint16 uid)` event. Explicit event
block/transaction positions, `removed: false`, the approved hotkey, canonical
uint16 data and unique log index are checked. UID zero is valid. At that same
canonical inclusion hash the adapter requires exact vault runtime and thirteen
getters, with only `escrowRegistered` changed to true, then neuron `getUid` at
`0x804` and metagraph `getHotkey`/`getColdkey` at `0x802`. UID, hotkey and the
vault's mapped coldkey must agree. Final native/EVM mapping checks still run
after those observations. A reverted call must have no events and retains no
successful registration digest.

Reviewed `STSettlementVault.sol` limits the bootstrap caller to one attempt that
succeeds, calls native `registerLimit` with the approved cap, and refunds surplus
while preserving the prior reducible balance. Failed execution rolls back its
flag and effects. These are source-supported bounded semantics. This adapter
does not measure actual native burn, existential deposit, refund or resulting
native balance from receipt gas fields; no such live observations are claimed.

Proxy receipt identity requires the original sender, null call target and exact
created address, alongside the original transaction bytes and financial bounds.
At that inclusion the adapter checks exact proxy runtime and 24 getters: network,
mapped coldkey, reserve/vault addresses, owner, configured/active guardian and
oracle, one policy, zero pending roles/epochs, unpaused state, zero campaign
reservation/operators/evidence/current epoch, the interface version, initial
policy by index and epoch, and epoch-zero start/end blocks. The two policy reads
must equal approved fields with effective epoch zero and effective block equal
to the receipt's EVM height. Native height, latest head and the ignored approved
input block cannot substitute for that observation.

Five `eth_getStorageAt` observations use the same canonical block: ERC1967
implementation is the completed implementation address; admin and beacon are
zero; the Initializable namespace holds version one with `_initializing` false;
and the Ownable namespace holds the exact approved owner. The owner namespace is
`0x9016d09d72d40fdae2fd8ceac6b6234c7706214fd39c1cd1e609a0528c199300`.
Admin/beacon zero does not imply owner zero. The adapter also reads the reviewed
implementation runtime and its disabled-initializer/zero-implementation storage
at this proxy inclusion, separately from its historical CREATE receipt. All
getters, storage and code share the authenticated canonical block object and
the final mapping rechecks. A reverted proxy CREATE retains only the consumed
nonce outcome; it cannot become initialized completion or a replacement nonce.

Reserve binding admission separately requires exact reserve runtime, all five
constructor getters with recorder still zero, zero principal, and zero recorder
and principal slots at both current canonical and pending state. An already
fixed recorder, including the intended proxy, prevents another call. Missing
getter/storage reads remain unresolved read errors; they neither establish a
collision nor authorize submission.

Binding completion requires explicit original receipt sender/target, null
CREATE address, exact transaction position and financial bounds. The successful
call must emit exactly one `RecorderFixed(address indexed recorder)` event from
the reserve, with the canonical padded proxy topic, no data, and exact receipt
transaction/block/index identity with explicit `removed: false`. At that same
authenticated inclusion the reserve runtime, six getters and two storage words
must match, with recorder changed only to the approved proxy and principal zero.

Present canonical/pending admission and binding inclusion independently recheck
the initialized recorder's exact proxy runtime, 23 stable getters, all five
proxy storage words, and implementation runtime plus its two constructor slots.
The initial policy still uses the proxy's **original EVM inclusion height** from
its retained and reauthenticated receipt, not the later binding block. The
time-varying `currentEpoch()` getter belongs only to the original proxy receipt
projection; later binding checks retain the exact policy/epoch-zero boundaries
while allowing the clock to advance. These observations do not infer live Safe
authority from the configured owner or introduce a Safe-inner operation.

Each invocation scans at most 128 new native headers. Completed chunks are
durable; an interrupted current chunk is repeated, at most 128 headers. A failed
candidate read cannot advance past that candidate. Archive unavailability
remains a read error, not evidence of a successful-value mismatch. Healthy head
advancement refreshes current runtime/nonce/balance/collision admission within
the same bounded operation rather than requiring a stationary chain. Changed
ancestry, runtime or custody observations still stop the write.

## Remaining live trust and graph work

The local flock is not a distributed deployer-key fence. Signed hashes attest
externally reviewed evidence; this command does not independently prove the
cutover, global custody exclusivity, source-to-Wasm provenance or ongoing runtime
governance freeze. Owned RPC finality/state remain assertions, not GRANDPA or
storage proofs. All live identity, route, funding, runtime and custody approvals
remain required inputs. No live signing, RPC write or deployment qualified this
candidate.

The [offline recovery and approval-preview follow-up](evidence/bootstrap-contract-preview-20260928.md)
has separate command regressions that passed normal/race qualification. Its local
signature fixture does not supply live approval authority.

EVM signatures have **no native block expiry**. The finite native window only
stops new local submissions. Retained bytes remain a liability until a canonical
nonce outcome is reconciled; approval expiry must never release that reservation.
A later unrelated runtime upgrade does not prevent authenticating an inclusion
under the originally approved historical runtime. Inclusion under an unapproved
execution/parent runtime remains unresolved and needs separately qualified
authority migration, not a fresh journal or an inferred compatibility waiver.

The remaining graph is vault coordinator binding, `STValidatorEvidence`
CREATE, and the separately authorized Safe `fixValidatorEvidence` call. The
last action needs exact Safe digest/signature/nonce ownership and the outer
relayer's distinct nonce/fee reservation. Both the Safe inner success and the
canonical coordinator getter must agree; outer EVM status 1 is insufficient.
Do not treat the existing `Deploy.s.sol` sequence as complete evidence anchoring.
