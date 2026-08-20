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

func LaunchDeployment(ctx context.Context, cfg *ResolvedConfig, stateDir string, p *SetupPlan, roles *RoleSecrets, executor *Executor, detach bool) error {
	if err := startDependencies(ctx, cfg, stateDir); err != nil {
		return err
	}
	bins, err := buildReleaseBinaries(ctx, cfg, stateDir)
	if err != nil {
		return err
	}
	if err := runDatabaseMigrations(ctx, cfg, stateDir, bins["server-ctl"]); err != nil {
		return err
	}
	serverSpecs := buildServerSpecs(cfg, stateDir, bins)
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
			return nil, fmt.Errorf("build %s: %w: %s", t.name, err, redactText(string(output), cfg.WalletSecret, cfg.WalletMaterial))
		}
		result[t.name] = path
	}
	return result, nil
}

func startDependencies(ctx context.Context, cfg *ResolvedConfig, stateDir string) error {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("managed_containers requires docker: %w", err)
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		ip := fmt.Sprintf("127.0.0.%d", 10+i)
		pgName := fmt.Sprintf("%s-pg-%d", cfg.Config.Deployment.DeploymentID, i)
		redisName := fmt.Sprintf("%s-redis-%d", cfg.Config.Deployment.DeploymentID, i)
		pgSeed := derive32(cfg, fmt.Sprintf("dependency/postgres-%d", i))
		pgPassword := hex.EncodeToString(pgSeed[:])
		if err := ensureContainer(ctx, docker, pgName, cfg.Release.Dependencies["postgres"], []string{"-e", "POSTGRES_USER=bringyour", "-e", "POSTGRES_PASSWORD=" + pgPassword, "-e", "POSTGRES_DB=bringyour", "-p", ip + ":5432:5432"}); err != nil {
			return err
		}
		if err := ensureContainer(ctx, docker, redisName, cfg.Release.Dependencies["redis"], []string{"-p", ip + ":6379:6379"}); err != nil {
			return err
		}
		if err := waitContainerReady(ctx, docker, pgName, []string{"pg_isready", "-U", "bringyour", "-d", "bringyour"}, 90*time.Second); err != nil {
			return err
		}
		if err := waitContainerReady(ctx, docker, redisName, []string{"redis-cli", "ping"}, 30*time.Second); err != nil {
			return err
		}
	}
	return nil
}
func ensureContainer(ctx context.Context, docker, name, image string, args []string) error {
	inspect := exec.CommandContext(ctx, docker, "inspect", "--format", "{{.Config.Image}}", name)
	if out, inspectErr := inspect.Output(); inspectErr == nil {
		if strings.TrimSpace(string(out)) != image {
			return fmt.Errorf("container %s image is %q, release lock requires %q", name, strings.TrimSpace(string(out)), image)
		}
		start := exec.CommandContext(ctx, docker, "start", name)
		if out, err := start.CombinedOutput(); err != nil {
			return fmt.Errorf("start %s: %w: %s", name, err, out)
		}
		return nil
	}
	cmdArgs := append([]string{"run", "-d", "--name", name, "--restart", "unless-stopped"}, args...)
	cmdArgs = append(cmdArgs, image)
	cmd := exec.CommandContext(ctx, docker, cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("run %s: %w: %s", name, err, out)
	}
	return nil
}

func waitContainerReady(ctx context.Context, docker, name string, probe []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		args := append([]string{"exec", name}, probe...)
		if exec.CommandContext(ctx, docker, args...).Run() == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("container %s readiness timeout", name)
}

func buildServerSpecs(cfg *ResolvedConfig, stateDir string, bins map[string]string) []ProcessSpec {
	var out []ProcessSpec
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
	return out
}

// Account provisioning requires only each operator's API. Starting connect
// and taskworker here would create two owners for the same durable EVM signer
// nonce before setup has completed. The full server set starts exactly once
// under the persistent supervisor after every planned setup transaction.
func selectProvisioningServerSpecs(specs []ProcessSpec) []ProcessSpec {
	out := make([]ProcessSpec, 0)
	for _, spec := range specs {
		if spec.Role == "operator-api" {
			out = append(out, spec)
		}
	}
	return out
}

func operatorBaseEnv(cfg *ResolvedConfig, stateDir string, operator int, ip string) map[string]string {
	root := filepath.Join(stateDir, "runtime", fmt.Sprintf("operator-%d", operator))
	return map[string]string{"WARP_ENV": fmt.Sprintf("sim-testnet-op-%d", operator), "WARP_VERSION": "1.0", "WARP_BLOCK": fmt.Sprintf("sim%d", operator), "WARP_HOST": "127.0.0.1", "WARP_VAULT_HOME": filepath.Join(root, "vault"), "WARP_CONFIG_HOME": filepath.Join(cfg.Repos.PlatformConfig, "local"), "WARP_SITE_HOME": filepath.Join(root, "site"), "BRINGYOUR_POSTGRES_HOSTNAME": ip, "BRINGYOUR_REDIS_HOSTNAME": ip, "BRINGYOUR_SUBTENSOR_HOSTNAME": stripScheme(cfg.Authority), "BRINGYOUR_MINIO_HOSTNAME": cfg.ObjectStoreHost, "BRINGYOUR_TRUSTED_PROXY_CIDRS": cfg.TrustedProxyCIDRs, "URNETWORK_ST_PROFILE": "testnet"}
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
			return fmt.Errorf("operator %d database migrations: %w: %s", i, err, redactText(string(output), cfg.WalletSecret, cfg.WalletMaterial))
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
		op := 1 + (i-1)%cfg.Config.Topology.Operators
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
			if 1+(i-1)%cfg.Config.Topology.Operators == op {
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
