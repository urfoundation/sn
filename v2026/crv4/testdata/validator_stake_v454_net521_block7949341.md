# Captured runtime454 stake response

The adjacent JSON is the actual response body from a read-only RPC at
2026-09-06 22:59:32 UTC, with a terminating newline added for the fixture.
No wallet, configuration secrets or transaction were used.

- Endpoint: `https://test.chain.opentensor.ai`
- Method: `state_call`
- Runtime API: `SubnetInfoRuntimeApi_get_selective_metagraph`
- SCALE input: `0x09021400001e00340039004500`
- Input meaning: netuid521; indexes0(netuid),30(num_uids),52(hotkeys),
  57(validator_permit),69(total_stake).
- Native block7949341:
  `0x7416115b9fdeebbe8522e8f76ae7fd562dfd2e9f731614cbe5ed26469dc6c1ce`
- Decoded response byte count:8821.
- UID0 hotkey:
  `a20080205804bba26541104cbac4def8a0eba26f7a86f6f1e7d16ad37f0cd658`
- Complete census:256; selected permit:true.
- UID0 compact stake bytes: `0b9c2253bbf85f`, meaning the unsigned integer
  `0x5ff8bb53229c` =105521899315868 rao after runtime fixed-point truncation.

The block/hotkey/permit/census independently match the earlier full WebSocket
identity observation in `sn-validator-identity-live-v1-WTWJOR`. That observation
authenticated node-subtensor/454/1/1 with code hash
`0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef`
and metadata hash
`0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702`.

The pinned source is Subtensor commit
`14cde6410fe8ec81a940e290c56f94a632a0988d`. The runtime API dispatch is in
`runtime/src/lib.rs`, and the layout/selection/calculation are in
`pallets/subtensor/src/rpc_info/metagraph.rs`,
`pallets/subtensor/src/staking/stake_utils.rs`,
`pallets/subtensor/src/epoch/math.rs` and
`pallets/subtensor/src/lib.rs` (`check_weights_min_stake`).

The fixture is a historical codec regression, not fresh validator eligibility,
cryptographic storage proof, activation inclusion or a transaction receipt.
