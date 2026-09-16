# Journal validation-index qualification

The formatted positive source is `19c980b5297e77ff679206b36df49957016735dc`, derived from authored `5802bcba852b745601c6c63f1644f6fcbec2e6bc` on base `9a1af956ced89307f978f650609ea5838b910c9b`.

The exact 15-root simulator selector passed in both modes. Each mode has a fresh source-specific compiler binary, an exact compiled census, source/binary/selector fences, test2json terminal events, and a package pass event. The deterministic replay covers 44,048 synthetic rows (6,000 actions × 7 stages plus 2,048 retries); the selected tests assert at most four historical comparisons per input row and at most four after reopen.

| Lane | Compiler | Census | Body / outer | Terminal event map | Binary SHA-256 |
| --- | ---: | ---: | ---: | --- | --- |
| Positive normal | 0 | 0 | 0 / 0 | 15 PASS | `ef7327658b417c1dcd1c49b1b98a6dfcbfd564f947aba5f66932be7d90db7904` |
| Positive race | 0 | 0 | 0 / 0 | 15 PASS | `648f17ebfbf56ad35cc3bb4ed6913e00fad2d96182b8320d4c0ea73095593676` |
| Causal original-scan normal | 0 | 0 | 1 / 1 expected | 13 PASS, 2 expected FAIL | `cd3e1b422c9da04536fcf948435fc0325600921ce16d1e36d0af558dfc68cd2d` |

The causal source is `1ea89249eea55e758123c82f44c13847aff08f76`. It restores only the original all-entries comparison selection. Exactly `TestJournalValidationIndexBoundsAuthenticatedReplay` and `TestJournalValidationIndexBoundsAppendAfterReopen` failed; the 13 compatibility controls passed.

The separate keyed-table follow-up `752ca68c06808b3b0c852e2bbf644ce342b9fe1c` was already gofmt-clean. It did not rerun semantic bodies.
