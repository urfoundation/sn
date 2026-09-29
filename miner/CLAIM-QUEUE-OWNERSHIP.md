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

This is local filesystem ownership. Every process using a queue must use the
same retained physical directory and a filesystem with working exclusive locks,
atomic rename, file sync and directory sync. Do not replace/delete the directory,
restore an older backup, copy it into an independent namespace, or run another
relayer with the same key under a different queue owner. Cross-host ownership
and a shared nonce owner across different queues/processes remain separate
production gates. The process-local swarm nonce admission retains its original
scope. Unsupported platforms refuse daemon startup before creating state.

The source and deterministic regressions are a qualification candidate.
Behavioral normal/race tests and causal controls are pending independent
execution. No mainnet route, key, transaction, receipt or deployment is supplied
by this change.
