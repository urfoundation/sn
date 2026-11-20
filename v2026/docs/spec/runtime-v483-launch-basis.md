# Runtime 483 launch basis

The owner waived qualification and requested use of the working source checkout
without requiring an official release. The selected local upstream checkout is
`d43691767756fabaec697aeb014eb788639cccf9`; runtime/src/lib.rs declares 483.
It is a source basis, not a reproducible-build attestation or proof that these
source bytes generated the chain artifact. No full qualification was run.

The consumed selective-metagraph runtime wrapper and SelectiveMetagraph SCALE
structure match the previously selected 482 source byte for byte. Captured
finalized metadata at block 9344073 differs from 482 metadata by one byte:
the runtime spec version. API version remains 3. The actual finalized code and
metadata remain independently pinned by the signed deployment plan.

Code BLAKE2b-256: 0x99856ec64a61eb8011999a803bd336979512275214a173fea583abd5c89952d2
Metadata BLAKE2b-256: 0xcdb8b463eb27b3157640e949fe06a242ae2f80a050f91bcd43d6b55932dda923
Metadata SHA256: 37eef25143aaf16971597fbcc29164f8a74343d5743454fbd8b99f5d1ee78262

The source table preserves exact source/spec identity and leaves future
versions unadmitted. Independent approval, custody, and transaction checks
remain required. Added deterministic regression tests were not run after
the owner's qualification waiver.
