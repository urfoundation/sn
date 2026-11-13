package commitmentoracle

// Health gauges, written after every cycle as one Prometheus textfile that the
// host's Fluent Bit node_exporter_metrics textfile input ships, the same way
// the validator's health gauges travel (xops SN-MAINNET-VALIDATOR.md, "Health
// metrics"). Every name starts with urnetwork_sn_commitment_oracle_. A gauge
// with no meaningful value (no success yet, nothing pending) is left out, so
// rules can tell unknown from zero. Not safe for concurrent use.

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const metricsPrefix = "urnetwork_sn_commitment_oracle_"

type metrics struct {
	processStart    float64
	dryRun          float64
	oracleActive    float64
	paused          float64
	evmFinalized    uint64
	nativeFinalized uint64
	lagBlocks       uint64
	commitments     int
	fleet           int
	mirrored        int
	expired         int
	conflicting     int
	inconsistent    int
	// One-field Sha256 commitments: a fleet published before the fleet tag
	// (it must republish) or a validator's source commitment. Never mirrored.
	legacy  int
	pending int
	// Blocks until the nearest pending commitment ages past
	// commitmentMaxAgeBlocks; meaningful only while pending > 0.
	minBlocksToExpiry uint64
	inFlight          float64
	inFlightSince     float64
	inFlightAge       float64
	balanceWei        *big.Int
	lowBalanceWei     *big.Int
	mirrorsSent       int
	lastSuccess       float64
	lastMirror        float64
	lastError         float64
	lastCycleOk       float64
	failures          int
}

func newMetrics(settings Settings, start time.Time) *metrics {
	lowBalance := settings.LowBalanceWei
	if lowBalance == nil {
		lowBalance = new(big.Int)
	}
	return &metrics{processStart: float64(start.Unix()), dryRun: boolGauge(settings.DryRun), lowBalanceWei: new(big.Int).Set(lowBalance)}
}

func boolGauge(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func (self *metrics) setBalance(balance *big.Int) {
	self.balanceWei = new(big.Int).Set(balance)
}

func (self *metrics) setInFlight(signedUnix int64, now time.Time) {
	if signedUnix == 0 {
		self.inFlight, self.inFlightSince, self.inFlightAge = 0, 0, 0
		return
	}
	self.inFlight = 1
	self.inFlightSince = float64(signedUnix)
	self.inFlightAge = max(0, float64(now.Unix()-signedUnix))
}

func (self *metrics) setCommitments(total int, fleet int, counts map[skipReason]int) {
	self.commitments = total
	self.fleet = fleet
	self.mirrored = counts[skipMirrored]
	self.expired = counts[skipExpired]
	self.conflicting = counts[skipConflict] + counts[skipNewerMirrored]
	self.inconsistent = counts[skipInconsistent]
	self.legacy = counts[skipLegacyFormat]
}

func (self *metrics) setPending(pending int, head uint64, expiries []uint64) {
	self.pending = pending
	self.minBlocksToExpiry = 0
	for index, expiry := range expiries {
		remaining := uint64(0)
		if expiry > head {
			remaining = expiry - head
		}
		if index == 0 || remaining < self.minBlocksToExpiry {
			self.minBlocksToExpiry = remaining
		}
	}
}

func (self *metrics) failed(now time.Time) {
	self.failures += 1
	self.lastError = float64(now.Unix())
	self.lastCycleOk = 0
}

func (self *metrics) succeeded() {
	self.failures = 0
	self.lastCycleOk = 1
}

// The gauge lines, in a fixed order.
func (self *metrics) render(now time.Time) string {
	lines := []string{}
	gauge := func(name string, description string, value float64) {
		lines = append(lines,
			fmt.Sprintf("# HELP %s%s %s", metricsPrefix, name, description),
			fmt.Sprintf("# TYPE %s%s gauge", metricsPrefix, name),
			fmt.Sprintf("%s%s %s", metricsPrefix, name, formatGauge(value)))
	}
	balanceLow := 1.0
	if self.balanceWei != nil && self.balanceWei.Cmp(self.lowBalanceWei) >= 0 {
		balanceLow = 0
	}
	gauge("sample_timestamp_seconds", "When this sample was written.", float64(now.Unix()))
	gauge("process_start_timestamp_seconds", "When the oracle process started.", self.processStart)
	gauge("dry_run", "1 when the oracle only logs the mirrors it would send.", self.dryRun)
	gauge("oracle_active", "1 while the key is the coordinator's active commitment oracle at the EVM finalized block.", self.oracleActive)
	gauge("coordinator_paused", "1 while the coordinator is paused.", self.paused)
	gauge("last_cycle_ok", "1 when the last cycle completed.", self.lastCycleOk)
	gauge("cycle_failures", "Consecutive failed cycles.", float64(self.failures))
	if self.evmFinalized != 0 {
		gauge("evm_finalized_block", "The EVM finalized block of the last cycle.", float64(self.evmFinalized))
	}
	if self.nativeFinalized != 0 {
		gauge("native_finalized_block", "The native finalized head of the last cycle.", float64(self.nativeFinalized))
		gauge("native_lag_blocks", "Native finalized blocks not yet covered by a complete cycle.", float64(self.lagBlocks))
		gauge("commitments", "CommitmentOf entries of the netuid at the finalized head.", float64(self.commitments))
		gauge("fleet_commitments", "Entries in the tagged fleet format (one Raw40 field, \"urfleet1\" then the hash).", float64(self.fleet))
		gauge("fleet_commitments_mirrored", "Fleet commitments already mirrored at the EVM finalized block.", float64(self.mirrored))
		gauge("fleet_commitments_expired", "Unmirrored fleet commitments older than commitmentMaxAgeBlocks.", float64(self.expired))
		gauge("fleet_commitments_conflicting", "Fleet commitments the coordinator holds a different or newer record for.", float64(self.conflicting))
		gauge("fleet_commitments_inconsistent", "Fleet commitments whose finalized evidence did not verify or agree across endpoints.", float64(self.inconsistent))
		gauge("legacy_sha256_commitments", "One-field Sha256 commitments, never mirrored: untagged fleets that must republish, and validator source commitments.", float64(self.legacy))
		gauge("pending_unmirrored", "Fresh fleet commitments not yet mirrored.", float64(self.pending))
		if self.pending > 0 {
			gauge("pending_min_blocks_to_expiry", "Blocks until the nearest pending commitment can no longer be bound.", float64(self.minBlocksToExpiry))
		}
	}
	gauge("in_flight", "1 while a signed mirror transaction is not final.", self.inFlight)
	if self.inFlight != 0 {
		gauge("in_flight_age_seconds", "Seconds since the in-flight transaction was signed.", self.inFlightAge)
	}
	if self.balanceWei != nil {
		gauge("oracle_balance_wei", "The oracle account's balance in wei (1 TAO = 1e18 wei).", weiGauge(self.balanceWei))
	}
	gauge("oracle_balance_low", "1 while the balance is unknown or below the low-balance threshold.", balanceLow)
	gauge("low_balance_threshold_wei", "The low-balance threshold in wei.", weiGauge(self.lowBalanceWei))
	gauge("mirrors_sent", "Mirror transactions sent since the process started.", float64(self.mirrorsSent))
	if self.lastSuccess != 0 {
		gauge("last_success_timestamp_seconds", "When a cycle last completed.", self.lastSuccess)
	}
	if self.lastMirror != 0 {
		gauge("last_mirror_timestamp_seconds", "When a mirror last became final.", self.lastMirror)
	}
	if self.lastError != 0 {
		gauge("last_error_timestamp_seconds", "When a cycle last failed.", self.lastError)
	}
	return strings.Join(lines, "\n") + "\n"
}

func formatGauge(value float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.6f", value), "0"), ".")
}

func weiGauge(wei *big.Int) float64 {
	value, _ := new(big.Float).SetInt(wei).Float64()
	return value
}

// Writes the sample atomically, world-readable so the shipper's group can read
// it; the directory must exist.
func (self *metrics) write(path string, now time.Time) error {
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return err
	}
	return atomicWrite(path, []byte(self.render(now)), 0o644)
}
