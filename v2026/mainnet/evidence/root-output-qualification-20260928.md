# Root daemon output qualification — 2026-09-28

All **41 affected mainnet roots** have passing normal and race results, with
unchanged passes reused after two fixture corrections. All **six causal
families** reach their intended failure in both modes. Mainnet vet and the
offline check/test of all seven root-monitor alert rules pass. This is component
qualification, not a deployed observer, signing service or delivered alert.

Production source `0811d611888fa3acfd0027b8af4791e226467c2e` is integrated as
`eb5fff6c`. Test-only successors `f54885f9`, `5e4a8e98` and `d9cef74e` are
integrated as `1295d34a`, `00038979` and `dea9d8d8`. The final frozen positive
head is `d9cef74ee69170112a260b28ed754b30b4ed210c`. These successors do not
change production bytes. Root compared the integrated mainnet/diagnostic
production source with that head; differences in this scope are documentation
and the separately qualified protocol maximum-ID fixture.

## Result scope

The actual `root-monitor` command owns bounded stdout/stderr from admission
through joined cleanup. Optional log/metrics failure cannot stop finalized
observation or required checkpoint progress. Compact event v2 retains hashes,
position and closed facts without copying the entire root census into a bounded
log record. Finite `root-preview` keeps its complete v1 output. Metrics distinguish
current reads, retained finalized evidence, useful progress and previous output
acknowledgment. Missing/unconfigured metrics do not mean healthy delivery.

The private root-service `Run` consumes a concrete bounded exporter, joins it
on early return, preserves original action/signature/attempt custody, and returns
the original poisoned-journal cause even alongside cancellation. The tests
exercise actual service and command owners, full physical pipes, refused and
disconnected output, ambiguous metrics writes, changed path ownership,
read outages, restart, original receipt reconciliation and hard custody errors.
Read retry policy retains the 60-second minimum and 300-second default.

Raw roots:

- `/mnt/data/sn-testnet/evidence/mainnet-root-output-20260928`
- `/mnt/data/sn-testnet/evidence/mainnet-root-output-fixture-20260928`

| Capture | Normal and race result | Use |
| --- | --- | --- |
| Original `terra-positive` at `0811d611` | 40 pass, 1 fixture failure each | Retain original failed packages; reuse unchanged passing roots. |
| Corrected `terra-positive` at `d9cef74e` | Both selected roots pass; maintained matrix 4/4 | Completes refused-output fixture and rechecks the counted-formatting fixture. |
| Original control A | Two intended failures each; matrix 4/4 | Optional output may not stop service progress; refused metrics may not stop sampling. |
| Original control B | Metrics assertion reached, then formatting probe panicked; early-return root absent | Incomplete matrix. Reuse only the completed metrics assertion in each mode. |
| Original control C | Intended original-custody assertion each; matrix 4/4 | Hard cause cannot be replaced by a later poisoned-owner error. |
| Corrected control A at `f19c6d00` | Selected intended failure each; matrix 4/4 | Rechecks the updated refused-output fixture. |
| Corrected control B at `70e8afcf` | Both intended failures each; matrix 4/4 | Forbidden formatting and missing early-return cleanup. |

The first fixture expected a failed-write counter to increase for a sink
refused before any write. It now checks the actual unavailable outcome, one
dropped offer, zero deliveries, zero attempted-write failures and zero calls to
the refused writer. The second fixture deliberately panicked in `Error()`,
aborting unrelated causal checks. Its service test now counts forbidden calls
atomically and emits the same assertion after `Run`. The original sentinel is
retained for unchanged command fixtures. No production behavior was relaxed.

All seven maintained captures report `source_unchanged=true`. Final positive/A
pre-execution records were taken at **08:26:16 UTC**, after the final fixture
commits, and name clean `d9cef74e`/`f19c6d00` with their exact manifests. A stale
handoff initially caused coordination uncertainty; actual fences establish that
no source drift occurred during these runs. The valid captures were retained
without repetition. Superseded metadata and unused successor checkout preparation
are not additional qualification results.

The qualification owner is the retained maintained runner with SHA-256
`cf73edc6abe2ddf42c7dbe5aa3840bd12d3093a6349ea7904e0b19cb3ba70368`.
Plans retain source manifests, actual module/package paths, build identities,
root census and exact causal literals. Siblings resolve to the frozen
`server-model-final-20260927` family: Connect `c68689c4`, server `4468a696`,
SDK `42241118`, proxy `6204ae7d`, glog `892ade4a`, goidenticons `325750b3`, and
userwireguard `85fb1ca4`. SCTP and npipe are covered by their containing trees.
This is not qualification of the later composed server or registration source.

`terra-vet.exit`, `terra-prom-check.exit` and `terra-prom-test.exit` in the
original raw root are zero. Promtool used the existing pinned image
`prom/prometheus@sha256:63805ebb8d2b3920190daf1cb14a60871b16fd38bed42b857a3182bc621f4996`
with no network or pull. Astra authored/compiled the changes; Terra executed
the bodies and rules; root reviewed and integrated the evidence.

The [machine-readable summary](/mnt/data/sn-testnet/evidence/mainnet-root-output-20260928/qualification-summary.json)
retains report and body hashes, root membership and source heads. SHA-256:
`7999e59c53c10560cebbac77399c34a61b53aba18d2017a2c7d4b746322fc710`.

## Rollout and remaining work

Upgrade root-monitor log consumers for compact event v2 before its producer;
complete census consumers continue using finite preview v1. Supply distinct
approved metrics roles and paths, independent expected-source/stale-file alerts,
and verify actual collector ingestion and notification delivery. A refused
metrics path stays untouched and requires explicit repair/reopen. Required
checkpoint/custody failures remain hard. Synchronous filesystem operations are
bounded in size and joined; this change does not promise to interrupt a hung
filesystem.

See [root observer operations](../ROOT-VALIDATOR.md) and
[root service ownership](../ROOT-SERVICE.md). The trail-worker stdout path,
miner callbacks, complete release composition, live root custody/activation,
other monitoring domains and the repair controller remain separate work.
No live RPC, key, registration, signing, deployment or acceptance is claimed.
