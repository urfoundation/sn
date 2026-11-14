# Bounded diagnostic output qualification

The integrated component has passing normal and race coverage for all 76
selected roots. Six regression controls reach their required failure assertions
in both modes. Vet and the three affected Prometheus rule suites pass. These
are local source results; no daemon rollout, Loki ingestion or alert delivery
is established.

Production source `74b827ab864650a8eb5c66b8e512d6719ed3149a` and the completed-write
fixture correction `ba6a7ec356738cd8afbe5e4183d501fcd4fbe70f` were qualified with
the frozen physical sibling repositories under
`/mnt/data/sn-testnet/worktrees/server-model-final-20260927`. The later one-line
fixture correction is `bc1aefcd64889c09e44db6e34184ea12bfeda1d8`.
Integration commits are `fd4c3334`, `fe750a59` and `10b046fd`; production bytes
match that qualified component. Receipt recovery has its separate evidence
and does not acquire qualification merely by being a dependency here.

| Package | Selected roots per mode | Retained result |
| --- | ---: | --- |
| `diagnostics` | 7 | All pass on `ba6a7ec3` |
| `mainnet` | 52 | All pass on `ba6a7ec3` |
| `protocol` | 6 | Five pass on `ba6a7ec3`; the corrected maximum-wire root passes on `bc1aefcd` |
| `validator` | 11 | All pass on `ba6a7ec3` |

The original normal and race invocations each retain **75 passes and one
failure**. Their maximum-wire fixture repeated a nine-byte word 32 times,
producing a 288-byte deployment ID where the existing limit is 256. The
correction uses exactly 256 synthetic bytes and changes no production validator,
8 KiB wire bound or consumer-first compatibility assertion. Only that failed
root was repeated; the original failed package results remain intact.

The six guard-disabled controls cover count bounds, immutable record copies,
partial-write refusal, typed-nil sink refusal, actual public-owner cleanup and
closed diagnostic read facts. Each compiled root fails at its intended
assertion normally and with race detection. The control branch is not integrated.

The maintained qualification owner retained actual module resolution, source
content, compiled roots, body/checker results and terminal membership. All three
completed captures report unchanged source. Their receipts are:

| Report under `/mnt/data/sn-testnet/evidence/` | SHA256 |
| --- | --- |
| `mainnet-bounded-output-20260928/terra-positive/report.json` | `fc41302bef6b9587199b4794e708cc434d66026259d6732a997e4af87bdb67c0` |
| `mainnet-bounded-output-20260928/terra-control/report.json` | `21ff39d7a6571725e3476f687dc0f60ecfa4c57e244f19cd8982e4c2ddb111cd` |
| `mainnet-output-wire-fixture-20260928/terra-positive/report.json` | `6e9778eb204ba57dc24ac57aef80e64921604a16da7a4f7962e9121dac698fff` |

`mainnet-output-wire-fixture-20260928/terra-vet.{log,exit}` records exit zero
for `go vet ./diagnostics ./protocol ./mainnet ./validator`. The original
diagnostic directory retains `terra-promtool-{check,test}.{log,exit}` with exit
zero, using the pinned local Prometheus image without network access. It checked
4 diagnostic, 8 service and 7 chain-monitor rules and passed their three fixture
files. The corrected capture's initial unsupported `matrix` invocation was a
runner-usage refusal before bodies; the retained `run` capture above completed.

Roll out the strict progress consumer before the validator's optional diagnostic
extension. Old producers remain readable; old strict consumers reject the new
field. Metrics distinguish queue admission, completed local writes, loss and
unavailable output from actual protocol progress. Unsupported regular-file
daemon redirection is unavailable, and an unconfigured independent metrics
consumer cannot prove log delivery. Root-service/root-monitor output has a
[separately qualified follow-up](root-output-qualification-20260928.md); it is
outside this 76-root component. The actual trail-worker stdout path remains
separate work, so this receipt does not claim complete validator log isolation.
