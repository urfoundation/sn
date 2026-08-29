// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {MerkleProof} from "@openzeppelin/contracts/utils/cryptography/MerkleProof.sol";

import {IStaking, ISTAKING_ADDRESS} from "./interfaces/stakingV2.sol";
import {INeuron, INeuron_ADDRESS} from "./interfaces/neuron.sol";

/// @title STSettlementVault
/// @notice Non-upgradeable custody and claims state machine. The coordinator
/// may create an epoch entitlement once, but neither it nor any administrator
/// can rewrite a finalized root/amount/expiry or pause a valid claim.
contract STSettlementVault {
    uint256 public constant BPS = 10_000;

    enum EpochStatus {
        Unset,
        Funded,
        Finalized,
        RootMissed,
        Carried
    }

    struct Pool {
        bytes32 hotkey;
        uint16 uid;
        bool active;
    }

    struct Entitlement {
        bytes32 payoutRoot;
        bytes32 artifactHash;
        uint256 funded;
        uint256 total;
        uint256 claimed;
        uint64 expiryBlock;
        EpochStatus status;
    }

    uint16 public immutable netuid;
    bytes32 public immutable escrowHotkey;
    bytes32 public immutable selfColdkey;
    uint64 public immutable minimumClaimTTLBlocks;
    address public immutable bootstrap;

    address public coordinator;
    bool public escrowRegistered;
    uint256 private _entered;

    mapping(uint256 noId => Pool pool) public pools;
    mapping(bytes32 hotkey => bool used) public poolHotkeyUsed;
    mapping(uint256 epoch => mapping(uint256 noId => Entitlement record)) private _entitlements;
    mapping(uint256 noId => uint256 amount) public carry;
    mapping(uint256 epoch => mapping(bytes32 claimKey => bool claimed)) public leafClaimed;

    uint256 public totalCaptured;
    uint256 public totalPaid;
    uint256 public pendingFunding;
    uint256 public outstandingLiability;
    uint256 public escrowAccounted;

    event CoordinatorFixed(address indexed coordinator);
    event EscrowRegistered(bytes32 indexed hotkey, uint16 uid);
    event PoolRegistered(uint256 indexed noId, bytes32 indexed hotkey, uint16 uid);
    event PoolActiveSet(uint256 indexed noId, bool active);
    event EmissionCaptured(
        uint256 indexed epoch, uint256 indexed noId, bytes32 indexed poolHotkey, uint256 amount
    );
    event EmissionDeferred(uint256 indexed epoch, uint256 indexed noId);
    event EntitlementFinalized(
        uint256 indexed epoch,
        uint256 indexed noId,
        bytes32 payoutRoot,
        bytes32 artifactHash,
        uint256 total,
        uint64 expiryBlock
    );
    event RootMissed(uint256 indexed epoch, uint256 indexed noId, uint256 carried);
    event Claimed(
        uint256 indexed epoch,
        uint256 indexed noId,
        bytes32 indexed coldkey,
        uint256 shareBps,
        uint256 amount,
        address relayer
    );
    event EntitlementExpired(
        uint256 indexed epoch, uint256 indexed noId, uint256 unclaimed, uint256 operatorCarry
    );

    error Unauthorized();
    error AlreadyInitialized();
    error InvalidConfiguration();
    error InvalidTransition();
    error NativeRefundFailed();
    error UnknownPool();
    error ClaimExpired();
    error InvalidProof();
    error AlreadyClaimed();
    error Underfunded();
    error Reentrancy();

    modifier onlyCoordinator() {
        if (msg.sender != coordinator) revert Unauthorized();
        _;
    }

    modifier nonReentrant() {
        if (_entered != 0) revert Reentrancy();
        _entered = 1;
        _;
        _entered = 0;
    }

    constructor(
        uint16 netuid_,
        bytes32 escrowHotkey_,
        bytes32 selfColdkey_,
        uint64 minimumClaimTTLBlocks_,
        address bootstrap_
    ) {
        if (
            netuid_ == 0 || escrowHotkey_ == bytes32(0) || selfColdkey_ == bytes32(0)
                || minimumClaimTTLBlocks_ == 0 || bootstrap_ == address(0)
        ) revert InvalidConfiguration();
        netuid = netuid_;
        escrowHotkey = escrowHotkey_;
        selfColdkey = selfColdkey_;
        minimumClaimTTLBlocks = minimumClaimTTLBlocks_;
        bootstrap = bootstrap_;
    }

    receive() external payable {}

    // Preserve any pre-existing vault balance and return only registration
    // funding left after the runtime charges this contract's SS58 mirror.
    function _refundRegistrationSurplus(address recipient, uint256 balanceBefore) private {
        uint256 current = address(this).balance;
        if (current <= balanceBefore) return;
        (bool refunded,) = payable(recipient).call{value: current - balanceBefore}("");
        if (!refunded) revert NativeRefundFailed();
    }

    function setCoordinatorOnce(address coordinator_) external {
        if (msg.sender != bootstrap) revert Unauthorized();
        if (coordinator != address(0)) revert AlreadyInitialized();
        if (coordinator_ == address(0) || coordinator_.code.length == 0) {
            revert InvalidConfiguration();
        }
        coordinator = coordinator_;
        emit CoordinatorFixed(coordinator_);
    }

    /// @notice Limit-registers the immutable claims escrow under this vault's
    /// mapped Substrate coldkey. The bootstrap may execute this exactly once
    /// during installation; no external key retains control of the hotkey.
    function registerEscrow(uint64 maximumBurnRao) external payable nonReentrant returns (uint16 uid) {
        if (msg.sender != bootstrap) revert Unauthorized();
        if (escrowRegistered) revert AlreadyInitialized();
        if (maximumBurnRao == 0) revert InvalidConfiguration();
        uint256 balanceBefore = address(this).balance - msg.value;
        // Checks-effects-interactions: a failed registration/read rolls this
        // flag back with the transaction, while a hostile callback can never
        // observe an unclaimed one-shot registration capability.
        escrowRegistered = true;
        // Runtime 451 charges this contract's SS58-mirror balance. msg.value
        // funds that balance before execution; forwarding it to the precompile
        // would move the funds away before the runtime dispatch can burn them.
        INeuron(INeuron_ADDRESS).registerLimit(netuid, escrowHotkey, maximumBurnRao);
        (bool exists, uint16 liveUid) = INeuron(INeuron_ADDRESS).getUid(netuid, escrowHotkey);
        if (!exists) revert UnknownPool();
        emit EscrowRegistered(escrowHotkey, liveUid);
        _refundRegistrationSurplus(msg.sender, balanceBefore);
        return liveUid;
    }

    function entitlement(uint256 epoch, uint256 noId) external view returns (Entitlement memory) {
        return _entitlements[epoch][noId];
    }

    // The runtime registration call can expose read-only public getters while
    // it executes, but every state-changing vault entry point shares the
    // nonReentrant guard and only the fixed coordinator can mutate a pool.
    // slither-disable-next-line reentrancy-no-eth
    function registerPool(uint256 noId, bytes32 poolHotkey, uint64 maximumBurnRao)
        external
        payable
        onlyCoordinator
        nonReentrant
        returns (uint16 uid)
    {
        if (
            noId == 0 || poolHotkey == bytes32(0) || poolHotkey == escrowHotkey
                || pools[noId].hotkey != bytes32(0) || poolHotkeyUsed[poolHotkey] || maximumBurnRao == 0
        ) revert InvalidConfiguration();
        uint256 balanceBefore = address(this).balance - msg.value;
        INeuron(INeuron_ADDRESS).registerLimit(netuid, poolHotkey, maximumBurnRao);
        (bool exists, uint16 liveUid) = INeuron(INeuron_ADDRESS).getUid(netuid, poolHotkey);
        if (!exists) revert UnknownPool();
        pools[noId] = Pool({hotkey: poolHotkey, uid: liveUid, active: true});
        poolHotkeyUsed[poolHotkey] = true;
        emit PoolRegistered(noId, poolHotkey, liveUid);
        _refundRegistrationSurplus(msg.sender, balanceBefore);
        return liveUid;
    }

    function setPoolActive(uint256 noId, bool active) external onlyCoordinator nonReentrant {
        Pool storage pool = pools[noId];
        if (pool.hotkey == bytes32(0)) revert UnknownPool();
        pool.active = active;
        emit PoolActiveSet(noId, active);
    }

    /// @notice Captures the complete stake on a pool hotkey into the immutable
    /// escrow hotkey. Coordinator timing guards decide which boundary this
    /// observation belongs to; this function prevents duplicate observation.
    function captureEmission(uint256 epoch, uint256 noId)
        external
        onlyCoordinator
        nonReentrant
        returns (uint256 amount)
    {
        Entitlement storage record = _entitlements[epoch][noId];
        Pool storage pool = pools[noId];
        if (record.status != EpochStatus.Unset) revert InvalidTransition();
        if (pool.hotkey == bytes32(0)) revert UnknownPool();

        amount = IStaking(ISTAKING_ADDRESS).getStake(pool.hotkey, selfColdkey, uint256(netuid));

        // Effects precede the stake move. A failed or partial runtime call
        // reverts the entire EVM transaction, while the transition marker
        // prevents a runtime callback from observing an unclaimed epoch.
        record.funded = amount;
        record.status = EpochStatus.Funded;
        totalCaptured += amount;
        pendingFunding += amount;
        escrowAccounted += amount;
        if (amount != 0) {
            IStaking(ISTAKING_ADDRESS)
                .moveStake(pool.hotkey, escrowHotkey, uint256(netuid), uint256(netuid), amount);
        }
        _requireBacking();
        emit EmissionCaptured(epoch, noId, pool.hotkey, amount);
    }

    /// @notice Records zero funding for a missed boundary without sweeping a
    /// multi-epoch delta into the first missed epoch. Stake remains on the pool
    /// hotkey and is captured at the next timely boundary for the same NO.
    function deferEmission(uint256 epoch, uint256 noId) external onlyCoordinator nonReentrant {
        Entitlement storage record = _entitlements[epoch][noId];
        if (record.status != EpochStatus.Unset) revert InvalidTransition();
        if (pools[noId].hotkey == bytes32(0)) revert UnknownPool();
        record.status = EpochStatus.Funded;
        emit EmissionDeferred(epoch, noId);
    }

    function finalizeEntitlement(
        uint256 epoch,
        uint256 noId,
        bytes32 payoutRoot,
        bytes32 artifactHash,
        uint64 expiryBlock
    ) external onlyCoordinator nonReentrant {
        Entitlement storage record = _entitlements[epoch][noId];
        if (record.status != EpochStatus.Funded) revert InvalidTransition();
        if (payoutRoot == bytes32(0) || artifactHash == bytes32(0)) {
            revert InvalidConfiguration();
        }
        if (expiryBlock < block.number + minimumClaimTTLBlocks) revert InvalidConfiguration();

        uint256 priorCarry = carry[noId];
        uint256 total = record.funded + priorCarry;
        carry[noId] = 0;
        pendingFunding -= record.funded;
        outstandingLiability += record.funded;

        record.payoutRoot = payoutRoot;
        record.artifactHash = artifactHash;
        record.total = total;
        record.expiryBlock = expiryBlock;
        record.status = EpochStatus.Finalized;
        _requireBacking();
        emit EntitlementFinalized(epoch, noId, payoutRoot, artifactHash, total, expiryBlock);
    }

    function markRootMissed(uint256 epoch, uint256 noId) external onlyCoordinator nonReentrant {
        Entitlement storage record = _entitlements[epoch][noId];
        if (record.status != EpochStatus.Funded) revert InvalidTransition();
        pendingFunding -= record.funded;
        outstandingLiability += record.funded;
        carry[noId] += record.funded;
        record.total = record.funded;
        record.status = EpochStatus.RootMissed;
        _requireBacking();
        emit RootMissed(epoch, noId, record.funded);
    }

    function payoutLeaf(bytes32 coldkey, uint256 shareBps) public pure returns (bytes32) {
        return keccak256(bytes.concat(keccak256(abi.encode(coldkey, shareBps))));
    }

    function claim(uint256 epoch, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] calldata proof)
        external
        nonReentrant
        returns (uint256 amount)
    {
        Entitlement storage record = _entitlements[epoch][noId];
        if (record.status != EpochStatus.Finalized) revert InvalidTransition();
        if (block.number > record.expiryBlock) revert ClaimExpired();
        if (coldkey == bytes32(0) || shareBps == 0 || shareBps > BPS) {
            revert InvalidConfiguration();
        }
        if (!MerkleProof.verify(proof, record.payoutRoot, payoutLeaf(coldkey, shareBps))) {
            revert InvalidProof();
        }
        bytes32 key = keccak256(abi.encode(noId, coldkey));
        if (leafClaimed[epoch][key]) revert AlreadyClaimed();
        leafClaimed[epoch][key] = true;

        amount = (record.total * shareBps) / BPS;
        record.claimed += amount;
        if (record.claimed > record.total) revert Underfunded();
        outstandingLiability -= amount;
        escrowAccounted -= amount;
        totalPaid += amount;
        if (amount != 0) {
            IStaking(ISTAKING_ADDRESS)
                .transferStake(coldkey, escrowHotkey, uint256(netuid), uint256(netuid), amount);
        }
        emit Claimed(epoch, noId, coldkey, shareBps, amount, msg.sender);
    }

    function expireEntitlement(uint256 epoch, uint256 noId) external nonReentrant {
        Entitlement storage record = _entitlements[epoch][noId];
        if (record.status != EpochStatus.Finalized) revert InvalidTransition();
        if (block.number <= record.expiryBlock) revert ClaimExpired();
        uint256 remainder = record.total - record.claimed;
        carry[noId] += remainder;
        record.status = EpochStatus.Carried;
        emit EntitlementExpired(epoch, noId, remainder, carry[noId]);
    }

    /// @notice Executable conservation identity. Unfinalized captured funds
    /// are pending; finalized and carried funds are outstanding liabilities.
    function conservationHolds() external view returns (bool) {
        return totalCaptured == totalPaid + escrowAccounted
            && escrowAccounted == pendingFunding + outstandingLiability
            && _liveEscrowStake() >= escrowAccounted;
    }

    function liveEscrowStake() external view returns (uint256) {
        return _liveEscrowStake();
    }

    function _liveEscrowStake() internal view returns (uint256) {
        return IStaking(ISTAKING_ADDRESS).getStake(escrowHotkey, selfColdkey, uint256(netuid));
    }

    function _requireBacking() internal view {
        if (_liveEscrowStake() < escrowAccounted) revert Underfunded();
    }
}
