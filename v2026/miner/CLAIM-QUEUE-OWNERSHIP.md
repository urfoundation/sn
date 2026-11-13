# Claim queue ownership

The production claim daemon and claim swarm hold an exclusive process lock on
each physical `state_dir` for their full lifetime. This addresses the
MG-03 / PH-01 and PH-07 gap where two daemon processes could independently load
and replace the same `claim-queue.json`, losing retained signed outcomes even
though each process serialized its own nonce work.

A daemon acquires its queue before reading retained custody, seeding its nonce
floor, publishing state, announcing readiness or starting API workers. A swarm
acquires every configured queue first; if any queue already has an owner, it
releases only the locks it acquired and starts no member. Workers use the exact
loaded configs and acquired stores. The owner releases its descriptor after
joined worker cleanup, including cancellation and startup errors.

The lock is on the opened directory, so replacing the queue file cannot replace
the lock. Existing aliases resolve once to the same physical directory. Reads,
temporary files, atomic replacement and directory sync use that descriptor;
later path replacement cannot redirect the owner's writes. A changed or
symlinked directory pathname stops further store access. Startup refuses
symlinked, non-regular, foreign-owned or non-private queue entries. An existing
owner may still atomically replace an unsafe queue leaf from its retained
in-memory state without following the unsafe entry, preserving the prior save
behavior.

The queue schema and serialized signed fields are unchanged. Opening or loading
an existing v1 queue does not migrate or reformat its bytes. Its existing
in-memory crash rule still converts `submitting` with retained transaction
identity to `uncertain`, retaining the exact hash and raw transaction. A closed
store cannot read or publish state; restart requires a new successful lock.

[Claim finality](../mainnet/evidence/miner-claim-evm-finality-20261001.md) uses the
EVM finalized identity for EVM state and receipt heights. Each consequential
observation closes its original canonical hash and the actual finalized tag;
a native header number supplies no EVM finality. Fresh publication reconciles
the original receipt after submission. Missing or regressed finality preserves
the signed queue entry and nonce floor for exact-byte recovery.

Retained reads and publications admit at most **16 MiB per queue**, including
the JSON writer's indentation and final newline. Startup checks the opened
file's size before allocation, then reads at most the limit plus one byte so
growth after that check cannot evade admission. A refused read returns no
queue. A refused publication creates no temporary file and leaves the previous
queue and its signed fields intact. Publication also checks the retained regular
file's size, so a repeated or changed save cannot replace an oversized file
after its prior acknowledgement was invalidated. The limit does not authorize
truncation, compaction, a fresh queue, or another nonce; an oversized retained
file needs separately reviewed recovery.

The sizing fixture uses the actual v1 queue and signed claim ABI/RLP encoding:
1,024 retained epochs, a 16-node proof and 2 KiB of diagnostic text per record,
plus hashes, receipt fields and timestamps. Each representative record fits
within 8 KiB and the complete fixture stays below half the 16 MiB limit. The
production reference claim window is eight epochs plus one grace epoch, and
1,024 mainnet epochs represent 51,609,600 blocks at the required 50,400-block
period. This gives substantial history margin without using accelerated testnet
timing. Swarm members keep separate queues; the up-to-1,000-member limit does
not multiply entries in one file. The fixture is a sizing assumption, not a
schema maximum: proof lengths and diagnostic strings can vary, and the actual
encoded byte count is the admission boundary.

This per-file boundary does not supply an aggregate host-memory budget, bound
future epoch-jump discovery, or cap JSON encoding allocations for already
oversized in-memory state. Those workload and storage qualifications remain
separate gates. Existing oversized files are preserved and refused, without a
schema migration or a weaker legacy reader.

This is local filesystem ownership. Every process using a queue must use the
same retained physical directory and a filesystem with working exclusive locks,
atomic rename, file sync and directory sync. Do not replace/delete the directory,
restore an older backup, copy it into an independent namespace, or run another
relayer with the same key under a different queue owner. Cross-host ownership
and a shared nonce owner across different queues/processes remain separate
production gates. The process-local swarm nonce admission retains its original
scope. Unsupported platforms refuse daemon startup before creating state.

The [ownership source at `b7e84b2f`](../mainnet/evidence/miner-claim-queue-owner-qualification-20260929.md)
and [retained-byte successor at `4a9b9e6a`](../mainnet/evidence/miner-claim-queue-capacity-qualification-20260929.md)
passed independent normal/race, full miner normal, vet and causal-control
qualification. Their product and test files were integrated byte-for-byte
into the mainnet hardening branch. No mainnet route, key, transaction, receipt
or deployment is supplied by either change.
