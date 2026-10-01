# Operator receipt collector qualification and integration

Server root `codex/mainnet-server-hardening-20260927` now includes and has
pushed the exact qualified successor
`b7c8c74336c98de89e17687861820fb2bfdaf831`, tree
`d3d67435575cde80c94ecba078d58062eb14bd74`. It fast-forwards from `fbe0c039`
through implementation `48a1878e` and the test/documentation correction, retaining
the complete prior [receipt-verifier](operator-receipt-commitments-qualification-20260929.md)
and [MG03/R48](operator-mg03-r48-composition-20260929.md) histories. The integrated
tree matches the qualified tree exactly; the remote branch head was verified.

The [collector contract](https://github.com/urnetwork/server/blob/b7c8c74336c98de89e17687861820fb2bfdaf831/strecovery/RECEIPT-COLLECTION.md)
adds `collect-receipts` with an explicit owned HTTP endpoint and separately
supplied native/EVM boundary claims. It reads every archived signed candidate,
retains separate native and EVM heights, follows raw EVM parent hashes, verifies
complete transaction/receipt vectors against both trie roots, and builds exact
inclusion/predecessor proofs. It rechecks network and boundary assertions and
replays the existing offline verifier before publishing one private create-only
evidence file. `verify-collection` replays that file entirely offline. Every
original, replacement, cancellation and unresolved sibling remains represented.

Transient reads use exact selectors and context-aware pacing for a configurable
60–900 second window, default 300, within a 15-minute overall maximum and shared
request/response limits. Unsupported raw capabilities, partial vectors, malformed
or conflicting data and cancellation do not publish a partial artifact. New
evidence is sealed to `0400`; identical retries preserve the existing inode and
different evidence cannot overwrite the previous file. No signer, submission,
database writer, nonce allocation or custody restoration is exposed.

Astra max implemented and corrected the source. Sol medium qualified the exact
successor in the pinned physical dependency graph:

| Scope | Normal | Race |
| --- | ---: | ---: |
| New collector/RPC and command roots | 19/19 | 19/19 |
| All declared recovery/CLI roots, including those new roots | 71/71 | 71/71 |
| Of that declared total: private PostgreSQL census roots | 3/3 | 3/3 |

Five isolated controls compiled and failed their intended assertions when final
proof replay, final boundary rechecking, final network rechecking or shared
response-byte accounting was removed, or the sealed file mode was weakened.
Vet, formatting, diff checks, source/module fences and fixture cleanup passed.
Every declared root appears once per complete mode, with no failure or skip.
No live RPC, shared database, signing or deployment was used.

The original `48a1878e` qualification remains failed: its new publication test
incorrectly expected final mode `0600`, while the unchanged existing publisher
seals temporary files to `0400` before fsync/rename. The successor changed only
tests and documentation, requiring `0400`, a regular unchanged inode on retry,
and refusal of exposed modes, aliases and a nonprivate containing directory.
The original normal/race failure remains retained; it was not relabeled passing.

The [sealed Sol receipt](/mnt/data/sn-testnet/qualification/mg03-receipt-collector-20260929/SOL-RESULT.md)
has SHA-256
`4a1ac9df3ce571cf42e131962397d1f07f7135473acd35e46ce5831a4a6a7b27`.
The [per-root inventory](/mnt/data/sn-testnet/qualification/mg03-receipt-collector-20260929/sol/successor/per-root.tsv)
and raw normal/race, private fixture and causal-control logs are retained
alongside it. The [integration review](/mnt/data/sn-testnet/qualification/mg03-receipt-collector-20260929/INTEGRATION-REVIEW.md)
has SHA-256
`a0528505ccf7850364e1756f38337b0ff0b5fd06d231c444f08d3e283040cabc`.

The module graph retains the earlier fbe0 qualification's SN `5198f6c9`, SDK
`516521fb`, Connect `b163f9dd` and other pinned physical siblings. Tracked module
files and dependency versions are unchanged. The collector consumes only
`sn/protocol` from local sibling modules; that exact subtree is unchanged at
reviewed SN root `8a74a605`. This supports scoped source compatibility, not a
new composed release qualification for all current roots or deployed artifacts.

The output always remains `unapproved_observation`. Its node capability and
external mapping inputs are not independently approved; native consensus/header
finality, native-to-EVM mapping and boundary account nonces remain unproven.
Canonical rechecks are owned-RPC assertions. Gas and outcomes are committed,
but rendered base fee/effective price, actual runtime debits and native fee
conversion are not. Actual receipt fees remain null and all finality,
canonical-accounting, actual-fee and spending-authority flags remain false.

This closes the bounded collector's source qualification only. Owned production
node capability and source/runtime admission, independent finality/mapping,
exact fee evidence, service adoption, release composition and live custody/
restart qualification remain open. MG03/PF03 and mainnet activation are not
closed by this implementation or receipt.
