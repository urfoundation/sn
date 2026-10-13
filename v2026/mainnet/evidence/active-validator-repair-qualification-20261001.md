# Active steering-hang repair source qualification — 2026-10-01

Source `38c61a4040e8549f87389c849ee98e97a3a8f6c4`, tree
`0989d84facc386d5256705dd5de04355eb8167a1`, is qualified for the bounded
[active repair interface](../ACTIVE-VALIDATOR-REPAIR.md) described here. The
isolated branch was rebased onto current main `fcd4ab9f` before final runs.
Documentation and this receipt follow the tested source without changing Go code.
This is source qualification, not production approval or deployment acceptance.

The capability admits a separately signed exact v4 steering incident, role,
source, release, installed unit, host/boot and previous process generation.
Current responsive/unknown observations, changed policy/identity and insufficient
signed stop/join/start headroom refuse a stop. Separate durable cuts consume one
stop and one start; genuine cgroup-v2 descendant emptiness is required between
them. The complete old checkpoint remains inside the approval and journal.
Permanent generation claims reject a second envelope or state path. Stopped
repair and activation share installed-unit exclusion, and stopped repair cannot
take over an active-repair lifetime claim. An uncertain start never retries.

No real unit was installed, stopped, started or reloaded. No production identity,
secret fixture, chain signer, transaction submission or broadcast was used.
Synthetic manager callbacks perform the effect assertions; the existing actual
child-process test separately verifies command cancellation and join.

## Positive and independent checks

| Check | Exact result |
| --- | --- |
| Selected normal `./mainnet`, `-count=1` | **PASS**, 60 top-level roots, 73.334 seconds. |
| Same selection with `-race -count=1` | **PASS**, 60 top-level roots, 490.861 seconds. |
| `go vet ./mainnet` | **PASS**, exit 0, empty diagnostic log. |
| Independent review on the same source/tree | **PASS**, 33 top-level roots normal/race, 9.703 / 46.110 seconds; vet exit 0. |

Each test selection also contains the one intentional
`TestRepairValidatorProcessHelper` skip. That entry runs only in the explicitly
spawned synthetic child; it is not counted as a positive root.

The primary 60-root selection is exact: 20 new active-repair roots, 13 existing
stopped-repair roots, 11 steering-liveness roots and all 16 roots in
`validator_activation_test.go`. `selected-roots.txt` and `selected-regex.txt`
retain every name. The full normal/race JSON streams are retained, including
barriers and fixtures covering:

- Fresh publisher with unchanged steering, direct recovery before the monitor,
  recovered checkpoints, missing/unavailable reads, clock and policy/source drift.
- Post-fsync source/custody changes; missing or tampered state/markers; loss of
  either generation claim artifact; alternate signed envelopes and journals.
- Lost stop acknowledgement, bounded join/observation exhaustion, empty main-pid
  view with populated descendants, post-stop availability loss and cancellation.
- Lost start acknowledgement, four ambiguous publication cuts, changed old join
  after start reservation, actual new steering completion and historical reopen.
- Stop propagation/kill-profile/prerequisite refusal, signed expiry headroom
  before and after reservation, shared activation/stopped-repair ownership, and
  refusal of a stopped-repair takeover after the active owner exits.

## Causal controls

Seven isolated Go overlays each remove one protection from the frozen source.
Each named root fails its intended observable assertion in **both** normal and
race modes: 14 expected failures, zero compile/panic/setup substitutions. The
working source is never changed by these overlays.

| Removed guard | Named failing root |
| --- | --- |
| Direct unchanged steering outcome | `TestRepairActiveValidatorRefusesRecoveredOrUnknownSource` |
| Current incident read after durable stop reservation | `TestRepairActiveValidatorStopReservationRechecksSourceAndCustody` |
| Descendant population check | `TestRepairActiveValidatorDescendantJoinIsRequired` |
| Exclusive lifetime generation claim creation | `TestRepairActiveValidatorDuplicateEnvelopeCannotRenewGeneration` |
| Actual replacement steering outcome | `TestRepairActiveValidatorCompletionRequiresActualSteering` |
| Signed recovery headroom before stop | `TestRepairActiveValidatorStopRequiresRecoveryHeadroom` |
| Stopped-repair refusal of active generation custody | `TestRepairActiveValidatorLifetimeClaimExcludesStoppedTakeover` |

## Reproduction and sealed artifacts

Restricted evidence root:
`/mnt/data/sn-testnet/mainnet-active-hang-repair-20261001/`.
The 68-file `manifest.sha256` covers raw runs, exact test selectors, module files,
dependency context, every overlay/source/log, causal results, independent review
and the machine-readable qualification. Build scratch/cache are excluded.

- `manifest.sha256` SHA-256:
  `35ea35c055766c39a87e1bd545b216328a27ad1440e7fe48224089543a4dfc40`
- `qualification.json` SHA-256:
  `9918d54b0d5470f580f5c220556a7b2f8ccb57e1802eb998065fb64b95ccecda`
- Independent receipt SHA-256:
  `e3a61fd98ed0c4d42b7468c5bf75fd1fb2643e937fc50aba1ff14d07506c9250`

Go 1.26.6, Linux/amd64. The external `qualification.mod` only resolves six sibling
replacements and the local third-party paths to their absolute source locations;
tracked module files are unchanged. Primary clean sibling heads:

| Module | Commit |
| --- | --- |
| server | `898dc8f3b211d1e2fca1b0a0c970f7673b36fd7b` |
| warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` |
| proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` |
| userwireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` |
| glog | `892ade4a6be396b32ea82a550f243190b5992180` |
| goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` |

The independent reviewer used a separate absolute server integration checkout
at `0b8e758db9ce5516de867e1b5d0a1c9660a0880b`; its modfile and receipt retain that
distinct graph. Neither checkout nor the primary modfile was changed by review.
Connect/SDK remain the reviewed version replacements in tracked `go.mod`.

Primary commands use the retained regex with `go test -mod=readonly
-modfile=.../qualification.mod ./mainnet -run REGEX -count=1 -json`, plus `-race`
for the race run; timeouts are 15/20 minutes. `select_tests.go`,
`causal_controls.go`, `collect_context.go` and `seal.go` retain the reproducible
selection, mutation, context and sealing logic. Earlier development runs are
retained separately and are not substituted for the frozen-source results.

## Remaining gates

MG-07/PH-28 remains open for independently issued production envelopes and SLOs,
exclusive host/signer/volume custody, anti-rollback/backup policy, exact installed
systemd property and stop/join behavior, a disposable real-host crash rehearsal,
deployment, delivered alerts and on-call response. Observations and a local lock
cannot atomically prevent a worker recovering after the final read, fence another
host or prevent privileged rollback; those limits must be reviewed explicitly.
An exhausted/failed recovery may leave the unit stopped for manual disposition.
Root/operator repair, initial activation, canonical chain/economic acceptance and
monetary repair remain separate. This receipt grants none of those authorities.
