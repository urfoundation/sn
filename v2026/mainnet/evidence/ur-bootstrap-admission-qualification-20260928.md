# Offline two-UR-validator config admission qualification

Sol medium qualified sealed SN `5198f6c96a363b86c7a9951ccefe395a2b54ef56`
for [bootstrap-chain v2](../BOOTSTRAP-CHAIN.md). Exactly two initial schema-3
configs now pass the existing validator production grammar and signed approval
verification against independent role, signer, runtime/source, deployment and
protected-generation inputs. The inspector consumes pinned bytes, exposes
public facts, refuses retained-approval substitution and overlapping custody
paths, and opens no key or operator evidence. V1 recovery keeps its original
unadmitted scope and cannot silently acquire v2 authority.

The R2 positive runner passed **24/24 joined stages**, **588/588 root
executions** and **183/183 descendant executions**, with no skips or race reports.
This includes the full normal mainnet census (391 roots, 79 descendants),
selected bootstrap-chain/root/EVM-create race roots, and affected validator
config, activation, production approval/runtime, authority history, observation
and four production authentication consumer roots in both modes. The shared
decoder change received fresh coverage; prior validator qualification was not
reused wholesale. Source, binary and module fences passed.

Six causal captures compiled in normal/race modes and failed at their named
assertion: original arbitrary-config admission, signed validator-ID mismatch,
wrong signature domain, reopening a substituted config path, retained-approval
fallback, and a sibling path hiding a custody ancestor. Their fixed counterparts
pass in the positive selection. R3's unchanged maintained runner subsequently
accepted all six captures with `finished=total=resumed=4`: **24/24 retained
stages**, **12 intended failure bodies**, and **zero body reruns**. All results
are joined; R2 positive and R3 before/after input seals and source fences pass.

Execution used Go 1.26.6, `GOWORK=off`, `GOTOOLCHAIN=local`,
`GOFLAGS=-mod=readonly` and `GOMAXPROCS=2`. Actual replacements resolve to
physical worktrees under
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`:

| Source | Exact commit |
| --- | --- |
| SN | `5198f6c96a363b86c7a9951ccefe395a2b54ef56` |
| Connect, including sctp | `b163f9dd9ac374942fe97331f26631248a9c1f81` |
| SDK | `516521fb16da46c9f4bff0b58221e1941694f616` |
| Server | `5dc11761373580b5a0ddd9757cd6e4eb94140e27` |
| Proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` |
| glog | `892ade4a6be396b32ea82a550f243190b5992180` |
| goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` |
| userwireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` |
| Warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` |
| operator-proxy | `448369740c70eb2e642bde9d36f3708fe9ad4b5d` |

Warp/operator-proxy are pinned sibling sources, not active modules in this Go
graph; npipe resolves inside the sealed SN tree. This is the current composed
dependency graph, distinct from the older v1 component graph. It qualifies the
listed source paths, not every package, built image or full mainnet release.

The original evidence prefix is
`/mnt/data/sn-testnet/evidence/mg08-ur-offline-admission-20260928`.
Positive evidence uses its `-r2` directory; accepted control reuse uses
`-r3-control-reuse`. Raw source maps, binaries, exact selectors/literals,
command/event logs, module proofs and exits remain there.

| Retained artifact | SHA-256 |
| --- | --- |
| R2 `inputs.sha256` | `df3e20d60017e0c0742760c21ffc5b18cccb0e709ee07e277edaa3fc2aae1083` |
| R2 `SEALED.json` | `a10ae4abddefb34c7618a5246fcdc93225751fff902b99948ca5141e64c522e0` |
| R2 `sol-positive/report.json` | `73aeabdce449c62c67598094c8b826e20fda85cbc7ad6436c3d69bfcdf6474dd` |
| R2 `sn.content.sha256` | `4d7b634a86358b93fe9d8c865136504e93d91d871c65bbcf79ddba032be2bc9a` |
| R2 `author/modules.json` | `4491f56781a2b966aac53fd931d6f22c22ee4b6f72f32cc0275ca304ccbe72f3` |
| R2 `source-refs.json` | `325f3a200965ff671b0cf2529f2692c990a022425efd0501121928482632e579` |
| R3 `inputs.sha256` | `a1504a347cff7df925552a53f41e92d39c5fe2f4e18f1067a3fa82d618d39f39` |
| R3 `SEALED.json` | `96dd3ab6a57ad6f61b4da5b4415b65a834a717557d31d16a3c81429e55868cfd` |
| R3 `run-sol.sh` | `54e4916b9ab64821d0388e0d6dece79b6f7588003de04c00edd8ef276c2f664d` |
| Maintained qualification runner | `15da70c645c7de9839c7c0a6c511358a8c65b29f823f4f6e9dad0dfc1e338d9d` |

The original unsorted positive census was refused before build/test. Sorting
the two affected TSVs preserved every identity/outcome; all 45 TSVs were audited.
During that correction, six global metadata inputs changed while old controls
were active, so their outer seal correctly failed despite valid local body
receipts. Those original bytes/refusals were retained, and R2/R3 were sealed in
separate directories. R2 resume then refused a byte-identical binary copied as
0700 under umask 077 instead of its declared 0755. R3 used umask 022 only for
the unchanged resume subprocess, retaining private 0700 capture directories
and 0600 evidence. No refusal is reclassified as passing, and no product,
assertion, selector membership or runner code changed during these repairs.

Astra max implemented/reviewed the change, ran compile-only checks and vet, and
ran no behavioral tests. Integration onto the shared branch preserves every
qualified Go/module/rule byte; only documentation differs from the sealed
source. Source equivalence and `git diff --check` passed without a test rerun.

MG-08 remains blocked for activation. Intended majority/secondary labels do not
prove live stake, permit, key possession, current generation/window, operator
health, deployed contract/runtime authenticity or running binary identity.
Approved mainnet identity and an owned working route, trim execution, complete
contract installation, current root authority, custody devices/global fencing,
Safe/funding, two live validators and operators, service activation, release
artifacts and observed native 10/90 economics remain open. No live RPC, signing,
transaction, service or deployment was performed for this qualification.
