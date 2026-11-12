//go:build linux || darwin

// The settlement v2 audit's runtime flags (VERSION2-FINAL.md §6.6, §13). They
// are not part of the signed configuration, so raising --ledger_max_bytes
// needs no re-sign. `validator run --config` carries them to the release
// runtime in its context, as it carries the durable volume reference.
package validator

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/miner/onchain"
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
	// --drand_url, the drand relay the capture the flag audit reads
	// quicknet's pulses from. Empty is DefaultDrandURL.
	DrandURL string
	// --amendments-fallback, the configured amendment lines read from it, one
	// per line (settlement_v2_amendments.go): the fallback where no signed
	// entry is in force. Empty configures none.
	AmendmentsFallback string
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
	if text := optString(opts, "--drand_url", ""); text != "" {
		parsed, err := url.Parse(text)
		if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return options, fmt.Errorf("--drand_url=%q must be an http(s) URL of a drand relay", text)
		}
		options.DrandURL = text
	}
	if options.AuditorKeyFile != "" {
		if _, err := onchain.LoadKeyFile(options.AuditorKeyFile); err != nil {
			return options, fmt.Errorf("--ledger_auditor_key_file: %w", err)
		}
	}
	lines, err := readSettlementV2AmendmentsFallback(expandHome(optString(opts, "--amendments-fallback", "")))
	if err != nil {
		return options, err
	}
	options.AmendmentsFallback = strings.Join(lines, "\n")
	return options, nil
}
