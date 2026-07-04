// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {IStaking, ISTAKING_ADDRESS} from "../interfaces/stakingV2.sol";
import {INeuron, INeuron_ADDRESS} from "../interfaces/neuron.sol";
import {IMetagraph, IMetagraph_ADDRESS} from "../interfaces/metagraph.sol";
import {IEd25519Verify, IED25519VERIFY_ADDRESS} from "../interfaces/ed25519Verify.sol";
import {Blake2b} from "../lib/Blake2b.sol";

/// @title STSubnetProbe — the SP-1 precompile-conformance probe (throwaway).
///
/// @notice A deploy-and-discard contract for verifying, ON THE LIVE MAINNET
///         RUNTIME, every subtensor-precompile assumption STSubnet depends on
///         (PLAN.md SP-1, `docs/LAUNCH.md` Phase B). It reproduces STSubnet's
///         exact precompile-access shapes (same vendored interfaces, same
///         `mirror(this)` coldkey custody), so what it observes is what the
///         real contract will observe.
///
/// @dev WHY A CONTRACT, AND WHY cast (not forge simulate): the subtensor
///      precompiles (0x802/0x804/0x805) live in the NODE RUNTIME, not as EVM
///      bytecode. Forge's local simulation has no implementation for them, so
///      a `forge script` that calls them reverts in simulation and never
///      reaches the node. The faithful path is to DEPLOY this probe once, then
///      call its views with `cast call <probe> ... --rpc-url mainnet` (a raw
///      eth_call executed ON the node, hitting the real precompiles) and its
///      state fns with `cast send`. The custody assumption — "a contract's
///      coldkey is mirror(contract)" — can only be tested from a contract, so
///      every stake op below runs in THIS contract's context.
///
///      Read battery (`readBattery`) is free (gas only, no funds, no state
///      moved). The value-bearing checks (`seedFromTao`, `moveRoundTrip`,
///      `transferOut`, the dividend two-step) move DUST real α and are
///      owner-gated and opt-in. `burnedRegister` conformance is deliberately
///      NOT exercised here (it burns TAO); its denomination is pinned by the
///      first real `registerOperator` at genesis (`docs/LAUNCH.md` C6).
///
///      This contract is NOT part of the production system — do not import it
///      into STSubnet or its tests' production paths.
contract STSubnetProbe {
    // ------------------------------------------------------------------
    // Known-answer vectors (public, auditable — see docs/LAUNCH.md B1)
    // ------------------------------------------------------------------

    /// @dev blake2f (0x09) KAT: mirror(0x1111...1111). Matches the value
    ///      pinned in evm/test/STSubnet.t.sol (Python hashlib.blake2b).
    address internal constant MIRROR_KAT_ADDR = 0x1111111111111111111111111111111111111111;
    bytes32 internal constant MIRROR_KAT = 0x32f955c958e51189a4921aed41ef00818f7368dfaec8d9969f091006f8066228;

    /// @dev Ed25519 (0x402) KAT: a real signature over a 32-byte message
    ///      (deterministic keypair, seed sha256("urnetwork/sp1/ed25519-kat/v1");
    ///      generation recorded in docs/LAUNCH.md B1). verify(MSG,PK,R,S)==true;
    ///      the same with a flipped S bit must be false.
    bytes32 internal constant ED_MSG = 0xca6dd518081710a6081369b7d2eb0cf32396bf77c9f091be21e6d4c8ed37a6cb;
    bytes32 internal constant ED_PK = 0x3f0d9ad990f7706d891de2dd0a52cc68a6cc631683a31977bb38b9f189d26de1;
    bytes32 internal constant ED_R = 0x2e530da93345ff099a7c46cb9aab8d964a7a016852b567e074f64f9cf1d5cf30;
    bytes32 internal constant ED_S = 0x35a13c64140c12e523a8e5fec6541fa846be95974aa399f81fc907d020955f0e;

    // ------------------------------------------------------------------
    // Config / state
    // ------------------------------------------------------------------

    address public immutable owner;
    uint16 public immutable netuid; // an EXISTING netuid to probe against

    // dividend two-step baselines (§7.4 reserve-leg conformance)
    mapping(bytes32 => uint256) public divBaseline; // hotkey -> getStake(hotkey, self) at snapshot
    mapping(bytes32 => uint64) public divBaselineBlock;

    event MoveRoundTrip(
        bytes32 indexed fromHotkey,
        bytes32 indexed toHotkey,
        uint256 amount,
        uint256 fromBefore,
        uint256 fromAfter,
        uint256 toBefore,
        uint256 toAfter
    );
    event Seeded(bytes32 indexed hotkey, uint256 amountArg, uint256 valueSent, uint256 stakeAfter);
    event DividendSnapshot(bytes32 indexed hotkey, uint256 baseline, uint64 blockNumber);

    modifier onlyOwner() {
        require(msg.sender == owner, "probe: not owner");
        _;
    }

    constructor(uint16 netuid_) {
        owner = msg.sender;
        netuid = netuid_;
    }

    receive() external payable {}

    // ------------------------------------------------------------------
    // Read battery — free (gas only). Call ON THE NODE:
    //   cast call <probe> "readBattery(bytes32)((...))" <sampleHotkey> --rpc-url mainnet
    // Every precompile touch is individually try/caught, so one missing
    // precompile does not mask the others — the struct shows exactly which
    // assumptions hold on the live runtime.
    // ------------------------------------------------------------------

    struct Battery {
        // 0x09 blake2f (the H160 -> ss58 mirror; the whole custody model)
        bool blakeOk;
        bytes32 mirrorKat; // observed mirror(MIRROR_KAT_ADDR)
        bool blakeKatMatch; // == MIRROR_KAT
        bytes32 selfColdkey; // mirror(this): the probe's coldkey the pallet sees
        // 0x402 ed25519 (head-binding + parked bounty)
        bool edOk;
        bool edVerifyGood; // KAT verifies true
        bool edVerifyBad; // tampered sig verifies false
        // 0x802 metagraph reads (uid resolution, coldkey binding)
        bool mgOk;
        uint16 uidCount;
        bytes32 uid0Hotkey;
        bytes32 uid0Coldkey;
        // 0x805 staking view (custody + the reserve/escrow audit)
        bool stakeViewOk;
        uint256 sampleSelfStake; // getStake(sampleHotkey, mirror(this), netuid)
    }

    function readBattery(bytes32 sampleHotkey) external view returns (Battery memory b) {
        // --- 0x09 blake2f, via the exact library STSubnet uses ---
        try this.mirrorExt(MIRROR_KAT_ADDR) returns (bytes32 m) {
            b.blakeOk = true;
            b.mirrorKat = m;
            b.blakeKatMatch = (m == MIRROR_KAT);
        } catch {}
        try this.mirrorExt(address(this)) returns (bytes32 self) {
            b.selfColdkey = self;
        } catch {}

        // --- 0x402 ed25519 verify ---
        try IEd25519Verify(IED25519VERIFY_ADDRESS).verify(ED_MSG, ED_PK, ED_R, ED_S) returns (bool good) {
            b.edOk = true;
            b.edVerifyGood = good;
        } catch {}
        try IEd25519Verify(IED25519VERIFY_ADDRESS).verify(ED_MSG, ED_PK, ED_R, ED_S ^ bytes32(uint256(1)))
        returns (bool bad) {
            b.edVerifyBad = !bad; // want: tampered sig is REJECTED
        } catch {}

        // --- 0x802 metagraph ---
        try IMetagraph(IMetagraph_ADDRESS).getUidCount(netuid) returns (uint16 n) {
            b.mgOk = true;
            b.uidCount = n;
            if (n > 0) {
                try IMetagraph(IMetagraph_ADDRESS).getHotkey(netuid, 0) returns (bytes32 hk) {
                    b.uid0Hotkey = hk;
                } catch {}
                try IMetagraph(IMetagraph_ADDRESS).getColdkey(netuid, 0) returns (bytes32 ck) {
                    b.uid0Coldkey = ck;
                } catch {}
            }
        } catch {}

        // --- 0x805 staking view: the probe's OWN stake at sampleHotkey ---
        bytes32 selfCk = Blake2b.mirror(address(this));
        try IStaking(ISTAKING_ADDRESS).getStake(sampleHotkey, selfCk, uint256(netuid)) returns (uint256 v) {
            b.stakeViewOk = true;
            b.sampleSelfStake = v;
        } catch {}
    }

    /// @dev external so readBattery can try/catch the library staticcall.
    function mirrorExt(address a) external view returns (bytes32) {
        return Blake2b.mirror(a);
    }

    /// @notice Plain view of the probe's stake under its own (contract) coldkey
    ///         at `hotkey` — the custody + rao-unit probe. Compare the returned
    ///         value to the dust α you know you moved in.
    function selfStake(bytes32 hotkey) external view returns (uint256) {
        return IStaking(ISTAKING_ADDRESS).getStake(hotkey, Blake2b.mirror(address(this)), uint256(netuid));
    }

    // ------------------------------------------------------------------
    // Value-bearing checks — owner-gated, DUST real α. `cast send`.
    // ------------------------------------------------------------------

    /// @notice Convert dust TAO -> α stake under the probe's own coldkey via
    ///         addStake, so the move/transfer checks have α to work with (the
    ///         self-contained path — no pre-existing α position needed). The
    ///         amount arg is documented as rao; forwarding msg.value covers the
    ///         payable ambiguity. Emits the observed post-stake balance so the
    ///         RAO-vs-18-dec unit scale is read off directly.
    function seedFromTao(bytes32 hotkey, uint256 raoAmount) external payable onlyOwner {
        IStaking(ISTAKING_ADDRESS).addStake{value: msg.value}(hotkey, raoAmount, uint256(netuid));
        uint256 after_ = IStaking(ISTAKING_ADDRESS).getStake(hotkey, Blake2b.mirror(address(this)), uint256(netuid));
        emit Seeded(hotkey, raoAmount, msg.value, after_);
    }

    /// @notice moveStake `amount` from one hotkey to another under the probe's
    ///         coldkey, recording both balances before/after. Confirms (a)
    ///         contract-as-coldkey custody (the moved stake is the probe's),
    ///         (b) slippage-free within-netuid (fromBefore-fromAfter ==
    ///         toAfter-toBefore == amount at the rao scale). Returns the four
    ///         readings for the driver to log.
    function moveRoundTrip(bytes32 fromHotkey, bytes32 toHotkey, uint256 amount)
        external
        onlyOwner
        returns (uint256 fromBefore, uint256 toBefore, uint256 fromAfter, uint256 toAfter)
    {
        bytes32 self = Blake2b.mirror(address(this));
        fromBefore = IStaking(ISTAKING_ADDRESS).getStake(fromHotkey, self, uint256(netuid));
        toBefore = IStaking(ISTAKING_ADDRESS).getStake(toHotkey, self, uint256(netuid));
        IStaking(ISTAKING_ADDRESS).moveStake(fromHotkey, toHotkey, uint256(netuid), uint256(netuid), amount);
        fromAfter = IStaking(ISTAKING_ADDRESS).getStake(fromHotkey, self, uint256(netuid));
        toAfter = IStaking(ISTAKING_ADDRESS).getStake(toHotkey, self, uint256(netuid));
        emit MoveRoundTrip(fromHotkey, toHotkey, amount, fromBefore, fromAfter, toBefore, toAfter);
    }

    /// @notice transferStake dust back out to `destColdkey` — recovers the
    ///         probe funds AND exercises transferStake from a contract (the
    ///         payout path STSubnet uses for claims).
    function transferOut(bytes32 destColdkey, bytes32 hotkey, uint256 amount) external onlyOwner {
        IStaking(ISTAKING_ADDRESS).transferStake(destColdkey, hotkey, uint256(netuid), uint256(netuid), amount);
    }

    // ------------------------------------------------------------------
    // Dividend auto-compounding (§7.4 reserve leg) — the two-step check.
    //   1. stake dust under a VALIDATING hotkey (seedFromTao / moveRoundTrip)
    //   2. snapshot() now; dividendDelta() after >= 1 tempo -> delta > 0 proves
    //      dividends auto-restake onto (hotkey, mirror(this)) with no action.
    // ------------------------------------------------------------------

    function snapshot(bytes32 hotkey) external onlyOwner {
        uint256 base = IStaking(ISTAKING_ADDRESS).getStake(hotkey, Blake2b.mirror(address(this)), uint256(netuid));
        divBaseline[hotkey] = base;
        divBaselineBlock[hotkey] = uint64(block.number);
        emit DividendSnapshot(hotkey, base, uint64(block.number));
    }

    function dividendDelta(bytes32 hotkey)
        external
        view
        returns (uint256 baseline, uint256 current, uint64 sinceBlock)
    {
        baseline = divBaseline[hotkey];
        current = IStaking(ISTAKING_ADDRESS).getStake(hotkey, Blake2b.mirror(address(this)), uint256(netuid));
        sinceBlock = divBaselineBlock[hotkey];
    }
}
