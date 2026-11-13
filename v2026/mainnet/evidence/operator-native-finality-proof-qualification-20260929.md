# Operator native finality proof qualification and integration

Shared server `codex/mainnet-server-hardening-20260927` now contains and has
pushed qualified commit `44636e5ee33a2e481634a6391f74655b99c43923`, tree
`3217c873c392411c9462e71c612b2055f1783988`. It fast-forwards directly from the
[qualified bounded collector](operator-receipt-collector-qualification-20260929.md)
at `b7c8c74336c98de89e17687861820fb2bfdaf831`; the integrated tree matches the
qualified tree exactly. Original collector, receipt verifier, signed histories
and module files remain unchanged. The exact remote head was verified.

The [offline native proof contract](https://github.com/urnetwork/server/blob/44636e5ee33a2e481634a6391f74655b99c43923/strecovery/NATIVE-FINALITY.md)
adds `VerifyReceiptFinality` and `strecovery verify-finality`. It replays the
complete existing receipt collection and checks native SCALE header ancestry,
GRANDPA Ed25519 signatures bound to the actual round and set ID, distinct
authorities with strictly more than two thirds of total voting weight,
descendant vote ancestry and the exact precommit GHOST. It then binds the final
native `fron` digest to the exact raw RLP15 EVM header in the collection. Native
height never selects an EVM block. A valid native quorum over an unrelated EVM
hash cannot authenticate the collection's supplied boundary.

Scheduled authority changes are authenticated through their native digests.
The outgoing set must certify the exact enactment header before successor keys
and the incremented consensus set ID count. Zero-delay changes, delayed changes
and delays spanning intermediate certificates are supported. The separate
checkpoint input contains complete post-finalization active and pending
authority state. Forced changes, disabled voters, pause/resume, overlapping
schedules and equivocations are outside the admitted profile and are refused.
The documented rolling-checkpoint policy retains the approved initial anchor,
exact proof history and derived successor state; the verifier itself cannot
approve its input keys or genesis.

One invocation admits at most 4096 shared native headers, 64 certificates,
1024 authorities/votes per set/certificate and 16 MiB of shared encoded proof
data, with smaller per-header/digest/justification bounds. The CLI has a bounded
deadline, private exact-byte input pins and no RPC, database or signing port.
Proof failures and cancellation produce no partial report or custody mutation.
Every original, replacement, cancellation and unresolved sibling remains in
the unchanged receipt reconciliation.

Astra max implemented the source and performed compile-only/vet checks. Sol
medium qualified the exact frozen commit in its pinned physical module graph:

| Scope | Normal | Race |
| --- | ---: | ---: |
| All declared recovery/CLI roots | 89/89 | 89/89 |
| Included new proof/codec/CLI roots | 18/18 | 18/18 |
| Included private PostgreSQL census roots | 3/3 | 3/3 |

No root failed or skipped. The 86 non-database roots and three database roots
each have one recorded result per mode. Database roots used fresh disposable
PostgreSQL/Redis fixtures; source/module fences, command exits and cleanup
passed, with no owned fixture containers remaining. Vet, formatting and diff
checks passed. Five isolated controls compiled and failed their named assertions:

| Removed or changed guard | Assertion exposed |
| --- | --- |
| Native/EVM hash equality | Unrelated valid native quorum became accepted |
| Strict weighted threshold | Majority by key count with insufficient weight became accepted |
| Ed25519 signature verification | Forged round/signature domain became accepted |
| Authority set-ID increment | Valid delayed successor certificate failed |
| Complete used-header coverage | Unused vote ancestry became accepted |

The [sealed Sol receipt](/mnt/data/sn-testnet/qualification/mg03-native-finality-proof-20260929/SOL-RESULT.md)
has SHA256
`80b3d731eab51fd32861578b465a6c85328e759cd35299a5a2ca66bad753c16d`.
Its [normal log](/mnt/data/sn-testnet/qualification/mg03-native-finality-proof-20260929/sol/all-nondb-normal.json),
[race log](/mnt/data/sn-testnet/qualification/mg03-native-finality-proof-20260929/sol/all-nondb-race.json),
private fixture logs and control diffs/JSON remain alongside the
[89-root source census](/mnt/data/sn-testnet/qualification/mg03-native-finality-proof-20260929/source/all-test-roots.txt).
The [handoff](/mnt/data/sn-testnet/qualification/mg03-native-finality-proof-20260929/HANDOFF.md)
hash is `9c5128e84de3063bd93db355f30229e3b8de719f8afe0ae87322e082ea4d3fae`.
Its original fifth-control recipe was found redundant during read-only review
and corrected before execution; the original handoff remains retained as
`HANDOFF-R1.md`. No failed qualification was relabeled passing.

The [ownership audit](/mnt/data/sn-testnet/qualification/mg03-native-finality-proof-20260929/OWNERSHIP-AUDIT.md)
hash is `3fc47ac73619478755e49f719950fd895f35c7c2e4abc0b67425d2dc58e74bef`.
It established that a separately spawned competing implementation occupied a
different physical worktree. None of that unqualified source was imported.
The authoritative ten-file manifest was verified before and after testing.

The exact module graph retains SN `5198f6c9`, SDK `516521fb`, Connect
`b163f9dd` and the other clean physical sibling pins from the collector
qualification. Resolver SHA256 is
`9303629371cfbb37d4e50d18a5c1db19f9813e8f9bde9a48bc6e646f28071531`.
Full paths/pins and reviewed codec source hashes are retained in the handoff
and its `source/` directory. Only `sn/protocol` is consumed from local sibling
modules; its subtree `7b446bee1e75fdb9205d5f35f53cd80059fe54b5` is identical
at qualification SN `5198f6c9` and integration base `3fcec46e`, with no diff in
that package or SN module files. This is scoped component qualification, not
a composed-release result for current moving roots. The separately retained
[shared-SN mainnet baseline build failure](/mnt/data/sn-testnet/qualification/sn-mainnet-baseline-20260929/SOL-RESULT.md)
against an older shared SDK graph is outside this receipt and remains separate.

Reviewed codec/fee references are Subtensor
`67dcf7f791dc495064c293f080a0702cb433e51e`, its pinned SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`, and finality-grandpa 0.16.3.
They are source references, not deployed-runtime attestations. In particular,
the Subtensor runtime's administrative `CurrentSetId` update on scheduling is
not used as consensus authority; the verifier follows the client's certified
enactment transition.

Every output retains `unapproved_checkpoint_proof`. No independently approved
mainnet genesis or checkpoint exists in this scope. Checkpoint, genesis,
runtime, finality, canonical-accounting, actual-fee and spending authorization
flags remain false. Successful signatures prove possession of the supplied
keys, not the independent approval of those keys or their checkpoint state.

Actual fees remain null. Frontier receipt gas and RPC-rendered prices do not
prove native fee debits: the reviewed custom handler converts 18-decimal EVM
units to 9-decimal native balances and applies a best-effort refund. This
increment supplies a native state-root commitment relative to the checkpoint,
but no authenticated transaction-scoped debit/refund storage or event proof.
Runtime provenance, metadata and actual native debit/refund evidence remain
necessary before fee authority can change.

MG-03/PF-03 remain open for independent initial/rolling checkpoint admission,
owned-node identity and raw capability, bounded native-proof capture, exact
runtime admission, account nonce proofs, actual fee proof, production service
adoption, release composition and live custody/restart. No live RPC, shared
operator database, signing, broadcast, deployment or mainnet activation was
performed or authorized by this qualification.
