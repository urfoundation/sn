# Initial standard-validator installation and process ownership

`sn-mainnet activate-validators` supplies an actual Linux static-unit installer,
current bootstrap admission and durable two-unit start/recovery owner. It is
bound to the original accepted bootstrap v3 plan and both independently signed
schema-3 UR configurations. Its [scoped independent qualification](evidence/validator-activation-qualification-20260930.md)
is sealed; no unit has been installed or started on a deployment host.

The native prerequisite reader now makes additional executable progress toward
current authority. `admit` and every injected fresh-start path authenticate the
original producer runtime at the current census and at the independently signed
activation checkpoint. The [independent native-admission qualification](evidence/validator-native-admission-qualification-20260930.md)
passes its focused and adjacent normal/race suites. The earlier receipt above
covers installation/process ownership; neither receipt authorizes a public
fresh start.

**Public fresh starts require separate current acceptance.** The
[current-admission adapter](VALIDATOR-CURRENT-ADMISSION.md) now reconstructs exact
original installation/anchor custody, both scan floors, native eligibility,
operator/key/proof and conservative majority-capacity evidence. It requires a
separately signed, explicitly selected policy and lifetime custody attestation.
Original process approval and earlier `admit-*` reports remain insufficient.
Initial scope requires fully imported empty local tails and an empty intent
graph. Implementation supplies no acceptance or live start.

`admit-evidence` adds a bounded current observation of the original deployed
contracts and both operator censuses. It performs real read-only transport work
when explicitly invoked; this implementation has made no deployment RPC or API
calls. Its optional `production_observation` journal projection and
`observed-operator-and-contract-evidence` disposition do not select the separate
current-authority capability. The [independent qualification](evidence/validator-current-evidence-qualification-20260930.md)
covers the local operator/contract readers and command fences, including a
corrected bounded EVM read profile; it does not qualify a combined live
deployment or public fresh start.

This component does not run netuid 0. The [root service](ROOT-SERVICE.md) still
requires its own current authority and native signing-device implementation.
Neither UR process counts as the root role. Chain identity and deployment also
remain open while Snow is reported syncing/preparing; this increment makes no
new live RPC observation.

## Independent scope

`validator_activation_authority.go` defines the signed
`urnetwork-mainnet-validator-activation-v1` envelope. Its independently supplied
Ed25519 key approves this process envelope; it is supplied separately rather
than selected from producer approvals or native signing material. The verifier
checks that supplied key and scope, not cross-role key-uniqueness policy.
Deployment must independently establish the approver's custody and authorized
operator role. Distinct public-key bytes alone would not prove separate control
or seed custody; native account identifiers also do not identify a signing
algorithm. No key loader or approval issuer is included. Approval-key custody
and any required cross-role separation policy remain deployment gates.
The signature covers the literal
`urnetwork-mainnet-validator-activation-approval-v1`, a zero byte and canonical
Go JSON of the typed envelope with `signature_ed25519` empty.

The complete plan binds:

- The original bootstrap config pathname and byte SHA-256, accepted v3 plan
  hash, netuid 25, the distinct majority/secondary roles and their approved
  producer config hashes. The original bootstrap admission still verifies
  hotkeys, native registration generations, independent producer approvals,
  operator configuration and separate custody namespaces.
- One exact standard-validator binary, fixed `sn-mainnet-validator-majority.service`
  and `sn-mainnet-validator-secondary.service` files, runtime config destinations,
  operational working/progress directories, UID/GID and progress source per role.
- Host machine/boot identity, the pinned systemctl binary, already-active mount
  requirements, one explicit owned IP RPC route (optional HTTPS SPKI), private
  activation journal path, a window of at most 24 hours, 1–128 operations,
  1–60-second manager calls and 1–600-second observation age.
- Explicit permission to install static units and to request production
  `validator run`. This request is necessary but is insufficient for a fresh
  start without the distinct current-authority implementation.

Readiness uses the existing complete finalized bootstrap census and retained
original custody. Both UR generations, activity, permits and signed native
windows must match. The bounded journal retains each role, original custody
seals, exact finalized block and full-readiness digest. These are owned-RPC
assertions, not independently verified finality/storage proofs. All activation
blockers remain explicit. Observation age begins before the read, so a slow read
cannot label old facts as newly fresh.

The new `native_prerequisites` projection checks the signed native epoch window,
the complete header/runtime at the signed activation block/hash, zero
`PendingServerEmission` and the original first native epoch at that checkpoint,
the same subnet generation/owner, one mechanism and explicit Recycle at both
boundaries. Current `LastUpdate` must cover the complete census; each role uses
the later of registration and last update under the originally signed age bound.
Current blocks may accumulate pending emissions and advance within the signed
epoch window. No post-start economic outcome is required to obtain these facts.

Both canonical anchors are checked again after all storage. Reads share one
finite signed route budget of 60–900 seconds, with existing transient retries
at the identical key/hash. The retained projection seals the complete raw-read
digest without duplicating full census vectors in both unit records.
Only the native epoch and signed checkpoint blockers are discharged in that
projection. `admitted-process-only`, `activation_ready: false` and the remaining
operator/contract/custody/majority blockers keep their meaning.

Read-only admission now also checks sample age and clock continuity before and
after its durable publication.
Its read permission may outlive the signed start window, but an expired sample
cannot be retained as newly admitted. Older records without this optional native
projection remain readable for their original consumed-start recovery; a fresh
start always makes a new complete observation.

The explicit `admit-evidence` operation first requires the complete original
approved contract profile. It authenticates the native-header Frontier mapping
and exact EVM header, then compares five deployed executable byte strings with
the approved implementation, proxy, reserve, vault and evidence artifacts. At
that same canonical hash it checks their stable domain getters, owner/guardian/
oracle and pause state, implementation/admin/beacon/initializer slots, the
reserve recorder, packed vault coordinator/escrow flag and evidence anchor.
Constructor accounting counters and operator count are not current zero
invariants. The initial single-policy profile remains required, with exact
approved economic/cadence fields and a nonzero effective block no later than
the observed EVM point; that bound does not prove the historical creation block. These reads
provide exact deployment observations, not source-to-bytecode provenance,
complete Safe history, independently proved consensus finality or storage proofs.

Each original independently signed schema-3 config supplies its exact operator
API/RPC routes and content-addressed activation context, activation payload and
both VPK/hotkey signatures. The observer verifies the dual signatures and exact
published activation record/companion domain. It then reads only an existing
private client JWT, requests a fresh nonce-bound client-key observation and
verifies its canonical complete registration history against actual historical
on-chain operator roots. The signed reply must echo the independently selected
client, hotkey, native point and mapped EVM point; the current registered key
must equal the activation VPK. A wrapper publisher or configured artifact
signer cannot substitute for that root authority. Both operator censuses use
the same mapped EVM point, even if the finalized head advances during the read.

This narrow evidence proves endpoint/client-key responsiveness and public
activation signature/publication consistency. It does not read signing seeds,
network JWTs or proof histories; register or refresh credentials; run proof or
producer workers; sign; submit; or start services. Historical native activation
eligibility, full proof-prefix/EMA/history replay and actual worker lifecycle
remain unverified. The broad production-health, deployed-contract provenance,
signer/global-custody and effective-majority blockers therefore remain explicit,
and `activation_ready` remains false.

Bounds are fixed at 16 operators per validator, 64 KiB per public context,
16 KiB per existing client credential and 256 KiB per client-key response,
with the protocol's existing registration-count limit. All public inputs for
one validator are authenticated before its first network call. Each validator
observation is clamped to two minutes inside the original signed route deadline;
the original observation-age and finite operation limits also apply. The
observer owns its finite HTTP/RPC transports and retains only public value
projections, exact response digests and request nonces, never credentials. Failure or
cancellation discards the complete new projection. Numeric EVM canonical
lookups, the native/EVM mapping and original native activation checkpoint are
rechecked after the final response; retained digests never authorize reuse as
fresh evidence. Old journals remain readable for original recovery.

This initial-bootstrap path accepts only the original schema-3 configs, whose
inspection explicitly excludes runtime-approval and production-authority
histories. Their single exact runtime tuple must therefore cover both blocks.
A later runtime upgrade requires separately approved producer continuity and a
bootstrap/activation rollover that preserves existing liabilities; changing
the original config or interpreting a newer spec version as compatible is not
authorized. This reader adds no compiled spec-version allowlist.

There is still no qualified production signer-custody handoff. The current
validator process loads a local hotkey seed and checks its identity; that is not
global exclusion of another signer. Its operator authentication belongs to the
running producer's credential and worker lifecycle. This command does not open
those credentials or treat config declarations, local files, separate public-key
bytes, or this native observation as substitutes for that missing authority.

## Concrete deployment files

The host custodian must preprovision the service accounts, protected release and
runtime-config directories, separate private operational directories, original
bootstrap custody and signed inputs. The command neither creates accounts nor
changes existing custody permissions.

Original bootstrap source configs remain private. Installation creates a
separate runtime config with **identical approved bytes and SHA-256**, owned by
root and mode **0440** with the exact service GID. The real production config
parser is run at that destination before admission; its complete inspection
must equal the original source inspection. Relative-path reinterpretation is
therefore refused. The copy has a different pathname and cannot replace a
missing or changed original source on restart.

The runtime config's physical parents must be traversable by the service and
protected from service writes. Referenced public approvals/evidence and protected
credentials retain their separately approved paths and custody; installation
copies none of them. Their production availability belongs to the still-open
current-authority/producer-startup gate. The working/progress directory must be
outside **both** validators' protocol, credential and evidence namespaces,
matching the actual producer progress publisher's path rule.

Installation uses temporary files, file sync, descriptor-relative
`RENAME_NOREPLACE` and directory sync. It never overwrites an existing config or
unit. An exact existing file can reconcile an interrupted installation; a
truncated or different file requires explicit operator disposition. The unit
profile is the existing qualified repair profile: `Type=exec`, numeric user/group,
absolute `validator run --config=... --progress-file=...`, `Restart=no`,
`KillMode=control-group`, no delegation, shell, environment files or hooks.

`install` performs a pinned, bounded `systemctl daemon-reload`, then verifies
both loaded units and their dependencies. It does not enable, start, stop or
restart them. Daemon reload is a real host effect and can run systemd's host
configuration machinery; the separately authorized deployment custodian owns
that host-wide boundary. No dynamic Warp unit is accepted.

## Operations and recovery

Start and generation-reconciliation steps share the installed-unit control lock
with [stopped repair](VALIDATOR-REPAIR.md) and
[active steering-hang repair](ACTIVE-VALIDATOR-REPAIR.md). That local exclusion
does not supply activation authority or replace independent host/signer custody.

Every invocation supplies the same exact approval bytes and independent key:

```text
sn-mainnet activate-validators claim|install|admit|admit-evidence|resume|status \
  --approval /absolute/activation-approval.json \
  --accept-approval-hash sha256:APPROVED_FILE_DIGEST \
  --independent-public-key 0xINDEPENDENT_APPROVAL_KEY
```

`claim` retains original authority in a private one-shot journal without network
or manager calls. `install` publishes the exact runtime configs/units and reloads
the manager. `admit` performs real loaded-unit and finalized bootstrap checks,
reporting `admitted-process-only`, never activation ready. `status` returns
retained observations. `resume` only reconciles already consumed starts and
cannot issue a fresh start.

`admit-evidence` performs those same host/native checks plus the bounded
contract/operator observation above. It consumes one original operation,
preserves both lifetime start allowances and requires the existing approved
client credentials and public activation files to be provisioned already.

The explicit `start --execute-approved-starts` form is implemented but remains
blocked without exact [current-policy acceptance](VALIDATOR-CURRENT-ADMISSION.md).
That refusal, and a purely local missing-installation refusal, do
not consume an operation allowance. Real bounded operations consume and sync an
operation before external work; retries cannot replenish the signed cap.

For each authorized fresh start, the owner re-reads current readiness and
independent authority, checks exact loaded release/config/unit properties and a
genuinely empty unified cgroup (including descendants), then syncs the consumed
start before the actual pinned `systemctl --job-mode=fail start`. It repeats
cancellation, clock, expiry and observation-age checks after that sync. Refusal
then retains the consumed start; it never refunds the allowance.

Each unit has one lifetime initial-start allowance. The exact acknowledged
systemd invocation/PID/monotonic start and fresh exact-source progress file form
its process postcondition. A first unit may complete while the second remains
unstarted or unresolved; both records survive restart separately. A lost start
acknowledgement is **uncertain consumed start**, even if a matching-looking
process exists afterwards. Manual host reconciliation is required, and no
automatic invocation adoption or replacement start is supplied. A retained
acknowledgement can resume its progress checks after expiry. Completion never
proves current health, weights, root participation or a 10/90 native outcome.

Cancellation joins each manager/RPC operation and releases the process journal
lock. Ambiguous synced publication poisons the current owner until reopen;
missing state, a different marker or changed original input cannot become a new
claim. All original root/contract journals and signed liabilities remain intact.

## Host and qualification limits

The host custodian must exclude concurrent privileged deployment edits and
administrative starts under one deployment lock. Pinning files and loaded
properties is not an atomic exclusion against another privileged actor. Local
markers do not provide hostile-host rollback resistance or cross-host hotkey
fencing. Changing a boot, unit generation or signed envelope requires explicit
retained-liability disposition, not deleting the journal.

Deterministic tests use real private files, existing independently signed
bootstrap fixtures and local finalized HTTP responses. A synthetic system manager
and explicitly test-only current authority exercise the exact command and
journal boundaries. No test executes a deployed validator or supplies production
credentials. Adjacent qualification includes the actual producer progress
publisher outside protocol state and its alias/ownership refusals, plus the real
child-process cancellation/join boundary. This is not a full `RunRelease`
operator/credential/deployment acceptance test. Sol medium qualification is
sealed: 16 focused and 28 adjacent roots pass normal/race (88 executions), and
twelve normal plus five selected race controls are causal. Astra max performed
implementation, compile/vet and the independent evidence audit without running
behavioral tests. The receipt retains exact source, dependency, binary and raw
evidence seals. It supplies no production activation approval.
