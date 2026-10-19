# Closed testnet service shutdown

On September 28, the closed testnet deployment still had its supervisor and
31 local workers running, including two claim relayers, 20 miner swarms, six
operator services and three RPC proxies. Both validator records already had
PID zero. These were deployment workers, not an active acceptance interval or
mainnet qualification processes.

The recorded service, executable, state directory, supervisor PID `2346275`
and start ticks `34893194` agreed with the live systemd owner. The unit was
static, with `Restart=no`. The original binary's `stop` command exited 1
before dispatch because current adversary timeout settings failed its launch
configuration validation. No stop was performed by that invocation.

Stopping the verified `urnetwork-sim-ur-subnet-testnet-v1.service` through
systemd then completed between **02:29:15 and 02:29:56 UTC**. The command
returned zero; systemd reported `Result=success`, `ExecMainStatus=0`,
`MainPID=0`, and `inactive/dead`. None of the recorded nonzero PIDs remained.
The retained supervisor state and logs were preserved. This operation stopped
local workers; it did not retire the subnet, remove test databases/volumes,
change a testnet verdict, or launch another signed interval.

Raw observations and both command outcomes are retained at
`/mnt/data/sn-testnet/evidence/closed-testnet-stop-20260928T0229Z/`.

| Observation | SHA-256 |
| --- | --- |
| Supervisor state before shutdown | `e5fbf3303cd4bfa4c4bd15dafb7aaefabbbff3f0295195c44434f89be36aa2b5` |
| Supervisor state after shutdown | `c9fa1e898e3998e88eefb1e1d125d78ca59ec83654909929abec2afce3ffd9bf` |
| Final systemd result | `f57986184f868475237d80178aacf4d4254bf338bb21f5290646b9e567531225` |
| Original CLI refusal | `79c1d5a21371ffb49dcf4bed36aac73a4576a3865cd9ca885d5a8e022a2f7810` |

The production lesson is to give campaign closure an explicit disposition for
every owned service, and to dispatch shutdown from retained process ownership
without requiring valid new-launch settings. A closed report is not evidence
that workers stopped. The old simulator configuration gate was not patched;
production shutdown qualification must cover this failure shape separately.
