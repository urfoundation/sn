# PF-02 taskworker profile handoff

The strict resume failed on four fresh process-log classes: error and warning
for each of the two operator taskworkers. Each worker recorded two validated
geolocation certificate rotations and one retail payout notification without
user authentication. The emitting layer was the ordinary server background
taskworker; these were neither stale log generations nor native-chain errors.
No log-classifier rule or task severity/result has been relaxed.

Private authoritative evidence is retained by Terra at
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r2/strict-resume-failure-evidence`.
Use its safe projection for reports; do not publish the raw log slices, identity
values, runtime configs or private receipts.

## Frozen inputs

- SN base `0fd7ffc0f6aaad5a2988427c4acb47e1f79c3819`, candidate
  `2ee84e98c29f7ab03b15c8166c2cee3caf450052`.
- Server exact physical-release base
  `0f095a639e111f71d231cd6f792a191cbc6b0a69`, candidate
  `e212069e44b748cf9d8b1c5dfc4abf00129acb33`.
- Both checkouts are clean under `workspace/`. Ten unchanged sibling
  dependencies are links to the existing physical workspace; server is the
  eleventh isolated SN sibling. `tmp/` and `gotmp/` exist beside `workspace/`.
- `server.patch`, `sn.patch` and both `*.changed-paths.sha256` files bind the
  complete unformatted proposals. Astra ran only source inspection and Git
  whitespace checks, no formatter, compiler or test.

## Scope and behavior

Server's ordinary default keeps its existing scheduling, registry, unknown
target retries and claim SQL. Explicit `subnet-operator` uses 49 existing task
implementations. `taskworker/WORKLOADS.md` records the complete dependency
rationale, including all five ST tasks and all four `/verify` tasks. Scheduling
and admission share one definition list. Canonical targets retain production
post hooks and legacy aliases.

The queue predicate runs before the candidate limit and recognizes versioned
names. It also follows a `RunPost` wrapper to its finished original task, using
the finished-task UUID primary key. Invalid JSON, invalid IDs and orphaned or
excluded wrappers cannot consume the scoped worker's candidate window.
Excluded pending rows remain unmodified, including API/controller enqueues
after startup. Ordinary task retention/maintenance remains active.

SN persists the explicit profile in both taskworker `ProcessSpec`s, which the
supervisor retains for restarts. Its internal taskworker command requires that
profile and rejects a missing/unknown one before environment access. API and
Connect commands reject a misplaced workload flag.

Provider probing is already disabled by authenticated renderer output in
`renderOperatorProviderEgressProbeIsolation`; synthetic location metadata comes
from `operatorSimulationSiteSettings`. Public geolocation pins serve the
independent operator-proxy prober, not `/verify`. Verification proxy egress
refresh, event indexing, API artifact publication/history, and operator
transaction reconciliation remain active. In particular, the profile does not
discard or reclassify the failed startup's operator EVM write records.

## Qualification

`affected-test-selection.json` gives exact selectors and expected root names:
8 server/task, 13 server/taskworker, 9 SN/sim-testnet roots, all expected PASS in
normal and race modes. These include actual queue scheduling/claim/dispatch,
retained queue rows larger than the candidate window, malformed/deferred posts,
legacy aliases, production defaults, lease/drain controls, both operator specs,
restart serialization and the unchanged strict log classifier.

The normal-only causal plan has three source variants/four package bodies,
12 selected roots total: 7 expected FAIL and 5 expected PASS. No causal body
executes a retail payment, public pin refresh, live native action or external
probe. Astra will materialize clean variants from Terra's formatted descendants
before causal compilation. Restoring unrestricted scheduling/registration is
tested only through registry and enqueue checks, never by running those jobs.

Terra owns all formatting, compilers, private database/test services and bodies.
No full producer gate, runtime/ABI review, chain qualification or unrelated
recovery-ledger tests are required by this change. Root owns review,
cross-repository integration, refreshed release identity, and native recovery.
