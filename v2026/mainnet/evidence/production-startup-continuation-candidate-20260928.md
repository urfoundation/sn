# Public production startup continuation candidate

Component qualification is complete, with failed captures and scoped recovery
preserved below. Source through `339656153ab41741fbf90a702908a6df27efd6fa` is
integrated through `56023b18`. This is not evidence of a live mainnet transaction,
an accepted composed release or an economic acceptance result.
The prior continuation/native transport scopes are unchanged except for the
localized historical constructor, composed-cause classifier and startup wiring
listed below. Their independent qualification remains reusable.

## Completed component results

Terra medium ran the frozen sources. Raw evidence is under
`/mnt/data/sn-testnet/qualification/production-startup-continuation-20260928`.

| Source and scope | Normal | Race | Retained disposition |
| --- | --- | --- | --- |
| `282fb402`, CRv4 constructor/cause selection | 4/4 passed, 0.012 s | 4/4 passed, 1.066 s | Reused unchanged |
| `282fb402`, validator selection | 65 passed, 2 failed; package fail, 91.538 s | 65 passed, 2 failed; package fail, 263.746 s | Original failures retained; 65 unaffected passes reused |
| `fca0b16c`, public retained-intent root | Passed, 31.17 s | Passed, 189.39 s | Original signature reconciled before current preparation; reused |
| `33965615`, fresh-start root and two handler ownership roots | 3/3 passed, 9.162 s | 3/3 passed, 63.952 s | Fresh startup and cleanup correction complete |

The original 67 validator roots therefore have passing scoped coverage in both
modes, plus the two added handler regressions. This does not relabel either
failing full `282fb402` invocation as a pass. The intermediate `cd709cf4` fixture
failures and the `fca0b16c` fresh-start cleanup interruptions remain in the
[diagnostic receipt](production-startup-diagnostic-20260928.md). The orphaned
`fca0b16c` race wrapper has no claimed shell exit.

Four causal controls in `282fb402/causal` reproduce their intended failures in
both modes: unrelated latest-runtime construction, native-only classification
of independent errors, a whole-stage I/O deadline, and request-as-readiness.
The fifth, original public startup ordering, completes on `33965615` in both
modes. Its normal collector initially compared the wrong literal; the retained
test already contained the manifest's exact assertion, `actual RunRelease
queried unrelated latest metadata before retained intent recovery`. The checker
was corrected without rerunning that body. Its trailing metadata cancellation
is the result of the test's deliberate barrier cancellation. The original
checker error is retained in `public-order-checker-correction.txt`.

CRv4 and validator vet pass at `33965615`, and source head/status and module-path
before/after checks match. The corrected capture did **not** seal physical
replacement contents or revisions before execution. These are component
results; retain that limitation and perform only affected interface checks
when composing the actual release dependency family.

| `33965615` stream | SHA-256 |
| --- | --- |
| `normal.jsonl` | `16e012d91af7c2a475245eaa90ae3de3c100f882a8c8b31ec27c21c3522af6ef` |
| `race.jsonl` | `5852dd8f389054c3a26f2cfe5055188edbf2541f3fb7be54d083f4d9ad5ac39b` |
| `public-order-normal.jsonl` | `5ff7cf8962581357182caa5ce5493a328260dfd58ca81c7119c6f737235943eb` |
| `public-order-race.jsonl` | `50dede5d3c5a1d54ad3957de80831b9d3e87425e932d091ad8bf20d994585c2d` |

## Correction and retained reproduction scope

The public `RunRelease` root now initializes observation at the independently
signed activation block, authenticates real activation/disk/history/intent
owners, and reconciles original signed native liability before unrelated current
preparation. Current UID/stake and actual initial settlement publication remain
mandatory before fresh intent preparation or trail workers. A request cannot
close that readiness gate. Parent cancellation owns long local semantic replay;
the 300-second I/O budget no longer becomes a timer for the entire CPU/disk stage.
Actual RPC owners retain finite request/retry budgets.

Independent parallel activation readers preserve both transient native and EVM
causes. The phase classifier checks each independent branch while treating a
native transport wrapper as one opaque verdict, so a local close/integrity cause
inside it cannot be retried by generic unwrapping. Historical observation remains
separate from current producer capability.

The real-root fixture uses a separately signed zero-price policy and genuine
M8 proofs, retained compact inputs, signed production sidecar, real V2 intent
begin/update, canonical native receipt/events/weights and ABI-encoded activation
journal state. Native traffic crosses an actual explicitly approved WebSocket
server; both operator JWT/session/public-object paths use real local HTTP and
WebSocket transports. A physical current-EVM outage barrier must occur only after
the original intent has reached its applied row. The original exact signature,
creation time and receipt survive; physical `author_*` requests are counted so a
replacement send cannot be hidden by a helper's in-memory submission adapter.

The distinct empty-store case uses a separately signed config, new private state
and scratch roots, and already provisioned client keys/JWTs. It reaches an actual
trail seed API only after current eligibility and durable initial settlement.
It does not claim new client registration or offline operator startup.

Author lane executed compile-only checks, not test bodies. Terra used these
initial selections normally and with race detection, retaining their actual
root census and results above:

```sh
go test ./crv4 -run '^Test(DialChainAtContext|DialChainContext|SubstrateReadHttpCauseSubtree)' -count=1 -timeout=600s
go test ./validator -run '^Test(ProductionStartup|ReleaseActivationV2|ReleaseBootstrapV2|ReleaseStartupOwnerV2|ReleaseShutdown|RunReleaseV2)' -count=1 -timeout=1200s
go vet ./crv4 ./validator
```

The independent mixed HTTP activation case keeps the actual supported 60-second
caller budget. It does not shorten production deadlines for test convenience.
Local-history timing is checked through the stage's exact inherited deadline
and a physical completed prefix; no five-minute wall-clock sleep is required.
Causal controls must restore the original public startup ordering, discard the
historical constructor pin, restore the native-only whole-join classifier, and
apply the old full-stage I/O deadline. Controls must fail named assertions after
compilation; unrelated setup errors do not count.

The earlier diagnostic history is retained in
[the diagnostic receipt](production-startup-diagnostic-20260928.md): HTTP writer
route rejection, approval hash before YAML normalization, public metadata/header
bounds, and the later chunk override. These were fixture assumptions that helper
admission did not expose. No production route, signature, capacity, custody or
policy gate was relaxed to pass them.

Remaining launch work is explicit: durable authenticated receipt-prefix chunks
for validator and miner, historical-only foreign-nonce resolution, automatic
compatible runtime-policy admission, and first/missing-client JWT registration
recovery. Both live operator server-key/public-object/session routes remain
startup dependencies. Registration can mutate identity and needs its own durable
reconciliation; it cannot be wrapped in generic read retries. Native writer
configuration still requires WS/WSS for `author_submitAndWatchExtrinsic`; HTTP
EVM/read capability on the same node grants no writer fallback. An epoch or local
approval deadline does not revoke already signed immortal bytes.

## Second fixture correction: real initial activation and complete JWT

Correction base: `cd709cf4c07a01e0c05087123c814791cd45e201`.
Worktree: `/mnt/data/sn-testnet/worktrees/sn-mainnet-startup-boundary-jwt-20260928`.
This correction changed test fixture files only
(`recycle_measurement_test.go`, `production_startup_fixture_test.go`) and these
receipts. No production implementation changed after the frozen full282 scope.

The retained-source fixture now runs the real V2 initializer before any genuine
M8 trail; source-local setup declares its complete one-operator census and the
public root later joins both independently created sources under the signed
complete config. Initial activation and ordinary detach are genuine durable
owners. The token factory supplies the required device identifier for both
stored and refreshed session bytes. See the preserved diagnostic receipt for
both prior normal/race failures.

Run the two affected public roots, normal and race, without repeating passed
unrelated full282 bodies:

```sh
go test ./validator -run '^TestProductionStartupRunRelease(ReconcilesBeforeCurrentPreparation|InitialDeploymentBecomesReady)$' -count=1 -timeout=600s
```

Then reuse these exact correction bytes for the pending predecessor public-root
causal control. Its failure must reach the intended unrelated-current-read
assertion, never count a fixture startup refusal as success. Additional public
root failures must remain explicit and be corrected in another frozen revision.

## Third fixture correction: bounded blocking HTTP ownership

Correction base: `fca0b16c843601b680d92fbe0a8a3a43dc6365fe`.
Worktree: `/mnt/data/sn-testnet/worktrees/sn-mainnet-startup-handler-close-20260928`.
The completed correction changes only startup fixture/test sources
and documentation. Retained-intent public startup passed normal (31.17s) and
race (189.39s) on fca0; its fresh-root cleanup hung after readiness. Preserve the
diagnostic stack and those scoped results.

Seed and EVM blocking handlers now consume/close bounded input before their
barrier and have independent release signals owned by fixture cleanup. Adjacent
WebSocket loops retain their client connection ownership. No runtime timeout
or production behavior changes. Compile-only validation is permitted in the
author lane; final bodies belong to Terra:

```sh
go test ./validator -run '^TestProductionStartup(RunReleaseInitialDeploymentBecomesReady|BlockedHandlersOwnRequestAndCleanup|BlockedHandlersRejectIncompleteInput)$' -count=1 -timeout=600s
```

Run normal and race. Then apply the original public-root ordering control to
`TestProductionStartupRunReleaseReconcilesBeforeCurrentPreparation` with these
corrected fixtures; a named unrelated-current-read failure is required. Do not
repeat the unchanged full selection or treat fixture setup/cleanup failures as
causal evidence. New barrier controls can restore only the old unread seed body
or remove the independent release to demonstrate their exact ownership seam.
