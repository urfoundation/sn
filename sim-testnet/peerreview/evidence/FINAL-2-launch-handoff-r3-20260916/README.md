# Closed launch handoff for recovery r3

This package records the completed continuation import, strict-history capture,
and temporary-helper teardown on September 16, 2026. It includes only closed
receipts. Strict-resume session 92055 and the release-candidate invocation are
excluded; no startup or campaign outcome is claimed.

## Exact continuation import

Root's session 30705 ran the `relay-apply-r2` body from **09:19:38 to 09:35:50
UTC**. [Body](relay-apply-r2/body.exit), [outer](relay-apply-r2/outer.exit), and
[join](relay-apply-r2/join.exit) exits are all 0. The complete
[197-byte result](relay-apply-r2/stdout.json) reports `adopted` and
`chain_transactions: 0` for plan
`0x17e49d00a7ce6aafac856e81a4ccf9eb37e4714ba7570a24cfa1c97a2d941f37`.

The [request](relay-apply-r2/REQUEST.json) binds the exact emitted plan SHA-256
`e40de369ee48a5bbc7c4b752295187cdc1f44e49117d2d324744f62b3ad90a48`.
The watched saved-plan digest after import matches that hash. Of the six
watched state files, **only `plan.json` changed**; journal, supervisor state,
supervisor manifest, redacted configuration, and public identities retained
their exact digests. Consequently `state_compare_exit=1` in the
[original result receipt](relay-apply-r2/result.status) records the expected
plan adoption. It is not a command failure. Binary and release-lock comparisons
are both 0.

The [closed capture and lossless review](../FINAL-2-relay-capture-r3-20260916/README.md)
retain the complete 27-path comparison, unchanged actions and approvals, and
hashes and sizes of the omitted large plans. The import records no new funding,
renewal, or chain transaction.

## History bundle and native epoch

Root's session 67825 ran history capture from **09:37:02 to 09:37:28 UTC**;
body, outer, and join exits are all 0. The complete
[2,201-byte history bundle](history-adoption/stdout.json) has SHA-256
`f2a7e1a24fc02cb9fbc3ee85af757b5795982ec42731f2b9fd34564485c479c9`.
The [binding receipt](history-adoption/BOUND-HISTORY.json) records its adopted
plan, native epoch, and saved local path. Packaging checks this closed receipt
against the captured bundle; it does not reopen the active saved-state path.

All six watched state hashes remained unchanged during history capture,
as did the executable and release lock. The bundle preserves validator 1's
empty intent prefix and validator 2's three-entry prefix, whose retained last
native epoch is 1405. Both validators are bound to first native epoch **1488**.

The complete closed [LAN requests](history-adoption/native-schedule/schedule.request.json)
and [raw responses](history-adoption/native-schedule/schedule.response.json),
[observation](history-adoption/native-schedule/OBSERVATION.json), and
[selection](history-adoption/native-schedule/SELECTION.json) are included.
At finalized block 8,017,425, runtime 460/1/1, the observed epoch was 1487,
last epoch block 8,017,091, tempo 360, and 334 blocks since the last step.
The selected epoch 1488 spans **[8,017,451, 8,017,811)**. Both first decision
and prepared snapshots must fall inside it. The fixed full-work start cutoff
remains 8,017,728, within that interval. These recorded bounds do not establish
that a later launch met them. All chain observations use only
`192.168.1.162:9944`; no independent-RPC verification is claimed.

## Witnessed temporary-helper teardown

The successful teardown witness ran **09:39:58–09:40:08 UTC**. Its
[closed owner receipt](temporary-services/teardown-witness/CLOSED-RESULT.json)
records owner PID 3626604, start ticks 183793743, and join exit 0.
The four helper exit codes are **unavailable** because their transient launch
parents had already exited. The result is `terminated-observed`, not a claim
that the helpers exited successfully.

| Helper | PID | Start ticks |
| --- | ---: | ---: |
| Subtensor EVM egress | 3527769 | 183368942 |
| Subtensor RPC proxy | 3527795 | 183368968 |
| Operator 1 API | 3527805 | 183368994 |
| Operator 2 API | 3527809 | 183368995 |

The [pre-signal identities](temporary-services/teardown-witness/pre-signal.tsv)
bind each original PID, start ticks, process group, and session. All four
[TERM dispatches](temporary-services/teardown-witness/signal-dispatch.tsv)
returned 0. The [observation sequence](temporary-services/teardown-witness/termination-observation.tsv)
ends with every original PID absent, and the
[listener snapshot](temporary-services/teardown-witness/listeners.after.txt)
contains only its header: all six owned listeners had closed. Root's separate,
closed [prelaunch witness](PRELAUNCH-WITNESS.json), observed at 09:41:15 UTC,
records the same absence and saved-plan digest. No live process was inspected
or signaled while packaging these records.

Two preceding failures are retained. At 09:39:30, the
[first wrapper](temporary-services/teardown-witness/owner-launch-attempt-1.txt)
failed to redirect output before its destination directory existed. At
09:39:45, the [second preflight](temporary-services/teardown-witness/preflight-attempt-2.txt)
used the wrong receipt field. Both failed before executing the teardown or
sending any helper signal; neither has a helper exit result.

The original invocation's
[SHA256SUMS](temporary-services/teardown-witness/SHA256SUMS) is preserved as
historical evidence. **It is not a valid closed seal:** its file enumeration
included its own changing manifest and files still open during the invocation.
The authoritative [25-entry closed manifest](temporary-services/teardown-witness/CLOSED-RECEIPT-MANIFEST.tsv)
was created after the owner joined. Its SHA-256 is
`f76c7558f04c34ccbf42065551dc7f37bad54cd56bf42f7b42710a5f8b424792`.
All 25 entries, the original seal, notes, and explicit exclusions are included
byte-for-byte. The original manifest's mode column describes the local source
receipts; packaged copies use readable file modes.

## Provenance and verification

Both completed native commands bind source
`aeda6abbd2dc0abc92bb0f60975cf89b509e8017`, executable SHA-256
`8fc61a65cd0524413a7ba70c61bcdb15962fa87ad7ab347b653abb27f8913f0b`,
and release-lock SHA-256
`bf417189d4c0a62f8116606f84f9c5509b3afe2dab611429c9b781998b3198fc`.
[SOURCE-FILES.tsv](SOURCE-FILES.tsv) maps all copies to their closed originals.
[WATCHED-STATE-COMPARISON.tsv](WATCHED-STATE-COMPARISON.tsv) compares the recorded
six-file watch set for each phase. [VERIFICATION.json](VERIFICATION.json)
records the offline checks, and [OMISSIONS.json](OMISSIONS.json) identifies
excluded payloads and active operations. Original command files are inert
provenance, not instructions to execute against a live deployment.

Verify from this directory:

```sh
sha256sum -c SHA256SUMS
sha256sum -c PACKAGE-SEAL.sha256
(cd temporary-services/teardown-witness && sha256sum -c CLOSED-RECEIPT-PORTABLE.sha256 && sha256sum -c CLOSED-RECEIPT-PORTABLE-SEAL.sha256)
```

The two portable teardown checksum files preserve the closed manifest's exact
digests using relative paths. They do not repair, replace, or validate the
invalid invocation manifest. Original absolute-path seals remain unchanged
for provenance. Packaging performed no tests, builds, RPC calls, active-state
access, process actions, source changes, or publication.
