package main

// adversary_runtime_probe.go performs narrow, read-only native-runtime
// observations required by the adversarial matrix. The reads are bound to one
// common finalized hash and to the reviewed metadata artifact before a storage
// key is constructed, so an endpoint cannot satisfy the probe with latest-head
// or untyped configuration data.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// Captures one endpoint's exact finalized commit/reveal configuration.
type adversaryCommitRevealObservation struct {
	Endpoint      string
	Finalized     uint64
	FinalizedHash types.Hash
	Enabled       bool
	Tempo         uint16
	RevealPeriods uint64
}

// Allows deterministic tests to replace the two-endpoint runtime observation.
type adversaryCommitRevealProbe func(context.Context, *ResolvedConfig) (adversaryCommitRevealObservation, adversaryCommitRevealObservation, uint64, error)

// Opens one chain only after matching its genesis to the release configuration.
func adversaryDialRuntimeChain(cfg *ResolvedConfig, endpoint string) (*crv4.Chain, error) {
	if cfg == nil || cfg.Public == nil || endpoint == "" {
		return nil, errors.New("commit/reveal runtime probe is missing a release endpoint")
	}
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(chain.GenesisHash.Hex(), cfg.Public.Chain.GenesisHash) {
		chain.API.Client.Close()
		return nil, fmt.Errorf("runtime probe genesis %s, want %s", chain.GenesisHash.Hex(), cfg.Public.Chain.GenesisHash)
	}
	return chain, nil
}

// Reads one finalized block number and hash from an authenticated chain.
func adversaryFinalizedNumber(chain *crv4.Chain) (uint64, types.Hash, error) {
	if chain == nil || chain.API == nil || chain.API.RPC == nil || chain.API.RPC.Chain == nil {
		return 0, types.Hash{}, errors.New("runtime probe chain is unavailable")
	}
	hash, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return 0, types.Hash{}, err
	}
	header, err := chain.API.RPC.Chain.GetHeader(hash)
	if err != nil {
		return 0, types.Hash{}, err
	}
	return uint64(header.Number), hash, nil
}

// Reads commit/reveal activation and timing from one exact historical runtime.
func adversaryCommitRevealAt(ctx context.Context, cfg *ResolvedConfig, endpoint string, chain *crv4.Chain, number uint64, hash types.Hash) (adversaryCommitRevealObservation, error) {
	result := adversaryCommitRevealObservation{Endpoint: endpoint, Finalized: number, FinalizedHash: hash}
	if ctx == nil || hash == (types.Hash{}) {
		return result, errors.New("commit/reveal runtime probe has no finalized hash")
	}
	authenticated, err := readAuthenticatedRuntimeMetadataAtContext(ctx, chain, cfg, hash)
	if err != nil {
		return result, fmt.Errorf("authenticate runtime at %s: %w", hash.Hex(), err)
	}
	historical := *chain
	bindAuthenticatedRuntime(&historical, authenticated)
	read := func(storage string, value any) error {
		key, keyErr := types.CreateStorageKey(historical.Meta, crv4.PalletName, storage, netuidArg(cfg.Netuid))
		if keyErr != nil {
			return keyErr
		}
		present, readErr := readStorageAt(&historical, key, crv4.PalletName, storage, value, hash)
		if readErr != nil {
			return readErr
		}
		if !present {
			return fmt.Errorf("%s is absent", storage)
		}
		return nil
	}
	var enabled types.Bool
	if err := read("CommitRevealWeightsEnabled", &enabled); err != nil {
		return result, err
	}
	var tempo types.U16
	if err := read("Tempo", &tempo); err != nil {
		return result, err
	}
	var revealPeriods types.U64
	if err := read("RevealPeriodEpochs", &revealPeriods); err != nil {
		return result, err
	}
	result.Enabled, result.Tempo, result.RevealPeriods = bool(enabled), uint16(tempo), uint64(revealPeriods)
	return result, nil
}

// Validates both direct finalized observations and returns the exact
// commit/reveal delay in blocks. A false flag is a failure even when both
// providers agree: this is a runtime-state proof, never a catalog declaration.
func validateAdversaryCommitRevealObservations(left, right adversaryCommitRevealObservation) (uint64, error) {
	if left.Endpoint == "" || right.Endpoint == "" || left.Finalized == 0 || right.Finalized == 0 || left.FinalizedHash == (types.Hash{}) || right.FinalizedHash == (types.Hash{}) {
		return 0, errors.New("commit/reveal runtime observation is malformed")
	}
	if left.Finalized != right.Finalized || left.FinalizedHash != right.FinalizedHash {
		return 0, fmt.Errorf("commit/reveal operational/public finalized checkpoints disagree %d/%s versus %d/%s", left.Finalized, left.FinalizedHash.Hex(), right.Finalized, right.FinalizedHash.Hex())
	}
	if left.Enabled != right.Enabled {
		return 0, fmt.Errorf("commit/reveal operational/public enabled state disagrees %t/%t", left.Enabled, right.Enabled)
	}
	if !left.Enabled {
		return 0, errors.New("commit/reveal is disabled at the common finalized checkpoint")
	}
	if left.Tempo == 0 || right.Tempo == 0 || left.RevealPeriods == 0 || right.RevealPeriods == 0 {
		return 0, errors.New("commit/reveal tempo or reveal period is zero")
	}
	if left.Tempo != right.Tempo || left.RevealPeriods != right.RevealPeriods {
		return 0, fmt.Errorf("commit/reveal operational/public schedule disagrees tempo=%d/%d periods=%d/%d", left.Tempo, right.Tempo, left.RevealPeriods, right.RevealPeriods)
	}
	delay, ok := checkedMul(uint64(left.Tempo), left.RevealPeriods)
	if !ok || delay == 0 {
		return 0, errors.New("commit/reveal delay overflows or is zero")
	}
	return delay, nil
}

// Observes both configured native endpoints at one shared finalized height.
// It intentionally uses the operational and public Substrate URLs, not their
// EVM aliases, because CommitRevealWeightsEnabled is FRAME storage.
func observeAdversaryCommitRevealRuntime(ctx context.Context, cfg *ResolvedConfig) (adversaryCommitRevealObservation, adversaryCommitRevealObservation, uint64, error) {
	if ctx == nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, errors.New("commit/reveal runtime probe context is nil")
	}
	if err := ctx.Err(); err != nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, err
	}
	operational, err := adversaryDialRuntimeChain(cfg, cfg.OperationalSubstrate)
	if err != nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, fmt.Errorf("operational substrate: %w", err)
	}
	defer operational.API.Client.Close()
	public, err := adversaryDialRuntimeChain(cfg, cfg.Public.Chain.SubstratePublicReadEndpoint)
	if err != nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, fmt.Errorf("public substrate: %w", err)
	}
	defer public.API.Client.Close()
	operationalNumber, _, err := adversaryFinalizedNumber(operational)
	if err != nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, fmt.Errorf("operational finalized head: %w", err)
	}
	publicNumber, _, err := adversaryFinalizedNumber(public)
	if err != nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, fmt.Errorf("public finalized head: %w", err)
	}
	common := operationalNumber
	if publicNumber < common {
		common = publicNumber
	}
	if common == 0 {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, errors.New("commit/reveal common finalized height is zero")
	}
	operationalHash, err := operational.API.RPC.Chain.GetBlockHash(common)
	if err != nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, fmt.Errorf("operational common finalized hash: %w", err)
	}
	publicHash, err := public.API.RPC.Chain.GetBlockHash(common)
	if err != nil {
		return adversaryCommitRevealObservation{}, adversaryCommitRevealObservation{}, 0, fmt.Errorf("public common finalized hash: %w", err)
	}
	left, err := adversaryCommitRevealAt(ctx, cfg, cfg.OperationalSubstrate, operational, common, operationalHash)
	if err != nil {
		return left, adversaryCommitRevealObservation{}, 0, fmt.Errorf("operational commit/reveal storage: %w", err)
	}
	right, err := adversaryCommitRevealAt(ctx, cfg, cfg.Public.Chain.SubstratePublicReadEndpoint, public, common, publicHash)
	if err != nil {
		return left, right, 0, fmt.Errorf("public commit/reveal storage: %w", err)
	}
	delay, err := validateAdversaryCommitRevealObservations(left, right)
	if err != nil {
		return left, right, 0, err
	}
	return left, right, delay, nil
}
