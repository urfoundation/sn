R46 adversarial intent source review, 2026-09-26

R46 remains failed. The sealed v3 diagnostic reports `adversarial-campaign=fail`
on the original `affiliation-mask-evasion` vector. Its original adversary artifact
has SHA256 `3aa5af34fef8598d80c0cfaef0e91199c8c57803ce620faed0c275d6c9499dbc`.
The owner result has 59 failed `adversary_*` assertions. None of this evidence
was edited, regenerated, or treated as a passing campaign.

The retained consensus actor completed zero samples and skipped all 4,757
attempts with `independent validator has no applied intent yet`. The affiliation
vector's sample floor is zero; only `unresolved_affiliations` was measured.
`mask_coverage_ppm` and `independent_validator_coverage` are absent. The final
validator observations separately show that validator-1's selected generation
has no local intent store. Validator-2 has a recorded local applied intent at
native epoch 1661, settlement epoch 614, application block 8080373. That predates
R46's signed baseline block 8084658 and cannot supply acceptance-window freshness.
Both validators' strict finalized/applied counters remain zero.

At base `0391c1b004cddecc8d2d68bb80b2290cc474c5fe`, the consensus actor always
called the legacy `inspectValidatorIntent` reader, which selects
`runtime/validator-2/state/steering-intents.json`. It did this twice per sample.
The scenario probe already selected the approved provisional or strict V2
generation, so the two consumers could disagree about the same validator.
This is a deterministic source-selection defect affecting future generation
runs; it does not explain away validator-1's real absence or the stale
validator-2 receipt.

The actor now uses the scenario probe's shared source selector. Its factory
receives the original approved configuration separately from the admitted RPC
transport derivative. A sample uses one observation for its vector and metrics;
an invalid or absent selected V2 source has no legacy fallback. Provisional
observations retain their zero strict receipt counters. Campaign pass rules,
sample requirements, real mask/cohort checks, native receipt authentication,
and acceptance-window requirements are unchanged.

The behavioral pre-fix test first proves that the existing source-aware probe
accepts a synthetic signed V2 applied local intent under the real test-owned
validator generation, with no legacy intent. It then exercises the production
actor factory. On the untouched base, the actor returns
`Outcome:skipped Detail:independent validator has no applied intent yet` with no
metrics; the test exits 1. The retained pre-fix test is pinned separately from
the final expanded test. The final fixture also removes the entire legacy
validator-2 state directory. Control and attack samples pass without recreating
it or changing source bytes. Missing intent, forged intent/measurement/envelope,
forged generation, and approval/transport substitution controls remain blocked.
The fixture explicitly leaves validator-1 absent and does not create native
receipt authority.

Qualification artifacts are outside the source and sealed R46 trees:

- `/mnt/data/sn-testnet/qualification/r46-adversarial-source-evidence-20260926/pre-fix.log`
- `/mnt/data/sn-testnet/qualification/r46-adversarial-source-evidence-20260926/pre-fix-test.go`
- `/mnt/data/sn-testnet/qualification/r46-adversarial-source-evidence-20260926/fixed-focused.log`
- `/mnt/data/sn-testnet/qualification/r46-adversarial-source-evidence-20260926/fixed-normal.log`
- `/mnt/data/sn-testnet/qualification/r46-adversarial-source-evidence-20260926/fixed-race.log`
- `/mnt/data/sn-testnet/qualification/r46-adversarial-source-evidence-20260926/retained-evidence-summary.json`
- `/mnt/data/sn-testnet/qualification/r46-adversarial-source-evidence-20260926/qualification.json`

Tests use Go 1.26.6 and the existing pinned external modfile
`/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod`
(SHA256 `c38fba12a5d6fe522eaebd02aea699c834565aa5ce76a8f8b3e0775b0821f010`).
The combined normal/race selector covers the new actor tests, source-aware
scenario authority, provisional projection, native source projection, bounded
consensus models, required-metric completeness, and owned RPC routing. Exact
commands, outcomes, and source fences are in `qualification.json`.

The adjacent call-site review found no remaining direct legacy
`inspectValidatorIntent` call outside the shared selector. Legacy final input
collection is fenced against V2. The separate dishonest-deposit helper still
contains a legacy intent-file reader; it was not exercised by this reproduction
and is not qualified by this patch. Existing emulation coverage constants are
also unchanged; they cannot replace the release's actual mask, cohort, and native
receipt gates.

A future qualifying run needs fresh original signed sources and native
commit/reveal/application evidence for both validators in its own signed
acceptance window, validator-1's actual required affiliation/self masks,
independent coverage of every legitimate cohort, complete compact source
capture, and a concurrently passing adversarial artifact with every required
metric and sample floor. Historical local epoch 1661, retrospective replay,
or an actor-only emulation success cannot supply those missing facts. This
review made no chain requests, broad RPC scan, live-state repair, or sealed
evidence write.
