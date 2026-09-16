# Failed managed-readiness generation: preserved-state census

The strict resume that ran 2026-09-16 09:41:17–10:20:25 UTC and joined with exit 1 submitted **14 new operator transactions**, all canonical status 1. It produced **no new signed validator steering intent or preparation for N1488**. Deployment-journal stability is not used to infer an absence of chain writes. This census describes the stopped generation before root's separately recorded restoration under `native-recovery-20260916-r4/signature-restoration`.

The complete read-only operator DB snapshot now has **244 signed attempts**, compared with 230 previously: operator 1 gained eight original execution attempts at nonces 107–114; operator 2 gained six at 107–112. No old intent/attempt row or original signed byte digest was removed or changed. All canceled, replaced, failed and nonterminal siblings remain included because the census has no status or time filter. The two old unsigned prepared intents remain untouched. The only old DB lifecycle changes are the production reconciler's finalization of three previously pending records from the prior four-transaction partial startup; all four are now terminal finalized in the DB.

| Operator | Nonce | Method and arguments | Canonical block |
| --- | ---: | --- | ---: |
| 1 | 107 | `deferMissedEmission(402,1)` | 8017620 |
| 1 | 108 | `deferMissedEmission(406,1)` | 8017623 |
| 1 | 109 | `deferMissedEmission(403,1)` | 8017623 |
| 1 | 110 | `deferMissedEmission(317,1)` | 8017623 |
| 1 | 111 | `finalizeOperatorEpoch(402,1)` | 8017626 |
| 1 | 112 | `finalizeOperatorEpoch(403,1)` | 8017629 |
| 1 | 113 | `finalizeOperatorEpoch(317,1)` | 8017629 |
| 1 | 114 | `finalizeOperatorEpoch(406,1)` | 8017629 |
| 2 | 107 | `deferMissedEmission(403,2)` | 8017621 |
| 2 | 108 | `deferMissedEmission(406,2)` | 8017621 |
| 2 | 109 | `finalizeOperatorEpoch(317,2)` | 8017624 |
| 2 | 110 | `finalizeOperatorEpoch(402,2)` | 8017624 |
| 2 | 111 | `finalizeOperatorEpoch(403,2)` | 8017627 |
| 2 | 112 | `finalizeOperatorEpoch(406,2)` | 8017630 |

`NEW-SIGNED-ATTEMPTS.json`, `NEW-TRANSACTION-PROJECTION.json` and `DECODED-METHODS-EVENTS.json` contain every full transaction hash and exact durable intent identity. The six defer calls emit `EmissionDeferred`; the eight finalizations emit `RootMissed(...,0)` and `OperatorEpochFinalized(...,false)`. No payout root is created by these calls. Frozen contract sources and digests are identified in `SOURCE-REFERENCES.json`.

LAN-only receipt/canonical observations prove all 14 inclusions below native/EVM finalized block **8017664**. Both finalized/latest/pending nonce triples agree: operator-1-root is **115**, operator-2-root is **113**; the other 11 approved roles are unchanged. The new maximum signed gas-plus-value envelope totals **90,515,040,996,642,416 wei** (0.090515040996642416 EVM TAO); actual finalized fees total **29,033,073,172,513,564 wei** (0.029033073172513564 EVM TAO). These are existing submitted liabilities, not proposed new spending or a change to approved caps.

The original **2,272 active RLP files**, including the earlier four restored attempts, exactly match the previous post-restoration manifest. The unchanged renewal signatures and complete 1,000-queue claim census reproduce the current approval's complete **2,518-transaction** digest `sha256:0ee805b329c6ae9799e8c9180a8c5dc5df7697a216a3df6ffd7bc9ce9fc66d15`. All 14 new operator attempts are absent from that retained union. Adding only their exact original bytes produces **2,532** transactions and digest `sha256:f07fe41eb13cd653c0cd9bba83012a7bcf9084d47819d0b9ccfd12d12c6d5773`. See `COMPLETE-UNION-DELTA.json`.

All 1,000 claim queues were rewritten and currently contain 378,000 entries. Their only four signed entries are the previously retained transactions. Of 283,272 entries updated since startup, none has a transaction hash, signed bytes or nonzero submission-attempt count. No complete queue-byte baseline exists for this generation, so the claim conclusion is based on the full current durable record census and timestamps, not an invented byte-for-byte comparison. Queue counts/hash receipts preserve this limit.

The exact N1488 bundle remains pinned by `sha256:f2a7e1a24fc02cb9fbc3ee85af757b5795982ec42731f2b9fd34564485c479c9`. Validator 1 still has no steering-intents file (approved count 0). Validator 2's entire 23,486-byte file remains exactly `sha256:0de9867f8c0b699954e9ef25c66759b0390214ddaf8313d9ca7f42df63e27727`, containing the same three applied intents ending at N1405. Neither coordinator-state directory has a file modified since startup. Original prepared/completed setup hashes match the bundle. Retained validator ledger database storage and session credentials were reopened/refreshed; these storage changes do not establish a new decision, and source capacity must be re-observed by the next native continuation. No historical validator replay was rerun for this census.

After the complete delta was established, root explicitly authorized private original-byte candidates. `RESTORATION-CANDIDATES.json` and `CANDIDATES.sha256` bind all 14 immutable files to their original DB rows, recovered signer, chain 945, nonce, coordinator recipient, zero value, gas/fee envelope and canonical receipt. Each candidate is 178 bytes, mode 0600, nlink 1. Six watched state files were unchanged across export. No active-state write, DB/journal mutation, transaction broadcast, helper launch, project build or test was performed by this lane. Root alone owns exclusive-create restoration; originals and candidates remain preserved.

Recovery must retain the saved plan, original identities, completed repairs/funding, both renewals, all validator prefixes, all 244 DB attempts and their production reconciliation. A fresh continuation must include the complete 2,532-signature union after root restoration. The old history request has acquired no N1488 decision; a later recovery still needs an exact freshly captured history request under its adopted plan and viable first native epoch. The window recommendation is separate from this sealed census.

`operator-*.delta.json` preserve the initial mechanical projections, including the added/omitted `source_operator` annotation; `operator-*.semantic-delta.json` exclude that annotation and are authoritative for DB field changes. Safe RPC projections omit transaction calldata/signature fields and native consensus digest; their receipts retain exact original response SHA256/length. Private originals and credential-related metadata are hash-referenced separately, not included in the safe payload manifest. The manifest is an explicit immutable membership list and excludes root's separate restoration receipts.
