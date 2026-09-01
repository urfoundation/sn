package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"

	"github.com/urfoundation/sn/clientauth"
)

type ProcessSpec struct {
	ID, Role, Identity, Command, WorkDir string
	Args                                 []string
	Env                                  map[string]string
	StdoutPath, StderrPath, HealthURL    string
	H3ProbeAddress                       string
	H3ProbeServerName                    string
	H3ProbeCAFile                        string
	RestartLimit                         int
}
type ProcessState struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Identity  string `json:"identity"`
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Restarts  int    `json:"restarts"`
	Healthy   bool   `json:"healthy"`
	ExitError string `json:"exit_error,omitempty"`
}
type SupervisorFile struct {
	Schema       string        `json:"schema"`
	DeploymentID string        `json:"deployment_id"`
	BinaryHash   string        `json:"binary_hash"`
	Specs        []ProcessSpec `json:"specs"`
}
type SupervisorState struct {
	Schema                   string         `json:"schema"`
	UpdatedAt                string         `json:"updated_at"`
	SupervisorPID            int            `json:"supervisor_pid"`
	SupervisorStartTimeTicks uint64         `json:"supervisor_start_time_ticks"`
	ManifestHash             string         `json:"manifest_hash"`
	Processes                []ProcessState `json:"processes"`
}

// Records the kernel identity of each provisioning helper so a resume can
// reap an orphan without ever signalling a PID that the kernel has reused.
type TemporaryProcessIdentity struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	Identity        string `json:"identity"`
	PID             int    `json:"pid"`
	ProcessGroupID  int    `json:"process_group_id"`
	StartTimeTicks  uint64 `json:"start_time_ticks"`
	ExecutableHash  string `json:"executable_hash"`
	CommandLineHash string `json:"command_line_hash"`
}

// Survives only an abnormal parent exit; normal teardown removes it after all
// exact process identities have disappeared.
type TemporaryProcessFile struct {
	Schema       string                     `json:"schema"`
	DeploymentID string                     `json:"deployment_id"`
	Processes    []TemporaryProcessIdentity `json:"processes"`
}

const temporaryProcessFileSchema = "urnetwork-sim-temporary-processes-v1"

type dockerCLI struct {
	Executable    string
	Prefix        []string
	ServerVersion string
}

func (self dockerCLI) commandContext(ctx context.Context, args ...string) *exec.Cmd {
	commandArgs := append([]string(nil), self.Prefix...)
	commandArgs = append(commandArgs, args...)
	return exec.CommandContext(ctx, self.Executable, commandArgs...)
}

// Prove that the selected CLI can talk to a daemon, not merely that it exists.
func dockerServerVersion(ctx context.Context, cli dockerCLI) (string, error) {
	output, err := cli.commandContext(ctx, "version", "--format", "{{.Server.Version}}").CombinedOutput()
	detail := strings.TrimSpace(string(output))
	if err != nil {
		return detail, fmt.Errorf("docker daemon is unavailable: %w: %s", err, detail)
	}
	if detail == "" {
		return "", errors.New("docker daemon returned an empty server version")
	}
	return detail, nil
}

// resolveDockerCLI prefers unprivileged Docker access, then accepts only a
// passwordless sudo fallback. It never blocks on an interactive prompt.
func resolveDockerCLI(ctx context.Context) (dockerCLI, error) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return dockerCLI{}, err
	}
	direct := dockerCLI{Executable: docker}
	if version, directErr := dockerServerVersion(ctx, direct); directErr == nil {
		direct.ServerVersion = version
		return direct, nil
	} else if sudo, sudoErr := exec.LookPath("sudo"); sudoErr == nil {
		privileged := dockerCLI{Executable: sudo, Prefix: []string{"-n", docker}}
		if version, privilegedErr := dockerServerVersion(ctx, privileged); privilegedErr == nil {
			privileged.ServerVersion = version
			return privileged, nil
		} else {
			return dockerCLI{}, fmt.Errorf("direct Docker access failed (%v); passwordless sudo Docker access failed (%v)", directErr, privilegedErr)
		}
	} else {
		return dockerCLI{}, fmt.Errorf("direct Docker access failed (%v); sudo is unavailable: %w", directErr, sudoErr)
	}
}

// managedContainerSpec is the complete reproducible Docker contract for one
// simulator-owned dependency. RunArgs precede the image and Command follows it.
type managedContainerSpec struct {
	Name              string
	Image             string
	ConfigurationHash string
	RestartPolicy     string
	RunArgs           []string
	Command           []string
	DataVolumes       []string
	ReadyProbe        []string
	ReadyExpected     string
	ReadyTimeout      time.Duration
}

const managedContainerSpecHashLabel = "com.urnetwork.sim-testnet.spec-hash"

const minimumReleaseStateFreeBytes uint64 = 20 * 1024 * 1024 * 1024

const operatorConnectExchangePortCount = 2

const (
	connectServerBinaryName       = "sim-testnet-connect"
	connectBindServiceCapability  = "cap_net_bind_service=+ep"
	connectBoundServiceCapability = "cap_net_bind_service=ep"
)

// Gives every operator an independent production-style ingress identity. The
// entire 127/8 range is routed by Linux to loopback without host configuration.
func operatorConnectHostIP(operator int) string {
	if operator < 1 || operator > 254 {
		return ""
	}
	return fmt.Sprintf("127.0.1.%d", operator)
}

// Returns the exact noninteractive command which grants only low-port bind
// authority. A private release binary receives the capability; miners,
// validators, APIs, taskworkers, and the supervisor remain unprivileged.
func bindServiceCapabilityCommand(effectiveUserID int, sudoPath, setcapPath, binary string) ([]string, error) {
	if !filepath.IsAbs(setcapPath) || binary == "" || !filepath.IsAbs(binary) {
		return nil, errors.New("bind-service capability command is incomplete")
	}
	if effectiveUserID == 0 {
		return []string{setcapPath, connectBindServiceCapability, binary}, nil
	}
	if !filepath.IsAbs(sudoPath) {
		return nil, errors.New("passwordless sudo is required to install the connect bind-service capability")
	}
	return []string{sudoPath, "-n", setcapPath, connectBindServiceCapability, binary}, nil
}

func validateConnectBindServiceCapability(binary string, output []byte) error {
	want := binary + " " + connectBoundServiceCapability
	if strings.TrimSpace(string(output)) != want {
		return fmt.Errorf("connect server binary capability is %q, require %s", strings.TrimSpace(string(output)), connectBoundServiceCapability)
	}
	return nil
}

// Installs and independently reads back the narrow file capability before any
// chain-capable executor exists. The target is owner-only inside the private
// run directory, so no unrelated executable gains low-port authority.
func installConnectBindServiceCapability(ctx context.Context, binary string) error {
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() {
		return stateMismatchError(err, "connect server binary %s is not a regular file", binary)
	}
	if err := os.Chmod(binary, 0o700); err != nil {
		return err
	}
	setcapPath, err := exec.LookPath("setcap")
	if err != nil {
		return fmt.Errorf("setcap is required for production connect ingress: %w", err)
	}
	sudoPath := ""
	if os.Geteuid() != 0 {
		sudoPath, err = exec.LookPath("sudo")
		if err != nil {
			return fmt.Errorf("sudo is required for production connect ingress: %w", err)
		}
	}
	command, err := bindServiceCapabilityCommand(os.Geteuid(), sudoPath, setcapPath, binary)
	if err != nil {
		return err
	}
	install := exec.CommandContext(ctx, command[0], command[1:]...)
	if output, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("install connect bind-service capability: %w: %s", err, strings.TrimSpace(string(output)))
	}
	getcapPath, err := exec.LookPath("getcap")
	if err != nil {
		return fmt.Errorf("getcap is required for production connect ingress: %w", err)
	}
	inspect := exec.CommandContext(ctx, getcapPath, binary)
	output, err := inspect.CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect connect bind-service capability: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return validateConnectBindServiceCapability(binary, output)
}

// Return available bytes to an unprivileged writer, which is the capacity the
// simulator can actually consume rather than root-reserved filesystem space.
func filesystemFreeBytes(path string) (uint64, error) {
	var state syscall.Statfs_t
	if err := syscall.Statfs(path, &state); err != nil {
		return 0, err
	}
	blocks := uint64(state.Bavail)
	blockSize := uint64(state.Bsize)
	if blockSize != 0 && blocks > ^uint64(0)/blockSize {
		return 0, errors.New("filesystem free-byte count overflows uint64")
	}
	return blocks * blockSize, nil
}

// Keep enough headroom for binaries, isolated databases, process logs, and
// complete evidence even when a soak produces unusually verbose diagnostics.
func validateReleaseStateFreeBytes(available uint64) error {
	if available < minimumReleaseStateFreeBytes {
		return fmt.Errorf("release state filesystem has %d free bytes, require at least %d", available, minimumReleaseStateFreeBytes)
	}
	return nil
}

// Enumerate every simulator-owned TCP listener that must be acquired before
// temporary provisioning or the persistent topology can start.
func releaseProcessListenAddresses(cfg *ResolvedConfig) []string {
	addresses := []string{workloadRPCProxyAddress, workloadRPCProxyHealthAddress, workloadSubstrateProxyAddress, workloadSubstrateHealthAddress}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		for _, port := range []int{18080 + operator, 20080 + operator} {
			addresses = append(addresses, fmt.Sprintf("127.0.0.1:%d", port))
		}
		connectIP := operatorConnectHostIP(operator)
		connectHostPorts := operatorConnectHostPorts(19080 + operator)
		addresses = append(addresses, fmt.Sprintf("%s:%d", connectIP, 19080+operator))
		for servicePort := 5080; servicePort < 5080+operatorConnectExchangePortCount; servicePort++ {
			addresses = append(addresses, fmt.Sprintf("%s:%d", connectIP, connectHostPorts[servicePort]))
		}
		addresses = append(addresses, fmt.Sprintf("127.0.0.1:%d", 22080+operator))
	}
	for swarm := 1; swarm <= cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		addresses = append(addresses, fmt.Sprintf("127.0.0.1:%d", 21080+swarm))
	}
	sort.Strings(addresses)
	return addresses
}

// Enumerate every simulator-owned UDP listener used by the production connect
// transports. Compatibility DNS stays live here so the testnet exercises the
// same listener set expected during the mainnet port migration.
func releaseProcessPacketListenAddresses(cfg *ResolvedConfig) []string {
	addresses := make([]string, 0, 3*cfg.Config.Topology.Operators)
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		connectIP := operatorConnectHostIP(operator)
		connectHostPorts := operatorConnectHostPorts(19080 + operator)
		for _, servicePort := range []int{443, 4053, 8053} {
			addresses = append(addresses, fmt.Sprintf("%s:%d", connectIP, connectHostPorts[servicePort]))
		}
	}
	sort.Strings(addresses)
	return addresses
}

// Bind every required address while no topology owns it. This is deliberately
// immediately pre-apply; a stale or unrelated listener fails before chain work.
func validateAvailableListenAddresses(addresses []string) error {
	return validateAvailableNetworkListenAddresses("tcp", addresses)
}

// Bind every required packet address before the persistent topology starts.
// A conflicting H3 or DNS listener is as fatal as a conflicting HTTP socket.
func validateAvailablePacketListenAddresses(addresses []string) error {
	return validateAvailableNetworkListenAddresses("udp", addresses)
}

// Encodes every exact address separately so neither shell parsing nor a
// delimiter can broaden a privileged bind probe.
func packetListenerProbeArguments(addresses []string) ([]string, error) {
	if len(addresses) == 0 {
		return nil, errors.New("packet listener probe has no addresses")
	}
	arguments := []string{"__listener_probe", "--network=udp"}
	seenAddresses := map[string]bool{}
	for _, address := range addresses {
		if strings.TrimSpace(address) == "" || seenAddresses[address] {
			return nil, fmt.Errorf("packet listener probe has an empty or duplicate address %q", address)
		}
		seenAddresses[address] = true
		arguments = append(arguments, "--address="+address)
	}
	return arguments, nil
}

// Runs the availability check through the capability-scoped binary. The
// ordinary simulator cannot bind UDP/443 or UDP/53, while this child has no
// authority beyond low-port binding and exits before chain execution.
func runPacketListenerProbe(ctx context.Context, binary string, addresses []string, run func(context.Context, string, ...string) ([]byte, error)) error {
	if binary == "" || !filepath.IsAbs(binary) {
		return errors.New("capability-scoped packet listener probe binary is unavailable")
	}
	if run == nil {
		return errors.New("capability-scoped packet listener probe runner is unavailable")
	}
	arguments, err := packetListenerProbeArguments(addresses)
	if err != nil {
		return err
	}
	if output, err := run(ctx, binary, arguments...); err != nil {
		return fmt.Errorf("privileged packet listener probe: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func validateAvailablePacketListenAddressesWithBinary(ctx context.Context, binary string, addresses []string) error {
	return runPacketListenerProbe(ctx, binary, addresses, func(ctx context.Context, binary string, arguments ...string) ([]byte, error) {
		return exec.CommandContext(ctx, binary, arguments...).CombinedOutput()
	})
}

// Reject duplicate allocations before probing the kernel. Connect deliberately
// enables reuse-port, so relying on bind failure alone could hide a collision.
func validateAvailableNetworkListenAddresses(network string, addresses []string) error {
	seenAddresses := map[string]bool{}
	for _, address := range addresses {
		if seenAddresses[address] {
			return fmt.Errorf("duplicate simulator %s listener %s", network, address)
		}
		seenAddresses[address] = true
		if network == "udp" {
			listener, err := net.ListenPacket(network, address)
			if err != nil {
				return fmt.Errorf("required simulator %s listener %s is unavailable: %w", network, address, err)
			}
			if err := listener.Close(); err != nil {
				return fmt.Errorf("close simulator %s listener probe %s: %w", network, address, err)
			}
			continue
		}
		if network != "tcp" {
			return fmt.Errorf("unsupported simulator listener network %q", network)
		}
		listener, err := net.Listen(network, address)
		if err != nil {
			return fmt.Errorf("required simulator %s listener %s is unavailable: %w", network, address, err)
		}
		if err := listener.Close(); err != nil {
			return fmt.Errorf("close simulator %s listener probe %s: %w", network, address, err)
		}
	}
	return nil
}

// Distinguish a healthy already-running deployment from stale files or an
// unrelated process occupying one of its ports.
func currentSupervisorReady(stateDir string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "supervisor.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var manifest SupervisorFile
	if err := json.Unmarshal(b, &manifest); err != nil {
		return false, fmt.Errorf("decode current supervisor manifest: %w", err)
	}
	ready, err := supervisorReadyNow(stateDir, manifest)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return ready, err
}

// Recheck the actual requested state filesystem and listeners after local
// dependencies/builds but before constructing a transaction-capable executor.
func preflightReleaseHost(ctx context.Context, stateDir string, cfg *ResolvedConfig, bins map[string]string) error {
	available, err := filesystemFreeBytes(stateDir)
	if err != nil {
		return fmt.Errorf("inspect release state filesystem: %w", err)
	}
	if err := validateReleaseStateFreeBytes(available); err != nil {
		return err
	}
	if err := validateOperatorConfigOverlays(cfg, stateDir); err != nil {
		return fmt.Errorf("operator config overlay: %w", err)
	}
	ready, err := currentSupervisorReady(stateDir)
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	serverSpecs, err := buildServerSpecs(cfg, stateDir, bins)
	if err != nil {
		return err
	}
	if err := recoverStaleTemporaryProcesses(ctx, stateDir, cfg.Config.Deployment.DeploymentID, selectProvisioningServerSpecs(serverSpecs)); err != nil {
		return fmt.Errorf("recover interrupted provisioning helpers: %w", err)
	}
	if err := validateAvailableListenAddresses(releaseProcessListenAddresses(cfg)); err != nil {
		return err
	}
	return validateAvailablePacketListenAddressesWithBinary(ctx, bins[connectServerBinaryName], releaseProcessPacketListenAddresses(cfg))
}

// postTopologyTournamentActions returns the only setup actions which are
// intentionally deferred until the real operator/miner/validator topology is
// healthy. Keeping this selector pure makes it impossible for launch/resume to
// silently stop at topology.launch while the approved challenger registrations
// remain unexecuted.
func postTopologyTournamentActions(plan *SetupPlan) ([]Action, error) {
	if plan == nil {
		return nil, errors.New("setup plan is unavailable")
	}
	seenTopology := false
	selected := make([]Action, 0)
	for _, action := range plan.Actions {
		if !seenTopology {
			if action.ID == "topology.launch" {
				seenTopology = true
			}
			continue
		}
		if action.ID == "churn.tournament-complete" {
			selected = append(selected, action)
			return selected, nil
		}
		if !strings.HasPrefix(action.ID, "fleet.register.") &&
			!strings.HasPrefix(action.ID, "fleet.commitment.") &&
			!strings.HasPrefix(action.ID, "fleet.mirror.") &&
			!strings.HasPrefix(action.ID, "fleet.bind.") {
			return nil, fmt.Errorf("unexpected post-topology action %s before churn tournament barrier", action.ID)
		}
		selected = append(selected, action)
	}
	if !seenTopology {
		return nil, errors.New("plan has no topology.launch action")
	}
	return nil, errors.New("plan has no churn.tournament-complete action after topology.launch")
}

func executePostTopologyTournament(ctx context.Context, plan *SetupPlan, executor *Executor) error {
	if executor == nil {
		return errors.New("post-topology tournament requires the approved setup executor")
	}
	actions, err := postTopologyTournamentActions(plan)
	if err != nil {
		return err
	}
	for _, action := range actions {
		if err := executor.Execute(ctx, action); err != nil {
			return err
		}
	}
	return nil
}

// Places the complete challenger tournament between two distinct semantic
// proof generations. Injected boundaries make the ordering deterministic in
// regressions without weakening the production checks.
func runPostTopologyTournamentGate(ctx context.Context, baseline func() (map[string]int, error), tournament func(context.Context) error, wait func(context.Context, map[string]int) error) error {
	if ctx == nil || baseline == nil || tournament == nil || wait == nil {
		return errors.New("post-topology tournament readiness dependencies are incomplete")
	}
	proofBaseline, err := baseline()
	if err != nil {
		return err
	}
	if err := tournament(ctx); err != nil {
		return err
	}
	return wait(ctx, proofBaseline)
}

// Executes the approved challenger writes and then proves that every
// validator/operator pair completed another trail without any process or
// supervisor generation change during the tournament.
func executePostTopologyTournamentWithReadiness(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, executor *Executor, supervisor SupervisorFile, supervisorPID int, supervisorStartTimeTicks uint64) error {
	return runPostTopologyTournamentGate(
		ctx,
		func() (map[string]int, error) {
			return releaseTopologyProofCounts(cfg, stateDir)
		},
		func(ctx context.Context) error {
			return executePostTopologyTournament(ctx, plan, executor)
		},
		func(ctx context.Context, baseline map[string]int) error {
			return waitReleaseTopologyReady(ctx, cfg, stateDir, supervisor, supervisorPID, supervisorStartTimeTicks, baseline, 5*time.Minute)
		},
	)
}

// Lists the append-only proof stores which must advance before a newly
// launched topology can claim semantic readiness. Every validator measures
// every operator independently, so omitting one pair would leave a blind
// routing or policy domain behind a process-only health check.
func releaseTopologyProofPaths(cfg *ResolvedConfig, stateDir string) map[string]string {
	paths := map[string]string{}
	if cfg == nil {
		return paths
	}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			identity := fmt.Sprintf("validator-%d/no-%d", validatorID, noID)
			paths[identity] = filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", validatorID), "state", "operators", fmt.Sprintf("no-%d", noID), "proofs.jsonl")
		}
	}
	return paths
}

// Counts complete, unique proof records while ignoring only a concurrently
// appended trailing fragment. A complete malformed, incomplete, or duplicate
// line is durable evidence corruption and fails the release gate instead of
// being silently omitted from its freshness count.
func completedReleaseProofLines(path string) ([][]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	completed := [][]byte{}
	lines := bytes.Split(b, []byte("\n"))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		lines = lines[:len(lines)-1]
	}
	seen := map[connect.Id]bool{}
	for lineIndex, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record struct {
			Version        int        `json:"v"`
			TrailID        connect.Id `json:"trail_id"`
			Coverage       uint64     `json:"coverage"`
			CompleteTimeMS uint64     `json:"complete_time_ms"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("proof line %d is malformed: %w", lineIndex+1, err)
		}
		if record.Version != 1 || record.TrailID == (connect.Id{}) || record.Coverage == 0 || record.CompleteTimeMS == 0 {
			return nil, fmt.Errorf("proof line %d has an incomplete release identity", lineIndex+1)
		}
		if seen[record.TrailID] {
			return nil, fmt.Errorf("proof line %d duplicates trail_id %s", lineIndex+1, record.TrailID)
		}
		seen[record.TrailID] = true
		completed = append(completed, append([]byte(nil), line...))
	}
	return completed, nil
}

func completedReleaseProofCount(path string) (int, error) {
	lines, err := completedReleaseProofLines(path)
	return len(lines), err
}

// Snapshots proof progress for every validator/operator pair.
func releaseTopologyProofCounts(cfg *ResolvedConfig, stateDir string) (map[string]int, error) {
	counts := map[string]int{}
	for identity, path := range releaseTopologyProofPaths(cfg, stateDir) {
		count, err := completedReleaseProofCount(path)
		if err != nil {
			return nil, fmt.Errorf("read %s release proofs: %w", identity, err)
		}
		counts[identity] = count
	}
	return counts, nil
}

// Separates ordinary process liveness from the release gate: initial launch
// tolerates no restart, and each real validator must complete a fresh verified
// trail through each real operator after this launch command began.
func releaseTopologyReady(state SupervisorState, wantHash string, specs []ProcessSpec, supervisorPID int, supervisorStartTimeTicks uint64, baseline, current map[string]int) (bool, error) {
	if state.SupervisorPID != supervisorPID || state.SupervisorStartTimeTicks != supervisorStartTimeTicks {
		return false, fmt.Errorf("release topology supervisor generation changed from pid=%d start=%d to pid=%d start=%d", supervisorPID, supervisorStartTimeTicks, state.SupervisorPID, state.SupervisorStartTimeTicks)
	}
	for _, process := range state.Processes {
		if process.Restarts != 0 {
			return false, fmt.Errorf("release topology process %s restarted %d time(s)", process.ID, process.Restarts)
		}
	}
	if !supervisorStateReady(state, wantHash, specs) {
		return false, nil
	}
	if len(baseline) == 0 || len(current) != len(baseline) {
		return false, errors.New("release topology proof domains are incomplete")
	}
	for identity, prior := range baseline {
		count, ok := current[identity]
		if !ok {
			return false, fmt.Errorf("release topology proof domain %s is missing", identity)
		}
		if count <= prior {
			return false, nil
		}
	}
	return true, nil
}

// Waits for semantic topology evidence without accepting stale proofs from an
// earlier attempt. A restart is terminal immediately; ordinary startup and
// trail progress may continue until the bounded launch deadline.
func waitReleaseTopologyReady(ctx context.Context, cfg *ResolvedConfig, stateDir string, want SupervisorFile, supervisorPID int, supervisorStartTimeTicks uint64, baseline map[string]int, timeout time.Duration) error {
	wantHash, err := canonicalHashHex(want)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		var state SupervisorState
		if err := readJSONFile(filepath.Join(stateDir, "supervisor.state.json"), &state); err != nil {
			lastErr = err
		} else if current, err := releaseTopologyProofCounts(cfg, stateDir); err != nil {
			lastErr = err
		} else if ready, err := releaseTopologyReady(state, wantHash, want.Specs, supervisorPID, supervisorStartTimeTicks, baseline, current); err != nil {
			return err
		} else if ready {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining > 500*time.Millisecond {
			remaining = 500 * time.Millisecond
		}
		if remaining <= 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(remaining):
		}
	}
	if lastErr != nil {
		return fmt.Errorf("release topology semantic readiness timeout: %w", lastErr)
	}
	return errors.New("release topology semantic readiness timeout: every validator must complete a fresh verified trail through every operator")
}

// Provision API-assigned identities, finish their chain bindings, then hand
// the checksum-locked process topology to the persistent supervisor.
func LaunchDeployment(ctx context.Context, cfg *ResolvedConfig, stateDir string, p *SetupPlan, roles *RoleSecrets, executor *Executor, bins map[string]string, detach bool) error {
	if len(bins) == 0 {
		return errors.New("launch requires preflighted release binaries")
	}
	if err := runDatabaseMigrations(ctx, cfg, stateDir, bins["server-ctl"]); err != nil {
		return err
	}
	serverSpecs, err := buildServerSpecs(cfg, stateDir, bins)
	if err != nil {
		return err
	}
	provisioningSpecs := selectProvisioningServerSpecs(serverSpecs)
	temporary, err := startTemporary(ctx, stateDir, cfg.Config.Deployment.DeploymentID, provisioningSpecs)
	if err != nil {
		return err
	}
	defer stopTemporaryCommands(stateDir, temporary)
	if err := waitSpecsReady(ctx, provisioningSpecs, 2*time.Minute); err != nil {
		return err
	}
	if err := provisionSimulationAccounts(ctx, cfg, stateDir, roles); err != nil {
		return err
	}
	if executor == nil {
		return fmt.Errorf("launch requires the approved setup executor")
	}
	// The public identity view changes only by filling server-assigned UUIDs;
	// key material remains fixed. Persist it before the signed fleet manifest.
	pub, _ := json.MarshalIndent(roles.Public(), "", "  ")
	if err := atomicWrite(filepath.Join(stateDir, "public", "identities.json"), append(pub, '\n'), 0o644); err != nil {
		return err
	}
	postProvision := false
	for _, action := range p.Actions {
		if action.ID == "accounts.provision" {
			postProvision = true
		}
		if !postProvision {
			continue
		}
		if action.ID == "topology.launch" {
			break
		}
		if err := executor.Execute(ctx, action); err != nil {
			return err
		}
	}
	// Fill the now-known client IDs and anchored fleet state into component
	// configs atomically while the temporary APIs are still the only services.
	if err := RenderRuntimeConfigs(cfg, stateDir, roles); err != nil {
		return err
	}
	stopTemporaryCommands(stateDir, temporary)
	specs := append(serverSpecs, buildClientSpecs(cfg, stateDir, bins, roles)...)
	binaryHash, err := fileSHA256(bins["sim-testnet"])
	if err != nil {
		return err
	}
	sf := SupervisorFile{Schema: "urnetwork-sim-supervisor-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, BinaryHash: binaryHash, Specs: specs}
	b, _ := json.MarshalIndent(sf, "", "  ")
	specPath := filepath.Join(stateDir, "supervisor.json")
	if err := atomicWrite(specPath, append(b, '\n'), 0o600); err != nil {
		return err
	}
	var topologyAction *Action
	for i := range p.Actions {
		if p.Actions[i].ID == "topology.launch" {
			topologyAction = &p.Actions[i]
			break
		}
	}
	if topologyAction == nil {
		return fmt.Errorf("plan has no topology.launch action")
	}
	proofBaseline, err := releaseTopologyProofCounts(cfg, stateDir)
	if err != nil {
		return err
	}
	if detach || cfg.Config.Deployment.DetachAfterLaunch {
		if ready, _ := supervisorReadyNow(stateDir, sf); !ready {
			if err := startPersistentSupervisor(ctx, cfg, bins["sim-testnet"], stateDir, specPath); err != nil {
				return err
			}
		}
		readyState, err := waitSupervisorReady(ctx, stateDir, sf, 3*time.Minute)
		if err != nil {
			return err
		}
		if err := waitReleaseTopologyReady(ctx, cfg, stateDir, sf, readyState.SupervisorPID, readyState.SupervisorStartTimeTicks, proofBaseline, 5*time.Minute); err != nil {
			return err
		}
		if err := executor.Execute(ctx, *topologyAction); err != nil {
			return err
		}
		if err := executePostTopologyTournamentWithReadiness(ctx, cfg, stateDir, p, executor, sf, readyState.SupervisorPID, readyState.SupervisorStartTimeTicks); err != nil {
			return err
		}
		return publishDeploymentEvidence(ctx, cfg, stateDir, p, roles)
	}
	supervisorCtx, cancelSupervisor := context.WithCancel(ctx)
	defer cancelSupervisor()
	supervisorErr := make(chan error, 1)
	go func() { supervisorErr <- supervise(supervisorCtx, stateDir, specPath) }()
	readyState, err := waitSupervisorReady(ctx, stateDir, sf, 3*time.Minute)
	if err != nil {
		cancelSupervisor()
		<-supervisorErr
		return err
	}
	if err := waitReleaseTopologyReady(ctx, cfg, stateDir, sf, readyState.SupervisorPID, readyState.SupervisorStartTimeTicks, proofBaseline, 5*time.Minute); err != nil {
		cancelSupervisor()
		<-supervisorErr
		return err
	}
	if err := executor.Execute(ctx, *topologyAction); err != nil {
		cancelSupervisor()
		<-supervisorErr
		return err
	}
	if err := executePostTopologyTournamentWithReadiness(ctx, cfg, stateDir, p, executor, sf, readyState.SupervisorPID, readyState.SupervisorStartTimeTicks); err != nil {
		cancelSupervisor()
		<-supervisorErr
		return err
	}
	if err := publishDeploymentEvidence(ctx, cfg, stateDir, p, roles); err != nil {
		cancelSupervisor()
		<-supervisorErr
		return err
	}
	return <-supervisorErr
}

func publishDeploymentEvidence(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, roles *RoleSecrets) error {
	manifest, err := writePublicDeploymentManifest(cfg, stateDir, plan)
	if err != nil {
		return err
	}
	if !cfg.Config.Analysis.PublishPublicManifest {
		return nil
	}
	published, err := publishEvidence(ctx, cfg, roles, stateDir, "deployment-manifest", "", manifest)
	if err != nil {
		return err
	}
	if err := verifyPublishedEvidenceOrigins(ctx, cfg, roles, published); err != nil {
		return err
	}
	locators := make([]map[string]any, 0, len(published))
	for i, receipt := range published {
		locators = append(locators, map[string]any{
			"operator_no_id": i + 1,
			"content_hash":   receipt.ContentHash,
			"url":            strings.TrimSuffix(cfg.OperatorAPIOrigins[i], "/") + "/sn/evidence?hash=" + receipt.ContentHash,
		})
	}
	return writePublicJSON(filepath.Join(stateDir, "public", "deployment-manifest.locators.json"), map[string]any{"schema": "urnetwork-public-manifest-locators-v1", "locators": locators})
}

func buildReleaseBinaries(ctx context.Context, cfg *ResolvedConfig, stateDir string) (map[string]string, error) {
	out := filepath.Join(stateDir, "build")
	if err := os.MkdirAll(out, 0o700); err != nil {
		return nil, err
	}
	targets := []struct{ name, dir, pkg string }{{"sim-testnet", cfg.Repos.SN, "./sim-testnet"}, {"server-ctl", cfg.Repos.Server, "./bringyourctl"}}
	result := map[string]string{}
	for _, t := range targets {
		path := filepath.Join(out, t.name)
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags=-buildid=", "-o", path, t.pkg)
		cmd.Dir = t.dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("build %s: %w: %s", t.name, err, redactText(string(output), cfg.WalletSecret, cfg.WalletMaterial, cfg.WalletPasswordSecret, cfg.WalletPassword))
		}
		result[t.name] = path
	}
	connectServerBinary := filepath.Join(out, connectServerBinaryName)
	if err := copyFile(result["sim-testnet"], connectServerBinary, 0o700); err != nil {
		return nil, fmt.Errorf("prepare private connect server binary: %w", err)
	}
	if err := installConnectBindServiceCapability(ctx, connectServerBinary); err != nil {
		return nil, err
	}
	connectServerHash, err := fileSHA256(connectServerBinary)
	if err != nil {
		return nil, err
	}
	simulatorHash, err := fileSHA256(result["sim-testnet"])
	if err != nil {
		return nil, err
	}
	if connectServerHash != simulatorHash {
		return nil, errors.New("capability-scoped connect binary bytes differ from the release simulator")
	}
	result[connectServerBinaryName] = connectServerBinary
	return result, nil
}

// startDependencies launches one isolated server/local-compatible PostgreSQL
// and Redis pair per operator. MinIO and Subtensor deliberately remain shared
// external services.
func startDependencies(ctx context.Context, cfg *ResolvedConfig) error {
	docker, err := resolveDockerCLI(ctx)
	if err != nil {
		return fmt.Errorf("managed_containers requires a usable Docker daemon: %w", err)
	}
	specs, err := dependencyContainerSpecs(cfg)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := ensureContainer(ctx, docker, spec); err != nil {
			return err
		}
		if err := waitContainerReady(ctx, docker, spec); err != nil {
			return err
		}
	}
	return nil
}

// dependencyContainerSpecs mirrors the operational settings in server/local
// while assigning every operator distinct loopback ports, names, and data.
func dependencyContainerSpecs(cfg *ResolvedConfig) ([]managedContainerSpec, error) {
	if cfg == nil || cfg.Release == nil {
		return nil, errors.New("managed dependency config is incomplete")
	}
	postgresImage := strings.TrimSpace(cfg.Release.Dependencies["postgres"])
	redisImage := strings.TrimSpace(cfg.Release.Dependencies["redis"])
	if postgresImage == "" || redisImage == "" {
		return nil, errors.New("managed dependency images are missing from the release lock")
	}
	initDir := filepath.Join(cfg.Repos.Server, "local", "postgres", "initdb")
	if info, err := os.Stat(initDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("server/local PostgreSQL init directory is unavailable: %s", initDir)
	}
	localConfigHash, err := serverLocalDependencyConfigHash(cfg.Repos.Server)
	if err != nil {
		return nil, fmt.Errorf("hash server/local dependency configuration: %w", err)
	}
	specs := make([]managedContainerSpec, 0, cfg.Config.Topology.Operators*2)
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		ip := fmt.Sprintf("127.0.0.%d", 10+i)
		pgName := fmt.Sprintf("%s-pg-%d", cfg.Config.Deployment.DeploymentID, i)
		redisName := fmt.Sprintf("%s-redis-%d", cfg.Config.Deployment.DeploymentID, i)
		pgSeed := derive32(cfg, fmt.Sprintf("dependency/postgres-%d", i))
		pgPassword := hex.EncodeToString(pgSeed[:])
		pgSuperuserSeed := derive32(cfg, fmt.Sprintf("dependency/postgres-superuser-%d", i))
		pgSuperuserPassword := hex.EncodeToString(pgSuperuserSeed[:])
		postgresArgs := []string{
			"--mount", "type=volume,src=" + pgName + "-data,dst=/var/lib/postgresql",
			"--mount", "type=bind,src=" + initDir + ",dst=/docker-entrypoint-initdb.d,readonly",
			"-e", "LANG=en_US.UTF-8",
			"-e", "POSTGRES_INITDB_ARGS=--locale=en_US.UTF-8",
			"-e", "POSTGRES_USER=postgres",
			"-e", "POSTGRES_PASSWORD=" + pgSuperuserPassword,
			"-e", "POSTGRES_DB=postgres",
			"-e", "APP_DB_USER=bringyour",
			"-e", "APP_DB_PASSWORD=" + pgPassword,
			"-e", "APP_DB_NAME=bringyour",
			"-p", ip + ":5432:5432",
		}
		postgresCommand := []string{"postgres", "-c", "max_connections=512", "-c", "shared_buffers=256MB"}
		specs = append(specs, managedContainerSpec{
			Name:              pgName,
			Image:             postgresImage,
			ConfigurationHash: localConfigHash,
			RestartPolicy:     "no",
			RunArgs:           postgresArgs,
			Command:           postgresCommand,
			DataVolumes:       []string{pgName + "-data"},
			ReadyProbe: []string{
				"env", "PGPASSWORD=" + pgPassword,
				"psql", "-h", "127.0.0.1", "-U", "bringyour", "-d", "bringyour", "-Atqc",
				"SELECT current_setting('max_connections') || ':' || current_setting('shared_buffers') || ':' || datcollate FROM pg_database WHERE datname=current_database()",
			},
			ReadyExpected: "512:256MB:en_US.UTF-8",
			ReadyTimeout:  90 * time.Second,
		})
		redisArgs := []string{
			"--ulimit", "nofile=65536:65536",
			"--sysctl", "net.core.somaxconn=65535",
			"--sysctl", "net.ipv4.tcp_max_syn_backlog=65535",
			"-p", ip + ":6379:6379",
		}
		redisCommand := []string{"redis-server", "--io-threads", "8", "--io-threads-do-reads", "yes", "--maxclients", "32768", "--tcp-backlog", "65535", "--save", "", "--appendonly", "no"}
		specs = append(specs, managedContainerSpec{
			Name:              redisName,
			Image:             redisImage,
			ConfigurationHash: localConfigHash,
			RestartPolicy:     "no",
			RunArgs:           redisArgs,
			Command:           redisCommand,
			ReadyProbe:        []string{"redis-cli", "ping"},
			ReadyExpected:     "PONG",
			ReadyTimeout:      30 * time.Second,
		})
	}
	return specs, nil
}

// managedContainerSpecHash binds the image and every creation argument. It
// prevents an old same-image container from silently reusing stale settings.
func managedContainerSpecHash(spec managedContainerSpec) (string, error) {
	canonical := struct {
		Image             string   `json:"image"`
		ConfigurationHash string   `json:"configuration_hash"`
		RestartPolicy     string   `json:"restart_policy"`
		RunArgs           []string `json:"run_args"`
		Command           []string `json:"command"`
		DataVolumes       []string `json:"data_volumes"`
	}{
		Image:             spec.Image,
		ConfigurationHash: spec.ConfigurationHash,
		RestartPolicy:     spec.RestartPolicy,
		RunArgs:           spec.RunArgs,
		Command:           spec.Command,
		DataVolumes:       spec.DataVolumes,
	}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Bind only settings that determine persistent data compatibility. Container
// lifecycle policy can change without invalidating an initialized database.
func managedContainerDataSpecHash(spec managedContainerSpec) (string, error) {
	canonical := struct {
		Image             string   `json:"image"`
		ConfigurationHash string   `json:"configuration_hash"`
		RunArgs           []string `json:"run_args"`
		Command           []string `json:"command"`
		DataVolumes       []string `json:"data_volumes"`
	}{
		Image:             spec.Image,
		ConfigurationHash: spec.ConfigurationHash,
		RunArgs:           spec.RunArgs,
		Command:           spec.Command,
		DataVolumes:       spec.DataVolumes,
	}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ensureContainer creates or starts a simulator-owned container only when its
// digest-pinned image and complete creation spec match the requested release.
func ensureContainer(ctx context.Context, docker dockerCLI, spec managedContainerSpec) error {
	if spec.RestartPolicy != "no" {
		return fmt.Errorf("container %s restart policy is %q, want no", spec.Name, spec.RestartPolicy)
	}
	specHash, err := managedContainerSpecHash(spec)
	if err != nil {
		return fmt.Errorf("hash container %s spec: %w", spec.Name, err)
	}
	dataSpecHash, err := managedContainerDataSpecHash(spec)
	if err != nil {
		return fmt.Errorf("hash container %s data spec: %w", spec.Name, err)
	}
	for _, volume := range spec.DataVolumes {
		if err := ensureManagedVolume(ctx, docker, volume, dataSpecHash); err != nil {
			return err
		}
	}
	inspect := docker.commandContext(ctx, "container", "inspect", "--format", "{{.Config.Image}}|{{index .Config.Labels \""+managedContainerSpecHashLabel+"\"}}", spec.Name)
	if out, inspectErr := inspect.CombinedOutput(); inspectErr == nil {
		parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
		if len(parts) != 2 || parts[0] != spec.Image || parts[1] != specHash {
			return fmt.Errorf("container %s does not match its release-locked image and creation spec; remove that simulator container before retrying", spec.Name)
		}
		start := docker.commandContext(ctx, "start", spec.Name)
		if out, err := start.CombinedOutput(); err != nil {
			return fmt.Errorf("start %s: %w: %s", spec.Name, err, out)
		}
		return nil
	} else if detail := strings.ToLower(string(out)); !strings.Contains(detail, "no such object") && !strings.Contains(detail, "no such container") {
		return fmt.Errorf("inspect container %s: %w: %s", spec.Name, inspectErr, strings.TrimSpace(string(out)))
	}
	cmdArgs := []string{"run", "-d", "--name", spec.Name, "--restart", spec.RestartPolicy, "--label", managedContainerSpecHashLabel + "=" + specHash}
	cmdArgs = append(cmdArgs, spec.RunArgs...)
	cmdArgs = append(cmdArgs, spec.Image)
	cmdArgs = append(cmdArgs, spec.Command...)
	cmd := docker.commandContext(ctx, cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("run %s: %w: %s", spec.Name, err, out)
	}
	return nil
}

// ensureManagedVolume prevents PostgreSQL from silently reusing data created
// by another release, init-hook set, password derivation, or container spec.
func ensureManagedVolume(ctx context.Context, docker dockerCLI, name, specHash string) error {
	inspect := docker.commandContext(ctx, "volume", "inspect", "--format", "{{index .Labels \""+managedContainerSpecHashLabel+"\"}}", name)
	out, err := inspect.CombinedOutput()
	if err == nil {
		if strings.TrimSpace(string(out)) != specHash {
			return fmt.Errorf("volume %s does not match its release-locked creation spec; remove that exact simulator volume before retrying", name)
		}
		return nil
	}
	if detail := strings.ToLower(string(out)); !strings.Contains(detail, "no such volume") {
		return fmt.Errorf("inspect volume %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	create := docker.commandContext(ctx, "volume", "create", "--label", managedContainerSpecHashLabel+"="+specHash, name)
	if out, err := create.CombinedOutput(); err != nil {
		return fmt.Errorf("create volume %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// waitContainerReady polls an in-container dependency probe until its bounded
// startup deadline or caller cancellation.
func waitContainerReady(ctx context.Context, docker dockerCLI, spec managedContainerSpec) error {
	deadline := time.Now().Add(spec.ReadyTimeout)
	var lastOutput string
	var lastErr error
	for time.Now().Before(deadline) {
		args := append([]string{"exec", spec.Name}, spec.ReadyProbe...)
		output, err := docker.commandContext(ctx, args...).CombinedOutput()
		lastOutput = strings.TrimSpace(string(output))
		lastErr = err
		if err == nil && (spec.ReadyExpected == "" || lastOutput == spec.ReadyExpected) {
			return nil
		}
		pause := time.Second
		if remaining := time.Until(deadline); remaining < pause {
			pause = remaining
		}
		if pause <= 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
	if lastErr != nil {
		return fmt.Errorf("container %s readiness timeout: %w", spec.Name, lastErr)
	}
	return fmt.Errorf("container %s readiness output %q, want %q", spec.Name, lastOutput, spec.ReadyExpected)
}

// Reproduces the production v21 ingress on an operator's distinct loopback
// IP: H3 is direct on UDP/443, public DNS/53 forwards to the private 4053
// service, and 8053 remains available only for rolling compatibility.
func operatorConnectHostPorts(statusPort int) map[int]int {
	hostPorts := map[int]int{
		443:        443,
		4053:       53,
		8053:       8053,
		statusPort: statusPort,
	}
	for offset := 0; offset < operatorConnectExchangePortCount; offset++ {
		hostPorts[5080+offset] = 5080 + offset
	}
	return hostPorts
}

// Encode Warp's service-to-host mapping in stable service-port order so the
// supervisor manifest and its hash do not depend on map iteration order.
func operatorConnectWarpPorts(statusPort int) string {
	hostPorts := operatorConnectHostPorts(statusPort)
	servicePorts := make([]int, 0, len(hostPorts))
	for servicePort := range hostPorts {
		servicePorts = append(servicePorts, servicePort)
	}
	sort.Ints(servicePorts)
	portPairs := make([]string, 0, len(servicePorts))
	for _, servicePort := range servicePorts {
		portPairs = append(portPairs, fmt.Sprintf("%d:%d", servicePort, hostPorts[servicePort]))
	}
	return strings.Join(portPairs, ",")
}

func buildServerSpecs(cfg *ResolvedConfig, stateDir string, bins map[string]string) ([]ProcessSpec, error) {
	if bins["sim-testnet"] == "" {
		return nil, errors.New("workload RPC proxy requires the sim-testnet release binary")
	}
	if bins[connectServerBinaryName] == "" {
		return nil, errors.New("operator connect requires the capability-scoped release binary")
	}
	proxyInputs := []struct {
		id, identity, endpoint, listen, health string
	}{
		{id: workloadRPCProxyProcessID, identity: "operational-evm-rpc", endpoint: cfg.OperationalEVM, listen: workloadRPCProxyAddress, health: workloadRPCProxyHealthAddress},
		{id: workloadSubstrateProcessID, identity: "operational-substrate-rpc", endpoint: cfg.OperationalSubstrate, listen: workloadSubstrateProxyAddress, health: workloadSubstrateHealthAddress},
	}
	out := make([]ProcessSpec, 0, 2+3*cfg.Config.Topology.Operators)
	for _, input := range proxyInputs {
		proxy, err := rpcProxyConfigForEndpoint(input.endpoint, input.listen, input.health)
		if err != nil {
			return nil, fmt.Errorf("workload %s proxy: %w", input.identity, err)
		}
		if input.id == workloadRPCProxyProcessID {
			proxy.MaximumRequestsPerMinute = configuredEVMRequestsPerMinute(cfg, input.endpoint)
		}
		if err := validateRPCProxyConfig(proxy); err != nil {
			return nil, fmt.Errorf("workload %s proxy: %w", input.identity, err)
		}
		proxyArgs := []string{"__rpc_proxy", "--listen=" + proxy.ListenAddress, "--health=" + proxy.HealthAddress, "--upstream=" + proxy.Upstream}
		if proxy.TLSServerName != "" {
			proxyArgs = append(proxyArgs, "--tls-server-name="+proxy.TLSServerName)
		}
		if proxy.HTTP {
			proxyArgs = append(proxyArgs, "--http")
		}
		if proxy.MaximumRequestsPerMinute > 0 {
			proxyArgs = append(proxyArgs, fmt.Sprintf("--maximum-requests-per-minute=%d", proxy.MaximumRequestsPerMinute))
		}
		out = append(out, ProcessSpec{
			ID: input.id, Role: "dependency-rpc-proxy", Identity: input.identity,
			Command: bins["sim-testnet"], Args: proxyArgs, WorkDir: cfg.Repos.SN,
			StdoutPath: filepath.Join(stateDir, "processes", input.id+".stdout.log"),
			StderrPath: filepath.Join(stateDir, "processes", input.id+".stderr.log"),
			HealthURL:  "http://" + input.health + "/healthz", RestartLimit: 5,
		})
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		ip := fmt.Sprintf("127.0.0.%d", 10+i)
		baseEnv := operatorBaseEnv(cfg, stateDir, i, ip)
		for _, svc := range []struct {
			role, bin, module string
			port              int
		}{{"api", "sim-testnet", "__server_api", 18080 + i}, {"connect", connectServerBinaryName, "__server_connect", 19080 + i}, {"taskworker", "sim-testnet", "__server_taskworker", 20080 + i}} {
			env := cloneStrings(baseEnv)
			env["WARP_SERVICE"] = svc.role
			listenIP := "127.0.0.1"
			env["WARP_HOST_IPV4"] = listenIP
			env["WARP_PORTS"] = fmt.Sprintf("%d:%d", svc.port, svc.port)
			if svc.role == "connect" {
				listenIP = operatorConnectHostIP(i)
				if listenIP == "" {
					return nil, fmt.Errorf("operator %d has no loopback connect identity", i)
				}
				env["WARP_HOST"] = listenIP
				env["WARP_HOST_IPV4"] = listenIP
				env["WARP_PORTS"] = operatorConnectWarpPorts(svc.port)
			}
			id := fmt.Sprintf("operator-%d-%s", i, svc.role)
			args := []string{fmt.Sprintf("--port=%d", svc.port)}
			if svc.module != "" {
				args = append([]string{svc.module}, args...)
			}
			if svc.role == "connect" {
				args = append(args, "--tls-default-host="+listenIP, "--direct-h3-loopback")
			}
			spec := ProcessSpec{ID: id, Role: "operator-" + svc.role, Identity: fmt.Sprintf("no:%d", i), Command: bins[svc.bin], Args: args, WorkDir: cfg.Repos.Server, Env: env, StdoutPath: filepath.Join(stateDir, "processes", id+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", id+".stderr.log"), HealthURL: fmt.Sprintf("http://%s:%d/status", listenIP, svc.port), RestartLimit: 5}
			if svc.role == "connect" {
				spec.H3ProbeAddress = net.JoinHostPort(listenIP, "443")
				spec.H3ProbeServerName = listenIP
				spec.H3ProbeCAFile = operatorConnectCAFile(stateDir)
			}
			out = append(out, spec)
		}
	}
	return out, nil
}

// Account provisioning requires only each operator's API. Starting connect
// and taskworker here would create two owners for the same durable EVM signer
// nonce before setup has completed. The full server set starts exactly once
// under the persistent supervisor after every planned setup transaction.
func selectProvisioningServerSpecs(specs []ProcessSpec) []ProcessSpec {
	out := make([]ProcessSpec, 0)
	for _, spec := range specs {
		if spec.Role == "dependency-rpc-proxy" || spec.Role == "operator-api" {
			out = append(out, spec)
		}
	}
	return out
}

func operatorBaseEnv(cfg *ResolvedConfig, stateDir string, operator int, ip string) map[string]string {
	root := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator))
	return map[string]string{"WARP_ENV": operatorEnvironment(operator), "WARP_VERSION": "1.0", "WARP_BLOCK": fmt.Sprintf("sim%d", operator), "WARP_HOST": "127.0.0.1", "WARP_VAULT_HOME": filepath.Join(root, "vault"), "WARP_CONFIG_HOME": operatorConfigHome(stateDir, operator), "WARP_SITE_HOME": filepath.Join(root, "site"), "BRINGYOUR_POSTGRES_HOSTNAME": ip, "BRINGYOUR_REDIS_HOSTNAME": ip, "BRINGYOUR_SUBTENSOR_HOSTNAME": workloadRPCAuthority(), "BRINGYOUR_MINIO_HOSTNAME": cfg.ObjectStoreHost, "URNETWORK_ST_PROFILE": "testnet"}
}

func runDatabaseMigrations(ctx context.Context, cfg *ResolvedConfig, stateDir, binary string) error {
	if binary == "" {
		return fmt.Errorf("server migration binary is missing")
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		ip := fmt.Sprintf("127.0.0.%d", 10+i)
		cmd := exec.CommandContext(ctx, binary, "db", "migrate")
		cmd.Dir = cfg.Repos.Server
		cmd.Env = envList(operatorBaseEnv(cfg, stateDir, i, ip))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("operator %d database migrations: %w: %s", i, err, redactText(string(output), cfg.WalletSecret, cfg.WalletMaterial, cfg.WalletPasswordSecret, cfg.WalletPassword))
		}
		if err := atomicWrite(filepath.Join(stateDir, "processes", fmt.Sprintf("operator-%d-db-migrate.log", i)), output, 0o600); err != nil {
			return err
		}
	}
	return nil
}
func buildClientSpecs(cfg *ResolvedConfig, stateDir string, bins map[string]string, roles *RoleSecrets) []ProcessSpec {
	var out []ProcessSpec
	connectClientEnv := map[string]string{
		"WARP_VERSION":             "1.0",
		connect.ExtraRootCAFileEnv: operatorConnectCAFile(stateDir),
	}
	minersPerSwarm := cfg.Config.Topology.Miners / cfg.Config.Topology.MinerSwarmProcesses
	for swarm := 1; swarm <= cfg.Config.Topology.MinerSwarmProcesses; swarm++ {
		id := fmt.Sprintf("miner-swarm-%d", swarm)
		first := (swarm-1)*minersPerSwarm + 1
		last := swarm * minersPerSwarm
		out = append(out, ProcessSpec{
			ID: id, Role: "miner-swarm", Identity: fmt.Sprintf("miners:%d-%d", first, last), Command: bins["sim-testnet"],
			Args:    []string{"__miner_swarm", "--config=" + filepath.Join(stateDir, "runtime", id, "swarm.json")},
			WorkDir: cfg.Repos.SN, Env: cloneStrings(connectClientEnv),
			StdoutPath: filepath.Join(stateDir, "processes", id+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", id+".stderr.log"),
			HealthURL: fmt.Sprintf("http://127.0.0.1:%d/status", 21080+swarm), RestartLimit: 5,
		})
	}
	for operator := 1; operator <= cfg.Config.Topology.Operators; operator++ {
		id := fmt.Sprintf("claim-relayer-%d", operator)
		out = append(out, ProcessSpec{
			ID: id, Role: "claim-relayer", Identity: fmt.Sprintf("no:%d", operator), Command: bins["sim-testnet"],
			Args:    []string{"__claim_swarm", "--config=" + filepath.Join(stateDir, "runtime", id, "swarm.json")},
			WorkDir: cfg.Repos.SN, Env: map[string]string{"WARP_VERSION": "1.0"},
			StdoutPath: filepath.Join(stateDir, "processes", id+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", id+".stderr.log"),
			HealthURL: fmt.Sprintf("http://127.0.0.1:%d/status", 22080+operator), RestartLimit: 5,
		})
	}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		id := fmt.Sprintf("validator-%d", i)
		args := []string{"__validator", "--config=" + filepath.Join(stateDir, "runtime", id, "validator.yml")}
		env := cloneStrings(connectClientEnv)
		env["URNETWORK_STATE_DIR"] = filepath.Join(stateDir, "runtime", id, "state")
		out = append(out, ProcessSpec{ID: id, Role: "validator", Identity: roles.Substrate[validatorHotkeyLabel(i)].SS58, Command: bins["sim-testnet"], Args: args, WorkDir: cfg.Repos.SN, Env: env, StdoutPath: filepath.Join(stateDir, "processes", id+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", id+".stderr.log"), RestartLimit: 5})
	}
	return out
}

// minerTestEgressSourceIP gives every logical provider a distinct loopback
// address. One member of each of the first two fleets shares a /29 while their
// other members remain unique, proving a 1/2 split without pushing those
// fleets below the top-200 boundary. Each four-member challenger collapses to
// one distinct /29, so both remain positive but deterministically rank 201st
// and 202nd until a selected fleet is degraded by the boundary fault.
func minerTestEgressSourceIP(miner int) string {
	if miner < 1 {
		return ""
	}
	switch {
	case miner == 1:
		return "127.64.0.1"
	case miner == 5:
		return "127.64.0.2"
	case miner >= 801 && miner <= 804:
		return fmt.Sprintf("127.64.1.%d", miner-800)
	case miner >= 805 && miner <= 808:
		return fmt.Sprintf("127.64.2.%d", miner-804)
	}
	index := miner - 1
	second := 65 + index/(32*256)
	third := (index / 32) % 256
	last := (index%32)*8 + 1
	if second > 254 {
		return ""
	}
	return fmt.Sprintf("127.%d.%d.%d", second, third, last)
}

// operatorForMiner keeps every head fleet within one operator so affiliation
// masking cannot contaminate every head UID. Whole fleets and tail miners are
// distributed round-robin, preserving the configured balanced topology.
func operatorForMiner(cfg *ResolvedConfig, miner int) int {
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.Operators <= 0 || miner <= 0 || miner > cfg.Config.Topology.Miners {
		return 0
	}
	topology := cfg.Config.Topology
	headMembers := topology.fleetCandidateMiners()
	if miner <= headMembers {
		fleet := 1 + (miner-1)/topology.ClientsPerHeadFleet
		return 1 + (fleet-1)%topology.Operators
	}
	tail := miner - headMembers
	return 1 + (tail-1)%topology.Operators
}

func stripScheme(s string) string {
	w, _, err := authorityURLs(s)
	if err != nil {
		return s
	}
	return strings.TrimPrefix(strings.TrimPrefix(w, "ws://"), "wss://")
}
func cloneStrings(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func envList(extra map[string]string) []string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	for k, v := range extra {
		m[k] = v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func temporaryProcessFilePath(stateDir string) string {
	return filepath.Join(stateDir, "temporary-processes.json")
}

func processStartTimeTicks(pid int) (uint64, error) {
	statBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	closing := strings.LastIndexByte(string(statBytes), ')')
	if closing < 0 {
		return 0, fmt.Errorf("process %d stat has no command boundary", pid)
	}
	fields := strings.Fields(string(statBytes[closing+1:]))
	if len(fields) <= 19 {
		return 0, fmt.Errorf("process %d stat is truncated", pid)
	}
	startTimeTicks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || startTimeTicks == 0 {
		return 0, fmt.Errorf("process %d start time is invalid", pid)
	}
	return startTimeTicks, nil
}

// Linux field 22 is stable across exec and, together with the process group,
// executable and argv, distinguishes an orphan from a reused PID.
func temporaryProcessIdentity(spec ProcessSpec, pid int) (TemporaryProcessIdentity, error) {
	startTimeTicks, err := processStartTimeTicks(pid)
	if err != nil {
		return TemporaryProcessIdentity{}, err
	}
	processGroupID, err := syscall.Getpgid(pid)
	if err != nil {
		return TemporaryProcessIdentity{}, err
	}
	executableHash, err := fileSHA256(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return TemporaryProcessIdentity{}, err
	}
	commandLine, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(commandLine) == 0 {
		return TemporaryProcessIdentity{}, stateMismatchError(err, "process %d command line is empty", pid)
	}
	commandLineHashBytes := sha256.Sum256(commandLine)
	return TemporaryProcessIdentity{
		ID: spec.ID, Role: spec.Role, Identity: spec.Identity, PID: pid,
		ProcessGroupID: processGroupID, StartTimeTicks: startTimeTicks,
		ExecutableHash: executableHash, CommandLineHash: hex.EncodeToString(commandLineHashBytes[:]),
	}, nil
}

func sameTemporaryProcessIdentity(left, right TemporaryProcessIdentity) bool {
	return left.PID == right.PID && left.ProcessGroupID == right.ProcessGroupID && left.StartTimeTicks == right.StartTimeTicks &&
		left.ExecutableHash == right.ExecutableHash && left.CommandLineHash == right.CommandLineHash
}

func writeTemporaryProcessFile(stateDir string, state TemporaryProcessFile) error {
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(temporaryProcessFilePath(stateDir), append(encoded, '\n'), 0o600)
}

// Signals only exact process identities recorded by this deployment. A PID
// mismatch is treated as normal PID reuse and is never signalled.
func recoverStaleTemporaryProcesses(ctx context.Context, stateDir, deploymentID string, specs []ProcessSpec) error {
	path := temporaryProcessFilePath(stateDir)
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state TemporaryProcessFile
	if err := json.Unmarshal(encoded, &state); err != nil {
		return err
	}
	if state.Schema != temporaryProcessFileSchema || state.DeploymentID != deploymentID || len(state.Processes) == 0 {
		return errors.New("temporary process ownership file has an invalid identity")
	}
	expected := make(map[string]ProcessSpec, len(specs))
	for _, spec := range specs {
		expected[spec.ID] = spec
	}
	owned := make([]TemporaryProcessIdentity, 0, len(state.Processes))
	seen := map[string]bool{}
	for _, recorded := range state.Processes {
		spec, ok := expected[recorded.ID]
		if !ok || seen[recorded.ID] || recorded.Role != spec.Role || recorded.Identity != spec.Identity || recorded.PID <= 1 || recorded.ProcessGroupID != recorded.PID {
			return fmt.Errorf("temporary process %q has an invalid ownership record", recorded.ID)
		}
		seen[recorded.ID] = true
		observed, observeErr := temporaryProcessIdentity(spec, recorded.PID)
		if errors.Is(observeErr, os.ErrNotExist) || errors.Is(observeErr, syscall.ESRCH) {
			continue
		}
		if observeErr != nil {
			return observeErr
		}
		if sameTemporaryProcessIdentity(recorded, observed) {
			owned = append(owned, recorded)
		}
	}
	for _, process := range owned {
		if err := syscall.Kill(-process.ProcessGroupID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	waitUntil := time.Now().Add(10 * time.Second)
	for len(owned) > 0 {
		remaining := owned[:0]
		for _, recorded := range owned {
			observedStartTime, observeErr := processStartTimeTicks(recorded.PID)
			if observeErr == nil && observedStartTime == recorded.StartTimeTicks {
				remaining = append(remaining, recorded)
			} else if observeErr != nil && !errors.Is(observeErr, os.ErrNotExist) && !errors.Is(observeErr, syscall.ESRCH) {
				return observeErr
			}
		}
		owned = remaining
		if len(owned) == 0 {
			break
		}
		if time.Now().After(waitUntil) {
			for _, process := range owned {
				_ = syscall.Kill(-process.ProcessGroupID, syscall.SIGKILL)
			}
			return fmt.Errorf("temporary processes did not stop: %v", owned)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return os.Remove(path)
}

func startTemporary(ctx context.Context, stateDir, deploymentID string, specs []ProcessSpec) ([]*exec.Cmd, error) {
	if _, err := os.Stat(temporaryProcessFilePath(stateDir)); err == nil {
		return nil, errors.New("temporary process ownership file already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	state := TemporaryProcessFile{Schema: temporaryProcessFileSchema, DeploymentID: deploymentID}
	var out []*exec.Cmd
	for _, s := range specs {
		cmd, err := startSpec(ctx, s)
		if err != nil {
			stopCommands(out)
			return nil, err
		}
		out = append(out, cmd)
		identity, err := temporaryProcessIdentity(s, cmd.Process.Pid)
		if err != nil {
			stopCommands(out)
			return nil, err
		}
		if identity.ProcessGroupID != identity.PID {
			stopCommands(out)
			return nil, fmt.Errorf("temporary process %s is not its process-group leader", s.ID)
		}
		state.Processes = append(state.Processes, identity)
		if err := writeTemporaryProcessFile(stateDir, state); err != nil {
			stopCommands(out)
			return nil, err
		}
	}
	return out, nil
}

func stopTemporaryCommands(stateDir string, commands []*exec.Cmd) {
	stopCommands(commands)
	statePath := temporaryProcessFilePath(stateDir)
	if encoded, err := os.ReadFile(statePath); err == nil {
		var state TemporaryProcessFile
		if json.Unmarshal(encoded, &state) == nil {
			for _, process := range state.Processes {
				if process.PID > 1 && syscall.Kill(process.PID, syscall.Signal(0)) == nil {
					return
				}
			}
		}
	}
	_ = os.Remove(statePath)
}
func startSpec(ctx context.Context, s ProcessSpec) (*exec.Cmd, error) {
	cmd, _, err := startSpecWithExit(ctx, s)
	return cmd, err
}

func startSpecWithExit(ctx context.Context, s ProcessSpec) (*exec.Cmd, <-chan error, error) {
	if err := os.MkdirAll(filepath.Dir(s.StdoutPath), 0o700); err != nil {
		return nil, nil, err
	}
	stdout, err := os.OpenFile(s.StdoutPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	stderr, err := os.OpenFile(s.StderrPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		stdout.Close()
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	cmd.Dir = s.WorkDir
	cmd.Env = envList(s.Env)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, nil, err
	}
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		close(exited)
		stdout.Close()
		stderr.Close()
	}()
	return cmd, exited, nil
}
func stopCommands(cmds []*exec.Cmd) {
	for _, c := range cmds {
		if c != nil && c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, c := range cmds {
			if c != nil && c.Process != nil && c.Process.Signal(syscall.Signal(0)) == nil {
				alive = true
			}
		}
		if !alive {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, c := range cmds {
		if c != nil && c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
	}
}

// Completes a real TLS-authenticated QUIC handshake through the exact socket
// clients will use. HTTP status alone cannot observe a Proxy Protocol wrapper
// discarding direct UDP before QUIC sees it.
func probeConnectH3Readiness(ctx context.Context, address, serverName, caFile string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf("invalid H3 readiness address %q", address)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || !ip.IsLoopback() || serverName != host {
		return errors.New("H3 readiness identity must be one exact IPv4 loopback address")
	}
	caContents, err := os.ReadFile(caFile)
	if err != nil {
		return fmt.Errorf("read H3 readiness CA: %w", err)
	}
	caCertificate, err := parseSingleCertificatePem(caContents)
	if err != nil {
		return fmt.Errorf("parse H3 readiness CA: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCertificate)
	connection, err := quic.DialAddr(
		ctx,
		address,
		&tls.Config{RootCAs: roots, ServerName: serverName, MinVersion: tls.VersionTLS13},
		&quic.Config{HandshakeIdleTimeout: time.Second, InitialPacketSize: connect.H3InitialPacketByteCount},
	)
	if err != nil {
		return fmt.Errorf("verified H3 handshake: %w", err)
	}
	return connection.CloseWithError(0, "readiness complete")
}

// Requires both the HTTP control surface and any transport-specific probe.
// The injected callback gives regressions an exact barrier without timing.
func processSpecReadinessWithH3Probe(ctx context.Context, client *http.Client, spec ProcessSpec, h3Probe func(context.Context, string, string, string) error) error {
	if spec.HealthURL == "" {
		return fmt.Errorf("process %s has no health endpoint", spec.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.HealthURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("health status %d", response.StatusCode)
	}
	probeFieldCount := 0
	for _, value := range []string{spec.H3ProbeAddress, spec.H3ProbeServerName, spec.H3ProbeCAFile} {
		if value != "" {
			probeFieldCount++
		}
	}
	if probeFieldCount == 0 {
		return nil
	}
	if probeFieldCount != 3 || h3Probe == nil {
		return errors.New("H3 readiness probe is incomplete")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return h3Probe(probeCtx, spec.H3ProbeAddress, spec.H3ProbeServerName, spec.H3ProbeCAFile)
}

func waitSpecsReady(ctx context.Context, specs []ProcessSpec, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pending := map[string]ProcessSpec{}
	for _, s := range specs {
		if s.HealthURL == "" {
			return fmt.Errorf("process %s has no health endpoint", s.ID)
		}
		pending[s.ID] = s
	}
	client := &http.Client{Timeout: 2 * time.Second}
	lastErrors := map[string]error{}
	for len(pending) > 0 && time.Now().Before(deadline) {
		for id, s := range pending {
			if err := processSpecReadinessWithH3Probe(ctx, client, s, probeConnectH3Readiness); err == nil {
				delete(pending, id)
				delete(lastErrors, id)
			} else {
				lastErrors[id] = err
			}
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if len(pending) > 0 {
		details := make([]string, 0, len(pending))
		for _, id := range mapKeys(pending) {
			details = append(details, fmt.Sprintf("%s: %v", id, lastErrors[id]))
		}
		return fmt.Errorf("readiness timeout: %s", strings.Join(details, "; "))
	}
	return nil
}
func mapKeys(m map[string]ProcessSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type networkCreateResult struct {
	Network *struct {
		ByJWT       string `json:"by_jwt"`
		NetworkName string `json:"network_name"`
	} `json:"network"`
	Seedphrase string `json:"seedphrase"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func provisionSimulationAccounts(ctx context.Context, cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) error {
	rolePath := filepath.Join(stateDir, "secrets", "roles.json")
	for op := 1; op <= cfg.Config.Topology.Operators; op++ {
		path := filepath.Join(stateDir, "secrets", fmt.Sprintf("operator-%d-network.json", op))
		var result networkCreateResult
		if b, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(b, &result); err != nil {
				return err
			}
		} else {
			body := strings.NewReader(`{"terms":true,"guest_mode":false}`)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/auth/network-create", 18080+op), body)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if err != nil {
				return err
			}
			if resp.StatusCode/100 != 2 {
				return fmt.Errorf("operator %d network create: HTTP %d", op, resp.StatusCode)
			}
			if err := json.Unmarshal(b, &result); err != nil {
				return err
			}
			if result.Error != nil {
				return fmt.Errorf("operator %d network create: %s", op, result.Error.Message)
			}
			if err := atomicWrite(path, b, 0o600); err != nil {
				return err
			}
		}
		if result.Network == nil || result.Network.ByJWT == "" {
			return fmt.Errorf("operator %d network response has no JWT", op)
		}
		for i := 1; i <= cfg.Config.Topology.Miners; i++ {
			if operatorForMiner(cfg, i) == op {
				state := filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", i), "state")
				if err := atomicWrite(filepath.Join(state, "jwt"), []byte(result.Network.ByJWT+"\n"), 0o600); err != nil {
					return err
				}
				seed, err := hex.DecodeString(roles.Clients[fmt.Sprintf("miner-%d", i)].SeedHex)
				if err != nil || len(seed) != 32 {
					return fmt.Errorf("miner-%d client seed is invalid", i)
				}
				if err := atomicWrite(filepath.Join(state, ".provider.key"), seed, 0o600); err != nil {
					return err
				}
				clientID, err := provisionClientJWT(ctx, fmt.Sprintf("http://127.0.0.1:%d", 18080+op), filepath.Join(state, "jwt"), filepath.Join(state, ".provider.jwt"), fmt.Sprintf("sim-testnet miner-%d", i))
				if err != nil {
					return err
				}
				client := roles.Clients[fmt.Sprintf("miner-%d", i)]
				client.ClientIDHex = hex.EncodeToString(clientID[:])
				roles.Clients[fmt.Sprintf("miner-%d", i)] = client
			}
		}
		for i := 1; i <= cfg.Config.Topology.Validators; i++ {
			state := filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", i), "state", "operators", fmt.Sprintf("no-%d", op))
			networkJWT := filepath.Join(state, "network.jwt")
			if err := atomicWrite(networkJWT, []byte(result.Network.ByJWT+"\n"), 0o600); err != nil {
				return err
			}
			label := fmt.Sprintf("validator-%d-no-%d", i, op)
			clientID, err := provisionClientJWT(ctx, fmt.Sprintf("http://127.0.0.1:%d", 18080+op), networkJWT, filepath.Join(state, "client.jwt"), "sim-testnet "+label)
			if err != nil {
				return err
			}
			client := roles.Clients[label]
			client.ClientIDHex = hex.EncodeToString(clientID[:])
			roles.Clients[label] = client
		}
	}
	return saveRoleSecrets(rolePath, roles)
}

func provisionClientJWT(ctx context.Context, apiURL, networkJWTPath, clientJWTPath, description string) (connect.Id, error) {
	strategy := connect.NewClientStrategyWithDefaults(ctx)
	defer strategy.Close()
	api := sdk.NewApi(ctx, strategy, apiURL)
	defer api.Close()
	_, clientID, err := clientauth.LoadOrCreateClientJwt(ctx, api, networkJWTPath, clientJWTPath, description)
	if err != nil {
		return connect.Id{}, fmt.Errorf("provision %s: %w", description, err)
	}
	return clientID, nil
}

func startDetachedSupervisor(ctx context.Context, binary, configPath, stateDir, specPath string) error {
	before := time.Time{}
	statePath := filepath.Join(stateDir, "supervisor.state.json")
	if info, err := os.Stat(statePath); err == nil {
		before = info.ModTime()
	}
	cmd := exec.Command(binary, "__supervise", "--config", configPath, "--state-dir", stateDir, "--manifest", specPath)
	cmd.Dir = filepath.Dir(binary)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logPath := filepath.Join(stateDir, "supervisor.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		return err
	}
	_ = cmd.Process.Release()
	log.Close()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(statePath); err == nil && info.ModTime().After(before) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("detached supervisor did not publish state")
}

type SupervisorService struct {
	Schema   string `json:"schema"`
	Name     string `json:"name"`
	Unit     string `json:"unit_path"`
	Binary   string `json:"binary"`
	StateDir string `json:"state_dir"`
}

func serviceToken(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func systemdExecArgument(value string) (string, error) {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return "", fmt.Errorf("systemd argument contains a control character")
		}
	}
	// ExecStart performs systemd specifier and environment expansion even for
	// quoted arguments. Doubling these characters preserves checkout paths
	// containing a literal '%' or '$' instead of silently changing them.
	value = strings.NewReplacer("%", "%%", "$", "$$").Replace(value)
	return strconv.Quote(value), nil
}

// Build a deliberately non-installable user unit. An explicit launch/resume
// starts it only after journal and chain reconciliation; reboot leaves it down.
func persistentSupervisorUnit(cfg *ResolvedConfig, binary, stateDir, specPath string) (string, error) {
	deploymentToken := serviceToken(cfg.Config.Deployment.DeploymentID)
	if deploymentToken == "" {
		return "", fmt.Errorf("deployment id cannot form a systemd service name")
	}
	configArg, err := systemdExecArgument(cfg.ConfigPath)
	if err != nil {
		return "", err
	}
	stateArg, err := systemdExecArgument(stateDir)
	if err != nil {
		return "", err
	}
	manifestArg, err := systemdExecArgument(specPath)
	if err != nil {
		return "", err
	}
	binaryArg, err := systemdExecArgument(binary)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`[Unit]
Description=UR Network real-testnet simulation %s
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s __supervise --config %s --state-dir %s --manifest %s
Restart=on-failure
RestartSec=5
KillMode=control-group
TimeoutStopSec=30
`, deploymentToken, binaryArg, configArg, stateArg, manifestArg), nil
}

// Remove any legacy boot activation before starting the unit for this boot.
func persistentSupervisorSystemctlActions(name string) [][]string {
	return [][]string{{"--user", "daemon-reload"}, {"--user", "disable", name}, {"--user", "start", name}}
}

func startPersistentSupervisor(ctx context.Context, cfg *ResolvedConfig, binary, stateDir, specPath string) error {
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return fmt.Errorf("persistent detached launch requires systemd user services: %w", err)
	}
	name := "urnetwork-sim-" + serviceToken(cfg.Config.Deployment.DeploymentID) + ".service"
	if name == "urnetwork-sim-.service" {
		return fmt.Errorf("deployment id cannot form a systemd service name")
	}
	unit, err := persistentSupervisorUnit(cfg, binary, stateDir, specPath)
	if err != nil {
		return err
	}
	configHome, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	unitPath := filepath.Join(configHome, "systemd", "user", name)
	if err := atomicWrite(unitPath, []byte(unit), 0o600); err != nil {
		return err
	}
	metadata := SupervisorService{Schema: "urnetwork-sim-supervisor-service-v1", Name: name, Unit: unitPath, Binary: binary, StateDir: stateDir}
	if err := writePublicJSON(filepath.Join(stateDir, "supervisor.service.json"), metadata); err != nil {
		return err
	}
	for _, args := range persistentSupervisorSystemctlActions(name) {
		cmd := exec.CommandContext(ctx, systemctl, args...)
		if output, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), runErr, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func supervisorReadyNow(stateDir string, want SupervisorFile) (bool, error) {
	wantHash, err := canonicalHashHex(want)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(filepath.Join(stateDir, "supervisor.state.json"))
	if err != nil {
		return false, err
	}
	var state SupervisorState
	if err := json.Unmarshal(b, &state); err != nil {
		return false, err
	}
	return supervisorStateReady(state, wantHash, want.Specs), nil
}

// Identify the listener-bearing services every release workload contacts while
// starting. Waiting on these exact roles prevents a newly spawned client herd
// from racing API, Connect or RPC listener initialization.
func supervisorStartupPrerequisite(spec ProcessSpec) bool {
	switch spec.Role {
	case "dependency-rpc-proxy", "operator-api", "operator-connect":
		return true
	default:
		return false
	}
}

// Start prerequisites, cross one explicit health barrier, and only then start
// dependent workers. Callbacks keep the ordering deterministic in tests while
// production retains the supervisor's real process ownership.
func startSupervisorSpecsWithReadiness(specs []ProcessSpec, start func(ProcessSpec) error, wait func([]ProcessSpec) error) error {
	if start == nil || wait == nil {
		return errors.New("supervisor startup callbacks are incomplete")
	}
	prerequisites := make([]ProcessSpec, 0, len(specs))
	dependents := make([]ProcessSpec, 0, len(specs))
	for _, spec := range specs {
		if supervisorStartupPrerequisite(spec) {
			if spec.HealthURL == "" {
				return fmt.Errorf("supervisor startup prerequisite %s has no health endpoint", spec.ID)
			}
			prerequisites = append(prerequisites, spec)
		} else {
			dependents = append(dependents, spec)
		}
	}
	for _, spec := range prerequisites {
		if err := start(spec); err != nil {
			return fmt.Errorf("start supervisor prerequisite %s: %w", spec.ID, err)
		}
	}
	if len(prerequisites) != 0 {
		if err := wait(prerequisites); err != nil {
			return fmt.Errorf("supervisor startup prerequisite readiness: %w", err)
		}
	}
	for _, spec := range dependents {
		if err := start(spec); err != nil {
			return fmt.Errorf("start supervisor dependent %s: %w", spec.ID, err)
		}
	}
	return nil
}

// Verifies the live kernel process generation rather than trusting a reusable
// pid recorded in a stale state file.
func validateSupervisorGeneration(state SupervisorState) error {
	if state.SupervisorPID <= 1 || state.SupervisorStartTimeTicks == 0 {
		return errors.New("supervisor process generation is incomplete")
	}
	observedStartTimeTicks, err := processStartTimeTicks(state.SupervisorPID)
	if err != nil {
		return fmt.Errorf("observe supervisor process generation: %w", err)
	}
	if observedStartTimeTicks != state.SupervisorStartTimeTicks {
		return fmt.Errorf("supervisor pid %d start time changed from %d to %d", state.SupervisorPID, state.SupervisorStartTimeTicks, observedStartTimeTicks)
	}
	if err := syscall.Kill(state.SupervisorPID, syscall.Signal(0)); err != nil {
		return fmt.Errorf("supervisor process is not live: %w", err)
	}
	return nil
}

func supervisorStateReady(state SupervisorState, wantHash string, specs []ProcessSpec) bool {
	if state.Schema != "urnetwork-sim-supervisor-state-v1" || state.ManifestHash != wantHash || validateSupervisorGeneration(state) != nil || len(state.Processes) != len(specs) {
		return false
	}
	want := make(map[string]ProcessSpec, len(specs))
	for _, spec := range specs {
		if spec.ID == "" || want[spec.ID].ID != "" {
			return false
		}
		want[spec.ID] = spec
	}
	for _, process := range state.Processes {
		spec, ok := want[process.ID]
		if !ok || process.Role != spec.Role || process.Identity != spec.Identity || process.PID <= 1 || !process.Healthy || syscall.Kill(process.PID, syscall.Signal(0)) != nil {
			return false
		}
		delete(want, process.ID)
	}
	return len(want) == 0
}

func waitSupervisorReady(ctx context.Context, stateDir string, want SupervisorFile, timeout time.Duration) (*SupervisorState, error) {
	wantHash, err := canonicalHashHex(want)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	var lastState *SupervisorState
	for time.Now().Before(deadline) {
		b, readErr := os.ReadFile(filepath.Join(stateDir, "supervisor.state.json"))
		if readErr == nil {
			var state SupervisorState
			if decodeErr := json.Unmarshal(b, &state); decodeErr != nil {
				lastErr = fmt.Errorf("decode supervisor state: %w", decodeErr)
			} else {
				lastErr = nil
				lastState = &state
				if supervisorStateReady(state, wantHash, want.Specs) {
					return &state, nil
				}
			}
		} else {
			lastErr = readErr
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if remaining > 500*time.Millisecond {
			remaining = 500 * time.Millisecond
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("supervisor readiness timeout: %w", lastErr)
	}
	if lastState != nil {
		return nil, fmt.Errorf("supervisor readiness timeout: supervisor_pid=%d manifest_hash=%s processes=%+v", lastState.SupervisorPID, lastState.ManifestHash, lastState.Processes)
	}
	return nil, fmt.Errorf("supervisor readiness timeout: no state observed")
}

func supervise(ctx context.Context, stateDir, specPath string) error {
	lock, err := os.OpenFile(filepath.Join(stateDir, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another supervisor owns %s: %w", stateDir, err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	b, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	var sf SupervisorFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	hash, err := fileSHA256(executable)
	if err != nil {
		return err
	}
	if hash != sf.BinaryHash {
		return fmt.Errorf("supervisor binary hash %s, manifest requires %s", hash, sf.BinaryHash)
	}
	manifestHash, err := canonicalHashHex(sf)
	if err != nil {
		return err
	}
	supervisorStartTimeTicks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		return fmt.Errorf("observe supervisor process generation: %w", err)
	}
	type running struct {
		spec       ProcessSpec
		cmd        *exec.Cmd
		state      ProcessState
		generation uint64
	}
	type exitNotice struct {
		id         string
		generation uint64
		err        error
	}
	type restartNotice struct {
		id         string
		generation uint64
	}
	runs := map[string]*running{}
	var mu sync.Mutex
	exits := make(chan exitNotice, len(sf.Specs)*2)
	restarts := make(chan restartNotice, len(sf.Specs))
	publish := func() error {
		mu.Lock()
		defer mu.Unlock()
		s := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), SupervisorPID: os.Getpid(), SupervisorStartTimeTicks: supervisorStartTimeTicks, ManifestHash: manifestHash}
		for _, r := range runs {
			s.Processes = append(s.Processes, r.state)
		}
		sort.Slice(s.Processes, func(i, j int) bool { return s.Processes[i].ID < s.Processes[j].ID })
		raw, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return fmt.Errorf("encode supervisor state: %w", err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "supervisor.state.json"), append(raw, '\n'), 0o600); err != nil {
			return fmt.Errorf("publish supervisor state: %w", err)
		}
		return nil
	}
	start := func(r *running) error {
		cmd, exited, err := startSpecWithExit(ctx, r.spec)
		if err != nil {
			return err
		}
		r.cmd = cmd
		r.generation++
		r.state.PID = cmd.Process.Pid
		r.state.StartedAt = time.Now().UTC().Format(time.RFC3339)
		r.state.ExitError = ""
		generation := r.generation
		go func(id string, ch <-chan error) {
			err := <-ch
			exits <- exitNotice{id: id, generation: generation, err: err}
		}(r.spec.ID, exited)
		return nil
	}
	for _, spec := range sf.Specs {
		r := &running{spec: spec, state: ProcessState{ID: spec.ID, Role: spec.Role, Identity: spec.Identity}}
		runs[spec.ID] = r
	}
	defer func() {
		var commands []*exec.Cmd
		for _, current := range runs {
			commands = append(commands, current.cmd)
		}
		stopCommands(commands)
	}()
	if err := startSupervisorSpecsWithReadiness(
		sf.Specs,
		func(spec ProcessSpec) error {
			r := runs[spec.ID]
			if r == nil {
				return fmt.Errorf("supervisor process %s is absent from its runtime inventory", spec.ID)
			}
			return start(r)
		},
		func(prerequisites []ProcessSpec) error {
			return waitSpecsReady(ctx, prerequisites, 2*time.Minute)
		},
	); err != nil {
		return err
	}
	if err := publish(); err != nil {
		return err
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			var commands []*exec.Cmd
			for _, current := range runs {
				commands = append(commands, current.cmd)
			}
			stopCommands(commands)
			for _, current := range runs {
				current.state.PID = 0
				current.state.Healthy = false
				current.state.ExitError = "supervisor stopped"
			}
			return publish()
		case notice := <-exits:
			r := runs[notice.id]
			if r == nil || notice.generation != r.generation {
				continue
			}
			r.state.PID = 0
			r.state.Healthy = false
			if notice.err != nil {
				r.state.ExitError = notice.err.Error()
			} else {
				r.state.ExitError = "process exited"
			}
			if r.state.Restarts < r.spec.RestartLimit {
				r.state.Restarts++
				delay := restartBackoff(r.state.Restarts)
				generation := r.generation
				go func(id string) {
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-ctx.Done():
					case <-timer.C:
						restarts <- restartNotice{id: id, generation: generation}
					}
				}(r.spec.ID)
			}
			if err := publish(); err != nil {
				return err
			}
		case notice := <-restarts:
			r := runs[notice.id]
			if r == nil || notice.generation != r.generation || r.state.PID != 0 {
				continue
			}
			if err := start(r); err != nil {
				r.state.ExitError = "restart: " + err.Error()
				// Feed the same bounded state machine without inventing a PID.
				exits <- exitNotice{id: r.spec.ID, generation: r.generation, err: err}
			}
			if err := publish(); err != nil {
				return err
			}
		case <-ticker.C:
			for _, r := range runs {
				r.state.Healthy = r.state.PID > 1 && healthOK(r.spec.HealthURL)
			}
			if err := publish(); err != nil {
				return err
			}
		}
	}
}

func restartBackoff(restarts int) time.Duration {
	if restarts < 1 {
		restarts = 1
	}
	d := time.Second << min(restarts-1, 5)
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}
func healthOK(url string) bool {
	if url == "" {
		return true
	}
	c := http.Client{Timeout: time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode/100 == 2
}

func StopDeployment(ctx context.Context, stateDir string) (map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "supervisor.state.json"))
	if err != nil {
		return nil, err
	}
	var s SupervisorState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	stopped := []string{}
	serviceStopped := ""
	if serviceBytes, readErr := os.ReadFile(filepath.Join(stateDir, "supervisor.service.json")); readErr == nil {
		var service SupervisorService
		if json.Unmarshal(serviceBytes, &service) == nil && service.Schema == "urnetwork-sim-supervisor-service-v1" && service.Name != "" {
			if systemctl, lookupErr := exec.LookPath("systemctl"); lookupErr == nil {
				cmd := exec.CommandContext(ctx, systemctl, "--user", "disable", "--now", service.Name)
				if output, stopErr := cmd.CombinedOutput(); stopErr != nil {
					return nil, fmt.Errorf("stop persistent supervisor: %w: %s", stopErr, strings.TrimSpace(string(output)))
				}
				serviceStopped = service.Name
			}
		}
	}
	if serviceStopped == "" && s.SupervisorPID > 1 {
		_ = syscall.Kill(s.SupervisorPID, syscall.SIGTERM)
	}
	deadline := time.Now().Add(15 * time.Second)
	for s.SupervisorPID > 1 && time.Now().Before(deadline) && syscall.Kill(s.SupervisorPID, syscall.Signal(0)) == nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	for _, p := range s.Processes {
		if p.PID > 1 {
			if err := syscall.Kill(-p.PID, syscall.SIGTERM); err == nil || errors.Is(err, syscall.ESRCH) {
				stopped = append(stopped, p.ID)
			}
		}
	}
	return map[string]any{"stopped": stopped, "service": serviceStopped, "on_chain_state_preserved": true, "state_dir": stateDir}, nil
}
func Tail(ctx context.Context, stateDir string, w io.Writer) error {
	files, err := filepath.Glob(filepath.Join(stateDir, "processes", "*.log"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no process logs in %s", stateDir)
	}
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fmt.Fprintf(w, "[%s] %s\n", filepath.Base(path), scanner.Text())
		}
		f.Close()
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	sizes := map[string]int64{}
	for _, p := range files {
		if s, e := os.Stat(p); e == nil {
			sizes[p] = s.Size()
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, p := range files {
				f, e := os.Open(p)
				if e != nil {
					continue
				}
				_, _ = f.Seek(sizes[p], io.SeekStart)
				b, _ := io.ReadAll(f)
				sizes[p] += int64(len(b))
				f.Close()
				if len(b) > 0 {
					fmt.Fprintf(w, "[%s] %s", filepath.Base(p), b)
				}
			}
		}
	}
}
func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}

var _ = strconv.Itoa
