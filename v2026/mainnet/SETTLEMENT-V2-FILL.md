# Filling the settlement v2 phase config (step 8.3)

This is the recipe for drafting `/secure/ur-mainnet/settlement-v2.unsigned.json`
from `mainnet/settlement-v2.unsigned.example.json`. No command builds the six
actions' `data` fields, so the recipe derives them with `cast`, `jq`, `b2sum`
and `xxd`, fills the example, runs `preview` and compares its answer with the
owner inputs. It stops at a reviewed preview. Signing and `plan` follow in step
8.3 of `VERSION2-LAUNCH.md`.

What stands behind it:

- The blocks marked `bash` below were extracted from this file and run in
  order on 2026-10-13, with synthetic owner inputs, against two builds of
  `sn-mainnet`: sn `29c8bd70`, and `main` at `c844f308`. With and without the
  claim batcher, `preview` admitted the result and every comparison of
  section 7 printed `ok`. Signed with a throwaway key, the six-action result
  passed `plan` with the preview's plan hash.
- The same derivation filled the config that deployed all six actions on the
  runtime 475 rehearsal node.
- Every refusal in section 8's table was produced by changing one field and
  running `preview`. Both builds answered alike, except on the runtime source:
  `main` also lists 477's, and binds each source to its spec version. The
  table gives both answers.
- Not run: mainnet, runtime 478, an `https://` route against a real server,
  and the real approval key. Tool versions used: `cast` 1.7.1, `jq` 1.7.

Work in one shell from the first block to the last. Section 9 lists what must
be redone when an input changes.

## 1. Before you start

- **Tools on `PATH`:** `sn-mainnet`, `cast`, `jq`, `bc`, `b2sum`, `xxd`,
  `sha256sum`, `awk`, `tr`, `seq`.
- **Files that must exist:**
  - the release file of step 8.1, `/secure/ur-mainnet/settlement-v2-contracts.json`;
  - the runtime snapshot of step 2, `/secure/ur-mainnet/runtime-snapshot.json`,
    taken again just before this recipe. It supplies O3 and the plan's start
    checkpoint. Every later `resume --online` scans forward from that
    checkpoint, and the send window closes 7,200 blocks after it, so an old
    snapshot costs time in step 8.4;
  - the example, in the frozen sn checkout.
- **Modes.** `preview` reads the config and the release file only when neither
  has a group or other permission bit, and only by absolute path. The first
  block sets `umask 077`, so the files this recipe writes are private.

## 2. Owner inputs

Replace every `<...>` placeholder. Write hex in lowercase; the block lowers it
again, because `preview` prints most addresses in lowercase.

```bash
umask 077
SECURE=/secure/ur-mainnet
SN='<absolute-path-of-the-frozen-sn-checkout>'
EXAMPLE=$SN/mainnet/settlement-v2.unsigned.example.json
RELEASE=$SECURE/settlement-v2-contracts.json
SNAPSHOT=$SECURE/runtime-snapshot.json
TEMPLATE=$SECURE/settlement-v2.template.json
UNSIGNED=$SECURE/settlement-v2.unsigned.json
PREVIEW=$SECURE/settlement-v2.preview.json

APPROVAL_PUBLIC_KEY='<approval-public-key-ed25519>'
O2_EVM_RPC_URL='<O2-evm-rpc-url>'
O3_RUNTIME_SOURCE_COMMIT='<O3-runtime-source-commit>'
O4_DEPLOYER='<O4-deployer-address>'
O4_NONCE='<O4-nonce>'
O5_ESCROW_HOTKEY='<O5-escrow-hotkey>'
O6_MAXIMUM_BURN_RAO='<O6-maximum-burn-rao>'
O7_GAS='[4000000,2500000,3000000,1000000,150000,400000]'
O7_FEE_CAP_WEI='<O7-fee-cap-wei>'
O7_TIP_CAP_WEI='<O7-tip-cap-wei>'
O7_SOURCE_LOCK_HASH='<O7-source-lock-hash>'
O7_CUSTODY_ID='<O7-custody-id>'
O7_CUSTODY_FENCE_HASH='<O7-custody-fence-hash>'
O7_CUTOVER_EVIDENCE_HASH='<O7-cutover-evidence-hash>'
O8_OWNER='<O8-owner-safe-address>'
O9_V1_COORDINATOR='<O9-v1-coordinator-address>'
O10_AUDITOR='<O10-auditor-address>'
O11_CLAIM_BATCHER='<O11-yes-or-no>'
O12_DEMAND_FEE_WEI=750000000000000

lower() { printf '%s' "$1" | tr 'A-F' 'a-f'; }
APPROVAL_PUBLIC_KEY=$(lower "$APPROVAL_PUBLIC_KEY")
O3_RUNTIME_SOURCE_COMMIT=$(lower "$O3_RUNTIME_SOURCE_COMMIT")
O4_DEPLOYER=$(lower "$O4_DEPLOYER")
O5_ESCROW_HOTKEY=$(lower "$O5_ESCROW_HOTKEY")
O8_OWNER=$(lower "$O8_OWNER")
O9_V1_COORDINATOR=$(lower "$O9_V1_COORDINATOR")
O10_AUDITOR=$(lower "$O10_AUDITOR")
for v in SN APPROVAL_PUBLIC_KEY O2_EVM_RPC_URL O3_RUNTIME_SOURCE_COMMIT O4_DEPLOYER O4_NONCE O5_ESCROW_HOTKEY O6_MAXIMUM_BURN_RAO O7_FEE_CAP_WEI O7_TIP_CAP_WEI O7_SOURCE_LOCK_HASH O7_CUSTODY_ID O7_CUSTODY_FENCE_HASH O7_CUTOVER_EVIDENCE_HASH O8_OWNER O9_V1_COORDINATOR O10_AUDITOR O11_CLAIM_BATCHER; do
  case "${!v}" in *'<'*|'') echo "STOP  $v is not filled";; esac
done; echo "inputs read"
```

Expect only `inputs read`.

| Variable | Owner input | Form | Where it lands |
| --- | --- | --- | --- |
| `APPROVAL_PUBLIC_KEY` | the approval key's public key, which LAUNCH.md:375-376 names | `0x` and 64 hex | `approval_public_key_ed25519` |
| `O2_EVM_RPC_URL` | O2, the route | `http://IP:port` with a literal IP, or `https://host` with a pin (section 6) | `plan.route.rpc_url` |
| `O3_RUNTIME_SOURCE_COMMIT` | O3, the reviewed source of the runtime step 2 observes | 40 hex. For runtime 478 the runbook names `850f2755bb05a3851f948489f0ab27b0645cebce`. The binary must list it (section 8) | `plan.runtime.runtime_source_commit` |
| `O4_DEPLOYER` | O4 | `0x` and 40 hex; not O8 and not O10 | every action's `sender`; the constructor's `bootstrap` word |
| `O4_NONCE` | O4, the deployer's next nonce N | decimal | the actions' nonces N to N+5; every predicted address |
| `O5_ESCROW_HOTKEY` | O5 | `0x` and 64 hex, nonzero | action 0's constructor |
| `O6_MAXIMUM_BURN_RAO` | O6, the escrow registration cap | decimal rao, nonzero | action 1's calldata and, × 10^9, its `value_wei` |
| `O7_GAS` | O7, one gas limit per action in order | JSON array; the example's starting limits | each action's `gas` |
| `O7_FEE_CAP_WEI`, `O7_TIP_CAP_WEI` | O7 | decimal wei; the fee cap nonzero, the tip at most the fee cap | each action's `fee_cap_wei`, `tip_cap_wei` |
| `O7_SOURCE_LOCK_HASH`, `O7_CUSTODY_FENCE_HASH`, `O7_CUTOVER_EVIDENCE_HASH` | O7, the three evidence pins | `sha256:` and 64 lowercase hex, not all zero; the command below the table pins a file | `plan.source_lock_hash`, `plan.custody_fence_hash`, `plan.cutover_evidence_hash` |
| `O7_CUSTODY_ID` | O7, the custody label | 1 to 128 of `A-Z a-z 0-9 - _` | `plan.custody_id` |
| `O8_OWNER` | O8, the governance Safe | `0x` and 40 hex | the initializer's `owner` |
| `O9_V1_COORDINATOR` | O9, the v1 coordinator proxy | `0x` and 40 hex | the initializer's `v1Coordinator` |
| `O10_AUDITOR` | O10, the auditor's address | `0x` and 40 hex | the initializer's `auditors`, with quorum 1 |
| `O11_CLAIM_BATCHER` | O11 | `yes` or `no` | with `no`, the sixth action is left out |
| `O12_DEMAND_FEE_WEI` | O12, decided | 750000000000000 | action 0's constructor |

The three evidence pins are O7's choice. The binary checks only their form
and signs them into the plan; nothing compares them with a file
(`mainnet/evm_phase.go:59-62,180`). The repositories name no file for them.
A suggestion, not a rule: pin the frozen checkout's source lock for the first,
and step 8.2's fence file, `/secure/ur-mainnet/run-v2-custody/former-writer.json`,
for the second. The source lock command needs Go 1.27.3 on `PATH`. The pin of
any file is `sha256:` and its SHA-256:

```sh
sn-mainnet source-lock --sn-dir "$SN" > /secure/ur-mainnet/source-lock.json
echo "sha256:$(sha256sum /secure/ur-mainnet/source-lock.json | cut -d' ' -f1)"
echo "sha256:$(sha256sum /secure/ur-mainnet/run-v2-custody/former-writer.json | cut -d' ' -f1)"
```

`preview` reads nothing from the chain. It cannot tell a wrong O5, O6, O8, O9,
O10, O12 or nonce from a right one, as long as the value is well formed
(section 8). Those are confirmed only by the comparison of section 7.

## 3. Values read from the snapshot and the release file

```bash
O2_NATIVE_CHAIN=$(jq -r '.snapshot.identity.native_chain' "$SNAPSHOT")
O2_GENESIS_HASH=$(jq -r '.snapshot.identity.genesis_hash' "$SNAPSHOT")
O3_RUNTIME_VERSION=$(jq -c '.snapshot.runtime_version' "$SNAPSHOT")
O3_RUNTIME_CODE_HASH=$(jq -r '.snapshot.runtime_code_hash' "$SNAPSHOT")
O3_RUNTIME_METADATA_HASH=$(jq -r '.snapshot.runtime_metadata_hash' "$SNAPSHOT")
O7_START_NATIVE_NUMBER=$(jq -r '.snapshot.identity.finalized_number' "$SNAPSHOT")
O7_START_NATIVE_HASH=$(jq -r '.snapshot.identity.finalized_hash' "$SNAPSHOT")
O7_VALID_THROUGH_NATIVE=$((O7_START_NATIVE_NUMBER + 7200))
RELEASE_SHA256=sha256:$(sha256sum "$RELEASE" | cut -d' ' -f1)
echo "chain $O2_NATIVE_CHAIN genesis $O2_GENESIS_HASH"
echo "runtime $O3_RUNTIME_VERSION code $O3_RUNTIME_CODE_HASH metadata $O3_RUNTIME_METADATA_HASH"
echo "start $O7_START_NATIVE_NUMBER $O7_START_NATIVE_HASH through $O7_VALID_THROUGH_NATIVE"
echo "release $RELEASE_SHA256"
[ "$O2_GENESIS_HASH" = 0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03 ] && echo "ok    mainnet genesis" || echo "STOP  the snapshot is not of mainnet"
```

Stop unless all of these hold:

- the chain is `Bittensor` and the last line is `ok    mainnet genesis`. The
  genesis it compares is the one step 2 passes as `--expected-genesis`;
- the runtime version and both hashes are the ones gate 2 passed on (O3);
- the release pin is the one step 8.1 produced. On 2026-10-13 it was
  `sha256:d0ffbe0b52ab61370fa4a33bec9f5688dd579f312263e867bd9056d78d673240`.

`O7_VALID_THROUGH_NATIVE` is the latest the plan admits, 7,200 blocks after
the checkpoint, about 24 hours at 12 seconds a block. O7 may set an earlier
one; a later one is refused.

## 4. Predicted addresses

A contract created by a plain transaction gets the address
keccak256(rlp(sender, nonce)), its last 20 bytes. `cast compute-address`
computes it. With D the deployer and N its nonce:

| Variable | Address of | Derived as |
| --- | --- | --- |
| `VAULT2` | vault2 | CREATE(D, N) |
| `IMPLEMENTATION` | the `STSettlement2` implementation | CREATE(D, N+2) |
| `PROXY` | the `STSettlement2` proxy, which the server and the validator name | CREATE(D, N+3) |
| `BATCHER` | the claim batcher | CREATE(D, N+5) |

N+1 is the escrow registration and N+4 the binding. They are calls and create
nothing.

`SELF_COLDKEY` is the native account the runtime maps vault2's address to:
BLAKE2b-256 of the four bytes `evm:` followed by the 20 bytes of the address.

```bash
predict() { cast compute-address "$O4_DEPLOYER" --nonce "$1" | awk '{print tolower($NF)}'; }
VAULT2=$(predict "$O4_NONCE")
IMPLEMENTATION=$(predict $((O4_NONCE + 2)))
PROXY=$(predict $((O4_NONCE + 3)))
BATCHER=$(predict $((O4_NONCE + 5)))
SELF_COLDKEY=0x$({ printf 'evm:'; printf '%s' "${VAULT2#0x}" | xxd -r -p; } | b2sum -l 256 | cut -d' ' -f1)
echo "vault2 $VAULT2 implementation $IMPLEMENTATION proxy $PROXY batcher $BATCHER"
echo "vault2's mapped coldkey $SELF_COLDKEY"
```

## 5. The six `data` fields

Each creation is the contract's `creation` hex from the release file, followed
by its constructor's ABI words when it has any. Each call is a 4-byte selector
and its ABI words.

| Action | `data` | Built from |
| --- | --- | --- |
| 0 `vault2-create` | `SettlementVault2` creation, then five words: `uint16 netuid` 25, `bytes32 escrowHotkey` O5, `bytes32 selfColdkey`, `address bootstrap` D, `uint256 demandFeeWei` O12 | O4, O5, O12 |
| 1 `escrow2-register` | `registerEscrow(uint64)`, selector `0x775d2762`, one word: the cap O6 | O6 |
| 2 `settlement2-create` | `Settlement2` creation, nothing after it | |
| 3 `proxy2-create` | `ERC1967Proxy` creation, then `(address implementation, bytes initializer)`. The initializer is the calldata of `initialize(address owner, address v1Coordinator, address vault, address[] auditors, uint256 quorum)`, selector `0x75d1a9d6`, with O8, O9, vault2, `[O10]` and 1 | O4, O8, O9, O10 |
| 4 `vault2-link` | `setCoordinatorOnce(address)`, selector `0xf406b76b`, one word: the proxy | O4 |
| 5 `batcher-create` | `ClaimBatcher` creation, nothing after it | O11 |

```bash
creation() { jq -r --arg name "$1" '.artifacts[] | select(.name == $name) | .creation' "$RELEASE"; }
VAULT2_WORDS=$(cast abi-encode 'constructor(uint16,bytes32,bytes32,address,uint256)' 25 "$O5_ESCROW_HOTKEY" "$SELF_COLDKEY" "$O4_DEPLOYER" "$O12_DEMAND_FEE_WEI")
VAULT2_CREATE_DATA=0x$(creation SettlementVault2)${VAULT2_WORDS#0x}
ESCROW2_REGISTER_DATA=$(cast calldata 'registerEscrow(uint64)' "$O6_MAXIMUM_BURN_RAO")
SETTLEMENT2_CREATE_DATA=0x$(creation Settlement2)
INITIALIZER=$(cast calldata 'initialize(address,address,address,address[],uint256)' "$O8_OWNER" "$O9_V1_COORDINATOR" "$VAULT2" "[$O10_AUDITOR]" 1)
PROXY_WORDS=$(cast abi-encode 'constructor(address,bytes)' "$IMPLEMENTATION" "$INITIALIZER")
PROXY2_CREATE_DATA=0x$(creation ERC1967Proxy)${PROXY_WORDS#0x}
VAULT2_LINK_DATA=$(cast calldata 'setCoordinatorOnce(address)' "$PROXY")
BATCHER_CREATE_DATA=0x$(creation ClaimBatcher)
echo "data lengths: ${#VAULT2_CREATE_DATA} ${#ESCROW2_REGISTER_DATA} ${#SETTLEMENT2_CREATE_DATA} ${#PROXY2_CREATE_DATA} ${#VAULT2_LINK_DATA} ${#BATCHER_CREATE_DATA}"
```

With the release of 2026-10-13 and one auditor the lengths, in characters with
the `0x`, are `27114 74 17596 2124 74 2202`. A shorter or empty one means a
command above failed: fix it before going on.

## 6. The value, the total, and the fill

The escrow registration's value is the cap in wei, the cap with nine zeros
appended. `maximum_total_wei` must cover that value plus every gas limit times
the fee cap; the block sets it to exactly that sum.

```bash
if [ "$O11_CLAIM_BATCHER" = yes ]; then ACTIONS=6; cp "$EXAMPLE" "$TEMPLATE"; else ACTIONS=5; jq 'del(.plan.actions[5])' "$EXAMPLE" > "$TEMPLATE"; fi
O6_ESCROW_VALUE_WEI=${O6_MAXIMUM_BURN_RAO}000000000
GAS_TOTAL=$(jq -n --argjson gas "$O7_GAS" --argjson actions "$ACTIONS" '$gas[0:$actions] | add')
O7_MAXIMUM_TOTAL_WEI=$(echo "$O6_ESCROW_VALUE_WEI + $GAS_TOTAL * $O7_FEE_CAP_WEI" | bc)
echo "actions $ACTIONS gas $GAS_TOTAL escrow value $O6_ESCROW_VALUE_WEI maximum total $O7_MAXIMUM_TOTAL_WEI"

jq \
  --arg approval_key "$APPROVAL_PUBLIC_KEY" \
  --arg native_chain "$O2_NATIVE_CHAIN" --arg genesis "$O2_GENESIS_HASH" --arg rpc "$O2_EVM_RPC_URL" \
  --arg source "$O3_RUNTIME_SOURCE_COMMIT" --argjson version "$O3_RUNTIME_VERSION" \
  --arg code "$O3_RUNTIME_CODE_HASH" --arg metadata "$O3_RUNTIME_METADATA_HASH" \
  --arg run_directory "$SECURE/run-v2" --arg release "$RELEASE" --arg release_sha256 "$RELEASE_SHA256" \
  --arg source_lock "$O7_SOURCE_LOCK_HASH" --arg custody_id "$O7_CUSTODY_ID" \
  --arg custody_fence "$O7_CUSTODY_FENCE_HASH" --arg cutover "$O7_CUTOVER_EVIDENCE_HASH" \
  --argjson start "$O7_START_NATIVE_NUMBER" --arg start_hash "$O7_START_NATIVE_HASH" \
  --argjson through "$O7_VALID_THROUGH_NATIVE" --arg total "$O7_MAXIMUM_TOTAL_WEI" \
  --argjson gas "$O7_GAS" --arg fee "$O7_FEE_CAP_WEI" --arg tip "$O7_TIP_CAP_WEI" \
  --arg deployer "$O4_DEPLOYER" --argjson nonce "$O4_NONCE" \
  --arg vault2 "$VAULT2" --arg escrow_value "$O6_ESCROW_VALUE_WEI" \
  --arg d0 "$VAULT2_CREATE_DATA" --arg d1 "$ESCROW2_REGISTER_DATA" --arg d2 "$SETTLEMENT2_CREATE_DATA" \
  --arg d3 "$PROXY2_CREATE_DATA" --arg d4 "$VAULT2_LINK_DATA" --arg d5 "$BATCHER_CREATE_DATA" '
  .approval_public_key_ed25519 = $approval_key
  | .plan.network.native_chain = $native_chain
  | .plan.network.genesis_hash = $genesis
  | .plan.runtime.runtime_source_commit = $source
  | .plan.runtime.runtime_version = $version
  | .plan.runtime.runtime_code_hash = $code
  | .plan.runtime.runtime_metadata_hash = $metadata
  | .plan.route.rpc_url = $rpc
  | .plan.run_directory = $run_directory
  | .plan.artifacts = {path: $release, sha256: $release_sha256}
  | .plan.source_lock_hash = $source_lock
  | .plan.custody_id = $custody_id
  | .plan.custody_fence_hash = $custody_fence
  | .plan.cutover_evidence_hash = $cutover
  | .plan.start_native_number = $start
  | .plan.start_native_hash = $start_hash
  | .plan.valid_through_native = $through
  | .plan.maximum_total_wei = $total
  | .plan.actions |= (to_entries | map(
      .value.sender = $deployer | .value.nonce = $nonce + .key | .value.gas = $gas[.key]
      | .value.fee_cap_wei = $fee | .value.tip_cap_wei = $tip | .value))
  | .plan.actions[0].data = $d0
  | .plan.actions[1] |= (.to = $vault2 | .data = $d1 | .value_wei = $escrow_value)
  | .plan.actions[2].data = $d2
  | .plan.actions[3].data = $d3
  | .plan.actions[4] |= (.to = $vault2 | .data = $d4)
  | if (.plan.actions | length) == 6 then .plan.actions[5].data = $d5 else . end
  ' "$TEMPLATE" > "$UNSIGNED"
grep -o '"<[^"]*>"' "$UNSIGNED" || echo "no placeholder left"
stat -c '%a %n' "$UNSIGNED"
```

Expect `no placeholder left` and mode `600`. `approval_signature_ed25519`
stays empty: `preview` refuses a config that carries a signature.

For an `https://` route only, add the leaf SPKI pin of O2 now. Offline,
`preview` admitted this form; it refuses `https` without a pin, and a pin on
`http`:

```sh
jq '.plan.route.tls_spki_sha256 = "<O2-tls-spki-sha256>"' "$UNSIGNED" > "$UNSIGNED.tmp" && mv "$UNSIGNED.tmp" "$UNSIGNED"
```

## 7. Preview, then compare

```bash
sn-mainnet bootstrap-contracts preview --action vault2-create --config "$UNSIGNED" > "$PREVIEW"
echo "preview exit $?"
```

Exit 0 means the structure, the release file and every derived word passed. A
refusal exits 2 and prints one line starting `contract phase unsigned preview:`
(section 8). Then compare. Each line must print `ok`:

```bash
check() { if [ "$2" = "$3" ]; then echo "ok    $1 = $2"; else echo "DIFF  $1: preview has $2, the inputs give $3"; fi; }
p() { jq -r "$1" "$PREVIEW"; }
check 'approval key'             "$(p '.approval_public_key_ed25519')" "$APPROVAL_PUBLIC_KEY"
check 'release file pin'         "$(p '.plan.artifacts.sha256')" "$RELEASE_SHA256"
check 'O4 sender of each action' "$(p '[.plan.actions[].sender] | unique | join(",")')" "$O4_DEPLOYER"
check 'O4 nonces'                "$(p '[.plan.actions[].nonce] | join(",")')" "$(seq -s, "$O4_NONCE" $((O4_NONCE + ACTIONS - 1)))"
check 'vault2 address'           "$(p '.vault2.address | ascii_downcase')" "$VAULT2"
check 'implementation address'   "$(p '.settlement2_implementation.address | ascii_downcase')" "$IMPLEMENTATION"
check 'proxy address'            "$(p '.settlement2.address | ascii_downcase')" "$PROXY"
if [ "$O11_CLAIM_BATCHER" = yes ]; then check 'claim batcher address' "$(p '.claim_batcher.address | ascii_downcase')" "$BATCHER"; else check 'no claim batcher' "$(p '.claim_batcher')" null; fi
check 'O5 escrow hotkey'         "$(p '.vault2_constructor.escrow_hotkey')" "$O5_ESCROW_HOTKEY"
check 'O12 demand fee'           "$(p '.vault2_constructor.demand_fee_wei')" "$O12_DEMAND_FEE_WEI"
check 'O6 burn cap'              "$(p '.escrow_registration.maximum_burn_rao')" "$O6_MAXIMUM_BURN_RAO"
check 'escrow value'             "$(p '.escrow_registration.funding_wei')" "$O6_ESCROW_VALUE_WEI"
check 'escrow hotkey, again'     "$(p '.escrow_registration.hotkey')" "$O5_ESCROW_HOTKEY"
check 'vault2 mapped coldkey'    "$(p '.escrow_registration.coldkey')" "$SELF_COLDKEY"
check 'O8 owner'                 "$(p '.settlement2_initializer.owner')" "$O8_OWNER"
check 'O9 v1 coordinator'        "$(p '.settlement2_initializer.v1_coordinator')" "$O9_V1_COORDINATOR"
check 'initializer vault'        "$(p '.settlement2_initializer.vault')" "$VAULT2"
check 'O10 auditors'             "$(p '.settlement2_initializer.auditors | join(",")')" "$O10_AUDITOR"
check 'quorum'                   "$(p '.settlement2_initializer.quorum')" 1
check 'binding vault'            "$(p '.vault_binding.vault')" "$VAULT2"
check 'binding coordinator'      "$(p '.vault_binding.coordinator')" "$PROXY"
jq -r '.plan.actions[] | [.id, "nonce \(.nonce)", "gas \(.gas)", "fee \(.fee_cap_wei)", "tip \(.tip_cap_wei)", "value \(.value_wei)", "to \(.to // "CREATE")"] | join("  ")' "$PREVIEW"
jq '.plan | {network, runtime, route, run_directory, artifacts, source_lock_hash, custody_id, custody_fence_hash, cutover_evidence_hash, start_native_number, start_native_hash, valid_through_native, maximum_attempts, maximum_total_wei, netuid}' "$PREVIEW"
jq '{plan_hash, approval_signing_message_sha256, approval_verified, runtime_hashes: {vault2: .vault2.runtime_hash, implementation: .settlement2_implementation.runtime_hash, proxy: .settlement2.runtime_hash, claim_batcher: (.claim_batcher.runtime_hash // null)}}' "$PREVIEW"
```

The `check` lines compare the preview with the shell variables, which proves
the fill. They do not prove the variables. Read the printed values against
the written owner inputs as well:

| Preview field | Confirms | Compare with |
| --- | --- | --- |
| `.approval_public_key_ed25519` | the key whose signature `plan` will demand | the approval key's public key |
| `.plan.artifacts.sha256` | the release file | step 8.1's pin |
| `.plan.actions[].sender`, `.nonce` | the deployer and its nonces | O4 |
| `.vault2.address`, `.settlement2_implementation.address`, `.settlement2.address`, `.claim_batcher.address` | the four predicted addresses. These four print in mixed case; the others in lowercase | section 4 |
| `.vault2_constructor.escrow_hotkey`, `.escrow_registration.hotkey` | the escrow hotkey | O5 |
| `.vault2_constructor.demand_fee_wei` | the demand fee | O12, 750000000000000 |
| `.escrow_registration.maximum_burn_rao`, `.funding_wei` | the cap and its value | O6 and O6 × 10^9 |
| `.escrow_registration.coldkey` | vault2's mapped coldkey, which will own the escrow hotkey | `SELF_COLDKEY` |
| `.settlement2_initializer.owner`, `.v1_coordinator`, `.auditors`, `.quorum` | the initializer | O8, O9, O10 and 1 |
| `.settlement2_initializer.vault`, `.vault_binding.vault`, `.vault_binding.coordinator` | the proxy is initialized with vault2, and vault2 is bound to the proxy | `VAULT2`, `VAULT2`, `PROXY` |
| the action lines: `gas`, `fee`, `tip`, `value`, `to` | the envelope of each action | O7; the value is zero except O6 × 10^9 on `escrow2-register`; `to` is vault2 on actions 1 and 4 |
| `.plan.network`, `.plan.runtime`, `.plan.route` | the chain, the runtime and the route | O2, O3 |
| `.plan.start_native_number`, `.start_native_hash`, `.valid_through_native`, `.maximum_total_wei`, the three pins, `.custody_id` | the window, the ceiling and the evidence | O7 |
| `.vault2.runtime_hash`, `.settlement2_implementation.runtime_hash`, `.settlement2.runtime_hash`, `.claim_batcher.runtime_hash`, printed last as `runtime_hashes` | the code hash each contract must have once deployed. The proxy's and the batcher's equal the release file's `runtime_hash`; vault2's and the implementation's do not, because their immutables are filled in | the receipt's `runtime_hash` in steps 8.4 and 8.5. In the dry run each receipt carried its contract's value; the escrow registration's and the binding's carried vault2's |
| `.plan_hash` | the plan this preview is of. `plan` prints the same value after signing, and it becomes `SETTLEMENT_V2_HASH` | |

Optional, against the approved route, read only. These read the chain, which
`preview` does not:

```sh
cast nonce --rpc-url "$O2_EVM_RPC_URL" "$O4_DEPLOYER"     # must print O4's nonce
cast balance --rpc-url "$O2_EVM_RPC_URL" "$O4_DEPLOYER"   # must be at least the maximum total
cast code --rpc-url "$O2_EVM_RPC_URL" "$VAULT2"           # must print 0x
```

Then continue step 8.3: extract `approval_signing_message_hex`, sign it, put
the signature into a copy saved as `settlement-v2.json`, and run `plan`.
`plan` prints the preview's `plan_hash`. Unlike `preview`, it needs the run
directory of step 8.2 to exist. Without it, it exits 2 with "contract phase
authority: bootstrap root directory cannot traverse symlinks", then "lstat
…/run-v2: no such file or directory".

## 8. What `preview` refuses, and what it cannot

Every refusal exits 2 with `contract phase unsigned preview: ` and one of
these. Each was produced by changing only the field named.

| Wrong field | Refusal |
| --- | --- |
| action 0: the netuid, `selfColdkey` or `bootstrap` word; a zero escrow hotkey; a demand fee of zero or not a multiple of 10^9 | approved vault2 constructor differs from its netuid, mapped coldkey and bootstrap, or lacks a nonzero escrow hotkey and a nonzero whole-rao demand fee |
| the nonce changed after the data was built, so vault2's address moved | the same line |
| action 0: a changed creation byte, or missing words | vault2 payload is not the exact release creation and five constructor words |
| action 0 with a `to` | first contract action must be a zero-value CREATE |
| action 1: a `to` that is not vault2, or another selector or length | vault2 escrow registration must be one exact uint64 call to the derived vault2 |
| action 1: a zero cap, or `value_wei` other than the cap × 10^9 | vault2 escrow registration selector, nonzero burn cap or exact wei funding differs |
| action 2: any byte beyond the creation | approved settlement2 implementation differs from the exact argument-free release creation |
| action 3: an implementation other than CREATE(D, N+2) | settlement2 proxy implementation or initializer differs |
| action 3: an initializer vault other than vault2; a zero owner or v1 coordinator; the deployer as owner or auditor; a repeated auditor; a quorum outside 1 to the auditor count | settlement2 initializer vault, owner, v1 coordinator, auditors, quorum or role separation differs |
| action 4: a coordinator other than the proxy, or a `to` that is not vault2 | vault2 binding must call the derived vault2 with the exact initialized proxy and zero value |
| action 5: any byte beyond the creation | approved claim batcher differs from the exact argument-free release creation |
| a gap in the nonces, or another sender on one action | settlement v2 actions must be one deployer's consecutive nonces with zero-value CREATEs |
| `maximum_total_wei` one wei below the sum; a value on action 0 | contract phase exceeds original total cost reservation |
| the release pin, or a release file its group or others can read | contract release differs from its independent content pin |
| a config file its group or others can read | bootstrap root input is not a bounded private regular file |
| a relative `--config` path | bootstrap root input context, path or bound is invalid |
| a `runtime_source_commit` the binary does not list. `main` at `c844f308` lists the sources of 470, 473, 475 and 477, so it refuses 478's `850f2755…` until that source is merged and `sn-mainnet` rebuilt | contract phase runtime/source approval is incomplete |
| a listed `runtime_source_commit` beside another runtime's `specVersion`, such as 475's or 477's commit with 478. `main` at `c844f308` refuses it; the `29c8bd70` build admitted it | contract phase runtime version differs from the one its source commit was reviewed for |
| a window longer than 7,200 blocks; a pin without `sha256:`; an all-zero genesis | contract phase lacks exact identity, scope or finite bounds |
| uppercase hex in a `data` | root receipt hex is invalid or noncanonical |
| a placeholder left in a `data` | root receipt hex exceeds bound or lacks prefix |
| a placeholder left where a number belongs | json: cannot unmarshal string into Go struct field evmPhaseConfig.plan.actions.5.nonce of type uint64 |
| a tip above the fee cap, or a gas limit below 21,000 | EVM action identity or envelope is invalid |
| a signature already in the file | contract preview requires an unsigned config; use plan to verify an approval |
| an approval key without `0x` | contract phase approval key is invalid |
| a `ws://` route | root submission requires one canonical credential-free http(s) route |
| `http://` with a host name | submission hostname route requires canonical HTTPS and a fully qualified host |
| `https://` without a pin, or a pin on `http://` | root submission tls pin is absent or attached to plaintext http |

`preview` admitted each of these, so only the comparison of section 7 catches
them:

- another nonzero escrow hotkey in place of O5;
- another demand fee that is a nonzero multiple of 10^9, in place of O12;
- another burn cap, when `value_wei` follows it;
- another nonzero owner, v1 coordinator or auditor, when none is the deployer;
  a second auditor at quorum 1; the owner as the auditor;
- a wrong deployer or nonce used consistently, since every address is derived
  from them and `preview` reads no chain;
- a wrong chain, runtime version, hash, route or start checkpoint that is well
  formed. The online steps compare these with the chain before each send.
  Step 8.3 of the runbook quotes the refusal for a runtime difference, "EVM
  current runtime differs from independent phase approval"; this recipe's
  tests did not trigger it;
- two harmless variants: a sender in mixed case, and a five-action plan whose
  `maximum_total_wei` still counts the sixth action's gas, since the total is
  only a ceiling.

## 9. When an input changes

Any change needs a new preview, and after signing a new approval, because the
approval signs the whole plan.

Change the value in section 2, or replace the file, then rerun:

- **a new nonce or deployer:** sections 2, 4, 5, 6 and 7. Every predicted
  address, `SELF_COLDKEY`, and the data of actions 0, 3 and 4 change;
- **a new snapshot, for a fresh start checkpoint or another runtime:**
  sections 3, 6 and 7;
- **a new release file:** sections 3, 5, 6 and 7;
- **O5, O6, O8, O9, O10 or O12:** sections 2, 5, 6 and 7;
- **O7 or O11 only:** sections 2, 6 and 7.
