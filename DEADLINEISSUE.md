# DEADLINEISSUE: epoch 3 missed its payout root window (2026-10-30)

For review. On mainnet, epoch 3's close never produced a payout root, and its
commit window passed. The cause is a census read that cannot finish in time at
production scale. A fix is on a server branch, tested but not merged or
deployed. Epoch 4 will hit the same wall unless the fix is deployed before
**2026-11-06 about 18:00Z**.

## Summary

- **What failed:** `StEpochClose` for epoch 3 timed out twice, at the
  taskworker's 3 h limit, inside the closed-work census read. No root was
  built, so `StCommitRoot` never ran. The commit deadline (block 9399628,
  about 4 h after the epoch boundary) passed with no root.
- **Consequence (D-11):** epoch 3 finalizes root-missed. Per PLAN.md D-11, "A
  missed commit rolls that pool's total into the next epoch": the funding
  carries to the operator's next root, and epoch 3's providers receive no
  entitlement from epoch 3 itself. No root was backdated or invented, and none
  should be.
- **Cause:** the census statement materializes every credited contract in the
  window (about 404M rows for epoch 3) and reads them in index order, one
  random heap fetch per row. That takes about 3 h before the first row
  returns. It also reads every row's close reports, which the census then
  discards at this scale.
- **Fix:** the server branch `fix/st-census-speed`. Above the census record
  cap, it reads the same SQL in one bitmap-ordered pass with no report reads.
  Same results, about 25–30 min for epoch 3 and about 45–65 min projected for
  epoch 4.

## How the close is supposed to work

From PLAN.md (D-11):
- **Windows:** each epoch is 50,400 blocks, about 7 days.
- **Close:** `StEpochClose` runs at the boundary. It freezes the usage and
  reliability window, computes the leaves and builds the root.
- **Commit:** `StCommitRoot` must land within the commit window, +4 h (1200
  blocks) on mainnet, and should alert at T-2h if unconfirmed.

In code:
- **v1 path (epoch 3):** `taskworker/work/st_work.go` `StEpochClose`
  (`stEpochCloseMaxTime = 3h`) → `controller.StComputeEpochPayout` →
  `stPrepareReleasePayout` → `model.GetStEpochProviderUsageWholeCensus`. That
  runs `stEpochProviderOriginalUsageSql` (`model/st_provider_close_originals.go`,
  wrapping `stEpochProviderUsageSql`) on `server.MaintenanceDb`, in one
  REPEATABLE READ, READ ONLY transaction.
- **v2 path (epoch 4 onward):** `st_ledger_v2_activation.first_epoch = 4`, so
  epoch 4's payout reads usage in the v2 epoch step (`stLedgerV2NewEpochRows` →
  `GetStEpochProviderUsageAtEpoch`). That is the plain usage statement, with the
  same slow plan, inside a step limited to 2 h. **Without the fix, epoch 4
  misses too.**

## Timeline (UTC)

| Time | Event |
|---|---|
| 10-30 18:25 | `StEpochClose` epoch 3, attempt 0, claimed at the epoch boundary |
| 18:26:39 | Census read starts. It holds the xmin horizon and spills temp files |
| 19:40 | Temp spill stops growing at 110 GB (`temp_file_limit` is 150 GB); still reading |
| 19:47 | Monitor PAGE `pg/transaction-horizon`: horizon at least 1h20m old |
| 21:25 | `transfer_debit_journal` at 20.1M dead tuples against about 12k live |
| about 21:26 | First read cut at the 3 h limit; temp files released |
| 21:27 | The task restarts the same read |
| about 22:25 | Commit deadline, block 9399628, passes with no root |
| 22:28 | Monitor WARN `subnet/settlement`: "An operator missed its payout root window" (invariant root-within-window violated at finalized block 9399641) |
| 10-31 00:26:15 | Second read cut; the task ends `Timeout`, `{"retry":false}` |
| 00:32 | The `transfer_contract` anti-wraparound vacuum, held back by the horizon, finally completes |

**Side effects on main's primary:**
- the oldest-transaction horizon was held for about 6 h in total;
- two 110 GB temp spills;
- dead-tuple build-up in the hot queues, which vacuum caught up once each
  read ended;
- a delayed anti-wraparound vacuum.

Debit flushing stayed healthy (oldest unapplied 23–61 s).

## Root cause

All of this is from read-only EXPLAINs and stats on main.

1. **The window is large.** Epoch 3 holds about 380–404M credited closed
   contracts. They sit on about 35% of `transfer_contract`'s 105M heap pages,
   about 11 rows per page.
2. **It is materialized before any row returns.** `usage_rows` is a
   MATERIALIZED CTE of about 404M × 275 B. The `min(contract_id)` InitPlans
   force all of it into a tuplestore before the first row comes back. That
   tuplestore is the 110 GB spill.
3. **It is read in index order.** The CTE fills through an Index Scan on
   `transfer_contract_closed_usage` in close-time order, with one heap fetch
   per row. Measured on cold adjacent windows, that runs 10–20K rows/s in
   index order against 126–166K rows/s in bitmap order. At about 35K rows/s,
   404M rows is about 3.2 h. That matches both attempts still filling
   `usage_rows` when cut.
4. **The report work is thrown away.** For every row, the statement probes
   `contract_close_report_evidence` (973M rows, 275 GB) and builds report
   JSON. But the census keeps at most `MaxClosedWorkRecords` = 32768 records.
   Above that, `Records` is nil, and the function returns census = nil and
   window = nil.
5. **No completed run has been recorded.** `pg_stat_statements` (reset 10-23)
   has no entry for either the census or the plain usage statement. No
   `StEpochClose` task finished in the 30 days before; epochs 0–2 show
   `finalized` in `st_epoch`. So this was the first run of this path at
   production scale.

## Fix: server `fix/st-census-speed` (pushed, not merged)

- **e173a4ae0 "Read whole epoch usage windows in one block-ordered pass" (the
  core fix):**
  - Both readers, census and plain, run in one REPEATABLE READ, READ ONLY
    maintenance transaction. The census keeps its cutover check first.
  - **Probe:** `SELECT count(*) FROM (SELECT 1 FROM (<usage statement>) LIMIT 32769)`.
  - **At 32768 rows or fewer:** exactly the old statement and plan, unchanged.
  - **Above 32768:**
    - It sets `SET LOCAL enable_indexscan=off, enable_seqscan=off,
      max_parallel_workers_per_gather=0, plan_cache_mode=force_custom_plan`.
    - It then runs the byte-identical usage statement with no close-report
      reads, nils `Records` up front, and checks that more than 32768 rows came
      back.
    - The plan on main: archive hashed in memory (12.5M rows) → Bitmap Heap
      Scan ← bitmap on `closed_usage`. No CTE, no sort, no spill.
- **00ca2a97e "Check whole-window usage rows on workers, adding them in order"
  (optional):**
  - Decoding and per-row checks run on 4 workers in 1024-row batches.
  - Rows are still applied one at a time in statement order.
  - The first refusal cancels the statement, and a panic is re-raised on the
    caller.
- **Files:**
  - `model/st_provider_usage.go`;
  - comments in `model/st_provider_close_originals.go` and
    `taskworker/work/st_work.go`;
  - tests in `model/st_provider_usage_original_control_test.go` (a verbatim
    copy of the old read), `..._whole_window_test.go`,
    `..._whole_window_plan_test.go` and `..._whole_window_batch_test.go`.

### Why the results are the same

- Every statement reads the same single snapshot.
- Above the bound, the SQL text is the same one the census statement embeds;
  only the plan changes.
- More than 32768 rows means the old code would also have returned
  census = nil and window = nil. The window sentinel is NULL at 1025 or more
  credited rows.
- A row missing its close time cuts the gate to at most 2 rows, so that
  refusal always takes the old path.
- Per-row checks run in the same order with the same messages.
- **One known difference:** if a large window contains two or more different
  refusals, the error may name a different contract. The old statement has no
  ORDER BY, so its choice already depended on the plan. With a single refusal
  the message is identical.

### Tests

- **Failing-first:** `TestStEpochCensusWholeWindowReadsNoCloseReports` locks
  the close-report tables and runs an overflowing census. On the old code it
  fails after 75 s ("waited on close reports it cannot retain … context
  deadline exceeded"). With the fix it passes in about 14 s.
- **Reverting each part fails its test:**
  - the old dispatch fails the census test;
  - empty plan settings fail `TestStEpochUsageWholeWindowPlan` ("by Index
    Scan, not by bitmap", using main's planner costs);
  - a "first refusal found wins" variant fails
    `TestStEpochUsageRowsFirstRefusalInReadOrder`.
- **Equivalence against the verbatim old read,** for both readers. Each case
  compares usages, error text, census count, records and hash, window JSON,
  and the public output, plus a SQL oracle. Cases:
  - the 32768/32769 boundary;
  - a 1100-row overflow;
  - every disposition;
  - the empty-window sentinel;
  - archive rows inside and outside the window;
  - every refusal: duplicate, nonconserving, two networks, int64 overflow,
    missing close time live and archived, legacy debt;
  - first refusal with 1, 4 and 8 workers.
- **Wider:**
  - the whole-window suite is clean under `-race`;
  - 133 model tests pass;
  - 95 controller tests pass (StComputeEpochPayout, the v2 rows, build,
    activation and dry run, closed work).
- **Environment:** run against disposable release-gate containers
  (`local/release-gate-services.sh`), because the run-local launcher was down.

### Expected runtime

| Window | Old read | Core fix | With 00ca2a97e |
|---|---|---|---|
| Epoch 3 (about 404M rows) | about 3 h just to fill `usage_rows` | about 25–30 min | about 10–17 min |
| Epoch 4 (projected 0.7–0.9B rows) | over 6 h | about 45–65 min | about 20–40 min |

- **Probe:** about 5–10 s.
- **Database side:**
  - the bitmap phase reads about 2M index leaf pages;
  - the heap phase reads about 37M pages for epoch 3.
- **Client side:** decode and checks take about 3.5–4.5 µs per row on main
  (estimated), so the core fix is client-bound.
- **Fit:** all of this fits inside v2's 2 h step and v1's 3 h close. The
  horizon is held only for the read.

## Epoch 4 deadlines

- **Epoch 4 ends:** block 9448828, about **2026-11-06 18:25Z**.
- **Commit deadline:** block 9450028, about **2026-11-06 22:25Z**.
- **Basis:** a measured 12.0 s/block, ±1–2 min.

## Deployment plan

1. **Deploy a taskworker with this fix before about 11-06 18:00Z.** Both the v2
   epoch step and any v1 close run in taskworker.
2. **Choose the base carefully.** Build from the running taskworker image's
   source plus this branch, or from main once main's other taskworker changes
   are deployable.
   - The rotation branch adds migration 902 and needs "migrate 902, then every
     api, then the taskworker". A taskworker from main after that merge must
     wait for the api pass.
   - Coordinate with the play-webhook-monitoring-fix session before any api or
     taskworker deploy.
3. **During the epoch-4 read, watch:**
   - the log line `[st]usage window … reading it whole`, and the read's
     duration;
   - the xmin horizon age;
   - shared_buffers churn: bitmap scans skip the bulk-read ring, so about
     300–550 GB passes through 256 GB of buffers;
   - archive growth: the in-memory hash would spill past roughly 60M archive
     rows.

## Rejected mitigations

- **Raising MaxTime:** it cannot save the old read, which needs 3–6 h or more,
  and it lengthens the horizon hold.
- **Starting the census before the boundary:** not provably equivalent. Closes
  take `clock_timestamp()` inside transactions that commit later, so an early
  snapshot can miss rows dated before it. Retention also moves rows from live
  to archive between snapshots, which breaks the duplicate check and could
  double-count.

## Questions for the reviewer

1. **Plan forcing:** is `SET LOCAL enable_indexscan/enable_seqscan = off`
   acceptable as the plan-stability mechanism? The alternative is a rewritten
   statement that the planner reliably runs as a bitmap scan.
2. **00ca2a97e:** ship the parallel decode commit now, or the core fix alone?
   Its gain is unmeasured on main.
3. **Error choice:** is the possible difference in which refusal is named,
   with two or more refusals, acceptable?
4. **Epoch 3:** is any remediation wanted beyond the D-11 carry? Its providers
   get nothing from epoch 3 itself.
5. **Alerting:** PLAN.md calls for an alert at T-2h when the root is
   unconfirmed. I did not see one. My monitor snapshots at 19:46Z and 22:17Z
   bracket that time, and only the second showed anything: the settlement WARN
   after the window closed. Should a close still running at T-2h page?
6. **Pre-boundary work:** should part of the close (for example, the reliability
   reads) move before the boundary, where that is provably equivalent?

## Status

- **Merged to server main 2026-10-31:**
  - 6a523f17d: the core fix (e173a4ae0 on the branch);
  - 7720f0deb: parallel decode (00ca2a97e on the branch).

  Both are rebased onto f45365d84 with no change to the tested files. The model
  suite passed again on gate containers (832 s).
- **Deployed 2026-10-31:** shipped in the owner-approved cohort-rotation
  release:
  - migrate 902 at 06:37:40Z;
  - api 1086286270 at 06:45Z;
  - alt 1086288290 at 06:47Z;
  - **taskworker `2026.10.31-outerwerld-1086289460`** (server 7720f0deb) on g1
    and g2 at 06:49Z.

  The image binary's vcs.revision is 7720f0deb, unmodified, and it contains the
  whole-window read.
- **On 11-06:** watch the epoch-4 read (log line, duration, horizon age).
- **This document:** in sn main.

## Funding facts on chain (added 2026-10-31)

These are read from snow's Finney archive at finalized block 9405583 (about
18:15Z), and the DB mirror (`st_epoch`, `st_publish`, `pending_task`) agrees.
They correct the consequence above in two ways:
- **Epoch 3 is pending, not yet carry.** vault1 shows epoch 3 *Funded with no
  root*: 4,132.715621039 α, which covers about two weeks of emission, because
  epoch 2 was deferred into it. It becomes carry only when the queued
  `StFinalizePoke` runs `finalizeOperatorEpoch(3, 1)` at about **2026-11-01
  18:25Z**. That is expected and loses nothing.
- **Epoch 1's 3,482.257057872 α has been in operator 1's v1 carry since
  10-18.** No v1 root has ever been committed (`rootCommitments(e, 1)` is zero
  for epochs 0–4), and vault1's `totalPaid` is 0. After 11-01, `carry(1)` is
  expected to be 7,614.972678911 α. Escrow conservation holds.

**What recovery takes:**
- **Epoch 4's v2 root cannot take v1 money.** vault2 binds each root's total to
  its own `fundedThrough`.
- **An epoch-3 root would mean backdating,** which is not allowed.
- **The planned Phase C sweep is the route** (VERSION2-FINAL.md §§12.2 and 16).
  - Already deployed: vault1 and the v1 coordinator functions,
    `vault2.absorbStake`, and `vault2.assignSurplus`.
  - Missing:
    - an `STSettlement2` upgrade adding `assignSurplus`, through the owner
      Safe with the Ledger;
    - the server sweep path (`stLedgerV1SweepEpoch` is still a stub);
    - a reader exception for the sweep root;
    - if missed-root funding is allocated by source-epoch rows, the ledger
      "recovery component".
- **The sweep can run early.** No v1 entitlement was ever finalized, so
  nothing waits on claim expiry.

**Owner decisions:**
- the allocation rule: (a) roll into the next v2 epoch's earners, (b) allocate
  by source-epoch rows (the plan of record), or (c) v1 rules;
- the sweep timing;
- signatures for the upgrade.

**Data to preserve:** usage rows are retained in `st_provider_usage_archive`
for about 52 weeks. The 30-day `client_reliability` floor starts dropping epoch
1's rows around 11-08 and epoch 3's around 11-22, which matters only for
option (c).
