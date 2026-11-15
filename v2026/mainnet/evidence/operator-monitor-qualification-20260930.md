# Operator journal monitor qualification — 2026-09-30

The read-only operator journal producer/consumer is qualified offline on the
exact source pair below. MG-07/PH-28 remains open for deployment, unobserved
domains and bounded repair. No live database, chain, signer, spending, public
Safe submission or activation claim follows from this receipt.

| Repository | Qualified commit | Tree |
| --- | --- | --- |
| SN | `070ec76988bcc12a79a11f7cea57c3d812b61ac4` | `83f3c177eb5fcc9b184df5a03d48346a5d165dbf` |
| server | `99130c2d698a25dfb4ff8cc56681160ef273adbb` | `2ece82b6089c284b347e5b0babf92ad60aa4af26` |

The integration docs child changes only Markdown beyond the qualified SN
source. Production, test, alert and module bytes retain this paired identity.
Astra max owned implementation, debugging, fixes and static compile/vet; Sol
medium independently ran behavioral positive, race, causal and alert checks.
The author ran no behavioral tests.

The [scope](../OPERATOR-MONITOR.md) is `server/stmonitor.Read` and the actual SN
`monitor --services` consumer. They reuse existing monitor workers, protected
checkpoint/metrics publication and bounded output. The adapter reads original
transaction/attempt custody, settlement publication state and the operator's
retained event scan. These are scoped database assertions, not authenticated
chain evidence, complete storage, account authority or liability reconciliation.

## Results

All 62 planned Go root executions passed: eight server focused roots, seven SN
focused roots and sixteen SN adjacent roots, each normal and race. Every raw
stream has the exact root census, package PASS and exit 0, with no skip or race
report. The adjacent group includes the inherited policy census, separate worker
outages, blocked source/exporter, ownership/cancellation cleanup, clock changes,
ambiguous publication, durable read/native-deadline incidents and bounded output.

All nine normal controls were causal. Exactly five were also causal under race:
genesis scope, read-only role, scan progress, unknown-domain recovery and
credential digest. Attempt completeness, repeatable database snapshot, caller
cancellation and role census ran normal only. Each accepted control has its
sealed mutation, exact named assertion, selected root/package FAIL and exit 1,
with no build, setup, timeout or race confounder. Every control checkout was
restored to its exact clean source.

The four Prometheus alert fixture files passed with promtool 3.5.0 in a container
with networking disabled: chain, service, native-deadline and operator monitor.
The new operator file contributes three scenarios and four assertions. Rules
remain deployment examples; no collector installation or delivered page is claimed.

PostgreSQL ran in a separately owned disposable gate, owner
`cc3becce5929a886e4b8feb83e086c4c`. Each fixture root created its own synthetic
database and dedicated observation role. These exercise real PostgreSQL
permissions, repeatable-read snapshots and cancellation over reduced-column
fixture tables, not the full production migration chain. The checks cover
superseded attempts, old-deployment reservations, foreign genesis/account
exclusion, write/elevated-role refusal, row/attempt capacity, missing and swapped
attempt identity, cancellation/backend cleanup, a forced snapshot interleaving
and connection admission. SN checks include the actual command beside blocked
chain/validator workers, restart, original ages, source/clock/cursor changes,
checkpoint corruption, credential digest and ambiguous publication.

The owned PostgreSQL and Redis containers were removed through ID-verified
cleanup with exit 0. The independent author audit also verified the absence of
both exact container IDs. No ambient service was reused.

## Retained evidence

Sol's immutable evidence directory is
`/home/by/urnetwork/temp/mg07-corrected-validation-20260930/evidence`.

- `manifest.json` SHA-256: `fe370cdb70bbd2b54b0827211218628e80b3b53403a563c8a36fae6274b7e078`.
- Its 29-file `SHA256SUMS` SHA-256: `24a77055efc1d50bbb237473b81955cfb3acf4b7136b2bbcb62215dc376c5688`.
- Independent author audit `/tmp/operator-monitor-final-author-audit.json` SHA-256: `eae946cc5ba6655b83a270c14f84bb5a16128934c33dda5b881fb0afaddb375a`.

The audit verifies raw positive/control records, both clean positive/control
checkouts, module files/graphs, all fourteen local replacement entries, alert
results, cleanup and the earlier failed streams. All checksum entries pass.

The static handoff is `/tmp/operator-monitor-corrected-handoff-20260930`:

- 20-entry `STATIC-SHA256SUMS`: `153dbae37489980a87f791baa8678bea8f9a810e4052cfa614eb4e243a3b074e`.
- `SOURCE-FENCE.json`: `b2a95f8d8f820b91715914c33f083a3183598f0e6779c5b0bceb8bcbd2a806c6`.
- `CONTROL-CHECKS.json`: `961da6077c3c08665f269f2553866f4ecff7f4ca278b03883a8798c4305db8e6`.
- `CONTROL-SHA256SUMS`: `aea1a439365cd03787b7f9338a3b6f519750d2be52ea4266a3a3dd8c1949b9c8`.
- Full 41-entry `SHA256SUMS`: `1406ed9d27627d1765b86e40c4cd35ba97f8d0147b1eda0334d36fc27bdeae10`.

Both source packages and all nine mutations compiled and passed vet. Mutation
checks ran in a separate restored-clean paired checkout. The original static
seal remains unchanged; the compile/vet supplement is separately sealed.

The earlier SN `9605654c` attempt remains failed preliminary evidence. Its
fourteen focused roots passed normal/race, but both adjacent runs failed
`TestMonitorServicesPolicyBoundsAndStrictWire`: an inherited oracle required
obsolete message text despite correct rejection. The corrected source supplies
a closed census error and checks actual admission invariants, adding combined
and operator-only census boundaries. Original failed stream SHA-256 values are
`f35ae9b9068b7798b4a8861bf98d6344b7adda918fe5a6e2b27fe42881e2fc7f`
(normal) and `c87d81b31afbac754252bc8cd7b6c757e600069dbebea9a8e045e2b8b087c1de`
(race). They were not relabeled as corrected-source passes.

## Open gates

MG-07/PH-28 still requires compatible consumer rollout, dedicated read-only
ACL/TLS credentials, representative query-plan and capacity rehearsal, full
migration qualification, approved SLOs, independent expected-role rosters,
collector ingestion and alert delivery, incident replication/ownership and
primary/backup on-call rehearsal. Local first/latest incident summaries do not
provide a complete replicated timeline.

Same-finalized-block independent RPC, root-validator progress, provider/client-key
readiness, account authority, canonical transaction receipts, full settlement
and liabilities, pending native work and current protocol deadlines remain
unobserved. Their capability metrics remain explicitly zero. Empty pending
counts and recovered ledger-observation incidents do not establish chain success
or a healthy deployment. No repair or signing capability is installed.
