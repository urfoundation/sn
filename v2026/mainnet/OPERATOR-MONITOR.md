# Read-only operator journal monitoring

`monitor --services` can now observe the existing operator PostgreSQL journals
alongside its independent chain and validator workers. This is a production
read path under MG-07/PH-28, with [offline source qualification](evidence/operator-monitor-qualification-20260930.md). It does not load `st.yml`,
a signing key, a nonce owner, or an application database pool. No write, repair,
submission, or service activation capability is installed.

The producer adapter is `server/stmonitor.Read`. It reads the actual
`st_chain_sync`, `st_epoch`, `st_publish`, `st_transaction_intent` and
`st_transaction_attempt` projections. Each completed sample uses a fresh
connection and one repeatable-read, read-only PostgreSQL transaction. The
consumer independently supplies database/user, deployment ID, chain/genesis,
coordinator, declared operator role and a sorted census of one through four
account addresses. It observes unresolved reservations for those accounts
across older deployment IDs/coordinators: a replacement deployment cannot hide
an older account nonce liability. It checks contiguous retained attempt numbers,
counts and the intent's current transaction hash, including superseded attempts.
It never reads calldata or signed transaction bytes.

The database assertions are **not authenticated chain evidence**. The legacy
settlement namespace is chain ID plus coordinator and lacks a genesis
attestation; its operator role is declared independently, not established by
these rows. The adapter does not verify signed bytes, account authority,
canonical receipts, settlement conservation, liabilities or current protocol
deadlines. Missing mirrors remain unknown. A complete empty pending census says
only that no unresolved row exists in the selected database scope. It cannot
establish successful transaction execution or healthy deployment.

## Admission and bounds

Add an optional `operators` array to the existing services v1 policy. Deploy a
compatible consumer before adding this field. Existing validator-only policies
remain valid. The shared policy limit remains 16 KiB, with at most eight
validators and four operators. Role names and every input/output/lock path must
be distinct. The following source is entirely synthetic:

```json
{
  "schema": "urnetwork-mainnet-monitor-services-v1",
  "validators": [],
  "operators": [{
    "role": "operator-a",
    "database_file": "/etc/sn-mainnet/operator-a.url",
    "database_sha256": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
    "pending_warning_seconds": 90,
    "pending_critical_seconds": 120,
    "expected_source": {
      "database": "operator_database",
      "user": "operator_observer",
      "deployment_id": "synthetic-deployment",
      "chain_id": 964,
      "genesis_hash": "0x2222222222222222222222222222222222222222222222222222222222222222",
      "coordinator": "0x3333333333333333333333333333333333333333",
      "operator_id": 1,
      "accounts": ["0x4444444444444444444444444444444444444444"]
    }
  }]
}
```

The credential file is a private protected regular file of at most 8 KiB. Its
exact bytes, including any newline, must match the independently supplied SHA-256.
Its explicit PostgreSQL URI supplies one host, port, database, user and password.
Remote connections require `sslmode=verify-full`; `sslmode=disable` is accepted
only for an explicit loopback IP. No multihost fallback or arbitrary connection
options are accepted. The optional `sslrootcert` path remains a deployment-owned
TLS trust input. Use dedicated scoped credentials, not the application vault.
The observer refuses elevated role flags, role memberships, and detected write
privileges on application tables. PostgreSQL enforces read-only transaction
execution as well. This check is not an independent audit of a hostile database
or every possible future role capability; review the deployment ACL and TLS
trust inputs separately. Grant only the necessary table/column SELECT access.

One observation has a 15-second caller-owned deadline, with a 10-second statement
timeout, 1-second lock timeout and 15-second idle transaction timeout. It reads
at most 256 unresolved intents, 16 attempts per intent, and 256 pending publish
rows. A one-row sentinel detects overflow. These are observer capacity limits,
not transaction authorization or lifecycle limits; overflow is `capacity`, with
no partial snapshot. Database query plans still require representative load
qualification: returned rows are bounded, while server scan/sort work is bounded
by the statement deadline rather than a row-work guarantee.

Cancellation owns rollback/close through a fresh 20-second cleanup budget and
joins pgx's separately bounded 15-second asynchronous connection cleanup. There
is no abandoned query goroutine or reconnect while the prior owner is cleaning
up. Filesystem operations remain byte-bounded and joined, as in the existing
validator consumer; a kernel filesystem stall is not a fabricated timed read.

## Progress and incidents

Successful DB access, original pending creation time, latest row update time,
mirror update time and observed scan movement remain distinct. A fresh read or
repeated mirror update cannot reset the monitor's scan-progress time. Pending
age uses the original retained creation time; rewriting it for the same oldest
intent/publication is refused. Account/nonce substitution, backward cursor/hash
changes and future/rollback clocks preserve the last accepted record and open a
read incident. These finite summaries do not compare every historical pending
row across restarts or detect an administrator deleting the whole journal.

The existing monitor's joined worker, protected lock, atomic checkpoint/fsync,
textfile metrics and bounded diagnostic exporter are reused. Each operator has
`monitor.operator-ROLE.json` and `monitor.operator-ROLE.prom` beside the existing
outputs. The checkpoint is bounded to 32 KiB and separately binds the exact
source and role. Restart cannot refresh a sample or manufacture a current read.

Four fixed incident domains are retained: read, scan, transactions and
publications. First/latest episodes preserve stable IDs, first/latest problem
observations, counts and accepted-snapshot hashes. A completed fresh DB read
recovers only the read incident; unknown transaction/publication/scan facts
cannot close their older incidents. A completed census can recover a **ledger
observation incident**, with its snapshot digest, without proving chain success.
Recurrence creates a new sequence while retaining the first episode. These
bounded local summaries are not a complete or replicated incident timeline;
external durable event retention, incident ownership and acknowledgment remain
open. Restoring an old checkpoint can restore old counts.

Metrics use only the independent `role` label and the
`sn_mainnet_operator_` prefix. Retained counts require `read_current=1` and the
applicable domain's `known=1` before they describe this sample. Capabilities
`independent_rpc`, `canonical_receipts_verified`, `mirror_genesis_verified`,
`operator_authority_verified`, `provider_readiness_known`,
`client_key_readiness_known`, `liabilities_known`, `protocol_deadlines_known`,
`root_validator_known` and `repair_authorized` remain explicitly zero. There is
no aggregate healthy gauge. JSON event schema is
`urnetwork-mainnet-operator-event-v1`; IDs and hashes never become metric labels.
Metrics report prior completed publication acknowledgment; an ambiguous
post-rename sync cannot acknowledge success. Stale and absent sample alerts
remain necessary even when the retained pending count is zero.

[Example alert rules](operator-monitor-alerts.example.yml) need an independently
provisioned expected host/role roster and real delivery rehearsal. Database
privileges, TLS/credential distribution, production query plans and capacities,
SLO/age thresholds, compatible rollout/rollback, supervision, alert delivery,
provider/client-key proof domains, full settlement/liabilities, pending native
work and actual deadline reconciliation remain launch gates. Root validator
progress and same-finalized-block independent RPC comparison remain separate.

Offline qualification passed 62 Go root executions in normal/race modes, four
alert fixtures, and nine normal plus five selected race causal controls. It used
real disposable PostgreSQL and the production reader/consumer with synthetic
reduced-column tables. It does not rehearse the full production migration chain
or establish any live database/chain identity.
