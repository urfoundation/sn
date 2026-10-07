# Finite offline runtime transition replay

This increment adds executable evidence checking on the path toward RT-04.
[Independent qualification](evidence/runtime-semantic-replay-qualification-20260930.md)
passes 67 positive executions, eight normal causal controls and four selected Go
race controls. Rust execution is qualified normally only. It does not establish
complete semantic equivalence, install automatic runtime selection or authorize signing.

`ReplayProductionRuntimeContinuityContext` authenticates the original schema-3
authority, independently signed continuity policy and separate verifier certificate.
The certificate's evidence hash must identify the exact replay job. That job binds
the policy bytes, source/build evidence reference, approved rules identity and exact original
and candidate artifact identities. No network connection or private signing key is
used. Original config, approvals, pending signed bytes and historical custody remain
unchanged.

The approved verifier executable is copied into a private directory, hashed against
the policy, and run as one joined subprocess with caller cancellation and a
two-minute deadline. Each output pipe is bounded to 64 KiB. A failed, cancelled,
truncated, mismatched or overclaimed result yields no report. The executable cap is
512 MiB and the job cap is 48 MiB; copying checks cancellation between file reads.
The deadline cannot interrupt an operating-system file read that itself stalls.

The separate `runtime-transition-replay` binary uses the existing pinned SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`. Both exact Wasm blobs first pass the
unchanged stateless `Core_version` and `Metadata_metadata` checks, including full
SCALE decoding and byte hashes. Transition calls then use **on-chain** context and
a separate explicit storage host tuple. The stateless metadata probe still cannot
invoke storage hosts.

Each supplied case starts from an explicit complete top-level fixture map plus the
executing artifact as immutable `:code`. Steps retain state within that case. Both
modules must match every declared return byte and the complete resulting map,
including insertions and deletions. The report binds the exact job, rules reference,
separate case-data digest and compared outputs; it reports finite coverage and keeps
semantic-rules-verified, complete-equivalence and selection false.
Fixture state is not authenticated mainnet state and no universal proof is inferred.

`rules_sha256` retains the independently approved semantic rules identity from the
policy. `cases_sha256` hashes the exact `cases_json` bytes as a separate input bound
by the signed evidence. Case data is not the semantic rules. This finite executor
does not consume or prove implementation/coverage of those external rules; it only
executes its fixed comparison procedure on the supplied cases. A certificate's
all-domain assertion remains an authenticated assertion rather than a proven fact.

The host subset supports get/read/set/clear/exists/next-key and balanced storage
transactions. It refuses invoked offchain, child-storage, prefix, append/root,
keystore and other unimplemented hosts. Limits are 16 cases, 16 steps per case,
8 MiB per Wasm, 1024 application keys/4 MiB aggregate state, 512-byte keys,
256 KiB values/call inputs/expected outputs, 4096 storage operations per case,
16 nested transactions and 1024 pages (64 MiB) of transition linear memory.
These bounds do not constitute an operating-system resident-memory sandbox.
The worker by itself has no wall-clock deadline; callers must use the Go owner.

Rust fixtures execute synthetic Wasm through the actual SDK. Go fixtures use an
explicit synthetic subprocess to qualify signature/custody, output and cancellation
boundaries. Neither fixture family is an approved mainnet base/candidate pair,
economic test suite or proof of the certificate's all-domain compatibility assertion.

Remaining P0 work includes independently approved real source-to-Wasm evidence,
authenticated state and rules covering the seven required domains, all needed host
semantics and changed-economic controls, qualified proof/output custody, and durable
production selection with immediate signing re-admission. Standard validator/miner
and other role integration remains open. No public Safe route, live chain action or
mainnet activation is part of this candidate.
