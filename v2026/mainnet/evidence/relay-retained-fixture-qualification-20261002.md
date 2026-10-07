# Retained relay fixture qualification — 2026-10-02

Frozen source `734a82dc338e3a00ef790c7574f5110051475fcd`, tree
`e1f7e290d029aa7c300b71ce6c9d03bad6ea0944`, changes only two simulation test
files. Production readers, signed plans, journal formats and runtime behavior
are unchanged. This repairs the separate SN vet failure recorded during the
focused server `025802a50e2dc56dc56c3cb749db7375aa9f72be` integration review.

The two provisional-continuation fixtures copied a runtime containing a mutex.
They now construct a fresh reader with the original executor, plan, chain and
retained source checkpoints. Synchronization and derived horizon state are new;
the original journal and signed authority remain shared intentionally.

The exact baseline SN `6cfc4773038fae8b1de07807eeeb4a97c32d07d3`, run with
vet disabled to reach fixture setup, fails both affected roots because its
synthetic native header lacks canonical receipt fields. The shared fixture now
provides all header coordinates and announces the Blake2b-256 hash of its exact
SCALE header. That repair exposed a second fixture mismatch: the post-forecast
candidate used an unrelated prepared activation. It now uses the activation
issued by the actual fixture producer. Original-plan immutability, strict
acceptance checks and the original slot limit remain asserted. No decoder was
relaxed to accept the old fixtures.

| Scope | Normal | Race | Vet |
| --- | --- | --- | --- |
| Author: affected and shared RPC roots, plus direct shared-fixture callers | 35 pass | 10 pass | `./sim-testnet` pass |
| Independent clean frozen-source checkout | 2 affected roots pass | 10 related roots pass | `./sim-testnet` pass |

Both fixed scopes have zero skips or failures. This is a synthetic, offline
fixture result, not whole-suite or live mainnet qualification. The unchanged
module selections include Connect `e1b5d77b5029` and SDK
`v0.0.0-20261001021058-5d37be3876e5`; server and local sibling pins match the
focused independent integration fence.

- Byte-identical committed [author receipt](relay-retained-fixture-author-20261002.json):
  `/mnt/data/sn-testnet/sim-relay-retained-fixture-20261002/evidence/receipt.json`,
  SHA256 `0d8d4155629e3389a93b72418a9760e54a5fed2dff0f40ec5dc9a68541ecdaff`.
  It binds the exact baseline header failures, intermediate activation-domain
  failure, fixed test logs and frozen source patch.
- Byte-identical committed [independent receipt](relay-retained-fixture-independent-20261002.json):
  `/mnt/data/sn-testnet/sol-server025-integration-independent-20261002/sn-fixture-receipt.json`,
  SHA256 `c54bfc26a96f96f171bcebf3eaa314728200a8c1cc6922c6590f31e1c66cdbaa`.
- Original focused server/SN receipt, including the original SN copylocks
  failure:
  `/mnt/data/sn-testnet/sol-server025-integration-independent-20261002/focused-causal-receipt.json`,
  SHA256 `484ada95842abdc5eed1b24a57bc0460d541c16c90d61884648ccb3055b9c31d`.

No signing, broadcast, deployment, live database access or service start was
performed for this fixture repair. Server full-model findings and MG-09 storage
adoption remain separate workstreams and are not qualified by these receipts.
