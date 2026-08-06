// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

import {STBase} from "./utils/STBase.sol";
import {STSubnet} from "../src/STSubnet.sol";
import {Blake2b} from "../src/lib/Blake2b.sol";

/// @dev Head-tier client_id <-> hotkey binding (WHITEPAPER §8.4/§11.4, D-18).
///      Mirrors the registerValidator (mirror + 0x402) proof shape (D-10): a
///      top-level miner proves control of BOTH the hotkey (coldkey == mirror of
///      the caller) AND the client_id (Ed25519 sig over the domain-separated
///      digest). Covers the happy path, both halves of the dual proof failing,
///      a non-live UID, the bijection/rotation rule, unbind, and the core
///      anti-theft property (a miner cannot claim a client_id it does not
///      operate). Uses free metagraph uids >= 5 (base setUp occupies 0..2).
contract HeadBindingTest is STBase {
    event HeadBound(
        bytes32 indexed hotkey, bytes32 indexed clientId, uint16 uid, address registrant
    );
    event HeadUnbound(
        bytes32 indexed hotkey, bytes32 indexed clientId, uint16 uid, address registrant
    );

    // ------------------------------------------------------------------
    // headBindDigest — domain-separated, replay-proof (like vpkBindDigest)
    // ------------------------------------------------------------------

    /// @dev Pins the exact 168-byte head-bind preimage
    ///      (domain ‖ chainid ‖ proxy ‖ registrant ‖ hotkey ‖ clientId).
    function test_headBindDigest_exactBytes() public view {
        address registrant = address(0x2222222222222222222222222222222222222222);
        bytes32 hk = keccak256("hb-hk");
        bytes32 cid = keccak256("hb-cid");
        bytes memory preimage = abi.encodePacked(
            keccak256("UR_ST_HEAD_BIND_V1"),
            uint256(block.chainid),
            address(st),
            registrant,
            hk,
            cid
        );
        assertEq(preimage.length, 168);
        assertEq(st.headBindDigest(registrant, hk, cid), keccak256(preimage));
        assertEq(st.HEAD_BIND_DOMAIN(), keccak256("UR_ST_HEAD_BIND_V1"));
    }

    // ------------------------------------------------------------------
    // bindHead — happy path + both maps + event
    // ------------------------------------------------------------------

    function test_bindHead_happy_setsBothMapsAndEmits() public {
        address w = makeAddr("head-1");
        bytes32 hk = keccak256("head-hk-1");
        bytes32 cid = keccak256("client-id-1");
        metagraph.setNeuron(5, hk, Blake2b.mirror(w));

        vm.expectEmit(true, true, false, true, address(st));
        emit HeadBound(hk, cid, 5, w);
        vm.prank(w);
        st.bindHead(hk, cid, _sig());

        assertEq(st.headHotkeyToClientId(hk), cid);
        assertEq(st.headClientIdToHotkey(cid), hk);
    }

    // ------------------------------------------------------------------
    // bindHead — dual-proof failures
    // ------------------------------------------------------------------

    function test_bindHead_badClientSig_reverts() public {
        address w = makeAddr("head-badsig");
        bytes32 hk = keccak256("head-hk-badsig");
        bytes32 cid = keccak256("client-id-badsig");
        metagraph.setNeuron(5, hk, Blake2b.mirror(w));

        // 0x402 rejects exactly this (digest, clientId, r, s) tuple — the caller
        // does not actually hold client_id's Ed25519 key.
        ed.setBad(
            st.headBindDigest(w, hk, cid), cid, keccak256("sig-r"), keccak256("sig-s"), true
        );
        vm.prank(w);
        vm.expectRevert("ST: bad client sig");
        st.bindHead(hk, cid, _sig());
    }

    function test_bindHead_wrongColdkeyMirror_reverts() public {
        // hotkey is a live UID but its coldkey is NOT mirror(msg.sender)
        address w = makeAddr("head-wrongck");
        bytes32 hk = keccak256("head-hk-wrongck");
        bytes32 cid = keccak256("client-id-wrongck");
        metagraph.setNeuron(5, hk, keccak256("someone-elses-coldkey"));
        vm.prank(w);
        vm.expectRevert("ST: coldkey != mirror(sender)");
        st.bindHead(hk, cid, _sig());
    }

    function test_bindHead_nonLiveUid_failsClosed() public {
        // hotkey is not in the metagraph -> the lookup reverts (fail-closed)
        bytes32 hk = keccak256("not-in-metagraph-hk");
        bytes32 cid = keccak256("client-id-nolive");
        vm.prank(rando);
        vm.expectRevert("ST: uid not found");
        st.bindHead(hk, cid, _sig());
    }

    function test_bindHead_inputGuards() public {
        vm.expectRevert("ST: zero key");
        st.bindHead(bytes32(0), keccak256("c"), _sig());
        vm.expectRevert("ST: zero key");
        st.bindHead(keccak256("h"), bytes32(0), _sig());
        vm.expectRevert("ST: sig length");
        st.bindHead(keccak256("h"), keccak256("c"), hex"deadbeef");
    }

    // ------------------------------------------------------------------
    // bindHead — anti-theft (the reason for the dual signature, §11.4)
    // ------------------------------------------------------------------

    /// @dev A miner cannot claim a client_id it does not operate and steal its
    ///      measured quality: binding a client_id to any hotkey requires an
    ///      Ed25519 signature BY that client_id, which an attacker lacking the
    ///      key cannot produce. The victim's binding is untouched.
    function test_bindHead_cannotStealAnotherProvidersClientId() public {
        // provider 1 legitimately binds (h1, cid)
        address p1 = makeAddr("provider-1");
        bytes32 h1 = keccak256("p1-hotkey");
        bytes32 cid = keccak256("measured-client-id");
        metagraph.setNeuron(5, h1, Blake2b.mirror(p1));
        vm.prank(p1);
        st.bindHead(h1, cid, _sig());

        // attacker controls its OWN hotkey h2 (mirror ok) but NOT cid's key.
        address att = makeAddr("attacker");
        bytes32 h2 = keccak256("attacker-hotkey");
        metagraph.setNeuron(6, h2, Blake2b.mirror(att));
        ed.setBad(
            st.headBindDigest(att, h2, cid), cid, keccak256("sig-r"), keccak256("sig-s"), true
        );

        vm.prank(att);
        vm.expectRevert("ST: bad client sig");
        st.bindHead(h2, cid, _sig());

        // the original binding is untouched
        assertEq(st.headClientIdToHotkey(cid), h1);
        assertEq(st.headHotkeyToClientId(h1), cid);
    }

    // ------------------------------------------------------------------
    // bindHead — bijection & rotation rule
    // ------------------------------------------------------------------

    function test_bindHead_idempotentSamePair() public {
        address w = makeAddr("head-idem");
        bytes32 hk = keccak256("idem-hk");
        bytes32 cid = keccak256("idem-cid");
        metagraph.setNeuron(5, hk, Blake2b.mirror(w));
        vm.startPrank(w);
        st.bindHead(hk, cid, _sig());
        st.bindHead(hk, cid, _sig()); // idempotent re-bind of the same pair
        vm.stopPrank();
        assertEq(st.headHotkeyToClientId(hk), cid);
        assertEq(st.headClientIdToHotkey(cid), hk);
    }

    /// @dev The client_id is the DURABLE identity, the hotkey CHURNS (§8.4). A
    ///      demoted provider re-points its client_id onto a freshly-registered
    ///      hotkey WITHOUT first unbinding the old (possibly lost) hotkey; the
    ///      stale forward map is cleared, keeping a clean bijection.
    function test_bindHead_rotateClientIdToNewHotkey() public {
        address prov = makeAddr("rotating-provider");
        bytes32 hOld = keccak256("old-hotkey");
        bytes32 hNew = keccak256("new-hotkey");
        bytes32 cid = keccak256("durable-client-id");

        metagraph.setNeuron(5, hOld, Blake2b.mirror(prov));
        vm.prank(prov);
        st.bindHead(hOld, cid, _sig());

        metagraph.setNeuron(6, hNew, Blake2b.mirror(prov));
        vm.prank(prov);
        st.bindHead(hNew, cid, _sig());

        assertEq(st.headClientIdToHotkey(cid), hNew);
        assertEq(st.headHotkeyToClientId(hNew), cid);
        assertEq(st.headHotkeyToClientId(hOld), bytes32(0), "stale forward map cleared");
    }

    /// @dev The hotkey's controller may re-point it to a new client_id; the
    ///      old client_id's reverse map is cleared (bijection preserved).
    function test_bindHead_repointHotkeyToNewClientId() public {
        address prov = makeAddr("repoint-provider");
        bytes32 hk = keccak256("repoint-hotkey");
        bytes32 c1 = keccak256("client-1");
        bytes32 c2 = keccak256("client-2");
        metagraph.setNeuron(5, hk, Blake2b.mirror(prov));

        vm.startPrank(prov);
        st.bindHead(hk, c1, _sig());
        st.bindHead(hk, c2, _sig()); // re-point to a new client_id
        vm.stopPrank();

        assertEq(st.headHotkeyToClientId(hk), c2);
        assertEq(st.headClientIdToHotkey(c2), hk);
        assertEq(st.headClientIdToHotkey(c1), bytes32(0), "stale reverse map cleared");
    }

    // ------------------------------------------------------------------
    // unbindHead — demotion / exit
    // ------------------------------------------------------------------

    function test_unbindHead_happy_clearsBothMapsAndEmits() public {
        address w = makeAddr("unbind-w");
        bytes32 hk = keccak256("unbind-hk");
        bytes32 cid = keccak256("unbind-cid");
        metagraph.setNeuron(5, hk, Blake2b.mirror(w));
        vm.prank(w);
        st.bindHead(hk, cid, _sig());

        vm.expectEmit(true, true, false, true, address(st));
        emit HeadUnbound(hk, cid, 5, w);
        vm.prank(w);
        st.unbindHead(hk);

        assertEq(st.headHotkeyToClientId(hk), bytes32(0));
        assertEq(st.headClientIdToHotkey(cid), bytes32(0));
    }

    function test_unbindHead_notBound_reverts() public {
        vm.prank(rando);
        vm.expectRevert("ST: not bound");
        st.unbindHead(keccak256("never-bound-hk"));
    }

    function test_unbindHead_wrongController_reverts() public {
        address w = makeAddr("owner-of-hk");
        bytes32 hk = keccak256("uh-hk");
        bytes32 cid = keccak256("uh-cid");
        metagraph.setNeuron(5, hk, Blake2b.mirror(w));
        vm.prank(w);
        st.bindHead(hk, cid, _sig());

        // a wallet whose mirror != the hotkey's coldkey cannot unbind it
        vm.prank(rando);
        vm.expectRevert("ST: coldkey != mirror(sender)");
        st.unbindHead(hk);
        // binding intact
        assertEq(st.headClientIdToHotkey(cid), hk);
    }
}
