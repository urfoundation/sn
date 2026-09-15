# Relay continuation correction and adoption

This public bundle separates three closed relay-continuation captures. It
contains only small terminal receipts, safe projections, and input hashes. It
does not contain a private relay plan, raw command or request, full capture
stdout, signed bytes, or secrets.

The first final capture (`failed-r1`) requested an 8,500-block span and was
refused by the unchanged source lifetime/storage capacity check. Its body and
outer exits are both 1, while its state, binary, and lock comparisons are all
0; no state or chain transaction changed. The error is preserved as a capacity
outcome, not reclassified as success.

The corrected final capture (`capture-r2`) used an 8,060-block span ending at
8,021,242. It retains all 7,570 required work blocks, the same 1,024-slot
ceiling, and identical budget operands. The source capacity calculation permits
at most 8,065 blocks and records zero relay debits. Its body and outer exits
are 0, and its state, binary, and lock comparisons are all 0. The completed
relay plan is
`0xe128f2988512285a6270f45031f4a8af16e04009ceca385259869af02c50be30`.

The full `capture-r2` stdout is deliberately not copied because it carries the
private plan. Its exact source SHA-256 is
`e390a6152b0743593d7972d2b5a8569259fcf93e802919c0e33edb59c9ebf055`.
`RELAY-CAPTURE-PROJECTION.json` contains only the plan relationship, end block,
required-work count, slot count, and epochs needed to review this correction.

The actual adoption (`apply-r1`) joined with body and outer exit 0. Its body
finished at `2026-09-15T20:04:13Z`; `APPLY-CLI-STDOUT.json` records the adopted
plan, end block, and `chain_transactions: 0`. The state comparison is 1 as
expected for adoption, while binary and lock comparisons are 0.

`INPUT-HASHES.tsv` records only command, request, and stdout hashes, retaining
the distinction between the failed input, corrected capture input, and adopted
input. `BINARY-LOCK-HASHES.tsv` retains their before/after identities without
source paths. The three receipt directories preserve their respective exits,
times, and comparison statuses.

No new RPC request, test, build, native command, or verifier ran to create this
bundle. Verify its files with `sha256sum --check --strict SHA256SUMS` from this
directory.
