// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ISTSettlementVault2} from "./interfaces/ISTSettlementVault2.sol";
import {IStaking, ISTAKING_ADDRESS} from "./interfaces/stakingV2.sol";
import {IStakingCollateral} from "./interfaces/stakingCollateral.sol";
import {IStakingAccountSettings} from "./interfaces/stakingAccountSettings.sol";
import {LedgerTree} from "./LedgerTree.sol";
import {Blake2b} from "./lib/Blake2b.sol";

/// @title STCtfCarry
/// @notice The carry contract of one capture the flag pool (CAPTURETHEFLAG.md
/// §4.6). vault2 requires each root's total to equal the pool's funding
/// through its epoch, so a pot waiting for a winner cannot wait inside the
/// pool: each root pays the pots and pending awards to this contract's
/// account, and `recycle` returns them to the pool. Anyone calls it. It claims
/// the account, moves what this contract's coldkey holds on vault2's escrow
/// hotkey above its residual onto the CTF pool hotkey, and transfers all of it
/// there to vault2's coldkey, whose position on the pool hotkey the pool's next
/// close captures. `recycledFor` records, for that epoch, how much vault2's
/// position grew, so the ledger can tell returned carry from fresh emission.
///
/// vault2's claim pays this coldkey on the escrow hotkey by a transfer from
/// another coldkey, which runtime 486 refuses when it would add the escrow
/// hotkey to a StakingHotkeys list that third parties have filled to 128
/// entries (VERSION2-FINAL.md §7.3), and a move of a whole position leaves no
/// dust, so without more the escrow hotkey would leave this coldkey's list at
/// every recycle and anyone could block the carry for good. So the contract
/// keeps a residual of its own on the escrow hotkey, which keeps that hotkey on
/// its list, and makes the pool hotkey its auto-stake destination on the
/// subnet, which keeps that one on its list too: every claim to it and every
/// move of its own lands on a listed hotkey, which the runtime admits whatever
/// the list's length. It also opts out of third-party list growth, which
/// keeps the list short, and keeps rejecting conviction-locked alpha. The
/// constructor seeds the residual from the TAO it is created with, by its own
/// addStake, which only the larger 256-entry cap of a coldkey's own
/// operations limits, and the residual never returns to the pool.
///
/// It has no owner and no upgrade. Its entry points are `recycle` and `seed`,
/// which only stakes this contract's own TAO onto the escrow hotkey, so that
/// the residual can be restored if the runtime ever clears it (root's sweep of
/// nominations below the nominator minimum). It reads vault2's escrow hotkey,
/// coldkey and netuid and the pool's hotkey once, at creation; vault2 never
/// changes them. Its own coldkey is its address's runtime mapping. Stake
/// reaches it only from vault2's claims, donations and its own seeds, and
/// leaves it only to vault2's position on the pool hotkey: it cannot be
/// drained or redirected. Beyond the residual it holds stake only until the
/// next recycle, which returns everything else it holds on the escrow hotkey.
// Its TAO is staked by the precompile's addStake, which debits this contract's
// mapped account natively, an outflow Slither cannot see.
// slither-disable-next-line locked-ether
contract STCtfCarry {
    // The staking precompile reads an amount of 2^64 − 1 or more as "the
    // whole position"; every amount passed to it stays below this.
    uint256 private constant WHOLE_POSITION = type(uint64).max;

    // One rao in wei: the runtime moves floor(value / 10^9) rao of native value.
    uint256 private constant RAO_WEI = 1e9;

    /// @notice The least TAO, in rao, a seed stakes: 0.1 TAO.
    uint256 public constant MINIMUM_SEED_RAO = 100_000_000;

    /// @notice A seed also stakes at least this many times the runtime's
    /// nominator minimum, below which root's sweep would clear the residual.
    uint256 public constant SEED_NOMINATOR_MULTIPLE = 10;

    // The gas a read of the nominator minimum may use: a precompile that does
    // not answer consumes all the gas forwarded to it.
    uint256 private constant NOMINATOR_READ_GAS = 100_000;

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

    /// @notice The part of this coldkey's position on the escrow hotkey that is
    /// its own: what its seeds staked there, never recycled. It only falls
    /// when the runtime lowers the position below it.
    uint256 public residual;

    event Recycled(uint256 indexed epoch, uint256 amount);
    /// @notice A seed staked `tao` rao of this contract's TAO, which added
    /// `amount` to its position on the escrow hotkey; `residual` is the new
    /// residual.
    event Seeded(uint256 tao, uint256 amount, uint256 residual);
    /// @notice The runtime lowered this coldkey's position on the escrow hotkey
    /// below the residual, which now follows it down.
    event ResidualLowered(uint256 from, uint256 to);

    error InvalidConfiguration();
    error InvalidColdkey();
    error NothingToRecycle();
    error AmountTooLarge();
    error RuntimeAccountingMismatch();
    error Reentrancy();
    error SeedTooSmall();
    error ResidualMissing();

    modifier nonReentrant() {
        if (_entered) revert Reentrancy();
        _entered = true;
        _;
        _entered = false;
    }

    /// @param vault_ vault2, which must hold pool `poolId_`.
    /// @param poolId_ the CTF pool, 2^32 + the operator's id.
    /// @dev Created with at least seedMinimum() rao of TAO, as call value or
    /// already on its address: all of it is staked on the escrow hotkey as the
    /// residual. The pool hotkey, which must hold a UID on the subnet, becomes
    /// this coldkey's auto-stake destination there. Then third parties can no
    /// longer grow this coldkey's StakingHotkeys list, and conviction-locked
    /// alpha stays refused.
    constructor(ISTSettlementVault2 vault_, uint256 poolId_) payable {
        if (address(vault_).code.length == 0) revert InvalidConfiguration();
        ISTSettlementVault2.Pool memory pool = vault_.pools(poolId_);
        if (!pool.registered) revert InvalidConfiguration();
        uint16 netuid_ = vault_.netuid();
        bytes32 escrow = vault_.escrowHotkey();
        bytes32 coldkey_ = Blake2b.mirror(address(this));
        vault = vault_;
        poolId = poolId_;
        netuid = netuid_;
        escrowHotkey = escrow;
        vaultColdkey = vault_.selfColdkey();
        poolHotkey = pool.hotkey;
        coldkey = coldkey_;

        (uint256 tao, uint256 amount) = _seed(escrow, netuid_, coldkey_);
        residual = amount;
        IStakingAccountSettings settings = IStakingAccountSettings(ISTAKING_ADDRESS);
        settings.setColdkeyAutoStakeHotkey(netuid_, pool.hotkey);
        settings.setRejectStakingHotkeyGrowth(true);
        settings.setRejectLockedAlpha(true);
        (bool pinned, bytes32 destination) = settings.getAutoStakeDestination(coldkey_, netuid_);
        if (
            !pinned || destination != pool.hotkey || !settings.getRejectStakingHotkeyGrowth(coldkey_)
                || !settings.getRejectLockedAlpha(coldkey_)
        ) {
            revert InvalidConfiguration();
        }
        emit Seeded(tao, amount, amount);
    }

    /// @notice A. Returns the carry to the pool. With an account, whose coldkey
    /// must be this contract's, it first claims it from vault2's root
    /// `rootIndex` with `proof`; an account already claimed in full, as anyone
    /// may claim it, claims nothing. A zero coldkey names no account. Then it
    /// moves all the stake this contract can move on the escrow hotkey above
    /// its residual onto the pool hotkey, and transfers all of its position
    /// there to vault2's coldkey. vault2's position grows by `amount`, recorded
    /// for `epoch`, the pool's next unfunded epoch, whose close captures it.
    /// Each move must debit its source exactly and credit its destination the
    /// amount or one rao less, as vault2 requires of its own moves; anything
    /// else reverts.
    function recycle(
        uint256 rootIndex,
        LedgerTree.Account calldata account,
        LedgerTree.ProofNode[] calldata proof
    ) external nonReentrant returns (uint256 epoch, uint256 amount) {
        uint256 kept = _keepResidual();
        if (account.coldkey != bytes32(0)) {
            if (account.coldkey != coldkey) {
                revert InvalidColdkey();
            }
            if (vault.claimed(poolId, account.index) < account.cumulative) {
                // Without its residual the escrow hotkey has left this
                // coldkey's list, and the runtime refuses vault2's transfer
                // to it until a seed restores the residual. A position the
                // runtime cleared reads exactly zero.
                // slither-disable-next-line incorrect-equality
                if (kept == 0) revert ResidualMissing();
                // vault2 pays this coldkey on its escrow hotkey, or reverts.
                // slither-disable-next-line unused-return
                vault.claim(poolId, rootIndex, account, proof);
            }
        }

        // Everything this coldkey can move on the escrow hotkey above its
        // residual, claimed by anyone or donated, moves onto the pool hotkey.
        uint256 escrowBefore = _stake(escrowHotkey, coldkey);
        uint256 held = _unlocked(escrowHotkey, escrowBefore);
        if (held <= kept) revert NothingToRecycle();
        uint256 returning = held - kept;
        if (returning >= WHOLE_POSITION) revert AmountTooLarge();
        uint256 poolBefore = _stake(poolHotkey, coldkey);
        IStaking(ISTAKING_ADDRESS).moveStake(escrowHotkey, poolHotkey, uint256(netuid), uint256(netuid), returning);
        uint256 poolAfter = _stake(poolHotkey, coldkey);
        if (
            _stake(escrowHotkey, coldkey) + returning != escrowBefore || poolAfter > poolBefore + returning
                || poolAfter + 1 < poolBefore + returning
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

    /// @notice B. Anyone, with or without value: stakes all of this
    /// contract's TAO, at least seedMinimum() rao, on the escrow hotkey and
    /// adds what its position gained to the residual. It restores a residual
    /// the runtime cleared, whose TAO the runtime returned to this contract.
    function seed() external payable nonReentrant returns (uint256 amount) {
        // The position before the seed bounds the residual kept, as in
        // recycle; the residual is read and written only once the seed is in.
        uint256 position = _unlocked(escrowHotkey, _stake(escrowHotkey, coldkey));
        uint256 tao;
        (tao, amount) = _seed(escrowHotkey, netuid, coldkey);
        uint256 kept = residual;
        if (position < kept) {
            emit ResidualLowered(kept, position);
            kept = position;
        }
        residual = kept + amount;
        emit Seeded(tao, amount, kept + amount);
    }

    /// @notice The least TAO, in rao, a seed must stake: SEED_NOMINATOR_MULTIPLE
    /// times the runtime's nominator minimum, and at least MINIMUM_SEED_RAO. A
    /// runtime that does not answer the nominator minimum leaves the floor.
    function seedMinimum() public view returns (uint256 minimum) {
        minimum = MINIMUM_SEED_RAO;
        (bool answered, bytes memory data) = ISTAKING_ADDRESS.staticcall{gas: NOMINATOR_READ_GAS}(
            abi.encodeCall(IStaking.getNominatorMinRequiredStake, ())
        );
        if (answered && data.length == 32) {
            uint256 nominator = abi.decode(data, (uint256));
            if (nominator <= type(uint128).max && nominator * SEED_NOMINATOR_MULTIPLE > minimum) {
                minimum = nominator * SEED_NOMINATOR_MULTIPLE;
            }
        }
    }

    // Stakes this contract's whole spendable TAO, at least seedMinimum(), on
    // the escrow hotkey by its own addStake, and returns the rao staked and
    // the position's gain. The constructor passes the immutables it is setting.
    function _seed(bytes32 escrow, uint16 netuid_, bytes32 coldkey_)
        private
        returns (uint256 tao, uint256 amount)
    {
        // The runtime reports the spendable balance, above the existential
        // deposit, and stakes from it.
        tao = address(this).balance / RAO_WEI;
        if (tao < seedMinimum()) revert SeedTooSmall();
        if (tao >= WHOLE_POSITION) revert AmountTooLarge();
        uint256 before = _stakeOf(escrow, coldkey_, netuid_);
        IStaking(ISTAKING_ADDRESS).addStake(escrow, tao, uint256(netuid_));
        uint256 after_ = _stakeOf(escrow, coldkey_, netuid_);
        if (after_ <= before) revert RuntimeAccountingMismatch();
        amount = after_ - before;
    }

    // The residual this call keeps on the escrow hotkey: the residual, or the
    // position this coldkey can move there when the runtime lowered it below
    // (root's sweep of small nominations, share rounding), which the residual
    // then follows. Stake owed to the pool never refills it.
    function _keepResidual() private returns (uint256 kept) {
        kept = residual;
        uint256 position = _unlocked(escrowHotkey, _stake(escrowHotkey, coldkey));
        if (position < kept) {
            residual = position;
            emit ResidualLowered(kept, position);
            kept = position;
        }
    }

    function _stake(bytes32 hotkey, bytes32 owner) private view returns (uint256) {
        return _stakeOf(hotkey, owner, netuid);
    }

    function _stakeOf(bytes32 hotkey, bytes32 owner, uint16 netuid_) private view returns (uint256) {
        return IStaking(ISTAKING_ADDRESS).getStake(hotkey, owner, uint256(netuid_));
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
