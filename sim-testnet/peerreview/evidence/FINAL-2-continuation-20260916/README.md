# Continuation and history adoption, 2026-09-16

The byte-preserved `receipts/` bundle records successful continuation capture,
zero-transaction import, and strict-history adoption before the failed managed
start. Its requests, terminal exits, before/after identity hashes and complete
capture-plan diff preserve exactly what was reused. It also records the build
that restored executable provenance after a documentation-only publication.

The adopted plan is
`0xb7fd2eb5030f73b424b3449d21302e6b3d17cd85142f4ca61b18aa1e0c2b5b17`.
Capture finished at 05:14:43 UTC; import at 05:39:02; history adoption at
05:41:45. All three commands exited 0. Import changed only the saved plan and
reported zero chain transactions. The exact history-bundle SHA-256 is
`e556044d5cf4b584856df3dc7c0a199a582ce14130f5818c09190ac54673eb53`.

The original bundle describes its publication state at capture time. It is
copied here without rewriting those historical records. Its SHA256SUMS hash is
`5784ea6375e40f58291f2eda6b2794ad7518a9a472444cd811cc5cdb90cecf0c`;
its result-seal hash is
`53ad0e52f86910b02bc051e9d8c455c6aa4bee491d9afc957a61eea25ac28c82`.
Verify its 107 sealed entries with `sha256sum -c SHA256SUMS` from `receipts/`.

These are preparation results, not campaign acceptance. The initial HTTP
stream stall remains unexplained. The later failed managed start is recorded
separately in [the failure projection](../FINAL-2-managed-start-failure-20260916/README.md).
Its actor effects must not be inferred from the import's zero-transaction
result. The captured end window and first native epoch are historical operands;
recovery chooses fresh values after the required corrections.
