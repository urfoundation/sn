// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {ERC1967Utils} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Utils.sol";

import {V2StackBase} from "./V2StackBase.sol";
import {V2TreeHelper} from "./V2TreeHelper.sol";
import {STSettlement2PhaseC} from "../../src/STSettlement2PhaseC.sol";
import {STSettlement2Ctf} from "../../src/STSettlement2Ctf.sol";

/// @dev The capture the flag upgrade (CAPTURETHEFLAG.md §3.1) on the whole
///      settlement v2 stack: NO1's provider pool is registered from
///      FIRST_EPOCH, and the proxy runs the phase C implementation, as mainnet
///      does, then the CTF implementation. NO2 is a v1 operator without a v2
///      pool. No CTF pool is registered yet; CTF_FIRST_EPOCH follows the
///      provider pool's first epoch, so epoch FIRST_EPOCH is before it.
abstract contract V2CtfBase is V2StackBase {
    uint256 internal constant CTF_BASE = 1 << 32;
    uint256 internal constant CTF1 = CTF_BASE + NO1;
    uint256 internal constant CTF2 = CTF_BASE + NO2;
    uint64 internal constant CTF_FIRST_EPOCH = FIRST_EPOCH + 1;
    uint16 internal constant CTF1_UID = 31;
    uint16 internal constant CTF2_UID = 32;
    // The call value that pays a registration capped at V2_BURN_LIMIT rao.
    uint256 internal constant BURN_WEI = uint256(V2_BURN_LIMIT) * 1e9;
    // The census window is half the 1,200-block root commit window.
    uint64 internal constant CENSUS_WINDOW = V2_ROOT_WINDOW / 2;

    address internal phaseCImplementation;
    address internal ctfImplementation;
    STSettlement2Ctf internal ctf;

    function setUp() public virtual override {
        super.setUp();
        _registerV2Pool(NO1, V2_POOL1, V2_POOL1_UID);
        phaseCImplementation = address(new STSettlement2PhaseC());
        vm.prank(owner);
        settlement.upgradeToAndCall(phaseCImplementation, "");
        ctfImplementation = address(new STSettlement2Ctf());
        vm.prank(owner);
        settlement.upgradeToAndCall(ctfImplementation, "");
        ctf = STSettlement2Ctf(payable(address(settlement)));
    }

    function _implementation() internal view returns (address) {
        return address(uint160(uint256(vm.load(address(settlement), ERC1967Utils.IMPLEMENTATION_SLOT))));
    }

    /// @dev The hotkey registerCtfPool derives for operator `noId` in the
    ///      current block.
    function _ctfHotkey(uint256 noId) internal view returns (bytes32) {
        return keccak256(
            abi.encode(address(settlement), CTF_BASE + noId, block.number, blockhash(block.number - 1))
        );
    }

    /// @dev The owner registers operator `noId`'s CTF pool from `firstEpoch`
    ///      in the current block, paying BURN_WEI; the runtime gives its
    ///      derived hotkey `uid`.
    function _registerCtf(uint256 noId, uint64 firstEpoch, uint16 uid) internal returns (bytes32 hotkey) {
        hotkey = _ctfHotkey(noId);
        neuron.setUid(NETUID, hotkey, uid);
        vm.deal(owner, owner.balance + BURN_WEI);
        vm.prank(owner);
        ctf.registerCtfPool{value: BURN_WEI}(noId, V2_BURN_LIMIT, firstEpoch);
    }

    function _pause() internal {
        vm.prank(guardian);
        coordinator.setPaused(true);
    }

    /// @dev NO2 joins v2 as NO1 did, through the CTF implementation.
    function _registerProviderPool2() internal {
        neuron.setUid(NETUID, V2_POOL2, V2_POOL2_UID);
        vm.prank(owner);
        ctf.registerOperatorPool(NO2, V2_POOL2, V2_BURN_LIMIT, FIRST_EPOCH);
    }

    /// @dev NO1's root signer commits `censusHash` for `epoch_` at `block_`.
    function _commitCensus(uint256 epoch_, bytes32 censusHash, uint256 block_) internal {
        vm.roll(block_);
        vm.prank(rootSigner1);
        ctf.commitCtfCensus(epoch_, NO1, censusHash);
    }

    /// @dev Funds the CTF pool's `epoch_` with what `values` add to its captured
    ///      total, then closes, commits, approves and finalizes the honest
    ///      tree of the accounts as NO1's root signer and the auditor, and
    ///      returns the tree and its root index.
    function _publishCtf(uint256 epoch_, bytes32 hotkey, bytes32[] memory coldkeys, uint256[] memory values)
        internal
        returns (V2TreeHelper.Node[][] memory levels, uint256 rootIndex)
    {
        levels = V2TreeHelper.build(coldkeys, values);
        _accrueV2(hotkey, V2TreeHelper.root(levels).sum - vault2.capturedTotal(CTF1));
        _closeV2(epoch_, CTF1);
        _commitV2(epoch_, CTF1, rootSigner1, levels, coldkeys.length);
        _approveV2(CTF1, auditor1);
        rootIndex = _finalizeV2(CTF1);
    }
}
