# Vault coordinator binding source qualification

The one-shot `STSettlementVault.setCoordinatorOnce(proxy)` executor in SN
commit `cdfe07449f86b041fc2aa27343e6d19e168e8ce5`, with the test-only
selector correction `504de248d810611b74015a90cb20ded725fd0f7c`, passed
offline source qualification. The integrated root branch preserves candidate
source bytes. The action remains under the same approved graph, six
predecessor custody/continuity checks, original signed bytes and bounded
attempt/funding limits.

The initial 29-root normal run found one deterministic test-server panic from
assuming a pending block selector was a map. The other 28 roots had no
assertion. The corrected checkpoint root passed normally in 265.958 seconds
and under race detection in 1493.718 seconds. All other **28** vault-link roots
passed under race in exact disjoint shards. All six adjacent predecessor
checkpoint roots passed normally and under race. Vet, clean source/module
fences, and an old-selector causal control in normal and race also passed.

The first checkpoint race attempt hit a 20-minute Go package timer without an
assertion; the exact root passed under a 35-minute bound. One four-root race
shard also hit a 20-minute package timer without assertion; only its 2+2 roots
were rerun and passed. Both timeout logs remain failed test attempts, not
product defects or claimed passes. A full package normal run was not repeated
for this test-only successor; the qualified reserve predecessor's 523-root
normal package passed separately.

The [raw Sol receipt](/mnt/data/sn-testnet/qualification/sol-vault-selector-fix-20260928/RESULT.md)
has SHA-256
`19a79d1dfdb39f5102c2226490b8e8d75bea19c8bde69d010e073101f74baf58`.
No live mainnet authority, custody, finality, signing or deployed state was
established by these local tests.
