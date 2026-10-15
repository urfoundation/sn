# Mainnet prelaunch fixes

## Own the epoch roster producer — October 6

A signing library and ingestion API do not establish a production producer.
Provision and supervise the separate SN `cli/payoutroster` service described in
[PAYOUT-ROSTER.md](PAYOUT-ROSTER.md), including its independent roster key,
complete reviewed epoch inputs and matching Server public authority pins.
Network-only provider payments require the v2 roster's original network-wallet
heads; do not treat missing heads as a complete successful payout or omit the
provider. Preserve the shared install-consent-first precedence.

Retain the exact request and signed roster before publication, then the verified
acknowledgement before retiring queue work. Lost responses retry the same bytes.
Missing reconstructible local data can be restored from the exact approved
request and fixed signer, checking any retained receipt before sending; actual
contradictions remain blocking. Require physical pre-provisioned private storage
instead of following ancestor symlinks or creating unsynced custody ancestors.
Bound each inbox scan while preserving forward progress; historical completed
requests and invalid entries must not starve future epochs. Qualify interrupted
publication, recovery, retirement, changed consent generations and network-only
providers with deterministic tests before deployment. This service is a new
completion increment; the older delivery receipt below does not qualify it.

## Code and focused-test checkpoint — October 6

The requested code changes, website guides and focused tests are complete. The [delivery checkpoint](evidence/code-delivery-checkpoint-20261006.json) records the final paired builds, Server operator 51 normal /27 race results, and website build/link/SEO/date checks. These results retain their exact source and selection; mainnet activation remains outside this scope.

The [current code/test checkpoint](MAINNET.md#code-and-focused-test-checkpoint--october-6), [initial evidence index](evidence/code-focused-qualification-20261006.json), [hardening qualification](evidence/code-focused-followup-20261006.json) and [carry, treasury and local qualification](evidence/carry-treasury-qualification-20261006.json) supersede older pending and current-state claims below. Root54, RPC7, schedule-preflight6, final validator3, Core8 and Server interop2 have passing focused results. Their original source tuples and waits remain separate. The follow-up records deadline22, taxonomy14, ordinary-mask fixture4 and storage normal15/race2 passes. Final SN and Server `go build ./...` results pass on SN `ff1ba7d2`, Server `875fbca2` and Connect `501c172d`; the subsequent SN `3a3fc001` checkpoint changes documentation only. Earlier results keep their own source pins and supply no blanket full-suite qualification.

Preserve the original RPC16 **11 PASS / 5 FAIL**, validator12 **10 PASS / 2 FAIL**, provider4 **3 PASS / 1 FAIL**, old-Connect all-package build failures and Core test-image staging failure. The RPC7 successor covers the five original RPC failures. Provider4 passes both original validator failures; final validator3 passes its added completed/failed/idle control plus the two new deadline cases. These are exact successor results, not rewritten original receipts or a new full-suite run. Prior retention20, raw-version scope3 and the original Rust11 result retain their evidence.

This completion scope is code and local focused tests. Live capture, production database migrations, deployment and activation were intentionally outside the task. The historical capture-v2 failure remains closed with its raw RPC reply unavailable; no exact service cause or chain mismatch is inferred. `activation: blocked` remains unchanged. The selected economic route pays providers momentum, 10% at launch, and `ur-reserve` the rest, miner emissions × (1 − momentum); earlier recycle or reserve-sending requirements retain historical scope only.

The selected deployment fields `deployment_id`, `coordinator`, `settlement_vault`, `policy_hash` and `readiness_sha256` remain empty. Owner/root/operator custody and roles, registered recipient setup, runtime authority and activation remain operational work outside this code/test scope. Historical pending statements keep their dated scope; the current qualification above records implementation and test status.

Source `270908be` corrects validator readiness for the selected treasury policy. Both verified producer approvals must bind the same treasury policy; current and signed activation boundaries authenticate `Burn` or `Recycle`, including the metadata-defined default. Legacy approvals retain their explicit `Recycle` rule. Readiness records the treasury policy and both observed modes, while same-hash contradictions remain hard failures. The current bootstrap report identifies the native 10% provider /90% treasury outcome without changing earlier sealed plan bytes. The original treasury14 run retained eight adjacent passes and six fixture-precondition failures. Test-only `874f9fc6` composes shared runtime/census originals before sealing the independent scopes; the six corrected cases and one passive-bootstrap control now pass on SN `ff1ba7d2`.

The same checkpoint records four pure cancellation tests, two isolated accounting DB tests and two economic-progress race tests on their original source tuples. Miner4 retains its original 3 PASS / 1 FAIL result and the corrected four-pass run. The initial DB collector import failure is preserved separately from the successful bodies and cleanup. The final Server operator tests and paired builds now have separate closed passing receipts in the delivery checkpoint.

## Preserve retry decisions, deadlines and write ordering

An explicitly allowed transport cause must survive later classification. `syscall.EIO` also implements `net.Error`, but both optional network hints are false; the old fallback overwrote its already accepted retry decision. Source `f4596373` preserves that decision while continuing to reject an error tree containing any independent hard cause. The same qualification corrected a fixture that confused the original logical owner deadline with each physical HTTP deadline: RPC attempts retain their shorter 30-second ceiling inside the unchanged owner budget, so their individual deadlines can move without extending the operation.

An elapsed deadline is authoritative even before its timer publishes `ctx.Err()`. The provider follow-up `3a8f6d75` checks the absolute deadline before another request and before accepting its result, preserving the original error causes. Source `57dfa287` shares that check with GET retries and `HttpWalletMappingReader` through `evidenceReadContextError`; the provider helper's body is preserved. A separate cold-source fixture failure came from three synthetic Ethereum headers missing required `Difficulty` fields. The repair supplies those fields and preserves strict production decoding. The added completed/failed/idle control then exposed a fixture that changed configuration after construction without updating the engine's copied resolver. Source `8a95318d` selects both before any request or record, preserving the original integrity assertions.

When a payout schedule is selected, validate its exact digest and bytes before database migration writes. Source `89c545c7` refuses empty, malformed or zero pins and unavailable or mismatched selected schedules before invoking migrations, then reloads and checks the schedule again before preparing its immutable payout boundary. Omission preserves the existing migration mode; successful preflight cannot authorize changed bytes after schema writes.

Expected provider and client-key reads must recover from admitted service failures, including HTTP 500, 408 and 425, within their original bounds. Sources `1044e0e6` and `eeaf1c80` preserve the original request, independent read/close errors and cancellation. A transient status cannot hide a completed integrity conflict or hard body-close failure. Client-key HTTP 429 quota responses remain terminal, and service recovery cannot create a third attempt beyond the existing two-reservation limit. Provider reads retain one 300-second owner and 60-second physical attempts.

Record the exact checkout behind every local module replacement. Both all-package builds exposed a missing published `ConnectOwnerLedger` API while using Connect `11f99c8f`; unchanged module files had not pinned a sufficient dependency revision. The reviewed fast-forward to `a5dfb3c7` supplies the published API. Retain the failed builds against their original dependency tuple and qualify the specific owner-ledger integration on the successor; that focused evidence does not qualify every upstream change. The later Core test-image compilation also required the staged `../gvisor` module and exact test-only `klauspost/compress` v1.19.1 cache. A passing ordinary build does not establish that complete test-image dependency closure; preserve that separate staging failure and its retry.

The three adjacent deadline paths are now corrected: scalar and batch chain reads (`f664f40b`), bootstrap-prefix reads (`d3ba5bbc`) and production steering reads (`4fa537c4`). They check elapsed deadlines before admitting another read and before accepting its result, preserving earlier caller deadlines, original operation budgets and hard error causes. All 22 selected tests pass on SN `bc3c20d0`: nine chain, seven bootstrap and six steering roots. The [additive evidence index](evidence/code-focused-followup-20261006.json) records the exact source tuple and closed result, completing the earlier source-only follow-on without implying a financial mismatch or live execution result.

A freshly compiled private fixture must not depend on the ambient umask. The original Server attempt produced mode `0775` and stopped in `TestMain` before any selected case; all 14 taxonomy tests later passed under `UMask0077`. Source `65b5d48d` pins the owned directory and newly built regular, singly linked executable, then narrows that file to `0700` while preserving inherited-fixture validation. The repaired fixture passes four selected tests under the ordinary mask, including cleanup, changed-inheritance refusal and a taxonomy control. These local results involve no real database or RPC.

Readable filesystem identity and free-space figures do not establish write health. Connect `fb43e776` makes initial writable admission and explicit `CheckWrite` perform a fresh, fixed-size anonymous write, file sync, close and root sync, then recheck identity and reserve. Failed or unsupported probes refuse admission; read-only inspection remains mutation-free. The merged `501c172d` passes 15 selected normal tests and two race tests. Kernel sync remains synchronous without a fixed wall-clock bound. The source review did not establish any bypass of preceding financial journal writes.

## Distinguish missing proof from conflicting chain evidence

A missing trie node after failed refill establishes incomplete execution evidence. It does not establish an absent storage value, archive corruption or a mismatched chain root. Capture-v2 retained that missing-node diagnostic and the RPC envelope refusal, but no raw failing HTTP/RPC body. Preserve that limit when diagnosing the service; retain bounded original status and reply evidence in future qualification.

The source review found that valid JSON from a transient HTTP gateway response could be classified as an RPC proof conflict before its HTTP status was considered. Distinguish unavailable transport evidence from a complete contradictory RPC reply, retaining hard refusal for authenticated parent/key or reply-identity conflicts. Absent proof material, null required results and transient transport replies must remain unavailable under the original retry budget. Decode and validate each retry into a fresh proof candidate so an omitted field cannot inherit a prior reply's parent or nodes; publish only a complete validated result. The focused correction results are recorded in the code/test checkpoint; no successful real replay is inferred.

## Derive transaction and storage trie versions separately

The [actual 15-extrinsic evidence](/home/by/sn-testnet-runtime473-capture-input-20261005/body-root-analysis-v1/ROOT-CAUSE-EVIDENCE.json) shows that all 6,918 original encoded bytes have canonical SCALE length prefixes and survive `OpaqueExtrinsic` decode/re-encode unchanged. Their calculated V0 ordered-trie root exactly matches the original header; V1 produces a different root. The parent runtime reports `systemVersion: 1`: pinned SDK `cacb4310` maps that to storage `state_version()` V1 and `extrinsics_root_state_version()` V0. Capture and replay incorrectly reused the storage version for the transaction root. The failure is in version selection, with no basis to rewrite the body, header or storage execution version.

Derive both versions independently from the authenticated parent runtime contract in capture and strict replay. Preserve each complete encoded extrinsic, including its length prefix. Keep the [original 15-extrinsic fixture](/home/by/sn-testnet-runtime473-capture-input-20261005/body-root-analysis-v1/actual-block-9218962.body-root-vector.json) as a regression case alongside synthetic fixtures, then qualify the corrected engine against this real epoch and its complete execution proof. Static root calculation and profile assembly establish neither successful runtime execution nor a Principal result. Preserve the original pre-VM failure and record any eventual successful capture/replay separately.

SDK conversion is not wire admission. `StateVersion::try_from` accepts raw version 2 as V1, while the existing historical job grammar admits raw versions 0 and 1 only. Check that declared scope explicitly before deriving either trie layout, and require exact equality with original `Core_version.system_version`; equal derived storage layouts cannot substitute for equal raw versions. Preserve the original failed assertion and the separate passing three-root successor recorded above.

## Continuous observation and qualification storage — October 5

The merged `e190a2eb4bd243871eb2ef114f67034baca8cb4f` successor implements bounded retention of incomplete principal originals. An explicit `principal_retention` policy lets the live checkpoint owner retain its exact original bytes between native attempts, before starting the next read. The compacted head keeps the incomplete cause status and original per-query sums; delayed capture checks hydrate one owned segment at a time. The complete-through cursor cannot cross an unresolved original, and spendable income remains unknown. Existing claim, capture and fee retirement predicates are unchanged. See the [operator selection and finite limits](ECONOMIC-CONSERVATION-STATUS.md#principal-original-retention).

This source correction has independent source review. Its 14 new roots and three existing adjacent roots are selected for focused qualification; their execution is pending here. Preserve the separate ordinary and actual capture/replay prerequisites. No source review supplies a passing body, approved principal scope, production slot configuration or permission to discard unresolved evidence. The selected global mutation prefixes retain foreign rows and shared-denominator effects; the actual observed batch still has to fit its original fact and byte limits.

Unsigned archive qualification must select its storage mode explicitly. Reusing the daemon constructor rejected the healthy internal-root fallback before real capture. The pending narrow correction selects the existing owner-local storage API only for an explicitly declared unsigned cache. Preserve daemon restrictions, exact path/filesystem custody, capacity bounds and one inherited execution deadline; never silently fall back to another filesystem. The original archive-command focused run continues separately from the new owner-local regression checks.

## Focused hardening results — October 5

The [source49 checkpoint](evidence/focused-qualification-source49-20261005.json) binds the completed focused results and their original source, image and wait records. The mainnet work uses SN `49ccc424b6dbe64166e28b3ecb87449f2c94774e`; the Server result retains its separately recorded compiler and model source scope.

| Completed scope | Exact retained evidence |
| --- | --- |
| Mainnet deployable binary and **21/21 normal Go roots**: metadata authority separation, recipient storage pairs, custody and exact call paths | [Closed Go owner and image pins](/home/by/sn-testnet-root-fallback-20261005/mainnet-next-v1/run/evidence/closed-user-readback-v1.json) |
| **18/18 Rust regression roots**: five capture, three metadata-scope and ten storage-call/recipient cases | [Closed Rust owner and original waits](/home/by/sn-testnet-root-fallback-20261005/rust-capture18/evidence/closed-owner-readback-v1.json) |
| **12/12 Server database roots**: earning-cutoff and ARIN behavior, with owned database/network cleanup complete | [Closed DB owner, source pins and cleanup](/home/by/sn-testnet-root-fallback-20261005/db12/closed-owner-readback-v1.json) |

These are focused passes, with the corresponding owners closed. Preserve the [original Rust nine-root result](/home/by/sn-testnet-root-fallback-20261005/rust9/evidence/original-failure-readback-v1.json), **8 PASS / 1 FAIL / 0 SKIP**, including the failed observer-only capture assertion; the corrected 18-root run includes that same root. The earlier [17-root static/profile result](/home/by/sn-testnet-root-fallback-20261005/rust/evidence/closed-owner-readback-v1.json) retains its own scope. Neither static profile checks nor these regressions establish actual runtime-473 block capture/replay, independent economic approval, deployment or activation. All historical counts below remain intact.

## Storage failure isolation — October 5, 20:19 UTC

The USB test-data SSD disconnected, returned under a different device name, then disconnected again. ext4 aborted its journal; free-space statistics still reported capacity while file reads returned I/O errors. All product jobs had completed before the incident. Canonical source on the internal SSD remains available. The [incident evidence](/home/by/sn-testnet-storage-incident-20261005T2020Z/HANDOFF.json) and [recovery readback](/home/by/sn-testnet-storage-incident-20261005T2020Z/root-recovery-readback.json) preserve the actual observations; no filesystem repair was applied.

Production hardening must check the mounted filesystem identity and read/write health, rather than accepting free bytes alone. An absent mount must not silently redirect financial data or caches onto the root filesystem. Keep the host coordination lock on stable storage and distinguish its holder generation from a removable volume's device/inode identity. Retire the previous holder after its workers close before rebinding recovery. Preserve completed receipts and signatures; recheck their bytes after recovery instead of replaying successful transactions. Isolate failed storage owners, continue independent work on a bounded healthy volume, and require journal recovery plus exact artifact readback before reusing the failed volume. A transport reconnect is not proof of filesystem health.

## Current reserve and economic decision — October 5

The user selected **10% of the native miner allocation for providers and 90% received by `ur-reserve` for future network improvements**. In current terms, providers receive **momentum**, the share of the native miner allocation paid to providers now, 10% at launch; `ur-reserve` receives the rest, miner emissions × (1 − momentum), so its share is not fixed. The signed treasury policy pins providers `1/10`, momentum's launch value, and treasury `9/10`, 1 − momentum at launch; changing momentum requires a newly signed treasury policy and approval. `ur-reserve` is the 2-of-3 native multisig and signs only to register its own recipient hotkeys; it registered SN25 UIDs 170 and 250 that way. The [treasury path](TREASURY-EMISSIONS.md) accepts the supplied native address and public recipient hotkeys without general reserve spending/signing qualification. This supersedes the earlier mandatory reserve-signing workflow and the owner-recycle choice. The treasury remains separate from provider claim collateral and the immutable, one-way `STReserveSink`.

The destination is `5CcHGEqKK3RXeEA2sVycHQAQGrqsyhWaYu9FjGtDVN6nwMwR`, whose valid prefix-42 encoding maps to AccountId32 `0x1815103f41a8d1e24c55d380c6f843fb36d715b4322a4e4f02bff36dfe74a410`. The [public destination descriptor](TREASURY-EMISSIONS.md#public-configuration-contract) uses schema `urnetwork-native-treasury-destination-v1` and only `profile`, `netuid`, `genesis_hash`, `account_id` and public `recipient_hotkeys`. Select its explicit absolute path with `--destination FILE --destination-sha256 HASH` for `treasury describe`, `observe` or `policy-plan`; observation and policy planning also require their public `--input`. If `vault/main/sn.yml` contains root keys or other private fields, extract a separate public-only descriptor and pin that snapshot. Never pass the combined vault file to these commands or alter the strict earnings schedule in `config/main/sn.yml`.

An empty recipient list can describe the known destination. Routing still requires at least two actual registered recipients under the existing cap, exact native `Owner`/UID/registration generations, owner-set exclusion, qualified runtime/profile and signed economic policy. The [native recipient setup note](TREASURY-RECEIVE-SETUP.md) records the selected path (owner decision, October 6): `ur-reserve` registers its own recipients through its multisig and pays their burns. That replaced the separately funded transfer and its **36,000-block announcement delay**, and `ur-reserve` registered SN25 UIDs 170 and 250 that way. Authenticate the resulting chain state after setup; no descriptor fabricates registration. `ur-reserve` is the confirmed **2-of-3 multisig**; the strict descriptor, account derivation and Ledger workflow of the optional sending-custody tooling remain available and unselected.

The transfer-based setup that the October 6 decision replaced needed this order: do not use an interim `transfer_stake` or `transfer_stake_and_hotkey` deposit to the reserve before the selected coldkey ownership swap. Incoming stake leaves the hotkey's `Owner` unchanged and can populate the destination's `StakingHotkeys`, which the native swap requires to be empty. Clearing that state through a reserve-origin transfer or unstake would violate the no-outgoing requirement. An existing mature source announcement or actual reserve-owned recipient can be reused only after exact chain readback.

Keep the 38 original requirements and every retained test count/evidence file intact. Qualify the selected reserve routing/approval and accounting/monitoring path; the optional reserve sending workflow is not a launch prerequisite. Earlier owner-recycle and multisig results retain their original scope. Earlier economic and mandatory reserve-custody statements below are historical. Owner, root and contract actions retain their own signing and authority requirements. Preserve the October 6 inclusive new-earnings boundary, pre-cutoff USDC obligations, owner-local Ledger/no Snow custody for those actions, the separate root role and `activation: blocked`.

## Runtime migration hardening — October 5

Original metadata pins and fee authority have separate scopes. The source correction validates generated metadata for native profiles without requiring unrelated Balances/Ethereum fee layouts; only explicit fee event callsites select fee candidates. A whole-fee authority still requires the pin, and native execution rejects unapproved fee paths. Historical nil metadata encodings and unselected fee/Yuma authority remain unchanged. The four Go metadata roots and three Rust metadata roots now pass within the [21/18 focused results](evidence/focused-qualification-source49-20261005.json). This qualifies their tested admission boundaries, not an actual runtime profile or signed policy.

Full Yuma input capture remains an optional, unimplemented profile extension for actual runtime 473. Enabling Yuma authority requires a complete authenticated census of settings, effective parent/child stake links, all UID weight and bond rows, and commit queues, joined to original epoch output and post-state. The epoch output vectors alone do not close that census. The enabled scope must account for suspended childkey relations, the explicit owner exception, dynamic activity cutoff and complete commit queue enumeration. No full-Yuma implementation or qualification is claimed, and an unselected Yuma authority adds no launch gate to the reviewed 10/90 receiving route.

The [reserve observation at block 9,219,009](/mnt/data/sn-testnet/reserve-sn25-recipients-20261005T200449Z/OBSERVATION.json) found an empty `OwnedHotkeys` list under the current runtime metadata. Keep destination identity separate from routing readiness: accept the supplied public account for describing a receiving destination, but authenticate actual registered hotkeys and their ownership before routing native emissions. An absent registry must not create fabricated recipients or a reserve-signing requirement. This observation is endpoint-finalized; retained proof bytes alone do not establish independent trie/GRANDPA verification.

The admission fixes through `a70dae9c` remove numeric-version admission from the affected owner, treasury and discovery paths while preserving reviewed source capabilities, exact runtime artifacts and independent approval. A signed native treasury authority can carry a bounded storage witness proving migration completion at its exact activation parent. Authenticate both the migration marker and runtime-code commitment against that parent's trie; reject incomplete migration, a different runtime, substituted later blocks and an unrelated principal parent. Historical authorities without this witness preserve their original encoding and may not inherit a newer migration marker.

The [selected runtime tests](/mnt/data/sn-testnet/sol-focused-recovery-61-20261005/sn473-normal-v1/evidence/sn473-normal.owner.receipt.json) passed 31/31, including 26 mainnet and five validator roots with actual retained assets and no skips. Actual Rust treasury exporter/consumer qualification remains pending. Production capture/replay still needs a reviewed actual runtime-473 execution profile; synthetic exporter mappings and function-name inventories are not substitutes. Preserve these separate requirements when preparing the activation proposal. No runtime number change alone should erase retained progress or require unrelated work to restart.

Runtime profile adapters must preserve where each value came from. The explicit single-mechanism storage join derives the epoch total from three original tranche returns, maps UIDs through original metadata-typed `Keys` gets, and uses the original epoch-counter write. Two bounded observer reads retain registration and generation from the same proof-backed execution overlay at the selected drain; they are separate from Wasm memory and runtime get records. Missing stack fields cannot be filled from later RPC snapshots. The corrected observer-only epoch capture root now passes in the focused Rust18 result; actual profile assembly, execution and independent approval remain separate obligations.

The storage adapter now matches a bounded exact leaf-to-caller path before reading memory. Recipient joins use original get/set pool deltas, explicit custody returns and actual zero-entitlement reads; fully captured rewards do not require a fabricated liquid credit. The recycle branch uses one bounded `Owner(hotkey)` observer read from its current execution overlay. Original recording capture must run the same observer as strict replay so its proof retains observer-only and dynamic storage paths with their original phase. It then discards the provisional trace before independent strict replay; a declared parent root or later RPC value cannot replace a missing original path. The affected capture and recipient regressions now pass in the focused Rust18/Go21 results, including the original failed capture assertion. Original metadata pins no longer implicitly select fee accounting; whole-fee and full-Yuma authority remain explicit optional scopes. These passes do not qualify an assembled production runtime profile or supply its approval.

## Local dependency validation and retained results — October 5

Durable owners must admit their actual physical compiler inputs while preserving independent peer progress. The [owner-local input guard](evidence/qualification-owner-local-inputs-source-readback-20261005.json) passes twelve deterministic cases: each owner retains immutable adopted job, runner and source pins; the shared plan retains exact command paths, USER roles, units, cgroups and caps. An authenticated complete package/native/embed/module scope compares physical regular-file bytes and modes with admitted Git blobs, including ignored or index-hidden files and missing or moved inputs. Selected and ancestor symlinks are refused, unrelated subtrees are pruned, and nonblocking descriptor checks reject replacement races. Repreparing an unstarted peer must not invalidate a healthy owner. Keep original generation, resource, executable and joined-wait checks; this helper neither discovers a dependency graph nor transfers old qualification to new inputs. Its earlier six-case receipts remain separate, and runtime adoption remains a caller-specific obligation.

**Current execution priority — after the October 5, 16:26 UTC checkpoint:** continue deployable builds and useful parallel focused validation, then remaining suites. The [closed four-attempt checkpoint](evidence/focused-window-terminal-and-treasury-status-20261005.json) preserves completed artifacts and unfinished work; it does not establish overall readiness. Full-catalog completion is not a prerequisite for focused work. Canonical config `9c4a3435` still blocks activation with empty deployment, contract, policy and readiness fields; software passes and the implemented treasury path do not supply those authorities.

The current [38-requirement evidence ledger](evidence/original38-current-evidence-ledger-20261005.json) separates implementation, normal/race/control evidence and external authority or rehearsal requirements. All original outcomes remain open; this does not mean there are 38 code defects. The [latest native-fee result](evidence/nativefee-and-bounded-copy-progress-20261005.json) passes all **13 roots normally and all 13 with race detection**, with zero failures/skips, joined waits and no resource errors. These are actual default-module results on the corrected `47003d6e` component. Its 2,017 pinned input files remain unchanged at `4991966f`; preserve those results without repeating the positive bodies solely for unrelated later changes. Native-fee causal controls and downstream consumers remain separate. The [retired-wallet payment correction](evidence/original-native-and-payment-progress-20261005.json) is merged in Server `d64c9beb`: retry only the durable original request and key, preserving fresh-wallet and earning-cutoff admission. Its ten new and two changed tests, plus eight controls, still require execution.

The defect was a promoted `bytes.Buffer.ReadFrom` method: `os/exec` copied output through that method and bypassed the guarded `Write`, including its byte cap and cancellation hook. Existing owner-cancellation commit checks were already present. The merged fix uses a private named buffer so output passes through the guard. A separate fixture correction owns a protected copy of the temporary test executable without relaxing production admission. Preserve the original [ten-pass/one-fail run](evidence/nativefee11-normal-result-20261005.json) and the later five-pass/eight-fail results in each mode; the current 13/13 results follow those distinct corrections. The merged mainnet fixture likewise preserves an already protected engine's exact path and hash, copying only an inadmissible temporary test image. The selected seven-root mainnet fixture group, including all three new custody roots, now passes normally and with race detection in the [joined adjacent-batch results](evidence/current-main-qualification-progress-20261005.json). This does not qualify the full mainnet package.

The [process recovery suite](evidence/process-recovery-tooling38-result-20261005.json) passes 38 tests, and the [expanded peer suite](evidence/nativefee-and-bounded-copy-progress-20261005.json) passes 50 in one joined invocation; the earlier 44-test result remains retained. These regressions cover recovery after the [compiler observation failure](evidence/default-server-compiler-interruption-20261005.json) and [independent peer refusal](evidence/independent-peer-interruption-20261005.json). Preserve original PID/start identities, durable waits on exceptional exits, and release a stopped peer's resource reservation independently of its test outcome. The optional immutable-source helper’s bounded-mount correction is merged. Its [34-test follow-up](evidence/original-native-and-payment-progress-20261005.json) retains **33 PASS / 1 FAIL**. The corrected assertion now passes in the [current tooling38 run](evidence/current-compilers-and-ph15-producer-normal-20261005.json), alongside ten interpreter and 27 ChildContext tests. A later duplicate launch stopped before any test body; the original 38 passes and joined wait remain intact. This is not a new full34 rerun. Preserve the earlier 31 failed outcomes. The helper and its 18 controls remain separate from active jobs, which continue full hashing; this does not delay product qualification.

The merged output-bound fixes add four release-builder and four inventory-command roots. The [adjacent batch has joined](evidence/current-main-qualification-progress-20261005.json): release-builder normal and race each retain **93 PASS / 2 FAIL / 0 SKIP** among 95 qualifying roots, while `arinshadowctl` and the selected replay group pass all **13 and seven roots respectively in each mode**. The [two release-builder failures](evidence/unix-socket-harness-diagnosis-20261005.json) arose in `net.Listen` before the intended assertions because the harness temporary path exceeded the Unix socket pathname limit. Only those two roots per mode were rerun with a short, fresh, owned temporary directory; **all four targeted invocations passed**, with unchanged source and assertions. The original 93 passes plus the two passing retries cover all 95 qualifying roots per mode. Preserve the failed original batch and the separate retry receipts; this is a union of results, not a new full-package rerun.

Size data-volume capacity from the non-root account’s available bytes after filesystem reservations. The [recorded `/mnt/data` reserve adjustment](evidence/data-volume-reserve-headroom-20261005.json) reduced this dedicated ext4 volume’s root reserve from 5% (about 100 GB) to 1% (about 20 GB), releasing 80,015,912,960 bytes to ordinary users. Record an explicit, reversible reserve policy for non-root data workloads; temporary-file cleanup cannot release reserved blocks. The original capacity baseline/cap, per-owner growth limits of 8 GiB and 2× forecast margins remain unchanged. Test and deployment status are unchanged.

The [merged start-window correction](evidence/repair-dispatch-window-source-and-scope-20261005.json) in `8776b038` rechecks signed authority after successful storage inspection and immediately before the fixed process-start command. A slow inspection or original census must not carry an expired start permission into dispatch. Preserve the original inspection failure, the appropriate wall/monotonic and observation-age bounds, and the durably consumed one-shot start. Its ten new tests and six controls remain unexecuted; fourteen existing adjacent tests outside the master catalog remain explicit pending completion obligations. The finite contract audit found no confirmed source gap in its reviewed inputs; actual deployment authority and pending qualification remain separate. Subsequent merged corrections and their qualification limits are recorded below.

The [merged runtime-identity correction](evidence/runtime-identity-source-and-scope-20261005.json) in `1d4251f4` compares each completed name, genesis and EVM identity before the next RPC. A completed contradiction remains a hard refusal even if a later read times out; null/empty replies remain unavailable and malformed replies remain hard. All five public readers retain their original read budgets. Its seven new validator tests and eleven controls are unexecuted; 28 existing adjacent tests outside the master remain separately required.

The [production-boundary clarification](evidence/production-boundary-source-and-scope-20261005.json) in `03b94755` confirms that the current schema-3 V2 measured, signed and durable submission path is implemented. Legacy observation and unsigned bootstrap review plans retain their intentional refusal to authorize execution. Only diagnostics and tests changed; newly emitted unsigned review-plan hashes can change. Two new validator roots, one changed existing mainnet assertion and three controls remain unexecuted.

The [merged PH-12 and ARIN checkpoint](evidence/ph12-auth-arin-source-and-scope-20261005.json) binds SN `5e71e63c`, Server `67467b26` and SDK `e3f4f3fb`. Claims use the provider credential and original eligible contribution; earning-wallet authority requires explicit prospective consent, not a generic login proof. Retain and sync the exact signed consent before POST, then replay that original after a lost acknowledgement without replacing its endpoint or identity. The SDK now decodes Server decimal-string `no_id` exactly into its existing `int64` field, while preserving legacy integer input and refusing malformed or partial results. The 20 new miner and 16 new Server roots remain unexecuted. SDK has a separate 15-root obligation: 12 new HTTP/unit roots, two existing API neighbors and one generator-currentness root under the nested `sdk/cgo` module. The current SDK normal result below covers its fourteen root-module obligations; the nested generator and SN/Server consumer qualification remain separate.

The [merged native-upload finality correction](evidence/upload-finality-source-and-scope-20261005.json) in `02e507ee` retains the selected block and first/highest finalized witnesses through replacement and nested reads. It treats authentic lower-only coverage as temporary unavailability, checks fresh closing finality after timestamp storage, and keeps completed canonical or identity conflicts hard. The original 300-second owner and shorter caller deadlines are preserved. Eleven new validator roots, one changed existing canonical-switch fixture and seven controls remain unexecuted.

The [merged EVM decision-finality correction](evidence/evm-decision-route-source-and-scope-20261005.json) in `516b397a` gives opening, financial and closing reads one original 300-second owner, with a 60-second ceiling for each physical header or the shorter caller deadline. Authentic lower or missing finalized coverage retries header evidence without replaying financial reads. Completed canonical contradictions remain hard, and errors cannot publish a partial decision. Its sixteen new validator roots, changed existing deadline fixture and thirteen controls remain unexecuted. The adjacent [route-identity correction](evidence/route-identity-and-skip-inputs-20261005.json) is now merged in `ee81380c`: opening and closing checks compare each completed chain ID and genesis before another RPC can obscure it. Null replies remain unavailable; present malformed or contradictory identities remain hard. The original 300-second owner and 60-second physical-read bounds are unchanged. Its nine new validator roots (56 cases), six existing neighbors and seven controls remain unexecuted.

The [retained Core224 normal run](evidence/focused-core-server-progress-20261005.json) has joined with **222 PASS / 2 FAIL / 0 SKIP** and no resource errors. Its earlier metadata/output namespace collision refused before any LIST or test body. Preserve both records. The H1 probe timer could mask a physical timeout with a closed-pipe cause, and the HTTP cause walker could admit callbacks before charging the complete child frame. Both corrections are merged in Connect `11f99c8f`; all twelve new roots and both original failed roots now pass in separate fourteen-root normal and race runs. Their eleven controls remain unexecuted. The legitimate upstream Core changes receive no pass credit from the older `026b8502` image. Published SDK `18fca2ed` includes the Hint callback fixture correction and legitimate upstream performance-profile validation; no additional stricter setter patch is needed. Its focused 23-root normal run now passes; SDK race, generator-currentness and twelve controls remain pending. SN `494c65be` adds two wallet-child diagnostic tests, retained as separate focused obligations without increasing the product master or crediting the old unobservable control.

The [merged Server fixture corrections](evidence/focused-core-server-progress-20261005.json) preserve the original populations and assertions: the opt-in subscriber benchmark keeps 20,000 providers and 240,000 connections, Stats keeps 160,000 connections and its sixteen query-plan calls, and setup commits bounded 512-row groups. They add one regression root and three controls; the benchmark reuses existing roots and ARIN controls. The paid/free failure came from comparing terminal originals before the real legacy worker completed, not a production weighting change. Its corrected fixture and new public Redis sibling add one root and two controls, all unexecuted. The original frozen model failures and skips remain recorded in the closed, incomplete run described below.

The merged retention correction hardens only the **existing optional MinIO/S3-compatible store**: startup paths apply the complete owned rule set for a shared endpoint and bucket. It adds no AWS service, dependency or required mainnet storage infrastructure. Missing or disabled storage remains a no-op, and startup application stays best-effort in the background. Its six new feature tests, nineteen existing neighbors and seven controls are separate from launch requirements and remain unqualified.

**Retained focused results:** [the same fourteen Core roots pass normally and with race detection](evidence/focused-sdk23-core-race-and-build-lessons-20261005.json) on `11f99c8f`, with zero failures/skips and joined, closed owners. SDK `18fca2ed` / Core `11f99c8f` passes all **23 focused normal tests**; SDK race, generator-currentness and twelve controls remain pending. Server `62104c48` retains its successful model-test compiler result. These results remain separate from the following incomplete attempts.

| Final-window attempt | Actual result and remaining scope |
| --- | --- |
| Deployable build | Server `go build ./...` was cut off after **221 seconds**, exit −15/joined. All four SN binary builds remained unstarted. No deployable-build pass is established. |
| SN focused normal3 | Mainnet compilation and full **2,413-root LIST passed**. Its 34-root body received only **three seconds**: **2 PASS / 1 SKIP / 1 timeout-failure / 30 unrun**. Validator compilation was cut off after 55 seconds, exit −15/joined; miner never started. Retain the mainnet image and LIST; the package scope remains incomplete. |
| Forge carry3/control1 | The 53-file build passed in **65.83 seconds**. LIST triggered ten more files of compilation and hit its **20-second timeout**. Neither the three positive bodies nor the control ran. |
| Server cancellation6 | The peer observer refused database startup before LIST or bodies. The child joined and exact owned PostgreSQL/network resources were removed. All six tests remain unrun. |

A readiness ETA is a reporting estimate, **not a timer that should kill a healthy phase**. Give compilation, implicit compilation during listing, and test bodies adequate physical timeouts with cleanup and resource allowance. Preserve completed artifacts and original failures, and continue unfinished work after the checkpoint; do not shorten a test to the few seconds left in an estimate.

Four subsequent deterministic DB-observer regression cases pass in the [separate harness receipt](evidence/focused-window-terminal-and-treasury-status-20261005.json). They execute no database integration or cancellation6 body and do not clear that unfinished scope.

The original mainnet34 compiler failed on the unused `encoding/hex` import; merged `802f9862` removes it, and the corrected mainnet test compiler now passes as recorded above. Deployable binaries still require successful builds. Compile them early to expose shared-package failures before expensive fixtures and catalog work. Preserve the earlier SDK pre-stage refusal caused by an extra `adopted_by` annotation: validate substantive approved fields exactly while permitting declared provenance annotations that cannot alter approval.

The original **1,865-root model run has closed with incomplete coverage**. Its 64-MiB output guard stopped the child before natural package completion; the Go test timeout did not expire. The unchanged raw stream records **1,813 RUN / 1,789 PASS / 7 FAIL / 10 SKIP**, plus **seven started roots without terminal outcomes and 52 never started**. Nested events retain 323 RUN / 322 PASS. The owner, process tree and owned database resources are closed. The original guarded body receipt keeps its empty count field; a separate executor audit supplies these partial observations. None of this qualifies the full suite or later source. Preserve all seven failures and ten uncovered skips, and do not restart the old full suite for bookkeeping.

The seven failures comprise three ARIN population fixtures, Stats population setup, the FP2 expectation, the paid/free worker fixture and the AsyncDebit page-cancellation boundary. The first six have the previously described source corrections. The [reviewed AsyncDebit correction](evidence/focused-compiler-and-model-stop-progress-20261005.json), source `24f75908` merged as `3ef7bd03`, now recognizes complete permitted joined-cancellation causes only after owned completed progress; adjacent legacy settlement preserves hard siblings and the prior cursor. Its financial and replay assertions remain intact. Two new roots, four retained debit roots, adjacent tests and nine controls are explicit pending obligations; no corrected body or control pass is claimed.

Keep full-suite log forecasts proportional to measured output, and process event streams without losing the distinction between observations and retained bytes. This run observed 67,108,865 bytes but retained 67,107,457 because the guard rejected the crossing chunk before writing it. The [separate harness qualification](evidence/focused-core14-and-current-server-compile-20261005.json) now passes ten log-budget, forty peer-observer and seven closed-owner tests under bounded memory and time. This preserves the old failure and supplies no full-suite or product-control pass; future jobs retain their own disk safeguards. The old eight-skip input snapshot remains historical; the final audit also records the subscriber benchmark and `auto_explain`-dependent query-plan skips. All ten remain uncovered. The active 401-byte `pro.yml` exists with inert values; apply the approved nonzero product/referral and onboarding inputs only to a fresh owned fixture.

The earlier [mainnet/miner compiler and PH-15 producer32 normal passes](evidence/current-compilers-and-ph15-producer-normal-20261005.json), [eight Server compiler passes](evidence/repair-dispatch-window-source-and-scope-20261005.json), and **113/113 Rust tests with 61 retained exports** retain their original input tuples and limits. Race tests, causal controls, current Go native86 consumers and remaining bodies are separate. These results do not transfer to changed source simply because it is on main. All 38 original outcomes and preserved scope counts remain intact.

The [finite launch-path review](evidence/focused-compiler-and-model-stop-progress-20261005.json) found no missing adapter on the selected existing-root-seat, reviewed-470, owner-local Ledger and approved two-UR-start route. Actual owner/device approvals, identities, funding, signed configuration and installed deployment authority remain required inputs. Three complete emission intervals and a full 50,400-block settlement/claim cycle are postactivation acceptance outcomes. The review does not grant a new root-lifecycle signer or authority for an unreviewed runtime version. Later historical sections retain their dated results and do not override this current checkpoint.

The [PH-13 source audit](evidence/server8-and-native113-progress-20261005.json) confirms the intended historical cancellation limit: current production has no authorizing cancellation operation or original cancellation clock. Retain canceled rows with unknown attribution; report ingestion, zero-byte cleanup and context cancellation cannot invent that authority. This audit adds no new source fix or qualification gate. The [PH-15 observer and dashboard extension](evidence/ph15-source-implementation-and-scope-20261005.json) is merged, and the same **12 targeted dashboard tests pass** with no failures or skips in the [latest 8.732-second run](evidence/focused-core14-and-current-server-compile-20261005.json). The earlier 6.981-second pass remains separate; this repeat adds no unique test coverage. It preserves the original 36 financial gauges and adds 36 coverage/progress scalars plus acknowledged operational status. Preparation throughput, local finalized-observation cadence and catch-up ETA to an admitted boundary stay distinct; future/repair estimates and unsupported facts remain unknown. The result used actual Promtool and SN source checks, and the process closed cleanly. All **32 Go producer normal tests now pass** with zero failures, skips or resource errors. The 32 race tests and four controls remain unrun; the prior conservation33 pass remains separate. Earlier YAML/Ansible loader failures are retained; the successful run used an existing qualified virtual environment without source changes or dependency installation. Other owners’ transaction/incident records and live ingestion, on-call delivery and outage drills still require their own evidence.

The [large-scale coverage review](evidence/large-scale-coverage-gap-20261005.json) identifies five required roots omitted from the 2,503-root selection: Claim129, Restore129, the 2,048-UID physical-head/archive/restore case, and both large-profile witness restoration cases. Preserve the existing selection and controls; add these roots explicitly or prove that their retained results apply to identical inputs. Historical Claim129 passes took about 1,694 seconds normally and 8,152 seconds with race detection. Keep separate adequate mode budgets and durable per-root results. The selected 1,024-UID case contains two reads each admitted for 900 seconds, so its previous 900-second whole-root timer can cancel healthy work. Its reviewed normal harness bound is now 3,600 seconds, with a 3,720-second child limit. This changes execution bounds, not assertions or census scale; execution and race qualification remain pending. The [explicit scale supplement](evidence/current-mainnet-scale-supplement-20261005.json) preserves the 2,503 existing roots and appends all five in both modes: a 2,508-root baseline, including 613 SN/Connect/VLESS roots. The [current selection](evidence/original-native-and-payment-progress-20261005.json) preserves that baseline and restores all **86 original native-readiness tests** omitted from the recent incremental selection. None overlaps the earlier 2,637 roots; together with the ten payment tests, these form the preserved 2,733-root baseline. The [PH-15 scope addition](evidence/ph15-source-implementation-and-scope-20261005.json) adds 17 new tests and restores five existing neighbors absent from that baseline. Its other ten existing neighbors are already counted. That 2,755-root selection is preserved. Restoring [two previously requested payment neighbors](evidence/current-compilers-and-ph15-producer-normal-20261005.json) forms the preserved 2,757-root baseline. The [start-window addition](evidence/repair-dispatch-window-source-and-scope-20261005.json) contributes exactly ten new mainnet roots. The [runtime-identity addition](evidence/runtime-identity-source-and-scope-20261005.json) adds seven validator roots; the production-boundary clarification adds two more, forming the preserved 2,776-root baseline. The [native-upload addition](evidence/upload-finality-source-and-scope-20261005.json) preserves the 2,787-root baseline. The [merged PH-12 and ARIN checkpoint](evidence/ph12-auth-arin-source-and-scope-20261005.json) adds 37 owned roots, forming the preserved 2,824-root baseline. The decision-finality and route-identity additions preserve the 2,849-root baseline. The twelve Core and two Server fixture additions bring the selection to **2,863 Go roots per mode**: 573 SN (302 mainnet and 104 validator), 236 Connect, eight VLESS and 2,046 Server (101 controller, 1,868 selected model and ten new `api/handlers` roots). This maintained model selection does not yet include any additional declarations from the separately reviewed upstream Server delta. The 116 existing adjacent obligations outside the master include the six route neighbors. Those obligations and the separate SDK 15 remain required by exact package/name recipes. The upstream review separately retains 51 relevant roots: 18 already selected and 33 additional obligations; three diagnostics are excluded from new mainnet scope. All prior 290 control groups remain; 33 SN, 27 Server, three ARIN fixture and four FP2 groups form the preserved 357-group baseline; thirteen decision-finality and seven route-identity controls preserve the 377-group baseline; sixteen Core/Server correction controls bring the operative catalog to **393 groups**. Reused ARIN controls are counted once. One additional SN redundant-defense diagnostic and the SDK’s twelve controls remain separately recorded; the diagnostic is excluded from expected causal kills. The two original native Rust controls were already counted. The four runtime-renewal consumers retain their actual five-original fixture prerequisite. The [native fixture map](evidence/server8-and-native113-progress-20261005.json) now binds all nine profiles with 38 original files. The full Rust suite retained the two distinct generic producer/conservation profiles with five original jobs each; these are separate from the renewal jobs. The Go native86 consumers still require their own execution. These counts describe selection, not execution passes or complete requirement coverage.

The [finite contract input comparison](evidence/ph12-auth-arin-source-and-scope-20261005.json) retains the actual **226 Forge passes across 20 suites** and the two five-contract builds under their original source/tool/library receipts. Current Solidity and literal build inputs match; unrelated Go changes do not require repeating those results. The three later carry roots already passed, and the original prior-carry omission control produced its required assertion failure. The [retained carry checkpoint](evidence/carry-treasury-qualification-20261006.json) pins those closed results and verifies all 1,658 current Solidity inputs with zero mismatches; no Forge rerun was needed. The historical recovery causal pair and four catalogue Go controls keep their exact scopes. None supplies deployed artifacts, native 10/90 proof, signing authority or live acceptance.

SN main `9c7a2842` now selects all twelve replacement modules from local paths, including Connect, SDK and Connect's SCTP fork. Server main also uses sibling substitutions. Keep external dependency versions pinned. Validate ordinary module resolution with `GOWORK=off`, using the actual sibling checkouts and their module sums; a workspace can mask obsolete published dependencies and therefore cannot establish that the declared module configuration builds. Record each sibling revision with the release so local substitution remains reproducible. Current sibling changes require affected validation; unchanged components retain their original evidence.

The [nine-package normal compiler result](evidence/main-nine-normal-compiler-result-20261005.json) passes all nine phases on the earlier SN `babb0af5` / Connect `7ca8e222` / Server `3e1fe2d7` workspace tuple. Those retained images support test execution on those exact inputs. They do not validate the newer sibling-default graph, test bodies, race behavior or deployment. The [corrected Rust libtest compilation](evidence/corrected-rust-renewal-compiler-result-20261005.json) now passes and joins cleanly, with no resource errors. The [targeted renewal test](evidence/corrected-rust-renewal-body-result-20261005.json) now passes with no skips and retains all five previously missing exports, independently hash-verified. The earlier 37 passing roots and original failed result remain retained. The [read-omission control](evidence/renewal-read-omission-control-result-20261005.json) now rejects the intended missing observation with the exact causal assertion: its child exits 101 with one expected failure, while the control runner completes successfully. The subsequent full 113-root Rust normal suite passes as recorded above; its original causal controls and the current Go native86 consumers remain pending. The monitoring conservation scope has passed as recorded below.

The [ordinary local-module checks](evidence/default-local-module-result-20261005.json) completed all ten source stages and four dependency graphs with joined waits and no resource errors. All four graphs failed: SN and Server lack checksums for the newly imported TLS dependency, while the offline cache lacks Connect and VLESS dependencies. The declaration and checksum correction is pushed in SN `fcfd5acd` and Server `c1982631`; local replacements and existing external selections are preserved. The [corrected read-only default graphs](evidence/readonly-default-module-result-20261005.json) now pass in all four contexts: SN 1,105 packages, Connect 468, Server 1,076 and VLESS 576. All five staging/list waits exit zero and join, with no resource errors. This resolves dependency preparation; compilation and product behavior remain separately unverified. A complete local sibling layout does not establish complete module sums or cache availability. Keep dependency preparation distinct from compiler and runtime results, and collect independent graph failures before fixing them.

Keep terminal receipts small and bind bulk evidence by hash. The dependency job completed all eight download/proposal commands with joined zero exits, then its final receipt exceeded the one-MiB serialization bound because it embedded 1,735 module records. The [compact recovery checker](evidence/authenticated-dependency-recovery-result-20261005.json) now passes and verifies all eight original phases and 1,735 download records, without repeating commands. Original outputs and waits remain authoritative; the failed original wrapper and absent final sample are retained. Completed commands must not be repeated solely because terminal serialization failed. A recovered summary must distinguish actual command success from its failed original wrapper and from later read-only build qualification. The [fifteen deterministic tooling regressions](evidence/hydration-tooling15-result-20261005.json) now pass in one actual invocation, exit zero with the child tree joined. They exercise compact receipt limits, corrupted or incomplete originals, bounded graph errors and retained-output refusal; no dependency or product commands were repeated.

The concurrency adapter's actual thirty-test run completes **29 PASS / 1 FAIL / 0 SKIP**, with no resource errors. A generation-refusal test expected a different refusal diagnostic. The source correction is merged in `5f377dd5`: reject a changed nonempty invocation before the stopped-process reconciliation branch, preserve original generation records, and add deterministic blank-invocation and revival coverage. The [corrected canonical-main suite](evidence/joint-adapter33-main-result-20261005.json) now passes all 33 tests, with joined waits and no resource errors. Preserve the original failure. Parallel product jobs still require their own exact inputs and resource bounds. Process census reads must also tolerate a process exiting between reads, while checking the same PID and start time so an observation retry cannot adopt a replacement process. An earlier wrapper refused before launching tests because unrelated stopped Docker containers existed. Admission now checks running workloads and the exact owned database resources. Historical foreign stopped containers are not active resource consumers and must not be deleted to make qualification pass. Resource and process checks must protect owned work while allowing unrelated, independently bounded validation to continue.

## Code frozen on main — October 5

**Retained publication checkpoint:** the [code freeze manifest](evidence/main-code-freeze-20261005.json) records SN `babb0af5`, Connect `7ca8e222`, Server `3e1fe2d7` and xops `482050c3`, with independently checked remote heads. This is a code freeze, not a verified release or deployment.

Verification at that freeze selected these combined main versions; subsequent source and dependency corrections are recorded above. Collect ordinary failures across the complete normal, race, model and causal-control scopes; fix them in targeted commits on main. Retain successful results when their complete source, dependency, tool and configuration inputs are unchanged. Do not require separate branch qualification before merging reviewed fixes. Deployment and signing retain their own approval and readiness requirements.

All seven compilers on the retained SN475/Core07/Server87d inputs passed. The [combined-main dependency result](evidence/published-main-metadata-result-20261005.json) now verifies the exact frozen main archives, checksum-pinned published gvisor dependency and three offline import graphs: seven phases exited zero and joined, with no resource errors. This establishes dependency resolution, not test behavior. Verify the main package images and execute the complete normal/race selections, including all 1,824 selected Server model roots on `c1982631`, without repeating the smaller model subset separately. The [current source coverage](evidence/current-mainnet-go-coverage-20261005.json) retains all prior selections and adds the affected neighbors: 2,503 Go roots per normal/race mode, with all 227 existing controls retained. Reuse older images only where complete compiler inputs match. The historical results and all 38 outcome requirements below remain intact.

The [retained 38-root Rust run](evidence/main-rust38-result-20261005.json) completed 37 passes and one failure, with no skips; all processes joined with no resource errors. It retained 24 of 29 planned exports. The runtime-renewal fixture counted its read/write host-observation pair as one principal operation. The corrected targeted rebuild, renewal body and read-omission control now pass; all five exports are verified, as recorded above. Preserve the original failed receipt and the separate targeted results. The subsequent full 113-root normal suite pass is recorded above; original causal controls and Go consumers remain separate.

### Distinguish host observations from committed monetary effects

A runtime hook spanning a complete principal function can observe both its storage read and write. Preserve both original records, join them to the independent mutation census, and validate amounts and opening/closing stock. Monetary projection must select effect-bearing operations rather than counting every hook record as a deposit. Deterministic renewal tests must exercise original replay, capture and reduced replay across the upgrade boundary, including withdrawals and refunds. A fixture count failure warrants a fixture correction when the original execution and production projection agree; retain the failed run and prove the corrected assertions.

The [canonical-main monitoring result](evidence/main-xops-conservation33-result-20261005.json) passes the corrected control, then all 33 conservation roots with no failures/skips: 34 invocations, 33 unique tests. The process joined and resource checks passed. Preserve the original 32-pass/one-fail result; production alert ingestion and delivery remain separate launch checks. The Rust fixture correction is pushed in main `b13a4225`; its targeted normal rebuild, renewal body and read-omission control now pass as recorded above. Remaining Rust and Go consumer qualification stays open.

## Retained qualification checkpoint — October 5, 04:47 UTC

**The October 5 01:00 UTC deadline was missed. All 38 original outcomes remain open.** This retained [source/receipt checkpoint](evidence/carry-compiler-checkpoint-20261005.json) records status at 04:47 UTC. Its pending and unqualified entries describe that checkpoint; the opening section records subsequent results. No deployment, live signing authority, October 6 transition or final acceptance is established.

| Result at the 04:47 UTC checkpoint | Action and scope limit recorded then |
| --- | --- |
| Server `4b917455` on SN `8e010496` / Core `25a4ce7f` passes **37 selected normal roots, zero failures/skips**, including all 19 previously failed model roots. Compiler/body and outer database waits exited zero/joined; resource qualification and cleanup completed. | Preserve original `c60` **1,703 RUN / 1,673 PASS / 19 FAIL / 11 allowed SKIP**, exit one with cleanup complete. Eleven failures exposed the signed-domain NUL/`jsonb` production defect; eight were fixture defects. `4b` preserves exact signed bytes, uses a canonical-prefix projection for indexing, corrects fresh 773/775 and appends 779. Qualify remaining migration, normal/race/vet and operative-control scopes; 37 passes do not establish a newer-source full-suite pass. |
| Owner/CLI/custody component on exact SN `8e010496` / Core `25a4ce7f` / Server `2f8a6ee4`: **75/75 normal PASS, zero skips**, formed from disjoint nine plus 66 roots. | The five original `178` failures remain recorded; their corrected roots now pass. Race, independent controls, actual owner-local Ledger replies and separate root/UR authority remain open. No later source inherits this result. |
| Ordinary SN8e/Core25a/Server2f8 funding remains **38 RUN / 37 PASS / 1 FAIL / 0 SKIP**; all 13 new joint-flow roots pass. Later [selected338 native carry](evidence/selected338-native-carry-result-20261005.json) finishes **3 RUN / 2 PASS / 1 FAIL / 0 SKIP**, exit one/joined: both funding consumers pass; chronology/archive still fails. | Source-only `47566b25` corrects the fixture: a 6% claim does not force capital use. The 99% leaf uses a 49-unit claim and 61-unit payment to require at least 13 capital units; a new small-credit neighbor retains unknown capital use through archive/restart. Production is unchanged; execute corrected/adjacent roots and controls, retaining both original failures. |
| Monitor `9430262c` on SN8e/Core25a compiles, then completes **17 RUN / 15 PASS / 2 FAIL / 0 SKIP**, exit one/joined, resource-qualified with cleanup complete. Fault fixtures name a nonexistent constraint (`42704`) and try to disable a protected system trigger (`42501`). | Four-file source child `ec3feae1` corrects both fault fixtures and adds canonical same-endpoint/hash close-expiry reads plus four roots. Execution remains pending; preserve the 943 result of 15/17, prior 7885 compile failure and 780 monitor/catalog, race and full-model qualification gaps. |
| On SN `88bd7956` / Core `07fe87bd` / Server `ec3feae1`, metadata passes 45 blobs/three graphs. The [four public compiler phases](evidence/latest88-public-compiler-result-20261005.json) finish: SN `nativefee`, Server root and `cli/stnativefees` **compile PASS**; `controller` **compile FAIL** on two stale fixture fields. All phases joined with no recorded resource errors. | New SN `47566b25` / Core `07fe87bd` / Server `87d278f5` is test-only corrected source: six stale references now use `ClosedWork.UsageBytes` (two Server/four SN), preserving all asserted totals. Source ACKs do not establish compilation; successor metadata/compiler and normal/race/control bodies remain pending. Older successful images stay bound to their original tuples. |
| Exact SN `3388840a` / Core `87e1bf9f` / Server `1ea80fa2` now passes **30 normal fee-settlement model roots, zero failures/skips**: 26 SQL/ledger roots and four public proof consumers using the 24 selected exports. Body and outer database waits exited zero/joined; resource qualification and cleanup completed. | Server root-package/controller/CLI, standalone nativefee tests, monitor/catalog, race, independent controls, full model and remaining Rust/Go carry bodies remain open. Unknown or unmappable contradictions retain liability; admitted settlements require all seven originals. No default conversion or production authority is inferred. |
| Published xops `a1fdda0d` retains 311/311 PASS. Clock-alert `2f1043a3` focused 33 now finishes **32 PASS / 1 FAIL / 0 SKIP**, exit one/joined, with no recorded resource errors. Its control expected `got: []`; actual Promtool printed `got:[]`. | Test-only `482050c3` accepts whitespace only within the anchored empty-result field and rejects nonempty/embedded/trailing diagnostics. Production renderer and 33/318 identities are unchanged; corrected execution remains pending. Preserve the actual failed control and all live ingestion/delivery gates. |

Exact SN338/Core87/Server1ea metadata, normal mainnet/model compilers and [one three-case proof exporter](evidence/native-fee-exporter-result-20261005.json) pass; all 24 exported files are hash-verified and protected. The exporter uses its own executable as a synthetic nested peer with real signed-receipt/GRANDPA checks. The [30 selected model roots](evidence/server1ea-native-model30-result-20261005.json) now pass within the scope above. [Exact selected 338 Rust receipts](evidence/selected338-rust-assets-result-20261005.json) establish successful `runtime-historical-capture`, `runtime-historical-proof` and library-test image compilation, exit zero/joined with no resource errors. One principal-export root passes with four protected fixture files, **zero failed/ignored and 112 tests filtered out**. This is one exporter result; the full Rust suite, corrected carry/archive root and independent controls remain unqualified. Neither these fixture results nor compilation establish mainnet deployment or production authority.

The source-only recovery regressions retain one captured cause graph through producers, preparation, steering and cancellation; inspect actual DNS children before availability flags; preserve hard Path/Link errors; and bound concrete geth revert inspection. Catalog projections require execution and do not prove a privileged production rehearsal. Compile fixtures against the actual public API: source ACKs are not compiler passes. Capital in an original source does not prove that every partial payment used capital. Retain observed pass/fail counts on Unix exit one, join the failed scope, and continue unrelated independently admitted work.

The complete PH-01–PH-28/MG-01–MG-10 ledger still governs recovery/custody, nonempty ingress-to-payout, native conservation, both validator roles and production rehearsal. October 6 governs new earnings; pre-cutoff USDC obligations remain payable. Published config `9c4a3435` remains blocked with five empty mainnet fields: `deployment_id`, `coordinator`, `settlement_vault`, `policy_hash` and `readiness_sha256`. Current source permits no post-cutoff USDC fallback; actual database preparation, running adoption and original/authority readiness remain unproved. Preserve 10% provider/90% recycle, owner-local Ledger/no Snow, separate root custody and the closed-testnet exceptions. Earlier dated evidence remains unchanged.

## Retained execution addendum — October 5, 00:55 UTC

**Frozen October 5 00:55 UTC; 4 minutes remained before the October 5 01:00 UTC deadline (October 4, 8 PM CDT).** This [additive readback](evidence/mainnet-release-addendum-20261005.json) supersedes current-state statements in the retained October 4 checkpoint below. **All 38 original outcomes remain open.** No deployment, owner/device approval, real root/UR activation or economic acceptance is established.

The executed compile tuple is SN `17863973` / Core `25a4ce7f` / Server `ba526028`: normal `-c -vet=off` passed, exit zero, joined, with no recorded resource errors. Its actual image lists all 66 selected roots successfully; the body finishes 61 PASS/5 FAIL/0 SKIP. Source successors Server `2f8a6ee4` (child of `902a924a`) and funding SN `127db99c` are separate uncompiled/unexecuted inputs. The protected full Server model remains the original `c60` / Core `7de` / SN `0fbb` tuple. A changed source graph does not inherit an older binary's passes.

| Actual observation and lesson | Exact correction and adjacent regression scope | Remaining qualification |
| --- | --- | --- |
| The earlier `5a` image compiled, then panicked in package initialization at `repair_operator_authority.go:30`; Go regexp refused `{1,2048}`. Selected test listing exited two, joined, before any of the 65 bodies ran. Null result counts are not zero-failure success. | [SN `17863973`](/mnt/data/urnetwork/operator-recovery-handoff-20261004/regex-init-17863973/handoff.txt) preserves the same ASCII class and 1–2,048-byte bound using `+` and an explicit byte guard. The new signed-host root covers 1/1,000/1,001/2,047/2,048 bytes, 2,049-byte rejection and invalid/empty characters; the omission control removes the byte guard. Exact peer source scan found no other counted-repeat literal above 1,000. | Actual normal body completed **66 RUN / 61 PASS / 5 FAIL / 0 SKIP**, exit one/joined, with no recorded resource errors. The new regex boundary root passes. The five owner-fixture failures remain open below; affected reruns, race/vet and independent controls are separate. |
| Selected178 normal execution has five owner-fixture failures: unfinished-reservation recovery plus four owner signing import/reply/v1-v2 custody roots. Their exact names and raw failures are retained in [the readback](evidence/mainnet-release-addendum-20261005.json). | Source followup `de32a1b4` replaces fabricated completed-checkpoint loss with actual precompletion store/recycle/trim interruption seams and adds two negative identity roots. Separate child `c43aa2fd` supplies the explicit durable-volume context in the four failed import/reply fixtures and adds one hard-refusal root. Both exact children of178 are bound in the readback; production custody/readiness guards remain intact. | Compose both source-only children onto funding127, compile and run the corrected/adjacent roots and readiness/custody controls. Neither child has executed. Keep the five original failures and all 61 passed roots; no hardware signing or live owner authority is inferred. |
| xops `55f1` retry2 completed 309 PASS/2 FAIL/0 SKIP; its original census/depth failures passed, but two alert timing fixtures failed. Corrected `a1` retry3 now completes **311 PASS / 0 FAIL / 0 SKIP**, 74.892 seconds, exit zero/joined, with no recorded resource errors. | [xops `a1fdda0d`](/mnt/data/sn-testnet/mainnet-parallel-20261004/domain-dashboard-a1fdda0d-intake.json) fixes unspecified recording/alert group evaluation order in two fixtures. All 311 identities, production rules, SN09 pin and 120/180-second thresholds remain unchanged. The actual run includes reversed-order controls and eventual 150/210-second alerts, real pinned promtool and loopback Prometheus. | Retain both failed runs and the exact successful311 scope. Extra independent final-fence controls and actual Grafana/Mimir ingestion, alert delivery and on-call/repair drills remain separate open gates. |
| The protected 1,703-root `c60` model prefix is **1,571 RUN / 1,545 PASS / 8 FAIL / 11 SKIP** and still has no package terminal. Its 8 observed failed roots remain failures. Every observed skip is in the frozen allowlist; those are coverage gaps, not passes. | Fixture child `902a924a` follows URL `ba526028`, cutover `01f6e707` and earlier barrier `ed2a77ab`; these address the first six failures. [Latest child `2f8a6ee4`](/mnt/data/urnetwork/legacy-settlement-fixtures-handoff-20261005/intake.json) addresses the next two legacy failures. Production guards/migrations are unchanged by all these fixture corrections. | Preserve the protected run to terminal exit and joined/resource disposition. Compile the corrected model graph and execute failed roots, adjacent regressions and controls. Legacy queue acknowledgement is not terminal settlement; the distinct intent worker must run. The mainnet-package compile above does not execute Server model tests. |
| Three URL fixtures expected expiry instead of the existing six-minute renewal headroom. | [`ba526028`](/mnt/data/sn-testnet/mainnet-parallel-20261005/astra-url-renewal-fixture/HANDOFF.json) asserts the original four-hour window minus six minutes using explicit clocks and joined workers: three corrected roots, 27 adjacent roots and two omission controls. | Corrected model bodies and controls remain pending; no timing threshold or quota/security rule is relaxed. |
| The missing-original census fixture inserted a newly settled NULL-usage row after prospective guard745, which correctly rejected it before the intended census assertion. | [`01f6e707`](/mnt/data/sn-testnet/mainnet-parallel-20261005/server-closed-work-cutover-fixture/HANDOFF-01f6e707.json) seeds actual historical NULL usage under prefix744, applies current migrations, then contrasts it with a genuine current close. One corrected root, four adjacent model roots, six guard roots and three controls retain nil-result refusal of incomplete originals. | Compile and execute corrected/adjacent/control scopes; production guard745 is preserved. Missing original usage never becomes zero credit. |
| The healthy force-close fixture expected the payer's SQL debit before the asynchronous debit worker ran. Raw failure was an aggregate accounting assertion; phase diagnosis comes from the exact production branches. | [`902a924a`](/mnt/data/sn-testnet/mainnet-parallel-20261005/astra-force-close-healthy-fixture/HANDOFF.json) proves retained 1,024-byte debt/journal before the public worker, exact debit/release after it, and no work on replay, while retaining the dispute-update rejection guard. One corrected root, 29 adjacent roots and three controls. | All corrected bodies/controls remain pending. Calling the public worker in a test does not establish deployed scheduler cadence/restart recovery. Contract-close `ed2` separately corrects its cached activity barrier with nine roots/one causal variant; original failures stay recorded. |
| Two explicit legacy fixtures treated foreground durable intent as synchronous financial completion. | [`2f8a6ee4`](/mnt/data/urnetwork/legacy-settlement-fixtures-handoff-20261005/intake.json) invokes actual `FlushLegacySettlements`, preserves original stream state, checks exact pending/terminal accounting and brackets the 15-minute refusal backoff with DB clocks around the worker. Two corrected roots, six neighbors and two omitted-worker controls are frozen/source-reviewed. | Corrected model compiler, bodies and controls remain pending. This separate intent worker does not use the Redis consumption-journal expectation, and fixture execution will not establish deployed scheduler cadence. |
| Independent subset bounds could hide shared funding: two 50-unit claims exhaust one 100-unit obligation funded by 50 income + 50 capital, yet prior aggregation allowed income `[0,100]`. | [SN `127db99c` / component `cf04a36f`](/mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-funding-correlation-final/evidence/intake-v2.json) correlates selected claims/payments through original obligation and carry capacity, including late payments, retired topology and cold replay; unknown sources remain unknown. Fifteen new roots/nine new controls; affected funding union 54 roots plus operator 23 and combined 33 controls. | Source review/application checks only. Funding is excluded from the `178` image. Required public native capture/replay originals, normal/race/vet and controls remain pending; no proxy proof or skipped public path qualifies it. |

The source graph through Server778 now includes the actual timed-open producer and publisher, with exact original boundary/DB-lock authority; that resolves the earlier missing-source description for its named graph. Complete authenticated nonempty ingress → payout → provider-window/Claim execution remains unqualified, and canceled historical work remains unknown. Source composition and migration availability establish no deployment.

The settled requirements are unchanged: 10% native miner allocation to providers and 90% owner recycle; October 6 at 00:00 UTC governs new earnings, while all pre-cutoff USDC obligations and retries remain payable without conversion or double payment. Every old planner must be replaced before relying on its new shared lock. Owners keep their Ledger locally with no Snow access; root hotkey/coldkey custody is separate. Both root and UR validator roles remain required, and current v470 participation has no periodic SetRootWeights obligation. The closed final testnet and its accepted exceptions are retained.

## Retained checkpoint — October 4, 23:56 UTC

**Finalization deadline: October 4 at 8 PM US Central (CDT), October 5 at 01:00 UTC.** This [read-only release checkpoint](evidence/mainnet-release-checkpoint-20261004.json) is frozen at **October 4 23:56 UTC: 63 minutes remained**. It establishes no production acceptance or deployment. At the deadline, report every unfinished implementation, qualification and external input explicitly. The [38-requirement ledger](PRELAUNCH-FIXES.md#reconciled-status-of-all-38-requirements--october-4) preserves all original outcomes; every row remains open.

The documentation base is SN `3b75fd9a`. The sealed source checkpoints used by this matrix are [SN `1fa43d01`](/mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-original-custody-final/evidence/intake.json), Core `25a4ce7f`, [Server `6c25ca53`](/mnt/data/sn-testnet/mainnet-parallel-20261004/server-final-777-union/source-handoff-6c25ca53.json) and [xops `86f5eedf`](/mnt/data/sn-testnet/mainnet-parallel-20261004/domain-dashboard-86f5eedf-intake.json). They are source checkpoints, not one qualified release. The running Server model image uses the earlier `c60b2bc1` / Core `7de1d3e8` / SN `0fbb0ccf` tuple. A separate normal `-c -vet=off` compile on SN `5aaf88a6` / Core `25a4ce7f` / Server `80bcc285` passed: exit zero, joined, no recorded resource errors, retained image 127,324,112 bytes. That result establishes compilation only; bodies, vet and causal controls remain pending. Exact generated bytes, schema, runtime engines, policy, host configuration and affected normal/race/control evidence still need one accepted manifest. Earlier dated summaries retain their original scopes.

Astra owns implementation and source review; Sol owns admitted compiler/test execution and independent result readback; Root owns final integration and publication. Preserve healthy work, exact successful receipts and original failures. All Git commits inherit the global Bitprecipice author configuration; never add repository/worktree identity overrides.

The settled economics remain **10% of native miner allocation for providers and 90% owner recycle**, with equal weight for paid/free completed traffic. Recycling does not fund the reserve. **2026-10-06 00:00:00 UTC is the inclusive new-earnings boundary**; obligations earned before it may finish paying in USDC later, including backlog and processor retries. Do not convert their unpaid value to alpha or pay the same usage twice. The [published policy readback](evidence/current-cutoff-readiness-config-20261003.json) records `activation: blocked`; the [fresh October 4 config/planner handoff](/mnt/data/sn-testnet/mainnet-parallel-20261004/oct6-planner-ownership/HANDOFF.json) retains the same `main/sn.yml` blob `05b56036` at config main `9c4a3435`. Loaded worker policy, deployment and actual boundary enforcement remain unverified. The new planner lock is effective only after every old planner binary has been replaced; old writers do not honor it.

The owners retain their Ledger and sign on their own device; they have **no Snow access**. Snow may verify, retain and submit only the exact returned signed action. Root hardware custody is separate and still needs its actual device/API and key roles identified; current root lifecycle actions may require the owning or staker coldkey rather than the hotkey. Both the owned netuid-0 root role and UR-subnet validator role remain required, including the two initial UR instances in the bootstrap plan. The reviewed v470 accumulation strategy needs no periodic hotkey signature or retired root-weight call, but a passive monitor alone does not establish that the actual root participant is admitted, active and earning.

A later [whole-work intake](/mnt/data/sn-testnet/mainnet-whole-work-inventory-20261004/evidence/sn-v11/intake.json), SN `864f3680`, adds immutable signed Open observations and public raw clocks: 164 roots per mode, 70 controls and three vets are authored and unexecuted. Reported Current `b037` / Server `d014` composition remains separate source work outside the sealed `1fa` / `6c25` graph below; canceled historical work remains unknown.

### Release actions remaining at this checkpoint

Declared test scopes below overlap; do not add them into a final unique-root total. A static patch-application check is not a causal body result. Sol selects and executes admitted phases under the current resource lease; source owners preserve the protected running image and prepare successors separately.

| Release lane | Actual evidence at the checkpoint | Next required result | Owner / open requirements |
| --- | --- | --- | --- |
| Claim and restore | [Corrected Claim129](evidence/mainnet-release-checkpoint-20261004.json) passes normally (1,694.27 seconds) and under race (8,152.19 seconds), exact `1eb679a4` / Core `e77caf30` / Server `f67c7ff7`, exit zero, joined and resource qualified. The original 129-review/2,048-work assertion is unchanged. Restore129 normal/race, twelve finite Claim roots per mode and six intended causal failures remain separate retained results. | Bind unchanged closure or run affected successors; finish complete owner/namespace restore and production recovery evidence. Keep original 2,184-work failure as historical evidence. | Sol / PH-01, 08, 09, 24; MG-03, 09 |
| Running Server model | [Actual 1,703-root model body](evidence/mainnet-release-checkpoint-20261004.json) is running on `c60`; frozen partial census is **1121 RUN / 1110 PASS / 1 FAIL / 9 SKIP**, with no package terminal. `TestContractCloseOriginalCanceledHistoryReadRollsBackThenRecovers` failed. The nine observed skips were preapproved, with zero unknown skips in the bound audit; missing configuration/dependencies and the optional benchmark remain coverage gaps. | Preserve this run to actual exit/cleanup/resource disposition. Qualify test-only `ed2a77ab` correction with nine roots and its causal variant; this running suite is not a suite PASS. | Sol, Bootstrap / PH-16; MG-02, 10 |
| Final source and schema | [Server `6c25ca53`](/mnt/data/sn-testnet/mainnet-parallel-20261004/server-final-777-union/source-handoff-6c25ca53.json) joins gas `a1c9ca00`, fixture `ed2a77ab` and prior-creation `cd453`/`a17`. Prefix through 775 and exact 776 are preserved; 777 is appended. Static declarations: 1,738 model, 1,013 controller, 531 root (530 Linux). All 88 control patches apply; bodies and compiler remain unexecuted. | Finish the separately tracked wallet/roster/gas-cleanup fixture children, join and qualify the authored still-open producer `f0f44f75`/778, then seal the required Core/SN/Server graph and qualify its changed closure. | Current, Composer, source owners / PH-06, 13, 16, 27; MG-02, 05 |
| Original custody and capacity | [SN `1fa43d01` / Core `25a4ce7f`](evidence/mainnet-release-checkpoint-20261004.json) adds complete original creation/request/publication preparation and restore. The custody handoff adds six Core and thirteen SN roots including capacity. Its sealed affected union has 157 roots per mode and 43 control recipes, all unexecuted. | Qualify real public fresh/restore paths, complete original files, empty/partial crash files and cold SDK lifecycles. Keep the original birth profile; capacity increase cannot silently adopt an old root. | Recovery, Integration, Sol / PH-01, 09, 23, 26; MG-03, 09 |
| Provider work and payout | [SN `bb75`](/mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-provider-finality-union/evidence/intake.json) retains 78 provider-consumer roots/21 controls, 148 whole-work roots/56 controls and 22 operator roots/15 controls per mode as source inventories. Server 776 and prior-creation acquisition are source-composed. | Prove the nonempty authenticated ingress → original creation/session/work → payout artifact → provider-window/Claim path, original earning/wallet selection, and independently authenticated still-open/canceled work. Producer `f0f44f75`/778 has six authored roots but is a separate unexecuted child; complete final public-path qualification and canceled historical work remain open. | Integration, Bootstrap, Current, Sol / PH-11–13, 17, 27; MG-03, 06 |
| Gas, native fees and conservation | [Gas `a1c9ca00`](/mnt/data/sn-testnet/mainnet-parallel-20261004/operator-gas-policy/INTAKE.json) implements independently pinned approval and conservative nonce/lifetime ceilings: 30 declared roots per mode, 19 controls, unexecuted. [Native readiness](/mnt/data/sn-testnet/mainnet-parallel-20261004/native-witness-review/sn/mainnet/evidence/native-qualification-readiness-20261004/readiness.json) names 22 Rust roots, 86 Go roots per mode and 23 controls on its own pinned graph. | Execute matching current capture/replay engines and native fee/finality/conservation consumers. Retain full gas ceilings; actual native-fee proof, denomination admission and idempotent liability release remain an explicit gate. Prove original 10/90 allocation; receipt status, inferred refunds and policy renewal release no reserved ceiling. | Native, WholeFee, DepositWallet, Sol / PH-04, 11, 12, 19; MG-04, 06 |
| Runtime, HTTP and recovery | Finality `74bcb574` preserves first/highest witnesses and implicit selection through the outer owner; its joined affected partition has 33 roots per mode. HTTP `3d7734ab` adds ten roots, 46 affected roots per mode and 19 controls. All remain source-only. | Execute affected normal/race/vet and operative controls on the joined graph; preserve deadlines, hard causes, original signed history, bounded parallel repair and actual child joins. | Budget, HTTP, Recovery, Sol / PH-03, 07, 18, 19, 21, 22, 25 |
| Monitoring and dashboard | [xops `86f5eedf`](evidence/mainnet-release-checkpoint-20261004.json) executed 311 roots with real pinned Prometheus/promtool: **309 PASS / 2 FAIL / 0 SKIP**, 47.962 seconds, exit one, joined. Failures affect the producer-census parser and deep-JSON refusal expectation. The exact SN `09f8f989` pin, 104 panels/177 queries and 14 chain/36 conservation/six controller gauges remain bound. | Qualify source correction `55f1acf0`, its adjacent cases/controls and unchanged 311-root combined scope. Reliability/provider-window gauges remain unknown. Heartbeat cannot refresh outcome freshness; actual Grafana/Mimir ingestion, alert delivery and on-call/repair drills remain open. | Sol, Monitoring, Ops / PH-15, 28; MG-07 |
| Custody and both validator roles | Owner-local Ledger workflow `fc55c559` adds three roots; 59 selected roots/seven controls remain unexecuted. Current two-UR start code exists. Current v470 root participation uses registration/stake/basket state; no periodic SetRootWeights is required. | Supply actual owner/device approvals and signed originals, identify separate root hotkey/coldkey custody, verify admitted active/earning root and two UR roles, installed contracts and approved host inputs. Software fixtures do not supply those facts. | Owner, Bootstrap, Root / PH-14; MG-01, 08 |
| Rollout and acceptance | October 6 new-earnings policy remains published with activation blocked. No loaded-worker adoption, deployment, real economic outcomes or final mainnet acceptance is established. | Replace every old payout planner before relying on the shared lock; preserve all pre-cutoff USDC debt/retries. Complete rollout/rollback/outage/restore rehearsal and, after authorized activation, observe three native emission intervals plus a complete 50,400-block UR settlement/claim cycle. | Root, operators / all 38 outcomes; MG-10 |

The final testnet remains closed. Its accepted operating exceptions and later repairs do not rewrite [R48's terminal result](../sim-testnet/FINAL-4.md): zero complete acceptance epochs and `final_acceptance=false`. Preserve missed native history, low-usage/provisional readiness and uncredited historical debt as explicit exceptions; mainnet qualification must address their production causes without restarting the closed testnet campaign.

Use the current reviewed finite resource admission and a fresh physical/memory check for every new qualification phase. Preserve the original 113,681,502,208-byte baseline, existing live reservations and 2× forecast margin. A larger scratch allowance, reclaimed checkout space or successful compiler is neither a product pass nor production-capacity evidence. This documentation reconciliation executes no compiler, database, test body, signing or deployment.

## Reconciled status of all 38 requirements — October 4

This ledger preserves each original outcome and does not replace it with a narrower component proxy. **Every row remains open.** “Source” means inspected/frozen implementation, “pass” means the exact named executed scope, and “control” means the intended old-body failure was actually observed. Missing results remain pending or unknown. Production approvals, deployed identities, device custody and live outcomes are not supplied by any source/test receipt below. The [independent coverage census][rec-audit] records the same 38 outcomes and the final-graph gaps.

The [retained source-lessons checkpoint][rec-lessons] recorded source-complete but unqualified native-fee integration and bounded transport/lifecycle fixes, plus the pre-780 monitor 15/17 result. Source/test status cells below reflect their linked checkpoints; the opening section records newer qualification results. Prior owner 75, funding 37/38 and corrected model 37 retain their exact source scopes. All 38 original outcomes remain open; no later source inherits those results.

| ID and required outcome | Implementation/source available | Retained qualification and causal limits | Remaining complete requirement |
| --- | --- | --- | --- |
| **PH-01** — Operator, validators, bootstrap: durable recovery with independent audit and runtime owners | Joined SN preserves original journals, separate runtime/audit custody, checkpoint publication and native/Claim restore references. | [Paired restore][rec-paired] and [Claim owner][rec-claim] retain their scopes. Restore129 normal/race and [corrected Claim129 normal/race][rec-release] now pass their separate public roots; neither proves the whole native namespace. | Final all-owner cross-volume restore, interrupted publication/restart, independent healthy-role progress and production recovery rehearsal. |
| **PH-02** — Native/EVM submitters: one logical action, reconciled signed attempts and exact custody | Native/EVM owners retain original/replacement/cancellation signatures and reconcile uncertain outcomes before repeating effects. | [Customer recovery][rec-customer] remains qualified by component; no retained receipt grants new signing authority. | Final signed-attempt census, nonce/outcome reconciliation and exact-byte/no-duplicate-spend behavior across all deployed owners. |
| **PH-03** — RPC, artifact and HTTP clients: bounded transient recovery without duplicate writes | Named SN `d9d01a6a` / Core `87e1bf9f` read owners bound the complete physical cause graph, including typed nil, cycles and joined hard causes. Legitimate optional DNS children remain valid; not-found/local custody stays hard. Upgrade/Close/cancel handling preserves original budgets and WebSocket batching. | Earlier miner/read components retain their exact passes. [New source lessons][rec-lessons] and Core 76-root/18-control normal/race inventory remain unexecuted; no ambiguous-write retry or broader qualification is inferred. | Every public read boundary must recover within its original budget without retrying ambiguous writes or masking hard causes. |
| **PH-04** — Native-chain consumers: compatible upgrades and block-correct historical decoding | Historical/current runtime views, purpose-scoped capability admission and retained approved renewal exist; native heap parity has a source successor. | [Runtime renewal][rec-runtime] is a component pass. New `6b3bd512` capture regressions are unexecuted. | Approved actual runtime/code/finality; upgrade-block signing, execution-parent and post-state consumers, including both validator roles. |
| **PH-05** — Historical verifiers: durable, dependency-bound successful proof reuse | Dependency-bound validator proof/cache reuse and original archive ownership are implemented. | [Validator component][rec-validator] retains fourteen roots per mode and twelve controls; [current integration][rec-validator-current] adds three affected roots per mode and vet. | Complete nonempty-head lineage, invalidation, cold/warm replay and work-count qualification on the final graph. |
| **PH-06** — Release/configuration tooling: explicit release identity and lossless plan migration | Current frozen source checkpoints SN `3388840a`, Core `87e1bf9f` and Server `1ea80fa2` remain unqualified. Eight pure bootstrap/root review modes reach input validation without unrelated durable-volume flags; retained-state and effect modes remain gated. | [Current checkpoint][rec-lessons] preserves separate old owner/funding/model passes and actual 943 monitor 15/17. Composed release qualification, pure-plan/affected regressions and controls remain pending. | One immutable final source/module/generated/schema/config/contract manifest, reproducible selected artifacts and accepted production identity. |
| **PH-07** — Service supervision: independent restart, single ownership and meaningful readiness | Independent supervision/readiness sources exist. Economic follow mode may continue healthy reads after an acknowledged financial checkpoint when only transient metrics publication is unavailable; it emits no false success/freshness and does not replay retained effects. | [Root startup clock][rec-followup] retains its normal/race pass. New metrics recovery, operator availability and hard-conflict/Close preservation in [the current source][rec-lessons] remain unexecuted. | All deployed role startup, readiness, dependency recovery, single ownership and host restart/stop rehearsal. |
| **PH-08** — Replay and workload scheduling: bounded work, memory and foreground latency | Foreground proof projection releases read ownership before remote callbacks; Claim source removes duplicate custody fences. | [Foreground successor][rec-foreground] remains qualified. [Corrected Claim129][rec-release] passes unchanged 129-review/2,048-work assertions normally and under race on `1eb679a4` / Core `e77caf30` / Server `f67c7ff7`. Twelve finite roots per mode and six intended causal failures remain retained. | Bind the completed long-history gate to the final changed/unchanged closure; still measure composed foreground latency, bounded memory and realistic dense native replay. |
| **PH-09** — State and artifact storage: explicit durable volume, atomic publication and recovery | Durable-volume ownership, exact archive namespace, capacity admission and restore adapters exist in components. | [Full512][rec-full512], Restore129 and [corrected Claim129 normal/race][rec-release] pass their distinct scopes. Original ade5 failures remain historical; these roots do not establish the complete original namespace/current-owner union. | Complete accepted owner/segment union, original authority, disk/inode forecasts, failure-safe cross-volume restore and production backup recovery. |
| **PH-10** — Epoch, fleet and evidence scheduling: resumable partial renewals and correct windows | Native renewal retains approved ancestry, original epoch scope, completed accounting and pending completion authority. | [Runtime renewal][rec-runtime] remains a component pass; new final native/Claim/fee joins are not inherited as qualified. | Partial fleet renewal, delayed review, interrupted epoch progress and exact final boundary behavior without repeating completed work. |
| **PH-11** — Treasury and bootstrap: conserved lifetime spend, reserve and funding semantics | Server `1ea80fa2` joins actual native-verifier invocation, independently signed denomination, once-only durable settlement and conservative contradiction holds, with migration 780 after 779. All seven proof originals must be retained before credit. | [Source implementation][rec-lessons] is unqualified. Unmappable contradictory fees remain held with unknown expense, never zero; original ceilings/attempts persist. Funding 37/38 and earlier native scopes are retained, not transferred. | Complete native-income versus opening-capital accounting, actual fees/refunds, funding bounds and 10/90 conformance under authentic authority. |
| **PH-12** — Contracts, operator and claims: complete settlement conservation and authorization | Original recipient entitlements, Claim lifecycle and carry conservation exist; `b4c69774` fixes delayed-root carry chronology. | [Current normal funding scope][rec-qualified] includes chronology component passes and one failed public archive root. Test-only `d2198a39` / `ec3b27f4` corrects its missing proof and capital expectation; actual native capture/replay, Solidity, race and controls remain pending. | Complete deposits/capture/carry/claims, original earning attribution, immutable vault liabilities and actual runtime rollback/transfer behavior. |
| **PH-13** — Provider, operator and validator protocol: identity isolation and durable proof progress | Server includes session 776, timed-open observation/publisher 778 and original prior creations. `4b917455` repairs signed-NUL original indexing with exact raw bytes retained and migration 779. | [Selected 37 normal][rec-qualified] passes all nineteen prior failed model roots plus adjacent original/open-observation scopes. Complete nonempty ingress-to-payout/provider-window execution remains unqualified; canceled history remains unknown. | Join actual producers, SDK, Server and validator consumers; prove complete paid/free/companion work, two-origin replay and immutable identity histories. |
| **PH-14** — Governance/bootstrap: actual capabilities, activated policy and both validator roles | Owner-local Ledger execution308 plus [portable workflowfc55][rec-owner-final] exist. Current two-UR start adapter and offline root-capabilities6a exist; root eligibility remains unknown. | [Exact owner75 normal][rec-qualified] passes with zero skips on SN8e/Core25a/Server 2f8, including corrected import/reply/recovery roots. Race, independent controls and actual device/activation authority remain separate. | Actual capabilities/contracts and owner approvals, admitted active root and UR participants, independently identified root coldkey/hardware custody for required lifecycle actions, and observed native behavior. |
| **PH-15** — Status/operations: actionable failure classes, progress and evidence-based ETA | Dashboard `86f5eedf` binds 14 chain/36 conservation/six controller gauges; heartbeat presence/age is independent of outcome freshness. Reliability/provider-window gauges remain explicitly unknown. | [Published xops a1][rec-qualified] retains 311/311 PASS. Separate clock-ahead alert 2f104 has seven new Promtool roots and 318 declared roots, all unexecuted; real ingestion/routing/drills and reliability/provider-window coverage remain open. | Complete status domains, truthful progress/ETA, independent missing-host detection and observed operational signals. |
| **PH-16** — Qualification and evidence: deterministic faults, composed coverage and independent replay | Protected source/image and child-context/wait safeguards are integrated; each new correction supplies adjacent/control scope. | [Current checkpoint][rec-lessons] retains original c60 failure and corrected 4b selected 37 PASS. Pre-780 monitor 943 compiles, then finishes 15 PASS/2 FAIL/0 SKIP with cleanup; no newer-source full suite, race or control qualification is inferred. | Deterministic causal controls, affected normal/race/vet and actual complete release/rollout/restore/recovery qualification with exact custody. |
| **PH-17** — Plan-derived indexes: bind cached lookup structures to their immutable plan/generation owner | Derived indexes remain bound to original plan/generation. Server `6c25ca53` adds prior-creation acquisition; completed immutable lineage stays distinct from live authority. | [Validator cache ownership][rec-cache] is qualified by component. Current prior-creation, wallet/roster fixtures and whole-work/frontier/economic indexes require exact final-graph tests and controls. | Final composed index invalidation, original-plan authentication and unchanged-history reuse without stale authority. |
| **PH-18** — Strict readers: re-authorize connection/runtime provenance at every boundary after provisional work | Strict readers reauthorize exact connection/runtime/purpose. Finality `74bcb574`, joined inSN `bb75f999`, retains first/highest witnesses and original implicit selection through outer retry. | [Observation short scope][rec-observation] retains 45 roots per mode. The current 33-root finality partition and operative controls are source-only; previous results do not qualify new callers. | Every affected public consumer must reauthorize after provisional work and reconnect without borrowing stale or foreign authority. |
| **PH-19** — Historical snapshots: use the reviewed historical runtime authority without weakening current writes | Historical snapshots retain original approved runtime authority. Finality `74bcb574` closes historical/current reads without replacing their block, including lower-only pending evidence and hard completed contradictions. | [Runtime renewal][rec-runtime] retains its exact scope. Current finality and native readiness recipes remain unexecuted; this outcome remains historical authority, not economic conservation. | Actual approved upgrade-boundary execution and post-state replay without weakening current signing or rewriting historical proof. |
| **PH-20** — Relay capacity: distinguish funded slots, retained history, scan pages and resident bytes | Capacity profiles distinguish slots, retained history, scan pages, serialized frames and resident work. | [Capacity component][rec-capacity] passes; sparse/zero-stake synthetic populations do not prove dense production sizing. | Final aggregate funded-slot/history/byte/descriptor/RPC limits and realistic populated fleet headroom. |
| **PH-21** — Fault controller: bounded parallel, idempotent component control with durable partial recovery | Bounded controller and passive-root dispatch exist. SN `bb75f999` joins the actual operator route, original session signer and service-credential-aware WARP resolution with Server392/c4. | Original four-manager/96-lifetime scope remains separate. The operator byte-bound root passes within [owner75 normal][rec-qualified]; full operator/controller execution, independent controls and host rehearsal remain open. | Bounded parallel all-component dispatch, operator adapter composition, interrupted recovery and original cumulative allowances. |
| **PH-22** — Service clients: retryable transport incidents, connection recovery and final error budgets | Physical transport/close/error causes remain explicit. HTTP `3d7734ab` joins remaining GET owners; Server `6c25ca53` retains wallet endpoint/finality and original signed-candidate continuity. | [Client/body-error qualification][rec-client] retains its old component. Current HTTP, wallet/gas, resolver and controller scopes await complete affected normal/race/control qualification. | Actual service transport incidents and final deadlines must retain the triggering cause, join owned work and leave unaffected roles progressing. |
| **PH-23** — Capacity revisions: bind funded slots, history horizon and every finite storage dimension | Signed capacity revisions preserve original heads, history horizon and physical admission; archive forecasts retain 2× margin. | [Capacity component][rec-capacity] does not qualify every new large-head, restore or whole-work profile. | Actual signed revision adoption and complete combined storage/resource dimensions on the final source and proposed hosts. |
| **PH-24** — Recovery performance: authenticate each retained plan once per immutable lineage | Immutable lineage indexes reuse admitted historical work; Claim keeps complete physical fences separate from hot epoch work. | [Cache component][rec-cache], finite Claim positives/causal scope and [corrected full129 normal/race][rec-release] pass. The original 2,048 ceiling was preserved; the passing body does not print a measured 1,926 value. | Retain exact passed source/peers and prove final changed-closure reuse or affected reruns; qualify composed original-plan authentication and foreground work. |
| **PH-25** — Supervisor lifecycle: explicit deployment stop joins every owned workload child | Core `87e1bf9f` retains the exact signed SDK close frame through native acknowledgement or one joined out-of-band cleanup handoff, including shutdown before the first worker or after a final empty scan. | [Core 76-root/18-control inventory][rec-lessons] and vet remain unexecuted. Older replay/lifecycle passes retain their source scope; real stop/join and public delivery remain unqualified. | Deployment stop joins every owned child and pending publication without callback self-join or lost original liability; actual cgroup/unit rehearsal. |
| **PH-26** — Large evidence transport: typed, cancellable public replay with finite admission | Protected bounded service-policy readers admit explicit complete frames; original-work transport retains finite byte/count limits. | Reader and transport component scopes are retained; new whole-work/public replay unions remain unexecuted. | Actual aggregate-size transport/replay, cancellation and complete namespace coverage under original signed admission. |
| **PH-27** — Policy activation: coherent validator evidence and client-key domains across operators | The economic reader now uses exact published settled_contract_close_time/finish_pre_cutoff_obligations identity strings, joined in SN d9. Server 1ea retains original request/earning histories, repairs 773/775 via 779 and adds native-fee 780 with its concrete catalog. | [Exact source/current results][rec-lessons] do not establish final identity/controller/client/participant qualification. Pre-780 monitor has two actual fixture failures; new code/corrected fault fixtures and live October 6 adoption remain pending. | Both operators and all validator/provider consumers must join original eligibility, binding, wallet consent and policy authority through rollover. |
| **PH-28** — Continuous operations: independent monitoring, bounded authorized repair and exercised on-call | Independent monitor rosters, signed bounded repair, operations manifest and dashboard sources exist. | [Published 311][rec-qualified] remains the actual xops result. Separate future-timestamp alert source has no execution pass; failed metrics publication cannot refresh financial evidence. Independent controls and actual alert/repair drills remain open. | Complete economic/role monitoring, real alert routing, distinct primary/backup on-call, external deadman and exercised bounded repair. |
| **MG-01** — Mainnet identity | Read-only identity/finality readers and exact runtime/source admission mechanisms exist. | Historical public-route agreement and runtime observations are evidence, not independent current approval. | Approved genesis/runtime/code/checkpoint and exact current consumer admission; endpoint ownership is not finality authority. |
| **MG-02** — Reproducible production release | SN338 / Core87/Server 1ea are frozen source inputs; exact executed owner/funding/model/monitor images still use their older named graphs. | [Current receipt/source checkpoint][rec-lessons] preserves 75 owner PASS, funding 37/38, model 37 PASS and monitor 15/17. No qualified final release graph, reproducible accepted deployment or later-source pass is established. | Integrate, qualify and reproducibly build one exact accepted production tuple, then bind published/running artifact identity. |
| **MG-03** — Durable recovery and complete evidence | Durable submitter/recovery and original receipt/fee/finality collectors preserve signatures and progress. | [Collector][rec-collector], [Claim owner][rec-claim], Restore129 and [corrected Claim129 normal/race][rec-release] qualify components. Complete original producer/creation namespace and final all-owner restore remain open. | Complete original signature/outcome/evidence census, independent owner continuation, stop/join and production restore/restart without duplicate effects. |
| **MG-04** — Runtime and native continuity | Purpose-scoped native continuity and approved ancestry exist. Runtime finality `74bcb574` and matching original-Wasm readiness are separately pinned current-source work. | Prior runtime/read-recovery passes do not establish latest all-role semantics; exact native22-Rust/86-Go/23-control readiness scope and final33-root observation partition remain unexecuted. | Approved actual runtime and complete signing/execution/history behavior across simulator-independent production roles and both validators. |
| **MG-05** — Policy and identity rollover | Canonical earning identity and exact signed-original repair are composed in source; Server 1ea appends 780 after 779 and includes its concrete schema catalog. | [Pre-780 monitor 943][rec-lessons] has 15 PASS/2 FAIL: nonexistent constraint and protected system-trigger fault injection. Source corrections, complete 780 catalog/rollover, controller/model, race and controls remain pending. | Actual two-operator host, credential, network and policy topology; fresh processed-key/proof progress through the production transition. |
| **MG-06** — Economics, settlement and custody | Original earning policy,10/90 entitlement/carry, wallet consent, serialized planner, conservative gas and shared funding-capacity sources exist. | [Native-fee bridge 780][rec-lessons] is source-implemented with exact denomination, all seven originals and sticky contradiction holds; unexecuted. Funding 37/38, corrected native carry, full participant-to-payout and actual 10/90 outcomes remain separate gates. | Exact original-work/reliability and recipient lineage, native 10/90 conformance, real reserves/deposits/claims and owner/operator custody; observe first-send outcomes prospectively. |
| **MG-07** — Continuous monitoring and bounded repair | Independent xops collectors/rosters and bounded repair sources include new manifest/dashboard/root dispatch. | [Published xops 311][rec-qualified] passes its exact scope. Separate clock-ahead child 318 and monitor 943 fault-fixture corrections remain unqualified; no live ingestion/repair/delivery is established. | Approved service SLOs, real routing/on-call/deadman, continuous conservation and exercised root/operator/validator repair. |
| **MG-08** — Mainnet bootstrap and both validator roles | Owner workflowfc55, contracts and current two-UR activation exist. Root470 outcome remains actual seat/stake/basket participation; offline capability shape supplies no live authority. | [Root source review][rec-root-current] leaves live membership, capacity, stake and earnings unknown. [Owner75 normal][rec-qualified] supplies software component evidence only; actual hardware custody and root/UR activation remain open. | Actual census/reset disposition, signed owner action, installed contracts/Safe/funding, independent custody and actual admitted/active root and UR roles, and separately authorized root custody/transport for any required current lifecycle mutation. |
| **MG-09** — Sustained resource and storage capacity | Complete volume/capacity/restore mechanisms and reviewed finite test-resource admission exist. | [Full512][rec-full512], Restore129 and [corrected Claim129 normal/race][rec-release] pass component scopes. Full namespace/current-owner restore, realistic sustained final workload and production sizing remain unproved. | Realistic sustained dense workload, complete accepted archive/owner restore, backup/media integrity and measured host headroom. |
| **MG-10** — Qualification, rollout and actual acceptance | Inclusive October 6 earning policy is published with activation blocked; exact receipts and rollout/acceptance requirements remain retained. | [Current checkpoint][rec-lessons] preserves every original failure and exact component pass, including monitor 943 15/17. New source and complete qualification/rollout remain open; no deployed worker adoption or mainnet acceptance is established. | Complete prelaunch qualification and rollout/rollback/outage rehearsal; after approved activation observe three complete native emission intervals and a full 50,400-block UR settlement/claim cycle. |

### New source corrections retain their actual causes and adjacent tests

| Frozen source | Actual correction and required proof |
| --- | --- |
| SN `b4662ed8` / fixture `1eb679a4` | Full129 observed excess work comes from repeated complete custody checks. Remove two duplicates without reducing the 2,048 assertion or accepted history. Initial new fixtures expected five physical checks instead of the observed four and used a callback no longer reached after both native blocks were consumed. `1eb` targets real HTTP/finalized-head boundaries and preserves all production bytes. [Twelve selected roots per mode pass, and all six causal variants fail at the intended assertion][rec-followup]. Finite vet remains separate. Restore129 passes both modes on its original 6a63 scope; corrected Claim129 now passes normally and under race on its exact standalone `1eb679a4` peers. The unchanged 2,048 gate passed; predicted 1,926 work is still not a printed measurement. |
| SN root-startup fixture `b870f2f3` | The actual race detector found the diagnostics completion goroutine reading a synthetic time value while the wait hook wrote it. The test now uses an atomic elapsed duration and retains the exact shared 300-second exhaustion assertion. Production bytes are unchanged; [the affected public root now passes normal/race with exit zero and joined children][rec-followup]. Preserve original b466 race failure. Wider startup/repair controls and final graph remain separate. |
| Server `3d5400fa` | Three tests passed `HandleError(any)` to `errors.Is(error)`, preventing affected package compilation. Require the recovered value to be an error, then preserve the original sentinel assertion. All production/module/schema bytes remain f67; qualify the corrected model/controller images and their original body/control scope. |
| SN `6b3bd512` | Native capture omitted proved `:heappages` while strict replay used the onchain context. Capture now uses the same proved heap semantics. Three authored roots cover proved heap parity, no-override dynamic growth and the unchanged memory bound; compilation, actual Rust execution and incremental trie-refill coverage remain pending. |
| SN `b4c69774` | Epoch-number ordering wrongly refused contract-valid carry from a newer epoch into an older delayed root. Preserve original source epoch/pool and require prior receipt order, including same-block transaction/log order. Eight Go roots per mode, three Solidity roots, adjacent settlement/claim/deposit suites and four mutants are in the [handoff][rec-carry]; none has executed. |
| SN `15c7b13f` | Root dispatch must consume original signed repair authority and retain pending outcomes without occupying busy-unit workers. Eleven authored regressions cover dispatch, nested output/custody paths and retained intent. Some nested/busy/intent controls exercise helpers; actual public controller/operator composition and independent controls remain pending. It repairs the passive observer, not native root signing. |
| xops operations `7b754d87`, dashboard `93687841` | [The earlier composed source][rec-dashboard-prior] includes the operations manifest, 200 monitor roots and 42 dashboard roots; none of those new bodies has executed. It consumes all 36 conservation gauges from SN440d9e43, preserving independent known/presence/freshness guards; reliability and provider-window coverage remain unknown. That earlier source pin is superseded by the bound `86f5eedf` successor below; its actual 311-root body has two failures, with corrected qualification and independent controls pending. Source generation provides no live alert/on-call acceptance. |
| SN `8c22ab41` / `7b318f64` / `d4a2ea83` | Cached attempt bindings could return without a current finalized-authority check; EVM checkpoint/source receipt reads needed closing native finality and canonical opening/closing witnesses. The isolated successors retain original deadlines/receipts, reject orphaned or regressing witnesses, and add adjacent cache/checkpoint/source-read regressions and timeout controls. They are source-only, uncompiled and unexecuted; the runtime-observation caller successor is separately pinned below. |
| SN runtime-finality `59a4c05d` | Opening canonical-hash checks alone permitted a lower closing finalized head, and transport replay could forget the higher original witness for a historical target. The [source readback][rec-followup] binds the successor over read-budget `9e2b6a13`: retain first/highest witnesses inside the original bounded read, recheck complete closing native finality, keep lag/missing evidence pending and changed canonical hashes hard. Six new roots cover thirty scripted cases, with the existing late-purpose canonical control updated. All compilers, bodies and causal execution remain pending; these source changes do not inherit older runtime qualification. |
| Server payout planner `cf5140e5` | Two ReadCommitted planners could select the same sweeps, then overwrite the original `payment_id` after one planner submitted. A shared transaction lock now precedes selection; final assignment compares the original owner and exact affected-row count or rolls back. [Four public DB-barrier regressions, eighteen neighbors and three omission controls][rec-planner] are authored/unexecuted. Every deployed old planner must be replaced before relying on this shared lock; original cutoff, accepted processor attempts and USDC obligations remain unchanged. |
| Server `c60b2bc1` | [Frozen source][rec-server-union] joins the nominated source through 775, including real wire-key correction in773 and original request closure in775. [Normal model compilation plus the actual 1,703-root inventory][rec-followup] succeeds on Core7de/SN0fbb. Original prebody harness failures and exact cleanup remain historical. The same protected 1,703-root image now has an actual running full model body, an observed contract-close fixture failure, and no terminal suite pass. Current `6c25ca53` source through 777 and later producer 778 remain separate qualification inputs. Real DB/schema, nonempty public ingress-to-payout, controller and causal scopes remain qualification work. |
| SN HTTP owner `a0132321` | Miner reads previously expired after 90 seconds, and the retained 64-attempt cap would cut a 300-second window short. Physical close causes, PathError precedence and interrupted hard-status/overflow/cancellation also needed preservation. [The source handoff][rec-http] adds thirteen deterministic roots; the 98-root union, two vets and sixteen causal groups remain unexecuted. No write retry is authorized. |
| SN owner-recycle `30851bab` | Original owner-recycle requests lacked their distinct owner-local Ledger reply path and separately authorized bounded native submission. [New source][rec-owner] adds those paths without reissuing unknown device results or replacing original signed bytes. Eleven new roots, a 52-root union and six omission groups remain unexecuted. Actual Ledger, owner action, runtime digest, bounded spend and finalized Recycle evidence remain external gates. |
| Server wallet `e9c8e201` | [Endpoint/finality and signed-candidate correction][rec-deposit], included in c60, keeps receipt/nonce observations on one endpoint, retains advanced-nonce uncertainty and cancels stale deposits within the original allowance. Seventeen new regressions, 66 controller/six model roots and twelve causal groups remain unexecuted. Independently approved per-intent/lifetime fee liability is not supplied. |
| SN root-capabilities `6a26316e` | [Current470 review][rec-root-current] distinguishes actual registration/stake/basket operations from retired periodic root weights. Eleven authored roots cover offline metadata shape, absence and typed unknowns; none executed. Live seat/capacity/stake/earning authority is unknown. Full-capacity registration needs sufficient applicant stake, while add-stake needs an existing hotkey account; do not promise unconditional register-then-stake ordering. |
| Runtime473 absent root seat | New `root-register` and `bootstrap-chain root-registration` source implements finalized census, explicit whole-operator-reducible-balance consent, exact coldkey payload, isolated hardware/public-signature custody, separately approved bounded submission, original body/event reconciliation and passive-role handoff. The operator is distinct from the receive-only reserve and current SN25 owner. Quoted burn/fee thresholds are preflight only; strict numeric burn-cap requests refuse. Existing-seat passive operation and eight contract actions remain unchanged. Focused new tests are authored, not execution-qualified by this entry; actual operator/device signatures, funding and production/runtime approvals remain external inputs. |
| Native fee boundary `beb511c3`; operator gas-policy `a1c9ca00` | [The explicit release gate][rec-fee-boundary] separates signed EVM wei ceilings from actual native fee verification. Frozen gas 777 source now retains the maximum approved candidate liability per nonce across original/replacement/cancellation candidates and retained generations; success, revert, expiry, cancellation or renewal alone release no credit. The SN verifier invocation is implemented in the public native-fee command and conservation worker. Server `1ea80fa2` now joins the verified invocation/settlement bridge, independent denomination, all seven original proofs and durable idempotent ledger 780; actual execution and external authority remain pending in [the current checkpoint][rec-lessons]. Preserve the separate one-Rust/34-Go runtime and twelve-Rust/53-Go whole-fee scopes; source policy does not replace original-Wasm execution or real native outcomes. |
| SN original custody `1fa43d01` / Core `25a4ce7f` | [The custody handoff][rec-custody-final] adds complete creation/request/publication preparation and restore, original birth and physical namespace guards. Six Core and ten SN roots plus ten controls are authored, unexecuted. [Capacity correction][rec-capacity-final] refuses declared profiles that cannot fit complete physical export: at most 1 TiB history and 32,767 files plus the root, with final windows and inert crash files sharing limits. Three public roots/four controls preserve original profile/birth/window bytes and refuse automatic capacity adoption; all remain unexecuted. |
| Server open observations `f0f44f75` / migration778 | [Exact source readback][rec-release] binds a separate child of `6c25ca53`. Previously undated unresolved rows lacked original exact-boundary authority. The new producer retains the first original observation under settlement row locks and database time, uses its independently configured signer, and refuses closed/missing/unauthorized/future-boundary inputs. Six roots cover retained boundary, no backdating, missing authority/original, terminal-owner ordering and DB clocks; compiler, bodies and composed public-path controls remain pending. Requires the separate SN453 protocol; canceled historical work remains unknown. |
| Server prior creations `cd453` / fixture `e686` | [The exact source handoff][rec-prior-final] corrects a retired SDK stream birth missing from current cuts: signed request IDs locate the independently published original; complete prior verification reconstructs checkpoint and cloned raw creation for the actual producer/public consumer. Missing bytes remain unavailable. The fixture adds genuine historical wallet consent/head pins. Three positive/missing/mutated-prior roots and five controls remain unexecuted; e686 is in a separate `2f655` fixture successor, not the running `c60b2bc1` image. |
| Final Server `6c25ca53` / gas `a1c9ca00` | [Frozen composition][rec-server-final] joins 30 new gas roots and 19 controls with original session 776, prior creation and fixture `ed2a77ab`. Independent signature/pin, active+retired account history, exact unsigned reservation before signing, unknown-signature recovery and cumulative nonce maxima are source-implemented. No receipt gas product or terminal-state credit is allowed. All bodies/compiles remain unexecuted; 88 control patches apply statically. Later wallet/roster/bounded-cleanup fixture children and authored producer `f0f44f75` / 778 remain separate. |
| Server fixture `ed2a77ab` | [Observed failure and exact correction][rec-ed2]: the holder transaction cached pg_stat_activity before the reader started, so the barrier could never see the new reader. The corrected test observes the actual advisory lock with pg_locks, deliberately proves the stale predicate misses it, cancels/joins the worker, checks zero effects and retries. Nine roots and one intended old-barrier control remain unexecuted. Production logic and budgets are unchanged; the running `c60b2bc1` failure is retained. |
| Runtime finality `74bcb574` | [Owner handoff][rec-finality-final] corrects outer retries forgetting prior high-water finality, implicit selection moving to a new head, lower-only evidence misclassification and completed fork evidence losing precedence to late cancellation. Seventeen new roots/39 cases plus four updated roots/17 cases are authored; the joined affected partition has 33 roots per mode. Source review and six mutation recipes are retained; compiler/body/control execution remains pending. |
| HTTP GET owner `3d7734ab` | [Remaining GET handoff][rec-http-final] gives Claim daemon reads the 300-second owner/60-second attempts and committed-artifact reads bounded retry under original hash/size/cause checks. Failed close discards idle connections. Ten new roots, 46 affected roots per mode and 19 controls remain unexecuted; no write retry is introduced. |
| Owner-local workflow `fc55c559` | [The source correction][rec-owner-final] moves portable recycle review ahead of durable-custody declaration; all nine effect/custody modes remain gated. Three new roots cover preparation, signing/import and bounded submit/reconcile workflow with synthetic device boundary. 59 selected roots/seven controls remain unexecuted; actual owner Ledger reply and independent host authority remain external. |
| xops `86f5eedf` | [Actual combined run][rec-release] completed 309 PASS/2 FAIL/0 SKIP, exit1/joined. Four controller counters really exist in pinned SN `09f8f989` repair_controller.go:247; the raw-source regex misses names after escaped newline letters within a Go format string. [Successor `55f1acf0`](/mnt/data/sn-testnet/mainnet-parallel-20261004/domain-dashboard-55f1acf0-intake.json) decodes emitted format lines, excludes lookalikes and retains original-parser plus four producer-removal controls. The JSON reader relied on interpreter RecursionError; the actual interpreter accepted 2,000 nested arrays before schema refusal. The successor enforces 64 containers, with 64/65/2,000-depth, encoding/escape and guard-omission cases inside the existing root. Same 311 identities and SN09 pin; both corrections have static review only, with all corrected bodies pending. Preserve 309 healthy results while qualifying the corrected combined scope and independent provider final-fence control. No reliability/provider-window gauges or live delivery are inferred. |
| Core/SN original-work and Server original-request sources | Aggregate close/report agreement cannot establish omitted requests or complete independently signed work. Preserve complete producer frontiers and pre-SEED request identity before interpreting absence. [Registry][rec-registry] explicitly leaves `OwnedRequestsComplete=false`; [Server 773][rec-original-request] returns retained original or unknown, never zero exposure. Qualify actual producer → SDK → Server → validator joins, ambiguity/concurrency/retry controls and real PostgreSQL migrations. |

Newly arriving source or test results must keep their exact commit, actual cause, adjacent scope and qualification state. A source review, formatting check, compile, fixture correction or successful subset is not a complete gate. Local intake links below are operational provenance; bind and sanitize the needed records into the final portable release manifest before relying on them outside this workspace.

[rec-lessons]: evidence/source-lessons-checkpoint-20261005.json
[rec-qualified]: evidence/qualified-progress-checkpoint-20261005.json
[rec-postdeadline]: evidence/post-deadline-production-status-20261005.json
[rec-addendum]: evidence/mainnet-release-addendum-20261005.json
[rec-audit]: /mnt/data/sn-testnet/mainnet-parallel-20261004/sol-final-coverage-audit-20261004.json
[rec-sn]: /mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-claim-custody/evidence/intake.json
[rec-server-union]: /mnt/data/sn-testnet/mainnet-parallel-20261004/server-final-union/final-source-handoff-c60b2bc1.json
[rec-claim-current]: /mnt/data/sn-testnet/sol-runtime-fd-qualification-20261004/evidence/final-1eb-mainnet-claim12-component-disposition-v1.json
[rec-core]: /mnt/data/sn-testnet/mainnet-whole-work-inventory-20261004/evidence/core/intake.json
[rec-work-sn]: /mnt/data/sn-testnet/mainnet-whole-work-inventory-20261004/evidence/sn/intake.json
[rec-registry]: /mnt/data/sn-testnet/mainnet-final-source-integration-20261004/provider-attempt-authority/evidence/intake.json
[rec-server]: /mnt/data/sn-testnet/mainnet-final-source-integration-20261004/current-server-final/evidence/intake.json
[rec-server-fixture]: /mnt/data/sn-testnet/mainnet-final-source-integration-20261004/server-final-error-fixtures/evidence/intake.json
[rec-original-request]: /mnt/data/sn-testnet/mainnet-final-source-integration-20261004/server-attempt-request-identity/evidence/intake.json
[rec-paired]: evidence/paired-metadata-sn-qualification-20261003.json
[rec-claim]: evidence/claim-owner-component-qualified-root-readback-20261004.json
[rec-customer]: evidence/customer-final-race-qualification-20261003.json
[rec-miner]: evidence/final-miner-selected-scope-root-readback-20261004.json
[rec-runtime]: evidence/native-runtime-renewal-qualification-20261003.json
[rec-validator]: evidence/validatorb6-selected-component-qualified-20261004.json
[rec-validator-current]: evidence/validator-current-e9-three-roots-vet-qualified-root-readback-20261004.json
[rec-compiles]: evidence/common-twelve-compiles-readback-20261003.json
[rec-correction]: evidence/0e5-mainnet-normal-independent-readback-20261003.json
[rec-foreground]: evidence/foreground-successor-controls-independent-review-20261004.json
[rec-claim-fail]: evidence/full129-normal-claim-failure-root-readback-20261004.json
[rec-full512]: evidence/full512-race-independent-readback-20261003.json
[rec-ade5]: evidence/native-adoptionade5-original-terminal-root-readback-20261004.json
[rec-funding]: evidence/funding-f31-affected-positive-root-readback-20261004.json
[rec-server-identity]: evidence/server5b-controls-root-readback-20261004.json
[rec-ur]: evidence/validator-activation-qualification-20260930.md
[rec-ledger]: evidence/owner-ledger-native-sdk-qualification-20260930.md
[rec-xops]: evidence/xops-final144-publication-root-readback-20261004.json
[rec-model]: evidence/server1709-complete-model-body-root-review-20261004.json
[rec-cache]: evidence/validator-cache-permission0b58-qualification-20261003.json
[rec-observation]: evidence/runtime-observation6fa-short-partition-root-review-20261004.json
[rec-capacity]: evidence/capacity-public-completion-qualification-20261003.json
[rec-manager]: evidence/miner-manager-composition549-source-root-review-20261004.json
[rec-client]: evidence/monitor-progress-body-qualification-20261003.json
[rec-replay]: evidence/replay-output-caller1b3f-qualification-20261003.json
[rec-collector]: evidence/capture-currentad0-qualification-20261003.json
[rec-cutoff]: evidence/current-cutoff-readiness-config-20261003.json
[rec-carry]: /mnt/data/sn-testnet/mainnet-parallel-20261004/contract-carry-invariants/HANDOFF.json
[rec-ops]: /mnt/data/sn-testnet/mainnet-parallel-20261004/operations-manifest-source-handoff-20261004.json
[rec-dashboard]: /mnt/data/sn-testnet/mainnet-parallel-20261004/domain-dashboard-86f5eedf-intake.json
[rec-planner]: /mnt/data/sn-testnet/mainnet-parallel-20261004/oct6-planner-ownership/HANDOFF.json

[rec-fee-boundary]: /mnt/data/sn-testnet/mainnet-parallel-20261004/astra-whole-fee-review/sn/mainnet/evidence/operator-gas-native-fee-release-boundary-20261004.json
[rec-followup]: evidence/mainnet-docs-evidence-followup-20261004.json
[rec-http]: /mnt/data/sn-testnet/mainnet-parallel-20261004/http-owner-adjacent/HANDOFF.json
[rec-owner]: /mnt/data/sn-testnet/mainnet-parallel-20261004/owner-recycle-execution-source-handoff-20261004.json
[rec-deposit]: /mnt/data/sn-testnet/mainnet-parallel-20261004/deposit-wallet-source-intake-20261004.json
[rec-root-current]: /mnt/data/sn-testnet/mainnet-parallel-20261004/root-current-participant-source-review-20261004.json

[rec-dashboard-prior]: /mnt/data/sn-testnet/mainnet-parallel-20261004/domain-dashboard-93687841-intake.json
[rec-custody-final]: /mnt/data/sn-testnet/mainnet-parallel-20261004/original-creation-publication-custody/handoff.json
[rec-capacity-final]: /mnt/data/sn-testnet/mainnet-parallel-20261004/publication-capacity-admission/handoff.json
[rec-prior-final]: /mnt/data/urnetwork/server-prior-creations-handoff-20261004/HANDOFF.json
[rec-release]: evidence/mainnet-release-checkpoint-20261004.json
[rec-sn-final]: /mnt/data/sn-testnet/mainnet-durable-volume-20261002/mainnet-provider-finality-union/evidence/intake.json
[rec-server-final]: /mnt/data/sn-testnet/mainnet-parallel-20261004/server-final-777-union/source-handoff-6c25ca53.json
[rec-gas]: /mnt/data/sn-testnet/mainnet-parallel-20261004/operator-gas-policy/INTAKE.json
[rec-ed2]: /mnt/data/urnetwork/close-original-db-barrier-handoff-20261004/intake.json
[rec-finality-final]: /mnt/data/sn-testnet/mainnet-parallel-20261004/runtime-finality-review/FINALITY-OWNER-HANDOFF.txt
[rec-http-final]: /mnt/data/sn-testnet/mainnet-parallel-20261004/remaining-get-owners/HANDOFF.json
[rec-owner-final]: /mnt/data/sn-testnet/mainnet-parallel-20261004/owner-recycle-workflow-source-handoff-20261004.json
[rec-operator-final]: /mnt/data/urnetwork/operator-recovery-handoff-20261004/v3/manifest.json

## Retained component progress — October 4

**October 4 continuous monitor deployment published:** The [latest independent publication readback](evidence/xops-fee-profile-current-publication-root-readback-20261004.json) verifies xops main `ba2f5323`, exact changed source/tool hashes and all 85 independent passes, including real Prometheus/promtool execution. It retains original durable roots and immutable input lineage, six independent role rosters, the 300-second read budget and explicit populated fee-profile bounds. The separately configured continuous conservation owner is still being added; production host activation and exercised alerts/on-call remain separate.

Future qualification uses the [100GiB cumulative scratch admission](evidence/future-parallel-qualification-resource-admission-20261004.json) under the standing automatic budget authorization. Preserve the original 113,681,502,208-byte baseline and 2× margin. At most two warm Go compilers and two bounded short bodies may overlap after fresh disk/memory admission; Rust, database fixtures and long history bodies require separate forecasts. Preserve running jobs and retained images; apply updated guards only to subsequent phases. Database qualification also retains the original Docker-root baseline and requires an exact container-owner census before launch.

**October 4 current signed-close source:** The [current generation readback](evidence/original-close-current-generation-root-readback-20261004.json) verifies global-author Core `7a805bd0` with the same generated tree and wire bytes as the earlier receipt. Actual protocol compilation exposed a test fixture passing a private-key slice as the public-key type; no behavior test ran. Correct the fixture, retain production signing and descriptor bytes, and qualify the real Core/Server/SN join. Generated source and compiler success are separate prerequisites.

**October 4 original Rust controls complete:** The [raw seven-control readback](evidence/original-capture-seven-rust-controls-root-readback-20261004.json) verifies21 clean/compile/body phases and seven exact intended assertion failures, with no ignored root and joined children. Retain the original fourteen capture/Yuma assets and successful exporters. Remaining work is affected Go integration, full funding/finality/fee consumers and current release qualification.


**October 4 actual vault fixture correction:** The [3f source review](evidence/capture-precompile-3f-root-source-review-20261004.json) confirms a test-only correction: synthetic precompile accounts now have code presence required by Solidity typed void calls, while actual registration, UID lookup and stake movement remain executed by the explicit adapters. Check those actual selector counts and unchanged success/rollback accounting. Qualify the affected vault/capture roots and a fixture-owned omitted-code control; no production contract behavior is weakened.


**October 4 corrected historical worker progress:** The [raw four-root readback](evidence/funding-corrected-proof-four-root-readback-20261004.json) confirms three passes and one RootMissed source-page failure in each mode. The correct `runtime-historical-proof` target resolves the wrong worker invocation; its original-income/later-payment, lost-ack composition and complete Yuma10/90 tests now pass. Keep `runtime-transition-replay` as a distinct protocol worker rather than substituting it by filename similarity. Preserve these six successes; repair the missed-root continuation and the separate real-vault precompile fixture before rerunning affected tests. Full economic acceptance remains incomplete.


**October 4 original-close wire generation:** The [pinned protobuf readback](evidence/original-close-pinned-protobuf-root-readback-20261004.json) verifies the exact generated Core child, unchanged proto input, maintained generator versions and sole generated-file delta. Original signed-close bytes use new field6 while stable report identity remains field5. The generation prerequisite is resolved; stage exact declared Core peers and qualify real producer, Server retention and SN consumers before publication. A generated descriptor is not a passing transport or accounting result.

**October 4 coherent capture/funding correction:** The [cd2 source and controls review](evidence/funding-dual-profile-cd2-root-source-review-20261004.json) verifies a five-path successor: remove the unsupported Go capture surcharge while retaining actual Rust/worker limits, select the maintained vault artifact and advance the original Claim expectation through matched epoch retirement. Four new deterministic roots cover profile combinations, exact boundaries, required-query rejection and real vault success/rollback. Qualify its19 affected/new/adjacent roots in both modes, vet and four labelled controls per mode using the matching8ad engines; retain all other successful partitions. Source review supplies no passing execution or mainnet acceptance.

**October 4 optional provider configuration liveness:** The [domain-reader source finding](evidence/provider-optional-domain-nonregular-source-finding-20261004.json) identifies blocking path open before descriptor admission: a FIFO with no writer can stall provider startup although close evidence is optional. Admit a regular file through nonblocking descriptor handling, bound its bytes and reject ambiguous documents. Missing or invalid optional input records a diagnostic and leaves the provider unsigned and operating. Add deterministic nonregular/no-writer tests and bounded worker cleanup; a byte cap alone does not prevent startup hangs.

**October 4 matching engines compiled:** The [independent build readback](evidence/funding-8ad5-matched-engines-root-readback-20261004.json) verifies all33 exact crate files, actual Cargo success for capture and replay, and two distinct protected executable images. Use these matching engines with the retained original capture/Yuma assets; the older engine/profile mismatch stays recorded. Only affected dual-engine partitions are rerun. Compilation alone does not establish execution behavior, funding conservation or mainnet acceptance.

**October 4 retained funding terminal results:** The [independent raw terminal census](evidence/funding-remaining-and-f51-terminal-root-readback-20261004.json) confirms20 ordinary passes and one fixture failure in each mode, plus nine dual-engine failures in each mode under the original mismatched composition. The f51 successor gets beyond the chain-identity seam but fails at a reused Claim epoch precondition in both modes. Review the complete fixture continuation before another successor, including expected epochs, signed admission, archive/reset and exact retries. Preserve the forty ordinary successful executions; rebuild matching dual engines and rerun affected bodies only. Funding qualification remains incomplete.

**October 4 combined capture and engine findings:** The [original dual-engine failures and exact source review](evidence/funding-dual-engine-original-failures-source-review-20261004.json) retain nine normal failures. The native capture caller allows272MiB plus two4MiB principal companions, while its shared worker rejects output above272MiB before execution. The actual Rust producer already includes both principal companions in its total272MiB native output limit; remove the unsupported Go-only8MiB surcharge and keep producer, caller and worker limits consistent. Test exact boundaries and adjacent modes. Five capture fixtures also request `STSettlementVault` although the maintained catalog names it `SettlementVault`. The Yuma continuation used an older engine without its required observation purposes; qualify fresh capture/replay engines from the actual matching production closure. Preserve successful ordinary tests and original assets, fix these related joins as a batch and rerun only affected partitions. No corrected execution or economic acceptance is established yet.

**October 4 funding fixture correction:** The [f51 source review](evidence/funding-candidate-f51-root-source-review-20261004.json) verifies a single test-only successor to the original funding candidate-isolation failure. The earlier fixture combined synthetic-mainnet authority with fixture-mainnet live readers and therefore refused before reaching its intended custody check. Enroll the fee and Claim authority before the live fixture first publishes; preserve its actual network and every original isolation/retry assertion. Qualify only the corrected root normally and under race detection, plus vet, retaining successful union partitions and the original failure. Production network identity checks remain unchanged.

**October 4 Server identity control results:** The [independent raw control readback](evidence/server5b-controls-root-readback-20261004.json) verifies all fourteen intended assertion failures across three control groups in normal and race modes, with no panic, timeout or skipped root. Together with the retained64 positive results and model vet, these bodies exercise immutable participant identity, deleted-client continuation and provider-local conflict isolation. The original resource wrapper remains unqualified: one PostgreSQL lifecycle observation failed after the final test body, followed by both containers observed absent and successful cleanup. Preserve that telemetry gap and qualify its teardown checker separately; do not repeat successful bodies or treat this selected scope as full Server acceptance. Optional original-evidence capacity must likewise leave ordinary valid close accounting available while recording unknown provenance; caller cancellation and actual signature contradictions retain their separate refusal semantics.


The [corrected original-capture exporter readback](evidence/funding-8ad5-original-capture-exporters-pass-root-readback-20261004.json) independently verifies both actual roots passing, joined descendants and seven exact private0600 outputs. Combined with seven retained Yuma/populated files, all14 original fixture prerequisites are available. Continue remaining funding41 partitions on existing protected Go images and the finality17 scope; preserve nine prior ABI positives and two framing successes. Fixture/control qualification and full conservation remain separate from these completed prerequisites.

The [original-finality implementation review](evidence/economic-finality-f7-production-tests-controls-root-review-20261004.json) covers frozen production integration, all13 new deterministic roots and seven exact operative controls. Retain original independently signed checkpoint/certificate bytes, require every quiet and financial EVM boundary, preserve economic cursors and incomplete evidence, and cache under held custody with transactional2x capacity charges. Qualify17 roots per mode, vet and14 causal executions on the actual dependency graph using corrected8ad capture assets. Complete provider provenance, whole original fees, runtime approval and activation remain independent requirements.

The [committed-capture correction review](evidence/funding-capture-committed-effects-source-review-20261004.json) distinguishes two genuine host observations (get/set) from one independently censused committed write. The exporter now verifies the pair’s actual before/after values, key, Apply identity and mutation ordinal/hash while retaining original amount, transaction, count and rollback assertions. The adjacent Go consumer already filters committed writes and joins original mutations; no production decoder/count relaxation is indicated. Qualify the two affected exporters and seven controls, preserving seven Yuma assets and two framing successes.

The [capture-framing execution](evidence/funding-e00-framing-pass-capture-census-failure-root-readback-20261004.json) passes both new deterministic framing roots. Both exporters now reach actual original capture and fail named census assertions (two versus one, four versus two). Review actual hook/mutation/transaction/Apply identity to distinguish observations from economic effects before adjusting fixtures or consumers; no capture output is accepted from these failures. Preserve both framing passes and seven prior Yuma assets while independent Server qualification continues.

The [qualified validator fix is published on main](evidence/validator-current-qualified-publication-root-readback-20261004.json) at `61972f83`. Cached terminal replay now remains bound to its original held archive custody and checks cancellation/admission before returning. The two published files exactly match the tested current-release candidate; all other Git paths/modules remain unchanged, and remote main is independently verified. This is an integrated component fix, not full mainnet acceptance.

The [current-release validator integration qualification](evidence/validator-current-e9-three-roots-vet-qualified-root-readback-20261004.json) independently counts all three affected original roots passing in normal and race modes, with zero failures/skips and successful vet. Full20,849-source and seven-peer closure is rechecked after execution. Together with retained14-root positives and12 causal controls, this qualifies publication of the exact two reviewed validator files on the currentCore2ea graph. Full historical-lineage/contention and all-role launch requirements remain separate.

The [capture-framing correction](evidence/funding-capture-extrinsic-framing-source-review-20261004.json) explicitly types the actual synthetic transaction bodies as bytes and adds independent canonical SCALE/header checks plus real capture/replay refusal tests for inferred integer words, truncated/trailing payload and nonminimal length. Keep strict production decoding and all seven passing Yuma assets. Execute the two failed exporters and two new regressions; existing five controls plus an inference-restoration control remain. The [scratch-cap addendum](evidence/qualification-scratch72-root-admission-addendum-20261004.json) raises only future test scratch allowance to72GiB under the user’s automatic budget approval, preserving the original baseline and2x resource margins; it grants no mainnet spend or signing authority.

The [original runtime exporter result](evidence/funding-fd922-original-exports-two-pass-two-fail-root-readback-20261004.json) independently counts two passing Yuma/populated roots and two failing capture roots after the corrected libtest compiles. Preserve the seven successful original output files and run their dependent Go scopes. Both capture failures reject nonexact SCALE bytes; Astra traced untyped integer vectors in the fixture to incorrect byte framing. Correct the test producer with explicit byte types, keep exact production decoding and add framing regressions. No successful capture asset or full funding qualification is inferred.

The [current-release validator image readback](evidence/validator-current-e9-two-image-root-readback-20261004.json) independently verifies successful normal and race compiler exits, joined descendants and exact protected image hashes/inodes. Execute the three affected roots per mode and vet before publication; compilation alone does not close the current-release integration gate.

The [corrected original-export runner](evidence/funding-fd922-original-export-runner-root-adoption-20261004.json) is reviewed and adopted subject to fresh compiler-slot/resource admission. Its four exporter definitions and fourteen output sets exactly retain the original recipe; the only source delta is the reviewed test import. Wait for the current validator compiler to join, then start under the existing2x reserve without another approval round or rebuilding unaffected Go images. Actual compile, exporter bodies and five causal controls remain unproven.

The [five corrected Rust causal controls](evidence/funding-fd922-rebased-controls-root-source-review-20261004.json) reproduce each exact original omission against the corrected source while retaining the codec import. This checks that fixing compilation did not erase the transaction-body, apply-index, Yuma-input, pending-parent or populated-matrix regression tests. Each control still needs a fresh compiling image and its named intended failure; source comparison is not qualification.

The [corrected original-export crate review](evidence/funding-original-export-codec-successor-source-review-20261004.json) verifies exactly one shared test-module SCALE codec import in successor `fd9229`. Its child Yuma modules inherit the alias, addressing all five observed compiler errors without changing economic assertions, production Rust, Cargo dependencies or Go source. Compile the corrected crate and execute the original four exporters/five controls; existing exact Go images remain reusable. The [current-release validator compiler review](evidence/validator-current-e9-image-runner-root-adoption-20261004.json) independently verifies all20,849 source entries and seven clean peers, then admits two protected images under the original cumulative resource budget. Three current dependency-graph roots per mode and vet remain required before publishing the qualified validator fix.

The [actual original-export libtest compile](evidence/funding-ab48-original-export-compile-root-finding-20261004.json) fails with five unresolved `codec` namespace references across three fixture source files. No exporter body ran. Repair the imports in a separately frozen successor, retain the original compiler failure, and rerun the unchanged four-export/five-control scope; this is a test-source compile defect, not an RPC or production-runtime failure. Independent component qualification continues.

The [original funding exporter runner review](evidence/funding-ab48-original-export-operational-root-adoption-20261004.json) admits the exact four positive exporter roots after fresh resource and full crate/toolchain/helper checks. Reuse warm dependencies while freshly compiling the package libtest; retain each original output and independent failure, protected image custody and joined descendants. Five causal controls remain separate. This admission supplies no passing execution or measured populated-workload sizing.

The [independently counted Server identity scope](evidence/server5b-selected-model-two-mode-root-readback-20261004.json) passes all 32 selected roots in both normal and race modes with zero failures or skips. Protected images retain exact hashes and inode custody; the fixture exits and joins cleanly, with complete sampled PostgreSQL/Redis coverage. Model vet and three causal-control groups remain before component qualification. Preserve the original workspace refusal beside this successful recovery.

The [duplicate-close-report source review](evidence/close-report-identity-tests-controls-root-review-20261004.json) covers all 17 new deterministic tests and eight production controls. Retain original authenticated client/ReportId and exact content atomically with byte accumulation; retry committed settlement after a lost reply, and keep report identity after contract/client cleanup. Public controller routing and equal paid/free/companion completed-byte weighting are covered at source level. Qualify the 34-root normal/race scope, three vets and eight controls per mode before deployment. Durable transport-authenticated reports alone do not prove independently signed physical work or a complete earning window.

The [original funding exporter scope](evidence/funding-ab48-original-export-scope-root-review-20261004.json) verifies all 33 crate source entries against the original reviewed composition. Execute four actual exporters, retain 14 exact outputs and qualify five causal controls, then feed those results to the existing corrected Go images for the full funding scope. Source recipes and matching hashes do not replace execution or measured populated resource sizing.

The [Server fixture workspace recovery](evidence/server5b-logical-workspace-recovery-review-20261004.json) fixes logical-versus-physical module path admission without rebuilding valid test images. Preflight the exact child helper command, working directory and GOWORK; a model-only graph cannot prove the startup helper resolves. The corrected helper and model graphs pass, and the unchanged 32-root normal/race scope is adopted. Preserve the original zero-body refusal and successful cleanup; actual model execution remains pending.

The [corrected ABI two-mode readback](evidence/coordinator-ab48-nine-two-mode-independent-readback-20261004.json) verifies nine normal and nine race passes, no skips or failures, joined exit zero and exact protected image bytes. The four original fixture failures now pass both modes. Fixture-control/vet and complete funding41 remain separate pending scopes.

The [current-main validator integration](evidence/validator-terminal-current-integration-source-review-20261004.json) contains only the exact two qualified terminal-custody files over current main. Other source and module bytes remain unchanged. Because the current Core/artifact package graph differs from the original qualification, execute the small affected-root normal/race and vet scope before publication; retain the already qualified original bodies and controls.

The [corrected ABI nine-root normal readback](evidence/coordinator-ab48-nine-normal-independent-readback-20261004.json) independently reproduces all nine passes, including the four original fixture failures, with no skips or failures and joined exit zero. Race and fixture-control results remain pending. Keep this result separate from full funding41 qualification and original failures.

The [original e51 partition readback](evidence/recoverye51-original-partitions-independent-readback-20261004.json) independently verifies all 47 normal roots: 43 pass, four fail, zero skip. It retains sealed race partitions separately and preserves the four original coordinator-fixture failures. Corrected ABI replay must qualify the affected roots; retain unaffected original results with explicit source/dependency joins. The separate 129-history recovery remains incomplete.

The [contextual provider correction](evidence/provider-context-cancellation-source-review-20261004.json) is source-reviewed across 12 changed files and nine deterministic regressions. Actual operation context reaches row copying, payout allocation, Merkle construction and publication/readback without changing legacy grammar. Independent canonical-value neighbors and a public HTTP cancellation-classification regression remain part of qualification.

Corrected `ab48` normal/race mainnet test images compile, but the original Yuma, populated 1,024/2,048-UID and vault-sequence exporters remain source-only. Qualify those actual original-runtime producers before the dependent funding tests; run independent ABI/recovery roots meanwhile and retain explicit unexecuted status for missing assets.

The [qualified validator selected component](evidence/validatorb6-selected-component-qualified-20261004.json) combines fourteen passing roots per mode, vet and all twelve intended causal-control failures. Root independently verified the final nonempty-head lineage and terminal-custody groups. Integrate the exact reviewed validator delta with current source/dependency evidence; broader-history and full release requirements remain open.

The [funding control review](evidence/fundinge0-operative-controls-source-review-20261004.json) verifies eight operative production mutations for original income/carry, retained totals, capital contradictions, tolerance, stale acceptance and owner custody. They remain unexecuted; join the corrected ABI fixture before full funding qualification.

The [closed-work behavioral source review](evidence/provider-closed-work-tests-controls-source-review-20261004.json) covers all 23 new deterministic roots and nine operative control groups. It includes actual paid/free SQL closure, retention migration, operator publication/retry, validator HTTP reconstruction and monitor active/cold evidence. The 50-root selected normal/race scope, six vets and 13 intended causal failures per mode still need execution; source review does not close provider authentication.

The [validator causal-control readback](evidence/validatorb6-four-controls-independent-readback-20261004.json) reproduces four operative groups in both normal and race modes: missing predecessor, unnecessary history reread, omitted cached-lookup custody and partial cache publication. Each modified build compiles and fails at the intended assertion while the original selected positive root passes. Two groups remain; retain the verified eight outcomes without repeating them.

The [Server attribution selected-test runner](evidence/server5b-selected-model-runner-root-review-20261004.json) is reviewed and adopted for 32 exact roots in normal and race modes using protected compiled images and private owned database/cache services. Fresh aggregate resource admission remains mandatory; the original cumulative baseline stays intact. Independent event counts, process joins and service cleanup are required before qualification. This does not qualify the later provider producer or whole Server suite.

The [close-report identity finding](evidence/close-report-dropped-identity-source-finding-20261004.json) confirms that Core retains a stable report ID on retries while the Server controller drops it before the incremental checkpoint update. Duplicate delivery can count bytes again. Retain report identity and semantic content atomically with accounting, make identical retries idempotent, and refuse conflicting reuse locally. Qualify concurrent retries, independent equal-byte reports, terminal retries and rollback/restart continuity; preserve legacy empty-ID behavior and review every hosted/native route before client rollout.

The [provider closed-work production review](evidence/provider-closed-work-production-source-review-20261004.json) verifies frozen SN `768e188a` and Server `5460b1d7` against exact source blobs. Usage and original rows share one SQL snapshot; an over-capacity optional census is discarded completely. Validator and monitor reconstruct identities, byte partitions and active/cold counters. This establishes a source component, not complete physical-work, reliability, eligibility or independent-window authentication. Qualify the declared tests and controls, then finish those authority producers before claiming the economic gate complete.

The [coordinator ABI fixture correction](evidence/coordinator-tuple-ab48-source-root-review-20261004.json) is source-reviewed at `ab48c41d`: both uint256 policy caps are concrete zero values, and a new actual HTTP/generated-ABI regression covers the policy, operator, commitment, vault and epoch getters. It changes no production timeout or retry behavior. Qualify the corrected fixture against all four affected original roots; retain original failures and other passing bodies.

The [partial e51 entitlement failure review](evidence/entitlemente51-partial-abi-tuple-fixture-finding-20261004.json) confirms three actual failures share a coordinator-fixture panic: policyAt leaves the two big.Int deposit caps nil, and ABI Outputs.Pack panics before responding. The 300-second GET retry runs correctly on EOF; the later artifact liveness guard never reaches HTTP. Initialize every ABI tuple field, inspect adjacent builders and add deterministic pack/public-route regressions. Preserve the original failures and keep remaining partitions running; this partial observation is not a complete 20-root verdict or production-node diagnosis.

The [validator14 two-mode terminal and vet readback](evidence/validatorb6-normal-race14-vet-independent-readback-20261004.json) independently reproduces fourteen normal and fourteen race passes, zero skips/failures, joined successful bodies and vet. Both protected images match their original receipts. Run the six staged causal control groups before claiming the full selected scope qualified; these results do not qualify other role or funding changes.

The [funding custody child review](evidence/fundinge0-custody-source-root-review-20261004.json) verifies exact e0f1011d over72 with four changed paths and six declared regressions. Public funding projection checks original owner/context before and after arithmetic and returns nil on refusal; private cold admission retains its one complete final fence. Tests count exactly two complete hot checks without payload rereads, refuse late equal-byte inode replacement/cancellation without JSON, and preserve a separately held original across candidate refusal and identical retry. The suspected shared-candidate mutation is not established as a product defect; the actual public isolation test remains to execute. This source correction is not qualification.

The [old normal 129-review timeout readback](evidence/restore-ade5-normal129-original-timeout-readback-20261004.json) independently counts one executed failure at 7200.04 seconds, joined exit 1 and the original archive-admission/custody stack. It is consistent with the reviewed repeated-prefix source defect, but proves neither corrected throughput nor a corrected full-history pass. Keep the original failure and automatically advanced race case; corrected ordinary work proceeds independently. Do not classify this local test deadline as an ephemeral RPC outage or restart preparation.

The [corrected e51 mainnet build readback](evidence/conservatione51-two-mainnet-builds-independent-readback-20261004.json) verifies successful normal/race compiler exits, joined processes and original owned test-image hashes. Sol runs the 47 ordinary roots in 20/20/7 partitions per mode and preserves the separate 129-review singleton, prior compiler failures and validator results. Compilation is not behavior or final conformance acceptance.

The [selected funding72 mathematical review](evidence/funding72-selected-mathematics-source-review-20261004.json) retains exact source and eighteen declared ordinary/public roots. Fungible stake uses conservative income/non-income bounds; missed roots and later claims transport original value without new earnings. Full Yuma tolerance is counted once, and reassessment drops stale target, fee and source projections. Qualify actual public capture/carry/credit/payment and cold/lost-ACK continuations after the owner-fence successor; candidate map isolation remains under review. The complete original measurement, finality and provider-fee producer joins remain required. This selected source review proves no execution or readiness.

The [funding-cache publication review](evidence/funding-public-cache-custody-source-finding-20261004.json) finds a related WIP owner-fence gap: earlier component admission precedes direct cold-funding map/counter use, and the public summary has no final archive-owner check. Bound summary computation/publication with original owner and cancellation checks; late identity loss must return no accepted projection. Keep private cold admission and its one complete final fence separate so this correction does not restore repeated historical prefix work. Add closed/canceled/replaced-owner and healthy no-reread regressions. This is a source finding assigned to Astra, not a reproduced test failure or completed correction.

The [actual b6 normal validator body](evidence/validatorb6-normal14-independent-body-readback-20261004.json) passes all fourteen selected roots with no failures/skips, exit 0 and joined processes. Root independently counted original test2json events and rechecked the protected image. Race, vet and six causal controls remain separate; preserve the successful normal body while correcting the unrelated mainnet precompile fixture.

The [e51 fixture correction and original four-build readback](evidence/conservation-capturee51-source-and-original-build-readback-20261004.json) verify the actual imported geth1.17 interface, exact six-line insertion, two retained mainnet compile failures and both successful protected validator images. Continue fourteen validator roots per mode using those images; compile only the corrected mainnet package and then resume its 48-root union. Source admission is not corrected execution.

The [immutable participant successor review](evidence/server-participant5b22-source-root-review-20261004.json) verifies exact Server 5b22 over 4a5 with only two changed paths. First admitted provider identity survives ordinary and companion retries after membership changes or removal. New streams still require current identity; conflicting retained roles hold only affected work. Qualify ten new and 22 neighboring model roots per mode, model vet and three causal controls/seven intended red roots per mode. Missing/canceled/aborted first admission must publish neither stream link nor partial participants. This is source-only and leaves the original full model result intact.

The [original b6 compiler failure](evidence/conservationb6-mainnet-original-compile-failure-readback-20261004.json) retains three exact missing-Name assignment diagnostics, exit 1 and joined compiler processes. The effective VM interface must be checked for every actual or fake precompile before qualification; graph resolution alone does not establish source compilation. Fix the test fixture against the pinned interface, collect remaining independent compile outcomes and preserve valid validator/history work. No mainnet body ran in this failed compile.

The [clean terminal-custody join review](evidence/conservation-follow-terminalb6-source-root-review-20261004.json) binds b6 to exact ffe plus two frozen464 validator paths. Terminal cached closure reads now recheck the original owner before returning an owned copy; cancellation or directory replacement refuses without a source-history replay. Sol is staging this exact candidate; 62 roots per mode and 14 declared controls are not yet qualified.

The [joined follow/history source review](evidence/conservation-follow-historyffe-scoped-source-root-review-20261004.json) verifies exact `ffe74b36`: native-worker isolation, bounded private cold admission and the actual-monitor restore fixture correction are joined without manual resolution. Cold admission reads every original page and runs one complete final custody fence before publishing its index; hot use and fresh reopen retain their own checks. Restore tests obtain actual runtime companions and compare the checked original durable state. Qualify48 mainnet and11 unchanged validator roots per mode plus13 controls; keep the full129-review singleton isolated and preserve prior successes. This join does not close funding conformance or the separate newly discovered terminal-custody seam.


The [dependent dual-engine normal19 readback](evidence/admissionab0-dual77-normal19-independent-failure-readback-20261004.json) records nine passes and ten failures with every root executed and no skips. Nine failures share a missing completed-block runtime read context; trace the restore fixture finite-CLI path against the real complete monitor observation rather than fabricating runtime fields or weakening admission. The separate full restore-state comparison failure remains under investigation. Preserve the nine successes, continue race and long-history work, and batch root causes with adjacent deterministic tests before qualifying affected successors.


The [continuous native-worker source review](evidence/conservation-native-followce78-scoped-root-source-review-20261004.json) verifies `ce78b81c`: one bounded native capture/replay worker hands its actual result to the parent under the exact original native predecessor and policy. Vault, Claim and artifact publication can continue while native work is pending; missing evidence remains pending, transient reads retry, and a genuine native contradiction holds only that domain. Cancellation joins the worker before outer custody closes. Qualify seven new and five neighboring roots per mode plus three exact controls. Full native-income-to-entitlement funding conformance remains separate and must exclude opening stock, deposits and refunds from earnings.


Qualification scheduling must follow actual workload and resource ownership. A restriction against a second long128/129-history body must not serialize unrelated ordinary dependent tests behind the first. Once exact source/image/export prerequisites and fresh combined resource admission pass, run the ordinary work alongside the retained long body with its original reservation included. Keep per-phase concurrency bounded and retain original outcomes; an unfinished long history is not a prerequisite for independently scoped ordinary roots.


The [three drained-exporter causal controls](evidence/proof-drained-continuation77-three-controls-independent-readback-20261004.json) independently reproduce the exact old first-block guard failure, repeated-accrual second-block failure and invalid nonzero opening-parent rejection. Every isolated mutation compiles successfully and reaches its named assertion; setup errors do not count as proof. Together with the six retained positive roots this qualifies the targeted fixture correction, while dependent Go execution and full composed-release acceptance remain separate.


The [populated composed-source review](evidence/conservation-populated3a13-composed-source-scope-review-20261004.json) verifies exact `3a13c69` inputs,174 Go declarations per mode,17 Rust roots and65 exact rebased controls. The join retains the large physical profile, complete restore owners, entitlement census, nonzero populated1,024/2,048-UID workload and corrected drained dispatch. Logical2x reserves must be followed by actual complete serialized-size and host measurements; sparse or zero-stake padding does not establish production capacity. Qualify the composed source separately from retained component results. Native-follow isolation, archive-prefix performance and full native-income-to-entitlement funding conformance remain subsequent implementation joins.


The [corrected exporter independent readback](evidence/proof-drained-continuation77-six-root-independent-readback-20261004.json) verifies a fresh Rust test image, six exact passing roots and all22 private hashed exports. This supplies the previously missing five-job proof-drained corpus without weakening the original post-state verifier. Advance the nineteen dependent Go roots using retained exact images; three causal controls and complete joined-source qualification remain separate. The original failed exporter and running long recovery remain retained.


The [corrected proof-drained exporter source review](evidence/proof-drained-continuation77-scoped-root-source-review-20261004.json) verifies the sole test-file change at `77a4ab38`. First-block dispatch uses the independently authenticated absent epoch marker, so a drained parent can execute accrual; later jobs retain that marker and do not repeat emissions. All five expected child roots remain independently constructed, and actual capture plus reduced replay are required before export. Qualify six Rust roots and three causal controls using a fresh libtest image while retaining unchanged qualified engines and Go images. Keep the original failure, successful ordinary roots and live long recovery; dependent Go results remain pending until the corrected corpus exists.

The [corrected single-root qualification](evidence/policy-refusalb1-one-root-and-retained-ordinary-qualified-20261004.json) verifies the policy-refusal assertion normally and with race detection at `b1`. Join that narrow result with the twenty unchanged original `ab0` successes per mode; preserve the original20PASS/1FAIL batches and vet result. This establishes the selected ordinary21 scope through retained evidence and one corrected test, rather than redoing every successful body. Dual-engine, long-history, causal-control and final composed-source work remain separate.

The [independent terminal and drained-program finding](evidence/admissionab0-ordinary-terminal-and-drained-root-finding-20261004.json) verifies20 passes/one diagnostic assertion failure in both Go modes and vet success. The new Rust test image compiles, but the proof-drained exporter correctly fails post-state reproduction: its initial accrual sits inside a nonzero-pending guard that a zero-pending parent cannot enter. Correct that specific original-program first-block gate and retain independent expected roots plus all later empty jobs; changing the verifier or copying its returned root would hide the defect. Keep the nineteen dependent roots visibly unexecuted until the corrected job corpus exists, while other implementation work continues.

The [independent ordinary21 readback](evidence/admissionab0-normal21-failure-and-b1-source-root-review-20261004.json) verifies20 normal passes and one retained diagnostic-order assertion failure. Both joined fee/Claim/native adoption regressions, actual initial Claim observation and the originally failing fee-revision root pass normally; their race/control and full joined scope remain separate. A narrow test-only `b1` successor accepts either actual original-policy refusal while preserving exact no-output, namespace and retained-identity checks. Do not label a refusal as owner reset merely because the diagnostic reached a different valid boundary.

The [joined admission source review](evidence/joined-admissionab0-scoped-source-root-review-20261004.json) verifies the batched `ab0` correction against eight exact changed Git inputs and41 declared roots per mode. After fee compaction forks an archive view, every later Claim/native adoption must consume the same fully admitted original view; a failed signed successor must leave the original checkpoint and obligations unchanged. Exercise lost acknowledgement after actual directory sync, repeated identical application and reopening from retained archives. Keep command-specific review files in separate namespaces so a later request cannot replace the storage preparation request. These source changes await behavioral qualification and do not relax custody or signature checks.

The [independent counter correction review](evidence/conservation-counterab0-independent-source-review-20261004.json) verifies the test-only `ab0` successor before compilation: compare the actual unsigned observation counter with an unsigned count and retain the original source finding. The corrected joined archive batch still requires its41-root normal/race qualification. The [independent normal full128 recovery readback](evidence/claim-full128-normal-recovery-independent-readback-20261004.json) verifies one actual public Claim-window root, exact RUN/PASS census and joined process exit; race and remaining recovery bodies are separate. Preserve successful original work while qualifying the successor.

Mainnet readiness remains unproven. The economic decisions are settled: providers receive 10% of the native miner allocation, 90% is recycled through the owner path, and paid/free completed traffic has equal weight. The October 6 `00:00 UTC` cutoff changes new-earnings attribution; pre-cutoff USDC obligations may finish paying later. Published configuration remains blocked from mainnet activation until the exact deployment package is ready.

The [requirement mapping correction](evidence/readiness-requirement-outcome-mapping-correction-20261004.json) restores every original outcome in the current checkpoint. PH-19 concerns historical runtime authority; economic funding and settlement belong to PH-11/PH-12 and MG-06. MG-03 retains complete durable recovery, and MG-10 retains release rehearsals plus the original post-activation observation intervals. Regenerate the readable checkpoint table from its JSON rows so dated component progress cannot silently replace the complete requirement. All38 requirements remain open; this tracking correction grants no implementation or acceptance credit.

The [fee/Claim archive handoff finding](evidence/fee-claim-archive-handoff-root-finding-20261004.json) confirms a product composition defect reached after correcting scratch ancestry: fee retirement forks a private archive view, then the planner updates Claim admission on the authoritative view while the candidate retains the stale fork. Reattach the exact admitted view after successful admission and before Claim/native adoption; preserve fee calculations and original authority rather than loosen the guard. The public regression must combine fee retirement with Claim/native continuation and verify refused admission publishes nothing. Collect the other checkpoint, drained-activation, restore-request/mount and declared-forecast failures in the same correction batch.

The [repeated capture source review](evidence/native-capture263b-sequence-source-root-review-20261004.json) preserves all production and module bytes while adding actual same-block transaction ordinals, next-block capture after archival and delayed first-receipt recovery through retired native inputs. An adjacent older outage fixture targeted a transaction removed by capture setup; explicitly target the actual original receipt and verify the fault is reached before interpreting recovery. Same-block deposited5 is not repeated earnings, and the archived opening stock14 is not counted again. The final pending union is86Go roots per mode and15Rust roots with its exact controls; execute the composed admission/profile/entitlement successor rather than rebuilding every superseded intermediate. FullUID zero-stake vectors do not settle nonzero-validator work budgets; qualify realistic weight/bond density and actual host sizing separately with the selected2x margin.

The [physical storage profile review](evidence/economic-storage5f5-scoped-source-root-review-20261004.json) verifies the fixed32MiB owner, separately signed monotonic logical head budget, original archive forecasts and matching preparation/restore registry. The full1024/2048 synthetic jobs retain every UID and row, with explicit zero-stake additional identities; they do not establish dense-population or production host sizing. Public tests cover larger heads, exact cold snapshots, signed growth and actual physical restore. Qualification remains pending on the composed source, including complete native-owner restore and entitlement reference/clone limits; preserve both original economic authority and existing successful scopes.

The [entitlement and funding source review](evidence/conservation-entitlement9a-scoped-source-root-review-20261004.json) pins fourteen changed Git sources and the original seven control groups. Commitment-transaction authority is distinct from the artifact signer; accepted live/cold claim leaves must match the complete original allocation. Carry edges retain their earlier epoch and pool, and fully paid Finalized roots remain active until actual expiry. Missing artifacts and unchanged capacity holds preserve progress and avoid repeating admitted work. This is source review only: the32-root normal/race scope, vets and controls remain, and the joined producer must separately establish native-income versus opening-capital composition before claiming the10/90 result. Include the actual drained-activation and archive-admission fixture baselines in the composed correction.

The [original composed Go terminal readback](evidence/native-adoptionade5-original-terminal-root-readback-20261004.json) independently verifies 75 file bindings and every selected raw root census: each mode completes fifty roots with seven passes, forty-three failures and zero skips; vet passes. All eighty-six failed roots report the same protected-ancestor refusal. These retained results do not establish product defects or final readiness; the separate protected-environment replay is collecting the previously unreachable assertions.

The [composed Go runner review](evidence/native-adoptionade5-go-body-operational-root-review-20261004.json) retains the exact fifty-root scope in both modes, including both original long-history cases. The [environment ancestry finding](evidence/native-adoptionade5-environment-ancestry-root-finding-20261004.json) establishes that a private scratch leaf below a group-writable ancestor cannot satisfy physical custody. Protect the complete owned ancestry before qualification; do not weaken production checks or change active directory metadata. Preserve the original terminal, successful roots, source and images, then replay only affected roots under a separately protected sibling scratch directory. A successful environment counterpart does not prove the remaining recovery behavior: the corrected replay has exposed additional checkpoint-census, restore-request and mount-admission refusals. Collect all outcomes and distinguish fixture reachability errors from production defects before batching deterministic corrections.

The [complete corrected restore terminal](evidence/native-restore729c-terminal-root-review-20261004.json) reproduces all 25 roots in each normal/race mode: eleven pass, fourteen fail, no skips; vet passes and all bodies join. The [three-file fixture successor](evidence/native-restore425-fixture-source-root-review-20261004.json) addresses both distinct setup causes without production changes. Qualify fourteen affected roots plus two new deterministic preparation roots per mode; keep the eleven successful roots. Fresh ancestors require explicit private protection, and future preparation targets must not acquire an owner generation before public preparation. The complete 129-review path remains isolated with its own long timeout.

The [Rust fixture resource admission](evidence/principal-activationec242-rust-resource-root-review-20261004.json) independently verifies all 29 staged crate files, exactly one changed test source and the original cumulative baseline with a two-times forecast. Run the three new/exporter roots and two fixture controls after the current lane joins; do not rebuild the unchanged capture/replay engines or repeat their 24 successful roots. A control-runner continuation must verify real sealed phase names and receipt layout: `a499-729-normal` cannot be looked up as the older `a499-normal`. Preserve the preflight refusal and use a distinct corrected runner; no product failure follows from that orchestration error.

The [signed native-renewal review](evidence/native-renewal1fe-source-root-review-20261004.json) verifies source `1fe78d5a`: original-key append-only adoption retains combined policy identity, independently signed inner reviews, pending job authority and authenticated archive history. Its nineteen-root normal/race scope and six causal groups remain unqualified. Compose the test-type and real activation corrections first; truthful archive entry capacity and a separate long-history timeout are required. Renewal must preserve completed work without treating a new runtime or build as a fresh owner.

The [allocation operational-cause finding](evidence/yuma-operational-cause-source-finding-20261004.json) identifies an adjacent recovery regression at source level. Original Yuma cancellation or witness-capacity exhaustion is wrapped as `errRpcIntegrity`; its consumer prioritizes that marker and retains a native hold. Cancellation during append recalculation also reaches a generic noncapacity hard-hold branch. Fix the actual replay/sample/save/follow path and test healthy continuation, capacity retention and genuine conflict precedence. Do not infer evidence disagreement from a timeout, cancellation or resource limit. The [exact principal successor stage](evidence/principal-activationec242-stage-root-review-20261004.json) verifies all nine paths against its physical base, including four new Go files; staging is separate from compilation and behavioral qualification.

The [corrected restore source](evidence/native-restore729c-test-types-source-root-review-20261004.json) fixes six test compilation errors without changing production behavior. Its [retained normal compile and runner review](evidence/native-restore729c-positive-operational-root-adoption-20261004.json) are independently verified. The [actual normal body](evidence/native-restore729c-normal-root-review-20261004.json) executes all 25 roots: eleven pass, fourteen fail, no skips. Failures reach storage preparation; the combined fixture has an unprotected fresh ancestor, while the native fixture encounters previous owner custody metadata. Trace and correct these distinct setup paths without weakening production custody. At that normal readback race/vet remained live; the later terminal above supersedes that state. Successful bodies remain retained, and the [adapted controls](evidence/native-restore729c-controls-operational-root-review-20261004.json) are admitted after their exact passing counterparts.

The [principal activation and subprocess successor](evidence/principal-activationec242-source-root-review-20261004.json) is source-reviewed at `ec242e3b`. Original parent proofs now start drained and the actual next-block program accrues before its epoch; the shared capture/replay worker retains bounded child diagnostics alongside the original wrapped process/cancellation/I/O cause. Fresh principal exports, sixteen Go roots per mode, three Rust roots, vet and five control groups remain to qualify. Diagnostic words must never determine retry or integrity classification. Preserve unchanged production engines and all earlier outcomes.

The [fixture normal readback](evidence/principal-fixture4af-normal-root-review-20261004.json) independently reproduces twelve executions: three pass, nine fail, no skips. Seven now reach the original drained-activation guard; two reach a rejected replay whose diagnostic is reduced to exit status. Preserve the actual parent proof with pending emissions rather than inventing a drained RPC state. Correct the original fixture to execute accrual from a genuinely drained parent, and retain bounded real child diagnostics with cancellation and source rejection kept distinct. The race body remains separate and continues; all successful bodies stay retained.

The complete native restore [positive runner](evidence/native-restore-a499-positive-operational-root-adoption-20261004.json) and [six-control continuation](evidence/native-restore-a499-controls-operational-root-adoption-20261004.json) are reviewed for automatic execution after the current lane joins and fresh admission passes. Exact normal/race images, real capture/replay engines, per-phase persisted resource samples and positive-counterpart bindings remain required. Control output paths include both group and mode; otherwise exclusive creation would fail on the second mode. These admissions do not establish behavioral success.

RPC authority lesson: public-RPC transport is approved while Snow synchronizes. Do not make an owned node a prerequisite or imply ownership from a legacy `owned-rpc-assertion` evidence tag. Review current route admission and authority labels while preserving historical signed policy domains; endpoint ownership, authenticated state and independently approved finality are separate claims. Matching same-provider routes do not establish independence.

The [targeted fixture runner](evidence/principal-fixture4af-operational-root-adoption-20261004.json) is admitted after its full mechanism/preflight review; [actual launch readback](evidence/principal-fixture4af-launch-root-readback-20261004.json) confirms the runner and normal compiler are live. Keep twelve selected roots per mode, vet and two causal controls per mode, retaining earlier successes and the distinct Rust engines. Its imported resource guard uses the stricter original twelve-GiB reserve, not the smaller proposal threshold. No test outcome is inferred from launch.

The [complete native restore resource review](evidence/native-restore-a499-finite-resource-root-review-20261004.json) independently verifies all 20,773 staged Git blobs and modes, measured 226,103,559 bytes, exact Core `c618` workspace inputs and 24 unique selected roots per mode. Preserve the original cumulative resource baseline with a two-times sixteen-GiB reserve. Split ordinary roots, actual capture/replay roots and the 129-review continuation into separately retained phases; keep six causal controls per mode and vet. Actual runner preflight and fresh physical admission remain required before execution.

The [effective fixture stage review](evidence/principal-fixture4af-effective-stage-root-review-20261004.json) verifies all four inherited/current test paths against exact `4af` Git blobs over the unchanged `dc1` base. Both virtual new files and existing helpers are present; the selected graph discovers them. The corrected twelve-root/two-control recipe is bound. Actual behavioral qualification remains; metadata discovery and source staging are separate from compilation and test results.

The [Core publication review](evidence/core-c618-publication-root-review-20261004.json) verifies the actual remote main and unchanged qualified production/test/module bytes; use `v0.0.0-20261004075743-c6186b79caaa` for dependency publication. Complete native restore still requires its separate behavioral gate. Overlay staging must include every inherited delta relative to its physical base, not only the newest parent commit: the `4af` fixture over `dc1` needs four exact test paths, including both retained `8d3` helpers. Metadata graph discovery alone does not prove those helper symbols compile.

The [principal fixture custody source review](evidence/principal-fixture4af-source-root-review-20261004.json) verifies the test-only `4af` successor and nineteen bindings. It provisions the actual checkpoint lock before declaring the synthetic volume, protects fresh archive metadata, and adds public tests that retain a lost lock or empty committed head without repair or extra source reads. Qualify only the ten previously failing roots plus these two new roots per mode, vet and two causal controls. Correct the inherited recipe wording before runner admission; preserve unchanged production/Rust bytes and prior successes.

The [read-only Core terminal review](evidence/core-c618-terminal-root-review-20261004.json) independently verifies 61 input/output bindings, ten exact passing roots in each normal and race mode, vet, and four named causal failures when the old production behavior is restored. Exact private read-only modes survive copy, lost acknowledgements and publication; genuine permission changes remain conflicts. Core `c618` is qualified for this component scope. Publish its module and join the complete native restore `a499` gate; component success does not establish full producer restore or mainnet readiness.

The fixture continuation is terminal: normal and race each retain ten passes and ten identical missing-checkpoint-lock failures, with no skips; vet passes. The next correction must provision the complete actual fixture custody before declaration and distinguish an absent initial head from an empty committed head. Retain successful bodies and qualified Rust assets instead of repeating unaffected work.

The [complete native restore source](evidence/native-complete-restorea499-source-root-review-20261004.json) is reviewed at `a4990a2f`, with 32 exact bindings, thirteen new public/adjacent roots and a 24-root normal/race scope. It restores the native proof/job namespace and independently signed approval files alongside the exact combined checkpoint, preserving partial writes, certified ancestry, renewal acknowledgements and a completed job whose acknowledgement was lost. The required Core `c618` dependency must be explicitly joined before qualification. The additive causal manifest keeps an original unused-import control-construction error and corrects it without altering product/test source. Actual bodies and six production controls remain to run.

The [fixture continuation normal body](evidence/principal-fixture8d3-normal-root-review-20261004.json) finishes ten passes and ten failures, with no skips. The fresh-root protection/tampering root passes; the remaining principal fixtures now expose missing checkpoint-lock provisioning. Fix the complete fixture ownership/provisioning chain and adjacent constructors together, keeping real runtime admission strict. Preserve the ten successes and original results; this batch does not establish complete principal accounting.

The [original principal Go batch](evidence/principal-dc1-go-original-terminal-root-review-20261004.json) is terminal and independently reproduced: normal and race each run all 38 roots with19 passes,19 identical fixture-custody failures and no skips; mainnet vet passes. Both protected images match their original receipts, and the wrapper/body processes have exited. The [reviewed incremental runner](evidence/principal-fixture8d3-operational-adoption-root-review-20261004.json) has started actual overlay compilation after its helper preflight and selected graph passed. It selects nineteen masked bodies plus the new protection/tamper root per mode, preserving the earlier nineteen successes and qualified Rust assets. Compilation, positive bodies, the fixture omission and six original production causal groups remain distinct outcomes to collect.

The [read-only Core successor](evidence/native-readonly-corec618-source-root-review-20261004.json) is source-reviewed at `c6186b79`. It admits exact private `0400`/`0600` files and restores the original mode after verified copy, before sync and publication. Four new public tests cover immutable bytes/mode/inode, lost acknowledgements and genuine changed-permission refusals; six neighboring roots and two exact old-body controls remain to execute in each mode. Publish and join the qualified Core version before qualifying the complete native restore adapter.

The [read-only native evidence restore finding](evidence/native-readonly-restore-mode-source-finding-20261004.json) confirms a cross-repository contract mismatch: finality publishes private read-only files (`0400`), while Core physical inventory accepts only `0600` and restore staging does not reseal the declared mode. Preserve exact original private modes through inventory, copy, interruption reconciliation and publication; refuse group/world permissions. Qualify a narrow Core successor and then the actual combined native job/checkpoint restore. Changing evidence to writable files would leave the underlying mismatch unresolved.

The [Yuma lifecycle review](evidence/yuma-owner-cancellation-source-finding-20261004.json) finds that the unfrozen allocation validator recalculates with a background context during active/history admission. Carry the real owner cancellation through derivation, reopen and summary, and stop expensive work when its finite arithmetic budget is exhausted. If validated work is reused, bind it to exact immutable evidence and authority. Add deterministic public canceled-owner and budget controls before this candidate is frozen; no behavioral success is claimed for the reviewed work in progress.

The [incremental fixture stage](evidence/principal-fixture8d3-incremental-stage-root-review-20261004.json) verifies the two read-only Go overlay files against the reviewed Git blobs, leaving the original complete source untouched. The finite twenty-root normal/race scope reuses qualified Rust assets and preserves unaffected bodies. Actual runner preflight, original-process completion, fresh physical resource admission and behavioral execution remain required; staged source alone is not qualification.

The [original dc1 Go normal body and fixture correction review](evidence/principal-fixture8d3-source-root-review-20261004.json) retain 38 exact executions: 19 pass, 19 fail and no skips. Every failed root reaches the same physical-custody refusal because the freshly created test directory inherited group-write permission. The reviewed `8d33892c` successor changes only two test files: provision the fresh selected directory explicitly, then prove the actual public consumer refuses later permission tampering without repair or another Claim read. Qualify the nineteen previously masked roots plus the new deterministic root in each mode and one fixture omission per mode; reuse exact qualified Rust assets after custody checks. Preserve original results rather than weakening runtime custody or repeating unaffected Rust work.

The [corrected Go operational admission](evidence/principal-dc1-go-fd4-operational-adoption-v2-20261004.json) retains both precompile wiring failures and pins the explicit ELF custody helper and its sibling import path. The complete runner must exercise its real imports, helper APIs and asset custody before an expensive phase; syntax checks and test declarations alone do not prove that launch path. No production deployment authority follows from offline qualification.

The [dependent Go capture/replay admission](evidence/principal-dc1-go-fd4-operational-root-adoption-20261004.json) binds the qualified original principal Rust assets to 38 public Go roots per mode and one vet. Current physical readback verifies 2,698 Go/module files against the frozen source. Use the original resource baseline, cumulative 56 GiB cap and a two-times forecast; run one compiler/body at a time, retaining exact images and sampled resources. Six Go causal groups remain separate. Admission does not imply any Go test success, live runtime authority or deployment.

The [two principal Rust causal controls](evidence/principal-dc1-rust-controls-root-review-20261004.json) are independently qualified: each changed private crate differs from the 29-file positive source census at exactly its declared production file, compiles freshly, and produces one named assertion failure with no ignored root. This completes the Rust component scope alongside the retained 24 positives and ten exported jobs. Actual Go fd4 capture/replay integration, principal effects and the complete economic witness remain separate.

The [original principal Rust positives](evidence/principal-dc1-rust-positives-root-review-20261004.json) pass all 24 exact selected roots without failures or skips, with ten actual exported jobs independently rehashed. The [fresh dual-engine build](evidence/principal-dc1-dual-engine-build-root-review-20261004.json) produces distinct protected capture/replay executables; the lib-test image is separately retained. The [operational admission](evidence/principal-dc1-rust-operational-root-adoption-20261004.json) preserves the original cumulative resource baseline and samples actual process-forest memory. Two Rust causal controls and 38 dependent public Go roots per mode remain separate; actual fd4 capture/replay execution and whole economic readiness are not inferred from these Rust positives.

The [original fee race body](evidence/fee-c04-original-race-terminal-root-review-20261004.json) completed all 49 selected roots: 48 passes, one fixture-status failure and no skipped, unfinished or timed-out bodies. The greater-than128-review history root passed in 2,845 seconds. The failed root correctly refused an archived receipt contradiction before another source read; its expected command status is corrected separately in `6820` and the reviewed successors. Retain the successful bodies and the original failed result. Qualification of changed economic behavior, causal controls and complete semantic restore remains separate.

The [corrected native receipt/finality source](evidence/server-receipt4a5-typed-causes-source-root-review-20261004.json) is reviewed at `4a5bbdae`. It uses the shared bounded cause inspector before invoking unwrap/network methods, retains unknown observations without inventing a verifier conflict, and preserves actual hard-refusal precedence. Eight new deterministic roots include real transport/body/close failure, charged-journal retention and healthy same-authority continuation. Qualify 24 roots per mode, two vets and four causal groups; preserve the completed model suite.

The [whole-read short partition](evidence/runtime-observation6fa-short-partition-root-review-20261004.json) independently reproduces 80 new passing root executions and retains the previously verified ten validator normal passes: 45 roots in each mode across five non-mainnet packages. All six vets pass. Raw events and nine protected images match; five mainnet roots per mode and twelve causal control groups remain separate. Preserve these successful bodies.

The [original principal-effects source](evidence/economic-principal-effects986-source-root-review-20261004.json) is reviewed at `986078c3`, with 58 exact bindings across 26 changed paths. Original before/after runtime queries and independent committed mutation census separate stock from income, retain unclassified net-zero writes, discard rolled-back effects and recompute archived totals from original checkpoints. Qualify the source independently; an earning label or matching balance cannot prove full 10/90 behavior. Actual native-to-vault capture join, full Yuma/quantization and entitlement/funding remain required.

The [native receipt retry source review](evidence/server1709-receipt-retry-typed-nil-source-finding-20261004.json) found an adjacent typed-nil dispatch gap outside the completed bounded-cause scope. Reject nil concrete receivers before custom unwrap/network methods, test actual no-retry/no-publication and healthy recovery, and qualify a distinct successor. Keep the original unexecuted scope and all successful component results; no live run failure is inferred.

The [bounded-cause causal controls](evidence/server1709-bounded-causes-controls-root-review-20261004.json) independently reproduce all six production omissions in normal and race modes: seven named assertion failures per mode, with successful positive counterparts and no setup, timeout or skipped-body substitution. This completes the selected retry/error-inspection component gate. Review exact changed-path coverage before Server publication; final release composition remains separate.

The [combined Claim-window source](evidence/conservation-claim-window7dc-source-root-review-20261004.json) is reviewed at `7dc19440`. Original-key offline proposals feed the existing durable archive plan/apply path. Unknown obligations, matched original receipts, cumulative reviews and bounded predecessor state remain retained through adoption and recovery. Qualify seventeen roots per mode and seven causal control groups; complete-owner cross-volume restore and full entitlement/root/funding census remain required. Forecast long-history duration separately with a two-times margin; preserve successful bodies rather than rerunning them after a package deadline.

The [Server bounded-cause positive scope](evidence/server1709-bounded-causes-positive-root-review-20261004.json) independently reproduces all 31 selected normal roots, 31 race roots and six vets on exact Server1709. Raw event censuses, protected images, selectors and source/dependency bindings match. The six causal omission groups remain separate; these successes do not close the whole release.

The [original opening-principal source](evidence/economic-principal-dc1-source-root-review-20261004.json) is reviewed at `dc1aeb15`. Original signed authority fixes the API/layout, exact parent and query census. Capture/replay use the same original proof, and the consumer independently verifies canonical results and retains the first stock identity through archive/reopen. Absent balances remain distinct from zero; incomplete observations preserve healthy sibling progress. Qualify 38 Go roots per mode, 24 Rust roots and the eight causal control groups. This stock-only increment does not prove deposits/withdrawals, the complete miner denominator/quantization, entitlement/funding, or activation readiness.

The [validator normal retry scope](evidence/runtime-observation6fa-validator-normal-root-review-20261004.json) passes all ten selected roots, including actual WebSocket reconnect through the production whole-read owner. It keeps original authority, retry budget, cancellation and genuine identity-conflict checks. Qualify remaining packages and race/control scopes separately; retain these exact successful bodies.

The [complete Server model body](evidence/server1709-complete-model-body-root-review-20261004.json) finished all 1,540 top-level roots: 1,529 passes, eleven skips and no failures. Test execution and owned cleanup exited zero. One PostgreSQL lifecycle observation error occurred after body exit; retain the original incomplete resource result separately and reuse the successful bodies. Configured-input, actual auto_explain and current source-host contract follow-ups remain; final changed non-model package and release composition are not inferred.

The [whole-read successor six-package compilation](evidence/runtime-observation6fa-six-compiles-root-review-20261004.json) passes on exact `6fa` source. Its fifty-root normal/race scope, six vets and twelve causal control groups remain. Both original reconnect failures are retained; compilation alone does not prove retry recovery.

These dated records preserve source identities, failures and scoped successes. Their older pending descriptions are historical; use the current table and readiness checkpoint for the next action.

The [manager recovery composition](evidence/miner-manager-composition549-source-root-review-20261004.json) is source-reviewed at `549f6926`: four real managers and96 synthetic instance lifetimes exercise durable-prefix reuse, fresh-owner reconciliation, manager-local replacement, write-before-HTTP dispatch and lost-reply recovery. Qualify its fifteen-root normal/race scope and five causal omissions with the required test build tag. This establishes a concrete test path, not SDK registration, resource sizing or full repair closure.

The [corrected signed fee-revision union](evidence/fee-revision3e7-source-root-review-20261004.json) is source-reviewed at `3e7feec7`. It joins the exact reviewed nil/index admission and command-status correction before revision/evidence mutation. Qualify its fifty-four-root normal/race scope and nine controls; keep the unexecuted bb716 precursor and original failures separate. Combined Claim-window adoption and complete-owner cross-volume restore are confirmed missing mechanisms and remain assigned implementation work.

The [archive-index successor](evidence/fee-retention6580-source-root-review-20261004.json) is source-reviewed at `6580a2b4`. It preserves nil-archive receipt-only indexing and refuses nil operands, empty or malformed segment catalogs, invalid retirement references and closed custody before retained-index mutation. Three deterministic roots cover those adjacent cases, with known-receipt idempotence and the exact command-status fixture correction retained. Qualify its forty-five-root normal/race union, seven causal control groups and vet under a cumulative budget informed by the measured large-history test; keep all earlier failed and successful results.

The [original fee race result](evidence/fee663-original-race-timeout-root-review-20261004.json) retains seventeen passes, fifteen failures and fourteen unrun roots. Its cumulative one-hour package deadline expired after a passing greater-than128-review test consumed about48 minutes; the named final root had run only two seconds. Retain that successful history test, collect unfinished/affected scopes separately, and investigate actual history scaling and repeated fixture-image hashing/sealing costs without reducing the scenario. The [scoped warm-Go admission](evidence/scoped-warm-go-resource-admission-root-review-20261004.json) permits one compile and one body beside the active model using a conservative two-times forecast, preserved capacity baseline and independently checked lease; it does not modify live runner guards or erase earlier admission refusals.

The [retirement normal batch](evidence/fee-retention121-partial-terminal-root-review-20261004.json) retains thirty-three passes, two failures and six unrun roots after a nil-archive panic. The new admission branch assumed an archive and last segment even for receipt-only indexing. Define safe legacy/no-retirement behavior, reject malformed retirement before retained-index mutations, and cover nil and empty segment cases deterministically. Batch this correction with the already reviewed command-status fixture fix; preserve successful bodies and qualify the unfinished/affected scope independently.

The [signed fee-revision test graph](evidence/original-key-fee-revision-test-graph-root-review-20261004.json) resolves all849 selected packages without incomplete/error entries and binds all six Core consumers to published `ad6fc3a1`. Its metadata-only admission keeps the original resource baseline and prior preflight refusal. Compilation and fifty-one-root normal/race qualification remain; a graph pass cannot substitute for economic behavior.

The [whole-read retry successor](evidence/runtime-observation-owner6fa-source-root-review-20261004.json) is source-reviewed at `6fa7c4ad`: it preserves the original query and shared retry deadline, and rejects typed nil causes before retry authority or custom unwrap dispatch. Qualify its fifty-root normal/race union, six package compiles/vets and twelve operative control groups. The original reconnect failures remain retained; source review does not establish all-role recovery.

The [corrected fee normal batch](evidence/fee-c04-normal-terminal-root-review-20261004.json) completed forty-eight passes and one fixture failure, with no skips. Both public commands reached the intended archived-receipt contradiction: observer reopen returns status3 and archive-plan admission returns status2. The distinct `6820f42a` test-only successor makes those expectations explicit while preserving no-read, no-output and custody assertions. Qualify its five-root scope and causal controls; retain the forty-eight successful bodies and the original failed result. Mainnet economic readiness still requires opening principal, execution effects, the complete miner allocation witness and entitlement/funding census.

The [original-key fee revision](evidence/original-key-fee-revision-source-root-review-20261004.json) is source-reviewed atbb716 with51 roots per mode and seven operative controls queued. It preserves signed predecessor/review lineage and historical policies for backlog, and releases only the corresponding typed policy hold after signed adoption. Ordinary proof/signature quarantines remain held. Opening principal, execution effects, the full miner denominator/quantization and entitlement/root/funding witnesses remain required implementation; selected fee sums cannot establish whole10/90 conformance.

The [observation-cause review](evidence/runtime-observation-typed-nil-source-finding-20261004.json) found typed nil causes could acquire generation-retry authority or reach a custom unwrap. Reject them before dispatch and add deterministic controls while preserving bounded traversal and hard/cancellation precedence. Do not qualify the uncorrected source as all-role recovery. A quiet or buffered raw log also does not establish that a body has not started: use the live protected test process and owning lineage; never restart for observation uncertainty.

The [retirement logical-guard correction](evidence/fee-retirement121-source-root-review-20261004.json) is source-reviewed at121. Both tests now use owned publication and require the actual logical cause. Qualify its41-root normal/race scope, five operative controls and vet; preserve the unexecuted precursor sources. The [configured-input follow-up](evidence/current-model-skipped-input-followup-root-review-20261004.json) stages exact public config for eight currently skipped model roots on a separate fixture after active-suite cleanup. Keep the current complete run unchanged. The remaining source-host comparison expects a removed prober table; review the current consumer contract rather than fabricate legacy input.

The [transport race batch](evidence/runtime-transport-original-race-root-review-20261004.json) reproduces the same sole reconnect failure:32 passes, one failure, no skips. Both original modes remain failed and retained. Qualify the distinct whole-read recovery successor; do not repeat f3 or infer recovery from compilation.

The [fee-retirement review](evidence/fee-retirement-logical-guard-source-finding-20261004.json) found two adjacent tests whose direct checkpoint overwrite could satisfy a generic refusal before reaching the intended logical guard. Correct the unknown-obligation and archive-summary fixtures through the actual physical-head publisher, require the named logical cause and qualify omitted-guard controls. Keep physical-tamper coverage separate. Retain the unexecuted eca source/graph history; qualify its corrected successor instead of collecting misleading positives. Production retirement preserves exact archived proofs, hot unresolved identities and transaction deduplication, but its41-root normal/race scope remains unqualified.

The [fee-fixture successor](evidence/fee-fixture-c04-source-root-review-20261004.json) is source-reviewed atc04 with49 roots per mode queued. It preserves all production bodies and corrects the signer, physical publication and stable Claim birth fixtures. Three new deterministic signature tests require original-key admission before malformed-wire and canonical foreign-key refusal. Preserve ineffective original controls as unqualified and prove the corrected omission reaches its intended guard.

The [transport normal batch](evidence/runtime-transport-original-normal-root-review-20261004.json) completes32 passes and one reconnect failure with no skipped roots. An implicit WebSocket reconnect changes transport generation during authentication; the attempted proof correctly expires. Implement whole-read reobservation through the actual public owner within the original budget and pinned block. Keep generation binding and genuine returned mismatch refusals. Collect the original race result independently and qualify the correction as a distinct successor.

The [transport-generation successor](evidence/runtime-transport-f3-compile-root-review-20261004.json) compiles all three selected packages. Its normal33-root scope has completed with32 passes and one reconnect failure; collect race and qualify the whole-read recovery successor independently. Reconnect expiry must trigger bounded reobservation of the original pinned block and preserve healthy sibling progress; returned semantic contradictions remain hard. Compilation alone does not close the all-role runtime requirement.

The [fee-consumer normal batch](evidence/fee-consumer-original-failures-root-review-20261004.json) completed all46 roots:28 pass,18 fail,none skipped. Preserve the failed source and complete diagnostics. Astra is correcting three fixture seams together: native-fee signatures must roundtrip the existing bare canonical wire format; logical receipt-contradiction fixtures must use the owned checkpoint publisher instead of bypassing physical custody; and a fixed Claim instance must retain its original start time across refreshes. Add causal public-path tests and qualify a distinct successor. A signature-check omission that still passes is an unqualified control, not proof of protection. The independent Server fee-context scope passes nine normal and ten race roots, with both selected vets passing. These scoped results do not qualify the failed SN consumer or the whole economic witness. The healthy complete model suite and original SN race batch continue independently.

The [container-lifecycle observer correction](evidence/container-lifecycle-scoped-root-review-20261004.json) passes nine deterministic controls. It records actual same-ID teardown states, retries within a finite observation budget and accepts absence only after workload exit and confirmed removal. Unknown/live/foreign observations remain unavailable. This is a scoped helper result; the active model sampler remains unchanged and original telemetry failures remain.

The [corrected complete Server model suite](evidence/server1709-complete-model-live-root-review-20261004.json) has started behavioral tests on Server1709/SN663/Coread6. Root independently verified the live test binary and exact launch bindings. Collect the full suite without stopping at ordinary assertions; report any telemetry gap separately. The fee-consumer and Server exporter test binaries compile, and their scoped normal/race/controls can qualify concurrently within measured resource admission. Full release qualification remains open.

The [corrected Server1709 compile](evidence/server1709-actual-compile-root-review-20261004.json) passes all six affected packages under the selected SN663/Coread6 graph. This fixes the a2 missing-import build failure; no behavioral root was selected. The corrected complete model fixture is authorized to start automatically. An observer teardown gap remains separately reportable and must not hold or erase an admitted test body.

A new adjacent review found RPC observation failures still combined with unproven mismatch assertions in runtime-continuity window/finality checks, producer/stake runtime checks and EVM/native checkpoint parent/canonical checks. Return an unavailable observation before comparing values. A timeout does not establish changed runtime, wrong genesis, a broken parent link or finality regression. Reconnect invalidation also needs a typed, bounded reobservation path for the original pinned block; genuine returned contradictions remain hard refusals. Astra must qualify timeout/reconnect recovery and actual mismatch controls at these public consumers, preserving prior certificates, cursors and signed bytes.

The [current combined Server model attempt](evidence/current-combined-model-build-failure-root-review-20261004.json) is terminal before any test root: `task/metrics.go` calls `errors.New` without importing `errors`. Fixture cleanup succeeds; the original failed compile and separate teardown sampling gap remain. Astra is fixing the import and reviewing adjacent changed-package build seams. Sol must compile the actual complete selected model package before starting its corrected full fixture. A passing dependency enumeration does not establish successful compilation. Preserve the [earlier live observation](evidence/current-combined-model-live-root-readback-20261004.json) as history, not current status.

The [corrected405 payout body scope](evidence/model405-body-teardown-gap-root-review-20261004.json) passes eleven normal and eleven race roots, vet and the four intended fixture-control failures. Its original terminal remains `FAIL_OR_INCOMPLETE`: a Redis process disappeared between container inspection and cgroup read during owned teardown. All494 host-resource samples remain available, and cleanup completes. Retain the single telemetry gap; do not rerun successful tests solely to make the measurement flag green. The next run needs a documented scoped disposition, verified owned cleanup and fresh cumulative resource admission.

The new fee-consumer review exposed the same architectural risk as sim-testnet startup: an optional verifier was placed before the conservation follow loop and every failure returned a global error. Correct that path before qualification. Keep the prior checkpoint on failed admission, isolate fee issues from native/vault/Claim progress, and merge only completed verifier results into the current owner state. A slow verifier must not block sibling observations or replace their newer cursors with its older snapshot. Add deterministic outage, recovery and cancellation controls.

The [corrected independent fee worker](evidence/independent-fee-worker-source-root-review-20261004.json) is frozen at `663a0ea2` and source-reviewed. It uses a separate bounded verifier and merges results into the latest checkpoint; six new causal roots cover held reads, missing-request recovery, cancellation, request quarantine, the actual finite deadline and handoff conflicts. Its46-root normal batch has completed with28 passes and18 retained failures; collect the original race batch and qualify the distinct corrected successor with operative controls. The selected Server fee-context tests and vets pass within their reported scope. Complete fee retirement, signed authority revisions and whole economic witnesses remain open.

Continuous fee operation also requires two real mechanisms beyond the first source-only consumer: retire complete proof payloads into authenticated archives while retaining transaction deduplication and unresolved fees, and admit signed semantic/engine/checkpoint revisions under the original trust root. Keeping every proof hot until the checkpoint byte limit or pinning one runtime profile forever would reproduce the capacity and version-change failures from testnet. A selected transaction census remains separate from whole-provider fee coverage.

The [native profile continuation](evidence/native-profile-final-root-review-20261004.json) is terminal with all26 phases passing and no unfinished phase. Both actual engines compile; corrected exporter and unsupported-profile cases pass, the public seventeen-root normal/race scopes pass, and all remaining recovery/prefix controls qualify. Completed Server, capacity and prefix positives retain their exact inherited sources. The original failed batches remain. Corrected405 payout body qualification has finished successfully within its scoped telemetry exception; final composed source, complete economic witnesses and unattended operator admission remain.

The remaining production work has explicit owners. Astra Integration implements admitted fee consumption, original opening-principal effects, the complete native miner denominator/quantization witness and entitlement/funding census. Astra Current implements the shared bounded error inspector, pooled route replacement, multi-component repair checkpoints, Claim-window adoption and cross-volume restore, then unattended native capture/replay/monitor and approved runtime-profile renewal. These are code requirements, not tasks assigned to an absent operator agent. Root reviews and integrates each qualified increment; Sol runs independent tests.

Run one complete model suite on the latest combined Server source, rather than repeat the obsolete payout-only composition. Qualify the corrected final SN union separately. Check wrapper, shell, job and adoption interfaces together before fixture startup: filenames and dependency enum values must agree. A metadata-only refusal does not invalidate retained source or body qualification. Preserve original failures, resource accounting and all unresolved production requirements.

The [qualified Core read-cause fix](evidence/core-ac65-main-publication-20261004.json) is now merged and pushed on Connect main at `ad6fc3a1`. The complete durablevolume subtree matches the tested ac65 source; upstream ledger/transfer changes are preserved. The final SN/Server candidate must adopt the published module explicitly and qualify that composition. Existing Core631/2ea results retain their original source identity.

The four critical workstreams are proceeding in parallel: economic execution witnesses; durable recovery, receipt archives and resources; complete source composition; and independent qualification. Astra Max owns implementation and root-cause fixes, with Sol Medium running tests. The [latest independently checked results](evidence/parallel-critical-path-readback-20261004.json) establish Core ac65 at sixteen passing normal and sixteen passing race roots, vet and four operative regression controls. Server c2b finishes both modes at forty-three passes and two companion-fixture failures, with worker tests, vets and six controls complete and owned fixtures removed. The exact zero-byte-anchor correction405 and receipt-lineage correctionc16 are source-reviewed; their behavioral qualification remains pending. Continue only the affected scopes, retain successful results, and join each qualified increment without restarting preparation.

The native lifetime successor now compiles both actual engines and passes eight original Rust roots plus the borrowed-backend regression root. Two exporters exposed a separate test-fixture defect: synthetic bulk-memory instructions are unsupported by the pinned node execution profile. Correction40e keeps the production VM unchanged, and the completed continuation above passes the affected exporters and unfinished checks using retained engines. Original failures remain.

The [original native qualification batch is terminal](evidence/native-finite66-terminal-independent-review-20261004.json): 20 phases passed, four failed and 42 were not run because their dependencies failed. Both 32-root Server modes, both vets, four Server controls and all six capacity phases passed. The original failed result is retained beside the now-passing native/profile continuation. Its initial preflight exposed an expired resource-lock holder before any test ran. Keep cumulative capacity accounting tied to an immutable filesystem baseline and authenticate the current lease holder separately; process renewal must not reset consumed capacity or discard completed work. The corrected runner uses that separation. The [lease-continuity evidence](evidence/native-qualification-lease-continuity-20261004.json) retains the original launch conditions; resource consumption is not reset for continuation.

The [reviewed source integration](evidence/qualified-base-main-integration-20261004.json) is now on SN main at `4a2c2681`. Its code, tests and module files exactly match the qualified `2477` composition; only current documentation and evidence differ. Original failed results remain retained. The [Connect merge](evidence/connect-upstream-main-integration-20261004.json) is also pushed at `c5b10cc7`, preserving upstream changes and exact qualified restore bytes. SN keeps the qualified Core `2ea` dependency; no new combined-source test result is inferred. Native producer, renewal, conservation and Server model correction qualification are still pending.

**October 4 composed Go compilation:** The [normal compiler and contiguous exporter review](evidence/native-adoptionade5-go-normal-compile-root-review-20261004.json) verifies the protected normal test image, joined compiler tree and one additional passing original producer exporter with five exact outputs. No Rust rebuild or repeated successful body was needed. Race compilation and the fifty selected roots per normal/race mode, two distinct long-history partitions, vet and applicable causal controls remain; preserve the original source/module and imported-package census alongside the images. Full entitlement/root agreement authenticates obligations but does not classify captured opening stock or deposits as new native income; capital provenance and the policy result stay unknown until separately established.

**October 4 capture review and completed Rust controls:** The [terminal Rust control review](evidence/native-adoptionade5-rust-controls-terminal-root-review-20261004.json) verifies two distinct omission libtests compile and each reaches its exact intended failure, with zero skips and joined children. The earlier runner import refusal remains retained beside the corrected same-command preflight; it is not a product-test failure. Preserve the four passing positive roots and twelve exports. The Go fifty-root union additionally needs five contiguous original producer jobs; run that exporter once from the same already-built, owned libtest rather than rebuild or repeat successful work.

The [selected native-to-vault source review](evidence/native-vault-captured039-partial-source-root-review-20261004.json) distinguishes opening stock, deposits, withdrawals, refunds and liquid earnings, and checks exact native mutation/transaction/receipt identity. Authored public tests cover rollback, foreign identities, delayed receipts and unexplained net-zero movements. Actual execution and broader capture composition remain pending; add repeated-capture ordinal and archive-continuation coverage before final qualification. This partial review does not close production accounting or capacity requirements.

**October 4 activation and regression-control follow-up:** The [Yuma activation correction review](evidence/native-yuma-activation79c-source-root-review-20261004.json) verifies the two test-only changes and fifteen exact bindings: legacy, Yuma3 and liquid branches now check all three original parent-trie pending values before actual accrual and replay. Production bytes remain unchanged relative to the capture-join candidate; this is source review, not a passing run. Preserve all pending capture, typed-cause and capacity scopes when composing qualification. The [Rust control mechanism review](evidence/native-adoptionade5-rust-controls-operational-root-review-20261004.json) admits two independent one-expression omission crates under the retained resource budget. Fresh compile, exact named intended failure and owned process/image closure are required before either control qualifies.

**October 4 composed recovery qualification:** The [exact composed source review](evidence/native-adoptionade5-composed-source-root-review-20261004.json) retains native signed renewal, restore, drained activation and bounded diagnostics together. The [fresh Rust terminal review](evidence/native-adoptionade5-rust-terminal-root-review-20261004.json) independently verifies every staged Git blob (20,780 entries), all four actual selected roots and twelve fixture outputs; fresh capture/replay engines were built for this production closure. Rust causal controls and the fifty-root Go union per normal/race mode, including both distinct long-history roots, remain required. The qualified classifier itself rejects nonzero exits, ignored roots and mismatched censuses before returning; tracing that helper disproved a suspected missing outcome guard, avoiding an unnecessary patch. These four results are also independently confirmed from raw output.

The [Yuma parent finding](evidence/yuma-activation-parent-source-finding-20261004.json) identifies a separate test seam: Yuma-only exporters left the original parent proof funded while their RPC fixture reported zero. Correct the actual parent trie and retain observed original accrual for every Yuma branch; do not weaken production validation or infer qualification from source alone. Full physical UID capacity and entitlement/root/funding conservation remain in progress.

The [38-requirement checkpoint](evidence/current-readiness-checkpoint-20261003.md) remains authoritative for scope. Historical receipts below keep their exact source and test conditions. Component qualification does not establish production activation.

| Workstream | Verified state | Required next result |
| --- | --- | --- |
| October 6 earnings and Server recovery | Server5f financial scope passes22 roots per normal/race mode, vet and five controls per mode. Its full model run retains1,507 passes/17 failures/11 skips; test-only correctionc2b completes both modes at43 passes/two companion-fixture failures; exact anchor correction405 is source-reviewed and queued. Config preserves the inclusive UTC cutoff and pre-cutoff USDC obligations. | Qualify affected model corrections and skipped-input follow-ups, then deployed schema/config/worker adoption and duplicate-free settlement. Historical full-suite failures remain retained. |
| Miner, provider and claim recovery | Retained request recovery, GET retry, rolling expectations and claim consumer scopes are qualified by component. | Current all-role continuation, actual operator identities/custody and Claim archive adapter preserving unresolved obligations. |
| Native and EVM accounting | Native/EVM integration and component runtime/catalog/archive scopes are qualified. Continuous producer SNb32/Server8eb is frozen and source-reviewed, with separate actual capture/replay engines; both corrected actual engines compile; original Rust roots and populated capacity pass. The40e continuation passes all26 phases, including corrected exporters, node-profile refusals and public/control scopes under its original Core631 graph. | Qualify actual native producer and populated provider capacity; complete signed authority/runtime/resource renewal preserving accounting history, then prove native/vault/Claim conservation and the 10/90 policy. |
| Original-runtime execution witness | Bounded host and current collector/request integration qualified and merged. Collector retains twelve Rust roots/four fault controls/three Go-Rust cases; request retains ten normal/race roots/six controls. | Offline archive adapter and fee-context verifier qualification; actual parent witness, runtime/source/profile and independently admitted finality. |
| Restore and capacity | Same-root and cross-root exact512/two-head restores pass normally and with race detection. Cross-root core passes sixteen roots per mode; custody successor passes eight core/five public roots per mode, vets and fourteen intended old-body failures. The original defect remains recorded. | Integrate qualified copied-source custody successor and qualify nested-path/EVM/Claim restore composition; signed capacity adoption and actual host backup/restore rehearsal. |
| Validator efficiency and isolation | Correction mainnet37 and durablehead6 roots pass in normal/race modes; three vets pass. Original validator scope retains the same three failures in both modes. Foreground successor2477 is qualified:20 roots per normal/race mode, vet and exact production-gate/fixture-capacity controls per mode. | Qualified successor and retained monitor fixes are integrated on main; preserve their original test scopes. Full historical lineage/invalidation, production sizing and composed role continuation remain. |
| Final release and launch inputs | Qualified native/EVM and read-cause increments published; immutable Cargo and Go test-image guards integrated. Collector composition, Claim and restore candidates retain separate qualification scopes. | Compose qualified source/module/config inputs; verify repeatable build, approved runtime/genesis/RPC, two-operator four-role hosts, signer custody and rollout. |
| Production outcome | October6 config is published; no deployment, owner signing or observed mainnet settlement claimed. | Owner-approved activation, deployed contract/policy admission, monitoring/on-call exercise and observed conservation/settlement interval. |

The [full-lineage probe source review](evidence/release-lineage-heade65-source-review-20261003.json) verifies SN `e65bd674`, a single test-file change over the integrated collector source. Seven new roots and four neighbors now exercise original signed nonempty HeadEMA, real source-read counts, owned-copy isolation, cancellation, late refusal and a genuinely signed double-fold artifact. Twenty-three bindings and five original-control inputs match. This is uncompiled test source: qualify its eleven-root union once rather than running the superseded empty-head slice. Production-sized history, upload contention, durable cross-process reuse and Claim CPU/capacity continuation remain separate work.

The [copied-source custody successor qualification](evidence/native-cohort63-scoped-qualification-20261003.json) now verifies SN `63a74b7b` with Connect `77235bb4`. Eight core and five public roots pass in each normal/race mode, including the exact 512-segment/two-head restore; both vets pass. Restoring five original core bodies makes five core and two public roots fail at the intended assertions in each mode, fourteen regression-control failures total, with all control images compiling successfully. Root matched 155 artifact bindings and independently counted every result; the original broad graph setup failure remains retained beside its successful selected-import retry.

The fix authenticates the copied original once under retained custody before target effects, checks source identity through application and retains completed healthy-peer progress. It avoids rehashing full payload history at every target write. This qualifies the exact component sources, not a current combined release: preserve the newer collector, accounting, guards and module inputs when integrating the full restore ancestry. Namespace, EVM/Claim continuation and production host restore still require their own results.

The [finite mixed qualification batch](evidence/mixed-qualification-terminal-20261003.json) has finished all 35 distinct charged phases. Root rehashed 283 artifact bindings, checked the immutable final ledger, and verified that its host lease was released. Sampled cumulative filesystem consumption was 3,692,138,496 bytes against the 24 GiB cap. Preserve the original dependency setup refusal and the four intended regression-control phase failures; completed work is not restarted or discarded. This sampled consumption informs the next workload forecast and does not establish production capacity.

EVM restore SN `42f76551` now passes the selected 848-package dependency graph and compilation. No EVM behavior/race/vet result follows from compilation. The next finite batch must use the completed current-source restore/Claim composition and frozen repair/lineage inputs, with separate forecasts for any database-backed full Server model suite. Historical full-model receipts remain distinct from current Server `5f2edd47`.

## Qualification continuity and node-profile fidelity — October 4

The [qualified Rust result classifier](evidence/rust-output-classifier-integration-20261004.json) is integrated on main. Fourteen unit tests and two original-predicate controls passed. Twelve retained prefix outputs were wrongly rejected because diagnostic JSON interrupted the printed test status; exact root identity, exit code and final one-root census reproduce their success without running them again. All fifteen prefix positives have now passed; four prefix controls remain unfinished. Original failure receipts remain unchanged.

Reading a protected executable may change its access time. Authenticate its inode, content, permissions, ownership, size and modification/change times; do not reject unchanged bytes because access time advanced. The corrected custody checker passed six tests and an original-body control, allowing retained Go images to be reused.

Synthetic runtime fixtures must obey the actual node feature profile. Unsupported instructions should fail explicitly, with parent state and pending work preserved. Qualification should fix an invalid fixture rather than enable unsupported production VM features. Compilation, execution, economic conservation and live runtime authority remain separate results.

The [Server model correction batch is running](evidence/parallel-qualification-launch-20261004.json). Its qualification has been admitted for 45 model roots and one worker root in both normal and race modes, two vets and three regression controls per mode. Use the original cumulative resource baseline and collect all ordinary failures. Run one separately forecast full successor model suite afterward. Astra continues conservation, renewal, retry and source integration work in parallel; none of this establishes deployed mainnet behavior. The corrected 26-phase native fixture continuation is adopted and queued after model cleanup, with engines and prior positive results retained. Current admission uses the original cumulative 48 GiB grant and workload forecasts with 2x margins; historical 110 GiB free-space requirements below describe earlier batches, not an additional current launch floor.

## Bounded error causes and mixed companion accounting — October 4

Error classification itself must terminate. Recursive wrapper walks can loop on a cycle or exhaust the stack on a deep chain; unbounded joined-error traversal can also defeat an otherwise bounded RPC retry. Bound depth and total visited causes, preserve hard-error precedence, and treat exhausted or empty classification as unknown. Receipt absence, funding-only backoff, intentional host exclusions, payment retries and schema authority have different policies: share traversal safeguards without merging their meanings. A nonempty joined error containing only nil children must not establish a completed receipt-absence census. Require deterministic cyclic, deep, wide, nil-child and mixed hard/transient tests at the actual consumers.

The active Server correction batch exposed two companion payout fixture failures. Both retain one pending consumed debit and two escrow rows, including a deliberately unpersisted zero-byte forward anchor. A conservation oracle must distinguish that anchor from the settled paying contract, then prove the anchor remains unchanged, the exact consumed debt is retained, available payer credit cannot increase and provider allocation/account balances remain stable through drain and replay. Do not remove the invalid-row assertion without proving which rows can legitimately remain unresolved. Preserve the current failed outputs, collect the remaining batch, and qualify any corrected fixture in a separate successor. These are current findings, not completed fixes. The [independently counted normal scope](evidence/model-c2b-normal-independent-census-20261004.json) completed all 45 model roots with 43 passes and two failures; the worker root passed. The [terminal readback](evidence/parallel-critical-path-readback-20261004.json) now verifies race at43 passes/two failures, both vets and six intended regression controls, with cleanup complete. Correction405 is queued; the original failed result remains.

## Combined release scope — October 4

The [frozen source input readback](evidence/composed-source-input-readback-20261004.json) verifies the exact combined SN and Server source, module files and physically retained tests. This is provenance evidence, not a combined release test result. Qualification must include the nonempty lineage scope, complete Claim/EVM/native restore union and its namespace boundaries, plus the new durable-command conflict regression. Four historical Claim controls are now narrowly rebased and source-reviewed; all fourteen original Claim/EVM control bodies match the combined candidate, preserving later qualified error handling. Their execution remains pending.

Keep broader requirements explicit: existing capability tests do not establish pooled endpoint replacement across every role; individual repair tests do not establish completed-prefix retention across simultaneous component repairs; small transport fixtures do not establish production fleet evidence sizing. Implement and test those actual paths. The standalone Core read-cause correction remains pending and cannot be inferred from a release that still selects its predecessor. Its [small preflight and helper-interface check](evidence/core-preflight-and-control-rebase-20261004.json) passed: 132 dependency packages, the new test file discovered, and an actual protected ELF record verified through the qualified helper. Native and Core tests may overlap after database cleanup when their combined 12 GiB doubled data forecast and 48 GiB memory admission fit the original resource grant. Reuse qualified process/image guards; do not introduce a fresh custody-record grammar in each scope.

## Evidence sampling must preserve the run — October 3

The [preexecution sampler review](evidence/model-sampler-source-finding-20261003.json) caught an undefined cache reference in the proposed model-test resource sampler. An uncaught thread error could leave incomplete measurements while a thread-not-alive check appeared successful. No fixture or test operation had started. Keep the original source and fix the sampler separately; this is a test-evidence defect, not an established production worker failure.

Sampler completion must be explicit. Record unavailable resource and container observations with their original causes, and distinguish a completed sample stream from a terminated thread. An observation failure must not stop an already admitted model run or erase its raw outcomes. Qualify the sampler failure paths, then report any remaining measurement gap separately from test success. Apply the same distinction to production monitoring: failed observation is neither proof of changed authority nor evidence of a healthy outcome.

## Validator repair observation recovery — October 3

An unavailable manager, cgroup, policy or journal read does not prove that the validator generation changed. The [repair source review](evidence/validator-repair0fef-source-review-20261003.json) verifies SN `0fef99be`: nine changed files, unchanged module inputs and fourteen evidence bindings. The implementation preserves original read/cancellation causes before comparing returned state. Acknowledged generations and consumed stop/start reservations remain durable; later observation of the same generation must not issue another action. Confirmed inode or retained-marker loss remains an integrity refusal.

Fourteen deterministic new test roots and six neighbors are queued for independent normal/race qualification with old-body controls. Tests inspect persisted approval/journal state and actual stop/start counts, including failed readback after acknowledgment and recovery on the same generation. This source is uncompiled; current role composition, native accounting and production host adoption remain open. Observation failures must not manufacture a generation conflict or replenish consumed action budgets.

## Restore namespace fidelity — October 3

Restore must accept the paths already admitted by the retained inventory. Reusing the fresh-provisioning depth limit rejected valid existing data: inventory accepted 32 components, while preparation and monitor-tree restore capped paths at 16. The [source-reviewed successor](evidence/namespace-restoreaa46-source-review-20261003.json) permits 32 components for restore and keeps fresh provisioning at 16. Original absolute-path 1,024-byte and filename 155-byte limits remain.

SN `aa46d5c1` and Connect `e7f24717` include deterministic deepest-path, escaped longest-path and overflow controls through public restore and pending continuation. Root verified thirteen evidence bindings and six changed Git blobs. These candidates are uncompiled and do not inherit qualification from shallower fixtures. Require independent normal/race checks, current-source composition and an actual host restore before accepting this fix. Paths beyond the inventory ceiling must fail before target effects; preserve original references and accounting throughout restoration.

The [historical unchanged full-model run](evidence/customer-current-full-model-terminal-20261003.json) is terminal: Server `3f32d730` completed all 1,457 top-level roots with 1,437 passes, nine failures and eleven explicit skips; fixture cleanup exited zero. Root rehashed eighteen artifact bindings and independently reproduced the complete counts, failed/skipped names and zero unfinished roots. This source remains failed. The nine failures map to the recovery/query-guard, allocator and reservation-oracle batches below. Run one full successor suite after its affected normal/race gates qualify; unrelated optional probes must not delay broad collection.

The [thirteenth inactive-cache reclaim](evidence/cache-reclaim-13-20261003.json) verified removal of 222 unreferenced old compiler archives and recovered 3.02 GiB. It preserved active caches, modules, sources and evidence; a transient compiler-directory scan failure remains recorded. Qualification admission must still recheck the 110 GiB floor before each heavy phase. This local headroom result does not establish production capacity or backup readiness.

The [fifteenth through seventeenth inactive-cache reclaims](evidence/cache-reclaim-15-17-20261003.json) recovered another 3.84 GiB from 124 unreferenced compiler archives across three inactive caches. Active qualification caches remain untouched. This restores local test headroom, not production resource acceptance.

The [eighteenth and nineteenth inactive-cache reclaims](evidence/cache-reclaim-18-19-20261003.json) recovered 0.96 GiB from twenty-six unreferenced compiler archives. Privileged process checks, the bounded metadata scan, repeated physical identity guards and final absence checks passed. Preserve the active warm cache and recheck the 110 GiB floor before each heavy phase; this local cleanup does not close production sizing or restore.

Do not stop broad qualification on its first ordinary assertion failure. Preserve the running source and collect all failures; repair in separate candidates, then qualify the actual combined release. Runtime/source approval and production activation remain distinct from offline implementation readiness.


The [original local restore race failure](evidence/local-pending-original-race-failure-20261003.json) retains four coverage refusals and three passing controls. The [corrected external signed-input fixture](evidence/local-signed-input-corrected-normal-20261003.json) now passes public resume, lost-ack recovery and two adjacent controls; its two pending-outcome roots still fail because their mock hides earlier historical receipts. Qualify the narrowly corrected pending mock separately. These results establish progress without claiming complete local recovery or changing production coverage requirements.

Astra’s source review indicates the dispute rollback fixture reads the old reservation counter while the public create path uses the newer counter. This is an unqualified fixture diagnosis: require a nonzero pre-sweep witness, both counters and the exact request token, followed by failed and successful public settlement checks. Apply the same review to drift and TTL failures; preserve every original result.

The [frozen allocator successor](evidence/redis-allocation-source-intake-20261003.json) preserves the public payer-client distribution policy and atomic whole-grant attempts. Ordinary discovery uses bounded keyset pages in original financial order, with explicit capacity holds and configurable reviewed limits. Root verified all 5,386 physical Git blobs and 44 file bindings; its [eight new roots and seventeen neighbors now pass independent normal execution](evidence/redis-allocation-normal-qualification-20261003.json), with both fixture cleanups successful. Root rehashed twenty-seven bindings and reproduced all twenty-five distinct passing names, with no failures or skips. The [same twenty-five roots also pass race detection and package vet](evidence/redis-allocation-product-qualification-20261003.json), with clean fixture shutdown; root rehashed seventeen result bindings and matched the exact passed names. The [five causal groups](evidence/redis-allocation-causal-qualification-20261003.json) now reproduce seven intended failures per mode. Only the first group restores the exact old operative allocator; the other four isolate omitted protections. These controls remain distinct from the reservation-oracle scope. These selected gates do not close the final combined release. Unknown counters must remain retained while independently valid funding peers can continue; paid/free SN weighting does not authorize changing the Server funding policy.

The [admission policy review](evidence/admission-policy-boundary-review-20261003.json) distinguishes intentional approximate Redis admission from authoritative settlement and payout obligations. Upstream explicitly accepts cache-loss and still-open-after-24-hour over-admission; do not impose absolute admission consistency through a test correction. Verify public request-token TTL, legacy mirror drift and mixed public/legacy terminal settlement separately. Scheduled reconciliation has a five-minute recurrence and thirty-minute task budget. The targeted CLI applies by default and currently lacks a finite whole-operation deadline; improve cancellation and bounded execution without losing retained debt or changing the declared economics.

The [reservation test successor](evidence/reservation-oracle-source-intake-20261003.json) is frozen at Server `1d3c275b`. Root verified thirty-four file bindings, the clean source tree and an exact four-test-file delta over allocator `4aaf4402`; production and module bytes are unchanged. Its [five affected/new roots and fourteen settlement neighbors pass independent normal execution](evidence/reservation-oracle-normal-qualification-20261003.json), with both private fixture cleanups complete. Root rehashed twenty-seven bindings, reproduced the exact nineteen passed names and matched the verified allocator parent receipt. The [same nineteen roots pass race detection and package vet](evidence/reservation-oracle-product-qualification-20261003.json), with fixture cleanup complete. Root reproduced the exact nineteen race positives and matched the prior normal scope. One full successor model run is now active on this exact source; the original full-suite failure remains retained. The [old-oracle discriminator](evidence/reservation-old-oracle-control-20261003.json) reproduces two intended failures per mode by restoring only the old test-state reader. Its runner exit one records intentional assertion failures; fixture startup and cleanup exit zero. This is test-oracle evidence, not an old-production regression control. Public dispute tests keep the real public create path; legacy-only batching and TTL tests select their legacy owner explicitly. A test correction must not erase a production accounting failure or substitute legacy coverage for the public path.

The [adjacent Safe read review](evidence/safe-read-error-adjacent-review-20261003.json) found combined read-error/mismatch branches in owner storage, nonce, guard slots, retained digest and pending code/authority checks. They can label missing observations as changed authority. Fix these in a separate successor: return read unavailability or cancellation before comparing values, then retain strict rejection of actual returned contradictions. Qualify the real public read paths; pure local signature/hash/encoding failures remain permanent. The [separate adjacent-read successor](evidence/safe-state-read-source-intake-20261003.json) is frozen at SN `296980a0`. Root verified seventeen bindings, the clean commit/tree and seven changed Git blobs. Its six new controls and four neighbors include actual public Safe attempt preservation and unavailable archive state after a retained receipt. The [original normal scope](evidence/safe-state-read-normal-failure-20261003.json) is terminal with nine passes and one returned-conflict assertion failure. Root rehashed ten bindings and verified raw counts and exact failure output. Retain this failed source and qualify a separate correction; race and five old-source controls remain separate, and compilation alone does not close these paths.

Full-model skip attribution is now exact: `TestNetEscrowRevisionTriggerUsesContractKeyAfterUpgrade` requires authorized `auto_explain`; `TestSubscriberGuardQueryPlan` instead requires the explicit `ARIN_SUBSCRIBER_BENCHMARK=1` disposable workload. Source/config/GeoLite and optional benchmark skips remain separate input-dependent results. Follow-ups must retain their own sources and conditions, and must not rewrite the original eleven skips.

The [native runtime-renewal successor](evidence/native-runtime-renewal-source-intake-20261003.json) is frozen at SN `881b967a`. Root verified forty bindings, the clean commit/tree and twelve changed Git blobs; module files are unchanged from `00d`. It preserves original checkpoint identity while allowing reviewed append-only read-purpose runtime history, nonshrinking capacities and acknowledged retry-budget renewal. The [independent 26-root qualification](evidence/native-runtime-renewal-qualification-20261003.json) now passes normally and under race detection, with package vet and graph preflight successful. Root rehashed twenty-eight bindings, reproduced all four raw batches and verified twenty-six distinct passed names per mode without failures or skips. Eight causal groups and a separate actual-old-grammar positive remain distinct pending scopes. Literal compatibility fixtures are synthetic old-layout bytes; no old-binary fixture-generation claim is made. Continuous EVM economics and rolling claims remain separate implementation work.

The [actual public paired-restore successor](evidence/paired-restore-public-source-intake-20261003.json) is frozen at SN `39e2744a`. Root rehashed twenty source/intake bindings, verified the clean head/tree and all ten changed Git blobs, and reviewed separation of passive inspection from approved exclusive reconciliation. Pending-next selection uses physical inode authority, including identical-payload images; unchanged retained member observations avoid repeated payload hashing. The [seven-root normal result](evidence/paired-restore-public-normal-failure-20261003.json) is terminal with five passes and two assertion failures: an unexpected local member during original-outcome recovery, and changed-member admission-read behavior. Root reproduced the raw counts and failed names. Preserve this source and collect race results. Astra’s current diagnosis is two fixture assertions: the allowed-name census omitted the already reserved exact terminal intent, and the work-count assertion expected a read for mutation of an acknowledged immutable member. Root source review confirms that this member mutation is refused before hashing. Qualify a separate test correction that binds the exact original intent lineage and independently exercises an admissible pending-member read; no production defect or successor pass is established yet. Its historical Connect/Server graph does not qualify the final current release. Joined capacity/retention revision and actual current-role recovery remain required.

The [first continuous EVM monitor candidate](evidence/continuous-evm-monitor-source-intake-20261003.json) is frozen at SN `0fb5db88`. Root verified thirty-two bindings, the clean head/tree and ten changed Git blobs. It contains complete transaction/receipt checks, contract capture/carry/credit/payment accounting and a durable public worker. Seventeen new and nine affected roots plus seven operative controls require independent qualification; The [first seventeen-root attempt](evidence/continuous-evm-monitor-build-failure-20261003.json) failed compilation because a peer service-event fixture references nonexistent `Current`; no test root executed. Root rehashed twelve bindings and matched the exact compiler diagnostic. Fix the fixture’s actual serialized event semantics and the resource-review history gap in a distinct successor. The [test-only successor intake](evidence/continuous-evm-monitor-fixture-source-intake-20261003.json) is verified at `a9eff813`: root rehashed five bindings and the exact single changed test Git blob; production and module bytes remain unchanged. Its decoded peer assertion now uses fresh public event fields and the resume barrier. The [first seventeen normal roots](evidence/continuous-evm-monitor-original-normal-failure-20261003.json) completed with fifteen passes and two restart/cancellation exit assertions. Root rehashed twelve bindings and reproduced both failed assertions; no passing qualification is claimed. Keep the original compiler failure and this exact source. The [nine affected neighbors](evidence/continuous-evm-monitor-neighbor-normal-qualification-20261003.json) pass normal execution; root verified twelve bindings and all nine raw names. A diagnostic-only two-root rerun passed without a behavioral change, so it cannot replace the original failed batch. Astra is adding a deterministic after-open cancellation control and reviewing adjacent owner-load/cleanup classification before a separately qualified fix. Source review found that only the latest resource review is retained: preserve this immutable candidate and implement append-only acknowledged revision provenance in a separate successor. Receipt gas observations remain distinct from actual native payer withdrawal/refund proof, independent finality and independently expected entitlement. None of those absent domains becomes zero or verified from this source intake.

The [Safe fixture successor](evidence/safe-state-read-fixture-source-intake-20261003.json), `9223d8d3`, and [paired-restore fixture successor](evidence/paired-restore-fixture-source-intake-20261003.json), `3af77073`, are source-verified with thirteen and fourteen bindings respectively. Root verified exact test/document deltas and unchanged production/module bytes. Safe conflict checks target the exact account and distinguish legitimate proxy/singleton reads from retries. Paired restore binds the exact reserved intent lineage and adds a distinct admissible pending-stage read alongside zero-read refusal of acknowledged-member mutation. Both require independent affected-root normal/race qualification; original failed sources remain retained.

The [twentieth guarded reclaim](evidence/cache-reclaim-20-20261003.json) recovered 5.56 GiB from 450 inactive archives. Its minimum archive size is four MiB rather than eight; task path and that threshold are the only helper changes. The forty-eight-hour age, archive magic, process/reference scans and repeated identity guards remain enforced. Active cache and evidence stay untouched. The 110 GiB floor limits new heavy admissions; crossing it alone must never stop or restart an existing run.

## Historical RPC recovery lesson — October 3

A missing expected historical receipt is an unavailable read, not proof that
previously authenticated chain evidence changed. Retry expected historical
receipt and transaction-position reads within the original signed route budget;
retain typed unavailability on exhaustion or cancellation. New approvals should
use the preferred 300-second budget, while existing signed 60–900-second budgets
must remain unchanged. An intentionally nullable pending-transaction lookup has
a different contract and must not wait merely because its outcome is not known.

Do not join a transport timeout or unavailable read with a canonical-boundary
conflict. Returned malformed data or contradictory hash, status or nonce still
requires an integrity refusal. Recovery must preserve the original signed
transaction and durable outcome, continue healthy independent owners, and never
resubmit blindly. Qualify delayed success beyond sixty logical seconds, null-read
exhaustion, cancellation, positive contradictions and healthy-peer continuation
through the actual production path. The [production successor is now frozen](evidence/retained-evm-read-source-intake-20261003.json) at SN `5a0a8f66`: root verified seventeen bindings, all seven changed Git blobs and the clean commit/tree. Its [nine-root normal/race scope and package vet now pass](evidence/retained-evm-read-qualification-20261003.json). Root rehashed seventeen bindings and reproduced both exact nine-root positive censuses. The [four exact old-source causal controls](evidence/retained-evm-read-causal-qualification-20261003.json) now reproduce their intended retained-history failures normally and under race detection. Root verified twelve bindings, the single added test-file overlay and each root’s assertion output; these are distinct from the nine positive roots. The paired-head and Redis scoped receipts do not qualify it; its historical dependency graph also does not establish the final composed release.


The [October 6 payout transition target](MAINNET.md#october-6-payout-transition-target)
is **2026-10-06 00:00:00 UTC**. Track its implementation and deployment separately:
legacy pre-cutoff USDC obligations may finish paying after the boundary, while
new earnings use mainnet. Qualify before/exactly-at/after boundary behavior,
delayed settlement, queued work, retries, short final subsidy windows and
consistent usage attribution. Preserve unpaid balances and submitted-transfer
reconciliation; prevent double payment across the two systems. A committed
configuration alone does not prove a running worker has adopted the schedule.

The [transition candidate and exact scope](evidence/payout-transition-plan-20261002.md)
also expose financial hardening requirements: bind the actual amount and wallet
atomically when reserving a processor key; retry stale snapshots without
releasing debt; forbid bonus mutation after submission admission; retain
explicit adjustment provenance and unresolved historical excess. Cached
reliability scores must match the earning interval. Bounded diagnostic samples
must disclose that their liability census is incomplete. The first normal
batch has one fixture failure; the candidate and its original evidence remain
retained while the successor is fixed and qualified.

The earning boundary itself needs durable admission identity. Review of the
transition candidate found that `LoadProviderPayoutTransition` rereads the
schedule for each operation and the USDC attribution query compares contracts
against that current cutoff. The attempt basis freezes amount and wallet, but
not the schedule: a later cutoff edit could admit usage previously excluded
from USDC, and an earlier edit could block retained original obligations.
Bind the adopted earning policy to durable production state, or refuse a
conflicting declaration before planning or sending. Preserve the October 6
boundary across restart and recovery; deployment identity changes must not
reclassify already attributed usage. Add deterministic actual-path controls
for a declaration replacement between planning, limiter admission and send.
Astra implemented the separate durable earning-policy anchor at frozen Server
`cdcb61fa`: exact-boundary initialization is idempotent, readiness updates stay
independent, and existing accepted-attempt reconciliation remains available.
Unavailable database reads must retain their retryable classification rather
than become policy conflicts. Initialization belongs in the existing deployment
preparation path, with no repeated historical census. The [source checkpoint](evidence/payout-boundary-source-20261003.md) records
the implementation and 13 new controls. Its [independent 28-root normal/race, causal and vet/build qualification](evidence/payout-boundary-qualification-20261003.json) is complete. Final combined release and production adoption remain pending; earlier candidate tests retain their own scopes.

Cancellation must preserve every unpaid earning component, not just released
contract sweeps. Review found that canceling a legacy payment can leave its
subsidy and reliability amounts on the canceled row while the recorded subsidy
window prevents the planner from creating them again. Retain and resume those
original obligations exactly once, with their source window and adjustment
provenance intact; prefer the original payment over issuing a new subsidy window.
Cover both unsubmitted cancellation and confirmed
processor cancellation, repeated cancellation/replanning, concurrent planning,
and cancellation after the October 6 boundary. An uncertain submission or an
observed transaction hash must remain in reconciliation rather than become
replacement debt. Frozen Server `97d22989` implements this correction, with
test-only successor `63027130` retaining identical production and module bytes.
Its [three corrected fixture tests pass under race detection](evidence/payout-retention-fixture-race-20261003.json),
and [five affected package vets plus the CLI build pass](evidence/payout-retention-vet-build-20261003.json).
The [normal recovery controls](evidence/payout-retention-causal-normal-20261003.json)
now produce all 12 intended behavioral failures across six groups, with owned
fixture cleanup completed. Omission controls and modified old-body controls
retain their explicit labels. [Fourteen additional production recovery roots pass with race detection](evidence/payout-retention-attempt-race-20261003.json),
with no skips or race reports. The
[race recovery controls](evidence/payout-retention-causal-race-20261003.json)
now discriminate all 12 intended failures without race reports. The [full financial model suite](evidence/payout-retention-full-model-20261003.json) at exact Server `63027130` now passes 1,400 top-level roots and all 249 nested subtests, with zero failures and nine explicit skips. These optional/configuration skips remain recorded; the [separate nested SQL-plan follow-up](evidence/payout-retention-auto-explain-20261003.json) now passes. Later candidates retain their own qualification requirements. The [normal operative controls](evidence/payout-operative-causal-normal-20261003.json)
produce five intended assertion failures for actual final-send admission,
submission basis and CLI refusal propagation. Their [race scope also produces
all five intended failures](evidence/payout-operative-causal-race-20261003.json),
with no race reports and completed fixture cleanup. These deliberately modified
controls do not supply a live payout claim or qualify the later policy-binding
and read-retry candidates. Neither source is merged or deployed; these scoped
receipts do not establish complete financial qualification.

Processor recovery must also bind each response to its original attempt before
updating, completing or resetting a payment. A delayed terminal response must
not clear or complete a newer attempt on the same payment. Preserve the original
idempotency key, request and response in durable attempt history before clearing
current submission markers. Review cancellation, denial, failure, progress and
completion together; test delayed responses and repeated terminal retries with
explicit barriers. Unknown or contradictory outcomes stay in reconciliation.

Restore must distinguish physical storage identity from logical signing
authority. The owner-device reservation binds the full device configuration,
including its logical state pathname, to the original request. Copying files to
a new volume may require reviewed physical identity rebinding; it must not
silently change that logical pathname or manufacture a fresh signing reservation.
Keep the original device response, request and pending intent, and test recovery
through the real owner-signing entry point. The
[32-test independent restore-core normal scope](evidence/restore-core-independent-normal-20261002.json)
does not yet qualify that consumer or production restoration.

Read-only recovery inspection must support a nonempty signed history without
requiring a writable backend. The ledger preparation review found a decoder
dereferencing optional writer hooks through a missing disk owner. Frozen
candidate `d3c84fe3` skips only that absent optional hook; signature, canonical
encoding and byte-bound checks remain required. Qualify an actual nonempty
signed prefix, preserve its bytes, and use the pre-fix path as the panic
discriminator. Its [independent two-root normal/race scope and validator vet](evidence/restore-ledger-readonly-independent-20261003.json)
now pass on exact `d3c84fe3` with the pinned workspace dependencies. The [original `1982267c` valid-prefix control](evidence/restore-ledger-readonly-causal-20261003.json)
reproduces the exact nil writable-hook panic in normal and race modes, using
only a test overlay; original production bytes were retained. The broader
public ledger-restore adapter at `912315cf` now has a [separate six-root
normal/race scope and two passing package vets](evidence/restore-ledger-adapter-independent-20261003.json),
after correcting only private scratch ancestry. Original signed history,
pending phases and actual owner reopening are exercised; missing/partial
intent, wrong heads and foreign members are refused. The original setup
failures remain retained. The [old public-dispatch control](evidence/restore-ledger-public-causal-20261003.json)
reproduces the missing semantic-adapter refusal normally and under race, with
only test overlays. Broader restore composition and live restoration remain
pending.

Restore qualification must preflight the whole temporary-directory ancestry.
The [first independent `912315cf` public-adapter run](evidence/restore-ledger-initial-setup-failure-20261003.json)
encountered group-writable ancestor directories even though its immediate
temporary root was private. All six selected roots in each mode failed at that
setup check; the two package vets passed but supplied no restore behavior.
That is a fixture admission failure, not evidence of a product restore defect.
Preserve the original outputs, correct the scratch ancestry, and run the same
immutable source again in a separate scope. Do not weaken the production owner
or mode checks to accommodate a test runner's default umask.

Physical restore must inspect identity embedded inside owner data, as well as
outer custody attributes. The immutable-member census contains original inode
fields: rebinding only its snapshot attribute leaves an unusable restored owner.
Keep original signed member bytes and the original archive intact. Derive only
unsigned physical census fields from reviewed staging descriptors, retain both
census digests and the derivation lineage, and publish those same staged inodes
with a no-replace move. Qualify interruption on both sides of the move and
publication, including directory fsync and repeated exact reconciliation.
For shared roots, prove the complete union of overlapping owners' files and
heads; filtering an inconvenient overlap must not produce a false complete
restore. Astra is implementing this separate increment; current scoped ledger
and raw-restore receipts do not close immutable-member or multi-owner restore.

The [current preparation state](MAINNET.md#current-preparation-state--october-2)
distinguishes integrated source fixes, qualified candidates and the remaining
production work. All 28 hardening lessons and ten production gates retain their
full scope. A historical checkpoint below does not supply a later composition,
deployment or acceptance verdict; use each linked receipt's exact source and
dependency scope.

The [durable claim producer checkpoint](evidence/claim-projection-progress-20261002.md)
at `347605fd` passes 37 author tests in each mode and two package vets. Main `bd5e7e72` now integrates that source; independent 29-test normal/race
qualification and two package vets retain their separate scope. The separately
configured claim monitor is still being implemented. MG-07/PH-28 remain open for that consumer, current-source
composition, contract/native economic observations and actual alert/repair delivery.

The [tracked preparation checkpoint](evidence/preparation-tracked-module-20261002.md)
now seals 75 selected author tests per mode by an exact checksum-only source join,
seven package vets and three verification builds at SN `0384cbfc` / Connect
`7600ea5c` / Server `c2563f9a`. Candidate `9d7d57fc` preserves the latest
`e08e1b11` source and documents byte-for-byte; its later claim composition is
not behaviorally qualified by that receipt. Connect publication, independent
current composition, retained/restore semantic rebinding and joined capacity
revisions remain open. MG-09/PH-09 are in progress, with PH-20/PH-23 unchanged.

## Historical qualification checkpoints

**October 2 continuation checkpoint:** observer `6d398662` is now independently qualified at 17 normal/17 race roots plus vet. Composition `69f4bbdd` / server `10a8f4d8` now passes 78 selected normal/78 race roots and vet, with a separate independent 12-root normal/race scope and vet. The [exact sealed composition](evidence/durable-owner-composition-qualification-20261002.md) is an incremental source qualification. Miner startup isolation `c3707376` passes 37 normal/37 race roots, while callback isolation, member recovery ordering and production preparation remain separate work. The [checkpoint and retained receipts](evidence/mainnet-continuation-checkpoint-20261002.md) record exact scopes, a newly exposed pending-stage ordering defect, private fixture ancestry and immutable physical evidence paths. No launch or full-backlog closure is inferred.

**October 2 MG-06 release composition (MG-02):** the [qualified scoped successor](evidence/release-258e25b4-server0aa1-20261002.md) at frozen SN `258e25b4` / server `0aa1e244` now packages economic-observer `dd21ed00` and signed-schedule `258e25b4` with the prior owner, contract-admission, recycle and schema-752 composition. Sequential source and eight-image repeats match and pass independent readback. Fresh current-pair qualification passes 83 normal/83 race roots and three-package vet; an independent nine-root normal/race scope also passes. Thirty-six component receipts and unchanged-server tests preserve their original scopes. This resolves the earlier source exclusion for the new artifact only. All ten fresh unsigned-plan actions stay blocked; independent compiler/dependency provenance, production policy/configuration, rollout/restore, published/running image identity and live authority remain open. Earlier releases stay immutable. Later server `025802a5` retention-debt/cleanup changes remain outside this frozen release. Its [static review and focused qualification checkpoint](evidence/server-and-storage-progress-20261002.md) is now recorded: 33 server roots pass normal/race and four package vets; seven SN composition roots pass normal/race, with its original `6cfc4773` mutex-copy vet failure preserved separately. Three historical server roots pass normal/race. The [full model run and separate test-only repair](evidence/server-stats-fixture-qualification-20261002.md) are now recorded: original `025802a5` executed all 1,345 roots with 1,334 passes, one statistics-fixture failure and ten optional skips; the isolated `a3fc4270` repair passes all eight affected roots normal/race and package vet. The original full run remains failed, and no patched-source full-suite pass is claimed. No successor production release or deployment qualification is inferred.

**October 2 retained relay fixture correction:** the separate [test-only fix](evidence/relay-retained-fixture-qualification-20261002.md) at SN `734a82dc` passes 35 author normal and 10 race tests, plus 2 independent normal and 10 race tests; `./sim-testnet` vet passes in both scopes. It retains the original signed plan and journal while rebuilding fixture synchronization, canonical header coordinates and the actual producer activation domain. The original `6cfc4773` vet/header failures remain preserved. This does not qualify the broader model run or change the frozen `258e25b4` / `0aa1e244` production artifact.

**October 2 durable storage (MG-09 / PH-09): in progress.** The merged Connect `6cd720cf` [generation/read-admission primitive](evidence/durable-generation-progress-20261002.md) passes 48 author and independent roots per mode plus vet. The new [isolated owner-custody checkpoint](evidence/durable-owner-custody-qualification-20261002.md) separately qualifies native journal `695f6683` (42 author/independent roots per mode), snapshot `a5c765c4` (17 roots plus 17 subtests per mode), and miner fleet/claim `b3c3d66` (201 roots plus three inherited subtests per mode). Monitor `f1b445f9` passes 114 author and independent normal/race roots and vet; these are still separate consumer scopes. These sources require explicit public-entry policy and preprovisioned retained-member authority, preserve reads under write pressure, reconcile only exact pending bytes after the old owner joins, and stop only the affected role. Connect inventory-v3 `0a5cda0e` separately passes 57 author and independent normal/race roots and vet with bounded owner-attribute retention. A separate [CLI candidate `7fb6b6a1`](evidence/durable-inventory-cli-qualification-20261002.md) passes nine author and independent normal/race roots and three-package vet, including explicit owner-local commands and report-only rebound comparison. The [independent receipts and new composition failures](evidence/durable-composition-progress-20261002.md) are now retained. Connect constructor `71df099c` independently passes 59 roots per mode and vet (including the prior 57) and is merged; the current consumer `0a5cda0e` module does not inherit it. The former `cb2e3ffe`/v2 and frozen release receipts are unchanged. The separate [validator/root/bootstrap/local-blob adopter](evidence/durable-adopter-qualification-20261002.md) at SN `eb0abe22` / server `1b7cc78b` passes 111 selected normal/race roots and four-package vet in both author and independent scopes; five causal controls discriminate per mode. The real service-credential test passes separately in the author scope and its evidence was independently rehashed. Module-only server `005a9066` pins the same tested Connect source, with a separate standalone dependency join. The independent adopter receipt is source-pinned to that exact pair; immutable registry/member census, broader fixture migration and peer composition are not closed. Production offline root/lease/nonce/owner-anchor preparation now has [source-qualified public plan/apply adapters](evidence/directory-owner-preparation-author-20261002.json), covering fresh precreated roots, exact owner profiles and interrupted publication. Those adapters are not yet merged into the current production composition; private-root creation, retained/restore rebinding and joined capacity revisions remain open. Validator/bootstrap/root/server composition, deployment declaration assets, capacity/rotation policy, actual restore and a new exact release remain open. No source receipt authorizes deployment or closes all of PH-09.

**October 2 actual public composition failures:** exact test-only `ea2bb37f` reproduces three failures in normal and race: persistent root observation omits durable context, a missing completed passive-monitor checkpoint is recreated, and passive preparation admits a missing retained snapshot head. Separate fixture `22d4ee3e` over unchanged adopter production exposes 48 successor write-admission refusals plus two outdated typed-identity assertions (75 roots: 25 pass, 50 fail). Fixes and complete source integration are in progress; neither failed composition is a qualified launch candidate. [Exact evidence and scope boundaries](evidence/durable-composition-progress-20261002.md).

**October 2 passive observer continuation:** the rendered passive service uses `Restart=no`; source `653061a1` exits after one exhausted preparation-observation budget and abandons unused signed samples. Its separate 11-root author normal/race and vet receipt remains scoped to the earlier custody/retry fixes. Implemented successor `97c7ae85` retains the same owners, emits `storage-unavailable` without a fresh observation and continues at the signed interval; exact test-only `6d398662` now passes 17 normal/17 race roots plus vet independently, preserving the earlier RPC-count fixture failure. Separately, the exclusive successor writer fix `f3c8a618` passes eight new plus two typed-identity roots independently in both modes and vet. Complete composition and module adoption remain open. [Evidence and continuation lesson](evidence/durable-observer-continuation-progress-20261002.md).

Updated 2026-10-02. This is the production gate tracker for UR mainnet
SN25 (netuid 25). Sim-testnet is **closed with known exceptions, without final
acceptance**. There is no R49 requirement or instruction to resume it. Mainnet
hardening may proceed; launch readiness must be established on the selected
production release. No mainnet deployment or spend is authorized by this tracker.

**October 2 contract installation admission (MG-02/MG-08):** the
[qualified source correction](evidence/contract-installation-admission-qualification-20261002.md)
at `ebf69b9` closes two pre-send defects. The first reserve CREATE now checks its
pending balance against all remaining same-sender value/gas reservations. Proxy
and descendant review require distinct deployer, owner, guardian and oracle
addresses under both signed review and unsigned preview. Exact unchanged-source
controls reproduce an underfunded synthetic send and all 48 invalid role
admissions. Author qualification passes 35 normal/35 race roots and vet, with
both causal roots failing as intended in each mode; independent qualification
passes fourteen normal/race roots and vet and reproduces both baseline failures.
Scopes overlap and do not establish full-package or release qualification.
Original signed intent, custody, nonce/attempt bounds and separate Safe approval
are unchanged. The [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md) now packages it with owner `0f7c8698` and recycle `25aa1515`; the older `3d1e2ecf` receipts stay unchanged. Live authority,
accepted production policy, installation and activation remain blocked.

**October 2 bootstrap readiness custody continuity (MG-03/MG-08):** the
[separate original-preparation correction](evidence/bootstrap-readiness-custody-qualification-20261001.md)
at `3d1e2ecf` retains the five original markers, the passive three-marker subset
and their exact journals through readback, successor send and host decisions.
Replacement or lost completed custody permanently fails that open owner without
rewriting signed intent; valid pending preparation still resumes. Passive result
reporting preserves consumed starts and refuses unretained completion. Author
qualification passes 44 normal roots, fourteen race roots and vet. Independent
qualification passes all eleven new roots normal/race and vet; seven failures
on unchanged source and the separate final-validator-check omission are causal.
The [qualified readiness baseline](evidence/release-3d1e2ecf-serverac86-20261002.md) packages this source
at SN `3d1e2ecf` / server `ac86855d`. It is superseded for launch by qualified
owner-trim source `0f7c8698`, contract-admission source `ebf69b9` and recycle-custody
source `25aa1515`, now included in the [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md);
the `1d580d5e` predecessor receipts remain unchanged.
The [owner-trim successor](evidence/owner-trim-retained-intent-20261002.md) qualifies
the additional exclusive store and guarded original-byte submission under a
separate signed policy. Legacy root native authority remains unavailable.
Actual owner/device approval, residual-risk acceptance, external custody and
activation gates remain open.

**October 2 release tooling lesson (MG-02):** the [readiness release](evidence/release-3d1e2ecf-serverac86-20261002.md#terminal-seals)
completed both source and image repeats before its final wrapper failed on a
changed independent-addendum schema. Preserve that exit-1 attempt and resume
only incomplete binding/seal/planning phases from verified checkpoints. Require
explicit primary/addendum manifest and audit joins before final qualification.
The resumed seal had already written its base receipt when the reviewer requested
two explicit join assertions, so an external four-file guard records those joins
and binds the unchanged base seal, independent receipts and original failure. No successful build or qualification phase was rerun. Review the base
receipt and corrected guard together; neither authorizes deployment.

**October 1 original contract custody continuity (MG-03/MG-08):** the
[writer and borrowed-reader correction](evidence/original-evm-custody-qualification-20261001.md)
retains the physical marker, directory and exact journal through all eight
original actions and their receipt/installation readback. An identical-byte
replacement of a lock pathname can admit another owner; deletion of a completed
journal is lost custody. Neither permits recreation, another send or a ready
installation. Source `cb9f3aa2` fixes the eight writers; separate `1922981d`
fixes their shared readers, including the installation proof consumed by
validator admission. Diagnostic successor `4e6b4a7e` preserves the original
approval/predecessor mismatch explanation. Deterministic pre-fix tests demonstrate
the five writer failures and two false-readiness failures. The receipt records
exact normal, race, causal and independent scopes. Older root/trim stores remain
explicit follow-ups; the separate five-marker bootstrap readiness cohort is
covered by the October 2 qualification above. The
SN `28ebfced` / server `ac86855d` release baseline predates these corrections.
The [exact custody successor](evidence/release-1d580d5e-serverac86-20261001.md) now packages them at SN `1d580d5e` /
server `ac86855d` with matching local source/image repeats. Independent release
qualification, live custody and acceptance remain open.

**October 1 bootstrap finality closure (MG-04/MG-08/PH-04):** the
[native observation and refreshed EVM admission corrections](evidence/bootstrap-finality-qualification-20261001.md)
retain the authenticated opening finalized witness across historical selection
and close finality after dependent runtime, census, readiness and admission reads.
Canonical membership alone cannot preserve eligibility after finality regresses.
The adjacent second EVM admission pass now closes its selected native/EVM mapping
after account/contract reads and before send readiness. Late contradictions keep
the original signed custody and consume no attempt; healthy advancement preserves
the same transaction. Source `98df8b5f`, fixture successor `1d5ebe55`, and EVM
successor `41f053ab` have separate exact evidence scopes. All 166 native and 52
EVM selected normal roots pass; their race coverage combines disjoint 155+11 and
45+7 collections, preserving both initial package timeouts as failed invocations.
Vet and causal controls are complete; independent normal/race qualifications
remain separately scoped. The receipts also preserve earlier fixture failures.
The [bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md) now packages these
corrections at exact SN `28ebfced` / server `ac86855d`, with matching local
source/image repeats. Independent release qualification, current authority
and live acceptance remain open.

**October 1 installation clock continuity (MG-08):** the
[qualified contract readback correction](evidence/installation-policy-clock-qualification-20261001.md)
now binds the current coordinator epoch-zero effective block to the original
authenticated proxy CREATE receipt's EVM inclusion block. A later nonzero clock
can no longer renew the initial policy after an implementation restoration.
Author and independent normal/race tests exercise current getter and storage
mutation, with causal omission controls; the independent broad race wrapper's
resource timeout and the author's moved-checkout fence remain explicit in the
receipt. The historical SN233/server942 release predates this correction;
the [bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md) packages
it from exact SN `28ebfced` / server `ac86855d`. Complete current contract/Safe
history, signer custody, approved production policy, live installation and
independent release qualification remain open.

**October 1 historical native proof capture (MG-03/PF-03):** server `8a47dfe3`
adds a [bounded producer and offline replay command](evidence/operator-historical-native-capture-20261001.md)
for selected receipt parent/child raw storage witnesses. Exact native selection
comes from original receipt/finality proofs; durable request/byte reservations
and completed raw results survive interruption. Author qualification passes
173 non-database recovery/CLI roots per normal/race mode, vet and seven causal
controls in both modes. The three database census roots remain unrun. This
source is merged into server `main` at `24ac67d4` and included in the selected
server `6c39d307` release. Independent 173-root normal/race/vet review and three
causal controls pass; runtime decoding and debit/refund attribution, production
archive capability, service adoption, independent release qualification and
live custody remain open; actual fees stay null
and no authority or spending flags become true.

**October 1 server successor integration gate (MG-02/MG-05/MG-08):** the
[static `720e7c61` review](evidence/server-720e-integration-review-20261001.md)
requires migration **750** before the new server binaries, including with
subscriber enforcement disabled. Keep `subscriber_quality_policy_version`
absent/`0` and the v2 candidate classifier/MMDB outside active launch inputs
unless the actual SN Quality/force-minimum picker proves fresh miner-trail
progress for both operators under that policy. New strict enforcement also
filters fallback and named providers; legacy or unclassified miners can lose
all seed eligibility. Separate composed-source normal/race/vet, schema/readiness
and cohort qualification remains required. Preserve the exact `0b8e758d`
activation/operator receipts; a release selecting `720e7c61` needs its own source,
migration/config inventory and rebuilt image/source joins. This review executes
no tests and does not approve release or deployment.

The [independent schema 750 qualification](evidence/server-schema750-qualification-20261001.md)
now passes the selected migration/readiness, default-off, seed-picker,
normal/race and vet checks on disposable services. Its causal mixed-writer
control shows that an old-shape UPSERT can leave a newer positive
`arin_quality_verified` value in place after changing the location; the
strict v2 guard expression still considers the row eligible when the other
flags remain favorable. Keep v2 and the candidate MMDB inactive at launch.
That receipt does not qualify the full live provider join or a production rollout.
The separate [schema-751 correction](evidence/server-schema751-write-guard-20261001.md)
at server `a464bb3e` binds each attestation to a fresh write token and atomically
revokes pre-751 positives. Author qualification passes 27 normal/race roots,
vet, the real live guard/cache/rollup checks and two expected-failing controls.
It is outside the pinned server-720 release. Independent normal/race/vet and
trigger-omission controls passed; a successor
release, migration lock-duration qualification, fleet/lookup coverage and real
miner-trail/load canaries remain required before future v2 enablement.

The [11:24 UTC read-only route check](evidence/public-route-check-20261001-1124.md)
reproduces the earlier mainnet genesis, EVM 964 and runtime 470 at finalized
block 9,187,604 through the public entrypoint. Snow's VPN route still answered
HTTP 502 for three identity methods. Neither observation supplies independent
identity/runtime approval or changes the selected release source.

**October 1 passive-root host owner (MG-08):** the separately signed
[`activate-root-passive` lifecycle](ROOT-PASSIVE-SERVICE.md#independently-approved-static-host-owner)
now binds exact v4 preparation and private runtime to a fixed sandboxed unit,
a dedicated writable checkpoint directory, one durable acknowledged start and
bounded invocation recovery. It preserves both UR allowances and all original
signed bytes. The [scoped qualification](evidence/passive-root-host-20261001.md)
uses synthetic manager and chain fixtures only. Independent mainnet config/host
signatures, exact release/host custody, current root seat/stake and observed live
monitor health remain external gates; no service was installed or started.
The source follows release fence `6c801a25`, which cannot attest this later code.

**October 1 original-authority contract anchor (MG-08):** the
[installation producer](BOOTSTRAP-CHAIN.md#original-authority-evidence-anchor-and-installation-readback)
requires the exact coordinator binding event within the retained Safe execution,
reauthenticates original CREATE/link receipts and counted recovery, and proves complete
initial storage for all five contracts and the Safe at anchor inclusion. Fresh
current readback binds the same installation to a selected finalized mapping,
complete Safe storage and explicit contract views. Its stable installation hash
is available to later service admission; imported JSON is never a capability.
The command makes network reads only but may recover the original terminal
journal locally. The [scoped evidence](evidence/contract-installation-anchor-20261001.md)
does not provide live v2 policy acceptance, signatures, distributed custody,
complete execution history or activation. MG-08 remains blocked for activation.

**October 1 current-runtime root scope (MG-01/MG-04/MG-08):** the
[runtime470 review](../docs/spec/runtime-470-audit.md) binds the official immutable
source/release to the observed code and executed metadata. The full local rebuild
differs only in 22 hash-table seed constants in one Wasmi function; exact source
reproducibility failed. On 2026-10-01 the subnet owner approved the exact
observed v470 artifact and its documented one-function reproducibility
exception for **launch planning only**. Independent genesis/finality authority,
live signing, deployment and activation remain unapproved. Runtime470 has removed
`set_root_weights` and its old enable/cap storage; the historical v3 root service
is not a launch-capable weight writer on that artifact. The additive
[v4 passive root service](ROOT-PASSIVE-SERVICE.md) selects
`passive_accumulate_in_place`, retains both independently approved UR roles,
and observes one separately approved existing netuid-0 identity under a finite
policy and independent full-config signature. It requires no heartbeat write,
root native signature, nonce or spend budget. Existing signed v3 actions and
custody remain unchanged; verify any externally held commitment before choosing
the first new plan. Native Sr25519/owner Ed25519 wire shapes pass current metadata
checks; native ECDSA signing remains unqualified by the 64-byte adapter. Real
root registration/stake, independently approved genesis/runtime/checkpoint,
qualified mapping, actual service installation/monitoring and complete UR
production admission remain open. No transaction or live deployment is implied.

**October 1 current-runtime census discovery (MG-01/MG-08):** the separate
[`subnet-discover` route](SUBNET-CENSUS.md) consumes a sealed but unapproved
runtime/finalized snapshot, rechecks its exact runtime and retained canonical
block, then collects bounded SN25/root forward/reverse membership and observed
owner/generation. Its [qualification](evidence/runtime470-subnet-discovery-20261001.md)
exercises v470's exact official metadata outside the repository and an
identity-free protocol projection with synthetic state in committed tests.
This resolves the initial discovery dependency on a pre-existing approved
owner/generation policy without manufacturing one: every seat remains
unclassified, membership completeness excludes custody/roles, and reset/apply
authority remains false. Independent network/runtime/source, protected-role,
removal and custody approvals, the complete launch census and trim execution
remain open. MG-01 and MG-08 are not closed by this increment.
The exact-key batch path completed the retained public snapshot with 256 SN25
and 64 root registrations in 13.231 seconds, after two 15-minute per-key
attempts failed under HTTP 429. Final affected coverage is 69 roots normal/race,
exact full-metadata tests normal/race, and six causal controls in each mode.

The [public EVM mapping fixture correction](evidence/evm-public-mapping-fixture-20261001.md)
restores meaningful outage and fallback coverage after adding the public header
route. Forty-two selected roots pass normal/race and three causal controls;
production bytes are unchanged. The unpartitioned 964-root `./mainnet` baseline
exceeded its 20/30-minute package limits, so a full-package verdict remains
unproven until its roots are qualified in complete disjoint partitions or with
a measured larger deadline.

The [October 1 read-only public finalized snapshot](evidence/public-finalized-snapshot-20261001.md)
closes the operational raw-header-method gap for the selected Rao archive:
the reconstructed Frontier header matched the native digest at finalized block
9,185,377. It retains `unapproved_observation` and does not close independent
runtime-source admission or authorize signing. That capture's frozen SN
source is `2d53e6f2`; server is `ecbf3aad`, with immutable Connect `e1b5d77b`
and SDK `5d37be38` pins in the server release graph. The
[fresh exact-source composition](evidence/release-current-source-20261001.md)
has passed for 17 binaries, five selected contracts and eight image contexts;
all eight local OCI image builds/readbacks and their independent archive audit
also passed as separate supplements. The earlier
image candidate is superseded. Local builds are not a published or approved
production release.

The [current unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
binds clean SN `de0823ce` and server `0b8e758d` to another successful combined
Rao capture at finalized block 9,186,298 with unchanged runtime470 code and
metadata hashes. Both effective API module graphs still consume the reviewed
Connect `e1b5d77b` / SDK `5d37be38` pins; newer observed main heads are recorded
separately. Its exact source lock, partial release inventory, release input and
blocked plan are retained together. All ten actions remain blocked; network
and runtime authority remain missing. The earlier complete eight-image
aggregate does not cover these successor SN/server commits. Complete current
release composition/provenance and independent approval remain open.

**October 2 native economics context and timing (MG-06/PH-04/10):** separate
source pins `dd21ed00` and `258e25b4` correct two runtime470 archive-observation
failures and the CRv4 tempo-drift/initialization-phase mismatch. The
[qualification record](evidence/native-economics-context-and-schedule-20261002.md)
keeps observer and scheduling receipts separate. The explicit independently
signed profile gates only fresh production; old approval hashes and retained
nonces, signatures, epochs and rounds are preserved. Monitor forecasts select
the same reviewed semantics without declaring unobserved misses. Exact live
runtime/source authority, signer/activation inputs, denominator, quantization,
actual provider entitlement and recycled value remain outstanding. This is a
later source increment requiring a successor release, not MG-06 closure.

## Closed testnet evidence and remaining lessons

Keep the original result, later recovery, and code qualification distinct:

| Evidence | Established result | Production obligation |
| --- | --- | --- |
| [Original R48 terminal report](../sim-testnet/FINAL-4.md) and [result](../sim-testnet/peerreview/evidence/FINAL-4-R48/result.json) | Run `20260926T202718.915754659Z-release-1.0` stopped on 2026-09-26 at 22:48:54 UTC with **zero complete acceptance epochs**, five failed assertions out of six, and `final_acceptance=false`. The signed boundary is not an accepted interval. | Preserve the failed result and excluded observations. Qualify actual production behavior independently. |
| R48 process-log gate | Two API stderr backlogs exceeded a scanner's 64 MiB delta; cursors did not advance. Validator steering also reported a missed approved native epoch 1696. | Adopt durable chunked scanning without starving foreground work; reconcile a separately authorized future native boundary for both validators. |
| Post-R48 scanner repair, SN `df7ae2c4` | Implemented in [process_log_gate.go](../sim-testnet/process_log_gate.go), with a causal regression and adjacent tests in the aligned R48 workspace. | This is simulator code and qualification, not a repaired R48 verdict or proof of the production image. Integrate and measure backlog/fault-heartbeat behavior on the release. |
| Retained recovery and client-key rollover | Recovery initially had zero ready providers despite live APIs. Server branch `fix/r48-client-key-policy-runtime-20260926` contains `4b2c4587` and `9da52551`; its policy-domain history fix removed that registration blocker. The [retained resume receipt](/mnt/data/sn-testnet/qualification/r48-continuation-20260926/resume-server-key-fix.json) records `retained_runtime=true`, `setup_actions_dispatched=0`, and **`final_acceptance=false`**. | PF-05 has a concrete server fix and a retained recovery result. Merge/lock the compatible server, migrations and consumers; prove processed-key and proof readiness prospectively. A successful resume cannot supply missed acceptance epochs. |
| R46 handoff replay during later recovery | [Continuation stderr](/mnt/data/sn-testnet/qualification/r48-continuation-20260926/release-r48c1.stderr) reports `provisional lifecycle cleanup observation differs from retained handoff bytes`. The [R46 handoff](../sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/runs/20260925T172403.199659160Z-release-1.0/fleet-lifecycle-handoff.json) retains its earlier approved lifecycle. The reader used the current recovery approval while validating historical provenance. | Treat this as a historical-approval validator bug, not demonstrated byte corruption. Candidate SN fixes `93949ee3` and `bbed208d` retain the authenticated historical approval through cold and cached replay; composed qualification and release inclusion remain open. Never rewrite the old handoff or relax current authority. |
| R48 immutable usage repair | Server `74893863` and the [guarded repair evidence](../sim-testnet/peerreview/evidence/FINAL-4-R48/repair-review.json) quarantine 4,183 epoch-658 contracts and retain 3,082,728 reported bytes as uncredited debt. Both epoch-658 roots missed their on-chain commit window; later epoch-659 commits are separate chain progress. | Deploy compatible snapshot writers/readers, retain the debt exception, and prove new usage, deposits, roots, capture and claims end to end. Neither a local close nor later roots repair the missed interval. |

The local recovery links are retained operational evidence, not portable release
artifacts. Hash and include the relevant sanitized records in the production
qualification manifest before relying on them outside this workspace. Closure
of testnet does not erase native-history, economic, archive, custody or coverage
exceptions.

The [closed testnet service cleanup](evidence/closed-testnet-services-20260928.md)
also stopped its four unused PostgreSQL/Redis containers after preserving their
state. Production shutdown must account for owned containers as well as process
children; retained evidence must not depend on keeping obsolete services alive.

## Production gates in execution order

These gates consolidate the stable RT/RL/PF/PH IDs below. The [October 4 ledger](#reconciled-status-of-all-38-requirements--october-4) is the current status authority; this section retains the detailed gate definitions and earlier evidence. Historical `Planned` labels do not mean the corresponding implementation is still absent. `In progress` means closure is incomplete; `Blocked` names a required input or demonstrated incompatibility; `Done` requires the complete stated evidence.
An implemented simulator fix is not a completed production gate. Owners below
are component responsibilities; assign a named operator before rollout.

The release manifest must name the production roles and platforms being
admitted. Qualify every selected artifact and its consumed dependencies, while
leaving unselected services/platforms explicitly unqualified and unavailable
for deployment. For example, an explicitly amd64-only release does not require
an arm64 execution pass. This narrows qualification scope without weakening
custody, economic, recovery or runtime checks for the deployed roles.

**September 30 custody decisions.** The subnet owners report that SN25's
existing owner account is Ledger-derived. They have no Snow access and will run
the signing command on their own device using the Polkadot generic app; Snow
may import only an exact signed reply after checking the current owner,
signature scheme, runtime metadata digest, nonce, era and approved action. The
current sr25519-only owner-trim v1 packet is not a Ledger signing path; retain
its original liabilities. The [offline Ed25519 owner command](OWNER-SIGNING.md)
and [independent software qualification](evidence/owner-ledger-signing-qualification-20260930.md)
are integrated. A [pinned Linux native SDK artifact](evidence/owner-ledger-native-sdk-qualification-20260930.md)
has actual-extension and synthetic-device qualification; the owners' platform,
physical device and deployed runtime digest remain unqualified. A separately
approved Ledger action is still required before a live owner call: strict v2
retains enforced-window semantics; the [fresh best-effort domain](OWNER-TRIM-BEST-EFFORT.md)
requires its separate exact residual-risk approval and original signed intent.
The netuid-0 root hotkey has separately reported **hardware custody** whose
device/API remains unspecified. Current root registration, claim, stake and
basket operations require the actual owning/staker coldkey or allowed proxy;
that authority and its device are also unestablished and cannot be inferred
from the subnet-owner Ledger. The selected passive-root v4 observer requires
public identity and independent host/config approval without a native signer.
Reviewed v470 accumulation needs no periodic hotkey signature; live seat,
stake and earning evidence still determine actual root participation. Each operator keeps its own EVM demand-deposit signing
key in that operator's **secrets vault**, separate from the on-chain settlement
vault and from owner/root custody. Verify both operator `depositSigner`
bindings and deposit hotkeys against the current coordinator version before a
new funded attempt; do not infer deployment or funding from this decision.

The [qualified server deposit-custody increment](evidence/operator-deposit-custody-qualification-20260930.md),
integrated at `45e11196`, binds each new or replacement demand deposit to the
current operator signer/hotkey and canonical economic graph before staging. It
preserves the original principal, bounds the two-rao reserve allowance and
one-rao transfer loss, and reconciles retained intents before another attempt.
Fifty-four roots pass normal/race and eight fault controls are causal. Actual
operator vault provisioning, two distinct funded addresses, live deposit
receipts and independent monitor deployment remain open.

| Gate / priority | Owner and linked items | Current state | Next action and completion evidence |
| --- | --- | --- | --- |
| MG-01 / P0 — Mainnet identity | Node operator; RT-01/02, PH-04/14/18/19 | **Open for activation:** the [fresh unsigned plan](evidence/release-258e25b4-server0aa1-20261002.md#fresh-unsigned-plan) binds exact SN `258e25b4` / server `0aa1e244` artifacts to combined public finalized block 9192802, Bittensor/EVM964/spec470. All ten actions remain blocked and network/runtime authority remains missing. The [identity review packet](evidence/mainnet-identity-review-20261002.md) retains same-provider agreement and the v470 planning-only exception; Snow cutover and independent finality/checkpoint approval remain absent. | Review the exact unsigned bundle, independently approve genesis/runtime/source identity, and repeat finalized checks before signing. Keep finite public-RPC concurrency; local target declarations and matching public routes are not authority. |
| MG-02 / P0 — Reproducible production release | Release owner; RL-01, PH-06/16 | **Blocked for launch; scoped MG-06 successor qualified:** [exact SN `258e25b4` / server `0aa1e244`](evidence/release-258e25b4-server0aa1-20261002.md) adds observer `dd21ed00` and signed scheduling to the owner/installation/recycle/server-752 composition. Two sequential source repeats match 17 binaries, ten bytecode outputs and 81 non-log artifacts; two eight-image repeats pass independent full-byte readback. Fresh tests on the current source pair pass 83 normal/83 race roots and vet in three packages; 95 builder roots and a separate independent nine-root scope also pass normal/race and vet. Thirty-six component receipts retain their original source/dependency scopes; original SN132 server 46, sampler 13, composition 42 and role tests are authenticated without claiming a rerun. Repeated inventory of 62 files retains 26 broader migration sources and 752 AST entries; builder selection remains 12. Independent final seal/planning readback passes, but all ten plan actions remain blocked. The prior MG-06 source exclusion is resolved only for this new artifact. Independent compiler/module provenance, arm64, accepted production policy/configuration/services, rollout/restore, attestation/SBOM/scanner, published/running image identity and approval remain absent. | Independently reproduce the selected release, close production configuration/policy, role behavior and migration/restore gates, bind actual published/running identity, then review one immutable manifest for approval. Keep 363 SN and 372 server incomplete module qualifications explicit; later implementation changes require a successor composition. Local repeats, selected tests and readback grant no deployment authority. |
| MG-03 / P0 — Durable recovery and complete evidence | Transaction/recovery owner; PF-01/03/04, PH-01/02/05/07/17/21/24/25/26 | **In progress:** the [mainnet miner fleet](../miner/FLEET-MAINNET-RUNTIME.md) now persists signed register/publish/bind/revoke intents and reconciles their original canonical outcomes before any identical-byte retry; affected miner/onchain/chain normal, race and vet pass. The [operator receipt-census fix](evidence/operator-recovery-census-20260927.md) preserves signed candidates after an inconclusive read; 19 affected test roots pass normal/race, with package vet and formatting checks. A [qualified status-independent signature census](evidence/operator-signature-census-qualification-20260928.md) preserves original, replacement and cancellation bytes across selected operator databases and evidence stores with private create-only restoration. The [conditional offline receipt/fee join](evidence/operator-receipt-fee-qualification-20260928.md) retains missing/conflicting candidates and counts observed gas once per resolved nonce. The [qualified receipt commitment verifier](evidence/operator-receipt-commitments-qualification-20260929.md), integrated at server `fbe0c039`, now authenticates exact signed transaction/receipt bytes, status, cumulative-gas differences and raw-header ancestry relative to a supplied EVM boundary; all 52 affected roots pass normal/race. The [qualified bounded collector](evidence/operator-receipt-collector-qualification-20260929.md) is integrated at server `b7c8c743`; all 71 recovery/CLI roots pass normal/race and five causal controls pass. The [qualified native finality proof](evidence/operator-native-finality-proof-qualification-20260929.md), integrated at `44636e5e`, verifies weighted GRANDPA certificates, scheduled authority handoffs and the exact native/EVM commitment relative to a pinned checkpoint; all 89 roots pass normal/race and five causal controls pass. Checkpoint/genesis/runtime authority remains unapproved and actual fees remain null. The [pinned-runtime fee dependency review](https://github.com/urnetwork/server/blob/cfcbfcbaa13b4f4d298acfeca761a7252c18ddee/strecovery/ACTUAL-FEE-DEPENDENCIES.md), integrated as documentation at `cfcbfcba`, keeps generic phase-bound balance events and block deltas unqualified for gas attribution; failed/partial refunds require exact runtime evidence. Bounded native-state proofs and runtime-qualified debit/refund attribution remain separate from checkpoint approval. The [qualified bounded native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md), integrated at server `5ff7bf02`, preserves the exact collection through durable native proof capture; all 112 affected roots pass normal/race and seven causal controls discriminate in both modes. Independent checkpoint admission, owned-node capability, account nonce and native debit/refund proofs, service adoption, historical approval correction, journal retention and cross-host custody remain open. | Migrate the retained-evidence model into every production owner; reconcile every original/replacement/cancellation signature and historical approval. Crash/restart and cold/warm-cache qualification must preserve finalized work, custody, failed evidence and single ownership without repeated spend. |
| MG-04 / P0 — Runtime and native continuity | Chain/validator owner; RT-01 through RT-08, PH-03/04/10/18/19/22 | **In progress:** the [standard validator production path](OWNER-RECYCLE-PRODUCTION.md) uses separately signed schema-3 authority, an exact block/purpose-bound producer interface and original authority through startup, preparation, recovery and archive readers. Bounded content-addressed complete config/approval history now preserves signed sidecars, the original drain and proof progress across compatible independently approved renewals and source-file loss. Old configs remain read-only. The [qualified source-receipt correction](evidence/validator-source-runtime-qualification-20260929.md) separates original signing, parent execution and post-state views across an approved upgrade; 103 selected roots pass normal/race. [Downstream upload admission](VALIDATOR-UPLOAD-RUNTIME.md) projects exact runtime windows from those bundles without retaining producer authority. The [miner fleet mainnet gate](../miner/FLEET-MAINNET-RUNTIME.md) retains exact-artifact and uncertain-send recovery for its four mutations. [Complete-header authority and its adjacent correction](evidence/current-native-header-adjacent-authority-20261001.md) reject substituted coordinates across producer/upload/observation windows and retained receipt/application evidence; approved update digests and original recovery retain their signed windows. The [claim EVM finality correction](evidence/miner-claim-evm-finality-20261001.md) closes state, replay, receipt recovery and fresh publication using the EVM clock while retaining exact signed custody. The [shared onchain successor](evidence/shared-evm-finality-closure-20261001.md) closes finalized/canonical receipt witnesses and keeps transient or absent evidence pending within the original deadline. The shared nonce reader requires the exact reviewed 56-byte Subtensor account layout. The [signed continuity policy and inspector](RUNTIME-CONTINUITY-POLICY.md) have scoped independent qualification; [finite offline replay](RUNTIME-SEMANTIC-REPLAY.md) checks exact supplied artifact/state cases. Complete semantic proof and automatic production selection remain absent. No live mainnet authority or deployment is supplied. | Complete remaining consumers, both validator roles, automatic compatible-upgrade and missed-boundary qualification. Arbitrary policy/key/custody changes require separate transitions. Preserve original pending bytes and finalized work; no backdated native success. |
| MG-05 / P0 — Policy and identity rollover | Server/validator owner; PF-02/05, PH-07/13/27 | **In progress:** server policy-domain rollover and retained resume remain evidenced above; the v651→v724 migration-monitor namespace bug is corrected. The [qualified MG03/R48 composition](evidence/operator-mg03-r48-composition-20260929.md) is integrated at server `05fee56f`, preserving both original histories and exact signed approvals through registration replay, policy rollover and deletion. The [operator epoch-policy correction](evidence/operator-policy-custody-qualification-20261001.md) authenticates retained payout policy/window independently of the current configuration and composes both operators' populated migration, processed registration, restart and fresh proof reads. Live operator cutover and readiness remain open. | Migrate both operators before APIs, retain old signed histories, activate all validator/operator evidence domains and authenticate persistent peer-key transitions. Prove production processed-key readiness and fresh proof progress through a future policy boundary; retain prior-epoch payout policy authority for successor deposit sizing. |
| MG-06 / P0 — Economics, settlement and custody | Protocol/contracts/treasury owner; PH-11/12/14 and R48 usage lessons | **Blocked for live activation:** The [exact MG-06 release successor](evidence/release-258e25b4-server0aa1-20261002.md) now packages the observer-context and signed schedule corrections; its fresh current-pair tests and repeated artifacts do not close economic outcome or authority gates. The selected 90% owner-recycle has a distinct [production transition](OWNER-RECYCLE-PRODUCTION.md), separate from the unchanged read-only admission/capsule formats. It joins genuine provider proof replay, canonical coordinator facts, native owner/validator eligibility and a signed drained activation block, then binds the 10/90 row through real CRv4 preparation, a hotkey sidecar and durable intent/archive replay. Complete original authority history now preserves these signed decisions under compatible approved renewals without reinterpreting their economic policy or requiring another drain. The [bounded native incentive observer](evidence/incremental-source-composition-20260929.md) now retains canonical event/state evidence and sealed partial results: 18 focused and 170 adjacent roots pass normal/race, with eight causal controls. Native denominator, quantization, recipient generation, provider entitlement, actual owner recycling and the 10/90 outcome remain unresolved. No live approval, native payout or 10% outcome exists. The non-upgradeable [vault claim repair](../evm/CLAIM-RECOVERY.md) preserves accepted credit after an exact runtime payment failure; Forge 226/226 and focused receipt normal/race passed, but runtime rollback and deployment remain unqualified. NetEscrow migrations through 728 and fenced publishers are not deployed. | Obtain actual mainnet identity, reviewed source/code mapping, signed production approval, recognized owner recipients and eligible independent validators. Supply complete real activation, operator API/key/payout custody and authenticated history; qualify production receipt/restart and the runtime's exact native rounding. Monitor inclusion, reveal/application and the actual 10% native-miner / 90% recycle outcome after first submission; that outcome is not a circular first-send prerequisite. Complete vault rollback, reserve funding, deposits/capture/carry/claims and NetEscrow cutover. Recycling does not fund the reserve. |
| MG-07 / P0 — Continuous monitoring and bounded repair | Operations owner; PH-15/28, PH-01/02/07/09/11/16 | **In progress:** signer-free `inspect`/`monitor` identity and finality commands exist. The v3 checkpoint retains finalized continuity, last successful read and initial or later read outages across restart; warnings begin at two minutes and critical status at five, with immediate escalation on clock rollback. [Monitor telemetry](MONITOR-TELEMETRY.md) wires atomic textfile gauges into the actual command; [alert examples](monitor-alerts.example.yml) detect missing expected hosts, stale samples and explicit severity. The [offline xops deployment increment](evidence/monitor-deployment-offline-20261002.md) supplies explicit host/release/config approval gates, scoped textfile collection and an independent expected roster. Author and independent scoped qualification pass 32 new and 19 adjacent xops tests and 13 SN normal/race roots plus vet and deployment controls; actual installation, ingestion and delivered alerts remain open. The [qualified operator journal increment](evidence/operator-monitor-qualification-20260930.md) adds bounded read-only transaction/settlement observations with incident continuity: 62 Go root executions and four alert fixtures pass; nine normal and five selected race controls are causal. The [qualified stopped-validator resume](evidence/validator-repair-qualification-20260930.md) adds one incident-bound, independently signed start under exact release/unit/generation custody: 52 positive executions pass normal/race, and twelve normal plus seven selected race controls are causal. No unit was installed or started. The separate [active-hang capability](ACTIVE-VALIDATOR-REPAIR.md) adds an independently signed one-generation stop/join/start interface. Production approval, real-systemd rehearsal, deployment, unobserved domains, root/operator services and broader repair coverage remain open. | Deploy and verify actual collector ingestion and delivered alerts against an independent expected-host roster. Implement the remaining [operating model](MAINNET.md#continuous-monitoring-and-repair), approve its SLOs and repair envelopes, provision primary/backup on-call, and rehearse outage, wrong chain, missed deadline, uncertain send, full disk and monitor failure. Independent alerts must survive a stopped application and a stopped controller. |
| MG-08 / P0 — Mainnet bootstrap and both validator roles | Bootstrap/governance owner; PH-14, [MAINNET.md](MAINNET.md) | **Blocked for activation:** the [owner-trim planner](SUBNET-CENSUS.md), [recheck/reconciliation](OWNER-TRIM-GUARD.md) and [bounded qualification](OWNER-TRIM-BOUNDED.md) retain safe partial candidates and old-miner residuals; no full reset is claimed. The [offline chain composition](BOOTSTRAP-CHAIN.md) has [qualified v2 UR config admission](evidence/ur-bootstrap-admission-qualification-20260928.md) for exactly two initial schema-3 signed UR configs matching independent role/signer/runtime/source/deployment pins and protected generations. [V3 root-role admission](evidence/root-role-admission-qualification-20260928.md) independently pins the netuid-0 generation, action approver and signed full-service approver while retaining live authority as pending. Durable local plan/apply/resume preserves child signatures and allowances; v1/v2 recovery keeps its original scope. The [qualified read-only readiness phase](evidence/bootstrap-readiness-qualification-20260929.md), integrated at `6627d15f`, binds original v3 custody to current finalized UR/root prerequisites; all 148 affected roots pass normal/race with four causal controls. The [qualified durable owner-trim action](evidence/owner-trim-null-storage-repair-20260929.md), integrated at `ce567305`, preserves original v3 custody and passes 25 focused plus 229 expanded roots normal/race with eight causal controls; the failed R1 null-storage qualification remains preserved. The qualified [contract-role declarations](evidence/bootstrap-contract-role-qualification-20260930.md), [original receipt prefix](evidence/bootstrap-contract-receipts-qualification-20260930.md), and [current five-account field checks](evidence/bootstrap-contract-current-qualification-20260930.md) now retain exact original authority with 40, 42 and 38 positive normal/race executions respectively. Current fields remain owned-RPC assertions. The [original-authority anchor producer](evidence/contract-installation-anchor-20261001.md) now binds exact creation and anchor receipts to complete initial storage proofs and a fresh current readback; actual policy acceptance, live installation, complete history and activation remain unverified. The [qualified archive census](evidence/safe-history-census-qualification-20260930.md) now retains complete bounded native/EVM witnesses with 68 positive normal/race executions and ten normal/five selected race causal controls; internal/reverted execution and complete Safe history remain unproven and public submission stays closed. The separately signed [passive-root host owner](evidence/passive-root-host-20261001.md) supplies fixed sandboxed installation and one acknowledged process start/recovery under original v4 authority; actual host acceptance and live observer health remain pending. The [qualified two-UR host component](evidence/validator-activation-qualification-20260930.md) implements static installation, exact runtime config copies and durable separate process starts/recovery: 88 positive root executions pass normal/race, with twelve normal and five selected race causal controls. Its public fresh-start authority remains deliberately unavailable; no unit was deployed or started. Strict production enforcement remains open. The [October 2 best-effort owner workflow](OWNER-TRIM-BEST-EFFORT.md) has a [qualified retained-intent and physical-custody source increment](evidence/owner-trim-retained-intent-20261002.md) at `0f7c8698`, with independent signed pruning/re-entry residual options; actual risk acceptance, owner device/signature, external custody and activation are still absent. The [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md) includes this later source while the historical `3d1e2ecf` receipts remain unchanged. Live eligibility, chain effects, service activation and full release qualification remain pending, with no live mainnet authority, signing device or global custody fence supplied. [Review schema v2](PLAN.md) keeps preconditions distinct from produced facts. | Complete live identity/census and custody effects; use the ranked owner trim, exact recheck/reconciliation and bounded protected-identity conditions, retaining explicit old-miner dispositions. Finish production safe-trim authority/execution and the remaining durable chain phases: complete contract installation including the evidence journal/anchor, two UR validators plus netuid-0 role, Safe authority and bounded funding. Qualify production current authority, custody/device and owned route, wire service activation, and observe actual 10/90 native outcomes after approved activation. |
| MG-09 / P1 — Sustained resource and storage capacity | Service/storage owner; PH-08/09/20/23/26 | **In progress; gate open:** [Native/snapshot/miner/monitor source scopes now pass independently; inventory-v3 has a separate author/independent scope](evidence/durable-owner-custody-qualification-20261002.md). [Inventory CLI `7fb6b6a1`](evidence/durable-inventory-cli-qualification-20261002.md) passes nine author/independent normal/race roots and vet. The [public observer and successor execution defects](evidence/durable-observer-continuation-progress-20261002.md) have a separately qualified exclusive-writer fix and a pending same-owner soft-outage successor; coherent integration remains open; module-only adoption of constructor `71df099c` is a separate pending consumer scope. [The separate validator/root/bootstrap/blob scope](evidence/durable-adopter-qualification-20261002.md) passes 111 normal/race roots and four-package vet in author and independent runs, with separate author real-credential preflight; immutable-member census and broader composition remain open. Production preparation, capacity policy and restore are open. Bounded simulator mechanisms exist; production sizing and restoration receipts are missing. A [qualification-host media error](evidence/qualification-host-media-error-20261001.md) proved that free space and process success do not prove readable storage; it is not a Snow/mainnet-host observation. [Prior verified relocations](evidence/release-1320845d-server0aa1-20261002.md#remaining-release-and-capacity-gates) preserved an unreadable old source tree and the active shared cache. The [MG-06 release](evidence/release-258e25b4-server0aa1-20261002.md#remaining-release-and-capacity-gates) retains the separate inactive-cache cleanup log, uses data-volume scratch and admits each serial image repeat only above 118 GiB to preserve 110 GiB with margin. None of this closes production capacity or restore gates. | Measure backlog, bytes, memory, RPC work and queue fairness with the proposed fleet and retention. Bind finite capacity with reviewed margin; monitor device media errors and backup integrity; prove missing/full-volume behavior, copy verification, backup restore, multi-gigabyte log drainage and foreground deadline headroom. Required before unattended operation. |
| MG-10 / P0 — Qualification, rollout and actual acceptance | Release/operations owner; PH-16 and every affected gate | **Planned:** no accepted composed mainnet release. | Before activation, complete production-path causal regressions, affected normal/race suites and a controlled upgrade/outage/restart/repair rehearsal; retain failed and reused scopes. After bounded activation, observe at least three complete native emission intervals and one full 50,400-block UR settlement/claim cycle before declaring program acceptance. Record pending live evidence as pending. |

MG-01 has a [later read-only public snapshot](evidence/public-route-check-20261001-1356.md)
at block 9,188,367 with the same genesis, chain ID and runtime-470 artifact
hashes. It is still one endpoint's unapproved assertion; independent identity,
source/Wasm and finality approval remain open.

The [later two-endpoint pinned readback](evidence/public-cross-endpoint-20261001.md)
at block 9,189,666 returns byte-identical normalized genesis, native header,
runtime version, metadata and `:code` hashes from Rao archive and the public
entrypoint. This is corroboration across RPC routes, not independent GRANDPA,
source/Wasm, custody or operator approval; MG-01 remains open.

The [21:43 UTC dual-route recheck](evidence/public-dual-route-recheck-20261001.md)
again observes the same mainnet genesis, EVM ID 964, runtime 470 and `:code`
hash at finalized block 9,190,703 on both public routes. Rao's v471 release is
still proposed; neither public route supplies independent finality or approval.
MG-01 remains open for activation.

The [23:05 UTC pinned readback](evidence/public-dual-route-recheck-20261001-2305.md)
again matches both routes at finalized block 9,191,112 for genesis, EVM ID 964,
runtime 470 and the same `:code` storage hash. Its separate current-head samples
differed, so the comparison is pinned to one block. Independent finality,
runtime/source approval and live action authority remain open.

MG-06 has a [pinned read-only recycle-mode observation](evidence/recycle-mode-observation-20261001.md): `RecycleOrBurn[25]` was absent at Rao archive finalized block 9,186,298, which the reviewed metadata interprets as the default `Burn`. The [later public readback](evidence/public-route-check-20261001-1124.md) also finds the key absent at block 9,187,604. Neither observation is independent state approval. The owner transition and finalized `Recycle` readback remain required before the 90% recycle policy can operate.

The [offline owner recycle transition](OWNER-RECYCLE-TRANSITION.md) resolves the
MG-06 authority question at pinned runtime 470: AdminUtils call 80 accepts the
subnet owner or Root, with per-subnet hyperparameter 24 rate limiting for the
owner and the admin window for either origin. Separate approval, exact native
Sr25519/Ed25519 request bytes, bounded inert Ledger framing, durable public
signature custody and canonical receipt/inclusion-block readback are implemented.
Merged source `233ea2be` has [sealed offline qualification](OWNER-RECYCLE-TRANSITION.md#qualification-scope)
at `b3880266`: 29 author and 24 independent Sol roots pass normal/race with zero
skips, and both mainnet vet runs pass. This qualifies the selected offline paths.
No signing, broadcast or service command is installed. Independent runtime/device
and custody qualification, an actual approved transition and finalized Recycle
state, followed by native 10/90 outcome evidence, remain required. This source
needs a successor release; the SN `6c801a25` / server `720e7c61` baseline retains its scope.

The [October 2 recycle custody correction](evidence/owner-recycle-custody-20261002.md)
at source `25aa1515` closes detached-marker signing handoffs, recreation of
deleted completed journals during mode reconciliation, and valid predecessor
rollback during active ownership. It preserves original approval/nonce/signature
bytes and interrupted unused-claim recovery. Source qualification is recorded
separately: author and independent runs each pass 23 roots normally and with the
race detector, plus vet, and reproduce seven causal baseline failures. The
exact `3d1e2ecf` release and all owner-trim receipts retain their
original scope. The full retained v470 native emission metadata profile passes
unchanged. This custody fix supplies neither a recycle device/submitter nor the
unresolved native denominator, quantization or actual 10/90 outcome; MG-06 and
successor release qualification remain open.

**Decoder resource admission implemented:** the shared
[`DecodeRuntimeMetadata` boundary](evidence/runtime-metadata-bounds-20261001.md)
at source `b9ee4c91` bounds encoded input before allocating raw bytes, collection
counts before backing allocation, and aggregate storage, recursive work and
depth across copied SCALE decoders. The v14 lookup-map copy uses the same budget.
Self-hashed, sealed discovery snapshots and direct RPC metadata receive the same
guard; a self-consistent hash still grants no runtime authority. Truncated
compact/option input returns an error without panic or fabricated `None`.
Independently pinned owner/root paths retain hash-first admission, and valid
runtime470 plus the separate pinned SDK-v15 owner path remain compatible.
The historical SN `689938d6` release predates this owned-fork change. The
[SN `28ebfced` baseline](evidence/release-28ebfced-serverac86-20261001.md)
now packages it; full release qualification and approval remain open.

**Shared native HTTP response admission implemented:**
[source `58852c47`](evidence/http-rpc-response-bounds-20261001.md) closes the owned
GSRPC transport bypass for unmarked submissions, unknown methods, direct clients
and batches. Configured CRV4 metadata reads were already marked and physically
bounded; mainnet discovery's separate RPC client also already used finite
per-call reads. The shared transport now admits at most 32 MiB + 64 KiB + 2
decompressed JSON body bytes, preserving one 16 MiB native events field after
hex expansion. The same finite aggregate cap applies to batches; owners must
split larger read batches before issuing them. Status bodies are closed without
reading, and complete body/framing, close, cancellation and response-ID checks
precede publication. No write retry is introduced. The adjacent server
subscription response/notification decimal-ID mismatch is corrected with a
local real-codec handshake and legacy numeric unsubscribe compatibility.

**Direct miner and operator-CLI EVM HTTP admission implemented:**
[source `0dea3f26`](evidence/evm-http-response-admission-20261001.md) routes all
seven direct miner/fleet/claim/submit and `stctl` dial sites through the shared
`evmrpc` owner. Each response and aggregate batch has a 32 MiB + 64 KiB + 2 byte
ceiling on both wire and expanded gzip bytes. Complete framing, unique response
IDs and physical close precede result publication. Non-success HTTP bodies are
never read or retained, so status text cannot masquerade as an acknowledged
transaction. Typed status retry rules remain with the existing read owner;
signed POSTs cannot follow redirects, and this transport grants no replay.
Original signed claim bytes survive refusal and remain eligible only for their
existing authenticated reconciliation/replay path.

Validator's 4 MiB EVM transport and mainnet's finite custom RPC profiles already
had separate bounds. The server's operator receipt collector separately limits
each response to 16 MiB, a collection to 128 MiB and 32,768 requests. This change
does not modify or requalify those owners. WebSocket and IPC retain their existing
transport semantics. Aggregate process/concurrency memory and host capacity
remain open; no universal HTTP-client or whole-process bound is claimed. The
decoder's budgets remain requested-storage/work limits, not a custom-code
sandbox. The [bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md)
packages these changes at exact SN `28ebfced` / server `ac86855d`; the live
read-only compatibility probe remains unapproved observation and full release
qualification remains open.

MG-01/MG-06 have a later [read-only public-entrypoint fallback](evidence/public-entrypoint-fallback-20261001.md): the archive route timed out, while the official mainnet entrypoint returned matching genesis/EVM/runtime identity at finalized block 9,187,206 and another absent `RecycleOrBurn[25]` value. The fallback is unapproved observation only; it neither retargets signed work nor provides archive history. Preserve the failed archive transcript and obtain separately approved route authority before any endpoint switch for execution.

MG-02/MG-10 have a [broad normal receipt for the exact Safe-integrated release code](evidence/release-source-broad-normal-20261001.md): all 16 disjoint `./mainnet` partitions passed, with 1,019 test roots passing and six intentional skips. The tested SN commit differs from the frozen release source only in Markdown; its integrated server pin is exact. Focused Safe normal/race and builder normal/race qualifications remain separate. This local source result does not approve the release or prove live rollout behavior.

MG-09 has a concrete [build-host storage observation](evidence/build-host-scratch-migration-20260930.md): the root volume reached 100% with 4.1 GiB available during release preparation. Two inactive, verified scratch trees were moved to `/mnt/data` while their original paths remained readable through symlinks, leaving about 54 GiB free on `/`. Continue placing qualification scratch and caches on `/mnt/data`; this cleanup does not close production capacity, retention, restore, or full-volume tests.

MG-03 also includes a [qualified miner claim-queue owner fix](evidence/miner-claim-queue-owner-qualification-20260929.md).

It locks the physical queue directory across read, publication and joined
shutdown so duplicate daemons or a replaced pathname cannot split signed
outcome custody. The separately [qualified retained-byte successor](evidence/miner-claim-queue-capacity-qualification-20260929.md)
caps each queue at 16 MiB and preserves oversized retained bytes on refusal.
Both sources are integrated; 76 affected roots pass normal/race and all 292
miner roots pass plain normal. The composed release still needs custody,
restart and aggregate fleet-capacity qualification before deployment.

MG-01's [read-only Snow route checks through October 1 01:18 UTC](evidence/snow-route-observation-20260930-1914.md)
returned HTTP 502 for both native genesis and EVM chain ID in the earlier
and latest probes, and for native genesis alone in the intervening probes.
They provide no new mainnet identity or sync evidence; the route remains a
live launch gate.

MG-08 now also has an [independently qualified native prerequisite reader](evidence/validator-native-admission-qualification-20260930.md)
for both UR validators. It checks the original signed runtime at the current
and activation-checkpoint hashes, native epoch/drain facts, owner and generation,
activity, explicit Recycle, canonical anchors and sample age. The public
fresh-start authority is still nil: operator proof/client-key readiness,
contracts, global signer custody and effective-majority stake remain open.
The original schema-3 bootstrap config cannot silently absorb a later runtime
upgrade; a separately approved continuity and activation rollover is needed.

The [qualified `admit-committed` increment](evidence/validator-committed-prefix-qualification-20260930.md)
reads each standard validator's service-owned committed control history against
both original operator origins and retains completed checkpoints. Its inherited
startup fixture failures were repaired in tests without changing production
bytes; the original failed receipt remains preserved. This closes only the
committed-control-prefix subgate. Unsealed ledger/intent state, live worker
attestation, signer custody, applied weights influence and launch authority
remain open; neither validator is authorized to start publicly.

The [independently qualified unsealed inventory](evidence/validator-unsealed-inventory-qualification-20261001.md)
extends that read-only observation to the actual signed ledger tails,
unfinished trails, import receipts and protected empty intent boundary.
Selected normal/race and privileged UID tests pass, and seven causal guards
refuse their intended mutations. The subsequent
[qualified bounded tail-boundary implementation](evidence/validator-tail-boundary-qualification-20261001.md)
derives every distinct boundary from signed tail replay and authenticates its
canonical finalized hash, epoch, policy window and operator eligibility. Its
new optional checkpoint scope preserves the earlier inventory's original
authority and retains explicit refusals for nonempty intent graphs. Independent
qualification of this extension passed; historical provider bindings, live
workers and the remaining launch gates stay open. Neither validator is
authorized to start.

The [qualified validator current-evidence increment](evidence/validator-current-evidence-qualification-20260930.md)
authenticates original dual-signed operator activation and fresh nonce-bound
client-key responses, and checks the approved five-contract graph at one
native-header-bound EVM point. Its first frozen build exposed an EVM RPC
allowlist mismatch; the corrected build uses the bounded EVM read profile and
passes the formerly failing contract fixtures. The failed and corrected receipts
are both retained. This is partial admission only: full proof-prefix/worker
health, deployment/source provenance, global signer custody and effective
majority remain open; public fresh start remains nil.

The [qualified stake-capacity admission](evidence/validator-stake-capacity-qualification-20260930.md)
adds an original-plan `admit-stake` observation for both UR validators.
Its complete census conservatively bounds weighted-stake floors through the
pinned runtime's threshold, owner exception, quantization and factor×tempo
activity rule. Independent frozen and merged suites pass, including a causal
pre-mask denominator control. A capacity lower bound is not applied-weight
influence or live effective majority; public starts and that outcome remain
open with producer health and signer custody.

The [qualified validator proof-health increment](evidence/validator-proof-health-qualification-20260930.md)
replays both roles' original pinned operator activation prefixes and retains
completed proof checkpoints if a later read fails. It reports standard process
progress separately from proof integrity: missing/stale progress is a warning,
while wrong ownership, source/generation or contradictory proof bytes refuse.
The service-UID ownership guard remains intact. Current mutable-prefix and
per-operator live-worker completeness, global signer custody, applied influence
and signed launch authority still block public start.

The [MG03/R48 server source composition](evidence/operator-mg03-r48-composition-20260929.md)
is integrated at server root `05fee56f`. Its parent full merge preserves both
original histories and matches the qualified MG03 tree byte-for-byte; the
successor adds only two deterministic composition tests. Sol qualified all
134 affected base roots and, separately, the two new roots plus 52 adjacent
model roots on the successor, each normally and under race. The six conflict
resolutions, complete controller lineage and original approvals remain intact.
The composed release build, actual dependency/artifact lock and live rollout
remain pending; the qualification's pinned dependency graph remains explicit.

MG-08 also exposed a production bootstrap scaling failure: action-seven
validation repeatedly expanded a shared predecessor graph, making one call
copy 128 projections and hash the same 106 KB approval configuration 254 times.
The [integrated graph correction](evidence/bootstrap-contract-plan-graph-qualification-20260929.md)
keeps one private copy and one validation per distinct object within each
invocation while retaining the exact approval, journal and output encoding.
Its scoped qualification passed 5 graph, 28 evidence and 177 adjacent roots in
both normal and race modes, with six causal controls. The independent full
`./mainnet` normal package reached its 60-minute timer after 299 passing roots
and zero assertions, so that broader check remains incomplete. Live deployment
is a separate gate; do not treat a package timer or a cached success from a
prior invocation as approval.

MG-10 testing lesson (2026-09-29): the SDK-pinned 618-root unsharded race
process reached its one-hour package timeout while
`TestEvmEscrowRegisterClaimRecoveryKeepsFourLocks` had been active for about
25 seconds; no top-level root failure was observed. The subsequent
[audit](evidence/incremental-source-composition-20260929.md#completed-and-pending-qualification)
records 618/618 top-level root-body passes, but only 410/618 have a pass inside
a stream with package PASS; 208 still require terminal package coverage.
Preserve the original unsharded and six initial shard timeouts; wave2/08 had
no package terminal at that snapshot. Use disjoint exact root shards, retain
each shard's source fence, package exit and original timeout, and preserve
completed results without resetting their evidence. Broad race package
qualification remains open.

The [corrected SDK coverage and scheduling review](evidence/bootstrap-contract-successor-qualification-20260929.md#separate-sdk-package-coverage-and-scheduling-lesson)
now records 543/618 roots backed by package-PASS race streams, 75 pending and no
root failures. The evidence predecessor checkpoint root executes fourteen serial
fixtures and passed in about 28 minutes in both its shard and exact-root retry.
Isolate that long root into its own package/time budget and partition other roots
by observed duration; retain all causal cases and the original timeout records.
This is a qualification scheduling lesson, not a demonstrated product failure.

The MG-08 [offline contract prerequisite increment](BOOTSTRAP-CHAIN.md#offline-contract-installation-prerequisites)
adds `bootstrap-chain contract-plan` before custody and `contract-readiness`
over the original v3 preparation and retained action journals. It exposes missing
anchor approval, the original eight-attempt/nine-action mismatch, and unresolved
Safe-inner authority, code/runtime provenance, relayer funding and custody.
Completed canonical-action receipts remain historical retained facts; eight
completed actions leave only the anchor unfinished and do not require replay.
A cap change needs a new independently signed successor that adopts the original
prefix, reconciles unfinished signed nonces, and conserves cumulative attempts
and lifetime financial exposure. The later conditional execution custody owner
is described below; its concrete canonical adapter has scoped independent
qualification.
The [focused qualification](evidence/bootstrap-contract-prerequisites-focused-qualification-20260929.md)
passes all twelve new roots normal/race with six causal controls in both modes.
The separate [partial adjacent battery](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md#separate-composed-smoke-and-partial-adjacent-coverage)
passes 150/270 roots in both modes, with ten package PASS/exit-zero terminals
and no root failures or skips; 120 roots remain unrun. This does not close MG-08 or
establish installation, role activation or native 10/90 acceptance.

The separate [unsigned contract successor proposal](BOOTSTRAP-CONTRACT-SUCCESSOR.md)
has [scoped Sol qualification](evidence/bootstrap-contract-successor-qualification-20260929.md):
six new and twelve inherited roots pass normal/race, with six causal controls
in both modes. It can retain eight
completed receipt seals, carry original spend forward, propose additive attempt
and lifetime ceilings, and preserve an original ninth reservation. It does not
sign or execute a successor. The later durable adoption owner has its own domain;
current Safe authority and canonical receipt proof remain required before live
execution. Changed original v1 approvals cannot adopt old custody.
The qualified [signed local preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md) adds
one fixed original-root claim, a distinct independently verified preparation
approval and resumable publication. Its [scoped receipt](evidence/bootstrap-successor-preparation-qualification-20260929.md)
passes thirteen focused and three adjacent roots normal/race, with thirty-two
intended causal executions. It preserves the eight retained receipts and
cumulative proposed floors; no executable allowance, Safe authority or signing
path is created. Copy/restore/move onto a different physical root requires a
separately approved migration. The original full-v3 command remains an explicit
adjacent root, and an eighth-action control checks its extracted fixture helper.
The [separate six-root composed smoke](evidence/bootstrap-successor-preparation-qualification-20260929.md#separate-composed-smoke)
passes normal/race on exact merge `93a0a060`. It composes both full-v3 commands,
same-root resume, pure Safe calculations and MG-07 incident recovery without
expanding the separately pending broader package coverage or live authority.
The [separate full-v3 public-command fixture](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md)
closes that specific gap on `c294fefd`: its positive root passes normal/race,
and all six causal executions reach the intended assertion. It approves the
successful full-metadata fixture's finite 60-second send budget before custody.
The original d7 control's one-second local POST timeout remains a preserved
noncausal attempt; production deadlines and targeted lost-reply fixtures are
unchanged. Give successful full-graph fixtures enough bounded local execution
time while retaining every causal case and the separate long-root scheduling
lesson above. The fifteen-root MG-07/prerequisite smoke on `79ff2c6e` and the
twelve-root successor smoke on `1e2b2abb` pass both modes on their own exact
graphs; neither expands the partial adjacent battery or SDK package coverage.

The [offline Safe release verifier](SAFE-RELEASE-VERIFY.md) now has [scoped Sol qualification](evidence/safe-release-profile-qualification-20260929.md):
six new and four adjacent roots pass normal/race, and eight causal controls each
reach their intended assertion in both modes. Explicit version/variant profiles
bind unchanged published archives, proxy/singleton code, ABI, compiler inputs,
source provenance and storage layout. Independent compiler rebuild, live Safe
binding/authority and signing/execution remain unresolved. Existing versus new
Safe remains a user decision; a new address must match retained initializerOwner
or have separately authorized ownership migration. This artifact-only increment
does not grant signed successor adoption, nonce custody or evidence-anchor execution.

The [pure Safe evidence increment](SAFE-EXECUTION-EVIDENCE.md) has
[scoped independent qualification](evidence/safe-execution-evidence-qualification-20260929.md)
on `c648495f`: thirteen focused and four adjacent roots pass normal/race,
with all twenty-six intended causal executions. Its [separate four-root composed smoke](evidence/safe-execution-evidence-qualification-20260929.md#separate-composed-smoke)
also passes both modes on that exact source; it predates the signed local
preparation merge. It calculates exact digests,
inspects supplied signature forms and classifies declared inner outcomes and
nonce rollback. Current owner membership/threshold, contract callbacks, approved
hash storage, canonical receipts, Safe authority, signing, custody and execution
remain unresolved. E074's twelve setup EOF failures are retained separately;
the corrected fixture filters catalog variants before decoding and tests absent,
empty and malformed unselected members. The earlier artifact verifier's separate
nine-root composed smoke passes both modes on `18a88db4`. These results do not
close the 75 SDK package gaps or 120 unrun MG-08 adjacent roots.

The [offline successor Safe review](BOOTSTRAP-SUCCESSOR-SAFE-REVIEW.md) has
[scoped independent qualification](evidence/bootstrap-successor-safe-review-qualification-20260929.md)
on `d0207448`. It connects the original v3 graph, completed independent preparation and
full selected Safe release to the exact zero-value evidence-anchor CALL digest.
Its new read-only preparation consumer rejects absent, partial, forged, staged,
unsafe or actively published claims without repairing their bytes. The proposed
relayer liability includes both completed maximum envelopes and any original
ninth reservation; original attempts and additive ceilings remain unchanged.
Test compilation and vet pass. Nine new and four adjacent roots pass independent
normal/race runs, including the full-v3 public command and published-bytecode
digest oracle. All fourteen causal executions reach their intended assertion;
the sealed logs, patches, exact source and dependency graph were independently
checked. This does not close MG-08 or broaden prior package coverage.
Safe owner signatures and complete outer calldata remain absent, as do
execution approval, nonce/budget allocation and live authority. The review's native
window is outside the Safe digest and cannot expire a signature. Canonical
eight-receipt adoption, Safe/evidence state, signature lifetime/window enforcement,
globally fenced relayer signer custody and qualified canonical execution remain
required before evidence anchoring or activation. The concrete adapter described
below does not supply the external live authority by itself.

The [successor execution custody and owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) now
binds independently approved signatures and an exact outer envelope to the
original eight-receipt adoption, cumulative attempt/financial floors and distinct
Safe-inner/relayer-outer nonce claims. Its qualified initial public commands are
offline; the one-send machine requires an authenticated adapter for every
historical, current-state and canonical receipt decision. Its
[scoped qualification receipt](evidence/bootstrap-successor-execution-qualification-20260929.md)
records twenty-one focused and six adjacent roots passing normal/race on
corrected `75ea2158`, including interrupted publication, counted attempts, ambiguous send recovery,
nonce conflicts, exact inner success and the full public-v3 original graph.
Ten isolated causal controls each reach their intended assertion in normal/race,
and the forty-five-file evidence manifest verifies. This does not close MG-08 or
broaden earlier package coverage.

The [live execution-custody follow-up](evidence/bootstrap-successor-live-custody-20261001.md)
closes an in-process gap: an owner could send after losing a counted intent, or
report installation after losing its terminal record, while using its cached
event. Read-only checkpoints now authenticate the complete intent/record prefix,
claim/ready markers and interrupted outcome bytes before send/result admission.
All 55 affected roots pass normal/race with package PASS, all three checkpoint
bypass controls fail at the intended assertions in both modes, and vet passes.
The original eight-action custody, exact signatures, cumulative attempts and
financial reservations stay unchanged. Public submission and current-only policy
approval remain closed gates; this correction supplies no live authority.

The initial twenty fixture failures remain preserved: private binary files had
shared temporary parent directories. The test-only correction explicitly uses
`0700` parents; production readers remain strict. The first corrected race run
hit the default ten-minute package timer after fifteen passing roots and zero
failed root assertions. The unchanged twenty-one-root retry passes in 662.949s
with an explicit twenty-minute harness budget. The public-v3 and thirty-boundary
recovery roots together account for about 398 seconds under race. Size the whole
qualification from measured fixture cost; preserve timed-out attempts and keep
production transaction deadlines unchanged.

The concrete canonical adapter has
[scoped independent qualification](evidence/bootstrap-successor-canonical-qualification-20260930.md)
on corrected frozen source `a7186754`: ten focused and twenty-two adjacent roots
pass normal/race, fourteen normal causal controls and six selected race controls
reach their intended assertions. The thirty-four-file Sol manifest and separate
forty-eight-file author handoff verify with matching source/module fences.
Preliminary `82da3d40`
passed all eight roots normal/race before discovery of the Safe storage-provenance
gap; preserve those results without treating them as corrected-source evidence.
A separate signed canonical authorization
binds the exact execution plan, pinned Safe build review, reviewed current-runtime
profile and its evidence, every Safe/relayer signer's cutover plus retained
original reservations, and a separately signed exact Safe deployment/storage
history statement. The adapter borrows all eight original marker locks and
reauthenticates the original signed bytes, receipts and postconditions through the
existing historical native/EVM adapter. Each original receipt receives its own
approved retry budget. Successor admission and inclusion use the separately
approved current runtime; an ordinary later runtime upgrade cannot erase a
historical original or counted successor receipt.

Online `contract-successor-execution-resume` requires the separately pinned
canonical approval. Public `--submit` is unavailable until a distinct canonical
Safe history authenticator is implemented; it exits before custody loading or
attempt reservation even when all independent review files are signed.
The [bounded archive census](SAFE-HISTORY-CAPTURE.md) supplies a real read-only
command with private create-only witness retention, complete native-body/EVM
transaction/receipt commitment checks and later-head continuity. Its
[paired-source qualification](evidence/safe-history-census-qualification-20260930.md)
is sealed: 68 positive root executions pass normal/race, ten normal and five
selected race controls are causal, including a distinct normal fixture-oracle
control. It accepts unrelated traffic but does not prove internal/reverted
actions, native hook effects, clean initialization
or complete Safe history. Public submission and MG-08 remain open gates; the
unchanged signed history policy cannot be discharged by these raw archives alone.
The [qualified native trace increment](evidence/safe-history-native-trace-qualification-20260930.md)
adds bounded block traces, parent runtime code proofs, retry/cancellation and
canonical closing checks without changing original custody. The SDK's filtered
keyless events and missing rollback/inner EVM boundaries keep complete Safe
history unproven and public successor submission closed. A qualified node
extension or independent full replay is still needed before that action.
The adapter checks actual pinned Safe proxy/singleton code and scoped
finalized/pending authority, both nonce domains, funding, current contracts and
the exact one-shot evidence binding. Finalized reads keep one canonical hash
while later heads advance; later native-window and runtime checks do not require
head equality or restarting the snapshot. Current state RPCs retain their own
bounded retries. Exact-hash lookup uses `eth_getTransactionByHash`; the adapter
requires neither `txpool_content` nor `author_pendingExtrinsics`.

Eight light roots cover independent authority, immutable authority recovery,
published Safe state, scoped pending lookup, strict receipt fields, signed
provenance scope and real orphan owner/module mappings. Two
separate heavy roots run the full original v3 graph and actual pinned Safe
execution under an explicitly injected synthetic history capability, including
lost reply/restart, approved runtime change, renewed read
deadlines, advancing canonical heads and refusal of a changed canonical hash.
Author compile-only, vet and formatting checks pass. Sol's independent heavy
normal packages take 51.406s and 45.496s; their separate race packages take
349.951s and 297.814s. The adjacent race package passes in 540.635s. Explicit
twenty-minute normal and thirty-minute heavy/adjacent race harness budgets leave
production transaction and individual read deadlines unchanged.

Enforced signer cutover to one registry, independently approved mainnet
genesis/runtime and Safe authority, funding, actual owner/relayer signatures and
live readback remain separate gates. The owned RPC's finality and account-pending
responses are assertions. Independent build and cutover evidence explicitly
attests external assumptions; local locks cannot establish cross-host signer
exclusivity or the absence of off-node signatures. MG-08 remains open, without
installation, activation or native 10/90 acceptance.

**P0 gate — Canonical Safe deployment and complete storage provenance.** The
independent provenance statement binds the exact plan, Safe/profile, published
proxy/singleton runtimes, deployment transaction, reviewed native snapshot and
separately pinned history evidence. Its signature is necessary review input and
does not implement the distinct `bootstrapSuccessorSafeProvenanceAuthenticator`.
Safe sentinel-list getters cannot prove the absence of enabled owner/module
mapping entries outside those lists. The real-code malicious-storage fixture
demonstrates both kinds of orphan authority while ordinary getters remain clean.
Before enabling the original complete-history submission route, implement and independently qualify canonical
deployment/initialization and every authority-relevant storage/delegatecall
mutation through finalized and scoped pending state, bound to the exact approved
route/account/profile and signed evidence. Reports or flags must never inject
this capability. The separate
[qualified readmission increment](evidence/bootstrap-successor-readmission-qualification-20260930.md)
`cd4261a8` puts expensive proof before the final scoped pending Safe/relayer nonce
and Safe-state admission, and clears earlier admission even when a refresh's
checkpoint fails. Deterministic fixture barriers change actual Safe/relayer
nonces and a later runtime during proof; a canceled refresh also cannot reuse an
earlier counted-send admission. Both heavy roots and three selected adjacent
roots pass normal/race; both causal controls reproduce their intended failure
in both modes, with source/dependency evidence sealed. The production history
capability remains absent. Preserve read-only
historical reconciliation and test missing,
swapped, incomplete and malicious history refusals. This is an open MG-08
implementation gate, separate from independent build review and signer cutover.

Complete current authority and historical provenance are distinct properties.
The separate `aa9f715b` [current-authority proposal](SAFE-CURRENT-AUTHORITY-PROPOSAL.md)
implements a complete native account-storage-prefix verifier, exact runtime and
published proxy/singleton code/metadata binding, and strict Safe storage layout.
It rejects omitted intersecting branches, genuine orphan owner/module mappings
and all extra words. It preserves one canonical finalized hash as later heads
advance. The [qualification note](evidence/safe-current-storage-qualification-20260930.md)
records eleven new and ten adjacent roots passing normal/race, all ten normal
and exactly five selected race controls causal, with the complete source/module
and local-dependency evidence sealed. This supplies read-only observations only.
A current snapshot does not prove clean past initialization/delegatecalls or a
complete pending overlay. The reviewed native RPC exposes neither a pending
proof root nor an atomic multi-read token; final known-word/code rechecks retain
that explicit limitation and cannot authorize a send.

**Current-policy custody — scoped qualification complete.** The separate `3f88a948`
[custody increment](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CUSTODY.md) implements an
immutable independently signed acceptance journal under the existing exclusive
owner, exact counted/outcome references and partial-publication recovery. Both
proposal and acceptance signatures are required. Runtime-prefix order and
interrupted terminal events constrain authority import; incomplete policy stages
block reservations and cross-imports. Original history statements, runtime
predecessors, receipts, signed bytes, nonces, attempts and liabilities remain
retained. Its [qualification note](evidence/safe-current-custody-qualification-20260930.md)
records eight new and thirty-one adjacent roots passing normal/race, all eight
normal and exactly five selected race controls causal, and the sealed exact
source/module/local-dependency evidence. That isolated custody increment did not
enable public policy import or submission; read-only import is supplied by the
separately qualified capability below.

**Current-policy approval and public release route.** The concrete native proof
path is independently qualified below. The October 1 v2 interface adds a separate
public route while actual production risk-policy acceptance remains a P0 gate.
The route requires
the complete independently signed proposal and acceptance, retain expensive proof
before final scoped pending re-admission, preserve the exact proof snapshot
separately from a later admission head, and keep the one retained exact-byte send.
The current-only policy must not reinterpret the existing signed complete-history
attestation or claim current proof establishes historical truth.

**Native current-policy capability — scoped qualification complete.** Frozen
`95a905d4` [implements the distinct native route](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md)
on the signed custody journal. That September 30 source kept public acceptance-file
import read-only and public submission closed. Its internal route requires both
independent signatures and the exact
completed runtime tip, refuses mixing with the history capability, proves the
complete finalized Safe prefix, then rechecks scoped pending Safe/relayer state,
nonce, funding and exact transaction identity after expensive proof/artifact work.
It preserves the original proof snapshot separately from the final admission
head and repeats complete observation after durable reservation. Its
[qualification note](evidence/safe-current-capability-qualification-20260930.md)
records two new and thirty-five adjacent roots passing normal/race, eight normal
and exactly five selected race controls causal, and sealed source/module/local
dependency evidence. Two original normal oracle mismatches remain unresolved;
fresh reproductions under the corrected assertions are sealed separately without
changing source, tests or mutation patches. The capability does not prove
historical initialization/delegatecalls, independent finality or complete pending
storage.
Explicit production acceptance of those current-only assumptions remains an open
P0 gate. No mainnet action is authorized by local qualification.

**Public current-only acceptance v2, October 1.** The separate
[public route](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md#explicit-public-v2-acceptance)
requires the original independent reviewer's new v2 signature and an exact
`--accept-safe-current-policy` acceptance object hash on every submit invocation.
V1 and proposal signatures cannot grant that authority. The signed policy names
owned-RPC finality, non-atomic pending assertions, exclusive signer/relayer cutover,
absent complete history/pending proof and between-read changes. Exact original
custody, complete runtime authority, native prefix proof and final scoped
readmission remain mandatory. Missing/wrong acceptance, incomplete publication,
runtime drift and mixed history capabilities refuse. Timeout and restart retain
the same signed bytes, nonce claims, counted attempts and maximum liability;
historical outcomes keep their original policy even after later imports.
The [qualification receipt](evidence/safe-current-public-qualification-20261001.md)
seals frozen `f3141591`: all 68 author-selected roots pass normal/race, three causal
control pairs reproduce their intended failures, and vet passes. Independent
review passes all 14 focused roots normal/race and vet with no blocker on its
separately pinned server graph. The 79-file manifest verifies. This implementation supplies
no production acceptance, signer or live transaction; MG-08 remains blocked for
actual authority, installation and activation.

**Contract-role declaration admission — scoped qualification complete.** The separate
[offline contract-role plan](BOOTSTRAP-CONTRACT-ROLES.md) addresses a cross-component
gap: two signed UR configs can agree with each other while targeting a foreign
vault/coordinator or the implementation instead of the approved proxy. The new
admission reconstructs the eight approved projections, binds both configs to the
exact proxy/vault/initial policy identifier and retains the evidence domain.
It preserves original v1/v2/v3 custody and does not grant live readiness. Sol
qualified corrected source `f4470d0d`: eight focused and twelve adjacent roots
passed normal/race (40 positive executions), and all three causal controls
reached their assigned assertions in both modes. The
[receipt](evidence/bootstrap-contract-role-qualification-20260930.md) retains
the original `43dcd01f` fixture failure separately; production guards were
unchanged by the fixture correction.

**Original contract receipt admission — scoped qualification complete.** The
[read-only historical increment](BOOTSTRAP-CONTRACT-RECEIPTS.md) borrows exact
original preparation and eight completed action records, reauthenticates their
canonical native/EVM receipts and historical postconditions, and checks each
signed EVM scan floor against the earliest original inclusion. Initial snapshot
and final checked-through head remain separate; ordinary advancement is accepted
while changed original inclusions fail. Original custody, attempts and pending
phases remain unchanged. Sol qualified frozen `c6b31fdb`: five focused and sixteen
adjacent roots passed normal/race (42 positive executions), all five normal
controls and exactly three selected light race controls were causal. The sealed
[receipt](evidence/bootstrap-contract-receipts-qualification-20260930.md) retains
raw results and source/dependency fences. Current installation, complete indexing,
evidence anchor and activation remain explicitly unverified.

**Current bootstrap contract fields — scoped qualification complete.** The
[five-account current-state increment](BOOTSTRAP-CONTRACT-CURRENT.md) checks the
original proxy implementation/owner/policy, reserve/vault links and evidence
domain at one finalized mapping after original receipt admission. It lists exact
getters/slots and labels their results owned-RPC assertions. Later-head continuity
preserves the observation block. Sol qualified frozen `2b87b133`: three focused
and sixteen adjacent roots passed normal/race (38 positive executions), all six
normal controls and exactly two selected light race controls were causal. The
sealed [receipt](evidence/bootstrap-contract-current-qualification-20260930.md)
retains raw results and source/dependency fences. Original zero-activity/policy
requirements remain strict. Complete storage, absent hidden mappings, governance
history and the evidence anchor remain unverified; no installation, activation,
public-route or live-action gate closes.

**P0 — Canonical installation-to-service admission, bounded code qualified;
live gate open.** The [separate current-admission route](VALIDATOR-CURRENT-ADMISSION.md)
binds both UR services to original CREATE/link and anchor receipts, actual EVM
scan floors, current executable/domain views and one shared native/EVM snapshot.
It composes native eligibility, conservative majority stake capacity, both
operator/client-key/proof domains and complete empty-tail ledgers. A separate
signed exact-policy envelope and lifetime custodian attestation are mandatory;
declarations, process approvals and old observations cannot select public starts.
Per-unit permanent claims and post-sync readmission preserve one initial start
across alternate envelopes, partial-pair progress and lost acknowledgements.
Current-only finality/governance assumptions and external signer/host exclusion
remain explicit acceptance inputs. Applied weights/10/90, actual signed live
inputs, systemd rehearsal, and separate root-service admission remain open. No
approval, mainnet transaction or live service action is supplied by this work.
The [sealed receipt](evidence/validator-current-admission-qualification-20261001.md)
pins SN `b8dc332a` and server `0b8e758d`: author 47 normal/all 12 new race roots,
independent mainnet 24 normal/all 12 new race and producer 12 normal/race roots,
vet and two causal omission pairs passed. The deliberately interrupted author
adjacent race stream remains an incomplete diagnostic. A separate server
`720e7c61` bounded compatibility receipt does not approve schema 750 or
subscriber-v2 rollout.

**Additive canonical runtime authority — scoped qualification complete.** The
`3d526830` [runtime revision increment](BOOTSTRAP-SUCCESSOR-RUNTIME-REVISIONS.md)
implements independently signed artifact additions while retaining the immutable
base, predecessor chain, original receipts, exact signed transaction, both nonce
claims, counted attempts and full liabilities. Hash-bound partial stages cannot
switch approvals; interrupted counted reservations remain consumed, and pending
terminal intents must finish before importing new authority. New event references
are monotonic while omitted references preserve old v1 bytes and seals. Historical
reads pass only their independently approved inclusion/parent pair to CRv4, with
no ten-profile lifecycle cap. The full local fixture retains twelve revisions.
The [qualification note](evidence/bootstrap-successor-runtime-qualification-20260930.md)
records ten new and thirty-six adjacent roots passing normal/race, twelve normal
and exactly seven selected light race controls causal, and the final sealed
source/dependency evidence. The CRv4 top-level census correction preserves its
original passing raw streams and false-failure harness ledger. Earlier `a7186754`
and `cd4261a8` receipts remain immutable and scoped to their own source.

**RT-04 signed continuity policy — inspection boundary qualified.** The
[production-aligned proposal](RUNTIME-CONTINUITY-POLICY.md) adds an independently
signed envelope bound to the original schema-3 validator authority and a separate
semantic-verifier certificate interface. The read-only inspector checks exact
candidate artifacts, consumed metadata/APIs, hashed headers, finite windows and
closing canonical continuity while preserving original config and signing views.
Certificates bind source/build evidence, verifier executable/rules and all consumed
execution/economic domains. A signature authenticates the assertion; no genuine
semantic verifier or independently replayed equivalence proof is supplied. Fresh
production selection stays exact and closed to unapproved successors. Automatic
selection/retention, original pending-byte custody and broader role/miner/bootstrap
adoption still require their own implementation and qualification. This increment
does not close MG-04 or RT-04 and performs no live chain action. The
[scoped qualification record](evidence/runtime-continuity-policy-qualification-20260930.md)
records fifty positive root executions, seven normal and three selected race causal
controls on the corrected exact source. The original typed-reply fixture failure
is retained separately; passing inspection never installs production authority.

**RT-04 finite executable replay — boundary qualified.** A separate
[offline SDK executor and bounded process owner](RUNTIME-SEMANTIC-REPLAY.md)
bind exact original/candidate Wasm, signed rules, evidence and executable identity.
On-chain transition cases compare complete declared storage effects, including
insertion/deletion, alongside return bytes. Explicit host/storage budgets and
joined cancellation qualify only this finite execution boundary. Real approved
source/build/state inputs, all-domain semantic proof and durable production
selection remain P0; this increment does not complete automatic compatibility.
[Independent qualification](evidence/runtime-semantic-replay-qualification-20260930.md)
records 48 Go positive executions normal/race, 19 Rust tests normally and eight
normal/four selected Go race causal controls. Rust has no race qualification.

**P0 follow-up — Automatic compatible runtime admission (RT-04).** A new runtime
still needs independently reviewed code/metadata and a signed revision. This
incremental authority path is not automatic runtime compatibility. Specify and
independently qualify a separately approved compatible-change policy and verifier
before claiming unattended routine upgrades. Preserve the complete historical
authority chain and all original custody, with no node self-approval or
provisional-runtime fallback. Same-version changed artifacts and unsupported
codecs remain closed explicit gates. Genuine Safe history authentication and the
public-submit gate remain unchanged; MG-08 stays open.
Later successors, filesystem migration, fee replacement and independently proved
external sends require separate approved liability-preserving transitions.

MG-07 now includes the qualified standard-validator
[service-progress producer](SERVICE-PROGRESS.md): 47 selected roots pass normal
and race, with four causal controls and the integrated authority-projection
check. Actual custody/settlement owners supply its facts; an isolated exporter
keeps heartbeat, useful progress and confirmed publication separate. Native
and steering hooks are covered by the separate
[continuation qualification](evidence/production-continuation-candidate-20260928.md).
The [read-only service consumer](SERVICE-MONITOR.md) is also implemented and
[qualified](evidence/service-monitor-qualification-20260928.md): 48 selected
roots pass normal/race, five controls reproduce their required failures in both
modes, and service plus inherited alert-rule fixtures pass. Each validator
role retains its own source checks, evidence ages and output files while the
chain worker retries independently. Application/reveal and settlement deadline
inference, other domain coverage, delivered alerts and the repair controller remain
open; source qualification does not establish live monitoring.

The [October 2 offline deployment implementation](evidence/monitor-deployment-offline-20261002.md)
addresses the earlier absence audit at xops `b98f8769`, with disjoint explicit
monitor/observer inventory, exact release/config and separate activation approval,
private checkpoint continuity, a scoped textfile collector and an off-host
expected roster/evaluator. New production native deadline policies require the
approved tempo-drift profile and 60-second GET retries. Snow remains testnet and
is refused. Author qualification passes 32 new and 19 adjacent xops tests,
13 SN monitor roots in normal/race, vet and four alert suites, plus controller,
unit syntax and real credential-reader controls. Independent qualification
passes the same 32 new and 19 adjacent xops tests, 13 SN monitor roots
normal/race, vet and controller/unit/credential controls on clean exact source;
its separate receipt is hash-bound in the evidence above.
MG-07 remains open for actual approved host installation, selected collector
acceptance, remote ingestion, delivered alert/recovery receipts, independent
watchdog/on-call rehearsal and broader monitoring/repair coverage; no live unit
or notification was started by this increment.

The MG-07 [read incident continuity increment](READ-INCIDENTS.md) now carries
stable outage IDs, first/latest failures, successful-read recovery and recurrence
through per-role checkpoint restart. Legacy history remains explicitly unknown;
recovery does not attest to service health or authorize repair/spend. The
[qualified receipt](evidence/read-incident-continuity-qualification-20260929.md)
records all 82 affected roots and six causal controls passing their required
normal/race outcomes at integrated SN `1bb311fc`. Complete incident retention,
compatible rollout and actual alert delivery remain open.

The MG-07 [steering responsiveness increment](SERVICE-MONITOR.md#steering-responsiveness)
closes the fresh-publisher/blocked-loop detection gap with explicit per-role
margins, a distinct durable incident and critical alert. Detection requires a
previously observed responsive steering instance; fresh read/receipt/reveal waits
do not imply a hang or protocol success. Restart, source loss, publisher restart
and policy removal preserve unresolved incidents until a real later loop outcome.
The [qualification receipt](evidence/steering-liveness-qualification-20261001.md)
retains source tests and controls. Checkpoint v4 preserves legacy history and
independently signed v3 stopped-validator repair scope. This diagnostic performs
no service mutation or signing.

The separate MG-07 [active steering-hang repair](ACTIVE-VALIDATOR-REPAIR.md) now
has a reviewable independent incident/role/release/host/generation envelope,
one durable stop, bounded descendant join and one durable start. Permanent
generation claims reject a second signed envelope or journal path; shared unit
ownership includes stopped repair and activation. Current healthy/unknown
steering, changed policy/source and expired recovery headroom refuse a stop,
including after fsync. Unknown starts never retry. The
[source receipt](evidence/active-validator-repair-qualification-20261001.md)
records deterministic effect and crash boundaries. Production SLOs and envelopes,
exclusive host/signer custody, real-systemd qualification, anti-rollback policy,
deployment/alert delivery and root/operator recovery remain P0 gates. No live
service was installed, stopped or started.

The MG-03 [qualified bounded native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md)
is integrated at server `5ff7bf02`. Its retained request/byte reservations,
partial native headers/certificates and offline completed replay preserve the
original receipt collection through exact-boundary or bounded-descendant proof
capture. All 112 affected roots pass normal/race, including 23 new roots and
three private-database roots; seven causal controls discriminate in both modes.
Vet and source/module fences pass. This qualifies capture source only; archive
capability, independent checkpoint/genesis/runtime admission, native account
and fee proofs, production service adoption, composed release and live custody
remain open, and actual fees remain null.

The MG-03 [qualified bounded native StorageProof verifier](evidence/operator-native-storage-proof-qualification-20260929.md)
is integrated at server `6201504e`. It derives the original collection-boundary
state root by replaying receipt/finality proofs, then checks raw storage proofs
against it. All 16 focused and six adjacent roots pass normal/race with package
exit zero; all 21 causal controls discriminate in both modes. The pinned SDK
oracle supplies 18 exact vectors, and the two prior fixture/control anomalies
remain separate. Its API covers the collection boundary; the qualified historical
interface below adds selected receipt/parent reads. Runtime decoding, checkpoint
approval, proof capture, fee attribution, owner-window/global-
custody authority and service adoption remain open, with actual fees null.

The MG-03/PF-03 [qualified historical receipt/native fee-context source](evidence/operator-native-fee-context-qualification-20260929.md)
at server `41527380` passes all ten new and 122 affected roots normal/race,
including three disposable-database roots; all six causal controls discriminate
in both modes. It is integrated after storage as server `1bccc3cd`; a separate
composed smoke passes 30 of 138 available roots per normal/race mode. It preserves
all signed history while deriving exact receipt-native
child/parent contexts; missing or ambiguous coverage remains unresolved. Actual
fees remain null and source-profile mapping grants no payer/runtime authority.
The original native StorageProof interface covers the collection-boundary root;
the qualified historical interface below adds selected child/parent reads.
Runtime-qualified withdrawal/refund attribution remains a separate dependency.

The MG-03/PF-03 [qualified historical native StorageProof API](evidence/operator-historical-native-storage-qualification-20260929.md)
is integrated at server `80c0e1b7`. Fresh receipt/native context replay derives
the exact selected parent
execution or child post-state root. A unique checkpoint child can prove raw reads
while its parent stays unavailable and nested fee coverage stays incomplete;
that child cannot become an execution-parent substitute. All 20 new and 30
adjacent roots pass normal/race with package exit zero; all 21 causal controls
discriminate in both modes. This 50-root scope does not claim whole-package
coverage. Runtime decoding, payer/fee attribution,
owner-window/global-custody authority and live action remain absent; fees are null.

The optional [native submission deadline observer](NATIVE-DEADLINES.md) now
wires explicit per-role completion margins into the actual service worker.
It distinguishes schedule forecasts and unavailable reads from a completed
receipt-pending report after the original intent's native epoch. First/latest
missed-window evidence survives restart, renewal and late application reports.
It has no success or incident-clear authority. The integrated
[qualification](evidence/native-deadline-qualification-20260928.md) passes all
35 selected roots normally and with race detection, five causal controls in
both modes, and both offline alert suites. Production margins, deployed
collection, alert delivery and authoritative incident resolution remain pending.

The integrated [bounded diagnostic exporter](evidence/bounded-diagnostic-output-qualification-20260928.md)
now isolates validator startup/steering/runtime diagnostics and chain/service-monitor work from stalled log
destinations. Each role has finite queue capacity; one joined destination owner
reports completed writes, dropped records and unavailable output independently
of protocol progress. All 76 affected roots have passing normal/race coverage,
six causal controls reproduce the intended failures in both modes, and vet plus
three offline alert-rule suites pass. The receipt preserves the original
maximum-ID fixture failure and its isolated correction. Consumer-first rollout,
actual alert delivery and other operational domains remain separate work.

The [root-service/root-monitor follow-up](evidence/root-output-qualification-20260928.md)
is also integrated and component qualified. All 41 affected roots have passing
normal/race coverage; six causal families, mainnet vet and seven offline alert
rules pass. The receipt retains the refused-sink assertion correction, incomplete
panicking control capture and exact corrected reuse. Root-monitor's compact
event v2 needs a consumer-first rollout; finite preview v1 retains the complete
census. Optional metrics failure preserves independent observations, while
original custody errors remain hard. Live root activation, deployed collection,
alert delivery and the repair controller remain open.

**Trail-worker output correction (September 28; integrated and component qualified).**
The actual `TrailEngine.Run` loop and proof-signature warning now use the same
bounded exporter, with closed scalar facts and explicit operator/epoch identity.
All 17 affected roots have passing normal/race coverage, and five causal controls
reproduce their intended failures in both modes
([evidence](evidence/trail-diagnostic-output-qualification-20260928.md)). The
physical full-pipe test proves actual trail progress and cancellation before
stdout drains. The receipt retains the original poisoned-ledger cleanup failure
and exact fixture correction. Required custody failures still stop the affected
work. SDK/internal logging, registration composition and live
delivery remain separate work; component coverage is not universal output isolation.

**Miner output and shutdown (September 28; integrated and component qualified).**
The actual provide owner now separates required authentication/file callbacks
from blocked stdout, joins admitted HTTP status handlers, and retains panic
cleanup causes. All 18 affected miner roots and the actual unchanged Warp status
reader pass normal/race checks; four causal controls reproduce their intended
failures ([evidence](evidence/miner-diagnostic-output-qualification-20260928.md)).
Optional versioned status counters report output delivery, while `status: ok`
remains process liveness. Consumer rollout, readiness and alert delivery remain
open. The [diagnostic cause isolation correction](evidence/diagnostic-cause-isolation-qualification-20260928.md)
also passed all 20 affected roots and five causal controls in both modes and
is integrated. PH-15 records its scope; the completed output tests remain retained.

**Versioned registration grammar (September 28; component qualified).** Server
candidate `736d7b8f` rejects ambiguous request-field aliases and duplicate or
invalid values before allocation. Its focused test passes normally and with
race detection; the exact old parser reproduces the expected failure in both
modes ([evidence](evidence/registration-request-grammar-qualification-20260928.md)).
The [underlying transaction qualification](evidence/registration-server-model-qualification-20260928.md)
now has 29 passing affected roots normally and under race detection. Its full
model execution finished with 1,118 passes, seven disclosed configuration/data
skips and no test failures. The retained checker rejection for undeclared legacy
subtests is separate from the successful original package exit. The
[SDK/Connect transport checks](evidence/registration-request-transport-qualification-20260928.md)
and [actual SDK-to-production-API/database checks](evidence/registration-production-api-qualification-20260928.md)
now pass normally and under race detection, including their causal controls.
The [final combined SN consumer check](evidence/registration-diagnostics-composed-qualification-20260928.md)
now passes all seven selected roots normally and under race detection on one
sealed registration/diagnostics source graph; all 18 maintained stages and the
independent after-fences passed. Server-first migration/rollout remains a
separate gate; these source results do not establish a deployed release.

The [concurrent-allocation fixture correction](evidence/registration-allocation-attempts-qualification-20260928.md)
also passes normally and with race detection, with the original isolation
control reproducing its expected failure in both modes. The fixture counts
allocation attempts across real transaction rollback/retry; final identity
equality alone had hidden the extra work. The original ineffective control is
retained. This test-only correction preserves the completed full-model capture
and does not qualify the newer composed client release.

**MG-04/MG-08 direct production cadence (September 28; source integrated,
component checks pass).** The [bounded representability fix](evidence/mainnet-steady-cadence-candidate-20260928.md)
permits zero accelerated epochs only for mainnet with four identical
initial/production windows and a 50,400-block period. It preserves every existing
positive-count transition, historical mainnet approval, and checked-in testnet
policy hash. Shared JSON schema tooling can now express that mode; the shared Go
validator enforces exact cross-field equality. Both current-steering and historical
decision snapshot consumers remain unchanged and compare actual epoch-zero RPC
bytes against an independently signed public production config in the new tests.
The installer must derive all initial policy fields from its complete approved
body and require effective epoch zero. Eleven protocol and thirteen validator
roots pass normal/race, and vet passes. Three causal controls each reproduce
their intended assertion in both modes. Composed installer acceptance remains
pending; the receipt distinguishes the tested dependencies from the future
release and retains one missing causal-wrapper exit. This is a source-only
correction; no mainnet configuration or deployment changed.

The September 30 [current production release builder](RELEASE-BUILD.md) preserves
seventeen commands, five production contracts and eight image contexts. The
original complete census found nine server build failures caused by stale local
SDK/Connect overrides; server `898dc8f3` fixes the exact module graph, and
independent Sol qualification rebuilds all thirteen server commands and passes
three source-graph roots in normal/race modes plus vet. The four SN commands
also compile in the author's census. The subsequent
[fresh-catalogue qualification](evidence/fresh-contract-catalogue-build-20260930.md)
records all seventeen binaries built in one sealed candidate, ten exact contract
byte pairs and unchanged deployment interfaces. Independent Sol qualification
passes all 31 builder roots normal/race, vet, four causal controls and all 170
artifact readbacks. Executable compile results remain separate from application
behavioral qualification.
The [bounded scratch-image qualification](evidence/scratch-image-qualification-20261001.md)
adds a repeatable Linux/amd64 OCI build and independent binary readback for
`server-competitionworker` from that exact candidate. Both author and Sol
independently produced the same archive; 51 builder roots pass normal/race.
The [seven-service qualification](evidence/seven-service-image-qualification-20261001.md)
closes the local Ubuntu base and remote `ADD` inputs with exact pinned offline
sources. Sol independently rebuilt all seven remaining images byte-identically,
verified their OCI/rootfs/binary content and passed 73 focused roots normally
and with race detection, vet and five causal controls. Aggregate release and
deployment flags remain false.
The [offline image aggregation](evidence/release-image-aggregate-qualification-20261001.md)
now verifies one original source manifest plus both supplements and emits a
separate complete eight-image attestation. It rehashes all 337 artifacts of the
frozen SN `2d53e6f2` / server `ecbf3aad` candidate, checks binary source/module
metadata, and replays all OCI/rootfs and source-input joins. Only local
`source_to_image_verified` becomes true; the original three receipts are
unchanged. Reproducibility, release completion and deployment approval stay
false. This closes the receipt-composition gap, while MG-02 remains open for
the independent builder, archive/restore, runtime/configuration and policy gates.
The later [pre-Safe baseline and permission repair](evidence/release-pre-safe-baseline-and-modes-20261001.md)
bind SN `1806b3b3` / server `0b8e758d` and repeat all seventeen executable
and ten contract creation/runtime outputs byte-identically on one host. The
first API export exposed a private-umask defect: correct binary bytes were
copied with `0700`, so strict OCI readback refused them. The separate builder
fix sets exact declared permissions through the owned descriptor; all 89 roots
pass normal/race and vet, while the unchanged-source causal test fails in
both modes. Fresh disjoint exports then pass all eight OCI readbacks under
`umask 077`, and their separate aggregate verifies all 337 retained artifacts
and exact source/image joins. Only local source-to-image coverage advances.
This baseline precedes the public Safe-submission changes and cannot attest
their release. The separate
[frozen Safe-source release](evidence/release-safe-source-20261001.md) builds
SN `095a2208` / server `0b8e758d` twice with the same dependency pins and
supplies eight fresh OCI images and its own complete local aggregate. All
eight platform manifests and OCI archives match on repeat. The
55-file inventory repeats exactly, with policy and published/deployed image
identity still missing. Independent reproducibility and actual production
configuration, migration/restore, service, policy and release approval remain
open; later reporting commits do not replace the frozen source identity.
The [current-admission packaging baseline](evidence/release-6c801a25-server720-20261001.md)
then binds SN `6c801a25` / server `720e7c61`, with its own source lock, two
fresh-cache 17-executable/five-contract builds and eight local OCI joins.
Only local source-to-image verification advances. The first worktree build
was refused for missing embedded VCS metadata; an exact-commit physical-clone
retry passes unchanged provenance checks. Independent release qualification,
live migration and policy remain gates. Later passive-root host, owner
recycle-mode transition and mixed-writer/migration-751 source selections need
a successor build.
The [successor preparation](evidence/release-successor-preparation-20261001.md)
retains exact pre-owner SN `7ee916cc` / server `94229abb`. The subsequent
[historical release](evidence/release-233ea2be-server942-20261001.md) builds
merged SN `233ea2be` / server `94229abb` twice after the owner gates pass,
with unchanged dependency pins. Both sets of seventeen executables, five
fresh contracts and eight local OCI images match; each complete aggregate
rehashes 337 artifacts. The 56-file inventory is identical on repeat and
retains 20 migration source files with 751 catalogue entries. All 89 builder
normal/race roots and vet pass. Later documentation is not the binary source;
independent reproduction, live policy, migration and deployment remain gates.

The [historical finality/migration successor](evidence/release-689938d6-server6c39-20261001.md)
binds exact SN `689938d6` / server `6c39d307`. Its two sequential source
builds match all seventeen binaries and ten contract bytecode outputs, retain
175 artifacts each, and include all twelve current migration implementations
and catalogs. Both complete eight-image aggregates verify 342 artifacts each,
with identical platform/configuration/archive bytes. The unsigned 61-file
inventory repeats exactly, retaining twenty-five database/monitor source files
and 751 catalogue entries without applying migrations. All 95 builder/source-graph
normal/race roots, vet and actual-source migration controls pass. The earlier
e4 attempt omitted eight required inputs and was superseded without repairing
its outputs. Independent compiler/build reproduction, live policy, migration
and deployment remain gates.

The [earlier metadata/HTTP successor](evidence/release-bab49e1c-servera3e2-20261001.md)
now binds exact SN `bab49e1c` / server `a3e2e668`. Its two sequential builds
and separate empty image stores reproduce all seventeen executables, ten
bytecode outputs and eight OCI platform/configuration/archive identities.
Both complete aggregates verify 342 original parent/supplement artifacts.
The repeated 61-file inventory totals 807,538,809 bytes and retains the same
twelve migration inputs and 751 source catalogue entries. All 95 builder/source-graph
and thirteen server sampler roots pass normal/race, with both sets of packages
passing vet. The sampler remains off by default. Full module-body qualification
is absent for 363 SN and 372 server graph nodes; local compilation is not full
provenance. Original component receipts keep their source/server scope, and
independent build, actual service/configuration/policy, migration/restore,
publication and deployment approval remain open.

The [bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md)
binds exact SN `28ebfced` / server `ac86855d`, including native observation
closure, refreshed EVM send admission and the selected-grant locking change.
Connect/SDK pins remain unchanged. Two sequential fresh-cache builds match all
seventeen executables and ten bytecode outputs; both complete eight-image
aggregates verify 342 artifacts with identical platform/configuration/archive
bytes. All 95 builder/source-graph and thirteen sampler roots pass normal/race
and their packages pass vet. The repeated 61-file inventory retains twelve
migration inputs and 751 source catalogue entries without applying migrations.
Sixteen original component records retain their exact source and failure scopes.
Independent readback passes both source candidates and both eight-image sets;
its primary receipt and separate B-image addendum preserve their distinct scope.
Local builds and artifact readback do not close independent compiler/build
reproduction, composed service/database behavior, production policy, migration,
rollout, published identity or release/deployment approval. Later source changes
require a separate exact release; reporting commits do not change these pins.
This baseline remains **superseded for launch** by the
[exact custody successor](evidence/release-1d580d5e-serverac86-20261001.md); its original receipts are preserved.

The [qualified readiness release baseline](evidence/release-3d1e2ecf-serverac86-20261002.md) binds frozen
SN `3d1e2ecf` / server `ac86855d`, including the five-marker/passive-root correction.
It is **superseded as local artifact selection** by the [qualified scoped baseline](evidence/release-1320845d-server0aa1-20261002.md) at SN `1320845d` / server `0aa1e244`, which includes the later owner, contract-admission and recycle corrections. Its own receipts remain historical and unchanged; neither selection authorizes launch.
Two sequential fresh-cache builds match seventeen binaries and ten bytecodes;
two separate eight-image builds match OCI platform/config/archive bytes.
Independent A/B source and all sixteen image readbacks pass. The unchanged author
seal verifies 1,060 retained files and must be read with the separate corrected
finalization guard. The failed schema adapter and checkpointed seal-only
continuation remain recorded; no source/image build or completed qualification
phase was rerun. Original readiness and historical component scopes stay separate.
The exact binary captures a new public combined snapshot at block 9,191,688 and
emits identical ten-action plans, all blocked/non-executable with twenty-eight
missing and two supplied-unvalidated requirements. The `1d580d5e` predecessor
receipts remain preserved. Independent compiler provenance, complete production
qualification, legacy writer custody, live authority and acceptance remain open.

The [original EVM custody successor](evidence/release-1d580d5e-serverac86-20261001.md) previously bound exact
SN `1d580d5e` / server `ac86855d`, including writer `cb9f3aa2`, borrowed-reader
`1922981d` and diagnostic `4e6b4a7e`. Ten clean source checkouts were freshly
fetched from remotes to the data volume. Two sequential fresh-cache builds
match seventeen executables and ten bytecode outputs; both eight-image
aggregates verify 342 source/image artifacts and match all platform,
configuration and archive bytes. Builder/source-graph and sampler normal/race
gates and vet pass. Twenty component records retain their original source,
failure and continuation scopes; exact Go/module inclusion of the qualified
custody source and all 107 custody author evidence checksums pass. The repeated
61-file inventory retains twelve migration inputs and 751 catalogue entries
without applying migrations. Independent compiler/build reproduction, complete
module-body provenance, arm64, restore, production policy/configuration,
service behavior, migration/rollout, attestation and actual deployed image
identity remain open. The separately [qualified five-marker/passive-root custody
correction](evidence/bootstrap-readiness-custody-qualification-20261001.md) at `3d1e2ecf`
is outside that historical source and is now included in the
[exact readiness successor](evidence/release-3d1e2ecf-serverac86-20261002.md). Neither local artifact selection
approves release or launch.

Independent A/B source and all sixteen OCI image readbacks pass, with the
primary receipt and B-image addendum retaining separate scopes. The exact
release binary also captures a fresh public combined snapshot at block
9,191,346 and emits two identical unsigned plans. All ten actions remain
blocked/non-executable, with twenty-eight missing and two supplied-unvalidated
requirements; both authority/readiness flags remain false. The planning
supplement is sealed separately from the build artifacts.

The retained-mode manifest keeps historical and compiled contract hashes:
Coordinator and ValidatorEvidence have metadata drift associated with the changed
imported SettlementVault source, so that selection's source-to-bytecode equality
stays false. Explicit `contract_catalog: "fresh"` now exports exact compiler
creation/runtime bytes for all five contracts to a separate schema-1 catalogue,
retains the old catalogue/history and checks unchanged ABI, constructors, layout
and semantic immutable references. Selected hashes are separate from historical
hashes; fresh mode refuses nonexact bytecode and retained mode refuses silent
replacement.

No signed mainnet plan has been evidenced; the existing release/testnet catalogue
does not establish a mainnet commitment. Independent qualification of fresh
selection is the preferred path for the first unsigned mainnet plan, consumed by
its exact file path and SHA256. Checking for externally held signed commitments
remains a launch gate; preserve and reconcile any such commitments before
changing selection. A metadata-equivalence exception remains a conditional
fallback and is not selected. Independent image reproduction remains scoped to
SN `2d53e6f2` / server `ecbf3aad`; later source candidates keep their separate
independent-builder gate. Published/deployed image identity,
compiler installation/config/policy qualification and release approval remain
open; the historical v11 inventory does not attest this current composition.

The newer server source branch `codex/mainnet-composed-hardening-20260927` at
`b6f49bdb` includes migration 728, the operator receipt-census correction,
atomic payer admission and checked settlement arithmetic on the v11 server
base. Its earlier `7bf88d79` combined controller selector passed normal, race
and vet on disposable PostgreSQL/Redis
([evidence](evidence/server-composed-hardening-20260927.md)). This source has no refreshed complete
release lock, binary/image inventory or deployment approval; the v11 inventory
does not attest these changes. MG-02, MG-03 and MG-06 remain open.

The first broad server-model run under
`/mnt/data/sn-testnet/evidence/mainnet-server-model-full-20260927` ended on its
90-minute deadline with **1,048 passed, eight failed and seven skipped roots**.
It is [incomplete diagnostic collection](evidence/server-model-diagnostic-20260928.md),
not frozen composed-release qualification. Its
launcher entered the original server directory, whose relative Go replacements
resolved active sibling checkouts rather than the prepared pinned workspace.
The actual executable and provenance correction are retained alongside that
directory's `PROVENANCE.md`; disposable-service cleanup completed successfully.
For MG-02/MG-10, validate the compiler's actual `go list -m -json all` module
directories, physical targets, exact revisions and clean state before and after
qualification. Use real isolated worktrees for every local replacement and
retain the compiled executable. A prepared workspace or an intended source lock
does not establish which dependency bytes the compiler used.

The corrected full model body finished separately under
`/mnt/data/sn-testnet/evidence/server-model-final-20260927`, with SN `615a7675`
and server `4468a696` in real frozen worktrees. The guard checks the actual
resolved module graph before compilation and after completion; the runner
retains model/controller/handler executables and build metadata. Its complete
census is **1,108 passed, three failed and seven skipped; package fail**, with
no missing roots. This is not an accepted composed release. The
[completion receipt](evidence/server-model-completion-20260928.md) records
terminal evidence, qualified corrections and capture limitations. Server
`4468a696` corrects the
historical payment, retention and probe fixtures without changing production
guards or scheduling; 18 affected roots passed normal and race qualification
([receipt](https://github.com/urnetwork/server/blob/4468a6961c00cf0ff8b84986259fa9698a7a8441/local/model-fixture-qualification-20260927.md)).
Its three remaining fixture failures have separate normal/race qualification
on the integrated corrections; the original full-body result remains retained.

All eight asserted diagnostic failures now have bounded fixture corrections in
the composed server branch through `936c3d9563372e8f424d516ee2dd3525555206de`.
The additional egress chronology selection passes 15 roots normally and with
race detection. The final two retention fixtures and six custody guards pass
both modes; old-fixture causal controls fail at the intended assertions.
[Integration evidence](evidence/server-model-fixture-integration-20260928.md)
records the exact commits and receipts. These changes preserve production
guards and probe policy; they do not turn the earlier deadline failure or the
completed frozen body's failing package result into a passing full invocation.

The frozen qualification's first direct-binary invocation used the module
directory instead of the test package directory. Its 15-test prefix is retained
and disqualified; the corrected body under `server-model-final-20260927/model-run`
reuses the same compiled binaries from `server/model`. Source and service cleanup
checks passed before that correction. Terminating the old `go tool test2json`
wrapper also returned zero without a complete run: exit status alone is not
qualification. Require all **1,118 roots listed by the actual Linux binary**
and the terminal package outcome, recording skips separately. The source tree's
1,122 function declarations are not the executed census. For future captures,
use the maintained [qualification owner](../scripts/qualification/main.go),
which already checks actual package/module paths, executes from the package
directory and verifies terminal membership. The retained body finished
naturally, so there is no missing prefix to restart. Its original shell wrapper
was lost; terminal events, source/binary checks and independently verified
disposable-service cleanup are retained without inventing a shell exit code.
The maintained runner also requires physical Go tool paths. A later focused
capture was refused before compilation because the default host cache path
traversed a symlink. Use the explicit `/mnt/data` cache and temporary-directory
profile in [MAINNET.md](MAINNET.md#acceptance-evidence-and-implementation-qualification)
for new captures; retain that preflight failure separately from executed tests.

For MG-08, the majority SN25 validator runs the standard `sn/validator` binary
with its ordinary evidence-based scoring policy. It is an indirect reset aid,
not a native removal authority. Old miners absent from eligible head and pool
evidence receive no positive weight from our validator; old miners with valid
current evidence may still be weighted. The resulting weights may move some old
miners toward the bottom of the emission-ranked trim order after native
processing, but majority control is not a promise of zero weight. Exact finalized
emission rows, all other eligible neurons, immunity and protected roles must
be re-censused before proposing a capacity. Root-subnet validator weights do
not perform SN25 deregistration. [Runtime trim source](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171), [subnet weight source](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/weights.rs#L865).
The reviewed registration-allowed setter is chain-Root-only, beyond the owned
SN25 keys, and a coldkey-authorized hotkey swap can change an existing UID
despite a closed registration flag. Admission must account for both facts
through inclusion. [Registration setter](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L728), [swap implementation](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/swap/swap_hotkey.rs#L101).
The owner-trim execution gate can use a bounded safe-set proof instead of an
atomic hotkey predicate only when every generation removable through call
expiry is an approved old miner and every protected identity stays immune.
Prove registration and swap/custody fences, immunity expiry and capacity over
the complete mortal call window, including earlier same-block actions. A
closed registration flag alone does not rule out subnet-owner takeover during
an epoch, which can directly register a neuron; first hotkey swaps also have
no cooldown. Reconcile actual removals from the finalized receipt.
Majority-validator weights may change trim order but cannot prove this gate.
The signer-free [`owner-trim-qualify`](OWNER-TRIM-BOUNDED.md) now checks a
conservative no-epoch window from authenticated census, metadata and storage,
including immunity-through-expiry, registration, both swap paths, admin timing,
lease absence and capacity. It preserves every requested old generation and
conditional residual count. Favorable predicates leave owner/proxy/pending
actions, governance/runtime and public subnet-pruning/reuse fences explicitly
unproved; executable apply,
actual signed mortality, custody effects and actual-subset receipt/reconciliation
remain blocked. [Qualification evidence](evidence/owner-trim-bounded-20260927.md).

MG-08 now has a separate signer-free [root observation foundation](ROOT-VALIDATOR.md):
`root-preview` and finite `root-monitor` bind the approved mainnet domain and
runtime artifacts to a complete root seat census, mapping/generation/ownership,
stake/pruning risk, stored strategy and delegation evidence. Do not equate a
read-only `ready` sample with mainnet activation. The observer never loads a
signer, automatically re-registers a pruned seat or mistakes a fresh burn quote
for a transaction cap. Missing full custom-weight eligibility, historical
basket/delegation custody and the separate root signing/capability service remain
explicit blockers; UR validator readiness remains independent.

The [SN25 reset preview](SUBNET-CENSUS.md) supplies a bounded, finalized
forward/reverse UID census and exact generation-scoped trim comparison. It
retains all owner identities as protected even when runtime immunity is
narrower, and refuses a candidate claim when the trim-call metadata changes.
It does not infer an arbitrary-removal owner call or permit destructive apply
without an execution-time guard and complete custody/history reconciliation.

Contract installation has a narrow build margin: the [2026-09-27 candidate
size check](evidence/contract-size-candidate-20260927.md) measures
`STCoordinator` at 24,564 runtime bytes, 12 bytes below Foundry's 24,576-byte
limit. Bind that exact build into MG-02/MG-08 and qualify creation on the
selected live runtime; a passing local build does not establish live
deployability after a source/toolchain change.

The separate nine-action installation candidate has successful complete-command
normal/race diagnostics, but its [failure-test correction is blocked by automatic
review](evidence/contract-graph-review-block-20260928.md), citing possible
cybersecurity risk. The frozen selection has finished its bodies with 47 of 48
roots passing in each mode, retaining the failed Safe inner-outcome test and
its non-discriminating control. No corrected fixture,
complete graph qualification or production integration is claimed. MG-08 stays
open; unrelated recovery and monitoring work continues.

MG-02 still needs actual OCI image identities and running-image readback.
Clean server `9f860731` API and taskworker Linux/amd64 binaries built and
repeated byte-for-byte under their Makefile profile; the
[external build record](/mnt/data/sn-testnet/evidence/mainnet-server-binaries-20260927/RESULT.md)
retains both hashes. Adjacent review found six server Dockerfiles still naming
mutable `ubuntu:24.04` while API pinned its multi-platform digest. Server
branch `codex/mainnet-server-image-pins-20260927` commit `969d6c74` pins all
six to that existing digest; both operator binaries remained byte-identical
after the source-only change. The branch is pushed and server `969d6c74` is
now part of the clean v9 source lock. The v9 composition built fresh server
API/taskworker binaries against SN `265231f9` and inventories all eight server
Dockerfiles. No selected or published OCI image digest, approved production architecture set,
deployment manifest or running-image readback exists. A Dockerfile pin and
local binary do not supply an image.

The [v9 image probe](/mnt/data/sn-testnet/evidence/mainnet-images-v9-20260927/RESULT.md)
found that the pinned base still installed packages from moving Ubuntu
repositories. Server commit `a211d56c` closes that input drift for seven
service Dockerfiles: the [package lock and signed-index proof](/mnt/data/sn-testnet/evidence/mainnet-server-package-pins-20260927/RESULT.md)
bind 40 exact payloads for amd64/arm64, and the final install runs without
network access. The [independent API/proxy image probes](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md)
built the amended amd64 recipes and extracted exact selected binaries. The
[v10 source and file inventory](evidence/release-candidate-v10-20260927.md)
includes the lock, payloads and signed-index inputs. Keep MG-02 open for an
owned package archive/restore, actual arm64 image qualification, selected and
published OCI manifests, full source-to-image provenance, production rollout
and running-image readback. A local image digest does not prove publication.
An [uncached API rebuild](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md#forced-rebuild-result)
with identical source inputs produced a different OCI digest: package install
logs/cache embedded wall-clock data and hundreds of file timestamps changed.
This is a demonstrated reproducibility defect to fix before claiming a
byte-identical production image build.
Server `77cb401e` addresses that defect for the tested package closures:
it removes only the two volatile build outputs and makes the seven image
recipes supply a fixed epoch and timestamp-rewriting export. The
[no-cache comparison](/mnt/data/sn-testnet/evidence/mainnet-server-image-repro-20260927/RESULT.md)
repeated API and proxy runnable amd64 platform bytes exactly, while keeping
distinct run-specific provenance-bearing indexes. The [v11 candidate](evidence/release-candidate-v11-20260927.md)
binds the new source and all seven Makefiles. Keep the other five services,
arm64, archive/restore, builder identity, attestation policy and deployed-image
readback as open gates; this local comparison does not approve an image.

The local validator init fixture initially failed under host umask `0002`
because `testing.TempDir` supplied a group-writable numbered seed parent.
The [causal correction](/mnt/data/sn-testnet/evidence/mainnet-seed-fixtures-20260927/RESULT.md)
creates an explicit private fixture child and passes real init under umasks
`0000`, `0002`, `0022` and `0077`; 87 affected tests pass normally and under
race. Preserve the production refusal of unsafe seed parents. Production
service manifests must independently provision and verify private state and
seed directories; passing a fixture is not a deployment permission audit.

The MG-08 action core supplies an offline-qualified
[existing-seat action owner](ROOT-ACTION.md): mortal root basket encoding,
one-request signing/nonce ownership, private durable state, exact-byte retries,
and retained finalized dispatch/fee/runtime-deviation or expiry evidence. Missing
or empty required state cannot resurrect an allowance, and a runtime change
blocks new effects while old receipts remain recoverable. The new
[offline custody handoff](ROOT-OFFLINE-CUSTODY.md) authenticates independent
exact-action approval, durably imports one matching public native signature and
recovers the same bytes after interruption; local absence never proves that no
signature was issued. The [root service decision owner](ROOT-SERVICE.md) now
retains a bounded approved-basket decision and original native intent atomically,
owns finite joined supervision and separates observation, current authority,
custody and submission capabilities. Exact-block read-only weight observation
and pinned-runtime normalization supply necessary decision checks; they never
authorize effects. The separate [owned-RPC submission adapter](ROOT-SUBMISSION.md)
now authenticates its own action/route approval, retains numbered uncertain sends
and reconciles exact original bytes before any later approved attempt. The
composed offline-custody/service/HTTP/receipt path passes deterministic local
qualification. The [qualified bounded root-service command](evidence/root-service-runtime-qualification-20260930.md)
now admits original input and recovers issued signatures without another
signing or broadcast allowance: 12 focused and 100 adjacent roots pass
normal/race; five normal and two race controls are causal. Production live
authority, globally fenced native custody, a separate hardware signing device
and activation remain absent. Native fee
quotes are not atomic caps; source/policy hashes are not on-chain runtime locks.
Do not activate signing from a read-only-ready sample or invent a heartbeat for
the accumulation strategy. Mainnet identity, existing seat, complete eligibility,
approved custody and limits remain explicit gates.

The [executable contract bootstrap phase](BOOTSTRAP-CONTRACTS.md) now
prepares and resumes reserve CREATE through original public EVM signed bytes,
bounded durable attempts, shared owned-HTTP transport and canonical
runtime/getter recovery. The [reserve qualification](evidence/bootstrap-contract-qualification-20260928.md)
records passed normal/race scopes, causal controls and the subsequent composed
dependency check. The [second vault CREATE action](evidence/bootstrap-contract-vault-qualification-20260928.md)
is predecessor-bound, shares the signed graph's attempts and funding ceiling,
and passed 51 focused normal/race roots, 426 full normal roots and five causal
controls on its frozen source graph. The [third coordinator implementation
CREATE action](evidence/bootstrap-contract-coordinator-qualification-20260928.md)
binds both predecessors and authenticates the disabled-initializer storage; its
71 focused roots, split race shards, 446 full normal roots and five causal
controls passed. The [fourth escrow-registration
action](evidence/bootstrap-contract-escrow-qualification-20260928.md) binds
the derived vault and three completed predecessors; 95 selected normal/race
roots, 470 full normal roots and eight causal controls passed. The [fifth atomic
proxy CREATE action](evidence/bootstrap-contract-proxy-qualification-20260928.md)
passed 120 selected normal/race roots, 495 full normal roots and ten causal
controls for its initializer, storage and recovery guards. Healthy head advancement is
revalidated within the operation; an unavailable mapping read does not become a
successful-value mismatch. EVM signature liability survives local approval
expiry. The [sixth reserve-binding action](evidence/bootstrap-contract-reserve-link-qualification-20260928.md)
passed all 27 roots normally and under race detection, the 523-root full normal
package, ten adjacent low-gas roots in both modes, and four causal controls.
The [seventh vault-binding action](evidence/bootstrap-contract-vault-link-qualification-20260929.md)
passed its corrected checkpoint normally and all 29 roots under race detection,
plus six adjacent checkpoint roots in both modes and a selector causal control.
The other two installation actions, Safe inner-call success/getter
verification and authenticated live custody/network inputs remain open.
The [approval-preview/offline recovery follow-up](evidence/bootstrap-contract-preview-20260928.md)
adds a read-only unsigned CLI export of exact approval bytes and preserves
terminal receipts on offline reopen with an explicit retained-observation label.
Preview opens no journal or route; signed execution remains independently
approved. Its nine affected command roots and five causal cases pass normally
and with race detection; this is local qualification, not live installation.

The [offline chain composition receipt](evidence/bootstrap-chain-qualification-20260928.md)
adds 369 passing full normal roots, all 87 adjacent race roots across six disjoint
shards, and four fixed/mutant causal controls. All 49 source/module fence
comparisons passed. The original aggregate race timeout remains failed evidence.
The tested physical graph used Connect `358cefae`, server `0633780c` and SDK
`42241118`; it does not qualify the current composed Connect `b163f9dd`, server
`5dc11761`, SDK `516521fb` release graph. Offline preparation leaves every
chain, producer/service activation, custody approval and actual-economic gate
open; no live effect or full release qualification is claimed.

The subsequent [integrated source qualification](evidence/final-composed-source-qualification-20260928.md)
does cover the current Connect `b163f9dd`, server `5dc11761`, SDK `516521fb`
physical graph: all 379 full normal mainnet roots and 79 descendants passed,
and all 122 selected monitor/bootstrap race roots plus 79 descendants passed
across nine bounded shards. Source and module fences passed. The prior
component graph limitation remains in its own receipt; the newer result is
source qualification, not a release image, live activation or accepted mainnet.

The MG-08 v2 addition verifies two initial schema-3 production configs and
their domain-separated approvals offline, using the existing validator loader
rules. Independent role/signer/runtime/source/deployment pins and retained
protected generations must agree; declared custody namespaces remain separate.
The intended majority/secondary labels do not establish live stake, permits,
key possession or service identity. Existing v1 custody remains resumable at
its original unadmitted scope. The [v2 admission and shared-decoder qualification](evidence/ur-bootstrap-admission-qualification-20260928.md)
now passes all 24 joined stages, 588 root and 183 descendant executions on the
frozen composed graph. All six original causal captures retain their normal/race
named failures; R3 resumed all 24 stages without rerunning bodies and passed
input/source fences. The original unsorted-census refusal, global input-seal
refusal and R2 binary-mode refusal remain recorded. These bounded offline
results keep all live chain/service/economic and release gates open.

The September 29 MG-08 `bootstrap-chain readiness` source increment adds a
bounded read-only phase after accepted v3 preparation. It checks original child
journals under shared read-only locks, then observes both UR generations,
activity, permits and signed block windows plus the separate root seat and
mortal checkpoint at one finalized snapshot. Conflicting generations and
subnet scope remain per-role blockers; transport/runtime/integrity gaps return
explicit unresolved output without partial eligibility. It preserves exact
signed approvals, child allowances and all five pending chain phases. Its
[Sol qualification](evidence/bootstrap-readiness-qualification-20260929.md)
passes ten new normal/race roots and the exact 148-root adjacent normal/race
scope, with vet, unchanged source/module fences and four causal controls.
The root integrated exact qualified source `6627d15f`; the receipt records the
exact race union. Sol later intentionally terminated the redundant broad process
after its passing prefix; that process has no claimed successful terminal exit.
The [composed receipt](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/COMPOSED-RESULT.md)
passes focused 10/10 normal/race, vet and fences on SN `e35771ec` plus server
`b7c8c743`, without changing the earlier exact 148-root qualification.
Current authority, effective stake/eligibility, global custody and signing-device
fencing, contract completion, service activation and actual native 10/90
outcomes remain open gates.

The September 29 [qualified owner-trim action increment](evidence/owner-trim-null-storage-repair-20260929.md),
integrated at exact source `ce567305`, adds a separately approved coldkey action
and owned route, a fixed sixth journal
under original v3 custody locks, exact signature/nonce/era recovery, canonical
dispatch/fee receipts and before/after actual-subset correspondence with explicit
old-miner residuals. Transaction finality and a completed native reset are
separate outcomes; unavailable post-state admits only readback continuation.
Current runtime predicates include proxy absence, nonce, bounded selection,
protected generations and subnet immunity through expiry. Independent enforced
owner/governance/custody/provenance/exposure authority remains required; a
conditional pass never supplies it. Sol's 25/25 focused and 229/229 expanded
roots pass normal/race, with vet, unchanged source/module fences and eight
intended causal controls. The exact race union is complete; the redundant
unsharded bootstrap process was intentionally terminated after its passing
29-root prefix and has no claimed successful terminal exit. The failed R1
candidate retains 21/22 focused and 225/226 expanded results in both modes;
its valid-null proxy-reader bug was fixed in R2, not relabeled as a pass.
The October 2 [retained-intent successor](evidence/owner-trim-retained-intent-20261002.md)
fixes the additional trim marker/journal custody and implements the separately
approved [best-effort workflow](OWNER-TRIM-BEST-EFFORT.md) at exact source
`0f7c869869a5201e8ffbbf4e053cc0ca20a12010`, preserving strict v1/v2.
Fresh current observations showed expired SN25 pruning immunity and both stored
registration flags true; inspected v470 setters do not supply owner authority to
close all registration routes. Separate signed options may accept public
pruning/netuid reuse and competing registration/re-entry risk, but neither option
ignores observed generation/flag/proxy drift or changes original signed bytes.
The code was authorized; no live residual risk was accepted and no live signing,
submission or activation occurred. Actual device, independent action and risk
approvals, classified census, external custody, canonical outcome review and both
validator-role activation gates remain open. `rootActionStore` still has only
test callers and legacy root service native mutation ports remain nil.
The selected passive observer does not need the retired root-weight action,
but observation alone does not establish the separately requested active root
participant. Reviewed v470 accumulation does not require periodic hotkey
signing or the retired root-weight call. Verify actual seat/stake/earning
behavior and separately approve the correct coldkey/proxy custody and transport
for any required current lifecycle action; the legacy native mutation ports
cannot supply that authority.
The historical owned Snow read returned HTTP 502 at 06:59 UTC on September 29;
that observation established no current mainnet identity or live authority.

Start with MG-01's read-only route correction and MG-02's source composition.
Then close the recovery, runtime, policy, settlement and monitoring dependencies
before MG-08 can apply a reviewed bootstrap plan. MG-09 must close before
unattended operation. MG-10 separates pre-activation qualification from the
post-activation observations that can only be collected on a running mainnet.
Record implementation, exact qualification selector/results, deployed image,
independent observation, remaining action and evidence owner for every gate.
Historical testnet gates below are lessons and reusable evidence, not an active
instruction to restart the closed campaign.

## Implemented building blocks and missing production work

| Surface | Already present | Still required |
| --- | --- | --- |
| Native-chain authentication | [Exact artifacts](../crv4/runtime_identity.go), [reviewed catalog](../crv4/reviewed_runtime.go), [consumed-interface profile](../crv4/runtime_profile.go), [provisional admission](../crv4/runtime_compatibility.go), bounded per-connection metadata reuse and the [five-command miner fleet mainnet authority gate](../miner/FLEET-MAINNET-RUNTIME.md). | Supply independently approved mainnet genesis and runtime source/build/code/metadata pins; extend immutable operation views to every consumer, qualify semantic/economic compatibility, durable uncertain-send reconciliation and controlled upgrades. Provisional admission is explicitly testnet-only. |
| Recovery and service profile | Simulator journal/plan caches and retained warmup cited under PF-01/04; shared server [subnet-operator workload profile](../../server/taskworker/workload_profile.go) under PF-02. | Production package ownership, complete signature discovery, migration/readiness composition, deployed evidence and finite foreground latency. |
| Client-key rollover | Server `4b2c4587`, projection regression `9da52551`, successful non-accepting retained resume; composed current-main migrations 724–726 at `9f860731`. | Bind the composed release and coordinate live operator cutover with all consumers; prove current-domain readiness, retained pins and historical replay on the deployed image. |
| Usage and logs | Qualified epoch-658 usage quarantine `74893863`; immutable usage guard and append-only archive in server `9f860731`; scanner `df7ae2c4`. | Compatible writer/reader fleet and prospective complete settlement; independent streaming observability with durable byte coverage. Historical debt remains uncredited. |
| Economic-policy observation | Signer-free [finalized recycle-mode gate and cumulative integer reference](ECONOMIC-GATE.md), with 37 focused mainnet tests passing normally and with race detection. | Approved mainnet genesis/source-to-code pins, runtime-recognized owner census, authorized Recycle setting, weight mechanism and observed 10%/90% native outcome; the reference is not payout evidence. |
| Historical lifecycle provenance | Candidate `93949ee3` and `bbed208d` preserve original approved context, including cache reconstruction. | Integrate and qualify the actual recovery reader; include corruption, wrong-approval, changed-config and strict/current-use negative controls. |
| Production control plane | [Executable local root custody](BOOTSTRAP-ROOT.md), root service/owned submission owners, independently approved [standard validator 10/90 producer](OWNER-RECYCLE-PRODUCTION.md), contract/protocol packages and signer-free identity/census/economic observers. | Remaining executable mainnet chain bootstrap, production current-authority/device and protected registration, live approved 10/90 activation/history and observed native outcomes, independent domain monitor, authorized repair controller and deployed alerts/runbooks. |

**Server integration (MG-02/05/06; source qualified, 2026-09-27).** Server
branch `codex/mainnet-current-main-20260927` at `9f860731` carries all eleven
custody prerequisites on `origin/main` base `af17d1d2`, preserving current-main
settlement batching and provider probes. Migrations 724–726 retain policy
domains, immutable usage and the append-only archive; migration 727 adds durable
per-balance reservation revisions and Redis revision-fenced snapshots. The
v651→v724 migration-monitor namespace correction is included. The
[composed qualification](</mnt/data/sn-testnet/qualification/mainnet-current-main-20260927/RESULT.md>)
retains causal failures, exact source locks and 291 identical selected server
tests in normal and race modes with zero skips, plus SN/Connect integration and
cross-module compile. The server branch is pushed for review. This qualifies
source composition, not a live migration or production release.

The [offline composed-source lock and SN checks](/mnt/data/sn-testnet/evidence/mainnet-source-lock-20260927/RESULT.md)
then bind clean SN `c2d7685d`, this server candidate `9f860731`, compatible
Connect `c68689c4` and every local Go replacement without a `go.work`
override. Cross-module compile, selected SN normal tests and composed miner/
mainnet race tests passed. This is a candidate snapshot only: subsequent SN,
contract, generated-artifact or config changes require a new source/artifact
lock and affected qualification before MG-02 can close.

The independent [NetEscrow qualification](/mnt/data/sn-testnet/evidence/mainnet-netescrow-ordering-20260927/RESULT.md)
reproduces delayed-create/release and zero-byte preimage failures. The composed
tests cover those paths and current-main settlement amplification. Deployment
must stop and drain every additive cache writer, migrate the matching catalog,
upgrade every publisher together, verify actual database guards, and reconcile
before traffic. Persistent Redis fences and PostgreSQL revision tombstones need
explicit restoration and capacity policy. That original mirror-only correction
did not make admission atomic; the subsequent custody slice below moves the
admission authority into the database.
Live operator cutover, economics, reserve/claim conservation, capacity and the
complete production release gate remain open. The older `0633780c` branch and
its qualification remain as causal history, not the proposed current-main pin.

**Atomic payer admission (MG-06/PH-12; source-qualified, 2026-09-27).** Composed
server `b6f49bdb` includes admission `fbadd281` and its required arithmetic
successor `04e65680` (isolated commit identities). The
[causal qualification](/mnt/data/sn-testnet/evidence/mainnet-netescrow-admission-20260927/RESULT.md)
reproduces three failures on composed server `7bf88d79`: a missing create post,
two deliberately interleaved creators, and a missing settlement debit post can
all admit the same payer credit twice. Origin and companion creation now lock
eligible payer balances in stable ID order and read durable reservations in a
separate read-committed statement after the lock. The terminal settlement owner
debits consumed payer bytes in the same PostgreSQL commit as its outcome.
Cache restoration, lost posts, transaction rollback/retry and duplicate
settlement cannot authorize that credit twice. Migration history through 728,
signed usage attribution and zero-byte/zero-credit history remain unchanged.

The same review reproduced two settlement arithmetic bypasses: adding two
maximum signed reports wrapped the mean negative, and a negative legacy grant
could wrap the cumulative allocation into a false completed debit. Use the
overflow-safe floor mean and refuse negative consumed reports/grants before
claiming an outcome. Exact maximum-credit, split-grant, checked reservation
sum and debit-rollback tests retain the storage-limit boundaries. Negative
payer balances are already excluded by the database-generated active column;
that is an adjacent verified control, not another reproduced admission bug.

Qualification retains 123 distinct model normal passes across documented
slices and the same 123 passing together with race detection on the admission
source. The arithmetic successor passes 35 affected tests normally and with
race detection; five monitor checks pass both modes and final model/monitor
vet passes. Exact sources, causal failures, fixture corrections and separate
compiled-source boundaries are retained; this is not a claim that the full
server model suite or production capacity passed. Both disposable PG/Redis
pairs were removed after qualification. The composed server tree matches the
qualified final isolated tree exactly; release locks/artifacts must name the
new composed revision.

This is prospective source qualification, not a deployed financial repair.
Drain and replace every creator and asynchronous old debit writer together;
mixed versions cannot preserve this guarantee. Historical terminal outcomes
and settled escrow markers do not prove an old debit post ran: require exact
historical balance/debit reconciliation before activation, without guessed
charges or automatic post replay. Participant sweep publication, Redis account
payout increments and statistics remain asynchronous residual work. Keep their
durability/idempotence, complete settlement conservation, per-payer contention,
archive capacity and coordinated database/cache restore as open launch gates.
Source review of `b6f49bdb` found the next specific provider-payment gap:
the terminal settlement transaction can commit before its in-memory post writes
`transfer_escrow_sweep`. The payment planner reads those PostgreSQL sweep rows,
so losing the post can omit a provider payment and a duplicate terminal claim
does not create it. Require a causal lost-post/rollback/replay test and a durable
same-transaction or journaled correction before MG-06 closure. Redis summary
counters are a separate, lower-priority publication path.
An earlier automatic approval review rejected the implementation action for
this provider-payment correction, citing possible cybersecurity risk. No
durable correction or successful qualification is claimed for that open item.

**Retained timestamp custody (MG-06/PH-12; source-qualified, 2026-09-27).** Server
`6e2bcfa7` appends this correction to the v11 server candidate `77cb401e`. The previous
live/archive reader filtered both stores by `close_time`, silently omitting
historical terminal rows whose timestamp is NULL. That could produce a partial
or empty payout despite retained unresolved work. The corrected single-statement
reader probes at most one unknown-time identity per store and refuses the new
payout before operator chain reads or artifact construction. Migration 728
appends a restartable online partial index and exact schema admission; the
catalog through 727 remains byte-identical. Canceled/open work stays excluded,
paid/free attribution is unchanged, and archived NULL debt stays unresolved.
The [causal qualification](/mnt/data/sn-testnet/evidence/mainnet-usage-time-custody-20260927/RESULT.md)
records red/green live and archived failures, retention rollback/retry, the
operator barrier, 140 distinct selected tests passing normally and with race
detection across documented qualification slices, and vet. Index admission also
accepts the two exactly verified PostgreSQL dump/restore predicate renderings
without admitting a changed predicate or key order. This does not deploy the
fix, authorize a historical exclusion
or make the prior artifacts complete. The coordinated 728 reader/writer/reaper
cutover, exact historical NULL census, archive capacity and NetEscrow restore
gates remain required; no close time may be guessed to unblock them.

**RPC read admission (MG-04/MG-07; In progress, 2026-09-27).** The shared
[JSON validator](../protocol/json_unique.go) and bounded
[miner](../miner/sn_rpc.go)/[validator](../validator/chain_http_envelope.go) RPC
readers now reject duplicate keys, including equivalent escaped spellings,
case-folded Unicode aliases, trailing JSON and HTTP redirects. The miner
transient-read operation has a tested 90-second retry budget. Full miner normal
and race suites passed; all 1,762 validator tests passed in both modes with zero
skips, and the final Unicode admission change passed focused consumer normal and
race tests. MG-04 and MG-07 remain open for broader production runtime and
deployed-monitor qualification; this source result does not close MG-02.

The September 27 SN root includes newer per-user deposit and zero-price policy
interfaces (`9b386fe8`). Old R48 pricing and build receipts cannot qualify those
new semantics. Freeze the intended launch policy and matching server/SDK APIs
under MG-02/06; do not resolve an interface mismatch by silently changing an
already signed policy or relabeling old economic evidence.

## Acceptance-window attribution from R44

R44 terminal diagnostics exposed two ways to draw a false conclusion from
otherwise valid retained evidence. A payout check selected the latest signed
artifact, which could belong to epoch 621 after the accepted [616,621)
window; an older signed claim observation lacked additive discovery fields,
which a diagnostic displayed as zero rather than unavailable. Before mainnet,
bind every tier/cohort assertion to the exact accepted epoch, committed root
and artifact hash. Treat absent legacy fields as unavailable, while preserving
real queue and receipt failures as failures. Qualify with a later conflicting
artifact, a missing historical field, an exact-window match and a changed
root/hash. The testnet repair is SN `f673ca9a`; composed release qualification
and deployment evidence are still required before this item is Done.

The detailed RT/RL/PF tracker began on 2026-09-22. Its requirements and historical
qualification receipts remain below; the production gates above determine the
current execution order. Automatic handling of compatible Subtensor upgrades
and the wider [hardening requirements](#production-hardening-from-sim-testnet)
still need production integration and qualification.

For each item, record its implementation commit and relevant test or operational
evidence before marking it done. Add newly discovered adjacent issues here.
Use `Planned`, `In progress`, `Blocked` and `Done` consistently. Keep each ID
stable, and record the concrete blocker and next action for blocked work.
The completion-evidence column below states the required result; it is not a
claim that the result has been achieved. Add links to actual receipts as work
closes, including the source revision and release containing the fix.
For each implementation update, record the remaining action, the affected
checks and any earlier results being reused. Passing tests alone does not mark
a fix done when its required deployment or operational evidence is still pending.
Dated incident accounts below describe their state at the time. They do not
reopen testnet or override the September 27 closure and implementation inventory.

## Prelaunch fix tracker

| ID | Fix | Depends on | Owner | Status | Completion evidence |
| --- | --- | --- | --- | --- | --- |
| RT-01 | Immutable runtime views anchored to the correct block and purpose | — | Astra | In progress | The [validator-read correction](evidence/validator-read-capability-20260927.md) anchors stake and schedule capabilities to the caller-approved exact historical artifact before their first storage decode. The [qualified source-receipt views](evidence/validator-source-runtime-qualification-20260929.md) bind original preparation, execution parent and post-state independently, including an approved upgrade block. Wider historical execution and concurrent signing contexts remain open. |
| RT-02 | Shared capability profiles for calls, storage, signing, CRv4, custom APIs and precompiles | — | Astra | In progress | [Stake/schedule read profiles](evidence/validator-read-capability-20260927.md) admit an independently approved compatible successor without another compiled spec entry and identify changed consumed storage/API interfaces. Production admission policy and other operation profiles remain open. |
| RT-03 | Construct and sign each native operation from one view; reconcile stale or uncertain attempts | RT-01, RT-02 | Astra | In progress | The [source capability correction](evidence/source-runtime-capability-20260927.md) refuses a strict successor's stale preparation view before storage/nonce work and preserves retained signatures across cold reauthentication and metadata eviction. [Approved-upgrade receipt recovery](evidence/validator-source-runtime-qualification-20260929.md) preserves original signed bytes and historical authority without rebroadcast or new signing authority. Wider native operation coverage and uncertain-send reconciliation remain open. |
| RT-04 | Replace version-specific live admission in simulator, miner, both validator paths and bootstrap | RT-01, RT-02, RT-03 | Astra | In progress | The [standard production validator](OWNER-RECYCLE-PRODUCTION.md) accepts independently signed schema-3 authority through exact producer capability checks and measured source/intent binding. Complete original authority bundles now preserve old sidecars, pending bytes and the economic activation across compatible approved renewals. [Downstream upload admission](VALIDATOR-UPLOAD-RUNTIME.md) projects their exact historical runtime windows without producer authority. Schema-2 observation remains read-only. A concrete [independently signed compatibility policy and semantic-certificate inspector](RUNTIME-CONTINUITY-POLICY.md) has scoped qualification; it grants no signing view or automatic selection. [Finite offline replay](RUNTIME-SEMANTIC-REPLAY.md) qualifies supplied cases without claiming semantic-rules coverage. Other roles, arbitrary policy/custody transitions and automatic upgrade qualification remain open. |
| RT-05 | Separate observed runtime from deployment, configuration and approval identity | RT-02, RT-04 | Astra | Planned | A compatible upgrade preserves the plan, approvals, leases, completed actions and observed epochs. |
| RT-06 | Preserve historical proof reuse and make metadata-cache capacity independent of catalog length | RT-01, RT-02 | Astra | In progress | The CRv4 per-connection metadata cache has fixed resident capacity, least-recent-use eviction, and exact-hash uncached admission when every slot is loading. The [proof-ownership correction](evidence/runtime-proof-eviction-20260927.md) separates an authenticated provisional view's authority from metadata residency: eviction preserves retained signatures and exact historical reuse, while fresh identity checks and strict/foreign-owner rejection remain. Composed release and production compatibility qualification remain open; no testnet profile grants mainnet authority. |
| RT-07 | Suspend only operations affected by an unsupported change and expose an actionable reason | RT-02, RT-04 | Astra | Planned | Independent services continue where their dependencies permit; recovery resumes from saved progress. |
| RT-08 | Qualify upgrade handling and record the mainnet-readiness evidence | RT-01 through RT-07 | Sol medium | Planned | Affected normal/race tests and a controlled upgrade during an active integration campaign pass with unchanged approvals and reconciled transactions. |
| RL-01 | Bind launch attestation to an explicitly approved immutable release and its complete input manifest | — | Astra | Planned | Publishing documentation or advancing main does not invalidate an approved unchanged deployment; changed executable, source, policy, contracts or dependencies still require the appropriate new approval. |
| PF-01 | Reuse an immutable journal index and authenticated historical plans within one reconciliation | — | Astra | In progress | Deterministic work-count tests bound journal indexing by entries and plan authentication by distinct sources, while each receipt retains its own identity and postcondition checks. |
| PF-02 | Give simulator operator taskworkers an explicit workload profile, including retained queue handling | — | Astra | In progress | Required subnet/operator tasks run for both operators; excluded queued tasks and post hooks remain untouched; production defaults and restart behavior pass affected tests and managed startup. |
| PF-03 | Include every retained operator signature in recovery and renewal accounting | — | Astra | In progress | The [receipt-census correction](evidence/operator-recovery-census-20260927.md) keeps original/replacement/cancellation attempts recoverable after read failures. The [qualified source census](evidence/operator-signature-census-qualification-20260928.md) supplies status-independent discovery across selected operator databases and evidence stores, plus byte-preserving local archive/restoration. The [qualified conditional observation join](evidence/operator-receipt-fee-qualification-20260928.md) adds offline receipt/fee accounting. Their [complete MG03/R48 lineage](evidence/operator-mg03-r48-composition-20260929.md) was composed and qualified at `05fee56f`, preserving the [original integration receipt](evidence/operator-mg03-integration-20260928.md) as history. The [qualified commitment verifier](evidence/operator-receipt-commitments-qualification-20260929.md) is now integrated at `fbe0c039`: 52 roots pass normal/race and four causal controls expose forged gas/status, substituted signed bytes and disconnected ancestry. The [qualified bounded collector](evidence/operator-receipt-collector-qualification-20260929.md), integrated at `b7c8c743`, now produces those proofs from explicit owned-RPC reads and replays them before private create-only publication; all 71 affected roots pass normal/race. The [qualified native finality proof](evidence/operator-native-finality-proof-qualification-20260929.md) is integrated at `44636e5e`: all 89 roots pass normal/race and five causal controls pass for weighted certificates, scheduled set changes and exact native/EVM mapping relative to a separately pinned checkpoint. Actual fees remain null; independent checkpoint/genesis/runtime admission and finality/account-nonce authority remain absent. The documentation-only [pinned-runtime fee dependency review](https://github.com/urnetwork/server/blob/cfcbfcbaa13b4f4d298acfeca761a7252c18ddee/strecovery/ACTUAL-FEE-DEPENDENCIES.md) at server `cfcbfcba` identifies generic-event and failed-refund attribution gaps. Bounded authenticated native-state proofs plus qualified execution replay or fee-specific runtime evidence are needed; a live approved checkpoint is not required for offline proof development. The [qualified native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md) at server `5ff7bf02` supplies bounded exact-boundary/descendant capture with preserved partial evidence and lifetime read budgets; all 112 affected roots pass normal/race with seven causal controls in both modes. Full closure still requires independent checkpoint admission, owned-node capability, authenticated native debit/refund and account nonce proofs, service adoption, composed release artifacts and live restart without duplicate actions or manual signature copying. |
| PF-04 | Diagnose validator warmup and support bounded, resumable semantic startup | — | Astra | In progress | Retained validators produce fresh proofs through both operators; startup exposes the pending criterion, uses a justified warmup budget, and preserves valid recovery progress without counting stale proofs as acceptance. |
| PF-05 | Give client-key histories an authenticated policy-scoped rollover | — | Astra | In progress | A scheduled policy change starts a new signed generation-1 segment for each client; old signed rows remain byte-identical and historically readable, current readers select only the active domain, and all miners regain processed-key readiness without bypassing it. |

PF-01, PF-02 and PF-04 have simulator implementation evidence; their remaining
work is production adoption and verification under MG-03/05. RT-01 through RT-08
form MG-04; RL-01 is part of MG-02. Link the bootstrap implementation and
launch sequence from [MAINNET.md](../mainnet/MAINNET.md) rather than maintaining
a second launch plan here.

RT-01 and RT-02 can proceed in parallel. After those foundations, cache/history,
transaction handling and operation-specific admission can progress independently
where their inputs are stable. An external manually reviewed artifact catalog
may be an interim aid; it does not complete RT-04's automatic-upgrade requirement.

The mainnet implementation order is:

1. Protect signing, historical interpretation and recovery first: RT-01,
   RT-02, RT-03 and PF-03. These establish the shared interfaces and preserve
   ownership of transactions across upgrades or interrupted startup.
2. Remove routine restart requirements: RT-04 through RT-07. Work on RL-01
   independently, and finish production verification for PF-01 and PF-02 on the
   selected release in the controlled rehearsal.
3. Complete RT-08 against the composed release, reusing unaffected results.
   Batch independent failures, fix their causes and adjacent paths, and rerun
   the affected checks. An interruption does not erase completed observations.

For each update, attach the implementation commit, affected checks, preserved
results, deployment evidence and next action to its stable ID. A new issue only
blocks operations that depend on it. Preserve testnet evidence as a closed source; qualify changed mainnet consumers
without restarting its acceptance campaign.

PF-05 follows the 2026-09-24 testnet policy-rate rollover. The new policy
activated and its runtime was published, but all 20 provider swarms reported
zero ready members even though their processes and both operator services were
live. `/connect/control` returned HTTP 200 with the application error
`client-key registration cannot replace a retired or different-domain head`.
The existing head is keyed by client ID while the signed history domain includes
the policy hash; a new policy therefore cannot append to the old segment.
Keep the old signed records immutable and introduce an additive domain-scoped
head and generation namespace. Authenticate policy activation before admitting
the new segment, keep generation-1 and same-domain successor rules strict, and
make current and historical API/validator readers choose their exact domain.
Test populated migration, concurrent rollover, retries, rotation, retirement,
network identity changes and old-epoch replay. The migration monitor's expected
schema must advance with the actual table shape. Retained resume must run both
operator migrations before starting the successor APIs; its older path omitted
that barrier. Any consumer using persistent peer key pins also needs an
authenticated policy transition for its domain ratchet, rather than clearing
pins. Do not treat HTTP success as
processed client-key success or mark a provider ready before its current-domain
registration completes. That testnet resume retained its supervisor on the
bounded readiness timeout. The later server branch and retained resume receipt
above establish recovery progress without repeated setup; production closure
still requires the composed migration and readiness evidence.

The [2026-10-01 operator qualification](evidence/operator-policy-custody-qualification-20261001.md)
also corrects a separate payout rollover defect: deposit sizing compared the
previous epoch's signed artifact with the newly active configuration hash and
could infer its window using the new epoch length. The server now authenticates
the current deployment, reads immutable `policyAt(sourceEpoch)` and the actual
epoch window at that same canonical hash, and repeats the native/network and
canonical boundary witness. Retained artifacts keep their original signatures
and need no original local policy file. Fresh issuance requires the configured
policy to match the authenticated epoch; it cannot backdate a successor policy.
The causal two-operator fixture includes the populated namespace migration,
processed control, restart, fresh validator proof reads, partial/foreign cohort
refusal, tombstones, deletion and exact successor deposit/reserve sizing. This
local qualification supplies no live migration, signing or deployment approval;
both production operators still need the future-boundary cutover and readiness
evidence above. MG-06 financial-history reconciliation and participant-sweep
durability remain separate open gates.

RL-01 follows an actual 2026-09-16 launch interruption: the qualified executable
was built at `541e13cf`, then publishing reports advanced main to `0fd7ffc0`.
All Go source, modules and release-lock bytes were identical, but
[executable attestation](../sim-testnet/executable_attestation.go) required the
running executable, checkout, fetched ref and current GitHub main to share the
same commit. Import correctly enforced that current rule and stopped before
mutation. The immediate recovery is a matching build and publication freeze.
The proposed replacement must authenticate a complete approved release;
matching only Go files is insufficient. Add deterministic controls for
documentation-only publication, unrelated later releases, unauthorized input
drift, revoked releases and restart from the retained approved release.
Its production implementation is tracked by MG-02; the historical launch remains
an unchanged failed attempt.

PF-01 follows source review during the 2026-09-16 managed startup.
[Carried-action preparation](../sim-testnet/carried_preparation.go) looks up each
action through a full journal copy and scan. The
[historical RPC identity check](../sim-testnet/owned_rpc_history.go) also rereads
and authenticates the same archived plan for each original public receipt.
These repeated costs remain after the separate journal-loading repair. Live
process counters establish ongoing work, not attribution of all startup time
to either path. That invocation later failed its taskworker log gate, recorded
separately under PF-02. The isolated correction is frozen at
`351ece79d9f4dad93888c74c8bdcc699dd4c8dac`; it is now published in SN release
`aeda6abbd2dc0abc92bb0f60975cf89b509e8017`. Verification during managed startup
remains pending. Its [qualification handoff](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/HANDOFF.md)
specifies 32 affected roots normally and under race, plus eight causal controls.
The initial normal and race runs each passed 31 roots and exposed one existing
cold-cache fixture mismatch. Fixture correction
`d52028de6864f7b1c48381fcb7652361d2520932` then passed the changed test and its
adjacent warm-cache control in both modes. Their recorded body, outer and join
exits are zero, with valid event sets and unchanged input checks. Retain the
other 30 passing roots per mode and the original causal result of two expected
failures and six passes. The [correction handoff](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/fixture-correction/HANDOFF.md)
defines that reuse; [normal evidence](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/body)
and [race evidence](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/race/body)
remain local qualification records. The candidate keeps both indexes local to
one invocation and leaves durable audit-cache authority intact.

Capture a consistent journal snapshot and index its applicable witnesses once.
Authenticate each distinct historical plan once into an immutable object scoped
to that reconciliation and its authority inputs. Keep per-receipt checks and
fresh operational observations. Add deterministic controls for multiple source
plans, ancestry, changed authority, appended journal entries, corrupt evidence,
cancellation and retry; measure operation counts rather than elapsed time.
Do not let reuse hide new state or turn a failed check into a passing result.

PF-02 follows the actual managed-start failure at 06:29:05 UTC on 2026-09-16.
Both operator taskworkers scheduled the full production backend workload.
Each emitted two geolocation certificate-pin rotation errors and one fiat
payment warning for a synthetic account without user authentication. These
six lines produced four blocking process-log classes. All 33 managed processes
then stopped; the release campaign did not start. See the retained chronology
in [FINAL-2.md](../sim-testnet/FINAL-2.md).

Add an explicit subnet-operator workload profile at the
[taskworker entry point](../../server/taskworker/run.go), retaining all tasks
required for operator service and subnet settlement. Restrict initial scheduling
and queue claims, including deferred post hooks; excluding a new schedule alone
does not handle unrelated jobs left in the retained database. Filter before
the claim limit so excluded rows cannot starve required work. Preserve those
rows, the ordinary production default, certificate checks and the process-log
gate. Bind the profile into both operators' launch and restart specifications.
Qualify scheduling, dispatch, retained queues, post hooks and defaults with
deterministic tests, then verify actual managed startup. Frozen SN `8e6d56b5`
and server `6752a8df` pass all 30 affected roots normally and under race, with
seven expected failures/five passes in the controls. The
[qualification evidence](../sim-testnet/peerreview/evidence/FINAL-2-preparation-fixes-20260916/README.md)
preserves the pre-test service refusals as well as successful bodies. Both
PF-01 and PF-02 are published in SN release `aeda6abbd2dc0abc92bb0f60975cf89b509e8017`;
server publication is `006e71b997db503604c4ef6bb0c2683dc0d984cd`, preserving the
qualified server commit `6752a8df246c0ee7e1c5a38cbd26b1e849b702ca` used by the
release. The matched executable has been built. Actual managed startup remains
pending, so both items remain in progress. Reuse the completed affected tests;
the remaining verification is operational startup and continuation of the
retained campaign.

PF-03 follows the continuation capture failure at 08:46:41 UTC on 2026-09-16:
operator-1-root nonce 106 had no signature in the collector's retained sources.
The complete census of both operator databases found 230 signed attempts,
including replacements and cancellations. Of those, 226 were already retained;
four original signed transactions were absent from the simulator's transaction
store. All four had successful canonical receipts, documented in the
[partial-start transaction evidence](../sim-testnet/peerreview/evidence/FINAL-2-startup-transactions-20260916/README.md).
The [sealed census and recovery evidence](../sim-testnet/peerreview/evidence/FINAL-2-signature-recovery-20260916/README.md)
records the complete comparison. At 08:57:55, create-only restoration added the
four original signatures while preserving all 2,268 existing RLP files and
the six watched state files. That operational repair submitted no transaction;
automatic collection remains proposed.

Unify the signature census used by continuation, renewal and recovery. Read
every signed attempt independently of its database status, deduplicate exact
hashes, and validate chain, recovered sender, nonce, destination, value and gas
envelope. Preserve distinct same-nonce replacements and cancellations, their
fee liabilities and original evidence. Unsigned intents are a separate class.
Retaining a signature does not authorize broadcasting it. Reconcile receipts
and nonce state before the owning production component retries an action;
missing evidence is not a reason to create a new logical action or rewrite
database status manually.

**2026-09-27 bounded production correction.** The operator account reconciler
discarded a receipt-read error when no receipt remained pending. It then
interpreted an advanced finalized nonce as unknown consumption and marked all
signed attempts `superseded`; subsequent scans excluded the entire intent.
The [qualified correction](evidence/operator-recovery-census-20260927.md)
keeps an incomplete census unresolved. The adjacent finality wait also
requires a complete census before offering a fee replacement. A known
canonical candidate can still resolve the nonce after another candidate's read
fails. The 19 affected test roots pass normally and with the race detector;
package vet and formatting checks pass. No historical status is rewritten
automatically, no signature is copied or broadcast by this fix, and complete
cross-store discovery remains open.

Use synthetic fixtures to reproduce the missing-store failure and cover both
operators, stale database statuses, multiple signatures for one nonce,
cancellations, malformed or conflicting records, partial export, idempotent
restart and an uncertain submission. Require complete nonce coverage, unchanged
spend limits and no duplicate execution. The current qualified collector's
supported restoration path remains available during implementation; replacing
the automated production replacement remains open under MG-03.

PF-04 follows the managed resume that ran 09:41:17–10:20:25 UTC on
2026-09-16. All 1,000 fleet checks and 4,673 carried-action checks completed.
The new generation reached 33 healthy processes with no restarts, but none of
the four validator/operator proof domains acquired a fresh completed trail.
The owned-node semantic readiness budget was five minutes. The command exited
one with `release topology semantic readiness timeout: every validator must
complete a fresh verified trail through every operator`, then stopped all
33 processes. The process-log gate recorded no findings through 10:20:10.
The saved plan, journal, configuration, public identities, executable and
release lock remained unchanged; the campaign did not start.

The [closed failure evidence](../sim-testnet/peerreview/evidence/FINAL-2-managed-readiness-20260916/README.md)
preserves all three failed exits and the watched-state comparison. Subsequent
shutdown diagnostics place both validators inside retained settlement-history
replay when the parent cancelled them. The current deadline depends on RPC
route, although that authenticated replay is needed with either route.
The bounded warmup correction is qualified at
`8270992eb8fb2b1599a29271ec44426379007306`. It gives
retained strict-history startup the existing 30-minute budget on every RPC
route. Adjacent review also found that an already-cancelled invocation could
admit an already-ready snapshot; the correction checks cancellation before
admission. All eleven affected roots pass normally and under race; separate
causal variants reproduce exactly one intended failure each, with the other
ten roots passing. The correction is integrated locally; deployment and actual
managed startup remain pending. That startup must demonstrate that the budget
suffices for this retained history.
The [closed qualification](../sim-testnet/peerreview/evidence/FINAL-2-retained-startup-qualification-20260916/README.md)
retains the affected results and original compiler-capture failures separately.

The timing review also identified a duplicate preparation pass between strict
resume and the separate campaign command. The explicit same-owner handoff,
`resume --then-release-candidate`, is qualified and integrated at
`e109ac35c5ea5ff5006040c2987118e99627e863`. It preserves all campaign checks and
the completed readiness results; failed or cancelled startup cannot enter the
campaign. All 14 affected roots pass normally and under race, with three
intended failures and three passes in the causal control. The
[qualification](../sim-testnet/peerreview/evidence/FINAL-2-resume-campaign-handoff-20260916/README.md)
retains original harness failures and identifies reused passing results.
The matching release is published at `1860261` and its executable has been
built. Subsequent setup completed all 4,673 carried checks but collected nine
runtime-admission errors after testnet advanced to 461. The
[closed preparation evidence](../sim-testnet/peerreview/evidence/FINAL-2-runtime461-preparation-20260916/README.md)
records that failure and the finalized observation. Actual managed startup and
campaign verification remain pending, so PF-04 is still in progress. This new
update is another concrete instance of RT-04's version-specific admission
problem; adding a reviewed 461 artifact alone will not complete the automatic
upgrade requirement.
The runtime-461 correction is qualified at `8edb3167`: 103 affected roots pass
normally and under race, with nine expected causal failures and nine controls.
It also repairs former-current460 companion history admission. See the
[qualification evidence](../sim-testnet/peerreview/evidence/FINAL-2-runtime461-qualification-20260916/README.md).
Deployment and actual managed startup remain pending; RT-04 and PF-04 are not
closed by this version-specific correction.

Make the warmup requirement and pending proof domains
observable, and distinguish recoverable incomplete startup from invalid
evidence. Assess the supported recovery path for retaining useful live work;
preserve signer ownership, approved budgets and authenticated history.
Add deterministic controls for delayed initialization, an actual initialization
failure, cancellation, partial domain progress, restart and stale proofs.
Fresh proofs from every required domain remain necessary for readiness, and
fully observed epochs remain necessary for final acceptance. Neither a larger
timeout nor healthy process endpoints alone closes this item.

## Runtime-upgrade compatibility proposal

Proposed design, 2026-09-16. Compatible chain upgrades should continue without
a subnet rebuild, plan migration, repeated funding, audit restart or lost soak
progress. The closed testnet evidence supplies regressions; this document
describes the subsequent production implementation.

## Why upgrades currently interrupt us

Current admission selects one compiled runtime artifact in
[crv4/reviewed_runtime.go](../crv4/reviewed_runtime.go), with separate consumers in
[validator/runtime_identity.go](../validator/runtime_identity.go),
[miner/fleet_runtime.go](../miner/fleet_runtime.go) and
[sim-testnet/runtime_identity.go](../sim-testnet/runtime_identity.go).
[runtime_config_identity.go](../sim-testnet/runtime_config_identity.go) also lists
specific permitted version transitions. Consequently a compatible chain
upgrade can require source edits, a release lock, a build and a new plan.

The [459-to-460 review](../docs/spec/runtime-460-audit.md) found unchanged interfaces
used by SN and a metadata change confined to the version constant. Runtime
staking internals nevertheless changed. That distinction matters: a metadata
comparison can establish encoding compatibility, but cannot prove all economic
behavior equivalent.

The SDK treats `spec_version` as a runtime specification identifier; even bug
fixes can change it. `transaction_version` describes dispatchable-call
compatibility under the SDK's versioning contract. Neither value alone proves
compatibility of every storage reader, custom runtime API or precompile we use.
[SDK runtime-version semantics](https://paritytech.github.io/polkadot-sdk/master/sp_version/struct.RuntimeVersion.html).

## Proposed operation model

Use a shared resolver in the existing native-chain layer. An operation asks
for the capabilities it needs and receives an immutable runtime view:

- Chain genesis, anchored block hash and observed runtime versions.
- Code and metadata hashes, with the actual metadata and supported encoders.
- The compatibility-policy revision and admitted operation profiles.
- The context's purpose: post-state reads, historical block execution, or
  construction of a new transaction.

Exact hashes remain evidence. Supported operation profiles determine whether
work can proceed. Do not mutate a connection-wide decoder while historical
readers or transaction constructors still use it. At upgrade boundaries,
distinguish the runtime executing a block from the runtime installed in its
post-state; historical events and extrinsics must use their execution context.

An unknown version number with an admitted profile proceeds automatically.
An unsupported capability produces a specific error and suspends its dependent
operations. Other capabilities continue where their dependencies permit it.
Keep completed actions and observed epochs; a missing required observation
still cannot be counted as a fully observed acceptance epoch.

## Compatibility checks

Check only the interfaces an operation consumes, using canonical structural
types rather than portable metadata type numbers or a hash of all metadata.
Unrelated pallets, documentation and version constants should not invalidate
an otherwise identical interface.

| Boundary | Required checks |
| --- | --- |
| Native calls | Call identity and argument order/types; resolve indices from admitted metadata where possible. |
| Storage | Keys, hashers, value types, optional/default behavior and relevant constants. |
| Signing | Extrinsic format and complete ordered signed-extension encoding and semantics supported by the signer. |
| Runtime APIs | Method/version and response encoding, including the selective metagraph API. |
| CRv4 | Prepared payload and source-commitment encoding, call indices, reveal rules and signing domain. |
| EVM integration | EVM chain identity, receipt/checkpoint mapping, required precompile interfaces and behavior. |
| Economic operations | Existing permission, fee, balance, collateral, reserve and scheduling bounds, plus action postconditions. |

Existing [CheckMetadata](../crv4/chain.go) needs stronger shape validation; storage
presence and an unknown extension having zero encoded size are insufficient
for automatic write admission. [preparedSourceEncoding](../crv4/source_commitment.go)
now separates schema-level signed-byte validation from its independently
authenticated source-call/signing capability. The
[selective-metagraph reader](../crv4/validator_stake.go) has the narrow read
profile below. Metadata format v14 alone does not establish the custom API's
return layout.

**2026-09-27 validator read capability (MG-04 / RT-01, RT-02).** The shared
stake reader previously rejected an independently approved exact artifact
solely because its spec was absent from a seven-version list. The
[correction and causal tests](evidence/validator-read-capability-20260927.md)
admit compatible successors through a block-bound read profile: exact consumed
storage types, hashers, defaults and prefix, plus the selective-metagraph API's
declared version and complete response decoding. Schedule reads additionally
require `SubnetEpochIndex` compatibility before reading any storage. Unrelated
calls, events and signed extensions do not revoke these read capabilities.
Exact historical pins and reviewed legacy adapters remain; the profile grants
no signing authority. Release configuration still requires separate production
admission, so this closes a redundant read gate without claiming RT-04 complete.

**2026-09-27 atomic source capability (MG-04 / RT-03, RT-04).** The
[source correction and causal controls](evidence/source-runtime-capability-20260927.md)
remove the next inner version-only refusal: an approved compatible successor
could not reconstruct its signed source bytes or construct its atomic call.
The persisted source schema now controls byte reconstruction; live use requires
an opaque exact-block artifact witness and matching selected calls plus complete
ordered signing extensions. The preparation block must match that witness;
retained-byte validation can use another authenticated block of the same
artifact without re-signing. Metadata eviction does not revoke the witness.
Unproved/foreign artifacts, incompatible calls/extensions, changed signature
domains and stripped provisional authority still fail at their actual admission
boundaries. This does not widen fleet or validator production configuration
approval or authorize new runtime tuples. The later [source-receipt
qualification](evidence/validator-source-runtime-qualification-20260929.md)
covers the standard validator's independently approved upgrade boundary;
wider native-operation and live upgrade qualification remain MG-04 work.

**2026-09-27 mainnet runtime observation (MG-04 / RT-04).** The
[outer admission correction](evidence/mainnet-runtime-observation-20260927.md)
accepts independently approved exact mainnet runtime identities through a
separate schema-2 config. Its bounded history pins each document's size/SHA,
revision, predecessor bytes, deployment/policy domain, source/review provenance
and finite native block interval. Successors append disjoint intervals;
spec-version ordering grants no authority. Reads check fresh native
name/genesis/EVM964, the approved route, canonical block/finality and the exact
artifact; config mutations and provisional connections fail. Existing producer,
bootstrap and archive loaders reject this observation config, and explicit key,
journal, startup-eligibility, signing and submission guards preserve that split.
This is a runtime identity observation API, not a storage/call compatibility
grant or a complete production successor policy. No actual mainnet approval,
identity or chain write was introduced; MG-04 and RT-04 remain open.

**2026-09-27 initial production successor (MG-04 / RT-04, MG-06).** The
[standard V2 producer transition](OWNER-RECYCLE-PRODUCTION.md) adds a separately
signed schema-3 production config, purpose-bound exact runtime admission and
finite original runtime history. Full measured provider proofs, native
owner/validator eligibility, canonical operator facts and the signed drained
activation block produce a 10/90 row. A distinct source hash and hotkey sidecar
bind that row to real CRv4 preparation, durable intent recovery and independently
observed archive replay. Observation-only configs/approvals remain unable to
send. [Producer qualification](evidence/owner-recycle-production-qualification-20260927.md)
and [runtime qualification](evidence/production-runtime-qualification-20260927.md)
record deterministic normal/race controls without live keys; actual native
economics remains a postcondition.
The subsequent [original-authority qualification](evidence/production-authority-history-qualification-20260928.md)
retains complete config/approval bundles for compatible independently approved
renewals. No mainnet approval, write or launch qualification is inferred.
MG-04/RT-04 and MG-06 remain open.

**2026-09-27 downstream production upload history (MG-04 / RT-04).** The
[read-only upload projection](VALIDATOR-UPLOAD-RUNTIME.md) requires an independent
content pin for the signed schema-3 configuration. It keeps exact signed runtime
intervals, route and deployment values after discarding the full config and its
producer capsule. Actual historical activation and current eligibility readers
now select their respective approved intervals, including current-window expiry.
Ordinary signed uploads cross the real server-used constructor, refresh and lease
in deterministic local fixtures. Three controls reproduce lost history, expired
current authority and an absent-field wire regression. No server writer, private
key access, live call or service activation is introduced. Original economic
authority verification, live trust and the remaining runtime consumers stay open.

**2026-09-28 durable original production authority (MG-04 / RT-04, MG-06).**
The signed config now selects bounded, content-addressed original config,
approval and runtime-document bundles with an exact signed predecessor prefix.
Source, intent, sidecar, capture and archive readers resolve their original
complete authority; current signing remains distinct. Receipt and application
readers use independently approved exact block windows, retaining uncertainty
as an error. Current renewal preserves the original drained activation and
first native epoch, so nonzero later pending rewards do not demand a restart.
Content-addressed fsync retention survives provisioning source loss without
overwriting original signatures. The upload observer copies only the validated
runtime windows. [Qualification](evidence/production-authority-history-qualification-20260928.md)
covers real signed proofs, actual native readers, cold capture, multiple renewals,
source loss, scope/substitution/conflict controls and exact predecessor-source
regressions. This continuity class holds policy, signer, operators, routes,
bounds and custody fixed. Automatic unknown-runtime approval, arbitrary policy
or custody migration, missing native epochs and live outcome remain separate;
no skipped interval becomes a successful native decision.

Automatic admission accepts upgrades authorized by the chain's governance
within these supported capabilities and operational bounds. It is not a proof
that arbitrary new runtime code preserves every economic rule. A semantic
change can retain its wire encoding; dry runs where available and ongoing
postcondition checks improve detection but do not eliminate that limitation.

## Upgrade and transaction handling

1. Observe an upgrade through the owned node and anchor the runtime context to
   a canonical block. A subscription is a notification; fetch matching version,
   code identity and metadata from the appropriate block context.
2. Validate the required profiles once per artifact and policy revision. Record
   the result and atomically make the context available to new operations.
3. Construct and sign a native transaction from the same immutable context,
   using its observed signing versions. Recheck the context before publishing
   signed bytes; handle an upgrade racing broadcast through reconciliation.
4. Preserve signed attempts. Establish inclusion, failure and nonce state before
   replacing a stale attempt. An uncertain submission must never trigger a
   blind repeat of a transfer or another non-idempotent action.
5. Append the new runtime observation and continue the existing campaign.

The SDK's `CheckSpecVersion` rejects transactions carrying an obsolete signing
version, so automatic compatibility cannot mean retaining stale signing data.
[SDK signing-version check](https://paritytech.github.io/polkadot-sdk/master/frame_system/struct.CheckSpecVersion.html).

In particular, [SubstrateManager](../sim-testnet/substrate.go) currently authenticates
a fresh head separately from the shared metadata used to construct calls.
`SendAsWithRecoveryPrecondition` accepts an already-built call. Change this
boundary so the admitted runtime view controls both construction and signing.
Widening the current allowlist alone would leave this gap.

Existing prepared CRv4 work retains its original bytes, runtime identity and
signatures. Any replacement is an explicit reconciled attempt; historical
proofs are never rewritten to name the newest runtime. EVM signatures use EVM
chain identity, so a native spec bump alone does not invalidate their bytes.
Relevant precompile and checkpoint behavior still requires admission.

## Stable identity, history and caches

Bind future deployment/configuration identity to chain identity, approved
economics and the compatibility policy. Store runtime observations separately
in the journal. A compatible observation should not change the plan hash,
activation identity, fleet leases, reserve-repair liabilities or approvals.
Every new signed artifact still records its own exact runtime and block
identity; the stable deployment policy does not replace its signing domain.
Existing signed manifests keep their original identity through an explicit
initial schema transition; do not reinterpret old hashes in place.

Historical reads select the applicable original artifact and adapter. Cache
their evidence with its block/artifact identity, verifier policy and actual
dependencies. Reuse a successful proof when those inputs are unchanged; a new
live runtime is not itself a reason to revalidate old facts. Continue checking
fresh balances, permits, fees and nonce state when an operation needs them.

The durable [historical audit cache](../sim-testnet/historical_audit_cache.go)
originally included plan, release and executable identity in every reuse key.
The 2026-09-21 [descendant-cache correction](../sim-testnet/historical_audit_descendant_cache.go)
admits compatible revisions for two immutable fleet proof kinds while retaining
authenticated authority, input and verifier dependencies. PH-05 below tracks
production adoption and the remaining scope. Eliminating routine rebuilds and
plan migrations also preserves existing exact-context hits; broader reuse still
requires proof that changed verifier and authority inputs invalidate affected
results.

The [metadata cache](../crv4/runtime_identity.go) now has a fixed 24-entry
resident bound independent of catalog length, evicts least-recently-used
completed entries, and authenticates without caching if all slots are loading.
Runtime discovery and admissible history no longer stop merely because more
versions have appeared. An evicted entry is loaded and authenticated again.
Measure the decoded-byte footprint and add a byte cap or durable
content-addressed artifacts if the measured bound requires them. Cache bounded
successful compatibility decisions; do not let transient RPC failures poison
admission. This cache correction alone does not grant production runtime
compatibility authority.

**2026-09-27 proof ownership correction.** The separate eight-entry provisional
metadata cache also acted as the authorization registry. Admitting another
compatible runtime could evict the artifact between authentication and binding,
or invalidate a retained source signature's independent runtime view. The
[qualified correction](evidence/runtime-proof-eviction-20260927.md) gives each
authenticated artifact and bound view an immutable, connection-owned proof.
It survives eviction without an unbounded authority registry. Reusing a held
view repeats exact block version, genesis, consumed API and code checks but
does not repeat metadata/profile validation or its durable observation.
Miner, validator, shared chain and simulator binding paths retain the proof;
strict bindings clear it. A new connection still authenticates against its
explicit authority and records its own observation. This fixes in-process
proof lifetime, not durable cross-process trust or production compatibility
admission; cold caches and restart do not inherit testnet authority.

## Delivery and acceptance

Implement immutable runtime views and shared profiles first, with comparison
against current admission during qualification. Then migrate live operations
and future plan identity to the policy model. An external reviewed artifact
catalog can remove recompilation as an interim step, but a manually maintained
catalog alone does not satisfy automatic upgrade handling.

Add deterministic synthetic tests following [CODESTYLE.md](../../connect/CODESTYLE.md):

- A higher spec version with compatible interfaces continues reads, signing and
  a running campaign without a new plan or repeated completed action.
- Unrelated metadata changes and type-number renumbering remain compatible.
- An upgrade between construction, signing and broadcast never mixes contexts
  or duplicates a submitted transaction, including uncertain outcomes.
- Upgrade-boundary and older-history decoding preserve their original runtime
  and signing domains while current work uses the new context.
- Changed consumed storage/call/API shapes and unsupported signed extensions
  suspend the affected operation with a precise reason.
- Semantic bounds still reject unsafe fee, permission, reserve or scheduling
  results even when their wire format is unchanged.
- Artifact eviction, RPC outage/reconnect and process restart retain valid
  progress and cannot turn stale or failed evidence into a passing result.

Astra (`gpt-6-astra`, effort `max`) owns all implementation, debugging and fixes; Sol
(`gpt-6-sol`, effort `medium`) runs affected tests normally and under race.
The final integration exercise upgrades a controlled runtime while the
subnet is active and demonstrates continued required observations, reconciled
transactions and unchanged approvals. It is a production qualification gate,
not a request to reopen testnet.

## Production hardening from sim-testnet

Reviewed 2026-09-21 by Astra (`gpt-6-astra`, effort `max`) against source through
SN `eb926565`, the [full finalization requirements](../FINALIZE.md),
[incremental recovery policy](../sim-testnet/README.md#incremental-recovery-and-acceptance),
[first report](../sim-testnet/FINAL.md),
[independent peer review](../sim-testnet/peerreview/verify/README.md), retained
failure bundles, and the corrective commits cited below. The
[September 17 handoff](../FINALIZE-HANDOFF.md) is historical evidence of a stopped
qualification, not the current execution instruction. The September 27 closure
and MG gates above supersede its instructions to continue testnet.

The recurring production risk is that an ordinary interruption can cross too
many ownership boundaries: an RPC failure invalidates startup, startup stops
healthy services, a patch changes approval/cache identity, and recovery repeats
history or financial preparation. Mainnet services must retain authenticated
progress, retry their own recoverable work, and suspend only operations whose
required safety conditions are unavailable. Passing final acceptance remains a
separate claim requiring complete evidence.

This section is a production implementation backlog. A committed simulator
repair is supporting evidence, not proof that the operator, miner, validator,
bootstrap or deployed mainnet path has the same protection. PH status denotes
remaining production work; the implementation inventory above records available
code without claiming production closure. Existing RT/RL/PF evidence is retained.
Do not copy testnet provisional flags or import the `sim-testnet` executable
into production. Extract required generic facilities into neutral packages and
qualify the production consumers described in [MAINNET.md](../mainnet/MAINNET.md#integration-with-this-repository).

### Priority, ownership and parallel delivery

`P0` protects funds, authority or required production liveness and must close
before mainnet activation. `P1` is required operational hardening before an
unattended mainnet launch; it can proceed alongside the P0 implementation.
These are production gates following the closed testnet effort. Astra authors and
reviews the implementation; Sol (`gpt-6-sol`, effort `medium`) runs the
affected tests and reports their exact failures to Astra max for debugging.
Preserve prior Terra receipts without repeating completed work for this model
change. The component column identifies the code
owner boundary, not an additional agent or approval requirement.

The status column below is the original backlog checkpoint. Use the [reconciled 38-row ledger](#reconciled-status-of-all-38-requirements--october-4) for current implementation and qualification status; the original outcomes and dependencies remain unchanged.

| ID | Priority | Production component and outcome | Existing work / dependencies | Historical status |
| --- | --- | --- | --- | --- |
| PH-01 | P0 | Operator, validators, bootstrap: durable recovery with independent audit and runtime owners | PF-01, PH-02 | Planned |
| PH-02 | P0 | Native/EVM submitters: one logical action, reconciled signed attempts and exact custody | RT-03, PF-03 | Planned |
| PH-03 | P0 | RPC, artifact and HTTP clients: bounded transient recovery without duplicate writes | PH-02 for submission recovery | Planned |
| PH-04 | P0 | Native-chain consumers: compatible upgrades and block-correct historical decoding | RT-01 through RT-08 | In progress |
| PH-05 | P1 | Historical verifiers: durable, dependency-bound successful proof reuse | RT-06, PF-01; PH-04 interfaces | Planned |
| PH-06 | P0 | Release/configuration tooling: explicit release identity and lossless plan migration | RL-01; PH-01, PH-02 | Planned |
| PH-07 | P0 | Service supervision: independent restart, single ownership and meaningful readiness | PF-02, PF-04; PH-01, PH-03 | Planned |
| PH-08 | P1 | Replay and workload scheduling: bounded work, memory and foreground latency | PF-01, PF-02; PH-05 | Planned |
| PH-09 | P1 | State and artifact storage: explicit durable volume, atomic publication and recovery | PH-01; storage adapter precedent | In progress — shared `6cd720cf`, native/snapshot/miner/monitor scoped independent gates pass; validator/root/bootstrap/blob author and independent 111-root normal/race scopes pass. Inventory-v3 57-root, CLI nine-root and constructor `71df099c` 59-root normal/race/vet scopes now pass independently and remain separate. The exclusive-writer correction is scoped independently; passive soft-outage continuation, complete composition, consumer module adoption, member census, preparation and production capacity/restore remain open |
| PH-10 | P0 | Epoch, fleet and evidence scheduling: resumable partial renewals and correct windows | PH-01, PH-02, PH-04 | Planned |
| PH-11 | P0 | Treasury and bootstrap: conserved lifetime spend, reserve and funding semantics | PH-02, PH-06 | Planned |
| PH-12 | P0 | Contracts, operator and claims: complete settlement conservation and authorization | PH-02, PH-04, PH-11 | Planned |
| PH-13 | P0 | Provider, operator and validator protocol: identity isolation and durable proof progress | PH-01, PH-03, PH-07 | Planned |
| PH-14 | P0 | Governance/bootstrap: actual capabilities, activated policy and both validator roles | RT-02; PH-10 through PH-12 | Planned |
| PH-15 | P0 | Status/operations: actionable failure classes, progress and evidence-based ETA | All runtime owners; PH-28 | Planned |
| PH-16 | P0 | Qualification and evidence: deterministic faults, composed coverage and independent replay | Every affected implementation | Planned |
| PH-17 | P0 | Plan-derived indexes: bind cached lookup structures to their immutable plan/generation owner | PH-01, PH-05, PH-06 | Planned |
| PH-18 | P0 | Strict readers: re-authorize connection/runtime provenance at every boundary after provisional work | PH-03, PH-04, PH-05 | Planned |
| PH-19 | P0 | Historical snapshots: use the reviewed historical runtime authority without weakening current writes | RT-01, RT-02, PH-18 | Planned |
| PH-20 | P0 | Relay capacity: distinguish funded slots, retained history, scan pages and resident bytes | PH-06, PH-09, PH-11 | Planned |
| PH-21 | P0 | Fault controller: bounded parallel, idempotent component control with durable partial recovery | PH-01, PH-03, PH-07, PH-10 | Planned |
| PH-22 | P0 | Service clients: retryable transport incidents, connection recovery and final error budgets | PH-03, PH-07, PH-13, PH-15 | Planned |
| PH-23 | P0 | Capacity revisions: bind funded slots, history horizon and every finite storage dimension | PH-06, PH-09, PH-11, PH-20 | Planned |
| PH-24 | P1 | Recovery performance: authenticate each retained plan once per immutable lineage | PH-01, PH-05, PH-17 | Planned |
| PH-25 | P1 | Supervisor lifecycle: explicit deployment stop joins every owned workload child | PH-01, PH-07, PH-21 | Planned |
| PH-26 | P1 | Large evidence transport: typed, cancellable public replay with finite admission | PH-03, PH-08, PH-09, PH-20, PH-23 | Planned |
| PH-27 | P0 | Policy activation: coherent validator evidence and client-key domains across operators | PF-05; PH-10, PH-13, PH-14 | In progress |
| PH-28 | P0 | Continuous operations: independent monitoring, bounded authorized repair and exercised on-call | PH-01/02/07/09/11/15/16; MG-01/02 | In progress |

Work in parallel on transaction/recovery (PH-01/02/06/11), chain access and
proofs (PH-03/04/05), service/storage (PH-07/08/09/13), and scheduling/economics
(PH-10/12/14). Agree on action, runtime-view and evidence identities first;
independent changes can then be integrated without rebuilding their consumers
repeatedly. PH-15 and PH-16 follow each change rather than waiting for a final
large cleanup. Mainnet economics and destructive UID operations retain the
specific unresolved choices and capability checks in MAINNET.md.

The [October 2 implementation backlog](evidence/mainnet-implementation-backlog-20261002.md)
maps every PH/MG row to current production boundaries and a concrete next
patch/test. A `Planned` row does not mean all corresponding code is absent:
selected retry, runtime-history, proof and control paths already exist, while
their complete production qualification remains open. The named source census
is read-only review evidence and does not add a passing test scope.

### PH-01 — Durable progress and separate audit/run ownership

**Lesson.** Setup served as deployment, historical audit, repair controller and
runtime launcher. A later read failure repeated already completed preparation.
The corrections include `31cfaf84` ([read-only audit](../sim-testnet/historical_audit_command.go)),
`fa8f84e4` (traffic independent of setup replay), `3541b3e0`
([durable accounted traffic](../sim-testnet/paid_traffic.go)) and `2269906e`
(provisional epoch completion distinct from strict acceptance).

**Production change.** Persist a dependency graph of logical actions and
per-component checkpoints. Commit each successful independent unit before
moving on. Let the run command resume the first pending unit and let a
read-only audit inspect a consistent immutable snapshot in parallel. Route an
audit-discovered repair through the existing transaction owner and a specific
repair action. A read-only audit cannot acquire a signer, mutate deployment
state or stop unrelated processes. Fresh authority, custody, chain identity,
spend, finality and value-conservation checks remain mandatory at the operation
that depends on them. Deferrable historical review remains visible until final
acceptance; invalid signatures or accounting never become soft failures.

**Closure.** Interrupt after every checkpoint, restart only one component, run
an audit concurrently, and inject a later audit failure. Completed actions and
observations must survive; independent traffic continues; only the invalidated
dependency is suspended. A required continuous epoch interrupted by the fault
must be reacquired with its dependent observations, without discarding earlier
valid phases or financial history. Verify that final acceptance cannot consume
a provisional, missing, canceled or failed result.

**2026-09-22 follow-up.** A successor relay plan accidentally restored an inline
public census despite provisional startup, so the campaign waited for hundreds
of historical publications after its local plan and debit checks had passed.
[The separate relay census](../sim-testnet/evidence_relay_public_audit.go)
retains bounded local manifest parsing, current chain/native authority and
original liabilities before startup. Its read-only worker owns copied source
and horizon state, reuses per-publication authenticated checkpoints, and must
finish successfully at the final gate. The transaction worker still verifies
each publication before sending. Production hardening must apply this separation
to successor plans as well as fresh deployments and service phase transitions
between individual replay items. Deterministic coverage must hold a real public
request open while proving release admission, then separately prove that failed,
canceled, missing or changed audit evidence cannot pass final acceptance.

**Process replacement follow-up.** A replacement driver previously spent its
startup budget reopening a signed interval owned by a dead process, then
invalidated that interval and required a second invocation to publish its
recovery. The [process recovery path](../sim-testnet/campaign_process_recovery.go)
now makes that decision before workers start. The exclusive deployment owner
appends a fresh signed interval under the phase lock, retaining original
observations, journal liabilities, deployment and authenticated fleet lifecycle.
Unstarted preparation keeps its checkpoint; a process gap cannot count toward
continuous acceptance. Qualify duplicate callers, interruption between
invalidation and publication, retained fleet evidence, successful/completed
sources, and read-only ownership before promoting the pattern to production.

**2026-09-28 production recovery follow-up (integrated and component qualified).** The composed
registration candidate exposed this coupling again in the actual public root.
`requireReleaseEvidenceV2Runtime` rebuilt the reserved-attempt replica census
before native intent reconciliation, and that census rejected an upload
session's canceled context. Withdrawing an admitted API session therefore
stopped otherwise valid retained native recovery after ten polling failures.
The invalid-reply and revocation regressions reproduced this in normal and
race execution of SN `9a5b2638`; the original captures remain in
`/mnt/data/sn-testnet/evidence/operator-registration-composed-20260928`.
Separate immutable configured-source/custody validation from permission to use
an active publication session. Keep the latter at new publication, preparation,
signing and rebroadcast boundaries. A revoked API must not erase native
liabilities; invalid retained authority or custody must still block recovery.

The [corrected production ownership](evidence/operator-withdrawal-ownership-qualification-20260928.md)
now passes all 20 affected roots normally and under race detection. Restoring
the old active-session gate and removing the API-local failure latch each
reproduce their intended failure in both modes on the corrected fixture.
Original signed intent, receipt recovery and active-write refusal remain
separate assertions. Preserve the parent's two real failures and unchanged
passing work; final dependency composition and deployment remain open.

The same tests used optional diagnostic delivery to release their receipt
fixtures. Bounded output may drop records, so that is not a reliable operation
barrier. Observe the real state transition through a nonblocking test hook,
retain the real HTTP/native operations, and assert diagnostic behavior
separately. Do not make logs part of transaction or recovery authority.

### PH-02 — Transaction idempotency, partial failure and custody

**Lesson.** Original signatures were missing from the simulator even though
their transactions finalized; superseded attempts, cancellations and partially
completed generations also escaped narrower recovery scans. See PF-03 and the
[signature census](../sim-testnet/peerreview/evidence/FINAL-2-signature-recovery-20260916/README.md),
`a2f0e12d` (carry finalized transactions), `0da3b1e1` (reconcile superseded spend
once) and `aa8f18e2` ([authenticate probe retirement](../sim-testnet/precompile_probe_retirement.go)).

**Production change.** Fsync logical intent and exact signed bytes before
broadcast; retain all original/replacement/cancellation attempts independently
of database status. Bind chain, signer, nonce, action, destination, value and
fee limits. Enforce one owner per signing/nonce domain across services,
including any native/EVM account aliasing. On uncertain submission, reconcile
the exact hash, canonical inclusion, successful dispatch and postcondition
before rebroadcasting or replacing. Record each batched child outcome. Retire
an unsubmitted action only with evidence that no signed/in-flight attempt
exists; an immutable deployed predecessor requires a proved successor, not
rewritten history. Evidence discovery alone never authorizes broadcasting.

**Immortal native liability (2026-09-28).** Current validator preparation signs
an immortal era. A local epoch crossing or approval deadline is not transaction
mortality. Keep the exact original bytes pending through an outage/restart;
before another signature, resolve them with a canonical receipt or prove a
foreign nonce consumption at the exact fully scanned boundary. A newer advertised
head may contain our own transaction and cannot supply a nonce against an older
absence scan. See the [continuation candidate](evidence/production-continuation-candidate-20260928.md);
its affected normal/race qualification is complete and does not grant old
decisions new epoch authority. Public startup and durable partial-scan recovery
retain their separate qualification scopes.

**Closure.** Inject crashes before/after intent fsync, signing, send, lost
response, inclusion, finality and postcondition publication. Cover two operators,
same-nonce replacements, cancellation, rejected dispatch, partial batches,
compacted journals and conflicting receipts. Assert at most one logical economic
effect, complete attempt/fee accounting, preserved original bytes and no
automatic nonce reset or duplicate deposit, stake, registration or claim.

**Current implementation boundary.** The [root action owner](ROOT-ACTION.md)
now reconstructs finalized native ancestry, body commitments, exact signed
bytes and phase-matched dispatch/fee evidence under an approved historical
execution profile. Its read-only port has no production signer or submitter;
the separate [approved submission port](ROOT-SUBMISSION.md) now owns actual
HTTP writes, immutable bytes and uncertain-attempt reconciliation, with no live
activation supplied by its qualification. In both adapters,
the owned RPC remains the finality/storage trust authority. Root UID and
registration generation are local approval context, not signed call arguments,
so pending seat changes require custody exclusion or incident reconciliation.

### PH-03 — Retry at the actual failing I/O boundary

**Lesson.** A healthy owned node still produced transport timeouts; repeatedly
sending a large archive batch exhausted its budget. `f57e8d46` added bounded
[EVM reads](../sim-testnet/evm_read_retry.go); `da67c494` split failed historical
batches. `114c9173`/`561ae3bc` addressed nested retry budgets; validator steering
and publication required their own corrections. The
[relay-stream incident](../sim-testnet/peerreview/evidence/FINAL-2-relay-stream-failure-20260916/README.md)
also shows that a successful later read does not establish the original stall's
root cause.

**Production change.** Inventory every direct native/EVM call, response-body
read, artifact upload/download, publication and readiness call. Apply a shared
typed error policy and a single end-to-end budget per logical operation, with
bounded attempts, cancellation, backoff and jitter. On an explicitly owned
unlimited RPC, retain zero request-quota pacing and no public fallback; bounded
in-flight work and recovery delay are still needed to avoid overload. Provider
rate responses must not impose a generic minutes-long cooldown on this route.
Split retryable failed batches, retain successful members, match response IDs
and pinned blocks, and keep all sub-batches inside their parent's deadline.
Do not multiply budgets through nested wrappers. Validate complete streamed
objects before publication; retry an idempotent object by its content hash.

Exhausted transient work becomes a persisted retryable operation with a next
attempt and alert. It must not kill unrelated services or count as success.
Cancellation, malformed data, wrong identity, permanent contract revert and
unavailable pruned history have distinct outcomes. Integrity errors containing
the word "timeout" remain integrity errors. Writes use PH-02 reconciliation,
not the read-retry loop.

**2026-10-03 payment-provider read inventory.** The financial transition review
found that the generic server GET helper's 60-second request timeout supplied
no retry loop. The separate candidate adds caller-owned retry budgets to actual
Circle transaction observation and Coinbase exchange-rate reads, but adjacent
Circle public-key, wallet-list and balance reads still require review. Classify
those read paths together; a successful transaction-read fix does not qualify
them. A composite wallet-list/balance operation must share one outer deadline,
rather than multiplying a 300-second budget by its wallet count. Keep identity
and complete-payload checks hard, close every response before retry, and never
apply the read loop to transfer creation. Frozen Server `5c93b812` now passes [12 independent normal/race roots and
controller vet](evidence/payment-get-retry-independent-20261003.json) for actual
Circle transaction and Coinbase rate reads. The tests use an injected clock for
65-second outage and 300-second budget scenarios; they do not establish a
wall-clock outage rehearsal. All five changed source files and module bytes
were compared with that exact Git source. The [retry-omission controls](evidence/payment-get-retry-causal-20261003.json)
now produce four intended failures and two permanent/hard positive controls
normally and under race, with no race reports. Adjacent wallet reads,
HTTP 500/Retry-After handling and final composition remain pending;
no POST retry or live deployment is claimed.

**2026-09-28 native HTTP cause preservation.** The pinned GSRPC HTTP client
flattened statuses to strings and returned decoder EOF without physical origin.
The [configured native-read adapter](evidence/native-http-read-causes-20260928.md)
now preserves typed status and incomplete-body causes before that flattening,
inside the existing 300-second total/60-second attempt budget. Complete malformed
JSON, permanent RPC errors, cancellation and mixed integrity
causes stay hard; writes never gain retry. All 30 selected roots passed normal
and race qualification, with ten intended causal failures retained.
The [production integration](evidence/production-native-http-integration-20260928.md)
consumes its strict physical-origin classifier without generic fallback for hard
native causes. It corrects the finite cap to include the existing 16 MiB event
field as hex plus 64 KiB of JSON framing, and distinguishes pure HTTP connection
failures during body close from local-file or mixed integrity failures. A retry
releases its body and discards idle connections first. The integration's genuine
configured-client deadline and close controls passed all 23 selected roots
normally and under race, with six intended control failures retained. This
transport slice alone does not close PH-03.

**2026-09-28 production steering review.** The standard validator's outer
steering loop tied several transient continuation branches to testnet
provisional permissions, which production disables. A receipt timeout outside
an explicitly classified recovery path could spend the hard-failure budget;
a later epoch change could reject the unfinished epoch before reconciliation.
The original-config pending branch is now covered by
the [authority-history qualification](evidence/production-authority-history-qualification-20260928.md).
The separately qualified continuation below covers the affected actual steering
owners. Public startup and durable partial scans now have separate component
qualification below; first-client recovery and final release composition keep
PH-03 open.
Preserve unknown or missed
outcomes and original signed bytes, keep unrelated workers running, and require
reconciliation before another send. A successful retry never manufactures a
missed emission interval. Review repeated finalized-block scans for reuse of
authenticated completed prefixes so retries do not perpetually repeat the same
history.

**Receipt coverage boundary.** A successful null or partial block response is
unknown evidence, not proof that a signed transaction is absent. Admission must
authenticate the complete canonical header and ordered extrinsics commitment;
known commitment layouts do not grant new runtime execution authority. Retry
unavailable evidence while retaining the exact signed attempt, and distinguish
it from a proved identity or commitment contradiction. Receipt absence, nonce
consumption and mortality must use the same authenticated finalized coverage
boundary. Reading a newer nonce after an older scan can falsely attribute our
transaction's inclusion to another transaction. Extend the scan before making
that inference, and never advance a reusable prefix on an incomplete read.
The shared complete-body admission and miner cursor correction are now
[qualified and integrated](evidence/receipt-recovery-qualification-20260928.md).
The production waits and durable partial prefixes are qualified separately below.

The same review found the miner fleet's native recovery cursor accepted an
explicit empty/truncated extrinsics vector without authenticating its body
commitment. Reuse the shared complete-body reader before advancing that cursor.
Bind new absence checkpoints to a semantic proof version and the existing
signed attempt; old checkpoints without that proof may require one rescan from
their original start. Preserve signed bytes, allowances and finalized outcomes.
Do not invalidate a qualified prefix merely because the executable changed.
Exercise the actual miner restart path, including a runtime-update digest,
beside the validator recovery tests.

The continuation review also found `VerifyFinalizedSourceContext` attaching a
canonicality/finality contradiction to a failed read before any contradictory
value was returned, then fetching the receipt body again to derive its event
index. Return the I/O cause first and reuse the admitted body/index for exact
dispatch and source-event checks. Miner scan ranges currently publish their
cursor only after up to 4,096 blocks: add bounded durable subranges so a late
timeout cannot repeatedly discard thousands of verified reads. Reuse the
header authenticated with each complete body instead of issuing duplicate
header/hash reads, while retaining canonical boundary checks and bounded fsync
work. The following prefix qualification covers these continuation improvements.

**2026-09-28 bounded prefix recovery (component qualified).** The
[shared chunk/checkpoint slice](evidence/receipt-prefix-qualification-20260928.md)
adds at most 128 fully authenticated bodies per chunk and preserves completed
chunks plus admitted partial prefixes before retrying a later unavailable read.
Miner recovery keeps its existing signed
`ScanProof` v1 and 4,096-block command bound. Validator recovery writes one small,
domain-signed disposable checkpoint tied to the exact original intent/config,
transaction and contiguous canonical range, without rewriting intent history.
Missing, empty or stale cache state requires a rescan, never a fresh launch or
signature; executable changes do not affect its key. Individual chunk reads keep
the 60-second attempt/300-second total budgets. Deterministic actual-owner
deadline barriers exercise progress within one interrupted chunk. An incomplete
body cannot advance coverage, and the saved prefix is rejoined to a canonical
parent before reuse. Optional cache read/save failures disable disk reuse and
emit one closed degradation observation; original custody and authority errors
remain hard. Memory-only progress never claims a successful durable write, and
the separately qualified public bounded diagnostic exporter exposes degradation.
Historical-only original approvals now reach same-boundary nonce reconciliation
before the no-rebroadcast wait. All 56 affected roots have scoped normal/race
passes after retained fixture corrections; nine causal families reached their
intended assertions in both modes and package vet passes. The receipt records
original failures, disqualified diagnostic captures and the incomplete
pre-execution seal of one fixture scope. Final release composition, automatic
runtime approval and mainnet activation remain open.

The new contract installer review found the same failed-read/contradiction join
in its native-to-EVM mapping check, plus admission that required the finalized
head to remain identical throughout preparation. Return failed reads before
evaluating values. Normal finalized-head advancement must not invalidate an
otherwise authorized original transaction or require restarting its phase.
Keep historical checks pinned, verify ancestry, and refresh only affected
current runtime/nonce/custody inputs within the existing bounded operation.
Test a head advancing during real HTTP readback; the chain must not need to
stand still for bootstrap to complete. Real identity, authority or ancestry
changes remain distinct from ordinary progress.

The [qualified 2026-09-28 continuation](evidence/production-continuation-candidate-20260928.md)
adds a production-only outer `Run` branch that reaches durable intent custody
before current scheduling, plus current-config receipt/application waits and
same-boundary nonce observation. Its actual nonempty owner fixtures use genuine
M8 work under a separately signed zero-price policy; they do not claim paid
capture or economic acceptance. Typed waits preserve original intent bytes and
age, expose operational unavailability, and do not spend the service's hard-error
budget. Real mixed integrity/custody failures and cancellation stay distinct.
All 10 CRv4 and 22 validator roots have normal/race coverage, preserving the
original race timeout and its six-root completion separately. All six control
families reached their intended assertions in both modes. Full fresh
`RunRelease` activation/config/dual-upload composition has its separate results
below. Historical-only foreign-nonce resolution and authenticated durable scan
chunks (including miner partial ranges) have the separate prefix qualification
above. The native HTTP integration is separately qualified;
no callback-only loop test closes the remaining physical ownership requirements.

The integrated [public startup continuation](evidence/production-startup-continuation-candidate-20260928.md)
adds the actual public-root composition with signed historical activation,
concrete disk/intent owners and real dual-operator sessions. Original liabilities
are reconciled before current UID/stake/preparation; fresh work waits for the
current eligibility and settlement-publication owner. Its tests distinguish
retained nonempty recovery from empty durable stores with already provisioned
client identities. All original 67 validator roots now have passing scoped
normal/race coverage, plus two handler regressions and four CRv4 roots. Five
causal controls reproduce their intended failures in both modes; validator vet
passes. Original failed packages, cleanup interruptions and a corrected checker
literal remain retained. The capture fenced source and module paths but omitted
a contemporaneous physical dependency-content seal; final release composition
remains open. Local semantic reconstruction must
not restart merely because the independent remote-read budget elapses. Parallel
native and EVM transient errors remain independently classified, while a hard
native physical subtree cannot be unwrapped into a retryable leaf.

Still open: deployment and live readiness of the operator API dependency and
first/missing-client JWT recovery. Registration can mutate identity and must
retain ambiguous outcomes; wrapping the whole operator constructor in read
retries or transferring a canceled startup context to its service is unsafe.

The [operator registration candidate](OPERATOR-REGISTRATION.md) now implements
the separate mutation owner: persistent opaque request plus first-send anchor,
atomic server allocation/dedup and stable-scope tombstone, exact replay and
durable credential handoff. The actual public root constructs local evidence
owners before its independent authentication worker, so retained receipt and
application observation can continue while first/legacy client recovery waits.
New publication, trails, fresh signing and rebroadcast still require readiness.
Signed `allow_client_registration` defaults false and preserves older omitted/
false configuration hashes; it authorizes one new operation, never renewal or
revocation bypass. Existing requests replay without that flag. Shared SDK
refresh rejects null/duplicate/mixed responses before both startup and background
callers; complete bad responses do not become success or confirmed logout.
The original [handoff](evidence/operator-registration-candidate-20260928.md)
is followed by the qualified [withdrawal correction](evidence/operator-withdrawal-ownership-qualification-20260928.md)
and [real API/database fixture](evidence/registration-production-api-qualification-20260928.md).
Final combined SN consumers remain under qualification. The additive DB migration and every approved operator's
versioned route must deploy before fresh clients; legacy missing identities
cannot be guessed from an empty native history. Public server-key and immutable
evidence availability remain separate startup inputs.
Native production writers continue to require explicit WS/WSS, independently of
the owned node's HTTP EVM/read capabilities. Helper-only fixture success did not
prove public config admission: the real-root diagnostics caught WS capability,
normalized signed-config representation, complete capacity relationships and
private scratch namespace assumptions; those corrections do not weaken gates.

**Registration transport follow-up (2026-09-28; component qualified).**
A mutation retry must preserve the reviewed destination as well as the original
request identity. Automatic redirects could otherwise send registration to a
different path or origin. Scope redirect refusal to the versioned mutation,
retain ordinary request behavior, and enforce limits on the encoded body after
escaping. Inspect physical error causes outside shared locks with finite
depth/node bounds; a mixed hard cause must not disappear behind a timeout.
Nil headers, browser request copies, incomplete replies, cyclic error graphs
and callbacks that block are adjacent cases. The
[transport qualification](evidence/registration-request-transport-qualification-20260928.md)
covers 47 roots plus 32 legacy descendants and seven causal roots in both
modes. Real-browser execution is not claimed. The separate actual API/DB
fixture proves lost-commit-response replay through the production route;
neither result supplies production rollout or live identity authority.

The bounded [source-finality read candidate](evidence/source-finality-read-candidate-20260928.md)
separates physical read errors from finality/schedule contradictions and reuses
one admitted body, index and event vector for dispatch and source proof. Missing
receipt wire data remains typed unknown evidence; complete contradictory data
and dispatch failures remain hard errors. The paired
[adjacent-read candidate](evidence/production-read-cause-adjacency-candidate-20260928.md)
applies the same rule to real miner recovery, owner census/eligibility and
activation setup reads. Their [combined qualification is complete](evidence/source-read-cause-qualification-20260928.md),
including normal/race causal controls and the corrected source-role fixture.
That read component does not replace the separate continuation qualification
above or the remaining composed-release and first-client startup work.

Full startup review found the same ordering defect above the steering loop:
[`runReleaseWithStartupAndProgressV2`](../validator/release_run.go) previously
dialed current native authority and requested a fresh EVM snapshot, UID and stake
before opening the historical activation, disk and intent owners. Correcting `Run` or
`submitOnceV2` alone cannot recover a process restart through that barrier.
Separate preparation-only prerequisites from historical custody recovery. Open
authenticated retained state first where its original authority permits it;
unavailable current reads must leave an observable recovery wait and preserve
independent monitoring. Current chain identity, capability and custody still
gate new signing. Require actual fresh-start and restart tests through the
public lifecycle, including activation, both upload owners and original receipt
reconciliation; callback-only loop tests do not close this requirement.

The startup composition exposed two adjacent retry defects. Parallel native and
EVM checks can both fail transiently, but applying the native-only classifier
to their entire joined error makes an ordinary EVM HTTP failure terminal.
Classify independent branches at their owning boundary, retaining each native
subtree's hard verdict for local, integrity or permanent failures. Also keep
the 300-second I/O retry budget separate from total semantic recovery: valid
local M8/history replay may take longer. An I/O timer must not repeatedly
discard and restart that work. Local reconstruction follows caller cancellation
and retained checkpoints; actual network operations retain finite retry owners.
Both corrections now have public-startup component qualification above.

**Closure.** Deterministically inject disconnect, DNS/HTTP failures, timeout
during body read, missing/reordered batch responses, partial success and a
large-batch refusal that succeeds when split. Verify exact call/attempt bounds,
shared deadlines, cancellation joins, preserved successes, correct permanent
classification and eventual continuation after a network outage. Exercise the
real caller layers, including both validator paths and the artifact reader.

**2026-09-23 release-interval follow-up.** The live RPC consistency actor
opened fresh native-chain readers for each sample, repeatedly decoding runtime
metadata and discarding an authenticated cache. Several sequential reads then
inherited the nearly exhausted 10-second sample deadline and were reported as
RPC timeouts even while direct LAN reads were fast. Production readers should
reuse an owner-scoped chain client and bounded immutable metadata cache,
authenticate each pinned block and runtime identity, and retry transport reads
inside one measured sample budget. Test the complete multi-call sample under
slow metadata and a one-call timeout; a fast isolated RPC probe is insufficient.

**2026-09-22 cancellation follow-up.** Generation 24 reached its signed
acceptance scope but a normal client-canceled immutable download became a
blocking process warning. The production artifact handler now distinguishes a
request-owned cancellation from deadline, integrity, storage and write errors;
a partial body still aborts. The simulator recognizes only the exact legacy
handler diagnostic and retains any subsequent joined failure across polls and
restarts. Interrupted runs label pending, never-triggered faults and unexercised
vectors as consequences of the recorded stop while retaining the failed final
verdict. Regression coverage exercises the actual handler, persisted scanner,
and a post-boundary scenario through later lifecycle and terminal snapshots.

The adjacent signed client-key observation path now retries an interrupted
HTTP read once with the same nonce and pinned decision. That retry and the
existing smaller-batch admission fallback share the two-reservation ceiling;
they cannot multiply quota. Complete response/body-close ownership precedes
retry, existing immutable capture slots are authenticated on recovery, and
signature, identity, quota and storage failures stay hard. A missing transport
response no longer adds a false signer-mismatch verdict: exhausted transient
reads remain eligible for the existing in-process steering continuation. Tests
discard a real signed response, authenticate its retry, reuse exact durable
captures without a live session, and continue the real compact-head collector
after a timeout into the next native epoch.

### PH-04 — Runtime changes and historical archive compatibility

**Lesson.** Repeated version-specific admission fixes for 455/458/459/460/461
and later runtimes blocked execution even when consumed interfaces were
compatible. Some historical reads incorrectly demanded the live artifact.
The [runtime/config migration evidence](../sim-testnet/peerreview/evidence/FINAL-2-runtime-config-identity-20260915/README.md)
preserves the original signing identity rather than relabeling it.

**Qualified validator source receipts (2026-09-29).** The standard production
validator previously rebound a retained source to the inclusion block's
post-state runtime, rejecting an unchanged signature when that block installed
an approved successor. The [integrated correction and scoped evidence](evidence/validator-source-runtime-qualification-20260929.md)
keep original signed preparation, parent execution and post-state decoding
separate. All 103 selected roots pass normal and race qualification; restoring
the old post-state call reproduces the intended signing-authority mismatch in
both modes. Exact signed runtime windows and consumed interface checks still
reject unsupported or unapproved artifacts. Historical views grant no current
signing authority. The complete-header receipt path covers an upgrade digest;
other SDK current-head readers, both validator roles, automatic admission and
live upgrade qualification remain open.

The [qualified test-style follow-up](evidence/validator-source-runtime-qualification-20260929.md)
keeps all seven receipt roots top-level and replaces the digest subtests with
a plain loop. Those seven roots pass normal/race and validator vet passes;
both digest variants still fail the isolated old-post-state control.
Production bytes are unchanged.

**Current admission correction (2026-10-01).** The
[first increment](evidence/current-native-header-authority-20261001.md) at
`889f5c29` authenticates producer/fleet coordinates. Independent review found
upload admission could still backdate committed block 250 to signed-window
height 150. The [adjacent successor](evidence/current-native-header-adjacent-authority-20261001.md)
at `30354d78` extends complete-header and canonical continuity checks across
upload and signed observation windows, startup/activation evidence, application
journals and the underlying identity/checkpoint/schedule/nonce/receipt readers.
It also closes the separate finalized witness before binding a historical
production runtime; failed or canceled reads leave the prior view unchanged.
Original signing, parent execution and post-state remain separate. Qualification
is local and source-fenced. The [claim clock correction](evidence/miner-claim-evm-finality-20261001.md)
at `6dcb94a1` uses the EVM finalized identity for claim state/replay and closes
the actual finalized tag and original canonical coordinates before publication.
It preserves pending bytes across restart and rechecks fresh submission receipts.
The [shared onchain successor](evidence/shared-evm-finality-closure-20261001.md)
at `222e45a8` closes that send helper's canonical/finalized witnesses and retries
transient or absent evidence under the original deadline. Both deployed
validator roles, automatic semantic successor proof and live upgrade acceptance
remain open. The [bootstrap finality baseline](evidence/release-28ebfced-serverac86-20261001.md)
packages this later source at exact SN `28ebfced`; the SN `689938d6` and
`233ea2be` images retain their earlier scope. Local construction does not supply
independent release qualification or live acceptance.

**Production change.** Deliver RT-01 through RT-08 across miner, operator, both
validator roles and bootstrap. Construct and sign from one immutable runtime
view; validate consumed call/storage/API/precompile/signing capabilities.
Separate block execution from post-state context at upgrade boundaries. Store
observed runtime versions as evidence, separate from stable deployment policy.
Historical reads bind genesis, block hash, original runtime/metadata and decoder
version. An archive-capability refusal identifies the missing proof and blocks
only dependent work; never substitute a current-state read for a historical
one. Keep artifact caches bounded independently of the number of known versions.
Authenticate the complete finalized header against its announced hash, including
parent, roots and digest, before using that hash to select runtime or storage.
Decode the complete runtime version through one shared parser; when an RPC
supplies both `stateVersion` and `systemVersion`, contradictory values are an
integrity error. An RPC's same-height hash lookup and matching version numbers
alone do not prove the header or state layout.

**Closure.** Use the RT-08 controlled upgrade plus historical reads on both
sides of the upgrade, concurrent signing, stale subscriptions, evicted metadata,
wrong genesis and pruned-state responses. A compatible update requires no
manual version entry or repeated funding; an incompatible consumed interface
halts that operation with a precise capability error. An ABI match alone does
not establish unchanged economic semantics.

**Current observation boundary.** `sn-mainnet runtime-snapshot` authenticates
the complete finalized SCALE header, retains raw `:code` and metadata at that
hash, confirms code bytes against `state_getStorageHash`, compares the complete
runtime tuple across reads, and repeats canonical/genesis/EVM/chain checks.
It accepts unfamiliar metadata bytes for review instead of pretending an old
decoder authorizes them. Unknown digest variants require an explicit profile
review. `inspect`/`monitor` check the complete runtime tuple internally but
their v1 JSON still exposes only spec/transaction numbers; serialize the full
tuple in a separately versioned observer artifact before treating that output
as independently auditable runtime identity. The [latest Snow sample](evidence/runtime-snapshot-header-auth-snow-20260927.json)
is testnet runtime 471; raw observation is not mainnet runtime admission or a
verified source-to-Wasm mapping.

The owned Snow testnet also exposes a Frontier `fron` digest in finalized
native headers. At six sampled historical/current heights, its leading EVM
hash matched the EVM RPC, while payload lengths differed. The signer-free
[`finalized-mapping` command](FINALIZED-MAPPING.md) now decodes the reviewed
SCALE/PostLog variants, authenticates exact raw EVM RLP by the digest-derived
hash and corroborates EVM canonicality using the RLP-decoded number. The
[Snow mapping observation](evidence/finalized-mapping-snow-20260927.json)
retains both headers and independently reproduced hashes. The newer
[`finalized-snapshot` command](FINALIZED-SNAPSHOT.md) captures those headers
and exact runtime bytes under one selected native hash; its [Snow observation](evidence/finalized-snapshot-snow-20260927.json)
reproduces all four hashes in one record. Finality remains an owned-RPC
assertion and runtime source is not proven. Repeat with independently approved
mainnet identity after route cutover; no testnet mapping approves it.

**Repair admission follow-up (2026-09-22).** Fleet renewal still demanded a
static runtime pin after continuation and diagnostics had authenticated the
same compatible successor. Use one retained-evidence authority model across
read-only planning, repair apply and readiness. The simulator now shares
[the approval source selector](../sim-testnet/provisional_continuation.go):
readers bind the active approval; setup and fleet repair bind an immutable
reviewed successor before activating it. Exact journal/source reconstruction,
custody, signing-domain, current capability and budget checks remain mandatory.
Do not promote a provisional observation into release acceptance.

Production should express these authorities as an evidence dependency ledger:
each durable proof names its immutable inputs, output digest, verifier version
and invalidation scope. Commands consume the same proof authority; they must
not independently invent stricter or weaker versions of it. Invalidate only
proofs dependent on changed code/metadata, chain, custody, policy, intent or
economic observations, preserving unrelated finalized work. Qualify the full
planning → reviewed successor → pre-apply readiness → partial apply → resume
sequence through a compatible runtime update and changed recovery executable,
including missing/altered archive bytes and a journal that advanced outside the
repair. This simulator correction is a regression pattern for RT-08, not proof
that production consumers already implement the ledger.

### PH-05 — Reusable proofs with explicit invalidation

**Lesson.** Executable, release and plan changes invalidated otherwise identical
historical proofs. `67c614f4` added dependency-bound reuse for exactly two fleet
proof kinds in [historical_audit_descendant_cache.go](../sim-testnet/historical_audit_descendant_cache.go).
Earlier fixes indexed journals and authenticated source plans once per
reconciliation; they did not authorize reuse of arbitrary current state.

**Production change.** Cache completed immutable proof units by full consumed
input: chain/checkpoint, action/receipt, target/calldata, expected decoder result,
verifier version, authority/observer profile and authenticated approval lineage.
Store provenance inside the authenticated envelope, write atomically, and
invalidate only changed dependencies. Recheck canonical/finalized identity and
local evidence as required. Re-read live nonce, balance, permit, fee, reserve
and lease observations when their operation needs them. Indexes are lookup
hints, not authority. Retain successful groups when a later group fails; do
not cache transient failures as successful decisions or share failed singleflight
results indefinitely. Legacy opaque entries lacking provenance need their exact
original context or one fresh validation; they cannot be guessed compatible.

**Closure.** A compatible hotfix and process restart perform zero repeated
immutable value calls while still making required freshness checks. Changed
calldata, code/decoder, verifier, policy, observer, signature, receipt or lineage
must invalidate the affected proof. Cover partial two-observer completion,
interruption, tampering, reorg, read-only mode, concurrent consumers and legacy
entry migration. Measure work counts, not a convenient warm-cache runtime.

**2026-09-22 path-proof follow-up.** The first scenario observation after a
driver replacement reverified roughly 550 MB of validator path proofs because
its prefix cache survived only in memory. The
[durable prefix store](../sim-testnet/scenario_path_proof_store.go) authenticates
each complete-record byte cut, SHA-256, verifier/key identity, count and unique
trail census. It checkpoints successful chunks even when a later record fails,
rehashes the source before reuse, and verifies only the appended suffix. A
changed verifier requires full validation; a changed trusted prefix remains an
integrity failure. Production consumers also need fixed snapshot cuts, bounded
line allocation, read-only cache access and atomic publication without letting
concurrent appends extend one observation indefinitely. Final semantic evidence
continues to authenticate the original proof records independently of this cache.

**2026-09-22 recovery-plan follow-up.** A read-only CPU profile found that cold
recovery validation decoded and rehashed the same large archived plans for each
signed generation, even though envelope reads had their own lookup. Share one
[authenticated plan lookup](../sim-testnet/campaign_plan_lookup.go) across root
succession, signed envelopes, approval edges and source reconstruction. Preserve
the exact raw-byte digest, all lineage and custody checks, and a bounded retained
size. Fence each reuse and the final return with directory/file identity and
change-time witnesses; replacement, truncation and same-size writes must fail.
Unavailable metadata or an exhausted memory budget requires the full reader.
Emit progress after each authenticated generation. Production qualification must
count full decodes per distinct approval and force mutations during validation,
so a warm envelope cache cannot conceal repeated work in adjacent readers.

### PH-06 — Release, plan and rendered configuration identity

**Lesson.** Publishing reports invalidated a qualified executable (RL-01),
budget/runtime revisions lost retained custody, and old render receipts were
treated as proof of new configuration. The
[render-convergence failure](../sim-testnet/peerreview/evidence/FINAL-2-render-convergence-20260914/README.md)
also exposed a direct plain-WebSocket route that violated the server's
transport policy.

**Production change.** Approve an immutable release manifest covering executable,
source/dependencies, contract artifacts, schema, policy and security inputs.
Distinguish it from the current branch tip and reporting files. A successor
plan records its predecessor and exact future-action diff, reuses completed
compatible actions and preserves original signatures, limits and custody.
Version rendered configuration by the fields it consumes: route, authority,
schema, service profile and identity. Converge changed local outputs explicitly;
do not overwrite an old receipt or mutate a running service's signed context.
Allow address/path relocation only when its actual identity and security
implications are reconciled. Retain transport authentication requirements.

**Closure.** Cover documentation-only commits, budget-only revisions, approved
runtime transitions, path relocation, stopped/running services, lost render
output, changed endpoint and unauthorized release drift. Unaffected progress
survives; changed executable, policy, contracts or authority cannot borrow an
unrelated approval. Exercise restart on the admitted release while main advances.

**Current implementation boundary.** Offline `source-lock` records clean SN
and local replacement Git heads, module hashes, Go toolchain and executable
hash. The [composed candidate](evidence/source-lock-composed-20260927.json)
pins SN `f321ba7c`, server `9f860731` and Connect `c68689c4`. It is an input
to a release manifest, not a lock of generated artifacts, signed policy,
Solidity bytecode, configuration, images or approved rollout identity.

### PH-07 — Process ownership, dependency recovery and readiness

**Lesson.** A taskworker log finding stopped all 33 processes; historical replay
consumed the five-minute readiness budget; replaced executable paths prevented
graceful shutdown; Docker restarts stranded dependencies. PF-02/PF-04 and
`6105e22e` ([dependency recovery](../sim-testnet/supervisor_dependency_recovery.go))
address parts of these failures.

**Production change.** Supervise each long-lived service and its dependencies
with explicit ownership, stable process identity and bounded restart/backoff.
Use PID start identity, executable identity and owned process group/cgroup;
a pathname or stale lock alone cannot prove a process is alive or safe to kill.
Dependency recovery must preserve volumes, identities and deployment state.
Keep one writer across restart and handoff. Separate liveness, replay/warmup,
semantic readiness and acceptance health. Retain healthy workers when one
recovers; wait for a canceled owner's children before replacement. A saturated
restart budget surfaces an actionable degraded state, not a green endpoint.

**2026-09-28 closure follow-up.** The closed testnet campaign left its supervisor
and 31 workers running after both validators stopped. The original `stop`
command then refused unrelated current launch settings before reaching its
shutdown handler. The verified service owner completed a graceful shutdown;
the [retained observation](evidence/closed-testnet-service-stop-20260928.md)
records both outcomes. Campaign closure must explicitly retain or stop each
owned service, with a reason and owner. Dispatch stop/drain from authenticated
retained process identity independently of new-launch configuration and live
RPC availability. Preserve journals and volumes; prove old workers exited
before admitting a replacement. This operational cleanup does not establish
that the production shutdown path is qualified.

**Closure.** Kill an operator or validator independently, restart an owned
database/object-store container, lose the observer connection, rotate the
executable path and simulate PID reuse. Verify no duplicate signer/sidecar,
no orphan process, no unexpected volume recreation and joined shutdown.
Include invalid replacement configuration and an unavailable RPC during stop;
neither may prevent terminating the exact already-owned process generation.
Delayed replay must expose progress; both UR validators must eventually produce
fresh verified trails through every required operator. The root validator's
readiness is its own netuid-0 role, never a substitute for a second UR validator.

### PH-08 — Bound replay and background workload

**Lesson.** Journal validation became repeated full scans; fleet history repeated
source authentication; whole-fleet fixtures performed unnecessary durable
writes; a path-proof reader followed a growing file indefinitely. Evidence:
[44,048-row journal regression](../sim-testnet/peerreview/evidence/FINAL-2-journal-recovery-20260916/README.md),
`91274acc`, `cbf15c3b`, `2c968635` and `74192404`.

**Production change.** Index each consistent journal snapshot once, authenticate
each distinct historical plan once per reconciliation, and bound reads by the
snapshot's initial length. Append-only growth is processed in a later segment.
Use bounded queues, byte/memory limits, RPC concurrency and cancellation-aware
joins. Keep background audits from starving signing, proofs, settlement and
claims. Give subnet operator taskworkers an explicit workload profile; filter
unrelated retained queue rows before claim limits and preserve their post hooks
for the correct worker. Preserve ordinary production defaults. Measure actual
provider/session memory and honor configured capacity instead of hiding leaks
with higher limits or disabling admission.

**Closure.** Assert linear/bounded operation counts under a representative
retained journal and fleet. Force concurrent append, cold cache, queue pressure,
slow archive, provider memory pressure and both operator workloads. Required
foreground work must progress within its deadline, excluded tasks remain
untouched, and cancellation joins without leaked buffers or goroutines. Fixture
optimizations retain at least one representative full integration path.

### PH-09 — Durable storage and usable test/build storage

**In progress, October 2.** The [source checkpoint](evidence/server-and-storage-progress-20261002.md#durable-storage-source-work) preserves three causal old-source failures: fleet and claim stores recreated missing state, and a checkpoint publisher wrote into a replaced parent. The [native/snapshot/miner checkpoint](evidence/durable-owner-custody-qualification-20261002.md) now has separate author/independent scopes, monitor also passes its independent 114-root normal/race gate, while inventory-v3 and the [nine-root public CLI successor](evidence/durable-inventory-cli-qualification-20261002.md) now have separate author and independent scopes; the [observer continuation and exclusive-writer checkpoint](evidence/durable-observer-continuation-progress-20261002.md) keeps separate author/independent scopes, with complete composition still open. The [validator/root/bootstrap/local-blob increment](evidence/durable-adopter-qualification-20261002.md) passes 111 author and independent normal/race roots and four-package vet against explicit Connect `6cd720cf` declarations; actual service-credential inspection remains a separately passing author scope. Original member loss, rollback and uncertain writes are covered at the named public owners. The remaining immutable-member/steering-history audit, offline preparation, broader fixture migration and complete source integration remain open. None of these local scopes proves remote PostgreSQL/Redis/MinIO or production restoration.

**Recovery classification.** A failed kernel observation or exhausted write reserve refuses the current admission but is not evidence of a changed identity. Retry only the affected owner against the same retained declaration and custody. Observed replacement, missing retained descendants, aliases or lost protection permanently invalidate that owner; join it before reopening the approved volume. A refusal before publication leaves the checkpoint unchanged. Once a write or rename may have happened, missing acknowledgement is uncertain publication: retain candidate and completed bytes, reopen/reconcile the authenticated checkpoint, and never infer rollback or reset the campaign.

**Lesson.** Root-volume pressure and scratch/cache placement delayed or stopped
qualification. `a5c23b39` and `2f9ef2b3` introduced data-volume workspaces and
the [storage adapter](../scripts/test-storage.sh).
On 2026-09-27 at 07:21 UTC the test-data USB SSD disconnected during isolated
mainnet-gate work. The kernel aborted its ext4 journal and `/mnt/data`
disappeared; the device returned under a different `/dev/sd*` name. Recovery
used the configured filesystem UUID, `e2fsck -p` journal replay (reported
clean), and remount; the isolated uncommitted worktree reappeared. This is a
storage-availability incident, not proof that every in-flight write survived.

**Production change.** Configure durable journal/database/artifact storage
separately from disposable scratch and build caches. Check the intended mount,
permissions, free bytes/inodes and I/O health; a missing mount must not silently
write to the root filesystem. Retain temporary/private directory permissions
without changing published artifact modes. Publish state with file fsync,
atomic rename and directory durability; preserve a verifiable prior version.
Back up signed journals, keys and evidence with separate access policies and
test restoration. Relocate old data only with ownership checks and preserved
live paths; paths themselves are not cryptographic identity. Production volume
selection is deployment configuration, not a hardcoded testnet USB path.
The Snow xops guard in branch `codex/mainnet-subtensor-mount-guard-20260927`
(commits `ca49e00`, `ec443da`) requires the configured data mount before either
playbook inspects or creates a node generation, and at each systemd start. It
also rejects a finney cutover retaining testnet chain ID, genesis or bootnode,
and renders the reviewed `/ws` bootnode form for both containers; the monitor
helper accepts that form. The 36 affected Python tests and both Ansible syntax
checks passed. The branch is not deployed and its checked-in chain selection
still targets testnet. During the operator's data move, verify the actual mount
path and volume UUID before applying that branch; then switch chain, genesis,
runtime, bootnode and reference pins together for mainnet.

**Closure.** Exercise missing mount, read-only/full volume, inode exhaustion,
device disconnect/re-enumeration, journal replay, partial write, crash
before/after rename, cache loss and restored backups. Check the mount by UUID
and filesystem identity before any write after recovery; rehash uncommitted
artifacts and rerun interrupted tests rather than treating directory
reappearance as completed work.
Recover the last authenticated checkpoint without losing a signed attempt or
marking incomplete publication complete. Release/test entry points propagate
selected scratch/cache paths to children and remain usable in isolated CI.

### PH-10 — Epoch boundaries, leases and partial renewal

**Lesson.** Expiring preparation windows repeatedly triggered renewal; forecast
end blocks were confused with minimum waiting periods; compacted and partially
activated fleet generations failed replay. `ed768df3`, `aedb4e74` and
[fleet_renewal_deadline.go](../sim-testnet/fleet_renewal_deadline.go) cover recent
recovery boundaries.

**Production change.** Model policy activation, evidence capacity horizon,
native epoch, settlement epoch, lease validity and claim expiry separately.
Choose fresh execution boundaries after slow preparation/import; derive them
from finalized chain state. Preserve finalized children of a renewal and
reconcile installed, pending, effective, expired and superseded generations
before signing remaining work. Recover compacted history through authenticated
witnesses. Parallelize independent fleet work within nonce/resource ownership.
Keep retention capacity sufficient for startup margin and the full required
window; a forecast end is not a reason to wait until that block to start.

**Concurrent signer follow-up (2026-09-22).** A fleet renewal repeatedly refused
approval because independent root publishers advanced their nonces. Admission
must bind exact nonce state to the transaction owners of the repair, then
reconcile bounded progress of other signers without changing custody, signed
liabilities or the approval hash. Unconfirmed observations may settle or leave
the pool; finalized history may not regress. The simulator now applies this
distinction to renewal checkpoints. Production closure also requires one owner
per actual signing stream and recovery of persisted signed bytes before any
retry; it does not permit silently changing an approved transaction nonce.

**Closure.** Move the finalized head across activation while part of a fleet is
renewed; interrupt and compact midway; delay import past a planned boundary.
Resume without double renewal, lost original lease proof or unauthorized fresh
funding. Assert that acceptance counts actual complete policy epochs and that
claim/commit/reveal deadlines are never inferred from stale wall-clock ETA.
Advance an unrelated signer between approval and apply, then prove the same
approval succeeds without signing or broadcasting twice. Keep changed renewal
signers, custody, liabilities, missing roles and unbounded observations hard.

Also cross the receipt/pool publication boundaries deterministically: finalize
the original transaction between its first receipt lookup and nonce read, lose
an accepted submission response, and delay the preceding pipeline nonce in the
pool. Reconcile the exact hash and persisted bytes under finite read/broadcast
budgets. Missing receipt plus advanced nonce is unresolved observation until
canonical evidence identifies the winning transaction; it is not proof that a
different transaction won. Never sign a replacement nonce to clear that gap.

### PH-11 — Budgets, reserve targets and native funding behavior

**Lesson.** Software changes retriggered a 65% reserve repair despite a valid
prior repair and a live share above the 60% operating floor. Lifetime increases
inflated future campaign allocations (`eceac4aa`), and successor renewals needed
funding reconciliation (`9b874e34`, `19b400ac`). The probe incorrectly transferred
value to a precompile whose staking path debited the caller's native balance
(`e604a8d1`). See the [reserve refusal](../sim-testnet/peerreview/evidence/FINAL-2-release-reserve-recovery-20260916/README.md).

**Production change.** Keep one cumulative ledger across releases and plan
lineage: paid fees/principal, signed outstanding liabilities, reservations,
replacements and remaining authorization. Distinguish EVM wei, TAO rao and
alpha units with checked integer arithmetic. Raising a lifetime ceiling does
not automatically expand each action allocation. Separate the live operating
floor, a repair target at its pinned execution block and any required terminal
target; preserve successful repairs while checking the current floor. Determine
transfer/stake source, value semantics, fees and actual credited amount from
the admitted precompile/runtime behavior, including dual native/EVM views of
one account. Do not assume a successful outer call funded the intended party.

**Closure.** Prove conservation through repeated budget revisions, partial
repairs, superseded signatures, nonce cancellation and renewal successors.
Exercise reserve rounding just below the target, below the floor, insufficient
native balance despite EVM balance, and precompile revert/partial behavior.
Only actual finalized balance/event/postcondition evidence releases liability.
Testnet automatic allowance approval is not a production spending policy;
mainnet uses its own explicitly configured limits, signers and custody rules.

### PH-12 — Settlement, carry and claims remain explainable end to end

**Lesson.** Epoch 309 captured zero but paid carried epoch-308 funds; the first
report omitted `RootMissed(308)`. All 16 payments and 8 alpha-rao of rounding
residue reproduced independently. Shared operator JWTs initially selected the
wrong provider wallet. The first report also records a NetEscrow cross-store
ordering race as a production limitation; detection is not evidence of a fix.
See [peer-review conformance facts](../sim-testnet/peerreview/verify/content.py)
and [claim receipts](../sim-testnet/peerreview/evidence/epoch309-paid-claims-20260912.json).

**Production change.** Trace captured emission, per-operator carry, entitlement,
Merkle root, claim, payment, outstanding liability and residue with exact units
and epoch identities. Use provider-specific authorization for claims and verify
the entitled client independently of a shared network credential. Preserve
zero-entitlement, deferred-payment, missed-root and expired-claim outcomes as
different states. Authenticate the coordinator-authorized commitment and its
artifact hash; recovering an artifact signer is not proof that signer had the
on-chain root role. Review and resolve the known cross-store ordering defect
with its actual production owner, or retain it as an explicit launch blocker;
monitor alerts alone do not close it. Preserve non-upgradeable custody and
already finalized claims across coordinator changes.

**Closure.** Reproduce missing roots followed by carry into the next epoch,
cross-operator isolation, floor division/dust, retry after payment uncertainty,
claim expiry and mismatched provider credentials. Force the NetEscrow ordering
race at the observable store boundary and prove repaired state convergence.
Independently rebuild leaves/root and every payment amount; vault conservation
must hold at each pinned transition, including zero-current-capture payments.

**Current implementation boundary.** The [claim-recovery correction](../evm/CLAIM-RECOVERY.md)
atomically rolls back a failed runtime payment while preserving an accepted
leaf and provider credit, and the receipt verifier accepts the actual
zero-based deferral enum. The non-upgradeable vault requires a new deployment;
its nested EVM/native rollback and exact stake deltas must be rehearsed against
the authenticated production runtime. Existing vaults are not patched.

### PH-13 — Protocol identity and proof/traffic continuity

**Lesson.** Stale measurement cuts, skipped settlement rounds, client-key
history deadlines and terminal-publication failures repeatedly stopped
validators while other services continued. Actual transport ACK volume did
not by itself establish eligible usage or a payable root; the shortened run
contained both positive settlement and a separate zero-usage epoch.

**Production change.** Bind every evidence, client-key history and publication
path to its operator, provider, validator, generation and epoch domain. Reconcile
late/stale messages against their own lineage before changing current state.
Resume verified history without fabricating missed measurements or applied
weights. Persist publication progress and retry content-addressed writes.
Preserve canonical serialized bytes, including signed framing/newline rules.
Use bounded flow control with clear buffer/goroutine ownership and cancellation
through SDK/operator/provider boundaries. Keep traffic ownership/accounting
durable while controllers or observers restart. Retain the safety differences
between testnet provisional gap handling and admissible mainnet history.

**Provider registration follow-up (2026-09-29; qualified and integrated).** The
[provider registration change](evidence/provider-client-registration-candidate-20260929.md)
replaces first-client allocation in `provide`/`auth-provide` with the existing
versioned request protocol. One retained seed owns all direct/proxy slots;
key/request publication precedes POST, replay retains the original operation,
and required registration/refresh/logout custody stays on that owner's physical
directory. New allocation needs explicit permission. First-upgrade legacy-key
adoption is a separate operator assertion, not a key-to-JWT proof or permission
to replace a lost identity. The source is qualified and integrated: 114 selected
roots pass per normal/race mode (228 executions), and all 17 causal variants
are valid in both modes (34 executions). This is 114/2324 package roots, not
full miner/validator coverage. The sealed e32 fixture-failure receipt remains
separate from child qualification. Public daemon fixtures stop at authenticated
handoff, with refresh/logout tested separately, so full serving, processed-key
and proof readiness remain open. The no-config measurement validator's durable
primary identity has its separate qualification below; this provider slice
cannot claim all role startup paths are repaired. Approved live
API deployment, actual custody, native/contract admission, economic acceptance
and independent operational monitoring remain external gates. PH-13 stays open.

**Measurement primary-client follow-up (2026-09-29; qualified and integrated).**
The [no-config validator change](evidence/validator-measurement-client-registration-candidate-20260929.md)
removes the legacy allocator from durable primary `.validator.jwt` startup. Its
closed measurement/direct scope always uses `allowCreate=false`, with original
key custody, explicit first legacy adoption or exact retained-operation replay;
missing unowned identity stays a recovery refusal. Refresh validates the original
identity and persists before publication. Actual API/transport/measurement users
join before key ownership ends, and successful replay releases only the shared
bootstrap lock. Parent `c3fe0cf2` remains unqualified after its process-exiting
docopt CLI fixture aborted both focused validator packages; its 62-file anomaly
receipt is preserved. Test-only parser child `e33f64d4` keeps production bytes
unchanged. All 37 selected roots pass normally and under race detection (74
executions, twelve package PASS/exit-zero streams), and all 38 causal executions
reach their intended assertions. Independent audits verify the sealed receipt,
exact source, ten local modules and eight physical roots; integration changes
only documentation above the qualified source. No full-package coverage or live
authority is inferred. The final-save fixture excludes an earlier periodic
snapshot at a real worker-join barrier. Existing ephemeral
tunnel-client allocation and separate proof/stats history recovery are unchanged;
seed discovery and local shutdown do not establish completed trails or live
readiness. Mainnet identity, deployment/custody, contract/Safe authority,
native/economic outcomes and independent monitoring gates remain open.

**Closure.** Inject stale generations, delayed proofs, mixed operator keys,
skipped rounds, partial artifact uploads, backpressure and controller restarts.
Test both normal and replay/fast paths at the layer where identity is consumed.
Require fresh proof progress for every validator/operator domain and connect
traffic to eligible usage, signed roots, native rows and paid entitlement;
bytes acknowledged or a healthy process alone cannot satisfy that chain.

**2026-09-23 reconnect follow-up.** Concurrent old/new Connect sessions can
share a reverse egress key. An old session's cleanup must compare its lease
owner before deletion, so it cannot remove the newer session's live mapping
and produce a synthetic verification hop. Cover reconnect overlap, stale TTL
expiry, proxy/direct handoff and replayed multi-hop verification in deterministic
tests. Retain bounded response diagnostics that identify a rejected verification
step without logging secrets.

**2026-09-23 fault-selection follow-up.** A verification probe selected miners
that a scheduled quality fault had deliberately disabled, then treated the
expected missing source lease as a protocol failure. Resolve the exact logical
miner before probe selection, exclude active fault targets and guard a signed
walk against a fault starting mid-request. Continue to reject wrong source,
signature and response content for every request actually issued; fault scope
must not become a blanket waiver for an entire swarm or operator.

### PH-14 — Governed limits and real on-chain activation

**Lesson.** The first run configured a future production policy but never
scheduled it: on-chain cadence stayed 300/50/150/5 instead of 360/60/180/6.
`max_allowed_validators=64` exceeded the design target of at most 56, and reserve
was 61.449% against its 65% target. These are the peer review's three explicit
findings, not arithmetic/test errors to suppress.

**Production change.** Make policy transitions durable scheduled actions and
prove their effective chain state. Read actual limits and authority; expose
root/governance-only changes and adapt the design to supported constraints.
A retained 64-validator exception must state its capacity consequence rather
than pretend the target was met. Follow MAINNET.md for literal UID-reset
capability, protected identities, actual contract/custody installation,
**10% of native miner allocation**, the user-selected **90% owner-recycle**
policy (no reserve credit), and **both
the netuid-0 root and UR subnet validator roles**. Scaling all weights or theta
alone cannot implement the 10% requirement. Recheck role/permit/registration
eligibility and operating reserve at execution, using approved semantics.

**Closure.** Preserve the unmet testnet requirement for three fully observed
accelerated production-policy epochs. Mainnet uses MAINNET.md's own acceptance
scope: at least three complete native emission intervals and a full mainnet UR
settlement/claim cycle, with configured intent distinguished from actual cadence.
Verify hyperparameters and reserve at pinned blocks and report unresolved
exceptions plainly. Mainnet activation additionally proves the chosen reset
capability, 10% denominator/rounding, actual reward outcome and both validator
roles. Test stale plans, unauthorized calls, competing registrations and policy
activation races without silently substituting a narrower reset or reward goal.

**Current implementation boundary.** The [signed owner-recycle admission](../validator/OWNER-RECYCLE-ADMISSION.md)
checks an independently pinned approval, immutable retained bytes and finalized
runtime/owner/Recycle-mode census. Those observation formats remain fenced.
The distinct [production successor](OWNER-RECYCLE-PRODUCTION.md) now carries
independent authority through measured proofs, native preparation, signed
sidecars, durable intents and archive observation in the standard V2 runtime.
It still needs actual production activation/API/key/history inputs and live
approval. An admitted weight row alone does not establish a 10% native outcome;
that outcome is a measured postcondition. Migration of old sidecars to a new
approved production config remains separate unresolved work.

### PH-17 — Bind derived indexes to the exact plan and generation

**Lesson.** During archived-plan recovery, executor copies could retain an
action index built for the current plan. A lookup after the copy switched to a
historical plan could therefore return an action authorized by the wrong plan.
The sim-testnet correction in `701f4456` makes the index owner explicit and
falls back to the copied plan when it differs; its causal control returned a
synthetic current-plan target under the prior implementation.

**Production change.** Every derived index, cache, iterator, batched-work map
and dependency resolver must carry the immutable plan hash and generation that
created it. At each read, verify object identity as well as content shape. A
copied/recovered executor must either reuse an index owned by its exact plan or
rebuild from its own authenticated source. Treat an index as an acceleration
only: it cannot supply authority, dependency order, approval scope or a target
that the bound immutable plan does not contain.

**Closure.** Clone each production reader across current, archived, successor,
cancelled and repaired plans; poison the original index and prove the clone
selects only the target/dependencies from its own plan. Cover concurrent index
publication, restart, eviction and plan migration. Include this in PH-05 proof
reuse and PH-06 migration qualification, with normal and race tests at every
consumer boundary.

### PH-18 — Re-authorize strict readers after provisional work

**Lesson.** A connection that was acceptable for provisional recovery could
otherwise retain its compatibility authority when later reused by a strict
reader. `701f4456` added an explicit strict-after-provisional fence and a
causal regression; connection reuse alone does not establish that the strict
runtime catalogue, metadata and capability decision were rechecked.

**Production change.** Model connection transport, observed chain state,
runtime/metadata catalogue, verification mode and approval lineage as separate
capabilities. Every strict reader must request and validate a fresh strict
capability at its own boundary, including after connection pooling, process
restart, runtime update, handoff and provisional repair. A provisional result
can be retained as labeled evidence but cannot populate a strict cache or
authorize strict historical decoding, signing, settlement, governance or final
acceptance. Invalidate/re-observe the relevant identity whenever the pinned
block, runtime, endpoint/peer, decoder or policy changes.

**Closure.** Reuse one pooled connection across provisional and strict readers,
then inject a changed runtime catalogue, metadata hash, peer identity and
unsupported capability. Prove strict work rejects the provisional authority,
performs its own pinned observation and leaves unrelated provisional traffic
running. Exercise both validators, miner/operator clients, bootstrap and
archive replay under normal and race qualification.

### PH-15 — Operational status that explains forward progress

**Lesson.** Repeated "hours remaining" estimates obscured whether the runtime
was producing transactions, replaying old evidence or waiting for a future
boundary. An observer timeout was also easy to confuse with a stopped owner.

**Production change.** Publish per-component owner identity, last successful
checkpoint/block, current action, attempts, retry-after, queue/backlog, proof
domains and blocking dependency. Use explicit classes: retryable transport,
deferred audit, pending finality, recoverable service, integrity/authorization
failure, accounting failure and acceptance failure. Emit one durable incident
with recurrence counters, retaining original errors and resolution evidence.
Expose real submitted/finalized transaction counts and workload/proof progress.
ETA separates observed preparation throughput, chain-block duration and unknown
repair time; update it from finalized block progress and measured cadence.

**Closure.** During injected outage and live recovery, status must identify the
same surviving owner, its pending operation and next retry. No live-process
claim comes solely from a lock/state file. A completed soft-error recovery
remains in the incident ledger for the improvement batch; missing required
evidence remains visible in acceptance. Verify meaningful signals under both
slow but progressing replay and an actual deadlock.

**2026-09-28 optional cause-classification follow-up (integrated and component qualified).**
Bounded output queues alone do not isolate a callback if it first invokes an
arbitrary error's `Unwrap`, `Is` or `As` method to choose a diagnostic label.
The miner, trail and release-read diagnostic classifiers contained this
coupling. Read only concrete owned or standard-library error fields with
finite traversal; classify custom and opaque wrappers as unknown without
calling their methods. Retain the original error for its required custody or
retry owner, whose decision policy remains separate. Required cancellation
must not wait for an optional diagnostic offer. Root scalar events and the
shared output queue do not traverse producer errors and are outside this
correction. The [20-root correction and five causal controls](evidence/diagnostic-cause-isolation-qualification-20260928.md)
pass normally and under race detection, including blocking-method barriers,
typed-nil and cyclic wrappers and actual authentication/file callbacks. Opaque
wrappers report `unknown`; no diagnostic cause grants retry authority. Final
combined dependency checks and deployment remain separate work, without
restarting the frozen output qualification.

**2026-09-23 release-heartbeat follow-up.** R31 entered the real release epoch
and then stopped because a heartbeat treated process-log findings as a reason
to terminate before the terminal acceptance block. Production monitoring must
persist classified findings and keep the interval running; the final gate still
rejects unresolved findings. Only evidence-integrity or authorization failures
should stop the heartbeat itself. Attribute a fault-related log to the exact
logical client and its authenticated event-time fault window, since a buffered
line may be scanned only after the fault has been restored. The affected swarm
process may remain healthy while one miner is intentionally disabled. Test both
the continued run and strict terminal rejection of an unrelated error.

**R44 evidence-detail follow-up.** Ten acceptance-scoped `exit-gap-timeout`
findings (14 events) came from the old Connect receiver's bare timeout line.
It did not retain the expected sequence, queued range or handoff state, so the
later closed-hole and ready-rendezvous fixes cannot prove which historical
timeouts they repair. Production gap incidents must include those bounded
sequence and ownership fields, the exact retry/expiry deadline, and whether
the hole was closed by an admitted packet or remains genuinely unresolved.
Keep real missing-packet expiry and incomplete steering continuity visible at
final acceptance. A normal websocket close and a compact artifact read timeout
need bounded retry with the same evidence identity; cancellation from an
intentional owner stop remains a distinct outcome. Test both repaired transient
paths and a true unresolved gap, then verify the live log carries enough detail
to attribute a recurrence without guessing from the error class alone.

### PH-28 — Continuous monitoring and authorized repair

**October 3 operational renewal review.** The [source review](evidence/monitor-policy-renewal-review-20261003.json) binds four current Git blobs and finds that claim/provider checkpoint admission hashes the entire declaration. Operational freshness changes and reviewed evolving expectations therefore need explicit continuation rather than a replacement checkpoint. The new continuous EVM candidate has the adjacent read-budget/history/batch/stall pattern; The frozen first EVM candidate now has acknowledged monotonic settings; it retains only the latest review provenance, so a separate successor must preserve bounded append-only revision history. Keep immutable chain/contract/member/pool/source authority separate from operational settings without removing authentication. Bind predecessor revisions, retain unresolved obligations and incident/cursor history, publish acknowledgement durably, and refuse shrinking resources or replacing identity without its own reviewed transition. Test actual old-checkpoint reopen and restart after accepted revision. Source review and implementation intent are not behavioral acceptance. The same bound provider/claim sources use one five-second HTTP GET per sample and classify HTTP 408/429 as invalid. Add one owned total retry budget of at least sixty seconds, preferably three hundred, with cancellation, bounded backoff and body-close joining. Keep authentication and observed identity failures permanent; retrying must not fabricate current progress.

The [one-shot stopped-validator capability](VALIDATOR-REPAIR.md) now has a
concrete fixed systemctl action, independently signed expiry/release/unit/boot
and generation authority, existing incident binding, permanent one-start custody
and generation/source postconditions. [Offline qualification](evidence/validator-repair-qualification-20260930.md)
is sealed for `af570cdc`: 52 positive normal/race executions passed, with twelve
normal and seven selected race controls causal. Genuine cgroup-v2 admission and
the post-sync expiry/sample-age gate retain the one-start boundary. Crashes before
durable start acknowledgement remain explicitly consumed and uncertain; no
automatic repeat can restore the allowance. Deployment and trusted exclusive
host service control, actual systemd rehearsal, alert delivery, production active
hang recovery, operator/root roles, initial activation and monetary repairs remain
open. The separate [active-hang increment](ACTIVE-VALIDATOR-REPAIR.md) supplies a
bounded independently signed stop/join/start interface and retained generation
custody; it does not approve or rehearse production service mutation.

**Production change.** Implement the [mainnet operating model](MAINNET.md#continuous-monitoring-and-repair)
as three separate owners: a signer-free finalized-chain monitor, bounded service
supervisors, and a repair controller that consumes an approved action envelope.
The monitor must survive stopped application processes and failed repairs. It
compares authenticated state at the same finalized native/EVM checkpoints on
the owned node and a separately provisioned canonical source, and retains raw
evidence independently of the transaction owner's claims. Until that second
source exists, report `independent_rpc=false`; another process reading Snow's
same backend is not node independence.

The [operator journal increment](OPERATOR-MONITOR.md) is independently qualified
offline on exact SN `070ec769` / server `99130c2d` sources. It observes the existing
transaction/attempt, publication and scan journals through one read-only database
snapshot, even while the operator process is stopped. Retained incident history
does not turn these database assertions into canonical receipts, complete
liabilities, provider/client-key evidence or current deadline admission. Its
[qualification receipt](evidence/operator-monitor-qualification-20260930.md)
keeps the unobserved domains and deployment/alert-delivery gates explicit.

Publish bounded metrics and structured incident records to the existing xops
Grafana/Mimir/Loki and host Fluent Bit infrastructure, with the established
exporter and credential boundaries. See [deployment infrastructure](../../xops/main/ansible/playbook-dbs.yml)
and [telemetry isolation](../../xops/VULNSCAN2.md). SN needs domain-specific
dashboards and alerts, not a second logging service. Telemetry has no signing,
database-mutation or full-host control authority. A separate dead-man alert
detects loss of the monitor and of alert delivery.

| Gate | Required operating evidence |
| --- | --- |
| SLOs and alert classification | Before activation, bind poll intervals, freshness/deadline margins, finite retry budgets, severity and primary/backup on-call in the operations manifest. Starting targets in MAINNET.md are proposed values, not measured availability. Distinguish unavailable reads, actual identity/integrity mismatches, missing progress, deadline risk and accounting failures. |
| Complete chain and application view | Prove current node/runtime identity; native/EVM finality mapping; both UR validators' permit, source and applied/revealed rows; root seat/delegation; every operator's policy/client-key/proof domain; usage, deposit, capture, root, carry, claim and liability accounting; release/config drift and pending signed attempts. HTTP 200, process liveness and acknowledged traffic are insufficient. |
| Automatic actions within authority | Reconnect/retry exact idempotent reads, resume bounded authenticated replay, restart the same approved service after joining its old owner, and reconcile already signed transactions. Any rebroadcast/replacement or scheduled renewal must be explicitly authorized, expiring and capped, with one writer and retained original bytes. A monitor finding alone is never permission to spend. |
| Operator-gated actions | Changed policy, runtime capability, contract/custody, endpoint authority, release, schema, native-history boundary, allowance, stake, UID reset or new registration requires its exact reviewable action and appropriate signer authority. No blanket autonomous repair, hidden funding or direct historical SQL rewrite. |
| Repair correctness | Append/fsync intent and signed attempt before effects; reconcile canonical receipts, dispatch, postcondition and nonce before retry; enforce lifetime/per-action/count/deadline caps across restart. Verify the result with the independent monitor and preserve unresolved or rejected repairs as incidents. |
| Incident and deployment discipline | Retain source/config/plan and process identities, pinned blocks, raw responses, log byte ranges, debt and liabilities, actions and terminal receipts. Reproduce and fix the observed cause, qualify affected/adjacent paths, canary a composed release, and check state-format compatibility before rollback. Finalized transactions and database migrations require a forward recovery plan where rollback is unsafe. |
| On-call rehearsal | Deliver a real test alert to designated operators, exercise primary/backup escalation and the stop/reconcile/recover/verify runbook, and show that no custody or claim obligation is discarded when a component stops. Preserve the incident timeline and independent recovery evidence. |

**Closure.** On the exact production image, inject RPC loss and wrong-chain
responses separately; stop the monitor, alert path, an operator and a validator;
stall a signed transaction; miss a native or policy boundary; exhaust log/disk
capacity; and create an accounting mismatch. Read outages must remain unknown
observations, known integrity failures must stop dependent signing, and neither
may create a successful acceptance sample. Prove alert delivery and durable
single-owner recovery within the selected SLOs. Review the repair allowlist,
caps and on-call roster before bounded activation; promote to unattended
operation only with the required live observations and no open critical
incidents. Record recurrence and near-miss trends for subsequent fixes without
erasing the original failures.

### PH-16 — Deterministic qualification and reviewable evidence

**Lesson.** Some prior failures were real production defects; others were
incorrect selectors, working directories, fixture assumptions, stale generated
artifacts, missing offline dependencies or observer/capture failures. Repeated
full gates and confirmation runs did not isolate those causes. The handoff's
`[no tests to run]` example and the retained failed bundles must stay distinguishable
from passes.

Blocking HTTP fixtures must consume and close bounded request input before
waiting on `request.Context()` cancellation, and retain an independent cleanup
release joined before the test server closes. An unread POST body can prevent
the expected cancellation notification. This lesson recurred in the monitor
and public-startup seed handlers; actual runtime completion and a hung fixture
cleanup are separate results. The startup correction audits both seed and EVM
outage barriers and preserves prior retained-intent normal/race passes.

The September 27 combined-source validator package run hit Go's default
10-minute deadline under `-parallel 2`. Its original PTY output was truncated,
so the exact active test cannot be established. The matching compact replay
error text is deliberately emitted by interrupted-read fixtures; their
six-case family passed in isolation. A [retained diagnostic](</mnt/data/sn-testnet/evidence/mainnet-validator-timeout-20260927/RESULT.md>)
was stopped after 350 seconds with 275 top-level passes and no assertion
failures, while serial, fsync-heavy tests were still running. This is **not** a
full-suite pass or a demonstrated production replay defect. Use a measured
package deadline and persistent per-test log for the frozen release, while
keeping changed-path normal/race results distinct from the incomplete broad run.

The September 28 continuation selector repeated the deadline problem at a
smaller scope: its 120.755-second normal run passed all 22 roots, but the race
run reached a 600.190-second package timeout after 16 passes, without an
assertion failure. Preserve that package failure and resume only the interrupted
root plus five unstarted roots. Choose future deadlines from the measured
workload **in the same mode**, with at least 2× headroom; real compact replay
has a materially larger race cost than its normal timing. Enumerate selectors
before running: a terminal `$` on a test-family prefix selects no descriptive
test names. Neither a zero-root invocation nor a runner timeout is a product
regression result.

A later monitor capture assembled its selector from a headerless outcome table
as though the first row were a header, omitting one of 48 expected roots. Keep
that invocation as a 47-root scope and run the missing root separately. Require
exact expected-versus-selected membership before bodies, including the first
and last entries; nonzero enumeration alone does not establish complete scope.
Prefer the maintained qualification owner over another untyped selector wrapper.

Go permits one literal `t.Run` name to contain slashes without emitting every
intermediate prefix as a test. The old declaration parser rejected complete
retained HTTP results by requiring those nonexistent events. The
[qualified correction](evidence/qualification-slash-parent-qualification-20260928.md)
uses the nearest explicitly source-declared ancestor, preserving exact event
membership, genuine parent ordering, failure literals and original binary exit.
Its own 31-root normal/race checks and two controls passed before read-only
replay recovered both original 33-root/32-descendant results. Keep declaration
repair separate from body execution; do not rerun successful unchanged bodies
to repair a checker. Ambiguous flat sibling prefixes remain an explicit limit.

The monitor's blocked HTTP fixture then consumed its whole package deadline in
server cleanup after the real monitor workers had exited. Its handler waited
for request cancellation without reading the POST body, preventing HTTP/1's
background disconnect read from starting. A blocked-read fixture must consume
and close the bounded real request before advertising its cancellation barrier.
Join the actual handler; manually releasing a separate test channel cannot
establish production cancellation. Keep the failed capture and qualify the
corrected shared fixture and its adjacent restart consumer.

**Production change.** Follow [CODESTYLE.md](../../connect/CODESTYLE.md): each
root cause needs a deterministic pre-fix failure and corrected result at its
observable layer, using barriers/hooks/state transitions instead of scheduler
luck. Inspect similar callers, alternate/replay/batch paths and adjacent failure
classes. Use synthetic identities and bounded fixtures; keep live custody and
private captures out of tests. Freeze each job's actual inputs, enumerate
selected roots, require nonzero expected membership and record test/build/body
and cleanup outcomes. Run normal/race modes where relevant. Repair the failed
scope and reuse demonstrably unaffected results; repeat only for a named
unresolved timing concern. Rerun the representative failed integration when a
small test cannot establish the workload/resource fix.

A fixture's SDK JSON encoder is not necessarily the RPC wire encoder. Receipt
qualification exposed an SDK that emits unprefixed block-number hex although
Substrate serves a `0x` quantity. Keep genuine SCALE header/body commitments,
serialize the actual wire format explicitly, and require malformed-response
tests to reach their injected read. An unrelated early fixture rejection is
not evidence that the intended failure was handled.

Maintain a requirement-to-evidence table for the composed release: original
failure, root cause, patch and adjacent paths, exact source/dependency/toolchain
inputs, causal test, normal/race results, reused scopes, deployment and operational
proof, unresolved work. Keep producer/aggregate requirements and complete live
acceptance in that table without turning each patch into another full restart.
Hash and retain raw receipts and numbered reports; preserve failed/canceled
attempts, not overwritten summaries.

Use the [independent verifier](../sim-testnet/peerreview/verify/README.md) as a
reproduction model: rebuild Merkle roots and signatures independently, pin
native/EVM mapping, decode transactions/events/storage, and declare archive
requirements. Parameterize new run/deployment inputs rather than editing old
expected findings into passes. Distinguish on-chain proof, authenticated artifact
content and off-chain operational assertions. The current testnet policy uses
only the owned LAN RPC and must say `independent_rpc=false`; running an
independent implementation against that node does not create an independent
observer. Preserve the first report's separate public-node comparison with its
original scope. A production independent observer, when provisioned, must have
its own declared endpoint, chain identity and observed checkpoints.

**Closure.** The affected qualifications plus controlled production-path
fault injection must prove the corresponding PH requirements. The final
exercise combines compatible runtime upgrade, interrupted submission, temporary
network loss, service/dependency restart and replay/cache reuse while retaining
financial history. Then acquire the required complete acceptance interval and
verify accounting, policy, both validator duties and graceful shutdown. A clean
test log, peer review of an earlier run, or report publication alone does not
complete mainnet readiness.

### PH-19 — Retained snapshots use historical runtime authority

**Lesson.** The 2026-09-21 provisional resume reached a retained relay
continuation recorded at reviewed runtime `node-subtensor/461/1/1`. Its reader
misclassified that immutable block as a current snapshot and applied the
current-only compatibility fallback, rejecting it before campaign activation.
The current finalized node was runtime 468; no current signing authority was
missing. The correction is SN `d3bbc8f5` and its local
Terra qualification record is `/mnt/data/sn-testnet/qualification/terra-runtime-461-20260921/`:
seven targeted roots pass normally and under race, while the old classification
reproduces the exact 461 rejection.

**Production change.** Give every native read an explicit purpose: immutable
activation/continuation history, newly selected finalized snapshot, or current
head/signing. Historical reads may use only the exact reviewed artifact for
their pinned block; they must not inherit a current-runtime requirement.
New snapshots, writes and signing retain current capability and approval
checks. Imported continuation pins must be canonical and finalized before they
are classified as history. No historical compatibility result may authorize a
new action.

**Closure.** Exercise historical continuation and activation snapshots across
compatible upgrades, including imported pins, changed metadata/code, noncanonical
hashes, cancelled reads, changed hotkeys, stake and permit. Prove a current
read and signing operation still reject the historical artifact. Cover both
validators, miner, bootstrap, settlement and archive readers in normal and
race qualification. This is an implementation input to RT-01, RT-02, RT-05,
RT-06 and PH-18; it is not complete for mainnet merely because the simulator
correction passed.

### PH-20 — Capacity accounting separates approved slots from scan pages

**October 3 actual production continuity review.** The [three-file source review](evidence/production-capacity-continuity-review-20261003.json) confirms that production config continuity compares the complete bounds vector. A separately signed capacity revision therefore needs an explicit predecessor-bound admission path; the private simulator V6 doubling helper is not mainnet authority. Astra is implementing retained/future horizon, upload, archive and resident-resource accounting on the actual config/history boundary. Preserve per-record and transport limits, old approvals, activation and signed prefix; capacity never grants active membership or new spend. Actual loader/reopen and populated-sidecar tests remain required.

**Lesson.** Immediately after PH-19 passed in the same 2026-09-21 resume, the
relay startup inventory stopped before historical reads with `observed manifest
slots exceed 1024`. Read-only census found 271 closed manifests for each of two
validators with two members per manifest, plus three validator-2 audits: 1,090
prospective member slots. The retained continuation authorizes only
`new_slots=1024` and has no approved journal debits. The fixed 1,024-entry
scanner ceiling exposed the excess early, but merely raising it would later
admit unapproved work and is unsafe. No transaction or journal entry was added
by this failure. Investigation is tracing which entries are historical versus
eligible new work; the correction is active in the sim-testnet run. On 2026-09-21
we selected an explicit 2,048-slot continuation allowance: a 2x margin over
the measured backlog. At the existing 1,000,000-gas / 25-gwei cap it binds
51.2 EVM TAO total, a 25.6 EVM TAO increase over 1,024 slots. It must be a
newly bound finite resource/spend revision, not a scanner-default change. The
testnet revision raises both lifetime EVM and total TAO ceilings to 512. The
2,048-slot relay reserve remains exactly 51.2 EVM TAO; the remaining ceiling is
headroom, not authorized relay spend. Its keeper top-up and relay-reserve
allocation are reconciled exactly once.

**Production change.** Represent separately: (1) immutable aggregate approved
slot/spend capacity, (2) source/member slot cost, (3) historical/previously
admitted evidence, (4) bounded directory/page read size, and (5) bounded
resident memory/byte budget. Enumerate large retained histories in authenticated
pages with a stable snapshot cut. Reconcile every candidate to a retained,
exactly approved slot before it can consume send authority; aggregate genuinely
new work against the approved slot capacity using checked arithmetic. The
selected testnet 2,048-slot allowance is an explicit revision with exact gas,
fee and aggregate-spend bounds; production derives its own approved allowance
from a census plus reviewed margin. Retain only bounded witnesses or streamed
verification state. A malformed directory,
unapproved candidate, changed scan cut, ownership escape, byte violation or
gap/duplicate fails precisely. Do not solve this by lifting a global constant
or silently increasing the approved spend.

**Closure.** Add deterministic pre-fix and fixed tests for exactly-full and
one-over aggregate new-work capacity; the exact selected 2,048 allowance;
more-than-one-page retained history;
per-source/member multiplication; retained-versus-new classification; changed
directory during scan; duplicate and missing pages; cancellation/restart;
imported continuation; malformed entry and byte exhaustion. Run normal and race tests at the startup, continuation,
archive-replay and final-acceptance consumers. A controlled production-path
rehearsal must resume a large authenticated history without redoing completed
work, while refusing unapproved extra work. Link the completed sim-testnet
fix, its Terra evidence and actual resume record before marking PH-20 done.

### Generation-25 follow-up — fault, transport, capacity and recovery hardening

Generation 25 started its acceptance interval at testnet block `8,062,774` on
2026-09-22 and produced a complete terminal evidence bundle. It did not
complete acceptance. The direct terminal error was `disable miner-848: context
deadline exceeded` while applying the 96-member quality cohort. The fault
controller had retained per-member intent and completed work, but dispatched
members serially while holding the campaign callback; a transient local timeout
therefore consumed the remaining fault window. The result also recorded
acceptance-scope TLS handshake timeouts and adversary artifact/API GET
deadlines. The 41 unexercised later faults are explicitly derived from this
interruption, rather than separate production defects. Evidence is retained in
the generation-25 `faults.json`, `process-logs.json`, `anomalies.json`,
`assertions.json` and `result.json` under `sim-testnet/runs`.

**PH-21 — Fault controller.** Persist an idempotent intent and completion
record for every independently controlled member. Dispatch independent service
or swarm controls with a bounded concurrency limit, never one unbounded serial
loop. On a transient timeout, first read and reconcile the member's actual
state, then retry only that pending member with bounded backoff; an already
applied disable or restore is success. Record trigger, first-dispatch,
per-member completion, effective cohort completion and restore boundaries
separately. A temporary control-plane timeout must not erase the durable
completed prefix or require a whole campaign restart. Invalid identities,
conflicting state and exhausted retries remain explicit failures.

The September 22 release also exposed a disagreement between these layers:
the control driver returned a legitimate partial round, but the signed campaign
validator required pending faults to have no process census or diagnostic. Its
rejection canceled observation before the next full snapshot. Both applying and
restoring retries must have explicit checkpoint semantics, with a canonical
first-dispatch boundary, monotonic retry count, exact target census and separate
completed-transition block. Preserve those diagnostics through checkpoint
signing and reopening; incomplete work must neither stop observation nor count
as a completed acceptance fault. Test the complete driver/controller/checkpoint
path together, including a pending heartbeat while a snapshot is still running.

Bounded rounds must also make progress across their completed prefix. Re-reading
every completed member at each ten-second boundary can starve a large batch
indefinitely under load. Retain verified member progress for one in-process
fault/action and owning worker generation, only after the live reconciliation
and durable completion write succeed. Reopen, parent cancellation, hard failure,
worker replacement or the opposite action must require fresh reconciliation;
an old completion file alone is never authority. Test multiple constrained
rounds, restored-state drift, same-PID worker replacement and mixed
cancellation/integrity failures. Keep all acceptance-window and minimum fault
duration checks unchanged.

The September 23 interval exposed a second starvation path: a shared ten-second
round deadline started before its generation census and serial intent fsyncs.
Disk pressure consumed the budget before requests were dispatched, then an
outer deadline was signed as a terminally failed cohort. Request deadlines must
start at actual dispatch. Bound request counts and simultaneous members instead
of charging storage admission to an HTTP timeout. Commit exact pending-target
and attempt intents in bounded batches, and batch completion checkpoints before
admitting later mutations. Keep a sole persistence owner and join every worker.
Temporary storage failures and outer deadlines retain applying/restoring intent;
they never certify completion or backdate the fault. Test a 96-target cohort
with simulated flushes longer than the old round deadline, crashes before and
after rename, no mutation before a failed intent flush, and restart reconciliation
without duplicate side effects. Permission, schema, custody and joined integrity
failures remain hard.

Control admission also needs a bounded readiness state for a checksum-bound
swarm that is temporarily unhealthy or between process generations. Wait for
that same owner before first dispatch, preserve an existing completed prefix,
and re-read its live member state after replacement. A PID change between the
admission read and the control round must defer that round before any request;
it must not cancel the campaign. Missing owners, changed identities, invalid
state and checksum failures remain hard. Tests must force first-dispatch,
partial-prefix and restore restart windows, cancellation, generation turnover,
and a mixed readiness/identity failure without sleeps.

Preserve every semantic failure when its diagnostic checkpoint also fails.
Classify each joined cause; a malformed or foreign status remains hard even
beside a retryable disk error or cancellation. Restoration cleanup has its own
durability boundary: retain the exact completed census before removing active
intent. A failed unlink/rename sync must resume from that checkpoint and observe
each member again, including after process restart; a retained completion alone
does not prove current state. Test both already restored and newly changed
members, partial or substituted checkpoints, and missing recovery evidence.

**PH-22 — Transport recovery and final signal.** Treat connect/read deadlines,
EOF/reset and HTTP `429`, `502`, `503` and `504` as bounded retry candidates
only for idempotent reads or controls with a retained idempotency key. Reuse the
same request identity, reconcile an uncertain outcome, record attempts and
backoff, and preserve cancellation as cancellation rather than retrying it.
Invalid JSON, identity/hash/signature mismatch and semantic API refusal remain
hard failures. Transport clients must repair TLS connections and report health
recovery; a correlated TLS incident remains visible and must be absent from the
final acceptance interval. Adversary probes may continue after a recovered
transient read, but final acceptance evaluates the persistent exhausted-retry
error budget rather than the first timeout.

The retained publication review also found that stream upload/read transports
discarded HTTP status into error text. A protected-quota `429` then consumed the
native failure budget instead of waiting for its hourly reset. Preserve typed
status and bounded server pacing through every wrapping and replica join;
authentication, conflicting content and mixed integrity failures remain hard
even when response text contains a transport-looking phrase. The transport
performs one immutable request; its existing lifecycle owner retries. Startup
honors a single positive integer `Retry-After`, bounded to one hour, within its
existing finite attempt count and cancellation scope. Provisional native
collection retains its cut across ordinary retry polls; strict final acceptance
keeps its original failure budget. Deterministic transport, mixed-cause, reset,
cancellation and strict/provisional tests cover this correction; production
closure still requires exercising actual quota exhaustion and recovery.

Apply the same ownership rule above the relay's individual reads. Its runtime
previously stopped the complete campaign when one closed-publication or deposit
audit step returned a transient error after lower-level recovery. Give each step
a finite operation retry budget, record the failure before retry, and re-enter
the existing exact signed-transaction reconciliation path; an accepted send
with a lost response must resolve to its original winner without a new nonce or
duplicate send. The independent historical census retains partial checkpoints
and retries under the same transport classification without canceling live
traffic. Preserve every independent integrity error, cancellation and terminal
exhaustion. Retry diagnostics remain durable under `evidence-relay-retries/`;
they confer no acceptance authority. Closure requires actual uncertain-send
reconciliation, mixed-failure, exhaustion and audit/runtime isolation tests.

**PH-23 — Funded capacity and physical resource profile.** A capacity revision
must bind four different facts: funded slot/spend allowance, source-history
horizon, upload quotas and finite archive metadata limits. Generation 25 found
that setting 2,048 slots while leaving a 2 GiB metadata document limit would
make the stated workload impossible. The successor profile therefore needs an
explicit source horizon and finite, non-preallocated typed-document, retained
metadata and supplemental-metadata ceilings with at least the reviewed 2x
margin. It must carry an authenticated predecessor reserve exactly when no new
spend is intended; it must never reconstruct fresh economics from the new slot
count. Admission rejects a requested profile that does not fit every bound.

Full fleet-renewal approvals crossed the ordinary proof-file limit: a compact
generated 35 MiB plan could not be imported by its own command. Output, import,
active/runtime reload, immutable archive and historical owner lookup now share
a separate 128 MiB approval bound while ordinary proofs remain at 32 MiB.
Production must qualify each producer-to-consumer path at the selected size,
including closed capture and public replica replay, before declaring the
profile usable. Preserve exact approval hashes, no-follow regular-file reads,
aggregate archive charges and independent cache memory limits. A valid plan
larger than an optional cache must bypass caching, never exhaust an eviction
queue or acquire unbounded retained memory.

The adjacent closure paths needed the same correction: capture bundles,
derived validator plans, fleet lineage, public signing/readback and completed
prior-phase carriers each had a different smaller limit. Use exact producer
paths and schemas to select capacity, retain separate plan/ordinary counters,
and clip their combined use to the configured grant. Capture only the approved
ancestor hashes, not unrelated reviews found in the archive directory. A public
blob write is incomplete until the actual API GET and exact-hash history routes
can authenticate and return it. Keep ordinary upload/proof limits unchanged;
test a generated large plan through capture, signed transport and replay,
alongside invalid aliases, one-byte overages and independent counter exhaustion.

Keep whole-source catch-up forecasts separate from live quota consumption. The
retained source forecast charges all source history and admitted refresh/retry
operations to one hourly bucket; it can exceed a retained deployment's limits
before any actual counter is exhausted. Testnet provisional continuation may
record this forecast as advisory with `final_acceptance=false`, but must preserve
every enforced object, byte and retry counter and deployment/replica owner.
Adopt larger production quotas only through an authenticated config/manifest
successor, with at least 2x all forecast dimensions; do not edit bound retained
configuration or waive a real quota to clear a forecast warning. Record actual
counter usage, resets and recovered throttles so final admission can distinguish
an oversized catch-up estimate from sustained insufficient capacity.

**PH-24 — Recovery-lineage work.** Generation 25 authenticated 24 retained
generations before it could publish its recovery record. The reader repeatedly
decoded and hashed the same archived plans even though the lineage already had
an immutable per-invocation lookup boundary. Cache each fully authenticated
plan by its raw digest, filesystem/source witness and lineage owner; retain
per-edge source and ordering checks on every reuse. Bound the cache, log
generation progress, and fall back to cold authentication after an immutable
source change. A cache must not bridge plans, authorities, generations or
changed bytes.

Runtime rendering exposed the same duplication inside one operation: nested
evidence, staging and manifest readers each revalidated the complete active
plan. [The scoped reader](../sim-testnet/runtime_plan_read_scope.go) retains one
successful proof for exact source bytes, state root, configuration and private
route/assurance fields. Every use still acquires and hashes the bounded source;
each caller receives its own decoded plan. Production qualification should
count full validations per render, then replace/truncate/symlink source files,
change authority and mutate returned values. No failed validation is reusable.

R35 exposed a remaining scope gap after this improvement: release observations
still re-entered recovery-lineage validation, repeatedly authenticating the
latest two generations in roughly nine-second passes. The controller accumulated
tens of gigabytes of logical reads while observations advanced. Before mainnet,
cache only the sealed predecessor-edge proof across observations under exact
source-byte, file-identity, plan and authority witnesses; invalidate it on any
changed generation or source. The current attempt envelope is rewritten during
observations and must remain freshly authenticated. Keep the per-edge checks
when a new generation is appended, and measure full lineage validations and
logical read bytes per observation in the actual release process. The repeated
edge is material to the observed 100–162-second gaps, but is not yet proven to
be their only cause. A process staying alive is not a throughput proof.

**PH-25 — Deployment workload ownership.** A terminal campaign and its
deployment have distinct lifecycles. A terminal scenario may retain the exact
healthy supervisor, claim relayers, miners, validators, proxies and supporting
services for a successor; it must not silently repurpose them for another
deployment. Explicit deployment stop must retain immutable evidence and the
durable restart/continuation record, then cancel and join every owned process
group before reporting shutdown. Generation 25 confirmed that explicit stop
removed its supervisor and children. Never infer either continuation or cleanup
from a dead parent while a recorded child process group remains live.

**Closure for PH-21 through PH-25.** Add deterministic tests for partial cohort
completion, timeout then state reconciliation, restart from a durable prefix,
already-applied members, bounded swarm concurrency, exhausted retry, and no
duplicate disable/restore. Test recovered and exhausted API/TLS reads,
cancellation without retry, and hard semantic/integrity responses. Test funded
successor capacity, one-byte/one-slot/one-object overages, every metadata
dimension and imported predecessor reserve preservation. Test shared retained
plan lookup under source replacement, truncation, symlink substitution,
concurrent mutation and bounded eviction. Run normal and race suites, then a
full final acceptance interval with a clean TLS and transport incident ledger.
Exercise terminal-scenario continuation with a live child workload, then an
explicit deployment stop that proves every owned process group exits while its
evidence and resumable state remain readable.

**PH-26 — Large evidence transport.** The fleet renewal exposed an evidence
shape that was valid under the selected capacity profile but could exceed the
ordinary 64 MiB HTTP GET deadline and body limit during closed capture or public
replay. Production must admit only explicitly typed plan, bundle and lineage
families to their separately reviewed byte limits. After header admission, the
server and client may use a byte-scaled, finite deadline and a bounded
large-response semaphore; ordinary metadata and ordinary HTTP routes retain
their existing deadline and size limits. Parent cancellation must close an
in-flight blob read and join its worker, so a timed-out reader cannot retain a
large buffer or slot. Every response still verifies the exact body digest,
schema, source identity and lineage ordering.

Generic metadata and manifests must not silently inherit the typed-evidence
exception. Before a production profile can produce metadata above the ordinary
transport limit, give that family its own finite transport owner and either a
streaming/reference representation or an independently tested typed admission
path. Deduplicate immutable lineage references rather than embedding the same
ancestry in plan and prior wrappers repeatedly. Qualification covers admitted
large GET, historical replay, server timeout cancellation, client cancellation,
busy admission, malformed headers, digest mismatch and concurrent ordinary
requests; it must prove finite memory, connection and worker usage under race.
An absent or empty optional completion checkpoint means no completed work yet;
it must initialize a durable empty state rather than crash fixture setup or
recovery. Malformed, substituted or conflicting completion records remain hard
failures.

Historical custody checks must retain bounded, authenticated progress across
sample deadlines. Rewalking every prior payout body made all 235 attack samples
exhaust the ten-second read budget while independent artifact checks passed.
The simulator now scopes a hash-to-epoch metadata cache to the complete payout
domain and checksum-bound API process generation, refreshes history membership,
and verifies the selected latest body and finalized vault state every attempt.
Missing or changed process ownership invalidates cache reuse; signatures,
content identity and same-epoch equivocation remain strict. An interrupted sample
is pending evidence and cannot satisfy the final proof gate. Production adoption
must prove interrupted-prefix continuation, source turnover, new equivocation,
latest-body substitution and finite entry counts with deterministic regressions.

A native-cycle custody proof must not monopolize release startup or a separate
journal writer while it waits for blocks. The simulator's explicit provisional
`scenario --name precompile-prepare` executes and authenticates the approved
transaction prefix through its finalized snapshot, then releases the command's
lock. The release's existing writer continues the remaining exact dividend and
transfer actions in bounded observation turns. Each unfinished read remains
pending; a transient read or interrupted transfer retains the verified frontier.
Final conformance still requires a full native window, a positive dividend,
exact conservation and complete recovery to the approved custody destination.
Never label the preparation result as release acceptance. Production adoption
must cover pending observer survival, incomplete or substituted receipt prefixes,
transfer interruption after dividend verification, source identity changes,
parent cancellation, and refusal of incomplete conformance at interval end.

### Closing and maintaining this hardening plan

For each PH item record the implementation/review commit, affected production
consumers, deterministic regression and adjacent review, qualification receipts,
release/deployment, operational evidence, remaining action and accepted
limitations. Mark `Done` only when its closure criteria are proved on the
production path. Simulator-only success remains partial evidence. Link related
RT/RL/PF rows so one completed implementation can satisfy multiple requirements
without duplicate qualification.

Roll out shared recovery/identity interfaces first, followed by independent
consumer migrations and a composed release. Retain the previous authenticated
release and state-format compatibility for roll-forward recovery; any rollback
must reconcile already submitted transactions and preserve finalized economics.
Fault injection may use a controlled integration network, but it must exercise
the production implementations and the actual capability assumptions; mocks
alone do not establish live precompile, governance or economic behavior.
Preserve the closed sim-testnet evidence. Use controlled production-path
rehearsals for new fixes and record actual mainnet observations after an approved
activation; neither can rewrite the original R48 outcome.

### Precompile stake requests versus native share rounding

The conformance harness treated a requested stake amount as both observed balance
changes. A finalized same-subnet move instead debited and credited the same amount
one alpha-rao below its request: the remaining unit stayed at the source. The
reverse path also tried to spend the original request rather than the amount
actually received. Production acceptance must distinguish requested units, observed
source debit, observed destination credit, and any explicitly recorded remainder.

The testnet repair records separate checksum-bound fields for the request minus
source debit (`native_share_residue_rao`) and source debit minus destination
credit (`native_share_credit_rounding_rao`), each bounded to zero or one rao.
A read-only call at the finalized forward receipt reproduced the adjacent reverse
case: an all-balance request clears its source, while its destination's native
share quote credits one fewer integer unit. Both conversions must be explicitly
accounted for; negative deltas, inflation, unrecorded differences and larger
rounding remain hard failures. Requested amount, pre-state, roles, signer, nonce,
chain, contract and transaction remain exact. Reverse calldata uses actual credit.

The shared accounting governs live reconciliation, retained postconditions,
successor receipt replay and final recovery. Finalized transactions resume without
another broadcast. Historical zero-rounding evidence keeps its original canonical
encoding. Round-trip accounting requires returned stake plus explicit native
credit quantization to equal the initial position and requires zero remaining
stake on the intermediate hotkey. Final transfer requires zero probe custody and
an exactly recorded destination credit plus its bounded conversion; rounding is
reported, never silently counted as a recipient payment.

Before mainnet, exercise native share conversion in move and transfer operations,
including even and odd requests, all-balance withdrawals, full custody recovery,
finalized-before-evidence restart, and negative controls for unmatched accounting,
more than one unit at either conversion, changed requests and arithmetic overflow.
Do not propagate this probe rule into payout accounting without independently
specifying and validating that contract's conservation and principal guarantee.

### Stake observations across blocks and residual recovery

The next live reverse move returned its exact approved principal but occurred
579 blocks after the forward move. Both positions had grown in the meantime:
the move hotkey carried 17,306,833 alpha-rao of additional stake and the sample
hotkey carried 21,110,029. Requiring the later pre-state to equal the earlier
post-state rejected a valid round trip before the snapshot transaction. Native
share rounding is a within-call conversion; it must not absorb inter-block
credits or become a broad numeric tolerance.

The harness records those receipt-proven credits and the unrecovered move
position separately. Snapshot preparation can continue while that liability
remains explicit. Snapshot calldata has no amount argument, so its event baseline
is the authoritative inclusion-time output; a positive credit after the pre-send
read does not change the signed intent. Replay still requires the exact hotkey,
receipt, inclusion block and retained baseline. Final acceptance continues to
require recovery of both positions. An exactly recorded pending recovery keeps
provisional observations running and returns before another transaction intent,
while altered evidence and missing files remain hard failures.

The small residual cannot simply be swept: pinned read-only calls showed the
17.32-million-rao residual and requests up to 100 million rao reverting, whereas
500-million-rao and larger funded operations succeeded. Empty revert data does
not establish a specific runtime minimum. Production recovery must check actual
runtime behavior and support a bounded top-up from existing custody before
sweeping a small residual. Each top-up and sweep needs its own authorized action,
durable nonce, exact receipt and recipient accounting. Do not overwrite the
original reverse receipt or claim that it left zero balance.

Before mainnet, cover stake growth between every pair of observations, including
read-to-inclusion and dividend-to-recovery. Account for a recovery top-up's sample
debit when comparing the eventual sample transfer with the earlier dividend
observation. Force interruption at every signing/finalization boundary, residual
growth during a sweep, and repeated recovery that retains completed actions.
The final proof must include both recovered recipient positions, bounded native
conversion residues, and zero source custody. Test forged extra credits, changed
roles/amounts, duplicate spends and missing repair authorization independently.

### Claim queue write amplification and admission budgets

The first live acceptance interval exposed a storage saturation loop: claim
workers reconciled old entries before checking their retry deadline, then
rewrote and fsynced their complete queue for every repeated not-ready result.
Readiness failures did not increment submission attempts, so their backoff never
grew. The two relayers generated roughly 116 MB/s of queue writes and starved
unrelated durable fault controls. The control round's deadline included its
sequential persistence work, leaving healthy local endpoints little or no
request time. Healthy process status alone did not establish useful progress.

The production queue now checks retry admission before API/RPC work, records
reconciliation attempts separately from transaction submissions, and combines
retry diagnostics into one checkpoint per poll. Historical readiness backoff is
bounded at one hour; the newest two epochs and exact uncertain transactions keep
a one-minute cap. Ordinary historical reconciliation has a small per-poll work
budget, with unvisited entries retained, so faster persistence does not create an
API catch-up burst. Current work and uncertain transaction outcomes remain
eligible. This is queue scheduling, not an RPC endpoint rate limit.

Unchanged saves require a successful acknowledgement from this store plus
matching current bytes in a private regular file. A reopened owner or failed
durability boundary must sync again. Submitting intent, prepared signed bytes,
broadcast checkpoints and finalized receipts remain immediately durable; the
diagnostic batch never grants transaction authority or marks an uncertain send
as absent. Deterministic tests cover historical backlog progress, future retry
deadlines, restart/backoff persistence, recent-epoch readiness, exact uncertain
outcomes, failed writes, cancellation, and changed or missing queue files.

Before mainnet, qualify the control scheduler and queue together under slow
durable writes. A network request's attempt budget must begin after required
intent admission; expired queued work must remain resumable without canceling
the observer. Batch intent where safe, keep one durable owner, and require exact
fresh completion evidence before assigning a fault's applied block. Track queue
write bytes, checkpoint latency, remaining historical work and admitted control
requests independently from process health. Production sizing must reserve the
agreed 2x margin without relying on filesystem stalls to throttle useful work.

**R44 follow-up (in progress, 2026-09-25).** The per-miner recent-first poll did
not make the two relayers fair across miners. One shared, non-cancelable lock
covered reconciliation, signing and finality; a miner recorded `submitting`
before waiting for it. A hashed live census at finalized block 8,081,014
found 744 miners still discovering epoch 614 while 256 had reached 617. All
1,000 eventually reached 617, but epochs 615 and 616 remained almost entirely
pending. Mainnet admission must assign one retained ticket per member, prioritize
the global newest epoch with a bounded historical share, let waiting members
continue discovery, and start the five-minute network budget only after
admission. A timeout after durable `Prepared` must not let the next member sign
the same nonce: seed a shared nonce floor from every validated member queue,
advance it only after the signed intent is fsynced, and reconcile or rebroadcast
the exact old raw transaction before treating its outcome as absent. Reject two
swarm members pointing to the same physical queue directory. Test mixed
discovery cursors, cancellation, stale pending nonces, restart, cross-member
fairness and exact signed-outcome retention in normal and race modes. No R44
runtime change or completed production qualification is claimed here.

The R44 acceptance reader also mixed lifetime claim counts with its signed
five-epoch window. A previous finalized claim could falsely satisfy current
coverage, while a historical uncertain claim could falsely fail it. Keep raw
lifetime history and scope acceptance and anomaly verdicts to the exact signed
epochs, requiring an observed outcome for every configured miner in each epoch.
Keep the whitepaper's claim TTL: `pending` and `retry` may remain after an epoch
finalizes while their value stays in outstanding liability. The phase-level
claim coverage and on-chain conservation checks still apply. Treat a current
`submitting` send as uncertain at the acceptance cut, and reject actual
unreconciled `uncertain` or `failed` outcomes. Do not close a signed uncertain
incident merely because
the local queue later says `finalized` or `no-claim`; first authenticate its
canonical receipt, block hash and Claimed event, or retain the incident open.
The completed window-only claim gate is SN `5615a382` and historical anomaly
scoping is SN `ac2beccd`; receipt-authenticated closure remains separate
qualification work.

### Supplemental repair allocation within lifetime caps

The repair proposal later exposed a separate budget boundary: fleet-renewal
liabilities had consumed the local campaign reserve while approved lifetime
headroom remained. Production must distinguish those two limits. A supplemental
repair approval may allocate its exact documented shortfall within both retained
lifetime caps, signed by the budget and custody owners, without replaying setup
or changing its actions. Retain active and superseded spend in that calculation,
round fractional native units upward, and recheck signed/queued exposure before
execution. Tests must reject cap substitution, omitted historical liabilities,
arbitrary extra margin, duplicate charging after restart, and integer overflow.

### Preparation must not acquire a stopped campaign's transport

Standalone precompile preparation reused completed chain evidence but then opened
a campaign executor, forcing its next call through a loopback EVM proxy owned by
a deliberately stopped supervisor. The authenticated command already had working
RPC and transaction managers. Preparation now borrows those exact owners and
retains their authorized route, journal, plan, native connection, payloads and
nonce management; it neither starts topology nor closes the caller's managers.
Owner or route drift remains a hard error. Release scenarios still perform their
separate retained-topology restart and supervised egress handoff. Deterministic
regressions cover stopped proxies, continued use of the original direct client,
foreign journal/plan/route owners, and unchanged release restart scope.

Retained release startup has the converse ownership requirement: its local-only
executor owns approved metadata but deliberately has no native connection to
lend. After topology restart, campaign construction must acquire that missing
reader through the ordinary authenticated constructor. It may do this only for
the exact provisional release plan, journal, directory, configuration and equal
reloaded credentials; partial or foreign connection owners remain errors. A live
parent's native reader stays borrowed. Supervised EVM egress and its readiness
errors remain mandatory, with no direct-route fallback. Test both absent and
existing native ownership, credential reload and drift, canceled construction,
and refusal to bypass a stopped proxy.

Retained process restart must distinguish approval of new work from continuation
of an approved plan. A completed fleet renewal appends transaction actions, so an
allowance-only classifier cannot admit its later process restart. Authenticate
the exact active and archived approval, reconstruct the fleet append from its
archived predecessor at the original journal checkpoint, then authenticate the
full later journal independently. Valid preparation after that checkpoint must
not be treated as conflicting renewal submission. Keep the original fleet-apply
exclusion for new work. Use the same restart admission for preflight binary and
readiness preparation, process startup and interrupted manifest publication;
none of these paths may replay pending setup or alter final acceptance.

### Recover custody without repeating completed acceptance epochs

The probe's immutable `transferOut` accepts an off-chain amount. A position can
accrue between that quote and inclusion, and small residual positions can fail a
runtime transfer minimum. Final custody cannot be inferred from a successful
receipt or a nearly equal balance. Keep requested units, actual source debit,
actual recipient credit, bounded share conversion and inter-block growth as
separate fields. Preserve successful receipts even when they leave a residual.

Production recovery must use separately signed, finite authority for each affected
position, bounded funding when a residual is below the transfer minimum, and one
durable transaction writer. Bind quotes into action intents; retain signed bytes
through timeout and crash recovery; verify gas, fee and value limits again during
replay. A final record must prove both source positions zero at one finalized
head. Test interrupted signing/finalization/postcondition boundaries, quote edits,
extra credits, receipt/recipient changes, gas-cap changes and reseed exhaustion.

Recovery admission must authenticate only the exact repair authority, custody,
immutable target and retained journal before allowing its durable sender to
reconcile pending bytes. A finalized deployment nonce census cannot precede that
reconciliation: the transaction being recovered may already consume the next
nonce without a finalized journal row. Restrict the recovery executor's dispatch
scope when reusing partial payloads, and enforce the signed fee/value envelope
before rebroadcast as well as during final replay. Test a crash with a signed,
unfinalized call and prove the same bytes finish without allocating another nonce.

For future probe/custody maintenance contracts, provide a narrowly authorized
operation that reads and transfers the full selected position in the same call,
with exact before/after events and an explicit recovery recipient. This removes
the quote-to-inclusion gap; it does not change exact-amount payout entitlements.
The already deployed testnet probe instead uses the bounded mechanism documented
in [PRECOMPILE-RECOVERY.md](../sim-testnet/PRECOMPILE-RECOVERY.md).

A later custody repair must not rewrite a signed interval or force already
observed epochs to repeat. Preserve the original result and add an authenticated
completion for the repaired scope; require the production handoff to understand
that composition explicitly. A new binary cannot silently join an immutable live
interval. Deferred historical audits and unrelated semantic checks remain their
own outstanding requirements until their exact proofs are accepted.

### Keep read availability separate from verified mismatches

Relay startup and receipt reconciliation must return an RPC read error before
comparing the unread value with an approved snapshot, nonce, registration or
transaction. Do not join a fabricated mismatch to a timeout: mixed errors remain
hard by design, so that join prevents the bounded transport retry from running.
The same rule applies to the final registration and canonical-hash rechecks in
the native schedule reader and to retained manifest reads.

Give initial phase admission and each consumed phase-transition request their
own bounded retry. Keep the first successful finalized head and original wall
deadline across admission attempts. A canceled caller releases its request while
the relay remains available; worker shutdown cancels an active request. Retain
hard integrity and local persistence failures even when joined with cancellation.

A permissionless publication race needs a typed canonical-revert outcome,
distinct from a failed journal write. Retry its independent winner, canonical
receipt and transaction-body reads using the original signed bytes and nonce.
Keep the reverted receipt and actual paid gas visible. Test failures at each
read boundary, exhaustion, request cancellation, original-deadline retention,
journal reopen and no duplicate broadcast. Test real mismatch and storage-error
controls beside every transient recovery path.

### Retain unsigned repair liabilities during independent observation

Recovery 29 stopped before its acceptance boundary because the initial snapshot
retried an unsigned probe repair whose estimate exceeded its signed gas-unit
limit. Cancellation then interrupted the independent evidence census. Repeating
the same operation could not supply the missing authority.

For PH-01, PH-02 and PH-05, separate observation, signing permission and final
acceptance. An explicitly provisional observer may retain an accounted repair
liability after a typed refusal raised before signing. Require the exact signed
action and gas cap, validated custody/accounting, a durable failed journal
frontier, and proof that no matching signed transaction exists, including orphan
transaction files saved before their broadcast record. Keep standalone repair,
mixed integrity/storage errors and other budget or fee refusals hard. Any change
to transaction authority still requires its own explicit signed amendment.

Under the same exclusive writer, reuse that refusal only while its plan, action,
authority and action-journal frontier remain unchanged. Reauthenticate those
inputs on each observation; do not append identical intent/failed records or
repeat the full signature census. Preserve the pending liability and completed
receipts in evidence. Strict final acceptance must still require complete,
verified custody recovery.

Simulator commits `8369f96a` and `16e9896a` implement this narrow continuation.
Eight focused normal and race tests passed, covering observer survival without
another send, durable failure requirements, orphan signatures, joined errors,
standalone scope and unchanged final rejection. See
[the regression](../sim-testnet/precompile_recovery_gas_pending_test.go).
Production integration and operational acceptance remain required before closing
the corresponding hardening items.

### Publish complete artifacts for failures before the first observation

An initial snapshot failure left a terminal result and process-log evidence but
no observation file, so the next recovery could not authenticate its predecessor.
For PH-01 and PH-09, publish an explicit zero-observation marker before the
terminal result when no observation or acceptance boundary exists. Propagate
append, sync and publication errors before claiming a complete terminal artifact
set. A failure to record evidence is a separate hard failure.

Legacy repair belongs to the authenticated recovery writer. It may add only a
missing marker after validating the exact failed result, zero recorded
heads/epochs/observation hashes, absent acceptance/start markers and the matching
process-log evidence. Preserve the original result bytes and all existing
observations. The next signed recovery binds the new marker; read-only validators
must continue to reject missing or changed sources.

Simulator commit `2a6340b1` implements this scoped repair. Thirteen focused tests
passed normally and with race detection, including a real initial-snapshot
failure followed by recovery-chain validation, legacy backfill without result
mutation, write-error propagation and observed-progress/source-substitution
rejection. See [the regressions](../sim-testnet/scenario_initial_observation_test.go).
Production crash-publication qualification remains required.

### Keep release intervals running while classifying adversary failures

R34 entered its signed release interval and continued making finalized-block
observations while three adversary probes found hard errors. RPC consistency
timeouts were successfully recorded as pending and then recovered. Separate
operator artifact GET timeouts and verification `503` responses remained hard
findings. A healthy fleet and advancing block head therefore show liveness,
not final acceptance. Production should retain the exact failed probe, actor,
target, block and fault window without terminating an otherwise useful interval;
the final gate must still reject unresolved required probes. Recovery must never
turn an unanswered read into a verified mismatch or a skipped probe into coverage.
Operational status must group pending and recovered rows by recovery ID: historical
pending rows remain after recovery and must not be counted as open incidents.

Give each HTTP operation its full configured attempt deadline before bounded
retry. Dividing a ten-second sample into short attempts canceled artifact reads
that were completing in roughly three seconds, creating failure during normal
load. Retain one overall budget, retry only classified transport and server
availability errors, and require the original content-addressed validation of
every nonempty recovered response. An empty history after timeout carries no
coverage. Test slow successful reads, timeout followed by success, exhausted
retry, empty history, malformed response and cancellation through the full actor.

For expected production GETs, use at least 60 seconds total and default to a
five-minute retry horizon when no tighter protocol deadline applies. Give each
attempt a real response deadline, back off between transient transport failures
and retryable server responses, and preserve the original request identity and
hash expectation throughout. A missing object, authorization refusal, malformed
response or hash mismatch is a semantic finding; repeated transport success
cannot waive it. Long retries must not hold the release heartbeat or silently
extend a signed fault window: persist the pending read, let unrelated work
continue, and complete or fail that exact read within its own bounded horizon.
Test an outage lasting longer than 60 seconds, recovery before five minutes,
exhaustion, cancellation and a fault-window transition during retry.

Fault admission must distinguish a scheduled trigger from a physically active
pre-arm. R34 installed exact validator-view exclusions before the release epoch
so the quality-fault boundary could start safely, but the verification actor
selected those excluded miners while their signed trigger was still pending.
Pre-arm the exclusions before any dependent fleet lifecycle action; publish
their exact target scope before installing physical files, and select probes
only from the eligible census. Across recovery, adopt only the signed filter
rules and verify their bytes, private-file mode and process ownership. Retain an
existing filter file without replacing its inode until the authorized restore;
reject unrelated faults or partial restoration. If an older predecessor removes
the filter during shutdown, record that continuity gap explicitly; a successor
that installs a fresh filter cannot claim uninterrupted protection.

Qualify the whole rollover, not only the candidate binary: authenticate the
sealed predecessor and signed invalidation, pin evidence hashes, verify the
retained fault registry and physical filters before service replacement, then
check their permitted state after the new supervisor starts. Require the exact
binary provenance, manifest, 33-process identity and healthy fleet before the
next interval. The handoff may tolerate a typed provisional predecessor and
recoverable transient errors, but must reject changed authorization, substituted
evidence, unexpected fault controls and missing physical safety rules. Exercise
pending pre-arms, retained filters, intentional absence after old cleanup,
interrupted install, changed registry, partial restore and rollback in tests.

**PH-27 — Policy rollover must include validator evidence activation.** R40
failed its live release interval while relaying a closed-census header for epoch
594. The signed header carried policy hash `0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277`,
but the finalized coordinator `policyAt(594)` returned
`0x41f0c7efe7e1b23b2fd22dac9352ca18be48d2e4d1fb5b41ce89660bc899b0dd`.
The LAN-node `eth_call` returned `InvalidEvidence()` (`0xc9779e3c`); the
write-once slot was empty, so rebroadcasting the same signed bytes cannot
repair it. The old activation was published at block 7,975,571, well before
the policy-v2 effective epoch. This is a policy-era authority mismatch, not
an RPC timeout or a nonce race. The original failed action and result remain
in the R40 evidence bundle.

Before mainnet, make a policy transition atomically schedule a new validator
evidence activation for each controlled operator and both validators, with
dual-key consent, publication and finalized readback before the first epoch
whose evidence uses the new policy. Bind the new activation to that policy and
its actual future epoch; do not rewrite historical signed headers or backdate
activation. The relay must compare each header's policy with finalized
`policyAt(header.epoch)` before budget admission or gas estimation, retain an
explicit unpublishable historical gap when they differ, and continue to valid
future slots without misreporting the gap as accepted coverage. Acceptance
must require every counted epoch's headers to have valid policy-era activation
and on-chain commitments. Test rollover at the exact effective boundary,
delayed deployment, interrupted activation, mixed old/new validators,
historical mismatch, and resumption after an unpublishable gap.
The existing activation-history file replays only legacy-to-V2 closures; it
cannot attest a V2-to-V2 rollover. The production handoff must verify the
latest signed V2 terminal closure and bind each new activation's
`firstSequence` and `priorRoot` to that operator's exact terminal cut while
writers are fenced. Preserve the old activation and ledger as immutable
history; verify the new policy at the activation epoch and start the new
producer only after all four new publications are finalized.
Do not restore the old policy merely to make old activation signatures
publishable: testnet policy v2 materially changed pool rates, and labeling
those rates with the old hash would misstate the governed policy. A fresh VPK
and namespace is a different measurement source, not a continuity proof;
admit it only through explicit source-generation and public lineage controls.
Classify an early root-commit deadline alert as a pending retry, retaining its
exact log evidence. A warning while time remains is not a missed deadline:
testnet epoch 597 warned with 7m48s left and both roots confirmed 37 seconds
later. Keep imminent/passed deadlines blocking and independently verify the
finalized root and deadline before accepting the epoch.
If an on-chain policy change makes an old signed measurement source
unpublishable, an explicitly new VPK/source generation can start at a future
untouched epoch without pretending its sequence, EMA, or ledger continues the
old source. Production needs one authenticated handoff controlling validator
directories, client identities/JWTs, API admission contexts, relay routing and
collector identity selection; preserve the old signed evidence and account
for the excluded gap separately.

### Count paid and free provider usage equally

For PH-12 and PH-13, keep the SN usage ledger independent of customer billing.
R35's signed artifacts for both operators in epochs 585 and 586 contained no
provider rows and zero total usage, although each operator had settled hundreds
of thousands of same-network contracts that day. The producer read only
`transfer_escrow_sweep`; those contracts correctly created no customer charge or
billing sweep and were consequently invisible to SN. The last positive billing
sweeps predated the campaign by eleven days. Creating paid sidecar traffic would
mask this accounting defect and is not its repair.

The required rule is that all valid paid and free provider bytes contribute
equally. Record immutable per-contract/provider usage independently of balances,
revenue and financial payout suppression, including same-network participants.
Bind direction, participants and settled byte evidence before mutable stream
membership or contract cleanup can change their attribution. Preserve exact
byte conservation, one-time settlement, explicit dispute outcomes, nonnegative
amounts and the exclusion of unfinished work. Customer billing and its existing
payment rules remain a separate consumer.

Keep the governed NO deposit/rate/quality formula unchanged. The signed artifact's
`total_usage_bytes` feeds the existing prior-epoch required-deposit calculation;
validators authenticate the artifact and audit that amount before weighting
`deposit / rate × Q`. A free-service operator funds the same required deposit
for the same usage. Customer payment status must not affect usage, relative
provider shares, required deposit or weight. Head exclusion, reliability floors,
deposit caps, tier snapshots and mismatch rejection remain strict.

The original incident required a new server usage producer. By R48 the
prospective usage path and the separate `74893863` legacy-quarantine fix had
implementation evidence; production composition and end-to-end qualification
remain open. Required regressions compare otherwise identical paid, free and
same-network transfers; cover forward/companion and multihop attribution,
duplicate/concurrent close, partial and disputed close, cleanup, exact epoch
boundaries, mixed financial allocations and zero usage; and trace equal usage
through artifact totals, required deposits and validator audit. Preserve every
already signed historical artifact and its original interpretation; deployment
must establish an explicit prospective accounting boundary. Earlier same-network
reply contracts could lose their companion/origin role during billing
normalization, so historical endpoint rows cannot safely recover the provider.
Do not backfill guessed usage or replace the signed zero-usage artifacts. Start
the new accepted measurement window after deployment of the usage producer;
an epoch crossing activation cannot silently count as complete. Closure requires
nonzero eligible provider artifacts, timely root commits and successful claims
for both operators in the accepted interval. Connect idle recovery alone does
not establish this settlement coverage.

### Size governed rates against the native movement minimum

Positive valid usage does not guarantee an executable demand deposit. The
coordinator moves the exact governed amount, while the native runtime and
immutable vault enforce a TAO-denominated transfer floor after alpha conversion.
A rate chosen without measured epoch bytes can produce a valid amount several
orders of magnitude below that floor. Increasing the transfer to the floor
would break the deposit audit and must remain forbidden.

Before mainnet launch, preflight the exact integer floor, epoch cap and reserve
rounding across every conviction tier and conservative observed epoch usage and
price states. Require at least twice the native minimum as operating headroom,
then repeat the exact minimum check immediately before each actual deposit.
Govern future rate changes explicitly and authenticate both old and new policy
documents; preserve the prior signed artifacts and original activation domain.
The deposit/rate/quality weighting rule remains unchanged. A uniform rate
increase changes operator funding economics even when uncapped relative demand
is preserved, so it requires a new reviewed policy and future activation.

The testnet correction retains both policy files, existing caps and one-epoch
usage lag. Its [rollout procedure](../sim-testnet/POLICY-RATE-AMENDMENT.md)
requires the two additive database migrations before service adoption, a
prospective immutable usage boundary, legacy-contract drainage, a complete new
policy source epoch, and a signed source/price/minimum readiness proof. There is
no historical backfill or acceptance waiver. Mainnet parameters require their
own measured economic and native-minimum qualification.

### R42 continuation: scheduler, recovery cache and fixture authority

The live R42 release interval exposed validator steering exits after repeated
native scheduler reads timed out during the injected RPC-proxy fault. A read
timeout creates no intent and does not establish a changed epoch or bad
evidence. Production steering must retry typed transport failures across
normal polls without spending the native submission-failure budget or clearing
the completed epoch, pending cut or unresolved submission. Cancellation still
ends owner work; epoch regression, joined integrity errors and actual
submission failures remain hard errors. The offline correction is covered by
deterministic transport, pending-cut, cancellation and regression tests; R42's
already-running fleet keeps its original executable and its failures remain in
the run evidence.
Another R42 exit occurred after a compact live-head binding census timed out
on an `eth_call` at a pinned canonical block while advancing an incomplete
native epoch. The timeout supplied no differing block hash and must be
retried as a transport interruption using the same pinned read context and
retained pending cut. A retry must not fabricate a completed census or turn
the timeout into a reorg/integrity error. Cover batched EVM reads, individual
GETs, source snapshots and native scheduler reads with the same typed policy;
keep actual changed canonical hashes and malformed results hard failures.

R42 validator 2 also exhausted steering retries after switching to a fresh
measurement source while retaining its native hotkey. The finalized native
source commitment slot still matches an applied, signed intent in the older
source generation, but the new generation's local intent store is empty. The
current role check therefore treats its own historical commitment as an
unretained write. Before mainnet, authenticate a narrow predecessor-source
handoff (exact hotkey, finalized commitment hash/block, owner and immutable
intent) across source generations. Never accept an occupied slot merely
because the hotkey matches, and never invent a missing local intent. Cover
legitimate retained predecessor, different role, altered hash/block, missing
signature and retry after interruption in deterministic tests. The R42 exit
and restart remain findings even if a later executable fixes this class.

R42 also reauthenticated 42 historical recovery generations on each live
checkpoint because mutable current journal and attempt files invalidated a
cache witness for otherwise immutable predecessor evidence. A mainnet recovery
cache must authenticate immutable generation sources once, verify the retained
journal prefix and only the appended suffix on continuation, and check the
current envelope afresh. It must reject modified old bytes, missing or
reordered entries and forged tails, including concurrent append during a cold
audit. Cache identity must follow authenticated evidence and authority rather
than executable hash or a mutable file's whole hash. Keep cache loss
recoverable by full verification, with bounded work and memory.

Finally, repository fixture generation must select an explicit supported
server manifest profile before comparing resources. The current pinned server
uses the 30-resource legacy geography profile; a proposed 28-resource
GeoLite profile is not an available server API. Reject mixed, incomplete and
unknown profiles, and guard missing decoded configuration or policy before
projection. Fixture tests must validate the selected source's real schema and
bytes; exporter parity remains pending until the exporter exists in the pinned
server source. These are build and qualification safeguards, not evidence that
R42's live acceptance has passed.

Terminal diagnostics must distinguish the chain's terminal block from the
runner's signed terminal result. A failed provisional interval may continue
until its watchdog after the block, so a short result-file wait can produce an
early inventory that lacks the final failure set. Keep the live runner and
read-only auditor independent; collect at the block for timely diagnosis, then
collect again after the exact signed result appears. Bind both inventories to
the same run ID, plan, boundary and source hash. A diagnostic report never
creates a pass marker or substitutes for the original signed result.

R44's first terminal capture also compared EVM addresses by presentation text:
a checksum-case validator configuration and a lowercase signed measurement
named the same coordinator and vault, but the collector rejected them. Validate
each complete address before comparing its 20-byte identity. Do not repair this
by remarshal, relabeling signed bytes, permissive padding/truncation, or ignoring
chain, genesis, policy and intent checks. Apply the same rule to historical
client-key decisions and artifact observation requests. Regression tests must
accept checksum/lowercase equivalents with original bytes unchanged and reject
different deployments, malformed addresses and wrong chain domains.

An external non-accepting terminal diagnostic must retain its admitted plan
when reading provisional companion and relay evidence. That read authority
does not reconcile the plan for strict acceptance or authorize any mutation.
Creation, anchoring and activation transactions keep their original approved
ancestor, ordered broadcast/finality/verification rows, exact signed transaction
and hashed postcondition. Authenticate the immutable archive and current
approved ancestry before selecting them; closed pre-broadcast attempts cannot
replace or invalidate a later authenticated original transaction. Keep the
ordinary collector strict and test both read scopes, changed command/config/RPC
authority, competing finality and tampered archives, signatures and receipts.

Keep independent diagnostic obligations separate. A missing lifecycle payout
index must remain unavailable, but it must not prevent collection of ordinary
signed acceptance-window payouts. Preserve the original observation and every
exception, validate ordinary signatures, content hashes and epoch coverage,
and report malformed lifecycle evidence as failed rather than merely absent.
The strict combined collector must continue requiring both scopes. Test this
with original signed bytes and an accepting-owner negative control; a useful
partial diagnostic is never evidence that the full qualification passed.

The production cadence scheduler and its receipt verifier must share one
finalized policy-history reader. R42's coordinator already has three versions
because an approved rate amendment added one before production; a fixed
two-version gate rejected that legitimate history. Admit only the exact
approved predecessor and amended policy, then one future production version;
pin every read to one finalized block and verify the append-only indices,
effective coordinates, active snapshot and receipt. Reentry after a partial
write must recover the same transaction without scheduling a fifth or using
an unreviewed policy version. The testnet correction is `402e6b1b`; mainnet
must rehearse its own approved policy sequence before launch.

An operational continuation from a failed testnet release must retain every
failed assertion and a distinct non-accepting gate. Before production can
start, authenticate the exact signed terminal source and result, recompute
custody and identity checks from the signed observation, and require every
scheduled release fault to be restored in both records with a subsequent
signed observation. The active-fault recovery ledger must be empty. Keep
historical lifecycle plan identity separate from the current plan authorizing
new actions. This permits diagnosis to continue without laundering a failed
release into a strict pass or overlapping old fault injection with production.

The R45 renewal review exposed the same authority distinction in retained
validator generation readers. Round seven appends a new fleet approval, but
the activated generation and its owner-signed source-role overlay still name
the original round-six approval. Requiring their source hash to equal the new
active plan would reject an otherwise unchanged retained restart. Resolve the
original approval only through its immutable archive and the current approved
ancestry; authenticate the exact deployment, evidence custody, configuration,
policy and owned RPC authority before reading the original generation bytes.
Never substitute a hash onto different configuration semantics. New rollover
or source-role mutations still require the exact current approval.

Qualify this boundary with real signed round-six to round-seven renewal
fixtures, unchanged manifest inventory and overlay bytes, repeated retained
reads, and refusal of a fresh mutation under the historical approval. Missing
or tampered archives, unrelated lineage, changed deployment custody and changes
to configuration, policy or RPC authority must remain hard failures. Include
launcher, manifest, observation and relay readers in the same migration test;
a successful doctor before renewal does not exercise the descendant-plan seam.

R44 exposed an impossible restoration predicate after its explicit testnet
lifecycle bypass: the bypass correctly retained no terminal-effective mutation
epoch, while a local companion filter required that epoch before removal.
Separate operational cleanup from proof that a lifecycle transition occurred.
A diagnostic successor may remove only the exact two authenticated local
filters after the full signed interval and their minimum durations, retaining
`RestoreConditionMet=false`, the original failed assertions, and
`final_acceptance=false`. Mainnet acceptance must still prove the actual
lifecycle transitions; diagnostic cleanup is not a substitute. Installed, paid,
and effective mutation predicates must not infer success from an operational
handoff stage reached through a bypass.

Checkpoint the exact cleanup request before touching the filter, retain the
removed target/role/identity census, and date completion from a subsequent
complete observation. If removal outlives its active ledger entry, reconcile
only through the retained plan/operator/rule-bound removal receipt and proof
that the exact rule is absent. Ordinary restore must not acquire this special
missing-ledger authority. A public evidence file written before its owner
checkpoint is not authoritative: recovery reads the signed fault state and
independently reconciles the physical outcome. Rehearse both interruption
windows, foreign receipts, reappeared rules, and the failed-release to
non-accepting production handoff without changing strict acceptance.

R42 ended before terminal because a provisional heartbeat treated a known
validator steering-continuity finding as a reason to stop the entire interval.
Production should keep collecting through recognized provisional findings and
report the full set at final acceptance; unknown process-log classes and
integrity failures remain hard. An interrupted process-restart fault must
checkpoint its exact signed target generation and retry a bounded health
observation on later heartbeats. A stuck child may need a targeted operator
repair, but neither a retry nor a restart may erase the active-fault ledger
without observing a different healthy supervised PID. Add deterministic tests
for mixed known findings, an unchanged unhealthy PID, replacement recovery,
and a changed manifest identity.

The source-role rollout's read-only native precheck met a new runtime 471 while
its retained config pinned 467. Every current-runtime reader, including
review/apply commands, must install the explicitly approved provisional
compatibility profile before authenticating the live artifact; historical
source signatures and blocks remain exact-pinned. Test a consumed-interface
successor and a real metadata/API incompatibility separately. A precheck must
not rewrite the original config or signed campaign evidence.

R45 qualification exposed a second runtime-version trap in tests rather than
the fleet code: four current-runtime fixtures still expected spec 461 while
the production constants, lockfile and reviewed artifact already agreed on
467. Mainnet's current-runtime tests must authenticate the independently
reviewed source commit and metadata hashes for the selected launch artifact,
then require production selection to match that evidence. Keep older versions
as explicit historical decode/rejection cases; an old fixture must not silently
become current authority. Run the full miner and on-chain suites normally and
under race detection after changing the launch runtime pin. The testnet repair
is SN `5a53b33c`, with the old-fixture tests causally reproducing all four
failures.

R43 startup stopped at a stale operator overlay resource list: the current
pinned server reads `mmdb/ip-ipinfo.mmdb` and `arindb/arin.mmdb`, but the
simulator demanded future `geolite2.mmdb` and `places.yml` files absent from
the selected config repository. The production preflight must derive required
resources from its pinned server/config profile, validate every required file
before topology stop when possible, and test both the supported legacy and
future profiles without mixing their manifests. A missing resource must be
reported precisely; it must not be fabricated or silently linked to a
different database schema.

R44's second terminal diagnostic could authenticate the signed start but its
newer checkpoint reader rejected `start_time_ticks` inside retained fault
process records. Historical signed evidence is a compatibility contract:
retain known optional nested wire fields even when the producing runtime
feature is no longer active. Decode them with bounded types, preserve original
signed bytes and hashes, and reject unknown or malformed fields and generation
rewrites. Test both legacy omission and a fully signed historical checkpoint
through the current forensic and recovery readers. Recognizing an old process
identity in evidence never grants authority to signal that process.

R44's later read-only terminal capture exhausted a 15-minute deadline while
reading a retained validator source: 62 cuts scheduled about 1.63 GB of chunk
GETs across two origins, and a verified 4 MB chunk alone took 18.30 seconds.
Production evidence readers should bound the whole job from measured bytes and
throughput, expose per-cut progress, retain completed authenticated chunks,
and reuse only exact immutable origin/kind/hash/size matches. Retry incomplete
HTTP bodies within that finite budget and distinguish budget exhaustion from
invalid signatures or conflicting content. Test slow, interrupted, duplicate
and conflicting chunks without reducing final integrity checks or restarting
unrelated runtime work.

R45 source review found eleven previously qualified recovery fixes absent from
the candidate main branch. A passing component test or isolated branch is not
deployment evidence. Before mainnet launch, derive the release image from a
reviewed dependency-ordered commit inventory, compare every changed source
file to its qualified hash fence, and run the affected composed normal/race
tests on that exact source. Include durable snapshot retry, transport error
classification, original-child signaling proof, write-ahead fault intent,
pending container restore and post-transition completion heads in the
composed recovery rehearsal. A documentation-only main advance should not
change the approved executable, but it must not conceal a missing code patch.

### R48 terminal: process-log throughput, native history and immutable usage

The post-R48 scanner patch on `main` drains the observed log size in bounded
64 MiB segments, checkpoints each completed cursor before the next read, and
resumes from that cursor after an interrupted read. Its causal regression
fails against the R48 scanner and passes with the patch; nearby log-gate tests
pass in the aligned R48 source workspace. This is a code repair, **not** a
retroactive R48 acceptance result or proof that the live supervisor uses the
new binary. The retained R48 process-log journal uses classifier v13; the
patch carries that classifier forward so a later reader can load it without
downgrading its findings. The missed native-1696 history and incomplete
acceptance interval remain open.

The post-R48 root composition could not compile `go test ./sim-testnet` with its
adjacent `server` and `sdk` checkouts: the captured server called the older
three-argument `protocol.RequiredDepositRao`, and the captured SDK lacked the wallet
challenge context method and `Purpose` field used by SN. Lock compatible
revisions of the complete production dependencies and qualify the combined
release. The new signer-free mainnet observer has its own passing focused
normal/race tests; it does not establish that combined build. The scanner regression was run in the already aligned
R48 source workspace with the changed files overlaid; that result must not be
misreported as a passing build of the current root checkout.

[R48's final report](../sim-testnet/FINAL-4.md) records a signed acceptance
boundary but no fully observed acceptance epoch. The immediate stop was the
process-log heartbeat: both operator API stderr streams had already exceeded
the 64 MiB scanner delta before acceptance, and the unchanged cursors produced
fresh `log-overrun` findings after the boundary. Mainnet's process-log reader
must consume large growth in bounded, hashed chunks with a durable cursor,
not classify an ordinary large backlog as permanently unscannable. Keep a
separate hard failure for inode changes, truncation, changed prefix, unreadable
bytes and overlong individual lines. Drain and authenticate any pre-boundary
backlog before binding the acceptance boundary, or disclose and sign an exact
exception; do not redate the same old gap as a new runtime event. Test a
multi-gigabyte noisy stream, concurrent append, a scanner crash between chunk
and cursor commit, rotation/truncation, restart, and a short scheduled fault
while the backlog is drained. Completion must show every byte accounted for,
bounded memory, and a fault heartbeat that still meets its block timing.

The other release-blocking class was validator steering repeatedly reporting
that strict history adoption missed the approved first native epoch 1696.
There is no authority to backdate that application. Before mainnet, provide
an owner-signed forward selection and handoff for **both** validators: retain
the old intent and EMA as historical evidence, choose a future native epoch
with a measured startup margin, checkpoint the exact source and transaction
outcome, then prove finalized on-chain weights from both validators at that
new boundary. A restart, stale runtime change or interrupted read must resume
that same authorized plan without inventing a prior success. The test must
cover a missed first epoch, pending transaction reconciliation, process
restart, exact signed history, and independently queried final weights.

R48 also found 4,183 settled epoch-658 contracts closed by a mixed-version
operator fleet without immutable `provider_usage`. The guarded one-time
testnet repair retained at least 3,082,728 bilateral-report bytes as explicit
uncredited debt (`final_acceptance=false`), allowing ordinary close to proceed
without guessing providers. Mainnet must deploy the snapshot writer and
reader as a compatible fleet, check for missing snapshots before settlement
and before release admission, and reject positive credit reconstructed from
mutable membership. If a historical repair is unavoidable, it needs an exact
whole-row/report manifest, serializable changed-row and complete-census
guards, a canonical per-row debt receipt, replay refusal, and a final report
that distinguishes omitted usage from paid usage. Rehearse a mixed-version
rollout and rollback, a canceled contract, altered bilateral report, expanded
NULL census, wrong epoch/ID/close time, and interrupted commit. The qualified
testnet server source is `74893863`; it is evidence for the design, not proof
that the mainnet release image contains the fix.

**P0 retention follow-up (MG-06, PH-09/12; In progress, 2026-09-27).**
Before the correction, seven-day completed-payment retention deleted
`transfer_contract` rows and their `provider_usage`, while the epoch reader
queried only the live table. Server `9f860731` now implements an append-only
archive and one-snapshot live/archive reader. Exact usage is captured atomically
with deletion; failed capture aborts deletion and duplicate credit-bearing
identities fail the window. Pinned and combined normal/race selectors passed,
as recorded in the integration note above. This remains an open production gate:
coordinate reader/writer/reaper cutover, prove the deployed schema and replay,
and qualify durable archive capacity. Already deleted rows remain an explicit
historical gap; do not backfill guessed providers or reconstruct positive credit.

R48's local epoch-658 close missed its on-chain commitment window, while
epoch-659 roots later committed on-chain. The mainnet gate must distinguish
that real chain progress from successful acceptance. It should continue
collecting a terminal report after recoverable process findings and clean up
exact pre-armed faults after any terminal failure, while keeping the original
failed assertions and signed boundary immutable. A partial or provisional
release must never be silently promoted to mainnet launch approval.

### October 2: physical recovery must preserve application ordering

The immutable-member work exposed a boundary error: a generic storage opener
completed a retained terminal or policy stage before the application performed
its canonical reconciliation. In the failed `8d37e7a5` adjacent gate, both
interrupted-ordering tests correctly refused that behavior. Retaining exact
bytes is necessary, but does not alone authorize declaring an action complete,
advancing runtime or policy authority, consuming a later action, or sending
again. The [continuation checkpoint](evidence/mainnet-continuation-checkpoint-20261002.md)
retains the failed source and the full 20-root census.

Across miner, validator, operator and bootstrap owners, recovery of a physical
reservation may materialize only its exact authorized missing bytes. Existing
staged outcomes remain staged until their owning application reconciles them.
Recovery must retain nonce/attempt floors, original runtime/policy identity and
all signed attempts. A read-only observer may report pending state but must not
promote it. Any durable-write uncertainty joins the affected owner before a
replacement opens; unrelated roles continue.

Qualification must cover interruption before stage creation, after stage fsync
and after publication, with exact retained payload and no additional send.
It must prove a pending old outcome refuses a newer runtime/policy action,
then completes only after canonical reconciliation. Test maps must distinguish
immutable signed protocol members from explicitly mutable authenticated census
heads. Exclude only the named mutable head from a positive immutable-byte check;
retain whole-directory no-effects checks at refused admission boundaries.

### October 2: every public read path needs its own retry contract

The manual provider claim command directly reads epoch and payout data through
the SDK, unlike the retained claim daemon. The selected SDK/Connect defaults
allow only one quick 502/503 retry, so a healthy service recovering moments later
can still make this command fail in under 60 seconds. A daemon receipt cannot
prove the finite command's behavior. Each public role must identify its actual
GET owner and retain a minimum 60-second transient retry budget, normally 300
seconds for expected available data. Transient network, timeout and gateway
failures retry the same immutable read request; authentication, malformed data,
wrong identity and proven integrity failures return typed permanent causes.
Cancellation joins outstanding requests and body readers. Signed wallet POSTs
and transaction broadcasts require durable outcome reconciliation, not this
read retry policy. This manual-command correction remains queued for causal
implementation and qualification; the source audit is not a fixed-code claim.

### October 2: isolate callback failures and qualify current dependency composition

The actual SDK JWT persistence callback now names a local storage cause and
quarantines only its member generation. Its owned teardown runs outside the SDK
callback, retains the slot until join, and prevents stale callbacks from stopping
a successor. Joined authentication or unknown causes remain terminal. Startup
admission retains healthy members after classified temporary failures without
blindly repeating a possibly signed wallet request. The qualified implementation
and deterministic old-body controls are [recorded here](evidence/provider-callback-qualification-20261002.md).

A published dependency version can omit required packages even when previous
local source passed. Current server intake exposed exactly that: Connect
`6443417d` lacks `durablevolume`. Preserve the failed exact-graph attempt and
qualify a compatible published successor; a local replacement cannot prove the
published graph. Module floors from unrelated upstream work must be preserved.

Cache cleanup must use bounded qualification manifests and process ownership
checks rather than scan every raw artifact as a testing prerequisite. This
iteration reclaimed 16 GiB from 221 inactive Go archive entries, preserving
source, module caches, executable cache entries and active caches. Each planned
inode was rechecked before deletion and absence verified afterward. The retained
receipt is `/mnt/data/sn-testnet/root-mainnet-cache-reclaim-2-20261002/receipt.json`.
Reference checks covered declared receipt/manifest/checksum/document classes;
they do not assert absence of references in every raw artifact.

### October 2: validate the whole local test graph before compilation

The independent member-recovery checkout initially omitted five local sibling
modules. Compilation stopped at the first missing import, before any product
test ran. Preserve this as a harness setup failure rather than a regression or
a passing qualification. A reusable preflight must resolve effective workspace
overrides before inspecting local replacements, report all missing paths and
pin mismatches together, and verify the staged source identity. An obsolete
replacement overridden by go.work must not create a false missing-path alarm.
The corrected member checkout has a passing graph preflight; its normal/race
qualification remains a separate result. This preflight does not replace
compiler checks, published-module qualification or release provenance.

### October 2: preserve reader sentinel contracts at guarded I/O boundaries

The first actual storage preparation CLI candidate compiled, then both public
tests failed at LevelDB inspection with EOF. Its new reader returned
`errors.Join(readErr, contextErr)` even when contextErr was nil, wrapping the
ordinary `io.EOF` sentinel. LevelDB's journal reader requires the original
sentinel. Preserve the underlying read error unchanged when the additional
guard succeeds; join only genuine additional causes. Never discard an integrity
or cancellation failure merely because another cause is EOF.

The adjacent existing validator spool at
`validator/attempt_cut_v2_seal_scratch.go` also joins every read error with its
postcheck. That is a concrete audit lead assigned for actual consumer-level
reproduction and correction, not a proven fixed behavior. Deterministic tests
must cover a complete stream, empty stream, short read and EOF together with
a genuine failed guard. Tests of `errors.Is(err, io.EOF)` alone cannot prove
compatibility with consumers that require an unwrapped sentinel. The original
preparation failure remains retained separately from later source qualification.

### October 2: reuse compiler work without reusing test outcomes

The independent member-review gate used a new private Go build cache and spent
minutes compiling unchanged normal/race dependencies. Subsequent selected
tests should reuse the reviewer's own existing build cache on /mnt/data while
keeping exact source/module pins, private working data and `-count=1` actual
test execution. Preserve a live compilation rather than restart it just to
change cache policy. Go's compilation cache and an authenticated runtime proof
cache have different contracts; neither permits reusing current balances,
nonces or test pass results. Independent release/compiler provenance remains
a separate MG-02 gate and is not established by these cached test runs.

### October 2: cancellation after durable preparation is uncertain progress

The preparation controls reproduced a cancellation after control-header fsync
that returned only context cancellation, despite retained progress. After a
durable header or root reservation may exist, report exact-plan readback
uncertainty and retain the original plan; do not let the caller treat this as
an unused preparation and generate another nonce or generation. Admission
pressure before effects remains separately retryable. Test cancellation and
lost acknowledgements at each publication boundary, including short report
delivery after successful preparation. The successor remains under qualification.

The [retained-member qualification](evidence/successor-member-qualification-20261002.md)
now independently reproduces all five lost-history/inode defects and passes
ten selected normal/race controls plus vet. Integration with the newer current
observer is a separate pending scope; those results do not imply restore or
offline owner enrollment is complete.


### October 2: guard failure must prevent full-buffer acceptance

Returning a full byte count together with a custody error is insufficient for
standard consumers: ReadFull can drop that error and a decoder can accept a
complete object. After a failed post-read guard, admit zero bytes while retaining
the true descriptor position and cause. Preserve bare EOF on an ordinary finish.
Test ReadAll, Copy, ReadFull, JSON, direct EOF and the actual production descriptor
reader, with cancellation and named-inode replacement at the read boundary.
[Author evidence and pending independent scope](evidence/current-graph-reader-progress-20261002.md)
retain the earlier correction and its four concrete full-buffer counterexamples.

### October 2: runtime continuation needs caller-owned GET retry budgets

The actual finite claim command on unchanged main made five parallel-route GET
attempts and panicked on a typed 503 after roughly 0.6 seconds, despite an outage
that remained transient. Route hedging and a small transport retry are not the
command's retry budget. Give epoch/pool reads a caller-owned 300-second budget,
retry classified transient failures, join cancellation and return errors rather
than panic. Test a sustained outage through the real public command and SDK;
keep signed submission outside read retry. The first cardinality-only fixture
was invalid because route attempts consumed its synthetic failures; retain it
as a harness failure, separately from the corrected causal controls. The fix is now merged after nine affected author and independent tests pass
in each mode, plus vet; two old public read-call controls fail as expected in
each mode. [Independent evidence](evidence/finite-claim-independent-20261002.json)
retains the corrected graph setup and exact source. The sustained-outage
SDK/HTTP requests are real, while the retry wait clock is accelerated to model
65 seconds per read. The deadline test likewise uses an injected clock. Neither
is a wall-clock production outage rehearsal; report clock fidelity explicitly.


### October 2: preserve qualification headroom through scoped cache cleanup

A further [8.04 GiB reclaim](evidence/cache-reclaim-3-20261002.json) removed
122 inactive Go archives from the old cache, each at least 48 hours old and
16 MiB, after privileged process-reference checks, archive-magic/link/stat
checks and a bounded declared-metadata reference scan. Repeat physical identity
checks before removal and verify absence afterward. Preserve active compilation,
source, modules, immutable evidence and executable artifacts. A new private
member cache had no large same-key duplicate candidates in the shared reviewer
cache and was preserved. Compiler-cache cleanup does not change source or test
qualification and does not establish global raw-artifact reference absence.


### October 2: review each guarded reader's actual consumers

The post-read full-count issue also appears in Server durableBlobReader.Read
and localBlobCapacityReader.Read, and the new preparation reader. Each returns
read bytes alongside a failed post-read guard. These are assigned for exact
consumer-level reproduction and narrow successors. Preserve successful frozen
scope receipts; do not mutate them or label source inspection a reproduced
production failure. Distinguish exposed standard consumers from outer copy or
ledger inspectors that already recheck the error/context before publication.


### October 2: a running chain can upgrade during offline launch preparation

The public archive advanced from observed v470 to exact finalized v472 while
preparation continued. Retain the [new observation](evidence/runtime-472-route-observation-20261002.json)
as unapproved intake, with raw code/metadata, hashes and block identity. An old
artifact exception does not approve its successor. Continue unaffected reads,
historical recovery and offline implementation; admit a current signed operation
only against an independently reviewed consumed interface and exact current
artifact. Runtime catalog selection must distinguish original receipt execution,
current observation, pre-sign admission and pre-broadcast recheck. The failing
Snow route supplies no replacement identity or permission to retarget signed plans.


### October 2: progress labels must retain their evidence domain

The [actual producer map](evidence/provider-proof-settlement-hook-map-20261002.md)
shows that an unsigned claim queue can become finalized from leafClaimed state,
while signed claims require exact-transaction receipts. Neither route is implied
by provider transport readiness, and a local operator database snapshot supplies
no native finality or payment amount. Extend monitoring through the existing
owned proof/receipt/durable-flush boundaries, with independent expected pool,
contract and operator identities. Keep unavailable, zero payout, carry, pending
claim and uncertain signed liabilities distinct. Do not duplicate a shared pool's
payment across provider slots or report status before durable acknowledgement.


### October 2: resumed custody assertions need an exact allowed transition

An old blanket file-equality assertion rejected an intentionally updated retained
member-census head. Replace only that assertion with the exact approved transition:
keep predecessor member inode/hash/size, authenticate each new execution and nonce
member, preserve original approval/adoption/count/outcome authority, and pin the
complete resulting namespace and head inode across subsequent resumes. A negative
missing-receipt control must restore the original held inode, not manufacture a
replacement. The [separate fixture qualification](evidence/successor-member-qualification-20261002.md)
retains the original normal/race failure and awaits independent replay. Do not
solve this by excluding every mutable file from custody checks.


### Classify constructor reachability before changing admission

A missing optional storage context is a lead, not proof that mainnet startup
bypasses custody. The [source-bound proof-store trace](evidence/proof-constructor-reachability-20261002.json)
classifies three `NewProofStore` omissions: flag-mode measurement, informational
summary, and a legacy release helper with no direct non-test production caller.
The actual V2 startup passes its owned context into the proof-store constructor.
This static check neither qualifies runtime behavior nor closes the wider
optional-context census. Remaining constructors must be traced from their actual
public role entry before a causal test or admission change is selected. Status
and legacy measurement remain separate scopes; never weaken production custody
to make those historical callers fit the mainnet path.


### Preserve capacity for qualification without deleting active compiler work

When free data-volume space approached the 110 GiB reserve, a further
[4.02 GiB scoped reclaim](evidence/cache-reclaim-4-20261002.json) removed 63
old compiler archives at least 48 hours old and 8 MiB each. Privileged process
reference checks, archive magic, inode/link/time checks and a bounded declared
metadata scan preceded removal; physical identities were checked again and
absence verified afterward. Active compilation continued. This creates local
qualification headroom; it does not prove production sizing or replace
capacity/rotation and restored-volume acceptance. Keep cache storage explicitly
separate from immutable proof, source and executable retention.


### Distinguish accepted claims, outstanding credits and actual payments

A successful exact claim receipt establishes accepted liability, not necessarily
provider payment. The vault can emit `Claimed` followed by
`ClaimPaymentDeferred` and retain the credit. Later `ClaimPaid` transfers the
entire coldkey credit accumulated across epochs and operators. Monitoring must
report these domains separately and must not duplicate an aggregate payment
across pools or provider slots. The [source-bound semantics review](evidence/claim-payment-semantics-20261002.json)
identifies existing real contract paths and required causal controls; it is not
a new executed test result. Preserve finalized claim recovery while projecting
unpaid credit and deferred reasons. A missing history or proof yields unavailable
paid attribution, never a fabricated zero or fully-paid status. Add exact
receipt/event and contract-path tests to the actual production monitor join.


### Runtime compatibility must follow the consumed purpose

The fleet's single-artifact gate also governed status reads, historical receipts
and present sends. Its [purpose-scoped successor](evidence/fleet-runtime-catalog-progress-20261002.md)
preserves an exact reviewed catalog and checks the specific consumed semantics.
Read-only compatibility does not authorize old signature domains or replace
retained approvals. An upgrade receipt must decode execution under its parent
runtime, then independently bind the included post-state; cancellation and wrong
callback identity must be checked before event decoding. The author23 normal/race
tests and causal controls are scoped evidence; independent review, current
production artifact approval and final composed release qualification remain open.


### Qualify changed public consumers, then preserve newer upstream work

The guarded Server reader change affects all public blob consumers, not only
the new reader-contract tests. The [independent26-test scope](evidence/current-graph-reader-progress-20261002.md)
adds nine disjoint existing public consumers to17 reader tests, without repeating
the three overlaps. Both modes and vet pass, with seven causal old-body failures
and six positive controls retained. Main integration preserves newer upstream
model/monitor code and exact qualified blob/module bytes. A component receipt
remains scoped to its tested source graph; the final composed release must
qualify its current dependencies rather than inherit whole-main authority.


### Keep source composition separate from published module consumption

Fresh owner APIs must coexist with retained-member and reader recovery while
preserving dependency fixes. The [bounded preparation composition](evidence/preparation-composition-author-20261002.json)
passes17 selected normal/race tests and four package vets on its exact eight-pin
workspace. Original core bytes remain unchanged and prior receipts stay separate.
An explicit workspace can prove these source seams together, but cannot prove
the executable consumes the intended published module. Finish the tracked-module
and current Server composition gate before declaring preparation release-ready.
Directory-only owner admission and retained/restore policy remain required
behavior, not documentation-only exceptions.


### Compiler reuse must account for checkout paths

The local Go build-action implementation includes absolute package directories
in cache identity when path trimming is disabled. Different private checkouts
can therefore recompile unchanged local packages. The [source-bound lead](evidence/compiler-cache-path-lead-20261002.json)
is not a measured explanation for the current compile duration. Qualify a stable
path-trimming mode for future harness gates only after checking path-sensitive
callers and proving reuse across exact source copies. Preserve compiler flags,
physical source bindings and fresh test execution (`-count=1`). Do not change
flags or restart a live gate to gain reuse. Compiler results remain separate
from authenticated protocol proofs and test verdicts.


### Derive event selectors from the consumed ABI

Pre-qualification review of the new claim projection caught a handwritten
`ClaimPaid` topic with swapped amount/relayer types. The contract and generated
binding declare `ClaimPaid(bytes32,uint256,address)`. Use the consumed ABI's
event identity instead of duplicating its signature string. Add actual canonical
receipt controls for deferred credit, aggregate payment larger than the current
claim, malformed/ambiguous events and amounts smaller than accepted liability.
Reporting failures must degrade payment observation without invalidating a valid
finalized claim or changing its retained signature. This was found in unfinished
candidate code; it is not evidence of a deployed payment fault.

Keep evidence strength explicit as well: configured-RPC receipts and leaf state
remain assertions until independent finality/runtime/code admission is supplied.
An EVM chain ID alone does not authenticate native genesis. A Merkle check or
accepted contract event must not silently become proof of finalized economics.

The corrected [producer `347605fd`](evidence/claim-projection-progress-20261002.md)
now obtains event selectors from the consumed ABI and publishes optional public
observations only after the actual retained queue acknowledges its bytes. Author
qualification passes 37 normal/race tests and two vets; three old-body controls
fail for the intended causes in each mode with two positive controls. Independent
qualification remains pending. Fresh and previously retained optional metadata
both yield to operational queue growth at the unchanged 16 MiB limit; signatures,
entries and completed history are never removed to make reporting fit. A bounded
public census includes the oldest unresolved epoch and omitted counts. A new
publication sequence proves an acknowledged heartbeat, not settlement progress.
Future-dated or ambiguous reporting degrades without rewriting claim authority.


### Purpose-scoped fleet admission is integrated, not live-approved

The fleet compatibility correction now has independent23-test normal/race
qualification, three package vets and six causal refusals in each mode, and
is merged into main. [Exact evidence](evidence/fleet-runtime-catalog-progress-20261002.md)
retains the source/module fence and all earlier failures. This advances the
production capability-selection gap while preserving current-read availability
when writes lack approval. It does not supply source authority for observed472
or a final composed release verdict. Keep immutable original recovery plans and
signature domains separate from new capability catalogs.


### Bounded status must retain backlog and semantic progress

A bounded recent-claim projection can hide the oldest unresolved liability when
more than64 entries remain pending. Keep bounded summaries for the total and
omitted unresolved census and the oldest unresolved epoch, or a selection that
preserves old blockers as well as recent work. Never delete retained signatures
or treat omission as absence of liability.

A writer sequence, saved queue digest or retry timestamp can advance without
settlement progress. Monitor heartbeat separately from accepted/paid outcomes and
backlog age; do not reset a settlement stall merely because another retry saved.
A receipt observation timestamp later than publication must degrade observation
instead of appearing active after wall-clock rollback. These production consumer
requirements are under implementation; no complete monitoring verdict is claimed.


### Keep the next qualification phase above its resource floor

While composed preparation passed101 normal tests and entered race execution,
data free space approached the110 GiB floor. A [verified8.02 GiB reclaim](evidence/cache-reclaim-5-20261002.json)
removed132 old inactive compiler archives, preserving the live handles, active
caches, source and evidence. The same process-reference, declared-metadata and
physical archive guards used in earlier cleanups were repeated; all removed paths
were verified absent. Admission measures free space again before each new phase.
This is qualification-host resource management, not production capacity closure
or an excuse to recreate lost protocol history.


### Directory-owner preparation qualification

Frozen SN `682d568c` / Connect `05e39766` passed 77 core, 21 public
preparation-command and five miner-constructor tests in each of normal and race
modes, plus four package vet scopes. Root verified all 49 manifest bindings,
the raw archive and source bundles in the [author receipt](evidence/directory-owner-preparation-author-20261002.json).
Receipt SHA-256: `16c755189250f04d75f29c9d745016ae38a6c89420451223f255b2f80ce19d84`.
The old code reproduced two core and three public-command failures in each
mode while its negative authority control passed.

Exact attribute-only and fixed-head owner profiles now enroll through the actual
plan/apply command and reopen through real miner constructors. Interrupted
publication resumes the original inode and checkpoint; missing completed heads
and replacement markers remain refused. Existing capacity dimensions remain
unchanged, including the claim queue's 16 MiB retained-byte bound.

This author qualification uses Server `2c4e5dca` and fresh precreated private
roots. Independent qualification, private-root creation, retained/restore semantic
rebind, capacity revisions and the composition with current main remain separate
requirements. The candidate is not merged or approved for live preparation.


### Optional telemetry cannot reserve operational claim capacity

Claim-queue review found a second pressure case: retaining every previously
acknowledged optional observation can still exceed the 16 MiB queue limit when
new signed or operational fields grow. Dropping only newly added observations
is insufficient. Add a second bounded fallback that omits prior optional
observations and reports that degradation, while preserving every entry,
signature, attempt counter and operational outcome. If those required bytes
alone exceed capacity, retain the actual capacity refusal.

Qualification must exercise the real durable save with prior observations near
the limit, subsequent signed-field growth, failed-save publication isolation
and reopen of the exact acknowledged fallback. An omitted observation remains
unobserved history; no economic outcome or signing authority follows from it.
This review finding is assigned to the claim-producer successor, not qualified
or merged yet.


### Independent preparation composition qualification

The exact SN `1f66a2bd` / Connect `ba74f897` / Server `2c4e5dca`
workspace now passes independent qualification: 101 selected tests normally
and with race detection (15 mainnet, 11 validator, 73 storage core and two
chain tests), plus five package vet scopes. All tests completed without
failures or skips. Root verified all 48 [receipt manifest](evidence/preparation-composition-independent-20261002.json)
bindings. Receipt SHA-256:
`b5f4725895eadc33d398b8329bea98671b39f1845db0203a49ae9a7b3f4cd6c4`.

The receipt binds the 648-module local workspace graph and exact source
readback (20,080 SN and 3,715 Connect tracked blobs), including the unchanged
core, reader and adapter joins. Earlier old-source causal failures remain
separate author evidence. This qualifies the frozen source composition; it
does not qualify tracked published-module consumption, current main with later
fleet/server changes, directory-owner/private-root successors or a production
release. Integrate those source/dependency changes without replacing completed
recovery history, and qualify their actual final graph before launch.


### Provider readiness integration retains evidence boundaries

Main `83d92f75` now includes the exact qualified provider readiness producer
and expected-roster monitor. [Independent integration evidence](evidence/provider-monitor-integration-20261002.md)
records 20 normal/race tests, three package vets, two causal capacity failures
per mode and preserved newer fleet/module bytes. The appended admitted-handler
shutdown control proves that close joins work already in flight, rather than
merely closing an idle HTTP listener. Device readiness remains separate from
proof, payment and genesis/finality authority. Current-main release composition,
claim monitoring, economic-domain evidence and actual alert/repair delivery
remain open; this source integration does not close MG-07 or PH-28.


### Keep qualification headroom without discarding completed work

As data-volume free space reached 115 GiB, the sixth guarded inactive-cache
reclaim removed 155 old compiler archives and recovered 8.01 GiB. The
[verified receipt](evidence/cache-reclaim-6-20261002.json) binds the process
censuses, final clean declared-metadata scan, physical identities and all
removed-path absences. Failed initial scan attempts are recorded; no deletion
was admitted before the privileged absolute-tool-path scan completed cleanly.
Active caches, source, modules and evidence remain retained. This preserves
running qualification progress and its 110 GiB floor; it does not close
production capacity, restoration or compiler-provenance requirements.


### Explicit private-root creation is source-qualified

Frozen SN `c8998b31` / Connect `7600ea5c` passes 83 core and 26 public
preparation-command tests in each of normal/race modes, plus four package
vets. Root verified all 43 [author receipt](evidence/private-root-preparation-author-20261002.json)
manifest bindings, raw archive and source bundles. Receipt SHA-256:
`693444206629643f2100b2e30c05700241769c1d325aa108fe07c41afaf512ac`.
The old directory-owner source fails the two new public creation controls in
each mode. First-checkpoint positive results retain their separate source scope.

The explicit fresh mode stages a private leaf inode during planning, binds its
parent and zero-history fence, reserves the original inode/control before an
atomic no-replace move, and syncs both parents. Child crashes, lost sync
acknowledgements, competing targets and changed or missing completed namespaces
resume only through original retained custody; runtime absence never triggers
implicit mkdir. Target parent, staging and metadata roots remain precreated.

This is Linux amd64 source/workspace qualification using Server `2c4e5dca`;
arm64 execution, independent current-main tracked-module qualification,
retained/restore semantic rebinding and capacity revisions remain open. The
qualified candidates are being composed with provider/fleet fixes and current
server before merge. No live preparation or deployment authority is supplied.

### Tracked preparation composition retains exact dependency failures

The current [author receipt](evidence/preparation-tracked-module-author-20261002.json)
seals core 14, public 31, native 2, ledger 11 and miner 10 tests in each mode on
`1d51f8be`; server blob 7 passes in each mode on `0384cbfc`. The only source
delta is four `go.sum` entries for server test dependencies. All Go and `go.mod`
bytes and effective module versions are identical. The earlier blob normal,
blob race and core/blob vet attempts failed before tests on missing sums and
remain failed in the receipt. Nothing is relabeled as 75 tests executed on one
commit. Complete `go list -deps -test` preflight now covers every selected
consumer and dependency package before qualification.

All seven package vets and three verification binary builds pass on `0384cbfc`.
The built mainnet command refuses missing plan inputs with exit 2 and no output;
no service or live preparation was started. The tracked Connect760 module is
authenticated from exact local Git using a file proxy; publication and release
provenance remain separate. Actual owner-startup EIO/EMFILE tests retain the
original native journal, physical generation and completed preparation control,
then reopen the same custody after the transient observation recovers.

The source-only [latest-main join](evidence/preparation-current-main-join-20261002.json)
retains all 27 preparation paths and all 21 newer main paths, including all 11 claim
producer Go files, without manual conflict adaptation. That join is a candidate,
not independent current-source qualification. Retained/restore rebinding,
capacity revisions, actual restore rehearsal and a composed release stay open.


### Durable claim projection is integrated

Main merge `bd5e7e72` includes exactly the 11 qualified producer files from
SN `347605fd`, preserving provider/fleet changes and original module files.
Root verified all 127 author bindings and 76 independent bindings. The
[independent receipt](evidence/claim-projection-independent-20261002.json), SHA-256
`1eda6ff63df36aa0b4260573f31076676badac83224fead862eda57ce4e86a9e`,
passes 29 tests per mode and two package vets; three old-body controls fail at
the intended assertions per mode. Author 37-test coverage remains separate.
The [main integration manifest](evidence/claim-projection-main-integration-20261002.json)
binds exact source bytes. This graph uses Server10a; current-server composition
and the independent claim consumer remain open. No finality, payment allocation
or live launch authority follows from the queue projection.


### October 3: native raw restore diagnostic correction

The [original native raw restore batch](evidence/restore-native-raw-original-20261003.json)
at frozen SN `00a30655` completed normally and under race detection. Each mode
passed ten mainnet roots and one chain root, with one mainnet assertion failure:
a duplicate JSON nomination was correctly refused by the earlier strict reader,
while the test expected a later raw-adapter diagnostic. Both package vets pass.
Preserve this failed batch; a separate test-only correction must reach that
assertion without weakening duplicate-input refusal. Broader member/multi-owner
restore and current published composition remain open.


### October 3: preflight container fixture access separately from custody

The [initial durable-boundary batch](evidence/payout-boundary-fixture-setup-failure-20261003.json)
failed before any product tests: the container PostgreSQL user could not open
its mode-700 bind-mounted init directory. Owned cleanup completed. Preserve
this setup failure separately from product assertions. A distinct readable
fixture copy retains identical initialization bytes; corrected execution is
pending. Preflight each mounted fixture's parent traversal and file access for
the actual container UID before launching the full gate. Do not apply the
fixture permission correction to production custody: host private-root ancestry
and container initialization mounts have different access requirements.


### October 3: qualify restored state through the actual execution consumer

Unsigned member/checkpoint rebinding is not complete recovery when a higher
layer still binds physical identity into reviewed authority. Current
`openBootstrapSuccessorExecutionStore` passes the original approval's
`Plan.Registry` into `openBootstrapSuccessorExecutionDirectory`; its ongoing
check compares pathname identity and descriptor device/inode with that root.
Local preparation also retains the original physical root. A valid copy on a
new generation can therefore remain unusable by the real execution consumer.

Keep original signed approvals, signed relayer bytes, Safe signatures, nonce
registry and attempts unchanged. Implement a separate reviewed rebind receipt
that links the original approval and authenticated restore lineage to the exact
new physical generation without increasing economic, nonce or signing authority.
Qualify the real reopen/resume entry point after restore, including pending
attempt reconciliation and rejection of an unrelated target, modified approval,
omitted registry member or surviving former writer. Storage-only successful
copy/inspection tests cannot close this consumer requirement. Implementation
and independent qualification remain open; no new signing is authorized here.


### October 3: durable earning boundary normal qualification

Frozen Server `cdcb61fa` now passes the [independent 28-root normal
batch](evidence/payout-boundary-normal-20261003.json): all 13 new boundary
controls and 15 neighbors pass, with no failures or skips and owned
PostgreSQL/Redis cleanup complete. The exact source tests concurrent explicit
preparation, declaration drift at actual planning/send paths, retained accepted
attempt reconciliation, process restart and a database timeout's bounded
continuation. The original container setup failure stays separately retained.
The [same 28-root race scope](evidence/payout-boundary-race-20261003.json) now
passes without failures, skips or race reports, with owned cleanup complete.
The [normal regression controls](evidence/payout-boundary-causal-normal-20261003.json)
also reproduce all eight intended failures while accepted reconciliation
remains a positive pass; owned cleanup completed. The five groups include
explicit omission/misclassification controls, not all unmodified historical
source. Race controls and vet/build remain queued. This does not relabel the earlier
full model source, prove a composed release or make the October 6 schedule
operational. Merge, production preparation and running worker evidence remain
required.


### October 3: follow the consumed pagination contract

Review of the isolated wallet GET successor found that it inferred wallet and
token-balance cursors from the last returned identifier. The retained official
Circle endpoint OpenAPI captures instead define next-page links in the response
header, with no next relation on the final page. The generic documentation
introduction was insufficient to establish the endpoint-specific contract.

Follow returned links within one logical read budget; do not manufacture a next
cursor or use a short page as proof of completion. Confine every next request
to the approved HTTPS host and exact endpoint path, retain required filters,
refuse cycles/ambiguous links and enforce finite page/member bounds. Test opaque
cursors different from item IDs, absent final-page links, multiple header
values, malformed/cross-host links and cancellation across pages. Never turn a
partial census into an empty wallet or zero balance. The successor is frozen
at Server `25f617dc`; no passing execution or release inclusion is claimed.

Root independently checked the retained endpoint captures at
`/mnt/data/sn-testnet/provider-usdc-transition-20261002/evidence/circle-contract-review/`.
`list-wallet-balance.md` SHA-256 is
`aaee9e8006d888edd8ee30d05468e1ccb39a9489f0094e9691b451f0e18ccbda`;
`list-wallets.md` is
`1cf0df5fe1294ffcbf564ab6d8cdd949e3af2d38bd18cfc43f688307ae2bdffb`.
The primary endpoints are [wallet balances](https://developers.circle.com/api-reference/wallets/user-controlled-wallets/list-wallet-balance.md)
and [wallet list](https://developers.circle.com/api-reference/wallets/user-controlled-wallets/list-wallets.md).
The [frozen successor intake](evidence/wallet-get-intake-20261003.json) retains
these source URLs and captures separately from executed regression evidence.
Root verified all 23 bindings and nine candidate source/module files against
Git `25f617dc`. Its 14 new plus 12 prior roots and five causal omission groups
remain queued for independent execution.


### October 3: retained native correction and restore-core race scope

The [single native diagnostic correction](evidence/restore-native-diagnostic-independent-20261003.json)
at test-only `3ca923a9` passes normally and under race detection, without skips
or race reports. Root verified its 11 bindings, actual test events and exact
corrected test bytes. Production/module bytes remain `00a30655`; its eleven
passing roots per mode and two vets remain separately retained, and the
original diagnostic assertion failure stays failed.

Connect `53bc92fa` now also passes the [same 32 restore-core roots under race
detection and package vet](evidence/restore-core-race-vet-20261003.json). Root
verified eight addendum bindings and all 32 tracked package source files against
the exact Git commit. The prior independent normal scope remains retained.
This advances the shared dependency qualification; public member/multi-owner
restore, execution rebind, current published composition and live restoration
remain separate requirements.

The [ninth bounded inactive-cache cleanup](evidence/cache-reclaim-9-20261003.json)
reclaimed 16,330,846,208 bytes (15.21 GiB) from 450 old compiler archives.
Privileged process checks and bounded metadata reference checks found no
references; exact physical identities were rechecked before deletion. Active
test caches, source, evidence, modules and executables were retained. The data
volume subsequently had about 146 GiB free; admission must still remeasure
headroom before each costly phase. This is qualification-host evidence, not
production capacity qualification.


### October 3: shared recovery dependency published

Connect main now publishes `9e7ec0afa426472c083c750196155f5708e7d23b`,
tree `b1919e473daab790c5cf7a6fb6fc56e74831d636`. The
[sealed source join](evidence/connect53-source-join-20261003.json) preserves
all durable-volume and module bytes from qualified `53bc92fa`, and all paths
outside that package from upstream `69a006b3`. Root verified all 37 bindings.
Pull/rebase changed the integration commit identity from `da2385aa`, but root
verified the entire published tree remained identical and independently read
back the remote main ref. The [publication checkpoint](evidence/connect53-publication-20261003.json)
retains that distinction.

The 32 independent normal/race roots and package vet retain their original
source scopes; earlier preparation receipts remain separate. This is shared
source publication, not proof of current SN/server module consumption, a
qualified production release or usable live restore. Pin this exact published
revision in the next consumer composition without retargeting active tests.
Member `332b1f43`, complete co-owner restoration, explicit execution rebind
and capacity revisions remain subsequent requirements.


### October 3: returned I/O errors must preserve their causes

Composition review found that accepted-payment transaction readback wrapped
its error with `%s`, discarding typed cancellation and timeout causes before
retained continuation handled it. Candidate `bfb224c8` changes the actual
returned wrapper to `%w` and adds end-to-end controls; it is not yet qualified.
Audit adjacent wallet, cancellation, submit/reset and record-save return paths
for the same defect, distinguishing returned errors from log formatting.
Preserve joined hard causes and caller cancellation through public boundaries.
A canceled owner must not schedule more work, and an ephemeral physical read
may retain the same attempt for bounded continuation. Existing passing scopes
do not qualify this later composition correction.


### October 3: read retry fixes do not make ambiguous writes safe

The frozen wallet GET candidate deliberately retains the existing
`WalletCircleTransferOut` POST body. Source review confirms four attempts each
create a fresh idempotency key for the same transfer challenge. An ambiguous
response can therefore create another remote challenge rather than reconcile
the first. This proves a duplicate-write risk, not duplicate paid transfers:
the user-controlled flow still requires user confirmation. Its cancellation
returns a non-nil `Done.` error but discards the cancellation cause.

Implement a separate logical write identity with durable original request/key,
bounded reconciliation of uncertain outcomes and no new key on retry. Preserve
caller cancellation, distinguish an unparseable accepted response from a
confirmed refusal, and test interruption/restart and repeated caller requests
through the actual challenge path. Audit exact decimal amount formatting and
minimum precision before submission; float formatting must not redefine the
requested value. Keep customer challenge writes distinct from provider payout
attribution. This correction is assigned to a separate successor; GET tests
and unchanged-POST byte joins do not qualify write recovery.

Bound custody by semantic progress, not arbitrary provider-response variation.
A changed raw response digest for the same challenge/status must not consume a
new permanent obligation slot on every expected GET. Retain original evidence
and disclose repeated observations; preserve room for terminal or contradictory
outcomes. Add a deterministic actual-path control with at least 65 equivalent
responses whose metadata differs, followed by completion, restart and a genuine
contradiction. If history capacity needs revision, it must preserve old evidence
and unfinished work rather than erase the request or mint another key.

The JavaScript caller boundary must reject unsafe numeric amounts before HTTP
or use an explicitly defined exact wire format. Go int64 and integer formatting
inside the server do not prove exactness after JavaScript number conversion.
Caller request IDs must survive retries and application restart; generated API
fields alone do not prove external applications have adopted that contract.


### October 3: composed payout/read continuation source

Frozen Server `2a453df9` joins the durable boundary `cdcb61fa` and wallet
GET successor `25f617dc`. The [source join](evidence/payout-get-composition-source-join-20261003.json)
retains all matched component bytes, with four deliberate composition changes:
public payment error/continuation handling, its new tests, the boundary drift
control and transition status. New legacy submission admission is distinct
from permission to reconcile existing attempts. Returned I/O errors retain
their causes, and canceled owners cannot schedule continuation tasks.

Root verified all 58 [intake bindings](evidence/payout-get-composition-intake-20261003.json),
46 candidate source/module files against Git, and every declared component
match. Ten controller roots and four operative regression controls are queued;
no composition execution, full-suite pass or deployment is claimed. Retain
the unchanged component and full-model scopes.

The source census also confirms a separate MG-07 implementation gap: current
`monitorServicesPolicy` has provider/validator/operator roles but no claim
consumer roles. Resume and integrate the retained `e7965b4` claim consumer
against the actual producer and current policy rather than declaring producer
publication alone to be monitoring closure. Sequential proof/receipt progress
requires qualification; the earlier review lead does not establish a hidden
leaf-progress defect in the producer's actual signed-receipt path.


## October 3 completed cutoff-boundary qualification

Frozen Server `cdcb61fa` completes its [independent scoped qualification](evidence/payout-boundary-qualification-20261003.json): 28 normal and 28 race roots pass, five package vets and the offline CLI build pass, and every owned fixture cleanup completes. The [race causal controls](evidence/payout-boundary-causal-race-20261003.json), like their normal counterparts, produce eight intended assertion failures and one accepted-payment reconciliation positive pass without race reports. The original container-bind setup failure remains retained. This qualifies the durable earning-policy boundary scope; wallet `25f617dc`, combined Server `2a453df9`, final release integration and production deployment remain separate gates.


## October 3 full hardening source reconciliation

The [38-requirement source review](evidence/current-requirements-20261003.md) covers every PH-01–PH-28 lesson and MG-01–MG-10 gate, with [68 exact Git-blob bindings](evidence/current-requirements-20261003.json). It identifies integrated fleet capability selection, finite GET recovery, provider monitoring, callback isolation and retained-member/reader recovery; these require composition and production rehearsal rather than duplicate implementations. Confirmed remaining implementation work includes the current claim consumer, durable customer transfer-out challenge recovery, independent continuous credit/reserve/capture/native economic observers, transaction-attributed fee verification, and joined capacity/restore behavior. Existing validator caches need causal work-count and invalidation qualification before additional caching is justified. This is a source census, not test or launch acceptance. Its pinned `4e413cb0`/Server `2a453df9` scopes remain historical; the separately recorded completed financial and cutoff-boundary tests retain their own exact sources.


## Continuous economic monitoring acceptance

Current `mainnet/economic_emission_observe.go` reads a bounded native range and explicitly labels finality as an owned-RPC assertion. It leaves the pre-withholding denominator, recipient generations, recycling and independent outcome proof unresolved. `mainnet/evm_reserve_link.go` verifies the original reserve recorder installation; it does not observe operating reserve health. Neither scope establishes continuous settlement monitoring.

The production observer must retain a contiguous finalized cursor and original event identities across restart. A partial page or timed-out block must remain pending, without advancing beyond unread evidence; repeated reads must not count capture, carry, credit or payment twice. Persist completed bounded batches and retry only the unfinished batch. Validate chain, contract, canonical boundary and event ordering before applying an observation. Preserve the RootMissed carry step explicitly between capture and later entitlement, as the testnet peer review required.

Expose observed contract conservation separately from independently verified native emissions and the 10% provider/90% recycle target. Missing denominator, fee attribution or finality authority is unknown, never zero or a successful economic check. Each domain needs distinct heartbeat, successful observation, durable progress and oldest unresolved obligation metrics. A reserve or claim read timeout must retry within the shared bounded budget and leave healthy validator/provider/operator observers running. Observers do not acquire repair or signing authority.

Qualification must exercise the actual public command and checkpoint owners: timeout midway through a batch, restart after persistence before acknowledgment, duplicate/overlapping pages, changed canonical boundary, capture carried into a later epoch, aggregate ClaimPaid without invented per-pool attribution, and unavailable native denominator. Prove exact-once observations, retained original evidence, bounded work, joined cancellation and domain isolation. These are required implementation and qualification items under PH-12, PH-15, PH-28 and MG-07; the source review and finite reader do not close them.


## October 3 wallet GET normal qualification

Frozen Server `25f617dc` passes its [independent 26-root normal scope](evidence/wallet-get-normal-20261003.json): fourteen new wallet-read controls and twelve prior payment-read controls, zero failures/skips and completed fixture cleanup. The sealed receipt binds twenty files; coordinator verification also reads the actual terminal JSON events and exact expected root names. Race execution, omission controls, vet and combined Server `2a453df9` qualification remain pending. This scope covers reads and the unchanged POST source join, not behavioral qualification of customer challenge writes or a deployed cutoff.


## October 3 wallet race and claim-consumer source checkpoint

The [same 26 wallet GET roots pass under race detection](evidence/wallet-get-race-20261003.json) at frozen Server `25f617dc`, with zero failures/skips/race reports and completed fixture cleanup. Omission controls, vet and combined-source qualification remain separate.

The new claim-consumer candidate `80030acb` has a [coordinator source review](evidence/claim-consumer-source-review-20261003.json): exactly nine changed files over the pinned current base; miner, validator, operator, protocol, chain and module bytes are unchanged. Qualification remains pending. Unsigned claimed-leaf progress and signed-receipt progress are distinct actual producer paths; later credit payments belong in the economic observer rather than rewriting original receipt evidence. An adjacent review identifies HTTP 408/429 classification and pacing for follow-up; these current statuses are nonterminal, so this is not evidence of a stopped observer.


## October 3 current claim-consumer qualification handoff

The [final frozen intake](evidence/claim-consumer-intake-20261003.json) for SN `80030acb` binds 31 source/control files and selects 23 roots: sixteen claim controls, two public worker neighbors and five actual producer controls. Six explicitly classified overlays expect eight behavioral assertion failures per mode. The final intake supersedes the retained preliminary control recipe; compiler failures cannot count as causal success. No execution result is claimed yet. This uses the unchanged producer347 graph (Server10a/Connect0a5), so published Connect9e/current Server2a consumption still requires its own release composition. The observer supplies no repair, signing, independent finality or per-epoch payment authority.


## October 3 runtime source refresh

A [read-only official source refresh](evidence/runtime-source-refresh-20261003.json) finds returned v470/v471 tags and releases; the main ref is `c004cebf`, whose runtime source and v471 manifest/digest explicitly describe spec version 471. These sources do not authorize the earlier observed v472 artifact. This does not establish absence of another branch or unpublished proposal, and does not claim a fresh on-chain version. Exact artifact/source attribution and consumed-purpose admission remain open launch inputs, independent of payout and monitoring qualification.


## October 3 wallet operative controls

The [normal wallet causal controls](evidence/wallet-get-causal-normal-20261003.json) at frozen Server `25f617dc` produce all nine intended assertion failures across five omission groups, with five positive neighbors passing and owned fixture cleanup complete. Coordinator verification covers all 59 bound files and the actual terminal event census. These discriminate retries, Link pagination, shared budgets, Retry-After and redirect credential protection; they are explicitly labelled operative omissions, not historical source replays. Race controls, vet and final combined-source qualification remain pending.


## October 3 original-authority registry recovery handoff

The [frozen registry-rebind intake](evidence/registry-rebind-intake-20261003.json) at SN `94e087da` binds fifteen files and proposes four public/neighbor normal/race roots, vet and an old-dispatcher causal check. Source review confirms physical adoption requires completed original claim and nonce custody, preserves exact nonce bytes and the original approver, and refuses an unrelated pending local publication. Compilation/preflight are complete; behavioral qualification is pending. This candidate uses Connect332/Serverc256 and does not establish published Connect9e/current Server2a consumption.

The first registry-only profile is an incremental implementation, not the completed restore requirement. Complete co-owner union restore, local-root rebind, original pending-outcome adoption, pending outer-head recovery, capacity revisions and the final published-module composition remain required. Rebinding must preserve original signing payloads, lifetime counters and histories and must not recreate acknowledged claims or missing receipts.


## October 3 current Server migration composition blocker

A [fresh upstream merge review](evidence/server-current-migration-join-20261003.json) found Server main `18ec7c05` has 87 changed files over the tested base `c2563f9a`; payout candidate `2a453df9` changes 44. Their sole file intersection is `db_migrations.go`. Upstream appends the Redis admission schema where the payout candidate appends bonus provenance and the earning-policy anchor. Existing production migration ordering must remain intact, with payout migrations appended afterward; an automatic conflict resolution or reordered list could make an already-applied version falsely represent another schema.

A separate current-main successor and deterministic upgrade controls are required before merging the payout release. Exercise the real existing-current-schema migration, repeated migration, missing-feature refusal and exact-policy initialization. Retain the completed financial630 and boundary/wallet component scopes; those tests did not use this newer upstream schema. Upstream deployment notes are not an independent production schema query. No migration or deployment was performed during this review.


## October 3 October 6 configuration publication

Reviewed config `93dc65fd` is now [published on config main](evidence/payout-config-publication-20261003.json), with exact `main/sn.yml` bytes preserved through fast-forward merge, pull/rebase and terminal push; remote main and clean worktree were verified. The cutoff remains `2026-10-06T00:00:00Z`, pre-cutoff USDC obligations may finish afterward, and mainnet activation remains explicitly blocked with readiness fields empty. This is configuration publication, not a deployed earning schedule or worker-adoption receipt. Corrected current Server migration composition and rollout verification remain required.


## October 3 completed wallet gate and combined payout normal result

Frozen Server `25f617dc` completes its [independent wallet GET gate](evidence/wallet-get-qualification-20261003.json): 26 normal and 26 race roots pass, controller vet passes, all fixture cleanup completes, and both causal modes discriminate nine intended assertion failures with five positive neighbors. The [race causal receipt](evidence/wallet-get-causal-race-20261003.json) retains explicit omission classifications; coordinator verification reads actual terminal events and all component bindings.

Frozen combined Server `2a453df9` separately passes its [ten-root normal composition scope](evidence/payout-get-composition-normal-20261003.json), covering retained read recovery during boundary hold, final-send boundary admission, public ST writer hold, cancellation causes, bounded continuation and atomic attempt-reset rollback. Race/causal/vet/build qualification remains pending. The newer current Server migration-order successor remains separate and must be qualified before production merge.


## October 3 combined payout static qualification

Frozen Server `2a453df9` passes [five-package vet and the offline CLI build](evidence/payout-get-composition-vet-build-20261003.json). Coordinator verification checks the seven bound files, terminal exit records and exact binary hash. This is static/compile qualification; it does not invoke the CLI or prove a deployment. Normal composition tests are separately recorded; race and operative controls remain pending. The newer migration-order successor must retain the complete upstream migration prefix and qualify an actual existing-current-schema upgrade, not infer safety from a fresh fixture.


## October 3 combined payout race and normal causal results

Frozen Server `2a453df9` passes the [same ten composition roots under race detection](evidence/payout-get-composition-race-20261003.json), with zero failures/skips/race reports and completed fixture cleanup. Its [four normal operative controls](evidence/payout-get-composition-causal-normal-20261003.json) all reach intended behavioral failures: lost cancellation cause, canceled continuation falsely acknowledged, missing retained GET recovery, and new-send hold conflated with accepted-attempt reconciliation. The exact prior error-format body and explicit omissions remain separately labelled. Race causal controls remain pending; this source does not include upstream18ec or establish the production migration upgrade.

The current-main successor also needs semantic integration coverage beyond conflict resolution: upstream18ec changes Redis escrow admission and settlement callbacks in otherwise disjoint files. Exercise actual contract close through that path into retained provider usage and cutoff accounting; a file-disjoint merge alone cannot prove unchanged financial behavior.


## October 3 completed frozen payout composition qualification

Frozen Server `2a453df9` completes its [independent combined gate](evidence/payout-get-composition-qualification-20261003.json): ten normal and ten race roots pass, four operative causal controls per mode reach intended assertions, five package vets and the offline CLI build pass, and all owned fixture cleanup completes. The [race causal receipt](evidence/payout-get-composition-causal-race-20261003.json) retains each control classification. The [independent source join](evidence/payout-get-composition-independent-join-20261003.json) is confirmed against all 46 physical files from the frozen components plus four explicit composition changes. Financial630 full-model results and wallet25f/boundarycdcb component receipts retain their original scopes.

The final current-main migration successor remains unqualified and unmerged. Preserve upstream18ec migration order and exercise the actual Redis-backed settlement integration before publishing that code; the completed frozen gate does not authorize a production upgrade or mainnet activation.


## October 3 claim normal execution and SQL-plan follow-up

Frozen SN `80030acb` passes [all 23 selected normal roots](evidence/claim-consumer-normal-20261003.json): sixteen consumer controls, two public worker neighbors and five actual producer controls, with zero failures/skips. Coordinator verification checks twenty bound files and actual terminal root names. Race/causal/vet and final module composition remain pending; the frozen producer347 graph is retained.

The unchanged financial630 [nested SQL-plan root passes separately](evidence/payout-retention-auto-explain-20261003.json). Its disposable PG18 fixture has the library; the original test role receives SQLSTATE42501 on LOAD. Capability is granted only to that synthetic role, then the exact root passes and fixture cleanup completes. All 27 receipt bindings and the actual terminal test event were verified. The original full-suite skip remains unchanged; no production database permission or newer Server source is qualified by this follow-up.


## Continuous claim expectation renewal

The frozen claim consumer requires one to sixteen independently declared epochs and binds its checkpoint to the exact policy hash, including that epoch list. This qualifies a finite reviewed expectation window. Adding a new epoch changes the policy identity and cannot be treated as a compatible reopen of the old checkpoint. It does not by itself implement continuous mainnet epoch monitoring.

Implement explicit bounded expectation renewal under the same reviewed pool/member/network domain. Preserve original policy references and completed observations; retain unresolved obligations across window changes and disclose archived/omitted coverage. Admit new expectations independently of producer assertions, reject altered original shares/roots/deadlines and foreign identities, and refuse capacity shrink that drops retained obligations. Do not delete checkpoints or silently relax their hash checks to admit a new window. Qualify actual public continuation through repeated renewals, overlap, unresolved old epochs, restart, history loss and exhausted capacity. This remains required under PH-17/PH-23/PH-28 and MG-07, separate from candidate800's finite-window gate.

## October 3 claim consumer race result

Frozen SN `80030acb` passes [all 23 selected roots under race detection](evidence/claim-consumer-race-20261003.json), with zero failures/skips/race reports. Coordinator verification checks all twenty bound files and actual terminal names across consumer, public worker and producer groups. Normal23 results remain separate. Causal and terminal qualification remain pending; published current-module adoption and continuous expectation renewal are separate requirements.


## October 3 claim normal causal qualification

Frozen SN `80030acb` reaches all [eight intended normal causal assertions](evidence/claim-consumer-causal-normal-20261003.json) across six groups: old public-policy admission, heartbeat/progress conflation, proof loss, receipt contradiction, healthy-peer cancellation and uncertain-publication continuation. Coordinator verification checks 39 bound files and actual failed-root events. One control uses the exact earlier operative policy body; five are explicitly labelled invariant omissions. Compiler/setup failures are not counted as causal evidence. Race causal execution remains active; final qualification and merge are pending.


## October 3 claim consumer merged on main

The independently configured claim consumer is integrated through SN `66f92220` after pull/rebase. Its [completed independent gate](evidence/claim-consumer-qualification-20261003.json) passes 23 normal and 23 race roots, mainnet/miner vet, and eight intended causal assertions in each mode. The [race causal receipt](evidence/claim-consumer-causal-race-20261003.json) preserves exact-old-body and explicit-omission classifications. The [merge source join](evidence/claim-consumer-merge-20261003.json) proves all nine changed code/test blobs are identical to qualified candidate800; other differences are documentation/evidence only. No duplicate execution is needed for unchanged tested bodies.

Current monitor policy and dispatch now include Claims alongside validator, operator and provider roles. This closes the missing finite-window claim-consumer code gap. Continuous expectation renewal, independent economic monitoring, current published-module release composition, real roster/alert delivery and production deployment remain open; MG-07 is not closed.


## October 3 current migration upgrade qualification

Current Server candidate `19952183` preserves the incoming `18ec7c05` 755-entry migration history exactly and appends payout provenance at version 756 and the immutable earning anchor at 757. The [source intake](evidence/payout-current-migration-intake-20261003.json) and [134-file composition join](evidence/payout-current-migration-source-join-20261003.json) were independently rehashed, including 143 intake bindings. Its [selected normal execution](evidence/payout-current-migration-normal-20261003.json) passes all 14 roots with no skips or failures and owned-fixture cleanup success. The scope includes actual 755→756→757 database upgrade, repeat idempotence, old conflicting payout history refusal, Redis settlement interaction and retained reconciliation. The separate [race execution](evidence/payout-current-migration-race-20261003.json) also passes all 14 roots with successful owned-fixture cleanup. Causal and static qualification remain separate; this candidate is not yet published or deployed.

The production lesson is to preserve already-published migration identities, not merely resolve overlapping source files. An appended migration must be checked against the actual incoming schema prefix and adjacent settlement callbacks before adoption. Do not replay an earlier alternative history at the same schema version, or relabel older-source full-suite results as current-composition acceptance.

### Test error contracts before weakening recovery admission

The current member and registry recovery tests exposed mismatched error assertions: signed-root or exact-lineage refusal happened correctly, but tests required a nil constructor result or an unrelated diagnostic word. Keep the production refusal intact. Separate test-only corrections must prove a returned failed constructor has closed custody, cannot publish, and creates no nonce; command refusals must retain their exact status and no-effects checks. Preserve original failed receipts and qualify corrections independently. This is not evidence that invalid authority was admitted.


## October 3 completed current migration candidate gate

Server `19952183` has completed its [independent candidate qualification](evidence/payout-current-migration-qualification-20261003.json): 14 normal and 14 race roots pass with no skips, failures or race reports; all three exact-old-migration controls fail as intended in each mode; five package vets and the offline CLI build pass. The [causal receipt](evidence/payout-current-migration-causal-20261003.json) and [independent source join](evidence/payout-current-migration-source-join-readback-20261003.json) were rehashed with their bound artifacts. This completes that immutable source scope, not production deployment.

A fresh publication fetch found Server main advanced to `ec6a038a` with hosted local-authority/proxy changes and updated modules after the candidate's `18ec7c05` base. Preserve both the qualified payout bytes and the new upstream work in a separate current publication composition. Reuse unchanged source-qualified outcomes with explicit joins; verify the changed dependency graph and its real consumers rather than assuming either that every test must repeat or that dependency changes cannot matter. The current publication join and release/recovery rehearsal remain outstanding. No migration or live payment was executed.


## October 3 combined-owner restore handoff

The [complete-union restore intake](evidence/restore-complete-union-intake-20261003.json) freezes SN `148ab9b4` and Connect `9fb897f8`. All 27 artifact bindings, both source heads/trees and 13 Git-backed source bindings were independently verified. Its proposed scope is six core and eight public roots covering complete disjoint owner coverage before staging, original heads/censuses, six-owner restore, real readers and cancellation after a partially published head. Compilation/preflight succeeded; behavioral normal/race qualification is still outstanding. This profile does not authorize restart or cover exclusive native/nonce/ledger/fleet/claim owners. Local execution-root rebinding, retained capacity revisions and final current dependency composition remain separate required work.

Current Server `ec6a038a` selects Connect `b8bd3c994855` and SDK `95ccd57da971`. The final composition must preserve that newer dependency floor and explicitly join its recovery code to qualified sources. Do not force the earlier published Connect `9e7ec0af` merely because it has an existing receipt: a qualified historical source is not authority to discard newer upstream behavior. Preserve historical receipts and qualify actual changed consumers on the selected current graph.


### Current Connect dependency join

Published Connect `5def2fa4` merges both `9e7ec0af` recovery APIs and `b8bd3c99` local discovery. The [independent Git source join](evidence/connect-current-recovery-source-join-20261003.json) verifies the complete durable-volume tree is identical to qualified `9e7ec0af`; only two local-discovery paths differ. The server's selected `b8bd3c99` branch alone lacks restore APIs, so the current consumer composition needs this combined published floor, with SDK `95ccd57d` retained. Source identity preserves earlier recovery qualifications within their original scope; it does not replace actual consumer compilation, changed-path tests or release rehearsal.


### Current Server publication source join

The [independent source join](evidence/server-publication-source-join-20261003.json) verifies candidate `84007fcb` retains all 46 financial paths exactly from qualified `19952183` and all ten newer upstream paths from `ec6a038a`. The path sets do not overlap and the join introduces no other changes relative to their common base. Preserve these qualified bodies while checking the changed dependency graph; this source receipt does not establish publication, deployment or final release acceptance.


## October 3 member restore test-only correction

The [independent affected-root gate](evidence/restore-member-correction-qualification-20261003.json) for `6b23b772` passes normal/race and mainnet vet. Its sole change from `f4e42066` is the test file; production and modules remain identical. The revised root retains the signed physical-directory refusal and proves closed custody, refusal to publish and no new nonce member. The original six-root public scope remains five passes/one failed assertion in each mode; no corrected-source full-suite pass is claimed. The [diagnosis erratum](evidence/restore-member-diagnosis-erratum-20261003.json) corrects a reversed predicate in receipt prose without changing original evidence.

The [original registry gate](evidence/restore-registry-original-failure-20261003.json) remains three passes/one diagnostic assertion failure per mode, with vet passing. Its actual exact-lineage refusal is preserved. The first separate diagnostic correction also encountered another mismatched expectation, so all fault cases are being traced together before further qualification. Constructor error outcomes must be judged by refusal and closed authority, not by an unrelated diagnostic substring or pointer presence alone. Local execution-root rebind and operational restore remain unqualified.


## October 3 complete-union gate and current module preflight

The [independent complete-union gate](evidence/restore-complete-union-qualification-20261003.json) now passes six core and eight public roots in both normal and race modes, with durable-volume/mainnet/durablehead vet success. Exact old-core and old-public test-only overlays produce all three intended missing-feature failures per mode. All 39 receipt bindings and actual terminal test outcomes were rechecked. This qualifies frozen SN `148ab9b4` / Connect `9fb897f8` on its inherited Server `c2563f9a` workspace; it is not a current-module, signed local-root rebind or production restore verdict.

Separately, [current-module preflight](evidence/current-module-5def-preflight-20261003.json) for SN `8cb08c95` / Server `0e04f038` passes real GOWORK=off graph/package resolution with published Connect `5def2fa4`, SDK `95ccd57d` and SCTP `6443417`. All bound artifacts were rehashed, including 32 downloaded durable-volume files joined to the qualified source. The two candidates change only module files from their immediate parents. Compilation, selected behavior, current-union integration and deployment remain separate. Qualified union/member fixes must be composed with current code and dependencies rather than left solely on historical branches.


## October 3 current compatibility and customer recovery handoffs

The [current public-module compatibility intake](evidence/current-module-compatibility-intake-20261003.json) fixes SN `8cb08c95` / Server `0e04f038` and 25 exact roots per mode across local authority/proxy/tunnel, payout/Redis/reconciliation and actual SN startup/restore consumers. All 71 bindings were rehashed. Sol has started the selected normal groups and passed the six Server/four SN package vets; terminal behavior and race receipts are still outstanding. This scope does not include unpublished union/local-root rebind code or establish a final production release.

The [customer-transfer source intake](evidence/customer-transfer-intake-20261003.json) freezes Server `530726fc`, SDK `9ae95704` and API-only Connect `56545699`. All 56 bindings and source heads/trees were rechecked; the [source join](evidence/customer-transfer-source-join-20261003.json) remains source-only. The successor retains request/key/body, GET-only known-challenge recovery, integer amount formatting and bounded semantic observation deduplication. Generated Go/JavaScript/C/C++ interfaces carry caller IDs; JavaScript rejects unsafe amount conversion before HTTP. Proposed controls cover 67 metadata-varying equivalent reads followed by completion, disabled custody guards, interrupted journals, contradictions, concurrent retries and client serialization. Behavior qualification and external application persistence/adoption remain open.

The [guarded cache reclaim](evidence/cache-reclaim-10-20261003.json) recovered 13.75 GiB from 450 inactive compiler archives at least 48 hours old. Process and metadata references, archive type/link count and exact physical identity were checked; all selected paths are absent. Source, evidence, modules, executable files and active caches were preserved. An initial missing sudo PATH entry for rg was retained separately; the successful absolute-path scan had no matches or diagnostics. The data volume had approximately 131 GiB available afterward, restoring qualification headroom without stopping a run.


## October 3 combined recovery dependency published

Connect main now publishes `08d48400`, verified against remote main with a clean local worktree. The [publication proof](evidence/connect-union-publication-20261003.json) joins the entire durable-volume subtree and module bytes to independently qualified `9fb897f8`, and every other path to newer upstream `ed82d358`. The [40-binding source join](evidence/connect-union-publication-source-join-20261003.json) preserves the earlier candidate. Rewritten ancestry caused pull/rebase add/add refusal before push; an ordinary merge preserved both sides, followed by successful pull and push. These recovery APIs are now published, but running compatibility tests remain pinned to `5def2fa4`; do not retarget them or infer adoption. SN union integration, local execution rebind, capacity revision and actual recovery remain separate.

The registry fault-case batch reached a genuine valid-resume defect after correcting diagnostic assertions: the storage reader admits the original-authorized rebind receipt, while the execution checkpoint inventory rejects that same retained file as unexpected. Unify authenticated receipt admission across construction, checkpoint, derivation and reopen paths. Test valid resume/restart/lost acknowledgment and reject forged, foreign or incorrectly staged receipts; do not fix this with a blanket filename-prefix exemption. Original failed scopes remain retained and no successful registry execution is claimed.


## October 3 current SN consumer qualification

The [independent SN consumer receipt](evidence/current-module-sn-qualification-20261003.json) now passes ten selected roots in normal and race modes, four-package vet and all source/dependency checks on frozen SN `8cb08c95` with Connect `5def2fa4`, SDK `95ccd57d` and SCTP `6443417`. All 38 bindings, actual test outcomes and vet exit were independently verified. This covers selected public startup/native/ledger/miner recovery consumers, not unpublished union/rebind/capacity code, the newer `08d48400` dependency or the full production release. Server `0e04f038` race qualification remains separately active; a later publication join must retain newer upstream Server `49450197`.

The [registry valid-resume failure](evidence/restore-registry-valid-resume-failure-20261003.json) records the genuine one-root failure on test-only batch `7a4123fe`, after invalid approvals correctly refused. Its metadata and actual terminal failure were rehashed. Static tracing identifies inconsistent authenticated receipt inventories between storage construction and checkpoint/replay. The production successor must qualify actual valid continuation, lost acknowledgment/reopen and forged/foreign-record refusal before this recovery gate can close. Earlier diagnostic-only failures remain distinct; no registry pass is inferred.

### October 3 completed compatibility and customer recovery scopes

The [current compatibility receipt](evidence/current-module-compatibility-qualification-20261003.json)
qualifies frozen SN `8cb08c95` / Server `0e04f038`: 25 selected roots
pass normal/race, ten package vets pass, and fixture cleanup exits zero.
All 122 receipt bindings were independently rehashed. This uses Connect
`5def2fa4`; it does not qualify the later `08d48400` composition.

Customer Server `530726fc` passes [14 selected roots in both modes](evidence/customer-server530-scoped-qualification-20261003.json),
but its original controller vet fails because a test does not release its
context on every path. The [test-only `a85c3e3f` correction](evidence/customer-context-correction-qualification-20261003.json)
adds the missing deferred cancellation; the affected root passes normal/race
and controller vet passes. Production and module bytes are unchanged. The
original vet failure remains a failure; no fresh full fourteen-root rerun is
claimed for the corrected source.

The [separate SDK scope](evidence/customer-sdk-qualification-20261003.json)
passes two Go roots normal/race, vet, six JavaScript checks and an actual C++
generated-interface roundtrip at SDK `9ae95704` / API `56545699`. An overbroad
offline module preflight appended unused checksums to the independent copy;
that failed attempt is retained. The tracked file was restored exactly before
the qualified Go reruns, whose before/after checksum guards match. Qualification
covers the imported package graph, not all unused module metadata.

Coordinator verification rehashed all 47 SDK, 49 original Server and 38 corrected
Server bindings and counted terminal Go root events in the qualified raw logs.
Customer causal controls, current-source joins, final model qualification and
publication remain separate work. Client adoption and deployed behavior are
unobserved. Latest Server `a244ae7c` on published Connect `08d48400` passes [sixteen roots in normal and race modes plus seven vets](evidence/server-a244-compatibility-qualification-20261003.json). Its scope remains selected compatibility, not the full model suite.

Continuous economic monitoring must catch up through authenticated bounded
historical pages rather than reject a cursor more than 4,096 blocks behind.
When an evidence-size limit is reached, commit only a complete verified subpage
and retain the original requested high-water; never skip history or reset the
cursor to latest. Native candidate `00d7efcd` implements this behavior and ten
new deterministic roots, including long outage, mid-page timeout, canonical
change, lost write acknowledgment and large census pressure. It is frozen for
independent qualification, not yet accepted. Continuous EVM reserve/credit/
capture monitoring, rolling claim expectations and composed recovery remain
required.

### October 3 Server publication

Server main now publishes `29ce22d6`; the [publication proof](evidence/server-publication-20261003.json) verifies remote main, clean custody and the whole source join. The financial/cutoff/retry source from qualified `a244ae7c` is preserved alongside upstream `87e7e8a6`. Coordinator verification independently rehashed all 88 qualification bindings and counted exactly sixteen passing roots per mode. No failed or skipped selected roots occurred; seven vets and both fixture cleanups exited zero.

The final pull observed a new upstream Redis accounting/test repair, and safely refused a divergent fast-forward. An ordinary merge retained its eleven changed paths plus the earlier documentation update; these do not overlap the financial change paths. The public escrow wrappers continue to apply Redis admission before entering their extracted shared bodies. The a244 receipt does not qualify the newly added upstream tests. Full current model qualification and customer recovery composition remain required. Published source is not deployed code or observed worker adoption.

### October 3 causal customer gate and cache review

Customer recovery now has independently verified [normal](evidence/customer-server-causal-normal-20261003.json) and [race](evidence/customer-server-causal-race-20261003.json) operative controls: six omitted/reintroduced mechanisms produce seven intended assertion failures per mode, with no compile/setup failures in those qualified runs. Coordinator verification rehashed 43 normal and 41 race bindings and matched the raw terminal roots and expected causes. This complements the successful original fourteen-root scope and one-root test-only cleanup correction; it does not rewrite their provenance. The separate initial runner path error is retained. Current Server/SDK/API publication composition is the next step.

The [eight-file cache source census](evidence/cache-source-census-20261003.json) finds three existing bounded mechanisms, avoiding duplicate implementation: authenticated original-intent receipt checkpoints, decision-local client-key authority reads, and cut-local exact assignment-signature reuse. Existing tests count actual body/HTTP/signature work and cover restart, eviction, changed runtime/route/boundary, independent client signatures and failed reads. Receipt scope explicitly excludes the executable hash; changed binaries alone need not discard that work. Reuse these controls against the final source/dependency graph and add a new cache only when a demonstrated uncovered repeat remains. This is source review, not a newly executed test gate or complete production lineage/capacity acceptance.

### October 3 customer libraries published

The [customer library publication proof](evidence/customer-library-publication-20261003.json) verifies SDK `9ae95704` and Connect `a53ed36a` on their remote main branches after clean fast-forward, pull and push. The SDK retains caller-owned request identity and exact amount handling; its [operative missing-intent control](evidence/customer-sdk-causal-20261003.json) fails at the intended assertion. API YAML exactly matches qualified `56545699`, while the rest of Connect remains exact `08d48400`.

The [complete customer source-join review](evidence/customer-current-source-join-20261003.json) independently rehashes all 5,380 Server, 3,729 Connect and 680 SDK tracked files, checks every Git blob/source correspondence and six original qualification/fence receipts. The Server candidate changes seven customer paths against published `29ce22d6`; final public dependency pins and current model qualification are being prepared. Earlier compatibility scopes stay unchanged. No deployed client adoption, live transfer or activation is claimed.

Qualification headroom: [reclaim11](evidence/cache-reclaim-11-20261003.json) removed 450 old, regular, single-link compiler archives (10.51 GiB) from the inactive cache after privileged process/reference checks and repeated physical identity checks. All deleted paths were verified absent; the active Sol cache, source and evidence remain intact. The volume returned to roughly 126 GiB free and existing test handles continued. Production sizing/restore gates remain open.

### October 3 native monitoring and final customer graph

Native `00d7efcd` passes [fifty selected roots in both normal and race modes plus mainnet vet](evidence/native-monitor-product-qualification-20261003.json). Coordinator verification rehashed all 44 bindings and counted exactly fifty passing terminal root events per mode. This is product qualification under the frozen `5def2fa4`/Server `0e04f038` graph; operative causal groups and final composition remain separate. The initial modfile-remapping setup failures remain recorded and are excluded from the behavioral census.

The [adjacent runtime renewal review](evidence/native-runtime-renewal-review-20261003.json) identifies a fixed-profile continuation gap: an approved new runtime can soft-hold the observer, while changing its policy prevents reopening the original checkpoint. A successor must admit a bounded monotonic reviewed runtime/read-purpose catalog, distinguish parent execution from post-state context, retain original historical profiles and preserve the same cursor, amounts and generation across renewal. Test a real upgrade boundary, approved renewal/restart and unknown-profile peer isolation. Preserve the completed00d gate; do not relabel it as qualification of the successor.

The [final customer intake](evidence/customer-final-intake-20261003.json) verifies Server `3f32d730`, all 5,381 tracked source entries, 26 intake bindings and all 3,581 Connect/523 SDK files in the public module archives against exact Git blobs. Its tracked graph now selects public API `a53ed36a` and SDK `9ae95704`. Twenty targeted roots, SDK compatibility and the required single full model run are underway or queued. Collect every targeted group and run the full model once source/fixture readiness is valid, even if an ordinary assertion fails; retain and repair all failures in a batch. Only structural corruption that invalidates the test run blocks that collection. Deployment and worker/client adoption remain unobserved.

### October 3 checkpoint recovery and full model execution

The [registry checkpoint fix](evidence/registry-checkpoint-fixed-qualification-20261003.json) at `08f32b7f` passes six selected roots normally and under race detection, plus mainnet vet. Coordinator verification rehashes all twelve bound files and counts six passing raw root events per mode. Shared authenticated receipt inventory now permits valid original-authority public resume and lost-ack recovery without admitting foreign receipts. The original `7a4123fe` failure stays recorded; historical/test-only causal comparison remains separate. This does not close local-root adoption with unfinished member outcomes, capacity revisions or full restored-service acceptance.

The native monitor's [eight operative normal controls](evidence/native-monitor-causal-normal-20261003.json) produce nine exact intended behavioral failures. Coordinator verification rehashes 43 files and matches the raw failed roots/assertions; setup and compile failures are absent from the qualified controls. Race controls remain pending. The fixed fifty-root normal/race product result retains its own scope.

Final customer Server `3f32d730` passes [all twenty targeted normal roots](evidence/customer-final-normal-qualification-20261003.json) and private fixture cleanup, verified against 28 raw/source/module hashes. The required full `./model` run has started on that exact source and published API `a53ed36a` / SDK `9ae95704` graph, alongside targeted race qualification. Preserve every assertion failure and optional skip, and feed actual failures into the next fix batch. No completed full-model result is claimed before the terminal receipt.


### October 3 current qualification and Redis refusal hardening

Final customer Server `3f32d730` passes [twenty targeted race roots](evidence/customer-final-race-qualification-20261003.json), complementing its twenty normal roots. [Five package vets and the CLI build](evidence/customer-final-vets-build-20261003.json) pass. The [current public API/SDK Go scope](evidence/customer-current-sdk-qualification-20261003.json) passes two roots in each mode and vet with unchanged module checksums. The [migration omission control](evidence/customer-current-migration-causal-20261003.json) compiles and fails at the intended missing-append assertion in both modes, with successful private-fixture cleanup. These selected results do not qualify the full model suite.

The required full model run remains active on the unchanged `3f32d730` source. It has exposed a genuine `TestCreateTransferEscrowSerializesWithDestinationDeactivation` failure: an inactive destination retains 1,024 escrow bytes rather than zero. The Redis admission path reserves request tokens before its later SQL eligibility checks; a known pre-SQL refusal can leave those tokens reserved. Preserve the failed run and continue collecting all failures while a separate successor fixes the class. No green full-model result or final publication qualification is claimed.

**Required adjacent fix.** Compensate only this request's exact attempted reservation tokens on every known pre-SQL exit: partial balance failure, inactive or mismatched identities, expired admission, caller cancellation and panic. Preserve unrelated live reservations and the original error/panic. Cleanup must survive caller cancellation; if transient Redis failure prevents completion, retain a retryable compensation obligation rather than silently leaking the reservation. Separate this known-no-SQL case from an uncertain SQL publication: reconcile persistence before releasing tokens that may back a committed escrow. Use deterministic barriers and actual public admission paths to verify both refused and successful/ambiguous outcomes, following `connect/CODESTYLE.md`. A short best-effort cleanup alone does not close this requirement. This lesson belongs to PH-03/PH-05/PH-11 and MG-06 recovery/accounting acceptance.

The [native monitoring race controls](evidence/native-monitor-causal-race-20261003.json) complete eight overlays with nine intended assertion failures and no setup/compiler failures. Coordinator verification rehashed all 43 bindings and counted the raw failed roots. The qualified fifty-root normal/race product gate retains its original source and dependency scope; reviewed runtime catalog renewal and continuous EVM/claim behavior remain open.

The [local execution-root intake](evidence/local-root-rebind-intake-20261003.json) at `2689fced` and [pending original-outcome successor intake](evidence/local-pending-outcome-intake-20261003.json) at `fcf41030` have independently rehashed 27 and 18 bindings respectively. Both remain compile-only, awaiting behavioral qualification. Their separate approval preserves original terminal payload, nonce and attempt authority; intake is not proof of successful restored-service continuation. Interrupted outer census-head restore, joined capacity revisions and the final current-module role composition remain required before closing PH-20/PH-23 and the corresponding launch gates.


The [registry old-source causal comparison](evidence/registry-checkpoint-causal-20261003.json) completes normally and under race detection: each mode exposes two intended public custody failures while the unapproved-receipt refusal remains passing. All nine bound files were rehashed and raw terminal events checked. Receipt v2 names the actual `source-preflight.json` directly instead of the original logical alias and retains the v1 digest; the other logical binding `old7a-source-fence.json` resolves to `/mnt/data/sn-testnet/sol-registry-rebind-7a-independent-20261003/evidence/source-fence.json`, as recorded by the sealer. No historical source or raw result was rewritten. This complements the fixed six-root gate; interrupted restore and capacity adoption remain open.


### October 3 incoming local gVisor dependency

An ordinary merge preserves incoming main commit `7debae6d`, which adds `replace gvisor.dev/gvisor => ../gvisor`. At this publication check `/home/by/urnetwork/gvisor/go.mod` is absent. This does not invalidate frozen qualification graphs, whose exact module declarations remain retained; it does mean current-main compilation and final release composition require the actual fork, its source identity and a reproducible dependency arrangement. Locate and bind the intended fork before testing the composed release. Do not remove the upstream replacement merely to reuse earlier green results, and do not claim those results qualify this new dependency.


### Restore fixture admission must exercise the complete owner plan

The first independent `2689fced` local-root control failed while constructing its complete owner-union preparation plan: `storage preparation capacities are absent or exceed the finite profile`. This occurs before the public local-rebind preview, so it is not yet evidence of a local-rebind production defect. Retain and finish the original test batch; diagnose whether the complete fixture omitted required profile values or exposed an actual bounded-profile mismatch. Do not widen production acceptance just to admit a test fixture. Before the next behavioral qualification, preflight the actual complete owner census, explicit capacity declarations, source graph and physical scratch ancestry. An empty/compile-only preflight cannot prove public restore admission. Any correction must retain the original failure and bind the corrected source/fixture separately.


### October 3 local gVisor fork materialized

The missing local dependency is now available through `/home/by/urnetwork/gvisor`, a symlink to the data-volume checkout `/mnt/data/sn-testnet/mainnet-gvisor-fork-20261003/gvisor`. The [source proof](evidence/local-gvisor-source-20261003.json) records repository `urnetwork/gvisor`, default branch `go`, commit `21c2a5da`, tree `e8600f8a`, and independent verification of all 2,440 tracked entries against their Git blobs and SHA-256 hashes. Its module declares Go 1.26.3. The checkout is clean; no frozen qualification graph was changed. This resolves the absent-directory obstacle, not compilation or behavioral acceptance of the new dependency. The final release must bind and qualify this exact source rather than depend on an unversioned sibling path.

The local-restore fixture mismatch has also been diagnosed: `localRebindTestRestore` requested a 16 MiB plan while the inherited production profile permits at most 8 MiB. Preserve original four-root normal/race outcomes and correct only the fixture request in separate successors for `2689fced` and `fcf41030`. This does not justify raising the production profile. The corrected fixture must exercise public planning and restore; compile success alone is insufficient.


The [original local-root normal failure receipt](evidence/local-root-original-normal-failure-20261003.json) preserves the exact `2689fced` source and raw outcomes: two fixture-planning failures and two passing authority-refusal controls. Coordinator verification rehashed every binding and matched the raw two failed/two passed root events. The failed public roots supply no restore acceptance; race/static results and the corrected fixture successor remain separate.


### October 3 qualification headroom restored

[Reclaim12](evidence/cache-reclaim-12-20261003.json) removes 450 old, inactive compiler archives totaling 7,633,731,584 bytes (7.11 GiB). The audited helper differs from reclaim11 only in its task directory. Privileged process-reference checks pass before planning and applying; the declared bounded metadata scan returns no references or diagnostics, and repeated physical identity checks plus final absence checks cover every selected file. The initial sudo PATH lookup failure is retained separately; the completed scan uses the absolute `rg` executable. Active test caches, source, evidence and executables remain intact. The volume returns to roughly 124 GiB free, allowing queued bounded qualification to continue without restarting existing handles. This is test-resource maintenance, not production capacity acceptance.


### October 3 corrected local-restore source join

The [fixture correction intake](evidence/local-fixture-correction-intake-20261003.json) binds `a07918db` over `2689fced` and `bdf63dea` over `fcf41030`. Coordinator verification rehashed all seven bindings and checked both exact Git parent diffs: each changes only `MaxPlanBytes` from 16 MiB to the existing 8 MiB limit in one test helper. Production and module changes are absent from those deltas. Independent qualification now runs seven roots on final `bdf63dea`: the two affected settled public paths, three pending-outcome paths and two authority-refusal controls. Results must name the actual tested final source; no separate acceptance of `a07918db` is inferred. Original failed `2689fced` runs remain retained.

**Optional recovery must not control foreground admission.** The Redis recovery draft needs an actual context budget for the whole page, not just a clock check between candidates. SQL and Redis waits must obey that budget. A timed-out optional SQL observation can abort a transaction even if its caller catches the error; never swallow that error and continue using an aborted transaction. Isolate optional recovery from healthy new admission where required, retain unknown tokens for later reconciliation, and verify slow/canceled recovery against a healthy foreground path with deterministic barriers. This is a review requirement for the separate successor, not a newly qualified behavior or authorization to release ambiguous SQL custody.


### October 3 full-model batch: companion query guard

The unchanged `3f32d730` full-model run found `TestOpenContractRuntimeQueriesUseStructuralPredicate` in addition to the real Redis refusal leak. The [exact-source review](evidence/companion-query-guard-review-20261003.json) verifies both relevant files against their Git blobs: the exported companion entry now delegates to the private shared implementation, whose SQL retains the structural outcome/dispute predicates. The test scans only the exported wrapper's string literals, so its zero count is a stale source-guard target rather than evidence that the runtime predicates disappeared. Preserve the failed run and correct the guard without weakening its required predicate counts. Explicitly protect public-wrapper linkage to the checked implementation; independently written EXPLAIN fixtures cannot alone prove the actual runtime query. Audit adjacent extracted-wrapper guards in the same batch, and qualify the final combined candidate. This source review supplies no newly passing test or completed model-suite acceptance.

The final `bdf63dea` restore batch has also exposed two pre-continuation pending-plan refusals: original members or owner checkpoints are omitted from its restore coverage. Keep collecting all seven roots, diagnose exact source/fixture coverage, and preflight reserved, acknowledged and first-stage variants before another freeze. Neither fixture setup refusal is silently converted into pending-continuation acceptance.


### October 3 completed restore failure collection

The [original `2689fced` race receipt](evidence/local-root-original-race-failure-20261003.json) preserves the same two capacity-fixture failures and two passing authority controls as its normal run. The [final `bdf63dea` normal receipt](evidence/local-pending-original-normal-failure-20261003.json) then collects all seven roots: four public paths fail at complete restore coverage admission, while first-stage generation and two authority controls pass; vet passes separately. Coordinator verification rehashed seven and nine bindings respectively and matched every raw top-level terminal outcome. None of the four public failures reaches successful local continuation. The shared inventory/checkpoint seam needs diagnosis across both settled and pending fixtures before another freeze; pending outcomes alone do not explain the whole failure class. Race collection continues on unchanged `bdf63dea`. Preserve these results and distinguish any fixture-only correction from a production coverage fix.


### October 3 paired-metadata core and signed-input placement

The [paired-metadata intake](evidence/paired-metadata-restore-intake-20261003.json) freezes Connect `5c48c3e5` and SN `11e282dc`. Coordinator verification rehashed all 25 intake bindings and checked clean commits, trees and all seven changed Git blobs. The [Connect primitive gate](evidence/paired-metadata-core-qualification-20261003.json) passes seven roots normally and under race detection, plus vet; all sixteen artifact/source/module/intake bindings and both raw seven-root censuses were independently checked. SN consumer qualification, old-source controls, restored preview/exclusive reconciliation and final source composition remain separate.

The [coverage diagnostic](evidence/local-coverage-diagnostic-20261003.json) reproduces one expected refusal after all fixed-owner planners and identifies eight unassigned external `.signed.bin` fixture inputs, with no omitted attributes. Its nine bound files and raw failed root are verified; the first missing-sibling workspace attempt remains recorded separately. The author traced these files to the test signer, while production retains imported bytes in `evmActionRecord.Signed`. The [separate `b35ca9c9` correction intake](evidence/local-signed-input-correction-intake-20261003.json) binds twelve files and changes only tests/documentation over `bdf63dea`: inputs stay beside the original config, journal/signature bytes are checked before restore, and all variants preflight complete owner coverage. Coordinator verification confirms the exact four-file delta and unchanged production/modules. This corrects fixture ownership; it does not exempt unknown runtime-root files or prove successful continuation before the actual public gate completes.

### October 3 Redis recovery source and failed fixture retained

The [frozen Redis intake](evidence/redis-recovery-source-intake-20261003.json) selects `f30583a5`, with production `164e42a6` and a test-only AST linkage correction. Coordinator verification rehashes all 23 intake bindings and all 5,384 tracked physical files against exact Git blobs; module bytes match `3f32d730`. Its recovery markers retain exact attempted tokens after exhausted/canceled cleanup, reconcile SQL custody under the original request fence, and isolate optional observation from the foreground transaction. These are source claims awaiting complete execution, not full release acceptance.

The [first seventeen-root normal receipt](evidence/redis-recovery-original-normal-failure-20261003.json) records sixteen passes and one fixture failure with successful owned cleanup. All 29 source/module/artifact bindings and the raw terminal counts are verified. The failing live-publication test holds a source-client share lock while synchronously awaiting a second caller's post-commit update on that same client. A separate test-only correction uses another client on the same payer network/balance; retain the original 30-second failure and verify the original request fence and 52-byte custody assertions. Extending the timeout alone would not repair that lock ordering.

The original full-model run also found public prober distribution and negative-reservation eligibility failures. They remain an open allocator review, not automatically harmless legacy assertions. Preserve public payer-hash spreading where required, retain exact allocation/priority and companion affinity, bound grant discovery/fallback work, and distinguish selection-cap holds from proven insufficient funding. Unknown/corrupt counters must not be silently repaired to zero; an independently valid neighbor may continue. The approved equal paid/free SN usage weighting does not authorize a change to Server funding eligibility. A separate allocator successor and final model qualification are still required.


### October 3 current recovery core publication and adjacent cancellation lessons

The [current paired-metadata core gate](evidence/current-connect-paired-core-qualification-20261003.json) passes seven selected roots normally and under race detection, with package vet and dependency preflight successful. The [exact source join](evidence/current-connect-paired-core-source-join-20261003.json) independently verifies all 3,730 physical Git blobs: every durablevolume mode/blob equals qualified `5c48c3e5`, and every outside file equals published `a53ed36a`. Upstream then advanced to `a8a71432`; the [published merge](evidence/current-connect-paired-core-publication-20261003.json), Connect `631bcb28`, preserves all of that upstream's outside mode/blobs and the exact 37 qualified core files. Push and subsequent pull completed, local/remote main agree, and the worktree is clean. Current consumer graph and whole-release qualification remain required; this does not inherit broad qualification of unrelated upstream changes.

The [original public paired-restore normal/race result](evidence/paired-restore-public-original-normal-race-failure-20261003.json) retains five passes and two failures in each mode. Fixture reserved-member ownership must bind the exact original intent and receipt rather than exempt a filename prefix or depend on iteration order. An acknowledged immutable-member mutation must retain the correct zero-payload-read rejection; prove one-read reuse separately on an admissible pending member. Test-only successor `3af77073` is a separate gate, not a rewrite of the failed `39e2744a` source.

The [Safe corrected assertion gate](evidence/safe-state-read-fixture-affected-qualification-20261003.json) verifies the affected root passing normally and under race detection, plus vet, on test-only `9223d8d3`. Account-specific instrumentation distinguishes legitimate proxy and singleton reads. Preserve the failed original `296980a0` normal result, returned-conflict refusal, and original signed retry budget. The remaining nine selected race roots are a disjoint pending scope; one positive does not qualify all ten or the current release graph.

The [EVM review/history source intake](evidence/continuous-evm-review-history-source-intake-20261003.json) verifies 42 bindings and thirteen changed Git blobs at `81de314b`. Its [first 25 normal roots](evidence/continuous-evm-review-history-new25-normal-20261003.json) pass with no failures or skips. Reviewed resource/settings acknowledgments must remain append-only with the original authority retained; record legacy latest-only imports honestly instead of inventing missing historical reviews. Cancellation after storage opens but before checkpoint load must be classified at that actual boundary, including adjacent native/provider/claim/service paths. A genuine identity, integrity or cleanup error retains precedence over cancellation. New deterministic barriers supply causal coverage; the naturally failing prior `a9eff813` run remains retained and is not claimed fully diagnosed from absent logs. Thirteen affected neighbors, race, causal controls and vet remain required.

Finish signed production capacity revisions, rolling independently reviewed claim expectations, current all-role composition and independent historical payer debit/refund verification. EVM receipt gas and effective price are not substitutes for a proved native fee witness. These changes qualify incremental recovery behavior; they do not close all PH/MG gates or authorize deployment.


### October 3 completed disjoint Safe scope and adjacent resource review

The [Safe joined scope](evidence/safe-state-read-joined-qualification-20261003.json) verifies all ten selected roots through disjoint evidence: nine historical normal positives from `296980a0`, the corrected affected normal/race root on test-only `9223d8d3`, and nine newly passing race roots on that successor. Vet passes. The original one-root normal failure remains failed and explicitly retained; production/module bytes are unchanged by the fixture correction. Old-source causal controls, current graph and final release qualification remain separate requirements.

The [EVM neighboring normal scope](evidence/continuous-evm-review-history-neighbors13-normal-20261003.json) adds thirteen passing roots to the separately verified first 25 normal roots on `81de314b`; race and operative controls remain pending. The [adjacent resource authority review](evidence/monitor-review-authority-adjacent-review-20261003.json) verifies two exact Git blobs and identifies non-adjacent A/B/A review-digest reuse as requiring a separate deterministic correction. Retained review references are labelled local configuration references, not verified signatures. Final configuration admission must bind actual approval authority; no scoped result grants signing or deployment authority. The replay backend prototype is proceeding separately, with historical native withdrawal/refund attribution still absent until proved.


### October 3 consolidated current readiness checkpoint

Use the [current 38-requirement checkpoint](evidence/current-readiness-checkpoint-20261003.md) to distinguish completed component implementation from remaining composition and live gates. It supersedes old absent-claim/customer/native-monitor labels without closing any whole PH/MG requirement or inheriting qualification across sources. The [current remote config inspection](evidence/current-cutoff-readiness-config-20261003.json) confirms the same October6 cutoff and pre-cutoff USDC obligation policy at config main `ba97927f`; `main/sn.yml` remains the exact published blob `05b56036`, activation is blocked, and deployment/readiness fields are empty. No local branch switch, production adoption or launch authority is inferred.


### October 3 corrected outer recovery and EVM race results

The [corrected outer fixture gate](evidence/paired-restore-fixture-affected-qualification-20261003.json) verifies three affected public roots passing normally and under race detection, plus vet, on exact test-only `3af77073`. All seventeen receipt bindings and raw per-root terminal results were independently rehashed/read. The original `39e2744a` two failures per mode remain failed historical evidence; the other five positive roots retain their original scope. Production bytes are unchanged by this correction. Current published consumer composition remains a separate requirement.

The [EVM review/history 25-root gate](evidence/continuous-evm-review-history-new25-normal-race-20261003.json) now passes all selected roots normally and under race detection at `81de314b`; sixteen bindings and both exact root censuses were independently verified. Thirteen affected race neighbors and ten operative control groups remain pending. Non-adjacent review-digest reuse still requires its separate successor. No native fee witness, whole-role acceptance or live economic approval is supplied by this receipt.

The [guarded cache reclaim](evidence/cache-reclaim-21-20261003.json) removed 450 inactive compiler archives, recovering 2.37 GiB. The helper changes only its receipt directory relative to the previously verified helper. Both privileged process censuses, no-match metadata reference scan, exact identity/age/archive guards, plan hash and allocated-byte sum were checked; all selected paths are absent. Active caches, modules, sources, evidence and executable files were preserved. Available data-volume space was approximately 114.66 GiB afterward. The 110 GiB admission floor does not stop or restart existing work.


### October 3 capacity revisions must preserve actual authority and admission

The [verified signed-capacity source candidate](evidence/production-capacity-revision-source-intake-20261003.json) adds an actual public proposal command, independent full-config approval linkage and retained-prefix checks at the production loader/runtime boundary. All fourteen changed source blobs, 29 bindings and four clean compile/preflight outcomes are verified. The eleven new deterministic public/config/ledger roots and four history neighbors still require Sol execution; compilation is not continuation acceptance.

Retained capacity must include complete physical source census and future forecasts, with a 2× margin and checked arithmetic across records, trails, bytes/files and aggregate history. Exact predecessor/economic authority, signed history and per-record/transport/member limits stay unchanged. Snapshot leases are exclusive, so affected-owner quiescence and explicit revision adoption must be planned before exhaustion while healthy peers continue. Do not quietly reinterpret funded slots as active membership, approve new spending through a storage revision, or claim automatic online adoption from an unsigned preview. The candidate is the validator slice; complete actual miner/operator/monitor capacity consumers, warning/adoption behavior, archive/restore and current-source qualification before closing PH-20/PH-23/MG-09.


### October 3 current production module join and EVM causal controls

The [current module source join](evidence/current-release-module-source-join-20261003.json) verifies clean SN `b18e76dc` and Server `5089a9dd`: 20,377 and 5,387 physical Git blobs respectively, exact production mode/blob equality to their parents outside `go.mod`/`go.sum`, and unchanged final heads/trees. Both select published Connect `631bcb28`, SDK `9ae95704`, SCTP `6443417d` and the explicit local gVisor fork. The [compile-only receipt](evidence/current-release-module-compile-20261003.json) verifies all 28 bindings and terminal clean-source exits for both actual GOWORK=off graph/dependency preflights and five package test binaries. This candidate excludes pending capacity/EVM successors; the live Server `1d3c275b` full model stays on its original graph. Independent current-role behavior, final source/dependency provenance and release qualification remain required.

The [EVM normal causal receipt](evidence/continuous-evm-review-history-causal-normal-20261003.json) verifies ten operative omission groups and twelve exact intended failed roots. Coordinator checks cover 59 receipt artifacts, forty author/independent overlay/source/replacement bindings, exact remapped overlay tables, and actual terminal roots and named behavioral assertions. These are expected control failures on frozen `81de314b`, not new product regressions or old-whole-source causal claims. Affected-neighbor race and race controls remain pending.

Qualification fixture admission must protect its owned durable root/ancestor modes before opening storage owners. A fixture setup refusal does not justify weakening production custody checks. Preserve failed setup evidence, correct only the owned environment, and rerun only affected bodies when the earlier positives are independent of the corrected metadata. Capacity `b1c5fb23` initially passed five validator roots and refused two at the owned unprotected ancestor; independent corrected execution and remaining scopes are pending, not inferred from the compile pass.


### October 3 corrected full model completion and current publication seam

The [corrected Server full-model receipt](evidence/customer-corrected-full-model-terminal-20261003.json) is terminal PASS at exact `1d3c275b`: 1,484 top-level roots, 1,473 passes, zero failures and eleven individually documented optional/input-dependent skips. Coordinator verification rehashes all 23 artifacts, reads every raw root event and skip reason, checks the single passing package terminal at 6,572.307 seconds, all service/runner cleanup exits and the exact owned network cleanup ID. The original `3f32d730` nine-failure result stays retained. This completes that model scope on its own graph; current module and deployment acceptance remain separate.

Publication must also preserve newer Server main `f024b11a`. The [readonly integration review](evidence/server-current-publication-integration-review-20261003.json) finds three actual conflicts against `5089a9dd`: migrations, module declarations and Redis admission. A separate Astra successor must preserve the published migration prefix, bounded grant selection, shrink-to-fit, and exact asynchronous debit-journal authority alongside owned-token recovery. Already reduced amounts and pending debit debt cannot be interpreted as absent custody or prematurely released. Review clean textual merges at the same semantic boundary and add deterministic affected/adjacent controls; the completed old-source model run is not evidence that this new join works.

The [EVM 38-root product gate](evidence/continuous-evm-review-history-product-qualification-20261003.json) passes every selected root normally and under race detection, plus vet, on `81de314b`. The [ten-group causal gate](evidence/continuous-evm-review-history-causal-qualification-20261003.json) discriminates twelve intended failed roots in each mode; 99 artifacts, exact selectors and named assertions are verified. These scopes do not supply the separate digest-reuse successor, true historical fee debit/refund witness, current-source composition or live observer acceptance.

The [guarded cache reclaim22](evidence/cache-reclaim-22-20261003.json) recovers 14.37 GiB from 4,500 old inactive compiler archives, preserving active/warm caches, source, evidence, modules and executables. Minimum archive size is now1MiB and the administrative batch cap4,500, while the16GiB ceiling,48-hour age and all identity/process/reference/archive guards remain unchanged. An overbroad helper text replacement was caught and retained before deletion; the final helper uses four exact context replacements and an independently verified reverse comparison. Available data volume space was approximately125GiB afterward, admitting queued qualification without restarting any work.


### October 3 historical proof completeness and retry-attempt admission

The [historical proof-backend qualification](evidence/historical-proof-backend-qualification-20261003.json) verifies twelve synthetic replay roots on fixture-corrected `93907cd3` and two isolated operative guard omissions. All 21 receipt artifacts and fourteen physical crate sources match their recorded hashes and exact Git content. Removing either top-level or child write-path precheck compiles but incorrectly accepts the unchanged declared root; the corresponding test fails at its intended assertion. The pinned SDK can fall back to the original/default root after an internal proof error, so an apparently matching post-state root alone is insufficient. Preserve fallible proof-completeness checks before root calculation; incomplete reads or writes must publish no authenticated fact.

The original `89e4c8c` fixture failures remain retained. Runtime-version fixtures must advertise the Core API layout that their SCALE encoding uses and assert strict encode/decode round trips before exercising execution. The correction changes only the test fixture; it does not authorize a runtime. This qualification uses caller-supplied anchors and synthetic runtime bodies. Actual mainnet runtime host admission, independent finality, native fee withdrawal/refund attribution and the production caller remain required for MG-03/MG-06.

HTTP resilience must admit a successful slow read within its total retry budget. A 300-second budget with five-second header/body attempts still rejects reads that need longer than five seconds. The separate `ff42a433` successor raises attempts to sixty seconds clipped by the remaining owned budget, adds typed transient route/DNS errors, and supplies the missing shared `rpcWait` hook declaration. Sol qualification is pending; source compilation does not establish behavior. Retain permanent identity/authentication errors, NXDOMAIN and mixed hard causes as failures, and keep this GET policy separate from transaction broadcast reconciliation.

Signed configuration previews must commit to the exact strict-decoded document that will be loaded. Nil versus empty collection round trips must not invalidate a retained approval. Test actual public preview→independent signing→document export→production reload without an in-memory rehash shortcut; preserve economic authority and original signed sidecars. The capacity candidate's remaining canonical-hash failure is being corrected in a distinct successor and is not a reason to bypass production approval checks.


The [capacity affected-scope failure receipt](evidence/capacity-affected-canonical-failure-20261003.json) independently verifies 27 artifacts and all four raw selected roots in each mode: three pass and the retained original-sidecar root fails at the same complete-configuration approval hash normally and under race detection; vet passes. These are terminal results, not ongoing tests or a capacity acceptance. Keep the failure visible while qualifying the separate fixture and public pre-sign document round-trip successors. Preserve the passing independent scopes rather than restarting unrelated work.


### October 3 body errors must retain identity and hard-cause precedence

The [HTTP attempt source review](evidence/monitor-progress-attempt-source-review-20261003.json) verifies all 33 nested intake bindings, four changed Git blobs and eight clean peer heads/trees for `ff42a433`. Qualification remains separate. The shared transport classifier handles mixed causes, but both actual provider and claim body paths still promote any read/close error to retryable and check complete identity only when the read error is nil. This adjacent gap requires a distinct successor: explicitly classify transient body/close I/O failures, preserve permanent causes inside joined errors, reject a completely decoded foreign identity even when the final read also reports a transport error, and continue retrying genuinely incomplete bodies. Add deterministic positive partial-body recovery and negative mixed-hard/foreign-identity controls for both consumers. Do not replace final integrity checks with a blanket retry, or classify an incomplete JSON sample as authoritative malformed evidence.


The [HTTP attempt selected qualification](evidence/monitor-progress-attempt-selected-qualification-20261003.json) now verifies 29 selected roots normally and with race detection, with zero failures or skips, plus vet and effective dependency preflight. All 31 artifacts, four independent overlay Git blobs and exact raw root/package terminals are checked. This proves the selected timeout/status/network/peer-continuation slice at `ff42a433`; seven operative controls and the separately discovered body/close hard-cause successor remain pending. Retain the original `a5d2c5b4` compile failure and do not infer rolling policy, composed-release or deployment acceptance from this slice.


### October 3 current Server financial integration preserves published history

The [current financial source join](evidence/server-current-financial-source-join-20261003.json) verifies clean `5f2edd47` against both merge parents: all 5,439 physical Git blobs and modes, 27 artifact bindings and four clean graph/compile-only jobs. Exactly 5,326 blobs are retained from the qualified parent, 104 match incoming published main and nine differ from both. The author reviews ten files because the retained module file is also in its explicit review scope; review categories must not be mistaken for changed-blob counts. Published `f024b11a` migration source is byte-for-byte preserved with only the exact final customer migration append before the closing brace.

Current settlement uses the incoming durable asynchronous debit journal. Recovery must retain pending payer consumption until committed debit application, recognize already reduced token amounts and preserve debt for archived contracts; provider payout rows are not payer-debit authority. Bounded grant selection may shrink only after a complete census, and public consumers must use the actual granted amount. The new merge has its own 22-root normal/race and five-package vet scope with operative controls. Source/compile verification and the completed older full model are insufficient to accept the merged behavior, database rollout or final release.


The separate [HTTP body-error source successor](evidence/monitor-progress-body-source-intake-20261003.json) is frozen at `1a4524f2`: complete decoded identity now precedes a terminal read fault, and observed body/close causes override status-only retry permission through all-causes transient classification. Explicit EIO remains recoverable; mixed permanent causes refuse. Four new tests cover both consumers, with seven neighbors and separate prior-reader controls. Source and artifact checks are complete; Sol behavior qualification remains pending, so the earlier discovered gap is implemented but not yet accepted.


The [HTTP retry normal controls](evidence/monitor-progress-attempt-controls-normal-20261003.json) verify seven isolated groups and fourteen intended failed roots at `ff42a433`. All 52 artifacts, exact remapped replacement tables, source hashes and raw named assertions are checked; every group compiled and reached its behavioral assertion. This complements the separate 29-root normal/race product gate. Race controls and the body-error successor remain distinct scopes.

The [guarded cache reclaim23](evidence/cache-reclaim-23-20261003.json) removes 2,568 old inactive compiler archives, recovering 3.30 GiB. The helper changes only its receipt directory; both privileged process censuses and all reference, identity, age, size and archive checks remain unchanged. An unprivileged metadata scan encountered protected directories and was retained; deletion occurred only after a privileged clean scan. All selected paths are absent. Approximately117.35GiB remains available, admitting queued qualification while preserving active caches, modules, sources, evidence, executables and every running job. The110GiB floor governs admission of new heavy work, not termination of existing work.


### October 3 qualified current Server published; preserve explicit remaining gates

The [current Server product qualification](evidence/server-current-financial-product-qualification-20261003.json) verifies all 22 selected roots normally and under race detection, five-package vet, owned fixture start/cleanup and graph preflights. All 84 unique artifacts, 5,439 independently archived Git blobs/modes, the actual 647-module graph and seven clean pinned peers are checked. The [isolated prior-file controls](evidence/server-current-financial-controls-qualification-20261003.json) verify three exact one-file overlays and five intended failed roots in each mode, with 55 artifact bindings and clean fixture lifetimes. These tests cover the merge's bounded shrink, pending/applied/archived debit custody, public consumers and lost-ack hook; the older `1d3c` full-model result retains its separate source.

The [publication receipt](evidence/server-current-financial-main-publication-20261003.json) verifies remote main and the ready release branch at exact `5f2edd47` / tree `4728109a`, plus both clean local checkouts. Published `f024b11a` is retained as a merge parent; remote main advanced without force. The local main was pulled only after a privileged 385-process ownership census found no active references. Frozen test sources and earlier module peers were preserved. Publication does not apply database migrations, deploy services, sign transactions or close the remaining composed-release/mainnet gates.

The [HTTP retry causal qualification](evidence/monitor-progress-attempt-controls-qualification-20261003.json) now verifies seven groups/fourteen intended behavioral failures in each mode, 89 unique artifacts and exact remapped replacement tables. This completes that causal scope alongside its 29-root product scope; body-error and rolling-policy successors remain distinct.

The [capacity wire failure review](evidence/capacity-wire-producer-failure-review-20261003.json) separates two failures at frozen `e606947d`: the original sidecar fixture mixes envelope allowances, while the actual preview serializes untagged nested bounds with JSON Go field names that the strict YAML loader rejects. Both recur normally and under race detection, with vet passing. The successor must emit a loader-compatible exact ConfigDocument before signing, then complete only the established approval descriptor after independent signature and verify the exact emitted document through the public loader. Validate the whole original profile after all fixture helper overrides; preserve approvals and envelope checks. Neither failure is waived or counted as a capacity pass.


### October 3 body-error retry qualification

The [body-error qualification](evidence/monitor-progress-body-qualification-20261003.json) verifies the exact `1a4524f2` successor: four new and seven adjacent roots pass normally and with race detection, with vet and dependency preflight passing. Two exact prior reader bodies produce two intended failures and two positive controls in each mode; 54 artifact bindings and six independent source overlays were checked. Complete decoded foreign identity must remain a permanent refusal even when the final read or close times out. Partial transient body failures and explicit EIO close failures remain retryable; a mixed permanent cause cannot borrow retry permission from HTTP status or another transient cause.

This qualifies provider/claim body handling on its recorded Connect631/SDK9ae/Server5089 graph. It does not qualify the final combined release, native fee authority, policy renewal, archive rollover or live activation. Preserve successful checkpoints and retry only affected reads. Capacity CLI tests currently demonstrate correct strict rejection but still require correction of negative-test serialization and diagnostic fixtures; no failed test is waived or counted as passing.


### October 3 retained monitoring policy renewal qualified

The [policy renewal qualification](evidence/monitor-policy-renewal-qualification-20261003.json) verifies `1a98ef89`: nine new and ten adjacent roots pass normally and with race detection, plus vet and graph preflight. Three isolated compiling controls produce six intended failures per mode. Ninety unique artifact/source bindings, eleven exact Git overlays and the original four-field JSON grammar are verified. The grammar fixture is synthetic, bound to prior source bytes; it is not claimed as output from an old executable.

Budget, review-history and claim-epoch capacity may grow without dropping the original roster, expected debts, policy origin or acknowledged history. Record a policy acknowledgment only after durable publication and owner verification succeed. Refuse predecessor mismatch, resource shrink, immutable expectation changes and nonadjacent review-reference reuse. A local review reference is not a cryptographic signature or payment/finality authority. Finite growth to128 entries does not close indefinite archival rollover: archive provisioning, capacity forecasts with the approved2x margin and actual host adoption remain launch work. Qualification uses the explicit Connect631/SDK9ae/Server5089 graph; final release composition remains separate.

The [guarded cache reclaim24](evidence/cache-reclaim-24-20261003.json) recovered3.17GiB from4,500 inactive compiler archives with48h age, process ownership, identity, archive and reference guards. Active warm caches, modules, source, evidence and executables remain preserved. An initial missing scan-filename alias refused before deletion; the successful retry retained the clean scan and repeated process/identity checks. Admission headroom must not stop or restart existing work.


### October 3 EVM retained review history qualified

The [six-root product qualification](evidence/evm-review-reuse-selected-qualification-20261003.json) verifies `8a2693c2` normally and with race detection, plus vet. The [exact prior-file control](evidence/evm-review-reuse-causal-qualification-20261003.json) restores only the old81de resource-history validator and reproduces both intended failures in both modes. Thirty-two product/source bindings and seventeen causal bindings are checked; a first control overlay with duplicate declarations is retained as a harness failure and excluded from causal evidence.

Every nonempty review reference must be unique across the entire retained history. Comparing only adjacent revisions permits an internally hashedA-B-A chain to reuse already acknowledged authority. Preserve all resource-growth, predecessor, content-hash and durable acknowledgment checks. This is local review provenance, not a cryptographic signature or payment authority. The qualified graph remains historical5def/SDK95/Server0e; the final current-source release must incorporate this exact correction and qualify its changed composition.

The [capacity fixture review](evidence/capacity-test-successor-source-review-20261003.json) verifies two successors change only one test file. A canonical public approval and full config-loader baseline must succeed before negative faults can be meaningful. Each refusal must reach its expected boundary, return the exact refusal code, emit no config and preserve custody; no production guard is relaxed to accommodate fixture errors.


### October 3 public capacity completion qualified

The [public completion qualification](evidence/capacity-public-completion-qualification-20261003.json) verifies the `214a6b3d` test-only successor over unchanged42ef production. Three unique selected roots pass normally and with race detection; mainnet and validator vets pass. Thirty-four artifact/source bindings and seven exact Git overlays are checked. The mainnet roots execute directly on the successor test source, while the byte-identical validator sidecar results retain their original42ef source.

Use the actual emitted strict-YAML configuration document and exact preview/signing message through the public approval/completion/config-loader path. Complete readonly output only after all independent authority, signature, hash and semantic checks succeed; report short writes without claiming delivery. Tests must admit the valid baseline before injecting negative faults and verify each fault reaches its intended boundary. Earlier canonical-envelope, diagnostic and private-directory setup failures remain recorded. This closes the selected public CLI slice only: final current-source composition, aggregate sizing, signed archive-capacity adoption, backup/restore and deployed host adoption remain required.


### October 3 subprocess bounds must cover fast-copy dispatch

The [source review](evidence/subprocess-fast-copy-bypass-source-review-20261003.json) identifies a common output-control bypass in the replay caller, owner signing adapter and service storage inspector: anonymously embedding `bytes.Buffer` promotes `ReadFrom`, which permits `io.Copy` to bypass the wrapper's guarded `Write`. This skips output limits and can skip the callback that cancels an owned process. Use a named private buffer and test the real pipe-copy and subprocess paths, including both stdout and stderr, overflow, cancellation and descendant cleanup. Audit related `ReaderFrom`/`WriterTo` fast paths and post-allocation output checks. Preserve original source/test failures and signing authorization semantics; the source finding alone does not qualify the fix.


### October 3 native history archive and current release source

The [native archive source review](evidence/native-archive-source-review-20261003.json) checks22 bindings, nine exact changed paths and unchanged modules at `3bc2f9a5`. Public `monitor-native-archive plan` and `apply` retain original checkpoints, cursor, pending high-water, cumulative amounts and runtime reviews. Publish and authenticate the archive before compacting active history. Require provisioned owners, exact file references and a joined affected-writer fence; healthy roles continue. The initial128-segment profile requires explicit forecasts and2x segment/byte/inode margins. Signed catalog growth, claim/EVM adapters, complete archive backup/restore and behavioral qualification remain required. This is source review, not deployment or a tested rollover claim.

The [frozen866 release source review](evidence/ready-866-source-join-review-20261003.json) independently authenticates20,453 SN and5,439 Server physical Git blobs/modes plus six merge parent/base joins. Its [556 publication successor review](evidence/ready-publication-source-successor-review-20261003.json) adds only two exact qualified8a Go blobs plus published docs/evidence, retains083main history and leaves modules unchanged. The16-root current-source composition is still pending; native archive, replay and native-fee observer are separate increments. Preserve those explicit exclusions until their qualified production changes are joined.

The [guarded cache reclaim25](evidence/cache-reclaim-25-20261003.json) recovered1.53GiB from3,877 old inactive compiler archives; process, reference, age, identity and archive guards remained unchanged. Source, evidence, modules, executable artifacts and active caches are retained. Resource admission can defer new heavy jobs but never invalidates completed work or stops active jobs.


### October 3 assembled release source and compile checks

The [candidate source/compile review](evidence/current-release-556-source-compile-review-20261003.json) independently verifies20,461 physical tracked Git blobs, modes and SHA256 values at `556e1476`, plus33 artifact bindings. The candidate joins the qualified recovery, EVM review history, GET retries, retained policy renewal and public capacity completion with published Server5f. Clean unchanged-source module/dependency preflights and affected role compiles pass. A prior Server package-list command named nonexistent `./storage`; its failure is retained, and the corrected command passes on the same source.

The candidate still requires16 targeted public roots normally and under race detection plus five-package vet before publication. Earlier component receipts keep their distinct sources and graphs. Native archive rollover, historical replay and native-fee observer are excluded from this candidate until their independent fixes and qualification are joined. Source/compile success does not establish deployed identity, runtime approval, owner-device custody, complete conservation or settlement behavior.


### October 3 assembled production increment qualified

The [composed qualification](evidence/publication556-composed-qualification-20261003.json) verifies68 bindings and all16 targeted public roots normally and with race detection at `556e1476`, plus vet in mainnet, validator, miner, chain and crv4. The actual current graph uses published Connect631/SDK9ae/SCTP644, Server5f and gvisor21c2. The qualified increment joins retained recovery, EVM history/review reuse, provider/claim retries and policy renewal, and public capacity completion. Merge and publication must preserve these exact production inputs while retaining newer documentation. Full reproducible images, actual host/device/runtime admission and observed settlement remain separate requirements.

The [original replay failure review](evidence/replay-caller454-original-failure-review-20261003.json) preserves all ten original public roots: seven pass and three fail around300 seconds at partial-output, output-bound and blocked-input cancellation. A separate later-root invocation reproduces the same blocked-input failure. These failures are not waived. The output successor is independently tested while original evidence stays immutable.

The [original-Wasm selected qualification](evidence/original-wasm-observer-selected-qualification-20261003.json) verifies24 bindings, fifteen exact Rust Git files and nineteen passing library roots on the locked/offline graph. Original-code host frames, rollback and post-state logic remain synthetic scope; binary and causal receipts are separate. Native fee withdrawal/refund amounts and finality authority remain unproved until the actual-runtime witness/profile is admitted.

Test harness isolation must cover compiler caches and child environments. Distinct mutated Rust crates with the same package identity can reuse a stale artifact when copied source mtimes precede the shared target; require verified mutated source and actual recompilation before accepting causal results. Pass parent Go modfile/overlay flags directly to the test command rather than leaking them through GOFLAGS into synthetic child modules. Preserve invalid setup attempts separately; they are not product behavior or causal evidence.

The [cache26](evidence/cache-reclaim-26-20261003.json) and [cache27](evidence/cache-reclaim-27-20261003.json) receipts verify removal of inactive regenerable compiler archives only. Cache27 extends selection to archives older24 hours, preserves active-cache/process/reference/identity guards, and applies a strict383-file ordered subset below16GiB; an original trailing archive that exceeded the generator budget remains preserved. It recovered15.99GiB, restoring qualification headroom without stopping active work. Resource admission must use the remaining job increment and2x margin above the retained floor, then recheck between new phases.


### October 3 qualified production composition merged into main

The [merge review](evidence/qualified-composition-main-merge-20261003.json) records `e34c683e`, joining qualified556 into main while preserving newer plans and evidence. Every tracked input outside the two launch plans and mainnet/evidence is byte-exact to the independently tested candidate, including Go, modules, configurations, contracts, scripts and test fixtures. Documentation-only differences do not require repeating the16-root gate. This increment includes recovery/preparation/restore, EVM observation and review reuse, provider/claim retries and retained renewal, and public capacity completion. Native archive, replay/output and actual native-fee increments remain separately qualified work; final images, authority, custody, deployed adoption and live settlement remain open.


### October 3 replay cancellation qualified; native fee decoder and restore limits

The [publication readback](evidence/qualified-composition-main-publication-20261003.json) confirms qualified production inputs on remote main at `ac8a09ae`, following successful pull and push. Subsequent documentation updates do not change that code scope.

The [replay output qualification](evidence/replay-output-caller1b3f-qualification-20261003.json) independently verifies103 artifact bindings, six changed Git blobs and their actual execution overlays. All23 selected roots pass normally and under race detection; mainnet vet succeeds. Five omission groups produce six intended behavioral failures in each mode. Named buffers prevent promoted ReaderFrom from bypassing output guards, active overflow cancels children, and bounded collection applies to adjacent owner signing, volume inspection, repair and source-lock commands. Preserve the original454 genuine timeout failures and the three separate child-GOFLAGS harness failures; only the latter were corrected by invocation isolation. This qualification uses the historical Server5089 graph. The current5f Go/Rust caller composition remains a separate required gate, and no actual owner device or deployed host is covered.

The [original-Wasm fee decoder qualification](evidence/original-wasm-fee-decoder-a7ed-qualification-20261003.json) verifies71 bindings, all16 actual crate Git files,27 passing library roots and five recompiled omission controls. Metadata, payer, amount layout and extrinsic placement must agree before an event becomes an attributed candidate. Missing refund evidence stays unknown rather than becoming zero or an inferred debit; the omission control demonstrates the false1000-rao attribution. These synthetic fixtures do not establish actual-chain fee authority. Require the original runtime profile, supported host surface, complete parent witness, failed-refund branch coverage and finality before accepting native conservation evidence.

The [restore source-limit review](evidence/native-archive-restore-owner-capacity-source-gap-20261003.json) confirms that published Connect631 admits at most32 preparation owners and128 owner attributes. A per-head restore profile cannot represent every head of the accepted long-history archive/catalog namespace. The successor must export, restore and restart the entire accepted segment union plus active/chain heads, preserving signed capacity authority and the2x forecast margin. Qualifying only small history or silently reducing accepted history does not resolve this seam. Archive/catalog tests already running remain independent and must retain their successful work while the restore increment is implemented.


The [native history archive qualification](evidence/native-history-archive3bc-qualification-20261003.json) verifies27 artifact bindings and all20,382 physical tracked source blobs and executable modes at `3bc2f9a5`. Nine public archive roots pass normally and under race detection, with mainnet vet successful. Restoring the exact old dispatcher makes the public rollover test fail at the missing archive command, demonstrating actual command admission. Rollover, lost acknowledgments, missing/forged history, affected-role isolation and unchanged-payload work counts are covered. The signed catalog, full accepted namespace restore, claim/EVM adapters and final release composition remain separate required work.


### October 3 original-Wasm binary/control review and existing-cache qualification

The [observer binary/control review](evidence/original-wasm-observer-binary-causal-qualification-20261003.json) verifies45 artifact bindings, fifteen exact original crate files and the locked/offline historical-proof binary. Three actual recompiled omission controls fail at body identity, storage rollback and changed post-state assertions. Each control changes exactly one production source file. Preserve the two stale Cargo artifact passes as invalid harness results; they do not establish behavior. Binary construction and synthetic omission controls still do not admit an actual-chain runtime, transaction fee authority or finality.

The [existing-cache source selection](evidence/existing-validator-cache-selection-source-review-20261003.json) pins ten actual published-source blobs and41 distinct validator roots for current-graph qualification. Boundary, client-key authority and assignment verification already include real work counters, cancellation, changed-domain rejection and invalidation checks. Reuse those mechanisms and prove their behavior on the current release rather than adding parallel caches from an inventory alone. Durable receipt checkpoint tests cover misses, custody and scanner continuity; they do not prove full archive/predecessor work reuse. Add a separate actual full-lineage archive admission/use work-count probe, preserving complete predecessor and HeadEMA verification and final canonical checks. Source selection is not a passing test result or closure of PH-05/PH-17/PH-24.


The [native signed-catalog qualification](evidence/native-signed-catalog1b5-qualification-20261003.json) verifies40 artifact bindings and20,386 physical source blobs/modes in each of the current and old08f control trees. Twelve public catalog/archive roots pass normally and under race detection, with mainnet vet successful. The old dispatcher refuses the missing catalog command; the old importer passes signed growth but rejects the independently signed complete frame in both modes. Bound the full schema/signature envelope separately from the revision payload: a valid16KiB revision can produce a larger signed frame. Preserve explicit approver enrollment, signed monotonic revisions, original pending ranges and forecast checks. This qualifies catalog adoption, not full accepted namespace restore, cross-consumer archive adapters or production deployment.


### October 3 cache fixture corrected; real caller protocol verified; complete restore scope retained

The [original cache census](evidence/validator-cache41-original-failure-review-20261003.json) preserves40 passes and one permission-fixture failure in both normal and race modes. Required umask077 masks requested0644 creation to0600, so a negative permission fixture must explicitly set and verify the fault mode. The [Astra correction](evidence/validator-cache-permission0b58-qualification-20261003.json) changes only that test function, checks serialization errors and actual regular-file permissions, and passes all three same-file affected roots in both modes plus validator vet. Thirty-eight unchanged roots retain their exact original passing evidence. The [merge](evidence/validator-cache-permission-main-merge-20261003.json) preserves all production and module inputs. This is joined scoped qualification, not a claimed single41-root successful invocation or complete historical lineage work-count proof.

The [actual Go/Rust caller commands](evidence/current-fee-caller-e274-cross-engine-qualification-20261003.json) verify36 bindings, protected single-link executable custody, exact job hashes and typed outputs for paired, missing and explicit-zero refunds. Paired1000/250 yields candidate750; missing refund stays unknown; explicit zero yields candidate1000. Runtime admission stays false and top-level native expenditure null in all three synthetic fixtures. The [original selected caller gate](evidence/current-fee-caller-e274-original-gate-review-20261003.json) separately verifies all20,333 physical source blobs,16 normal passes and15 race passes with one deadline-fixture failure. A five-second prelaunch deadline cannot guarantee a child starts under race instrumentation. The reported whole-test duration includes fixture preparation and does not prove a missed production deadline. Astra's deterministic actual-output deadline control and preexpired-owner refusal are independently qualified before replacing that fixture; no original failure is relabeled.

The [cross-root archive source review](evidence/native-archive-cross-root-restore-scope-review-20261003.json) shows that current archive admission permits original and archived heads under different declared roots. Complete restore therefore requires a reviewed cohort, retaining every original signed reference, complete co-owner/capacity coverage and incremental recovery after partial restoration. The514-head same-root increment remains distinct; it cannot close this already accepted cross-root scope.

The [retained original observer binary](evidence/original-wasm-observer-binary-retention-20261003.json) preserves exact d022 bytes in an owned single-link artifact after the shared compiler output path was reused for a newer decoder build. Original receipts remain unchanged. Compiler target names are mutable build locations; copy byte-exact executables into stable retained evidence before target reuse, and never relabel a newer binary as an earlier source.


### October 3 deterministic deadlines and current release integration

The [deadline correction qualification](evidence/fee-caller-deadline4c96-qualification-20261003.json) verifies21 artifact bindings and20,333 physical Git blobs/modes. Four affected roots pass normally and under race detection, with mainnet vet successful. The test expires the owner deadline at actual partial output, verifies the inherited deadline and joins the real child. A preexpired owner refuses before opening an absent engine. This corrects a timing-dependent fixture without changing production timeouts; thirteen unchanged original race passes remain separate evidence, not a claimed single17-root invocation. Keep the original failure intact.

The [current release source join](evidence/current-caller-cb796-source-join-review-20261003.json) verifies27 artifact bindings,20,503 physical Git blobs/modes and23 changed paths over published3dc main. Twenty-two paths retain the qualified historical caller bytes; the remaining path adds only the public dispatcher to current main. Qualified556 production, current modules, cache-fixture correction and existing plans remain retained. Current-graph behavioral qualification is pending. A root-only storage inspector fixture must be reported as skipped in an ordinary user run and qualified separately with its explicit synthetic fixture authorization; a skip cannot establish deployed storage behavior.

For restore tests, isolate archive and preparation request namespaces. The original same-root fixture wrote both requests to the same metadata/request.json, overwriting the daemon preparation request before restore. Preserve the resulting original failures; qualify the test-only correction and the complete512-segment/514-head path independently. Do not weaken request admission to accommodate an overwritten fixture. Cross-root restore remains required because existing archive admission already permits heads on different declared roots. Hold all affected root leases, authenticate the complete cohort before any mutation, and retain each root's incremental journal and original signed references.


### October 3 EVM archival increment and large-restore race follow-up

The [EVM adapter source/compile review](evidence/evm-history8d-source-compile-review-20261003.json) verifies42 artifact bindings and all eleven changed physical Git blobs/modes at8d9904a5. Archive the exact acknowledged head before compacting; recover lost acknowledgments from exact original/next bytes, preserve credit/carry snapshots, cumulative event/fee counts, receipt-derived fee costs and original resource-review/catalog prefixes. Hold shared readers after admission and check custody without repeating payload reads. Signed capacity and2x physical forecasts remain independent of economic authority. Nineteen new public roots, eight neighbors and five omission controls await behavioral qualification; compile success is not a passing run or actual native-fee/finality authority.

The [original restore and scoped fixture review](evidence/native-restore-original-and-fixture259-review-20261003.json) verifies83 artifact bindings and the actual test-only delta. The original512-segment restore passes normally but its race run times out waiting60 seconds for the next sample. This is distinct from the request namespace collision, whose three affected roots now pass normally and under race detection with vet successful on test-only259. Preserve both original failure classes. Investigate actual worker progress and errors before attributing a sample observation timeout to permanent failure. Complete large-history race qualification and the already accepted cross-root cohort are still required; do not replace them with only small same-root tests.


### October 3 actual fee-proof acquisition and qualification headroom

Actual runtime fee authority requires a complete execution witness. An arbitrary-key state_getReadProof response does not establish that every execution read, write or prefix path was covered. The remaining collector must use the pinned SDK execution-proof mechanism against the canonical parent state, capture the exact child body/header and original runtime code/heap configuration, bound work and honor cancellation without committing state. Admit the job only after original-runtime replay reproduces the child root. Independently review the fee callsite/profile, payer mapping, failed/partial/zero refund branches and finality; incomplete evidence remains unknown. This is required implementation work, not a waived deployment condition or a confirmed RPC capability blocker.

The [guarded cache reclaim28](evidence/cache-reclaim-28-20261003.json) recovered5.41GiB from1,722 inactive unreferenced compiler archives, preserving source, evidence, modules, executable artifacts and active caches. Qualification admissions must recheck their own2x incremental forecast above the retained floor; a saved free-space snapshot does not authorize later work. Existing successful results and live processes remain retained.


The [large-history diagnostic source review](evidence/native-admission-ceae-source-review-20261003.json) verifies a two-test-file change over259 with all production/module tree entries retained. Observe positive reads of every exact retained inode under a300-second liveness budget, then require the unchanged public sample; access events alone do not prove admission. Preserve early worker errors, cancellation and original timeout evidence separately. Check observer cleanup errors and join its reader. Behavioral qualification and the cleanup successor remain separate from this source review.


### October 3 current replay/caller increment qualified and merged

The [current caller qualification](evidence/current-caller-cb796-joined-qualification-20261003.json) verifies59 artifact bindings. Normal and race ordinary-user gates each pass11 roots with one intentional root-only skip; the skipped storage inspector passes separately in both modes using protected pinned binaries and the observed filesystem UUID. Mainnet vet passes. The [merge review](evidence/current-caller-cb796-main-merge-20261003.json) verifies all23 actual changed files at778fca83 against the independently qualified candidate; every other code/module/config input matches that candidate while newer plans and evidence are retained.

Main now includes bounded subprocess collection and cancellation for replay, signing adapters, repair and storage inspection, plus the public original-Wasm historical verifier and typed fee-observation caller. Missing refund evidence remains unknown; synthetic candidates remain unapproved. Complete actual-runtime proof acquisition, source/profile/finality authority, archive adapters, full cross-root restore, deployment and observed settlement are still required. A published verifier alone cannot authorize production native fee accounting.


### October 3 reusable compiler-control guard review

The [Cargo guard source review](evidence/cargo-control-f0-source-review-20261003.json) verifies three workflow files at f0efe0f6 without a production change. Require the exact baseline and declared mutation, a fresh compiler artifact from that physical crate, an immutable retained executable and the intended single-test assertion. Preserve stale reused-binary passes as invalid harness evidence. Package-only cleanup and a shared target lease preserve warm dependencies and refuse active owners.

The same review identifies adjacent work before the guard is complete: join surviving descendants even when the leader exits normally or exits after TERM; check a 2x incremental compile, retention and log forecast above the disk floor; bound recipe and compiler-output reads before allocation. Add deterministic controls for these cases. Qualification of the existing six freshness tests and one real omission remains useful within its scope, but cannot establish those missing lifecycle and resource guarantees. Join only the qualified workflow delta onto current main; its historical host parent must not introduce unqualified production changes.


### October 3 compiler guard integrated and bounded host scope qualified

The [guard qualification](evidence/cargo-control-e68-qualification-20261003.json) verifies eleven harness roots and one actual freshly compiled omission, including the exact source assertion site and SDK error. The [isolated integration](evidence/cargo-control-e68-main-integration-20261003.json) places three byte-exact workflow files on main at62aaf598, retaining every other source entry. Bounded regular reads, streamed log limits, descendant cleanup and 2x incremental disk forecasts now accompany source/artifact freshness checks. Host production from the guard's parent is excluded from this integration.

The [bounded host qualification](evidence/runtime-hosts9cf-scoped-qualification-20261003.json) verifies39 Rust roots, including the twelve new roots, five Go roots normally and under race detection, five freshly compiled Rust omission groups with six intended assertion failures, two Go omissions in both modes and three actual Go-to-Rust typed reports. A dynamic error assertion may print the unexpected SDK error rather than its expected phrase: bind the exact source assertion site and actual payload instead of accepting a generic failing exit. Synthetic host support still does not admit actual runtime fee authority or finality.

Current-main host integration remains a separate gate. Four initial Go-run fixture failures rejected the temporary test image at protected-executable admission; the same seven roots pass from an owned single-link pinned image. Preserve the original result and qualify normal/race continuations from the immutable executable, with mode and ancestor custody recorded. Do not relax production executable admission to accommodate a compiler-generated fixture.


### October 3 current runtime-host integration completed

The [joined qualification](evidence/current-hosts-c59-joined-qualification-20261003.json) verifies seven selected roots in each normal and race mode, vet, and three exact Go-to-Rust executions. The [source join](evidence/current-hosts-c59-source-join-review-20261003.json) retains qualified Rust component results through nine byte-exact files. The [actual merge](evidence/current-hosts-c59-main-merge-20261003.json) records main0a9aeb34 and preserves the three compiler-guard workflow files.

Preserve the original temporary-image failures separately from the successful owned-image runs. The original failed executable-admission predicate was not captured; do not infer that its link count alone caused the rejection. Capture mode, links, ownership, ancestors and executable digest before running a protected test image. A corrected harness result does not change production admission policy.

This integration implements bounded historical host operations and separates job-read failures from evidence contradictions. It does not approve the actual mainnet runtime, establish a complete execution witness or authorize native fee accounting. Complete collector qualification, runtime/profile/finality review, economic conservation, cross-root recovery and actual host adoption remain required.

Operational headroom: the guarded reclaim29 plan found no eligible inactive archives older than12hours and removed nothing. Preserve this failed admission; select other disposable build data or derive a measured incremental forecast before admitting larger work. The full512 fixture contains encoded checkpoints, not an established512MiB payload; size forecasts must use measured or bounded actual data.


### October 3 EVM archival qualification completed

The [independent EVM archive review](evidence/evm-history8d-qualification-review-20261003.json) binds66 artifacts,27 normal roots from disjoint19+8 runs, the same27 under race detection, vet and five operative omissions in both modes. Controls require exact retained archive custody, fee-prefix accounting, archive-before-compaction publication, original legacy review provenance and retained observation census. A generic failing exit is insufficient: preserve the selected root and intended source assertion.

Qualified source8d9904a5 retains the original checkpoint before compaction and preserves credit, carry, payments and EVM cost history. Its current-main source join remains separate; this component result does not establish actual native fee authority, Claim/native conservation, deployment or observed settlement. Complete the Claim archive adapter and cross-domain economic rehearsal before accepting the continuous monitor as production-ready.

The [guarded cache reclaim30](evidence/cache-reclaim-30-20261003.json) recovered11.07GiB from1,139 inactive compiler archives in the superseded durable-volume cache. Exact-path reference scanning, active-process checks and physical identity checks passed; source, modules, evidence and ELF executables remain retained. Recheck each workload’s current2x incremental forecast before admission; this free-space snapshot is not a lasting capacity guarantee.


### October 3 retained Go qualification image safeguard

The [test-image review](evidence/go-test-image-173f-review-20261003.json) verifies six actual-file stdlib controls and three byte-exact workflow files integrated at6f6169c6. Capture the original image’s mode, links, ownership, ancestors and hash, then retain its exact pinned bytes in an owned mode0500 single-link image. Verify physical custody and hash before and after execution, retain bounded logs, join descendant processes and remove inherited GOFLAGS that would retarget fixture subprocesses.

This workflow addresses protected executable fixture admission without weakening production executable checks. Its receipt means pinned execution only: selected application roots, compiler/source/module provenance and actual outcomes require their own evidence. Do not rerun previously qualified application scopes merely to introduce this helper, or infer an original failed admission predicate that was not observed.


### October 3 archive continuation source review and observation causes

The [native/EVM integration source review](evidence/native-evm-current292-source-review-20261003.json) verifies22 changed physical files at29283743:21 match the independently qualified archive component and one joins the retained historical verifier dispatch. Current bounded-host and Cargo guard bytes are retained. The later Go workflow additions require their own exact preservation at final integration. Source review alone does not establish composed behavior.

An archive observation failure must retain its original cause before checking returned presence, digest or bytes. Read timeout or cancellation does not establish owner disappearance or contradictory evidence. Review native/EVM archive, catalog and Claim branches for combined error-and-comparison conditions; fix confirmed misclassification in a separate successor with deterministic interrupted-read controls. Preserve frozen component outcomes and durable original/next checkpoints while qualifying the affected continuation path.


### October 3 complete-proof collector cancellation finding

The [original collector result](evidence/proof-capture7b81-original-partial-review-20261003.json) retains seven passing Rust roots and one failing cancellation root at7b81da23. A cancellation after a real parent-trie read was translated by the SDK into `Invalid state root`; the caller returned that wrapper before checking its retained accessor cause. No complete collector qualification is claimed.

After each fallible backend code/heap, execution, write-root and strict-replay phase, inspect the retained accessor outcome before treating a wrapper as contradictory chain evidence. Preserve a concrete integrity error already observed before a later cancellation. Add deterministic before-read, after-read and adjacent phase controls in a distinct successor; retain original failed results and immutable input/output bounds. A failed observation must not become a false permanent rejection of authenticated progress.


### October 3 current native/EVM archive integration gate

The [current source review](evidence/native-evm-current0f-source-review-20261003.json) verifies39 artifacts and22 actual changed files at0f888694. The qualified archive components are joined with current bounded-host code and both executable qualification guards; all current module inputs are retained. The author’s full20,544-file inventory is bound separately from root’s changed-file verification.

Compilation and dependency preflight passed; the eight public archive/catalog, pending/admission and historical/capacity dispatch roots still require independent normal/race qualification and vet. Do not merge incomplete behavior evidence into launch acceptance. The distinct observation-error successor must preserve this frozen scope and its eventual results.


### October 3 cross-root restore core qualification

The [cross-root core review](evidence/native-cohort8fe-core16-review-20261003.json) verifies16 roots in each normal and race mode, with core/public vet successful, on frozen SN8fe01722/Connect1610f7a5. The checks cover exact per-mount and total declaration limits, refusal before the first mutation, healthy untouched owners, completed/pending peers, retained membership, sibling-root staged inode custody and aggregate capacity. Public10 and causal controls remain separate; no full public restore or actual host rehearsal is inferred.

A restore cohort must authenticate every original root and combined capacity before its first effect, then journal exact progress so interrupted or lost-ack continuation preserves completed peers. Keep root count, owner count, serialized declaration bytes, descriptors, retained segment bytes and disk/inode forecasts as distinct limits. Current accepted root ceilings remain64 per mount and256 total; explicit many-owner capacity is not permission to discard untouched roots or weaken identity checks.


### October 3 full512 recovery and collector Go scopes verified

The [full512 recovery review](evidence/native-ceae-full512-review-20261003.json) verifies the same full-history public restore root normally and under race detection, three adjacent negative roots per mode and vet. The primary test takes310.58seconds normally and731.17seconds under race detection. Positive reads of every retained inode precede the unchanged public sample assertion; the original d3d timeout remains failed historical evidence. Preserve the original progress and distinguish observation liveness from permanent worker failure. Cross-root public/causal qualification and actual host rehearsal still remain.

The [collector Go scope](evidence/proof-capture7b81-go10-review-20261003.json) verifies ten roots in each normal/race mode and both owned, pinned, single-link executable images. This preserves public request/job identity, bounded cancellation and missing-versus-zero observations. It does not erase the original Rust7PASS/1FAIL cancellation defect or qualify the complete collector, exporter, actual runtime profile/finality or native fee authority. Carry unchanged Go results into the distinct Rust fix only through exact source/module joins.


### October 3 source-custody admission during cross-root restore

The [original public cohort result](evidence/native-cohort8fe-public9-original-review-20261003.json) retains eight passing roots and one genuine late-source-custody failure. After valid planning, removal of an inventoried copied source-archive lock was missed by public apply because only its staged counterpart was admitted. The failing test stops before its subsequent no-mutation assertions; those assertions are not claimed as verified.

Check the required original source archive and owner attributes before the first effect, alongside staged inventory admission, for both single-root and cohort restore. Preserve completed target journals and permit exact interrupted resume; do not restart completed peers. Distinguish source custody from intentionally regenerated target inode identity. Qualify missing/replaced late members, pending/lost-ack continuation and unaffected peers in a distinct successor while preserving original failed evidence.


### October 3 collector cancellation successor source review

The [collector fix review](evidence/proof-capture550-source-review-20261003.json) verifies exactly two Rust files over7b81 at5501f9ac; Go and Cargo inputs are unchanged. Inspect retained accessor failure before SDK translation, preserve a returned node contradiction ahead of cancellation, and check completed code/heap/execution/root/replay/report boundaries with instance-local deterministic controls. Prior Go10 results retain their actual execution context and need no repeat without a concrete changed dependency.

Independent Rust positives, compiled omission controls, export and actual Go-to-Rust capture are separate gates. No complete runtime authority or finality admission follows from this source review, even when synthetic parent replay succeeds.


### October 3 qualified native/EVM archive continuation integrated

The [joined-source qualification](evidence/native-evm-current0f-qualification-20261003.json) verifies eight public roots normally and under race detection, vet and current dependency inputs. The initial four-pass/four-failure runner attempt used module-root cwd and could not find package-relative metadata fixtures; the exact unchanged source and owned binary passed from the correct package cwd. Preserve both outcomes and record execution context explicitly.

The [actual merge](evidence/native-evm-current0f-main-merge-20261003.json) integrates22 exact candidate files at1a7a123d; every non-documentation Git entry matches the qualified candidate and newer plans/evidence survive. Main now includes native/EVM immutable archive rollover, reviewed signed capacity catalogs and retained checkpoint continuation. Observation-error refinements, Claim archive lineage, source-custody restore, actual runtime fee/finality authority, cross-domain conservation and production rehearsal remain distinct open work.


### October 3 actual runtime472 structural review

The [retained runtime structure review](evidence/runtime472-structure-review-20261003.json) independently verifies compressed code and metadata against the saved snapshot, expanded Wasm bytes and five original function-body hashes. There are51 imports total, including50 functions and one memory; function-body indices must count only function imports. Observed fee-handler indices3053/3058 differ from runtime470’s3052/3057. Names and indices are source-review leads, not admitted economic semantics.

Bind a callsite profile to the exact original parent code and metadata; never carry an old runtime’s indices forward merely because function names resemble one another. Source/build correspondence, original branch/return evidence, complete execution witness and independent finality remain required. A missing refund event remains unknown; the failed-deposit branch cannot be inferred to return zero merely from absent events. Reuse existing receipt/finality verifiers and retained code snapshots while keeping their authority scopes explicit.

The owned archive’s configuration supplies a location lead, not live custody: Snow’s staged mainnet service uses `/data/subtensor-mainnet-archive-v1` and loopback RPC19945. Verify actual startup, sync and backend/export access before borrowing a parent trie. Ordinary state read proofs and a runtime-code snapshot do not constitute a complete parent execution witness.


### October 3 complete execution-proof collector component qualified

The [collector qualification review](evidence/proof-capture550-qualification-20261003.json) verifies192 artifact bindings: twelve Rust roots, four freshly compiled omission controls with one intended assertion failure each, and actual owned Go-to-Rust capture over complete synthetic parent nodes for pair, missing and zero-refund cases. Exported reports match exactly as typed JSON. Missing refund remains null, explicit zero remains0, and all cases retain unapproved runtime/native-fee authority. Original7b81 failure remains historical evidence; unchanged Go10 normal/race scope is retained through the exact source/module join.

Use immutable executable images and the qualified compiler freshness guard before target reuse. A Cargo `fresh:false` artifact means a new compile, not stale reuse. Bind the actual selected failing root and expected assertion rather than accepting a generic exit101. Public RPC request971/current-main composition and actual archive-parent, runtime/profile/source and independently admitted finality remain separate required gates.


### October 3 archive observation failures separated from conflicts

The [independent qualification](evidence/native-evm-read-causes9d-qualification-20261003.json) verifies seven deterministic roots normally and with race detection, vet, and two operative old-branch overlays. The overlays produce six and four intended behavioral failures per mode while unaffected controls pass. All seven changed paths are included in the [actual main merge](evidence/native-evm-read-causes9d-main-merge-20261003.json); every non-documentation Git entry matches the qualified candidate.

Preserve read failures as their original wrapped causes before considering absence, hash mismatch, changed bytes or reconstructed-plan differences. A read that returns no observation cannot establish an integrity contradiction. Exact same-plan continuation retains published archive/catalog work, while confirmed replacement remains rejected. This correction covers native and EVM archive/catalog paths and public reviewed-input reads; controller retry policy and final all-role composition remain separate work.

A read-only SSH attempt to Snow was rejected with public-key authentication before any remote command ran. That establishes missing access from this session, not an absent archive. Obtain a consistent separately owned parent-trie export or an in-node read-only backend route; do not open the running ParityDB with its exclusive-lock read-only API. Actual runtime/source correspondence, complete parent witness and independently admitted finality remain required before native fee accounting is approved.


### October 3 complete-history restore and evidence retention

The [original cohort terminal review](evidence/native-cohort8fe-original-terminal-review-20261003.json) verifies the exact 512-segment/two-head public restore root normally and with race detection. The separate nine-root batch still fails its late-source-custody root in both modes; the source-custody successor must pass before broad restore qualification. An initial misspelled race selector ran zero tests despite exit0. Require the declared root census, not process success alone, and retain the invalid run alongside its exact-selector correction.

The [compiler-image retention supplement](evidence/duplicate-compiler-image-retention-20261003.json) records524,730,368 allocated bytes reclaimed from six inactive compiler outputs after exact hash, physical identity and privileged process checks. Each exact executable remains at its original protected owned path; all source, logs and original receipt bytes remain. Bound duplicate paths can be retired only with an explicit byte-preserving mapping. Failed or insufficient cache-reclaim plans do not justify lowering the resource floor or deleting unique evidence.

The same retention supplement records a further74,248,192 bytes from the completed historical-request normal compiler duplicate, for598,978,560 total allocated bytes reclaimed. Future internally owned compilation can use a unique private output, then verify its identity/hash and set mode0500 before execution, rather than retaining a second identical ELF. Never reuse that output path for a subsequent compile. External or untrusted executable inputs still require the existing pinned-copy custody checks. Race and causal request qualification remain required; this operational change does not count them as passed.

Four further inactive0f/7b81 compiler duplicates were retired after the same independent hash, physical-identity and privileged process audit, reclaiming348,274,688 allocated bytes. All four qualified owned images and both original qualification documents remain unchanged. Total duplicate-only reclaim is947,253,248 bytes across eleven images; no source, active cache or unique evidence was deleted.


### October 3 workload-specific admission and historical request qualification

The [request qualification](evidence/capture-request971-final-review-20261003.json) verifies ten positive roots normally and under race detection, vet, and six intended behavioral failures from three operative controls in both modes. The actual failure sites113/209/219 establish the omitted parent-runtime, complete-body and closing-canonical checks; descriptive labels are not substituted for observed assertions. The [current-source union](evidence/capture-currentad0-source-review-20261003.json) preserves eighteen exact collector/request files and every other current-main entry. Its eight-root composed qualification remains required before merge.

The resource audit traced110GiB to an earlier serial OCI release preparation reserve, subsequently reused for unrelated Go tests without a new workload derivation. Preserve original policies and receipts, but size each admitted class from explicit production/emergency reserves, outstanding concurrent reservations and twice its incremental forecast. Separate already allocated retained inputs, cumulative growth and transient peaks. Warm Go, cold Go, Cargo, OCI, databases and full-history restores require distinct profiles and one host admission ledger.

A finite reviewed four-GiB grant admitted only the three staged request race controls, serially, from one pinned baseline. All attempts joined; timestamped0.2-second byte/inode samples observed749,080,576 bytes of cumulative consumption, below the cap. Sampling demonstrates an observed low-water, not an instantaneous maximum. Neither cleanup nor new free space recharges that grant, and it does not authorize other workloads. Crossing an old admission floor during an admitted finite test blocks inappropriate new work rather than canceling completed or active work. Other scopes retain their policies until separately reviewed.


### October 3 finite integration-test admission

The [mixed qualification admission review](evidence/mixed-qualification-resource-adoption-20261003.json) binds52 input artifacts, the independent V2 review and root adoption. The finite24GiB grant covers only34 enumerated serial archive, graph, compile, selected-test and vet phases: current collector integration, copied-source custody restore, a synthetic fee-context exporter and EVM restore compilation. Conservative retained compiler/cache growth is512MiB per compile; twice the aggregate forecast is21.5GiB, rounded24GiB. The original16GiB proposal remains historical evidence.

Keep one fixed baseline, one host lease and persistent byte/inode samples. A completed phase or cleanup does not refill attempts or budget. Full512 normal execution must measure its declared workset before race admission. Stop new admissions on a forecast overrun and retain/join admitted children; crossing an unrelated old floor alone must not cancel them. This grant excludes Rust, databases, full-model runs, live recovery and deployment, and does not change production reserves or global policy. Adoption establishes test resources, not passing behavior or final mainnet readiness.


### October 3 Claim archive source and foreground-work lesson

The [Claim archive source review](evidence/claim-archivee594-source-review-20261003.json) independently verifies26 bindings, nine exact changed Git blobs and unchanged module inputs at e5947c6f. Eleven new tests and seven neighbors are declared, with five operative controls; no compilation or behavior result is claimed. The candidate retains original checkpoint evidence, unresolved obligations and independently renewed policy history under signed archive capacity. Whole-Claim restore and continued renewal beyond finite epoch/review bounds remain required.

A source review found that `externalizeMonitorClaimRecord` compares every current epoch with every archived epoch during each save. Avoid work proportional to the product of those censuses: use an admitted commitment lookup while preserving ordered census checks and exact original evidence hashes. Keep the frozen candidate and qualify a separate successor with deterministic growing-census work counts. Measure hydration, compaction, state validation and replay/upload contention too; avoiding payload rereads alone does not establish bounded foreground work. This is a confirmed source finding, not a measured production stall.


### October 3 synthetic fee-context inputs verified

The [synthetic exporter review](evidence/synthetic-fee-exporter-review-20261003.json) independently verifies24 artifact bindings, the exact one-root pass, three successful graph/compile/test phases and two complete0600 fixtures totaling171,137bytes. Server5f uses its actual selected dependency graph and declared SN866 peer; unused peer packages gain no qualification. The immutable ledger snapshot matches the original output manifest. The public fee-context verifier can now qualify against these exact inputs; actual runtime/source, complete parent execution evidence and finality admission remain required.

The exporter omitted paired before/after executable custody during its test. Compiler post-mode identity, test input hash and later physical readback match, but they do not prove a stronger execution-boundary claim. Preserve that evidence limit, reuse the generated inputs through the verifier's own checks and record complete paired custody for subsequent tests. Do not repeat useful work merely to improve receipt wording or silently rewrite original evidence.


### October 3 current collector integration and scoped graph recovery

The [current collector qualification](evidence/capture-currentad0-qualification-20261003.json) verifies56 bindings, eight passing roots in each normal/race mode, vet and matched paired executable custody. The [actual main merge](evidence/capture-currentad0-main-merge-20261003.json) integrates all eighteen exact qualified paths at9576811d; every non-plan/evidence Git entry matches the candidate. The earlier Rust550 and Go971 component results retain their exact scopes. Actual parent export, runtime/source/callsite admission, independent checkpoint/finality and native fee authority remain open.

The [current fee-context source join](evidence/fee-context-current1b3-source-review-20261003.json) verifies27 bindings and seven exact feature paths over the current collector composition, retaining module inputs and all other source entries. Qualify this combined candidate once rather than first testing an older graph and rebuilding merely to compose it. Its ten-root/four-control gate is pending; synthetic export does not approve actual fees.

The [restore graph preflight review](evidence/selected-graph63-retry-review-20261003.json) retains an ordinary runner failure: an eager all-module query followed an unused replacement and failed before selected dependency inspection. Scope graph admission to imported production/test packages and record their resolved modules and physical inputs. Keep the failed attempt charged and use one explicitly bounded metadata retry after the corrected reader; preserve all completed tests and the original fixed resource baseline. An unused metadata failure must not trigger a full restart or invalidate authenticated components.


### October 3 full-scope economic and repair implementation gaps

The [full hardening source review](evidence/full-hardening-source-gap-review-20261003.json) independently verifies31 immutable Git bindings and29 current product readbacks. Native denominator evidence still always leaves allocation and quantization dust unknown, and the native summary does not derive provider entitlement or owner recycling. This is missing consumer code as well as missing real-chain authority. Implement the bounded runtime-specific execution-witness arithmetic and UID/hotkey generation consumer, then compose native, vault and independent Claim conservation. Cover intra-block accrual/drain, zero-incentive fallback, registration takeover, parent-runtime changes, omitted evidence and rounding; incomplete or unapproved evidence remains unknown.

Validator repair stores and manager rereads also conflate some unavailable observations with changed state. Return the original I/O/cancellation cause before comparing returned values, preserving confirmed contradictions and completed repairs. Add deterministic no-new-effect and healthy-peer continuation controls. Existing Solidity reserve, credit, carry and exact-payment guards remain reusable; no new contract defect is established by this source review. Runtime/source/finality approval, installed contracts, funded hosts and live role activation remain separate requirements.

The corrected selected-import graph retry passed with132 core packages/one module and848 SN packages/131 modules;3,895,108 raw metadata bytes and all source controls were verified. The original broad query failure remains charged. A narrow preflight repair preserves completed work and does not authorize source or resource-policy drift.


### October 3 retained model preparation and sampler continuation

The [continuation adoption](evidence/model-continuation-adoption-20261003.json) preserves the completed source archive and the dependency preflight failure caused by an uncreated private temporary directory. Provision that directory and retry only the affected graph inspection; do not restart source capture or discard the failed attempt. One corrected graph retry and one full current Server5f model invocation are admitted at the same resource baseline. Admission is not a passing model result.

The distinct sampler removes the undefined cache reference and records unavailable observations, final CSV readback and an explicit terminal outcome. Four independent lightweight controls distinguish complete telemetry, memory/container observation gaps and existing-output refusal. Keep test execution and telemetry qualification separate: a sampler failure must not terminate admitted work or fabricate zero resource usage. Current emissions, Claim rollover/restore, composed economic conservation and actual mainnet authority remain required.


### Native allocation conservation and launch split conformance

The [native accounting source finding](evidence/native-split-conformance-source-finding-20261003.json) distinguishes two requirements: authenticating observed emissions and enforcing the approved 10% provider / 90% owner recycle policy. Conserving the total alone cannot establish that split. Expose actual deviations and unknown authority explicitly; compare cumulative amounts against exact references with independently reviewed rounding, collateral capture and Claim reconciliation. Test a conserving wrong split and partitioned continuation so a new observation page cannot reset rounding carry or make incorrect economics appear ready. This finding concerns unsealed implementation and remains unqualified.


### Requested and granted transfer capacity

The [original full-model subsidy failure](evidence/server-model-subsidy-original-failure-20261003.json) remains retained while the suite continues. Initial source triage found a consumption loop counting requested capacity although successful escrow can now grant fewer bytes. Confirm the cause before changing assertions. Consumers and fixtures must use the returned, signed grant for capacity and completed-byte accounting; test partial grants and repeated exhaustion with exact revenue conservation. Check adjacent loops and production callers, preserving legitimate balance/debt retention. No post-fix result is established yet.


The [source review of the subsidy correction](evidence/subsidy-grant804-source-review-20261003.json) verifies the exact test-only successor: returned grant consumption, checked settlement errors, original exhaustion/payment assertions and unchanged production/modules. Qualify the affected root and two declared neighbors after the ongoing full suite joins; retain unaffected results through exact source scope rather than restarting the entire suite. The original failure remains evidence, and this source review supplies no behavioral pass.


### Role isolation must include startup admission

The [startup source finding](evidence/monitor-startup-isolation-source-finding-20261003.json) shows validator/operator/provider/Claim owner-admission failures can stop the monitor before healthy workers start. Runtime isolation alone does not cover this path. Separate invalid shared configuration and network/declaration admission from role-local observation or custody errors. Start healthy domains, retry recoverable local admission and quarantine confirmed local integrity failures, preserving exclusive ownership and checkpoints. Exercise actual multi-role startup, cancellation and duplicate-writer controls before closing this requirement.


### Preserve debit lifecycle and meaningful writer guards in tests

The [current full-model failure batch](evidence/server-model-clock-and-debit-original-failures-20261003.json) retains two writer-guard failures and six participant payout failures. Source triage separates a new early refusal branch from the later checked endpoint lock, and terminal settlement from asynchronous journal application. Review actual control flow and keep missing/late-lock mutants effective. For asynchronous debit, assert retained pending consumption and unavailable spendable credit, then exercise the real replay-safe flusher before checking raw balance and released reservations. Do not sleep, ignore close/read errors or weaken payout conservation to make fixtures pass. Continue the full run and qualify one combined affected-scope correction afterward.


### Preserve current wire changes in recovery dependency composition

The [Connect integration preview](evidence/current-connect772-integration-preview-20261003.json) retains all fifteen qualified recovery files alongside newer upstream optional close-report identity fields. The [composed source review](evidence/current-connect-composed-source-review-20261003.json) now binds commit `2ea8d82ea5cbc8bdebdee05f5b90a9f21b64128f`: all preview paths remain exact except the two namespace replacements from `e7f24717`. This is a Git-only composition, not a published dependency or test result. Qualify the final common dependency graph, including namespace controls and optional identity wire compatibility, once; then publish and pin it in SN. Optional report identity must remain non-emitting until the backend provides durable deduplication of the exact party, amount and checkpoint; a compatible wire field alone cannot authorize that behavior.


### Continuous native evidence requires an admitted producer

The [native admission gap](evidence/native-continuous-admission-source-gap-20261003.json) separates a signed per-block evidence consumer from an operational producer. Reuse the reviewed runtime/layout/engine authority, but automatically collect and independently verify fresh finalized boundaries, jobs and provider generations under explicit bounded producer authority. Owners must not manually sign every block, and an artifact signed by its own unverified collector is not independent proof. Qualify the actual producer→Rust→public consumer path, restart continuity and incomplete/canonical-change controls. Actual network and runtime approval remain required.

The [combined fixture source review](evidence/combined-model4754-source-review-20261003.json) supersedes the804-only testing plan with ten affected roots and six neighbors. Pending debt, public debit/replay and unchanged provider/account allocations are asserted before the original exhaustion checks. Source review establishes no behavioral pass; the unchanged full suite continues before correction tests are admitted. The [correction proposal review](evidence/model4754-proposal-source-review-20261003.json) verifies all four overlay files, sixteen selected test declarations and the resource arithmetic. The newly added helper file must be discovered by the actual Go overlay graph and compiled before the affected tests run. Retain original configuration-dependent skips; qualify referral and onboarding behavior with explicitly pinned configuration in a separate follow-up instead of treating skipped functional coverage as passed.


The [frozen native source review](evidence/native-ad1-source-review-20261003.json) verifies all sixteen changed files and twenty-four intake bindings at `ad1e5b19`. Qualification must exercise the actual Rust-exported original-Wasm job through the owned Go replay process and accounting consumer; its environment-dependent skip is not a pass. Normal/race, Rust controls and the continuous independent producer remain outstanding. Missing admission input must preserve the cursor and unknown amounts while healthy domains continue; confirmed identity conflicts retain their integrity handling.


The [closed-observation successor review](evidence/repair-closed6d-source-review-20261003.json) verifies `6d0612b8`, four changed files and eleven intake bindings. A closed handle or `EBADF` now remains unavailable evidence at stopped-generation and active pre/post-join checks; it cannot establish generation change or replenish the original join window. Positive integrity loss and uncertain durable publication keep precedence. Four new deterministic public controls join the existing twenty-root repair scope; qualification and current-role composition are pending.


The [common hardening source preview](evidence/hardening-composition-source-preview-20261003.json) joins Claim/fee/restore, native execution, repair closed-observation and lineage controls at `b704fcde`, retaining the exact Core `2ea8d82e` pin. Integration exposed overlapping replay-fixture dispatch changes; Astra resolved them at `5029c6c2` by retaining the fee marker and both native/other-profile paths. Qualify all retained scopes on this common graph instead of rebuilding separate precursors. This is a Git-only preview: final intake/census, compilation, normal/race and actual cross-engine qualification remain pending; no publication or activation is implied.


The [StatsProvider original failure](evidence/server-stats-original-failure-20261003.json) is the tenth current full-model failure and the seventh shared-balance assertion affected by asynchronous journal application. Its complete excerpt remains separate from the earlier six-test bundle. It was already included in the combined4754 ten-affected/six-neighbor scope; retain that batch and require actual public debit flushing/replay before raw-balance assertions. No correction pass is established yet.


The [final Claim source review](evidence/claim-final-source-review-20261003.json) verifies fifty-two changed Git bindings, fifty-one physical intake bindings and sixty-five exact test declarations (forty-four primary plus twenty-one compatibility roots). The common source preserves all reviewed Claim files except the explicitly resolved replay-test fixture. Keep the fourteen operative control groups and the native/repair/lineage scopes in the combined qualification. This source remains uncompiled and unqualified; official dependency packaging, behavioral execution, actual upload contention and production authority are still required.


The [startup/foreground source review](evidence/startup-foreground-source-review-20261003.json) binds independently reviewed six-role startup isolation and four actual replay/upload/foreground controls on the common preview. Startup emits bounded pending/quarantined/re-admitted events even when metrics cannot open, without claiming fresh checkpoint or metrics evidence. Cleanup errors must remain typed through nested archive load/open failure paths; a clean outer close cannot turn an unjoined nested owner into retry permission. That adjacent successor and all behavioral qualification remain pending. Performance observations from synthetic signed histories prove only their fixture scope, not production host or fleet sizing.


The [five additional original financial failures](evidence/server-additional-financial-original-failures-20261003.json) extend the live model failure set beyond the ten-root4754 correction. Retention must keep unapplied debt and suppress premature tombstones. Force-close tests must retain terminal/escrow/provider assertions while distinguishing committed journal consumption from its later raw-balance application. Exhaustion fixtures must report the returned grant, check close and forced-close errors, and prove public available credit before flushing; a malformed over-grant close cannot be counted as paid usage. Keep the frozen4754 source and original failures separate from the expanded correction and its future qualification.

The [current migration-plan skip](evidence/server-migration-plan-original-skip-20261003.json) confirms that PostgreSQL `auto_explain` availability or LOAD permission prevented the actual nested-plan test from running. The shared skip message does not identify which prerequisite failed. After the current full suite terminates and its fixture is cleaned up, rerun this exact test with the extension and permission provided in an owned disposable fixture. Require actual custom and generic INSERT/UPDATE/DELETE plans across the migration on an existing connection; retain the original skip and completed model results.

The [nested cleanup source review](evidence/nested-cleanup-source-review-20261003.json) retains cleanup uncertainty through native, EVM and Claim archive admission and all seven role recovery callbacks. Parent cancellation must not erase a real close failure or authorize another owner. Ordinary read failures remain retryable after the exact reader closes. Seven new deterministic roots and nine neighbors cover actual descriptor release, lost-ack Claim recovery, healthy peer continuity and cancellation precedence; qualification remains pending on the common source.

The [common qualification scope review](evidence/common-qualification-scope-review-20261003.json) binds the combined cleanup successor and Core dependency to 205 distinct Go test declarations, with complete matching normal and race selectors. The proposed 32 GiB data allowance covers a 2× margin on the 14 GiB planning forecast; it is not a measured production bound. Retain separate Rust/export and operative-control qualification, original model cleanup and fresh resource admission before execution. Monitoring completion also requires the continuous independently verified native producer and actual launch authority.

The [additional request-lease and retention failures](evidence/server-lease-retention-original-failures-20261003.json) require explicit separation of terminal settlement, pending debit, applied balance and token retirement. Preserve the original request expiry and safe retention guards. Qualify pre-flush accounting and available credit, public flush and repeated replay before declaring these fixture assumptions corrected; the original failures remain retained.

The [producer and packaging prerequisite review](evidence/producer-packaging-source-prerequisites-20261003.json) preserves two continuation invariants. A native producer must pair advanced GRANDPA authority state with the actual certified header, separately from earlier parent/child storage-proof blocks; qualify delayed handoff and descendant restart together. Final dependency packaging must retain the exact current and historical floor commits used by its official module verifier, with owned Git objects and measured source bytes. Neither source review establishes runtime approval or completed qualification.

The [current Server full-model terminal result](evidence/server-full-model-terminal-20261003.json) retains 1,507 passes, seventeen failures and eleven skips. The suite completed before its timeout with no unfinished roots; its owned database, Redis and network were removed successfully. Retain these results and qualify the expanded correction plus exact skipped-input follow-ups. The four extra names in the declaration forecast are non-test `Testing_` helpers; all 1,535 actual test roots have terminal outcomes. Future source forecasts must recognize test naming and signatures, rather than treating every `Test` prefix as a runnable test.

The [expanded seventeen-failure correction review](evidence/server-retention-c2b-source-review-20261003.json) binds nine test-only changes, 45 model roots and one actual debit-worker continuation root. It preserves pre-flush available credit, original journal identity across metadata removal, returned-grant accounting, provider allocation and exact post-acknowledgment token release. Three operative omission controls must fail for the intended behavioral reasons. Qualification remains pending; the completed original model suite remains retained.

The [four Claim control rebase review](evidence/claim-b6-control-source-review-20261003.json) preserves the current typed cleanup fix while carrying only the intended omission effects. Test-control overlays must match the composed production source; replacing an entire older file can silently remove an adjacent fix. Qualification coordination must also use one canonical shared host lock, rather than separate per-job locks that permit simultaneous holders. Retain completed sources and evidence when correcting either preparation issue.

The [combined physical source stage](evidence/common-source-stage-readback-20261003.json) passes for the exact SN and Core commits with seven bound peers and the historical module floor. Preserve its receipt and charged disk usage while later packaging, graph and test phases continue under the same baseline. Successful source staging establishes provenance, not behavioral or launch acceptance.

The [official dependency package readback](evidence/common-module-package-readback-20261003.json) passes the exact Core ZIP and module hashes independently. Preserve the source stage, package and original fixed resource baseline across later qualification. This proves reproducible dependency packaging; it does not prove combined behavior or authorize deployment. Selected graph checks now proceed separately, retaining either outcome without discarding the other.

The [selected dependency graph readback](evidence/common-selected-graphs-readback-20261003.json) passes all six selected SN/Core package imports against the exact private dependency. Additional Git Origin metadata in the module cache is recorded separately from exact ZIP/MOD bytes and matching version/time; provenance checks must compare the relevant semantics rather than reject this expected cache enrichment. Compilation and behavioral qualification remain pending.

Unattended native accounting also needs an automatic historical trie source. A supplied content-addressed directory alone does not meet continuous-operation requirements. Implement a bounded archive read-through or an equivalent production feed: request proofs at the certified parent block, retain request identity and the original retry budget, and let the owned original runtime verify the accessed nodes and resulting state root. RPC-returned values are not accounting authority. Missing nodes must leave the cursor and amounts pending while healthy domains continue. Unsupported child-trie or range operations must remain explicitly unavailable until proof completeness is implemented. Qualify actual missing-node replenishment, restart and cumulative resource limits; this work remains pending.

The [populated native capacity source review](evidence/native-populated-capacity-source-review-20261003.json) replaces empty-trie sizing assumptions with four families of 4,096 present provider records, both pinned SDK layouts, and a large separately hashed value. Qualification must measure actual raw nodes and decoded/serialized bytes with a 2× margin, reject real missing-node proofs, and exercise the native admission path. This synthetic fixture is unexecuted and grants no actual runtime or provider authority; it does not bound the complete VM state footprint. Apply capacity profiles across the complete path: native storage verification, Rust historical capture, retained/combined proof limits and Go replay decoding. Enlarging only the storage verifier leaves an earlier VM proof cap unchanged. Preserve the independently bounded receipt profile; qualify native execution with the enlarged profile and actual populated proofs.

The [proof-feed operation review](evidence/native-proof-feed-operation-review-20261003.json) binds the pinned SDK surface beyond point reads: child storage, iterator progression and root computation also need authenticated trie access. Key-discovery RPC pagination is insufficient to prove complete ranges or end-of-range. Test unavailable sibling nodes during state-root updates, wrong-parent proofs, interrupted replenishment and restart through the actual execution path. This design review does not establish implemented or qualified read-through behavior.

The pinned SDK root calculation can log an error and return an old or default root; its ephemeral lookup can turn a storage error into missing data. The proof-feed owner must retain an independent error latch and check it before accepting execution output or advancing the accounting cursor. Qualify transient storage failures during parent and child root updates, including unchanged-root cases. Logging alone cannot establish successful execution. This newly identified gap is assigned to the native producer implementation and remains unqualified.

The [six ordinary package compiles](evidence/common-six-normal-compiles-readback-20261003.json) pass on the frozen combined source. Behavioral tests may use those exact retained images while independent race compilation continues under a combined resource forecast and the original baseline. Do not impose an all-compilers barrier on independent completed packages. Compilation alone does not close behavior, capacity or deployment requirements.

The [producer integration findings](evidence/native-producer-integration-findings-20261003.json) require separately approved capture and replay executable identities: the real Rust programs accept different fixed protocols, although a synthetic peer can hide that mismatch. Qualify actual capture, retained-job restart and replay with the real binaries, including wrong-mode and changed-image controls. Independently approved monotonic authority/capacity renewal must retain the original cursor, completions and predecessor approvals; a new runtime or larger allowance must not force a fresh accounting history. Both fixes remain pending in the producer lane.

The [complete twelve-image compilation readback](evidence/common-twelve-compiles-readback-20261003.json) passes all six packages in ordinary and race modes on the combined frozen source. Retain these exact images for selected behavioral checks. Compilation does not qualify runtime assertions, real cross-engine execution, omission controls or the newer producer implementation.

The [original combined normal behavioral result](evidence/common-normal-ordinary-original-failures-20261003.json) retains 137 passes and 66 failures across 203 completed roots. Fifty-nine failures are package-relative fixture errors from the direct runner's module-root working directory; seven require separate behavioral analysis. Direct compiled tests must use the package directory, and preflight must validate fixture resolution before costly execution. Do not count a harness correction as a production fix, dismiss the separate failures, or repeat successful source/module/compile preparation.

The [checkpoint readback finding](evidence/durable-checkpoint-readback-identity-source-finding-20261003.json) identifies a production failure class: a canceled or failed read was compared with expected bytes and labeled an identity change. Failed observations must retain their cause and pending reconciliation; only successfully returned differing bytes establish a mismatch. Add actual publication-boundary cancellation and true-mismatch controls, then inspect adjacent preparation and inventory readers for the same conflation. This source finding remains unqualified and does not establish the cause of every original test failure.

The [corrected normal assertion scope](evidence/common-normal-corrected-assertions-20261003.json) retains 190 passes and thirteen failures, with all 203 selected roots terminal and no skips. Independent role quarantine requires waiting for the affected worker and proving peer continuity, rather than expecting the whole process to exit. Restore fixtures must preserve the untouched metrics root's prepared reserve, and foreground fixtures must advertise headers within the actual public metadata allowance. Correct these assumptions while retaining true authority, changed-byte and over-limit refusal controls; the production cancellation/readback defects require their own deterministic tests.

The [full512 normal qualification](evidence/full512-normal-retained-qualification-20261003.json) passes the actual public restore of all 512 original retained EVM segments. Its 331 sampler rows record a 132,313,088-byte allocated peak, thirteen ordinary child-path removals and complete final cleanup. This is sampled synthetic-fixture evidence, not a production sizing bound. Preserve body, measurement and executable custody separately from a post-body summary-path collision: regenerate the checker summary under a unique job namespace without rerunning successful work. Race and final composition remain pending.

The [frozen correction source review](evidence/b6-correction-source-review-20261003.json) binds fourteen changed files and 52 selected roots, with seven operative controls. Production corrections preserve checkpoint uncertainty and plan-file read causes; fixture corrections retain independent role continuity, original reserve authority and public header bounds. This batch is uncompiled and unexecuted. Qualification must retain the original failures and reuse unchanged source/module preparation; separate Core read-cause and continuous native-producer work remains open.

The [mixed cancellation finding](evidence/mixed-cancellation-source-finding-20261003.json) extends the error-classification lesson: a joined parent cancellation and owned deadline expiration is still pure cancellation. Require the actual owner cause, inspect every joined leaf, and preserve independent I/O, identity and cleanup failures. Add deterministic both-order controls before qualifying the correction successor; do not rerun or alter currently active qualification jobs.

The [mixed cancellation successor review](evidence/mixed-cancellation-successor-source-review-20261003.json) verifies the three-file child of the frozen correction batch. Both observation and checkpoint classifiers now accept joined cancellation/deadline leaves while requiring the actual owner cause and retaining hard-error dominance. The 52-root union and eight operative controls still need normal/race qualification; qualify this final child once rather than compiling the precursor again.

The [pending-file sync finding](evidence/native-pending-file-sync-source-finding-20261003.json) separates visible byte equality from durable publication. If the original file sync failed, retry must sync the exact retained pending file before rename; syncing only the directory does not repair file-data durability. Cover complete-write/file-sync failure, partial pending bytes and independent directory-sync failure with deterministic controls, and inspect adjacent node/evidence publishers. This moving-source finding is not yet qualified.

The [finite correction resource review](evidence/0e5-finite-resource-review-20261003.json) retains one fixed consumption baseline while allowing a reviewed 48-GiB aggregate scratch budget. It accounts for existing live reservations and a 2x margin for the 33-phase correction scope. An old accounting cap should not create an artificial serial bottleneck when actual host capacity safely admits overlap; exact executable jobs, current resource checks and owned process cleanup remain required. This review authorizes resource capacity, not mainnet activation or a behavioral pass.

The [full512 race readback](evidence/full512-race-independent-readback-20261003.json) now independently verifies the public restore of 512 retained EVM segments under race detection: one complete passing root, 719 error-free sampler rows, a sampled 127,963,136-byte allocated peak and empty final fixture. Normal and race results are both retained. This does not establish production capacity, the native producer or full composed launch readiness; do not repeat the successful scope merely to regenerate summaries.

The [Core preparation read-cause review](evidence/core-preparation-read-cause-source-review-20261003.json) verifies the adjacent error-first correction for retained staging plans and moved-root reservations. Failed reads preserve their cause and original journal; only complete observed contradictions establish differing custody. Four new deterministic roots and twelve neighbors cover interrupted real root publication, healthy exact replay and true missing/changed reservation. This three-file successor is uncompiled and unexecuted; qualification and later caller adoption remain separate from the current SN0e5/Core2ea tests.

The [correction source-stage readback](evidence/0e5-source-stage-independent-readback-20261003.json) independently verifies the final cancellation/recovery successor as a complete clean archive: 20,650 tracked entries and 224,960,146 bytes, exact commit/tree and unchanged module inputs. Retain this preparation for subsequent graph, compile and behavioral phases; source provenance alone grants no behavioral or launch acceptance.

The [finite correction continuation authority](evidence/0e5-finite-continuation-authority-20261003.json) removes per-phase coordination waits after review of the complete source, scope and resource budget. The test owner may finish the remaining 31 specified phases with previously reviewed runners adapted only for paths and selectors, preserving exact input checks, bounded execution and independent result readback. Retain completed preparation; report substantive runner changes, custody failures and new scope rather than silently weakening them. This local test authority does not authorize signing or deployment.

The [correction durablehead graph readback](evidence/0e5-durablehead-graph-independent-readback-20261003.json) verifies readonly resolution of the newly selected package under the exact reused Core2ea module. Raw graph/error output, unchanged source modules and physical GoMod files match the recorded evidence. Compilation and behavioral qualification remain separate; successful source and graph preparation is retained.

The [correction peer-staging finding](evidence/0e5-peer-staging-compile-finding-20261003.json) retains two successful durablehead compiles and four compile failures caused by omitted local replacement peers. A selected package graph does not prove the complete workspace layout. Restore the seven exact already-qualified peer links, preflight every local module replacement and retry only the failed compiles; preserve tracked source/modules and passing images. This preparation defect is separate from product behavior.

The [peer-link repair readback](evidence/0e5-peer-link-independent-readback-20261003.json) independently verifies all seven restored links against their exact existing target commits, clean tracked trees and module hashes. No full peer restaging or source revision occurred. Preserve both durablehead images and original missing-peer failures; the four affected compile retries and subsequent behavioral tests remain separate evidence.

The [corrected durablehead positive readback](evidence/0e5-durablehead-positive-independent-readback-20261003.json) verifies all six selected roots normally and under race detection. Failed acknowledgment reads retain uncertainty and their cause, actual changed bytes still establish conflict, and exact lost-ack recovery remains intact. Raw top-level outcomes and protected image custody are independently checked. Vet and the old-body regression control remain separate; this component pass does not imply the remaining mainnet/validator scope or live deployment.

The [corrected mainnet normal readback](evidence/0e5-mainnet-normal-independent-readback-20261003.json) now verifies all 37 selected roots passing, including the original affected cancellation, plan-input, Claim restore and fee-input cases. Each root ran once with no skip; protected executable identity and raw results match. Retain the original failures as historical evidence. Race, validator, vet and operative controls remain required before this correction batch is accepted.

The [current foreground failures](evidence/0e5-validator-foreground-failure-source-review-20261003.json) retain six passes and three failures in each nine-root normal and race scope. A remote object write inside a ledger Walk retains the single-reader admission needed by completed RunTrail disk-proof projection; Stats catchup shares that gate too. Release read ownership before remote callbacks using signature-checked point reads over the fixed captured prefix, then prove the actual blocked mid-stream path. Continuous-work fixtures must budget their seeded records plus new foreground records, while retaining real capacity refusal. Require immutable prefix isolation, bounded memory/snapshots and owned cancellation without partial publication. Mainnet and durablehead successes remain retained.

The [three-package vet readback](evidence/0e5-three-vet-independent-readback-20261004.json) verifies successful mainnet, validator and durablehead vet commands with unchanged modules and matching raw output. Behavioral failures and operative regression controls remain separate.

The [continuous native producer source review](evidence/native-continuous-producer-source-review-20261004.json) records the frozen capture/replay, certified-descendant and retained-completion implementation. Source review is not qualification: require actual Rust engines through the public restart path and populated provider capacity. Continuous production must renew signed authority, runtime and resource scope without resetting completed accounting, rewriting historical evidence or recapturing acknowledged jobs. The current exact-authority admission and 4,096-job ceiling still need that successor. Native/vault/Claim conservation and the 10/90 economic policy remain end-to-end requirements.

The [native capture heap review](evidence/native-capture-heap-parity-source-review-20261004.json) identifies a capture/replay context mismatch against the actual pinned SDK. Capture now executes the original block onchain, preserving the proved `:heappages` setting used by strict replay; a complete parent with an explicit heap override must not remain unavailable because capture selected different memory semantics. Three deterministic regressions cover the override, default dynamic growth and the unchanged memory ceiling. They are authored and unexecuted. Existing complete-proof refill qualification still needs child, iterator and root-sibling requests that are not satisfied by returning the entire fixture proof on the first RPC response.

The [incremental native refill review](evidence/native-capture-incremental-refill-source-review-20261004.json) adds six unexecuted Rust roots through the public directory/feed capture entry. Every refill proves the requested hash using the pinned SDK's exact top or child prefix, then retains only that node. The cases include unread root siblings, odd-nibble ranges, missing iterator/root nodes and request-bound acknowledgements. Independent compilation and execution remain required; the Go broker, process deadline and durable cursor still need their own complete qualification.

The [ade5 failed-root census](evidence/native-adoptionade5-failed-root-census-source-review-20261004.json) independently reproduces all 43 failed names and seven passes per mode from hash-bound raw output. Every original failure stops at the unprotected scratch ancestor, before a restore product guard. One same-image protected-environment smoke root passes per mode. Continue the exact failed roots beneath protected ancestry while preserving original failures and successful evidence; these setup failures do not justify weakening durable custody or changing restore production behavior.

The [first four normal regression controls](evidence/0e5-four-normal-operative-controls-independent-review-20261004.json) independently reproduce the intended failures with old acknowledgment-read, named-cancellation and plan-input bodies, and with Claim compaction omitted. Each compiled and reached its named assertion with no skip; the changed test suite detects those production defects. Twelve remaining normal/race controls and their causes remain to be reviewed. Preserve the narrower early-cancellation scope of the plan-input control; it does not alone prove every read/close path.

The [mainnet correction race readback](evidence/0e5-mainnet-race-independent-readback-20261004.json) verifies all 37 selected roots passing once without skips and with unchanged executable custody. Together with normal results and vets, this retains the unaffected correction scope while the isolated validator successor is tested.

The [foreground successor review](evidence/foreground-successor-source-review-20261004.json) binds the three-file fix and its deterministic controls. Read ownership ends before remote callbacks while ledger lifetime remains owned and cancellable. Tests separately target original-prefix isolation, full foreground proof projection, joined owner close and unchanged real capacity refusal. The 20-root successor qualification remains pending; source review does not establish a passing release.

The [RPC retry traversal review](evidence/rpc-retry-cause-traversal-finding-20261004.json) identifies an adjacent hardening task: bound error-tree traversal and require a real transient cause. Cyclic, over-budget and empty/all-nil joined shapes must not acquire retry admission; genuine timeouts still retry and an independent permanent cause still wins. This is a source finding rather than a live incident, and correction must preserve the frozen producer qualification work.

The [foreground successor normal readback](evidence/foreground-successor-normal-independent-readback-20261004.json) independently verifies all 20 roots passing once with no skips, including blocked HTTP, cancellation and continuous replay. Validator vet passes with unchanged modules. Original failures remain retained; race and the two intended old-body controls still determine full correction acceptance.

The [native recipient reconciliation finding](evidence/native-recipient-cross-domain-finding-20261004.json) requires retaining the individual amounts already authenticated by execution, instead of only recipient identities and aggregate sums. Bind those amounts to the exact original earning interval and reconcile capture, carry, entitlement and payment without duplicate attribution. A RootMissed carry may legitimately pay in a later epoch with no new capture; preserve the earlier source obligation. Equal aggregate totals do not excuse changed recipients or boundaries. Amount authentication remains distinct from complete 10/90 conformance and actual runtime authority.

The [foreground successor race readback](evidence/foreground-successor-race-independent-readback-20261004.json) verifies the same 20 roots passing once under race detection with no skips and unchanged protected executable custody. Both positive modes and vet now pass; exact old-body controls still determine causal acceptance before integration.

The [complete correction controls readback](evidence/0e5-sixteen-operative-controls-independent-review-20261004.json) verifies eight intended old-body failures in each normal/race mode: 16 clean compiles, exact named assertions and no skips or unrelated race report. Acknowledgment uncertainty, cancellation taxonomy, Claim custody/receipt retention/compaction and indexed work are causally exercised. The original0e5 positive result remains 49 passes/3 validator failures per mode; the separate foreground successor owns their correction.

The [native finite resource admission](evidence/native-finite-resource-admission-20261004.json) admits the complete 66-phase producer, SDK-prefix and populated-capacity qualification scope under the existing cumulative budget and 2x forecast margin. Preserve completed source/dependency work and avoid per-phase coordination pauses for reviewed runners; new orchestration mechanisms still receive one bounded review. This is test-resource admission, not production capacity or launch approval.

The [foreground correction control review](evidence/foreground-successor-controls-independent-review-20261004.json) completes focused qualification. Restoring the old production iterator reproduces the exact gate assertion in both modes; restoring the old test fixture capacity reproduces the expected ceiling failure in both modes. These are distinct defect types. Combined with 20 positive roots per mode and vet, the correction is ready for source integration; actual host sizing and the full mainnet release remain separate requirements.

The [delayed renewal review](evidence/delayed-native-renewal-source-finding-20261004.json) applies the stale-plan lesson to continuous native accounting. Ordinary authorized progress must not invalidate a reviewed successor merely because its signed predecessor is no longer the latest cursor. Prove that predecessor as retained authenticated ancestry, preserve intervening completions and pending scope, then recheck cumulative capacity before adoption at an untouched boundary. Unrelated or rewritten ancestry remains a refusal. Deterministic delayed-review and pending-completion tests are required.

The [final miner selected-scope readback](evidence/final-miner-selected-scope-root-readback-20261004.json) retains seventeen passing roots in each normal/race mode: sixteen unchanged successful bodies plus one corrected fixture executed on both final images. Reuse requires checking the complete imported package closure, not merely the caller file or executable name. The correction supplies the valid synthetic genesis required to reach the intended oversized-identity assertion; it does not weaken production chain authority. This selected scope does not establish the broader runtime, control or release gates.

Final Server preparation exposed four compile errors in three new test files: a recovered panic has type `any`, so fixtures must first prove it is an error before inspecting its cause. Preserve the production error and the original compiler diagnostics; correct every adjacent occurrence together, then continue unaffected package discovery. A batch should collect independently reachable compile failures instead of stopping all qualification at the first package. The narrow test-only successor is awaiting independent compilation and execution.

A signed repair controller must preserve independence before assigning scarce worker slots. Multiple approved envelopes for the same service must not occupy all workers behind a blocking service mutex and starve an unrelated incident. Both the outer grouping lock and late-resolved service lock need a nonblocking admission outcome, without creating an intent or consuming action allowance. The isolated successor has a deterministic barrier regression: the first service cannot finish until an unrelated fifth envelope runs. Normal/race execution and omission controls remain pending.

Qualification tooling must freeze executable paths, complete child environment, package working directory and private-file creation mode. Resolve the actual `test2json` tool through the selected Go toolchain, rather than guessing under GOROOT; identify live compilers by executable identity and argument boundaries, including absolute Go paths. Use UMask0077 for private test scratch: Go-created child directories otherwise inherit a permissive parent process mask and legitimately fail protected-path admission. Preserve successful test images and body results when repairing orchestration.

The [future history resource extension](evidence/future-history-qualification-resource-extension-20261004.json) retains the original consumption baseline and a 2x forecast margin. At 99% data-volume occupancy, a larger accounting cap cannot create physical space. New compiler scratch on the root volume is a [proposed temporary exception](evidence/temporary-root-qualification-scratch-proposal-20261004.json), pending the user's storage-direction approval; running histories and evidence stay on the data volume. Independent long race bodies may overlap only after a complete fresh disk/memory forecast; compiler and database overlap need their own combined allowance. Do not terminate healthy work to reclaim its files.

The [old compiler archive cleanup](evidence/old-compiler-intermediate-cleanup-readback-20261004.json) reclaims 728,561,584 logical bytes from 153 exact obsolete intermediate archives. Identity checks and a privileged scan of process file descriptors, executable paths, working directories, mappings and arguments found no live references. No complete directories, test images, caches, source or accounting evidence were removed. Keep cleanup scoped to verified disposable intermediates; a closed historical job alone is insufficient evidence that every file beneath its scratch root is disposable. This recovery is too small to satisfy the final image forecast.

Passive-root repair needs a distinct signed role profile and the original activation/bootstrap custody. A stopped service and preserved checkpoint do not permit a controller to invent a new generation: persist a cross-journal generation claim and the single start intent before the effect, then retain uncertainty rather than retrying a possibly successful start. A later incident requires its own signed approval and acknowledged predecessor. The additive adapter source is frozen for independent qualification; the operator repair profile still needs the actual WARP environment, database and signing-resource census. Neither adapter may borrow validator-role authority.

The [final monitoring publication readback](evidence/xops-final144-publication-root-readback-20261004.json) verifies 144 distinct successful tests, exact raw output hashes, actual exit zero and joined process tree against published xops main. The first successful body had lost its Unix status during receipt postprocessing; its raw successes remain retained separately. Persist the raw wait result before context validation, shape conversion or report checking so a checker failure supports checker-only recovery instead of repeating completed work. The shared harness successor has this behavior and deterministic checker-failure/custody controls; independent qualification remains pending. This monitoring result covers offline tooling, actual promtool and loopback Prometheus, not host activation or SN role behavior.

The [long Claim normal failure](evidence/full129-normal-claim-failure-root-readback-20261004.json) is retained as an actual failure: 129 original payload admissions and 2,184 aggregate work units per later transition exceed its original 2,048 ceiling. Added archive-custody instrumentation appears in the same counter, but splitting stages alone cannot establish the original total foreground bound. Quantify complete physical and hot work and assess redundant custody fences against the stated linear/bounded PH08 and fresh-custody PH01 requirements. Preserve the passing normal restore and its healthy race run; do not promote the failed Claim scope or disable owner-loss detection.

Final role qualification exposed a production frame integration defect: the whole-fee policy loader and active-validator repair passed their enlarged policy allowance to a generic reader that rejected every requested allowance above 128 KiB before any I/O. Even a tiny valid legacy policy was therefore rejected. Give policy ingestion its explicit bounded profile while preserving the generic reader's smaller guard and the parsed policy's per-role frame checks. Qualify both actual loading and repair admission, the enlarged profile, legacy inputs and true over-limit refusal. The coherent successor is frozen for qualification; source review alone does not close this issue.

The [future narrow compiler admission](evidence/future108-narrow-compiler-adoption-20261004.json) preserves the original baseline and both 2x forecasts while increasing only the standing-authorized accounting budget. Clamp the accounting floor at zero: a budget above the original free-space baseline must never subtract from the physical reserve. The new phase still requires 20 GiB physically free for one six-GiB compiler and one four-GiB history body, plus unchanged memory admission. This grant excludes database, Rust, short-body and second-history overlap and does not authorize final image retention or changing the storage location.

The [provider whole-work launch source handoff](evidence/provider-whole-work-launch-source-20261004.json) freezes SN `7da3e844` over `b4662ed8`, requiring Core `7de1d3e8` and SDK `9ae95704` in the final composition. Standalone and swarm provider roles now configure their actual SDK manager from an exact reviewed profile binding the independent request key, complete original domain, retained client identity and private outbox. Complete-mode CLI and bootstrap exports require that profile; missing configuration refuses startup, while legacy omission remains unknown. No request key is inferred from SQL, artifact signing or prior bootstrap approval. Restart preserves original cut bytes and enrolls a fresh manager generation.

The eight new miner and three new mainnet roots are authored but unexecuted. Public CLI refusal, public bootstrap export and actual shared DeviceLocal constructor lifecycle are distinct fixture scopes. Preserve the historical miner17/mode qualification separately; it does not qualify this new feature. Signed enrollment proves key possession, not a complete work census. Independent complete owner/window authority, Server route/custody and pre-sign attachment composition, exact normal/race/causal qualification, actual host adoption and mandatory MG06 conformance remain launch gates.

The [provider prepared-custody successor](evidence/provider-whole-work-prepared-source-20261004.json) freezes SN `07d531ba` with Core `77069204`. Offline recovery now shares the exact approved-profile decoder without requiring a moved original directory to be live. The actual standalone and swarm SDK constructor checks explicitly prepared outbox custody and signed scope before creating the device; runtime settings pin the same independently approved provider key. Runtime cannot create a missing birth. Rotated historical keys need separate original-profile/key-history approval, and retained leaves cannot supply that authority. Read-only admission releases its temporary lease; the live worker reacquires and monitors custody.

This successor adds six distinct miner roots and revises the two existing lifecycle fixtures to prepare one synthetic birth before first startup. The prior six other miner and three bootstrap root sources are unchanged. All new and revised roots remain authored and unexecuted; historical miner17/mode evidence is separate. The corresponding fresh/restore preparation adapter, exact composed qualification, actual approved deployment inputs and MG06 remain required.


Qualification source identity is now local to each execution owner. During the internal-SSD fallback, an unrelated SN mainnet/Rust merge caused a whole-repository cutoff guard to refuse an unstarted Go owner. Its shared plan also rehashed the Go job from the healthy Rust sampler, so changing the Go preparation would have interrupted Rust. The original Go owner stayed unstarted and Rust kept running. The [owner-local source readback](evidence/qualification-owner-local-inputs-source-readback-20261005.json) records the correction and twelve actual deterministic passes.

New callers use `scripts/qualification/owner_input_admission.py`: the shared plan fixes USER units, command paths, cgroups and budgets, while each owner freezes its own adopted job, runner and complete selected-input scope. Unrelated files or a not-yet-started peer's preparation do not invalidate another owner's source authority. Selected physical Go, module, native and embedded files are compared to authenticated Git blobs, including ignored and index-hidden files; selected symlinks, moved inputs and nonblocking open replacement races refuse. Only selected subtrees and their ancestors are traversed. Selected inputs still refuse changes, and resolved dependency paths/device/inode checks detect broken or rebound local cache/source symlinks. Existing lease, fixed resource floors, process ancestry, OOM, child joins and result checks remain mandatory. This source helper does not discover a dependency graph or promote retained test results; runtime callers must explicitly adopt it.
