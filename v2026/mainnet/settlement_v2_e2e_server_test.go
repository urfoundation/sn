//go:build linux || darwin

// The operator server on the end-to-end chain, for E-1 and E-3
// (VERSION2-FINAL.md §8, §15.6). The server's real settlement v2 driver,
// ledger builder, event mirror, ledger endpoints and earnings readers run
// against the fixture's contracts over the chain's JSON-RPC, on the database
// of the server's disposable test environment
// (server/local/release-gate-services.sh). Providers are rows of that
// database: settled contracts in each epoch's earning window, and each
// provider's own wallet consent, signed by its sr25519 coldkey and approved
// prospectively by the root key. The validator approves only what
// ledger.Verify passes over the objects the server's endpoint serves, and
// sends its approval with its own sender.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urfoundation/sn/v2026/validator"

	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/controller"
	"github.com/urnetwork/server/v2026/model"
	"github.com/urnetwork/server/v2026/session"
)

const (
	e2eDeploymentID = "urnetwork-e2e"
	// e2eReadiness stands for the launch readiness receipt's digest.
	e2eReadiness = "5ead1e55000000000000000000000000000000000000000000000000000000e2"
	// e2eGasLimit is the operator gas policy's maximum_gas re-signed for v2
	// (§13): a capturing close needs a limit of about 2.63M, signed at about
	// 3.2M with the engine's margin.
	e2eGasLimit = 3_500_000
)

// e2eGasApprover is the operator gas policy's independent approver.
var e2eGasApprover = ed25519.NewKeyFromSeed(crypto.Keccak256([]byte("urnetwork-e2e-gas-approver")))

// e2eServerRPC is the one JSON-RPC endpoint the server's chain client dials,
// for the life of the test binary. The server builds that client once per
// process from the first configuration it reads, so every test's server reads
// one configuration, and the endpoint forwards to the current test's chain.
// Every fixture deploys the same transactions, so the contracts' addresses
// agree. The server also caches a registered pool's first epoch for the
// process, so every test's pool registers from e2eDefaultFirst.
var e2eServerRPC struct {
	once   sync.Once
	lock   sync.Mutex
	chain  *e2eChain
	server *httptest.Server
	config *controller.StConfig
}

func e2eServerRPCURL() string {
	e2eServerRPC.once.Do(func() {
		e2eServerRPC.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			e2eServerRPC.lock.Lock()
			chain := e2eServerRPC.chain
			e2eServerRPC.lock.Unlock()
			if chain == nil {
				http.Error(writer, "no chain", http.StatusServiceUnavailable)
				return
			}
			chain.ServeHTTP(writer, request)
		}))
	})
	return e2eServerRPC.server.URL
}

// e2eServerEnv runs a test in the server's disposable test environment once:
// the server keeps process-wide driver state (alerts raised, registrations
// and windows read), so a rerun would not be independent.
func e2eServerEnv(t *testing.T, test func(t testing.TB)) {
	(&server.TestEnv{ApplyDbMigrations: true}).Run(t, test)
}

// e2eRowsOf is an epoch's provider rows as the server builds them from
// usage: one row per provider with its own consent's coldkey.
func e2eRowsOf(usage map[*e2eProvider]uint64) []protocol.LedgerUsageRow {
	rows := []protocol.LedgerUsageRow{}
	for p, bytes := range usage {
		rows = append(rows, protocol.LedgerUsageRow{ClientID: [16]byte(p.client), Coldkey: p.coldkey, UsageBytes: bytes})
	}
	return rows
}

// e2eServer is the operator server, and the validator, over one fixture.
type e2eServer struct {
	t      testing.TB
	ctx    context.Context
	f      *e2eFixture
	cfg    *controller.StConfig
	key    model.StDeploymentKey
	api    *httptest.Server
	emails *e2eEmails
	// The validator's audit custody, its sender and its chain client.
	custody *ledger.MemoryCustody
	sender  *validator.SettlementV2Sender
	chain   *validator.ChainClient
	// domain is the operator's client-key domain, which consents name.
	domain protocol.ClientKeyHistoryDomain
}

// newE2EServer configures the server for f's deployment: st.yml's identity
// with the v2 addresses, sn.yml's reviewed earnings schedule, the re-signed
// operator gas policy, the ledger object store and the real builder, the
// ledger endpoints, and the evidence journal the deployment anchors. It must
// run inside the server's test environment.
func newE2EServer(t testing.TB, f *e2eFixture) *e2eServer {
	t.Helper()
	ctx := context.Background()
	url := e2eServerRPCURL()
	e2eServerRPC.lock.Lock()
	if e2eServerRPC.config == nil {
		e2eServerRPC.config = &controller.StConfig{Profile: "mainnet", Enabled: true, RpcUrls: []string{url}, ChainId: e2eChainID, GenesisHash: e2eGenesisHash,
			DeploymentId: e2eDeploymentID, PolicyHash: f.policy(e2eEpochBlocks, e2eWindow).PolicyHash, LaunchReadinessSha256: e2eReadiness,
			ContractAddress: f.coordinator, SettlementVault: f.vault1, ReserveSink: f.sink, SettlementV2: f.settlement, VaultV2: f.vault2,
			Netuid: e2eNetuid, NoId: e2eNoID, DepositHotkey: f.deposit, DepositKey: f.depositSigner.key, RootKey: f.operator.key, OpsKey: f.operator.key,
			BlockSeconds: e2eBlockSeconds, DeployBlock: 1, DepositZeroRateAction: "equal_demand"}
	}
	cfg := e2eServerRPC.config
	e2eServerRPC.chain = f.chain
	e2eServerRPC.lock.Unlock()
	t.Cleanup(func() {
		e2eServerRPC.lock.Lock()
		e2eServerRPC.chain = nil
		e2eServerRPC.lock.Unlock()
	})
	if cfg.ContractAddress != f.coordinator || cfg.SettlementVault != f.vault1 || cfg.SettlementV2 != f.settlement || cfg.VaultV2 != f.vault2 || f.firstEpoch != e2eDefaultFirst {
		t.Fatal("the fixture differs from the deployment the server's configuration names")
	}
	s := &e2eServer{t: t, ctx: ctx, f: f, cfg: cfg, key: cfg.DeploymentKey(), custody: ledger.NewMemoryCustody(),
		domain: protocol.ClientKeyHistoryDomain{ChainID: cfg.ChainId, GenesisHash: cfg.GenesisHash, Netuid: uint16(cfg.Netuid), Coordinator: cfg.ContractAddress,
			SettlementVault: cfg.SettlementVault, DeploymentIDHash: sha256.Sum256([]byte(cfg.DeploymentId)), PolicyHash: cfg.PolicyHash, NoID: cfg.NoId}}

	// The launch anchored the v1 evidence journal, which names the
	// deployment's identity that payout epoch authority reads.
	evidence := f.deploy(f.deployer, e2eCatalogArtifact(t, "ValidatorEvidence"), nil, f.coordinator, e2eGenesisHash, s.domain.DeploymentIDHash)
	f.requireLogs(f.must(f.owner, f.coordinator, nil, e2eCoordinatorBinding.PackFixValidatorEvidence(evidence)), f.coordinatorLog("ValidatorEvidenceFixed", evidence))

	controller.SetStConfig(cfg)
	controller.SetStLedgerV2ObjectStore(server.NewLocalBlobStore(t.TempDir(), "e2e"))
	if !controller.InstallStLedgerBuilder() {
		t.Fatal("the server installed no ledger builder")
	}
	t.Cleanup(func() {
		controller.SetStLedgerBuilder(nil)
		controller.SetStLedgerV2ObjectStore(nil)
		controller.SetStConfig(nil)
	})
	s.schedule()
	s.gasPolicy()
	s.emails = &e2eEmails{}
	previous := controller.GetAWSMessageSender()
	controller.SetMessageSender(s.emails)
	t.Cleanup(func() { controller.SetMessageSender(previous) })

	mux := http.NewServeMux()
	mux.HandleFunc("/sn/ledger/object", controller.SnLedgerObject)
	mux.HandleFunc("/sn/ledger/account", controller.SnLedgerAccount)
	s.api = httptest.NewServer(mux)
	t.Cleanup(s.api.Close)
	// The validator reads the chain at its own endpoint.
	chain, err := validator.DialReleaseChainContext(ctx, []string{f.chain.server.URL}, f.coordinator)
	if err != nil {
		t.Fatal(err)
	}
	s.chain = chain
	s.sender, err = validator.NewSettlementV2Sender(chain, []string{f.chain.server.URL}, e2eChainID, validator.SettlementV2Addresses{Settlement: f.settlement, Vault: f.vault2}, f.auditor.keyFile(t), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// schedule pushes sn.yml's reviewed earnings schedule for the deployment,
// whose cutoff precedes every epoch, and prepares its database boundary.
func (s *e2eServer) schedule() {
	cfg := s.cfg
	data := fmt.Sprintf("schema: urnetwork-provider-payout-transition-v1\ncutoff_utc: %q\nattribution: settled_contract_close_time\nlegacy_usdc: finish_pre_cutoff_obligations\n"+
		"mainnet:\n  profile: mainnet\n  chain_id: %d\n  genesis_hash: %q\n  netuid: %d\n  activation: reviewed\n  deployment_id: %q\n  coordinator: %q\n  settlement_vault: %q\n"+
		"  policy_hash: %q\n  readiness_sha256: %q\n  settlement_v2_address: %q\n  vault_v2_address: %q\n",
		time.Unix(e2eGenesisTime, 0).UTC().Format(time.RFC3339Nano), cfg.ChainId, fmt.Sprintf("0x%x", cfg.GenesisHash), cfg.Netuid, cfg.DeploymentId,
		strings.ToLower(cfg.ContractAddress.Hex()), strings.ToLower(cfg.SettlementVault.Hex()), fmt.Sprintf("0x%x", cfg.PolicyHash), e2eReadiness,
		strings.ToLower(cfg.SettlementV2.Hex()), strings.ToLower(cfg.VaultV2.Hex()))
	s.t.Cleanup(server.Config.PushSimpleResource("sn.yml", []byte(data)))
	policy, err := server.LoadProviderPayoutTransition(s.ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := server.PrepareProviderPayoutBoundary(s.ctx, policy.ConfigSha256); err != nil {
		s.t.Fatal(err)
	}
}

// gasPolicy signs the operator gas policy for the root and deposit accounts
// once, as part of the one configuration, pinned to the empty signature
// history every fresh database gives them, and publishes its independent
// authority beside the configuration.
func (s *e2eServer) gasPolicy() {
	cfg := s.cfg
	if cfg.OperatorGasPolicy == nil {
		policy := &server.StOperatorGasPolicy{Schema: server.StOperatorGasPolicySchema, Profile: cfg.Profile, ChainId: cfg.ChainId, GenesisHash: fmt.Sprintf("0x%x", cfg.GenesisHash),
			NoId: cfg.NoId, Coordinator: strings.ToLower(cfg.ContractAddress.Hex()), PolicyHash: fmt.Sprintf("0x%x", cfg.PolicyHash), ValidFrom: 1, ValidUntil: 1 << 40,
			MaximumGas: e2eGasLimit, MaximumFeePerGasWei: "30000000000", MaximumTipPerGasWei: "30000000000", MaximumIntentLiabilityWei: "105000000000000000",
			MaximumLifetimeLiabilityWei: "105000000000000000000", MaximumIntentAttempts: 3, MaximumLifetimeAttempts: 1000}
		for _, account := range []struct {
			role string
			key  *ecdsa.PrivateKey
		}{{"deposit", cfg.DepositKey}, {"root", cfg.RootKey}} {
			address := strings.ToLower(crypto.PubkeyToAddress(account.key.PublicKey).Hex())
			history, err := model.StOperatorGasAccountHistorySha256(s.ctx, policy.ChainId, policy.GenesisHash, address, policy.MaximumLifetimeAttempts)
			if err != nil {
				s.t.Fatal(err)
			}
			policy.Accounts = append(policy.Accounts, server.StOperatorGasPolicyAccount{Role: account.role, Address: address, InitialHistorySha256: history})
		}
		signing, err := policy.SigningBytes()
		if err != nil {
			s.t.Fatal(err)
		}
		policy.Signature = hex.EncodeToString(ed25519.Sign(e2eGasApprover, signing))
		cfg.OperatorGasPolicy = policy
	}
	digest, err := cfg.OperatorGasPolicy.Digest()
	if err != nil {
		s.t.Fatal(err)
	}
	authority := fmt.Sprintf("schema: %s\nprofile: %s\nchain_id: %d\ngenesis_hash: %q\nno_id: %d\napprover_public_key: %q\npolicy_sha256: %q\n", server.StOperatorGasAuthoritySchema,
		cfg.Profile, cfg.ChainId, cfg.OperatorGasPolicy.GenesisHash, cfg.NoId, hex.EncodeToString(e2eGasApprover.Public().(ed25519.PublicKey)), digest)
	s.t.Cleanup(server.Config.PushSimpleResource("operator-gas-authority.yml", []byte(authority)))
}

// e2eStep is one planned driver step and how it ended.
type e2eStep struct {
	step    *controller.StLedgerV2Step
	outcome *controller.StPublishOutcome
}

// mirror runs the first half of a sync turn of the server's st worker
// (taskworker/work/st_work.go): the epoch state, the event mirror up to the
// finalized head, and the previous epoch's row. The readers and the v2
// driver's plan read the mirror.
func (s *e2eServer) mirror() *controller.StEpochState {
	s.t.Helper()
	state, err := controller.StSyncChainState(s.ctx)
	if err != nil {
		s.t.Fatalf("st sync chain state: %v", err)
	}
	for model.GetStChainCheckpoint(s.ctx, s.key).NextBlock <= state.HeadBlock {
		if _, err := controller.StSyncChainEvents(s.ctx, state.HeadBlock); err != nil {
			s.t.Fatalf("st sync chain events: %v", err)
		}
	}
	if 0 < state.Epoch {
		if _, err := controller.StBackfillEpochRow(s.ctx, state, state.Epoch-1); err != nil {
			s.t.Fatalf("st backfill epoch %d: %v", state.Epoch-1, err)
		}
	}
	return state
}

// turn runs one sync turn for settlement v2: the mirror, then the v2
// driver's plan, whose every step runs now. It returns the steps in plan
// order with their outcomes.
func (s *e2eServer) turn() []e2eStep {
	s.t.Helper()
	steps, err := controller.StLedgerV2Sync(s.ctx, s.mirror())
	if err != nil {
		s.t.Fatalf("st ledger v2 sync: %v", err)
	}
	ran := []e2eStep{}
	for _, step := range steps {
		outcome, err := controller.StLedgerV2RunStep(s.ctx, step)
		if err != nil {
			s.t.Fatalf("st ledger v2 %s step: %v", step.Kind, err)
		}
		ran = append(ran, e2eStep{step: step, outcome: outcome})
	}
	return ran
}

// e2eRan finds the step of a kind for an epoch among a turn's steps.
func e2eRan(t testing.TB, ran []e2eStep, kind string, epoch uint64) *controller.StPublishOutcome {
	t.Helper()
	for _, step := range ran {
		if step.step.Kind == kind && step.step.Epoch == epoch {
			return step.outcome
		}
	}
	t.Fatalf("the turn ran no %s step for epoch %d", kind, epoch)
	return nil
}

// requireOutcome checks a step's outcome by status and reason prefix.
func requireOutcome(t testing.TB, outcome *controller.StPublishOutcome, status string, reason string) {
	t.Helper()
	if outcome.Status != status || !strings.HasPrefix(outcome.Reason, reason) {
		t.Fatalf("step ended %s (%s), want %s (%s)", outcome.Status, outcome.Reason, status, reason)
	}
}

// e2eServerObjects is the validator's fetch of ledger objects from the
// server's public endpoint.
type e2eServerObjects struct{ url string }

func (self e2eServerObjects) Fetch(ctx context.Context, ref ledger.Ref) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, self.url+"/sn/ledger/object?hash="+ref.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	switch response.StatusCode {
	case http.StatusOK:
		return response.Body, nil
	case http.StatusNotFound:
		response.Body.Close()
		return nil, fmt.Errorf("%w: %v", ledger.ErrObjectNotFound, ref)
	}
	response.Body.Close()
	return nil, fmt.Errorf("ledger object %v: HTTP %d", ref, response.StatusCode)
}

// audit runs the validator's ledger audit (§9.2, without head bindings) of
// the pending root over the objects the server's endpoint serves.
func (s *e2eServer) audit() *ledger.Result {
	s.t.Helper()
	f := s.f
	pending := f.pending()
	in := ledger.VerifyInput{NoID: e2eNoID, FirstEpoch: f.firstEpoch, Seq: pending.Seq, FundedThrough: map[uint64]uint256.Int{},
		Pending: e2eLedgerRoot(pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash),
		Fetcher: e2eServerObjects{url: s.api.URL}, Custody: s.custody, MaxFetchBytes: ledger.DefaultMaxFetchBytes}
	p := f.firstEpoch - 1
	if count := f.rootCount(); count != 0 {
		root := f.root(count - 1)
		previous := e2eLedgerRoot(root.Epoch, root.Root, root.Total, root.LeafCount, root.LedgerHash)
		in.Previous, p = &previous, root.Epoch
	}
	for k := p + 1; k <= pending.Epoch; k++ {
		in.FundedThrough[k] = *uint256.MustFromBig(f.fundedThrough(k))
	}
	result, err := ledger.Verify(s.ctx, in)
	if err != nil {
		s.t.Fatal(err)
	}
	return result
}

// approve audits the pending root and, when the audit approves it, sends the
// validator's approval through its sender.
func (s *e2eServer) approve() {
	s.t.Helper()
	if result := s.audit(); result.Action() != ledger.ActionApprove {
		s.t.Fatalf("the validator refuses the pending root: %v (%v check: %v)", result.Action(), result.Check, result.Err)
	}
	pending := s.f.pending()
	if err := s.sender.Approve(s.ctx, e2eNoID, pending); err != nil {
		s.t.Fatal(err)
	}
	sent := s.f.chain.sentBy(s.f.auditor.address)
	s.f.requireLogs(s.f.chain.receipt(sent[len(sent)-1].Hash()), s.f.settlementLog("DistributionApproved", e2eRao(e2eNoID), pending.Seq, s.f.auditor.address, uint64(1), common.Big1))
}

// e2eProvider is a provider in the server's database: one client in its own
// network, whose own consent selects its sr25519 coldkey.
type e2eProvider struct {
	name     string
	client   server.Id
	network  server.Id
	userAuth string
	coldkey  [32]byte
}

func e2eServerId(label string) server.Id {
	digest := sha256.Sum256([]byte("urnetwork-e2e-id:" + label))
	return server.Id(digest[:16])
}

// provider creates a provider's network and its consent chain's first
// generation, effective from epoch from: approved prospectively by the root
// key in the epoch before, expired by the start of epoch from.
func (s *e2eServer) provider(name string, from uint64) *e2eProvider {
	s.t.Helper()
	mini, err := schnorrkel.NewMiniSecretKeyFromRaw(sha256.Sum256([]byte("urnetwork-e2e-sr25519:" + name)))
	if err != nil {
		s.t.Fatal(err)
	}
	secret := mini.ExpandEd25519()
	public, err := secret.Public()
	if err != nil {
		s.t.Fatal(err)
	}
	p := &e2eProvider{name: name, client: e2eServerId("client:" + name), network: e2eServerId("network:" + name), coldkey: public.Encode()}
	p.userAuth = model.Testing_CreateNetwork(s.ctx, p.network, "e2e-"+name, e2eServerId("user:"+name))

	start := s.f.end(from - 1)
	expires := int64(e2eBlockTime(start))
	statement := protocol.WalletMappingStatement{Schema: protocol.WalletMappingConsentSchema, Domain: s.domain, UserId: [16]byte(e2eServerId("user:" + name)),
		ClientId: [16]byte(p.client), NetworkId: [16]byte(p.network), Coldkey: p.coldkey, Generation: 1, Nonce: sha256.Sum256([]byte("urnetwork-e2e-consent:" + name)),
		IssuedAt: expires - 120, ExpiresAt: expires, FromEpoch: from, ThroughEpoch: from + 1000}
	boundary := protocol.ClientKeyEffectiveBoundary{Epoch: from - 1, Block: start - 1, Hash: e2eBlockHash(start - 1)}
	if err := protocol.SignProspectiveWalletMapping(&statement, boundary, s.cfg.RootKey); err != nil {
		s.t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		s.t.Fatal(err)
	}
	signature, err := secret.Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		s.t.Fatal(err)
	}
	original, err := json.Marshal(protocol.WalletMappingConsent{Message: message, Signature: signature.Encode()})
	if err != nil {
		s.t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	domainHash, err := s.domain.Digest()
	if err != nil {
		s.t.Fatal(err)
	}
	server.Tx(s.ctx, func(tx server.PgTx) {
		server.RaisePgResult(tx.Exec(s.ctx, `INSERT INTO wallet_mapping_challenge(nonce,domain_hash,client_id,generation,expires_at,message) VALUES($1,$2,$3,1,$4,$5)`,
			statement.Nonce[:], domainHash[:], p.client, expires, message))
		server.RaisePgResult(tx.Exec(s.ctx, `INSERT INTO wallet_mapping_consent(domain_hash,client_id,generation,original_hash,nonce,original,accepted_at) VALUES($1,$2,1,$3,$4,$5,$6)`,
			domainHash[:], p.client, hash[:], statement.Nonce[:], original, time.Unix(expires, 0).UTC().Add(-time.Minute)))
	})
	return p
}

// settle retains, for each provider, one contract settled an hour into
// epoch's earning window with the provider's bytes.
func (s *e2eServer) settle(epoch uint64, usage map[*e2eProvider]uint64) {
	s.t.Helper()
	closed := time.Unix(int64(e2eBlockTime(s.f.end(epoch-1))), 0).UTC().Add(time.Hour)
	for p, bytes := range usage {
		providers := fmt.Sprintf(`{"version":1,"byte_count":%d,"providers":[{"client_id":%q,"network_id":%q,"byte_count":%d}]}`, bytes, p.client.String(), p.network.String(), bytes)
		server.Tx(s.ctx, func(tx server.PgTx) {
			server.RaisePgResult(tx.Exec(s.ctx, `INSERT INTO transfer_contract(contract_id,source_network_id,source_id,destination_network_id,destination_id,transfer_byte_count,create_time,close_time,outcome,provider_usage,usage_origin_is_source)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,'settled',$9,true)`, server.NewId(), server.NewId(), server.NewId(), server.NewId(), server.NewId(), int64(bytes), closed.Add(-time.Hour), closed, providers))
		})
	}
}

// ledgerAccount is GET /sn/ledger/account for the coldkey, as its ss58
// address.
func (s *e2eServer) ledgerAccount(coldkey [32]byte) *controller.StLedgerV2AccountsResult {
	s.t.Helper()
	address, err := ss58.Encode(coldkey, ss58.BittensorPrefix)
	if err != nil {
		s.t.Fatal(err)
	}
	response, err := http.Get(s.api.URL + "/sn/ledger/account?coldkey=" + address)
	if err != nil {
		s.t.Fatal(err)
	}
	defer response.Body.Close()
	var result controller.StLedgerV2AccountsResult
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&result) != nil {
		s.t.Fatalf("GET /sn/ledger/account: HTTP %d", response.StatusCode)
	}
	return &result
}

// requireAccount checks the endpoint's one entry for the coldkey with no
// pending root: its finalized cumulative with a proof that verifies against
// vault2's root, and claimed as vault2 reads it.
func (s *e2eServer) requireAccount(coldkey [32]byte, rootIndex uint64, index uint64, cumulative uint64, claimed uint64) {
	s.t.Helper()
	result := s.ledgerAccount(coldkey)
	if result.Coldkey != fmt.Sprintf("0x%x", coldkey) || len(result.Accounts) != 1 {
		s.t.Fatalf("GET /sn/ledger/account for 0x%x: %+v", coldkey, result)
	}
	entry := result.Accounts[0]
	if entry.Vault != strings.ToLower(s.f.vault2.Hex()) || entry.NoId != e2eNoID || entry.Index != index || entry.RootIndex == nil || *entry.RootIndex != rootIndex ||
		entry.Cumulative != fmt.Sprintf("%d", cumulative) || entry.Claimed != fmt.Sprintf("%d", claimed) || entry.PendingCumulative != nil {
		s.t.Fatalf("GET /sn/ledger/account for 0x%x: %+v", coldkey, entry)
	}
	root := s.f.root(rootIndex)
	proof := make([]merkle.LedgerNode, len(entry.Proof))
	for k, node := range entry.Proof {
		sum, err := uint256.FromDecimal(node.Sum)
		if err != nil {
			s.t.Fatal(err)
		}
		proof[k] = merkle.LedgerNode{Hash: common.HexToHash(node.Hash), Sum: *sum}
	}
	if !merkle.VerifyLedgerProof(root.Root, uint256.MustFromBig(root.Total), root.LeafCount, index, coldkey, uint256.NewInt(cumulative), proof) {
		s.t.Fatalf("the endpoint's proof for 0x%x does not verify against root %d", coldkey, rootIndex)
	}
}

// accountEpochs is GET /account/epochs for the provider's network.
func (s *e2eServer) accountEpochs(p *e2eProvider) []controller.AccountEpoch {
	s.t.Helper()
	clientSession := session.Testing_CreateClientSession(s.ctx, &session.ByJwt{NetworkId: p.network})
	defer clientSession.Cancel()
	result, err := controller.AccountEpochs(&controller.AccountEpochsArgs{}, clientSession)
	if err != nil || result.Error != nil {
		s.t.Fatalf("GET /account/epochs: %v %+v", err, result)
	}
	return result.Epochs
}

// e2eEmails records each earnings email as "epoch <e> <user> share <bps>
// alpha <rao>".
type e2eEmails struct {
	lock    sync.Mutex
	sent    []string
	checked int
}

func (self *e2eEmails) SendAccountMessageTemplate(userAuth string, template controller.Template, sendOpts ...any) error {
	earnings, ok := template.(*controller.EpochEarningsTemplate)
	if !ok {
		return errors.New("not an earnings email")
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	self.sent = append(self.sent, fmt.Sprintf("epoch %d %s share %d alpha %s", earnings.Epoch, userAuth, earnings.ShareBps, earnings.UnclaimedRao))
	return nil
}

// require checks the next emails the server sends, in any order; it sends
// them in the background.
func (self *e2eEmails) require(t testing.TB, want ...string) {
	t.Helper()
	want = slices.Clone(want)
	slices.Sort(want)
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(20 * time.Millisecond) {
		self.lock.Lock()
		next := slices.Clone(self.sent[self.checked:])
		self.lock.Unlock()
		if len(want) <= len(next) || deadline.Before(time.Now()) {
			if slices.Sort(next); !slices.Equal(next, want) {
				t.Fatalf("earnings emails %q, want %q", next, want)
			}
			self.lock.Lock()
			self.checked += len(next)
			self.lock.Unlock()
			return
		}
	}
}

// e2eMailed is the earnings emails an epoch's v2 amounts send: to each
// provider's network with an amount, its share of the epoch's amounts in
// basis points and its amount.
func e2eMailed(epoch uint64, amounts map[*e2eProvider]uint64) []string {
	var total uint64
	for _, amount := range amounts {
		total += amount
	}
	mailed := []string{}
	for p, amount := range amounts {
		mailed = append(mailed, fmt.Sprintf("epoch %d %s share %d alpha %d", epoch, p.userAuth, amount*10_000/total, amount))
	}
	return mailed
}

// v1Leaves records an epoch's v1 payout leaves as v1's close writes them,
// and returns their tree, whose root the operator commits.
func (s *e2eServer) v1Leaves(epoch uint64, shares map[*e2eProvider]int) *merkle.Tree {
	s.t.Helper()
	providers := []*e2eProvider{}
	for p := range shares {
		providers = append(providers, p)
	}
	slices.SortFunc(providers, func(a, b *e2eProvider) int { return strings.Compare(string(a.client[:]), string(b.client[:])) })
	leaves := []*model.StPayoutLeaf{}
	merkleLeaves := []merkle.Leaf{}
	for i, p := range providers {
		client := p.client
		leaves = append(leaves, &model.StPayoutLeaf{Epoch: epoch, NoId: e2eNoID, ClientId: &client, NetworkId: p.network, Coldkey: p.coldkey, ShareBps: shares[p], LeafIndex: i})
		merkleLeaves = append(merkleLeaves, merkle.PayoutLeaf(p.coldkey, big.NewInt(int64(shares[p]))))
	}
	model.SetStPayoutLeaves(s.ctx, s.key, epoch, e2eNoID, leaves)
	tree, err := merkle.NewTree(merkleLeaves)
	if err != nil {
		s.t.Fatal(err)
	}
	return tree
}

// mark counts the transactions the server's root key has sent so far.
func (s *e2eServer) mark() int {
	return len(s.f.chain.sentBy(s.f.operator.address))
}

// receipts returns the receipts of the transactions the server's root key
// sent since mark, in order.
func (s *e2eServer) receipts(mark int) []*types.Receipt {
	s.t.Helper()
	sent := s.f.chain.sentBy(s.f.operator.address)[mark:]
	receipts := make([]*types.Receipt, len(sent))
	for i, tx := range sent {
		if receipts[i] = s.f.chain.receipt(tx.Hash()); receipts[i] == nil {
			s.t.Fatalf("the server's transaction %s was not mined", tx.Hash())
		}
	}
	return receipts
}

// commit runs the driver's turn at end(epoch), which closes the epoch and
// commits its root: the close defers each epoch of deferred, with the
// funding captured so far, and captures the pool hotkey's movable stake; the
// commit names the pending root and the root it replaces. It returns the
// pending root and the turn's steps.
func (s *e2eServer) commit(epoch uint64, deferred ...uint64) (stabi.ISTSettlementVault2Pending, []e2eStep) {
	s.t.Helper()
	f := s.f
	// The driver plans the epoch once the finalized head reaches its end.
	if end := f.end(epoch); f.chain.next() <= end {
		f.chain.rollTo(end + 1)
	}
	observed := f.chain.stake(f.poolV2, f.vault2Coldkey)
	captured := e2eView(f, f.vault2, e2eVault2Binding.PackCapturedTotal(e2eRao(e2eNoID)), e2eVault2Binding.UnpackCapturedTotal)
	replaced, mark := f.pending(), s.mark()
	ran := s.turn()
	requireOutcome(s.t, e2eRan(s.t, ran, controller.StLedgerV2StepEpoch, epoch), model.StPublishStatusConfirmed, "")
	receipts := s.receipts(mark)
	if len(receipts) != 2 {
		s.t.Fatalf("the driver sent %d transactions for epoch %d", len(receipts), epoch)
	}
	var logs []e2eLog
	for _, k := range deferred {
		logs = append(logs, f.vault2Log("EmissionDeferred", e2eRao(k), e2eRao(e2eNoID), captured))
	}
	f.requireLogs(receipts[0], append(logs, f.vault2Log("EmissionCaptured", e2eRao(epoch), e2eRao(e2eNoID), observed, observed, new(big.Int).Add(captured, observed)))...)
	pending := f.pending()
	f.requireLogs(receipts[1], f.vault2Log("DistributionCommitted", e2eRao(e2eNoID), pending.Seq, epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash, replaced.Seq))
	if pending.Epoch != epoch || pending.Total.Cmp(f.fundedThrough(epoch)) != 0 {
		s.t.Fatalf("the driver committed %+v for epoch %d", pending, epoch)
	}
	return pending, ran
}

// finalize runs the driver's turn once the pending root's window has
// passed, which finalizes it, and the next, which records it for the
// readers. It returns the root's index.
func (s *e2eServer) finalize() uint64 {
	s.t.Helper()
	f := s.f
	pending := f.pending()
	// The driver finalizes once the finalized head reaches the window's end.
	if at := pending.CommittedBlock + e2eWindow; f.chain.next() <= at {
		f.chain.rollTo(at + 1)
	}
	index, mark := f.rootCount(), s.mark()
	requireOutcome(s.t, e2eRan(s.t, s.turn(), controller.StLedgerV2StepFinalize, pending.Epoch), model.StPublishStatusConfirmed, "")
	receipts := s.receipts(mark)
	if len(receipts) != 1 {
		s.t.Fatalf("the driver sent %d transactions to finalize", len(receipts))
	}
	f.requireLogs(receipts[0], f.vault2Log("DistributionFinalized", e2eRao(e2eNoID), pending.Seq, e2eRao(index), pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash))
	requireOutcome(s.t, e2eRan(s.t, s.turn(), controller.StLedgerV2StepRecord, pending.Epoch), model.StPublishStatusConfirmed, fmt.Sprintf("root %d recorded", index))
	return index
}

// epoch runs one v2 epoch through the server and the validator: the
// providers' usage settles and the pool earns; at the epoch's end the driver
// closes it and commits its root; the validator approves; the driver
// finalizes and records it. It returns the root's index.
func (s *e2eServer) epoch(epoch uint64, emission uint64, usage map[*e2eProvider]uint64) uint64 {
	s.t.Helper()
	s.settle(epoch, usage)
	s.f.emitV2(emission)
	s.commit(epoch)
	s.approve()
	return s.finalize()
}

// header reads a root's header from the server's endpoint.
func (s *e2eServer) header(ledgerHash [32]byte) *ledger.Header {
	s.t.Helper()
	body, err := e2eServerObjects{url: s.api.URL}.Fetch(s.ctx, ledger.Ref(ledgerHash))
	if err != nil {
		s.t.Fatal(err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		s.t.Fatal(err)
	}
	header, err := ledger.DecodeHeader(data)
	if err != nil {
		s.t.Fatal(err)
	}
	return header
}

// covers checks the epochs a root's header covers.
func (s *e2eServer) covers(ledgerHash [32]byte, epochs ...uint64) *ledger.Header {
	s.t.Helper()
	header := s.header(ledgerHash)
	covered := []uint64{}
	for _, epoch := range header.Epochs {
		covered = append(covered, epoch.Epoch)
	}
	if !slices.Equal(covered, epochs) {
		s.t.Fatalf("the root covers epochs %v, want %v", covered, epochs)
	}
	return header
}
