# Successful continuation capture and lossless review

The closed `relay-capture-r3` invocation emitted successor plan
`0x17e49d00a7ce6aafac856e81a4ccf9eb37e4714ba7570a24cfa1c97a2d941f37`,
extending approved plan
`0x49ddbc495a51c7c089ed5838299d6d65fb40cfb9b35be3ee38cd4ca1de7aa876`.
Its body ran **09:00:03–09:15:28 UTC on September 16, 2026**. Body, outer,
and join exits are all 0. The original 11,302,363-byte output has SHA-256
`e40de369ee48a5bbc7c4b752295187cdc1f44e49117d2d324744f62b3ad90a48`.

This package covers that capture and its completed review. Import session
30705 (`relay-apply-r2`), subsequent startup, campaign results, and live state
are outside its scope. No successful import or acceptance epoch is claimed.

## Capture receipts

The [request](capture/REQUEST.json), [original command](capture/command.sh),
[input digests](capture/command.inputs.sha256), and [result](capture/result.status)
bind source `aeda6abbd2dc0abc92bb0f60975cf89b509e8017`, executable SHA-256
`8fc61a65cd0524413a7ba70c61bcdb15962fa87ad7ab347b653abb27f8913f0b`,
and release-lock SHA-256
`bf417189d4c0a62f8116606f84f9c5509b3afe2dab611429c9b781998b3198fc`.
The recorded before/after digests match for the executable, release lock,
and all six watched state files. This is the captured watch set, not a claim
that every runtime file was hashed. Original command files preserve local
provenance and are not instructions to rerun against a live deployment.

The [late head observation](capture/native-head/OBSERVATION.json) selected
END 8,025,298 from finalized native block 8,017,238 on the sole LAN authority
`192.168.1.162:9944`, runtime 460/1/1. There is no independent-RPC claim.
The emitted continuation anchors at native/EVM block 8,017,249. Its complete
required work remains **7,570 blocks**, with a fixed latest full-work start
of **8,017,728**; native import and startup retain their own current-boundary
checks.

## Complete review

The unchanged [original review](lossless-review/REVIEW.md) and
[nine-file manifest](lossless-review/SHA256SUMS) are preserved. The manifest's
[original seal](lossless-review/SEAL.sha256) is
`81879a0c23542ea26870ae3ea59548148b132f0eabb3a5e07edca8f4e6c40c39`.
All nine original entries were verified locally before packaging. Seven small
review payloads are included; the two large rendered plans are omitted with
their exact hashes and sizes in [OMISSIONS.json](OMISSIONS.json).

The entire [137-line diff](lossless-review/plan.pretty.diff) and
[27-path semantic comparison](lossless-review/semantic-differences.json)
are included, alongside the independent
[root comparison](capture/root-exact-comparison.json). Only continuation
observations, the appended predecessor, and the successor plan hash change.
END advances from 8,024,100 to 8,025,298; snapshots and capacity forecasts
advance; filesystem observations refresh; both operator root nonces become
107; and the signed-transaction census incorporates the four restored
finalized attempts. The inherited `generated_at` is unchanged.

The [original rendering receipt](lossless-review/render-receipt.json) binds
both exact input and rendered output hashes. It records preservation of every
non-whitespace token, including all 171,382 integer lexemes per plan and all
1,881 exceeding 2^53. Packaging copies this receipt and the review byte-for-byte;
it does not reserialize the plans or large integer fields.

The [unchanged invariants](lossless-review/unchanged-invariants.json) retain
all **4,733 actions**, dependencies, spending, custody, identities, release,
configuration, policy, repair history, and both completed renewals. Approved
limits remain 205 EVM / 225 total TAO and 37,250 alpha, with the latest
6,000-alpha repair tranche. All four signed ledger heads and intent prefixes,
182 retained requests, zero relay debits, zero historical relay liability,
and 1,024 new slots remain unchanged. The subject forecast is 766 of 1,024.

The [transaction census proof](lossless-review/transaction-census-comparison.json)
reproduces both plan digests: 2,518 unique transactions, or 2,514 after removing
exactly the four restored finalized attempts, with no other additions or
removals. Their maximum signed envelope is 25,635,775,234,311,880 wei and actual
finalized fees are 8,264,277,772,552,846 wei. The prior
[signature recovery package](../FINAL-2-signature-recovery-20260916/README.md)
contains the complete both-operator census, canonical receipt links, and
create-only restoration receipts. These amounts do not increase spending
authority or replace native budget checks.

The [fleet capacity note](lossless-review/fleet-capacity-note.md) distinguishes
the relay forecast through settlement epoch 433 from the unchanged fleet
leases 393–424 and actual M2/M3 acceptance guards. Forecast 433 alone neither
changes a lease nor authorizes another renewal. Actual phase baselines and
terminal tails remain subject to the existing native checks.

## Integrity and omissions

[SOURCE-FILES.tsv](SOURCE-FILES.tsv) maps every copied file to its closed
original, exact digest, and byte count. [VERIFICATION.json](VERIFICATION.json)
records offline comparisons. Four large files, totaling 54,762,095 bytes,
remain local: both exact plans and their lossless renderings. Raw signed
transactions, calldata, private database/configuration payloads, credentials,
and active import files are excluded. No test, build, RPC, process action,
or active-state access was performed during packaging.

From this directory, verify the portable payload and preserved review seal:

```sh
sha256sum -c SHA256SUMS
sha256sum -c PACKAGE-SEAL.sha256
(cd lossless-review && sha256sum -c INCLUDED-REVIEW.sha256 && sha256sum -c SEAL.sha256)
```

The top-level manifest covers every packaged file except itself and its seal.
`INCLUDED-REVIEW.sha256` selects the seven included payloads from the unchanged
original manifest. Checking all nine original entries additionally requires
the two omitted rendered plans; the full original manifest is retained without
removing their entries or implying that those payloads are packaged.
