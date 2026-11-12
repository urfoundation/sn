// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ISTSettlementVault2} from "./interfaces/ISTSettlementVault2.sol";
import {IStaking, ISTAKING_ADDRESS} from "./interfaces/stakingV2.sol";
import {IStakingCollateral} from "./interfaces/stakingCollateral.sol";
import {LedgerTree} from "./LedgerTree.sol";
import {Blake2b} from "./lib/Blake2b.sol";

/// @title STCtfCarry
/// @notice The carry contract of one capture the flag pool (CAPTURETHEFLAG.md
/// §4.6). vault2 requires each root's total to equal the pool's funding
/// through its epoch, so a pot waiting for a winner cannot wait inside the
/// pool: each root pays the pots and pending awards to this contract's
/// account, and `recycle` returns them to the pool. Anyone calls it. It claims
/// the account, moves this contract's coldkey's stake from vault2's escrow
/// hotkey onto the CTF pool hotkey, and transfers all of it there to vault2's
/// coldkey, whose position on the pool hotkey the pool's next close captures.
/// `recycledFor` records, for that epoch, how much vault2's position grew, so
/// the ledger can tell returned carry from fresh emission.
///
/// It has no owner, no upgrade and no other entry point. It reads vault2's
/// escrow hotkey, coldkey and netuid and the pool's hotkey once, at creation;
/// vault2 never changes them. Its own coldkey is its address's runtime
/// mapping. Stake reaches it only from vault2's claims and from donations,
/// and leaves it only to vault2's position on the pool hotkey: it cannot be
/// drained or redirected, and it holds stake only until the next recycle,
/// which returns everything it holds. A claim to its coldkey is a transfer
/// from vault2, which the runtime refuses while third parties hold this
/// coldkey's staking-hotkey list at 128 entries without the escrow hotkey
/// (VERSION2-FINAL.md §7.3); the account then stays claimable.
contract STCtfCarry {
    // The staking precompile reads an amount of 2^64 − 1 or more as "the
    // whole position"; every amount passed to it stays below this.
    uint256 private constant WHOLE_POSITION = type(uint64).max;

    /// @notice vault2 and the CTF pool this contract returns carry to.
    ISTSettlementVault2 public immutable vault;
    uint256 public immutable poolId;
    uint16 public immutable netuid;
    bytes32 public immutable escrowHotkey;
    bytes32 public immutable poolHotkey;
    /// @notice vault2's coldkey, which owns the pool hotkey's captured position.
    bytes32 public immutable vaultColdkey;
    /// @notice This contract's coldkey: the carry account's leaf coldkey.
    bytes32 public immutable coldkey;

    /// @notice The growth of vault2's position on the pool hotkey that recycles
    /// made for each epoch: the epoch whose close captures it, the pool's
    /// lastFundedEpoch + 1 at the recycle.
    mapping(uint256 epoch => uint256 amount) public recycledFor;

    // The guard stays in persistent storage: the runtime's EVM keeps a
    // transient write made in a call that reverts (scripts/runtime-rehearsal/README.md).
    bool private _entered;

    event Recycled(uint256 indexed epoch, uint256 amount);

    error InvalidConfiguration();
    error InvalidColdkey();
    error NothingToRecycle();
    error AmountTooLarge();
    error RuntimeAccountingMismatch();
    error Reentrancy();

    modifier nonReentrant() {
        if (_entered) revert Reentrancy();
        _entered = true;
        _;
        _entered = false;
    }

    /// @param vault_ vault2, which must hold pool `poolId_`.
    /// @param poolId_ the CTF pool, 2^32 + the operator's id.
    constructor(ISTSettlementVault2 vault_, uint256 poolId_) {
        if (address(vault_).code.length == 0) revert InvalidConfiguration();
        ISTSettlementVault2.Pool memory pool = vault_.pools(poolId_);
        if (!pool.registered) revert InvalidConfiguration();
        vault = vault_;
        poolId = poolId_;
        netuid = vault_.netuid();
        escrowHotkey = vault_.escrowHotkey();
        vaultColdkey = vault_.selfColdkey();
        poolHotkey = pool.hotkey;
        coldkey = Blake2b.mirror(address(this));
    }

    /// @notice A. Returns the carry to the pool. With an account, whose coldkey
    /// must be this contract's, it first claims it from vault2's root
    /// `rootIndex` with `proof`; an account already claimed in full, as anyone
    /// may claim it, claims nothing. A zero coldkey names no account. Then it
    /// moves all the stake this contract can move on the escrow hotkey onto the
    /// pool hotkey, and transfers all of its position there to vault2's
    /// coldkey. vault2's position grows by `amount`, recorded for `epoch`, the
    /// pool's next unfunded epoch, whose close captures it. Each move must
    /// debit its source exactly and credit its destination the amount or one
    /// rao less, as vault2 requires of its own moves; anything else reverts.
    function recycle(
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata proof
    ) external nonReentrant returns (uint256 epoch, uint256 amount) {
        if (account.coldkey != bytes32(0)) {
            if (account.coldkey != coldkey) {
                revert InvalidColdkey();
            }
            if (vault.claimed(poolId, account.index) < account.cumulative) {
                // vault2 pays this coldkey on its escrow hotkey, or reverts.
                // slither-disable-next-line unused-return
                vault.claim(poolId, rootIndex, account, proof);
            }
        }

        // Everything this coldkey can move on the escrow hotkey, claimed by
        // anyone or donated, moves onto the pool hotkey.
        uint256 escrowBefore = _stake(escrowHotkey, coldkey);
        uint256 held = _unlocked(escrowHotkey, escrowBefore);
        if (held == 0) revert NothingToRecycle();
        if (held >= WHOLE_POSITION) revert AmountTooLarge();
        uint256 poolBefore = _stake(poolHotkey, coldkey);
        IStaking(ISTAKING_ADDRESS).moveStake(escrowHotkey, poolHotkey, uint256(netuid), uint256(netuid), held);
        uint256 poolAfter = _stake(poolHotkey, coldkey);
        if (
            _stake(escrowHotkey, coldkey) + held != escrowBefore || poolAfter > poolBefore + held
                || poolAfter + 1 < poolBefore + held
        ) revert RuntimeAccountingMismatch();

        // All of this coldkey's position on the pool hotkey goes to vault2's,
        // which the pool's next close captures.
        uint256 moving = _unlocked(poolHotkey, poolAfter);
        if (moving >= WHOLE_POSITION) revert AmountTooLarge();
        uint256 vaultBefore = _stake(poolHotkey, vaultColdkey);
        IStaking(ISTAKING_ADDRESS)
            .transferStake(vaultColdkey, poolHotkey, uint256(netuid), uint256(netuid), moving);
        uint256 vaultAfter = _stake(poolHotkey, vaultColdkey);
        if (
            _stake(poolHotkey, coldkey) + moving != poolAfter || vaultAfter > vaultBefore + moving
                || vaultAfter + 1 < vaultBefore + moving
        ) revert RuntimeAccountingMismatch();

        amount = vaultAfter - vaultBefore;
        epoch = vault.lastFundedEpoch(poolId) + 1;
        recycledFor[epoch] += amount;
        emit Recycled(epoch, amount);
    }

    function _stake(bytes32 hotkey, bytes32 owner) private view returns (uint256) {
        return IStaking(ISTAKING_ADDRESS).getStake(hotkey, owner, uint256(netuid));
    }

    // `staked`, this coldkey's whole position on `hotkey`, less any
    // registration collateral locked on it, or 0 below the lock. This contract
    // registers nothing, so nothing should be locked; a move that dug into a
    // lock would fail StakeUnavailable.
    function _unlocked(bytes32 hotkey, uint256 staked) private view returns (uint256) {
        // Only the locked amount limits a move; the other fields say how it drains.
        // slither-disable-next-line unused-return
        (, uint64 locked,,,) =
            IStakingCollateral(ISTAKING_ADDRESS).getMinerCollateral(netuid, hotkey, coldkey);
        return staked > locked ? staked - locked : 0;
    }
}
