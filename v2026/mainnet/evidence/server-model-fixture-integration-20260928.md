# Server model fixture integration

The composed server branch `codex/mainnet-composed-hardening-20260927` advanced
from `b6f49bdbe6a61ef0cca3392b3a3ee47fe4806ec2` to
`936c3d9563372e8f424d516ee2dd3525555206de` by fast-forward. The three commits
change only tests and qualification receipts. Production custody guards,
retention behavior and probe scheduling are unchanged.

| Commit | Qualified change | Evidence |
| --- | --- | --- |
| `4468a6961c00cf0ff8b84986259fa9698a7a8441` | Historical payment inputs carry original terminal time/usage; retention fixtures use real bilateral settlement; excluded providers follow the current health refresh schedule. Eighteen affected roots pass normal/race. | [Server receipt](https://github.com/urnetwork/server/blob/4468a6961c00cf0ff8b84986259fa9698a7a8441/local/model-fixture-qualification-20260927.md) |
| `d62f6fcc4ba8d02d169b9476b6e49f870d4c81d0` | Historical location update time precedes its later health sample. A genuine later client verdict still requests a new probe. Fifteen roots pass normal/race; original-fixture and disabled-verdict controls fail as intended. | [Server receipt](https://github.com/urnetwork/server/blob/d62f6fcc4ba8d02d169b9476b6e49f870d4c81d0/local/egress-fixture-chronology-20260928.md) |
| `936c3d9563372e8f424d516ee2dd3525555206de` | Two additional settled-row fixtures supply original time, complete usage and direction in the first insert. Duplicate billing rows divide the recorded bytes without changing drain assertions. Two changed roots and six guards pass normal/race; both original fixtures fail at the expected custody guard. | [Server receipt](https://github.com/urnetwork/server/blob/936c3d9563372e8f424d516ee2dd3525555206de/local/retention-fixture-custody-20260928.md) |

The final retention selection took 10.505s normal and 15.722s with race;
the six guards took 32.718s and 43.826s. No selected positive root skipped,
and each joined test body exited zero. Vet, actual module-resolution checks,
source fences, maintained causal replay and disposable-service cleanup passed.
Raw evidence remains under `/mnt/data/sn-testnet/evidence/` in the
`server-model-fixtures-20260927`, `server-egress-fixture-chronology-20260928/physical-cache`
and `server-retention-fixture-custody-20260928/sorted-roots` directories.

This addresses all eight asserted failures from the
[original diagnostic collection](server-model-diagnostic-20260928.md).
The independent full model body finished on its frozen server `4468a696`
graph: [1,108 passed, three known fixture failures, seven skipped and no missing
roots](server-model-completion-20260928.md). Later fixture repairs are qualified
separately; the frozen package result remains fail. Optional configuration
scopes and the original diagnostic deadline failure remain disclosed. No
release, deployment or live mainnet acceptance is established by this integration.
