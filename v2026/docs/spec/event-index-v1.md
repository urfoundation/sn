# Finalized event index v1

The deployment receipt defines a nonzero start block and hash. Index checkpoints
store `(chain_id, block_number, block_hash, runtime_spec, transaction_version)`.
Only finalized canonical logs and Substrate events may affect deposits, policy,
bindings, roots, payouts, or validator weights. On restart the index re-reads the
checkpoint hash; a mismatch rewinds to the most recent matching ancestor and
replays. A gap, unknown runtime, endpoint disagreement, removed log, or missing
deployment code hash stops all writers while reads and immutable-vault claims
remain available.

Raw finalized receipts and public artifacts are append-only in server/blob
MinIO. PostgreSQL is a rebuildable query index. The public server API addresses
artifact bytes by SHA-256 and indexes them by deployment, run, netuid, epoch,
operator, validator, and transaction.
