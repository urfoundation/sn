# Current native headers and runtime authority

Date: 2026-10-01. Source candidate:
`889f5c293a71122126aeddb1f68b83cee9ddbe85`, tree
`6d30d4be93523a979a3547d9410e0dd606e05f1b`, based on SN
`4164b197cf8d6ae2a7ceb7d3d8d55fed8f1de097`. Author normal, race and vet
qualification passes on this immutable graph. Independent review and composed
release qualification remain separate gates.

## Concrete failure and correction

Current runtime admission used an RPC header number without verifying the
complete SCALE header against its announced block hash. A synthetic real block
250 could be returned with number 150 and matching same-height RPC answers; the
standard production validator then selected its signed 101–200 runtime window
and installed producer authority. The shared registration/stake admission also
accepted changed header fields and a canonical hash replacement during artifact
authentication. Exact runtime hashes did not authenticate the header coordinates.

The corrected production validator authenticates every header field before
selecting a current or historical signed window. Shared native registration,
stake and validator registration status use the same complete-header reader and
check canonical height before and after runtime authentication. Mainnet fleet
authority applies these checks on its actual native/EVM connection; native and
EVM preparation retain a freshly authenticated original header height. Failed
admission leaves the caller's prior bound view unchanged.

The existing complete-header reader supports the exact one-byte
`RuntimeEnvironmentUpdated` digest. The old SDK accepted that tag while discarding
its content, so the reproduced defect is missing commitment authentication,
not a demonstrated inability to decode that tag. Positive fixtures retain both
current and historical upgrade blocks, ordinary producer-purpose checks and
original one-send fleet recovery. No new runtime, window, policy or signature
authority is introduced.

Original signing, inclusion-parent execution and inclusion post-state remain
separate authenticated views. The source receipt and native capture call sites
now use the same strict artifact helper without changing their historical bind
purpose or execution/post-state selection.

## Qualification and retained diagnostics

The exact author graph uses a clean isolated server
`94229abb02819cea97f972421972b4e79c1a32ab`, Connect `e1b5d77b5029` and SDK
`5d37be3876e5`, with the checked-in SN GSRPC/npipe forks. No module pin changes
are part of the source commit. The external modfile, module graph, source patch,
commands, raw JSONL streams and old-code overlays are retained under
`/mnt/data/sn-testnet/mainnet-current-header-authority-astra-20261001/`.

The [sealed author receipt](/mnt/data/sn-testnet/mainnet-current-header-authority-astra-20261001/receipt.json)
has SHA-256
`e46055c9819a9c15c2d94bdc6a5799f310fd81c09f011940a55836bad3e1421a`.
Its [52-file manifest](/mnt/data/sn-testnet/mainnet-current-header-authority-astra-20261001/SHA256SUMS)
has SHA-256
`782ef8e2351b2e8880387d67282025afa31284c487863c4e376b67d1675103fa`;
all entries passed local checksum readback.

| Author gate | Exact outcome |
| --- | --- |
| Matched normal selection | 198 top-level roots, plus 45 existing subtests; all four packages pass |
| Matched race selection | The same 198 top-level roots pass; all 78 miner roots pass in one package, then 98 validator roots pass in four disjoint partitions and 22 chain/crv4 roots pass in one small partition |
| Causal controls | Five intended integrity failures normally and the same five under race; no build or panic failure substitutes for an assertion |
| Vet | `./chain ./miner ./validator ./crv4`, exit 0 |
| Environment/startup rechecks | Three isolated normal roots pass |

The evidence audit verifies the sorted race-root union equals the normal-root
set. This is 396 matched positive root executions plus three isolated checks.
The original race runner completed miner in 594.986 seconds and was terminated
during the next package's compilation, before any validator test began. Remaining
partitions use explicit 30-minute deadlines and at most five concurrent processes
with `GOMAXPROCS=2`; completed miner work was not repeated.

Twelve new top-level roots exercise full-header substitution, signed-window
substitution, cold/warm canonical closing checks, cancellation, current versus
historical purpose, all five public fleet commands and original native/EVM
recovery across restart. Five integrity assertions fail when the corresponding
original chain, validator or fleet admission file is restored individually;
normal and race controls retain those exact failures.

Initial logs remain diagnostic evidence: the first selection hit the native Unix
socket path bound in a replay fixture; a broader run then found an inherited
group-writable temporary parent in the protected-state fixture. The short owned
temporary directory is now mode 0700, and both fixture rechecks pass. The broader
run also exhausted its total ten-minute package budget sixteen seconds into a
startup-read test; that test passed separately in 68.707 seconds. Four privileged
foreign-UID skips in the broader run are not passing qualification. The separate
terminal normal/race receipt above supplies the qualified scope.

## Gates retained

This is local synthetic qualification of current header identity and the selected
runtime/recovery paths. Finality and native storage still depend on the approved
RPC and their own proof/admission requirements; these checks do not independently
prove chain state. Independently approved runtime/source provenance, exact signed
config and finite windows, genuine semantic successor proof, other native
consumers, both deployed validator roles and controlled live upgrade/restart
acceptance remain open under MG-04/PH-04.

The source follows release fence SN `233ea2be` / server `94229abb`. That release's
source-to-image and reproducibility receipts cannot cover this later code. A
fresh exact successor release and independent release/deployment approval remain
required. No live signing, broadcast, service start or publication occurred.
