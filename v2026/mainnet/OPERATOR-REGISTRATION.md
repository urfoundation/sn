# Durable operator registration and independent native recovery

This source candidate removes first-client authentication from the retained
native observation prerequisite. It has not been deployed or independently
qualified. Existing signed activation, server-key/public-object history, private
disk and intent ownership remain required before `RunRelease` starts workers.
The real operator upload and measurement owners exist before authentication;
their credential getters refuse API work until the durable credential handoff.
Receipt, exact-boundary nonce and application observation can therefore continue
while an operator waits for its original client identity. Initial publication,
trails, fresh intent preparation/signing and native rebroadcast remain gated.

`operators[].allow_client_registration` is a separately approved schema-3
configuration choice. Its default is false, and omitted/false uses the exact
older JSON/YAML encoding so historical complete-configuration hashes do not
change. True permits creation of a new versioned operation; it never resets an
existing operation or revives a revoked client. A known credential refreshes
normally, and an existing durable versioned operation replays even when the
flag is false. Empty local native history is not proof that a legacy process
never allocated a client. Missing legacy credentials with no versioned request
remain an explicit recovery wait; the program does not guess a client ID.

Before any creation POST, the private credential owner durably writes both the
opaque request and an independent first-send anchor beside `client.jwt`.
Both use bounded descriptor-relative, no-follow custody, an exclusive file
lock, atomic rename and directory fsync. The request binds the exact endpoint,
deployment/genesis/netuid, validator/operator identity, public client key and
creation payload. Stable authenticated principal/network/user/roles are retained
separately. Bearer bytes, token timestamps, executable hashes and unrelated
configuration changes cannot select a new request. Missing post-send request
custody refuses creation. A server-bound identity is saved before its credential;
crash recovery repeats the original operation if that handoff was interrupted.

Only `POST /network/register-client-v1` implements this contract. An old server
that ignores new fields on `/network/auth-client` cannot accidentally turn a
retry into another allocation. Unsupported capability is an explicit wait, with
no legacy fallback. The legacy endpoint remains unchanged for older clients.

The new `network_client_registration` migration stores network/request and
network/scope uniqueness, canonical request and stable principal hashes, and the
server-issued client/device IDs. Allocation and the binding commit in the same
transaction under a network-scoped PostgreSQL advisory lock. Read-committed
lookup after lock acquisition observes a concurrent predecessor's commit.
Exact replay checks active client/device ownership and roles before signing a
renewed credential. Revocation/deletion leaves a binding tombstone. Another
request ID for the same stable scope conflicts, including when all local
request files were lost; it does not allocate another client or adopt a guessed
legacy identity. Conflicts, revocations and changed principal/payload stay
distinct from a transient missing reply.

Each registration operation has a 300-second outer ceiling; the configured
HTTP transport retains its 120-second request and 45-second connection bounds.
The original exact request is the only mutation eligible for replay. Exhausted
or unavailable outcomes leave the service running with a closed authentication
wait diagnostic. Unsupported capability and required identity recovery remain
observable waits. Complete malformed/mixed remote replies latch that API owner
in an explicit hard recovery-required state: no automatic retry, replacement,
publication, or new signing. Its already authenticated native receipt observer
continues. Shared ledger/intent corruption and private descriptor/custody errors
remain shared hard failures. No generic read retry wraps legacy allocation. The cumulative Connect transport
retains all observed failure leaves at exhausted HTTP/WS/H1 exits; unknown/hard
causes cannot disappear into a generic timeout or a selected status leaf.

The shared SDK refresh decoder now rejects null, duplicate-field, case-fold ownership/error aliases, partial and
mixed success/error replies before startup or the background token manager can
use them. Existing SDK client/device identity validation still owns token
publication. SN additionally preserves stable principal and role identity.
The SDK reports an invalid completed live refresh to its captured original
credential owner without publishing a new token or inventing revocation. Its
optional close action checks the original token/generation atomically at the
actual effect boundary, so even an equal-byte replacement during callback
delivery invalidates a stale notice. SN latches API readiness off and stops that
operator's API/transport workers; receipt recovery remains independent.
Only an all-leaf typed unavailable refresh may return the already retained
credential as usable on the legacy startup path; a malformed response remains
an error. Production requires an actual successful refresh for API readiness,
so even an old revocation whose marker failed to persist cannot restore readiness
during an outage. Native observation continues while that refresh is unavailable. A
mixed HTTP401 plus another failure is not confirmed revocation. Legitimate
token renewal may change timestamps/expiry while retaining identity. API upload
getters expose only SN's durably saved credential, including during refresh.
Live revocation or failed refresh persistence withdraws readiness and cancels
the operator-owned trail context. The real trail workers join independently;
mixed cancellation and a durable proof failure remain hard. A failed rejection
marker write emits a closed diagnostic and never re-enables the old session.

The outer native operation validates immutable configured upload/source ownership
separately from live destination readiness. Qualification of the initial candidate
found that rebuilding active upload writers before every `SubmitOnce` made a
correct API withdrawal cancel receipt recovery before it could read the retained
intent. The successor preserves exact route, bounds, concrete writer, activation
and source-key checks there; publication builders and already obtained callbacks
still reject a closed session. Native receipt/nonce/application observation does
not need active upload credentials. Its public-root fixtures order the native
response after a copied real withdrawal transition; optional lossy diagnostics
remain separately asserted and do not authorize or release native work.

An already present legacy client receives a durable `.registration.existing`
identity marker before refresh. Losing that token later cannot consume the
fresh-create flag or mint another client; explicit identity recovery is required.
The marker carries stable scope/principal/client/device, never bearer bytes or
token expiry. It does not fabricate a server-side registration binding.

Roll out in this order:

1. Qualify the exact server/SDK/SN candidate and its physical module graph,
   including PostgreSQL normal/race tests and the actual public startup cases.
2. Apply the additive server migration and deploy the versioned route to every
   approved operator API. Verify exact duplicate allocation, conflict and
   revocation behavior on an isolated deployment before enabling clients.
3. Deploy the compatible SDK/SN build. Existing signed configs with the field
   omitted remain valid; existing credentials need no new allocation.
4. Include explicit creation opt-in in the normal independent approval for a
   fresh mainnet operator. Preserve the request, anchor and binding during
   upgrades/recovery. Back them up as private custody with the client token.

No migration, registration, credential or chain operation was performed by
authoring this candidate. Source qualification does not supply a live endpoint,
production approval or mainnet economic acceptance. See the
[candidate qualification handoff](evidence/operator-registration-candidate-20260928.md)
for exact source and test scope.

Subsequent component qualification is recorded separately: the
[server transaction and complete model execution](evidence/registration-server-model-qualification-20260928.md),
[withdrawal/native recovery](evidence/operator-withdrawal-ownership-qualification-20260928.md),
[SDK/Connect transport](evidence/registration-request-transport-qualification-20260928.md)
and [actual production API/database path](evidence/registration-production-api-qualification-20260928.md).
The retained receipts disclose original failures, seven optional full-model
skips and exact dependency graphs. Final combined SN consumer checks and the
server-first rollout remain distinct from these passing component bodies.
