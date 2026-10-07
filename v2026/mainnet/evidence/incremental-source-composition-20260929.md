# Incremental SDK / MG06 source composition

This is a provisional source integration with explicit qualification limits.
The reviewed SDK graph and bounded native incentive observer have completed
their focused normal/race and causal checks. The original SDK candidate has
normal coverage for all 618 mainnet roots. The later race audit records 618/618
root-body passes, with 410/618 backed by package-PASS streams and 208 still
pending that terminal coverage. Broad race package qualification remains open.
No single-process full mainnet race pass, full 636-root composed pass, approved
release or deployment is claimed.

## Preserved source history

| Commit | Role |
| --- | --- |
| `7151283c702ce9f679965ea30a2da460b9157ee7` | Shared integration base |
| `86ebb8c12e66b085adb3577815e36c993c8870dd` | Original SDK source-graph correction |
| `840379ddce28e290712589006c706eb23ef3fef3` | Existing `cherry-pick -x` of the SDK correction onto the base |
| `9bc0a53bbe0cf2fa1666300b4cb2efa7199710ad` | Standalone bounded MG06 observer |
| `1ab0982031c647e0f5496365d6268e390235b8e1` | Qualified focused composition of `840379dd` and `9bc0a53b` |
| `a30186d1213b28fce4ccc3bf18d0dd4d94d2f053` | Merge of that composition and original SDK `86ebb8c1`, retaining both original histories |

The ancestry merge uses ordinary Git merge semantics and changes no tree bytes.
Both `a30186d1` and qualified `1ab09820` have tree
`0bd0edecf3e68fda9e7267beda81f6c2107c4fcf`. All ten MG06 paths are blob-identical
to standalone `9bc0a53b`, all five SDK correction paths match original `86ebb8c1`,
and every other composed path remains unchanged from `840379dd`. The subsequent
documentation changes record these receipts and the actual-fee dependency review;
they change no Go source, tests, module file or runtime configuration. No history
was squashed, rebased away or replaced with a fabricated passing result.

The original [shared-graph baseline failure](/mnt/data/sn-testnet/qualification/sn-mainnet-baseline-20260929/SOL-RESULT.md)
remains evidence: clean SN `3fcec46e` selected older local SDK `42241118`, lacked
the required registration/refresh APIs and failed compilation before any test
body ran. The [source correction](clientauth-source-graph-correction-20260929.md)
fixes that effective dependency selection through checksum-bound replacements.
The baseline failure has not been recast as a behavioral result.

## Completed and pending qualification

| Scope and exact SN candidate | Normal | Race | Controls / limits |
| --- | --- | --- | --- |
| SDK focused, `86ebb8c1` | 19/19 | 19/19 | Three distinct SDK/Connect/SCTP sibling overrides compiled and failed their named identity assertions |
| SDK mainnet, `86ebb8c1` | 618/618 exact root union | 618/618 root-body passes; 410/618 with package PASS, 208 pending | Four normal shards have package passes. Original unsharded race and six initial race shards timed out at one hour; wave2/08 had no terminal at the audited snapshot. These package outcomes remain separate evidence. |
| MG06 focused, `9bc0a53b` | 18/18 | 18/18 | Eight single-edit mutants compiled and failed the intended guard assertions |
| MG06 adjacent, `9bc0a53b` | 170/170 | 170/170 exact root union | Three race shards have package passes; original adjacent race remained active at sealing |
| Composed focused, `1ab09820` | 37/37 | 37/37 | 18 MG06 + 16 client authentication + 3 source graph roots; every selected root observed, all six commands exit zero |
| Composed representative smoke, `1ab09820` | 4/4 | Not selected | Separate seventh command exits zero |

The [independent SDK race audit](/mnt/data/sn-testnet/qualification/mg07-read-incidents-sol-20260929/sdk-race-audit.json),
SHA256 `80f858409dcdf7c1e8705b9a66b696d8b78c97590ffa2352b6f0c206a2ee2414`,
was checked against its 618-root census, all 23 retained JSON prefixes, nine
physical source fences and the original module-fence hashes. Every top-level
root has a passing body outcome, with no root failure or skip. Only 410 roots
have a pass inside a stream whose package itself passed; the other 208 still
need that coverage. This distinguishes body completion from package cleanup,
race checks and exit. Duplicate attempts remain separately recorded.

The original unsharded SDK race run and six initial shards timed out at one
hour; those package failures are preserved. The unsharded timeout occurred while
`TestEvmEscrowRegisterClaimRecoveryKeepsFourLocks` had been active for about
25 seconds, not after that root had consumed an hour. Wave2/08 had no package
terminal in the audited snapshot. Targeted bounded race shards must close the
exact uncovered set before broad package qualification is declared. Keep each
shard's source/root census, terminal exit and original timeout, and preserve
completed results. This audit adds no current-composition, release or deployment
acceptance and does not turn pending or failed package output into a pass.

Astra performed compile-only/vet, formatting, module verification and source
fences. Sol owned behavioral execution. Retained composed compile/vet checks
all exit zero, `go mod tidy -diff` and formatting output are empty, and all
physical source/module identities match before and after qualification.
The ancestry merge has identical source bytes; no additional behavior was
executed by Astra for the merge or documentation.

| Receipt | SHA256 |
| --- | --- |
| [SDK focused](/mnt/data/sn-testnet/worktrees/sn-mainnet-clientauth-source-graph-20260929/sol/SOL-FOCUSED-RESULT.md) | `466094a9ecca3f9eda85b521f740d6407e2f8e464849570dcdde867b31473b22` |
| [SDK 618-root normal union](/mnt/data/sn-testnet/worktrees/sn-mainnet-clientauth-source-graph-20260929/sol/SOL-NORMAL-RESULT.md) | `6854cfcd1f6c0793e9468414c0a44cc49379eabbcde59e848f1db6500483762d` |
| [MG06 standalone](/mnt/data/sn-testnet/qualification/mg06-economic-observation-20260929/sol/SOL-RESULT.md) | `c73e02fbee69868f506489268eb16e4a66d239ec8df3b2da3e345da12e76a5dd` |
| [Composed owned receipt](/mnt/data/sn-testnet/qualification/mg06-sdk-composition-20260929/sol/focused-owned/SOL-RESULT.md) | `062de53e203abc23dcf0646536a0f08ab0c24305f14b0e8ba607b9b47402230e` |
| [Composed owned manifest](/mnt/data/sn-testnet/qualification/mg06-sdk-composition-20260929/sol/focused-owned/SHA256SUMS) | `1e6ac671e77bd206d005ba30331e39a914c23c6c00f9516f659be205f0b42953` |
| [Astra composed handoff](/mnt/data/sn-testnet/qualification/mg06-sdk-composition-20260929/astra/HANDOFF.md) | `737db79a3f20e2a1c830cb866787eabad3901b93afd8d30617756dd55dc3f394` |
| [Astra composed manifest](/mnt/data/sn-testnet/qualification/mg06-sdk-composition-20260929/astra/SHA256SUMS) | `1756088a5b7006d0f1c3a0289d220ac3581f504260887b5a0814f7de28bebba5` |

The composed Sol receipt occupies its own create-only evidence directory.
Another concurrent runner retained independent same-candidate output in the
parent directory and reused its receipt filename. Those records are preserved,
but they are outside the owned receipt's immutable file manifest; their later
writes cannot change the cited owned qualification. Raw logs, source/module
fences, exact selectors and causal diffs remain with their respective receipts.

## Integration source graph

Keep `GOWORK=off` and the checked-in module files. The compiler selects these
versioned archives rather than mutable SDK/Connect sibling HEADs:

| Requested module | Effective source |
| --- | --- |
| `github.com/urnetwork/sdk` | `github.com/urnetwork/sdk v0.0.0-20260928100458-516521fb16da` |
| `github.com/urnetwork/connect` | `github.com/urnetwork/connect v0.0.0-20260928101830-b163f9dd9ac3` |
| `github.com/pion/sctp` | `github.com/urnetwork/connect/sctp v0.0.0-20260928101830-b163f9dd9ac3` |

The focused composition was qualified against physical server
`44636e5ee33a2e481634a6391f74655b99c43923`, tree
`3217c873c392411c9462e71c612b2055f1783988`. The shared integration graph explicitly
uses its documentation-only successor **`cfcbfcbaa13b4f4d298acfeca761a7252c18ddee`**,
tree `8eee38335f1859e5bd715742f24a20170dc85df6`, at
`/home/by/urnetwork/server`. Its only changes are
`strecovery/ACTUAL-FEE-DEPENDENCIES.md`, `strecovery/NATIVE-FINALITY.md` and
`strecovery/actual-fee-reviewed-sources.tsv`; every Go source and module file is
unchanged. This records the physical server advance without moving the earlier
qualification's commit or claiming new server behavior coverage.

The other local replacement pins are proxy
`6204ae7df2a9868bbb3a7b61231917a36e4f5c9f`, userwireguard
`85fb1ca4086fa5dbfcda526bec7a17a894e691b9`, warp
`7498864c7cd3605aad3c43eabfab9008ed7f7228`, glog
`892ade4a6be396b32ea82a550f243190b5992180`, and goidenticons
`325750b38314313dc5f44c880ab6f12f6c1ecb3c`. Npipe remains the checked-in
`third_party/npipe` subtree. The ancestry worktree's complete effective module
identities equal the composed graph; only physical SN path fields differ.
Actual post-integration local/remote heads and the physical source/module
snapshot are retained under
`/mnt/data/sn-testnet/qualification/mainnet-incremental-composition-20260929`.
These records are not an approved release manifest or source-to-image proof.

The mainnet package/test dependency census consumes no server package. Its
focused receipt therefore does not requalify MG03. The separate
[native finality qualification](operator-native-finality-proof-qualification-20260929.md)
retains server `44636e5e` and 89 normal/race roots. The new
[actual-fee dependency review](https://github.com/urnetwork/server/blob/cfcbfcbaa13b4f4d298acfeca761a7252c18ddee/strecovery/ACTUAL-FEE-DEPENDENCIES.md)
is documentation; generic balance events and block deltas do not supply exact
native gas debit/refund attribution. Actual fees remain null and MG03/PF03 stay
open for authenticated state capture, runtime-qualified attribution and the
other retained operational gates.

## Economic and launch boundaries

`observe-native-miner-emission` retains bounded archived event/state evidence,
canonical/ancestry checks, parent-runtime decoding and sealed partial results.
The observer's denominator, provider entitlement, owner recycling, quantization
tolerance and actual 10/90 outcome remain null, and activation remains false.
Complete reads are not a native payment, recipient-generation or entitlement
proof. Synthetic fixtures do not approve deployed metadata, runtime code or an
owned archive node.

MG01, MG02, MG03/PF03, MG06 and the other unclosed release gates remain open for
their recorded evidence. No live RPC, shared database, real-key access, signing,
broadcast, contract deployment or mainnet activation occurred in this source
integration or its local qualification.
