# First on-chain evidence from renewal 2

This is a two-transaction sample from the active approved renewal plan
`0x09ac683bae8bf99362bfc427776987fce951db58b71b3f01966236abbf7c91f1`.
It establishes actual chain activity, not whole-renewal or campaign completion.
All observations use `http://192.168.1.162:9944`; `independent_rpc=false`.
The recorded genesis is `0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`
and EVM chain ID is 945.

- Native action `fleet.renew.2.5.commitment`: transaction
  `0x0765b51047adebbb53d91a74bcca26ad21423025b2ba099ae4d100bad7dc44f0`,
  zero-based extrinsic index 14 of 17 in block 8,012,725, hash
  `0xb60dd5f11cd3dbda3294ad76dbfc2c9815ffb56772d518edf9f062f4a20412de`.
  Decoding the complete RPC extrinsic hex and hashing it with GNU
  `b2sum -l 256` reproduces that transaction hash. The full block and all 17
  extrinsic hashes are retained. A separate height lookup matches its block hash.
- EVM action `fleet.renew.2.10.mirror`: transaction
  `0x1a845bceaec25bed3ee3a7197e1b39f10afe6f6c1dbc91fc8181391a5b45dc0f`,
  successful receipt status `0x1` in block 8,012,728, hash
  `0xeed0240f6c12bd39df88013773e7b08d276e4d4a51df225ec5955c3091ffdba7`.
  Its destination is the coordinator
  `0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8`. The canonical EVM block lookup
  matches both the receipt block hash and transaction membership.

The recorded native finalized head is block 8,012,747, hash
`0x53fb30a2f580420f7d4021574a0ccda4963e2a7b077856439f36ccf04a3a0c09`,
above both inclusion heights. `requests.json` / `responses.json` and
`finality.requests.json` / `finality.responses.json` contain the eight actual
RPC requests and responses, with command exits and timestamps. No request
submitted a transaction. The finalized-head observations retain their original
scope and do not claim an independent second node.

The exact raw responses are copied from private capture
`temp/sn-infrastructure-release-integration-20260914/fleet-renewal-first-onchain-20260915`;
its original 14-file manifest SHA-256 is
`0605b0ced21a41a8ac8a07da839ec15e098a18a27f2b43cd0d7546551df5f9d7`.
The public manifest additionally covers this README. Verify the copied files
with `sha256sum --check --strict SHA256SUMS` in this directory. Replaying the
recorded RPC requests requires LAN access and historical state retention;
the moving finalized-head response will naturally advance.
