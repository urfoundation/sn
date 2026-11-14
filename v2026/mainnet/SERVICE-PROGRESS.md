# Validator service progress

The optional validator status publisher exposes bounded operational evidence
from the actual V2 intent and settlement owners. It neither reads an intent
store independently nor authorizes a signature, retry, or chain result.

```
validator run --config=/etc/ur-validator/release.yml \
  --progress-file=/var/lib/ur-validator-observation/progress.json
```

Create the observation directory separately from the configured protocol state.
The destination must be an absolute canonical `.json` path with an existing
physical parent, outside `state_dir`. The parent must not be group/world
writable. The publisher holds a private process lock and refuses aliases,
another publisher, replacement predecessors, malformed existing output, and
output belonging to a different deployment/validator/chain/genesis/netuid.
Published files are regular, atomic, at most 8 KiB, and mode `0644`; select
directory traversal permissions for the intended read-only observer.

## What the record means

The strict `urnetwork-validator-progress-v1` wire is defined in
[`protocol/validator_progress.go`](../protocol/validator_progress.go). Its source
contains the complete canonical configuration hash and deployment/validator
identity. An observed intent separately retains the hash of its original
authorized configuration, including after an approved configuration renewal.
The record contains no participant lists, vectors, signatures, credentials,
artifact paths, or raw errors.

The following are different signals:

| Signal | Evidence |
| --- | --- |
| `heartbeat_at` | The publisher took a process snapshot. It does not mean a protocol operation succeeded. |
| `publisher.last_success_at` | The previous acknowledged atomic publication completed, including directory sync. This acknowledgment appears in a later snapshot; the first snapshot can legitimately have none. |
| `intent.last_success_at` | The real intent operation and its physical custody close succeeded. `value` absent means known empty only with a fresh `current: true` observation. |
| `intent.value.progress_at` | The observed intent changed semantic status or finalized/applied boundary. Repeated reads and pending error updates do not reset this age or `created_at`. |
| `settlement.last_success_at` | The real advance/reconciliation completed successfully. |
| `settlement.cursor_known`, `epoch` | A coherent durable cursor was copied from the actual runtime owner. This can remain known when later public publication fails. Unknown is distinct from epoch zero. |
| `settlement.progress_at` | A previously known durable cursor advanced. An initial observation alone does not invent a progress time. |
| `target_epoch`, `pending_publications`, `first_pending_epoch` | Bounded outstanding settlement publication work from the same owned operation. |

Intent observations are emitted after real custody closes and after releasing
the intent owner. Settlement observations are copied under the existing gate
and emitted after releasing it. Each sequence rejects a delayed callback from
an older operation. Observing these fields acquires no new replay reader,
transcript census, signer, or background protocol worker.

A fresh successful observation is separate from progress: a validator can
legitimately wait for an epoch or a reveal block. Intents retain their actual
prepared, reveal, finalized, and application blocks. The qualified production
continuation hooks now populate the optional native schedule from authenticated
reads and the steering outcome from the actual loop. Failed reads retain the
previous observation with its original age and mark it unavailable. A consumer
must report absent or unavailable schedule evidence as unknown rather than
inventing a deadline or acceptance.

## Failure and restart

One joined worker owns publication. Ordinary I/O failures retry at 1, 2, 4, 8,
16, 32, then at most 60 seconds. Healthy publication uses a 30-second interval.
Validation, signing reconciliation, and other workers continue. An ownership
or path-integrity fault disables only this publisher. Parent cancellation joins
it; there are no detached workers.

Fixed stderr codes report `publication_unavailable_retrying`,
`publication_recovered`, `publisher_disabled_ownership`,
`publisher_disabled_configuration`, or `close_error`. No raw error or path is
copied into these events. A pre-rename failure leaves the previous file aging.
A post-rename sync failure can leave a fresh visible snapshot; the next attempt
reports `publisher.outcome: retrying` without advancing its previous successful
publication time. That distinguishes a fresh process snapshot from confirmed
publication even during repeated ambiguous writes.

Restart retains old values and ages but marks protocol domains unavailable
until their real owners observe them. If a fresh failure races ahead of the
prior-file read, that failure retains the old good evidence. Fresh successful
evidence wins, and a same-cursor observation preserves the old progress age.
Clock rollback remains visible in the timestamps for an independently clocked
consumer to diagnose.

## Scope and qualification

This slice adds the producer, command option, bounded wire, and actual owner
hooks. The separately qualified [service monitor](SERVICE-MONITOR.md) now adds
read-only consumption, Prometheus metrics, alert examples and independent role
polling. Miner status extension, telemetry installation and delivered alerts
remain open. Neither producer nor consumer qualification establishes deployment.

Deterministic tests exercise actual empty intent custody and physical close
faults, actual settlement closure/publication and cold disk restart, delayed
observations, output repair, ownership refusal, blocked I/O, joined cancellation,
ambiguous writes, and restart/clock ordering. A genuine nonempty production V2
`begin`/`update` fixture and typed native/steering loop hooks are now covered by
the separate [continuation qualification](evidence/production-continuation-candidate-20260928.md).
That scope preserves its original race-package timeout and targeted completion;
it does not establish the complete public startup or live economic outcome.

[Terra qualification passed](evidence/validator-service-progress-qualification-20260928.md):
47 selected roots passed normally and with race detection, all four causal
controls failed as intended in both modes, and vet passed. The integrated
packages compile together and the changed-dependency original-authority
projection passes both modes. Earlier compile/vet and bounded normal
investigations remain separate from final source qualification.
