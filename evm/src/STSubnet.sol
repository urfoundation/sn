// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";
import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {MerkleProof} from "@openzeppelin/contracts/utils/cryptography/MerkleProof.sol";

import {IStaking, ISTAKING_ADDRESS} from "./interfaces/stakingV2.sol";
import {INeuron, INeuron_ADDRESS} from "./interfaces/neuron.sol";
import {IMetagraph, IMetagraph_ADDRESS} from "./interfaces/metagraph.sol";
import {IEd25519Verify, IED25519VERIFY_ADDRESS} from "./interfaces/ed25519Verify.sol";
import {Blake2b} from "./lib/Blake2b.sol";

/// @title STSubnet — custody / settlement core for the UR Bittensor subnet.
///
/// @notice One UUPS-upgradeable contract that is simultaneously (WHITEPAPER §6):
///         the subnet's α custodian (a coldkey via the EVM mirror), the deposit
///         ledger, the BUYBACK RESERVE custodian (every deposit staked in full
///         onto the owner-validator `reserveHotkey`, locked — §7.4/D23), the
///         miner-pool emission custodian (it `burnedRegister`s and owns every
///         NO's pool UID), and the per-epoch settlement / pull-claims engine.
///         It is NOT the subnet validator: independent validators set weights
///         themselves and earn native dividends; this contract custodies only
///         the miner emission and the reserve. (The validator effort-bounty
///         subsystem — fee pool, vpk registry, trail claims/disputes — is
///         DEFERRED to the independent-validator phase, WHITEPAPER §9.3; the
///         hardened v0.2 implementation is parked at docs/parked/.)
///
/// @dev STRUCTURAL INVARIANTS (WHITEPAPER §6.4, D-12/D23):
///      1. Once `finalizeEpoch(e)` runs, the claims of epoch `e` are
///         sacrosanct. No owner, guardian, pause, parameter, or upgrade path
///         in this contract writes to `poolTotal[e]`, `noCommit[e]`, or the
///         claim dedup/cap state of a finalized epoch, and the claim
///         functions carry no pause gate. Admin power reaches only future
///         epochs.
///      2. The buyback reserve is ONE-WAY: no function sources a stake
///         transfer from `reserveHotkey` — deposits (and the dividends the
///         chain auto-compounds onto them) only ever ENTER it. Payouts source
///         exclusively from `treasuryHotkey` (the claims escrow).
///
///      Every subtensor precompile is reached through a small virtual accessor
///      (see "precompile accessors") because the precompile ABIs are UNVERIFIED
///      against the live runtime (PLAN.md SP-1). Tests mock them with vm.etch.
contract STSubnet is Initializable, UUPSUpgradeable, OwnableUpgradeable {
    // ---------------------------------------------------------------------
    // Types
    // ---------------------------------------------------------------------

    /// @dev WHITEPAPER §6.1. Field order is pinned by stctl/st_abi.json.
    struct Operator {
        bytes32 coldkey; // NO's substrate coldkey (informational / governance)
        uint16 minerUid; // pool UID owned by this contract (accrual slot)
        bytes32 minerHotkey; // pool hotkey emission accrues on (D-4)
        bool active;
    }

    /// @dev WHITEPAPER §6.1.
    struct NoCommit {
        bytes32 payoutRoot; // Merkle root over (provider coldkey, shareBps) leaves
        bytes off; // off-chain pointer (IPFS/HTTPS) to the full payout list
    }

    // (Validator / ServerKey / TrailSubmission / TrailLeaf: deferred with the
    //  effort bounty — WHITEPAPER §9.3/D23; parked at docs/parked/.)

    // ---------------------------------------------------------------------
    // Constants
    // ---------------------------------------------------------------------

    uint256 public constant BPS = 10_000;
    /// @dev Cap on lazy epoch rolls per call so a long-idle contract can be
    ///      caught up in chunks via rollEpochs() without exceeding block gas.
    uint256 public constant MAX_ROLLS_PER_CALL = 32;
    // Bounds the operator set so the per-roll sweep and the finalize/roll loops
    // cannot be pushed past the block gas limit (matches the max_uids cap).
    // internal: no external consumer, and keeps the ABI stable.
    uint256 internal constant MAX_OPERATORS = 256;
    /// @dev Domain separator of the head-tier client_id<->hotkey bind digest.
    bytes32 public constant HEAD_BIND_DOMAIN = keccak256("UR_ST_HEAD_BIND_V1");

    // ---------------------------------------------------------------------
    // Storage (append-only across upgrades)
    // ---------------------------------------------------------------------

    // --- core config ---
    uint16 public netuid;
    bytes32 public treasuryHotkey; // claims-ESCROW hotkey: deposit push landing pad + swept emission awaiting claims
    bytes32 public selfColdkey; // mirror(address(this)) — the contract's substrate coldkey
    address public guardian; // pause-only role (D-12)
    bool public paused;
    /// @dev Buyback reserve staking target (§7.4/D23): the owner-validator
    ///      hotkey. MUST differ from treasuryHotkey — dividends auto-compound
    ///      on this hotkey, and mixing them into the escrow would break the
    ///      exact push-then-credit check in deposit(). Set once at initialize.
    bytes32 public reserveHotkey;
    /// @dev Cumulative deposits moved to the reserve. Dividends compound on
    ///      top, so live reserve = getStake(reserveHotkey) >= buybackTotal —
    ///      the on-chain buyback audit (WHITEPAPER §12.4).
    uint256 public buybackTotal;

    // --- epoch machine (D-11: all governance-settable, in blocks) ---
    uint256 public epoch; // current OPEN epoch index
    uint64 public epochStartBlock; // start block of the current open epoch
    uint64 public tEpoch; // epoch length (50_400 mainnet default)
    uint64 public commitWindowBlocks; // +4h default (1_200 @ 12s)
    uint64 public trailsWindowBlocks; // RESERVED for the bounty phase (gates nothing in v1; kept so the epoch API is stable)
    uint64 public finalizeOffsetBlocks; // +48h default (14_400)
    mapping(uint256 => uint64) public epochCloseBlock; // e -> intended close block (set at roll)

    // F2 fix: the commit/trails/dispute/finalize window lengths are frozen
    // per-epoch AT CLOSE. Every deadline guard reads this snapshot, never the
    // live setters, so `setEpochParams` can only change epochs that close AFTER
    // the call — a pending epoch's deadlines can never be moved out from under
    // an in-flight commit, prove, or dispute (upholds D-12 "future epochs only").
    struct EpochWindowSnapshot {
        uint64 commitWindowBlocks;
        uint64 trailsWindowBlocks;
        uint64 finalizeOffsetBlocks;
    }
    mapping(uint256 => EpochWindowSnapshot) internal epochWindows; // e -> window params at close

    // --- registries ---
    mapping(uint256 => Operator) public operators; // noId -> Operator
    uint256[] public operatorIds;
    mapping(uint256 => address) public operatorAddress; // noId -> authorized deposit/commit wallet
    mapping(bytes32 => bool) public minerHotkeyUsed;
    // (validator registry / serverKeys / fee params φ,ω: deferred with the bounty, §9.3/D23)

    // --- deposits: NO on-chain weighting ledger (D25: removed) ---
    // DT[e][noId] / totalDT are GONE — the contract does no deposit weighting or
    // attribution. Per-NO deposits are published by the Deposited(e, noId, from,
    // amount) event log; validators sum them (this epoch -> demand signal;
    // all-time -> conviction/tier) and weight the pools themselves (WHITEPAPER
    // §8.1). buybackTotal (below) is the only on-chain deposit aggregate kept.
    /// @dev α on treasuryHotkey already attributed by this ledger. Deposits are
    ///      push-then-credit (see README: StakingV2 v3.2.7 has no
    ///      transferStakeFrom): getStake(treasury) - accountedStake = pushed,
    ///      not-yet-credited α available to deposit(). Credited deposits leave
    ///      the escrow immediately (moveStake -> reserveHotkey), so this ledger
    ///      keeps tracking exactly the CLAIMABLE escrow (swept emission).
    uint256 public accountedStake;

    // --- pool emission measurement (D-4: stake-delta snapshots) ---
    mapping(uint256 => uint256) public poolBaseline; // noId -> unswept stake already measured
    mapping(uint256 => uint256) public poolAccrued; // noId -> emission attributed to the open epoch
    mapping(uint256 => mapping(uint256 => uint256)) public poolEmission; // e -> noId -> measured emission
    mapping(uint256 => uint256) public carry; // noId -> pool total carried from missed commits

    // --- per-epoch operator commitment ---
    mapping(uint256 => mapping(uint256 => NoCommit)) public noCommit; // e -> noId

    // (per-epoch validator effort state: deferred with the bounty, §9.3/D23)

    // --- per-epoch settlement (append-only once finalized) ---
    mapping(uint256 => mapping(uint256 => uint256)) public poolTotal; // e -> noId -> emission-only (§8.3/D23)
    mapping(uint256 => mapping(uint256 => uint256)) public claimedMiner; // e -> noId -> α paid out
    mapping(uint256 => bool) public finalized;
    uint256 public nextFinalizeEpoch;
    mapping(uint256 => mapping(bytes32 => bool)) public minerClaimedBy; // e -> keccak(noId,coldkey)

    // ---------------------------------------------------------------------
    // Events (the off-chain sync in st_controller is built on these — SP-4)
    // ---------------------------------------------------------------------

    event OperatorRegistered(
        uint256 indexed noId, bytes32 coldkey, uint16 minerUid, bytes32 minerHotkey
    );
    event OperatorAddressSet(uint256 indexed noId, address addr);
    event OperatorActiveSet(uint256 indexed noId, bool active);

    // Head tier (WHITEPAPER §8.4/§11.4): the two lookup keys (hotkey, clientId)
    // are indexed so validators can filter the binding log cheaply.
    event HeadBound(
        bytes32 indexed hotkey, bytes32 indexed clientId, uint16 uid, address registrant
    );
    event HeadUnbound(
        bytes32 indexed hotkey, bytes32 indexed clientId, uint16 uid, address registrant
    );

    event Deposited(uint256 indexed e, uint256 indexed noId, address from, uint256 amount);
    /// @dev The buyback audit event (§7.4/§12.4): the running locked total per
    ///      credit. Dashboards read this + getStake(reserveHotkey).
    event BuybackReserved(
        uint256 indexed e, uint256 indexed noId, uint256 amount, uint256 buybackTotal
    );

    event EpochRolled(uint256 indexed closedEpoch, uint256 indexed newEpoch, uint64 closeBlock);
    event PoolSwept(uint256 indexed noId, uint256 measured, uint256 swept, bool moveOk);

    event OperatorCommitted(uint256 indexed e, uint256 indexed noId, bytes32 payoutRoot, bytes off);

    event EpochFinalized(uint256 indexed e);
    event PoolFinalized(uint256 indexed e, uint256 indexed noId, uint256 poolTotal);
    event PoolCarried(uint256 indexed e, uint256 indexed noId, uint256 carried);

    event MinerClaimed(
        uint256 indexed e,
        uint256 indexed noId,
        bytes32 indexed coldkey,
        uint256 shareBps,
        uint256 amount,
        address caller
    );

    event EpochParamsSet(
        uint64 tEpoch, uint64 commitWindowBlocks, uint64 trailsWindowBlocks, uint64 finalizeOffsetBlocks
    );
    event GuardianSet(address guardian);
    event PausedSet(bool paused, address by);
    event SelfColdkeySet(bytes32 selfColdkey);

    /// @dev Minimal storage-based reentrancy flag. Deliberately NOT OZ's
    ///      ReentrancyGuardTransient: transient storage (EIP-1153) support on
    ///      the subtensor Frontier EVM is unverified (SP-1), and the
    ///      upgradeable package no longer ships a storage-based guard.
    uint256 private _reentrancy;

    // ---------------------------------------------------------------------
    // Modifiers
    // ---------------------------------------------------------------------

    modifier nonReentrant() {
        require(_reentrancy == 0, "ST: reentrancy");
        _reentrancy = 1;
        _;
        _reentrancy = 0;
    }

    /// @dev Guardian pause surface (D-12): deposits, effort submission and
    ///      NEW claim opening (finalize). Never on claim/dispute paths.
    modifier whenNotPausedST() {
        require(!paused, "ST: paused");
        _;
    }

    modifier onlyOperatorOrOwner(uint256 noId) {
        require(
            msg.sender == owner() || msg.sender == operatorAddress[noId],
            "ST: not operator"
        );
        _;
    }

    // ---------------------------------------------------------------------
    // Initialization
    // ---------------------------------------------------------------------

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @notice Initializer (replaces the st_abi.json v1 constructor — UUPS).
    /// @param selfColdkey_ bytes32(0) => compute mirror(address(this)) on-chain
    ///        via blake2f (0x09); pass an explicit value if 0x09 is unavailable
    ///        on the target runtime (SP-1 fallback).
    function initialize(
        uint16 netuid_,
        address owner_,
        address guardian_,
        bytes32 treasuryHotkey_,
        bytes32 reserveHotkey_,
        uint64 tEpoch_,
        uint64 commitWindowBlocks_,
        uint64 trailsWindowBlocks_,
        uint64 finalizeOffsetBlocks_,
        bytes32 selfColdkey_
    ) external initializer {
        __Ownable_init(owner_);

        require(treasuryHotkey_ != bytes32(0), "ST: treasury hotkey 0");
        // §7.4/D23: the reserve hotkey is set ONCE, here — there is no setter.
        // It must differ from the escrow hotkey: dividends auto-compound on the
        // reserve, and mixing them onto the escrow would break the exact
        // push-then-credit attribution check in deposit().
        require(reserveHotkey_ != bytes32(0), "ST: reserve hotkey 0");
        require(reserveHotkey_ != treasuryHotkey_, "ST: reserve==treasury");
        _checkEpochParams(tEpoch_, commitWindowBlocks_, trailsWindowBlocks_, finalizeOffsetBlocks_);

        netuid = netuid_;
        guardian = guardian_;
        treasuryHotkey = treasuryHotkey_;
        reserveHotkey = reserveHotkey_;
        tEpoch = tEpoch_;
        commitWindowBlocks = commitWindowBlocks_;
        trailsWindowBlocks = trailsWindowBlocks_;
        finalizeOffsetBlocks = finalizeOffsetBlocks_;
        selfColdkey = selfColdkey_ == bytes32(0) ? _mirror(address(this)) : selfColdkey_;

        epoch = 0;
        epochStartBlock = uint64(block.number);

        emit SelfColdkeySet(selfColdkey);
        emit EpochParamsSet(tEpoch_, commitWindowBlocks_, trailsWindowBlocks_, finalizeOffsetBlocks_);
        emit GuardianSet(guardian_);
    }

    /// @dev The contract must be able to hold native TAO (gas / burnedRegister
    ///      burns are deducted from its mirror account).
    receive() external payable {}

    // ---------------------------------------------------------------------
    // Epoch machine
    // ---------------------------------------------------------------------

    /// @notice Lazily roll the epoch counter over every elapsed boundary
    ///         (bounded by MAX_ROLLS_PER_CALL). Permissionless. At each roll
    ///         the per-pool emission delta is measured (D-4) and swept onto
    ///         treasuryHotkey.
    function rollEpochs() external nonReentrant {
        _rollEpochs();
    }

    /// @notice Epoch index the chain is currently in (view; ignores unrolled state).
    function pendingEpoch() public view returns (uint256) {
        return epoch + (block.number - uint256(epochStartBlock)) / uint256(tEpoch);
    }

    function _rollEpochs() internal {
        uint256 rolls = 0;
        while (
            block.number >= uint256(epochStartBlock) + uint256(tEpoch) && rolls < MAX_ROLLS_PER_CALL
        ) {
            uint256 e = epoch;
            uint64 closeB = epochStartBlock + tEpoch; // intended boundary, not roll block
            epochCloseBlock[e] = closeB;
            // freeze this epoch's window lengths (F2): later setEpochParams
            // calls cannot retroactively move e's deadlines
            epochWindows[e] = EpochWindowSnapshot({
                commitWindowBlocks: commitWindowBlocks,
                trailsWindowBlocks: trailsWindowBlocks,
                finalizeOffsetBlocks: finalizeOffsetBlocks
            });

            // D-4: measure per-pool emission as the stake delta on the pool's
            // own hotkey, then sweep it to the treasury hotkey. If the roll is
            // late, everything accrued since the last roll lands in epoch e.
            uint256 n = operatorIds.length;
            for (uint256 i = 0; i < n; i++) {
                uint256 noId = operatorIds[i];
                _sweepPool(noId);
                poolEmission[e][noId] = poolAccrued[noId];
                poolAccrued[noId] = 0;
            }

            epoch = e + 1;
            epochStartBlock = closeB;
            rolls++;
            emit EpochRolled(e, e + 1, closeB);
        }
    }

    /// @dev Reverts if lazy rolling could not fully catch up (extreme backlog);
    ///      call rollEpochs() repeatedly first.
    function _requireRolled() internal view {
        require(block.number < uint256(epochStartBlock) + uint256(tEpoch), "ST: roll backlog");
    }

    /// @notice Measure + sweep one pool's emission now (also runs at each roll).
    ///         Permissionless; useful to consolidate custody mid-epoch.
    function sweepPool(uint256 noId) external nonReentrant {
        require(operators[noId].minerHotkey != bytes32(0), "ST: no operator");
        _sweepPool(noId);
    }

    function _sweepPool(uint256 noId) internal {
        Operator storage op = operators[noId];
        if (op.minerHotkey == bytes32(0)) return;

        uint256 cur = _getStake(op.minerHotkey);
        uint256 base = poolBaseline[noId];
        uint256 delta = cur > base ? cur - base : 0;
        poolAccrued[noId] += delta;

        bool moveOk = true;
        if (cur > 0) {
            moveOk = _tryMoveStake(op.minerHotkey, treasuryHotkey, cur);
            if (moveOk) {
                poolBaseline[noId] = 0;
                accountedStake += cur; // swept α is custody, not an unattributed push
            } else {
                // measured but unswept: remember the level so it is not
                // double-counted at the next measurement.
                poolBaseline[noId] = cur;
            }
        } else {
            poolBaseline[noId] = 0;
        }
        emit PoolSwept(noId, delta, moveOk ? cur : 0, moveOk);
    }

    // ---------------------------------------------------------------------
    // Registries
    // ---------------------------------------------------------------------

    /// @notice Register a network operator (owner-gated, WHITEPAPER §6.2).
    ///         The contract `burnedRegister`s `minerHotkey` on the subnet via
    ///         the Neuron precompile — the pool UID is owned by this contract
    ///         (coldkey = mirror(this)) and is a pure accrual slot.
    /// @dev The contract's account must hold enough TAO for the registration
    ///      burn (fund via plain transfer; burn denomination is SP-1-gated).
    function registerOperator(uint256 noId, bytes32 coldkey, bytes32 minerHotkey)
        external
        onlyOwner
        nonReentrant
    {
        require(operatorIds.length < MAX_OPERATORS, "ST: max operators");
        require(operators[noId].minerHotkey == bytes32(0), "ST: noId exists");
        require(minerHotkey != bytes32(0), "ST: hotkey 0");
        // a pool hotkey equal to the treasury hotkey would make the stake-delta
        // emission sweep book the whole treasury as this pool's emission and
        // double-count accountedStake — reject it (review finding C)
        require(minerHotkey != treasuryHotkey, "ST: hotkey==treasury");
        // same family: a pool hotkey equal to the reserve hotkey would book the
        // whole buyback reserve (deposits + compounded dividends) as this
        // pool's emission at the first sweep (§7.4)
        require(minerHotkey != reserveHotkey, "ST: hotkey==reserve");
        require(!minerHotkeyUsed[minerHotkey], "ST: hotkey used");
        minerHotkeyUsed[minerHotkey] = true;

        _burnedRegister(netuid, minerHotkey);
        uint16 uid = _findUid(minerHotkey);

        operators[noId] = Operator({
            coldkey: coldkey,
            minerUid: uid,
            minerHotkey: minerHotkey,
            active: true
        });
        operatorIds.push(noId);
        emit OperatorRegistered(noId, coldkey, uid, minerHotkey);
    }

    function operatorCount() external view returns (uint256) {
        return operatorIds.length;
    }

    // (setOperatorServerKey / vpkBindDigest / registerValidator[For]: the
    //  effort-bounty registry — deferred, WHITEPAPER §9.3/D23. The head-tier
    //  binding below is the only Ed25519 identity surface v1 needs.)

    // ---------------------------------------------------------------------
    // Head tier — client_id <-> hotkey binding (WHITEPAPER §8.4 / §11.4, D-18)
    //
    // A top-level miner (its own UID, steered by validators on pure measured
    // quality, paid natively) publishes a DUAL-SIGNED association between the
    // client_id its trails are measured under and its subnet hotkey. It is
    // trust-minimized: no operator/server sits in the identity path. The proof
    // shape mirrors registerValidator (mirror + 0x402, D-10). Storage-only:
    // no custody, no reentrancy surface, permissionless (proven per-call, not
    // onlyOwner); a non-live-UID hotkey fails closed (the metagraph read
    // reverts). Validators read the two public mappings via eth_call.
    // ---------------------------------------------------------------------

    /// @notice The 32-byte digest signed (Ed25519, by `clientId`'s key) for
    ///         bindHead. Domain-separated exactly like vpkBindDigest:
    ///         keccak256(abi.encodePacked(
    ///             HEAD_BIND_DOMAIN,  // 32B domain
    ///             block.chainid,     // 32B
    ///             address(this),     // 20B proxy address
    ///             registrant,        // 20B msg.sender
    ///             hotkey,            // 32B
    ///             clientId           // 32B
    ///         )) — a 168-byte preimage.
    function headBindDigest(address registrant, bytes32 hotkey, bytes32 clientId)
        public
        view
        returns (bytes32)
    {
        return keccak256(
            abi.encodePacked(
                HEAD_BIND_DOMAIN, block.chainid, address(this), registrant, hotkey, clientId
            )
        );
    }

    /// @notice Bind (or re-point) a `client_id` to a top-level miner `hotkey`.
    ///         Permissionless but DUAL-PROVED, so a miner cannot claim a
    ///         client_id it does not operate and steal another provider's
    ///         measured quality (WHITEPAPER §8.4/§11.4):
    ///         1. client_id control — `clientIdSig` (64B r‖s) is a valid Ed25519
    ///            signature by `clientId` over headBindDigest(msg.sender,
    ///            hotkey, clientId) via 0x402. `clientId` doubles as the
    ///            client's Ed25519 public key (VALIDATOR.md §2 ckey/vpk).
    ///         2. hotkey control — `hotkey` is a LIVE UID on `netuid` whose
    ///            coldkey == mirror(msg.sender), exactly like registerValidator.
    ///
    /// @dev Bijection & rotation (the safe rule): the client_id is the DURABLE
    ///      identity; the hotkey CHURNS — head UIDs are reclaimed by
    ///      lowest-emission deregistration (§8.4), so a demoted/re-promoted
    ///      provider must be able to move its measured client_id onto a fresh
    ///      hotkey WITHOUT first unbinding a hotkey it may no longer control.
    ///      Therefore a bind whose client_id or hotkey is already tied to a
    ///      DIFFERENT counterpart RE-POINTS it, clearing the stale side. This is
    ///      safe precisely because the caller has just proven control of BOTH
    ///      keys — the exact authority needed to move either side — and no
    ///      unrelated binding is ever touched (an attacker without the client_id
    ///      private key cannot pass check (1), so no quality theft). Re-binding
    ///      the identical pair is idempotent. Every write keeps both maps paired,
    ///      so the mapping stays a clean bijection.
    function bindHead(bytes32 hotkey, bytes32 clientId, bytes calldata clientIdSig)
        external
        nonReentrant
    {
        require(hotkey != bytes32(0) && clientId != bytes32(0), "ST: zero key");
        require(clientIdSig.length == 64, "ST: sig length");

        // (1) client_id control — Ed25519 over the domain-separated, replay-proof
        //     digest (binds this wallet + hotkey + clientId together).
        bytes32 r = bytes32(clientIdSig[0:32]);
        bytes32 s = bytes32(clientIdSig[32:64]);
        require(
            _ed25519Verify(headBindDigest(msg.sender, hotkey, clientId), clientId, r, s),
            "ST: bad client sig"
        );

        // (2) hotkey control — a live UID whose coldkey mirrors the caller.
        //     _findUid reverts if the hotkey is not a live UID (fail-closed).
        uint16 uid = _findUid(hotkey);
        require(_mgColdkey(uid) == _mirror(msg.sender), "ST: coldkey != mirror(sender)");

        // Bijection: clear a stale counterpart on either side (rotation), then
        // write the pair. Both clears are authorized by the dual proof above.
        bytes32 prevClient = headHotkeyToClientId[hotkey];
        if (prevClient != bytes32(0) && prevClient != clientId) {
            delete headClientIdToHotkey[prevClient];
        }
        bytes32 prevHotkey = headClientIdToHotkey[clientId];
        if (prevHotkey != bytes32(0) && prevHotkey != hotkey) {
            delete headHotkeyToClientId[prevHotkey];
        }
        headHotkeyToClientId[hotkey] = clientId;
        headClientIdToHotkey[clientId] = hotkey;
        emit HeadBound(hotkey, clientId, uid, msg.sender);
    }

    /// @notice Release a head binding (demotion / exit). Same hotkey-control
    ///         proof as bindHead; clears both directions.
    function unbindHead(bytes32 hotkey) external nonReentrant {
        bytes32 clientId = headHotkeyToClientId[hotkey];
        require(clientId != bytes32(0), "ST: not bound");
        uint16 uid = _findUid(hotkey);
        require(_mgColdkey(uid) == _mirror(msg.sender), "ST: coldkey != mirror(sender)");
        delete headHotkeyToClientId[hotkey];
        delete headClientIdToHotkey[clientId];
        emit HeadUnbound(hotkey, clientId, uid, msg.sender);
    }

    // ---------------------------------------------------------------------
    // Deposits (§6.3/§7.4): pushed onto treasuryHotkey (the escrow), credited,
    // then IMMEDIATELY staked in full into the buyback reserve.
    // ---------------------------------------------------------------------

    /// @notice Credit a NO deposit for the CURRENT epoch, then move the FULL
    ///         amount into the buyback reserve (§7.4/D23). Push-then-credit
    ///         (see README): the NO first moves α onto
    ///         (coldkey = mirror(this), hotkey = treasuryHotkey) with
    ///         StakingV2.transferStake, then calls deposit(); the contract
    ///         checks getStake(treasury) covers accountedStake + alphaAmount,
    ///         credits the steering signal `DT`, and moveStakes the α onto
    ///         reserveHotkey — slippage-free end to end (stake never leaves α).
    ///         The deposit is NEVER distributed: it earns no claim, and no
    ///         function can move it back out (the one-way reserve invariant).
    /// @dev Caller must be the NO's registered operator address (or owner) —
    ///      attribution of pushed stake must not be stealable. accountedStake
    ///      is deliberately NOT incremented: the α leaves the escrow in the
    ///      same call, so the escrow ledger keeps tracking exactly the
    ///      claimable balance. The reserve move is STRICT (reverts on
    ///      precompile failure) — a deposit either fully reserves or does not
    ///      credit at all.
    function deposit(uint256 noId, uint256 alphaAmount)
        external
        nonReentrant
        whenNotPausedST
        onlyOperatorOrOwner(noId)
    {
        _rollEpochs();
        _requireRolled();
        require(alphaAmount > 0, "ST: amount 0");
        require(operators[noId].active, "ST: operator inactive");

        uint256 treasury = _getStake(treasuryHotkey);
        require(treasury >= accountedStake + alphaAmount, "ST: stake not received");

        uint256 e = epoch;
        buybackTotal += alphaAmount; // no per-NO DT ledger (D25); the Deposited event is the record
        _moveStakeStrict(treasuryHotkey, reserveHotkey, alphaAmount);
        emit Deposited(e, noId, msg.sender, alphaAmount);
        emit BuybackReserved(e, noId, alphaAmount, buybackTotal);
    }

    // ---------------------------------------------------------------------
    // Per-epoch operator commitment (close(e) .. +commitWindow)
    // ---------------------------------------------------------------------

    /// @notice Commit the NO's payout-share Merkle root for closed epoch `e`.
    ///         Leaves: keccak256(bytes.concat(keccak256(abi.encode(
    ///         bytes32 providerColdkey, uint256 shareBps)))) — OZ double-hash,
    ///         sorted-pair tree, Σ shareBps <= 10_000 (over-committing only
    ///         drains the NO's own pool early; §8.3).
    ///         Re-commits are allowed within the window (ops fix-ups); a pool
    ///         that never commits has its total carried to the next epoch (D-11).
    function commitOperator(uint256 e, uint256 noId, bytes32 payoutRoot, bytes calldata off)
        external
        onlyOperatorOrOwner(noId)
    {
        _rollEpochs();
        require(operators[noId].active, "ST: operator inactive");
        require(payoutRoot != bytes32(0), "ST: root 0");
        uint64 closeB = epochCloseBlock[e];
        require(e < epoch && closeB != 0, "ST: epoch open");
        require(block.number <= uint256(closeB) + uint256(epochWindows[e].commitWindowBlocks), "ST: commit window");
        require(!finalized[e], "ST: finalized");

        noCommit[e][noId] = NoCommit({payoutRoot: payoutRoot, off: off});
        emit OperatorCommitted(e, noId, payoutRoot, off);
    }

    // (Per-epoch validator effort claims — submitTrails / trailSampleSeed /
    //  sampleIndices / proveTrailSamples / reseedTrailSamples / disputeTrailLeaf
    //  / disputeTrailLeafPair and their helpers: DEFERRED with the effort
    //  bounty, WHITEPAPER §9.3/D23. The hardened v0.2 implementation — sampled
    //  estimator credit, coverage-bound A2 signatures, HF-2 reseed caps — is
    //  parked at docs/parked/STSubnet-v0.2-effort.sol.ref for that phase.)

    // ---------------------------------------------------------------------
    // Settlement (close(e) + finalizeOffset; permissionless; append-only)
    // ---------------------------------------------------------------------

    /// @notice Finalize closed epoch `e` (in order). Snapshots, per NO:
    ///         poolTotal = measured pool-hotkey stake delta (D-4)
    ///                   + carried balance — EMISSION ONLY (§8.3/D23: deposits
    ///         are reserved, never distributed). A pool with NO commit for `e`
    ///         has that total carried to the next epoch instead (D-11).
    ///         After this, epoch e's claims are open and IMMUTABLE.
    function finalizeEpoch(uint256 e) external nonReentrant whenNotPausedST {
        _rollEpochs();
        require(e == nextFinalizeEpoch, "ST: finalize in order");
        uint64 closeB = epochCloseBlock[e];
        require(e < epoch && closeB != 0, "ST: epoch open");
        require(
            block.number >= uint256(closeB) + uint256(epochWindows[e].finalizeOffsetBlocks), "ST: finalize window"
        );
        require(!finalized[e], "ST: finalized");

        uint256 n = operatorIds.length;
        for (uint256 i = 0; i < n; i++) {
            uint256 noId = operatorIds[i];
            uint256 pt = poolEmission[e][noId] + carry[noId];
            if (pt == 0) continue;
            if (noCommit[e][noId].payoutRoot == bytes32(0)) {
                carry[noId] = pt; // missed commit: roll the pool total forward
                emit PoolCarried(e, noId, pt);
            } else {
                poolTotal[e][noId] = pt;
                carry[noId] = 0;
                emit PoolFinalized(e, noId, pt);
            }
        }

        finalized[e] = true;
        nextFinalizeEpoch = e + 1;
        emit EpochFinalized(e);
    }

    // ---------------------------------------------------------------------
    // Claims (WHITEPAPER §8.3 / §11.2) — NO pause gate, NO admin path.
    // ---------------------------------------------------------------------

    /// @notice Miner-leaf hash: OZ double-hash of abi.encode(coldkey, shareBps).
    ///         NOTE: PLAN.md §2 double-hash OVERRIDES the single-hash snippet in
    ///         WHITEPAPER §11.2. sn/merkle (Go) must match this exactly.
    function minerLeafHash(bytes32 coldkey, uint256 shareBps) public pure returns (bytes32) {
        return keccak256(bytes.concat(keccak256(abi.encode(coldkey, shareBps))));
    }

    // (trailLeafHash / effortDigest: deferred with the bounty, §9.3/D23. The
    //  wire-level coverage attestation — the server signing
    //  sha256(finalDigest ‖ coverage) — REMAINS live in /verify
    //  (connect.VerifyEffortDigest): proofs minted today stay consumable by
    //  the bounty phase.)

    /// @notice Claim a provider's share of pool `noId` for finalized epoch `e`.
    ///         Permissionless (relayer-compatible, D-2): the α always goes to
    ///         `coldkey` as stake on treasuryHotkey via transferStake.
    ///         amount = shareBps · poolTotal / 10_000, cumulative-capped at
    ///         poolTotal (a NO whose shares exceed 1 only drains its own pool).
    function claimMiner(
        uint256 e,
        uint256 noId,
        bytes32 coldkey,
        uint256 shareBps,
        bytes32[] calldata proof
    ) external nonReentrant {
        require(finalized[e], "ST: not finalized");
        require(coldkey != bytes32(0), "ST: coldkey 0");
        require(shareBps > 0 && shareBps <= BPS, "ST: shareBps");
        bytes32 root = noCommit[e][noId].payoutRoot;
        require(root != bytes32(0), "ST: no commit");
        require(MerkleProof.verify(proof, root, minerLeafHash(coldkey, shareBps)), "ST: bad proof");

        bytes32 key = keccak256(abi.encode(noId, coldkey));
        require(!minerClaimedBy[e][key], "ST: claimed");
        minerClaimedBy[e][key] = true;

        uint256 amount = (shareBps * poolTotal[e][noId]) / BPS;
        claimedMiner[e][noId] += amount;
        require(claimedMiner[e][noId] <= poolTotal[e][noId], "ST: pool over-drained");

        _payout(coldkey, amount);
        emit MinerClaimed(e, noId, coldkey, shareBps, amount, msg.sender);
    }

    // (claimValidator: deferred with the bounty, §9.3/D23.)

    /// @dev Every payout sources from the treasury ESCROW — never the reserve
    ///      (the one-way invariant, §7.4).
    function _payout(bytes32 coldkey, uint256 amount) internal {
        if (amount == 0) return;
        accountedStake -= amount;
        _transferStake(coldkey, treasuryHotkey, amount);
    }

    // ---------------------------------------------------------------------
    // Governance / policy surface (grouped for a later STSubnetPolicy
    // extraction per WHITEPAPER §6.4.3 — kept in-contract for v1).
    // Every setter here affects FUTURE epochs / future state only.
    // ---------------------------------------------------------------------

    // (setFeeParams(φ, ω): deferred with the bounty, §9.3/D23. There is
    //  deliberately NO setter for reserveHotkey — the reserve target is fixed
    //  at initialize; re-pointing it is an upgrade-grade decision.)

    function setEpochParams(
        uint64 tEpoch_,
        uint64 commitWindowBlocks_,
        uint64 trailsWindowBlocks_,
        uint64 finalizeOffsetBlocks_
    ) external onlyOwner {
        _checkEpochParams(tEpoch_, commitWindowBlocks_, trailsWindowBlocks_, finalizeOffsetBlocks_);
        tEpoch = tEpoch_;
        commitWindowBlocks = commitWindowBlocks_;
        trailsWindowBlocks = trailsWindowBlocks_;
        finalizeOffsetBlocks = finalizeOffsetBlocks_;
        emit EpochParamsSet(tEpoch_, commitWindowBlocks_, trailsWindowBlocks_, finalizeOffsetBlocks_);
    }

    function _checkEpochParams(uint64 tEpoch_, uint64 commitW, uint64 trailsW, uint64 finalizeOff)
        internal
        pure
    {
        require(tEpoch_ >= 1, "ST: tEpoch 0");
        // ordering only (equality allowed for operator flexibility). NOTE: for
        // a non-empty post-prove dispute buffer, operators should configure
        // trailsW < finalizeOff — at trailsW == finalizeOff a leaf proved at the
        // exact boundary block is undisputable (prove uses <=, dispute uses <).
        // Left as guidance, not enforced, so commit/trails windows may overlap
        // the finalize offset (review finding D, accepted LOW).
        require(commitW <= trailsW && trailsW <= finalizeOff, "ST: window order");
    }

    function setGuardian(address guardian_) external onlyOwner {
        guardian = guardian_;
        emit GuardianSet(guardian_);
    }

    /// @notice Guardian/owner pause (D-12): halts deposit and finalizeEpoch
    ///         (the opening of NEW claims). It can NEVER touch finalized-epoch
    ///         claims — those functions carry no pause gate.
    function setPaused(bool paused_) external {
        require(msg.sender == guardian || msg.sender == owner(), "ST: not guardian");
        paused = paused_;
        emit PausedSet(paused_, msg.sender);
    }

    function setOperatorAddress(uint256 noId, address addr) external onlyOwner {
        require(operators[noId].minerHotkey != bytes32(0), "ST: no operator");
        operatorAddress[noId] = addr;
        emit OperatorAddressSet(noId, addr);
    }

    function setOperatorActive(uint256 noId, bool active) external onlyOwner {
        require(operators[noId].minerHotkey != bytes32(0), "ST: no operator");
        operators[noId].active = active;
        emit OperatorActiveSet(noId, active);
    }

    /// @notice SP-1 escape hatch: correct the contract's substrate coldkey if
    ///         the on-chain blake2f mirror computation mismatches the runtime.
    ///         Affects future stake measurements only (never finalized claims).
    function setSelfColdkey(bytes32 selfColdkey_) external onlyOwner {
        require(selfColdkey_ != bytes32(0), "ST: coldkey 0");
        selfColdkey = selfColdkey_;
        emit SelfColdkeySet(selfColdkey_);
    }

    // ---------------------------------------------------------------------
    // Precompile accessors — the ONLY places precompiles are touched.
    // Virtual + address-constant so SP-1 findings / tests can swap them
    // (tests vm.etch mock code at the canonical addresses).
    // ABIs UNVERIFIED against the live runtime: subtensor v3.2.7 (SP-1).
    // ---------------------------------------------------------------------

    function _staking() internal view virtual returns (IStaking) {
        return IStaking(ISTAKING_ADDRESS); // 0x805
    }

    function _neuron() internal view virtual returns (INeuron) {
        return INeuron(INeuron_ADDRESS); // 0x804
    }

    function _metagraph() internal view virtual returns (IMetagraph) {
        return IMetagraph(IMetagraph_ADDRESS); // 0x802
    }

    function _ed25519() internal view virtual returns (IEd25519Verify) {
        return IEd25519Verify(IED25519VERIFY_ADDRESS); // 0x402
    }

    /// @dev Contract-held α on `hotkey` (coldkey = selfColdkey), in rao.
    function _getStake(bytes32 hotkey) internal view virtual returns (uint256) {
        return _staking().getStake(hotkey, selfColdkey, uint256(netuid));
    }

    /// @dev Move contract-held stake between hotkeys; non-reverting (used in
    ///      the epoch roll so a precompile failure cannot brick the epoch
    ///      machine — the measurement baseline handles retry).
    function _tryMoveStake(bytes32 fromHotkey, bytes32 toHotkey, uint256 amount)
        internal
        virtual
        returns (bool ok)
    {
        (ok,) = address(_staking()).call(
            abi.encodeCall(
                IStaking.moveStake, (fromHotkey, toHotkey, uint256(netuid), uint256(netuid), amount)
            )
        );
    }

    /// @dev Strict move for the deposit -> reserve leg (§7.4): unlike the sweep
    ///      (where a precompile hiccup must not brick the epoch machine), a
    ///      deposit must either fully reserve or revert the whole credit.
    function _moveStakeStrict(bytes32 fromHotkey, bytes32 toHotkey, uint256 amount)
        internal
        virtual
    {
        _staking().moveStake(fromHotkey, toHotkey, uint256(netuid), uint256(netuid), amount);
    }

    /// @dev Pay out contract-held stake on `hotkey` to `destColdkey` (stays α
    ///      stake — slippage-free; recipients removeStake at their discretion).
    function _transferStake(bytes32 destColdkey, bytes32 hotkey, uint256 amount) internal virtual {
        _staking().transferStake(destColdkey, hotkey, uint256(netuid), uint256(netuid), amount);
    }

    function _burnedRegister(uint16 netuid_, bytes32 hotkey) internal virtual {
        _neuron().burnedRegister(netuid_, hotkey);
    }

    function _ed25519Verify(bytes32 message, bytes32 pubkey, bytes32 r, bytes32 s)
        internal
        view
        virtual
        returns (bool)
    {
        return _ed25519().verify(message, pubkey, r, s);
    }

    function _mgColdkey(uint16 uid) internal view virtual returns (bytes32) {
        return _metagraph().getColdkey(netuid, uid);
    }

    /// @dev Linear metagraph scan (no reverse lookup precompile for hotkeys at
    ///      v3.2.7). Bounded by the subnet UID cap (max_uids <= 256, PLAN §3.8).
    function _findUid(bytes32 hotkey) internal view virtual returns (uint16) {
        IMetagraph mg = _metagraph();
        uint16 n = mg.getUidCount(netuid);
        for (uint16 i = 0; i < n; i++) {
            if (mg.getHotkey(netuid, i) == hotkey) {
                return i;
            }
        }
        revert("ST: uid not found");
    }

    /// @dev H160 -> AccountId32 mirror via blake2f (0x09). Virtual so tests /
    ///      an SP-1 fallback build can swap it if blake2f is unavailable on
    ///      the live runtime.
    function _mirror(address account) internal view virtual returns (bytes32) {
        return Blake2b.mirror(account);
    }

    // ---------------------------------------------------------------------
    // UUPS
    // ---------------------------------------------------------------------

    function _authorizeUpgrade(address) internal override onlyOwner {}

    // --- head tier: client_id <-> hotkey binding (WHITEPAPER §8.4/§11.4, D-18) ---
    // Appended at the END of storage (not in the registries block above) so this
    // upgrade preserves every pre-existing slot: the two new mappings consume 2
    // of the reserved __gap slots (50 -> 48), the textbook UUPS append.
    // Bijective — one hotkey <-> one client_id; both public for validator
    // eth_call reads. See bindHead / unbindHead / headBindDigest below.
    mapping(bytes32 => bytes32) public headHotkeyToClientId; // hotkey  -> client_id
    mapping(bytes32 => bytes32) public headClientIdToHotkey; // client_id -> hotkey

    /// @dev Reserved storage for future upgrades / policy-module extraction.
    uint256[48] private __gap;
}
