# `sim-testnet` release 1.0 harness

`sim-testnet` is the only supported release-1.0 testnet installer and integration
test. It converges an **existing** Bittensor testnet subnet, deploys the reviewed
reserve/vault/coordinator contract set, provisions two operators, 1,000 real
miner identities in 20 production swarms, two validators, 202 independently
keyed four-client head-candidate fleets, and 192 long-tail miners, then leaves
the topology running for inspection and named scenarios. Exactly 200 of the 202
candidate fleets receive native head slots. Each fleet stays within one operator
and whole fleets are balanced across
operators, so the affiliated-validator self-dealing mask leaves an independent
head and pool instead of contaminating every head UID.

It never creates a subnet. Every write is bounded by an approved, content-hashed
plan. `doctor`, `plan`, `status`, `inspect` and `analyze` are read-only. `setup`,
`launch`, `resume`, `scenario` and `retire` are dry-runs unless both `--apply`
and the exact `--plan-hash` are supplied.

## Agent execution policy

New qualification tooling, status/report processing and its tests are written
in Go. Keep legacy process-ownership adapters only until their Go replacement
passes the same deterministic cancellation, escaped-child, lost-completion and
ACK/join controls; language migration must not weaken lifecycle guarantees.

Release qualification assigns test and gate execution to `gpt-5.6-terra` with
reasoning effort `max`. If a test or gate fails, retain its exact output and
assign root-cause diagnosis, adjacent-path review, implementation, and the
deterministic regression to `gpt-6-astra` with reasoning effort `max`. Terra
then reruns the affected test matrix. Run the two complete gate workloads
concurrently with independent fixes, using immutable source and private mutable
resources for each admitted job. A failed release preflight is retained as a
refusal, not counted as executed tests or a release-qualified pass.

After diagnosis and correction, each failed test must pass three consecutive
uncached executions on the same source/dependency snapshot in every mode that
failed. Use a fresh process for each execution and retain its exact root, mode,
binary/source identity, raw output, actual exit and cleanup result. A failure,
timeout, skip, missing result or changed input resets the affected streak;
results from different binaries or revisions cannot be combined. Independent
roots may run concurrently, but each root's three confirmations are sequential.
Retain the original failure, add deterministic root-cause and adjacent controls,
and rerun affected integration coverage. Three later passes do not retroactively
pass a failed full gate: final acceptance still requires both complete gates
on the final candidate. This division does not authorize a simulator write;
the normal plan-hash and `--apply` boundaries still control testnet mutations.

If an assigned agent loses execution capacity, inspect any already-started
host process through its PID and output before deciding it stopped. A missing
observer session is not a terminal test result. Preserve the real terminal
record and wait for the assigned model to become available or an explicit
user-approved reassignment; an agent error does not authorize a silent model
substitution, duplicate test process, or bypassed gate.

For local qualification captures, freeze the runner before launch and invoke
`bash scripts/run-qualification-capture.sh SOURCE_ROOT SOURCE_MANIFEST FROZEN_RUNNER`
with three absolute paths. The wrapper checks relative source inventory entries
from the declared source root and explicitly runs the frozen body with Bash;
it does not rely on the execute bit of a newly written capture script. The body
still owns separate compile/list/execute budgets, exact selector/census checks,
immediate source/runtime/external fences, and raw exit/index capture. Include
the wrapper's bytes in the execution provenance. Never edit a live runner or
change a running capture to this wrapper. Preserve failed prelaunch records as
observer failures, not product test failures or qualifying passes.

Preflight all selector inputs together before compiling: root lists must be
sorted uniquely with `LC_ALL=C`, contain no blank records, and end in one newline;
anchored selectors and expected-outcome tables must describe exactly the same
roots. Keep expected causal assertion text unchanged. If historical input files
need a canonical projection, preserve the originals and record the transformation
before launch; do not drop tests or replace a failed product run with a formatting
retry. Current source locations and qualified scopes are recorded in
[`FINALIZE-COMPLETE.md`](../FINALIZE-COMPLETE.md#12-freeze-and-execution-record).

Check the compiled list before admitting either test mode: its exact root names
must equal the declared selection, and a required selection must not be empty.
Preserve the selector literally; adding a trailing `$` to a prefix selector
changes its meaning. For a package-main build, supply `go build -o` with an
explicit private output file, never the package directory. Check all manifest
paths and portable modes before starting the expensive matrix. A preflight
failure stops that capture before test execution; an exit-zero empty selection
is not a pass. Also check signed fixture lifetime record/trail capacity before
adding work to a migrated history; migration does not reset those limits.

Preflight the exact outer invocation as well as the frozen command it calls.
Prefer the reusable Go qualification runner or an already qualified compiler
owner. A direct bounded `go test -c` in a verified private snapshot is also
valid when command/exit and before/after source, mode and binary receipts are
captured separately. Do not introduce bespoke shell functions or pipelines
as a prerequisite to starting each compiler. For a retained shell owner, use
a stable literal `bash /absolute/compiler.command` with an explicit package
working directory and `login:false`. Preserve the
tool's actual output and terminal exit; label a combined stdout/stderr stream
as combined. If separate on-disk streams are required, their redirections
belong to a frozen, syntax-checked capture owner, not a newly retyped inline
wrapper. An unmatched outer quote is a pre-body launcher refusal even when
the nested compiler command passed syntax checks. Correct only the refused
launch; never restart an already-live peer compilation or test body.

An isolated Go copy needs module metadata for every configured local repository
and recursively selected local replacement/nested module, including replacements
not selected by the current package graph. Include existing `go.mod`, `go.sum`,
`go.work` and `go.work.sum` files together. Classify absolute generated test-link
files reported by `go list` separately; do not prefix a build-cache pathname
with a repository directory. Before a body starts, validate the whole copied
module graph once rather than discovering missing replacements one at a time.
Scope a source lease to its actual consumers: server-only compilation does not
depend on an unrelated simulator test/formatting file. Preserve the earlier
refusal and correct its exact prerequisite without restarting live peers.

For repeated offline selections on the same frozen source, compile one test
binary per package and mode (`go test -c`, with `-race` for the race binary),
using an explicit private output and the existing separate compilation budget.
Record the command, dependency/source identity, binary hash and actual exit.
Run that exact binary from its package directory with the unchanged
`-test.run`, `-test.timeout`, `-test.parallel`, `-test.count` and verbose settings.
Record compilation and test execution separately; repeated concurrent builds
must not consume a selection's execution allowance. A source/dependency change
requires a new binary. This development workflow does not replace or change
either checked-in complete release-gate invocation.

Measure native body duration at its actual start and finish. Offline
`go tool test2json -t` timestamps and its package-level elapsed value measure
conversion, not the earlier test execution. Individual test elapsed records
can describe that test; the final test's duration is not the suite duration.

### Compact Go qualification workflow

The reusable Go implementation is `scripts/qualification`. Build it once to a
private output outside the source tree:

```sh
go build -o /absolute/capture-tools/qualification ./scripts/qualification
/absolute/capture-tools/qualification run /absolute/matrix.json /absolute/new-capture
/absolute/capture-tools/qualification status /absolute/new-capture
```

The version1 JSON plan declares `source_root`, `sources` (physical `root` and
SHA256 `manifest` per source/dependency), `limits`, `packages` and `suites`.
Set `source_root` to the actual SN Git worktree, not the enclosing multi-repo
workspace: this is also the owner of `scripts/release-gate-jobs.sh`. Declare
each required sibling worktree separately in `sources`, with manifest paths
relative to that worktree. A workspace-wide manifest cannot be reinterpreted
as an SN-relative manifest; preserve and verify any per-repo projection.
Every package specifies `id`, physical `directory` and exact `import_path`.
Every suite specifies `id`, `package`, `mode` (`normal` or `race`), `outcomes`
and `failure_literals`. These last two fields are absolute paths to canonical,
sorted TSV files: `TestName<TAB>PASS|FAIL`, and one identity-owned literal per
expected failure. There are no implicit selectors, skipped roots or default
budgets. Limits explicitly name `jobs`, `build_seconds`, `test_seconds`,
`outer_seconds`, `parallel` and `gomaxprocs`; use the admitted existing values.
An empty matrix, duplicate roots within a package/mode, malformed metadata or
unlisted dirty/new source fails preflight. Represent overlapping obligations
with one exact root union and a retained obligation map, not duplicate runs.

Each package/mode is compiled once. Suites become ready after their own build,
not after unrelated builds. The compiled list must match the exact declared
roots before execution. The Go event verifier requires complete package/root
transitions and attributes each expected failure literal to that identity only.
Existing subtests must be explicitly declared with their complete slash identity
and every ancestor; they are counted separately from top-level roots. Missing,
foreign or unfinished descendants are refused, not filtered from the evidence.
Actual command and owner exits, binary hashes, source hashes/modes/Git state,
module graph, full stdout/stderr and immutable requests remain in the capture.
The current implementation uses the qualified gate ownership adapters to join
descendants; composing those adapters is a prerequisite, not an optional
fallback to unowned subprocesses. It does not run either full release gate or
launch a live campaign by itself.

To repair a checker-only refusal, the same Go tool can replay the immutable
original inputs without another test or converter run:

```sh
/absolute/capture-tools/qualification replay EVENTS OUTCOMES LITERALS PACKAGE BODY_EXIT
```

Retain the original refusal and capture the replay's source, binary and input
hashes separately. An accepted replay verifies that old body; it does not turn
the old source into a new qualification run or clear unrelated body failures.

Use `status.json` for routine progress. Read `report.json` for verified outcomes
and `failures.json` plus the referenced requests/logs for debugging. Passing
updates should contain only the phase, exact counts, elapsed time, actual exits,
integrity verdict and capture path. Do not repeatedly send passing raw logs,
long hash inventories or the full historical handoff to an agent. Retain all
raw evidence on disk and expand any failed or suspicious result for Astra max;
compact reporting never means ignoring an anomaly or capping its investigation.
Do not retry a failure blindly or declare a timeout an expected assertion
failure. After its root cause is resolved, perform the required three-pass
confirmation above; any recurrence returns to diagnosis and resets that
root's streak. Keep a short active-work index linking to detailed history.

Validate the exact filenames and invocation consumed by the frozen body, not
only a staging convention: a package-prefixed `sim-testnet.expected.txt` does
not satisfy a runner that opens `expected.txt`. Any adapter must explicitly
bind and check that mapping before build, along with selector, outcome and
root-bound assertion files. Keep package working directory, Go build target
and artifact label distinct (`server` root builds `.`). Parallel causal output
must bind each assertion to its actual test identity, not an overlapping
`RUN`-to-`FAIL` text interval. A failure before build/list/test is a preserved
launcher failure; only a fresh reviewed capture can follow its correction.

Both release gates export `WARP_TEST_ENV_FAIL_FAST=1` before running tests.
This makes the server's default test environment fail on its first assertion,
fatal error, or panic instead of accepting a later successful retry. Abandoned
environment teardown is also a release failure, even if its callback passed.
Invalid attempt counts, failure followed by skip, nonreturning lifecycle
callbacks, and late or cleanup assertions cannot turn a failure into a pass. Unset or
`0` keeps development defaults; malformed settings fail closed. Explicit retry
state-machine meta-tests retain their deliberately selected retry policy. Keep
the complete first-failure log even when a later independent run passes.

## Host concurrency

Maintain one current integration candidate under `temp/sn-*/sn`, with a single
integration owner. Move qualified method changes into that candidate while
preserving newer primary fixes; a passing older snapshot is not a complete
replacement for current source. The next release milestone is the real
startup-to-submission path on this combined candidate, not the number of
isolated suites that pass. Independent source fixes and causal controls may
retain their own exact preimages, but do not create another source checkout
for a corrected selector, output filename, or report.

During implementation, keep two Astra max implementation/fix agents and one
Terra max execution agent. Once independent integration suites are ready,
use two Terra max execution agents and one Astra max production/fix agent;
the primary agent takes the second review role. The user explicitly requested
this integration dispatch on2026-09-09. Split validator/simulator qualification
from fixture/database/Solidity qualification. An execution agent's command
preparation queue is not a dependency for another lane. Routine test-fixture
or launcher repairs must not preempt the production lane. The primary agent
owns integration, source boundaries and review while execution continues.

The Terra agent drives multiple independent isolated build/test/gate jobs
concurrently; one agent is not a one-process or one-core execution limit.
Transfer every live handle, command, source/binary fence, capture and remaining
obligation when changing owners. Join or explicitly transfer already-running
processes before retiring a lane; do not restart work because its agent changed.
Freeze the candidate during each admitted run; prepare the next delta outside
that source and apply it only after every reader is joined.
Reuse unchanged dependency checkouts and Go build/module caches, recording
their exact identities, while keeping runtime state and generated outputs
private. Source changes invalidate affected results; packaging a completed
result does not hold the next ready command.

The integration dispatch supersedes the earlier four-small-body development
reservation. On the measured24-logical-CPU/12-physical-core/125GiB host, admit up to eight small bodies
and two compilers across both workers, while the total reserved processor
allowance remains at most24 across both gates and component jobs together,
and measured memory keeps headroom for services. At2026-09-09 06:49UTC the
host had a one-minute load of10.72 and about113GiB of available memory; the
earlier20-CPU cap was holding a ready validator job behind unused capacity.
The admitted overlap is gates16 + population2 + storage2 + validator4. A
preparation queue owns no processor reservation. Reassess pressure before
admitting successors; do not exceed the measured logical-processor allowance.
Charge each job's actual worker allowance, including database/static jobs;
do not change a running job or its original per-test worker/deadline limits.
The first unmeasured full-census metadata stress runs in one heavy slot until
its peak memory is known; independent small jobs continue. Both complete gates
retain their existing per-job resource admission and count toward that total.
Each worker records its active reservations and transfers unused capacity;
one worker must not reserve the whole host while preparing a later command.

For development qualification, measured heavy independent roots may run in
separate exact-membership shards. Prove the disjoint union equals the original
selection, preserve every assertion and per-process limit, and label the
result as a selected union. This never replaces either complete final release
gate. Prepare evidence locators and the final-report checklist concurrently;
independent analysis of closed captures may overlap later live windows, but
only the completed verification can support `FINAL.md` success claims.

For every job, ask **can this run concurrently with the work already running?**
Concurrency is the default, including preparation, source review, formatting,
compilation, ordinary/race qualification, independent repairs, read-only RPC
checks, and evidence analysis. A queue position or an unfinished unrelated
report is not a dependency. Record each held job's exact prerequisite or
exclusive resource, its owner, and the condition that releases it; reassess
that hold whenever a job starts, finishes, fails, or releases source.

- Start independent ordinary/race, causal/repaired, and package suites on
  immutable inputs together. Review may overlap testing; promotion requires
  both. Only a job's own build/list/admission must precede its execution.
  A reproduced failure holds affected promotion, not unrelated offline work.
- Freeze source and launchers per job, not across the entire work queue.
  Use private state, ports, logs, and artifact paths. Keep temporary checkouts
  under `temp/sn-*/`; verify every relative Go module replacement and its
  actual dependency identity before building. Moving a checkout can change
  its dependency graph even when every tracked source byte is unchanged.
- Capture each job's real command, execution handle, raw output, immediate
  input fences, and terminal exit independently. Launch the next ready job
  before packaging unrelated completed reports. Join every owned process;
  no detached jobs, lost failures, or final success before all required joins.
- Isolate mutable resources by default: separate disposable PostgreSQL/Redis
  instances (not proxies to the same data), daemon-assigned private ports,
  source/generation targets, Foundry output/cache directories, runtime state,
  and artifact paths. Each owner cleans up only its own identified resources
  and uses no restart policy that survives a host reboot. A lock or shared
  service queue is a fallback requiring a specific reason, not the default.
  Signer nonces and causally dependent chain transitions still require order.
  An entire gate must not wait merely because one later phase needs isolation.
- Use the host's effective cores and memory across ready jobs. Measure
  aggregate process-tree CPU, resident memory, I/O and memory pressure rather
  than one process's CPU. Keep the established per-test worker and timeout
  bounds; increase independent job concurrency while capacity is available.
  Reserve headroom for live services and timing-sensitive tests. Record
  measured pressure if it actually requires throttling; do not assume scarcity
  or create redundant work just to keep CPU busy.

Both gate scripts now compose per-gate job ownership, private PostgreSQL/Redis
profiles and separate full-build/static-analysis Foundry outputs and caches.
Keep that isolation when running them concurrently, and enable the complete
database profile with `RUN_SERVER_DB_TESTS=1`. Strict release qualification
requires the clean, pinned source checks; a diagnostic workload run must retain
any failed attestation and cannot grant release approval. Producer-gate success
still precedes a live campaign write. Partial parallel prequalification is not
a full gate certificate.

The producer's ordinary capture, private fixtures, reopened prior replay,
complete publication population and metadata census use independent admitted
jobs. Exact source guards require every selected root to have one execution
owner in each mode.
The `capture-population` job runs the unchanged 900-object, 296 MiB publisher and
public readback with its five-minute normal and ten-minute race limits. This
finite owner is separate from the ordinary roots' package timeout and keeps
their process-global allocation controls serial. The `capture-private` job runs
the real rendered-setup root and six private pending-prior/job roots together.
Their five-minute normal and ten-minute race budgets start without the ordinary
package's serial prefix. They retain independent configuration, state, signer
copies and disk stores; all durable writes and assertions are unchanged.
`capture-prior` runs the exact reopened-handoff substitution root, including
its complete semantic fixture, sealing and prior closure verification, at the
same five/ten-minute limits. Ordinary capture excludes only these eight roots
and the two original stress roots. The six process-wide allocation controls
and both original stress roots remain serial. The
`capture-metadata` job executes the exact full metadata root in both modes.
Ordinary capture keeps its five-minute normal and ten-minute race limits;
full metadata keeps its five-minute normal limit and has a separately
scoped 45-minute race limit. The unchanged complete race census took 1,839.622
seconds with a sampled peak of 32,756,132 KiB in the retained diagnostic; this
budget provides about 47% wall-time headroom. The complete aggregate simulator
race budget remains 90 minutes. No census, byte bound, hash/signature check or
production deadline changes.

The original full-metadata ten-minute race timeout remains a failure. Its
profiled 90-minute diagnostic completion is not qualification. Require three
fresh sequential unprofiled confirmations on the same source/binary under the
corrected 45-minute limit, plus affected normal/race coverage and both strict
gates; the diagnostic does not count toward that streak. The partition omits
neither stress root and does not replace a complete gate certificate.

Both gates also run the shared-boundary and distinct-boundary full client-key
history populations as separate jobs. Their combined measured race runtime
left insufficient time for the ordinary controller cases. Each exact population
keeps its ten-minute normal/race limits. These processes share only their gate's
private service containers: TestEnv assigns each a unique PostgreSQL database
and exclusive renewable Redis database lease. The parent retains the containers
until all admitted jobs have joined; no in-process parallelism is added around
the server's global test environment.

The qualified isolation components include the Linux child owner (Python3
with `pidfd_open`/`pidfd_send_signal`, kernel subreaping and `/proc`) and
`server/local/release-gate-services.sh`. The service helper creates separately
labelled PostgreSQL/Redis containers with daemon-assigned loopback ports,
tmpfs data and `restart=no`; cleanup checks immutable IDs and owner labels.
It accepts ordinary Docker access or passwordless `sudo -n docker`. It does
not use the shared local service aliases. Each private portable profile
requires both explicit escape flags and rechecks exact application,
maintenance and Redis authorities before service access. These components
and the real two-owner Docker smoke pass; full queued-gate composition remains
pending. The server test environment no longer starts an implicit shared6060
profiler; use explicit Go CPU/memory profiles when profiling is needed.

The release binary uses Go's effective `GOMAXPROCS` for CPU-bound evidence
verification and starts one bounded worker per independent signed validator
cycle, dishonest-deposit decision, lineage edge, or lifecycle decision, up to
that processor limit. Results are joined and inspected in canonical evidence
order, so scheduling cannot change the reported first failure. The live
topology also runs 20 miner-swarm processes, two validator processes, two claim
swarms, and the operator services concurrently. An individual process may show
100% while its ordered setup, nonce, or block-finality path is active; inspect
the complete supervisor process group to measure campaign-wide CPU use. Chain
mutations sharing a signer and causally ordered lineage transitions remain
serial by design.

The offline release tests separately cap Go's parallel test roots with
`-parallel=4`. Every test using the cached full 1,000-miner/202-candidate fixture
receives a detached graph and joins that bounded parallel group. Public replay
uses at most four independent TLS views at a time, retaining all 17 rejection
cases and the accepted graph. Signed bytes are immutable; maps, histories,
probes, and HTTP clients are isolated per view. A transport failure or a
different rejection reason fails the case. All workers join and report in
canonical case order; neither the test census nor the gate deadlines are
reduced to obtain a pass. This test-only scheduling does not alter live
transaction ordering or production verification limits.

The same four-worker, ordered join-all helper bounds the 38 independent
tampered-graph cases, five fleet-projection mutations, and test-fixture
supplement-file preparation. Each file
still uses the production preparation/signing path; the complete supplement
is signed only after every file worker joins. Shared identity derivation uses
the exact-key cache's detached copies. The authoritative semantic census is
[`semantic-integrity-tests.txt`](semantic-integrity-tests.txt); source/invocation
guards must prove every listed root is actually selected by both release gates.
The producer admits the exact replicated owner-completion replay and exact
fleet-audit projection replay as separate jobs. The remaining semantic job
retains the complete census check and excludes only those two roots. All three
owners retain the original 15-minute normal and 25-minute race limits, with
four parallel roots; source guards reject missing, duplicate or broadened
owners. The complete aggregate simulator keeps its existing 90-minute bound.
Deterministic worker-bound/join and callback-nonreturn regressions remain
selected. A callback that exits without returning must produce an explicit
failure and cannot exhaust the worker pool or hide a later case. All 18 public
replay cases remain mandatory; setup timing lines are not case completion
evidence.

The production artifact loader owns each distinct URI's byte stream and
computes its digest once per load operation. Every repeated reference still
checks its own claimed size and content hash, in source order. Different URIs
remain independently owned even if a loader reuses its backing buffer. The
deep-verification cache key continues to bind the exact evidence and graph
bytes; cached digests never authorize an unread or mismatched artifact.

## Pre-launch approval

The testnet inputs are stored under testnet-prefixed keys in
`../vault/main/st.yml`. Do not run `setup --apply` or `launch --apply` until
`doctor` is green and the printed plan hash and maximum spend have been reviewed.
Loading the configuration does not itself write to either chain.

Required `testnet-` keys:

| key | required value |
|---|---|
| `testnet-wallet` | A portable `vault-wallet:relative/path` to a standard encrypted Bittensor wallet directory, or the legacy signer forms `env:VARIABLE` and `file:/absolute/owner-only/path`. The coldkey is decrypted only in memory and must match `coldkeypub.txt`; its public default hotkey is also identity-checked. |
| `testnet-wallet-password` | A contained, non-symlink `vault-file:relative/path` to the encrypted wallet password. On execution hosts it must be owner-readable with no group/other permission bits (for example `chmod 600 vault/subtensor/testnet_wallet.password`). It is never accepted as a CLI flag or emitted in evidence. |
| `testnet-netuid` | The existing nonzero netuid owned by that wallet. |
| `testnet-spending-limit-tao-rao` | Maximum total testTAO outflow, as an integer number of rao. |
| `testnet-spending-limit-alpha-rao` | Maximum existing subnet-alpha transferred into release roles, as integer rao. The wallet must already control a registered staking hotkey with enough transferable alpha after conviction-lock and miner-collateral restrictions. The release profile reserves 20,000 alpha for demand custody plus reserve/independent validator bootstrap. |
| `testnet-spending-limit-evm-gas-wei` | Maximum aggregate EVM gas funding/use, as a canonical nonnegative decimal integer in wei. Quote values above `uint64` in YAML; the release profile uses `"100000000000000000000"` (100 testTAO). |
| `testnet-operator-api-origins` | Exactly two distinct bare `http(s)://host[:port]` origins, in NO 1/NO 2 order. Each must externally route to the corresponding API port and expose `/status`, `/verify/*`, `/sn/artifact*`, `/sn/attempt-artifact`, and `/sn/evidence*`. Launch verifies the signed content and history through these origins before publishing a portable manifest. |

The checked-in testnet governance value is `single-owner`; the harness generates
a dedicated capped testnet owner and a separate guardian. Unprefixed values are
mainnet-only and retain `safe-2-of-3`; `sim-testnet` refuses to resolve them.

`testnet-authority` remains the private fallback and must resolve
`sim-testnet:9944` from any execution host. The primary profile currently sets
both `public_substrate_rpc_override` and `public_evm_rpc_override`, so operational
reads, writes and workload proxies use the official public testnet services while
the private archive syncs. The fields are a pair: set both typed URLs or neither.
Removing both selects the private authority without changing vault data. The
lightnode profile deliberately omits them and therefore exercises private
fallback routing.

Public override mode is testnet acceptance mode, not independent infrastructure
proof. The official service is shared and rate limited, and its operational and
postcondition routes may be the same backend. `doctor` reports that distinction
as non-hard, and every postcondition/public manifest records
`independent_rpc=false`. Current-state checks, bounded release-window event reads
and testnet transactions may proceed; archive/history stress, high-rate campaigns
and the final mainnet-promotion soak must be repeated against the synced private
node plus a physically independent observer. Evidence uses the existing shared
`server/blob` MinIO configuration and bucket; no second object store is started.
MinIO and Subtensor are the only external shared services.

The active release pin is Subtensor runtime 454, transaction version 1, source
tag `v454`, commit `14cde6410fe8ec81a940e290c56f94a632a0988d`, and finalized
Wasm storage hash `0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef`.
The mandatory release gate also executes the exact v451/v452/v453/v454 on-chain
artifacts under a storage-free host boundary and reproduces the complete
version, code and metadata identities in
`docs/spec/runtime-metadata-artifacts.json`. The source-to-chain identity and five-delta compatibility review are recorded in
[`docs/spec/runtime-v454-audit.md`](../docs/spec/runtime-v454-audit.md).

Runtime 454 retains the distinction between atomic alpha transfers (`TransferToggle`, managed by
`sudo_set_toggle_transfer`) from the one-time trading/emission activation
(`SubtokenEnabled`, managed by the subnet owner's `start_call`). The harness
checks these as distinct storage postconditions.

Plan schema v5 also keeps demand deposits and validator stake economically
separate. The campaign deposit cap does not size validators. At one finalized
checkpoint the harness reads the runtime transfer floor and alpha price, every
registered hotkey's stake, the source position, coldkey-wide stake, stored
conviction lock, and position/coldkey miner collateral. It then allocates the
reserve validator to a 65% target share, allocates 1,000 alpha to the independent
validator, requires the reserve to remain above 60%, and retains at least 2,000
alpha at the source. The exact amounts are approval-bound and rechecked against
live price, registration, lock, collateral, majority, and remainder immediately
before signing. A changed or unavailable constraint stops without broadcasting.
Runtime 454 retains each coldkey's entitlement as a `SafeFloat` share even though
`transfer_stake_and_hotkey` conserves the exact integer amount in the hotkey
aggregate. `getStake` may consequently floor the destination entitlement by one
rao. Plan schema v8 binds that maximum shortfall explicitly, adds one rao to
fresh bootstrap allocations, and sizes reserve majority from the minimum credit.
Recovery verifies the destination at the transaction's parent and inclusion
blocks; it never derives a pre-state from an already-mutated live balance. A
finalized v5-v7 transfer that stopped at the old exact-balance observer is locally
reconciled and may execute only one separately budgeted runtime-minimum repair,
never the campaign allocation a second time.

If later emissions dilute an already verified reserve below its 65% target, a
revision preserves the bootstrap transfer and appends a fixed repair tranche.
The tranche is capped at 3,000 alpha and by the cumulative vault alpha ceiling;
it is not resized from the moving emission snapshot between review and apply.
Planning fails unless that fixed amount can restore 65%. Immediately before
signing, the harness rechecks price, transferable source capacity, the retained
source position, and the full live registered-alpha composition at 65%; the
postcondition proves the same share at the finalized transaction block. A
separate 60% barrier then protects the remainder of setup from later dilution.

Generation-2 fleet refresh intentionally consumes generation-1 mirror and
binding live state. Resume accepts an older receipt historically only when the
append-only journal contains its exact later generation-1 install/convergence
batch and the same-range generation-2 refresh, with ordered operational and
comparison checkpoints. The original mirror or binding is replayed at its
recorded EVM block; an adjacent or partial batch cannot authorize it. Challenger
fleets are not refreshed and therefore retain ordinary live revalidation.
Carried-plan preflight groups the resulting historical block headers and
block-pinned mirror/binding calls into at-most-50-element JSON-RPC batches. This
changes only transport cardinality: every response is still matched to its
exact action, receipt hash, recorded height, canonical hash and observed state.
Private mode repeats the batches through the independent observer; public
override mode requires identical detached comparison evidence. Successful
generation-1 mirror/binding proofs are saved individually under the state directory's
`historical-audit-cache-v1` directory, so a later failure or process restart
retains completed work. Entries authenticate the exact inputs and bind the
plan, release, verifier executable and authorized observers. Changed inputs,
unreadable entries or authentication failures cause the original verification
to run again. Builds predating this cache cannot contribute entries from their
progress logs.

Cache hits still require fresh canonical/finalized checkpoints and revalidated
local receipts, decoder inputs and successor relationships. Current balances,
runtime identity and live postconditions are always checked again. Finalized
native extrinsic proofs use the same persistent cache after their fresh chain
checks. Install batches also retain their successful block-pinned mirror/binding
comparisons before fetching the transaction receipt. Local member signatures,
native commitments, transaction receipts and install events remain outside that
cache boundary. No failed or canceled proof is saved, and independent observers
must both succeed before a combined fleet proof can be reused.

For a provisional testnet run, `resume` and `scenario` accept
`--provisional-resume` together with `--apply` and the exact persisted plan hash.
Retain the original configuration and repository arguments when replacing the
driver. This mode authenticates completed receipts locally, keeps pending
transaction recovery and spending limits, and uses the admitted driver image
for restarted components. It records actual executable provenance before work
begins and marks scenario results provisional with `final_acceptance=false`.
Strict release acceptance cannot consume those provisional results.
Provider swarms sign a fresh `/auth/wallet-challenge` with each miner's existing
payout coldkey before submitting `/sn/wallet` for that provider identity. The
renderer materializes the existing role at `secrets/miner-N-payout.seed`, with
private file permissions and runtime-manifest coverage; an occupied mismatching
seed is rejected and never replaced. Strict mode keeps unsigned wallet setting
disabled. Provisional mode retains the unsigned setting for older testnet clients.

Two authenticated atomic-alias receipt formats exist. Current aliases name the
exact source batch receipt and clone its finalized checkpoints. The first five
migration fleets instead recorded separate live mirror/binding reads after their
batch. Resume recognizes that old format only when all source metadata is absent,
the receipt is strictly ordered after the exact install and before the exact
refresh in both journal and checkpoint domains, and its original state replays at
the recorded block. Partial metadata or differing observer formats fail closed.

Demand custody crosses two runtime share pools: a same-coldkey `moveStake` to
the reserve hotkey and a `transferStake` to the immutable sink coldkey. Runtime
454 retains the one-rao floor present in v452/v453 for each destination
entitlement, so every
reserve call stages exactly two allowance rao and requires the final sink delta
to remain in `[principal, principal+2]`. The plan binds the number of reserve
calls and this per-call allowance in schema v9. Schema v8 remains byte-for-byte
authenticatable as a revision ancestor; its meaning is not strengthened in
place. Revisions retain every verified repair in cumulative spend and add only
one runtime-minimum top-up if conservative verified credit is below the stricter
absolute campaign requirement. Schema v10 additionally requires a duplicate-conviction
reconciliation to retain the exact authenticated original action intent. A
later gas-ceiling refresh or custody-repair dependency may gate new work, but
cannot turn that already-finalized one-shot action into a new intent.
Schema v11 retains v10 as authenticated history and binds `config.render` and
`topology.launch` to the exact approved config and policy hashes. Rendering also
writes `runtime-config-manifest.json`, whose sorted inventory, modes and SHA-256
digests cover every immutable operator, miner, validator, swarm and relayer
input. Launch fails closed on a missing, changed, additional or symlinked static
file.

An already-live immutable custody generation is never redeployed merely because
the coordinator changes. A repeated UUPS revision binds the exact next deployer
nonce and CREATE address, the finalized ERC-1967 implementation slot and active
runtime, every prior full runtime hash, and normalized executable hashes for the
reserve, vault, and precompile probe. Normalization removes only constructor
immutables and Solidity metadata; any executable custody drift fails closed.
The new implementation is additive, while the reserve/vault/proxy addresses and
their historical evidence remain unchanged.

Runtime 454 also raises a subnet's burn after successful registration. The
release plan therefore reserves at most `100000000` rao per registration and
binds that same ceiling into every native `register_limit` and EVM
`registerLimit` action. EVM callers are funded at their SS58 mirrors and pass
zero value to the neuron precompile; the runtime deducts the burn from the
caller mirror. Contract registrations supply the full ceiling and return the
unburned surplus, so an in-flight price increase cannot produce an underfunded
call below the approved cap.

Plan schema v5 binds every EVM transaction in two dimensions:
`maximum_gas_units` and `maximum_fee_per_gas_wei`. The checked-in fee ceiling is
100 gwei. Fixed setup unit limits are derived from the locked Foundry gas report
and include the manager's 20% plus 25,000-unit live-estimate margin. Signer
funding first covers the exact sum of that signer's explicit action ceilings;
only the remaining campaign allowance is weighted across keeper, deposit, root,
and claim-relayer roles. The aggregate campaign ceiling uses an arbitrary-size
canonical decimal so it cannot wrap or stop at the former `uint64` limit of
approximately 18.45 testTAO. Immediately before signing, the manager rejects a
fee spike, padded gas growth, aggregate mismatch, or value-plus-gas balance
shortfall without persisting or broadcasting transaction bytes.

## Host prerequisites

- Linux amd64, Go 1.26.x, Git, and a running user systemd manager.
- Both `net.core.rmem_max` and `net.core.wmem_max` at least 7 MiB, matching
  quic-go's release socket requirement. `doctor` fails closed below that floor;
  on Linux set a 16 MiB margin before launch with
  `sudo sysctl -w net.core.rmem_max=16777216 net.core.wmem_max=16777216`.
- At least 20 GiB free on the simulator state filesystem. Immediately before a
  launch/resume can construct a chain-capable executor, the harness also binds
  every required loopback process port and rejects any unrelated or stale listener.
- Docker with direct permission for the invoking user or passwordless `sudo -n
  docker`. The harness prefers direct access and never opens an interactive sudo
  prompt. One isolated PostgreSQL 18 and Redis 8 pair is created per operator from the exact digests in
  `deploy/testnet/release.lock.yml`. Their locale, database initialization,
  connection capacity, Redis threading, and persistence settings mirror
  `server/local`; they never use shared PG or Redis services. PostgreSQL data
  volumes and containers carry the same complete release/config hash, and stale
  or unlabelled volumes are rejected instead of silently reusing old init hooks.
- The locked `sn`, `server`, `operator-proxy`, `vault`, platform `config`,
  `connect`, `sdk`, `glog`, `goidenticons`, `proxy`, `userwireguard`, and `xops`
  repositories checked out beneath one parent. Repository discovery uses Go
  module identity plus required resource files; `--sn-repo`, `--server-repo`,
  `--operator-proxy-repo`, `--vault-repo`, and `--platform-config-repo` are
  available when the layout differs. Operator-proxy is bound by both its
  production-source hash and exact clean Git commit; the other executable Go
  sources and non-secret operator config tree are content-locked as documented
  in `release.lock.yml`.
- Network reachability to the selected operational Substrate/EVM pair, public
  comparison endpoints, and existing MinIO service. Private fallback additionally
  requires the overlay gateway.
- Rust 1.89 with Cargo, plus `curl`, `jq`, `sha256sum` and `xxd`, solely for
  release-time compatibility checks against the upstream Subtensor runtime.
  No miner, validator, operator, contract, or simulator service is implemented
  in or launches Rust. The first exact-runtime gate builds the audit-only,
  pinned restricted Wasm executor from `tools/runtime-metadata-probe` into the
  sibling `temp/runtime-metadata-probe-target` cache; later gates reuse that
  target. Selected `cargo test` commands also run in a clean temporary checkout
  of upstream Subtensor v454 and do not add a runtime dependency to this stack.
- Foundry 1.7.1 build commit
  `4072e48705af9d93e3c0f6e29e93b5e9a40caed8` only for developer
  rebuild/review. The release gate also requires the exact clean, commit-pinned
  forge-std and OpenZeppelin checkouts named in `release.lock.yml`. A launch
  embeds locked bytecode and never compiles Solidity at runtime.

On this checkout Foundry is installed at `/home/by/.foundry/bin`.

## Atomic release-lock refresh

Refresh observed source, interface, infrastructure, and EVM fields only after
every contributing repository is frozen in a clean commit. The renderer uses
the verifier's exact observation schema, checks all configured and sibling
repositories plus the three Foundry dependency worktrees before and after the
observation, and preserves the separately reviewed runtime, image,
dependency, compiler-policy, and audited-base fields.

From the `sn` repository, review the canonical candidate without writing:

```bash
go run ./sim-testnet release-lock --config sim-testnet/testnet.yml
```

Then install those exact bytes with one atomic replacement:

```bash
SN_REPO="$(pwd -P)"
WORKSPACE="$(dirname "$SN_REPO")"
release_head="$(git rev-parse HEAD)"
build_utc="$(date -u +%Y%m%dT%H%M%SZ)"
SIM_TESTNET_RELEASE_DIR="$WORKSPACE/temp/sim-testnet-lock-${release_head}-${build_utc}"
SIM_TESTNET_BINARY="$SIM_TESTNET_RELEASE_DIR/sim-testnet"
mkdir -p "$SIM_TESTNET_RELEASE_DIR"
go build -trimpath -buildvcs=true -o "$SIM_TESTNET_BINARY" ./sim-testnet
go version -m "$SIM_TESTNET_BINARY"
sha256sum "$SIM_TESTNET_BINARY"
"$SIM_TESTNET_BINARY" release-lock --config sim-testnet/testnet.yml --apply
```

The apply form refuses a missing or dirty checkout, a checkout that advances
during observation, an incomplete observation, a symlinked or escaping lock
path, or lock bytes changed since configuration load. It intentionally leaves
the `sn` worktree dirty only by the updated `deploy/testnet/release.lock.yml`;
review and commit that file before running `doctor` or refreshing again.
The temporary executable must be built from a clean commit already equal to
`origin/main` and invoked by that exact absolute, nonsymlink path. A relative,
PATH-resolved, dirty, unstamped, non-`-trimpath`, stale, or unpushed executable
cannot write even when it is given `--apply`.

## Build and read-only preflight

From the `sn` repository:

```bash
SN_REPO="$(pwd -P)"
WORKSPACE="$(dirname "$SN_REPO")"
release_head="$(git rev-parse HEAD)"
build_utc="$(date -u +%Y%m%dT%H%M%SZ)"
SIM_TESTNET_RELEASE_DIR="$WORKSPACE/temp/sim-testnet-${release_head}-${build_utc}"
SIM_TESTNET_BINARY="$SIM_TESTNET_RELEASE_DIR/sim-testnet"
SIM_TESTNET_LIGHT_BINARY="$SIM_TESTNET_RELEASE_DIR/sim-testnet-light"
SIM_TESTNET_STATE_DIR="$SN_REPO/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4"
mkdir -p "$SIM_TESTNET_RELEASE_DIR"
go build -trimpath -buildvcs=true -o "$SIM_TESTNET_BINARY" ./sim-testnet
go build -trimpath -buildvcs=true -o "$SIM_TESTNET_LIGHT_BINARY" ./sim-testnet
go version -m "$SIM_TESTNET_BINARY"
sha256sum "$SIM_TESTNET_BINARY" "$SIM_TESTNET_LIGHT_BINARY"

"$SIM_TESTNET_BINARY" doctor \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --format json

# Same release checks and topology, isolated state/artifact names, and the
# side-by-side warp-synced lightnode RPC selected by the executable name.
"$SIM_TESTNET_LIGHT_BINARY" doctor --format json

"$SIM_TESTNET_BINARY" plan \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --format json > /home/by/urnetwork/temp/ur-subnet-testnet-plan.json
```

`doctor` checks the release lock, repository source hashes, wallet proof,
ownership, balances, budget, runtime/genesis/chain identity, metadata and call
shapes, the exact finalized runtime Wasm hash, finalized subnet-token/emission
activation, recent historical EVM state, gateway methods, the signed finalized-head
lag bound, a canonical common checkpoint, precompiles, MinIO's exact HTTP live
endpoint, the Docker daemon and systemd. Private mode additionally hard-requires
connected consensus peers and distinct physical operational/observation backends;
public override mode records those unavailable assurances without overstating them.
`plan` repeats those gates, reads finalized setup facts, and prints every intended
action, dependency, maximum spend and the canonical `plan_hash`. That approval
hash binds the complete release lock, harness/public/hyperparameter manifests and
all non-secret values resolved from the vault, not only their YAML references. The
signed policy has its own canonical hash. Neither command submits a transaction or
extrinsic.

Runtime-454 transfer economics are approval-bound explicitly: the exact
finalized Wasm hash must match the release lock, and that block's
`SubtensorModule.InitialMinTransfer` metadata constant—the value used by the
runtime's internal `DefaultMinTransfer` function—must equal
`public.yml:chain.expected_default_min_transfer_rao`. Every planned alpha
transfer is sized from it at the same finalized snapshot, and the value is an
immutable settlement-vault constructor/runtime word. Historical v5-v10 plans
keep their authenticated wire semantics; current approvals use plan schema v11.

The public-chain integration probes are opt-in:

```bash
SIM_TESTNET_LIVE_WALLET=1 go test ./sim-testnet -run TestLiveVaultWalletResolution -v
SIM_TESTNET_LIVE_READ=1 go test ./sim-testnet -run TestLiveBalanceProbe -v
```

The alpha-bootstrap integration test additionally requires its exact
`SIM_TESTNET_STAKE_ALPHA` confirmation string. It is idempotent once the target
alpha position exists and otherwise journals activation and staking before
checking finalized storage. It is not part of the ordinary unit-test suite.

## Approved setup and launch

Use the exact hash from the reviewed plan. A changed config, resolved vault input,
policy, release lock, role derivation, source checkout, artifact, runtime fact, or
persisted plan fails closed. Every apply reruns `doctor` and rechecks finalized
economic facts against the exact unverified remainder. Docker dependencies and
all release binaries are preflighted before a transaction-capable executor opens.

```bash
# Optional: converge chain/contracts/config without starting services.
"$SIM_TESTNET_BINARY" setup \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

# Converge setup, start the persistent topology, run the mandatory M0B
# precompile-conformance gate, prove readiness, and run smoke.
"$SIM_TESTNET_BINARY" launch \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH \
  --detach
```

The journal records intent, signed bytes/nonce, broadcast, inclusion, finality and
postcondition. If the command is interrupted, use the same approval:

```bash
"$SIM_TESTNET_BINARY" resume \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH \
  --detach
```

Head-fleet setup is bounded but not artificially serialized. All ten
independently signed Substrate commitment writes run concurrently inside an
explicit ten-fleet plan group; every write retains its own fee ceiling, raw
transaction, journal lineage, finalized storage proof, and idempotent recovery.
The testnet-only `STFleetBatcher` then installs or refreshes that group in one
atomic EVM transaction. It accepts at most ten fleets with four members each,
only from the immutable original commitment oracle, rejects duplicate fleet or
member identities, and exercises the coordinator's normal dual-signature and
client-revocation checks. The owner activates it through the ordinary
future-epoch oracle schedule and restores the original oracle before topology
launch. Maximum-size Foundry calls use 9,535,582 gas for install and 9,080,115
for refresh, below the approval-bound 18,000,000 and 24,000,000 ceilings.
Existing verified per-member writes remain charged verbatim across a formal
plan revision; absent or partial batches fail closed instead of being inferred.
A later release revision authenticates the helper's extra deployer nonce and
runtime before advancing the CREATE boundary. Any verified EVM action replaced
by that revision moves once into cumulative superseded gas, so acceleration
cannot erase historical spend from the approval envelope.

Every historical coordinator read in a batch remains pinned to one exact EVM
block, but the HTTP transport groups at most 50 `eth_call` elements, matching
the public endpoint's enforced limit. The individual mirror/member plan actions
then derive their receipts from the authenticated batch receipt and their
canonical signed artifacts; they do not repeat the batch's live RPC surface.
Resume still revalidates the source batch on chain before any new mutation.

Generation-2 refreshes use the same rule on both execution and replay. Before a
fresh atomic refresh is signed, all 40 predecessor count/record pairs are read
as two HTTP batches of 50 and 30 elements. A carried 10-fleet refresh checks its
ten mirrors, 40 version counts, 40 truncated predecessors, 40 successors and ten
fleet cardinalities as three batches of 50, 50 and 40 elements. Oracle routing's
five independent fields share one block-pinned request. Batching changes only
transport: every returned field is decoded at its original position and checked
against the signed evidence, deterministic manifest member and exact selected
block. Provider/context failures remain operational failures and are never
reported as evidence that a miner supplied a dishonest generation.

At the pinned 12-second public-testnet cadence, this changes head-fleet setup
from a many-hour serialized transaction chain to roughly 1--3 hours, including
boundary alignment: 400 native commitments run in ten-wide waves, 40 EVM
batches replace 1,600 per-member install/refresh calls, historical reads use
bounded RPC batches, and two future-epoch oracle handoffs remain.
The five accelerated acceptance epochs require about five hours. `release-1.0`
first discards the post-preparation partial accelerated epoch and waits through
the final 150-block settlement offset. The three 360-block production UR blocks
retain about 3.6 hours of complete chain observation after a second
post-preparation partial epoch, plus the final 180-block settlement window. The
combined live acceptance path is expected to take roughly 12--15 hours including
future-effective transition and boundary alignment. Those protocol-time gates
are not shortened or simulated off-chain.

There is no separate one-hour M1 wait before that five-epoch interval. Launch
hands directly to `release-1.0`; its first complete reconciled epoch is both the
M1 end-to-end proof and epoch 1 of M2. On a clean release marker,
`production-soak` starts immediately and schedules its future-effective policy
without an operator pause. The five-epoch campaign keeps the deployed
300-block accelerated cadence. Its future-effective transition to the
release-locked 360-block acceptance policy is a new, explicitly hashed and
journaled approval lineage.

Host reboot is an intentional stop boundary. The supervisor unit is started but
never enabled, managed PostgreSQL/Redis containers use Docker restart policy
`no`, and loginctl linger is not required. After a reboot, run `resume` explicitly;
it re-runs doctor and reconciles the journal and finalized chain before starting
any dependency or process. Provisioning helpers also persist PID, process-group,
kernel start-time, executable-hash and argv-hash ownership. If the parent exits
abnormally, resume reaps only an exact orphan identity and never a reused PID.
Topology readiness is deliberately plan-generation-local: a revised plan
authenticates the ancestor launch receipt as history but cannot use it to satisfy
the new plan's launch dependency. Only after the current binaries, supervisor
generation, children, fresh validator proofs and fenced logs pass readiness does
the current plan record `topology.launch`. Same-plan resume likewise rechecks the
new live generation without duplicating its terminal journal entry.

Before smoke, `launch` automatically runs the named `precompile-conformance`
scenario. It finalized-reads and replaces/restores a native commitment, deploys
the locked disposable probe, checks Blake2/Ed25519/sr25519/metagraph/neuron/staking,
converts only the plan-approved TAO dust into alpha, performs an exact two-hotkey
round trip, observes a take-zero dividend cycle, and transfers every attributable
alpha unit to a controlled provider coldkey. Each phase has a separate transaction
intent, ceiling, finalized receipt, postcondition and signed evidence record.

## Observe and run release campaigns

```bash
"$SIM_TESTNET_BINARY" status  --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR" --format json
"$SIM_TESTNET_BINARY" inspect --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR" --format json
"$SIM_TESTNET_BINARY" analyze --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR" --format json
"$SIM_TESTNET_BINARY" tail    --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR"

"$SIM_TESTNET_BINARY" scenario --name precompile-conformance \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

"$SIM_TESTNET_BINARY" scenario --name release-1.0 \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

"$SIM_TESTNET_BINARY" scenario --name production-soak \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

# Recommended uninterrupted M2 -> M3 release-candidate campaign. It adopts
# only exact signed clean phase markers and runs the first missing phase.
"$SIM_TESTNET_BINARY" scenario --name release-candidate \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH
```

`release-candidate` is a resumable orchestration name, not a weaker scenario.
It runs `release-1.0`, independently reloads and authenticates the signed result,
complete marker, lifecycle-handoff bytes and every named evidence file, then
binds those exact hashes into a durable production attempt before any production
mutation. It never selects a merely newer release. If M2 is already valid it
runs only its bound M3 attempt; if both are valid it performs no scenario action.
A failed, canceled or unsigned phase is never adopted and never silently
replaced with a new run ID: the next invocation reopens the same attempt and
reconciles its signed checkpoints before resuming.

`release-1.0` requires five accelerated epochs, real two-NO verification,
independently applied CRv4 vectors and self masks, isolated deposits and conviction,
public roots, claims from both pools, cryptographically reconstructed head bindings,
and a fresh, independently reconstructed and signature-verified validator path
proof from every validator/operator pair in every required epoch. The live custody
actor also submits a mutated real payout leaf by read-only `eth_call` against both
NO entitlements and requires the exact invalid-proof error with no state change.
It further requires
a nonzero native head weight, a real promotion/demotion transition across the
200-slot boundary, exact selected/rejected native reward channels, one-tier payout
exclusion, actual `ClaimPaid` settlement (distinct from accepted/deferred claim
credit), exact signed-policy max-weight-cap compliance, reserve principal plus
auto-compounded yield, process fault recovery and both exact vault conservation
identities (`captured = paid + escrow`, `escrow = pending + outstanding`).
Each validator's immutable intent includes all 202 canonical rational fleet
scores computed from its own trails. The simulator independently ranks those
scores and reconstructs that validator's exact 200 selected and two rejected
UIDs; the two validators are allowed to reach different boundaries from their
own evidence. Every unmasked UID selected by a validator must have positive
weight in that validator's applied vector, while each UID it rejects must have
zero weight there. A positive weight for any UID outside that validator's
selected set and the two live pool UIDs is a hard failure. Finalized native
reward vectors must pay every unanimously selected fleet and pay no fleet
rejected by every validator; a disputed boundary is left to Yuma's stake-weighted
median and clipping and is reported from the chain. The same checks cover every
applied intent created after the acceptance baseline, so a later valid decision
cannot erase an intermediate boundary or weight violation. During M2 a private,
testnet-only operator API filter withholds fleet 4 from validator 1 but not
validator 2. A common native epoch must show validator 1 rejecting that UID at
zero and positively weighting its replacement while validator 2 makes the
opposite decision; a later common epoch must prove restoration. The filter is
mode- and deployment-confined, atomically persisted, ledger-recovered, and is
restored immediately after authenticated applied-decision evidence proves the
divergence. Native head windows rotate atomically, same-epoch retries reuse the
same evidence, and the terminal interval budgets a complete fresh trail and a
strict recovery fold. All 808 providers
bound to
the 202 live fleet UIDs—including rejected but still registered fleets—must be
absent from operator payout leaves, preventing double pay until actual
deregistration returns a fleet to its pool.
The runner snapshots finalized contract geometry after preparation, discards
that containing epoch, accepts only the next five complete epochs, then waits
through terminal finalization. Its signed result binds the baseline observation,
exact start/end/terminal blocks and terminal status for both operator positions;
the campaign verifier reconstructs those boundaries independently. Every
ordinary M2 fault must also trigger and restore inside the accepted five-epoch
interval. The two lifecycle filters are the explicit exception: if the
independent native clock cannot complete the three causal pre-handoff decisions
inside those 1,650 blocks, they remain active only through a separately reported,
schedule-bounded release-handoff tail (at most 810 blocks for the pinned
tempo-360/reveal-period-1 profile) and restore as soon as their exact evidence
conditions hold. Tail blocks never count as a sixth accepted epoch.
`production-soak` schedules the testnet-only 360-block (approximately 72-minute)
policy and immunity period, deliberately under-deposits one operator and proves
that both validators zero its pool until an exact later deposit recovers it,
rotates each operator verification key while retaining old proof verification,
runs three consecutive fully observed production epochs, and genuinely restarts
(new PID, healthy replacement) every operator service, miner/claim daemon and
validator without overlapping faults. Mainnet remains locked to the whitepaper's
separately reviewed 50,400-block/seven-day cadence. The three production epochs
and 180-block terminal interval remain the exact acceptance window. If their
terminal head does not yet contain the finalized terminal-generation CRv4
application, the runner continues only through a separately labeled,
schedule-derived evidence tail and stops at the first such proof. It never
reports tail blocks as an additional accepted epoch; the signed evidence exposes
both settlement and native-subnet clock domains and the exact tail bound.

While the topology is live, one supervised non-faulted loopback EVM egress owns
the configured upstream quota. Workloads reach it through their faultable proxy;
scenario writers, observers, adversarial actors and concurrent
`status`/`inspect`/`analyze` commands reach it directly. A live supervisor may
not fall back around a missing or unhealthy gate. Before launch and after stop,
read-only commands use the canonical configured endpoint.

## Continuous adversarial campaign

The `release-1.0` and `production-soak` scenarios always load the release-locked
[`adversarial-matrix-v1.json`](../docs/spec/adversarial-matrix-v1.json). Its 61
rows cover Yuma/YC3 cabals, stale and reveal-following weight copies, liquid-alpha
bond timing and validator-permit churn, all eight published Subtensor security
advisories, historical runtime atomicity/accounting/identity/resource failures,
subnet reserve/registration/liquidity/eviction pressure, hidden root-basket rewards,
proxy-stake MEV/slippage, four security-relevant Bittensor SDK/transport issue
families (missing signatures, finality-era expiry, plaintext unauthenticated
transport, and constant body hashes), runtime/precompile drift, identity
and proxy churn, commitment-field parser confusion, operator/verification abuse, artifact equivocation, contract
authorization/custody, settlement, runtime transfer-floor/durable-credit, and
dependency failures.

Seven attributed actors start before the happy path and remain active until
after its final reconciliation:

- bounded operator API and real `/verify` pressure, including simultaneous
  identical EXTENDs, replays, invalid signatures, poison-shape comparisons, and
  per-source vpk rotation;
- independent private/public finalized-RPC agreement, observed runtime-spec and
  transaction-version identity, plus common-height subnet UID,
  spot/moving-price, and TAO/alpha-reserve reads;
- artifact fetch/reconstruction/tamper pressure and fleet identity-generation
  mutations; and
- deterministic consensus, liquid-alpha, custody, unit/domain, rounding,
  root-index, and upstream reserve-flow emulation.

Every fifth sample is a control and the other four are adversarial. Release
configuration requires at least 100 non-skipped samples per actor, both phases,
zero unexpected actor errors, p99 latency at most 15 seconds, attack/control p95
latency no worse than 20×, at most eight
operator requests/second and two RPC requests/second. Expected 400/409/429
rejections are recorded separately from faults. Campaign evidence includes the
matrix hash, lifecycle overlap, request/in-flight totals, latency distributions,
per-vector required and actually sampled metric names, and full-run minima/maxima
for on-chain numeric sentinels in
`runs/<deployment-id>/runs/<run-id>/adversaries.json`.

The release schedules non-overlapping outages for every simulator-owned
PostgreSQL/Redis pair and the simulator-owned loopback Subtensor RPC proxy, then
rolls every persistent process. It records exact downstream impact windows and
requires healthy replacement PIDs. The external shared Subtensor and MinIO
services are not destructively faulted; MinIO remains under continuous
history/reconstruction/tamper pressure.

Every run also writes `anomalies.json`. It is built from failed assertions,
deployment warnings, component errors, unresolved claims, supervisor health and
restart deltas, incomplete faults, and adversary actor/vector failures. Scheduled
restart faults are reconciled exactly; any excess or missing restart is an
anomaly. A release run passes only when this append-only ledger is `clean` with
zero entries. Failed runs leave entries `open` for the root-cause, minimized
reproduction, regression, and clean-rerun evidence required by the mainnet
readiness dossier.

Shared-testnet safety is structural: live actors touch only loopback operator
endpoints, our deployment/netuid identities, and capped read RPC. Chain-wide
flooding, proxy takeover, cooldown bypass, and global state-bloat exploits run
only against the exact pinned local runtime; their live actors are read-only
sentinels or bounded state-machine emulators. Any unexplained error, drift,
latency breach, missing sample, process restart, or happy-path discrepancy fails
the scenario and remains a root-cause investigation item—it is never waived as
“adversarial noise.”

For independent inspection, use any signed deployment-manifest evidence URL from
`runs/ur-subnet-testnet-v1/public/deployment-manifest.locators.json` on a clean
compatible checkout:

```bash
./build/sim-testnet inspect \
  --config sim-testnet/testnet.yml \
  --manifest 'https://NO/sn/evidence?hash=sha256:...'

./build/sim-testnet analyze \
  --config sim-testnet/testnet.yml \
  --manifest 'https://NO/sn/evidence?hash=sha256:...' \
  --run-id 'SIGNED_CAMPAIGN_RUN_ID'
```

Final validation must use the latest authorized strict-format manifest. A
revision-zero pre-fix locator is lineage-only and still requires its exact
signed `--run-id`; mixed legacy/current reviewer metadata fails closed.

## Stop and retire

`stop` terminates only local supervised processes; it preserves containers,
secrets, evidence and all chain state. Retirement is a separate future-effective,
hash-approved on-chain plan and is dry-run by default:

```bash
"$SIM_TESTNET_BINARY" stop --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR"
"$SIM_TESTNET_BINARY" retire --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR" --format json
"$SIM_TESTNET_BINARY" retire --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_RETIREMENT_PLAN_HASH
```

Retirement deactivates operator versions at the next epoch. It never deletes the
immutable vault, reserve, prior entitlements, claims, MinIO history, role store, or
local run evidence.
`stop` remains available during source repair or release-lock drift. It does
not make chain writes and retains its independent exact PID, process start-time,
executable, and argv ownership checks so a failed campaign cannot linger merely
because its checkout is being repaired.

## Local verification

These commands are safe before launch approval and perform no testnet writes:

```bash
go test ./...
go test -race ./crv4 ./miner/... ./protocol ./sim-testnet ./validator

PATH=/home/by/.foundry/bin:$PATH \
  bash -c 'cd evm && forge fmt --check && forge build --sizes && forge test --summary'

cd ../server
WARP_ENV=main \
BRINGYOUR_MINIO_HOSTNAME=172.28.208.177 \
SIM_TESTNET_LIVE_BLOB=1 \
go test . -run '^TestLiveBlobStoreContentAddressedCanary$' -count=1
```

The opt-in blob test writes one fixed content-addressed canary, then reads and
lists it through the real server/blob service account. Repeated runs overwrite
the same bytes at the same key; ordinary tests never access external storage.

Database-backed server tests additionally need the hermetic PostgreSQL/Redis/vault
profile that `launch` materializes after verified contract addresses exist. Running
them with only `RUN_SERVER_DB_TESTS=1` and no `WARP_ENV` is an expected fail-closed
configuration error, not a database-health result. The release campaign runs them
against both rendered managed-operator databases before the final go/no-go decision.
