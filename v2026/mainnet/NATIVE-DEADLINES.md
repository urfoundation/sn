# Native submission window observation

The actual `monitor --services` worker can now forecast a pending validator
intent's native submission window and retain a reported miss. It consumes the
existing bounded progress file. It has no signer, receipt reader, repair action,
or authority to expire an immortal extrinsic.

Enable it independently for each validator role in `services.json`. Existing
policies retain their original scheduler when the profile is absent:

```json
"native_deadline": {"completion_margin_blocks": 5}
```

For newly approved v470 production, select the same reviewed profile as the
producer's independently signed approval and pin its exact expected config:

```json
"native_deadline": {
  "completion_margin_blocks": 5,
  "epoch_schedule_profile": "urnetwork-subtensor-tempo-drift-v1"
}
```

The profile is an explicit operator-policy choice. It is not inferred from a
runtime number, metadata shape or a testnet observation. Unknown profiles fail
policy admission. See the [source and causal qualification](evidence/native-economics-context-and-schedule-20261002.md).

Five is a synthetic example, not an approved production margin. Supply the
measured p95 completion/finality cost in native blocks from the admitted
workload. The accepted range is 1 through 50,400. A missing policy disables new
inference; it cannot clear retained incidents. No default timing assumption
activates deadline alarms. The existing expected source must still match every
deployment, validator, chain, genesis, netuid and config field.

The first possible next epoch is forecast from the producer's authenticated
native schedule: normal tempo, an earlier owner trigger, and the strict
`BlocksSinceLastStep > tempo` condition after a block's increment for the
explicit tempo-drift profile. An absent profile retains the original
`> MaxTempo` interpretation. Valid root-set u16 tempos above the owner setter
cap also retain their observed incidents through checkpoint reopening.
The forecast uses the selected rules in `crv4/schedule.go`. A deferred epoch
remains a forecast for the next block. A changed schedule can move that forecast;
neither wall time nor passing an old predicted block proves a miss. Warning
begins at the greater of the ceiling of 20% of the observed tempo and twice the
configured completion margin. Critical begins when remaining blocks are at or
below that margin. These alerts describe submission risk, not a chain result.

Inference requires fresh successful intent, native and steering observations
from a fresh accepted source. The steering result must be `receipt_pending`,
name the native observation's exact epoch, and have a timestamp strictly after
both the intent and schedule reads. Equal timestamps or mixed loop generations
stay incoherent. Missing evidence is unknown; stale, failed or rejected evidence
is unavailable. An empty, finalized, applied or failed intent has no inferred
submission deadline. A reveal block is an earliest prediction and does not
serve as an expiry boundary. Application/reveal and settlement deadline
observation need their own authoritative evidence and remain separate work.

A `missed-window` report requires the completed receipt search and native epoch
to be beyond the still-pending intent's original epoch. The production owner
reconciles canonical absence before emitting this receipt-pending result; the
observer records what that owner reported. The local progress file is unsigned,
so the finding is operational evidence for independent review, never independent
chain acceptance or proof that the signature ceased to be a liability.

The v2 per-role checkpoint retains the complete fixed-size intent/native/
steering evidence for the first and most recent missed window, their original
observation times, and a saturating distinct-epoch count. Repeated reads do not
add misses or refresh the first detection. Restart, outages, config renewal,
later healthy/application reports and disabling the policy cannot clear them.
Middle occurrences remain in the ordinary event stream. Checksums detect local
corruption; protected files are not an attestation against a compromised host.

There is intentionally no acknowledgement or automatic resolution endpoint in
this observer. A later applied report cannot turn a historical missed interval
into a timely result. Independent canonical receipt/history evidence, incident
review and an explicitly approved resolution mechanism are required before
clearing the operational page. That mechanism is not yet implemented; deleting
or rewriting a checkpoint is not a supported resolution. Preserve incidents
and arrange on-call handling before enabling this feature unattended.

Valid v1 checkpoints load without inventing prior deadline history and upgrade
to v2 on the next completed sample. Old binaries cannot consume v2; rollback
requires an explicit compatible migration retaining incident evidence. The
optional `native_deadline` event extension and state fields require compatible
log consumers before rollout. Each role still owns its independent checkpoint,
atomic metrics, bounded retries and joined worker. An ambiguous directory sync
does not advance the confirmed publication timestamp.

Metrics use the existing bounded `role` label. `protocol_deadline_known` and
`native_deadline_current` qualify current forecasts. `native_deadline_status`
maps disabled/unknown/unavailable/incoherent/not-pending/pending/warning/critical/
missed-window to 0 through 8. Additional gauges expose the observed native block
and epoch, original intent epoch, projected boundary, remaining blocks, actual
warning/critical margins, unresolved incident, count and first/last evidence
boundaries. There is no success/acceptance gauge. The independent unresolved
gauge remains set when current evidence becomes unavailable.

The [service rules](service-alerts.example.yml) include immediate warning and
critical forecast alerts, a retained missed-window page, and a five-minute
unknown-evidence warning. [Deterministic rule fixtures](native-deadline-alerts.test.yml)
cover thresholds, role separation, absence and retained incidents. Keep the
independent missing-host and stale-sample rules: no local metric can deliver its
own dead-man alarm. Production margin approval, deployment, external collection,
alert delivery, native acceptance and incident resolution remain live gates.

Source regressions cover physical progress files through the real monitor
command, exact schedule boundaries, deferred epochs, completed versus failed
receipt reads, old/mixed source records, restart/renewal/late application,
ambiguous checkpoint sync, role isolation, legacy migration and corruption.
[Sol qualification passed](evidence/native-deadline-qualification-20260928.md):
all 35 selected roots pass normally and with race detection, all five causal
controls fail at their required assertion in both modes, and the new plus
inherited service alert suites pass. Author compile/vet and the executor's
source/module fences also passed. These component results do not establish
deployment, delivered alerts or live native acceptance.
