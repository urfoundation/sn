# Runtime 460 causal composition index

This staging receipt preserves every original causal body. It combines only the
single test-only native replacement authorized after the original native
control failed unexpectedly. It does not replace the original capture or
authorize a release build, live operation, race rerun, or full gate.

| cohort/package receipt | source | raw body / qualification | roots retained or supplied |
| --- | --- | --- | --- |
| current artifact / miner | `d216a9311e64df7c6e23d5eef45de353d7294b8d` | `1 / 0` | 1 FAIL, 1 PASS |
| current artifact / validator | `d216a9311e64df7c6e23d5eef45de353d7294b8d` | `1 / 0` | 1 FAIL, 1 PASS |
| current artifact / sim-testnet | `d216a9311e64df7c6e23d5eef45de353d7294b8d` | `1 / 0` | 1 FAIL, 1 PASS |
| native old source (original) | `62ff658759606c15d979d36ffc8f415f463454b6` | `1 / 127` | raw 5 FAIL, 3 PASS; retain 4 expected FAIL and 3 unaffected PASS only |
| native metadata-constant replacement | `b2ea4f83a3931780137bccb612dbe78a71dbd864` | `0 / 0` | supplies only `TestSourceCommitmentRuntime460MetadataChangesOnlySpecConstant` PASS |
| original configuration / sim-testnet | `4c25223b3c9a28f84cc6a03b98200da21378e684` | `1 / 0` | 1 FAIL, 1 PASS |
| former current archive / sim-testnet | `58a80118679f561fd78ab05cbddfb2a2c7a94152` | `1 / 0` | 2 FAIL, 1 PASS |
| catalog predecessor / CRV4 type repair | `ec2a30367c2b66245319ef36365a07d6984a1702` | `1 / 0` | 1 FAIL, 0 PASS |
| catalog predecessor / validator | `d09ad52c76e064512f7bc428c1c20818a1293bbc` | `1 / 0` | 1 FAIL, 1 PASS |
| catalog predecessor / sim-testnet | `d09ad52c76e064512f7bc428c1c20818a1293bbc` | `1 / 0` | 1 FAIL, 1 PASS |

All eight original qualifying receipts have exact compiled-root censuses,
expected event membership, source/dependency/binary/selector fences, and
qualification exit `0`. The original native capture has the same source,
dependency, binary, selector, and compiled-root fences, but remains exactly
`qualification=127`; it is not relabeled. The one-root replacement is based on
the native rollback source and differs from `62ff...` only in
`crv4/source_commitment_runtime460_test.go` (SHA256
`ebe20b2004d9d989cfa8504fd7fce13c2a40cfa0c9edbfac85a682f8ce2cfb2f`).

The composed causal result is **13 expected FAIL roots and 11 PASS roots**:
current artifact 3/3, native 4/4, original configuration 1/1, former archive
2/1, and catalog predecessor 3/2. The raw original native mismatch remains in
`raw/native_encoding_stake_and_capacity/crv4/` and the narrowed replacement is
in `raw/native_encoding_stake_and_capacity/metadata-constant-replacement/`.

`RECEIPT-LOCATORS.tsv` maps every staged subset to its immutable capture;
`RAW-SHA256SUMS` covers the index inputs and staged raw files.
