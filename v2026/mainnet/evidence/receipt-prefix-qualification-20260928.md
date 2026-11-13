# Native receipt-prefix qualification — 2026-09-28

All **56 affected roots** have passing normal and race results across the
original source and its two fixture corrections. All **nine causal control
families** reached their intended assertion in both modes. This is component
qualification; it does not establish a composed mainnet release or live outcome.

Production source is `22aca4ccf6ad8671baf2117dd06fc98e3969a2b6`, integrated as
`a2779277`. Fixture corrections are `093272b62879928ffdc198d3709f016f6bb92cdf`
(composed as `06da5cbe`, integrated as `ff890817`) and
`2fcdfa3d71210511773860b5a8ac6282366e51ef` (integrated as `40b7d19a`). Neither
fixture correction changes production behavior or expands an already signed
approval. The [source design](receipt-prefix-candidate-20260928.md) and
[fixture explanation](receipt-prefix-fixture-correction-20260928.md) describe
the scope. Terra executed the bodies; Astra authored the changes and root
reviewed the captures and physical source comparisons.

## Results and retained failures

Raw evidence root:
`/mnt/data/sn-testnet/qualification/receipt-prefix-20260928`.
Durations below are Go package durations in seconds, normal / race.

| Scope | Normal / race result | Duration | Raw directory |
| --- | --- | --- | --- |
| Shared CRv4 complete-body/chunk readers | 15 pass / 15 pass | 0.017 / 1.136 | `terra-22aca4cc` |
| Miner command/store recovery | 23 pass / 23 pass | 23.873 / 212.218 | `terra-22aca4cc` |
| Original validator scope | 13 pass, 5 fail / same | 178.234 / 1020.924 | `terra-22aca4cc` |
| Five affected fixture roots at `093272b6` | 4 pass, 1 fail / same | 50.938 / 386.458 | `terra-093272b6` |
| Remaining uncached-read fixture at `2fcdfa3d` | 1 pass / 1 pass | 2.367 / 14.723 | `terra-2fc-frozen` |

The first four fixture failures lacked the complete 56-byte Subtensor account
row required by same-boundary nonce reconciliation. The long recovery fixture
approved reads only through block 200 but placed its receipt at 230. Corrected
fixtures supply the actual row at the authenticated boundary and sign the
longer read window before creating the intent. The remaining timeout fixture
injected its error into an already cached body. It now advances to uncached
block 102, checks the error there, and proves bodies 100/101 were not repeated.
The failed original packages remain failed records, not rewritten passes.

The nine controls cover miner prefix retention, validator restart reuse,
historical-only foreign nonce resolution, cache signature verification, scanner
input binding, canonical parent continuity, interrupted partial chunks, and
optional cache read/write degradation. Each selected root failed at the exact
expected assertion with its causal change; there was no substituted build
failure, panic or race report. Validator prefix reuse was checked on the
corrected frozen `2fcdfa3d` fixture: both modes report `durable restart repeated
completed body 100: 2 reads`. Its package durations were 22.573 / 179.759 seconds.
All controls remain outside production source. Vet of `./crv4 ./miner
./validator` exited zero in `terra-2fc-frozen/vet.exit`.

## Source provenance and limits

The three frozen SN checkouts resolve to the same physical sibling family at
`/mnt/data/sn-testnet/worktrees/server-model-final-20260927`: Connect
`c68689c4`, server `4468a696`, SDK `42241118`, proxy `6204ae7d`, glog `892ade4a`,
goidenticons `325750b3`, and userwireguard `85fb1ca4`. SCTP is covered by the
Connect tree; npipe is covered by SN. Exact full hashes and paths are retained
in the raw module/physical manifests. This family is not the later composed
server release at `936c3d95`.

Original `22aca4cc` and corrected `2fcdfa3d` have actual pre-execution content
manifests for all eight physical roots. Post-run comparison found every tracked
file unchanged, all trees clean at their expected heads, and all three module
graphs byte-identical to their captures. The `093272b6` module/content capture
was taken during its normal execution, not before it; its pre-execution SN
content seal is absent. The later comparison does not retroactively supply
that proof. Final release composition remains required.

The first direct `timeout2fc` execution resolved to the active server/Connect
family rather than the frozen family. Its passing bodies are diagnostic only;
the `terra-2fc-frozen` execution supplies the intended scoped result. The first
validator-prefix control used the old fixture and failed before its intended
assertion; it is non-discriminating and retained in `terra-22aca4cc`. Only the
later `validator-prefix-corrected` result is counted. The first `093272b6`
wrapper exited after its normal package failed, so it omitted its normal exit
sidecar; the terminal Go JSON failure remains, and race ran separately.

The machine-readable [result](/mnt/data/sn-testnet/qualification/receipt-prefix-20260928/qualification-result.json)
contains root membership, raw-log hashes and exact causal lines. SHA-256:
`3c8a163e55bafc97300147540c6eeabccc3928ece9da969c13963d9bdc5b9ea3`.
The [post-run comparison](/mnt/data/sn-testnet/qualification/receipt-prefix-20260928/post-run-seals/result.json)
has SHA-256 `b4107155410cf314ccecb7bdcfe39f416aeb5fc2a776291485a0f6f0dec78b9d`.

The separately [qualified diagnostic owner](bounded-diagnostic-output-qualification-20260928.md)
now exposes optional-cache degradation through the public validator lifecycle.
This closes the bounded-prefix and historical-only nonce component gaps;
first-client startup recovery, final release composition, live authority and
deployment still keep MG-04/PH-03 incomplete. No live RPC, signing, deployment,
native payment, testnet acceptance or mainnet acceptance is claimed here.
