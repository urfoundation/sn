# Incremental capture repair qualification

This portable bundle records the three bounded replacements for the cumulative
capture-fixture timeout. It contains terminal receipts, exact selected member
lists, qualified replay results, source/binary identities, and cleanup facts.
It does not replace or relabel the original failed c5 capture race.

| Scope | Result | Exact coverage |
| --- | --- | --- |
| Corrected normal | PASS | 9 roots and 29 children; 38 expected outcomes |
| Complete corrected race | PASS | 378 roots and 140 children; 518 expected outcomes; 479.526 seconds |
| Old-fixture causal control | PASS as a control | 1 root with its required failure; body exit 1 and replay exit 0 |

The candidate is `93f6d352d979c3dfb5fe068d4c07f400a227bc56`. Its only
changed product input is `sim-testnet/campaign_succession_test.go`, whose
SHA-256 is `f96c234571958f05259df3b279d72d8f561355ee223881644c6d6df4126e19e8`.
The correction is test-only: production code, protocol, dependencies, release
lock, and native preparation inputs are unchanged. `SOURCE-IDENTITY.tsv` and
`SOURCE-MANIFESTS.tsv` carry the public identity and manifest hashes without
copying source trees.

`CAUSAL-OUTCOME-PROVENANCE.tsv` records the exact sealed causal outcome input
used by the completed causal body and replay. Its public copy in
`records/causal/body/expected.outcomes.tsv` has the same SHA-256 and contains
the required one-root FAIL expectation.

The race's raw compiled list had 490 roots. The sealed skip expression was
applied before the membership comparison, leaving the exact 378-root capture
selection in `records/race/body/list.roots.sorted`; its 518 expected outcomes
and the qualified replay result are retained beside it. The normal and causal
records use the same receipt layout.

All three bodies were dispatched together with `GOMAXPROCS=4` per owner. The
race body overlapped the actual normal body from 17:17:08 to 17:17:20 UTC.
The dispatch host had 24 logical processors but a lighter measured one-minute
load of 1.19. This records the actual conditions only; it makes no claim to
have recreated the older 24-CPU stress condition. `LOAD-AND-CLEANUP.tsv`
records the timestamps, observed load, and terminal process cleanup. The
underlying dispatch-load and terminal empty-process-census receipts are
`records/race/body/host.load.dispatch.txt` and
`records/race/body/host.load.after-terminal.txt`.

The original c5 race timeout remains a FAIL at 600.377 seconds. The successor
race above is a separate incremental replacement receipt. This bundle makes no
chain, native-plan, renewal, or live-acceptance claim and excludes native plans,
keys, signed bytes, runtime directories, caches, binaries, and source copies.

Verify this public subset with `sha256sum --check --strict SHA256SUMS` from
this directory.
