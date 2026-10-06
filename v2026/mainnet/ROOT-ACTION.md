# Existing-seat root action owner

The root role now has an offline-qualified **single-action ownership core** in
[root_action.go](root_action.go), a private durable store in
[root_action_store.go](root_action_store.go), and a bounded root basket-call
encoder in [root_signing.go](root_signing.go). The read-only production chain port
in [root_receipt_chain.go](root_receipt_chain.go) now reconciles finalized native
receipts. The [offline custody handoff](ROOT-OFFLINE-CUSTODY.md) now verifies an
independently approved packet and durably imports/replays its exact public native
signature. The [service decision owner](ROOT-SERVICE.md) now atomically couples
an approved weight decision to this original intent and supplies bounded joined
supervision with independent signer/submission ports. The
[owned submission adapter](ROOT-SUBMISSION.md) now implements separately approved
native HTTP submission, one durable attempt identity and original-byte canonical
reconciliation. There is no production native signer or current-authority adapter,
`root-service` command or active root weight publisher. This increment does not
claim mainnet readiness.

The current proposed strategy remains `accumulate_in_place`: a retained root
seat requires **no periodic native transaction** for that strategy. The observer
continues to monitor membership and custody. There is no manufactured heartbeat,
automatic root registration, staking, re-registration, delegation, claim or
take change. `set_root_weights` changes the basket strategy and requires its own
explicit approval and complete eligibility qualification. The user's subnet
90% owner-recycle decision supplies no root basket or custody authority.

## Implemented boundary

One action artifact has `schema: urnetwork-mainnet-root-action-v1` and binds:

- The separate `bittensor-root-validator` role, netuid 0, native chain,
  independently approved nonzero genesis and EVM chain ID 964. ID 945 is rejected.
- Full runtime version, `:code` and raw metadata hashes, plus the inspected
  source profile `67dcf7f791dc495064c293f080a0702cb433e51e`.
- Exact hotkey, coldkey, existing UID and registration block. No observation or
  hardcoded slot is treated as proof that an approved seat exists.
- Independent policy and approval artifact hashes, custody ID, canonical private
  state-file path, and the separately selected `explicit_root_weights` strategy.
- Sorted unique destination netuids, positive u16 weights, one nonce, a finalized
  mortal anchor, a period of 4–256 blocks, one finite approval expiry, one local
  fee reservation, and at most eight same-byte broadcast attempts.

The profile checks metadata14/extrinsic4, the selected AccountId32 and sr25519
variants, the complete signed-extension order and consumed wire shapes, and the
root call's two `Vec<u16>` parameters. The inspected payment wrapper's `metadata()`
returns the inner **`ChargeTransactionPayment`** identifier and shape; expecting
`ChargeTransactionPaymentWrapper` was a codec bug. Unknown or reordered
extensions are refused. The V1 codec's source pin is separate from the observer's
profile so an observer upgrade cannot silently invalidate historical actions. No UR scoring vector can be sent
through this core without a separately approved root action artifact.

Every action is one-shot for its entire lifetime. Dispatch failure, expiry or
an exhausted broadcast count does not reset the signature/count allowance. A
future action needs a new approved artifact and authoritative hotkey/nonce
ownership reconciliation. Automatically opening another state directory is not
a legitimate way to obtain another allowance.

The encoder uses a mortal Substrate era and zero tip. All allowed periods use
unit quantization, so the supplied finalized anchor is the exact era birth.
Payloads longer than 256 bytes use Substrate's Blake2b-256 rule before sr25519
verification. The core verifies the returned public signature and builds only
the exact retained call, nonce, era and signer bytes. Existing immortal UR/native
helpers remain separate; their recovery assumptions are unchanged.

The local policy/runtime hashes bind the request identity and admission checks.
They are **not additional fields in the native signed payload**. The protocol
signature binds genesis, spec/transaction versions and mortal checkpoint; a
same-version code replacement is not cryptographically excluded by that payload.
The metadata-hash extension is explicitly disabled in this profile. Production
must assess that residual runtime-upgrade exposure rather than claim that a
local exact-code check is an on-chain lock.

## Durable transitions and restart behavior

| Retained phase | Permitted next work | Retained obligation |
| --- | --- | --- |
| `reserved` | Authenticate current chain, generation, nonce and complete independent authority; persist `signing` before custody | One exact action and its local fee reservation |
| `signing` | Recover custody's original signature; an authoritative never-issued response may re-enter current admission before `signOnce` | Same request ID, nonce and allowance; no blind replacement signature |
| `signed` | Reconcile complete finalized history before any submission | Exact signed bytes and transaction hash |
| `pending` | Reconcile first, then optionally retry identical bytes within count/era bounds | Ambiguous sends keep ownership and reservation |
| `finalized`, `dispatch-failed`, `fee-overrun`, `runtime-deviation` | Retain exact receipt, dispatch, execution-runtime and actual-fee evidence | Completed action consumes this one-shot artifact permanently |
| `expired` | Retain complete finalized absence through the mortal window and unchanged account nonce | No implicit new action or signature allowance |

Each `step` makes at most one custody signing/recovery operation or submission;
there is no internal unbounded sender loop. Reconciliation precedes every
broadcast. Its caller owns the finite retry schedule and context. RPC timeouts,
dropped/usurped pool notifications and lost acknowledgements are not terminal
dispatch outcomes. The production chain port must use bounded read retry of at
least 60 seconds, normally 300 seconds, and join its workers on cancellation.

If custody reports an unknown outcome, the action stays pending. Only a fenced,
authoritative custody response that this request has **never** issued a signature
may authorize another `signOnce` invocation, after fresh domain/eligibility checks.
A generic not-found, missing file or timeout is not that proof. The custody
service must itself be idempotent for the request ID, including concurrent or
lost-response calls, and must retain its signature receipt before replying.

Signed intent and broadcast count are saved before their external effect. A
durability error poisons the open owner: it cannot perform another side effect
until reopen determines which complete record survived. A post-rename error may
already be durable and is never treated as a safe rollback. A crash after a
broadcast-intent write may consume an attempt without sending; that conservative
count does not create a new signature or lose subsequent receipt reconciliation.

A runtime/seat/nonce change blocks new signing and broadcast but leaves old
receipt reconciliation available. A genuine receipt from before an upgrade is
retained under its original execution runtime. A transaction executed under a
different code, metadata or full runtime version is stored as `runtime-deviation`
with its actual result and fee; the observer cannot erase that financial event.
A later unapproved current runtime is recorded with `state_unavailable: true`:
no signing or absence expiry can use its unqualified nonce/seat layout, while an
older receipt remains readable under its independently approved execution
profile. A later reaped account and reset nonce likewise cannot erase exact
finalized inclusion. Only absence expiry needs a qualified unchanged nonce.

Expiry requires finalized coverage from birth+1 through death−1, revalidation
of the original anchor, a finalized head at or beyond death, and an unchanged
hotkey nonce. A wall clock, best head, missing subscription notification or
unexplained consumed nonce cannot release the reservation. If the signature
itself remains unavailable from custody, this increment conservatively retains
the unresolved action; automated unsigned-intent retirement is not implemented.

The store requires a precreated private directory without symlink traversal,
private regular files, an exclusive process lock, strict bounded JSON and content
hash checks. An immutable local marker binds the original request. Explicit
creation refuses an existing marker even when the record is missing or empty.
Atomic writes sync the file, rename it and sync its containing directory. No
completed or partial record is interpreted as an empty unused allowance.

This is local ownership, not distributed custody security. A checksum can be
recomputed; a hostile host can replace both marker and state or restore an old
directory. Custody must durably fence the whole hotkey across hosts, enforce
request/count limits independently and reject rollback/replayed authorization.
The local store is not a hardware signer or an independently authenticated
transaction log.

## Read-only canonical receipt port

Construction requires an independently supplied native chain, nonzero genesis,
EVM chain ID 964, the owned HTTP(S) route and one to eight exact runtime/source,
code and metadata profiles. No defaults are learned from Snow; ID 945, unknown
source profiles and ambiguous runtime/code entries are rejected. `submit`
unconditionally returns a disabled error. The shared RPC reader also refuses
methods outside its explicit read-only list before HTTP delivery.

One reconciliation verifies the native SCALE header hash and every parent link
from the owned RPC's finalized head back to the exact retained mortal anchor.
It rejects missing fields, height gaps, changed canonical head hashes and wrong
network identity, and repeats the network check before returning evidence.
Modern `RuntimeEnvironmentUpdated` digest8 is decoded directly because the
pinned GSRPC library omits it. Header digest vectors are bounded before decoding.

Every block body in birth+1 through min(finalized head, death−1) must reproduce
the authenticated header's ordered extrinsics trie root before inclusion or
absence is reported. The two known layouts are independently qualified against
Rust `sp-trie` from SDK `cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`, locked by
the inspected Subtensor source. Checking the committed root does not require
interpreting intervening unknown runtimes. An actual matching receipt still
requires its independently approved **parent-state execution runtime**, not the
new code/metadata installed by that block. The admitted source uses
`systemVersion/stateVersion = 1`: state trie layout1 but extrinsics trie layout0.

The port independently verifies the retained sr25519 signature and all signed
fields, then requires exact full extrinsic bytes and index, one phase-matched
System success/failure, one native-u64 `TransactionFeePaid` from the direct hotkey
with zero tip, and the exact seat's `RootWeightsSet` for success. A failure keeps
its raw SCALE dispatch error and actual fee. Another transaction's event cannot
supply the outcome. Duplicate inclusion, duplicate terminal/fee events, absent
fee storage, trailing SCALE and oversized lengths are rejected. `event_hash`
is Blake2b-256 of the complete raw canonical SCALE event-storage bundle.

Qualified current state uses metadata-checked **56-byte** Subtensor
`System.Account` rows, including u64 balances; generic u128-balance account
layouts are rejected. Root UID, reverse hotkey, owner and registration generation
are checked at that same finalized hash. Explicit null account storage means
nonce zero; a failed or malformed read never means absence. Successful root
weight/last-update post-state is retained separately. A readback gap is an
explicit `post_state.issue` and does not erase a verified financial receipt.
Readback is the final state of the entire block and may reflect a later call;
it is not claimed to be exclusively caused by this transaction.
The signed root call does not include a registration-generation argument.
`RootWeightsSet` identifies a UID, while the finalized census identifies its
current generation; neither alone proves the generation at an earlier
intra-block execution point. A contradictory UID requires receipt review.
Production custody must prevent administrative seat changes while an action is
pending or provide separately authenticated incident reconciliation.

Each read has an explicit 60–900 second retry budget (normally select 300).
Individual attempts can use up to 60 seconds, avoiding the prior 15-second cut
off for a read that may legitimately take longer. Transport/overload errors and recognized server read-timeout replies retry;
archive pruning, integrity failures and unknown application errors remain
visible. Reconciliation has a 15-minute total budget and cancellation-aware
serialization, no detached workers and a 4,096-header ancestry limit. Bodies and
events are bounded to 10 MiB and 65,536 entries; SCALE traversal is work/depth
bounded and unsupported shapes fail explicitly. Failed authentication is never
cached. Runtime metadata is retained only under the full approved tuple.
Restart re-reads complete history rather than inventing a checkpoint or renewed
signing allowance. An offline gap beyond the ancestry limit requires separately
reviewed archive recovery; it cannot become automatic expiry.

**Trust boundary:** the independently approved owned RPC is the authority for
finality and pinned storage/runtime replies. Header/body commitments and ancestry
are recomputed, but this port does not verify GRANDPA justifications, storage
trie proofs or a source-to-Wasm build attestation. A compromised owned node could
lie about finalized history or state. Choosing stronger independent consensus/
storage authentication is an outstanding deployment decision, not a property
of this implementation. No live RPC or chain mutation was used to qualify it.

## Production adapter contracts and remaining gates

The core now has a production **read-only reconciliation** implementation and
an offline public-signature handoff. Live authority, globally fenced native
signing and submission have no production implementation. They cannot
be activated by a policy boolean, a successful `root-preview`, or a testnet
allowance. Before adding a signing CLI/service, supply and qualify:

1. **Independent mainnet authority.** Approved genesis, owned route, exact
   metadata/code/full runtime and source-to-Wasm provenance; approved existing
   root hotkey/coldkey, registration receipt/generation, policy, operator and
   custody identity, finite mainnet limits, signer role and state ownership.
   The Snow observation must not approve its own genesis. No actual mainnet pins
   or keys are included here.
2. **Action and eligibility authority.** An explicitly selected root basket
   strategy and full effective eligibility at a finalized view: current seat,
   root enablement, rate limit, owner exception, inherited parent/child stake,
   fixed-point TAO weighting, live destinations, minimum diversity and
   concentration. Raw stake and the observer's `read_only_ready` are insufficient.
   Preserve existing basket/delegation and all-staker rights before an economic
   transition; current-network census does not settle retired-network history.
3. **Protected custody and cost.** Approve a globally fenced hotkey signing
   service with durable idempotent receipts, one nonce lane, rollback protection,
   bounded request/count/expiry and recovery credentials. Online observers do not
   receive coldkeys. The local `fee_reserve_rao` is accounting only: native zero
   tip and a fee quote are not a hard maximum-fee argument. Qualify the actual
   payer, failure fees and enforceable exposure mechanism or obtain explicit
   approval of the bounded residual fee exposure. Do not label this implementation
   max-fee protected. Fresh registration remains separately blocked because the
   inspected root-registration call has no maximum-burn argument.
4. **Chain trust and receipt activation.** Qualify the implemented port against
   approved mainnet archive fixtures and exact runtime artifacts; approve the
   owned-RPC trust boundary or supply independent consensus/storage verification.
   Connect globally fenced pending-nonce custody and the separately bounded
   submission adapter. Resolve post-state issues and define archive recovery for
   gaps beyond 4,096 headers. Unknown receipt execution runtimes still require
   independent profile review before event/fee interpretation. EVM receipts and
   UR validator evidence do not replace native root receipts.
5. **Supervisor and operations.** A finite action queue with its own globally
   reserved limits, bounded cadence/backoff, cancellation/join, alerts and named
   on-call ownership. Reconcile old actions when admission changes; never loop
   around a blocked action by changing directories, nonce, era or approval hash.
   Rehearse actual signer crash/host failover, lost responses, same-version code
   change and archive loss using the exact release before activation.

The tested core is a concrete step toward running the separately requested root
role. MG-08 remains blocked on these production adapters and approvals. UR
validators, root membership and substrate administrative Root origin remain
three distinct authorities.

## Qualification scope

`root_action_test.go` uses real generated test-only sr25519 keys, a public metadata
fixture retaining its real inner payment-extension metadata, private
disposable files, deterministic durability failures and in-memory authority/chain
ports. Tests cover signature/domain replay, mortality, extension/shape drift,
missing authority, lost signer/send responses, both sides of durable writes,
exact-byte retry, fee/dispatch/runtime deviations, expiry gaps, unexpected nonce
consumption, stale seat generation and runtime, finality rollback, local lock
ownership and missing/empty/rehashed state. `root_receipt_test.go` adds synthetic
HTTP archive fixtures for canonical inclusion, full mortal expiry, parent-runtime
upgrade decoding, reaped accounts, unknown-current-runtime continuation,
code deviations, exact signature correspondence, body/header corruption,
duplicate inclusion, cancellation, retry/restart, wrong-chain/profile rejection,
post-state gaps and independent Rust trie vectors. There is no live RPC or
transaction.

Run normal/race/vet qualification with the composed workspace source lock:

```
go test ./mainnet -count=1
go test -race ./mainnet -count=1
go vet ./mainnet
```

Source semantics inspected at the pinned commit: `runtime/src/lib.rs`
(`SystemTxExtension`, `CustomTxExtension`, `TxExtension`),
`runtime/src/check_mortality.rs`, `runtime/src/check_nonce.rs`,
`runtime/src/transaction_payment_wrapper.rs`, `runtime/src/fee_filters.rs`, and
`pallets/subtensor/src/macros/dispatches.rs` (`set_root_weights`). Their local
inspection is not evidence that current mainnet runs those bytes.
