# Server 720e integration gate

This is a read-only source review, not a compatibility or deployment pass.
Server `720e7c61182983dd2cd6de667787bb5b52f4d8a4`, tree
`4418c879d562fdf2ec2b9c5e10eebda3c6aede9b`, adds three commits and changes 48
files relative to the qualified server
`0b8e758db9ce5516de867e1b5d0a1c9660a0880b`. The reviewed checkout was clean.
Existing anchor, operator and release receipts retain their exact `0b8e758d`
scope. No frozen checkout, signed custody, production config or service was
changed by this review, and no tests were executed as part of it.

The three changes stage conservative subscriber/proxy classification
(`dfd2c235`), improve local provider search (`6c434f11`), and disable eager
unpacking in timestamp-rewritten publishing recipes (`720e7c61`). No direct
change appears in the ST controller/model authority paths, `stabi`, `strecovery`,
`stmonitor`, `go.mod`, `go.sum`, or the eight service Dockerfiles. This limits the
expected integration surface; it does not qualify the new composed binary graph.

## Required schema and policy decisions

The appended migration at index 749 advances the database from **749 to 750**
and adds `network_client_location.arin_quality_verified NOT NULL DEFAULT false`.
New connection writers and reliability queries reference the column even when
subscriber enforcement is disabled. Apply and audit migration 750 before new
server binaries take traffic; merely leaving the policy off does not make a
749 schema compatible. Existing startup readiness compares the database with
the new binary's full migration head. Earlier signed-history migrations and
their identities are unchanged. See the exact
[migration append](https://github.com/urnetwork/server/blob/720e7c61182983dd2cd6de667787bb5b52f4d8a4/db_migrations.go#L9437)
and [readiness check](https://github.com/urnetwork/server/blob/720e7c61182983dd2cd6de667787bb5b52f4d8a4/router/warp_handlers.go#L154).

**Keep `subscriber_quality_policy_version` absent or `0` in the reviewed launch
`provider.yml`, and keep the v2 candidate classifier/MMDB out of active release
inputs, unless miner-trail compatibility has been independently proven and
approved.** This is a launch constraint, not an observation of live config.
Absence/zero retains legacy serving policy; malformed, unavailable or unsupported
config is not an approved way to disable enforcement.

The [SN seed picker](../../validator/transport.go) requests eight best-available
providers with `rank_mode="quality"` and `force_minimum=true`. That fallback
deliberately admits connected providers before speed/latency history exists,
because validator trails produce the history. The new
[subscriber guard](https://github.com/urnetwork/server/blob/720e7c61182983dd2cd6de667787bb5b52f4d8a4/model/provider_subscriber_eligibility.go)
requires affirmative subscriber facts on every current live connection when
v2 is enabled, including named providers, force-minimum, Speed borrowing and
Online fallback. A legacy MMDB, missing row or unclassified connection cannot
qualify. No returned candidates means `no seed providers available`; a successful
API response or healthy service process does not prove trail progress.

The candidate's affirmative access catalog is deliberately narrow. Enabling it
without measured miner coverage can remove required seed providers and prevent
fresh trail/proof progress across both operators. Independent proxy-risk positives
also affect Speed/Online/probe admission. Keeping only the runtime flag off is
insufficient if the candidate MMDB is promoted: its raw `risk`/`non_quality`
flags still affect existing classification paths. Keep both inputs staged. The
server's [cutover requirements](https://github.com/urnetwork/server/blob/720e7c61182983dd2cd6de667787bb5b52f4d8a4/arindbctl/CLASSIFICATION.md#L166)
also prohibit claiming consistent v2 enforcement with old API binaries.

Before a future v2 enablement, prove a populated two-operator cohort through
the actual SN picker and server handler, using initially unmeasured miners,
current/missing/changed connection facts, warm caches, and restart. Observe
fresh completed trails and their proof/accounting progress; do not treat a
request-shape unit test as that proof. Bind exact classifier rules/MMDB generation,
API and Connect writer versions, rollup/index refresh, config bytes and cohort
coverage. Include rollback and mixed-generation cases, including an old writer
updating a row previously written by a new writer. The new migration regression
covers old inserts/defaults; it does not by itself prove that mixed update case.

## Focused compatibility gate

Independent compatibility qualification should retain its own exact SN commit,
server `720e7c61`, external modfile/resolved graph, test census and logs. Keep the
primary activation qualification on its already selected `0b8e758d` graph until
the separate gate completes. Do not silently substitute the legacy
`/home/by/urnetwork/server` checkout or chase later dependency heads.

| Scope | Focused evidence required |
| --- | --- |
| Schema/readiness | `TestSubscriberQualityMigrationFrom749`, `TestStartupReadiness`, and migration identity/audit checks on a disposable 749-to-750 database. Confirm 749 refuses new startup and 750 permits it. |
| Subscriber facts and selection | `TestArinSubscriberEvidenceRequiresVersionedPositiveLookup`, both `TestArinConnectionFacts*` roots, `TestSubscriberQualityActivationDefaultsOff`, `TestSubscriberQualityActivationRejectsInvalidPolicy`, and `TestQualityRequiresSubscriberAcrossNativeFallbackForceAndNamed`; adjacent native-index, postfilter/refill and IP-family selection roots in normal/race modes. |
| Search | `TestCompactHistogramPreservesLegacyPredicate`, `TestSearchLocalAllAliasesMatchBruteForce`, `TestSearchLocalSnapshotPreservesMatchesAndLimit`, and `TestSearchLocalConcurrentSnapshots`, including race. The change publishes immutable per-value maps; it does not change the public lookup API. |
| SN consumers and operator retention | Compile/build and vet the exact composed SN/server graph; run the selected current-start/anchor roots and `TestFindProvidersSeedPickerRequestsBootstrapCandidates`. Retain the three `TestStPayoutPolicyRollover*` roots against the appended schema to protect both operators' prior signed history. These checks do not authorize subscriber v2. |
| Image recipes and source joins | `TestRuntimeImageBuildPinsEpochAndRewritesLayers`, `TestRuntimeImageRejectsInvalidEpochBeforeBuilder`, adjacent container checks, and the release builder's source/image argument and aggregate checks. Rebuild/read back the exact successor artifacts before claiming local source-to-image coverage. |

The optional subscriber query-plan/source-cost benchmarks are performance
diagnostics, not a substitute for cohort correctness or production capacity
evidence. A default-off launch need not activate or publish candidate resources
to run its compatibility checks.

## Successor release requirement

The publishing Makefiles now specify
`type=image,push=true,rewrite-timestamp=true,unpack=false`; no Dockerfile or
package lock changed in this delta. SN's qualified local image builder uses the
separate `type=oci` exporter with network-disabled pinned contexts. Do not infer
an offline-builder incompatibility from the publishing flag change, or invoke a
publishing recipe to obtain local evidence.

Any release selecting `720e7c61` must bind the new exact source and migration
inventory, the launch policy/config and classifier inputs, rebuilt executables,
all eight selected image/platform manifests, and fresh source-to-image joins.
Preserve the earlier `095a2208`/`0b8e758d` release as historical evidence. Neither
that aggregate nor the anchor's `0b8e758d` tests cover this candidate by
inheritance. Independent provenance, deployment approval, live installation
and subscriber-policy activation remain unproven.
