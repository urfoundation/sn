# R46 signed-source capture review

Reviewed SN revision `6adc7be2e4abdd5c17426dbdf9b8ca66536bdba7` in isolated branch
`codex/r46-capture-review-20260926`, worktree `sn/`. No source changes were needed.
The capture, stream transport, archive-budget and terminal-capture files match
the running diagnostic's `363882d6` source exactly. The live diagnostic, its
output and the shared integration branch were not modified. No LAN RPC calls
were made by this review.

The existing diagnostic options already use two origin readers, retry each
immutable read inside a five-minute operation budget with fresh one-minute
attempts, and keep completed chunk witnesses until the invocation finishes.
The key includes origin, typed route, hash and size. A witness is created only
after complete authenticated EOF, successful close and successful retention;
the shared sink enforces the global object/data/control allowances. These
mechanisms address the preceding serial capture timeout without granting
partial bodies or one origin's result authority for the other origin.

A new invocation intentionally starts with no origin witnesses. The existing
`TestReleaseCaptureStreamReuseRetainsEveryOriginAndSharedPrefix` explicitly
requires the next invocation to fetch again. Reusing prior output as capture
authority would change that contract. I found no additional concrete defect
that justifies such a change. An ordinary timeout returns already retained
sources to the failed check's evidence; it does not erase the archived bytes.

The source inventory itself can grow between attempts. Capture retains all
immutable controls present at its initial inventory, including closures after
the accepted publication window; `ThroughEpoch` limits publication selection,
not the source history required for replay. This explains why a later restart
can require more work even before accounting for repeated reads.

The read-only census at 2026-09-26 07:10:52 UTC is retained in
`stream-census.json`. It read small control/descriptor objects and checked
chunk path sizes; it did not hash or replay the large stream bodies.

| Measurement | Sealed v2 | Active v3 at census |
| --- | ---: | ---: |
| Input journals | 32 | 33 |
| Settlement closures | 31 | 36 |
| Closure epoch range | 604–634 | 604–639 |
| Signed cuts | 96 | 107 |
| Distinct nonempty stream references | 141 | 149 |
| Sum of distinct-reference data bytes per origin | 878,024,312 | 984,400,967 |
| References whose entire chunk paths are present | 114 | 104 |
| References with absent manifest | 27 | 44 |
| References with a manifest but incomplete chunk paths | 0 | 1 |

At that census v3 had 655,926,701 bytes of unique described chunks, of which
648,729,414 bytes had complete-sized paths. Missing manifests described at
most another 320,091,304 bytes. Thus the conservative upper bound on missing
distinct shared-archive data was **327,288,591 bytes**. If both origins owed
exactly that absent subset, it would require 654,577,182 bytes of reads.

Both origin observations map to the same content-addressed files, so path
presence cannot establish each origin's completion. Allowing the sibling to
owe every already-present body as well gives a conservative upper bound of
**1,303,306,596 bytes** of remaining origin reads:
`2 * (655926701 + 320091304) - 648729414`.
This excludes metadata overhead, retries and later publication readbacks.
It is a conservative data-work bound, not proof of origin completion or a
wall-clock guarantee. At the observed aggregate pace, comparable progress by
both origin readers would plausibly fit the approximately 30 minutes remaining
until 07:41; an origin lag or further transport stalls could still exhaust it.

Validation uses local deterministic fixtures only, with `GOMAXPROCS=2` and
`-p 1` to limit load. Exact selector:

```
go test -p 1 ./validator -run '^(TestReleaseCapture|TestReleaseNativeCapture|TestAttemptStreamV2HTTP|TestEvidenceTransport)' -count=1 -timeout=10m
go test -race -p 1 ./validator -run '^(TestReleaseCapture|TestReleaseNativeCapture|TestAttemptStreamV2HTTP|TestEvidenceTransport)' -count=1 -timeout=10m
```

Normal validation passed (`normal.log`, package test time 7.084s), and race
validation passed (`race.log`, package test time 13.589s). Both exited 0.
`test-receipt.json` records the exact selector, source revision and receipt
hashes. The isolated worktree remains clean.

No pre-fix red control was added because this review made no code fix.
