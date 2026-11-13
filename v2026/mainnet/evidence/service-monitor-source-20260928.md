# MG07 service consumer source candidate

Follow-up: the corrected source completed normal/race, five controls and rule
qualification. See [the completed receipt](service-monitor-qualification-20260928.md).
The source-seal description below retains its original pending status.

Base: `52fb3b0f46eec3d69e3580e07526022f7d9dc208`. The qualified producer
`c67f8471431bfdfb09766d2ea7cc94e151110248` is inherited unchanged. This slice
changes the mainnet monitor consumer only; it does not edit producer, PH03,
contract, provider-payment, validator signing or runtime owner code.

The real `monitor --services` command owns independent joined chain/role workers,
strict bounded expected-source reads, retained role checkpoints, role-labeled
atomic metrics and closed JSON diagnostics. [SERVICE-MONITOR.md](../SERVICE-MONITOR.md)
documents the wire, unknown/current distinction, conservative outage boundary,
clock retention, publication acknowledgment and provisional alert policy.

Astra authored 21 new top-level command/file/metric regressions. The proposed
affected selection contains these plus 27 existing monitor/inspect roots.
Five explicit decision controls restore the saved startup-age bug or disable
source identity, stable intent progress age, acknowledgment discipline or role
routing. Each has a required failure literal from an actual command assertion;
compile failures, hangs and arbitrary nonzero exits are not causal evidence.

Author checks: the normal and controlled source compile with `-run '^$'`, and
`go vet ./mainnet` succeeds. These execute no test body. Normal/race bodies,
five causal controls in both modes and six Prometheus rule fixtures are handed
to the requested Terra medium lane. **Their results are pending at this source
seal; this document makes no passing-body claim.**

Exact local inputs and handoff are retained at
`/mnt/data/sn-testnet/evidence/mainnet-service-monitor-20260928/`:

- `causal-inputs/mainnet-outcomes.tsv`, `control-outcomes.tsv`,
  `control-literals.tsv`, and `overlay.json` declare the finite execution census.
- `resolved-dependencies-current.json` and `consumed-package-graph.json` retain
  actual Go resolution; `physical-module-seal.json` records resolved physical
  roots and exact clean Git identities, including nested local modules.
- `compile-final.log`, `causal-compile.log`, and `vet-01.log` retain author
  investigation outcomes. Temporary compiler work stays under the evidence
  directory's `tmp`, with the existing `/mnt/data/sn-testnet/gocache`.

All service fixtures use synthetic local files and HTTP endpoints. Remote rule
installation, Grafana/Mimir/Loki ingestion, alert delivery, process supervision,
protocol deadline policy and miner/operator progress coverage remain unproven.
