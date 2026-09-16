# Runtime 460 positive qualification portable receipts

This staging directory is a portable copy of raw positive qualification receipts for root's evidence bundle.

It includes:
- initial formatted 0127 positive compiler/body receipts, including preserved failures;
- isolated 27e CRV4 type-repair receipts;
- final 7989 four-root retry receipts;
- the accepted 216-row root-to-receipt map and event-membership validation;
- the accepted offline exact-Wasm probe receipt.

Compiled test binaries and private tmp/gotmp trees are intentionally omitted. Each capture retains compiler binary SHA-256 files, command scripts, source/selector/dependency fences, test2json streams, stdout/stderr, exits, and launcher joins. No test is run by this packaging step.
The original invalid RESULT.json and its pre-correction seals are preserved in history/invalid-result-r1/. RESULT.json was regenerated with jq using the exact ACCEPTED-ROOT-MAP.tsv SHA-256.
