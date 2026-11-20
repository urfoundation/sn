# Bounded repair of one active steering hang

`sn-mainnet repair-active-validator claim|resume|status` is a separate,
independently signed local service capability for an already installed standard
UR validator. It admits a retained v4 steering-liveness incident with a previously
responsive loop, a current fresh publisher, and an unchanged actual steering
outcome. It can consume one stop, observe the exact old generation's empty
descendant cgroup, and consume one start. Monitoring alone never grants this
permission. The [stopped-validator command](VALIDATOR-REPAIR.md) still cannot stop
an active process.

This implementation has no approver key loader, signer, broadcast, unit installer,
daemon reload, automatic retry loop or implicit `restart`. No live service was
changed during [source qualification](evidence/active-validator-repair-qualification-20261001.md).
Production approval, host custody and real-systemd rehearsal remain MG-07/PH-28
gates. Root/operator services, first activation and monetary repair are outside
this capability.

## Independent envelope

All three commands require explicit inputs:

```text
sn-mainnet repair-active-validator claim \
  --approval /ABSOLUTE/APPROVED/active-repair.json \
  --accept-approval-hash sha256:EXACT_FILE_DIGEST \
  --independent-public-key 0xINDEPENDENT_ED25519_PUBLIC_KEY
```

The schema is `urnetwork-mainnet-active-validator-repair-v1`. The signature is
64 bytes of lowercase hex over UTF-8
`urnetwork-mainnet-active-validator-repair-approval-v1`, a zero byte, then Go's
compact JSON encoding of `repairActiveValidatorApproval` with its signature
field empty. Declared field order, exact original checkpoint and canonical
timestamps participate. The command input selects the independent public key;
the envelope cannot nominate its own trusted verifier. Existing stopped-repair,
activation and monitor approvals are not signatures in this domain.

| Signed plan input | Required scope |
| --- | --- |
| `process` | The fixed `repairValidatorPlan` profile: exact role/source/config, machine and boot, unit/release/systemctl hashes, installed paths, UID/GID, required mounts, prior InvocationID/PID/monotonic start, state path, window and finite observation/start limits. |
| `process.original`, `process.incident_id` | Entire original v4 checkpoint and its unresolved steering incident, including healthy baseline, failure sample, original producer and margins. Missing/starting-only steering does not qualify. |
| `monitor_services` | Exact path and file hash of the independently selected services policy. Its unique role, source, progress path and liveness margins must match the signed incident. |
| `maximum_stops` | Exactly one, separate from `process.maximum_starts=1`. |
| `join_window_seconds` | 60–600 seconds from durable stop consumption, checked with wall and monotonic clocks. |
| `process.command_timeout_seconds` | 35–60 seconds for each joined manager command; the installed unit has 30-second start/stop timeouts. |
| `process.maximum_observations` | 1–1,024. Each resume observation is reserved before host reads; reopen does not refill it. |
| `process.valid_from`, `process.expires_at` | At most 24 hours. A new stop requires remaining time of at least the full join window plus two command timeouts, including after its reservation sync. No implicit recovery grace exists. |

The approver must independently associate the retained producer with the exact
host invocation, review release/config and existing signer/volume custody, and
approve the complete encoded envelope. A caller-supplied hash without a valid
independent signature is insufficient. The existing config and state directory
are preserved; no process repair resolves or discards pending transactions,
claim obligations or old authority.

## Current evidence and finite effects

Before stop, both initial admission and the check after durable reservation
require `ReadStatus=ok`, a current fresh stalled checkpoint, the exact unresolved
episode, no clock fault and unchanged source/policy. A direct strict producer
read additionally requires heartbeat and acknowledged publication younger than
90 seconds, with every timestamp no later than now. Any changed steering outcome,
instance, config or start boundary refuses the stop, including recovery before
the monitor's next sample. Legitimate fresh epoch/reveal/read waits do not admit
this incident. Missing/unavailable reads cannot authorize a stop.

The existing fixed installed profile and hashes are checked against systemd's
loaded properties. The active path additionally refuses aliases, reverse stop
dependencies, restarters, stop propagation, differing kill signals/timeouts and
`StopWhenUnneeded` on the unit or its admitted mount/slice prerequisites. This
accounts for systemd's [dependency and stop propagation semantics](https://github.com/systemd/systemd/blob/v257/man/systemd.unit.xml)
and [bounded service stop behavior](https://github.com/systemd/systemd/blob/v257/man/systemd.service.xml).
Unknown or missing properties fail closed; synthetic property strings do not
qualify an installed systemd version.

The only effect arguments are:

```text
--system --no-pager --no-ask-password --job-mode=fail stop -- UNIT
--system --no-pager --no-ask-password --job-mode=fail start -- UNIT
```

Each uses the exact pinned local executable with no shell or remote manager.
Cancellation kills and joins the command's process group. Systemd may already
have accepted the request, so cancellation never refunds an action.

After stop consumption, resume may observe the same previous generation as
inactive/failed with no main/control pid or pending job. A genuine cgroup-v2 root,
empty `cgroup.procs` and `cgroup.events populated 0` must prove all descendants
joined. A disappeared unit cgroup is accepted only after authenticating cgroup v2;
lost generation fields, another invocation or remaining descendants refuse start.
The join window is finite across process restarts. Missing producer output after
the consumed stop may be expected, but the current monitor must retain the exact
unrecovered episode and policy. Changed or recovered producer evidence still
refuses start.

The separate start allowance is synced before the command. The exact old join,
incident, policy, physical custody, clocks and expiry are rechecked afterward.
An acknowledged start must retain a new exact InvocationID/PID/monotonic start.
Completion additionally requires that generation to publish an actual fresh
steering outcome, with a new instance and start time after consumption; a fresh
heartbeat alone is insufficient. A returned `read_wait` proves responsiveness
without asserting steering or chain success.

## Durable ownership and uncertainty

A common installed-unit `.sn-control.lock` excludes cooperating active repair,
stopped repair and activation processes while their commands are owned. Separately,
two permanent exclusive files derived from unit fragment, boot and previous
InvocationID bind the exact active envelope and independent key:

```text
UNIT_FILE.sn-active-BOOT_WITHOUT_DASHES-INVOCATION.claim
UNIT_FILE.sn-active-BOOT_WITHOUT_DASHES-INVOCATION.claim.lock
```

Their name excludes incident ID and journal path. A newly signed envelope for
the same generation cannot create a second claim. Losing either artifact refuses
both resume and a fresh alternate-path claim. The stopped-repair controller also
refuses to take over that claimed generation after the active controller exits.
The per-envelope journal and its permanent `.lock` retain the complete original
approval, wall-clock high water, observation count, consumed stop/start cuts,
join receipt and any acknowledged replacement. Missing or changed markers/state
refuse effects. Ambiguous publication poisons that owner until a fresh reopen.

An unacknowledged stop is never repeated. Within the original bounds, a later
owner may observe the exact old generation joined and proceed. An unacknowledged
start is always `uncertain-consumed-start`; a visible new process cannot supply
the missing acknowledgement. Another path, signature or deleted artifact cannot
be used as an automatic retry. Failed prerequisites, exhausted bounds or expired
authority may leave the process stopped and require manual host reconciliation.
Headroom reserves authority time; it cannot guarantee recovery during host or
source failure.

Exit 0 from `claim` establishes custody only. Exit 0 from `resume` means a
historically acknowledged replacement published responsive steering, possibly on
an earlier invocation. It is not current health, economic correctness, canonical
chain success or recovery of every domain. Pending/refused/uncertain results use
exit 3; invalid inputs or signatures use exit 2. `status` reads retained evidence
and issues no service command.

## Remaining production gates

Root, the service manager/kernel, protected filesystem and producer UID remain
trusted. Checks are current observations, not an atomic systemd compare-and-stop
operation: the worker can recover after the final read, and a privileged actor
can change a unit between observation and action. The independent operator must
approve this detection/recovery policy and provide exclusive deployment/service
custody. The local files do not fence another host, a duplicate signer, privileged
rollback or coordinated deletion/replacement of all retained evidence. External
anti-rollback and backup policy remains required.

Qualify exact installed systemd property formatting, dependency behavior, real
kill/join semantics, process/source association, cancellation and crash recovery
on a disposable deployment before issuing a production envelope. Deploy the
approved monitor budgets, independent expected-host alerting and delivered
on-call escalation. Keep public activation, chain signing, protocol liabilities
and the broader root/operator recovery gates separate.
