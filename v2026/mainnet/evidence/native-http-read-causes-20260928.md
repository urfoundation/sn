# Native configured HTTP read cause preservation, 2026-09-28

The component qualification below completed on clean author source
`ec2bc584d5640f7d22dc06f79c7e26ea357cb84a`, integrated without conflicts as
`790dacb0`. This records the original transport component. The separately
[qualified continuation integration](production-native-http-integration-20260928.md)
adds response-size headroom and retries pure physical close failures; its
evidence does not replace the original component results below.

Author workspace: `/mnt/data/sn-testnet/worktrees/native-http-read-causes-20260928/sn`,
based on qualified root `b00d1e54be93a5f47d047810dd76ed15744d66fa`.
Evidence and Terra runner:
`/mnt/data/sn-testnet/evidence/native-http-read-causes-20260928/`.
The physical sibling replacements come from the frozen
`server-model-final-20260927` graph; consumed modules/files and clean Git heads
are retained separately in the evidence manifest. No frozen installer candidate
or previous test capture is changed.

## Root cause and bounded change

The pinned `github.com/centrifuge/go-substrate-rpc-client/v4` version
`v4.2.2-0.20240919131012-e3b938563803` in `gethrpc/http.go` returns non-2xx status
through `errors.New(resp.Status)` and later text formatting. Its success decoder
returns EOF without retaining whether the physical read or complete JSON failed.
Inspection of the local pinned module and actual configured-client tests ground
the change; no replacement module or fork is used.

`DialChainContext` uses the library's existing `DialHTTPWithClient` hook for HTTP.
A private context marker is set only by the existing allowlisted native read
owner. Its instance-owned transport retains numeric HTTP statuses and physical
round-trip/body errors before GSRPC can flatten them. It reads and closes the
complete physical body before admitting JSON decoding, so a valid JSON prefix
cannot conceal interrupted framing. A 32 MiB finite response bound exceeds the
existing 17 MiB runtime-code and 21 MiB block wire bounds; oversize replies stop
hard and are not silently truncated or retried. Standard HTTP decompression is
bounded at its decoded body reader. A separate body-close failure stays hard.

No new retry loop is added. The existing 300-second owner, 60-second attempts,
argument freezing, caller cancellation and shared capacity handling remain in
charge. Only 408/425/429 and 5xx statuses are transient. An empty 2xx body is an
absent physical response; nonempty fully received malformed JSON, permanent
JSON-RPC errors and redirects remain hard. Writes and unknown methods bypass
the decorator and cannot acquire the read marker or a new attempt budget.

The public `RetryableSubstrateReadTransportError` requires actual configured
read origin and an entirely transient error tree. Joined owner deadlines retain
the origin; joined integrity, local file, close or cancellation failures refuse
retry. Bare EOF, timeout or status text grants no authority. Existing websocket
reconnect-marker compatibility remains private and is not used by this HTTP
classifier. Runtime, canonical header/body and receipt checks are unchanged.

## Deterministic qualification scope

Eleven new `TestSubstrateReadHttp` roots cover:

- Actual configured 500/502 recovery and unchanged pinned parameters.
- Persistent 502 through one virtual five-minute owner and structured cause.
- Empty body, incomplete physical JSON and complete JSON with broken framing.
- Complete malformed JSON, result type errors and permanent JSON-RPC failures.
- Permanent statuses/redirects and caller cancellation during a physical body.
- One-call write/unknown-method failure with no read retry owner.
- High-level `DialChainContext` metadata recovery before genesis/runtime reads.
- Body-close/size failures, mixed integrity joins and diagnostic lookalikes.

Time control replaces only the existing retry clock/pacing hooks; HTTP requests,
GSRPC decoding and production constructor routing are real local fixtures.
Explicit response/cancellation barriers establish ordering. The selected body
also includes prior `TestSubstrateRead`, native capacity and `TestDialChainContext`
regressions affected by the shared classifier/constructor. The collector retains
all independent normal/race failures and only counts causal controls when they
reach their exact intended assertions.

Author checks are `go test -mod=readonly -c ./crv4`, `go vet -mod=readonly ./crv4`
and formatting/diff checks. **No test bodies ran in the Astra author lane.**
Terra completed normal/race and causal qualification. No live route, live key,
signing, broadcast or node mutation was used. Production continuation integration
with the exported classifier is recorded in the separate receipt linked above.

## Completed component qualification

The selector `^Test(SubstrateRead|DialChainContext|ContextSubstrateClientRecognizesOnlyPrivateIPv4AsOwnedRoute)`
ran all 30 selected top-level roots: normal 64.441 seconds and race 65.622
seconds, both package results passing. Five controlled regression families
reached their intended named assertions in both modes, with ten retained
exit-1 captures: flattened status, decoder-before-framing, write replay,
hidden close failure and mixed integrity classification. Vet and build passed.
The source, 89 consumed local files and qualification-tool manifests passed
before and after; the author head remained unchanged and its worktree clean.

Root integration combined the constructor change with the separately qualified
receipt/source reader changes in `crv4/chain.go`. The HTTP adapter, retry owner
and their new tests match the qualified source exactly. On integrated
`790dacb0`, compile-only checks passed for `crv4`, `validator` and `mainnet`.
The two roots `TestSubstrateReadHttpDialChainRecoversInitialization` and
`TestLocateFinalizedExtrinsicPreservesContextAcrossScan` then passed normally
(1.012 seconds) and with race detection (1.092 seconds). The integrated head
remained unchanged, with only documentation edits present, and the resolved
module graph was byte-identical before and after. Those checks are retained in
`/mnt/data/sn-testnet/qualification/native-http-read-causes-20260928/integrated-790dacb0`.
They do not constitute a complete release/dependency attestation; unaffected
component bodies were not rerun.

| Retained input or result | SHA-256 |
| --- | --- |
| Source manifest | `c1d6d26966e3f0b387892cb18c5f5d37e15dce79409cd0f04379418a0a61660b` |
| Consumed local source manifest | `eac072dde6952d531a77ae721bf75e0e0371658bb99f1a6fe528b1ad173aa93e` |
| Qualification tools | `823ef97cf22108f73397a8b44fec431fbf590a87bbf8636672f7c5fef1142eb6` |
| Aggregate result | `52ee4dc23644386d51506e517793e44f333ef395b4eb200b20bbc59aad3df6ef` |
| Normal event stream | `78975a6d27023c635b13db531b07aaa55a5cd205c0fb5fc2d443f157248a09ae` |
| Race event stream | `74b9d246b9ff9c8797a1c904d6c422d3f379000b15b759b37b217d23514a407c` |
| Integrated two-root normal event stream | `5bf65165ad3f5c3b5d128288fe4cad9cc5b35637c53a1f05d5056e6491ef2b2c` |
| Integrated two-root race event stream | `90ce6e7094b8eecc2469f685b1e79663860e0556c97a3de92adaed09ca352b04` |
| Integrated resolved module graph, before/after | `e6502b79a35bbc61f3a9dd3cb7649865ca6bfc39c63573e1e73bf0331e302917` |

Integration review identified a finite-bound edge: the shared receipt reader
admits up to 16 MiB decoded `System.Events`, whose hexadecimal wire value plus
JSON-RPC envelope exceeds this component's 32 MiB response ceiling. A follow-up
admits that existing field bound with explicit envelope headroom and a
deterministic boundary test. That follow-up is now qualified in the separate
integration receipt. The original component evidence and its narrower bound
remain preserved here.
