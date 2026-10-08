# MG-03 bounded historical native capture source qualification

Server candidate `8a47dfe301095bb35803debe2bcd54149f4368f5`, tree
`e7ae57da927444edb20088a1ddbbf11200b6f2ed`, adds durable historical native
storage capture on integrated server `94229abb`. It is committed on
`fix/operator-historical-native-capture-20261001` in
[/home/by/urnetwork/temp/astra-operator-native-capture-20261001/server](/home/by/urnetwork/temp/astra-operator-native-capture-20261001/server).
The source is merged into server `main` at `24ac67d4`, after an unrelated
monitoring-document update. Independent source review has passed; release
composition and deployment are not established. The selected
[SN233/server942 release](release-233ea2be-server942-20261001.md) predates this
source and cannot attest its new commands or binaries.

The [capture contract](/home/by/urnetwork/temp/astra-operator-native-capture-20261001/server/strecovery/HISTORICAL-NATIVE-CAPTURE.md)
supplies the missing producer for the
[qualified historical native witness](operator-historical-native-storage-qualification-20260929.md).
`CaptureReceiptHistoricalNativeState` and `strecovery capture-historical-state`
first replay original archive, receipt, checkpoint and finality evidence, then
derive one receipt's unique native child and linked parent internally. The
explicit `parent_execution` or `child_post_state` role selects that exact native
hash. Missing parent roots and ambiguous mappings refuse before ownership or
network reads. No supplied root or latest/finalized label can select state.

The three-method read profile admits only `chain_getBlockHash`,
`state_getReadProof` and `state_getStorage`. Each incomplete invocation compares
observed genesis with the supplied checkpoint, then reads the exact selected
native hash. Proof `at` must match it. The existing trie verifier must establish
every selected raw value or absence, distinguishing null from a present empty
value and from incomplete proof nodes. The pinned SDK state API, `ReadProof`
encoding and full-state implementation are retained with source hashes; this
codec review does not admit a deployed runtime or production archive endpoint.

The existing private directory lock, create-only publisher and request/byte
ledger are reused. The full config and original census bind the journal;
changed routes, key sets, root roles or proof inputs cannot reuse it. Requests
are synced before transport, and missing completions conservatively consume a
full response allowance. Completed RPC result data is retained before semantic
interpretation. Conflicting retained replies refuse restart instead of being
replaced by later responses. Interrupted reads preserve already collected
results and spent budgets.

Bounds are 1–32 sorted distinct raw keys, 1,024 bytes per key, 8,192 proof nodes,
16 MiB per wire response, 128 MiB lifetime response bytes, 32,768 lifetime
requests, 64 MiB decoded nodes/keys/values, finite 60–900 second retry windows
(default 300), and a maximum 15-minute invocation. The wire bound is stricter
than the existing verifier's decoded item bound for large hex responses; no
partial selected-key census is published. No claimed archive completeness or
runtime interpretation follows from these resource limits.

Only complete fresh verifier replay can publish mode-0400 `witness.json`.
Completed capture replays without creating an RPC client; the separate
`strecovery verify-historical-state` command is entirely offline. A smaller
independently valid key subset cannot replace the selected capture scope.
Original archive, collection, checkpoint and finality bytes remain unchanged.

Qualification used Go **1.26.6 linux/amd64**, exact server `8a47dfe3`, companion
SN `8eb43180`, and the unchanged resolved module graph, including Connect
`e1b5d77b` and SDK `5d37be38`. All six local physical module repositories were
clean. The later SN documentation base `4164b197` adds reporting only; it does
not replace the tested source identity.

**173 roots pass in normal and race modes**, including 12 new library and two
new CLI roots. The primary streams contain 172 roots each. A scope audit found
that their `^TestCensusDatabase` skip prefix also excluded the local
connection-pin unit test; a separate disjoint stream qualifies that root in
each mode. Every selected root has RUN/PASS, all package/wrapper exits are zero,
and no selected root is skipped. The three unchanged PostgreSQL census roots
remain unrun; this is not full-package database coverage. `go vet` passes both
packages, module graphs match byte-for-byte, and source remains clean.

Independent qualification on clean exports of the exact server `8a47dfe3`
and companion SN `b13fe38f` trees passed all 173 selected roots in normal and
race modes, with zero skips/failures, and vet passed. Three independently
selected causal controls removed proof-block equality, trie verification or
retained budget enforcement; each failed at the intended assertion in both
modes. The independent [review](/mnt/data/sn-testnet/qualification/operator-native-capture-sol-independent-20261001/review.md)
and [receipt](/mnt/data/sn-testnet/qualification/operator-native-capture-sol-independent-20261001/summary.json)
retain exact commands, graph and logs; receipt SHA-256 is
`1e3f3a3e5e4bf2132756ac1d2f94ec94ad0164cb778795f893db743d94136fd9`.
This remains source qualification, not production endpoint admission.

Seven single-regression author controls compile and reach the intended assertion,
root/package FAIL and exit one in **both normal and race modes**:

| Removed or weakened behavior | Observed causal failure |
| --- | --- |
| Exact proof `at` equality | Another block label is accepted despite the selected parent identity. |
| Trie key/value verification | A forged raw value is published as a witness. |
| Durable pre-request reservation | The actual transport runs before its reservation file exists. |
| Retained response-budget enforcement | Eight interrupted full reservations are reset and more reads succeed. |
| Completed raw-result reuse | Restart refetches already retained proof/value evidence. |
| Capture-manifest refusal | A completed capture is retargeted to another route. |
| Observed genesis equality | A distinct synthetic genesis reaches storage capture. |

The first focused run had one fixture error: its nominal foreign genesis
`0xaaaa…` equaled the synthetic checkpoint. The fixture now uses distinct
`0xbbbb…` with an explicit inequality precondition. The original failed log is
retained; no production check was changed to make that fixture pass. Later
normal/race qualification and the causal genesis control use the corrected
committed fixture.

| Retained artifact | SHA-256 |
| --- | --- |
| [85-file manifest](/mnt/data/sn-testnet/qualification/operator-historical-native-capture-20261001/SHA256SUMS) | `3f7d83b0e75db0341b530eb54599e6f8c0ed1ab04a0e6ad9a8206fe88ff8c455` |
| [Exact positive scope and controls summary](/mnt/data/sn-testnet/qualification/operator-historical-native-capture-20261001/summary.json) | `a5bc1c14198dc35db07610d742b920eac5705519e3d8c2e13e70b296ca638453` |
| [Fourteen causal execution receipts](/mnt/data/sn-testnet/qualification/operator-historical-native-capture-20261001/controls/results.json) | `2b248742094497e8ccd4b2be6695600d5eb9e43dd0744200c4ae5b534607fa44` |
| [Ten changed source-file digests](/mnt/data/sn-testnet/qualification/operator-historical-native-capture-20261001/source-files.sha256) | `ef4d2f5e7a9e85079226b9c69133627687ba05ac515d884899d4ffc316efc40a` |

All node interaction was synthetic transport or loopback HTTP. No production
database mutation, live signing, broadcast, service installation or deployment
occurred. The result remains `unapproved_historical_storage_capture`; all
independent authority/accounting/spending flags stay false, and original,
replacement, cancellation and reverted histories retain null actual fees.

MG-03/PF-03 remain open for independently admitted genesis/checkpoint and
execution runtime/source/metadata, runtime-qualified historical debit/refund
attribution, payer/native extrinsic and account nonce interpretation, owned
production archive capability, service adoption, release composition,
distributed custody and live restart without duplicate actions. Local private
journaling does not prove anti-rollback or complete cross-host custody. This
increment closes the bounded historical proof **capture implementation** only.
