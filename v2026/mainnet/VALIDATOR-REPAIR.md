# One stopped standard-validator generation

`sn-mainnet repair-validator claim|resume|status` is a concrete, separately
authorized local service capability. `resume` can issue one blocking
`systemctl --system --no-pager --no-ask-password --job-mode=fail start -- UNIT`.
It never stops an active validator. Initial installation and activation, active
hang repair, dynamic Warp workers, operator/root services, protocol signing,
spending, transaction replacement and public Safe submission remain outside this
capability. [Offline source qualification](evidence/validator-repair-qualification-20260930.md)
is sealed: 52 positive executions passed normal/race; twelve normal and seven
selected race controls were causal. No unit has been installed or restarted by
this increment. MG-07/PH-28 remains open.

The existing Warp supervisor polls release/configuration versions, so restarting
that unit would not preserve a fixed release. This profile instead invokes the
existing standard-validator `run --config=... --progress-file=...` production
entrypoint directly. It requires an already installed dedicated unit, a previous
observed generation, existing production configuration/custody, and a current
open monitor availability incident. No missing journal, first boot or fresh
progress file supplies these prerequisites.

## Independent approval

All three commands require these explicit inputs:

```text
sn-mainnet repair-validator claim \
  --approval /ABSOLUTE/APPROVED/repair.json \
  --accept-approval-hash sha256:EXACT_FILE_DIGEST \
  --independent-public-key 0xINDEPENDENT_ED25519_PUBLIC_KEY
```

Use `resume` for a bounded observation/action step and `status` to read retained
custody. Exit 0 from `claim` means local custody only. Exit 0 from `resume` means
one acknowledged process generation published exact-source progress, possibly
on a prior invocation. It does not assert current health, canonical chain
success, economic correctness or resolution of other incident domains. Pending,
refused and uncertain dispositions return exit 3 with structured JSON; invalid
command/approval inputs return exit 2.

The schema is `urnetwork-mainnet-validator-repair-v1`. The independently supplied
key is not selected by the envelope. `signature_ed25519` is exactly 64 bytes of
lowercase hex. Its message is UTF-8
`urnetwork-mainnet-validator-repair-approval-v1`, a zero byte, then Go's compact
JSON encoding of `repairValidatorApproval` with the signature field empty. The
declared field order, every original checkpoint field and canonical timestamps
participate. The controller contains no signing-key loader or approval issuer.
The approver must review the exact encoded plan and independently establish the
key/host/release custody; approving a file hash alone is insufficient.

| Signed input | Meaning |
| --- | --- |
| `role`, `source` | Exact independently declared deployment, validator, config hash, native genesis, EVM chain 964 and subnet. |
| `machine_id`, `boot_id` | Exact host and boot. A reboot requires new independent authority. |
| `unit` | Fixed name, canonical unit-file hash, validator/config file hashes and absolute paths, existing state directory, progress path, non-root UID/GID. |
| `systemctl`, `required_mounts` | Pinned local systemctl executable and bounded sorted mount-unit census. The system slice and all mounts must already be active without a job. |
| `previous` | Independently recorded InvocationID, ExecMainPID and monotonic start of the stopped producer. The original monitor instance is retained separately. |
| `original`, `incident_id`, `monitor_checkpoint`, `monitor_uid` | Full original v3 monitor checkpoint and exact open availability incident; current reads must retain that episode, prior producer and expected owner. |
| `state_path` | One private physical root-custody journal path, including its permanent `.lock` marker. No alternate path is inferred. |
| `valid_from`, `expires_at` | At most 24 hours; fresh starts only inside the window. Historical reconciliation does not rewrite expiry. |
| `maximum_starts`, `maximum_observations` | Exactly one start, at most 1,024 bounded observations. Reopening does not replenish either. |
| `command_timeout_seconds`, `maximum_sample_age_seconds` | 1–60 seconds per manager call; 1–600 seconds for incident/progress freshness. |

The config pin preserves the existing validator's authority and journal paths;
it does not create a new signing policy. Runtime/config incompatibility must
still fail through normal production startup. Root/operator roles are not
accepted by this standard-validator profile.

## Installed profile and host ownership

`repairValidatorUnit.render` defines the exact unit bytes. Its installable
profile is illustrated by [the unit example](validator-repair-unit.example.service).
Replace every synthetic path/UID/name through an independently reviewed release,
and hash the exact rendered bytes. The controller neither copies the files nor
reloads/enables systemd. It checks protected physical path ancestry, root-owned
release files, signed hashes, loaded fragment/exec properties, no drop-ins,
no reload pending, `Restart=no`, and no hooks, dynamic environment files or
additional action-bearing dependency units.

`WorkingDirectory` creates implicit mount requirements; systemd also requires its
service slice. The controller accepts only the signed mount census and
`system.slice`, already active with no jobs. It does not mount volumes or start
another service to make prerequisites pass. This follows systemd's
[implicit execution dependencies](https://github.com/systemd/systemd/blob/v257/man/systemd.exec.xml)
and [slice dependencies](https://github.com/systemd/systemd/blob/v257/man/systemd.resource-control.xml).
Exact installed-version property formatting, unit execution, preserved stopped
fields and dependency census require an isolated real-systemd deployment
rehearsal; synthetic manager fixtures do not supply that evidence.
The loaded slice must be `system.slice`, and a real `statfs` of the fixed cgroup
root must identify cgroup v2 before any absent unit directory can mean empty.
V1, hybrid/unmounted and unavailable hierarchies are refused.

One trusted host deployment/custody owner must exclude concurrent administrative
starts, file/unit replacement and volume changes. The journal lock excludes
other controller processes using the same signed path. It does **not** lock out
another privileged actor between a pin/read and the start. Root, the service
manager/kernel, approved environment and producer UID remain trusted. This is
local operational evidence, not hostile-host attestation or complete storage
proof. Do not deploy until the service-control policy or shared deployment lock
provides that ownership. Rollback/copying of journals, changing state paths, or
issuing multiple envelopes for the same incident cannot be automated around.

The stopped snapshot requires MainPID/ControlPID zero, no pending job and the
retained signed ExecMainPID/start. An inactive InvocationID may have been cleared;
the other generation fields may not. A lost/garbage-collected generation is a
refusal, not a fresh installation opportunity. `cgroup.events` must report
`populated 0` for descendants, and `cgroup.procs` must be empty. An active process,
unknown cgroup or foreign generation prevents a start.

## Crash and postcondition behavior

The permanent marker binds approval/key before atomic synced state publication.
An observation is consumed before external reads. The one start is consumed and
synced **before** calling systemctl. A successful blocking call is followed by
exact release/manager re-admission and a durable receipt for the new
InvocationID/PID/monotonic start; that start cannot predate the consumed monotonic
boundary. Later invocations can reconcile only this acknowledged generation.

Immediately after the reservation sync, the controller rechecks expiry, clock
continuity, cancellation and the age of the exact admitted incident sample.
A slow disk cannot extend either authority window. Refusal at this boundary
keeps the start consumed without issuing the command or refunding its allowance.

A crash or command error after consumption but before that receipt is
`uncertain-consumed-start`. The output explicitly requires manual host
reconciliation and forbids automatic retry, journal deletion or a replacement
state path. Even a visible new process cannot supply the missing acknowledgement.
A canceled systemctl child is killed and joined; systemd may already have
accepted its request, so cancellation never refunds the start.

Completion requires fresh progress from the approved path/UID and exact source,
a new producer instance starting after consumption, an acknowledged publication,
and the same systemd generation after the read. The journal retains its record
digest and observation time. Missing/stale/mismatched progress remains pending;
another invocation cannot clear it. Completed receipts survive expiry/reopen
without consuming observations or issuing another start. Publication uncertainty
poisons that owner until reopen; no guessed rollback restores capacity.

Deployment still needs independent envelopes/key custody, root-only physical
state storage and anti-rollback/backup policy, the fixed unit and service UID,
existing validator signer/volume ownership, a real-systemd crash/cancellation
rehearsal, incident ingestion and delivered alerts/on-call. Active hangs,
root/operator service recovery, cross-host fencing, automatic uncertain-start
resolution and monetary repairs remain explicit follow-ups.
This first incident binding admits missing/unavailable read output only. A
readable-but-stale progress file is still monitored but does not grant this
repair action; admitting that failure class needs its own retained incident
and independently reviewed policy.

The [steering responsiveness monitor](SERVICE-MONITOR.md#steering-responsiveness)
now retains a separate stale/missing loop-outcome incident even while the
independent publisher remains fresh. That diagnostic grants no stop or restart
permission. This controller accepts compatible v4 monitor checkpoints and
preserves independently signed v3 envelopes; its admitted failure class and
required stopped/empty generation remain unchanged. The separate
[active steering-hang capability](ACTIVE-VALIDATOR-REPAIR.md) requires its own
independent stop/join/start signature and permanent generation claim. Both paths
share an installed-unit control lock; this stopped-repair controller refuses a
generation already claimed by active repair, even after that owner exits.
Production approval, deployment custody and real-systemd rehearsal remain open.
