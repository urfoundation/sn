// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";
import {STSettlementVault2} from "../src/STSettlementVault2.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";

/// @dev Pays a registration and tries to re-enter the vault from the refund.
contract V2ReentrantPayer {
    bool public reentered;
    bytes4 public refusal;
    bytes private _reentry;

    function registerPoolUid(
        STSettlementVault2 target,
        uint256 noId,
        uint64 maximumBurnRao,
        bytes calldata reentry
    ) external payable {
        _reentry = reentry;
        target.registerPoolUid{value: msg.value}(noId, maximumBurnRao);
    }

    receive() external payable {
        (bool ok, bytes memory result) = msg.sender.call(_reentry);
        reentered = ok;
        // Revert data starts with the refusal's 4-byte selector.
        // forge-lint: disable-next-line(unsafe-typecast)
        refusal = bytes4(result);
    }
}

/// @dev V-20: no runtime or refund callback can enter the vault while one of
///      its calls is in progress. Each precompile mock's hook calls back this
///      contract, which is the bootstrap and the coordinator, so every entry
///      point passes its caller check and reaches the shared guard.
contract V2VaultReentryTest is V2VaultBase {
    bytes32 internal constant FOREIGN = keccak256("v1-escrow-hotkey");
    bytes32 internal constant FRESH_ESCROW = keccak256("v2-fresh-escrow-hotkey");

    bool internal reentered;
    bytes4 internal refusal;
    // When set, the runtime gives this hotkey a UID after the reentry attempt.
    bytes32 internal uidHotkey;

    function reenter(address target, bytes calldata data) external {
        (bool ok, bytes memory result) = target.call(data);
        reentered = ok;
        // Revert data starts with the refusal's 4-byte selector.
        // forge-lint: disable-next-line(unsafe-typecast)
        refusal = bytes4(result);
        if (uidHotkey != bytes32(0)) neuron.setUid(NETUID, uidHotkey, 42);
    }

    // One call to each of the vault's thirteen state-changing entry points.
    function _entryPoints() internal view returns (bytes[] memory calls) {
        LedgerTree.Account memory account = _account(0, ALICE, 600);
        LedgerTree.ProofNode[] memory proof = new LedgerTree.ProofNode[](0);
        LedgerTree.PathNode[] memory path = new LedgerTree.PathNode[](0);
        ISTSettlementVault2.Commitment memory commitment = ISTSettlementVault2.Commitment({
            root: keccak256("root"), total: 1, leafCount: 1, ledgerHash: keccak256("ledger")
        });
        calls = new bytes[](13);
        calls[0] = abi.encodeCall(ISTSettlementVault2.setCoordinatorOnce, (address(this)));
        calls[1] = abi.encodeCall(ISTSettlementVault2.registerEscrow, (BURN_LIMIT));
        calls[2] = abi.encodeCall(ISTSettlementVault2.registerPool, (NO2, POOL2, BURN_LIMIT, FIRST_EPOCH));
        calls[3] = abi.encodeCall(ISTSettlementVault2.registerPoolUid, (NO1, BURN_LIMIT));
        calls[4] = abi.encodeCall(ISTSettlementVault2.captureEmission, (vault.lastFundedEpoch(NO1) + 1, NO1));
        calls[5] = abi.encodeCall(ISTSettlementVault2.deferEmission, (vault.lastFundedEpoch(NO1) + 1, NO1));
        calls[6] = abi.encodeCall(ISTSettlementVault2.commitPending, (FIRST_EPOCH, NO1, 0, commitment));
        calls[7] = abi.encodeCall(ISTSettlementVault2.demandPath, (NO1, 0, account, proof));
        calls[8] = abi.encodeCall(ISTSettlementVault2.checkPath, (NO1, 0, account, proof, path));
        calls[9] = abi.encodeCall(ISTSettlementVault2.finalizePending, (NO1));
        calls[10] = abi.encodeCall(ISTSettlementVault2.claim, (NO1, 0, account, proof));
        calls[11] = abi.encodeCall(ISTSettlementVault2.absorbStake, (FOREIGN));
        calls[12] = abi.encodeCall(ISTSettlementVault2.assignSurplus, (NO1, 1));
    }

    // Runs `outer` once per entry point, with the staking or neuron hook
    // re-entering `target` at it, and checks that the guard refused each one.
    function _assertGuarded(function() internal outer, address target, bool throughNeuron) internal {
        bytes[] memory calls = _entryPoints();
        for (uint256 i; i < calls.length; i++) {
            uint256 snapshot = vm.snapshotState();
            bytes memory hook = abi.encodeCall(this.reenter, (target, calls[i]));
            if (throughNeuron) neuron.setReentry(address(this), hook);
            else staking.setReentry(address(this), hook);
            outer();
            assertFalse(reentered, "re-entered");
            assertEq(refusal, ISTSettlementVault2.Reentrancy.selector, "refusal");
            vm.revertToState(snapshot);
        }
    }

    function _captureOuter() internal {
        _capture(NO1, 1_000);
        assertEq(vault.totalCaptured(), 1_000);
    }

    function _absorbOuter() internal {
        staking.setStake(FOREIGN, SELF, 500);
        assertEq(vault.absorbStake(FOREIGN), 500);
    }

    // The hook is held back while the root is funded, so it fires in the claim.
    function _claimOuter() internal {
        bytes memory hook = staking.reentryData();
        staking.clearReentry();
        V2TreeHelper.Node[][] memory levels = _publish(NO1, _keys(ALICE, BOB), _values(600, 400));
        staking.setReentry(address(this), hook);
        assertEq(_claim(NO1, 0, levels, 0, ALICE, 600), 600);
    }

    function _registerPoolOuter() internal {
        neuron.setUid(NETUID, POOL2, POOL2_UID);
        assertEq(vault.registerPool(NO2, POOL2, BURN_LIMIT, FIRST_EPOCH), POOL2_UID);
    }

    function _registerPoolUidOuter() internal {
        neuron.clearUid(NETUID, POOL1);
        uidHotkey = POOL1;
        assertEq(vault.registerPoolUid(NO1, BURN_LIMIT), 42);
    }

    // V-20
    function test_V20_captureEmissionCannotBeReentered() public {
        _assertGuarded(_captureOuter, address(vault), false);
    }

    // V-20
    function test_V20_absorbStakeCannotBeReentered() public {
        _assertGuarded(_absorbOuter, address(vault), false);
    }

    // V-20
    function test_V20_claimCannotBeReentered() public {
        _assertGuarded(_claimOuter, address(vault), false);
    }

    // V-20
    function test_V20_registerPoolCannotBeReentered() public {
        _assertGuarded(_registerPoolOuter, address(vault), true);
    }

    // V-20
    function test_V20_registerPoolUidCannotBeReentered() public {
        _assertGuarded(_registerPoolUidOuter, address(vault), true);
    }

    // V-20: the escrow registration of a vault whose coordinator is fixed.
    function test_V20_registerEscrowCannotBeReentered() public {
        STSettlementVault2 fresh =
            new STSettlementVault2(NETUID, FRESH_ESCROW, SELF, address(this), DEMAND_FEE);
        fresh.setCoordinatorOnce(address(this));
        neuron.setUid(NETUID, FRESH_ESCROW, 20);
        vault = fresh;
        _assertGuarded(_registerEscrowOuter, address(fresh), true);
    }

    function _registerEscrowOuter() internal {
        assertEq(vault.registerEscrow(BURN_LIMIT), 20);
    }

    // V-20: the registration refund goes to the caller, which cannot re-enter.
    function test_V20_aRegistrationRefundCannotReenter() public {
        neuron.clearUid(NETUID, POOL1);
        _uidOnRegistration(POOL1, 42);
        V2ReentrantPayer payer = new V2ReentrantPayer();
        vm.deal(address(this), 1 ether);
        bytes memory reentry = abi.encodeCall(ISTSettlementVault2.registerPoolUid, (NO1, BURN_LIMIT));
        payer.registerPoolUid{value: 1 ether}(vault, NO1, BURN_LIMIT, reentry);
        assertFalse(payer.reentered());
        assertEq(payer.refusal(), ISTSettlementVault2.Reentrancy.selector);
        assertEq(address(payer).balance, 1 ether);
    }
}
