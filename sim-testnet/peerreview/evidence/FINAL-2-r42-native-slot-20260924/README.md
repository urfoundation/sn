# R42 validator 2 native source-slot continuity

`validator-2-slot-proof.json` is a read-only capture at finalized testnet block
8,078,196 (`0xd2c26fad9af8046b103ac27dd75547462a3d1b56409b3e79e02595b8880d09ea`)
from the LAN RPC at `192.168.1.162:9944`. SHA-256:
`8ffd40956a1e7b7d2d41ad913eb03cd1a2730ded94f2dbbe99dc086063a24413`.
It includes the exact `state_getStorage` keys, block hash, and returned values
for `Commitments.CommitmentOf` and `Commitments.LastCommitment`, plus the public
hotkey and the prior local signed intent metadata. A reviewer can repeat the
listed RPC calls against an archive endpoint with the same chain genesis.

Both finalized slot values match the prior applied intent's source commitment
hash and finalized block 7,987,774. The prior intent file SHA-256 is
`0de9867f8c0b699954e9ef25c66759b0390214ddaf8313d9ca7f42df63e27727`.
The active generation-1 intent file was absent at capture. This explains the
live validator's `native slot belongs to another role or unretained write`
refusal: its current source view cannot authenticate the retained predecessor
proof. The capture does not by itself authorize accepting an occupied slot;
the predecessor's signed role and exact hash/block must be verified in the
source-generation handoff before any production fix.
