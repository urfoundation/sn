package main

// bindhead.go — head-tier client_id <-> hotkey binding (WHITEPAPER §8.4/§11.4,
// decisions D-6/D-18). The stdlib-only `provider bind-head` (connect/provider)
// holds the client key: it signs the on-chain headBindDigest and prints the
// client_id + 64-byte Ed25519 signature. snclaim is the go-ethereum-equipped
// counterpart that packs bindHead(hotkey, clientId, sig) and submits it from
// the EVM key. Because headBindDigest is domain-separated on the registrant
// (msg.sender), snclaim re-derives the digest under ITS OWN sender address and
// verifies the provider's signature locally before spending gas — so a
// signature produced for a different registrant/hotkey/client_id/contract/chain
// is refused here instead of reverting on-chain. Unbind is mirror-gated only
// (no signature).

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/crypto"

	docopt "github.com/docopt/docopt-go"
)

// cmdBindHead implements `snclaim bind-head`: pack bindHead(hotkey, clientId,
// sig) and submit it. The client_id signature is verified against the digest
// the contract will check for this sender before sending.
func cmdBindHead(opts docopt.Opts) error {
	hotkey, err := parseHotkey(strOpt(opts, "--hotkey"))
	if err != nil {
		return err
	}
	clientID, err := parseHex32("--client_id", strOpt(opts, "--client_id"))
	if err != nil {
		return err
	}
	sig, err := parseSig(strOpt(opts, "--sig"))
	if err != nil {
		return err
	}
	contract, err := parseContract(strOpt(opts, "--contract"))
	if err != nil {
		return err
	}
	rpcs := strsOpt(opts, "--rpc")
	if len(rpcs) == 0 {
		return fmt.Errorf("--rpc: at least one endpoint required")
	}

	wantChainID, gasLimit, err := parseChainAndGas(opts)
	if err != nil {
		return err
	}

	key, err := loadKeyFile(strOpt(opts, "--key_file"))
	if err != nil {
		return err
	}
	from := crypto.PubkeyToAddress(key.PublicKey)

	ctx := context.Background()
	client, chainID, rpcURL, err := dialFirst(ctx, rpcs)
	if err != nil {
		return err
	}
	defer client.Close()
	if wantChainID != nil && chainID.Cmp(wantChainID) != 0 {
		return fmt.Errorf("chain id mismatch: --chain_id=%s but %s reports %s", wantChainID, rpcURL, chainID)
	}

	// Re-derive the exact digest the contract will verify for THIS sender, and
	// check the provider's signature over it before spending gas. headBindDigest
	// binds the registrant (msg.sender), so a signature the provider produced
	// for a different registrant/hotkey/client_id/contract/chain cannot pass —
	// catching the mismatch locally with a clear message rather than an opaque
	// on-chain "ST: bad client sig" revert.
	digestRet, err := ethCall(ctx, client, contract, stSubnet.PackHeadBindDigest(from, hotkey, clientID))
	if err != nil {
		return fmt.Errorf("headBindDigest(%s,...): %w", from.Hex(), err)
	}
	digest, err := stSubnet.UnpackHeadBindDigest(digestRet)
	if err != nil {
		return fmt.Errorf("headBindDigest decode: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(clientID[:]), digest[:], sig) {
		return fmt.Errorf(
			"client_id signature does not verify for sender %s.\n"+
				"  The provider must sign for THIS EVM address; re-run:\n"+
				"    provider bind-head --hotkey=0x%x --registrant=%s --contract=%s --rpc=<url>\n"+
				"  A signature for a different registrant, hotkey, client_id, contract, or chain cannot be submitted",
			from.Hex(), hotkey, from.Hex(), contract.Hex())
	}

	calldata, err := stSubnet.TryPackBindHead(hotkey, clientID, sig)
	if err != nil {
		return fmt.Errorf("pack bindHead: %w", err)
	}

	printIntent := func(gasEst uint64, gasErr error) {
		fmt.Printf("bindHead intent\n")
		fmt.Printf("  contract:   %s (chain id %s, rpc %s)\n", contract.Hex(), chainID, rpcURL)
		fmt.Printf("  from:       %s (registrant; mirror must be the hotkey's coldkey)\n", from.Hex())
		fmt.Printf("  hotkey:     0x%x\n", hotkey)
		fmt.Printf("  client_id:  0x%x\n", clientID)
		fmt.Printf("  digest:     0x%x (headBindDigest, client_id sig verified)\n", digest)
		fmt.Printf("  calldata:   %d bytes, selector 0x%x\n", len(calldata), calldata[:4])
		if gasErr == nil {
			fmt.Printf("  gas (est):  %d\n", gasEst)
		} else {
			fmt.Printf("  gas (est):  unavailable (%v); using --gas_limit=%d\n", gasErr, gasLimit)
		}
	}
	receipt, err := runTx(ctx, client, chainID, txRequest{
		contract: contract,
		from:     from,
		key:      key,
		calldata: calldata,
		gasLimit: gasLimit,
		dryRun:   boolOpt(opts, "--dry-run"),
	}, printIntent)
	if err != nil {
		return err
	}
	if receipt == nil {
		return nil // dry run
	}
	decoded := false
	for _, lg := range receipt.Logs {
		if lg.Address != contract {
			continue
		}
		ev, uerr := stSubnet.UnpackHeadBoundEvent(lg)
		if uerr != nil {
			continue
		}
		fmt.Printf("HeadBound: uid %d, registrant %s\n", ev.Uid, ev.Registrant.Hex())
		fmt.Printf("  hotkey:    0x%x\n", ev.Hotkey)
		fmt.Printf("  client_id: 0x%x\n", ev.ClientId)
		decoded = true
	}
	if !decoded {
		fmt.Println("warning: no HeadBound event decoded from the receipt")
	}
	return nil
}

// cmdUnbindHead implements `snclaim unbind-head`: pack and submit
// unbindHead(hotkey). Mirror-gated only — no signature.
func cmdUnbindHead(opts docopt.Opts) error {
	hotkey, err := parseHotkey(strOpt(opts, "--hotkey"))
	if err != nil {
		return err
	}
	contract, err := parseContract(strOpt(opts, "--contract"))
	if err != nil {
		return err
	}
	rpcs := strsOpt(opts, "--rpc")
	if len(rpcs) == 0 {
		return fmt.Errorf("--rpc: at least one endpoint required")
	}

	wantChainID, gasLimit, err := parseChainAndGas(opts)
	if err != nil {
		return err
	}

	key, err := loadKeyFile(strOpt(opts, "--key_file"))
	if err != nil {
		return err
	}
	from := crypto.PubkeyToAddress(key.PublicKey)

	ctx := context.Background()
	client, chainID, rpcURL, err := dialFirst(ctx, rpcs)
	if err != nil {
		return err
	}
	defer client.Close()
	if wantChainID != nil && chainID.Cmp(wantChainID) != 0 {
		return fmt.Errorf("chain id mismatch: --chain_id=%s but %s reports %s", wantChainID, rpcURL, chainID)
	}

	calldata, err := stSubnet.TryPackUnbindHead(hotkey)
	if err != nil {
		return fmt.Errorf("pack unbindHead: %w", err)
	}

	printIntent := func(gasEst uint64, gasErr error) {
		fmt.Printf("unbindHead intent\n")
		fmt.Printf("  contract:   %s (chain id %s, rpc %s)\n", contract.Hex(), chainID, rpcURL)
		fmt.Printf("  from:       %s (mirror must be the hotkey's coldkey)\n", from.Hex())
		fmt.Printf("  hotkey:     0x%x\n", hotkey)
		fmt.Printf("  calldata:   %d bytes, selector 0x%x\n", len(calldata), calldata[:4])
		if gasErr == nil {
			fmt.Printf("  gas (est):  %d\n", gasEst)
		} else {
			fmt.Printf("  gas (est):  unavailable (%v); using --gas_limit=%d\n", gasErr, gasLimit)
		}
	}
	receipt, err := runTx(ctx, client, chainID, txRequest{
		contract: contract,
		from:     from,
		key:      key,
		calldata: calldata,
		gasLimit: gasLimit,
		dryRun:   boolOpt(opts, "--dry-run"),
	}, printIntent)
	if err != nil {
		return err
	}
	if receipt == nil {
		return nil // dry run
	}
	decoded := false
	for _, lg := range receipt.Logs {
		if lg.Address != contract {
			continue
		}
		ev, uerr := stSubnet.UnpackHeadUnboundEvent(lg)
		if uerr != nil {
			continue
		}
		fmt.Printf("HeadUnbound: uid %d, registrant %s\n", ev.Uid, ev.Registrant.Hex())
		fmt.Printf("  hotkey:    0x%x\n", ev.Hotkey)
		fmt.Printf("  client_id: 0x%x\n", ev.ClientId)
		decoded = true
	}
	if !decoded {
		fmt.Println("warning: no HeadUnbound event decoded from the receipt")
	}
	return nil
}

// parseChainAndGas parses the shared optional --chain_id and --gas_limit flags.
func parseChainAndGas(opts docopt.Opts) (wantChainID *big.Int, gasLimit uint64, err error) {
	if s := strOpt(opts, "--chain_id"); s != "" {
		if wantChainID, err = parseBig("--chain_id", s); err != nil {
			return nil, 0, err
		}
	}
	if s := strOpt(opts, "--gas_limit"); s != "" {
		if gasLimit, err = strconv.ParseUint(s, 0, 64); err != nil {
			return nil, 0, fmt.Errorf("--gas_limit: %w", err)
		}
	}
	return wantChainID, gasLimit, nil
}
