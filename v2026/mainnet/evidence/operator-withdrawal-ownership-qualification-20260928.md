# Registration recovery and withdrawn upload sessions

SN registration `9a5b2638b27216e23f72d1b86bb9dca7bea15494` and its withdrawal
correction `d3ce3e88a41f0418743d58941efe18bad1d47a83` are integrated as
`deae4a88` and `76b75252`. Their integrated Go files match the frozen corrected
candidate byte-for-byte. Astra authored the changes; Terra executed the tests.

The original public-root qualification exposed two real failures: an invalid
API reply and a revoked client each stopped retained native observation after
ten canceled-context failures. Both reproduced normally and under race
detection. The shared runtime gate reconstructed an active upload census before
reading the original signed native intent. Canceling a correctly withdrawn API
session therefore prevented unrelated native receipt/application recovery.

The correction validates immutable configured source/custody at that recovery
gate. Active-session checks remain at publication construction and effect-side
callbacks, including callbacks acquired before withdrawal. Invalid original
authority, signer, routes, bounds and custody still reject recovery. Tests now
release canonical native responses through the actual withdrawal transition;
optional diagnostic delivery no longer orders their recovery fixture.

| Corrected scope | Normal | Race |
| --- | --- | --- |
| 20 affected roots, including both failed public roots | 20 passed; 102.010 s | 20 passed; 597.215 s |
| Restore the outer active-session gate | Intended failure; 33.077 s | Intended failure; 119.908 s |
| Remove the API-local permanent-failure latch | Intended failure; 18.092 s | Intended failure; 100.678 s |

All three maintained captures passed four stages each, with unchanged source
and joined processes. Positive bodies exited 0; controls exited 1 at their
declared assertions. Compile-only, validator vet and actual metadata/module
admission passed. Each control preserves the corrected fixture bytes. The latch
control is rebased onto `d3ce3e88`, so its matching whole-root positive passes;
the earlier latch result on the failing parent is not used as a closed pair.

Raw evidence is in
`/mnt/data/sn-testnet/evidence/operator-withdrawal-ownership-20260928`.

| Report | SHA-256 |
| --- | --- |
| `terra-positive/report.json` | `69dcaf005d7cf89ce8258330b3896fed7b84c272e3f681b950fb05e59fd92296` |
| `terra-control/report.json` | `4e8ae01ebed992d583514ebde69e8f10984bb2d987a0723ae89fc68025d6c969` |
| `terra-latch-control/report.json` | `6fdefcb0cd1322c2fed4d7d4f559fe54e851fed3eb20d3f8bdb41f02c16bb261` |

The original composed capture remains in
`/mnt/data/sn-testnet/evidence/operator-registration-composed-20260928`.
Its SDK scope passed 47 roots plus 11 legacy descendants, and clientauth passed
16 roots in both modes. Its validator scope passed 58 roots and failed the two
public roots in each mode. Root converted the already retained failed stdout
with Go's standard test2json solely to enumerate those outcomes; no body was
repeated. The original failed package and report are preserved, not rewritten.

The original causal families also remain retained. The SDK Connect-timeout
control initially refused before any body because its alternate Go replacement
left nested SCTP in the original Connect tree, which was omitted from the
declared sources. Adding that already-frozen source declaration allowed both
control modes to reach their intended assertion; no production or test bytes
changed. The original refusal remains alongside the corrected report, SHA-256
`07506d4fb559584bd0e5872a0e11aad23d6dae0a0913a722e333a6cad0aed2b1`.

This corrected graph uses SDK `31f92343`, cumulative Connect `bcf9b324` and
server `736d7b8f`, with nine complete physical source manifests and resolved
module paths retained in the handoff. The maintained runner SHA-256 is
`cf73edc6abe2ddf42c7dbe5aa3840bd12d3093a6349ea7904e0b19cb3ba70368`.
Separately qualified server transaction/grammar/allocation fixtures retain
their own graphs. Later SDK/Connect redirect changes, diagnostic cause
isolation, the actual production route/DB fixture, final release composition,
server-first migration and deployment require their own evidence. No new live
identity, mainnet action or activation is authorized by these local tests.
