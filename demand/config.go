// Package demand pays the operator's demand deposits. The operator has one
// Bittensor wallet holding alpha and one Solana wallet holding USDC. When the
// alpha wallet cannot pay a deposit in full, the Funder converts USDC into SN25
// alpha over a fully on-chain route: Jupiter swaps USDC for canonical TAO on
// Solana, LayerZero bridges the TAO to wTAO on Bittensor EVM, wTAO unwraps to TAO,
// and the TAO stakes into alpha on the wallet's deposit hotkey. See README.md.
package demand

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/demand/solana"
)

// ResourceName is the vault resource holding the two wallets.
const ResourceName = "demand.yml"

// Conversion's defaults. A conversion spends real money without a person
// watching, so every limit has a value even when demand.yml names none.
const (
	defaultMaxUSDCPerEpoch    = 6_000.0
	defaultMaxPriceGap        = 0.01
	defaultMaxSlippageBps     = 100
	defaultMinSOL             = 0.06
	defaultMaxBridgeFeeSOL    = 0.05
	defaultAlphaBufferBps     = 100
	defaultPoolPriceTolerance = 0.01
	// At the 30 gwei fee cap a deposit needs 0.102 TAO of gas, and a staging and
	// deposit pair 0.186.
	defaultGasReserveTAO = 0.2
	// Less than this cannot pay for a stake, whose gas limit is about 2,800,000, or
	// 0.014 TAO at the chain's usual 5 gwei, and then an unwrap.
	minGasReserveTAO = 0.02
	// No limit needs more than this, and it keeps every amount well inside a
	// uint64 of its smallest unit.
	maxConfigAmount = 1e9

	lamportsPerSOL = 1_000_000_000
	raoPerTAO      = 1_000_000_000
	microPerUSDC   = 1_000_000
)

// Conversion limits the USDC to alpha conversion.
type Conversion struct {
	// Enabled must be set for the wallet's USDC to be spent. Without it a short
	// alpha wallet is an error and nothing else happens.
	Enabled bool
	// MaxUSDCMicroPerEpoch caps the USDC swapped for one epoch's deposit.
	MaxUSDCMicroPerEpoch uint64
	// MaxPriceGap is how far above Coinbase's public TAO ask a swap may pay.
	MaxPriceGap float64
	// MaxSlippageBps caps Jupiter's slippage even when the quote is far from
	// the floor.
	MaxSlippageBps int
	// MinSOLLamports is the least SOL the wallet must hold to start a swap: both
	// Solana legs of the route pay their fees from it, so it covers both at their
	// most.
	MinSOLLamports uint64
	// MaxBridgeFeeLamports caps LayerZero's fee for one bridge send.
	MaxBridgeFeeLamports uint64
	// AlphaBufferBps buys this much more than the shortfall, so a price move
	// between the swap and the stake does not need a second conversion. What it
	// leaves over stays in the wallet for the next deposit.
	AlphaBufferBps uint64
	// GasReserveRao is the TAO the alpha wallet keeps unstaked for gas.
	GasReserveRao uint64
	// PoolPriceTolerance is how far over the pool's expected price after the
	// purchase the stake may fill.
	PoolPriceTolerance float64
	// JupiterAPIKey is optional; a key raises Jupiter's rate limit.
	JupiterAPIKey string
}

// Format keeps JupiterAPIKey out of every fmt verb, on a value or a pointer.
func (c Conversion) Format(state fmt.State, _ rune) { io.WriteString(state, c.String()) }

// GoString keeps JupiterAPIKey out of anything that asks for Go syntax directly.
func (c Conversion) GoString() string { return c.String() }

// String states the limits and leaves out the key.
func (c Conversion) String() string {
	return fmt.Sprintf("demand.Conversion{enabled %t, max_usdc_micro_per_epoch %d, max_price_gap %v, max_slippage_bps %d, min_sol_lamports %d, max_bridge_fee_lamports %d, alpha_buffer_bps %d, gas_reserve_rao %d, pool_price_tolerance %v}",
		c.Enabled, c.MaxUSDCMicroPerEpoch, c.MaxPriceGap, c.MaxSlippageBps, c.MinSOLLamports, c.MaxBridgeFeeLamports, c.AlphaBufferBps, c.GasReserveRao, c.PoolPriceTolerance)
}

// Config is demand.yml parsed and checked. It holds private keys, so it is
// never logged or formatted: printing one, or a copy of one, gives only String.
type Config struct {
	AlphaAddress common.Address
	// Hotkey is the deposit hotkey the alpha wallet's stake sits on.
	Hotkey      [32]byte
	USDCAddress solana.PublicKey
	Conversion  Conversion

	alphaKey *ecdsa.PrivateKey
	usdcKey  ed25519.PrivateKey
}

// AlphaKey is the alpha wallet's EVM signing key.
func (c *Config) AlphaKey() *ecdsa.PrivateKey { return c.alphaKey }

// Format keeps the keys out of every fmt verb, on a value or a pointer. Without it
// a verb that takes no Stringer, such as %d, would print a value's fields.
func (c Config) Format(state fmt.State, _ rune) { io.WriteString(state, c.String()) }

// GoString keeps the keys out of anything that asks for Go syntax directly.
func (c Config) GoString() string { return c.String() }

// String names the wallets and leaves out their keys.
func (c Config) String() string {
	return fmt.Sprintf("demand.Config{alpha %s, usdc %s}", c.AlphaAddress.Hex(), c.USDCAddress)
}

type yamlConfig struct {
	AlphaWallet struct {
		Address    string `yaml:"address"`
		PrivateKey string `yaml:"private_key"`
		Hotkey     string `yaml:"hotkey"`
	} `yaml:"alpha_wallet"`
	USDCWallet struct {
		Address    string `yaml:"address"`
		PrivateKey string `yaml:"private_key"`
	} `yaml:"usdc_wallet"`
	Conversion struct {
		Enabled            bool     `yaml:"enabled"`
		MaxUSDCPerEpoch    *float64 `yaml:"max_usdc_per_epoch"`
		MaxPriceGap        *float64 `yaml:"max_price_gap"`
		MaxSlippageBps     *int     `yaml:"max_slippage_bps"`
		MinSOL             *float64 `yaml:"min_sol"`
		MaxBridgeFeeSOL    *float64 `yaml:"max_bridge_fee_sol"`
		AlphaBufferBps     *uint64  `yaml:"alpha_buffer_bps"`
		GasReserveTAO      *float64 `yaml:"gas_reserve_tao"`
		PoolPriceTolerance *float64 `yaml:"pool_price_tolerance"`
		JupiterAPIKey      string   `yaml:"jupiter_api_key"`
	} `yaml:"conversion"`
}

func orDefault[T any](value *T, fallback T) T {
	if value != nil {
		return *value
	}
	return fallback
}

// ParseConfig parses demand.yml. Each wallet's key must derive its stated
// address. Errors never quote a value, since a value may be a private key: one
// about a known field names the field, and a document that does not parse as the
// expected fields says only that, since yaml's own errors quote what they read.
func ParseConfig(raw []byte) (*Config, error) {
	var file yamlConfig
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		// yaml errors can quote the document, so only their kind is reported.
		return nil, errors.New("demand: demand.yml does not parse as the expected fields")
	}
	config := &Config{}
	if !common.IsHexAddress(file.AlphaWallet.Address) {
		return nil, errors.New("demand: alpha_wallet.address is not an EVM address")
	}
	config.AlphaAddress = common.HexToAddress(file.AlphaWallet.Address)
	alphaKey, err := crypto.HexToECDSA(strings.TrimPrefix(file.AlphaWallet.PrivateKey, "0x"))
	if err != nil {
		return nil, errors.New("demand: alpha_wallet.private_key is not a hex secp256k1 key")
	}
	if crypto.PubkeyToAddress(alphaKey.PublicKey) != config.AlphaAddress {
		return nil, errors.New("demand: alpha_wallet.private_key does not derive alpha_wallet.address")
	}
	config.alphaKey = alphaKey
	hotkey, err := hex.DecodeString(strings.TrimPrefix(file.AlphaWallet.Hotkey, "0x"))
	if err != nil || len(hotkey) != 32 || [32]byte(hotkey) == ([32]byte{}) {
		return nil, errors.New("demand: alpha_wallet.hotkey is not a 32-byte hex key")
	}
	config.Hotkey = [32]byte(hotkey)
	if config.USDCAddress, err = solana.ParsePublicKey(file.USDCWallet.Address); err != nil {
		return nil, errors.New("demand: usdc_wallet.address is not a Solana address")
	}
	secret, err := solana.Base58Decode(file.USDCWallet.PrivateKey)
	if err != nil || len(secret) != ed25519.PrivateKeySize {
		return nil, errors.New("demand: usdc_wallet.private_key is not the base58 of a 64-byte Solana secret")
	}
	config.usdcKey = ed25519.NewKeyFromSeed(secret[:ed25519.SeedSize])
	if solana.PublicKeyOf(config.usdcKey) != config.USDCAddress || string(config.usdcKey) != string(secret) {
		return nil, errors.New("demand: usdc_wallet.private_key does not derive usdc_wallet.address")
	}
	maxUSDC := orDefault(file.Conversion.MaxUSDCPerEpoch, defaultMaxUSDCPerEpoch)
	maxPriceGap := orDefault(file.Conversion.MaxPriceGap, defaultMaxPriceGap)
	maxSlippageBps := orDefault(file.Conversion.MaxSlippageBps, defaultMaxSlippageBps)
	minSOL := orDefault(file.Conversion.MinSOL, defaultMinSOL)
	maxBridgeFee := orDefault(file.Conversion.MaxBridgeFeeSOL, defaultMaxBridgeFeeSOL)
	alphaBufferBps := orDefault(file.Conversion.AlphaBufferBps, defaultAlphaBufferBps)
	gasReserve := orDefault(file.Conversion.GasReserveTAO, defaultGasReserveTAO)
	poolPriceTolerance := orDefault(file.Conversion.PoolPriceTolerance, defaultPoolPriceTolerance)
	// Each line says what is accepted, so NaN, which compares false with everything,
	// fails all of them, as infinity fails all of their upper bounds. A check written
	// as what is refused lets NaN through: as max_price_gap it leaves no floor, and
	// converted to an integer, an infinite budget is none.
	for _, limit := range []struct {
		accepted bool
		refusal  string
	}{
		{maxUSDC > 0 && maxUSDC <= maxConfigAmount, "conversion.max_usdc_per_epoch must be above 0"},
		{maxPriceGap > 0 && maxPriceGap < 0.1, "conversion.max_price_gap must be between 0 and 0.1"},
		{maxSlippageBps >= 1 && maxSlippageBps <= 1_000, "conversion.max_slippage_bps must be between 1 and 1000"},
		{minSOL > 0 && minSOL <= maxConfigAmount, "conversion.min_sol must be above 0"},
		{maxBridgeFee > 0 && maxBridgeFee <= maxConfigAmount, "conversion.max_bridge_fee_sol must be above 0"},
		{alphaBufferBps <= 1_000, "conversion.alpha_buffer_bps must be at most 1000"},
		{gasReserve >= minGasReserveTAO && gasReserve <= maxConfigAmount, "conversion.gas_reserve_tao must be at least 0.02"},
		{poolPriceTolerance > 0 && poolPriceTolerance < 0.1, "conversion.pool_price_tolerance must be between 0 and 0.1"},
	} {
		if !limit.accepted {
			return nil, errors.New("demand: " + limit.refusal)
		}
	}
	config.Conversion = Conversion{
		Enabled:              file.Conversion.Enabled,
		MaxUSDCMicroPerEpoch: uint64(math.Round(maxUSDC * microPerUSDC)),
		MaxPriceGap:          maxPriceGap,
		MaxSlippageBps:       maxSlippageBps,
		MinSOLLamports:       uint64(math.Round(minSOL * lamportsPerSOL)),
		MaxBridgeFeeLamports: uint64(math.Round(maxBridgeFee * lamportsPerSOL)),
		AlphaBufferBps:       alphaBufferBps,
		GasReserveRao:        uint64(math.Round(gasReserve * raoPerTAO)),
		PoolPriceTolerance:   poolPriceTolerance,
		JupiterAPIKey:        file.Conversion.JupiterAPIKey,
	}
	limits := config.Conversion
	switch {
	case limits.MaxUSDCMicroPerEpoch == 0:
		return nil, errors.New("demand: conversion.max_usdc_per_epoch is below one micro-USDC")
	case limits.MaxBridgeFeeLamports == 0:
		return nil, errors.New("demand: conversion.max_bridge_fee_sol is below one lamport")
	case limits.MinSOLLamports < limits.MaxBridgeFeeLamports+bridgeTxLamports+maxSwapLamports:
		// A swap may land and leave the bridge send short of SOL otherwise.
		return nil, errors.New("demand: conversion.min_sol must cover conversion.max_bridge_fee_sol plus 0.007 for the swap and the send")
	}
	return config, nil
}
