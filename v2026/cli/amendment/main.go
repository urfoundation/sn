// Command amendment is the owner's tool for the signed settlement amendments at
// https://ur.xyz/amendments.yml (WHITEPAPER.md §8.6.7, CAPTURETHEFLAG.md §2.4). It
// works as the price sheet's tool does. message prints the exact line to approve and
// the command that signs it on the Ledger, attach adds a signed entry after the last
// one once its signer checks out, and verify reads a file or the published one as the
// server, the validator and snclaim do. sign signs a line with a key held in a file,
// rather than on the Ledger, and attaches it.
//
// An amendment is signed before its first epoch starts, so message, sign and attach
// refuse a line whose first epoch has started. The caller gives the current epoch,
// or an RPC to read it from.
//
//	amendment message --in=amendments.yml --line=LINE (--current-epoch=N | --rpc=URL) [--ledger-path=PATH]
//	amendment attach --in=amendments.yml --line=LINE --signature=0x... (--current-epoch=N | --rpc=URL)
//	amendment sign --in=amendments.yml --line=LINE --key-file=PATH (--current-epoch=N | --rpc=URL)
//	amendment verify --in=FILE | --url=URL [--chain=964] [--current-epoch=N | --rpc=URL]
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/amendment"
	"github.com/urfoundation/sn/v2026/evmrpc"
	"github.com/urfoundation/sn/v2026/miner/onchain"
	"github.com/urfoundation/sn/v2026/stabi"
)

const commandUsage = `amendment publishes the signed settlement amendments. The owner's key signs on its
Ledger: message prints what to sign, and attach adds the signature.

  amendment message --in=amendments.yml --line=LINE (--current-epoch=N | --rpc=URL) [--ledger-path=PATH]
      Checks the line and prints it with the cast command that signs it on the Ledger.
      PATH is the owner key's derivation path, which this tool does not hold; without
      it the printed command has a blank to fill in. It refuses what attach would
      refuse, so nobody signs it: a line that is not an amendment, one for a
      deployment with no pinned signer, one at or before the newest entry of its kind
      for its operator, and one whose first epoch has started.

  amendment attach --in=amendments.yml --line=LINE --signature=0x... (--current-epoch=N | --rpc=URL)
      Adds the signed entry after the last one in signed_amendments, leaving every
      other byte of the file as it is. It refuses what message refuses and a signature
      from anyone but the pinned signer, and writes only after verifying the whole
      result.

  amendment sign --in=amendments.yml --line=LINE --key-file=PATH (--current-epoch=N | --rpc=URL)
      Signs the line with the hex secp256k1 key in PATH, which must be the pinned
      signer's, and attaches it as attach does. It is for a key held in a file. The
      owner's key is on its Ledger and signs through message and attach.

  amendment verify --in=FILE | --url=URL [--chain=964] [--current-epoch=N | --rpc=URL]
      Parses strictly and verifies every entry. With the current epoch it says which
      entries apply now.

LINE is the amendment's exact line, quoted for the shell. Version 2, for example:

  UR settlement amendment v2: chain 964, sn 25, no 1, from epoch 8, capture the flag, ctf pool 4294967297, carry 0x..., shares pool 4/10 ctf 1/10 head 5/10, head cedes to pool, slots 5 providers 5 extenders, countries iso 3166-1 249, presence half epoch, units egress prefix, draw drand quicknet 60s after census

N is the current epoch. URL is an EVM RPC endpoint for chain 964, from which the
current epoch is read. An amendment is signed before its first epoch starts, so the
earliest first epoch is N + 1.
`

const (
	defaultChain = "964"
	// ledgerPathBlank stands in for the owner key's derivation path, which stays out of
	// this code. cast cannot read it as a path, so the command fails until it is filled.
	ledgerPathBlank = "<ledger-path>"
	readTimeout     = 30 * time.Second
	// amendment.ParseFile refuses a file past this size; reading one byte more lets it.
	maxFileBytes = 256 << 10
	// placeRefusal says why an entry cannot go where it would.
	placeRefusal = "a new entry takes effect no earlier than every entry in the file, and later than every one of its kind for its operator"
	// maxHeadAge is how old an RPC's latest block may be. A node further behind the
	// chain could report an epoch that has already ended.
	maxHeadAge = 5 * time.Minute
	// blockSeconds is the chain's measured block time, for saying how long is left.
	blockSeconds = 12
)

// mainnetCoordinator counts the epochs of the only deployment with a pinned signer.
var mainnetCoordinator = common.HexToAddress("0xC18925925E2B7bb9059b7d696b8c92762AE86406")

// coordinatorFor is the coordinator that counts a deployment's epochs.
func coordinatorFor(chainID uint64, netuid uint16) (common.Address, bool) {
	if chainID == 964 && netuid == 25 {
		return mainnetCoordinator, true
	}
	return common.Address{}, false
}

var (
	wholeText      = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	ledgerPathText = regexp.MustCompile(`^m(/[0-9]+'?)+$`)
	// A signature exactly as cast prints it.
	signatureText = regexp.MustCompile(`^0x[0-9a-fA-F]{130}$`)
)

// dependencies are what a test replaces: the signer here is always the pinned one.
type dependencies struct {
	signerFor func(chainID uint64, netuid uint16) (common.Address, bool)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, dependencies{signerFor: amendment.SignerFor}); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "amendment:", strings.ReplaceAll(err.Error(), "amendment: ", ""))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, deps dependencies) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(stdout, commandUsage)
		return err
	}
	command := args[0]
	if command != "message" && command != "attach" && command != "sign" && command != "verify" {
		return fmt.Errorf("unknown command %q; use --help", command)
	}
	opts, err := parseOptions(command, args[1:])
	if errors.Is(err, flag.ErrHelp) {
		_, err := io.WriteString(stdout, commandUsage)
		return err
	}
	if err != nil {
		return err
	}
	switch command {
	case "message":
		return runMessage(ctx, opts, stdout, deps)
	case "attach":
		return runAttach(ctx, opts, stdout, deps)
	case "sign":
		return runSign(ctx, opts, stdout, deps)
	default:
		return runVerify(ctx, opts, stdout, deps)
	}
}

type options struct {
	in, url, line, chain, signature, rpc, currentEpoch, ledgerPath, keyFile string
}

// parseOptions takes every number as text. The flag package's own integers read 010
// as 8 and 0x10 as 16, which is no way to name an epoch.
func parseOptions(command string, args []string) (*options, error) {
	opts := &options{}
	flags := flag.NewFlagSet("amendment "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.in, "in", "", "the amendments file")
	flags.StringVar(&opts.currentEpoch, "current-epoch", "", "the current epoch")
	flags.StringVar(&opts.rpc, "rpc", "", "a read-only RPC to read the current epoch from")
	if command != "verify" {
		flags.StringVar(&opts.line, "line", "", "the amendment's exact line")
	}
	switch command {
	case "message":
		flags.StringVar(&opts.ledgerPath, "ledger-path", "", "the owner key's derivation path, for the printed cast command")
	case "attach":
		flags.StringVar(&opts.signature, "signature", "", "the signature cast printed")
	case "sign":
		flags.StringVar(&opts.keyFile, "key-file", "", "a file holding the signer's hex secp256k1 key")
	case "verify":
		flags.StringVar(&opts.url, "url", "", "fetch the file from this address instead of --in")
		flags.StringVar(&opts.chain, "chain", defaultChain, "the EVM chain id")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %w; use --help", command, err)
	}
	if flags.NArg() != 0 {
		return nil, fmt.Errorf("%s takes no positional arguments, got %q; use --help", command, flags.Arg(0))
	}
	return opts, nil
}

func parseWhole(name, text string) (uint64, error) {
	if !wholeText.MatchString(text) {
		return 0, fmt.Errorf("%s must be a plain whole number, got %q", name, text)
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is out of range: %q", name, text)
	}
	return value, nil
}

// epochNow is the current epoch the caller gave, or read from the RPC. said says where
// it came from, and startsAt, for an RPC and a first epoch from, when that epoch
// starts. Nil means neither was given.
func epochNow(ctx context.Context, opts *options, chainID uint64, netuid uint16, from *uint64) (current *uint64, said, startsAt string, err error) {
	switch {
	case opts.currentEpoch != "" && opts.rpc != "":
		return nil, "", "", errors.New("give --current-epoch or --rpc, not both")
	case opts.currentEpoch != "":
		epoch, err := parseWhole("--current-epoch", opts.currentEpoch)
		if err != nil {
			return nil, "", "", err
		}
		return &epoch, fmt.Sprintf("The current epoch, as given, is %d.", epoch), "", nil
	case opts.rpc != "":
		coordinator, ok := coordinatorFor(chainID, netuid)
		if !ok {
			return nil, "", "", fmt.Errorf("no coordinator is known for chain %d sn %d to read the epoch from; give --current-epoch", chainID, netuid)
		}
		reading, err := readChain(ctx, opts.rpc, chainID, coordinator, from, time.Now())
		if err != nil {
			return nil, "", "", err
		}
		said = fmt.Sprintf("The current epoch, read from the chain at block %d, is %d.", reading.head, reading.epoch)
		if from != nil && reading.startsAt > reading.head {
			blocks := reading.startsAt - reading.head
			startsAt = fmt.Sprintf("it starts at block %d, %d blocks from now, about %s at %d s a block", reading.startsAt, blocks, roughly(blocks*blockSeconds), blockSeconds)
		}
		return &reading.epoch, said, startsAt, nil
	}
	return nil, "", "", nil
}

// roughly says a span of seconds in minutes, hours or days.
func roughly(seconds uint64) string {
	switch {
	case seconds < 2*3600:
		return fmt.Sprintf("%d minutes", (seconds+59)/60)
	case seconds < 2*86400:
		return fmt.Sprintf("%.1f hours", float64(seconds)/3600)
	}
	return fmt.Sprintf("%.1f days", float64(seconds)/86400)
}

// chainReading is what the coordinator says at the RPC's latest block.
type chainReading struct {
	head, epoch uint64
	// startsAt is the first block of the epoch asked about, if any.
	startsAt uint64
}

// readChain reads the current epoch from the coordinator at the RPC's latest block,
// and the first block of epoch from, if given. It refuses a node whose latest block is
// older than maxHeadAge, since one behind the chain would report an epoch that has
// already ended.
func readChain(ctx context.Context, endpoint string, chainID uint64, coordinatorAddress common.Address, from *uint64, now time.Time) (*chainReading, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	client, err := evmrpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("the RPC at %s: %w", endpoint, err)
	}
	defer client.Close()
	reported, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the chain id from %s: %w", endpoint, err)
	}
	if !reported.IsUint64() || reported.Uint64() != chainID {
		return nil, fmt.Errorf("the RPC at %s is on chain %s, not %d", endpoint, reported, chainID)
	}
	var head struct {
		Number    hexutil.Uint64 `json:"number"`
		Timestamp hexutil.Uint64 `json:"timestamp"`
	}
	if err := client.Client().CallContext(ctx, &head, "eth_getBlockByNumber", "latest", false); err != nil {
		return nil, fmt.Errorf("read the latest block from %s: %w", endpoint, err)
	}
	if age := now.Sub(time.Unix(int64(head.Timestamp), 0)); age > maxHeadAge {
		return nil, fmt.Errorf("the RPC at %s is behind: its latest block, %d, is %s old, so the epoch it reports may have ended; use a node at the chain's head", endpoint, uint64(head.Number), age.Round(time.Second))
	}
	block := new(big.Int).SetUint64(uint64(head.Number))
	coordinator := stabi.NewSTCoordinator()
	read := func(name string, data []byte, unpack func([]byte) (*big.Int, error)) (uint64, error) {
		output, err := client.CallContract(ctx, ethereum.CallMsg{To: &coordinatorAddress, Data: data}, block)
		if err != nil {
			return 0, fmt.Errorf("read %s from %s: %w", name, endpoint, err)
		}
		value, err := unpack(output)
		if err != nil {
			return 0, fmt.Errorf("read %s from %s: %w", name, endpoint, err)
		}
		if !value.IsUint64() {
			return 0, fmt.Errorf("%s from %s is out of range: %s", name, endpoint, value)
		}
		return value.Uint64(), nil
	}
	reading := &chainReading{head: uint64(head.Number)}
	if reading.epoch, err = read("currentEpoch()", coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch); err != nil {
		return nil, err
	}
	if from != nil {
		if reading.startsAt, err = read("epochStartBlock()", coordinator.PackEpochStartBlock(new(big.Int).SetUint64(*from)), coordinator.UnpackEpochStartBlock); err != nil {
			return nil, err
		}
	}
	return reading, nil
}

// parseStrict is amendment.ParseFile and one thing more. ParseFile passes over
// top-level fields it does not use, which suits a reader but not the tool publishing
// the file: a misspelt signed_amendments would read as no amendments at all.
func parseStrict(raw []byte) (*amendment.File, error) {
	file, err := amendment.ParseFile(raw)
	if err != nil {
		return nil, err
	}
	root, err := documentRoot(raw)
	if err != nil {
		return nil, err
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		switch {
		case key.Kind != yaml.ScalarNode:
			return nil, fmt.Errorf("line %d: unexpected top-level field", key.Line)
		case key.Value != "sn" && key.Value != "signed_amendments":
			return nil, fmt.Errorf("line %d: unexpected top-level field %q", key.Line, key.Value)
		}
	}
	return file, nil
}

func documentRoot(raw []byte) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("not YAML: %w", err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("not a YAML document")
	}
	return document.Content[0], nil
}

func pinnedSigner(deps dependencies, chainID uint64, netuid uint16) (common.Address, error) {
	signer, ok := deps.signerFor(chainID, netuid)
	if !ok {
		return common.Address{}, fmt.Errorf("chain %d sn %d has no pinned signer, so it has no published amendments", chainID, netuid)
	}
	return signer, nil
}

// draft is an amendment on its way into a file, signed or not.
type draft struct {
	in     string
	raw    []byte
	file   *amendment.File
	signer common.Address
	entry  amendment.Entry
	line   string
	// now says what the current epoch is and where it came from.
	now string
}

// loadDraft checks the line against the file and the chain before anyone signs it. It
// refuses a first epoch that has started: an amendment is signed before its first
// epoch starts, so that no reader has decided an epoch under other rules.
func loadDraft(ctx context.Context, opts *options, deps dependencies) (*draft, error) {
	if opts.in == "" {
		return nil, errors.New("--in is required")
	}
	if opts.line == "" {
		return nil, errors.New("--line is required: the amendment's exact line")
	}
	entry, err := amendment.ParseLine(opts.line)
	if err != nil {
		return nil, err
	}
	signer, err := pinnedSigner(deps, entry.ChainID, entry.Netuid)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(opts.in)
	if err != nil {
		return nil, err
	}
	file, err := parseStrict(raw)
	if err != nil {
		return nil, err
	}
	if file.Netuid() != entry.Netuid {
		return nil, fmt.Errorf("the line is for sn %d and %s for sn %d", entry.Netuid, opts.in, file.Netuid())
	}
	if err := file.Verify(entry.ChainID, entry.Netuid, signer); err != nil {
		return nil, fmt.Errorf("an entry already in %s does not verify: %w", opts.in, err)
	}
	d := &draft{in: opts.in, raw: raw, file: file, signer: signer, entry: *entry, line: opts.line}
	if err := d.checkPlace(); err != nil {
		return nil, err
	}
	current, said, startsAt, err := epochNow(ctx, opts, entry.ChainID, entry.Netuid, &entry.FromEpoch)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, errors.New("give the current epoch, --current-epoch=N or --rpc=URL: an amendment whose first epoch has started is refused")
	}
	if entry.FromEpoch <= *current {
		earliest := fmt.Sprintf("so the earliest is %d", *current+1)
		if *current == math.MaxUint64 {
			earliest = "and no later epoch exists"
		}
		return nil, fmt.Errorf("epoch %d has started: the current epoch is %d, and an amendment is signed before its first epoch starts, %s", entry.FromEpoch, *current, earliest)
	}
	d.now = fmt.Sprintf("%s Epoch %d has not started.", said, entry.FromEpoch)
	if startsAt != "" {
		d.now = fmt.Sprintf("%s Epoch %d has not started: %s. Publish the entry before then.", said, entry.FromEpoch, startsAt)
	}
	return d, nil
}

// checkPlace refuses, before anyone signs, an entry the file could not take. A file
// lists its entries in the order they take effect, so a new entry takes effect no
// earlier than any entry in it, and later than every one of its kind for its operator.
// One placed earlier could change epochs readers have already decided, so they refuse
// that file, and one at an existing epoch of its kind would change a signed entry,
// which never changes.
func (d *draft) checkPlace() error {
	var newest, ofKind *amendment.Entry
	for _, entry := range d.file.Entries() {
		if newest == nil || entry.FromEpoch >= newest.FromEpoch {
			newest = &entry
		}
		if entry.Kind() == d.entry.Kind() && entry.NoID == d.entry.NoID {
			ofKind = &entry
		}
	}
	switch {
	case ofKind != nil && d.entry.FromEpoch <= ofKind.FromEpoch:
		return fmt.Errorf("epoch %d is not after the newest entry of %s for no %d, from epoch %d: %s, because readers refuse a file that could change epochs they have already decided, and a signed entry never changes", d.entry.FromEpoch, d.entry.Kind(), d.entry.NoID, ofKind.FromEpoch, placeRefusal)
	case newest != nil && d.entry.FromEpoch < newest.FromEpoch:
		return fmt.Errorf("epoch %d is before the newest entry in the file, from epoch %d: %s, because readers refuse a file that could change epochs they have already decided", d.entry.FromEpoch, newest.FromEpoch, placeRefusal)
	}
	return nil
}

func runMessage(ctx context.Context, opts *options, stdout io.Writer, deps dependencies) error {
	ledgerPath, ledgerNote := opts.ledgerPath, ""
	if ledgerPath == "" {
		ledgerPath = ledgerPathBlank
		ledgerNote = "\nPut the owner key's derivation path in place of " + ledgerPathBlank + ", or pass --ledger-path.\n"
	} else if !ledgerPathText.MatchString(ledgerPath) {
		return fmt.Errorf("--ledger-path must look like m/44'/60'/0'/0/0, got %q", ledgerPath)
	}
	d, err := loadDraft(ctx, opts, deps)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, `The line to sign:

%[1]s

%[2]s

Sign it with the Ledger's Ethereum app open. The device must show exactly this line,
and the key is %[3]s:

cast wallet sign --ledger --mnemonic-derivation-path "%[4]s" "%[1]s"
%[6]s
Then attach the signature it prints, giving the current epoch again (or --rpc), since
attach also refuses an amendment whose first epoch has started:

go run ./cli/amendment attach --in=%[5]s --current-epoch=N --signature=0x... --line="%[1]s"
`, d.line, d.now, d.signer, ledgerPath, opts.in, ledgerNote)
	return err
}

func runAttach(ctx context.Context, opts *options, stdout io.Writer, deps dependencies) error {
	if !signatureText.MatchString(opts.signature) {
		return errors.New("--signature must be 0x and 65 bytes of hex, as cast prints it")
	}
	d, err := loadDraft(ctx, opts, deps)
	if err != nil {
		return err
	}
	signature, err := hex.DecodeString(opts.signature[2:])
	if err != nil {
		return fmt.Errorf("--signature: %w", err)
	}
	d.entry.Signature = lowS(signature)
	if err := d.entry.Verify(d.entry.ChainID, d.entry.Netuid, d.signer); err != nil {
		return fmt.Errorf("%w\nThe signature must be the pinned signer's, over exactly this line:\n%s", err, d.line)
	}
	return d.attach(stdout, "Attached")
}

// lowS is the signature with s in the lower half of the curve order, the one form
// readers take. A signature with s in the upper half is the same signature by the same
// key, so attach stores its lower twin: a signer whose device returned the upper one
// is not left with a line it could never publish, since a deterministic signer signs a
// line the same way every time.
func lowS(signature []byte) []byte {
	out := bytes.Clone(signature)
	if len(out) != crypto.SignatureLength || (out[crypto.RecoveryIDOffset] != 27 && out[crypto.RecoveryIDOffset] != 28) {
		return out
	}
	order := crypto.S256().Params().N
	s := new(big.Int).SetBytes(out[32:64])
	if s.Cmp(new(big.Int).Rsh(order, 1)) <= 0 {
		return out
	}
	copy(out[32:64], common.LeftPadBytes(new(big.Int).Sub(order, s).Bytes(), 32))
	out[crypto.RecoveryIDOffset] = 27 + 28 - out[crypto.RecoveryIDOffset]
	return out
}

func runSign(ctx context.Context, opts *options, stdout io.Writer, deps dependencies) error {
	if opts.keyFile == "" {
		return errors.New("--key-file is required: a file holding the signer's hex secp256k1 key")
	}
	d, err := loadDraft(ctx, opts, deps)
	if err != nil {
		return err
	}
	info, err := os.Stat(opts.keyFile)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s can be read by others (mode %04o): keep a key file to its owner, with chmod 600", opts.keyFile, info.Mode().Perm())
	}
	key, err := onchain.LoadKeyFile(opts.keyFile)
	if err != nil {
		return err
	}
	if address := crypto.PubkeyToAddress(key.PublicKey); address != d.signer {
		return fmt.Errorf("the key in %s is %s, not the pinned signer %s", opts.keyFile, address, d.signer)
	}
	if err := d.entry.Sign(key); err != nil {
		return err
	}
	if err := d.entry.Verify(d.entry.ChainID, d.entry.Netuid, d.signer); err != nil {
		return err
	}
	return d.attach(stdout, "Signed and attached")
}

// attach writes the signed entry into the file after the last one, once the result
// reads back as the same file with this one entry more.
func (d *draft) attach(stdout io.Writer, done string) error {
	next, err := appendEntry(d.raw, d.line, d.entry.Signature)
	if err != nil {
		return err
	}
	if err := d.checkResult(next); err != nil {
		return fmt.Errorf("nothing written: %w", err)
	}
	// The chain was read since the file was, so check nobody changed it meanwhile.
	if now, err := os.ReadFile(d.in); err != nil || !bytes.Equal(now, d.raw) {
		return fmt.Errorf("nothing written: %s changed while this ran; run it again", d.in)
	}
	if err := writeFileAtomic(d.in, next); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s to %s, signed by %s:\n\n%s\n\nsignature 0x%s\n\nCheck the whole file before publishing it:\n\ngo run ./cli/amendment verify --in=%s --rpc=URL\n", done, d.in, d.signer, d.line, hex.EncodeToString(d.entry.Signature), d.in)
	return err
}

// checkResult reads the bytes about to be written as any reader would and holds them
// to what attach promises: the same file with this one entry more.
func (d *draft) checkResult(next []byte) error {
	file, err := parseStrict(next)
	if err != nil {
		return err
	}
	if err := file.Verify(d.entry.ChainID, d.entry.Netuid, d.signer); err != nil {
		return err
	}
	entries := file.Entries()
	if len(entries) != len(d.file.Entries())+1 || !amendment.Extends(file, d.file) {
		return errors.New("the result does not keep every earlier entry unchanged")
	}
	added := &entries[len(entries)-1]
	line, err := added.Line()
	if err != nil {
		return err
	}
	if line != d.line || !bytes.Equal(added.Signature, d.entry.Signature) {
		return fmt.Errorf("the result's last entry is not this line with this signature:\n  signed: %s\n  result: %s", d.line, line)
	}
	return nil
}

// appendEntry adds the entry's lines right after the last entry and touches nothing
// else, so comments and earlier entries stay byte for byte. A layout it cannot add
// lines to is refused; whatever it does write is read back by checkResult before it
// reaches the disk.
func appendEntry(raw []byte, line string, signature []byte) ([]byte, error) {
	if bytes.IndexByte(raw, '\r') >= 0 {
		return nil, errors.New("the file has carriage returns; use plain newlines")
	}
	root, err := documentRoot(raw)
	if err != nil {
		return nil, err
	}
	var key, list *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "signed_amendments" {
			key, list = root.Content[i], root.Content[i+1]
		}
	}
	if key == nil {
		return splice(raw, len(raw), "signed_amendments:\n"+renderEntry(line, signature, 2)), nil
	}
	if list.Kind != yaml.SequenceNode || list.Style&yaml.FlowStyle != 0 || len(list.Content) == 0 {
		return nil, fmt.Errorf("line %d: signed_amendments must be a block list for attach to add to; remove it if it is empty", key.Line)
	}
	after := lastLine(list.Content[len(list.Content)-1])
	// A block list's column is its first dash's.
	return splice(raw, offsetAfterLine(raw, after), renderEntry(line, signature, list.Column-1)), nil
}

func lastLine(node *yaml.Node) int {
	last := node.Line
	for _, child := range node.Content {
		last = max(last, lastLine(child))
	}
	return last
}

func offsetAfterLine(raw []byte, line int) int {
	offset := 0
	for range line {
		next := bytes.IndexByte(raw[offset:], '\n')
		if next < 0 {
			return len(raw)
		}
		offset += next + 1
	}
	return offset
}

func splice(raw []byte, at int, block string) []byte {
	out := make([]byte, 0, len(raw)+len(block)+1)
	out = append(out, raw[:at]...)
	if at > 0 && raw[at-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, block...)
	return append(out, raw[at:]...)
}

// renderEntry writes an entry as readers require it: the line in double quotes,
// exactly as signed, and the signature in lowercase hex. A valid line holds no quote
// or backslash, so it needs no escape.
func renderEntry(line string, signature []byte, indent int) string {
	pad := strings.Repeat(" ", indent)
	return pad + `- line: "` + line + "\"\n" + pad + `  signature: "0x` + hex.EncodeToString(signature) + "\"\n"
}

// writeFileAtomic replaces the file whole, so a failure leaves the old one intact.
func writeFileAtomic(path string, data []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), target)
}

func fetchFile(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/yaml, text/plain")
	request.Header.Set("Cache-Control", "no-cache")
	// Readers do not follow a redirect off the published address, so this does not.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered HTTP %d", url, response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxFileBytes+1))
}

func runVerify(ctx context.Context, opts *options, stdout io.Writer, deps dependencies) error {
	if (opts.in == "") == (opts.url == "") {
		return errors.New("verify takes exactly one of --in and --url")
	}
	chainID, err := parseWhole("--chain", opts.chain)
	if err != nil {
		return err
	}
	source := opts.in
	var raw []byte
	if opts.url != "" {
		source = opts.url
		raw, err = fetchFile(ctx, opts.url)
	} else {
		raw, err = os.ReadFile(opts.in)
	}
	if err != nil {
		return err
	}
	file, err := parseStrict(raw)
	if err != nil {
		return err
	}
	signer, err := pinnedSigner(deps, chainID, file.Netuid())
	if err != nil {
		return err
	}
	if err := file.Verify(chainID, file.Netuid(), signer); err != nil {
		return err
	}
	current, _, _, err := epochNow(ctx, opts, chainID, file.Netuid(), nil)
	if err != nil {
		return err
	}
	return report(stdout, source, file, chainID, signer, current)
}

func report(stdout io.Writer, source string, file *amendment.File, chainID uint64, signer common.Address, current *uint64) error {
	var out strings.Builder
	entries := file.Entries()
	fmt.Fprintf(&out, "%s: chain %d, sn %d, signer %s\n", source, chainID, file.Netuid(), signer)
	if len(entries) == 0 {
		fmt.Fprintln(&out, "No signed entries: no signed amendment is in force.")
		_, err := io.WriteString(stdout, out.String())
		return err
	}
	fmt.Fprintf(&out, "%d signed, each verified:\n", len(entries))
	for i := range entries {
		line, err := entries[i].Line()
		if err != nil {
			return err
		}
		fmt.Fprintf(&out, "  %s\n", line)
	}
	if current == nil {
		out.WriteString("Pass --current-epoch or --rpc to see which entries apply now.\n")
	} else {
		statement, err := nowStatement(file, *current)
		if err != nil {
			return err
		}
		out.WriteString(statement)
	}
	_, err := io.WriteString(stdout, out.String())
	return err
}

// nowStatement says, for each kind and operator the file names, which entry applies at
// the current epoch and which comes next.
func nowStatement(file *amendment.File, current uint64) (string, error) {
	type series struct {
		kind amendment.Kind
		noID uint64
	}
	entries := file.Entries()
	var order []series
	seen := map[series]bool{}
	for i := range entries {
		key := series{entries[i].Kind(), entries[i].NoID}
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Current epoch %d:\n", current)
	for _, key := range order {
		in, err := file.At(key.noID, current, nil)
		if err != nil {
			return "", err
		}
		applies := in.Extenders
		if key.kind == amendment.KindCaptureTheFlag {
			applies = in.CaptureTheFlag
		}
		statement := "none applies yet."
		if applies != nil {
			statement = fmt.Sprintf("the entry from epoch %d applies.", applies.FromEpoch)
		}
		for i := range entries {
			entry := &entries[i]
			if entry.Kind() == key.kind && entry.NoID == key.noID && entry.FromEpoch > current {
				statement += fmt.Sprintf(" The entry from epoch %d is next.", entry.FromEpoch)
				break
			}
		}
		fmt.Fprintf(&out, "  no %d, %s: %s\n", key.noID, key.kind, statement)
	}
	return out.String(), nil
}
