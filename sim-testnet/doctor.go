package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
)

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Hard   bool   `json:"hard"`
	Detail string `json:"detail"`
}
type DoctorReport struct {
	Schema      string  `json:"schema"`
	GeneratedAt string  `json:"generated_at"`
	ConfigHash  string  `json:"config_hash"`
	PolicyHash  string  `json:"policy_hash"`
	Checks      []Check `json:"checks"`
	Ready       bool    `json:"ready"`
}

func (r DoctorReport) Error() error {
	if r.Ready {
		return nil
	}
	var failed []string
	for _, check := range r.Checks {
		if check.Hard && !check.OK {
			failed = append(failed, check.Name)
		}
	}
	return fmt.Errorf("doctor hard failures: %s", strings.Join(failed, ", "))
}
func (r *DoctorReport) add(name string, hard bool, err error, detail string) {
	c := Check{Name: name, Hard: hard, OK: err == nil, Detail: detail}
	if err != nil {
		if c.Detail == "" {
			c.Detail = err.Error()
		} else {
			c.Detail += "; error=" + err.Error()
		}
	}
	r.Checks = append(r.Checks, c)
}

type doctorPlanBudget struct {
	Plan      *SetupPlan
	Remaining Spend
}

func RunDoctor(ctx context.Context, cfg *ResolvedConfig) DoctorReport {
	return runDoctor(ctx, cfg, nil)
}

// The approved mode rechecks changing facts against only unverified spend;
// the read-only mode additionally proves a newly generated plan is affordable.
func runDoctor(ctx context.Context, cfg *ResolvedConfig, approved *doctorPlanBudget) DoctorReport {
	r := DoctorReport{Schema: "urnetwork-sim-doctor-v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339), ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Ready: true}
	r.add("host/linux-amd64", true, validateHostPlatform(runtime.GOOS, runtime.GOARCH), runtime.GOOS+"/"+runtime.GOARCH)
	defaultStateRoot := filepath.Dir(cfg.ConfigPath)
	freeBytes, diskErr := filesystemFreeBytes(defaultStateRoot)
	if diskErr == nil {
		diskErr = validateReleaseStateFreeBytes(freeBytes)
	}
	r.add("host/default-state-disk", true, diskErr, fmt.Sprintf("path=%s free_bytes=%d minimum_bytes=%d", defaultStateRoot, freeBytes, minimumReleaseStateFreeBytes))
	for _, tool := range []string{"go", "git", "docker"} {
		p, err := exec.LookPath(tool)
		r.add("tool/"+tool, true, err, p)
	}
	docker, daemonErr := resolveDockerCLI(ctx)
	dockerDetail := docker.ServerVersion
	if len(docker.Prefix) != 0 {
		dockerDetail += " via passwordless sudo"
	}
	r.add("docker/daemon", true, daemonErr, dockerDetail)
	if systemctl, err := exec.LookPath("systemctl"); err != nil {
		r.add("supervisor/systemd-user", true, err, "")
	} else {
		cmd := exec.CommandContext(ctx, systemctl, "--user", "is-system-running")
		output, runErr := cmd.CombinedOutput()
		state := strings.TrimSpace(string(output))
		if runErr != nil || state != "running" {
			if runErr == nil {
				runErr = fmt.Errorf("systemd user manager is %q", state)
			}
			r.add("supervisor/systemd-user", true, runErr, state)
		} else {
			r.add("supervisor/systemd-user", true, nil, state)
		}
	}
	for name, path := range map[string]string{"sn": cfg.Repos.SN, "server": cfg.Repos.Server, "vault": cfg.Repos.Vault, "platform-config": cfg.Repos.PlatformConfig} {
		err := validateRepoIdentity(name, path)
		r.add("repository/"+name, true, err, path)
	}
	r.add("release-lock", true, validateReleaseLock(cfg), cfg.Release.Release)
	r.add("vault/wallet", true, nonempty(cfg.WalletMaterial, "testnet-wallet is empty"), cfg.WalletPublic)
	r.add("vault/netuid", true, nonzero(uint64(cfg.Netuid), "testnet-netuid is zero"), fmt.Sprint(cfg.Netuid))
	r.add("vault/budgets", true, allNonzero(cfg.MaximumTAORao, cfg.MaximumAlphaRao, cfg.MaximumEVMGasWei), fmt.Sprintf("tao_rao=%d alpha_rao=%d evm_gas_wei=%d", cfg.MaximumTAORao, cfg.MaximumAlphaRao, cfg.MaximumEVMGasWei))
	r.add("vault/governance", true, validateGovernanceSeparation(cfg.Vault), "testnet=single-owner mainnet=safe-2-of-3")
	routingDetail := fmt.Sprintf("mode=%s substrate=%s evm=%s", cfg.OperationalRPCMode, redactURL(cfg.OperationalSubstrate), redactURL(cfg.OperationalEVM))
	r.add("config/operational-rpcs", true, validateOperationalRPCRouting(cfg), routingDetail)
	independentHard := independentRPCRequired(cfg)
	independenceDetail := "operational and postcondition RPC endpoints are distinct"
	if !independentHard {
		independenceDetail = "public override intentionally shares the operational and postcondition RPC provider; backend independence is not asserted"
	}
	r.add("config/independent-rpcs", independentHard, validateIndependentRPCEndpoints(cfg), independenceDetail)
	checkBlobConfig(ctx, &r, cfg)
	if err := ctx.Err(); err == nil {
		if sameRPCEndpoint(cfg.OperationalSubstrate, cfg.Public.Chain.SubstratePublicReadEndpoint) {
			start := len(r.Checks)
			checkSubstrate(&r, cfg, true)
			aliasSameEndpointChecks(&r, start, map[string]string{
				"rpc/substrate-operational":               "rpc/substrate-public",
				"runtime/code-hash-operational":           "runtime/code-hash-public",
				"runtime/metadata-operational":            "runtime/metadata-public",
				"runtime/release-call-shapes-operational": "runtime/release-call-shapes-public",
				"runtime/compatibility-gates-operational": "runtime/compatibility-gates-public",
				"subnet/activation-operational":           "subnet/activation-public",
				"subnet/uid-capacity-operational":         "subnet/uid-capacity-public",
				"subnet/owner-operational":                "subnet/owner-public",
			})
		} else {
			checkSubstrate(&r, cfg, false)
			checkSubstrate(&r, cfg, true)
		}
		topology := checkSubstrateTopology(cfg)
		r.add("rpc/substrate-operational-readiness", true, topology.ReadinessErr, topology.ReadinessDetail)
		r.add("rpc/substrate-physical-independence", independentHard, topology.IndependenceErr, topology.IndependenceDetail)
		checkEVM(ctx, &r, cfg)
		facts, factsErr := ReadSetupFacts(ctx, cfg)
		detail := ""
		if facts != nil {
			detail = fmt.Sprintf("source=%s alpha_rao=%d burn_rao=%d existential_deposit_rao=%d finalized=%d", facts.AlphaSourceHotkey, facts.AlphaAvailableRao, facts.BurnRao, facts.ExistentialDepositRao, facts.FinalizedBlock)
		}
		r.add("wallet/finalized-alpha-source", true, factsErr, detail)
		if factsErr == nil {
			var planErr error
			var plan *SetupPlan
			if approved != nil {
				plan = approved.Plan
				planErr = validateApprovedSetupFacts(plan, facts, approved.Remaining)
			} else {
				roles, roleErr := derivePublicRoles(cfg)
				if roleErr == nil {
					plan, planErr = buildPlan(cfg, facts, roles, time.Unix(0, 0).UTC())
				} else {
					planErr = roleErr
				}
			}
			planDetail := ""
			if plan != nil {
				required := plan.MaximumSpend
				if approved != nil {
					required = approved.Remaining
				}
				planDetail = fmt.Sprintf("tao_rao=%d/%d alpha_rao=%d/%d gas_wei=%d/%d registrations=%d/%d", required.TAORao, plan.Limits.TAORao, required.AlphaRao, plan.Limits.AlphaRao, required.EVMGasWei, plan.Limits.EVMGasWei, required.Registrations, plan.Limits.Registrations)
				if approved == nil && facts.WalletFreeTAORao < plan.MaximumSpend.TAORao {
					planErr = fmt.Errorf("wallet free TAO %d rao is below planned maximum outflow %d rao", facts.WalletFreeTAORao, plan.MaximumSpend.TAORao)
				}
			}
			r.add("wallet/setup-budget-and-balance", true, planErr, planDetail)
		}
	}
	for _, c := range r.Checks {
		if c.Hard && !c.OK {
			r.Ready = false
		}
	}
	sort.SliceStable(r.Checks, func(i, j int) bool { return r.Checks[i].Name < r.Checks[j].Name })
	return r
}

// Public override mode is deliberately a preliminary testnet assurance level;
// mainnet promotion still requires a separately operated observation backend.
func independentRPCRequired(cfg *ResolvedConfig) bool {
	return cfg == nil || cfg.OperationalRPCMode != rpcModePublicOverride
}

// sameRPCEndpoint recognizes syntactically equivalent URLs without treating
// credentials, query strings, or distinct paths as interchangeable authorities.
// Public override mode uses this to perform one honest observation instead of
// opening duplicate connections and presenting them as independent evidence.
func sameRPCEndpoint(left, right string) bool {
	normalize := func(raw string) (string, bool) {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", false
		}
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		if u.Path == "" {
			u.Path = "/"
		}
		if len(u.Path) > 1 {
			u.Path = strings.TrimRight(u.Path, "/")
		}
		return u.String(), true
	}
	a, aOK := normalize(left)
	b, bOK := normalize(right)
	return aOK && bOK && a == b
}

func aliasSameEndpointChecks(report *DoctorReport, start int, names map[string]string) {
	if report == nil || start < 0 || start > len(report.Checks) {
		return
	}
	source := append([]Check(nil), report.Checks[start:]...)
	for _, check := range source {
		alias, ok := names[check.Name]
		if !ok {
			continue
		}
		check.Name = alias
		if check.Detail == "" {
			check.Detail = "same_endpoint_observation=true"
		} else {
			check.Detail += "; same_endpoint_observation=true"
		}
		report.Checks = append(report.Checks, check)
	}
}

// The release binaries and pinned dependency images are qualified only on the
// architecture declared by the deployment profile.
func validateHostPlatform(goos, goarch string) error {
	if goos != "linux" || goarch != "amd64" {
		return fmt.Errorf("host platform is %s/%s, want linux/amd64", goos, goarch)
	}
	return nil
}

// Compare mutable finalized facts to the reviewed plan without invalidating it
// merely because prior journaled actions consumed their approved balances.
func validateApprovedSetupFacts(plan *SetupPlan, current *SetupFacts, remaining Spend) error {
	if plan == nil || current == nil {
		return errors.New("approved plan and finalized setup facts are required")
	}
	approved := plan.LiveFacts
	if current.ExistentialDepositRao != approved.ExistentialDepositRao || current.ExistentialDepositRao == 0 {
		return fmt.Errorf("runtime existential deposit changed from approved %d to %d rao", approved.ExistentialDepositRao, current.ExistentialDepositRao)
	}
	if remaining.Registrations > 0 && (plan.RegistrationBurnLimitRao == 0 || current.BurnRao > plan.RegistrationBurnLimitRao) {
		return fmt.Errorf("registration burn is %d rao, remaining actions are capped at %d", current.BurnRao, plan.RegistrationBurnLimitRao)
	}
	if current.NominatorMinimumRao != approved.NominatorMinimumRao || current.ProbeTAORao != approved.ProbeTAORao {
		return fmt.Errorf("staking precompile minimum changed from approved %d/%d to %d/%d", approved.NominatorMinimumRao, approved.ProbeTAORao, current.NominatorMinimumRao, current.ProbeTAORao)
	}
	if remaining.AlphaRao > 0 && current.AlphaSourceHotkey != approved.AlphaSourceHotkey {
		return fmt.Errorf("alpha source changed from approved %s to %s", approved.AlphaSourceHotkey, current.AlphaSourceHotkey)
	}
	if current.AlphaAvailableRao < remaining.AlphaRao {
		return fmt.Errorf("approved alpha source has %d rao, remaining actions require %d", current.AlphaAvailableRao, remaining.AlphaRao)
	}
	if current.WalletFreeTAORao < remaining.TAORao {
		return fmt.Errorf("wallet free TAO is %d rao, remaining actions require %d", current.WalletFreeTAORao, remaining.TAORao)
	}
	return validatePlanBudget(plan)
}

func validateGovernanceSeparation(vault map[string]any) error {
	if err := expectString(vault["testnet-contract-governance"], "single-owner"); err != nil {
		return fmt.Errorf("testnet governance: %w", err)
	}
	if err := expectString(vault["contract_governance"], "safe-2-of-3"); err != nil {
		return fmt.Errorf("mainnet governance: %w", err)
	}
	return nil
}

func validateRepoIdentity(name, path string) error {
	if path == "" {
		return fmt.Errorf("empty repository path")
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return err
	}
	switch name {
	case "sn", "server":
		b, err := os.ReadFile(filepath.Join(path, "go.mod"))
		if err != nil {
			return err
		}
		want := "github.com/urfoundation/sn"
		if name == "server" {
			want = "github.com/urnetwork/server"
		}
		if !strings.Contains(string(b), "module "+want) {
			return fmt.Errorf("go.mod is not %s", want)
		}
	case "vault":
		if _, err := os.Stat(filepath.Join(path, "main", "st.yml")); err != nil {
			return err
		}
	case "platform-config":
		if _, err := os.Stat(filepath.Join(path, "local", "settings.yml")); err != nil {
			return err
		}
	}
	return nil
}
func nonempty(v, msg string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s", msg)
	}
	return nil
}
func nonzero(v uint64, msg string) error {
	if v == 0 {
		return fmt.Errorf("%s", msg)
	}
	return nil
}
func allNonzero(v ...uint64) error {
	for _, n := range v {
		if n == 0 {
			return fmt.Errorf("all testnet spending limits must be nonzero")
		}
	}
	return nil
}
func expectString(v any, want string) error {
	if fmt.Sprint(v) != want {
		return fmt.Errorf("got %q, want %q", fmt.Sprint(v), want)
	}
	return nil
}

// Require an exact 200 response without following a misleading redirect.
func checkHTTPHealth(ctx context.Context, endpoint string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("health endpoint redirected")
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}

// Parse the same non-secret MinIO resource consumed by server/blob and reject
// a health-only endpoint whose application service account is incomplete.
func validateBlobConfig(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var config struct {
		Authority string `yaml:"authority"`
		AccessKey string `yaml:"access_key"`
		SecretKey string `yaml:"secret_key"`
		Bucket    string `yaml:"bucket"`
		TLS       bool   `yaml:"tls"`
		Prefix    string `yaml:"prefix"`
	}
	if err := yaml.Unmarshal(b, &config); err != nil {
		return fmt.Errorf("decode server/blob MinIO config: %w", err)
	}
	if strings.TrimSpace(config.Authority) == "" || strings.TrimSpace(config.AccessKey) == "" || strings.TrimSpace(config.SecretKey) == "" {
		return errors.New("server/blob MinIO authority or service-account credentials are empty")
	}
	if config.Bucket != "blob" {
		return fmt.Errorf("server/blob MinIO bucket is %q, want blob", config.Bucket)
	}
	return nil
}

// Check both server/blob configuration completeness and endpoint liveness.
func checkBlobConfig(ctx context.Context, r *DoctorReport, cfg *ResolvedConfig) {
	path := filepath.Join(cfg.Repos.Vault, "main", "minio.yml")
	err := validateBlobConfig(path)
	r.add("artifact-store/server-blob", true, err, path)
	if err == nil {
		endpoint := (&url.URL{Scheme: "http", Host: net.JoinHostPort(cfg.ObjectStoreHost, "23900"), Path: "/minio/health/live"}).String()
		healthErr := checkHTTPHealth(ctx, endpoint)
		r.add("artifact-store/minio-live", true, healthErr, endpoint)
	}
}

func authorityURLs(authority string) (wsURL, httpURL string, err error) {
	a := strings.TrimSpace(authority)
	if a == "" {
		return "", "", fmt.Errorf("empty authority")
	}
	if strings.Contains(a, "{{") {
		return "", "", fmt.Errorf("authority contains an unresolved template")
	}
	if strings.Contains(a, "://") {
		u, e := url.Parse(a)
		if e != nil {
			return "", "", e
		}
		u.User = nil
		switch u.Scheme {
		case "ws", "wss":
			wsURL = u.String()
			if u.Scheme == "ws" {
				u.Scheme = "http"
			} else {
				u.Scheme = "https"
			}
			httpURL = u.String()
		case "http", "https":
			httpURL = u.String()
			if u.Scheme == "http" {
				u.Scheme = "ws"
			} else {
				u.Scheme = "wss"
			}
			wsURL = u.String()
		default:
			return "", "", fmt.Errorf("unsupported authority scheme %q", u.Scheme)
		}
	} else {
		wsURL = "ws://" + a
		httpURL = "http://" + a
	}
	return
}

// Use a single exact finalized block so shape-restricted production gateways
// can prove log support without admitting an unbounded symbolic range.
func finalizedLogProbe(blockNumber uint64) map[string]any {
	block := fmt.Sprintf("0x%x", blockNumber)
	return map[string]any{"fromBlock": block, "toBlock": block}
}

func checkSubstrate(r *DoctorReport, cfg *ResolvedConfig, operational bool) {
	name := "public"
	endpoint := cfg.Public.Chain.SubstratePublicReadEndpoint
	if operational {
		name = "operational"
		endpoint = cfg.OperationalSubstrate
	}
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		r.add("rpc/substrate-"+name, true, err, redactURL(endpoint))
		return
	}
	defer chain.API.Client.Close()
	if strings.ToLower(chain.GenesisHash.Hex()) != testnetGenesis {
		err = fmt.Errorf("genesis %s, want %s", chain.GenesisHash.Hex(), testnetGenesis)
	} else if uint32(chain.Runtime.SpecVersion) != cfg.Public.Chain.ExpectedRuntimeSpec {
		err = fmt.Errorf("runtime spec %d, want %d", chain.Runtime.SpecVersion, cfg.Public.Chain.ExpectedRuntimeSpec)
	} else if uint32(chain.Runtime.TransactionVersion) != cfg.Public.Chain.ExpectedTransactionVersion {
		err = fmt.Errorf("transaction version %d, want %d", chain.Runtime.TransactionVersion, cfg.Public.Chain.ExpectedTransactionVersion)
	}
	r.add("rpc/substrate-"+name, true, err, fmt.Sprintf("%s genesis=%s spec=%d tx=%d", redactURL(endpoint), chain.GenesisHash.Hex(), chain.Runtime.SpecVersion, chain.Runtime.TransactionVersion))
	if err == nil {
		codeHash, codeHashErr := finalizedRuntimeCodeHash(chain)
		if codeHashErr == nil {
			codeHashErr = validateRuntimeCodeHash(codeHash, cfg.Release.Runtime.CodeHash)
		}
		r.add("runtime/code-hash-"+name, true, codeHashErr, codeHash)
		metadata, metadataErr := chain.CheckMetadata()
		metadataDetail := ""
		if metadata != nil {
			metadataDetail = fmt.Sprintf("pallet_index=%d signed_extensions=%d", metadata.PalletIndex, len(metadata.Extensions))
			if metadataErr == nil && len(metadata.Problems) != 0 {
				metadataErr = fmt.Errorf("%s", strings.Join(metadata.Problems, "; "))
			}
		}
		r.add("runtime/metadata-"+name, true, metadataErr, metadataDetail)
		r.add("runtime/release-call-shapes-"+name, true, checkReleaseCallShapes(chain, cfg), "registration, staking/activation, take, transfer, commitments, and owner calls")
		gates, gateErr := verifyCompatibilityGates(chain, cfg)
		gateDetail := ""
		if gates != nil {
			if encoded, encodeErr := json.Marshal(gates); encodeErr == nil {
				gateDetail = string(encoded)
			}
		}
		r.add("runtime/compatibility-gates-"+name, true, gateErr, gateDetail)
		activation, activationErr := readAndValidateSubnetActivation(chain, cfg)
		activationDetail := fmt.Sprintf("enabled=%t registered_at=%d first_emission=%d finalized=%d", activation.SubtokenEnabled, activation.NetworkRegisteredAt, activation.FirstEmissionBlockNumber, activation.FinalizedBlock)
		r.add("subnet/activation-"+name, true, activationErr, activationDetail)
		r.add("subnet/uid-capacity-"+name, true, checkUIDCapacity(chain, cfg), "all missing release identities fit the intended finalized UID cap")
	}
	if err == nil && cfg.Netuid != 0 && cfg.WalletPublic != "" {
		ownerErr, detail := verifySubnetOwner(chain, cfg.Netuid, cfg.WalletPublic)
		r.add("subnet/owner-"+name, true, ownerErr, detail)
	}
	if operational {
		var logs any
		finalizedHash, finalizedErr := chain.API.RPC.Chain.GetFinalizedHead()
		var finalizedNumber uint64
		if finalizedErr == nil {
			var header *types.Header
			header, finalizedErr = chain.API.RPC.Chain.GetHeader(finalizedHash)
			if finalizedErr == nil {
				finalizedNumber = uint64(header.Number)
			}
		}
		if finalizedErr == nil {
			finalizedErr = chain.API.Client.Call(&logs, "eth_getLogs", finalizedLogProbe(finalizedNumber))
		}
		r.add("rpc/operational-eth_getLogs", true, finalizedErr, fmt.Sprintf("exact finalized block=%d", finalizedNumber))
	}
}

// Pin the exact on-chain Wasm at a finalized block. A spec version alone is
// not a content identity and can be reused accidentally by an upstream build.
func finalizedRuntimeCodeHash(chain *crv4.Chain) (string, error) {
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return "", err
	}
	var codeHash string
	if err := chain.API.Client.Call(&codeHash, "state_getStorageHash", "0x3a636f6465", finalized.Hex()); err != nil {
		return "", err
	}
	if err := validateRuntimeCodeHash(codeHash, codeHash); err != nil {
		return "", err
	}
	return strings.ToLower(codeHash), nil
}

func validateRuntimeCodeHash(observed, expected string) error {
	validate := func(label, value string) error {
		if len(value) != 66 || !strings.HasPrefix(value, "0x") {
			return fmt.Errorf("invalid %s runtime code hash %q", label, value)
		}
		if _, err := hex.DecodeString(value[2:]); err != nil {
			return fmt.Errorf("invalid %s runtime code hash %q: %w", label, value, err)
		}
		return nil
	}
	if err := validate("finalized", observed); err != nil {
		return err
	}
	if err := validate("release-lock", expected); err != nil {
		return err
	}
	if !strings.EqualFold(observed, expected) {
		return fmt.Errorf("finalized runtime code hash %s, want %s", observed, expected)
	}
	return nil
}

func validateSubnetActivation(state SubnetActivationState) error {
	if !state.SubtokenEnabled || state.NetworkRegisteredAt == 0 || state.FirstEmissionBlockNumber == 0 {
		return fmt.Errorf("subnet token/emission is not activated: %+v", state)
	}
	readyAt, ok := checkedAdd(state.NetworkRegisteredAt, state.StartCallDelay)
	if !ok || state.FirstEmissionBlockNumber < readyAt || state.FirstEmissionBlockNumber > state.FinalizedBlock {
		return fmt.Errorf("subnet activation block is outside the finalized lifetime: %+v", state)
	}
	return nil
}

func readAndValidateSubnetActivation(chain *crv4.Chain, cfg *ResolvedConfig) (SubnetActivationState, error) {
	state, err := (&SubstrateManager{chain: chain, cfg: cfg}).ActivationState()
	if err != nil {
		return state, err
	}
	return state, validateSubnetActivation(state)
}

// Extract unique physical libp2p identities from multiaddresses.
func peerIDsFromListenAddresses(addresses []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, address := range addresses {
		parts := strings.Split(address, "/")
		for index := 0; index+1 < len(parts); index++ {
			if parts[index] != "p2p" || parts[index+1] == "" || seen[parts[index+1]] {
				continue
			}
			seen[parts[index+1]] = true
			result = append(result, parts[index+1])
		}
	}
	sort.Strings(result)
	return result
}

// Extract the stable libp2p identity exposed by one already connected node.
func substratePeerIDs(chain *crv4.Chain) ([]string, error) {
	var addresses []string
	if err := chain.API.Client.Call(&addresses, "system_localListenAddresses"); err != nil {
		return nil, err
	}
	peers := peerIDsFromListenAddresses(addresses)
	if len(peers) == 0 {
		return nil, errors.New("system_localListenAddresses returned no physical peer identity")
	}
	return peers, nil
}

// Reject any physical identity shared by the write and observation endpoints.
func disjointSubstratePeers(operationalPeers, publicPeers []string) error {
	public := map[string]bool{}
	for _, peer := range publicPeers {
		public[peer] = true
	}
	for _, peer := range operationalPeers {
		if public[peer] {
			return fmt.Errorf("operational and public RPCs expose the same physical Subtensor peer %s", peer)
		}
	}
	return nil
}

// Reject an operational endpoint which is only another route to the public backend.
func checkSubstratePeerIndependence(operationalChain, publicChain *crv4.Chain) (string, error) {
	operationalPeers, err := substratePeerIDs(operationalChain)
	if err != nil {
		return "", fmt.Errorf("operational RPC physical identity: %w", err)
	}
	publicPeers, err := substratePeerIDs(publicChain)
	if err != nil {
		return "", fmt.Errorf("public RPC physical identity: %w", err)
	}
	detail := fmt.Sprintf("operational=%s public=%s", strings.Join(operationalPeers, ","), strings.Join(publicPeers, ","))
	return detail, disjointSubstratePeers(operationalPeers, publicPeers)
}

// substrateHealth is the node's direct declaration of whether it is connected
// and catching up. A reachable RPC can otherwise look healthy while stranded
// on an old finalized state, as happened during the testnet archive bootstrap.
type substrateHealth struct {
	Peers           uint64 `json:"peers"`
	IsSyncing       bool   `json:"isSyncing"`
	ShouldHavePeers bool   `json:"shouldHavePeers"`
}

// substrateReadinessObservation binds peer health to a canonical checkpoint
// independently read from the operational and public physical nodes.
type substrateReadinessObservation struct {
	Health                    substrateHealth
	OperationalFinalized      uint64
	PublicFinalized           uint64
	Checkpoint                uint64
	OperationalCheckpointHash types.Hash
	PublicCheckpointHash      types.Hash
}

// substrateTopologyChecks keeps the independent readiness and physical-node
// results separate while sharing the same two RPC connections.
type substrateTopologyChecks struct {
	ReadinessDetail    string
	ReadinessErr       error
	IndependenceDetail string
	IndependenceErr    error
}

// Reject any endpoint which cannot prove both live consensus participation and
// agreement with an independently operated finalized chain.
func validateSubstrateReadiness(observation substrateReadinessObservation, maximumLag uint64) error {
	var problems []string
	if maximumLag == 0 {
		problems = append(problems, "maximum finalized-head lag is zero")
	}
	if !observation.Health.ShouldHavePeers {
		problems = append(problems, "node does not expect peers")
	}
	if observation.Health.Peers == 0 {
		problems = append(problems, "node has zero connected peers")
	}
	if observation.Health.IsSyncing {
		problems = append(problems, "node is still syncing")
	}
	if observation.PublicFinalized > observation.OperationalFinalized && observation.PublicFinalized-observation.OperationalFinalized > maximumLag {
		problems = append(problems, fmt.Sprintf("operational finalized head lags public by %d blocks (maximum %d)", observation.PublicFinalized-observation.OperationalFinalized, maximumLag))
	}
	if observation.Checkpoint == 0 || observation.OperationalCheckpointHash == (types.Hash{}) || observation.PublicCheckpointHash == (types.Hash{}) {
		problems = append(problems, "canonical checkpoint is unavailable")
	} else if observation.OperationalCheckpointHash != observation.PublicCheckpointHash {
		problems = append(problems, fmt.Sprintf("operational/public canonical hashes disagree at block %d", observation.Checkpoint))
	}
	if len(problems) != 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// Read the number corresponding to the node's explicit finalized hash.
func finalizedSubstrateNumber(chain *crv4.Chain) (uint64, error) {
	hash, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return 0, err
	}
	header, err := chain.API.RPC.Chain.GetHeader(hash)
	if err != nil {
		return 0, err
	}
	return uint64(header.Number), nil
}

// Observe both physical endpoints at one common finalized checkpoint.
func checkSubstrateReadiness(operationalChain, publicChain *crv4.Chain, maximumLag uint64) (string, error) {
	var observation substrateReadinessObservation
	var err error
	if err := operationalChain.API.Client.Call(&observation.Health, "system_health"); err != nil {
		return "", fmt.Errorf("operational system_health: %w", err)
	}
	observation.OperationalFinalized, err = finalizedSubstrateNumber(operationalChain)
	if err != nil {
		return "", fmt.Errorf("operational finalized head: %w", err)
	}
	observation.PublicFinalized, err = finalizedSubstrateNumber(publicChain)
	if err != nil {
		return "", fmt.Errorf("public finalized head: %w", err)
	}
	observation.Checkpoint = observation.OperationalFinalized
	if observation.PublicFinalized < observation.Checkpoint {
		observation.Checkpoint = observation.PublicFinalized
	}
	var observationErrors []error
	if observation.Checkpoint != 0 {
		observation.OperationalCheckpointHash, err = operationalChain.API.RPC.Chain.GetBlockHash(observation.Checkpoint)
		if err != nil {
			observationErrors = append(observationErrors, fmt.Errorf("operational checkpoint %d: %w", observation.Checkpoint, err))
		}
		observation.PublicCheckpointHash, err = publicChain.API.RPC.Chain.GetBlockHash(observation.Checkpoint)
		if err != nil {
			observationErrors = append(observationErrors, fmt.Errorf("public checkpoint %d: %w", observation.Checkpoint, err))
		}
	}
	if readinessErr := validateSubstrateReadiness(observation, maximumLag); readinessErr != nil {
		observationErrors = append(observationErrors, readinessErr)
	}
	detail := fmt.Sprintf("peers=%d syncing=%t operational_finalized=%d public_finalized=%d checkpoint=%d", observation.Health.Peers, observation.Health.IsSyncing, observation.OperationalFinalized, observation.PublicFinalized, observation.Checkpoint)
	return detail, errors.Join(observationErrors...)
}

// Check live consensus state and backend independence with one connection per
// endpoint, reducing public RPC pressure and keeping both observations close.
func checkSubstrateTopology(cfg *ResolvedConfig) substrateTopologyChecks {
	operationalChain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		err = fmt.Errorf("operational RPC: %w", err)
		return substrateTopologyChecks{ReadinessErr: err, IndependenceErr: err}
	}
	defer operationalChain.API.Client.Close()
	if sameRPCEndpoint(cfg.OperationalSubstrate, cfg.Public.Chain.SubstratePublicReadEndpoint) {
		result := substrateTopologyChecks{}
		result.ReadinessDetail, result.ReadinessErr = checkSubstrateReadiness(operationalChain, operationalChain, uint64(cfg.Policy.Safety.MaximumFinalizedHeadLagBlocks))
		result.IndependenceDetail, result.IndependenceErr = checkSubstratePeerIndependence(operationalChain, operationalChain)
		return result
	}
	publicChain, err := crv4.DialChain(cfg.Public.Chain.SubstratePublicReadEndpoint)
	if err != nil {
		err = fmt.Errorf("public RPC: %w", err)
		return substrateTopologyChecks{ReadinessErr: err, IndependenceErr: err}
	}
	defer publicChain.API.Client.Close()

	result := substrateTopologyChecks{}
	result.ReadinessDetail, result.ReadinessErr = checkSubstrateReadiness(operationalChain, publicChain, uint64(cfg.Policy.Safety.MaximumFinalizedHeadLagBlocks))
	result.IndependenceDetail, result.IndependenceErr = checkSubstratePeerIndependence(operationalChain, publicChain)
	return result
}

type releaseCallRequirement struct {
	Pallet string
	Call   string
	Names  []string
	Shapes []string
}

func checkReleaseCallShapes(chain *crv4.Chain, cfg *ResolvedConfig) error {
	requirements := []releaseCallRequirement{
		{
			Pallet: crv4.PalletName,
			Call:   "burned_register",
			Names:  []string{"netuid", "hotkey"},
			Shapes: []string{"u16", "[u8;32]"},
		},
		{
			Pallet: crv4.PalletName,
			Call:   "register_limit",
			Names:  []string{"netuid", "hotkey", "limit_price"},
			Shapes: []string{"u16", "[u8;32]", "u64"},
		},
		{
			Pallet: crv4.PalletName,
			Call:   "add_stake",
			Names:  []string{"hotkey", "netuid", "amount_staked"},
			Shapes: []string{"[u8;32]", "u16", "u64"},
		},
		{
			Pallet: crv4.PalletName,
			Call:   "add_stake_limit",
			Names:  []string{"hotkey", "netuid", "amount_staked", "limit_price", "allow_partial"},
			Shapes: []string{"[u8;32]", "u16", "u64", "u64", "bool"},
		},
		{
			Pallet: crv4.PalletName,
			Call:   "start_call",
			Names:  []string{"netuid"},
			Shapes: []string{"u16"},
		},
		{
			Pallet: crv4.PalletName,
			Call:   "decrease_take",
			Names:  []string{"hotkey", "take"},
			Shapes: []string{"[u8;32]", "u16"},
		},
		{
			Pallet: crv4.PalletName,
			Call:   "transfer_stake_and_hotkey",
			Names:  []string{"destination_coldkey", "origin_hotkey", "destination_hotkey", "origin_netuid", "destination_netuid", "alpha_amount"},
			Shapes: []string{"[u8;32]", "[u8;32]", "[u8;32]", "u16", "u16", "u64"},
		},
	}
	for _, requirement := range requirements {
		report, err := chain.DescribeCall(requirement.Pallet, requirement.Call)
		if err != nil {
			return err
		}
		if !report.Found {
			return fmt.Errorf("call %s.%s not found", requirement.Pallet, requirement.Call)
		}
		if len(report.Args) != len(requirement.Names) {
			return fmt.Errorf("call %s.%s has %d arguments, want %d", requirement.Pallet, requirement.Call, len(report.Args), len(requirement.Names))
		}
		for i, argument := range report.Args {
			if argument.Name != requirement.Names[i] || argument.Shape != requirement.Shapes[i] {
				return fmt.Errorf("call %s.%s argument %d is %s:%s, want %s:%s", requirement.Pallet, requirement.Call, i, argument.Name, argument.Shape, requirement.Names[i], requirement.Shapes[i])
			}
		}
	}
	manager := &SubstrateManager{chain: chain, cfg: cfg}
	for name, value := range cfg.Hyperparameters.OwnerControlled {
		if _, err := manager.HyperCall(name, value); err != nil {
			return fmt.Errorf("owner hyperparameter call %s: %w", name, err)
		}
	}
	if _, err := chain.NewSetFleetCommitmentCall(cfg.Netuid, [32]byte{1}); err != nil {
		return fmt.Errorf("commitments call shape: %w", err)
	}
	return nil
}

func checkUIDCapacity(chain *crv4.Chain, cfg *ResolvedConfig) error {
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return err
	}
	key, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "SubnetworkN", netuidArg(cfg.Netuid))
	if err != nil {
		return err
	}
	var current types.U16
	if ok, readErr := chain.API.RPC.State.GetStorage(key, &current, finalized); readErr != nil || !ok {
		if readErr != nil {
			return readErr
		}
		return fmt.Errorf("SubnetworkN is absent for netuid %d", cfg.Netuid)
	}
	maxValue, ok := cfg.Hyperparameters.OwnerControlled["max_allowed_uids"]
	if !ok {
		return fmt.Errorf("max_allowed_uids intent is missing")
	}
	normalized, err := normalizeYAMLValue(maxValue, "u16")
	if err != nil {
		return err
	}
	maximum := normalized.(uint64)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return err
	}
	labels := []string{"escrow-hotkey"}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		labels = append(labels, churnHotkeyLabel(churn))
	}
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		labels = append(labels, fleetHotkeyLabel(fleet))
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		labels = append(labels, fmt.Sprintf("operator-%d-pool-hotkey", i), fmt.Sprintf("operator-%d-deposit-hotkey", i))
	}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		labels = append(labels, validatorHotkeyLabel(i))
	}
	uidKeys := make([]types.StorageKey, 0, len(labels))
	for _, label := range labels {
		hotkey, decodeErr := decodeHex32(label, roles.Substrate[label].PublicKeyHex)
		if decodeErr != nil {
			return decodeErr
		}
		uidKey, keyErr := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Uids", netuidArg(cfg.Netuid), hotkey[:])
		if keyErr != nil {
			return keyErr
		}
		uidKeys = append(uidKeys, uidKey)
	}
	changeSets, err := chain.API.RPC.State.QueryStorageAt(uidKeys, finalized)
	if err != nil {
		return err
	}
	missing, err := countMissingStorageKeysAt(uidKeys, changeSets, finalized)
	if err != nil {
		return fmt.Errorf("batched finalized UID lookup: %w", err)
	}
	needed := uint64(current) + missing
	if needed > maximum {
		return fmt.Errorf("netuid %d has %d UIDs and needs %d missing release registrations, exceeding intended maximum %d", cfg.Netuid, current, missing, maximum)
	}
	return nil
}

// countMissingStorageKeysAt validates the complete response to one finalized
// state_queryStorageAt batch. A provider that drops a key, returns another block,
// or duplicates a row cannot make an unknown UID look absent or present.
func countMissingStorageKeysAt(keys []types.StorageKey, changeSets []types.StorageChangeSet, block types.Hash) (uint64, error) {
	if len(keys) == 0 {
		return 0, errors.New("storage-key batch is empty")
	}
	expected := make(map[string]bool, len(keys))
	for _, key := range keys {
		hexKey := key.Hex()
		if expected[hexKey] {
			return 0, fmt.Errorf("duplicate requested storage key %s", hexKey)
		}
		expected[hexKey] = true
	}
	if len(changeSets) != 1 {
		return 0, fmt.Errorf("storage response has %d change sets, want one", len(changeSets))
	}
	set := changeSets[0]
	if set.Block != block {
		return 0, fmt.Errorf("storage response block %s, want %s", set.Block.Hex(), block.Hex())
	}
	seen := make(map[string]bool, len(set.Changes))
	missing := uint64(0)
	for _, change := range set.Changes {
		hexKey := change.StorageKey.Hex()
		if !expected[hexKey] {
			return 0, fmt.Errorf("storage response contains unrequested key %s", hexKey)
		}
		if seen[hexKey] {
			return 0, fmt.Errorf("storage response duplicates key %s", hexKey)
		}
		seen[hexKey] = true
		if !change.HasStorageData {
			missing++
		}
	}
	if len(seen) != len(expected) {
		return 0, fmt.Errorf("storage response contains %d of %d requested keys", len(seen), len(expected))
	}
	return missing, nil
}

func verifySubnetOwner(chain *crv4.Chain, netuid uint16, walletAddress string) (error, string) {
	var arg [2]byte
	binary.LittleEndian.PutUint16(arg[:], netuid)
	key, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "SubnetOwner", arg[:])
	if err != nil {
		return err, ""
	}
	var owner types.AccountID
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return err, ""
	}
	ok, err := chain.API.RPC.State.GetStorage(key, &owner, finalized)
	if err != nil {
		return err, ""
	}
	if !ok {
		return fmt.Errorf("netuid %d has no owner", netuid), ""
	}
	ownerSS58, err := ss58.Encode([32]byte(owner), 42)
	if err != nil {
		return err, ""
	}
	if ownerSS58 != walletAddress {
		return fmt.Errorf("wallet %s does not own netuid %d (owner %s)", walletAddress, netuid, ownerSS58), ownerSS58
	}
	return nil, ownerSS58
}

const doctorPrecompileABI = `[
{"type":"function","name":"verify","inputs":[{"name":"message","type":"bytes32"},{"name":"publicKey","type":"bytes32"},{"name":"r","type":"bytes32"},{"name":"s","type":"bytes32"}],"outputs":[{"name":"","type":"bool"}],"stateMutability":"view"},
{"type":"function","name":"getUidCount","inputs":[{"name":"netuid","type":"uint16"}],"outputs":[{"name":"","type":"uint16"}],"stateMutability":"view"},
{"type":"function","name":"getHotkey","inputs":[{"name":"netuid","type":"uint16"},{"name":"uid","type":"uint16"}],"outputs":[{"name":"","type":"bytes32"}],"stateMutability":"view"},
{"type":"function","name":"getColdkey","inputs":[{"name":"netuid","type":"uint16"},{"name":"uid","type":"uint16"}],"outputs":[{"name":"","type":"bytes32"}],"stateMutability":"view"},
{"type":"function","name":"getUid","inputs":[{"name":"netuid","type":"uint16"},{"name":"hotkey","type":"bytes32"}],"outputs":[{"name":"exists","type":"bool"},{"name":"uid","type":"uint16"}],"stateMutability":"view"},
{"type":"function","name":"getStake","inputs":[{"name":"hotkey","type":"bytes32"},{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint256"}],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"},
{"type":"function","name":"getNominatorMinRequiredStake","inputs":[],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"}
]`

var (
	doctorEdKAT = [4][32]byte{
		common.HexToHash("0xca6dd518081710a6081369b7d2eb0cf32396bf77c9f091be21e6d4c8ed37a6cb"),
		common.HexToHash("0x3f0d9ad990f7706d891de2dd0a52cc68a6cc631683a31977bb38b9f189d26de1"),
		common.HexToHash("0x2e530da93345ff099a7c46cb9aab8d964a7a016852b567e074f64f9cf1d5cf30"),
		common.HexToHash("0x35a13c64140c12e523a8e5fec6541fa846be95974aa399f81fc907d020955f0e"),
	}
	doctorSrKAT = [4][32]byte{
		common.HexToHash("0x0de356fd56fc28d72efe5724a81b2462a7f2bb3f041f48128e2d511b0ae05ba7"),
		common.HexToHash("0x94ad8d1ead1a2bff9bbbac89aa89b13df2fe9ec929a09c90bc5ddb1dff723b47"),
		common.HexToHash("0xf4edfe605b1a20514ce7cd0323e32eee364d10b706292028f234d3edde2b5527"),
		common.HexToHash("0xc93b12e32a60f8f531875060d67b9feca33c9a42bf8bb20debef3aab4b4bf087"),
	}
)

func validateIndependentRPCEndpoints(cfg *ResolvedConfig) error {
	host := func(raw string) (string, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("invalid RPC endpoint %q", raw)
		}
		return strings.ToLower(u.Host), nil
	}
	operationalSubstrate, err := host(cfg.OperationalSubstrate)
	if err != nil {
		return err
	}
	publicSubstrate, err := host(cfg.Public.Chain.SubstratePublicReadEndpoint)
	if err != nil {
		return err
	}
	operationalEVM, err := host(cfg.OperationalEVM)
	if err != nil {
		return err
	}
	publicEVM, err := host(cfg.Public.Chain.EVMPublicReadEndpoint)
	if err != nil {
		return err
	}
	if operationalSubstrate == publicSubstrate || operationalEVM == publicEVM {
		return errors.New("public postcondition RPC is shared with the operational endpoint")
	}
	return nil
}

// Writers must always reproduce the configured operational route. Physical
// endpoint independence remains mandatory for private-authority releases, while
// the explicitly lower-assurance public testnet override records it as absent.
func validateExecutionRPCConfiguration(cfg *ResolvedConfig) error {
	if err := validateOperationalRPCRouting(cfg); err != nil {
		return err
	}
	if independentRPCRequired(cfg) {
		return validateIndependentRPCEndpoints(cfg)
	}
	return nil
}

// Recompute endpoint selection so an in-memory or deserialization mismatch
// cannot route writes differently from the configuration bound into the plan.
func validateOperationalRPCRouting(cfg *ResolvedConfig) error {
	substrate, evm, mode, err := resolveOperationalRPCs(cfg.Authority, cfg.Config.LaunchInputs.PublicSubstrateRPCOverride, cfg.Config.LaunchInputs.PublicEVMRPCOverride)
	if err != nil {
		return err
	}
	if substrate != cfg.OperationalSubstrate || evm != cfg.OperationalEVM || mode != cfg.OperationalRPCMode {
		return errors.New("resolved operational RPC routing does not match the launch configuration")
	}
	return nil
}

func checkEVM(parent context.Context, r *DoctorReport, cfg *ResolvedConfig) {
	start := len(r.Checks)
	checkEVMEndpoint(parent, r, cfg, "operational", cfg.OperationalEVM)
	if sameRPCEndpoint(cfg.OperationalEVM, cfg.Public.Chain.EVMPublicReadEndpoint) {
		aliasSameEndpointChecks(r, start, map[string]string{
			"rpc/evm-operational":                 "rpc/evm-public",
			"runtime/evm-finality":                "runtime/evm-finality-public",
			"runtime/precompile-abi":              "runtime/precompile-abi-public",
			"runtime/precompile-signatures":       "runtime/precompile-signatures-public",
			"runtime/precompile-metagraph-neuron": "runtime/precompile-metagraph-neuron-public",
			"runtime/precompile-staking":          "runtime/precompile-staking-public",
		})
		return
	}
	checkEVMEndpoint(parent, r, cfg, "public", cfg.Public.Chain.EVMPublicReadEndpoint)
}

func checkEVMEndpoint(parent context.Context, r *DoctorReport, cfg *ResolvedConfig, name, httpURL string) {
	timeout := 20 * time.Second
	if configuredEVMRequestsPerMinute(cfg, httpURL) > 0 {
		// A public provider can legitimately direct a source-wide 60-second
		// cooldown. Keep the doctor bounded while allowing the transport to
		// honor that policy instead of converting it into false unavailability.
		timeout = 4 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	client, err := dialConfiguredEVMClient(ctx, cfg, httpURL)
	if err == nil {
		defer client.Close()
		id, e := client.ChainID(ctx)
		if e != nil {
			err = e
		} else if id.Uint64() != testnetChainID {
			err = fmt.Errorf("chain id %d, want %d", id.Uint64(), testnetChainID)
		}
	}
	r.add("rpc/evm-"+name, true, err, redactURL(httpURL))
	if err != nil {
		return
	}
	head, headErr := finalizedEVMHead(ctx, client)
	suffix := ""
	if name != "operational" {
		suffix = "-" + name
	}
	r.add("runtime/evm-finality"+suffix, true, headErr, fmt.Sprintf("number=%d hash=%s", head.Number, head.Hash))
	if headErr != nil {
		return
	}
	if name == "operational" {
		var historicalErr error
		var historicalBlock uint64
		if head.Number < 2 {
			historicalErr = errors.New("operational EVM finalized head is too early for historical-state verification")
		} else {
			historicalBlock = head.Number - 1
			_, historicalErr = client.BalanceAt(ctx, common.Address{}, new(big.Int).SetUint64(historicalBlock))
		}
		r.add("runtime/evm-historical-state", true, historicalErr, fmt.Sprintf("block=%d", historicalBlock))
	}
	parsed, parseErr := abi.JSON(strings.NewReader(doctorPrecompileABI))
	if parseErr != nil {
		r.add("runtime/precompile-abi"+suffix, true, parseErr, "")
		return
	}
	sigErr := checkDoctorSignaturePrecompiles(ctx, client, parsed, head.Number)
	r.add("runtime/precompile-signatures"+suffix, true, sigErr, "ed25519=good/bad sr25519=good/bad")
	identityDetail, identityErr := checkDoctorIdentityPrecompiles(ctx, client, parsed, head.Number, cfg)
	r.add("runtime/precompile-metagraph-neuron"+suffix, true, identityErr, identityDetail)
	stakeDetail, stakeErr := checkDoctorStakingPrecompile(ctx, client, parsed, head.Number, cfg)
	r.add("runtime/precompile-staking"+suffix, true, stakeErr, stakeDetail)
}

func checkDoctorSignaturePrecompiles(ctx context.Context, client *ethclient.Client, parsed abi.ABI, block uint64) error {
	for _, test := range []struct {
		name    string
		address common.Address
		kat     [4][32]byte
	}{
		{"ed25519", common.HexToAddress("0x402"), doctorEdKAT},
		{"sr25519", common.HexToAddress("0x403"), doctorSrKAT},
	} {
		good, err := contractCallAt(ctx, client, test.address, parsed, "verify", block, test.kat[0], test.kat[1], test.kat[2], test.kat[3])
		if err != nil || len(good) != 1 || !valueBool(good[0]) {
			return conformanceMismatch(test.name+" known-answer signature was not accepted", err)
		}
		badS := test.kat[3]
		badS[31] ^= 1
		bad, err := contractCallAt(ctx, client, test.address, parsed, "verify", block, test.kat[0], test.kat[1], test.kat[2], badS)
		if err != nil || len(bad) != 1 || valueBool(bad[0]) {
			return conformanceMismatch(test.name+" tampered signature was not rejected", err)
		}
	}
	return nil
}

func checkDoctorIdentityPrecompiles(ctx context.Context, client *ethclient.Client, parsed abi.ABI, block uint64, cfg *ResolvedConfig) (string, error) {
	metagraph := common.HexToAddress("0x802")
	neuron := common.HexToAddress("0x804")
	countValues, err := contractCallAt(ctx, client, metagraph, parsed, "getUidCount", block, cfg.Netuid)
	if err != nil || len(countValues) != 1 {
		return "", conformanceMismatch("metagraph UID count is unavailable", err)
	}
	count, ok := numericUint64(countValues[0])
	if !ok || count == 0 || count > 65535 {
		return "", fmt.Errorf("metagraph UID count is invalid: %v", countValues[0])
	}
	hotValues, err := contractCallAt(ctx, client, metagraph, parsed, "getHotkey", block, cfg.Netuid, uint16(0))
	if err != nil || len(hotValues) != 1 {
		return "", conformanceMismatch("metagraph UID 0 hotkey is unavailable", err)
	}
	coldValues, err := contractCallAt(ctx, client, metagraph, parsed, "getColdkey", block, cfg.Netuid, uint16(0))
	if err != nil || len(coldValues) != 1 {
		return "", conformanceMismatch("metagraph UID 0 coldkey is unavailable", err)
	}
	hotkey, hotOK := hotValues[0].([32]byte)
	coldkey, coldOK := coldValues[0].([32]byte)
	if !hotOK || !coldOK || hotkey == ([32]byte{}) || coldkey == ([32]byte{}) {
		return "", fmt.Errorf("metagraph UID 0 identities are invalid")
	}
	uidValues, err := contractCallAt(ctx, client, neuron, parsed, "getUid", block, cfg.Netuid, hotkey)
	if err != nil || len(uidValues) != 2 || !valueBool(uidValues[0]) || valueUint16(uidValues[1]) != 0 {
		return "", conformanceMismatch("neuron reverse lookup does not resolve metagraph UID 0", err)
	}
	absent := derive32(cfg, "doctor/absent-hotkey")
	absentValues, err := contractCallAt(ctx, client, neuron, parsed, "getUid", block, cfg.Netuid, absent)
	if err != nil || len(absentValues) != 2 || valueBool(absentValues[0]) {
		return "", conformanceMismatch("neuron absent-hotkey control was not rejected", err)
	}
	return fmt.Sprintf("uid_count=%d uid0_hotkey=%s uid0_coldkey=%s", count, hexBytesValue(hotkey[:]), hexBytesValue(coldkey[:])), nil
}

func checkDoctorStakingPrecompile(ctx context.Context, client *ethclient.Client, parsed abi.ABI, block uint64, cfg *ResolvedConfig) (string, error) {
	metagraph := common.HexToAddress("0x802")
	staking := common.HexToAddress("0x805")
	hotValues, err := contractCallAt(ctx, client, metagraph, parsed, "getHotkey", block, cfg.Netuid, uint16(0))
	if err != nil || len(hotValues) != 1 {
		return "", conformanceMismatch("staking sample hotkey is unavailable", err)
	}
	coldValues, err := contractCallAt(ctx, client, metagraph, parsed, "getColdkey", block, cfg.Netuid, uint16(0))
	if err != nil || len(coldValues) != 1 {
		return "", conformanceMismatch("staking sample coldkey is unavailable", err)
	}
	hotkey, hotOK := hotValues[0].([32]byte)
	coldkey, coldOK := coldValues[0].([32]byte)
	if !hotOK || !coldOK {
		return "", fmt.Errorf("staking sample identities have incompatible ABI types")
	}
	stakeValues, err := contractCallAt(ctx, client, staking, parsed, "getStake", block, hotkey, coldkey, new(big.Int).SetUint64(uint64(cfg.Netuid)))
	if err != nil || len(stakeValues) != 1 {
		return "", conformanceMismatch("staking getStake is unavailable", err)
	}
	stake, stakeOK := stakeValues[0].(*big.Int)
	minimumValues, minimumErr := contractCallAt(ctx, client, staking, parsed, "getNominatorMinRequiredStake", block)
	if minimumErr != nil || len(minimumValues) != 1 {
		return "", conformanceMismatch("staking nominator minimum is unavailable", minimumErr)
	}
	minimum, minimumOK := minimumValues[0].(*big.Int)
	if !stakeOK || stake == nil || stake.Sign() < 0 || !minimumOK || minimum == nil || minimum.Sign() < 0 || !minimum.IsUint64() {
		return "", fmt.Errorf("staking precompile returned incompatible values")
	}
	return fmt.Sprintf("uid0_stake_rao=%s nominator_minimum_rao=%s", stake.String(), minimum.String()), nil
}
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "<redacted-url>"
	}
	u.User = nil
	q := u.Query()
	for k := range q {
		q.Set(k, "REDACTED")
	}
	u.RawQuery = q.Encode()
	return u.String()
}
