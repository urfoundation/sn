# EVM — UR Subnet release 1.0 contracts

This Foundry project contains the reviewed Subtensor EVM contract set for
`WHITEPAPER.md` 1.0. The supported deployment is deliberately split across three
contracts:

- `STReserveSink` is non-upgradeable one-way conviction custody. It has no
  outbound stake, generic-call, delegatecall, owner, proxy, or upgrade path.
- `STSettlementVault` is non-upgradeable pool-emission custody and Merkle
  settlement. Finalized claims have no pause/admin dependency.
- `STCoordinator` is the UUPS policy, role, root, commitment, and fleet-binding
  layer. It owns neither reserve nor settlement stake.

`src/STSubnet.sol` and its original tests are retained only as pre-1.0 regression
history. Neither `Deploy.s.sol` nor `sim-testnet` installs it.

## Toolchain and dependencies

The load-bearing build pins are Solidity 0.8.24, Cancun, optimizer/via-IR settings
from `foundry.toml`, and Foundry 1.7.1. Vendored libraries under ignored `lib/` are:

| dependency | tag |
|---|---|
| OpenZeppelin contracts | v5.6.1 |
| OpenZeppelin upgradeable | v5.6.1 |
| forge-std | v1.16.2 |

Install those exact tags without local modifications, then run:

```bash
export PATH="$HOME/.foundry/bin:$PATH"
forge fmt --check
forge build --deny warnings --sizes
forge test --summary
../scripts/test-solidity-static.sh
```

After any contract-source or compiler-setting change, refresh the embedded Go
artifacts and bindings from the reviewed Foundry output:

```bash
cd ..
go generate ./sim-testnet
./stabi/generate.sh
```

`sim-testnet/contracts_gen.go`, `stabi/`, and `deploy/testnet/release.lock.yml`
must match the final build exactly. Freeze the release lock only after every
source, generated artifact, and infrastructure change is complete.

## Contract state and value flow

Each contract has a distinct H160-mirrored Substrate coldkey.

1. Every NO has an isolated coordinator-owned deposit hotkey plus a scoped EVM
   deposit signer. The native funding intent stages an exact alpha amount there.
2. `deposit` or `addConviction` checks signer, nonce, deadline, policy caps, and
   available stake. In one EVM transaction it moves the amount to the reserve
   hotkey, transfers it to the immutable sink coldkey, records principal, and
   emits the policy-bound event. Any failed runtime call reverts all accounting.
3. During installation the immutable vault limit-registers its escrow hotkey
   exactly once under its own mapped coldkey. Runtime 447 burns from the funded
   caller mirror, so the vault calls the neuron precompile with zero call value.
   It also owns one pool hotkey per NO. A timely boundary call moves the
   complete realized pool stake to that escrow; a missed boundary defers the
   still-on-pool stake rather than misattributing a multi-epoch delta.
4. The NO commits a root plus canonical artifact hash. Finalization fixes one
   vault entitlement containing captured emission plus same-NO carry. Claims use
   the shared double-hashed OZ Merkle leaf and transfer alpha stake directly to
   the provider coldkey. Expired/unclaimed value remains only that NO's carry.

The executable vault identities are:

```text
totalCaptured = totalPaid + escrowAccounted
escrowAccounted = pendingFunding + outstandingLiability
liveEscrowStake >= escrowAccounted
```

The sink invariant is `liveStake >= principal`; no deployed bytecode can source
that position.

## Fleet binding

`STCoordinator` implements the canonical incremental many-client binding in
`docs/spec/fleet-binding-v1.md`. The signed payload includes chain ID, netuid,
coordinator, fleet ID, hotkey, 16-byte client ID, 32-byte client Ed25519 key,
generation, validity epochs, and commitment hash. Both signatures cover the
payload's Keccak-256 digest:

- client consent is checked by Ed25519 precompile `0x402`;
- hotkey authorization is checked by sr25519 precompile `0x403` using the
  Substrate signing context;
- live UID resolution is checked at Neuron `0x804`; and
- the commitment must equal a fresh finalized pallet observation mirrored by
  the narrowly scoped commitment oracle.

Bindings start no earlier than the next epoch, cannot overlap, can be revoked by
the client for a future epoch, and can be cleaned permissionlessly after expiry,
deregistration, or UID reuse.

## Deployment

`script/Deploy.s.sol` installs the sink, vault, coordinator implementation, and
ERC1967 proxy, limit-registers the vault-owned escrow with the required
runtime-enforced `ST_REGISTRATION_BURN_LIMIT_RAO` (funded at the full ceiling;
the actual burn is charged and surplus is returned), then irreversibly fixes
the proxy as sink recorder
and vault coordinator. It accepts only Bittensor testnet chain 945 or mainnet chain 964.
Testnet requires a dedicated EOA owner. Mainnet calls the standard Safe
`getThreshold()`/`getOwners()` views and refuses deployment unless the owner is
exactly a 2-of-3 Safe with three distinct nonzero owners. Deployer, owner,
guardian, and commitment oracle must also be pairwise distinct.

The release deployment path is the Go `sim-testnet` harness, which embeds and
verifies the exact reviewed artifacts. It predicts all mirror identities, uses
an approved spend-bounded plan, journals signed transactions and receipts, and
checks deployed runtime hashes. Manual `forge script --broadcast` is a developer
diagnostic, not the release procedure.

## Live precompile conformance

`src/probe/STSubnetProbe.sol` is a locked, disposable testnet-only artifact. It is
not part of production custody. `sim-testnet launch` runs its mandatory
`precompile-conformance` scenario before the release scenario can pass:

- replace and restore an exact finalized commitments-pallet value;
- check Blake2 mirror, good/bad Ed25519 and sr25519 KATs, metagraph UID 0,
  live/absent neuron lookup, staking views, and the nominator minimum;
- convert only the approved TAO dust to alpha under the probe coldkey;
- move an exact amount between two live validator hotkeys and back;
- observe positive auto-compounded dividend stake after a native tempo; and
- transfer every probe-attributable alpha unit to a controlled provider
  coldkey, proving exact source/destination deltas and a zero residual.

Every phase is separately journaled and finalized. The content-hashed evidence
is identity-bound to chain, genesis, netuid, config, policy, deployment, and
probe runtime. A failed subcheck or incomplete recovery blocks release 1.0.

## Runtime interfaces

Release code uses Blake2f `0x09`, Ed25519 `0x402`, sr25519 `0x403`, Neuron
`0x804`, and Staking V2 `0x805`. The probe additionally checks Metagraph `0x802`.
Validators submit CRv4 through native Substrate extrinsics, not through the EVM.
Alpha `0x808` and BalanceTransfer `0x800` remain vendored reference interfaces
but are not release value paths.

Runtime ABIs are not assumed stable. A runtime-version change stops automated
writes until metadata, call shapes, all KATs, value-unit conservation, bytecode
compatibility, and the release lock are revalidated.
