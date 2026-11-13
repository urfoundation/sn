# Contract installation funding and role admission qualification

Date: 2026-10-02. Frozen implementation:
`ebf69b90411932d0043ee464f7de3a0ddce54a68`, tree
`15f6f2d3eb9611fe3fc07ae41e263d7afa2bcdb0`, based on unchanged
SN `ff7869d4f757af6f32d4b41904db4f65c4ba5809`. Server is
`ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`; Connect/SCTP remains
`e1b5d77b5029`, SDK remains `5d37be3876e5`, and the sibling/module pins
are recorded in the raw evidence. Go is `go1.26.6 linux/amd64`. No external
modfile or source override was used. Documentation is a separate reporting
commit and does not replace this tested source identity.

This scoped correction addresses two defects before a contract submission.
It does not qualify a production release, supply live mainnet authority, or
complete MG-02/MG-08. All test signatures, sends, contract execution and RPC
responses use synthetic local fixtures. No live signature, transaction,
deployment, service start or policy acceptance occurred.

## Causal failures and corrected behavior

The original first reserve CREATE checked only its own value and maximum gas
liability. The same admission function preserved later same-sender reservations
only when `ActionIndex > 0`. With a balance one wei below the complete approved
two-action reservation, unchanged source still sent the first synthetic CREATE.

`evmOwnedChain.admitCurrent` now sums the selected action and every later sealed
reservation belonging to that sender for **all eight executable actions**,
including the first reserve CREATE. Full-width value-plus-maximum-gas arithmetic
and the sender filter remain unchanged. This is a pending-balance requirement,
not an on-chain fund lock. A refusal changes neither original journal bytes nor
attempt count. Exact sufficient funding sends the same retained bytes once;
later balance loss does not block recovery of that original canonical receipt.
A separate positive control preserves another sender's future reservation
without charging it to the deployer or making that reservation executable.

The proxy builder previously checked nonzero roles and policy bounds but omitted
the pairwise address separation required by `evm/script/Deploy.s.sol` and
`MAINNET.md`. A valid approval signature therefore admitted overlapping
deployer, owner, guardian and commitment-oracle addresses.

`contractProxyPayload` now enforces all six pairwise inequalities while decoding
the exact approved initializer. The proxy and its reserve-link, vault-link and
evidence-CREATE descendants share this check. Both signed `plan` and unsigned
`preview` reject each of the six collisions for all four selections: **48
rejection cases**. Valid distinct addresses remain accepted. This is address
separation; it does not prove independent human custody or actual Safe authority.

The final unchanged-baseline controls overlay only the exact frozen regression
file on `ff7869d4`; production files remain unchanged. Both normal and race runs
fail at the intended assertions, with one actual synthetic underfunded send and
48 admitted role collisions in each mode. They are causal assertion failures,
not compiler, timeout or data-race failures. Initial two-root diagnostics and
focused positive runs are retained separately and are not added to final counts.

## Adjacent review and qualification

Later original actions already preserved same-sender reservations in this
admission function. The separate Safe successor also preserves original
unexecuted reservations belonging to its relayer and needs no matching fix.
The unchanged `bootstrap-chain` preparation remains offline; its contract-role
review selects the complete evidence graph and consumes the corrected proxy
builder. The approved original graph, serialized approval bytes, original nonce
and journal custody, eight-attempt ceiling, and separate signed Safe anchor
acceptance remain unchanged.

| Execution | Result |
| --- | --- |
| Author selected normal | 35/35 roots PASS; 293.154 seconds in Go JSON |
| Author same selected race scope | 35/35 roots PASS; 1640.323 seconds |
| Author vet | Exit 0 |
| Exact baseline normal | Both causal roots FAIL as intended; 13.060 seconds |
| Exact baseline race | Both causal roots FAIL as intended; 89.554 seconds |
| Independent Sol normal | 14/14 selected roots PASS |
| Independent Sol race | Same 14/14 roots PASS |
| Independent Sol vet | Exit 0 |
| Independent exact baseline normal | Both causal roots FAIL with the same one-send/48-admission effects |

The author scope includes the three new roots, all seven later-action funding
checks, signed/unsigned preview and graph-copy validation, exact proxy policy
and full-width claim-window checks, original receipt/attempt recovery, contract
role binding, full evidence CREATE, and complete public Safe-anchor installation
readback. The last path passed in 80.83 seconds normally and 501.33 seconds under
race. Both final positive packages have no skipped roots, failed assertions,
timeouts or race reports. The root manifest and every per-root duration are
retained. The 14 independent roots overlap the author scope; these counts are
not additive or full-package coverage.

The independent receipt uses clean physical source checkouts without object
alternates, the same exact source/tree/server pins, and regression SHA-256
`08e4e838d42f4990ed1a5575ed935482c702488dc6b9ad0b621ec4823aabc35f`.
The author binding rehashes its unchanged receipt, both source fences, and all
twelve command/stdout/stderr artifacts. Explicit joins check the source,
tree, server, causal baseline, exact regression, root identities and verdicts.

## Sealed evidence

The [machine-readable author receipt](contract-installation-admission-qualification-20261002.json)
is an unchanged copy of
`/mnt/data/sn-testnet/mainnet-installation-path-astra-20261002/evidence/source-qualification-receipt.json`.

| Record | SHA-256 |
| --- | --- |
| Author receipt | `ad0932c43819836b71d1007a806f89e64339b88bd04d91e0c2e3fbc6b3aaeee1` |
| Author 68-entry `SHA256SUMS` | `1441da9417dc8ce1246125d3526d47116bd57dfdf590e5ad2c568493ffea41eb` |
| Independent source/regression binding | `ebc3d1e4b4b997c397f1e677854541a63eb67ddf66aaa94e1c877314dfc1ca7d` |
| Independent Sol receipt | `1fa49a4db32451677e92eb19634a78827203fd4f89406d875ff96c16fe819f90` |

All 68 manifest entries were rehashed successfully. The unchanged separate
independent receipt remains at
`/mnt/data/sn-testnet/sol-mainnet-installation-independent-20261002/receipt.json`;
its copied bytes and the explicit binding are also covered by the author seal.
Raw qualification commands, UTC boundaries, source/module/tool hashes, final
source checks, causal logs and earlier diagnostics remain under the author
evidence root. Source, checkouts, caches and test scratch are on `/mnt/data`;
no source read error was observed.

The frozen SN `3d1e2ecf` / server `ac86855d` release predates this correction.
Its receipts remain unchanged. Compose this fix with the later owner-custody
successors in a new exact release before considering launch. This source result
does not establish independent compiler/build provenance, complete dependency
qualification, actual Safe/signing-device custody, accepted production policy,
owned-RPC authority, runtime/code authority for execution, mainnet installation
or service activation. The separate live gates remain open.
