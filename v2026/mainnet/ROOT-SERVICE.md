# Root service decision and intent owner

[root_service.go](root_service.go) supplies one bounded existing-seat lifecycle:
observe a separately approved root basket, retain its decision and original
native intent atomically, and drive the [action owner](ROOT-ACTION.md) through
independent custody, current-authority and submission ports. The finite `Run`
supervisor joins every operation before returning. This closes the missing
decision-to-action ownership layer; it does not provide an activated validator.

The `root-service` command now composes this owner, original offline custody and
the independently [approved owned route](ROOT-SUBMISSION.md) for bounded
observation and original-liability recovery. Its public `run` has no mutation
authority or submission port. `activate` reports the missing capabilities and
exits before opening a journal or contacting a route. There is still no native
secret loader, real signing-device transport, production mutation-authority
adapter or root deployment. Exact-byte native HTTP submission remains a separate
package-local capability; a signed route approval cannot activate it.
The [bootstrap local phase](BOOTSTRAP-ROOT.md) now creates and resumes this
actual service journal alongside its custody owner. It neither runs the service
loop nor consumes an observation/broadcast allowance. Its signature import is
the same public-receipt handoff used by the service's independent signer port.
The canonical chain's existing `submit` method remains disabled. A successful
decision, adapter return or supervisor completion is never activation authority;
every service event keeps `activation_ready: false`.

## Actual deployment boundary

The separate [two-UR installation owner](VALIDATOR-ACTIVATION.md) now supplies
static units invoking the actual standard validator, exact runtime config
copies, current bootstrap admission and durable per-role start/recovery. Its
scoped [source qualification](evidence/validator-activation-qualification-20260930.md)
is sealed, and public fresh starts stay closed because a
qualified current activation-authority adapter is absent. It does not implement
the root role or make the standard validator accept netuid 0.

The executable now composes `rootServiceOwner.Run`, `rootOfflineCustody`
recovery and the reconciliation port of independently approved
`rootOwnedSubmission`. Before exposing mutation, it still needs a production `rootActionAuthority`
that admits effective eligibility, current seat/nonce/runtime and enforceable
fee/exposure bounds, plus global hotkey/nonce and pending-seat exclusion. It
also needs a real protected native signing device with durable request-hash
idempotency, exact issued-signature recovery and authenticated never-issued
responses. A missing public receipt is not a never-signed attestation. Route,
service-config and action approvals are separate original authorities; none may
be synthesized from a journal, root preview or signed process envelope. Native
secret loading/device transport, this authority and a deployed root supervisor
remain absent. The UR host component does not install this root command or any
native signing route.

The root `SetRootWeights` key is in a **separate hardware signer** whose model,
API and host transport remain unspecified. The Ledger with Polkadot Substrate
app holds subnet-owner setup keys; it is not the selected root signer. Each
operator's demand-deposit wallet is held in that operator's own vault and is
another distinct role. None of these identities can substitute for the root
hotkey in the original approved packet. This command loads no native secret.

Subnet owners have no Snow access. Their Ledger signing command must run on
their own host/device, with the signed public output delivered for Snow-side
import and reconciliation. No owner Ledger attachment or owner key loader on
Snow is assumed. The paths below name Snow-side copies of public, independently
approved artifacts provisioned by its operator; the owners do not need to read
or write those paths. This root command implements neither the separate owner
signing command nor an owner signature's conversion into root authority. Its
native receipt checks remain bound to the original root hotkey and action.

A future root-device adapter must fit `rootActionSigner`: `signOnce` admits
only the exact original request hash and action, while `recoverSignature` only
retrieves already issued bytes. Device and surrounding custody controls must
durably provide request idempotency, global hotkey/nonce and pending-seat
exclusion, and authenticated never-issued responses. If physical confirmation
is required, the request must remain finite and durable while waiting. Canceling
or losing the device response leaves the same unresolved signing request; it
cannot authorize a second signature. A disconnected device, missing host
receipt, generic app response or operator confirmation is not proof that a
signature was never issued. No unattended signing behavior is assumed.

## Executable observation and recovery

[root_service_command.go](root_service_command.go) dispatches
`root-service plan|prepare|status|run|activate`. A private
`urnetwork-mainnet-root-service-runtime-config-v1` file supplies three scoped
inputs:

- `root_config`: the exact path and SHA-256 of the original `bootstrapRootConfig`.
  Its original pinned service file is reloaded and authenticated.
- `root_validator`: the independently provisioned netuid-0 role, native
  generation, action-approval key, service-approval key and pinned
  `bootstrapChainRootApproval` file, using the existing v3 approval domain.
- `submission_config`: the exact path and SHA-256 of the original separately
  signed `rootSubmissionConfig`, binding the same complete service configuration.

Every invocation reloads these bounded, private regular files. Inputs, journals
and ownership markers must have distinct canonical paths. The command checks
only this root action's original approvals and custody; it does not repeat
unrelated validator, contract or historical bootstrap work. The runtime file
is independently provisioned configuration, not authority inferred from state.

```sh
sn-mainnet root-service plan --config /PRIVATE/root-runtime.json
sn-mainnet root-service prepare --config /PRIVATE/root-runtime.json \
  --accept-runtime-sha256 sha256:REVIEWED_RUNTIME_FILE_DIGEST
sn-mainnet root-service status --config /PRIVATE/root-runtime.json \
  --accept-runtime-sha256 sha256:REVIEWED_RUNTIME_FILE_DIGEST
sn-mainnet root-service run --config /PRIVATE/root-runtime.json \
  --accept-runtime-sha256 sha256:REVIEWED_RUNTIME_FILE_DIGEST \
  --maximum-steps 1 --interval 30s
```

`plan` returns the runtime byte hash and original approval identities without
opening journals or a route. `prepare` requires the existing service and custody
journals, then durably claims the exact submission configuration in the service
journal before creating or reopening that child. It marks completion only after
the complete child opens successfully. Repeating `prepare` resumes an interrupted
claim or reopens completed ownership; it never resets the child. If a completed
submission journal or marker disappears, even disappearance of both files
cannot create a new allowance. A malformed partial child remains unresolved.
`status` reads original retained state without chain access or allowance use.

`run` first compares all retained signatures in service, custody and submission
state. Conflicting valid signatures stop before a chain read. Any original
issued signature is retained before a new basket observation: the command can
recover it even if the observer is unavailable, current authority is gone, the
target is already stored, or the mortal window has closed. The same public
receipt is recovered into offline custody. The highest retained broadcast
number is preserved; uncertain sends keep their original bytes and attempt
history. An interruption between the two local writes resumes from whichever
original copy survived. A missing receipt remains unresolved, never never-issued.

The optional `recovered_signature` journal field identifies a receipt-only
liability recovered without requiring an invented `intent` decision. Previous
hold decisions, consumed observations and interrupted-observation markers stay
unchanged. This branch cannot sign or broadcast, even if another embedding
supplies mutation ports. Normal active actions retain their original lifecycle.
New submission-preparation fields preserve one-shot child ownership. Existing
journals without these optional fields remain readable; an older executable
that cannot decode new fields must not be used to reopen the updated journal.

After recovery, `run` invokes the real bounded supervisor. Unsigned actions may
observe the basket and retain a decision. Pending signed actions reconcile
canonical receipts through the approved owned route. Missing fresh-effect
capabilities stop before any signing or broadcast reservation. There is no RPC
override, private-key flag, device command, environment activation or retrying
HTTP write. Approved read retries remain 60–900 seconds (300 seconds is the
ordinary deployment choice), inside the reader's 15-minute operation deadline.
Run diagnostics and final scalar status use bounded joined exporters; sink
failure never controls custody progress. Every operation and store is closed
before return.

Exit 0 means the requested bounded work completed, not activation. Exit 2 is
invalid input; exit 3 covers blocked/unresolved ownership or execution and
finalized dispatch failure, fee overrun or runtime deviation; required finite
output/cleanup failures use exit 1. Original journal outcomes remain authoritative.
Every result reports `activation_ready`, `native_signing`, `network_effects` and
`current_authority_verified` as false. `activate` always exits 3 with explicit
effective-eligibility, device/global-hotkey-nonce, pending-seat, exposure and live
qualification blockers. No synthetic fixture discharges these production gates.

## Approved existing-seat scope

The independently provisioned `rootServiceConfig` contains a verified
[offline custody packet and trust](ROOT-OFFLINE-CUSTODY.md), plus a lifetime
allowance of 1–10,000 observations. That packet binds one native mainnet action:
EVM chain ID **964**, exact genesis/runtime/source artifacts, owned root
hotkey/coldkey and generation, nonce, mortal checkpoint, positive basket, fee
reservation, bounded broadcast count and private action-state path. EVM945 and
unapproved routes remain inadmissible. Configuration is supplied independently;
the journal cannot approve its own replacement configuration or allowance.

The [offline chain preparation](BOOTSTRAP-CHAIN.md) v3 admission independently
pins both this action approver and a full-service config approver. An external
domain-separated signature binds the complete root child plan and service
config, including `MaximumObservations`; the existing action signature alone
does not bind that service allowance. The accepted review retains the public
approval and complete child configuration. This offline check supplies no
`rootActionAuthority` port, current eligibility, custody fence or service start.
The standalone service and custody journal formats retain their original scope.

This layer supports only an independently approved `explicit_root_weights`
action. The proposed `accumulate_in_place` strategy still needs no periodic
native transaction. Root weights allocate a basket over destination **netuids**;
they are not SN25 miner scores or a reset mechanism. The majority SN25 validator
continues to use the standard `sn/validator` binary and its evidence-based
weights. That role supplies neither root basket approval nor chain Root origin.

## Finalized decision

[root_weight_observer.go](root_weight_observer.go) extends the approved read-only
canonical chain with an exact-block weight view. It first authenticates the
finalized ancestry back to the approved mortal checkpoint, current runtime,
root ownership/generation and nonce through the existing receipt port. At that
same finalized hash it then reads a complete bounded `NetworksAdded` census,
the selected root UID's weights, last update, setter enablement, rate and cap.
Only entries whose stored boolean is true count toward the runtime's active
network count. Absent weight rows use the authenticated metadata default; absent
or short last-update vectors use the pinned runtime's zero fallback.

The whole operation has a 15-minute deadline, bounded RPC retries and joined
storage workers. It repeats canonical block and network checks after reading.
Any contradiction, incomplete census or interruption returns no weight view.
The typed view and storage-evidence digest are retained in the decision. The
digest is not a storage proof. The port still trusts the approved owned node for
finality and storage; it does not implement GRANDPA or storage-trie verification.

[root_weight_decision.go](root_weight_decision.go) compares the target with the
runtime's max-upscaled stored weights, accounting for vector order and ignored
zero entries. It returns `target-observed` for a stored match, otherwise a hold
for disabled setter, rate limit, destination count/existence or concentration
cap, or `intent` when these necessary checks pass. A stored match is only a
comparison of the observed vector; it is not current submission eligibility.
Both fixed-point normalization branches and positive rounding match the pinned
Rust source. Offline replay covers every input value for maxima 32768, 32769 and
65535, a total of 131,075 two-element vectors.

The reviewed source is Subtensor
`67dcf7f791dc495064c293f080a0702cb433e51e`:

- `pallets/subtensor/src/subnets/weights.rs:881–994`: root setter checks and
  max-upscaled storage; `1144–1163`: last-update rate rule.
- `pallets/subtensor/src/subnets/subnet.rs:55`: true active network enumeration;
  `pallets/subtensor/src/utils/misc.rs:293`: missing last-update slot is zero.
- `pallets/subtensor/src/epoch/math.rs:78–130`: max-upscale conversion, with
  `substrate-fixed` commit `d5f70362f2e05b5f33fb51cd7baa825323e4e6c5`.
- `pallets/subtensor/src/lib.rs:3373`: effective-stake/owner-UID eligibility,
  which this decision layer does **not** authenticate.

These are reviewed source semantics, not a live mainnet Wasm attestation.
Complete stake attribution, economic-transition approval and current custody
still belong to the separately qualified authority before every fresh effect.

## One durable owner

`openRootServiceStore(config, create)` uses the action's approved state path for
one composite journal. The journal contains configuration, observation count,
pending-observation marker, last complete decision and the complete original
action record. It persists a consumed observation attempt before contacting the
reader. An interruption consumes that attempt while preserving the last complete
decision; a restart cannot replenish the observation allowance.

An `intent` decision and its reserved native action are one atomic write. Only
then does the local phase become `active`. That phase describes local ownership,
not a running or authorized mainnet service. All subsequent steps reconcile the
same action; they never sample a replacement basket, nonce, era or generation.
Finalized success, dispatch failure, fee overrun, runtime deviation or supported
expiry are retained with service completion in the same atomic write. Completion
never creates another action allowance.

The private store uses strict bounded JSON, private regular files, no symlink
traversal, exclusive process locking, a one-shot configuration marker and synced
atomic replacement. An ambiguous write or integrity failure poisons the owner
until reopen. Missing, malformed or foreign state cannot become fresh state.
There is no implicit migration from a standalone action journal: retain and
reconcile that journal with its original action owner. Creating another path is
not permission to duplicate the same hotkey's nonce or spend allowance.

Operations serialize through context-aware ownership. Canceled waiters do not
enter; active synchronous ports must return/join before `step` or `Run` returns.
The externally driven supervisor takes 1–10,000 steps and a 1-second to 1-hour
cadence. It stops on terminal/blocked state or poisoned storage, returning the
original hard cause even if cancellation also arrives. Optional event delivery
does not stop observation, signing reconciliation or the existing action. The
caller joins all use before closing the store. Local files/process locks do not
supply hostile-host rollback resistance or cross-host custody exclusion.

The private `Run` API now consumes a concrete `rootServiceOutput` created with
`newRootServiceOutput`, replacing the arbitrary synchronous publication callback.
It closes and joins that owner even when run admission fails. Each diagnostic is
a fixed scalar projection (`urnetwork-mainnet-root-service-diagnostic-v1`) with
closed phase/status, observation count, failure presence and previous delivery
counters. It contains no action vectors, custody packet, signature, raw error or
inferred activation readiness. The original error remains available to the caller;
the output path never calls its `Error` or `String` method. Poisoned storage
cannot hide its original cause behind a later reopen-required error.

The shared [diagnostic owner](../diagnostics/README.md) bounds queue count/bytes,
write deadlines and final drain. A full, disconnected, unsupported or partially
written destination remains operational output failure. Descriptor cleanup
failures are joined with the run result. Delivery snapshots survive `Run` return
for the embedding to inspect; without an independent consumer they are not an
alert-delivery claim. This does not wire a production root signing service.
Finite bootstrap output and its required action/custody writes retain their
existing behavior and original journal formats.

## Independent capability boundary

Observation, canonical reconciliation, current authority, native custody and
submission are separate objects. Missing authority, signer or submitter blocks
before a fresh signature or broadcast attempt is reserved. This does not block
lookup of an already issued signature or canonical reconciliation of an old
receipt: those use their original immutable request even after authority changes.
Local absence of a signature never becomes a custody-issued never-signed proof.

The signing bridge requires the exact durable `signing` intent. The submission
bridge requires exact verified signed bytes and their durable `pending` attempt.
Its independent `submitRoot` port receives the approved packet, service-config
hash, broadcast-attempt number and exact extrinsic/hash. The independently
[approved owned submitter](ROOT-SUBMISSION.md) authenticates its route/action
approval, pins original bytes and durably admits each numbered write once.
Current authority still supplies eligibility, global fencing and exposure.
Those fields correlate a request; they do not grant authority or prove a
custody device's behavior.

Production still needs qualified current effective eligibility, global hotkey
and nonce fencing, device-side durable idempotency/recovery, independently
approved live submission/owned RPC identity, and enforceable payment exposure.
The native signature does not bind registration generation, source/code hashes
or maximum fee. Finalized observations cannot prevent an inclusion-time seat
change or same-version upgrade. Pending-seat exclusion or authenticated incident
reconciliation, original-byte recovery and the fee/runtime limits in
[ROOT-ACTION.md](ROOT-ACTION.md) remain mandatory. New root registration and
protection are outside this existing-seat owner.

## Qualification

`root_service_test.go` and `root_weight_observer_test.go` use synthetic approval
and native keys, private local journals, local read-only HTTP fixtures and
explicit cancellation barriers. They cover durable decision-before-effects,
rehashed intent substitution, finalized rollback/fork, missing/denied ports,
lost signer acknowledgement, same-byte attempt limits, terminal fee evidence,
interrupted observation budgets, both sides of ambiguous writes, restart,
concurrent callers and rejected special/public/foreign state. The reader tests
cover metadata defaults, true network census and changed runtime/code/metadata,
network/finality and canceled final rechecks. Exact qualification is retained in
[the service owner evidence](evidence/root-service-owner-20260927.md). No live
key, RPC, transaction, activation or deployment is used.

The executable-runtime increment adds `root_service_runtime_test.go` and
`root_service_recovery_test.go`: public activation refusal, exact original
inputs, issued-signature recovery before an unavailable observer, recovery from
submission alone, retained uncertain attempts, conflicting signatures,
interrupted preparation prefixes, missing-child refusal, ambiguous signature
retention and canceled ownership waiters. Author checks are compilation and vet
only; independent normal/race and causal qualification must seal this increment
before any stronger source-qualification claim. Real device/global fencing,
effective eligibility, enforced exposure and live activation remain separate.
