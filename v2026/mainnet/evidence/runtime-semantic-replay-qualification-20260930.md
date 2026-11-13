# Finite runtime replay qualification — independently sealed

Independent qualification passes 48 Go root executions normal/race and 19 Rust
tests normally: 67 positive executions. All eight normal causal controls and four
selected Go race controls are causal. This qualifies the finite execution/custody
boundary only. No production runtime selection, signing route, live mainnet action
or public Safe route is enabled.

## Exact source and author checks

- Source: `861409348cf2ff9f84ffcd2262dd1961ab0b4b98`.
- Tree: `28294fa2ab8ac8d39bb1f81f096ea2b24a10c574`.
- Owner checkout: `/home/by/urnetwork/sn-runtime-semantic-replay-corrected-owner-20260930`.
- Static handoff: `/tmp/runtime-semantic-replay-corrected-handoff-20260930`.
- Eleven-entry `STATIC-SHA256SUMS` SHA-256:
  `0e2992eac6c1bb7b0643b0de38c55c3de3d53adc08916dd8b1f354518f0dc8a8`.
- `SOURCE-FENCE.json` SHA-256:
  `8dc8849aa3cb65667f4fb8d4a47b9b220e1a8540bbe31927a480832a455ea2c7`.
- Nine-entry control-payload seal SHA-256:
  `b6e08f3856ebe3f7e8f2d2656d6af5b872e9fd7adee0f2a19f0e8d35a3243745`.

The Astra author ran formatting, Go compile/vet and offline locked Rust
check/clippy with warnings denied. The author did not run behavioral tests.
Sol medium owns all behavior and causal-control qualification.

The fence records Go module files/graph, six clean local replacement commits and
trees, the unchanged independent Safe proof oracle, Cargo manifest/lock/toolchain,
and SDK `cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a` with tracked files clean.
Cargo's untracked `.cargo-ok` marker is recorded separately. No new registry
dependency version was selected; existing locked crates became direct dependencies.

## Sealed independent matrix

| Scope | Normal | Go race |
| --- | --- | --- |
| Eight new process/custody roots | Pass | Pass |
| Sixteen adjacent policy/runtime/library roots | Pass | Pass |
| Seven new actual SDK/Wasm replay roots | Pass | Not in scope |
| Twelve unchanged stateless-probe roots | Pass | Not in scope |
| Five Go causal controls | Five causal | Four selected causal |
| Three Rust causal controls | Three causal | Not in scope |

The positive census is 48 Go root executions plus 19 Rust tests. A Go race
result does not qualify Rust concurrency. All selected roots have exact run/pass
censuses, terminal package/lib results and retained raw streams. Expected bounded
host assertions are caught SDK execution errors inside passing Rust refusal tests;
they are not uncaught suite failures.

The final independent evidence directory is
`/home/by/urnetwork/temp/runtime-semantic-replay-corrected-validation-86140934`. Its manifest SHA-256 is
`6f8a99f52be1e889c35ceda4be3bb5184b6c1d8a253563957ec38be5d32627e3`; its complete `SHA256SUMS` SHA-256 is
`d6d10b83cec93f5ddec731a8c863118c1e93c53da0a081e7158082f716c97694`. The author independently checked every seal entry,
raw positive/control stream, source/dependency fence and restored checkout.
Author final audit `/tmp/runtime-semantic-replay-final-author-audit.json` SHA-256:
`5581cf7acd2a4d08b1264e9aa384deadfe8ee2780cc1e41a06560cf0b4ec25a4`.

The independent worker binary SHA-256 is
`fd8439a41bf95b76d5edf4920aa14c6f9631d1eb9656c2a3487609e2d0af9874`
(208072896 bytes). This records the qualified build; it is not a mainnet-approved
verifier pin. Full 38-entry author handoff `SHA256SUMS` SHA-256:
`8f4ec41dc210610214c29d8c5e40783450a37b0ebe450a10fcd891b6ae98c221`.

The selected Go race controls are exact signed evidence, separate case-data digest,
executed binary hash and refusal of semantic/selection overclaims. Original/candidate
authority binding is also mutated normally. Rust controls remove the original
artifact probe, candidate artifact probe, and both storage-effect comparisons.
Removing storage from both expected-state and cross-artifact digests ensures the
last control tests the complete boundary rather than triggering a redundant gate.

## Retained preliminary source

`7e172a96aeeaabf4962610e2f973db5a0fc1c406` / tree
`d4dad27bba3736b119bbebded0473572d75bfc4b` passed seven focused Go roots normally.
Its immutable log at
`/home/by/urnetwork/temp/runtime-semantic-replay-validation-7e172a96/go-focused-normal.log`
is hashed in the corrected fence and is preliminary only. Review then identified
the missing nil-context refusal and conflated semantic-rules/case-data digests.
The corrected source adds the nil regression, distinct case identity and an explicit
false `semantic_rules_verified` result. Final qualification reruns exact corrected
source; the preliminary pass is not substituted into it.

## Scope and remaining gates

The Rust tests execute synthetic Wasm through the actual pinned SDK; Go tests use
an explicit synthetic worker to exercise signature/custody, process output and
cancellation. These are separate component fixtures, not a composed mainnet run.
The worker compares exact return bytes and complete declared top-level fixture
state, including insertions/deletions, in on-chain context. The existing metadata
probe keeps its stateless host set.

The independently approved semantic-rules digest remains a referenced identity.
Exact case-data bytes have a separate digest bound by signed evidence. Finite
execution does not prove implementation or coverage of those semantic rules,
authenticate real mainnet state, or establish universal economic equivalence.
Reports keep semantic-rules verification, complete equivalence and selection false.

Real approved source/build/state evidence, all-domain rules and semantic proof,
needed host semantics, changed-economic controls, durable selection/custody and
immediate signing re-admission remain P0. Standard validator/miner and other role
integration remain open. MG-04/RT-04, Safe history and activation remain open.
