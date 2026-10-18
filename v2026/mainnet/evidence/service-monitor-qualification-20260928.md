# Validator service monitor qualification

Astra authored consumer `a511e00aeffc85387b120b22955bda13a0568c47` and the
HTTP cancellation fixture correction
`43280615be701f3c6cfdf3a581209de7e98e7b0a`. Terra medium qualified the corrected
source. Root integrated them as `8a5027c6` and `c8b7be52`; the changed Go files
and alert rules match the qualified source byte-for-byte.

The real `monitor --services` command now observes independently configured
validator roles alongside its chain worker. It preserves last accepted
evidence, original pending ages and acknowledged publication across outages
and restart. The monitor opens neither signer custody nor protocol intent
owners. [SERVICE-MONITOR.md](../SERVICE-MONITOR.md) defines current versus
retained evidence, per-role files, bounded fields and operational limitations.

| Check | Result |
| --- | --- |
| Selected normal tests | Exactly 48/48 roots passed; body 1.534s |
| Selected race tests | Exactly 48/48 roots passed; body 3.292s |
| Five decision controls, both modes | All ten executions failed at the required assertions; bodies 0.300s/0.389s |
| Normal/race builds | Passed, positive 4.222s/25.547s; controlled 17.138s/24.308s |
| Prometheus rules | Six service fixtures and the inherited chain-rule suite passed |
| Source fences | Both maintained reports passed all four stages and retained unchanged physical inputs |

Controls expose a new outage incorrectly inheriting healthy process age,
candidate-selected source identity, ambiguous export refreshing confirmed
success, unchanged pending intent refreshing its progress age, and incorrect
role routing. Setup failures, arbitrary nonzero exits and package timeouts do
not satisfy those assertions. The control worktree contains the declared
mutations; it is separately hashed rather than described as clean source.

The maintained owner checked exact expected membership before bodies, including
`TestInspectRejectsTestnetRoute`. It retains requests, executable hashes, JSON
events, outcome tables, joined terminal results, resolved local modules and
before/after source manifests under
`/mnt/data/sn-testnet/evidence/mainnet-service-monitor-http-fixture-20260928/`.
The positive tree is clean at `43280615`. Physical sibling inputs remain the
frozen server-model graph: server `4468a696`, Connect `c68689c4`, proxy
`6204ae7d`, SDK `42241118`, glog `892ade4a`, goidenticons `325750b3` and
userwireguard `85fb1ca4`. The runner/helper source is separately recorded at
SN `615a7675`. These component results are not a final composed-release lock.

Rule logs are `rules-terra/service-alerts.test.yml.retry.log` and
`rules-terra/monitor-alerts.test.yml.retry.log`. Terra used `/bin/promtool` from
`prom/prometheus@sha256:63805ebb8d2b3920190daf1cb14a60871b16fd38bed42b857a3182bc621f4996`,
with network disabled, a read-only rule mount and bounded temporary storage.
The earlier default-entrypoint invocations are retained as invalid harness
attempts; they did not execute the rule suites.

The original `a511e00a` normal invocation remains a **package timeout at
600.116s**, not a pass. Its headerless table selector omitted one expected
root, and a synthetic HTTP handler blocked cleanup after the monitor workers
had exited. The fixture correction consumes the bounded POST before waiting
for actual request cancellation and also repairs the adjacent legacy restart
fixture. Production monitor code did not change for this correction. The first
corrected-plan admission also failed before bodies because lowercase outcome
tokens did not match the runner's uppercase format; the exact census remained
48 positive roots and five controls after normalization.

SHA-256 receipts:

- Positive report: `05046e706befeb6a5ecc6d37113625410f4fcd3db1fb7a1ec9eba4470e7b7ba3`
- Control report: `e094c18011384f54b5f1b3da38a3f4e883fb7a26128cda0e2926b11f18ffc0df`
- Physical module seal: `9d76a35921eed40f866b10630f42d698f2a90eb4f9b65308476596f5dcee217b`
- Positive outcome table, each mode: `273a7565fee0998e17602be3f68e3b187535a693e9981b87ff0a56f5e1e89792`
- Control outcome table, each mode: `7a086a2ede60bd81aef3dfd0df38675c7182a216e48e6a239ed7e83e10b36d53`
- Each rule success log: `8289a4919870ab5b28f5e90a86b4af3cdbd8c244516c02fca90d6e3f289a6711`
- Terra summary: `6fda88e40b621f16fd522ed9bc125abc54fb5dc5bfb08c3812696cbd704aee3b`

MG-07 remains open. Collector ingestion, delivered alerts, independent expected
rosters, supervision, other production domains and the authorized repair
controller are not deployed or qualified by these local fixtures. Protocol
deadlines remain explicitly unknown. A blocked synchronous log sink can still
stall workers and their join; bounded log export is separate pending work.

The composed branch check at clean `424af08ce301ac0597feb74e6374e32a6adabbab`
also passed `./mainnet` compilation and exactly two real-command roots:
`TestMonitorServicesCommandSeparatesBlockedChainAndRoleOutage` and
`TestMonitorServicesCommandRenewalRetainsOriginalIntentThroughOutage`.
Normal/race bodies took 0.144s/1.212s. This checks the newer native/continuation
dependencies while reusing the unchanged 48-root consumer results. Source stayed
clean and resolved modules were byte-identical before/after. Raw output is
`/mnt/data/sn-testnet/qualification/service-monitor-composed-424af08c/`:
normal SHA-256 `e9a651008372b17ccc78d68c285f1443c1c665671b1a6f581276b3d24daed1b7`,
race `94cb186aa91658ac67a4d1899fa162648c328f77ba49e04cfa4ecb1c74ce7886`,
modules `e6502b79a35bbc61f3a9dd3cb7649865ca6bfc39c63573e1e73bf0331e302917`.
