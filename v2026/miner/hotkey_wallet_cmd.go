package miner

// The all-operators payout (docs/OPERATOR-DISCOVERY.md sections 5.2 and 6).
// One global consent, signed once by the coldkey and once by the hotkey, is
// kept as a chain under <base>/hotkey-wallet by package hotkeywallet. Every
// authenticated listed operator stores the chain, and the hotkey alone signs a
// delegation of the miner's network there to the chain's head:
//   - "wallet hotkey challenge" builds the next statement and keeps it pending
//     for an offline coldkey;
//   - "wallet hotkey set" adds both signatures, appends the generation, then
//     stores the chain and the delegation at each authenticated operator. One
//     operator's failure is reported and never stops the others;
//   - "wallet hotkey status" prints the chain and what each operator adopts.
//
// A generation 1 statement names the subnet that every reachable
// authenticated operator states in its own network consent challenge, which
// is never signed and expires at the operator. A later generation keeps its
// chain's subnet.
//
// Epochs when the flags are absent: generation 1 earns from epoch 0 and a
// later generation from the operators' current epoch plus 1, or just after
// the previous generation's start when that is later. A delegation earns from
// the operator's current epoch plus 2, past the operator's prospective
// boundary, or just after the previous delegation's start when that is later.
// Every interval ends 65535 epochs after its start.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/hotkeywallet"
	"github.com/urfoundation/sn/v2026/operatorlist"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
)

const hotkeyWalletDirectoryName = "hotkey-wallet"

// An earning interval holds at most 65536 epochs.
const hotkeyWalletIntervalEpochs = 65535

// The wallet list may name every provider of the network.
const hotkeyWalletAnswerBytes = 16 * 1024 * 1024

// Keeps every computed interval end within uint64.
const hotkeyWalletMaximumEpoch = math.MaxUint64 - 4*(hotkeyWalletIntervalEpochs+1)

func hotkeyWalletStore(base string) hotkeywallet.Store {
	return hotkeywallet.Store{Directory: filepath.Join(base, hotkeyWalletDirectoryName)}
}

// One authenticated listed operator.
type hotkeyWalletTarget struct {
	domain string
	apiUrl string
	byJwt  string
}

// The listed operators with a network jwt, and the domains of the others.
func hotkeyWalletTargets(snapshot *operatorlist.Snapshot, base string) ([]hotkeyWalletTarget, []string) {
	var targets []hotkeyWalletTarget
	var awaiting []string
	for _, operator := range snapshot.List.Operators {
		byJwt, err := clientauth.ReadToken(operatorJwtPath(base, operator.Domain))
		if err != nil {
			awaiting = append(awaiting, operator.Domain)
			continue
		}
		targets = append(targets, hotkeyWalletTarget{domain: operator.Domain, apiUrl: operator.ApiUrl, byJwt: byJwt})
	}
	return targets, awaiting
}

// One request the way hotkeywallet calls an operator: the network jwt as
// bearer, no redirects, a bounded JSON answer, and any status but 200 an
// error. The list already holds the api url to https or loopback http.
func hotkeyWalletCall(ctx context.Context, target hotkeyWalletTarget, method string, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(target.apiUrl, "/")+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+target.byJwt)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, hotkeyWalletAnswerBytes+1))
	if err != nil {
		return err
	}
	if len(answer) > hotkeyWalletAnswerBytes {
		return fmt.Errorf("%s %s answer exceeds %d bytes", method, path, hotkeyWalletAnswerBytes)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s: %s", method, path, response.Status, strings.TrimSpace(string(answer[:min(len(answer), 512)])))
	}
	if err := json.Unmarshal(answer, result); err != nil {
		return fmt.Errorf("%s %s answer is not JSON: %w", method, path, err)
	}
	return nil
}

func hotkeyWalletEpoch(ctx context.Context, target hotkeyWalletTarget) (uint64, error) {
	var result struct {
		Epoch uint64 `json:"epoch"`
	}
	if err := hotkeyWalletCall(ctx, target, http.MethodGet, "/sn/epoch", nil, &result); err != nil {
		return 0, err
	}
	if result.Epoch > hotkeyWalletMaximumEpoch {
		return 0, fmt.Errorf("the operator's current epoch %d is implausible", result.Epoch)
	}
	return result.Epoch, nil
}

// A network-level entry of GET /sn/wallet.
type hotkeyWalletEntry struct {
	ClientId          *string `json:"client_id"`
	ConsentScope      string  `json:"consent_scope"`
	ColdkeySs58       string  `json:"coldkey_ss58"`
	HotkeySs58        string  `json:"hotkey_ss58"`
	FromEpoch         uint64  `json:"from_epoch"`
	ThroughEpoch      uint64  `json:"through_epoch"`
	ConsentHeadHash   string  `json:"consent_head_hash"`
	ConsentGeneration uint64  `json:"consent_generation"`
}

// The network consent entry and the hotkey delegation entry, when listed.
func hotkeyWalletEntries(ctx context.Context, target hotkeyWalletTarget) (network *hotkeyWalletEntry, hotkey *hotkeyWalletEntry, returnErr error) {
	var result struct {
		Wallets []hotkeyWalletEntry `json:"wallets"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := hotkeyWalletCall(ctx, target, http.MethodGet, "/sn/wallet", nil, &result); err != nil {
		return nil, nil, err
	}
	if result.Error != nil {
		return nil, nil, fmt.Errorf("GET /sn/wallet: %s", result.Error.Message)
	}
	for index := range result.Wallets {
		entry := &result.Wallets[index]
		if entry.ClientId != nil || entry.FromEpoch > hotkeyWalletMaximumEpoch {
			continue
		}
		switch {
		case entry.ConsentScope == "network" && network == nil:
			network = entry
		case entry.ConsentScope == protocol.EarningWalletModeHotkey && hotkey == nil:
			hotkey = entry
		}
	}
	return network, hotkey, nil
}

// Whether the entry delegates the network to this head of the hotkey's chain.
func (self *hotkeyWalletEntry) adopts(hotkey [32]byte, headHash [32]byte, generation uint64) bool {
	if self == nil {
		return false
	}
	entryHotkey, err := ss58.DecodeWithPrefix(self.HotkeySs58, ss58.BittensorPrefix)
	if err != nil || entryHotkey != hotkey || self.ConsentGeneration != generation {
		return false
	}
	head, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(self.ConsentHeadHash, "0x"), "0X"))
	return err == nil && bytes.Equal(head, headHash[:])
}

// The subnet one operator states in a network consent challenge for the
// coldkey. The challenge is only read, never signed.
func hotkeyWalletOperatorSubnet(ctx context.Context, target hotkeyWalletTarget, coldkeySs58 string) (protocol.HotkeyWalletMappingSubnet, error) {
	epoch, err := hotkeyWalletEpoch(ctx, target)
	if err != nil {
		return protocol.HotkeyWalletMappingSubnet{}, err
	}
	network, _, err := hotkeyWalletEntries(ctx, target)
	if err != nil {
		return protocol.HotkeyWalletMappingSubnet{}, err
	}
	// past the operator's boundary and any network consent it already holds
	from := epoch + 2
	if network != nil && from <= network.FromEpoch {
		from = network.FromEpoch + 1
	}
	var challenge struct {
		Message string `json:"message"`
	}
	args := map[string]any{"coldkey_ss58": coldkeySs58, "from_epoch": from, "through_epoch": from + hotkeyWalletIntervalEpochs}
	if err := hotkeyWalletCall(ctx, target, http.MethodPost, "/sn/wallet/network-consent", args, &challenge); err != nil {
		return protocol.HotkeyWalletMappingSubnet{}, err
	}
	statement, err := protocol.DecodeNetworkWalletMappingStatement(challenge.Message)
	if err != nil {
		return protocol.HotkeyWalletMappingSubnet{}, fmt.Errorf("the network consent challenge does not decode: %w", err)
	}
	subnet := statement.Domain.HotkeySubnet()
	return subnet, subnet.Validate()
}

// The subnet the reachable authenticated operators state. They must all
// agree, and at least one must answer; an unreachable one is reported.
func hotkeyWalletSubnet(ctx context.Context, targets []hotkeyWalletTarget, coldkeySs58 string, out io.Writer) (protocol.HotkeyWalletMappingSubnet, error) {
	var subnet protocol.HotkeyWalletMappingSubnet
	source := ""
	for _, target := range targets {
		stated, err := hotkeyWalletOperatorSubnet(ctx, target, coldkeySs58)
		if err != nil {
			fmt.Fprintf(out, "operator %s: its subnet is unavailable: %v\n", target.domain, err)
			continue
		}
		if source == "" {
			subnet, source = stated, target.domain
		} else if stated != subnet {
			return protocol.HotkeyWalletMappingSubnet{}, fmt.Errorf("operators disagree on the subnet: %s states chain %d genesis 0x%x netuid %d, %s states chain %d genesis 0x%x netuid %d", source, subnet.ChainID, subnet.GenesisHash, subnet.Netuid, target.domain, stated.ChainID, stated.GenesisHash, stated.Netuid)
		}
	}
	if source == "" {
		return protocol.HotkeyWalletMappingSubnet{}, errors.New("no authenticated listed operator stated its subnet; authenticate one with provider auth --operator=<domain>")
	}
	return subnet, nil
}

// The latest current epoch among the reachable operators.
func hotkeyWalletCurrentEpoch(ctx context.Context, targets []hotkeyWalletTarget, out io.Writer) (uint64, error) {
	current, known := uint64(0), false
	for _, target := range targets {
		epoch, err := hotkeyWalletEpoch(ctx, target)
		if err != nil {
			fmt.Fprintf(out, "operator %s: its current epoch is unavailable: %v\n", target.domain, err)
			continue
		}
		current, known = max(current, epoch), true
	}
	if !known {
		return 0, errors.New("no authenticated listed operator stated its current epoch; give --wallet-from-epoch and --wallet-through-epoch")
	}
	return current, nil
}

// The global consent's earning epochs from the command line.
type hotkeyWalletEpochs struct {
	from    uint64
	through uint64
}

func hotkeyWalletEpochsFromOpts(opts docopt.Opts) (*hotkeyWalletEpochs, error) {
	from, _ := opts.String("--wallet-from-epoch")
	through, _ := opts.String("--wallet-through-epoch")
	if (from == "") != (through == "") {
		return nil, errors.New("--wallet-from-epoch and --wallet-through-epoch go together")
	}
	if from == "" {
		return nil, nil
	}
	first, err := strconv.ParseUint(from, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("--wallet-from-epoch=%s is not an epoch", from)
	}
	last, err := strconv.ParseUint(through, 10, 64)
	if err != nil || last < first || last-first > hotkeyWalletIntervalEpochs {
		return nil, fmt.Errorf("--wallet-through-epoch=%s must be at most %d epochs after --wallet-from-epoch", through, hotkeyWalletIntervalEpochs)
	}
	return &hotkeyWalletEpochs{from: first, through: last}, nil
}

// The next generation's statement for the coldkey, with the epochs of the
// flags or the defaults in the file comment.
func hotkeyWalletNextStatement(ctx context.Context, store hotkeywallet.Store, targets []hotkeyWalletTarget, hotkey *crv4.Keypair, coldkey [32]byte, coldkeySs58 string, epochs *hotkeyWalletEpochs, out io.Writer) (*protocol.HotkeyWalletMappingStatement, error) {
	chain, err := store.Chain()
	if err != nil {
		return nil, err
	}
	var head *protocol.HotkeyWalletMappingStatement
	var subnet protocol.HotkeyWalletMappingSubnet
	if len(chain) == 0 {
		if subnet, err = hotkeyWalletSubnet(ctx, targets, coldkeySs58, out); err != nil {
			return nil, err
		}
	} else {
		if head, _, err = protocol.VerifyHotkeyWalletMappingLineage(ctx, chain); err != nil {
			return nil, err
		}
		subnet = head.Subnet
	}
	if epochs == nil {
		epochs = &hotkeyWalletEpochs{}
		if head != nil {
			current, err := hotkeyWalletCurrentEpoch(ctx, targets, out)
			if err != nil {
				return nil, err
			}
			epochs.from = max(current+1, head.FromEpoch+1)
		}
		epochs.through = epochs.from + hotkeyWalletIntervalEpochs
	}
	return hotkeywallet.NextStatement(chain, subnet, hotkey.PublicKey(), coldkey, epochs.from, epochs.through, time.Now())
}

// What ensureOperatorHotkeyWallet left at one operator.
type hotkeyWalletOutcome struct {
	generation uint64
	// a new delegation was accepted, rather than one adopting the head found
	delegated    bool
	fromEpoch    uint64
	throughEpoch uint64
}

func (self hotkeyWalletOutcome) String() string {
	if !self.delegated {
		return fmt.Sprintf("chain stored; the delegation already adopts generation %d", self.generation)
	}
	return fmt.Sprintf("chain stored; delegated to generation %d from epoch %d through %d", self.generation, self.fromEpoch, self.throughEpoch)
}

// Stores the chain at one operator, then delegates the miner's network there
// to the chain's head unless the delegation already adopts it.
func ensureOperatorHotkeyWallet(ctx context.Context, target hotkeyWalletTarget, hotkey *crv4.Keypair, chain []protocol.HotkeyWalletMappingConsent) (hotkeyWalletOutcome, error) {
	operator := hotkeywallet.Operator{ApiUrl: target.apiUrl, ByJwt: target.byJwt}
	headHash, generation, err := operator.SubmitChain(ctx, chain)
	if err != nil {
		return hotkeyWalletOutcome{}, fmt.Errorf("storing the chain: %w", err)
	}
	outcome := hotkeyWalletOutcome{generation: generation}
	_, entry, err := hotkeyWalletEntries(ctx, target)
	if err != nil {
		return outcome, err
	}
	if entry.adopts(hotkey.PublicKey(), headHash, generation) {
		return outcome, nil
	}
	epoch, err := hotkeyWalletEpoch(ctx, target)
	if err != nil {
		return outcome, err
	}
	outcome.delegated = true
	outcome.fromEpoch = epoch + 2
	// the operator's chain of delegations starts strictly later each time
	if entry != nil && outcome.fromEpoch <= entry.FromEpoch {
		outcome.fromEpoch = entry.FromEpoch + 1
	}
	outcome.throughEpoch = outcome.fromEpoch + hotkeyWalletIntervalEpochs
	if err := operator.EnsureDelegation(ctx, hotkey, headHash, generation, outcome.fromEpoch, outcome.throughEpoch); err != nil {
		return outcome, fmt.Errorf("delegating the network: %w", err)
	}
	return outcome, nil
}

// Every authenticated operator in turn, each reported; returns how many failed.
func hotkeyWalletSubmit(ctx context.Context, targets []hotkeyWalletTarget, awaiting []string, hotkey *crv4.Keypair, chain []protocol.HotkeyWalletMappingConsent, out io.Writer) int {
	failed := 0
	for _, target := range targets {
		outcome, err := ensureOperatorHotkeyWallet(ctx, target, hotkey, chain)
		if err != nil {
			failed++
			fmt.Fprintf(out, "operator %s: failed: %v\n", target.domain, err)
			continue
		}
		fmt.Fprintf(out, "operator %s: %s\n", target.domain, outcome)
	}
	for _, domain := range awaiting {
		fmt.Fprintf(out, "operator %s: skipped, awaiting auth: provider auth --operator=%s\n", domain, domain)
	}
	return failed
}

func hotkeyWalletColdkey(opts docopt.Opts) (string, [32]byte, error) {
	coldkeySs58, _ := opts.String("<coldkey_ss58>")
	coldkeySs58 = strings.TrimSpace(coldkeySs58)
	coldkey, err := ss58.DecodeWithPrefix(coldkeySs58, ss58.BittensorPrefix)
	if err != nil {
		return "", [32]byte{}, fmt.Errorf("invalid ss58 coldkey %q: %w", coldkeySs58, err)
	}
	return coldkeySs58, coldkey, nil
}

func hotkeyWalletCommandHotkey(opts docopt.Opts) (*crv4.Keypair, string, error) {
	seedPath, err := opts.String("--hotkey_seed_file")
	if err != nil || seedPath == "" {
		return nil, "", errors.New("--hotkey_seed_file=<path> is required")
	}
	hotkey, err := loadOperatorHotkey(seedPath)
	return hotkey, seedPath, err
}

// "provider wallet hotkey challenge <coldkey_ss58>".
func hotkeyWalletChallenge(ctx context.Context, opts docopt.Opts, out io.Writer) error {
	base, err := providerStateDir()
	if err != nil {
		return err
	}
	hotkey, seedPath, err := hotkeyWalletCommandHotkey(opts)
	if err != nil {
		return err
	}
	coldkeySs58, coldkey, err := hotkeyWalletColdkey(opts)
	if err != nil {
		return err
	}
	epochs, err := hotkeyWalletEpochsFromOpts(opts)
	if err != nil {
		return err
	}
	snapshot, err := loadOperatorList(ctx, opts, base)
	if err != nil {
		return err
	}
	targets, _ := hotkeyWalletTargets(snapshot, base)
	store := hotkeyWalletStore(base)
	statement, err := hotkeyWalletNextStatement(ctx, store, targets, hotkey, coldkey, coldkeySs58, epochs, out)
	if err != nil {
		return err
	}
	if err := store.SetPending(*statement); err != nil {
		return err
	}
	message, err := statement.Message()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Sign this message with the coldkey %s: sr25519, \"substrate\" signing context.\n", coldkeySs58)
	fmt.Fprintf(out, "The bytes to sign are the UTF-8 text between the markers exactly as printed (LF line endings, no trailing newline);\n")
	fmt.Fprintf(out, "a Polkadot extension signRaw of type \"bytes\", which wraps them in <Bytes>...</Bytes>, is accepted too.\n")
	fmt.Fprintf(out, "It is generation %d of the global consent that pays the networks delegated to hotkey %s on every operator of netuid %d to this coldkey, for earning epochs %d through %d.\n", statement.Generation, hotkey.Address(), statement.Subnet.Netuid, statement.FromEpoch, statement.ThroughEpoch)
	fmt.Fprintf(out, "It stays pending in %s until it is set or replaced.\n", filepath.Join(store.Directory, "pending.json"))
	fmt.Fprintf(out, "\n----- message -----\n%s\n----- end -----\n", message)
	fmt.Fprintf(out, "message bytes (hex): 0x%s\n", hex.EncodeToString([]byte(message)))
	fmt.Fprintf(out, "\nThen submit the 64-byte signature as hex:\n")
	fmt.Fprintf(out, "  provider wallet hotkey set %s --hotkey_seed_file=%s --message='%s' --signature=0x<128 hex chars>", coldkeySs58, snWalletShellValue(seedPath), snEscapeMessage(message))
	if operatorsUrl, _ := opts.String("--operators-url"); operatorsUrl != "" {
		fmt.Fprintf(out, " --operators-url=%s", snWalletShellValue(operatorsUrl))
	}
	fmt.Fprintln(out)
	return nil
}

// "provider wallet hotkey set <coldkey_ss58>".
func hotkeyWalletSet(ctx context.Context, opts docopt.Opts, out io.Writer) error {
	base, err := providerStateDir()
	if err != nil {
		return err
	}
	hotkey, _, err := hotkeyWalletCommandHotkey(opts)
	if err != nil {
		return err
	}
	coldkeySs58, coldkey, err := hotkeyWalletColdkey(opts)
	if err != nil {
		return err
	}
	epochs, err := hotkeyWalletEpochsFromOpts(opts)
	if err != nil {
		return err
	}
	seedFile, _ := opts.String("--coldkey_seed_file")
	message, _ := opts.String("--message")
	signatureHex, _ := opts.String("--signature")
	if seedFile == "" && message == "" {
		return errors.New("the coldkey's signature is required: --coldkey_seed_file=<path>, or --message and --signature for the statement provider wallet hotkey challenge printed")
	}
	snapshot, err := loadOperatorList(ctx, opts, base)
	if err != nil {
		return err
	}
	targets, awaiting := hotkeyWalletTargets(snapshot, base)
	store := hotkeyWalletStore(base)
	chain, err := store.Chain()
	if err != nil {
		return err
	}

	var original protocol.HotkeyWalletMappingConsent
	stored := false
	if seedFile != "" {
		coldkeyPair, err := snLoadColdkey(seedFile, coldkeySs58, coldkey)
		if err != nil {
			return err
		}
		// a pending statement for this coldkey and these epochs is the one to sign
		statement, err := store.Pending()
		if err != nil {
			return err
		}
		if statement == nil || statement.Generation != uint64(len(chain))+1 || statement.Hotkey != hotkey.PublicKey() || statement.Coldkey != coldkey || epochs != nil && (statement.FromEpoch != epochs.from || statement.ThroughEpoch != epochs.through) {
			if statement, err = hotkeyWalletNextStatement(ctx, store, targets, hotkey, coldkey, coldkeySs58, epochs, out); err != nil {
				return err
			}
			if err := store.SetPending(*statement); err != nil {
				return err
			}
		}
		if original.Message, err = statement.Message(); err != nil {
			return err
		}
		if original.ColdkeySignature, err = hotkeywallet.Sign(coldkeyPair, original.Message); err != nil {
			return err
		}
	} else {
		original.Message = snUnescapeMessage(message)
		normalized, err := snNormalizeSignatureHex(signatureHex)
		if err != nil {
			return err
		}
		signature, _ := hex.DecodeString(strings.TrimPrefix(normalized, "0x"))
		original.ColdkeySignature = [64]byte(signature)
		if len(chain) > 0 && chain[len(chain)-1].Message == original.Message {
			// set again after the generation was appended: resend the stored head
			original.HotkeySignature = chain[len(chain)-1].HotkeySignature
			statement, _, err := protocol.VerifyHotkeyWalletMappingConsent(ctx, original)
			if err != nil {
				return fmt.Errorf("the signature does not verify over the stored head: %w", err)
			}
			if statement.Coldkey != coldkey {
				return fmt.Errorf("the stored head is not a statement for coldkey %s", coldkeySs58)
			}
			original, stored = chain[len(chain)-1], true
		} else {
			pending, err := store.Pending()
			if err != nil {
				return err
			}
			if pending == nil {
				return errors.New("no hotkey wallet statement is pending; run provider wallet hotkey challenge first")
			}
			pendingMessage, err := pending.Message()
			if err != nil {
				return err
			}
			switch {
			case pendingMessage != original.Message:
				return fmt.Errorf("--message is not the pending statement in %s; sign the message that provider wallet hotkey challenge printed last", filepath.Join(store.Directory, "pending.json"))
			case pending.Coldkey != coldkey:
				pendingColdkey, _ := ss58.Encode(pending.Coldkey, ss58.BittensorPrefix)
				return fmt.Errorf("the pending statement names coldkey %s, not %s", pendingColdkey, coldkeySs58)
			case pending.Hotkey != hotkey.PublicKey():
				return errors.New("the pending statement names another hotkey than --hotkey_seed_file")
			case epochs != nil && (pending.FromEpoch != epochs.from || pending.ThroughEpoch != epochs.through):
				return fmt.Errorf("the pending statement earns epochs %d through %d, not %d through %d", pending.FromEpoch, pending.ThroughEpoch, epochs.from, epochs.through)
			}
		}
	}
	if !stored {
		if original.HotkeySignature, err = hotkeywallet.Sign(hotkey, original.Message); err != nil {
			return err
		}
		if _, _, err := protocol.VerifyHotkeyWalletMappingConsent(ctx, original); err != nil {
			return fmt.Errorf("the coldkey's signature does not verify for %s: %w", coldkeySs58, err)
		}
		if err := store.Append(original); err != nil {
			return err
		}
	}
	if chain, err = store.Chain(); err != nil {
		return err
	}
	head, headHash, err := protocol.VerifyHotkeyWalletMappingLineage(ctx, chain)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "hotkey wallet generation %d: coldkey %s, epochs %d through %d, head 0x%x\n", head.Generation, coldkeySs58, head.FromEpoch, head.ThroughEpoch, headHash)
	if failed := hotkeyWalletSubmit(ctx, targets, awaiting, hotkey, chain, out); failed > 0 {
		return fmt.Errorf("%d of %d authenticated operators do not adopt the head yet; set it again with the same --message and --signature, or keep provide --all-operators --hotkey_seed_file running, which retries every hour", failed, len(targets))
	}
	return nil
}

// "provider wallet hotkey status".
func hotkeyWalletStatus(ctx context.Context, opts docopt.Opts, out io.Writer) error {
	base, err := providerStateDir()
	if err != nil {
		return err
	}
	store := hotkeyWalletStore(base)
	chain, err := store.Chain()
	if err != nil {
		return err
	}
	var hotkey [32]byte
	if seedPath, err := opts.String("--hotkey_seed_file"); err == nil && seedPath != "" {
		keypair, err := loadOperatorHotkey(seedPath)
		if err != nil {
			return err
		}
		hotkey = keypair.PublicKey()
	}
	var head *protocol.HotkeyWalletMappingStatement
	var headHash [32]byte
	if len(chain) == 0 {
		fmt.Fprintf(out, "hotkey wallet chain: none in %s\n", store.Directory)
	} else {
		fmt.Fprintf(out, "hotkey wallet chain in %s:\n", store.Directory)
		for _, original := range chain {
			statement, hash, err := protocol.VerifyHotkeyWalletMappingConsent(ctx, original)
			if err != nil {
				return err
			}
			coldkeySs58, _ := ss58.Encode(statement.Coldkey, ss58.BittensorPrefix)
			fmt.Fprintf(out, "  generation %d: coldkey %s, epochs %d through %d, original 0x%x\n", statement.Generation, coldkeySs58, statement.FromEpoch, statement.ThroughEpoch, hash)
			head, headHash = statement, hash
		}
		hotkeySs58, _ := ss58.Encode(head.Hotkey, ss58.BittensorPrefix)
		fmt.Fprintf(out, "head: generation %d, 0x%x, hotkey %s, chain %d netuid %d\n", head.Generation, headHash, hotkeySs58, head.Subnet.ChainID, head.Subnet.Netuid)
		if hotkey != ([32]byte{}) && hotkey != head.Hotkey {
			fmt.Fprintln(out, "warning: --hotkey_seed_file is not the chain's hotkey")
		}
		hotkey = head.Hotkey
	}
	if pending, err := store.Pending(); err != nil {
		return err
	} else if pending != nil {
		coldkeySs58, _ := ss58.Encode(pending.Coldkey, ss58.BittensorPrefix)
		fmt.Fprintf(out, "pending: generation %d for coldkey %s, epochs %d through %d, awaiting the coldkey's signature\n", pending.Generation, coldkeySs58, pending.FromEpoch, pending.ThroughEpoch)
	}
	snapshot, err := loadOperatorList(ctx, opts, base)
	if err != nil {
		return err
	}
	targets, awaiting := hotkeyWalletTargets(snapshot, base)
	for _, target := range targets {
		_, entry, err := hotkeyWalletEntries(ctx, target)
		switch {
		case err != nil:
			fmt.Fprintf(out, "operator %s: unavailable: %v\n", target.domain, err)
		case entry == nil:
			fmt.Fprintf(out, "operator %s: no hotkey delegation\n", target.domain)
		case head != nil && entry.adopts(hotkey, headHash, head.Generation):
			fmt.Fprintf(out, "operator %s: adopts the head, generation %d, from epoch %d through %d\n", target.domain, head.Generation, entry.FromEpoch, entry.ThroughEpoch)
		default:
			fmt.Fprintf(out, "operator %s: delegates to hotkey %s at generation %d, not the head\n", target.domain, entry.HotkeySs58, entry.ConsentGeneration)
		}
	}
	for _, domain := range awaiting {
		fmt.Fprintf(out, "operator %s: awaiting auth\n", domain)
	}
	return nil
}

// "provider wallet hotkey challenge|set|status".
func hotkeyWalletCmd(opts docopt.Opts) {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(event.Ctx())
	var err error
	if challenge, _ := opts.Bool("challenge"); challenge {
		err = hotkeyWalletChallenge(ctx, opts, os.Stdout)
	} else if set, _ := opts.Bool("set"); set {
		err = hotkeyWalletSet(ctx, opts, os.Stdout)
	} else {
		err = hotkeyWalletStatus(ctx, opts, os.Stdout)
	}
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallet hotkey: %v\n", err)
		os.Exit(1)
	}
}
