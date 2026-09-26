# R47 measured settlement: zero-funded epoch 651

This read-only bundle was collected from the LAN archive RPC
`http://192.168.1.162:9944` while R47 remained live. The finalized head in
`onchain-epoch651-zero-entitlement.json` was block 8,091,280. The
contract is the immutable settlement vault
`0x09d5d7a5c3e94b6ae42b09889a1cee50f970fc5e` on EVM chain 945.

For epoch 651, both `EmissionCaptured` logs at block **8,090,977** report
**0 alpha-rao**. Both `EntitlementFinalized` logs at block **8,091,127** bind
the payout roots and artifact hashes from the separately retained epoch-651
`RootCommitted` receipts, but each entitlement total is **0 alpha-rao**.
Successful `Claimed` receipts sampled at blocks **8,091,156** and **8,091,174**
carry epoch 651 and operators 1 and 2, respectively; each event amount is
**0 alpha-rao**, and neither receipt has a `ClaimPaid` event. The owner
observation at 15:26:59 UTC reports finalized claim statuses. That status
therefore proves execution of the claim path, not a nonzero transfer.

`capture-history-scan.json` preserves every `EmissionCaptured` log returned
for blocks 8,084,000–8,091,280. Of 50 logs, six are nonzero; the latest
nonzero capture is epoch 631 at block **8,084,977**. All **42 later captures**
through epoch 652 at block **8,091,277** report zero. This is a bounded
history scan, not a claim about earlier epochs or future recovery. The vault
source emits `EmissionCaptured(..., 0)` when its pool-hotkey stake observation
is zero. Below-minimum and missed-boundary cases emit different events, so
these zero captures should not be described as dust or deferred funding.

The JSON contains the exact `eth_getLogs` responses, all six sampled
transaction receipts and transactions, EVM block headers, Substrate hashes
at the same heights, finalized-head checkpoint, and owner-observation offset,
SHA-256 and owner hash. EVM and Substrate block hashes are distinct namespaces.
A reviewer can requery the LAN RPC with the encoded filters and transaction
hashes. This bundle proves the observed zero funding and claims; it does not
by itself prove why pool stake was absent. The authenticated native-1690
steering inputs and eligibility audit are analyzed separately for that cause.

The event topic hashes are the Keccak-256 signatures of
`EmissionCaptured(uint256,uint256,bytes32,uint256)`,
`EntitlementFinalized(uint256,uint256,bytes32,bytes32,uint256,uint64)`, and
`Claimed(uint256,uint256,bytes32,uint256,uint256,address)` in
`evm/src/STSettlementVault.sol`. Indexed topics carry epoch and operator;
non-indexed words carry captured amount or entitlement total and claim amount.
