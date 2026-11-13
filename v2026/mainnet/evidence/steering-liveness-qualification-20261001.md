# Standard-validator steering responsiveness, 2026-10-01

This increment diagnoses an observed steering loop that stops returning while
its independent progress publisher continues. It performs no systemd mutation,
signing, broadcast or live service/RPC observation. MG-07 remains open for
approved production budgets, deployment, delivered alerts and active-hang
stop/join/custody authority.

The implementation is frozen at `40eaee2c79071c070dc47da317ba210020bdeaeb` on
`fix/mainnet-validator-progress-20261001`, based on SN
`46efd24c1d434147b686a7d113539c669805748a`. A subsequent test-only assertion
strengthens the maximum-size publication check without changing production code.
Exact source hashes, dependency modfile,
selections, raw results and controls are retained under restricted directory
`/mnt/data/sn-testnet/mainnet-steering-liveness-20261001/`. The external modfile
changes only relative sibling replacements to their absolute workspace paths;
the published SDK/Connect pins remain unchanged. Go is `go1.26.6`.

## Qualified behavior

- Explicit per-role warning/critical budgets use actual
  `Steering.ObservedAt` outcomes after a fresh responsive baseline. Process
  heartbeat, successful reads and unchanged economic state cannot refresh them.
- A returned retry/read/receipt/reveal/epoch wait demonstrates responsiveness
  without asserting protocol success. Unknown first startup and replacement
  instances require a real outcome before arming. A same-instance `starting`
  report cannot erase the retained baseline; changed startup boundaries and
  regressed outcomes are refused for armed instances.
- Fixed checkpoint v4 history retains baseline, original episode/budget,
  detection, recurrence and real later recovery across restart, source loss,
  clock faults and policy changes. Recovery must follow the detection cut.
  Read/native incidents retain their separate meanings. v1–v3 checkpoints
  migrate with unknown liveness history, and independently signed v3 stopped
  repair envelopes retain their original scope.
- Twelve fixed gauges and three alert rules expose liveness warning, retained
  critical incidents and unavailable/unknown signals. Existing independent
  expected-host/role and stale-sample rules cover a missing monitor/host.
  Active processes remain refused by the stopped-validator controller.

The producer test blocks the actual production loop's submitted callback at an
explicit channel barrier, takes real progress snapshots while publication
continues, then releases the call into a transport-wait outcome. Consumer tests
use actual command/file/checkpoint/metric paths and controlled observation
clocks. The callbacks are synthetic operation-boundary fixtures, not a live
validator, independent process attestation or proof of a deadlock's cause.
All identities, domains and host/process fixtures are synthetic.

## Final positive source checks

| Selection | Roots | Normal package PASS | Race package PASS |
| --- | ---: | ---: | ---: |
| Monitor service/read/native/steering and stopped repair | 68 | 6.075 s | 13.814 s |
| Production steering and producer progress | 28 | 12.542 s | 74.703 s |

The exact selections are `selected-mainnet.txt` and `selected-validator.txt`.
Final raw streams are `sealed-mainnet-normal.jsonl`,
`sealed-mainnet-race.jsonl`, `final-validator-normal.jsonl` and
`sealed-validator-race.jsonl`. All 96 roots passed in each mode, with package
PASS, exit 0 and no skipped roots or race reports. The intentionally skipped
subprocess entry is excluded from the mainnet selection; its invoking repair
test remains selected. This is bounded affected-path qualification, not a full
repository suite claim.

`go vet` passed for `./mainnet ./validator`; `gofmt`, `git diff --check` and exact
source-hash verification passed. Promtool 3.5.0 passed the service alert fixtures
in the preexisting container image with networking disabled. The four added
scenarios distinguish stale steering under fresh publisher heartbeat, actual
recovery, unknown startup, warning and an absent independently expected host.
The existing worst-case escaped deployment identity, maximum diagnostic
counters, retained native incident and 25 read-outage cycles now also retain a
steering episode and recovery within the unchanged 16 KiB checkpoint/event and
32 KiB textfile limits. The strengthened bound assertion additionally requires
acknowledged publication and the current episode/recovery in the actual saved
checkpoint. Its final separate normal/race package passes are 1.015 s/3.330 s
(`bounds-normal.jsonl`, `bounds-race.jsonl`); final vet also passes.

## Causal controls and retained development evidence

All five isolated Go overlays fail at their intended assertion in both normal
and race modes: suppress elapsed loop age, accept recovery from before detection,
drop liveness on checkpoint publication, remove the stopped-generation check,
and bypass explicit budget admission. All ten have selected root/package FAIL
and exit 1, with no compile failure, timeout, panic or race report. The exact
mutations, invocations and assertion logs are in `controls/`, with
`control-results.json` and `control-run.log`. Overlay files never modify the
working source or issue a live command. `qualification.json` independently
audits the final root census, package completion, controls, source/dependency
hashes and explicit no-signing/no-service-mutation scope; `SHA256SUMS` seals
the retained evidence files and excludes temporary compiler output.

Earlier failures remain retained: the first focused run omitted the synthetic
native-chain expectation in one checkpoint fixture; the next used the wrong Go
field name and failed to build mainnet. Both were corrected. An initial broad
adjacent selection exposed a real legacy clock-tolerance regression, which was
fixed by applying the new exact boundary checks only to armed liveness policies.
That same broad selection included two unrelated PostgreSQL operator roots
without their required disposable database environment; those setup failures
are not passes. The final selected normal/race packages above are complete and
include the previously regressed native-deadline root. Earlier partial stages
do not substitute for those final package results.

No approved production SLO, active-hang repair permission, global signer fencing,
installed alert route or delivered incident page is supplied by this receipt.
