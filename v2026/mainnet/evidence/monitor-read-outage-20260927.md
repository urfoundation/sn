# Mainnet monitor read-outage continuity

The signer-free mainnet monitor now retains the first failed RPC-read time
after a healthy finalized baseline. It keeps emitting `rpc-error` while
retrying, adds `severity: warning` after two minutes and `severity: critical`
after five, and escalates immediately if the host clock moves backward.
A complete identity and retained-finality read clears the outage. The private
single-owner checkpoint persists the start across process restart; a failed
checkpoint write exits with `checkpoint-error` rather than reporting health.

The checkpoint writer uses schema v2. It reads authenticated v1 finality
records and writes v2 on the next state change. Rollback to the old binary
needs a reviewed compatibility procedure because the old reader will reject
the new schema. Before the first healthy finalized sample, there is no
checkpointed baseline; an independent dead-man alert remains required.

Deterministic tests force a healthy sample, route failure, warning, critical
and restart using a scripted clock and local RPC, plus clock rollback and v1
checkpoint migration. The outage clock includes time spent retrying an identity
read or the later retained-finality read. The affected monitor/RPC/identity
selector passed normally (`0.750s`) and with race detection (`1.984s`); `go vet ./mainnet`
and `git diff --check` passed. The output is status evidence only: no alert
route or repair controller was installed, and no mainnet endpoint was used.
