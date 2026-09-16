# Software revision reserve-target refusal

At 02:48:30 UTC on 2026-09-16, the read-only native plan revision exited 1.
The patched journal executable was built from `1270adc17cd0097226b2b9fb188c283ffc9c5405`.
The intended change was its software identity; no successor plan was emitted
or adopted and no transaction was submitted. All six watched state files,
the executable and the release lock retained their exact bytes. The actual
failed command, stderr, body/outer/join exits and fences are in `plan-refusal/`.

The planner demanded 471,808,849 alpha rao for a fresh repair to its 65% target,
but current plus superseded liabilities already use the full approved
37,250-alpha lifetime limit. This is an actual refusal, not a successful plan.
The 6,000-alpha repair previously finalized at block 8,009,634 and its pinned
target proof remain valid historical evidence.

## Current on-chain observation

The read-only LAN census in `live-census/` was captured at 02:54:45 UTC:

| Item | Value |
| --- | --- |
| Native finalized block | 8,015,417 |
| Native block hash | `0xd0bc6e10880e751dd97880f2d32ecb48fa0aee8e22f6fc78f33f5cd2a46656ad` |
| Runtime | 460 |
| Registered UIDs queried | 256 |
| Registered alpha rao | 92,840,400,514,372 |
| Reserve alpha rao | 60,345,788,525,494 |
| Reserve share | 64.9994918065% |
| 60% operating floor | Pass at this snapshot |
| 65% repair target | Fail at this snapshot |
| Additional credit needed for 65% | 471,808,848 alpha rao |

All 256 UID identities match the earlier complete census. All 256 hotkey
stakes were queried at the same native finalized hash; 248 absent
`TotalHotkeyAlpha` entries use the runtime's `ValueQuery` zero default.
The extra one rao in the planner's required transfer covers its minimum-credit
rounding allowance. This observation does not establish an end-of-run share.

Requests and responses are preserved exactly. Reproduce each request with an
archive-capable node, for example:

```sh
curl --header 'Content-Type: application/json' \
  --data-binary @live-census/census.request.json \
  http://192.168.1.162:9944
```

New evidence uses the user-owned LAN node, without request pacing or public
fallback, and records `independent_rpc=false`. The copied `command.sh` retains
the original private capture paths as execution provenance; it is not a
portable replay command and must not overwrite that completed capture.

## Recovery scope

The ordinary planner tries to restore 65% on a release revision, while ongoing
reserve-majority admission uses the 60% operating floor and completed repairs
replay their 65% proof at the original transfer block. A bounded software-only
recovery correction retains that completed chain when the whole predecessor
approval matches after restoring only the release identity and ancestry. It
retains original spending, target proofs and dependencies, checks the fresh
floor, and preserves ordinary planning for initial, unfinished,
changed-economic and below-floor cases.

Focused qualification is accepted with **28 roots normally and under race**,
including the full authenticated revision renderer and adjacent fleet recovery.
Restoring the original planner reproduces **three expected failures and three
passing controls**. The original failed invocations, test-fixture corrections
and unrun race bodies remain explicitly recorded; acceptance composes retained
results with three replacement roots per mode.
[Exact composition and raw receipts](qualification/README.md).

The matched release build and native adoption are next. This local qualification
does not establish a new live reserve-target result. The soak remains stopped.
