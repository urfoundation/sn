# Operator signed-attempt census and original-byte recovery candidates

The 08:51:48 UTC read-only census contains **230 signed attempts: 226 already present in the approved retained census and exactly four missing**. All four are the canonical successful operator transactions from the partial startup. This receipt describes the pre-restoration snapshot. Root separately owns `restore/`; it is excluded from this immutable census seal.

| Role | Missing nonces | Signed attempts | Unique nonces |
| --- | --- | --- | --- |
| operator-1-root | 106 | 109 | 107 (0–106) |
| operator-2-root | 104–106 | 107 | 107 (0–106) |
| operator-1-deposit | none | 7 | 7 (3–9) |
| operator-2-deposit | none | 7 | 7 (1–7) |

`READ-ONLY-CENSUS.sql` reads every intent and attempt in both retained operator databases using repeatable-read, read-only transactions; it has no mutable-status or time filter. The 232 rows include two old unsigned prepared nonce-0 intents, one per operator. These have no signed bytes to restore and remain untouched. Both alternative signed attempts at operator-1-root nonces 0 and 37, the old broadcast/replaced attempts, and all six cancellations are included. Their original bytes were already retained. Cancellation validation uses the decoded self-transfer recipient and empty input; it does not mistake the original coordinator intent recipient for the signed cancellation recipient.

`VALIDATION-METHOD.json` records the local decoder and checks. `validated-attempts.json` binds all 230 original DB attempts to chain 945, the approved genesis/deployment, recovered role signer, nonce, destination, value, gas envelope and original byte digest. `missing-attempts.json` and `RESTORE-CANDIDATES.tsv` identify the four immutable private candidates. Each is 178 bytes, mode 0600, with one link. No original signed bytes or calldata are included in the safe projections.

`CANONICAL-RECEIPT-BINDINGS.json` independently binds the four candidates to the closed partial-start reconciliation evidence: exact transaction identity and canonical status-1 receipts in blocks 8016488, 8016489 and 8016491, below finalized native/EVM height 8016641. Their maximum signed gas-plus-value envelope totals **25,635,775,234,311,880 wei** (0.025635775234311880 EVM TAO); actual finalized fees total **8,264,277,772,552,846 wei** (0.008264277772552846 EVM TAO). These numbers do not replace the native complete-census budget checks.

The fresh LAN-only observation at 08:54:27 UTC compares all 13 approved roles at finalized block 8017216, latest block 8017218 and pending. Only operator-1-root advanced 106→107 and operator-2-root advanced 104→107; each has finalized/latest/pending nonce 107. The other 11 roles are unchanged. No retained signed DB attempt has a nonce at or above its observed pending nonce. See `ROLE-NONCE-COMPARISON.json` and the exact requests/responses.

The existing continuation collector (`sim-testnet/evidence_relay_continuation_capture.go:73`) reads claim queues, both approved renewal evidence sets and every canonical `transactions/<hash-without-0x>.rlp`, validating names and decoded hashes. Its existing nonce/exposure guards remain active (`fleet_renewal_budget.go:148,289`). The original signatures are stored durably before broadcast (`server/controller/st_controller.go:1021–1027`; model `st_transaction_attempt.raw_transaction`). Restoring these exact missing files therefore supplies the existing collector without new signatures, fabricated journal rows or a source rebuild. DB receipt statuses remain untouched for the production reconciler.

The private 230-transaction JSON export is complete for these two operator DBs. A future fleet renewal must still provide the complete external evidence union required by that command; this DB-only export does not claim to be the entire campaign union. No chain transaction, DB mutation, active-state write, test or project build was performed by this census owner. Root's separately captured restoration must be assessed using its own receipt.

`CENSUS-MANIFEST.sha256` seals only the listed safe census artifacts. `PRIVATE-ARTIFACTS.sha256` identifies immutable private originals by digest without publishing their contents. Original canonical receipt/request files are hash-referenced in the bindings rather than copied. The manifest deliberately excludes `restore/`, private payloads and its own seal.
