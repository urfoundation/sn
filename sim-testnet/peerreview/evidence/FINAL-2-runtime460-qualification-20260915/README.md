# Runtime 460 affected qualification

The affected qualification completed on September 15, 2026. The effective
source is `7989fa78faae81b82138c79e43dded16aad449dc`. All **108 selected roots
pass normally and under race**, by composition of unchanged passing results
and the four corrected tests. This is affected qualification; the earlier
complete producer/aggregate coverage remains separately recorded.

| Package | Roots per mode | Accepted source composition |
| --- | ---: | --- |
| CRV4 | 13 | 12 from `27eecfdf`, one metadata comparison from `7989fa78` |
| Miner | 16 | 15 from `0127b7ee`, one source-commit assertion from `7989fa78` |
| Validator | 25 | All from `0127b7ee` |
| Simulator | 54 | 52 from `0127b7ee`, two fixture/census checks from `7989fa78` |

Root independently compared the union of raw passing root events with every
name in the affected selection, in both modes: no missing or extra roots.
The [positive receipts](positive/index/POSITIVE-COMPOSITION-INDEX.md) retain the original
compiler and test failures, replacements, compiled selections, actual exits,
and source/dependency/binary/selector checks.

The initial CRV4 compiler exposed test integer/byte type assumptions after
cache capacity became catalog-derived. A test-only correction enabled its
body. Four remaining test failures were fixed together: the metadata test
used the RPC client's incompatible SCALE decoder; the miner expected the
previous source commit; the simulator census retained an old test name; and
the historical artifact fixture assumed seven catalog entries. Only those
four roots reran in each mode. No production algorithm changed in those fixes.

The [causal composition](causal/COMPOSITION-INDEX.md) contains **13 expected
FAIL and 11 PASS outcomes**. Its original native control remains recorded as
body exit 1 / qualification exit 127 because the metadata comparison failed
unexpectedly. The corrected test ran once in the same native rollback source
and passed, supplying only that missing passing control. The other seven
native outcomes and eight complete variant receipts remain retained.

The [runtime artifact review and offline probe](../FINAL-2-runtime460-20260915/EVIDENCE.md)
are separate evidence. These tests do not establish live campaign acceptance,
production cadence, final reserve share or completed soak. Native deployment
and the required live campaign remain pending at this qualification checkpoint.
All new actual chain observations use the owned LAN node; no independent RPC
claim is made.

Each `positive/` and `causal/` index maps the portable subset to its original
capture. Their manifests preserve the raw artifacts; the bundle's `SHA256SUMS`
also covers this index. Earlier failures have not been relabeled as passes.
