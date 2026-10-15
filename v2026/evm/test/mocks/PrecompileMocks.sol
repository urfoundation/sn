// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Vm} from "forge-std/Vm.sol";

/// @dev vm.etch-able mocks of the subtensor precompiles (SP-1: real ABIs are
///      unverified; STSubnet reaches them only through virtual accessors, and
///      these mocks pin the v3.2.7 vendored interface shapes).
///      Pattern: deploy normally, `vm.etch(PRECOMPILE_ADDR, address(mock).code)`,
///      then configure via the etched address (storage lives at the precompile
///      address).

/// @dev Mock of IStaking (0x805, stakingV2). Tracks per-(hotkey, coldkey)
///      stake. The caller's coldkey (the EVM mirror the runtime would derive)
///      must be registered with setColdkey. Runtime 475's registration
///      collateral (IStakingCollateral) locks part of a position: getStake
///      includes it, and a move or transfer that digs into it is refused.
contract MockStakingV2 {
    Vm private constant VM = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));
    mapping(bytes32 => mapping(bytes32 => uint256)) public stakes; // hotkey -> coldkey -> rao
    mapping(bytes32 => mapping(bytes32 => uint64)) public collateral; // hotkey -> coldkey -> locked rao
    mapping(address => bytes32) public callerColdkey;
    bool public failMoveStake;
    bool public failTransferStake;
    uint256 public nominatorMinimum = 1_000;
    uint256 public moveStakeShortfall;
    uint256 public moveStakeSourceResidue;
    uint256 public transferStakeShortfall;
    uint256 public transferStakeSourceResidue;
    uint256 public transferStakeSourceExtraDebit;
    uint256 public transferStakeDestinationExtraDebit;
    uint256 public minimumMoveAmount;
    uint256 public minimumTransferAmount;
    address public reentryTarget;
    bytes public reentryData;
    bool public reentrySucceeded;
    bytes4 public reentryFailureSelector;
    // A transfer to one of these coldkeys fails as the runtime refuses a
    // precompile call: it consumes all the gas forwarded and returns no data.
    mapping(bytes32 => bool) public exhaustTransferTo;

    // --- test configuration ---
    function setColdkey(address caller, bytes32 coldkey) external {
        callerColdkey[caller] = coldkey;
    }

    function setExhaustTransferTo(bytes32 coldkey, bool exhaust) external {
        exhaustTransferTo[coldkey] = exhaust;
    }

    function setStake(bytes32 hotkey, bytes32 coldkey, uint256 amount) external {
        stakes[hotkey][coldkey] = amount;
    }

    function setCollateral(bytes32 hotkey, bytes32 coldkey, uint64 locked) external {
        collateral[hotkey][coldkey] = locked;
    }

    function setFailMoveStake(bool fail) external {
        failMoveStake = fail;
    }

    function setFailTransferStake(bool fail) external {
        failTransferStake = fail;
    }

    function setNominatorMinimum(uint256 amount) external {
        nominatorMinimum = amount;
    }

    function setMoveStakeShortfall(uint256 amount) external {
        moveStakeShortfall = amount;
    }

    function setMoveStakeSourceResidue(uint256 amount) external {
        moveStakeSourceResidue = amount;
    }

    function setTransferStakeShortfall(uint256 amount) external {
        transferStakeShortfall = amount;
    }

    function setTransferStakeSourceResidue(uint256 amount) external {
        transferStakeSourceResidue = amount;
    }

    function setTransferStakeSourceExtraDebit(uint256 amount) external {
        transferStakeSourceExtraDebit = amount;
    }

    function setTransferStakeDestinationExtraDebit(uint256 amount) external {
        transferStakeDestinationExtraDebit = amount;
    }

    function setMinimumMoveAmount(uint256 amount) external {
        minimumMoveAmount = amount;
    }

    function setMinimumTransferAmount(uint256 amount) external {
        minimumTransferAmount = amount;
    }

    function setReentry(address target, bytes calldata data) external {
        reentryTarget = target;
        reentryData = data;
        reentrySucceeded = false;
        reentryFailureSelector = bytes4(0);
    }

    function clearReentry() external {
        delete reentryTarget;
        delete reentryData;
        reentrySucceeded = false;
        reentryFailureSelector = bytes4(0);
    }

    function _attemptReentry() private {
        if (reentryTarget == address(0)) return;
        (bool ok, bytes memory result) = reentryTarget.call(reentryData);
        reentrySucceeded = ok;
        if (!ok && result.length >= 4) {
            bytes4 selector;
            assembly {
                selector := mload(add(result, 32))
            }
            reentryFailureSelector = selector;
        }
    }

    // --- IStaking surface used by STSubnet ---
    function getStake(bytes32 hotkey, bytes32 coldkey, uint256) external view returns (uint256) {
        return stakes[hotkey][coldkey];
    }

    function getNominatorMinRequiredStake() external view returns (uint256) {
        return nominatorMinimum;
    }

    function getMinerCollateral(uint16, bytes32 hotkey, bytes32 coldkey)
        external
        view
        returns (bool exists, uint64 locked, uint128 drainRatio, uint64 minLocked, uint64 earned)
    {
        locked = collateral[hotkey][coldkey];
        return (locked != 0, locked, 0, 0, 0);
    }

    /// @dev SP-1 probe support: TAO->α at 1:1 (the mock has no AMM slippage;
    ///      it exists to test the probe's harness logic, not economics). The
    ///      runtime withdraws native funds from the caller's mapped account;
    ///      EVM value sent to 0x805 cannot fund that withdrawal. Model this
    ///      native debit with a cheatcode because Solidity cannot debit callers.
    function addStake(bytes32 hotkey, uint256 amount, uint256) external payable {
        require(address(this) == address(0x805), "runtime453: foreign staking frame");
        bytes32 ck = callerColdkey[msg.sender];
        require(ck != bytes32(0), "mock: unknown caller");
        uint256 amountWei = amount * 1e9;
        require(msg.sender.balance >= amountWei, "NotEnoughBalanceToStake");
        VM.deal(msg.sender, msg.sender.balance - amountWei);
        stakes[hotkey][ck] += amount;
    }

    function moveStake(bytes32 originHotkey, bytes32 destinationHotkey, uint256, uint256, uint256 amount)
        external
    {
        require(address(this) == address(0x805), "runtime453: foreign staking frame");
        require(!failMoveStake, "mock: moveStake down");
        require(amount >= minimumMoveAmount, "mock: move amount too low");
        require(amount > moveStakeShortfall, "mock: move shortfall");
        require(amount > moveStakeSourceResidue, "mock: move source residue");
        bytes32 ck = callerColdkey[msg.sender];
        require(ck != bytes32(0), "mock: unknown caller");
        require(stakes[originHotkey][ck] >= amount, "mock: insufficient");
        require(stakes[originHotkey][ck] - amount >= collateral[originHotkey][ck], "mock: StakeUnavailable");
        _attemptReentry();
        stakes[originHotkey][ck] -= amount - moveStakeSourceResidue;
        stakes[destinationHotkey][ck] += amount - moveStakeShortfall;
    }

    function transferStake(bytes32 destinationColdkey, bytes32 hotkey, uint256, uint256, uint256 amount)
        external
    {
        require(address(this) == address(0x805), "runtime453: foreign staking frame");
        require(!failTransferStake, "mock: transferStake down");
        if (exhaustTransferTo[destinationColdkey]) {
            assembly {
                invalid()
            }
        }
        bytes32 ck = callerColdkey[msg.sender];
        require(ck != bytes32(0), "mock: unknown caller");
        require(amount >= minimumTransferAmount, "mock: transfer amount too low");
        require(amount > transferStakeShortfall, "mock: transfer shortfall");
        require(amount > transferStakeSourceResidue, "mock: transfer source residue");
        require(stakes[hotkey][ck] >= amount, "mock: insufficient");
        require(stakes[hotkey][ck] - amount >= collateral[hotkey][ck], "mock: StakeUnavailable");
        _attemptReentry();
        stakes[hotkey][ck] -= amount - transferStakeSourceResidue;
        stakes[hotkey][ck] -= transferStakeSourceExtraDebit;
        stakes[hotkey][destinationColdkey] += amount - transferStakeShortfall;
        stakes[hotkey][destinationColdkey] -= transferStakeDestinationExtraDebit;
    }
}

/// @dev Mock of IAlpha (0x808). Runtime 453 returns a 9-decimal spot price
///      converted to the EVM's 18-decimal balance scale.
contract MockAlpha {
    mapping(uint16 => uint256) public prices;
    bool public failPrice;

    function setAlphaPrice(uint16 netuid, uint256 price) external {
        prices[netuid] = price;
    }

    function setFailPrice(bool fail) external {
        failPrice = fail;
    }

    function getAlphaPrice(uint16 netuid) external view returns (uint256) {
        require(!failPrice, "mock: alpha price down");
        return prices[netuid];
    }
}

/// @dev Mock of INeuron (0x804). registerLimit can be refused, burns from
///      the registrant's native balance as the runtime burns from its mapped
///      account, and can call a reentry target before it records anything.
///      The burn is a cheatcode balance write, which a revert does not undo.
contract MockNeuron {
    Vm private constant VM = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));
    uint256 public registerCount;
    bytes32 public lastHotkey;
    uint16 public lastNetuid;
    address public lastRegistrant;
    uint64 public lastLimitPrice;
    uint256 public lastRegistrationCallValue;
    mapping(uint16 => mapping(bytes32 => address)) public registrants;
    mapping(uint16 => mapping(bytes32 => uint16)) public uids;
    mapping(uint16 => mapping(bytes32 => bool)) public uidExists;
    bool public failRegistration;
    uint256 public registrationBurnWei;
    address public reentryTarget;
    bytes public reentryData;
    bool public reentrySucceeded;
    bytes4 public reentryFailureSelector;

    function setFailRegistration(bool fail) external {
        failRegistration = fail;
    }

    function setRegistrationBurn(uint256 burnWei) external {
        registrationBurnWei = burnWei;
    }

    function setReentry(address target, bytes calldata data) external {
        reentryTarget = target;
        reentryData = data;
        reentrySucceeded = false;
        reentryFailureSelector = bytes4(0);
    }

    function clearReentry() external {
        delete reentryTarget;
        delete reentryData;
        reentrySucceeded = false;
        reentryFailureSelector = bytes4(0);
    }

    function _attemptReentry() private {
        if (reentryTarget == address(0)) return;
        (bool ok, bytes memory result) = reentryTarget.call(reentryData);
        reentrySucceeded = ok;
        if (!ok && result.length >= 4) {
            bytes4 selector;
            assembly {
                selector := mload(add(result, 32))
            }
            reentryFailureSelector = selector;
        }
    }

    function setUid(uint16 netuid, bytes32 hotkey, uint16 uid) external {
        uids[netuid][hotkey] = uid;
        uidExists[netuid][hotkey] = true;
    }

    function setUidResponse(uint16 netuid, bytes32 hotkey, bool exists, uint16 uid) external {
        uids[netuid][hotkey] = uid;
        uidExists[netuid][hotkey] = exists;
    }

    function clearUid(uint16 netuid, bytes32 hotkey) external {
        delete uids[netuid][hotkey];
        delete uidExists[netuid][hotkey];
    }

    function burnedRegister(uint16 netuid, bytes32 hotkey) external payable {
        require(address(this) == address(0x804), "runtime453: foreign neuron frame");
        registerCount++;
        lastNetuid = netuid;
        lastHotkey = hotkey;
        lastRegistrant = msg.sender;
        registrants[netuid][hotkey] = msg.sender;
    }

    function registerLimit(uint16 netuid, bytes32 hotkey, uint64 limitPrice) external payable {
        require(address(this) == address(0x804), "runtime453: foreign neuron frame");
        require(!failRegistration, "mock: registration refused");
        if (registrationBurnWei != 0) {
            require(msg.sender.balance >= registrationBurnWei, "mock: burn exceeds balance");
            VM.deal(msg.sender, msg.sender.balance - registrationBurnWei);
        }
        _attemptReentry();
        registerCount++;
        lastNetuid = netuid;
        lastHotkey = hotkey;
        lastRegistrant = msg.sender;
        lastLimitPrice = limitPrice;
        lastRegistrationCallValue = msg.value;
        registrants[netuid][hotkey] = msg.sender;
    }

    function getUid(uint16 netuid, bytes32 hotkey) external view returns (bool exists, uint16 uid) {
        return (uidExists[netuid][hotkey], uids[netuid][hotkey]);
    }
}

/// @dev Mock of IMetagraph (0x802) — just the lookups STSubnet uses.
contract MockMetagraph {
    uint16 public count;
    mapping(uint16 => bytes32) public hotkeys;
    mapping(uint16 => bytes32) public coldkeys;

    function setNeuron(uint16 uid, bytes32 hotkey, bytes32 coldkey) external {
        hotkeys[uid] = hotkey;
        coldkeys[uid] = coldkey;
        if (uid >= count) {
            count = uid + 1;
        }
    }

    function getUidCount(uint16) external view returns (uint16) {
        return count;
    }

    function getHotkey(uint16, uint16 uid) external view returns (bytes32) {
        return hotkeys[uid];
    }

    function getColdkey(uint16, uint16 uid) external view returns (bytes32) {
        return coldkeys[uid];
    }
}

/// @dev Mock of IEd25519Verify (0x402). Verifies everything with nonzero
///      (r, s) unless the exact tuple is flagged bad.
contract MockEd25519 {
    mapping(bytes32 => bool) public bad;

    function setBad(bytes32 message, bytes32 pubkey, bytes32 r, bytes32 s, bool isBad) external {
        bad[keccak256(abi.encode(message, pubkey, r, s))] = isBad;
    }

    function verify(bytes32 message, bytes32 pubkey, bytes32 r, bytes32 s) external view returns (bool) {
        if (r == bytes32(0) && s == bytes32(0)) {
            return false;
        }
        return !bad[keccak256(abi.encode(message, pubkey, r, s))];
    }
}

/// @dev Mock of runtime-453 sr25519 verifier (0x403), with the same failure
/// controls as the Ed25519 mock. Real randomized sr25519 vectors are exercised
/// by the Go fixture and live preflight.
contract MockSr25519 {
    mapping(bytes32 => bool) public bad;

    function setBad(bytes32 message, bytes32 pubkey, bytes32 r, bytes32 s, bool isBad) external {
        bad[keccak256(abi.encode(message, pubkey, r, s))] = isBad;
    }

    function verify(bytes32 message, bytes32 pubkey, bytes32 r, bytes32 s) external view returns (bool) {
        if (r == bytes32(0) && s == bytes32(0)) return false;
        return !bad[keccak256(abi.encode(message, pubkey, r, s))];
    }
}
