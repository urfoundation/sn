package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
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

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/ss58"
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
	return fmt.Errorf("doctor found one or more hard failures")
}
func (r *DoctorReport) add(name string, hard bool, err error, detail string) {
	c := Check{Name: name, Hard: hard, OK: err == nil, Detail: detail}
	if err != nil {
		c.Detail = err.Error()
	}
	r.Checks = append(r.Checks, c)
}

func RunDoctor(ctx context.Context, cfg *ResolvedConfig) DoctorReport {
	r := DoctorReport{Schema: "urnetwork-sim-doctor-v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339), ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Ready: true}
	r.add("host/linux-amd64", true, nil, runtime.GOOS+"/"+runtime.GOARCH)
	for _, tool := range []string{"go", "git", "docker"} {
		p, err := exec.LookPath(tool)
		r.add("tool/"+tool, true, err, p)
	}
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
	r.add("config/independent-rpcs", true, validateIndependentRPCEndpoints(cfg), "private write/finality endpoints are distinct from public postcondition endpoints")
	r.add("config/trusted-proxies", true, validateCIDRs(cfg.TrustedProxyCIDRs), cfg.TrustedProxyCIDRs)
	checkBlobConfig(&r, cfg)
	if err := ctx.Err(); err == nil {
		checkSubstrate(&r, cfg, false)
		checkSubstrate(&r, cfg, true)
		checkEVM(ctx, &r, cfg)
		facts, factsErr := ReadSetupFacts(ctx, cfg)
		detail := ""
		if facts != nil {
			detail = fmt.Sprintf("source=%s alpha_rao=%d burn_rao=%d finalized=%d", facts.AlphaSourceHotkey, facts.AlphaAvailableRao, facts.BurnRao, facts.FinalizedBlock)
		}
		r.add("wallet/finalized-alpha-source", true, factsErr, detail)
		if factsErr == nil {
			roles, roleErr := derivePublicRoles(cfg)
			var planErr error
			var plan *SetupPlan
			if roleErr == nil {
				plan, planErr = buildPlan(cfg, facts, roles, time.Unix(0, 0).UTC())
			} else {
				planErr = roleErr
			}
			planDetail := ""
			if plan != nil {
				planDetail = fmt.Sprintf("tao_rao=%d/%d alpha_rao=%d/%d gas_wei=%d/%d registrations=%d/%d", plan.MaximumSpend.TAORao, plan.Limits.TAORao, plan.MaximumSpend.AlphaRao, plan.Limits.AlphaRao, plan.MaximumSpend.EVMGasWei, plan.Limits.EVMGasWei, plan.MaximumSpend.Registrations, plan.Limits.Registrations)
				if facts.WalletFreeTAORao < plan.MaximumSpend.TAORao {
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

func validateGovernanceSeparation(vault map[string]any) error {
	if err := expectString(vault["testnet-contract-governance"], "single-owner"); err != nil {
		return fmt.Errorf("testnet governance: %w", err)
	}
	if err := expectString(vault["contract_governance"], "safe-2-of-3"); err != nil {
		return fmt.Errorf("mainnet governance: %w", err)
	}
	return nil
}

func validateCIDRs(raw string) error {
	count := 0
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, err := netip.ParsePrefix(item); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q: %w", item, err)
		}
		count++
	}
	if count == 0 {
		return fmt.Errorf("trusted proxy CIDR set is empty")
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
		want := "github.com/urfoundation/sn/v2026"
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

func checkBlobConfig(r *DoctorReport, cfg *ResolvedConfig) {
	path := filepath.Join(cfg.Repos.Vault, "main", "minio.yml")
	b, err := os.ReadFile(path)
	if err == nil {
		lower := strings.ToLower(string(b))
		if !strings.Contains(lower, "bucket: blob") {
			err = fmt.Errorf("minio config does not select blob bucket")
		}
	}
	r.add("artifact-store/server-blob", true, err, path)
	if err == nil {
		conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort(cfg.ObjectStoreHost, "23900"), 5*time.Second)
		if dialErr == nil {
			_ = conn.Close()
		}
		r.add("artifact-store/minio-reachable", true, dialErr, net.JoinHostPort(cfg.ObjectStoreHost, "23900"))
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

func checkSubstrate(r *DoctorReport, cfg *ResolvedConfig, private bool) {
	name := "public"
	endpoint := cfg.Public.Chain.SubstratePublicReadEndpoint
	if private {
		name = "private"
		ws, _, err := authorityURLs(cfg.Authority)
		if err != nil {
			r.add("rpc/substrate-private", true, err, "")
			return
		}
		endpoint = ws
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
		metadata, metadataErr := chain.CheckMetadata()
		metadataDetail := ""
		if metadata != nil {
			metadataDetail = fmt.Sprintf("pallet_index=%d signed_extensions=%d", metadata.PalletIndex, len(metadata.Extensions))
			if metadataErr == nil && len(metadata.Problems) != 0 {
				metadataErr = fmt.Errorf("%s", strings.Join(metadata.Problems, "; "))
			}
		}
		r.add("runtime/metadata-"+name, true, metadataErr, metadataDetail)
		r.add("runtime/release-call-shapes-"+name, true, checkReleaseCallShapes(chain, cfg), "burned_register, decrease_take, transfer_stake_and_hotkey, commitments, and owner calls")
		gates, gateErr := verifyCompatibilityGates(chain, cfg)
		gateDetail := ""
		if gates != nil {
			if encoded, encodeErr := json.Marshal(gates); encodeErr == nil {
				gateDetail = string(encoded)
			}
		}
		r.add("runtime/compatibility-gates-"+name, true, gateErr, gateDetail)
		r.add("subnet/uid-capacity-"+name, true, checkUIDCapacity(chain, cfg), "all missing release identities fit the intended finalized UID cap")
	}
	if err == nil && cfg.Netuid != 0 && cfg.WalletPublic != "" {
		ownerErr, detail := verifySubnetOwner(chain, cfg.Netuid, cfg.WalletPublic)
		r.add("subnet/owner-"+name, true, ownerErr, detail)
	}
	if private {
		var logs any
		callErr := chain.API.Client.Call(&logs, "eth_getLogs", map[string]any{"fromBlock": "latest", "toBlock": "latest"})
		r.add("rpc/private-eth_getLogs", true, callErr, "method available")
	}
}

type releaseCallRequirement struct {
	Pallet string
	Call   string
	Names  []string
	Shapes []string
}

func checkReleaseCallShapes(chain *crv4.Chain, cfg *ResolvedConfig) error {
	requirements := []releaseCallRequirement{
		{crv4.PalletName, "burned_register", []string{"netuid", "hotkey"}, []string{"u16", "[u8;32]"}},
		{crv4.PalletName, "decrease_take", []string{"hotkey", "take"}, []string{"[u8;32]", "u16"}},
		{crv4.PalletName, "transfer_stake_and_hotkey", []string{"destination_coldkey", "origin_hotkey", "destination_hotkey", "origin_netuid", "destination_netuid", "alpha_amount"}, []string{"[u8;32]", "[u8;32]", "[u8;32]", "u16", "u16", "u64"}},
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
	for fleet := 1; fleet <= cfg.Config.Topology.HeadFleets; fleet++ {
		labels = append(labels, fleetHotkeyLabel(fleet))
	}
	for i := 1; i <= cfg.Config.Topology.Operators; i++ {
		labels = append(labels, fmt.Sprintf("operator-%d-pool-hotkey", i), fmt.Sprintf("operator-%d-deposit-hotkey", i))
	}
	for i := 1; i <= cfg.Config.Topology.Validators; i++ {
		labels = append(labels, validatorHotkeyLabel(i))
	}
	missing := uint64(0)
	for _, label := range labels {
		hotkey, decodeErr := decodeHex32(label, roles.Substrate[label].PublicKeyHex)
		if decodeErr != nil {
			return decodeErr
		}
		uidKey, keyErr := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Uids", netuidArg(cfg.Netuid), hotkey[:])
		if keyErr != nil {
			return keyErr
		}
		var uid types.U16
		present, readErr := chain.API.RPC.State.GetStorage(uidKey, &uid, finalized)
		if readErr != nil {
			return readErr
		}
		if !present {
			missing++
		}
	}
	needed := uint64(current) + missing
	if needed > maximum {
		return fmt.Errorf("netuid %d has %d UIDs and needs %d missing release registrations, exceeding intended maximum %d", cfg.Netuid, current, missing, maximum)
	}
	return nil
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
	privateWS, privateHTTP, err := authorityURLs(cfg.Authority)
	if err != nil {
		return err
	}
	host := func(raw string) (string, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("invalid RPC endpoint %q", raw)
		}
		return strings.ToLower(u.Host), nil
	}
	privateSubstrate, err := host(privateWS)
	if err != nil {
		return err
	}
	publicSubstrate, err := host(cfg.Public.Chain.SubstratePublicReadEndpoint)
	if err != nil {
		return err
	}
	privateEVM, err := host(privateHTTP)
	if err != nil {
		return err
	}
	publicEVM, err := host(cfg.Public.Chain.EVMPublicReadEndpoint)
	if err != nil {
		return err
	}
	if privateSubstrate == publicSubstrate || privateEVM == publicEVM {
		return errors.New("public postcondition RPC must not resolve to the private authority")
	}
	return nil
}

func checkEVM(parent context.Context, r *DoctorReport, cfg *ResolvedConfig) {
	_, httpURL, err := authorityURLs(cfg.Authority)
	if err != nil {
		r.add("rpc/evm-private", true, err, "")
		return
	}
	checkEVMEndpoint(parent, r, cfg, "private", httpURL)
	checkEVMEndpoint(parent, r, cfg, "public", cfg.Public.Chain.EVMPublicReadEndpoint)
}

func checkEVMEndpoint(parent context.Context, r *DoctorReport, cfg *ResolvedConfig, name, httpURL string) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	client, err := ethclient.DialContext(ctx, httpURL)
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
	if name != "private" {
		suffix = "-" + name
	}
	r.add("runtime/evm-finality"+suffix, true, headErr, fmt.Sprintf("number=%d hash=%s", head.Number, head.Hash))
	if headErr != nil {
		return
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
