# Validator canonical tail-boundary qualification, October 1

Frozen implementation `b8ffe82d` extends the read-only unsealed inventory to
authenticate every distinct EVM boundary referenced by signed records after
each committed cut. It uses the canonical production historical reader for
finalized hash, epoch, policy window and operator eligibility, with a finite
256-boundary census per operator. A later failed read retains earlier durable
checkpoints. Test-only successor `de9c5aaf` changes one fixture line to isolate
the retained-proof downgrade guard; production bytes remain unchanged.

The [author receipt](/mnt/data/sn-testnet/validator-tail-boundaries-20261001/evidence/author-receipt.json)
is sealed by SHA256SUMS digest
`4c3301dd5c1d60de740808eae83be7760ffcbeb33ac4cca514d9c350ea4e6f87`.
It records 58 affected normal roots, 12 focused race roots, vet and actual
foreign-UID normal/race passes. Four original causal controls failed as
intended. The first retained-proof control survived because a different shape
guard rejected first; that failed isolation is preserved.

The [independent Sol receipt](/mnt/data/sn-testnet/validator-tail-boundaries-20261001/sol-independent-evidence/independent-receipt.json)
is sealed by SHA256SUMS digest
`71887a5fa230e4a66659c113d2f59dac8c5c6437da10ac67f958443aac8e45c0`.
It reproduced the passing normal/race and privileged-UID results and the same
causal blind spot. The corrected one-line test passes on frozen production
and fails at the intended retained-proof assertion when that guard is removed.
The [Astra test-only successor receipt](/mnt/data/sn-testnet/validator-tail-boundaries-20261001/test-successor-evidence/receipt.json)
is sealed by SHA256SUMS digest
`85eb9e0f85a5fd87a39100ad40ea01396324e922b3852c879fdbbe34ae7a41ca`;
its three focused tests pass normal/race and the corrected causal control
fails as intended in both modes.

This qualifies only canonical tail EVM boundaries. Nonempty steering-intent
graphs, historical provider bindings, live worker evidence, global signer
custody, applied weights influence and signed launch authority remain open.
No live service, signer or chain action was performed.
