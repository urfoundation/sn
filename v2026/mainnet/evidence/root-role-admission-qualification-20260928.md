# Offline root-role admission qualification

The selected mainnet source adds bootstrap-chain v3 offline admission for the
separate netuid-0 root validator. It requires an explicit root identity and
generation, an independently pinned action approver and a distinct approver for
the complete root-service configuration. A domain-separated signed approval
binds the child root plan, service configuration, deployment and strategy.
Preparation retains the verified public inspection while reporting live root
authority and service activation as pending. V1 and v2 journals retain their
original plan domains, hashes, result schemas and limited recovery scope.
There was no mainnet signing, RPC mutation, transaction or service activation.

Astra max implemented the change in isolated commit `812a8bbc395be3ca11c20c77c53c7c14cab11034`.
Sol medium tested the corrected commit `cbb560e414fb4a81d43eb04dd514046623836eb0` in a frozen physical source graph.
The root branch cherry-picked these as `0b461c30` and `54c784b5`; the
`mainnet/*.go` bytes at integration match the corrected tested commit.

Sol's full normal `./mainnet` gate passed **403 top-level roots / 482 test
executions**, with no skips. The exact 38 bootstrap-chain roots passed race
detection in three disjoint shards of 12, 14 and 12. Seven affected root
monitor output roots passed race detection, and `go vet ./mainnet` passed.
Before/after source, module graph and clean-status fences matched. The raw
[Sol result](/mnt/data/sn-testnet/evidence/mg08-root-role-admission-20260928/RESULT.md)
has SHA-256
`5a143c4041d371a29d5334bd9020df2fee0e5aaf259d82d84305342530c4509c`.

The first full normal attempt found five older metrics test fixtures that used
group-writable temporary directories under umask `0002`. The same five roots
failed on unchanged baseline `d3dadebe` under `0002` and passed under `0077`.
The fixture-only correction makes them pass under `0002`, without changing
production writer behavior or test assertions. The initial combined race
selector reached its 12-minute cumulative package timeout with no individual
failure or race report; its result is retained, and the disjoint shards above
cover its exact selected root census. Neither failed attempt is counted as a
pass. [Fixture details](root-monitor-fixture-permissions-20260928.md) are
separate from the v3 authority result.

This is an offline source qualification, not a composed production release or
deployed netuid-0 service. Mainnet chain identity, live current authority,
actual root stake/eligibility, custody, route and service health still require
independent evidence before activation.
