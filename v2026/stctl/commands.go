package main

import (
	"context"
	"fmt"
	"math/big"
	"os"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
)

// stringOpt returns a docopt option value, "" when absent.
func stringOpt(opts docopt.Opts, name string) string {
	value, err := opts.String(name)
	if err != nil {
		return ""
	}
	return value
}

// openSession loads the config and dials the first answering rpc endpoint.
func openSession(opts docopt.Opts) (*Config, *session, error) {
	path := configPathFromOpts(opts)
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, nil, err
	}
	s, err := dialSession(cfg)
	if err != nil {
		return nil, nil, err
	}
	return cfg, s, nil
}

// evm-address <h160>

// evmAddressReport renders the substrate mirror of an EVM H160
// (pubkey = blake2b_256("evm:" || h160), ss58 prefix 42). Fund this ss58
// via btcli to fund the H160 on the subtensor EVM (PLAN.md §3.6).
func evmAddressReport(h160 string) (string, error) {
	if !common.IsHexAddress(h160) {
		return "", fmt.Errorf("%q is not a valid 20-byte hex EVM address", h160)
	}
	addr := common.HexToAddress(h160)
	pubkey := ss58.EvmMirrorPubkey(addr)
	address, err := ss58.Encode(pubkey, ss58.BittensorPrefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"h160:          %s\n"+
			"mirror pubkey: 0x%x\n"+
			"mirror ss58:   %s (prefix %d)\n"+
			"\n"+
			"fund it: btcli wallet transfer --destination %s --network test\n",
		addr, pubkey, address, ss58.BittensorPrefix, address,
	), nil
}

func cmdEvmAddress(opts docopt.Opts) error {
	report, err := evmAddressReport(stringOpt(opts, "<h160>"))
	if err != nil {
		return err
	}
	fmt.Print(report)
	return nil
}

// deploy-status

func cmdDeployStatus(opts docopt.Opts) error {
	path := configPathFromOpts(opts)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("config %s does not exist. Example (copy, edit, retry):\n\n%s", path, exampleConfig)
		return nil
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	fmt.Printf("config:            %s\n", path)
	fmt.Printf("chain id:          %d\n", cfg.ChainId)
	fmt.Printf("netuid (config):   %d\n", cfg.Netuid)

	// signer key (optional for reads)
	if cfg.KeyFile == "" {
		fmt.Printf("key_file:          (not set; state-changing commands unavailable)\n")
	} else if key, from, err := loadKey(cfg); err != nil {
		fmt.Printf("key_file:          WARNING: %v\n", err)
	} else {
		_ = key
		fmt.Printf("signer h160:       %s\n", from)
		fmt.Printf("signer mirror:     %s (fund via btcli to fund the signer)\n", mirrorSS58(from))
	}

	s, err := dialSession(cfg)
	if err != nil {
		return err
	}
	defer s.close()
	fmt.Printf("rpc:               %s (chain id %s ok)\n", s.rpcUrl, s.chainId)

	addr, err := cfg.contractAddr()
	if err != nil {
		fmt.Printf("contract:          %v\n", err)
		return nil
	}
	fmt.Printf("contract:          %s\n", addr)
	fmt.Printf("contract mirror:   %s (proxy funding: burnedRegister TAO, pushed stake)\n", mirrorSS58(addr))

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	code, err := s.client.CodeAt(ctx, addr, nil)
	cancel()
	if err != nil {
		return fmt.Errorf("eth_getCode %s: %w", addr, err)
	}
	if len(code) == 0 {
		fmt.Printf("deployed:          NO (no code at address; run the forge deploy script)\n")
		return nil
	}
	fmt.Printf("deployed:          yes (%d bytes of code)\n", len(code))

	st := s.st
	netuid, err := view(s, st.PackNetuid(), st.UnpackNetuid)
	if err != nil {
		return fmt.Errorf("netuid(): %w", err)
	}
	netuidNote := ""
	if cfg.Netuid != 0 && netuid != cfg.Netuid {
		netuidNote = fmt.Sprintf("  WARNING: config says %d", cfg.Netuid)
	}
	fmt.Printf("netuid (chain):    %d%s\n", netuid, netuidNote)

	owner, err := view(s, st.PackOwner(), st.UnpackOwner)
	if err != nil {
		return fmt.Errorf("owner(): %w", err)
	}
	guardian, err := view(s, st.PackGuardian(), st.UnpackGuardian)
	if err != nil {
		return fmt.Errorf("guardian(): %w", err)
	}
	paused, err := view(s, st.PackPaused(), st.UnpackPaused)
	if err != nil {
		return fmt.Errorf("paused(): %w", err)
	}
	treasury, err := view(s, st.PackTreasuryHotkey(), st.UnpackTreasuryHotkey)
	if err != nil {
		return fmt.Errorf("treasuryHotkey(): %w", err)
	}
	reserve, err := view(s, st.PackReserveHotkey(), st.UnpackReserveHotkey)
	if err != nil {
		return fmt.Errorf("reserveHotkey(): %w", err)
	}
	selfColdkey, err := view(s, st.PackSelfColdkey(), st.UnpackSelfColdkey)
	if err != nil {
		return fmt.Errorf("selfColdkey(): %w", err)
	}
	fmt.Printf("owner:             %s\n", owner)
	fmt.Printf("guardian:          %s\n", guardian)
	fmt.Printf("paused:            %t\n", paused)
	fmt.Printf("treasury hotkey:   %s\n", formatKey32(treasury))
	reserveNote := "  (buyback reserve; set once, no setter)"
	if reserve == treasury {
		reserveNote = "  WARNING: == treasury hotkey (initialize forbids this)"
	}
	fmt.Printf("reserve hotkey:    %s%s\n", formatKey32(reserve), reserveNote)
	selfNote := ""
	if selfColdkey != ss58.EvmMirrorPubkey(addr) {
		selfNote = "  WARNING: != mirror(proxy); check setSelfColdkey"
	}
	fmt.Printf("self coldkey:      %s%s\n", formatKey32(selfColdkey), selfNote)

	epoch, err := view(s, st.PackEpoch(), st.UnpackEpoch)
	if err != nil {
		return fmt.Errorf("epoch(): %w", err)
	}
	pending, err := view(s, st.PackPendingEpoch(), st.UnpackPendingEpoch)
	if err != nil {
		return fmt.Errorf("pendingEpoch(): %w", err)
	}
	tEpoch, err := view(s, st.PackTEpoch(), st.UnpackTEpoch)
	if err != nil {
		return fmt.Errorf("tEpoch(): %w", err)
	}
	commitWindow, err := view(s, st.PackCommitWindowBlocks(), st.UnpackCommitWindowBlocks)
	if err != nil {
		return fmt.Errorf("commitWindowBlocks(): %w", err)
	}
	trailsWindow, err := view(s, st.PackTrailsWindowBlocks(), st.UnpackTrailsWindowBlocks)
	if err != nil {
		return fmt.Errorf("trailsWindowBlocks(): %w", err)
	}
	finalizeOffset, err := view(s, st.PackFinalizeOffsetBlocks(), st.UnpackFinalizeOffsetBlocks)
	if err != nil {
		return fmt.Errorf("finalizeOffsetBlocks(): %w", err)
	}
	accounted, err := view(s, st.PackAccountedStake(), st.UnpackAccountedStake)
	if err != nil {
		return fmt.Errorf("accountedStake(): %w", err)
	}
	buybackTotal, err := view(s, st.PackBuybackTotal(), st.UnpackBuybackTotal)
	if err != nil {
		return fmt.Errorf("buybackTotal(): %w", err)
	}
	operatorCount, err := view(s, st.PackOperatorCount(), st.UnpackOperatorCount)
	if err != nil {
		return fmt.Errorf("operatorCount(): %w", err)
	}
	fmt.Printf("epoch:             %s (pending %s)\n", epoch, pending)
	fmt.Printf("epoch params:      tEpoch=%d commitWindow=%d trailsWindow=%d finalizeOffset=%d blocks\n",
		tEpoch, commitWindow, trailsWindow, finalizeOffset)
	fmt.Printf("                   (trails window: reserved dial for the deferred bounty phase)\n")
	fmt.Printf("accounted stake:   %s\n", formatAlpha(accounted))
	fmt.Printf("buyback total:     %s (deposits reserved onto the reserve hotkey)\n", formatAlpha(buybackTotal))
	fmt.Printf("operators:         %s\n", operatorCount)
	return nil
}

// initialize

// mainnetChainId selects the mainnet window-parameter profile; any other
// chain id gets the testnet profile (mirrors evm/script/Deploy.s.sol).
const mainnetChainId = 964

// epochProfile is one Deploy.s.sol window-parameter default set.
type epochProfile struct {
	tEpoch         uint64
	commitWindow   uint64
	trailsWindow   uint64
	finalizeOffset uint64
}

var (
	mainnetProfile = epochProfile{tEpoch: 50_400, commitWindow: 1_200, trailsWindow: 7_200, finalizeOffset: 14_400}
	testnetProfile = epochProfile{tEpoch: 300, commitWindow: 50, trailsWindow: 100, finalizeOffset: 150}
)

// initializeFlags are the raw initialize flag strings ("" = omitted for the
// optional ones: guardian, the window parameters, and selfColdkey).
type initializeFlags struct {
	owner          string
	guardian       string
	treasuryHotkey string
	reserveHotkey  string
	tEpoch         string
	commitWindow   string
	trailsWindow   string
	finalizeOffset string
	selfColdkey    string
}

// initializeArgs are the parsed initialize(...) parameters (everything but
// netuid, which comes from the config).
type initializeArgs struct {
	owner          common.Address
	guardian       common.Address
	treasuryHotkey [32]byte
	reserveHotkey  [32]byte
	tEpoch         uint64
	commitWindow   uint64
	trailsWindow   uint64
	finalizeOffset uint64
	selfColdkey    [32]byte
}

// parseInitializeArgs parses the initialize flags, fills window-parameter
// defaults from the chain profile, and pre-validates what the contract
// requires: nonzero treasury + reserve hotkeys, reserve != treasury
// (dividends compound on the reserve; mixing them onto the escrow would
// break deposit()'s exact push-then-credit attribution — WHITEPAPER §7.4,
// D23), tEpoch >= 1, and commitWindow <= trailsWindow <= finalizeOffset.
func parseInitializeArgs(flags initializeFlags, mainnet bool) (*initializeArgs, error) {
	profile := testnetProfile
	if mainnet {
		profile = mainnetProfile
	}
	args := &initializeArgs{
		tEpoch:         profile.tEpoch,
		commitWindow:   profile.commitWindow,
		trailsWindow:   profile.trailsWindow,
		finalizeOffset: profile.finalizeOffset,
	}
	var err error
	if args.owner, err = parseH160("--owner", flags.owner); err != nil {
		return nil, err
	}
	if flags.guardian != "" {
		if args.guardian, err = parseH160("--guardian", flags.guardian); err != nil {
			return nil, err
		}
	}
	if args.treasuryHotkey, err = parseAccount32("--treasury_hotkey", flags.treasuryHotkey); err != nil {
		return nil, err
	}
	if args.reserveHotkey, err = parseAccount32("--reserve_hotkey", flags.reserveHotkey); err != nil {
		return nil, err
	}
	if flags.tEpoch != "" {
		if args.tEpoch, err = parseUint64("--t_epoch", flags.tEpoch); err != nil {
			return nil, err
		}
	}
	if flags.commitWindow != "" {
		if args.commitWindow, err = parseUint64("--commit_window", flags.commitWindow); err != nil {
			return nil, err
		}
	}
	if flags.trailsWindow != "" {
		if args.trailsWindow, err = parseUint64("--trails_window", flags.trailsWindow); err != nil {
			return nil, err
		}
	}
	if flags.finalizeOffset != "" {
		if args.finalizeOffset, err = parseUint64("--finalize_offset", flags.finalizeOffset); err != nil {
			return nil, err
		}
	}
	if flags.selfColdkey != "" {
		if args.selfColdkey, err = parseAccount32("--self_coldkey", flags.selfColdkey); err != nil {
			return nil, err
		}
	}
	if args.treasuryHotkey == ([32]byte{}) {
		return nil, fmt.Errorf("--treasury_hotkey: must not be the zero key")
	}
	if args.reserveHotkey == ([32]byte{}) {
		return nil, fmt.Errorf("--reserve_hotkey: must not be the zero key")
	}
	if args.reserveHotkey == args.treasuryHotkey {
		return nil, fmt.Errorf("--reserve_hotkey: must differ from --treasury_hotkey " +
			"(reserve dividends compound; the escrow needs exact deposit attribution)")
	}
	if args.tEpoch == 0 {
		return nil, fmt.Errorf("--t_epoch: must be >= 1")
	}
	if args.commitWindow > args.trailsWindow || args.trailsWindow > args.finalizeOffset {
		return nil, fmt.Errorf(
			"window order: need --commit_window (%d) <= --trails_window (%d) <= --finalize_offset (%d)",
			args.commitWindow, args.trailsWindow, args.finalizeOffset,
		)
	}
	return args, nil
}

// initializeCalldata is the flag -> calldata path (golden-tested).
func initializeCalldata(st *stabi.STSubnet, netuid uint16, flags initializeFlags, mainnet bool) ([]byte, error) {
	args, err := parseInitializeArgs(flags, mainnet)
	if err != nil {
		return nil, err
	}
	return st.PackInitialize(netuid, args.owner, args.guardian,
		args.treasuryHotkey, args.reserveHotkey,
		args.tEpoch, args.commitWindow, args.trailsWindow, args.finalizeOffset,
		args.selfColdkey), nil
}

func cmdInitialize(opts docopt.Opts) error {
	cfg, err := loadConfig(configPathFromOpts(opts))
	if err != nil {
		return err
	}
	if cfg.Netuid == 0 {
		return fmt.Errorf("config: netuid must be set (initialize writes it on-chain)")
	}
	args, err := parseInitializeArgs(initializeFlags{
		owner:          stringOpt(opts, "--owner"),
		guardian:       stringOpt(opts, "--guardian"),
		treasuryHotkey: stringOpt(opts, "--treasury_hotkey"),
		reserveHotkey:  stringOpt(opts, "--reserve_hotkey"),
		tEpoch:         stringOpt(opts, "--t_epoch"),
		commitWindow:   stringOpt(opts, "--commit_window"),
		trailsWindow:   stringOpt(opts, "--trails_window"),
		finalizeOffset: stringOpt(opts, "--finalize_offset"),
		selfColdkey:    stringOpt(opts, "--self_coldkey"),
	}, cfg.ChainId == mainnetChainId)
	if err != nil {
		return err
	}
	s, err := dialSession(cfg)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.requireContract(); err != nil {
		return err
	}
	key, _, err := loadKey(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("initialize netuid=%d owner=%s guardian=%s\n", cfg.Netuid, args.owner, args.guardian)
	fmt.Printf("  treasury hotkey: %s\n", formatKey32(args.treasuryHotkey))
	fmt.Printf("  reserve hotkey:  %s\n", formatKey32(args.reserveHotkey))
	fmt.Printf("                   (buyback reserve target, WHITEPAPER §7.4/D23; set once, no setter)\n")
	fmt.Printf("  epoch params:    tEpoch=%d commitWindow=%d trailsWindow=%d finalizeOffset=%d blocks\n",
		args.tEpoch, args.commitWindow, args.trailsWindow, args.finalizeOffset)
	fmt.Printf("                   (trails window: reserved dial for the deferred bounty phase)\n")
	if args.selfColdkey == ([32]byte{}) {
		fmt.Printf("  self coldkey:    (zero: the contract computes mirror(proxy) via blake2f 0x09)\n")
	} else {
		fmt.Printf("  self coldkey:    %s\n", formatKey32(args.selfColdkey))
	}
	fmt.Printf("note: the forge deploy script initializes atomically in the proxy constructor\n")
	fmt.Printf("      (evm/script/Deploy.s.sol); this command completes a proxy that was\n")
	fmt.Printf("      deployed with empty initializer calldata.\n")
	calldata := s.st.PackInitialize(cfg.Netuid, args.owner, args.guardian,
		args.treasuryHotkey, args.reserveHotkey,
		args.tEpoch, args.commitWindow, args.trailsWindow, args.finalizeOffset,
		args.selfColdkey)
	_, err = s.sendContractTx(key, calldata)
	return err
}

// register-operator

func cmdRegisterOperator(opts docopt.Opts) error {
	noId, err := parseUint256("--no_id", stringOpt(opts, "--no_id"))
	if err != nil {
		return err
	}
	coldkey, err := parseAccount32("--coldkey", stringOpt(opts, "--coldkey"))
	if err != nil {
		return err
	}
	minerHotkey, err := parseAccount32("--miner_hotkey", stringOpt(opts, "--miner_hotkey"))
	if err != nil {
		return err
	}
	cfg, s, err := openSession(opts)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.requireContract(); err != nil {
		return err
	}
	key, _, err := loadKey(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("registerOperator noId=%s coldkey=%s minerHotkey=%s\n", noId, formatKey32(coldkey), formatKey32(minerHotkey))
	fmt.Printf("note: the contract burnedRegisters the pool UID; its mirror account\n")
	fmt.Printf("      %s must hold TAO for the burn (fund via btcli).\n", mirrorSS58(s.contractAddr))
	_, err = s.sendContractTx(key, s.st.PackRegisterOperator(noId, coldkey, minerHotkey))
	return err
}

// deposit

func cmdDeposit(opts docopt.Opts) error {
	noId, err := parseUint256("--no_id", stringOpt(opts, "--no_id"))
	if err != nil {
		return err
	}
	alpha, err := parseUint256("--alpha", stringOpt(opts, "--alpha"))
	if err != nil {
		return err
	}
	push, _ := opts.Bool("--push")

	cfg, s, err := openSession(opts)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.requireContract(); err != nil {
		return err
	}
	key, from, err := loadKey(cfg)
	if err != nil {
		return err
	}

	fmt.Printf("deposit noId=%s amount=%s\n", noId, formatAlpha(alpha))
	fmt.Printf("contract mirror (btcli funding target): %s\n", mirrorSS58(s.contractAddr))

	if push {
		// Push-then-credit (evm/README.md deviation 3): move the caller's
		// stake on treasuryHotkey to the contract's mirror coldkey first,
		// then deposit() attributes it. netuid and treasuryHotkey are read
		// from the contract (authoritative), cross-checked against config.
		netuid, err := view(s, s.st.PackNetuid(), s.st.UnpackNetuid)
		if err != nil {
			return fmt.Errorf("netuid(): %w", err)
		}
		if cfg.Netuid != 0 && netuid != cfg.Netuid {
			return fmt.Errorf("contract netuid %d != config netuid %d", netuid, cfg.Netuid)
		}
		treasury, err := view(s, s.st.PackTreasuryHotkey(), s.st.UnpackTreasuryHotkey)
		if err != nil {
			return fmt.Errorf("treasuryHotkey(): %w", err)
		}
		destColdkey := ss58.EvmMirrorPubkey(s.contractAddr)
		netuidBig := new(big.Int).SetUint64(uint64(netuid))
		calldata, err := packTransferStake(destColdkey, treasury, netuidBig, netuidBig, alpha)
		if err != nil {
			return err
		}
		fmt.Printf("push: StakingV2(0x805).transferStake(mirror(proxy), treasuryHotkey=%s, netuid=%d, netuid=%d, %s)\n",
			formatKey32(treasury), netuid, netuid, formatAlpha(alpha))
		fmt.Printf("      (moves the signer mirror %s stake on treasuryHotkey to the contract)\n", mirrorSS58(from))
		if _, err := s.sendRawTx(key, stakingPrecompileAddress, calldata); err != nil {
			return fmt.Errorf("push transferStake: %w", err)
		}
	} else {
		fmt.Printf("(no --push: assumes %s was already pushed to the contract's treasuryHotkey stake)\n", formatAlpha(alpha))
	}

	_, err = s.sendContractTx(key, s.st.PackDeposit(noId, alpha))
	return err
}

// commit-root

func cmdCommitRoot(opts docopt.Opts) error {
	epoch, err := parseUint256("--epoch", stringOpt(opts, "--epoch"))
	if err != nil {
		return err
	}
	noId, err := parseUint256("--no_id", stringOpt(opts, "--no_id"))
	if err != nil {
		return err
	}
	root, err := parseHex32("--root", stringOpt(opts, "--root"))
	if err != nil {
		return err
	}
	off, err := parseHexBytes("--off", stringOpt(opts, "--off"))
	if err != nil {
		return err
	}
	cfg, s, err := openSession(opts)
	if err != nil {
		return err
	}
	defer s.close()
	key, _, err := loadKey(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("commitOperator epoch=%s noId=%s payoutRoot=0x%x off=0x%x\n", epoch, noId, root, off)
	_, err = s.sendContractTx(key, s.st.PackCommitOperator(epoch, noId, root, off))
	return err
}

// finalize

func cmdFinalize(opts docopt.Opts) error {
	epoch, err := parseUint256("--epoch", stringOpt(opts, "--epoch"))
	if err != nil {
		return err
	}
	cfg, s, err := openSession(opts)
	if err != nil {
		return err
	}
	defer s.close()
	key, _, err := loadKey(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("finalizeEpoch epoch=%s\n", epoch)
	_, err = s.sendContractTx(key, s.st.PackFinalizeEpoch(epoch))
	return err
}

// claim-miner

// claimMinerArgs are the parsed claimMiner flag values.
type claimMinerArgs struct {
	epoch    *big.Int
	noId     *big.Int
	coldkey  [32]byte
	shareBps *big.Int
	proof    [][32]byte
}

// parseClaimMinerArgs parses the claim-miner flags.
func parseClaimMinerArgs(epochStr, noIdStr, coldkeyStr, shareBpsStr, proofStr string) (*claimMinerArgs, error) {
	epoch, err := parseUint256("--epoch", epochStr)
	if err != nil {
		return nil, err
	}
	noId, err := parseUint256("--no_id", noIdStr)
	if err != nil {
		return nil, err
	}
	coldkey, err := parseAccount32("--coldkey", coldkeyStr)
	if err != nil {
		return nil, err
	}
	shareBps, err := parseUint256("--share_bps", shareBpsStr)
	if err != nil {
		return nil, err
	}
	if shareBps.Cmp(big.NewInt(10_000)) > 0 {
		return nil, fmt.Errorf("--share_bps: %s exceeds 10000 (100%%)", shareBps)
	}
	proof, err := parseProof("--proof", proofStr)
	if err != nil {
		return nil, err
	}
	return &claimMinerArgs{
		epoch:    epoch,
		noId:     noId,
		coldkey:  coldkey,
		shareBps: shareBps,
		proof:    proof,
	}, nil
}

// claimMinerCalldata is the flag -> calldata path (golden-tested).
func claimMinerCalldata(st *stabi.STSubnet, epochStr, noIdStr, coldkeyStr, shareBpsStr, proofStr string) ([]byte, error) {
	args, err := parseClaimMinerArgs(epochStr, noIdStr, coldkeyStr, shareBpsStr, proofStr)
	if err != nil {
		return nil, err
	}
	return st.PackClaimMiner(args.epoch, args.noId, args.coldkey, args.shareBps, args.proof), nil
}

// depositCalldata is the flag -> calldata path (golden-tested).
func depositCalldata(st *stabi.STSubnet, noIdStr, alphaStr string) ([]byte, error) {
	noId, err := parseUint256("--no_id", noIdStr)
	if err != nil {
		return nil, err
	}
	alpha, err := parseUint256("--alpha", alphaStr)
	if err != nil {
		return nil, err
	}
	return st.PackDeposit(noId, alpha), nil
}

// minerDedupKey mirrors the contract's dedup key
// keccak256(abi.encode(noId, coldkey)) (evm/README.md deviation 11).
func minerDedupKey(noId *big.Int, coldkey [32]byte) [32]byte {
	payload := make([]byte, 64)
	noId.FillBytes(payload[:32])
	copy(payload[32:], coldkey[:])
	var out [32]byte
	copy(out[:], crypto.Keccak256(payload))
	return out
}

func cmdClaimMiner(opts docopt.Opts) error {
	args, err := parseClaimMinerArgs(
		stringOpt(opts, "--epoch"),
		stringOpt(opts, "--no_id"),
		stringOpt(opts, "--coldkey"),
		stringOpt(opts, "--share_bps"),
		stringOpt(opts, "--proof"),
	)
	if err != nil {
		return err
	}
	force, _ := opts.Bool("--force")

	cfg, s, err := openSession(opts)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.requireContract(); err != nil {
		return err
	}
	key, _, err := loadKey(cfg)
	if err != nil {
		return err
	}

	fmt.Printf("claimMiner epoch=%s noId=%s coldkey=%s shareBps=%s proof=%d nodes\n",
		args.epoch, args.noId, formatKey32(args.coldkey), args.shareBps, len(args.proof))

	// Local pre-flight (skippable with --force): finalized epoch, committed
	// root, unclaimed dedup key, and a merkle proof check against the
	// committed root using the shared sn/merkle scheme.
	if !force {
		finalized, err := view(s, s.st.PackFinalized(args.epoch), s.st.UnpackFinalized)
		if err != nil {
			return fmt.Errorf("finalized(): %w", err)
		}
		if !finalized {
			return fmt.Errorf("epoch %s is not finalized yet (run `stctl state --epoch=%s`; --force to send anyway)", args.epoch, args.epoch)
		}
		commit, err := view(s, s.st.PackNoCommit(args.epoch, args.noId), s.st.UnpackNoCommit)
		if err != nil {
			return fmt.Errorf("noCommit(): %w", err)
		}
		if commit.PayoutRoot == ([32]byte{}) {
			return fmt.Errorf("no payout root committed for epoch %s noId %s (--force to send anyway)", args.epoch, args.noId)
		}
		leaf := merkle.PayoutLeaf(args.coldkey, args.shareBps)
		if !merkle.Verify(commit.PayoutRoot, leaf, args.proof) {
			return fmt.Errorf(
				"proof does not verify against committed payoutRoot 0x%x (leaf 0x%x; --force to send anyway)",
				commit.PayoutRoot, leaf,
			)
		}
		claimed, err := view(s, s.st.PackMinerClaimedBy(args.epoch, minerDedupKey(args.noId, args.coldkey)), s.st.UnpackMinerClaimedBy)
		if err != nil {
			return fmt.Errorf("minerClaimedBy(): %w", err)
		}
		if claimed {
			return fmt.Errorf("already claimed for epoch %s noId %s coldkey %s", args.epoch, args.noId, formatKey32(args.coldkey))
		}
		fmt.Printf("pre-flight ok: epoch finalized, proof verifies against 0x%x\n", commit.PayoutRoot)
	}

	calldata := s.st.PackClaimMiner(args.epoch, args.noId, args.coldkey, args.shareBps, args.proof)
	_, err = s.sendContractTx(key, calldata)
	return err
}

// epoch

func cmdEpoch(opts docopt.Opts) error {
	_, s, err := openSession(opts)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.requireContract(); err != nil {
		return err
	}
	st := s.st

	currentBlock, err := s.blockNumber()
	if err != nil {
		return fmt.Errorf("eth_blockNumber: %w", err)
	}
	epoch, err := view(s, st.PackEpoch(), st.UnpackEpoch)
	if err != nil {
		return fmt.Errorf("epoch(): %w", err)
	}
	pending, err := view(s, st.PackPendingEpoch(), st.UnpackPendingEpoch)
	if err != nil {
		return fmt.Errorf("pendingEpoch(): %w", err)
	}
	startBlock, err := view(s, st.PackEpochStartBlock(), st.UnpackEpochStartBlock)
	if err != nil {
		return fmt.Errorf("epochStartBlock(): %w", err)
	}
	tEpoch, err := view(s, st.PackTEpoch(), st.UnpackTEpoch)
	if err != nil {
		return fmt.Errorf("tEpoch(): %w", err)
	}
	commitWindow, err := view(s, st.PackCommitWindowBlocks(), st.UnpackCommitWindowBlocks)
	if err != nil {
		return fmt.Errorf("commitWindowBlocks(): %w", err)
	}
	trailsWindow, err := view(s, st.PackTrailsWindowBlocks(), st.UnpackTrailsWindowBlocks)
	if err != nil {
		return fmt.Errorf("trailsWindowBlocks(): %w", err)
	}
	finalizeOffset, err := view(s, st.PackFinalizeOffsetBlocks(), st.UnpackFinalizeOffsetBlocks)
	if err != nil {
		return fmt.Errorf("finalizeOffsetBlocks(): %w", err)
	}

	intendedClose := startBlock + tEpoch
	fmt.Printf("epoch (rolled):    %s\n", epoch)
	rollNote := ""
	if pending.Cmp(epoch) > 0 {
		rollNote = "  (rollEpochs pending; the next time-gated tx rolls it)"
	}
	fmt.Printf("epoch (pending):   %s%s\n", pending, rollNote)
	fmt.Printf("current block:     %d\n", currentBlock)
	fmt.Printf("epoch start:       block %d\n", startBlock)
	fmt.Printf("intended close:    %s\n", formatBlockETA(currentBlock, intendedClose))
	fmt.Printf("after close(e):    commit until close+%d, trails until close+%d, finalize from close+%d\n",
		commitWindow, trailsWindow, finalizeOffset)
	fmt.Printf("  commit window:   %s\n", formatBlockETA(currentBlock, intendedClose+commitWindow))
	fmt.Printf("  trails window:   %s (reserved for the deferred bounty phase)\n", formatBlockETA(currentBlock, intendedClose+trailsWindow))
	fmt.Printf("  finalize open:   %s\n", formatBlockETA(currentBlock, intendedClose+finalizeOffset))
	return nil
}

// state

func cmdState(opts docopt.Opts) error {
	_, s, err := openSession(opts)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.requireContract(); err != nil {
		return err
	}
	st := s.st

	currentEpoch, err := view(s, st.PackEpoch(), st.UnpackEpoch)
	if err != nil {
		return fmt.Errorf("epoch(): %w", err)
	}
	epoch := currentEpoch
	if epochStr := stringOpt(opts, "--epoch"); epochStr != "" {
		if epoch, err = parseUint256("--epoch", epochStr); err != nil {
			return err
		}
	}
	currentBlock, err := s.blockNumber()
	if err != nil {
		return fmt.Errorf("eth_blockNumber: %w", err)
	}

	finalized, err := view(s, st.PackFinalized(epoch), st.UnpackFinalized)
	if err != nil {
		return fmt.Errorf("finalized(): %w", err)
	}
	closeBlock, err := view(s, st.PackEpochCloseBlock(epoch), st.UnpackEpochCloseBlock)
	if err != nil {
		return fmt.Errorf("epochCloseBlock(): %w", err)
	}
	buybackTotal, err := view(s, st.PackBuybackTotal(), st.UnpackBuybackTotal)
	if err != nil {
		return fmt.Errorf("buybackTotal(): %w", err)
	}
	reserve, err := view(s, st.PackReserveHotkey(), st.UnpackReserveHotkey)
	if err != nil {
		return fmt.Errorf("reserveHotkey(): %w", err)
	}

	fmt.Printf("epoch:            %s (current rolled epoch %s)\n", epoch, currentEpoch)
	fmt.Printf("current block:    %d\n", currentBlock)
	fmt.Printf("finalized:        %t\n", finalized)

	// close(e): actual once rolled, projected for the current epoch.
	closeKnown := true
	closeNote := ""
	if closeBlock == 0 {
		if epoch.Cmp(currentEpoch) == 0 {
			startBlock, err := view(s, st.PackEpochStartBlock(), st.UnpackEpochStartBlock)
			if err != nil {
				return fmt.Errorf("epochStartBlock(): %w", err)
			}
			tEpoch, err := view(s, st.PackTEpoch(), st.UnpackTEpoch)
			if err != nil {
				return fmt.Errorf("tEpoch(): %w", err)
			}
			closeBlock = startBlock + tEpoch
			closeNote = " (projected; epoch not rolled yet)"
		} else {
			closeKnown = false
		}
	}
	if closeKnown {
		commitWindow, err := view(s, st.PackCommitWindowBlocks(), st.UnpackCommitWindowBlocks)
		if err != nil {
			return fmt.Errorf("commitWindowBlocks(): %w", err)
		}
		trailsWindow, err := view(s, st.PackTrailsWindowBlocks(), st.UnpackTrailsWindowBlocks)
		if err != nil {
			return fmt.Errorf("trailsWindowBlocks(): %w", err)
		}
		finalizeOffset, err := view(s, st.PackFinalizeOffsetBlocks(), st.UnpackFinalizeOffsetBlocks)
		if err != nil {
			return fmt.Errorf("finalizeOffsetBlocks(): %w", err)
		}
		fmt.Printf("close(e):         %s%s\n", formatBlockETA(currentBlock, closeBlock), closeNote)
		fmt.Printf("  commit until:   %s\n", formatBlockETA(currentBlock, closeBlock+commitWindow))
		fmt.Printf("  trails until:   %s (reserved for the deferred bounty phase)\n", formatBlockETA(currentBlock, closeBlock+trailsWindow))
		fmt.Printf("  finalize from:  %s\n", formatBlockETA(currentBlock, closeBlock+finalizeOffset))
	} else {
		fmt.Printf("close(e):         (unknown; epoch is ahead of the roll)\n")
	}
	// per-NO / total deposits are no longer an on-chain ledger (D25) — sum the
	// Deposited event log for per-NO deposits; buybackTotal is the cumulative aggregate.
	fmt.Printf("buyback total:    %s (cumulative deposits; staked on the reserve hotkey)\n", formatAlpha(buybackTotal))
	fmt.Printf("reserve hotkey:   %s\n", formatKey32(reserve))

	// per-NO detail
	var noIds []*big.Int
	if noIdStr := stringOpt(opts, "--no_id"); noIdStr != "" {
		noId, err := parseUint256("--no_id", noIdStr)
		if err != nil {
			return err
		}
		noIds = append(noIds, noId)
	} else {
		count, err := view(s, st.PackOperatorCount(), st.UnpackOperatorCount)
		if err != nil {
			return fmt.Errorf("operatorCount(): %w", err)
		}
		n := count.Int64()
		const maxOperators = 256
		if n > maxOperators {
			fmt.Printf("(showing first %d of %s operators)\n", maxOperators, count)
			n = maxOperators
		}
		for i := int64(0); i < n; i++ {
			noId, err := view(s, st.PackOperatorIds(big.NewInt(i)), st.UnpackOperatorIds)
			if err != nil {
				return fmt.Errorf("operatorIds(%d): %w", i, err)
			}
			noIds = append(noIds, noId)
		}
	}
	for _, noId := range noIds {
		if err := printOperatorState(s, epoch, noId); err != nil {
			return err
		}
	}
	return nil
}

func printOperatorState(s *session, epoch, noId *big.Int) error {
	st := s.st
	operator, err := view(s, st.PackOperators(noId), st.UnpackOperators)
	if err != nil {
		return fmt.Errorf("operators(%s): %w", noId, err)
	}
	poolEmission, err := view(s, st.PackPoolEmission(epoch, noId), st.UnpackPoolEmission)
	if err != nil {
		return fmt.Errorf("poolEmission(%s,%s): %w", epoch, noId, err)
	}
	poolTotal, err := view(s, st.PackPoolTotal(epoch, noId), st.UnpackPoolTotal)
	if err != nil {
		return fmt.Errorf("poolTotal(%s,%s): %w", epoch, noId, err)
	}
	claimedMiner, err := view(s, st.PackClaimedMiner(epoch, noId), st.UnpackClaimedMiner)
	if err != nil {
		return fmt.Errorf("claimedMiner(%s,%s): %w", epoch, noId, err)
	}
	carry, err := view(s, st.PackCarry(noId), st.UnpackCarry)
	if err != nil {
		return fmt.Errorf("carry(%s): %w", noId, err)
	}
	commit, err := view(s, st.PackNoCommit(epoch, noId), st.UnpackNoCommit)
	if err != nil {
		return fmt.Errorf("noCommit(%s,%s): %w", epoch, noId, err)
	}

	fmt.Printf("operator %s:\n", noId)
	fmt.Printf("  coldkey:        %s\n", formatKey32(operator.Coldkey))
	fmt.Printf("  miner uid:      %d  hotkey %s  active %t\n", operator.MinerUid, formatKey32(operator.MinerHotkey), operator.Active)
	// per-NO deposits: no on-chain ledger (D25) — sum the Deposited event log.
	fmt.Printf("  pool emission:  %s\n", formatAlpha(poolEmission))
	fmt.Printf("  pool total:     %s\n", formatAlpha(poolTotal))
	fmt.Printf("  claimed miner:  %s\n", formatAlpha(claimedMiner))
	fmt.Printf("  carry:          %s\n", formatAlpha(carry))
	if commit.PayoutRoot == ([32]byte{}) {
		fmt.Printf("  commit:         (none)\n")
	} else {
		fmt.Printf("  commit root:    0x%x\n", commit.PayoutRoot)
		fmt.Printf("  commit off:     0x%x\n", commit.Off)
	}
	return nil
}
