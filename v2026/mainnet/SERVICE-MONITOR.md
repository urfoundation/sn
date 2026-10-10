# Read-only validator service monitoring

`sn-mainnet monitor --services /absolute/path/services.json` observes the
[producer's bounded operational file](SERVICE-PROGRESS.md) alongside the
existing chain monitor. Both `--checkpoint` and `--metrics-file` are required.
One joined worker observes the chain and one worker observes each configured
validator role. A chain read using its full retry window cannot delay service
reads. A blocked role read or ordinary publication error cannot stop its peers.
The observer never opens an intent store, acquires a signing owner, performs a
repair, or treats producer reports as independent on-chain acceptance.

The strict policy is at most 16 KiB. Validator-only policies contain one through
eight roles. The optional [operator journal extension](OPERATOR-MONITOR.md)
allows at most eight validators and four operators, with at least one total role. Supply
the expected source from the approved deployment configuration, independently
of the candidate file. All six identity fields must match exactly. The chain
id and genesis must also match the command's explicit chain expectation.
This synthetic example illustrates the shape; its hashes are not approvals:

```json
{
  "schema": "urnetwork-mainnet-monitor-services-v1",
  "validators": [{
    "role": "alpha",
    "progress_file": "/var/lib/sn-validator-alpha/progress.json",
    "expected_source": {
      "config_hash": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
      "deployment_id": "synthetic-deployment",
      "validator_id": 1,
      "chain_id": 964,
      "genesis_hash": "0x2222222222222222222222222222222222222222222222222222222222222222",
      "netuid": 25
    }
  }]
}
```

For example, add `--services /etc/sn-mainnet/services.json` to the existing
monitor invocation with `--checkpoint /var/lib/sn-mainnet/monitor.json` and
`--metrics-file /var/lib/fluent-bit/textfile/monitor.prom`. The command also
writes `monitor.validator-alpha.json` beside the checkpoint and
`monitor.validator-alpha.prom` beside the metrics. Each role has separate
ownership locks. Role names match `[a-z][a-z0-9_-]{0,31}`; duplicate roles,
duplicate producer identities and overlapping input/output/lock paths are
refused. Use one complete expected role census per host. Separate monitor
commands on the same host must not repeat these metric families and role labels;
different output filenames alone do not distinguish time series.

Precreate physical directories without group/world write permission. Source
files and metrics may be group-readable; checkpoints and locks are private.
Aliases, special files, empty/partial/oversized sources and replacements during
a read are refused. The source limit is the existing 8 KiB producer wire.
Atomic replacement between observations is expected. Reads own their descriptors
through identity checks and actual close. Regular-file work is byte-bounded and
remains joined; a kernel/filesystem stall is not disguised by an abandoned
timeout goroutine. Put these small files on a local filesystem. Independent
sample-age alerts remain necessary if a worker or the host stops making progress.

## What observations mean

A completed strict read, process heartbeat, prior acknowledged publication,
successful domain observation and actual protocol transition have separate
timestamps. Re-reading an unchanged record cannot refresh its heartbeat,
intent progress time or settlement progress time. A missing intent is known
empty only when its producer observation is current and fresh. Missing,
unavailable, stale or invalid input preserves the last accepted record, with
current flags cleared. Numeric zero without its current/known flag is unknown.

Current intent, native and settlement evidence must each be fresh. An incomplete
domain reports `unknown`; the absence of an error is not readiness. Native and
steering producer hooks are qualified separately. This consumer accepts their
existing wire fields but does not manufacture them. It exports pending counts,
original intent creation/progress timestamps, and prepared/reveal/finalized/
application blocks. Optional [native deadline observation](NATIVE-DEADLINES.md)
now forecasts the submission window from these native schedule fields and
retains a reported missed window after a completed receipt search crosses the
original intent's epoch. It needs an explicit per-role completion margin;
without that policy `protocol_deadline_known` remains zero. Reveal blocks are
earliest predictions, not expiry deadlines. Elapsed wall time alone cannot
make an old pending intent stalled. Miner progress ingestion remains later work.

Policy renewal may change the current config hash for the same deployment,
validator, chain, genesis and netuid. Restart retains the previous accepted
record while requiring a new read matching the independently expected current
source. Its intent still names its original config and creation time. A candidate
cannot rewrite the config, creation time or unchanged progress time of the same
intent vector. The observer grants neither configuration signing authority.

A first failure after a successful healthy period starts a new known outage at
that failed observation, not at process startup. Before any success, startup
bounds the initial outage. A persisted unresolved outage retains its boundary
across restart. A previously healthy checkpoint does not prove when an outage
began during downtime. Checkpoints retain consumer clock high-water and detected
future producer clocks; missing input cannot clear a clock incident. A valid
new observation is required for recovery, and consumer time must catch up to its
retained high-water within the allowed skew.

[Read incident continuity](READ-INCIDENTS.md) now retains stable outage IDs,
first/latest failure summaries, successful-read recovery evidence and recurrence
counts across restart. A successful read closes only its read incident; stale
producer evidence and unresolved native deadlines keep their own status. Legacy
history is explicitly unknown. The [qualified source receipt](evidence/read-incident-continuity-qualification-20260929.md)
records all 82 affected normal/race roots and six causal controls in both modes.
The v3 checkpoint still needs compatible rollback and log consumers; live
deployment remains pending.

## Steering responsiveness

An independent progress publisher can continue refreshing `heartbeat_at` while
the standard validator's steering owner is blocked inside a call. The optional
per-validator `steering_liveness` policy detects this separately:

```json
"steering_liveness": {
  "warning_after_seconds": 120,
  "critical_after_seconds": 300
}
```

These are synthetic example margins, not an approved production SLO. Explicitly
choose `90 <= warning < critical <= 86400` seconds from the maximum admitted
steering operation, poll interval, sample interval and workload qualification.
There is no enabled default and these seconds never become a native deadline.

The signal arms only after an exact-source, freshly acknowledged publication
contains a real steering outcome from the current producer instance, observed
within 90 seconds. The producer updates `steering.observed_at` after its actual
loop call returns. A fresh `read_wait`, `receipt_transport_wait`, `epoch_wait`
or `reveal_wait` demonstrates responsiveness even when no protocol work succeeds.
Unchanged intent age, native block or settlement cursor alone cannot open a
steering incident. A first startup, old restored outcome or replacement instance
without its own returned outcome is explicitly unknown.

After this baseline, a fresh publisher with a stale or missing steering outcome
warns at the explicit first margin and opens a distinct critical incident at the
second. Repeated reads, `starting` reports from that same instance, and other
domain progress cannot advance the retained outcome time. A changed start time
or regressed outcome for an armed same instance is refused as integrity/clock
evidence. Read outages and unconfirmed publication remain separate: they cannot
create a new steering-stall finding without fresh source evidence.

Checkpoint v4 retains the last responsive baseline, first detection time,
incident count and latest episode's ID, original margins, failure and recovery
cuts. An open episode survives monitor restart, missing files, new publisher
heartbeats, policy edits/removal and clock incidents. Recovery requires a fresh
actual steering outcome strictly after the detection cut. It resolves only loop
responsiveness; a returned failed read can recover that signal while native,
intent, settlement and read incidents keep their own meanings. Earlier complete
episodes require external event retention. Removing policy prevents new findings
and recovery; it does not acknowledge an existing episode.

The `steering_liveness` event projection and twelve
`sn_mainnet_validator_steering_liveness_*{role}` gauges expose enabled/current
state, last outcome, explicit margins, retained baseline, incident count,
unresolved state and detection/recovery times. Status codes are 0 disabled,
1 unavailable, 2 unknown, 3 responsive, 4 warning, 5 stalled and 6 incoherent.
Service status 14 is steering-stalled. The dedicated critical alert follows the
retained unresolved gauge even during a later source outage. Keep the independent
expected-host/role roster and missing/stale sample alerts: local liveness metrics
cannot report their own absent host or stopped monitor.

This is bounded local diagnostic evidence. It does not independently attest to
an active systemd generation, diagnose a deadlock's cause, prove chain failure,
or authorize stopping/restarting a process. The [repair controller](VALIDATOR-REPAIR.md)
still requires its independently signed stopped-generation availability incident;
a readable steering stall cannot substitute. Active-hang intervention still
needs a separately approved stop/join/custody policy and real-host rehearsal.
The [active repair interface](ACTIVE-VALIDATOR-REPAIR.md) now implements that
separate one-generation signed envelope and bounded custody; it cannot obtain
authority from this diagnostic or its alert.

Deploy v4 consumers before writing v4 checkpoints. The loader accepts v1–v3 with
their original unknown liveness history; existing independently signed v3 repair
envelopes retain their exact stopped-only scope. Old strict consumers cannot read
v4, so rollback must retain a compatible consumer and the original journals.
The [qualification receipt](evidence/steering-liveness-qualification-20261001.md)
records deterministic source evidence, not deployment or delivered alerts.

## Publication and independent telemetry

Per-role checkpoints are at most 16 KiB; textfiles contain 86 fixed role gauges within
32 KiB. The only new metric label is the independently configured `role`.
Config/deployment identifiers, paths, vector hashes and raw errors are never
labels. JSON events use `urnetwork-mainnet-validator-event-v1` and preserve the
bounded source/intent evidence for the existing log pipeline. Role read failures
deliberately expose closed classifications rather than raw filesystem details;
policy admission retains wrapped causes for operator inspection. This change
does not add a log service or configure Loki ingestion.

Every gauge begins with `sn_mainnet_validator_`:

| Suffix group | Meaning |
| --- | --- |
| `sample_timestamp_seconds`, `read_last_success_timestamp_seconds`, `read_current`, `read_outage_started_timestamp_seconds` | Completed consumer observation, successful exact-source read and unresolved read outage. |
| `read_incident_*` | Durable first/latest failures, read recovery and recurrence; see [read incident continuity](READ-INCIDENTS.md). |
| `status`, `severity`, `clock_fault_timestamp_seconds`, `candidate_heartbeat_timestamp_seconds` | Closed diagnostic state, severity and rejected clock evidence. |
| `export_status`, `export_last_success_timestamp_seconds`, `checkpoint_current` | Previous acknowledged complete export and current checkpoint result. |
| `has_record`, `source_current`, `source_config_current`, `heartbeat_timestamp_seconds` | Retained-record presence, fresh exact-source evidence, config match and producer heartbeat. |
| `producer_publish_status`, `producer_publish_last_success_timestamp_seconds` | Producer's prior publication acknowledgment, distinct from this monitor's exporter. |
| `intent_current`, `intent_known_empty`, `intent_present`, `intent_original_config`, `intent_status` | Current observation versus retained pending/completed intent; original config differs from current policy. |
| `intent_observed_timestamp_seconds`, `intent_last_success_timestamp_seconds`, `intent_created_timestamp_seconds`, `intent_progress_timestamp_seconds` | Observation success, original pending age and actual producer-reported lifecycle progress. |
| `intent_prepared_block`, `intent_reveal_block`, `intent_finalized_block`, `intent_application_block` | Retained original intent boundaries, with no inferred acceptance. |
| `native_current`, `native_last_success_timestamp_seconds`, `native_epoch`, `native_block` | Authenticated scheduler input reported by the producer, possibly unknown/stale. |
| `settlement_current`, `settlement_cursor_known`, `settlement_last_success_timestamp_seconds`, `settlement_progress_timestamp_seconds` | Current observation, retained known cursor and actual durable progress. |
| `settlement_epoch`, `settlement_target_epoch`, `settlement_pending_publications`, `settlement_first_pending_epoch` | Durable closure and still-pending publication remain distinct. |
| `steering_current`, `steering_last_success_timestamp_seconds`, `steering_status` | Producer's classified loop outcome; absent native epoch stays unknown. |
| `protocol_deadline_known` | Current native submission forecast or reported epoch crossing, only with the optional deadline policy; never chain acceptance. |
| `native_deadline_*` | Per-role forecast, explicit completion margins and retained first/latest missed-window incidents; see [native deadline observation](NATIVE-DEADLINES.md). |

Status codes are 0 starting, 1 observed, 2 missing, 3 unavailable, 4 invalid,
5 identity mismatch, 6 clock incident, 7 stale heartbeat, 8 producer publication
uncertainty, 9 unknown domain, 10 failed intent, 11 source changed during read,
12 native deadline risk, 13 unresolved reported native window miss and
14 steering responsiveness warning/stall.
Severity is 0 none, 1 warning, 2 critical. Publication codes are 0 starting,
1 previously published, 2 retrying. Intent codes are 0 absent, 1 pending,
2 finalized, 3 applied, 4 failed. Steering codes are 0 starting, 1 working,
2 epoch wait, 3 reveal wait, 4 receipt pending, 5 receipt transport wait,
6 read wait, 7 complete and 8 hard error. Retained codes require fresh/current
flags before they describe current operation.

Role checkpoint/metrics errors retry at 1, 2, 4, 8, 16, 32, then at most 60
seconds. A new completed observation continues through ordinary export errors;
the other roles and chain worker keep running. A metrics snapshot reports the
previous confirmed export time because bytes cannot acknowledge their own
directory sync. A failure after rename may leave complete visible bytes but
cannot advance confirmed success. The next attempt reports retrying and
republishes a complete snapshot. JSON reports the outcome after the attempt.
Restart preserves existing textfiles until a real sample completes.

JSON events and diagnostics use the [bounded daemon exporter](../diagnostics/README.md).
Each expected role has two independent 16 KiB queue slots. A full log destination
cannot block sampling, textfile publication, or cancellation. Aliased stdout and
stderr share one writer; failed partial output disables that destination to
prevent record splicing. Zero-byte failures remain retryable. Linux daemon logs
must use journal/socket/pipe/terminal output or an explicit cancellation-aware
embedding. Regular-file redirection is reported unavailable and is never silently
treated as accepted logging. Finite CLI commands keep their original output.

The existing `sn_mainnet_validator_output_*{role,stream}` families describe this
monitor's prior delivery state. `stream` is `events` or `diagnostics`; status is
0 starting, 1 delivered, 2 retrying, 3 unavailable. `known`,
`last_success_timestamp_seconds`, `delivered_total`, `dropped_total`,
`dropped_bytes_total`, and `unavailable_total` are independent of protocol
observations. Counters saturate and reset with process restart. An idle diagnostic
stream need not produce recent records, so acknowledgment age alone is not a
stall alarm. Unsupported sinks have known unavailable state from admission.

The optional producer `diagnostics` wire extension exposes the same local states
through `sn_mainnet_validator_producer_diagnostic_*{role,domain}`. Domains are the
fixed census `startup`, `steering`, `progress`, `operator`, `runtime`; `current`
requires both a fresh producer source and diagnostic observation. Retained counts
remain available with current=0. Same-instance counter rollback is refused;
changed instance IDs permit fresh counters. Diagnostic timestamps participate in
clock checks and do not advance native, intent or settlement progress.

Deploy consumers first: upgraded consumers accept old records with diagnostics
unknown, while old strict consumers reject the new optional member. The complete
producer wire remains at most 8 KiB. Producer snapshots acknowledge earlier log
writes, never their own file publication. Startup/read waits retain closed cause
and phase categories and original epoch knowledge; JWT failures retain numeric
operator identity, and optional receipt-cache faults retain read/write stage.
No raw read error or custody content is formatted into these producer messages.
Chain event read/publication detail is likewise deliberately redacted to closed
descriptions; finite admission and local cleanup diagnostics retain wrapped causes.

Terminal policy, checksum or output-ownership faults cancel the composition,
join every launched worker, and close every admitted owner. Cleanup errors are
retained in operator diagnostics and return exit 3. The legacy chain monitor's
recoverable output result is supervised with bounded retry in service mode;
its integrity/configuration failures remain terminal. The no-services command
retains its original exit behavior, with close errors now reported explicitly.

## Provisional alert rules

[service-alerts.example.yml](service-alerts.example.yml) extends the existing
xops Fluent Bit textfile → Prometheus remote write → Mimir/Grafana path. The
checked xops planetoid configuration enables `node_exporter_metrics`' textfile
collector at `/var/lib/fluent-bit/textfile`, scrapes every 15 seconds and adds
`env`, `host`, `job` labels. No collector, remote receiver or rule installation
is performed here. Keep the existing [chain rules](monitor-alerts.example.yml)
alongside the service rules and [diagnostic rules](diagnostic-alerts.example.yml).
The diagnostic examples use provisional two-minute unavailable and five-minute
drop windows. They supplement the independent expected-source and stale-file
rules; they do not prove installation, Loki ingestion, or delivered alerts.

Supply `sn_mainnet_validator_expected{env,host,role}=1` from an independent
expected-service roster in the evaluator. A roster originating only on the
monitored host cannot detect that host disappearing. Matching includes the
role, so a healthy validator cannot conceal another missing validator on the
same host. If the actual routing labels differ, update both roster and rules.

Initial thresholds are **provisional**: heartbeat/domain freshness warns at
90 seconds, heartbeat becomes critical at 120 seconds, read outages warn at
120 seconds and become critical at 300 seconds, and future clocks allow
30 seconds. Example external rules detect missing/stale samples, explicit
severity, stale producer heartbeat, unknown domains and stale confirmed export.
An unknown-domain warning describes missing evidence, not a failed protocol
operation. These thresholds assume a 30-second sample interval and need
workload qualification before activation; a longer configured interval must
be reflected in the rules. Test the rules with
`promtool test rules service-alerts.test.yml`.

Installation, alert routing/delivery, remote ingestion, service supervision,
operator capture/miner coverage and measured production deadline margins remain open work.
Source and fixture qualification alone do not establish any of those outcomes.
