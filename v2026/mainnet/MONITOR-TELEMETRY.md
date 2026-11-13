# Monitor telemetry and restart continuity

`sn-mainnet monitor --metrics-file /absolute/path/monitor.prom` publishes
eleven fixed chain gauges and bounded diagnostic-delivery series alongside its
existing JSON events. Use
`--checkpoint /absolute/private/path/monitor.json` to retain finalized
continuity, the last successful read and the first unresolved outage across
restart. Both options are implemented in the real monitor command.

Optional `--services /absolute/path/services.json` adds independently sampled,
bounded validator roles using the existing producer status wire. See
[service monitoring](SERVICE-MONITOR.md) for exact expected-source policy,
separate current/retained evidence, per-role metrics and alert examples. A
blocked chain sample cannot suppress those service observations.

Long-lived event/diagnostic output uses the bounded exporter described in
[service monitoring](SERVICE-MONITOR.md). `sn_mainnet_monitor_output_*{stream}`
reports the standalone chain command's local delivery/drop state. Diagnostic
congestion never refreshes successful chain observations and no longer holds
the sampling worker. Regular-file daemon log sinks are explicitly unavailable;
textfile metrics remain independent. Ordinary finite CLI output is unchanged.

The textfile contains no endpoint, address, key, transaction hash or raw error.
The existing Fluent Bit `node_exporter_metrics` textfile collector can read it;
the example xops configuration uses `/var/lib/fluent-bit/textfile` and adds
`env`, `host` and `job` labels during remote write. Provision one monitor file
per host for this metric family. Multiple files with identical metric names and
host labels are not independent monitors.

Precreate a physical output directory owned by the monitor account, with no
group/world write permission and read/search access for the collector, such as
mode `0750` with the collector group. The monitor creates a private ownership
lock and publishes a mode `0644` file by synced atomic replacement. Temporary
files do not end in `.prom`. The file remains after shutdown so its sample age
continues to grow. The private checkpoint belongs in a separate location; its
path and lock cannot overlap the metrics output. The command compares physical
parent paths before opening either owner; a checkpoint parent alias is resolved
once so retargeting that alias cannot redirect later writes away from its lock.

A restart preserves an existing metrics file until a new sample completes. It
cannot clear a retained critical event or refresh stale healthy output simply
by starting. Only an absent file receives the zero-sample startup marker.

Every metric begins with `sn_mainnet_monitor_`:

| Suffix | Meaning |
| --- | --- |
| `sample_timestamp_seconds` | Completion time of the last sample, including a required continuity read; zero before the first sample of a new metrics file. |
| `last_success_timestamp_seconds` | Last complete identity/continuity read; zero if unknown. An unavailable read does not advance it. |
| `healthy` | One only for an `ok` sample. Always check sample freshness separately. |
| `status` | 0 starting, 1 healthy, 2 unavailable, 3 finality stalled, 4 identity mismatch, 5 finality conflict, 6 malformed/inconsistent RPC evidence, 7 checkpoint failure, 8 metrics failure. |
| `severity` | 0 none, 1 warning, 2 critical. |
| `has_finalized_evidence`, `finalized_block` | Whether a checked finalized position exists and its native height. Zero height alone is not evidence. |
| `finalized_progress_timestamp_seconds` | Last checked finalized advance; zero if unobserved. |
| `read_outage_active`, `read_outage_started_timestamp_seconds`, `read_outage_age_seconds` | Unresolved read-outage flag, original start and age at sample completion. These freeze if sampling stops. |

Read outages warn after two minutes and become critical after five; backward
clock movement escalates immediately. Identity/finality/integrity failures and
a stall at `--stall-after` carry explicit critical severity. A successful
identity/continuity read of an unchanged head updates the read-success time but
does not reset finalized progress. It can remain `finality-stalled`.

Checkpoint schema v3 can retain an initial outage with **no** finalized position.
It accepts valid v1/v2 records and writes v3 on the next change. It does not
invent a historical last-success timestamp absent from older records. Partial
positions, a claimed success without finality, invalid times and checksum
changes are refused. Downgrading the executable requires a compatible explicit
checkpoint migration. Missing fresh state remains a fresh monitor, not recovered
finality; backup/restore must retain the actual checkpoint.

Publication precedes each JSON event. A write failure emits a critical
`metrics-error` or `checkpoint-error` and exits. An error after rename can leave
a complete new file visible even though directory durability was not confirmed.
The process does not guess which generation survived; reopening validates the
complete retained checkpoint. External sample-age alerts still detect stopped
publication. These files are local evidence, not hostile-host rollback protection.

## Independent alerts

[monitor-alerts.example.yml](monitor-alerts.example.yml) supplies candidate
rules for missing hosts, missing first samples, delayed/stopped sampling,
future clocks and explicit warning/critical state. Install them in a separately
hosted evaluator and supply `sn_mainnet_monitor_expected{env,host}=1` from an
independent expected-host roster. A roster emitted only by the monitored host
cannot detect that host disappearing. The `unless on (env, host)` rule checks
each expected host, so another healthy host cannot conceal its disappearance.
Adapt these labels to the actual collector and keep them consistent across the
roster and monitor metrics.

Rules warn after 90 seconds without a new completed sample and page after two
minutes. RPC retries may still be running during that gap; the alert identifies
missing observation progress, not a proven dead process. No independent heartbeat
is fabricated while an expensive sample is blocked. Qualify the thresholds with
the actual RPC workload before activation.

The rules use standard Prometheus [alert evaluation](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/)
and [vector matching](https://prometheus.io/docs/prometheus/latest/querying/operators/).
`promtool test rules monitor-alerts.test.yml` exercises two-host disappearance,
stale healthy output, initial absence, clock drift and recovery. Go regressions
exercise the real command, checkpoint and atomic textfile boundaries, including
a blocked initial read after restart, aliased output/lock collisions, a
retargeted checkpoint parent, a slow successful historical continuity read and
a closed owner attempting to overwrite its successor's checkpoint.
The [qualification record](evidence/monitor-telemetry-20260927.md) retains
normal/race/rule results and the controlled pre-fix failures.

This supplies source and local qualification. Deployment, remote ingestion,
actual alert delivery, expected-host provisioning, independent monitor
supervision and complete protocol/resource domain coverage remain open MG-07
work. The service consumer adds producer-reported validator/settlement evidence;
it is not independent chain acceptance. No alert message, service activation or mainnet transaction is performed
by the implementation or its tests.
