# MG-04 / RT-01, RT-03 / PH-04: validator source receipt runtime views

Date: 2026-09-29. The frozen candidate is
`76951cca82811334b7cc5e1c5db4576ffcc6d33d`, following implementation
`99bd94c0dfcbb90bda4bbc198a4dd5e8266c5264`. The candidate is pushed on
`fix/validator-runtime-history-20260929`. SN root
`codex/mainnet-hardening-20260927` integrates these commits as
`3bd3ab09fec15dbe6ac5197738f6f4facd9bf40f` and
`4d970602a44ac50a0e84e89765b8200d345a7373`, after server documentation
`bc434270314b4030adc1ad1b20786ccf93d57151`. All ten changed source, test
and overview files were verified byte-for-byte against the frozen candidate
after integration. No conflict or production-source adjustment was needed.

The [source overview](../VALIDATOR-SOURCE-RUNTIME.md) describes the concrete
failure: production pending recovery and semantic archive readers rebound an
original signed source to the inclusion block's post-state runtime. If that
block installed an approved compatible successor, source validation rejected
the signature solely because the post-state spec differed from its signed
execution domain.

The correction retains three independently authenticated views. Original
preparation selects the complete historical signed authority and unchanged
extrinsic bytes. The inclusion header's parent selects execution metadata and
must still validate the original signature. Inclusion post-state selects the
commitment storage decoder through its own approved runtime window. Complete
canonical headers bind the parent and heights; receipt verification checks the
committed body, dispatch and source events before exact commitment readback.
Native capture retains the execution metadata even when already cached.
Historical binding grants no current producer or signing authority. Missing
approvals, incompatible consumed interfaces, changed execution signing domains,
unavailable archive evidence and canonical conflicts remain hard errors.

Sol's sealed [behavioral receipt](/mnt/data/sn-testnet/qualification/validator-runtime-history-20260929/RESULT.md)
has SHA-256 `0981f394ea1289c81c2661cc0ae76a2b2b696024e4c669f3e531e823cec079da`.
The [per-root census](/mnt/data/sn-testnet/qualification/validator-runtime-history-20260929/CENSUS.json)
has SHA-256 `8b8b058d87fba0a50bfb18173693d62b5cab2f2cb74e186e15b41307b675be4d`.
The census checks the exact declared roots against raw Go JSON pass events;
there are no missing roots, unexpected roots or root failures in the selected
qualification:

| Selected scope | Normal | Race |
| --- | ---: | ---: |
| New historical source receipt | 7/7 | 7/7 |
| Validator runtime, authority, continuation, receipt and source-finality neighbors | 51/51 | 51/51 |
| Native capture | 15/15 | 15/15 |
| Steering loop/native HTTP | 5/5 | 5/5 |
| CRv4 source commitment and unavailable receipt | 25/25 | 25/25 |
| Total selected roots | 103/103 | 103/103 |

The seven new roots use selector `^TestProductionSourceReceipt`. They cover
ordinary and digest-bearing upgrades, separated view ownership, retained
original approvals and signed bytes, independently approved incompatible
storage, missing approvals, header/code substitution, archive failure,
cancellation, dispatch failure, commitment mismatch, native capture and
durable pending recovery without rebroadcast. Full selectors, raw logs and
shard manifests remain beside the sealed receipt. These counts do not claim a
whole-package or combined-release run.

The 51-root adjacent normal run and original broad race run started at parent
`99bd94c0`. The successor only fixes JSON result decoding and precise negative
assertions in the new receipt fixture; no production file or selected adjacent
test body changed. Three disjoint adjacent race shards started after the
successor was fenced. Their passes plus explicit pass events from the original
broad run cover all 51 roots. That broad process was still active when the
receipt was sealed; its package completion is not claimed. The old parent's
focused normal fixture failures and intentionally stopped focused race attempt
remain in their original logs, separate from the seven passing successor roots.

The isolated [causal patch](/mnt/data/sn-testnet/qualification/validator-runtime-history-20260929/control-old-poststate.patch)
has SHA-256 `3ac4560f2f4b5dce66c5bdf3cae575dd8668050a6d825faa49d3f132e9d559c2`.
It restores the old post-state `VerifyFinalizedSourceContext` call after
binding the approved successor. The unchanged upgrade-boundary test passes on
the candidate and fails on that mutant in normal and race modes, with both
digest variants reporting `crv4: prepared source chain or runtime differs from
independent signing authority`. The mutant is isolated from the candidate and
integrated source.

`go vet ./validator ./crv4` passed. Compilation and qualification used an
external absolute-replacement modfile and the complete pinned dependency tree,
including SDK `516521fb16da46c9f4bff0b58221e1941694f616`; tracked module files
were unchanged. The physical module graph was byte-identical before and after,
SHA-256 `f83f7046b913c4839492810f66952e6a761f6de256724ad2c3e2634a5e0ee67c`,
and every local replacement remained clean at its pinned commit.

The receipt path authenticates complete headers including
`RuntimeEnvironmentUpdated`; the digest fixture reads the upgrade receipt from
the following finalized block because other SDK current-head consumers still
need qualification. Automatic runtime approval, broader native consumers,
both validator roles, composed release/dependency qualification and live
upgrade acceptance remain open. Tests used deterministic local RPC fixtures
and synthetic signatures; no live chain, custody key or deployment was touched.
The hashed local raw records must be archived with any portable release proof.

**Test-style follow-up (2026-09-29).** Frozen candidate
`2b6102d7e03305f8b1dbf9a294368218d4eaa932` follows root
`1ab013a033f538a6cc220694fd0d4c199d505c20` and is integrated unchanged by
fast-forward on `codex/mainnet-hardening-20260927`. Its only changed file is
`validator/production_source_receipt_test.go`. A plain loop replaces the
ordinary `t.Run` boundary for the two homogeneous digest variants; contextual
errors retain both case outcomes. All seven top-level names remain unchanged.
The unused import and acronym comment capitalization are also corrected.
All production files remain byte-identical to the preceding root.

Sol's sealed [style qualification receipt](/mnt/data/sn-testnet/qualification/validator-receipt-test-style-20260929/RESULT.md)
has SHA-256 `a3f18bbfd83a4b0aa9abc01311f22a650aafc2acd589d0081fe7d60e0b7f8073`.
The seven declared roots passed normal and race qualification with zero
failures, skips or child events; `go vet ./validator` also passed. The isolated
old-post-state control failed in both modes at the intended independent
signing-authority mismatch, with both digest variants reported. No race report
occurred. These are the focused follow-up results; the earlier 103-root
qualification above was not rerun for this test-only change.

The [exact selector](/mnt/data/sn-testnet/qualification/validator-receipt-test-style-20260929/focused.selector),
raw normal/race logs, causal patch and pinned dependency records remain beside
the sealed receipt. The module graph was byte-identical before, after and
following qualification, SHA-256
`fda5020aa587f3d135f7e310b97e5f9dacf3403bbc7f489f2c2844a8cc2584e5`.
Integration verified the test file against the frozen candidate, SHA-256
`492811dd2aea115b0bdeafb02d6d3f0ecaad3c7aa9b9e6ac11c3e70020cfec70`;
no behavioral reruns or production changes were needed during integration.
