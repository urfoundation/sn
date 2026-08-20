// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";

import {IStaking, ISTAKING_ADDRESS} from "./interfaces/stakingV2.sol";
import {INeuron, INeuron_ADDRESS} from "./interfaces/neuron.sol";
import {IEd25519Verify, IED25519VERIFY_ADDRESS} from "./interfaces/ed25519Verify.sol";
import {ISR25519Verify, ISR25519VERIFY_ADDRESS} from "./interfaces/sr25519Verify.sol";
import {STReserveSink} from "./STReserveSink.sol";
import {STSettlementVault} from "./STSettlementVault.sol";

/// @title STCoordinator
/// @notice Upgradeable release-1.0 policy/roles/binding coordinator. Economic
/// custody is deliberately outside this proxy in immutable contracts.
contract STCoordinator is Initializable, OwnableUpgradeable, UUPSUpgradeable {
    string public constant FLEET_BINDING_DOMAIN = "urnetwork/fleet-binding/v1";
    string public constant FLEET_REVOKE_DOMAIN = "urnetwork/fleet-revoke/v1";
    uint256 public constant MAX_POLICY_VERSIONS = 64;
    uint256 public constant MAX_OPERATOR_VERSIONS = 64;

    struct PolicySnapshot {
        bytes32 policyHash;
        uint64 effectiveEpoch;
        uint64 effectiveBlock;
        uint64 epochBlocks;
        uint64 rootCommitWindowBlocks;
        uint64 finalizeOffsetBlocks;
        uint64 closeGraceBlocks;
        uint64 claimTTLEpochs;
        uint64 claimGraceEpochs;
        uint64 maximumBindingValidityEpochs;
        uint64 commitmentMaxAgeBlocks;
        uint256 epochDepositCapRao;
        uint256 campaignDepositCapRao;
    }

    struct OperatorVersion {
        bytes32 coldkey;
        bytes32 poolHotkey;
        bytes32 depositHotkey;
        address depositSigner;
        address rootSigner;
        uint64 effectiveEpoch;
        bool active;
    }

    struct RootCommitment {
        bytes32 payoutRoot;
        bytes32 artifactHash;
        address committer;
        uint64 commitBlock;
    }

    struct FleetBinding {
        uint64 chainId;
        uint16 netuid;
        address coordinator;
        bytes32 fleetId;
        bytes32 hotkey;
        bytes16 clientId;
        bytes32 clientKey;
        uint64 generation;
        uint64 validFromEpoch;
        uint64 validToEpoch;
        bytes32 commitmentHash;
    }

    struct BindingRecord {
        bytes32 fleetId;
        bytes32 hotkey;
        bytes32 clientKey;
        bytes32 commitmentHash;
        uint64 generation;
        uint64 validFromEpoch;
        uint64 validToEpoch;
        uint64 cleanedAtEpoch;
        uint16 uid;
        bool cleaned;
    }

    struct CommitmentRecord {
        bytes32 commitmentHash;
        bytes32 finalizedBlockHash;
        uint64 finalizedBlock;
    }

    uint16 public netuid;
    bytes32 public selfColdkey;
    STSettlementVault public settlementVault;
    STReserveSink public reserveSink;
    address public guardian;
    address public pendingGuardian;
    uint64 public pendingGuardianEpoch;
    bool public paused;

    PolicySnapshot[] private _policies;
    mapping(uint256 noId => OperatorVersion[] versions) private _operatorVersions;
    uint256[] private _operatorIds;
    mapping(bytes32 hotkey => bool used) public depositHotkeyUsed;

    mapping(uint256 noId => uint256 nonce) public nextDepositNonce;
    mapping(uint256 epoch => mapping(uint256 noId => uint256 amount)) public epochDeposits;
    mapping(uint256 epoch => mapping(uint256 noId => uint256 amount)) public epochConvictionAdded;
    mapping(uint256 noId => uint256 amount) public cumulativeConviction;
    uint256 public campaignReserved;

    mapping(uint256 epoch => mapping(uint256 noId => RootCommitment commitment)) public rootCommitments;

    address public commitmentOracle;
    address public pendingCommitmentOracle;
    uint64 public pendingCommitmentOracleEpoch;
    mapping(bytes32 hotkey => CommitmentRecord record) public mirroredCommitments;
    mapping(bytes16 clientId => mapping(uint256 index => BindingRecord record)) private _bindingVersions;
    mapping(bytes16 clientId => uint256 count) private _bindingVersionCounts;
    /// @notice Number of clients whose latest scheduled, non-cleaned binding
    /// names a fleet. Historical membership is queried with bindingAt.
    mapping(bytes32 fleetId => uint256 members) public fleetMemberCount;

    uint256 private _entered;

    event PolicyScheduled(
        uint256 indexed index,
        bytes32 indexed policyHash,
        uint64 indexed effectiveEpoch,
        uint64 effectiveBlock
    );
    event OperatorScheduled(
        uint256 indexed noId,
        uint64 indexed effectiveEpoch,
        bytes32 coldkey,
        bytes32 poolHotkey,
        bytes32 depositHotkey,
        address depositSigner,
        address rootSigner,
        bool active
    );
    event Deposit(
        uint256 indexed noId,
        uint256 indexed epoch,
        address indexed funder,
        uint256 amount,
        bytes32 policyHash,
        uint256 nonce
    );
    event ConvictionAdded(
        uint256 indexed noId,
        uint256 indexed epoch,
        address indexed funder,
        uint256 amount,
        bytes32 policyHash,
        uint256 nonce
    );
    event OperatorRootCommitted(
        uint256 indexed epoch,
        uint256 indexed noId,
        bytes32 payoutRoot,
        bytes32 artifactHash,
        address committer
    );
    event OperatorEpochFinalized(uint256 indexed epoch, uint256 indexed noId, bool rootPresent);
    event CommitmentOracleScheduled(address indexed oracle, uint64 indexed effectiveEpoch);
    event CommitmentMirrored(
        bytes32 indexed hotkey,
        bytes32 indexed commitmentHash,
        uint64 finalizedBlock,
        bytes32 finalizedBlockHash
    );
    event FleetBound(
        bytes16 indexed clientId,
        bytes32 indexed fleetId,
        bytes32 indexed hotkey,
        uint16 uid,
        uint64 generation,
        uint64 validFromEpoch,
        uint64 validToEpoch
    );
    event FleetBindingRevoked(bytes16 indexed clientId, uint64 generation, uint64 effectiveEpoch);
    event FleetBindingCleaned(bytes16 indexed clientId, uint64 indexed cleanedAtEpoch);
    event GuardianSet(address indexed guardian);
    event GuardianScheduled(address indexed guardian, uint64 indexed effectiveEpoch);
    event PausedSet(bool paused, address indexed caller);

    error Unauthorized();
    error InvalidConfiguration();
    error InvalidPolicy();
    error InvalidEpoch();
    error InvalidWindow();
    error UnknownOperator();
    error InactiveOperator();
    error InvalidNonce();
    error DeadlineExpired();
    error CapExceeded();
    error FundsNotReceived();
    error AlreadyCommitted();
    error InvalidSignature();
    error InvalidBinding();
    error StaleCommitment();
    error RuntimeIdentityMissing();
    error NativeRefundFailed();
    error Paused();
    error Reentrancy();

    modifier whenNotPaused() {
        if (paused) revert Paused();
        _;
    }

    modifier nonReentrant() {
        if (_entered != 0) revert Reentrancy();
        _entered = 1;
        _;
        _entered = 0;
    }

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        uint16 netuid_,
        address owner_,
        address guardian_,
        bytes32 selfColdkey_,
        STSettlementVault settlementVault_,
        STReserveSink reserveSink_,
        address commitmentOracle_,
        PolicySnapshot calldata initialPolicy
    ) external initializer {
        if (
            netuid_ == 0 || owner_ == address(0) || guardian_ == address(0) || selfColdkey_ == bytes32(0)
                || address(settlementVault_) == address(0) || address(reserveSink_) == address(0)
                || commitmentOracle_ == address(0)
        ) revert InvalidConfiguration();
        if (
            settlementVault_.netuid() != netuid_ || reserveSink_.netuid() != netuid_
                || settlementVault_.escrowHotkey() == reserveSink_.reserveHotkey()
                || !settlementVault_.escrowRegistered()
        ) revert InvalidConfiguration();
        __Ownable_init(owner_);
        netuid = netuid_;
        guardian = guardian_;
        selfColdkey = selfColdkey_;
        settlementVault = settlementVault_;
        reserveSink = reserveSink_;
        commitmentOracle = commitmentOracle_;

        PolicySnapshot memory first = initialPolicy;
        first.effectiveEpoch = 0;
        first.effectiveBlock = uint64(block.number);
        _validatePolicy(first);
        _policies.push(first);
        emit PolicyScheduled(0, first.policyHash, 0, first.effectiveBlock);
        emit GuardianSet(guardian_);
    }

    receive() external payable {}

    function policyCount() external view returns (uint256) {
        return _policies.length;
    }

    function policyByIndex(uint256 index) external view returns (PolicySnapshot memory) {
        return _policies[index];
    }

    function policyAt(uint256 epoch_) public view returns (PolicySnapshot memory) {
        for (uint256 i = _policies.length; i != 0; i--) {
            if (_policies[i - 1].effectiveEpoch <= epoch_) return _policies[i - 1];
        }
        revert InvalidPolicy();
    }

    function currentEpoch() public view returns (uint256) {
        for (uint256 i = _policies.length; i != 0; i--) {
            PolicySnapshot storage p = _policies[i - 1];
            if (block.number >= p.effectiveBlock) {
                return p.effectiveEpoch + (block.number - uint256(p.effectiveBlock)) / uint256(p.epochBlocks);
            }
        }
        revert InvalidEpoch();
    }

    function epochStartBlock(uint256 epoch_) public view returns (uint256) {
        PolicySnapshot memory p = policyAt(epoch_);
        return uint256(p.effectiveBlock) + (epoch_ - uint256(p.effectiveEpoch)) * uint256(p.epochBlocks);
    }

    function epochEndBlock(uint256 epoch_) public view returns (uint256) {
        PolicySnapshot memory p = policyAt(epoch_);
        return epochStartBlock(epoch_) + uint256(p.epochBlocks);
    }

    function schedulePolicy(PolicySnapshot calldata next) external onlyOwner {
        if (_policies.length >= MAX_POLICY_VERSIONS) revert InvalidPolicy();
        uint256 nowEpoch = currentEpoch();
        PolicySnapshot storage last = _policies[_policies.length - 1];
        if (next.effectiveEpoch <= nowEpoch || next.effectiveEpoch <= last.effectiveEpoch) {
            revert InvalidEpoch();
        }
        PolicySnapshot memory scheduled = next;
        scheduled.effectiveBlock = uint64(epochStartBlock(next.effectiveEpoch));
        _validatePolicy(scheduled);
        _policies.push(scheduled);
        emit PolicyScheduled(
            _policies.length - 1, scheduled.policyHash, scheduled.effectiveEpoch, scheduled.effectiveBlock
        );
    }

    // Exact equality is required for configuration sentinels; this function
    // never compares a market-controlled price or balance.
    // slither-disable-next-line incorrect-equality
    function _validatePolicy(PolicySnapshot memory p) internal pure {
        // Exact zero sentinels are intentional configuration-validity checks,
        // not price or balance comparisons.
        if (
            p.policyHash == bytes32(0) || p.epochBlocks == 0 || p.rootCommitWindowBlocks == 0
                || p.finalizeOffsetBlocks == 0 || p.closeGraceBlocks == 0 || p.claimTTLEpochs == 0
                || p.maximumBindingValidityEpochs == 0 || p.commitmentMaxAgeBlocks == 0
                || p.epochDepositCapRao == 0 || p.campaignDepositCapRao == 0
        ) revert InvalidPolicy();
        if (
            p.closeGraceBlocks > p.rootCommitWindowBlocks || p.rootCommitWindowBlocks > p.finalizeOffsetBlocks
                || p.claimGraceEpochs > p.claimTTLEpochs || p.epochDepositCapRao > p.campaignDepositCapRao
        ) revert InvalidPolicy();
    }

    function operatorCount() external view returns (uint256) {
        return _operatorIds.length;
    }

    function operatorIdAt(uint256 index) external view returns (uint256) {
        return _operatorIds[index];
    }

    function operatorVersionCount(uint256 noId) external view returns (uint256) {
        return _operatorVersions[noId].length;
    }

    function operatorAt(uint256 noId, uint256 epoch_) public view returns (OperatorVersion memory) {
        OperatorVersion[] storage versions = _operatorVersions[noId];
        for (uint256 i = versions.length; i != 0; i--) {
            if (versions[i - 1].effectiveEpoch <= epoch_) return versions[i - 1];
        }
        revert UnknownOperator();
    }

    function registerOperator(
        uint256 noId,
        bytes32 coldkey,
        bytes32 poolHotkey,
        bytes32 depositHotkey,
        address depositSigner,
        address rootSigner,
        uint64 effectiveEpoch,
        uint64 maximumBurnRao
    ) external payable onlyOwner nonReentrant returns (uint16 uid) {
        if (_operatorVersions[noId].length != 0 || noId == 0) {
            revert InvalidConfiguration();
        }
        if (effectiveEpoch < currentEpoch()) revert InvalidEpoch();
        uint256 balanceBefore = address(this).balance - msg.value;
        _validateOperator(coldkey, poolHotkey, depositHotkey, depositSigner, rootSigner);
        if (depositHotkeyUsed[depositHotkey]) revert InvalidConfiguration();
        depositHotkeyUsed[depositHotkey] = true;

        // Reserve the complete coordinator identity before crossing into the
        // runtime-backed vault. Reverts unwind these effects atomically.
        _operatorVersions[noId].push(
            OperatorVersion({
                coldkey: coldkey,
                poolHotkey: poolHotkey,
                depositHotkey: depositHotkey,
                depositSigner: depositSigner,
                rootSigner: rootSigner,
                effectiveEpoch: effectiveEpoch,
                active: true
            })
        );
        _operatorIds.push(noId);
        uid = settlementVault.registerPool{value: msg.value}(noId, poolHotkey, maximumBurnRao);
        emit OperatorScheduled(
            noId, effectiveEpoch, coldkey, poolHotkey, depositHotkey, depositSigner, rootSigner, true
        );
        uint256 current = address(this).balance;
        if (current > balanceBefore) {
            (bool refunded,) = payable(msg.sender).call{value: current - balanceBefore}("");
            if (!refunded) revert NativeRefundFailed();
        }
    }

    function scheduleOperator(
        uint256 noId,
        bytes32 depositHotkey,
        address depositSigner,
        address rootSigner,
        bool active,
        uint64 effectiveEpoch
    ) external onlyOwner {
        OperatorVersion[] storage versions = _operatorVersions[noId];
        if (versions.length == 0 || versions.length >= MAX_OPERATOR_VERSIONS) {
            revert UnknownOperator();
        }
        if (
            effectiveEpoch <= currentEpoch() || effectiveEpoch <= versions[versions.length - 1].effectiveEpoch
        ) revert InvalidEpoch();
        OperatorVersion storage prior = versions[versions.length - 1];
        _validateOperator(prior.coldkey, prior.poolHotkey, depositHotkey, depositSigner, rootSigner);
        if (depositHotkey != prior.depositHotkey) {
            if (depositHotkeyUsed[depositHotkey]) revert InvalidConfiguration();
            depositHotkeyUsed[depositHotkey] = true;
        }
        versions.push(
            OperatorVersion({
                coldkey: prior.coldkey,
                poolHotkey: prior.poolHotkey,
                depositHotkey: depositHotkey,
                depositSigner: depositSigner,
                rootSigner: rootSigner,
                effectiveEpoch: effectiveEpoch,
                active: active
            })
        );
        emit OperatorScheduled(
            noId,
            effectiveEpoch,
            prior.coldkey,
            prior.poolHotkey,
            depositHotkey,
            depositSigner,
            rootSigner,
            active
        );
    }

    function _validateOperator(
        bytes32 coldkey,
        bytes32 poolHotkey,
        bytes32 depositHotkey,
        address depositSigner,
        address rootSigner
    ) internal view {
        if (
            coldkey == bytes32(0) || poolHotkey == bytes32(0) || depositHotkey == bytes32(0)
                || depositSigner == address(0) || rootSigner == address(0) || depositSigner == rootSigner
                || depositHotkey == poolHotkey || depositHotkey == settlementVault.escrowHotkey()
                || depositHotkey == reserveSink.reserveHotkey()
        ) revert InvalidConfiguration();
    }

    function deposit(uint256 noId, uint256 amount, uint256 nonce, uint64 deadlineBlock)
        external
        whenNotPaused
        nonReentrant
    {
        _reserve(noId, amount, nonce, deadlineBlock, true);
    }

    function addConviction(uint256 noId, uint256 amount, uint256 nonce, uint64 deadlineBlock)
        external
        whenNotPaused
        nonReentrant
    {
        _reserve(noId, amount, nonce, deadlineBlock, false);
    }

    function _reserve(uint256 noId, uint256 amount, uint256 nonce, uint64 deadlineBlock, bool demand)
        internal
    {
        uint256 epoch_ = currentEpoch();
        OperatorVersion memory op = operatorAt(noId, epoch_);
        if (!op.active) revert InactiveOperator();
        if (msg.sender != op.depositSigner) revert Unauthorized();
        if (block.number > deadlineBlock) revert DeadlineExpired();
        if (nonce != nextDepositNonce[noId]) revert InvalidNonce();
        if (amount == 0) revert InvalidConfiguration();
        PolicySnapshot memory p = policyAt(epoch_);
        if (demand && epochDeposits[epoch_][noId] + amount > p.epochDepositCapRao) {
            revert CapExceeded();
        }
        if (campaignReserved + amount > p.campaignDepositCapRao) revert CapExceeded();

        uint256 available =
            IStaking(ISTAKING_ADDRESS).getStake(op.depositHotkey, selfColdkey, uint256(netuid));
        if (available < amount) revert FundsNotReceived();
        // Commit all coordinator accounting before external runtime calls.
        // Any failed precompile or sink check reverts the complete transaction.
        nextDepositNonce[noId] = nonce + 1;
        campaignReserved += amount;
        cumulativeConviction[noId] += amount;
        if (demand) {
            epochDeposits[epoch_][noId] += amount;
        } else {
            epochConvictionAdded[epoch_][noId] += amount;
        }

        IStaking(ISTAKING_ADDRESS)
            .moveStake(
                op.depositHotkey, reserveSink.reserveHotkey(), uint256(netuid), uint256(netuid), amount
            );
        IStaking(ISTAKING_ADDRESS)
            .transferStake(
                reserveSink.selfColdkey(),
                reserveSink.reserveHotkey(),
                uint256(netuid),
                uint256(netuid),
                amount
            );
        reserveSink.recordPrincipal(epoch_, noId, amount);

        if (demand) {
            emit Deposit(noId, epoch_, msg.sender, amount, p.policyHash, nonce);
        } else {
            emit ConvictionAdded(noId, epoch_, msg.sender, amount, p.policyHash, nonce);
        }
    }

    function closeOperatorEpoch(uint256 epoch_, uint256 noId)
        external
        whenNotPaused
        nonReentrant
        returns (uint256 amount)
    {
        OperatorVersion memory op = operatorAt(noId, epoch_);
        if (!op.active) revert InactiveOperator();
        PolicySnapshot memory p = policyAt(epoch_);
        uint256 end = epochEndBlock(epoch_);
        if (block.number < end || block.number > end + p.closeGraceBlocks) {
            revert InvalidWindow();
        }
        return settlementVault.captureEmission(epoch_, noId);
    }

    function deferMissedEmission(uint256 epoch_, uint256 noId) external whenNotPaused {
        operatorAt(noId, epoch_);
        PolicySnapshot memory p = policyAt(epoch_);
        if (block.number <= epochEndBlock(epoch_) + p.closeGraceBlocks) revert InvalidWindow();
        settlementVault.deferEmission(epoch_, noId);
    }

    function commitOperatorRoot(uint256 epoch_, uint256 noId, bytes32 payoutRoot, bytes32 artifactHash)
        external
        whenNotPaused
    {
        OperatorVersion memory op = operatorAt(noId, epoch_);
        if (!op.active) revert InactiveOperator();
        if (msg.sender != op.rootSigner) revert Unauthorized();
        RootCommitment storage existing = rootCommitments[epoch_][noId];
        if (existing.payoutRoot != bytes32(0)) revert AlreadyCommitted();
        if (payoutRoot == bytes32(0) || artifactHash == bytes32(0)) {
            revert InvalidConfiguration();
        }
        PolicySnapshot memory p = policyAt(epoch_);
        uint256 end = epochEndBlock(epoch_);
        if (block.number < end || block.number > end + p.rootCommitWindowBlocks) {
            revert InvalidWindow();
        }
        rootCommitments[epoch_][noId] = RootCommitment({
            payoutRoot: payoutRoot,
            artifactHash: artifactHash,
            committer: msg.sender,
            commitBlock: uint64(block.number)
        });
        emit OperatorRootCommitted(epoch_, noId, payoutRoot, artifactHash, msg.sender);
    }

    function finalizeOperatorEpoch(uint256 epoch_, uint256 noId) external whenNotPaused nonReentrant {
        PolicySnapshot memory p = policyAt(epoch_);
        uint256 end = epochEndBlock(epoch_);
        if (block.number < end + p.finalizeOffsetBlocks) revert InvalidWindow();
        RootCommitment storage root = rootCommitments[epoch_][noId];
        bool present = root.payoutRoot != bytes32(0);
        if (present) {
            uint256 expiryEpoch = epoch_ + p.claimTTLEpochs + p.claimGraceEpochs + 1;
            uint256 expiry = epochStartBlock(expiryEpoch) - 1;
            settlementVault.finalizeEntitlement(
                epoch_, noId, root.payoutRoot, root.artifactHash, SafeCast.toUint64(expiry)
            );
        } else {
            if (block.number <= end + p.rootCommitWindowBlocks) revert InvalidWindow();
            settlementVault.markRootMissed(epoch_, noId);
        }
        emit OperatorEpochFinalized(epoch_, noId, present);
    }

    function scheduleCommitmentOracle(address oracle, uint64 effectiveEpoch) external onlyOwner {
        if (oracle == address(0) || effectiveEpoch <= currentEpoch()) {
            revert InvalidConfiguration();
        }
        pendingCommitmentOracle = oracle;
        pendingCommitmentOracleEpoch = effectiveEpoch;
        emit CommitmentOracleScheduled(oracle, effectiveEpoch);
    }

    function activeCommitmentOracle() public view returns (address) {
        if (pendingCommitmentOracle != address(0) && currentEpoch() >= pendingCommitmentOracleEpoch) {
            return pendingCommitmentOracle;
        }
        return commitmentOracle;
    }

    /// @notice Mirrors a commitment only after the indexer has independently
    /// observed it in finalized pallet state. Runtime 447 has no EVM metadata
    /// commitment getter, so this narrowly scoped oracle is an explicit seam.
    function mirrorCommitment(
        bytes32 hotkey,
        bytes32 commitmentHash,
        uint64 finalizedBlock,
        bytes32 finalizedBlockHash
    ) external whenNotPaused {
        if (msg.sender != activeCommitmentOracle()) revert Unauthorized();
        if (
            hotkey == bytes32(0) || commitmentHash == bytes32(0) || finalizedBlockHash == bytes32(0)
                || finalizedBlock > block.number
        ) revert InvalidConfiguration();
        mirroredCommitments[hotkey] = CommitmentRecord({
            commitmentHash: commitmentHash,
            finalizedBlockHash: finalizedBlockHash,
            finalizedBlock: finalizedBlock
        });
        emit CommitmentMirrored(hotkey, commitmentHash, finalizedBlock, finalizedBlockHash);
    }

    function fleetBindingDigest(FleetBinding calldata binding) public pure returns (bytes32) {
        return keccak256(
            abi.encodePacked(
                bytes(FLEET_BINDING_DOMAIN),
                binding.chainId,
                binding.netuid,
                binding.coordinator,
                binding.fleetId,
                binding.hotkey,
                binding.clientId,
                binding.clientKey,
                binding.generation,
                binding.validFromEpoch,
                binding.validToEpoch,
                binding.commitmentHash
            )
        );
    }

    function bindFleetMember(
        FleetBinding calldata binding,
        bytes calldata clientSignature,
        bytes calldata hotkeySignature
    ) external whenNotPaused nonReentrant returns (uint16 uid) {
        uint256 epoch_ = currentEpoch();
        PolicySnapshot memory p = policyAt(epoch_);
        if (
            binding.chainId != block.chainid || binding.netuid != netuid
                || binding.coordinator != address(this) || binding.fleetId == bytes32(0)
                || binding.hotkey == bytes32(0) || binding.clientId == bytes16(0)
                || binding.clientKey == bytes32(0) || binding.commitmentHash == bytes32(0)
                || binding.generation == 0 || binding.validFromEpoch <= epoch_
                || binding.validToEpoch < binding.validFromEpoch
                || binding.validToEpoch - binding.validFromEpoch + 1 > p.maximumBindingValidityEpochs
                || clientSignature.length != 64 || hotkeySignature.length != 64
        ) revert InvalidBinding();

        CommitmentRecord memory mirrored = mirroredCommitments[binding.hotkey];
        if (mirrored.commitmentHash != binding.commitmentHash) revert StaleCommitment();
        if (block.number > uint256(mirrored.finalizedBlock) + p.commitmentMaxAgeBlocks) {
            revert StaleCommitment();
        }
        (bool exists, uint16 liveUid) = INeuron(INeuron_ADDRESS).getUid(netuid, binding.hotkey);
        if (!exists) revert RuntimeIdentityMissing();
        uid = liveUid;

        bytes32 digest = fleetBindingDigest(binding);
        bytes32 clientR = bytes32(clientSignature[0:32]);
        bytes32 clientS = bytes32(clientSignature[32:64]);
        bytes32 hotR = bytes32(hotkeySignature[0:32]);
        bytes32 hotS = bytes32(hotkeySignature[32:64]);
        if (
            !IEd25519Verify(IED25519VERIFY_ADDRESS).verify(digest, binding.clientKey, clientR, clientS)
                || !ISR25519Verify(ISR25519VERIFY_ADDRESS).verify(digest, binding.hotkey, hotR, hotS)
        ) revert InvalidSignature();

        uint256 versionCount = _bindingVersionCounts[binding.clientId];
        if (versionCount != 0) {
            BindingRecord storage prior = _bindingVersions[binding.clientId][versionCount - 1];
            if (
                binding.generation <= prior.generation
                    || (!prior.cleaned && prior.validToEpoch >= binding.validFromEpoch)
            ) revert InvalidBinding();
            if (!prior.cleaned) fleetMemberCount[prior.fleetId] -= 1;
        }
        _bindingVersions[binding.clientId][versionCount] = BindingRecord({
            fleetId: binding.fleetId,
            hotkey: binding.hotkey,
            clientKey: binding.clientKey,
            commitmentHash: binding.commitmentHash,
            generation: binding.generation,
            validFromEpoch: binding.validFromEpoch,
            validToEpoch: binding.validToEpoch,
            cleanedAtEpoch: 0,
            uid: uid,
            cleaned: false
        });
        _bindingVersionCounts[binding.clientId] = versionCount + 1;
        fleetMemberCount[binding.fleetId] += 1;
        emit FleetBound(
            binding.clientId,
            binding.fleetId,
            binding.hotkey,
            uid,
            binding.generation,
            binding.validFromEpoch,
            binding.validToEpoch
        );
    }

    function getFleetBinding(bytes16 clientId) external view returns (BindingRecord memory) {
        uint256 count = _bindingVersionCounts[clientId];
        if (count == 0) {
            return BindingRecord(bytes32(0), bytes32(0), bytes32(0), bytes32(0), 0, 0, 0, 0, 0, false);
        }
        return _bindingVersions[clientId][count - 1];
    }

    function bindingVersionCount(bytes16 clientId) external view returns (uint256) {
        return _bindingVersionCounts[clientId];
    }

    function bindingVersionAt(bytes16 clientId, uint256 index) external view returns (BindingRecord memory) {
        return _bindingVersions[clientId][index];
    }

    function bindingAt(bytes16 clientId, uint256 epoch_)
        public
        view
        returns (bool active, BindingRecord memory record)
    {
        uint256 count = _bindingVersionCounts[clientId];
        for (uint256 i = count; i != 0; i--) {
            BindingRecord storage candidate = _bindingVersions[clientId][i - 1];
            if (epoch_ < candidate.validFromEpoch) continue;
            record = candidate;
            active =
                epoch_ <= candidate.validToEpoch && (!candidate.cleaned || epoch_ < candidate.cleanedAtEpoch);
            return (active, record);
        }
    }

    function fleetRevokeDigest(bytes16 clientId, uint64 generation, uint64 effectiveEpoch)
        public
        view
        returns (bytes32)
    {
        return keccak256(
            abi.encodePacked(
                bytes(FLEET_REVOKE_DOMAIN),
                uint64(block.chainid),
                netuid,
                address(this),
                clientId,
                generation,
                effectiveEpoch
            )
        );
    }

    function revokeFleetBinding(
        bytes16 clientId,
        uint64 generation,
        uint64 effectiveEpoch,
        bytes calldata clientSignature
    ) external whenNotPaused {
        uint256 count = _bindingVersionCounts[clientId];
        uint256 recordIndex = type(uint256).max;
        for (uint256 i = count; i != 0; i--) {
            if (_bindingVersions[clientId][i - 1].generation == generation) {
                recordIndex = i - 1;
                break;
            }
        }
        if (recordIndex == type(uint256).max) revert InvalidBinding();
        BindingRecord storage record = _bindingVersions[clientId][recordIndex];
        if (
            record.cleaned || clientSignature.length != 64 || effectiveEpoch <= currentEpoch()
                || effectiveEpoch > record.validToEpoch
        ) revert InvalidBinding();
        bytes32 digest = fleetRevokeDigest(clientId, generation, effectiveEpoch);
        if (!IEd25519Verify(IED25519VERIFY_ADDRESS)
                .verify(
                    digest, record.clientKey, bytes32(clientSignature[0:32]), bytes32(clientSignature[32:64])
                )) revert InvalidSignature();
        record.validToEpoch = effectiveEpoch - 1;
        emit FleetBindingRevoked(clientId, generation, effectiveEpoch);
    }

    function cleanupFleetBinding(bytes16 clientId) external {
        uint256 count = _bindingVersionCounts[clientId];
        if (count == 0) revert InvalidBinding();
        BindingRecord storage record = _bindingVersions[clientId][count - 1];
        if (record.cleaned) revert InvalidBinding();
        uint256 epoch_ = currentEpoch();
        (bool exists, uint16 liveUid) = INeuron(INeuron_ADDRESS).getUid(netuid, record.hotkey);
        if (exists && liveUid == record.uid && epoch_ <= record.validToEpoch) revert InvalidBinding();
        uint64 cleanedAtEpoch = SafeCast.toUint64(epoch_);
        record.cleaned = true;
        record.cleanedAtEpoch = cleanedAtEpoch;
        fleetMemberCount[record.fleetId] -= 1;
        emit FleetBindingCleaned(clientId, cleanedAtEpoch);
    }

    function scheduleGuardian(address guardian_, uint64 effectiveEpoch) external onlyOwner {
        if (guardian_ == address(0) || effectiveEpoch <= currentEpoch()) {
            revert InvalidConfiguration();
        }
        pendingGuardian = guardian_;
        pendingGuardianEpoch = effectiveEpoch;
        emit GuardianScheduled(guardian_, effectiveEpoch);
    }

    function activeGuardian() public view returns (address) {
        if (pendingGuardian != address(0) && currentEpoch() >= pendingGuardianEpoch) {
            return pendingGuardian;
        }
        return guardian;
    }

    function setPaused(bool paused_) external {
        if (paused_) {
            if (msg.sender != activeGuardian() && msg.sender != owner()) revert Unauthorized();
        } else if (msg.sender != owner()) {
            revert Unauthorized();
        }
        paused = paused_;
        emit PausedSet(paused_, msg.sender);
    }

    function _authorizeUpgrade(address) internal override onlyOwner {}

    uint256[40] private __gap;
}
