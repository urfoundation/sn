//go:build linux || darwin

// The settlement v2 audit's runtime flags (VERSION2-FINAL.md §6.6, §13). They
// are not part of the signed configuration, so raising --ledger_max_bytes
// needs no re-sign. `validator run --config` carries them to the release
// runtime in its context, as it carries the durable volume reference.
package validator

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/miner/onchain"
)

// DefaultLedgerMaxBytes bounds the ledger objects new to custody that the
// audit fetches per root: 2 GiB.
const DefaultLedgerMaxBytes uint64 = 2 << 30

// Release runtime flags remain outside the signed configuration.
type SettlementV2RuntimeOptions struct {
	// --ledger_auditor_key_file, the key NewSettlementV2Sender signs with.
	// Empty sends no auditor transaction.
	AuditorKeyFile string
	// --ledger_max_bytes
	LedgerMaxBytes uint64
	// Optional whole steering-operation budget. Zero selects
	// DefaultReleaseSteeringSettings; negative values are invalid.
	SteeringOperationTimeout time.Duration
}

type settlementV2RuntimeOptionsKey struct{}

// WithSettlementV2RuntimeOptions carries the flags to the release runtime.
func WithSettlementV2RuntimeOptions(ctx context.Context, options SettlementV2RuntimeOptions) context.Context {
	return context.WithValue(ctx, settlementV2RuntimeOptionsKey{}, options)
}

// SettlementV2RuntimeOptionsFrom returns the flags `validator run` carried in
// ctx, or the defaults: no auditor key and DefaultLedgerMaxBytes.
func SettlementV2RuntimeOptionsFrom(ctx context.Context) SettlementV2RuntimeOptions {
	if options, ok := ctx.Value(settlementV2RuntimeOptionsKey{}).(SettlementV2RuntimeOptions); ok {
		return options
	}
	return SettlementV2RuntimeOptions{LedgerMaxBytes: DefaultLedgerMaxBytes}
}

// Parses the flags strictly. The key is loaded once here, so a wrong file
// stops `validator run` at startup rather than at the first approval.
func settlementV2RuntimeOptionsFromOpts(opts docopt.Opts) (SettlementV2RuntimeOptions, error) {
	options := SettlementV2RuntimeOptions{AuditorKeyFile: expandHome(optString(opts, "--ledger_auditor_key_file", "")), LedgerMaxBytes: DefaultLedgerMaxBytes}
	if text, present := opts["--steering-operation-timeout"].(string); present {
		timeout, err := time.ParseDuration(text)
		if err != nil || timeout < productionSteeringReadTimeout {
			return options, fmt.Errorf("--steering-operation-timeout=%q must be a duration of at least %s", text, productionSteeringReadTimeout)
		}
		options.SteeringOperationTimeout = timeout
	}
	if text := optString(opts, "--ledger_max_bytes", ""); text != "" {
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil || value == 0 {
			return options, fmt.Errorf("--ledger_max_bytes=%q must be a positive number of bytes", text)
		}
		options.LedgerMaxBytes = value
	}
	if options.AuditorKeyFile != "" {
		if _, err := onchain.LoadKeyFile(options.AuditorKeyFile); err != nil {
			return options, fmt.Errorf("--ledger_auditor_key_file: %w", err)
		}
	}
	return options, nil
}
