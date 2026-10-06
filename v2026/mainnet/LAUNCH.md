# SN25 launch handoff

Prepared 2026-10-06. Use this document in the reviewed SN checkout alongside
[MAINNET.md](MAINNET.md). `mainnet/` is the corrected directory name; do not
recreate `mainnnet/`.

The task is to produce a real, reviewed deployment and then publish its identity
in `config/main/sn.yml`. These configuration fields do not deploy contracts or
prove readiness. At preparation time, activation is `blocked` and all five
fields below are empty. This document does not authorize new transactions or
assert that installation, hardware signing or live readiness has succeeded.

## The five fields, in plain language

| Field | Meaning | How to obtain it |
| --- | --- | --- |
| `deployment_id` | A stable name for this installation, such as `sn25-mainnet-20261006-01`. This is an example, not a selected deployment. | Choose once, before preparing signed plans. Use exactly the same bytes throughout. |
| `coordinator` | The public EVM address of the coordinator **proxy** that services call. | Take `coordinator_proxy_address` from the approved graph, then prove its finalized creation, implementation and initialized state. |
| `settlement_vault` | The public EVM address of the immutable settlement contract that accounts for provider entitlements and claims. | Take `vault_address` from the graph, then prove its finalized creation, runtime and coordinator binding. |
| `policy_hash` | The fingerprint of the validated protocol rules installed for this deployment. | Load the approved policy with `protocol.LoadPolicy`, then call `Policy.HashHex()`. Match it to initialization and all producer configurations. |
| `readiness_sha256` | The fingerprint of the exact final reviewed launch receipt file. | Assemble and independently review the evidence manifest described below; hash its final bytes. |

Choosing a name or calculating a hash requires no blockchain signature. Creating
contracts, installing policy, anchoring evidence and configuring on-chain roles
do require their respective authorities. A predicted address is useful for
planning; it is not evidence of deployment.

`deployment_id` accepts 1–128 ASCII letters, digits, hyphens and underscores. The
evidence contract binds SHA-256 of the exact label bytes. Separately, Server's
durable payout namespace is `chain_id:lowercase(coordinator)`; changing the label
does not reset journals, liabilities or progress.

Both contract addresses are nonzero 20-byte `0x` EVM addresses. The coordinator
implementation is a different address and must not populate `coordinator`.
Neither field is the native SS58 reserve wallet.

`policy_hash` is `0x` followed by 64 lowercase hex characters: SHA-256 of the
canonical typed policy JSON. It is **not** the policy YAML file hash, a Keccak
hash, the treasury economic policy hash or a bootstrap plan hash.

`readiness_sha256` is 64 hex characters **without** `0x` or `sha256:`. Current
Server code checks its syntax and equality with the operator's configured
`launch_readiness_sha256`; it does not load or authenticate a receipt file. The
local agent and reviewer therefore must perform the evidence review themselves.

## Network and authority inputs

The current public schedule selects mainnet EVM chain ID **964**, netuid **25**,
and native genesis:

```text
0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03
```

Independently authenticate this identity against the approved mainnet route
before preparing signatures. Use the authorized Rao public mainnet RPC while
Snow synchronizes, then adopt Snow only after verifying its network and route.
Obtain the actual approved URL locally; this document does not invent one.
The previous LAN testnet endpoint is not a mainnet fallback. A fresh runtime
number or an RPC response alone does not approve runtime signing authority. The
v470 artifact exception was planning-only.

Collect these inputs before requesting signatures:

- Reviewed source/build locks, exact binary and contract artifact hashes.
- Selected deployment label, validated mainnet protocol policy and all typed
  chain/contract configurations. Fresh passive-root composition uses
  `urnetwork-mainnet-bootstrap-chain-config-v4`.
- Independent Ed25519 approval public keys and separately approved runtime,
  metadata, network, submission route, attempt and value budgets.
- EVM deployer, governance Safe, guardian and commitment oracle; these roles
  are distinct. Actual sender nonces, bounded fees and validity windows.
- Each operator's distinct EVM `depositSigner` and `rootSigner`, native validator
  identities, signed producer configurations and durable service locations.
- Dedicated native treasury recipient hotkeys and observed UID generations.
- Host and owner-local durable-volume declarations and their exact hashes.

Private keys belong in their existing custody: owners' Ledger devices and
operators' secrets vaults. Request packages contain public artifacts, exact
messages and reviewed pins, never seed phrases or copied keys.

## Who signs what

| Authority | Request | Result |
| --- | --- | --- |
| Existing SN25 owner, on their own Ledger | Approved native owner trim request, using the existing derived account and reviewed Polkadot app/metadata | Public signed native reply; owner needs no Snow access |
| Independent Ed25519 approver | Exact bytes emitted by plan previews for the specified approval domain | Approval envelope binding the exact plan, not transaction custody |
| EVM deployer | Exact approved EIP-1559 creation/link transaction | Binary signed chain-964 transaction |
| Coordinator governance Safe owners | Exact evidence-anchor Safe EIP-712 transaction | Accepted ordered Safe signatures |
| Safe relayer | Exact outer EIP-1559 `execTransaction` transaction | Binary signed relayer transaction |
| Operator signers | Their separately approved deposit and payout-root operations | Role-specific EVM signatures |

The Ledger Polkadot owner path is not an Ethereum signing interface. Establish
EVM signer custody separately. `STCoordinator.rootSigner` signs operator payout
roots and is unrelated to the native netuid-0 root validator. The selected
passive native root strategy does not require periodic `SetRootWeights` signing;
its registration and readiness prerequisites still apply.

The receive-only `ur-reserve` wallet signs nothing for receipt of emissions:

```text
5CcHGEqKK3RXeEA2sVycHQAQGrqsyhWaYu9FjGtDVN6nwMwR
AccountId32: 0x1815103f41a8d1e24c55d380c6f843fb36d715b4322a4e4f02bff36dfe74a410
```

Do not request reserve spending, gas payment or multisig signing. This native
wallet is distinct from the immutable EVM `STReserveSink`.

## Execution order and parallel work

Prepare policy, owner-local signing prerequisites, treasury registration plans,
operator configurations and evidence packaging in parallel. Do not parallelize
nonce-dependent contract sends. Keep services from populating contract storage
until installation readback has completed.

1. Freeze the reviewed source/artifacts, label, policy, identities, route and
   durable custody; construct and review typed plans. Complete fresh storage
   preparation, including the trim snapshot, then run the approved offline
   `bootstrap-chain apply` to establish original journals before `trim-plan`.
2. Perform approved best-effort native owner trim and role/treasury preparation.
   Retain residual native registrations honestly; owner keys cannot guarantee a
   literal full native reset. Do not claim otherwise.
3. Install the eight-action contract graph sequentially, retaining every receipt.
4. Perform the separately approved Safe evidence-anchor successor and complete
   pristine installation readback **before operator/service population**.
5. Configure and verify operator/validator roles, native treasury routing,
   database earning-boundary preparation and service dependencies.
6. Assemble the final receipt, obtain review, hash it, publish matching public and
   operator identity fields, then verify actual service adoption.

Treasury setup can be a long prerequisite: the retained runtime473 owner-coldkey
transfer path has a 36,000-block delay. Observe the actual runtime and selected
path; do not promise that this can be completed in an hour. See
[TREASURY-RECEIVE-SETUP.md](TREASURY-RECEIVE-SETUP.md).

### Native owner requests: portable, owner-local

Follow [OWNER-SIGNING.md](OWNER-SIGNING.md) and
[OWNER-CUSTODY-PREPARATION.md](OWNER-CUSTODY-PREPARATION.md) for the full commands,
SDK/platform qualification, metadata14/15, RFC78 proof, existing account/path,
app version and independently authenticated pins.

After fresh storage preparation and offline `bootstrap-chain apply` have
established the original custody, the host sequence is `bootstrap-chain trim-plan`,
attach the independent exact
execution approval, `trim-apply`, then `trim-export`. Send the owner the portable
request and independently authenticated request content hash, genesis, existing
owner AccountId32 and approval key. The owner runs `owner-signing inspect`,
`owner-signing sign`, then `owner-signing verify` locally. Return the original
public reply and its file SHA-256. The host uses `trim-import-reply`, prepares and
approves `trim-submit-plan`, reconciles, then uses `trim-submit` under that
separate submission policy.

The request content hash and reply file hash are different pins. Fresh
best-effort custody uses its own approved domain; never relabel an existing
strict journal. Missing or uncertain signing outcomes require original-custody
recovery, not deleting journals or issuing another signature casually.

### Contract graph: exact action requests

Build the CLI from the reviewed checkout:

```bash
go build -o /absolute/path/sn-mainnet ./mainnet
```

The examples below use Bash. Set every variable from reviewed artifacts;
placeholders are not runnable launch configuration.

```bash
SN_MAINNET=/absolute/path/sn-mainnet
REVIEW_DIR=/absolute/private/review
RUN_DIR=/absolute/private/original-custody
HOST=(
  --durable-volumes "$HOST_DECLARATION"
  --durable-volumes-sha256 "$HOST_DECLARATION_SHA256"
)

"$SN_MAINNET" bootstrap-contracts preview \
  --config "$CONTRACT_DRAFT" --action evidence-create \
  > "$REVIEW_DIR/contracts-preview.json"

jq -r '.approval_signing_message_hex' "$REVIEW_DIR/contracts-preview.json" |
  xxd -r -p > "$REVIEW_DIR/contract-approval-message.bin"
```

The preview accepts the unsigned
`urnetwork-mainnet-contract-phase-config-v1` draft. Ask the independent Ed25519
approver to sign the **decoded message bytes**, not the displayed hex text or
SHA-256. Insert the 128-character unprefixed signature into
`approval_signature_ed25519`, preserving the complete plan and independently
pinned approval public key. The domain is
`urnetwork-mainnet-contract-phase-approval-v1`, followed by NUL and the exact
compact typed plan JSON. Do not reconstruct signing bytes with another JSON
serializer.

```bash
"$SN_MAINNET" bootstrap-contracts plan \
  --config "$CONTRACT_CONFIG" --action evidence-create
"$SN_MAINNET" bootstrap-chain plan --config "$CHAIN_CONFIG"
"$SN_MAINNET" bootstrap-chain contract-role-plan --config "$CHAIN_CONFIG"
"$SN_MAINNET" bootstrap-chain apply \
  --config "$CHAIN_CONFIG" --run-dir "$RUN_DIR" \
  --accept-plan-hash "$CHAIN_PLAN_HASH" "${HOST[@]}"
```

Review plan output and the exact typed fields in
[BOOTSTRAP-CONTRACTS.md](BOOTSTRAP-CONTRACTS.md),
[evm_phase.go](evm_phase.go) and [BOOTSTRAP-CHAIN.md](BOOTSTRAP-CHAIN.md).
`CHAIN_PLAN_HASH` and `CONTRACT_PLAN_HASH` are distinct accepted objects.

| Order | Action | Effect |
| --- | --- | --- |
| 0 | `reserve-create` | Create immutable EVM deposit sink |
| 1 | `vault-create` | Create settlement vault |
| 2 | `coordinator-create` | Create coordinator implementation |
| 3 | `escrow-register` | Register approved vault escrow |
| 4 | `proxy-create` | Create proxy and atomically initialize ownership/policy/links |
| 5 | `reserve-link` | Bind sink recorder to proxy |
| 6 | `vault-link` | Bind vault coordinator to proxy |
| 7 | `evidence-create` | Create validator evidence contract, still unanchored |

All eight use the approved deployer and consecutive nonces starting at `n`.
For one action at a time, after its predecessors are canonically complete:

```bash
"$SN_MAINNET" bootstrap-contracts apply \
  --config "$CONTRACT_CONFIG" --action "$ACTION" \
  --run-dir "$RUN_DIR" --accept-plan-hash "$CONTRACT_PLAN_HASH" \
  "${HOST[@]}" > "$ACTION_REQUEST"

# External EVM signer signs the exact unsigned_transaction from ACTION_REQUEST.
# Return canonical binary signed EIP-1559 bytes, not a JSON or hex-text wrapper.
"$SN_MAINNET" bootstrap-contracts resume \
  --config "$CONTRACT_CONFIG" --action "$ACTION" \
  --run-dir "$RUN_DIR" --accept-plan-hash "$CONTRACT_PLAN_HASH" \
  --signed-transaction "$SIGNED_BIN" \
  --signed-transaction-hash "$SIGNED_BIN_SHA256" "${HOST[@]}"

# Reconcile retained bytes against the approved route before permitting a send.
"$SN_MAINNET" bootstrap-contracts resume \
  --config "$CONTRACT_CONFIG" --action "$ACTION" \
  --run-dir "$RUN_DIR" --accept-plan-hash "$CONTRACT_PLAN_HASH" \
  --online "${HOST[@]}"

# Under the exact scoped authority, permit one durably counted send.
"$SN_MAINNET" bootstrap-contracts resume \
  --config "$CONTRACT_CONFIG" --action "$ACTION" \
  --run-dir "$RUN_DIR" --accept-plan-hash "$CONTRACT_PLAN_HASH" \
  --online --submit "${HOST[@]}"
```

File pins here use `sha256:<digest>`. Import validates sender, chain, nonce,
target, calldata, value and fee envelope. A timeout or lost reply is not proof
that nothing was broadcast: reconcile the original journal and signed bytes.
Never repeat completed CREATEs or change accepted graph/nonces to escape an
ambiguous outcome.

### Safe evidence anchor and installation readback

`evidence-anchor` is **not** an accepted `bootstrap-contracts --action`. It is a
separate approved successor calling `fixValidatorEvidence(evidence)` through the
coordinator-owner Safe. Do not expand the original eight-attempt phase cap.

Follow [BOOTSTRAP-SUCCESSOR-EXECUTION.md](BOOTSTRAP-SUCCESSOR-EXECUTION.md),
[BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md)
and [BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CUSTODY.md](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CUSTODY.md).
The sequence is successor plan/preview, independent preparation approval,
prepare, Safe review, Safe-owner signatures, relayer signing, execution preview,
independent execution approval, claim, canonical authorization, reconcile,
scoped submit and installation readback.

The current profile requires an independently verified 2-of-3 Safe with three
sorted distinct owners, no modules, guard or fallback handler. Two actual owners
sign the emitted EIP-712 transaction; the accepted concatenation is 130 binary
bytes. The relayer then signs its exact outer transaction. Native review expiry
does not revoke Safe signatures. Some planning/review commands intentionally
return exit 3 with unresolved-authority reports; inspect the report rather than
interpreting that exit as blanket success.

Complete-history Safe authentication is not implemented. The supported bounded
alternative requires a separately signed current-only proposal v1 nested in a
separately signed acceptance revision v2, canonical authorization and the exact
retained acceptance object hash via
`--accept-safe-current-policy` for the selected submission. Importing
`--safe-current-revision` alone does not enable sending. Record this narrower
assurance explicitly; never label it complete-history evidence.

Use `bootstrap-chain contract-successor-execution-readback` with the exact
original config/run/plan, successor/Safe/execution requests, execution approval
and pins, `--online`, canonical approval and any retained runtime/current-policy
authority. For the current-only route, also supply
`--accept-safe-current-policy "$CURRENT_POLICY_V2_OBJECT_HASH"` on readback;
retaining or importing the revision alone does not select this capability.
Readback refuses `--submit`. Preserve its
`urnetwork-mainnet-bootstrap-contract-installation-v1` output, including
`installation_identity_hash`, finality and authority policy. Its
`activation_ready: false` is not an overall launch approval. Complete pristine
installation readback before allowing operator or service writes.

## Produce the protocol policy hash

Use approved mainnet policy YAML with a `policy:` wrapper and schema
`urnetwork-policy-v1`; do not copy accelerated testnet settings. Mainnet steady
production cadence is 50,400 blocks. A steady initial policy has
`after_accelerated_epochs: 0` and matching initial/production windows. Economic
parameters and signer approvals still need their actual reviewed values.

There is no dedicated `policy-hash` CLI. Save this helper outside repository
package directories, for example `$REVIEW_DIR/policyhash.go`, then run it from
the reviewed SN module:

```go
package main

import (
    "fmt"
    "os"

    "github.com/urfoundation/sn/protocol"
)

func main() {
    if len(os.Args) != 2 {
        fmt.Fprintln(os.Stderr, "usage: policyhash /absolute/path/policy.yml")
        os.Exit(2)
    }
    policy, err := protocol.LoadPolicy(os.Args[1])
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    if policy.NetworkProfile != "mainnet" {
        fmt.Fprintln(os.Stderr, "mainnet policy required")
        os.Exit(1)
    }
    hash, err := policy.HashHex()
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    fmt.Println(hash)
}
```

```bash
go run "$REVIEW_DIR/policyhash.go" "$POLICY_FILE"
```

Retain the policy YAML and canonical bytes from `Policy.CanonicalBytes()`.
Compare the digest with approved proxy initialization, on-chain policy and both
signed producer configurations. Pretty-printing canonical JSON changes file
bytes; the protocol hash uses the original compact bytes.

## Native reserve and service prerequisites

The selected launch economics are **10% of native miner allocation to providers
and 90% to the native reserve**, with equal weight for paid/free completed
traffic. This supersedes the earlier owner-recycling proposal.

At least two ordinary registered recipient hotkeys must be owned by the reserve
coldkey, outside the subnet-owner hotkey set and provider roles. Retain observed
UID/hotkey/coldkey/registration generations; initially split the reserve row
approximately 45% each because of the native per-weight cap. An empty recipient
list identifies a destination but cannot establish routing readiness.

Keep the reserve public configuration and operator secrets in their selected
`vault/main/sn.yml` layout. Extract a **separate public-only**
`urnetwork-native-treasury-destination-v1` descriptor for `treasury describe`,
`treasury observe` and `treasury policy-plan`. Never pass the combined secrets
file to that strict descriptor reader. Policy-plan output is unsigned and
`approved: false`; independent economic approval is still needed. See
[TREASURY-EMISSIONS.md](TREASURY-EMISSIONS.md).

Provision the separate epoch roster authority and producer described in
[PAYOUT-ROSTER.md](PAYOUT-ROSTER.md). The SN `cli/payoutroster` service signs the
explicitly complete roster, including v2 network-wallet heads when present,
and publishes its retained original to the operator's provider-work API. Its
key custody is separate from the payout artifact worker; match its public
signer/domain/request-key pins to Server's `provider_work.yml`. Supplying a
payout artifact signer or installing contracts does not supply this service.
Verify reviewed epoch input delivery, durable request/signature/receipt storage
and publication adoption as part of operator readiness. Keep unmapped providers
explicit; never infer a complete roster from observed usage.

Verify actual operator registration, distinct deposit/root signers, funds,
validator roles and native treasury routing; dependencies, durable archives,
Postgres/Redis and schema/earnings-boundary preparation; and recovery/status
instrumentation. The bootstrap `readiness` command is a prerequisite observation,
not a final receipt producer:

```bash
"$SN_MAINNET" bootstrap-chain readiness \
  --config "$CHAIN_CONFIG" --run-dir "$RUN_DIR" \
  --accept-plan-hash "$CHAIN_PLAN_HASH" \
  --rpc "$MAINNET_RPC" --retry-window 300s "${HOST[@]}"
```

An exit 0 or `observed-prerequisites` does not assert overall activation. Preserve
reported blockers and actual live adoption evidence separately from local build
and focused-test receipts. Existing code-only evidence is not a mainnet capture.

## Assemble and review the readiness receipt

There is currently **no complete launch-receipt producer or receipt validator**.
The following manifest is a handoff convention introduced by this document, not
an existing tool schema. Store it as `launch-readiness.json` in durable review
storage, with separately retained evidence files and exact hashes.

Include:

- Exact public network/deployment identity and protocol policy hash.
- Source/build/artifact locks and canonical policy artifact reference.
- Original plans, exact approval references and actual finalized transaction
  receipts, native/EVM block hashes and numbers.
- Authenticated proxy/vault/evidence addresses, code/implementation/immutables,
  policy, links, governance and role readbacks; installation identity hash.
- Selected Safe assurance policy and its precise limitations.
- Runtime/metadata/network authority and approved submission-route evidence.
- Actual native roles and reserve recipient census; signed 10/90 treasury policy.
- Operator/validator public configuration projections, dependencies, schema and
  earning-boundary preparation, monitoring and recovery evidence.
- Explicit unresolved blockers/exceptions and independent reviewer decision.

Do not manufacture a `ready` decision from matching strings or passing local
unit tests. Any unresolved launch blocker keeps activation blocked. Any accepted
exception needs its real scope and approval retained; it must not silently
become a successful check.

Avoid a hash cycle: the receipt must not contain its own digest, or a digest of
a final config file that contains that receipt digest. Bind a precisely defined
public config projection **excluding `launch_readiness_sha256`** (and excluding
secrets). After publication, retain final adopted file hashes, binary hashes and
service instances in a separate adoption record referencing the receipt.

Freeze the exact reviewed receipt bytes, obtain the independent approval over
those bytes or their expressly agreed digest, then calculate:

```bash
sha256sum "$REVIEW_DIR/launch-readiness.json"
```

Copy only the 64-hex digest into `readiness_sha256`. Preserve the original bytes
and reviewer approval. Editing or reserializing the receipt afterward requires
new review and a new digest; do not silently overwrite the accepted file.

## Publish the matching identities

Only after the preceding review, update the existing public schedule. Preserve
its attribution and pre-cutoff obligation policy. This is a field illustration,
not a ready-to-apply configuration:

```yaml
mainnet:
  profile: mainnet
  chain_id: 964
  genesis_hash: "0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03"
  netuid: 25
  activation: reviewed
  deployment_id: "<exact selected label>"
  coordinator: "0x<actual authenticated proxy>"
  settlement_vault: "0x<actual authenticated vault>"
  policy_hash: "0x<canonical protocol policy hash>"
  readiness_sha256: "<64-hex exact receipt digest>"
```

Match these existing unprefixed operator fields in `vault/main/st.yml`:

```yaml
deployment_id: "<same label>"
coordinator_address: "0x<same proxy>"
settlement_vault_address: "0x<same vault>"
policy_hash: "0x<same canonical policy hash>"
launch_readiness_sha256: "<same receipt digest>"
```

This is not the full operator config: its existing enable/profile/network,
operator ID, deployment block, signers and other required settings still apply.
Do not print the secrets file into logs or the handoff. Validate public
projections and signed validator/operator configurations separately.

Run `bringyourctl sn-transition-status` with the selected main environment; it
reports configuration/declaration matching, not proof of running services or
chain readiness. Pin the actual schedule file SHA-256 for the approved database
migration (`bringyourctl db migrate --sn-schedule-sha256=HEX`, or the all-in-one
operator's `all db migrate` equivalent). Retain migration results and verify
actual worker/service adoption; publishing config alone does not perform these
steps. Follow the operator guide for Warp layout and `WARP_ENV=main`.

The October 6 00:00 UTC cutoff attributes **new earnings** to mainnet. It is not
the last USDC transfer date: pre-cutoff obligations can finish paying later.
Blocked activation must not convert liabilities, double-pay providers or enable
new post-cutoff USDC fallback.

## Return package for the next agent

Return the selected identity values, receipt file/digest and reviewer approval;
public source/build and config projections; all original request/reply and
approval references; finalized receipts and installation readback; treasury and
role evidence; migration/adoption status; and an explicit remaining-blocker
list. Include original durable journal paths and recovery instructions. Never
include private keys.

For every interrupted action, report whether it is unsigned, signed, submitted,
ambiguous or finalized. Retry transient reads using retained checkpoints; do
not redo completed actions. If a signed or broadcast outcome is unknown,
reconcile the original custody before requesting another signature.

The field definitions are implemented in
[Server's transition gate](../../server/provider_payout_transition.go),
[operator identity matching](../../server/controller/st_payout_transition.go),
[protocol policy canonicalization](../protocol/policy.go),
[contract preview](evm_phase_preview.go) and
[evidence deployment domain](evm_evidence_create.go). Use these current sources
when older planning prose disagrees.
