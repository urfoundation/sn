# Independently approved root submission

[root_submission.go](root_submission.go) implements the production submission
port for the [existing-seat root service owner](ROOT-SERVICE.md). It sends the
original native signed bytes through an independently approved owned HTTP route,
retains uncertain attempts durably, and uses the existing canonical receipt
adapter to reconcile outcomes. This is an actual `author_submitExtrinsic`
transport implementation, exercised only against local fixtures in qualification.

Mutation remains a package-local capability, with no public submission CLI,
service activation or deployment. The `root-service` command now prepares or
reopens its original journal and uses only its canonical reconciliation port,
with current authority absent. It recovers original signed bytes and retained
attempts before considering a new weight observation; it cannot send them.
No live key or chain send was used. This port does not implement current eligibility,
global custody authority or a native signing device. Those independent ports and
live approval remain required before production effects. The read-only canonical
chain's `submit` method and the ordinary RPC method whitelist remain disabled for
writes; constructing an observer cannot construct this capability.

The root key is held by a separate hardware signer with model/API/transport
still unspecified. The Ledger with Polkadot Substrate app belongs to subnet-owner
setup custody; each operator's demand-deposit vault wallet is separate again.
This submission port loads no seed or private-key file and commands none of
those devices. Exact native-payload support and durable request-hash
signing/recovery require separate verification before a root issuing adapter
exists. Physical confirmation may be required; no unattended behavior is
assumed. Original-byte reconciliation remains available while these gates stay open.
Subnet owners do not access Snow: owner signing runs on their own Ledger host,
and signed public output is handed off for import/reconciliation. This root
submission port does not require their device to be attached to Snow and does
not consume their private keys or replace root-hotkey approval with owner approval.

## Independent action and route approval

`rootSubmissionConfig` contains the independently provisioned service config and
a separate `rootSubmissionApproval`. The existing offline approval authenticates
the exact native action. The additional approval binds the complete service
configuration hash, packet hash, exact RPC URL, TLS pin, submission journal path,
read retry window and write deadline. Its Ed25519 signature is verified with the
service's independently provisioned approval public key; imported state cannot
choose that key or approve its own changed route.

The bytes approved are the literal
`urnetwork-mainnet-root-submission-approval-v1`, one zero byte, and Go
`encoding/json.Marshal` of the typed approval with its signature field empty.
This domain differs from the offline native-action approval. The full service
config binds mainnet EVM ID **964**, native chain/genesis, reviewed runtime and
source, existing root seat/generation, native nonce/mortal window, custody,
positive basket, fee reservation and original 1–8 broadcast bound. EVM945 is
refused. A proposal's approval does not prove current custody or eligibility.

The route requires one exact canonical HTTP(S) URL. HTTPS supports a lowercase
public hostname, including `https://archive.chain.opentensor.ai` with its default
port 443, or a literal IP address with an explicit port. Plain HTTP retains the
literal-IP and explicit-port profile. DNS resolves only the approved hostname;
every dial checks that exact hostname/IP and port. There is no proxy, redirect,
credential, query, fragment, alternate endpoint or fallback node. HTTPS requires
ordinary certificate/hostname verification and an independently approved
SHA-256 leaf SPKI pin. The URL and pin remain signed approval fields; an existing
signature cannot authorize another spelling, endpoint or certificate key.
Plain HTTP relies on the approved network's integrity. Endpoint ownership,
transport authentication and independent finality authority are separate facts;
the public archive does not need to be an owned node. Legacy signed policy
strings and evidence tags retain their exact bytes, including
`owned-rpc-assertion`, without proving public endpoint ownership.

## Durable original bytes and numbered attempts

`openRootSubmissionStore(config, create)` creates one private local submission
journal. Its path and `.lock` marker must be distinct from both the service/action
and offline-custody journals. Explicit creation is one-shot; reopen requires the
original signed configuration and complete state. Private regular files,
no symlink traversal, process locking, strict bounded JSON and synced atomic
replacement follow the existing custody/service storage pattern. A missing or
malformed file never supplies a fresh allowance. A durability/integrity failure
poisons the open owner until explicit reopen.

The [executable root runtime](ROOT-SERVICE.md#executable-observation-and-recovery)
also binds this exact submission configuration into the original service journal
before child creation and retains completed preparation there. Interrupted
preparation can reopen the original child, but completed child disappearance,
including removal of both state and lock marker, cannot replenish its allowance.
This adds no global custody or hostile-host rollback guarantee.

The adapter verifies the native signature and exact approved call using the
existing crypto/encoding implementation. It pins the first complete signed
extrinsic and hash durably. Another valid signature of the same payload cannot
replace it. Neither nonce, mortal era nor runtime domain is regenerated.

Each `submitRoot` call validates its packet, service hash, exact signed bytes and
attempt number against independently approved configuration. It then reconciles
the original signed action through the canonical owned-node reader, including
native ancestry/body commitments, current chain/runtime/seat/nonce and any exact
historical receipt. Finalized rollback or a same-height fork is refused.

Before a new write, the adapter requires the independent current-authority port,
checks the current signed domain/mortal window, repeats network identity checks,
and syncs that attempt as **uncertain**. Only then can bytes reach the transport.
The current-authority port must check complete effective eligibility, custody
fencing, approval/revocation and enforceable exposure; a read-only sample cannot
stand in for it.

One numbered attempt permits at most one HTTP request. The transport disables
connection reuse and request-body replay, has an explicitly approved 1–60 second
deadline, and contains no retry loop. Read retries use the independently approved
60–900 second window, within the operation's total 15-minute deadline. Duplicate
attempt calls reconcile and return the retained outcome; they do not resend.
After an uncertain attempt, a higher number can send the **same bytes** only after
fresh reconciliation and authority checks, within the original broadcast bound.
Gaps are allowed because the service may have reserved an attempt before crashing
without reaching the submitter; a later call cannot refill a skipped lower number.

A strict, bounded JSON-RPC response acknowledging the exact native hash changes
the local attempt to `acknowledged`. This is not finality. EOF, cancellation,
timeout, overload, redirect, malformed or contradictory response, wrong hash and
pool errors all retain uncertainty. Even an explicit pool rejection is not
treated as proof that these signed bytes were never accepted elsewhere. Failed
acknowledgement persistence cannot release or resend the numbered attempt.

## Reconciliation and service composition

The same owner implements `rootServiceReconciler`. It retains the canonical
reconciliation and terminal outcome independently of the service's journal.
Stored finalized dispatch/fee/runtime-deviation or supported mortal-expiry
evidence stays readable after current authority or route availability changes.
Reopening always preserves the original bytes and every recorded attempt.
Terminal evidence never renews another signing or send allowance.

Supply this object as the service's `Submitter` and `Reconciler`, and the
[offline public-signature handoff](ROOT-OFFLINE-CUSTODY.md) as its `Signer`, with
separately qualified current authority. This composition is tested through an
actual local HTTP submission and canonical finalized receipt. Construction alone
performs no RPC. A nil authority permits old receipt recovery but cannot send.
No constructor chooses a live endpoint or loads a native secret.

Calls serialize through cancellation-aware ownership and join their synchronous
ports. Canceled waiters do not consume an attempt. Cancellation after durable
reservation conservatively retains uncertainty, even if no request was sent.
The caller joins all use before closing the journal. The service's existing
supervisor owns cadence; this adapter starts no detached retry worker.

## Remaining production gates

The adapter reduces the missing production transport dependency. It does not
qualify a live route, owned root seat, current effective stake, economic policy,
global hotkey/nonce custody fence, native signing device, approval revocation,
hostile-host rollback resistance or actual payment exposure. Its local marker
cannot stop another host or an externally issued/broadcast signature. Creating
another directory is not another allowance.

The owned node remains the finality/storage trust boundary: header/body checking
does not implement GRANDPA or storage-trie proofs. A finalized preflight and
network recheck cannot atomically fence route cutover, seat churn, nonce changes
or same-version runtime replacement at inclusion. Native signatures bind their
native signing domain but do not bind registration generation, source/code hash
or a maximum fee. Preserve the original action's custody and inclusion-time
conditions from [ROOT-ACTION.md](ROOT-ACTION.md). Independently qualify authority,
native custody, route/runtime artifacts and activation before connecting a live
service. New root registration and SN25 miner/reset policy remain separate work.

Local deterministic qualification is retained in
[the submission adapter evidence](evidence/root-submission-20260927.md).
