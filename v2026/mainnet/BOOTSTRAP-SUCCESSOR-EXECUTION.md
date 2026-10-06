# Successor execution custody and owner

The successor has a separate signed execution domain, durable adoption and
nonce ownership, and a one-send execution state machine. Public commands can
preview, claim and recover that custody offline. Online resume additionally
uses a concrete canonical adapter and separately signed build, current-runtime,
signer-cutover and Safe deployment/storage-provenance authority. The original
complete-history submission route remains unavailable until its distinct history
authenticator is implemented and qualified. A separate [current-only route](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md)
requires a new independently signed v2 risk-policy acceptance and an exact revision
hash opt-in. No live execution or mainnet authority is claimed.
The separate [archive capture command](SAFE-HISTORY-CAPTURE.md) has
[sealed paired-source qualification](evidence/safe-history-census-qualification-20260930.md):
68 positive normal/race root executions and ten normal/five selected race causal
controls. It supplies complete bounded raw witnesses and explicit unresolved
internal-execution/history verdicts. It is not the required history authenticator
and leaves this public gate unchanged.
The corrected adapter's [scoped independent qualification](evidence/bootstrap-successor-canonical-qualification-20260930.md)
passes ten focused and twenty-two adjacent roots normal/race, fourteen normal
causal controls and six selected race controls on frozen `a7186754`.
The earlier [custody qualification](evidence/bootstrap-successor-execution-qualification-20260929.md)
passes twenty-one focused and six adjacent roots normal/race, with ten causal
control pairs. All tests use offline custody and explicitly synthetic canonical
responses; it does not qualify the new concrete adapter.

The concrete `bootstrapSuccessorExecutionChain` implementation reopens and
authenticates original full records through the existing owned native/EVM
adapter. RPC observations remain assertions of the independently approved owned
route, not independent proofs of consensus or distributed signer exclusivity.

## Independent execution approval

First retain the original eight successful receipts, the
[signed local preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md), and its
[exact offline Safe review](BOOTSTRAP-SUCCESSOR-SAFE-REVIEW.md). Independent
signing systems supply two Safe signatures and one signed relayer transaction.
These commands never read private keys or create a signature.

The private execution request uses schema
`urnetwork-mainnet-successor-execution-request-v1` and these fields:

| Field | Meaning |
| --- | --- |
| `safe_review_hash` | Exact reconstructed review seal. |
| `registry_directory` | Precreated private, dedicated nonce registry shared by every cooperating execution owner and signing system. |
| `owners` | Three distinct, sorted nonzero owner addresses. Current membership is still unverified. |
| `singleton` | Independently selected singleton address for the reviewed Safe release. Current code and storage remain unverified. |
| `safe_signatures` | `path` and `sha256` of exactly 130 binary signature bytes. |
| `relayer_transaction` | `path` and `sha256` of the canonical binary signed EIP-1559 transaction. |

This first profile permits two EIP-712 ECDSA signatures from the approved census,
threshold two, no modules, zero guard/module guard and zero fallback handler.
Contract signatures, approved-hash signatures, `eth_sign`, trailing signature
bytes, replacements and fee changes are unsupported. The outer envelope must
match every retained relayer field and the exact encoded `execTransaction` call.
Its chain, sender, target, nonce, value, gas, fee caps and empty access list are
authenticated by signature recovery. This admission is mathematical; it does not
prove current owner membership, relayer cutover, funding or permission to send.

```sh
sn-mainnet bootstrap-chain contract-successor-execution-preview \
  --config /private/chain.json --run-dir /private/original-custody \
  --accept-plan-hash sha256:ORIGINAL_V3_DIGEST \
  --request /private/successor-request.json \
  --safe-request /private/safe-review-request.json \
  --execution-request /private/execution-request.json
```

Preview reconstructs the original graph and completed preparation while holding
the five original shared marker locks. It verifies the pinned complete Safe
archive, imports the two pinned binary files and records both physical directory
identities. It emits `plan`, `execution_plan_hash` and hexadecimal
`execution_signing_bytes`. Those bytes are the ASCII domain
`urnetwork-mainnet-successor-execution-approval-v1`, NUL, then compact Go JSON of
the entire execution plan. This includes the exact eight adoption seals, original
attempts and reservations, proposed additive ceilings, retained review, separate
nonces, complete signed payloads, selected owners and registry identity.

The original config's independently pinned Ed25519 key approves the envelope
`{"schema":"urnetwork-mainnet-successor-execution-envelope-v1","plan":EXACT_PLAN_OBJECT,"signature_ed25519":"128_LOWERCASE_HEX"}`.
Neither original contract approval nor local preparation approval can substitute
for this domain. Importing signatures for preview grants no additional authority
to an external signing system; its own independent approval/custody rules apply.

Use `contract-successor-execution-claim` with all preview flags plus `--approval`,
`--approval-sha256` and `--accept-execution-hash`. Use
`contract-successor-execution-resume` with those exact inputs after interruption.
Exit 0 confirms the local command only. Exit 1 means unresolved custody, inputs,
cancellation or output; exit 2 means flags or execution approval are invalid;
exit 3 means the accepted execution hash differs. The original inputs, archive
and pinned signature files must remain available for every public reopen.

## Separate canonical authority and online resume

Before online resume, the same original independent Ed25519 approver signs a
new authorization object, in this field order:

| Field | Meaning |
| --- | --- |
| `schema` | `urnetwork-mainnet-successor-canonical-authorization-v1`. |
| `execution_plan_hash` | Exact accepted execution plan hash. |
| `safe_build_evidence` | Private `path` and `sha256` of independent Safe source/build review. |
| `current_runtime` | Independently reviewed `runtime_source_commit`, `runtime_version`, `runtime_code_hash` and `runtime_metadata_hash` for successor admission and inclusion. |
| `current_runtime_evidence` | Separate private `path` and `sha256` of current-runtime artifact and supported-codec review. |
| `signer_cutover_evidence` | Separate private `path` and `sha256` of every Safe and relayer signer's cutover, outstanding-signature inventory and retained original reservations. |
| `safe_deployment_provenance` | Separate private `path` and `sha256` of the independently signed exact Safe deployment and complete storage-history statement described below. |
| `accepted_policy` | Exact `bootstrapSuccessorCanonicalPolicy` string in `bootstrap_successor_canonical_authority.go`. |

The current runtime may differ from the original eight-action runtime. Its
source commit identifies the supported reviewed Frontier codec, while its
version/code/metadata tuple identifies the separately reviewed artifact. A node
cannot approve its own runtime. Original receipts still use their original
runtime authority; successor inclusion uses independently retained profiles at
inclusion and parent, so a later upgrade does not erase a counted historical outcome.

Signing bytes are the ASCII domain
`urnetwork-mainnet-successor-canonical-authorization-approval-v1`, NUL, then compact
Go JSON of that authorization object. The imported envelope is
`{"authorization":EXACT_AUTHORIZATION_OBJECT,"signature_ed25519":"128_LOWERCASE_HEX"}`.
All four evidence files must be nonempty, private, separately pinned and outside
the original custody and nonce-registry directories. The adapter rereads them;
their content remains an explicit independent attestation, not an automated
source-to-build proof or a distributed signer lock.

The additional provenance envelope contains `provenance` and
`signature_ed25519`. The statement uses schema
`urnetwork-mainnet-successor-safe-provenance-v1` and binds the exact execution
plan hash, Safe address, version, variant, singleton address, published proxy and
singleton runtime hashes, deployment transaction hash, reviewed native snapshot
number/hash, separately pinned private `history_evidence`, and the exact
`bootstrapSuccessorSafeProvenancePolicy` string. The original independent key
signs domain `urnetwork-mainnet-successor-safe-provenance-approval-v1`, NUL, then
compact Go JSON of the complete statement. Neither another Safe's signed report
nor an execution/canonical signature can replace it.

That signed statement is necessary review input; it does not prove deployment or
complete storage history. Safe owner and module getters follow sentinel lists,
while signature/module authorization also accepts nonzero mapping entries that
may be unreachable from those lists. Clean-looking getters cannot exclude
authority left by initialization or delegatecall storage writes. The distinct
`bootstrapSuccessorSafeProvenanceAuthenticator` must authenticate deployment,
initialization and every authority-relevant storage mutation through the current
finalized and scoped pending state, including unreachable entries. No production
implementation exists, and files or command flags cannot supply this capability.

Add `--online --canonical-approval /private/canonical-approval.json
--canonical-approval-sha256 sha256:CANONICAL_APPROVAL_DIGEST` to the exact
`contract-successor-execution-resume` command. Without `--submit`, online resume
authenticates the original receipts and reconciles the retained transaction.
Public `--submit` without the separate v2 current-policy opt-in returns exit 2 with
the missing-provenance-capability diagnostic before loading custody or reserving
an attempt, even when all original review files are present and signed. The
[v2 route](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md#explicit-public-v2-acceptance)
does not implement or infer complete-history authority.
Preview and claim remain offline; neither accepts these online flags. The route,
account, fees, signatures and transaction bytes always come from retained signed
inputs.

The immutable `contract-successor-execution.canonical-authorization` file binds
the authority before networking. Counted events retain its seal across restart.
A missing or changed counted authority cannot be recreated from another input.
The base authorization stays immutable. The
[qualified runtime revision path](BOOTSTRAP-SUCCESSOR-RUNTIME-REVISIONS.md)
lets online resume import one independently signed additive artifact with
`--runtime-revision` and `--runtime-revision-sha256`, preserving every prior
authorization, counted attempt, nonce claim and signed byte. This adds an explicit
review path; automatic runtime compatibility remains a separate P0. It supplies
neither a genuine Safe history authenticator nor public submission authority.

## Durable ownership and recovery

The original five shared locks precede an exclusive original-directory lock,
then an exclusive registry-directory lock. The original reserve marker already
fences all cooperating original action writers. Original markers and receipts
are never rewritten. One fixed `contract-successor-execution.claim` retains the
complete approval before any nonce or incremented allowance can be used.
Two immutable registry files independently fence `(chain, Safe, uint256 nonce)`
and `(chain, relayer, uint64 nonce)`. Each also binds the complete approval, signed
payload identities and original physical root. Changing one nonce cannot free
the other. A failed competing claim remains retained evidence.

The owner publishes an adoption event containing the original attempt floor and
the sum of completed original maximum envelopes, any original unexecuted
reservation, and the new outer maximum envelope. It then publishes a fixed ready
marker. Subsequent numbered events each have a full immutable `.intent` followed
by an identical `.json` record. Their seals bind the previous record and approval.
Eight original attempts plus two approved additions allow at most ten cumulative
attempts. Identical rebroadcasts keep one outer maximum liability because sender,
nonce and transaction bytes cannot change; each transport call consumes another
attempt. No receipt gas estimate or transport error reduces a reservation.

Descriptor-relative no-replace publication syncs a claimant's staged filename,
then full file contents, then the renamed directory entry. An empty synced stage
already reserves its claimant. Exact prefixes can resume under the same approval.
A partial attempt intent recovers as a consumed attempt, even if no send occurred.
A partial terminal outcome blocks sending until the exact canonical outcome can
finish it. A complete intent recovers its missing final record. Missing completed
adoption or nonce custody, changed bytes, unknown stages, sequence gaps, links,
extra hardlinks and nonprivate files refuse recovery. No files are deleted.

Live checkpoints also reread the execution claim and ready marker, walk every
exact intent/record pair back from the owner's retained seal to original adoption,
and reject unexpected event or stage files. An interrupted terminal stage keeps
its exact observed byte hash during ownership. Missing or changed custody stops
the owner before a send or installation result; a healthy chain observation cannot
substitute for the counted journal. These checks are read-only. Exact intent
recovery remains an explicit reopen operation and never renews an attempt.
The [custody regression evidence](evidence/bootstrap-successor-live-custody-20261001.md)
records the pre-fix send/result failures and scoped validation.

Copied or replaced root/registry inodes cannot inherit the approval. These are
cooperating local-filesystem fences, not cross-host, unerasable or anti-rollback
custody. Signer enforcement of the single approved registry remains a production
gate. An operator with power to rewrite or delete all custody can destroy that
evidence. A new root, restore, different registry, replacement transaction, fee
increase or later successor requires a separately implemented and qualified
migration preserving every previous seal and liability; this schema cannot do it.

## Canonical execution machinery

The owner authenticates the exact ordered eight original full-record seals
through the canonical adapter, then reconciles the exact retained outer
transaction. Pending and unavailable results cannot permit another send.
Reconciliation precedes expiry and attempt checks, so exhausted or expired
custody can still retain a previously counted canonical success. An inclusion
without a preceding counted local attempt is refused; independently proved
external-send adoption/disposition remains unsupported.

Before a send, current finalized and scoped pending Safe proxy/singleton code,
selected owners, threshold, modules, both guards, fallback, inner nonce,
relayer confirmed/pending nonces, funding, coordinator owner and unbound evidence
slot must match. Evidence runtime and immutable getter digest must equal the
adopted original CREATE receipt. Funding also preserves original unexecuted
reservations belonging to the same relayer. After durably reserving an attempt,
the owner repeats current-state admission and checks retained execution/nonce
custody before at most one exact transport write. Every ambiguous outcome keeps
the same signatures, counters and financial liability. A pinned finalized hash
keeps the snapshot consistent while later heads advance normally; canonical
ancestry, current runtime and native validity are checked at the end. The adapter
does not wait for head equality. Each of the eight original receipt operations
gets its own approved retry budget, and direct current-state RPC reads receive
separate bounded retries under caller cancellation.

The separate [qualified readmission increment](evidence/bootstrap-successor-readmission-qualification-20260930.md)
at `cd4261a8` moves expensive history verification before pending observations
and the final canonical/runtime/window checks. Every new observation invalidates
earlier admission, including a refresh canceled at its initial checkpoint.
Both heavy roots and three selected adjacent roots pass normal/race, with both
causal controls reproducing the intended failure in both modes. Exact source and
dependency evidence is sealed; the production history capability and public
submission remain unavailable.

`eth_getTransactionByHash` looks up only the exact retained hash. A null response
is an owned-node assertion of that hash's absence. Confirmed/pending relayer
nonces and Safe storage cover this execution's account state. No
`txpool_content` or `author_pendingExtrinsics` census is required. Off-node Safe
signatures, another relayer's future Safe operation, and global signer fencing
are covered by the explicit independent cutover attestation; these reads cannot
prove their absence.

Completion requires canonical native/EVM inclusion of that exact outer
transaction, the exact Safe inner-success event/digest and independently read
coordinator/evidence runtime/domain binding at inclusion. Outer status one alone
is insufficient. Outer revert is terminal for that outer transaction and keeps
the potentially live inner signature fenced. Installation never implies native
economy activation, either UR validator or the root role. Local resume reports
canonical authority unresolved even for a retained terminal event.

Ten new test roots cover canonical authorization, authority recovery, signed
provenance scope, real orphan owner/module mappings, actual pinned Safe getters,
exact pending lookup, strict receipt parsing and two full local v3/native/EVM
fixtures. Those heavy fixtures explicitly inject a synthetic history capability
into the internal command implementation and execute the reviewed Safe
proxy/singleton and coordinator, including an uncertain send and restart. They
do not enable public submission or qualify an arbitrary deployed Safe's history.
Their sealed normal/race and causal evidence is recorded in the
[canonical qualification receipt](evidence/bootstrap-successor-canonical-qualification-20260930.md).

Live mainnet genesis/runtime, actual Safe/custody selection, independent build
and runtime review, signer cutover, owner/relayer signatures and funding remain
operator-provided authority. This implementation establishes no live installation,
validator activation or native 10/90 acceptance.
