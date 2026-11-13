# Original-authority contract installation anchor

This increment closes a local MG-08 installation gap: original counted anchor
recovery now requires its exact coordinator binding event, and a fresh typed
producer joins the original eight CREATE/link records and anchor receipt to
complete initial contract/Safe storage proofs. It does not establish a live
installation or approve service activation.

## Frozen source and dependency graph

The six-file code/test commit is
`a5d1e98048b5159d91451d95f35f9862eac264d9`, tree
`d5550629452357f57dacc8d684decb880330daea`, based on
`9e913cf84e7d3516a5408693746682c4adf28a7e`. This note and the accompanying operator
documentation are separate from that frozen source tree. No
`validator_activation*` source was changed by this increment.

Final author qualification uses the external modfile
`/mnt/data/sn-testnet/mainnet-contract-anchor-astra-20261001/author.go.mod`
(SHA256 `3645dfaccfbb85171eb8f833d7ed989ce42ebbbfd912e8bd787317cf45a72be5`).
It resolves the clean integrated server
`0b8e758db9ce5516de867e1b5d0a1c9660a0880b`, tree
`500391d3919ff8d3301455d7c735aa91cb74af5c`, through the preserved physical source
checkout. Effective Connect/SCTP
`v0.0.0-20261001021459-e1b5d77b5029` and SDK
`v0.0.0-20261001021058-5d37be3876e5` remain unchanged. The local GSRPC fork and
npipe replacements resolve inside the frozen SN source. `evidence/source.json`
records every effective replacement and the checked-in/external module hashes;
`evidence/modules.json` retains the complete Go module graph. The toolchain is
Go 1.26.6 on linux/amd64.

## Production behavior and causal coverage

The existing durable successor action still owns the exact signed, zero-value
Safe CALL, original preparation and eight creation/link journals, counted
attempt, nonce floors, budget, canonical/runtime approvals and public-policy
acceptance. No alternative signing or send path was introduced. Its receipt
classifier now requires exactly one `ValidatorEvidenceFixed(address)` event
from the exact coordinator for the retained evidence address, before the exact
Safe `ExecutionSuccess` event. An outer success or current getter cannot stand
in for that binding event.

`contract-successor-execution-readback` requires `--online` and rejects
`--submit`. Public current-only use additionally requires the exact independently
signed retained v2 policy hash. It only reads the network, but may reconcile the
original terminal local journal after an interrupted reply/publication. The
original signatures, counted attempt and CREATE/link journals remain intact;
recovery does not resend the anchor.

At the anchor's first native/EVM inclusion, readback checks complete authenticated
storage prefixes, code and metadata for all five contracts and the Safe. The
contract profile fixes reviewed storage layouts, original policy activation
height, initial accounting and evidence binding. Hidden mapping entries,
unresolved proof branches, changed domains or runtime metadata fail closed. The
proof's historical runtime must occur in the original terminal event's retained
authority prefix; a later approval cannot be backdated into that event. This is
a finite initial-storage profile: operator/service population must follow the
anchor, including within its EVM block.

At one current finalized native/EVM mapping, readback separately proves complete
Safe storage and checks the five contracts' executable/domain views. Those
current views allow ordinary later accounting. A later proved Safe nonce is an
observation, not approval of its intervening operations or another send. Final
mapping, ancestor continuity and original live custody checks close the result.

`inspectBootstrapContractInstallationAt` lets a later service consumer select
the same finalized boundary as its other checks. The stable
`installation_identity_hash` binds original authority, eight original custody
seals and the exact terminal journal event/receipt, excluding the moving
snapshot. Its `anchor_event_hash` field means the terminal **local journal** seal;
the EVM binding event is authenticated within `anchor_receipt`. The separate
report `content_hash` includes the observation boundary. Neither JSON nor a
saved report is a capability; a consumer must freshly execute the producer under
its independently approved inputs.

The four new top-level roots exercise:

- Exact anchor-event admission, including missing/foreign/malformed/duplicate or
  late events, and an unrelated same-digest Safe event that must stay admissible.
- Full original-graph public-v2 readback after one lost anchor reply, recovery
  without resend, original byte preservation, stable identity after the head and
  Safe nonce advance, and original custody mutation after expensive proof reads.
- Independent native-trie proofs built from real local EVM execution for all
  five complete prefixes, with hidden evidence/operator state, altered binding,
  initial policy clock, code and metadata controls.
- Interrupted terminal-journal publication: a missing binding event prevents
  recovery; the exact receipt finishes the same counted attempt without resend.

## Qualification

Evidence root:
`/mnt/data/sn-testnet/mainnet-contract-anchor-astra-20261001/`.
The sealed `receipt.json` has SHA256
`4b142e4eda0a4c10ab03a2fa44b72c372911d690c8744b65a3dc5c2280b90f4c`.
Its 56-entry `SHA256SUMS` manifest has SHA256
`7ed86afacbbb1dd5c2a9727eeb35557430ead873412c022a2f7a97cbad11ca1d`;
all 56 entries passed checksum readback. `evidence/qualification-audit.json`
audits exact root execution, package outcome, zero skips, the intended causal
assertions and both exact source graphs. It records 50 positive root executions
and six intended causal-control failures, without counting diagnostic streams.
The exact 35-root selected scope is retained in `evidence/source.json`. It
includes the four new roots, all successor-execution roots, real canonical and
public-policy command paths, and original receipt/current/role-graph admission.
All test commands use `-mod=readonly`, the external modfile above, bounded
parallelism and `/mnt/data` cache/scratch.

| Check | Result |
| --- | --- |
| Author selected normal | 35/35 roots and package PASS; 439.356 seconds |
| Author 35-root race | Deliberately interrupted after independent race qualification; diagnostic only |
| Author `go vet ./mainnet` | Exit 0 |
| Causal omission: mandatory anchor event | Expected assertion failure in normal and race |
| Causal omission: complete contract prefix count | Expected assertion failure in normal and race |
| Causal omission: final original-custody check | Expected assertion failure in normal and race |
| Independent Sol normal | 9/9 roots and package PASS; 205.104 seconds |
| Independent Sol new race | 4/4 roots and package PASS; 739.334 seconds |
| Independent Sol adjacent race | Original-receipt and current-state roots/package PASS; 576.569 seconds |
| Independent Sol vet | Exit 0; static review found no blocker |

Each causal control removes only its named production check in a disposable
checkout of the frozen commit, leaving the tests and resolved integrated graph
unchanged. Patches, exact exit status and raw Go JSON streams are retained.

The independent receipt is
`/mnt/data/sn-testnet/sol-contract-anchor-independent-20261001/receipt.json`,
SHA256 `45f86e6c6e96685662392df9aee0e28e0949b658d08ce1f2064fb8e0fcacdb49`.
Its external `active.go.mod` has SHA256
`d5242680793bd81005dbc3dfc6c2e1ecb6c5104922a230b71b0cb5a0055267d1` and resolves
the same clean SN/server graph. Its `SHA256SUMS` manifest
(`df03c49c5e22a19061e6d0963a1ab5b04604960014e5acb2d0c38e862a1c3a1e`)
passed author readback of all 17 entries. An unchanged copy is retained under
`evidence/sol-independent/` in the author evidence bundle.

The author 35-root race run was stopped with exit 143 after the independent
new/critical-adjacent race packages passed and the parent accepted the integration
gate. Its incomplete stream is retained as `evidence/author-35-race-interrupted.*`,
never as package PASS. The receipt claims the six completed independent race
roots; it does not claim a 35-root race sweep. Later server `720e7c61` is outside
this frozen graph and requires separate integration qualification.

The earlier author diagnostic streams used the legacy server checkout
`898dc8f3b211d1e2fca1b0a0c970f7673b36fd7b`. They are preserved explicitly as
superseded diagnostics and supply no integrated-graph qualification. The selected
legacy run was deliberately interrupted when the mismatch was found. Pre-freeze
failures also remain: an unused-import compile failure, followed by a fixture
failure after replacing its native proof root without rekeying the fixture's
historical mapping storage. The fixture now commits the complete proof root and
matching native storage before any observation; no production integrity check
was weakened. Independent Sol qualification uses the integrated server graph.

## Remaining gates

Under public current-only v2, complete Safe history stays false. Complete
contract execution history, complete pending state, activation readiness and
network-effects claims also stay false. Complete storage at one authenticated
root does not prove all intervening execution; native finality still relies on
the signed owned route. Current-only risk-policy choice and the actual
independent signature remain external requirements. Exact original custody,
current service/operator/validator admission and real installation still need
their separately approved production evidence.

No live transaction was signed or submitted, no image was published and no
service was touched. The prior release built from `095a2208` does not cover this
new source: a successor release and its independent provenance/approval remain
required. This local source qualification cannot be inherited as release,
deployment or activation approval.
