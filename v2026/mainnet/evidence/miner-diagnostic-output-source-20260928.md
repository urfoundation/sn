# Miner callback diagnostic output source

The long-lived `provider provide` owner uses the existing bounded diagnostic
exporter for authentication, key persistence notices, extender observations and
runtime/status notices. Four fixed domains share only this run's explicit
budget: two records per domain, a 16 KiB exporter record ceiling, one owned writer
and bounded write/retry/close behavior. Actual miner records contain only closed
strings, booleans and numeric provider ordinals and fit below 1 KiB. Ordinals
identify children within this run; they are not persistent proxy identities.
Credentials, seeds, public identities, routes and raw errors are not log fields.

The real refresh callback still writes the replacement token before returning;
a file failure retains its original cause and requests the same cancellation.
Logout still removes the credential and records the rejection marker before
cancellation. Key persistence keeps its existing nonfatal behavior. A successful
key derivation is called `identity_available`, separately from key-save outcomes;
it is not a persistence or network-acceptance assertion. Unknown extender input
remains different from a known disabled extender. Repeated scalar observations
coalesce without retaining arbitrary SDK error or address strings.

The provide run owns its status listener, provider workers and output owner. A
status bind refusal precedes launching provider children. Worker completion and
cancellation close handler admission, interrupt HTTP I/O and join Serve plus
every previously admitted Status handler before the output owner closes. Late
requests do not enter Status. Panic recovery preserves both the original error
and later cleanup causes. Normal command return preserves the previous
zero completion exit while permitting deferred signal cleanup. Finite CLI setup,
authentication, wallet, claim and proxy commands retain their existing behavior.
The provider worker's original errors remain available at the owned run boundary;
optional publication does not decide custody or recovery policy.

When the existing status endpoint is enabled, `diagnostics` is an optional object
with schema `urnetwork-provider-diagnostics-v1` and the four fixed domain delivery
snapshots. `status: "ok"` continues to mean process liveness only. Absent output
ownership omits the extension; refused destinations report unavailable without
inventing an attempted write. Drops and last successful sink writes refer to this
process instance, not an alert receipt or protocol progress. The current Warp
status sampler accepts additive JSON fields; its unchanged real sampler has a
separate reader compatibility fixture. Strict unknown-field consumers must add
this optional extension before producer rollout. No source here installs a
collector, alert rule, service or external notification.

New regressions exercise real SDK refresh/logout dispatch, token and rejection
files, key file codecs, actual run early returns/authentication waits, HTTP status
ownership, disconnected output, and a child whose physical stdout is full before
it starts. The parent waits for completion before draining that pipe. The child
uses the actual provide run owner directly with owned endpoints and state; finite
CLI parsing/proxy-file discovery is not part of this physical test. No test loads
live identities or invokes financial actions. Old-path controls restore blocking
callback output, missing output closure and raw formatting independently.

Terra normal/race/control qualification has passed; the
[qualification receipt](miner-diagnostic-output-qualification-20260928.md)
records exact scope and retained evidence. Arbitrary diagnostic error methods
have a separate pending correction. SDK, Connect and other internal logging
remain a separate boundary: this change does
not claim every library callback or device close is isolated from every sink.
Existing provider key-file admission/durability policies and fresh authentication
protocols are unchanged. Miner protocol readiness, durable service-progress
observation, metrics/alert installation and live acceptance remain open scopes.
