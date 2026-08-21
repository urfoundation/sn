package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
)

type ProcessSpec struct {
	ID, Role, Identity, Command, WorkDir string
	Args                                 []string
	Env                                  map[string]string
	StdoutPath, StderrPath, HealthURL    string
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
	Schema        string         `json:"schema"`
	UpdatedAt     string         `json:"updated_at"`
	SupervisorPID int            `json:"supervisor_pid"`
	ManifestHash  string         `json:"manifest_hash"`
	Processes     []ProcessState `json:"processes"`
}

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
	RunArgs           []string
	Command           []string
	DataVolumes       []string
	ReadyProbe        []string
	ReadyExpected     string
	ReadyTimeout      time.Duration
}

const managedContainerSpecHashLabel = "com.urnetwork.sim-testnet.spec-hash"

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
	temporary, err := startTemporary(ctx, provisioningSpecs)
	if err != nil {
		return err
	}
	defer stopCommands(temporary)
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
	stopCommands(temporary)
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
	if detach || cfg.Config.Deployment.DetachAfterLaunch {
		if ready, _ := supervisorReadyNow(stateDir, sf); !ready {
			if err := startPersistentSupervisor(ctx, cfg, bins["sim-testnet"], stateDir, specPath); err != nil {
				return err
			}
		}
		if err := waitSupervisorReady(ctx, stateDir, sf, 3*time.Minute); err != nil {
			return err
		}
		if err := executor.Execute(ctx, *topologyAction); err != nil {
			return err
		}
		return publishDeploymentEvidence(ctx, cfg, stateDir, p, roles)
	}
	supervisorCtx, cancelSupervisor := context.WithCancel(ctx)
	defer cancelSupervisor()
	supervisorErr := make(chan error, 1)
	go func() { supervisorErr <- supervise(supervisorCtx, stateDir, specPath) }()
	if err := waitSupervisorReady(ctx, stateDir, sf, 3*time.Minute); err != nil {
		cancelSupervisor()
		<-supervisorErr
		return err
	}
	if err := executor.Execute(ctx, *topologyAction); err != nil {
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
	targets := []struct{ name, dir, pkg string }{{"sim-testnet", cfg.Repos.SN, "./sim-testnet"}, {"miner", cfg.Repos.SN, "./cli/miner"}, {"validator", cfg.Repos.SN, "./cli/validator"}, {"server-api", cfg.Repos.Server, "./cli/api"}, {"server-connect", cfg.Repos.Server, "./cli/connect"}, {"server-taskworker", cfg.Repos.Server, "./cli/taskworker"}, {"server-ctl", cfg.Repos.Server, "./bringyourctl"}}
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
	specHash, err := managedContainerSpecHash(spec)
	if err != nil {
		return fmt.Errorf("hash container %s spec: %w", spec.Name, err)
	}
	for _, volume := range spec.DataVolumes {
		if err := ensureManagedVolume(ctx, docker, volume, specHash); err != nil {
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
	cmdArgs := []string{"run", "-d", "--name", spec.Name, "--restart", "unless-stopped", "--label", managedContainerSpecHashLabel + "=" + specHash}
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

func buildServerSpecs(cfg *ResolvedConfig, stateDir string, bins map[string]string) ([]ProcessSpec, error) {
	proxy, err := rpcProxyConfigForAuthority(cfg.Authority)
	if err != nil {
		return nil, fmt.Errorf("workload RPC proxy: %w", err)
	}
	if bins["sim-testnet"] == "" {
		return nil, errors.New("workload RPC proxy requires the sim-testnet release binary")
	}
	proxyArgs := []string{
		"__rpc_proxy",
		"--listen=" + proxy.ListenAddress,
		"--health=" + proxy.HealthAddress,
		"--upstream=" + proxy.Upstream,
	}
	if proxy.TLSServerName != "" {
		proxyArgs = append(proxyArgs, "--tls-server-name="+proxy.TLSServerName)
	}
	out := []ProcessSpec{{
		ID: workloadRPCProxyProcessID, Role: "dependency-rpc-proxy", Identity: "private-subtensor-rpc",
		Command: bins["sim-testnet"], Args: proxyArgs, WorkDir: cfg.Repos.SN,
		StdoutPath: filepath.Join(stateDir, "processes", workloadRPCProxyProcessID+".stdout.log"),
		StderrPath: filepath.Join(stateDir, "processes", workloadRPCProxyProcessID+".stderr.log"),
		HealthURL:  "http://" + workloadRPCProxyHealthAddress + "/healthz", RestartLimit: 5,
	}}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		ip := fmt.Sprintf("127.0.0.%d", 10+i)
		baseEnv := operatorBaseEnv(cfg, stateDir, i, ip)
		for _, svc := range []struct {
			role, bin string
			port      int
		}{{"api", "server-api", 18080 + i}, {"connect", "server-connect", 19080 + i}, {"taskworker", "server-taskworker", 20080 + i}} {
			env := cloneStrings(baseEnv)
			env["WARP_SERVICE"] = svc.role
			id := fmt.Sprintf("operator-%d-%s", i, svc.role)
			out = append(out, ProcessSpec{ID: id, Role: "operator-" + svc.role, Identity: fmt.Sprintf("no:%d", i), Command: bins[svc.bin], Args: []string{fmt.Sprintf("--port=%d", svc.port)}, WorkDir: cfg.Repos.Server, Env: env, StdoutPath: filepath.Join(stateDir, "processes", id+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", id+".stderr.log"), HealthURL: fmt.Sprintf("http://127.0.0.1:%d/status", svc.port), RestartLimit: 5})
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
	return map[string]string{"WARP_ENV": fmt.Sprintf("sim-testnet-op-%d", operator), "WARP_VERSION": "1.0", "WARP_BLOCK": fmt.Sprintf("sim%d", operator), "WARP_HOST": "127.0.0.1", "WARP_VAULT_HOME": filepath.Join(root, "vault"), "WARP_CONFIG_HOME": filepath.Join(cfg.Repos.PlatformConfig, "local"), "WARP_SITE_HOME": filepath.Join(root, "site"), "BRINGYOUR_POSTGRES_HOSTNAME": ip, "BRINGYOUR_REDIS_HOSTNAME": ip, "BRINGYOUR_SUBTENSOR_HOSTNAME": workloadRPCAuthority(), "BRINGYOUR_MINIO_HOSTNAME": cfg.ObjectStoreHost, "BRINGYOUR_TRUSTED_PROXY_CIDRS": cfg.TrustedProxyCIDRs, "URNETWORK_ST_PROFILE": "testnet"}
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
	for i := 1; i <= cfg.Config.Topology.Miners; i++ {
		op := operatorForMiner(cfg, i)
		minerState := filepath.Join(stateDir, "runtime", fmt.Sprintf("miner-%d", i), "state")
		id := fmt.Sprintf("miner-%d", i)
		wallet := roles.Substrate[fmt.Sprintf("miner-%d-payout", i)].SS58
		out = append(out, ProcessSpec{ID: id, Role: "miner", Identity: "0x" + roles.Clients[id].ClientIDHex, Command: bins["miner"], Args: []string{"provide", fmt.Sprintf("--api_url=http://127.0.0.1:%d", 18080+op), fmt.Sprintf("--connect_url=ws://127.0.0.1:%d", 19080+op), "--wallet=" + wallet, "--test-egress-source-ip=" + minerTestEgressSourceIP(i), fmt.Sprintf("--port=%d", 21080+i)}, WorkDir: cfg.Repos.SN, Env: map[string]string{"URNETWORK_STATE_DIR": minerState, "WARP_VERSION": "1.0"}, StdoutPath: filepath.Join(stateDir, "processes", id+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", id+".stderr.log"), HealthURL: fmt.Sprintf("http://127.0.0.1:%d/status", 21080+i), RestartLimit: 5})
		claimID := fmt.Sprintf("miner-%d-claims", i)
		out = append(out, ProcessSpec{ID: claimID, Role: "claim-daemon", Identity: "0x" + roles.Clients[id].ClientIDHex, Command: bins["miner"], Args: []string{"claim-daemon", "--config=" + filepath.Join(stateDir, "runtime", id, "claim-daemon.yml")}, WorkDir: cfg.Repos.SN, Env: map[string]string{"URNETWORK_STATE_DIR": minerState, "WARP_VERSION": "1.0"}, StdoutPath: filepath.Join(stateDir, "processes", claimID+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", claimID+".stderr.log"), RestartLimit: 5})
	}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		id := fmt.Sprintf("validator-%d", i)
		args := []string{"run", "--config=" + filepath.Join(stateDir, "runtime", id, "validator.yml")}
		out = append(out, ProcessSpec{ID: id, Role: "validator", Identity: roles.Substrate[validatorHotkeyLabel(i)].SS58, Command: bins["validator"], Args: args, WorkDir: cfg.Repos.SN, Env: map[string]string{"URNETWORK_STATE_DIR": filepath.Join(stateDir, "runtime", id, "state"), "WARP_VERSION": "1.0"}, StdoutPath: filepath.Join(stateDir, "processes", id+".stdout.log"), StderrPath: filepath.Join(stateDir, "processes", id+".stderr.log"), RestartLimit: 5})
	}
	return out
}

// minerTestEgressSourceIP gives every provider a distinct exact loopback
// address. The six head providers deliberately occupy one /29, so their
// signed trail hashes are identical and the two three-client fleets each get
// an exact 1/2 prefix claim. The two tail providers use separate /29s. The
// miner flag accepts loopback only, keeping this topology test-local.
func minerTestEgressSourceIP(miner int) string {
	if miner <= 6 {
		return fmt.Sprintf("127.64.0.%d", miner)
	}
	return fmt.Sprintf("127.65.%d.1", miner-7)
}

// operatorForMiner keeps every head fleet within one operator so affiliation
// masking cannot contaminate every head UID. Whole fleets and tail miners are
// distributed round-robin, preserving the configured balanced topology.
func operatorForMiner(cfg *ResolvedConfig, miner int) int {
	if cfg == nil || cfg.Config == nil || cfg.Config.Topology.Operators <= 0 || miner <= 0 || miner > cfg.Config.Topology.Miners {
		return 0
	}
	topology := cfg.Config.Topology
	headMembers := topology.HeadFleets * topology.ClientsPerHeadFleet
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

func startTemporary(ctx context.Context, specs []ProcessSpec) ([]*exec.Cmd, error) {
	var out []*exec.Cmd
	for _, s := range specs {
		cmd, err := startSpec(ctx, s)
		if err != nil {
			stopCommands(out)
			return nil, err
		}
		out = append(out, cmd)
	}
	return out, nil
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
func waitSpecsReady(ctx context.Context, specs []ProcessSpec, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pending := map[string]ProcessSpec{}
	for _, s := range specs {
		pending[s.ID] = s
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for len(pending) > 0 && time.Now().Before(deadline) {
		for id, s := range pending {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.HealthURL, nil)
			resp, err := client.Do(req)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					delete(pending, id)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("readiness timeout: %v", mapKeys(pending))
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

func systemdQuote(value string) (string, error) {
	if strings.ContainsAny(value, "\n\r\x00") {
		return "", fmt.Errorf("systemd argument contains a control character")
	}
	return strconv.Quote(value), nil
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
	configArg, err := systemdQuote(cfg.ConfigPath)
	if err != nil {
		return err
	}
	stateArg, err := systemdQuote(stateDir)
	if err != nil {
		return err
	}
	manifestArg, err := systemdQuote(specPath)
	if err != nil {
		return err
	}
	binaryArg, err := systemdQuote(binary)
	if err != nil {
		return err
	}
	workArg, err := systemdQuote(filepath.Dir(binary))
	if err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=UR Network real-testnet simulation %s
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s __supervise --config %s --state-dir %s --manifest %s
Restart=on-failure
RestartSec=5
KillMode=control-group
TimeoutStopSec=30

[Install]
WantedBy=default.target
`, cfg.Config.Deployment.DeploymentID, workArg, binaryArg, configArg, stateArg, manifestArg)
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
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", name}} {
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
	if state.ManifestHash != wantHash || state.SupervisorPID <= 1 || len(state.Processes) != len(want.Specs) || syscall.Kill(state.SupervisorPID, syscall.Signal(0)) != nil {
		return false, nil
	}
	for _, process := range state.Processes {
		if process.PID <= 1 || !process.Healthy || syscall.Kill(process.PID, syscall.Signal(0)) != nil {
			return false, nil
		}
	}
	return true, nil
}

func waitSupervisorReady(ctx context.Context, stateDir string, want SupervisorFile, timeout time.Duration) error {
	wantHash, err := canonicalHashHex(want)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b, readErr := os.ReadFile(filepath.Join(stateDir, "supervisor.state.json"))
		if readErr == nil {
			var state SupervisorState
			if json.Unmarshal(b, &state) == nil && state.ManifestHash == wantHash && state.SupervisorPID > 1 && len(state.Processes) == len(want.Specs) {
				ready := true
				for _, process := range state.Processes {
					if process.PID <= 1 || !process.Healthy || syscall.Kill(process.PID, syscall.Signal(0)) != nil {
						ready = false
						break
					}
				}
				if ready {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("supervisor readiness timeout")
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
	publish := func() {
		mu.Lock()
		defer mu.Unlock()
		s := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), SupervisorPID: os.Getpid(), ManifestHash: manifestHash}
		for _, r := range runs {
			s.Processes = append(s.Processes, r.state)
		}
		sort.Slice(s.Processes, func(i, j int) bool { return s.Processes[i].ID < s.Processes[j].ID })
		raw, _ := json.MarshalIndent(s, "", "  ")
		_ = atomicWrite(filepath.Join(stateDir, "supervisor.state.json"), append(raw, '\n'), 0o600)
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
		if err := start(r); err != nil {
			var started []*exec.Cmd
			for _, prior := range runs {
				if prior.cmd != nil {
					started = append(started, prior.cmd)
				}
			}
			stopCommands(started)
			return err
		}
	}
	publish()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			var cmds []*exec.Cmd
			for _, r := range runs {
				cmds = append(cmds, r.cmd)
			}
			stopCommands(cmds)
			publish()
			return nil
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
			publish()
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
			publish()
		case <-ticker.C:
			for _, r := range runs {
				r.state.Healthy = r.state.PID > 1 && healthOK(r.spec.HealthURL)
			}
			publish()
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
