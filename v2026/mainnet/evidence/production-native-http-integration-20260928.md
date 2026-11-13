# Production native HTTP integration — 2026-09-28

Status: **component qualification complete** on clean author
`a0249f6a4d5b154e5a6a242511fac3596dcde62c`, integrated as `05b88228`.
Astra authored source and fixtures; Terra executed normal/race and controls.
No live endpoint, key, submission, deployment, simulator or release lock was
touched. Complete public startup remains a separate requirement.

## Prerequisites and narrow changes

This slice follows continuation `89ebd498` and its receiver-only follow-up
`18ff77cb`. Native adapter `ec2bc584` was imported as `f35a3ab2`; the parent reports
Terra's original component normal/race and ten intended causal controls passed,
then integration as `790dacb0`. Those frozen candidates were not edited.

Production phase classification now consumes the configured native client's
strict typed origin. A separate presence query prevents generic URL/EOF
unwrapping from overriding the native owner's hard verdict on a mixed or local
custody error. Text, unowned EOF and permanent protocol/application errors do not
gain read authority.

The 32 MiB transport ceiling omitted JSON framing around a previously admitted
16 MiB event value, which becomes 32 MiB plus `0x` on wire. The finite ceiling now
derives from that same event bound plus 64 KiB of framing. This changes transport
admission only, without increasing the event decoder's bound or approving event
semantics. An oversized declared response remains hard.

The original adapter treated every HTTP response-body close failure as hard.
That was unnecessary for a pure typed connection timeout, EOF or truncated close
from an allowlisted read. The response is released exactly once, idle connections
are discarded, and the attempt context is canceled before another request. Only
the complete transient error tree may retry. A local file close, explicit
cancellation, unknown release defect or any joined integrity cause remains hard.
Writes and unknown RPC methods still do not enter this read transport.

## Deterministic qualification

Use physical `GOCACHE=/mnt/data/sn-testnet/gocache`, a capture-specific physical
`TMPDIR`, `GOWORK=off` and `GOMAXPROCS=2`. Enumerate roots before normal/race bodies.

- `go test ./crv4 -run '^TestSubstrateReadHttp' -count=1`
- `go test ./validator -run '^TestProductionSteering(NativeHttp|ReadCauseClassification|ReadBudget|ReadRejects|Loop)' -count=1`
- The same selectors with `-race`; `go vet ./crv4 ./validator`.

The actual configured native constructor reads synthetic metadata, genesis and
runtime over local HTTP. A later response reaches an explicit body barrier before
the existing phase clock advances its requested 60-second deadline. Its genuine
physical error returns through GSRPC; the 300-second owner exhausts while the
caller stays live. The resulting receipt wait preserves original epoch/hash,
remains visible and does not spend the loop's hard-error budget. Mixed integrity
and a complete malformed response remain hard. Clock hooks change no production
budget or public API, and no injected callback grants an RPC success verdict.

The close fixture decorates only the physical RoundTripper beneath the real
configured adapter. It checks one close per response, idle discard before reuse,
the original read budget and recovery from EOF, unexpected EOF and typed network
timeout. File, canceled and mixed close causes are negative controls. A maximum
event hex field goes through the actual configured HTTP client; its decoder's
event semantics are intentionally outside this transport test.

Required causal controls restore the previous 32 MiB limit, previous hard-for-all
HTTP-close verdict, and rejection of native origin at the phase boundary. Each
must reach its named behavior assertion, not fail compilation or setup. Preserve
normal/race logs and source fences separately from prerequisite qualification.

Terra completed the exact frozen scopes in
`/mnt/data/sn-testnet/qualification/production-native-http-integration-20260928/a0249f6a`.

| Scope | Normal | Race |
| --- | --- | --- |
| CRv4 HTTP, 16 roots | 16 passed, 2.390 s | 16 passed, 19.807 s |
| Validator composition, 7 roots | 7 passed, 0.033 s | 7 passed, 1.183 s |
| Three controls | Three intended assertion failures | Three intended assertion failures |

The first two controls restore predecessor decisions. The phase-origin refusal
is an explicit bounded mutation testing composition sensitivity; it is not
claimed as an exact predecessor implementation. All six captures exited 1 at
their named behavior assertion. Vet passed. Before/after source records retain
the same clean author head; the resolved module graph is byte-identical. These
records do not constitute a whole-release source/dependency attestation.

Root integrated the required continuation fixture/core/style stack and reused
its already integrated native adapter. The corresponding patch IDs matched;
no duplicate producer, source-role fixture or native-adapter commit was applied.
At `05b88228`, all Go files in `crv4`, `validator` and `protocol` match author
`a0249f6a` exactly. Documentation conflicts retain both the newer root findings
and this qualification.

The narrow installer/root-service composition then passed on actual source
`fef30e5fd59d07cbca806b49d3826dcc40eb52c0`, which changes only documentation
after `05b88228`. Compile-only checks passed for `crv4`, `validator` and
`mainnet`. The exact roots `TestEvmCreateCommandExecutesReviewedReserveAndResumes`
and `TestRootSubmissionOfflineCustodyServiceComposition` passed normally in
1.681 seconds and with race detection in 11.212 seconds. The worktree remained
at the same clean source and its resolved module graph was unchanged. Evidence
is under `/mnt/data/sn-testnet/qualification/continuation-composed-05b88228`;
that directory label names the code integration, not the actual execution head.
The component bodies above were not repeated.

| Retained file | SHA-256 |
| --- | --- |
| `crv4-normal.jsonl` | `09549f29d04d1ea2ce39dae4328556aecaddd878dd1df14148614d4ff0d0b8a9` |
| `crv4-race.jsonl` | `7585014c516615d1a721976890a58ba6a429079320de93dc53659ace468e19a0` |
| `validator-normal.jsonl` | `9e4f76a4759b9e98cda17a048344779b259ab5cf427cc043877f3c497d0a8298` |
| `validator-race.jsonl` | `f081827cc342acc0d8f62fe97156fb07297dab2b60cb8d628fce0aa80fafe71e` |
| Resolved module graph | `bcc684d0d13654b9e488eefbfa745ff71841f113e6464d9ed7fa3910636a21bb` |
| `causal/controls.tsv` | `2fe7127de503d710ca59ad5efa66fb0beac41b7073efa3e17d4007422bbf1d1c` |
| `causal/summary.txt` | `66b353235eb1c64a8958f753582541429f3e72297b9be05c7d198bae2e6112fd` |
| Composed two-root normal stream | `9bdd3a08656d9a494a2b570c50345a5160f4d34f11e1fed41c7b55478be78100` |
| Composed two-root race stream | `50c18035c60c1aa644dedb5852b8dec92e96af88e214c8bcc0f946a5df5ceb2e` |
| Composed resolved module graph | `e6502b79a35bbc61f3a9dd3cb7649865ca6bfc39c63573e1e73bf0331e302917` |

## Limits and naming-only changes

This qualifies physical native deadline composition; the original component
separately covers full status/EOF budget exhaustion using its own deterministic
clock. It does not claim a 300-second wall-clock HTTP500 outage in the validator.
Full real `RunRelease` activation/config/dual-upload composition, bounded durable
receipt prefixes, the historical-only renewal foreign-nonce path and miner
partial-range checkpoints remain open. No new signing or epoch authority is
created.

The prior continuation test's eleven positional case literals now name the
same `name`, `err` and `want` fields. Values, ordering and assertions are unchanged.
The receipt-method receiver rename was already isolated in `18ff77cb`; neither
naming-only edit is a new behavioral qualification claim.
