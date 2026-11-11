// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {V2SettlementApprovalTest} from "./V2SettlementApproval.t.sol";
import {V2SettlementEpochsTest} from "./V2SettlementEpochs.t.sol";
import {V2SettlementGovernanceTest} from "./V2SettlementGovernance.t.sol";
import {V2SettlementStackTest} from "./V2SettlementStack.t.sol";
import {V2SettlementPhaseCTest} from "./V2SettlementPhaseC.t.sol";
import {V2SettlementTimelockTest} from "./V2SettlementTimelock.t.sol";
import {STSettlement2Ctf} from "../src/STSettlement2Ctf.sol";

// STSettlement2Ctf restates STSettlement2 and STSettlement2PhaseC instead of
// inheriting them (CAPTURETHEFLAG.md §3.1): their registerOperatorPool and
// commitDistribution are not virtual and their sources are release-locked. So
// every phase A suite (S-1 to S-9) and every phase C and timelock suite
// (SC-1 to SC-17, TL-1 to TL-6) runs again, unchanged, against the CTF
// implementation, which must keep each of those functions, events and errors.

contract V2SettlementApprovalCtfTest is V2SettlementApprovalTest {
    function _newSettlementImplementation() internal override returns (address) {
        return address(new STSettlement2Ctf());
    }
}

contract V2SettlementEpochsCtfTest is V2SettlementEpochsTest {
    function _newSettlementImplementation() internal override returns (address) {
        return address(new STSettlement2Ctf());
    }
}

contract V2SettlementGovernanceCtfTest is V2SettlementGovernanceTest {
    function _newSettlementImplementation() internal override returns (address) {
        return address(new STSettlement2Ctf());
    }
}

contract V2SettlementStackCtfTest is V2SettlementStackTest {
    function _newSettlementImplementation() internal override returns (address) {
        return address(new STSettlement2Ctf());
    }
}

contract V2SettlementPhaseCCtfTest is V2SettlementPhaseCTest {
    function _newPhaseCImplementation() internal override returns (address) {
        return address(new STSettlement2Ctf());
    }
}

contract V2SettlementTimelockCtfTest is V2SettlementTimelockTest {
    function _newPhaseCImplementation() internal override returns (address) {
        return address(new STSettlement2Ctf());
    }
}
