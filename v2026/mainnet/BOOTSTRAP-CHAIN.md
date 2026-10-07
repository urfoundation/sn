# Chain preparation, readiness and retained trim action

`sn-mainnet bootstrap-chain plan/apply/resume` binds the retained owner-trim
review, two protected UR schema-3 configs, separately approved root-service
config, signed contract phase and signed root phase to one durable local
preparation. Apply prepares the existing reserve
CREATE custody owner and [root custody owners](BOOTSTRAP-ROOT.md). It does not
open an RPC connection, import a signature, issue a transaction or start a
service. These three preparation modes have no online or submission option.
The separate `readiness` mode observes current finalized role prerequisites
from an explicitly selected route after checking the original v3 custody.

This closes the missing composition and restart boundary between the separate
preparation commands. Every result retains `activation_ready: false`,
`network_effects: false` and the outstanding chain phases. A successful local
preparation does not establish a safe executed trim, full contract installation,
two eligible validators, a running root role or realized native economics.

## Offline contract installation prerequisites

Review the installation scope before preparing or importing transaction custody:

```sh
sn-mainnet bootstrap-chain contract-plan --config /private/chain.json
```

This reads the original independently pinned v3 approvals and release artifacts,
rebuilds every approved implemented action projection through evidence CREATE,
and reports the full nine-action installation scope. It opens no journal or
network route. A shorter approved prefix remains valid for its original actions;
missing anchor approval cannot invalidate or replay earlier completed work.
An approved ninth envelope is reported as `sealed-reservation-only`: its outer
transaction fields do not establish Safe-inner semantics or authority.

After preparation or any completed action, inspect the same retained custody:

```sh
sn-mainnet bootstrap-chain contract-readiness --config /private/chain.json \
  --run-dir /private/custody --accept-plan-hash sha256:ORIGINAL_V3_DIGEST
```

The command first verifies all five original preparation journals under shared
locks, then retains shared locks for the approved contract prefix. Each existing
action must carry its original complete marker, signed config, exact signature,
predecessor seal, cumulative attempt allowance and successful receipt's semantic
and financial postconditions. The report retains each receipt and both journal
hashes. `receipt_observation: retained` is historical local evidence; there is no
current canonical chain audit. All original files and markers stay unchanged.
Later absent journal/marker pairs are `not-claimed`. A missing original journal,
partial claim, conflicting owner, changed lineage or invalid receipt is
`unresolved`; inspection cannot repair it. A valid earlier prefix remains visible
if a later child is unresolved, but complete attempt accounting remains false.
Missing later journals are stable while the reserve's shared lock prevents a
cooperating contract writer from acquiring the prefix.

Results use `urnetwork-mainnet-bootstrap-chain-contract-readiness-v1`. Every
result preserves all five pending chain phases and false signing, network effect,
current-chain, Safe-authority, installation and activation flags. Exit 3 means
the bounded inspection finished with explicit installation blockers; exit 1
means unresolved custody/cancellation/output, and exit 2 means invalid inputs or
flags. Neither mode accepts RPC, signing, import, submission or service flags.
Original preparation plan schemas/hashes and v1/v2 resume semantics are unchanged;
older scope cannot acquire this inspection through a new accepted hash.

### Remaining authority and forward-only budget work

Eight original actions are executable. The ninth, `fixValidatorEvidence`, is an
owner-only call on the initialized proxy. `Deploy.s.sol` stops after vault
binding, and `STCoordinator.sol` checks only that the one-time evidence address
has code. Installation therefore still needs the independently authenticated
evidence immutable domain and code provenance, exact Safe runtime/owner/threshold,
Safe transaction digest/nonce/signatures, separate relayer nonce/fee custody,
Safe-inner success, exact binding event and canonical coordinator getter. An
outer receipt status of one cannot substitute for these observations. The
configured owner address and release file hash alone do not prove them.

The original contract schema caps cumulative local attempts at eight. Nine fresh
installation sends exceed that bound, and even an approved nine-action prefix
does not implement a full-installation attempt policy. Both new modes expose the
mismatch before live signing. `minimum_fresh_installation_attempts: 9` describes
a fresh installation, **not** a requirement to replay completed actions. With
eight retained successful receipts, `unfinished_actions` contains only
`evidence-anchor`, spent attempts remain eight, and the original remaining
allowance is zero. A signed pending action remains a liability and must first be
reconciled; an additional send is not automatically necessary.

Increasing `maximum_attempts` in place cannot solve this: every original marker
and record binds the exact signed config, and every descendant binds its complete
predecessor record. Even a separately signed changed config cannot adopt those
records. Values above eight are also outside the original schema. The actual
requirement is an independently approved successor owner that can adopt a
completed prefix while conserving original liabilities. The separate
[successor execution owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) implements that
adoption; these prerequisite reports synthesize no executor or approval. The
report binds its requirements to the original contract plan/config and the
validated retained action seals.

The successor code path uses a new domain/schema and separate
durable claim, with an independent signature covering:

1. The original plan/config hashes, custody ID and directory, every adopted
   action's exact journal/content seal, original signed envelope/transaction hash,
   receipt and predecessor relation. Completed actions are adopted without
   re-signing or replay. Before an online action, their historical receipts must
   be reauthenticated through the existing canonical adapter.
2. Every unfinished original nonce and signature, with reconciliation before
   deciding whether another send is needed. No expiry or failed transport reply
   releases original EVM liability. Added actions require explicit envelope,
   artifact, owner/Safe and relayer authority; they cannot inherit it from adoption.
3. An absolute cumulative attempt cap at least equal to original recorded
   attempts plus the finite approved allowance for unfinished sends and a retry
   margin. The original spent attempts are an immutable floor. A successor
   cannot reset counters, and a further successor must include all earlier spend.
4. A lifetime value-plus-maximum-gas cap conserving all unresolved original
   reservations and canonical spend, with explicit incremental reservations for
   added actions/senders. Separate Safe-inner and outer-relayer liabilities and
   current funding checks must remain bounded. Renewing approval cannot silently
   renew spent financial authority.

These requirements permit an approved increase without restarting deployment.
The [focused qualification](evidence/bootstrap-contract-prerequisites-focused-qualification-20260929.md)
passes all twelve missing/partial custody, cap-change, exact completed-prefix
reuse, lock-release and command-scope roots in normal and race modes, with six
causal controls in both modes. The broader [partial adjacent qualification](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md#separate-composed-smoke-and-partial-adjacent-coverage)
passes 150/270 roots in both modes; 120 remain unrun. No live installation is claimed.

The qualified [signed local successor preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md)
adds preview/prepare/resume under one fixed original physical custody root.
It separately approves local retained-prefix preparation with additive proposed
ceilings; it grants no executable allowance, Safe authority or signing path.
Its [scoped receipt](evidence/bootstrap-successor-preparation-qualification-20260929.md)
covers thirteen focused and three adjacent roots normal/race and thirty-two
intended causal executions. A copied or moved root requires separately approved
migration; ordinary same-root restart retains the claim.

The separate [unsigned successor proposal](BOOTSTRAP-CONTRACT-SUCCESSOR.md)
reads all eight completed originals, preserves their seals and any ninth reserved
envelope, and computes an additive attempt/lifetime proposal for the unfinished
anchor and retry margin. It creates no successor approval or execution custody;
current Safe authority/binding and the signed migration owner remain unresolved.
The separate [qualified offline Safe profile verifier](SAFE-RELEASE-VERIFY.md)
checks explicit published proxy/singleton artifacts, ABI, source/compiler inputs
and storage layout, while retaining false live-account and authority flags.
Its [scoped qualification](evidence/bootstrap-contract-successor-qualification-20260929.md)
passes six new and twelve inherited roots normal/race plus six causal controls
in both modes. The separate [successful full-v3 public-command fixture](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md)
passes normal/race and all six causal executions on `c294fefd`, retaining its
original five preparation journals and eight executed contract actions.
This proposal does not create executable authority.

The execution owner additionally rechecks its exact live claim, ready marker,
counted intent/record prefix and interrupted outcome before a send or completion
report. [Deterministic regressions](evidence/bootstrap-successor-live-custody-20261001.md)
cover changed or missing custody after reservation. Original approvals and
journals remain unchanged. The separate [public current-only Safe route](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md)
requires a newly signed v2 risk-policy acceptance and an exact revision opt-in;
actual policy acceptance, installation and activation remain unresolved gates.

### Original-authority evidence anchor and installation readback

`contract-successor-execution-readback` uses the same original config, custody,
successor request, Safe request, signed execution approval and canonical approval
flags as online successor resume. It requires `--online` and refuses `--submit`.
For the public current-only route it additionally requires the exact retained,
independently signed v2 `--accept-safe-current-policy` hash. The choice of that
risk policy and its actual approval remain external gates. No report or flag
supplies a signature, new nonce, custody fence or service-start authority.

This command makes network reads only. It may finish the original counted
terminal journal locally after an interrupted reply/publication. Original eight
CREATE/link records, signatures, attempts, reservations and successor approval
stay unchanged. An anchor receipt must contain the exact coordinator
`ValidatorEvidenceFixed` event before the matching Safe `ExecutionSuccess`,
with the retained evidence address, digest and transaction identity. A current
getter or outer status alone cannot complete the action.

Readback reauthenticates every original receipt and the anchor's native/EVM
inclusion. At the first anchor inclusion it proves the complete native storage
prefixes and exact code/metadata for all five installed accounts, plus the Safe's
complete storage with its exact incremented nonce. The finite initial profile
requires the reviewed layouts and initial policy height; unknown mapping entries,
previous service accounting, changed evidence domain and incomplete proofs refuse.
Consequently installation must precede operator/service population, including
within the anchor's EVM block. The original terminal event's retained runtime
authority prefix selects the historical interpretation; later approvals cannot
backdate a different runtime into that event.

At one current finalized mapping it also verifies the complete Safe storage and
the existing five-account executable/domain views. Normal later accounting is
permitted by those explicit current views. A later Safe nonce is an authenticated
observation, never approval of that intervening operation or another send. A final
canonical/ancestry and live custody check closes the readback. The typed
`inspectBootstrapContractInstallationAt` producer allows later service admission
to select this same boundary for its other checks; a JSON report is not a
capability or a replacement for fresh producer execution.

The current initial-policy effective block must still equal the original proxy
CREATE receipt's exact EVM block. Head advancement and later Safe activity cannot
reset that epoch clock. The [clock qualification](evidence/installation-policy-clock-qualification-20261001.md)
covers changed getter replies and actual storage rewritten after the anchor;
these current field reads retain their owned-RPC trust boundary.

The `urnetwork-mainnet-bootstrap-contract-installation-v1` result includes a
stable `installation_identity_hash` over original preparation/contract/execution
authority, eight original custody seals and the exact terminal event/receipt.
The identity's `anchor_event_hash` is the terminal **local journal** event seal;
the exact EVM binding log is authenticated inside `anchor_receipt`, not named by
that field.
Its separate `content_hash` binds the moving observation snapshot and selected
current authority. Complete Safe history remains false under current-only v2;
complete contract execution history, complete pending state, activation and
network-effects flags remain false. Native finality still trusts the signed
owned route. No live installation, public policy acceptance or deployment is
established by the [local qualification](evidence/contract-installation-anchor-20261001.md).

## Read-only current prerequisites

After local preparation, run:

```sh
sn-mainnet bootstrap-chain readiness --config /private/chain.json \
  --run-dir /private/custody --accept-plan-hash sha256:REVIEWED_DIGEST \
  --rpc https://owned-rpc.example --retry-window 300s
```

The command reloads the original independently pinned approvals and requires
the same accepted v3 plan and run directory. It opens all five existing markers
read-only, takes nonblocking shared locks, and verifies the complete original
journals and their signature lineage. Missing or interrupted state is unresolved;
readiness never creates, repairs, signs, imports or advances it. Concurrent
custody writers must finish before observation starts. V1/v2 journals retain
their original resume behavior and cannot acquire v3 readiness scope.

One bounded census at one authenticated finalized block checks the approved
network, runtime code and metadata, subnet owner/generation, both UR hotkey,
coldkey and registration generations, current activity and validator permits,
signed native block windows, and the signed maximum subnet census count. The
separate netuid-0 role checks its exact approved uid and registration generation,
mortal action window and canonical era checkpoint. A canonical block recheck
after the checkpoint read detects a changed finalized mapping. Original trim
removal requests remain review evidence; completing these role checks does not
claim the old miners were removed or that the trim is executable.

Results use `urnetwork-mainnet-bootstrap-chain-readiness-v1`, bind the accepted
plan, the five retained journal hashes, the complete census envelope and distinct
majority/secondary/root blockers. `observed-prerequisites` means only the listed
read-only checks passed; `blocked` retains a complete observation with explicit
conflicts. An unavailable route, unsupported or changed runtime, incomplete
census or changed finalized mapping returns `unresolved`, with no partial role
success. HTTP 502 cannot become a retained-census fallback. Each invocation
observes again; it never reuses an earlier readiness result as authority.

`current_authority_verified`, `native_signing`, `network_effects` and
`activation_ready` remain false. Each role separately retains its missing
activation prerequisites. These include the UR native epoch window and signed
activation checkpoint, production admission/operator health, deployed contract
verification, effective stake majority, and signing-device/global custody
fencing. Root effective delegated stake, eligibility, nonce/weight requirements,
independent current authority and actual service activation remain unresolved.
All five original pending chain phases remain in every result.

Exit 0 means the bounded observed prerequisites passed. Exit 3 means an
observed blocker, integrity refusal or old-scope refusal; exit 1 means unresolved
transport/custody/cancellation or output failure; exit 2 means invalid flags,
route or independently approved input. Only completed input admission produces
the readiness result. A route supplies observations, never submission approval.

The September 29 source increment includes deterministic command/restart,
stale-generation, permit/activity, signed-window, checkpoint conflict, reorg,
route/runtime failure, missing-state and lock-release regressions. Its
[Sol qualification](evidence/bootstrap-readiness-qualification-20260929.md)
passes all ten new roots and the exact 148-root adjacent scope in normal/race
modes, plus vet and four causal controls. This qualifies the recorded source
and module graph; live eligibility and activation remain unresolved.

## Separately approved owner-trim action

Owners sign on their own computer without Snow access. The
[owner-side Ledger workflow](OWNER-SIGNING.md) adds a separately approved v2
Ed25519/RFC78 action, portable `trim-export` request, real pinned-SDK signing
command and exact `trim-import-reply` handoff. It preserves original v1 custody
and keeps physical device, runtime digest and current authority qualification
explicitly open. The v1 procedure below retains its original sr25519 contract.
The separate [best-effort workflow](OWNER-TRIM-BEST-EFFORT.md) uses a fresh Ledger
action domain under original v3 or passive-root v4 custody, plus `trim-submit-plan`
and `trim-submit` with an independently signed exact residual-risk policy. It
cannot reinterpret a claimed strict action or supply its enforced-window capability.

The trim phase retains the exact accepted v3 preparation and its five existing
journals. It adds one fixed `owner-trim-action.json` with a permanent exclusive
marker. `trim-plan` reads an unsigned `urnetwork-mainnet-owner-trim-execution-v1`
template and exact runtime metadata, fills the original preparation/custody,
policy/review, subnet owner/generation and reviewed best-capacity bindings, and
emits an unsigned config. The template supplies the independently selected
custody ID, nonce, finalized birth/hash, period, fee reserve, broadcast limit and
owned route. No nonce or anchor is inferred from a public service.

```sh
sn-mainnet bootstrap-chain trim-plan --config /private/chain.json \
  --run-dir /private/custody --accept-plan-hash sha256:ORIGINAL_V3_DIGEST \
  --trim-config /private/trim-template.json --metadata /private/runtime.hex \
  --trim-approval-key 0xINDEPENDENT_PUBLIC_KEY
```

An external approver signs the execution schema, a zero byte, and canonical Go
JSON for the complete config with `approval_signature_ed25519` empty. The key
is supplied independently with every invocation; the config cannot provide its
own trust. This fresh domain covers the exact native action and canonical owned
IP route, TLS pin when HTTPS, read/send deadlines, custody and financial bounds.
The original v3 approvals remain unchanged. Native sr25519 signs the direct
owner call's standard mortal payload, not the separate approval envelope.

After external approval, `trim-apply` claims the new local action and
`trim-resume` reopens it, using the same flags except `--metadata`. Neither opens
RPC or signs. `trim-import --signature /private/original-signature.hex` retains
one independently obtained public native signature; it never creates one and
cannot replace valid original bytes or resolve unknown signing custody.
`trim-reconcile` reads only the signed owned route and can settle original
dispatch/fees, mortal expiry or nonce conflict. It has no `--rpc` override and
no authority, signing, submission or activation switch.

The durable executor implements signing recovery, numbered submission attempts
and exact canonical receipt recovery. The concrete native adapter authenticates
headers, complete block bodies, phase-specific dispatch and the coldkey's fee.
These reads trust the independently approved owned node for consensus/storage;
they do not implement a light client or storage proofs. A signature lookup or
send timeout preserves the same action, nonce, era and allowance. Any ambiguous
journal publication requires reopening. New approval cannot adopt old custody.

Financial finality is reported separately from generation correspondence. The
parent and inclusion-block censuses retain exact protected UR/root generations,
actual surviving UID compression and every observed old-miner residual. A later
same-block call may affect post-state; no missing generation is falsely attributed
solely to this trim. If post-state is unavailable, the financial receipt remains
durable and a later reconcile can fill only the missing readback. The command
never reports a full reset or service activation.

Current admission rechecks the exact nonce/call/profile and original generation
scope, the existing bounded safe-set predicates, absence of owner proxies and
network immunity through the **original** expiry. Rechecking at a later head
conservatively demands a full period beyond that head while retaining the
original action expiry. This may refuse a shorter safe remainder; it cannot
renew the action. Proxy absence does not freeze future proxy/multisig actions.
Subnet immunity does not prevent a privileged dissolution or immunity change.

No production capability currently enforces future owner/governance/root
changes, global coldkey exclusivity, source-to-Wasm provenance and fee exposure.
The executable owner requires that independent capability before signing and
again before each send; the owned adapter independently refuses an absent one.
Conditional qualification and signed configuration are insufficient for this
strict path. The [October 2 separate best-effort domain](OWNER-TRIM-BEST-EFFORT.md)
implements original-byte submission only after independent exact residual-risk
approval; no such live approval is supplied. Its conservative defaults still
require pruning immunity through original expiry and observed closed registration.
Current runtime/route approval, device and exclusive external custody remain live
inputs. See the historical
[source checkpoint and qualification scope](evidence/owner-trim-execution-source-20260929.md).

The [qualified successor](evidence/owner-trim-null-storage-repair-20260929.md)
`ce567305` is integrated with original v3 approval/custody semantics intact.
Sol's 25 focused roots and exact 229-root expanded union pass normal/race,
with vet and all eight causal controls. Its 23 new roots include the proxy-reader
repair; two pre-existing command roots also match the focused selector.
R1 `4033609` remains a preserved failed qualification: 21/22 focused and
225/226 expanded roots passed in each mode before the null-storage bug was
fixed. These local results do not supply the missing production capabilities
or establish a native reset, service activation or live acceptance.

## Inputs and review

The strict JSON configuration has these fields:

| Field | Required value |
| --- | --- |
| `schema` | `urnetwork-mainnet-bootstrap-chain-config-v3` for new preparation |
| `deployment_id` | Same independently chosen deployment as both child plans |
| `netuid` | `25` |
| `network` | Independently provisioned `native_chain`, `genesis_hash`, `evm_chain_id: 964` |
| `run_directory` | Precreated canonical absolute owner-private directory shared by both children |
| `owner_trim_policy` | `{path, sha256}` for the exact original census policy |
| `owner_trim_plan` | `{path, sha256}` for the retained safe partial owner-trim plan |
| `contracts` | `{path, sha256}` for the signed [contract phase config](BOOTSTRAP-CONTRACTS.md) |
| `root` | `{path, sha256}` for the [root bootstrap config](BOOTSTRAP-ROOT.md) |
| `ur_validators` | Exactly two public role declarations described below |
| `root_validator` | Independent netuid-0 role, action/config approval keys and pinned config approval described below |

Each role declares `validator_id`, `hotkey_account_id`, `coldkey_account_id`,
`registration_block`, `config: {path, sha256}`, `role`, `implementation`, and
`approval_public_key_ed25519`. The roles are ordered `majority`, then
`secondary`; both declare `implementation: "sn/validator"`. Each independent
approval signer is a canonical nonzero `0x`-prefixed 32-byte Ed25519 public key.
The IDs, hotkeys, config
paths and config byte hashes must differ. Each exact registration generation
must appear under `ur-validator` in the retained trim policy's protection set
and in the selected plan's exact surviving generations.
The separate root hotkey cannot count as either UR validator or appear in the
requested trim removals. The planned reserve hotkey is also excluded from
requested removals. The root's exact UID, hotkey, coldkey and registration block
must match the retained excluded-root census.

V2 and v3 decode the exact pinned config bytes through the standard validator's
strict grammar and full initial schema-3 validation, then verifies the declared
content-addressed production approval and its domain-separated signature.
Signed hotkey, validator ID, network and deployment must match the independently
selected role. Runtime version, code/metadata and reviewed source must match
the trim and signed child scopes; the approved subnet owner must match the trim
policy. Both approvals include both protected UR hotkeys and agree on their
initial policy, runtime, census, activation window and contract declarations.
Distinct role custody namespaces cannot overlap other roles or bootstrap
inputs, journals or lock markers.

The accepted plan retains these public signed facts under
`ur_validator_config_inspections`. The result reports
`ur_validator_configs_verified: true` and
`two-signed-production-configs-verified-live-admission-pending`. This verifies
config admission only. The intended majority label does not prove current
stake, permit, key ownership, binary identity or a healthy running service.
Coldkey and registration generation are matched against the retained protected
census; the producer approval itself signs the hotkey, not its live generation.
Actual generation, current signed window, deployed contract addresses/code,
operator evidence and health, runtime capability and two live validators remain
pending. No credential or operator evidence contents are opened and no signature
is created. The later [production path](VALIDATOR-PRODUCTION-RUNTIME.md) must
perform its own current admission before starting a writer.

This initial-bootstrap check rejects runtime/authority history and schema-2
observation inputs. It reads the declared approval source on every invocation;
unlike an existing producer's restart loader, it cannot replace a missing or
changed source with retained-state approval bytes. Approved renewal/history
requires the existing producer workflow and is outside this composition.

The root action and contract approval signatures are verified through their
existing validators. Their exact network, runtime/source tuple, deployment and
state paths must agree with the preparation and retained trim policy.

## Separate root config admission

V3 requires this independently provisioned `root_validator` declaration:

| Field | Required value |
| --- | --- |
| `role`, `netuid` | `bittensor-root-validator`, explicit `0` |
| `implementation` | `sn/mainnet/root-service` |
| `hotkey_account_id`, `coldkey_account_id` | Canonical nonzero AccountId32 hex, matching the signed root action and retained root census |
| `seat` | Exact `{uid, registration_block}` for the existing netuid-0 generation |
| `strategy` | `explicit_root_weights`, the existing supported service strategy |
| `action_approval_public_key_ed25519` | Independent public key pin for the existing root action approval |
| `approval_public_key_ed25519` | Independent public key pin for full service-config approval |
| `approval` | `{path, sha256}` for the external public service-config approval |

Both keys are canonical nonzero `0x`-prefixed 32-byte Ed25519 public keys. The
action key must match the root service's custody trust. The full-config key
verifies the additional approval below; the two approval keys may differ. They
must come from the operator's approved authority through the independent chain
input. Neither key is learned from the inspected root service or approval file,
and arbitrary self-signed input does not establish organizational authority.

The strict approval file has schema
`urnetwork-mainnet-root-service-approval-v1`, `deployment_id`, `root_plan_hash`,
`service_config_hash` and `approval_signature_ed25519`. The child
`bootstrap plan` command produces the read-only root plan whose `content_hash`
is approved. `service_config_hash` is `sha256:` followed by SHA256 of canonical
Go JSON for that plan's entire `service` field. Signature bytes are 64-byte
lowercase hex without `0x`. Ed25519 signs the approval schema string, a zero
byte, and canonical Go JSON of the approval with an empty signature, in the
field order listed above. Approval issuance stays with the external approver;
the command only verifies public signatures.

The child plan seals exact config/service file bytes, the complete action and
custody trust, network/runtime/source, deployment, run directory and state paths.
The additional signature therefore covers the full observation allowance and
service configuration, which the original action signature alone does not
approve. Changing whitespace in a pinned child file requires a new approval as
well as a new accepted preparation hash. A signed replacement action key still
must match the independent role pin. A new valid config approval cannot adopt
or renew any already claimed custody.

The v3 plan retains `root_validator_config_inspection`, containing the full child
root plan and config approval. The result reports
`root_validator_config_verified: true` and
`root_validator_status: signed-root-service-config-verified-live-authority-pending`.
These facts authenticate offline configuration only. Current root eligibility,
effective stake, seat retention, delegation and basket rights, source-to-Wasm
authority, live route, global custody fencing, fee exposure, key possession and
the actual service/binary remain unproved. No service loop or current-authority
adapter is attached. The root role remains separate from both UR validators.

V3 supports the existing explicit-root-weight service only. The read-only
observer's `accumulate_in_place` policy remains a separate workflow and requires
no periodic native transaction. This preparation does not change that policy.

## Exact offline inputs

The trim selection is rebuilt from the retained census to reject a resealed
different removal set. This is offline internal consistency, not authenticated
live state or an execution-time invariant. All original execution blockers and
the original plan hash remain in the preparation record. The existing owned
route recheck/qualification and actual safe execution are still required.

All inputs and transitive root-service/contract-artifact references retain
their exact byte pins. Each input must be a bounded owner-private regular file
with a physical owner-private parent directory; canonical absolute paths must
not traverse symlinks. The existing loaders enforce their bounds: chain/root
configs 1 MiB, contract/UR configs 2 MiB, UR production approvals 64 KiB, root
service-config approvals 16 KiB, retained trim plans 32 MiB. The
contract artifact catalog retains its existing separate bound. The same bytes
that pass a pin are decoded, without reopening root or validator config paths.
The resulting preparation plan, including its full root inspection, is bounded
to 512 KiB before any journal opens. The root approval file participates in all
input/journal and UR custody namespace separation checks. Every invocation reads
its exact source bytes; retained progress never replaces a missing approval.

```sh
sn-mainnet bootstrap-chain plan --config /secure/ur-mainnet/chain-preparation.json
sn-mainnet bootstrap-chain apply --config /secure/ur-mainnet/chain-preparation.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CHAIN_PREPARATION_HASH"
sn-mainnet bootstrap-chain resume --config /secure/ur-mainnet/chain-preparation.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$CHAIN_PREPARATION_HASH"
```

Plan is read-only and deterministic. The dedicated
`urnetwork-mainnet-bootstrap-chain-preparation-v3` seal is SHA256 over that
schema, a zero byte and canonical Go JSON with an empty `content_hash`.
Neither the blocked review graph hash nor an individual child plan hash is
accepted as the local composition confirmation.

Existing v1 and v2 configs and journals remain readable and resumable under their
original domains and exact hashes. V1 role config bytes remain opaque and its
result continues to say
`two-protected-role-inputs-pinned-production-admission-pending`; no inspection
facts or verified flag are added. V2 retains its two verified UR configs and
original v2 result, without root-role inspection or verified fields. Neither old
schema accepts a root-role declaration or acquires root-config authority.
New `apply` requires v3. A new v3 plan cannot adopt or upgrade already claimed
v1/v2 custody, even with a newly accepted hash.

## Ownership and recovery

The shared run directory has five distinct journals and their `.lock` markers:
`bootstrap-chain.json`, `reserve-create.json`, `bootstrap-root.json`, and the
exact approved root custody/service state paths. Input files cannot alias any
journal, lock marker or another input. Apply requires all five destinations and
markers unused. It does not adopt an existing independent deployment under a
new preparation; retain those original child commands when custody already
exists without this parent journal.

The chain journal retains the full preparation plan, then advances from
`claimed` to `contracts-retained` to `prepared`. Those are local milestones,
not chain phases. Atomic file replacement and file/directory sync precede
acknowledgement; one local exclusive lock serializes the composition. Child
owners close before their progress is acknowledged. A failed publication
requires reopening the preparation owner.

Resume reloads every pinned input before opening the journal. It reopens and
checks real child state even after local completion. A child that completed
before its parent progress was published is reconciled in place. A missing or
invalid completed journal/marker is refused; lost custody cannot become an
unsigned fresh allowance. The one pre-child recovery case permits a complete
accepted-plan marker and absent/initial progress only while every child file
and marker is absent. Partial markers, advanced progress and ambiguous child
state fail closed.

The existing child commands remain the public signature-import interfaces.
Use their original child hashes and approved configs after this preparation;
chain resume then reports their exact retained signatures and consumed
allowances. It does not rewrite an approval, refresh an era or nonce, replenish
a limit, or rebroadcast. Observed signature hashes are consistency checks;
child journals remain authoritative. Local locking does not establish a
distributed custody fence or protect against hostile-host rollback.

Exit 0 means that the requested local plan/result was emitted. Malformed or
stale inputs exit 2; an unaccepted plan or refused retained ownership exits 3;
phase, cancellation, durability and output failures exit 1. Keep all journals
and markers after any failure.

## Qualification scope

V3 adds deterministic regressions for independently pinned root action/config
approvers, changed service allowances, exact approval domain and full child
scope, malformed/missing role inputs, stale or unavailable approval sources,
custody/input namespace overlap and refusal to renew existing custody. An
independent pre-v3 wire shape checks exact v2 canonical bytes, hash and result
scope; v1 recovery retains its existing compatibility check. Its separate
[v3 qualification](evidence/root-role-admission-qualification-20260928.md)
records 403 full normal roots and all 38 bootstrap-chain race roots. The
earlier v1/v2 receipts below retain their original narrower scope.

V2 adds deterministic controls for real signed two-role admission, absent or
wrong-domain signatures, independent signer/role/runtime/source disagreement,
stale approval bytes, reapproved-config restart refusal, namespace overlap and
exact v1 recovery. These additions and the shared validator decode extraction
have their own [Sol qualification](evidence/ur-bootstrap-admission-qualification-20260928.md):
all 588 root and 183 descendant executions pass on the frozen composed graph,
with six causal families in normal/race modes. Its retained refusals remain
separate from accepted evidence; every live chain/service gate stays open.
The following earlier receipt covers v1.

Deterministic tests cover real command dispatch and child custody owners,
original signature preservation, interrupted claim/child/publication boundaries,
lost output, exclusive ownership, missing completed journals, stale transitive
inputs, duplicate/root-conflicting roles and resealed trim selection. The
byte-based root-plan regression replaces its pathname between read and decode
and checks both the pinned result and the distinct reopened control. Sol medium's
[qualification receipt](evidence/bootstrap-chain-qualification-20260928.md)
records 369 full normal roots, all 87 adjacent race roots across six disjoint
shards, and four passing fixed controls with four intended mutant failures. It
retains the original aggregate race timeout and identifies the actual tested
dependency graph. These synthetic local results do not qualify a different
composed release or supply any live launch gate.
