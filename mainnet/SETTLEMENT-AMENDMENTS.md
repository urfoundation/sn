# Signing a settlement amendment

How the owner fixes when an amendment to settlement starts, on the sn side. Two amendments start at epoch 8:

- the extender pool, whose first epoch is `E_x` ([WHITEPAPER.md](../WHITEPAPER.md) §8.6.7);
- capture the flag, whose first epoch `E_c` and split ([CAPTURETHEFLAG.md](../CAPTURETHEFLAG.md) §2.4).

Each is fixed by one line of plain text, signed with the price sheet's pinned key: the sole owner of the governance
Safe, on its Ledger. The signed lines go in [`amendments.yml`](../amendments.yml), beside `price.yml`, which the site
publishes at <https://ur.xyz/amendments.yml>. The server, the validator and `snclaim` read it through the
`amendment` package.

Both entries for epoch 8 are due during epoch 7, before epoch 8 starts at block 9,600,028 (about 2026-11-27 18:29Z).
The tool refuses an entry whose first epoch has started, so a missed deadline moves the start to epoch 9.

## 1. Before signing

- **The carry contract's address,** for capture the flag's line. The contract is created after the timelock
  operation, with a private random salt, so the line is signed once it exists ([CAPTURETHEFLAG.md](../CAPTURETHEFLAG.md)
  §11.2). Take the address from `sn-mainnet ctf-carry review` after the creation, which checks the code, the
  residual and the opt-outs at it ([BOOTSTRAP-CONTRACTS.md](BOOTSTRAP-CONTRACTS.md), "Capture the flag carry").
  Write it in its EIP-55 checksum form, as `cast to-check-sum-address` prints it. The tool refuses any other
  form, so a mistyped address fails before anyone signs it.
- **The readers' releases.** The server and every validator must run a build that reads amendments before epoch
  8.
- **The site.** It must serve `amendments.yml` before any reader depends on it. A reader with a pinned signer that
  has never verified the file treats the amendments in force as unknown, and configuration does not stand in for
  that.

## 2. Sign each line

The lines for epoch 8:

```text
UR settlement amendment v1: chain 964, sn 25, no 1, from epoch 8, intra-platform and intra-network extenders, units egress prefix, free and paid, zeta 1
UR settlement amendment v2: chain 964, sn 25, no 1, from epoch 8, capture the flag, ctf pool 4294967297, carry <the carry contract's address>, shares pool 4/10 ctf 1/10 head 5/10, head cedes to pool, slots 5 providers 5 extenders, countries iso 3166-1 249, presence half epoch, units egress prefix, draw drand quicknet 60s after census
```

Only the chain, sn, operator, first epoch and carry contract can vary. Every other term is fixed by the owner's
decisions, and the tool refuses a line that differs from them in any character.

For each line, in this repository:

1. Print what to sign. The owner supplies the signing key's derivation path:

   ```bash
   go run ./cli/amendment message --in=amendments.yml --rpc="$RPC" \
     --ledger-path="<the owner key's derivation path>" --line="<the line>"
   ```

   `$RPC` is an EVM RPC endpoint for chain 964. The tool reads the current epoch from the coordinator and refuses
   in these cases:
   - a node whose latest block is more than five minutes old;
   - a first epoch that has started;
   - an entry that would come before one already in the file.

   It also says how many blocks remain before the first epoch starts.
2. Run the `cast wallet sign --ledger` command it prints. Approve only if the device shows exactly the printed
   line.
3. Attach the signature:

   ```bash
   go run ./cli/amendment attach --in=amendments.yml --rpc="$RPC" --signature=0x... --line="<the line>"
   ```

   Attach checks the epoch again. It accepts only the pinned signer's signature over exactly that line, and it
   writes nothing unless the whole result verifies.

`--current-epoch=N` can stand in for `--rpc` where no RPC is at hand. The tool then trusts N.

## 3. Verify and publish

```bash
go run ./cli/amendment verify --in=amendments.yml --rpc="$RPC"
```

During epoch 7 it reports, for no 1, that neither amendment applies yet and that each entry from epoch 8 is next.

Merge `amendments.yml` to `main`. The site serves its own copy, refreshed from this file outside this repository,
as it does for `price.yml`. Then, before epoch 8 starts:

```bash
go run ./cli/amendment verify --url=https://ur.xyz/amendments.yml --rpc="$RPC"
```

## If the signing cannot happen in time

Configuration is the fallback (WHITEPAPER.md §8.6.7).

- **Configure the line.** The server's and the validator's configuration name the same line, unsigned, identically
  for every reader. A configured line stands in only for epochs where no signed entry of its kind is in force.
- **Sign later.** Sign the entry from the next epoch that has not started. From then on the signed entry applies.
- **Remove the configured line** once the signed entry covers its epochs. A line left in place still applies
  before a later signed start, so postponing a start also means removing the line from every reader.

## A signed entry is permanent

Readers refuse a file that drops or changes an entry, or adds one that takes effect before an entry they hold. So
the file lists its entries in the order they take effect, and only a later entry of the same kind can correct a
mistake, from its own first epoch.

`sign --key-file=PATH` signs with a key held in a file, readable only by its owner, and attaches the entry. It
refuses any key but the pinned signer's, so it is not the owner's path: the owner's key is on its Ledger.
