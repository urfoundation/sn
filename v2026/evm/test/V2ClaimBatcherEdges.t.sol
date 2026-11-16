// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {Vm, VmSafe} from "forge-std/Vm.sol";

import {STClaimBatcher} from "../src/STClaimBatcher.sol";
import {ISTSettlementVault2} from "../src/interfaces/ISTSettlementVault2.sol";
import {LedgerTree} from "../src/LedgerTree.sol";
import {V2VaultBase} from "./utils/V2VaultBase.sol";
import {V2TreeHelper} from "./utils/V2TreeHelper.sol";

/// @dev Records the gas left at the start of each call and pays 1, as the
///      suite's V2GasProbeVault does.
contract EdgeGasProbeVault {
    uint256[] private _received;

    function received() external view returns (uint256[] memory) {
        return _received;
    }

    fallback() external {
        _received.push(gasleft());
        assembly {
            mstore(0, 1)
            return(0, 32)
        }
    }
}

/// @dev Reverts every call with `size` bytes of revert data.
contract EdgeBombVault {
    uint256 public immutable size;

    constructor(uint256 size_) {
        size = size_;
    }

    fallback() external {
        uint256 n = size;
        assembly {
            revert(0, n)
        }
    }
}

/// @dev Reverts every call with the two bytes 0xabcd.
contract EdgeShortRevertVault {
    fallback() external {
        assembly {
            mstore(0, shl(240, 0xabcd))
            revert(0, 2)
        }
    }
}

/// @dev Answers every call with success and no return data.
contract EdgeSilentVault {
    fallback() external {}
}

/// @dev Re-enters the batcher from its first call, with itself as the vault,
///      then pays 7.
contract EdgeReentrantVault {
    STClaimBatcher public immutable batcher;
    bool private _entered;

    constructor(STClaimBatcher batcher_) {
        batcher = batcher_;
    }

    fallback() external {
        if (!_entered) {
            _entered = true;
            batcher.claimMany(ISTSettlementVault2(address(this)), new STClaimBatcher.Claim[](2), 100_000);
        }
        assembly {
            mstore(0, 7)
            return(0, 32)
        }
    }
}

/// @dev STClaimBatcher's edges, from the review of snv2 77f86ead: the gas an
///      item receives when its proof is as long as a real one and the vault's
///      address is cold (CB-4), and what a vault other than vault2 can do to a
///      batch, which only its sender chooses (CB-8).
contract V2ClaimBatcherEdgesTest is Test {
    uint256 internal constant CAP = 2_600_000;

    STClaimBatcher internal batcher;

    function setUp() public {
        batcher = new STClaimBatcher();
    }

    function _needed(uint256 cap) internal view returns (uint256) {
        return cap + cap / 63 + batcher.BOOKKEEPING_GAS();
    }

    function _items(uint256 count, uint256 proofLength)
        internal
        pure
        returns (STClaimBatcher.Claim[] memory items)
    {
        items = new STClaimBatcher.Claim[](count);
        for (uint256 i; i < count; i++) {
            items[i].noId = 1;
            items[i].account = LedgerTree.Account(i, keccak256(abi.encode(i)), 1e9 + i);
            items[i].proof = new LedgerTree.ProofNode[](proofLength);
            for (uint256 k; k < proofLength; k++) {
                items[i].proof[k] = LedgerTree.ProofNode(keccak256(abi.encode(i, k)), 1e9 + k);
            }
        }
    }

    // The gas the probe's fallback spends before it reads the gas left.
    function _probeOverhead() internal returns (uint256) {
        EdgeGasProbeVault probe = new EdgeGasProbeVault();
        (bool ok,) = address(probe).call{gas: 100_000}(
            abi.encodeCall(
                ISTSettlementVault2.claim, (1, 0, LedgerTree.Account(0, 0, 0), new LedgerTree.ProofNode[](0))
            )
        );
        assertTrue(ok);
        return 100_000 - probe.received()[0];
    }

    // The least gas at which a batch of one item of `proofLength` siblings is
    // attempted, and the gas its claim then receives. `cold` makes the vault's
    // address cold, as it is for a transaction's first item.
    function _atTheRequirement(uint256 proofLength, bool cold) internal returns (uint256 least, uint256 got) {
        EdgeGasProbeVault probe = new EdgeGasProbeVault();
        bytes memory data = abi.encodeCall(
            STClaimBatcher.claimMany, (ISTSettlementVault2(address(probe)), _items(1, proofLength), CAP)
        );
        uint256 low = 21_000;
        uint256 high = 4 * _needed(CAP);
        while (high - low > 1) {
            uint256 middle = (low + high) / 2;
            uint256 snapshot = vm.snapshotState();
            if (cold) vm.cool(address(probe));
            (bool ok, bytes memory out) = address(batcher).call{gas: middle}(data);
            bool attempted = ok && abi.decode(out, (uint256)) == 1;
            vm.revertToState(snapshot);
            if (attempted) high = middle;
            else low = middle;
        }
        least = high;
        if (cold) vm.cool(address(probe));
        (bool sent, bytes memory result) = address(batcher).call{gas: least}(data);
        assertTrue(sent, "the batch at its requirement completed");
        assertEq(abi.decode(result, (uint256)), 1);
        got = probe.received()[0];
    }

    // CB-4: an attempted item receives exactly its cap, also with the 29
    // siblings of the deepest ledger and the vault's address cold: the 20,000
    // gas of bookkeeping cover the encoding. They stop covering it for proofs
    // no ledger has (vault2 refuses any proof longer than 29). The encoding's
    // gas is the release build's; a coverage build is unoptimized and
    // instrumented, so it is skipped, as are the next two.
    function test_CB4_anItemWithARealProofReceivesItsCapAtItsRequirement() public {
        vm.skip(vm.isContext(VmSafe.ForgeContext.Coverage));
        uint256 overhead = _probeOverhead();
        emit log_named_uint("probe overhead", overhead);
        uint256[7] memory lengths = [uint256(0), 1, 29, 64, 128, 256, 1024];
        for (uint256 i; i < lengths.length; i++) {
            for (uint256 cold; cold < 2; cold++) {
                (uint256 least, uint256 got) = _atTheRequirement(lengths[i], cold == 1);
                emit log_named_uint(
                    string.concat(
                        "proof length ", vm.toString(lengths[i]), cold == 1 ? " cold" : " warm", ": least gas"
                    ),
                    least
                );
                emit log_named_uint("  gas received", got + overhead);
                if (lengths[i] <= 29) {
                    assertEq(got + overhead, CAP, "an item with a real proof received its whole cap");
                }
            }
        }
    }

    // CB-4: the last of the most claims one transaction holds (418 at depth
    // 29, R-7), attempted from the least gas at which the batch attempts them
    // all, still receives its whole cap: the batcher's memory does not grow
    // with the items it encodes.
    function test_CB4_theLastItemOfAFullBatchReceivesItsCap() public {
        vm.skip(vm.isContext(VmSafe.ForgeContext.Coverage));
        uint256 overhead = _probeOverhead();
        uint256 count = 418;
        uint256 cap = 100_000;
        EdgeGasProbeVault probe = new EdgeGasProbeVault();
        bytes memory data = abi.encodeCall(
            STClaimBatcher.claimMany, (ISTSettlementVault2(address(probe)), _items(count, 29), cap)
        );
        uint256 low = 21_000;
        uint256 high = count * (_needed(cap) + 50_000);
        while (high - low > 1) {
            uint256 middle = (low + high) / 2;
            uint256 snapshot = vm.snapshotState();
            vm.cool(address(probe));
            (bool ok, bytes memory out) = address(batcher).call{gas: middle}(data);
            bool all = ok && abi.decode(out, (uint256)) == count;
            vm.revertToState(snapshot);
            if (all) high = middle;
            else low = middle;
        }
        vm.cool(address(probe));
        (bool sent, bytes memory result) = address(batcher).call{gas: high}(data);
        assertTrue(sent);
        assertEq(abi.decode(result, (uint256)), count);
        uint256[] memory received = probe.received();
        emit log_named_uint("least gas for all", high);
        emit log_named_uint("first item received", received[0] + overhead);
        emit log_named_uint("last item received", received[count - 1] + overhead);
        assertEq(received[count - 1] + overhead, cap, "the last item received its whole cap");
    }

    // CB-4: a batch of items with 29-sibling proofs never runs out of gas,
    // whatever its gas, and attempts a prefix; each attempted item receives
    // its cap.
    function testFuzz_CB4_aBatchOfRealProofsCompletesAtAnyGas(uint256 limit) public {
        vm.skip(vm.isContext(VmSafe.ForgeContext.Coverage));
        EdgeGasProbeVault probe = new EdgeGasProbeVault();
        uint256 cap = 200_000;
        limit = bound(limit, 80_000, 6 * _needed(cap));
        bytes memory data = abi.encodeCall(
            STClaimBatcher.claimMany, (ISTSettlementVault2(address(probe)), _items(5, 29), cap)
        );
        vm.cool(address(probe));
        (bool ok, bytes memory result) = address(batcher).call{gas: limit}(data);
        assertTrue(ok, "the batch completed");
        uint256 attempted = abi.decode(result, (uint256));
        uint256[] memory received = probe.received();
        assertEq(received.length, attempted);
        for (uint256 i; i < attempted; i++) {
            assertLe(received[i], cap);
            assertGe(received[i], cap - 200, "the whole cap");
        }
    }

    // CB-8: a vault other than vault2 can revert the whole batch: the batcher
    // copies all the revert data it is handed, and 1 MiB of it costs more than
    // the gas the batcher keeps. vault2 returns at most a 4-byte error, a
    // 36-byte panic or what a precompile returns, so this needs a hostile
    // `vault` argument, which only the sender chooses.
    function test_CB8_aVaultReturningMuchRevertDataRevertsTheBatch() public {
        EdgeBombVault bomb = new EdgeBombVault(1 << 20);
        bytes memory data = abi.encodeCall(
            STClaimBatcher.claimMany, (ISTSettlementVault2(address(bomb)), _items(3, 29), CAP)
        );
        (bool ok,) = address(batcher).call{gas: 3 * _needed(CAP) + 200_000}(data);
        assertFalse(ok, "the batch survived 1 MiB of revert data");

        // Revert data of a few words is reported, by its first four bytes.
        EdgeBombVault small = new EdgeBombVault(100);
        vm.recordLogs();
        assertEq(batcher.claimMany(ISTSettlementVault2(address(small)), _items(2, 29), CAP), 2);
        Vm.Log[] memory logs = vm.getRecordedLogs();
        assertEq(logs.length, 2);
        assertEq(logs[0].topics[0], STClaimBatcher.ClaimRefused.selector);
        assertEq(logs[0].data, abi.encode(uint256(0), bytes4(0)));
    }

    // CB-8: a `vault` without code, or one that returns fewer than 32 bytes,
    // reverts the batch: the decoding of a successful call's return data is
    // outside the try.
    function test_CB8_aVaultWithoutAnAnswerRevertsTheBatch() public {
        STClaimBatcher.Claim[] memory items = _items(2, 29);
        vm.expectRevert();
        batcher.claimMany(ISTSettlementVault2(makeAddr("no code")), items, CAP);
        EdgeSilentVault silent = new EdgeSilentVault();
        vm.expectRevert();
        batcher.claimMany(ISTSettlementVault2(address(silent)), items, CAP);
    }

    // CB-8: a vault other than vault2 can re-enter the batcher. The inner
    // batch's events come from the batcher too, inside the outer batch's, so
    // a receipt then holds batcher events that do not count the outer batch's
    // items in order, which the claim tools refuse. vault2 calls only
    // precompiles and cannot do this.
    function test_CB8_aReentrantVaultInterleavesBatcherEvents() public {
        EdgeReentrantVault vault = new EdgeReentrantVault(batcher);
        vm.recordLogs();
        assertEq(batcher.claimMany(ISTSettlementVault2(address(vault)), _items(2, 0), CAP), 2);
        Vm.Log[] memory logs = vm.getRecordedLogs();
        assertEq(logs.length, 4);
        // Inner items 0 and 1, then outer items 0 and 1.
        assertEq(logs[0].data, abi.encode(uint256(0), uint256(7)));
        assertEq(logs[1].data, abi.encode(uint256(1), uint256(7)));
        assertEq(logs[2].data, abi.encode(uint256(0), uint256(7)));
        assertEq(logs[3].data, abi.encode(uint256(1), uint256(7)));
    }

    // CB-2: revert data shorter than four bytes is reported zero-padded, and
    // the batch goes on.
    function test_CB2_shortRevertDataIsReportedPadded() public {
        EdgeShortRevertVault vault = new EdgeShortRevertVault();
        vm.recordLogs();
        assertEq(batcher.claimMany(ISTSettlementVault2(address(vault)), _items(2, 29), CAP), 2);
        Vm.Log[] memory logs = vm.getRecordedLogs();
        assertEq(logs.length, 2);
        assertEq(logs[0].data, abi.encode(uint256(0), bytes4(0xabcd0000)));
        assertEq(logs[1].data, abi.encode(uint256(1), bytes4(0xabcd0000)));
    }

    // CB-2: gasPerClaim 0 refuses every item without data, as a runtime
    // refusal reports: the batcher cannot tell a cap that is too small from a
    // refusal.
    function test_CB2_aZeroCapRefusesEveryItem() public {
        EdgeGasProbeVault probe = new EdgeGasProbeVault();
        vm.recordLogs();
        assertEq(batcher.claimMany(ISTSettlementVault2(address(probe)), _items(3, 29), 0), 3);
        Vm.Log[] memory logs = vm.getRecordedLogs();
        assertEq(logs.length, 3);
        for (uint256 i; i < 3; i++) {
            assertEq(logs[i].topics[0], STClaimBatcher.ClaimRefused.selector);
            assertEq(logs[i].data, abi.encode(i, bytes4(0)));
        }
        assertEq(probe.received().length, 0);
    }
}

/// @dev STClaimBatcher against vault2 on the precompile mocks: duplicate
///      items and items of several pools in one batch.
contract V2ClaimBatcherEdgesVaultTest is V2VaultBase {
    STClaimBatcher internal batcher;

    function setUp() public override {
        super.setUp();
        batcher = new STClaimBatcher();
        _registerPool2();
    }

    function _item(
        uint256 noId,
        V2TreeHelper.Node[][] memory levels,
        uint256 index,
        bytes32 coldkey,
        uint256 value
    ) internal pure returns (STClaimBatcher.Claim memory) {
        return STClaimBatcher.Claim({
            noId: noId,
            rootIndex: 0,
            account: _account(index, coldkey, value),
            proof: V2TreeHelper.proof(levels, index)
        });
    }

    // CB-1: one batch claims Alice in pool 1 and in pool 2, Alice in pool 1
    // again and Carol in pool 2: each account pays once to its leaf's
    // coldkey, the duplicate reports NothingToClaim, and vault2 names the
    // batcher as the caller of every Claimed.
    function test_CB1_duplicatesAndSeveralPools() public {
        V2TreeHelper.Node[][] memory one = _publish(NO1, _keys(ALICE, BOB), _values(500, 600));
        V2TreeHelper.Node[][] memory two = _publish(NO2, _keys(ALICE, CAROL), _values(300, 400));
        STClaimBatcher.Claim[] memory items = new STClaimBatcher.Claim[](4);
        items[0] = _item(NO1, one, 0, ALICE, 500);
        items[1] = _item(NO2, two, 0, ALICE, 300);
        items[2] = _item(NO1, one, 0, ALICE, 500);
        items[3] = _item(NO2, two, 1, CAROL, 400);
        vm.recordLogs();
        assertEq(batcher.claimMany(vault, items, 1_000_000), 4);
        Vm.Log[] memory logs = vm.getRecordedLogs();
        assertEq(logs.length, 7);
        bytes32 claimed = ISTSettlementVault2.Claimed.selector;
        uint256[3] memory pools = [NO1, NO2, NO2];
        uint256[3] memory indexes = [uint256(0), 0, 1];
        bytes32[3] memory keys = [ALICE, ALICE, CAROL];
        uint256[3] memory amounts = [uint256(500), 300, 400];
        uint256[3] memory at = [uint256(0), 2, 5];
        uint256[3] memory items_ = [uint256(0), 1, 3];
        for (uint256 i; i < 3; i++) {
            Vm.Log memory log = logs[at[i]];
            assertEq(log.emitter, address(vault));
            assertEq(log.topics[0], claimed);
            assertEq(log.topics[1], bytes32(pools[i]));
            assertEq(log.topics[2], bytes32(indexes[i]));
            assertEq(log.topics[3], keys[i]);
            assertEq(log.data, abi.encode(uint256(0), amounts[i], amounts[i], address(batcher)));
            assertEq(logs[at[i] + 1].emitter, address(batcher));
            assertEq(logs[at[i] + 1].data, abi.encode(items_[i], amounts[i]));
        }
        assertEq(logs[4].emitter, address(batcher));
        assertEq(logs[4].topics[0], STClaimBatcher.ClaimRefused.selector);
        assertEq(logs[4].data, abi.encode(uint256(2), ISTSettlementVault2.NothingToClaim.selector));
        assertEq(staking.stakes(ESCROW, ALICE), 800);
        assertEq(staking.stakes(ESCROW, CAROL), 400);
        assertEq(vault.claimedTotal(NO1), 500);
        assertEq(vault.claimedTotal(NO2), 700);
        assertTrue(vault.conservationHolds());
    }
}
