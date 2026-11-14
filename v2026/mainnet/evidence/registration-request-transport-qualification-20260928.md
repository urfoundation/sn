# Versioned registration transport qualification

Astra's cumulative Connect `b163f9dd9ac374942fe97331f26631248a9c1f81` and
SDK `516521fb16da46c9f4bff0b58221e1941694f616` passed their component checks.
The review branches in both repositories are
`codex/mainnet-registration-hardening-20260928`. They include the earlier
durable registration and bounded exhausted-cause changes, not just the final
redirect patch. Production deployment remains separate.

Versioned registration scopes redirect refusal to that operation, preserving
its original endpoint, request bytes and bearer across retries. Ordinary HTTP
operations retain their existing redirect behavior. The SDK checks the encoded
16-KiB body bound, including escaped characters. Connect bounds physical error
inspection outside its state lock and retains ambiguity as a hard cause rather
than flattening integrity failures into a retryable timeout. Nil caller headers
are preserved while constructing the browser-specific request copy.

Terra ran **38 Connect roots plus 32 declared legacy descendants**, and **nine
SDK roots**, normally and with race detection. All passed; the positive capture
passed eight stages with joined exit-0 bodies and unchanged source. Seven causal
roots across six control groups reached their intended failures in both modes.
Two collector roots use the same byte-identical overlay and share one group.
Every control passed four stages, joined exit-1 bodies and unchanged source.
The complete driver exited 0. Compile-only, vet and metadata/module checks passed.

The physical tests cover 307/308 same-origin path and cross-origin redirects,
unscoped redirect behavior, incomplete response retry with the original request,
encoded-size overflow, blocked foreign error methods, bounded cyclic/deep/wide
error graphs, and nil/empty headers. Native tests and JS library compilation
do **not** establish execution in a real browser. That limit remains explicit.

Raw evidence is in
`/mnt/data/sn-testnet/evidence/registration-request-no-redirect-20260928`.

| Report | SHA-256 |
| --- | --- |
| `terra-positive/report.json` | `75b3cd60a0db85dd353a8dd247d659e4cc8e3face6aeb4a6ec78223f14f577f6` |
| `terra-connect-redirect/report.json` | `56d1fa4aef8225686103956640cd482cde5217117dbcd49469d5a8a90f8dbe25` |
| `terra-browser-fetch-option/report.json` | `5e6fb643dc752c7436f6d5bd28cc075aa748f34b82f1fc61903d248347d3861f` |
| `terra-collector-exhaustion/report.json` | `e83105aa1e1de4657631ef6c6479310fceda1161db386b6cf122c6886bf042a4` |
| `terra-sdk-request-scope/report.json` | `459c1c8bd174b8212bdd5d94b80619ec501e0ec28b33cee83d9725ae436b134a` |
| `terra-sdk-encoded-bound/report.json` | `06efc152596c88ce586065887c37db14c88536ca59e1ee73ba9dd5a42a4e8aab` |
| `terra-browser-nil-header/report.json` | `2b630dbd9e77f29b625c245a35463a0225945d1500ec9df703dcf0c4de289d57` |

Complete physical manifests include SN `d3ce3e88`, server `736d7b8f`, SDK,
Connect including nested SCTP, and the other pinned replacements. Mutated
controls additionally declare their own complete source tree. The runner is
the independently qualified literal-slash checker, SHA-256
`15da70c645c7de9839c7c0a6c511358a8c65b29f823f4f6e9dad0dfc1e338d9d`.
Original earlier metadata refusals and retained Connect evidence stay intact.
The real API/database path and the final combined SN consumers have separate
checks; component passes do not qualify server migration or live activation.
