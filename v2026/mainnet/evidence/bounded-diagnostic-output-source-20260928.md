# Bounded diagnostic output candidate

Source is isolated in `sn-mainnet-bounded-output-20260928/sn`, based on
`56023b184877a7e70d7ae9d31e363ac248c9730c`. Receipt-prefix dependency
`22aca4ccf6ad8671baf2117dd06fc98e3969a2b6` is composed as `fbaa71fe`;
fixture-only successor `093272b62879928ffdc198d3709f016f6bb92cdf` is composed as
`06da5cbe`. The latter preserves the existing startup helper and passes an
explicit long-scan window into the same approval builder. Neither dependency
is authored diagnostic code or should be integrated twice.

The candidate replaces blocking monitor/production-validator diagnostic writes
with an instance-owned bounded exporter. Role workers, source files, checkpoint
and metric ownership remain independent of log delivery. Actual production
startup, steering, progress reporting, operator JWT-save failure and runtime
announcement sites enqueue closed facts. The actual `RunRelease` context wires
the receipt-cache once-per-runtime read/write notice even without a progress
file. These changes carry no signing, repair or protocol admission authority.

The optional producer extension stays within the existing 8 KiB wire and requires
consumer-first rollout. Local delivery counts cannot attest to Loki ingestion,
protocol progress, metric-file durability or delivered alerts. Idle log streams
may legitimately have old acknowledgments. Existing independent expected-source
and stale-file rules remain necessary. Linux daemon outputs require admitted
nonblocking descriptors or an explicit cancellation-aware embedding; ordinary
regular-file redirection is reported unavailable.

Deterministic source tests cover real full pipe/socket cancellation and flags,
per-domain count/byte budgets, immutable record custody, failed-prefix refusal,
counter saturation/restart, typed-nil/unsupported sink admission, actual monitor
role files under congestion, early lifecycle cleanup, producer clock/counter
continuity, actual startup/steering waits and public root cache diagnostics.
Positive delivery assertions use completed-write barriers before shutdown;
bounded Close is not assumed to flush every queued record.

Author-lane checks are compile-only and vet. Separate Terra
[qualification](bounded-diagnostic-output-qualification-20260928.md) now supplies
passing scoped normal/race coverage for all 76 selected roots, six causal
controls in both modes, vet and offline alert-rule checks. The original
maximum-wire fixture failure and its one-root correction remain separate.
Exact selected roots,
guard-disabled controls, physical source/module manifests, runner admission and
commands are retained at
`/mnt/data/sn-testnet/evidence/mainnet-bounded-output-20260928/`.
This is source evidence, not deployment or alert-delivery evidence.
