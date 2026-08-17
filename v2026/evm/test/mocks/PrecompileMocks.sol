// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// @dev vm.etch-able mocks of the subtensor precompiles (SP-1: real ABIs are
///      unverified; STSubnet reaches them only through virtual accessors, and
///      these mocks pin the v3.2.7 vendored interface shapes).
///      Pattern: deploy normally, `vm.etch(PRECOMPILE_ADDR, address(mock).code)`,
///      then configure via the etched address (storage lives at the precompile
///      address).

/// @dev Mock of IStaking (0x805, stakingV2). Tracks per-(hotkey, coldkey)
///      stake. The caller's coldkey (the EVM mirror the runtime would derive)
///      must be registered with setColdkey.
contract MockStakingV2 {
    mapping(bytes32 => mapping(bytes32 => uint256)) public stakes; // hotkey -> coldkey -> rao
    mapping(address => bytes32) public callerColdkey;
    bool public failMoveStake;

    // --- test configuration ---
    function setColdkey(address caller, bytes32 coldkey) external {
        callerColdkey[caller] = coldkey;
    }

    function setStake(bytes32 hotkey, bytes32 coldkey, uint256 amount) external {
        stakes[hotkey][coldkey] = amount;
    }

    function setFailMoveStake(bool fail) external {
        failMoveStake = fail;
    }

    // --- IStaking surface used by STSubnet ---
    function getStake(bytes32 hotkey, bytes32 coldkey, uint256) external view returns (uint256) {
        return stakes[hotkey][coldkey];
    }

    /// @dev SP-1 probe support: TAO->α at 1:1 (the mock has no AMM slippage;
    ///      it exists to test the probe's harness logic, not economics). Credits
    ///      `amount` at (hotkey, caller's coldkey).
    function addStake(bytes32 hotkey, uint256 amount, uint256) external payable {
        bytes32 ck = callerColdkey[msg.sender];
        require(ck != bytes32(0), "mock: unknown caller");
        stakes[hotkey][ck] += amount;
    }

    function moveStake(
        bytes32 originHotkey,
        bytes32 destinationHotkey,
        uint256,
        uint256,
        uint256 amount
    ) external {
        require(!failMoveStake, "mock: moveStake down");
        bytes32 ck = callerColdkey[msg.sender];
        require(ck != bytes32(0), "mock: unknown caller");
        require(stakes[originHotkey][ck] >= amount, "mock: insufficient");
        stakes[originHotkey][ck] -= amount;
        stakes[destinationHotkey][ck] += amount;
    }

    function transferStake(
        bytes32 destinationColdkey,
        bytes32 hotkey,
        uint256,
        uint256,
        uint256 amount
    ) external {
        bytes32 ck = callerColdkey[msg.sender];
        require(ck != bytes32(0), "mock: unknown caller");
        require(stakes[hotkey][ck] >= amount, "mock: insufficient");
        stakes[hotkey][ck] -= amount;
        stakes[hotkey][destinationColdkey] += amount;
    }
}

/// @dev Mock of INeuron (0x804).
contract MockNeuron {
    uint256 public registerCount;
    bytes32 public lastHotkey;
    uint16 public lastNetuid;

    function burnedRegister(uint16 netuid, bytes32 hotkey) external payable {
        registerCount++;
        lastNetuid = netuid;
        lastHotkey = hotkey;
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

    function verify(bytes32 message, bytes32 pubkey, bytes32 r, bytes32 s)
        external
        view
        returns (bool)
    {
        if (r == bytes32(0) && s == bytes32(0)) {
            return false;
        }
        return !bad[keccak256(abi.encode(message, pubkey, r, s))];
    }
}
