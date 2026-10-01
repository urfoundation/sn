# Root monitor fixture permission correction

The first full mainnet qualification of offline root-role admission candidate
`812a8bbc395be3ca11c20c77c53c7c14cab11034` failed in five existing root-monitor
output tests. Four could not read their expected `root.prom`; the custody and
cleanup test observed only one closed file owner. Its focused bootstrap-chain
selection passed. Sol retains the original full-suite log at
`/mnt/data/sn-testnet/evidence/mg08-root-role-admission-20260928/full-normal.jsonl`.

The five fixtures used `t.TempDir()` directly as a metrics parent. The tested Go
implementation creates its numbered temporary child directory with mode `0777`,
subject to the process umask. The qualification shell's umask was `0002`, so
these directories were group-writable `0775`. `openMonitorMetrics` correctly
refuses such directories before claiming publication. This accounts for both
the absent metrics files and the single checkpoint cleanup: the metrics owner
was never admitted. The tests discarded or deliberately blocked diagnostics,
so their later publication assertions hid the earlier admission failure.

The correction uses the existing `monitorMetricsTestDir` helper at all five
positive root-monitor metrics fixtures. That helper explicitly provisions mode
`0750`. The generic chain-monitor fixtures already use that helper or explicit
protected directories. Root-monitor's intentional unsafe-directory refusal
fixture explicitly sets `0770` and remains unchanged, as does its special-file
refusal fixture. No production admission rule, assertion, timing, retry budget
or command behavior changes.

The affected regressions are the read-outage, publication-ambiguity,
read-cancellation, custody/cleanup and physically blocked-output cases in
`root_monitor_output_test.go` and `root_output_linux_test.go`. Their existing
barriers still check real publication and joined ownership. Qualification must
retain the original failure and separately exercise baseline and corrected
source under explicit `0002` and `0077` masks; changing only the runner's mask
would leave the fixture defect latent. Sol owns these executions and the final
qualification receipt. The implementation agent ran formatting and diff checks
only, with no behavioral tests or live actions.
