# Shared EVM receipt finality closure

The shared helper correction is frozen at SN
`222e45a821a59e8e9027370dadf17afd05cdee0b`, tree
`dca6803c80590789fb278e692316eed09d7892d0`, based on main `61bb0469`.
Its author graph retains server `6c39d3079700c779e026a8e0244775abb7e458b4`,
Connect `e1b5d77b5029`, SDK `5d37be3876e5` and the checked-in SN forks.
The preceding [claim correction](miner-claim-evm-finality-20261001.md) and its
independent evidence remain separate and unchanged.

## Failure and production behavior

The shared `miner/onchain/eth.go` `waitFinalized` helper previously accepted a
canonical receipt after one opening EVM finalized-tag read. A synthetic receipt
at 90 could complete from an opening tag at 95 even after the actual finalized
tag regressed to 70. A failed canonical block read also returned immediately,
stranding an observation that could recover within the existing deadline.

Each attempt now reads the actual closing finalized tag, authenticates the
canonical hashes of its current and original finalized witnesses, and rechecks
the exact canonical inclusion block. Ordinary advancement from 95 to 110
retains receipt 90. A regressed frontier remains pending and may recover on a
later complete observation; it does not establish a reorganization.

Transient network/server errors, HTTP 429/5xx, end-of-stream errors and null
block replies retry at the existing three-second cadence within the caller's
original context. Non-null malformed identities and unsupported requests stay
refusals. Only a successfully decoded conflicting canonical identity supplies
replacement evidence. Cancellation/deadline expiry remains an ambiguous mined
outcome requiring original nonce/chain-state recovery, with no automatic resend
or new signature.

The intermediate `a8e7885d` had the closing bracket but treated a null witness as
a hard error. The final `222e45a8` successor makes missing evidence pending.
Separate omission controls retain the final tests and remove either the whole
correction or only that missing-witness retry classification.

The claim fresh-submit negative fixture now cancels at the third finalized
read, proving the shared poller actually retried the regressed frontier. It
still retains one synthetic send, the exact prepared transaction and nonce
floor, and no finalized publication. Production changes are confined to the
shared helper; the claim owner's additional reconciliation remains in place.

## Qualification

All 74 selected roots pass in normal and race modes without skips: the complete
29-root onchain package plus 45 claim, signed-outcome, nonce, receipt and fleet
EVM recovery roots. This includes all 11 new shared-helper roots. Read-only
build and vet pass for both packages using the retained external
`author.go.mod` and exact server source above.

Two source omissions fail at the intended assertions in both modes: restoring
the original helper and removing only the null-retry classification from the
final correction. These exercise nine distinct causal roots, with 11 root
executions per mode. The retained tests require complete retry/closing reads,
reject replaced identities and canceled observations, and preserve an
ambiguous mined receipt while the required evidence is unavailable. Tests use
synthetic local Rpc, generated fixture signatures, explicit request ordering
and the production retry cadence. Wall-clock sleeps are not the proof of
missing authority.

The sealed author receipt is
`/mnt/data/sn-testnet/shared-evm-finality-astra-20261001/receipt.json`, SHA-256
`bbdf1a3b745778429d5f2c8ba4efa54f768babc8d33398c6803c9488a86eabdb`.
Its 50-entry `SHA256SUMS`, SHA-256
`24af60de3af433abeb985dbae8b592136a2727dd7cc3253162d1143e1a3e77e8`,
passes complete readback. The qualification audit names the terminal commands,
root census, raw streams and causal assertions. Earlier diagnostic runs and
the intermediate `a8e7885d` are not the final successor qualification.

Independent Sol qualification on exact `222e45a8` and server `6c39d307` also
passes: 52 representative committed roots in normal mode, 53 roots under race
including its separate 12-case control matrix, and build/vet for both packages.
The matrix covers regressed and advancing finality, transient and null replies
at all three read positions, persistent missing evidence, cancellation and
actual inclusion replacement. Its archived physical source/module graph is
unchanged after the tests. The separate report is
`/mnt/data/sn-testnet/sol-shared-evm-finality-222e45a8/report.txt`, SHA-256
`b6a912a3a9835f36800e9afd7faf314ccfe4f60a68d43e08a947f1d23bdca2a7`.
The earlier independent baseline and intermediate `a8e7885d` null controls
remain expected failures, not qualifications of those predecessors.

## Remaining launch gates

This closes the identified shared send-helper gap. It does not establish all
independent recovery/authority paths or supply a native consensus/storage proof.
Both deployed validator roles, controlled live upgrade/restart acceptance,
automatic semantic runtime approval and independently signed operating
authority remain separate MG-04/PH-04 requirements.

A fresh exact current-source/image release, provenance/reproducibility evidence
and independent release/deployment approval are still required. Earlier source
and image receipts cannot cover these later bytes. No live signing, chain
submission, service start, deployment or publication occurred.
