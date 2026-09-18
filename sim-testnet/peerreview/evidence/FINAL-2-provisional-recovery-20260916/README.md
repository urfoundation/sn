# Provisional recovery evidence — September 16, 2026

The strict startup ended at 15:41:52 UTC without fresh validator trails. During
that generation, the two operators submitted eight transactions with successful
canonical receipts, at blocks 8,019,104–8,019,127. The observed fees total
16,528,555,545,105,692 wei (0.016528555545105692 EVM TAO). This is real testnet
activity during an unsuccessful startup, not a completed acceptance campaign.

`receipt-safe.tsv` maps each operator, nonce, hash, block and fee.
`receipts.json` retains the original RPC batch: eight successful receipts and
one failed runtime query that incorrectly used an EVM hash as a native hash.
The separate `runtime-head-hash.json` and `runtime-version.json` retain the
subsequent native-hash lookup and successful runtime 461/1/1 observation.
All queries used `192.168.1.162:9944`; `independent_rpc=false`.

Reproduce the eight receipts against the same LAN node:

```sh
curl --fail --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary @reproduce-receipts.json http://192.168.1.162:9944
```

`signature-restoration.json` records restoration of the eight original signed
payloads into local recovery storage. It records no new signatures, broadcasts,
journal edits or database edits. Signed payload bytes are not included here.

The qualification summaries and patches describe the fixes for provisional
LAN continuation/startup, runtime-specific configuration rendering and bounded
receipt authentication. Results are accepted by composition with the original
fixture failures retained. These are local test assertions, distinct from the
on-chain receipts. Their raw test outputs remain under
`/mnt/data/sn-testnet/qualification/` in the correspondingly named
`provisional-continuation-20260916-r1`, `runtime461-render-refresh-20260916-r1`
and `receipt-cache-incremental-20260916-r1` directories. The cache result records
14/14 normal roots by composition, 14/14 race roots and all three intended
causal failures. `provisional-cache-driver.json` identifies the separately
built VCS-stamped candidate; that candidate was not used for the live resume or
release-candidate command.

The provisional resume started at 16:21:51 UTC using driver SHA-256
`5ff1130c0f1b7ef3404c2d7a8c37f388854bed8299b3ae4bb152ac3ebf220348`
and retained plan
`0xcba026fa05f500c6ee120a19c94b1f3f0d1362bf4e6cab0db21aa4306d8b4d0f`.
It authenticated 4,674 local receipts at approximately 16:31 UTC, completed at
16:52:13 UTC and handed the adopted live topology to the release-candidate
scenario at 16:53:00 UTC. At that adoption boundary validator-1 had one observed
restart caused by a 403 staging-discovery range-budget refusal; the replacement
process was present, but process presence does not establish successful terminal
publication. These milestones do not establish campaign completion or final
acceptance.

The first release-candidate command closed at 17:01:52 UTC after authenticating
the same 4,674 receipts. It reached no measured phase and refused to open the
durable attempt because provisional continuation was excluded from campaign
succession. By closure both validators had encountered the independent staging
configuration defect, with three total restarts. The sealed command result,
exact terminal error, restart census and bounded configuration/log observation
are under [`release-candidate-failure/`](release-candidate-failure/README.md).

The combined succession, retained-context rendering and local-intent correction
was subsequently qualified as driver SHA-256
`b85ab8529118c9bc9f2848865882dfa1291651a1c08de8ad28bffcb72857707d`.
Its composition receipt is SHA-256
`e68198669ad9f73703f35163cb50de3330fc5d8a018ca4ca3631433a0e8e403d`:
19/19 selected roots passed normally and under race detection, and seven causal
controls produced exactly their intended failures. This is local qualification,
not campaign or on-chain acceptance.

The first corrected live resume ran from 18:34:01 through 18:51:08 UTC. The
product command authenticated all 4,674 retained receipts, generated both exact
corrected operator configurations, started all 33 processes and exited zero.
The outer readiness check sampled one transiently unhealthy miner at its first
snapshot and rolled the generation back successfully. That failure established
that a single instantaneous all-process health sample was too strict for the
miner probes; it did not reproduce the earlier authority or discovery-window
refusals.

The second corrected live resume ran from 19:07:14 through 19:25:32 UTC under
request SHA-256
`2401b3ca5898bc909f10d7127a813e97c362a2c5778174ccb0df8fa6618ae3dd`.
The product command again authenticated all 4,674 receipts, created supervisor
PID 494735/start ticks 187281576 with 33 processes and zero restarts, and exited
zero. Across 24 five-second readiness samples, both validators and every
non-miner service were healthy in all 24; every miner was healthy in at least 22,
and nine samples had all 33 processes healthy. Both operators emitted the exact
provisional retained-context startup authority marker. The refusal scan was
empty. Neither operator emitted the separate `local-readiness` marker because
that marker is produced only when a caller invokes the internal `WaitReady`
path, so the outer gate timed out before entering its stability phase. Its
result SHA-256 is
`24f8cb4b813522b38ead14ae7b7309f9d873afb75b7c67acfd2cb5139fa2ee1a`;
resume and rollback both exited zero, while operational readiness and the outer
owner exited one. Contexts, executable and release-lock hashes remained
byte-identical. The exact capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r5/topology-preserving-corrected-provisional-resume-r2`.
It remains a failed provisional attempt with `final_acceptance=false`.

The third corrected resume ran from 19:40:04 through 19:57:09 UTC. Its product
and outer owner both exited zero. All 4,674 retained receipts authenticated; the
16 readiness samples covered 82 seconds, included seven 33/33 samples, observed
every process healthy and kept validators and other critical services healthy
throughout. All identities, PIDs and OS start ticks stayed stable, and there
were zero restarts and no refusal. Its request and result SHA-256 values are
`0c3b723efdb34dcfb609b8be2352867886fd10069e10352a87ec3fce9458fb9f` and
`4f2740bd3b9cb542636c75f42608a232c7c90643176d6bbe85497a2cedebeae6`.

The bound release-candidate owner then ran from 19:58:56 through 20:00:05 UTC.
It authenticated the same 4,674 receipts with zero failures and created signed
successor run `20260916T195955.196642218Z-release-1.0`, whose attempt envelope
has SHA-256
`9aa94b86effb8aafc37bee904ff1e909a14edc6630a67f0a5588728ad7900417`.
The source-capacity gate then refused before preparation, transaction or spend:
the LAN runtime's 15-second validator poll raises the worst-case protected retry
forecast to 6,012,260 requests/hour, above the configured 4,194,304 ceiling.
The object and byte dimensions still fit. The live topology remained in place,
and later invocation must reopen the signed successor rather than create a new
attempt. The raw owner capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r5/provisional-release-candidate-retry-r3`;
its result SHA-256 is
`a8175704a505198827ac47367f9ad4463d8e57233cd2eb2e0874bf44673c87af`.

The capacity correction passed 21/21 normal and 21/21 race roots, plus the exact
old-code causal refusal. The incremental r4 wrapper passed 81/81 cases and bound
request SHA-256
`ac01e73b1833f2232ac8a0da2474b543749dda4556bb85f77d474a51b7064204`.
Its real owner ran from 20:43:00 through 20:45:18 UTC, reopened the same signed
successor, and authenticated all 4,674 receipts. It stopped before preparation,
transaction, spend or scenario actions because the chain changed from runtime
461/1/1 at block 8,020,753 to 463/1/1 at block 8,020,754. The closed r4 result
SHA-256 is
`e688eaffe3153b45d0a3b55cd06f0f25d745baa34d8da97d10a5a94c2c35a002`;
its raw capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r5/provisional-release-candidate-retry-r4`.

The LAN-only artifact observation is
`/mnt/data/sn-testnet/qualification/runtime463-live-observation-20260916-r1/OBSERVATION.json`,
SHA-256
`62808a33e87248172336f2365e44e1e4d831aeca4572391c873e7ed78242f401`.
It records runtime-463 code and metadata BLAKE2b-256 hashes
`0x9745e3f66053c3c7cb30ea45b88c66438b5076da78154f477e8660b0ded43869`
and `0xe9af0fcab804e08c0f6cc2c13715b1e366a916eda6a61aec6fb2601bc2a66b4c`,
and independently reproduces both reviewed runtime-461 hashes. The wrapper's
watched plan, journal, configuration, roles, predecessor evidence and successor
remain unchanged. Both validators later exhausted five restart attempts against
the old 461 pin; the other 31 process restart counts remain zero. Recovery must
retain the validator ledgers/proofs/statistics and the campaign successor while
using the supported retained-plan stop/resume path.
