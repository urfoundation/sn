"""Tests of guardian_associate.py.

The unit tests need only the bittensor SDK. The node tests run when GUARDIAN_ASSOCIATE_NODE names a local
chain's ws:// endpoint whose //Alice is funded and whose MEV shield runs: upstream's localnet, e.g.
  docker run -d -p 127.0.0.1:29944:9944 ghcr.io/raofoundation/subtensor-localnet:v490 True
  GUARDIAN_ASSOCIATE_NODE=ws://127.0.0.1:29944 python -I mainnet/guardian_associate_test.py -v
The node tests refuse a chain whose genesis is Finney's. A software ed25519 key stands in for the Ledger:
it takes the same metadata digest and builds the same RFC-0078 proof the device would, so the inner carries
CheckMetadataHash's digest exactly as on the device.
"""

import asyncio
import hashlib
import os
import secrets
import sys
import tempfile
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import guardian_associate as ga  # noqa: E402

FINNEY_GENESIS = "0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03"
NODE = os.environ.get("GUARDIAN_ASSOCIATE_NODE")
TAO = 10**9


class Unit(unittest.TestCase):
    def test_selectors(self):
        from Crypto.Hash import keccak

        for signature, selector in (("getHotkeyOwner(bytes32)", ga.GET_HOTKEY_OWNER), ("selfColdkey()", ga.SELF_COLDKEY)):
            digest = keccak.new(digest_bits=256)
            digest.update(signature.encode())
            self.assertEqual(digest.hexdigest()[:8], selector)

    def test_vault2_coldkey_is_its_evm_mirror(self):
        mirror = hashlib.blake2b(b"evm:" + bytes.fromhex("e4865eb1bebdf5c6a10c9d37f3ccdd59aee8f20c"), digest_size=32)
        self.assertEqual(mirror.hexdigest(), "656918b9cd3624156def4a61b421eefe633242ac185d45b28ecb7ca1eb6c6e51")
        self.assertEqual(ga.ss58(mirror.digest()), "5EMfux1tRHAL2TSRss8W5cD1tUMqT23F2oeUsRfshErhHUPq")

    def test_storage_keys(self):
        # Owner of the zero account, and the timestamp's Now: standard Substrate prefixes.
        self.assertEqual(ga.value_key("Timestamp", "Now"),
                         "0xf0c365c3cf59d671eb72da0e7a4113c49f1f0515f462cdcf84e0f1d6045dfcbb")
        key = ga.map_key("SubtensorModule", "Owner", bytes(32), "blake2_128_concat")
        self.assertTrue(key.endswith(hashlib.blake2b(bytes(32), digest_size=16).hexdigest() + "00" * 32))

    def test_swap_commitments(self):
        account = bytes(range(32))
        self.assertEqual(ga.legacy_swap_hash(account), hashlib.blake2b(account, digest_size=32).digest())
        self.assertNotEqual(ga.v2_swap_hash(account, True), ga.v2_swap_hash(account, False))
        self.assertEqual(len(b"coldkey-swap-v2-policy"), 22)

    def test_account_parsing(self):
        raw = bytes.fromhex("656918b9cd3624156def4a61b421eefe633242ac185d45b28ecb7ca1eb6c6e51")
        self.assertEqual(ga.account_bytes("0x" + raw.hex(), "x"), raw)
        self.assertEqual(ga.account_bytes(ga.ss58(raw), "x"), raw)
        self.assertEqual(ga.account_bytes("0x" + raw.hex().upper(), "x"), raw)
        for bad in ("0x" + "00" * 32, "0x" + raw.hex()[:-2], "0x" + raw.hex() + "00", "not-an-address",
                    ga.ss58(raw)[:-1] + "x"):
            with self.assertRaises(ValueError):
                ga.account_bytes(bad, "x")

    def test_usage(self):
        base = ["associate", "--rpc", "ws://127.0.0.1:1", "--hotkey", "0x" + "11" * 32, "--guardian", "0x" + "22" * 32]
        self.assertEqual(ga.parse(base).era, 64)
        for extra in (["--send"], ["--send", "--ledger-account", "11"], ["--era", "100"], ["--era", "8"],
                      ["--vault", "0x1234"], ["--genesis", "0x12"]):
            with self.assertRaises(SystemExit):
                ga.parse(base + extra)


class SoftLedger:
    """An ed25519 key behind the Ledger signer's interface: metadata digest, RFC-0078 proof, clear signing."""

    crypto_type = 0
    ss58_format = 42

    def __init__(self):
        import bittensor_core

        self.core = bittensor_core
        self.key = bittensor_core.Keypair.create_from_seed(secrets.token_bytes(32), bittensor_core.CRYPTO_ED25519)
        self.public_key = bytes(self.key.public_key)
        self.ss58_address = self.key.ss58_address
        self.context = None
        self.signed = []

    def metadata_digest(self, context):
        self.context = context
        return bytes(self.core.metadata_digest(context.metadata_bytes, context.spec_version, context.spec_name,
                                               context.ss58_format, 9, "TAO"))

    def sign_unsigned_extrinsic(self, unsigned):
        context = self.context
        proof = bytes(self.core.generate_extrinsic_proof(
            unsigned.call_data, unsigned.included_in_extrinsic, unsigned.included_in_signed_data,
            context.metadata_bytes, context.spec_version, context.spec_name, context.ss58_format, 9, "TAO"))
        payload = unsigned.call_data + unsigned.included_in_extrinsic + unsigned.included_in_signed_data
        expected = payload if len(payload) <= 256 else hashlib.blake2b(payload, digest_size=32).digest()
        assert bytes(unsigned.payload) == expected, "the payload is not its seams"
        assert unsigned.metadata_hash is not None, "the inner does not carry CheckMetadataHash's digest"
        self.signed.append({"payload": len(payload), "proof": len(proof), "call": unsigned.call_data.hex()})
        return b"\x00" + bytes(self.key.sign(unsigned.payload))

    def sign(self, _data):
        raise RuntimeError("the generic app does not blind-sign")


class StaleFirstKey(ga.Hooks):
    """Holds the first carrier back until its key has expired, as a slow approval would."""

    def __init__(self, blocks):
        self.blocks = blocks

    async def after_encrypt(self, attempt, chain):
        if attempt == 0:
            await asyncio.sleep(self.blocks * await chain.substrate.block_time())


def run(coroutine):
    return asyncio.run(coroutine)


@unittest.skipUnless(NODE, "GUARDIAN_ASSOCIATE_NODE is unset")
class Node(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        import bittensor_core

        cls.core = bittensor_core
        cls.alice = bittensor_core.Keypair.create_from_uri("//Alice")
        cls.tmp = tempfile.mkdtemp(prefix="guardian-associate-")
        genesis = run(cls.read(lambda chain: chain.rpc("chain_getBlockHash", [0])))
        if genesis == FINNEY_GENESIS:
            raise unittest.SkipTest("refusing to run against Finney")
        cls.guardian = SoftLedger()
        cls.carrier_wallet = "carrier-" + secrets.token_hex(4)
        from bittensor import wallets

        carrier = wallets.regen_coldkey(name=cls.carrier_wallet, path=cls.tmp, seed="0x" + secrets.token_hex(32),
                                        use_password=False, crypto_type=1)
        cls.carrier = bytes(carrier.coldkeypub.public_key)
        cls.fund(cls.guardian.public_key, TAO)
        cls.fund(cls.carrier, TAO)

    # -- helpers ------------------------------------------------------------------------------------------

    @staticmethod
    async def read(operation):
        from bittensor.client import Client

        client = Client(NODE, fallback_endpoints=[], archive_endpoints=[])
        await client.connect()
        try:
            return await operation(ga.Chain(client))
        finally:
            await client.close()

    @classmethod
    def submit(cls, keypair, module, function, params):
        async def go(chain):
            call = await chain.substrate.compose((module, function, params))
            return await chain.substrate.submit(call, keypair, wait_for_inclusion=True, wait_for_finalization=False)

        return run(cls.read(go))

    @classmethod
    def fund(cls, account, amount):
        result = cls.submit(cls.alice, "Balances", "transfer_keep_alive", {"dest": ga.ss58(account), "value": amount})
        assert result.success, result.message

    def keypair(self, amount=10 * TAO):
        key = self.core.Keypair.create_from_seed(secrets.token_bytes(32), self.core.CRYPTO_SR25519)
        self.fund(bytes(key.public_key), amount)
        return key

    @staticmethod
    def mirror():
        """A vault-like coldkey: the EVM mirror of a fresh address, which no key signs for."""
        return hashlib.blake2b(b"evm:" + secrets.token_bytes(20), digest_size=32).digest()

    def owner(self, account):
        async def go(chain):
            _, block_hash = await chain.best()
            return await chain.owner(account, block_hash)

        return run(self.read(go))

    def nonce(self, account):
        return run(self.read(lambda chain: chain.nonce(account)))

    def wait_blocks(self, count):
        target = run(self.read(lambda chain: chain.best()))[0] + count
        while run(self.read(lambda chain: chain.best()))[0] < target:
            time.sleep(0.25)

    def tool(self, hotkey, *extra, signer=None, hooks=None):
        argv = ["associate", "--rpc", NODE, "--hotkey", ga.hex0x(hotkey), "--guardian", self.guardian.ss58_address,
                "--carrier-wallet", self.carrier_wallet, "--wallet-path", self.tmp, "--ledger-account", "0", *extra]
        args = ga.parse(argv)
        signer = signer or self.guardian
        return run(ga.associate(args, signer_factory=lambda: signer, hooks=hooks))

    # -- cases --------------------------------------------------------------------------------------------

    def test_dry_run_sends_nothing(self):
        hotkey = self.mirror()
        before = (self.nonce(self.guardian.public_key), self.nonce(self.carrier), len(self.guardian.signed))
        status, report = self.tool(hotkey)
        self.assertEqual(status, 0, report)
        self.assertEqual(report["status"], "dry run: nothing signed or sent")
        # SubtensorModule (7) try_associate_hotkey (91), carried by MevShield (30) submit_encrypted (1).
        self.assertEqual(report["call"]["call_data"][:6], "0x075b")
        self.assertEqual(report["shield"]["carrier_call_index"], "0x1e01")
        self.assertEqual(report["call"]["hotkey"], ga.ss58(hotkey))
        self.assertGreater(report["carrier"]["fee_rao"], 0)
        after = (self.nonce(self.guardian.public_key), self.nonce(self.carrier), len(self.guardian.signed))
        self.assertEqual(before, after)
        self.assertIsNone(self.owner(hotkey))

    def test_associates_vault_like_and_carry_like_coldkeys_through_the_shield(self):
        for name in ("vault-like", "carry-like"):
            with self.subTest(name):
                hotkey = self.mirror()
                carrier_nonce = self.nonce(self.carrier)
                status, report = self.tool(hotkey, "--send")
                self.assertEqual(status, 0, report)
                self.assertEqual(report["status"], "associated")
                self.assertEqual(report["verified"]["owner"], ga.hex0x(self.guardian.public_key))
                self.assertEqual(report["verified"]["getHotkeyOwner"],
                                 {"exists": True, "owner": ga.hex0x(self.guardian.public_key)})
                self.assertEqual(self.owner(hotkey), self.guardian.public_key)
                # Only carriers went out in the clear: the carrier's nonce moved once per carrier included, and
                # the inner landed in a carrier's block or just after.
                attempts = report["attempts"]
                included = sum(1 for attempt in attempts if attempt["carrier"]["success"])
                self.assertEqual(self.nonce(self.carrier), carrier_nonce + included)
                last = attempts[-1]
                self.assertTrue(last["carrier"]["success"])
                self.assertTrue(last["inner"]["success"])
                self.assertLessEqual(last["inner"]["block"] - last["carrier"]["block"], ga.FOLLOW_BLOCKS)
                self.assertLessEqual(self.guardian.signed[-1]["payload"] + self.guardian.signed[-1]["proof"], 16 * 1024)
                # Once done, the tool sends nothing more.
                again, report = self.tool(hotkey, "--send")
                self.assertEqual(again, 0, report)
                self.assertEqual(report["status"], "already the guardian's hotkey")
                self.assertEqual(self.nonce(self.carrier), carrier_nonce + included)

    def test_a_stale_key_is_retried_under_a_fresh_one(self):
        hotkey = self.mirror()
        signed = len(self.guardian.signed)
        status, report = self.tool(hotkey, "--send", hooks=StaleFirstKey(blocks=6))
        self.assertEqual(status, 0, report)
        # The held-back carrier misses (dropped from the pool, or included with nothing the author can
        # decrypt); a later one under a fresh key lands, and the Ledger signed once.
        self.assertGreaterEqual(len(report["attempts"]), 2)
        self.assertNotIn("inner", report["attempts"][0])
        self.assertTrue(report["attempts"][-1]["inner"]["success"])
        self.assertEqual(len(self.guardian.signed), signed + 1, "the Ledger signs once")
        self.assertEqual(self.owner(hotkey), self.guardian.public_key)

    def test_a_coldkey_swap_into_the_guardians_hotkey_is_refused(self):
        hotkey = self.mirror()
        swapper = self.keypair()
        # A legacy announcement made before the association commits only to a hash and reserves nothing.
        announced = self.submit(swapper, "SubtensorModule", "announce_coldkey_swap",
                                {"new_coldkey_hash": ga.hex0x(ga.legacy_swap_hash(hotkey))})
        self.assertTrue(announced.success, announced.message)
        status, report = self.tool(hotkey, "--send")
        self.assertEqual(status, 0, report)
        self.assertEqual([a["kind"] for a in report["announcements"]], ["legacy"])
        # A v2 announcement into the account is refused before any fee.
        other = self.keypair()
        refused = self.submit(other, "SubtensorModule", "announce_coldkey_swap_v2", {"new_coldkey": ga.ss58(hotkey)})
        self.assertFalse(refused.success)
        self.assertEqual(refused.error.name, "NewColdKeyIsHotkey", refused.message)
        # The legacy announcement's execution is refused too, after its delay (50 blocks on a fast-runtime
        # chain; mainnet's five days are not waited for).
        delay = run(self.read(lambda chain: chain.u32_value("SubtensorModule", "ColdkeySwapAnnouncementDelay")))
        if delay > 200:
            self.skipTest(f"the announcement delay is {delay} blocks")
        self.wait_blocks(delay + 1)
        executed = self.submit(swapper, "SubtensorModule", "swap_coldkey_announced", {"new_coldkey": ga.ss58(hotkey)})
        self.assertFalse(executed.success)
        self.assertEqual(executed.error.name, "NewColdKeyIsHotkey", executed.message)
        self.assertEqual(self.owner(hotkey), self.guardian.public_key)

    def test_someone_elses_hotkey_is_refused_before_signing(self):
        hotkey = self.mirror()
        third = self.keypair()
        taken = self.submit(third, "SubtensorModule", "try_associate_hotkey", {"hotkey": ga.ss58(hotkey)})
        self.assertTrue(taken.success, taken.message)
        signed = len(self.guardian.signed)
        for extra in ((), ("--send",)):
            status, report = self.tool(hotkey, *extra)
            self.assertEqual(status, 3, report)
            self.assertIn(ga.hex0x(bytes(third.public_key)), report["reason"])
        self.assertEqual(len(self.guardian.signed), signed)
        # And the guardian's association of it would change nothing: the third party keeps it.
        noop = self.submit(self.guardian, "SubtensorModule", "try_associate_hotkey", {"hotkey": ga.ss58(hotkey)})
        self.assertTrue(noop.success, noop.message)
        self.assertEqual(self.owner(hotkey), bytes(third.public_key))

    def test_a_reserved_destination_is_refused(self):
        hotkey = self.mirror()
        announcer = self.keypair()
        reserved = self.submit(announcer, "SubtensorModule", "announce_coldkey_swap_v2", {"new_coldkey": ga.ss58(hotkey)})
        self.assertTrue(reserved.success, reserved.message)
        status, report = self.tool(hotkey, "--send")
        self.assertEqual(status, 3, report)
        self.assertTrue(report["reservation"]["live"])
        self.assertIn("ColdkeySwapDestinationReserved", report["reason"])
        direct = self.submit(self.guardian, "SubtensorModule", "try_associate_hotkey", {"hotkey": ga.ss58(hotkey)})
        self.assertFalse(direct.success)
        self.assertEqual(direct.error.name, "ColdkeySwapDestinationReserved", direct.message)
        self.assertIsNone(self.owner(hotkey))

    def test_refusals_before_signing(self):
        hotkey = self.mirror()
        signed = len(self.guardian.signed)
        cases = (
            (("--genesis", "0x" + "01" * 32), "genesis"),
            (("--vault", "0x" + "ab" * 20), "selfColdkey"),
        )
        for extra, word in cases:
            status, report = self.tool(hotkey, "--send", *extra)
            self.assertEqual(status, 3, report)
            self.assertIn(word, report["reason"])
        status, report = self.tool(hotkey, "--send", signer=SoftLedger())
        self.assertEqual(status, 3, report)
        self.assertIn("not the guardian", report["reason"])
        self.assertEqual(len(self.guardian.signed), signed)
        self.assertIsNone(self.owner(hotkey))


if __name__ == "__main__":
    unittest.main()
