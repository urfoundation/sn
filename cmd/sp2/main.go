// sp2 is the SP-2 CRv4 conformance harness: Go-native Bittensor
// commit-reveal v4 (drand timelock) weight submission checks against a live
// subtensor chain, plus offline payload/round/ciphertext tooling.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urnetwork/sn/crv4"
)

const usage = `sp2 - CRv4 (commit-reveal v4) conformance harness for the UR subnet.

check-metadata verifies the live chain exposes the commit extrinsics, signed
extensions and storage this package assumes. round computes the drand reveal
round from the epoch schedule (from flags, or live from --substrate). encrypt
produces a chain-format timelock ciphertext. commit performs a full CRv4
weight commit signed by the hotkey seed (use --dry-run to encode without
submitting).

Usage:
    sp2 check-metadata --substrate=<ws>
    sp2 round --tempo=<n> --block=<n> [--last-epoch-block=<n>] [--pending-epoch-at=<n>]
        [--epoch-index=<n>] [--blocks-since-last-step=<n>] [--reveal-period-epochs=<n>]
        [--block-time=<secs>] [--now=<unix>]
    sp2 round --substrate=<ws> --netuid=<id> [--reveal-period-epochs=<n>] [--block-time=<secs>]
    sp2 encrypt --uids=<csv> --values=<csv> --round=<n> [--hotkey=<hex32>] [--version-key=<n>]
    sp2 decrypt --ciphertext=<hex> --signature=<hex>
    sp2 keygen [--seed-file=<path>]
    sp2 address --seed-file=<path> [--prefix=<n>]
    sp2 commit --substrate=<ws> --netuid=<id> --seed-file=<path> --uids=<csv> --values=<csv>
        [--version-key=<n>] [--reveal-period-epochs=<n>] [--block-time=<secs>] [--mecid=<m>] [--dry-run]
    sp2 -h | --help

Options:
    --substrate=<ws>              Substrate websocket url, e.g. wss://test.finney.opentensor.ai:443
    --netuid=<id>                 Subnet netuid.
    --tempo=<n>                   Subnet tempo in blocks.
    --block=<n>                   Current (head) block number.
    --last-epoch-block=<n>        LastEpochBlock storage value [default: 0].
    --pending-epoch-at=<n>        PendingEpochAt storage value [default: 0].
    --epoch-index=<n>             SubnetEpochIndex storage value [default: 0].
    --blocks-since-last-step=<n>  BlocksSinceLastStep storage value [default: 0].
    --reveal-period-epochs=<n>    RevealPeriodEpochs hyperparameter (read live when omitted).
    --block-time=<secs>           Block time in seconds [default: 12.0].
    --now=<unix>                  Fixed wall clock (unix seconds) for reproducible output.
    --uids=<csv>                  Comma-separated u16 uids.
    --values=<csv>                Comma-separated u16 weight values.
    --round=<n>                   Drand quicknet round to encrypt to.
    --hotkey=<hex32>              32-byte hotkey public key hex for the payload [default: zeros].
    --version-key=<n>             Weights version key [default: 0].
    --seed-file=<path>            32-byte sr25519 hotkey seed file (hex or raw).
    --prefix=<n>                  ss58 prefix [default: 42].
    --mecid=<m>                   Mechanism id; uses commit_timelocked_mechanism_weights when set.
    --dry-run                     Build and sign the extrinsic but do not submit.
`

func main() {
	opts, err := docopt.ParseDoc(usage)
	if err != nil {
		fail("%v", err)
	}
	switch {
	case boolOpt(opts, "check-metadata"):
		runCheckMetadata(opts)
	case boolOpt(opts, "round"):
		runRound(opts)
	case boolOpt(opts, "encrypt"):
		runEncrypt(opts)
	case boolOpt(opts, "decrypt"):
		runDecrypt(opts)
	case boolOpt(opts, "keygen"):
		runKeygen(opts)
	case boolOpt(opts, "address"):
		runAddress(opts)
	case boolOpt(opts, "commit"):
		runCommit(opts)
	default:
		fail("no command")
	}
}

func runCheckMetadata(opts docopt.Opts) {
	chain := dial(opts)
	report, err := chain.CheckMetadata()
	if err != nil {
		fail("check metadata: %v", err)
	}
	fmt.Printf("runtime: %s spec_version=%d transaction_version=%d\n", report.SpecName, report.SpecVersion, report.TransactionVersion)
	fmt.Printf("pallet %s index=%d\n", crv4.PalletName, report.PalletIndex)
	printCall := func(name string, r crv4.CallReport) {
		if !r.Found {
			fmt.Printf("  call %s: NOT FOUND\n", name)
			return
		}
		fmt.Printf("  call %s: call_index=%d\n", name, r.CallIndex)
		for _, a := range r.Args {
			fmt.Printf("    %-24s type=%-16s shape=%s\n", a.Name, a.TypeName, a.Shape)
		}
	}
	printCall(crv4.CallCommitTimelocked, report.CommitTimelocked)
	printCall(crv4.CallCommitTimelockedMech, report.CommitMechanism)
	if report.LegacyCrv3Present {
		fmt.Println("  legacy commit_crv3_weights: still present")
	} else {
		fmt.Println("  legacy commit_crv3_weights: absent (expected)")
	}
	fmt.Println("signed extensions:")
	for _, e := range report.Extensions {
		status := "ok"
		if !e.Handled {
			if e.ZeroSize {
				status = "unhandled (zero-size, safe)"
			} else {
				status = "UNSUPPORTED"
			}
		}
		fmt.Printf("  %-32s type=%-32s %s\n", e.Identifier, e.TypeName, status)
	}
	fmt.Println("storage items:")
	for _, name := range []string{"Tempo", "LastEpochBlock", "PendingEpochAt", "SubnetEpochIndex", "BlocksSinceLastStep", "RevealPeriodEpochs", "CommitRevealWeightsEnabled", "CommitRevealWeightsVersion", "MaxWeightsLimit", "WeightsVersionKey"} {
		fmt.Printf("  %-28s found=%v\n", name, report.StorageFound[name])
	}
	if v, err := chain.CommitRevealVersion(); err == nil {
		fmt.Printf("CommitRevealWeightsVersion (live) = %d (package submits %d)\n", v, crv4.CommitRevealVersion4)
	}
	if len(report.Problems) == 0 {
		fmt.Println("RESULT: conformant")
		return
	}
	fmt.Println("RESULT: PROBLEMS")
	for _, p := range report.Problems {
		fmt.Println("  - " + p)
	}
	os.Exit(1)
}

func runRound(opts docopt.Opts) {
	blockTime := floatOpt(opts, "--block-time", 12.0)
	rpe := uint64Opt(opts, "--reveal-period-epochs", 1)

	var state *crv4.EpochScheduleState
	if stringOpt(opts, "--substrate") != "" {
		chain := dial(opts)
		netuid := uint16(uint64Opt(opts, "--netuid", 0))
		s, err := chain.EpochScheduleState(netuid)
		if err != nil {
			fail("epoch schedule state: %v", err)
		}
		state = s
		if !optProvided(opts, "--reveal-period-epochs") {
			if v, err := chain.RevealPeriodEpochs(netuid); err == nil {
				rpe = v
			}
		}
	} else {
		state = &crv4.EpochScheduleState{
			LastEpochBlock:      uint64Opt(opts, "--last-epoch-block", 0),
			PendingEpochAt:      uint64Opt(opts, "--pending-epoch-at", 0),
			SubnetEpochIndex:    uint64Opt(opts, "--epoch-index", 0),
			Tempo:               uint16(uint64Opt(opts, "--tempo", 0)),
			BlocksSinceLastStep: uint64Opt(opts, "--blocks-since-last-step", 0),
			CurrentBlock:        uint64Opt(opts, "--block", 0),
		}
	}

	now := time.Now()
	if optProvided(opts, "--now") {
		now = time.Unix(int64(uint64Opt(opts, "--now", 0)), 0)
	}

	round, revealBlock, err := crv4.RevealRound(now, state, rpe, blockTime)
	if err != nil {
		fail("reveal round: %v", err)
	}
	fmt.Printf("state: %+v\n", *state)
	fmt.Printf("reveal_period_epochs=%d block_time=%v now=%d\n", rpe, blockTime, now.Unix())
	fmt.Printf("reveal_block=%d\nreveal_round=%d\ncurrent_drand_round=%d\n", revealBlock, round, crv4.CurrentDrandRound(now))
}

func runEncrypt(opts docopt.Opts) {
	payload := buildPayload(opts)
	enc, err := payload.Encode()
	if err != nil {
		fail("encode payload: %v", err)
	}
	round := uint64Opt(opts, "--round", 0)
	ct, err := crv4.Encrypt(enc, round)
	if err != nil {
		fail("encrypt: %v", err)
	}
	fmt.Printf("payload_scale=%s\n", hex.EncodeToString(enc))
	fmt.Printf("reveal_round=%d\nciphertext_len=%d\nciphertext=%s\n", round, len(ct), hex.EncodeToString(ct))
}

func runDecrypt(opts docopt.Opts) {
	ct, err := hex.DecodeString(strings.TrimPrefix(stringOpt(opts, "--ciphertext"), "0x"))
	if err != nil {
		fail("bad ciphertext hex: %v", err)
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(stringOpt(opts, "--signature"), "0x"))
	if err != nil {
		fail("bad signature hex: %v", err)
	}
	pt, err := crv4.Decrypt(ct, sig)
	if err != nil {
		fail("decrypt: %v", err)
	}
	fmt.Printf("plaintext=%s\n", hex.EncodeToString(pt))
}

func runKeygen(opts docopt.Opts) {
	path := stringOpt(opts, "--seed-file")
	if path == "" {
		path = "hotkey.seed"
	}
	seed, created, err := crv4.LoadOrCreateSeedFile(path)
	if err != nil {
		fail("keygen: %v", err)
	}
	kp, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		fail("keygen: %v", err)
	}
	if created {
		fmt.Printf("generated new seed at %s\n", path)
	} else {
		fmt.Printf("seed already exists at %s\n", path)
	}
	pub := kp.PublicKey()
	fmt.Printf("public_key=0x%s\nss58_address=%s\n", hex.EncodeToString(pub[:]), kp.Address())
}

func runAddress(opts docopt.Opts) {
	seed, err := crv4.LoadSeedFile(stringOpt(opts, "--seed-file"))
	if err != nil {
		fail("load seed: %v", err)
	}
	kp, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		fail("keypair: %v", err)
	}
	prefix := uint16(uint64Opt(opts, "--prefix", 42))
	pub := kp.PublicKey()
	fmt.Printf("public_key=0x%s\nss58_address=%s\n", hex.EncodeToString(pub[:]), kp.SS58(prefix))
}

func runCommit(opts docopt.Opts) {
	chain := dial(opts)
	netuid := uint16(uint64Opt(opts, "--netuid", 0))
	seed, err := crv4.LoadSeedFile(stringOpt(opts, "--seed-file"))
	if err != nil {
		fail("load seed: %v", err)
	}
	kp, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		fail("keypair: %v", err)
	}

	uids := parseU16CSV(opts, "--uids")
	values := parseU16CSV(opts, "--values")

	enabled, err := chain.CommitRevealEnabled(netuid)
	if err != nil {
		fail("read CommitRevealWeightsEnabled: %v", err)
	}
	if !enabled {
		fail("commit-reveal is disabled on netuid %d", netuid)
	}
	version, err := chain.CommitRevealVersion()
	if err != nil {
		fail("read CommitRevealWeightsVersion: %v", err)
	}
	rpe := uint64Opt(opts, "--reveal-period-epochs", 0)
	if rpe == 0 {
		if rpe, err = chain.RevealPeriodEpochs(netuid); err != nil {
			fail("read RevealPeriodEpochs: %v", err)
		}
	}
	state, err := chain.EpochScheduleState(netuid)
	if err != nil {
		fail("epoch schedule state: %v", err)
	}
	blockTime := floatOpt(opts, "--block-time", 12.0)
	round, revealBlock, err := crv4.RevealRound(time.Now(), state, rpe, blockTime)
	if err != nil {
		fail("reveal round: %v", err)
	}

	payload := &crv4.Payload{
		Hotkey:     kp.PublicKey(),
		Uids:       uids,
		Values:     values,
		VersionKey: uint64Opt(opts, "--version-key", 0),
	}
	enc, err := payload.Encode()
	if err != nil {
		fail("encode payload: %v", err)
	}
	ct, err := crv4.Encrypt(enc, round)
	if err != nil {
		fail("encrypt: %v", err)
	}

	var mecid *uint8
	if optProvided(opts, "--mecid") {
		m := uint8(uint64Opt(opts, "--mecid", 0))
		mecid = &m
	}

	fmt.Printf("hotkey=%s\nnetuid=%d commit_reveal_version=%d reveal_period_epochs=%d\n", kp.Address(), netuid, version, rpe)
	fmt.Printf("reveal_block=%d reveal_round=%d ciphertext_len=%d\n", revealBlock, round, len(ct))

	if boolOpt(opts, "--dry-run") {
		nonce, err := chain.AccountNonce(kp.Address())
		if err != nil {
			fail("account nonce: %v", err)
		}
		ext, err := chain.NewCommitExtrinsic(kp, netuid, mecid, ct, round, version, nonce)
		if err != nil {
			fail("build extrinsic: %v", err)
		}
		hexEnc, err := crv4.EncodeExtrinsic(ext)
		if err != nil {
			fail("encode extrinsic: %v", err)
		}
		fmt.Printf("dry_run_extrinsic=%s\n", hexEnc)
		return
	}

	txHash, err := chain.Commit(context.Background(), kp, netuid, mecid, ct, round, version)
	if err != nil {
		fail("commit: %v", err)
	}
	fmt.Printf("tx_hash=%s\n", txHash.Hex())
}

// --- option helpers ---

func dial(opts docopt.Opts) *crv4.Chain {
	url := stringOpt(opts, "--substrate")
	if url == "" {
		fail("--substrate is required")
	}
	chain, err := crv4.DialChain(url)
	if err != nil {
		fail("%v", err)
	}
	return chain
}

func buildPayload(opts docopt.Opts) *crv4.Payload {
	p := &crv4.Payload{
		Uids:       parseU16CSV(opts, "--uids"),
		Values:     parseU16CSV(opts, "--values"),
		VersionKey: uint64Opt(opts, "--version-key", 0),
	}
	if hk := stringOpt(opts, "--hotkey"); hk != "" && hk != "zeros" {
		b, err := hex.DecodeString(strings.TrimPrefix(hk, "0x"))
		if err != nil || len(b) != 32 {
			fail("--hotkey must be 32 bytes of hex")
		}
		copy(p.Hotkey[:], b)
	}
	return p
}

func parseU16CSV(opts docopt.Opts, key string) []uint16 {
	s := stringOpt(opts, key)
	if s == "" {
		return nil
	}
	var out []uint16
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			fail("bad %s entry %q: %v", key, part, err)
		}
		out = append(out, uint16(v))
	}
	return out
}

func boolOpt(opts docopt.Opts, key string) bool {
	v, _ := opts.Bool(key)
	return v
}

func stringOpt(opts docopt.Opts, key string) string {
	if opts[key] == nil {
		return ""
	}
	v, _ := opts.String(key)
	return v
}

func optProvided(opts docopt.Opts, key string) bool {
	return opts[key] != nil
}

func uint64Opt(opts docopt.Opts, key string, def uint64) uint64 {
	s := stringOpt(opts, key)
	if s == "" {
		return def
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		fail("bad %s: %v", key, err)
	}
	return v
}

func floatOpt(opts docopt.Opts, key string, def float64) float64 {
	s := stringOpt(opts, key)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		fail("bad %s: %v", key, err)
	}
	return v
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "sp2: "+format+"\n", args...)
	os.Exit(1)
}
