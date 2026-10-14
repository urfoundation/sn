# Executable contract bootstrap through unanchored evidence CREATE

`bootstrap-contracts preview/plan/apply/resume` implements the first eight installation
actions: exact `STReserveSink`, `STSettlementVault` and `STCoordinator` implementation CREATE, the vault's bounded `registerEscrow` call, atomic initialized `ERC1967Proxy` CREATE, the reserve recorder and vault coordinator bindings, and immutable `STValidatorEvidence` CREATE through retained
public EVM custody and the owned HTTP submission adapter. `--action reserve-create`
is the unchanged default; `--action vault-create`, `--action coordinator-create`,
`--action escrow-register`, `--action proxy-create`, `--action reserve-link`, `--action vault-link` and `--action evidence-create` explicitly select actions one through seven from
the **same already approved graph**.
It does not anchor evidence, register miners, change emissions, activate validators, or complete
mainnet bootstrap. The same command also runs the separately approved
[settlement v2 contract phase](#settlement-v2-contract-phase). The [reserve qualification receipt](evidence/bootstrap-contract-qualification-20260928.md)
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
The [reserve binding qualification](evidence/bootstrap-contract-reserve-link-qualification-20260928.md)
records its complete normal/race root coverage, full normal package, causal
controls and retained fixture/time-limit failures.
The [getter-gas fixture correction](evidence/bootstrap-contract-read-gas-fixture-correction-20260928.md)
records its reproduced positive failure and independent read simulation budget.
The [vault binding source handoff](evidence/bootstrap-contract-vault-link-source-20260928.md)
records its packed storage and six-predecessor custody. Its
[qualification](evidence/bootstrap-contract-vault-link-qualification-20260929.md)
covers all 29 race roots, the corrected checkpoint normal root, adjacent
checkpoint roots, causal controls and retained test-time-limit attempts.
The [vault selector fixture correction](evidence/bootstrap-contract-vault-selector-fixture-correction-20260928.md)
records the pending-selector panic and deterministic callback correction.
The [evidence CREATE source handoff](evidence/bootstrap-contract-evidence-source-20260928.md)
records its provisional eight-action prefix and separate pending qualification.

Evidence CREATE (index seven) is provisionally integrated with the
[admission test correction](evidence/bootstrap-contract-evidence-admission-test-correction-20260929.md).
Its initial integration had all 28 normal action roots passing, with race and
live deployment gates still open.

The [graph work correction](evidence/bootstrap-contract-plan-graph-work-20260929.md)
is integrated from frozen source `1c87fce8`. Its
[qualification receipt](evidence/bootstrap-contract-plan-graph-qualification-20260929.md)
records complete scoped checks: 5/5 graph roots, 28/28 evidence roots and 177/177
adjacent roots pass normally and under race, and all six graph causal controls
discriminate. The exact seven-predecessor checkpoint race passed in 1975.38 seconds
under its 60-minute retry; the original 30-minute package timeout remains recorded
as a test time limit. The independent full `./mainnet` normal package reached
its 60-minute timer after 299 top-level passes and zero root assertions; its
unfinished root had already passed in exact adjacent normal and race runs.
That broader package remains incomplete, and live deployment gates remain open.

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
are decoded only from approved calldata. Deployer, owner, guardian and commitment
oracle addresses must be pairwise distinct, matching `Deploy.s.sol`. Proxy and
descendant selection enforce this in both signed review and unsigned preview.
The source's policy bounds and immutable vault claim-window condition are
checked with full-width arithmetic. No live Safe authority is inferred from
the configured owner address.

Reserve binding selection requires action five to call the derived reserve with
the generated binding's exact 36-byte `setRecorderOnce(address)` calldata
(selector `0xc00d1252`). The argument is the initialized proxy's derived address,
the sender is the same bootstrap deployer at its next nonce, and value is zero.
`evm/script/Deploy.s.sol` places this call immediately after proxy creation.
The decoded `reserve_binding` review fields add no new authority or payload.

Vault binding selection requires action six to call the derived vault with the
exact 36-byte `setCoordinatorOnce(address)` payload (selector `0xf406b76b`). Its
argument is the same initialized proxy, value is zero, and the bootstrap deployer
uses the next nonce after reserve binding. The reviewed deployment script calls
it immediately after `setRecorderOnce`. `vault_binding` is derived review data.

Evidence selection requires index seven to be the same deployer's next zero-value
CREATE after vault binding. The exact release creation bytes are followed by
three canonical ABI words: the derived initialized proxy, the approved native
genesis hash, and SHA-256 of the approved deployment ID bytes. That hash rule
comes from the existing reviewed `sim-testnet/evidence_deployment.go`; simulator
upgrade/fleet nonce reservations are not part of this mainnet graph. The
generated binding and artifact ABI must repack the same 96 constructor bytes.
`Deploy.s.sol` itself stops after vault binding and does not install this journal.

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
With `--action vault-link`, preview adds `vault_binding` and seven
`vault_binding_storage` words. Slot zero packs the coordinator in its low 20
bytes with `escrowRegistered` at byte offset 20; binding preserves that flag as
one. Reentrancy slot one and accounting slots eight through twelve stay zero.
With `--action evidence-create`, preview also adds `validator_evidence_address`,
`expected_evidence_runtime_hash`, the six derived `evidence_constructor` values,
and two `evidence_storage` mapping-root words. Earlier proxy and binding review
projections remain unchanged. The new journal is still unanchored.

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
native starting checkpoint and finite send window, exact approved RPC URL,
source-lock/cutover/custody-fence evidence hashes, custody identifier, artifact
file, journal directory, finite attempts, total value plus maximum gas liability,
and every prepared action's sender, nonce, target, calldata, value, gas and fees.
HTTPS requires normal certificate/hostname validation and the approved leaf
SPKI pin. Canonical lowercase public hostnames are supported, including
`https://archive.chain.opentensor.ai` with the default HTTPS port 443. Literal IP
routes retain their explicit-port requirement, and plaintext HTTP remains
restricted to that profile. No proxy, redirect, alternate endpoint or automatic
write retry is admitted. The URL and SPKI pin remain part of each signed plan;
an existing approval cannot be retargeted during recovery. Endpoint ownership
and independently approved finality are separate from this transport admission.
Legacy signed policy strings and `owned-rpc-assertion` evidence tags retain
their bytes; that label does not establish ownership of a public endpoint.

The bounded graph may contain the ordered prefix of one through nine actions.
One approval covers all supplied action reservations; no per-step confirmation
is invented. Actions zero through seven are executable in this candidate. Later actions
remain sealed reservations, not claims that their semantic builders or Safe
executor exist. Extending a prefix changes authority and cannot silently amend
an existing journal. A previously approved reserve-only prefix cannot gain vault
authority on reopen. Each prepared vault and implementation must be a zero-value
CREATE by the reserve deployer at exactly the next nonce. Later executors and any full-installation
attempt policy remain separate unfinished work.

For the eight executable actions, this candidate conservatively counts all
their submission attempts together against the original `maximum_attempts`.
Child resume cannot renew any predecessor's spent allowance. The existing limit of
eight remains unchanged; this is not an implemented nine-action send policy.
`maximum_total_wei` still bounds the value plus maximum gas liability of every
approved action. Every executable action's send admission, including the first
reserve CREATE, also requires enough pending balance for the selected action and
all later sealed reservations belonging to the same sender. Another sender's
reservation is not charged to the
deployer. This admission check does not lock funds on-chain or prevent recovery
of an already included original transaction. The
[installation-admission qualification](evidence/contract-installation-admission-qualification-20261002.md)
records the first-action funding and distinct-role corrections, causal controls,
and separate author/independent normal and race scopes.

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

After exact reserve recorder binding is retained, select the original vault call:

```sh
sn-mainnet bootstrap-contracts plan --action vault-link --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --action vault-link --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --action vault-link --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/vault-link.signed.bin --signed-transaction-hash "$VAULT_LINK_SIGNED_FILE_HASH"
```

After exact vault binding is retained, select the approved evidence CREATE:

```sh
sn-mainnet bootstrap-contracts plan --action evidence-create --config /secure/ur-mainnet/contract-phase.json
sn-mainnet bootstrap-contracts apply --action evidence-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH"
sn-mainnet bootstrap-contracts resume --action evidence-create --config /secure/ur-mainnet/contract-phase.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CONTRACT_PHASE_HASH" --signed-transaction /secure/ur-mainnet/evidence.signed.bin --signed-transaction-hash "$EVIDENCE_SIGNED_FILE_HASH"
```

The result distinguishes `signature-awaiting-import`, `signed-custody-complete`,
uncertain/pending chain work, `reserve-created`, `vault-created`,
`coordinator-created`, `escrow-registered`, `proxy-created-initialized`, `reserve-recorder-bound`, `vault-coordinator-bound`, `evidence-created-unanchored`, and a reverted action with its
nonce consumed. The escrow failure status is
`escrow-registration-reverted-nonce-consumed`; reserve binding failure is
`reserve-binding-reverted-nonce-consumed`; vault binding failure is
`vault-binding-reverted-nonce-consumed`. `installation_complete` and
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
`executable_action: "reserve-link"`; three actions remain after recorder binding.
Vault binding results also include `vault_binding` with
`executable_action: "vault-link"`; two actions remain. Evidence results retain
those projections and add `validator_evidence_address` and `evidence_constructor`
with `executable_action: "evidence-create"`; only the anchor remains. Creation
does not anchor the journal, authenticate published evidence, or complete
installation or activation. Failed evidence CREATE retains
`create-reverted-nonce-consumed`.
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

`vault-link.json` uses `urnetwork-mainnet-evm-vault-link-state-v1` and seals exact
completed reserve binding. All seven journals remain locked; six historical
receipts and their checkpoint ancestry are required through initial and
refreshed admission. Successful receipts add optional `vault_binding_hash`
and `coordinator_log_index`, committing the derived vault/proxy pair and event
position. Index zero is valid and included in the digest. Older receipt bytes,
hashes, marker authority and original approval remain unchanged.

`evidence-create.json` uses `urnetwork-mainnet-evm-evidence-state-v1` and seals the
exact completed vault-binding record. All eight journal locks remain held, all
seven historical receipts are reauthenticated, and their checkpoints fence both
current and refreshed admission. No new receipt field changes historical bytes.
The original maximum-eight attempt bound is unchanged: seven prior submissions
leave one evidence attempt. An uncertain durable attempt publication can consume
that last allowance without a transport write; restart preserves the liability
and does not invent an additional attempt. Later same-sender value/gas remains
reserved, even though the final anchor is unimplemented.

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

Vault binding has independent current canonical/pending one-shot admission:
exact vault runtime, all thirteen registered-but-unbound getters, and seven
storage words must agree before a send. An already fixed coordinator prevents
another call, even when it is the intended proxy. Missing reads remain unresolved.
Success requires original receipt sender/target, null CREATE address, original
transaction and fee bounds, and one exact `CoordinatorFixed(address indexed
coordinator)` event from the vault with the proxy topic, empty data and explicit
canonical transaction/block/index identity and `removed: false`.

At authenticated inclusion the same thirteen getters require coordinator equal
to the proxy, registration still true and all five accounting counters zero.
Raw storage independently requires the exact packed slot-zero word: eleven zero
bytes, one registration byte `01`, then the twenty coordinator bytes. The
reentrancy guard at slot one and `totalCaptured`, `totalPaid`, `pendingFunding`,
`outstandingLiability`, `escrowAccounted` at slots eight through twelve must all
remain zero. The setter writes only the coordinator member and emits its event;
it makes no external call and does not rewrite registration or accounting.

Current and inclusion observations also require the already-bound reserve's
exact runtime, six getters and two storage words, then the initialized proxy's
23 stable getters, five distinct namespaces and implementation runtime/storage
under the same block selector. Those checks remain separate from the six
historical receipts. Initial proxy policy still uses its original EVM height.
No live Safe ownership or authority is inferred by binding the coordinator.

Evidence admission independently requires an empty predicted CREATE address and
the exact bound vault, bound reserve, initialized proxy and implementation under
current canonical and pending selectors. Successful inclusion requires the
original transaction, exact receipt sender, null `to`, predicted CREATE address,
gas/fee bounds and an explicit empty log list. The reviewed constructor makes
static coordinator getter calls and emits no events. At that authenticated EVM
inclusion, the exact patched evidence runtime and all six immutable getters
must agree: coordinator, settlement vault, chain ID, netuid, genesis hash and
deployment ID hash. Mapping-root slots zero and one must be zero; these words
are not enumerable evidence about all hashed mapping entries.

The same inclusion rechecks bound vault/reserve state and initialized proxy/
implementation state, keeping the original proxy's EVM policy height and
requiring its `validatorEvidence()` getter still zero. Missing observations
remain read errors; an unmapped receipt remains nonfinal. The separate anchor
executor is not selected or implemented by evidence creation.

Each invocation scans at most 128 new native headers. Completed chunks are
durable; an interrupted current chunk is repeated, at most 128 headers. A failed
candidate read cannot advance past that candidate. Archive unavailability
remains a read error, not evidence of a successful-value mismatch. Healthy head
advancement refreshes current runtime/nonce/balance/collision admission within
the same bounded operation rather than requiring a stationary chain. Changed
ancestry, runtime or custody observations still stop the write.

## Remaining live trust and graph work

The offline [chain contract prerequisite phase](BOOTSTRAP-CHAIN.md#offline-contract-installation-prerequisites)
now exposes the complete approved prefix and eight-attempt/nine-action mismatch
before signing. It can inspect all retained action receipts without opening RPC
or changing original custody. A shorter approval and missing Safe anchor do not
force replay: the separately signed successor must adopt completed
receipts, reconcile unfinished signed nonces, and reserve only unfinished sends
plus an approved retry margin while conserving cumulative attempts and lifetime
financial exposure. The [successor execution owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md)
implements that exact adoption and cumulative custody, including
[live journal checks](evidence/bootstrap-successor-live-custody-20261001.md)
before sends and completion reports. Changing the original signed cap cannot
reinterpret its retained journals. Public submission remains closed by the Safe
authority gate described below.

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

The remaining graph is the separately authorized Safe `fixValidatorEvidence`
call. The [successor execution owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) already
adopts the eight original actions, retains their cumulative spend, and owns the
exact Safe digest/signatures/nonce plus the outer relayer's distinct nonce and
fee reservation. It has durable attempt intent, restart recovery, bounded reads,
canonical inner-success receipt checks and independent coordinator/evidence
readback. It cannot be added as a ninth send under the original eight-attempt
approval, and the existing `Deploy.s.sol` sequence does not install the anchor.

Public successor submission remains closed because the production complete Safe
history authenticator is absent. The configured owner and a successful outer
receipt cannot replace that capability. The optional
[native archive trace capture](SAFE-HISTORY-CAPTURE.md#optional-keyed-native-traces-and-parent-runtime-proofs)
adds actual SDK keyed traces and authenticated parent runtime bytes to the
existing full block census. Its explicit prefix-deletion, rollback, root and
inner-EVM completeness fields remain false: these archive APIs cannot prove the
unchanged full-history policy. A qualified node trace extension or independent
complete replay is still required. No current-storage-only policy, signature,
approval budget, original journal or public send authority changes here.

## Settlement v2 contract phase

`bootstrap-contracts` also runs the separate settlement v2 graph of five
actions, or six with the optional claim batcher (`VERSION2-FINAL.md` §8.5,
§10.2, step 2 of §12.1). One independent approval covers them all, sent by
one bootstrap deployer at consecutive nonces:

| Index | `--action` | Transaction | Journal | Completed status |
| --- | --- | --- | --- | --- |
| 0 | `vault2-create` | `STSettlementVault2(netuid, escrowHotkey, selfColdkey, bootstrap, demandFeeWei)` CREATE | `vault2-create.json` | `vault2-created` |
| 1 | `escrow2-register` | vault2 `registerEscrow(maximumBurnRao)` with `maximumBurnRao × 10^9` wei | `escrow2-register.json` | `escrow2-registered` |
| 2 | `settlement2-create` | `STSettlement2` implementation CREATE | `settlement2-create.json` | `settlement2-created` |
| 3 | `proxy2-create` | `ERC1967Proxy(implementation, initialize(owner, v1Coordinator, vault2, auditors, quorum))` CREATE | `proxy2-create.json` | `proxy2-created-initialized` |
| 4 | `vault2-link` | vault2 `setCoordinatorOnce(proxy)` | `vault2-link.json` | `vault2-coordinator-bound` |
| 5 | `batcher-create` | `STClaimBatcher` CREATE, optional | `batcher-create.json` | `batcher-created` |

The config schema is `urnetwork-mainnet-settlement-v2-contract-phase-config-v1`
and the plan schema `urnetwork-mainnet-settlement-v2-contract-phase-v1`. The
plan has the original fields without `reserve_hotkey` and exactly the first
five of these actions, or all six. Its `runtime_source_commit` must be the
source reviewed for its runtime version: a reviewed commit (v470, v473, v475,
v477 and v478, `crv4.ReviewedNativeOwnerSourceSpec`) names only the spec version
it declares. v475's commit with version 477, or v477's with 478, is refused, in
`preview` too, with
"contract phase runtime version differs from the one its source commit was
reviewed for". The frontier mapping source
(`67dcf7f791dc495064c293f080a0702cb433e51e`) declares no version and is still
accepted, so the approver checks that the config names the runtime's reviewed
commit. The original phase's recorded configs keep the pairs they were
approved with. The approval signs `urnetwork-mainnet-settlement-v2-contract-phase-approval-v1`,
a zero byte and the canonical plan JSON, and the plan hash uses the v2 plan
schema as its domain, so neither phase's approval or hash verifies the other's
plan. A v2 config selects only the v2 ids it approves and an original config
only the original ones. Signature encoding, key provisioning, `preview`/`plan`/`apply`/`resume`,
`--accept-plan-hash`, `--run-dir`, signed-byte import, `--online`, `--submit`,
the per-sender funding admission and the cumulative `maximum_attempts` (at most
eight across all its actions, the back-to-back escrow send included) are the
original phase's. `artifacts` pins a release file of schema
`urnetwork-settlement-v2-contract-release-artifacts-v1` holding exactly
`SettlementVault2`, `Settlement2`, `ERC1967Proxy` and `ClaimBatcher`, whether
or not the plan creates the batcher, checked like the original release. The same generator exports it from the reviewed catalog, whose
`SettlementV2ReleaseArtifacts` list stays apart from the original release:

```sh
go run ./sim-testnet/gencontracts --release-v2-json evm/out sim-testnet/contracts_gen.go /secure/ur-mainnet/settlement-v2-contracts.json
```

The catalog holds the final reviewed contracts: vault2 with the contract
review's fix and its gas reduction (12,815 runtime bytes), `STSettlement2`
(8,588) and the claim batcher (1,073), whose storage layout the generator
requires to stay empty.
After a full `forge build`, `go generate ./sim-testnet` rebuilds the catalog,
and `stabi/generate.sh` binds the three in one combined abigen pass and
exports their ABIs to `evm/abi/`. `go run ./sim-testnet/gencontracts --check
evm/out sim-testnet/contracts_gen.go` and `stabi/generate.sh --check` confirm
that the catalog, the bindings and the ABIs match that build.

**Gas.** Each action's gas limit is part of the approval. Size it from the
measurements in `scripts/runtime-rehearsal/README.md`: the runtime admits a
precompile dispatch only if the forwarded gas covers its declared weight, while
it charges the actual weight. There `registerLimit` used about 1.41M gas with
an `eth_estimateGas` limit of about 1.49M, and `registerPoolUid`, the closest
measured analogue of `registerEscrow`, used 1,449,743, so `escrow2-register`
needs a limit well above 1.5M. No other v2 action calls a precompile, and none
moves stake, so the 2.4M to 2.6M limits of capturing closes and claims do not
apply. Reasonable starting limits are 4,000,000 for `vault2-create`, 2,500,000
for `escrow2-register`, 3,000,000 for `settlement2-create`, 1,000,000 for
`proxy2-create`, 150,000 for `vault2-link` and 400,000 for the optional
`batcher-create`. With them R-1 (below) completed every action of the final
contracts, using 2,821,690, 1,463,833, 1,935,622, 310,486, 31,329 and 285,209
gas (2026-10-12; the figures vary by a few gas with the random addresses in
calldata, and the README records them with the R battery). `maximum_total_wei` covers every limit times its fee cap plus the
escrow funding.

**Builder checks.** Every selection, in `preview`, `plan` and every custody
operation, rebuilds the complete graph from the approval and the release, so a
fault in any action refuses even the first one, before any journal or route
opens. One sender sends them all at consecutive nonces, and the CREATEs carry
no value.

- Vault2 is the exact release creation and five constructor words. Netuid 25,
  `selfColdkey` and `bootstrap` are rebuilt: `selfColdkey` is the mapped
  account of the CREATE address predicted from the deployer nonce, as
  `evm/script/Deploy.s.sol:53-59,113-115` predicts it, and `bootstrap` is the
  deployer. The escrow hotkey and `demandFeeWei` are the approved inputs. The
  hotkey must be nonzero, and the fee a nonzero multiple of 10^9 wei, because
  the runtime truncates sub-rao value; the command checks nothing more and has
  no default fee. The approved fee is 750,000 rao, 750,000,000,000,000 wei:
  one answer's gas R-6 measured at depth 29, 149,074, at the 5 gwei planning
  fee, rounded up (VERSION2-FINAL.md §7.1).
  The words repack exactly through the release ABI, and the five immutables
  `netuid`, `escrowHotkey`, `selfColdkey`, `bootstrap` and `demandFeeWei` are
  patched into the exact release runtime.
- Escrow registration is the exact 36-byte `registerEscrow(uint64)` call to
  the derived vault2, with a nonzero cap and value `cap × 10^9` wei. Its
  `escrow_registration` review values are the original phase's.
- The implementation is the exact argument-free release creation; its UUPS
  `__self` immutable is its predicted address.
- The proxy is the exact release creation with `(implementation, initializer)`.
  The implementation is the derived one. The initializer's vault is the derived
  vault2, the owner and v1 coordinator are nonzero, the auditors number 1 to 16
  with no zero or repeated address and `1 <= quorum <= auditors`, which are the
  contract's own `setAuditors` bounds, and the deployer is neither the owner nor
  an auditor. Both ABI layers repack exactly. At launch the auditor set is the
  validator's admitted address with quorum 1. A configured owner does not prove
  live Safe authority.
- The binding is the exact 36-byte `setCoordinatorOnce(proxy)` call to vault2,
  with zero value.
- The optional claim batcher is the exact argument-free release creation, at
  the deployer's nonce after the binding. It has no constructor argument, no
  immutables and no state, and nothing else depends on it.

`plan` exports the decoded `vault2_constructor` (escrow hotkey and demand fee),
`escrow_registration`, `settlement2_initializer` and `vault_binding` of the
selected prefix. `preview` (schema
`urnetwork-mainnet-settlement-v2-contract-phase-preview-v1`) exports the
approval bytes and plan hash with, for vault2, the implementation and the
proxy, the predicted address, runtime hash and the exact completion readback:
every getter and storage word; and, when the plan creates it, the claim
batcher's predicted address and runtime hash (`claim_batcher`).

**Custody.** Each action has its own journal and schema
(`urnetwork-mainnet-evm-vault2-state-v1`, `…-escrow2-state-v1`,
`…-settlement2-state-v1`, `…-proxy2-state-v1`, `…-vault2-link-state-v1` and
`…-batcher-state-v1`), and each seals the exact completed record of the action
before it, while every earlier journal stays locked. Prepare a
`mainnet-evm-action` snapshot with each approved action's file name in the
approved run directory. An action opens only after each
predecessor's receipt is retained with its readback, and every online operation
reauthenticates them, as in the original chain.

**Receipts and readback.** A receipt is retained only after its readback at
the authenticated canonical inclusion:

- `vault2-create`: the exact runtime and the getters `netuid`, `escrowHotkey`,
  `selfColdkey`, `bootstrap`, `demandFeeWei`, `CHALLENGE_BLOCKS` (14,400),
  `coordinator` (zero), `totalCaptured`, `totalPaid` and `escrowAccounted`
  (zero) and `conservationHolds` (true), and the zero storage word of the
  reviewed layout that packs `totalCaptured` and `totalPaid` in slot one
  (`escrowAccounted` is their difference). That is every
  argument-free view except `liveEscrowStake`, which anyone can raise by
  transferring stake to vault2's escrow position (`conservationHolds`
  evaluates it through the staking precompile), and `escrowRegistered`: the
  back-to-back registration usually executes in vault2's own block, so the
  escrow flag is pinned from `escrow2-register` on.
- `escrow2-register`: the original escrow checks (one `EscrowRegistered` event
  and the `getUid`/`getHotkey`/`getColdkey` mapping), the vault2 getters with
  `escrowRegistered` true, slot zero packing `coordinator` in its low 20
  bytes with `escrowRegistered` at byte offset 20 and the reentrancy flag,
  zero, at offset 21, the zero word, and the escrow position's
  registration collateral, `getMinerCollateral(netuid, escrowHotkey,
  selfColdkey)` on the staking precompile `0x805`
  (`evm/src/interfaces/stakingCollateral.sol`). While SN25's
  collateral lock share is zero, registration locks nothing and the view must
  read `(false, 0, 0, 0, 0)`; any other value refuses the readback until a new
  approval reviews it. The receipt's `getter_hash` records it and the result
  reports it as `escrow_collateral`.
- `settlement2-create`: the implementation's `vault`, `v1Coordinator`,
  `auditors` (empty), `quorum`, `auditorSetVersion` and `owner`, all zero,
  `UPGRADE_INTERFACE_VERSION` (`5.0.0`) and `proxiableUUID` (the ERC1967
  implementation slot); its Initializable word is disabled at `uint64` maximum
  and its own ERC1967 implementation slot and linear slots zero to four are
  zero.
- `proxy2-create`: the original proxy receipt identity, then through the proxy
  `vault` (vault2), `v1Coordinator`, `auditors`, `quorum`, `auditorSetVersion`
  (1), `owner` and the interface version, and the storage words: the ERC1967
  implementation slot holds the implementation, admin and beacon are zero, the
  Initializable word is version 1, the Ownable word is the owner, and the
  reviewed linear layout (`evm/layout/STSettlement2.json`) holds the v1
  coordinator, vault2, the auditor count with each auditor from
  `keccak256(2)`, the quorum and version 1 in slots zero to four. The
  implementation's runtime and storage are read again at the same block.
- `vault2-link`: one `CoordinatorFixed` event, then the completion readback,
  all at the binding's canonical inclusion: vault2's runtime, getters and
  storage words with `coordinator` the proxy, `escrowRegistered` true and the
  escrow collateral, and the proxy's and the implementation's runtime, getters
  and storage words.
  Before its send, admission performs the same readback of the registered,
  unbound vault2, the proxy and the implementation at both the canonical head
  and pending state. A coordinator already fixed, even to the proxy, prevents
  the send.
- `batcher-create`: the exact runtime at the canonical inclusion, the whole
  readback of a contract without getters to check or state. Its journal opens
  only after the binding's completed record, and its admission, like every
  CREATE's, refuses an address that already has code.

`installation_complete` becomes true only with a retained successful
`vault2-link` receipt, and the claim batcher's retained receipt reports it
again; `activation_ready` stays false, because activation is the
Safe's `registerOperatorPool` (§12.1 step 5). The five journals' receipts are
the v2 deployment record. v1's launch skipped installation readback
(`LAUNCH.md:50-58`); the v2 phase cannot complete without it. The launch
readiness digest is unchanged: the phase changes no v1 contract, config or
journal.

**Back-to-back escrow registration (A7).** Vault2's constructor calldata
reveals the escrow hotkey as soon as its CREATE is broadcast. Anyone who
registers that hotkey first, under their own coldkey, makes `registerEscrow`
fail for good. So the registration is the deployer's very next nonce, and
vault2's journal can also hold the registration's original signed bytes.
Every `vault2-create` result, `apply`'s included, prints the registration's
envelope, the approved action 1, as `escrow_signing_digest` and
`escrow_unsigned_transaction` beside vault2's own `signing_digest` and
`unsigned_transaction`, so the deployer signs both before any send;
`escrow2-register`'s journal later prints the same bytes as its own. Import
the registration's signature with `--escrow-signed-transaction` and its pin
beside vault2's own bytes. `resume --action vault2-create --online --submit` then sends vault2's
transaction and, right after it in the same invocation, the registration's
bytes, each after its own durable attempt within the one allowance. The status
is `submitted-with-escrow-registration-awaiting-canonical-receipts`, or
`escrow-registration-allowance-exhausted` when no attempt remains. Vault2's
admission refuses both sends while the escrow hotkey already has a UID, at the
canonical head or in pending state. The chain usually includes both
transactions in one block, the registration right after vault2's creation;
vault2's readback at that block leaves the escrow flag to the registration's
own readback, so either inclusion completes. Once vault2 completes, open
`escrow2-register`'s journal and import exactly the same bytes, which its
journal adopts; any other signature is refused. Its online `resume` then finds
the registration's receipt and completes it with the readback. Without the
escrow bytes, `escrow2-register` is admitted and sent by its own journal after
vault2's finalized readback, with the same unregistered-hotkey check.

Two residuals remain. A registration of the hotkey that lands before ours
leaves `escrow-registration-reverted-nonce-consumed`; the call reverts and
returns its funding, and the recovery is a redeploy under a new approval with a
fresh escrow hotkey and fresh nonces. No funds are at risk: vault2 holds
nothing until a pool registers, and `registerPool` refuses until the escrow
is registered (contract review L1), so such a vault2 never holds a pool. And
the back-to-back send leaves the registration in flight before vault2's own
receipt is known: were vault2's creation to fail after admission, the
registration's funding would reach an address without code. The builder
admits only a valid constructor, so only an approved gas limit below vault2's
creation cost could cause that; size it from the rehearsal.

```sh
sn-mainnet bootstrap-contracts preview --action vault2-create --config /secure/ur-mainnet/settlement-v2.unsigned.json > /secure/ur-mainnet/settlement-v2.preview.json
sn-mainnet bootstrap-contracts plan --action vault2-create --config /secure/ur-mainnet/settlement-v2.json
sn-mainnet bootstrap-contracts apply --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH"
sn-mainnet bootstrap-contracts resume --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --signed-transaction /secure/ur-mainnet/vault2.signed.bin --signed-transaction-hash "$VAULT2_SIGNED_FILE_HASH" --escrow-signed-transaction /secure/ur-mainnet/escrow2.signed.bin --escrow-signed-transaction-hash "$ESCROW2_SIGNED_FILE_HASH"
sn-mainnet bootstrap-contracts resume --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --online --submit
sn-mainnet bootstrap-contracts resume --action vault2-create --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --online
sn-mainnet bootstrap-contracts apply --action escrow2-register --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH"
sn-mainnet bootstrap-contracts resume --action escrow2-register --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --signed-transaction /secure/ur-mainnet/escrow2.signed.bin --signed-transaction-hash "$ESCROW2_SIGNED_FILE_HASH"
sn-mainnet bootstrap-contracts resume --action escrow2-register --config /secure/ur-mainnet/settlement-v2.json --run-dir /secure/ur-mainnet/run-v2 --accept-plan-hash "$SETTLEMENT_V2_HASH" --online
```

Then repeat `apply`, signed-byte import, `resume --online --submit` and
`resume --online` for `settlement2-create`, `proxy2-create`, `vault2-link` and,
when approved, `batcher-create`, in that order, each after the previous
action's completed status.

**Runtime rehearsal (R-1).** `TestR1SettlementV2Phase`
(`evm_v2_rehearsal_test.go`, build tag `runtime_rehearsal`) drives exactly
these commands, in the order above, through the command dispatcher the binary
runs, against the rehearsal node of `scripts/runtime-rehearsal/README.md`:

```sh
RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
  go test -tags runtime_rehearsal ./mainnet/ -run TestR1SettlementV2Phase -v
```

It starts its own node and creates netuid 25, which the phase is pinned to,
with its emission started. It deploys the reviewed v1 coordinator
implementation as the initializer's v1 coordinator, which needs only code
there, and funds a fresh deployer. Its config carries the node's identity:
`system_chain`, the genesis hash, EVM chain 964, the reviewed source of the
node's runtime version (`850f2755bb05a3851f948489f0ab27b0645cebce` on a 478
node) with that version and
the BLAKE2b-256 of its `:code` and metadata, a finalized start block, and the
route `http://127.0.0.1:<port>` with no TLS pin. The approval covers the
release from `contracts_gen.go`, the starting gas limits above, a fee cap of
twice the node's gas price and the recommended demand fee. Each unsigned
transaction a command prints is signed outside it, the escrow registration's
from vault2's `apply`; vault2 and its escrow registration
go out back to back and are sealed into one block, and each later action is
sealed into its own. Every action completes only after its readback against
the real precompiles. The custody declaration is the synthetic one of every
`bootstrap-contracts` test; a production run instead prepares the approved
actions' journals with `storage-prepare` as `mainnet-evm-action` and passes the
binary's global `--durable-volumes` and `--durable-volumes-sha256`. Manual
sealing advances the chain one 250 ms slot per block, faster than real time,
and the node builds the pending block that admission reads at the wall-clock
slot, which Aura refuses unless it increases, so the test waits for real time
to pass the latest block before each online command. The whole run takes
about 80 seconds. On 2026-10-12 it passed on the final contracts in 86
seconds, with the escrow registration in vault2's block and no registration
collateral on the escrow position; with the claim batcher's creation added as
the sixth action, it passed in 72 seconds, and in 100 seconds on a loaded
machine against vault2 after its gas reduction, the batcher read back at its
own block. On 2026-10-13 it passed in 85 seconds signing only the envelopes
the commands printed, with the same gas.

## Settlement v2 activation

Phase A activates when the governance Safe sends STSettlement2's
`registerOperatorPool(noId, poolHotkeyV2, maximumBurnRao, E_s)`, with value
`maximumBurnRao × 10^9` wei, as a direct call, the way v1's `registerOperator`
went (`VERSION2-FINAL.md` §12.1 step 5, §14). `register-operator-pool` builds
and preflights that call and reads its result back; it never signs or sends.
Every flag is required. Its reads are `eth_chainId`, `eth_blockNumber`, then
each `eth_call` at that head block, and for readback the Safe transaction's
`eth_getTransactionReceipt`, read before the head:

```sh
sn-mainnet register-operator-pool review --rpc "$EVM_RPC" --settlement2 "$SETTLEMENT2_PROXY" \
  --safe "$GOVERNANCE_SAFE" --no-id 1 --pool-hotkey "$POOL_HOTKEY_V2" \
  --maximum-burn-rao "$MAXIMUM_BURN_RAO" --value-wei "$VALUE_WEI" --first-epoch "$E_S"
sn-mainnet register-operator-pool readback --rpc "$EVM_RPC" --settlement2 "$SETTLEMENT2_PROXY" \
  --no-id 1 --pool-hotkey "$POOL_HOTKEY_V2" --first-epoch "$E_S" --transaction-hash "$SAFE_TRANSACTION_HASH"
```

The hotkey is `0x` and its 32 bytes in hex, as in the phase config. `review`
refuses, before any read, a zero pool hotkey and a value other than
`maximumBurnRao × 10^9` wei; then a route that is not EVM chain 964, vault2's
own escrow hotkey (`escrowHotkey()` of STSettlement2's `vault()`), and a first
epoch whose start block, `epochStartBlock(E_s)` on STSettlement2's
`v1Coordinator()`, the head has reached. It then runs the exact call as an
`eth_call` from the Safe with its value and names the revert:
`InactiveOperator`, `InvalidEpoch`, `CadenceTooShort`, `NativeRefundFailed`,
vault2's `InvalidConfiguration`, `OwnableUnauthorizedAccount` (the Safe does
not own STSettlement2) or the v1 coordinator's `UnknownOperator`. A Safe that
holds less than the value is refused as `OutOfFund`, and any other failed
call with the node's own message and data. Its result, schema
`urnetwork-mainnet-settlement-v2-pool-registration-review-v1`, is the call
for review and Safe import, `to`, `value_wei`, `data`, `function` and `args`
(`noId`, `hotkey`, `maximumBurnRao`, `firstEpoch`), with `chain_id`, `safe`,
`vault2`, `escrow_hotkey`, `v1_coordinator`, `head_block`,
`first_epoch_start_block`, `blocks_remaining` and `preflight_uid`, the UID
the preflight registered at the head. The Safe sends exactly `to`,
`value_wei` and `data`.

`readback` names every difference from the reviewed registration: no receipt;
a reverted transaction; no `PoolRegistered` event of `noId` from vault2 in the
receipt, or one with another hotkey or first epoch; `pools(noId)` unregistered
or with another first epoch or hotkey; and `settlementAt(noId, E_s)` with a
vault other than vault2, another hotkey or a version other than 2. Its result,
schema `urnetwork-mainnet-settlement-v2-pool-registration-readback-v1`, names
`chain_id`, `settlement2`, `vault2`, `no_id`, `pool_hotkey`, `first_epoch`,
the event's `uid`, `transaction_hash`, `block_number` and `head_block`.

A call the chain ran and failed is never a failed read: no rerun changes it.
Subtensor's node answers a revert with code -32603, the message `VM Exception
while processing transaction: revert` and the revert's bytes as hex `data`;
the data is `0x` when the revert carries no reason, which is how the runtime
refuses the registration itself, for one a burn above `maximumBurnRao`. It
answers an EVM error the same way with empty data, as `evm error: OutOfFund`
for a sender short of the value. Other nodes answer a revert with code 3.
Either mode takes hex data, code 3, or a message that names a revert or an
EVM error as that evidence, in the preflight and in every view: `review`
refuses, and `readback` reports a difference, as for a `settlementAt` that
reverts `UnknownOperator` because `noId` has neither a pool nor a v1
operator. A view answered as its contract would not answer it, as an address
without the contract gives, is refused the same way.

| Exit | Meaning |
| --- | --- |
| 0 | The result is printed: the reviewed call, or the confirmed registration. |
| 1 | A read failed, which decides nothing: no answer, an HTTP error, a reply that is no JSON-RPC, an RPC error without evidence of a failed call, or an output that could not be written. Run it again. |
| 2 | Usage: a missing, unknown or malformed argument. Nothing was read. |
| 3 | `review` refuses, so the call must not be sent, or `readback` differs, so the chain does not hold the reviewed registration. Every revert and EVM error is here, named or not. |

`evm_v2_pool_test.go` (PR-1 to PR-6) runs both against the reviewed contracts
on the in-repo end-to-end chain, PR-6 with each shape of the node's answer.
R-4 (`rehearsal/r4_registration_test.go`) registers its pool through them on
the rehearsal node, and `TestR4RegistrationRefusals` drives the node's real
reverts, `OutOfFund` and a reverted view through them.
