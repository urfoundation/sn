# Approved campaign lineage recovery

Qualified source: `df9ba9d8077d686a8b91363f0b8a6dcc0057f41e`.

The provisional campaign stopped after adopting topology because historical
attempts were read with the current configuration and plan hashes. The source
change reads each signed ancestor against its exact archived approval, then
appends a distinct recovery under the current approval. The transaction journal,
approved plan and historical evidence remain unchanged. Production descendants,
active predecessors, unknown or mixed ancestry, and changed policy, contracts,
chain or custody remain hard errors. New approvals cannot inherit preparation
or an acceptance interval.

Historical definition and matrix hashes are owner-signed commitments, not
archived executable definition preimages. Failed historical windows and fault
state are validated against their original signed bytes and the unchanged
governed cadence. Current acceptance still uses the current executable
definition. Every warm ancestor-cache hit rechecks the exact provisional plan
and `provisional=true`, `final_acceptance=false` admission.

Terra medium ran the tests; Astra max implemented and independently reviewed
the repair. The six new regression roots passed normally in 4.892 seconds.
The broader 25-root source census passed normally in 15.088 seconds and under
race detection in 108.982 seconds. Commands used the data-volume wrapper:

```sh
./scripts/with-test-storage.sh go test ./sim-testnet \
  -run '^TestScenarioCampaignHistoricalLineage.*$' -count=1 -parallel=4 -timeout=10m
./scripts/with-test-storage.sh go test ./sim-testnet \
  -run "$SEL" -count=1 -parallel=4 -timeout=10m
./scripts/with-test-storage.sh go test -race ./sim-testnet \
  -run "$SEL" -count=1 -parallel=4 -timeout=10m
```

The exact broad selector is retained in `terra/final/broad-selector.txt` under
the evidence directory below. External overlays restored the old reader,
removed the warm admission guard, and imposed current geometry on historical
acceptance. All compiled and failed at their intended assertions; each retained
exit receipt is 1. The controls used separate overlay sources while the candidate
source stayed frozen.

Five-file source manifest SHA256, unchanged before/after all qualification:

`3d75904faf682322f8aaa8f9081b79dae235d2512b5307a8ac53fe8fb5a9a8e7`

Full evidence and reproduction overlays:
`/mnt/data/sn-testnet/qualification/campaign-lineage-20260921/`.
`REVIEW.md` records the actual old/current hash mismatch; `terra/final/` holds
logs, exit receipts, selectors, source census and before/after manifests.
Initial incomplete-fixture failures and superseded runs remain separate.

This qualification changed no live audit, supervisor, deployment or campaign
state. The separate fleet-lifecycle compatibility change is outside this source
commit and has its own qualification.
