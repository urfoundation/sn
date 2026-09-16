# Native continuation and history receipts

This is a local, reviewable preparation bundle. It is not published and does not claim final acceptance.

| Label | Value |
| --- | --- |
| LAN `independent_rpc` | `false` |
| Local preparation | `true` |
| Import transactions | `0` |
| Final acceptance | `false` |

The bundle preserves the closed relay capture, zero-transaction import, and strict-history adoption receipts: each operation's request, terminal exits, state/binary/lock before-and-after hashes, and selected raw evidence are included. `history-adoption/history.bundle.json` is the exact captured strict history bundle. `capture-r2/plan.raw.diff` and `capture-r2/REVIEW.json` retain the complete reviewed capture delta.

The source capture plan stdout is intentionally not duplicated because it is large. `references/SOURCE-CAPTURE-PLAN-STDOUT.json` binds its exact local path, size, and SHA-256; `import-r2/REQUEST.json` independently binds the same captured plan hash. The predecessor plan and token-layout inputs are likewise not copied; the complete raw diff and review are the public review artifacts.

`doc-restamp-build/` records the matching doc-revision CLI build identity and closed build, outer, and launcher exits. `socket-monitors/` contains the two closed monitor RESULT files and their source seal material. Source monitor SHA256SUMS files remain references to their complete local captures, which are not duplicated here.

The original HTTP-stream failure remains separately documented through the committed bundle listed in `references/COMMITTED-PROVENANCE.txt`. A successful retry does not establish the original failure's cause.

No executable, private configuration, `.env` file, source code, compiled Go file, token-layout input, test result, new RPC observation, or chain action is included. Verify copied bytes with `sha256sum -c SHA256SUMS` from this directory.
