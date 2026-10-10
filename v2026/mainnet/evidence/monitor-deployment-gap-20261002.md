# Mainnet monitor deployment gap (2026-10-02)

This is a read-only source audit, not a deployment receipt. It compares SN
`393d599a59b29ddb93672734089bcd6dece8597a` with xops `origin/main`
`da09ab5ab313acd1ea37eba259011e19e9035c1a` after fetching that ref.

The `sn-mainnet monitor` command can write an atomic `.prom` textfile and a
separate durable checkpoint. Its alert rules require an independently supplied
`sn_mainnet_monitor_expected{env,host}` roster so a missing monitored host
does not disappear from both the signal and the alert expression. These are
source capabilities and fixtures, not evidence of delivery.

The checked xops Subtensor playbook installs Fluent Bit and copies the shared
`files/fluent-bit/fluent-bit.conf`. That config's node exporter input lists
`cpu,meminfo,diskstats,filesystem,uname,loadavg,netdev` without `textfile` or
`collector.textfile.path`. Its separate `subtensor.conf` scrapes only the native
and light-node Prometheus endpoints. An `origin/main` Ansible search found no
`sn-mainnet` unit, `sn_mainnet_monitor_expected` roster, or SN monitor metrics
configuration. Planetoid has a working textfile example, but it is a different
host configuration. Thus the SN monitor's generated `.prom` file is not
currently wired into the checked Subtensor deployment path.

Snow's checked xops vars still select `subtensor_chain: testfinney`, testnet
genesis and EVM 945. Installing a mainnet observer on Snow must be gated on a
separately reviewed mainnet host/chain configuration. The public mainnet RPC
read path is a temporary planning route; source audit alone does not prove the
final service host or its telemetry identity.

Before MG-07 can close, produce and review an exact release-pinned monitor
service and restricted account, a private checkpoint directory, a textfile
collector on the chosen host, bounded remote-write labels, an expected-host
roster emitted outside the monitored host, and alert rules with a separately
hosted delivery test. Qualify owner/permission and restart behavior offline,
then verify actual ingestion, stale/missing-host paging and recovery on the
approved production route. No service was installed or started by this audit.
