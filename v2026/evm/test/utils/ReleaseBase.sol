// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Test} from "forge-std/Test.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

import {STCoordinator} from "../../src/STCoordinator.sol";
import {STSettlementVault} from "../../src/STSettlementVault.sol";
import {STReserveSink} from "../../src/STReserveSink.sol";
import {ISTAKING_ADDRESS} from "../../src/interfaces/stakingV2.sol";
import {INeuron_ADDRESS} from "../../src/interfaces/neuron.sol";
import {IALPHA_ADDRESS} from "../../src/interfaces/alpha.sol";
import {IED25519VERIFY_ADDRESS} from "../../src/interfaces/ed25519Verify.sol";
import {ISR25519VERIFY_ADDRESS} from "../../src/interfaces/sr25519Verify.sol";
import {MockStakingV2, MockNeuron, MockAlpha, MockEd25519, MockSr25519} from "../mocks/PrecompileMocks.sol";
import {MerkleBuilder} from "./MerkleBuilder.sol";

abstract contract ReleaseBase is Test {
    uint16 internal constant NETUID = 77;
    uint64 internal constant EPOCH_BLOCKS = 100;
    uint64 internal constant ROOT_WINDOW = 20;
    uint64 internal constant FINALIZE_OFFSET = 40;
    uint64 internal constant CLOSE_GRACE = 5;
    uint64 internal constant START_BLOCK = 1_000;
    uint64 internal constant REGISTRATION_BURN_LIMIT = 100_000_000;
    uint64 internal constant MINIMUM_TRANSFER_TAO_RAO = 1;
    uint64 internal constant MINIMUM_CLAIM_TTL_BLOCKS = EPOCH_BLOCKS * 8;

    uint256 internal constant NO1 = 1;
    uint256 internal constant NO2 = 2;
    bytes32 internal constant COORD_COLDKEY = keccak256("coordinator-coldkey");
    bytes32 internal constant VAULT_COLDKEY = keccak256("vault-coldkey");
    bytes32 internal constant SINK_COLDKEY = keccak256("sink-coldkey");
    bytes32 internal constant ESCROW_HOTKEY = keccak256("escrow-hotkey");
    bytes32 internal constant RESERVE_HOTKEY = keccak256("reserve-hotkey");
    bytes32 internal constant POOL1 = keccak256("pool-1");
    bytes32 internal constant POOL2 = keccak256("pool-2");
    bytes32 internal constant DEPOSIT1 = keccak256("deposit-1");
    bytes32 internal constant DEPOSIT2 = keccak256("deposit-2");
    bytes32 internal constant COLD1 = keccak256("operator-1-coldkey");
    bytes32 internal constant COLD2 = keccak256("operator-2-coldkey");
    bytes32 internal constant PROVIDER1 = keccak256("provider-1");
    bytes32 internal constant PROVIDER2 = keccak256("provider-2");

    address internal owner = makeAddr("release-owner");
    address internal guardian = makeAddr("release-guardian");
    address internal oracle = makeAddr("commitment-oracle");
    address internal depositSigner1 = makeAddr("deposit-signer-1");
    address internal depositSigner2 = makeAddr("deposit-signer-2");
    address internal rootSigner1 = makeAddr("root-signer-1");
    address internal rootSigner2 = makeAddr("root-signer-2");
    address internal stranger = makeAddr("stranger");

    STCoordinator internal coordinator;
    STCoordinator internal implementation;
    STSettlementVault internal vault;
    STReserveSink internal sink;

    MockStakingV2 internal staking = MockStakingV2(ISTAKING_ADDRESS);
    MockNeuron internal neuron = MockNeuron(INeuron_ADDRESS);
    MockAlpha internal alpha = MockAlpha(IALPHA_ADDRESS);
    MockEd25519 internal ed = MockEd25519(IED25519VERIFY_ADDRESS);
    MockSr25519 internal sr = MockSr25519(ISR25519VERIFY_ADDRESS);

    function setUp() public virtual {
        vm.chainId(945);
        vm.roll(START_BLOCK);
        vm.etch(ISTAKING_ADDRESS, address(new MockStakingV2()).code);
        vm.etch(INeuron_ADDRESS, address(new MockNeuron()).code);
        vm.etch(IALPHA_ADDRESS, address(new MockAlpha()).code);
        vm.etch(IED25519VERIFY_ADDRESS, address(new MockEd25519()).code);
        vm.etch(ISR25519VERIFY_ADDRESS, address(new MockSr25519()).code);

        sink = new STReserveSink(NETUID, RESERVE_HOTKEY, SINK_COLDKEY, address(this));
        vault = new STSettlementVault(
            NETUID,
            ESCROW_HOTKEY,
            VAULT_COLDKEY,
            MINIMUM_CLAIM_TTL_BLOCKS,
            MINIMUM_TRANSFER_TAO_RAO,
            address(this)
        );
        alpha.setAlphaPrice(NETUID, 1 ether);
        neuron.setUid(NETUID, ESCROW_HOTKEY, 10);
        vault.registerEscrow(REGISTRATION_BURN_LIMIT);
        implementation = new STCoordinator();
        STCoordinator.PolicySnapshot memory policy = _policy();
        ERC1967Proxy proxy = new ERC1967Proxy(
            address(implementation),
            abi.encodeCall(
                STCoordinator.initialize,
                (NETUID, owner, guardian, COORD_COLDKEY, vault, sink, oracle, policy)
            )
        );
        coordinator = STCoordinator(payable(address(proxy)));
        sink.setRecorderOnce(address(coordinator));
        vault.setCoordinatorOnce(address(coordinator));

        staking.setColdkey(address(coordinator), COORD_COLDKEY);
        staking.setColdkey(address(vault), VAULT_COLDKEY);
        staking.setColdkey(address(sink), SINK_COLDKEY);
        neuron.setUid(NETUID, POOL1, 11);
        neuron.setUid(NETUID, POOL2, 12);
        vm.startPrank(owner);
        coordinator.registerOperator(
            NO1, COLD1, POOL1, DEPOSIT1, depositSigner1, rootSigner1, 0, REGISTRATION_BURN_LIMIT
        );
        coordinator.registerOperator(
            NO2, COLD2, POOL2, DEPOSIT2, depositSigner2, rootSigner2, 0, REGISTRATION_BURN_LIMIT
        );
        vm.stopPrank();
    }

    function _policy() internal pure returns (STCoordinator.PolicySnapshot memory) {
        return STCoordinator.PolicySnapshot({
            policyHash: keccak256("release-policy-v1"),
            effectiveEpoch: 0,
            effectiveBlock: 0,
            epochBlocks: EPOCH_BLOCKS,
            rootCommitWindowBlocks: ROOT_WINDOW,
            finalizeOffsetBlocks: FINALIZE_OFFSET,
            closeGraceBlocks: CLOSE_GRACE,
            claimTTLEpochs: 8,
            claimGraceEpochs: 1,
            maximumBindingValidityEpochs: 32,
            commitmentMaxAgeBlocks: 200,
            epochDepositCapRao: 1_000_000,
            campaignDepositCapRao: 100_000_000
        });
    }

    function _pushDeposit(uint256 noId, uint256 amount) internal {
        bytes32 hotkey = noId == NO1 ? DEPOSIT1 : DEPOSIT2;
        staking.setStake(
            hotkey,
            COORD_COLDKEY,
            staking.stakes(hotkey, COORD_COLDKEY) + amount + coordinator.RESERVE_ROUNDING_ALLOWANCE_RAO()
        );
    }

    function _deposit(uint256 noId, uint256 amount, uint256 nonce) internal {
        _pushDeposit(noId, amount);
        address signer = noId == NO1 ? depositSigner1 : depositSigner2;
        vm.prank(signer);
        coordinator.deposit(noId, amount, nonce, uint64(block.number + 10));
    }

    function _accrue(uint256 noId, uint256 amount) internal {
        bytes32 hotkey = noId == NO1 ? POOL1 : POOL2;
        staking.setStake(hotkey, VAULT_COLDKEY, staking.stakes(hotkey, VAULT_COLDKEY) + amount);
    }

    function _end(uint256 epoch_) internal view returns (uint256) {
        return coordinator.epochEndBlock(epoch_);
    }

    function _close(uint256 epoch_, uint256 noId) internal returns (uint256) {
        vm.roll(_end(epoch_));
        return coordinator.closeOperatorEpoch(epoch_, noId);
    }

    function _singleLeaf(bytes32 coldkey, uint256 shareBps)
        internal
        view
        returns (bytes32 root, bytes32[] memory proof)
    {
        root = vault.payoutLeaf(coldkey, shareBps);
        proof = new bytes32[](0);
    }

    function _twoLeafTree()
        internal
        view
        returns (bytes32 root, bytes32[] memory proof1, bytes32[] memory proof2)
    {
        bytes32[] memory leaves = new bytes32[](2);
        leaves[0] = vault.payoutLeaf(PROVIDER1, 6_000);
        leaves[1] = vault.payoutLeaf(PROVIDER2, 4_000);
        root = MerkleBuilder.root(leaves);
        proof1 = MerkleBuilder.proof(leaves, 0);
        proof2 = MerkleBuilder.proof(leaves, 1);
    }
}
