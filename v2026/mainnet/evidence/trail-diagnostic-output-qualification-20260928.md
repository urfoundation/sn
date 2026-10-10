# Validator trail output qualification

The actual trail worker now keeps optional diagnostic output independent of
durable trail progress and cancellation. Source
`83fd96a68665a4ac0036a5442e1a5a738b8b6626` and its fixture-only correction
`4e8abc9e79f2f16d48b7eb4806bf4a5fae0fc1c2` are integrated as `0644206f`
and `76a6a7f9`. Astra authored the changes; Terra executed the test bodies.
Root reviewed the source, exact terminal results and retained source manifests.

All **17 affected roots have passing normal and race coverage**: 16 unchanged
roots passed in the original capture, and the single corrected root passed in
its separate capture. Five causal controls reached their exact intended
assertions in both modes. This is component qualification, not live protocol
acceptance, deployed logging or a completed mainnet release.

## Retained results

The original positive capture remains failed: normal and race each have
16 passes and one failure in
`TestTrailDiagnosticsHardCustodyFailureStillStopsRun`. That fixture required a
clean ledger close after deliberately injecting a durable append failure. The
ledger correctly retained its fault. The correction demands that exact original
cause, rejects additional joined errors, and verifies stable repeated close.
Projection failure and all other new fixtures still require a clean ledger
close. Production bytes and the other selected test bodies are unchanged.

| Scope | Normal | Race | Maintained report |
| --- | --- | --- | --- |
| Original 17 positive roots | 16 pass, 1 fail; 1.677 s | 16 pass, 1 fail; 4.168 s | Failed, retained |
| Corrected cleanup root | Pass; 0.612 s | Pass; 1.977 s | 4/4 stages pass |
| A: synchronous completion output | Intended failure; 45.044 s | Intended failure; 45.174 s | 4/4 stages pass |
| B: worker identity and child lifetime | Both intended failures; 0.123 s | Both intended failures; 0.410 s | 4/4 stages pass |
| C: arbitrary formatting and epoch-zero knownness | Both intended failures; 0.044 s | Both intended failures; 0.196 s | 4/4 stages pass |

Every body was joined. Positive failures and all causal bodies exited 1; the
corrected positive bodies exited 0. All five reports record
`source_unchanged=true`. The original candidate's compile-only and validator vet
checks passed. No alert rule or wire format changed, so no alert-rule repeat was
needed. The corrected scope used one worker alongside the independent server
and causal checks; the author plan's two-worker limit was narrowed without
changing source, roots or assertions.

The physical full-pipe regression fills stdout before starting its child. Trail
completion, its next picker, cancellation and joining must finish before the
parent drains the pipe. The old synchronous-output control instead reaches the
45-second deadlock backstop, where the parent kills and joins its owned child
and reports the expected failure. The other controls fail by assertions rather
than aborting sibling tests with a panic.

## Source and evidence

The raw directories are
`/mnt/data/sn-testnet/evidence/mainnet-trail-output-20260928` and
`/mnt/data/sn-testnet/evidence/mainnet-trail-output-fixture-20260928`.
Their handoffs, exact plans, root lists, failure literals, command/result files,
body output, resolved module graphs and before/after content manifests remain
retained. `qualification-summary.json` in the first directory has SHA-256
`cc2968de6dbfd56661a04680d820f56b2d8f1645c8f6c63ae4001bd4b5231571`.

| Capture `report.json` | SHA-256 |
| --- | --- |
| Original `terra-positive` | `56af352577db8d49843ddc8a14314bce1e384157a16399f2b9d5388a7446758a` |
| `terra-control-a` | `9f7faa36439582c4c7f0147cc85042c66442e00f34019480020c044c9c078ba2` |
| `terra-control-b` | `c7b3e204774c3cc2c6ba6996288641c374b2ef385edac6cac01cde0c5cbe8644` |
| `terra-control-c` | `eeae22f28e11333e5b73c6a7f555be0df4c0daa4b676492f35acd544dca52834` |
| Corrected `terra-positive` | `948bb1c9417bd88a7011b75f44563731a3be8f4de89c9fdc63a13fbd86a0495f` |

The maintained runner hash is
`cf73edc6abe2ddf42c7dbe5aa3840bd12d3093a6349ea7904e0b19cb3ba70368`.
Both candidate generations consume the same physical frozen dependencies:
Connect/SCTP `c68689c4`, server `4468a696`, SDK `42241118`, proxy `6204ae7d`,
glog `892ade4a`, goidenticons `325750b3` and userwireguard `85fb1ca4`.
Complete identities and content hashes are in the retained manifests. Control
commits `1b0e3482`, `dd775e95` and `51f456e5` are evidence only and were not
integrated. This graph does not include the pending new registration components.

## Operational scope

Completion, failure and proof-signature notices use closed scalar facts in the
existing bounded runtime domain, with explicit operator and epoch knownness.
Epoch zero remains a valid known epoch. No raw error text, peer identity or
signature is formatted. Optional output loss does not soften required ledger or
proof-store failures, and local proof completion does not establish native
weights, settlement or independent acceptance.

The existing progress-consumer rollout still applies; this slice adds no new
producer schema or domain. Registration's precreated child lifetime has a
tested diagnostic-copy seam, but its composed public startup needs separate
qualification. Miner callbacks, SDK/internal logging, delivered alerts and the
repair controller remain outside this result.
