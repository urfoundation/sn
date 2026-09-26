package miner

// sn_wallet.go — `provider wallet set` / `provider wallet challenge` and the
// `provide --wallet` startup set (sn/PLAN.md 7.3, decision D-2).
//
// `POST /sn/wallet` on the main network refuses a pasted address: the set
// must carry the coldkey's sr25519 signature over the single-use challenge
// issued by `POST /auth/wallet-challenge` (blockchain TAO, pinned to the
// address), exactly what the ur.io wallet bridge sends. The CLI proves
// possession of the coldkey one of two ways:
//
//   - --coldkey_seed_file=<path>: the 32-byte sr25519 seed (raw, or 64 hex
//     chars with an optional 0x prefix). The keypair is derived the way
//     subkey / polkadot-js / btcli derive it (crv4.KeypairFromSeed), refused
//     unless it derives <coldkey_ss58>; the CLI fetches the challenge and
//     signs it locally. The seed never leaves the host.
//   - --message=<text> --signature=<hex>: a challenge printed by
//     `provider wallet challenge <coldkey_ss58>` and signed elsewhere (btcli,
//     a hardware wallet, a browser extension).
//
// Signing format, byte for byte what the server verifies
// (server/model/auth_bittensor.go VerifyBittensorSignature): an sr25519
// signature in the "substrate" signing context over the UTF-8 challenge text
// (LF line endings, no trailing newline), either raw or wrapped in
// <Bytes>…</Bytes> — the wrapped form is what a Polkadot extension's signRaw
// of type "bytes" signs, so the seed-file path signs that form. The signature
// travels as hex, 0x-prefixed, 64 bytes.
//
// With neither option the request is sent unsigned, which only a deployment
// with the `wallet_allow_unsigned` policy accepts (never main); the refusal
// is explained with the two ways to sign.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
)

// snWalletChallengePurpose is the purpose the ur.io wallet bridge sends with
// the challenge request when the signature proves a subnet claim wallet.
const snWalletChallengePurpose = "connect"

// snWalletProofHint is appended to a refused unsigned set.
const snWalletProofHint = `the main network requires the coldkey's signature over the wallet challenge. Either:
  provider wallet set <coldkey_ss58> --coldkey_seed_file=<path>            (signs the challenge here)
or sign it elsewhere:
  provider wallet challenge <coldkey_ss58>                                 (prints the message to sign)
  provider wallet set <coldkey_ss58> --message=<text> --signature=<hex>`

// snWalletProof is how a wallet set proves possession of the coldkey. At most
// one of the two forms is set; both empty means an unsigned set.
type snWalletProof struct {
	// SeedFile holds the coldkey's 32-byte sr25519 seed.
	SeedFile string
	// Message is the exact challenge text (or its one-line form with a
	// literal \n per line break) and Signature the coldkey's signature over
	// it, hex with an optional 0x prefix.
	Message   string
	Signature string
}

func (self *snWalletProof) unsigned() bool {
	return self == nil || (self.SeedFile == "" && self.Message == "" && self.Signature == "")
}

// snWalletProofFromOpts reads --coldkey_seed_file, --message and --signature.
func snWalletProofFromOpts(opts docopt.Opts) (*snWalletProof, error) {
	proof := &snWalletProof{}
	if seedFile, err := opts.String("--coldkey_seed_file"); err == nil {
		proof.SeedFile = strings.TrimSpace(seedFile)
	}
	if message, err := opts.String("--message"); err == nil {
		proof.Message = message
	}
	if signature, err := opts.String("--signature"); err == nil {
		proof.Signature = strings.TrimSpace(signature)
	}
	if proof.SeedFile != "" && (proof.Message != "" || proof.Signature != "") {
		return nil, errors.New("use either --coldkey_seed_file or --message with --signature, not both")
	}
	if (proof.Message == "") != (proof.Signature == "") {
		return nil, errors.New("--message and --signature go together")
	}
	return proof, nil
}

// snWalletChallenge fetches the single-use challenge for the coldkey: the
// same request the ur.io wallet bridge makes (blockchain TAO, purpose
// "connect", pinned to the address). Public route; sent before the jwt is
// attached, like the app.
func snWalletChallenge(ctx context.Context, api *sdk.Api, coldkeySs58 string) (*sdk.AuthWalletChallengeResult, error) {
	result, err := api.AuthWalletChallengeSyncWithContext(ctx, &sdk.AuthWalletChallengeArgs{
		WalletAddress: coldkeySs58,
		Blockchain:    sdk.TAO,
		Purpose:       snWalletChallengePurpose,
	})
	if err != nil {
		return nil, fmt.Errorf("wallet challenge: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("wallet challenge: %s", result.Error.Message)
	}
	if result.MessageTemplate == "" {
		return nil, errors.New("wallet challenge: the server returned no message to sign")
	}
	return result, nil
}

// snWrapBytes is the polkadot-js signRaw payload wrapper for type "bytes".
func snWrapBytes(message string) []byte {
	return []byte("<Bytes>" + message + "</Bytes>")
}

// snSignWalletChallenge signs the challenge text the way the Polkadot signRaw
// path does: sr25519 in the "substrate" signing context over the
// <Bytes>…</Bytes>-wrapped UTF-8 text. Returns the 0x-prefixed hex of the
// 64-byte signature, the form the server and the ur.io bridge use.
func snSignWalletChallenge(keypair *crv4.Keypair, message string) (string, error) {
	signature, err := keypair.Sign(snWrapBytes(message))
	if err != nil {
		return "", fmt.Errorf("sign wallet challenge: %w", err)
	}
	if len(signature) != 64 {
		return "", fmt.Errorf("sign wallet challenge: %d-byte signature", len(signature))
	}
	return "0x" + hex.EncodeToString(signature), nil
}

// snLoadColdkey loads the coldkey seed (never creating the file) and refuses
// one that does not derive the given address.
func snLoadColdkey(seedFile string, coldkeySs58 string, pubkey [32]byte) (*crv4.Keypair, error) {
	seed, err := crv4.LoadSeedFile(seedFile)
	if err != nil {
		return nil, fmt.Errorf("coldkey seed file %s: %w", seedFile, err)
	}
	keypair, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		return nil, fmt.Errorf("coldkey seed file %s: %w", seedFile, err)
	}
	if keypair.PublicKey() != pubkey {
		return nil, fmt.Errorf("coldkey seed file %s derives %s, not %s", seedFile, keypair.Address(), coldkeySs58)
	}
	return keypair, nil
}

// snNormalizeSignatureHex checks a signature given on the command line (64
// bytes of hex, 0x optional) and returns it 0x-prefixed.
func snNormalizeSignatureHex(signature string) (string, error) {
	clean := strings.TrimSpace(signature)
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "0x"), "0X")
	raw, err := hex.DecodeString(clean)
	if err != nil {
		return "", fmt.Errorf("--signature must be hex: %w", err)
	}
	if len(raw) != 64 {
		return "", fmt.Errorf("--signature must be a 64-byte sr25519 signature, got %d bytes", len(raw))
	}
	return "0x" + hex.EncodeToString(raw), nil
}

// snEscapeMessage is the one-line form of a challenge: a literal \n per line
// break. The challenge grammar (server FormatWalletAuthChallengeMessage:
// fixed words, base64url, digits) has no backslashes, so it round-trips.
func snEscapeMessage(message string) string {
	return strings.ReplaceAll(message, "\n", `\n`)
}

// snUnescapeMessage inverts snEscapeMessage. A message passed with real
// newlines is left as it is.
func snUnescapeMessage(message string) string {
	if strings.Contains(message, "\n") {
		return message
	}
	return strings.ReplaceAll(message, `\n`, "\n")
}

// snWalletSetArgs turns the proof into the `POST /sn/wallet` body, fetching
// and signing the challenge for the seed-file form.
func snWalletSetArgs(ctx context.Context, api *sdk.Api, coldkeySs58 string, pubkey [32]byte, proof *snWalletProof) (*sdk.SnSetWalletArgs, error) {
	args := &sdk.SnSetWalletArgs{ColdkeySs58: coldkeySs58}
	switch {
	case proof != nil && proof.SeedFile != "":
		keypair, err := snLoadColdkey(proof.SeedFile, coldkeySs58, pubkey)
		if err != nil {
			return nil, err
		}
		challenge, err := snWalletChallenge(ctx, api, coldkeySs58)
		if err != nil {
			return nil, err
		}
		signature, err := snSignWalletChallenge(keypair, challenge.MessageTemplate)
		if err != nil {
			return nil, err
		}
		args.Message = challenge.MessageTemplate
		args.Signature = signature
	case proof != nil && (proof.Message != "" || proof.Signature != ""):
		if proof.Message == "" || proof.Signature == "" {
			return nil, errors.New("--message and --signature go together")
		}
		signature, err := snNormalizeSignatureHex(proof.Signature)
		if err != nil {
			return nil, err
		}
		args.Message = snUnescapeMessage(proof.Message)
		args.Signature = signature
	}
	return args, nil
}

// snSetWallet validates the ss58 coldkey locally, proves possession of it
// (snWalletProof) and idempotently sets it as the network's subnet claim
// wallet via the authenticated `POST /sn/wallet` route. Prints the result on
// success. An unsigned set (no proof) is sent as the explicit fallback for a
// deployment with the wallet_allow_unsigned policy, and its refusal explains
// what the main network needs.
func snSetWallet(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string, proof *snWalletProof) error {
	coldkeySs58 = strings.TrimSpace(coldkeySs58)
	pubkey, err := ss58.DecodeWithPrefix(coldkeySs58, ss58.BittensorPrefix)
	if err != nil {
		return fmt.Errorf("invalid ss58 coldkey %q: %s", coldkeySs58, err)
	}
	byJwt, err := readNetworkJwt()
	if err != nil {
		return err
	}
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer func() {
		_ = api.CloseAndWait(context.Background())
	}()
	if proof.unsigned() {
		fmt.Printf("no coldkey proof given (--coldkey_seed_file, or --message with --signature): sending an unsigned wallet set, which the main network refuses\n")
	}
	args, err := snWalletSetArgs(ctx, api, coldkeySs58, pubkey, proof)
	if err != nil {
		return err
	}
	api.SetByJwt(byJwt)
	result, err := api.SnSetWalletSync(args)
	if err != nil {
		return err
	}
	if result.Error != nil {
		if args.Signature == "" {
			return fmt.Errorf("%s\n%s", result.Error.Message, snWalletProofHint)
		}
		return fmt.Errorf("%s", result.Error.Message)
	}
	if args.Signature == "" {
		fmt.Printf("subnet wallet set to %s (pubkey 0x%x), unsigned\n", coldkeySs58, pubkey)
	} else {
		fmt.Printf("subnet wallet set to %s (pubkey 0x%x), proven by the coldkey's signature\n", coldkeySs58, pubkey)
	}
	return nil
}

// snWalletChallengeText is what `provider wallet challenge` prints: the exact
// bytes to sign, how to sign them, and the command that submits the result.
func snWalletChallengeText(coldkeySs58 string, challenge *sdk.AuthWalletChallengeResult) string {
	message := challenge.MessageTemplate
	var b strings.Builder
	fmt.Fprintf(&b, "Sign this message with the coldkey %s: sr25519, \"substrate\" signing context.\n", coldkeySs58)
	fmt.Fprintf(&b, "The bytes to sign are the UTF-8 text between the markers exactly as printed (LF line endings, no trailing newline);\n")
	fmt.Fprintf(&b, "a Polkadot extension signRaw of type \"bytes\", which wraps them in <Bytes>...</Bytes>, is accepted too.\n")
	fmt.Fprintf(&b, "The challenge is single-use, bound to this coldkey, and expires in %d seconds.\n", challenge.ExpiresIn)
	fmt.Fprintf(&b, "\n----- message -----\n%s\n----- end -----\n", message)
	fmt.Fprintf(&b, "message bytes (hex): 0x%s\n", hex.EncodeToString([]byte(message)))
	fmt.Fprintf(&b, "\nThen, within that window, submit the 64-byte signature as hex:\n")
	fmt.Fprintf(&b, "  provider wallet set %s --message='%s' --signature=0x<128 hex chars>\n", coldkeySs58, snEscapeMessage(message))
	return b.String()
}

// snPrintWalletChallenge implements `provider wallet challenge <coldkey_ss58>`.
func snPrintWalletChallenge(ctx context.Context, clientStrategy *connect.ClientStrategy, apiUrl string, coldkeySs58 string, out io.Writer) error {
	coldkeySs58 = strings.TrimSpace(coldkeySs58)
	if _, err := ss58.DecodeWithPrefix(coldkeySs58, ss58.BittensorPrefix); err != nil {
		return fmt.Errorf("invalid ss58 coldkey %q: %s", coldkeySs58, err)
	}
	api := sdk.NewApi(ctx, clientStrategy, apiUrl)
	defer func() {
		_ = api.CloseAndWait(context.Background())
	}()
	challenge, err := snWalletChallenge(ctx, api, coldkeySs58)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, snWalletChallengeText(coldkeySs58, challenge))
	return err
}

// walletCommandContext is the shared setup of the wallet subcommands.
func walletCommandContext(opts docopt.Opts) (string, context.Context, context.CancelFunc, *connect.ClientStrategy) {
	apiUrl, err := resolveApiUrl(opts)
	if err != nil {
		fmt.Printf("network config error: %s\n", err)
		os.Exit(1)
	}

	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(event.Ctx())
	clientStrategy := connect.NewClientStrategyWithDefaults(ctx)
	return apiUrl, ctx, cancel, clientStrategy
}

// walletSet implements `provider wallet set <coldkey_ss58>
// [--coldkey_seed_file=<path> | --message=<text> --signature=<hex>]`.
func walletSet(opts docopt.Opts) {
	proof, err := snWalletProofFromOpts(opts)
	if err != nil {
		fmt.Printf("subnet wallet not set: %s\n", err)
		os.Exit(1)
	}
	apiUrl, ctx, cancel, clientStrategy := walletCommandContext(opts)
	defer cancel()
	defer clientStrategy.Close()

	coldkeySs58, _ := opts.String("<coldkey_ss58>")
	if err := snSetWallet(ctx, clientStrategy, apiUrl, coldkeySs58, proof); err != nil {
		fmt.Printf("subnet wallet not set: %s\n", err)
		os.Exit(1)
	}
}

// walletChallenge implements `provider wallet challenge <coldkey_ss58>`.
func walletChallenge(opts docopt.Opts) {
	apiUrl, ctx, cancel, clientStrategy := walletCommandContext(opts)
	defer cancel()
	defer clientStrategy.Close()

	coldkeySs58, _ := opts.String("<coldkey_ss58>")
	if err := snPrintWalletChallenge(ctx, clientStrategy, apiUrl, coldkeySs58, os.Stdout); err != nil {
		fmt.Printf("subnet wallet challenge not issued: %s\n", err)
		os.Exit(1)
	}
}

// snProvideWalletMisuse names the proof options given to `provide` without
// --wallet (the option grammar cannot tie them together), or "".
func snProvideWalletMisuse(opts docopt.Opts) string {
	if coldkeySs58, err := opts.String("--wallet"); err == nil && coldkeySs58 != "" {
		return ""
	}
	var given []string
	for _, name := range []string{"--coldkey_seed_file", "--message", "--signature"} {
		if value, err := opts.String(name); err == nil && value != "" {
			given = append(given, name)
		}
	}
	if len(given) == 0 {
		return ""
	}
	return fmt.Sprintf("subnet wallet: %s given without --wallet=<coldkey_ss58>; no wallet is set", strings.Join(given, ", "))
}

// provideSetWallet is the `provide --wallet` startup set: validate and
// register the coldkey before providing starts. A failure warns and does not
// block providing — the wallet may already be set from a previous run, and
// the call can be retried any time with `provider wallet set`.
func provideSetWallet(ctx context.Context, apiUrl string, opts docopt.Opts) {
	coldkeySs58, walletErr := opts.String("--wallet")
	if walletErr != nil || coldkeySs58 == "" {
		if misuse := snProvideWalletMisuse(opts); misuse != "" {
			fmt.Printf("%s\n", misuse)
		}
		return
	}
	proof, err := snWalletProofFromOpts(opts)
	if err == nil {
		walletClientStrategy := connect.NewClientStrategyWithDefaults(ctx)
		defer walletClientStrategy.Close()
		err = snSetWallet(ctx, walletClientStrategy, apiUrl, coldkeySs58, proof)
	}
	if err != nil {
		fmt.Printf("subnet wallet not set: %s\n", err)
		fmt.Printf("continuing to provide. Retry with: provider wallet set <coldkey_ss58> --coldkey_seed_file=<path>\n")
	}
}
