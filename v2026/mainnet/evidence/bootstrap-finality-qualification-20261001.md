# Bootstrap native finality and refreshed EVM admission

These are local source qualifications on the owned-RPC trust model. They do not
approve a mainnet route, consensus checkpoint, runtime, release or deployment.

## Exact sources

| Change | Frozen SN source | Scope |
| --- | --- | --- |
| Native finality closure | `98df8b5f2b2dea8f6670b2d1b47cdb30af14fab4` | Identity, mapping, runtime and dependent bootstrap observations. |
| Closing-anchor fixture correction | `1d5ebe55d8bb92bfd5fa83358310a8d0067828af`, tree `fd96c3cd0ae7170022c67439e919c58ba56137e8` | Production bytes unchanged; all six native activation closing checks are exercised. |
| Refreshed EVM admission closure | `41f053abf86fb9e886be97fed4df74a9e74e63d0`, tree `542fc15302383bd2c8e5a62ef19a378a275e58bd` | Nine production lines close the second admission pass before send readiness. |
| Fixture naming follow-up | `7ceacf6b4ee89881b23f2342a0b56a2945fda2c7` | One local response map follows the house naming rule; production bytes unchanged. |

Both frozen author graphs use clean server
`64cde171bde27aa60972f79be4a0ea1138cb905c`. Connect and its SCTP fork are pinned
to `v0.0.0-20261001021459-e1b5d77b5029`; SDK is pinned to
`v0.0.0-20261001021058-5d37be3876e5`. The two checked-in SN forks come from the
unchanged `1d5ebe55` checkout. Other selected local replacement commits and trees
are retained and compared in each receipt's dependency identity files.
Go is `go1.26.6 linux/amd64`. Tests use `GOWORK=off`, the retained external
module file, `-mod=readonly`, exact selected roots and `-count=1`.
The native and EVM source worktrees remain separately frozen.

## Failure and behavior

Canonical membership alone did not establish that a retained block was still
covered by finality after a dependent read. The old historical identity reader
could publish native block 100 after the reported finalized frontier fell from
150 to 90. The EVM mapper could similarly retain native block 100/EVM block 37
after losing its original finalized block 150 witness during the EVM read.

The native reader now retains the original finality point authenticated by the
complete header in memory, separately from a historical selection. It closes
each observation with a fresh complete finalized header, the original witness's
canonical hash and the selected block's canonical hash. Normal advancement to
180 keeps native block 100/EVM block 37 selected. Regressed, replaced or malformed
evidence refuses publication; transport errors retain their read cause and
existing bounded retry owner. Cancellation cannot publish a partial identity or
mapping. The private witness is absent from serialized observation bytes and
cannot be imported as authority.

Runtime/metadata, root preview, subnet census/discovery, recycle mode, bootstrap
readiness, native validator prerequisites and stake admission close finality
after their own dependent reads. A successful earlier identity or census does
not authorize a later result. Existing contract and validator consumers retain
their final mapping checks.

Adjacent review found a distinct send-admission gap. After a healthy first-pass
advance from native block 100 to 101, `reconcile` returns its refreshed `admitCurrent`
result directly. That second pass previously read pending account/code state and
could set `SendReady` even when finality regressed at its final code reply.
The new closing mapping runs after all account/contract reads and before send
readiness. It preserves read errors and classifies a changed returned mapping as
an integrity contradiction.

The public-command regression injects four faults at the second pass's final code
reply: regressed native finality, replaced native block 101, replaced EVM block
38, and an absent finalized head. Every old-owner variant broadcasts once to the
synthetic fixture. The corrected owner retains the same complete signed-record
digest and consumes no attempt. After recovery, the original transaction sends once and
retains its native block 102/EVM block 39 receipt. Further healthy advancement
during the second pass also succeeds, with one unchanged transaction and a
native block 103/EVM block 40 receipt.

## Qualification and preserved failures

The author native successor passes the exact 166-root selected normal scope in
282.945 seconds. Its broad race command reached the 30-minute package limit after
155 completed passing roots, with no earlier assertion failure or skip. The exact
eleven-root unfinished complement then reached terminal PASS in 271.358 seconds
on the same frozen source. The failed package invocation and its completed bodies
remain distinct from that disjoint continuation. Ten separate source omissions
cover identity, mapping and eight dependent owners, with sixteen expected failing
root executions per mode. All twenty control commands reproduce their intended
assertions. The sealed receipt checks the exact root census, terminal package
statuses, exit codes, unchanged clean source and the retained module/dependency
identities:
`/mnt/data/sn-testnet/bootstrap-finality-bracket-astra-20261001/integration-20261001/receipt.json`,
SHA-256 `a0fd45ce9b009f2c7a548a5f642fdd8655f8d3f763430d91a3ba5e8165389c58`.
All 117 manifest entries pass readback; the manifest SHA-256 is
`ff1231dad9347883d9c792ba02340eb346a23e9fbf00e5b566f7f8579f772eed`.

The earlier author `adjacent-normal.exit=1` precedes fixture corrections already
in `98df8b5f`: added finality reads changed exact counters and numbered injection
positions. The later `final-normal.jsonl` is a terminal failure at
`TestValidatorActivationNativeClosingAnchorsDiscardPartialEvidence`, without a
separate exit file. It correctly returned nil evidence and `errRpcIntegrity`,
but demanded an obsolete error message at a lookup now belonging to the first
anchor's finality closure. `1d5ebe55` exercises every one of the six post-storage
canonical checks and requires the typed refusal at each. Neither failed stream
is relabeled as a pass; both are retained under
`/mnt/data/sn-testnet/bootstrap-finality-bracket-astra-20261001/evidence/`.
The author's earlier passing race scope contained only sixty roots.

Independent Sol qualification on exact `98df8b5f` passes all 21 new roots in
normal/race modes, seven adjacent normal roots, four adjacent race roots and
vet. Its review also validates all eight dependent-owner omission assertions.
The primary receipt is
`/mnt/data/sn-testnet/sol-bootstrap-finality-independent-20261001/primary-receipt.json`,
SHA-256 `0e019f76c705b1846cb88a1b0b99f1d6b074aa8d33654c6ae50128204fadd02a`.
All eighteen primary manifest entries pass readback. The three separately
started heavy adjacent race roots later reached terminal package PASS at
584.223 seconds; their raw stream SHA-256 is
`c34b69fd01ece192f10415b5497a9e8a08d5f4d1f6644b23bed72cd2fed7910e`.
That later stream remains separate from the original primary receipt.

The EVM successor passes all 52 selected author normal roots in 575.428 seconds.
Its broad race command reached the 30-minute package limit after 45 completed
passing roots, with no earlier assertion failure or skip. The exact seven-root
unfinished complement reached terminal PASS in 1038.728 seconds on the same
frozen source. The receipt preserves the failed initial package invocation and
its completed bodies separately from the passing continuation:
`/mnt/data/sn-testnet/bootstrap-send-finality-20261001/receipt.json`, SHA-256
`299050c126a4605dbf217f1a69f79e51d7becebcef8bbd44df7ccc04b9553409`.
All forty manifest entries pass readback; the manifest SHA-256 is
`2d901890eee322949804e2a785111ad579fca7c408dcc80009532f539a1a359b`.
The old-owner overlay reproduces all four late faults and the recovery assertion
in both modes; the positive advancement control passes. The controls retain the
final tests and replace only the nine production lines with their parent source.
The initial baseline failures remain available.
Both source scopes pass vet, retain unchanged source/module/dependency identities
and contain no unclassified assertion failure, skip or data-race diagnostic.

Independent Sol qualification on exact `41f053ab` passes eight representative
roots in both modes with no skips, including all three new roots and the complete
eight-action public-header path; vet also passes. Its omission control reproduces
the four late synthetic broadcasts and recovery failure while further healthy
advancement remains accepted. The report is
`/mnt/data/sn-testnet/sol-bootstrap-send-finality-20261001/report.txt`, SHA-256
`d39d55ff4c65bf865e8245a0e2bb4bcde27791dd6a266fcb3017c0eae4cd2d72`.
All eleven manifest entries pass readback. These eight roots are independent
scoped evidence, not a substitute label for the broader author race run.

All three affected roots pass normal/race on the final naming-only fixture
successor `7ceacf6b`. Its four-entry evidence manifest is
`/mnt/data/sn-testnet/bootstrap-finality-fixture-style-20261001/SHA256SUMS`,
SHA-256 `5d7e04f27639838b91608b8dd597806bd0bff717beeafffc69f85f2ebaa5d762`.

## Remaining launch gates

These checks preserve the owned-RPC assertion boundary. They do not supply native
consensus or storage proofs, independently approved current chain/runtime
authority, external signer/host custody, live restart/upgrade acceptance, applied
10/90 economics or service activation.

The earlier release at SN `bab49e1c` / server `a3e2e668` predates both production
corrections and the server `64cde171` source. The
[composed baseline](release-28ebfced-serverac86-20261001.md) now binds
SN `28ebfced` / server `ac86855d` with matching local source/image repeats.
Independent release qualification and deployment approval remain required.
No live signing, submission, deployment or service start occurred.
