# Continuous economic observation status

`observe-economic-conservation --follow` emits `operational_progress` in each
acknowledged sample. The command records its actual process ID, invocation start,
original policy and checkpoint path, acknowledged checkpoint hash, completed
sample time, parent sample attempts and delay before the next sample. These are
read-only observations. A retained file or process ID alone is not a liveness
claim, a transaction submission receipt or permission to repair a service.

Each native/vault component reports the acknowledged cursor, original observed
finalized boundary and read timestamp, current action, blocking dependency and
backlog to that boundary. A pending capture/replay says `deferred-audit`; a
current caught-up read says `pending-finality`; known retained integrity holds
remain holds. Other unavailable observations remain explicitly unavailable,
with their original error in the surrounding sample. They are not reclassified
as transport failures merely because an estimate cannot be made. The existing
Claim statuses, fee issue/pending state, entitlement root census, acceptance
assessment and exact financial counts remain independent evidence domains.

Preparation throughput measures acknowledged block advancement over elapsed
time in this invocation. Its anchor survives pending polls, including the
whole time spent inside a long native capture/replay. A successful read with
no advancement publishes no new rate. An unavailable source, integrity hold,
changed original identity or unordered clock withdraws measurements; recovery
requires a new comparable interval. Restart starts a new interval and never
counts downtime as work. Metrics publication I/O recovery keeps this same
observer and acknowledged financial progress; failed publication emits no
success JSON record.

`catchup_eta_seconds` is the exact rational ceiling of retained backlog divided
by observed preparation throughput. Its target is the last admitted finalized
boundary, not an extrapolated moving head. A current, comparable zero backlog
can have ETA zero without a measured rate. Missing targets, pending reads,
unavailable sources and unmeasured positive backlogs have a null ETA. Future
boundary and repair ETA are always separate null fields because this observer
has no measurement authorizing either prediction.

`local_finalized_observation_cadence` measures delivered finalized-block
advancement between ordered original local read/handoff timestamps. It is
separate from preparation throughput and from parent publication delay. It is
**not independent chain production time or consensus timing**. An unchanged
head has no new cadence measurement; the successful-read high-water mark still
rejects later out-of-order timestamps. The original reader can successfully
confirm an unchanged head without replacing its retained read timestamp; that
does not turn a current zero backlog into an outage.

The existing 36 `sn_mainnet_conservation_` gauges retain their semantics. This
extension adds the following fixed, unlabelled gauges. Consumers still require
a positive, nonfuture, sufficiently recent `sample_timestamp_seconds`; that
is completed-sample freshness, not a fabricated heartbeat or proof of a live
child process. The same prepared `conservation.prom` destination and 16 KiB
metrics owner remain in use; no new authority, file or activation flag is needed.

| Suffix | Meaning and required gate |
| --- | --- |
| `entitlement_census_present` | The independently selected census exists in this sample. |
| `provider_coverage_known` | The census exists and all coverage counts below have exact scalar representations. |
| `entitlement_finalized_roots`, `entitlement_complete_roots` | Original finalized roots and roots with admitted census; require both presence and coverage known. |
| `provider_measurement_roots`, `provider_measurement_providers` | Roots with original provider work/trial measurements and provider appearances summed across those roots, not unique providers across epochs; same coverage gates. |
| `closed_work_roots`, `closed_work_windows`, `clock_matched_windows` | Original closed-work roots, windows, and windows matched to their original clock; same coverage gates. |
| `complete_report_inventories`, `inventory_reports`, `signed_close_reports`, `registered_close_reports`, `close_amount_joins` | Admitted inventory, signature, registration and amount-join coverage; same coverage gates. No count alone proves complete traffic. |
| `provider_bytes_known`, `provider_bytes` | Exact sum of admitted provider completed bytes; requires census presence and its own known bit. |
| `provider_assignments_known`, `provider_assignments` | Exact original trial assignment sum; same independent known gating. |
| `provider_confirmations_known`, `provider_confirmations` | Exact original trial confirmation sum; same independent known gating. |
| `native_backlog_known`, `native_backlog_blocks`; `vault_backlog_known`, `vault_backlog_blocks` | Backlog to each retained admitted finalized boundary. It may remain visible during an outage; currentness is separate. |
| `native_catchup_eta_known`, `native_catchup_eta_seconds`; corresponding `vault_` names | Current measured estimate to that retained target only, or current known zero. Require the component's ETA-known bit. |
| `native_rate_blocks`, `native_rate_window_seconds`; corresponding `vault_` names | Actual preparation blocks and upward-rounded elapsed seconds. Both must be positive. JSON retains the exact nanosecond interval. |
| `native_cadence_known`, `native_cadence_milliseconds`; corresponding `vault_` names | Local finalized-observation delivery cadence in upward-rounded milliseconds per block. Requires its own known bit; not chain production cadence. |

Coverage and trial counters can describe partial admitted evidence. Do not gate
them on the existing `provider_measurements_authenticated` bit and hide a real
gap. That bit still requires a complete observed root census and original
provider measurements for every finalized root. Known zero trial counts require
an actual admitted measurement; absent authority is unknown. A ratio of aggregate
confirmations to assignments is **not** a per-provider reliability score: the
original reliability rules also bind provider identity, minimum assignments,
eligibility and exclusions. Those exact originals remain the authority.

Decimal counts use canonical integer parsing. Values above 2^53 keep their exact
JSON strings and withdraw only the corresponding scalar projection. They are
never rounded into a plausible Prometheus value or rejected as invalid financial
evidence. Unsigned coverage counters use a shared exact-representation bit;
preparation, backlog, ETA and cadence have independent projection guards.

This status closes the combined observer's supported coverage/progress export
gap. Full PH-15 still includes other owners' actual submission/finalization
receipts, durable incident recurrence/resolution records, future-boundary
measurements where available, and operational outage/deadlock rehearsals. Those
must be evaluated against their real producers. No combined conservation count
substitutes for them. Production also needs the original provider/attempt/window
authorities, owned durable roots, active collector ingestion, independently
rostered alert receivers and verified on-call delivery. Missing deployment or
measurement evidence remains explicit; this command creates none of it.

## Principal original retention

Incomplete principal causes can remain unknown while later native rewards are
observed. The optional `principal_retention` object in the original conservation
policy selects automatic cold retention under the existing checkpoint writer:

```json
{
  "principal_retention": {
    "schema": "urnetwork-original-principal-retention-v1",
    "trigger_principal_facts": 256,
    "preprovisioned_archive_paths": [
      "/durable/conservation/principal-0001.json",
      "/durable/conservation/principal-0002.json"
    ]
  }
}
```

These paths are examples. Select actual canonical absolute paths under the
declared durable volume and provision each snapshot and private lock with the
same physical storage profile as the checkpoint. Slots must be distinct from
the checkpoint, policy, metrics and fee-request owners. The positive threshold
must be at most half the original `maximum_facts`; the example therefore needs
at least 512 facts. The object is included in the exact policy hash. Omitting it
preserves the existing policy bytes and manual archive behavior.

`observe-economic-conservation --follow` checks this threshold between completed
native attempts. It retains and rereads the exact current checkpoint in the next
unused slot, reconstructs the candidate archive under held snapshot owners, then
publishes the compacted checkpoint. It does not cancel an active capture/replay
or replace that worker's original cursor. An interrupted attempt can reuse a slot
only when its bytes equal the same original checkpoint. A separately selected
manual `economic-conservation-archive` request may set
`"retain_principal_originals": true`; its existing writer-fence requirements
still apply.

The sample's `original_principal_execution.retained_original_effects` reports
the retained interval, unresolved block count and latest original stake queries.
Its `complete_through` stops at the first incomplete block, even if later stock
changes cancel numerically. Treasury availability remains the latest original
coldkey-wide API observation, counted once. Cold retention does not make
`approved_position_causes_complete` true or fill spendable income bounds.
Provider claims, unresolved captures, fees and their signatures keep their
existing independent retention rules.

This is finite storage management. Slot exhaustion, archive/index limits,
physical custody loss and malformed originals remain explicit refusals with
the original checkpoint intact. A single actual native batch that exceeds the
admitted fact or byte limit also remains a capacity outcome. Do not filter
foreign mutations from an approved global storage scope, increase producer
bounds without its original authority, or relabel pool changes as per-coldkey
causes to fit capacity. The implementation and source-reviewed tests do not
supply deployment configuration, economic approvals or executed qualification.
