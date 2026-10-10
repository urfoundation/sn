// Mainnet commands observe approved state and prepare bounded local custody
// phases. Owner-local device signing is separate from submission and activation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durableinspect"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
)

const monitorSchema = "urnetwork-mainnet-monitor-event-v1"
const mainnetEvmChainId = 964

// monitorEvent is one JSON line suitable for the existing log/alert pipeline.
type monitorEvent struct {
	Schema        string                        `json:"schema"`
	ObservedAt    string                        `json:"observed_at"`
	Status        string                        `json:"status"`
	Severity      string                        `json:"severity,omitempty"`
	Detail        string                        `json:"detail,omitempty"`
	Snapshot      *identityEnvelope             `json:"snapshot,omitempty"`
	Diagnostics   *monitorDiagnosticObservation `json:"diagnostics,omitempty"`
	RpcComparison *monitorRpcComparisonResult   `json:"rpc_comparison,omitempty"`
}

// monitorState tracks finalized progress without treating a changing tip as finality.
type monitorState struct {
	lastHash         string
	lastNumber       uint64
	lastProgressAt   time.Time
	lastSuccessAt    time.Time
	unavailableSince time.Time
}

// A read outage remains visible across samples and checkpointed restarts.
// Clock rollback cannot postpone its page threshold indefinitely.
func (self *monitorState) observeUnavailable(startedAt, now time.Time) (string, bool) {
	changed := self.unavailableSince.IsZero()
	if changed {
		self.unavailableSince = startedAt
	}
	if now.Before(self.unavailableSince) || now.Sub(self.unavailableSince) >= 5*time.Minute {
		return "critical", changed
	}
	if now.Sub(self.unavailableSince) >= 2*time.Minute {
		return "warning", changed
	}
	return "", changed
}

// A fully checked read, including retained finality, ends an outage.
func (self *monitorState) clearUnavailable() bool {
	if self.unavailableSince.IsZero() {
		return false
	}
	self.unavailableSince = time.Time{}
	return true
}

// observe recognizes frozen or contradictory finalized heads at one route.
func (self *monitorState) observe(now time.Time, identity chainIdentity, stallAfter time.Duration) (string, error) {
	if self.lastHash == "" {
		self.lastHash, self.lastNumber, self.lastProgressAt = identity.FinalizedHash, identity.FinalizedNumber, now
		self.lastSuccessAt = now
		return "ok", nil
	}
	if identity.FinalizedNumber < self.lastNumber || identity.FinalizedNumber == self.lastNumber && !strings.EqualFold(identity.FinalizedHash, self.lastHash) || identity.FinalizedNumber > self.lastNumber && strings.EqualFold(identity.FinalizedHash, self.lastHash) {
		return "finality-conflict", fmt.Errorf("finalized head changed incompatibly: %d/%s to %d/%s", self.lastNumber, self.lastHash, identity.FinalizedNumber, identity.FinalizedHash)
	}
	self.lastSuccessAt = now
	if identity.FinalizedNumber > self.lastNumber {
		self.lastHash, self.lastNumber, self.lastProgressAt = identity.FinalizedHash, identity.FinalizedNumber, now
		return "ok", nil
	}
	// A future retained timestamp cannot postpone a stall indefinitely after a
	// host clock rollback. Only new finalized progress resets that uncertainty.
	if self.lastProgressAt.After(now) || now.Sub(self.lastProgressAt) >= stallAfter {
		return "finality-stalled", nil
	}
	return "ok", nil
}

// Wire cancellation once; device commands retain their explicit custody gates.
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(runMain(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// Dispatches observations, review plans and bounded local custody with explicit exits.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runMainWithClock(ctx, args, stdout, stderr, time.Now)
}

// A supplied clock makes outage and finality deadlines reproducible in tests.
func runMainWithClock(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	return runMainWithMonitorHooks(ctx, args, stdout, stderr, now, monitorServiceHooks{})
}

// Test observers cover real file operations and owned waits, not source verdicts.
func runMainWithMonitorHooks(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) int {
	var argumentErr error
	ctx, args, argumentErr = mainnetDurableVolumeArguments(ctx, args)
	if argumentErr != nil {
		fmt.Fprintln(stderr, "durable-volume command inputs:", argumentErr)
		return 2
	}
	if mainnetRequiresDurableVolumes(args) {
		if err := durablepath.Require(ctx); err != nil {
			fmt.Fprintln(stderr, "durable custody declaration:", err)
			return 2
		}
	}
	if len(args) != 0 && args[0] == "treasury" {
		return runTreasuryCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "storage-inventory" {
		return runStorageInventory(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "storage-verify" {
		return runStorageVerify(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "storage-owner-inventory" {
		return runStorageOwnerInventory(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "storage-owner-verify" {
		return runStorageOwnerVerify(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "storage-inspect" {
		return durableinspect.Run(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "monitor-native-archive" {
		return runMonitorNativeArchive(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "monitor-claim-catalog" {
		return runMonitorClaimCatalog(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "monitor-claim-window" {
		return runMonitorClaimWindow(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "monitor-claim-archive" {
		return runMonitorClaimArchive(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "monitor-evm-archive" {
		return runMonitorEvmArchive(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "monitor-evm-catalog" {
		return runMonitorEvmCatalog(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "monitor-native-catalog" {
		return runMonitorNativeCatalog(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && (args[0] == "storage-prepare" || args[0] == "storage-owner-prepare") {
		return runStoragePreparationCommand(ctx, args[1:], stdout, stderr, args[0] == "storage-owner-prepare")
	}
	if len(args) != 0 && args[0] == "validator-capacity-preview" {
		return runValidatorCapacityPreview(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "validator-capacity-config" {
		return runValidatorCapacityConfig(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "root-service" {
		return runRootServiceCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "root-capabilities" {
		return runRootCurrentCapabilitiesCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "root-register" {
		return runRootRegisterCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "activate-root-passive" {
		return runRootPassiveHostCommand(ctx, args[1:], stdout, stderr, now)
	}
	if len(args) != 0 && args[0] == "root-passive-service" {
		return runRootPassiveServiceCommandWithMonitorHooks(ctx, args[1:], stdout, stderr, now, hooks)
	}
	if len(args) != 0 && args[0] == "owner-signing" {
		return runOwnerSigningCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "activate-validators" {
		return runValidatorActivationCommand(ctx, args[1:], stdout, stderr, now)
	}
	if len(args) != 0 && args[0] == "safe-history-capture" {
		return runSafeHistoryCaptureCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "repair-validator" {
		return runRepairValidatorCommand(ctx, args[1:], stdout, stderr, now)
	}
	if len(args) != 0 && args[0] == "repair-controller" {
		return runRepairControllerCommand(ctx, args[1:], stdout, stderr, now)
	}
	if len(args) != 0 && args[0] == "repair-active-validator" {
		return runRepairActiveValidatorCommand(ctx, args[1:], stdout, stderr, now)
	}
	if len(args) != 0 && args[0] == "safe-release-verify" {
		return runSafeReleaseVerify(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "bootstrap-chain" {
		return runBootstrapChainCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "bootstrap-contracts" {
		return runBootstrapContractCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "bootstrap" {
		return runBootstrapRootCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "release-inventory" {
		return runReleaseInventoryCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "plan" {
		return runPlanCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "finalized-snapshot" {
		return runFinalizedSnapshotCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "finalized-mapping" {
		return runFinalizedMappingCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "runtime-snapshot" {
		return runRuntimeSnapshotCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "source-lock" {
		return runSourceLockCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "subnet-discover" {
		return runSubnetDiscoveryCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && (args[0] == "subnet-preview" || args[0] == "owner-trim-plan") {
		return runSubnetCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && (args[0] == "owner-trim-recheck" || args[0] == "owner-trim-reconcile") {
		return runOwnerTrimGuardCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "owner-trim-qualify" {
		return runOwnerTrimBoundedCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && (args[0] == "root-preview" || args[0] == "root-monitor") {
		return runRootCommandWithMonitorHooks(ctx, args, stdout, stderr, now, hooks)
	}
	if len(args) != 0 && (args[0] == "check-recycle-mode" || args[0] == "economic-reference") {
		return runEconomicCommand(ctx, args, stdout, stderr)
	}
	if len(args) != 0 && args[0] == "owner-recycle" {
		return runOwnerRecycleCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "observe-native-miner-emission" {
		return runEconomicEmissionCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "economic-conservation-archive" {
		return runEconomicConservationArchive(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "economic-conservation-claim-window" {
		return runEconomicConservationClaimWindow(ctx, args[1:], stdout, stderr, hooks)
	}
	if len(args) != 0 && args[0] == "observe-economic-conservation" {
		return runEconomicConservationCommand(ctx, args[1:], stdout, stderr, now, hooks)
	}
	if len(args) != 0 && args[0] == "verify-historical-execution" {
		return runHistoricalReplayCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "verify-admitted-native-fees" {
		return runEconomicNativeFeeEvidenceCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "verify-native-fee-outcome" {
		return runEconomicNativeFeeOutcomeCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "verify-historical-fee-context" {
		return runHistoricalFeeContextCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "capture-historical-execution" {
		return runHistoricalCaptureCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "historical-capture-request" {
		return runHistoricalCaptureRequestCommand(ctx, args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "inspect" && args[0] != "monitor" {
		fmt.Fprintln(stderr, "storage reports: sn-mainnet storage-inventory|storage-verify --durable-volumes FILE --durable-volumes-sha256 sha256:DIGEST --root DIR --former-writer-fence FILE --former-writer-fence-sha256 sha256:DIGEST [--inventory FILE --inventory-sha256 sha256:DIGEST]; reports do not authorize restart")
		fmt.Fprintln(stderr, "owner-local storage reports: storage-owner-inventory|storage-owner-verify selects only the owner-local declaration schema; --max-owner-attributes and --max-owner-attribute-bytes bound retained custody metadata; verification --compare-reviewed-rebound only reports comparison to an explicit target declaration")
		fmt.Fprintln(stderr, "owner recycle custody: sn-mainnet owner-recycle observe|plan|reserve|export|inspect-request|ledger-plan|sign|import|import-reply|status|reconcile|submit-plan|submit [explicit policy, approval, original action and request pins]")
		fmt.Fprintln(stderr, "root registration custody: sn-mainnet root-register observe|plan|reserve|export|inspect-request|ledger-plan|sign|import|import-reply|status|reconcile|submit-plan|submit|bootstrap-handoff [explicit operator, full balance exposure consent, approval and original request pins]")
		fmt.Fprintln(stderr, "absent-seat bootstrap precursor: sn-mainnet bootstrap-chain root-registration <root-register subcommand>; finalized seat requires separate passive-service configuration and approval")
		fmt.Fprintln(stderr, "offline root capabilities: sn-mainnet root-capabilities --metadata FILE --metadata-hash 0xHASH --runtime-source-commit COMMIT; call compatibility does not establish current participation or signing authority")
		fmt.Fprintln(stderr, "offline owner handoff: sn-mainnet owner-signing inspect|sign|reply|verify|ledger-plan --request FILE --accept-request-hash HASH --trim-approval-key HEX --owner-account-id HEX --expected-genesis HEX [--signatory-account-id HEX for a native multisig owner step] [owner-local device custody, public response or proof flags]")
		fmt.Fprintln(stderr, "offline artifacts: sn-mainnet safe-release-verify --version 1.4.1|1.5.0 --variant Safe|SafeL2 --archive ABSOLUTE_FILE")
		fmt.Fprintln(stderr, "offline successor preparation: bootstrap-chain contract-successor-preview|contract-successor-prepare|contract-successor-resume --config FILE --run-dir DIR --accept-plan-hash HASH --request FILE [exact preparation approval flags]")
		fmt.Fprintln(stderr, "usage: sn-mainnet inspect|monitor|runtime-snapshot|finalized-mapping|finalized-snapshot --rpc URL [identity flags]; subnet-discover --rpc URL --snapshot FILE; root-preview|root-monitor|subnet-preview|owner-trim-plan --rpc URL --policy FILE; owner-trim-recheck|owner-trim-reconcile --rpc URL --policy FILE --plan FILE --plan-hash sha256:DIGEST; owner-trim-qualify --rpc URL --policy FILE --window FILE; check-recycle-mode|observe-native-miner-emission --rpc URL --policy FILE; economic-reference --input FILE; source-lock --sn-dir DIR; plan --outline|--config FILE; bootstrap|bootstrap-chain plan|apply|resume --config FILE [local custody confirmation flags]; bootstrap-chain readiness --config FILE --run-dir DIR --accept-plan-hash HASH --rpc URL; bootstrap-chain contract-plan|contract-readiness|contract-successor-plan --config FILE [original custody confirmation flags]; bootstrap-contracts preview|plan|apply|resume --config FILE; release-inventory --config FILE")
		return 2
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) RPC URL")
	expectedChain := flags.String("expected-chain", "", "approved native chain name")
	expectedGenesis := flags.String("expected-genesis", "", "approved native genesis hash")
	expectedEvmChainId := flags.Uint64("expected-evm-chain-id", 0, "approved EVM chain ID")
	defaultRetryWindow := 60 * time.Second
	if command == "monitor" {
		defaultRetryWindow = defaultMonitorProgressReadBudget
	}
	retryWindow := flags.Duration("retry-window", defaultRetryWindow, "total transient retry window per read")
	interval := flags.Duration("interval", 30*time.Second, "monitor sampling interval")
	stallAfter := flags.Duration("stall-after", 5*time.Minute, "finality progress alert threshold")
	checkpointPath := flags.String("checkpoint", "", "absolute path for a durable monitor finality checkpoint")
	metricsPath := flags.String("metrics-file", "", "absolute .prom path for atomic monitor telemetry")
	servicesPath := flags.String("services", "", "bounded expected service-role policy; requires checkpoint and metrics-file")
	comparisonPath := flags.String("rpc-comparison-policy", "", "optional private independently approved second-route comparison policy")
	comparisonPin := flags.String("rpc-comparison-policy-sha256", "", "exact independently admitted comparison policy SHA-256")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" {
		fmt.Fprintln(stderr, "command requires --rpc and no positional arguments")
		return 2
	}
	if command == "monitor" && (*interval <= 0 || *stallAfter <= 0) {
		fmt.Fprintln(stderr, "monitor interval and stall threshold must be positive")
		return 2
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if hooks.afterRpcClient != nil {
		hooks.afterRpcClient(command, client.retryWindow)
	}
	expected := identityExpectation{NativeChain: *expectedChain, GenesisHash: *expectedGenesis, EvmChainId: *expectedEvmChainId}
	checkExpected := command == "monitor" || expected.NativeChain != "" || expected.GenesisHash != "" || expected.EvmChainId != 0
	if checkExpected && (expected.NativeChain == "" || !validHash(expected.GenesisHash) || expected.EvmChainId == 0) {
		fmt.Fprintln(stderr, "approved chain, genesis and EVM chain ID must all be supplied")
		return 2
	}
	if command == "monitor" && expected.EvmChainId != mainnetEvmChainId {
		fmt.Fprintf(stderr, "mainnet monitor requires EVM chain ID %d\n", mainnetEvmChainId)
		return 2
	}
	if command == "inspect" && (*checkpointPath != "" || *metricsPath != "" || *servicesPath != "") {
		fmt.Fprintln(stderr, "--checkpoint, --metrics-file and --services are only valid for monitor")
		return 2
	}
	if (*comparisonPath == "") != (*comparisonPin == "") || command != "monitor" && (*comparisonPath != "" || *comparisonPin != "") {
		fmt.Fprintln(stderr, "RPC comparison requires both policy flags and the monitor command")
		return 2
	}
	if *comparisonPath != "" {
		if !bootstrapRootAbsolutePath(*comparisonPath) || !planSha256(*comparisonPin) {
			fmt.Fprintln(stderr, "RPC comparison requires an absolute policy path and canonical SHA-256 pin")
			return 2
		}
		for _, output := range []string{*checkpointPath, *metricsPath} {
			if output != "" && (*comparisonPath == output || *comparisonPath == output+".lock") {
				fmt.Fprintln(stderr, "RPC comparison input must be separate from monitor outputs and locks")
				return 2
			}
		}
		comparison := &monitorRpcComparisonRequest{path: *comparisonPath, pin: *comparisonPin, primaryUrl: *rpcUrl, expected: expected, now: now}
		defer comparison.close()
		ctx = context.WithValue(ctx, monitorRpcComparisonContextKey{}, comparison)
	}
	if *metricsPath != "" && *checkpointPath != "" {
		metrics, metricsErr := resolveMonitorDestination(*metricsPath)
		checkpoint, checkpointErr := resolveMonitorDestination(*checkpointPath)
		if metricsErr != nil || checkpointErr != nil {
			fmt.Fprintln(stderr, "monitor output paths:", errors.Join(metricsErr, checkpointErr))
			return 2
		}
		if metrics == checkpoint || metrics+".lock" == checkpoint || metrics == checkpoint+".lock" {
			fmt.Fprintln(stderr, "monitor metrics and checkpoint paths must be separate")
			return 2
		}
		// Ownership must use the destination that passed separation, even if
		// the original caller-supplied alias changes before the store opens.
		*checkpointPath = checkpoint
	}
	encoder := json.NewEncoder(stdout)
	if command == "inspect" {
		identity, err := client.readIdentity(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		snapshot, err := sealIdentity(identity)
		if err != nil {
			fmt.Fprintln(stderr, "write identity snapshot:", err)
			return 1
		}
		if err := encoder.Encode(snapshot); err != nil {
			fmt.Fprintln(stderr, "write identity snapshot:", err)
			return 1
		}
		if checkExpected {
			if err := expected.match(identity); err != nil {
				fmt.Fprintln(stderr, err)
				return 3
			}
		}
		return 0
	}
	if *servicesPath != "" {
		if *checkpointPath == "" || *metricsPath == "" {
			fmt.Fprintln(stderr, "--services requires separate --checkpoint and --metrics-file outputs")
			return 2
		}
		policy, err := loadMonitorServices(ctx, *servicesPath, expected, *checkpointPath, *metricsPath)
		if err != nil {
			fmt.Fprintln(stderr, "monitor service policy:", err)
			return 2
		}
		return runMonitorServices(ctx, client, expected, policy, *checkpointPath, *metricsPath, *interval, *stallAfter, stdout, stderr, now, hooks)
	}
	return runMonitorOnly(ctx, client, expected, *checkpointPath, *metricsPath, *interval, *stallAfter, stdout, stderr, now, hooks)
}

// Chain sampling retains its original continuity, publication and exit rules.
// Service composition supervises this independently of bounded file reads.
func runChainMonitor(ctx context.Context, client *rpcClient, expected identityExpectation, checkpointPath, metricsPath string, interval, stallAfter time.Duration, stdout, stderr io.Writer, now func() time.Time, hooks monitorServiceHooks) (result int) {
	if ctx.Err() != nil {
		return 0
	}
	encoder := json.NewEncoder(stdout)
	var err error
	state := &monitorState{}
	var checkpoint *monitorCheckpointStore
	var metrics *monitorMetricsStore
	var storage monitorStorageRecovery
	defer func() {
		if err := errors.Join(metrics.close(), checkpoint.close()); err != nil {
			fmt.Fprintln(stderr, "monitor chain cleanup:", err)
			result = 3
		}
	}()
	if checkpointPath != "" {
		checkpoint, err = openMonitorCheckpoint(checkpointPath, expected, ctx)
		if err != nil {
			if monitorCanceledCheckpointLoad(ctx, err) {
				return 0
			}
			fmt.Fprintln(stderr, "monitor checkpoint:", err)
			if ctx.Err() == nil && monitorStartupPending(err) {
				return 1
			}
			return 3
		}
		if hooks.afterCheckpointOpen != nil {
			hooks.afterCheckpointOpen(ctx, "chain", checkpoint.lock)
		}
		budget := defaultMonitorProgressReadBudget
		if client != nil && client.retryWindow > 0 {
			budget = client.retryWindow
		}
		state, err = loadMonitorChainCheckpoint(ctx, checkpoint, budget, stdout, stderr, now, hooks)
		if err != nil {
			if monitorCanceledCheckpointLoad(ctx, err) {
				return 0
			}
			fmt.Fprintln(stderr, "monitor checkpoint:", err)
			if monitorStartupPending(err) {
				if ctx.Err() != nil {
					return 0
				}
				return 1
			}
			publishMonitorAdmission("chain", "quarantined", err, 0, stdout, stderr, now)
			return 3
		}
		if hooks.syncDirectory != nil {
			checkpoint.syncDirectory = func(file *os.File) error { return hooks.syncDirectory("chain", "checkpoint", file) }
		}
	}
	if metricsPath != "" {
		metrics, err = openMonitorMetrics(metricsPath, ctx)
		if err != nil {
			if monitorCanceledCheckpointLoad(ctx, err) {
				return 0
			}
			fmt.Fprintln(stderr, "monitor metrics:", err)
			if ctx.Err() == nil && monitorStartupPending(err) {
				return 1
			}
			return 2
		}
		if hooks.syncDirectory != nil {
			metrics.syncDirectory = func(file *os.File) error { return hooks.syncDirectory("chain", "metrics", file) }
		}
		for {
			if err := metrics.initialize(state); err != nil {
				if monitorCanceledCheckpointLoad(ctx, err) {
					return 0
				}
				fmt.Fprintln(stderr, "initialize monitor metrics:", err)
				var ownership *monitorOutputOwnershipError
				if errors.As(err, &ownership) {
					return 3
				}
				if !waitMonitorService(ctx, "chain", time.Second, hooks) {
					return 0
				}
				continue
			}
			break
		}
	}
	// Publication follows durable continuity. Errors preserve an explicit failed
	// event; a stopped/blocked loop leaves a stale textfile for external alerts.
	var metricsErr error
	publishEvent := func(event *monitorEvent) error {
		event.Diagnostics = monitorDiagnosticSnapshot(stdout, stderr)
		switch event.Status {
		case "identity-mismatch", "finality-conflict", "rpc-integrity", "finality-stalled", "checkpoint-error":
			event.Severity = "critical"
		}
		if metrics != nil {
			metricsErr = metrics.save(*event, state)
			if metricsErr != nil {
				event.Detail = fmt.Sprintf("status=%s severity=%s; metrics publication unavailable", event.Status, event.Severity)
				event.Status, event.Severity = "metrics-error", "critical"
			}
		}
		return encoder.Encode(event)
	}
	for {
		if ctx.Err() != nil {
			return 0
		}
		comparisonResult := unknownMonitorRpcComparison("independent RPC policy or chain observation unavailable")
		event := monitorEvent{Schema: monitorSchema, RpcComparison: &comparisonResult}
		var checkpointErr error
		metricsErr = nil
		sampleStartedAt := now().UTC()
		identity, readErr := client.readIdentity(ctx)
		sampledAt := now().UTC()
		event.ObservedAt = sampledAt.Format(time.RFC3339Nano)
		markUnavailable := func(readErr error) {
			event.Status, event.Detail = "rpc-error", "RPC observation unavailable"
			if errors.Is(readErr, context.DeadlineExceeded) {
				event.Detail = "RPC observation timed out"
			}
			if errors.Is(readErr, errRpcIntegrity) {
				event.Status, event.Severity = "rpc-integrity", "critical"
				return
			}
			var changed bool
			event.Severity, changed = state.observeUnavailable(sampleStartedAt, sampledAt)
			if changed && checkpoint != nil {
				checkpointErr = checkpoint.save(state)
				if checkpointErr != nil {
					event.Status, event.Severity = "checkpoint-error", "critical"
					event.Detail = "RPC observation and checkpoint publication unavailable"
				}
			}
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return 0
			}
			markUnavailable(readErr)
		} else {
			snapshot, sealErr := sealIdentity(identity)
			if sealErr != nil {
				fmt.Fprintln(stderr, sealErr)
				return 1
			}
			event.Snapshot = &snapshot
			if identityErr := expected.match(identity); identityErr != nil {
				event.Status, event.Detail = "identity-mismatch", "observed identity differs from expected configuration"
				// Output failure cannot turn an observed identity mismatch into a
				// retryable source observation or a newly initialized owner.
				_ = publishEvent(&event)
				return 3
			}
			continuous, continuityErr := client.priorFinalizedMatches(ctx, state, identity)
			sampledAt = now().UTC()
			event.ObservedAt = sampledAt.Format(time.RFC3339Nano)
			if continuityErr != nil {
				markUnavailable(continuityErr)
			} else if !continuous {
				event.Status, event.Detail = "finality-conflict", "previously finalized block hash changed at its original height"
			} else {
				previousHash, previousNumber, previousSuccess := state.lastHash, state.lastNumber, state.lastSuccessAt
				event.Status, err = state.observe(sampledAt, identity, stallAfter)
				if err != nil {
					event.Detail = "finality progress observation failed"
				} else if recovered := state.clearUnavailable(); checkpoint != nil && (recovered || state.lastHash != previousHash || state.lastNumber != previousNumber || !state.lastSuccessAt.Equal(previousSuccess)) {
					checkpointErr = checkpoint.save(state)
					if checkpointErr != nil {
						event.Status, event.Severity, event.Detail = "checkpoint-error", "critical", "checkpoint publication unavailable"
					}
				}
			}
		}
		if event.Status == "ok" || event.Status == "finality-stalled" {
			if comparison, ok := ctx.Value(monitorRpcComparisonContextKey{}).(monitorRpcComparisonReader); ok {
				comparisonResult = comparison.compare(ctx, identity.FinalizedHash)
			}
			if ctx.Err() != nil {
				return 0
			}
			if comparisonResult.Status == "disagreement" {
				event.Severity = "critical"
			}
		}
		terminalObservation := event.Status == "finality-conflict" || event.Status == "rpc-integrity"
		if err := publishEvent(&event); err != nil {
			if ctx.Err() != nil {
				return 0
			}
			fmt.Fprintln(stderr, "write monitor event:", err)
			return 3
		}
		if hooks.afterEvent != nil {
			hooks.afterEvent(ctx, "chain")
		}
		var ownership *monitorOutputOwnershipError
		if terminalObservation || errors.As(errors.Join(checkpointErr, metricsErr), &ownership) {
			return 3
		}
		if errors.Is(checkpointErr, durablehead.ErrUncertain) {
			prior := checkpoint
			err := storage.resume(ctx, "chain", func() error {
				file := prior.lock
				err := prior.close()
				if hooks.afterClose != nil {
					err = errors.Join(err, hooks.afterClose("chain", "checkpoint", file))
				}
				return err
			}, func() error {
				next, err := openMonitorCheckpoint(prior.path, prior.expected, ctx)
				if err != nil {
					return err
				}
				loaded, err := next.load()
				if err != nil {
					return monitorAdmissionFailure(err, next.close())
				}
				next.syncDirectory = prior.syncDirectory
				checkpoint, state = next, loaded
				return nil
			}, hooks)
			if err != nil {
				if monitorCanceledCheckpointLoad(ctx, err) {
					return 0
				}
				fmt.Fprintln(stderr, "monitor chain storage continuation:", err)
				return 3
			}
			continue
		}
		delay := interval
		if checkpointErr != nil || metricsErr != nil {
			delay = time.Second
		}
		if !waitMonitorService(ctx, "chain", delay, hooks) {
			return 0
		}
	}
}
