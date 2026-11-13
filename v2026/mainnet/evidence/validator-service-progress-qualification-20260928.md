# Validator progress producer qualification

Astra authored source `c67f8471431bfdfb09766d2ea7cc94e151110248`; Terra medium
qualified it and root integrated it as `15446f82d65dc3db5d506fc202e180d385548fad`.
No live node, service, key, transaction or telemetry deployment was used.

The standard validator accepts an optional `--progress-file` outside protocol
state. Its bounded publisher observes actual intent and settlement owners
after custody release. Heartbeat, successful observation, useful progress and
acknowledged publication remain separate. Read outages and restart retain old
facts and their ages as unavailable evidence; exporter failures do not cancel
the validator. See [the wire and operational contract](../SERVICE-PROGRESS.md).

| Check | Result |
| --- | --- |
| Selected normal tests | 47/47 observed roots passed: protocol 0.047s, validator 13.843s |
| Selected race tests | 47/47 observed roots passed: protocol 1.070s, validator 69.595s |
| Four causal controls, both modes | All failed with the expected named assertions |
| Vet | `./protocol ./validator` passed |
| Integrated compile, no test bodies | `./protocol ./validator ./mainnet ./crv4 ./miner` passed |
| Changed-dependency integration seam | Original signed-intent authority projection passed normal 2.538s and race 15.478s |

The causal controls cover actual intent custody release, durable settlement
transition, retained good evidence after an initial restart read failure, and
ambiguous publication falsely refreshing success. They do not use setup errors,
panics, timeouts or races as the expected failure.

The integrated seam is deliberately narrow: receipt admission and shared
validator fixtures changed between the producer's base and integration. Its
genuine signed sidecar still projects the original authority after renewal.
The remaining unchanged producer results are reused rather than rerun.

Candidate and integration source stayed clean at their recorded heads. Source,
module graph and supplied causal inputs passed before/after checks. JSON events,
test executables, build information and actual package working directories are
retained under
`/mnt/data/sn-testnet/evidence/mainnet-service-progress-20260928/terra-qualification`.
The integrated checks are in its `integrated-composition` directory. These are
component results with recorded physical dependency paths, not a frozen final
release or a claim of delivered alerts.

Manifest SHA-256 values:

- Candidate source: `a3d1504cb621c5ed83a159ca28c2b8af864b1e76241daee15c3e9516599c4d73`
- Module inputs: `1c73aa444ce385ba12db9f15ec8cfdbd6d5b2c1c52c795ac1b6d886df22fd0f8`
- Causal inputs: `64a71a5a6ec6b1ffee9216b2afd3f420e58191e103f5ab46fc5cfd8dbb5b8b12`

MG-07 remains open. Subsequent separately qualified work adds the
[monitor consumer](service-monitor-qualification-20260928.md) and the native/
steering hooks in a genuine nonempty
[production continuation](production-continuation-candidate-20260928.md).
Other domain coverage, delivered alerts and the repair controller remain open.
Those subsequent receipts preserve their own exact source and scope; neither
extends this producer qualification into complete public startup or live acceptance.
