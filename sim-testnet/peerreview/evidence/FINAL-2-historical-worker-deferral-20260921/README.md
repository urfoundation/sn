# Historical worker deferral qualification — 2026-09-21

This patch stops a completed historical RPC retry budget from reopening a whole
strict census or a second complete action deadline. The strict controller returns
all ordered unresolved errors and keeps its action gate closed. Individual RPC
retries, authenticated completed proof groups, signed plan checks, transaction
reconciliation and spending limits remain unchanged.

Explicit provisional startup continues using authenticated local receipts and
records `historical-audit-deferred.json` beside its invocation provenance. Every
row binds the complete Action hash and exact original JournalEntry; the inventory
also binds the plan, journal boundary, provenance and both EVM routes. It grants
no execution authority and sets `final_acceptance=false`. Strict preparation
retains actual RPC errors; provisional inventory states only that archive replay
was deferred. Independent audit can retry unresolved history while the campaign
runs. Strict final acceptance continues refusing provisional results.

The live `cce4ce2f` executable predates the `8445f4e` whole-census continuation
loop. Its repeated neighboring-block reads therefore do not establish that loop
as the cause of the live delay. Source review identified the loop as an adjacent
amplification defect before deployment. This isolated patch does not interrupt a
live process, change deployment state, or replay a transaction.

## Qualification

Terra medium ran tests; Astra max implemented the fixes. The main selector passed
normal and race checks on the exact eight files in [main-source.sha256](main-source.sha256),
whose manifest digest is `91856a0de158d0ae092e31ecba3b066d7b11bb60c04e5761fc34e69c79035555`.
The only subsequent source change completed the existing setup test fixture's
immutable provenance file and hash; production checks were preserved. Those
eight files remained unchanged. The final nine-file manifest is
[final-source.sha256](final-source.sha256), digest
`f125535f921d6374448499bba8d2420cb2f0b86f77fac179a43c3db05d788743`.

| Scope | Result | Package time | Wall time | Evidence |
| --- | --- | ---: | ---: | --- |
| Main selector, normal | Pass | 14.074 s | 75.84 s | [log](main-normal.log) |
| Main selector, race | Pass | 28.571 s | 316.44 s | [log](main-race.log) |
| Corrected setup fixture, normal | Pass | 0.825 s | 62.09 s | [log](setup-fixture-normal.log) |
| All adjacent startup tests, race | Pass | 300.494 s | 457.59 s | [log](adjacent-race.log) |

An initial run caught the new publisher incorrectly requiring the output leaf to
exist. It now validates the parent, allows first atomic creation and rejects an
existing link, nonregular file or nonprivate file. Tests cover first publication,
warm reuse, changed provenance, cancellation and file permissions independent of
umask. The first adjacent normal run then exposed only the incomplete existing
setup fixture; its three affected roots passed normally after supplying the
same provenance bytes/hash that the real command creates. All adjacent roots
subsequently passed with race detection. Initial failures remain in the external
evidence directory rather than being overwritten or counted as passes.

## Causal controls

External Go overlays change one boundary at a time without modifying the tested
candidate. The retained controls must fail at the specified assertion, not at
compilation or test setup:

| Control | Observed regression |
| --- | --- |
| Restore `8445f4e` census loop | Second census hides the first exhaustion: `reads=2 applications=1 published=false` |
| Restore old action wrapper | Raw owned deadline invokes the complete action twice: `calls=2` |
| Disable completed proof-group reuse | Retry performs `135/7` contract reads/batches instead of `90/6`; fresh finality/canonical checks remain `2/2` |
| Restore initial missing-leaf publisher | First publication fails with `ENOENT` |
| Disable provisional branch on current-plan receipt fixture | Deferral inventory is absent; this control alone does not prove an archive request |

The main candidate test separately proves zero requests to an unavailable
synthetic archive, stable journal receipts, distinct action identities and strict
rejection of provisional final results. The corrected supplemental pair makes
both receipts eligible for strict audit only after fixture construction. The
candidate still passes with zero archive requests: [normal 0.118 s](controls/archive-candidate-v2.log)
and [race 1.492 s](controls/archive-candidate-v2-race.log). Disabling its provisional
branch instead enters the real RPC client and preserves the checkpoint plus both
action errors after four synthetic 503 attempts: [normal expected failure](controls/strict-archive-control-v2.log)
(package 7.136 s), [race expected failure](controls/strict-archive-control-v2-race.log)
(package 7.505 s). The final source manifest remains unchanged after these checks.

The first supplemental fixture set read-only mode before creating its receipts;
its two construction failures are preserved under [invalid-fixture-v1](controls/invalid-fixture-v1/)
and excluded from qualification. [Control notes](controls/README.md) distinguish
these failures from the causal assertions and preserve the narrower meaning of
the original current-plan inventory control.

## Reproduction

From the SN module, use the data-volume wrapper for release qualification:

```sh
./scripts/with-test-storage.sh go test ./sim-testnet -run "$(cat sim-testnet/peerreview/evidence/FINAL-2-historical-worker-deferral-20260921/main-selector.txt)" -count=1 -parallel=4 -timeout=10m
./scripts/with-test-storage.sh go test -race ./sim-testnet -run "$(cat sim-testnet/peerreview/evidence/FINAL-2-historical-worker-deferral-20260921/main-selector.txt)" -count=1 -parallel=4 -timeout=10m
./scripts/with-test-storage.sh go test ./sim-testnet -run '^TestProvisionalSetupRevision.*$' -count=1 -parallel=4 -timeout=10m
./scripts/with-test-storage.sh go test -race ./sim-testnet -run "$(cat sim-testnet/peerreview/evidence/FINAL-2-historical-worker-deferral-20260921/adjacent-selector.txt)" -count=1 -parallel=4 -timeout=10m
```

Raw qualification, before/after manifests, initial failures and overlay source
are retained at `/mnt/data/sn-testnet/qualification/historical-worker-deferral-20260921/`.
These local synthetic tests establish the recovery boundaries; they do not claim
a live campaign or on-chain final acceptance result.
