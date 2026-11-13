# Required Safe state observation successor

This candidate is a distinct successor of frozen retained-receipt `5a0a8f66`. It addresses the adjacent current Safe code/storage/getter and final current-policy readmission paths. Earlier source and behavioral receipts remain unchanged.

The old wrappers combined an unsuccessful remote read with a returned-value comparison. A timeout or cancellation could therefore be labelled changed owner, nonce, guard, code, digest or pending authority without observing such a change. The ordinary EVM profile also treated a null required state result as an integrity contradiction. Empty EVM code is `"0x"` and an empty word has a 32-byte zero encoding; null supplies neither fact.

The explicit required-state profile admits only `eth_getCode`, `eth_getStorageAt` and `eth_call`, reusing the exact retained route, cancellation, response bounds and signed retry window. Owned contract reads select it for both current state and retained-receipt postconditions. Ordinary `rpcClient.callEvmRead`, native profiles, and intentionally nullable pending transaction/receipt lookups keep their existing semantics. No signed configuration is rewritten; only a newly synthetic signed test phase selects 300 seconds.

Safe state, scoped pending proof rechecks and final current-policy readmission now propagate unsuccessful observations before comparing values. Returned malformed or contradictory state remains a hard integrity refusal. The adjacent original-contract postcondition control proves that a recovered receipt cannot then lose custody merely because its historical code read is absent.

Six new deterministic roots cover more than a logical minute of null retries, timeout/null/cancellation at exact owner/nonce/guard/digest boundaries, positive returned conflicts, the real finalized-proof-to-pending reader, original receipt preservation, and the actual public v2 current-policy command. The public control cancels precisely at its final pending slot-8 read, checks no attempt/nonce/signature mutation, distinguishes an actual nonzero word, then continues through the original synthetic approved transaction. Behavioral normal/race/vet and causal results are delegated separately; this source note asserts no passing result.

Pure local encoding/hash checks and native proof decoding were not converted into transport errors. Paired-head public adoption `39e2744a`, capacity/retention revisions, and the final current published dependency composition are separate scopes.
