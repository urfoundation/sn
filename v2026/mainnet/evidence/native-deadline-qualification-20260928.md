# Native submission deadline observer qualification

The [native submission observer](../NATIVE-DEADLINES.md) is integrated and
component qualified. The actual per-role monitor forecasts submission risk
from native schedule evidence and retains reported missed windows across
restart, renewal and late application reports. It neither expires signatures
nor treats the unsigned progress file as independent chain acceptance.

Sol medium qualified sealed SN
`7ef5e98086632c38cde134e2bf3baec00c62802c`. Author commits `b34ed0fa`,
`392b34ac` and `7ef5e980` were integrated as `12ee97f6`, `0884c976` and
`be0aa2e1`. All mainnet Go and rule files match the sealed source byte-for-byte.
Integration checked that equivalence and `git diff --check`; no behavioral
tests were repeated on the root workspace.

| Check | Result |
| --- | --- |
| Positive selector `^Test(MonitorNativeDeadline\|MonitorServices\|MonitorOutput)` | Exactly 35/35 roots passed normally and 35/35 with race detection; compiled membership matched the author census. |
| Five causal controls | All ten normal/race executions failed at their required assertion, with no race report or setup/timeout substitute. |
| Offline alert fixtures | Three native-deadline and six inherited service scenarios passed in the pinned Prometheus image. |
| Source fences | Six SN worktrees and nine physical sibling worktrees retained identical clean heads, tracked-file hashes and resolved module inputs before/after execution. |
| Author checks | Final source compiled; package vet, gofmt and whitespace checks passed. |

The controls independently remove incident retention, erase history at restart,
accept a stale native observation, shift the strict safety-counter boundary,
and let an unsigned applied report clear history. The positive tests also cover
failed/unavailable receipt evidence, exact block thresholds and deferral,
per-role isolation, ambiguous checkpoint sync, compatible legacy migration,
and checksum-valid incident chronology corruption. Later healthy observations
and disabling future inference cannot clear retained misses.

Execution used Go 1.26.6, linux/amd64, `CGO_ENABLED=1`, `GOWORK=off`,
`GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly` and `GOMAXPROCS=2`. The test
processes used 120-second package and 140-second outer limits and all joined.
Prometheus image
`sha256:63805ebb8d2b3920190daf1cb14a60871b16fd38bed42b857a3182bc621f4996`
ran without network, with read-only rules and bounded temporary storage. No
live chain, signer, deployment or external service was used.

The physical dependency graph is the frozen `server-model-final-20260927`
workspace: Connect `c68689c4`, server `4468a696`, proxy `6204ae7d`, SDK
`42241118`, userwireguard `85fb1ca4`, glog `892ade4a`, goidenticons `325750b3`,
Warp `7498864c` and the available operator-proxy sibling `44836974`. The npipe
replacement belongs to the sealed SN tree. This component result does not
qualify a different composed dependency graph.

The retained [executor result](/mnt/data/sn-testnet/evidence/mainnet-native-deadline-20260928/RESULT.md)
and adjacent `run-sol.sh`, `runs/`, `rules/`, `binaries/`, `before/`, `after/`
and fence tables contain exact commands, binaries, source manifests and exits.
The [author handoff](/mnt/data/sn-testnet/worktrees/sn-mainnet-native-deadline-20260928/HANDOFF.md)
contains the five control commits, selectors and required assertions. SHA-256:

| Artifact | Hash |
| --- | --- |
| Executor result | `6066ac2c3e1ac937ebee6f0e9e9663669e6b1cb577a3c35125396c42f235596f` |
| Positive tracked-file manifest | `3c9794258579e34c20d125357821f0bee613185355511e763e3139388d5bdc6d` |
| Positive module graph | `963c4febc0787a2f7b158ee79cb913cd7deb857c5dc540cf6c337fee44147b47` |
| Exact positive census | `5d759e8d0774275a1147f930845b5c3e23f36c51d6a35d4b7eac7a20a6a8f219` |

MG-07 remains open for measured approved margins, compatible consumer rollout,
collector ingestion, delivered alerts, independent supervision and an approved
incident-resolution/repair mechanism. This observer has no incident-clear
endpoint. MG-08 still requires approved mainnet identity and custody, complete
bootstrap/validator activation and actual native economic outcomes. Local
qualification supplies none of those authorizations or live success claims.
