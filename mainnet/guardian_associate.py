"""Make an account a hotkey account of the guardian, through the MEV shield.

Capture the flag (CAPTURETHEFLAG.md §4.6, §11.2): a coldkey swap refuses a destination that is a hotkey
account (NewColdKeyIsHotkey), so the guardian, a coldkey that never swaps the hotkeys it holds, makes
vault2's coldkey, and later the carry's, its own hotkey account with
SubtensorModule.try_associate_hotkey. Anyone can associate a free account, and whoever holds it can release
it together with a coldkey swap into it, so the association travels encrypted (MevShield.submit_encrypted)
and is checked afterwards: getHotkeyOwner must name the guardian.

The guardian signs only the inner try_associate_hotkey, on its Ledger (Polkadot generic app, clear-signed
with CheckMetadataHash): one approval. A local carrier key signs the MevShield.submit_encrypted carrier. The
encryption key (MevShield.NextKey) serves about two blocks, so it is read after the Ledger approval, and the
carrier is sent again under a fresh key until the inner lands or its mortal era ends.

  address    print a Ledger account's address and public key (--confirm shows it on the device)
  associate  check, and print what would be signed and sent; with --send, sign and send

associate refuses (exit 3, nothing sent) when the account is someone else's hotkey, is reserved as a coldkey
swap's destination, is a hashed account a classical guardian cannot own, when --vault's selfColdkey() is
another account, when --genesis names another chain, or when the guardian or the carrier cannot pay. When
the account is already the guardian's hotkey it reports that and sends nothing.

Run with the virtualenv that has btcli (bittensor 11), in isolated mode:
  python -I mainnet/guardian_associate.py associate --rpc ws://172.28.208.185:9944 \\
    --genesis 0x2f0555cc76fc2840a25a6ea3b9637146806f1f44b090c175ffde2a7e5ab36c03 \\
    --hotkey 0x656918b9cd3624156def4a61b421eefe633242ac185d45b28ecb7ca1eb6c6e51 \\
    --vault 0xe4865eb1bebdf5c6a10c9d37f3ccdd59aee8f20c --guardian <ss58> \\
    --ledger-account 11 --carrier-wallet <wallet> [--send]

The report is one JSON object on stdout; progress goes to stderr. Exit status: 0 done, already done, or a
clean dry run; 2 usage; 3 refused before sending; 4 sent but not confirmed.
"""

import argparse
import asyncio
import hashlib
import json
import os
import sys
import time

SCHEMA = "urnetwork-mainnet-guardian-associate-v1"
SS58_FORMAT = 42
STAKING_V2 = "0x0000000000000000000000000000000000000805"
GET_HOTKEY_OWNER = "ad6b780c"  # keccak256("getHotkeyOwner(bytes32)")[:4]
SELF_COLDKEY = "877e4394"  # keccak256("selfColdkey()")[:4]
MAX_STAKING_HOTKEYS = 256
CARRIER_ERA = 8  # the runtime refuses a submit_encrypted with a longer era
SHIELD_OVERHEAD = 16 + 2 + 1088 + 24 + 16  # key hash, kem length, ML-KEM-768 ciphertext, nonce, AEAD tag
FOLLOW_BLOCKS = 2  # blocks after the carrier's in which the inner may still appear


class Refused(Exception):
    """A check refused the association; nothing was sent."""


class Unconfirmed(Exception):
    """Something was sent, and the association is not confirmed."""


def log(message):
    sys.stderr.write(f"[guardian] {message}\n")
    sys.stderr.flush()


# -- keys and encodings ------------------------------------------------------------------------------------

def twox128(data):
    import xxhash

    return xxhash.xxh64(data, seed=0).intdigest().to_bytes(8, "little") + xxhash.xxh64(
        data, seed=1
    ).intdigest().to_bytes(8, "little")


def twox64(data):
    import xxhash

    return xxhash.xxh64(data, seed=0).intdigest().to_bytes(8, "little")


def blake2(data, size):
    return hashlib.blake2b(data, digest_size=size).digest()


def map_key(pallet, item, key, hasher):
    prefix = twox128(pallet.encode()) + twox128(item.encode())
    if hasher == "blake2_128_concat":
        return "0x" + (prefix + blake2(key, 16) + key).hex()
    if hasher == "twox64_concat":
        return "0x" + (prefix + twox64(key) + key).hex()
    raise ValueError(hasher)


def value_key(pallet, item):
    return "0x" + (twox128(pallet.encode()) + twox128(item.encode())).hex()


def legacy_swap_hash(account):
    """announce_coldkey_swap's commitment: BlakeTwo256 of the destination."""
    return blake2(account, 32)


def v2_swap_hash(account, import_policies):
    """announce_coldkey_swap_v2's commitment (swap_coldkey.rs coldkey_swap_v2_announcement_hash)."""
    return blake2(b"coldkey-swap-v2-policy" + account + bytes([1 if import_policies else 0]), 32)


def compact_length(raw):
    mode = raw[0] & 3
    if mode == 0:
        return raw[0] >> 2
    if mode == 1:
        return int.from_bytes(raw[:2], "little") >> 2
    if mode == 2:
        return int.from_bytes(raw[:4], "little") >> 2
    raise ValueError("compact length too large")


def account_bytes(value, name):
    """A 0x 32-byte public key, or an ss58 address of the chain's format."""
    import bittensor_core

    if isinstance(value, str) and value.startswith("0x"):
        raw = bytes.fromhex(value[2:]) if len(value) == 66 else b""
        if len(raw) != 32 or "0x" + raw.hex() != value.lower():
            raise ValueError(f"{name} must be 0x and 32 bytes of hex, or an ss58 address")
    else:
        try:
            raw = bytes(bittensor_core.ss58_decode(value))
        except Exception:
            raise ValueError(f"{name} must be 0x and 32 bytes of hex, or an ss58 address") from None
        if ss58(raw) != value:
            raise ValueError(f"{name} is not an ss58 address of format {SS58_FORMAT}")
    if raw == bytes(32):
        raise ValueError(f"{name} is the zero account")
    return raw


def ss58(raw):
    import bittensor_core

    return bittensor_core.ss58_encode(raw, SS58_FORMAT)


def hex0x(raw):
    return "0x" + raw.hex()


# -- chain reads -------------------------------------------------------------------------------------------

class Chain:
    """Reads through one pinned endpoint: no fallback, no archive, no public pool."""

    def __init__(self, client):
        self.client = client
        self.substrate = client._substrate

    async def rpc(self, method, params):
        return await self.substrate.raw.rpc_request(method, params)

    async def storage(self, key, block_hash):
        value = await self.rpc("state_getStorage", [key, block_hash])
        return None if value is None else bytes.fromhex(value[2:])

    async def best(self):
        block_hash = await self.rpc("chain_getBlockHash", [])
        header = await self.rpc("chain_getHeader", [block_hash])
        return int(header["number"], 16), block_hash

    async def finalized(self):
        block_hash = await self.rpc("chain_getFinalizedHead", [])
        header = await self.rpc("chain_getHeader", [block_hash])
        return int(header["number"], 16), block_hash

    async def nonce(self, account):
        """The account's next nonce, pool included, from the node: the SDK's cache would count our own
        signatures."""
        return int(await self.rpc("system_accountNextIndex", [ss58(account)]))

    async def owner(self, account, block_hash):
        raw = await self.storage(map_key("SubtensorModule", "Owner", account, "blake2_128_concat"), block_hash)
        return None if raw is None else raw[:32]

    async def hotkey_owner_precompile(self, account, block_number):
        data = "0x" + GET_HOTKEY_OWNER + account.hex()
        result = await self.rpc("eth_call", [{"to": STAKING_V2, "data": data}, hex(block_number)])
        raw = bytes.fromhex(result[2:])
        if len(raw) != 64:
            raise ValueError("getHotkeyOwner answered an unexpected length")
        return {"exists": raw[31] == 1, "owner": hex0x(raw[32:64])}

    async def self_coldkey(self, vault, block_number):
        result = await self.rpc("eth_call", [{"to": vault, "data": "0x" + SELF_COLDKEY}, hex(block_number)])
        raw = bytes.fromhex(result[2:])
        if len(raw) != 32:
            raise ValueError("selfColdkey() answered an unexpected length")
        return raw

    async def scan(self, pallet, item, block_hash):
        prefix = value_key(pallet, item)
        keys, start = [], prefix
        while True:
            page = await self.rpc("state_getKeysPaged", [prefix, 1000, start, block_hash])
            keys += page
            if len(page) < 1000:
                break
            start = page[-1]
        values = {}
        for i in range(0, len(keys), 200):
            for changes in await self.rpc("state_queryStorageAt", [keys[i : i + 200], block_hash]):
                for key, value in changes["changes"]:
                    if value is not None:
                        values[key] = bytes.fromhex(value[2:])
        return values

    async def u32_value(self, pallet, item):
        value = await self.substrate.query(pallet, item)
        return int(value)

    async def account(self, address):
        info = await self.substrate.query("System", "Account", [address])
        return int(info["data"]["free"]), int(info["data"]["frozen"])


async def reservation(chain, hotkey, block_number, block_hash):
    """The live reservation of `hotkey` as a coldkey swap's destination, if any (swap_coldkey.rs)."""
    raw = await chain.storage(
        map_key("SubtensorModule", "ColdkeySwapDestinationReservations", hotkey, "twox64_concat"), block_hash
    )
    if raw is None:
        return None
    announcer = raw[:32]
    announcement = await chain.storage(
        map_key("SubtensorModule", "ColdkeySwapAnnouncements", announcer, "twox64_concat"), block_hash
    )
    ttl = await chain.u32_value("SubtensorModule", "ColdkeySwapReannouncementDelay")
    if announcement is None:
        return {"announcer": hex0x(announcer), "live": False}
    when = int.from_bytes(announcement[:4], "little")
    return {"announcer": hex0x(announcer), "when": when, "until": when + ttl, "live": block_number <= when + ttl}


async def announcements_naming(chain, hotkey, block_hash):
    """Swap announcements whose commitment is `hotkey`: legacy hash or either v2 policy hash."""
    names = {
        legacy_swap_hash(hotkey): "legacy",
        v2_swap_hash(hotkey, True): "v2",
        v2_swap_hash(hotkey, False): "v2",
    }
    found = []
    for key, value in (await chain.scan("SubtensorModule", "ColdkeySwapAnnouncements", block_hash)).items():
        kind = names.get(value[4:36])
        if kind:
            found.append({"announcer": "0x" + key[-64:], "kind": kind, "when": int.from_bytes(value[:4], "little")})
    return found


async def hashed_account(chain, hotkey, block_hash):
    """The hashed-account kinds that would refuse a classical owner (runtime hashed_owner.rs)."""
    kinds = []
    for item in ("Accounts", "MlDsaAccounts", "DerivedAuthorities"):
        key = map_key("HashedAccounts", item, hotkey, "blake2_128_concat")
        if await chain.storage(key, block_hash) is not None:
            kinds.append(item)
    return kinds


# -- signers -------------------------------------------------------------------------------------------------

class PublicAccount:
    """An address for fee estimation only: the estimate signs with a zero signature."""

    def __init__(self, raw, crypto_type):
        self.public_key = raw
        self.ss58_address = ss58(raw)
        self.ss58_format = SS58_FORMAT
        self.crypto_type = crypto_type

    def sign(self, _data):
        raise RuntimeError("an estimation account cannot sign")


def ledger_signer(account, index):
    from bittensor.ledger import LedgerSigner

    return LedgerSigner(account=account, index=index)


def carrier_keypair(wallet, path):
    from bittensor import wallets

    return wallets.open_wallet(wallet, "default", path).coldkey


def carrier_address(wallet, path):
    from bittensor import wallets

    return account_bytes(wallets.open_wallet(wallet, "default", path).coldkeypub.ss58_address, "carrier")


# -- associate -----------------------------------------------------------------------------------------------

async def checks(chain, args, report):
    """Everything that must hold before signing. Raises Refused; returns True when already done."""
    hotkey, guardian = args.hotkey_bytes, args.guardian_bytes
    genesis = await chain.rpc("chain_getBlockHash", [0])
    version = await chain.rpc("state_getRuntimeVersion", [])
    best_number, best_hash = await chain.best()
    final_number, final_hash = await chain.finalized()
    report["chain"] = {
        "genesis": genesis,
        "spec_name": version["specName"],
        "spec_version": version["specVersion"],
        "best": best_number,
        "finalized": final_number,
    }
    if args.genesis and genesis != args.genesis:
        raise Refused(f"the endpoint's genesis is {genesis}, not {args.genesis}")
    if hotkey == guardian:
        raise Refused("the guardian cannot be the account it guards")
    if args.vault:
        try:
            self_coldkey = await chain.self_coldkey(args.vault, final_number)
        except Exception as error:
            raise Refused(f"vault {args.vault} does not answer selfColdkey(): {error}") from None
        report["vault"] = {"address": args.vault, "selfColdkey": hex0x(self_coldkey)}
        if self_coldkey != hotkey:
            raise Refused(f"vault {args.vault}'s selfColdkey() is {hex0x(self_coldkey)}, not --hotkey")

    # Who holds the account now, at the best and the finalized block.
    for label, number, block_hash in (("best", best_number, best_hash), ("finalized", final_number, final_hash)):
        owner = await chain.owner(hotkey, block_hash)
        report.setdefault("owner", {})[label] = None if owner is None else hex0x(owner)
        if owner is not None and owner != guardian:
            raise Refused(
                f"the account is already the hotkey of {hex0x(owner)} ({ss58(owner)}) at block {number}, not "
                "the guardian's: its holder can release it together with a coldkey swap into it. Stop and escalate."
            )
    report["getHotkeyOwner"] = await chain.hotkey_owner_precompile(hotkey, final_number)
    if report["owner"]["finalized"] is not None:
        report["status"] = "already the guardian's hotkey"
        return True
    if report["owner"]["best"] is not None:
        report["status"] = "the guardian's hotkey at the best block, not yet finalized"
        return True

    reserved = await reservation(chain, hotkey, best_number, best_hash)
    report["reservation"] = reserved
    if reserved and reserved["live"]:
        raise Refused(
            f"the account is reserved as a coldkey swap's destination by {reserved['announcer']} until block "
            f"{reserved['until']} (the swap may execute from block {reserved['when']}): the association is "
            "refused (ColdkeySwapDestinationReserved) while it lasts. Stop and escalate."
        )
    report["announcements"] = await announcements_naming(chain, hotkey, best_hash)
    if report["announcements"]:
        log("warning: coldkey swap announcements name this account; once it is a hotkey account their "
            "execution is refused (NewColdKeyIsHotkey)")
    kinds = await hashed_account(chain, hotkey, best_hash)
    if kinds:
        raise Refused(f"the account is a hashed account ({', '.join(kinds)}): a classical guardian cannot own it")

    # The guardian: an ordinary account that has announced no swap, with room on its list, that can pay.
    announcement = await chain.storage(
        map_key("SubtensorModule", "ColdkeySwapAnnouncements", guardian, "twox64_concat"), best_hash
    )
    if announcement is not None:
        raise Refused("the guardian has announced a coldkey swap; a guardian never swaps")
    staking = await chain.storage(map_key("SubtensorModule", "StakingHotkeys", guardian, "blake2_128_concat"), best_hash)
    listed = 0 if staking is None else compact_length(staking)
    if listed >= MAX_STAKING_HOTKEYS:
        raise Refused(f"the guardian's staking-hotkey list holds {listed} entries, the cap")
    existential = int(await chain.substrate.constant("Balances", "ExistentialDeposit"))
    call = await chain.substrate.compose(("SubtensorModule", "try_associate_hotkey", {"hotkey": ss58(hotkey)}))
    inner_fee = int((await chain.substrate.estimate_fee(call, PublicAccount(guardian, 0))).rao)
    free, frozen = await chain.account(ss58(guardian))
    nonce = await chain.nonce(guardian)
    report["guardian"].update(free_rao=free, nonce=nonce, staking_hotkeys=listed, inner_fee_rao=inner_fee)
    report["call"] = {
        "pallet": "SubtensorModule", "function": "try_associate_hotkey", "hotkey": ss58(hotkey),
        "hotkey_public_key": hex0x(hotkey), "call_data": hex0x(bytes(call.data)),
        "signer": ss58(guardian), "nonce": nonce, "era": args.era, "tip": 0,
    }
    if free - frozen < existential + 2 * inner_fee:
        raise Refused(
            f"the guardian holds {free} rao free; fund it with at least {existential + 2 * inner_fee - free + frozen} "
            "rao more (the existential deposit and the inner's fee, twice)"
        )

    # The carrier: a local key that pays submit_encrypted, sent at most once a block until the inner lands.
    key = await chain.substrate.mev_next_key()
    if not key:
        raise Refused("MevShield.NextKey is unset: the shield is not running on this chain")
    inner_size = 200
    dummy = bytes(SHIELD_OVERHEAD + inner_size)
    outer = await chain.substrate.compose(("MevShield", "submit_encrypted", {"ciphertext": "0x" + dummy.hex()}))
    if args.carrier_bytes is not None:
        carrier_fee = int((await chain.substrate.estimate_fee(outer, PublicAccount(args.carrier_bytes, 1))).rao)
        carrier_free, carrier_frozen = await chain.account(ss58(args.carrier_bytes))
        attempts = args.max_attempts
        report["carrier"] = {
            "address": ss58(args.carrier_bytes), "free_rao": carrier_free, "fee_rao": carrier_fee,
            "attempts_covered": attempts,
        }
        if carrier_free - carrier_frozen < existential + attempts * carrier_fee:
            raise Refused(
                f"the carrier holds {carrier_free} rao free; fund it with at least "
                f"{existential + attempts * carrier_fee - carrier_free + carrier_frozen} rao more "
                f"(the existential deposit and {attempts} carriers)"
            )
    report["shield"] = {"carrier_call_index": hex0x(bytes(outer.data)[:2]), "carrier_era": CARRIER_ERA,
                        "next_key_hash": hex0x(twox128(bytes(key))), "next_key_bytes": len(key)}
    return False


async def follow(chain, inner_hash, first_block):
    """The inner's receipt if it appears in the carrier's block or the next FOLLOW_BLOCKS."""
    for number in range(first_block, first_block + FOLLOW_BLOCKS + 1):
        deadline = time.monotonic() + 120
        while True:
            best, _ = await chain.best()
            if best >= number:
                break
            if time.monotonic() > deadline:
                return None
            await asyncio.sleep(max(0.05, await chain.substrate.block_time() / 4))
        block_hash = await chain.rpc("chain_getBlockHash", [number])
        found = await chain.substrate.find_extrinsic(inner_hash, block_hash)
        if found is not None:
            return found
    return None


async def wait_finalized(chain, number, timeout):
    deadline = time.monotonic() + timeout
    while True:
        final_number, final_hash = await chain.finalized()
        if final_number >= number:
            return final_number, final_hash
        if time.monotonic() > deadline:
            raise Unconfirmed(f"block {number} is not finalized after {timeout} s")
        await asyncio.sleep(max(0.1, await chain.substrate.block_time() / 2))


async def send(chain, args, report, signer, carrier, hooks):
    """Sign the inner once, then carry it under fresh keys until it lands or its era ends."""
    import bittensor_core

    hotkey, guardian = args.hotkey_bytes, args.guardian_bytes
    if bytes(signer.public_key) != guardian:
        raise Refused(f"the signer is {signer.ss58_address}, not the guardian {ss58(guardian)}")
    if bytes(carrier.public_key) == guardian:
        raise Refused("the carrier must be a key other than the guardian")
    call = await chain.substrate.compose(("SubtensorModule", "try_associate_hotkey", {"hotkey": ss58(hotkey)}))
    nonce = await chain.nonce(guardian)
    signed_at, _ = await chain.finalized()
    log(f"sign try_associate_hotkey({ss58(hotkey)}) as {ss58(guardian)}, nonce {nonce}, era {args.era} "
        "blocks: approve on the Ledger")
    try:
        inner, inner_hash = await chain.substrate.sign_extrinsic(call, signer, nonce=nonce, period=args.era)
    except Exception as error:  # a rejection on the device, a lock, a disconnect: nothing was sent
        raise Refused(f"the Ledger did not sign: {error}") from None
    inner = bytes(inner)
    # The era starts at the finalized head when signing began; leave the last two blocks unused.
    expires = signed_at + args.era - 2
    report["inner"] = {"hash": inner_hash, "bytes": len(inner), "nonce": nonce, "signed_at": signed_at,
                       "expires": expires}
    log(f"signed: inner {inner_hash}, {len(inner)} bytes, valid until about block {expires}")
    attempts = []
    report["attempts"] = attempts
    while True:
        best_number, best_hash = await chain.best()
        owner = await chain.owner(hotkey, best_hash)
        if owner is not None:
            if owner != guardian:
                raise Unconfirmed(f"the account became the hotkey of {hex0x(owner)} at block {best_number}: stop "
                                  "and escalate")
            break
        if await chain.nonce(guardian) != nonce:
            raise Unconfirmed("the guardian's nonce moved without the association: another transaction used it")
        if best_number >= expires:
            raise Unconfirmed(f"the signed inner expires at block {expires}: run associate --send again")
        if len(attempts) >= args.max_attempts:
            raise Unconfirmed(f"{len(attempts)} carriers sent without the inner landing")
        await hooks.before_encrypt(len(attempts), chain)
        key = await chain.substrate.mev_next_key()
        if not key:
            raise Unconfirmed("MevShield.NextKey is unset")
        ciphertext = bytes(bittensor_core.encrypt_mlkem768(bytes(key), inner, include_key_hash=True))
        await hooks.after_encrypt(len(attempts), chain)
        outer = await chain.substrate.compose(("MevShield", "submit_encrypted", {"ciphertext": "0x" + ciphertext.hex()}))
        attempt = {"key_hash": hex0x(twox128(bytes(key))), "best": best_number}
        attempts.append(attempt)
        log(f"carrier {len(attempts)}: key {attempt['key_hash'][:18]}…, best block {best_number}")
        result = await submit_carrier(chain, outer, carrier)
        included = block_of(result)
        attempt["carrier"] = {"extrinsic": result.extrinsic_id, "success": result.success, "block": included,
                              "message": result.message}
        if not result.success or included is None:
            log(f"carrier {len(attempts)} not included: {result.message}")
            await asyncio.sleep(await chain.substrate.block_time())
            continue
        found = await follow(chain, inner_hash, included)
        if found is None:
            log(f"carrier {len(attempts)} landed in block {included} without the inner; a fresh key next")
            continue
        attempt["inner"] = {"extrinsic": found.extrinsic_id, "block": block_of(found), "success": found.success,
                            "message": found.message}
        if not found.success:
            raise Unconfirmed(f"the inner landed as {found.extrinsic_id} and failed: {found.message}")
        log(f"the inner landed as extrinsic {found.extrinsic_id}")
        break
    landed = max([a["inner"]["block"] for a in attempts if "inner" in a] or [best_number])
    final_number, final_hash = await wait_finalized(chain, landed, args.finality_timeout)
    owner = await chain.owner(hotkey, final_hash)
    report["verified"] = {
        "finalized": final_number,
        "owner": None if owner is None else hex0x(owner),
        "getHotkeyOwner": await chain.hotkey_owner_precompile(hotkey, final_number),
    }
    if owner != guardian or report["verified"]["getHotkeyOwner"]["owner"] != hex0x(guardian):
        raise Unconfirmed("the finalized chain does not name the guardian as the account's owner")
    report["status"] = "associated"


async def submit_carrier(chain, outer, carrier):
    """Sign the carrier at the node's nonce, its short era anchored near the best block, and watch it in.

    Anchored at the finalized head (the SDK's default), an 8-block era can be over before it is sent when
    finality lags; two blocks below the best keeps it clear of a one-block reorganisation.
    """
    best, _ = await chain.best()
    final, _ = await chain.finalized()
    nonce = await chain.nonce(bytes(carrier.public_key))
    extrinsic = await chain.substrate.raw.create_signed_extrinsic(
        outer, carrier, era={"period": CARRIER_ERA, "current": max(final, best - 2)}, nonce=nonce
    )
    return await chain.substrate.submit_signed(extrinsic, carrier, wait_for_inclusion=True,
                                               wait_for_finalization=False)


def block_of(result):
    """The including block's number, from the result's "block-index" extrinsic id."""
    if not result.extrinsic_id:
        return None
    height, _, _ = result.extrinsic_id.partition("-")
    return int(height) if height.isdigit() else None


class Hooks:
    """Test seams around encryption; the command line uses these no-ops."""

    async def before_encrypt(self, attempt, chain):
        return None

    async def after_encrypt(self, attempt, chain):
        return None


async def associate(args, signer_factory=None, carrier_factory=None, hooks=None):
    from bittensor.client import Client

    report = {"schema": SCHEMA, "rpc": args.rpc, "send": args.send,
              "hotkey": {"address": ss58(args.hotkey_bytes), "public_key": hex0x(args.hotkey_bytes)},
              "guardian": {"address": ss58(args.guardian_bytes), "public_key": hex0x(args.guardian_bytes)}}
    status = 0
    client = Client(args.rpc, fallback_endpoints=[], archive_endpoints=[])
    await client.connect()
    try:
        chain = Chain(client)
        try:
            done = await checks(chain, args, report)
            if done:
                log(report["status"])
            elif not args.send:
                report["status"] = "dry run: nothing signed or sent"
                log("dry run: nothing signed or sent; --send signs on the Ledger and sends")
            else:
                carrier = (carrier_factory or (lambda: carrier_keypair(args.carrier_wallet, args.wallet_path)))()
                try:
                    signer = (signer_factory or (lambda: ledger_signer(args.ledger_account, args.ledger_index)))()
                except Exception as error:
                    raise Refused(f"no Ledger: {error}; unlock it, open the Polkadot app and run again") from None
                await send(chain, args, report, signer, carrier, hooks or Hooks())
                log(f"associated: {ss58(args.hotkey_bytes)} is the hotkey of {ss58(args.guardian_bytes)}")
        except Refused as error:
            report["status"] = "refused"
            report["reason"] = str(error)
            log(f"refused: {error}")
            status = 3
        except Unconfirmed as error:
            report["status"] = "unconfirmed"
            report["reason"] = str(error)
            log(f"unconfirmed: {error}")
            status = 4
    finally:
        await client.close()
    return status, report


def address(args):
    signer = ledger_signer(args.ledger_account, args.ledger_index)
    shown = signer.confirm_address() if args.confirm else signer.ss58_address
    result = {"schema": SCHEMA, "path": f"m/44'/354'/{args.ledger_account}'/0'/{args.ledger_index}'",
              "ss58Address": shown, "publicKey": hex0x(bytes(signer.public_key)), "cryptoType": 0}
    if shown != signer.ss58_address:
        raise SystemExit("the device showed another address")
    return result


def parse(argv):
    parser = argparse.ArgumentParser(prog="guardian_associate.py", description=__doc__.split("\n\n")[0])
    commands = parser.add_subparsers(dest="command", required=True)
    shown = commands.add_parser("address", help="print a Ledger account's address and public key")
    shown.add_argument("--ledger-account", type=int, required=True)
    shown.add_argument("--ledger-index", type=int, default=0)
    shown.add_argument("--confirm", action="store_true", help="also show the address on the device")
    run = commands.add_parser("associate", help="check, then print or (--send) sign and send the association")
    run.add_argument("--rpc", required=True, help="ws:// endpoint; the only one used")
    run.add_argument("--hotkey", required=True, help="the account to associate: 0x and 32 bytes, or ss58")
    run.add_argument("--guardian", required=True, help="the guardian coldkey: 0x and 32 bytes, or ss58")
    run.add_argument("--vault", help="vault2's address: its selfColdkey() must be --hotkey")
    run.add_argument("--genesis", help="the chain's genesis hash; refuses another chain")
    run.add_argument("--ledger-account", type=int, default=None)
    run.add_argument("--ledger-index", type=int, default=0)
    run.add_argument("--carrier-wallet", help="the wallet whose coldkey signs the carrier")
    run.add_argument("--wallet-path", default=None)
    run.add_argument("--era", type=int, default=64, help="the inner's mortal era in blocks (a power of two)")
    run.add_argument("--max-attempts", type=int, default=16, help="carriers to send at most (and to fund)")
    run.add_argument("--finality-timeout", type=float, default=180.0)
    run.add_argument("--send", action="store_true", help="sign on the Ledger and send; without it, a dry run")
    args = parser.parse_args(argv)
    if args.command == "associate":
        try:
            args.hotkey_bytes = account_bytes(args.hotkey, "--hotkey")
            args.guardian_bytes = account_bytes(args.guardian, "--guardian")
        except ValueError as error:
            parser.error(str(error))
        if args.vault and not (args.vault.startswith("0x") and len(args.vault) == 42):
            parser.error("--vault must be a 0x 20-byte address")
        if args.genesis and not (args.genesis.startswith("0x") and len(args.genesis) == 66):
            parser.error("--genesis must be a 0x 32-byte hash")
        if args.era < 16 or args.era > 4096 or args.era & (args.era - 1):
            parser.error("--era must be a power of two from 16 to 4096")
        if args.send and (args.ledger_account is None or not args.carrier_wallet):
            parser.error("--send needs --ledger-account and --carrier-wallet")
        args.carrier_bytes = None
        if args.carrier_wallet:
            from bittensor.wallet import DEFAULT_WALLET_PATH

            args.wallet_path = os.path.expanduser(args.wallet_path or DEFAULT_WALLET_PATH)
            args.carrier_bytes = carrier_address(args.carrier_wallet, args.wallet_path)
    return args


def main(argv=None):
    args = parse(sys.argv[1:] if argv is None else argv)
    if args.command == "address":
        sys.stdout.write(json.dumps(address(args)) + "\n")
        return 0
    status, report = asyncio.run(associate(args))
    sys.stdout.write(json.dumps(report, indent=2) + "\n")
    return status


if __name__ == "__main__":
    sys.exit(main())
