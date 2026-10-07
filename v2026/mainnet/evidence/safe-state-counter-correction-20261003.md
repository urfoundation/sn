# Safe account read counter correction

Frozen `296980a056b7a92596d51d17e8ad020e8329bc2c` produced nine passing roots and one assertion failure in Sol's ten-root normal run. The exact conflict diagnostic included `RPC evidence is inconsistent or malformed`, `successor canonical Safe runtime differs from reviewed release`, **two calls and zero waits**. The retained log is `/mnt/data/sn-testnet/sol-safe-state-296-independent-20261003/evidence/ten-normal.jsonl`.

The failed fixture selected every `eth_getCode` request. The actual consumer reads the proxy and singleton at distinct addresses before comparing both runtime hashes. Counting those two required reads as a retry was incorrect.

This test-only successor selects each exact account separately and exercises both conflicts. Each selected account must be read once, with zero retry waits, an integrity error, no unavailable classification and no send. Slot and getter conflict controls retain their exact selections. Production and module files remain identical to `296980a0`; original read budgets and positive-conflict behavior are unchanged. Independent qualification remains pending.
