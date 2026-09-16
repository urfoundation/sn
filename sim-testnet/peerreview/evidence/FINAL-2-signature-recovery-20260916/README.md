# Original signature recovery after interrupted startup

The closed continuation capture failed because the simulator's retained
transaction census omitted four transactions already signed by the operator
workers. A complete read-only database census found exactly those four gaps.
Root restored their original bytes to the existing transaction store without
resending a transaction, changing a database status, or rewriting the journal.

This package records that recovery only. It does not establish a successful
continuation, startup, campaign, or acceptance epoch. The later continuation
capture and all active state are excluded.

## Closed failure

[Capture request](failed-capture/REQUEST.json) identifies SN source
`aeda6abbd2dc0abc92bb0f60975cf89b509e8017`, executable SHA-256
`8fc61a65cd0524413a7ba70c61bcdb15962fa87ad7ab347b653abb27f8913f0b`,
release-lock SHA-256
`bf417189d4c0a62f8116606f84f9c5509b3afe2dab611429c9b781998b3198fc`,
and source plan
`0x49ddbc495a51c7c089ed5838299d6d65fb40cfb9b35be3ee38cd4ca1de7aa876`.
The physical server revision was
`6752a8df246c0ee7e1c5a38cbd26b1e849b702ca`. Root identified this invocation
as session 52913; its recorded outer PID is in
[outer.pid](failed-capture/outer.pid).

The body ran from 08:31:32 to 08:46:41 UTC on September 16, 2026 and exited 1.
Its [exact stderr](failed-capture/stderr) ends with:

> renewal gas accounting is incomplete: role operator-1-root nonce 106 has no retained signed transaction

[stdout](failed-capture/stdout.json) is empty: no continuation plan was emitted.
The [closed result](failed-capture/result.status) records unchanged hashes for
all six watched state files, the executable, and the release lock. These are
the recorded watch set, not a claim that every runtime file was hashed.

## Complete census and canonical transactions

The [original census](census/README.md) and its unchanged 22-file manifest are
included byte-for-byte. Its manifest SHA-256 is
`4301631acb4268862a19273e35a8aadf442f1748b06e9fed0940737769737700`.
Both operator queries completed at 08:51:48 UTC using repeatable-read,
read-only transactions with no status or time filter.

| Source | Signed attempts | Unique nonces | Missing from retained evidence |
| --- | ---: | ---: | --- |
| Operator 1 root | 109 | 107 | 106 |
| Operator 2 root | 107 | 107 | 104–106 |
| Operator 1 deposit | 7 | 7 | None |
| Operator 2 deposit | 7 | 7 | None |

All 230 signed attempts were decoded and checked; 226 already matched retained
bytes exactly. This includes both alternatives at operator 1 root nonces 0
and 37 and all six cancellation attempts. Cancellations were checked as signed
self-transfers, separately from their original intent recipients. Two old
unsigned prepared intents have no signed bytes and were left unchanged.

The [13-role nonce comparison](census/ROLE-NONCE-COMPARISON.json), observed
at 08:54:27 UTC using only `192.168.1.162:9944`, found finalized/latest/pending
nonce 107 for both root signers. Only those two roles advanced; the other
11 remained unchanged. No signed database attempt was at or above its
observed pending nonce. There is no independent-RPC claim.

[Canonical receipt bindings](census/CANONICAL-RECEIPT-BINDINGS.json) identify
the four exact transaction hashes and original-byte digests. The earlier
[startup reconciliation](../FINAL-2-startup-transactions-20260916/README.md)
contains their canonical status-1 receipts in blocks 8,016,488–8,016,491,
below finalized native and EVM height 8,016,641. [Canonical links](CANONICAL-LINKS.json)
retain the original paths, hashes, sizes, and repository-relative links. All
four linked files were checked against both the closed originals and the
existing repository copies; their payloads are not duplicated here.

The four maximum gas-plus-value envelopes total **25,635,775,234,311,880 wei**;
their actual finalized fees total **8,264,277,772,552,846 wei**. These are
retained liabilities and incurred fees, not new spending authority or a
replacement for the continuation's native budget checks.

The preserved [validation method](census/VALIDATION-METHOD.json) records
server review checkout `006e71b997db503604c4ef6bb0c2683dc0d984cd` and the
exact local decoder. That review revision is distinct from the physical
server revision used by the failed invocation above.

## Completed restoration

Root's [request](restore/REQUEST.json), [original command](restore/command.sh),
and [result](restore/result.status) record a create-only restoration that
started at 08:57:54 and completed at **08:57:55 UTC**, exit 0. It added four
original 178-byte files, 712 bytes total, with mode 0600 and one link each.
The command checked source digests and canonical names, refused existing
destinations, used exclusive creation with file synchronization, compared
each restored file to its original, and synchronized the store.

All **2,268 original RLP files** remain unchanged; the resulting census has
2,272 files. [Store additions](STORE-ADDITIONS.sha256) exactly match the four
[reviewed candidate digests](restore/candidates.sha256), and
[store removals](STORE-REMOVALS.txt) is empty. The six watched state hashes
are unchanged, and the failure's poststate matches the restoration's prestate.
No chain transaction, journal mutation, database mutation, new signature,
new spending allowance, or renewal occurred during restoration.

The existing continuation collector reads canonical hash-named RLPs alongside
claim queues and retained renewal evidence. Capture and exact import retain
their normal hash, chain, signer, nonce, liability, and source checks. No code
change or executable rebuild was needed for this recovery. A future fleet
renewal still needs its complete external evidence union; the private
230-transaction database export alone is not that union.

## Package integrity and omissions

[SOURCE-FILES.tsv](SOURCE-FILES.tsv) maps every copied file to its closed
original, SHA-256, and size. [VERIFICATION.json](VERIFICATION.json) records
the offline comparisons. Original command files are evidence, not portable
instructions to rerun against a live deployment; their absolute paths retain
local provenance.

[OMISSIONS.json](OMISSIONS.json) records the excluded large source plan by
hash and size. Raw signatures, transaction calldata, private database exports,
credentials, configuration payloads, executable bytes, active state, temporary
directories, and later captures are excluded. The census's
[private artifact manifest](census/PRIVATE-ARTIFACTS.sha256) publishes only
digests and names of originals that remain private. Those payloads are not
required to verify this package's copied-file integrity and cannot be
reconstructed from it.

From this directory, verify the portable package and preserved census:

```sh
sha256sum -c SHA256SUMS
sha256sum -c PACKAGE-SEAL.sha256
(cd census && sha256sum -c CENSUS-MANIFEST.sha256 && sha256sum -c CENSUS-SEAL.sha256)
```

The top-level manifest covers every packaged file except itself and its seal.
Packaging performed no test, build, RPC, active-state write, or process action.
