# Receipt prefix fixture correction

This successor preserves all production source from candidate `22aca4ccf6ad8671baf2117dd06fc98e3969a2b6`. It corrects three test files after Terra's frozen normal capture found five failures. [Qualification is complete](receipt-prefix-qualification-20260928.md), including the uncached-read follow-up below; author execution was compile-only. The original failed captures and provenance limits remain in that receipt.

The four original-authority pending-loop tests now serve a complete 56-byte Subtensor `System.Account` row using the actual metadata-derived hotkey key. Its nonce equals the original prepared transaction's nonce. The fixture refuses any nonce read outside block 101, the exact authenticated complete-body boundary. Existing hard-error, cancellation, epoch-crossing and visible-wait assertions are unchanged.

The long chunk/restart/eviction test separately approves blocks 100–230 before the complete production config, source envelope and original intent are signed. Its late receipt at 230 is therefore within the independently signed read window. The default fixture still approves only through block 200. No approval is changed after signing, and no runtime gate is relaxed.

Compile-only passed with `GOWORK=off GOMAXPROCS=2 GOCACHE=/mnt/data/sn-testnet/gocache`, a physical capture `TMPDIR`, and `go test ./validator -run '^$' -count=1 -timeout=300s`. `gofmt` and `git diff --check` passed. No author test bodies ran.

Terra preserved the original failed capture at `/mnt/data/sn-testnet/qualification/receipt-prefix-20260928/terra-22aca4cc/validator-normal.jsonl` and reran only these five affected roots normally and with race detection:

```text
^TestProduction(AuthorityHistoryPending(WaitKeepsLoopAlive|WaitReconcilesNextEpoch|WaitPreservesHardFailures|ReceiptTimeoutKeepsObservation)|ReceiptChunkResumesDurableIntentAfterOutageAndEviction)$
```

Reuse completed original-candidate positives and their causal controls. Production bytes and the diagnostic callback interface are unchanged, so the separately authored bounded-output composition remains valid.

## Uncached-read follow-up

Terra's `093272b6` normal run passed four roots and found one further fixture assumption: after the first complete scan through block 101, the timeout case kept the same finalized head and injected its error into `chain_getBlock`, a read the new cache correctly skipped. The fixture now advances its independently authenticated canonical head to 102 and requires timeout, mixed-integrity and cancellation faults to reach that exact uncached body. It verifies that the two original bodies are read once, while every later fault remains visible without altering the original intent.

This follow-up changes only the pending-recovery fixture and this receipt. The other four corrected roots and all unchanged scanner/miner positives were reused. Terra's selector `^TestProductionAuthorityHistoryPendingReceiptTimeoutKeepsObservation$` passed normal and race in the frozen `terra-2fc-frozen` composition. The previous result remains at `/mnt/data/sn-testnet/qualification/receipt-prefix-20260928/terra-093272b6/normal.jsonl`. Author qualification remains compile-only; no production byte changed.
