# Integrated mainnet source qualification

Sol medium qualified the current integrated SN source
`4590fa8525f546cfd088a6ee4f5bfeb7a1ddf67a` on a frozen physical graph:
Connect `b163f9dd`, SDK `516521fb`, server `5dc11761`, and the other exact
replacements in the [sealed handoff](/mnt/data/sn-testnet/evidence/mainnet-final-composed-20260928/HANDOFF.md).
This composes the qualified registration/diagnostic, native deadline observer,
and offline bootstrap-chain changes. It is source qualification, not a built or
deployed mainnet release.

The maintained runner passed all **12 of 12 joined stages**: two normal/race
builds, a full normal `mainnet` package body, and nine bounded race bodies.
The full normal body passed **379 of 379 roots and 79 of 79 declared
descendants**. The race shards passed the exact selected 35 monitor and 87
bootstrap roots, plus 79 descendants. Overall, **501 of 501 root executions**
and **158 of 158 descendant executions** passed, with zero skips, failures or
race reports. The source and binary fences in the runner passed; Sol's
independent before/after admission found the same ten physical sources, four
module graphs, one mainnet package identity and sealed input bytes. All Sol
driver and phase exits were zero, with no worker left running.

Astra max independently compiled `./mainnet` normally and with race detection
and ran package vet. Its author after-fence and sealed-input comparison passed;
it ran no behavioral test bodies. The first evidence-only metadata helper build
omitted an external helper definition and failed. Its log is retained. The
corrected helper built and admitted the same source before Sol started; no
product code, source selection or test assertion changed.

The earlier seven registration/diagnostic consumer roots and their component
controls were reused because all clientauth, miner and validator Go files,
module declarations and exact physical dependency refs are unchanged from
their sealed composition. The native observer's five causal controls and two
alert fixture suites, and the bootstrap's four causal controls, retain their
separate passing component receipts and unchanged affected Go/rule bytes.
The bootstrap's original combined race attempt timed out under one cumulative
ten-minute budget; its successful six-shard replacement and failed attempt are
both retained in the [bootstrap receipt](bootstrap-chain-qualification-20260928.md).
The current composition uses the same complete race root selection in nine
bounded shards. This result does not erase that earlier timeout.

Raw plans, test binaries, event logs, exact membership, source maps, reuse
proofs and phase exits are under
`/mnt/data/sn-testnet/evidence/mainnet-final-composed-20260928`.

| Retained artifact | SHA-256 |
| --- | --- |
| `inputs.sha256` | `3acad5831d2081f895f28cc65ed6d1e9314c057c613613419e6ec14156d0de5e` |
| `sol-positive/report.json` | `144a48c3dcab69b0516ae1a3e6ae4b72ebfab04b5575bc26a8ea8dc7fadb642f` |
| `sol-after/status.json` | `284259687b5b0aac095834d8942469cc9d671c75b44404b596c04368d0fec5fa` |
| `AUTHOR-CHECKS.json` | `3568251fd6d8acc8323af503eda991f92dbca949feea9cf2be393a5b038da253` |

MG-02/MG-10 still require a release manifest, binaries/images and deployment
provenance, relevant production-path integration, and live acceptance evidence.
MG-01 remains blocked on an approved mainnet genesis and a working owned RPC.
MG-06/MG-08 still require actual custody, signing, contract installation,
validator service activation and observed 10/90 native economics. MG-07 still
requires deployed collection, delivered alerts and incident resolution. No
chain transaction, live RPC, signer or deployment was involved in this test.
