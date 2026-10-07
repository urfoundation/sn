# Offline contract successor proposal

`bootstrap-chain contract-successor-plan` prepares unsigned review material for
the one remaining evidence anchor after all eight original actions have retained
successful receipts. It reads the accepted v3 preparation and original journals
under shared locks. It preserves the original approval, signed transaction hashes,
receipt facts, predecessor seals, spent attempts and budget ceilings. It creates
no successor journal, approval signing payload, Safe digest, transaction or key.

```sh
sn-mainnet bootstrap-chain contract-successor-plan \
  --config /private/chain.json --run-dir /private/custody \
  --accept-plan-hash sha256:ORIGINAL_V3_DIGEST \
  --request /private/successor-request.json
```

The private, regular request file is strictly decoded, limited to 16 KiB and
uses schema `urnetwork-mainnet-contract-successor-request-v1`. Its fields are:

| Field | Meaning |
| --- | --- |
| `successor_id` | A bounded review identifier distinct from the original custody ID. |
| `bootstrap_plan_hash` | Exact original accepted v3 preparation hash. |
| `original_contract_plan_hash` | Exact original signed contract graph hash. |
| `additional_maximum_attempts` | Proposed increment of 1 through 16 attempts. |
| `additional_maximum_wei` | Positive canonical uint256 increment to the original lifetime ceiling. |
| `intended_owner_safe` | Proposed Safe identity, exactly equal to the original proxy initializer's owner. This comparison does not establish that the address is a Safe. |
| `intended_safe_nonce` | Explicit canonical uint256 Safe nonce intention, awaiting live authentication. |
| `intended_relayer` | Nonzero identity distinct from the declared Safe and all original contract addresses. |
| `intended_relayer_nonce` | Explicit uint64 outer nonce, distinct from every consumed or reserved original sender/nonce pair. |

The output schema is `urnetwork-mainnet-contract-successor-proposal-v1`, with
status `unsigned-proposal-prerequisites-unresolved`. The content hash seals review
material only. All approval, signing-payload, executable, current-chain,
Safe-authority, network-effect, installation and activation flags remain false.
The request digest, original config hash, custody ID/directory and eight complete
action/receipt seals identify proposed adoption. The derived coordinator calldata
is the exact one-shot `fixValidatorEvidence` call for the original evidence CREATE
address, with zero value; it is not encoded as a Safe transaction.

With eight completed attempts and two proposed additional attempts, the cumulative
ceiling is ten, remaining allowance is two, unfinished action count is one and
retry margin is one. The original eight sends are not repeated. These counts
reserve capacity for unfinished work; they do not require a new broadcast when
canonical reconciliation can establish that an existing signature already landed.
The lifetime ceiling is the original approved ceiling **plus** the requested
increment, checked for uint256 overflow. Completed envelope reservations remain
conservative liabilities instead of being replaced by receipt gas estimates.
Any original ninth reservation is copied verbatim, including its separate nonce,
calldata and maximum value/gas liability. Its disposition remains unresolved.

Missing original preparation, missing or partial action claims, an incomplete
signed action, changed lineage or invalid completion refuses a proposal. Original
owners must reconcile that work. Repeated proposals can revise the *requested*
increment without changing the original signed config, counters or receipts.
Exit 3 means a complete unsigned proposal with unresolved prerequisites; exit 1
means unresolved custody, cancellation or output; exit 2 means invalid inputs or
flags. A different accepted preparation/run directory exits 3 without a proposal.
There is no RPC, signature import, apply, resume, submission or service option.

## Required signed successor and custody migration

The separate [signed local preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md)
now has [independent scoped qualification](evidence/bootstrap-successor-preparation-qualification-20260929.md).
It adds a distinct approval domain and one fixed durable local claim, locally
authenticates the original eight receipt records, preserves additive floors and
resumes interrupted publication. Current canonical reauthentication remains a
separate gate. It grants no execution allowance or Safe authority. The unsigned
proposal command and original v1 approval/markers retain their existing scope.

The [successor execution owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) now implements
independent approval, durable exact-prefix adoption, separate nonce claims and
one-send reconciliation machinery, with [scoped independent qualification](evidence/bootstrap-successor-execution-qualification-20260929.md).
Its new concrete canonical adapter awaits independent qualification. Increasing the v1 contract
schema's eight-attempt cap cannot migrate custody: every existing marker binds
the full signed config and each descendant binds its original predecessor record.
Replacing that config invalidates those seals; it cannot adopt completed work.

The next [offline Safe review bridge](BOOTSTRAP-SUCCESSOR-SAFE-REVIEW.md) now
reconstructs that exact completed preparation under shared ownership, verifies
the selected complete published Safe release and derives the retained anchor's
zero-value CALL digest. Its bounded relayer intention preserves completed and
unexecuted original liabilities. It creates no new approval, signature, nonce
reservation, outer transaction or executable allowance. Canonical state and the
separate globally fenced execution owner remain unresolved.

The execution implementation uses a new versioned successor plan and independently
signed approval domain, distinct from both the original contract approval and
this unsigned request/proposal schema. The approval must cover:

1. Original bootstrap, phase plan and config hashes; original approval identity,
   custody ID/directory; each adopted action's exact journal/full-record seal,
   original signature/transaction hash, canonical receipt and predecessor relation.
   Adoption copies immutable references, without signing or executing them again.
2. Every outstanding original envelope and nonce, including any ninth reservation,
   with canonical reconciliation or independently authorized disposition. A new
   signature, expiry or transport error must not release an earlier liability.
3. The full selected Safe release/source/runtime/storage profile, exact inner
   operation/digest/nonce and owner signatures, approved outer relayer envelope,
   fee/value ceilings, validity window, and separate globally fenced custody.
4. Absolute cumulative attempt and lifetime ceilings plus explicit increments.
   Original attempts and maximum unresolved liabilities are immutable floors.
   A later successor must cover every earlier successor seal and spend as well.

The new journal owner acquires original locks in their existing order,
validate exact completed custody, and claim a separate successor destination
without rewriting original markers. Its durable creation state must bind the
approved adoption list before admitting any signature or incremented allowance.
Interrupted claims resume under that same successor approval; absence or partial
publication must never cause another owner to create a fresh budget. Original
receipt reauthentication precedes online eligibility. The owner durably reserves
attempt/financial capacity before each send, preserves ambiguous send liability,
and reconciles Safe-inner and relayer-outer nonces separately. Canonical completion
must establish Safe inner success and exact coordinator binding before marking
installation complete. These transitions require their own causal qualification.

## Authority and provenance prerequisites

The deployment catalog covers ReserveSink, SettlementVault, Coordinator,
ERC1967Proxy and ValidatorEvidence. The separate [qualified offline Safe release verifier](SAFE-RELEASE-VERIFY.md)
now pins explicit 1.4.1/1.5.0 Safe/SafeL2 singleton/proxy artifacts, ABI,
published source/compiler inputs and storage layout. It establishes no current
account authority, independent rebuild, transaction-digest implementation or
separate Safe custody owner. The simulator's direct owner anchor call cannot
supply those production authorities. `fixValidatorEvidence`
is `onlyOwner`, accepts code at a one-time address and does not itself establish
the evidence immutable genesis/deployment/coordinator domain.

Before an executable successor can be approved, complete independent release/build
review of the selected pinned profile, then authenticate the current singleton,
owners/threshold, modules, guard, fallback handler, nonce and pending transaction
state at the selected finalized mainnet point. Admit exact Safe operation and
signature semantics under that profile, and independently fence the relayer's
nonce, signer and funding. Authenticate the evidence runtime and immutable domain,
and reauthenticate all eight historical receipts through the canonical adapter.
An outer EVM status of one does not prove Safe-inner execution success, the expected
event, the one-shot coordinator getter or current authority. Neither an address
label nor a local artifact hash can replace these prerequisites.

This bounded source increment has [scoped Sol qualification](evidence/bootstrap-contract-successor-qualification-20260929.md):
six new and twelve inherited roots pass normal/race, with six causal controls in
both modes. The [separate successful full-v3 public-command fixture](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md)
passes normal/race plus six intended causal executions on `c294fefd`.
The broad prerequisite battery remains partial at 150/270 roots in both modes,
with 120 unrun. It does not install contracts, approve more spending, run the
10/90 native economy or activate either UR validator or the separate root role.
