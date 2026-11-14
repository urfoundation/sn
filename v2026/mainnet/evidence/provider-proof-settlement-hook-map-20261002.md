# Provider proof and settlement producer map

Read-only source review at SN main `16304b2e`, provider candidate `98fcff06`
and frozen Server `10a8f4d8`. This identifies implementation work; it does not
qualify a new producer or change the UNKNOWN fields. SDK fields below were
cross-checked in `sdk/api_sn.go`; the consuming branches still require exact
module-pinned tests. Existing chain results remain RPC assertions until the
independent finality/source gate is satisfied.

| Domain | Existing source boundary | What it establishes | Next required producer/observer join |
| --- | --- | --- | --- |
| Transport readiness | `miner/provider_progress.go`, `protocol/provider_progress.go` in `98fcff06` | Instance/sequence, expected slot/configuration, current connected and registered device | Preserve this contract; never promote it into payout proof or payment. |
| Pool inclusion proof | `miner/claim_daemon.go: claimCalldata`; `sdk/api_sn.go: SnPoolClaimResult` | Merkle leaf/proof against the advertised root, with epoch/noId/coldkey/share | Publish a bounded observation tied to independently expected chain/vault/noId/coldkey/epoch and exact artifact/root; advertising alone is insufficient. |
| Finalized entitlement and leaf-claimed state | `miner/claim_daemon.go: queryClaimedFinalized`; `miner/claim_finality.go` | Exact finalized EVM identity, entitlement root comparison, leafClaimed and closing identity check | Retain identity with the observation; make unavailable/mismatch/window-pending distinct. Returning true is not an amount or a receipt for this daemon's transaction. |
| Signed claim outcome | `miner/claim_signed_outcome.go: authenticateSignedClaim/verifySignedClaimReceipt/reconcileSignedClaim`; `miner/claim_daemon.go: finalizedClaimReceipt/recordFinalizedClaimReceipt` | Exact signed transaction intent and one matching Claimed event, canonical finalized receipt and retained block/log hash | Export only after durable queue acknowledgement; independently compare original authority and receipt. Retain amount/identity if the status projection needs them; current queue stores only receipt-log hash, not a payment census. |
| Queue retry and durable progress | `miner/claim_queue_poll.go: pollClaimQueue`, `miner/claim_queue_run.go` | Per-entry retry/uncertain status, separate reconciliation counts, bounded historical work, flush before terminal acknowledgement | Give the existing single daemon owner a bounded public projection; a monitor must not open a second writer or re-create missing history. Join any publisher and do not expose keys, JWTs or raw diagnostic text. |
| Operator publication and mirror progress | `server/stmonitor/snapshot.go: Snapshot/Read`; `mainnet/monitor_operator.go` | Local database assertion: deployment domain, mirror cursor, intents, attempts, uncertain liabilities and publication backlog | Use to diagnose stalls and compare expected operators; it does not establish native finality, contract roots, reserve amounts or complete signed liabilities. |
| Contract carry, entitlement and payment | `evm/src/STSettlementVault.sol: carry/entitlement/claim`, RootMissed and Claimed events | On-chain transition semantics when independently observed at exact identities | Add separate expected-vault/operator/epoch observations for capture → RootMissed carry → entitlement → claims/residue; retain zero capture with carried payout as a valid case. Do not infer payment from a queue label. |
| Native 10/90 economics | `mainnet/economic_emission_observe.go: observeEconomicEmission`; `mainnet/economic_emission_state.go` | Bounded native observation with explicit unresolved denominator/outcome/source blockers | Keep conservation/recycle/fee and provider payment evidence separate; a complete read range is not verified economics. |

Two materially different `finalized` queue routes exist. A signed entry passes
exact-transaction receipt reconciliation. An unsigned entry can return finalized
from leafClaimed state without a receipt for this daemon, or no-claim from an API
zero payout. A common status string therefore cannot authorize a common paid
amount or signed-outcome metric. Typed projected states must retain that distinction.

Expected domains belong to reviewed configuration, independently of the producer:
provider group/configuration hash and member slot/generation; operator deployment
and publication domain; network noId and payout coldkey; genesis/EVM chain/vault;
epoch/artifact/root; relayer and original signed transaction where applicable.
A device ClientId is not a pool coldkey. Shared network pools require a reviewed
slot-to-pool mapping; duplicated slots must not duplicate the pool's payment.

Implement incrementally on actual owners: first durable public queue projection,
then independent proof/entitlement comparison, then contract carry/reserve/payment
and native economic domains. Preserve prior v1 transport observations and define
schema migration explicitly. Required causal controls include publication before
fsync, stale/foreign expected pool, same queue status with different authority,
zero payout versus signed uncertain outcome, mismatched root, stale/replaced
checkpoint, restart sequence, closing finality failure and transient read outage.
Neither a reported proof nor a healthy monitor authorizes automatic spending;
repair envelopes remain separately reviewed and bounded.


## Accepted claim differs from transferred payment

A [source-bound contract trace](claim-payment-semantics-20261002.json) refines
the signed-outcome row above. `claim` emits `Claimed` and adds accepted liability
to `claimCredit[coldkey]`; its successful transaction can then emit
`ClaimPaymentDeferred`. The existing exact signed receipt verifier authenticates
`Claimed`, not transfer completion. Preserve that valid finalized-claim outcome
and report payment separately. `ClaimPaid` follows a successful nested transfer
and includes the entire accumulated coldkey credit, potentially from multiple
epochs/operators. Its amount cannot be assigned wholesale to the current pool
or repeated for each provider slot.

Use distinct accepted amount, retained unpaid credit and aggregate payment
observations. Per-pool paid attribution needs complete independent credit
history and an explicit allocation rule. Existing minimum-threshold and recovery
tests assert retained and aggregated credit; this review did not execute them.
New producer/monitor tests must exercise deferred payment in a successful claim,
later aggregate payment larger than the current claim, entitlement expiry with
accepted credit, and unavailable historical attribution.
