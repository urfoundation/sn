# Provisional validator evidence CREATE source handoff

Exact source parent: `504de248d810611b74015a90cb20ded725fd0f7c`, the vault
checkpoint selector correction on provisional vault-link
`cdfe07449f86b041fc2aa27343e6d19e168e8ce5`, which inherits read-gas correction
`e27addf6d6057751eec01387676b5947f141d9c2`. This candidate is isolated at
`/home/by/urnetwork/temp/sn-mainnet-evidence-create-20260928` on
`codex/mainnet-evidence-create-20260928`. The containing commit is a provisional
source freeze. It makes no integration, behavioral qualification or live
deployment claim. Sol owns behavioral gates; predecessor outcomes remain
separately recorded and any predecessor correction requires a successor here.

At this freeze Sol reported only the corrected vault checkpoint root's targeted
normal pass, in 265.958 seconds. Its race/control boundary and broader vault
qualification remain open. The parent explicitly authorized this provisional
source freeze without treating that targeted pass as predecessor qualification.
The original vault failure evidence and exact fixture correction are preserved
in `bootstrap-contract-vault-selector-fixture-correction-20260928.md`.

The existing mainnet graph explicitly orders `evidence-create` at index seven,
after vault binding, followed only by `evidence-anchor`. The original approved
plan/signature/hash and historical custody bytes are unchanged. Action seven
must be the same bootstrap deployer's next nonce after action six, with a null
transaction target and exactly zero value. Simulator upgrade, repair and fleet
nonce rules are not imported into this eight-action mainnet prefix.

Reviewed source is `evm/src/STValidatorEvidence.sol`; the generated public ABI is
`stabi/stvalidatorevidence.go`, and the existing release catalog already contains
the `ValidatorEvidence` artifact and its six semantic immutable-reference maps.
No artifact or binding was generated or edited. `Deploy.s.sol` stops after vault
binding. The evidence constructor/domain design is separately established by
the contract, generated ABI and `sim-testnet/evidence_deployment.go`:

```
STValidatorEvidence(derivedProxy, approvedNativeGenesis, sha256(deploymentIdBytes))
```

The deployment ID hash is SHA-256 of the original approved ID bytes, exactly as
`buildValidatorEvidenceDeploymentFromArtifact` does. It is not Keccak, a new
unsigned field, the plan hash or a node-supplied value. Three canonical ABI words
must repack identically through the artifact ABI and generated binding, and the
complete creation bytes must equal the original approved action payload.
Constructor suffixes, wide address words, alternate proxy/implementation, genesis,
deployment ID, sender, nonce, value or target are refused before custody.

The constructor requires code at the supplied coordinator, reads its vault and
netuid, requires code at that vault and a nonzero chain ID, then assigns six
immutables. The expected runtime patches exactly `coordinator`,
`settlementVault`, `chainId`, `netuid`, `genesisHash` and `deploymentIdHash`.
The independent phase fixes chain ID 964 and netuid 25; derived graph identities
fix both contract addresses. The six exact getters retain the same values:

| Getter | Selector | Expected source |
| --- | --- | --- |
| `coordinator()` | `0x0a009097` | Original initialized proxy CREATE |
| `settlementVault()` | `0x2aa84ce6` | Original vault CREATE |
| `chainId()` | `0x9a8a0592` | Approved EVM network, 964 |
| `netuid()` | `0xe78015b1` | Approved subnet, 25 |
| `genesisHash()` | `0x94391a6d` | Approved native genesis |
| `deploymentIdHash()` | `0x05f548dc` | SHA-256 of approved deployment ID bytes |

The reviewed constructor emits no events and its external getter calls are
static. A receipt must explicitly name the original sender, null `to`, the
predicted CREATE address on success, and an empty log list for either status.
Existing transaction-at-block/index, gas/fee, native ancestry and authenticated
native-to-EVM mapping checks still apply. Exact patched runtime, six getters and
zero raw words at slots zero and one are checked at the same canonical inclusion.
Those two words are `_activations` and `_commitments` mapping roots. They are not
an enumerable proof of every hashed mapping entry, historical proof truth or
public evidence availability. An unmapped receipt stays nonfinal and cannot
grant another nonce, signature or allowance.

Current admission is separate from historical receipt validation. The predicted
CREATE address must have no code in both current canonical and pending state.
At each of those selectors, and again at evidence inclusion, the adapter reads
the already-bound vault's runtime, thirteen getters and seven words, then the
bound reserve's runtime, six getters and two words. It also rechecks the
initialized proxy's runtime, 23 stable getters and five distinct storage words,
plus implementation runtime and two constructor words. The proxy policy keeps
its original EVM inclusion height; native height, latest clock and the later
evidence height cannot substitute for it. The proxy's `validatorEvidence()`
getter must still be zero. Configured ownership addresses imply no live Safe
authority. Missing code/getter/storage observations remain read errors.

`evidence-create.json` uses `urnetwork-mainnet-evm-evidence-state-v1` and seals
the exact completed vault-binding journal hash. All eight journal locks remain
held; seven ancestor receipts are reauthenticated and their checkpoints fenced
through initial and refreshed admission. No receipt fields are added. Existing
schemas, signature custody, approval bytes, marker rules and older result
encodings retain their original behavior. Only the selected evidence review
and result add derived domain/address/storage fields.

The original maximum-eight attempt limit is unchanged. Seven prior submissions
leave one evidence attempt. A publication interruption after durable attempt
accounting poisons that owner; on restart the spent last attempt remains spent
even when no transport write occurred. Lost acknowledgement after genuine
creation instead resolves the original receipt without a second send. All later
same-sender value and maximum gas liability remains reserved. These semantics
do not implement a complete nine-action attempt policy or the owner anchor.

Completion is `evidence-created-unanchored`; canonical failure remains
`create-reverted-nonce-consumed`. The only remaining graph action is
`evidence-anchor`, which this command still rejects. Installation and activation
flags remain false. No Safe-inner executor, live signing, RPC access/write,
chain mutation, DB action or approval expansion occurred. External producer/
source approval, authenticated native-to-EVM mapping, owned-RPC state/finality
trust and distributed custody exclusivity remain open.

Twenty-eight deterministic top-level `TestEvmEvidenceCreate*` roots were authored.
The new fixture forms constructor input directly from the generated ABI and
release, then executes all seven genuine predecessor effects and the reviewed
evidence constructor in geth. It inherits the fixed independent getter budget
and vault's instance-local call intrinsic-gas handling. Its receipt logs come
from geth's actual transaction log set. The 500,000-gas constructor failure is
genuine code-deposit exhaustion, with empty deployed code and a consumed original
nonce; no status-zero receipt is fabricated. This behavioral assertion awaits
Sol's execution, as do all other roots.

Coverage includes independent constructor/domain encoding, prior byte retention,
whole-graph preview, exact receipt identity/empty logs, all six evidence getters
and both root words, every bound-vault getter/word, current/inclusion reserve,
proxy and implementation dependencies, original proxy EVM policy height,
unavailable reads, unmapped receipt retention and later genuine inclusion,
seven receipt reaudits and fourteen checkpoint barriers, eight locks and sixteen
custody aliases, cumulative/future funding, last-attempt publication ambiguity,
lost reply, initial claim boundaries, changed ancestor lineage, output loss,
gas failure, rejected failed vault binding, expiry/nonce/runtime changes,
retained semantic hashes and explicit rejection of the anchor command.

The evidence checkpoint callback dispatches canonical map selectors and pending
string selectors separately. A direct pending owner-slot probe must leave the
historical inclusion barrier untouched; its observation is cleared before each
real command, whose refreshed case must then observe its own pending read.
This carries the vault selector correction into the copied evidence callback
and makes restoration of an unchecked map assertion fail directly, without
waiting for an HTTP panic to exhaust retries.

Compile-only `go test -c -p=2` passed for product, authored fixtures, final
source and the typed selector correction. The exact source on the corrected
parent was compiled again before freeze. The implementation owner ran no
behavioral test body. Gofmt and
`git diff --check` passed as static checks. Logs and the unexecuted binary are
under `/mnt/data/sn-testnet/worktrees/sn-mainnet-evidence-create-20260928/`:
`compile-product.log`, `compile-fixtures.log`, `compile-final.log`,
`compile-selector.log`, `compile-parent-504de2.log`, external
`compile.mod`/`compile.sum`, and `mainnet-static.test`. Committed module files,
generated artifacts/bindings and shared branches were untouched.

The physical compile dependency graph remains under
`/mnt/data/sn-testnet/qualification/mg08-ur-offline-admission-20260928/source`:

| Module | Commit |
| --- | --- |
| Connect | `b163f9dd9ac374942fe97331f26631248a9c1f81` |
| Server | `5dc11761373580b5a0ddd9757cd6e4eb94140e27` |
| SDK | `516521fb16da46c9f4bff0b58221e1941694f616` |
| Proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` |
| UserWireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` |
| Warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` |
| Glog | `892ade4a6be396b32ea82a550f243190b5992180` |
| Goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` |

Sol should fence the exact containing commit, run `^TestEvmEvidenceCreate`
normally and under race, then adjacent
`^TestEvm(Create|VaultCreate|CoordinatorCreate|EscrowRegister|ProxyCreate|ReserveLink|VaultLink|PhasePreview)`
and appropriate full/static gates. Bound expensive scopes with exact shards
while preserving aggregate timeout evidence. Establish each passing positive
root before its isolated causal control:

| Control | Root expected to reject its mutant |
| --- | --- |
| Remove exact approved evidence creation-byte comparison | `TestEvmEvidenceCreateRejectsChangedConstructor` |
| Replace SHA-256 deployment ID rule with Keccak | `TestEvmEvidenceCreateExecutesExactConstructorAndPreservesAncestors` |
| Bypass evidence receipt sender/CREATE target checks | `TestEvmEvidenceCreateRequiresExactReceipt` |
| Bypass explicit empty constructor-log check | `TestEvmEvidenceCreateRequiresExactReceipt` |
| Skip evidence inclusion runtime comparison | `TestEvmEvidenceCreateRequiresEveryOwnPostcondition` |
| Skip evidence inclusion getter reads | `TestEvmEvidenceCreateRequiresEveryOwnPostcondition` |
| Skip evidence inclusion mapping-root reads | `TestEvmEvidenceCreateRequiresEveryOwnPostcondition` |
| Bypass `admitEvidenceCreate` | `TestEvmEvidenceCreateRechecksBoundVaultAtCurrentAndInclusion` |
| Skip same-block bound-vault observations | `TestEvmEvidenceCreateRechecksBoundVaultAtCurrentAndInclusion` |
| Skip nested reserve/proxy/implementation observations | `TestEvmEvidenceCreateRechecksReserveProxyAndImplementation` |
| Substitute native for original proxy EVM policy height | `TestEvmEvidenceCreatePreservesOriginalProxyPolicyHeight` |
| Skip online predecessor audits | `TestEvmEvidenceCreateReauditsSevenPredecessors` |
| Remove both predecessor continuity loops | `TestEvmEvidenceCreateFencesSevenPredecessorCheckpoints` |
| Restore unchecked map assertion for mixed storage selectors | `TestEvmEvidenceCreateFencesSevenPredecessorCheckpoints` |
| Omit predecessor attempts from send allowance | `TestEvmEvidenceCreateKeepsCumulativeGraphAttempts` |
| Ignore later same-sender value/gas liability | `TestEvmEvidenceCreatePreservesFutureValueAndGasFunding` |

Keep exact mutant provenance and assertion output. A positive fixture/product or
causal failure requires diagnosis and a successor, not a waiver or carried pass.
