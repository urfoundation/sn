//go:build runtime_rehearsal

package rehearsal

import (
	"math/big"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/crv4"
)

const (
	// challengeBlocks is vault2's CHALLENGE_BLOCKS.
	challengeBlocks = 14_400
	// window is every policy's rootCommitWindowBlocks: epoch e closes and
	// commits in blocks [end(e), end(e) + window].
	window = 10
	// cadence is the shortest epoch STSettlement2 accepts for a pool's first
	// epoch (§7.2): window + CHALLENGE_BLOCKS.
	cadence = window + challengeBlocks
	// setupEpochBlocks is epoch 0, which holds the deployment.
	setupEpochBlocks = 300
	// shortEpochBlocks is the epoch length from stackConfig.shortFrom.
	shortEpochBlocks = 100
	// maximumBurn is every registration's ceiling, the runtime's default MaxBurn.
	maximumBurn = 100 * tao
)

// demandFeeWei is vault2's demand fee: 0.001 TAO, a whole number of rao (SPEC A5).
var demandFeeWei = new(big.Int).Mul(big.NewInt(1_000_000), big.NewInt(tao))

// policySnapshot is STCoordinator.PolicySnapshot.
type policySnapshot struct {
	PolicyHash                   [32]byte
	EffectiveEpoch               uint64
	EffectiveBlock               uint64
	EpochBlocks                  uint64
	RootCommitWindowBlocks       uint64
	FinalizeOffsetBlocks         uint64
	CloseGraceBlocks             uint64
	ClaimTTLEpochs               uint64
	ClaimGraceEpochs             uint64
	MaximumBindingValidityEpochs uint64
	CommitmentMaxAgeBlocks       uint64
	EpochDepositCapRao           *big.Int
	CampaignDepositCapRao        *big.Int
}

// policy returns a v1 policy of epochBlocks per epoch from effectiveEpoch,
// with the rehearsal's commit window and the smallest values v1 accepts
// elsewhere.
func policy(effectiveEpoch, epochBlocks uint64) policySnapshot {
	return policySnapshot{
		PolicyHash:                   crypto.Keccak256Hash([]byte("urnetwork rehearsal policy")),
		EffectiveEpoch:               effectiveEpoch,
		EpochBlocks:                  epochBlocks,
		RootCommitWindowBlocks:       window,
		FinalizeOffsetBlocks:         2 * window,
		CloseGraceBlocks:             1,
		ClaimTTLEpochs:               1,
		ClaimGraceEpochs:             1,
		MaximumBindingValidityEpochs: 1,
		CommitmentMaxAgeBlocks:       1,
		EpochDepositCapRao:           big.NewInt(1),
		CampaignDepositCapRao:        big.NewInt(1),
	}
}

// stackConfig shapes a stack: v1 operators 1 … operators, and from epoch
// shortFrom (when not 0) short epochs, so that tests can close many epochs.
type stackConfig struct {
	operators int
	shortFrom uint64
}

// operator is one v1 operator and its v2 pool hotkey.
type operator struct {
	rootSigner evmKey
	v1Pool     [32]byte
	pool       [32]byte
}

// stack is one deployment of the settlement contracts, from the exact forge
// artifacts and deployed directly: the v1 reserve sink, vault and
// coordinator proxy, whose operators, windows and pause STSettlement2 reads,
// then vault2 and the STSettlement2 proxy (SPEC A7 order). Epoch 0 is short
// and holds the deployment; epoch 1, every pool's first epoch, is the
// shortest cadence STSettlement2 accepts.
type stack struct {
	n           *node
	netuid      uint16
	ownerHotkey *crv4.Keypair
	deployer    evmKey
	owner       evmKey
	auditor     evmKey
	operators   map[uint64]*operator

	v1, v1Vault, sink, vault, settlement common.Address
	vaultColdkey, escrow, v1Escrow       [32]byte

	coordinatorArtifact, v1VaultArtifact, vaultArtifact, settlementArtifact, collateralArtifact artifact
}

// raoWei converts rao to wei.
func raoWei(rao uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(rao), big.NewInt(tao))
}

// withArgs appends ABI-encoded constructor arguments to creation bytecode.
func withArgs(t *testing.T, contract artifact, args ...any) []byte {
	t.Helper()
	encoded, err := contract.abi.Pack("", args...)
	if err != nil {
		t.Fatalf("constructor arguments: %v", err)
	}
	return append(append([]byte{}, contract.creation...), encoded...)
}

func deployStack(t *testing.T, n *node, config stackConfig) *stack {
	t.Helper()
	s := &stack{n: n, deployer: newEVMKey(t), owner: newEVMKey(t), auditor: newEVMKey(t), operators: map[uint64]*operator{}}
	s.coordinatorArtifact = forgeArtifact(t, "src/STCoordinator.sol", "STCoordinator")
	s.v1VaultArtifact = forgeArtifact(t, "src/STSettlementVault.sol", "STSettlementVault")
	s.vaultArtifact = forgeArtifact(t, "src/STSettlementVault2.sol", "STSettlementVault2")
	s.settlementArtifact = forgeArtifact(t, "src/STSettlement2.sol", "STSettlement2")
	s.collateralArtifact = forgeArtifact(t, "src/interfaces/stakingCollateral.sol", "IStakingCollateral")
	sink := forgeArtifact(t, "src/STReserveSink.sol", "STReserveSink")
	proxy := forgeArtifact(t, "lib/openzeppelin-contracts/contracts/proxy/ERC1967/ERC1967Proxy.sol", "ERC1967Proxy")

	s.ownerHotkey = newColdkey(t)
	s.netuid = n.createSubnet(t, alice, s.ownerHotkey.PublicKey())
	// With the fast runtime's tempo of 10, the default 10-block admin freeze
	// window would close admin calls in 9 of every 10 blocks.
	n.sudo(t, n.call(t, "AdminUtils.sudo_set_admin_freeze_window", types.NewU16(0)))
	for _, key := range []evmKey{s.deployer, s.owner, s.auditor} {
		n.fundEVM(t, key.address, 10_000*tao)
	}

	// v1, in Deploy.s.sol's order.
	nonce := n.nonce(t, s.deployer)
	s.v1Escrow = newAccount(t)
	s.sink = n.deploy(t, s.deployer, withArgs(t, sink, s.netuid, newAccount(t),
		mirror(crypto.CreateAddress(s.deployer.address, nonce)), s.deployer.address))
	s.v1Vault = n.deploy(t, s.deployer, withArgs(t, s.v1VaultArtifact, s.netuid, s.v1Escrow,
		mirror(crypto.CreateAddress(s.deployer.address, nonce+1)), uint64(1), uint64(minimumTransferTaoRao), s.deployer.address))
	implementation := n.deploy(t, s.deployer, s.coordinatorArtifact.creation)
	n.send(t, s.deployer, s.v1Vault, s.v1VaultArtifact, raoWei(maximumBurn), "registerEscrow", uint64(maximumBurn))
	proxyAddress := crypto.CreateAddress(s.deployer.address, n.nonce(t, s.deployer))
	initialize := s.coordinatorArtifact.pack(t, "initialize", s.netuid, s.owner.address, s.owner.address,
		mirror(proxyAddress), s.v1Vault, s.sink, s.owner.address, policy(0, setupEpochBlocks))
	s.v1 = n.deploy(t, s.deployer, withArgs(t, proxy, implementation, initialize))
	n.send(t, s.deployer, s.sink, sink, nil, "setRecorderOnce", s.v1)
	n.send(t, s.deployer, s.v1Vault, s.v1VaultArtifact, nil, "setCoordinatorOnce", s.v1)
	n.send(t, s.owner, s.v1, s.coordinatorArtifact, nil, "schedulePolicy", policy(1, cadence))
	if config.shortFrom != 0 {
		n.send(t, s.owner, s.v1, s.coordinatorArtifact, nil, "schedulePolicy", policy(config.shortFrom, shortEpochBlocks))
	}
	for noId := uint64(1); noId <= uint64(config.operators); noId++ {
		op := &operator{rootSigner: newEVMKey(t), v1Pool: newAccount(t), pool: newAccount(t)}
		n.fundEVM(t, op.rootSigner.address, 100*tao)
		n.send(t, s.owner, s.v1, s.coordinatorArtifact, raoWei(maximumBurn), "registerOperator", new(big.Int).SetUint64(noId),
			newAccount(t), op.v1Pool, newAccount(t), newEVMKey(t).address, op.rootSigner.address, uint64(0), uint64(maximumBurn))
		s.operators[noId] = op
	}

	// v2: vault2's creation and its escrow registration go out back to back
	// (SPEC A7).
	s.vault = crypto.CreateAddress(s.deployer.address, n.nonce(t, s.deployer))
	s.vaultColdkey = mirror(s.vault)
	s.escrow = newAccount(t)
	if deployed := n.deploy(t, s.deployer, withArgs(t, s.vaultArtifact, s.netuid, s.escrow, s.vaultColdkey, s.deployer.address, demandFeeWei)); deployed != s.vault {
		t.Fatalf("vault2 deployed at %s, predicted %s", deployed, s.vault)
	}
	n.send(t, s.deployer, s.vault, s.vaultArtifact, raoWei(maximumBurn), "registerEscrow", uint64(maximumBurn))
	implementation = n.deploy(t, s.deployer, s.settlementArtifact.creation)
	s.settlement = n.deploy(t, s.deployer, withArgs(t, proxy, implementation, s.settlementArtifact.pack(t, "initialize",
		s.owner.address, s.v1, s.vault, []common.Address{s.auditor.address}, big.NewInt(1))))
	n.send(t, s.deployer, s.vault, s.vaultArtifact, nil, "setCoordinatorOnce", s.settlement)
	s.registerPool(t, 1, 1)
	return s
}

// registerPool joins v1 operator noId's pool to v2 from firstEpoch through
// STSettlement2, paying the burn ceiling and receiving the surplus.
func (self *stack) registerPool(t *testing.T, noId, firstEpoch uint64) *gethtypes.Receipt {
	t.Helper()
	return self.n.send(t, self.owner, self.settlement, self.settlementArtifact, raoWei(maximumBurn), "registerOperatorPool",
		new(big.Int).SetUint64(noId), self.operators[noId].pool, uint64(maximumBurn), firstEpoch)
}

// end returns the v1 coordinator's epochEndBlock(epoch).
func (self *stack) end(t *testing.T, epoch uint64) uint64 {
	t.Helper()
	return self.n.read(t, self.v1, self.coordinatorArtifact, "epochEndBlock", new(big.Int).SetUint64(epoch))[0].(*big.Int).Uint64()
}

// inWindow seals blocks until epoch's window [end(epoch), end(epoch) +
// window] holds both the latest block, at which eth_call and eth_estimateGas
// run, and the next one, in which a transaction runs. It fails if the window
// has passed.
func (self *stack) inWindow(t *testing.T, epoch uint64) {
	t.Helper()
	end := self.end(t, epoch)
	if head := self.n.number(t); head < end {
		self.n.sealUntil(t, end)
	} else if head+1 > end+window {
		t.Fatalf("epoch %d's window [%d, %d] has passed (head %d)", epoch, end, end+window, head)
	}
}

// close calls closeOperatorEpoch(epoch, noId) inside epoch's window.
func (self *stack) close(t *testing.T, epoch, noId uint64) *gethtypes.Receipt {
	t.Helper()
	self.inWindow(t, epoch)
	return self.n.send(t, self.owner, self.settlement, self.settlementArtifact, nil, "closeOperatorEpoch",
		new(big.Int).SetUint64(epoch), new(big.Int).SetUint64(noId))
}

// poolRecord and pendingRoot are ISTSettlementVault2.Pool and Pending.
type poolRecord struct {
	Hotkey     [32]byte
	FirstEpoch uint64
	Registered bool
}

type pendingRoot struct {
	Seq            uint64
	Root           [32]byte
	Total          *big.Int
	LeafCount      uint64
	Epoch          uint64
	LedgerHash     [32]byte
	CommittedBlock uint64
	OpenDemands    uint64
}

func (self *stack) pool(t *testing.T, noId uint64) poolRecord {
	t.Helper()
	return *abi.ConvertType(self.n.read(t, self.vault, self.vaultArtifact, "pools", id(noId))[0], new(poolRecord)).(*poolRecord)
}

func (self *stack) pending(t *testing.T, noId uint64) pendingRoot {
	t.Helper()
	return *abi.ConvertType(self.n.read(t, self.vault, self.vaultArtifact, "pending", id(noId))[0], new(pendingRoot)).(*pendingRoot)
}

// vaultUint reads a uint256 view of vault2.
func (self *stack) vaultUint(t *testing.T, method string, args ...any) *big.Int {
	t.Helper()
	return self.n.read(t, self.vault, self.vaultArtifact, method, args...)[0].(*big.Int)
}

// requireConservation requires vault2's conservation identity (§4.2 G1).
func (self *stack) requireConservation(t *testing.T) {
	t.Helper()
	if !self.n.read(t, self.vault, self.vaultArtifact, "conservationHolds")[0].(bool) {
		t.Fatalf("conservation fails: captured %s, paid %s, accounted %s, live escrow %s",
			self.vaultUint(t, "totalCaptured"), self.vaultUint(t, "totalPaid"), self.vaultUint(t, "escrowAccounted"), self.vaultUint(t, "liveEscrowStake"))
	}
}

// locked returns the collateral locked on vault2's position on hotkey.
func (self *stack) locked(t *testing.T, hotkey [32]byte) uint64 {
	t.Helper()
	return self.n.read(t, stakingAddress, self.collateralArtifact, "getMinerCollateral", self.netuid, hotkey, self.vaultColdkey)[1].(uint64)
}

// nonce returns an account's next EVM nonce.
func (self *node) nonce(t *testing.T, key evmKey) uint64 {
	t.Helper()
	nonce, err := self.eth.NonceAt(self.ctx, key.address, nil)
	if err != nil {
		t.Fatal(err)
	}
	return nonce
}

func id(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}
