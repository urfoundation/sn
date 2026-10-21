# Validator read incident continuity

The `monitor --services` worker retains read outages in each role's existing
private checkpoint. Previously, a successful source read cleared `outage_since`
and left no durable outage identity, recovery record or recurrence count. The
new `read_incidents` state keeps bounded continuity after recovery and restart.
This MG-07/PH-28 increment is integrated at `1bb311fc`; its
[qualification receipt](evidence/read-incident-continuity-qualification-20260929.md)
records all 82 affected roots and six causal controls in normal/race modes.
Deployment, delivered alerts and the complete incident store remain pending.

One contiguous sequence of `missing`, `unavailable`, `changed`, `invalid`,
`identity` or `clock` reads opens one incident. Its ID hashes a domain tag, the
independently configured role and original producer identity/config, the durable
incident sequence, the original outage boundary and the first retained failure.
Changing error class or restarting does not create another incident. A failed
read increases `failed_reads`; the initial failure is included, so subsequent
failed attempts are that count minus one. Rendering or redelivering an event
does not modify the checkpoint or its counts.

The first successful exact-source read closes that **read incident**. Recovery
retains the incident ID, sequence, observation time, actual config and instance,
original heartbeat and SHA-256 of `ValidatorProgress.Encode()` for the accepted
decoded record, including its trailing newline. This canonical digest is not a
hash of the original whitespace or an independent signature. Re-reading the
record does not change the first recovery evidence. A new failed read opens the
next incident; a config renewal preserves the old incident config and records
the new recovery config under the same producer role.

A readable record can still be stale, incomplete or report failed work. Recovery
does not assert service health, finalized receipts, fee sufficiency, protocol
success, incident resolution by an operator, or permission to repair or spend.
Native deadline incidents retain their existing unresolved status. No signer,
transaction owner, endpoint fallback, external alert route or repair executor is
added. Canceled unfinished reads create no incident transition.

The bounded history contains its observation baseline and producer identity,
lifetime episode/failure counts, the first and latest incident summaries, and
the latest recovery even while another outage is open. Failure counters saturate
at `uint64` maximum; a new episode refuses exhausted sequence space rather than
reusing an ID. Counts belong to the retained local checkpoint lineage, not a
host-independent database. Restoring an older checkpoint can restore older
counts. The first and latest summaries do not retain every middle episode.
Complete incident timelines still require separately qualified durable event
retention and ingestion; the bounded log exporter can report loss. Consumers
should set state from an incident ID, phase and failed-read count rather than
counting duplicate delivered JSON events as new failures.

Checkpoint schema `urnetwork-mainnet-validator-checkpoint-v3` retains the existing
16 KiB bound and atomic/fsync/lock ownership. The worker records the completed
read before publication; a failed checkpoint write leaves `checkpoint_current=0`
and cannot acknowledge a durable export. A later attempt retries the same in-memory
continuity. A crash can only restore the complete generation actually retained
on disk. This does not provide zero-loss host-failure replication.

Strict v1/v2 checkpoints migrate without inventing earlier history. A retained
open outage keeps its original `outage_since` and last known failed sample, even
if the first new read recovers it. `prior_history_unknown=true` marks missing
earlier failures/recoveries, and their counts remain unknown. A healthy legacy
checkpoint does not fabricate a previous incident or downtime boundary. A fresh
monitor starts prospective coverage at `started_at`; it makes no assertion about
time before that baseline. Legacy schema names containing new incident fields,
corruption, foreign producer identities and inconsistent counts/recovery are
refused. Older binaries cannot read v3: preserve checkpoints and qualify a
compatible forward recovery before rollback; do not strip incident history.

JSON service events keep their existing v1 envelope with optional
`state.read_incidents`. Deploy compatible log consumers before this extension.
Incident IDs, configs and record hashes are never metric labels. Ten fixed gauges
use the existing `sn_mainnet_validator_read_incident_` prefix and `role` label:

| Suffix | Meaning |
| --- | --- |
| `history_known`, `prior_history_unknown` | A local baseline exists; migration cannot reconstruct earlier observations. |
| `count`, `failed_reads` | Retained lifetime incident and failed-read counts, with the saturation/migration limits above. |
| `open` | The latest read incident has no successful-read recovery. |
| `first_failure_timestamp_seconds`, `last_failure_timestamp_seconds` | First retained failure and latest failed read. Clock rollback remains evidence rather than being reordered. |
| `last_recovery_timestamp_seconds` | Latest successful-read recovery, retained through the next outage. |
| `latest_failed_reads`, `latest_failure_status` | Failed attempts and final failure class of the latest incident; status uses the existing service codes. |

The qualified scope covers the real worker/restart/cancellation, corruption,
migration, recurrence, publication-ambiguity and finite-capacity regressions in
`monitor_read_incident_test.go`, plus the adjacent service/native-deadline/output
paths. A stopped observer and collector still require independent absent/stale
alerts. SLO approval, operator incident ownership and live alert delivery remain
separate MG-07 gates.
