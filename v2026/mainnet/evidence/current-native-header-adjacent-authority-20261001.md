# Adjacent native header authority and retained evidence

The composed implementation is frozen at SN
`30354d7837e3ed08ca6c5a9e2174443c699271f3`, tree
`3cf03d831b7f8b651b0cb0b03c1ec3151fc26f5c`, on main `f9964504`, following the first
[current-header increment](current-native-header-authority-20261001.md) at
`889f5c29`. The earlier receipt remains evidence of its stated scope. Independent
review then reproduced an adjacent upload-admission bypass, so that first
increment alone was insufficient to close this class of authority selectors.
The intermediate `12df174f` selector fix is retained as historical evidence;
its separate historical finality-closing gap is corrected in `30354d78`.

## Causal failure and source scope

A complete synthetic block committed height 250. The Rpc substituted height 150
and consistent same-height canonical answers. The producer gate at `889f5c29`
rejected the changed header, but upload admission selected its signed 101–200
window and returned a successful current native observer. Both normal and race
independent controls reproduced that result. Valid approved digest-tag-8 headers
already worked; this was a commitment-authentication failure, not a demonstrated
digest-decoder availability failure.

The next independent control held historical block 100 canonical while changing
the mapping for its finalized witness at block 150 during runtime reads. The
intermediate implementation closed block 100 alone and bound the historical
view. The final correction also closes the original witness before publishing
the artifact. Cold and warm failures, and cancellation at that closing read,
leave the previous view intact. Stable historical authority still works and
remains read-only; this control demonstrated no current-signing escalation.

The successor uses the existing bounded complete SCALE-header authenticator
before coordinates select authority or retained evidence. Opening and closing
canonical checks surround the dependent reads where needed. The audited scope is:

- Upload freshness and independently signed current/historical observation
  windows, including owner-recycle census approval.
- Startup validator stake/identity and both validator observations plus the
  original owner-recycle activation header.
- Fresh activation preparation, ordinary and V2 applied-weight journals, and
  production retained continuation's final applied-row observation.
- CRV4 historical identity/stake and native/EVM first-insertion checkpoints;
  parent execution and inclusion post-state remain separate.
- Schedule, finalized account nonce, finalized weights and fleet commitment
  evidence. Failed reads return no partial observation.
- Watched native receipts, whose returned number now comes from the verified
  complete body instead of a separate convenience header. A local websocket
  control demonstrates a committed block 5 being mislabeled as 99 by old code.

The exact signed config windows, current versus historical binding purpose,
original transaction/source/sidecar bytes and producer capability checks are
unchanged. A failed application observation preserves the original journal and
subsequent valid observation finishes the same work without another broadcast.
No module version or live authority changes are included.

## Qualification

The final author graph retains clean server
`24ac67d41449c7a87aedc3949221bfb60ae735f8`, Connect `e1b5d77b5029`, SDK
`5d37be3876e5`, and the checked-in SN forks through an exact external modfile.
The source composition cherry-picks the initial and adjacent corrections onto
main `f9964504` without changing the contract-installation source owned by that
main. All 211 selected normal roots pass. The final bounded race selection
covers all 19 new roots and adjacent original-authority, upload, producer and
receipt recovery paths: all 38 roots pass. Four-package vet and the read-only
full `go build ./...` also pass. Removing only the new finality-closing check
reproduces acceptance of the changed witness under normal and race; the fixed
source rejects it while preserving the original historical purpose.

The final author evidence is
`/mnt/data/sn-testnet/mainnet-adjacent-header-composed-astra-20261001/receipt.json`,
SHA256 `0b0619045d7a75bb260856d6c6e6d1819eadc0bb6a3bdb966360fa78d288c863`.
Its 56-entry `SHA256SUMS` is
`bdd17561d3b3fbf7782cb69bce5607467fbad162a49fa8d3d391918ebcb4c1ec`;
all entries passed readback. The receipt records exact source/dependency fences,
external modfile, named selections, commands and terminal logs.

The historical `12df174f` / server `94229abb` graph passed 209 selected roots
under normal and race, four-package vet, and all 14 selected old-file causal
omissions failed at their intended assertions in both modes. Its receipt is
explicitly superseded for the separate production historical-finality gap:
`/mnt/data/sn-testnet/mainnet-adjacent-header-authority-astra-20261001/receipt.json`,
SHA256 `e1af5c87df83b5b815d6b0b75077e26e51f8883fcb02fd0ee4ce9d0f6f68e984`.
The 75-entry `SHA256SUMS` is
`326eea82a2274c43f82a441036ef995edae6a77ae58d8b64b8d5a55f160449ff`;
all entries passed readback. The initial 198-root receipt and this intermediate
receipt are not relabeled as covering the final composed source.

Independent Sol qualification of exact `30354d78` / server `24ac67d4` passes 34
normal roots, 15 selected race roots, three-package vet and its original
historical-finality reproducer. Separate controls reject short, null and zero
canonical hashes and cancellation immediately after a correct response.
The report and exact graph/commands are retained under
`/mnt/data/sn-testnet/sol-runtime-header-composed/`; `report.txt` SHA256 is
`3b19462cead19513f06453c3389661f3160e16ae01da9781cab17b6d5870a53a`.

Diagnostic runs retain the original minimal-header/typed-result fixture
failures, exact-call-count corrections and incomplete synthetic event-metadata
setup. These are not substituted for the final positive package outcomes.
Qualification uses generated approvals, local private histories and synthetic
Rpc transports. It supplies no live source/runtime approval or service acceptance.

## Claim-recovery follow-up and release gates

A separate static audit found `miner/claim_daemon.go:461` reading only the native
finalized header number. Callers around lines 532 and 589 use that number for EVM
contract calls; line 826 compares it with an EVM receipt number. With native
finality 100 and EVM finality 70, a receipt at EVM 90 can satisfy that numeric
comparison without an authenticated native/EVM mapping. This is a concrete
source-level defect requiring its own causal recovery tests and correction;
no live exploit or claim outcome was tested here. The separately qualified
[claim successor at `6dcb94a1`](miner-claim-evm-finality-20261001.md) now corrects
those paths and fresh claim publication. Its shared onchain helper's other
callers remain an explicit follow-up; MG-04/PH-04 remain open.

Generic external consumers of the compatibility `HeaderAtContext` projection,
automatic semantic successor approval, both deployed validator roles, current
signed policies and controlled live upgrade/restart acceptance also remain
separate gates. The complete-header readers authenticate commitments under the
approved Rpc's canonical/finality assertions; they do not independently prove
native state or finality.

The code is later than release source SN `233ea2be` / server `94229abb`. A fresh
exact composed source/image release and independent release/deployment approval
are required. Earlier reproducibility and image attestations cannot be inherited.
Server main subsequently advanced to `6c39d307`; this author graph remains pinned
to `24ac67d4`. A separate narrow independent compatibility gate for unchanged
SN `30354d78` / server `6c39d307` passes eight normal roots, four race roots,
three-package build and vet. Its report is
`/mnt/data/sn-testnet/sol-runtime-header-server-main-6c39/report.txt`, SHA256
`63538723b3590f467fe2deab68a1acd4521b4825dd3335e50a83a98e206c0377`.
That gate supplies source compatibility evidence, not qualification of new
server monitoring behavior or a successor image/release attestation.
No live signing, chain submission, service start or publication occurred.
