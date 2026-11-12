// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Vm} from "forge-std/Vm.sol";

import {MockStakingV2, MockNeuron} from "./PrecompileMocks.sol";
import {Blake2b} from "../../src/lib/Blake2b.sol";

/// @dev MockStakingV2 with runtime 486's StakingHotkeys rules, which the carry
///      contract's residual answers (CAPTURETHEFLAG.md §4.6; upstream d665b0eb
///      pallets/subtensor/src). Etched over MockStakingV2 at 0x805 after the
///      stack's setUp, so the base's storage carries over. It models:
///      - each coldkey's StakingHotkeys list: an entry is added when the pair
///        gains stake and dropped when its position reaches zero, as a move of
///        a whole position leaves no dust (primitives/share-pool,
///        withdrawal_drains_member; staking/stake_utils.rs:921-949);
///      - a transfer from another coldkey adds the hotkey only to a list of
///        fewer than 128 entries, and not at all to a coldkey that rejects
///        growth unless it owns the hotkey (stake_utils.rs:1567-1586); a
///        coldkey's own add or move needs fewer than 256 (stake_utils.rs:1513-
///        1525, add_stake validation);
///      - a coldkey reserved as a coldkey swap's destination refuses every
///        transfer from another coldkey (stake_utils.rs:1937-1945);
///      - root's sweep of nominations below the nominator minimum, which
///        returns the stake to the coldkey's TAO (staking/helpers.rs:236-301);
///      - a coldkey swap merging one coldkey's positions into another's, which
///        a destination that rejects growth refuses when it would bring a new
///        hotkey (swap/swap_coldkey.rs:645-666);
///      - a coldkey's auto-stake destination on a subnet, which must hold a UID
///        there (the neuron mock's) and keeps its entry on the list without
///        stake (dispatches.rs:1978-2026, stake_utils.rs:921-930).
///      A caller the mock does not know gets the runtime's mapping of its
///      address as its coldkey, so a contract can stake from its constructor.
///      Refusals revert with "mock: <runtime error>".
contract MockStakingRuntime486 is MockStakingV2 {
    Vm private constant CHEATS = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    uint256 public constant MAX_THIRD_PARTY_STAKING_HOTKEYS = 128;
    uint256 public constant MAX_STAKING_HOTKEYS = 256;

    mapping(bytes32 coldkey => bytes32[] hotkeys) private _lists;
    mapping(bytes32 coldkey => mapping(bytes32 hotkey => uint256 indexPlusOne)) private _listed;
    mapping(bytes32 coldkey => bool) public rejectsGrowth;
    mapping(bytes32 coldkey => bool) public acceptsLockedAlpha;
    mapping(bytes32 coldkey => bool) public reserved;
    mapping(bytes32 hotkey => bytes32 coldkey) public hotkeyOwner;
    // The EVM account whose native balance is a coldkey's TAO.
    mapping(bytes32 coldkey => address) public accountOf;
    mapping(bytes32 coldkey => mapping(uint16 netuid => bytes32 hotkey)) public autoStake;
    mapping(bytes32 coldkey => mapping(bytes32 hotkey => uint256 count)) private _pins;
    uint256 public minimumAddStake;
    bool public failFlags;
    uint256 public addStakeCalls;

    // --- test configuration ---
    function setReserved(bytes32 coldkey, bool isReserved) external {
        reserved[coldkey] = isReserved;
    }

    function setHotkeyOwner(bytes32 hotkey, bytes32 coldkey) external {
        hotkeyOwner[hotkey] = coldkey;
    }

    function setMinimumAddStake(uint256 amount) external {
        minimumAddStake = amount;
    }

    function setFailFlags(bool fail) external {
        failFlags = fail;
    }

    // --- views of the model ---
    function stakingHotkeys(bytes32 coldkey) external view returns (bytes32[] memory) {
        return _lists[coldkey];
    }

    function stakingHotkeyCount(bytes32 coldkey) external view returns (uint256) {
        return _lists[coldkey].length;
    }

    function isStakingHotkey(bytes32 coldkey, bytes32 hotkey) external view returns (bool) {
        return _listed[coldkey][hotkey] != 0;
    }

    // --- runtime 486's account flags ---
    function setRejectStakingHotkeyGrowth(bool enabled) external {
        require(!failFlags, "mock: flags down");
        rejectsGrowth[_caller()] = enabled;
    }

    function getRejectStakingHotkeyGrowth(bytes32 coldkey) external view returns (bool) {
        return rejectsGrowth[coldkey];
    }

    function setRejectLockedAlpha(bool enabled) external payable {
        require(!failFlags, "mock: flags down");
        acceptsLockedAlpha[_caller()] = !enabled;
    }

    function getRejectLockedAlpha(bytes32 coldkey) external view returns (bool) {
        return !acceptsLockedAlpha[coldkey];
    }

    function setColdkeyAutoStakeHotkey(uint16 netuid, bytes32 hotkey) external {
        bytes32 ck = _caller();
        (bool registered,) = MockNeuron(address(0x804)).getUid(netuid, hotkey);
        require(registered, "mock: HotKeyNotRegisteredInSubNet");
        _ensureOwnGrowth(ck, hotkey);
        bytes32 old = autoStake[ck][netuid];
        require(old != hotkey, "mock: SameAutoStakeHotkeyAlreadySet");
        autoStake[ck][netuid] = hotkey;
        _pins[ck][hotkey]++;
        _sync(ck, hotkey);
        if (old != bytes32(0)) {
            _pins[ck][old]--;
            _sync(ck, old);
        }
    }

    function getAutoStakeDestination(bytes32 coldkey, uint16 netuid) external view returns (bool, bytes32) {
        bytes32 hotkey = autoStake[coldkey][netuid];
        return (hotkey != bytes32(0), hotkey);
    }

    // --- stake operations under the list rules ---

    /// @dev A protocol credit or debit: no cap applies, and the list follows.
    function setStake(bytes32 hotkey, bytes32 coldkey, uint256 amount) public override {
        super.setStake(hotkey, coldkey, amount);
        _sync(coldkey, hotkey);
    }

    function addStake(bytes32 hotkey, uint256 amount, uint256 netuid) public payable override {
        bytes32 ck = _caller();
        require(amount >= minimumAddStake, "mock: AmountTooLow");
        _ensureOwnGrowth(ck, hotkey);
        addStakeCalls++;
        super.addStake(hotkey, amount, netuid);
        _sync(ck, hotkey);
    }

    function moveStake(
        bytes32 originHotkey,
        bytes32 destinationHotkey,
        uint256 originNetuid,
        uint256 destinationNetuid,
        uint256 amount
    ) public override {
        bytes32 ck = _caller();
        _ensureOwnGrowth(ck, destinationHotkey);
        super.moveStake(originHotkey, destinationHotkey, originNetuid, destinationNetuid, amount);
        _sync(ck, originHotkey);
        _sync(ck, destinationHotkey);
    }

    function transferStake(
        bytes32 destinationColdkey,
        bytes32 hotkey,
        uint256 originNetuid,
        uint256 destinationNetuid,
        uint256 amount
    ) public override {
        bytes32 ck = _caller();
        if (destinationColdkey != ck) {
            require(!reserved[destinationColdkey], "mock: ColdkeySwapDestinationReserved");
            _ensureThirdPartyGrowth(destinationColdkey, hotkey);
        }
        super.transferStake(destinationColdkey, hotkey, originNetuid, destinationNetuid, amount);
        _sync(ck, hotkey);
        _sync(destinationColdkey, hotkey);
    }

    /// @dev Root raising the nominator minimum: a position of `coldkey` on a
    ///      hotkey it does not own, worth less than `minimumAlpha`, is unstaked
    ///      to the coldkey's TAO, at 1:1, and leaves its list.
    function sweepSmallNomination(bytes32 hotkey, bytes32 coldkey, uint256 minimumAlpha) external {
        uint256 held = stakes[hotkey][coldkey];
        if (hotkeyOwner[hotkey] == coldkey || held == 0 || held >= minimumAlpha) return;
        stakes[hotkey][coldkey] = 0;
        _sync(coldkey, hotkey);
        address account = accountOf[coldkey];
        if (account != address(0)) {
            CHEATS.deal(account, account.balance + held * 1e9);
        }
    }

    /// @dev Whether a coldkey swap from `source` into `destination` is
    ///      admitted: a destination that rejects growth refuses a source with
    ///      a hotkey that is not on its own list, and the union of the lists
    ///      must fit the swap's 256-hotkey bound.
    function coldkeySwapAdmitted(bytes32 source, bytes32 destination) public view returns (bool) {
        bytes32[] storage from = _lists[source];
        uint256 added;
        for (uint256 i = 0; i < from.length; i++) {
            if (_listed[destination][from[i]] == 0) {
                if (rejectsGrowth[destination]) return false;
                added++;
            }
        }
        return _lists[destination].length + added <= MAX_STAKING_HOTKEYS;
    }

    /// @dev A coldkey swap's merge, once admitted: every position of `source`
    ///      moves to `destination`, whose list gains each new hotkey.
    function swapColdkeyInto(bytes32 source, bytes32 destination) external {
        require(!rejectsGrowth[destination] || coldkeySwapAdmitted(source, destination),
            "mock: AccountRejectsStakingHotkeyGrowth");
        require(coldkeySwapAdmitted(source, destination), "mock: ColdkeySwapTooHeavy");
        bytes32[] memory from = _lists[source];
        for (uint256 i = 0; i < from.length; i++) {
            uint256 held = stakes[from[i]][source];
            stakes[from[i]][source] = 0;
            stakes[from[i]][destination] += held;
            _sync(source, from[i]);
            _sync(destination, from[i]);
        }
    }

    // --- the list rules ---
    function _caller() private returns (bytes32 ck) {
        ck = callerColdkey[msg.sender];
        if (ck == bytes32(0)) {
            ck = Blake2b.hash256(abi.encodePacked(bytes4(0x65766d3a), msg.sender));
            callerColdkey[msg.sender] = ck;
        }
        if (accountOf[ck] == address(0)) {
            accountOf[ck] = msg.sender;
        }
    }

    function _ensureOwnGrowth(bytes32 coldkey, bytes32 hotkey) private view {
        if (_listed[coldkey][hotkey] != 0) return;
        require(_lists[coldkey].length < MAX_STAKING_HOTKEYS, "mock: TooManyStakingHotkeys");
    }

    function _ensureThirdPartyGrowth(bytes32 coldkey, bytes32 hotkey) private view {
        if (_listed[coldkey][hotkey] != 0) return;
        require(
            !rejectsGrowth[coldkey] || hotkeyOwner[hotkey] == coldkey, "mock: AccountRejectsStakingHotkeyGrowth"
        );
        require(_lists[coldkey].length < MAX_THIRD_PARTY_STAKING_HOTKEYS, "mock: TooManyStakingHotkeys");
    }

    // An entry stays while the pair holds stake, or the hotkey is the
    // coldkey's auto-stake destination.
    function _sync(bytes32 coldkey, bytes32 hotkey) private {
        uint256 index = _listed[coldkey][hotkey];
        if (stakes[hotkey][coldkey] != 0 || _pins[coldkey][hotkey] != 0) {
            if (index == 0) {
                _lists[coldkey].push(hotkey);
                _listed[coldkey][hotkey] = _lists[coldkey].length;
            }
        } else if (index != 0) {
            bytes32[] storage list = _lists[coldkey];
            bytes32 last = list[list.length - 1];
            list[index - 1] = last;
            _listed[coldkey][last] = index;
            list.pop();
            delete _listed[coldkey][hotkey];
        }
    }
}
