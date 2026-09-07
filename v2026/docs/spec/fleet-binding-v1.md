# Fleet binding v1

Domain: the 26 ASCII bytes `urnetwork/fleet-binding/v1`. The signed payload is
the domain followed without padding by `chain_id:u64be`, `netuid:u16be`,
`coordinator:20`, `fleet_id:32`, `hotkey:32`, `client_id:16`, `client_key:32`,
`generation:u64be`, `valid_from_epoch:u64be`, `valid_to_epoch:u64be`, and
`commitment_hash:32`. Both signatures cover `keccak256(payload)`.

The client signs with Ed25519 and the fleet hotkey signs with sr25519 using the
Substrate signing context. The coordinator additionally requires a live UID and
an exact commitment-pallet match. Membership begins no earlier than the next
epoch, expires at `valid_to_epoch`, and a strictly larger generation is required
for replacement or revocation. A client can belong to at most one fleet in any
epoch; many clients can name the same fleet and hotkey.

The machine-readable golden vector is `protocol/testdata/fleet-binding-v1.json` and is
consumed by both Go and Solidity tests.
