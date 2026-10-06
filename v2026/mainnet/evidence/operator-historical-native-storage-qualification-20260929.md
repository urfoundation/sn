# MG-03 historical native StorageProof qualification

The exact qualified source is server `80c0e1b7d9fb48ee6f929bca8158ac925b114fe0`,
tree `e78d5078d1b5d5cc39c6167c8e94639bd4f97ea0`. It was fast-forwarded onto
`codex/mainnet-server-hardening-20260927`, with a successful non-force push and
independently verified clean local/remote head. Its integrated base is
`1bccc3cd7138faefddaa3ab4df15c5cc77e1bd4a`, tree
`1fd1f1155abfcfa600bd1891ef7bc56081f6c346`. The
[API contract](https://github.com/urnetwork/server/blob/80c0e1b7d9fb48ee6f929bca8158ac925b114fe0/strecovery/HISTORICAL-NATIVE-STORAGE.md)
adds one bounded offline historical raw-read verifier, its private witness loader
and deterministic fixtures. Exactly three files are added; all preexisting
production, test and Go module files remain byte-identical to the composed base.

`VerifyReceiptHistoricalNativeState` freshly invokes `VerifyReceiptFeeContexts`
for every request. That replays archived transaction/receipt evidence, native
ancestry and GRANDPA certificates before deriving exact Frontier mappings. One
committed receipt EVM block must select one unique native child. The witness
binds the exact collection/checkpoint/proof, EVM and mapped native identities,
explicit root role and selected native state identity. It accepts no root or
prior reconciliation from the caller.

`parent_execution` uses only a retained, linked native parent's identity/root;
an unseen checkpoint parent is refused. `child_post_state` uses the mapped
child's own root. A unique checkpoint child can prove its raw state even when
its parent is unavailable: the nested fee context remains
`native_parent_unavailable` with incomplete coverage. The implementation does
not invent parent execution evidence from that child. Equal trie roots do not
erase block/role identity; changing only the role or selected identity fails,
while changing both consistently can describe a valid different raw fact.

Missing or ambiguous mappings, receipt-free EVM ancestors, block/proof/role
substitution, corrupted resealed evidence and incomplete storage proofs fail.
Later certified descendants supply neither post-boundary historical mappings
nor a replacement state root. The existing bounded raw trie reader checks the
selected root, keys and values. Original histories and null actual fees survive;
returned raw values and derived contexts are owned, and errors/cancellation
return no partial report.

Independent static review identified the initial unnecessary refusal of a valid
checkpoint child when its parent was missing. The revised source preserves that
provable child fact while continuing to reject parent fallback. The reviewer
also requested the equal-root identity fixture. Frozen production/test hashes
match the reviewed snapshot; no unresolved source-level blocker was reported.
This is static review, not a passing behavioral test or an approval of authority.

Sol qualified all 20 new roots and 30 explicit adjacent roots in normal/race
modes: 16 raw/boundary-storage, eight fee-context and six finality/capture roots.
All four streams contain every selected root, package PASS and exit zero, with
no missing, failed, skipped or extra roots. All 21 isolated causal patches
compiled and reached the exact named assertion, root/package FAIL and exit one
in both modes: 42/42 discriminating executions. Author compilation, vet and every
mutant compilation passed without executing a behavioral body.
The raw SDK oracle remains the exact independently qualified 18-vector file,
SHA-256 `b875b9eb1aa497233f68bb4cae02b4331320fcaeeaaefbefe867658f2aebaecd`.
The source fence freshly queries 12 logical rows across 11 clean physical Git
roots and retains the exact isolated Go graph. Sol's post-test roots stayed clean
and its module graph remained byte-identical. Astra independently checked all
188 receipt-manifest entries, the four positive streams, all 42 causal outcomes
and actual mutant source bytes, then rechecked the 151-file author manifest,
physical source graph, oracle and current resolved Go modules before integration.
This is 50 selected roots per mode; it does not establish whole-package or
production-service coverage.

| Retained qualification artifact | SHA-256 |
| --- | --- |
| [Sol result](/mnt/data/sn-testnet/qualification/native-historical-storage-sol-20260929/frozen-80c0/SOL-RESULT.md) | `d38b0b8b5794e967576c9ebd7780e0ab504b755b1a88746cdf24c1997eb37d39` |
| [188-file qualification manifest](/mnt/data/sn-testnet/qualification/native-historical-storage-sol-20260929/frozen-80c0/SHA256SUMS) | `b1f58d9c7dc3b180691c7163e80d9d7bfeab19e01f44b132987a57eb535628d4` |
| [Author handoff](/home/by/urnetwork/temp/server-native-historical-storage-20260929/evidence/HANDOFF.md) | `8b1a687c41b9a6a34805869a5edb2cfaae86a4e74659dcf193eef97dbff016cd` |
| [151-file author manifest](/home/by/urnetwork/temp/server-native-historical-storage-20260929/evidence/SHA256SUMS) | `b259e7d0b245a16da9b897c1f81381c5c9d21cddbab1dbc7da10311f8e4addb2` |
| [Independent static review](/mnt/data/sn-testnet/qualification/mg03-historical-native-state-design-review-20260929/REVIEW.md) | `febdd43eef223fa327a9c385d29f5d8bf0f387d538fec5ed1670a1cb28cb926f` |
| [Five-file review manifest](/mnt/data/sn-testnet/qualification/mg03-historical-native-state-design-review-20260929/SHA256SUMS) | `7419f986441c8b877cd81e59c9edf3f13e909fb2bf303fd7fe913cd49586e6d3` |

The base's separate composed smoke passes 30 of 138 available roots in each
normal/race mode, including four CLI roots, as recorded in the
[storage/fee-context integration receipt](operator-native-fee-context-qualification-20260929.md).
That receipt does not qualify this new joining layer. Earlier StorageProof b0ca
and 4ffe anomalies remain preserved in the
[boundary-verifier receipt](operator-native-storage-proof-qualification-20260929.md).

Only selected historical context and raw storage facts become true relative to
the supplied, independently unapproved checkpoint. Genesis/checkpoint admission,
runtime source/decoding, payer/native extrinsic binding, live finality, fee
attribution, owner window, global custody, fee exposure and spending remain false.
The nested broad native-read and fee-context completeness claims are not promoted
by selected raw reads. Actual withdrawal, refund, gas-debit and gas-fee amounts
remain null. No collector, CLI, runtime decoder, signer or transaction is added.

Authenticated runtime/source/metadata interpretation, native placement/payer
evidence and runtime-qualified debit/refund attribution remain separate work.
Live identity/checkpoint admission, service adoption, release composition and
enforced custody remain open MG-03/PF-03 gates. The separate SDK race coverage
stays 543/618 package-backed roots with 75 pending; MG08 adjacent coverage stays
150/270 in both modes with 120 unrun. No full-suite or mainnet activation claim
follows from this scoped source qualification.
