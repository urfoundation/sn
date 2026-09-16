# Strict resume to full release-candidate handoff

Frozen implementation: `9ce20e9d11f06851783fdc904802d3565ab077ca`, clean isolated
source `workspace/sn`, parent `8270992eb8fb2b1599a29271ec44426379007306`.
No formatter, build, test, RPC or native operation was executed by Astra.

The explicit option is `resume --then-release-candidate --apply --detach`, with
the exact approved plan and strict-history-adoption path/SHA256. `--detach`
applies to the supervisor: the command remains the foreground campaign owner
and returns only after full campaign completion or a failure. It rejects
preparation-only, provisional options, other commands and a separate `--name`.
Both actual phase definitions are validated before plan/state preparation.

The ordinary preparation, doctor, retained-history authentication, signed-state
checks, setup action reconciliation and strict LaunchDeployment run once.
On successful startup, the same executor, plan, roles, journal and deployment
lock go directly to the existing runReleaseCandidateCampaign. There is no
second runMutation, doctor, dependency preparation or carried-action preflight.
The ordinary campaign still owns archive preflight, signed phase adoption,
native warmup and observation, fixed relay horizon, faults/adversaries, five
accelerated and three full production epochs, and both semantic analyzers.
No prepared phase allowance or acceptance condition was reduced.

LaunchDeployment still owns failed-startup generation cleanup. Cancellation
before or after startup prevents campaign entry; parent context is passed to
both owners. Campaign failure remains failure. The combined JSON result carries
`command=resume`, `scenario=release-candidate`, `campaign_complete=true` only
after the full campaign returns success. It never first publishes a standalone
resume success. A later campaign failure retains ordinary standalone campaign
process/evidence behavior; no new implicit stop or cleanup policy was added.

Only four Go paths changed, listed in selection.json and
source-files.before-formatting.sha256. Terra may gofmt only those paths and
commit the formatter descendant. All eleven sibling links are recorded in
sibling-targets.txt, pointing to the existing physical release. Private tmp and
gotmp already exist under this run; use /mnt/data/sn-testnet/gocache.

Qualification is the exact 14 roots in selection.json once normally and under
race. Six new deterministic roots cover early option/phase refusal, real writer
lock retention, exact launch/campaign order, cancellation, changed approval or
owner, and the real campaign archive/semantic failure boundaries. Eight existing
adjacent controls cover default CLI behavior, preparation gates, generation
cleanup, unsigned handoff refusal, real phase order, both analyzers and parent
cancellation. New roots use the existing StrictHistoryAdoption producer selector.
No full gate or readiness rerun is requested: base827's eleven readiness roots
already passed both modes and both causal controls.

After Terra freezes formatting, Astra will materialize one fresh causal view
from that exact descendant. It removes only campaign dispatch from the new
handoff, retaining the old resume completion boundary. The six new roots must
produce exactly three FAIL (writer handoff, failure/cancellation, full campaign
gates) and three PASS, with body exit1. This tests the causal missing handoff;
it does not claim a timing speedup or live campaign result.

Adjacent paths inspected: main command parsing/direct runMutation validation,
strict adoption identity and invocation context, prepare-only action gate,
LaunchDeployment detached readiness/failure cleanup, native executor ownership,
scenario campaign phase creation and authenticated reuse, archive preflight,
semantic analyzer joining, and final CLI result publication. Standalone setup,
launch, resume and scenario behavior is unchanged when the option is absent.

The actual combined native invocation remains the representative completion
and horizon check. This patch removes a demonstrated duplicate preparation
invocation; it does not promise that any unmeasured live workload fits a fixed
window.
