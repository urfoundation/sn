# Registration transaction checks and complete server model execution

Server `da4621fe9ffdc627bdf40aa8bbc7f78e56485a33`, based on the integrated
server `936c3d95`, completed its affected qualification and full model execution.
Astra authored the change; Terra executed the bodies on separately owned
PostgreSQL/Redis fixtures. No live database or chain was changed.

The 24 affected model roots and five controller roots passed normally and with
race detection. The maintained positive capture passed all eight stages with
unchanged source and joined exit-0 bodies. Controls removing durable binding
and original-request validation each reached their intended failures normally
and under race detection, with unchanged source and four passing control stages.
The initial stale-allocation control was ineffective and remains a failed
qualification. The separately qualified
[allocation-attempt fixture correction](registration-allocation-attempts-qualification-20260928.md)
then proved that root cause in both modes; final identity equality alone had
missed redundant work inside a rolled-back transaction.

The original full-model normal body ran for 5,064.304 seconds and joined with
exit 0 and a Go package PASS. All **1,125 expected top-level tests** reached
terminal outcomes: **1,118 passed, seven skipped, none failed**. An additional
167 legacy subtests passed. This is complete execution with disclosed skips,
not a zero-skip full-suite qualification.

| Skipped root | Missing fixture input |
| --- | --- |
| `TestGeoLite2CitiesAsLegacyRowsResolveToThemselves` | GeoLite2 places list / `PLACES_YML` |
| `TestNetworkReferralCode` | `pro.yml` |
| `TestNetworkReferral` | `pro.yml` |
| `TestAddReferralBonusesGrantsBothSides` | `pro.yml` |
| `TestReferralBonusCount` | `pro.yml` |
| `TestProConfig` | `pro.yml` |
| `TestOnboardingRegistryFile` | Adjacent config repository's onboarding file |

The full capture's maintained checker rejected the first undeclared legacy
child, `TestRemoveBoundAuthMethodMatrix/apple`. Its original root-only PASS
declaration also cannot express the seven skips. Preserve that failed report;
do not relabel it as passed or repeat the 84-minute body for metadata repair.
The independent `retained-full-model-outcomes.json` compares the complete
original event stream against all 1,125 predeclared roots and records every
root/child outcome, skip reason, original joined exit, package terminal and
input hash. It does not infer passing expectations from observed events or
grant strict qualification. No body was repeated for this receipt.

All ten physical source manifests matched before and after, and the resolved
module graph was unchanged. Model vet and owned-service cleanup exited 0.
The overall driver exit remains 1 because it retains the ineffective original
control and full-capture checker rejection. The earlier missing-runner refusal
is also retained separately; it started no test body.

Raw evidence is in
`/mnt/data/sn-testnet/evidence/operator-registration-server-20260928/run2`.

| Receipt | SHA-256 |
| --- | --- |
| `affected-capture/report.json` | `1f8f3190a182792eb8aec838360f071feed853bd30c3956392e10a7fe5f91ed8` |
| `missing-binding-capture/report.json` | `dd81ca75611091bb1afda076edc97efcf40103e8df76624f5e6b53d1547ab8b0` |
| `missing-original-request-check-capture/report.json` | `17c00f0049b36a1fa5d932e50df56cd14761916ec17a80157ebb87a1b0530e1e` |
| `stale-allocation-snapshot-capture/report.json` | `fd78287839d2d332c90a281682201c26a1765970cd596382efe39c9990a41ca1` |
| `full-model-capture/report.json` | `25666659a671bc9aba3dbf216475812a0c696d267e0949af708939fd1ef9d7e6` |
| `retained-full-model-outcomes.json` | `34445776cdf52b94793a8b98d0d758f2431b8886bac30e153e46e9dccc77d545` |

This graph used SN `9da4213d`, SDK `42241118`, Connect `358cefae`, Warp
`7498864c` and the pinned local replacements, including physical operator-proxy
and nested SCTP. Complete revisions, manifests, actual module paths and commands
remain in the capture. Later grammar and allocation-fixture checks have their
own receipts. The actual SDK-to-production-API/database test uses a newer
separately frozen graph; it, migration/rollout, release composition and live
acceptance remain separate gates. These bodies do not qualify those changes.
