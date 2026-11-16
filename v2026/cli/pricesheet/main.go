// Command pricesheet is the owner's tool for the signed demand deposit price at
// https://ur.xyz/price.yml. It holds no key and signs nothing: message prints the
// exact line to approve and the command that signs it on the Ledger, attach adds a
// signed entry after the last one once its signer checks out, and verify reads a file
// or the published sheet as operators and validators do.
//
//	pricesheet message --in=price.yml --epoch=N --cap=ALPHA [--chain=964] [--rpc=URL]
//	pricesheet attach --in=price.yml --epoch=N --cap=ALPHA --signature=0x... [--chain=964] [--rpc=URL]
//	pricesheet verify --in=FILE | --url=URL [--chain=964] [--rpc=URL]
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/demand/pricesheet"
	"github.com/urfoundation/sn/v2026/evmrpc"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

const commandUsage = `pricesheet publishes the signed demand deposit price. It holds no key and signs nothing.

  pricesheet message --in=price.yml --epoch=N --cap=ALPHA [--chain=964] [--rpc=URL] [--ledger-path=PATH]
      Reads the rates from the file's tiered_per_block_demand_deposits and prints the
      exact line to sign and the cast command that signs it on the Ledger. PATH is the
      owner key's derivation path, which this tool does not hold; without it the printed
      command has a blank to fill in. It refuses an epoch at or below the newest entry,
      as attach would, so nobody signs for it. With --rpc it also refuses an epoch that
      is not at least the current epoch + 1.

  pricesheet attach --in=price.yml --epoch=N --cap=ALPHA --signature=0x... [--chain=964] [--rpc=URL]
      Adds the signed entry after the last one in signed_schedule, leaving every other
      byte of the file as it is. It refuses an epoch at or below the newest entry and a
      signature from anyone but the pinned signer, and writes only after verifying the
      whole result.

  pricesheet verify --in=FILE | --url=URL [--chain=964] [--rpc=URL]
      Parses strictly, verifies every entry, and checks that the file's top-level rates
      equal the newest entry. With --rpc it says which entry applies now.
`

const (
	defaultChain = "964"
	// ledgerPathBlank stands in for the owner key's derivation path, which stays out of
	// this code. cast cannot read it as a path, so the command fails until it is filled.
	ledgerPathBlank = "<ledger-path>"
	readTimeout     = 30 * time.Second
	// pricesheet.Parse refuses a file past this size; reading one byte more lets it.
	maxSheetBytes = 256 << 10
)

// mainnetCoordinator counts the epochs of the only deployment with a pinned signer.
var mainnetCoordinator = common.HexToAddress("0xC18925925E2B7bb9059b7d696b8c92762AE86406")

var (
	wholeText      = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	ledgerPathText = regexp.MustCompile(`^m(/[0-9]+'?)+$`)
	decimalText    = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
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
	if err := run(ctx, os.Args[1:], os.Stdout, dependencies{signerFor: pricesheet.SignerFor}); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "pricesheet:", strings.ReplaceAll(err.Error(), "pricesheet: ", ""))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, deps dependencies) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(stdout, commandUsage)
		return err
	}
	command := args[0]
	if command != "message" && command != "attach" && command != "verify" {
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
	default:
		return runVerify(ctx, opts, stdout, deps)
	}
}

type options struct {
	in, url, epoch, capAlpha, chain, signature, rpc, ledgerPath string
}

// parseOptions takes every number as text. The flag package's own integers read
// 010 as 8 and 0x10 as 16, which is no way to name the epoch of a price.
func parseOptions(command string, args []string) (*options, error) {
	opts := &options{}
	flags := flag.NewFlagSet("pricesheet "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.in, "in", "", "the price sheet file")
	flags.StringVar(&opts.chain, "chain", defaultChain, "the EVM chain id")
	flags.StringVar(&opts.rpc, "rpc", "", "a read-only RPC to read the current epoch from")
	if command == "message" || command == "attach" {
		flags.StringVar(&opts.epoch, "epoch", "", "the first epoch the price applies to")
		flags.StringVar(&opts.capAlpha, "cap", "", "one operator's deposit cap for one epoch, in alpha")
	}
	if command == "message" {
		flags.StringVar(&opts.ledgerPath, "ledger-path", "", "the owner key's derivation path, for the printed cast command")
	}
	if command == "attach" {
		flags.StringVar(&opts.signature, "signature", "", "the signature cast printed")
	}
	if command == "verify" {
		flags.StringVar(&opts.url, "url", "", "fetch the sheet from this address instead of --in")
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

// parseAlpha reads an amount of alpha exactly, never through a float.
func parseAlpha(name, text string) (uint64, error) {
	if !decimalText.MatchString(text) {
		return 0, fmt.Errorf("%s must be a plain decimal number of alpha, got %q", name, text)
	}
	alpha, ok := new(big.Rat).SetString(text)
	if !ok {
		return 0, fmt.Errorf("%s must be a plain decimal number of alpha, got %q", name, text)
	}
	rao := alpha.Mul(alpha, big.NewRat(1_000_000_000, 1))
	if !rao.IsInt() || !rao.Num().IsUint64() {
		return 0, fmt.Errorf("%s is not a whole number of rao: %q", name, text)
	}
	return rao.Num().Uint64(), nil
}

// parseStrict is pricesheet.Parse and one thing more. Parse passes over top-level
// fields it does not use, which suits a reader but not the tool publishing the file:
// a misspelt signed_schedule would read as no signed prices at all.
func parseStrict(raw []byte) (*pricesheet.Sheet, error) {
	sheet, err := pricesheet.Parse(raw)
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
		case key.Value != "sn" && key.Value != "tiered_per_block_demand_deposits" && key.Value != "signed_schedule":
			return nil, fmt.Errorf("line %d: unexpected top-level field %q", key.Line, key.Value)
		}
	}
	return sheet, nil
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

// checkMirror checks the top-level rates: a valid schedule and, once any price is
// signed, the same rates as the newest entry. The comparison is of the signed line,
// so it is by value: 0.10 and 0.1 state one price.
func checkMirror(sheet *pricesheet.Sheet, chainID uint64) error {
	if _, err := protocol.ValidateDepositTiers(sheet.Mirror); err != nil {
		return fmt.Errorf("the rates in tiered_per_block_demand_deposits: %w", err)
	}
	probe := pricesheet.Entry{EpochCapRao: 1, Tiers: sheet.Mirror}
	if _, err := probe.Message(chainID, sheet.Netuid); err != nil {
		return fmt.Errorf("the rates in tiered_per_block_demand_deposits: %w", err)
	}
	newest := sheet.Newest()
	if newest == nil {
		return nil
	}
	signed, err := newest.Message(chainID, sheet.Netuid)
	if err != nil {
		return err
	}
	probe = *newest
	probe.Tiers = sheet.Mirror
	shown, err := probe.Message(chainID, sheet.Netuid)
	if err != nil {
		return fmt.Errorf("the rates in tiered_per_block_demand_deposits: %w", err)
	}
	if shown != signed {
		return fmt.Errorf("the rates in tiered_per_block_demand_deposits differ from the newest entry:\n  newest: %s\n  file:   %s", signed, shown)
	}
	return nil
}

// checkSheet is all verify promises about a parsed sheet.
func checkSheet(sheet *pricesheet.Sheet, chainID uint64, signer common.Address) error {
	if err := sheet.Verify(chainID, sheet.Netuid, signer); err != nil {
		return err
	}
	return checkMirror(sheet, chainID)
}

func pinnedSigner(deps dependencies, chainID uint64, netuid uint16) (common.Address, error) {
	signer, ok := deps.signerFor(chainID, netuid)
	if !ok {
		return common.Address{}, fmt.Errorf("chain %d sn %d has no pinned signer, so it has no published price", chainID, netuid)
	}
	return signer, nil
}

// draft is a price on its way into a sheet, signed or not.
type draft struct {
	raw     []byte
	sheet   *pricesheet.Sheet
	chainID uint64
	signer  common.Address
	entry   pricesheet.Entry
	line    string
}

func loadDraft(opts *options, deps dependencies) (*draft, error) {
	if opts.in == "" {
		return nil, errors.New("--in is required")
	}
	epoch, err := parseWhole("--epoch", opts.epoch)
	if err != nil {
		return nil, err
	}
	capRao, err := parseAlpha("--cap", opts.capAlpha)
	if err != nil {
		return nil, err
	}
	chainID, err := parseWhole("--chain", opts.chain)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(opts.in)
	if err != nil {
		return nil, err
	}
	sheet, err := parseStrict(raw)
	if err != nil {
		return nil, err
	}
	signer, err := pinnedSigner(deps, chainID, sheet.Netuid)
	if err != nil {
		return nil, err
	}
	if err := sheet.Verify(chainID, sheet.Netuid, signer); err != nil {
		return nil, fmt.Errorf("an entry already in %s does not verify: %w", opts.in, err)
	}
	// The rates to sign are the file's own, so a person reads them where the site does.
	entry := pricesheet.Entry{EffectiveEpoch: epoch, EpochCapRao: capRao, Tiers: slices.Clone(sheet.Mirror)}
	line, err := entry.Message(chainID, sheet.Netuid)
	if err != nil {
		return nil, fmt.Errorf("the rates in tiered_per_block_demand_deposits: %w", err)
	}
	return &draft{raw: raw, sheet: sheet, chainID: chainID, signer: signer, entry: entry, line: line}, nil
}

// checkPlace refuses, before anyone signs, a price the sheet could not take: a new
// entry comes after every existing one. One placed earlier could reprice epochs readers
// have already decided, so they refuse that file, and one at an existing epoch would
// change a signed entry, which never changes.
func (d *draft) checkPlace() error {
	newest := d.sheet.Newest()
	if newest == nil || d.entry.EffectiveEpoch > newest.EffectiveEpoch {
		return nil
	}
	return fmt.Errorf("epoch %d is not after the newest entry, from epoch %d: a new entry must come after every existing one, because readers refuse a file that could reprice epochs they have already decided, and a signed entry never changes", d.entry.EffectiveEpoch, newest.EffectiveEpoch)
}

// checkEpochAhead refuses an epoch too soon to publish for: a price is published at
// least one epoch before it takes effect.
func checkEpochAhead(ctx context.Context, endpoint string, chainID, epoch uint64) (current uint64, err error) {
	current, err = readCurrentEpoch(ctx, endpoint, chainID)
	if err != nil {
		return 0, err
	}
	if epoch <= current {
		return current, fmt.Errorf("epoch %d is too soon: the current epoch is %d and a price is published at least one epoch before it takes effect, so the earliest is %d", epoch, current, current+1)
	}
	return current, nil
}

func readCurrentEpoch(ctx context.Context, endpoint string, chainID uint64) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	client, err := evmrpc.DialContext(ctx, endpoint)
	if err != nil {
		return 0, fmt.Errorf("the RPC at %s: %w", endpoint, err)
	}
	defer client.Close()
	reported, err := client.ChainID(ctx)
	if err != nil {
		return 0, fmt.Errorf("read the chain id from %s: %w", endpoint, err)
	}
	if !reported.IsUint64() || reported.Uint64() != chainID {
		return 0, fmt.Errorf("the RPC at %s is on chain %s, not %d", endpoint, reported, chainID)
	}
	coordinator := stabi.NewSTCoordinator()
	output, err := client.CallContract(ctx, ethereum.CallMsg{To: &mainnetCoordinator, Data: coordinator.PackCurrentEpoch()}, nil)
	if err != nil {
		return 0, fmt.Errorf("read currentEpoch() from %s: %w", endpoint, err)
	}
	epoch, err := coordinator.UnpackCurrentEpoch(output)
	if err != nil {
		return 0, fmt.Errorf("read currentEpoch() from %s: %w", endpoint, err)
	}
	if !epoch.IsUint64() {
		return 0, fmt.Errorf("currentEpoch() from %s is out of range: %s", endpoint, epoch)
	}
	return epoch.Uint64(), nil
}

func runMessage(ctx context.Context, opts *options, stdout io.Writer, deps dependencies) error {
	ledgerPath, ledgerNote := opts.ledgerPath, ""
	if ledgerPath == "" {
		ledgerPath = ledgerPathBlank
		ledgerNote = "\nPut the owner key's derivation path in place of " + ledgerPathBlank + ", or pass --ledger-path.\n"
	} else if !ledgerPathText.MatchString(ledgerPath) {
		return fmt.Errorf("--ledger-path must look like m/44'/60'/0'/0/0, got %q", ledgerPath)
	}
	d, err := loadDraft(opts, deps)
	if err != nil {
		return err
	}
	if err := d.checkPlace(); err != nil {
		return err
	}
	chainCheck := "Not checked against the chain: pass --rpc to refuse an epoch that is too soon."
	if opts.rpc != "" {
		current, err := checkEpochAhead(ctx, opts.rpc, d.chainID, d.entry.EffectiveEpoch)
		if err != nil {
			return err
		}
		chainCheck = fmt.Sprintf("The current epoch is %d, so epoch %d is at least one epoch ahead.", current, d.entry.EffectiveEpoch)
	}
	chainFlag := ""
	if opts.chain != defaultChain {
		chainFlag = " --chain=" + opts.chain
	}
	_, err = fmt.Fprintf(stdout, `The line to sign:

%[1]s

%[2]s

Sign it with the Ledger's Ethereum app open. The device must show exactly this line,
and the key is %[3]s:

cast wallet sign --ledger --mnemonic-derivation-path "%[4]s" "%[1]s"
%[9]s
Then attach the signature it prints:

go run ./cli/pricesheet attach --in=%[5]s --epoch=%[6]d --cap=%[7]s%[8]s --signature=0x...
`, d.line, chainCheck, d.signer, ledgerPath, opts.in, d.entry.EffectiveEpoch, alphaText(d.entry.EpochCapRao, 1), chainFlag, ledgerNote)
	return err
}

func runAttach(ctx context.Context, opts *options, stdout io.Writer, deps dependencies) error {
	d, err := loadDraft(opts, deps)
	if err != nil {
		return err
	}
	if err := d.checkPlace(); err != nil {
		return err
	}
	if !signatureText.MatchString(opts.signature) {
		return errors.New("--signature must be 0x and 65 bytes of hex, as cast prints it")
	}
	if d.entry.Signature, err = hex.DecodeString(opts.signature[2:]); err != nil {
		return fmt.Errorf("--signature: %w", err)
	}
	if err := d.entry.Verify(d.chainID, d.sheet.Netuid, d.signer); err != nil {
		return fmt.Errorf("%w\nThe signature must be over exactly this line, built from the file's rates, --epoch and --cap:\n%s", err, d.line)
	}
	if opts.rpc != "" {
		if _, err := checkEpochAhead(ctx, opts.rpc, d.chainID, d.entry.EffectiveEpoch); err != nil {
			return err
		}
	}
	next, err := appendEntry(d.raw, &d.entry)
	if err != nil {
		return err
	}
	if err := d.checkResult(next); err != nil {
		return fmt.Errorf("nothing written: %w", err)
	}
	if err := writeFileAtomic(opts.in, next); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Attached to %s, signed by %s:\n\n%s\n\nCheck the whole file before publishing it:\n\ngo run ./cli/pricesheet verify --in=%s --rpc=URL\n", opts.in, d.signer, d.line, opts.in)
	return err
}

// checkResult reads the bytes about to be written as any reader would and holds them
// to what attach promises: the same sheet with this one entry more.
func (d *draft) checkResult(next []byte) error {
	sheet, err := parseStrict(next)
	if err != nil {
		return err
	}
	if err := checkSheet(sheet, d.chainID, d.signer); err != nil {
		return err
	}
	if len(sheet.Entries) != len(d.sheet.Entries)+1 || !pricesheet.Extends(sheet, d.sheet) {
		return errors.New("the result does not keep every earlier entry unchanged")
	}
	added := sheet.Newest()
	if added.EffectiveEpoch != d.entry.EffectiveEpoch || !bytes.Equal(added.Signature, d.entry.Signature) {
		return fmt.Errorf("the result's newest entry is not epoch %d with this signature", d.entry.EffectiveEpoch)
	}
	line, err := added.Message(d.chainID, sheet.Netuid)
	if err != nil {
		return err
	}
	if line != d.line {
		return fmt.Errorf("the result reads differently from what was signed:\n  signed: %s\n  result: %s", d.line, line)
	}
	return nil
}

// appendEntry adds the entry's lines right after the last entry and touches nothing
// else, so comments and earlier entries stay byte for byte. A layout it cannot add
// lines to is refused; whatever it does write is read back by checkResult before it
// reaches the disk.
func appendEntry(raw []byte, entry *pricesheet.Entry) ([]byte, error) {
	if bytes.IndexByte(raw, '\r') >= 0 {
		return nil, errors.New("the file has carriage returns; use plain newlines")
	}
	root, err := documentRoot(raw)
	if err != nil {
		return nil, err
	}
	var key, schedule *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "signed_schedule" {
			key, schedule = root.Content[i], root.Content[i+1]
		}
	}
	if key == nil {
		return splice(raw, len(raw), "signed_schedule:\n"+renderEntry(entry, 2)), nil
	}
	if schedule.Kind != yaml.MappingNode || schedule.Style&yaml.FlowStyle != 0 || len(schedule.Content) == 0 {
		return nil, fmt.Errorf("line %d: signed_schedule must be a block mapping for attach to add to; remove it if it is empty", key.Line)
	}
	after := lastLine(schedule.Content[len(schedule.Content)-1])
	return splice(raw, offsetAfterLine(raw, after), renderEntry(entry, schedule.Content[0].Column-1)), nil
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

func renderEntry(entry *pricesheet.Entry, indent int) string {
	var text strings.Builder
	write := func(level int, format string, args ...any) {
		text.WriteString(strings.Repeat(" ", indent+2*level))
		fmt.Fprintf(&text, format+"\n", args...)
	}
	write(0, "%d:", entry.EffectiveEpoch)
	write(1, "epoch_cap_alpha: %s", alphaText(entry.EpochCapRao, 1))
	write(1, `signature: "0x%s"`, hex.EncodeToString(entry.Signature))
	write(1, "tiers:")
	for _, tier := range entry.Tiers {
		write(2, "%s:", alphaText(tier.MinConvictionRao, 1))
		write(3, "alpha_per_gib: %s", alphaText(tier.RateNumeratorRaoPerGiB, tier.RateDenominator))
		write(3, "alpha_per_user: %s", alphaText(tier.RateNumeratorRaoPerUser, tier.RateDenominator))
	}
	return text.String()
}

// alphaText writes numerator/denominator rao as an exact decimal of alpha. A sheet's
// denominators are powers of ten that fit a uint64, so 36 places always end the number.
func alphaText(numerator, denominator uint64) string {
	alpha := new(big.Rat).SetFrac(
		new(big.Int).SetUint64(numerator),
		new(big.Int).Mul(new(big.Int).SetUint64(denominator), big.NewInt(1_000_000_000)),
	)
	text := strings.TrimRight(alpha.FloatString(36), "0")
	return strings.TrimSuffix(text, ".")
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

func fetchSheet(ctx context.Context, url string) ([]byte, error) {
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
	return io.ReadAll(io.LimitReader(response.Body, maxSheetBytes+1))
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
		raw, err = fetchSheet(ctx, opts.url)
	} else {
		raw, err = os.ReadFile(opts.in)
	}
	if err != nil {
		return err
	}
	sheet, err := parseStrict(raw)
	if err != nil {
		return err
	}
	signer, err := pinnedSigner(deps, chainID, sheet.Netuid)
	if err != nil {
		return err
	}
	if err := checkSheet(sheet, chainID, signer); err != nil {
		return err
	}
	var current *uint64
	if opts.rpc != "" {
		epoch, err := readCurrentEpoch(ctx, opts.rpc, chainID)
		if err != nil {
			return err
		}
		current = &epoch
	}
	return report(stdout, source, sheet, chainID, signer, current)
}

func report(stdout io.Writer, source string, sheet *pricesheet.Sheet, chainID uint64, signer common.Address, current *uint64) error {
	var out strings.Builder
	fmt.Fprintf(&out, "%s: chain %d, sn %d, signer %s\n", source, chainID, sheet.Netuid, signer)
	if newest := sheet.Newest(); newest != nil {
		fmt.Fprintf(&out, "%d signed, each verified:\n", len(sheet.Entries))
		for i := range sheet.Entries {
			line, err := sheet.Entries[i].Message(chainID, sheet.Netuid)
			if err != nil {
				return err
			}
			fmt.Fprintf(&out, "  %s\n", line)
		}
		fmt.Fprintf(&out, "The file's top-level rates equal the newest entry, from epoch %d.\n", newest.EffectiveEpoch)
	} else {
		fmt.Fprintln(&out, "No signed entries: no signed price is in force, and the signed policy's schedule stays in force.")
		if zeroPrice, _ := protocol.ValidateDepositTiers(sheet.Mirror); !zeroPrice {
			fmt.Fprintln(&out, "Warning: the file's top-level rates are not zero and nobody signed them. They are in force nowhere, and the site's publish gate refuses them.")
		}
	}
	out.WriteString(nowStatement(sheet, current))
	_, err := io.WriteString(stdout, out.String())
	return err
}

// nowStatement says which entry applies at the current epoch, and which comes next.
func nowStatement(sheet *pricesheet.Sheet, current *uint64) string {
	if current == nil {
		if len(sheet.Entries) == 0 {
			return ""
		}
		return "Pass --rpc to see which entry applies now.\n"
	}
	var next *pricesheet.Entry
	for i := range sheet.Entries {
		if sheet.Entries[i].EffectiveEpoch > *current {
			next = &sheet.Entries[i]
			break
		}
	}
	statement := fmt.Sprintf("Current epoch %d: no signed price is in force yet.", *current)
	if entry := sheet.At(*current); entry != nil {
		statement = fmt.Sprintf("Current epoch %d: the entry from epoch %d applies.", *current, entry.EffectiveEpoch)
	} else if next == nil {
		statement = fmt.Sprintf("Current epoch %d: no signed price is in force.", *current)
	}
	if next != nil {
		statement += fmt.Sprintf(" The entry from epoch %d is next.", next.EffectiveEpoch)
	}
	return statement + "\n"
}
