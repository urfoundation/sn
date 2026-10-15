# Validator trail diagnostic output candidate

The source and fixture-only cleanup correction are now integrated and
[component qualified](trail-diagnostic-output-qualification-20260928.md).
That receipt records the retained original failure, passing affected scopes,
five causal controls and operational limits. The text below describes the
original source candidate and its author-side checks.

This source is isolated from the frozen root-output qualification and from the
blocked contract/provider-payment work. Its base is root
`5950a8be9d964d0e66b97e84b8a0a2408b368966`, which already contains the qualified
bounded diagnostic owner and unchanged five-domain producer extension.

Actual `TrailEngine.Run` completion and ordinary failure messages, its hard
custody failure notice, and the verified-signature anomaly now enqueue fixed
scalar records through the existing release instance's `runtime` domain. A full,
disconnected or refused optional log destination cannot hold trail workers or
their cancellation. Required ledger and proof writes still determine progress;
the original hard error still cancels all trail workers and is returned. These
local observations neither authorize another operation nor attest to native
weights, contract settlement, independent proof acceptance or Loki delivery.

The real release worker binds its admitted numeric operator identity. A small
context-copy helper preserves a precreated registration child's cancellation and
deadline while copying only diagnostic routing and operator identity. The
registration author must compose
`copyTrailDiagnosticContext(self.trailContext, ctx)` at the actual child Run call.
That integration is separate until its exact composed source is qualified.

Trail records contain only closed outcome/kind/cause, numeric operator id, proof
depth and settlement epoch with explicit knownness. Epoch zero remains known
when a ledger or configured epoch source owns it; no epoch source stays unknown.
Records never include a trail/peer id, signature, raw Error/String result or
native epoch inferred from proof work. No worker labels or new producer domains
are introduced. The public 8 KiB protocol wire and its existing consumer-first
rollout requirement are unchanged; no additional schema migration is needed.
All trail workers in one release instance share the already-bounded runtime
queue. A noisy worker may cause optional runtime records to be dropped; the five
existing per-domain delivery/drop counters expose that independently of durable
measurement. A caller without the release owner has no diagnostic delivery.

Deterministic tests use actual signed M8 trails, original ledger/proof files,
operator worker construction, typed failures, and owned completion barriers.
A child process receives physically full stdout before it starts; its completion
barrier must arrive before the parent drains that pipe. The child retains one
original proof, reaches its next picker, cancels and joins, with unchanged stdout
flags. Other controls cover disconnected output, durable append/projection
failures, unknown versus known-zero epoch, two operator bindings, closed scalar
formatting, and the registration child context-copy seam. Astra authors and
compile-checks these tests; Terra owns normal/race bodies and causal controls.
No passing body, deployment, alert delivery or live identity is claimed here.

Adjacent review found synchronous miner `provideWithProxy` authentication retry,
JWT/key persistence and status callbacks. They remain an explicit next component;
this slice does not claim to bound miner output. Legacy nonproduction steering
prints are outside the selected mainnet path. Finite CLI output remains unchanged.
