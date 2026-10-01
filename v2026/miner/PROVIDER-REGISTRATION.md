# Durable provider client registration

`provider provide` and `provider auth-provide` retain an immutable versioned
client operation before sending it to `/network/register-client-v1`. If the
server commits and its reply is lost, restart replays the original request and
server identity. It cannot fall back to `/network/auth-client`, rotate a key or
invent a validator identity to get past an error.

This is local provider client custody. It does not prove miner admission,
processed client-key registration, validator membership, native finality,
contract authority, account funding or an actual fee. API authentication still
comes from the configured server and its network principal.

## Operator decisions

- A known new installation or new proxy member requires
  `--allow-client-registration`. New key creation also requires no retained
  provider history. This flag never clears an operation, first-send anchor,
  existing-client marker, rejection or key-identity mismatch.
- An ordinary restart with retained key/marker and operation or credential
  needs no creation flag. A persisted request can resume without the flag even
  if it has not received its first reply.
- An older installation with an unmarked `.provider.key` and legacy client
  JWTs requires the separate `--adopt-legacy-provider-key` first-upgrade
  assertion. The operator must have recovered and identified the original seed.
  JWT claims cannot establish its historical link to that key. This option is
  mutually exclusive with new-client permission, grants no new allocation, and
  first-upgrade adoption permits only existing-client refresh. Publishing a
  missing marker accepts only legacy JWT names and legacy certificate/extender
  side files. In that missing-marker case, any retained versioned operation,
  lock, first-send/existing marker, rejection or unknown provider history makes
  this a recovery case instead of an automatic upgrade.
- Missing or unsafe key material, a key/marker mismatch, or a missing key marker
  with retained versioned history requires custody recovery. Neither flag
  repairs it. A bare existing seed with no provider history may finish the
  seed-before-marker crash window using those exact bytes.

Keep the key, identity marker, per-slot credentials and all registration side
files together in a private backup. Complete deletion of all local history is
indistinguishable from a fresh directory. Losing every trace for one proxy slot
is likewise indistinguishable locally from a genuinely new slot. Explicit
creation is an operator assertion about new work, not proof that no earlier
allocation exists. Never use it as an automatic lost-key recovery policy.

## Scope and ordering

The canonical scope has `client_role=provider-v1`, the exact endpoint returned
by the SDK, the Ed25519 public key derived from the retained 32-byte seed, and
`client_slot=direct` or `proxy-sha256:` plus the full SHA256 of the effective
proxy `Network + NUL + Address`. The SDK trims a trailing slash and validates
the endpoint; it does not normalize host case or default ports. Credentials,
proxy iteration order, executable version, paths and serving policy do not
select identity. The registration description is the fixed string `provider`.
All chain, deployment, netuid, validator and operator fields are empty/zero.
Historical validator scope encodings omit the additive role/slot fields and
retain their original bytes.

The key owner locks `.provider.key.registration.lock` across all provider
workers. It publishes and fsyncs `.provider.key`, then `.provider.key.identity`,
before any API allocation. Each independent slot retains its existing JWT
filename, plus `.registration`, `.registration.started`, and when adopting an
existing token, `.registration.existing`. The existing 8-byte proxy filename
suffix is only a path: the full scope digest is authoritative for local replay.
Duplicate effective slots and duplicate credential paths are refused before
workers start. A changed scope at an existing path is refused.

The versioned loader fsyncs the original request and independent first-send
anchor before POST. It validates the response against that request and original
principal, retains the exact client/device binding, then installs the token.
The server's qualified versioned protocol serializes allocation and enforces
per-network operation/scope uniqueness. Legacy allocations do not have that
server registration row; a lost legacy reply cannot be reconstructed here.

The shared key owner remains held until every device and API callback joins.
Registration stores are opened relative to that owner's directory descriptor;
bootstrap/rejection reads and refresh/logout credential writes use the same
physical custody. Renaming or replacing the state directory stops new work
before it can select replacement files. A completed response cannot write into
a substituted directory. Provider revocation retains a sticky `blocked` marker;
fresh network login does not erase this recovery requirement.
Device setup receives the retained seed and must return the same seed. Its
best-effort certificate/extender writes cannot overwrite the critical client
key. File ownership, private modes, no-follow opens, link refusal and directory
identity checks come from the durable registration store. The supported store
implementation is Linux/Darwin; unsupported platforms fail closed. These checks
do not claim protection from a malicious local owner or complete filesystem
rollback. A directory census is bounded at 4096 entries/members.

Each API attempt has a 300-second ceiling. Only an entirely typed transient
availability cause tree can retry, at one-second intervals under the service
context, with the same retained operation. Completed refusals, unsupported
routes, malformed replies, changed identity and local custody errors stop that
worker. Cancellation retains the error and joins its owners. The CLI preserves
its prior daemon-completion exit policy; the internal runner returns the cause.
Admission failures offer a closed `startup_recovery_required` message with flag
and custody guidance before the existing bounded diagnostic drain. Unavailable
output remains best effort and never holds authentication or shutdown.
Live refresh checks stable principal, roles and client/device identity before
persisting. SDK integrity notices cancel only their still-current credential
generation, including protection against stale equal-byte replacement logins.

## Qualification and remaining scope

Deterministic fixtures use private temporary files, real local HTTP and the real
SDK/daemon authentication path. Barriers cover seed fsync, request/binding
publication, committed lost replies, restart, proxy membership and joined key
ownership. Daemon fixtures stop at the authenticated handoff before constructing
a full serving device; refresh/logout callbacks are exercised separately. No
fixture establishes live API or chain authority. Independent
normal/race and causal qualification must name the exact source and physical
module graph; compiling this candidate alone is not behavioral qualification.

The active no-config measurement validator, nonproduction legacy release branch,
swarm credential consumption and simulation orchestrator are separate consumers.
This provider change does not migrate or qualify them. In particular,
`validator/run.go` still has its legacy allocation seam; the schema3 configured
production validator already uses the separately qualified durable owner.

The physical build must include the versioned registration/integrity SDK APIs.
The reviewed graph pins SDK `516521fb16da46c9f4bff0b58221e1941694f616` and Connect
`b163f9dd9ac374942fe97331f26631248a9c1f81`. SDK `42241118` lacks those interfaces.
No server wire change is introduced by this provider adapter.
